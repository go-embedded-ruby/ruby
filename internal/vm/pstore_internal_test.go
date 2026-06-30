// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/object"
	"github.com/go-ruby-marshal/marshal"
)

// errInjected is the sentinel returned by the fault-injection seams (createTemp /
// renameFile / flockSyscall) so a test can assert Store / flockFile surface it.
var errInjected = errors.New("pstore: injected fault")

// TestPStoreDirOf covers dirOf's three branches: a path with a directory, a path at
// the filesystem root, and a bare filename (the "." fallback that keeps the temp
// file beside a relative store).
func TestPStoreDirOf(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"/a/b/c.pstore", "/a/b"},
		{"/c.pstore", "/"},
		{"bare.pstore", "."},
		{"", "."},
	} {
		if got := dirOf(c.in); got != c.want {
			t.Errorf("dirOf(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestPStoreFileBackend covers the fileBackend Load/Store cycle and its error
// branches: a missing file loads as the empty table, a written file round-trips,
// and a Store into a non-existent directory (a CreateTemp failure) errors.
func TestPStoreFileBackend(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "b.pstore")
	b := &fileBackend{path: path}

	// Missing file -> empty load (nil, nil), the fresh-store case.
	if data, err := b.Load(); err != nil || data != nil {
		t.Fatalf("Load missing = %v, %v; want nil, nil", data, err)
	}

	// Store then Load round-trips the exact bytes.
	want := []byte("hello marshal bytes")
	if err := b.Store(want); err != nil {
		t.Fatalf("Store: %v", err)
	}
	got, err := b.Load()
	if err != nil || string(got) != string(want) {
		t.Fatalf("Load after Store = %q, %v; want %q", got, err, want)
	}

	// A Store whose temp-dir does not exist fails at CreateTemp.
	bad := &fileBackend{path: filepath.Join(dir, "no-such-dir", "x.pstore")}
	if err := bad.Store([]byte("x")); err == nil {
		t.Fatal("Store into missing dir: want error, got nil")
	}

	// A Load of an unreadable path (a directory, which ReadFile cannot read as a
	// file) surfaces the non-IsNotExist error branch.
	bd := &fileBackend{path: dir}
	if _, err := bd.Load(); err == nil {
		t.Fatal("Load of a directory: want error, got nil")
	}
}

// failTmp is a tmpFile whose Write and/or Close fail, injected via createTemp to
// reach Store's error branches (a real disk-full / fd-error cannot be forced
// portably).
type failTmp struct {
	name               string
	writeErr, closeErr error
}

func (f *failTmp) Write(b []byte) (int, error) {
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	return len(b), nil
}
func (f *failTmp) Close() error { return f.closeErr }
func (f *failTmp) Name() string { return f.name }

// TestPStoreStoreFaults covers fileBackend.Store's Write-error and Close-error
// branches via the createTemp seam, and the rename branch via the renameFile seam.
func TestPStoreStoreFaults(t *testing.T) {
	dir := t.TempDir()
	b := &fileBackend{path: filepath.Join(dir, "s.pstore")}

	// Write fails: Store returns the write error (and removes the temp file).
	restore := createTemp
	createTemp = func(string) (tmpFile, error) {
		return &failTmp{name: filepath.Join(dir, "tmp-w"), writeErr: errInjected}, nil
	}
	if err := b.Store([]byte("x")); err != errInjected {
		t.Fatalf("Store write-fail = %v, want errInjected", err)
	}

	// Close fails: Store returns the close error.
	createTemp = func(string) (tmpFile, error) {
		return &failTmp{name: filepath.Join(dir, "tmp-c"), closeErr: errInjected}, nil
	}
	if err := b.Store([]byte("x")); err != errInjected {
		t.Fatalf("Store close-fail = %v, want errInjected", err)
	}

	// createTemp itself fails: Store returns that error.
	createTemp = func(string) (tmpFile, error) { return nil, errInjected }
	if err := b.Store([]byte("x")); err != errInjected {
		t.Fatalf("Store createTemp-fail = %v, want errInjected", err)
	}
	createTemp = restore

	// rename fails: Store returns the rename error (a real temp file is written,
	// then the injected rename fails).
	restoreRename := renameFile
	renameFile = func(string, string) error { return errInjected }
	if err := b.Store([]byte("ok")); err != errInjected {
		t.Fatalf("Store rename-fail = %v, want errInjected", err)
	}
	renameFile = restoreRename
}

// TestPStoreFlockFile covers flockFile: a successful exclusive lock + unlock, a
// successful shared lock, the OpenFile error path for an unopenable path, and the
// Flock-failure branch via the flockSyscall seam.
func TestPStoreFlockFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lock.pstore")

	unlock, err := flockFile(path, false) // LOCK_EX
	if err != nil {
		t.Fatalf("flockFile EX: %v", err)
	}
	unlock()

	unlock, err = flockFile(path, true) // LOCK_SH
	if err != nil {
		t.Fatalf("flockFile SH: %v", err)
	}
	unlock()

	// An unopenable path (a missing parent directory) fails at OpenFile.
	if _, err := flockFile(filepath.Join(dir, "nope", "x"), false); err == nil {
		t.Fatal("flockFile missing dir: want error, got nil")
	}

	// flock itself failing (injected) closes the fd and surfaces the error.
	restore := flockSyscall
	flockSyscall = func(int, int) error { return errInjected }
	if _, err := flockFile(path, false); err != errInjected {
		t.Fatalf("flockFile flock-fail = %v, want errInjected", err)
	}
	flockSyscall = restore
}

// TestPStoreRaisePStore covers raisePStore's three branches: a nil error is a
// no-op, a *libpstore.Error and a plain os error both raise PStore::Error.
func TestPStoreRaisePStore(t *testing.T) {
	// nil -> no panic.
	raisePStore(nil)

	// A plain (non-PStore) error raises PStore::Error (the IO-error fallback).
	func() {
		defer func() {
			r := recover()
			re, ok := r.(RubyError)
			if !ok || re.Class != "PStore::Error" {
				t.Fatalf("raisePStore(os error): recovered %#v, want PStore::Error", r)
			}
		}()
		raisePStore(os.ErrPermission)
	}()
}

// TestPStoreValNil covers pstoreVal's nil branch (a nil marshal.Value becomes Ruby
// nil), which the in-transaction Get/Delete miss paths feed.
func TestPStoreValNil(t *testing.T) {
	if v := pstoreVal(nil); v != object.NilV {
		t.Fatalf("pstoreVal(nil) = %v, want nil", v)
	}
	// A non-nil value converts through the Marshal binding.
	if v := pstoreVal(marshal.Int{I: big.NewInt(1)}); v != object.Integer(1) {
		t.Fatalf("pstoreVal(1) = %v, want 1", v)
	}
}

// TestPStoreTxGuardNotInTransaction covers the txGuard accessors reached with a nil
// Tx: each must raise PStore::Error ("not in transaction"), the guard that lets an
// accessor called outside a transaction reproduce MRI's error.
func TestPStoreTxGuardNotInTransaction(t *testing.T) {
	g := &txGuard{tx: nil}
	key := marshal.Symbol("k")

	calls := []func(){
		func() { g.Get(key) },
		func() { g.Set(key, key) },
		func() { g.Delete(key) },
		func() { g.Fetch(key, nil) },
		func() { g.Roots() },
		func() { g.RootQ(key) },
	}
	for i, call := range calls {
		func() {
			defer func() {
				r := recover()
				re, ok := r.(RubyError)
				if !ok || re.Class != "PStore::Error" || re.Message != "not in transaction" {
					t.Fatalf("call %d: recovered %#v, want PStore::Error 'not in transaction'", i, r)
				}
			}()
			call()
		}()
	}
}
