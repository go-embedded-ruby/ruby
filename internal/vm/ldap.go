// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"

	ldap "github.com/go-ruby-ldap/ldap"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerNetLDAP installs the Net::LDAP class (require "net/ldap"): the
// net-ldap-gem-flavoured LDAP client, reimplemented in pure Go (CGO=0) by
// github.com/go-ruby-ldap/ldap on top of the official github.com/go-ldap/ldap/v3
// transport. The library owns the protocol; this file is the thin shell mapping
// its surface onto rbgo classes:
//
//	Net::LDAP.new(host:, port:, base:, auth:) — the connection: #bind, #search
//	                                            (block or Array), #add, #modify,
//	                                            #delete, #rename, #compare,
//	                                            #get_operation_result
//	Net::LDAP::Filter — the filter builder (eq/present/ge/le/substrings/&/|/negate)
//	Net::LDAP::Entry  — one search result (dn + case-insensitive attributes)
//	Net::LDAP::OperationResult — the last operation's code/message/matched DN
//	Net::LDAP::Error (< StandardError) — the result-code error tree
//
// It nests under the Net module, so it runs after registerNetHTTP (which creates
// Net). The value types, argument coercions and the error bridge live in
// ldap_bind.go. Operations return the booleans / arrays net-ldap returns and
// record the outcome in #get_operation_result; only a broken connection (a
// network-class failure) re-raises a Net::LDAP exception.
func (vm *VM) registerNetLDAP() {
	netMod := vm.consts["Net"].(*RClass)

	conn := newClass("Net::LDAP", vm.cObject)
	netMod.consts["LDAP"] = conn
	vm.consts["Net::LDAP"] = conn

	cl := &ldapClasses{conn: conn}
	cl.entry = vm.registerLDAPNested(conn, "Entry")
	cl.filter = vm.registerLDAPNested(conn, "Filter")
	cl.result = vm.registerLDAPNested(conn, "OperationResult")

	vm.registerLDAPErrors(conn)
	vm.registerLDAPScopes(conn)
	vm.registerLDAPClient(cl)
	vm.registerLDAPEntry(cl)
	vm.registerLDAPFilter(cl)
	vm.registerLDAPResult(cl)
}

// registerLDAPNested creates a class named simple nested under Net::LDAP,
// publishing it both scoped (Net::LDAP::Entry) and flat in vm.consts so raise
// and classOf resolve it by qualified name.
func (vm *VM) registerLDAPNested(conn *RClass, simple string) *RClass {
	full := "Net::LDAP::" + simple
	cls := newClass(full, vm.cObject)
	conn.consts[simple] = cls
	vm.consts[full] = cls
	return cls
}

// registerLDAPErrors installs the Net::LDAP exception tree: Net::LDAP::Error <
// StandardError, and one subclass per result code (Net::LDAP::NoSuchObject,
// Net::LDAP::InvalidCredentials, …) named exactly as the library's *ldap.Error
// reports, so raiseLDAPError maps any library error onto a registered class.
func (vm *VM) registerLDAPErrors(conn *RClass) {
	std := vm.consts["StandardError"].(*RClass)
	base := newClass("Net::LDAP::Error", std)
	conn.consts["Error"] = base
	vm.consts["Net::LDAP::Error"] = base
	for _, name := range []string{
		"OperationsError", "ProtocolError", "TimeLimitExceeded", "SizeLimitExceeded",
		"AuthMethodNotSupported", "StrongAuthRequired", "NoSuchAttribute",
		"ConstraintViolation", "AttributeOrValueExists", "InvalidAttributeSyntax",
		"NoSuchObject", "InvalidDNSyntax", "InappropriateAuthentication",
		"InvalidCredentials", "InsufficientAccessRights", "Busy", "Unavailable",
		"UnwillingToPerform", "NamingViolation", "ObjectClassViolation",
		"NotAllowedOnNonLeaf", "NotAllowedOnRDN", "EntryAlreadyExists", "Other",
		"Network", "FilterSyntax",
	} {
		c := newClass("Net::LDAP::"+name, base)
		conn.consts[name] = c
		vm.consts["Net::LDAP::"+name] = c
	}
}

// registerLDAPScopes installs the Net::LDAP::SearchScope_* constants mirroring
// the gem's scope values.
func (vm *VM) registerLDAPScopes(conn *RClass) {
	conn.consts["SearchScope_BaseObject"] = object.IntValue(int64(ldap.ScopeBase))
	conn.consts["SearchScope_SingleLevel"] = object.IntValue(int64(ldap.ScopeSingleLevel))
	conn.consts["SearchScope_WholeSubtree"] = object.IntValue(int64(ldap.ScopeSubtree))
}

// registerLDAPClient installs Net::LDAP's constructor and instance surface.
func (vm *VM) registerLDAPClient(cl *ldapClasses) {
	conn := cl.conn
	conn.smethods["new"] = &Method{name: "new", owner: conn,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			return vm.ldapConnect(cl, args)
		}}
	self := func(v object.Value) *LDAPConn { return v.(*LDAPConn) }
	d := func(name string, fn NativeFn) { conn.define(name, fn) }

	d("host", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).host)
	})
	d("port", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).port))
	})
	d("base", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).c.Base())
	})

	// #bind(auth=nil) authenticates the connection and returns true on success,
	// false on an authentication failure. An optional auth Hash (username:,
	// password:) overrides the configured credentials for this bind.
	d("bind", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		c := self(v)
		if h := ldapOptsHash(args, 0); h != nil {
			user := ldapStrOpt(h, "username", "")
			pass := ldapStrOpt(h, "password", "")
			return ldapBoolResult(c.c.BindWith(user, pass))
		}
		return ldapBoolResult(c.c.Bind())
	})

	// #search(options={}, &block) searches the directory and returns the matched
	// Net::LDAP::Entry Array (yielding each to the block when given). options may
	// carry base:, filter: (a String or Net::LDAP::Filter), scope:, attributes:
	// and size:.
	d("search", func(vm *VM, v object.Value, args []object.Value, blk *Proc) object.Value {
		return self(v).search(vm, ldapOptsHash(args, 0), blk)
	})

	// #add(dn:, attributes:) creates an entry and returns true, or false when it
	// already exists (see #get_operation_result).
	d("add", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		h := ldapReqHash(args)
		dn := ldapStrOpt(h, "dn", "")
		var attrs map[string][]string
		if av, ok := h.Get(object.Symbol("attributes")); ok {
			attrs = ldapAttributes(av)
		}
		return ldapBoolResult(self(v).c.Add(dn, attrs))
	})

	// #modify(dn:, operations:) applies the [op, attribute, values] operations and
	// returns true on success.
	d("modify", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		h := ldapReqHash(args)
		dn := ldapStrOpt(h, "dn", "")
		var ops []ldap.ModifyOp
		if ov, ok := h.Get(object.Symbol("operations")); ok {
			ops = ldapOperations(ov)
		}
		return ldapBoolResult(self(v).c.Modify(dn, ops))
	})

	// #delete(dn:) removes an entry and returns true, or false when it does not
	// exist.
	d("delete", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		h := ldapReqHash(args)
		return ldapBoolResult(self(v).c.Delete(ldapStrOpt(h, "dn", "")))
	})

	// #rename / #modify_rdn(olddn:, newrdn:, delete_attributes:, new_superior:)
	// changes an entry's relative DN and returns true on success.
	rename := func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		h := ldapReqHash(args)
		old := ldapStrOpt(h, "olddn", "")
		newRDN := ldapStrOpt(h, "newrdn", "")
		delOld := ldapBoolOpt(h, "delete_attributes", true)
		newSup := ldapStrOpt(h, "new_superior", "")
		return ldapBoolResult(self(v).c.Rename(old, newRDN, delOld, newSup))
	}
	d("rename", rename)
	d("modify_rdn", rename)

	// #replace_attribute(dn, attr, value) / #add_attribute / #delete_attribute
	// are the single-attribute modify shortcuts.
	d("replace_attribute", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		dn, attr, vals := ldapAttrArgs(args, true)
		return ldapBoolResult(self(v).c.ReplaceAttribute(dn, attr, vals))
	})
	d("add_attribute", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		dn, attr, vals := ldapAttrArgs(args, true)
		return ldapBoolResult(self(v).c.AddAttribute(dn, attr, vals))
	})
	d("delete_attribute", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		dn, attr, _ := ldapAttrArgs(args, false)
		return ldapBoolResult(self(v).c.DeleteAttribute(dn, attr))
	})

	// #compare(dn, attribute, value) reports whether the attribute has the value.
	d("compare", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		dn, attr, vals := ldapAttrArgs(args, true)
		ok, err := self(v).c.Compare(dn, attr, vals[0])
		if err != nil {
			return ldapBoolResult(err)
		}
		return object.Bool(ok)
	})

	// #get_operation_result returns the Net::LDAP::OperationResult of the last
	// operation.
	d("get_operation_result", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return &LDAPResult{cls: cl.result, r: self(v).c.OperationResult()}
	})

	// #open { |ldap| ... } yields the connection to the block and closes it
	// afterwards, returning the block's value; without a block it returns self.
	d("open", func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			return v
		}
		res := vm.callBlock(blk, []object.Value{v})
		_ = self(v).c.Close()
		return res
	})

	// #close releases the connection.
	d("close", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		_ = self(v).c.Close()
		return object.NilV
	})
}

// ldapConnect builds a *ldap.Client from a Net::LDAP.new options Hash: host:
// (default 127.0.0.1), port: (default 389), base:, and auth: {method:,
// username:, password:}. It raises Net::LDAP::Error when the directory is
// unreachable.
func (vm *VM) ldapConnect(cl *ldapClasses, args []object.Value) object.Value {
	h := ldapOptsHash(args, 0)
	if h == nil {
		raise("ArgumentError", "expected connection options (host:, port:, base:)")
	}
	host := ldapStrOpt(h, "host", "127.0.0.1")
	port := ldapIntOpt(h, "port", 389)
	base := ldapStrOpt(h, "base", "")
	cfg := ldap.Config{Host: host, Port: port, Base: base, Method: "anonymous"}
	if av, ok := h.Get(object.Symbol("auth")); ok {
		ah, ok := av.(*object.Hash)
		if !ok {
			raise("TypeError", "auth must be a Hash, got %s", av.Inspect())
		}
		if mv, ok := ah.Get(object.Symbol("method")); ok {
			cfg.Method = ldapMethodName(mv)
		} else {
			cfg.Method = "simple"
		}
		cfg.Username = ldapStrOpt(ah, "username", "")
		cfg.Password = ldapStrOpt(ah, "password", "")
	}
	c, err := ldap.New(cfg)
	if err != nil {
		raiseLDAPError(err)
	}
	return &LDAPConn{cls: cl.conn, c: c, host: host, port: port, cl: cl}
}

// search runs a search from an options Hash, yielding each entry to blk when
// given, and returns the Array of Net::LDAP::Entry (or nil on a result-code
// failure). A network failure re-raises.
func (c *LDAPConn) search(vm *VM, h *object.Hash, blk *Proc) object.Value {
	req := &ldap.SearchRequest{Scope: ldap.ScopeSubtree}
	if h != nil {
		req.Base = ldapStrOpt(h, "base", "")
		if v, ok := h.Get(object.Symbol("filter")); ok {
			req.Filter = ldapFilterArg(v)
		}
		if v, ok := h.Get(object.Symbol("scope")); ok {
			req.Scope = ldapScope(v)
		}
		if v, ok := h.Get(object.Symbol("attributes")); ok {
			req.Attributes = ldapAttrValues(v)
		}
		req.SizeLimit = ldapIntOpt(h, "size", 0)
	}
	res, err := c.c.Search(req)
	if err != nil {
		if errors.Is(err, ldap.ErrNetwork) {
			raiseLDAPError(err)
		}
		return object.NilV
	}
	out := make([]object.Value, len(res.Entries))
	for i, e := range res.Entries {
		ev := ldapEntryValue(c.cl, e)
		out[i] = ev
		if blk != nil {
			vm.callBlock(blk, []object.Value{ev})
		}
	}
	return object.NewArrayFromSlice(out)
}

// registerLDAPEntry installs Net::LDAP::Entry and its readers.
func (vm *VM) registerLDAPEntry(cl *ldapClasses) {
	cls := cl.entry
	self := func(v object.Value) *ldap.Entry { return v.(*LDAPEntry).e }
	d := func(name string, fn NativeFn) { cls.define(name, fn) }

	d("dn", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).DN())
	})
	// #[](name) returns the attribute's values as an Array (case-insensitive).
	d("[]", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		return ldapStrArray(self(v).Get(ldapKeyName(args[0])))
	})
	// #first(name) returns the attribute's first value, or nil.
	d("first", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		vals := self(v).Get(ldapKeyName(args[0]))
		if len(vals) == 0 {
			return object.NilV
		}
		return object.NewString(vals[0])
	})
	d("attribute_names", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return ldapStrArray(self(v).AttributeNames())
	})
	// #each_attribute { |name, values| ... } yields each attribute name and its
	// values, returning self.
	d("each_attribute", func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (yield)")
		}
		e := self(v)
		for _, name := range e.AttributeNames() {
			vm.callBlock(blk, []object.Value{object.NewString(name), ldapStrArray(e.Get(name))})
		}
		return v
	})
	d("to_h", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		e := self(v)
		h := object.NewHash()
		h.Set(object.Symbol("dn"), object.NewString(e.DN()))
		for _, name := range e.AttributeNames() {
			h.Set(object.Symbol(name), ldapStrArray(e.Get(name)))
		}
		return h
	})
}

// registerLDAPFilter installs Net::LDAP::Filter: the class-level builders and the
// instance-level combinators.
func (vm *VM) registerLDAPFilter(cl *ldapClasses) {
	cls := cl.filter
	mk := func(f ldap.Filter) object.Value { return ldapFilterValue(cl, f) }

	// Binary class builders: name(attr, value).
	bin := func(name string, build func(a, b string) ldap.Filter) {
		cls.smethods[name] = &Method{name: name, owner: cls,
			native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
				if len(args) < 2 {
					raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
				}
				return mk(build(strArg(args[0]), strArg(args[1])))
			}}
	}
	bin("eq", ldap.Eq)
	bin("ge", ldap.Ge)
	bin("le", ldap.Le)
	bin("begins", ldap.Begins)
	bin("ends", ldap.Ends)
	bin("contains", ldap.Contains)

	// Presence class builders: name(attr).
	pres := func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		return mk(ldap.Present(strArg(args[0])))
	}
	cls.smethods["present"] = &Method{name: "present", owner: cls, native: pres}
	cls.smethods["pres"] = cls.smethods["present"]

	// construct / from_rfc2254 parse an RFC 4515 string, raising
	// Net::LDAP::FilterSyntax on a malformed filter.
	construct := func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		return mk(ldapFilterArg(args[0]))
	}
	cls.smethods["construct"] = &Method{name: "construct", owner: cls, native: construct}
	cls.smethods["from_rfc2254"] = cls.smethods["construct"]

	self := func(v object.Value) ldap.Filter { return v.(*LDAPFilter).f }
	filterArg := func(v object.Value) ldap.Filter {
		f, ok := v.(*LDAPFilter)
		if !ok {
			raise("TypeError", "expected a Net::LDAP::Filter, got %s", v.Inspect())
		}
		return f.f
	}
	d := func(name string, fn NativeFn) { cls.define(name, fn) }

	// #& / #| combine two filters; #negate / #~ negates one; #to_s renders the
	// RFC 4515 string.
	d("&", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return mk(ldap.And(self(v), filterArg(args[0])))
	})
	d("|", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return mk(ldap.Or(self(v), filterArg(args[0])))
	})
	negate := func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return mk(ldap.Not(self(v)))
	}
	d("negate", negate)
	d("~", negate)
	// #to_s falls back to the wrapper's ToS (the RFC 4515 string) — no separate
	// Ruby method is needed.
}

// registerLDAPResult installs Net::LDAP::OperationResult and its readers.
func (vm *VM) registerLDAPResult(cl *ldapClasses) {
	cls := cl.result
	self := func(v object.Value) ldap.OperationResult { return v.(*LDAPResult).r }
	d := func(name string, fn NativeFn) { cls.define(name, fn) }

	d("code", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).Code))
	})
	d("name", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).Name)
	})
	message := func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).Message)
	}
	d("message", message)
	d("error_message", message)
	d("matched_dn", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).MatchedDN)
	})
	d("success?", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).Code == 0)
	})
}

// ldapReqHash returns the required trailing options Hash of an operation, raising
// ArgumentError when it is missing.
func ldapReqHash(args []object.Value) *object.Hash {
	h := ldapOptsHash(args, 0)
	if h == nil {
		raise("ArgumentError", "expected an options Hash")
	}
	return h
}

// ldapAttrArgs reads the (dn, attribute, value) positional arguments of the
// single-attribute modify shortcuts. When needValue is false the value is
// optional (a delete), and the returned slice is nil.
func ldapAttrArgs(args []object.Value, needValue bool) (string, string, []string) {
	want := 2
	if needValue {
		want = 3
	}
	if len(args) < want {
		raise("ArgumentError", "wrong number of arguments (given %d, expected %d)", len(args), want)
	}
	dn := strArg(args[0])
	attr := ldapKeyName(args[1])
	if !needValue {
		return dn, attr, nil
	}
	return dn, attr, ldapAttrValues(args[2])
}
