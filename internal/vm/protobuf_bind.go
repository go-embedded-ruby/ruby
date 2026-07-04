// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"math"
	"math/big"

	protobuf "github.com/go-ruby-protobuf/protobuf"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin binding between rbgo's Ruby object graph and the
// interpreter-independent runtime of github.com/go-ruby-protobuf/protobuf — a
// pure-Go (CGO-free) reimplementation of the google-protobuf gem's object model
// built on google.golang.org/protobuf, the canonical Go runtime. rbgo only
// carries the handful of value shells the Google::Protobuf namespace is made of
// (a pool, its builder-DSL receivers, descriptors, a generated message class and
// its instances, and the RepeatedField / Map containers) and translates Ruby
// field values to and from the library's fixed Go value model. Every byte on the
// wire is produced by the canonical runtime, so encode/decode is wire-compatible
// with real protobuf by construction. The module wiring lives in protobuf.go.

// --- value shells ----------------------------------------------------------

// ProtobufPool is a Google::Protobuf::DescriptorPool: a registry of descriptors.
type ProtobufPool struct {
	cls *RClass
	p   *protobuf.DescriptorPool
}

func (o *ProtobufPool) ToS() string     { return "#<Google::Protobuf::DescriptorPool>" }
func (o *ProtobufPool) Inspect() string { return o.ToS() }
func (o *ProtobufPool) Truthy() bool    { return true }

// ProtobufBuilder is the receiver of a pool.build block (add_message/add_enum).
// It is live only for the duration of the block: it wraps the library's *Builder,
// which is valid only inside the pool.Build callback rbgo runs the block within.
type ProtobufBuilder struct {
	cls *RClass
	b   *protobuf.Builder
}

func (o *ProtobufBuilder) ToS() string     { return "#<Google::Protobuf::Builder>" }
func (o *ProtobufBuilder) Inspect() string { return o.ToS() }
func (o *ProtobufBuilder) Truthy() bool    { return true }

// ProtobufMessageBuilder is the receiver of an add_message block
// (optional/repeated/map/oneof). Live only inside that block.
type ProtobufMessageBuilder struct {
	cls *RClass
	mb  *protobuf.MessageBuilder
}

func (o *ProtobufMessageBuilder) ToS() string     { return "#<Google::Protobuf::MessageBuilder>" }
func (o *ProtobufMessageBuilder) Inspect() string { return o.ToS() }
func (o *ProtobufMessageBuilder) Truthy() bool    { return true }

// ProtobufOneofBuilder is the receiver of a oneof block. Live only inside it.
type ProtobufOneofBuilder struct {
	cls *RClass
	ob  *protobuf.OneofBuilder
}

func (o *ProtobufOneofBuilder) ToS() string     { return "#<Google::Protobuf::OneofBuilder>" }
func (o *ProtobufOneofBuilder) Inspect() string { return o.ToS() }
func (o *ProtobufOneofBuilder) Truthy() bool    { return true }

// ProtobufEnumBuilder is the receiver of an add_enum block (value). Live only
// inside it.
type ProtobufEnumBuilder struct {
	cls *RClass
	eb  *protobuf.EnumBuilder
}

func (o *ProtobufEnumBuilder) ToS() string     { return "#<Google::Protobuf::EnumBuilder>" }
func (o *ProtobufEnumBuilder) Inspect() string { return o.ToS() }
func (o *ProtobufEnumBuilder) Truthy() bool    { return true }

// ProtobufDescriptor is a Google::Protobuf::Descriptor (a message descriptor).
type ProtobufDescriptor struct {
	cls *RClass
	d   *protobuf.Descriptor
}

func (o *ProtobufDescriptor) ToS() string {
	return "#<Google::Protobuf::Descriptor: " + o.d.Name() + ">"
}
func (o *ProtobufDescriptor) Inspect() string { return o.ToS() }
func (o *ProtobufDescriptor) Truthy() bool    { return true }

// ProtobufEnumDescriptor is a Google::Protobuf::EnumDescriptor.
type ProtobufEnumDescriptor struct {
	cls *RClass
	ed  *protobuf.EnumDescriptor
}

func (o *ProtobufEnumDescriptor) ToS() string {
	return "#<Google::Protobuf::EnumDescriptor: " + o.ed.Name() + ">"
}
func (o *ProtobufEnumDescriptor) Inspect() string { return o.ToS() }
func (o *ProtobufEnumDescriptor) Truthy() bool    { return true }

// ProtobufFieldDescriptor is a Google::Protobuf::FieldDescriptor.
type ProtobufFieldDescriptor struct {
	cls *RClass
	fd  *protobuf.FieldDescriptor
}

func (o *ProtobufFieldDescriptor) ToS() string {
	return "#<Google::Protobuf::FieldDescriptor: " + o.fd.Name() + ">"
}
func (o *ProtobufFieldDescriptor) Inspect() string { return o.ToS() }
func (o *ProtobufFieldDescriptor) Truthy() bool    { return true }

// ProtobufMsgClass is a generated message class (descriptor.msgclass): the value
// a Ruby program calls .new / .name / .descriptor on.
type ProtobufMsgClass struct {
	cls *RClass
	mc  *protobuf.MessageClass
}

func (o *ProtobufMsgClass) ToS() string     { return o.mc.Name() }
func (o *ProtobufMsgClass) Inspect() string { return o.mc.Name() }
func (o *ProtobufMsgClass) Truthy() bool    { return true }

// ProtobufMessage is a message instance: a dynamicpb dynamic message wrapped so
// its fields are read/written through method_missing accessors and it encodes and
// decodes through the canonical runtime.
type ProtobufMessage struct {
	cls *RClass
	m   *protobuf.Message
}

func (o *ProtobufMessage) ToS() string     { return o.m.Inspect() }
func (o *ProtobufMessage) Inspect() string { return o.m.Inspect() }
func (o *ProtobufMessage) Truthy() bool    { return true }

// ProtobufRepeatedField is a Google::Protobuf::RepeatedField (a typed list).
type ProtobufRepeatedField struct {
	cls *RClass
	r   *protobuf.RepeatedField
}

func (o *ProtobufRepeatedField) ToS() string     { return o.r.Inspect() }
func (o *ProtobufRepeatedField) Inspect() string { return o.r.Inspect() }
func (o *ProtobufRepeatedField) Truthy() bool    { return true }

// ProtobufMap is a Google::Protobuf::Map (a typed map).
type ProtobufMap struct {
	cls *RClass
	m   *protobuf.Map
}

func (o *ProtobufMap) ToS() string     { return o.m.Inspect() }
func (o *ProtobufMap) Inspect() string { return o.m.Inspect() }
func (o *ProtobufMap) Truthy() bool    { return true }

// --- class lookups ---------------------------------------------------------

// pbClass returns the RClass for a nested constant of Google::Protobuf (e.g.
// "Message", "RepeatedField", "Map"), which registerProtobuf always installs.
func (vm *VM) pbClass(short string) *RClass {
	return vm.consts["Google::Protobuf::"+short].(*RClass)
}

// newProtobufMessage wraps a library message as the Ruby Google::Protobuf::Message
// value type. A nil message (an unset sub-message field) becomes Ruby nil.
func (vm *VM) newProtobufMessage(m *protobuf.Message) object.Value {
	if m == nil {
		return object.NilV
	}
	return &ProtobufMessage{cls: vm.pbClass("Message"), m: m}
}

// newProtobufMsgClass wraps a library message class as the Ruby msgclass value.
func (vm *VM) newProtobufMsgClass(mc *protobuf.MessageClass) object.Value {
	return &ProtobufMsgClass{cls: vm.pbClass("AbstractMessageClass"), mc: mc}
}

// --- Ruby value <-> protobuf field value -----------------------------------

// rubyToPB maps a Ruby value to the protobuf library's fixed Go value model (the
// `any` a field setter / container insertion expects): bool, int64 (signed
// integer), uint64 (an unsigned value beyond int64), float64, string (a UTF-8
// String), []byte (a binary String), a Symbol (an enum value name), a *Message,
// a *RepeatedField or a *Map. An Array becomes []any (a repeated-field
// replacement) and a Hash becomes map[any]any (a map replacement). Anything else
// is handed to the library as-is, which reports the resulting TypeError.
func (vm *VM) rubyToPB(v object.Value) any {
	switch x := v.(type) {
	case nil, object.Nil:
		return nil
	case object.Bool:
		return bool(x)
	case object.Integer:
		return int64(x)
	case *object.Bignum:
		// A rbgo Bignum always lies outside int64 (values that fit are normalised
		// back to Integer), so only the unsigned window and the overflow tail remain.
		if x.I.IsUint64() {
			return x.I.Uint64()
		}
		return x.I // out of range for every field: the library reports it
	case object.Float:
		return float64(x)
	case *object.String:
		if x.IsBinary() {
			return x.Bytes()
		}
		return x.Str()
	case object.Symbol:
		return protobuf.Symbol(string(x))
	case *ProtobufMessage:
		return x.m
	case *ProtobufRepeatedField:
		return x.r
	case *ProtobufMap:
		return x.m
	case *object.Array:
		out := make([]any, len(x.Elems))
		for i, e := range x.Elems {
			out[i] = vm.rubyToPB(e)
		}
		return out
	case *object.Hash:
		m := map[any]any{}
		for _, k := range x.Keys {
			val, _ := x.Get(k)
			m[vm.rubyToPB(k)] = vm.rubyToPB(val)
		}
		return m
	default:
		return v // an unmapped value: the library raises the matching TypeError
	}
}

// pbToRuby maps a protobuf value (a field read, a container element, or a node of
// a to_h tree) back into the rbgo object graph. A bytes value becomes an
// ASCII-8BIT String, a string a UTF-8 String, an enum a Symbol, a message the
// Message value type, and the container shapes recurse — a []any to an Array, a
// map[any]any (a protobuf map) to a Hash with its raw keys, and a map[string]any
// (a message's to_h) to a Hash keyed by field-name Symbols, matching the gem.
func (vm *VM) pbToRuby(v any) object.Value {
	switch x := v.(type) {
	case nil:
		return object.NilV
	case bool:
		return object.Bool(x)
	case int64:
		return object.IntValue(x)
	case uint64:
		if x <= math.MaxInt64 {
			return object.IntValue(int64(x))
		}
		return object.NormInt(new(big.Int).SetUint64(x))
	case float64:
		return object.Float(x)
	case string:
		return object.NewString(x)
	case []byte:
		return object.NewStringBytesEnc(x, "ASCII-8BIT")
	case protobuf.Symbol:
		return object.Symbol(string(x))
	case *protobuf.Message:
		return vm.newProtobufMessage(x)
	case *protobuf.RepeatedField:
		return &ProtobufRepeatedField{cls: vm.pbClass("RepeatedField"), r: x}
	case *protobuf.Map:
		return &ProtobufMap{cls: vm.pbClass("Map"), m: x}
	case []any:
		arr := object.NewArrayFromSlice(make([]object.Value, len(x)))
		for i, e := range x {
			arr.Elems[i] = vm.pbToRuby(e)
		}
		return arr
	case map[any]any:
		h := object.NewHash()
		for k, val := range x {
			h.Set(vm.pbToRuby(k), vm.pbToRuby(val))
		}
		return h
	case map[string]any:
		h := object.NewHash()
		for k, val := range x {
			h.Set(object.Symbol(k), vm.pbToRuby(val))
		}
		return h
	default:
		return object.NilV // the library never produces another shape
	}
}

// pbInitHash reads an optional trailing keyword Hash of field=>value assignments
// (MyMsg.new(name: "Ada", id: 1)) into the map[string]any the library's New
// takes, translating each value through rubyToPB. A missing/empty Hash yields an
// empty map (New with no initialiser).
func (vm *VM) pbInitHash(args []object.Value) map[string]any {
	out := map[string]any{}
	if len(args) == 0 {
		return out
	}
	h, ok := args[len(args)-1].(*object.Hash)
	if !ok {
		return out
	}
	for _, k := range h.Keys {
		val, _ := h.Get(k)
		out[pbName(k)] = vm.rubyToPB(val)
	}
	return out
}

// --- small argument helpers ------------------------------------------------

// pbName reads a field/method name from a Ruby Symbol or String.
func pbName(v object.Value) string {
	switch n := v.(type) {
	case object.Symbol:
		return string(n)
	case *object.String:
		return n.Str()
	default:
		raise("TypeError", "no implicit conversion of %T into a field name", v)
		return ""
	}
}

// pbSym reads a type/enum Symbol argument (the builder DSL's :int32 / :string),
// accepting a Ruby Symbol or String.
func pbSym(v object.Value) protobuf.Symbol {
	return protobuf.Symbol(pbName(v))
}

// pbTypeName reads the optional trailing "TypeName" argument of a message/enum
// field declaration: it returns the String at args[i] (i within range), else "".
func pbTypeName(args []object.Value, i int) string {
	if i < len(args) {
		if s, ok := args[i].(*object.String); ok {
			return s.Str()
		}
	}
	return ""
}

// raisePBError re-raises a protobuf-library error as the Ruby exception it names.
// Every error the library returns implements protobuf.Error (its RubyClass is one
// of Google::Protobuf::TypeError / ParseError or the core RangeError /
// ArgumentError), so a plain type assertion is total.
func raisePBError(err error) object.Value {
	pe := err.(protobuf.Error)
	return raise(pe.RubyClass(), "%s", pe.Error())
}
