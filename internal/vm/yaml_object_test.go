// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import "testing"

// TestYAMLDumpObject covers Object#to_yaml / YAML.dump of an arbitrary object as
// a !ruby/object:Class mapping of its instance variables. Each expected document
// is the exact Psych.dump output in MRI (the instance variables here are in
// alphabetical order, which the deterministic emitter preserves).
func TestYAMLDumpObject(t *testing.T) {
	cases := []struct{ src, want string }{
		// A simple object via to_yaml.
		{`class Foo; def initialize; @a=1; @b="hi"; @c=:sym; end; end
puts Foo.new.to_yaml`, "--- !ruby/object:Foo\na: 1\nb: hi\nc: :sym\n"},
		// Via YAML.dump as well.
		{`class Foo2; def initialize; @a=1; end; end
puts YAML.dump(Foo2.new)`, "--- !ruby/object:Foo2\na: 1\n"},
		// An object with no instance variables: the inline "{}" form.
		{`class E; end
puts E.new.to_yaml`, "--- !ruby/object:E {}\n"},
		// A nested object value is indented two under its key.
		{`class Inner; def initialize; @v=1; end; end
class Outer; def initialize; @inner=Inner.new; end; end
puts Outer.new.to_yaml`, "--- !ruby/object:Outer\ninner: !ruby/object:Inner\n  v: 1\n"},
		// An object held in a hash value.
		{`class P; def initialize; @n=1; end; end
puts({"k" => P.new}.to_yaml)`, "---\nk: !ruby/object:P\n  n: 1\n"},
		// An object in a sequence.
		{`class Q; def initialize; @n=1; end; end
puts [Q.new].to_yaml`, "---\n- !ruby/object:Q\n  n: 1\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestYAMLDumpRange covers a Range (!ruby/range begin/end/excl).
func TestYAMLDumpRange(t *testing.T) {
	cases := []struct{ src, want string }{
		{`puts((1..5).to_yaml)`, "--- !ruby/range\nbegin: 1\nend: 5\nexcl: false\n"},
		{`puts((1...5).to_yaml)`, "--- !ruby/range\nbegin: 1\nend: 5\nexcl: true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestYAMLDumpSet covers a Set, which Psych writes as !ruby/object:Set with its
// backing "hash" ivar.
func TestYAMLDumpSet(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "set"; puts Set.new([1, 2]).to_yaml`, "--- !ruby/object:Set\nhash:\n  1: true\n  2: true\n"},
		{`require "set"; puts Set.new.to_yaml`, "--- !ruby/object:Set\nhash: {}\n"},
		// A Set held as a mapping value.
		{`require "set"; puts({"t" => Set.new([1])}.to_yaml)`, "---\nt: !ruby/object:Set\n  hash:\n    1: true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestYAMLDumpRegexpAndClass covers the inline tagged scalars: a Regexp and a
// Class / Module reference.
func TestYAMLDumpRegexpAndClass(t *testing.T) {
	cases := []struct{ src, want string }{
		{`puts(/ab/.to_yaml)`, "--- !ruby/regexp /ab/\n"},
		{`puts(/ab/i.to_yaml)`, "--- !ruby/regexp /ab/i\n"},
		{`puts({:k => String}.to_yaml)`, "---\n:k: !ruby/class 'String'\n"},
		{`puts({:k => Comparable}.to_yaml)`, "---\n:k: !ruby/module 'Comparable'\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestYAMLDumpComplexKeys covers Psych's explicit "?"/":" form for non-scalar
// mapping keys (objects, arrays, hashes) and the nil key ("! ”").
func TestYAMLDumpComplexKeys(t *testing.T) {
	cases := []struct{ src, want string }{
		{`class K; def initialize; @a=1; end; end
puts({K.new => 1}.to_yaml)`, "---\n? !ruby/object:K\n  a: 1\n: 1\n"},
		{`puts({[1, 2] => 1}.to_yaml)`, "---\n? - 1\n  - 2\n: 1\n"},
		{`puts({{"a" => 1} => 2}.to_yaml)`, "---\n? a: 1\n: 2\n"},
		{`puts({nil => 1}.to_yaml)`, "---\n! '': 1\n"},
		// A complex key appearing as a sequence element's inline-first hash entry.
		{`puts [{[1] => 2}].to_yaml`, "---\n- ? - 1\n  : 2\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestYAMLDumpSharedAnchor covers a shared / repeated reference written once with
// "&N" and aliased "*N" thereafter, and a self-referential cycle (which must
// terminate).
func TestYAMLDumpSharedAnchor(t *testing.T) {
	cases := []struct{ src, want string }{
		// The same object twice in an array.
		{`class S; def initialize; @n=7; end; end
b = S.new
puts [b, b].to_yaml`, "---\n- &1 !ruby/object:S\n  n: 7\n- *1\n"},
		// A shared array.
		{`a = [1, 2]
puts [a, a].to_yaml`, "---\n- &1\n  - 1\n  - 2\n- *1\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestYAMLDumpCycleTerminates covers that a self-referential object graph dumps
// without looping forever (the value is emitted once, then aliased).
func TestYAMLDumpCycleTerminates(t *testing.T) {
	src := `class C; attr_accessor :self_ref; end
c = C.new
c.self_ref = c
y = c.to_yaml
p y.include?("&1")
p y.include?("*1")`
	want := "true\ntrue\n"
	if got := eval(t, src); got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}

// TestYAMLObjectRoundTrip covers dump-then-load of an object graph returning an
// equivalent structure (same class, same ivars, shared refs preserved).
func TestYAMLObjectRoundTrip(t *testing.T) {
	src := `class Person; attr_reader :name, :age; end
p0 = Person.new
p0.instance_variable_set(:@name, "Ada")
p0.instance_variable_set(:@age, 36)
doc = [p0, p0].to_yaml
r = YAML.load(doc)
p r[0].class
p r[0].name
p r[0].age
p r[0].equal?(r[1])`
	want := "Person\n\"Ada\"\n36\ntrue\n"
	if got := eval(t, src); got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}
