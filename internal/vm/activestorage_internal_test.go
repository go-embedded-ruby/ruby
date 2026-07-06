// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"
	"io"
	"testing"

	activestorage "github.com/go-ruby-activestorage/activestorage"

	"github.com/go-embedded-ruby/ruby/internal/compiler"
	"github.com/go-ruby-parser/parser"
)

// errAS is the sentinel the fault-injection seams below return; it is not one of
// the library's mapped sentinels, so it exercises the generic ActiveStorage::Error
// arm of asRaise.
var errAS = errors.New("activestorage: injected failure")

// asFailStore is a ModelStore whose every operation fails — the seam that reaches
// the store-error arms of the binding (the deterministic MemStore never errors).
type asFailStore struct{}

func (asFailStore) InsertBlob(*activestorage.Blob) error             { return errAS }
func (asFailStore) FindBlob(int64) (*activestorage.Blob, error)      { return nil, errAS }
func (asFailStore) UpdateBlob(*activestorage.Blob) error             { return errAS }
func (asFailStore) DeleteBlob(int64) error                           { return errAS }
func (asFailStore) InsertAttachment(*activestorage.Attachment) error { return errAS }
func (asFailStore) FindAttachments(string, int64, string) ([]*activestorage.Attachment, error) {
	return nil, errAS
}
func (asFailStore) DeleteAttachment(int64) error { return errAS }

// asFailService is a Service whose every operation fails — the seam that reaches
// the backend-error arms the disk service cannot provoke portably (exist?, size,
// delete, url).
type asFailService struct{}

func (asFailService) Name() string                                         { return "fail" }
func (asFailService) Upload(string, io.Reader, string) error               { return errAS }
func (asFailService) Download(string) (io.ReadCloser, error)               { return nil, errAS }
func (asFailService) DownloadChunk(string, int64, int64) ([]byte, error)   { return nil, errAS }
func (asFailService) Delete(string) error                                  { return errAS }
func (asFailService) Exist(string) (bool, error)                           { return false, errAS }
func (asFailService) Url(string, activestorage.URLOptions) (string, error) { return "", errAS }
func (asFailService) Size(string) (int64, error)                           { return 0, errAS }

// asFailSigner is a Signer whose sign/verify fail — the seam that reaches the
// signing-error arm (the default HMAC signer never errors on Sign).
type asFailSigner struct{}

func (asFailSigner) Sign(string, string) (string, error)   { return "", errAS }
func (asFailSigner) Verify(string, string) (string, error) { return "", errAS }

// asRun runs src on v, failing the test on any runtime error.
func asRun(t *testing.T, v *VM, src string) {
	t.Helper()
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	iseq, err := compiler.Compile(prog)
	if err != nil {
		t.Fatalf("compile %q: %v", src, err)
	}
	if _, err := v.Run(iseq); err != nil {
		t.Fatalf("run %q: %v", src, err)
	}
}

// asRunErr runs src on v expecting a runtime RubyError and returns its class.
func asRunErr(t *testing.T, v *VM, src string) string {
	t.Helper()
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	iseq, err := compiler.Compile(prog)
	if err != nil {
		t.Fatalf("compile %q: %v", src, err)
	}
	_, rerr := v.Run(iseq)
	re, ok := rerr.(RubyError)
	if !ok {
		t.Fatalf("src=%q: expected a RubyError, got %#v", src, rerr)
	}
	return re.Class
}

// TestASDetRandom covers the deterministic RandomSource directly: a normal draw
// (RandomBytes / RandomNumber / next) and the injected-error arm of RandomBytes,
// which no create path can provoke through the default (never-erroring) source.
func TestActiveStorageDetRandom(t *testing.T) {
	r := &asDetRandom{state: 1}
	b, err := r.RandomBytes(4)
	if err != nil || len(b) != 4 {
		t.Fatalf("RandomBytes got b=%v err=%v", b, err)
	}
	n, err := r.RandomNumber(36)
	if err != nil || n < 0 || n >= 36 {
		t.Fatalf("RandomNumber got n=%d err=%v", n, err)
	}
	if _, err := (&asDetRandom{err: errAS}).RandomBytes(1); err == nil {
		t.Fatalf("RandomBytes with injected error: want error, got nil")
	}
}

// TestASMkdirTempError covers the temp-directory failure arm of asRequireConfig.
func TestActiveStorageMkdirTempError(t *testing.T) {
	old := asMkdirTemp
	defer func() { asMkdirTemp = old }()
	asMkdirTemp = func(string, string) (string, error) { return "", errAS }

	v := New(io.Discard)
	if class := asRunErr(t, v, `ActiveStorage::Blob.service`); class != "ActiveStorage::Error" {
		t.Errorf("mkdir-temp failure: got class=%q want ActiveStorage::Error", class)
	}
}

// TestASRandomKeyError covers the key-generation failure arm of
// create_before_direct_upload! (the only create path with no upload step, so its
// error is reachable solely through a failing RandomSource).
func TestActiveStorageRandomKeyError(t *testing.T) {
	v := New(io.Discard)
	v.asRequireConfig().Random = &asDetRandom{err: errAS}
	src := `ActiveStorage::Blob.create_before_direct_upload!(filename: "a.txt", byte_size: 1, checksum: "c")`
	if class := asRunErr(t, v, src); class != "ActiveStorage::Error" {
		t.Errorf("random key failure: got class=%q want ActiveStorage::Error", class)
	}
}

// TestASServiceRegistryError covers Blob.service when no default service is
// registered (an empty registry).
func TestActiveStorageServiceRegistryError(t *testing.T) {
	v := New(io.Discard)
	v.asRequireConfig().Services = activestorage.NewRegistry()
	if class := asRunErr(t, v, `ActiveStorage::Blob.service`); class != "ActiveStorage::Error" {
		t.Errorf("empty registry: got class=%q want ActiveStorage::Error", class)
	}
}

// TestASSignerError covers Blob#signed_id when the signer fails.
func TestActiveStorageSignerError(t *testing.T) {
	v := New(io.Discard)
	v.asRequireConfig().Signer = asFailSigner{}
	src := `ActiveStorage::Blob.build_after_upload(io: "x", filename: "a.txt").signed_id`
	if class := asRunErr(t, v, src); class != "ActiveStorage::Error" {
		t.Errorf("signer failure: got class=%q want ActiveStorage::Error", class)
	}
}

// TestASServiceBackendErrors covers the service-backend error arms (exist?, size,
// delete, url) via a failing Service.
func TestActiveStorageServiceBackendErrors(t *testing.T) {
	for _, op := range []string{
		`ActiveStorage::Blob.service.exist?("k")`,
		`ActiveStorage::Blob.service.size("k")`,
		`ActiveStorage::Blob.service.delete("k")`,
		`ActiveStorage::Blob.service.url("k")`,
	} {
		v := New(io.Discard)
		v.asRequireConfig().Services = activestorage.NewRegistry().Register(asFailService{})
		if class := asRunErr(t, v, op); class != "ActiveStorage::Error" {
			t.Errorf("op=%q: got class=%q want ActiveStorage::Error", op, class)
		}
	}
}

// TestASProxyStoreErrors covers the store-error arms of the has_one_attached /
// has_many_attached proxies via a failing ModelStore.
func TestActiveStorageProxyStoreErrors(t *testing.T) {
	ops := []string{
		`ActiveStorage::Attached::One.new(record_type: "U", record_id: 1, name: "a").attached?`,
		`ActiveStorage::Attached::One.new(record_type: "U", record_id: 1, name: "a").attachment`,
		`ActiveStorage::Attached::One.new(record_type: "U", record_id: 1, name: "a").blob`,
		`ActiveStorage::Attached::One.new(record_type: "U", record_id: 1, name: "a").detach`,
		`ActiveStorage::Attached::One.new(record_type: "U", record_id: 1, name: "a").purge`,
		`ActiveStorage::Attached::Many.new(record_type: "U", record_id: 1, name: "a").attachments`,
		`ActiveStorage::Attached::Many.new(record_type: "U", record_id: 1, name: "a").attached?`,
		`ActiveStorage::Attached::Many.new(record_type: "U", record_id: 1, name: "a").blobs`,
		`ActiveStorage::Attached::Many.new(record_type: "U", record_id: 1, name: "a").detach`,
		`ActiveStorage::Attached::Many.new(record_type: "U", record_id: 1, name: "a").purge`,
	}
	for _, op := range ops {
		v := New(io.Discard)
		v.asRequireConfig().Store = asFailStore{}
		if class := asRunErr(t, v, op); class != "ActiveStorage::Error" {
			t.Errorf("op=%q: got class=%q want ActiveStorage::Error", op, class)
		}
	}
}

// TestASAttachmentPurgeError covers Attachment#purge when the join-record delete
// fails: an attachment is first created against the real store, then the store is
// swapped for a failing one before purging.
func TestActiveStorageAttachmentPurgeError(t *testing.T) {
	v := New(io.Discard)
	v.asRequireConfig()
	asRun(t, v, `$one = ActiveStorage::Attached::One.new(record_type: "U", record_id: 1, name: "a")
$att = $one.attach(ActiveStorage::Blob.create_and_upload!(io: "x", filename: "a.txt"))`)
	v.asConfig.Store = asFailStore{}
	if class := asRunErr(t, v, `$att.purge`); class != "ActiveStorage::Error" {
		t.Errorf("attachment purge failure: got class=%q want ActiveStorage::Error", class)
	}
}
