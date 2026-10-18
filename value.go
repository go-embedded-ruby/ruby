package ruby

import (
	"fmt"
	"math"
	"math/big"
	"reflect"

	nd "github.com/go-ndarray/ndarray"

	"github.com/go-embedded-ruby/ruby/internal/object"
	"github.com/go-embedded-ruby/ruby/internal/vm"
)

// Value is a Ruby object held by Go. It stays valid as long as its Interp.
// The zero Value is Ruby's nil.
//
// # Conversions
//
// Go to Ruby (Set, the arguments of Block.Call and Value.Call, a Func's result):
//
//	nil                         nil
//	bool                        true / false
//	int, int8 … int64, uint …   Integer (a uint64 above 2**63-1 is a Bignum)
//	*big.Int                    Integer (a copy; Fixnum when it fits)
//	float32, float64            Float
//	complex64, complex128       Complex
//	*big.Rat                    Rational (a copy)
//	string, []byte              String
//	Symbol                      Symbol
//	*ndarray.Array              NDArray — the SAME array, not a copy
//	Value                       itself
//	other slices and arrays     Array, element by element
//	maps                        Hash, entry by entry
//	anything else               an opaque object: an instance of the class Bind
//	                            registered for its type, else of GoValue
//
// Ruby to Go (Get, Value.Go):
//
//	nil                         nil
//	true / false                bool
//	Integer                     int64, or *big.Int when it does not fit
//	Float                       float64
//	Complex                     complex128 (Value when a part is not real)
//	Rational                    *big.Rat
//	String                      string
//	Symbol                      Symbol
//	Array                       []any, element by element
//	Hash                        map[any]any, entry by entry (a key Go cannot
//	                            hash, an Array say, stays a Value)
//	NDArray                     *ndarray.Array — the same array
//	an opaque object            the Go value that was wrapped
//	anything else               Value
type Value struct {
	in *Interp
	v  object.Value
}

// Symbol is a Ruby Symbol on the Go side: Set(name, Symbol("a")) assigns :a.
type Symbol string

func (in *Interp) wrap(v object.Value) Value {
	if v == nil {
		v = object.NilV
	}
	return Value{in: in, v: v}
}

func (v Value) obj() object.Value {
	if v.v == nil {
		return object.NilV
	}
	return v.v
}

// IsNil reports whether v is nil.
func (v Value) IsNil() bool { return object.IsNil(v.obj()) }

// ClassName is the name of v's class.
func (v Value) ClassName() string {
	if v.in == nil {
		return "NilClass"
	}
	return v.in.vm.ClassOf(v.obj()).Name()
}

// Inspect is v.inspect, as p prints it.
func (v Value) Inspect() string {
	if v.in == nil {
		return "nil"
	}
	var s string
	_, err := v.in.vm.Protect(func() object.Value {
		s = v.in.vm.InspectValue(v.obj())
		return object.NilV
	})
	if err != nil {
		return v.obj().Inspect()
	}
	return s
}

// String is v.to_s, as puts prints it.
func (v Value) String() string {
	if v.in == nil {
		return ""
	}
	var s string
	_, err := v.in.vm.Protect(func() object.Value {
		s = v.in.vm.ToSValue(v.obj())
		return object.NilV
	})
	if err != nil {
		return v.obj().ToS()
	}
	return s
}

// Go converts v to a Go value; see Value for the table.
func (v Value) Go() any { return v.in.fromRuby(v.obj(), map[object.Value]any{}) }

// Call calls the method name on v with args converted to Ruby, and returns its
// result. A Ruby exception comes back as an *Error.
func (v Value) Call(name string, args ...any) (Value, error) {
	in := v.in
	if in == nil {
		return Value{}, fmt.Errorf("ruby: %s called on a Value with no interpreter", name)
	}
	rargs, err := in.toRubyAll(args)
	if err != nil {
		return Value{}, err
	}
	r, rerr := in.vm.Protect(func() object.Value { return in.vm.Send(v.obj(), name, rargs, nil) })
	if rerr != nil {
		return Value{}, in.newError(rerr, "")
	}
	return in.wrap(r), nil
}

// ValueOf converts a Go value to Ruby, as Set does, without assigning it.
func (in *Interp) ValueOf(x any) (Value, error) {
	r, err := in.toRuby(x)
	if err != nil {
		return Value{}, err
	}
	return in.wrap(r), nil
}

func (in *Interp) toRubyAll(args []any) ([]object.Value, error) {
	out := make([]object.Value, len(args))
	for i, a := range args {
		r, err := in.toRuby(a)
		if err != nil {
			return nil, err
		}
		out[i] = r
	}
	return out, nil
}

// toRuby converts a Go value to Ruby; see Value.
func (in *Interp) toRuby(x any) (object.Value, error) {
	switch v := x.(type) {
	case nil:
		return object.NilV, nil
	case Value:
		if v.in != nil && v.in != in {
			return nil, fmt.Errorf("ruby: a Value from another interpreter")
		}
		return v.obj(), nil
	case bool:
		return object.Bool(v), nil
	case int:
		return object.IntValue(int64(v)), nil
	case int8:
		return object.IntValue(int64(v)), nil
	case int16:
		return object.IntValue(int64(v)), nil
	case int32:
		return object.IntValue(int64(v)), nil
	case int64:
		return object.IntValue(v), nil
	case uint:
		return uintValue(uint64(v)), nil
	case uint8:
		return object.IntValue(int64(v)), nil
	case uint16:
		return object.IntValue(int64(v)), nil
	case uint32:
		return object.IntValue(int64(v)), nil
	case uint64:
		return uintValue(v), nil
	case *big.Int:
		if v == nil {
			return object.NilV, nil
		}
		return object.NormInt(new(big.Int).Set(v)), nil
	case float64:
		return object.Float(v), nil
	case float32:
		return object.Float(float64(v)), nil
	case complex128:
		return &object.Complex{Re: object.Float(real(v)), Im: object.Float(imag(v))}, nil
	case complex64:
		return &object.Complex{Re: object.Float(real(v)), Im: object.Float(imag(v))}, nil
	case *big.Rat:
		if v == nil {
			return object.NilV, nil
		}
		return &object.Rational{R: new(big.Rat).Set(v)}, nil
	case string:
		return object.NewString(v), nil
	case []byte:
		return object.NewStringBytes(append([]byte(nil), v...)), nil
	case Symbol:
		return object.SymVal(string(v)), nil
	case *nd.Array:
		if v == nil {
			return object.NilV, nil
		}
		return vm.WrapNDArray(v), nil
	}
	rv := reflect.ValueOf(x)
	if cls, ok := in.types[rv.Type()]; ok {
		return in.vm.WrapGo(x, cls), nil
	}
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		if rv.Kind() == reflect.Slice && rv.IsNil() {
			return object.NilV, nil
		}
		elems := make([]object.Value, rv.Len())
		for i := range elems {
			e, err := in.toRuby(rv.Index(i).Interface())
			if err != nil {
				return nil, err
			}
			elems[i] = e
		}
		return object.NewArrayFromSlice(elems), nil
	case reflect.Map:
		if rv.IsNil() {
			return object.NilV, nil
		}
		h := object.NewHash()
		it := rv.MapRange()
		for it.Next() {
			k, err := in.toRuby(it.Key().Interface())
			if err != nil {
				return nil, err
			}
			e, err := in.toRuby(it.Value().Interface())
			if err != nil {
				return nil, err
			}
			h.Set(k, e)
		}
		return h, nil
	}
	return in.vm.WrapGo(x, in.opaqueClass()), nil
}

func uintValue(u uint64) object.Value {
	if u > math.MaxInt64 {
		return object.NormInt(new(big.Int).SetUint64(u))
	}
	return object.IntValue(int64(u))
}

// opaqueClass is GoValue, the class of a Go value no Bind names. It is made on
// first use, so an interpreter that never sees one defines no such constant.
func (in *Interp) opaqueClass() *vm.RClass {
	if in.opaque == nil {
		c, _ := in.vm.DefineClass(nil, "GoValue", nil)
		c.DefineNative("inspect", func(_ *vm.VM, self object.Value, _ []object.Value, _ *vm.Proc) object.Value {
			return object.NewString(fmt.Sprintf("#<GoValue %T>", self.(*vm.GoObject).Value()))
		})
		in.opaque = c
	}
	return in.opaque
}

// fromRuby converts a Ruby value to Go; see Value. seen breaks cycles: an Array
// or Hash that contains itself converts to a Go value that contains itself.
func (in *Interp) fromRuby(v object.Value, seen map[object.Value]any) any {
	switch x := v.(type) {
	case object.Nil:
		return nil
	case object.Bool:
		return bool(x)
	case object.Integer:
		return int64(x)
	case *object.Bignum:
		return new(big.Int).Set(x.I)
	case object.Float:
		return float64(x)
	case *object.Complex:
		re, ok1 := realOf(x.Re)
		im, ok2 := realOf(x.Im)
		if ok1 && ok2 {
			return complex(re, im)
		}
	case *object.Rational:
		return new(big.Rat).Set(x.R)
	case *object.String:
		return x.Str()
	case object.Symbol:
		return Symbol(x)
	case *object.Array:
		if s, ok := seen[x]; ok {
			return s
		}
		out := make([]any, len(x.Elems))
		seen[x] = out
		for i, e := range x.Elems {
			out[i] = in.fromRuby(e, seen)
		}
		return out
	case *object.Hash:
		if s, ok := seen[x]; ok {
			return s
		}
		out := make(map[any]any, x.Len())
		seen[x] = out
		for _, k := range x.Keys {
			e, _ := x.Get(k)
			gk := in.fromRuby(k, seen)
			if gk != nil && !reflect.TypeOf(gk).Comparable() {
				gk = in.wrap(k)
			}
			out[gk] = in.fromRuby(e, seen)
		}
		return out
	case *vm.GoObject:
		return x.Value()
	}
	if a, ok := vm.UnwrapNDArray(v); ok {
		return a
	}
	return in.wrap(v)
}

// realOf reads an Integer or a Float as a float64.
func realOf(v object.Value) (float64, bool) {
	switch x := v.(type) {
	case object.Integer:
		return float64(x), true
	case object.Float:
		return float64(x), true
	}
	return 0, false
}
