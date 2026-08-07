package vm_test

import (
	"strings"
	"testing"
)

// TestChunkCycleStartWith covers Enumerable#chunk / #minmax_by / #cycle(n) and
// String#start_with? with multiple args and a Regexp. Asserted against MRI Ruby
// 4.0.5.
func TestChunkCycleStartWith(t *testing.T) {
	cases := []struct{ src, want string }{
		// String#start_with?: Regexp (must match at offset 0) and several prefixes.
		{`p "Hello".start_with?(/H/)`, "true\n"},
		{`p "Hello".start_with?(/x/)`, "false\n"},   // regex no match
		{`p "Hello".start_with?(/ell/)`, "false\n"}, // matches, but not at offset 0
		{`p "Hello".start_with?("He", "X")`, "true\n"},
		{`p "Hello".start_with?("Z", "He")`, "true\n"}, // first prefix misses, second hits
		{`p "Hello".start_with?("Z", "Y")`, "false\n"}, // none match
		// Enumerable#chunk.
		{`p [1, 2, 2, 3, 3, 3].chunk { |x| x }.map { |k, v| [k, v.size] }`, "[[1, 1], [2, 2], [3, 3]]\n"},
		{`p [1, 1, 2, 3, 3].chunk { |x| x.odd? }.to_a`, "[[true, [1, 1]], [false, [2]], [true, [3, 3]]]\n"},
		// Enumerable#minmax_by.
		{`p [1, 2, 3, 4, 5].minmax_by { |x| (x - 3).abs }`, "[3, 1]\n"},
		{`p (1..5).minmax_by { |x| -x }`, "[5, 1]\n"},
		{`p ["a", "bb", "ccc"].minmax_by(&:length)`, "[\"a\", \"ccc\"]\n"},
		// Enumerable#cycle(n): finite repetition (block and Enumerator forms).
		{`p [1, 2, 3].cycle(2).to_a`, "[1, 2, 3, 1, 2, 3]\n"},
		{`r = []; [1, 2, 3].cycle(2) { |x| r << x }; p r`, "[1, 2, 3, 1, 2, 3]\n"},
		{`p (1..3).cycle(2).to_a`, "[1, 2, 3, 1, 2, 3]\n"},
		{`p [].cycle(3).to_a`, "[]\n"}, // empty -> nothing
		// cycle with no count loops forever; break stops it (covers that branch).
		{`r = []; [1, 2, 3].cycle { |x| r << x; break if r.size >= 4 }; p r`, "[1, 2, 3, 1]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// customEnumChunk is a fresh #each-only Enumerable (also answering #size) used to
// prove Enumerable#chunk works over any Enumerable, not just Array.
const customEnumChunk = "class CE\n include Enumerable\n def initialize(*a) @a=a end\n def each; @a.each { |x| yield x } end\n def size; @a.size end\nend\n"

// TestEnumerableChunkFull covers Enumerable#chunk added in the prelude to MRI
// 3.4/4.0: it returns an Enumerator (with or without a block, #size nil), groups
// consecutive equal keys, drops nil/:_separator (breaking the run), isolates
// :_alone elements, reserves other _-symbols (RuntimeError), rejects arguments,
// gathers multi-value yields, and works over any Enumerable. MRI Ruby 4.0.5.
func TestEnumerableChunkFull(t *testing.T) {
	cases := []struct{ src, want string }{
		// returns an Enumerator both with and without a block.
		{`p [1, 2, 3].chunk { |x| x }.class`, "Enumerator\n"},
		{`p [1, 2, 3].chunk.class`, "Enumerator\n"},
		// no block: with_index supplies the key block (index appended).
		{`p [1, 2, 3, 1, 2].chunk.with_index { |elt, i| elt - i }.to_a`, "[[1, [1, 2, 3]], [-2, [1, 2]]]\n"},
		// [v, ary] grouping of consecutive equal keys.
		{`p [1, 2, 3, 2, 3, 2, 1].chunk { |x| x < 3 && 1 || 0 }.to_a`, "[[1, [1, 2]], [0, [3]], [1, [2]], [0, [3]], [1, [2, 1]]]\n"},
		// :_alone puts each matching element in its own chunk.
		{`p [1, 2, 3, 2, 1].chunk { |x| x < 2 && :_alone }.to_a`, "[[:_alone, [1]], [false, [2, 3, 2]], [:_alone, [1]]]\n"},
		// :_separator drops the element AND breaks the run (same later key = new chunk).
		{`p [1, 2, 3, 3, 2, 1].chunk { |x| x == 2 ? :_separator : 1 }.to_a`, "[[1, [1]], [1, [3, 3]], [1, [1]]]\n"},
		// nil behaves like :_separator (dropped, run broken).
		{`p [1, 2, 3, 2, 1].chunk { |x| x == 2 ? nil : 1 }.to_a`, "[[1, [1]], [1, [3]], [1, [1]]]\n"},
		// a lone Array element is yielded whole to a rest parameter.
		{`p [[1, 2]].chunk { |*x| x }.to_a`, "[[[[1, 2]], [[1, 2]]]]\n"},
		// #size of the returned Enumerator is nil.
		{`p [1, 2, 3].chunk { |x| true }.size`, "nil\n"},
		{`p [1, 2, 3].chunk.size`, "nil\n"},
		// works over a custom Enumerable and gathers multi-value yields.
		{customEnumChunk + `p CE.new(1, 1, 2, 2, 2, 3).chunk { |x| x }.map { |k, v| [k, v.length] }`, "[[1, 2], [2, 3], [3, 1]]\n"},
		{"class MC\n include Enumerable\n def each; yield 1, 2; yield 1, 2; yield 3, 4; end\nend\n" +
			`p MC.new.chunk { |x| x }.to_a`, "[[[1, 2], [[1, 2], [1, 2]]], [[3, 4], [[3, 4]]]]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
	// A reserved underscore symbol (other than :_alone/:_separator) raises
	// RuntimeError; chunk accepts no positional arguments.
	if err := runErr(t, `[1, 2, 3].chunk { |x| :_arbitrary }.to_a`); err == nil || !strings.Contains(err.Error(), "RuntimeError") {
		t.Errorf("reserved _-symbol: got %v want RuntimeError", err)
	}
	if err := runErr(t, `[1, 2, 3].chunk(1) {}`); err == nil || !strings.Contains(err.Error(), "ArgumentError") {
		t.Errorf("chunk with argument: got %v want ArgumentError", err)
	}
}
