// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !rbgo_core

package vm

import (
	"bytes"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestClassOfGemTwoUnreachedCases covers the two classOfGem cases no program in
// the suite reaches: a GraphQL type wrapper reports the class stamped on it, and
// a Minitest::Assertions box reports Minitest::Assertions. They were the two
// cases that held classOf at 99.5% before the gem cases moved out of it.
func TestClassOfGemTwoUnreachedCases(t *testing.T) {
	vm := New(&bytes.Buffer{})
	stamped := newClass("GraphQL::ObjectType", vm.cObject)
	if got := vm.classOf(&GraphQLType{cls: stamped}); got != stamped {
		t.Errorf("classOf(GraphQLType) = %v, want the stamped class", got)
	}
	vm.doRequire("minitest", false)
	want := vm.consts["Minitest::Assertions"].(*RClass)
	if got := vm.classOf(&MinitestAssertionsBox{}); got != want {
		t.Errorf("classOf(MinitestAssertionsBox) = %v, want Minitest::Assertions", got)
	}
	// A core value is not a gem's: the seam answers false and classOf is
	// unaffected.
	if c, ok := vm.classOfGem(object.IntValue(1)); ok || c != nil {
		t.Errorf("classOfGem(1) = %v, %v; want nil, false", c, ok)
	}
}
