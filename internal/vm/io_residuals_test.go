package vm_test

import (
	"os"
	"path/filepath"
	"runtime"
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

// TestIOClassReaders covers the IO.readlines / IO.foreach class methods'
// separator/limit/$/ and chomp handling, and the mode-flag decoding, against
// MRI Ruby 4.0.6. The reader class methods open a real file, so a temp file is
// written first.
func TestIOClassReaders(t *testing.T) {
	dir := filepath.ToSlash(t.TempDir())
	write := func(name, content string) string {
		p := dir + "/" + name
		if err := os.WriteFile(filepath.FromSlash(p), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return `"` + p + `"`
	}
	cases := []struct{ src, want string }{
		// A #to_str-convertible separator splits the lines.
		func() (c struct{ src, want string }) {
			c.src = `o = Object.new; def o.to_str; ">"; end; p IO.readlines(` + write("r1", "a>b>c") + `, o)`
			c.want = "[\"a>\", \"b>\", \"c\"]\n"
			return
		}(),
		// An Integer limit caps each line's byte length.
		func() (c struct{ src, want string }) {
			c.src = `p IO.readlines(` + write("r2", "hello world") + `, 4)`
			c.want = "[\"hell\", \"o wo\", \"rld\"]\n"
			return
		}(),
		// The default separator follows $/.
		func() (c struct{ src, want string }) {
			c.src = `$/ = " "; r = IO.readlines(` + write("r3", "an example here") + `); $/ = "\n"; p r`
			c.want = "[\"an \", \"example \", \"here\"]\n"
			return
		}(),
		// chomp: as a keyword option strips the separator.
		func() (c struct{ src, want string }) {
			c.src = `p IO.readlines(` + write("r4", "x\ny\n") + `, chomp: true)`
			c.want = "[\"x\", \"y\"]\n"
			return
		}(),
		// IO.foreach with a block yields each line; the separator is honoured.
		func() (c struct{ src, want string }) {
			c.src = `a = []; IO.foreach(` + write("r5", "p>q>r") + `, ">") { |l| a << l }; p a`
			c.want = "[\"p>\", \"q>\", \"r\"]\n"
			return
		}(),
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}

	// A zero limit for the line-iterating class readers is rejected.
	if err := runErr(t, `IO.readlines(`+write("e1", "x\ny")+`, 0)`); err == nil || !strings.Contains(err.Error(), "invalid limit: 0 for readlines") {
		t.Errorf("IO.readlines(path, 0) got %v", err)
	}
}

// TestStringIOModeFlags covers the remaining StringIO mode variants — a+, w+,
// binary/text suffixes, and encoding-tagged modes — against MRI Ruby 4.0.6.
func TestStringIOModeFlags(t *testing.T) {
	const req = `require "stringio"; `
	cases := []struct{ src, want string }{
		// "a+" appends on write but is readable from the start.
		{req + `s = StringIO.new(+"data", "a+"); a = s.read; s.write("X"); p [a, s.string]`,
			"[\"data\", \"dataX\"]\n"},
		// "w+" truncates yet stays readable and writable.
		{req + `s = StringIO.new(+"data", "w+"); s.write("hi"); s.rewind; p [s.read, s.string]`,
			"[\"hi\", \"hi\"]\n"},
		// A "rb" (binary) suffix keeps read access; a trailing :enc is tolerated.
		{req + `p [StringIO.new(+"x", "rb").read, StringIO.new(+"x", "r:utf-8").read]`,
			"[\"x\", \"x\"]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestIOResidualBranches exercises the less-travelled branches of the reader/
// writer paths so they are covered: enumerator forms, recursive-array puts,
// paragraph mode on a real IO (two-newline rule, chomp with and without a
// terminating run, leading blanks, EOF), a real-String read buffer, and File
// modes that gate reads/writes. Values verified against MRI Ruby 4.0.6.
func TestIOResidualBranches(t *testing.T) {
	const req = `require "stringio"; `
	cases := []struct{ src, want string }{
		// Enumerator forms (no block) iterate correctly and report #size nil.
		{req + `p StringIO.new("ab").each_char.to_a`, "[\"a\", \"b\"]\n"},
		{req + `p StringIO.new("ab").each_byte.to_a`, "[97, 98]\n"},
		{req + `p StringIO.new("x\ny\n").each_line.to_a`, "[\"x\\n\", \"y\\n\"]\n"},
		{req + `p StringIO.new("ab").each_char.size`, "nil\n"},
		// puts writes [...] for a self-referential array and appends a newline only
		// when the string does not already end in one.
		{req + `a = [1]; a << a; s = StringIO.new; s.puts(a); p s.string`, "\"1\\n[...]\\n\"\n"},
		{req + `s = StringIO.new; s.puts("x\n", "y"); p s.string`, "\"x\\ny\\n\"\n"},
		{req + `s = StringIO.new; s.puts([]); p s.string`, "\"\"\n"},
		// Paragraph mode edges: leading blank lines skipped; chomp strips a
		// terminating run but keeps a lone EOF newline; nil at true EOF.
		{req + `p StringIO.new("\n\nabc").gets("")`, "\"abc\"\n"},
		// A buffer of only blank lines is consumed to EOF and yields nil.
		{req + `p StringIO.new("\n\n").gets("")`, "nil\n"},
		{req + `p StringIO.new("a\n\nb\n").each_line("", chomp: true).to_a`, "[\"a\", \"b\\n\"]\n"},
		{req + `s = StringIO.new("x"); s.read; p s.gets("")`, "nil\n"},
		// A real String read buffer is filled in place and returned.
		{req + `s = StringIO.new("hello"); b = +"z"; r = s.read(3, b); p [r, b, r.equal?(b)]`,
			"[\"hel\", \"hel\", true]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestFileModeAccess covers openFileIO's mode-driven read/write gating: a File
// opened write-only rejects reads, append-only rejects reads and appends every
// write at end, and read-only rejects writes. Verified against MRI Ruby 4.0.6.
func TestFileModeAccess(t *testing.T) {
	dir := filepath.ToSlash(t.TempDir())
	path := func(name, content string) string {
		p := dir + "/" + name
		if err := os.WriteFile(filepath.FromSlash(p), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return `"` + p + `"`
	}
	// Append mode writes at end and reports the appended content on reopen.
	a1 := path("a1", "base")
	if got := eval(t, `File.open(`+a1+`, "a") { |f| f.write("+more") }; p File.read(`+a1+`)`); got != "\"base+more\"\n" {
		t.Errorf("append-mode File = %q", got)
	}
	oks := []struct{ src, want string }{
		// Paragraph mode on a real File keeps only the two terminating newlines.
		{`p(File.open(` + path("pg", "a\n\n\nb") + `, "r") { |f| f.gets("") })`, "\"a\\n\\n\"\n"},
		// A write-only File with no explicit encoding reports a nil external encoding.
		{`p(File.open(` + path("we", "") + `, "w") { |f| f.external_encoding })`, "nil\n"},
	}
	if runtime.GOOS != "windows" {
		// A character device (non-regular) opens with an empty buffer, so a read
		// sees end-of-file rather than an unbounded stream; appending to one just
		// reports the byte count without buffering readable content. /dev/null is
		// POSIX-only (Windows spells the null device NUL), so gate these two.
		oks = append(oks,
			struct{ src, want string }{`p(File.open("/dev/null", "r") { |f| f.read })`, "\"\"\n"},
			struct{ src, want string }{`p(File.open("/dev/null", "a") { |f| f.write("x") })`, "1\n"},
		)
	}
	for _, c := range oks {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
	errs := []struct{ src, want string }{
		{`File.open(` + path("w1", "x") + `, "w") { |f| f.read }`, "not opened for reading"},
		{`File.open(` + path("a2", "x") + `, "a") { |f| f.gets }`, "not opened for reading"},
		{`File.open(` + path("r1", "x") + `, "r") { |f| f.write("y") }`, "not opened for writing"},
	}
	for _, c := range errs {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got=%v want error containing %q", c.src, err, c.want)
		}
	}
}
