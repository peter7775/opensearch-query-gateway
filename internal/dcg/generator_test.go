package dcg

import (
	"strings"
	"testing"
)

func TestGenerate(t *testing.T) {
	g := NewGenerator()
	rules := []Rule{
		{
			Head: "expr",
			Body: Seq{Items: []Expr{
				Symbol{Name: "term"},
				Terminal{Value: "plus"},
				Symbol{Name: "factor"},
			}},
		},
	}

	out, err := g.Generate(rules)
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}

	if !strings.Contains(out, "expr(S0,S)") {
		t.Fatalf("unexpected output: %s", out)
	}
}
