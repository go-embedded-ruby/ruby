package vm

import (
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
	sdef("default_internal", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		if vm.defInternalEnc == nil {
			return object.NilV
		}
		return vm.defInternalEnc
	})
	sdef("default_internal=", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		// nil clears it (MRI's default); otherwise remember the Encoding so
		// force_encoding("internal") can resolve it. rbgo strings stay UTF-8 either
		// way — only the reported default and the "internal" alias track this.
		if _, isNil := args[0].(object.Nil); isNil {
			vm.defInternalEnc = nil
		} else {
			vm.defInternalEnc = vm.encodingArg(args[0])
		}
		return args[0]
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
	// Encoding.compatible?(obj1, obj2) implements MRI's encoding negotiation,
	// returning the encoding a concatenation would take, or nil when incompatible.
	sdef("compatible?", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.encodingCompatible(args[0], args[1])
	})
}

// encOperand is one argument to Encoding.compatible?, reduced to what the
// negotiation needs: its governing encoding, its bytes (for String/Symbol content),
// and whether it carries content at all (a String or Symbol) versus being a bare
// Encoding object.
type encOperand struct {
	enc        *encodingObj
	bytes      []byte
	stringLike bool
	ok         bool
}

// is7bit reports whether the operand is ASCII-only *for compatibility purposes*:
// its encoding must be ASCII-compatible AND every byte 7-bit. Content in a
// non-ASCII-compatible encoding (UTF-16, ISO-2022-JP, …) is never 7-bit even when
// empty or all-ASCII, matching MRI's coderange (rb_enc_str_coderange).
func (o encOperand) is7bit() bool { return o.enc.asciiCompat && asciiOnly(o.bytes) }

// encOperandOf reduces a compatibility argument to an encOperand: a String keeps
// its tagged encoding and bytes; a Symbol is US-ASCII when ASCII-only else UTF-8;
// an Encoding is itself (no content); anything else has no encoding, making the
// pair incompatible. (Regexp operands are a named residual.)
func (vm *VM) encOperandOf(v object.Value) encOperand {
	switch x := v.(type) {
	case *object.String:
		return encOperand{enc: vm.internEncoding(x.EncName()), bytes: x.Bytes(), stringLike: true, ok: true}
	case object.Symbol:
		b := []byte(string(x))
		name := "UTF-8"
		if asciiOnly(b) {
			name = "US-ASCII"
		}
		return encOperand{enc: vm.encodings[name], bytes: b, stringLike: true, ok: true}
	case *encodingObj:
		return encOperand{enc: x, ok: true}
	}
	return encOperand{}
}

// encodingCompatible is Encoding.compatible?(obj1, obj2): the encoding of the
// string that would result from concatenating the two, or nil if they cannot be
// combined. It follows MRI's rb_enc_compatible negotiation:
//
//   - identical encodings are always compatible;
//   - an empty second string yields the first's encoding;
//   - against an empty first string, the second's encoding wins unless the first
//     is ASCII-compatible and the second is 7-bit;
//   - otherwise both encodings must be ASCII-compatible, and the 7-bit (ASCII-only)
//     operand adopts the other's encoding; two non-7-bit operands are incompatible.
func (vm *VM) encodingCompatible(a, b object.Value) object.Value {
	oa := vm.encOperandOf(a)
	ob := vm.encOperandOf(b)
	if !oa.ok || !ob.ok {
		return object.NilV
	}
	e1, e2 := oa.enc, ob.enc
	if e1 == e2 {
		return e1
	}
	if ob.stringLike && len(ob.bytes) == 0 {
		return e1
	}
	if oa.stringLike && ob.stringLike && len(oa.bytes) == 0 {
		if e1.asciiCompat && ob.is7bit() {
			return e1
		}
		return e2
	}
	if !e1.asciiCompat || !e2.asciiCompat {
		return object.NilV
	}
	if oa.stringLike && ob.stringLike {
		// Two content operands: the 7-bit (ASCII-only) one adopts the other's
		// encoding; two non-7-bit operands are incompatible.
		if ob.is7bit() {
			return e1
		}
		if oa.is7bit() {
			return e2
		}
		return object.NilV
	}
	if oa.stringLike != ob.stringLike {
		// One content operand, one bare Encoding. Verified against MRI in both
		// argument orders: a US-ASCII Encoding always keeps the string's own
		// encoding; otherwise, when the string is ASCII-only, the result is the
		// second operand's encoding (the Encoding when it is second, the string's
		// encoding when the Encoding is first); a non-ASCII string is incompatible.
		str, enc, encSecond := oa, e2, true
		if !oa.stringLike {
			str, enc, encSecond = ob, e1, false
		}
		if enc.name == "US-ASCII" {
			return str.enc
		}
		if str.is7bit() {
			if encSecond {
				return enc
			}
			return str.enc
		}
	}
	return object.NilV
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
	if vm.respondsToDynamic(v, "to_str") {
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
		vm.checkFrozen(s)
		s.Enc = vm.forceEncodingName(args[0])
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
		return object.Bool(validInEncoding(s.Bytes(), s.EncName()))
	})
	vm.registerStringEncodeMethods()
}

// registerEncodingErrors defines the EncodingError hierarchy raised by the
// transcoding path: EncodingError < StandardError, and Encoding's
// UndefinedConversionError / InvalidByteSequenceError / ConverterNotFoundError /
// CompatibilityError under it.
func (vm *VM) registerEncodingErrors() {
	std := vm.consts["StandardError"].(*RClass)
	encErr := newClass("EncodingError", std)
	vm.consts["EncodingError"] = encErr
	for _, n := range []string{"UndefinedConversionError", "InvalidByteSequenceError", "ConverterNotFoundError", "CompatibilityError"} {
		c := newClass("Encoding::"+n, encErr)
		vm.cEncoding.consts[n] = c
		vm.consts["Encoding::"+n] = c
	}
}
