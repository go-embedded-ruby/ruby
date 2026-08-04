// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"bytes"
	"slices"
	"strings"
	"testing"
)

// TestWasmUnavailableFeaturesTable pins the roster of require feature names whose
// heavy network/OS backends are compiled out of the js/wasm build. It is the
// native assertion over the wasm stub-registration table (backends_wasm.go wires
// each of these to a LoadError-raising require hook): if a backend is added to or
// removed from the guard, this list must change with it.
func TestWasmUnavailableFeaturesTable(t *testing.T) {
	want := []string{
		"grpc", "nats", "kafka", "mysql2", "mysql",
		"mongo", "bson", "parquet", "arrow", "openstack",
		"sidekiq", "resque",
	}
	if !slices.Equal(want, wasmUnavailableFeatures) {
		t.Fatalf("wasmUnavailableFeatures = %v, want %v", wasmUnavailableFeatures, want)
	}
	// Every listed feature must be a name require.go actually advertises as
	// provided — otherwise the wasm hook would never fire (require would already
	// raise "cannot load such file" from the file search, or the name is a typo).
	for _, f := range wasmUnavailableFeatures {
		if !providedFeatures[f] {
			t.Errorf("feature %q is guarded for wasm but missing from require.go providedFeatures", f)
		}
	}
}

// TestRaiseWasmUnavailable proves the LoadError a js/wasm require of a
// compiled-out backend produces: the exact MRI-style "cannot load such file"
// class and message (annotated with the reason), for every guarded feature. It
// also covers the loaded-marker reset that lets a retried require re-raise rather
// than silently return false.
func TestRaiseWasmUnavailable(t *testing.T) {
	for _, feature := range wasmUnavailableFeatures {
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
