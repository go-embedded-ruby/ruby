// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"
	"strings"

	sodium "github.com/go-ruby-sodium/sodium"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin binding between rbgo's Ruby object graph and the
// interpreter-independent NaCl core of github.com/go-ruby-sodium/sodium (a
// pure-Go, no-cgo port of the RbNaCl gem, itself a binding to libsodium).
// registerRbNaCl (in rbnacl.go) wires the RbNaCl module — SecretBox, Box, the
// PrivateKey/PublicKey and SigningKey/VerifyKey pairs, Hash, the Auth HMAC
// authenticators, PasswordHash, the AEAD constructions, GroupElement and Random,
// plus the RbNaCl::CryptoError exception tree — onto the library's primitives.
// This file holds the shared value wrapper, the Ruby-String<->[]byte bridge and
// the error mapper those bindings lean on. All cryptography is delegated to
// go-ruby-sodium; a ciphertext rbgo emits opens under RbNaCl and vice-versa.

// naclObj is the single value wrapper every RbNaCl instance uses: it carries the
// Ruby class it reports for dispatch (SecretBox, PrivateKey, VerifyKey, …) and
// the underlying go-ruby-sodium value (a *sodium.SecretBox, *sodium.PrivateKey,
// …). Instance methods type-assert self to *naclObj and switch on v, so one
// classOf case and one wrapper serve the whole module.
type naclObj struct {
	cls *RClass
	v   any
}

// ToS renders a generic instance form; the concrete classes do not override to_s
// (RbNaCl's value objects inherit Object#to_s), so this only backs inspect's
// default and is never the primary surface.
func (o *naclObj) ToS() string { return "#<" + o.cls.name + ">" }

// Inspect renders the Ruby inspect form, #<RbNaCl::Class>.
func (o *naclObj) Inspect() string { return "#<" + o.cls.name + ">" }

func (o *naclObj) Truthy() bool { return true }

// naclBytes extracts the raw bytes of a Ruby String argument. Keys, nonces,
// messages and ciphertexts are all byte strings; strArg raises TypeError for a
// non-String, matching MRI's implicit-conversion failure.
func naclBytes(v object.Value) []byte { return []byte(strArg(v)) }

// naclString wraps a raw byte slice as a Ruby String without a round-trip through
// UTF-8 validation, so binary output (ciphertext, a raw key, a digest) survives
// intact — the equivalent of RbNaCl returning an ASCII-8BIT String.
func naclString(b []byte) object.Value { return object.NewStringBytes(b) }

// naclPrivateKey unwraps a Ruby RbNaCl::PrivateKey to its *sodium.PrivateKey,
// raising TypeError when handed anything else (as Box.new(pub, priv) does when
// its private-key argument is not a PrivateKey).
func naclPrivateKey(v object.Value) *sodium.PrivateKey {
	if o, ok := v.(*naclObj); ok {
		if pk, ok := o.v.(*sodium.PrivateKey); ok {
			return pk
		}
	}
	raise("TypeError", "expected RbNaCl::PrivateKey")
	return nil
}

// naclPublicKey unwraps a Ruby RbNaCl::PublicKey to its *sodium.PublicKey,
// raising TypeError for anything else (as Box.new(pub, priv) does for a bad
// public-key argument).
func naclPublicKey(v object.Value) *sodium.PublicKey {
	if o, ok := v.(*naclObj); ok {
		if pub, ok := o.v.(*sodium.PublicKey); ok {
			return pub
		}
	}
	raise("TypeError", "expected RbNaCl::PublicKey")
	return nil
}

// naclOptsHash returns the trailing keyword Hash of an RbNaCl entry point (the
// digest_size:/key:/salt:/personal: options to Hash.blake2b), or nil when the
// arguments are empty or the last is not a Hash.
func naclOptsHash(rest []object.Value) *object.Hash {
	if len(rest) == 0 {
		return nil
	}
	h, _ := rest[len(rest)-1].(*object.Hash)
	return h
}

// naclBlake2bOpts maps a Hash.blake2b options Hash to the library's
// Blake2bOptions. An absent Hash yields the zero value (the default 32-byte
// unkeyed digest); digest_size: sets the output length and key:/salt:/personal:
// supply the keyed-MAC and parameter-block inputs.
func naclBlake2bOpts(rest []object.Value) sodium.Blake2bOptions {
	var o sodium.Blake2bOptions
	h := naclOptsHash(rest)
	if h == nil {
		return o
	}
	if v, ok := h.Get(object.Symbol("digest_size")); ok {
		o.DigestSize = int(intArg(v))
	}
	if v, ok := h.Get(object.Symbol("key")); ok {
		o.Key = naclBytes(v)
	}
	if v, ok := h.Get(object.Symbol("salt")); ok {
		o.Salt = naclBytes(v)
	}
	if v, ok := h.Get(object.Symbol("personal")); ok {
		o.Personal = naclBytes(v)
	}
	return o
}

// naclOptBytes returns the raw bytes of an optional trailing argument (the
// additional_data of an AEAD encrypt/decrypt), or nil when it is absent.
func naclOptBytes(args []object.Value, i int) []byte {
	if len(args) > i && args[i] != object.NilV {
		return naclBytes(args[i])
	}
	return nil
}

// raiseSodiumError re-raises a go-ruby-sodium error as the matching Ruby
// exception. A *LengthError (a mis-sized key/nonce/tag) maps to
// RbNaCl::LengthError; a *BadAuthenticatorError maps to
// RbNaCl::BadSignatureError when it comes from signature verification and
// RbNaCl::BadAuthenticatorError otherwise (a forged ciphertext); any other
// crypto error maps to the RbNaCl::CryptoError root. It never returns (raise
// panics) but yields an object.Value so callers can write
// `return raiseSodiumError(err)` in a value position with no dead code after.
func raiseSodiumError(err error) object.Value {
	var le *sodium.LengthError
	if errors.As(err, &le) {
		return raise("RbNaCl::LengthError", "%s", le.Error())
	}
	var be *sodium.BadAuthenticatorError
	if errors.As(err, &be) {
		if strings.Contains(be.Op, "signature") {
			return raise("RbNaCl::BadSignatureError", "%s", be.Error())
		}
		return raise("RbNaCl::BadAuthenticatorError", "%s", be.Error())
	}
	return raise("RbNaCl::CryptoError", "%s", err.Error())
}
