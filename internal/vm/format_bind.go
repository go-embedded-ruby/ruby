// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"
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

// CoerceChar resolves a user object's %c operand with MRI's to_str-then-to_int
// protocol (sprintf.c's 'c' case): a String operand is used natively by the
// engine, so this hook only fires for a non-String, non-Integer value. It
// dispatches #to_str first — a String result feeds the engine's first-character
// rendering, a non-String result raises TypeError "can't convert X into String";
// otherwise #to_int — an Integer result feeds the code-point rendering, a
// non-Integer result raises TypeError "can't convert X into Integer". An object
// answering neither raises TypeError "no implicit conversion of X into Integer",
// exactly as MRI's NUM2INT does. Method presence is probed through the method
// table (not #respond_to?), so a BasicObject that defines only #to_str is
// coerced without touching a method it does not have. A non-RObject (String,
// Integer, nil, Array) returns ok=false so the engine keeps its Kind-based path
// and its own "no implicit conversion" / "invalid character" errors.
func (fv formatValue) CoerceChar() (format.Value, bool) {
	o, ok := fv.v.(*RObject)
	if !ok || fv.vm == nil {
		return nil, false
	}
	vm := fv.vm
	if vm.respondsTo(o, "to_str") {
		r := vm.send(o, "to_str", nil, nil)
		if s, isStr := r.(*object.String); isStr {
			return formatValue{v: s, vm: vm}, true
		}
		raise("TypeError", "can't convert %s into String", vm.classOf(o).name)
	}
	if vm.respondsTo(o, "to_int") {
		r := vm.send(o, "to_int", nil, nil)
		if _, isInt := object.BigOf(r); isInt {
			return formatValue{v: r, vm: vm}, true
		}
		raise("TypeError", "can't convert %s into Integer", vm.classOf(o).name)
	}
	raise("TypeError", "no implicit conversion of %s into Integer", vm.classOf(o).name)
	return nil, false
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
	// A String operand for %d/%i/%u/%o/%b/%x/%X is parsed exactly as Kernel#Integer
	// would (base 0: prefix-detected radix, and MRI's strict digit-separator rule
	// where a single '_' is legal only between two digits). Route through the same
	// intFromString primitive rather than a blind underscore strip, so
	// "0777"->511, "0b1101_0000"->208, but "123__456"/"_1"/"08" raise ArgumentError.
	v, ok := intFromString(s, 0)
	if !ok {
		// Return only the message: the format engine promotes a non-nil parse
		// error to an ArgumentError by wrapping err.Error(), so a *format.Error
		// here (whose Error() is "Class: Message") would double the class prefix.
		return nil, errors.New("invalid value for Integer(): " + inspect)
	}
	z, _ := object.BigOf(v)
	return z, nil
}

// parseFormatFloat parses a String operand for a float conversion exactly as
// MRI's Kernel#Float does (the "%e/%f/%g behaves as if calling Kernel#Float"
// rule): it honours a "0x…" hexadecimal-float literal ("0xA" -> 10.0), applies
// MRI's strict digit-separator underscore placement, and treats an out-of-range
// magnitude as ±Inf / 0.0 (overflow/underflow) rather than an error — routing
// through the same normalizeFloatLiteral primitive as Kernel#Float so the two
// stay byte-for-byte identical. A genuinely malformed value yields a non-nil
// error whose message matches MRI's `invalid value for Float(): <inspect>` (the
// library promotes it to an ArgumentError).
func parseFormatFloat(s, inspect string) (float64, error) {
	// Return only the message: the format engine promotes a non-nil parse error
	// to an ArgumentError by wrapping err.Error(), so a *format.Error here (whose
	// Error() is "Class: Message") would double the class prefix.
	badFloat := func() error {
		return errors.New("invalid value for Float(): " + inspect)
	}
	norm, ok := normalizeFloatLiteral(s)
	if !ok {
		return 0, badFloat()
	}
	f, err := strconv.ParseFloat(norm, 64)
	if err != nil {
		// An out-of-range literal is not malformed: MRI yields ±Infinity (overflow)
		// or 0.0 (underflow), which is exactly what ParseFloat returns with ErrRange.
		if ne, ok := err.(*strconv.NumError); ok && ne.Err == strconv.ErrRange {
			return f, nil
		}
		return 0, badFloat()
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
	na := format.NewNamedArgs(m)
	// A %<name>/%{name} reference to a key that is not a present symbol entry of
	// the hash resolves through the hash's own #[] (Hash#default / default_proc),
	// exactly as MRI does: a non-nil default is used, a nil one leaves the key
	// unresolved so the engine raises the MRI KeyError. A present key (even one
	// whose value is nil) is already in m above and never reaches this resolver,
	// so `"%{foo}" % {foo: nil}` still renders "" rather than consulting a default.
	na.SetDefault(func(name string) (format.Value, bool) {
		r := vm.send(h, "[]", []object.Value{object.Symbol(name)}, nil)
		if object.IsNil(r) {
			return nil, false
		}
		return formatValue{v: r, vm: vm}, true
	})
	return na
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
		vm.raiseFormatError(err, args)
	}
	return out
}

// raiseFormatError re-raises a go-ruby-format error as the matching Ruby
// exception: a *format.Error carries the MRI exception class and message
// (ArgumentError / KeyError / TypeError) verbatim; any other error (which the
// library never produces, but is handled defensively) surfaces as an
// ArgumentError. Every KeyError the engine raises comes from an unmatched
// %<name>/%{name} reference, which is possible only when the sole operand is a
// hash; that KeyError is re-raised with MRI's #key (the missing key as a Symbol)
// and #receiver (that hash) set, so `rescue KeyError => e; e.key; e.receiver`
// behave as MRI does. args is the positional operand list. It never returns.
func (vm *VM) raiseFormatError(err error, args []object.Value) {
	fe, ok := err.(*format.Error)
	if !ok {
		raise("ArgumentError", "%s", err.Error())
	}
	if fe.Class == "KeyError" {
		var receiver object.Value
		if len(args) == 1 {
			if h, isHash := args[0].(*object.Hash); isHash {
				receiver = h
			}
		}
		vm.raiseWithIvars("KeyError", fe.Message, map[string]object.Value{
			"@key":      object.Symbol(namedKeyFromMessage(fe.Message)),
			"@receiver": receiver,
		})
	}
	raise(fe.Class, "%s", fe.Message)
}

// namedKeyFromMessage extracts the referenced key from the format engine's
// unmatched-reference KeyError message, which is `key{NAME} not found` for a
// %{NAME} reference and `key<NAME> not found` for a %<NAME> one; either bracket
// style yields the same MRI #key. The NAME token never itself contains a
// reference bracket (such a byte would terminate the reference), so trimming the
// fixed prefix/suffix and the surrounding brackets recovers it exactly.
func namedKeyFromMessage(msg string) string {
	inner := strings.TrimPrefix(strings.TrimSuffix(msg, " not found"), "key")
	return strings.TrimFunc(inner, func(r rune) bool {
		return r == '{' || r == '}' || r == '<' || r == '>'
	})
}

// formatArgs unpacks the right-hand operand of String#%: an Array spreads into
// the argument list; any other value is the single argument. A sole Hash thus
// stays a one-element argument list, which formatString both treats as a
// positional operand and exposes as the %<name>/%{name} hash (MRI's behaviour
// where "%<a>d %s" % {a: 1} formats the hash for both forms).
// vmFormatArgs turns the right-hand side of String#% into the positional
// argument list. An Array is spread; a Hash is kept whole (for %{name}/%<name>
// references); any other object is converted with #to_ary when it responds —
// an Array result is spread, a nil result falls back to a single argument, and a
// non-Array result raises TypeError, as MRI does; otherwise it is a single
// argument.
func (vm *VM) vmFormatArgs(b object.Value) []object.Value {
	switch b.(type) {
	case *object.Array:
		return b.(*object.Array).Elems
	case *object.Hash:
		return []object.Value{b}
	}
	if vm.respondsToDynamic(b, "to_ary") {
		if r := vm.send(b, "to_ary", nil, nil); !object.IsNil(r) {
			if arr, ok := r.(*object.Array); ok {
				return arr.Elems
			}
			raise("TypeError", "can't convert %s to Array (%s#to_ary gives %s)",
				vm.classOf(b).name, vm.classOf(b).name, vm.classOf(r).name)
		}
	}
	return []object.Value{b}
}
