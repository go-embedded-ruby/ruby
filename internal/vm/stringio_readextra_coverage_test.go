package vm_test

import "testing"

// TestStringIOReadExtraCoverage exercises the buffer-backed byte/char readers
// (getbyte / readbyte / readchar) across their normal and end-of-file branches
// and the ungetbyte / ungetc push-back methods across their Integer, String,
// nil-no-op and wrong-type branches, so defIOReadExtra stays fully covered.
// Verified against MRI Ruby 4.0.6.
func TestStringIOReadExtraCoverage(t *testing.T) {
	const req = "require 'stringio'\n"
	ok := []struct{ src, want string }{
		// getbyte: two bytes then nil at EOF.
		{req + `s = StringIO.new("AB"); p [s.getbyte, s.getbyte, s.getbyte]`, "[65, 66, nil]\n"},
		// readbyte / readchar: a value then EOFError.
		{req + `s = StringIO.new("A"); p s.readbyte`, "65\n"},
		{req + `s = StringIO.new("A"); p s.readchar`, "\"A\"\n"},
		// ungetbyte: Integer, String (pushes each byte), and a nil no-op.
		{req + `s = StringIO.new("X"); s.getbyte; s.ungetbyte(65); p s.getbyte`, "65\n"},
		{req + `s = StringIO.new("X"); s.getbyte; s.ungetbyte("AB"); p [s.getbyte, s.getbyte]`, "[65, 66]\n"},
		{req + `s = StringIO.new("X"); p s.ungetbyte(nil)`, "nil\n"},
		// ungetc: Integer codepoint and String.
		{req + `s = StringIO.new("X"); s.getc; s.ungetc(90); p s.getc`, "\"Z\"\n"},
		{req + `s = StringIO.new("X"); s.getc; s.ungetc("Z"); p s.getc`, "\"Z\"\n"},
	}
	for _, c := range ok {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}

	errs := []struct{ src, want string }{
		// End-of-file raises EOFError for readbyte / readchar.
		{req + `StringIO.new("").readbyte`, "end of file"},
		{req + `StringIO.new("").readchar`, "end of file"},
		// A wrong-type push-back argument raises TypeError (the default branch).
		{req + `StringIO.new("X").ungetbyte([1])`, "TypeError"},
		{req + `StringIO.new("X").ungetc([1])`, "TypeError"},
	}
	for _, c := range errs {
		err := runErr(t, c.src)
		if err == nil {
			t.Errorf("runErr(%q) = nil, want error containing %q", c.src, c.want)
			continue
		}
		if !containsStr(err.Error(), c.want) {
			t.Errorf("runErr(%q) error = %q, want containing %q", c.src, err.Error(), c.want)
		}
	}
}

func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
