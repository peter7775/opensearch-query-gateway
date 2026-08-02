package dcg

import "testing"

func TestParseRules(t *testing.T) {
	p := NewParser(`expr --> term "plus" factor.`)
	rules, err := p.ParseRules()
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(rules))
	}
	if rules[0].Head != "expr" {
		t.Fatalf("expected expr, got %s", rules[0].Head)
	}
}

func TestParseAlt(t *testing.T) {
	p := NewParser(`expr --> term | factor.`)
	rules, err := p.ParseRules()
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(rules))
	}
}
