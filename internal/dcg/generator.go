package dcg

import (
	"fmt"
	"strings"
)

type Generator struct {
	counter int
}

func NewGenerator() *Generator {
	return &Generator{}
}

func (g *Generator) fresh() string {
	g.counter++
	return fmt.Sprintf("S%d", g.counter)
}

func (g *Generator) Generate(rules []Rule) (string, error) {
	var out []string
	for _, r := range rules {
		out = append(out, g.genRule(r))
	}
	return strings.Join(out, "\n"), nil
}

func (g *Generator) genRule(r Rule) string {
	return fmt.Sprintf("%s(S0,S) :- %s.", r.Head, g.genExpr(r.Body, "S0", "S"))
}

func (g *Generator) genExpr(e Expr, in, out string) string {
	switch v := e.(type) {
	case Symbol:
		return fmt.Sprintf("%s(%s,%s)", v.Name, in, out)
	case Terminal:
		return fmt.Sprintf("%s = [%q|%s]", in, v.Value, out)
	case Seq:
		return g.genSeq(v.Items, in, out)
	case Alt:
		return g.genAlt(v.Options, in, out)
	case Empty:
		return fmt.Sprintf("%s = %s", in, out)
	default:
		return "true"
	}
}

func (g *Generator) genSeq(items []Expr, in, out string) string {
	if len(items) == 0 {
		return fmt.Sprintf("%s = %s", in, out)
	}
	if len(items) == 1 {
		return g.genExpr(items[0], in, out)
	}

	parts := make([]string, 0, len(items))
	currIn := in
	for i := 0; i < len(items)-1; i++ {
		next := g.fresh()
		parts = append(parts, g.genExpr(items[i], currIn, next))
		currIn = next
	}
	parts = append(parts, g.genExpr(items[len(items)-1], currIn, out))
	return strings.Join(parts, ", ")
}

func (g *Generator) genAlt(opts []Expr, in, out string) string {
	parts := make([]string, 0, len(opts))
	for _, opt := range opts {
		parts = append(parts, g.genExpr(opt, in, out))
	}
	return strings.Join(parts, " ; ")
}
