// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"bytes"
	"strings"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// runGvar runs a Ruby program on a fresh VM and returns its trimmed stdout. It
// is runSrc under another name so the intent of these cases — every one of them
// is a `$special = …` whose answer was witnessed on MRI 4.0.5 — stays readable.
func runGvar(t *testing.T, src string) string {
	t.Helper()
	return runSrc(t, src)
}

// gvarProbe wraps a global assignment so the class and message of whatever it
// raises print in one line, which is the form the MRI witness was taken in.
const gvarProbe = "def t(l); print l, \": \"; begin; p(yield); rescue Exception => e; puts \"#{e.class}: #{e.message}\"; end; end\n"

// TestGvarReadOnly covers storeGVar's read-only arm. Every one of these raises
// NameError on MRI 4.0.5 (witnessed one by one); the message names the spelling
// written through, which is variable.c v3_4_0 rb_gvar_readonly_setter's
// rb_name_error(id, "%s is a read-only variable", QUOTE_ID(id)).
func TestGvarReadOnly(t *testing.T) {
	for _, name := range []string{"$!", "$&", "$`", "$'", "$+", "$:", "$LOAD_PATH",
		"$-I", `$"`, "$LOADED_FEATURES", "$<", "$FILENAME", "$*", "$?", "$-a",
		"$-l", "$-p", "$-W"} {
		src := "begin; " + name + " = nil; rescue NameError => e; puts e.message; end"
		want := name + " is a read-only variable"
		if got := runGvar(t, src); got != want {
			t.Errorf("%s: got %q want %q", name, got, want)
		}
	}
	// Read-only-ness follows an English alias, and the message names the alias.
	if got := runGvar(t, `begin; $ERROR_INFO = 1; rescue NameError => e; puts e.message; end`); got != "$ERROR_INFO is a read-only variable" {
		t.Errorf("english alias: got %q", got)
	}
	// NameError#name carries the offending global.
	if got := runGvar(t, `begin; $: = []; rescue NameError => e; p e.name; end`); got != `:$:` {
		t.Errorf("name ivar: got %q", got)
	}
}

// TestGvarTypedSetters covers the setters that check their value: the
// I/O-formatting specials (io.c v3_4_0 deprecated_str_setter -> string.c
// rb_str_setter / rb_fs_setter), $. (io.c argf_lineno_setter's NUM2INT), the
// standard streams (io.c must_respond_to) and $~ (re.c match_setter). Each
// expectation is the MRI 4.0.5 answer.
func TestGvarTypedSetters(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"rs_int", `$/ = 1`, "TypeError: value of $/ must be String"},
		{"rs_dash0", `$-0 = 1`, "TypeError: value of $-0 must be String"},
		{"ors", `$\ = 1`, `TypeError: value of $\ must be String`},
		{"ofs", `$, = 1`, "TypeError: value of $, must be String"},
		{"fs", `$; = 1`, "TypeError: value of $; must be String or Regexp"},
		{"lineno_str", `$. = "x"`, "TypeError: no implicit conversion of String into Integer"},
		{"stdout_nil", `$stdout = nil`, "TypeError: $stdout must have write method, NilClass given"},
		{"stderr_int", `$stderr = 1`, "TypeError: $stderr must have write method, Integer given"},
		{"gt_int", `$> = 1`, "TypeError: $stdout must have write method, Integer given"},
		{"progname", `$0 = 1`, "TypeError: no implicit conversion of Integer into String"},
		{"backref", `$~ = 1`, "TypeError: wrong argument type Integer (expected MatchData)"},
		{"errat_unset", `$@ = []`, "ArgumentError: $! not set"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := gvarProbe + "t(\"x\") { " + tc.src + " }"
			if got := runGvar(t, src); got != "x: "+tc.want {
				t.Fatalf("got %q want %q", got, "x: "+tc.want)
			}
		})
	}
	// The accepting arms: $/ and $-0 share one slot, $; takes a String, a Regexp
	// or anything with #to_str, $. truncates a Float through NUM2INT, and a
	// $stdout that answers #write is accepted.
	ok := []struct{ name, src, want string }{
		{"rs_shared", `$/ = "xyz"; p $-0`, `"xyz"`},
		{"rs_dash0_shared", `$-0 = "q"; p $/`, `"q"`},
		{"rs_nil", `$/ = nil; p $/`, "nil"},
		{"fs_string", `$; = ","; p $;`, `","`},
		{"fs_regexp", `$; = /x/; p $;`, "/x/"},
		{"fs_to_str", `class S; def to_str; "-"; end; end; $; = S.new; p $;`, `"-"`},
		{"fs_nil", `$; = nil; p $;`, "nil"},
		{"fs_dashF", `$-F = ":"; p $;`, `":"`},
		{"lineno_float", `$. = 123.5; p $.`, "123"},
		{"lineno_to_int", `class I; def to_int; 7; end; end; $. = I.new; p $.`, "7"},
		{"stdout_ok", `class W; def write(*a); end; end; w = W.new; $stdout = w; o = $stdout; $stdout = STDOUT; p o.equal?(w)`, "true"},
		{"gt_alias", `class W; def write(*a); end; end; w = W.new; $> = w; o = $stdout; $stdout = STDOUT; p o.equal?(w)`, "true"},
		{"progname_str", `$0 = "z"; p $PROGRAM_NAME`, `"z"`},
		{"progname_to_str", `class S; def to_str; "n"; end; end; $PROGRAM_NAME = S.new; p $0`, `"n"`},
	}
	for _, tc := range ok {
		t.Run(tc.name, func(t *testing.T) {
			if got := runGvar(t, tc.src); got != tc.want {
				t.Fatalf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

// TestGvarBackrefAssignment covers match_setter: assigning $~ moves the backref,
// so the derived $&, $`, $', $+ and $1.. follow it, and assigning nil clears it.
func TestGvarBackrefAssignment(t *testing.T) {
	const src = `"foo hello" =~ /(f)oo/; m = $~
"bar world" =~ /(b)ar/
$~ = m
p [$&, $1, $', $+]
$~ = nil
p [$~, $&]`
	want := "[\"foo\", \"f\", \" hello\", \"f\"]\n[nil, nil]"
	if got := runGvar(t, src); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

// TestGvarErrat covers the $@ getter and setter (eval.c v3_4_0 errat_getter /
// errat_setter): outside a rescue it reads nil and cannot be assigned, and
// inside one it reads the rescued exception's backtrace and writes through
// Exception#set_backtrace.
func TestGvarErrat(t *testing.T) {
	const src = `p $@
begin
  raise "x"
rescue
  p $@ == $!.backtrace
  $@ = ["here"]
  p $!.backtrace
  p $@
end`
	want := "nil\ntrue\n[\"here\"]\n[\"here\"]"
	if got := runGvar(t, src); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

// TestGvarVerboseAndDebug covers verboseSlot and verbose_setter: $VERBOSE and
// $DEBUG read false until assigned (ruby.c v3_4_0 starts ruby_verbose and
// ruby_debug at Qfalse), $-v/$-w/$-d are the same slots under other names, a
// truthy assignment becomes exactly true, and $-W derives 0/1/2 from $VERBOSE.
func TestGvarVerboseAndDebug(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"defaults", `p [$VERBOSE, $-v, $-w, $DEBUG, $-d, $-W]`, "[false, false, false, false, false, 1]"},
		{"truthy", `$VERBOSE = 1; p [$VERBOSE, $-v, $-W]`, "[true, true, 2]"},
		{"nil", `$VERBOSE = nil; p [$VERBOSE, $-W]`, "[nil, 0]"},
		{"false", `$VERBOSE = false; p $VERBOSE`, "false"},
		{"via_dash_v", `$-v = true; p $VERBOSE`, "true"},
		{"via_dash_w", `$-w = "s"; p $VERBOSE`, "true"},
		{"debug_alias", `$DEBUG = true; p $-d`, "true"},
		{"debug_via_dash_d", `$-d = true; p $DEBUG`, "true"},
		{"switch_flags", `p [$-a, $-l, $-p]`, "[false, false, false]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := runGvar(t, tc.src); got != tc.want {
				t.Fatalf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

// TestGvarIgnorecaseAndAliases covers the $= residue (re.c v3_4_0
// ignorecase_getter: reads false and warns under the deprecated category) and
// the shared-slot spellings resolving to one value on both sides.
func TestGvarIgnorecaseAndAliases(t *testing.T) {
	if got := runGvar(t, `p $=`); got != "false" {
		t.Fatalf("$=: got %q", got)
	}
	// $= warns only while Warning[:deprecated] is on, which is off by default —
	// error.c deprecation_warning_enabled(). Both states are exercised.
	const warned = `require "stringio"
Warning[:deprecated] = true
$VERBOSE = false
o, $stderr = $stderr, StringIO.new
$=
$; = ","
s = $stderr.string
$stderr = o
print s`
	got := runGvar(t, warned)
	if !strings.Contains(got, "variable $= is no longer effective") ||
		!strings.Contains(got, "non-nil '$;' is deprecated") {
		t.Fatalf("deprecated warnings: got %q", got)
	}
	// With $VERBOSE nil nothing is emitted even with the category on.
	const silent = `require "stringio"
Warning[:deprecated] = true
$VERBOSE = nil
o, $stderr = $stderr, StringIO.new
$=
$/ = "x"
s = $stderr.string
$stderr = o
p s`
	if got := runGvar(t, silent); got != `""` {
		t.Fatalf("silent under $VERBOSE=nil: got %q", got)
	}
	// $: / $-I / $LOAD_PATH are one Array; $" and $LOADED_FEATURES likewise.
	if got := runGvar(t, `p [$:.equal?($LOAD_PATH), $-I.equal?($LOAD_PATH), $".equal?($LOADED_FEATURES)]`); got != "[true, true, true]" {
		t.Fatalf("shared slots: got %q", got)
	}
}

// TestGvarTraceVar covers trace_var/untrace_var and the hook firing in setGVar.
// Every expectation was witnessed on MRI 4.0.5: hooks fire most-recently-added
// first, a String command is evaluated rather than called, untrace_var returns
// the removed commands, and a name that is neither traced nor a defined global
// is a NameError.
func TestGvarTraceVar(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"block", `$tv = nil; c = nil; trace_var(:$tv) { |v| c = v }; $tv = "foo"; p c`, `"foo"`},
		{"proc", `$tv = nil; c = nil; trace_var :$tv, proc { |v| c = v }; $tv = 1; p c`, "1"},
		{"string_cmd", `$tv = nil; trace_var :$tv, '$extra = true'; $tv = 1; p $extra`, "true"},
		{"order", `$tv = nil; r = []; trace_var(:$tv) { r << 1 }; trace_var(:$tv) { r << 2 }; $tv = 1; p r`, "[2, 1]"},
		{"returns_nil", `$tv = nil; p(trace_var(:$tv) { })`, "nil"},
		{"untrace_all", `$tv = nil; trace_var(:$tv) { }; p untrace_var(:$tv).size`, "1"},
		{"untrace_one", `$tv = nil; c = proc { }; trace_var(:$tv, c); p untrace_var(:$tv, c) == [c]`, "true"},
		{"untrace_other", `$tv = nil; trace_var(:$tv) { }; p untrace_var(:$tv, proc { })`, "nil"},
		{"untrace_untraced", `$tv = nil; p untrace_var(:$tv)`, "[]"},
		{"untrace_untraced_cmd", `$tv = nil; p untrace_var(:$tv, proc { })`, "nil"},
		{"untrace_twice", `$tv = nil; trace_var(:$tv) { }; untrace_var(:$tv); p untrace_var(:$tv)`, "[]"},
		{"no_hook_after_untrace", `$tv = nil; r = []; trace_var(:$tv) { r << 1 }; untrace_var :$tv; $tv = 2; p r`, "[]"},
		{"string_name", `$tv = nil; c = nil; trace_var("$tv") { |v| c = v }; $tv = 3; untrace_var "$tv"; p c`, "3"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := runGvar(t, tc.src); got != tc.want {
				t.Fatalf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
	errs := []struct{ name, src, want string }{
		{"no_block", `trace_var :$zz`, "ArgumentError: tried to create Proc object without a block"},
		{"nil_cmd", `$zz = nil; trace_var(:$zz, nil)`, "[]"},
		{"argc_trace", `trace_var`, "ArgumentError: wrong number of arguments (given 0, expected 1..2)"},
		{"argc_untrace", `untrace_var`, "ArgumentError: wrong number of arguments (given 0, expected 1..2)"},
		{"unknown", `untrace_var :$never_ever_seen`, "NameError: undefined global variable $never_ever_seen"},
	}
	for _, tc := range errs {
		t.Run(tc.name, func(t *testing.T) {
			src := gvarProbe + "t(\"x\") { " + tc.src + " }"
			if got := runGvar(t, src); got != "x: "+tc.want {
				t.Fatalf("got %q want %q", got, "x: "+tc.want)
			}
		})
	}
	// A hook on a global whose setter raises never runs, and one on $~ fires
	// through the backref arm rather than the plain-slot store.
	if got := runGvar(t, `r = []; trace_var(:$~) { |v| r << v.class }; "a" =~ /a/; $~ = $~; p r`); got != "[MatchData]" {
		t.Fatalf("backref trace: got %q", got)
	}
	if got := runGvar(t, `$tv = nil; r = []; trace_var(:$tv) { r << 1 }; begin; raise "x"; rescue; end; p r`); got != "[]" {
		t.Fatalf("unrelated assignment: got %q", got)
	}
	// Visibility: a private instance method of Kernel and a public method on it.
	if got := runGvar(t, `p [Kernel.private_instance_methods(false).include?(:trace_var), Kernel.public_methods(false).include?(:untrace_var)]`); got != "[true, true]" {
		t.Fatalf("visibility: got %q", got)
	}
}

// TestGvarTraceTableLazy covers gvarTraces' create/lookup arms directly: the
// table does not exist until the first trace_var, so an ordinary program pays
// nothing for the feature.
func TestGvarTraceTableLazy(t *testing.T) {
	vm := New(&bytes.Buffer{})
	if h := vm.gvarTraces(false); h != nil {
		t.Fatalf("table exists before any trace_var: %#v", h)
	}
	h := vm.gvarTraces(true)
	if h == nil {
		t.Fatal("gvarTraces(true) returned nil")
	}
	if again := vm.gvarTraces(false); again != h {
		t.Fatalf("second lookup returned a different table")
	}
	// A table holding something other than an Array under a name is ignored
	// rather than crashing the assignment path (defensive: only trace_var writes
	// this table, and it only ever writes Arrays).
	h.Set(object.Symbol("$junk"), object.IntValue(1))
	vm.fireGvarTraces("$junk", object.NilV)
	vm.fireGvarTraces("$untraced", object.NilV)
}

// TestLoadPathDirsNonArray covers loadPathDirs' non-Array arm. $LOAD_PATH is
// read-only from Ruby (load.c v3_4_0 gives it rb_gvar_readonly_setter), so the
// arm is only reachable from a host that rebinds the slot itself.
func TestLoadPathDirsNonArray(t *testing.T) {
	vm := New(&bytes.Buffer{})
	vm.globals["$LOAD_PATH"] = object.IntValue(42)
	if dirs := vm.loadPathDirs(); dirs != nil {
		t.Fatalf("non-Array $LOAD_PATH gave %#v, want nil", dirs)
	}
	if lp := vm.expandedLoadPath(); len(lp) != 0 {
		t.Fatalf("expandedLoadPath gave %#v, want empty", lp)
	}
	vm.globals["$LOADED_FEATURES"] = object.IntValue(7)
	if vm.featureProvided("anything") {
		t.Fatal("featureProvided true with no $LOADED_FEATURES Array")
	}
}

// TestFeatureProvided covers the rb_feature_p port (load.c v3_4_0): an entry
// spelled like the feature, an entry under a $LOAD_PATH prefix, the extension
// rules, and the shapes that must NOT match.
func TestFeatureProvided(t *testing.T) {
	cases := []struct {
		name     string
		features []string
		loadPath []string
		feature  string
		want     bool
	}{
		{"verbatim_no_ext", []string{"foo"}, nil, "foo", true},
		{"verbatim_with_ext", []string{"./foo.rb"}, nil, "./foo.rb", true},
		{"stem_plus_rb", []string{"foo.rb"}, nil, "foo", true},
		{"stem_plus_so", []string{"foo.so"}, nil, "foo", true},
		{"exact_name_but_ext_asked", []string{"foo"}, nil, "foo.rb", false},
		{"rb_asked_so_stored", []string{"foo.so"}, nil, "foo.rb", false},
		{"so_asked_rb_stored", []string{"foo.rb"}, nil, "foo.so", false},
		{"unknown_ext_is_part_of_name", []string{"foo.conf"}, nil, "foo.conf", true},
		{"load_path_prefix", []string{"/lib/foo.rb"}, []string{"/lib"}, "foo", true},
		{"load_path_prefix_dotted", []string{"/lib/a.b.rb"}, []string{"/lib"}, "a.b.rb", true},
		{"prefix_not_on_load_path", []string{"/other/foo.rb"}, []string{"/lib"}, "foo", false},
		{"no_dot_in_last_segment", []string{"/lib/foo"}, []string{"/lib"}, "foo", false},
		{"partial_segment", []string{"/lib/barfoo.rb"}, []string{"/lib"}, "foo", false},
		{"shorter_than_feature", []string{"fo"}, nil, "foo", false},
		{"same_length_other_name", []string{"bar"}, nil, "foo", false},
		{"stem_is_prefix_of_entry", []string{"foobar"}, nil, "foo", false},
		{"dotted_stem_no_ext", []string{"/lib/a.b"}, []string{"/lib"}, "a.b", true},
		{"prefixed_rb_asked_so_stored", []string{"/lib/foo.so"}, []string{"/lib"}, "foo.rb", false},
		{"prefixed_so_asked_rb_stored", []string{"/lib/foo.rb"}, []string{"/lib"}, "foo.so", false},
		{"nothing_loaded", nil, nil, "foo", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vm := New(&bytes.Buffer{})
			feats := object.NewArray()
			for _, f := range tc.features {
				feats.Elems = append(feats.Elems, object.NewString(f))
			}
			// A non-String entry is skipped rather than coerced, as rb_feature_p's
			// StringValuePtr walk over a features array only ever sees Strings.
			feats.Elems = append(feats.Elems, object.IntValue(1))
			vm.globals["$LOADED_FEATURES"] = feats
			lp := object.NewArray()
			for _, d := range tc.loadPath {
				lp.Elems = append(lp.Elems, object.NewString(d))
			}
			vm.globals["$LOAD_PATH"] = lp
			if got := vm.featureProvided(tc.feature); got != tc.want {
				t.Fatalf("featureProvided(%q) with %v = %v, want %v", tc.feature, tc.features, got, tc.want)
			}
		})
	}
}

// TestLocationArrayToBacktrace covers the rb_location_ary_to_backtrace port: a
// non-Array, an empty Array and an Array holding anything but a
// Thread::Backtrace::Location all report "not a location array" so
// Exception#set_backtrace falls back to rb_check_backtrace's String rules.
func TestLocationArrayToBacktrace(t *testing.T) {
	vm := New(&bytes.Buffer{})
	if v := vm.locationArrayToBacktrace(object.NewString("x")); v != nil {
		t.Fatalf("String: got %#v", v)
	}
	if v := vm.locationArrayToBacktrace(object.NewArray()); v != nil {
		t.Fatalf("empty Array: got %#v", v)
	}
	if v := vm.locationArrayToBacktrace(object.NewArray(object.NewString("x"))); v != nil {
		t.Fatalf("Array of String: got %#v", v)
	}
	loc := vm.backtraceLocation("a.rb:3:in 'f'")
	got, ok := vm.locationArrayToBacktrace(object.NewArray(loc)).(*object.Array)
	if !ok || len(got.Elems) != 1 || got.Elems[0].ToS() != "a.rb:3:in 'f'" {
		t.Fatalf("Array of Location: got %#v", got)
	}
	// End to end: copying one exception's backtrace_locations into another sets
	// both #backtrace and #backtrace_locations (error.c exc_set_backtrace).
	const src = `def f; raise "x"; end
begin; f; rescue => a; end
b = RuntimeError.new
b.set_backtrace(a.backtrace_locations)
p [b.backtrace == a.backtrace, b.backtrace_locations.map(&:to_s) == a.backtrace.to_a]`
	if got := runGvar(t, src); got != "[true, true]" {
		t.Fatalf("set_backtrace from locations: got %q", got)
	}
}

// TestSpecialGvarEnglishAndPid covers the remaining specialGvar arms: $$ is the
// process id, the English aliases resolve to their cryptic targets, and a name
// the resolver does not own falls through to the ordinary user-global path.
func TestSpecialGvarEnglishAndPid(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"pid", `p [$$ == Process.pid, $PID == $$, $PROCESS_ID == $$]`, "[true, true, true]"},
		{"errinfo", `begin; raise "x"; rescue; p $ERROR_INFO.message; end`, `"x"`},
		{"errposition", `begin; raise "x"; rescue; p $ERROR_POSITION == $!.backtrace; end`, "true"},
		{"match_aliases", `"xab" =~ /a(b)/; p [$MATCH, $PREMATCH, $POSTMATCH, $LAST_MATCH_INFO[1]]`, `["ab", "x", "", "b"]`},
		{"progname_default", `p $0.class`, "String"},
		{"unset_user_global", `p $no_such_global_at_all`, "nil"},
		{"user_global", `$mine = 5; p $mine`, "5"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := runGvar(t, tc.src); got != tc.want {
				t.Fatalf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

// TestRubyReleaseConstants covers the RUBY_RELEASE_DATE / RUBY_REVISION
// constants: both are frozen Strings, as core/builtin_constants asserts.
func TestRubyReleaseConstants(t *testing.T) {
	if got := runGvar(t, `p [RUBY_RELEASE_DATE.class, RUBY_RELEASE_DATE.frozen?, RUBY_REVISION.class, RUBY_REVISION.frozen?]`); got != "[String, true, String, true]" {
		t.Fatalf("got %q", got)
	}
}

// TestKernelComplexConvert covers makeComplex, the nucomp_convert port, step by
// step. Every expectation was witnessed on ruby 4.0.5 in one probe run.
func TestKernelComplexConvert(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"complex_complex", `p Complex(Complex(3, 4), Complex(5, 6))`, "(-3+9i)"},
		{"complex_complex_float", `p Complex(Complex(1.5, 2), Complex(-5, 6.3))`, "(-4.8-3.0i)"},
		{"lone_complex", `p Complex(Complex(1, 2))`, "(1+2i)"},
		{"complex_exact_zero_imag", `p Complex(Complex(1, 0), 2)`, "(1+2i)"},
		{"complex_zero_second", `p Complex(Complex(1, 2), 0)`, "(1+2i)"},
		{"string_string", `p Complex("1", "2")`, "(1+2i)"},
		{"to_c", `o = Object.new; def o.to_c; Complex(0, 1); end; p Complex(o)`, "(0+1i)"},
		{"non_real_numeric", "class NR < Numeric; def real?; false; end; end; p Complex(NR.new).class", "NR"},
		{"rational_parts", `p Complex(Rational(1, 2), 1)`, "((1/2)+1i)"},
		// f_real_p calls Complex(1, 0.0) real (f_zero_p), so the f_add path is NOT
		// taken and nucomp_real_check unwraps it to the Integer 1 — the imaginary
		// part stays an Integer. Complex(1, 0.0) + Complex(0, 2) is (1+2.0i) by
		// contrast, and both were witnessed on 4.0.5.
		{"float_zero_imag", `p Complex(Complex(1, 0.0), 2)`, "(1+2i)"},
		{"float_zero_imag_add", `p(Complex(1, 0.0) + Complex(0, 2))`, "(1+2.0i)"},
		{"nonzero_imag_adds", `p Complex(Complex(1, 0.5), 2)`, "(1+2.5i)"},
		{"complex_imag_arg", `p Complex(1, Complex(2, 3))`, "(-2+2i)"},
		{"exc_false_string", `p Complex("123", exception: false)`, "(123+0i)"},
		{"exc_false_nonnumeric", `p Complex(Object.new, exception: false)`, "nil"},
		{"exc_false_nil", `p Complex(nil, exception: false)`, "nil"},
		{"exc_false_second_nonnumeric", `p Complex(0, :sym, exception: false)`, "nil"},
		{"exc_false_second_string", `p Complex(0, "b", exception: false)`, "nil"},
		{"exc_false_nullbyte", `p Complex("1-2i\0", exception: false)`, "nil"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := runGvar(t, tc.src); got != tc.want {
				t.Fatalf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
	errs := []struct{ name, src, want string }{
		{"nil_first", `Complex(nil)`, "TypeError: can't convert nil into Complex"},
		{"nil_second", `Complex(0, nil)`, "TypeError: can't convert nil into Complex"},
		{"nil_first_of_two", `Complex(nil, 0)`, "TypeError: can't convert nil into Complex"},
		{"nullbyte", "Complex(\"1-2i\\0\")", "ArgumentError: string contains null byte"},
		{"bad_string", `Complex("zz")`, `ArgumentError: invalid value for convert(): "zz"`},
		{"no_to_c", `Complex(Object.new)`, "TypeError: can't convert Object into Complex"},
		{"to_c_wrong_type", `o = Object.new; def o.to_c; 1; end; Complex(o)`, "TypeError: can't convert Object to Complex (Object#to_c gives Integer)"},
		{"not_a_real_exc_false", `Complex(:sym, 0, exception: false)`, "TypeError: not a real"},
		{"not_a_real", `Complex(:sym, 0)`, "TypeError: not a real"},
	}
	for _, tc := range errs {
		t.Run(tc.name, func(t *testing.T) {
			src := gvarProbe + "t(\"x\") { " + tc.src + " }"
			if got := runGvar(t, src); got != "x: "+tc.want {
				t.Fatalf("got %q want %q", got, "x: "+tc.want)
			}
		})
	}
}
