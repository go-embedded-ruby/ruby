package vm

import (
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// classNameOf names a value's class for the TypeError messages various builtins
// (and the format binding's formatValue.ClassName) raise without a VM handle.
func classNameOf(v object.Value) string {
	{
		__sw57 := v
		switch {
		case object.IsInt(__sw57):
			return "Integer"
		case object.IsFloat(__sw57):
			return "Float"
		case object.IsKind[*object.String](__sw57):
			return "String"
		case object.IsKind[object.Symbol](__sw57):
			return "Symbol"
		case object.IsKind[*object.Array](__sw57):
			return "Array"
		case object.IsKind[*object.Hash](__sw57):
			return "Hash"
		case object.IsKind[*Regexp](__sw57):
			return "Regexp"
		case object.IsKind[*MatchData](__sw57):
			return "MatchData"
		case object.IsNilObj(__sw57):
			return "nil"
		default:
			return "Object"
		}
	}
}
