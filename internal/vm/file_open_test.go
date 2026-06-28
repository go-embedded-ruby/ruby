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

// TestFileOpenIntMode covers File.open with an integer open-mode (a bit-OR of
// the File::Constants flags) and a trailing opts Hash, as Puppet's Uniquefile
// uses it. The flag combinations map to the same access modes as the string
// forms. Asserted against MRI 4.0.5 over a temp directory.
func TestFileOpenIntMode(t *testing.T) {
	dir := filepath.ToSlash(t.TempDir())
	mk := func(name, content string) string {
		p := dir + "/" + name
		if err := os.WriteFile(filepath.FromSlash(p), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	fresh := dir + "/fresh.txt"
	rw := mk("rw.txt", "abcd")
	app := mk("app.txt", "AB")
	apprw := mk("apprw.txt", "AB")
	won := mk("won.txt", "old")
	ro := mk("ro.txt", "read")

	cases := []struct{ src, want string }{
		// RDWR|CREAT|EXCL with a perm opts Hash: creates an empty file ("w+").
		{fmt.Sprintf(`File.open(%q, File::RDWR | File::CREAT | File::EXCL, perm: 0o600) { |io| io.print "hi" }; p File.read(%q)`, fresh, fresh), "\"hi\"\n"},
		// Plain RDWR keeps existing content and overwrites at the cursor ("r+").
		{fmt.Sprintf(`File.open(%q, File::RDWR) { |io| io.read(1); io.write("X") }; p File.read(%q)`, rw, rw), "\"aXcd\"\n"},
		// APPEND adds to the end ("a").
		{fmt.Sprintf(`File.open(%q, File::WRONLY | File::APPEND) { |io| io.write("CD") }; p File.read(%q)`, app, app), "\"ABCD\"\n"},
		// RDWR|APPEND ("a+") also appends.
		{fmt.Sprintf(`File.open(%q, File::RDWR | File::APPEND) { |io| io.write("Z") }; p File.read(%q)`, apprw, apprw), "\"ABZ\"\n"},
		// WRONLY|CREAT|TRUNC truncates ("w").
		{fmt.Sprintf(`File.open(%q, File::WRONLY | File::CREAT | File::TRUNC) { |io| io.write("N") }; p File.read(%q)`, won, won), "\"N\"\n"},
		// RDONLY (0) reads.
		{fmt.Sprintf(`p File.open(%q, File::RDONLY) { |io| io.read }`, ro), "\"read\"\n"},
		// The constants carry the canonical POSIX values.
		{`p [File::RDONLY, File::WRONLY, File::RDWR]`, "[0, 1, 2]\n"},
		{`p [File::CREAT, File::EXCL, File::TRUNC, File::APPEND]`, "[64, 128, 512, 8]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestFileRenameAndInstanceMeta covers File.rename and the File instance
// path/chmod/chown methods (the ops Puppet's replace_file drives). chmod/chown
// only assert success and that they leave content intact — not the resulting
// permission bits or owner, which differ on Windows.
func TestFileRenameAndInstanceMeta(t *testing.T) {
	dir := filepath.ToSlash(t.TempDir())
	src := dir + "/src.txt"
	dst := dir + "/dst.txt"
	meta := dir + "/meta.txt"
	if err := os.WriteFile(filepath.FromSlash(src), []byte("moved"), 0o644); err != nil {
		t.Fatal(err)
	}

	cases := []struct{ s, want string }{
		// rename moves the file and returns 0.
		{fmt.Sprintf(`r = File.rename(%q, %q); p [r, File.exist?(%q), File.read(%q)]`, src, dst, src, dst),
			"[0, false, \"moved\"]\n"},
		// path / to_path report the backing path.
		{fmt.Sprintf(`f = File.open(%q, "w"); p [f.path, f.to_path]; f.close`, meta), fmt.Sprintf("[%q, %q]\n", meta, meta)},
		// chmod / chown return 0 and flush the buffer so content survives.
		{fmt.Sprintf(`f = File.open(%q, "w"); f.write("keep"); p f.chmod(0o644); f.close; p File.read(%q)`, meta, meta),
			"0\n\"keep\"\n"},
		{fmt.Sprintf(`f = File.open(%q, "w"); f.write("own"); p f.chown(-1, -1); f.close; p File.read(%q)`, meta, meta),
			"0\n\"own\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.s); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.s, got, c.want)
		}
	}

	// Error paths: rename / chmod / chown on a non-existent path raise Errno.
	errs := []string{
		fmt.Sprintf(`File.rename(%q, %q)`, dir+"/nope.txt", dst),
	}
	for _, s := range errs {
		if err := runErr(t, s); err == nil || !strings.Contains(err.Error(), "No such file") {
			t.Errorf("src=%q got=%v want No such file", s, err)
		}
	}
}
