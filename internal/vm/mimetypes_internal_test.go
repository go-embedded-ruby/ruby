// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"testing"

	mimetypes "github.com/go-ruby-mime-types/mime-types"
)

// TestMIMETypeShell covers the Go-only arms of the MIME::Type value shell — ToS
// and Truthy — which the Ruby-level to_s override does not reach.
func TestMIMETypeShell(t *testing.T) {
	ts := mimetypes.Default().Get("text/html")
	if len(ts) == 0 {
		t.Fatal("text/html not in registry")
	}
	v := &MIMEType{t: mimeTypeVal{t: ts[0]}}
	if got := v.ToS(); got != "text/html" {
		t.Errorf("ToS() = %q", got)
	}
	if !v.Truthy() {
		t.Error("a MIME::Type must be truthy")
	}
}
