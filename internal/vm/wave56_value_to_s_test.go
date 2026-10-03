// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestNilTrueFalseRangeOwnTheirToS: MRI defines #to_s on NilClass, TrueClass,
// FalseClass and Range. rbgo defined none of them, so all four fell through to
// Kernel#to_s and reported Kernel as #owner.
//
// The TEXT was already right on every one -- Kernel#to_s forwards to the Go
// type's own ToS(), which knows what to print -- which is exactly why this
// survived: nothing that reads the output could see it. It is visible only
// through #owner, #method and instance_methods(false), and those are what the
// rows below pin, alongside the texts to show they did not move.
//
// It also has to land BEFORE Kernel#to_s grows MRI's rb_any_to_s address
// (#756): with these four still inheriting it, an address there would turn
// nil.to_s into "#<NilClass:0x...>".
func TestNilTrueFalseRangeOwnTheirToS(t *testing.T) {
	src := `[[nil, "nil"], [true, "true"], [false, "false"], [(1..2), "(1..2)"]].each do |v, l|
  puts "#{l}: owner=#{v.method(:to_s).owner} to_s=#{v.to_s.inspect} own=#{v.class.instance_methods(false).include?(:to_s)}"
end
p nil.to_s.frozen?
p (1..2).to_s
p (1...2).to_s
p ("a".."c").to_s
`
	const want = `nil: owner=NilClass to_s="" own=true
true: owner=TrueClass to_s="true" own=true
false: owner=FalseClass to_s="false" own=true
(1..2): owner=Range to_s="1..2" own=true
false
"1..2"
"1...2"
"a..c"
`
	if got := eval(t, src); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}
