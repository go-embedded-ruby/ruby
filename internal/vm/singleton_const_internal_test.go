// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestAConstantBoundToItsOwnScope covers a class assigned to a constant inside
// its own body.
//
//	class << o
//	  CONST = self
//	end
//
// The scope and the value are the same singleton class, and a singleton class
// is unnamed, so assignConstIn used to record it as its own lexical parent.
// Every later bare-constant read in that scope then walked a ring: 17 GB for
// six lines, and a CI runner killed under language/singleton_class_spec.rb.
//
// Verified against ruby 3.
func TestAConstantBoundToItsOwnScope(t *testing.T) {
	cases := []struct{ src, want string }{
		// The reading that used to take the machine, in the four shapes
		// language/singleton_class_spec.rb reads it.
		{`o = Object.new
class << o
  CONST = self
end
class << o
  p CONST == self
end`, `true`},
		{`o = Object.new
class << o
  CONST = self
  p CONST.equal?(self)
end`, `true`},
		{`o = Object.new
class << o
  CONST = self
end
class << o
  p self::CONST == self
end`, `true`},
		{`o = Object.new
class << o
  CONST = self
end
class << o
  p const_get(:CONST) == self
end`, `true`},
		// The same shape one level in, where the scope is a real class's
		// singleton rather than an object's.
		{`class Outer
  class << self
    INNER = self
  end
  def self.read; class << self; INNER; end; end
end
p Outer.read == Outer.singleton_class`, `true`},
		// A constant that is not a class is unaffected, and so is a class bound
		// somewhere that is not its own body — it still takes the lexical parent
		// and the qualified name it always did.
		{`o = Object.new
class << o
  CONST = 42
  p CONST
end`, `42`},
		{`module M
  Inner = Class.new
end
p M::Inner.name`, `"M::Inner"`},
		{`module Outer2
  module Inner2
    THING = 7
  end
  p Inner2::THING
end`, `7`},
		// A missing constant still raises rather than being swallowed by any of
		// this.
		{`o = Object.new
class << o
  begin; NOPE; rescue NameError; p :raised; end
end`, `:raised`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}
