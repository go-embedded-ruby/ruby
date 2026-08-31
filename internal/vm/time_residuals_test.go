package vm_test

import "testing"

// TestTimeAliasIdentity pins the documented Time method aliases as sharing a
// single method entry, exactly as MRI 4.0.6 does: Time.instance_method(:alias)
// == Time.instance_method(:canonical). rubyspec asserts this for #mon, #mday,
// #tv_sec, #tv_usec, #tv_nsec, #gmt_offset, #gmtoff, #gmt?, #isdst, #gmtime,
// #getgm, #ctime and #xmlschema.
func TestTimeAliasIdentity(t *testing.T) {
	pairs := []struct{ a, b string }{
		{"mon", "month"}, {"mday", "day"},
		{"tv_sec", "to_i"}, {"tv_usec", "usec"}, {"tv_nsec", "nsec"},
		{"gmt_offset", "utc_offset"}, {"gmtoff", "utc_offset"},
		{"gmt?", "utc?"}, {"isdst", "dst?"},
		{"gmtime", "utc"}, {"getgm", "getutc"},
		{"ctime", "asctime"}, {"xmlschema", "iso8601"},
	}
	for _, p := range pairs {
		src := "p Time.instance_method(:" + p.a + ") == Time.instance_method(:" + p.b + ")"
		if got := eval(t, src); got != "true\n" {
			t.Errorf("alias %s->%s: got=%q want=%q", p.a, p.b, got, "true\n")
		}
	}
}

// TestTimeAliasBehavior confirms the aliases still compute the right value once
// they share the canonical body (a shared record must not lose behavior).
func TestTimeAliasBehavior(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p Time.utc(2020, 3, 1, 12, 0, 0).mon`, "3\n"},
		{`p Time.utc(2020, 3, 5, 12, 0, 0).mday`, "5\n"},
		{`p Time.utc(2020, 3, 1, 12, 0, 0).tv_sec`, "1583064000\n"},
		{`p Time.utc(2020, 3, 1, 12, 0, 0, 123456).tv_usec`, "123456\n"},
		{`p Time.utc(2020, 3, 1, 12, 0, 0, 123456).tv_nsec`, "123456000\n"},
		{`p Time.utc(2020, 3, 1, 12, 0, 0).gmt_offset`, "0\n"},
		{`p Time.utc(2020, 3, 1, 12, 0, 0).gmtoff`, "0\n"},
		{`p Time.utc(2020, 3, 1, 12, 0, 0).gmt?`, "true\n"},
		{`p Time.utc(2020, 3, 1, 12, 0, 0).isdst`, "false\n"},
		{`p Time.utc(2020, 3, 1, 12, 0, 0).getgm.utc?`, "true\n"},
		{`p Time.utc(2020, 3, 1, 12, 0, 0).ctime`, "\"Sun Mar  1 12:00:00 2020\"\n"},
		{`p Time.utc(2020, 3, 1, 12, 0, 0).xmlschema`, "\"2020-03-01T12:00:00Z\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestTimeStrftimeDirectives covers the strftime additions: %g (two-digit
// commercial/ISO week year), %v (the VMS/Oracle date, an alias of %e-%^b-%Y),
// and the GNU flag modifiers applying through an alias directive (%^h upcases
// %b, with width and pad flags composing), all matched against MRI 4.0.6.
func TestTimeStrftimeDirectives(t *testing.T) {
	// A fixed UTC instant keeps the assertions zone-independent.
	base := `t = Time.utc(2001, 2, 3, 4, 5, 6); `
	cases := []struct{ src, want string }{
		{base + `p t.strftime("%g")`, "\"01\"\n"},
		{base + `p Time.utc(2000, 4, 6).strftime("%g")`, "\"00\"\n"},
		{base + `p t.strftime("%v")`, "\" 3-FEB-2001\"\n"},
		{base + `p t.strftime("%v") == t.strftime("%e-%^b-%Y")`, "true\n"},
		{base + `p t.strftime("%^h")`, "\"FEB\"\n"},
		{base + `p t.strftime("%^_5h")`, "\"  FEB\"\n"},
		{base + `p t.strftime("%0^5h")`, "\"00FEB\"\n"},
		{base + `p t.strftime("%0-^5h")`, "\"FEB\"\n"},
		{base + `p t.strftime("%^ha")`, "\"FEBa\"\n"},
		{base + `p t.strftime("%10h")`, "\"       Feb\"\n"},
		{base + `p t.strftime("%_010h")`, "\"0000000Feb\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestTimeShiftPreservesZone covers timeShift: Time#+ and Time#- keep both the
// receiver's UTC-ness / fixed offset and its Ruby timezone object, as MRI does.
func TestTimeShiftPreservesZone(t *testing.T) {
	cases := []struct{ src, want string }{
		// A UTC receiver stays UTC after a shift.
		{`p (Time.utc(2020, 3, 1, 12, 0, 0) + 10).utc?`, "true\n"},
		{`p (Time.utc(2020, 3, 1, 12, 0, 0) - 10).utc?`, "true\n"},
		// A fixed-offset receiver keeps its offset.
		{`p (Time.at(0).getlocal("+05:00") + 1).utc_offset`, "18000\n"},
		{`p (Time.at(0).getlocal("-07:00") - 1).utc_offset`, "-25200\n"},
		// A Rational shift via the explicit method call is lossless to the second.
		{`p Time.at(0).+(Rational(3, 2)).usec`, "500000\n"},
		// A timezone object rides along through +/-.
		{`
class TZ
  def utc_to_local(t) t end
  def local_to_utc(t) t end
  def name; "TZ"; end
end
t = Time.new(2020, 3, 1, 12, 0, 0, TZ.new)
p [(t + 10).zone.name, (t - 10).zone.name]`, "[\"TZ\", \"TZ\"]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}
