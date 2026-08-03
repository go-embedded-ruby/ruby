// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"io"
	"testing"
)

// TestInternEncodingFallback exercises internEncoding directly: a registered name
// resolves to the shared registry object (and is stable across calls), while an
// otherwise-unknown tag — which a String's Enc should never actually carry — mints
// a transient ASCII-compatible Encoding so callers never see nil.
func TestInternEncodingFallback(t *testing.T) {
	vm := New(io.Discard)

	// Registered names (canonical and alias) return the same interned object.
	utf8a := vm.internEncoding("UTF-8")
	utf8b := vm.internEncoding("utf-8")
	if utf8a == nil || utf8a != utf8b {
		t.Fatalf("registered lookups differ: %p vs %p", utf8a, utf8b)
	}
	if utf8a.name != "UTF-8" {
		t.Errorf("name = %q, want UTF-8", utf8a.name)
	}

	// An unregistered tag mints a transient, ASCII-compatible, non-dummy encoding,
	// interned so repeat calls are stable.
	custom := vm.internEncoding("X-CUSTOM-TAG")
	if custom == nil {
		t.Fatal("internEncoding returned nil for an unknown tag")
	}
	if custom.name != "X-CUSTOM-TAG" || !custom.asciiCompat || custom.dummy {
		t.Errorf("transient encoding = %+v, want ascii-compatible non-dummy named X-CUSTOM-TAG", custom)
	}
	if again := vm.internEncoding("X-CUSTOM-TAG"); again != custom {
		t.Errorf("transient encoding not interned: %p vs %p", again, custom)
	}
}
