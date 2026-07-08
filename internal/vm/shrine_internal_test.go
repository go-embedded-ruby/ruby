// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	shrine "github.com/go-ruby-shrine/shrine"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// shrineClasses returns the registered Shrine class tree on a fresh VM for the
// direct (Go-level) tests.
func shrineClasses(vm *VM) (sh, uf, att, mem, fs *RClass) {
	sh = vm.consts["Shrine"].(*RClass)
	uf = sh.consts["UploadedFile"].(*RClass)
	att = sh.consts["Attacher"].(*RClass)
	st := sh.consts["Storage"].(*RClass)
	mem = st.consts["Memory"].(*RClass)
	fs = st.consts["FileSystem"].(*RClass)
	return
}

// mustRaiseShrine runs fn and asserts it raises the RubyError class want.
func mustRaiseShrine(t *testing.T, want string, fn func()) {
	t.Helper()
	defer func() {
		re, ok := recover().(RubyError)
		if !ok {
			t.Fatalf("expected a RubyError (%s), got %v", want, recover())
		}
		if re.Class != want {
			t.Errorf("raised %q, want %q", re.Class, want)
		}
	}()
	fn()
}

// errBoom is the sentinel the fake storages return to exercise the error arms.
var errBoom = errors.New("boom")

// fakeStorage is a shrine.Storage whose per-operation errors are configurable, so a
// test can drive any single failure arm without a real backend.
type fakeStorage struct {
	uploadErr, openErr, deleteErr error
}

func (f fakeStorage) Upload(io.Reader, string, map[string]any) error { return f.uploadErr }
func (f fakeStorage) Open(string) (io.ReadCloser, error) {
	if f.openErr != nil {
		return nil, f.openErr
	}
	return io.NopCloser(strings.NewReader("")), nil
}
func (f fakeStorage) Exists(string) bool                { return false }
func (f fakeStorage) Delete(string) error               { return f.deleteErr }
func (f fakeStorage) URL(string, map[string]any) string { return "fake://x" }

// badReader errors on the first Read, exercising the io.ReadAll error arm.
type badReader struct{}

func (badReader) Read([]byte) (int, error) { return 0, errBoom }

// readErrStorage opens successfully but yields a reader that fails mid-drain, so a
// Storage#open reaches shrineStringIO's ReadAll error arm.
type readErrStorage struct{}

func (readErrStorage) Upload(io.Reader, string, map[string]any) error { return nil }
func (readErrStorage) Open(string) (io.ReadCloser, error) {
	return io.NopCloser(badReader{}), nil
}
func (readErrStorage) Exists(string) bool                { return false }
func (readErrStorage) Delete(string) error               { return nil }
func (readErrStorage) URL(string, map[string]any) string { return "" }

// TestShrineMemoryRoundTrip drives the headline flow through rbgo over in-memory
// storages: assign the registry, upload a StringIO, and read every UploadedFile
// accessor back, then rehydrate the same file from its JSON and download it.
func TestShrineMemoryRoundTrip(t *testing.T) {
	src := `
require "shrine"
Shrine.storages = { cache: Shrine::Storage::Memory.new, store: Shrine::Storage::Memory.new }
up = Shrine.new(:store)
f = up.upload(StringIO.new("hello world"), filename: "a.txt")
r = []
r << (f.id.end_with?(".txt") ? "id" : "no-id")
r << f.storage
r << f.filename
r << f.mime_type.split(";").first
r << f.size.to_s
r << (f.url.start_with?("memory://") ? "url" : "no-url")
r << f.metadata["size"].to_s
r << (f.exists? ? "exists" : "gone")
r << up.storage_key
g = Shrine.uploaded_file(f.to_json)
r << g.download
h = Shrine.uploaded_file(f.data)
r << h.download
r << (f.open.read == "hello world" ? "open" : "no-open")
puts r.join("|")
`
	want := "id|store|a.txt|text/plain|11|url|11|exists|store|hello world|hello world|open"
	if got := runSrc(t, src); got != want {
		t.Fatalf("memory round-trip =\n %q\nwant\n %q", got, want)
	}
}

// TestShrineFileSystemStorage drives a filesystem-rooted store: an upload writes to
// disk under a t.TempDir(), the file is downloadable and exists, and #delete removes
// it. Also exercises the low-level Storage#upload/#open/#exists?/#url/#delete surface.
func TestShrineFileSystemStorage(t *testing.T) {
	dir := t.TempDir()
	src := `
require "shrine"
Shrine.storages = { cache: Shrine::Storage::Memory.new, store: Shrine::Storage::FileSystem.new(` + rubyStr(dir) + `) }
up = Shrine.new(:store)
f = up.upload("raw bytes", filename: "b.dat")
r = []
r << (f.exists? ? "exists" : "gone")
r << f.download
r << (f.url.include?(f.id) ? "url" : "no-url")
f.delete
r << (f.exists? ? "still" : "deleted")

st = Shrine::Storage::Memory.new
st.upload(StringIO.new("direct"), "k1")
r << (st.exists?("k1") ? "up" : "no-up")
r << st.open("k1").read
r << (st.url("k1").start_with?("memory://") ? "surl" : "no-surl")
st.delete("k1")
r << (st.exists?("k1") ? "s-still" : "s-deleted")
puts r.join("|")
`
	want := "exists|raw bytes|url|deleted|up|direct|surl|s-deleted"
	if got := runSrc(t, src); got != want {
		t.Fatalf("filesystem storage =\n %q\nwant\n %q", got, want)
	}
}

// TestShrineAttacherLifecycle drives the cache→store lifecycle: assign caches a file
// (changed?), finalize promotes it to the store, get reads it back, destroy removes
// it. Also covers set/get and the fresh-attacher nil get.
func TestShrineAttacherLifecycle(t *testing.T) {
	src := `
require "shrine"
Shrine.storages = { cache: Shrine::Storage::Memory.new, store: Shrine::Storage::Memory.new }
att = Shrine::Attacher.new
r = []
r << (att.get.nil? ? "empty" : "full")
r << (att.changed? ? "changed" : "clean")
att.assign(StringIO.new("payload"), filename: "c.txt")
r << (att.changed? ? "changed" : "clean")
r << att.get.storage
att.finalize
stored = att.get
r << stored.storage
r << stored.download

att2 = Shrine::Attacher.new(cache: :cache, store: :store)
att2.set(stored)
r << att2.get.download
att2.promote rescue nil
att.destroy
r << (stored.exists? ? "still" : "destroyed")
puts r.join("|")
`
	want := "empty|clean|changed|cache|store|payload|payload|destroyed"
	if got := runSrc(t, src); got != want {
		t.Fatalf("attacher lifecycle =\n %q\nwant\n %q", got, want)
	}
}

// TestShrineReplaceAndStorages covers UploadedFile#replace (a new file, old deleted),
// the Shrine.storages reader, and metadata/url options carrying assorted value types
// (exercising the Ruby→Go metadata conversion and the symbol-key path).
func TestShrineReplaceAndStorages(t *testing.T) {
	src := `
require "shrine"
Shrine.storages = { cache: Shrine::Storage::Memory.new, store: Shrine::Storage::Memory.new }
up = Shrine.new(:store)
f = up.upload("v1", filename: "d.txt", metadata: { "tag" => "x", "n" => 3, "ratio" => 1.5, "flag" => true, "none" => nil })
r = []
r << f.metadata["tag"]
r << f.metadata["n"].to_s
r << f.metadata["ratio"].to_s
r << f.metadata["flag"].to_s
g = f.replace("v2")
r << g.download
r << (f.exists? ? "old-kept" : "old-gone")
r << f.url(expires: 60).class.to_s
s = Shrine.storages
r << (s.key?(:cache) && s.key?(:store) ? "both" : "miss")
r << s[:store].class.name
puts r.join("|")
`
	want := "x|3|1.5|true|v2|old-gone|String|both|Shrine::Storage::Memory"
	if got := runSrc(t, src); got != want {
		t.Fatalf("replace/storages =\n %q\nwant\n %q", got, want)
	}
}

// TestShrineRubyErrors covers the error arms reachable from Ruby: an unknown storage
// (Shrine.new / Attacher.new), invalid rehydration JSON, a download of a missing
// file, and the type guard on a non-IO upload argument.
func TestShrineRubyErrors(t *testing.T) {
	src := `
require "shrine"
Shrine.storages = { cache: Shrine::Storage::Memory.new, store: Shrine::Storage::Memory.new }
r = []
begin; Shrine.new(:nope); rescue Shrine::Error; r << "new"; end
begin; Shrine::Attacher.new(store: :nope); rescue Shrine::Error; r << "att"; end
begin; Shrine.uploaded_file("not json"); rescue Shrine::Error; r << "json"; end
begin; Shrine.uploaded_file(42); rescue TypeError; r << "uf-type"; end
begin; Shrine.new(:store).upload(42); rescue TypeError; r << "up-type"; end
miss = Shrine.uploaded_file({ "id" => "ghost.txt", "storage" => "store", "metadata" => {} })
begin; miss.download; rescue Shrine::Error; r << "dl"; end
r << (Shrine::Error.ancestors.include?(StandardError) ? "std" : "no-std")
puts r.join("|")
`
	want := "new|att|json|uf-type|up-type|dl|std"
	if got := runSrc(t, src); got != want {
		t.Fatalf("ruby errors =\n %q\nwant\n %q", got, want)
	}
}

// newFakeUploadedFile rehydrates an UploadedFile bound to a ctx whose "store" is fs,
// so its byte operations hit the fake's error arms.
func newFakeUploadedFile(t *testing.T, fs shrine.Storage) *shrine.UploadedFile {
	t.Helper()
	ctx := shrine.New()
	ctx.Register("store", fs)
	f, err := ctx.UploadedFile(`{"id":"x","storage":"store","metadata":{}}`)
	if err != nil {
		t.Fatalf("rehydrate: %v", err)
	}
	return f
}

// TestShrineStorageErrorArms drives every "err != nil → raiseShrineError" arm that a
// real Memory/FileSystem backend never reaches, by invoking the bound native methods
// directly against fake storages that fail one operation each.
func TestShrineStorageErrorArms(t *testing.T) {
	vm := New(&bytes.Buffer{})
	sh, uf, att, mem, _ := shrineClasses(vm)
	call := func(cls *RClass, name string, recv object.Value, args ...object.Value) {
		cls.methods[name].native(vm, recv, args, nil)
	}
	sarg := object.NewString("data")

	// Uploader#upload → storage Upload fails.
	uctx := shrine.New()
	uctx.Register("store", fakeStorage{uploadErr: errBoom})
	u, _ := uctx.Uploader("store")
	mustRaiseShrine(t, "Shrine::Error", func() {
		call(sh, "upload", &ShrineUploader{cls: sh, u: u}, sarg)
	})

	// UploadedFile#download / #open → storage Open fails.
	fOpen := &ShrineUploadedFile{cls: uf, f: newFakeUploadedFile(t, fakeStorage{openErr: errBoom})}
	mustRaiseShrine(t, "Shrine::Error", func() { call(uf, "download", fOpen) })
	mustRaiseShrine(t, "Shrine::Error", func() { call(uf, "open", fOpen) })

	// UploadedFile#delete → storage Delete fails.
	fDel := &ShrineUploadedFile{cls: uf, f: newFakeUploadedFile(t, fakeStorage{deleteErr: errBoom})}
	mustRaiseShrine(t, "Shrine::Error", func() { call(uf, "delete", fDel) })

	// UploadedFile#replace → storage Upload fails.
	fRep := &ShrineUploadedFile{cls: uf, f: newFakeUploadedFile(t, fakeStorage{uploadErr: errBoom})}
	mustRaiseShrine(t, "Shrine::Error", func() { call(uf, "replace", fRep, sarg) })

	// UploadedFile#to_json → json.Marshal fails on an unmarshalable metadata value.
	bad := &ShrineUploadedFile{cls: uf, f: &shrine.UploadedFile{ID: "x", StorageKey: "store", Metadata: shrine.Metadata{"c": make(chan int)}}}
	mustRaiseShrine(t, "Shrine::Error", func() { call(uf, "to_json", bad) })

	// Storage#upload / #open / #delete direct arms.
	mustRaiseShrine(t, "Shrine::Error", func() {
		call(mem, "upload", &ShrineStorage{cls: mem, st: fakeStorage{uploadErr: errBoom}, kind: "Memory"}, sarg, object.NewString("id"))
	})
	mustRaiseShrine(t, "Shrine::Error", func() {
		call(mem, "open", &ShrineStorage{cls: mem, st: fakeStorage{openErr: errBoom}, kind: "Memory"}, object.NewString("id"))
	})
	// Open succeeds but the returned reader fails mid-drain (shrineStringIO arm).
	mustRaiseShrine(t, "Shrine::Error", func() {
		call(mem, "open", &ShrineStorage{cls: mem, st: readErrStorage{}, kind: "Memory"}, object.NewString("id"))
	})
	mustRaiseShrine(t, "Shrine::Error", func() {
		call(mem, "delete", &ShrineStorage{cls: mem, st: fakeStorage{deleteErr: errBoom}, kind: "Memory"}, object.NewString("id"))
	})

	// Attacher#assign → cache Upload fails.
	aCtx := shrine.New()
	aCtx.Register("cache", fakeStorage{uploadErr: errBoom})
	aCtx.Register("store", shrine.NewMemory())
	aAssign, _ := aCtx.Attacher("cache", "store")
	mustRaiseShrine(t, "Shrine::Error", func() {
		call(att, "assign", &ShrineAttacher{cls: att, a: aAssign}, sarg)
	})

	// Attacher#promote / #finalize → store Upload fails after a cached assign.
	for _, name := range []string{"promote", "finalize"} {
		pCtx := shrine.New()
		pCtx.Register("cache", shrine.NewMemory())
		pCtx.Register("store", fakeStorage{uploadErr: errBoom})
		a, _ := pCtx.Attacher("cache", "store")
		if err := a.Assign(strings.NewReader("x"), nil); err != nil {
			t.Fatalf("assign: %v", err)
		}
		recv := &ShrineAttacher{cls: att, a: a}
		mustRaiseShrine(t, "Shrine::Error", func() { call(att, name, recv) })
	}

	// Attacher#destroy → the attached (cached) file's Delete fails.
	dCtx := shrine.New()
	dCtx.Register("cache", fakeStorage{deleteErr: errBoom})
	dCtx.Register("store", shrine.NewMemory())
	aDestroy, _ := dCtx.Attacher("cache", "store")
	if err := aDestroy.Assign(strings.NewReader("x"), nil); err != nil {
		t.Fatalf("assign: %v", err)
	}
	mustRaiseShrine(t, "Shrine::Error", func() {
		call(att, "destroy", &ShrineAttacher{cls: att, a: aDestroy})
	})
}

// TestShrineArgumentGuards covers the arity / type guards on every bound method by
// invoking the native functions directly with the wrong shape.
func TestShrineArgumentGuards(t *testing.T) {
	vm := New(&bytes.Buffer{})
	sh, uf, att, mem, fs := shrineClasses(vm)
	scall := func(cls *RClass, name string, args ...object.Value) {
		cls.smethods[name].native(vm, cls, args, nil)
	}
	mcall := func(cls *RClass, name string, recv object.Value, args ...object.Value) {
		cls.methods[name].native(vm, recv, args, nil)
	}

	// Class-method arity / type guards.
	mustRaiseShrine(t, "ArgumentError", func() { scall(fs, "new") })
	mustRaiseShrine(t, "ArgumentError", func() { scall(sh, "new") })
	mustRaiseShrine(t, "ArgumentError", func() { scall(sh, "uploaded_file") })
	mustRaiseShrine(t, "ArgumentError", func() { scall(sh, "storages=") })
	mustRaiseShrine(t, "TypeError", func() { scall(sh, "storages=", object.NewString("x")) })
	badHash := object.NewHash()
	badHash.Set(object.Symbol("store"), object.NewString("not-a-storage"))
	mustRaiseShrine(t, "TypeError", func() { scall(sh, "storages=", badHash) })

	// Instance-method guards.
	up := &ShrineUploader{cls: sh, u: mustUploader(t)}
	mustRaiseShrine(t, "ArgumentError", func() { mcall(sh, "upload", up) })
	file := &ShrineUploadedFile{cls: uf, f: newFakeUploadedFile(t, shrine.NewMemory())}
	mustRaiseShrine(t, "ArgumentError", func() { mcall(uf, "replace", file) })
	attacher := &ShrineAttacher{cls: att, a: mustAttacher(t)}
	mustRaiseShrine(t, "ArgumentError", func() { mcall(att, "assign", attacher) })
	mustRaiseShrine(t, "ArgumentError", func() { mcall(att, "set", attacher) })
	mustRaiseShrine(t, "TypeError", func() { mcall(att, "set", attacher, object.NewString("x")) })

	store := &ShrineStorage{cls: mem, st: shrine.NewMemory(), kind: "Memory"}
	mustRaiseShrine(t, "ArgumentError", func() { mcall(mem, "upload", store) })
	mustRaiseShrine(t, "ArgumentError", func() { mcall(mem, "upload", store, object.NewString("only-one")) })
	for _, name := range []string{"open", "exists?", "delete", "url"} {
		mustRaiseShrine(t, "ArgumentError", func() { mcall(mem, name, store) })
	}
}

func mustUploader(t *testing.T) *shrine.Uploader {
	t.Helper()
	ctx := shrine.New()
	ctx.Register("store", shrine.NewMemory())
	u, err := ctx.Uploader("store")
	if err != nil {
		t.Fatalf("uploader: %v", err)
	}
	return u
}

func mustAttacher(t *testing.T) *shrine.Attacher {
	t.Helper()
	ctx := shrine.New()
	ctx.Register("cache", shrine.NewMemory())
	ctx.Register("store", shrine.NewMemory())
	a, err := ctx.Attacher("cache", "store")
	if err != nil {
		t.Fatalf("attacher: %v", err)
	}
	return a
}

// TestShrineConversions covers the pure Ruby↔Go conversion helpers directly,
// including the branches unreachable from the happy-path flow.
func TestShrineConversions(t *testing.T) {
	// shrineReader: the clamp branch (pos past the buffer) and the type guard.
	if r := shrineReader(&IOObj{isStr: true, buf: []byte("hi"), pos: 99}); readAll(t, r) != "" {
		t.Error("clamped reader should be empty")
	}
	if r := shrineReader(&IOObj{isStr: true, buf: []byte("body"), pos: 1}); readAll(t, r) != "ody" {
		t.Error("reader should read from the cursor")
	}
	mustRaiseShrine(t, "TypeError", func() { shrineReader(object.IntValue(7)) })

	// shrineRubyToGo: the nested-Hash, Array, nil and #to_s-fallback branches (the
	// scalar branches are covered by the metadata round-trip flow).
	rh := object.NewHash()
	rh.Set(object.NewString("k"), object.IntValue(2))
	if m, ok := shrineRubyToGo(rh).(map[string]any); !ok || m["k"] != int64(2) {
		t.Errorf("shrineRubyToGo(Hash) = %#v", shrineRubyToGo(rh))
	}
	if s, ok := shrineRubyToGo(object.NewArray(object.IntValue(1), object.NewString("a"))).([]any); !ok || len(s) != 2 || s[0] != int64(1) {
		t.Errorf("shrineRubyToGo(Array) = %#v", shrineRubyToGo(object.NewArray()))
	}
	if shrineRubyToGo(object.NilV) != nil {
		t.Error("shrineRubyToGo(nil) should be nil")
	}
	if shrineRubyToGo(object.Symbol("z")) != "z" {
		t.Errorf("shrineRubyToGo(Symbol) = %v", shrineRubyToGo(object.Symbol("z")))
	}

	// shrineName: symbol, string and the #to_s fallback.
	if shrineName(object.Symbol("s")) != "s" || shrineName(object.NewString("t")) != "t" || shrineName(object.IntValue(7)) != "7" {
		t.Error("shrineName mismatch")
	}

	// shrineDataJSON: the non-String/Hash type guard.
	mustRaiseShrine(t, "TypeError", func() { shrineDataJSON(object.IntValue(1)) })

	// shrineHashGet on a nil Hash.
	if _, ok := shrineHashGet(nil, "x"); ok {
		t.Error("nil hash should miss")
	}

	// shrineOptsHash: no args, and a trailing non-Hash.
	if shrineOptsHash(nil) != nil || shrineOptsHash([]object.Value{object.IntValue(1)}) != nil {
		t.Error("shrineOptsHash should be nil")
	}

	// shrineUploadOptions: a metadata: value that is not a Hash is ignored.
	h := object.NewHash()
	h.Set(object.Symbol("location"), object.NewString("loc"))
	h.Set(object.Symbol("filename"), object.NewString("fn"))
	h.Set(object.Symbol("metadata"), object.NewString("not-a-hash"))
	opts := shrineUploadOptions([]object.Value{h})
	if opts.Location != "loc" || opts.Filename != "fn" || opts.Metadata != nil {
		t.Errorf("upload options = %+v", opts)
	}

	// shrineGoToRuby: every scalar/container branch.
	cases := []struct {
		in   any
		want string
	}{
		{nil, "nil"},
		{"s", `"s"`},
		{true, "true"},
		{int64(5), "5"},
		{int(6), "6"},
		{float64(7), "7"},
		{float64(1.5), "1.5"},
		{shrine.Metadata{"k": "v"}, `{"k" => "v"}`},
		{map[string]any{"k": int64(2)}, `{"k" => 2}`},
		{[]any{int64(1), "a"}, `[1, "a"]`},
		{struct{}{}, `""`},
	}
	for _, c := range cases {
		if got := shrineGoToRuby(c.in).Inspect(); got != c.want {
			t.Errorf("shrineGoToRuby(%v) = %q want %q", c.in, got, c.want)
		}
	}
}

// readAll drains an io.Reader to a string for the reader tests.
func readAll(t *testing.T, r io.Reader) string {
	t.Helper()
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return string(b)
}

// TestShrineValueMethods covers the Go-level object.Value surface (ToS / Inspect /
// Truthy) and classOf dispatch of every wrapper type.
func TestShrineValueMethods(t *testing.T) {
	vm := New(&bytes.Buffer{})
	sh, uf, att, mem, fs := shrineClasses(vm)

	memStore := &ShrineStorage{cls: mem, st: shrine.NewMemory(), kind: "Memory"}
	fsStore := &ShrineStorage{cls: fs, st: shrine.NewFileSystem(t.TempDir()), kind: "FileSystem"}
	uploader := &ShrineUploader{cls: sh, u: mustUploader(t)}
	file := &ShrineUploadedFile{cls: uf, f: &shrine.UploadedFile{ID: "abc.txt"}}
	attacher := &ShrineAttacher{cls: att, a: mustAttacher(t)}

	checks := []struct {
		v   object.Value
		s   string
		cls *RClass
	}{
		{memStore, "#<Shrine::Storage::Memory>", mem},
		{fsStore, "#<Shrine::Storage::FileSystem>", fs},
		{uploader, "#<Shrine @storage_key=store>", sh},
		{file, "#<Shrine::UploadedFile id=abc.txt>", uf},
		{attacher, "#<Shrine::Attacher>", att},
	}
	for _, c := range checks {
		if !c.v.Truthy() {
			t.Errorf("%T should be truthy", c.v)
		}
		if c.v.ToS() != c.s || c.v.Inspect() != c.s {
			t.Errorf("%T render = %q / %q want %q", c.v, c.v.ToS(), c.v.Inspect(), c.s)
		}
		if got := vm.classOf(c.v); got != c.cls {
			t.Errorf("classOf(%T) = %v want %v", c.v, got, c.cls)
		}
	}
}

// TestShrineFeatureProvided proves require "shrine" reports a first-load true then a
// second-load false, matching a normal gem load.
func TestShrineFeatureProvided(t *testing.T) {
	src := `
r = []
r << require("shrine").to_s
r << require("shrine").to_s
puts r.join("|")
`
	if got := runSrc(t, src); got != "true|false" {
		t.Fatalf("require shrine = %q", got)
	}
}
