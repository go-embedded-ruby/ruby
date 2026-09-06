// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"bytes"
	"strings"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/compiler"
	"github.com/go-ruby-parser/parser"
)

// seamedENV installs an in-memory process environment (order-preserving, like
// envSeams) and returns a VM writing to a buffer, so the ENV method surface can
// be exercised deterministically from Ruby — never touching the real process
// environment.
func seamedENV(t *testing.T) (*VM, *bytes.Buffer) {
	t.Helper()
	store := map[string]string{}
	var order []string
	origGet, origLook, origSet, origUnset, origEnv :=
		envGetenv, envLookup, envSetenv, envUnsetenv, envEnviron
	t.Cleanup(func() {
		envGetenv, envLookup, envSetenv, envUnsetenv, envEnviron =
			origGet, origLook, origSet, origUnset, origEnv
	})
	envGetenv = func(k string) string { return store[k] }
	envLookup = func(k string) (string, bool) { v, ok := store[k]; return v, ok }
	envSetenv = func(k, v string) error {
		// Mirror os.Setenv's validation so the EINVAL path is reachable off the seam.
		if k == "" || strings.ContainsRune(k, '=') {
			return errEINVAL
		}
		if _, ok := store[k]; !ok {
			order = append(order, k)
		}
		store[k] = v
		return nil
	}
	envUnsetenv = func(k string) error {
		delete(store, k)
		for i, kk := range order {
			if kk == k {
				order = append(order[:i], order[i+1:]...)
				break
			}
		}
		return nil
	}
	envEnviron = func() []string {
		out := make([]string, 0, len(order))
		for _, k := range order {
			out = append(out, k+"="+store[k])
		}
		return out
	}
	var buf bytes.Buffer
	return New(&buf), &buf
}

// errEINVAL is a stand-in the seam returns for an invalid name; env.go validates
// the name itself, so the exact error value is never inspected.
var errEINVAL = &envTestErr{}

type envTestErr struct{}

func (*envTestErr) Error() string { return "invalid argument" }

// TestENVWave16 exercises the full ENV method surface added in wave16 against the
// in-memory seam, asserting behaviour verified against MRI (no wall-clock or
// machine-dependent values).
func TestENVWave16(t *testing.T) {
	vm, buf := seamedENV(t)
	run := func(src string) string {
		buf.Reset()
		prog, err := parser.Parse(src)
		if err != nil {
			t.Fatalf("parse %q: %v", src, err)
		}
		iseq, err := compiler.Compile(prog)
		if err != nil {
			t.Fatalf("compile %q: %v", src, err)
		}
		if _, err := vm.Run(iseq); err != nil {
			t.Fatalf("run %q: %v", src, err)
		}
		return strings.TrimRight(buf.String(), "\n")
	}

	cases := []struct{ src, want string }{
		// [] reads a frozen value; a missing name is nil.
		{`ENV.clear; ENV["A"]="1"; p ENV["A"]`, `"1"`},
		{`ENV.clear; ENV["A"]="1"; p ENV["A"].frozen?`, `true`},
		{`ENV.clear; p ENV["MISSING"]`, `nil`},
		// []= returns its value; store is a true alias.
		{`ENV.clear; p(ENV.send(:[]=, "A", "z"))`, `"z"`},
		{`ENV.clear; ENV.store("A","2"); p ENV["A"]`, `"2"`},
		{`p ENV.method(:store) == ENV.method(:[]=)`, `true`},
		// []= with nil deletes.
		{`ENV.clear; ENV["A"]="1"; ENV["A"]=nil; p ENV.key?("A")`, `false`},
		// EINVAL for an empty or '='-bearing name.
		{`ENV.clear; begin; ENV[""]="x"; rescue => e; p e.class; end`, `Errno::EINVAL`},
		{`ENV.clear; begin; ENV["a=b"]="x"; rescue => e; p e.class; end`, `Errno::EINVAL`},
		// A nil value on an invalid name does nothing (no error).
		{`ENV.clear; ENV["a=b"]=nil; p ENV.key?("a=b")`, `false`},
		// #to_str coercion of names and values; a non-coercible name is a TypeError.
		{`ENV.clear; k=Object.new; def k.to_str; "A"; end; ENV[k]="1"; p ENV["A"]`, `"1"`},
		{`ENV.clear; v=Object.new; def v.to_str; "1"; end; ENV["A"]=v; p ENV["A"]`, `"1"`},
		{`ENV.clear; begin; ENV[Object.new]; rescue => e; p e.message; end`,
			`"no implicit conversion of Object into String"`},

		// fetch: value, default, block, KeyError, and block+default warning.
		{`ENV.clear; ENV["A"]="1"; p ENV.fetch("A")`, `"1"`},
		{`ENV.clear; p ENV.fetch("X","d")`, `"d"`},
		// block + default: the block supersedes the default (and MRI warns).
		{`ENV.clear; ENV["A"]="1"; p ENV.fetch("A","d"){|k| "blk"}`, `"1"`},
		{`ENV.clear; p ENV.fetch("X","d"){|k| "blk:#{k}"}`, `"blk:X"`},
		{`ENV.clear; p ENV.fetch("X"){|k| "blk:#{k}"}`, `"blk:X"`},
		{`ENV.clear; begin; ENV.fetch("X"); rescue KeyError => e; p [e.message, e.key, e.receiver.equal?(ENV)]; end`,
			`["key not found: \"X\"", "X", true]`},

		// include? and its aliases; key/value lookups.
		{`ENV.clear; ENV["A"]="1"; p [ENV.include?("A"), ENV.member?("A"), ENV.has_key?("A"), ENV.key?("A")]`,
			`[true, true, true, true]`},
		{`ENV.clear; p ENV.include?("X")`, `false`},
		{`ENV.clear; ENV["A"]="1"; p ENV.key("1")`, `"A"`},
		{`ENV.clear; p ENV.key("1")`, `nil`},
		{`ENV.clear; ENV["A"]="1"; p [ENV.value?("1"), ENV.has_value?("2"), ENV.value?(Object.new)]`,
			`[true, false, nil]`},

		// delete with/without block.
		{`ENV.clear; ENV["A"]="1"; p ENV.delete("A")`, `"1"`},
		{`ENV.clear; p ENV.delete("X")`, `nil`},
		{`ENV.clear; p ENV.delete("X"){|k| "gone:#{k}"}`, `"gone:X"`},

		// keys/values/values_at/size/empty?.
		{`ENV.clear; ENV["A"]="1"; ENV["B"]="2"; p ENV.keys`, `["A", "B"]`},
		{`ENV.clear; ENV["A"]="1"; ENV["B"]="2"; p ENV.values`, `["1", "2"]`},
		{`ENV.clear; ENV["A"]="1"; ENV["B"]="2"; p ENV.values_at("B","X","A")`, `["2", nil, "1"]`},
		{`ENV.clear; ENV["A"]="1"; ENV["B"]="2"; p [ENV.size, ENV.length, ENV.empty?]`, `[2, 2, false]`},
		{`ENV.clear; p ENV.empty?`, `true`},

		// to_hash/to_h and to_h with a block (plus its error branches).
		{`ENV.clear; ENV["A"]="1"; p ENV.to_hash`, `{"A" => "1"}`},
		{`ENV.clear; ENV["A"]="1"; p ENV.to_h`, `{"A" => "1"}`},
		{`ENV.clear; ENV["A"]="1"; p(ENV.to_h{|k,v| [k.downcase, v]})`, `{"a" => "1"}`},
		{`ENV.clear; ENV["A"]="1"; begin; ENV.to_h{|k,v| "x"}; rescue TypeError => e; p e.message; end`,
			`"wrong element type String (expected array)"`},
		{`ENV.clear; ENV["A"]="1"; begin; ENV.to_h{|k,v| [1,2,3]}; rescue ArgumentError => e; p e.message; end`,
			`"element has wrong array length (expected 2, was 3)"`},
		// to_h block result coerced to a pair via #to_ary.
		{`ENV.clear; ENV["A"]="1"; o=Object.new; def o.to_ary; ["x","y"]; end; p(ENV.to_h{|k,v| o})`,
			`{"x" => "y"}`},
		// value? / rassoc coerce the argument with #to_str.
		{`ENV.clear; ENV["A"]="1"; v=Object.new; def v.to_str; "1"; end; p ENV.value?(v)`, `true`},
		{`ENV.clear; ENV["A"]="1"; v=Object.new; def v.to_str; "1"; end; p ENV.rassoc(v)`, `["A", "1"]`},

		// inspect (MRI env_inspect spacing, no spaces around =>) and to_s.
		{`ENV.clear; ENV["A"]="1"; ENV["B"]="2"; p ENV.inspect`, `"{\"A\"=>\"1\", \"B\"=>\"2\"}"`},
		{`ENV.clear; p ENV.inspect`, `"{}"`},
		{`p ENV.to_s`, `"ENV"`},
		{`p ENV.rehash`, `nil`},

		// clone/dup are refused.
		{`begin; ENV.dup; rescue TypeError => e; p e.message; end`,
			`"Cannot dup ENV, use ENV.to_h to get a copy of ENV as a hash"`},
		{`begin; ENV.clone; rescue TypeError => e; p e.message; end`,
			`"Cannot clone ENV, use ENV.to_h to get a copy of ENV as a hash"`},
		{`begin; ENV.clone(freeze: 1); rescue ArgumentError => e; p e.class; end`, `ArgumentError`},
		{`begin; ENV.clone(foo: 1); rescue ArgumentError => e; p e.class; end`, `ArgumentError`},
		{`begin; ENV.clone(freeze: true); rescue TypeError => e; p e.class; end`, `TypeError`},
	}
	for _, c := range cases {
		if got := run(c.src); got != c.want {
			t.Errorf("%s\n  got  %s\n  want %s", c.src, got, c.want)
		}
	}
}

// TestENVWave16Iteration covers the block/enumerator iteration and filter methods
// and the collection helpers (slice/except/assoc/rassoc/merge!/replace/shift/…).
func TestENVWave16Iteration(t *testing.T) {
	vm, buf := seamedENV(t)
	run := func(src string) string {
		buf.Reset()
		prog, err := parser.Parse(src)
		if err != nil {
			t.Fatalf("parse %q: %v", src, err)
		}
		iseq, err := compiler.Compile(prog)
		if err != nil {
			t.Fatalf("compile %q: %v", src, err)
		}
		if _, err := vm.Run(iseq); err != nil {
			t.Fatalf("run %q: %v", src, err)
		}
		return strings.TrimRight(buf.String(), "\n")
	}
	seed := `ENV.clear; ENV["A"]="1"; ENV["B"]="2"; `
	cases := []struct{ src, want string }{
		// each_pair (block returns self) and its true alias each.
		{seed + `acc=[]; r=ENV.each_pair{|k,v| acc<<"#{k}=#{v}"}; p [acc, r.equal?(ENV)]`,
			`[["A=1", "B=2"], true]`},
		{`p ENV.method(:each) == ENV.method(:each_pair)`, `true`},
		{seed + `e=ENV.each_pair; p [e.class, e.size]`, `[Enumerator, 2]`},
		{seed + `acc=[]; ENV.each_pair.each{|k,v| acc<<k}; p acc`, `["A", "B"]`},
		// each_key / each_value, block and enumerator.
		{seed + `acc=[]; ENV.each_key{|k| acc<<k}; p acc`, `["A", "B"]`},
		{seed + `p ENV.each_key.to_a == ENV.keys`, `true`},
		{seed + `acc=[]; ENV.each_value{|v| acc<<v}; p acc`, `["1", "2"]`},
		{seed + `p ENV.each_value.to_a == ENV.values`, `true`},
		// select / filter and their in-place forms.
		{seed + `p(ENV.select{|k,v| k=="A"})`, `{"A" => "1"}`},
		{seed + `p ENV.select.class`, `Enumerator`},
		{`p ENV.method(:filter) == ENV.method(:select)`, `true`},
		{seed + `r=ENV.select!{|k,v| k=="A"}; p [ENV.keys, r.equal?(ENV)]`, `[["A"], true]`},
		{seed + `p ENV.select!{|k,v| true}`, `nil`},
		{seed + `p ENV.select!.class`, `Enumerator`},
		{`p ENV.method(:filter!) == ENV.method(:select!)`, `true`},
		// reject / reject!.
		{seed + `p(ENV.reject{|k,v| k=="A"})`, `{"B" => "2"}`},
		{seed + `p ENV.reject.class`, `Enumerator`},
		{seed + `r=ENV.reject!{|k,v| k=="A"}; p [ENV.keys, r.equal?(ENV)]`, `[["B"], true]`},
		{seed + `p ENV.reject!{|k,v| false}`, `nil`},
		{seed + `p ENV.reject!.class`, `Enumerator`},
		// keep_if / delete_if always return self.
		{seed + `r=ENV.keep_if{|k,v| k=="A"}; p [ENV.keys, r.equal?(ENV)]`, `[["A"], true]`},
		{seed + `r=ENV.keep_if{|k,v| true}; p r.equal?(ENV)`, `true`},
		{seed + `p ENV.keep_if.class`, `Enumerator`},
		{seed + `r=ENV.delete_if{|k,v| k=="A"}; p [ENV.keys, r.equal?(ENV)]`, `[["B"], true]`},
		{seed + `r=ENV.delete_if{|k,v| false}; p r.equal?(ENV)`, `true`},
		{seed + `p ENV.delete_if.class`, `Enumerator`},
		// slice keeps the original argument objects as keys; except removes names.
		{seed + `p ENV.slice("A","X","B")`, `{"A" => "1", "B" => "2"}`},
		{seed + `p ENV.except("A")`, `{"B" => "2"}`},
		// assoc / rassoc.
		{seed + `p ENV.assoc("A")`, `["A", "1"]`},
		{seed + `p ENV.assoc("X")`, `nil`},
		{seed + `p ENV.rassoc("2")`, `["B", "2"]`},
		{seed + `p ENV.rassoc("9")`, `nil`},
		{seed + `p ENV.rassoc(Object.new)`, `nil`},
		// merge! / update (alias): multiple hashes and the 3-arg block.
		{seed + `r=ENV.merge!({"C"=>"3"},{"D"=>"4"}); p [ENV.keys, r.equal?(ENV)]`,
			`[["A", "B", "C", "D"], true]`},
		{seed + `ENV.merge!("A"=>"9"){|k,o,n| o+n}; p ENV["A"]`, `"19"`},
		{seed + `ENV.merge!("Z"=>"5"){|k,o,n| "unused"}; p ENV["Z"]`, `"5"`},
		{`p ENV.method(:update) == ENV.method(:merge!)`, `true`},
		{`p ENV.method(:has_value?) == ENV.method(:value?)`, `true`},
		{`p ENV.method(:length) == ENV.method(:size)`, `true`},
		// replace clears prior names not in the new hash.
		{seed + `r=ENV.replace("C"=>"3"); p [ENV.to_hash, r.equal?(ENV)]`, `[{"C" => "3"}, true]`},
		// shift removes and returns the first pair; nil when empty.
		{seed + `p ENV.shift`, `["A", "1"]`},
		{`ENV.clear; p ENV.shift`, `nil`},
		// invert / to_a.
		{seed + `p ENV.invert`, `{"1" => "A", "2" => "B"}`},
		{seed + `p ENV.to_a`, `[["A", "1"], ["B", "2"]]`},
	}
	for _, c := range cases {
		if got := run(c.src); got != c.want {
			t.Errorf("%s\n  got  %s\n  want %s", c.src, got, c.want)
		}
	}
}
