// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestIsExplicitRelative covers MRI's is_explicit_relative (file.c v3_4_0): a
// path is "explicitly relative" -- and so resolved against the working directory
// instead of being searched on $LOAD_PATH -- only when it begins with "./" or
// "../". A name that merely starts with a dot is not.
func TestIsExplicitRelative(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"./x", true},
		{"../x", true},
		// isdirsep accepts a backslash only on Windows, so MRI's answer for these
		// two differs by platform and so must rbgo's.
		{`.\x`, runtime.GOOS == "windows"},
		{`..\x`, runtime.GOOS == "windows"},
		{".", false},
		{"..", false},
		{"...", false},
		{"..foo", false},
		{".hidden", false},
		{"x", false},
		{"", false},
		{"/abs", false},
	}
	for _, c := range cases {
		if got := isExplicitRelative(c.path); got != c.want {
			t.Errorf("isExplicitRelative(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

// TestBuiltinClassName covers rb_builtin_class_name (error.c v3_4_0), which
// names nil, true and false as those words in a TypeError and otherwise reports
// the object's class.
func TestBuiltinClassName(t *testing.T) {
	vm := New(io.Discard)
	cases := []struct {
		val  object.Value
		want string
	}{
		{object.NilV, "nil"},
		{object.Bool(true), "true"},
		{object.Bool(false), "false"},
		{object.IntValue(1), "Integer"},
		{object.NewString("s"), "String"},
		{object.Symbol("s"), "Symbol"},
	}
	for _, c := range cases {
		if got := vm.builtinClassName(c.val); got != c.want {
			t.Errorf("builtinClassName(%v) = %q, want %q", c.val, got, c.want)
		}
	}
}

// TestChopStrEncoding covers chopStr's encoding-dependent tail: "the last
// character" is one byte in ASCII-8BIT and one UTF-8 sequence otherwise, a
// trailing CRLF counts as one either way, and an empty string chops to itself.
// An invalid byte in a UTF-8 string is one character (utf8.DecodeLastRune gives
// size 1) and the bytes BEFORE it must come back untouched.
func TestChopStrEncoding(t *testing.T) {
	cases := []struct{ s, enc, want string }{
		{"abc\r\n", "UTF-8", "abc"},
		{"abc\r\n", "ASCII-8BIT", "abc"},
		{"", "UTF-8", ""},
		{"", "ASCII-8BIT", ""},
		{"hé", "UTF-8", "h"},          // a two-byte rune goes whole
		{"hé", "ASCII-8BIT", "h\xc3"}, // ... but is two characters in binary
		{"ab\xdf", "UTF-8", "ab"},     // an invalid lead byte is one character
		{"\xe3\x81\x82\xa4", "UTF-8", "\xe3\x81\x82"},
		{"\xa4b", "UTF-8", "\xa4"}, // the invalid PREFIX survives byte for byte
	}
	for _, c := range cases {
		if got := chopStr(c.s, c.enc); got != c.want {
			t.Errorf("chopStr(%q, %s) = %q, want %q", c.s, c.enc, got, c.want)
		}
	}
}

// TestStrCharPieces covers the encoding-aware character split behind
// String#chars / #each_char: one byte per character in ASCII-8BIT, one UTF-8
// sequence otherwise, an invalid byte standing as its own character, and every
// piece a slice of the original so no byte is rewritten.
func TestStrCharPieces(t *testing.T) {
	cases := []struct {
		s, enc string
		want   []string
	}{
		{"", "UTF-8", []string{}},
		{"", "ASCII-8BIT", []string{}},
		{"abc", "UTF-8", []string{"a", "b", "c"}},
		{"hé", "UTF-8", []string{"h", "\xc3\xa9"}},
		{"hé", "ASCII-8BIT", []string{"h", "\xc3", "\xa9"}},
		{"\xa4", "UTF-8", []string{"\xa4"}},
		{"\xe3\x81\x82\xa4x", "UTF-8", []string{"\xe3\x81\x82", "\xa4", "x"}},
		{"\xf0\xa4\xad\xa2", "ASCII-8BIT", []string{"\xf0", "\xa4", "\xad", "\xa2"}},
	}
	for _, c := range cases {
		got := strCharPieces(c.s, c.enc)
		if len(got) != len(c.want) {
			t.Errorf("strCharPieces(%q, %s) = %q, want %q", c.s, c.enc, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("strCharPieces(%q, %s)[%d] = %q, want %q", c.s, c.enc, i, got[i], c.want[i])
			}
		}
	}
}

// TestRequirePathCoercion covers requireName / requirePathStr, MRI's rb_get_path
// for a require argument: a String passes through, #to_path is tried first, its
// non-String result then goes through #to_str, and a value answering neither --
// or answering with a non-String -- is a TypeError. The NUL-byte and
// ASCII-incompatible guards of rb_get_path_check_convert fire too.
func TestRequirePathCoercion(t *testing.T) {
	cases := []struct{ src, wantClass, wantMsg string }{
		{`require Object.new`, "TypeError", "no implicit conversion of Object into String"},
		{`require 42`, "TypeError", "no implicit conversion of Integer into String"},
		{`require nil`, "TypeError", "no implicit conversion of nil into String"},
		{`o = Object.new; def o.to_path; 42; end; require o`, "TypeError",
			"no implicit conversion of Integer into String"},
		{`o = Object.new; def o.to_str; 42; end; require o`, "TypeError",
			"can't convert Object to String (Object#to_str gives Integer)"},
		{"require \"a\\0b\"", "ArgumentError", "path name contains null byte"},
		// A #to_path answering a String is used as the feature name: it reaches the
		// LoadError naming exactly that name.
		{`o = Object.new; def o.to_path; "no_such_feature_xyz"; end; require o`, "LoadError",
			"cannot load such file -- no_such_feature_xyz"},
		// A #to_path answering a non-String that answers #to_str: the chain runs to
		// the end and the STRING is the feature name.
		{`i = Object.new; def i.to_str; "no_such_feature_pq"; end
		  o = Object.new; def o.to_path; $i; end; $i = i; require o`, "LoadError",
			"cannot load such file -- no_such_feature_pq"},
	}
	for _, c := range cases {
		cls, msg := evalErr(t, c.src)
		if cls != c.wantClass || msg != c.wantMsg {
			t.Errorf("src=%q got %s: %q, want %s: %q", c.src, cls, msg, c.wantClass, c.wantMsg)
		}
	}
}

// TestLoadErrorPath covers LoadError#path, the reader MRI declares with
// rb_attr(rb_eLoadError, path, TRUE, FALSE, FALSE): it reports the feature name
// as PASSED for a failed require/require_relative/load, and nil for a LoadError
// built by hand.
func TestLoadErrorPath(t *testing.T) {
	cases := []struct{ src, want string }{
		{`begin; require "abcd1234"; rescue LoadError => e; p e.path; end`, `"abcd1234"`},
		{`begin; load "abcd1234"; rescue LoadError => e; p e.path; end`, `"abcd1234"`},
		{`p LoadError.new("x").path`, `nil`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got %q, want %q", c.src, got, c.want+"\n")
		}
	}
	// require_relative names the EXPANDED path, because rb_f_require_relative
	// expands before require_internal sees the name.
	got := eval(t, `begin; require_relative "nope_xyz"; rescue LoadError => e; p e.path.end_with?("nope_xyz"), e.path.start_with?("/"); end`)
	if got != "true\ntrue\n" {
		t.Errorf("require_relative LoadError#path got %q", got)
	}
}

// TestRequireResolutionRules covers requireCandidates against rb_find_file
// (file.c v3_4_0): an absolute require_relative argument ignores the base, and a
// "~", absolute or explicitly-relative name is never searched on $LOAD_PATH.
func TestRequireResolutionRules(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "feat.rb"), []byte("FEAT = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	q := strconv.Quote(filepath.Join(dir, "feat.rb"))
	// An absolute path given to require_relative is used verbatim.
	if got := eval(t, "p require_relative("+q+")\np FEAT\n"); got != "true\n1\n" {
		t.Errorf("absolute require_relative got %q", got)
	}
	// "./feat.rb" is NOT found through a $LOAD_PATH entry that holds it.
	if cls, _ := evalErr(t, "$LOAD_PATH.unshift "+strconv.Quote(dir)+"\nrequire \"./feat.rb\"\n"); cls != "LoadError" {
		t.Errorf("./ relative resolved against $LOAD_PATH: got %s, want LoadError", cls)
	}
	if cls, _ := evalErr(t, "$LOAD_PATH.unshift "+strconv.Quote(dir)+"\nrequire \"../feat.rb\"\n"); cls != "LoadError" {
		t.Errorf("../ relative resolved against $LOAD_PATH: got %s, want LoadError", cls)
	}
	// A "~" name is expanded through HOME and not searched on the load path.
	if cls, _ := evalErr(t, "$LOAD_PATH.unshift "+strconv.Quote(dir)+"\nrequire \"~/feat.rb\"\n"); cls != "LoadError" {
		t.Errorf("~ path resolved against $LOAD_PATH: got %s, want LoadError", cls)
	}
	// The bare name still resolves through $LOAD_PATH.
	if got := eval(t, "$LOAD_PATH.unshift "+strconv.Quote(dir)+"\nrequire \"feat\"\np FEAT\n"); !strings.Contains(got, "1") {
		t.Errorf("bare require via $LOAD_PATH got %q", got)
	}
}

// TestRequireAlreadyProvidedBareName covers the last branch of doRequire: a
// require whose name carries no extension and that finds nothing on disk still
// returns false when $LOADED_FEATURES already lists that exact name.
// search_required (load.c v3_4_0) asks rb_feature_p before rb_find_file_ext, and
// a no-extension entry answers 'u', which reaches `case 0: if (ft) goto
// feature_present;` -- so require_internal reports false instead of failing.
// Witnessed against MRI 4.0.5, including the cases that must still raise.
func TestRequireAlreadyProvidedBareName(t *testing.T) {
	cases := []struct{ src, want string }{
		{`$LOADED_FEATURES << "no_such_feature_abc"
		  p require("no_such_feature_abc")`, `false`},
		{`$LOADED_FEATURES << "dir/other_missing"
		  p require("dir/other_missing")`, `false`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got %q, want %q", c.src, got, c.want+"\n")
		}
	}
	// An extensioned name is NOT matched by a bare entry, and a name that is not
	// listed at all still raises -- both branches of the guard.
	bad := []string{
		`$LOADED_FEATURES << "no_such_feature_abc"` + "\n" + `require("no_such_feature_abc.rb")`,
		`require("totally_absent_zz")`,
		`$LOADED_FEATURES << "no_such_feature_abc"` + "\n" + `require_relative("no_such_feature_abc")`,
	}
	for _, src := range bad {
		if cls, _ := evalErr(t, src); cls != "LoadError" {
			t.Errorf("src=%q got %s, want LoadError", src, cls)
		}
	}
}

// TestRequireRelativeBase covers the expansion rb_f_require_relative applies
// before require_internal runs: an already-absolute name is left alone (only
// cleaned), a relative one is joined onto the requiring file's directory.
func TestRequireRelativeBase(t *testing.T) {
	vm := New(io.Discard)
	abs := filepath.Join(t.TempDir(), "a", "..", "b.rb")
	if got := vm.requireRelativeBase(abs); got != featurePath(abs) || strings.Contains(got, "..") {
		t.Errorf("requireRelativeBase(%q) = %q, want the cleaned absolute path", abs, got)
	}
	got := vm.requireRelativeBase("rel.rb")
	if !filepath.IsAbs(filepath.FromSlash(got)) || !strings.HasSuffix(got, "rel.rb") {
		t.Errorf("requireRelativeBase(rel.rb) = %q, want an absolute path ending in rel.rb", got)
	}
}

// TestCircularRequireWarning covers warnCircularRequire. MRI's load_lock issues
// the warning through rb_warning, so it appears only when $VERBOSE is true, and
// only for a re-entrant require ON THE SAME thread (rb_thread_shield_owned).
func TestCircularRequireWarning(t *testing.T) {
	dir := t.TempDir()
	self := filepath.Join(dir, "selfreq.rb")
	if err := os.WriteFile(self, []byte("require_relative 'selfreq'\nSEEN = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	q := strconv.Quote(self)
	// $VERBOSE true: the warning is written to $stderr and the file still loads once.
	src := "$VERBOSE = true\n$stderr = StringIO.new\nrequire " + q + "\np $stderr.string.include?('circular require considered harmful')\n"
	if got := eval(t, src); !strings.HasSuffix(got, "true\n") {
		t.Errorf("verbose circular require: got %q, want the warning", got)
	}
	// $VERBOSE false: rb_warning is silent.
	src = "$VERBOSE = false\n$stderr = StringIO.new\nrequire " + q + "\np $stderr.string\n"
	if got := eval(t, src); !strings.HasSuffix(got, "\"\"\n") {
		t.Errorf("non-verbose circular require: got %q, want no warning", got)
	}
	// $VERBOSE nil: likewise silent (the nil branch of the Bool assertion).
	src = "$VERBOSE = nil\n$stderr = StringIO.new\nrequire " + q + "\np $stderr.string\n"
	if got := eval(t, src); !strings.HasSuffix(got, "\"\"\n") {
		t.Errorf("VERBOSE=nil circular require: got %q, want no warning", got)
	}
}

// TestSplitCheckArgs covers the two guards rb_str_split_m runs first:
// mustnot_broken on the receiver and on a String pattern, and the NUM2INT range
// on the limit (which is also converted exactly once).
func TestSplitCheckArgs(t *testing.T) {
	cases := []struct{ src, wantClass, wantMsg string }{
		{`"\xDF".split("a")`, "ArgumentError", "invalid byte sequence in UTF-8"},
		{`"\xDF".split`, "ArgumentError", "invalid byte sequence in UTF-8"},
		{`"\xDF".split(/a/)`, "ArgumentError", "invalid byte sequence in UTF-8"},
		{`"a".split("\xDF")`, "ArgumentError", "invalid byte sequence in UTF-8"},
		{`"a,b".split(",", 2147483649)`, "RangeError", "integer 2147483649 too big to convert to 'int'"},
		{`"a,b".split(",", -2147483649)`, "RangeError", "integer -2147483649 too small to convert to 'int'"},
		{`"a".split(",", Object.new)`, "TypeError", "no implicit conversion of Object into Integer"},
	}
	for _, c := range cases {
		cls, msg := evalErr(t, c.src)
		if cls != c.wantClass || msg != c.wantMsg {
			t.Errorf("src=%q got %s: %q, want %s: %q", c.src, cls, msg, c.wantClass, c.wantMsg)
		}
	}
	// A nil limit takes the IsNil branch of the range guard and is left to the
	// splitter, which refuses it as MRI does (MRI: "no implicit conversion from
	// nil to integer"; rbgo words it "of NilClass into Integer" -- a pre-existing
	// difference in toIntCoerce, not introduced here, so only the class is
	// pinned).
	if cls, _ := evalErr(t, `"a,b".split(",", nil)`); cls != "TypeError" {
		t.Errorf("nil split limit: got %s, want TypeError", cls)
	}
	// A binary receiver is never broken, an in-range limit passes, and #to_int is
	// called exactly once.
	ok := []struct{ src, want string }{
		{`p "\xDF a".b.split("a")`, `["\xDF "]`},
		{`p "a,b,c".split(",", 2)`, `["a", "b,c"]`},
		{`p "a,b".split(",", 2147483647)`, `["a", "b"]`},
		{`p "a,b".split(",", -2147483648)`, `["a", "b"]`},
		{`o = Object.new; def o.to_int; $n = ($n || 0) + 1; 2; end
		  r = "a,b,c".split(",", o); p r, $n`, "[\"a\", \"b,c\"]\n1"},
	}
	for _, c := range ok {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got %q, want %q", c.src, got, c.want+"\n")
		}
	}
}

// TestByteIndexCoercion covers strValueArg and the NUM2LONG offset of
// String#byteindex / #byterindex: #to_str and #to_int are honoured, and the
// refusals name the value the way rb_builtin_class_name does.
func TestByteIndexCoercion(t *testing.T) {
	ok := []struct{ src, want string }{
		{`o = Object.new; def o.to_str; "bla"; end; p "blablabla".byteindex(o)`, `0`},
		{`o = Object.new; def o.to_str; "bla"; end; p "blablabla".byterindex(o)`, `6`},
		{`o = Object.new; def o.to_int; 3; end; p "blablabla".byteindex("bla", o)`, `3`},
		{`o = Object.new; def o.to_int; 3; end; p "blablabla".byterindex("bla", o)`, `3`},
		// The reverse search sees the whole subject, so \A pins to offset 0.
		{`p "blablabla".byterindex(/\A/)`, `0`},
		{`p "blablabla".byterindex(/\z/)`, `9`},
		{`p "blablabla".byterindex(/bla|a/)`, `8`},
		{`p "blablabla".byterindex(/zzz/)`, `nil`},
		{`p "blablabla".byterindex(/bla/)`, `6`},
	}
	for _, c := range ok {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got %q, want %q", c.src, got, c.want+"\n")
		}
	}
	// A winning reverse match records $~.
	if got := eval(t, `p "blablabla".byterindex(/b(la)/); p $~[1]`); got != "6\n\"la\"\n" {
		t.Errorf("byterindex $~ got %q", got)
	}
	// A failing one clears it.
	if got := eval(t, `"blablabla" =~ /bla/; "blablabla".byterindex(/zzz/); p $~`); got != "nil\n" {
		t.Errorf("byterindex clears $~ got %q", got)
	}
	bad := []struct{ src, wantMsg string }{
		{`"a".byteindex(true)`, "no implicit conversion of true into String"},
		{`"a".byteindex(false)`, "no implicit conversion of false into String"},
		{`"a".byterindex(nil)`, "no implicit conversion of nil into String"},
		{`"a".byteindex(Object.new)`, "no implicit conversion of Object into String"},
		{`o = Object.new; def o.to_str; 1; end; "a".byteindex(o)`,
			"can't convert Object to String (Object#to_str gives Integer)"},
		{`"a".byteindex("a", Object.new)`, "no implicit conversion of Object into Integer"},
	}
	for _, c := range bad {
		cls, msg := evalErr(t, c.src)
		if cls != "TypeError" || msg != c.wantMsg {
			t.Errorf("src=%q got %s: %q, want TypeError: %q", c.src, cls, msg, c.wantMsg)
		}
	}
}

// TestSymbolEncoding covers Symbol#encoding: rb_str_intern re-tags an ASCII-only
// string as US-ASCII whatever it arrived in, and rbgo derives the rest from the
// bytes (valid UTF-8, else ASCII-8BIT).
func TestSymbolEncoding(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p "foobar".to_sym.encoding`, `#<Encoding:US-ASCII>`},
		{`p "foobar".b.to_sym.encoding`, `#<Encoding:US-ASCII>`},
		{`p :"".encoding`, `#<Encoding:US-ASCII>`},
		{`p "il était une fois".to_sym.encoding`, `#<Encoding:UTF-8>`},
		{`p "\xDF".b.to_sym.encoding`, `#<Encoding:BINARY (ASCII-8BIT)>`},
		{`p "h\xDFi".b.to_sym.encoding`, `#<Encoding:BINARY (ASCII-8BIT)>`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got %q, want %q", c.src, got, c.want+"\n")
		}
	}
}
