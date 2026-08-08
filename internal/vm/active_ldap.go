// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	activeldap "github.com/go-ruby-activeldap/activeldap"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerActiveLdap installs the ActiveLdap ORM (require "active_ldap"): the
// ActiveRecord-style LDAP mapper of the `activeldap` gem, backed by
// github.com/go-ruby-activeldap/activeldap. The mapping, DN, RFC 4515 filter,
// dirty-diff and LDIF logic live in that pure-Go library over a Directory seam —
// the four Net::LDAP operations (search / add / modify / delete). rbgo wires the
// seam two ways:
//
//   - ActiveLdap.mock_directory — the library's in-memory MockDirectory, so the
//     whole ORM runs with no server (the offline/testing path);
//   - ActiveLdap.directory(conn) — wraps any object answering the Net::LDAP
//     surface (#search/#add/#modify/#delete), i.e. a Net::LDAP connection from
//     go-ruby-ldap, so a model reads and writes a real directory.
//
// A connection binds a directory to a base DN; a model compiles an ldap_mapping
// (dn_attribute/prefix/classes/scope) against a connection; records answer the
// familiar find/create/save/update/destroy/valid?/to_ldif surface. The
// ActiveLdap::Error tree is registered so a failure rescues as the gem-faithful
// class.
func (vm *VM) registerActiveLdap() {
	mod := newClass("ActiveLdap", nil)
	mod.isModule = true
	vm.consts["ActiveLdap"] = mod

	vm.registerActiveLdapErrors(mod)
	classes := vm.registerActiveLdapClasses(mod)

	// ActiveLdap.mock_directory — an in-memory Directory needing no server.
	mod.smethods["mock_directory"] = &Method{name: "mock_directory", owner: mod, native: func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return &ALDirectoryObj{cls: classes.dir, dir: activeldap.NewMockDirectory()}
	}}

	// ActiveLdap.directory(conn) — wrap a Net::LDAP-shaped object as a Directory.
	mod.smethods["directory"] = &Method{name: "directory", owner: mod, native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "ActiveLdap.directory requires a Net::LDAP-shaped connection")
		}
		return &ALDirectoryObj{cls: classes.dir, dir: &rubyDirectory{vm: vm, obj: args[0]}}
	}}

	// ActiveLdap.connection(directory, base:) — bind a directory to a base DN.
	mod.smethods["connection"] = &Method{name: "connection", owner: mod, native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return alConnection(classes, args)
	}}

	// ActiveLdap.model(name, connection, dn_attribute:, prefix:, classes:, scope:,
	// aliases:, single_valued:) — compile an ldap_mapping into a model.
	mod.smethods["model"] = &Method{name: "model", owner: mod, native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return alModel(classes, args)
	}}

	// ActiveLdap.escape_filter(value) — RFC 4515 assertion-value escaping, the
	// class-level helper mirroring Net::LDAP::Filter.escape.
	mod.smethods["escape_filter"] = &Method{name: "escape_filter", owner: mod, native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(activeldap.EscapeFilterValue(strArg(args[0])))
	}}
}

// alClasses bundles the four ActiveLdap wrapper classes, threaded through the
// factory methods so a wrapper can mint its children (a model builds records).
type alClasses struct {
	dir, conn, model, record *RClass
}

// registerActiveLdapErrors installs ActiveLdap::Error < StandardError plus
// EntryNotFound (a miss find), RecordInvalid (save! on invalid) and
// ConnectionError (a directory failure) beneath it.
func (vm *VM) registerActiveLdapErrors(mod *RClass) {
	std := vm.consts["StandardError"].(*RClass)
	base := newClass("ActiveLdap::Error", std)
	mod.consts["Error"] = base
	vm.consts["ActiveLdap::Error"] = base
	for _, name := range []string{"EntryNotFound", "RecordInvalid", "ConnectionError"} {
		c := newClass("ActiveLdap::"+name, base)
		mod.consts[name] = c
		vm.consts["ActiveLdap::"+name] = c
	}
}

// registerActiveLdapClasses installs the Directory / Connection / Model / Record
// classes and returns them bundled.
func (vm *VM) registerActiveLdapClasses(mod *RClass) alClasses {
	c := alClasses{
		dir:    newClass("ActiveLdap::Directory", vm.cObject),
		conn:   newClass("ActiveLdap::Connection", vm.cObject),
		model:  newClass("ActiveLdap::Model", vm.cObject),
		record: newClass("ActiveLdap::Record", vm.cObject),
	}
	for _, e := range []struct {
		name string
		cls  *RClass
	}{{"Directory", c.dir}, {"Connection", c.conn}, {"Model", c.model}, {"Record", c.record}} {
		mod.consts[e.name] = e.cls
		vm.consts["ActiveLdap::"+e.name] = e.cls
	}
	c.conn.define("base", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self.(*ALConnectionObj).conn.Base)
	})
	registerALModelMethods(c.model)
	registerALRecordMethods(c.record)
	return c
}

// alConnection implements ActiveLdap.connection(directory, base:).
func alConnection(classes alClasses, args []object.Value) object.Value {
	if len(args) == 0 {
		raise("ArgumentError", "ActiveLdap.connection requires a directory")
	}
	d, ok := args[0].(*ALDirectoryObj)
	if !ok {
		raise("TypeError", "ActiveLdap.connection expects an ActiveLdap::Directory")
	}
	base := ""
	if len(args) > 1 {
		if h, ok := args[1].(*object.Hash); ok {
			if v, ok := alKw(h, "base"); ok {
				base = strArg(v)
			}
		}
	}
	return &ALConnectionObj{cls: classes.conn, conn: activeldap.NewConnection(d.dir, base)}
}

// alModel implements ActiveLdap.model(name, connection, **mapping).
func alModel(classes alClasses, args []object.Value) object.Value {
	if len(args) < 2 {
		raise("ArgumentError", "ActiveLdap.model requires a name and a connection")
	}
	name := strArg(args[0])
	conn, ok := args[1].(*ALConnectionObj)
	if !ok {
		raise("TypeError", "ActiveLdap.model expects an ActiveLdap::Connection")
	}
	m := &activeldap.Mapping{Scope: activeldap.ScopeSub}
	if len(args) > 2 {
		if h, ok := args[2].(*object.Hash); ok {
			alBuildMapping(m, h)
		}
	}
	if m.DNAttribute == "" {
		raise("ActiveLdap::Error", "ldap_mapping requires a dn_attribute")
	}
	return &ALModelObj{cls: classes.model, recordCls: classes.record, model: activeldap.NewClass(name, m, conn.conn)}
}

// alBuildMapping fills a Mapping from the keyword Hash of ActiveLdap.model.
func alBuildMapping(m *activeldap.Mapping, h *object.Hash) {
	if v, ok := alKw(h, "dn_attribute"); ok {
		m.DNAttribute = strArg(v)
	}
	if v, ok := alKw(h, "prefix"); ok {
		m.Prefix = strArg(v)
	}
	if v, ok := alKw(h, "classes"); ok {
		m.Classes = rubyStrList(v)
	}
	if v, ok := alKw(h, "scope"); ok {
		if s, ok := activeldap.ParseScope(strArg(v)); ok {
			m.Scope = s
		}
	}
	if v, ok := alKw(h, "aliases"); ok {
		if ah, ok := v.(*object.Hash); ok {
			m.Aliases = map[string]string{}
			for i := 0; i < ah.Len(); i++ {
				k := ah.Keys[i]
				val, _ := ah.Get(k)
				m.Aliases[strArg(k)] = strArg(val)
			}
		}
	}
	if v, ok := alKw(h, "single_valued"); ok {
		m.SingleValued = rubyStrList(v)
	}
}

// alKw reads a keyword by symbol or string key from a kwargs/options Hash.
func alKw(h *object.Hash, name string) (object.Value, bool) {
	if v, ok := h.Get(object.SymVal(name)); ok {
		return v, true
	}
	return h.Get(object.NewString(name))
}
