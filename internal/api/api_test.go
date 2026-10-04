package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/peter7775/opensearch-query-gateway/internal/config"
	"github.com/peter7775/opensearch-query-gateway/internal/dslbuilder"
	"github.com/peter7775/opensearch-query-gateway/internal/executor"
	"github.com/peter7775/opensearch-query-gateway/internal/parser"
	"github.com/peter7775/opensearch-query-gateway/internal/rules"
)

type fakeBackend struct {
	lastIndex string
	lastBody  map[string]interface{}
	err       error
	mapping   []byte
}

func (f *fakeBackend) Search(_ context.Context, index string, body map[string]interface{}) (map[string]interface{}, error) {
	f.lastIndex, f.lastBody = index, body
	if f.err != nil {
		return nil, f.err
	}
	return map[string]interface{}{"hits": map[string]interface{}{"total": map[string]interface{}{"value": 1}}}, nil
}

func (f *fakeBackend) GetMapping(context.Context, string) ([]byte, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.mapping, nil
}

func (f *fakeBackend) Ping(context.Context) error { return f.err }

func newTestServer(t *testing.T, be *fakeBackend, rps float64) http.Handler {
	t.Helper()
	re, err := rules.New("../rules/bootstrap.pl", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(parser.New(), re, dslbuilder.New(), be, Options{
		OpenSearch: config.OpenSearchConfig{Index: "documents", AllowedIndices: []string{"logs"}, Purpose: "test docs"},
		Search:     config.SearchConfig{DefaultSize: 10, MaxSize: 50, MaxFrom: 1000},
	})
	lim := NewRateLimiter(rps, int(rps), time.Minute, nil)
	t.Cleanup(lim.Close)
	return NewRouter(srv, lim, nil)
}

func do(t *testing.T, h http.Handler, method, path, body string) (*httptest.ResponseRecorder, map[string]interface{}) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out
}

func errCode(out map[string]interface{}) string {
	e, _ := out["error"].(map[string]interface{})
	c, _ := e["code"].(string)
	return c
}

func TestSearchHappyPath(t *testing.T) {
	be := &fakeBackend{}
	h := newTestServer(t, be, 100)
	rec, _ := do(t, h, "POST", "/v1/search", `{"query":"author:petr AND tag:go* AND views:>=10","size":5,"sort":["published_at:desc"]}`)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if rec.Header().Get("X-Request-ID") == "" {
		t.Error("missing X-Request-ID")
	}
	got, _ := json.Marshal(be.lastBody)
	want := `{"query":{"bool":{"must":[{"match":{"created_by":"petr"}},{"wildcard":{"tags":{"case_insensitive":true,"value":"go*"}}},{"range":{"views":{"gte":"10"}}}]}},"size":5,"sort":[{"published_at":{"order":"desc"}}]}`
	if string(got) != want {
		t.Errorf("DSL:\n got  %s\n want %s", got, want)
	}
	if be.lastIndex != "documents" {
		t.Errorf("index %q", be.lastIndex)
	}
}

func TestSearchExplain(t *testing.T) {
	h := newTestServer(t, &fakeBackend{}, 100)
	rec, out := do(t, h, "POST", "/v1/search", `{"query":"tag:go","explain":true}`)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	ex, ok := out["explain"].(map[string]interface{})
	if !ok || out["result"] == nil {
		t.Fatalf("missing explain/result: %v", out)
	}
	norm := ex["normalized_fields"].([]interface{})
	if len(norm) != 1 || norm[0].(map[string]interface{})["to"] != "tags" {
		t.Errorf("normalized: %v", norm)
	}
}

func TestTranslateDoesNotHitBackend(t *testing.T) {
	be := &fakeBackend{}
	h := newTestServer(t, be, 100)
	rec, out := do(t, h, "POST", "/v1/translate", `{"query":"last:15m"}`)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if be.lastBody != nil {
		t.Error("translate must not execute the query")
	}
	dsl, _ := json.Marshal(out["dsl"])
	if string(dsl) != `{"query":{"range":{"@timestamp":{"gte":"now-15m"}}},"size":10}` {
		t.Errorf("dsl %s", dsl)
	}
}

func TestSearchErrors(t *testing.T) {
	h := newTestServer(t, &fakeBackend{}, 1000)
	cases := []struct {
		body   string
		status int
		code   string
	}{
		{`not json`, 400, "invalid_body"},
		{`{"query":"tag:go","bogus":1}`, 400, "invalid_body"},
		{`{"query":""}`, 400, "empty_query"},
		{`{"query":"tag:(go"}`, 400, "invalid_query"},
		{`{"query":"secret:x"}`, 400, "clause_rejected"},
		{`{"query":"title:[a TO b]"}`, 400, "clause_rejected"},
		{`{"query":"last:soon"}`, 400, "invalid_query"},
		{`{"query":"tag:go","size":500}`, 400, "invalid_size"},
		{`{"query":"tag:go","from":999}`, 400, "invalid_from"},
		{`{"query":"tag:go","sort":["secret:desc"]}`, 400, "invalid_sort"},
		{`{"query":"tag:go","sort":["title:sideways"]}`, 400, "invalid_sort"},
		{`{"query":"tag:go","index":"other"}`, 403, "index_not_allowed"},
	}
	for _, c := range cases {
		rec, out := do(t, h, "POST", "/v1/search", c.body)
		if rec.Code != c.status || errCode(out) != c.code {
			t.Errorf("%s: got %d %q, want %d %q (%s)", c.body, rec.Code, errCode(out), c.status, c.code, rec.Body)
		}
	}
}

func TestBackendErrorMapping(t *testing.T) {
	be := &fakeBackend{err: &executor.Error{Status: 400, Type: "query_shard_exception", Reason: "failed to create query"}}
	h := newTestServer(t, be, 100)
	rec, out := do(t, h, "POST", "/v1/search", `{"query":"tag:go"}`)
	if rec.Code != 400 || errCode(out) != "opensearch_rejected" {
		t.Errorf("got %d %s", rec.Code, rec.Body)
	}
	be.err = &executor.Error{Status: 404, Type: "index_not_found_exception"}
	rec, _ = do(t, h, "POST", "/v1/search", `{"query":"tag:go"}`)
	if rec.Code != 404 {
		t.Errorf("got %d", rec.Code)
	}
	rec, _ = do(t, h, "GET", "/readyz", "")
	if rec.Code != 503 {
		t.Errorf("readyz with failing backend: %d", rec.Code)
	}
}

func TestMethodNotAllowed(t *testing.T) {
	h := newTestServer(t, &fakeBackend{}, 100)
	rec, _ := do(t, h, "GET", "/v1/search", "")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("got %d", rec.Code)
	}
}

func TestIntrospect(t *testing.T) {
	mapping, err := os.ReadFile("../schema/testdata/logs_mapping.json")
	if err != nil {
		t.Fatal(err)
	}
	h := newTestServer(t, &fakeBackend{mapping: mapping}, 100)

	rec, out := do(t, h, "GET", "/v1/schema/introspect?index=logs", "")
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if out["index"] != "logs" || out["summary"].(map[string]interface{})["fields_total"].(float64) != 10 {
		t.Errorf("unexpected introspection: %s", rec.Body)
	}

	rec, _ = do(t, h, "GET", "/v1/schema/introspect?index=forbidden", "")
	if rec.Code != 403 {
		t.Errorf("forbidden index: %d", rec.Code)
	}

	rec, out = do(t, h, "POST", "/v1/schema/introspect", `{"properties":{"a":{"type":"keyword"}}}`)
	if rec.Code != 200 || out["purpose"] != "test docs" {
		t.Errorf("offline introspection: %d %s", rec.Code, rec.Body)
	}

	rec, _ = do(t, h, "GET", "/v1/schema/prolog?index=logs", "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "field_type(logs, 'http.status', integer).") {
		t.Errorf("prolog: %d %s", rec.Code, rec.Body)
	}
}

func TestRateLimit(t *testing.T) {
	h := newTestServer(t, &fakeBackend{}, 2)
	codes := []int{}
	for i := 0; i < 4; i++ {
		rec, _ := do(t, h, "POST", "/v1/translate", `{"query":"tag:go"}`)
		codes = append(codes, rec.Code)
	}
	if codes[0] != 200 || codes[3] != 429 {
		t.Errorf("codes %v", codes)
	}
	// health endpointy nejsou limitované
	for i := 0; i < 5; i++ {
		if rec, _ := do(t, h, "GET", "/healthz", ""); rec.Code != 200 {
			t.Fatalf("healthz %d", rec.Code)
		}
	}
}

func TestClientIP(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.1:1234"
	r.Header.Set("X-Forwarded-For", "1.2.3.4, 10.0.0.1")
	if got := ClientIP(false)(r); got != "10.0.0.1" {
		t.Errorf("untrusted: %s", got)
	}
	if got := ClientIP(true)(r); got != "1.2.3.4" {
		t.Errorf("trusted: %s", got)
	}
}
