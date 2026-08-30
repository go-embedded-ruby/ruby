package vm_test

import (
	"strings"
	"testing"
)

// TestMarshalConformance pins the MRI-4.0.x Marshal behaviours added to the VM's
// codec (internal/vm/marshal.go and marshal_codec.go). Every expected value in
// this file was checked byte-for-byte against the reference `ruby` interpreter.
func TestMarshalConformance(t *testing.T) {
	cases := []struct{ name, src, want string }{
		// --- encoded symbols: a non-ASCII symbol is 'I'-wrapped with :E=>true,
		//     and the E symbol is shared across the stream -----------------------
		{"sym_encoded_pair",
			`p Marshal.dump(["€a".to_sym, "€b".to_sym]).bytes`,
			"[4, 8, 91, 7, 73, 58, 9, 226, 130, 172, 97, 6, 58, 6, 69, 84, 73, 58, 9, 226, 130, 172, 98, 6, 59, 6, 84]\n"},
		{"sym_encoded_roundtrip",
			`p Marshal.load(Marshal.dump(:€x))`, ":\"€x\"\n"},
		{"sym_ascii_unwrapped",
			`p Marshal.dump(:sym).bytes`, "[4, 8, 58, 8, 115, 121, 109]\n"},
		// A non-ASCII ivar name is likewise wrapped on dump and unwrapped on load.
		{"sym_encoded_ivar_dump",
			`o = Object.new; o.instance_variable_set("@é", 1); p Marshal.dump(o).bytes`,
			"[4, 8, 111, 58, 11, 79, 98, 106, 101, 99, 116, 6, 73, 58, 8, 64, 195, 169, 6, 58, 6, 69, 84, 105, 6]\n"},
		{"sym_encoded_ivar_load",
			`o = Marshal.load("\x04\bo:\vObject\x06I:\b@\xC3\xA9\x06:\x06ETi\x06"); p o.instance_variable_get(:@é)`,
			"1\n"},

		// --- Bignum object links: a repeated Bignum links; a repeated large
		//     (immediate) Integer does not ------------------------------------
		{"bignum_links",
			`x = 2**64; p Marshal.dump([x, x]).bytes`,
			"[4, 8, 91, 7, 108, 43, 10, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 64, 6]\n"},
		{"bignum_roundtrip",
			`x = 2**64; c = Marshal.load(Marshal.dump([x, x])); p [c[0] == x, c[0].equal?(c[1])]`,
			"[true, true]\n"},
		{"integer_large_not_linked",
			`x = 2**40; p Marshal.dump([x, x]).bytes`,
			"[4, 8, 91, 7, 108, 43, 8, 0, 0, 0, 0, 0, 1, 108, 43, 8, 0, 0, 0, 0, 0, 1]\n"},

		// --- compare_by_identity Hash: dumped in a 'C':Hash carrier and the flag
		//     restored on load --------------------------------------------------
		{"cbi_hash_dump",
			`h = {}; h.compare_by_identity; p Marshal.dump(h).bytes`,
			"[4, 8, 67, 58, 9, 72, 97, 115, 104, 123, 0]\n"},
		{"cbi_hash_roundtrip",
			`h = {}; h.compare_by_identity; p Marshal.load(Marshal.dump(h)).compare_by_identity?`,
			"true\n"},

		// --- recursion limit (Marshal.dump's 3rd argument) --------------------
		{"limit_ok",
			`p Marshal.dump([1, [2]], 5).class`, "String\n"},
		{"limit_three_arg_form",
			`p Marshal.dump(1, nil, 5).bytes`, "[4, 8, 105, 6]\n"},

		// --- Exception dump/load: :mesg / :bt pseudo-ivars --------------------
		{"exc_empty_dump",
			`p Marshal.dump(Exception.new).bytes`,
			"[4, 8, 111, 58, 14, 69, 120, 99, 101, 112, 116, 105, 111, 110, 7, 58, 9, 109, 101, 115, 103, 48, 58, 7, 98, 116, 48]\n"},
		{"exc_message_dump",
			`p Marshal.dump(Exception.new("foo")).bytes`,
			"[4, 8, 111, 58, 14, 69, 120, 99, 101, 112, 116, 105, 111, 110, 7, 58, 9, 109, 101, 115, 103, 73, 34, 8, 102, 111, 111, 6, 58, 6, 69, 84, 58, 7, 98, 116, 48]\n"},
		{"exc_message_load",
			`p Marshal.load("\x04\bo:\x0EException\a:\tmesg\"\bfoo:\abt0").message`, "\"foo\"\n"},
		{"exc_no_message_load",
			`p Marshal.load("\x04\bo:\x0EException\a:\tmesg0:\abt0").message`, "\"Exception\"\n"},
		{"exc_backtrace_load",
			`p Marshal.load("\x04\bo:\016Exception\a:\abt[\006\"\022foo/bar.rb:10:\tmesg\"\bfoo").backtrace`,
			"[\"foo/bar.rb:10\"]\n"},
		{"exc_message_roundtrip",
			`p Marshal.load(Marshal.dump(RuntimeError.new("boom"))).message`, "\"boom\"\n"},
		{"exc_ivar_roundtrip",
			`e = RuntimeError.new("x"); e.instance_variable_set(:@k, 5); p Marshal.load(Marshal.dump(e)).instance_variable_get(:@k)`,
			"5\n"},

		// --- Float load: an old-format NUL + mantissa suffix is trimmed -------
		{"float_load_with_mantissa",
			`p Marshal.load("\x04\bf\v1.3\x00\xcc\xcd")`, "1.3\n"},

		// --- class / module references ---------------------------------------
		{"old_module_load",
			`p Marshal.load("\x04\bM\vKernel") == Kernel`, "true\n"},
		{"named_module_dump",
			`p Marshal.dump(Comparable).bytes`,
			"[4, 8, 109, 15, 67, 111, 109, 112, 97, 114, 97, 98, 108, 101]\n"},
		{"named_module_roundtrip",
			`p Marshal.load(Marshal.dump(Comparable)) == Comparable`, "true\n"},

		// --- load proc: its return value replaces the loaded object -----------
		{"proc_return_replaces",
			`p Marshal.load(Marshal.dump([1, 2]), proc { [3, 4] })`, "[3, 4]\n"},
		{"proc_visits_each",
			`seen = []; Marshal.load(Marshal.dump([1, 2]), proc { |o| seen << o; o }); p seen`,
			"[1, 2, [1, 2]]\n"},

		// --- dump to an IO: #binmode is called when defined; a writer without
		//     #binmode still works ----------------------------------------------
		{"io_binmode_called",
			`class WBM; def binmode; print "BM "; end; def write(x); end; end; Marshal.dump("x", WBM.new); puts`,
			"BM \n"},
		{"io_without_binmode",
			`class WNB; def write(x); end; end; p Marshal.dump("x", WNB.new).class.name`, "\"WNB\"\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := eval(t, c.src); got != c.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
			}
		})
	}
}

// TestMarshalConformanceErrors pins the error conditions added to the codec.
func TestMarshalConformanceErrors(t *testing.T) {
	cases := []struct{ name, src, want string }{
		// Recursion limit exceeded -> ArgumentError at the offending depth.
		{"limit_zero_any_value", `Marshal.dump([], 0)`, "exceed depth limit"},
		{"limit_bare_value", `Marshal.dump(1, 0)`, "exceed depth limit"},
		{"limit_nested", `Marshal.dump([[[]]], 1)`, "exceed depth limit"},
		{"limit_three_arg", `Marshal.dump([[[]]], nil, 1)`, "exceed depth limit"},

		// A non-writable, non-Integer second argument is not a valid IO.
		{"io_not_writable", `Marshal.dump("x", Object.new)`, "IO needed"},

		// Singleton objects cannot be dumped.
		{"singleton_method", `o = Object.new; def o.foo; end; Marshal.dump(o)`, "singleton can't be dumped"},
		{"singleton_ivar", `o = Object.new; class << o; @v = 1; end; Marshal.dump(o)`, "singleton can't be dumped"},

		// Anonymous classes/modules cannot be dumped.
		{"anon_module", `Marshal.dump(Module.new)`, "anonymous module"},
		{"anon_struct", `Marshal.dump(Struct.new(:a).new(1))`, "anonymous class"},
		{"anon_range_subclass", `Marshal.dump(Class.new(Range).new(1, 2))`, "anonymous class"},
		{"anon_marshal_dump",
			`class UMx; def marshal_dump; 0; end; def marshal_load(v); end; end; Marshal.dump(Class.new(UMx).new)`,
			"anonymous class"},
		{"anon_dump",
			`class UDx; def _dump(l); "x"; end; end; Marshal.dump(Class.new(UDx).new)`,
			"anonymous class"},

		// 'c'/'m' references insist on the right kind of constant.
		{"class_ref_needs_class", `Marshal.load("\x04\bc\vKernel")`, "does not refer to class"},
		{"module_ref_needs_module", `Marshal.load("\x04\bm\vString")`, "does not refer to module"},

		// An empty IO is at end of file.
		{"eof_empty_io", `require "stringio"; Marshal.load(StringIO.new(""))`, "end of file reached"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := runErr(t, c.src)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("src=%q got=%v want error containing %q", c.src, err, c.want)
			}
		})
	}
}
