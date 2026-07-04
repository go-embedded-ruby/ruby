// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"
	"testing"

	libwebauthn "github.com/go-ruby-webauthn/webauthn"
)

// TestRaiseWebAuthnErr covers raiseWebAuthnErr directly: nil is a no-op; a library
// sentinel with a specific class maps to its WebAuthn:: subclass; the root
// ErrWebAuthn (class "Error") and any non-library error both map to the
// WebAuthn::Error base. The Ruby-driven verification tests exercise the specific
// subclasses; this covers the fall-through the ceremonies cannot reach.
func TestRaiseWebAuthnErr(t *testing.T) {
	raiseWebAuthnErr(nil) // no panic, no raise

	assertRaises(t, "WebAuthn::ChallengeVerificationError", func() {
		raiseWebAuthnErr(libwebauthn.ChallengeVerificationError)
	})
	assertRaises(t, "WebAuthn::Error", func() {
		raiseWebAuthnErr(libwebauthn.ErrWebAuthn)
	})
	assertRaises(t, "WebAuthn::Error", func() {
		raiseWebAuthnErr(errors.New("some non-webauthn failure"))
	})
}
