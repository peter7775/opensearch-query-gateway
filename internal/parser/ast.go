package parser

// Query je kořenový uzel naparsovaného dotazu ve vlastním dotazovacím jazyce.
type Query struct {
	Root *Expression `parser:"@@"`
}

// Expression řeší OR s nejnižší precedencí: a OR b OR c.
// Pokud je jen jeden operand (žádné explicitní OR), vrací ho beze změny —
// viz dslbuilder, který díky tomu nesestavuje zbytečně vnořené bool dotazy.
type Expression struct {
	Left  *AndExpr `parser:"@@"`
	Right []*OrOp  `parser:"@@*"`
}

type OrOp struct {
	Op    string   `parser:"\"OR\""`
	Right *AndExpr `parser:"@@"`
}

// AndExpr řeší AND s vyšší precedencí než OR. Operátor AND je nepovinný —
// klauzule oddělené jen mezerou (bez explicitního AND) se chovají jako
// implicitní AND, podobně jako ve většině vyhledávacích syntaxí.
type AndExpr struct {
	Left  *NotExpr `parser:"@@"`
	Right []*AndOp `parser:"@@*"`
}

type AndOp struct {
	Op    string   `parser:"@\"AND\"?"`
	Right *NotExpr `parser:"@@"`
}

// NotExpr má nejvyšší precedenci — NOT se váže jen na bezprostředně
// následující primární výraz (jednu klauzuli nebo celou skupinu v závorkách).
type NotExpr struct {
	Not     bool     `parser:"@\"NOT\"?"`
	Primary *Primary `parser:"@@"`
}

// Primary je buď klauzule field:value, nebo celý výraz uzavřený v závorkách.
// Závorky umožňují libovolné vnořování a explicitní řízení precedence, např.:
//
//	(tag:golang OR tag:go) AND NOT status:archived
type Primary struct {
	Group  *Expression `parser:"  \"(\" @@ \")\""`
	Clause *Clause     `parser:"| @@"`
}

// Clause reprezentuje jednu podmínku field:value.
type Clause struct {
	Field string `parser:"@Ident \":\""`
	Value *Value `parser:"@@"`
}

// Value je hodnota podmínky — rozsah, fráze v uvozovkách, nebo slovo.
// Slovo může obsahovat wildcard znaky * (libovolný počet znaků) a
// ? (jeden znak) — jejich překlad na OpenSearch wildcard query řeší
// dslbuilder, gramatika je jen propouští jako běžné znaky identifikátoru.
//
// Za hodnotou mohou nepovinně následovat modifikátory ve stylu Lucene:
//   - ~N   fuzzy shoda u slova (fuzziness, edit distance) nebo proximity
//     u fráze (slop) — bez čísla se použije rozumný výchozí (AUTO/2).
//   - ^N   boost (relevance weight), lze kombinovat s ~N, např. slovo~2^1.5.
type Value struct {
	Range  *Range  `parser:"(  @@"`
	Phrase *string `parser:"  | @String"`
	Word   *string `parser:"  | @Ident )"`
	Fuzzy  *Fuzzy  `parser:"( \"~\" @@ )?"`
	Boost  *Boost  `parser:"( \"^\" @@ )?"`
}

// Fuzzy zachycuje nepovinný modifikátor ~N. Distance je prázdný string,
// pokud byl uveden bez čísla (bare ~) — dslbuilder v tom případě dosadí
// rozumnou výchozí hodnotu podle typu hodnoty (slovo vs. fráze).
type Fuzzy struct {
	Distance string `parser:"@Ident?"`
}

// Boost zachycuje modifikátor ^N. Factor je vždy vyžadovaný — bare ^ bez
// čísla nemá smysl.
type Boost struct {
	Factor string `parser:"@Ident"`
}

// Range reprezentuje field:[min TO max] syntaxi.
type Range struct {
	Min string `parser:"\"[\" @Ident"`
	Max string `parser:"\"TO\" @Ident \"]\""`
}

// WalkClauses projde celý AST bez ohledu na vnořené závorky a booleovské
// operátory a zavolá fn na každou listovou klauzuli. Clause je pointer,
// takže fn ji může upravovat in-place (např. normalizace názvu pole přes
// rule engine) a změna se promítne i do stromu, ze kterého potom čte dslbuilder.
func (q *Query) WalkClauses(fn func(*Clause)) {
	walkExpression(q.Root, fn)
}

func walkExpression(e *Expression, fn func(*Clause)) {
	walkAnd(e.Left, fn)
	for _, op := range e.Right {
		walkAnd(op.Right, fn)
	}
}

func walkAnd(a *AndExpr, fn func(*Clause)) {
	walkNot(a.Left, fn)
	for _, op := range a.Right {
		walkNot(op.Right, fn)
	}
}

func walkNot(n *NotExpr, fn func(*Clause)) {
	walkPrimary(n.Primary, fn)
}

func walkPrimary(p *Primary, fn func(*Clause)) {
	if p.Group != nil {
		walkExpression(p.Group, fn)
		return
	}
	fn(p.Clause)
}

// Op vrátí typ operace klauzule pro rule engine ("range" nebo "eq").
func (c *Clause) Op() string {
	if c.Value.Range != nil {
		return "range"
	}
	return "eq"
}

// RawValue vrátí textovou reprezentaci hodnoty klauzule včetně modifikátorů
// (~fuzzy, ^boost) — použitelné pro logování a validaci v rule enginu.
func (c *Clause) RawValue() string {
	var base string
	switch {
	case c.Value.Range != nil:
		base = c.Value.Range.Min + " TO " + c.Value.Range.Max
	case c.Value.Phrase != nil:
		base = *c.Value.Phrase
	case c.Value.Word != nil:
		base = *c.Value.Word
	}
	if c.Value.Fuzzy != nil {
		base += "~" + c.Value.Fuzzy.Distance
	}
	if c.Value.Boost != nil {
		base += "^" + c.Value.Boost.Factor
	}
	return base
}
