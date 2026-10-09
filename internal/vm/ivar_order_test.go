// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import "testing"

// TestYAMLBuiltIvarsAreEnumerable (#787).
//
// A YAML-loaded object answered `o.a == 1` and `instance_variables == []`. The
// ivars were there and readable by name; only the enumeration could not see
// them, which is the worst version of the bug: every direct check passes and
// anything that WALKS the object -- marshalling, inspect, a serialiser, an
// equality written over instance_variables -- silently sees an empty object.
//
// Cause: for an *RObject the order list is consulted FIRST, and the pointer to
// it is the address of a struct field, so it is never nil. A constructor that
// fills the ivars map directly and leaves ivarOrder empty therefore produces
// ivars that read back and do not enumerate, with nothing to indicate it.
func TestYAMLBuiltIvarsAreEnumerable(t *testing.T) {
	checkCases(t, []runCase{
		{`require "yaml"
class P; attr_accessor :a, :b; end
o = YAML.safe_load("--- !ruby/object:P\na: 1\nb: 2\n", permitted_classes: [P])
p o.instance_variables`, "[:@a, :@b]\n"},

		// The order is the DOCUMENT's, not the map's: z, a, m is neither
		// alphabetical nor reversed, so a run that reproduced it by accident is
		// unlikely, and Go's map iteration is randomised per run.
		{`require "yaml"
class Q; attr_accessor :z, :a, :m; end
o = YAML.safe_load("--- !ruby/object:Q\nz: 1\na: 2\nm: 3\n", permitted_classes: [Q])
p o.instance_variables`, "[:@z, :@a, :@m]\n"},

		// Reading was never broken, and must stay unbroken -- a fix that rebuilt
		// the object could satisfy the enumeration and lose the values.
		{`require "yaml"
class R; attr_accessor :a, :b; end
o = YAML.safe_load("--- !ruby/object:R\na: 1\nb: 2\n", permitted_classes: [R])
p [o.a, o.b, o.instance_variable_get(:@a)]`, "[1, 2, 1]\n"},
	})
}

// TestALiveIvarIsNeverHiddenByTheOrderList is the floor under the above: the
// enumeration reports a live ivar even when the order list does not mention it,
// so the next constructor that fills the map directly is wrong in a VISIBLE way
// rather than a silent one.
//
// It is exercised through Marshal, whose loader is a second object builder, and
// through an ordinary object to show the common path still reports creation
// order rather than the sorted fallback.
func TestALiveIvarIsNeverHiddenByTheOrderList(t *testing.T) {
	checkCases(t, []runCase{
		// Creation order, not sorted: b before a.
		{`class S; def initialize; @b = 1; @a = 2; end; end
p S.new.instance_variables`, "[:@b, :@a]\n"},
		// remove_instance_variable still drops the name.
		{`class T; def initialize; @x = 1; @y = 2; end
  def drop; remove_instance_variable(:@x); end
end
t = T.new; t.drop; p t.instance_variables`, "[:@y]\n"},
		{`class U; def initialize; @a = 1; @b = 2; end; end
u = Marshal.load(Marshal.dump(U.new))
p u.instance_variables`, "[:@a, :@b]\n"},
	})
}
