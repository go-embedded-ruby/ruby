// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"bytes"
	"strings"
	"testing"
)

// TestClosedUnavailableRoster pins the roster of features a closed-world build
// compiles out. It is a literal list rather than a count so that adding one is a
// deliberate edit with a reason, and so the reason can be read here: the roster
// exists because a closed-world binary must not link the front-end, and RuboCop
// imports the parser.
func TestClosedUnavailableRoster(t *testing.T) {
	if got, want := len(closedUnavailableFeatures), 1; got != want {
		t.Fatalf("closedUnavailableFeatures has %d entries (%v), want %d", got, closedUnavailableFeatures, want)
	}
	if closedUnavailableFeatures[0] != "rubocop" {
		t.Errorf("closedUnavailableFeatures = %v, want [rubocop]", closedUnavailableFeatures)
	}
	// Every name in the roster must be a feature require.go actually provides:
	// the hook fires only after require.go has marked a PROVIDED feature loaded,
	// so a name absent from providedFeatures would make `require` fail with the
	// ordinary missing-gem LoadError and the hook would never run — the roster
	// would look installed while being dead.
	for _, f := range closedUnavailableFeatures {
		if !providedFeatures[f] {
			t.Errorf("%q is in the closed roster but not in require.go's providedFeatures, so its hook can never fire", f)
		}
	}
}

// TestRaiseClosedUnavailable proves the LoadError a closed-world require of a
// compiled-out gem produces: MRI's "cannot load such file" class and message with
// the reason annotated, plus the loaded-marker reset that lets a retried require
// re-raise instead of silently returning false.
//
// It runs natively. That is the point of keeping the roster and the raise in a
// build-tag-free file: asserting this would otherwise need a closed-world build,
// and a check that needs a special build is a check that does not run.
func TestRaiseClosedUnavailable(t *testing.T) {
	for _, feature := range closedUnavailableFeatures {
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
				wantMsg := "cannot load such file -- " + feature + " (not available in the closed-world build)"
				if e.Message != wantMsg {
					t.Errorf("message = %q, want %q", e.Message, wantMsg)
				}
				if vm.loaded[key] {
					t.Errorf("loaded marker %q not cleared before raise", key)
				}
			}()
			vm.raiseClosedUnavailable(feature)
			t.Fatal("raiseClosedUnavailable returned without raising")
		})
	}
}

// TestClosedAndWasmUnavailableAgreeOnShape is the guard on a deliberate
// duplication. raiseClosedUnavailable and raiseWasmUnavailable are two copies of
// one rule — same class, same MRI wording, same marker reset, differing only in the
// reason named — because merging them means editing backends_unavailable.go, which
// the change that added the closed-world half was not scoped to touch.
//
// A duplication nothing checks drifts. This asserts the two against each other
// rather than against two independently written literals: it takes the wasm
// message as the reference, swaps only the parenthesised reason, and requires the
// closed message to match. Change either format string alone and this fails.
func TestClosedAndWasmUnavailableAgreeOnShape(t *testing.T) {
	const feature = "rubocop"

	capture := func(fn func()) RubyError {
		t.Helper()
		var e RubyError
		func() {
			defer func() {
				var ok bool
				e, ok = recover().(RubyError)
				if !ok {
					t.Fatal("expected a RubyError")
				}
			}()
			fn()
		}()
		return e
	}

	vm := New(&bytes.Buffer{})
	wasm := capture(func() { vm.raiseWasmUnavailable(feature) })
	closed := capture(func() { vm.raiseClosedUnavailable(feature) })

	if wasm.Class != closed.Class {
		t.Errorf("classes differ: wasm %q vs closed %q", wasm.Class, closed.Class)
	}
	// The wasm message is the reference. Only the reason may differ.
	const wasmReason = "(not available in the wasm build)"
	const closedReason = "(not available in the closed-world build)"
	if !strings.HasSuffix(wasm.Message, wasmReason) {
		t.Fatalf("wasm message %q does not end in %q — the reference wording moved", wasm.Message, wasmReason)
	}
	want := strings.TrimSuffix(wasm.Message, wasmReason) + closedReason
	if closed.Message != want {
		t.Errorf("the two messages have drifted apart:\n  closed: %q\n  want:   %q (the wasm message with only its reason swapped)", closed.Message, want)
	}
}
