// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// dmRun runs a Ruby program with `require "deep_merge"` prepended.
func dmRun(t *testing.T, body string) string {
	t.Helper()
	return runSrc(t, "require \"deep_merge\"\n"+body)
}

// TestDeepMergeModuleNonBangNested covers DeepMerge.deep_merge (non-bang): a
// recursive merge that leaves dest untouched and preserves integer values.
func TestDeepMergeModuleNonBangNested(t *testing.T) {
	got := dmRun(t, `
dest = {"a" => {"x" => 1}, "b" => 2}
m = DeepMerge.deep_merge({"a" => {"y" => 3}, "c" => 4}, dest)
puts m["a"]["x"]
puts m["a"]["y"]
puts m["b"]
puts m["c"]
puts dest.key?("c")
`)
	want := "1\n3\n2\n4\nfalse"
	if got != want {
		t.Fatalf("module deep_merge (non-bang):\n got=%q\nwant=%q", got, want)
	}
}

// TestDeepMergeHashBang covers Hash#deep_merge!: it mutates the receiver in place,
// returns the same object, and preserves symbol key identity.
func TestDeepMergeHashBang(t *testing.T) {
	got := dmRun(t, `
h = {a: 1}
r = h.deep_merge!({b: 2})
puts r.equal?(h)
puts h[:a]
puts h[:b]
puts h.keys.map(&:class).uniq.inspect
`)
	want := "true\n1\n2\n[Symbol]"
	if got != want {
		t.Fatalf("deep_merge! (bang):\n got=%q\nwant=%q", got, want)
	}
}

// TestDeepMergeBangScalarSourceLeavesReceiver covers the branch where a non-hash
// source overwrites the hash: the receiver's Hash identity cannot become the
// scalar, so it is left unchanged.
func TestDeepMergeBangScalarSourceLeavesReceiver(t *testing.T) {
	got := dmRun(t, `
h = {"a" => 1}
r = h.deep_merge!(5)
puts r.class
puts r["a"]
`)
	want := "Hash\n1"
	if got != want {
		t.Fatalf("scalar source:\n got=%q\nwant=%q", got, want)
	}
}

// TestDeepMergeModule covers the DeepMerge module form (source, dest): the bang
// form mutates a Hash dest, the non-bang form returns a new value, and a non-Hash
// dest returns the converted merge result.
func TestDeepMergeModule(t *testing.T) {
	got := dmRun(t, `
dest = {"a" => 1}
DeepMerge.deep_merge!({"b" => 2}, dest)
puts dest["a"]
puts dest["b"]
puts DeepMerge.deep_merge({"b" => 2}, {"a" => 1})["b"]
puts DeepMerge.deep_merge!([2], [1]).sort.inspect
`)
	want := "1\n2\n2\n[1, 2]"
	if got != want {
		t.Fatalf("module form:\n got=%q\nwant=%q", got, want)
	}
}

// TestDeepMergeScalarValueTypes covers the deepMergeToRuby scalar cases: float,
// bool, string, nil and a Bignum (*big.Int).
func TestDeepMergeScalarValueTypes(t *testing.T) {
	got := dmRun(t, `
r = DeepMerge.deep_merge({"x" => 2}, {"f" => 1.5, "b" => true, "s" => "hi", "n" => nil, "big" => 10 ** 20})
puts r["f"]
puts r["b"]
puts r["s"]
puts r["n"].inspect
puts r["big"]
puts(r["big"] == 10 ** 20)
puts r["x"]
`)
	want := "1.5\ntrue\nhi\nnil\n100000000000000000000\ntrue\n2"
	if got != want {
		t.Fatalf("scalar value types:\n got=%q\nwant=%q", got, want)
	}
}

// TestDeepMergeNestedArrayOfHashes covers the collectHashKeys array recursion and
// the []any conversion, merging a hash that holds an array of hashes.
func TestDeepMergeNestedArrayOfHashes(t *testing.T) {
	got := dmRun(t, `
r = DeepMerge.deep_merge({"x" => 2}, {"list" => [{"k" => 1}]})
puts r["list"].inspect
puts r["x"]
`)
	want := `[{"k" => 1}]` + "\n2"
	if got != want {
		t.Fatalf("nested array of hashes:\n got=%q\nwant=%q", got, want)
	}
}

// TestDeepMergeAllOptions parses every supported option key once (covering each
// switch arm and all three truthiness arms: a bool, a non-bool truthy value and
// nil), on a valid bang call whose knockout prefix is allowed by overwrite.
func TestDeepMergeAllOptions(t *testing.T) {
	got := dmRun(t, `
opts = {
  "preserve_unmergeables" => false,
  "knockout_prefix" => "--",
  "overwrite_arrays" => false,
  "sort_merged_arrays" => "yes",
  "unpack_arrays" => ",",
  "merge_hash_arrays" => false,
  "extend_existing_arrays" => false,
  "keep_array_duplicates" => nil,
  "merge_nil_values" => true,
}
r = {"a" => ["x"]}.deep_merge!({"a" => ["y", "z"]}, opts)
puts r["a"].inspect
`)
	want := `["x", "y", "z"]`
	if got != want {
		t.Fatalf("all options:\n got=%q\nwant=%q", got, want)
	}
}

// TestDeepMergeKnockoutRemovesElement covers a functional knockout: a source
// element prefixed with the knockout string removes the matching dest element.
func TestDeepMergeKnockoutRemovesElement(t *testing.T) {
	got := dmRun(t, `
r = {"a" => ["x", "y"]}.deep_merge!({"a" => ["--y", "z"]}, {"knockout_prefix" => "--"})
puts r["a"].inspect
`)
	want := `["x", "z"]`
	if got != want {
		t.Fatalf("knockout:\n got=%q\nwant=%q", got, want)
	}
}

// TestDeepMergeInvalidParameter covers the pre-validated invalid combination — a
// knockout prefix on a non-bang (preserve_unmergeables) call — raising
// DeepMerge::InvalidParameter, which is a StandardError.
func TestDeepMergeInvalidParameter(t *testing.T) {
	got := dmRun(t, `
src = {"b" => 2}
dest = {"a" => 1}
opts = {"knockout_prefix" => "--"}
begin
  DeepMerge.deep_merge(src, dest, opts)
  puts "NORAISE"
rescue => e
  puts e.class
  puts(e.is_a?(StandardError))
end
`)
	want := "DeepMerge::InvalidParameter\ntrue"
	if got != want {
		t.Fatalf("invalid parameter:\n got=%q\nwant=%q", got, want)
	}
}

// TestDeepMergeNilOptions covers the explicit nil options argument (treated as no
// options).
func TestDeepMergeNilOptions(t *testing.T) {
	got := dmRun(t, `puts({"a" => 1}.deep_merge({"b" => 2}, nil)["b"])`)
	if got != "2" {
		t.Fatalf("nil options: got=%q want %q", got, "2")
	}
}

// TestDeepMergeArgumentErrors covers every ArgumentError path: wrong arity on the
// module and Hash forms, and a non-Hash options argument.
func TestDeepMergeArgumentErrors(t *testing.T) {
	// h is a plain hash used as receiver/args so no brace-literal is parsed as a
	// block.
	cases := []string{
		`DeepMerge.deep_merge!(h)`,          // module: too few (1 of 2..3)
		`DeepMerge.deep_merge!(h, h, h, h)`, // module: too many
		`h.deep_merge!`,                     // hash bang: too few (0 of 1..2)
		`h.deep_merge!(h, h, h)`,            // hash bang: too many
		`h.deep_merge!(h, 5)`,               // options not a Hash
	}
	for _, expr := range cases {
		got := dmRun(t, "h = {\"a\" => 1}\nbegin\n  "+expr+"\n  puts \"NORAISE\"\nrescue ArgumentError\n  puts \"ArgumentError\"\nend")
		if got != "ArgumentError" {
			t.Fatalf("%s expected ArgumentError, got %q", expr, got)
		}
	}
}
