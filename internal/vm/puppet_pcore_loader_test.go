package vm_test

import "testing"

// TestHashObjectKeySemantics covers the Hash key path that made Puppet's loader
// registry (keyed by a TypedName with a custom #hash/#eql?) miss in rbgo: a
// non-identical key that is #eql? with the same #hash must find the stored entry,
// and Array/Hash keys must compare by content. Asserted against MRI 4.0.5.
func TestHashObjectKeySemantics(t *testing.T) {
	cases := []struct{ src, want string }{
		// User object with custom #hash/#eql? — fresh-but-eql? key hits.
		{`
class K
  attr_reader :name, :hash
  def initialize(n); @name=n; @hash=n.hash; freeze; end
  def ==(o); o.class==self.class && o.name==@name; end
  alias eql? ==
end
h={}; h[K.new("annotation")]="V"
p h[K.new("annotation")]
p h.key?(K.new("annotation"))
p h[K.new("other")]
`, "\"V\"\ntrue\nnil\n"},
		// Array key by content.
		{`h={}; h[[1,2]]="a"; p h[[1,2]]`, "\"a\"\n"},
		// Hash key by content (exercises valKey on the nested value).
		{`h={}; h[{x: [1,2]}]="b"; p h[{x: [1,2]}]`, "\"b\"\n"},
		// Bignum #hash result (hashAsInt Bignum branch).
		{`
class Big; def hash; 10**40; end; def eql?(o); o.is_a?(Big); end; end
h={}; h[Big.new]="big"; p h[Big.new]
`, "\"big\"\n"},
		// A user subclass of String / Array used as a key unwraps to the value's
		// content (the KeyUnwrapper path).
		{`
class MyStr < String; end
h={}; h[MyStr.new("k")]=1; p h["k"]; p h[MyStr.new("k")]
`, "1\n1\n"},
		{`
class MyArr < Array; end
h={}; h[MyArr.new([1,2])]="v"; p h[[1,2]]
`, "\"v\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestHashCustomKeyCollisionsAndDelete covers reuseBucket: distinct (not #eql?)
// keys that share a #hash stay separate, an #eql? re-insert replaces, and delete
// removes by value — matching MRI's open-addressed table.
func TestHashCustomKeyCollisionsAndDelete(t *testing.T) {
	src := `
class K
  attr_reader :name
  def initialize(n); @name=n; end
  def hash; 42; end                 # everything collides on hash
  def eql?(o); o.is_a?(K) && o.name==@name; end
  def ==(o); eql?(o); end
end
h={}
h[K.new("a")]=1
h[K.new("b")]=2
h[K.new("a")]=3                     # replace, not add
p h.size
p h[K.new("a")]
p h[K.new("b")]
h.delete(K.new("a"))
p h.size
p h[K.new("a")]
p h.keys.map(&:name)
`
	want := "2\n3\n2\n1\nnil\n[\"b\"]\n"
	if got := eval(t, src); got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}

// TestHashNonIntegerHashRaises matches MRI: a key whose #hash returns a non-Integer
// raises TypeError when used as a Hash key.
func TestHashNonIntegerHashRaises(t *testing.T) {
	src := `
class N; def hash; "notint"; end; def eql?(o); equal?(o); end; end
begin
  h={}; h[N.new]="x"
rescue TypeError => e
  puts e.message
end
`
	want := "no implicit conversion of String into Integer\n"
	if got := eval(t, src); got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}

// TestHashClear covers Hash#clear, including that a custom-key hash is fully reset.
func TestHashClear(t *testing.T) {
	cases := []struct{ src, want string }{
		{`h={a:1,b:2}; r=h.clear; p r; p h.size`, "{}\n0\n"},
		{`
class K
  attr_reader :name, :hash
  def initialize(n); @name=n; @hash=n.hash; freeze; end
  def eql?(o); o.is_a?(K) && o.name==@name; end
end
h={}; h[K.new("x")]=1; h.clear; p h.size; p h[K.new("x")]
`, "0\nnil\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestHashMember covers the Hash#member? alias for #key?.
func TestHashMember(t *testing.T) {
	if got := eval(t, `h={a:1}; p h.member?(:a); p h.member?(:b)`); got != "true\nfalse\n" {
		t.Errorf("got=%q", got)
	}
}

// TestCatchThrowRestoresStacks covers the per-frame tracking-stack restore on a
// matching throw: many deep throws must not leak frames, and __FILE__ stays the
// program's file rather than a leaked deep frame. (Without the fix, racc's
// catch+throw leaked ~6 fileStack entries per type parse, corrupting __FILE__ and
// breaking a later require_relative during Puppet's parse.)
func TestCatchThrowRestoresStacks(t *testing.T) {
	src := `
def deep(n)
  n == 0 ? throw(:done, "v") : deep(n-1)
end
before = __FILE__
50.times { catch(:done) { deep(8); "no" } }
p __FILE__ == before
p(catch(:done) { deep(3); "no" })
`
	if got := eval(t, src); got != "true\n\"v\"\n" {
		t.Errorf("got=%q", got)
	}
}

// TestCatchThrowNonMatchingRepropagates covers the non-matching-tag branch of catch
// (the throw passes through an inner catch to the outer one).
func TestCatchThrowNonMatchingRepropagates(t *testing.T) {
	src := `
r = catch(:outer) do
  catch(:inner) do
    throw :outer, "hit"
  end
  "inner-returned"
end
p r
`
	if got := eval(t, src); got != "\"hit\"\n" {
		t.Errorf("got=%q", got)
	}
}

// TestRegexpCaseEqualSetsLastMatch covers Regexp#=== recording $~ (so a case/when
// over a string makes Regexp.last_match / $1 work in the taken branch) and the
// non-string operand clearing it — the Trollop :long derivation depended on this.
func TestRegexpCaseEqualSetsLastMatch(t *testing.T) {
	cases := []struct{ src, want string }{
		{`
r = case "--version"
    when /^--([^-].*)$/ then Regexp.last_match(1)
    when /^[^-]/ then "bare"
    else "else"
    end
p r
`, "\"version\"\n"},
		{`/(\d+)/ === "abc42"; p $1`, "\"42\"\n"},
		{`/(\d+)/ === "abc42"; p($~ ? $~[0] : nil); p(/x/ === 99); p $~`, "\"42\"\nfalse\nnil\n"},
		{`p(/x/ === "no"); p $~`, "false\nnil\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestRegexpLastMatch covers Regexp.last_match with and without an argument, and
// when there has been no match.
func TestRegexpLastMatch(t *testing.T) {
	cases := []struct{ src, want string }{
		{`"foo123" =~ /(\d+)/; p Regexp.last_match.class; p Regexp.last_match(1)`, "MatchData\n\"123\"\n"},
		{`"abc" =~ /z/; p Regexp.last_match`, "nil\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestStringScannerIndex covers StringScanner#[]: the whole match, a numbered group,
// a getch-then-[0], and out-of-range / no-match returning nil.
func TestStringScannerIndex(t *testing.T) {
	src := `
require "strscan"
s = StringScanner.new("foo bar")
s.scan(/(\w+)(\s*)/)
p s[0]; p s[1]; p s[2]; p s[5]
s2 = StringScanner.new("xy")
s2.getch
p s2[0]; p s2[1]
s2.terminate
p s2[0]
s3 = StringScanner.new("zz")
p s3.scan(/q/)
p s3[1]
`
	want := "\"foo \"\n\"foo\"\n\" \"\nnil\n\"x\"\nnil\nnil\nnil\nnil\n"
	if got := eval(t, src); got != want {
		t.Errorf("got=%q\nwant=%q", got, want)
	}
}

// TestStringScannerScanUntilIndex covers scan_until storing the match for #[].
func TestStringScannerScanUntilIndex(t *testing.T) {
	src := `
require "strscan"
s = StringScanner.new("aXbY")
p s.scan_until(/(X)/)
p s[0]; p s[1]
`
	if got := eval(t, src); got != "\"aX\"\n\"X\"\n\"X\"\n" {
		t.Errorf("got=%q", got)
	}
}

// TestSingletonMethodBindCall covers the Trollop cloaker trick: an UnboundMethod
// taken from a per-object singleton class re-bound (bind_call) onto the same object.
func TestSingletonMethodBindCall(t *testing.T) {
	src := `
class Foo
  def make(&b)
    (class << self; self; end).class_eval do
      define_method(:cloaker_, &b)
      m = instance_method(:cloaker_)
      remove_method(:cloaker_)
      m
    end
  end
end
f = Foo.new
m = f.make { |x| "got #{x} in #{self.class}" }
p m.bind_call(f, 7)
`
	if got := eval(t, src); got != "\"got 7 in Foo\"\n" {
		t.Errorf("got=%q", got)
	}
}

// TestUnboundMethodBindRejectsIncompatible covers the checkBindable failure path:
// an UnboundMethod from one class cannot bind to an unrelated receiver.
func TestUnboundMethodBindRejectsIncompatible(t *testing.T) {
	src := `
class A; def foo; "a"; end; end
class B; end
m = A.instance_method(:foo)
begin
  m.bind(B.new)
rescue TypeError => e
  puts e.message
end
`
	want := "bind argument must be an instance of A\n"
	if got := eval(t, src); got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}
