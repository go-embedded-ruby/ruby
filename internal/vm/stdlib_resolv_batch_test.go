// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// This file covers the "puppet-resolv-batch" round: Symbol#intern, the Resolv /
// optparse / ripper loadable stdlib, the SystemExit and friends exception
// classes, the Singleton prelude module, Kernel#load, $LOADED_FEATURES,
// File.mtime / Pathname path coercion, and the singleton-class `include` method
// lookup fix. Behaviour is asserted against MRI Ruby 4.0.5.

// TestSymbolIntern covers Symbol#intern, MRI's alias of Symbol#to_sym (returns
// self), which Puppet relies on so :sym.intern does not fall through to a
// user-defined Object#intern.
func TestSymbolIntern(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p :foo.intern`, ":foo\n"},
		{`p :foo.intern == :foo`, "true\n"},
		{`p :foo.intern.equal?(:foo)`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestResolvRequire covers require "resolv" returning true then false.
func TestResolvRequire(t *testing.T) {
	if got := eval(t, `p require "resolv"`); got != "true\n" {
		t.Errorf("first require got %q", got)
	}
	if got := eval(t, `require "resolv"; p require "resolv"`); got != "false\n" {
		t.Errorf("second require got %q", got)
	}
}

// TestResolvIPv4 covers Resolv::IPv4: create/new, to_s/to_str, inspect, ==, the
// Regex constant, and the ArgumentError raised for a malformed address.
func TestResolvIPv4(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "resolv"; p Resolv::IPv4.create("1.2.3.4").to_s`, "\"1.2.3.4\"\n"},
		{`require "resolv"; p Resolv::IPv4.new("10.0.0.1").to_str`, "\"10.0.0.1\"\n"},
		{`require "resolv"; p Resolv::IPv4.create("1.2.3.4").inspect`, "\"#<Resolv::IPv4 1.2.3.4>\"\n"},
		{`require "resolv"; a=Resolv::IPv4.create("1.2.3.4"); b=Resolv::IPv4.create("1.2.3.4"); p(a==b)`, "true\n"},
		{`require "resolv"; a=Resolv::IPv4.create("1.2.3.4"); b=Resolv::IPv4.create("4.3.2.1"); p(a==b)`, "false\n"},
		{`require "resolv"; p(Resolv::IPv4.create("1.2.3.4") == "1.2.3.4")`, "false\n"},
		{`require "resolv"; p(Resolv::IPv4::Regex.match?("192.168.0.1"))`, "true\n"},
		{`require "resolv"; p(Resolv::IPv4::Regex.match?("not-an-ip"))`, "false\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	// Malformed input raises ArgumentError.
	if class, _ := evalErr(t, `require "resolv"; Resolv::IPv4.create("999.1")`); class != "ArgumentError" {
		t.Errorf("bad IPv4 raised %q, want ArgumentError", class)
	}
	// An IPv6 string is not a valid IPv4 address.
	if class, _ := evalErr(t, `require "resolv"; Resolv::IPv4.create("::1")`); class != "ArgumentError" {
		t.Errorf("ipv6-as-ipv4 raised %q, want ArgumentError", class)
	}
}

// TestResolvIPv6 covers Resolv::IPv6: create/new, to_s/to_str, inspect, ==, the
// Regex constant, the zone-id form, and the ArgumentError for bad input.
func TestResolvIPv6(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "resolv"; p Resolv::IPv6.create("::1").to_s`, "\"::1\"\n"},
		{`require "resolv"; p Resolv::IPv6.new("fe80::1").to_str.start_with?("fe80")`, "true\n"},
		{`require "resolv"; p Resolv::IPv6.create("::1").inspect`, "\"#<Resolv::IPv6 ::1>\"\n"},
		{`require "resolv"; a=Resolv::IPv6.create("::1"); b=Resolv::IPv6.create("::1"); p(a==b)`, "true\n"},
		{`require "resolv"; a=Resolv::IPv6.create("::1"); b=Resolv::IPv6.create("::2"); p(a==b)`, "false\n"},
		{`require "resolv"; p(Resolv::IPv6.create("::1") == "::1")`, "false\n"},
		{`require "resolv"; p(Resolv::IPv6::Regex.match?("::1"))`, "true\n"},
		{`require "resolv"; p(Resolv::IPv6::Regex.match?("zzz"))`, "false\n"},
		// Zone id is preserved through create.
		{`require "resolv"; p Resolv::IPv6.create("fe80::1%eth0").to_s.end_with?("%eth0")`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	if class, _ := evalErr(t, `require "resolv"; Resolv::IPv6.create("not-ipv6")`); class != "ArgumentError" {
		t.Errorf("bad IPv6 raised %q, want ArgumentError", class)
	}
	// An IPv4 string is not a valid IPv6 address.
	if class, _ := evalErr(t, `require "resolv"; Resolv::IPv6.create("1.2.3.4")`); class != "ArgumentError" {
		t.Errorf("ipv4-as-ipv6 raised %q, want ArgumentError", class)
	}
}

// TestResolvModuleMethods covers Resolv.getaddress / getaddresses (real for a
// literal IP), the ResolvError for a hostname, and the NotImplementedError for
// the socket-backed helpers.
func TestResolvModuleMethods(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "resolv"; p Resolv.getaddress("127.0.0.1")`, "\"127.0.0.1\"\n"},
		{`require "resolv"; p Resolv.getaddress("::1")`, "\"::1\"\n"},
		{`require "resolv"; p Resolv.getaddresses("127.0.0.1")`, "[\"127.0.0.1\"]\n"},
		{`require "resolv"; p Resolv.getaddresses("example.invalid")`, "[]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	if class, _ := evalErr(t, `require "resolv"; Resolv.getaddress("example.invalid")`); class != "Resolv::ResolvError" {
		t.Errorf("getaddress(name) raised %q, want Resolv::ResolvError", class)
	}
	for _, m := range []string{"getname", "getnames", "each_address", "each_name"} {
		if class, _ := evalErr(t, `require "resolv"; Resolv.`+m+`("x")`); class != "NotImplementedError" {
			t.Errorf("Resolv.%s raised %q, want NotImplementedError", m, class)
		}
	}
}

// TestResolvDNS covers Resolv::DNS (constructable, query methods raise), the
// Resource constant tree, and Resolv::DNS::Name and Resolv::Hosts.
func TestResolvDNS(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "resolv"; p Resolv::DNS.new.class`, "Resolv::DNS\n"},
		{`require "resolv"; p Resolv::DNS.open.class`, "Resolv::DNS\n"},
		{`require "resolv"; p Resolv::DNS::Resource::IN::SRV.name`, "\"Resolv::DNS::Resource::IN::SRV\"\n"},
		{`require "resolv"; p Resolv::DNS::Resource::IN::A.name`, "\"Resolv::DNS::Resource::IN::A\"\n"},
		{`require "resolv"; p defined?(Resolv::DNS::Resource::IN::AAAA)`, "\"constant\"\n"},
		{`require "resolv"; p Resolv::DNS::Name.create("example.com").to_s`, "\"example.com\"\n"},
		{`require "resolv"; p Resolv::Hosts.new.class`, "Resolv::Hosts\n"},
		{`require "resolv"; p Resolv::Hosts::DefaultFileName`, "\"/etc/hosts\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	for _, m := range []string{
		"getresource", "getresources", "getaddress", "getaddresses",
		"getname", "getnames", "each_resource", "each_address", "each_name", "close",
	} {
		if class, _ := evalErr(t, `require "resolv"; Resolv::DNS.new.`+m+`("x")`); class != "NotImplementedError" {
			t.Errorf("Resolv::DNS#%s raised %q, want NotImplementedError", m, class)
		}
	}
}

// TestOptParse covers the optparse loadable shell: require, OptionParser.new
// (with and without a banner and a block), the chainable declaration methods,
// banner accessors, the error class tree, and the NotImplementedError parsing
// methods.
func TestOptParse(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p require "optparse"`, "true\n"},
		{`require "optparse"; p OptionParser.new.class`, "OptionParser\n"},
		{`require "optparse"; p OptParse.equal?(OptionParser)`, "true\n"},
		{`require "optparse"; p OptionParser.new("usage").banner`, "\"usage\"\n"},
		// Block form yields the parser.
		{`require "optparse"; OptionParser.new { |o| p o.class }`, "OptionParser\n"},
		// Declaration methods return self (chainable) and the block-capturing on.
		{`require "optparse"; o=OptionParser.new; p o.on("-x").equal?(o)`, "true\n"},
		{`require "optparse"; o=OptionParser.new; p o.separator("--").equal?(o)`, "true\n"},
		{`require "optparse"; o=OptionParser.new; p o.on_tail("-h").equal?(o)`, "true\n"},
		{`require "optparse"; o=OptionParser.new; p o.on_head("-v").equal?(o)`, "true\n"},
		{`require "optparse"; o=OptionParser.new; p o.accept(Integer).equal?(o)`, "true\n"},
		{`require "optparse"; o=OptionParser.new; p o.reject(Integer).equal?(o)`, "true\n"},
		// banner=/banner round-trip.
		{`require "optparse"; o=OptionParser.new; o.banner="b"; p o.banner`, "\"b\"\n"},
		// Error tree.
		{`require "optparse"; p OptionParser::InvalidOption.ancestors.include?(OptionParser::ParseError)`, "true\n"},
		{`require "optparse"; p OptionParser::ParseError.ancestors.include?(StandardError)`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	for _, m := range []string{"parse", "parse!", "order", "order!", "permute", "permute!", "to_a", "help"} {
		if class, _ := evalErr(t, `require "optparse"; OptionParser.new.`+m+`([])`); class != "NotImplementedError" {
			t.Errorf("OptionParser#%s raised %q, want NotImplementedError", m, class)
		}
	}
}

// TestRipper covers the ripper loadable shell: require and the
// NotImplementedError class methods.
func TestRipper(t *testing.T) {
	if got := eval(t, `p require "ripper"`); got != "true\n" {
		t.Errorf("require got %q", got)
	}
	if got := eval(t, `require "ripper"; p Ripper.class`); got != "Class\n" {
		t.Errorf("Ripper.class got %q", got)
	}
	for _, m := range []string{"sexp", "sexp_raw", "lex", "tokenize", "parse", "slice"} {
		if class, _ := evalErr(t, `require "ripper"; Ripper.`+m+`("1+1")`); class != "NotImplementedError" {
			t.Errorf("Ripper.%s raised %q, want NotImplementedError", m, class)
		}
	}
}

// TestSystemExitFamily covers SystemExit (status/success?/message) and the other
// non-StandardError exception classes added this round.
func TestSystemExitFamily(t *testing.T) {
	cases := []struct{ src, want string }{
		// SystemExit hierarchy + default status.
		{`p SystemExit.ancestors.include?(Exception)`, "true\n"},
		{`p SystemExit.ancestors.include?(StandardError)`, "false\n"},
		{`p SystemExit.new.status`, "0\n"},
		{`p SystemExit.new(2).status`, "2\n"},
		{`p SystemExit.new(2).success?`, "false\n"},
		{`p SystemExit.new(0).success?`, "true\n"},
		{`p SystemExit.new.success?`, "true\n"},
		// (status, message) form.
		{`p SystemExit.new(3, "bye").status`, "3\n"},
		{`p SystemExit.new(3, "bye").message`, "\"bye\"\n"},
		// String-only argument is the message, status defaults to 0.
		{`p SystemExit.new("oops").message`, "\"oops\"\n"},
		{`p SystemExit.new("oops").status`, "0\n"},
		// The other new classes sit under Exception, not StandardError.
		{`p NoMemoryError.ancestors.include?(Exception)`, "true\n"},
		{`p SecurityError.ancestors.include?(Exception)`, "true\n"},
		{`p Interrupt.ancestors.include?(SignalException)`, "true\n"},
		{`p SignalException.ancestors.include?(Exception)`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestSingletonModule covers the Singleton prelude module: require, the memoized
// .instance, and the privatized .new/.allocate.
func TestSingletonModule(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p require "singleton"`, "true\n"},
		{`require "singleton"; class C; include Singleton; def initialize; @n=7; end; def n; @n; end; end; p C.instance.n`, "7\n"},
		{`require "singleton"; class C2; include Singleton; end; p C2.instance.equal?(C2.instance)`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	// .new is private after include Singleton.
	if class, _ := evalErr(t, `require "singleton"; class C3; include Singleton; end; C3.new`); class != "NoMethodError" {
		t.Errorf("C3.new raised %q, want NoMethodError", class)
	}
	if class, _ := evalErr(t, `require "singleton"; class C4; include Singleton; end; C4.allocate`); class != "NoMethodError" {
		t.Errorf("C4.allocate raised %q, want NoMethodError", class)
	}
}

// TestSingletonClassInclude covers the VM fix that makes a module included into
// `class << self` (or via extend) supply class methods, including inheritance by
// a subclass — the mechanism Puppet's InstanceLoader uses.
func TestSingletonClassInclude(t *testing.T) {
	cases := []struct{ src, want string }{
		// include into class << self -> class method.
		{`module M; def hi(x); "hi #{x}"; end; end
class A; class << self; include M; end; end
p A.hi("there")`, "\"hi there\"\n"},
		// subclass inherits it.
		{`module M; def hi(x); "hi #{x}"; end; end
class A; class << self; include M; end; end
class B < A; end
p B.hi("sub")`, "\"hi sub\"\n"},
		// extend gives the same result.
		{`module M2; def yo; 42; end; end
class C; extend M2; end
p C.yo`, "42\n"},
		// prepend into the singleton class wins over an own class method.
		{`module P; def who; "module"; end; end
class D
  class << self
    prepend P
    def who; "class"; end
  end
end
p D.who`, "\"module\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestKernelLoad covers Kernel#load (and Kernel.load): re-executing a file every
// call, returning true, resolving via the script directory, and raising
// LoadError for a missing file.
func TestKernelLoad(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "side.rb", "$counter ||= 0\n$counter += 1\n")

	// load runs the file each time and returns true; the side effect accumulates.
	out, err := runInDir(t, dir, `p load("side.rb")
p load("side.rb")
p $counter`)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if out != "true\ntrue\n2\n" {
		t.Errorf("load got %q", out)
	}

	// Kernel.load works as a module method too.
	out, err = runInDir(t, dir, `Kernel.load("side.rb"); p $counter`)
	if err != nil {
		t.Fatalf("Kernel.load: %v", err)
	}
	if out != "1\n" {
		t.Errorf("Kernel.load got %q", out)
	}

	// An absolute path loads directly.
	abs := filepath.Join(dir, "side.rb")
	out, err = runInDir(t, dir, `p load(`+strconv.Quote(abs)+`)`)
	if err != nil {
		t.Fatalf("abs load: %v", err)
	}
	if out != "true\n" {
		t.Errorf("abs load got %q", out)
	}

	// A missing file raises LoadError.
	if class, _ := evalErr(t, `load("definitely-not-here.rb")`); class != "LoadError" {
		t.Errorf("missing load raised %q, want LoadError", class)
	}

	// A syntax error in the loaded file raises SyntaxError.
	write(t, dir, "bad.rb", "def (")
	_, err = runInDir(t, dir, `load("bad.rb")`)
	if err == nil || !strings.Contains(err.Error(), "SyntaxError") {
		t.Errorf("bad load err=%v, want SyntaxError", err)
	}
}

// TestLoadedFeatures covers the $LOADED_FEATURES / $" mutable Array.
func TestLoadedFeatures(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p $LOADED_FEATURES.class`, "Array\n"},
		{`$LOADED_FEATURES << "x"; p $LOADED_FEATURES.include?("x")`, "true\n"},
		// $" is the same object.
		{`p $".equal?($LOADED_FEATURES)`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestFileMtime covers File.mtime returning a Time, and the Errno::ENOENT for a
// missing file.
func TestFileMtime(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(p, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, rerr := runInDir(t, dir, `p File.mtime(`+strconv.Quote(p)+`).class`)
	if rerr != nil {
		t.Fatalf("mtime: %v", rerr)
	}
	if out != "Time\n" {
		t.Errorf("File.mtime class got %q", out)
	}
	if class, _ := evalErr(t, `File.mtime("/no/such/file/here")`); class != "Errno::ENOENT" {
		t.Errorf("missing mtime raised %q, want Errno::ENOENT", class)
	}
}

// TestFilePathnameCoercion covers File path-argument coercion via #to_path /
// #to_str (Pathname), across the path-manipulation and filesystem-test methods,
// plus the TypeError for a non-coercible argument.
func TestFilePathnameCoercion(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "g.txt")
	if err := os.WriteFile(p, []byte("data!"), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []struct{ src, want string }{
		// Pathname (responds to to_path) is accepted by File.exist?.
		{`require "pathname"; p File.exist?(Pathname.new(` + strconv.Quote(p) + `))`, "true\n"},
		{`require "pathname"; p File.file?(Pathname.new(` + strconv.Quote(p) + `))`, "true\n"},
		{`require "pathname"; p File.directory?(Pathname.new(` + strconv.Quote(dir) + `))`, "true\n"},
		{`require "pathname"; p File.size(Pathname.new(` + strconv.Quote(p) + `))`, "5\n"},
		{`require "pathname"; p File.read(Pathname.new(` + strconv.Quote(p) + `))`, "\"data!\"\n"},
		{`require "pathname"; p File.basename(Pathname.new("/a/b/c.rb"))`, "\"c.rb\"\n"},
		{`require "pathname"; p File.dirname(Pathname.new("/a/b/c.rb"))`, "\"/a/b\"\n"},
		{`require "pathname"; p File.extname(Pathname.new("/a/b/c.rb"))`, "\".rb\"\n"},
		{`require "pathname"; p File.split(Pathname.new("/a/b/c.rb"))`, "[\"/a/b\", \"c.rb\"]\n"},
		{`require "pathname"; p File.join(Pathname.new("a"), Pathname.new("b"))`, "\"a/b\"\n"},
		{`require "pathname"; p File.expand_path(Pathname.new("/a/./b"))`, "\"/a/b\"\n"},
		{`require "pathname"; p File.absolute_path?(Pathname.new("/a"))`, "true\n"},
		{`require "pathname"; p File.absolute_path(Pathname.new("/a/b"))`, "\"/a/b\"\n"},
	}
	for _, c := range cases {
		out, rerr := runInDir(t, dir, c.src)
		if rerr != nil {
			t.Fatalf("src=%q err=%v", c.src, rerr)
		}
		if out != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, out, c.want)
		}
	}
	// An object that responds to to_str (but not to_path) is coerced via the
	// to_str fallback arm.
	if got := eval(t, `class OnlyStr; def to_str; "/x"; end; end; p File.basename(OnlyStr.new)`); got != "\"x\"\n" {
		t.Errorf("to_str coercion got %q", got)
	}
	// A non-coercible argument raises TypeError (the no-to_path/to_str arm).
	if class, _ := evalErr(t, `File.exist?(123)`); class != "TypeError" {
		t.Errorf("File.exist?(Integer) raised %q, want TypeError", class)
	}
	// A to_path that returns a non-String raises TypeError (the !ok arm).
	if class, _ := evalErr(t, `class BadPath; def to_path; 42; end; end; File.exist?(BadPath.new)`); class != "TypeError" {
		t.Errorf("File.exist?(bad to_path) raised %q, want TypeError", class)
	}
	// write/delete also coerce.
	out, rerr := runInDir(t, dir, `require "pathname"
pn = Pathname.new(`+strconv.Quote(filepath.Join(dir, "w.txt"))+`)
File.write(pn, "abc")
v = File.read(pn)
File.delete(pn)
p [v, File.exist?(pn)]`)
	if rerr != nil {
		t.Fatalf("write/delete: %v", rerr)
	}
	if out != "[\"abc\", false]\n" {
		t.Errorf("write/delete got %q", out)
	}
}
