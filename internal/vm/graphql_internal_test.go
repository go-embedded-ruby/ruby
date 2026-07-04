// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"context"
	"testing"

	graphql "github.com/go-ruby-graphql/graphql"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestGraphQLGoFromRuby covers every arm of goFromRuby, including the nil, Symbol
// and Hash-key arms and the #to_s default for an unhandled value.
func TestGraphQLGoFromRuby(t *testing.T) {
	if goFromRuby(nil) != nil || goFromRuby(object.NilV) != nil {
		t.Fatal("nil arms")
	}
	if goFromRuby(object.Bool(true)) != true {
		t.Fatal("bool arm")
	}
	if goFromRuby(object.Integer(3)) != 3 {
		t.Fatal("int arm")
	}
	if goFromRuby(object.Float(1.5)) != 1.5 {
		t.Fatal("float arm")
	}
	if goFromRuby(object.NewString("s")) != "s" {
		t.Fatal("string arm")
	}
	if goFromRuby(object.Symbol("sym")) != "sym" {
		t.Fatal("symbol arm")
	}
	arr := goFromRuby(object.NewArray(object.Integer(1), object.NewString("x"))).([]interface{})
	if len(arr) != 2 || arr[0] != 1 || arr[1] != "x" {
		t.Fatalf("array arm: %v", arr)
	}
	h := object.NewHash()
	h.Set(object.Symbol("k"), object.Integer(9))
	m := goFromRuby(h).(map[string]interface{})
	if m["k"] != 9 {
		t.Fatalf("hash arm: %v", m)
	}
	// default arm: a wrapper value with no dedicated case renders via #to_s.
	if goFromRuby(&GraphQLType{cls: newClass("X", nil)}) != "#<X>" {
		t.Fatal("default arm")
	}
}

// TestGraphQLRubyFromGo covers every arm of rubyFromGo, including int64, the
// []map error-tree arm, and the fmt default for an unhandled Go value.
func TestGraphQLRubyFromGo(t *testing.T) {
	if rubyFromGo(nil) != object.NilV {
		t.Fatal("nil arm")
	}
	if rubyFromGo(true) != object.Bool(true) {
		t.Fatal("bool arm")
	}
	if rubyFromGo(7) != object.Integer(7) {
		t.Fatal("int arm")
	}
	if rubyFromGo(int64(8)) != object.Integer(8) {
		t.Fatal("int64 arm")
	}
	if rubyFromGo(2.5) != object.Float(2.5) {
		t.Fatal("float arm")
	}
	if s, ok := rubyFromGo("z").(*object.String); !ok || s.Str() != "z" {
		t.Fatal("string arm")
	}
	if a, ok := rubyFromGo([]interface{}{1, "x"}).(*object.Array); !ok || len(a.Elems) != 2 {
		t.Fatal("slice arm")
	}
	errs := []map[string]interface{}{{"message": "m"}}
	if a, ok := rubyFromGo(errs).(*object.Array); !ok || len(a.Elems) != 1 {
		t.Fatal("[]map arm")
	}
	hm := rubyFromGo(map[string]interface{}{"k": 1}).(*object.Hash)
	if v, _ := hm.Get(object.NewString("k")); v != object.Integer(1) {
		t.Fatal("map arm")
	}
	if s, ok := rubyFromGo(uint(4)).(*object.String); !ok || s.Str() != "4" {
		t.Fatal("default arm")
	}
}

// TestGraphQLKeyString covers the Symbol, String and #to_s-default arms of
// graphqlKeyString.
func TestGraphQLKeyString(t *testing.T) {
	if graphqlKeyString(object.Symbol("a")) != "a" ||
		graphqlKeyString(object.NewString("b")) != "b" ||
		graphqlKeyString(object.Integer(3)) != "3" {
		t.Fatal("graphqlKeyString arms")
	}
}

// TestGraphQLKwargs covers graphqlKwargs (empty, non-Hash trailing, and Hash
// arms) and graphqlKwGet (absent-Hash, present and missing key arms).
func TestGraphQLKwargs(t *testing.T) {
	if graphqlKwargs(nil) != nil {
		t.Fatal("empty arm")
	}
	if graphqlKwargs([]object.Value{object.NewString("x")}) != nil {
		t.Fatal("non-hash arm")
	}
	h := object.NewHash()
	h.Set(object.Symbol("k"), object.Integer(1))
	if graphqlKwargs([]object.Value{h}) != h {
		t.Fatal("hash arm")
	}
	if _, ok := graphqlKwGet(nil, "k"); ok {
		t.Fatal("nil-hash arm")
	}
	if v, ok := graphqlKwGet(h, "k"); !ok || v != object.Integer(1) {
		t.Fatal("present arm")
	}
	if _, ok := graphqlKwGet(h, "absent"); ok {
		t.Fatal("missing arm")
	}
}

// TestGraphQLContextValue covers graphqlContextValue for a nil context, a
// context without the value, and a context carrying it.
func TestGraphQLContextValue(t *testing.T) {
	if graphqlContextValue(nil) != object.NilV {
		t.Fatal("nil context")
	}
	if graphqlContextValue(context.Background()) != object.NilV {
		t.Fatal("no value")
	}
	want := object.NewString("ctx")
	ctx := context.WithValue(context.Background(), graphqlCtxKey{}, object.Value(want))
	if graphqlContextValue(ctx) != want {
		t.Fatal("carried value")
	}
}

// TestGraphQLExecErr covers graphqlExecErr's three false arms: a non-object
// value, a missing ExecutionError constant, and an object whose class chain does
// not include ExecutionError.
func TestGraphQLExecErr(t *testing.T) {
	vm := newTestVM()
	if _, ok := vm.graphqlExecErr(object.NewString("x")); ok {
		t.Fatal("non-object arm")
	}
	plain := &RObject{class: vm.cObject, ivars: map[string]object.Value{}}
	if _, ok := vm.graphqlExecErr(plain); ok {
		t.Fatal("non-matching class arm")
	}
	saved := vm.consts["GraphQL::ExecutionError"]
	delete(vm.consts, "GraphQL::ExecutionError")
	if _, ok := vm.graphqlExecErr(plain); ok {
		t.Fatal("missing-const arm")
	}
	vm.consts["GraphQL::ExecutionError"] = saved
}

// TestGraphQLResolveRepanic proves graphqlResolve re-raises a non-Ruby panic
// unchanged (only a RubyError is converted to a field error).
func TestGraphQLResolveRepanic(t *testing.T) {
	vm := newTestVM()
	proc := &Proc{native: func(_ *VM, _ []object.Value) object.Value { panic("boom-native") }}
	defer func() {
		if r := recover(); r != "boom-native" {
			t.Fatalf("re-panic = %v want boom-native", r)
		}
	}()
	vm.graphqlResolve(proc, graphql.ResolveParams{})
}

// TestGraphQLResolveRubyError proves a RubyError raised inside the resolver is
// converted to a GraphQL ExecutionError carrying its message.
func TestGraphQLResolveRubyError(t *testing.T) {
	vm := newTestVM()
	proc := &Proc{native: func(_ *VM, _ []object.Value) object.Value { return raise("RuntimeError", "boom") }}
	_, err := vm.graphqlResolve(proc, graphql.ResolveParams{})
	if err == nil || err.Error() != "boom" {
		t.Fatalf("ruby-error arm = %v want boom", err)
	}
}

// TestGraphQLWrapperMethods covers the ToS / Inspect / Truthy value-protocol arms
// of the three wrapper types, including a GraphQLType with no name.
func TestGraphQLWrapperMethods(t *testing.T) {
	named := &GraphQLType{cls: newClass("GraphQL::Type", nil), name: "Int"}
	if named.ToS() != "#<GraphQL::Type Int>" || named.Inspect() != named.ToS() || !named.Truthy() {
		t.Fatalf("named type methods: %q", named.ToS())
	}
	anon := &GraphQLType{cls: newClass("GraphQL::Type", nil)}
	if anon.ToS() != "#<GraphQL::Type>" {
		t.Fatalf("anon type ToS: %q", anon.ToS())
	}
	s := &GraphQLSchema{}
	if s.ToS() == "" || s.Inspect() != s.ToS() || !s.Truthy() {
		t.Fatal("schema methods")
	}
	d := &GraphQLObjectDSL{}
	if d.ToS() == "" || d.Inspect() != d.ToS() || !d.Truthy() {
		t.Fatal("dsl methods")
	}
}
