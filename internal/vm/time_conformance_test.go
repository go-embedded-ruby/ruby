// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// TestTimeConstructors covers Time.new / Time.utc / Time.gm / Time.local /
// Time.mktime / Time.at across their argument forms (parts, sub-second, unit,
// the in: keyword and a fixed-offset zone), asserted against MRI 4.0 on fixed
// instants so the suite is independent of the wall clock and machine TZ.
func TestTimeConstructors(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		// Time.utc / Time.gm — a whole UTC instant and its parts.
		{`p Time.utc(2026,6,21,12,34,56).to_i`, "1782045296\n"},
		{`p Time.gm(2026,6,21,12,34,56).to_i`, "1782045296\n"},
		{`puts Time.utc(2026,6,21,12,34,56)`, "2026-06-21 12:34:56 +0000\n"},
		// Defaulted trailing parts.
		{`puts Time.utc(2026)`, "2026-01-01 00:00:00 +0000\n"},
		{`puts Time.utc(2026,6)`, "2026-06-01 00:00:00 +0000\n"},
		{`puts Time.utc(2026,6,21)`, "2026-06-21 00:00:00 +0000\n"},
		{`puts Time.utc(2026,6,21,12)`, "2026-06-21 12:00:00 +0000\n"},
		{`puts Time.utc(2026,6,21,12,34)`, "2026-06-21 12:34:00 +0000\n"},
		// 7th arg to utc/local is microseconds (Float sub-usec allowed).
		{`p Time.utc(2026,6,21,12,34,56,789012.5).nsec`, "789012500\n"},
		{`p Time.utc(2026,6,21,12,34,56,500).usec`, "500\n"},
		// Float / truncating calendar parts (MRI truncates).
		{`p Time.utc(2026,6.9,1).mon`, "6\n"},
		{`p Time.utc(2026,6,1,5.7).hour`, "5\n"},
		// Overflow normalises the way MRI (and Go) do.
		{`puts Time.utc(2026,2,30)`, "2026-03-02 00:00:00 +0000\n"},
		{`puts Time.utc(2026,1,1,24)`, "2026-01-02 00:00:00 +0000\n"},
		{`puts Time.utc(2026,1,1,0,0,60)`, "2026-01-01 00:01:00 +0000\n"},
		// Time.local / Time.mktime — local zone (assert only TZ-independent facts).
		{`p Time.local(2026,6,21).year`, "2026\n"},
		{`p Time.mktime(2026,6,21).mon`, "6\n"},
		{`p Time.local(2026,1,1).utc?`, "false\n"},
		{`p Time.local(2026,1,1,0,0,0,500).usec`, "500\n"},
		// Time.new — parts, local default, and a fixed-offset zone string (7th arg).
		{`p Time.new(2026,1,1).year`, "2026\n"},
		{`p Time.new(2026,6,21,12,0,0,"+09:00").utc_offset`, "32400\n"},
		{`puts Time.new(2026,6,21,12,0,0,"+09:00")`, "2026-06-21 12:00:00 +0900\n"},
		{`p Time.new(2026,6,21,12,0,0,"+09:00").zone`, "nil\n"},
		// Time.new/at/now with the 4.0 in: keyword.
		{`puts Time.new(2026,6,21,12,0,0, in: "-03:00")`, "2026-06-21 12:00:00 -0300\n"},
		{`puts Time.at(0, in: "+02:00")`, "1970-01-01 02:00:00 +0200\n"},
		{`p Time.now(in: "+02:00").utc_offset`, "7200\n"},
		{`p Time.new(in: "+05:00").utc_offset`, "18000\n"}, // Time.new now, zoned
		// Time.at — Integer / Float / Rational / Time, with subsec + unit.
		{`p Time.at(1000).to_i`, "1000\n"},
		{`p Time.at(1.5).subsec`, "(1/2)\n"},
		{`p Time.at(Rational(3,2)).subsec`, "(1/2)\n"},
		{`p Time.at(1782045296, 500000, :microsecond).nsec`, "500000000\n"},
		{`p Time.at(0, 500000).nsec`, "500000000\n"}, // default unit = microsecond
		{`p Time.at(0, 250, :millisecond).nsec`, "250000000\n"},
		{`p Time.at(0, 5, :nanosecond).nsec`, "5\n"},
		{`p Time.at(0, 1, :usec).nsec`, "1000\n"},
		{`p Time.at(0, 7, :nsec).nsec`, "7\n"},
		{`p Time.at(Time.utc(2020,1,1,0,0,0,123456)).usec`, "123456\n"},
		{`p Time.at(-1.5).nsec`, "500000000\n"},
	} {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestTimeParts covers the field accessors, sub-second parts, zone queries and
// the boolean predicates on a fixed instant with a known sub-second fraction.
func TestTimeParts(t *testing.T) {
	base := `t = Time.utc(2026,6,21,12,34,56,789012.5); `
	for _, c := range []struct{ src, want string }{
		{base + `p t.year`, "2026\n"},
		{base + `p t.month`, "6\n"},
		{base + `p t.mon`, "6\n"},
		{base + `p t.day`, "21\n"},
		{base + `p t.mday`, "21\n"},
		{base + `p t.hour`, "12\n"},
		{base + `p t.min`, "34\n"},
		{base + `p t.sec`, "56\n"},
		{base + `p t.usec`, "789012\n"},
		{base + `p t.nsec`, "789012500\n"},
		{base + `p t.subsec`, "(63121/80000)\n"},
		{base + `p t.tv_sec`, "1782045296\n"},
		{base + `p t.tv_usec`, "789012\n"},
		{base + `p t.tv_nsec`, "789012500\n"},
		{base + `p t.yday`, "172\n"},
		{base + `p t.wday`, "0\n"},
		{base + `p t.zone`, "\"UTC\"\n"},
		{base + `p t.utc_offset`, "0\n"},
		{base + `p t.gmt_offset`, "0\n"},
		{base + `p t.gmtoff`, "0\n"},
		{base + `p t.utc?`, "true\n"},
		{base + `p t.gmt?`, "true\n"},
		{base + `p t.dst?`, "false\n"},
		{base + `p t.isdst`, "false\n"},
		{base + `p t.to_i`, "1782045296\n"},
		{base + `p t.to_r`, "(142563623743121/80000)\n"},
		// subsec of a whole second is Integer 0, not a Rational.
		{`p Time.utc(2026,1,1).subsec`, "0\n"},
		{`p Time.utc(2026,1,1).to_r`, "(1767225600/1)\n"},
		// to_a → [sec,min,hour,mday,mon,year,wday,yday,isdst,zone].
		{`p Time.utc(2026,6,21,12,34,56).to_a`, "[56, 34, 12, 21, 6, 2026, 0, 172, false, \"UTC\"]\n"},
		// A fixed-offset Time reports nil zone and its offset.
		{`p Time.new(2026,1,1,0,0,0,"-05:30").utc_offset`, "-19800\n"},
		{`p Time.new(2026,1,1,0,0,0,"-05:30").zone`, "nil\n"},
	} {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestTimeConversions covers utc/gmtime/localtime mutation, getutc/getlocal,
// round/floor/ceil, arithmetic and ordering / equality on fixed instants.
func TestTimeConversions(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		// utc / gmtime mutate the receiver and return it (MRI in-place semantics).
		{`t = Time.new(2026,1,1,0,0,0,"+09:00"); t.utc; p t.utc?`, "true\n"},
		{`t = Time.new(2026,1,1,0,0,0,"+09:00"); t.gmtime; p t.hour`, "15\n"},
		// getutc / getlocal return a new Time, leaving the receiver alone.
		{`t = Time.new(2026,1,1,0,0,0,"+09:00"); u = t.getutc; p [t.utc?, u.utc?]`, "[false, true]\n"},
		{`puts Time.utc(2026,6,21,12,34,56).getlocal("+05:30")`, "2026-06-21 18:04:56 +0530\n"},
		{`p Time.utc(2026,1,1).localtime.utc?`, "false\n"},
		{`p Time.utc(2026,1,1).getlocal.utc?`, "false\n"},
		// round / floor / ceil.
		{`p Time.at(0,500001,:microsecond).round.to_i`, "1\n"},
		{`p Time.at(0,400000,:microsecond).round.to_i`, "0\n"},
		{`p Time.utc(2026,1,1,0,0,0,789012.5).floor(3).subsec`, "(789/1000)\n"},
		{`p Time.utc(2026,1,1,0,0,0,789012.5).ceil(3).subsec`, "(79/100)\n"},
		{`p Time.utc(2026,1,1,0,0,0,500000).floor.subsec`, "0\n"},
		{`p Time.utc(2026,1,1,0,0,0,500000).ceil.sec`, "1\n"},
		{`p Time.at(0,123456789,:nanosecond).round(9).nsec`, "123456789\n"},
		{`p Time.at(0,123456789,:nanosecond).floor(12).nsec`, "123456789\n"},
		// Arithmetic: fractional add keeps the fraction; Time − Time is a Float.
		{`p((Time.at(1000) + 60).to_i)`, "1060\n"},
		{`p (Time.utc(2026,1,1) + 0.5).subsec`, "(1/2)\n"},
		{`p(Time.utc(2026,1,1,0,0,1) - Time.utc(2026,1,1))`, "1.0\n"},
		// Ordering + Comparable-style operators, equality, eql?, hash.
		{`p(Time.at(1000) <=> Time.at(2000))`, "-1\n"},
		{`p(Time.at(1000) <=> 5)`, "nil\n"},
		{`p(Time.at(1000) < Time.at(2000))`, "true\n"},
		{`p(Time.at(2000) > Time.at(1000))`, "true\n"},
		{`p(Time.at(1000) <= Time.at(1000))`, "true\n"},
		{`p(Time.at(1000) >= Time.at(1000))`, "true\n"},
		{`p(Time.utc(2026,1,1) == Time.utc(2026,1,1))`, "true\n"},
		{`p Time.utc(2026,1,1).eql?(Time.utc(2026,1,1))`, "true\n"},
		{`p Time.utc(2026,1,1).eql?(Time.utc(2026,1,2))`, "false\n"},
		{`p(Time.utc(2026,1,1).hash == Time.utc(2026,1,1).hash)`, "true\n"},
	} {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestTimeStrftime covers the strftime directive set, flags, width and the
// compound directives, plus inspect's sub-second rendering (MRI 4.0).
func TestTimeStrftime(t *testing.T) {
	base := `t = Time.utc(2026,6,21,12,34,56); `
	for _, c := range []struct{ src, want string }{
		{base + `puts t.strftime("%Y-%m-%d %H:%M:%S")`, "2026-06-21 12:34:56\n"},
		{base + `puts t.strftime("%C|%y|%G|%V|%U|%W")`, "20|26|2026|25|25|24\n"},
		{base + `puts t.strftime("%A|%a|%B|%b|%h")`, "Sunday|Sun|June|Jun|Jun\n"},
		{base + `puts t.strftime("%p|%P|%u|%w|%j|%s")`, "PM|pm|7|0|172|1782045296\n"},
		{base + `puts t.strftime("%I|%l|%k|%e")`, "12|12|12|21\n"},
		{base + `puts t.strftime("%z|%:z|%::z|%Z")`, "+0000|+00:00|+00:00:00|UTC\n"},
		{base + `puts t.strftime("%L|%3N|%6N|%9N|%12N")`, "000|000|000000|000000000|000000000000\n"},
		{base + `puts t.strftime("%n|%t|%%")`, "\n|\t|%\n"},
		// Compound directives.
		{base + `puts t.strftime("%c")`, "Sun Jun 21 12:34:56 2026\n"},
		{base + `puts t.strftime("%D|%x")`, "06/21/26|06/21/26\n"},
		{base + `puts t.strftime("%F")`, "2026-06-21\n"},
		{base + `puts t.strftime("%R|%T|%X")`, "12:34|12:34:56|12:34:56\n"},
		{base + `puts t.strftime("%r")`, "12:34:56 PM\n"},
		// Flags + width.
		{base + `puts t.strftime("%-m|%_3m|%05Y|%^a|%#p|%-d")`, "6|  6|02026|SUN|pm|21\n"},
		{base + `puts t.strftime("%#Y")`, "2026\n"},            // '#' over digits is a no-op
		{`puts Time.utc(2026,1,1,13).strftime("%#P")`, "PM\n"}, // '#' upcases pm → PM
		{`puts Time.utc(2026,6,22).strftime("%u")`, "1\n"},     // %u Monday = 1 (non-Sunday)
		// Unknown directive + trailing-% edge cases pass through verbatim.
		{base + `puts t.strftime("%Q")`, "%Q\n"},
		{base + `puts t.strftime("done %")`, "done %\n"},
		{base + `puts t.strftime("x%-")`, "x%-\n"},
		// %Z on a fixed-offset zone renders the numeric offset; %z sign both ways.
		{`puts Time.new(2026,1,1,0,0,0,"+09:00").strftime("%Z")`, "+09:00\n"},
		{`puts Time.new(2026,1,1,0,0,0,"-05:30").strftime("%z|%:z|%::z")`, "-0530|-05:30|-05:30:00\n"},
		// hour12 wrap: midnight and noon both render 12.
		{`puts Time.utc(2026,1,1,0,0,0).strftime("%I %p")`, "12 AM\n"},
		{`puts Time.utc(2026,1,1,13,0,0).strftime("%I %l %P")`, "01  1 pm\n"},
		// inspect includes the sub-second fraction (trailing zeros trimmed); to_s
		// does not.
		{base + `p t.inspect`, "\"2026-06-21 12:34:56 +0000\"\n"},
		{`p Time.utc(2026,6,21,12,34,56,789012.5).inspect`, "\"2026-06-21 12:34:56.7890125 +0000\"\n"},
		{`p Time.utc(2026,6,21,12,34,56,789012.5).to_s`, "\"2026-06-21 12:34:56 +0000\"\n"},
		// ctime / asctime.
		{base + `puts t.ctime`, "Sun Jun 21 12:34:56 2026\n"},
		{base + `puts t.asctime`, "Sun Jun 21 12:34:56 2026\n"},
	} {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestTimeConformanceErrors covers the raising paths of the new surface: field
// range validation, the offset parser, the unit symbol and numeric coercion.
func TestTimeConformanceErrors(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{`Time.utc(2026,13,1)`, "ArgumentError"},   // mon out of range
		{`Time.utc(2026,1,32)`, "ArgumentError"},   // mday out of range
		{`Time.utc(2026,1,1,25)`, "ArgumentError"}, // hour out of range
		{`Time.utc(2026,1,1,0,60)`, "ArgumentError"},
		{`Time.utc(2026,1,1,0,0,61)`, "ArgumentError"},
		{`Time.new(2026,1,1,0,0,0,"bogus")`, "ArgumentError"}, // bad offset (no sign)
		{`Time.new(2026,1,1,0,0,0,"+1")`, "ArgumentError"},    // bad offset (length)
		{`Time.new(2026,1,1,0,0,0,"+ab")`, "ArgumentError"},   // bad offset (non-digit)
		{`Time.at(0, 1, :furlong)`, "ArgumentError"},          // bad unit
		{`Time.at()`, "ArgumentError"},                        // no positional argument
		{`Time.at("x")`, "TypeError"},                         // non-numeric
		{`Time.new("x")`, "TypeError"},                        // non-integer year
		{`Time.at(0) + "x"`, "TypeError"},                     // + non-numeric
	} {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got=%v want %q", c.src, err, c.want)
		}
	}
}

// TestTimeZoneOffsetForms covers parseZone's accepted offset spellings (":"-free
// and ":"-separated, hour-only and hour:min:sec) and the UTC / Z aliases.
func TestTimeZoneOffsetForms(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{`p Time.new(2026,1,1,0,0,0,"+09").utc_offset`, "32400\n"},       // +HH
		{`p Time.new(2026,1,1,0,0,0,"+0930").utc_offset`, "34200\n"},     // +HHMM
		{`p Time.new(2026,1,1,0,0,0,"+09:30").utc_offset`, "34200\n"},    // +HH:MM
		{`p Time.new(2026,1,1,0,0,0,"+093015").utc_offset`, "34215\n"},   // +HHMMSS
		{`p Time.new(2026,1,1,0,0,0,"+09:30:15").utc_offset`, "34215\n"}, // +HH:MM:SS
		{`p Time.new(2026,1,1,0,0,0,"UTC").utc?`, "true\n"},
		{`p Time.new(2026,1,1,0,0,0,"Z").utc?`, "true\n"},
	} {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}
