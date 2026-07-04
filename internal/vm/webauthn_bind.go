// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"

	libwebauthn "github.com/go-ruby-webauthn/webauthn"
	protocol "github.com/go-webauthn/webauthn/protocol"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin value bridge between rbgo's Ruby object graph and the Go
// model of github.com/go-ruby-webauthn/webauthn. It maps the keyword-argument
// Hashes of the ceremony entry points onto the library's CreateOptions /
// GetOptions / VerifyOption values, reads binary arguments (challenges,
// credential IDs, COSE keys, client responses) as raw bytes, renders bytes back
// as binary (ASCII-8BIT) Strings, and re-raises the library's WebAuthn::Error
// tree as the matching Ruby exceptions. See webauthn.go for the class surface.

// waSMethod installs a class ("singleton") method on a class.
func waSMethod(cls *RClass, name string, fn NativeFn) {
	cls.smethods[name] = &Method{name: name, owner: cls, native: fn}
}

// waArity raises a Ruby ArgumentError when fewer than want arguments were given,
// matching MRI's arity error for the native method.
func waArity(args []object.Value, want int, name string) {
	if len(args) < want {
		raise("ArgumentError", "wrong number of arguments (given %d, expected %d) for %s",
			len(args), want, name)
	}
}

// waKwargs returns the trailing keyword Hash of an entry point, or an empty Hash
// when the last argument is not a Hash (so a caller can always read options).
func waKwargs(args []object.Value) *object.Hash {
	if len(args) == 0 {
		return object.NewHash()
	}
	if h, ok := args[len(args)-1].(*object.Hash); ok {
		return h
	}
	return object.NewHash()
}

// waHashGet fetches a symbol-keyed option from an options Hash.
func waHashGet(h *object.Hash, key string) (object.Value, bool) {
	return h.Get(object.Symbol(key))
}

// waRequiredStr fetches a required String keyword, raising ArgumentError when it
// is absent, matching the gem's required keyword arguments.
func waRequiredStr(h *object.Hash, key string) string {
	v, ok := waHashGet(h, key)
	if !ok {
		raise("ArgumentError", "missing keyword: :%s", key)
	}
	return strArg(v)
}

// waStringArray maps a Ruby Array of String/Symbol into a []string (used for the
// algorithms: option). A non-Array raises TypeError.
func waStringArray(v object.Value) []string {
	arr, ok := v.(*object.Array)
	if !ok {
		raise("TypeError", "expected an Array of algorithm names, got %s", v.Inspect())
	}
	out := make([]string, len(arr.Elems))
	for i, e := range arr.Elems {
		out[i] = waName(e)
	}
	return out
}

// waName renders a String or Symbol as its Go string, raising TypeError otherwise.
func waName(v object.Value) string {
	switch n := v.(type) {
	case *object.String:
		return n.Str()
	case object.Symbol:
		return string(n)
	}
	raise("TypeError", "expected a String or Symbol, got %s", v.Inspect())
	panic("unreachable")
}

// waBytes reads a Ruby String argument as its raw bytes, raising TypeError for a
// non-String (a challenge / credential ID / COSE key / client response is binary).
func waBytes(v object.Value) []byte {
	s, ok := v.(*object.String)
	if !ok {
		raise("TypeError", "no implicit conversion of %s into String", v.Inspect())
	}
	return s.Bytes()
}

// waBinary renders raw bytes as a binary (ASCII-8BIT) Ruby String.
func waBinary(b []byte) object.Value {
	return object.NewStringBytesEnc(b, "ASCII-8BIT")
}

// waPublicKeyBytes reads a verify_authentication public_key: option, which is
// either a WebAuthn::PublicKey (its COSE encoding is used) or a String of raw
// COSE bytes.
func waPublicKeyBytes(v object.Value) []byte {
	if pk, ok := v.(*WebAuthnPublicKey); ok {
		return pk.pk.COSEKey()
	}
	return waBytes(v)
}

// waCredentialIDs renders a credential descriptor list as a Ruby Array of binary
// Strings, one per credential ID (used by #exclude_credentials / #allow_credentials).
func waCredentialIDs(list []protocol.CredentialDescriptor) object.Value {
	out := make([]object.Value, len(list))
	for i, d := range list {
		out[i] = waBinary(d.CredentialID)
	}
	return object.NewArrayFromSlice(out)
}

// waCreateOptions maps the options_for_create keyword Hash onto the library's
// CreateOptions: the required user: (a Hash of id:/name:/display_name:), and the
// optional exclude:, user_verification:, attestation: and challenge: values.
func waCreateOptions(h *object.Hash) libwebauthn.CreateOptions {
	uv, ok := waHashGet(h, "user")
	if !ok {
		raise("ArgumentError", "missing keyword: :user")
	}
	uh, ok := uv.(*object.Hash)
	if !ok {
		raise("TypeError", "user: must be a Hash, got %s", uv.Inspect())
	}

	co := libwebauthn.CreateOptions{User: libwebauthn.User{}}
	if id, ok := waHashGet(uh, "id"); ok {
		co.User.ID = waBytes(id)
	}
	if name, ok := waHashGet(uh, "name"); ok {
		co.User.Name = strArg(name)
	}
	if dn, ok := waHashGet(uh, "display_name"); ok {
		co.User.DisplayName = strArg(dn)
	}

	if ex, ok := waHashGet(h, "exclude"); ok && ex.Truthy() {
		co.Exclude = waByteLists(ex)
	}
	if v, ok := waHashGet(h, "user_verification"); ok && v.Truthy() {
		co.UserVerification = protocol.UserVerificationRequirement(waName(v))
	}
	if v, ok := waHashGet(h, "attestation"); ok && v.Truthy() {
		co.Attestation = protocol.ConveyancePreference(waName(v))
	}
	if v, ok := waHashGet(h, "challenge"); ok && v.Truthy() {
		co.Challenge = waBytes(v)
	}
	return co
}

// waGetOptions maps the options_for_get keyword Hash onto the library's
// GetOptions: the optional allow:, user_verification: and challenge: values.
func waGetOptions(h *object.Hash) libwebauthn.GetOptions {
	var go_ libwebauthn.GetOptions
	if v, ok := waHashGet(h, "allow"); ok && v.Truthy() {
		go_.Allow = waByteLists(v)
	}
	if v, ok := waHashGet(h, "user_verification"); ok && v.Truthy() {
		go_.UserVerification = protocol.UserVerificationRequirement(waName(v))
	}
	if v, ok := waHashGet(h, "challenge"); ok && v.Truthy() {
		go_.Challenge = waBytes(v)
	}
	return go_
}

// waByteLists maps a Ruby value onto a [][]byte for the exclude:/allow: options:
// a single String yields a one-element list, an Array yields one entry per String
// element. Anything else raises TypeError.
func waByteLists(v object.Value) [][]byte {
	switch x := v.(type) {
	case *object.String:
		return [][]byte{x.Bytes()}
	case *object.Array:
		out := make([][]byte, len(x.Elems))
		for i, e := range x.Elems {
			out[i] = waBytes(e)
		}
		return out
	}
	raise("TypeError", "expected a String or an Array of credential IDs, got %s", v.Inspect())
	panic("unreachable")
}

// waVerifyOpts builds the VerifyOption slice from the trailing keyword arguments
// of verify_registration (a user_verification: flag).
func waVerifyOpts(rest []object.Value) []libwebauthn.VerifyOption {
	return waVerifyOptsHash(waKwargs(rest))
}

// waVerifyOptsHash builds the VerifyOption slice from an options Hash: a truthy
// user_verification: requires the authenticator's user-verified flag.
func waVerifyOptsHash(h *object.Hash) []libwebauthn.VerifyOption {
	if v, ok := waHashGet(h, "user_verification"); ok && v.Truthy() {
		return []libwebauthn.VerifyOption{libwebauthn.RequireUserVerification()}
	}
	return nil
}

// raiseWebAuthnErr re-raises a library error as the faithful Ruby exception. The
// library tags each error with a class name (Class()) that mirrors the gem's
// WebAuthn::Error subclass; the root ("Error") and any non-library error map to
// WebAuthn::Error. It never returns when err is non-nil.
func raiseWebAuthnErr(err error) {
	if err == nil {
		return
	}
	var we *libwebauthn.Error
	if errors.As(err, &we) && we.Class() != "Error" {
		raise("WebAuthn::"+we.Class(), "%s", err.Error())
	}
	raise("WebAuthn::Error", "%s", err.Error())
}
