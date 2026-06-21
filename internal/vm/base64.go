package vm

import (
	"encoding/base64"
	"strings"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerBase64 installs the Base64 module (require "base64"), a thin binding of
// Go's encoding/base64 with MRI-compatible semantics: encode64 wraps at 60
// columns with a trailing newline; the strict_/urlsafe_ variants don't wrap.
func (vm *VM) registerBase64() {
	mod := newClass("Base64", nil)
	mod.isModule = true
	vm.consts["Base64"] = mod
	def := func(name string, fn NativeFn) { mod.smethods[name] = &Method{name: name, owner: mod, native: fn} }

	def("encode64", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(wrap60(base64.StdEncoding.EncodeToString([]byte(base64Arg(args[0])))))
	})
	def("strict_encode64", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(base64.StdEncoding.EncodeToString([]byte(base64Arg(args[0]))))
	})
	def("urlsafe_encode64", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(base64.URLEncoding.EncodeToString([]byte(base64Arg(args[0]))))
	})
	def("decode64", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		// Lenient (RFC 2045 "m"): drop every non-alphabet byte and decode without
		// requiring padding; an orphaned final sextet is discarded.
		f := filterBase64(base64Arg(args[0]))
		out, err := base64.RawStdEncoding.DecodeString(f)
		if err != nil && len(f) > 0 {
			out, _ = base64.RawStdEncoding.DecodeString(f[:len(f)-1])
		}
		return object.NewString(string(out))
	})
	def("strict_decode64", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		out, err := base64.StdEncoding.DecodeString(base64Arg(args[0]))
		if err != nil {
			raise("ArgumentError", "invalid base64")
		}
		return object.NewString(string(out))
	})
	def("urlsafe_decode64", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		out, err := base64.URLEncoding.DecodeString(base64Arg(args[0]))
		if err != nil {
			raise("ArgumentError", "invalid base64")
		}
		return object.NewString(string(out))
	})
}

// filterBase64 keeps only standard base64 alphabet bytes (dropping padding,
// whitespace and any other character), for lenient decode64.
func filterBase64(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '+' || c == '/' {
			b.WriteByte(c)
		}
	}
	return b.String()
}

// base64Arg returns the String argument's bytes, raising TypeError otherwise.
func base64Arg(v object.Value) string {
	s, ok := v.(*object.String)
	if !ok {
		raise("TypeError", "no implicit conversion of %s into String", classNameOf(v))
	}
	return string(s.B)
}

// wrap60 inserts a newline every 60 columns and a trailing newline, matching
// MRI's Base64.encode64.
func wrap60(s string) string {
	var b strings.Builder
	for len(s) > 60 {
		b.WriteString(s[:60])
		b.WriteByte('\n')
		s = s[60:]
	}
	b.WriteString(s)
	b.WriteByte('\n')
	return b.String()
}
