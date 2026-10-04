package schema

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/peter7775/opensearch-query-gateway/internal/rules"
)

func loadLogs(t *testing.T) *IndexSchema {
	t.Helper()
	raw, err := os.ReadFile("testdata/logs_mapping.json")
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewMapper().FromJSON("logs", raw)
	if err != nil {
		t.Fatalf("FromJSON: %v", err)
	}
	return s
}

func TestMapperFlattensMapping(t *testing.T) {
	s := loadLogs(t)
	for _, p := range []string{"@timestamp", "service", "message", "message.keyword", "http", "http.status", "http.user_agent", "tags", "tags.name"} {
		if !s.HasField(p) {
			t.Errorf("missing field %s", p)
		}
	}
	f, _ := s.FieldByPath("message.keyword")
	if f.Parent != "message" || f.Type != "keyword" || f.IgnoreAbove == nil || *f.IgnoreAbove != 256 {
		t.Errorf("unexpected multi-field: %+v", f)
	}
	if f, _ := s.FieldByPath("http"); !f.IsObject {
		t.Errorf("http should be object")
	}
	if f, _ := s.FieldByPath("tags"); !f.IsNested {
		t.Errorf("tags should be nested")
	}
}

func TestMapperAcceptsAllShapes(t *testing.T) {
	m := NewMapper()
	for _, in := range []string{
		`{"properties":{"a":{"type":"keyword"}}}`,
		`{"mappings":{"properties":{"a":{"type":"keyword"}}}}`,
		`{"idx":{"mappings":{"properties":{"a":{"type":"keyword"}}}}}`,
	} {
		s, err := m.FromJSON("x", []byte(in))
		if err != nil || !s.HasField("a") {
			t.Errorf("shape %s: %v", in, err)
		}
	}
	if _, err := m.FromJSON("x", []byte(`{"foo":1}`)); err == nil {
		t.Error("expected error for mapping without properties")
	}
}

func TestAnalyze(t *testing.T) {
	in := Analyze(loadLogs(t), "Application logs")
	if in.Summary.FieldsTotal != 10 {
		t.Errorf("fields_total = %d", in.Summary.FieldsTotal)
	}
	// @timestamp, service, severity, message, message.keyword, http.status, tags.name
	if in.Summary.Searchable != 7 {
		t.Errorf("searchable = %d", in.Summary.Searchable)
	}
	// @timestamp, service, severity, message.keyword, http.status, tags.name
	if in.Summary.Aggregatable != 6 {
		t.Errorf("aggregatable = %d", in.Summary.Aggregatable)
	}
	if in.Summary.ObjectFields != 1 || in.Summary.NestedFields != 1 {
		t.Errorf("object/nested = %d/%d", in.Summary.ObjectFields, in.Summary.NestedFields)
	}
	joined := strings.Join(in.Highlights, " | ")
	for _, want := range []string{"Time-based filtering", "service", "message supports full-text"} {
		if !strings.Contains(joined, want) {
			t.Errorf("highlights %q missing %q", joined, want)
		}
	}
	for _, f := range in.Fields {
		if f.Path == "@timestamp" && f.Notes != "Primary time filter; format strict_date_optional_time||epoch_millis" {
			t.Errorf("timestamp notes: %q", f.Notes)
		}
		if f.Path == "http.user_agent" && (f.Searchable || f.Aggregatable) {
			t.Errorf("user_agent has index/doc_values disabled")
		}
	}
}

func TestAtom(t *testing.T) {
	cases := map[string]string{
		"logs":        "logs",
		"http.status": "'http.status'",
		"@timestamp":  "'@timestamp'",
		"Upper":       "'Upper'",
		"it's":        `'it\'s'`,
		"":            "''",
	}
	for in, want := range cases {
		if got := atom(in); got != want {
			t.Errorf("atom(%q) = %s, want %s", in, got, want)
		}
	}
}

// Výstup PrologWriteru musí jít načíst do rule enginu a pole ze schématu se
// tím stanou validními pro dotazy.
func TestPrologOutputLoadsIntoRules(t *testing.T) {
	out := NewPrologWriter().Write(loadLogs(t))
	path := filepath.Join(t.TempDir(), "logs.pl")
	if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
		t.Fatal(err)
	}
	e, err := rules.New("../rules/bootstrap.pl", 0, path)
	if err != nil {
		t.Fatalf("load generated facts: %v\n%s", err, out)
	}
	ctx := context.Background()
	if err := e.Validate(ctx, "http.status", "range", "500 TO 599"); err != nil {
		t.Errorf("http.status range: %v", err)
	}
	if err := e.Validate(ctx, "message", "range", "a TO b"); err == nil {
		t.Errorf("range over text field must be rejected")
	}
	if err := e.Validate(ctx, "severity", "eq", "error"); err != nil {
		t.Errorf("severity eq: %v", err)
	}
	err = e.Validate(ctx, "tags.name", "eq", "x")
	if err == nil || !strings.Contains(err.Error(), "nested") {
		t.Errorf("nested sub-field must be rejected with a clear reason, got %v", err)
	}
}
