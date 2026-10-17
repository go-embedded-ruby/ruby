package ruby_test

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"math/big"
	"reflect"
	"strings"
	"testing"

	nd "github.com/go-ndarray/ndarray"

	ruby "github.com/go-embedded-ruby/ruby"
)

func newInterp(t *testing.T) (*ruby.Interp, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	var out, errOut bytes.Buffer
	return ruby.New(ruby.WithStdout(&out), ruby.WithStderr(&errOut)), &out, &errOut
}

func mustEval(t *testing.T, in *ruby.Interp, src string) ruby.Value {
	t.Helper()
	v, err := in.Eval(src, "test.rb", 1)
	if err != nil {
		t.Fatalf("Eval(%q): %v", src, err)
	}
	return v
}

// Top-level locals persist from one Eval to the next, and so do methods,
// constants and globals.
func TestInterpLocalsPersist(t *testing.T) {
	in, out, _ := newInterp(t)
	mustEval(t, in, "x = 40\ndef twice(v) = v * 2\nK = 1\n$g = 2")
	if got := mustEval(t, in, "x += 2; [x, twice(x), K, $g]").Inspect(); got != "[42, 84, 1, 2]" {
		t.Fatalf("got %s", got)
	}
	mustEval(t, in, "ys = [1, 2].map { |e| e + x }")
	if got := mustEval(t, in, "ys").Inspect(); got != "[43, 44]" {
		t.Fatalf("ys = %s", got)
	}
	// A block made in one Eval still closes over the local in a later one.
	mustEval(t, in, "inc = -> { x += 1 }")
	mustEval(t, in, "inc.call")
	if got := mustEval(t, in, "x").Inspect(); got != "43" {
		t.Fatalf("x = %s after the closure ran", got)
	}
	mustEval(t, in, `puts "hi"`)
	if out.String() != "hi\n" {
		t.Fatalf("stdout = %q", out.String())
	}
	if got := in.Locals(); !reflect.DeepEqual(got, []string{"x", "ys", "inc"}) {
		t.Fatalf("Locals = %v", got)
	}
}

func TestInterpStreams(t *testing.T) {
	in, out, errOut := newInterp(t)
	mustEval(t, in, `$stdout.print "O"; $stderr.print "E"; warn "W"`)
	if out.String() != "O" || errOut.String() != "EW\n" {
		t.Fatalf("stdout %q stderr %q", out.String(), errOut.String())
	}
	var o2, e2 bytes.Buffer
	in.SetStdout(&o2)
	in.SetStderr(&e2)
	mustEval(t, in, `print 1; $stderr.print 2`)
	if o2.String() != "1" || e2.String() != "2" || out.Len() != 1 {
		t.Fatalf("after switching: %q %q %q", o2.String(), e2.String(), out.String())
	}
	// The default streams are the process's.
	if ruby.New() == nil {
		t.Fatal("New returned nil")
	}
}

func TestInterpErrors(t *testing.T) {
	in, _, _ := newInterp(t)
	_, err := in.Eval("a = 1\n\nraise ArgumentError, 'bad'", "cell.rb", 10)
	var e *ruby.Error
	if !errors.As(err, &e) {
		t.Fatalf("err = %#v", err)
	}
	if e.Class != "ArgumentError" || e.Message != "bad" || e.File != "cell.rb" || e.Line != 12 {
		t.Fatalf("error = %+v", e)
	}
	if e.Error() != "cell.rb:12: bad (ArgumentError)" {
		t.Fatalf("Error() = %q", e.Error())
	}
	if len(e.Backtrace) == 0 || !strings.HasPrefix(e.Backtrace[0], "cell.rb:12") {
		t.Fatalf("backtrace = %q", e.Backtrace)
	}
	// The failed Eval still assigned what ran before the raise, and the
	// interpreter carries on.
	if got := mustEval(t, in, "a").Inspect(); got != "1" {
		t.Fatalf("a = %s", got)
	}

	// Raised inside a method: placed at the line of the evaluated source.
	mustEval(t, in, "def boom\n  1 / 0\nend")
	_, err = in.Eval("x = 1\nboom", "cell2.rb", 1)
	errors.As(err, &e)
	if e.Class != "ZeroDivisionError" || e.File != "cell2.rb" || e.Line != 2 {
		t.Fatalf("error = %+v", e)
	}

	// A syntax error, at its line in the file.
	_, err = in.Eval("x = 1\ndef (", "cell3.rb", 5)
	errors.As(err, &e)
	if e.Class != "SyntaxError" || e.File != "cell3.rb" || e.Line != 6 {
		t.Fatalf("syntax error = %+v (%v)", e, err)
	}

	// exit is an error that says so, and the interpreter stays usable.
	_, err = in.Eval("exit 3", "c.rb", 1)
	errors.As(err, &e)
	if st, ok := e.ExitStatus(); !ok || st != 3 {
		t.Fatalf("exit status = %d %v", st, ok)
	}
	_, err = in.Eval("raise 'not an exit'", "c.rb", 1)
	errors.As(err, &e)
	if _, ok := e.ExitStatus(); ok {
		t.Fatal("a RuntimeError is not an exit")
	}
	if _, ok := ruby.NewError("", "x").ExitStatus(); ok {
		t.Fatal("NewError is not an exit")
	}
	if got := ruby.NewError("", "boom").Error(); got != "RuntimeError: boom" {
		t.Fatalf("NewError = %q", got)
	}
	// throw without a catch.
	_, err = in.Eval("throw :nope", "c.rb", 1)
	errors.As(err, &e)
	if e.Class != "UncaughtThrowError" {
		t.Fatalf("throw = %+v", e)
	}
	if got := mustEval(t, in, "1 + 1").Inspect(); got != "2" {
		t.Fatal(got)
	}
}

func TestInterpClose(t *testing.T) {
	in, out, _ := newInterp(t)
	mustEval(t, in, `at_exit { puts "bye" }`)
	if err := in.Close(); err != nil || out.String() != "bye\n" {
		t.Fatalf("Close: %v, %q", err, out.String())
	}
	in2, _, _ := newInterp(t)
	mustEval(t, in2, `at_exit { raise "in at_exit" }`)
	if err := in2.Close(); err == nil {
		// MRI reports an at_exit exception and carries on; either is fine as
		// long as the host is not crashed.
		_ = err
	}
}

func TestInterpSetGet(t *testing.T) {
	in, _, _ := newInterp(t)
	big1 := new(big.Int).Lsh(big.NewInt(1), 100)
	rat := big.NewRat(1, 3)
	arr, _ := nd.FromData([]float64{1, 2, 3}, 3)
	type point struct{ X, Y int }
	cases := []struct {
		name    string
		in      any
		inspect string
		back    any
	}{
		{"n", nil, "nil", nil},
		{"t", true, "true", true},
		{"i", 7, "7", int64(7)},
		{"i8", int8(-8), "-8", int64(-8)},
		{"i16", int16(16), "16", int64(16)},
		{"i32", int32(32), "32", int64(32)},
		{"i64", int64(64), "64", int64(64)},
		{"u", uint(1), "1", int64(1)},
		{"u8", uint8(8), "8", int64(8)},
		{"u16", uint16(16), "16", int64(16)},
		{"u32", uint32(32), "32", int64(32)},
		{"u64", uint64(64), "64", int64(64)},
		{"ubig", uint64(math.MaxUint64), "18446744073709551615", new(big.Int).SetUint64(math.MaxUint64)},
		{"b", big1, big1.String(), big1},
		{"bsmall", big.NewInt(5), "5", int64(5)},
		{"bnil", (*big.Int)(nil), "nil", nil},
		{"f", 1.5, "1.5", 1.5},
		{"f32", float32(0.5), "0.5", 0.5},
		{"c", complex(1, 2), "(1.0+2.0i)", complex(1, 2)},
		{"c64", complex64(complex(1, -2)), "(1.0-2.0i)", complex(1, -2)},
		{"r", rat, "(1/3)", rat},
		{"rnil", (*big.Rat)(nil), "nil", nil},
		{"s", "héllo", `"héllo"`, "héllo"},
		{"by", []byte("ab"), `"ab"`, "ab"},
		{"sym", ruby.Symbol("k"), ":k", ruby.Symbol("k")},
		{"fs", []float64{1, 2.5}, "[1.0, 2.5]", []any{1.0, 2.5}},
		{"ga", [2]string{"a", "b"}, `["a", "b"]`, []any{"a", "b"}},
		{"ns", []int(nil), "nil", nil},
		{"m", map[string]int{"a": 1}, `{"a" => 1}`, map[any]any{"a": int64(1)}},
		{"nm", map[string]int(nil), "nil", nil},
		{"ndnil", (*nd.Array)(nil), "nil", nil},
	}
	for _, c := range cases {
		if err := in.Set(c.name, c.in); err != nil {
			t.Fatalf("Set(%s): %v", c.name, err)
		}
		if got := mustEval(t, in, c.name).Inspect(); got != c.inspect {
			t.Errorf("%s: inspect %s, want %s", c.name, got, c.inspect)
		}
		got, err := in.Get(c.name)
		if err != nil {
			t.Fatalf("Get(%s): %v", c.name, err)
		}
		if !reflect.DeepEqual(got, c.back) {
			t.Errorf("%s: Get = %#v, want %#v", c.name, got, c.back)
		}
	}

	// An NDArray is the same array on both sides.
	if err := in.Set("a", arr); err != nil {
		t.Fatal(err)
	}
	mustEval(t, in, "b = a * 2")
	got, _ := in.Get("a")
	if got.(*nd.Array) != arr {
		t.Fatal("Get returned another array")
	}
	if g, _ := in.Get("b"); g.(*nd.Array).Repr() != "array([2., 4., 6.])" {
		t.Fatalf("b = %s", g.(*nd.Array).Repr())
	}

	// An opaque Go value comes back as itself.
	p := &point{1, 2}
	in.Set("pt", p)
	if got := mustEval(t, in, "pt").Inspect(); got != "#<GoValue *ruby_test.point>" {
		t.Fatalf("opaque inspect = %s", got)
	}
	if g, _ := in.Get("pt"); g != p {
		t.Fatal("opaque value changed identity")
	}
	if got := mustEval(t, in, "pt.class").Inspect(); got != "GoValue" {
		t.Fatal(got)
	}

	// Ruby values with no Go equivalent stay Values, and round-trip.
	mustEval(t, in, "o = Object.new; h = {[1] => 2, nil => 3, o => 4}; cy = [1]; cy << cy; ch = {}; ch[:me] = ch; cx = Complex(1, Rational(1, 2))")
	h, _ := in.Get("h")
	hm := h.(map[any]any)
	if len(hm) != 3 || hm[nil] != int64(3) {
		t.Fatalf("h = %#v", hm)
	}
	for k := range hm {
		if v, ok := k.(ruby.Value); ok && v.ClassName() == "Array" {
			if v.Inspect() != "[1]" {
				t.Fatal(v.Inspect())
			}
		}
	}
	cy, _ := in.Get("cy")
	if s := cy.([]any); len(s) != 2 {
		t.Fatalf("cy = %v", s)
	}
	ch, _ := in.Get("ch")
	if m := ch.(map[any]any); len(m) != 1 {
		t.Fatalf("ch = %v", m)
	}
	cx, _ := in.Get("cx")
	if v, ok := cx.(ruby.Value); !ok || v.ClassName() != "Complex" {
		t.Fatalf("cx = %#v", cx)
	}
	o, _ := in.Get("o")
	ov := o.(ruby.Value)
	in.Set("o2", ov)
	if got := mustEval(t, in, "o2.equal?(o)").Inspect(); got != "true" {
		t.Fatal("a Value did not round-trip as the same object")
	}

	// Assigning an existing local replaces it.
	in.Set("i", "again")
	if got := mustEval(t, in, "i").Inspect(); got != `"again"` {
		t.Fatal(got)
	}

	// Errors.
	if err := in.Set("Bad", 1); err == nil {
		t.Fatal("Set accepted a constant name")
	}
	if _, err := in.Get("missing"); err == nil {
		t.Fatal("Get of a missing local succeeded")
	}
	other := ruby.New()
	ov2, _ := other.Eval("Object.new", "o.rb", 1)
	if err := in.Set("x", ov2); err == nil {
		t.Fatal("Set accepted another interpreter's Value")
	}
	if err := in.Set("x", []any{ov2}); err == nil {
		t.Fatal("Set accepted another interpreter's Value inside a slice")
	}
	if err := in.Set("x", map[string]any{"k": ov2}); err == nil {
		t.Fatal("Set accepted another interpreter's Value inside a map")
	}
	if err := in.Set("x", map[any]int{ov2: 1}); err == nil {
		t.Fatal("Set accepted another interpreter's Value as a map key")
	}
}

func TestIsLocalName(t *testing.T) {
	for s, want := range map[string]bool{
		"x": true, "_x": true, "aB9": true, "été": true,
		"": false, "X": false, "9a": false, "a-b": false, "end": false, "self": false,
	} {
		if got := ruby.IsLocalName(s); got != want {
			t.Errorf("IsLocalName(%q) = %v", s, got)
		}
	}
}

func TestValue(t *testing.T) {
	in, _, _ := newInterp(t)
	var zero ruby.Value
	if !zero.IsNil() || zero.Inspect() != "nil" || zero.String() != "" || zero.ClassName() != "NilClass" || zero.Go() != nil {
		t.Fatal("zero Value is not nil")
	}
	if _, err := zero.Call("to_s"); err == nil {
		t.Fatal("Call on a zero Value succeeded")
	}
	v := mustEval(t, in, `class Odd; def inspect = raise("no"); def to_s = raise("no"); end; Odd.new`)
	if v.Inspect() == "" || v.String() == "" {
		t.Fatal("a raising inspect/to_s must still render")
	}
	if got := mustEval(t, in, "class NotS; def to_s = 1; end; NotS.new").String(); !strings.HasPrefix(got, "#<NotS") {
		t.Fatalf("a to_s that is not a String: %q", got)
	}
	if got := mustEval(t, in, "class HasS; def to_s = 'custom'; end; HasS.new").String(); got != "custom" {
		t.Fatalf("to_s = %q", got)
	}
	other := ruby.New()
	ov, _ := other.Eval("1", "o.rb", 1)
	s := mustEval(t, in, `"abc"`)
	if s.String() != "abc" || s.Inspect() != `"abc"` || s.IsNil() || s.ClassName() != "String" {
		t.Fatal("String value")
	}
	if v, err := in.ValueOf([]int{1, 2}); err != nil || v.Inspect() != "[1, 2]" {
		t.Fatalf("ValueOf = %v %v", v, err)
	}
	if _, err := in.ValueOf(ov); err == nil {
		t.Fatal("ValueOf accepted another interpreter's Value")
	}
	r, err := s.Call("center", 7, "*")
	if err != nil || r.Go() != "**abc**" {
		t.Fatalf("Call = %v %v", r, err)
	}
	if _, err := s.Call("nope"); err == nil {
		t.Fatal("Call of a missing method succeeded")
	}
	if _, err := s.Call("+", ov); err == nil {
		t.Fatal("Call accepted another interpreter's Value")
	}
}

type normal struct{ mu, sigma float64 }

func (n *normal) pdf(x float64) float64 {
	z := (x - n.mu) / n.sigma
	return math.Exp(-z*z/2) / (n.sigma * math.Sqrt(2*math.Pi))
}

func TestDefine(t *testing.T) {
	in, out, _ := newInterp(t)
	stats, err := in.DefineModule("Stats")
	if err != nil {
		t.Fatal(err)
	}
	if again, _ := in.DefineModule("Stats"); again.Name() != "Stats" {
		t.Fatal("DefineModule did not find the existing module")
	}
	if stats.Value().Inspect() != "Stats" {
		t.Fatal(stats.Value().Inspect())
	}
	cls, err := stats.DefineClass("Normal", nil)
	if err != nil || cls.Name() != "Stats::Normal" {
		t.Fatalf("DefineClass: %v %v", cls, err)
	}
	cls.Bind(&normal{})
	cls.DefineSingletonMethod("new", func(c *ruby.Call) (any, error) {
		mu, sigma := 0.0, 1.0
		if v, ok := c.Kwargs["loc"]; ok {
			mu = toF(v)
		}
		if v, ok := c.Kwargs["scale"]; ok {
			sigma = toF(v)
		}
		if sigma <= 0 {
			return nil, ruby.NewError("ArgumentError", "scale must be positive")
		}
		return &normal{mu, sigma}, nil
	})
	cls.DefineMethod("pdf", func(c *ruby.Call) (any, error) {
		n := c.Self.Go().(*normal)
		return n.pdf(toF(c.Args[0])), nil
	})
	cls.DefineMethod("inspect", func(c *ruby.Call) (any, error) {
		n := c.Self.Go().(*normal)
		return fmt.Sprintf("#<Stats::Normal loc=%g scale=%g>", n.mu, n.sigma), nil
	})
	got := mustEval(t, in, "d = Stats::Normal.new(loc: 1, scale: 2.0)\nd.pdf(1.5)").Go().(float64)
	if want := (&normal{1, 2}).pdf(1.5); got != want {
		t.Fatalf("pdf = %v, want %v", got, want)
	}
	if got := mustEval(t, in, "!!d").Inspect(); got != "true" {
		t.Fatal("an opaque object is truthy")
	}
	if s := mustEval(t, in, "d").Inspect(); s != "#<Stats::Normal loc=1 scale=2>" {
		t.Fatal(s)
	}
	mustEval(t, in, "p d; puts d.class, d.is_a?(Stats::Normal)")
	if out.String() != "#<Stats::Normal loc=1 scale=2>\nStats::Normal\ntrue\n" {
		t.Fatalf("out = %q", out.String())
	}
	if s := cls.Wrap(&normal{0, 1}).Inspect(); s != "#<Stats::Normal loc=0 scale=1>" {
		t.Fatal(s)
	}
	// to_s of an instance without its own falls back to #<Class>.
	if s := mustEval(t, in, `"#{d}"`).Go(); s != "#<Stats::Normal>" {
		t.Fatalf("to_s = %v", s)
	}

	// An *Error raises its class; rescue sees it.
	if got := mustEval(t, in, "begin; Stats::Normal.new(scale: -1); rescue ArgumentError => e; e.message; end").Go(); got != "scale must be positive" {
		t.Fatalf("rescue = %v", got)
	}
	// A positional Hash is not keywords when the site says so.
	stats.DefineSingletonMethod("argc", func(c *ruby.Call) (any, error) {
		return []any{len(c.Args), len(c.Kwargs)}, nil
	})
	for src, want := range map[string]string{
		"Stats.argc(1, a: 2)":         "[1, 1]",
		"Stats.argc({a: 2})":          "[1, 0]",
		"Stats.argc({'a' => 2})":      "[1, 0]",
		"Stats.argc({})":              "[1, 0]",
		"h = {a: 1}; Stats.argc(**h)": "[0, 1]",
		"Stats.argc(a: 1, 'b' => 2)":  "[1, 0]",
	} {
		if got := mustEval(t, in, src).Inspect(); got != want {
			t.Errorf("%s = %s, want %s", src, got, want)
		}
	}

	// Blocks: called from Go, their exceptions re-raised unchanged, break honoured.
	stats.DefineSingletonMethod("each_twice", func(c *ruby.Call) (any, error) {
		if c.Block == nil {
			return nil, errors.New("no block given")
		}
		var acc []any
		for i := range 2 {
			r, err := c.Block.Call(i, c.Block.Arity())
			if err != nil {
				return nil, err
			}
			acc = append(acc, r)
		}
		return acc, nil
	})
	if got := mustEval(t, in, "Stats.each_twice { |i, a| i * 10 + a }").Inspect(); got != "[2, 12]" {
		t.Fatalf("block = %s", got)
	}
	if got := mustEval(t, in, "catch(:done) { Stats.each_twice { |i| throw :done, :thrown if i == 1 } }").Inspect(); got != ":thrown" {
		t.Fatalf("throw through a Go method = %s", got)
	}
	if got := mustEval(t, in, "def early = Stats.each_twice { |i| return :returned }\nearly").Inspect(); got != ":returned" {
		t.Fatalf("return through a Go method = %s", got)
	}
	if got := mustEval(t, in, "Stats.each_twice { |i| break :out if i == 1; i }").Inspect(); got != ":out" {
		t.Fatalf("break = %s", got)
	}
	if got := mustEval(t, in, "class MyErr < StandardError; end\nbegin; Stats.each_twice { raise MyErr, 'x' }; rescue MyErr => e; e.class; end").Inspect(); got != "MyErr" {
		t.Fatalf("rescued %s", got)
	}
	if got := mustEval(t, in, "begin; Stats.each_twice; rescue => e; [e.class, e.message]; end").Inspect(); got != `[RuntimeError, "no block given"]` {
		t.Fatalf("plain error = %s", got)
	}
	stats.DefineSingletonMethod("blockbad", func(c *ruby.Call) (any, error) {
		_, err := c.Block.Call(make(chan int))
		return nil, err
	})
	// A chan is opaque, so that converts; a Value of another interpreter does not.
	other := ruby.New()
	ov, _ := other.Eval("1", "o.rb", 1)
	stats.DefineSingletonMethod("blockother", func(c *ruby.Call) (any, error) {
		return c.Block.Call(ov)
	})
	if _, err := in.Eval("Stats.blockother { }", "x.rb", 1); err == nil {
		t.Fatal("Block.Call accepted another interpreter's Value")
	}
	stats.DefineSingletonMethod("retother", func(c *ruby.Call) (any, error) { return ov, nil })
	_, err = in.Eval("Stats.retother", "x.rb", 1)
	var e *ruby.Error
	if !errors.As(err, &e) || e.Class != "TypeError" {
		t.Fatalf("returning another interpreter's Value: %v", err)
	}
	mustEval(t, in, "Stats.blockbad { |c| c }")

	// A Go panic that is not a runtime error is not laundered into an exception.
	stats.DefineSingletonMethod("panics", func(c *ruby.Call) (any, error) { panic("host bug") })
	func() {
		defer func() {
			if r := recover(); r != "host bug" {
				t.Fatalf("recovered %v", r)
			}
		}()
		in.Eval("Stats.panics", "x.rb", 1)
		t.Fatal("the panic did not propagate")
	}()
	if got := mustEval(t, in, "1 + 2").Inspect(); got != "3" {
		t.Fatal("the interpreter is unusable after a host panic")
	}

	// Constants.
	if err := stats.SetConst("VERSION", "1.0"); err != nil {
		t.Fatal(err)
	}
	if got := mustEval(t, in, "Stats::VERSION").Go(); got != "1.0" {
		t.Fatal(got)
	}
	if err := stats.SetConst("lower", 1); err == nil {
		t.Fatal("SetConst accepted a lower-case name")
	}
	if err := stats.SetConst("X", ov); err == nil {
		t.Fatal("SetConst accepted another interpreter's Value")
	}
	top, _ := in.DefineClass("TopThing", cls)
	if got := mustEval(t, in, "TopThing.superclass").Inspect(); got != "Stats::Normal" {
		t.Fatal(got)
	}
	_ = top
	nested, err := in.DefineModule("A::B::C")
	if err != nil || nested.Name() != "A::B::C" {
		t.Fatalf("nested: %v %v", nested, err)
	}
	if _, err := in.DefineModule("Stats::Normal"); err == nil {
		t.Fatal("DefineModule over a class succeeded")
	}
	if _, err := in.DefineClass("Stats", nil); err == nil {
		t.Fatal("DefineClass over a module succeeded")
	}
	if _, err := in.DefineModule("bad"); err == nil {
		t.Fatal("DefineModule accepted a lower-case name")
	}
	if _, err := in.DefineModule("A::b"); err == nil {
		t.Fatal("DefineModule accepted a lower-case part")
	}
	if _, err := in.DefineModule("A-B"); err == nil {
		t.Fatal("DefineModule accepted a dash")
	}
	if _, err := stats.DefineModule("Inner"); err != nil {
		t.Fatal(err)
	}
	if mustEval(t, in, "Stats::Inner").Inspect() != "Stats::Inner" {
		t.Fatal("Stats::Inner")
	}
	if c, err := nested.DefineClass("D", nil); err != nil || c.Name() != "A::B::C::D" {
		t.Fatalf("nested class: %v %v", c, err)
	}
	if mustEval(t, in, "A::B::C::D.new.class").Inspect() != "A::B::C::D" {
		t.Fatal("A::B::C::D")
	}
}

func toF(v ruby.Value) float64 {
	switch x := v.Go().(type) {
	case int64:
		return float64(x)
	case float64:
		return x
	}
	return math.NaN()
}

// The interpreter runs Ruby exactly as Run does: the same program prints the same.
func TestInterpMatchesRun(t *testing.T) {
	src := `def fib(n) = n < 2 ? n : fib(n - 1) + fib(n - 2)
p (0..10).map { fib(_1) }
p NDArray.arange(4).reshape(2, 2)`
	var a bytes.Buffer
	if err := ruby.Run(src, &a); err != nil {
		t.Fatal(err)
	}
	in, out, _ := newInterp(t)
	mustEval(t, in, src)
	if a.String() != out.String() {
		t.Fatalf("Run %q, Interp %q", a.String(), out.String())
	}
}
