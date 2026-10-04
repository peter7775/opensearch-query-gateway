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

func TestCompileAltInsideSeqIsParenthesized(t *testing.T) {
	out, err := Compile(`q --> "a", ("b" | "c").`)
	if err != nil {
		t.Fatal(err)
	}
	want := "q(S0,S) :- S0 = ['a'|S1], (S1 = ['b'|S] ; S1 = ['c'|S])."
	if out != want {
		t.Errorf("got  %s\nwant %s", out, want)
	}
}

func TestCompileRejectsIllegalInput(t *testing.T) {
	for _, src := range []string{`q --> a ; b.`, `q --> "unterminated.`, `q --> (a | b.`} {
		if _, err := Compile(src); err == nil {
			t.Errorf("expected error for %q", src)
		}
	}
}
