// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestStringDedupIsAnAliasOfUnaryMinus: String#dedup is MRI's 4.0 ALIAS of -@,
// rb_define_alias(rb_cString, "dedup", "-@") (string.c ruby_4_0:12618), whose
// documentation gives both spellings on one call-seq.
//
// It aliases rather than re-defines because the difference is observable, and
// #original_name is where: a second definition would report :dedup. Every row
// is the byte-for-byte answer of ruby 4.0.5.
func TestStringDedupIsAnAliasOfUnaryMinus(t *testing.T) {
	src := `s = "lit"
p s.dedup
p s.dedup.frozen?
p "lit".dedup.equal?("lit".dedup)
p s.dedup.equal?(-s)
p String.instance_method(:dedup).owner
p String.instance_method(:dedup).original_name
p String.instance_method(:dedup).arity
`
	const want = "\"lit\"\ntrue\ntrue\ntrue\nString\n:-@\n0\n"
	if got := eval(t, src); got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

// TestAliasOfANativeMethodKeepsItsOriginalName is the general defect that
// writing the alias above uncovered, and it is reachable from ordinary Ruby
// with no builtin involved on the user's side.
//
// methodOriginalName recovers the original for two of the three kinds of
// method: an iseq-backed one shares its iseq, whose Name is the original, and a
// transplanted body carries origName. A NATIVE method has neither, so the
// fallback returned the ALIAS's own name:
//
//	class String; alias my_up upcase; end
//	String.instance_method(:my_up).original_name
//	  ruby 4.0.5  :upcase
//	  before      :my_up
//
// aliasMethod now pins it, which also makes an alias OF an alias answer the
// first original rather than the intermediate name -- ruby does the same,
// measured, and the last row is what says so.
func TestAliasOfANativeMethodKeepsItsOriginalName(t *testing.T) {
	src := `class Foo
  def orig; 1; end
  alias a1 orig
  alias a2 a1
end
class String
  alias my_up upcase
  alias my_up2 my_up
end
p Foo.instance_method(:a1).original_name
p Foo.instance_method(:a2).original_name
p String.instance_method(:my_up).original_name
p String.instance_method(:my_up2).original_name
p "ab".my_up
`
	const want = ":orig\n:orig\n:upcase\n:upcase\n\"AB\"\n"
	if got := eval(t, src); got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

// TestAliasOfANativeMethodEqualsItsOriginal: MRI's method_eq compares
// DEFINITIONS, not names -- rb_method_entry_eq(m1->me, m2->me) (proc.c
// r4:2007) -- and an alias shares its original's definition, so
//
//	String.instance_method(:dedup) == String.instance_method(:-@)   # => true
//
// which is the whole of core/string/dedup_spec.rb (0 -> 1 with this pin).
//
// An iseq- or proc-backed alias was already equal: the clone shares that
// pointer. A NATIVE one was not, and could not be: every Go closure built from
// one literal shares a code pointer, so keying natives on the func pointer
// would equate methods MRI holds apart (the attr_reader row below is that
// control, and it is false in ruby 4.0.5 too, which keys an IVAR definition on
// the ivar name).
//
// The redefinition rows are the discriminating control: MRI answers true before
// and false after, because the redefinition installs a new definition that the
// alias -- still holding the old one -- no longer shares. Both are measured.
func TestAliasOfANativeMethodEqualsItsOriginal(t *testing.T) {
	src := `class A
  attr_reader :x, :y
  def m1; end
  alias m2 m1
end
class String
  alias my_up upcase
end
im = ->(c, n) { c.instance_method(n) }
p im.(String, :dedup) == im.(String, :-@)
p im.(String, :dedup).hash == im.(String, :-@).hash
p im.(String, :my_up) == im.(String, :upcase)
p im.(A, :m1) == im.(A, :m2)
p im.(A, :x) == im.(A, :y)
p im.(String, :downcase) == im.(String, :my_up)
p im.(String, :dedup) == im.(Object, :frozen?)
class String
  def upcase; "redefined"; end
end
p im.(String, :my_up) == im.(String, :upcase)
p "ab".my_up
p "ab".upcase
`
	const want = "true\ntrue\ntrue\ntrue\nfalse\nfalse\nfalse\n" +
		"false\n\"AB\"\n\"redefined\"\n"
	if got := eval(t, src); got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}
