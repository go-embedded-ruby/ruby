// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"

	age "github.com/go-ruby-age/age"
	"github.com/go-ruby-age/age/scrypt"
	"github.com/go-ruby-age/age/x25519"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin binding between rbgo's Ruby object graph and the
// interpreter-independent age core of github.com/go-ruby-age/age (a pure-Go,
// no-cgo façade over filippo.io/age, the reference age implementation). It
// carries the four instance value types Age wraps — an X25519 identity and
// recipient and an scrypt identity and recipient — plus the conversions that
// turn a Ruby recipients:/identities: option into the []age.Recipient /
// []age.Identity the library encrypts and decrypts with, and the error bridge
// that re-raises the library's sentinel tree as the Age::Error exceptions. All
// cryptography is delegated to go-ruby-age; a file rbgo emits is read by the
// reference age CLI and vice-versa. See age.go for the module wiring.

// AgeX25519Identity is an instance of Age::X25519::Identity: an X25519 key pair
// backed by a go-ruby-age *x25519.Identity. Its #to_s is the "AGE-SECRET-KEY-1…"
// secret key and #to_public yields the matching Age::X25519::Recipient.
type AgeX25519Identity struct {
	cls *RClass
	id  *x25519.Identity
}

// ToS renders the secret key, matching Age::X25519::Identity#to_s.
func (i *AgeX25519Identity) ToS() string { return i.id.String() }

// Inspect renders the identity as a quoted secret-key String.
func (i *AgeX25519Identity) Inspect() string { return object.NewString(i.id.String()).Inspect() }

func (i *AgeX25519Identity) Truthy() bool { return true }

// AgeX25519Recipient is an instance of Age::X25519::Recipient: an X25519 public
// recipient backed by a go-ruby-age *x25519.Recipient. Its #to_s is the "age1…"
// recipient string.
type AgeX25519Recipient struct {
	cls *RClass
	rcp *x25519.Recipient
}

// ToS renders the recipient, matching Age::X25519::Recipient#to_s.
func (r *AgeX25519Recipient) ToS() string { return r.rcp.String() }

// Inspect renders the recipient as a quoted "age1…" String.
func (r *AgeX25519Recipient) Inspect() string { return object.NewString(r.rcp.String()).Inspect() }

func (r *AgeX25519Recipient) Truthy() bool { return true }

// AgeScryptIdentity is an instance of Age::Scrypt::Identity: a passphrase-based
// identity backed by a go-ruby-age *scrypt.Identity.
type AgeScryptIdentity struct {
	cls *RClass
	id  *scrypt.Identity
}

func (i *AgeScryptIdentity) ToS() string     { return "#<Age::Scrypt::Identity>" }
func (i *AgeScryptIdentity) Inspect() string { return i.ToS() }
func (i *AgeScryptIdentity) Truthy() bool    { return true }

// AgeScryptRecipient is an instance of Age::Scrypt::Recipient: a passphrase-based
// recipient backed by a go-ruby-age *scrypt.Recipient. It must be the sole
// recipient of a message.
type AgeScryptRecipient struct {
	cls *RClass
	rcp *scrypt.Recipient
}

func (r *AgeScryptRecipient) ToS() string     { return "#<Age::Scrypt::Recipient>" }
func (r *AgeScryptRecipient) Inspect() string { return r.ToS() }
func (r *AgeScryptRecipient) Truthy() bool    { return true }

// ageRecipients converts a Ruby recipients: option to the []age.Recipient the
// library encrypts with. The value is a single recipient or an Array of them;
// each element is an Age::X25519::Recipient, an Age::Scrypt::Recipient, or an
// "age1…" String parsed as an X25519 recipient. Anything else raises TypeError,
// and an unparseable String raises Age::ParseError.
func ageRecipients(v object.Value) []age.Recipient {
	elems := ageOptElems(v)
	out := make([]age.Recipient, len(elems))
	for i, e := range elems {
		switch r := e.(type) {
		case *AgeX25519Recipient:
			out[i] = r.rcp
		case *AgeScryptRecipient:
			out[i] = r.rcp
		case *object.String:
			pr, err := x25519.ParseRecipient(r.Str())
			if err != nil {
				raiseAgeError(err)
			}
			out[i] = pr
		default:
			raise("TypeError", "no implicit conversion of %s into Age recipient", e.Inspect())
		}
	}
	return out
}

// ageIdentities converts a Ruby identities: option to the []age.Identity the
// library decrypts with. The value is a single identity or an Array of them;
// each element is an Age::X25519::Identity, an Age::Scrypt::Identity, or an
// "AGE-SECRET-KEY-1…" String parsed as an X25519 identity. Anything else raises
// TypeError, and an unparseable String raises Age::ParseError.
func ageIdentities(v object.Value) []age.Identity {
	elems := ageOptElems(v)
	out := make([]age.Identity, len(elems))
	for i, e := range elems {
		switch id := e.(type) {
		case *AgeX25519Identity:
			out[i] = id.id
		case *AgeScryptIdentity:
			out[i] = id.id
		case *object.String:
			pi, err := x25519.ParseIdentity(id.Str())
			if err != nil {
				raiseAgeError(err)
			}
			out[i] = pi
		default:
			raise("TypeError", "no implicit conversion of %s into Age identity", e.Inspect())
		}
	}
	return out
}

// ageOptElems normalises a recipients:/identities: option to a slice: an Array
// yields its elements, and any other single value yields a one-element slice.
func ageOptElems(v object.Value) []object.Value {
	if arr, ok := v.(*object.Array); ok {
		return arr.Elems
	}
	return []object.Value{v}
}

// raiseAgeError re-raises a go-ruby-age sentinel as the matching Age::Error
// exception: ErrNoIdentityMatch / ErrIncorrectPassphrase / ErrParse / ErrFormat /
// ErrEncrypt map to their Age::* classes, and any other error wrapping age.Err
// (or an unrecognised error) to the Age::Error base. raiseAgeError never returns
// (raise panics); it is typed to return any so a caller can write
// `return raiseAgeError(err)` in a value position and leave no dead code.
func raiseAgeError(err error) any {
	switch {
	case errors.Is(err, age.ErrNoIdentityMatch):
		raise("Age::NoIdentityMatchError", "%s", err.Error())
	case errors.Is(err, age.ErrIncorrectPassphrase):
		raise("Age::IncorrectPassphraseError", "%s", err.Error())
	case errors.Is(err, age.ErrParse):
		raise("Age::ParseError", "%s", err.Error())
	case errors.Is(err, age.ErrFormat):
		raise("Age::FormatError", "%s", err.Error())
	case errors.Is(err, age.ErrEncrypt):
		raise("Age::EncryptError", "%s", err.Error())
	}
	return raise("Age::Error", "%s", err.Error())
}
