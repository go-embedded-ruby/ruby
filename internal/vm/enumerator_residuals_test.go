package vm_test

import (
	"strings"
	"testing"
)

// TestEnumeratorResiduals covers the Enumerator + Enumerator::Lazy conformance
// residuals: #feed, Yielder#to_proc/#<< arity, #each with appended arguments,
// #each_with_index arity, the uninitialized #inspect form, Enumerator::Lazy.new,
// lazy #size propagation, lazy external iteration, lazy #to_enum/#enum_for, the
// method aliases, lazy multi-value block semantics (rb_yield_values2 vs gathered),
// flat_map flattening, lazy #zip validation, lazy #with_index nil offset and lazy
// #take quota/zero-count behaviour — all asserted against MRI Ruby 4.0.
func TestEnumeratorResiduals(t *testing.T) {
	cases := []struct{ src, want string }{
		// --- #feed: the queued value is what the source's next yield returns ---
		{`e = Object.new
def e.each; 3.times { |i| $out << yield }; end
$out = []
en = e.to_enum(:each)
en.feed :a
en.next; en.next; en.next
p $out`, "[:a, nil]\n"},
		// #feed called during iteration; #feed returns nil
		{`e = Object.new
def e.each; 3.times { $out << yield }; end
$out = []
en = e.to_enum(:each)
en.next
p(en.feed(:a))
en.next; en.next
p $out`, "nil\n[:a, nil]\n"},
		// Yielder#yield surfaces the fed value
		{`out = []
enum = Enumerator.new { |y| out << y.yield }
enum.next
enum.feed :a
(enum.next rescue nil)
p out`, "[:a]\n"},
		// #rewind discards a pending feed
		{`e = Object.new
def e.each; 2.times { $out << yield }; end
$out = []
en = e.to_enum(:each)
en.feed :a
en.rewind
en.next; en.next
p $out`, "[nil]\n"},

		// --- Yielder#to_proc: &yielder feeds each element in ---
		{`e = Enumerator.new { |y| "a\nb".each_line(&y) }
p e.to_a`, "[\"a\\n\", \"b\"]\n"},
		// Yielder#yield returns the yielder-consumer value (nil here), #<< chains
		{`e = Enumerator.new { |y| (y << 1) << 2 }
p e.to_a`, "[1, 2]\n"},

		// --- Enumerator#each with appended arguments ---
		{`o = Object.new
def o.each(*a); yield a; end
e = o.to_enum(:each, :x, :y)
p e.each(:z).to_a`, "[[:x, :y, :z]]\n"},
		// each(*args) with a block forwards through
		{`o = Object.new
def o.each(*a); yield a; :ret; end
e = o.to_enum(:each, :x)
p(e.each(:y) { |v| $seen = v })
p $seen`, ":ret\n[:x, :y]\n"},

		// --- uninitialized #inspect ---
		{`p Enumerator.allocate.inspect`, "\"#<Enumerator: uninitialized>\"\n"},
		{`p Enumerator::Lazy.allocate.inspect`, "\"#<Enumerator::Lazy: uninitialized>\"\n"},
		// a live enumerator still inspects normally
		{`p [1, 2].each.inspect`, "\"#<Enumerator: [1, 2]:each>\"\n"},

		// --- Enumerator::Lazy.new ---
		{`r = Object.new
def r.each; yield 0; yield 1; yield 2; end
p Enumerator::Lazy.new(r) { |y, *v| y.<<(*v) }.first(2)`, "[0, 1]\n"},
		{`p Enumerator::Lazy.new(Object.new, 100) {}.map {}.size`, "100\n"},
		{`p Enumerator::Lazy.new(Object.new, 100) {}.select { true }.size`, "nil\n"},
		{`p Enumerator::Lazy.new(Object.new, nil) {}.size`, "nil\n"},
		{`p Enumerator::Lazy.new(Object.new, -> { 7 }) {}.size`, "7\n"},

		// --- lazy #size across the op chain ---
		{`p (1..100).lazy.map { |x| x }.size`, "100\n"},
		{`p (1..100).lazy.with_index.size`, "100\n"},
		{`p (1..100).lazy.zip([1]).size`, "100\n"},
		{`p (1..100).lazy.reject { |x| x }.size`, "nil\n"},
		{`p (1..100).lazy.take(20).size`, "20\n"},
		{`p (1..100).lazy.take(200).size`, "100\n"},
		{`p (1..100).lazy.drop(20).size`, "80\n"},
		{`p (1..100).lazy.drop(200).size`, "0\n"},
		{`p (1..Float::INFINITY).lazy.take(20).size`, "20\n"},
		{`p (1..Float::INFINITY).lazy.drop(20).size`, "Infinity\n"},
		{`p (1..Float::INFINITY).lazy.map { |x| x }.size`, "Infinity\n"},
		// a source with no #size yields nil
		{`o = Object.new
def o.each; yield 1; end
o.extend(Enumerable)
p o.lazy.map { |x| x }.size`, "nil\n"},

		// --- lazy external iteration (no over-evaluation) ---
		{`n = 0
e = [1, 2, 3].lazy.select { n += 1; true }
p n
e.peek; e.peek
p n
e.next
p n`, "0\n1\n1\n"},
		{`e = (1..Float::INFINITY).lazy.map { |x| x * 2 }
p [e.next, e.next, e.next]`, "[2, 4, 6]\n"},
		{`e = [1, 2].lazy.map { |x| x }
e.next; e.rewind
p e.next`, "1\n"},
		{`e = [10, 20].lazy.map { |x| x }
p e.next_values
p e.peek_values`, "[10]\n[20]\n"},

		// --- lazy #to_enum / #enum_for return a distinct Lazy ---
		{`l = (1..3).lazy
p l.to_enum.class`, "Enumerator::Lazy\n"},
		{`l = (1..3).lazy
p l.to_enum.equal?(l)`, "false\n"},
		{`p Enumerator::Lazy.new(Object.new, 100) {}.to_enum { 30 }.size`, "30\n"},
		{`p Enumerator::Lazy.new(Object.new, 100) {}.to_enum.size`, "nil\n"},

		// --- aliases share the method object ---
		{`p Enumerator::Lazy.instance_method(:collect) == Enumerator::Lazy.instance_method(:map)`, "true\n"},
		{`p Enumerator::Lazy.instance_method(:filter) == Enumerator::Lazy.instance_method(:select)`, "true\n"},
		{`p Enumerator::Lazy.instance_method(:find_all) == Enumerator::Lazy.instance_method(:select)`, "true\n"},
		{`p Enumerator::Lazy.instance_method(:collect_concat) == Enumerator::Lazy.instance_method(:flat_map)`, "true\n"},
		{`p Enumerator::Lazy.instance_method(:enum_for) == Enumerator::Lazy.instance_method(:to_enum)`, "true\n"},

		// --- multi-value block semantics (rb_yield_values2 vs gathered) ---
		// map/flat_map/filter_map/take_while/drop_while see the first value
		{`o = Object.new
def o.each; yield 0, 1; yield 2, 3; end
p o.to_enum.lazy.map { |v| v }.force`, "[0, 2]\n"},
		{`o = Object.new
def o.each; yield 0, 1; yield 2, 3; end
p o.to_enum.lazy.flat_map { |v| [v] }.force`, "[0, 2]\n"},
		{`o = Object.new
def o.each; yield 0, 1; yield 2, 3; end
p o.to_enum.lazy.take_while { |v| true }.force`, "[[0, 1], [2, 3]]\n"},
		{`o = Object.new
def o.each; yield 0, 1; yield 2, 3; end
p o.to_enum.lazy.drop_while { |v| false }.force`, "[[0, 1], [2, 3]]\n"},
		// select/reject/uniq see the gathered array
		{`o = Object.new
def o.each; yield 0, 1; yield 2, 3; end
s = []
o.to_enum.lazy.select { |v| s << v; true }.force
p s`, "[[0, 1], [2, 3]]\n"},
		{`o = Object.new
def o.each; yield 0, 1; yield 2, 3; end
p o.to_enum.lazy.uniq.force`, "[[0, 1], [2, 3]]\n"},
		// gathered multi-value survives a filter into a downstream map (sees first)
		{`o = Object.new
def o.each; yield 0, 1; yield 2, 3; end
p o.to_enum.lazy.select { true }.map { |v| v }.force`, "[0, 2]\n"},
		// with_index gathers the value, no-block pairs it
		{`o = Object.new
def o.each; yield 0, 1; yield 2, 3; end
p o.to_enum.lazy.with_index.force`, "[[[0, 1], 0], [[2, 3], 1]]\n"},

		// --- flat_map flattening rules ---
		{`p [1, 2].lazy.flat_map { |n| [n, -n] }.force`, "[1, -1, 2, -2]\n"},
		{`p [1, 2].lazy.flat_map { |n| n.to_s.each_char.lazy }.force`, "[\"1\", \"2\"]\n"},
		{`p [1, 2].lazy.flat_map { |n| n.to_s.each_char }.first(2).all? { |o| o.instance_of?(Enumerator) }`, "true\n"},

		// --- with_index nil offset starts at 0 ---
		{`p [10, 20].lazy.with_index(nil).force`, "[[10, 0], [20, 1]]\n"},
		{`p [10, 20].lazy.with_index(5).force`, "[[10, 5], [20, 6]]\n"},

		// --- take quota / zero-count ---
		{`$out = []
o = Object.new
def o.each; $out << :before; yield 0; $out << :after; end
o.to_enum.lazy.take(1).force
p $out`, "[:before]\n"},
		{`$out = []
o = Object.new
def o.each; $out << :before; yield 0; end
o.to_enum.lazy.take(0).force
p $out`, "[]\n"},
		{`p (1..Float::INFINITY).lazy.map { |x| x }.take(3).force`, "[1, 2, 3]\n"},

		// --- eager/force still work ---
		{`p [1, 2, 3].lazy.map { |x| x * 2 }.to_a`, "[2, 4, 6]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}

	errs := []struct{ src, want string }{
		// #feed twice without advancing raises TypeError
		{`e = [1, 2].each; e.feed :a; e.feed :b`, "TypeError"},
		// Yielder#<< with more than one argument
		{`Enumerator.new { |y| y.<<(1, 2) }.to_a`, "wrong number of arguments (given 2, expected 1)"},
		// each_with_index rejects extra arguments
		{`[1].to_enum.each_with_index(:x)`, "ArgumentError"},
		// Enumerator::Lazy.new without a block
		{`Enumerator::Lazy.new(Object.new)`, "ArgumentError"},
		// Enumerator::Lazy.new without a source
		{`Enumerator::Lazy.new { |y| }`, "ArgumentError"},
		// lazy zip with a non-list argument
		{`[1, 2].lazy.zip([], Object.new)`, "TypeError"},
	}
	for _, c := range errs {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got=%v want containing %q", c.src, err, c.want)
		}
	}
}
