// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"

	ldap "github.com/go-ruby-ldap/ldap"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin binding between rbgo's Ruby object graph and the
// interpreter-independent Net::LDAP client of github.com/go-ruby-ldap/ldap — a
// pure-Go (CGO=0), net-ldap-gem-flavoured surface over the official
// github.com/go-ldap/ldap/v3 transport. It carries the value types the
// Net::LDAP module wraps — an open connection, a search Entry, a Filter, and the
// operation-result view — plus the argument coercions and the error bridge that
// re-raises the library's *ldap.Error result tree as the matching Ruby
// exceptions. All protocol work is delegated to go-ruby-ldap; see ldap.go for
// the module and method wiring.

// ldapClasses holds every Ruby class the Net::LDAP binding constructs, resolved
// once at registration and threaded through the value wrappers so a result built
// deep in a bridge (a search Entry) reports the right class without a consts
// lookup.
type ldapClasses struct {
	conn, entry, filter, result *RClass
}

// LDAPConn is an instance of Net::LDAP: a connection bound to a directory
// through a go-ruby-ldap *ldap.Client. It builds requests, drives them over the
// client and maps the replies back into the object graph.
type LDAPConn struct {
	cls  *RClass
	c    *ldap.Client
	host string
	port int
	cl   *ldapClasses
}

func (c *LDAPConn) ToS() string     { return "#<Net::LDAP>" }
func (c *LDAPConn) Inspect() string { return "#<Net::LDAP>" }
func (c *LDAPConn) Truthy() bool    { return true }

// LDAPEntry is an instance of Net::LDAP::Entry: one search result's DN and its
// case-insensitive attributes, mirroring the gem's Entry.
type LDAPEntry struct {
	cls *RClass
	e   *ldap.Entry
}

func (e *LDAPEntry) ToS() string     { return "#<Net::LDAP::Entry dn=" + e.e.DN() + ">" }
func (e *LDAPEntry) Inspect() string { return "#<Net::LDAP::Entry dn=" + e.e.DN() + ">" }
func (e *LDAPEntry) Truthy() bool    { return true }

// LDAPFilter is an instance of Net::LDAP::Filter: an RFC 4515 search filter,
// mirroring the gem's Filter (composed with & / | / negate).
type LDAPFilter struct {
	cls *RClass
	f   ldap.Filter
}

func (f *LDAPFilter) ToS() string     { return f.f.String() }
func (f *LDAPFilter) Inspect() string { return "#<Net::LDAP::Filter " + f.f.String() + ">" }
func (f *LDAPFilter) Truthy() bool    { return true }

// LDAPResult is an instance of Net::LDAP::OperationResult: the code, name,
// message and matched DN of the last operation, mirroring the gem's
// #get_operation_result.
type LDAPResult struct {
	cls *RClass
	r   ldap.OperationResult
}

func (r *LDAPResult) ToS() string     { return "#<Net::LDAP::OperationResult " + r.r.Name + ">" }
func (r *LDAPResult) Inspect() string { return "#<Net::LDAP::OperationResult " + r.r.Name + ">" }
func (r *LDAPResult) Truthy() bool    { return true }

// raiseLDAPError re-raises a go-ruby-ldap error as the matching Ruby exception.
// An *ldap.Error carries the result-code class name (e.g. "NoSuchObject"), so it
// raises the registered Net::LDAP::NoSuchObject when that subclass exists and
// the Net::LDAP::Error base otherwise. It never returns (raise panics).
func raiseLDAPError(err error) {
	name := "Error"
	var le *ldap.Error
	if errors.As(err, &le) {
		name = le.Name
	}
	raise("Net::LDAP::"+name, "%s", err.Error())
}

// ldapEntryValue wraps an *ldap.Entry as a Net::LDAP::Entry.
func ldapEntryValue(cl *ldapClasses, e *ldap.Entry) *LDAPEntry {
	return &LDAPEntry{cls: cl.entry, e: e}
}

// ldapFilterValue wraps an ldap.Filter as a Net::LDAP::Filter.
func ldapFilterValue(cl *ldapClasses, f ldap.Filter) *LDAPFilter {
	return &LDAPFilter{cls: cl.filter, f: f}
}

// ldapStrArray wraps a Go string slice as a Ruby Array of Strings.
func ldapStrArray(ss []string) *object.Array {
	out := make([]object.Value, len(ss))
	for i, s := range ss {
		out[i] = object.NewString(s)
	}
	return object.NewArrayFromSlice(out)
}

// ldapOptsHash returns the trailing keyword Hash of an operation (the last
// argument at or after from when it is a Hash), or nil otherwise.
func ldapOptsHash(args []object.Value, from int) *object.Hash {
	if len(args) <= from {
		return nil
	}
	h, ok := args[len(args)-1].(*object.Hash)
	if !ok {
		return nil
	}
	return h
}

// ldapStrOpt reads a String option under key from h (which callers always pass
// non-nil), returning def when the key is absent.
func ldapStrOpt(h *object.Hash, key, def string) string {
	if v, ok := h.Get(object.Symbol(key)); ok {
		return strArg(v)
	}
	return def
}

// ldapIntOpt reads an Integer option under key from h, returning def when it is
// absent.
func ldapIntOpt(h *object.Hash, key string, def int) int {
	if v, ok := h.Get(object.Symbol(key)); ok {
		return int(intArg(v))
	}
	return def
}

// ldapBoolOpt reads a boolean option under key from h: any truthy value enables
// it, an absent key yields def.
func ldapBoolOpt(h *object.Hash, key string, def bool) bool {
	if v, ok := h.Get(object.Symbol(key)); ok {
		return v.Truthy()
	}
	return def
}

// ldapFilterArg coerces a filter argument into an ldap.Filter: a Net::LDAP::
// Filter is used directly, a String is parsed with Construct (raising
// Net::LDAP::FilterSyntax on a bad string), and an absent/nil filter yields the
// match-everything filter. Anything else raises TypeError.
func ldapFilterArg(v object.Value) ldap.Filter {
	switch x := v.(type) {
	case *LDAPFilter:
		return x.f
	case *object.String:
		f, err := ldap.Construct(x.Str())
		if err != nil {
			raiseLDAPError(err)
		}
		return f
	}
	if v == object.NilV {
		return ldap.Filter{}
	}
	raise("TypeError", "no implicit conversion of %s into a Net::LDAP::Filter", v.Inspect())
	return ldap.Filter{}
}

// ldapAttrValues coerces an attribute value — a String or an Array of Strings —
// into a Go string slice, raising TypeError for anything else.
func ldapAttrValues(v object.Value) []string {
	switch x := v.(type) {
	case *object.String:
		return []string{x.Str()}
	case *object.Array:
		out := make([]string, len(x.Elems))
		for i, e := range x.Elems {
			out[i] = strArg(e)
		}
		return out
	}
	raise("TypeError", "attribute value must be a String or Array, got %s", v.Inspect())
	return nil
}

// ldapAttributes coerces an attributes: Hash into a Go map of attribute name to
// values, accepting Symbol or String keys and String or Array values.
func ldapAttributes(v object.Value) map[string][]string {
	h, ok := v.(*object.Hash)
	if !ok {
		raise("TypeError", "attributes must be a Hash, got %s", v.Inspect())
	}
	out := map[string][]string{}
	for _, k := range h.Keys {
		val, _ := h.Get(k)
		out[ldapKeyName(k)] = ldapAttrValues(val)
	}
	return out
}

// ldapKeyName renders a Hash key (Symbol or String) as an attribute name.
func ldapKeyName(k object.Value) string {
	if sym, ok := k.(object.Symbol); ok {
		return string(sym)
	}
	return strArg(k)
}

// ldapModType maps a Ruby modify operation — a Symbol (:add / :replace /
// :delete) or the equivalent String — to an ldap.ModType, raising ArgumentError
// for anything else.
func ldapModType(v object.Value) ldap.ModType {
	var s string
	switch x := v.(type) {
	case object.Symbol:
		s = string(x)
	case *object.String:
		s = x.Str()
	default:
		raise("ArgumentError", "modify operation must be a Symbol or String, got %s", v.Inspect())
	}
	switch s {
	case "add":
		return ldap.ModAdd
	case "replace":
		return ldap.ModReplace
	case "delete":
		return ldap.ModDelete
	}
	raise("ArgumentError", "unknown modify operation %q", s)
	return ldap.ModAdd
}

// ldapBoolResult maps an operation's error into the boolean net-ldap returns: a
// nil error is true; an LDAP result-code failure (bad credentials, no such
// object, already exists, …) is false, with the detail left in
// #get_operation_result; a network-class failure re-raises the mapped Net::LDAP
// exception, matching net-ldap raising on a broken connection.
func ldapBoolResult(err error) object.Value {
	if err == nil {
		return object.Bool(true)
	}
	if errors.Is(err, ldap.ErrNetwork) {
		raiseLDAPError(err)
	}
	return object.Bool(false)
}

// ldapScope coerces a search scope option — an Integer (a
// Net::LDAP::SearchScope_* constant) or a Symbol (:base / :one / :single /
// :sub / :subtree) — into an ldap.Scope, raising ArgumentError for anything
// else.
func ldapScope(v object.Value) ldap.Scope {
	switch x := v.(type) {
	case object.Integer:
		return ldap.Scope(int(x))
	case object.Symbol:
		switch string(x) {
		case "base", "base_object":
			return ldap.ScopeBase
		case "one", "single", "single_level", "onelevel":
			return ldap.ScopeSingleLevel
		case "sub", "subtree", "whole", "whole_subtree":
			return ldap.ScopeSubtree
		}
	}
	raise("ArgumentError", "invalid search scope %s", v.Inspect())
	return ldap.ScopeBase
}

// ldapMethodName renders an auth :method value (a Symbol or String) as a lower-
// case method name, defaulting to "simple" for anything else.
func ldapMethodName(v object.Value) string {
	switch x := v.(type) {
	case object.Symbol:
		return string(x)
	case *object.String:
		return x.Str()
	}
	return "simple"
}

// ldapOperations coerces a modify operations: argument — an Array of [op, attr,
// values] triples — into a slice of ldap.ModifyOp. A malformed triple raises
// ArgumentError.
func ldapOperations(v object.Value) []ldap.ModifyOp {
	arr, ok := v.(*object.Array)
	if !ok {
		raise("TypeError", "operations must be an Array, got %s", v.Inspect())
	}
	out := make([]ldap.ModifyOp, 0, len(arr.Elems))
	for _, e := range arr.Elems {
		triple, ok := e.(*object.Array)
		if !ok || len(triple.Elems) < 2 {
			raise("ArgumentError", "each operation must be [op, attribute, values]")
		}
		op := ldap.ModifyOp{Type: ldapModType(triple.Elems[0]), Attr: ldapKeyName(triple.Elems[1])}
		if len(triple.Elems) >= 3 {
			op.Values = ldapAttrValues(triple.Elems[2])
		}
		out = append(out, op)
	}
	return out
}
