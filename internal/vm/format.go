package vm

import (
	"fmt"
	"strings"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// formatString implements Ruby's String#% / Kernel#sprintf for the common
// conversion set: %d %i %s %f %e %E %g %G %x %X %o %b %c and %% (literal),
// with the -, +, space, 0 and # flags plus width and precision. Each conversion
// is delegated to Go's fmt with an argument coerced to the verb's expected type,
// so number/string formatting matches MRI for these verbs.
func formatString(format string, args []object.Value) string {
	var b strings.Builder
	argi := 0
	next := func() object.Value {
		if argi >= len(args) {
			raise("ArgumentError", "too few arguments")
		}
		v := args[argi]
		argi++
		return v
	}
	for i := 0; i < len(format); {
		if format[i] != '%' {
			b.WriteByte(format[i])
			i++
			continue
		}
		// Parse %[flags][width][.precision]verb.
		j := i + 1
		for j < len(format) && strings.IndexByte("-+ 0#", format[j]) >= 0 {
			j++
		}
		for j < len(format) && format[j] >= '0' && format[j] <= '9' {
			j++
		}
		if j < len(format) && format[j] == '.' {
			j++
			for j < len(format) && format[j] >= '0' && format[j] <= '9' {
				j++
			}
		}
		if j >= len(format) {
			raise("ArgumentError", "malformed format string - %%*[0-9]")
		}
		verb := format[j]
		spec := format[i : j+1]
		i = j + 1
		if verb == '%' {
			b.WriteByte('%')
			continue
		}
		b.WriteString(formatOne(spec, verb, next))
	}
	return b.String()
}

// formatOne renders a single conversion, coercing the next argument to the type
// its verb expects before handing it to Go's fmt.
func formatOne(spec string, verb byte, next func() object.Value) string {
	switch verb {
	case 'd', 'i', 'o', 'x', 'X', 'b', 'B':
		goSpec := spec
		if verb == 'i' { // Go has no %i; it means decimal integer
			goSpec = spec[:len(spec)-1] + "d"
		}
		return fmt.Sprintf(goSpec, toFormatInt(next()))
	case 'f', 'e', 'E', 'g', 'G':
		return fmt.Sprintf(spec, toFormatFloat(next()))
	case 's':
		return fmt.Sprintf(spec, next().ToS())
	case 'c':
		// %c renders the character as a string so an empty String yields "" (as
		// in MRI) and width/justification still apply via the %s machinery.
		return fmt.Sprintf(spec[:len(spec)-1]+"s", toFormatChar(next()))
	default:
		raise("ArgumentError", "malformed format string - %%%c", verb)
		return ""
	}
}

// toFormatInt coerces an argument for an integer conversion (floats truncate
// toward zero, as in Ruby).
func toFormatInt(v object.Value) int64 {
	switch x := v.(type) {
	case object.Integer:
		return int64(x)
	case object.Float:
		return int64(x)
	default:
		raise("TypeError", "no implicit conversion of %s into Integer", classNameOf(v))
		return 0
	}
}

// toFormatFloat coerces an argument for a floating conversion.
func toFormatFloat(v object.Value) float64 {
	switch x := v.(type) {
	case object.Integer:
		return float64(x)
	case object.Float:
		return float64(x)
	default:
		raise("TypeError", "no implicit conversion of %s into Float", classNameOf(v))
		return 0
	}
}

// toFormatChar coerces an argument for %c into the character as a string: an
// Integer is a code point, a String contributes its first character (or "" when
// empty, matching MRI).
func toFormatChar(v object.Value) string {
	switch x := v.(type) {
	case object.Integer:
		return string(rune(x))
	case object.String:
		for _, r := range string(x) {
			return string(r)
		}
		return ""
	default:
		raise("TypeError", "no implicit conversion of %s into Integer", classNameOf(v))
		return ""
	}
}

// classNameOf names a value's class for error messages without a VM handle.
func classNameOf(v object.Value) string {
	switch v.(type) {
	case object.Integer:
		return "Integer"
	case object.Float:
		return "Float"
	case object.String:
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

// formatArgs unpacks the right-hand operand of String#%: an Array spreads into
// the argument list, any other value is the single argument.
func formatArgs(b object.Value) []object.Value {
	if arr, ok := b.(*object.Array); ok {
		return arr.Elems
	}
	return []object.Value{b}
}
