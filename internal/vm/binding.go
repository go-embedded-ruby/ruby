package vm

import (
	"github.com/go-embedded-ruby/ruby/internal/compiler"
	"github.com/go-embedded-ruby/ruby/internal/object"
	"github.com/go-ruby-parser/parser"
)

// Binding captures a frame's local-variable environment, self and definee, so
// code can be eval'd against it later (Binding#eval, eval(str, binding)) and its
// locals inspected/mutated. names maps slot index → local name (a mutable copy,
// so local_variable_set can add a binding-only local without touching the ISeq).
type Binding struct {
	env     *Env
	self    object.Value
	definee *RClass
	names   []string // slot index → local name (original ISeq locals, then injected ones)
	added   []string // names injected via local_variable_set, in insertion order
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

	cBinding.define("eval", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.bindingEval(self.(*Binding), args[0])
	})
	cBinding.define("receiver", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return self.(*Binding).self
	})
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
		return &object.Array{Elems: elems}
	})
	cBinding.define("local_variable_get", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		b := self.(*Binding)
		name := bindingVarName(args[0])
		i := b.slotOf(name)
		if i < 0 {
			raise("NameError", "local variable '%s' is not defined for %s", name, b.ToS())
		}
		return b.env.slots[i]
	})
	cBinding.define("local_variable_set", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		b := self.(*Binding)
		name := bindingVarName(args[0])
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
	cBinding.define("local_variable_defined?", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(self.(*Binding).slotOf(bindingVarName(args[0])) >= 0)
	})
}

// bindingVarName coerces a Symbol/String local-variable name to a Go string.
func bindingVarName(v object.Value) string {
	switch n := v.(type) {
	case object.Symbol:
		return string(n)
	case *object.String:
		return n.Str()
	default:
		raise("TypeError", "%s is not a symbol nor a string", v.Inspect())
		return ""
	}
}

// bindingEval compiles src against the binding's locals (resolved at depth 1 via
// a child scope) and runs it with the binding's environment, self and definee —
// so the eval'd code sees and writes the binding's local variables.
func (vm *VM) bindingEval(b *Binding, srcV object.Value) object.Value {
	s, ok := srcV.(*object.String)
	if !ok {
		raise("TypeError", "no implicit conversion of %s into String", classNameOf(srcV))
	}
	prog, perr := parser.Parse(s.Str())
	if perr != nil {
		raise("SyntaxError", "%s", perr.Error())
	}
	iseq, cerr := compiler.CompileWithLocals(prog, b.names)
	if cerr != nil {
		raise("SyntaxError", "%s", cerr.Error())
	}
	iseq.Name = "(eval)"
	return vm.exec(iseq, b.self, nil, b.definee, "", b.env, nil, nil)
}
