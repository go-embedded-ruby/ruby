// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"strings"

	confd "github.com/go-ruby-confd/confd"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the value bridge for the Confd module (see confd.go). It flattens
// the Ruby vars Hash into confd's "/"-path key/value map, threads the optional
// render options (prefix / keys / format) through to confd.RenderString, and
// re-raises any render failure as Confd::Error with a message stripped of the
// adapter's internally-managed staged-file path.

// confdRender implements Confd.render(template[, vars][, prefix:][, keys:][, format:]):
// it seeds an in-memory backend from vars and renders template through confd's
// real engine. A missing template raises ArgumentError, a non-String template
// TypeError, and a bad template or a missing key Confd::Error.
//
// vars is a positional Hash whose keys become confd "/"-paths (a leading "/" is
// added when absent) and whose nested Hashes flatten into further path segments;
// values are stringified. A trailing Hash is read as render options — not vars —
// only when it carries a recognised option key (prefix / keys / format), so a
// plain data Hash is never mistaken for options.
func (vm *VM) confdRender(args []object.Value) object.Value {
	if len(args) == 0 {
		raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
	}
	tmpl, ok := args[0].(*object.String)
	if !ok {
		raise("TypeError", "no implicit conversion of %s into String", classNameOf(args[0]))
	}

	rest := args[1:]
	opts := confdOptHash(rest)
	if opts != nil {
		rest = rest[:len(rest)-1]
	}

	var vars *object.Hash
	if len(rest) > 0 && !object.IsNil(rest[0]) {
		h, ok := rest[0].(*object.Hash)
		if !ok {
			raise("TypeError", "no implicit conversion of %s into Hash", classNameOf(rest[0]))
		}
		vars = h
	}

	out, err := confd.RenderString(tmpl.Str(), confdVars(vars), confdRenderOpts(opts)...)
	if err != nil {
		raise("Confd::Error", "%s", confdCleanErr(err))
	}
	return object.NewString(out)
}

// confdVars flattens a Ruby vars Hash into confd's key/value map: each key
// becomes a "/"-prefixed path, nested Hashes extend the path, and values are
// stringified. A nil Hash yields an empty map.
func confdVars(h *object.Hash) map[string]string {
	m := map[string]string{}
	confdFlatten(m, "", h)
	return m
}

// confdFlatten walks h under the path prefix, writing leaf values into m. At the
// top level (prefix == "") a key that already begins with "/" is kept verbatim;
// otherwise a leading "/" is added. Nested Hashes recurse with the joined path.
func confdFlatten(m map[string]string, prefix string, h *object.Hash) {
	if h == nil {
		return
	}
	for _, k := range h.Keys {
		v, _ := h.Get(k)
		key := k.ToS()
		var path string
		switch {
		case prefix != "":
			path = prefix + "/" + key
		case strings.HasPrefix(key, "/"):
			path = key
		default:
			path = "/" + key
		}
		if sub, ok := v.(*object.Hash); ok {
			confdFlatten(m, path, sub)
			continue
		}
		m[path] = v.ToS()
	}
}

// confdRenderOpts maps the recognised keys of an options Hash onto confd's
// RenderOptions: :prefix → WithPrefix, :keys (a String or Array of paths) →
// WithKeys, :format → WithOutputFormat. A nil Hash yields no options.
func confdRenderOpts(h *object.Hash) []confd.RenderOption {
	if h == nil {
		return nil
	}
	var out []confd.RenderOption
	if v, ok := confdOptGet(h, "prefix"); ok {
		out = append(out, confd.WithPrefix(v.ToS()))
	}
	if v, ok := confdOptGet(h, "keys"); ok {
		out = append(out, confd.WithKeys(confdKeyList(v)...))
	}
	if v, ok := confdOptGet(h, "format"); ok {
		out = append(out, confd.WithOutputFormat(v.ToS()))
	}
	return out
}

// confdKeyList reads a :keys option as a list of key prefixes: an Array yields
// one entry per element, any other value a single-element list.
func confdKeyList(v object.Value) []string {
	if arr, ok := v.(*object.Array); ok {
		keys := make([]string, len(arr.Elems))
		for i, e := range arr.Elems {
			keys[i] = e.ToS()
		}
		return keys
	}
	return []string{v.ToS()}
}

// confdOptHash returns the trailing Hash of args when it carries a recognised
// render-option key (prefix / keys / format), marking it as options rather than
// positional vars; otherwise it returns nil.
func confdOptHash(args []object.Value) *object.Hash {
	n := len(args)
	if n == 0 {
		return nil
	}
	h, ok := args[n-1].(*object.Hash)
	if !ok {
		return nil
	}
	for _, k := range []string{"prefix", "keys", "format"} {
		if _, ok := confdOptGet(h, k); ok {
			return h
		}
	}
	return nil
}

// confdOptGet reads an option by symbol or string key from an options Hash (a nil
// Hash, or an absent key, reports false).
func confdOptGet(h *object.Hash, key string) (object.Value, bool) {
	if h == nil {
		return nil, false
	}
	if v, ok := h.Get(object.Symbol(key)); ok {
		return v, true
	}
	return h.Get(object.NewString(key))
}

// confdCleanErr shapes a confd render error into a clean Ruby message by dropping
// the leading "<staged-file path>: " that confd prepends (the staged file lives
// in an internally-managed temp directory the caller never sees).
func confdCleanErr(err error) string {
	msg := err.Error()
	if i := strings.Index(msg, "rendered.out: "); i >= 0 {
		return msg[i+len("rendered.out: "):]
	}
	return msg
}
