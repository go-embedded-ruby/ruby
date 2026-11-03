package vm

import (
	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/object"
)

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
	names   []string // name index → local name (this frame's locals, then the enclosing scopes', then injected ones)
	added   []string // names injected via local_variable_set/eval, in insertion order

	// locs places each entry of names in the ENV CHAIN: names[i] lives at
	// locs[i].slot of env.ancestor(locs[i].depth). A nil locs means the identity
	// map — every name at depth 0, slot == index — which is what a binding with no
	// enclosing scope has and what every Binding was before this field existed.
	//
	// The chain is the whole of the block-scope defect. MRI's binding pins an ep,
	// and the ep chain out to the method's is intact, so eval_make_iseq ->
	// pm_eval_make_iseq builds ONE COMPILE SCOPE PER parent_iseq
	// (vm_eval.c ruby_4_0:1702-1732: `do { scopes_count++; } while ((iseq =
	// ISEQ_BODY(iseq)->parent_iseq));`) and a name found two scopes out compiles to
	// a depth-2 reference into the LIVE env. rbgo recorded only the innermost
	// frame's locals, so `[1].map { eval("y") }` could not see the y beside the
	// map, and `binding.local_variables` there answered [] — a wrong answer with no
	// exception.
	locs []bindLoc

	// block is the block in scope at the capture site — MRI's bind->block, whose
	// ep is what an eval run through this binding inherits. It is why
	// `def m; eval("yield"); end; m { 7 }` is 7 and `[1].map { eval("block_given?") }`
	// inside a method with a block is [true] on ruby 4.0.5: the eval frame is not a
	// fresh block-less scope, it continues the captured one.
	block *Proc

	// implicit maps an IMPLICIT parameter's name — `it`, or `_1`..`_N` — to its
	// slot in env, the binding's own (depth 0) scope. Only depth 0: MRI reads
	// these with get_local_variable_ptr(&env, lid, FALSE), where the final
	// argument is "search the parent scopes" and is TRUE for every local-variable
	// method and FALSE for every implicit-parameter one (proc.c v4.0.9:688, 734,
	// 782). That single flag is the whole of "ignores a numbered parameter
	// defined in a parent scope", which ruby/spec pins four times over.
	//
	// nil for a scope with none, which is every scope that is not a block whose
	// parameters the parser synthesised.
	implicit map[string]int

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

// bindLoc is one name's place in a Binding's environment chain: how many parent
// links out, and which slot there. It is MRI's (level, index) operand pair, the
// one getlocal/setlocal carry.
type bindLoc struct{ depth, slot int }

// slotOf returns the NAME INDEX of a local, or -1. The index is not a slot: use
// at() to reach the value, since a name from an enclosing scope lives in an
// ancestor env. The first match wins, which is what makes an inner local SHADOW
// an outer one of the same name — collectBindingLocals adds inner first, and
// MRI's local_var_list is a hash keyed by name for the same reason.
func (b *Binding) slotOf(name string) int {
	for i, n := range b.names {
		if n == name {
			return i
		}
	}
	return -1
}

// at resolves name index i to the environment holding it and the slot within it.
func (b *Binding) at(i int) (*Env, int) {
	if b.locs == nil {
		return b.env, i
	}
	l := b.locs[i]
	return b.env.ancestor(l.depth), l.slot
}

// ensureLocs materialises the identity map so a later append can extend it. A
// Binding built without one (the top level's, Proc#binding's) has names and slots
// in step; once ANY entry is appended the two can no longer be assumed aligned,
// so the invariant len(locs) == len(names) has to start holding before the first
// append rather than after it.
func (b *Binding) ensureLocs() {
	if b.locs != nil || len(b.names) == 0 {
		return
	}
	b.locs = make([]bindLoc, len(b.names))
	for i := range b.names {
		b.locs[i] = bindLoc{slot: i}
	}
}

// addLocal appends a binding-only local (local_variable_set, or a name an eval
// string declares at its top scope) to the binding's innermost env — depth 0,
// which is the scope MRI's vm_bind_update_env extends.
func (b *Binding) addLocal(name string, v object.Value) {
	b.ensureLocs()
	b.env.slots = append(b.env.slots, v)
	b.names = append(b.names, name)
	b.added = append(b.added, name)
	b.locs = append(b.locs, bindLoc{slot: len(b.env.slots) - 1})
}

// retargetLocals moves a compiled eval body's references to the binding's locals
// from the ONE borrowed compile scope the front end builds onto the binding's
// real environment chain.
//
// MRI never needs this step: pm_eval_make_iseq declares one parser scope per
// parent_iseq and hands the parser the names at each level, so the compiler
// itself emits getlocal/setlocal with the right (level, index) — vm_eval.c
// ruby_4_0:1702-1760. rbgo's CompileEvalWithLocals builds a SINGLE synthetic
// parent holding a flat name list, so every binding local compiles to depth 1.
// The RUNTIME chain is already correct — the eval frame's env.parent IS the
// binding's env, whose own parent is the enclosing scope — so only the operands
// are wrong, and they are wrong by exactly the name's depth.
//
// The rewrite is total and local: at tree level k inside the eval (the eval body
// itself is 0), depth k reaches the eval body's own env and depth k+1 is the
// borrowed scope — the deepest reference the compiler can emit there, since the
// borrowed scope is the outermost one it knows. So `B == k+1` identifies a
// reference to the binding and nothing else. A child that BREAKS the env chain (a
// `def` or a class body written inside the eval) cannot reach past itself, so its
// instructions never carry a depth that high and descending into it with k+1
// rewrites nothing — which is why the walk does not need to tell a block child
// from a method child.
func (b *Binding) retargetLocals(iseq *bytecode.ISeq) {
	if b.locs == nil {
		return
	}
	// The identity map is what a binding with no enclosing scope has, and it is
	// the overwhelmingly common one: skip the walk rather than rewrite operands
	// to the values they already hold.
	identity := true
	for i, l := range b.locs {
		if l.depth != 0 || l.slot != i {
			identity = false
			break
		}
	}
	if identity {
		return
	}
	b.retargetLevel(iseq, 0)
}

func (b *Binding) retargetLevel(iseq *bytecode.ISeq, k int) {
	for i := range iseq.Insns {
		in := &iseq.Insns[i]
		if in.Op != bytecode.OpGetLocal && in.Op != bytecode.OpSetLocal {
			continue
		}
		if in.B != k+1 || in.A < 0 || in.A >= len(b.locs) {
			continue
		}
		l := b.locs[in.A]
		in.A = l.slot
		in.B += l.depth
	}
	for _, c := range iseq.Children {
		b.retargetLevel(c, k+1)
	}
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
			block:   b.block,
			file:    b.file,
			line:    b.line,
			method:  b.method,
			names:   append([]string(nil), b.names...),
			locs:    append([]bindLoc(nil), b.locs...),
			added:   append([]string(nil), b.added...),
			// Shared by value, not by reference: nothing mutates the map after
			// capture, but a copy keeps that true if anything ever does.
			implicit: copyImplicit(b.implicit),
		}
	}
	cBinding.define("dup", bindingDup)
	cBinding.define("clone", bindingDup)
	cBinding.define("local_variables", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return self.(*Binding).localVariableNames()
	})
	cBinding.define("local_variable_get", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		b := self.(*Binding)
		name := vm.bindingVarName(args[0])
		refuseNumberedParam(name)
		i := b.slotOf(name)
		if i < 0 {
			raise("NameError", "local variable '%s' is not defined for %s", name, b.ToS())
		}
		e, slot := b.at(i)
		return e.slots[slot]
	})
	cBinding.define("local_variable_set", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		b := self.(*Binding)
		name := vm.bindingVarName(args[0])
		refuseNumberedParam(name)
		if i := b.slotOf(name); i >= 0 {
			// The write lands where the name LIVES, which for a binding taken inside a
			// block is an ANCESTOR env. Writing b.env.slots[i] instead put the value in
			// the block's own frame under an index that named something else there, so
			// `[1].each { b = binding }; b.local_variable_set(:q, 43)` left q at 42 and
			// raised nothing — one of this defect's silent faces.
			e, slot := b.at(i)
			e.slots[slot] = args[1]
		} else {
			// A new binding-local: extend the name map, the environment and the
			// injected-locals list (which local_variables surfaces first).
			b.addLocal(name, args[1])
		}
		return args[1]
	})
	cBinding.define("local_variable_defined?", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		name := vm.bindingVarName(args[0])
		// ⛔ RAISES, it does not answer false. MRI puts the refusal before the
		// lookup (proc.c v4.0.9:649), so this is not "no such local" — it is
		// "that is not the kind of question this method answers".
		refuseNumberedParam(name)
		return object.Bool(self.(*Binding).slotOf(name) >= 0)
	})

	// Ruby 4.0's implicit-parameter trio (proc.c v4.0.9:680-783). They are the
	// other half of taking `_1` out of #local_variables: the implicit parameters
	// do not vanish, they move to an API of their own.
	//
	// bind_implicit_parameters answers [:it] when `it` is in scope and the
	// numbered parameters otherwise — never a mixture, because a block cannot
	// have both.
	cBinding.define("implicit_parameters", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) != 0 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 0)", len(args))
		}
		return self.(*Binding).implicitParameterNames()
	})
	cBinding.define("implicit_parameter_get", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) != 1 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 1)", len(args))
		}
		b := self.(*Binding)
		name := vm.implicitParamName(args[0])
		slot, ok := b.implicit[name]
		if !ok {
			raise("NameError", "implicit parameter '%s' is not defined for %s", name, b.ToS())
		}
		return b.env.slots[slot]
	})
	cBinding.define("implicit_parameter_defined?", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) != 1 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 1)", len(args))
		}
		b := self.(*Binding)
		_, ok := b.implicit[vm.implicitParamName(args[0])]
		return object.Bool(ok)
	})
}

// implicitParamName coerces the argument of #implicit_parameter_get /
// #implicit_parameter_defined? and REFUSES a name that is not an implicit
// parameter's at all.
//
// ⛔ THE TWO REFUSALS ARE DIFFERENT AND BOTH ARE PINNED. MRI checks the shape of
// the NAME before it looks for a value (proc.c v4.0.9:726, 775):
//
//	:a   -> NameError "'a' is not an implicit parameter"          — never could be
//	:_1  -> NameError "implicit parameter '_1' is not defined for …" — could be, is not
//
// Collapsing them would pass "not defined" for a name no scope could ever hold.
// The TypeError for a non-String/Symbol is bindingVarName's, which is already
// MRI's message.
func (vm *VM) implicitParamName(v object.Value) string {
	name := vm.bindingVarName(v)
	if !isImplicitParamName(name) {
		raise("NameError", "'%s' is not an implicit parameter", name)
	}
	return name
}

func copyImplicit(m map[string]int) map[string]int {
	if m == nil {
		return nil
	}
	out := make(map[string]int, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// isImplicitParamName is MRI's rb_implicit_param_p (proc.c v4.0.9:517-521):
// `it`, or `_` followed by a single digit 1..9. `_0` and `_10` are ordinary
// locals.
func isImplicitParamName(name string) bool {
	return name == "it" || isNumberedParamName(name)
}

// isNumberedParamName is MRI's rb_numparam_id_p (proc.c v4.0.9:511-515), which
// tests `_1`..`_9` and NOT `it`.
//
// ⛔ THE TWO PREDICATES ARE NOT INTERCHANGEABLE, and MRI uses this narrower one
// in all three of Binding's local-variable methods (proc.c:549, 602, 649) while
// rb_implicit_param_p is used elsewhere. The reason is not a detail: `it` is a
// LEGAL local variable name in Ruby — `it = 5` compiles — so
// `binding.local_variable_get(:it)` must answer normally, while `_1` can never
// be a local and is refused. Widening the refusal to rb_implicit_param_p would
// break every binding that happens to hold a variable called `it`.
func isNumberedParamName(name string) bool {
	return len(name) == 2 && name[0] == '_' && name[1] >= '1' && name[1] <= '9'
}

// refuseNumberedParam is the guard MRI puts at the top of #local_variable_get,
// #local_variable_set and #local_variable_defined? (proc.c v4.0.9:549, 602,
// 649), from Ruby 4.0.
//
// ⛔ IT TESTS THE NAME, NOT THE BINDING, and fires BEFORE any lookup — so
// `binding.local_variable_get(:_1)` raises this even in a binding that has no
// numbered parameter at all, and even at the top level. `_1` is not a local
// variable that happens to be hidden; it is not a local variable name.
func refuseNumberedParam(name string) {
	if isNumberedParamName(name) {
		raise("NameError", "numbered parameter '%s' is not a local variable", name)
	}
}

// implicitParameterNames is bind_implicit_parameters (proc.c v4.0.9:680): [:it]
// when `it` is this scope's implicit parameter, otherwise the numbered ones in
// order. A block has one kind or the other and never both — `proc { |x| _1 }` is
// a syntax error — so the `it` arm returns before the numbered ones are looked
// at, as MRI's does.
func (b *Binding) implicitParameterNames() object.Value {
	if _, ok := b.implicit["it"]; ok {
		return object.NewArray(object.Symbol("it"))
	}
	var elems []object.Value
	// Ascending by name, which for `_1`..`_9` is ascending by slot too. MRI
	// builds the list from the local table in order for the same reason.
	for d := byte('1'); d <= '9'; d++ {
		n := string([]byte{'_', d})
		if _, ok := b.implicit[n]; !ok {
			break
		}
		elems = append(elems, object.Symbol(n))
	}
	return object.NewArrayFromSlice(elems)
}

// localVariableNames is the Symbol array Binding#local_variables and
// Kernel#local_variables both answer with. It is one body because in MRI they are
// one walk: bind_local_variables calls rb_vm_bind_local_variables, and
// rb_f_local_variables performs the same collection over the caller's frames
// (vm_eval.c ruby_4_0:2755-2787). Names are deduplicated, which is how an inner
// local shadowing an outer one is listed once — MRI's local_var_list is a hash.
func (b *Binding) localVariableNames() object.Value {
	seen := map[string]bool{}
	var elems []object.Value
	add := func(n string) {
		if n != "" && !seen[n] { // skip anonymous slots (pattern subjects etc.)
			seen[n] = true
			elems = append(elems, object.Symbol(n))
		}
	}
	// MRI lists local_variable_set-injected locals first (most-recent first),
	// then the binding's own locals — this frame's in slot order, then each
	// enclosing scope's, which is the order collectBindingLocals built.
	for i := len(b.added) - 1; i >= 0; i-- {
		add(b.added[i])
	}
	for _, n := range b.names[:len(b.names)-len(b.added)] {
		add(n)
	}
	return object.NewArrayFromSlice(elems)
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
