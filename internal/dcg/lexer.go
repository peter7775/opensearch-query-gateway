package dcg

import (
	"strings"
	"unicode"
)

type TokenType int

const (
	TokenEOF TokenType = iota
	TokenIdent
	TokenString
	TokenArrow
	TokenDot
	TokenComma
	TokenBar
	TokenLParen
	TokenRParen
	// TokenIllegal je neznámý znak nebo neukončený řetězec — parser ho
	// hlásí jako chybu místo tichého ukončení vstupu.
	TokenIllegal
)

type Token struct {
	Type  TokenType
	Value string
	Pos   int
}

type Lexer struct {
	input []rune
	pos   int
}

func NewLexer(s string) *Lexer {
	return &Lexer{input: []rune(s)}
}

func (l *Lexer) eof() bool {
	return l.pos >= len(l.input)
}

func (l *Lexer) peek() rune {
	if l.eof() {
		return 0
	}
	return l.input[l.pos]
}

func (l *Lexer) next() rune {
	ch := l.peek()
	if !l.eof() {
		l.pos++
	}
	return ch
}

// skipSpaces přeskočí bílé znaky a řádkové komentáře (% … do konce řádku).
func (l *Lexer) skipSpaces() {
	for !l.eof() {
		switch ch := l.peek(); {
		case unicode.IsSpace(ch):
			l.pos++
		case ch == '%':
			for !l.eof() && l.peek() != '\n' {
				l.pos++
			}
		default:
			return
		}
	}
}

func (l *Lexer) NextToken() Token {
	l.skipSpaces()
	start := l.pos

	if l.eof() {
		return Token{Type: TokenEOF, Pos: l.pos}
	}

	switch ch := l.peek(); ch {
	case '.':
		l.pos++
		return Token{Type: TokenDot, Value: ".", Pos: start}
	case ',':
		l.pos++
		return Token{Type: TokenComma, Value: ",", Pos: start}
	case '|':
		l.pos++
		return Token{Type: TokenBar, Value: "|", Pos: start}
	case '(':
		l.pos++
		return Token{Type: TokenLParen, Value: "(", Pos: start}
	case ')':
		l.pos++
		return Token{Type: TokenRParen, Value: ")", Pos: start}
	case '"':
		l.pos++
		var b strings.Builder
		for !l.eof() && l.peek() != '"' {
			ch := l.next()
			if ch == '\\' && !l.eof() {
				ch = l.next()
			}
			b.WriteRune(ch)
		}
		if l.eof() {
			return Token{Type: TokenIllegal, Value: "unterminated string", Pos: start}
		}
		l.pos++
		return Token{Type: TokenString, Value: b.String(), Pos: start}
	case '-':
		if l.pos+2 < len(l.input) && l.input[l.pos+1] == '-' && l.input[l.pos+2] == '>' {
			l.pos += 3
			return Token{Type: TokenArrow, Value: "-->", Pos: start}
		}
	}

	if unicode.IsLetter(l.peek()) || l.peek() == '_' {
		var b strings.Builder
		for !l.eof() {
			ch := l.peek()
			if unicode.IsLetter(ch) || unicode.IsDigit(ch) || ch == '_' {
				b.WriteRune(ch)
				l.pos++
			} else {
				break
			}
		}
		return Token{Type: TokenIdent, Value: b.String(), Pos: start}
	}

	ch := l.next()
	return Token{Type: TokenIllegal, Value: string(ch), Pos: start}
}
