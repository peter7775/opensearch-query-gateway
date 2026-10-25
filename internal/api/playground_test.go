package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/peter7775/opensearch-query-gateway/internal/config"
	"github.com/peter7775/opensearch-query-gateway/internal/dslbuilder"
	"github.com/peter7775/opensearch-query-gateway/internal/parser"
	"github.com/peter7775/opensearch-query-gateway/internal/rules"
)

func newPlaygroundTestServer(t *testing.T, rps float64) http.Handler {
	t.Helper()
	re, err := rules.New("../rules/bootstrap.pl", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = re.Close() })
	s := NewServer(parser.New(), re, dslbuilder.New(dslbuilder.WithTimeField("published_at")), nil, Options{
		OpenSearch:   config.OpenSearchConfig{Index: "documents"},
		MaxBodyBytes: 8192,
	})
	limiter := NewRateLimiter(rps, int(rps), time.Minute, nil)
	t.Cleanup(limiter.Close)
	return NewPlaygroundRouter(s, limiter, nil)
}

func TestPlaygroundAssets(t *testing.T) {
	h := newPlaygroundTestServer(t, 100)
	for _, tc := range []struct {
		path, contentType, content string
	}{
		{"/", "text/html", "Less JSON."},
		{"/assets/playground.css", "text/css", "@media"},
		{"/assets/playground.js", "javascript", `fetch("/v1/translate"`},
	} {
		t.Run(tc.path, func(t *testing.T) {
			rec, _ := do(t, h, "GET", tc.path, "")
			if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), tc.content) {
				t.Fatalf("asset: %d %s", rec.Code, rec.Body)
			}
			if !strings.Contains(rec.Header().Get("Content-Type"), tc.contentType) {
				t.Errorf("content type: %s", rec.Header().Get("Content-Type"))
			}
			if !strings.Contains(rec.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") ||
				rec.Header().Get("X-Content-Type-Options") != "nosniff" {
				t.Error("missing security headers")
			}
		})
	}
}

func TestPlaygroundRestrictedRoutes(t *testing.T) {
	h := newPlaygroundTestServer(t, 100)
	for _, path := range []string{"/v1/search", "/v1/schema/introspect", "/v1/schema/prolog", "/unknown", "/assets/missing.js"} {
		for _, method := range []string{"GET", "POST"} {
			rec, _ := do(t, h, method, path, `{"query":"tag:go"}`)
			if rec.Code != http.StatusNotFound {
				t.Errorf("%s %s: got %d", method, path, rec.Code)
			}
		}
	}
	for _, path := range []string{"/healthz", "/readyz"} {
		rec, _ := do(t, h, "GET", path, "")
		if rec.Code != http.StatusOK {
			t.Errorf("%s: %d", path, rec.Code)
		}
	}
	rec, _ := do(t, h, "GET", "/v1/translate", "")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("translate GET: %d", rec.Code)
	}
}

func TestPlaygroundExamples(t *testing.T) {
	h := newPlaygroundTestServer(t, 100)
	rec, _ := do(t, h, "GET", "/", "")
	// Read examples from the actual HTML so UI and backend cannot drift.
	matches := regexp.MustCompile(`data-query=(?:"([^"]*)"|'([^']*)')`).FindAllStringSubmatch(rec.Body.String(), -1)
	if len(matches) != 6 {
		t.Fatalf("expected 6 examples, got %d", len(matches))
	}
	for i, match := range matches {
		query := match[1] + match[2]
		body, _ := json.Marshal(map[string]string{"query": query})
		rec, out := do(t, h, "POST", "/v1/translate", string(body))
		if i == len(matches)-1 {
			if rec.Code != 400 || errCode(out) != "clause_rejected" {
				t.Errorf("invalid example: %d %s", rec.Code, rec.Body)
			}
		} else if rec.Code != 200 || out["dsl"] == nil {
			t.Errorf("%s: %d %s", query, rec.Code, rec.Body)
		}
		if i == 0 {
			norm, _ := json.Marshal(out["normalized_fields"])
			if string(norm) != `[{"from":"author","to":"created_by"}]` {
				t.Errorf("normalization: %s", norm)
			}
			dsl, _ := json.Marshal(out["dsl"])
			if string(dsl) != `{"query":{"bool":{"must":[{"match":{"created_by":"petr"}},{"range":{"views":{"gte":"100"}}}]}},"size":10}` {
				t.Errorf("DSL: %s", dsl)
			}
		}
	}
}

func TestPlaygroundErrorsAndLimits(t *testing.T) {
	h := newPlaygroundTestServer(t, 100)
	for _, tc := range []struct {
		body, code string
		status     int
	}{
		{`{"query":""}`, "empty_query", 400},
		{`{"query":"tag:(go"}`, "invalid_query", 400},
		{`{"query":"unknown:x"}`, "clause_rejected", 400},
		{`not json`, "invalid_body", 400},
		{`{"query":"tag:go","index":"private"}`, "index_not_allowed", 403},
		{`{"query":"` + strings.Repeat("x", 9000) + `"}`, "body_too_large", 413},
	} {
		rec, out := do(t, h, "POST", "/v1/translate", tc.body)
		if rec.Code != tc.status || errCode(out) != tc.code {
			t.Errorf("%s: %d %s", tc.code, rec.Code, rec.Body)
		}
	}
	limited := newPlaygroundTestServer(t, 1)
	do(t, limited, "POST", "/v1/translate", `{"query":"tag:go"}`)
	rec, out := do(t, limited, "POST", "/v1/translate", `{"query":"tag:go"}`)
	if rec.Code != 429 || errCode(out) != "rate_limited" {
		t.Errorf("rate limiting: %d %s", rec.Code, rec.Body)
	}
	for _, path := range []string{"/", "/healthz", "/readyz"} {
		req := httptest.NewRequest("GET", path, nil)
		rec := httptest.NewRecorder()
		limited.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Errorf("rate limit affected %s: %d", path, rec.Code)
		}
	}
}
