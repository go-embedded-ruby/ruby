package vm

import "github.com/go-embedded-ruby/ruby/internal/object"

// Binding captures a frame's local-variable environment, self and definee, so
// code can be eval'd against it later (Binding#eval, eval(str, binding)) and its
// locals inspected/mutated. names maps slot index → local name (a mutable copy,
// so local_variable_set can add a binding-only local without touching the ISeq).
type Binding struct {
	env     *Env
	self    object.Value
	definee *RClass
	file    string   // source file the binding was captured in ("" for compiled-in code)
	names   []string // slot index → local name (original ISeq locals, then injected ones)
	added   []string // names injected via local_variable_set/eval, in insertion order
}

func (b *Binding) ToS() string     { return "#<Binding>" }
func (b *Binding) Inspect() string { return "#<Binding>" }
func (b *Binding) Truthy() bool    { return true }

// slotOf returns the env slot of a named local, or -1.
func (b *Binding) slotOf(name string) int {
	for i, n := range b.names {
		if n == name {
			return i
		}
	}
	return -1
}

func (vm *VM) registerBinding() {
	cBinding := newClass("Binding", vm.cObject)
	vm.consts["Binding"] = cBinding

	// TOPLEVEL_BINDING is a Binding for the top level: its self is main and its
	// definee is Object, so TOPLEVEL_BINDING.eval("def m; end" / "private :m")
	// defines and sets visibility on Object exactly as top-level code does. It
	// starts with no locals of its own.
	vm.consts["TOPLEVEL_BINDING"] = &Binding{env: &Env{}, self: vm.main, definee: vm.cObject}

	cBinding.define("eval", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.bindingEval(self.(*Binding), args[0])
	})
	cBinding.define("receiver", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return self.(*Binding).self
	})
	// source_location reports [file, line] for where the binding was captured, or
	// nil when no source is known (compiled-in code such as the prelude, whose
	// ISeq carries no File). As with Proc#source_location the VM does not track
	// per-instruction line numbers, so the line is reported as 0 — the array shape
	// ([String, Integer]) is what callers depend on.
	cBinding.define("source_location", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		b := self.(*Binding)
		if b.file == "" {
			return object.NilV
		}
		return object.NewArray(object.NewString(b.file), object.IntValue(0))
	})
	// dup / clone return a shallow copy: the environment is shared (so a write to
	// an existing local through one copy is visible through the other, as MRI
	// does), but the name/injected lists are independent, so a local_variable_set
	// (or eval-created local) on the copy does not leak into the original.
	bindingDup := func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		b := self.(*Binding)
		return &Binding{
			env:     b.env,
			self:    b.self,
			definee: b.definee,
			file:    b.file,
			names:   append([]string(nil), b.names...),
			added:   append([]string(nil), b.added...),
		}
	}
	cBinding.define("dup", bindingDup)
	cBinding.define("clone", bindingDup)
	cBinding.define("local_variables", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		b := self.(*Binding)
		seen := map[string]bool{}
		var elems []object.Value
		add := func(n string) {
			if n != "" && !seen[n] { // skip anonymous slots (pattern subjects etc.)
				seen[n] = true
				elems = append(elems, object.Symbol(n))
			}
		}
		// MRI lists local_variable_set-injected locals first (most-recent first),
		// then the binding's original locals in slot order.
		for i := len(b.added) - 1; i >= 0; i-- {
			add(b.added[i])
		}
		for _, n := range b.names[:len(b.names)-len(b.added)] {
			add(n)
		}
		return object.NewArrayFromSlice(elems)
	})
	cBinding.define("local_variable_get", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		b := self.(*Binding)
		name := vm.bindingVarName(args[0])
		i := b.slotOf(name)
		if i < 0 {
			raise("NameError", "local variable '%s' is not defined for %s", name, b.ToS())
		}
		return b.env.slots[i]
	})
	cBinding.define("local_variable_set", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		b := self.(*Binding)
		name := vm.bindingVarName(args[0])
		if i := b.slotOf(name); i >= 0 {
			b.env.slots[i] = args[1]
		} else {
			// A new binding-local: extend the name map, the environment and the
			// injected-locals list (which local_variables surfaces first).
			b.names = append(b.names, name)
			b.added = append(b.added, name)
			b.env.slots = append(b.env.slots, args[1])
		}
		return args[1]
	})
	cBinding.define("local_variable_defined?", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(self.(*Binding).slotOf(vm.bindingVarName(args[0])) >= 0)
	})
}

// bindingVarName coerces a local-variable name argument to a Go string: a Symbol
// or String directly, otherwise an object responding to #to_str (MRI's implicit
// String conversion). Anything else raises TypeError with MRI's message.
func (vm *VM) bindingVarName(v object.Value) string {
	switch n := v.(type) {
	case object.Symbol:
		return string(n)
	case *object.String:
		return n.Str()
	}
	if vm.respondsToDynamic(v, "to_str") {
		if s, ok := vm.send(v, "to_str", nil, nil).(*object.String); ok {
			return s.Str()
		}
	}
	raise("TypeError", "%s is not a symbol nor a string", v.Inspect())
	return ""
}

// bindingEval lives in binding_eval_open.go / binding_eval_closed.go: it needs
// the front-end (CompileWithLocals), so a closed-world build stubs it out.
