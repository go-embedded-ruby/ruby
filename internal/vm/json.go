package vm

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerJSON installs the JSON module (require "json"): JSON.generate/dump,
// JSON.pretty_generate, JSON.parse, and Object#to_json. Generation walks the
// Ruby value tree directly (so float and hash-key formatting match MRI); parsing
// uses encoding/json's token stream so object key order is preserved, as MRI
// keeps it.
func (vm *VM) registerJSON() {
	mod := newClass("JSON", nil)
	mod.isModule = true
	vm.consts["JSON"] = mod
	def := func(name string, fn NativeFn) { mod.smethods[name] = &Method{name: name, owner: mod, native: fn} }

	generate := func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		var sb strings.Builder
		jsonGen(args[0], &sb)
		return object.NewString(sb.String())
	}
	def("generate", generate)
	def("dump", generate)
	def("pretty_generate", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		var sb strings.Builder
		jsonPretty(args[0], &sb, 0)
		return object.NewString(sb.String())
	})
	def("parse", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		s, ok := args[0].(*object.String)
		if !ok {
			raise("TypeError", "no implicit conversion of %s into String", classNameOf(args[0]))
		}
		dec := json.NewDecoder(strings.NewReader(s.Str()))
		dec.UseNumber()
		v := jsonParse(dec)
		if _, err := dec.Token(); err == nil {
			// Trailing content after a complete value is malformed.
			raise("RuntimeError", "unexpected token after JSON value")
		}
		return v
	})

	// Object#to_json serialises any value (Array/Hash/… included) via generate.
	vm.cObject.define("to_json", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		var sb strings.Builder
		jsonGen(self, &sb)
		return object.NewString(sb.String())
	})
}

// jsonGen writes the compact JSON encoding of v.
func jsonGen(v object.Value, sb *strings.Builder) {
	switch x := v.(type) {
	case object.Integer:
		sb.WriteString(strconv.FormatInt(int64(x), 10))
	case *object.Bignum:
		sb.WriteString(x.ToS())
	case object.Float:
		f := float64(x)
		if math.IsInf(f, 0) || math.IsNaN(f) {
			raise("RuntimeError", "%s not allowed in JSON", x.ToS())
		}
		sb.WriteString(x.ToS())
	case *object.String:
		jsonStr(x.Str(), sb)
	case object.Symbol:
		jsonStr(string(x), sb)
	case object.Bool:
		if bool(x) {
			sb.WriteString("true")
		} else {
			sb.WriteString("false")
		}
	case object.Nil:
		sb.WriteString("null")
	case *object.Array:
		sb.WriteByte('[')
		for i, e := range x.Elems {
			if i > 0 {
				sb.WriteByte(',')
			}
			jsonGen(e, sb)
		}
		sb.WriteByte(']')
	case *object.Hash:
		sb.WriteByte('{')
		for i, k := range x.Keys {
			if i > 0 {
				sb.WriteByte(',')
			}
			jsonStr(jsonKey(k), sb)
			sb.WriteByte(':')
			val, _ := x.Get(k)
			jsonGen(val, sb)
		}
		sb.WriteByte('}')
	default:
		// Anything else is serialised by its string form (matching MRI's
		// to_s-of-unknown fallback closely enough for our value set).
		jsonStr(v.ToS(), sb)
	}
}

// jsonPretty writes the 2-space-indented JSON encoding of v (MRI's
// JSON.pretty_generate layout).
func jsonPretty(v object.Value, sb *strings.Builder, depth int) {
	switch x := v.(type) {
	case *object.Array:
		if len(x.Elems) == 0 {
			sb.WriteString("[]")
			return
		}
		sb.WriteString("[\n")
		for i, e := range x.Elems {
			if i > 0 {
				sb.WriteString(",\n")
			}
			jsonIndent(sb, depth+1)
			jsonPretty(e, sb, depth+1)
		}
		sb.WriteByte('\n')
		jsonIndent(sb, depth)
		sb.WriteByte(']')
	case *object.Hash:
		if len(x.Keys) == 0 {
			sb.WriteString("{}")
			return
		}
		sb.WriteString("{\n")
		for i, k := range x.Keys {
			if i > 0 {
				sb.WriteString(",\n")
			}
			jsonIndent(sb, depth+1)
			jsonStr(jsonKey(k), sb)
			sb.WriteString(": ")
			val, _ := x.Get(k)
			jsonPretty(val, sb, depth+1)
		}
		sb.WriteByte('\n')
		jsonIndent(sb, depth)
		sb.WriteByte('}')
	default:
		jsonGen(v, sb)
	}
}

func jsonIndent(sb *strings.Builder, depth int) {
	for i := 0; i < depth; i++ {
		sb.WriteString("  ")
	}
}

// jsonKey renders a hash key as the string JSON requires (symbol → name,
// anything else → its to_s).
func jsonKey(k object.Value) string {
	if s, ok := k.(*object.String); ok {
		return s.Str()
	}
	return k.ToS()
}

// jsonStr writes a JSON string literal, escaping per MRI (control characters and
// the structural characters only — UTF-8 text passes through unescaped).
func jsonStr(s string, sb *strings.Builder) {
	sb.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			sb.WriteString(`\"`)
		case '\\':
			sb.WriteString(`\\`)
		case '\n':
			sb.WriteString(`\n`)
		case '\r':
			sb.WriteString(`\r`)
		case '\t':
			sb.WriteString(`\t`)
		case '\f':
			sb.WriteString(`\f`)
		case '\b':
			sb.WriteString(`\b`)
		default:
			if r < 0x20 {
				sb.WriteString(`\u`)
				const hexdigits = "0123456789abcdef"
				sb.WriteByte('0')
				sb.WriteByte('0')
				sb.WriteByte(hexdigits[(r>>4)&0xf])
				sb.WriteByte(hexdigits[r&0xf])
			} else {
				sb.WriteRune(r)
			}
		}
	}
	sb.WriteByte('"')
}

// jsonParse reads one JSON value from dec's token stream, preserving object key
// order. Malformed input raises (the decoder surfaces the error).
func jsonParse(dec *json.Decoder) object.Value {
	tok, err := dec.Token()
	if err != nil {
		raise("RuntimeError", "unexpected token in JSON")
	}
	switch t := tok.(type) {
	case json.Delim:
		// A value can only begin with '[' or '{' — the decoder errors on a stray
		// ']'/'}' before yielding it here, so a non-'[' delim is necessarily '{'.
		if t == '[' {
			arr := &object.Array{}
			for dec.More() {
				arr.Elems = append(arr.Elems, jsonParse(dec))
			}
			dec.Token() // consume ']'
			return arr
		}
		h := object.NewHash()
		for dec.More() {
			keyTok, err := dec.Token() // object keys are always strings
			key, ok := keyTok.(string)
			if err != nil || !ok {
				raise("RuntimeError", "unexpected token in JSON object key")
			}
			h.Set(object.NewString(key), jsonParse(dec))
		}
		dec.Token() // consume '}'
		return h
	case string:
		return object.NewString(t)
	case json.Number:
		return jsonNumber(string(t))
	case bool:
		return object.Bool(t)
	}
	// The only remaining token type is a JSON null.
	return object.NilV
}

// jsonNumber turns a JSON numeric literal into an Integer when it has no
// fractional/exponent part, else a Float (matching MRI's JSON.parse).
func jsonNumber(s string) object.Value {
	if !strings.ContainsAny(s, ".eE") {
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return object.Integer(n)
		}
	}
	f, _ := strconv.ParseFloat(s, 64)
	return object.Float(f)
}
