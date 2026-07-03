package vm

import (
	"fmt"
	"math/big"

	"github.com/go-embedded-ruby/ruby/internal/object"
	"github.com/go-ruby-marshal/marshal"
)

// registerMarshal installs the core Marshal module: Marshal.dump / Marshal.load
// and the MAJOR_VERSION / MINOR_VERSION constants. Serialization runs on the
// standalone pure-Go github.com/go-ruby-marshal/marshal engine (CGO=0), with
// the VM's values converted to and from that engine's value model — preserving
// object identity so shared and cyclic structures round-trip as in MRI.
func (vm *VM) registerMarshal() {
	mod := newClass("Marshal", nil)
	mod.isModule = true
	mod.consts["MAJOR_VERSION"] = object.IntValue(4)
	mod.consts["MINOR_VERSION"] = object.IntValue(8)
	vm.consts["Marshal"] = object.Wrap(mod)
	def := func(name string, fn NativeFn) { mod.smethods[name] = &Method{name: name, owner: mod, native: fn} }

	def("dump", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		mv := toMarshalValue(args[0], map[object.Value]marshal.Value{})
		return object.Wrap(object.NewString(string(marshal.Dump(mv))))
	})
	def("load", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		s, ok := object.KindOK[*object.String](args[0])
		if !ok {
			raise("TypeError", "instance of IO needed")
		}
		v, err := marshal.Load([]byte(s.Str()))
		if err != nil {
			raise("ArgumentError", "%s", err.Error())
		}
		return fromMarshalValue(v, map[marshal.Value]object.Value{})
	})
	def("restore", mod.smethods["load"].native) // Marshal.restore is an alias for load
}

// toMarshalValue converts a VM value to the marshal engine's value model. seen
// maps already-converted composite pointers to their marshal counterparts so
// shared/cyclic structures map to shared/cyclic ones (and thus encode as links).
func toMarshalValue(v object.Value, seen map[object.Value]marshal.Value) marshal.Value {
	{
		__sw93 := v
		switch {
		case object.IsNilObj(__sw93):
			x := object.NilObj()
			_ = x
			return marshal.Nil{}
		case object.IsBool(__sw93):
			x := object.AsBoolV(__sw93)
			_ = x
			return marshal.Bool(bool(x))
		case object.IsInt(__sw93):
			x := object.AsInteger(__sw93)
			_ = x
			return marshal.Int{I: big.NewInt(int64(x))}
		case object.IsKind[*object.Bignum](__sw93):
			x := object.Kind[*object.Bignum](__sw93)
			_ = x
			return marshal.Int{I: new(big.Int).Set(x.I)}
		case object.IsFloat(__sw93):
			x := object.AsFloatV(__sw93)
			_ = x
			return marshal.Float(float64(x))
		case object.IsKind[object.Symbol](__sw93):
			x := object.Kind[object.Symbol](__sw93)
			_ = x
			return marshal.Symbol(string(x))
		case object.IsKind[*object.String](__sw93):
			x := object.Kind[*object.String](__sw93)
			_ = x
			if m, ok := seen[v]; ok {
				return m
			}
			ms := &marshal.Str{Bytes: append([]byte(nil), x.Bytes()...), Enc: marshal.UTF8}
			seen[v] = ms
			return ms
		case object.IsKind[*object.Array](__sw93):
			x := object.Kind[*object.Array](__sw93)
			_ = x
			if m, ok := seen[v]; ok {
				return m
			}
			ma := &marshal.Array{}
			seen[v] = ma
			for _, e := range x.Elems {
				ma.Elems = append(ma.Elems, toMarshalValue(e, seen))
			}
			return ma
		case object.IsKind[*object.Hash](__sw93):
			x := object.Kind[*object.Hash](__sw93)
			_ = x
			if m, ok := seen[v]; ok {
				return m
			}
			if !object.IsNil(x.DefaultProc) {
				raise("TypeError", "can't dump hash with default proc")
			}
			mh := &marshal.Hash{}
			seen[v] = mh
			for _, k := range x.Keys {
				val, _ := x.Get(k)
				mh.Keys = append(mh.Keys, toMarshalValue(k, seen))
				mh.Vals = append(mh.Vals, toMarshalValue(val, seen))
			}
			if !object.IsNil(x.Default) {
				mh.Default = toMarshalValue(x.Default, seen)
			}
			return mh
		default:
			x := __sw93
			_ = x
			panic(RubyError{Class: "TypeError",
				Message: fmt.Sprintf("no _dump_data is defined for class %s", classNameOf(v))})
		}
	}
}

// fromMarshalValue converts a marshal value back to a VM value, sharing identity
// for composites so links and cycles reconstruct.
func fromMarshalValue(v marshal.Value, seen map[marshal.Value]object.Value) object.Value {
	switch x := v.(type) {
	case marshal.Nil:
		return object.NilVal()
	case marshal.Bool:
		return object.BoolValue(bool(object.Bool(bool(x))))
	case marshal.Int:
		return object.NormInt(new(big.Int).Set(x.I))
	case marshal.Float:
		return object.FloatValue(float64(object.Float(float64(x))))
	case marshal.Symbol:
		return object.SymVal(string(object.Symbol(string(x))))
	case *marshal.Str:
		if o, ok := seen[v]; ok {
			return o
		}
		os := object.NewStringBytes(append([]byte(nil), x.Bytes...))
		seen[v] = object.Wrap(os)
		return object.Wrap(os)
	case *marshal.Array:
		if o, ok := seen[v]; ok {
			return o
		}
		oa := object.NewArray()
		seen[v] = object.Wrap(oa)
		for _, e := range x.Elems {
			oa.Elems = append(oa.Elems, fromMarshalValue(e, seen))
		}
		return object.Wrap(oa)
	case *marshal.Hash:
		if o, ok := seen[v]; ok {
			return o
		}
		oh := object.NewHash()
		seen[v] = object.Wrap(oh)
		for i := range x.Keys {
			oh.Set(fromMarshalValue(x.Keys[i], seen), fromMarshalValue(x.Vals[i], seen))
		}
		if x.Default != nil {
			oh.Default = fromMarshalValue(x.Default, seen)
		}
		return object.Wrap(oh)
	default:
		// Defensive: the marshal engine only produces the cases above.
		return raise("ArgumentError", "marshal: unsupported value %s", v.RubyClass())
	}
}
