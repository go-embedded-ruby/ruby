package vm

import (
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// classNameOf names a value's class for the TypeError messages various builtins
// (and the format binding's formatValue.ClassName) raise without a VM handle.
func classNameOf(v object.Value) string {
	switch v.(type) {
	case object.Integer:
		return "Integer"
	case object.Float:
		return "Float"
	case *object.String:
		return "String"
	case object.Symbol:
		return "Symbol"
	case *object.Array:
		return "Array"
	case *object.Hash:
		return "Hash"
	case *Regexp:
		return "Regexp"
	case *MatchData:
		return "MatchData"
	case object.Nil:
		return "nil"
	default:
		return "Object"
	}
}
