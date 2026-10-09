// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestIvarNamesInOrderReportsNamesMissingFromTheOrderList covers the floor under
// #787 directly, because nothing in the VM reaches it any more: the constructor
// that used to leave ivarOrder empty now fills it, which is the fix, and this
// arm is what keeps the NEXT one from being silent.
//
// The coverage gate named it, and the question a named function asks is whether
// the branch is dead or unreached. It is not dead -- the shape it guards is a
// plain *RObject built with a populated ivars map, which any new constructor can
// produce in one line -- so it gets a test that builds exactly that shape rather
// than being deleted.
func TestIvarNamesInOrderReportsNamesMissingFromTheOrderList(t *testing.T) {
	// Exactly the shape yaml_bind.go produced before #787: a live map, an empty
	// order list.
	o := &RObject{ivars: map[string]object.Value{
		"@b": object.IntValue(1),
		"@a": object.IntValue(2),
	}}
	got := namesOf(ivarNamesInOrder(o))
	// Sorted, because the order list says nothing and Go's map order is random:
	// an enumeration that changes between runs would be its own defect.
	if len(got) != 2 || got[0] != "@a" || got[1] != "@b" {
		t.Errorf("ivarNamesInOrder = %v, want [@a @b] (sorted, since the order list is empty)", got)
	}

	// A half-filled order list: the named one keeps its position, the rest are
	// appended sorted. This is the arm that would be wrong if the floor simply
	// replaced the ordered walk instead of extending it.
	o2 := &RObject{
		ivars: map[string]object.Value{
			"@z": object.IntValue(1),
			"@m": object.IntValue(2),
			"@a": object.IntValue(3),
		},
		ivarOrder: []string{"@z"},
	}
	got = namesOf(ivarNamesInOrder(o2))
	if len(got) != 3 || got[0] != "@z" || got[1] != "@a" || got[2] != "@m" {
		t.Errorf("ivarNamesInOrder = %v, want [@z @a @m]: the listed name keeps its place, the rest follow sorted", got)
	}

	// And a name in the order list whose value is gone stays gone -- the floor
	// must not resurrect what remove_instance_variable deleted.
	o3 := &RObject{
		ivars:     map[string]object.Value{"@keep": object.IntValue(1)},
		ivarOrder: []string{"@gone", "@keep"},
	}
	got = namesOf(ivarNamesInOrder(o3))
	if len(got) != 1 || got[0] != "@keep" {
		t.Errorf("ivarNamesInOrder = %v, want [@keep]: a removed ivar stays removed", got)
	}
}

func namesOf(vs []object.Value) []string {
	out := make([]string, 0, len(vs))
	for _, v := range vs {
		out = append(out, string(v.(object.Symbol)))
	}
	return out
}
