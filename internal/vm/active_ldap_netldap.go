// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"fmt"

	activeldap "github.com/go-ruby-activeldap/activeldap"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// rubyDirectory adapts any Ruby object answering the Net::LDAP surface to the
// activeldap.Directory seam. It is what ActiveLdap.directory(conn) wraps: the
// four operations are dispatched to the object with #search / #add / #modify /
// #delete, exactly as ActiveLdap drives Net::LDAP. A Net::LDAP connection from
// go-ruby-ldap is the production object; a duck-typed stub answering the same
// methods drives it in tests. Search results are read back through the
// Net::LDAP::Entry contract (#dn, #attribute_names, #[]).
type rubyDirectory struct {
	vm  *VM
	obj object.Value
}

// searchOptions builds the options Hash passed to #search: base:, scope: (as the
// :base/:one/:sub symbol the Net::LDAP adapter maps to a scope constant),
// filter:, and (when limited) attributes:.
func searchOptions(req activeldap.SearchRequest) *object.Hash {
	h := object.NewHash()
	h.Set(object.SymVal("base"), object.NewString(req.Base))
	h.Set(object.SymVal("scope"), object.SymVal(req.Scope.String()))
	h.Set(object.SymVal("filter"), object.NewString(req.Filter))
	if len(req.Attributes) > 0 {
		arr := object.NewArrayFromSlice(make([]object.Value, len(req.Attributes)))
		for i, a := range req.Attributes {
			arr.Elems[i] = object.NewString(a)
		}
		h.Set(object.SymVal("attributes"), arr)
	}
	return h
}

// Search dispatches #search and converts the returned Array of Net::LDAP::Entry
// objects to activeldap entries.
func (r *rubyDirectory) Search(req activeldap.SearchRequest) ([]*activeldap.Entry, error) {
	res := r.vm.send(r.obj, "search", []object.Value{searchOptions(req)}, nil)
	arr, ok := res.(*object.Array)
	if !ok {
		return nil, nil // nil / false result: no entries (Net::LDAP miss)
	}
	out := make([]*activeldap.Entry, 0, len(arr.Elems))
	for _, e := range arr.Elems {
		out = append(out, r.readEntry(e))
	}
	return out, nil
}

// readEntry reads one Net::LDAP::Entry via #dn and #attribute_names / #[].
func (r *rubyDirectory) readEntry(e object.Value) *activeldap.Entry {
	dn := rubyToStr(r.vm.send(e, "dn", nil, nil))
	entry := &activeldap.Entry{DN: dn, Attributes: map[string][]string{}}
	names, ok := r.vm.send(e, "attribute_names", nil, nil).(*object.Array)
	if !ok {
		return entry
	}
	for _, n := range names.Elems {
		name := rubyToStr(n)
		vals := r.vm.send(e, "[]", []object.Value{object.NewString(name)}, nil)
		entry.Attributes[name] = rubyStrList(vals)
	}
	return entry
}

// Add dispatches #add(dn:, attributes:) and treats a falsy return as a failure.
func (r *rubyDirectory) Add(dn string, attributes map[string][]string) error {
	h := object.NewHash()
	h.Set(object.SymVal("dn"), object.NewString(dn))
	h.Set(object.SymVal("attributes"), goAttrsToRuby(attributes))
	return r.opResult("add", r.vm.send(r.obj, "add", []object.Value{h}, nil))
}

// Modify dispatches #modify(dn:, operations:) where operations is the Net::LDAP
// [op_symbol, attribute, values] triples list.
func (r *rubyDirectory) Modify(dn string, ops []activeldap.ModifyOp) error {
	triples := object.NewArrayFromSlice(make([]object.Value, len(ops)))
	for i, op := range ops {
		vals := object.NewArrayFromSlice(make([]object.Value, len(op.Values)))
		for j, v := range op.Values {
			vals.Elems[j] = object.NewString(v)
		}
		triples.Elems[i] = object.NewArray(
			object.SymVal(op.Op.String()), object.NewString(op.Attribute), vals)
	}
	h := object.NewHash()
	h.Set(object.SymVal("dn"), object.NewString(dn))
	h.Set(object.SymVal("operations"), triples)
	return r.opResult("modify", r.vm.send(r.obj, "modify", []object.Value{h}, nil))
}

// Delete dispatches #delete(dn:) and treats a falsy return as a failure.
func (r *rubyDirectory) Delete(dn string) error {
	h := object.NewHash()
	h.Set(object.SymVal("dn"), object.NewString(dn))
	return r.opResult("delete", r.vm.send(r.obj, "delete", []object.Value{h}, nil))
}

// opResult turns a Net::LDAP operation's boolean return into an error: a truthy
// return is success; a falsy one is a failure carrying the operation name (the
// caller maps it onto ActiveLdap::ConnectionError).
func (r *rubyDirectory) opResult(op string, res object.Value) error {
	if res != nil && res.Truthy() {
		return nil
	}
	return fmt.Errorf("ldap %s failed", op)
}
