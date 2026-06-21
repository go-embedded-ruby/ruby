package vm_test

import (
	"strings"
	"testing"
)

// TestDate covers Ruby Date (backed by github.com/go-composites/date — the
// fourth go-composites consumer after Set, Time and BigDecimal): construction
// (new / parse), the field accessors (year / month / mon / day / mday), the
// day-of-week queries (wday / cwday), the ISO string (to_s / inspect), the day
// arithmetic (+/- and next_day / prev_day) and the ordering / equality
// operators — each value asserted against MRI's Date semantics on fixed
// calendar dates. 2026-06-21 is a Sunday, which anchors the weekday checks.
func TestDate(t *testing.T) {
	cases := []struct{ src, want string }{
		// Construction + ISO string (to_s / inspect, and Go-level Inspect via Array).
		{`puts Date.new(2026, 6, 21)`, "2026-06-21\n"},
		{`p Date.new(2026, 6, 21).to_s`, "\"2026-06-21\"\n"},
		{`p Date.new(2026, 6, 21).inspect`, "\"2026-06-21\"\n"},
		{`p [Date.new(2026, 6, 21)]`, "[2026-06-21]\n"},
		{`p Date.new(2024, 2, 29).to_s`, "\"2024-02-29\"\n"}, // leap day is valid
		// Field accessors.
		{`p Date.new(2026, 6, 21).year`, "2026\n"},
		{`p Date.new(2026, 6, 21).month`, "6\n"},
		{`p Date.new(2026, 6, 21).mon`, "6\n"},
		{`p Date.new(2026, 6, 21).day`, "21\n"},
		{`p Date.new(2026, 6, 21).mday`, "21\n"},
		// Day-of-week: 2026-06-21 is a Sunday (wday 0 / cwday 7); 2026-06-22 is a
		// Monday (wday 1 / cwday 1).
		{`p Date.new(2026, 6, 21).wday`, "0\n"},
		{`p Date.new(2026, 6, 21).cwday`, "7\n"},
		{`p Date.new(2026, 6, 22).wday`, "1\n"},
		{`p Date.new(2026, 6, 22).cwday`, "1\n"},
		// + days / - days (AddDays-backed), and Date - Date = day count between.
		{`puts (Date.new(2026, 6, 21) + 10).to_s`, "2026-07-01\n"},
		{`puts (Date.new(2026, 6, 21) - 10).to_s`, "2026-06-11\n"},
		{`p(Date.new(2026, 7, 1) - Date.new(2026, 6, 21))`, "10\n"},
		{`p(Date.new(2026, 6, 21) - Date.new(2026, 7, 1))`, "-10\n"},    // negative span
		{`puts Date.new(2026, 6, 21).send(:+, 1).to_s`, "2026-06-22\n"}, // method send agrees
		{`puts Date.new(2026, 6, 21).send(:-, 1).to_s`, "2026-06-20\n"},
		// next_day / prev_day (default 1, explicit n).
		{`puts Date.new(2026, 6, 21).next_day.to_s`, "2026-06-22\n"},
		{`puts Date.new(2026, 6, 21).next_day(5).to_s`, "2026-06-26\n"},
		{`puts Date.new(2026, 6, 21).prev_day.to_s`, "2026-06-20\n"},
		{`puts Date.new(2026, 6, 21).prev_day(2).to_s`, "2026-06-19\n"},
		// leap? (→ IsLeapYear): 2024 is a leap year, 2026 is not, 2000 is (÷400),
		// 1900 is not (÷100 but not ÷400).
		{`p Date.new(2024, 1, 1).leap?`, "true\n"},
		{`p Date.new(2026, 1, 1).leap?`, "false\n"},
		{`p Date.new(2000, 1, 1).leap?`, "true\n"},
		{`p Date.new(1900, 1, 1).leap?`, "false\n"},
		// yday (→ DayOfYear): 1-based day of year. Jan 1 = 1; Mar 15 2026 = 74
		// (31 + 28 + 15, 2026 not a leap year); Dec 31 2024 = 366 (leap year).
		{`p Date.new(2026, 1, 1).yday`, "1\n"},
		{`p Date.new(2026, 3, 15).yday`, "74\n"},
		{`p Date.new(2024, 12, 31).yday`, "366\n"},
		// next_month / prev_month (default 1, explicit n) → AddMonths. MRI's
		// day-of-month normalisation: 31 Jan + 1 month → 3 Mar (Feb has no 31st).
		{`puts Date.new(2026, 6, 21).next_month.to_s`, "2026-07-21\n"},
		{`puts Date.new(2026, 6, 21).next_month(3).to_s`, "2026-09-21\n"},
		{`puts Date.new(2026, 6, 21).prev_month.to_s`, "2026-05-21\n"},
		{`puts Date.new(2026, 6, 21).prev_month(2).to_s`, "2026-04-21\n"},
		{`puts Date.new(2026, 1, 31).next_month.to_s`, "2026-03-03\n"}, // overflow normalised
		// >> n / << n: month-shift operators (dispatch as method sends; no opcode).
		{`puts (Date.new(2026, 1, 31) >> 1).to_s`, "2026-03-03\n"},
		{`puts (Date.new(2026, 6, 21) >> 6).to_s`, "2026-12-21\n"},
		{`puts (Date.new(2026, 6, 21) << 6).to_s`, "2025-12-21\n"},
		{`puts (Date.new(2026, 6, 21) >> -1).to_s`, "2026-05-21\n"}, // negative shift
		{`puts Date.new(2026, 6, 21).send(:">>", 1).to_s`, "2026-07-21\n"},
		{`puts Date.new(2026, 6, 21).send(:"<<", 1).to_s`, "2026-05-21\n"},
		// Parse (ISO).
		{`puts Date.parse("2026-02-14")`, "2026-02-14\n"},
		{`p Date.parse("2026-02-14").class`, "Date\n"},
		// Ordering: <=> and the boolean operators.
		{`p(Date.new(2026, 6, 21) <=> Date.new(2026, 6, 22))`, "-1\n"},
		{`p(Date.new(2026, 6, 22) <=> Date.new(2026, 6, 21))`, "1\n"},
		{`p(Date.new(2026, 6, 21) <=> Date.new(2026, 6, 21))`, "0\n"},
		{`p(Date.new(2026, 6, 21) <=> 5)`, "nil\n"}, // non-Date operand
		{`p(Date.new(2026, 6, 21) < Date.new(2026, 12, 25))`, "true\n"},
		{`p(Date.new(2026, 12, 25) < Date.new(2026, 6, 21))`, "false\n"},
		{`p(Date.new(2026, 12, 25) > Date.new(2026, 6, 21))`, "true\n"},
		{`p(Date.new(2026, 6, 21) > Date.new(2026, 12, 25))`, "false\n"},
		{`p(Date.new(2026, 6, 21) <= Date.new(2026, 6, 21))`, "true\n"},
		{`p(Date.new(2026, 12, 25) <= Date.new(2026, 6, 21))`, "false\n"},
		{`p(Date.new(2026, 6, 21) >= Date.new(2026, 6, 21))`, "true\n"},
		{`p(Date.new(2026, 6, 21) >= Date.new(2026, 12, 25))`, "false\n"},
		// Equality (operator routes through valueEqual; the explicit == method is
		// exercised too, including its non-Date short-circuit).
		{`p(Date.new(2026, 6, 21) == Date.new(2026, 6, 21))`, "true\n"},
		{`p(Date.new(2026, 6, 21) == Date.new(2026, 6, 22))`, "false\n"},
		{`p(Date.new(2026, 6, 21) == 5)`, "false\n"}, // non-Date operand
		{`p Date.new(2026, 6, 21).send(:==, Date.new(2026, 6, 21))`, "true\n"},
		{`p Date.new(2026, 6, 21).send(:==, Date.new(2026, 6, 22))`, "false\n"},
		{`p Date.new(2026, 6, 21).send(:==, 42)`, "false\n"}, // method, non-Date
		// truthiness + class.
		{`p(Date.new(2026, 6, 21) ? "y" : "n")`, "\"y\"\n"},
		{`p Date.new(2026, 6, 21).class`, "Date\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestDateErrors covers the raising paths: an invalid calendar date (whose
// composite error Result surfaces as a Ruby ArgumentError), a malformed parse
// input (likewise ArgumentError), a non-Integer day offset / non-Date operand to
// the arithmetic and ordering operators (TypeError via the coercion), and a
// non-Integer constructor argument (TypeError via intArg).
func TestDateErrors(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{`Date.new(2026, 2, 29)`, "ArgumentError"}, // Feb 29 in a non-leap year
		{`Date.new(2026, 13, 1)`, "ArgumentError"}, // month out of range
		{`Date.new(2026, 1, 32)`, "ArgumentError"}, // day out of range
		{`Date.parse("not a date")`, "ArgumentError"},
		{`Date.parse("2026/06/21")`, "ArgumentError"},                      // non-ISO separator
		{`Date.new(2026, 6, 21) + "x"`, "TypeError"},                       // + non-Integer
		{`Date.new(2026, 6, 21) + 1.5`, "TypeError"},                       // + Float (days must be whole)
		{`Date.new(2026, 6, 21) - "x"`, "TypeError"},                       // - non-Integer, non-Date
		{`Date.new(2026, 6, 21).next_day("x")`, "TypeError"},               // next_day non-Integer
		{`Date.new(2026, 6, 21).prev_day("x")`, "TypeError"},               // prev_day non-Integer
		{`Date.new(2026, 6, 21).next_month("x")`, "TypeError"},             // next_month non-Integer
		{`Date.new(2026, 6, 21).prev_month("x")`, "TypeError"},             // prev_month non-Integer
		{`Date.new(2026, 6, 21) >> "x"`, "TypeError"},                      // >> non-Integer month count
		{`Date.new(2026, 6, 21) << "x"`, "TypeError"},                      // << non-Integer month count
		{`Date.new(2026, 6, 21) >> 1.5`, "TypeError"},                      // >> Float (months must be whole)
		{`Date.new(2026, 6, 21) < 5`, "TypeError"},                         // ordering against a non-Date
		{`Date.new(2026, 6, 21) > 5`, "TypeError"},                         // ordering against a non-Date
		{`Date.new(2026, 6, 21) <= 5`, "TypeError"},                        // ordering against a non-Date
		{`Date.new(2026, 6, 21) >= 5`, "TypeError"},                        // ordering against a non-Date
		{`Date.new(2026, 6, 21) * Date.new(2026, 6, 21)`, "NoMethodError"}, // unsupported operator
		{`Date.new("x", 6, 21)`, "TypeError"},                              // non-Integer constructor arg
	} {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got=%v want %q", c.src, err, c.want)
		}
	}
}
