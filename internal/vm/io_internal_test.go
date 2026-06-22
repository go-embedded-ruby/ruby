package vm

import "testing"

// TestIODisplayMarkers covers the ToS/Inspect/Truthy methods of IOObj for both
// kinds (real IO and StringIO); their string forms differ from MRI's
// address-bearing inspect, so they are not part of the differential suite.
func TestIODisplayMarkers(t *testing.T) {
	for _, c := range []struct {
		o    *IOObj
		want string
	}{
		{&IOObj{isStr: true}, "#<StringIO>"},
		{&IOObj{label: "STDERR"}, "#<IO:<STDERR>>"},
	} {
		if c.o.ToS() != c.want || c.o.Inspect() != c.want || !c.o.Truthy() {
			t.Errorf("%q: ToS=%q Inspect=%q Truthy=%v", c.want, c.o.ToS(), c.o.Inspect(), c.o.Truthy())
		}
	}
}
