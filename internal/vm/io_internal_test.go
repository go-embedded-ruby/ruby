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

// TestIOFileno covers IO#fileno / #to_i: the standard streams report 0/1/2, any
// other stream gets a distinct descriptor that is stable across calls, and a
// closed stream raises IOError. Asserted against MRI Ruby 4.0.6.
func TestIOFileno(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p [$stdin.fileno, $stdout.fileno, $stderr.fileno]`, "[0, 1, 2]\n"},
		{`p $stdout.to_i`, "1\n"},
		{`r, w = IO.pipe; p [r.fileno.is_a?(Integer), r.fileno != w.fileno, r.fileno == r.fileno]`, "[true, true, true]\n"},
		{`r, w = IO.pipe; r.close; begin; r.fileno; rescue IOError => e; p e.class; end`, "IOError\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestIOReadpartial covers StringIO#readpartial: reading up to a length, the
// output-buffer form, the zero-length "" case, EOFError at end of input, a
// negative length ArgumentError and IOError on a closed stream. Asserted against
// MRI Ruby 4.0.6.
func TestIOReadpartial(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "stringio"; s = StringIO.new("hello world"); p [s.readpartial(5), s.readpartial(100)]`, "[\"hello\", \" world\"]\n"},
		{`require "stringio"; s = StringIO.new("abc"); buf = "x"; r = s.readpartial(2, buf); p [r, buf.equal?(r)]`, "[\"ab\", true]\n"},
		{`require "stringio"; p StringIO.new("abc").readpartial(0)`, "\"\"\n"},
		{`require "stringio"; s = StringIO.new("hi"); s.readpartial(2); begin; s.readpartial(5); rescue EOFError => e; p e.class; end`, "EOFError\n"},
		{`require "stringio"; begin; StringIO.new("x").readpartial(-1); rescue ArgumentError => e; p e.class; end`, "ArgumentError\n"},
		{`require "stringio"; s = StringIO.new("x"); s.close; begin; s.readpartial(5); rescue IOError => e; p e.class; end`, "IOError\n"},
		{`require "stringio"; s = StringIO.new("hi"); begin; s.readpartial(2, "z".freeze); rescue FrozenError => e; p e.class; end`, "FrozenError\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}
