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
