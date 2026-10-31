//go:build !rbgo_closed

package vm

import (
	"fmt"

	"github.com/go-ruby-parser/parser"
)

// parseFault parses src and reports a panic of the parser — an
// index out of range on a malformed string, say — as an error. A syntax error
// is not a fault: the evaluation reports it.
func parseFault(src string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%v", r)
		}
	}()
	_, _ = parser.Parse(src)
	return nil
}
