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
	line    int      // 1-based line the binding was captured at (MRI's bind->first_lineno)
	names   []string // slot index → local name (original ISeq locals, then injected ones)
	added   []string // names injected via local_variable_set/eval, in insertion order

	// method is the __method__/__callee__ pair of the frame the binding was
	// captured in, so eval'd code reports the enclosing method. MRI reaches the
	// same answer from the binding's own frame (rb_vm_frame_method_entry on the
	// cfp the binding pins); rbgo records the pair the frame already carries.
	method frameMethod
}

// toplevelBindingFile is the path MRI reports for TOPLEVEL_BINDING: the binding
// is made before any script frame exists, so bind_location has no real path and
// pathobj_path yields the "<main>" label. Measured on ruby 4.0.5 for both a
// script and -e: TOPLEVEL_BINDING.source_location == ["<main>", 0].
const toplevelBindingFile = "<main>"

// newToplevelBinding builds a Binding for the top level: its self is main and its
// definee is Object, so TOPLEVEL_BINDING.eval("def m; end" / "private :m")
// defines and sets visibility on Object exactly as top-level code does. It starts
// with no locals of its own.
func (vm *VM) newToplevelBinding() *Binding {
	return &Binding{env: &Env{}, self: vm.main, definee: vm.cObject, file: toplevelBindingFile}
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

	vm.consts["TOPLEVEL_BINDING"] = vm.newToplevelBinding()

	// Kernel#binding is a METHOD in MRI, not an instruction: rb_f_binding is
	// installed by rb_define_global_function("binding", rb_f_binding, 0) (proc.c
	// ruby_4_0:4726) and its body is rb_binding_new() -> rb_vm_make_binding(ec,
	// ec->cfp) (proc.c ruby_4_0:328-333). A CFUNC frame cannot make a binding, so
	// rb_vm_get_binding_creatable_next_cfp walks past it to the calling Ruby frame
	// — which in rbgo is simply the top of the frame stack, since a native pushes
	// no frame. Being a method is what makes obj.send(:binding) work; it used to
	// raise NoMethodError because the only way to reach a Binding was the bareword
	// intrinsic the compiler lowers to OpBinding.
	//
	// rb_define_global_function is the module-function form, so the Object-side
	// copy is PRIVATE: `obj.binding` raises "private method 'binding' called for
	// …" on ruby 4.0.5, and marking it here is what reproduces that.
	cObject := vm.cObject
	cObject.define("binding", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) != 0 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 0)", len(args))
		}
		return vm.callerBinding()
	})
	if m := cObject.methods["binding"]; m != nil {
		m.vis = visPrivate
	}

	// Binding#eval(src [, file [, line]]) is bind_eval (proc.c ruby_4_0:402): it
	// scans "12" and then calls rb_f_eval with the binding inserted as the scope,
	// so its optional filename and first line are eval's — and an absent filename
	// takes the same "(eval at FILE:LINE)" default, naming the CALLER of
	// Binding#eval (a native pushes no frame, so that is the innermost one).
	cBinding.define("eval", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 1 || len(args) > 3 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 1..3)", len(args))
		}
		src := vm.coerceFormatString(args[0])
		var fileArg, lineArg object.Value
		if len(args) >= 2 {
			fileArg = args[1]
		}
		if len(args) >= 3 {
			lineArg = args[2]
		}
		// nilFileAllowed is false: bind_eval splices the binding into rb_f_eval's
		// argv, so an explicit nil filename reaches StringValue there and raises,
		// exactly as eval(src, b, nil) does.
		return vm.bindingEval(self.(*Binding), src, vm.evalLoc(fileArg, lineArg, false))
	})
	cBinding.define("receiver", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return self.(*Binding).self
	})
	// source_location reports [file, line] for where the binding was captured, or
	// nil when no source is known (compiled-in code such as the prelude, whose
	// ISeq carries no File). This is bind_location (proc.c ruby_4_0:805-815): a
	// pure read of the two capture-time fields, pathobj_path(bind->pathobj) and
	// INT2FIX(bind->first_lineno) — no stack walk, so it keeps reporting the
	// CREATION site however far the frame has since run on. rbgo reported 0 for the
	// line because the Binding recorded none; the frame's live pc now supplies it
	// (frameBinding), the same pc a backtrace resolves.
	cBinding.define("source_location", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		b := self.(*Binding)
		if b.file == "" {
			return object.NilV
		}
		return object.NewArray(object.NewString(b.file), object.IntValue(int64(b.line)))
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
			line:    b.line,
			method:  b.method,
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
