// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"math/big"
	"strconv"
	"strings"

	format "github.com/go-ruby-format/format"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin binding between rbgo's Ruby object graph (object.Value)
// and the interpreter-independent format-string engine of
// github.com/go-ruby-format/format. The conversion machinery itself
// (parseConversion / per-verb rendering / arbitrary-precision integers / the
// MRI ArgumentError/KeyError/TypeError messages) lives in that library, ported
// byte-for-byte from rbgo's former internal formatter; rbgo only wraps its
// values in the library's small Value interface around a single format.Format
// call, so Kernel#sprintf / Kernel#format / String#% behave identically to
// before (the behaviour Puppet's output relies on) by construction.

// formatValue adapts an rbgo object.Value to format.Value so the library can
// format it without an intermediate copy. ClassName mirrors classNameOf so the
// library's TypeError messages ("no implicit conversion of X into Integer")
// match MRI exactly.
type formatValue struct {
	v  object.Value
	vm *VM // non-nil: enables MRI argument coercion (to_s/inspect/to_int/to_i/to_f)
}

// Kind reports which family of conversions the value natively satisfies,
// mapping the rbgo dynamic type to the library's Kind enumeration.
func (fv formatValue) Kind() format.Kind {
	switch fv.v.(type) {
	case object.Integer, *object.Bignum:
		return format.KindInteger
	case object.Float:
		return format.KindFloat
	case *object.String:
		return format.KindString
	case object.Symbol:
		return format.KindSymbol
	case *object.Array:
		return format.KindArray
	case *object.Hash:
		return format.KindHash
	case object.Nil:
		return format.KindNil
	default:
		return format.KindOther
	}
}

// Int64Fast reports a genuine int64-range Ruby Integer without allocating a
// *big.Int, letting the library's integer conversions (%d/%x/%o/%b/…) skip
// math/big for the common small-integer case. A Bignum reports its int64 value
// only when it fits; every other value (Float, String, etc.) reports ok=false so
// the formatter uses the precise Int() path with its coercion and error rules.
func (fv formatValue) Int64Fast() (int64, bool) {
	switch x := fv.v.(type) {
	case object.Integer:
		return int64(x), true
	case *object.Bignum:
		if x.I.IsInt64() {
			return x.I.Int64(), true
		}
		return 0, false
	default:
		return 0, false
	}
}

// ToS is the Ruby to_s rendering (%s and the textual value of %{name}). For a
// user object it dispatches #to_s (MRI coerces %s / %{name} arguments with #to_s,
// never #to_str), so e.g. `"%s" % mock_returning("abc")` yields "abc".
func (fv formatValue) ToS() string {
	if o, ok := fv.v.(*RObject); ok && fv.vm != nil {
		return fv.vm.formatDispatchStr(o, "to_s")
	}
	return fv.v.ToS()
}

// Inspect is the Ruby inspect rendering (%p); for a user object it dispatches
// #inspect.
func (fv formatValue) Inspect() string {
	if o, ok := fv.v.(*RObject); ok && fv.vm != nil {
		return fv.vm.formatDispatchStr(o, "inspect")
	}
	return fv.v.Inspect()
}

// formatDispatchStr sends name to o and returns the resulting String's bytes (or
// the value's default rendering if it is not a String), for %s/%p/%{name}.
func (vm *VM) formatDispatchStr(o *RObject, name string) string {
	r := vm.send(o, name, nil, nil)
	if s, ok := r.(*object.String); ok {
		return s.Str()
	}
	return r.ToS()
}

// ClassName names the value's Ruby class for TypeError messages, mirroring
// classNameOf so the library's messages are byte-identical to the former
// formatter's.
func (fv formatValue) ClassName() string { return classNameOf(fv.v) }

// Int returns the value as an arbitrary-precision integer for the integer
// conversions, honouring the library's (z, err, ok) contract: ok=false marks a
// value that is not an integer at all (the library raises TypeError); a non-nil
// err marks a String that is not a valid Integer() literal (ArgumentError). A
// Float truncates toward zero, matching MRI's "%d" % 3.9.
func (fv formatValue) Int() (*big.Int, error, bool) {
	switch x := fv.v.(type) {
	case object.Integer:
		return big.NewInt(int64(x)), nil, true
	case *object.Bignum:
		return new(big.Int).Set(x.I), nil, true
	case object.Float:
		z, _ := big.NewFloat(float64(x)).Int(nil)
		return z, nil, true
	case *object.String:
		z, err := parseFormatInteger(x.Str(), x.Inspect())
		return z, err, true
	case *RObject:
		// A formatValue reaching the integer conversions always carries the VM that
		// built it (formatString / formatNamedArgs set it), so coerce via the object.
		return fv.vm.formatObjInt(x)
	default:
		return nil, nil, false
	}
}

// formatObjInt coerces a user object to an integer for the integer conversions:
// #to_int first, then #to_i (MRI's rule for %d/%i/%u/%o/%b/%x/%X). A coercion
// method returning a non-Integer raises TypeError; an object answering neither
// reports ok=false so the library raises its own "no implicit conversion".
func (vm *VM) formatObjInt(o *RObject) (*big.Int, error, bool) {
	for _, m := range []string{"to_int", "to_i"} {
		if !vm.respondsToDynamic(o, m) {
			continue
		}
		r := vm.send(o, m, nil, nil)
		if z, ok := object.BigOf(r); ok {
			return z, nil, true
		}
		raise("TypeError", "can't convert %s to Integer (%s#%s gives %s)",
			vm.classOf(o).name, vm.classOf(o).name, m, vm.classOf(r).name)
	}
	return nil, nil, false
}

// Float returns the value as a float64 for the float conversions, with the same
// ok/err contract as Int. A String is parsed with Float() semantics; a
// non-numeric String reports a non-nil error so the library raises
// ArgumentError.
func (fv formatValue) Float() (float64, error, bool) {
	switch x := fv.v.(type) {
	case object.Integer:
		return float64(x), nil, true
	case *object.Bignum:
		f, _ := new(big.Float).SetInt(x.I).Float64()
		return f, nil, true
	case object.Float:
		return float64(x), nil, true
	case *object.String:
		f, err := parseFormatFloat(x.Str(), x.Inspect())
		return f, err, true
	case *RObject:
		return fv.vm.formatObjFloat(x)
	default:
		return 0, nil, false
	}
}

// formatObjFloat coerces a user object to a float for the float conversions via
// #to_f (MRI's rule for %f/%e/%E/%g/%G). A non-numeric #to_f result raises
// TypeError; an object without #to_f reports ok=false.
func (vm *VM) formatObjFloat(o *RObject) (float64, error, bool) {
	if !vm.respondsToDynamic(o, "to_f") {
		return 0, nil, false
	}
	r := vm.send(o, "to_f", nil, nil)
	switch n := r.(type) {
	case object.Float:
		return float64(n), nil, true
	case object.Integer:
		return float64(n), nil, true
	}
	raise("TypeError", "can't convert %s into Float", vm.classOf(o).name)
	return 0, nil, false
}

// parseFormatInteger parses a String operand for an integer conversion as MRI's
// Integer() does for sprintf: surrounding whitespace is trimmed, underscores
// between digits are dropped, and 0x/0o/0b/0 radix prefixes are honoured. A
// malformed value yields a non-nil error whose message matches MRI's
// `invalid value for Integer(): <inspect>` (the library promotes it to an
// ArgumentError).
func parseFormatInteger(s, inspect string) (*big.Int, error) {
	clean := strings.ReplaceAll(strings.TrimSpace(s), "_", "")
	z, ok := new(big.Int).SetString(clean, 0)
	if !ok {
		return nil, &format.Error{
			Class:   "ArgumentError",
			Message: "invalid value for Integer(): " + inspect,
		}
	}
	return z, nil
}

// parseFormatFloat parses a String operand for a float conversion as MRI's
// Float() does for sprintf: surrounding whitespace is trimmed. A malformed
// value yields a non-nil error whose message matches MRI's
// `invalid value for Float(): <inspect>`.
func parseFormatFloat(s, inspect string) (float64, error) {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0, &format.Error{
			Class:   "ArgumentError",
			Message: "invalid value for Float(): " + inspect,
		}
	}
	return f, nil
}

// formatNamedArgs builds the *format.NamedArgs backing %<name>/%{name} from the
// formatter's argument list: the sole operand when it is a Hash, keyed by each
// Symbol key's name (the shape Kernel#sprintf("%<n>d", n: 1) and String#% with a
// trailing hash both produce). A non-Symbol key is skipped, since only symbol
// references are addressable by %<name>. When there is no hash operand, nil is
// returned and the library raises the MRI "one hash required" ArgumentError on
// the first named reference.
func (vm *VM) formatNamedArgs(args []object.Value) *format.NamedArgs {
	if len(args) != 1 {
		return nil
	}
	h, ok := args[0].(*object.Hash)
	if !ok {
		return nil
	}
	m := make(map[string]format.Value, h.Len())
	for _, k := range h.Keys {
		sym, isSym := k.(object.Symbol)
		if !isSym {
			continue
		}
		v, _ := h.Get(k)
		m[string(sym)] = formatValue{v: v, vm: vm}
	}
	return format.NewNamedArgs(m)
}

// formatString renders a Ruby format string with the given positional operands,
// delegating to the go-ruby-format engine and re-raising its *format.Error as
// the matching Ruby exception (ArgumentError / KeyError / TypeError). It is the
// single entry point Kernel#sprintf / Kernel#format / IO#printf / String#% all
// funnel through, so their formatting behaviour is identical.
func (vm *VM) formatString(fmtStr string, args []object.Value) string {
	// One backing array of wrappers, referenced by pointer, so wrapping N
	// operands costs a single allocation instead of one interface box per arg.
	wraps := make([]formatValue, len(args))
	vals := make([]format.Value, len(args))
	for i, a := range args {
		wraps[i].v = a
		wraps[i].vm = vm
		vals[i] = &wraps[i]
	}
	out, err := format.Format(fmtStr, vals, vm.formatNamedArgs(args))
	if err != nil {
		raiseFormatError(err)
	}
	return out
}

// raiseFormatError re-raises a go-ruby-format error as the matching Ruby
// exception: a *format.Error carries the MRI exception class and message
// (ArgumentError / KeyError / TypeError) verbatim; any other error (which the
// library never produces, but is handled defensively) surfaces as an
// ArgumentError. It never returns.
func raiseFormatError(err error) {
	if fe, ok := err.(*format.Error); ok {
		raise(fe.Class, "%s", fe.Message)
	}
	raise("ArgumentError", "%s", err.Error())
}

// formatArgs unpacks the right-hand operand of String#%: an Array spreads into
// the argument list; any other value is the single argument. A sole Hash thus
// stays a one-element argument list, which formatString both treats as a
// positional operand and exposes as the %<name>/%{name} hash (MRI's behaviour
// where "%<a>d %s" % {a: 1} formats the hash for both forms).
func formatArgs(b object.Value) []object.Value {
	if arr, ok := b.(*object.Array); ok {
		return arr.Elems
	}
	return []object.Value{b}
}
