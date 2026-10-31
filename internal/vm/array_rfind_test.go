package vm_test

import "testing"

// Array#rfind, new in Ruby 4.0 (ruby/ruby array.c at v4.0.9: rb_ary_rfind, and
// rb_define_method(rb_cArray, "rfind", …) at line 8918 — Array's own, not
// Enumerable's, which is also why core/array/{find,detect,rfind}_spec.rb all
// sit behind a `ruby_version_is "4.0"` guard).
//
// The cases follow core/array/rfind_spec.rb. That file is entirely behind the
// 4.0 guard, so the conformance gate cannot see it while the shim resolves at
// 3.4.1; these assertions are what holds the behaviour until it can.
func TestArrayRfind(t *testing.T) {
	checkCases(t, []runCase{
		// The last match, not the first.
		{`p [1, 2, 3, 4, 5].rfind { |x| x % 2 == 0 }`, "4\n"},
		{`p [1, 2, 3, 4, 5, 6].rfind { |x| x < 5 }`, "4\n"},
		{`p [1, 2, 3].rfind { |x| false }`, "nil\n"},

		// ⛔ Reverse order, and it STOPS at the first match from the end. A
		// correct-looking implementation that found the last match by scanning
		// forwards would satisfy the three cases above and fail both of these.
		{`visited = []
[1, 2, 3].rfind { |x| visited << x; false }
p visited`, "[3, 2, 1]\n"},
		{`visited = []
[1, 2, 3, 4, 5].rfind { |x| visited << x; x == 3 }
p visited`, "[5, 4, 3]\n"},

		// The ifnone proc: called when nothing matches, once, not at all when
		// something matches, ignored when nil, and NoMethodError when it is not
		// callable.
		{`p [1, 2, 3].rfind(-> { "cheeseburgers" }) { |x| false }`, "\"cheeseburgers\"\n"},
		{`p [1, 2, 3].rfind(-> { raise "must not be called" }) { |x| x == 3 }`, "3\n"},
		{`times = 0
fail_proc = -> { times += 1; raise if times > 1; "cheeseburgers" }
p [1, 2, 3].rfind(fail_proc) { |x| false }
p times`, "\"cheeseburgers\"\n1\n"},
		{`p [].rfind(-> { "yay" }) { |x| true }`, "\"yay\"\n"},
		{`p [1, 2, 3].rfind(nil) { |x| false }`, "nil\n"},
		{`begin
  [1, 2, 3].rfind(42) { |x| false }
rescue NoMethodError
  puts "NoMethodError"
end`, "NoMethodError\n"},

		// An element is yielded whole; a two-parameter block destructures it, as
		// any Ruby block does.
		{`p [[1, 2], [3, 4, 5], [6, 7, 8, 9]].rfind { |e| e == [1, 2] }`, "[1, 2]\n"},
		{`seen = []
[:a, :b].each_with_index.to_a.rfind { |x, i| seen << [x, i]; nil }
p seen`, "[[:b, 1], [:a, 0]]\n"},

		// No block: an Enumerator, of unknown size, carrying the ifnone proc.
		{`p [1, 2, 3].rfind.instance_of?(Enumerator)`, "true\n"},
		{`p [1, 2, 3].rfind.size`, "nil\n"},
		{`enum = [1, 2, 3].rfind(-> { "cheeseburgers" })
p enum.each { |x| false }`, "\"cheeseburgers\"\n"},

		// ⛔ THE CASE THAT RULES OUT `to_a.reverse.find` AND `reverse_each`.
		// The size is re-read after every yield and the index clamped down to
		// it, so a block that empties the receiver ends the walk after ONE
		// element. core/array/rfind_spec.rb:81. A snapshot-based implementation
		// sees all six and passes every other case in this file.
		{`ary = [4, 2, 1, 5, 1, 3]
seen = []
ary.rfind { |x| seen << x; ary.clear; false }
p seen`, "[3]\n"},

		// The clamp only ever LOWERS the index, so a block that grows the array
		// does not revisit the tail it has already passed.
		{`ary = [1, 2, 3]
seen = []
ary.rfind { |x| seen << x; ary.push(99) if seen.size == 1; false }
p seen`, "[3, 2, 1]\n"},

		// find/detect still walk forwards: rfind is not an alias of either, and
		// a mistake that aliased them would pass none of the reverse cases but
		// is worth pinning from this side too.
		{`p [1, 2, 3, 4].find { |x| x % 2 == 0 }`, "2\n"},
		{`p [1, 2, 3, 4].rfind { |x| x % 2 == 0 }`, "4\n"},
	})
}
