package dslbuilder

import (
	"encoding/json"
	"testing"

	"github.com/peter7775/opensearch-query-gateway/internal/parser"
)

func jsonOf(t *testing.T, v interface{}) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestBuildDSLCases(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{`views:>=100`, `{"query":{"range":{"views":{"gte":"100"}}}}`},
		{`views:<10`, `{"query":{"range":{"views":{"lt":"10"}}}}`},
		{`views:[100 TO *]`, `{"query":{"range":{"views":{"gte":"100"}}}}`},
		{`views:[* TO 5]`, `{"query":{"range":{"views":{"lte":"5"}}}}`},
		{`published_at:["2024-01-01T00:00:00" TO now]`, `{"query":{"range":{"published_at":{"gte":"2024-01-01T00:00:00","lte":"now"}}}}`},
		{`last:15m`, `{"query":{"range":{"@timestamp":{"gte":"now-15m"}}}}`},
		{`NOT status:archived`, `{"query":{"bool":{"must_not":[{"match":{"status":"archived"}}]}}}`},
		{`service:gateway and severity:error and last:1h`, `{"query":{"bool":{"must":[{"match":{"service":"gateway"}},{"match":{"severity":"error"}},{"range":{"@timestamp":{"gte":"now-1h"}}}]}}}`},
		{`a:x or b:y`, `{"query":{"bool":{"minimum_should_match":1,"should":[{"match":{"a":"x"}},{"match":{"b":"y"}}]}}}`},
	}
	p := parser.New()
	for _, c := range cases {
		ast, err := p.Parse(c.in)
		if err != nil {
			t.Errorf("%s: parse: %v", c.in, err)
			continue
		}
		if got := jsonOf(t, New().Build(ast)); got != c.want {
			t.Errorf("%s:\n got  %s\n want %s", c.in, got, c.want)
		}
	}
}

func TestCustomTimeField(t *testing.T) {
	ast, err := parser.New().Parse(`last:7d`)
	if err != nil {
		t.Fatal(err)
	}
	got := jsonOf(t, New(WithTimeField("published_at")).Build(ast))
	if got != `{"query":{"range":{"published_at":{"gte":"now-7d"}}}}` {
		t.Errorf("got %s", got)
	}
}

func TestBuildSearchPaging(t *testing.T) {
	ast, _ := parser.New().Parse(`tag:go`)
	body := New().BuildSearch(ast, Page{Size: 20, From: 40, Sort: []SortField{{Field: "published_at", Desc: true}}, Track: true})
	want := `{"from":40,"query":{"match":{"tag":"go"}},"size":20,"sort":[{"published_at":{"order":"desc"}}],"track_total_hits":true}`
	if got := jsonOf(t, body); got != want {
		t.Errorf("got %s", got)
	}
}
