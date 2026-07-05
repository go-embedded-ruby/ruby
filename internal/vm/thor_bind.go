// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	thor "github.com/go-ruby-thor/thor"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin binding between rbgo's Ruby object graph (object.Value)
// and the interpreter-independent github.com/go-ruby-thor/thor library. The
// library owns the whole deterministic Thor core — the option value model,
// the argv parser, the command registry, and byte-faithful help/usage rendering;
// rbgo wraps each library object as a Ruby object reporting the matching Thor::*
// class (see thor.go for the class + method registration) and converts values
// across the boundary here. Thor's value model maps a fixed set of Go types to
// Ruby values, reproduced by thorValueToRuby / thorValueToGo below.

// The wrapper types. Each holds a pointer into the library and reports the
// matching Thor::* class (see classOf); the methods registered in thor.go operate
// on the held value.

// ThorOption wraps a *thor.Option (Thor::Option).
type ThorOption struct{ o *thor.Option }

// ThorOptions wraps a *thor.Options parser (Thor::Options); remaining records the
// non-option argv tail from the most recent #parse, which #remaining returns.
type ThorOptions struct {
	parser    *thor.Options
	remaining []string
}

// ThorCommand wraps a *thor.Command (Thor::Command).
type ThorCommand struct{ c *thor.Command }

// ThorBase wraps a *thor.Base command registry (Thor::Base).
type ThorBase struct{ b *thor.Base }

func (v *ThorOption) ToS() string      { return "#<Thor::Option>" }
func (v *ThorOption) Inspect() string  { return "#<Thor::Option>" }
func (v *ThorOption) Truthy() bool     { return true }
func (v *ThorOptions) ToS() string     { return "#<Thor::Options>" }
func (v *ThorOptions) Inspect() string { return "#<Thor::Options>" }
func (v *ThorOptions) Truthy() bool    { return true }
func (v *ThorCommand) ToS() string     { return "#<Thor::Command>" }
func (v *ThorCommand) Inspect() string { return "#<Thor::Command>" }
func (v *ThorCommand) Truthy() bool    { return true }
func (v *ThorBase) ToS() string        { return "#<Thor::Base>" }
func (v *ThorBase) Inspect() string    { return "#<Thor::Base>" }
func (v *ThorBase) Truthy() bool       { return true }

// raiseThorError re-raises a library *thor.Error as its matching Ruby exception,
// named by the error's ErrorKind (a Thor::* class, or Ruby's ArgumentError for
// KindArgument), carrying the gem-exact message. Every error the parser and
// dispatcher return is a *thor.Error, so the assertion is total.
func (vm *VM) raiseThorError(err error) {
	te := err.(*thor.Error)
	raise(te.Kind.String(), "%s", te.Msg)
}

// thorBuildOption builds a *thor.Option from a name and the gem's option keyword
// Hash (:desc/:type/:required/:default/:lazy_default/:aliases/:banner/:enum/
// :group/:hide/:repeatable), applying Thor's defaults via thor.NewOption (which
// rejects the boolean+required declaration Thor rejects).
func (vm *VM) thorBuildOption(name string, h *object.Hash) (*thor.Option, error) {
	var o thor.Option
	if h != nil {
		if v, ok := thorKw(h, "desc"); ok {
			o.Desc = strArg(v)
		}
		if v, ok := thorKw(h, "type"); ok {
			o.Typ = thor.Type(thorName(v))
		}
		if v, ok := thorKw(h, "required"); ok {
			o.Required = v.Truthy()
		}
		if v, ok := thorKw(h, "default"); ok {
			o.Default = thorValueToGo(v)
		}
		if v, ok := thorKw(h, "lazy_default"); ok {
			o.LazyDefault = thorValueToGo(v)
		}
		if v, ok := thorKw(h, "aliases"); ok {
			o.Aliases = thorStrList(v)
		}
		if v, ok := thorKw(h, "banner"); ok {
			o.Banner = strArg(v)
			o.BannerSet = true
		}
		if v, ok := thorKw(h, "enum"); ok {
			o.Enum = thorStrList(v)
		}
		if v, ok := thorKw(h, "group"); ok {
			o.Group = strArg(v)
		}
		if v, ok := thorKw(h, "hide"); ok {
			o.Hide = v.Truthy()
		}
		if v, ok := thorKw(h, "repeatable"); ok {
			o.Repeatable = v.Truthy()
		}
	}
	return thor.NewOption(name, o)
}

// thorOptionList reads an Array of Thor::Option into a []*thor.Option, raising
// TypeError on any element that is not a Thor::Option.
func (vm *VM) thorOptionList(v object.Value) []*thor.Option {
	arr, ok := v.(*object.Array)
	if !ok {
		raise("TypeError", "wrong argument type %s (expected Array of Thor::Option)", vm.classOf(v).name)
	}
	out := make([]*thor.Option, len(arr.Elems))
	for i, e := range arr.Elems {
		to, ok := e.(*ThorOption)
		if !ok {
			raise("TypeError", "wrong element type %s (expected Thor::Option)", vm.classOf(e).name)
		}
		out[i] = to.o
	}
	return out
}

// thorConfig builds a thor.Config from an optional Ruby Hash
// (:basename/:package_name/:terminal_width), the deterministic knobs of Thor's
// help/usage layout. Getenv stays nil so only TerminalWidth/80 apply.
func thorConfig(h *object.Hash) thor.Config {
	var c thor.Config
	if h != nil {
		if v, ok := thorKw(h, "basename"); ok {
			c.Basename = strArg(v)
		}
		if v, ok := thorKw(h, "package_name"); ok {
			c.PackageName = strArg(v)
		}
		if v, ok := thorKw(h, "terminal_width"); ok {
			if n, ok := v.(object.Integer); ok {
				c.TerminalWidth = int(n)
			}
		}
	}
	return c
}

// --- argument helpers ------------------------------------------------------

// thorOptHash returns args[i] as an *object.Hash (the trailing keyword Hash), or
// nil when the index is out of range or the argument is not a Hash.
func thorOptHash(args []object.Value, i int) *object.Hash {
	if i < len(args) {
		if h, ok := args[i].(*object.Hash); ok {
			return h
		}
	}
	return nil
}

// thorKw returns a keyword option by Symbol or String key, matching the gem's
// symbol-keyed option Hash while tolerating string keys.
func thorKw(h *object.Hash, key string) (object.Value, bool) {
	if v, ok := h.Get(object.Symbol(key)); ok {
		return v, true
	}
	return h.Get(object.NewString(key))
}

// thorIntArg returns args[i] as an int (0 when absent or not an Integer), used
// for Thor::Option#usage's padding argument.
func thorIntArg(args []object.Value, i int) int {
	if i < len(args) {
		if n, ok := args[i].(object.Integer); ok {
			return int(n)
		}
	}
	return 0
}

// thorArgv reads args[i] as a []string argv from a Ruby Array (nil when absent).
func thorArgv(args []object.Value, i int) []string {
	if i >= len(args) {
		return nil
	}
	arr, ok := args[i].(*object.Array)
	if !ok {
		return nil
	}
	out := make([]string, len(arr.Elems))
	for j, e := range arr.Elems {
		out[j] = strArg(e)
	}
	return out
}

// thorName coerces a :type/name argument (a Symbol like :boolean or a String) to
// its plain string, matching Thor's Symbol-or-String option declarations.
func thorName(v object.Value) string {
	if s, ok := v.(object.Symbol); ok {
		return string(s)
	}
	return strArg(v)
}

// thorStrList maps an aliases/enum argument to a []string: an Array's elements by
// #to_s, or a single scalar wrapped as one entry (Thor accepts either).
func thorStrList(v object.Value) []string {
	if arr, ok := v.(*object.Array); ok {
		out := make([]string, len(arr.Elems))
		for i, e := range arr.Elems {
			out[i] = strArg(e)
		}
		return out
	}
	return []string{strArg(v)}
}

// --- Thor value model ⇄ Ruby -----------------------------------------------

// thorValueToGo maps a Ruby value to the Go value Thor's model uses: nil, bool,
// string, int64, float64, []string (Array), or *thor.OrderedMap (Hash), mirroring
// the mapping Thor's parser produces.
func thorValueToGo(v object.Value) any {
	switch x := v.(type) {
	case object.Integer:
		return int64(x)
	case object.Float:
		return float64(x)
	case object.Bool:
		return bool(x)
	case *object.String:
		return x.Str()
	case object.Symbol:
		return string(x)
	case *object.Array:
		out := make([]string, len(x.Elems))
		for i, e := range x.Elems {
			out[i] = e.ToS()
		}
		return out
	case *object.Hash:
		m := thor.NewOrderedMap()
		for _, k := range x.Keys {
			val, _ := x.Get(k)
			m.Set(k.ToS(), val.ToS())
		}
		return m
	}
	if object.IsNil(v) {
		return nil
	}
	return v.ToS()
}

// thorValueToRuby maps a Go value from Thor's model back to a Ruby value.
func thorValueToRuby(v any) object.Value {
	switch x := v.(type) {
	case nil:
		return object.NilV
	case bool:
		return object.Bool(x)
	case string:
		return object.NewString(x)
	case int64:
		return object.IntValue(x)
	case float64:
		return object.Float(x)
	case []string:
		return thorStrArray(x)
	case *thor.OrderedMap:
		h := object.NewHash()
		for _, k := range x.Keys() {
			val, _ := x.Get(k)
			h.Set(object.NewString(k), object.NewString(val))
		}
		return h
	}
	return object.NilV
}

// thorValueMapToHash renders a *thor.ValueMap (option human name → value, in
// declaration order) as a Ruby Hash of String→value, the parsed-options Hash Thor
// hands a command.
func thorValueMapToHash(m *thor.ValueMap) object.Value {
	h := object.NewHash()
	for _, k := range m.Keys() {
		v, _ := m.Get(k)
		h.Set(object.NewString(k), thorValueToRuby(v))
	}
	return h
}

// thorStrArray wraps a []string as a Ruby Array of Strings.
func thorStrArray(ss []string) object.Value {
	elems := make([]object.Value, len(ss))
	for i, s := range ss {
		elems[i] = object.NewString(s)
	}
	return object.NewArrayFromSlice(elems)
}
