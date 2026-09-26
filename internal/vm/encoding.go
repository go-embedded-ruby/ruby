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
	// Encoding#to_s is a true alias of #name (they share one Method record, so
	// Encoding.instance_method(:to_s) == Encoding.instance_method(:name), as MRI).
	vm.cEncoding.methods["to_s"] = vm.cEncoding.methods["name"]
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
	vm.defExternalEnc = vm.encodings["UTF-8"]
	sdef := func(name string, fn NativeFn) {
		vm.cEncoding.smethods[name] = &Method{name: name, owner: vm.cEncoding, native: fn}
	}
	sdef("default_external", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.defExternalEnc
	})
	sdef("default_external=", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		// MRI rejects nil with ArgumentError (not the TypeError encodingArg would
		// raise), and setting the default also redirects the "external",
		// "filesystem" and "locale" names Encoding.find resolves.
		if _, isNil := args[0].(object.Nil); isNil {
			raise("ArgumentError", "default external can not be nil")
		}
		vm.defExternalEnc = vm.encodingArg(args[0])
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
	// registry object, raising ArgumentError for an unknown name as MRI does. The
	// dynamic names track the current process defaults: "external"/"filesystem"/
	// "locale" are Encoding.default_external, and "internal" is
	// Encoding.default_internal (nil when unset).
	sdef("find", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if s, ok := args[0].(*object.String); ok {
			switch lower(s.Str()) {
			case "external", "filesystem", "locale":
				return vm.defExternalEnc
			case "internal":
				if vm.defInternalEnc == nil {
					return object.NilV
				}
				return vm.defInternalEnc
			}
		}
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
// negotiation needs: its governing encoding, its bytes (for String content), and
// whether it is a real String (whose byte content — its coderange — participates)
// as opposed to a Symbol, Regexp or bare Encoding (whose declared encoding alone
// participates, exactly as MRI treats every non-T_STRING encoding-capable object).
type encOperand struct {
	enc   *encodingObj
	bytes []byte
	isStr bool
	ok    bool
}

// is7bit reports whether a String operand is ASCII-only *for compatibility
// purposes* (MRI's ENC_CODERANGE_7BIT): its encoding must be ASCII-compatible AND
// every byte 7-bit. Content in a non-ASCII-compatible encoding (UTF-16,
// ISO-2022-JP, …) is never 7-bit even when empty or all-ASCII.
func (o encOperand) is7bit() bool { return o.enc.asciiCompat && asciiOnly(o.bytes) }

// encOperandOf reduces a compatibility argument to an encOperand:
//   - a String keeps its tagged encoding and bytes (isStr, so its coderange is
//     consulted below);
//   - a Symbol, Regexp or Encoding contributes only its declared encoding (not a
//     String, so its content coderange is never examined) — a Symbol is US-ASCII
//     when ASCII-only else UTF-8, a Regexp reports its source encoding, and an
//     Encoding is itself;
//   - anything else has no encoding, making the pair incompatible.
func (vm *VM) encOperandOf(v object.Value) encOperand {
	switch x := v.(type) {
	case *object.String:
		return encOperand{enc: vm.internEncoding(x.EncName()), bytes: x.Bytes(), isStr: true, ok: true}
	case object.Symbol:
		name := "UTF-8"
		if asciiOnly([]byte(string(x))) {
			name = "US-ASCII"
		}
		return encOperand{enc: vm.encodings[name], ok: true}
	case *Regexp:
		return encOperand{enc: vm.internEncoding(x.encodingName()), ok: true}
	case *encodingObj:
		return encOperand{enc: x, ok: true}
	}
	return encOperand{}
}

// encodingCompatible is Encoding.compatible?(obj1, obj2): the encoding of the
// string that would result from concatenating the two, or nil if they cannot be
// combined. It follows MRI's rb_enc_compatible / enc_compatible_latter negotiation
// faithfully, including the String-vs-non-String distinction (only a real String's
// byte coderange participates; a Symbol/Regexp/Encoding contributes its declared
// encoding, with US-ASCII the special "same as contents" case).
func (vm *VM) encodingCompatible(a, b object.Value) object.Value {
	oa := vm.encOperandOf(a)
	ob := vm.encOperandOf(b)
	if !oa.ok || !ob.ok {
		return object.NilV
	}
	enc1, enc2 := oa.enc, ob.enc
	// Identical encodings are always compatible.
	if enc1 == enc2 {
		return enc1
	}
	// An empty second String takes the first's encoding.
	if ob.isStr && len(ob.bytes) == 0 {
		return enc1
	}
	// An empty first String yields the second's encoding, unless the first is
	// ASCII-compatible and the second is 7-bit (then the first's).
	if oa.isStr && ob.isStr && len(oa.bytes) == 0 {
		if enc1.asciiCompat && ob.is7bit() {
			return enc1
		}
		return enc2
	}
	// From here both encodings must be ASCII-compatible.
	if !enc1.asciiCompat || !enc2.asciiCompat {
		return object.NilV
	}
	// A non-String whose declared encoding is US-ASCII contributes the other
	// operand's encoding (MRI's "objects whose encoding is the same of contents").
	if !ob.isStr && enc2.name == "US-ASCII" {
		return enc1
	}
	if !oa.isStr && enc1.name == "US-ASCII" {
		return enc2
	}
	// Orient so s1 is the String (if either is). enc1/enc2 keep referring to the
	// original operand positions, as in MRI where only str1/str2 are swapped.
	s1, s2 := oa, ob
	if !oa.isStr {
		s1, s2 = ob, oa
	}
	if s1.isStr {
		cr1 := s1.is7bit()
		if s2.isStr {
			cr2 := s2.is7bit()
			if cr1 != cr2 {
				if cr1 {
					return enc2
				}
				return enc1
			}
			if cr2 {
				return enc1
			}
		}
		if cr1 {
			return enc2
		}
	}
	return object.NilV
}

// combinedEncName returns the encoding name of the string formed by combining a
// and b (append, concatenation, replacement), following MRI's compatibility
// negotiation; it raises Encoding::CompatibilityError when the two are
// incompatible.
func (vm *VM) combinedEncName(a, b *object.String) string {
	r := vm.encodingCompatible(a, b)
	if object.IsNil(r) {
		raise("Encoding::CompatibilityError", "incompatible character encodings: %s and %s", a.EncName(), b.EncName())
	}
	return r.(*encodingObj).name
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
	vm.cString.define("ascii_only?", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		// #ascii_only? is ENC_CODERANGE_7BIT: content in a non-ASCII-compatible
		// encoding (UTF-16/32, …) is never 7-bit, even when empty or all-ASCII in
		// bytes, so it reports false regardless of the bytes.
		s := self.(*object.String)
		return object.Bool(vm.internEncoding(s.EncName()).asciiCompat && asciiOnly(s.Bytes()))
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
	undef := vm.consts["Encoding::UndefinedConversionError"].(*RClass)
	invalid := vm.consts["Encoding::InvalidByteSequenceError"].(*RClass)
	// transcode.c: every accessor is a plain rb_attr_get of the ivar
	// make_econv_exception stored, so an exception built any other way (a bare
	// .new) reports nil rather than raising.
	for _, cls := range []*RClass{undef, invalid} {
		for _, name := range []string{
			"source_encoding", "source_encoding_name",
			"destination_encoding", "destination_encoding_name",
		} {
			cls.define(name, econvAttrReader("@"+name))
		}
	}
	undef.define("error_char", econvAttrReader("@error_char"))
	invalid.define("error_bytes", econvAttrReader("@error_bytes"))
	invalid.define("readagain_bytes", econvAttrReader("@readagain_bytes"))
	invalid.define("incomplete_input?", econvAttrReader("@incomplete_input"))
}

// raiseEconvError raises the very exception object the converter recorded as its
// #last_error. rb_econv_check_error (transcode.c) passes make_econv_exception's
// result straight to rb_exc_raise, so the raised object and #last_error are one
// object carrying one set of attributes; rebuilding a second exception from the
// message alone would raise something that answers nil to every accessor.
func (vm *VM) raiseEconvError(exc object.Value) object.Value {
	panic(vm.excError(exc))
}

// econvAttrReader is the ecerr_* accessor shape from transcode.c: each of
// #source_encoding, #error_char, #incomplete_input? and their siblings is
// rb_attr_get of one ivar, so an unset one reads as nil (the spec for
// `Encoding::InvalidByteSequenceError.new.incomplete_input?` asserts exactly
// that) instead of raising NameError.
func econvAttrReader(ivar string) NativeFn {
	return func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return getIvar(self, ivar)
	}
}

// setEconvErrorAttrs attaches the transcoding-error attributes MRI's
// make_econv_exception (transcode.c) stores on the exception it builds, from the
// converter's last_error state: srcEnc/dstEnc are the encodings of the *hop that
// failed* (not the converter's own endpoints), errBytes the discarded bytes and
// readAgain the bytes to be read again.
//
//   - undefined_conversion sets @error_char: the error bytes tagged with the
//     failing hop's source encoding, so it is the one character that could not be
//     converted (rb_enc_associate_index on the source encoding index).
//   - invalid_byte_sequence / incomplete_input set @error_bytes and
//     @readagain_bytes as BINARY strings — rb_str_new leaves them ASCII-8BIT —
//     with @readagain_bytes nil when readagain_len is 0, plus @incomplete_input.
//
// Both then fall through to set_encs, which records the names unconditionally and
// the Encoding objects only when the name resolves to a registered encoding.
func (vm *VM) setEconvErrorAttrs(exc object.Value, status string, errBytes, readAgain []byte, srcEnc, dstEnc string) {
	switch status {
	case "undefined_conversion":
		ec := object.NewStringBytes(append([]byte(nil), errBytes...))
		if _, ok := vm.findEncoding(srcEnc); ok {
			ec.Enc = srcEnc
		}
		setIvar(exc, "@error_char", ec)
	case "invalid_byte_sequence", "incomplete_input":
		setIvar(exc, "@error_bytes", binStr(errBytes))
		if len(readAgain) > 0 {
			setIvar(exc, "@readagain_bytes", binStr(readAgain))
		} else {
			setIvar(exc, "@readagain_bytes", object.NilV)
		}
		setIvar(exc, "@incomplete_input", object.Bool(status == "incomplete_input"))
	default:
		return
	}
	setIvar(exc, "@source_encoding_name", object.NewString(srcEnc))
	setIvar(exc, "@destination_encoding_name", object.NewString(dstEnc))
	if e, ok := vm.findEncoding(srcEnc); ok {
		setIvar(exc, "@source_encoding", e)
	}
	if e, ok := vm.findEncoding(dstEnc); ok {
		setIvar(exc, "@destination_encoding", e)
	}
}
