// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import "testing"

// A splat (`*v`) reaches #to_a the way MRI's rb_check_to_array does, which means
// it does not reach it at all in two cases this used to get wrong.
//
// MRI compiles `*v` to the splatarray instruction, whose helper is
//
//	static inline VALUE
//	vm_splat_array(VALUE flag, VALUE ary)
//	{
//	    VALUE tmp = rb_check_to_array(ary);
//	    if (NIL_P(tmp))      return rb_ary_new3(1, ary);
//	    else if (RTEST(flag)) return rb_ary_dup(tmp);
//	    else                  return tmp;
//	}
//
// and rb_check_to_array is rb_check_convert_type_with_id(ary, T_ARRAY, "Array",
// idTo_a). Two properties of that call are the whole of this test: it returns
// the value UNTOUCHED when the value is already T_ARRAY — which an Array
// subclass instance is — and it goes through rb_check_funcall, which asks an
// overridden #respond_to? first and does not call the method when that says no.
//
// Measured against /usr/bin/ruby on the same source before being written here.
func TestSplatDoesNotCallToAWhenMRIWouldNot(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{
			// ⛔ An Array SUBCLASS. rbgo used to miss this because its fast path is a
			// Go type assertion on *object.Array, and a subclass instance is an
			// RObject wrapping one — so the assertion failed, Array#to_a was found
			// in the method table, and the spec's `should_not_receive(:to_a)` fired.
			name: "an Array subclass is already an Array",
			src: `cls = Class.new(Array) do
  def to_a; $called = true; super; end
end
$called = false
a = *cls.new([1, 2])
p [a, $called]`,
			want: "[[1, 2], false]\n",
		}, {
			// ⛔ The neighbour that makes the fix non-obvious: the RESULT must be a
			// plain Array, because vm_splat_array returns rb_ary_dup(tmp). Until the
			// fix this passed only BECAUSE #to_a was wrongly sent — Array#to_a
			// answers with a plain copy — so returning the subclass instance would
			// have repaired the row above by breaking this one.
			name: "the result of splatting a subclass is a plain Array",
			src: `cls = Class.new(Array)
x = (a = *cls.new([1, 2]))
p [x.instance_of?(Array), a.instance_of?(Array)]`,
			want: "[true, true]\n",
		}, {
			// And the dup is a dup: the splat must not alias the subclass instance's
			// own storage, or appending to the result would mutate the original.
			name: "splatting a subclass does not alias its storage",
			src: `cls = Class.new(Array)
ary = cls.new([1, 2])
a = *ary
a << 3
p [ary.size, a.size]`,
			want: "[2, 3]\n",
		}, {
			// ⛔ An object that says it does not respond to #to_a. rbgo asked the
			// method table directly, so it called #to_a on an object that had just
			// answered false — and took the result, which is the visible damage:
			// the element was :wrong instead of the object itself.
			name: "respond_to? false means no conversion",
			src: `class SplatRefuser
  def respond_to?(name, include_private = false); false; end
  def to_a; [:wrong]; end
end
a = *SplatRefuser.new
p [a.size, a.first.is_a?(SplatRefuser)]`,
			want: "[1, true]\n",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := eval(t, c.src); got != c.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
			}
		})
	}
}

// The controls: every shape that must keep working, including the ones that
// prove the conversion still HAPPENS. A splat that had simply stopped calling
// #to_a would pass all four rows above.
func TestSplatStillConverts(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"a plain Array passes through", `p(*[1, 2])`, "1\n2\n"},
		{"an Array is copied, not aliased", `ary = [1, 2]
a = *ary
a << 3
p [ary.size, a.size]`, "[2, 3]\n"},
		{"nil splats to empty", `a = *nil
p a`, "[]\n"},
		{"an object answering #to_a is converted", `class SplatYes
  def to_a; [7, 8]; end
end
p(*SplatYes.new)`, "7\n8\n"},
		// A private #to_a IS reached, by both engines: rb_check_funcall passes
		// priv=TRUE, and here the method table lookup finds a private method just
		// the same. Measured on /usr/bin/ruby — this row asserts MRI's answer, not
		// the one that reads as tidier.
		{"a private #to_a is reached", `class SplatPriv
  private
  def to_a; [7, 8]; end
end
a = *SplatPriv.new
p a`, "[7, 8]\n"},
		{"a nil #to_a result wraps the object", `class SplatNil
  def to_a; nil; end
end
a = *SplatNil.new
p a.first.is_a?(SplatNil)`, "true\n"},
		{"a non-Array #to_a result is a TypeError", `class SplatBad
  def to_a; 42; end
end
begin
  a = *SplatBad.new
rescue TypeError => e
  puts e.message
end`, "can't convert SplatBad to Array (SplatBad#to_a gives Integer)\n"},
		// Hash#to_a exists, so a Hash is CONVERTED, not wrapped — the pair list,
		// not the Hash in a one-element Array. Measured both ways round.
		{"a Hash is converted through #to_a", `a = *{x: 1}
p a`, "[[:x, 1]]\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := eval(t, c.src); got != c.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
			}
		})
	}
}
