package parser

import (
	"errors"
	"strings"

	"github.com/alecthomas/participle/v2"
	"github.com/alecthomas/participle/v2/lexer"
)

var customLexer = lexer.MustSimple([]lexer.SimpleRule{
	{Name: "Whitespace", Pattern: `\s+`},
	{Name: "String", Pattern: `"(\\"|[^"])*"`},
	{Name: "Punct", Pattern: `>=|<=|[:\[\]()~^<>]`},
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
		// Klíčová slova AND/OR/NOT/TO fungují i malými písmeny
		// (service:gateway and severity:error).
		participle.CaseInsensitive("Ident"),
	)
	return &Parser{inner: p}
}

// Parse zpracuje vstupní textový dotaz na AST.
func (p *Parser) Parse(input string) (*Query, error) {
	if strings.TrimSpace(input) == "" {
		return nil, ErrEmptyQuery
	}
	return p.inner.ParseString("", input)
}

// ErrEmptyQuery vrací Parse pro prázdný (nebo jen bílé znaky) vstup.
var ErrEmptyQuery = errors.New("empty query")
