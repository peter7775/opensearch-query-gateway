package rules

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func newEngine(t *testing.T, extra ...string) *Engine {
	t.Helper()
	e, err := New("bootstrap.pl", 0, extra...)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	return e
}

func TestNormalizeField(t *testing.T) {
	e := newEngine(t)
	ctx := context.Background()
	cases := map[string]string{
		"author": "created_by",
		"tag":    "tags",
		"ts":     "@timestamp",
		"title":  "title",
		"nope":   "nope",
	}
	for in, want := range cases {
		got, err := e.NormalizeField(ctx, in)
		if err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		if got != want {
			t.Errorf("NormalizeField(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidate(t *testing.T) {
	e := newEngine(t)
	ctx := context.Background()

	if err := e.Validate(ctx, "title", "eq", "go"); err != nil {
		t.Errorf("title eq should pass: %v", err)
	}
	if err := e.Validate(ctx, "published_at", "range", "2020 TO 2021"); err != nil {
		t.Errorf("published_at range should pass: %v", err)
	}

	err := e.Validate(ctx, "secret", "eq", "x")
	var rej *RejectionError
	if !errors.As(err, &rej) || !errors.Is(err, ErrRejected) {
		t.Fatalf("expected rejection, got %v", err)
	}
	if rej.Reason != "unknown field" {
		t.Errorf("unexpected reason %q", rej.Reason)
	}

	err = e.Validate(ctx, "title", "range", "a TO b")
	if !errors.As(err, &rej) {
		t.Fatalf("expected rejection for range on text, got %v", err)
	}
	if rej.Reason != "range queries require a numeric or date field" {
		t.Errorf("unexpected reason %q", rej.Reason)
	}
}

func TestSchemaFacts(t *testing.T) {
	dir := t.TempDir()
	facts := filepath.Join(dir, "schema.pl")
	src := "index(logs).\nfield(logs, http).\nfield(logs, 'http.status').\nfield_type(logs, 'http.status', integer).\nobject(logs, http).\n"
	if err := os.WriteFile(facts, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	e := newEngine(t, facts)
	ctx := context.Background()

	if err := e.Validate(ctx, "http.status", "range", "500 TO 599"); err != nil {
		t.Errorf("schema numeric field should allow range: %v", err)
	}
	if err := e.Validate(ctx, "http", "eq", "x"); err == nil {
		t.Errorf("object field must not be queryable directly")
	}
}

func TestConcurrentQueries(t *testing.T) {
	e := newEngine(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if got, err := e.NormalizeField(ctx, "tag"); err != nil || got != "tags" {
				t.Errorf("got %q, %v", got, err)
			}
		}()
	}
	wg.Wait()
}

func TestDCGGrammarFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "severity.dcg")
	src := "% závažnosti logů\nseverity --> \"error\" | \"warn\" | \"info\".\nfilter --> \"severity\" \":\" severity.\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	e := newEngine(t, path)
	ctx := context.Background()
	ok, err := e.first(ctx, `filter(?, []).`, nil, []string{"severity", ":", "warn"})
	if err != nil || !ok {
		t.Fatalf("grammar should accept [severity, :, warn]: ok=%v err=%v", ok, err)
	}
	ok, err = e.first(ctx, `filter(?, []).`, nil, []string{"severity", ":", "fatal"})
	if err != nil || ok {
		t.Fatalf("grammar should reject fatal: ok=%v err=%v", ok, err)
	}
}
