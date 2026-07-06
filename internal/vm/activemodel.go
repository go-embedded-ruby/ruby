// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	activemodel "github.com/go-ruby-activemodel/activemodel"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// ASModelName is the Ruby wrapper around an activemodel.Name — the bundle of
// conventional names (singular/plural/element/human/collection/param_key/…) Rails
// derives from a model class name via ActiveModel::Naming. The pure derivation
// (through go-ruby-activesupport's Inflector, byte-for-byte with Rails) lives in
// the github.com/go-ruby-activemodel/activemodel library; this is the thin shell
// exposing it to rbgo.
type ASModelName struct{ n activemodel.Name }

func (m *ASModelName) ToS() string     { return m.n.Name }
func (m *ASModelName) Inspect() string { return "#<ActiveModel::Name " + m.n.Name + ">" }
func (m *ASModelName) Truthy() bool    { return true }

// ASModelErrors is the Ruby wrapper around an *activemodel.Errors — the ordered
// collection of a model's validation errors (ActiveModel::Errors): #add,
// #full_messages, #messages, #details, #where, #added? and friends.
type ASModelErrors struct {
	e     *activemodel.Errors
	model *amModel
}

func (e *ASModelErrors) ToS() string     { return "#<ActiveModel::Errors>" }
func (e *ASModelErrors) Inspect() string { return e.ToS() }
func (e *ASModelErrors) Truthy() bool    { return true }

// ASModelError is the Ruby wrapper around an *activemodel.Error — a single
// validation error bound to an attribute (ActiveModel::Error): #attribute, #type,
// #message, #full_message, #options, #details.
type ASModelError struct{ e *activemodel.Error }

func (e *ASModelError) ToS() string { return e.e.FullMessage() }
func (e *ASModelError) Inspect() string {
	return "#<ActiveModel::Error attribute=" + e.e.Attribute() + ">"
}
func (e *ASModelError) Truthy() bool { return true }

// registerActiveModel installs the ActiveModel module (require "active_model"):
// the Validations mixin (`include ActiveModel::Validations` brings in the
// class-level validates / validate / validates_each / validates_with DSL and the
// instance #valid? / #invalid? / #errors), the Errors + Error collection it
// populates, the Name / Naming naming layer, and the Validator / EachValidator
// base contracts. The validation engine (the standard validators, the message
// table and its %{...} interpolation, Naming's inflections) lives in the
// github.com/go-ruby-activemodel/activemodel library; this binding wires its two
// host seams — attribute access and method dispatch (Attr/Dispatcher, combined as
// Model) plus if:/unless: conditions — onto rbgo's object model, running every
// Ruby validator block inline under the GVL.
func (vm *VM) registerActiveModel() {
	mod := newClass("ActiveModel", nil)
	mod.isModule = true
	vm.consts["ActiveModel"] = mod

	vm.registerActiveModelName(mod)
	vm.registerActiveModelNaming(mod)
	vm.registerActiveModelErrorsClasses(mod)
	vm.registerActiveModelValidators(mod)
	vm.registerActiveModelValidations(mod)
}

// registerActiveModelName installs ActiveModel::Name and its readers.
func (vm *VM) registerActiveModelName(mod *RClass) {
	cls := newClass("ActiveModel::Name", vm.cObject)
	mod.consts["Name"] = cls
	vm.consts["ActiveModel::Name"] = cls

	// Name.new(klass_or_name, namespace = nil, name = nil): mirror
	// ActiveModel::Name.new(klass, namespace, name), deriving the class name from a
	// Class (or a String/Symbol) and an optional namespace module.
	cls.smethods["new"] = &Method{name: "new", owner: cls, native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..3)")
		}
		name := vm.amClassName(args[0])
		namespace := ""
		if len(args) > 1 && args[1].Truthy() {
			namespace = vm.amClassName(args[1])
		}
		if len(args) > 2 && args[2].Truthy() {
			name = arStr(args[2])
		}
		return &ASModelName{n: activemodel.NewNamespacedName(name, namespace)}
	}}

	self := func(v object.Value) activemodel.Name { return v.(*ASModelName).n }
	str := func(name string, get func(activemodel.Name) string) {
		cls.define(name, func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
			return object.NewString(get(self(v)))
		})
	}
	str("name", func(n activemodel.Name) string { return n.Name })
	str("to_s", func(n activemodel.Name) string { return n.Name })
	str("to_str", func(n activemodel.Name) string { return n.Name })
	str("singular", func(n activemodel.Name) string { return n.Singular })
	str("plural", func(n activemodel.Name) string { return n.Plural })
	str("element", func(n activemodel.Name) string { return n.Element })
	str("human", func(n activemodel.Name) string { return n.Human })
	str("collection", func(n activemodel.Name) string { return n.Collection })
	str("param_key", func(n activemodel.Name) string { return n.ParamKey })
	str("route_key", func(n activemodel.Name) string { return n.RouteKey })
	str("singular_route_key", func(n activemodel.Name) string { return n.SingularRouteKey })
	// i18n_key is a Symbol in Rails.
	cls.define("i18n_key", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Symbol(self(v).I18nKey)
	})
}

// registerActiveModelNaming installs ActiveModel::Naming: the module functions
// (singular / plural / param_key / route_key / singular_route_key / uncountable?)
// plus the model_name class method installed on any class that does
// `extend ActiveModel::Naming`.
func (vm *VM) registerActiveModelNaming(mod *RClass) {
	naming := newClass("ActiveModel::Naming", nil)
	naming.isModule = true
	mod.consts["Naming"] = naming
	vm.consts["ActiveModel::Naming"] = naming

	// extend ActiveModel::Naming installs a memoization-free model_name reader.
	naming.smethods["extended"] = &Method{name: "extended", owner: naming, native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		base, ok := args[0].(*RClass)
		if !ok {
			return object.NilV
		}
		base.smethods["model_name"] = &Method{name: "model_name", owner: base, native: func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
			return &ASModelName{n: vm.amNameFor(self.(*RClass))}
		}}
		return object.NilV
	}}

	sm := func(name string, get func(activemodel.Name) string) {
		naming.smethods[name] = &Method{name: name, owner: naming, native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			if len(args) == 0 {
				raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
			}
			return object.NewString(get(vm.amNameOf(args[0])))
		}}
	}
	sm("singular", func(n activemodel.Name) string { return n.Singular })
	sm("plural", func(n activemodel.Name) string { return n.Plural })
	sm("param_key", func(n activemodel.Name) string { return n.ParamKey })
	sm("route_key", func(n activemodel.Name) string { return n.RouteKey })
	sm("singular_route_key", func(n activemodel.Name) string { return n.SingularRouteKey })
	naming.smethods["uncountable?"] = &Method{name: "uncountable?", owner: naming, native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		n := vm.amNameOf(args[0])
		return object.Bool(n.Singular == n.Plural)
	}}
}

// registerActiveModelValidators installs the ActiveModel::Validator base contract
// and its ActiveModel::EachValidator refinement: a custom validator subclasses
// one, stores its options, and overrides #validate (whole record) or #validate_each
// (per attribute); the base bodies raise NotImplementedError as in Rails.
func (vm *VM) registerActiveModelValidators(mod *RClass) {
	validator := newClass("ActiveModel::Validator", vm.cObject)
	mod.consts["Validator"] = validator
	vm.consts["ActiveModel::Validator"] = validator

	validator.define("initialize", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		opts := object.Value(object.NewHash())
		if len(args) > 0 {
			opts = args[0]
		}
		setIvar(self, "@options", opts)
		return object.NilV
	})
	validator.define("options", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return getIvar(self, "@options")
	})
	validator.define("validate", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		raise("NotImplementedError", "Subclasses must implement a validate(record) method.")
		return object.NilV
	})

	each := newClass("ActiveModel::EachValidator", validator)
	mod.consts["EachValidator"] = each
	vm.consts["ActiveModel::EachValidator"] = each
	each.define("validate_each", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		raise("NotImplementedError", "Subclasses must implement a validate_each(record, attribute, value) method")
		return object.NilV
	})
}

// registerActiveModelValidations installs the ActiveModel::Validations mixin. Its
// instance methods live on the module (reached through the include chain); its
// class-level DSL is installed on each includer by the included hook.
func (vm *VM) registerActiveModelValidations(mod *RClass) {
	val := newClass("ActiveModel::Validations", nil)
	val.isModule = true
	mod.consts["Validations"] = val
	vm.consts["ActiveModel::Validations"] = val

	// #valid?(context = nil): run validation, returning whether no errors were added.
	val.define("valid?", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		ok, _ := vm.amValidate(self, amContext(args))
		return object.Bool(ok)
	})
	// #validate is an alias of #valid? (ActiveModel::Validations).
	val.define("validate", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		ok, _ := vm.amValidate(self, amContext(args))
		return object.Bool(ok)
	})
	// #invalid?(context = nil): the negation of #valid?.
	val.define("invalid?", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		ok, _ := vm.amValidate(self, amContext(args))
		return object.Bool(!ok)
	})
	// #errors: the memoized ActiveModel::Errors for the instance.
	val.define("errors", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.amErrorsFor(self)
	})
	// #read_attribute_for_validation(key): ActiveModel's attribute reader, the
	// reader method when defined, else the @key instance variable.
	val.define("read_attribute_for_validation", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		name := arStr(args[0])
		if vm.respondsTo(self, name) {
			return vm.send(self, name, nil, nil)
		}
		return getIvar(self, "@"+name)
	})

	// included(base): install the class-level DSL on the class that includes the
	// mixin (ActiveModel::Validations::ClassMethods). The include machinery always
	// hands the hook the including class.
	val.smethods["included"] = &Method{name: "included", owner: val, native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		vm.amInstallValidationsDSL(args[0].(*RClass))
		return object.NilV
	}}
}

// amContext reads the optional validation-context argument (a Symbol/String) into
// the context string; absent (or nil) means the default context.
func amContext(args []object.Value) string {
	if len(args) > 0 && args[0].Truthy() {
		return arStr(args[0])
	}
	return ""
}

// amInstallValidationsDSL installs the class-level validates / validate /
// validates_each / validates_with DSL on a class that includes
// ActiveModel::Validations, recording each declaration as a thunk replayed at
// #valid? time.
func (vm *VM) amInstallValidationsDSL(base *RClass) {
	sm := func(name string, fn NativeFn) {
		base.smethods[name] = &Method{name: name, owner: base, native: fn}
	}

	// validates(*attrs, **options): register the configured standard validators.
	sm("validates", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		cls := self.(*RClass)
		attrs, opts := amAttrsAndOpts(args)
		if len(attrs) == 0 {
			raise("ArgumentError", "You need to supply at least one attribute")
		}
		o := vm.amBuildOptions(opts)
		vm.amAddThunk(cls, func(v *activemodel.Validations) { v.Validates(attrs, o) })
		return object.NilV
	})

	// validate(*methods, **options, &block): register a whole-record custom
	// validation from method names and/or a block.
	sm("validate", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		cls := self.(*RClass)
		names, opts := amAttrsAndOpts(args)
		conds := vm.amBuildConditions(opts)
		if blk == nil && len(names) == 0 {
			raise("ArgumentError", "You must inform at least one validation method or supply a block")
		}
		if blk != nil {
			b := blk
			vm.amAddThunk(cls, func(v *activemodel.Validations) {
				v.Validate(conds, func(m activemodel.Model, e *activemodel.Errors) {
					rm := m.(*amModel)
					vm.amExposeErrors(rm, e)
					vm.callBlockSelf(b, rm.inst, []object.Value{rm.inst})
				})
			})
		}
		for _, nm := range names {
			name := nm
			vm.amAddThunk(cls, func(v *activemodel.Validations) {
				v.Validate(conds, func(m activemodel.Model, e *activemodel.Errors) {
					rm := m.(*amModel)
					vm.amExposeErrors(rm, e)
					vm.send(rm.inst, name, nil, nil)
				})
			})
		}
		return object.NilV
	})

	// validates_each(*attrs, **options, &block): run the block once per attribute,
	// yielding (record, attribute, value).
	sm("validates_each", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		cls := self.(*RClass)
		attrs, opts := amAttrsAndOpts(args)
		if blk == nil {
			raise("ArgumentError", "validates_each requires a block")
		}
		conds := vm.amBuildConditions(opts)
		b := blk
		vm.amAddThunk(cls, func(v *activemodel.Validations) {
			v.ValidatesEach(attrs, conds, func(m activemodel.Model, e *activemodel.Errors, attribute string, value any) {
				rm := m.(*amModel)
				vm.amExposeErrors(rm, e)
				vm.callBlockSelf(b, rm.inst, []object.Value{rm.inst, object.Symbol(attribute), rubyOfGo(value)})
			})
		})
		return object.NilV
	})

	// validates_with(validator_class, **options): register a whole-record custom
	// Validator, instantiated with the options and sent #validate(record).
	sm("validates_with", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		cls := self.(*RClass)
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..)")
		}
		vcls, ok := args[0].(*RClass)
		if !ok {
			raise("ArgumentError", "validates_with expects a validator class")
		}
		var optsHash *object.Hash
		var opts object.Value = object.NilV
		if len(args) > 1 {
			if h, ok := args[1].(*object.Hash); ok {
				optsHash, opts = h, h
			}
		}
		conds := vm.amBuildConditions(optsHash)
		vm.amAddThunk(cls, func(v *activemodel.Validations) {
			v.ValidatesWith(&amRubyValidator{vm: vm, cls: vcls, opts: opts}, conds)
		})
		return object.NilV
	})
}

// registerActiveModelErrorsClasses installs ActiveModel::Errors and
// ActiveModel::Error — the value objects a validation produces.
func (vm *VM) registerActiveModelErrorsClasses(mod *RClass) {
	vm.registerActiveModelErrorClass(mod)
	vm.registerActiveModelErrorsClass(mod)
}

// registerActiveModelErrorClass installs ActiveModel::Error (a single error).
func (vm *VM) registerActiveModelErrorClass(mod *RClass) {
	cls := newClass("ActiveModel::Error", vm.cObject)
	mod.consts["Error"] = cls
	vm.consts["ActiveModel::Error"] = cls

	self := func(v object.Value) *activemodel.Error { return v.(*ASModelError).e }
	cls.define("attribute", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Symbol(self(v).Attribute())
	})
	cls.define("type", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return rubyOfGo(self(v).Type())
	})
	cls.define("message", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).Message())
	})
	cls.define("full_message", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).FullMessage())
	})
	cls.define("options", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return amRubyOptions(self(v).Options())
	})
	cls.define("details", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return amRubyDetail(self(v).Details())
	})
}

// registerActiveModelErrorsClass installs ActiveModel::Errors (the collection).
func (vm *VM) registerActiveModelErrorsClass(mod *RClass) {
	cls := newClass("ActiveModel::Errors", vm.cObject)
	mod.consts["Errors"] = cls
	vm.consts["ActiveModel::Errors"] = cls

	handle := func(v object.Value) *ASModelErrors { return v.(*ASModelErrors) }
	self := func(v object.Value) *activemodel.Errors { return handle(v).e }

	// add(attribute, type = :invalid, **options): append an error and return it.
	cls.define("add", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..)")
		}
		attr := arStr(args[0])
		var typ object.Value = object.NilV
		if len(args) > 1 {
			typ = args[1]
		}
		return &ASModelError{e: self(v).Add(attr, vm.amErrorType(typ), amOptsMap(amTail(args, 2)))}
	})
	cls.define("empty?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).Empty())
	})
	cls.define("blank?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).Empty())
	})
	cls.define("any?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).Any())
	})
	cls.define("size", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).Size()))
	})
	cls.define("count", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).Size()))
	})
	cls.define("clear", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		self(v).Clear()
		return v
	})
	cls.define("full_messages", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return arStrings(self(v).FullMessages())
	})
	cls.define("full_messages_for", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		return arStrings(self(v).FullMessagesFor(arStr(args[0])))
	})
	cls.define("messages_for", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		return arStrings(self(v).MessagesFor(arStr(args[0])))
	})
	cls.define("[]", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		return arStrings(self(v).Get(arStr(args[0])))
	})
	cls.define("include?", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		return object.Bool(self(v).Include(arStr(args[0])))
	})
	cls.define("has_key?", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		return object.Bool(self(v).Include(arStr(args[0])))
	})
	cls.define("key?", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		return object.Bool(self(v).Include(arStr(args[0])))
	})
	cls.define("attribute_names", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return amSymArray(self(v).AttributeNames())
	})
	cls.define("messages", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		h := object.NewHash()
		for attr, msgs := range self(v).Messages() {
			h.Set(object.Symbol(attr), arStrings(msgs))
		}
		return h
	})
	cls.define("details", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		h := object.NewHash()
		for attr, ds := range self(v).Details() {
			arr := object.NewArrayFromSlice(make([]object.Value, len(ds)))
			for i, d := range ds {
				arr.Elems[i] = amRubyDetail(d)
			}
			h.Set(object.Symbol(attr), arr)
		}
		return h
	})
	cls.define("added?", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..)")
		}
		attr, typ, opts := vm.amMatchArgs(args)
		return object.Bool(self(v).Added(attr, typ, opts))
	})
	cls.define("of_kind?", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..)")
		}
		attr, typ, _ := vm.amMatchArgs(args)
		return object.Bool(self(v).OfKind(attr, typ))
	})
	cls.define("where", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..)")
		}
		attr, typ, opts := vm.amMatchArgs(args)
		found := self(v).Where(attr, typ, opts)
		arr := object.NewArrayFromSlice(make([]object.Value, len(found)))
		for i, e := range found {
			arr.Elems[i] = &ASModelError{e: e}
		}
		return arr
	})
	cls.define("each", func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("ArgumentError", "no block given")
		}
		self(v).Each(func(e *activemodel.Error) {
			vm.callBlock(blk, []object.Value{&ASModelError{e: e}})
		})
		return v
	})
}

// amMatchArgs reads the (attribute, type, options) arguments shared by
// Errors#added? / #of_kind? / #where.
func (vm *VM) amMatchArgs(args []object.Value) (string, any, map[string]any) {
	attr := arStr(args[0])
	var typ any
	if len(args) > 1 {
		typ = vm.amErrorType(args[1])
	}
	return attr, typ, amOptsMap(amTail(args, 2))
}

// amRubyOptions renders an error's options map as a Ruby Hash keyed by Symbol,
// with :message surfaced as a String (a Symbol/proc message is dropped, as its
// text is what #message renders).
func amRubyOptions(opts map[string]any) object.Value {
	h := object.NewHash()
	for k, v := range opts {
		if k == "message" {
			if s, ok := v.(string); ok {
				h.Set(object.Symbol(k), object.NewString(s))
			}
			continue
		}
		h.Set(object.Symbol(k), rubyOfGo(v))
	}
	return h
}

// amRubyDetail renders an ActiveModel::Error#details hash ({error: type, ...}) as a
// Ruby Hash keyed by Symbol.
func amRubyDetail(d map[string]any) object.Value {
	h := object.NewHash()
	for k, v := range d {
		h.Set(object.Symbol(k), rubyOfGo(v))
	}
	return h
}
