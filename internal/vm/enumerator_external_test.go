package vm_test

import (
	"strings"
	"testing"
)

// TestEnumeratorExternal covers Fiber-driven external iteration
// (next/peek/next_values/peek_values/rewind), Enumerator.produce,
// Enumerator::Chain (+/chain), with_index/with_object and #size — all asserted
// against MRI Ruby 4.0.5.
func TestEnumeratorExternal(t *testing.T) {
	cases := []struct{ src, want string }{
		// --- next / peek on a driven receiver ---
		{`e = 1.upto(3); p [e.next, e.next, e.next]`, "[1, 2, 3]\n"},
		{`e = (1..5).to_a.to_enum; p e.peek; p e.peek; p e.next; p e.next`, "1\n1\n1\n2\n"},
		// peek then next consistency, then rewind resets, peek after rewind
		{`e = 1.upto(3); e.next; e.next; e.rewind; e.next; p e.peek`, "2\n"},
		// StopIteration#result is the source's return value (Array#each returns self)
		{`e = [1, 2].each; 2.times { e.next }
begin; e.next; rescue StopIteration => ex; p ex.result; end`, "[1, 2]\n"},
		// generator's return value surfaces as StopIteration#result
		{`e = Enumerator.new { |y| y << 1; 99 }; e.next
begin; e.next; rescue StopIteration => ex; p ex.result; end`, "99\n"},

		// --- next_values / peek_values (rb_enum_values_pack) ---
		{`o = Object.new
def o.each; yield :a; yield :b1, :b2; yield; yield [:f1, :f2]; end
e = o.to_enum
p e.next_values
p e.peek_values
p e.next_values
p e.next_values
p e.next_values`, "[:a]\n[:b1, :b2]\n[:b1, :b2]\n[]\n[[:f1, :f2]]\n"},
		// next unpacks; next_values keeps the array form
		{`o = Object.new
def o.each; yield :a; yield :b1, :b2; end
e = o.to_enum
p e.next
p e.next_values`, ":a\n[:b1, :b2]\n"},

		// --- rewind hook: a driven source that responds to #rewind is rewound ---
		{`obj = Object.new
def obj.each; yield 10; yield 20; end
def obj.rewind; $rw = true; end
e = obj.to_enum; e.next; e.rewind
p $rw; p e.next`, "true\n10\n"},

		// --- Enumerator.produce ---
		{`p Enumerator.produce(1) { |n| n * 2 }.first(5)`, "[1, 2, 4, 8, 16]\n"},
		{`p Enumerator.produce(0) { |prev| raise StopIteration if prev >= 2; prev + 1 }.to_a`, "[0, 1, 2]\n"},
		{`p Enumerator.produce { |prev| (prev || 0) + 1 }.take(3)`, "[1, 2, 3]\n"},
		{`e = Enumerator.produce(1) { |n| n + 1 }; p [e.next, e.next, e.next]`, "[1, 2, 3]\n"},
		{`p Enumerator.produce(0) { |n| n + 1 }.size`, "Infinity\n"},
		{`p Enumerator.produce(0, size: 10) { |n| n + 1 }.size`, "10\n"},
		{`p Enumerator.produce(0, size: -> { 5 * 5 }) { |n| n + 1 }.size`, "25\n"},
		{`p Enumerator.produce(0, size: nil) { |n| n + 1 }.size`, "nil\n"},
		{`p Enumerator.produce(5) { |n| n + 5 }.class`, "Enumerator\n"},

		// --- Enumerator::Chain / + / chain ---
		{`one = Enumerator.new { |y| y << 1 }; two = Enumerator.new { |y| y << 2 }
p (one + two).class`, "Enumerator::Chain\n"},
		{`p ([1, 2].each + [3, 4].each).to_a`, "[1, 2, 3, 4]\n"},
		{`p [1, 2, 3].chain([4, 5]).to_a`, "[1, 2, 3, 4, 5]\n"},
		{`p [1, 2].chain([3, 4], [5]).to_a`, "[1, 2, 3, 4, 5]\n"},
		{`p Enumerator::Chain.new(1..2, 3..4).inspect`, "\"#<Enumerator::Chain: [1..2, 3..4]>\"\n"},
		{`p Enumerator::Chain.new.inspect`, "\"#<Enumerator::Chain: []>\"\n"},
		{`p Enumerator::Chain.new([:a, :b], [:c, :d, :e]).size`, "5\n"},
		// chain size short-circuits on the first nil/Infinity element
		{`p [1, 2].chain(Enumerator.produce(1) { |n| n + 1 }).size`, "Infinity\n"},
		{`p [1, 2].chain(Enumerator.new { |y| y << 1 }).size`, "nil\n"},
		{`p Enumerator.include?(Enumerable)`, "true\n"},
		// chain each drives constituents in order
		{`r = []; Enumerator::Chain.new([:a, :b], [:c]).each { |x| r << x }; p r`, "[:a, :b, :c]\n"},

		// --- chain rewind: reverse order, only entered parts that respond ---
		{`log = []
a = Object.new; a.define_singleton_method(:each) { |&b| [1].each(&b) }; a.define_singleton_method(:rewind) { log << :a }
b = Object.new; b.define_singleton_method(:each) { |&b2| [2].each(&b2) }; b.define_singleton_method(:rewind) { log << :b }
ch = Enumerator::Chain.new(a, b); ch.each {}; ch.rewind; p log`, "[:b, :a]\n"},
		// rewind does nothing for parts not iterated
		{`log = []
a = Object.new; a.define_singleton_method(:each) { |&b| [1].each(&b) }; a.define_singleton_method(:rewind) { log << :a }
ch = Enumerator::Chain.new(a); ch.rewind; p log`, "[]\n"},

		// --- with_index conversions & return values ---
		{`p [10, 20, 30].each.with_index.to_a`, "[[10, 0], [20, 1], [30, 2]]\n"},
		{`p [1, 2, 3, 4].each.with_index(1).to_a`, "[[1, 1], [2, 2], [3, 3], [4, 4]]\n"},
		{`p [1, 2, 3, 4].each.with_index(nil).to_a`, "[[1, 0], [2, 1], [3, 2], [4, 3]]\n"},
		{`p [1, 2, 3, 4].each.with_index(1.678).to_a`, "[[1, 1], [2, 2], [3, 3], [4, 4]]\n"},
		{`p [1, 2, 3, 4].each.with_index(-1).to_a`, "[[1, -1], [2, 0], [3, 1], [4, 2]]\n"},
		{`(o = Object.new).define_singleton_method(:to_int) { 1 }
p [1, 2].each.with_index(o).to_a`, "[[1, 1], [2, 2]]\n"},
		{`e = [1, 2, 3].select; en = e.with_index; p en.instance_of?(Enumerator)`, "true\n"},
		{`p [1, 2, 3].each.with_index.size`, "3\n"},

		// a bare generator's #inspect uses the Generator placeholder + default :each
		{`p Enumerator.new { |y| y << 1 }.inspect`, "\"#<Enumerator: #<Enumerator::Generator>:each>\"\n"},
		// with_object / each_with_object with NO block returns an Enumerator
		{`p [1, 2].each.with_object("m").class`, "Enumerator\n"},
		{`p [3, 4].each.each_with_object("z").class`, "Enumerator\n"},

		// --- with_object / each_with_object ---
		{`p [1, 2, 3].each.with_object([]) { |x, memo| memo << x * 2 }`, "[2, 4, 6]\n"},
		{`p [1, 2, 3].each.with_object("z") { |x, m| }`, "\"z\"\n"},
		{`p [1, 2].each.each_with_object([]) { |x, m| m << x }`, "[1, 2]\n"},
		// with_object is the same method as each_with_object
		{`p Enumerator.instance_method(:with_object) == Enumerator.instance_method(:each_with_object)`, "true\n"},

		// --- Enumerator.new(size) { … } ---
		{`p Enumerator.new(100) {}.size`, "100\n"},
		{`p Enumerator.new(nil) {}.size`, "nil\n"},
		{`base = 100; e = Enumerator.new(-> { base + 1 }) {}; base = 200; p e.size`, "201\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}

	// Error paths.
	type errCase struct{ src, want string }
	errs := []errCase{
		// next / peek / *_values past the end raise StopIteration
		{`e = [1].each; e.next; e.next`, "StopIteration"},
		{`e = [1].each; e.next; e.peek`, "StopIteration"},
		{`e = [1].each; e.next_values; e.next_values`, "StopIteration"},
		{`e = [1].each; e.next; e.peek_values`, "StopIteration"},
		// next after end keeps raising until a rewind
		{`e = [1].each; e.next
begin; e.next; rescue StopIteration; end
e.next`, "StopIteration"},
		// peek after the enumerator is already ended (e.ended already set) re-raises
		{`e = [1].each; e.next
begin; e.next; rescue StopIteration; end
e.peek`, "StopIteration"},
		// with_index with a non-convertible argument raises TypeError
		{`[1, 2].each.with_index("1") { |*i| i }`, "TypeError"},
		// with_object with no argument raises ArgumentError
		{`[1, 2].each.with_object`, "ArgumentError"},
		// produce with no block raises ArgumentError
		{`Enumerator.produce`, "ArgumentError"},
		// produce with unknown keywords raises ArgumentError
		{`Enumerator.produce(a: 1, b: 1) {}`, "unknown keywords"},
		// produce with too many positional arguments
		{`Enumerator.produce(1, 2) { |n| n }`, "ArgumentError"},
		// a non-StopIteration exception from a produce block propagates
		{`Enumerator.produce(1) { |n| raise "boom" if n > 2; n + 1 }.first(5)`, "boom"},
		// Enumerator.new without a block raises ArgumentError
		{`Enumerator.new`, "wrong number of arguments"},
	}
	for _, c := range errs {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got=%v want containing %q", c.src, err, c.want)
		}
	}

	// The enumerator restarts after an exception terminated a previous iteration.
	if got := eval(t, `enum = Enumerator.new { raise "boom" }
p (2.times.map { enum.next rescue $!.message })`); got != "[\"boom\", \"boom\"]\n" {
		t.Errorf("exception-restart: got %q", got)
	}
}
