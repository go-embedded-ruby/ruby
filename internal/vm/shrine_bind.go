// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"bytes"
	"encoding/json"
	"io"

	shrine "github.com/go-ruby-shrine/shrine"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin binding between rbgo's Ruby object graph and the
// interpreter-independent file-attachment core of github.com/go-ruby-shrine/shrine
// (a pure-Go, no-cgo reimplementation of the deterministic core of Ruby's shrine
// gem). It carries the instance value types Shrine wraps — a Storage, an Uploader,
// an UploadedFile and an Attacher — plus the conversions that turn Ruby Strings /
// StringIOs into the io.Reader a Storage consumes, a metadata Hash into the
// library's Metadata, and the error bridge that re-raises the library's sentinels
// as the Shrine::Error Ruby class. All attachment logic is delegated to
// go-ruby-shrine. See shrine.go for the module and method wiring.
//
// Seam wiring:
//   - Storage IO ↔ Ruby String / StringIO: uploads read their bytes from a Ruby
//     String blob or a StringIO/IO (shrineReader); downloads return a binary Ruby
//     String; #open returns a StringIO over the stored bytes.
//   - FS seam → real filesystem: the FileSystem storage runs over shrine's default
//     OSFS (a real directory). Tests root it at a t.TempDir(); shrine's own FS-seam
//     injection (NewFileSystemWithFS) stays a library concern.
//   - GenerateLocation / DetectMIME defaults: shrine.New() installs the random-hex
//     id + net/http content-sniff seams; the binding keeps those defaults.
//
// Deferred: the gem's broad plugin system (Shrine.plugin :foo) mostly needs the
// interpreter and is left as an extension seam — only shrine's Plugin registration
// hook exists in the library, and no plugin surface is bound here yet.

// ShrineStorage is an instance of a Shrine::Storage backend (Memory or FileSystem),
// wrapping a go-ruby-shrine Storage. cls carries the concrete Ruby class so
// classOf / is_a? report Shrine::Storage::Memory or Shrine::Storage::FileSystem.
type ShrineStorage struct {
	cls  *RClass
	st   shrine.Storage
	kind string // "Memory" / "FileSystem", for #to_s / #inspect
}

func (s *ShrineStorage) ToS() string     { return "#<Shrine::Storage::" + s.kind + ">" }
func (s *ShrineStorage) Inspect() string { return s.ToS() }
func (s *ShrineStorage) Truthy() bool    { return true }

// ShrineUploader is an instance of the Shrine class bound to a storage key
// (Shrine.new(:store)): it wraps a go-ruby-shrine *Uploader and uploads to that one
// storage.
type ShrineUploader struct {
	cls *RClass
	u   *shrine.Uploader
}

func (u *ShrineUploader) ToS() string     { return "#<Shrine @storage_key=" + u.u.StorageKey() + ">" }
func (u *ShrineUploader) Inspect() string { return u.ToS() }
func (u *ShrineUploader) Truthy() bool    { return true }

// ShrineUploadedFile is a Shrine::UploadedFile value: the reference returned by an
// upload, wrapping a go-ruby-shrine *UploadedFile that knows how to reach its bytes
// through the storage registry it was created from.
type ShrineUploadedFile struct {
	cls *RClass
	f   *shrine.UploadedFile
}

func (f *ShrineUploadedFile) ToS() string     { return "#<Shrine::UploadedFile id=" + f.f.ID + ">" }
func (f *ShrineUploadedFile) Inspect() string { return f.ToS() }
func (f *ShrineUploadedFile) Truthy() bool    { return true }

// ShrineAttacher is a Shrine::Attacher: it wraps a go-ruby-shrine *Attacher and
// drives the cache→store attachment lifecycle.
type ShrineAttacher struct {
	cls *RClass
	a   *shrine.Attacher
}

func (a *ShrineAttacher) ToS() string     { return "#<Shrine::Attacher>" }
func (a *ShrineAttacher) Inspect() string { return a.ToS() }
func (a *ShrineAttacher) Truthy() bool    { return true }

// shrineReader coerces an upload source into an io.Reader: a Ruby String is read as
// its raw bytes (a blob), and a StringIO / IO / File (an *IOObj) as its buffered
// content from the current read cursor (advancing it, mirroring IO#read). Any other
// value raises TypeError, matching the gem's "io must respond to read" contract.
func shrineReader(v object.Value) io.Reader {
	switch x := v.(type) {
	case *object.String:
		return bytes.NewReader(x.Bytes())
	case *IOObj:
		x.pipeRefresh()
		pos := x.pos
		if pos > len(x.buf) {
			pos = len(x.buf)
		}
		data := append([]byte(nil), x.buf[pos:]...)
		x.pos = len(x.buf)
		return bytes.NewReader(data)
	default:
		raise("TypeError", "shrine: expected a String or an IO, got %s", classNameOf(v))
		return nil
	}
}

// shrineUploadOptions maps an upload/assign/replace keyword tail (location:,
// filename:, metadata:) to the library's *UploadOptions, returning nil when there
// is no trailing options Hash so the library applies its own defaults.
func shrineUploadOptions(rest []object.Value) *shrine.UploadOptions {
	h := shrineOptsHash(rest)
	if h == nil {
		return nil
	}
	o := &shrine.UploadOptions{}
	if v, ok := shrineHashGet(h, "location"); ok {
		o.Location = strArg(v)
	}
	if v, ok := shrineHashGet(h, "filename"); ok {
		o.Filename = strArg(v)
	}
	if v, ok := shrineHashGet(h, "metadata"); ok {
		if mh, ok := v.(*object.Hash); ok {
			o.Metadata = shrineMetadataFromHash(mh)
		}
	}
	return o
}

// shrineMetadataFromHash converts a Ruby metadata Hash to the library's Metadata,
// rendering each Symbol/String key as a bare string and each value through
// shrineRubyToGo so it round-trips through the UploadedFile JSON representation.
func shrineMetadataFromHash(h *object.Hash) shrine.Metadata {
	m := shrine.Metadata{}
	for _, k := range h.Keys {
		val, _ := h.Get(k)
		m[shrineName(k)] = shrineRubyToGo(val)
	}
	return m
}

// shrineRubyToGo lowers a Ruby value to the Go any a Metadata / Data entry holds: a
// String to string, an Integer to int64, a Float to float64, a Bool to bool, nil to
// nil, a nested Hash to a string-keyed map and an Array to a slice (so a Data() Hash
// round-trips back through Shrine.uploaded_file), and anything else to its Ruby #to_s.
func shrineRubyToGo(v object.Value) any {
	switch x := v.(type) {
	case *object.String:
		return x.Str()
	case object.Integer:
		return int64(x)
	case object.Float:
		return float64(x)
	case object.Bool:
		return bool(x)
	case *object.Hash:
		m := map[string]any{}
		for _, k := range x.Keys {
			val, _ := x.Get(k)
			m[shrineName(k)] = shrineRubyToGo(val)
		}
		return m
	case *object.Array:
		s := make([]any, len(x.Elems))
		for i, e := range x.Elems {
			s[i] = shrineRubyToGo(e)
		}
		return s
	}
	if object.IsNil(v) {
		return nil
	}
	return v.ToS()
}

// shrineGoToRuby lifts a Go value decoded from the library (a Metadata value or a
// Data() entry) into a Ruby object: strings, numbers, booleans, nil, nested maps
// (Ruby Hash with String keys) and slices (Ruby Array). JSON numbers arrive as
// float64; a whole-valued float becomes an Integer so a size reads as 5, not 5.0.
func shrineGoToRuby(v any) object.Value {
	switch x := v.(type) {
	case nil:
		return object.NilV
	case string:
		return object.NewString(x)
	case bool:
		return object.Bool(x)
	case int64:
		return object.IntValue(x)
	case int:
		return object.IntValue(int64(x))
	case float64:
		if x == float64(int64(x)) {
			return object.IntValue(int64(x))
		}
		return object.Float(x)
	case shrine.Metadata:
		return shrineMapToHash(map[string]any(x))
	case map[string]any:
		return shrineMapToHash(x)
	case []any:
		elems := make([]object.Value, len(x))
		for i, e := range x {
			elems[i] = shrineGoToRuby(e)
		}
		return object.NewArrayFromSlice(elems)
	}
	return object.NewString("")
}

// shrineMapToHash builds a Ruby Hash with String keys from a Go string-keyed map,
// recursing through shrineGoToRuby for each value.
func shrineMapToHash(m map[string]any) *object.Hash {
	h := object.NewHash()
	for k, v := range m {
		h.Set(object.NewString(k), shrineGoToRuby(v))
	}
	return h
}

// shrineURLOptions maps a #url options tail (a trailing keyword Hash) to the
// map[string]any the library's URL takes, or nil when none is given.
func shrineURLOptions(rest []object.Value) map[string]any {
	h := shrineOptsHash(rest)
	if h == nil {
		return nil
	}
	m := map[string]any{}
	for _, k := range h.Keys {
		v, _ := h.Get(k)
		m[shrineName(k)] = shrineRubyToGo(v)
	}
	return m
}

// shrineDataJSON turns the argument of Shrine.uploaded_file into the JSON string
// the library rehydrates from: a String is passed through verbatim, and a Hash is
// re-encoded through shrineRubyToGo so a symbol-keyed {id:, storage:, metadata:}
// Hash round-trips. Any other value raises TypeError.
func shrineDataJSON(v object.Value) string {
	switch x := v.(type) {
	case *object.String:
		return x.Str()
	case *object.Hash:
		m := map[string]any{}
		for _, k := range x.Keys {
			val, _ := x.Get(k)
			m[shrineName(k)] = shrineRubyToGo(val)
		}
		// m holds only JSON-safe scalars produced by shrineRubyToGo, so Marshal
		// cannot fail here.
		b, _ := json.Marshal(m)
		return string(b)
	default:
		raise("TypeError", "shrine: uploaded_file expects a String or Hash, got %s", classNameOf(v))
		return ""
	}
}

// shrineName renders a Symbol or String option/key as its bare string, falling back
// to the value's Ruby #to_s.
func shrineName(v object.Value) string {
	switch s := v.(type) {
	case object.Symbol:
		return string(s)
	case *object.String:
		return s.Str()
	}
	return v.ToS()
}

// shrineOptsHash returns the trailing keyword Hash of a Shrine call, or nil when the
// last argument is not a Hash.
func shrineOptsHash(rest []object.Value) *object.Hash {
	if len(rest) == 0 {
		return nil
	}
	h, ok := rest[len(rest)-1].(*object.Hash)
	if !ok {
		return nil
	}
	return h
}

// shrineHashGet fetches a symbol-keyed option from an options Hash, reporting
// ok=false when the Hash is absent or the key is missing.
func shrineHashGet(h *object.Hash, key string) (object.Value, bool) {
	if h == nil {
		return object.NilV, false
	}
	return h.Get(object.Symbol(key))
}

// raiseShrineError re-raises a go-ruby-shrine error as the Shrine::Error Ruby
// exception (the gem's single error base). It never returns (raise panics); it is
// typed to return any so a caller can write `return raiseShrineError(err)` in a
// value position.
func raiseShrineError(err error) any {
	return raise("Shrine::Error", "%s", err.Error())
}
