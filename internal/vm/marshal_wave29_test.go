package vm_test

import (
	"strings"
	"testing"
)

// TestMarshalWave29 pins the Marshal and Time behaviours corrected in wave 29.
// Every expected value was produced by the reference `ruby` (MRI 4.0.5) running
// the same source, so these are witnesses rather than transcriptions: the
// generator ran each snippet under MRI and captured its output verbatim.
func TestMarshalWave29(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"ext_string",
			"module Mx; end; s = \"cd\".dup; s.extend(Mx); p Marshal.dump(s).bytes",
			"[4, 8, 73, 101, 58, 7, 77, 120, 34, 7, 99, 100, 6, 58, 6, 69, 84]\n"},
		{"ext_array",
			"module Mx; end; a = [1]; a.extend(Mx); p Marshal.dump(a).bytes",
			"[4, 8, 101, 58, 7, 77, 120, 91, 6, 105, 6]\n"},
		{"ext_hash",
			"module Mx; end; h = {}; h.extend(Mx); p Marshal.dump(h).bytes",
			"[4, 8, 101, 58, 7, 77, 120, 123, 0]\n"},
		{"ext_regexp",
			"module Mx; end; r = /ab/.dup; r.extend(Mx); p Marshal.dump(r).bytes",
			"[4, 8, 73, 101, 58, 7, 77, 120, 47, 7, 97, 98, 0, 6, 58, 6, 69, 70]\n"},
		{"ext_range",
			"module Mx; end; r = (1..2).dup; r.extend(Mx); p Marshal.dump(r).bytes",
			"[4, 8, 101, 58, 7, 77, 120, 111, 58, 10, 82, 97, 110, 103, 101, 8, 58, 9, 101, 120, 99, 108, 70, 58, 10, 98, 101, 103, 105, 110, 105, 6, 58, 8, 101, 110, 100, 105, 7]\n"},
		{"ext_umarshal",
			"module Mx; end; class UM1; def marshal_dump; 1; end; def marshal_load(v); end; end; o = UM1.new; o.extend(Mx); p Marshal.dump(o).bytes",
			"[4, 8, 85, 58, 8, 85, 77, 49, 105, 6]\n"},
		{"ext_udump",
			"module Mx; end; class UD1; def _dump(l); \"z\".dup.force_encoding(\"binary\"); end; def self._load(s); new; end; end; o = UD1.new; o.extend(Mx); p Marshal.dump(o).bytes",
			"[4, 8, 117, 58, 8, 85, 68, 49, 6, 122]\n"},
		{"sub_string",
			"class US1 < String; end; p Marshal.dump(US1.new(\"ab\")).bytes",
			"[4, 8, 73, 67, 58, 8, 85, 83, 49, 34, 7, 97, 98, 6, 58, 6, 69, 84]\n"},
		{"sub_str_iv",
			"class US2 < String; end; s = US2.new(\"ab\"); s.instance_variable_set(:@foo, 1); p Marshal.dump(s).bytes",
			"[4, 8, 73, 67, 58, 8, 85, 83, 50, 34, 7, 97, 98, 7, 58, 6, 69, 84, 58, 9, 64, 102, 111, 111, 105, 6]\n"},
		{"sub_str_ext",
			"module Mx; end; class US3 < String; end; s = US3.new(\"ab\"); s.extend(Mx); p Marshal.dump(s).bytes",
			"[4, 8, 73, 101, 58, 7, 77, 120, 67, 58, 8, 85, 83, 51, 34, 7, 97, 98, 6, 58, 6, 69, 84]\n"},
		{"sub_ary",
			"class UA1 < Array; end; p Marshal.dump(UA1.new).bytes",
			"[4, 8, 67, 58, 8, 85, 65, 49, 91, 0]\n"},
		{"struct_iv",
			"S1 = Struct.new(:a); s = S1.new(1); s.instance_variable_set(:@iv, 2); p Marshal.dump(s).bytes",
			"[4, 8, 73, 83, 58, 7, 83, 49, 6, 58, 6, 97, 105, 6, 6, 58, 8, 64, 105, 118, 105, 7]\n"},
		{"struct_plain",
			"S2 = Struct.new(:a); p Marshal.dump(S2.new(1)).bytes",
			"[4, 8, 83, 58, 7, 83, 50, 6, 58, 6, 97, 105, 6]\n"},
		{"data_plain",
			"D1 = Data.define(:a); p Marshal.dump(D1.new(a: 1)).bytes",
			"[4, 8, 83, 58, 7, 68, 49, 6, 58, 6, 97, 105, 6]\n"},
		{"udump_utf8",
			"class UD2; def _dump(l); \"z\"; end; def self._load(s); new; end; end; p Marshal.dump(UD2.new).bytes",
			"[4, 8, 73, 117, 58, 8, 85, 68, 50, 6, 122, 6, 58, 6, 69, 84]\n"},
		{"udump_bin",
			"class UD3; def _dump(l); \"z\".dup.force_encoding(\"binary\"); end; def self._load(s); new; end; end; o = UD3.new; o.instance_variable_set(:@a, 1); p Marshal.dump(o).bytes",
			"[4, 8, 117, 58, 8, 85, 68, 51, 6, 122]\n"},
		{"udump_w1251",
			"class UD4; def _dump(l); \"a\".dup.force_encoding(\"windows-1251\"); end; def self._load(s); new; end; end; p Marshal.dump(UD4.new).bytes",
			"[4, 8, 73, 117, 58, 8, 85, 68, 52, 6, 97, 6, 58, 13, 101, 110, 99, 111, 100, 105, 110, 103, 34, 17, 87, 105, 110, 100, 111, 119, 115, 45, 49, 50, 53, 49]\n"},
		{"str_binary",
			"p Marshal.dump(\"a\".dup.force_encoding(\"binary\")).bytes",
			"[4, 8, 34, 6, 97]\n"},
		{"str_usascii",
			"p Marshal.dump(\"a\".dup.force_encoding(\"US-ASCII\")).bytes",
			"[4, 8, 73, 34, 6, 97, 6, 58, 6, 69, 70]\n"},
		{"str_utf8",
			"p Marshal.dump(\"a\").bytes",
			"[4, 8, 73, 34, 6, 97, 6, 58, 6, 69, 84]\n"},
		{"str_w1251",
			"p Marshal.dump(\"a\".dup.force_encoding(\"windows-1251\")).bytes",
			"[4, 8, 73, 34, 6, 97, 6, 58, 13, 101, 110, 99, 111, 100, 105, 110, 103, 34, 17, 87, 105, 110, 100, 111, 119, 115, 45, 49, 50, 53, 49]\n"},
		{"time_utc_iv",
			"t = Time.utc(2000,1,1); t.instance_variable_set(:@foo, 1); p Marshal.dump(t).bytes",
			"[4, 8, 73, 117, 58, 9, 84, 105, 109, 101, 13, 32, 0, 25, 192, 0, 0, 0, 0, 7, 58, 9, 64, 102, 111, 111, 105, 6, 58, 9, 122, 111, 110, 101, 73, 34, 8, 85, 84, 67, 6, 58, 6, 69, 70]\n"},
		{"time_offset",
			"t = Time.new(2007,10,1,0,0,0,\"-08:00\"); p Marshal.dump(t).bytes",
			"[4, 8, 73, 117, 58, 9, 84, 105, 109, 101, 13, 40, 228, 26, 128, 0, 0, 0, 0, 7, 58, 11, 111, 102, 102, 115, 101, 116, 105, 254, 128, 143, 58, 9, 122, 111, 110, 101, 48]\n"},
		{"time_links",
			"t = Time.utc(2000,1,1); v = \"x\".dup.force_encoding(\"binary\"); t.instance_variable_set(:@foo, v); p Marshal.dump([t, t, v]).bytes",
			"[4, 8, 91, 8, 73, 117, 58, 9, 84, 105, 109, 101, 13, 32, 0, 25, 192, 0, 0, 0, 0, 7, 58, 9, 64, 102, 111, 111, 34, 6, 120, 58, 9, 122, 111, 110, 101, 73, 34, 8, 85, 84, 67, 6, 58, 6, 69, 70, 64, 8, 64, 6]\n"},
		{"time_rt_off",
			"t = Time.new(2007,10,1,0,0,0,\"-08:00\"); l = Marshal.load(Marshal.dump(t)); p [l.utc_offset, l.zone, l.to_i]",
			"[-28800, nil, 1191225600]\n"},
		{"time_rt_utc",
			"t = Time.utc(2000,1,1); t.instance_variable_set(:@foo, \"bar\"); l = Marshal.load(Marshal.dump(t)); p [l.utc?, l.zone, l.instance_variable_get(:@foo)]",
			"[true, \"UTC\", \"bar\"]\n"},
		{"time_ivars",
			"t = Time.at(0); t.instance_variable_set(:@a, 1); p [t.instance_variable_get(:@a), t.instance_variables]",
			"[1, [:@a]]\n"},
		{"time_dup",
			"t = Time.at(100); t.freeze; p [t.dup.equal?(t), t.dup.frozen?, t.clone.frozen?, t.clone(freeze: false).frozen?, t.dup.to_i]",
			"[false, false, true, false, 100]\n"},
		{"time_dup_iv",
			"t = Time.at(5); t.instance_variable_set(:@a, 1); p t.dup.instance_variable_get(:@a)",
			"1\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := eval(t, c.src); got != c.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
			}
		})
	}
}

// TestMarshalWave29Errors pins the refusals. Each message was checked against
// MRI 4.0.5, which raises the same class with the same text.
func TestMarshalWave29Errors(t *testing.T) {
	cases := []struct{ name, src, want string }{
		// SINGLETON_DUMP_UNABLE_P now reaches the built-in containers, not just
		// plain objects: a singleton carrying its own methods cannot be rebuilt
		// from the stream, so w_extended refuses it.
		{"singleton_on_string", `s = "x".dup; def s.foo; end; Marshal.dump(s)`, "singleton can't be dumped"},
		{"singleton_on_array", `a = []; def a.foo; end; Marshal.dump(a)`, "singleton can't be dumped"},
		{"singleton_on_hash", `h = {}; def h.foo; end; Marshal.dump(h)`, "singleton can't be dumped"},
		{"singleton_ivar_on_string", `s = "x".dup; class << s; @v = 1; end; Marshal.dump(s)`, "singleton can't be dumped"},
		// An extended module with no name cannot be named in the stream.
		{"anon_module_extended", `m = Module.new; s = "x".dup; s.extend(m); Marshal.dump(s)`, "anonymous class"},

		// An 'o' container may only rebuild a plain object. MRI allocates and
		// rejects a non-T_OBJECT; rbgo rejects the class by ancestry.
		{"o_names_file", `Marshal.load("\x04\bo:\tFile\x06:\n@pathI\"\x06.\x06:\x06ET")`, "dump format error"},
		{"o_names_array", `Marshal.load("\x04\bo:\nArray\x00")`, "dump format error"},

		// The 'd' container: a regular object class is an ArgumentError, a class
		// in the data protocol but missing #_load_data is a TypeError.
		{"d_regular_object",
			`class DReg; end; Marshal.load("\x04\bd:\tDReg\x00")`, "dump format error"},
		{"d_missing_load_data",
			`class DHalf; def _dump_data; "x"; end; end; Marshal.load("\x04\bd:\nDHalf\x00")`,
			"needs to have instance method '_load_data'"},

		// Time#clone's freeze: keyword accepts only true/false/nil.
		{"clone_unknown_keyword", `Time.at(0).clone(foo: 1)`, "unknown keyword: :foo"},
		{"clone_bad_freeze", `Time.at(0).clone(freeze: 1)`, "unexpected value for freeze: Integer"},
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

// TestMarshalWave29DataLoad exercises the 'd' container's success path: the
// instance is registered before the payload is read and #_load_data rebuilds it.
//
// This is a DOCUMENTED DIVERGENCE, not an MRI witness. MRI decides the branch on
// the allocated object's type, so it refuses this very stream with "dump format
// error": a pure-Ruby class is a T_OBJECT, and only a C-wrapped T_DATA (Dir and
// friends) can load through 'd'. rbgo has no user-reachable T_DATA, so it asks
// instead whether the class takes part in the _dump_data / _load_data protocol.
// The two agree on every stream MRI could have WRITTEN — a 'd' container MRI
// produced always names a T_DATA class — and on both refusals the ruby/spec
// suite checks; they part only on a hand-built stream naming a pure-Ruby class
// that defines both hooks, which MRI would never have emitted. Closing the gap
// needs a T_DATA notion in the object model, which is not in this file.
func TestMarshalWave29DataLoad(t *testing.T) {
	src := `class DOK
  def _dump_data; @v; end
  def _load_data(s); @v = s; end
  def v; @v; end
end
p Marshal.load("\x04\bd:\bDOKI\"\babc\x06:\x06ET").v`
	if got := eval(t, src); got != "\"abc\"\n" {
		t.Errorf("got=%q want=%q", got, "\"abc\"\n")
	}
}

// TestMarshalWave29Coverage pins paths the ruby/spec corpus does not reach but
// the coverage gate does. Each of these found a real defect: the 'C' container
// stored its encoding ivar as a literal variable named "E" (so a re-dump emitted
// the encoding twice), and a Timezone object's #name was re-tagged US-ASCII when
// MRI keeps the String that method returned. Expected values are MRI 4.0.5's.
func TestMarshalWave29Coverage(t *testing.T) {
	cases := []struct{ name, src, want string }{
		// A Timezone object's #name is stored as-is, so an ASCII-only name from a
		// UTF-8 literal dumps :E => true, NOT the :E => false a tz abbreviation gets.
		{"tzobj_zone_name", `class TZ9
  def name; "XYZ"; end
  def local_to_utc(t); t - 3600; end
  def utc_to_local(t); t + 3600; end
end
p Marshal.dump(Time.new(2000,1,1,0,0,0, TZ9.new)).bytes`,
			"[4, 8, 73, 117, 58, 9, 84, 105, 109, 101, 13, 247, 239, 24, 128, 0, 0, 0, 0, 7, 58, 11, 111, 102, 102, 115, 101, 116, 105, 2, 16, 14, 58, 9, 122, 111, 110, 101, 73, 34, 8, 88, 89, 90, 6, 58, 6, 69, 84]\n"},
		// A zone carried only as a location keeps the tz database's US-ASCII.
		{"time_zone_abbrev_usascii", `p Marshal.dump(Time.utc(2000,1,1)).bytes`,
			"[4, 8, 73, 117, 58, 9, 84, 105, 109, 101, 13, 32, 0, 25, 192, 0, 0, 0, 0, 6, 58, 9, 122, 111, 110, 101, 73, 34, 8, 85, 84, 67, 6, 58, 6, 69, 70]\n"},
		// Re-dumping a Regexp subclass loaded from a 'C' container must emit ONE
		// encoding ivar, from the payload — not a second one named "E".
		{"regexp_subclass_container", `class URx < Regexp; end
r = Marshal.load("\x04\bIC:\bURx/\x00\x00\x06:\x06EF")
p [r.class.to_s, Marshal.dump(r).bytes]`,
			"[\"URx\", [4, 8, 73, 67, 58, 8, 85, 82, 120, 47, 0, 0, 6, 58, 6, 69, 70]]\n"},
		// The same for a String subclass, whose own @foo does stay an ivar.
		{"string_subclass_container", `class USx < String; end
s = Marshal.load("\x04\bIC:\bUSx\"\aab\a:\x06ET:\t@fooi\x06")
p [s.class.to_s, s.encoding.to_s, s.instance_variable_get(:@foo), Marshal.dump(s).bytes]`,
			"[\"USx\", \"UTF-8\", 1, [4, 8, 73, 67, 58, 8, 85, 83, 120, 34, 7, 97, 98, 7, 58, 6, 69, 84, 58, 9, 64, 102, 111, 111, 105, 6]]\n"},
		// Time#dup / #clone carry the exact sub-nanosecond fraction.
		{"subnano_dup", `t = Time.at(Rational(1,3)); p [t.dup.subsec, t.clone.subsec]`,
			"[(1/3), (1/3)]\n"},
		// A Timezone object whose #name is not a String contributes no zone, so
		// :zone dumps nil (the trailing 48 == '0').
		{"tzobj_nil_name", `class TZN
  def name; nil; end
  def local_to_utc(t); t; end
  def utc_to_local(t); t; end
end
p Marshal.dump(Time.new(2000,1,1,0,0,0, TZN.new)).bytes`,
			"[4, 8, 73, 117, 58, 9, 84, 105, 109, 101, 13, 32, 0, 25, 128, 0, 0, 0, 0, 7, 58, 11, 111, 102, 102, 115, 101, 116, 105, 0, 58, 9, 122, 111, 110, 101, 48]\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := eval(t, c.src); got != c.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
			}
		})
	}
}
