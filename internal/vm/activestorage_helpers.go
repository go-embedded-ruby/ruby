// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"bytes"
	"io"

	activestorage "github.com/go-ruby-activestorage/activestorage"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// asDetRandom is the deterministic RandomSource the binding installs so a blob's
// storage key is reproducible across runs. It is a splitmix64 generator: each
// draw advances a 64-bit state and mixes it, giving a well-distributed byte
// stream (base36 key generation needs both RandomBytes and, for residues the
// alphabet cannot cover uniformly, RandomNumber). The err field is a
// fault-injection seam the suite uses to reach the key-generation error paths;
// production leaves it nil.
type asDetRandom struct {
	state uint64
	err   error
}

func (r *asDetRandom) next() uint64 {
	r.state += 0x9e3779b97f4a7c15
	z := r.state
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return z ^ (z >> 31)
}

// RandomBytes returns n deterministic bytes, or the injected error when set.
func (r *asDetRandom) RandomBytes(n int) ([]byte, error) {
	if r.err != nil {
		return nil, r.err
	}
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(r.next())
	}
	return b, nil
}

// RandomNumber returns a deterministic integer in [0, max).
func (r *asDetRandom) RandomNumber(max int) (int, error) {
	return int(r.next() % uint64(max)), nil
}

// asKwargs returns the trailing keyword Hash of a native call, or an empty Hash
// when none was passed.
func asKwargs(args []object.Value) *object.Hash {
	if len(args) > 0 {
		if h, ok := args[len(args)-1].(*object.Hash); ok {
			return h
		}
	}
	return object.NewHash()
}

// asKwGet looks up a keyword by name, accepting either a Symbol or a String key,
// and returns nil (NilV) when absent.
func asKwGet(h *object.Hash, name string) object.Value {
	if v, ok := h.Get(object.Symbol(name)); ok {
		return v
	}
	if v, ok := h.Get(object.NewString(name)); ok {
		return v
	}
	return object.NilV
}

// asKwString returns a keyword's value as a String, or "" when absent/nil.
func asKwString(h *object.Hash, name string) string {
	v := asKwGet(h, name)
	if object.IsNil(v) {
		return ""
	}
	return arStr(v)
}

// asKwInt returns a keyword's value as an int64, or 0 when absent/non-integer.
func asKwInt(h *object.Hash, name string) int64 {
	if n, ok := asKwGet(h, name).(object.Integer); ok {
		return int64(n)
	}
	return 0
}

// asPosStr reads the i-th positional argument as a String, or "" when absent.
func asPosStr(args []object.Value, i int) string {
	if i < len(args) {
		return arStr(args[i])
	}
	return ""
}

// asPosInt reads the i-th positional argument as an int64, or 0 when absent or
// non-integer.
func asPosInt(args []object.Value, i int) int64 {
	if i < len(args) {
		if n, ok := args[i].(object.Integer); ok {
			return int64(n)
		}
	}
	return 0
}

// argAt returns the i-th positional argument, or NilV when absent.
func argAt(args []object.Value, i int) object.Value {
	if i < len(args) {
		return args[i]
	}
	return object.NilV
}

// asReader turns a Ruby io: argument into an io.Reader: a String yields a reader
// over its bytes; anything else raises TypeError (matching the io-or-nothing
// contract of create_and_upload!'s io: keyword).
func asReader(v object.Value) io.Reader {
	if s, ok := v.(*object.String); ok {
		return bytes.NewReader(s.Bytes())
	}
	raise("TypeError", "io: must be a String, got %s", v.Inspect())
	return nil
}

// asBlobParams reads the shared blob keyword options (filename, content_type,
// key, service_name) into a BlobParams. byte_size / checksum are filled by the
// upload helpers, or read explicitly for a direct-upload blob.
func asBlobParams(h *object.Hash) activestorage.BlobParams {
	return activestorage.BlobParams{
		Filename:    asKwString(h, "filename"),
		ContentType: asKwString(h, "content_type"),
		Key:         asKwString(h, "key"),
		ServiceName: asKwString(h, "service_name"),
	}
}

// asURLOptions reads the url() keyword options (disposition, content_type,
// filename) into a service URLOptions.
func asURLOptions(h *object.Hash) activestorage.URLOptions {
	return activestorage.URLOptions{
		Disposition: asKwString(h, "disposition"),
		ContentType: asKwString(h, "content_type"),
		Filename:    activestorage.NewFilename(asKwString(h, "filename")),
	}
}

// asRecordRef reads the record_type / record_id keyword pair identifying an
// attachment's owner.
func asRecordRef(h *object.Hash) activestorage.RecordRef {
	return activestorage.RecordRef{Type: asKwString(h, "record_type"), ID: asKwInt(h, "record_id")}
}

// asAttachable maps a Ruby value into the attachable form the library resolves:
// an ASBlob to its *Blob, a Hash to an Upload (its io / filename / content_type),
// a String to a signed id, and anything else through unchanged so the library
// reports it as unattachable.
func asAttachable(v object.Value) any {
	switch t := v.(type) {
	case *ASBlob:
		return t.b
	case *object.Hash:
		return activestorage.Upload{
			Filename:    asKwString(t, "filename"),
			ContentType: asKwString(t, "content_type"),
			Reader:      asReader(asKwGet(t, "io")),
		}
	case *object.String:
		return t.Str()
	default:
		return v
	}
}

// asBlobValue wraps a library blob for Ruby, or nil when the blob is absent.
func asBlobValue(b *activestorage.Blob) object.Value {
	if b == nil {
		return object.NilV
	}
	return &ASBlob{b: b}
}

// asAttachmentValue wraps a library attachment for Ruby, or nil when absent.
func asAttachmentValue(a *activestorage.Attachment) object.Value {
	if a == nil {
		return object.NilV
	}
	return &ASAttachment{a: a}
}
