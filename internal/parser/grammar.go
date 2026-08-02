package parser

import (
	"github.com/alecthomas/participle/v2"
	"github.com/alecthomas/participle/v2/lexer"
)

var customLexer = lexer.MustSimple([]lexer.SimpleRule{
	{Name: "Whitespace", Pattern: `\s+`},
	{Name: "String", Pattern: `"(\\"|[^"])*"`},
	{Name: "Punct", Pattern: `[:\[\]()~^]`},
	{Name: "Ident", Pattern: `[a-zA-Z0-9_.\-*?]+`},
})

// Parser obaluje zkompilovanou participle gramatiku vlastního dotazovacího jazyka.
type Parser struct {
	inner *participle.Parser[Query]
}

// New sestaví parser. Panic při chybě v gramatice je zde v pořádku, protože
// jde o programátorskou chybu odhalitelnou při startu/testech, ne runtime stav.
func New() *Parser {
	p := participle.MustBuild[Query](
		participle.Lexer(customLexer),
		participle.Elide("Whitespace"),
		participle.Unquote("String"),
	)
	return &Parser{inner: p}
}

// Parse zpracuje vstupní textový dotaz na AST.
func (p *Parser) Parse(input string) (*Query, error) {
	return p.inner.ParseString("", input)
}
