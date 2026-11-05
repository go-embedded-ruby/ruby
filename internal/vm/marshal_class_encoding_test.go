// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// Ruby 4.0 records the encoding of a class path that is not ASCII, the same way
// it records a String's. marshal.c's T_CLASS and T_MODULE arms bracket the
// bytes with w_encivar/w_encname:
//
//	VALUE encname = w_encivar(path, arg);   /* emits 'I' when path is non-ASCII */
//	w_byte(TYPE_CLASS, arg);
//	w_bytes(RSTRING_PTR(path), RSTRING_LEN(path), arg);
//	w_encname(encname, arg);                /* 1 ivar: :E => true               */
//
// The three ruby/spec examples behind `ruby_version_is "4.0"` in
// core/marshal/dump_spec.rb are the first three rows, with the byte strings
// copied from the spec rather than from this implementation's output.
func TestMarshalDumpsAClassPathsEncoding(t *testing.T) {
	const defs = `
module MarshalSpec; end
eval("class MarshalSpec::MultibyteぁあぃいClass; end".dup.force_encoding(Encoding::UTF_8))
eval("module MarshalSpec::MultibyteけげこごModule; end".dup.force_encoding(Encoding::UTF_8))
eval("class MarshalSpec::MultibyteぁあぃいTime < Time; end".dup.force_encoding(Encoding::UTF_8))
K  = eval("MarshalSpec::MultibyteぁあぃいClass".dup.force_encoding(Encoding::UTF_8))
M  = eval("MarshalSpec::MultibyteけげこごModule".dup.force_encoding(Encoding::UTF_8))
TC = eval("MarshalSpec::MultibyteぁあぃいTime".dup.force_encoding(Encoding::UTF_8))
`
	for _, tc := range []struct{ name, src, want string }{
		{"a class with a multibyte name", `p Marshal.dump(K)`,
			`"\x04\bIc,MarshalSpec::Multibyte\xE3\x81\x81\xE3\x81\x82\xE3\x81\x83\xE3\x81\x84Class\x06:\x06ET"`},
		{"a module with a multibyte name", `p Marshal.dump(M)`,
			`"\x04\bIm-MarshalSpec::Multibyte\xE3\x81\x91\xE3\x81\x92\xE3\x81\x93\xE3\x81\x94Module\x06:\x06ET"`},
		{"a Time subclass with a multibyte name", `p Marshal.dump(TC)`,
			`"\x04\bIc+MarshalSpec::Multibyte\xE3\x81\x81\xE3\x81\x82\xE3\x81\x83\xE3\x81\x84Time\x06:\x06ET"`},

		// The dump has to be readable by the reader that already exists: the
		// 'I' wrapper in front of a 'c' is a shape it had never been handed.
		{"the class round-trips", `p(Marshal.load(Marshal.dump(K)).equal?(K))`, "true"},
		{"the module round-trips", `p(Marshal.load(Marshal.dump(M)).equal?(M))`, "true"},
		{"the Time subclass round-trips", `p(Marshal.load(Marshal.dump(TC)).equal?(TC))`, "true"},

		// ⛔ THE CONTROL. w_encivar returns nil for an ASCII-only path, so an
		// ordinary class name must dump byte-for-byte as it did before. A
		// wrapper emitted unconditionally would still pass every row above.
		{"an ASCII class name is unwrapped", `p Marshal.dump(String)`, `"\x04\bc\vString"`},
		{"an ASCII module name is unwrapped", `p Marshal.dump(Marshal)`, `"\x04\bm\fMarshal"`},

		// Object links still point at the start of the wrapped form, not at the
		// 'c' inside it: @\x06 is index 1, the array being 0.
		{"a repeated multibyte class is a link", `p Marshal.dump([K, K])`,
			`"\x04\b[\aIc,MarshalSpec::Multibyte\xE3\x81\x81\xE3\x81\x82\xE3\x81\x83\xE3\x81\x84Class\x06:\x06ET@\x06"`},
		{"the link round-trips to the same object",
			`p(Marshal.load(Marshal.dump([K, K])).map { |c| c.equal?(K) })`, "[true, true]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := runSrc(t, defs+tc.src); got != tc.want {
				t.Errorf("%s\n got %s\nwant %s", tc.src, got, tc.want)
			}
		})
	}
}

// classPathEncIvar is the whole decision, so it is worth testing on its own:
// the wrapper appears for a non-ASCII byte anywhere in the name and nowhere
// else. An empty name cannot reach it — writeClassRef refuses an anonymous
// class first — but it must not panic if it ever does.
func TestClassPathEncIvarIsAboutTheBytes(t *testing.T) {
	for _, tc := range []struct {
		name string
		want bool
	}{
		{"String", false},
		{"MarshalSpec::ClassWithOverriddenName", false},
		{"", false},
		{"\x7f", false},               // the last ASCII byte
		{"AぁB", true},                 // multibyte in the middle
		{"ぁ", true},                   // multibyte alone
		{"MarshalSpec::ぁClass", true}, // the shape the specs use
	} {
		if got := classPathEncIvar(tc.name) != nil; got != tc.want {
			t.Errorf("classPathEncIvar(%q) wrapped=%v, want %v", tc.name, got, tc.want)
		}
	}
	// And when it does answer, it answers with the short :E => true form —
	// encoding_name maps UTF-8 to Qtrue, and there is no way to name a constant
	// in any other encoding.
	e := classPathEncIvar("ぁ")
	if e.sym != "E" || e.tf != 'T' {
		t.Errorf("classPathEncIvar gave sym=%q tf=%q, want \"E\" and 'T'", e.sym, e.tf)
	}
}
