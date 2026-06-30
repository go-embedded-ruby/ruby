// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// TestIPAddr covers the Ruby IPAddr class (backed by
// github.com/go-ruby-ipaddr/ipaddr, the MRI-4.0.5-faithful port of the `ipaddr`
// stdlib): construction (v4/v6, with/without prefix, packed integer, packed
// bytes), the to_s / to_string / cidr / inspect / netmask rendering, prefix and
// masking, membership and to_range, the bitwise operators and address
// arithmetic, the comparison/identity protocol, the predicates and the
// conversions. Every value is asserted against MRI 4.0.5's stdlib. The few
// methods that go beyond MRI 4.0.5 (each, ^, multicast?) are noted at their
// cases; the raw family integer is OS-dependent so ipv4?/ipv6? are asserted
// instead.
func TestIPAddr(t *testing.T) {
	const req = `require "ipaddr"; `
	for _, c := range []struct{ src, want string }{
		// Construction + rendering (MRI to_s masks; inspect shows IPv4:addr/mask).
		{`puts IPAddr.new("192.168.1.5/24").to_s`, "192.168.1.0\n"},
		{`puts IPAddr.new("192.168.1.5/24").to_string`, "192.168.1.0\n"},
		{`puts IPAddr.new("192.168.1.5/24").cidr`, "192.168.1.0/24\n"},
		{`puts IPAddr.new("192.168.1.5/24").inspect`, "#<IPAddr: IPv4:192.168.1.0/255.255.255.0>\n"},
		{`p IPAddr.new("192.168.1.5/24")`, "#<IPAddr: IPv4:192.168.1.0/255.255.255.0>\n"},
		// puts of the IPAddr itself routes through the wrapper's ToS (the masked form).
		{`puts IPAddr.new("192.168.1.5/24")`, "192.168.1.0\n"},
		// An IPAddr is always truthy (the wrapper's Truthy).
		{`puts(IPAddr.new("1.2.3.4") ? "t" : "f")`, "t\n"},
		{`puts IPAddr.new("192.168.1.5/24").netmask`, "255.255.255.0\n"},
		{`puts IPAddr.new("192.168.1.5/24").prefix`, "24\n"},
		// A bare address has a host (all-ones) mask.
		{`puts IPAddr.new("192.168.1.5").to_s`, "192.168.1.5\n"},
		// IPv6.
		{`puts IPAddr.new("::1").inspect`, "#<IPAddr: IPv6:0000:0000:0000:0000:0000:0000:0000:0001/ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff>\n"},
		{`puts IPAddr.new("2001:db8::1/64").to_s`, "2001:db8::\n"},
		{`puts IPAddr.new("2001:db8::1/64").cidr`, "2001:db8::/64\n"},
		{`puts IPAddr.new("2001:db8::1/64").prefix`, "64\n"},
		// Packed-integer form: IPAddr.new(integer, family). Socket::AF_INET == 2.
		{`puts IPAddr.new(0x01020304, 2).to_s`, "1.2.3.4\n"},
		{`puts IPAddr.new(0x01020304, 2).ipv4?`, "true\n"},
		// A non-AF_INET second arg builds an IPv6 address (family int is OS-dependent:
		// the library/Linux use AF_INET6 == 10; on macOS MRI it is 30).
		{`puts IPAddr.new(1, 10).ipv6?`, "true\n"},
		// A Bignum (> 2**63) packed-integer address (exercises the Bignum branch).
		{`puts IPAddr.new(2**64, 10).to_s`, "0:0:0:1::\n"},
		// new_ntoh / ntop from packed network bytes.
		{`puts IPAddr.new_ntoh([1,2,3,4].pack("C*")).to_s`, "1.2.3.4\n"},
		{`puts IPAddr.ntop([1,2,3,4].pack("C*"))`, "1.2.3.4\n"},
		// ntop honours MRI's encoding precedence: a non-BINARY (UTF-8) String
		// raises InvalidAddressError before any length check, while a BINARY
		// string of the wrong length raises AddressFamilyError.
		{`begin; IPAddr.ntop("xy"); rescue => e; puts e.class; end`, "IPAddr::InvalidAddressError\n"},
		{`begin; IPAddr.ntop("xy".b); rescue => e; puts e.class; end`, "IPAddr::AddressFamilyError\n"},

		// to_i (the big.Int-backed unbounded conversion, IPv4 + IPv6).
		{`puts IPAddr.new("10.0.0.1").to_i`, "167772161\n"},
		{`puts IPAddr.new("2001:db8::1").to_i`, "42540766411282592856903984951653826561\n"},

		// Prefix / masking.
		{`puts IPAddr.new("1.2.3.4").mask(16).to_s`, "1.2.0.0\n"},
		{`puts IPAddr.new("1.2.3.4").mask("255.255.0.0").to_s`, "1.2.0.0\n"},
		{`a=IPAddr.new("1.2.3.4/24"); a.prefix=16; puts a.to_s`, "1.2.0.0\n"},
		{`a=IPAddr.new("1.2.3.4/24"); a.prefix=16; puts a.prefix`, "16\n"},

		// Membership: include? / === with a String, an IPAddr and a miss.
		{`puts IPAddr.new("192.168.0.0/16").include?("192.168.5.5")`, "true\n"},
		{`puts IPAddr.new("192.168.0.0/16").include?(IPAddr.new("192.168.5.5"))`, "true\n"},
		{`puts IPAddr.new("192.168.0.0/16").include?("10.0.0.1")`, "false\n"},
		{`puts(IPAddr.new("192.168.0.0/16") === "192.168.5.5")`, "true\n"},
		{`puts IPAddr.new("0.0.0.0/0").include?(167772161)`, "true\n"}, // Integer operand coerces
		{`puts IPAddr.new("::/0").include?(2**64)`, "true\n"},          // Bignum operand coerces

		// to_range returns a Range of host-masked IPAddrs (begin/end).
		{`puts IPAddr.new("10.0.0.0/30").to_range.begin.to_s`, "10.0.0.0\n"},
		{`puts IPAddr.new("10.0.0.0/30").to_range.end.to_s`, "10.0.0.3\n"},
		// each is an EXTENSION beyond MRI 4.0.5 (the library's iteration over to_range).
		{`r=[]; IPAddr.new("10.0.0.0/30").each{|x| r<<x.to_s}; p r`,
			"[\"10.0.0.0\", \"10.0.0.1\", \"10.0.0.2\", \"10.0.0.3\"]\n"},

		// Bitwise operators.
		{`puts((IPAddr.new("10.0.0.0/8") | 0x00010203).to_s)`, "10.1.2.3\n"},
		{`puts((IPAddr.new("10.1.2.3") & "255.255.0.0").to_s)`, "10.1.0.0\n"},
		{`puts((IPAddr.new("1.2.3.4") & IPAddr.new("255.255.0.0")).to_s)`, "1.2.0.0\n"}, // IPAddr operand
		{`puts((~IPAddr.new("0.0.0.0")).to_s)`, "255.255.255.255\n"},
		// ^ / xor are EXTENSIONS beyond MRI 4.0.5 (MRI has no IPAddr#^).
		{`puts((IPAddr.new("1.2.3.4") ^ 0x00000001).to_s)`, "1.2.3.5\n"},
		{`puts(IPAddr.new("1.2.3.4").xor(0x00000001).to_s)`, "1.2.3.5\n"},
		// & | with a Bignum operand (exercises the *object.Bignum coercion branch).
		{`puts((IPAddr.new("0.0.0.0") | (2**32 - 1)).to_s)`, "255.255.255.255\n"},

		// & | with a Bignum operand (> 2**63, exercises the big.Int coercion branch).
		{`puts((IPAddr.new("::") | (2**64)).to_s)`, "0:0:0:1::\n"},
		{`puts((IPAddr.new("ffff:ffff:ffff:ffff::") & (2**128 - 2**64)).to_s)`, "ffff:ffff:ffff:ffff::\n"},

		// Address arithmetic: + and - shift by an offset (operator fast path).
		{`puts((IPAddr.new("1.2.3.4") + 1).to_s)`, "1.2.3.5\n"},
		{`puts((IPAddr.new("1.2.3.4") - 1).to_s)`, "1.2.3.3\n"},
		// The method-dispatch form (.send) reaches the +/- method closures (the bare
		// `ip + n` form takes the binary() operator fast path instead).
		{`puts(IPAddr.new("1.2.3.4").send(:+, 1).to_s)`, "1.2.3.5\n"},
		{`puts(IPAddr.new("1.2.3.4").send(:-, 1).to_s)`, "1.2.3.3\n"},
		{`puts(IPAddr.new("1.2.3.4").send(:==, IPAddr.new("1.2.3.4")))`, "true\n"},
		{`puts(IPAddr.new("1.2.3.4").send(:==, 7))`, "false\n"},
		{`puts IPAddr.new("1.2.3.4").succ.to_s`, "1.2.3.5\n"},

		// Comparison / Comparable / identity / hash.
		{`puts(IPAddr.new("1.2.3.4") <=> IPAddr.new("1.2.3.5"))`, "-1\n"},
		{`puts(IPAddr.new("1.2.3.4") <=> IPAddr.new("1.2.3.4"))`, "0\n"},
		{`puts(IPAddr.new("1.2.3.5") <=> IPAddr.new("1.2.3.4"))`, "1\n"},
		{`puts(IPAddr.new("1.2.3.4") <=> 7)`, "1\n"},                // Integer coerces to 0.0.0.7
		{`puts(IPAddr.new("0.0.0.4") <=> "0.0.0.5")`, "-1\n"},       // String coerces
		{`puts(IPAddr.new("::1:0:0:0:1") <=> (2**64))`, "1\n"},      // Bignum coerces
		{`puts(IPAddr.new("1.2.3.4") <=> :sym)`, "\n"},              // uncoercible -> nil
		{`puts(IPAddr.new("1.2.3.4") <=> IPAddr.new("::1"))`, "\n"}, // cross-family -> nil
		{`puts(IPAddr.new("1.2.3.4") == IPAddr.new("1.2.3.4"))`, "true\n"},
		{`puts(IPAddr.new("1.2.3.4") == IPAddr.new("1.2.3.5"))`, "false\n"},
		{`puts(IPAddr.new("1.2.3.4") == "1.2.3.4")`, "false\n"},           // != non-IPAddr
		{`puts(IPAddr.new("1.2.3.4") < IPAddr.new("1.2.3.5"))`, "true\n"}, // Comparable#<
		{`puts(IPAddr.new("1.2.3.5") > IPAddr.new("1.2.3.4"))`, "true\n"}, // Comparable#>
		{`puts(IPAddr.new("1.2.3.4").eql?(IPAddr.new("1.2.3.4")))`, "true\n"},
		{`puts(IPAddr.new("1.2.3.4").eql?(7))`, "false\n"},
		{`puts(IPAddr.new("1.2.3.4").hash == IPAddr.new("1.2.3.4").hash)`, "true\n"},

		// Predicates.
		{`puts IPAddr.new("192.168.5.5").ipv4?`, "true\n"},
		{`puts IPAddr.new("192.168.5.5").ipv6?`, "false\n"},
		{`puts IPAddr.new("::1").ipv6?`, "true\n"},
		{`puts IPAddr.new("127.0.0.1").loopback?`, "true\n"},
		{`puts IPAddr.new("::1").loopback?`, "true\n"},
		{`puts IPAddr.new("8.8.8.8").loopback?`, "false\n"},
		{`puts IPAddr.new("192.168.0.1").private?`, "true\n"},
		{`puts IPAddr.new("8.8.8.8").private?`, "false\n"},
		{`puts IPAddr.new("169.254.1.1").link_local?`, "true\n"},
		{`puts IPAddr.new("fe80::1").link_local?`, "true\n"},
		{`puts IPAddr.new("8.8.8.8").link_local?`, "false\n"},
		{`puts IPAddr.new("::ffff:1.2.3.4").ipv4_mapped?`, "true\n"},
		{`puts IPAddr.new("1.2.3.4").ipv4_mapped?`, "false\n"},
		{`puts IPAddr.new("::1.2.3.4").ipv4_compat?`, "true\n"},
		{`puts IPAddr.new("1.2.3.4").ipv4_compat?`, "false\n"},
		// multicast? is an EXTENSION beyond MRI 4.0.5 (MRI's IPAddr has no #multicast?).
		{`puts IPAddr.new("224.0.0.1").multicast?`, "true\n"},
		{`puts IPAddr.new("8.8.8.8").multicast?`, "false\n"},

		// Conversions.
		{`puts IPAddr.new("1.2.3.4").native.to_s`, "1.2.3.4\n"}, // already native -> unchanged
		{`puts IPAddr.new("::ffff:1.2.3.4").native.to_s`, "1.2.3.4\n"},
		{`puts IPAddr.new("1.2.3.4").ipv4_mapped.to_s`, "::ffff:1.2.3.4\n"},
		{`puts IPAddr.new("1.2.3.4").ipv4_compat.to_s`, "::1.2.3.4\n"},
		{`puts IPAddr.new("1.2.3.4").hton.unpack("C*").inspect`, "[1, 2, 3, 4]\n"},
		// family returns an integer (the library exposes Linux's canonical value);
		// IPv4 is 2 (AF_INET, the same on every platform).
		{`puts IPAddr.new("1.2.3.4").family`, "2\n"},
	} {
		if got := eval(t, req+c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestIPAddrErrors covers the IPAddr error classes and their MRI hierarchy
// (IPAddr::InvalidPrefixError < InvalidAddressError < Error < ArgumentError),
// and that each raise from the binding produces a rescuable nested exception
// with MRI's verbatim message.
func TestIPAddrErrors(t *testing.T) {
	const req = `require "ipaddr"; `

	// Constant resolution + rescuable hierarchy.
	for _, c := range []struct{ src, want string }{
		{`p IPAddr::Error`, "IPAddr::Error\n"},
		{`p IPAddr::InvalidAddressError`, "IPAddr::InvalidAddressError\n"},
		{`p IPAddr::InvalidPrefixError`, "IPAddr::InvalidPrefixError\n"},
		{`p IPAddr::AddressFamilyError`, "IPAddr::AddressFamilyError\n"},
		{`p IPAddr::Error.ancestors.include?(ArgumentError)`, "true\n"},
		{`p IPAddr::InvalidAddressError.ancestors.include?(IPAddr::Error)`, "true\n"},
		{`p IPAddr::InvalidPrefixError.ancestors.include?(IPAddr::InvalidAddressError)`, "true\n"},
		{`p IPAddr::AddressFamilyError.ancestors.include?(IPAddr::Error)`, "true\n"},
		// A bad address raises IPAddr::InvalidAddressError with MRI's message.
		{`begin; IPAddr.new("999.1.1.1"); rescue IPAddr::InvalidAddressError => e; puts e.message; end`,
			"invalid address: 999.1.1.1\n"},
		// A bad prefix raises IPAddr::InvalidPrefixError (rescuable as Error too).
		{`begin; IPAddr.new("1.2.3.4/33"); rescue IPAddr::InvalidPrefixError => e; puts e.message; end`,
			"invalid length\n"},
		{`begin; IPAddr.new("1.2.3.4/33"); rescue IPAddr::Error; puts "caught"; end`, "caught\n"},
		// The ambiguous zero-filled IPv4 octet raises InvalidAddressError.
		{`begin; IPAddr.new("010.0.0.0"); rescue IPAddr::InvalidAddressError => e; puts e.message; end`,
			"zero-filled number in IPv4 address is ambiguous: 010.0.0.0\n"},
		// succ past the broadcast address overflows -> InvalidAddressError.
		{`begin; IPAddr.new("255.255.255.255").succ; rescue IPAddr::InvalidAddressError => e; puts e.message; end`,
			"invalid address: 4294967296\n"},
	} {
		if got := eval(t, req+c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestIPAddrTypeErrors covers the binding's argument-guard raises: a non-string
// new, a bad operand for the bitwise/arithmetic operators, a bad mask argument,
// the packed-bytes guards and the no-block each path. These are binding seams
// (not MRI-defined surfaces), so only the exception class is asserted.
func TestIPAddrTypeErrors(t *testing.T) {
	const req = `require "ipaddr"; `
	for _, c := range []struct{ src, want string }{
		{`IPAddr.new`, "ArgumentError"},               // no argument
		{`IPAddr.new(:sym)`, "TypeError"},             // neither String nor Integer
		{`IPAddr.new("1.2.3.4") & :sym`, "TypeError"}, // bad bitwise operand
		{`IPAddr.new("1.2.3.4") | :sym`, "TypeError"},
		{`IPAddr.new("1.2.3.4") + "x"`, "TypeError"},      // non-integer offset
		{`IPAddr.new("1.2.3.4") - :sym`, "TypeError"},     // non-integer offset
		{`IPAddr.new("1.2.3.4") + (2**64)`, "RangeError"}, // Bignum offset out of int64 range
		{`IPAddr.new("1.2.3.4").mask(:sym)`, "TypeError"},
		{`IPAddr.new("1.2.3.4").include?(:sym)`, "IPAddr::Error"}, // uncoercible operand -> base error
		{`IPAddr.new("1.2.3.4") * 2`, "NoMethodError"},            // unsupported IPAddr operator
		{`IPAddr.new("1.2.3.4").prefix = "x"`, "TypeError"},       // non-integer prefix
		{`IPAddr.new_ntoh(42)`, "TypeError"},                      // packed bytes not a String
		{`IPAddr.ntop(42)`, "TypeError"},                          // packed bytes not a String
		{`IPAddr.ntop("xy")`, "InvalidAddressError"},              // non-BINARY (UTF-8) String -> InvalidAddressError, like MRI
		{`IPAddr.new("1.2.3.4").each`, "LocalJumpError"},          // each without a block
	} {
		if err := runErr(t, req+c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got=%v want %q", c.src, err, c.want)
		}
	}
}
