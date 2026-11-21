// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestIOPositionalPathAnswersOnlyForARegularFile: #pwrite and #pread reach the
// disk only through a stream that names a regular file. Each refusal below
// would otherwise send a positional write to a path that is not this stream's
// file: the pipe and popen rows carry a real regular file precisely so that
// only their own guard can refuse them.
func TestIOPositionalPathAnswersOnlyForARegularFile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "f")
	if err := os.WriteFile(file, []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		o    *IOObj
		want bool
	}{
		{"a regular file", &IOObj{path: file}, true},
		{"no path (a StringIO)", &IOObj{}, false},
		{"a path that does not resolve", &IOObj{path: filepath.Join(dir, "gone")}, false},
		{"a directory", &IOObj{path: dir}, false},
		{"a pipe end, even over a regular path", &IOObj{path: file, pipe: &pipeBuf{}}, false},
		{"a popen stream, even over a regular path", &IOObj{path: file, popen: &popenProc{}}, false},
	}
	for _, c := range cases {
		got, ok := ioPositionalPath(c.o)
		if ok != c.want {
			t.Errorf("%s: ok = %v, want %v", c.name, ok, c.want)
		}
		if ok && got != file {
			t.Errorf("%s: path = %q, want %q", c.name, got, file)
		}
	}
}

// TestWriteAtWithOwnerWriteKeepsTheRefusal: when the mode cannot be read, the
// retry must give the original refusal back rather than go on with no mode to
// restore.
func TestWriteAtWithOwnerWriteKeepsTheRefusal(t *testing.T) {
	refused := errors.New("the first refusal")
	err := writeAtWithOwnerWrite(filepath.Join(t.TempDir(), "gone"), []byte("x"), 0, refused)
	if !errors.Is(err, refused) {
		t.Errorf("got %v, want the original refusal", err)
	}
}

// TestWriteAtPathReportsAFailedWrite: a write that fails after the open
// succeeded is reported, not swallowed behind a clean Close.
func TestWriteAtPathReportsAFailedWrite(t *testing.T) {
	file := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(file, []byte("0123"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeAtPath(file, []byte("x"), -1); err == nil {
		t.Error("a write at offset -1 reported success")
	}
}

// TestPreadFileErrors separates the three answers IO#pread acts on: EOF (raised
// as EOFError), a real error (the caller falls back to the buffer), and data.
func TestPreadFileErrors(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "f")
	if err := os.WriteFile(file, []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := preadFile(file, 3, 7); err != nil || string(got) != "789" {
		t.Errorf("read at 7: got %q, %v", got, err)
	}
	if _, err := preadFile(file, 1, 10); !errors.Is(err, io.EOF) {
		t.Errorf("read at the end: got %v, want io.EOF", err)
	}
	if _, err := preadFile(filepath.Join(dir, "gone"), 1, 0); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("missing file: got %v, want fs.ErrNotExist", err)
	}
	if runtime.GOOS != "windows" { // reading a directory handle is an error on unix
		if _, err := preadFile(dir, 1, 0); err == nil || errors.Is(err, io.EOF) {
			t.Errorf("a directory: got %v, want a read error that is not EOF", err)
		}
	}
}
