// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"strings"

	protobuf "github.com/go-ruby-protobuf/protobuf"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerProtobuf installs the Google::Protobuf module (require "google/protobuf"),
// backed by github.com/go-ruby-protobuf/protobuf — a pure-Go (CGO-free)
// reimplementation of the google-protobuf gem's runtime and builder surface built
// on google.golang.org/protobuf, the canonical Go runtime. It wires the
// DescriptorPool and its pool.build DSL, the generated message classes and their
// dynamic instances (field accessors, to_h, ==, dup), the RepeatedField and Map
// containers, the module-level encode / decode / encode_json / decode_json
// one-shots, the pre-registered well-known types (Timestamp / Duration / Any /
// Struct / …) and the Google::Protobuf::TypeError / ParseError exception tree.
// Every byte on the wire is produced by the canonical runtime, so encode/decode is
// wire-compatible with real protobuf. The value shells and the Ruby<->protobuf
// value bridge live in protobuf_bind.go.
func (vm *VM) registerProtobuf() {
	google := newClass("Google", nil)
	google.isModule = true
	vm.consts["Google"] = google

	mod := newClass("Google::Protobuf", nil)
	mod.isModule = true
	google.consts["Protobuf"] = mod
	vm.consts["Google::Protobuf"] = mod

	vm.registerProtobufErrors(mod)
	vm.registerProtobufClasses(mod)
	vm.registerProtobufWKT(mod)
	vm.registerProtobufModuleMethods(mod)
}

// pbNewClass creates a Google::Protobuf::<short> class (super = Object), registers
// it both as a nested constant of the module and under its qualified name in the
// top-level table (so a re-raised library error and a `Google::Protobuf::X`
// reference both resolve to it), and returns it.
func (vm *VM) pbNewClass(mod *RClass, short string) *RClass {
	c := newClass("Google::Protobuf::"+short, vm.cObject)
	mod.consts[short] = c
	vm.consts["Google::Protobuf::"+short] = c
	return c
}

// registerProtobufErrors installs the exception tree the library maps its errors
// to: Google::Protobuf::TypeError (< the core ::TypeError, as in MRI) and
// Google::Protobuf::ParseError (< RuntimeError). The library's other errors report
// the core RangeError / ArgumentError, which already exist.
func (vm *VM) registerProtobufErrors(mod *RClass) {
	coreTypeError := vm.consts["TypeError"].(*RClass)
	runtimeError := vm.consts["RuntimeError"].(*RClass)
	reg := func(short string, super *RClass) {
		c := newClass("Google::Protobuf::"+short, super)
		mod.consts[short] = c
		vm.consts["Google::Protobuf::"+short] = c
	}
	reg("TypeError", coreTypeError)
	reg("ParseError", runtimeError)
}

// registerProtobufClasses installs the descriptor, builder-DSL, message-class,
// message-instance and container classes.
func (vm *VM) registerProtobufClasses(mod *RClass) {
	poolCls := vm.pbNewClass(mod, "DescriptorPool")
	builderCls := vm.pbNewClass(mod, "Builder")
	msgBuilderCls := vm.pbNewClass(mod, "MessageBuilder")
	oneofBuilderCls := vm.pbNewClass(mod, "OneofBuilder")
	enumBuilderCls := vm.pbNewClass(mod, "EnumBuilder")
	descCls := vm.pbNewClass(mod, "Descriptor")
	enumDescCls := vm.pbNewClass(mod, "EnumDescriptor")
	fieldDescCls := vm.pbNewClass(mod, "FieldDescriptor")
	msgClassCls := vm.pbNewClass(mod, "AbstractMessageClass")
	msgCls := vm.pbNewClass(mod, "Message")
	repeatedCls := vm.pbNewClass(mod, "RepeatedField")
	mapCls := vm.pbNewClass(mod, "Map")

	vm.registerProtobufPool(poolCls, builderCls)
	vm.registerProtobufBuilders(builderCls, msgBuilderCls, oneofBuilderCls, enumBuilderCls)
	vm.registerProtobufDescriptors(descCls, enumDescCls, fieldDescCls)
	vm.registerProtobufMsgClass(msgClassCls, descCls)
	vm.registerProtobufMessage(msgCls)
	vm.registerProtobufRepeated(repeatedCls)
	vm.registerProtobufMap(mapCls)
}

// registerProtobufPool wires DescriptorPool: the generated_pool / new class
// methods and the #build DSL and #lookup instance methods.
func (vm *VM) registerProtobufPool(poolCls, builderCls *RClass) {
	poolCls.smethods["generated_pool"] = &Method{name: "generated_pool", owner: poolCls,
		native: func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
			return &ProtobufPool{cls: poolCls, p: protobuf.GeneratedPool()}
		}}
	poolCls.smethods["new"] = &Method{name: "new", owner: poolCls,
		native: func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
			return &ProtobufPool{cls: poolCls, p: protobuf.NewDescriptorPool()}
		}}

	// pool.build { … } compiles a batch of message/enum definitions. The block is
	// instance_eval'd against a Builder (and also passed it as its argument), so
	// both `add_message "Foo" do … end` and `do |b| b.add_message … end` resolve.
	poolCls.define("build", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("ArgumentError", "DescriptorPool#build requires a block")
		}
		p := self.(*ProtobufPool).p
		if err := p.Build(func(b *protobuf.Builder) {
			vm.callBlockSelf(blk, &ProtobufBuilder{cls: builderCls, b: b}, []object.Value{&ProtobufBuilder{cls: builderCls, b: b}})
		}); err != nil {
			raisePBError(err)
		}
		return object.NilV
	})

	// pool.lookup(name) → the message Descriptor, the EnumDescriptor, or nil.
	poolCls.define("lookup", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		switch r := self.(*ProtobufPool).p.Lookup(pbName(args[0])).(type) {
		case *protobuf.Descriptor:
			return &ProtobufDescriptor{cls: vm.pbClass("Descriptor"), d: r}
		case *protobuf.EnumDescriptor:
			return &ProtobufEnumDescriptor{cls: vm.pbClass("EnumDescriptor"), ed: r}
		default:
			return object.NilV
		}
	})
}

// pbYieldSelf instance_eval's a DSL sub-block (add_message / add_enum / oneof)
// against wrapper, also passing it as the block argument. A nil block (an
// add_message with no field block — a legal empty message) is a no-op.
func (vm *VM) pbYieldSelf(blk *Proc, wrapper object.Value) {
	if blk != nil {
		vm.callBlockSelf(blk, wrapper, []object.Value{wrapper})
	}
}

// registerProtobufBuilders wires the DSL receiver classes: Builder
// (add_message/add_enum), MessageBuilder (optional/repeated/map/oneof),
// OneofBuilder (optional) and EnumBuilder (value).
func (vm *VM) registerProtobufBuilders(builderCls, msgBuilderCls, oneofBuilderCls, enumBuilderCls *RClass) {
	builderCls.define("add_message", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		self.(*ProtobufBuilder).b.AddMessage(pbName(args[0]), func(mb *protobuf.MessageBuilder) {
			vm.pbYieldSelf(blk, &ProtobufMessageBuilder{cls: msgBuilderCls, mb: mb})
		})
		return object.NilV
	})
	builderCls.define("add_enum", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		self.(*ProtobufBuilder).b.AddEnum(pbName(args[0]), func(eb *protobuf.EnumBuilder) {
			vm.pbYieldSelf(blk, &ProtobufEnumBuilder{cls: enumBuilderCls, eb: eb})
		})
		return object.NilV
	})

	msgBuilderCls.define("optional", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		mb := self.(*ProtobufMessageBuilder).mb
		if tn := pbTypeName(args, 3); tn != "" {
			mb.Optional(pbName(args[0]), pbSym(args[1]), int(intArg(args[2])), tn)
		} else {
			mb.Optional(pbName(args[0]), pbSym(args[1]), int(intArg(args[2])))
		}
		return object.NilV
	})
	msgBuilderCls.define("repeated", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		mb := self.(*ProtobufMessageBuilder).mb
		if tn := pbTypeName(args, 3); tn != "" {
			mb.Repeated(pbName(args[0]), pbSym(args[1]), int(intArg(args[2])), tn)
		} else {
			mb.Repeated(pbName(args[0]), pbSym(args[1]), int(intArg(args[2])))
		}
		return object.NilV
	})
	msgBuilderCls.define("map", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		mb := self.(*ProtobufMessageBuilder).mb
		if tn := pbTypeName(args, 4); tn != "" {
			mb.Map(pbName(args[0]), pbSym(args[1]), pbSym(args[2]), int(intArg(args[3])), tn)
		} else {
			mb.Map(pbName(args[0]), pbSym(args[1]), pbSym(args[2]), int(intArg(args[3])))
		}
		return object.NilV
	})
	msgBuilderCls.define("oneof", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		self.(*ProtobufMessageBuilder).mb.Oneof(pbName(args[0]), func(ob *protobuf.OneofBuilder) {
			vm.pbYieldSelf(blk, &ProtobufOneofBuilder{cls: oneofBuilderCls, ob: ob})
		})
		return object.NilV
	})

	oneofBuilderCls.define("optional", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		ob := self.(*ProtobufOneofBuilder).ob
		if tn := pbTypeName(args, 3); tn != "" {
			ob.Optional(pbName(args[0]), pbSym(args[1]), int(intArg(args[2])), tn)
		} else {
			ob.Optional(pbName(args[0]), pbSym(args[1]), int(intArg(args[2])))
		}
		return object.NilV
	})

	enumBuilderCls.define("value", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		self.(*ProtobufEnumBuilder).eb.Value(pbSym(args[0]), int(intArg(args[1])))
		return object.NilV
	})
}

// registerProtobufDescriptors wires Descriptor (msgclass/name/lookup/each),
// EnumDescriptor (name/lookup_name/lookup_value) and FieldDescriptor
// (name/type/label/number).
func (vm *VM) registerProtobufDescriptors(descCls, enumDescCls, fieldDescCls *RClass) {
	descCls.define("msgclass", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.newProtobufMsgClass(self.(*ProtobufDescriptor).d.Msgclass())
	})
	descCls.define("name", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self.(*ProtobufDescriptor).d.Name())
	})
	descCls.define("lookup", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		fd := self.(*ProtobufDescriptor).d.Lookup(pbName(args[0]))
		if fd == nil {
			return object.NilV
		}
		return &ProtobufFieldDescriptor{cls: fieldDescCls, fd: fd}
	})
	descCls.define("each", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		self.(*ProtobufDescriptor).d.Each(func(fd *protobuf.FieldDescriptor) {
			vm.callBlock(blk, []object.Value{&ProtobufFieldDescriptor{cls: fieldDescCls, fd: fd}})
		})
		return self
	})

	enumDescCls.define("name", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self.(*ProtobufEnumDescriptor).ed.Name())
	})
	enumDescCls.define("lookup_name", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		n, ok := self.(*ProtobufEnumDescriptor).ed.LookupName(pbName(args[0]))
		if !ok {
			return object.NilV
		}
		return object.IntValue(int64(n))
	})
	enumDescCls.define("lookup_value", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		s, ok := self.(*ProtobufEnumDescriptor).ed.LookupValue(int(intArg(args[0])))
		if !ok {
			return object.NilV
		}
		return object.Symbol(string(s))
	})

	fieldDescCls.define("name", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self.(*ProtobufFieldDescriptor).fd.Name())
	})
	fieldDescCls.define("type", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Symbol(string(self.(*ProtobufFieldDescriptor).fd.Type()))
	})
	fieldDescCls.define("label", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Symbol(string(self.(*ProtobufFieldDescriptor).fd.Label()))
	})
	fieldDescCls.define("number", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self.(*ProtobufFieldDescriptor).fd.Number()))
	})
}

// registerProtobufMsgClass wires a generated message class (descriptor.msgclass):
// new (build an instance), name, descriptor, and Any.pack.
func (vm *VM) registerProtobufMsgClass(msgClassCls, descCls *RClass) {
	msgClassCls.define("new", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		m, err := self.(*ProtobufMsgClass).mc.New(vm.pbInitHash(args))
		if err != nil {
			raisePBError(err)
		}
		return vm.newProtobufMessage(m)
	})
	msgClassCls.define("name", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self.(*ProtobufMsgClass).mc.Name())
	})
	msgClassCls.define("descriptor", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return &ProtobufDescriptor{cls: descCls, d: self.(*ProtobufMsgClass).mc.Descriptor()}
	})
	// Google::Protobuf::Any.pack(msg) wraps msg in a new Any (type.googleapis.com/…).
	msgClassCls.define("pack", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		msg, ok := args[0].(*ProtobufMessage)
		if !ok {
			raise("TypeError", "Any.pack expects a message")
		}
		any, err := protobuf.AnyPack(msg.m)
		if err != nil {
			raisePBError(err)
		}
		return vm.newProtobufMessage(any)
	})
}

// registerProtobufMessage wires a message instance: the method_missing field
// accessors (msg.name / msg.name = v), respond_to_missing?, to_h, ==, dup, clone,
// inspect, to_s and the Any helpers unpack / is?.
func (vm *VM) registerProtobufMessage(msgCls *RClass) {
	msgCls.define("method_missing", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		m := self.(*ProtobufMessage).m
		name := pbName(args[0])
		if strings.HasSuffix(name, "=") {
			base := name[:len(name)-1]
			if err := m.Set(base, vm.rubyToPB(args[1])); err != nil {
				if _, unknown := err.(*protobuf.ArgumentError); unknown {
					raise("NoMethodError", "undefined method '%s=' for %s", base, m.Inspect())
				}
				raisePBError(err)
			}
			return args[1]
		}
		v, err := m.Get(name)
		if err != nil {
			raise("NoMethodError", "undefined method '%s' for %s", name, m.Inspect())
		}
		return vm.pbToRuby(v)
	})
	msgCls.define("respond_to_missing?", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		_, err := self.(*ProtobufMessage).m.Get(strings.TrimSuffix(pbName(args[0]), "="))
		return object.Bool(err == nil)
	})
	msgCls.define("to_h", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.pbToRuby(self.(*ProtobufMessage).m.ToH())
	})
	msgCls.define("==", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		other, ok := args[0].(*ProtobufMessage)
		if !ok {
			return object.False
		}
		return object.Bool(self.(*ProtobufMessage).m.Equal(other.m))
	})
	msgCls.define("dup", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.newProtobufMessage(self.(*ProtobufMessage).m.Dup())
	})
	msgCls.define("clone", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.newProtobufMessage(self.(*ProtobufMessage).m.Clone())
	})
	// inspect / to_s inherit Object's defaults, which call the value's Go
	// Inspect() / ToS() (both the library's msg.Inspect()); no Ruby override needed.
	msgCls.define("unpack", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		klass, ok := args[0].(*ProtobufMsgClass)
		if !ok {
			raise("TypeError", "Any#unpack expects a message class")
		}
		m, err := protobuf.AnyUnpack(self.(*ProtobufMessage).m, klass.mc)
		if err != nil {
			raisePBError(err)
		}
		return vm.newProtobufMessage(m)
	})
	msgCls.define("is?", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		klass, ok := args[0].(*ProtobufMsgClass)
		if !ok {
			return object.False
		}
		return object.Bool(protobuf.AnyIs(self.(*ProtobufMessage).m, klass.mc))
	})
}

// registerProtobufRepeated wires RepeatedField: the .new class method and the
// Enumerable-ish instance surface (push/<</[]/[]=/each/to_a/+/concat/==/clear/
// length/size/empty?/first/last/include?/dup/inspect/to_s).
func (vm *VM) registerProtobufRepeated(repeatedCls *RClass) {
	repeatedCls.smethods["new"] = &Method{name: "new", owner: repeatedCls,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			r, err := protobuf.NewRepeatedField(pbSym(args[0]), vm.pbArrayToGo(args, 1)...)
			if err != nil {
				raisePBError(err)
			}
			return &ProtobufRepeatedField{cls: repeatedCls, r: r}
		}}

	self := func(v object.Value) *protobuf.RepeatedField { return v.(*ProtobufRepeatedField).r }

	repeatedCls.define("push", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		vals := make([]any, len(args))
		for i, a := range args {
			vals[i] = vm.rubyToPB(a)
		}
		if err := self(v).Push(vals...); err != nil {
			raisePBError(err)
		}
		return v
	})
	repeatedCls.define("<<", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if err := self(v).Push(vm.rubyToPB(args[0])); err != nil {
			raisePBError(err)
		}
		return v
	})
	repeatedCls.define("[]", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.pbToRuby(self(v).At(int(intArg(args[0]))))
	})
	repeatedCls.define("[]=", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if err := self(v).SetAt(int(intArg(args[0])), vm.rubyToPB(args[1])); err != nil {
			raisePBError(err)
		}
		return args[1]
	})
	repeatedCls.define("each", func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		self(v).Each(func(e any) { vm.callBlock(blk, []object.Value{vm.pbToRuby(e)}) })
		return v
	})
	repeatedCls.define("to_a", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.pbToRuby(any(self(v).ToArray()))
	})
	repeatedCls.define("+", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		dup := self(v).Dup()
		if err := dup.Concat(vm.rubyToPB(args[0])); err != nil {
			raisePBError(err)
		}
		return &ProtobufRepeatedField{cls: repeatedCls, r: dup}
	})
	repeatedCls.define("concat", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if err := self(v).Concat(vm.rubyToPB(args[0])); err != nil {
			raisePBError(err)
		}
		return v
	})
	repeatedCls.define("==", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		other, ok := args[0].(*ProtobufRepeatedField)
		if !ok {
			return object.False
		}
		return object.Bool(self(v).Equal(other.r))
	})
	repeatedCls.define("clear", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		self(v).Clear()
		return v
	})
	lenFn := func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).Length()))
	}
	repeatedCls.define("length", lenFn)
	repeatedCls.define("size", lenFn)
	repeatedCls.define("empty?", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).Length() == 0)
	})
	repeatedCls.define("first", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.pbToRuby(self(v).At(0))
	})
	repeatedCls.define("last", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.pbToRuby(self(v).At(self(v).Length() - 1))
	})
	repeatedCls.define("include?", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		found := false
		self(v).Each(func(e any) {
			if vm.send(vm.pbToRuby(e), "==", []object.Value{args[0]}, nil).Truthy() {
				found = true
			}
		})
		return object.Bool(found)
	})
	repeatedCls.define("dup", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return &ProtobufRepeatedField{cls: repeatedCls, r: self(v).Dup()}
	})
	// inspect / to_s inherit Object's defaults (which call the value's Go
	// Inspect() / ToS(), both the library's list Inspect()).
}

// registerProtobufMap wires Map: the .new class method and the Hash-ish instance
// surface ([]/[]=/each/keys/values/to_h/==/length/size/delete/has_key?/key?/
// include?/clear/empty?/dup/inspect/to_s).
func (vm *VM) registerProtobufMap(mapCls *RClass) {
	mapCls.smethods["new"] = &Method{name: "new", owner: mapCls,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			m, err := protobuf.NewMap(pbSym(args[0]), pbSym(args[1]))
			if err != nil {
				raisePBError(err)
			}
			return &ProtobufMap{cls: mapCls, m: m}
		}}

	self := func(v object.Value) *protobuf.Map { return v.(*ProtobufMap).m }

	mapCls.define("[]", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		val, ok := self(v).Get(vm.rubyToPB(args[0]))
		if !ok {
			return object.NilV
		}
		return vm.pbToRuby(val)
	})
	mapCls.define("[]=", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if err := self(v).Set(vm.rubyToPB(args[0]), vm.rubyToPB(args[1])); err != nil {
			raisePBError(err)
		}
		return args[1]
	})
	mapCls.define("each", func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		self(v).Each(func(k, val any) {
			vm.callBlock(blk, []object.Value{vm.pbToRuby(k), vm.pbToRuby(val)})
		})
		return v
	})
	mapCls.define("keys", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.pbToRuby(any(self(v).Keys()))
	})
	mapCls.define("values", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.pbToRuby(any(self(v).Values()))
	})
	mapCls.define("to_h", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.pbToRuby(self(v).ToHash())
	})
	mapCls.define("==", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		other, ok := args[0].(*ProtobufMap)
		if !ok {
			return object.False
		}
		return object.Bool(self(v).Equal(other.m))
	})
	mapLenFn := func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).Length()))
	}
	mapCls.define("length", mapLenFn)
	mapCls.define("size", mapLenFn)
	mapCls.define("delete", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).Delete(vm.rubyToPB(args[0])))
	})
	hasFn := func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).Has(vm.rubyToPB(args[0])))
	}
	mapCls.define("has_key?", hasFn)
	mapCls.define("key?", hasFn)
	mapCls.define("include?", hasFn)
	mapCls.define("empty?", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).Length() == 0)
	})
	mapCls.define("clear", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		self(v).Clear()
		return v
	})
	mapCls.define("dup", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return &ProtobufMap{cls: mapCls, m: self(v).Dup()}
	})
	// inspect / to_s inherit Object's defaults (which call the value's Go
	// Inspect() / ToS(), both the library's map Inspect()).
}

// pbArrayToGo reads an optional Array argument at args[i] into the []any a
// standalone RepeatedField.new initialiser expects, translating each element
// through rubyToPB. A missing or non-Array argument yields nil (an empty field).
func (vm *VM) pbArrayToGo(args []object.Value, i int) []any {
	if i >= len(args) {
		return nil
	}
	arr, ok := args[i].(*object.Array)
	if !ok {
		return nil
	}
	out := make([]any, len(arr.Elems))
	for j, e := range arr.Elems {
		out[j] = vm.rubyToPB(e)
	}
	return out
}

// registerProtobufWKT pre-registers the well-known types as message-class
// constants of the module (Google::Protobuf::Timestamp.new, …), matching the
// gem's generated_pool. Every listed name is a valid well-known type, so the
// library resolves each to a non-nil class.
func (vm *VM) registerProtobufWKT(mod *RClass) {
	for _, name := range []string{
		"Timestamp", "Duration", "Any", "Struct", "Value", "ListValue", "FieldMask", "Empty",
		"DoubleValue", "FloatValue", "Int64Value", "UInt64Value", "Int32Value", "UInt32Value",
		"BoolValue", "StringValue", "BytesValue",
	} {
		mod.consts[name] = vm.newProtobufMsgClass(protobuf.WellKnownType(name))
	}
}

// registerProtobufModuleMethods wires the module one-shots
// Google::Protobuf.encode / decode / encode_json / decode_json.
func (vm *VM) registerProtobufModuleMethods(mod *RClass) {
	mod.smethods["encode"] = &Method{name: "encode", owner: mod,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			msg, ok := args[0].(*ProtobufMessage)
			if !ok {
				raise("TypeError", "encode expects a message")
			}
			b, err := protobuf.Encode(msg.m)
			if err != nil {
				raisePBError(err)
			}
			return object.NewStringBytesEnc(b, "ASCII-8BIT")
		}}
	mod.smethods["decode"] = &Method{name: "decode", owner: mod,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			klass, ok := args[0].(*ProtobufMsgClass)
			if !ok {
				raise("TypeError", "decode expects a message class")
			}
			m, err := protobuf.Decode(klass.mc, pbBytes(args[1]))
			if err != nil {
				raisePBError(err)
			}
			return vm.newProtobufMessage(m)
		}}
	mod.smethods["encode_json"] = &Method{name: "encode_json", owner: mod,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			msg, ok := args[0].(*ProtobufMessage)
			if !ok {
				raise("TypeError", "encode_json expects a message")
			}
			b, err := protobuf.EncodeJSON(msg.m, pbJSONOpts(args[1:]))
			if err != nil {
				raisePBError(err)
			}
			return object.NewString(string(b))
		}}
	mod.smethods["decode_json"] = &Method{name: "decode_json", owner: mod,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			klass, ok := args[0].(*ProtobufMsgClass)
			if !ok {
				raise("TypeError", "decode_json expects a message class")
			}
			m, err := protobuf.DecodeJSON(klass.mc, pbBytes(args[1]), pbJSONOpts(args[2:]))
			if err != nil {
				raisePBError(err)
			}
			return vm.newProtobufMessage(m)
		}}
}

// pbBytes reads the raw bytes of a String argument (the binary/JSON payload of
// decode / decode_json), raising TypeError for a non-String.
func pbBytes(v object.Value) []byte {
	s, ok := v.(*object.String)
	if !ok {
		raise("TypeError", "expected a String")
		return nil
	}
	return s.Bytes()
}

// pbJSONOpts reads the trailing keyword Hash of encode_json / decode_json into the
// library's JSONOptions: emit_defaults, preserve_proto_fieldnames and
// ignore_unknown_fields, mirroring the gem's options. An absent/non-Hash trailing
// argument yields the zero options.
func pbJSONOpts(rest []object.Value) protobuf.JSONOptions {
	o := protobuf.JSONOptions{}
	if len(rest) == 0 {
		return o
	}
	h, ok := rest[len(rest)-1].(*object.Hash)
	if !ok {
		return o
	}
	o.EmitDefaults = pbTruthyKey(h, "emit_defaults")
	o.PreserveProtoNames = pbTruthyKey(h, "preserve_proto_fieldnames")
	o.IgnoreUnknownFields = pbTruthyKey(h, "ignore_unknown_fields")
	return o
}

// pbTruthyKey reports whether the Symbol-keyed option key is present and truthy.
func pbTruthyKey(h *object.Hash, key string) bool {
	v, ok := h.Get(object.Symbol(key))
	return ok && v.Truthy()
}
