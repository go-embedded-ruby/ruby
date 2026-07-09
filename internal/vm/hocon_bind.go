// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"math"
	"sort"

	hocon "github.com/go-ruby-hocon/hocon"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// hoconStrArg reads the single document/path string argument, raising
// ArgumentError when it is missing.
func hoconStrArg(args []object.Value) string {
	if len(args) == 0 {
		raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
	}
	return args[0].ToS()
}

// hoconPathArg reads the single dotted-path argument for a Config accessor.
func hoconPathArg(args []object.Value) string {
	if len(args) == 0 {
		raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
	}
	return args[0].ToS()
}

// hoconParse parses a HOCON document, raising Hocon::ConfigError on a syntax
// error.
func hoconParse(src string) object.Value {
	c, err := hocon.Parse(src)
	if err != nil {
		raise("Hocon::ConfigError", "%s", err.Error())
	}
	return &HoconConfig{c: c}
}

// registerHoconConfig installs the Hocon::Config instance surface.
func (vm *VM) registerHoconConfig(cls *RClass) {
	self := func(v object.Value) *hocon.Config { return v.(*HoconConfig).c }

	cls.define("get_string", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		s, err := self(v).GetString(hoconPathArg(args))
		hoconRaise(err)
		return object.NewString(s)
	})
	cls.define("get_int", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		n, err := self(v).GetInt(hoconPathArg(args))
		hoconRaise(err)
		return object.IntValue(n)
	})
	cls.define("get_boolean", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		b, err := self(v).GetBoolean(hoconPathArg(args))
		hoconRaise(err)
		return object.Bool(b)
	})
	cls.define("get_double", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		f, err := self(v).GetDouble(hoconPathArg(args))
		hoconRaise(err)
		return object.Float(f)
	})
	cls.define("get_list", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		list, err := self(v).GetList(hoconPathArg(args))
		hoconRaise(err)
		out := make([]object.Value, len(list))
		for i, cv := range list {
			out[i] = hoconValueToRuby(cv)
		}
		return object.NewArrayFromSlice(out)
	})
	cls.define("get_config", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		sub, err := self(v).GetConfig(hoconPathArg(args))
		hoconRaise(err)
		return &HoconConfig{c: sub}
	})
	cls.define("get_duration", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		d, err := self(v).GetDuration(hoconPathArg(args))
		hoconRaise(err)
		return object.IntValue(int64(d))
	})
	cls.define("get_bytes", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		n, err := self(v).GetBytes(hoconPathArg(args))
		hoconRaise(err)
		return object.IntValue(n)
	})
	cls.define("has_path?", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).HasPath(hoconPathArg(args)))
	})

	// [](path) — the value at path converted to a native Ruby value, or nil when
	// the path is absent.
	cls.define("[]", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		cv, err := self(v).GetValue(hoconPathArg(args))
		if err != nil {
			return object.NilV
		}
		return hoconValueToRuby(cv)
	})

	// root — the whole config as a native Ruby Hash.
	cls.define("root", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return hoconValueToRuby(self(v).Root())
	})

	// render — the config serialised back to HOCON text.
	cls.define("render", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).Render(hocon.DefaultRenderOptions()))
	})

	// with_fallback(other) — this config layered over another.
	cls.define("with_fallback", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		o, ok := args[0].(*HoconConfig)
		if !ok {
			raise("ArgumentError", "with_fallback requires a Hocon::Config")
		}
		return &HoconConfig{c: self(v).WithFallback(o.c)}
	})
}

// hoconRaise turns a non-nil accessor error into a Hocon::ConfigError.
func hoconRaise(err error) {
	if err != nil {
		raise("Hocon::ConfigError", "%s", err.Error())
	}
}

// hoconValueToRuby maps a resolved HOCON value onto its Ruby counterpart,
// recursing through objects and arrays. Numbers that are whole become Integer,
// matching how the gem renders them. The six ConfigValueType cases are
// exhaustive; object is handled as the fall-through so there is no unreachable
// default arm.
func hoconValueToRuby(cv *hocon.ConfigValue) object.Value {
	switch cv.Type() {
	case hocon.NullType:
		return object.NilV
	case hocon.BooleanType:
		return object.Bool(cv.Unwrap().(bool))
	case hocon.NumberType:
		f := cv.Unwrap().(float64)
		if f == math.Trunc(f) && f >= math.MinInt64 && f <= math.MaxInt64 {
			return object.IntValue(int64(f))
		}
		return object.Float(f)
	case hocon.StringType:
		return object.NewString(cv.Unwrap().(string))
	case hocon.ArrayType:
		elems := cv.Elements()
		out := make([]object.Value, len(elems))
		for i, e := range elems {
			out[i] = hoconValueToRuby(e)
		}
		return object.NewArrayFromSlice(out)
	}
	// hocon.ObjectType — the only remaining type.
	fields := cv.Fields()
	h := object.NewHash()
	for _, k := range hoconSortedKeys(fields) {
		h.Set(object.NewString(k), hoconValueToRuby(fields[k]))
	}
	return h
}

// hoconSortedKeys returns a field map's keys in sorted order so objects convert
// to Ruby Hashes deterministically.
func hoconSortedKeys(m map[string]*hocon.ConfigValue) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
