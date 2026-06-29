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

// resolvReq prefixes a require so each Resolv snippet is self-contained.
const resolvReq = `require "resolv"; `

// TestResolvName covers Resolv::DNS::Name (the go-ruby-resolv binding):
// create/to_s, the case-insensitive ==/eql?, length, and the trailing-dot
// absolute? flag — each pinned to MRI 4.0.5.
func TestResolvName(t *testing.T) {
	cases := []struct{ src, want string }{
		{resolvReq + `p Resolv::DNS::Name.create("www.example.com").to_s`, "\"www.example.com\"\n"},
		// The original byte string is preserved by to_s.
		{resolvReq + `p Resolv::DNS::Name.create("WWW.Example.COM").to_s`, "\"WWW.Example.COM\"\n"},
		{resolvReq + `p Resolv::DNS::Name.create("a.b.c").length`, "3\n"},
		{resolvReq + `p Resolv::DNS::Name.create("a.b.").absolute?`, "true\n"},
		{resolvReq + `p Resolv::DNS::Name.create("a.b").absolute?`, "false\n"},
		// == / eql? compare labels case-insensitively (RFC 4343).
		{resolvReq + `p(Resolv::DNS::Name.create("A.B") == Resolv::DNS::Name.create("a.b"))`, "true\n"},
		{resolvReq + `p Resolv::DNS::Name.create("A.B").eql?(Resolv::DNS::Name.create("a.b"))`, "true\n"},
		{resolvReq + `p(Resolv::DNS::Name.create("a.b") == Resolv::DNS::Name.create("a.c"))`, "false\n"},
		// A non-Name compares unequal.
		{resolvReq + `p(Resolv::DNS::Name.create("a.b") == "a.b")`, "false\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestResolvIPv6Compression covers the library's canonical "::" rendering, which
// must match MRI's first-longest-run compression quirk exactly.
func TestResolvIPv6Compression(t *testing.T) {
	cases := []struct{ src, want string }{
		{resolvReq + `p Resolv::IPv6.create("fe80:0:0:0:0:0:0:1").to_s`, "\"fe80::1\"\n"},
		{resolvReq + `p Resolv::IPv6.create("0:0:0:0:0:0:0:0").to_s`, "\"::\"\n"},
		{resolvReq + `p Resolv::IPv6.create("1:0:0:0:0:0:0:1").to_s`, "\"1::1\"\n"},
		{resolvReq + `p Resolv::IPv6.create("2001:db8::1").to_s`, "\"2001:db8::1\"\n"},
		// The Regex constant is the library's MRI-exact matcher.
		{resolvReq + `p Resolv::IPv4::Regex.match?("256.1.1.1")`, "false\n"},
		{resolvReq + `p Resolv::IPv4::Regex.match?("10.0.0.1")`, "true\n"},
		{resolvReq + `p Resolv::IPv6::Regex.match?("2001:db8::1")`, "true\n"},
		{resolvReq + `p Resolv::IPv6::Regex.match?("nope")`, "false\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestResolvMessage covers Resolv::DNS::Message: new/id, the header flag
// accessors, add_question, encode (byte-exact wire + the encoding-name quirk),
// the Message.decode class method, question/each_question, and a DecodeError.
func TestResolvMessage(t *testing.T) {
	const mk = resolvReq + `m=Resolv::DNS::Message.new(0x1234); m.rd=1; ` +
		`m.add_question("www.example.com",Resolv::DNS::Resource::IN::A); `
	cases := []struct{ src, want string }{
		{resolvReq + `p Resolv::DNS::Message.new(7).id`, "7\n"},
		{resolvReq + `p Resolv::DNS::Message.new.id`, "0\n"},
		// Wire encoding is byte-exact against MRI's Message#encode.
		{mk + `p m.encode.unpack1("H*")`,
			"\"12340100000100000000000003777777076578616d706c6503636f6d0000010001\"\n"},
		// An all-ASCII wire string reports the default (UTF-8) encoding…
		{mk + `p m.encode.encoding.name`, "\"UTF-8\"\n"},
		// …a high address octet flips it to ASCII-8BIT (matching MRI).
		{resolvReq + `m=Resolv::DNS::Message.new(1)
m.add_answer("a.b",1,Resolv::DNS::Resource::IN::A.new(Resolv::IPv4.create("255.0.0.255")))
p m.encode.encoding.name`, "\"ASCII-8BIT\"\n"},
		// Header flag accessors round-trip.
		{resolvReq + `m=Resolv::DNS::Message.new(1); m.qr=1; m.aa=1; m.tc=0; m.ra=1; m.rcode=3; m.opcode=2
p [m.qr,m.opcode,m.aa,m.tc,m.rd,m.ra,m.rcode]`, "[1, 2, 1, 0, 0, 1, 3]\n"},
		// decode round-trips the header and question.
		{mk + `d=Resolv::DNS::Message.decode(m.encode); p [d.id, d.rd, d.question[0][0].to_s, d.question[0][1].name]`,
			"[4660, 1, \"www.example.com\", \"Resolv::DNS::Resource::IN::A\"]\n"},
		// add_question defaults to the A type when none is given.
		{resolvReq + `m=Resolv::DNS::Message.new(1); m.add_question("x.y")
d=Resolv::DNS::Message.decode(m.encode); p d.question[0][1].name`,
			"\"Resolv::DNS::Resource::IN::A\"\n"},
		// each_question yields [name, typeclass].
		{mk + `d=Resolv::DNS::Message.decode(m.encode); d.each_question{|nm,tc| p [nm.to_s, tc.name]}`,
			"[\"www.example.com\", \"Resolv::DNS::Resource::IN::A\"]\n"},
		// A name argument (not a String) is accepted by add_question.
		{resolvReq + `m=Resolv::DNS::Message.new(1)
m.add_question(Resolv::DNS::Name.create("q.r"),Resolv::DNS::Resource::IN::A)
d=Resolv::DNS::Message.decode(m.encode); p d.question[0][0].to_s`, "\"q.r\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
	// Malformed wire data raises a DecodeError.
	if class, _ := evalErr(t, resolvReq+`Resolv::DNS::Message.decode("\xff")`); class != "Resolv::DNS::DecodeError" {
		t.Errorf("bad decode raised %q, want Resolv::DNS::DecodeError", class)
	}
}

// TestResolvRecords covers the Resolv::DNS::Resource::IN::* record classes:
// construction, the MRI accessors, and a full encode/decode round-trip through a
// Message answer section. Every wired record type is exercised.
func TestResolvRecords(t *testing.T) {
	// roundTrip wraps a record in an answer, encodes, decodes, and reads it back.
	const rt = resolvReq + `def rt(rec)
  m=Resolv::DNS::Message.new(1); m.add_answer("a.b",7,rec)
  Resolv::DNS::Message.decode(m.encode).answer[0]
end
`
	cases := []struct{ src, want string }{
		// A / AAAA address accessors.
		{rt + `a=rt(Resolv::DNS::Resource::IN::A.new(Resolv::IPv4.create("1.2.3.4")))
p [a[0].to_s, a[1], a[2].address.to_s, a[2].class.name]`,
			"[\"a.b\", 7, \"1.2.3.4\", \"Resolv::DNS::Resource::IN::A\"]\n"},
		{rt + `a=rt(Resolv::DNS::Resource::IN::AAAA.new(Resolv::IPv6.create("::1")))
p a[2].address.to_s`, "\"::1\"\n"},
		// A accepts a plain dotted String too (rbgo is lenient over MRI's IPv4 arg).
		{rt + `p rt(Resolv::DNS::Resource::IN::A.new("9.9.9.9"))[2].address.to_s`, "\"9.9.9.9\"\n"},
		// MX preference + exchange.
		{rt + `m=rt(Resolv::DNS::Resource::IN::MX.new(10,Resolv::DNS::Name.create("mail.a.b")))
p [m[2].preference, m[2].exchange.to_s]`, "[10, \"mail.a.b\"]\n"},
		// SRV priority/weight/port/target.
		{rt + `s=rt(Resolv::DNS::Resource::IN::SRV.new(1,2,80,Resolv::DNS::Name.create("h.a.b")))
p [s[2].priority, s[2].weight, s[2].port, s[2].target.to_s]`, "[1, 2, 80, \"h.a.b\"]\n"},
		// CNAME / NS / PTR single-name records.
		{rt + `p rt(Resolv::DNS::Resource::IN::CNAME.new(Resolv::DNS::Name.create("c.d")))[2].name.to_s`, "\"c.d\"\n"},
		{rt + `p rt(Resolv::DNS::Resource::IN::NS.new(Resolv::DNS::Name.create("ns.d")))[2].name.to_s`, "\"ns.d\"\n"},
		{rt + `p rt(Resolv::DNS::Resource::IN::PTR.new(Resolv::DNS::Name.create("p.d")))[2].name.to_s`, "\"p.d\"\n"},
		// TXT strings + the data convenience accessor.
		{rt + `t=rt(Resolv::DNS::Resource::IN::TXT.new("hi","there")); p [t[2].strings, t[2].data]`,
			"[[\"hi\", \"there\"], \"hi\"]\n"},
		// A single-string TXT.
		{rt + `p rt(Resolv::DNS::Resource::IN::TXT.new("solo"))[2].data`, "\"solo\"\n"},
		// HINFO cpu/os.
		{rt + `h=rt(Resolv::DNS::Resource::IN::HINFO.new("amd64","linux")); p [h[2].cpu, h[2].os]`,
			"[\"amd64\", \"linux\"]\n"},
		// each_answer yields [name, ttl, data].
		{rt + `m=Resolv::DNS::Message.new(1)
m.add_answer("a.b",5,Resolv::DNS::Resource::IN::A.new(Resolv::IPv4.create("8.8.8.8")))
Resolv::DNS::Message.decode(m.encode).each_answer{|nm,ttl,data| p [nm.to_s, ttl, data.address.to_s]}`,
			"[\"a.b\", 5, \"8.8.8.8\"]\n"},
		// authority / additional sections render the same [name, ttl, data] tuples.
		{rt + `m=Resolv::DNS::Message.new(1)
m.add_authority("a.b",1,Resolv::DNS::Resource::IN::NS.new(Resolv::DNS::Name.create("ns.a.b")))
m.add_additional("a.b",1,Resolv::DNS::Resource::IN::A.new(Resolv::IPv4.create("1.1.1.1")))
d=Resolv::DNS::Message.decode(m.encode)
p [d.authority[0][2].name.to_s, d.additional[0][2].address.to_s]`,
			"[\"ns.a.b\", \"1.1.1.1\"]\n"},
		// SOA: all seven fields round-trip through encode/decode.
		{rt + `soa=Resolv::DNS::Resource::IN::SOA.new(
  Resolv::DNS::Name.create("ns.a.b"), Resolv::DNS::Name.create("root.a.b"),
  2024010101, 7200, 3600, 1209600, 86400)
r=rt(soa)[2]
p [r.mname.to_s, r.rname.to_s, r.serial, r.refresh, r.retry, r.expire, r.minimum]`,
			"[\"ns.a.b\", \"root.a.b\", 2024010101, 7200, 3600, 1209600, 86400]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
	// A malformed IPv4/IPv6 passed to an A/AAAA record raises ArgumentError.
	if class, _ := evalErr(t, resolvReq+`Resolv::DNS::Resource::IN::A.new("999.1")`); class != "ArgumentError" {
		t.Errorf("bad A addr raised %q, want ArgumentError", class)
	}
	if class, _ := evalErr(t, resolvReq+`Resolv::DNS::Resource::IN::AAAA.new("nope")`); class != "ArgumentError" {
		t.Errorf("bad AAAA addr raised %q, want ArgumentError", class)
	}
}

// TestResolvHostsParse covers Resolv::Hosts backed by go-ruby-resolv's
// ParseHosts: the content-string constructor, getaddress(es)/getname(s) with
// MRI's reversed per-name order, and the ResolvError for an absent entry.
func TestResolvHostsParse(t *testing.T) {
	const h = resolvReq + `h=Resolv::Hosts.new("127.0.0.1 localhost loop\n10.0.0.2 foo foo.example\n10.0.0.3 foo\n"); `
	cases := []struct{ src, want string }{
		{h + `p h.getaddress("localhost")`, "\"127.0.0.1\"\n"},
		{h + `p h.getaddress("loop")`, "\"127.0.0.1\"\n"},
		// getaddresses returns MRI's reversed order across the two foo lines.
		{h + `p h.getaddresses("foo")`, "[\"10.0.0.3\", \"10.0.0.2\"]\n"},
		{h + `p h.getname("10.0.0.2")`, "\"foo\"\n"},
		{h + `p h.getnames("10.0.0.2")`, "[\"foo\", \"foo.example\"]\n"},
		// getaddresses for an absent name is [].
		{h + `p h.getaddresses("absent")`, "[]\n"},
		{h + `p h.getnames("9.9.9.9")`, "[]\n"},
		// An empty (default) table answers nothing.
		{resolvReq + `p Resolv::Hosts.new.getaddresses("x")`, "[]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	// An absent name / address raises ResolvError from getaddress / getname.
	if class, _ := evalErr(t, h+`h.getaddress("nope")`); class != "Resolv::ResolvError" {
		t.Errorf("getaddress(absent) raised %q, want Resolv::ResolvError", class)
	}
	if class, _ := evalErr(t, h+`h.getname("9.9.9.9")`); class != "Resolv::ResolvError" {
		t.Errorf("getname(absent) raised %q, want Resolv::ResolvError", class)
	}
}

// TestOptParse covers the real pure-Ruby OptionParser (prelude): require,
// OptionParser.new (with and without a banner and a block), the chainable
// declaration methods, the error class tree, and the parse family — long/short
// options, mandatory/optional arguments, =-joined values, bundled shorts,
// --[no-] negation, abbreviation, type coercion, the -- terminator, order!/
// permute!/getopts, and each MRI-exact ParseError. Behaviour is pinned against
// MRI 4.x.
func TestOptParse(t *testing.T) {
	const req = `require "optparse"; `
	cases := []struct{ src, want string }{
		{`p require "optparse"`, "true\n"},
		{req + `p require "optparse"`, "false\n"}, // second require returns false
		{req + `p OptionParser.new.class`, "OptionParser\n"},
		{req + `p OptParse.equal?(OptionParser)`, "true\n"},
		{req + `p OptionParser.new("usage").banner`, "\"usage\"\n"},
		// Block form yields the parser.
		{req + `OptionParser.new { |o| p o.class }`, "OptionParser\n"},
		// Declaration methods return self (chainable).
		{req + `o=OptionParser.new; p o.on("-x"){}.equal?(o)`, "true\n"},
		{req + `o=OptionParser.new; p o.separator("--").equal?(o)`, "true\n"},
		{req + `o=OptionParser.new; p o.on_tail("-h"){}.equal?(o)`, "true\n"},
		{req + `o=OptionParser.new; p o.on_head("-v"){}.equal?(o)`, "true\n"},
		{req + `o=OptionParser.new; p o.accept(Integer){}.equal?(o)`, "true\n"},
		{req + `o=OptionParser.new; p o.reject(Integer).equal?(o)`, "true\n"},
		{req + `o=OptionParser.new; p o.define("-d"){}.equal?(o)`, "true\n"},
		{req + `o=OptionParser.new; p o.def_option("-d"){}.equal?(o)`, "true\n"},
		// banner=/banner round-trip and program_name/version/release.
		{req + `o=OptionParser.new; o.banner="b"; p o.banner`, "\"b\"\n"},
		{req + `o=OptionParser.new; o.program_name="prog"; p o.program_name`, "\"prog\"\n"},
		{req + `o=OptionParser.new; o.program_name="prog"; o.version="1.2"; o.release="r1"; p o.ver`,
			"\"prog 1.2 (r1)\"\n"},
		{req + `o=OptionParser.new; p o.ver`, "nil\n"},
		{req + `o=OptionParser.new; o.program_name="p"; o.version="1"; p o.ver`,
			"\"p 1\"\n"},
		{req + `o=OptionParser.new; o.summary_width=20; p o.summary_width`, "20\n"},
		{req + `o=OptionParser.new; o.summary_indent="  "; p o.summary_indent`,
			"\"  \"\n"},
		{req + `o=OptionParser.new; o.default_argv=["a"]; p o.default_argv`,
			"[\"a\"]\n"},
		// Error tree.
		{req + `p OptionParser::InvalidOption.ancestors.include?(OptionParser::ParseError)`, "true\n"},
		{req + `p OptionParser::ParseError.ancestors.include?(StandardError)`, "true\n"},
		{req + `p OptionParser::AmbiguousArgument.ancestors.include?(OptionParser::InvalidArgument)`, "true\n"},
		// --- parsing -----------------------------------------------------------
		// Long option with a flag and a mandatory argument; argv is mutated.
		{req + `o={}; pr=OptionParser.new
pr.on("-v","--verbose"){o[:v]=true}; pr.on("-n","--name NAME"){|x|o[:n]=x}
a=["-v","--name","bob","f"]; pr.parse!(a); p [o,a]`,
			"[{v: true, n: \"bob\"}, [\"f\"]]\n"},
		// =-joined value.
		{req + `o={}; pr=OptionParser.new; pr.on("--name NAME"){|x|o[:n]=x}
pr.parse!(["--name=bob"]); p o`, "{n: \"bob\"}\n"},
		// Optional argument: present, taken from next token, and absent.
		{req + `o={}; pr=OptionParser.new; pr.on("--name [V]"){|x|o[:n]=x}
pr.parse!(["--name","x"]); p o`, "{n: \"x\"}\n"},
		{req + `o={}; pr=OptionParser.new; pr.on("--name [V]"){|x|o[:n]=x}
pr.parse!(["--name"]); p o`, "{n: nil}\n"},
		// Bundled shorts, including a trailing arg-taking short.
		{req + `o={}; pr=OptionParser.new
pr.on("-x"){o[:x]=1}; pr.on("-v"){o[:v]=1}; pr.on("-f N"){|n|o[:f]=n}
pr.parse!(["-xvfval"]); p o`, "{x: 1, v: 1, f: \"val\"}\n"},
		// --[no-] negation.
		{req + `o={}; pr=OptionParser.new; pr.on("--[no-]color"){|b|o[:c]=b}
pr.parse!(["--no-color"]); p o`, "{c: false}\n"},
		{req + `o={}; pr=OptionParser.new; pr.on("--[no-]color"){|b|o[:c]=b}
pr.parse!(["--color"]); p o`, "{c: true}\n"},
		// Type coercion: Integer (decimal + hex), Float, Array.
		{req + `o={}; pr=OptionParser.new; pr.on("--n N",Integer){|n|o[:n]=n}
pr.parse!(["--n","0xff"]); p o`, "{n: 255}\n"},
		{req + `o={}; pr=OptionParser.new; pr.on("--f F",Float){|f|o[:f]=f}
pr.parse!(["--f","3.5"]); p o`, "{f: 3.5}\n"},
		{req + `o={}; pr=OptionParser.new; pr.on("--l L",Array){|l|o[:l]=l}
pr.parse!(["--l","a,b,c"]); p o`, "{l: [\"a\", \"b\", \"c\"]}\n"},
		// A custom accept-list (Array of allowed values, with prefix completion).
		{req + `o={}; pr=OptionParser.new; pr.on("--s S",[:big,:small]){|s|o[:s]=s}
pr.parse!(["--s","bi"]); p o`, "{s: :big}\n"},
		// Abbreviation (unique prefix) and the -- terminator.
		{req + `o={}; pr=OptionParser.new; pr.on("--verbose"){o[:v]=1}
pr.parse!(["--verb"]); p o`, "{v: 1}\n"},
		{req + `o={}; pr=OptionParser.new; pr.on("-v"){o[:v]=1}; pr.on("-x"){o[:x]=1}
a=["-v","--","-x"]; pr.parse!(a); p [o,a]`, "[{v: 1}, [\"-x\"]]\n"},
		// "-" is an operand.
		{req + `o={}; pr=OptionParser.new; pr.on("-v"){o[:v]=1}
a=["-v","-"]; pr.parse!(a); p [o,a]`, "[{v: 1}, [\"-\"]]\n"},
		// order! stops at the first non-option; permute! reorders.
		{req + `o={}; pr=OptionParser.new; pr.on("-v"){o[:v]=1}; pr.on("-x"){o[:x]=1}
a=["-v","cmd","-x"]; pr.order!(a); p [o,a]`, "[{v: 1}, [\"cmd\", \"-x\"]]\n"},
		{req + `o={}; pr=OptionParser.new; pr.on("-v"){o[:v]=1}; pr.on("-x"){o[:x]=1}
a=["-v","cmd","-x"]; pr.permute!(a); p [o,a]`, "[{v: 1, x: 1}, [\"cmd\"]]\n"},
		// order with a non-option block sink.
		{req + `seen=[]; pr=OptionParser.new; pr.on("-v"){}
a=["-v","x","y"]; pr.order!(a){|n|seen<<n}; p seen`, "[\"x\", \"y\"]\n"},
		// parse (non-bang) leaves the source argv untouched.
		{req + `pr=OptionParser.new; pr.on("-v"){}
a=["-v","x"]; r=pr.parse(a); p [a,r]`,
			"[[\"-v\", \"x\"], [\"x\"]]\n"},
		{req + `pr=OptionParser.new; pr.on("-v"){}
a=["-v","x"]; r=pr.permute(a); p [a,r]`,
			"[[\"-v\", \"x\"], [\"x\"]]\n"},
		{req + `pr=OptionParser.new; pr.on("-v"){}
a=["-v","x","-v"]; r=pr.order(a); p [a,r]`,
			"[[\"-v\", \"x\", \"-v\"], [\"x\", \"-v\"]]\n"},
		// getopts.
		{req + `pr=OptionParser.new; a=["-a","--bar","v","rest"]
r=pr.getopts(a,"a","bar:"); p [r,a]`,
			"[{\"a\" => true, \"bar\" => \"v\"}, [\"rest\"]]\n"},
		// help / to_s structure.
		{req + `pr=OptionParser.new; pr.banner="Usage: x"; pr.on("-v","--verbose","be v")
print pr.help`,
			"Usage: x\n    -v, --verbose                    be v\n"},
		{req + `pr=OptionParser.new; pr.program_name="optparse"; pr.on("--name N","the name")
print pr.to_s`,
			"Usage: optparse [options]\n        --name N                     the name\n"},
		{req + `pr=OptionParser.new; pr.separator("S:"); pr.on("-v","d")
print pr.summarize.join`,
			"S:\n    -v                               d\n"},
		// A short character with no -X switch completes to a unique long option.
		{req + `o={}; pr=OptionParser.new; pr.on("--name N"){|x|o[:n]=x}
pr.parse!(["-n","bob"]); p o`, "{n: \"bob\"}\n"},
		// The native parser is a truthy object and inspects as #<OptionParser>.
		{req + `p OptionParser.new ? 1 : 2`, "1\n"},
		{req + `p OptionParser.new`, "#<OptionParser>\n"},
		// new(banner, width, indent): the width/indent override the summary layout
		// (a nil banner keeps the default Usage line).
		{req + `o=OptionParser.new(nil, 10, "  "); p [o.summary_width, o.summary_indent]`,
			"[10, \"  \"]\n"},
		// A Hash candidate map: the matched key reports the Hash value.
		{req + `o={}; pr=OptionParser.new; pr.on("--x X",{"a"=>1,"bb"=>2}){|v|o[:x]=v}
pr.parse!(["--x","a"]); p o`, "{x: 1}\n"},
		// An Array candidate set of non-String objects keys on each #to_s and reports
		// the original object — the prelude's list behaviour this binding preserves.
		{req + `o={}; pr=OptionParser.new; pr.on("--n N",[1,2,30]){|v|o[:n]=v}
pr.parse!(["--n","30"]); p o`, "{n: 30}\n"},
		// String coercion is the identity accept.
		{req + `o={}; pr=OptionParser.new; pr.on("--s S",String){|v|o[:s]=v}
pr.parse!(["--s","hi"]); p o`, "{s: \"hi\"}\n"},
		// A very large Integer coerces through to a Bignum, round-tripping exactly.
		{req + `o={}; pr=OptionParser.new; pr.on("--n N",Integer){|n|o[:n]=n}
pr.parse!(["--n","0xffffffffffffffffffffffffff"]); p o`,
			"{n: 20282409603651670423947251286015}\n"},
		// An unknown coercion Class falls back to the identity String accept — the
		// prelude's @acceptables miss behaviour this binding preserves.
		{req + `class C; end; o={}; pr=OptionParser.new; pr.on("--x X",C){|v|o[:x]=v}
pr.parse!(["--x","hi"]); p o`, "{x: \"hi\"}\n"},
		// version/release round-trip; version is nil until set, release likewise.
		{req + `o=OptionParser.new; p o.version; o.version="1"; p o.version
p o.release; o.release="r"; p o.release`, "nil\n\"1\"\nnil\n\"r\"\n"},
		// parse!/order! with no argv argument operate on default_argv in place.
		{req + `o={}; pr=OptionParser.new; pr.on("-v"){o[:v]=1}
pr.default_argv=["-v","f"]; r=pr.parse!; p [o,r]`, "[{v: 1}, [\"f\"]]\n"},
		{req + `o={}; pr=OptionParser.new; pr.on("-v"){o[:v]=1}
pr.default_argv=["-v","cmd","-v"]; r=pr.order!; p [o,r]`,
			"[{v: 1}, [\"cmd\", \"-v\"]]\n"},
		// getopts off default_argv (no leading Array argument).
		{req + `pr=OptionParser.new; pr.default_argv=["-a","rest"]
r=pr.getopts("a"); p [r,pr.default_argv]`,
			"[{\"a\" => true}, [\"rest\"]]\n"},
		// getopts with an absent arg-taking option reports nil.
		{req + `pr=OptionParser.new; p pr.getopts(["x"],"a","bar:")`,
			"{\"a\" => false, \"bar\" => nil}\n"},
		// on(...) with no block records the value but dispatches nothing.
		{req + `pr=OptionParser.new; pr.on("-v"); p pr.parse!(["-v","x"])`,
			"[\"x\"]\n"},
		// separator with no argument inserts a blank line.
		{req + `pr=OptionParser.new; pr.separator; pr.on("-v","d")
print pr.summarize.join`, "\n    -v                               d\n"},
		// program_name defaults to "optparse" when no explicit name and $0 unset,
		// and feeds the default banner; an explicit program_name overrides it.
		{req + `pr=OptionParser.new; pr.program_name="myprog"; p pr.program_name`,
			"\"myprog\"\n"},
		{req + `pr=OptionParser.new; pr.program_name="myprog"; pr.on("-v","d")
print pr.help`, "Usage: myprog [options]\n    -v                               d\n"},
		// program_name with no explicit name falls back to File.basename($0).
		{req + `$0="/usr/bin/foo"; p OptionParser.new.program_name`, "\"foo\"\n"},
		// new with non-Integer width / non-String indent placeholders keeps the
		// defaults (the nil-banner path), and a non-Array default_argv= is ignored.
		{req + `o=OptionParser.new("u", nil, nil)
p [o.banner, o.summary_width, o.summary_indent]`, "[\"u\", 32, \"    \"]\n"},
		{req + `o=OptionParser.new; o.default_argv="x"; p o.default_argv`, "[]\n"},
		// banner is nil until set (the default Usage line is only synthesized for
		// help/to_s) — the prelude's attr_accessor behaviour.
		{req + `p OptionParser.new.banner`, "nil\n"},
		// order (non-bang) with a non-option block sink leaves the source untouched.
		{req + `seen=[]; pr=OptionParser.new; pr.on("-v"){}
r=pr.order(["-v","x","y"]){|n|seen<<n}; p [seen,r]`,
			"[[\"x\", \"y\"], []]\n"},
	}

	// The non-mutating parse/getopts forms re-raise a library error as the matching
	// OptionParser:: exception, just like the bang forms.
	for _, c := range []struct{ src, class, msg string }{
		{req + `OptionParser.new.parse(["--bad"])`,
			"OptionParser::InvalidOption", "invalid option: --bad"},
		{req + `OptionParser.new.getopts(["--bad"],"a")`,
			"OptionParser::InvalidOption", "invalid option: --bad"},
	} {
		if class, msg := evalErr(t, c.src); class != c.class || msg != c.msg {
			t.Errorf("src=%q got (%s, %q), want (%s, %q)", c.src, class, msg, c.class, c.msg)
		}
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}

	// ParseError subclasses, messages, reasons and args — each pinned to MRI.
	errCases := []struct{ src, class, msg string }{
		{req + `pr=OptionParser.new; pr.on("--verbose"){}; pr.parse!(["--bogus"])`,
			"OptionParser::InvalidOption", "invalid option: --bogus"},
		{req + `pr=OptionParser.new; pr.on("--name N"){}; pr.parse!(["--name"])`,
			"OptionParser::MissingArgument", "missing argument: --name"},
		{req + `pr=OptionParser.new; pr.on("--flag"){}; pr.parse!(["--flag=x"])`,
			"OptionParser::NeedlessArgument", "needless argument: --flag=x"},
		{req + `pr=OptionParser.new; pr.on("--verbose"){}; pr.on("--version"){}; pr.parse!(["--ve"])`,
			"OptionParser::AmbiguousOption", "ambiguous option: --ve"},
		{req + `pr=OptionParser.new; pr.on("--n N",Integer){}; pr.parse!(["--n","abc"])`,
			"OptionParser::InvalidArgument", "invalid argument: --n abc"},
		{req + `pr=OptionParser.new; pr.on("--f F",Float){}; pr.parse!(["--f","x"])`,
			"OptionParser::InvalidArgument", "invalid argument: --f x"},
		{req + `pr=OptionParser.new; pr.on("--s S",[:a,:b]){}; pr.parse!(["--s","z"])`,
			"OptionParser::InvalidArgument", "invalid argument: --s z"},
		{req + `pr=OptionParser.new; pr.parse!(["-x"])`,
			"OptionParser::InvalidOption", "invalid option: -x"},
		{req + `pr=OptionParser.new; pr.on("-n N"){}; pr.parse!(["-n"])`,
			"OptionParser::MissingArgument", "missing argument: -n"},
	}
	for _, c := range errCases {
		class, msg := evalErr(t, c.src)
		if class != c.class || msg != c.msg {
			t.Errorf("src=%q got (%s, %q), want (%s, %q)", c.src, class, msg, c.class, c.msg)
		}
	}

	// reason / args / recover on a ParseError.
	if got := eval(t, req+`
e=OptionParser::InvalidArgument.new("--n","abc")
p [e.reason, e.args, e.message]`); got != "[\"invalid argument\", [\"--n\", \"abc\"], \"invalid argument: --n abc\"]\n" {
		t.Errorf("ParseError accessors: got %q", got)
	}
	if got := eval(t, req+`
e=OptionParser::InvalidOption.new("--bad"); a=["x"]; e.recover(a); p a`); got != "[\"--bad\", \"x\"]\n" {
		t.Errorf("ParseError#recover: got %q", got)
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
