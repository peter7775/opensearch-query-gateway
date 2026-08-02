package dcg

type Parser struct {
	lx   *Lexer
	cur  Token
	peek Token
}

func NewParser(input string) *Parser {
	lx := NewLexer(input)
	p := &Parser{lx: lx}
	p.cur = lx.NextToken()
	p.peek = lx.NextToken()
	return p
}

func (p *Parser) advance() {
	p.cur = p.peek
	p.peek = p.lx.NextToken()
}

func (p *Parser) ParseRules() ([]Rule, error) {
	var rules []Rule
	for p.cur.Type != TokenEOF {
		r, err := p.parseRule()
		if err != nil {
			return nil, err
		}
		rules = append(rules, r)
	}
	return rules, nil
}

func (p *Parser) parseRule() (Rule, error) {
	head, err := p.parseIdent()
	if err != nil {
		return Rule{}, err
	}
	if p.cur.Type != TokenArrow {
		return Rule{}, &ParseError{Pos: p.cur.Pos, Msg: "expected -->"}
	}
	p.advance()

	body, err := p.parseExpr()
	if err != nil {
		return Rule{}, err
	}

	if p.cur.Type != TokenDot {
		return Rule{}, &ParseError{Pos: p.cur.Pos, Msg: "expected ."}
	}
	p.advance()

	return Rule{Head: head, Body: body}, nil
}

func (p *Parser) parseExpr() (Expr, error) {
	left, err := p.parseSeq()
	if err != nil {
		return nil, err
	}

	if p.cur.Type != TokenBar {
		return left, nil
	}

	opts := []Expr{left}
	for p.cur.Type == TokenBar {
		p.advance()
		right, err := p.parseSeq()
		if err != nil {
			return nil, err
		}
		opts = append(opts, right)
	}
	return Alt{Options: opts}, nil
}

func (p *Parser) parseSeq() (Expr, error) {
	var items []Expr
	for p.cur.Type != TokenEOF &&
		p.cur.Type != TokenDot &&
		p.cur.Type != TokenBar &&
		p.cur.Type != TokenRParen {
		if p.cur.Type == TokenComma {
			p.advance()
			continue
		}
		item, err := p.parseItem()
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if len(items) == 0 {
		return Empty{}, nil
	}
	if len(items) == 1 {
		return items[0], nil
	}
	return Seq{Items: items}, nil
}

func (p *Parser) parseItem() (Expr, error) {
	switch p.cur.Type {
	case TokenIdent:
		v := p.cur.Value
		p.advance()
		return Symbol{Name: v}, nil
	case TokenString:
		v := p.cur.Value
		p.advance()
		return Terminal{Value: v}, nil
	default:
		return nil, &ParseError{Pos: p.cur.Pos, Msg: "expected symbol or string"}
	}
}

func (p *Parser) parseIdent() (string, error) {
	if p.cur.Type != TokenIdent {
		return "", &ParseError{Pos: p.cur.Pos, Msg: "expected identifier"}
	}
	v := p.cur.Value
	p.advance()
	return v, nil
}
