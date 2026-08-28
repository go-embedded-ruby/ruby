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

// TestIOEncodingMethods covers IO#external_encoding / #internal_encoding /
// #set_encoding on a pipe: a readable end defaults to Encoding.default_external
// with no internal encoding, set_encoding accepts a name, a combined
// "ext:int" string and an Encoding pair (returning self), a nil argument clears
// a side, and a non-String/Encoding argument is a TypeError. Asserted against
// MRI Ruby 4.0.6.
func TestIOEncodingMethods(t *testing.T) {
	cases := []struct{ src, want string }{
		{`r, w = IO.pipe; p [r.external_encoding.name, r.internal_encoding]`, "[\"UTF-8\", nil]\n"},
		{`r, w = IO.pipe; r.set_encoding("US-ASCII"); p [r.external_encoding.name, r.internal_encoding]`, "[\"US-ASCII\", nil]\n"},
		{`r, w = IO.pipe; r.set_encoding("UTF-8", "UTF-16LE"); p [r.external_encoding.name, r.internal_encoding.name]`, "[\"UTF-8\", \"UTF-16LE\"]\n"},
		{`r, w = IO.pipe; r.set_encoding("UTF-8:UTF-16LE"); p [r.external_encoding.name, r.internal_encoding.name]`, "[\"UTF-8\", \"UTF-16LE\"]\n"},
		{`r, w = IO.pipe; r.set_encoding(Encoding::UTF_8, Encoding::UTF_16LE); p r.internal_encoding.name`, "\"UTF-16LE\"\n"},
		{`r, w = IO.pipe; p r.set_encoding("UTF-8").equal?(r)`, "true\n"},
		{`r, w = IO.pipe; r.set_encoding("UTF-8", "UTF-16LE"); r.set_encoding("US-ASCII"); p [r.external_encoding.name, r.internal_encoding]`, "[\"US-ASCII\", nil]\n"},
		// A non-String/Encoding argument raises TypeError (via the encoding coercion).
		{`r, w = IO.pipe; begin; r.set_encoding(42); rescue TypeError => e; p e.message; end`, "\"no implicit conversion of Integer into String\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}
