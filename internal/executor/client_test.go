package executor

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/peter7775/opensearch-query-gateway/internal/config"
)

func fakeCluster(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/docs/_search":
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), `"match"`) {
				w.WriteHeader(400)
				_, _ = io.WriteString(w, `{"error":{"type":"search_phase_execution_exception","reason":"all shards failed","root_cause":[{"type":"query_shard_exception","reason":"failed to create query"}]},"status":400}`)
				return
			}
			_, _ = io.WriteString(w, `{"hits":{"total":{"value":3}}}`)
		case r.URL.Path == "/docs/_mapping":
			_, _ = io.WriteString(w, `{"docs":{"mappings":{"properties":{"a":{"type":"keyword"}}}}}`)
		case r.URL.Path == "/missing/_mapping":
			w.WriteHeader(404)
			_, _ = io.WriteString(w, `{"error":{"type":"index_not_found_exception","reason":"no such index [missing]"},"status":404}`)
		default:
			_, _ = io.WriteString(w, `{}`)
		}
	}))
}

func TestClient(t *testing.T) {
	srv := fakeCluster(t)
	defer srv.Close()
	c, err := NewClient(config.OpenSearchConfig{Addresses: []string{srv.URL}, Index: "docs"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	res, err := c.Search(ctx, "", map[string]interface{}{"query": map[string]interface{}{"match": map[string]interface{}{"a": "b"}}})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if res["hits"].(map[string]interface{})["total"].(map[string]interface{})["value"].(float64) != 3 {
		t.Errorf("unexpected result %v", res)
	}

	_, err = c.Search(ctx, "docs", map[string]interface{}{"query": map[string]interface{}{"term": 1}})
	var oe *Error
	if !errors.As(err, &oe) || oe.Status != 400 || oe.Type != "query_shard_exception" || oe.Reason != "failed to create query" {
		t.Errorf("unexpected error %#v", err)
	}

	raw, err := c.GetMapping(ctx, "docs")
	if err != nil || !strings.Contains(string(raw), "keyword") {
		t.Errorf("mapping: %s %v", raw, err)
	}
	if _, err := c.GetMapping(ctx, "missing"); !errors.As(err, &oe) || oe.Status != 404 {
		t.Errorf("missing mapping: %v", err)
	}
	if err := c.Ping(ctx); err != nil {
		t.Errorf("ping: %v", err)
	}
}
