// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// identOf records a file's identity the way rememberFile does.
func identOf(t *testing.T, path string) os.FileInfo {
	t.Helper()
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	_ = os.SameFile(st, st)
	return st
}

// writeFile writes content to path, failing the test on error.
func writeFile(t *testing.T, path, content string, perm os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), perm); err != nil {
		t.Fatal(err)
	}
}

// replaceFile renames a new file with content over path.
func replaceFile(t *testing.T, path, content string, perm os.FileMode) {
	t.Helper()
	tmp := path + ".new"
	writeFile(t, tmp, content, perm)
	if err := os.Rename(tmp, path); err != nil {
		t.Fatal(err)
	}
}

// TestOpenOpenedFileOpensOnlyThatFile: each refusal below is a path that names
// something other than the file the stream opened.
func TestOpenOpenedFileOpensOnlyThatFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	writeFile(t, path, "abc", 0o644)
	ident := identOf(t, path)

	f, err := openOpenedFile(path, ident, os.O_RDONLY)
	if err != nil {
		t.Fatalf("the opened file itself: %v", err)
	}
	_ = f.Close()

	if _, err := openOpenedFile(filepath.Join(dir, "gone"), ident, os.O_RDONLY); !errors.Is(err, errNotTheOpenedFile) {
		t.Errorf("a path that is gone: got %v, want errNotTheOpenedFile", err)
	}
	if _, err := openOpenedFile(filepath.Join(dir, "gone"), ident, os.O_WRONLY|os.O_CREATE); !errors.Is(err, errNotTheOpenedFile) {
		t.Errorf("a path that is gone, with O_CREATE: got %v, want errNotTheOpenedFile", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "gone")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a refused O_CREATE created the file: %v", err)
	}

	replaceFile(t, path, "other", 0o644)
	if _, err := openOpenedFile(path, ident, os.O_WRONLY); !errors.Is(err, errNotTheOpenedFile) {
		t.Errorf("a file renamed over the path: got %v, want errNotTheOpenedFile", err)
	}

	// With no identity it is a plain open, which is what a stream with no
	// regular file behind it keeps.
	f, err = openOpenedFile(path, nil, os.O_RDONLY)
	if err != nil {
		t.Fatalf("no identity: %v", err)
	}
	_ = f.Close()
	if _, err := openOpenedFile(filepath.Join(dir, "gone"), nil, os.O_RDONLY); errors.Is(err, errNotTheOpenedFile) || err == nil {
		t.Errorf("no identity, a path that is gone: got %v, want the open's own error", err)
	}
}

// TestOpenOpenedFileChecksTheDescriptor: a rename between the check on the
// path and the open is caught by the check on what was opened, and the file
// that took the path is not written.
func TestOpenOpenedFileChecksTheDescriptor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	writeFile(t, path, "abc", 0o644)
	ident := identOf(t, path)

	betweenCheckAndOpen = func(p string) { replaceFile(t, p, "other", 0o644) }
	defer func() { betweenCheckAndOpen = nil }()
	f, err := openOpenedFile(path, ident, os.O_WRONLY)
	if !errors.Is(err, errNotTheOpenedFile) {
		if f != nil {
			_ = f.Close()
		}
		t.Fatalf("got %v, want errNotTheOpenedFile", err)
	}
	if f != nil {
		t.Error("a refused open handed back a file")
	}
	if got, _ := os.ReadFile(path); string(got) != "other" {
		t.Errorf("the replacement holds %q, want \"other\"", got)
	}
}

// TestTruncateOpenedFile: the opened file is truncated, a replacement is not,
// and a stream with no identity truncates its path.
func TestTruncateOpenedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	writeFile(t, path, "abcdef", 0o644)
	o := &IOObj{path: path}
	o.rememberFile()
	if o.ident == nil {
		t.Fatal("rememberFile recorded nothing for a regular file")
	}
	if err := o.truncateOpenedFile(2); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != "ab" {
		t.Errorf("the opened file: %q, want \"ab\"", got)
	}
	replaceFile(t, path, "other", 0o644)
	if err := o.truncateOpenedFile(1); err != nil {
		t.Errorf("a replaced path: %v, want nil", err)
	}
	if got, _ := os.ReadFile(path); string(got) != "other" {
		t.Errorf("the replacement: %q, want \"other\"", got)
	}
	plain := &IOObj{path: path}
	if err := plain.truncateOpenedFile(1); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != "o" {
		t.Errorf("no identity: %q, want \"o\"", got)
	}
	if err := (&IOObj{path: filepath.Join(dir, "gone"), ident: o.ident}).truncateOpenedFile(0); err != nil {
		t.Errorf("a path that is gone: %v, want nil", err)
	}

	// The opened file's own errors are reported, not taken for a replacement.
	own := filepath.Join(dir, "own")
	writeFile(t, own, "abc", 0o644)
	ow := &IOObj{path: own}
	ow.rememberFile()
	if err := ow.truncateOpenedFile(-1); err == nil {
		t.Error("a truncate to -1 reported success")
	}
	if runtime.GOOS != "windows" && os.Getuid() != 0 {
		if err := os.Chmod(own, 0o444); err != nil {
			t.Fatal(err)
		}
		if err := ow.truncateOpenedFile(0); !errors.Is(err, os.ErrPermission) {
			t.Errorf("a read-only file: %v, want a permission error", err)
		}
	}

	// rememberFile records nothing for what is not a regular file.
	d := &IOObj{path: dir}
	d.rememberFile()
	if d.ident != nil {
		t.Error("rememberFile recorded a directory")
	}
}

// TestTruncateOnDiskRaisesARefusal: a truncate the file refuses is raised with
// its errno. A directory under a stream that recorded no file is the case: the
// open for writing is refused before any truncate.
func TestTruncateOnDiskRaisesARefusal(t *testing.T) {
	o := &IOObj{path: t.TempDir()}
	if got := catchRaise(func() { o.truncateOnDisk(0) }); !strings.HasPrefix(got, "Errno::") {
		t.Errorf("truncating a directory raised %q, want an Errno", got)
	}
	// A replaced path is left alone, and nothing is raised.
	path := filepath.Join(t.TempDir(), "f")
	writeFile(t, path, "abcdef", 0o644)
	r := &IOObj{path: path}
	r.rememberFile()
	defer r.unpin()
	replaceFile(t, path, "other", 0o644)
	if got := catchRaise(func() { r.truncateOnDisk(1) }); got != "" {
		t.Errorf("a replaced path raised %q", got)
	}
	if got, _ := os.ReadFile(path); string(got) != "other" {
		t.Errorf("the replacement holds %q, want \"other\"", got)
	}
}

// TestWithOwnerWriteTouchesOnlyTheOpenedFile: the owner-write grant goes to
// the file the stream opened, never to what its path names now.
func TestWithOwnerWriteTouchesOnlyTheOpenedFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("asserts Unix permission bits")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	writeFile(t, path, "abc", 0o444)
	ident := identOf(t, path)
	refused := errors.New("refused")

	// The path names another file: nothing runs, nothing is chmodded.
	replaceFile(t, path, "other", 0o444)
	called := false
	err := withOwnerWrite(path, ident, refused, func() error { called = true; return nil })
	if !errors.Is(err, errNotTheOpenedFile) || called {
		t.Errorf("a replaced path: err=%v called=%v, want errNotTheOpenedFile and no write", err, called)
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o444 {
		t.Errorf("the replacement's mode became %v", st.Mode().Perm())
	}

	// Replaced DURING the write: the restoring chmod is skipped, so the
	// file that took the path keeps its own owner-write bit.
	ident = identOf(t, path)
	err = withOwnerWrite(path, ident, refused, func() error {
		replaceFile(t, path, "third", 0o644)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o644 {
		t.Errorf("a file put in place during the write: mode %v, want 0644", st.Mode().Perm())
	}
}
