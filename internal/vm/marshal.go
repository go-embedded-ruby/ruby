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
	vm.consts["Marshal"] = mod
	def := func(name string, fn NativeFn) { mod.smethods[name] = &Method{name: name, owner: mod, native: fn} }

	// Marshal.dump(obj, io=nil, limit=-1): serialize obj. When an IO-like second
	// argument is given the bytes are written to it and it is returned; otherwise
	// the bytes are returned as a String. The depth limit is accepted for arity
	// but not enforced.
	def("dump", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..3)")
		}
		data := vm.marshalDump(args[0])
		if io := marshalDumpIO(vm, args); io != nil {
			vm.send(io, "write", []object.Value{object.NewStringBytes(data)}, nil)
			return io
		}
		return object.NewStringBytes(data)
	})

	// Marshal.load(source, proc=nil, freeze:) / Marshal.restore: deserialize from
	// a String or IO. A proc (positional or block) is called on each loaded
	// object; the freeze: keyword deep-freezes the result.
	load := func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
		}
		freeze := false
		rest := args[1:]
		if n := len(rest); n > 0 {
			if h, ok := rest[n-1].(*object.Hash); ok && marshalIsKwargs(h) {
				freeze = marshalKwFreeze(h)
				rest = rest[:n-1]
			}
		}
		proc := blk
		if len(rest) > 0 && !object.IsNil(rest[0]) {
			if p, ok := rest[0].(*Proc); ok {
				proc = p
			}
		}
		data := marshalLoadSource(vm, args[0])
		return vm.marshalLoad(data, proc, freeze)
	}
	def("load", load)
	def("restore", load) // Marshal.restore is an alias for load
}

// marshalDumpIO returns the IO destination for Marshal.dump, or nil when the
// call has no IO argument. The second argument is an IO when it is non-nil and
// responds to #write; a bare Integer there is the depth limit, not an IO.
func marshalDumpIO(vm *VM, args []object.Value) object.Value {
	if len(args) < 2 || object.IsNil(args[1]) {
		return nil
	}
	if _, ok := args[1].(object.Integer); ok {
		return nil
	}
	if vm.respondsToDynamic(args[1], "write") {
		return args[1]
	}
	return nil
}

// marshalLoadSource returns the raw bytes of a Marshal.load source: a String
// directly, or the result of #read on an IO-like object.
func marshalLoadSource(vm *VM, src object.Value) []byte {
	if s, ok := src.(*object.String); ok {
		return s.Bytes()
	}
	if vm.respondsToDynamic(src, "read") {
		r := vm.send(src, "read", nil, nil)
		if s, ok := r.(*object.String); ok {
			return s.Bytes()
		}
	}
	raise("TypeError", "instance of IO needed")
	return nil // unreachable
}

// marshalIsKwargs reports whether h is a keyword-argument hash (all Symbol keys)
// as opposed to a positional Hash argument.
func marshalIsKwargs(h *object.Hash) bool {
	if len(h.Keys) == 0 {
		return false
	}
	for _, k := range h.Keys {
		if _, ok := k.(object.Symbol); !ok {
			return false
		}
	}
	return true
}

// marshalKwFreeze reads the freeze: keyword's truthiness from a kwargs hash.
func marshalKwFreeze(h *object.Hash) bool {
	v, ok := h.Get(object.Symbol("freeze"))
	return ok && v.Truthy()
}

// toMarshalValue converts a VM value to the marshal engine's value model. seen
// maps already-converted composite pointers to their marshal counterparts so
// shared/cyclic structures map to shared/cyclic ones (and thus encode as links).
func toMarshalValue(v object.Value, seen map[object.Value]marshal.Value) marshal.Value {
	switch x := v.(type) {
	case object.Nil:
		return marshal.Nil{}
	case object.Bool:
		return marshal.Bool(bool(x))
	case object.Integer:
		return marshal.Int{I: big.NewInt(int64(x))}
	case *object.Bignum:
		return marshal.Int{I: new(big.Int).Set(x.I)}
	case object.Float:
		return marshal.Float(float64(x))
	case object.Symbol:
		return marshal.Symbol(string(x))
	case *object.String:
		if m, ok := seen[v]; ok {
			return m
		}
		ms := &marshal.Str{Bytes: append([]byte(nil), x.Bytes()...), Enc: marshal.UTF8}
		seen[v] = ms
		return ms
	case *object.Array:
		if m, ok := seen[v]; ok {
			return m
		}
		ma := &marshal.Array{}
		seen[v] = ma
		for _, e := range x.Elems {
			ma.Elems = append(ma.Elems, toMarshalValue(e, seen))
		}
		return ma
	case *object.Hash:
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
		if x.Default != nil {
			mh.Default = toMarshalValue(x.Default, seen)
		}
		return mh
	default:
		panic(RubyError{Class: "TypeError",
			Message: fmt.Sprintf("no _dump_data is defined for class %s", classNameOf(v))})
	}
}

// fromMarshalValue converts a marshal value back to a VM value, sharing identity
// for composites so links and cycles reconstruct.
func fromMarshalValue(v marshal.Value, seen map[marshal.Value]object.Value) object.Value {
	switch x := v.(type) {
	case marshal.Nil:
		return object.NilV
	case marshal.Bool:
		return object.Bool(bool(x))
	case marshal.Int:
		return object.NormInt(new(big.Int).Set(x.I))
	case marshal.Float:
		return object.Float(float64(x))
	case marshal.Symbol:
		return object.Symbol(string(x))
	case *marshal.Str:
		if o, ok := seen[v]; ok {
			return o
		}
		os := object.NewStringBytes(append([]byte(nil), x.Bytes...))
		seen[v] = os
		return os
	case *marshal.Array:
		if o, ok := seen[v]; ok {
			return o
		}
		oa := object.NewArray()
		seen[v] = oa
		for _, e := range x.Elems {
			oa.Elems = append(oa.Elems, fromMarshalValue(e, seen))
		}
		return oa
	case *marshal.Hash:
		if o, ok := seen[v]; ok {
			return o
		}
		oh := object.NewHash()
		seen[v] = oh
		for i := range x.Keys {
			oh.Set(fromMarshalValue(x.Keys[i], seen), fromMarshalValue(x.Vals[i], seen))
		}
		if x.Default != nil {
			oh.Default = fromMarshalValue(x.Default, seen)
		}
		return oh
	default:
		// Defensive: the marshal engine only produces the cases above.
		return raise("ArgumentError", "marshal: unsupported value %s", v.RubyClass())
	}
}
