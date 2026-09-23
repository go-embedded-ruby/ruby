// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
)

// realpathTree builds the symlink tree the resolver tests share and returns its
// root, already resolved — the root itself may sit under a symlinked temp
// directory (macOS puts /tmp behind /private/tmp), and every expectation here is
// about what the resolver does INSIDE the tree.
func realpathTree(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the POSIX symlink tree needs privileges on Windows")
	}
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	mk := func(p string) string { return filepath.Join(base, p) }
	if err := os.Mkdir(mk("d"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mk("d/f"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, l := range [][2]string{
		{mk("d/absent"), mk("dangling")}, // -> a name that does not exist
		{mk("nodir/x"), mk("deep")},      // -> a name under a missing directory
		{"loop_b", mk("loop_a")},         // a relative two-link cycle
		{"loop_a", mk("loop_b")},         //
		{mk("d"), mk("dlink")},           // -> a real directory, absolute
		{"d", mk("rellink")},             // -> the same directory, relative
	} {
		if err := os.Symlink(l[0], l[1]); err != nil {
			t.Fatal(err)
		}
	}
	return base
}

// TestRealpathResolve drives the resolver over a real tree. Every expectation is
// MRI 4.0.5's own answer for the same path (File.realpath / File.realdirpath),
// captured on darwin.
func TestRealpathResolve(t *testing.T) {
	base := realpathTree(t)
	j := func(p string) string { return base + "/" + p }
	for _, c := range []struct {
		name          string
		path          string
		strict, trail bool
		want          string
		wantClass     string
		wantPath      string
	}{
		{name: "root", path: "/", want: "/"},
		{name: "plain file", path: j("d/f"), strict: true, want: j("d/f")},
		{name: "through a directory symlink", path: j("dlink/f"), strict: true, want: j("d/f")},
		{name: "through a relative directory symlink", path: j("rellink/f"), strict: true, want: j("d/f")},
		{name: "dot and dotdot", path: j("dlink/./../d/f"), strict: true, want: j("d/f")},
		{name: "dotdot past the root", path: "/../../", want: "/"},
		// realdirpath answers a dangling link with its target; realpath refuses it.
		{name: "dangling link, lenient", path: j("dangling"), want: j("d/absent")},
		{name: "dangling link, strict", path: j("dangling"), strict: true,
			wantClass: "Errno::ENOENT", wantPath: j("d/absent")},
		// An absent LAST component is the answer; an absent one in the middle is not.
		{name: "absent leaf", path: j("d/nope"), want: j("d/nope")},
		{name: "absent leaf, trailing separator", path: j("d/nope"), trail: true,
			wantClass: "Errno::ENOENT", wantPath: j("d/nope")},
		{name: "absent leaf, strict", path: j("d/nope"), strict: true,
			wantClass: "Errno::ENOENT", wantPath: j("d/nope")},
		{name: "absent directory in the middle", path: j("nodir/x"),
			wantClass: "Errno::ENOENT", wantPath: j("nodir")},
		{name: "link to a name under an absent directory", path: j("deep"),
			wantClass: "Errno::ENOENT", wantPath: j("nodir")},
		{name: "symlink cycle", path: j("loop_a"),
			wantClass: "Errno::ELOOP", wantPath: j("loop_a")},
		// Walking through a regular file is ENOTDIR, not ENOENT.
		{name: "file used as a directory", path: j("d/f/x"),
			wantClass: "Errno::ENOTDIR", wantPath: j("d/f/x")},
	} {
		got, err := realpathResolve(c.path, c.strict, c.trail)
		if c.wantClass != "" {
			if err == nil {
				t.Errorf("%s: resolved to %q, want %s", c.name, got, c.wantClass)
				continue
			}
			if err.class != c.wantClass || err.path != c.wantPath {
				t.Errorf("%s: got %s at %q, want %s at %q", c.name, err.class, err.path, c.wantClass, c.wantPath)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
		} else if got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

// TestRealpathLoopCache covers the loop table's second role: a symlink already
// resolved once is answered from the table rather than walked again, which is the
// branch a single traversal never reaches.
func TestRealpathLoopCache(t *testing.T) {
	base := realpathTree(t)
	got, err := realpathResolve(base+"/dlink/../dlink/f", true, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := base + "/d/f"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestRealpathFallback covers realpath_rec's fallback (ruby/ruby v3_4_0
// file.c:4383): when a symlink's TARGET is missing but the symlink itself stats
// clean, the symlink's own path is the answer. A dangling link cannot produce
// that state on a real filesystem — stat follows the link and fails too — so the
// stat seam supplies it.
func TestRealpathFallback(t *testing.T) {
	base := realpathTree(t)
	defer func(orig func(string) (fs.FileInfo, error)) { osStat = orig }(osStat)
	osStat = func(string) (fs.FileInfo, error) { return nil, nil }
	got, err := realpathResolve(base+"/dangling", true, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := base + "/dangling"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestRealpathReadlinkFailure covers the readlink error branch: lstat says the
// component is a symlink but reading it fails, which a real filesystem only
// produces under a race.
func TestRealpathReadlinkFailure(t *testing.T) {
	base := realpathTree(t)
	defer func(orig func(string) (string, error)) { osReadlink = orig }(osReadlink)
	osReadlink = func(string) (string, error) { return "", syscall.EACCES }
	_, err := realpathResolve(base+"/dlink/f", true, false)
	if err == nil || err.class != "Errno::EACCES" {
		t.Fatalf("got %v, want Errno::EACCES", err)
	}
}

// TestRealpathSyscallErr covers the three shapes realpathSyscallErr distinguishes:
// an errno with a registered class, an errno without one, and an error that is
// not a syscall.Errno at all.
func TestRealpathSyscallErr(t *testing.T) {
	if e := realpathSyscallErr(syscall.ENOTDIR, "/p"); e.class != "Errno::ENOTDIR" || e.message != "Not a directory" {
		t.Errorf("ENOTDIR: got %s/%q", e.class, e.message)
	}
	if e := realpathSyscallErr(syscall.Errno(1<<20), "/p"); e.class != "SystemCallError" {
		t.Errorf("unregistered errno: got %s", e.class)
	}
	if e := realpathSyscallErr(errors.New("not an errno"), "/p"); e.class != "Errno::ENOENT" {
		t.Errorf("plain error: got %s", e.class)
	}
	if got := (&realpathErr{"Errno::ENOENT", "No such file or directory", "/p"}).Error(); got != "No such file or directory - /p" {
		t.Errorf("Error(): got %q", got)
	}
}

// TestRealpathParentAndJoin pins the two path primitives at the root, where the
// separator handling differs from every other position.
func TestRealpathParentAndJoin(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"/a/b", "/a"}, {"/a", "/"}, {"/", "/"}, {"", "/"},
	} {
		if got := realpathParent(c.in); got != c.want {
			t.Errorf("realpathParent(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	if got := realpathJoin("/", "a"); got != "/a" {
		t.Errorf("realpathJoin at root = %q", got)
	}
	if got := realpathJoin("/a", "b"); got != "/a/b" {
		t.Errorf("realpathJoin = %q", got)
	}
}

// TestRealpathRubySurface checks the two wordings MRI uses for the SAME failure,
// which is the part of File.realpath that is not the resolver: realpath(3) fails
// first there, so ENOENT and ELOOP name rb_check_realpath_internal and the whole
// argument, while ENOTDIR falls through to the emulation and names realpath_rec
// and the component. File.realdirpath is the emulation, so it always says
// realpath_rec.
func TestRealpathRubySurface(t *testing.T) {
	base := realpathTree(t)
	for _, c := range []struct{ src, want string }{
		{`File.realpath("` + base + `/d/nope")`,
			"No such file or directory @ rb_check_realpath_internal - " + base + "/d/nope"},
		{`File.realpath("` + base + `/loop_a")`,
			"Too many levels of symbolic links @ rb_check_realpath_internal - " + base + "/loop_a"},
		{`File.realpath("` + base + `/d/f/x")`,
			"Not a directory @ realpath_rec - " + base + "/d/f/x"},
		{`File.realdirpath("` + base + `/nodir/x")`,
			"No such file or directory @ realpath_rec - " + base + "/nodir"},
		{`File.realdirpath("` + base + `/loop_a")`,
			"Too many levels of symbolic links @ realpath_rec - " + base + "/loop_a"},
	} {
		got := runFS(t, `begin; `+c.src+`; rescue SystemCallError => e; puts e.message; end`)
		if got != c.want+"\n" {
			t.Errorf("%s\n got=%q\nwant=%q", c.src, got, c.want+"\n")
		}
	}
	// The resolving answers themselves, through the Ruby surface.
	if got := runFS(t, `puts File.realdirpath("`+base+`/dangling")`); got != base+"/d/absent\n" {
		t.Errorf("realdirpath of a dangling link: got %q", got)
	}
	if got := runFS(t, `puts File.realpath("f", "`+base+`/dlink")`); got != base+"/d/f\n" {
		t.Errorf("realpath with a base directory: got %q", got)
	}
}
