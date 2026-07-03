// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"math/big"

	json "github.com/go-ruby-json/json"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin binding between rbgo's Ruby object graph (object.Value)
// and the interpreter-independent value model of github.com/go-ruby-json/json.
// The parser and generator themselves live in that library (ported, byte-for-byte,
// from rbgo's former internal/vm/json.go); rbgo only translates its values to and
// from the library in a single streaming pass: parsing drives a json.Builder
// (objBuilder) that materialises the rbgo object graph directly — no intermediate
// json.Value tree and no second conversion — and generation pushes the rbgo graph
// straight into the library's json.Encoder via a json.Source (objSource). The
// library's typed errors are re-raised as the matching Ruby exception via their
// RubyClass().

// jsonParse parses a JSON document into a Ruby value by driving json.ParseInto
// with an objBuilder, building the rbgo object graph in one pass. A malformed
// document raises JSON::ParserError; exceeding the nesting limit
// JSON::NestingError.
func jsonParse(src string, opts ...json.Option) object.Value {
	var b objBuilder
	if err := json.ParseInto(src, &b, opts...); err != nil {
		raiseJSONError(err)
	}
	return b.Result().(object.Value)
}

// jsonGenerate renders a Ruby value to a compact JSON document via
// json.GenerateSource, re-raising a non-finite-float / over-deep value as the
// matching Ruby exception.
func jsonGenerate(v object.Value, opts ...json.Option) string {
	out, err := json.GenerateSource(objSource{v}, opts...)
	if err != nil {
		raiseJSONError(err)
	}
	return out
}

// jsonPrettyGenerate renders a Ruby value with MRI's JSON.pretty_generate layout.
func jsonPrettyGenerate(v object.Value, opts ...json.Option) string {
	out, err := json.PrettyGenerateSource(objSource{v}, opts...)
	if err != nil {
		raiseJSONError(err)
	}
	return out
}

// raiseJSONError re-raises a library error as the Ruby exception named by its
// RubyClass() (JSON::ParserError / JSON::NestingError / JSON::GeneratorError /
// TypeError). Every error Parse / Generate / PrettyGenerate returns is a
// json.Error, so the assertion is total.
func raiseJSONError(err error) {
	je := err.(json.Error)
	raise(je.RubyClass(), "%s", je.Error())
}

// --- library parse events -> rbgo object graph (objBuilder) ----------------

// objBuilder is the json.Builder that materialises rbgo values directly as the
// parser reads them. It keeps an explicit stack of open Array/Hash containers and
// attaches each emitted value to the innermost one (an array element, or the
// value of the pending Hash key), so parsing builds the object graph in a single
// allocation-light pass: arrays and hashes are pre-sized to their exact length,
// small integers are interned (object.IntValue), and strings come straight from
// the parser's zero-copy slices.
type objBuilder struct {
	stack []objFrame
	root  object.Value
}

// objFrame is one open container on the builder stack: an *object.Array while an
// array is open, or an *object.Hash plus its pending key while an object is open.
type objFrame struct {
	arr *object.Array
	h   *object.Hash
	key object.Value // pending Hash key set by Key, consumed by the next emit
}

// emit attaches v to the innermost open container, or records it as the parse
// result at the top level.
func (b *objBuilder) emit(v object.Value) {
	if n := len(b.stack); n > 0 {
		f := &b.stack[n-1]
		if f.h != nil {
			f.h.Set(f.key, v)
			return
		}
		f.arr.Elems = append(f.arr.Elems, v)
		return
	}
	b.root = v
}

func (b *objBuilder) Null()           { b.emit(object.NilVal()) }
func (b *objBuilder) Bool(x bool)     { b.emit(object.BoolValue(bool(object.Bool(x)))) }
func (b *objBuilder) Int(n int64)     { b.emit(object.IntValue(n)) }
func (b *objBuilder) Float(f float64) { b.emit(object.FloatValue(float64(object.Float(f)))) }
func (b *objBuilder) Str(s string)    { b.emit(object.Wrap(object.NewString(s))) }

func (b *objBuilder) Big(n *big.Int) { b.emit(object.NormInt(n)) }

func (b *objBuilder) BeginArray(n int) {
	b.stack = append(b.stack, objFrame{arr: object.NewArrayFromSlice(make([]object.Value, 0, n))})
}

func (b *objBuilder) EndArray() {
	f := b.stack[len(b.stack)-1]
	b.stack = b.stack[:len(b.stack)-1]
	b.emit(object.Wrap(f.arr))
}

func (b *objBuilder) BeginObject(n int) {
	b.stack = append(b.stack, objFrame{h: object.NewHashCap(n)})
}

func (b *objBuilder) Key(s string, symbolize bool) {
	f := &b.stack[len(b.stack)-1]
	if symbolize {
		f.key = object.SymVal(string(object.Symbol(s)))
	} else {
		f.key = object.Wrap(object.NewString(s))
	}
}

func (b *objBuilder) EndObject() {
	f := b.stack[len(b.stack)-1]
	b.stack = b.stack[:len(b.stack)-1]
	b.emit(object.Wrap(f.h))
}

// Result returns the top-level value (as json.Value, per the Builder interface);
// jsonParse asserts it back to object.Value.
func (b *objBuilder) Result() json.Value { return b.root }

// --- rbgo object graph -> library generate events (objSource) --------------

// objSource is the json.Source that streams a Ruby value into the library's
// Encoder, walking the rbgo object graph once with no intermediate json value.
// The JSON value shapes (nil / true / false / Integer / Bignum / Float / String /
// Symbol / Array / ordered Hash) all map directly; any other value emits its Ruby
// to_s as a JSON string, matching the former generator's to_s-of-unknown fallback
// (e.g. a Range serialises as "1..2").
type objSource struct{ v object.Value }

// EmitTo pushes the wrapped value (and, recursively, its children) into e.
func (s objSource) EmitTo(e *json.Encoder) error { return emitValue(e, s.v) }

// emitValue renders one rbgo value through the encoder.
func emitValue(e *json.Encoder, v object.Value) error {
	{
		__sw79 := v
		switch {
		case object.IsNil(__sw79) || object.IsNilObj(__sw79):
			n := __sw79
			_ = n
			e.Null()
		case object.IsBool(__sw79):
			n := object.AsBoolV(__sw79)
			_ = n
			e.Bool(bool(n))
		case object.IsInt(__sw79):
			n := object.AsInteger(__sw79)
			_ = n
			e.Int(int64(n))
		case object.IsKind[*object.Bignum](__sw79):
			n := object.Kind[*object.Bignum](__sw79)
			_ = n
			e.Big(n.I)
		case object.IsFloat(__sw79):
			n := object.AsFloatV(__sw79)
			_ = n
			return e.Float(float64(n))
		case object.IsKind[*object.String](__sw79):
			n := object.Kind[*object.String](__sw79)
			_ = n
			e.Str(n.Str())
		case object.IsKind[object.Symbol](__sw79):
			n := object.Kind[object.Symbol](__sw79)
			_ = n
			e.Str(string(n))
		case object.IsKind[*object.Array](__sw79):
			n := object.Kind[*object.Array](__sw79)
			_ = n
			elems := n.Elems
			return e.Array(len(elems), func() error {
				for _, el := range elems {
					e.Elem()
					if err := emitValue(e, el); err != nil {
						return err
					}
				}
				return nil
			})
		case object.IsKind[*object.Hash](__sw79):
			n := object.Kind[*object.Hash](__sw79)
			_ = n
			keys := n.Keys
			return e.Object(len(keys), func() error {
				for _, k := range keys {
					val, _ := n.Get(k)
					e.Key(jsonKeyString(k))
					if err := emitValue(e, val); err != nil {
						return err
					}
				}
				return nil
			})
		default:
			n := __sw79
			_ = n
			e.Str(v.ToS())
		}
	}
	return nil
}

// jsonKeyString renders a Hash key as the string JSON requires: a String keeps
// its contents, a Symbol its bare name, anything else its Ruby to_s (MRI coerces
// every JSON object key to a string).
func jsonKeyString(k object.Value) string {
	{
		__sw80 := k
		switch {
		case object.IsKind[*object.String](__sw80):
			key := object.Kind[*object.String](__sw80)
			_ = key
			return key.Str()
		case object.IsKind[object.Symbol](__sw80):
			key := object.Kind[object.Symbol](__sw80)
			_ = key
			return string(key)
		}
	}
	return k.ToS()
}
