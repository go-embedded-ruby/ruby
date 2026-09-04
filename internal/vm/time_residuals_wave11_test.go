package vm_test

import (
	"strings"
	"testing"
)

// TestTimeArithExact pins Time#+ / Time#- to MRI 4.0.6: the shift amount is
// coerced with num_exact (Integer/Rational exact, Float via its exact #to_r) and
// applied to the nanosecond, so microsecond/nanosecond tracking is lossless and a
// negative Float truncates toward MRI's exact-binary result. Fixed instants only.
func TestTimeArithExact(t *testing.T) {
	cases := []struct{ src, want string }{
		// Rational shift is exact: 1.1 + 0.9 == 2.
		{`p((Time.at(Rational(11, 10)) + Rational(9, 10)) == Time.at(2))`, "true\n"},
		// Integer shift.
		{`p (Time.utc(1970, 1, 1, 0, 0, 0) + 5).sec`, "5\n"},
		{`p (Time.utc(1970, 1, 1, 0, 0, 10) - 4).sec`, "6\n"},
		// A negative Float truncates to MRI's exact-binary microseconds.
		{`p (Time.at(100) + -1.3).usec`, "699999\n"},
		{`p (Time.at(100) + -1.3).to_i`, "98\n"},
		// Microsecond / nanosecond tracking stays exact across repeated adds.
		{`t = Time.at(0); t += Rational(123_456, 1_000_000); p t.usec`, "123456\n"},
		{`t = Time.at(0); t += Rational(123_456_789, 1_000_000_000); p t.nsec`, "123456789\n"},
		// Subtracting two Times yields the Float seconds between them.
		{`p Time.utc(2020, 1, 1, 0, 0, 10) - Time.utc(2020, 1, 1, 0, 0, 0)`, "10.0\n"},
		// A Bignum shift.
		{`p (Time.at(0) + (10 ** 18)).to_i`, "1000000000000000000\n"},
	}
	for _, tc := range cases {
		if got := eval(t, tc.src); got != tc.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
		}
	}
}

// TestTimeArithCoercion pins the num_exact object protocol: an object is coerced
// via #to_r only when it also answers #to_int (else via #to_int alone), while a
// String, nil, an object answering neither, and a #to_r returning a non-number
// are all rejected with TypeError — matching MRI 4.0.6.
func TestTimeArithCoercion(t *testing.T) {
	// #to_r honoured when #to_int is present too.
	if got := eval(t, `o = Object.new
def o.to_r; Rational(3, 2); end
def o.to_int; 1; end
p((Time.at(0) + o).subsec)`); got != "(1/2)\n" {
		t.Errorf("to_r+to_int: got %q", got)
	}
	// #to_int alone is used when #to_r is absent.
	if got := eval(t, `o = Object.new
def o.to_int; 5; end
p((Time.utc(1970, 1, 1, 0, 0, 0) + o).sec)`); got != "5\n" {
		t.Errorf("to_int only: got %q", got)
	}
	// Rejections.
	rejects := []string{
		`Time.now + "1"`,
		`Time.now + nil`,
		`Time.now + Object.new`,
		`o = Object.new; def o.to_r; Rational(3, 2); end; Time.now + o`,              // to_r but no to_int
		`o = Object.new; def o.to_int; "x"; end; def o.to_r; "y"; end; Time.now + o`, // to_r returns non-number
	}
	for _, src := range rejects {
		if err := runErr(t, src); err == nil || !strings.Contains(err.Error(), "TypeError") {
			t.Errorf("expected TypeError for %q, got %v", src, err)
		}
	}
	// Adding two Times is rejected.
	if err := runErr(t, `Time.now + Time.now`); err == nil || !strings.Contains(err.Error(), "TypeError") {
		t.Errorf("Time+Time: expected TypeError, got %v", err)
	}
	// A non-finite Float shift raises FloatDomainError.
	if err := runErr(t, `Time.utc(2020) + (1.0 / 0.0)`); err == nil || !strings.Contains(err.Error(), "FloatDomainError") {
		t.Errorf("Time+Infinity: expected FloatDomainError, got %v", err)
	}
	// Every other arithmetic operator on a Time is undefined.
	if err := runErr(t, `Time.utc(2020) * 2`); err == nil || !strings.Contains(err.Error(), "NoMethodError") {
		t.Errorf("Time*2: expected NoMethodError, got %v", err)
	}
}

// TestTimeExactRationalSeconds pins Time.utc/local's seconds field, when given as
// a Rational (or a Numeric coerced through #to_r), to MRI's exact conversion: no
// Float round-trip, so "25.02" is 20_000_000 ns, not 19_999_999.
func TestTimeExactRationalSeconds(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p Time.utc(2010, 3, 30, 5, 43, "25.02".to_r).nsec`, "20000000\n"},
		{`p Time.utc(2010, 3, 30, 5, 43, "25.0123456789".to_r).nsec`, "12345678\n"},
		// A Numeric subclass answering #to_r (but not #to_int) coerces exactly.
		{`class MyNum < Numeric; def to_r; Rational(5, 2); end; end
p Time.utc(2010, 1, 1, 0, 0, MyNum.new).nsec`, "500000000\n"},
	}
	for _, tc := range cases {
		if got := eval(t, tc.src); got != tc.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
		}
	}
}

// TestTimeGetlocalTimezoneObject pins Time#getlocal / Time#localtime with a
// timezone object (one answering #utc_to_local): the instant is rendered through
// that object exactly as Time.at(in: zone), the object becomes the result's zone,
// and the derived fixed offset is reported by #utc_offset — matching MRI 4.0.6.
func TestTimeGetlocalTimezoneObject(t *testing.T) {
	// getlocal returns a fresh Time in the zone.
	if got := eval(t, `z = Object.new
def z.utc_to_local(t); t + 3600; end
t = Time.utc(2000, 1, 1, 12, 0, 0).getlocal(z)
p t.utc_offset
p t.zone.equal?(z)`); got != "3600\ntrue\n" {
		t.Errorf("getlocal(zone): got %q", got)
	}
	// localtime mutates the receiver into the zone.
	if got := eval(t, `z = Object.new
def z.utc_to_local(t); t + 3600; end
t = Time.utc(2000, 1, 1, 12, 0, 0)
t.localtime(z)
p t.utc_offset
p t.zone.equal?(z)`); got != "3600\ntrue\n" {
		t.Errorf("localtime(zone): got %q", got)
	}
	// A zone answering neither #utc_to_local nor a numeric/offset protocol is a
	// TypeError, as MRI reports for an object it cannot read as an offset.
	if err := runErr(t, `z = Object.new
def z.local_to_utc(t); t; end
Time.utc(2000, 1, 1, 12, 0, 0).getlocal(z)`); err == nil || !strings.Contains(err.Error(), "TypeError") {
		t.Errorf("getlocal(bad zone): expected TypeError, got %v", err)
	}
}

// TestTimeLocalTimezoneEnv pins rbgo's local timezone resolution to the runtime
// TZ (as a spec's with_timezone sets via ENV['TZ']): an IANA name is honoured
// with DST, while an unloadable name defaults to UTC — the source restoring TZ so
// the process default (used by every other example) is unchanged.
func TestTimeLocalTimezoneEnv(t *testing.T) {
	cases := []struct{ src, want string }{
		// IANA name, DST-aware: winter EST, summer EDT.
		{`ENV["TZ"] = "America/New_York"
r = [Time.new(2001, 1, 1, 0, 0, 0).zone, Time.new(2001, 7, 1, 0, 0, 0).zone]
ENV.delete("TZ")
p r`, `["EST", "EDT"]` + "\n"},
		// An unloadable TZ falls back to UTC (offset 0), matching MRI.
		{`ENV["TZ"] = "hello-foo"
r = Time.new(2001, 1, 1, 0, 0, 0).utc_offset
ENV.delete("TZ")
p r`, "0\n"},
	}
	for _, tc := range cases {
		if got := eval(t, tc.src); got != tc.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
		}
	}
}

// TestTimeZoneStrings pins Time#zone / strftime("%Z") for the UTC and
// fixed-offset string forms: "-00:00" is RFC 3339 unknown-offset UTC (zone
// "UTC"), a plain numeric offset has no zone name (nil / ""), matching MRI 4.0.6.
func TestTimeZoneStrings(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p Time.new(2022, 1, 1, 0, 0, 0, "-00:00").zone`, "\"UTC\"\n"},
		{`p Time.new(2022, 1, 1, 0, 0, 0, "UTC").zone`, "\"UTC\"\n"},
		{`p Time.new(2022, 1, 1, 0, 0, 0, 3600).zone`, "nil\n"},
		// strftime %Z is empty for an offset-only zone (unlike #inspect).
		{`p Time.new(2000, 1, 1, 0, 0, 0, 42).strftime("%Z")`, "\"\"\n"},
		{`p Time.utc(2000).strftime("%Z")`, "\"UTC\"\n"},
	}
	for _, tc := range cases {
		if got := eval(t, tc.src); got != tc.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
		}
	}
}
