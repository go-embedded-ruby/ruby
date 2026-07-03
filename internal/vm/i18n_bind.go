// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	i18n "github.com/go-ruby-i18n/i18n"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin binding between rbgo's Ruby object graph and the
// interpreter-independent github.com/go-ruby-i18n/i18n core. The library owns
// the lookup / interpolation / pluralization / fallback / strftime logic; this
// file only maps rbgo Symbols/Strings/Hashes onto the library's Options and
// Value model, converts results back, adapts a rbgo Date/Time to the library's
// Temporal seam and translates the library's typed errors to the I18n Ruby
// exception tree.

// newI18nInstance builds the process-wide I18n with the gem's default locale
// (:en). It installs the raising missing-interpolation handler so an absent
// %{name} raises I18n::MissingInterpolationArgument, matching the i18n gem's
// out-of-the-box default (its shipped missing_interpolation_argument_handler
// raises). It is created once by registerI18n and shared by every I18n.* call
// and every I18n.backend handle.
func newI18nInstance() *i18n.I18n {
	inst := i18n.New("en")
	inst.SetMissingInterpolationHandler(i18n.RaiseMissingInterpolation)
	return inst
}

// i18nLocaleArg coerces a locale argument (a Symbol or String) to its bare name.
func i18nLocaleArg(v object.Value) string { return i18nStr(v) }

// i18nKeyArg coerces a translation key (a Symbol or String) to its bare name.
func i18nKeyArg(v object.Value) string { return i18nStr(v) }

// i18nStr renders a Symbol or String as its text; any other value falls back to
// its to_s, so a stray argument does not crash the lookup.
func i18nStr(v object.Value) string {
	switch s := v.(type) {
	case object.Symbol:
		return string(s)
	case *object.String:
		return s.Str()
	}
	return v.ToS()
}

// i18nSymbolArray wraps a []string of locale names as a Ruby Array of Symbols
// (the gem returns symbols from available_locales).
func i18nSymbolArray(ss []string) object.Value {
	elems := make([]object.Value, len(ss))
	for i, s := range ss {
		elems[i] = object.Symbol(s)
	}
	return object.NewArrayFromSlice(elems)
}

// i18nOptions builds the library Options from the trailing keyword Hash of a
// translate call. :locale, :scope, :count, :default and :raise are the
// structural keys; every other key is an interpolation variable. bang forces
// Raise on (I18n.t! / translate!).
func (vm *VM) i18nOptions(args []object.Value, bang bool) *i18n.Options {
	opts := &i18n.Options{Raise: bang}
	h, ok := lastHash(args)
	if !ok {
		return opts
	}
	values := map[string]i18n.Value{}
	for _, k := range h.Keys {
		val, _ := h.Get(k)
		switch i18nStr(k) {
		case "locale":
			opts.Locale = i18nLocaleArg(val)
		case "scope":
			opts.Scope = i18nScope(val)
		case "count":
			if n, ok := val.(object.Integer); ok {
				c := int(n)
				opts.Count = &c
			}
		case "default":
			opts.Default = i18nDefaults(val)
		case "raise":
			if val.Truthy() {
				opts.Raise = true
			}
		default:
			values[i18nStr(k)] = vm.toI18nValue(val)
		}
	}
	if len(values) > 0 {
		opts.Values = values
	}
	return opts
}

// i18nLocalizeOptions parses the trailing keyword Hash of a localize call: it
// returns the format (a Symbol names a stored format, looked up with named=true;
// a String is a literal strftime pattern with named=false) and an Options
// carrying :locale. The default format is the named :default.
func (vm *VM) i18nLocalizeOptions(args []object.Value) (format string, named bool, opts *i18n.Options) {
	format, named, opts = "default", true, &i18n.Options{}
	h, ok := lastHash(args)
	if !ok {
		return format, named, opts
	}
	for _, k := range h.Keys {
		val, _ := h.Get(k)
		switch i18nStr(k) {
		case "format":
			switch f := val.(type) {
			case object.Symbol:
				format, named = string(f), true
			case *object.String:
				format, named = f.Str(), false
			default:
				format, named = val.ToS(), false
			}
		case "locale":
			opts.Locale = i18nLocaleArg(val)
		}
	}
	return format, named, opts
}

// i18nScope coerces a :scope option to a []string: a single Symbol/String is one
// entry, an Array yields one entry per element. Dotted entries are split by the
// library itself.
func i18nScope(v object.Value) []string {
	if arr, ok := v.(*object.Array); ok {
		out := make([]string, len(arr.Elems))
		for i, e := range arr.Elems {
			out[i] = i18nStr(e)
		}
		return out
	}
	return []string{i18nStr(v)}
}

// i18nDefaults coerces a :default option to a []DefaultEntry: a Symbol is a key
// default (looked up if reached), any other value is a literal; an Array yields
// one entry per element in order.
func i18nDefaults(v object.Value) []i18n.DefaultEntry {
	if arr, ok := v.(*object.Array); ok {
		out := make([]i18n.DefaultEntry, len(arr.Elems))
		for i, e := range arr.Elems {
			out[i] = i18nDefaultEntry(e)
		}
		return out
	}
	return []i18n.DefaultEntry{i18nDefaultEntry(v)}
}

// i18nDefaultEntry maps one :default element: a Symbol becomes a key default, any
// other value a literal (its text).
func i18nDefaultEntry(v object.Value) i18n.DefaultEntry {
	if sym, ok := v.(object.Symbol); ok {
		return i18n.Key(string(sym))
	}
	return i18n.Lit(i18nStr(v))
}

// i18nDataArg coerces the data argument of store_translations to the library's
// nested map[string]Value tree. A non-Hash argument stores nothing (an empty
// tree), matching the gem, which deep-merges a Hash.
func i18nDataArg(v object.Value) map[string]i18n.Value {
	h, ok := v.(*object.Hash)
	if !ok {
		return map[string]i18n.Value{}
	}
	return i18nHashData(h)
}

// i18nHashData converts a Ruby Hash to a symbol-keyed map[string]Value tree,
// recursing into nested Hashes and Arrays so a whole translation subtree stores
// in one call.
func i18nHashData(h *object.Hash) map[string]i18n.Value {
	out := make(map[string]i18n.Value, len(h.Keys))
	for _, k := range h.Keys {
		val, _ := h.Get(k)
		out[i18nStr(k)] = toI18nData(val)
	}
	return out
}

// toI18nData converts a stored translation value (String/Symbol/number/bool/nil,
// or a nested Hash/Array) to the library's Value model.
func toI18nData(v object.Value) i18n.Value {
	switch x := v.(type) {
	case *object.String:
		return x.Str()
	case object.Symbol:
		return string(x)
	case object.Integer:
		return int(x)
	case object.Float:
		return float64(x)
	case object.Bool:
		return bool(x)
	case object.Nil:
		return nil
	case *object.Array:
		out := make([]i18n.Value, len(x.Elems))
		for i, e := range x.Elems {
			out[i] = toI18nData(e)
		}
		return out
	case *object.Hash:
		return i18nHashData(x)
	}
	return v.ToS()
}

// toI18nValue converts an interpolation variable to the library's Value model:
// numbers stay numeric (so %<count>d formats), everything else is coerced the
// same way stored data is.
func (vm *VM) toI18nValue(v object.Value) i18n.Value { return toI18nData(v) }

// fromI18nValue converts a resolved library Value back to a Ruby value: a leaf
// String/number/bool/nil maps directly, and a subtree (Array / symbol-keyed map)
// maps to a Ruby Array / Hash with Symbol keys, mirroring the gem returning the
// raw translation data for a non-leaf key.
func (vm *VM) fromI18nValue(v i18n.Value) object.Value {
	switch x := v.(type) {
	case string:
		return object.NewString(x)
	case int:
		return object.IntValue(int64(x))
	case int64:
		return object.IntValue(x)
	case float64:
		return object.Float(x)
	case bool:
		return object.Bool(x)
	case nil:
		return object.NilV
	case []i18n.Value:
		elems := make([]object.Value, len(x))
		for i, e := range x {
			elems[i] = vm.fromI18nValue(e)
		}
		return object.NewArrayFromSlice(elems)
	case map[string]i18n.Value:
		h := object.NewHash()
		for k, val := range x {
			h.Set(object.Symbol(k), vm.fromI18nValue(val))
		}
		return h
	}
	return object.NewString(i18n.AsString(v))
}

// raiseI18n maps a library error to the matching I18n Ruby exception. Every
// typed error the library returns has a dedicated class under I18n; anything
// unrecognised falls back to I18n::ArgumentError, the tree's base.
func (vm *VM) raiseI18n(err error) object.Value {
	switch e := err.(type) {
	case *i18n.MissingTranslation:
		return raise("I18n::MissingTranslationData", "%s", e.Message())
	case *i18n.MissingInterpolationArgument:
		return raise("I18n::MissingInterpolationArgument", "%s", e.Error())
	case *i18n.ReservedInterpolationKey:
		return raise("I18n::ReservedInterpolationKey", "%s", e.Error())
	case *i18n.InvalidPluralizationData:
		return raise("I18n::InvalidPluralizationData", "%s", e.Error())
	}
	return raise("I18n::ArgumentError", "%s", err.Error())
}

// temporalOf adapts a rbgo Date/Time/DateTime to the library's Temporal seam by
// reading its broken-down fields through Ruby method sends. HasTime selects the
// "time" vs "date" format tree: a plain Date has no clock, a Time or DateTime
// does. rbgo defines hour/min/sec on Date too (a Date reports midnight), so the
// gem's respond_to?(:sec) discriminator would misfire here; the class name is
// the reliable signal — anything other than a bare Date that still exposes #hour
// carries a clock.
func (vm *VM) temporalOf(v object.Value) i18n.Temporal {
	hasTime := vm.classOf(v).name != "Date" && vm.respondsTo(v, "hour")
	return &i18nTemporal{vm: vm, obj: v, hasTime: hasTime}
}

// i18nTemporal is the Temporal adapter over a rbgo date/time value. Each accessor
// sends the corresponding Ruby method, guarding with respond_to? so a bare Date
// (no clock) yields zeros for the time fields rather than raising.
type i18nTemporal struct {
	vm      *VM
	obj     object.Value
	hasTime bool
}

func (t *i18nTemporal) HasTime() bool { return t.hasTime }
func (t *i18nTemporal) Year() int     { return t.field("year") }
func (t *i18nTemporal) Month() int    { return t.field("month") }
func (t *i18nTemporal) Day() int      { return t.field("day") }
func (t *i18nTemporal) Hour() int     { return t.field("hour") }
func (t *i18nTemporal) Min() int      { return t.field("min") }
func (t *i18nTemporal) Sec() int      { return t.field("sec") }
func (t *i18nTemporal) Nsec() int     { return t.field("nsec") }
func (t *i18nTemporal) Wday() int     { return t.field("wday") }
func (t *i18nTemporal) Yday() int     { return t.field("yday") }

// ZoneOffset reads Time#utc_offset (a bare Date has none, so 0).
func (t *i18nTemporal) ZoneOffset() int { return t.field("utc_offset") }

// ZoneName reads Time#zone as its abbreviated name, "" when the value has no
// zone (a bare Date, or a nil zone).
func (t *i18nTemporal) ZoneName() string {
	if !t.vm.respondsTo(t.obj, "zone") {
		return ""
	}
	if s, ok := t.vm.send(t.obj, "zone", nil, nil).(*object.String); ok {
		return s.Str()
	}
	return ""
}

// field sends a broken-down accessor and returns its Integer value, or 0 when
// the value does not respond to it (a bare Date has no hour/min/sec/utc_offset).
func (t *i18nTemporal) field(name string) int {
	if !t.vm.respondsTo(t.obj, name) {
		return 0
	}
	if n, ok := t.vm.send(t.obj, name, nil, nil).(object.Integer); ok {
		return int(n)
	}
	return 0
}

// lastHash returns the trailing Hash argument (the keyword options) when the
// last positional argument is a Hash, matching how rbgo passes keyword args.
func lastHash(args []object.Value) (*object.Hash, bool) {
	if len(args) == 0 {
		return nil, false
	}
	h, ok := args[len(args)-1].(*object.Hash)
	return h, ok
}
