// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// A reusable Ruby timezone object: #local_to_utc / #utc_to_local shift by a fixed
// offset, and #abbr reports a name. It exercises the Time.new / Time.now / Time.at
// timezone-object protocol without depending on the machine's zone database.
const tzClass = `
class TZ
  def initialize(off, abbr = nil); @off = off; @abbr = abbr; end
  def local_to_utc(t); t - @off; end
  def utc_to_local(t); t + @off; end
  def abbr(t); @abbr; end
end
`

// numClass is a Numeric subclass whose #to_r is 5/2, exercising the num_exact
// #to_r coercion path (which Time only takes for a genuine Numeric).
const numClass = "class N < Numeric; def to_r; Rational(5, 2); end; end; "

// TestTimeNewConformance covers the argument forms added for MRI 4.0.6
// Time.new / Time.utc / Time.local conformance: String and coerced calendar
// fields, the String-instant form, utc_offset resolution, and timezone objects.
// Every expectation was verified byte-for-byte against MRI 4.0 on fixed instants.
func TestTimeNewConformance(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		// --- String / coerced calendar fields (obj2int / month_arg / obj2subsec).
		{`p Time.utc("2000").year`, "2000\n"},
		{`p Time.utc(2000, "12").mon`, "12\n"},
		{`p Time.utc(2000, "dec").mon`, "12\n"}, // three-letter month name
		{`p Time.utc(2000, "DEC").mon`, "12\n"}, // case-insensitive
		{`p Time.utc(2000, "12", "15").day`, "15\n"},
		{`p Time.utc(2000, 1, 1, "5").hour`, "5\n"},
		{`p Time.utc(2000, 1, 1, 1, "8").min`, "8\n"},
		{`p Time.utc(2000, 1, 1, 1, 1, "8").sec`, "8\n"},
		{`p Time.utc("2000","09","09","09","09","09").to_a[0..5]`, "[9, 9, 9, 9, 9, 2000]\n"}, // base 10, not octal
		// #to_str month, #to_int fields.
		{`o=Object.new; def o.to_str; "12"; end; p Time.utc(2008, o).mon`, "12\n"},
		{`o=Object.new; def o.to_int; 3; end; p Time.utc(2008, o).mon`, "3\n"},
		{`o=Object.new; def o.to_int; 1; end; p Time.utc(o).year`, "1\n"},
		{`o=Object.new; def o.to_int; 7; end; p Time.utc(2000,1,1,1,1,o).sec`, "7\n"},
		// nil calendar fields fall back to the default.
		{`p Time.utc(2000, nil, nil, nil, nil, nil).to_a[0..5]`, "[0, 0, 0, 1, 1, 2000]\n"},
		// Fractional seconds as Float / Rational.
		{`p Time.utc(2000,1,1,20,15,1.75).usec`, "750000\n"},
		{`p Time.utc(2000,1,1,20,15,Rational(99,10)).sec`, "9\n"},
		{`p Time.utc(2000,1,1,20,15,Rational(99,10)).usec`, "900000\n"},

		// --- 7th argument to utc/local is microseconds.
		{`p Time.utc(2000,1,1,0,0,1,999999).nsec`, "999999000\n"},
		{`p Time.utc(2000,1,1,0,0,1,1.75).nsec`, "1750\n"},
		{`p Time.utc(2000,1,1,0,0,1,Rational(99,10)).nsec`, "9900\n"},
		// A microseconds argument discards any fractional seconds.
		{`p Time.utc(2000,1,1,0,0,1.75,2).nsec`, "2000\n"},
		{`o=Object.new; def o.to_int; 5; end; p Time.utc(2000,1,1,0,0,0,o).usec`, "5\n"},

		// --- C-style 10-argument gmtime form (sec, min, hour, mday, mon, year, …).
		{`p Time.utc(1,15,20,1,1,2000,:x,:x,:x,:x).to_a[0..5]`, "[1, 15, 20, 1, 1, 2000]\n"},
		{`p Time.local(1,15,20,1,1,2000,:x,:x,:x,:x).sec`, "1\n"},
		{`p Time.utc(1.0,15.0,20.0,1.0,1.0,2000.0,:x,:x,:x,:x).to_a[0..5]`, "[1, 15, 20, 1, 1, 2000]\n"},
		// An 8th normal-form argument is ignored.
		{`p Time.utc(2000,1,1,0,0,0,500000,777).usec`, "500000\n"},

		// --- utc_offset resolution (Integer / Float / String / letter / coercion).
		{`p Time.new(2000,1,1,0,0,0,123).utc_offset`, "123\n"},
		{`p Time.new(2000,1,1,0,0,0,3600.0).utc_offset`, "3600\n"},
		{`p Time.new(2000,1,1,0,0,0,"+05:30").utc_offset`, "19800\n"},
		{`p Time.new(2000,1,1,0,0,0,"-04:10").utc_offset`, "-15000\n"},
		{`p Time.new(2000,1,1,0,0,0,"+05:30:37").utc_offset`, "19837\n"},
		{`p Time.new(2000,1,1,0,0,0,"+05").utc_offset`, "18000\n"},
		{`p Time.new(2000,1,1,0,0,0,"+0530").utc_offset`, "19800\n"},
		{`p Time.new(2000,1,1,0,0,0,"+053037").utc_offset`, "19837\n"},
		{`p Time.new(2000,1,1,0,0,0,"UTC").utc_offset`, "0\n"},
		{`p Time.new(2000,1,1,0,0,0,"B").utc_offset`, "7200\n"}, // military letter
		{`p Time.new(2000,1,1,0,0,0,"Y").utc_offset`, "-43200\n"},
		{`o=Object.new; def o.to_int; 123; end; p Time.new(2000,1,1,0,0,0,o).utc_offset`, "123\n"},
		{`o=Object.new; def o.to_str; "+05:30"; end; p Time.new(2000,1,1,0,0,0,o).utc_offset`, "19800\n"},
		{`p Time.new(2000,1,1,0,0,0,86400-1).utc_offset`, "86399\n"},
		{`p Time.new(2000,1,1,0,0,0,-86400+1).utc_offset`, "-86399\n"},
		{`p Time.new(2000,1,1,0,0,0,"+09:00").zone`, "nil\n"},

		// --- The String-instant form (Ruby 3.2+).
		{`p Time.new("2020-12-24T15:56:17Z").to_i`, "1608825377\n"},
		{`p Time.new("2020-12-25 00:56:17 +09:00").to_i`, "1608825377\n"},
		{`p Time.new("2020-12-25 00:57:47 +09:01:30").to_i`, "1608825377\n"},
		{`p Time.new("2020-12-25 00:56:17 +0900").to_i`, "1608825377\n"},
		{`p Time.new("2020-12-25T00:56:17+09:00").to_i`, "1608825377\n"},
		{`p Time.new("2020-12-25T00:56:17.123456+09:00").nsec`, "123456000\n"},
		{`p Time.new("2020-12-25T00:56:17.123 +09:00").nsec`, "123000000\n"},
		{`p Time.new("2020-12-25T00:56:17.123456789876 +09:00").nsec`, "123456789\n"}, // truncated to 9
		{`p Time.new("2021").to_a[0..5]`, "[0, 0, 0, 1, 1, 2021]\n"},
		{`puts Time.new("2021-12-25 00:00:00 +05:00")`, "2021-12-25 00:00:00 +0500\n"},
		// The String's own offset wins over the in: keyword.
		{`puts Time.new("2021-12-25 00:00:00 +09:00", in: "-01:00")`, "2021-12-25 00:00:00 +0900\n"},
		{`puts Time.new("2021-12-25 00:00:00", in: "-01:00")`, "2021-12-25 00:00:00 -0100\n"},
		{`puts Time.new("2021", in: "+17:00")`, "2021-01-01 00:00:00 +1700\n"},
		// precision: truncates the sub-second part.
		{`p Time.new("2021-12-25 00:00:00.123456789876 +09:00", precision: 3).subsec`, "(123/1000)\n"},
		{`p Time.new("2021-12-25 00:00:00 +09:00", precision: 0).subsec`, "0\n"},
		{`o=Object.new; def o.to_int; 3; end; p Time.new("2021-12-25 00:00:00.123456789876 +09:00", precision: o).subsec`, "(123/1000)\n"},
		{`p Time.new("2021-12-25 00:00:00.123456789876 +09:00", precision: 1.2).subsec`, "(1/10)\n"},

		// --- Timezone object (Time.new via #local_to_utc).
		{tzClass + `z = TZ.new(5*3600+30*60); p Time.new(2000,1,1,12,0,0,z).utc_offset`, "19800\n"},
		{tzClass + `z = TZ.new(5*3600+30*60); t = Time.new(2000,1,1,12,0,0,z); p [t.wday, t.yday]`, "[6, 1]\n"},
		{tzClass + `z = TZ.new(5*3600+30*60); t = Time.new(2000,1,1,12,0,0,z); p t.zone.equal?(z)`, "true\n"},
		{tzClass + `z = TZ.new(19800, "MMT"); p Time.new(2000,1,1,12,0,0,z).strftime("%Z")`, "\"MMT\"\n"},
		{tzClass + `z = TZ.new(5*3600+30*60); t = Time.new(2000,1,1,12,0,0, in: z); p [t.utc_offset, t.zone.equal?(z)]`, "[19800, true]\n"},
		// #local_to_utc may return a Time, an Integer or any object answering #to_i.
		{`z=Object.new; def z.local_to_utc(t); Time.utc(t.year,t.mon,t.day,t.hour,t.min,t.sec) - 3600; end; p Time.new(2000,1,1,12,0,0,z).utc_offset`, "3600\n"},
		{`z=Object.new; def z.local_to_utc(t); o=Object.new; e=t.to_i-3600; o.define_singleton_method(:to_i){e}; o; end; p Time.new(2000,1,1,12,0,0,z).utc_offset`, "3600\n"},
		{`z=Object.new; def z.local_to_utc(t); t; end; p Time.new(2000,1,1,12,0,0,z).is_a?(Time)`, "true\n"},

		// --- Timezone object (Time.at / Time.now via #utc_to_local).
		{tzClass + `z = TZ.new(3600); t = Time.at(1234567890, in: z); p [t.utc_offset, t.to_i]`, "[3600, 1234567890]\n"},
		{`z=Object.new; def z.utc_to_local(t); t.to_i + 3600; end; p Time.at(1000, in: z).utc_offset`, "3600\n"},

		// --- Time.at exactness and coercion.
		{`p Time.at(Rational(1486570508539759, 1000000)).usec`, "539759\n"},
		{`p Time.at(Rational(1486570508539759123, 1000000000)).nsec`, "539759123\n"},
		{`o=Object.new; def o.to_int; 0; end; p Time.at(o) == Time.at(0)`, "true\n"},
		{`o=Object.new; def o.to_int; 10; end; p Time.at(0, o).tv_usec`, "10\n"},
		{`p Time.at(10, 500.500).tv_nsec`, "500500\n"},
		{`p Time.at(0, 123456789, :nanosecond).nsec`, "123456789\n"},

		// --- gm is the very same method object as utc; getgm aliases getutc.
		{`p Time.method(:gm) == Time.method(:utc)`, "true\n"},
		{`p Time.utc(2000,1,1,12).getgm.utc?`, "true\n"},

		// --- A Numeric subclass answering #to_r is coerced everywhere num_exact is.
		{numClass + `p Time.at(N.new).nsec`, "500000000\n"},                                                                  // timeAtSeconds via #to_r
		{numClass + `p Time.at(0, N.new).nsec`, "2500\n"},                                                                    // numExactFloat via #to_r
		{numClass + `p Time.utc(2000,1,1,0,0,N.new).usec`, "500000\n"},                                                       // timeSecParts via #to_r
		{`class NO < Numeric; def to_r; Rational(3600,1); end; end; p Time.new(2000,1,1,0,0,0,NO.new).utc_offset`, "3600\n"}, // newTimeOffset via #to_r
		{`p Time.new(2000,1,1,0,0,0,Rational(3600,1)).utc_offset`, "3600\n"},                                                 // Rational offset
		// A no-offset String instant falls back to the local zone (assert year only).
		{`p Time.new("2021-12-25 00:00:00").year`, "2021\n"},
		// precision: nil keeps up to nanosecond resolution; a value above 9 is clamped.
		{`p Time.new("2021-12-25 00:00:00.123456789 +09:00", precision: nil).nsec`, "123456789\n"},
		{`p Time.new("2021-12-25 00:00:00.123456789876 +09:00", precision: 20).nsec`, "123456789\n"},
		// A Time.at Time source rendered in a fixed offset keeps its instant.
		{`p Time.at(Time.utc(2000,1,1,0,0,0), in: "+05:00").utc_offset`, "18000\n"},
		// Time.now / Time.new(in:) with a timezone object (offset is deterministic).
		{`class TZN; def utc_to_local(t); t + 3600; end; end; p Time.now(in: TZN.new).utc_offset`, "3600\n"},
		{`class TZN; def utc_to_local(t); t + 3600; end; end; p Time.new(in: TZN.new).utc_offset`, "3600\n"},
		// #local_to_utc returning a bare Integer / an object answering #to_i.
		{`z=Object.new; def z.local_to_utc(t); (t.to_i - 3600); end; p Time.new(2000,1,1,12,0,0,z).utc_offset`, "3600\n"},
		// Bare Time.new is the current instant; a nil 7th positional is no zone.
		{`p Time.new.is_a?(Time)`, "true\n"},
		{`p Time.new(2000,1,1,0,0,0,nil).year`, "2000\n"},
		// Signed String calendar fields (Kernel#Integer semantics).
		{`p Time.utc(2000, "+3").mon`, "3\n"},
		{`p Time.utc("-44").year`, "-44\n"},
		// Time#+ (via send, off the operator fast path) shifts by a Rational.
		{`p Time.utc(2000,1,1,0,0,0).send(:+, Rational(3,2)).nsec`, "500000000\n"},
	} {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestTimeNewConformanceErrors covers the ArgumentError / TypeError paths the
// MRI 4.0.6 constructors raise, asserted on the exact message where the spec
// fixes it and on the class otherwise.
func TestTimeNewConformanceErrors(t *testing.T) {
	// Message-exact ArgumentErrors, checked via a rescue that prints the message.
	for _, c := range []struct{ src, want string }{
		// Two-tier range messages: negative → "argument out of range".
		{`Time.utc(2008,12,31,23,59,-1)`, "argument out of range"},
		{`Time.utc(2008,12,31,23,59,61)`, "sec out of range"},
		{`Time.utc(2008,16,31)`, "mon out of range"},
		{`Time.utc(2008,12,32)`, "mday out of range"},
		{`Time.utc(2008,12,31,25)`, "hour out of range"},
		{`Time.utc(2008,12,31,23,61)`, "min out of range"},
		// utc_offset out of range vs bad format.
		{`Time.new(2000,1,1,0,0,0,86400)`, "utc_offset out of range"},
		{`Time.new(2000,1,1,0,0,0,-86400)`, "utc_offset out of range"},
		{`Time.new(2000,1,1,0,0,0,10**20)`, "utc_offset out of range"},
		{`Time.new("2020-12-25 00:56:17 +24:00")`, "utc_offset out of range"},
		{`Time.new("2020-12-25 00:56:17 +00:60")`, `expected for utc_offset: +00:60`},
		{`Time.new(2000,1,1,0,0,0,"3600")`, `expected for utc_offset`},
		{`Time.new(2000,1,1,0,0,0,"J")`, `expected for utc_offset: J`},
		// Two zone arguments.
		{`Time.new(2000,1,1,12,0,0,"+05:00", in: "+05:00")`, "timezone argument given as positional and keyword arguments"},
		// A too-many-positional Time.new.
		{`Time.new(0,0,0,0,0,0,0,0)`, "wrong number of arguments (given 8, expected 0..7)"},
		// The String-instant form: unparseable input, out-of-range fields, encoding.
		{`Time.new("a\nb")`, `can't parse: "a\nb"`},
		{`Time.new(" 2020-12-02 00:00:00")`, "can't parse"},
		{`Time.new("2020-12-02 00:00:00 ")`, "can't parse"},
		{`Time.new("2020-012-25 00:56:17 +0900")`, "can't parse"},
		{`Time.new("2020-12-25 00:56:17. +0900")`, "can't parse"},
		{`Time.new("2020-12-25")`, "can't parse"},
		{`Time.new("2020-13-25 00:56:17 +09:00")`, "mon out of range"},
		{`Time.new("2020-12-25 00:56:61 +09:00")`, "sec out of range"},
		{`Time.new("2021-11-31 00:00:60 +09:00".encode("utf-32le"))`, "time string should have ASCII compatible encoding"},
		// The C-style form arity, and microseconds out of range.
		{`Time.utc(1,2,3,4,5,6,7,8,9)`, "wrong number of arguments (given 9, expected 1..8)"},
		{`Time.utc(2000,1,1,0,0,0,1000000)`, "subsecx out of range"},
		// Timezone-object difference too large; and a Bignum result epoch is likewise
		// out of range.
		{`z=Object.new; def z.local_to_utc(t); Time.utc(t.year,t.mon,t.day+1,t.hour,t.min,t.sec); end; Time.new(2000,1,1,12,0,0,z)`, "utc_offset out of range"},
		{`z=Object.new; def z.local_to_utc(t); 10**20; end; Time.new(2000,1,1,12,0,0,z)`, "utc_offset out of range"},
		{`z=Object.new; def z.local_to_utc(t); o=Object.new; def o.to_i; 10**20; end; o; end; Time.new(2000,1,1,12,0,0,z)`, "utc_offset out of range"},
		// A non-numeral String field, or a bare sign, is an Integer() error.
		{`Time.utc(2000, "zzz")`, "invalid value for Integer()"},
		{`Time.utc(2000, "+")`, "invalid value for Integer()"},
	} {
		got := eval(t, "begin\n"+c.src+"\nrescue => e\n  puts e.message\nend")
		if !strings.Contains(got, c.want) {
			t.Errorf("src=%q\n got=%q\nwant containing %q", c.src, got, c.want)
		}
	}

	// TypeError paths (class only).
	for _, c := range []struct{ src, cls string }{
		{`Time.new(2000,1,1,0,0,0, Object.new)`, "TypeError"},                    // not offset, not a zone
		{`Time.new("2021-12-25 00:00:00.1 +09:00", precision: "")`, "TypeError"}, // String precision
		{`Time.at("0")`, "TypeError"},                                            // String source
		{`Time.at(nil)`, "TypeError"},                                            // nil source
		{`Time.at(0, nil)`, "TypeError"},                                         // nil subsec
		{`Time.at(0, "0")`, "TypeError"},                                         // String subsec
		{`Time.at(Time.now, 500000)`, "TypeError"},                               // Time source with subsec
		{`Time.new(2000, {})`, "TypeError"},                                      // trailing non-kwargs hash → month coercion fails
		{`Time.utc(2000,1,1,0,0, Object.new)`, "TypeError"},                      // seconds: not numeric / String / #to_int / #to_r
		{`Time.utc(2000,1,1,0,0,0).send(:+, "x")`, "TypeError"},                  // Time#+ non-numeric
		// #local_to_utc result whose #to_i is not an Integer (internal guard).
		{`z=Object.new; def z.local_to_utc(t); o=Object.new; def o.to_i; "x"; end; o; end; Time.new(2000,1,1,12,0,0,z)`, "TypeError"},
	} {
		err := runErr(t, c.src)
		if err == nil || !strings.Contains(err.Error(), c.cls) {
			t.Errorf("src=%q: got err=%v, want %s", c.src, err, c.cls)
		}
	}
}
