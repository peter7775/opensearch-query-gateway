package dslbuilder

import (
	"strconv"
	"strings"

	"github.com/example/opensearch-query-gateway/internal/parser"
)

// Builder převádí normalizované AST na OpenSearch Query DSL dokument.
type Builder struct{}

func New() *Builder {
	return &Builder{}
}

// Build sestaví kompletní tělo požadavku pro OpenSearch _search endpoint.
func (b *Builder) Build(q *parser.Query) map[string]interface{} {
	return map[string]interface{}{
		"query": b.expression(q.Root),
	}
}

// expression řeší OR (nejnižší precedence). Pokud je jen jeden operand,
// vrací ho beze zabalení do bool/should — zbytečně vnořené bool dotazy
// jen komplikují čtení výsledného DSL a mírně zatěžují query planner.
func (b *Builder) expression(e *parser.Expression) map[string]interface{} {
	first := b.and(e.Left)
	if len(e.Right) == 0 {
		return first
	}

	should := []map[string]interface{}{first}
	for _, op := range e.Right {
		should = append(should, b.and(op.Right))
	}

	return map[string]interface{}{
		"bool": map[string]interface{}{
			"should":               should,
			"minimum_should_match": 1,
		},
	}
}

// and řeší AND a NOT uvnitř jedné úrovně precedence (must/must_not).
func (b *Builder) and(a *parser.AndExpr) map[string]interface{} {
	must := []map[string]interface{}{}
	mustNot := []map[string]interface{}{}

	collect := func(n *parser.NotExpr) {
		q := b.primary(n.Primary)
		if n.Not {
			mustNot = append(mustNot, q)
		} else {
			must = append(must, q)
		}
	}

	collect(a.Left)
	for _, op := range a.Right {
		collect(op.Right)
	}

	// Jediná kladná podmínka bez negace: vrátíme ji přímo, ať nevznikají
	// zbytečně vnořené bool dotazy pro triviální případ (tzn. `tag:golang`
	// nebude obalené do bool.must s jedním prvkem).
	if len(must) == 1 && len(mustNot) == 0 {
		return must[0]
	}

	return map[string]interface{}{
		"bool": map[string]interface{}{
			"must":     must,
			"must_not": mustNot,
		},
	}
}

// primary rozbalí buď vnořenou skupinu v závorkách, nebo listovou klauzuli.
func (b *Builder) primary(p *parser.Primary) map[string]interface{} {
	if p.Group != nil {
		return b.expression(p.Group)
	}
	return b.clause(p.Clause)
}

func (b *Builder) clause(c *parser.Clause) map[string]interface{} {
	boost := boostFactor(c.Value.Boost)

	switch {
	case c.Value.Range != nil:
		return b.rangeClause(c.Field, c.Value.Range, boost)

	case c.Value.Phrase != nil:
		return b.phraseClause(c.Field, *c.Value.Phrase, c.Value.Fuzzy, boost)

	default:
		word := *c.Value.Word
		if containsWildcard(word) {
			// Fuzzy nemá u wildcardu smysl (OpenSearch fuzzy wildcard
			// nekombinuje) — pokud je uveden zároveň, boost se použije,
			// fuzzy modifikátor se u wildcard hodnoty ignoruje.
			return b.wildcardClause(c.Field, word, boost)
		}
		if c.Value.Fuzzy != nil {
			return b.fuzzyClause(c.Field, word, c.Value.Fuzzy.Distance, boost)
		}
		return b.matchClause(c.Field, word, boost)
	}
}

func (b *Builder) rangeClause(field string, r *parser.Range, boost *float64) map[string]interface{} {
	params := map[string]interface{}{
		"gte": r.Min,
		"lte": r.Max,
	}
	if boost != nil {
		params["boost"] = *boost
	}
	return map[string]interface{}{
		"range": map[string]interface{}{
			field: params,
		},
	}
}

func (b *Builder) matchClause(field, word string, boost *float64) map[string]interface{} {
	if boost == nil {
		return map[string]interface{}{
			"match": map[string]interface{}{
				field: word,
			},
		}
	}
	return map[string]interface{}{
		"match": map[string]interface{}{
			field: map[string]interface{}{
				"query": word,
				"boost": *boost,
			},
		},
	}
}

func (b *Builder) phraseClause(field, phrase string, fuzzy *parser.Fuzzy, boost *float64) map[string]interface{} {
	if fuzzy == nil && boost == nil {
		return map[string]interface{}{
			"match_phrase": map[string]interface{}{
				field: phrase,
			},
		}
	}

	params := map[string]interface{}{"query": phrase}
	if fuzzy != nil {
		// U fráze odpovídá ~N slopu (kolik slov navíc/v jiném pořadí mezi
		// tokeny frázi ještě tolerujeme). Bez čísla (bare ~) použijeme
		// rozumný výchozí slop 2.
		params["slop"] = slopValue(fuzzy.Distance, 2)
	}
	if boost != nil {
		params["boost"] = *boost
	}

	return map[string]interface{}{
		"match_phrase": map[string]interface{}{
			field: params,
		},
	}
}

// fuzzyClause sestaví OpenSearch fuzzy query — hledá termy v editační
// vzdálenosti od zadaného slova (překlepy, drobné odchylky).
func (b *Builder) fuzzyClause(field, word, distance string, boost *float64) map[string]interface{} {
	params := map[string]interface{}{
		"value":     word,
		"fuzziness": fuzzinessValue(distance),
	}
	if boost != nil {
		params["boost"] = *boost
	}
	return map[string]interface{}{
		"fuzzy": map[string]interface{}{
			field: params,
		},
	}
}

// wildcardClause sestaví OpenSearch wildcard query. Glob syntaxe (* pro
// libovolný počet znaků, ? pro jeden znak) je stejná jako v našem
// dotazovacím jazyce, takže hodnotu není potřeba překládat.
//
// Pozor: wildcard query spolehlivě funguje jen nad keyword/not-analyzed
// poli. Nad analyzovaným textovým polem se porovnává proti tokenům až po
// analýze (lowercase, tokenizace…), takže prefix/mnohoznak dotaz často
// nevrátí to, co by člověk čekal — ověřte mapping cílového pole.
func (b *Builder) wildcardClause(field, value string, boost *float64) map[string]interface{} {
	params := map[string]interface{}{
		"value":            value,
		"case_insensitive": true,
	}
	if boost != nil {
		params["boost"] = *boost
	}
	return map[string]interface{}{
		"wildcard": map[string]interface{}{
			field: params,
		},
	}
}

func containsWildcard(s string) bool {
	return strings.ContainsAny(s, "*?")
}

// boostFactor rozparsuje ^N modifikátor na float64. Pokud N chybí nebo
// není platné číslo, vrátí boost 1 (neutrální, ale explicitně uvedený).
func boostFactor(b *parser.Boost) *float64 {
	if b == nil {
		return nil
	}
	f, err := strconv.ParseFloat(b.Factor, 64)
	if err != nil {
		f = 1
	}
	return &f
}

// fuzzinessValue vrátí OpenSearch "fuzziness" hodnotu pro term fuzzy query —
// buď konkrétní editační vzdálenost, nebo "AUTO", pokud číslo nebylo uvedeno
// nebo nebylo platné (AUTO nechá OpenSearch zvolit vzdálenost podle délky termu).
func fuzzinessValue(distance string) interface{} {
	if distance == "" {
		return "AUTO"
	}
	if n, err := strconv.Atoi(distance); err == nil {
		return n
	}
	return "AUTO"
}

// slopValue vrátí slop pro match_phrase — pokud číslo nebylo uvedeno nebo
// nebylo platné, použije se zadaná výchozí hodnota.
func slopValue(distance string, def int) int {
	if distance == "" {
		return def
	}
	if n, err := strconv.Atoi(distance); err == nil {
		return n
	}
	return def
}
