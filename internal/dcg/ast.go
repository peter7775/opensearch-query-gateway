package dcg

type Rule struct {
	Head string
	Body Expr
}

type Expr interface {
	exprNode()
}

type Seq struct {
	Items []Expr
}

func (Seq) exprNode() {}

type Alt struct {
	Options []Expr
}

func (Alt) exprNode() {}

type Symbol struct {
	Name string
}

func (Symbol) exprNode() {}

type Terminal struct {
	Value string
}

func (Terminal) exprNode() {}

type Empty struct{}

func (Empty) exprNode() {}
