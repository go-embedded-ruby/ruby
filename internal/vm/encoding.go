package vm

import (
	"unicode/utf8"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// encodingObj is an instance of the Encoding class. The objects are interned per
// VM from the registry (encTable), so two strings of the same encoding return
// the identical Encoding (and == by identity), as in MRI.
type encodingObj struct {
	name        string
	aliases     []string // alias names, in MRI #names order (after the canonical name)
	dummy       bool
	asciiCompat bool
}

func (e *encodingObj) ToS() string { return e.name }
func (e *encodingObj) Inspect() string {
	pre := "#<Encoding:" + e.name
	if e.dummy {
		return pre + " (dummy)>"
	}
	if e.name == "ASCII-8BIT" { // MRI displays it under its BINARY alias
		return "#<Encoding:BINARY (ASCII-8BIT)>"
	}
	return pre + ">"
}
func (e *encodingObj) Truthy() bool { return true }

// internEncoding returns the shared Encoding object for a name. A registered
// (canonical or alias) name resolves to the interned registry object. A String's
// Enc tag is always a registered canonical name, so the fallback — which mints a
// transient ASCII-compatible Encoding for an otherwise-unknown tag — is defensive
// only, guaranteeing callers never see nil.
func (vm *VM) internEncoding(name string) *encodingObj {
	if e, ok := vm.findEncoding(name); ok {
		return e
	}
	e := &encodingObj{name: name, asciiCompat: true}
	vm.encodings[name] = e
	vm.encLookup[lower(name)] = e // intern so repeat calls stay stable
	return e
}

// ensureEncodings builds the registry (canonical objects + case-insensitive
// lookup over every name and alias) the first time it is needed.
func (vm *VM) ensureEncodings() {
	if vm.encodings != nil {
		return
	}
	vm.encodings = make(map[string]*encodingObj, len(encTable))
	vm.encLookup = map[string]*encodingObj{}
	for _, info := range encTable {
		e := &encodingObj{name: info.name, aliases: info.aliases, dummy: info.dummy, asciiCompat: info.asciiCompat}
		vm.encodings[info.name] = e
		vm.encLookup[lower(info.name)] = e
		for _, a := range info.aliases {
			vm.encLookup[lower(a)] = e
		}
	}
}

// findEncoding resolves an encoding name (case-insensitively over canonical names
// and aliases) to its registry object.
func (vm *VM) findEncoding(name string) (*encodingObj, bool) {
	vm.ensureEncodings()
	e, ok := vm.encLookup[lower(name)]
	return e, ok
}

func lower(s string) string {
	b := []byte(s)
	changed := false
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
			changed = true
		}
	}
	if !changed {
		return s
	}
	return string(b)
}

func (vm *VM) registerEncoding() {
	vm.ensureEncodings()
	vm.cEncoding = newClass("Encoding", vm.cObject)
	vm.consts["Encoding"] = vm.cEncoding

	// Every Encoding:: constant points at its canonical registry object.
	for constName, canon := range encConstNames {
		vm.cEncoding.consts[constName] = vm.encodings[canon]
	}

	vm.cEncoding.define("name", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self.(*encodingObj).name)
	})
	vm.cEncoding.define("to_s", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self.(*encodingObj).name)
	})
	vm.cEncoding.define("inspect", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self.(*encodingObj).Inspect())
	})
	vm.cEncoding.define("names", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		e := self.(*encodingObj)
		out := make([]object.Value, 0, 1+len(e.aliases))
		out = append(out, object.NewString(e.name))
		for _, a := range e.aliases {
			out = append(out, object.NewString(a))
		}
		return object.NewArrayFromSlice(out)
	})
	vm.cEncoding.define("dummy?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self.(*encodingObj).dummy)
	})
	vm.cEncoding.define("ascii_compatible?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self.(*encodingObj).asciiCompat)
	})
	// == is not defined here: the operator goes through the VM's identity equality,
	// and interned Encoding objects compare correctly by identity.

	// Process default encodings. rbgo works in UTF-8, so default_external is the
	// interned UTF-8 encoding and default_internal is nil (MRI's default), the
	// values Puppet's log_runtime_environment and IO setup read. The setters are
	// accepted (and remembered) so code that brackets work in an encoding override
	// runs; rbgo's string layer remains UTF-8 regardless.
	defExternal := vm.encodings["UTF-8"]
	sdef := func(name string, fn NativeFn) {
		vm.cEncoding.smethods[name] = &Method{name: name, owner: vm.cEncoding, native: fn}
	}
	sdef("default_external", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return defExternal
	})
	sdef("default_external=", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		defExternal = vm.encodingArg(args[0])
		return args[0]
	})
	sdef("default_internal", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NilV
	})
	sdef("default_internal=", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return args[0] // accepted but not applied — rbgo strings stay UTF-8
	})
	sdef("locale_charmap", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString("UTF-8")
	})
	// Encoding.find(name) resolves a name (or a pass-through Encoding) to the
	// registry object, raising ArgumentError for an unknown name as MRI does.
	sdef("find", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.encodingArg(args[0])
	})
	// Encoding.list returns every registered (non-alias) encoding, in registration
	// order.
	sdef("list", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		out := make([]object.Value, 0, len(encTable))
		for _, info := range encTable {
			out = append(out, vm.encodings[info.name])
		}
		return object.NewArrayFromSlice(out)
	})
	// Encoding.name_list returns every name AND alias as Strings.
	sdef("name_list", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		out := make([]object.Value, 0, len(encTable)*2)
		for _, info := range encTable {
			out = append(out, object.NewString(info.name))
			for _, a := range info.aliases {
				out = append(out, object.NewString(a))
			}
		}
		return object.NewArrayFromSlice(out)
	})
	// Encoding.aliases returns the alias => canonical-name Hash.
	sdef("aliases", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		h := object.NewHash()
		for _, info := range encTable {
			for _, a := range info.aliases {
				h.Set(object.NewString(a), object.NewString(info.name))
			}
		}
		return h
	})
}

// encodingArg resolves an Encoding.find / force_encoding argument to a registry
// object: an Encoding is returned as-is, a String (or #to_str) is looked up
// case-insensitively, a Symbol (and anything else) raises TypeError, and an
// unknown name raises ArgumentError — all matching MRI.
func (vm *VM) encodingArg(v object.Value) *encodingObj {
	switch e := v.(type) {
	case *encodingObj:
		return e
	case *object.String:
		return vm.lookupEncodingName(e.Str())
	}
	if vm.respondsTo(v, "to_str") {
		r := vm.send(v, "to_str", nil, nil)
		if s, ok := r.(*object.String); ok {
			return vm.lookupEncodingName(s.Str())
		}
	}
	raise("TypeError", "no implicit conversion of %s into String", classNameOf(v))
	return nil
}

func (vm *VM) lookupEncodingName(name string) *encodingObj {
	if e, ok := vm.findEncoding(name); ok {
		return e
	}
	raise("ArgumentError", "unknown encoding name - %s", name)
	return nil
}

// encodingName extracts the canonical encoding name from a force_encoding
// argument (a String, an Encoding, or a #to_str), raising as MRI does.
func (vm *VM) encodingName(v object.Value) string {
	return vm.encodingArg(v).name
}

// asciiOnly reports whether every byte is 7-bit ASCII.
func asciiOnly(b []byte) bool {
	for _, c := range b {
		if c >= 0x80 {
			return false
		}
	}
	return true
}

// registerStringEncoding adds the encoding-aware String methods. (Called from the
// String setup so it shares cString.)
func (vm *VM) registerStringEncoding() {
	vm.cString.define("encoding", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.internEncoding(self.(*object.String).EncName())
	})
	vm.cString.define("force_encoding", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		s := self.(*object.String)
		s.Enc = vm.encodingName(args[0])
		return s
	})
	vm.cString.define("b", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		d := self.(*object.String).Dup()
		d.Enc = "ASCII-8BIT"
		return d
	})
	vm.cString.define("ascii_only?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(asciiOnly(self.(*object.String).Bytes()))
	})
	vm.cString.define("valid_encoding?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		s := self.(*object.String)
		// Binary is always valid; a UTF-8 string is valid iff it decodes cleanly.
		return object.Bool(s.IsBinary() || utf8.Valid(s.Bytes()))
	})
}
