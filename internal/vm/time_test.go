package vm_test

import (
	"strings"
	"testing"
)

// TestTime covers Ruby Time (backed by github.com/go-composites/time):
// construction (at / parse / strptime), conversion (to_i / to_f / to_s /
// inspect), formatting (strftime), the field accessors, the UTC/zone queries,
// the +/- arithmetic (Duration-backed), and the ordering / equality operators —
// each value asserted against MRI's Time semantics on a fixed UTC instant.
func TestTime(t *testing.T) {
	cases := []struct{ src, want string }{
		// Construction + to_i (Time.at from a Unix timestamp, UTC).
		{`p Time.at(1000).to_i`, "1000\n"},
		{`p Time.at(1000.9).to_i`, "1000\n"}, // Float seconds truncate
		{`p Time.at(0).to_f`, "0.0\n"},
		// to_s / inspect (RFC-ish, UTC offset).
		{`puts Time.at(0)`, "1970-01-01 00:00:00 +0000\n"},
		{`p Time.at(0).to_s`, "\"1970-01-01 00:00:00 +0000\"\n"},
		{`p Time.at(0).inspect`, "\"1970-01-01 00:00:00 +0000\"\n"},
		{`p [Time.at(0)]`, "[1970-01-01 00:00:00 +0000]\n"}, // Go-level Inspect, via Array#inspect
		// strftime.
		{`puts Time.at(0).strftime("%Y-%m-%d %H:%M:%S")`, "1970-01-01 00:00:00\n"},
		{`puts Time.at(0).strftime("%Y/%m/%d")`, "1970/01/01\n"},
		{`puts Time.at(0).strftime("100%% done")`, "100% done\n"}, // %% literal
		{`puts Time.at(0).strftime("%Q")`, "%Q\n"},                // unknown directive passes through
		// Field accessors on a known instant: 2026-06-21T12:34:56Z = 1782045296.
		{`p Time.at(1782045296).year`, "2026\n"},
		{`p Time.at(1782045296).month`, "6\n"},
		{`p Time.at(1782045296).mon`, "6\n"},
		{`p Time.at(1782045296).day`, "21\n"},
		{`p Time.at(1782045296).mday`, "21\n"},
		{`p Time.at(1782045296).hour`, "12\n"},
		{`p Time.at(1782045296).min`, "34\n"},
		{`p Time.at(1782045296).sec`, "56\n"},
		// utc / getutc / zone.
		{`p Time.at(0).utc.zone`, "\"UTC\"\n"},
		{`p Time.at(0).getutc.zone`, "\"UTC\"\n"},
		{`p Time.at(0).zone`, "\"UTC\"\n"},
		// + / - seconds (Duration-backed), and Time - Time = seconds between.
		{`p((Time.at(1000) + 60).to_i)`, "1060\n"},
		{`p((Time.at(1000) - 60).to_i)`, "940\n"},
		{`p((Time.at(1000) + 1.9).to_i)`, "1001\n"}, // Float seconds truncate
		{`p(Time.at(2000) - Time.at(1000))`, "1000\n"},
		{`p Time.at(1000).send(:+, 5).to_i`, "1005\n"}, // explicit method send agrees
		{`p Time.at(1000).send(:-, 5).to_i`, "995\n"},
		// Ordering: <=> and the boolean operators.
		{`p(Time.at(1000) <=> Time.at(2000))`, "-1\n"},
		{`p(Time.at(2000) <=> Time.at(1000))`, "1\n"},
		{`p(Time.at(1000) <=> Time.at(1000))`, "0\n"},
		{`p(Time.at(1000) <=> 5)`, "nil\n"}, // non-Time operand
		{`p(Time.at(1000) < Time.at(2000))`, "true\n"},
		{`p(Time.at(2000) < Time.at(1000))`, "false\n"},
		{`p(Time.at(2000) > Time.at(1000))`, "true\n"},
		{`p(Time.at(1000) > Time.at(2000))`, "false\n"},
		{`p(Time.at(1000) <= Time.at(1000))`, "true\n"},
		{`p(Time.at(2000) <= Time.at(1000))`, "false\n"},
		{`p(Time.at(1000) >= Time.at(1000))`, "true\n"},
		{`p(Time.at(1000) >= Time.at(2000))`, "false\n"},
		// Equality (operator routes through valueEqual; the explicit == method is
		// exercised too, including its non-Time short-circuit).
		{`p(Time.at(1000) == Time.at(1000))`, "true\n"},
		{`p(Time.at(1000) == Time.at(2000))`, "false\n"},
		{`p(Time.at(1000) == 1000)`, "false\n"}, // non-Time operand
		{`p Time.at(1000).send(:==, Time.at(1000))`, "true\n"},
		{`p Time.at(1000).send(:==, Time.at(2000))`, "false\n"},
		{`p Time.at(1000).send(:==, 42)`, "false\n"}, // method, non-Time
		// Parse (RFC3339) and strptime (strftime layout).
		{`p Time.parse("2026-06-21T12:34:56Z").to_i`, "1782045296\n"},
		{`p Time.strptime("2026-06-21", "%Y-%m-%d").to_i`, "1782000000\n"},
		// truthiness + class.
		{`p(Time.at(0) ? "y" : "n")`, "\"y\"\n"},
		{`p Time.at(0).class`, "Time\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestTimeWeekday covers wday and the seven weekday predicates, anchored on the
// Unix epoch (1970-01-01, a Thursday → wday 4) and stepped one day at a time
// (86400 s) so every weekday number and predicate is asserted deterministically.
func TestTimeWeekday(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		// wday across a full week, epoch + N days (Thu, Fri, Sat, Sun, Mon, Tue, Wed).
		{`p Time.at(0).wday`, "4\n"},         // Thursday
		{`p Time.at(86400).wday`, "5\n"},     // Friday
		{`p Time.at(86400 * 2).wday`, "6\n"}, // Saturday
		{`p Time.at(86400 * 3).wday`, "0\n"}, // Sunday
		{`p Time.at(86400 * 4).wday`, "1\n"}, // Monday
		{`p Time.at(86400 * 5).wday`, "2\n"}, // Tuesday
		{`p Time.at(86400 * 6).wday`, "3\n"}, // Wednesday
		{`p Time.at(86400 * 7).wday`, "4\n"}, // back to Thursday
		// Each predicate true on its day and false on the epoch (Thursday) for
		// the non-Thursday ones — so every predicate is exercised both ways.
		{`p Time.at(86400 * 3).sunday?`, "true\n"},
		{`p Time.at(0).sunday?`, "false\n"},
		{`p Time.at(86400 * 4).monday?`, "true\n"},
		{`p Time.at(0).monday?`, "false\n"},
		{`p Time.at(86400 * 5).tuesday?`, "true\n"},
		{`p Time.at(0).tuesday?`, "false\n"},
		{`p Time.at(86400 * 6).wednesday?`, "true\n"},
		{`p Time.at(0).wednesday?`, "false\n"},
		{`p Time.at(0).thursday?`, "true\n"},
		{`p Time.at(86400).thursday?`, "false\n"},
		{`p Time.at(86400).friday?`, "true\n"},
		{`p Time.at(0).friday?`, "false\n"},
		{`p Time.at(86400 * 2).saturday?`, "true\n"},
		{`p Time.at(0).saturday?`, "false\n"},
	} {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestTimeNow checks Time.now: it is non-deterministic by nature (Go's wall
// clock fed to the composite's FromUnix), so we only assert it yields a positive
// instant whose class is Time — the deterministic seam is exercised in the
// whitebox test.
func TestTimeNow(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{`p(Time.now.to_i > 0)`, "true\n"},
		{`p Time.now.class`, "Time\n"},
	} {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestTimeErrors covers the raising paths: a non-numeric/non-Time operand to the
// arithmetic and ordering operators, and a malformed parse/strptime input (whose
// composite error Result is surfaced as a Ruby ArgumentError).
func TestTimeErrors(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{`Time.at("x")`, "TypeError"},     // bad Time.at argument
		{`Time.at(0) + "x"`, "TypeError"}, // + non-numeric
		{`Time.at(0) - "x"`, "TypeError"}, // - non-numeric, non-Time
		{`Time.at(0) < 5`, "TypeError"},   // ordering against a non-Time
		{`Time.at(0) > 5`, "TypeError"},   // ordering against a non-Time
		{`Time.at(0) <= 5`, "TypeError"},  // ordering against a non-Time
		{`Time.at(0) >= 5`, "TypeError"},  // ordering against a non-Time
		{`Time.parse("not a time")`, "ArgumentError"},
		{`Time.strptime("nope", "%Y-%m-%d")`, "ArgumentError"},
	} {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got=%v want %q", c.src, err, c.want)
		}
	}
}
