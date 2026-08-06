// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"bytes"
	"strings"
	"testing"
)

// TestServerGemFeaturesProvided pins the roster of server/ops/testing require
// feature names compiled out of the js/wasm build and proves each is a name
// require.go actually advertises as provided — otherwise its wasm LoadError hook
// (servergems_wasm.go) would never fire, and native require would already raise
// "cannot load such file" from the file search (or the name is a typo). If a gem
// is added to or removed from the server-gem seam, this list must change with it.
func TestServerGemFeaturesProvided(t *testing.T) {
	if len(serverGemFeatures) == 0 {
		t.Fatal("serverGemFeatures is empty")
	}
	seen := map[string]bool{}
	for _, f := range serverGemFeatures {
		if seen[f] {
			t.Errorf("feature %q listed twice in serverGemFeatures", f)
		}
		seen[f] = true
		if !providedFeatures[f] {
			t.Errorf("feature %q is guarded for wasm but missing from require.go providedFeatures", f)
		}
	}
	// Sidekiq and Resque belong to the network-backend seam, not this one; they
	// must not be duplicated here (that would double-hook their require).
	for _, f := range []string{"sidekiq", "resque"} {
		if seen[f] {
			t.Errorf("feature %q is owned by the backend seam and must not be in serverGemFeatures", f)
		}
	}
}

// TestServerGemsRaiseWasmUnavailable proves that, for every server-gem feature,
// the require hook the wasm build installs raises the exact MRI-style LoadError
// and clears the loaded marker so a retried require re-raises. raiseWasmUnavailable
// is the same shared routine the wasm stub wires, so this native test covers the
// LoadError behaviour the browser build produces.
func TestServerGemsRaiseWasmUnavailable(t *testing.T) {
	for _, feature := range serverGemFeatures {
		t.Run(feature, func(t *testing.T) {
			vm := New(&bytes.Buffer{})
			key := "feature:" + feature
			vm.loaded[key] = true // simulate require.go having marked it loaded
			defer func() {
				r := recover()
				e, ok := r.(RubyError)
				if !ok {
					t.Fatalf("want RubyError, got %T: %v", r, r)
				}
				if e.Class != "LoadError" {
					t.Errorf("class = %q, want LoadError", e.Class)
				}
				wantMsg := "cannot load such file -- " + feature + " (not available in the wasm build)"
				if e.Message != wantMsg {
					t.Errorf("message = %q, want %q", e.Message, wantMsg)
				}
				if !strings.Contains(e.Message, "not available in the wasm build") {
					t.Errorf("message %q missing reason annotation", e.Message)
				}
				if vm.loaded[key] {
					t.Errorf("loaded marker %q not cleared before raise", key)
				}
			}()
			vm.raiseWasmUnavailable(feature)
			t.Fatal("raiseWasmUnavailable returned without raising")
		})
	}
}
