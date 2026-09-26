package vm_test

import "testing"

// TestMarshalWave46Ivars pins Marshal's handling of the instance variables of
// the kinds that have no ivar field of their own -- String, Array, Hash, Regexp
// -- and of the String a #_dump hook returns.
//
// Every want value was produced by running the SAME source under the reference
// `ruby` (MRI 4.0.5) and capturing its output verbatim, so these are witnesses
// to MRI's bytes rather than transcriptions of rbgo's. The generator refused any
// case MRI could not run.
//
// Cases marked "witness" FAIL on the code before this change: it consulted only
// the encoding ivar when dumping, and dropped every non-encoding ivar of a
// String or Regexp when loading. Cases marked "guard" PASS before and after --
// they are here so that emitting a record MRI does not expect cannot pass
// unnoticed, which is the way a fix like this breaks specs that used to pass by
// omission.
func TestMarshalWave46Ivars(t *testing.T) {
	cases := []struct{ name, src, want string }{
		// witness
		{"str_one_ivar",
			"s = \"ab\".dup; s.instance_variable_set(:@foo, \"bar\"); p Marshal.dump(s).bytes",
			"[4, 8, 73, 34, 7, 97, 98, 7, 58, 6, 69, 84, 58, 9, 64, 102, 111, 111, 73, 34, 8, 98, 97, 114, 6, 59, 0, 84]\n"},
		// witness
		{"str_three_ivars",
			"s = \"ab\".dup; s.instance_variable_set(:@a, 1); s.instance_variable_set(:@b, :sym); s.instance_variable_set(:@c, [1]); p Marshal.dump(s).bytes",
			"[4, 8, 73, 34, 7, 97, 98, 9, 58, 6, 69, 84, 58, 7, 64, 97, 105, 6, 58, 7, 64, 98, 58, 8, 115, 121, 109, 58, 7, 64, 99, 91, 6, 105, 6]\n"},
		// witness
		{"str_usascii_ivar",
			"s = \"ab\".dup.force_encoding(\"US-ASCII\"); s.instance_variable_set(:@foo, 1); p Marshal.dump(s).bytes",
			"[4, 8, 73, 34, 7, 97, 98, 7, 58, 6, 69, 70, 58, 9, 64, 102, 111, 111, 105, 6]\n"},
		// witness
		{"str_latin1_ivar",
			"s = \"ab\".dup.force_encoding(\"ISO-8859-1\"); s.instance_variable_set(:@foo, 1); p Marshal.dump(s).bytes",
			"[4, 8, 73, 34, 7, 97, 98, 7, 58, 13, 101, 110, 99, 111, 100, 105, 110, 103, 34, 15, 73, 83, 79, 45, 56, 56, 53, 57, 45, 49, 58, 9, 64, 102, 111, 111, 105, 6]\n"},
		// witness
		{"str_binary_ivar_only",
			"s = \"ab\".dup.force_encoding(\"binary\"); s.instance_variable_set(:@foo, 1); p Marshal.dump(s).bytes",
			"[4, 8, 73, 34, 7, 97, 98, 6, 58, 9, 64, 102, 111, 111, 105, 6]\n"},
		// guard
		{"str_binary_no_ivar",
			"s = \"ab\".dup.force_encoding(\"binary\"); p Marshal.dump(s).bytes",
			"[4, 8, 34, 7, 97, 98]\n"},
		// guard
		{"str_no_ivar_utf8",
			"p Marshal.dump(\"ab\".dup).bytes",
			"[4, 8, 73, 34, 7, 97, 98, 6, 58, 6, 69, 84]\n"},
		// witness
		{"str_ivar_nested_ivar",
			"i = \"in\".dup; i.instance_variable_set(:@deep, 1); s = \"ou\".dup; s.instance_variable_set(:@inner, i); p Marshal.dump(s).bytes",
			"[4, 8, 73, 34, 7, 111, 117, 7, 58, 6, 69, 84, 58, 11, 64, 105, 110, 110, 101, 114, 73, 34, 7, 105, 110, 7, 59, 0, 84, 58, 10, 64, 100, 101, 101, 112, 105, 6]\n"},
		// witness
		{"str_removed_ivar",
			"s = \"ab\".dup; s.instance_variable_set(:@a, 1); s.instance_variable_set(:@b, 2); s.send(:remove_instance_variable, :@a); p Marshal.dump(s).bytes",
			"[4, 8, 73, 34, 7, 97, 98, 7, 58, 6, 69, 84, 58, 7, 64, 98, 105, 7]\n"},
		// witness
		{"str_ext_and_ivar",
			"module Mv; end; s = \"ab\".dup; s.instance_variable_set(:@foo, 1); s.extend(Mv); p Marshal.dump(s).bytes",
			"[4, 8, 73, 101, 58, 7, 77, 118, 34, 7, 97, 98, 7, 58, 6, 69, 84, 58, 9, 64, 102, 111, 111, 105, 6]\n"},
		// witness
		{"ary_ivar",
			"a = [1]; a.instance_variable_set(:@foo, 1); p Marshal.dump(a).bytes",
			"[4, 8, 73, 91, 6, 105, 6, 6, 58, 9, 64, 102, 111, 111, 105, 6]\n"},
		// guard
		{"ary_no_ivar",
			"p Marshal.dump([1]).bytes",
			"[4, 8, 91, 6, 105, 6]\n"},
		// witness
		{"ary_ext_ivar",
			"module Mw; end; a = [1]; a.instance_variable_set(:@foo, 1); a.extend(Mw); p Marshal.dump(a).bytes",
			"[4, 8, 73, 101, 58, 7, 77, 119, 91, 6, 105, 6, 6, 58, 9, 64, 102, 111, 111, 105, 6]\n"},
		// witness
		{"hash_ivar",
			"h = {1=>2}; h.instance_variable_set(:@foo, 1); p Marshal.dump(h).bytes",
			"[4, 8, 73, 123, 6, 105, 6, 105, 7, 6, 58, 9, 64, 102, 111, 111, 105, 6]\n"},
		// witness
		{"hash_default_ivar",
			"h = Hash.new(9); h[1] = 2; h.instance_variable_set(:@foo, 1); p Marshal.dump(h).bytes",
			"[4, 8, 73, 125, 6, 105, 6, 105, 7, 105, 14, 6, 58, 9, 64, 102, 111, 111, 105, 6]\n"},
		// witness
		{"hash_identity_ivar",
			"h = {}.compare_by_identity; h.instance_variable_set(:@foo, 1); p Marshal.dump(h).bytes",
			"[4, 8, 73, 67, 58, 9, 72, 97, 115, 104, 123, 0, 6, 58, 9, 64, 102, 111, 111, 105, 6]\n"},
		// guard
		{"hash_no_ivar",
			"p Marshal.dump({1=>2}).bytes",
			"[4, 8, 123, 6, 105, 6, 105, 7]\n"},
		// witness
		{"regexp_ivar",
			"r = Regexp.new(\"ab\"); r.instance_variable_set(:@foo, 1); p Marshal.dump(r).bytes",
			"[4, 8, 73, 47, 7, 97, 98, 0, 7, 58, 6, 69, 70, 58, 9, 64, 102, 111, 111, 105, 6]\n"},
		// guard
		{"regexp_no_ivar",
			"p Marshal.dump(Regexp.new(\"ab\")).bytes",
			"[4, 8, 73, 47, 7, 97, 98, 0, 6, 58, 6, 69, 70]\n"},
		// witness
		{"link_same_ivar_string_twice",
			"s = \"ab\".dup; s.instance_variable_set(:@foo, \"bar\"); p Marshal.dump([s, s]).bytes",
			"[4, 8, 91, 7, 73, 34, 7, 97, 98, 7, 58, 6, 69, 84, 58, 9, 64, 102, 111, 111, 73, 34, 8, 98, 97, 114, 6, 59, 0, 84, 64, 6]\n"},
		// witness
		{"link_shared_ivar_value",
			"v = \"sh\".dup; s = \"ab\".dup; s.instance_variable_set(:@a, v); s.instance_variable_set(:@b, v); p Marshal.dump(s).bytes",
			"[4, 8, 73, 34, 7, 97, 98, 8, 58, 6, 69, 84, 58, 7, 64, 97, 73, 34, 7, 115, 104, 6, 59, 0, 84, 58, 7, 64, 98, 64, 6]\n"},
		// witness
		{"udump_payload_ivar",
			"class UDv; def _dump(d); s = \"pl\".dup; s.instance_variable_set(:@foo, \"bar\"); s; end; def self._load(x); new; end; end; p Marshal.dump(UDv.new).bytes",
			"[4, 8, 73, 117, 58, 8, 85, 68, 118, 7, 112, 108, 7, 58, 6, 69, 84, 58, 9, 64, 102, 111, 111, 73, 34, 8, 98, 97, 114, 6, 59, 6, 84]\n"},
		// witness
		{"udump_payload_ivar_link",
			"class UDw; def _dump(d); s = \"pl\".dup; s.instance_variable_set(:@foo, \"bar\"); s; end; def self._load(x); new; end; end; o = UDw.new; p Marshal.dump([o, o]).bytes",
			"[4, 8, 91, 7, 73, 117, 58, 8, 85, 68, 119, 7, 112, 108, 7, 58, 6, 69, 84, 58, 9, 64, 102, 111, 111, 73, 34, 8, 98, 97, 114, 6, 59, 6, 84, 64, 7]\n"},
		// guard
		{"udump_object_ivar_not_dumped",
			"class UDx; def initialize; @objiv = 1; end; def _dump(d); \"pl\".dup.force_encoding(\"binary\"); end; def self._load(x); new; end; end; p Marshal.dump(UDx.new).bytes",
			"[4, 8, 117, 58, 8, 85, 68, 120, 7, 112, 108]\n"},
		// witness
		{"load_str_ivar",
			"s = Marshal.load(\"\\x04\\bI\\\"\\aab\\a:\\x06ET:\\t@fooi\\x06\"); p [s, s.encoding.to_s, s.instance_variable_get(:@foo)]",
			"[\"ab\", \"UTF-8\", 1]\n"},
		// witness
		{"load_str_ivar_no_enc",
			"s = Marshal.load(\"\\x04\\bI\\\"\\aab\\x06:\\t@fooi\\x06\"); p [s, s.encoding.to_s, s.instance_variable_get(:@foo)]",
			"[\"ab\", \"ASCII-8BIT\", 1]\n"},
		// witness
		{"load_regexp_ivar",
			"r = Marshal.load(\"\\x04\\bI/\\aab\\x00\\a:\\x06EF:\\t@fooi\\x06\"); p [r.source, r.encoding.to_s, r.instance_variable_get(:@foo)]",
			"[\"ab\", \"US-ASCII\", 1]\n"},
		// guard
		{"load_ary_ivar",
			"a = Marshal.load(\"\\x04\\bI[\\x06i\\x06\\x06:\\t@fooi\\a\"); p [a, a.instance_variable_get(:@foo)]",
			"[[1], 2]\n"},
		// guard
		{"load_hash_ivar",
			"h = Marshal.load(\"\\x04\\bI{\\x06i\\x06i\\a\\x06:\\t@fooi\\b\"); p [h, h.instance_variable_get(:@foo)]",
			"[{1 => 2}, 3]\n"},
		// witness
		{"load_link_ivar_string_twice",
			"a = Marshal.load(\"\\x04\\b[\\aI\\\"\\aab\\a:\\x06ET:\\t@foo\\\"\\bbar@\\x06\"); p [a[0].equal?(a[1]), a[0].instance_variable_get(:@foo)]",
			"[true, \"bar\"]\n"},
		// witness
		{"roundtrip_str_ivar",
			"s = \"ab\".dup; s.instance_variable_set(:@foo, \"bar\"); t = Marshal.load(Marshal.dump(s)); p [t, t.instance_variables, t.instance_variable_get(:@foo)]",
			"[\"ab\", [:@foo], \"bar\"]\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := eval(t, c.src); got != c.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
			}
		})
	}
}
