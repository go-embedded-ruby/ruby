// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

// Coverage for the NEW branches the Puppet-boot stdlib batch added to several
// existing source files, driving each remaining statement back to 100%:
//
//   - format.go       formatString named refs (%<name>spec / %{name}),
//                     namedFormatHash, parseConversion, and the too-few-arguments
//                     / missing-hash / missing-key error paths.
//   - struct.go       setupStruct keyword_init block-cref lexParent path,
//                     newStructClass []= / each_pair / class-level members,
//                     structSetMember (found + NameError).
//   - digest.go       digestNewByName's unknown-name (nil) branch.
//   - builtins.go     String#scrub (scrubUTF8) and Class#allocate (bootstrap).
//   - file.go         File.join nested-Array flattening, absolute_path,
//                     absolute_path?.
//   - regexp.go       Regexp.escape/quote and Regexp.union.
//   - object_model.go classOf for an OpenSSL::Digest instance.
//   - kernel_introspect.go RbConfig keys (+ the EXEEXT seam) and Kernel
//                     introspection (__method__, __FILE__, __dir__, at_exit).
//
// Output values are asserted against MRI 4.0.5 (ruby -e); error paths assert the
// engine's documented Ruby error class (captured via begin/rescue inside the
// snippet so the test is Windows-portable and needs no Go-level error plumbing).
// runSrc (aot_dispatch_test.go) runs a snippet on a fresh VM and returns its
// stdout with the trailing newline trimmed.

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/compiler"
	"github.com/go-ruby-parser/parser"
)

// --- format.go ----------------------------------------------------------------

// TestPBFormatNamedReferences drives the %<name>spec and %{name} named-reference
// paths of formatString (and the namedFormatHash / parseConversion helpers they
// call) across the conversion specifiers, matching MRI.
func TestPBFormatNamedReferences(t *testing.T) {
	cases := []struct{ src, want string }{
		// %{name}: to_s insert with no further formatting.
		{`print format("%<x>d and %{y}", x: 5, y: "hi")`, "5 and hi"},
		// %<name>spec with flags/width/precision on a float.
		{`print format("%<v>05.2f", v: 3.14159)`, "03.14"},
		// Symbol value through %s.
		{`print format("%<n>s", n: :sym)`, "sym"},
		// %c via a named ref (toFormatChar Integer branch).
		{`print format("%<c>c", c: 65)`, "A"},
		// Octal / binary / hex named refs (parseConversion verb dispatch).
		{`print format("%<o>o %<b>b %<X>X", o: 8, b: 5, X: 255)`, "10 101 FF"},
	}
	for _, tc := range cases {
		if got := runSrc(t, tc.src); got != tc.want {
			t.Errorf("%s => %q, want %q", tc.src, got, tc.want)
		}
	}
}

// TestPBFormatCharFromString covers toFormatChar's String branch: %c takes the
// first character of a String, and the empty string yields "" (as in MRI).
func TestPBFormatCharFromString(t *testing.T) {
	cases := []struct{ src, want string }{
		{`print format("%c", "hello")`, "h"},
		{`print format("%c", "")`, ""},
		{`print format("%<ch>c", ch: "Z")`, "Z"},
	}
	for _, tc := range cases {
		if got := runSrc(t, tc.src); got != tc.want {
			t.Errorf("%s => %q, want %q", tc.src, got, tc.want)
		}
	}
}

// TestPBFormatErrorPaths covers formatString's error branches: too-few arguments
// (the next closure), a named ref with no hash argument or an absent key, and the
// malformed %{ / %< / dangling-conversion cases.
func TestPBFormatErrorPaths(t *testing.T) {
	cases := []struct{ src, want string }{
		// next(): plain conversion with no argument.
		{`p(begin; format("%d"); rescue => e; e.class.name; end)`, `"ArgumentError"`},
		// named(): %<name> / %{name} with a non-hash sole argument -> "one hash required".
		{`p(begin; format("%<x>d", 1); rescue => e; e.class.name; end)`, `"ArgumentError"`},
		{`p(begin; format("%{x}", 1); rescue => e; e.class.name; end)`, `"ArgumentError"`},
		// named(): key absent from the hash -> KeyError.
		{`p(begin; format("%<x>d", a: 1); rescue => e; e.class.name; end)`, `"KeyError"`},
		{`p(begin; format("%{x}", a: 1); rescue => e; e.class.name; end)`, `"KeyError"`},
		// Malformed %{ (no closing brace) and %< (no closing angle).
		{`p(begin; format("%{x"); rescue => e; e.class.name; end)`, `"ArgumentError"`},
		{`p(begin; format("%<x"); rescue => e; e.class.name; end)`, `"ArgumentError"`},
		// parseConversion running off the end (no verb after %<name>).
		{`p(begin; format("%<x>", x: 1); rescue => e; e.class.name; end)`, `"ArgumentError"`},
	}
	for _, tc := range cases {
		if got := runSrc(t, tc.src); got != tc.want {
			t.Errorf("%s => %q, want %q", tc.src, got, tc.want)
		}
	}
}

// --- struct.go ----------------------------------------------------------------

// TestPBStructIndexAssign covers Struct#[]= for an Integer index (including a
// negative index and the IndexError on overflow), Symbol and String member names
// (via structSetMember), and the TypeError default branch.
func TestPBStructIndexAssign(t *testing.T) {
	cases := []struct{ src, want string }{
		{`S = Struct.new(:a, :b, :c); s = S.new(1, 2, 3); s[0] = 10; s[-1] = 30; p [s.a, s.c]`, "[10, 30]"},
		{`S = Struct.new(:a, :b); s = S.new(1, 2); s[:a] = 7; s["b"] = 9; p [s.a, s.b]`, "[7, 9]"},
		// IndexError when the integer index is out of range.
		{`S = Struct.new(:a); s = S.new(1); p(begin; s[5] = 9; rescue => e; e.class.name; end)`, `"IndexError"`},
		// NameError when a Symbol / String name is not a member (structSetMember miss).
		{`S = Struct.new(:a); s = S.new(1); p(begin; s[:nope] = 9; rescue => e; e.class.name; end)`, `"NameError"`},
		{`S = Struct.new(:a); s = S.new(1); p(begin; s["nope"] = 9; rescue => e; e.class.name; end)`, `"NameError"`},
		// TypeError default branch: a non-Integer/Symbol/String index.
		{`S = Struct.new(:a); s = S.new(1); p(begin; s[1.0] = 9; rescue => e; e.class.name; end)`, `"TypeError"`},
	}
	for _, tc := range cases {
		if got := runSrc(t, tc.src); got != tc.want {
			t.Errorf("%s => %q, want %q", tc.src, got, tc.want)
		}
	}
}

// TestPBStructEachPair covers Struct#each_pair (yield order) and its no-block
// LocalJumpError branch.
func TestPBStructEachPair(t *testing.T) {
	if got := runSrc(t, `S = Struct.new(:a, :b); s = S.new(1, 2); s.each_pair { |k, v| print "#{k}=#{v} " }`); got != "a=1 b=2 " {
		t.Errorf("each_pair => %q", got)
	}
	if got := runSrc(t, `S = Struct.new(:a); p(begin; S.new(1).each_pair; rescue => err; err.class.name; end)`); got != `"LocalJumpError"` {
		t.Errorf("each_pair no-block => %q", got)
	}
}

// TestPBStructClassMembers covers the class-level members reader installed on the
// Struct subclass itself.
func TestPBStructClassMembers(t *testing.T) {
	if got := runSrc(t, `S = Struct.new(:a, :b, :c); p S.members`); got != "[:a, :b, :c]" {
		t.Errorf("class members => %q", got)
	}
}

// TestPBStructBlockLexParent covers setupStruct's block path where the block's
// captured lexical scope is a module (blk.cref != Object): the body must resolve
// a sibling constant via the recorded lexParent, matching MRI.
func TestPBStructBlockLexParent(t *testing.T) {
	src := `
module Outer
  Helper = 42
  T = Struct.new(:x) do
    def get_helper; Helper; end
  end
end
p Outer::T.new(1).get_helper`
	if got := runSrc(t, src); got != "42" {
		t.Errorf("struct block lexParent => %q, want %q", got, "42")
	}
}

// --- digest.go / object_model.go / openssl ------------------------------------

// TestPBDigestNewByNameUnknown covers digestNewByName's nil (unknown algorithm)
// branch, surfaced as the RuntimeError OpenSSL::Digest.new raises, and the
// classOf branch for a live OpenSSL::Digest instance.
func TestPBDigestNewByNameUnknown(t *testing.T) {
	if got := runSrc(t, `require "openssl"; p(begin; OpenSSL::Digest.new("BOGUS"); rescue => e; e.class.name; end)`); got != `"RuntimeError"` {
		t.Errorf("unknown digest => %q", got)
	}
	// classOf(*opensslDigest) -> the instance's own class.
	if got := runSrc(t, `require "openssl"; puts OpenSSL::Digest.new("SHA256").class`); got != "OpenSSL::Digest" {
		t.Errorf("opensslDigest classOf => %q", got)
	}
}

// --- builtins.go --------------------------------------------------------------

// TestPBStringScrub covers scrubUTF8: a valid string is returned unchanged (the
// fast path), and invalid bytes are replaced — by the default replacement char
// and by an explicit replacement string.
func TestPBStringScrub(t *testing.T) {
	// Valid UTF-8: returned verbatim.
	if got := runSrc(t, `puts "héllo".scrub == "héllo"`); got != "true" {
		t.Errorf("scrub valid => %q", got)
	}
	// Invalid byte (0xFF) collapses to the default U+FFFD: the 3-byte replacement
	// turns a 3-byte input into a 5-byte output.
	if got := runSrc(t, `s = [97, 255, 98].pack("C*"); p s.scrub.bytes`); got != "[97, 239, 191, 189, 98]" {
		t.Errorf("scrub default repl => %q", got)
	}
	// Explicit replacement string.
	if got := runSrc(t, `s = [97, 255, 98].pack("C*"); puts s.scrub("?")`); got != "a?b" {
		t.Errorf("scrub custom repl => %q", got)
	}
}

// TestPBClassAllocate covers Class#allocate (a bootstrap branch): it returns an
// uninitialized instance of the receiver with no initialize call.
func TestPBClassAllocate(t *testing.T) {
	src := `
class Foo
  def initialize; @set = true; end
  def set?; @set; end
end
o = Foo.allocate
p [o.class == Foo, o.set?]`
	if got := runSrc(t, src); got != "[true, nil]" {
		t.Errorf("allocate => %q, want %q", got, "[true, nil]")
	}
}

// --- file.go ------------------------------------------------------------------

// TestPBFileNewBranches covers File.join's nested-Array flattening and the
// absolute_path / absolute_path? methods added in the batch.
func TestPBFileNewBranches(t *testing.T) {
	cases := []struct{ src, want string }{
		{`puts File.join(["a", "b"], "c")`, "a/b/c"},
		{`puts File.absolute_path("foo", "/base")`, "/base/foo"},
		{`puts File.absolute_path?("/abs")`, "true"},
		{`puts File.absolute_path?("rel")`, "false"},
	}
	for _, tc := range cases {
		if got := runSrc(t, tc.src); got != tc.want {
			t.Errorf("%s => %q, want %q", tc.src, got, tc.want)
		}
	}
}

// --- regexp.go ----------------------------------------------------------------

// TestPBRegexpNewBranches covers Regexp.escape/quote and Regexp.union (Array
// form, mixed Regexp/String members, and the no-argument never-match case).
func TestPBRegexpNewBranches(t *testing.T) {
	cases := []struct{ src, want string }{
		{`puts Regexp.escape("a.b*c")`, `a\.b\*c`},
		{`puts Regexp.quote("a.b*c")`, `a\.b\*c`},
		// String member is escaped, Regexp member contributes its source.
		{`puts Regexp.union("a.b", /cd/).source`, `a\.b|cd`},
		// Single Array argument is the pattern list.
		{`puts Regexp.union(["x", "y"]).source`, "x|y"},
		// No arguments -> a pattern that matches nothing.
		{`puts Regexp.union().source`, "(?!)"},
	}
	for _, tc := range cases {
		if got := runSrc(t, tc.src); got != tc.want {
			t.Errorf("%s => %q, want %q", tc.src, got, tc.want)
		}
	}
}

// --- kernel_introspect.go -----------------------------------------------------

// TestPBRbConfigKeys covers registerRbConfig: the CONFIG keys app code reads, and
// (via the rbconfigGOOS seam) both the Windows and non-Windows EXEEXT branches,
// which are otherwise OS-gated and unreachable on a single host.
func TestPBRbConfigKeys(t *testing.T) {
	src := `
require "rbconfig"
puts RbConfig::CONFIG["ruby_install_name"]
puts RbConfig::CONFIG["RUBY_INSTALL_NAME"]
puts RbConfig::CONFIG["bindir"]
puts RbConfig::CONFIG["ruby_version"]
puts RbConfig::CONFIG["rubylibdir"]`
	want := "ruby\nruby\n/usr/bin\n3.4.1\n/usr/lib/ruby"
	if got := runSrc(t, src); got != want {
		t.Errorf("RbConfig keys => %q, want %q", got, want)
	}

	// EXEEXT: drive both OS branches through the seam, asserting the value MRI's
	// RbConfig reports for each platform (".exe" on Windows, "" elsewhere).
	orig := rbconfigGOOS
	t.Cleanup(func() { rbconfigGOOS = orig })
	for _, c := range []struct{ goos, want string }{
		{"windows", ".exe"},
		{"linux", ""},
	} {
		rbconfigGOOS = func() string { return c.goos }
		if got := runSrc(t, `require "rbconfig"; puts RbConfig::CONFIG["EXEEXT"].inspect`); got != `"`+c.want+`"` {
			t.Errorf("EXEEXT on %s => %q, want %q", c.goos, got, `"`+c.want+`"`)
		}
	}
}

// TestPBKernelIntrospectionEdges covers the introspection branches: __method__
// inside a method (Symbol), at the top level (nil) and inside a block (nil),
// __dir__ for a file-less program (the f=="" -> nil branch, since runSrc sets no
// script path), at_exit returning the registered Proc, and at_exit's no-block
// LocalJumpError.
func TestPBKernelIntrospectionEdges(t *testing.T) {
	// __method__ returns the method name inside a def.
	if got := runSrc(t, `def foo; __method__; end; p foo`); got != ":foo" {
		t.Errorf("__method__ in def => %q", got)
	}
	// Top-level __method__ and __dir__ resolve to nil when there is no enclosing
	// method and no script file (the embedding/-e path).
	if got := runSrc(t, `p __method__; p __dir__`); got != "nil\nnil" {
		t.Errorf("top-level __method__/__dir__ => %q", got)
	}
	// __method__ inside a block is nil (block body, no method frame).
	if got := runSrc(t, `[1].each { p __method__ }`); got != "nil" {
		t.Errorf("__method__ in block => %q", got)
	}
	// at_exit with a block returns that block (a Proc); without a block raises.
	if got := runSrc(t, `r = at_exit { }; puts r.class`); got != "Proc" {
		t.Errorf("at_exit return => %q", got)
	}
	if got := runSrc(t, `p(begin; at_exit; rescue => e; e.class.name; end)`); got != `"LocalJumpError"` {
		t.Errorf("at_exit no-block => %q", got)
	}
}

// TestPBKernelFileWithScriptPath covers __FILE__ and __dir__'s non-empty
// branches: when the host records a script path (SetScriptPath, as the CLI
// does), __FILE__ returns it and __dir__ returns its directory. runSrc leaves the
// script path empty, so this drives the public embedding seam directly with a
// forward-slash path that is portable across hosts.
func TestPBKernelFileWithScriptPath(t *testing.T) {
	prog, err := parser.Parse(`puts __FILE__; puts __dir__`)
	if err != nil {
		t.Fatal(err)
	}
	iseq, err := compiler.Compile(prog)
	if err != nil {
		t.Fatal(err)
	}
	// Use an OS-native absolute path so SetScriptPath / filepath.Dir behave the
	// same way the test's expectation is computed, on Windows and POSIX alike.
	script := filepath.Join(t.TempDir(), "main.rb")
	var buf bytes.Buffer
	m := New(&buf)
	m.SetScriptPath(script)
	if _, err := m.Run(iseq); err != nil {
		t.Fatal(err)
	}
	if got, want := buf.String(), script+"\n"+filepath.Dir(script)+"\n"; got != want {
		t.Errorf("__FILE__/__dir__ => %q, want %q", got, want)
	}
}
