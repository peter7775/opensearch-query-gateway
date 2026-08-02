package dslbuilder

import (
	"testing"

	"github.com/example/opensearch-query-gateway/internal/parser"
)

func build(t *testing.T, q string) map[string]interface{} {
	t.Helper()
	p := parser.New()
	ast, err := p.Parse(q)
	if err != nil {
		t.Fatalf("parse error for %q: %v", q, err)
	}
	return New().Build(ast)
}

func TestBuildSimpleMatch(t *testing.T) {
	body := build(t, `tag:golang`)
	query := body["query"].(map[string]interface{})
	match, ok := query["match"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected match query, got %+v", query)
	}
	if match["tag"] != "golang" {
		t.Fatalf("unexpected match value: %+v", match)
	}
}

func TestBuildWildcard(t *testing.T) {
	body := build(t, `title:jarn*`)
	query := body["query"].(map[string]interface{})
	wc, ok := query["wildcard"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected wildcard query, got %+v", query)
	}
	field, ok := wc["title"].(map[string]interface{})
	if !ok || field["value"] != "jarn*" {
		t.Fatalf("unexpected wildcard field: %+v", wc)
	}
}

func TestBuildSingleCharWildcard(t *testing.T) {
	body := build(t, `code:a?c`)
	query := body["query"].(map[string]interface{})
	wc, ok := query["wildcard"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected wildcard query for ? pattern, got %+v", query)
	}
	field := wc["code"].(map[string]interface{})
	if field["value"] != "a?c" {
		t.Fatalf("unexpected wildcard value: %+v", field)
	}
}

func TestBuildOrGroupAndNot(t *testing.T) {
	body := build(t, `(tag:golang OR tag:go) AND NOT status:archived`)
	query := body["query"].(map[string]interface{})
	boolq, ok := query["bool"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected top-level bool query, got %+v", query)
	}

	must := boolq["must"].([]map[string]interface{})
	if len(must) != 1 {
		t.Fatalf("expected 1 must clause (the OR group), got %d", len(must))
	}

	group, ok := must[0]["bool"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected nested bool for OR group, got %+v", must[0])
	}
	should, ok := group["should"].([]map[string]interface{})
	if !ok || len(should) != 2 {
		t.Fatalf("expected 2 should clauses, got %+v", group["should"])
	}

	mustNot := boolq["must_not"].([]map[string]interface{})
	if len(mustNot) != 1 {
		t.Fatalf("expected 1 must_not clause, got %d", len(mustNot))
	}
}

func TestBuildImplicitAnd(t *testing.T) {
	body := build(t, `tag:golang status:published`)
	query := body["query"].(map[string]interface{})
	boolq, ok := query["bool"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected bool query for implicit AND, got %+v", query)
	}
	must := boolq["must"].([]map[string]interface{})
	if len(must) != 2 {
		t.Fatalf("expected 2 must clauses, got %d", len(must))
	}
}

func TestBuildRange(t *testing.T) {
	body := build(t, `published_at:[2024-01-01 TO 2024-12-31]`)
	query := body["query"].(map[string]interface{})
	rangeQ, ok := query["range"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected range query, got %+v", query)
	}
	field := rangeQ["published_at"].(map[string]interface{})
	if field["gte"] != "2024-01-01" || field["lte"] != "2024-12-31" {
		t.Fatalf("unexpected range: %+v", field)
	}
}

func TestBuildBoostOnMatch(t *testing.T) {
	body := build(t, `tag:golang^2.5`)
	query := body["query"].(map[string]interface{})
	match, ok := query["match"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected match query, got %+v", query)
	}
	field, ok := match["tag"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected expanded match form with boost, got %+v", match["tag"])
	}
	if field["query"] != "golang" || field["boost"] != 2.5 {
		t.Fatalf("unexpected match+boost params: %+v", field)
	}
}

func TestBuildFuzzyWord(t *testing.T) {
	body := build(t, `title:jarnik~2`)
	query := body["query"].(map[string]interface{})
	fuzzy, ok := query["fuzzy"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected fuzzy query, got %+v", query)
	}
	field := fuzzy["title"].(map[string]interface{})
	if field["value"] != "jarnik" || field["fuzziness"] != 2 {
		t.Fatalf("unexpected fuzzy params: %+v", field)
	}
}

func TestBuildFuzzyWordBareDefaultsToAuto(t *testing.T) {
	body := build(t, `title:jarnik~`)
	query := body["query"].(map[string]interface{})
	fuzzy := query["fuzzy"].(map[string]interface{})
	field := fuzzy["title"].(map[string]interface{})
	if field["fuzziness"] != "AUTO" {
		t.Fatalf("expected AUTO fuzziness for bare ~, got %+v", field["fuzziness"])
	}
}

func TestBuildFuzzyAndBoostCombined(t *testing.T) {
	body := build(t, `title:jarnik~2^1.5`)
	query := body["query"].(map[string]interface{})
	fuzzy := query["fuzzy"].(map[string]interface{})
	field := fuzzy["title"].(map[string]interface{})
	if field["fuzziness"] != 2 || field["boost"] != 1.5 {
		t.Fatalf("unexpected fuzzy+boost params: %+v", field)
	}
}

func TestBuildPhraseWithSlopAndBoost(t *testing.T) {
	body := build(t, `title:"hello world"~3^2`)
	query := body["query"].(map[string]interface{})
	phrase, ok := query["match_phrase"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected match_phrase query, got %+v", query)
	}
	field, ok := phrase["title"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected expanded match_phrase form, got %+v", phrase["title"])
	}
	if field["query"] != "hello world" || field["slop"] != 3 || field["boost"] != 2.0 {
		t.Fatalf("unexpected phrase params: %+v", field)
	}
}

func TestBuildRangeWithBoost(t *testing.T) {
	body := build(t, `published_at:[2024-01-01 TO 2024-12-31]^2`)
	query := body["query"].(map[string]interface{})
	rangeQ := query["range"].(map[string]interface{})
	field := rangeQ["published_at"].(map[string]interface{})
	if field["boost"] != 2.0 {
		t.Fatalf("expected boost 2 on range query, got %+v", field)
	}
}

func TestBuildWildcardWithBoost(t *testing.T) {
	body := build(t, `title:jarn*^3`)
	query := body["query"].(map[string]interface{})
	wc := query["wildcard"].(map[string]interface{})
	field := wc["title"].(map[string]interface{})
	if field["boost"] != 3.0 {
		t.Fatalf("expected boost 3 on wildcard query, got %+v", field)
	}
}

func TestBuildPhrase(t *testing.T) {
	body := build(t, `title:"hello world"`)
	query := body["query"].(map[string]interface{})
	phrase, ok := query["match_phrase"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected match_phrase query, got %+v", query)
	}
	if phrase["title"] != "hello world" {
		t.Fatalf("unexpected phrase value: %+v", phrase)
	}
}
