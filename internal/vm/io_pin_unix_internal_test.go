// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !windows && !wasm && !plan9

package vm

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestRememberFilePinsTheOpenedInode: a regular file is pinned and its
// identity is the pin's; unpin releases the descriptor. Without the pin, a
// file deleted and created again at once on ext4 takes the old inode number
// and passes for the opened one.
func TestRememberFilePinsTheOpenedInode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	writeFile(t, path, "abc", 0o644)
	o := &IOObj{path: path}
	o.rememberFile()
	if o.pin == nil || o.ident == nil {
		t.Fatalf("pin=%v ident=%v, want both", o.pin, o.ident)
	}
	pst, err := o.pin.Stat()
	if err != nil || !os.SameFile(pst, o.ident) {
		t.Errorf("the identity is not the pin's: %v", err)
	}
	pin := o.pin
	o.unpin()
	if o.pin != nil {
		t.Error("unpin left the pin")
	}
	if _, err := pin.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Errorf("unpin did not close the descriptor: %v", err)
	}
	o.unpin() // a second release is a no-op

	// Remembering again releases the previous pin first.
	o.rememberFile()
	first := o.pin
	o.rememberFile()
	if _, err := first.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Errorf("a second rememberFile leaked the first pin: %v", err)
	}
	o.unpin()
}

// TestRememberFileOutsideARegularFile: a directory is opened by pinFile and
// let go, and a file no open accepts keeps the plain stat identity.
func TestRememberFileOutsideARegularFile(t *testing.T) {
	dir := t.TempDir()
	d := &IOObj{path: dir}
	d.rememberFile()
	if d.pin != nil || d.ident != nil {
		t.Errorf("a directory: pin=%v ident=%v, want neither", d.pin, d.ident)
	}
	gone := &IOObj{path: filepath.Join(dir, "gone")}
	gone.rememberFile()
	if gone.pin != nil || gone.ident != nil {
		t.Errorf("a missing path: pin=%v ident=%v, want neither", gone.pin, gone.ident)
	}
	if os.Getuid() == 0 {
		t.Skip("root opens a file whatever its mode")
	}
	wo := filepath.Join(dir, "wo")
	writeFile(t, wo, "abc", 0o200)
	w := &IOObj{path: wo}
	w.rememberFile()
	if w.pin == nil {
		t.Error("a write-only file was not pinned through the write-only open")
	}
	w.unpin()
	none := filepath.Join(dir, "none")
	writeFile(t, none, "abc", 0o000)
	n := &IOObj{path: none}
	n.rememberFile()
	if n.pin != nil || n.ident == nil {
		t.Errorf("a file no open accepts: pin=%v ident=%v, want the stat identity alone", n.pin, n.ident)
	}
}

// TestDupPinGivesItsOwnDescriptor: the dup survives the original's close and
// names the same inode; nil, a closed pin and a failed dup give no pin.
func TestDupPinGivesItsOwnDescriptor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	writeFile(t, path, "abc", 0o644)
	pin := pinFile(path)
	if pin == nil {
		t.Fatal("pinFile refused a readable file")
	}
	dup := dupPin(pin)
	if dup == nil {
		t.Fatal("dupPin refused an open pin")
	}
	a, _ := pin.Stat()
	_ = pin.Close()
	b, err := dup.Stat()
	if err != nil || !os.SameFile(a, b) {
		t.Errorf("the dup does not hold the same inode after the original closed: %v", err)
	}
	_ = dup.Close()

	if dupPin(nil) != nil {
		t.Error("dupPin(nil) gave a pin")
	}
	if dupPin(pin) != nil {
		t.Error("dupPin of a closed pin gave a pin")
	}
	again := pinFile(path)
	defer func() { _ = again.Close() }()
	saved := dupFd
	dupFd = func(int) (int, error) { return -1, errors.New("dup refused") }
	defer func() { dupFd = saved }()
	if dupPin(again) != nil {
		t.Error("a failed dup gave a pin")
	}
}
