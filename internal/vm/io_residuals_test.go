package vm_test

import (
	"strings"
	"testing"
)

// TestIOResiduals covers the StringIO/IO reader and writer conformance details
// brought in line with MRI Ruby 4.0.6: mode-aware StringIO/File construction,
// the gets separator/limit coercion and $/ default, paragraph mode, read's
// binary encoding and #to_int/#to_str coercions, print's $_ / $, / $\ handling,
// puts's #to_ary expansion, and the #each alias. Every want was checked against
// MRI 4.0.6.
func TestIOResiduals(t *testing.T) {
	const req = `require "stringio"; `
	cases := []struct{ src, want string }{
		// --- StringIO.new modes -------------------------------------------------
		// Append mode writes at end, ignoring the cursor, and updates pos to end.
		{req + `s = StringIO.new(+"example", "a"); s.print(", x"); p [s.string, s.pos]`,
			"[\"example, x\", 10]\n"},
		{req + `s = StringIO.new(+"example", "a"); s.pos = 3; s << ", y"; p s.string`,
			"\"example, y\"\n"},
		// "w"/"w+" truncate the backing string; "r+" preserves it.
		{req + `p [StringIO.new(+"data", "w").string, StringIO.new(+"data", "w+").string, StringIO.new(+"data", "r+").string]`,
			"[\"\", \"\", \"data\"]\n"},
		// r+ is readable and writable; a mid-buffer write overwrites in place.
		{req + `s = StringIO.new(+"abcd", "r+"); a = s.read(2); s.write("XY"); p [a, s.string]`,
			"[\"ab\", \"abXY\"]\n"},
		// mode: keyword and an integer mode are both honoured.
		{req + `p [StringIO.new(+"x", mode: "r").class, StringIO.new(+"x", File::WRONLY).class]`,
			"[StringIO, StringIO]\n"},
		// A frozen backing string opens read-only.
		{req + `s = StringIO.new("frozen".freeze); r = (s.write("y") rescue $!.class); p r`,
			"IOError\n"},

		// --- read ---------------------------------------------------------------
		// read(length) is ASCII-8BIT; read (no length) is the default encoding.
		{req + `s = StringIO.new("abc"); p [s.read(2).encoding.name, StringIO.new("abc").read.encoding.name]`,
			"[\"ASCII-8BIT\", \"UTF-8\"]\n"},
		// #to_int coercion of the length argument.
		{req + `o = Object.new; def o.to_int; 3; end; p StringIO.new("hello").read(o)`,
			"\"hel\"\n"},
		// A #to_str-convertible output buffer is filled and returned.
		{req + `o = Object.new; def o.to_str; +""; end; b = StringIO.new("hello").read(3, o); p b`,
			"\"hel\"\n"},
		// nil vs "" at EOF: a length read past EOF is nil, a zero-length read is "".
		{req + `s = StringIO.new("ab"); s.pos = 2; p [s.read(3), s.read(0)]`,
			"[nil, \"\"]\n"},

		// --- gets separator / limit / $/ ---------------------------------------
		// A #to_str separator and a #to_int limit are each coerced.
		{req + `o = Object.new; def o.to_str; ">"; end; p StringIO.new("a>b>c").gets(o)`,
			"\"a>\"\n"},
		{req + `o = Object.new; def o.to_int; 2; end; p StringIO.new("hello").gets(o)`,
			"\"he\"\n"},
		// Separator and limit together; the limit truncates before the separator.
		{req + `s = StringIO.new("this>is"); p [s.gets(">", 8), s.gets(">", 2)]`,
			"[\"this>\", \"is\"]\n"},
		// A nil separator with a limit reads the limit's worth of the remainder.
		{req + `p StringIO.new("this>is").gets(nil, 5)`, "\"this>\"\n"},
		// The default separator follows $/.
		{req + `$/ = " "; r = StringIO.new("an example").gets; $/ = "\n"; p r`, "\"an \"\n"},
		// chomp strips the separator (and a preceding \r for the "\n" default).
		{req + `p StringIO.new("a\r\nb").gets(chomp: true)`, "\"a\"\n"},

		// --- paragraph mode -----------------------------------------------------
		// StringIO keeps the whole newline run; a limit still caps the bytes.
		{req + `p [StringIO.new("a\n\n\nb").gets(""), StringIO.new("one\n\ntwo").gets("", 4)]`,
			"[\"a\\n\\n\\n\", \"one\\n\"]\n"},

		// --- readlines / each_line ---------------------------------------------
		{req + `p StringIO.new("a>b>c").readlines(">")`, "[\"a>\", \"b>\", \"c\"]\n"},
		{req + `a = []; StringIO.new("x>y").each_line(">") { |l| a << l }; p a`,
			"[\"x>\", \"y\"]\n"},

		// --- print / puts / write ----------------------------------------------
		// print with no args writes $_; multiple args use $, and terminate with $\.
		{req + `$_ = "LL"; s = StringIO.new; s.print; p s.string`, "\"LL\"\n"},
		{req + `$, = "-"; $\ = "!"; s = StringIO.new; s.print("a", "b"); $, = nil; $\ = nil; p s.string`,
			"\"a-b!\"\n"},
		// write/<< stringify via #to_s.
		{req + `o = Object.new; def o.to_s; "TS"; end; s = StringIO.new; s.write(o); s << o; p s.string`,
			"\"TSTS\"\n"},
		// puts expands a #to_ary object, one element per line.
		{req + `o = Object.new; def o.to_ary; ["p", "q"]; end; s = StringIO.new; s.puts(o); p s.string`,
			"\"p\\nq\\n\"\n"},

		// --- #each alias --------------------------------------------------------
		{req + `p [StringIO.instance_method(:each) == StringIO.instance_method(:each_line), IO.instance_method(:each) == IO.instance_method(:each_line)]`,
			"[true, true]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}

	errs := []struct{ src, want string }{
		// A read-only StringIO rejects writes; a write-only one rejects reads.
		{req + `StringIO.new(+"x", "r").write("y")`, "not opened for writing"},
		{req + `StringIO.new(+"x", "w").read`, "not opened for reading"},
		{req + `StringIO.new(+"x", "w").gets`, "not opened for reading"},
		// A negative read length is an ArgumentError.
		{req + `StringIO.new("x").read(-1)`, "negative length"},
		// A read length that is neither Integer nor #to_int-convertible.
		{req + `StringIO.new("x").read(Object.new)`, "into Integer"},
		// A read buffer that cannot become a String.
		{req + `StringIO.new("hello").read(3, Object.new)`, "into String"},
		// A zero limit for a line-iterating read is rejected.
		{req + `StringIO.new("x\ny").readlines(0)`, "invalid limit: 0 for readlines"},
		{req + `StringIO.new("x\ny").each_line(0) { |l| l }`, "invalid limit: 0 for each_line"},
		// An invalid StringIO mode string.
		{req + `StringIO.new(+"x", "z")`, "invalid access mode"},
	}
	for _, c := range errs {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got=%v want error containing %q", c.src, err, c.want)
		}
	}
}
