package vm

import (
	b64 "github.com/go-ruby-base64/base64"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerBase64 installs the Base64 module (require "base64") as a thin binding
// of github.com/go-ruby-base64/base64, a pure-Go, MRI-4.0.5-faithful Base64 that
// runs the standard-alphabet hot paths on go-simd kernels. encode64 wraps at 60
// columns with a trailing newline; the strict_/urlsafe_ variants don't wrap.
func (vm *VM) registerBase64() {
	mod := newClass("Base64", nil)
	mod.isModule = true
	vm.consts["Base64"] = mod
	def := func(name string, fn NativeFn) { mod.smethods[name] = &Method{name: name, owner: mod, native: fn} }

	def("encode64", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(b64.Encode64(base64Arg(args[0])))
	})
	def("strict_encode64", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(b64.StrictEncode64(base64Arg(args[0])))
	})
	def("urlsafe_encode64", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(b64.UrlsafeEncode64(base64Arg(args[0])))
	})
	def("decode64", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(b64.Decode64(base64Arg(args[0])))
	})
	def("strict_decode64", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		out, err := b64.StrictDecode64(base64Arg(args[0]))
		if err != nil {
			raiseBase64Invalid()
		}
		return object.NewString(out)
	})
	def("urlsafe_decode64", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		out, err := b64.UrlsafeDecode64(base64Arg(args[0]))
		if err != nil {
			raiseBase64Invalid()
		}
		return object.NewString(out)
	})
}

// raiseBase64Invalid maps the library's ErrInvalid (the only error returned by
// StrictDecode64/UrlsafeDecode64) to Ruby's ArgumentError("invalid base64"),
// matching MRI's strict_/urlsafe_decode64.
func raiseBase64Invalid() {
	raise("ArgumentError", "invalid base64")
}

// base64Arg returns the String argument's bytes, raising TypeError otherwise.
// Shared by Base64 and the OpenSSL/Digest bindings as a generic String coercion.
func base64Arg(v object.Value) string {
	s, ok := v.(*object.String)
	if !ok {
		raise("TypeError", "no implicit conversion of %s into String", classNameOf(v))
	}
	return string(s.B)
}
