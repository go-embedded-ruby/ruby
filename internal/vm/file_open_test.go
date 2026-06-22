package vm_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFileStreams covers File.open and the file-backed IO it returns, asserted
// against MRI Ruby 4.0.5 over a temp directory.
func TestFileStreams(t *testing.T) {
	dir := filepath.ToSlash(t.TempDir())
	// write seeds a fresh file and returns its '/'-path for interpolation.
	write := func(name, content string) string {
		p := dir + "/" + name
		if err := os.WriteFile(filepath.FromSlash(p), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	w := dir + "/w.txt"
	xyz := write("xyz.txt", "x\ny\nz")
	hello := write("hello.txt", "hello")
	lines := write("lines.txt", "one\ntwo\n")
	pq := write("pq.txt", "p\nq\n")
	ab := write("ab.txt", "AB")
	abc := write("abc.txt", "abc")

	cases := []struct{ src, want string }{
		// Write via the block form (auto flush+close), then read back.
		{fmt.Sprintf(`File.open(%q, "w") { |io| io.puts("a"); io.print("b") }; p File.read(%q)`, w, w), "\"a\\nb\"\n"},
		// Read line by line.
		{fmt.Sprintf(`p File.open(%q) { |io| [io.gets, io.gets, io.gets] }`, xyz), "[\"x\\n\", \"y\\n\", \"z\"]\n"},
		// read(n), class, and File < IO.
		{fmt.Sprintf(`f = File.open(%q); p [f.read(3), f.class, f.is_a?(IO)]; f.close`, hello), "[\"hel\", File, true]\n"},
		{fmt.Sprintf(`p File.readlines(%q)`, lines), "[\"one\\n\", \"two\\n\"]\n"},
		{fmt.Sprintf(`r = []; File.foreach(%q) { |l| r << l.chomp }; p r`, pq), "[\"p\", \"q\"]\n"},
		{fmt.Sprintf(`r = []; File.open(%q) { |io| io.each_line { |l| r << l } }; p r`, pq), "[\"p\\n\", \"q\\n\"]\n"},
		// Append mode keeps existing content.
		{fmt.Sprintf(`File.open(%q, "a") { |io| io.write("CD") }; p File.read(%q)`, ab, ab), "\"ABCD\"\n"},
		// rewind re-reads from the start.
		{fmt.Sprintf(`File.open(%q) { |io| io.read; io.rewind; p io.gets }`, pq), "\"p\\n\"\n"},
		// r+ reads then overwrites at the cursor.
		{fmt.Sprintf(`File.open(%q, "r+") { |io| io.read(1); io.write("X") }; p File.read(%q)`, abc, abc), "\"aXc\"\n"},
		// The block's value is returned.
		{fmt.Sprintf(`p File.open(%q) { |io| 99 }`, hello), "99\n"},
		// flush writes the buffer to disk mid-stream.
		{fmt.Sprintf(`File.open(%q, "w") { |io| io.write("A"); io.flush; p File.read(%q) }`, w, w), "\"A\"\n"},
		// Non-block open: write, then close flushes.
		{fmt.Sprintf(`f = File.open(%q, "w"); f.write("Z"); f.close; p File.read(%q)`, w, w), "\"Z\"\n"},
		{fmt.Sprintf(`p File.open(%q, "w").class`, w), "File\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}

	errs := []struct{ src, want string }{
		{fmt.Sprintf(`File.open(%q)`, dir+"/nope.txt"), "No such file"},                         // missing file, mode "r"
		{fmt.Sprintf(`File.open(%q, "")`, hello), "invalid access mode"},                        // empty mode
		{fmt.Sprintf(`File.open(%q, "z")`, hello), "invalid access mode"},                       // unknown mode
		{fmt.Sprintf(`File.open(%q, "w") { |io| io.write("x") }`, dir+"/no/x"), "No such file"}, // flush into missing dir
		{fmt.Sprintf(`File.readlines(%q)`, dir+"/nope.txt"), "No such file"},
	}
	for _, c := range errs {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got=%v want error containing %q", c.src, err, c.want)
		}
	}
}
