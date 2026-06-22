package vm_test

import (
	"strings"
	"testing"
)

// TestMarshal covers the Marshal module (dump/load/restore + version constants),
// asserted against MRI Ruby 4.0.5. Marshal is a core module (no require).
func TestMarshal(t *testing.T) {
	cases := []struct{ src, want string }{
		// Byte-exactness vs MRI: the raw dump bytes.
		{`p Marshal.dump(123).bytes`, "[4, 8, 105, 1, 123]\n"},
		{`p Marshal.dump([1, 2, 3]).bytes`, "[4, 8, 91, 8, 105, 6, 105, 7, 105, 8]\n"},

		// Round-trips across the value types.
		{`p Marshal.load(Marshal.dump(123)) == 123`, "true\n"},
		{`p Marshal.load(Marshal.dump(-1000000000)) == -1000000000`, "true\n"},
		{`p Marshal.load(Marshal.dump(2**70)) == 2**70`, "true\n"},
		{`p Marshal.load(Marshal.dump(-(2**70))) == -(2**70)`, "true\n"},
		{`p Marshal.load(Marshal.dump(3.14)) == 3.14`, "true\n"},
		{`p Marshal.load(Marshal.dump(1.0/0)).infinite?`, "1\n"},
		{`p Marshal.load(Marshal.dump("héllo")) == "héllo"`, "true\n"},
		{`p Marshal.load(Marshal.dump(:sym)) == :sym`, "true\n"},
		{`p Marshal.load(Marshal.dump(true))`, "true\n"},
		{`p Marshal.load(Marshal.dump(false))`, "false\n"},
		{`p Marshal.load(Marshal.dump(nil)).nil?`, "true\n"},
		{`p Marshal.load(Marshal.dump([1, [2, 3], "x"])) == [1, [2, 3], "x"]`, "true\n"},
		{`p Marshal.load(Marshal.dump({"a" => 1, "b" => [true, nil]})) == {"a" => 1, "b" => [true, nil]}`, "true\n"},
		{`p Marshal.load(Marshal.dump({1 => 2, 3 => 4})) == {1 => 2, 3 => 4}`, "true\n"},

		// Hash default value survives a round-trip (a missing key reads it).
		{`p Marshal.load(Marshal.dump(Hash.new(0)))[:missing]`, "0\n"},

		// Object identity: shared elements (array, string, hash) stay shared.
		{`a = [1]; c = Marshal.load(Marshal.dump([a, a])); p c[0].equal?(c[1])`, "true\n"},
		{`s = "x"; c = Marshal.load(Marshal.dump([s, s])); p c[0].equal?(c[1])`, "true\n"},
		{`h = {1 => 2}; c = Marshal.load(Marshal.dump([h, h])); p c[0].equal?(c[1])`, "true\n"},
		// Cyclic structure reconstructs its self-reference.
		{`a = []; a << a; b = Marshal.load(Marshal.dump(a)); p b[0].equal?(b)`, "true\n"},

		// Version constants and the restore alias.
		{`p Marshal::MAJOR_VERSION`, "4\n"},
		{`p Marshal::MINOR_VERSION`, "8\n"},
		{`p Marshal.restore(Marshal.dump([1, 2])) == [1, 2]`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}

	errs := []struct{ src, want string }{
		{`Marshal.load(Marshal.dump(123)[0..2])`, "end of input"}, // truncated dump
		{`Marshal.dump(proc { 1 })`, "_dump"},                     // unsupported type
		{`Marshal.dump(Hash.new { |h, k| 0 })`, "default proc"},   // hash with default proc
		{`Marshal.load(42)`, "IO needed"},                         // non-String argument
	}
	for _, c := range errs {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got=%v want error containing %q", c.src, err, c.want)
		}
	}
}
