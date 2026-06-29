package vm_test

import (
	"strings"
	"testing"
)

// TestDate covers Ruby Date and DateTime, now backed by
// github.com/go-ruby-date/date — an MRI-4.0.5-byte-exact Date / DateTime. Every
// value is asserted against MRI's own output (`ruby -rdate -e`). The cases that
// changed from the former go-composites-backed binding are the MRI-faithful
// upgrades: #inspect is MRI's "#<Date: ... ((...))>" form (not the bare ISO
// string), Jan-31 >> 1 is 2026-02-28 (MRI's last-valid-day normalisation, not the
// composite's 2026-03-03 overflow), and Date.parse accepts MRI's heuristic
// formats (e.g. "2026/06/21"). 2026-06-29 is a Monday, anchoring the weekday and
// week-date checks.
func TestDate(t *testing.T) {
	cases := []struct{ src, want string }{
		// Construction + to_s / inspect (the MRI #<Date: ...> inspect form).
		{`puts Date.new(2026, 6, 29)`, "2026-06-29\n"},
		{`p Date.new(2026, 6, 29).to_s`, "\"2026-06-29\"\n"},
		{`p Date.new(2026, 6, 29).inspect`, "\"#<Date: 2026-06-29 ((2461221j,0s,0n),+0s,2299161j)>\"\n"},
		{`p [Date.new(2026, 6, 29)]`, "[#<Date: 2026-06-29 ((2461221j,0s,0n),+0s,2299161j)>]\n"},
		{`p Date.civil(2026, 6, 29).to_s`, "\"2026-06-29\"\n"}, // civil is the alias for new
		{`p Date.new(2024, 2, 29).to_s`, "\"2024-02-29\"\n"},   // leap day is valid
		{`p Date.new.to_s`, "\"-4712-01-01\"\n"},               // no args → the MRI epoch (jd 0)
		// jd / ordinal / commercial constructors (new to the library binding).
		{`p Date.jd(2461221).to_s`, "\"2026-06-29\"\n"},
		{`p Date.ordinal(2026, 180).to_s`, "\"2026-06-29\"\n"},
		{`p Date.commercial(2026, 27, 1).to_s`, "\"2026-06-29\"\n"},
		// Field accessors.
		{`p Date.new(2026, 6, 29).year`, "2026\n"},
		{`p Date.new(2026, 6, 29).month`, "6\n"},
		{`p Date.new(2026, 6, 29).mon`, "6\n"},
		{`p Date.new(2026, 6, 29).day`, "29\n"},
		{`p Date.new(2026, 6, 29).mday`, "29\n"},
		{`p Date.new(2026, 6, 29).jd`, "2461221\n"},
		{`p Date.new(2026, 6, 29).mjd`, "61220\n"},
		// Day-of-week and ISO week date: 2026-06-29 is a Monday (wday 1 / cwday 1),
		// in ISO week 27 of commercial year 2026; 2026-06-28 is a Sunday (wday 0 /
		// cwday 7).
		{`p Date.new(2026, 6, 29).wday`, "1\n"},
		{`p Date.new(2026, 6, 29).cwday`, "1\n"},
		{`p Date.new(2026, 6, 28).wday`, "0\n"},
		{`p Date.new(2026, 6, 28).cwday`, "7\n"},
		{`p Date.new(2026, 6, 29).cweek`, "27\n"},
		{`p Date.new(2026, 6, 29).cwyear`, "2026\n"},
		// strftime (the headline upgrade — the former binding had none).
		{`p Date.new(2026, 6, 29).strftime("%A %Y-%m-%d")`, "\"Monday 2026-06-29\"\n"},
		{`p Date.new(2026, 6, 29).strftime("%A, %B %-d, %Y")`, "\"Monday, June 29, 2026\"\n"},
		// Named string formats.
		{`p Date.new(2026, 6, 29).iso8601`, "\"2026-06-29\"\n"},
		{`p Date.new(2026, 6, 29).rfc3339`, "\"2026-06-29T00:00:00+00:00\"\n"},
		{`p Date.new(2026, 6, 29).rfc2822`, "\"Mon, 29 Jun 2026 00:00:00 +0000\"\n"},
		{`p Date.new(2026, 6, 29).rfc822`, "\"Mon, 29 Jun 2026 00:00:00 +0000\"\n"},
		{`p Date.new(2026, 6, 29).httpdate`, "\"Mon, 29 Jun 2026 00:00:00 GMT\"\n"},
		{`p Date.new(2026, 6, 29).ctime`, "\"Mon Jun 29 00:00:00 2026\"\n"},
		{`p Date.new(2026, 6, 29).asctime`, "\"Mon Jun 29 00:00:00 2026\"\n"},
		{`p Date.new(2026, 6, 29).jisx0301`, "\"R08.06.29\"\n"},
		// + days / - days, and Date - Date = day count between.
		{`puts (Date.new(2026, 6, 29) + 10).to_s`, "2026-07-09\n"},
		{`puts (Date.new(2026, 6, 29) - 10).to_s`, "2026-06-19\n"},
		{`p(Date.new(2026, 7, 9) - Date.new(2026, 6, 29))`, "10\n"},
		{`p(Date.new(2026, 6, 29) - Date.new(2026, 7, 9))`, "-10\n"},    // negative span
		{`puts Date.new(2026, 6, 29).send(:+, 1).to_s`, "2026-06-30\n"}, // method send agrees
		{`puts Date.new(2026, 6, 29).send(:-, 1).to_s`, "2026-06-28\n"},
		// next / succ (the following day).
		{`puts Date.new(2026, 6, 29).next.to_s`, "2026-06-30\n"},
		{`puts Date.new(2026, 6, 29).succ.to_s`, "2026-06-30\n"},
		// next_day / prev_day (default 1, explicit n).
		{`puts Date.new(2026, 6, 29).next_day.to_s`, "2026-06-30\n"},
		{`puts Date.new(2026, 6, 29).next_day(5).to_s`, "2026-07-04\n"},
		{`puts Date.new(2026, 6, 29).prev_day.to_s`, "2026-06-28\n"},
		{`puts Date.new(2026, 6, 29).prev_day(2).to_s`, "2026-06-27\n"},
		// next_year / prev_year.
		{`puts Date.new(2026, 6, 29).next_year.to_s`, "2027-06-29\n"},
		{`puts Date.new(2026, 6, 29).prev_year(2).to_s`, "2024-06-29\n"},
		// leap? : 2024 / 2000 are leap, 2026 / 1900 are not.
		{`p Date.new(2024, 1, 1).leap?`, "true\n"},
		{`p Date.new(2026, 1, 1).leap?`, "false\n"},
		{`p Date.new(2000, 1, 1).leap?`, "true\n"},
		{`p Date.new(1900, 1, 1).leap?`, "false\n"},
		// yday : 1-based day of year.
		{`p Date.new(2026, 1, 1).yday`, "1\n"},
		{`p Date.new(2026, 3, 15).yday`, "74\n"},
		{`p Date.new(2024, 12, 31).yday`, "366\n"},
		// next_month / prev_month, and the >> / << month-shift operators. MRI's
		// last-valid-day normalisation: Jan-31 >> 1 → Feb-28 (NOT Mar-03).
		{`puts Date.new(2026, 6, 29).next_month.to_s`, "2026-07-29\n"},
		{`puts Date.new(2026, 6, 29).next_month(3).to_s`, "2026-09-29\n"},
		{`puts Date.new(2026, 6, 29).prev_month.to_s`, "2026-05-29\n"},
		{`puts Date.new(2026, 6, 29).prev_month(2).to_s`, "2026-04-29\n"},
		{`puts Date.new(2026, 1, 31).next_month.to_s`, "2026-02-28\n"}, // overflow → last valid day
		{`puts (Date.new(2026, 1, 31) >> 1).to_s`, "2026-02-28\n"},
		{`puts (Date.new(2026, 6, 29) >> 6).to_s`, "2026-12-29\n"},
		{`puts (Date.new(2026, 6, 29) << 6).to_s`, "2025-12-29\n"},
		{`puts (Date.new(2026, 6, 29) >> -1).to_s`, "2026-05-29\n"}, // negative shift
		{`puts Date.new(2026, 6, 29).send(:">>", 1).to_s`, "2026-07-29\n"},
		{`puts Date.new(2026, 6, 29).send(:"<<", 1).to_s`, "2026-05-29\n"},
		// upto / downto / step block iterators (and their no-block Enumerator form).
		{`a=[]; Date.new(2026,1,1).upto(Date.new(2026,1,3)){|d| a<<d.to_s}; p a`,
			"[\"2026-01-01\", \"2026-01-02\", \"2026-01-03\"]\n"},
		{`b=[]; Date.new(2026,1,3).downto(Date.new(2026,1,1)){|d| b<<d.to_s}; p b`,
			"[\"2026-01-03\", \"2026-01-02\", \"2026-01-01\"]\n"},
		{`c=[]; Date.new(2026,1,1).step(Date.new(2026,1,7),2){|d| c<<d.to_s}; p c`,
			"[\"2026-01-01\", \"2026-01-03\", \"2026-01-05\", \"2026-01-07\"]\n"},
		{`s=[]; Date.new(2026,1,1).step(Date.new(2026,1,3)){|d| s<<d.to_s}; p s`, // default step 1
			"[\"2026-01-01\", \"2026-01-02\", \"2026-01-03\"]\n"},
		{`p Date.new(2026,1,1).upto(Date.new(2026,1,3)).class`, "Enumerator\n"},
		{`p Date.new(2026,1,3).downto(Date.new(2026,1,1)).class`, "Enumerator\n"},
		{`p Date.new(2026,1,1).step(Date.new(2026,1,3)).class`, "Enumerator\n"},
		// Parse (MRI's heuristic — accepts "/"-separated, the former ISO-only binding
		// rejected it).
		{`puts Date.parse("2026-06-29")`, "2026-06-29\n"},
		{`puts Date.parse("2026/06/29")`, "2026-06-29\n"},
		{`p Date.parse("2026-06-29").class`, "Date\n"},
		// strptime / _strptime.
		{`puts Date.strptime("06/29/2026", "%m/%d/%Y")`, "2026-06-29\n"},
		{`p Date.strptime("06/29/2026", "%m/%d/%Y").class`, "Date\n"},
		{`p Date._strptime("2026-06-29", "%Y-%m-%d")`, "{year: 2026, mon: 6, mday: 29}\n"},
		{`p Date._strptime("2026-06-29 12:30:45", "%Y-%m-%d %H:%M:%S")`,
			"{year: 2026, mon: 6, mday: 29, hour: 12, min: 30, sec: 45}\n"},
		{`p Date._strptime("nope", "%Y-%m-%d")`, "nil\n"}, // no match → nil
		// to_date (a plain Date is its own calendar date).
		{`p Date.new(2026, 6, 29).to_date.class`, "Date\n"},
		{`p Date.new(2026, 6, 29).to_date.to_s`, "\"2026-06-29\"\n"},
		// Ordering: <=> and the boolean operators.
		{`p(Date.new(2026, 6, 29) <=> Date.new(2026, 6, 30))`, "-1\n"},
		{`p(Date.new(2026, 6, 30) <=> Date.new(2026, 6, 29))`, "1\n"},
		{`p(Date.new(2026, 6, 29) <=> Date.new(2026, 6, 29))`, "0\n"},
		{`p(Date.new(2026, 6, 29) <=> 5)`, "nil\n"}, // non-Date operand
		{`p(Date.new(2026, 6, 29) < Date.new(2026, 12, 25))`, "true\n"},
		{`p(Date.new(2026, 12, 25) < Date.new(2026, 6, 29))`, "false\n"},
		{`p(Date.new(2026, 12, 25) > Date.new(2026, 6, 29))`, "true\n"},
		{`p(Date.new(2026, 6, 29) > Date.new(2026, 12, 25))`, "false\n"},
		{`p(Date.new(2026, 6, 29) <= Date.new(2026, 6, 29))`, "true\n"},
		{`p(Date.new(2026, 12, 25) <= Date.new(2026, 6, 29))`, "false\n"},
		{`p(Date.new(2026, 6, 29) >= Date.new(2026, 6, 29))`, "true\n"},
		{`p(Date.new(2026, 6, 29) >= Date.new(2026, 12, 25))`, "false\n"},
		// Equality (operator routes through valueEqual; the explicit == method too).
		{`p(Date.new(2026, 6, 29) == Date.new(2026, 6, 29))`, "true\n"},
		{`p(Date.new(2026, 6, 29) == Date.new(2026, 6, 30))`, "false\n"},
		{`p(Date.new(2026, 6, 29) == 5)`, "false\n"}, // non-Date operand
		{`p Date.new(2026, 6, 29).send(:==, Date.new(2026, 6, 29))`, "true\n"},
		{`p Date.new(2026, 6, 29).send(:==, Date.new(2026, 6, 30))`, "false\n"},
		{`p Date.new(2026, 6, 29).send(:==, 42)`, "false\n"}, // method, non-Date
		// truthiness + class.
		{`p(Date.new(2026, 6, 29) ? "y" : "n")`, "\"y\"\n"},
		{`p Date.new(2026, 6, 29).class`, "Date\n"},
		{`p Date.ancestors.include?(Comparable)`, "false\n"}, // Date is a plain class here
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestDateTime covers the DateTime subclass: its constructors (new / civil /
// parse / strptime), the wall-clock accessors (hour / min / sec / offset), the
// instant to_s / inspect, that it inherits Date's calendar methods and arithmetic
// (a DateTime + 1 stays a DateTime), and to_date / the cross-type equality MRI
// reports for the same instant.
func TestDateTime(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{`p DateTime.new(2026, 6, 29, 12, 30, 45, "+02:00").to_s`, "\"2026-06-29T12:30:45+02:00\"\n"},
		{`p DateTime.civil(2026, 6, 29, 12, 30, 45, "+02:00").to_s`, "\"2026-06-29T12:30:45+02:00\"\n"},
		{`p DateTime.new(2026, 6, 29).to_s`, "\"2026-06-29T00:00:00+00:00\"\n"}, // defaults
		{`p DateTime.new(2026, 6, 29, 12, 30, 45, "+02:00").inspect`,
			"\"#<DateTime: 2026-06-29T12:30:45+02:00 ((2461221j,37845s,0n),+7200s,2299161j)>\"\n"},
		{`p DateTime.new(2026, 6, 29, 12, 30, 45, "+02:00").class`, "DateTime\n"},
		{`p DateTime.new(2026, 6, 29).is_a?(Date)`, "true\n"}, // subclass of Date
		// Wall-clock accessors.
		{`p DateTime.new(2026, 6, 29, 12, 30, 45, "+02:00").hour`, "12\n"},
		{`p DateTime.new(2026, 6, 29, 12, 30, 45, "+02:00").min`, "30\n"},
		{`p DateTime.new(2026, 6, 29, 12, 30, 45, "+02:00").sec`, "45\n"},
		{`p DateTime.new(2026, 6, 29, 12, 30, 45, "+02:00").offset`, "(1/12)\n"}, // Rational day fraction
		{`p DateTime.new(2026, 6, 29, 12, 0, 0, "Z").offset`, "(0/1)\n"},         // "Z" → UTC
		{`p DateTime.new(2026, 6, 29, 12, 0, 0, "-0530").offset`, "(-11/48)\n"},  // compact zone
		// strftime over the wall clock.
		{`p DateTime.new(2026, 6, 29, 12, 30, 45, "+02:00").strftime("%H:%M:%S %z")`, "\"12:30:45 +0200\"\n"},
		// parse / strptime → a DateTime.
		{`p DateTime.parse("2026-06-29T12:30:45+02:00").class`, "DateTime\n"},
		{`p DateTime.parse("2026-06-29T12:30:45+02:00").strftime("%H:%M %z")`, "\"12:30 +0200\"\n"},
		{`p DateTime.parse("2026-06-29T12:30:45+02:00").hour`, "12\n"},
		{`p DateTime.strptime("2026-06-29 12:30", "%Y-%m-%d %H:%M").to_s`, "\"2026-06-29T12:30:00+00:00\"\n"},
		// Inherited Date arithmetic keeps the DateTime class and time-of-day.
		{`p (DateTime.new(2026, 6, 29, 12, 0, 0) + 1).class`, "DateTime\n"},
		{`p (DateTime.new(2026, 6, 29, 12, 0, 0) + 1).to_s`, "\"2026-06-30T12:00:00+00:00\"\n"},
		// to_date strips the time-of-day to a plain Date.
		{`p DateTime.new(2026, 6, 29, 12, 0, 0).to_date.class`, "Date\n"},
		{`p DateTime.new(2026, 6, 29, 12, 0, 0).to_date.to_s`, "\"2026-06-29\"\n"},
		// A Date and a midnight-UTC DateTime are the same instant → equal.
		{`p(Date.new(2026, 6, 29) == DateTime.new(2026, 6, 29))`, "true\n"},
	} {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestDateErrors covers the raising paths. An invalid calendar date and a failed
// parse raise Date::Error (MRI's own class, a subclass of ArgumentError — so the
// rescue still catches ArgumentError); a non-Integer / non-Date operand to the
// arithmetic and ordering operators raises TypeError; an unparsable zone string
// and a bad offset type raise Date::Error / TypeError.
func TestDateErrors(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{`Date.new(2026, 2, 29)`, "Date::Error"}, // Feb 29 in a non-leap year
		{`Date.new(2026, 13, 1)`, "Date::Error"}, // month out of range
		{`Date.new(2026, 1, 32)`, "Date::Error"}, // day out of range
		{`Date.ordinal(2026, 400)`, "Date::Error"},
		{`Date.commercial(2026, 60, 1)`, "Date::Error"},
		{`Date.parse("not a date at all")`, "Date::Error"},
		{`Date.strptime("nope", "%Y-%m-%d")`, "Date::Error"},
		{`begin; Date.new(2026, 2, 29); rescue ArgumentError; puts "caught"; end`, ""}, // Date::Error < ArgumentError
		{`Date.new(2026, 6, 29) + "x"`, "TypeError"},                                   // + non-Integer
		{`Date.new(2026, 6, 29) + 1.5`, "TypeError"},                                   // + Float (days must be whole)
		{`Date.new(2026, 6, 29) - "x"`, "TypeError"},                                   // - non-Integer, non-Date
		{`Date.new(2026, 6, 29).next_day("x")`, "TypeError"},                           // next_day non-Integer
		{`Date.new(2026, 6, 29).prev_day("x")`, "TypeError"},                           // prev_day non-Integer
		{`Date.new(2026, 6, 29).next_month("x")`, "TypeError"},                         // next_month non-Integer
		{`Date.new(2026, 6, 29).prev_month("x")`, "TypeError"},                         // prev_month non-Integer
		{`Date.new(2026, 6, 29).next_year("x")`, "TypeError"},                          // next_year non-Integer
		{`Date.new(2026, 6, 29).prev_year("x")`, "TypeError"},                          // prev_year non-Integer
		{`Date.new(2026, 6, 29) >> "x"`, "TypeError"},                                  // >> non-Integer month count
		{`Date.new(2026, 6, 29) << "x"`, "TypeError"},                                  // << non-Integer month count
		{`Date.new(2026, 6, 29) >> 1.5`, "TypeError"},                                  // >> Float (months must be whole)
		{`Date.new(2026, 6, 29) < 5`, "TypeError"},                                     // ordering against a non-Date
		{`Date.new(2026, 6, 29) > 5`, "TypeError"},                                     // ordering against a non-Date
		{`Date.new(2026, 6, 29) <= 5`, "TypeError"},                                    // ordering against a non-Date
		{`Date.new(2026, 6, 29) >= 5`, "TypeError"},                                    // ordering against a non-Date
		{`Date.new(2026, 6, 29) * Date.new(2026, 6, 29)`, "NoMethodError"},             // unsupported operator
		{`Date.new("x", 6, 29)`, "TypeError"},                                          // non-Integer constructor arg
		{`Date.new(2026, 6, 29).upto(5){}`, "TypeError"},                               // upto limit non-Date
		{`Date.new(2026, 6, 29).step(5){}`, "TypeError"},                               // step limit non-Date
		{`DateTime.new(2026, 6, 29, 0, 0, 0, "bogus")`, "Date::Error"},                 // unparsable zone
		{`DateTime.new(2026, 6, 29, 0, 0, 0, [])`, "TypeError"},                        // offset wrong type
		{`DateTime.new(2026, 25, 29)`, "Date::Error"},                                  // invalid datetime field
	} {
		err := runErr(t, c.src)
		if c.want == "" {
			if err != nil {
				t.Errorf("src=%q got err=%v want clean", c.src, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got=%v want %q", c.src, err, c.want)
		}
	}
}
