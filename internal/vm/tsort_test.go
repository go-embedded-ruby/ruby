// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import "testing"

// tsortPrelude defines a graph as a Hash that includes TSort (the canonical MRI
// example): tsort_each_node enumerates the keys, tsort_each_child yields a key's
// dependency list. Reused by the mixin test cases.
const tsortPrelude = `require "tsort"
class TGraph < Hash
  include TSort
  def tsort_each_node(&b) = each_key(&b)
  def tsort_each_child(node, &b) = fetch(node).each(&b)
end
g = TGraph.new
g[1] = [2, 3]; g[2] = [3]; g[3] = []; g[4] = []
`

// tsortCallables defines the singleton-form callbacks for the 1->2->3 chain as
// objects responding to #call with a block (the #call body uses yield). MRI's
// TSort.tsort(each_node, each_child) accepts any such callables (a method object,
// a proc, etc.); a yield-based #call exercises the same path portably.
const tsortCallables = `require "tsort"
EachNode = Object.new
def EachNode.call
  [1, 2, 3].each { |n| yield n }
end
EachChild = Object.new
def EachChild.call(node)
  {1 => [2], 2 => [3], 3 => []}.fetch(node).each { |c| yield c }
end
each_node = EachNode
each_child = EachChild
`

// TestTSort covers the TSort mixin and singleton forms backed by
// github.com/go-ruby-tsort/tsort (the MRI-4.0.5 faithful Tarjan port):
// topological sort, strongly connected components, the each_* iterators, the
// _from variant, and the cycle error — values asserted against MRI 4.0.5's TSort.
func TestTSort(t *testing.T) {
	cases := []struct{ src, want string }{
		// Mixin #tsort: children before parents, then independent nodes.
		{tsortPrelude + `p g.tsort`, "[3, 2, 1, 4]\n"},
		// #tsort_each yields the sorted nodes.
		{tsortPrelude + `r = []; g.tsort_each { |n| r << n }; p r`, "[3, 2, 1, 4]\n"},
		{tsortPrelude + `p g.tsort_each.class`, "Enumerator\n"},
		// #strongly_connected_components: one singleton SCC per node (acyclic),
		// in reverse topological order.
		{tsortPrelude + `p g.strongly_connected_components`, "[[3], [2], [1], [4]]\n"},
		// #each_strongly_connected_component yields each component.
		{tsortPrelude + `r = []; g.each_strongly_connected_component { |c| r << c }; p r`, "[[3], [2], [1], [4]]\n"},
		{tsortPrelude + `p g.each_strongly_connected_component.class`, "Enumerator\n"},
		// #each_strongly_connected_component_from walks the subgraph from a start
		// node (the start itself is NOT enumerated as a node set).
		{tsortPrelude + `r = []; g.each_strongly_connected_component_from(1) { |c| r << c }; p r`, "[[3], [2], [1]]\n"},
		{tsortPrelude + `p g.each_strongly_connected_component_from(1).class`, "Enumerator\n"},

		// Singleton form: TSort.tsort(each_node, each_child) where each callback is
		// any object responding to #call with a block (here #call uses yield).
		{tsortCallables + `p TSort.tsort(each_node, each_child)`, "[3, 2, 1]\n"},
		{tsortCallables + `p TSort.strongly_connected_components(each_node, each_child)`, "[[3], [2], [1]]\n"},
		{tsortCallables + `r = []; TSort.each_strongly_connected_component(each_node, each_child) { |c| r << c }; p r`, "[[3], [2], [1]]\n"},
		{tsortCallables + `r = []; TSort.each_strongly_connected_component_from(1, each_child) { |c| r << c }; p r`, "[[3], [2], [1]]\n"},

		// A self-loop is a single-node SCC and does NOT raise (matching MRI).
		{`require "tsort"
class SGraph < Hash
  include TSort
  def tsort_each_node(&b) = each_key(&b)
  def tsort_each_child(node, &b) = fetch(node).each(&b)
end
s = SGraph.new; s[1] = [1]; p s.tsort`, "[1]\n"},

		// defined? after require; require returns true once then false.
		{`require "tsort"; p defined?(TSort)`, "\"constant\"\n"},
		{`require "tsort"; p(TSort::Cyclic.ancestors.include?(StandardError))`, "true\n"},
		{`p require("tsort"); p require("tsort")`, "true\nfalse\n"},
	}
	for _, tc := range cases {
		if got := eval(t, tc.src); got != tc.want {
			t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
		}
	}
}

// TestTSortLazyLoad asserts the TSort module is absent until require "tsort".
func TestTSortLazyLoad(t *testing.T) {
	if got := eval(t, `p defined?(TSort)`); got != "nil\n" {
		t.Errorf("TSort should be undefined before require, got %q", got)
	}
}

// TestTSortCyclic covers the cycle error: a multi-node SCC raises TSort::Cyclic
// with MRI's exact "topological sort failed" message, rendered with Ruby
// #inspect of the offending component.
func TestTSortCyclic(t *testing.T) {
	src := `require "tsort"
class CGraph < Hash
  include TSort
  def tsort_each_node(&b) = each_key(&b)
  def tsort_each_child(node, &b) = fetch(node).each(&b)
end
c = CGraph.new; c[1] = [2]; c[2] = [1]; c.tsort`
	class, msg := evalErr(t, src)
	if class != "TSort::Cyclic" {
		t.Fatalf("got class %s, want TSort::Cyclic", class)
	}
	if msg != "topological sort failed: [1, 2]" && msg != "topological sort failed: [2, 1]" {
		t.Errorf("unexpected Cyclic message %q", msg)
	}
}
