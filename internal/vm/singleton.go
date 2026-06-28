package vm

import "github.com/go-embedded-ruby/ruby/internal/object"

// registerSingleton installs the per-object singleton-method API:
// define_singleton_method and extend. Both work on ordinary objects (via a
// lazily-created singleton class) and on classes/modules (via their class
// methods). The `extended` hook fires when a module is mixed into an object.
func (vm *VM) registerSingleton() {
	vm.cObject.define("define_singleton_method", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		body := blk
		if body == nil {
			if len(args) > 1 {
				p, ok := args[1].(*Proc)
				if !ok {
					raise("TypeError", "wrong argument type %s (expected Proc)", classNameOf(args[1]))
				}
				body = p
			} else {
				raise("ArgumentError", "tried to create a method without a block")
			}
		}
		name := args[0].ToS()
		if t, ok := self.(*RClass); ok {
			t.smethods[name] = &Method{name: name, proc: body, owner: t}
			bumpMethodSerial()
			return object.Symbol(name)
		}
		sc, ok := vm.ensureSingleton(self)
		if !ok {
			raise("TypeError", "can't define singleton method %q for %s", name, vm.classOf(self).name)
		}
		sc.methods[name] = &Method{name: name, proc: body, owner: sc}
		bumpMethodSerial()
		return object.Symbol(name)
	})

	vm.cObject.define("extend", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1+)")
		}
		for _, a := range args {
			mod, ok := a.(*RClass)
			if !ok {
				raise("TypeError", "wrong argument type %s (expected Module)", classNameOf(a))
			}
			if t, ok := self.(*RClass); ok {
				// C.extend(M): M (and every module M itself includes) becomes part of
				// C's singleton-class ancestry, so M's own instance methods *and* the
				// methods of its transitively-included modules all become class methods
				// of C. Mixing M into the metaclass's includes (rather than copying
				// M.methods) lets lookupSMethod walk the whole include tree — matching
				// MRI, where `extend M` inserts M's full ancestry into the singleton
				// class.
				mc := t.metaClass()
				mc.includes = append(mc.includes, mod)
			} else {
				// Any other object (incl. builtin-backed Array/String/… such as
				// $LOAD_PATH) mixes the module into its singleton class.
				sc, ok := vm.ensureSingleton(self)
				if !ok {
					raise("TypeError", "can't extend a %s", vm.classOf(self).name)
				}
				sc.includes = append(sc.includes, mod)
			}
			bumpMethodSerial()
			// Hook: module.extended(object), if the module defines it.
			if hook := lookupSMethod(mod, "extended"); hook != nil {
				vm.invoke(hook, mod, []object.Value{self}, nil)
			}
		}
		return self
	})
}
