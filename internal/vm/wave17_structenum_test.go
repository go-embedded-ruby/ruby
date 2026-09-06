package vm_test

import (
	"strings"
	"testing"
)

// TestWave17EnumeratorInitialize covers Enumerator#initialize / Enumerator::Lazy
// #initialize over Class#allocate (the properly-typed uninitialized value, its
// #inspect form, size handling, frozen refusal and privacy), plus the lazy-aware
// #zip block form, #to_enum driving, and #each_with_index / #each_with_object /
// #with_object — all asserted against MRI Ruby 4.0.
func TestWave17EnumeratorInitialize(t *testing.T) {
	cases := []struct{ src, want string }{
		// --- allocate + uninitialized #inspect ---
		{`p Enumerator.allocate.inspect`, "\"#<Enumerator: uninitialized>\"\n"},
		{`p Enumerator::Lazy.allocate.inspect`, "\"#<Enumerator::Lazy: uninitialized>\"\n"},
		// #initialize returns self and installs the generator block
		{`e = Enumerator.allocate
r = e.send(:initialize) { |y| y << 5 << 6 }
p r.equal?(e)
p e.to_a`, "true\n[5, 6]\n"},
		// #size from the given size argument: Integer / nil / absent / callable
		{`p Enumerator.allocate.send(:initialize, 100) {}.size`, "100\n"},
		{`p Enumerator.allocate.send(:initialize, nil) {}.size`, "nil\n"},
		{`p Enumerator.allocate.send(:initialize) {}.size`, "nil\n"},
		{`p Enumerator.allocate.send(:initialize, -> { 200 }) {}.size`, "200\n"},
		// #initialize is private
		{`p Enumerator.private_instance_methods(false).include?(:initialize)`, "true\n"},
		// re-initialising a live Enumerator replaces its source
		{`e = Enumerator.new { |y| y << 1 }
e.send(:initialize) { |y| y << 9 }
p e.to_a`, "[9]\n"},
		// --- Enumerator::Lazy#initialize ---
		{`o = Object.new
def o.each; yield 0; yield 1; yield 2; end
l = Enumerator::Lazy.allocate
r = l.send(:initialize, o) { |y, *v| y.<<(*v) }
p r.equal?(l)
p r.first(2)`, "true\n[0, 1]\n"},
		{`o = Object.new
def o.each; yield 0; end
p Enumerator::Lazy.allocate.send(:initialize, o, 100) {}.size`, "100\n"},
		{`o = Object.new
def o.each; yield 0; end
p Enumerator::Lazy.allocate.send(:initialize, o) {}.size`, "nil\n"},
		{`p Enumerator::Lazy.private_instance_methods(false).include?(:initialize)`, "true\n"},
		// --- frozen? tracks through the boxed state ---
		{`e = Enumerator.new { |y| y << 1 }; e.freeze; p e.frozen?`, "true\n"},
		{`l = [1].lazy; l.freeze; p l.frozen?`, "true\n"},
		// an ivar set on an Enumerator round-trips through the boxed state
		{`e = [1].each; e.instance_variable_set(:@x, 42); p e.instance_variable_get(:@x)`, "42\n"},
		// --- Enumerator::Lazy#zip with a block behaves as Enumerable#zip ---
		{`y = []
r = [1, 2, 3].lazy.zip([4, 5]) { |t| y << t }
p r
p y`, "nil\n[[1, 4], [2, 5], [3, nil]]\n"},
		// #zip without a block stays lazy
		{`p [1, 2, 3].lazy.zip([4, 5, 6]).first(2)`, "[[1, 4], [2, 5]]\n"},
		// --- each_with_index / each_with_object / with_object on a Lazy ---
		{`p [10, 20, 30].lazy.each_with_index.first(3)`, "[[10, 0], [20, 1], [30, 2]]\n"},
		{`a = []
[10, 20].lazy.each_with_index { |x, i| a << [x, i] }
p a`, "[[10, 0], [20, 1]]\n"},
		{`p [1, 2].lazy.each_with_object(:m).first(2)`, "[[1, :m], [2, :m]]\n"},
		{`p [1, 2, 3].lazy.each_with_object([]) { |x, m| m << x * 2 }`, "[2, 4, 6]\n"},
		{`p [1, 2].lazy.with_object(:z).first(2)`, "[[1, :z], [2, :z]]\n"},
		// --- Lazy#to_enum drives a lazy transform and an eager Enumerable method ---
		{`p (0..Float::INFINITY).lazy.to_enum(:with_index, 10).first(3)`, "[[0, 10], [1, 11], [2, 12]]\n"},
		// a finite source forced to completion drives the whole generator
		{`p [1, 2, 3].lazy.to_enum(:with_index, 10).force`, "[[1, 10], [2, 11], [3, 12]]\n"},
		{`p (0..Float::INFINITY).lazy.to_enum(:each_slice, 2).map { |arr| arr.sum }.first(3)`, "[1, 5, 9]\n"},
		// --- lazy over an Enumerator with an overridden (singleton) #each ---
		// single-value yields are pulled through the dispatching fiber
		{`e = Object.new.to_enum
def e.each; yield 10; yield 20; end
p e.lazy.map { |x| x + 1 }.force`, "[11, 21]\n"},
		// multi-value yields are gathered, and an infinite override stays usable
		{`e = Object.new.to_enum
def e.each; i = 0; loop { yield i, i * 2; i += 1 }; end
p e.lazy.map { |a| a }.first(3)`, "[0, 1, 2]\n"},
		// uniq over multi-argument yields keys on the block value
		{`e = Object.new.to_enum
def e.each; yield 0, "foo"; yield 1, "FOO"; yield 2, "bar"; end
p e.lazy.uniq { |_, l| l.downcase }.force`, "[[0, \"foo\"], [2, \"bar\"]]\n"},
		// used by parent methods: every listed helper returns a Lazy (no crash)
		{`L = (0..Float::INFINITY).lazy
r = { each_with_index: [], with_index: [], each_with_object: [Object.new],
      with_object: [Object.new], each_slice: [2], each_cons: [2] }.map { |m, a|
  L.send(m, *a).instance_of?(Enumerator::Lazy)
}
p r.all?`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}

	errs := []struct{ src, want string }{
		// Enumerator#initialize without a block (MRI's Proc message)
		{`Enumerator.allocate.send(:initialize)`, "tried to create Proc object without a block"},
		// too many positional arguments
		{`Enumerator.allocate.send(:initialize, 1, 2) {}`, "ArgumentError"},
		// a frozen receiver refuses re-initialisation
		{`e = Enumerator.allocate; e.freeze; e.send(:initialize) {}`, "FrozenError"},
		// Lazy#initialize without a block / without a source
		{`Enumerator::Lazy.allocate.send(:initialize, Object.new)`, "tried to call lazy new without a block"},
		{`Enumerator::Lazy.allocate.send(:initialize) {}`, "ArgumentError"},
		{`l = Enumerator::Lazy.allocate; l.freeze; l.send(:initialize, Object.new) {}`, "FrozenError"},
		// lazy each_with_index rejects an argument; each_with_object requires one
		{`[1, 2, 3].lazy.each_with_index(:x) {}`, "ArgumentError"},
		{`[1, 2, 3].lazy.each_with_object`, "given 0, expected 1"},
	}
	for _, c := range errs {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got=%v want containing %q", c.src, err, c.want)
		}
	}
}
