// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	activeldap "github.com/go-ruby-activeldap/activeldap"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin binding between rbgo's Ruby object graph and the pure-Go
// ActiveLdap ORM core (github.com/go-ruby-activeldap/activeldap). The mapping,
// DN, filter, dirty-diff and LDIF logic live in that library over a Directory
// seam; rbgo supplies the seam (either the library's in-memory MockDirectory or
// a wrapper over a Net::LDAP-shaped Ruby object) and maps values across the
// boundary.

// --- wrapper types ----------------------------------------------------------

// ALDirectoryObj wraps an activeldap.Directory (ActiveLdap::Directory).
type ALDirectoryObj struct {
	cls *RClass
	dir activeldap.Directory
}

func (d *ALDirectoryObj) ToS() string     { return "#<ActiveLdap::Directory>" }
func (d *ALDirectoryObj) Inspect() string { return d.ToS() }
func (d *ALDirectoryObj) Truthy() bool    { return true }

// ALConnectionObj wraps an *activeldap.Connection (ActiveLdap::Connection).
type ALConnectionObj struct {
	cls  *RClass
	conn *activeldap.Connection
}

func (c *ALConnectionObj) ToS() string     { return "#<ActiveLdap::Connection base=" + c.conn.Base + ">" }
func (c *ALConnectionObj) Inspect() string { return c.ToS() }
func (c *ALConnectionObj) Truthy() bool    { return true }

// ALModelObj wraps a compiled *activeldap.Class (ActiveLdap::Model); recordCls is
// the class its finders wrap results in.
type ALModelObj struct {
	cls       *RClass
	recordCls *RClass
	model     *activeldap.Class
}

func (m *ALModelObj) ToS() string     { return "#<ActiveLdap::Model " + m.model.Name() + ">" }
func (m *ALModelObj) Inspect() string { return m.ToS() }
func (m *ALModelObj) Truthy() bool    { return true }

// ALRecordObj wraps an *activeldap.Base (ActiveLdap::Record).
type ALRecordObj struct {
	cls *RClass
	rec *activeldap.Base
}

func (r *ALRecordObj) ToS() string     { return "#<ActiveLdap::Record " + r.rec.ToS() + ">" }
func (r *ALRecordObj) Inspect() string { return r.ToS() }
func (r *ALRecordObj) Truthy() bool    { return true }

func (m *ALModelObj) wrap(rec *activeldap.Base) *ALRecordObj {
	return &ALRecordObj{cls: m.recordCls, rec: rec}
}

// --- Model methods ----------------------------------------------------------

func registerALModelMethods(cls *RClass) {
	d := func(name string, fn NativeFn) { cls.define(name, fn) }
	self := func(v object.Value) *ALModelObj { return v.(*ALModelObj) }

	d("name", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).model.Name())
	})
	d("base_dn", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).model.BaseDN())
	})
	d("build", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		m := self(v)
		rec := m.model.New()
		if len(args) > 0 {
			if h, ok := args[0].(*object.Hash); ok {
				alAssign(rec, h)
			}
		}
		return m.wrap(rec)
	})
	d("new", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NilV // records are minted via build/create; .new is not the entry point
	})
	d("create", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		m := self(v)
		attrs := map[string][]string{}
		if len(args) > 0 {
			if h, ok := args[0].(*object.Hash); ok {
				attrs = alAttrsFromHash(h)
			}
		}
		rec, err := m.model.Create(attrs)
		if _, bad := err.(*activeldap.ValidationError); err != nil && !bad {
			alRaise(err)
		}
		return m.wrap(rec)
	})
	d("find", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		m := self(v)
		rec, err := m.model.Find(strArg(args[0]))
		alRaise(err)
		return m.wrap(rec)
	})
	d("find_first", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		m := self(v)
		rec, err := m.model.FindFirst(alFindOptions(args))
		alRaise(err)
		if rec == nil {
			return object.NilV
		}
		return m.wrap(rec)
	})
	search := func(vm *VM, v object.Value, args []object.Value, blk *Proc) object.Value {
		m := self(v)
		recs, err := m.model.Search(alFindOptions(args))
		alRaise(err)
		out := object.NewArrayFromSlice(make([]object.Value, len(recs)))
		for i, r := range recs {
			wr := m.wrap(r)
			out.Elems[i] = wr
			if blk != nil {
				vm.callBlock(blk, []object.Value{wr})
			}
		}
		return out
	}
	d("search", search)
	d("find_all", search)
	d("exist?", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		ok, err := self(v).model.Exist(strArg(args[0]))
		alRaise(err)
		return object.Bool(ok)
	})
}

// alFindOptions builds activeldap.FindOptions from a trailing options Hash with
// filter: (String), base:, scope:, limit: keys.
func alFindOptions(args []object.Value) activeldap.FindOptions {
	var opts activeldap.FindOptions
	if len(args) == 0 {
		return opts
	}
	h, ok := args[0].(*object.Hash)
	if !ok {
		return opts
	}
	if v, ok := alKw(h, "filter"); ok {
		opts.Filter = activeldap.RawFilter(strArg(v))
	}
	if v, ok := alKw(h, "base"); ok {
		opts.Base = strArg(v)
	}
	if v, ok := alKw(h, "scope"); ok {
		if s, ok := activeldap.ParseScope(strArg(v)); ok {
			opts.Scope = &s
		}
	}
	if v, ok := alKw(h, "limit"); ok {
		opts.Limit = int(intArg(v))
	}
	if v, ok := alKw(h, "attributes"); ok {
		opts.Attributes = rubyStrList(v)
	}
	return opts
}

// --- Record methods ---------------------------------------------------------

func registerALRecordMethods(cls *RClass) {
	d := func(name string, fn NativeFn) { cls.define(name, fn) }
	self := func(v object.Value) *ALRecordObj { return v.(*ALRecordObj) }

	d("[]", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return goStrsToRuby(self(v).rec.Get(strArg(args[0])))
	})
	d("[]=", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		self(v).rec.Set(strArg(args[0]), rubyStrList(args[1])...)
		return args[1]
	})
	d("one", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).rec.One(strArg(args[0])))
	})
	d("id", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).rec.ID())
	})
	d("dn", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).rec.DN())
	})
	d("persisted?", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).rec.Persisted())
	})
	d("new_record?", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).rec.NewRecord())
	})
	d("changed?", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).rec.Changed())
	})
	d("attributes", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return goAttrsToRuby(self(v).rec.Attributes())
	})
	d("to_ldif", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).rec.ToLDIF())
	})
	d("valid?", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).rec.Valid())
	})
	d("errors", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		msgs := self(v).rec.Errors().FullMessages()
		out := object.NewArrayFromSlice(make([]object.Value, len(msgs)))
		for i, m := range msgs {
			out.Elems[i] = object.NewString(m)
		}
		return out
	})
	d("save", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		err := self(v).rec.Save()
		if err == nil {
			return object.Bool(true)
		}
		if _, ok := err.(*activeldap.ValidationError); ok {
			return object.Bool(false)
		}
		alRaise(err)
		return object.Bool(false)
	})
	d("save!", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		alRaise(self(v).rec.Save())
		return v
	})
	d("update", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		r := self(v)
		if len(args) > 0 {
			if h, ok := args[0].(*object.Hash); ok {
				alAssign(r.rec, h)
			}
		}
		err := r.rec.Save()
		if err == nil {
			return object.Bool(true)
		}
		if _, ok := err.(*activeldap.ValidationError); ok {
			return object.Bool(false)
		}
		alRaise(err)
		return object.Bool(false)
	})
	d("destroy", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		alRaise(self(v).rec.Destroy())
		return v
	})
	d("reload", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		alRaise(self(v).rec.Reload())
		return v
	})
}

// alAssign sets each key of an attributes Hash on a record.
func alAssign(rec *activeldap.Base, h *object.Hash) {
	for i := 0; i < h.Len(); i++ {
		k := h.Keys[i]
		val, _ := h.Get(k)
		rec.Set(rubyToStr(k), rubyStrList(val)...)
	}
}

// alAttrsFromHash converts a Ruby attributes Hash to the Go create map.
func alAttrsFromHash(h *object.Hash) map[string][]string {
	out := map[string][]string{}
	for i := 0; i < h.Len(); i++ {
		k := h.Keys[i]
		val, _ := h.Get(k)
		out[rubyToStr(k)] = rubyStrList(val)
	}
	return out
}

// alRaise maps a core error onto the ActiveLdap exception tree (a nil error is a
// no-op): EntryNotFound for a missed find, RecordInvalid for a failed validation,
// ConnectionError for a directory failure.
func alRaise(err error) {
	switch err.(type) {
	case nil:
		return
	case *activeldap.EntryNotFoundError:
		raise("ActiveLdap::EntryNotFound", "%s", err.Error())
	case *activeldap.ValidationError:
		raise("ActiveLdap::RecordInvalid", "%s", err.Error())
	default:
		raise("ActiveLdap::ConnectionError", "%s", err.Error())
	}
}

// --- value conversions ------------------------------------------------------

// rubyToStr renders a Ruby value used as an attribute name or scalar value as a
// Go string: a String yields its bytes, anything else its to_s.
func rubyToStr(v object.Value) string {
	if s, ok := v.(*object.String); ok {
		return s.Str()
	}
	return v.ToS()
}

// rubyStrList maps a Ruby value to a []string of LDAP attribute values: an Array
// becomes its per-element strings, nil becomes nil, and any scalar becomes a
// one-element list.
func rubyStrList(v object.Value) []string {
	switch n := v.(type) {
	case nil, object.Nil:
		return nil
	case *object.Array:
		out := make([]string, len(n.Elems))
		for i, e := range n.Elems {
			out[i] = rubyToStr(e)
		}
		return out
	default:
		return []string{rubyToStr(v)}
	}
}

// goStrsToRuby maps a []string of attribute values to a Ruby Array of Strings; a
// nil slice becomes nil (an absent attribute), matching ActiveLdap's #[] on a
// missing attribute.
func goStrsToRuby(vals []string) object.Value {
	if vals == nil {
		return object.NilV
	}
	out := object.NewArrayFromSlice(make([]object.Value, len(vals)))
	for i, s := range vals {
		out.Elems[i] = object.NewString(s)
	}
	return out
}

// goAttrsToRuby maps a name→values attribute map to a Ruby Hash of String→Array.
func goAttrsToRuby(attrs map[string][]string) *object.Hash {
	h := object.NewHash()
	for name, vals := range attrs {
		h.Set(object.NewString(name), goStrsToRuby(vals))
	}
	return h
}
