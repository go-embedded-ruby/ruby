package vm

import (
	"unicode/utf8"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// encodingObj is an instance of the Encoding class. The objects are interned per
// VM, so two strings of the same encoding return the identical Encoding (and ==
// by identity), as in MRI.
type encodingObj struct{ name string }

func (e *encodingObj) ToS() string { return e.name }
func (e *encodingObj) Inspect() string {
	if e.name == "ASCII-8BIT" { // MRI displays it under its BINARY alias
		return "#<Encoding:BINARY (ASCII-8BIT)>"
	}
	return "#<Encoding:" + e.name + ">"
}
func (e *encodingObj) Truthy() bool { return true }

// internEncoding returns the shared Encoding object for a normalised name.
func (vm *VM) internEncoding(name string) *encodingObj {
	if vm.encodings == nil {
		vm.encodings = map[string]*encodingObj{}
	}
	if e, ok := vm.encodings[name]; ok {
		return e
	}
	e := &encodingObj{name: name}
	vm.encodings[name] = e
	return e
}

// normalizeEncoding maps an encoding name (case-insensitively, with BINARY as the
// ASCII-8BIT alias) to its canonical form; an unknown name is returned as-is.
func normalizeEncoding(name string) string {
	switch upper(name) {
	case "UTF-8", "UTF8":
		return "UTF-8"
	case "ASCII-8BIT", "BINARY":
		return "ASCII-8BIT"
	case "US-ASCII", "ASCII", "ANSI_X3.4-1968":
		return "US-ASCII"
	}
	return name
}

func upper(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] = c - 32
		}
	}
	return string(b)
}

func (vm *VM) registerEncoding() {
	vm.cEncoding = newClass("Encoding", vm.cObject)
	vm.consts["Encoding"] = vm.cEncoding

	mkConst := func(constName, name string) *encodingObj {
		e := vm.internEncoding(name)
		vm.cEncoding.consts[constName] = e
		return e
	}
	mkConst("UTF_8", "UTF-8")
	ascii := mkConst("ASCII_8BIT", "ASCII-8BIT")
	vm.cEncoding.consts["BINARY"] = ascii // BINARY is an alias of ASCII-8BIT
	mkConst("US_ASCII", "US-ASCII")

	vm.cEncoding.define("name", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self.(*encodingObj).name)
	})
	vm.cEncoding.define("to_s", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self.(*encodingObj).name)
	})
	vm.cEncoding.define("inspect", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self.(*encodingObj).Inspect())
	})
	// == is not defined here: the operator goes through the VM's identity equality,
	// and interned Encoding objects compare correctly by identity.
}

// encodingName extracts an encoding name from a force_encoding argument (a String
// or an Encoding object).
func encodingName(v object.Value) string {
	switch e := v.(type) {
	case *object.String:
		return normalizeEncoding(e.Str())
	case *encodingObj:
		return e.name
	}
	raise("TypeError", "no implicit conversion of %s into String", classNameOf(v))
	return ""
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
	vm.cString.define("force_encoding", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		s := self.(*object.String)
		s.Enc = encodingName(args[0])
		return s
	})
	vm.cString.define("b", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		d := self.(*object.String).Dup()
		d.Enc = "ASCII-8BIT"
		return d
	})
	vm.cString.define("ascii_only?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(asciiOnly(self.(*object.String).B))
	})
	vm.cString.define("valid_encoding?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		s := self.(*object.String)
		// Binary is always valid; a UTF-8 string is valid iff it decodes cleanly.
		return object.Bool(s.IsBinary() || utf8.Valid(s.B))
	})
}
