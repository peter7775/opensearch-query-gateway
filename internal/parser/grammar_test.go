package parser

import "testing"

func TestParseSimpleClause(t *testing.T) {
	p := New()
	q, err := p.Parse(`tag:golang`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	c := q.Root.Left.Left.Primary.Clause
	if c == nil || c.Field != "tag" || c.Value.Word == nil || *c.Value.Word != "golang" {
		t.Fatalf("unexpected clause: %+v", c)
	}
}

func TestParseImplicitAnd(t *testing.T) {
	p := New()
	q, err := p.Parse(`tag:golang status:published`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(q.Root.Left.Right) != 1 {
		t.Fatalf("expected implicit AND to produce one AndOp, got %d", len(q.Root.Left.Right))
	}
}

func TestParseAndOr(t *testing.T) {
	p := New()
	q, err := p.Parse(`tag:golang AND status:published OR tag:go`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(q.Root.Right) != 1 {
		t.Fatalf("expected one OR branch, got %d", len(q.Root.Right))
	}
	if len(q.Root.Left.Right) != 1 {
		t.Fatalf("expected one AND branch on left side, got %d", len(q.Root.Left.Right))
	}
}

func TestParseNotAndGroup(t *testing.T) {
	p := New()
	q, err := p.Parse(`(tag:golang OR tag:go) AND NOT status:archived`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if q.Root.Left.Left.Not {
		t.Fatal("first operand should not be negated")
	}
	if q.Root.Left.Left.Primary.Group == nil {
		t.Fatal("expected first operand to be a parenthesized group")
	}
	if len(q.Root.Left.Right) != 1 || !q.Root.Left.Right[0].Right.Not {
		t.Fatal("expected second operand to be negated")
	}
}

func TestParseImplicitAndNot(t *testing.T) {
	p := New()
	q, err := p.Parse(`tag:golang NOT status:archived`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(q.Root.Left.Right) != 1 || !q.Root.Left.Right[0].Right.Not {
		t.Fatal("expected NOT to apply to second operand without requiring explicit AND")
	}
}

func TestParseWildcard(t *testing.T) {
	p := New()
	q, err := p.Parse(`title:jarn*`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	c := q.Root.Left.Left.Primary.Clause
	if c.Value.Word == nil || *c.Value.Word != "jarn*" {
		t.Fatalf("unexpected value: %+v", c.Value)
	}
}

func TestParseRange(t *testing.T) {
	p := New()
	q, err := p.Parse(`published_at:[2024-01-01 TO 2024-12-31]`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	c := q.Root.Left.Left.Primary.Clause
	if c.Value.Range == nil || c.Value.Range.Min != "2024-01-01" || c.Value.Range.Max != "2024-12-31" {
		t.Fatalf("unexpected range: %+v", c.Value.Range)
	}
}

func TestParseNestedGroups(t *testing.T) {
	p := New()
	q, err := p.Parse(`((tag:golang OR tag:go) AND NOT status:archived) OR title:jarn*`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(q.Root.Right) != 1 {
		t.Fatalf("expected top-level OR, got %d branches", len(q.Root.Right))
	}
	if q.Root.Left.Left.Primary.Group == nil {
		t.Fatal("expected outer group to be parsed as a nested expression")
	}
}

func TestParseFuzzyBareWord(t *testing.T) {
	p := New()
	q, err := p.Parse(`title:jarnik~`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	c := q.Root.Left.Left.Primary.Clause
	if c.Value.Fuzzy == nil || c.Value.Fuzzy.Distance != "" {
		t.Fatalf("expected bare fuzzy modifier, got %+v", c.Value.Fuzzy)
	}
}

func TestParseFuzzyWithDistance(t *testing.T) {
	p := New()
	q, err := p.Parse(`title:jarnik~2`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	c := q.Root.Left.Left.Primary.Clause
	if c.Value.Fuzzy == nil || c.Value.Fuzzy.Distance != "2" {
		t.Fatalf("expected fuzzy distance 2, got %+v", c.Value.Fuzzy)
	}
}

func TestParseBoost(t *testing.T) {
	p := New()
	q, err := p.Parse(`tag:golang^2.5`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	c := q.Root.Left.Left.Primary.Clause
	if c.Value.Boost == nil || c.Value.Boost.Factor != "2.5" {
		t.Fatalf("expected boost 2.5, got %+v", c.Value.Boost)
	}
}

func TestParseFuzzyAndBoostCombined(t *testing.T) {
	p := New()
	q, err := p.Parse(`title:jarnik~2^1.5`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	c := q.Root.Left.Left.Primary.Clause
	if c.Value.Fuzzy == nil || c.Value.Fuzzy.Distance != "2" {
		t.Fatalf("expected fuzzy distance 2, got %+v", c.Value.Fuzzy)
	}
	if c.Value.Boost == nil || c.Value.Boost.Factor != "1.5" {
		t.Fatalf("expected boost 1.5, got %+v", c.Value.Boost)
	}
}

func TestParsePhraseWithSlopAndBoost(t *testing.T) {
	p := New()
	q, err := p.Parse(`title:"hello world"~3^2`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	c := q.Root.Left.Left.Primary.Clause
	if c.Value.Phrase == nil || *c.Value.Phrase != "hello world" {
		t.Fatalf("unexpected phrase: %+v", c.Value.Phrase)
	}
	if c.Value.Fuzzy == nil || c.Value.Fuzzy.Distance != "3" {
		t.Fatalf("expected slop 3, got %+v", c.Value.Fuzzy)
	}
	if c.Value.Boost == nil || c.Value.Boost.Factor != "2" {
		t.Fatalf("expected boost 2, got %+v", c.Value.Boost)
	}
}

func TestParseRangeWithBoost(t *testing.T) {
	p := New()
	q, err := p.Parse(`published_at:[2024-01-01 TO 2024-12-31]^2`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	c := q.Root.Left.Left.Primary.Clause
	if c.Value.Range == nil {
		t.Fatal("expected range value")
	}
	if c.Value.Boost == nil || c.Value.Boost.Factor != "2" {
		t.Fatalf("expected boost 2, got %+v", c.Value.Boost)
	}
}

func TestWalkClauses(t *testing.T) {
	p := New()
	q, err := p.Parse(`(tag:golang OR tag:go) AND NOT status:archived`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var fields []string
	q.WalkClauses(func(c *Clause) {
		fields = append(fields, c.Field)
	})
	if len(fields) != 3 {
		t.Fatalf("expected 3 clauses, got %d: %v", len(fields), fields)
	}
}

func TestWalkClausesMutation(t *testing.T) {
	p := New()
	q, err := p.Parse(`author:petr`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	q.WalkClauses(func(c *Clause) {
		c.Field = "created_by"
	})
	if q.Root.Left.Left.Primary.Clause.Field != "created_by" {
		t.Fatal("expected in-place mutation of clause field to persist in the tree")
	}
}
