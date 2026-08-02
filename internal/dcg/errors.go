package dcg

import "fmt"

type ParseError struct {
	Pos int
	Msg string
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("parse error at %d: %s", e.Pos, e.Msg)
}
