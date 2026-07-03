// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	mimetypes "github.com/go-ruby-mime-types/mime-types"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin binding between rbgo's Ruby object graph and the
// interpreter-independent github.com/go-ruby-mime-types/mime-types registry. The
// IANA data and lookup behaviour live in that library; rbgo only maps a Ruby
// query String to a registry call and each *mimetypes.Type result to a MIME::Type
// value object, so the mime-types-gem-faithful behaviour the MIME::Types module
// relies on is preserved by construction.

// mimeTypeVal is the accessor view a MIME::Type shell delegates to, wrapping a
// *mimetypes.Type so mimetypes.go never imports the library type directly.
type mimeTypeVal struct{ t *mimetypes.Type }

func (m mimeTypeVal) ContentType() string        { return m.t.ContentType() }
func (m mimeTypeVal) MediaType() string          { return m.t.MediaType() }
func (m mimeTypeVal) SubType() string            { return m.t.SubType() }
func (m mimeTypeVal) Simplified() string         { return m.t.Simplified() }
func (m mimeTypeVal) Friendly() string           { return m.t.Friendly() }
func (m mimeTypeVal) Encoding() string           { return m.t.Encoding() }
func (m mimeTypeVal) Extensions() []string       { return m.t.Extensions() }
func (m mimeTypeVal) PreferredExtension() string { return m.t.PreferredExtension() }
func (m mimeTypeVal) Binary() bool               { return m.t.Binary() }
func (m mimeTypeVal) ASCII() bool                { return m.t.ASCII() }
func (m mimeTypeVal) Registered() bool           { return m.t.Registered() }
func (m mimeTypeVal) Obsolete() bool             { return m.t.Obsolete() }
func (m mimeTypeVal) Complete() bool             { return m.t.Complete() }
func (m mimeTypeVal) Signature() bool            { return m.t.Signature() }
func (m mimeTypeVal) Provisional() bool          { return m.t.Provisional() }

// mimeDefault is the complete embedded registry, the object MIME::Types module
// methods query. It is the library's shared Default() singleton (read-only and
// safe for reuse across calls).
func mimeDefault() *mimetypes.Registry { return mimetypes.Default() }

// mimeTypeArray wraps a priority-sorted slice of library types into a Ruby Array
// of MIME::Type value objects, preserving the registry's order.
func mimeTypeArray(ts []*mimetypes.Type) object.Value {
	arr := object.NewArrayFromSlice(make([]object.Value, len(ts)))
	for i, t := range ts {
		arr.Elems[i] = &MIMEType{t: mimeTypeVal{t: t}}
	}
	return arr
}
