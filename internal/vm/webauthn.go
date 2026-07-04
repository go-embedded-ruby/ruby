// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	libwebauthn "github.com/go-ruby-webauthn/webauthn"
	protocol "github.com/go-webauthn/webauthn/protocol"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file binds github.com/go-ruby-webauthn/webauthn — the pure-Go (CGO=0),
// webauthn-ruby-gem-faithful relying-party library (a gem-shaped orchestration on
// top of go-webauthn/webauthn) — into rbgo as the native `WebAuthn` module
// (require "webauthn"). The library owns the two ceremonies: it builds the
// creation/request options a browser passes to navigator.credentials, and it
// verifies the attestation (registration) and assertion (authentication) client
// responses, delegating CBOR/COSE decoding and the signature checks to
// go-webauthn. This file is the thin shell mapping Ruby values onto the library's
// Go model (see webauthn_bind.go) and exposing the gem-flavoured surface:
//
//	WebAuthn::RelyingParty   — origin/name/id + the four ceremony entry points
//	WebAuthn::PublicKey      — a stored COSE credential public key (+ verify)
//	WebAuthn::Credential     — a verified registration/authentication result
//	WebAuthn::PublicKeyCredentialCreationOptions / ...RequestOptions — options
//	WebAuthn::Error (< StandardError) and its subclass tree — the exceptions
//
// Every verification is endianness- and timezone-independent and builds with CGO
// disabled on all six supported 64-bit targets, including the big-endian s390x.

// WebAuthnRelyingParty is an instance of WebAuthn::RelyingParty, wrapping a
// go-ruby-webauthn *RelyingParty (the gem's WebAuthn::RelyingParty).
type WebAuthnRelyingParty struct {
	cls *RClass
	rp  *libwebauthn.RelyingParty
}

func (r *WebAuthnRelyingParty) ToS() string     { return "#<WebAuthn::RelyingParty " + r.rp.ID + ">" }
func (r *WebAuthnRelyingParty) Inspect() string { return r.ToS() }
func (r *WebAuthnRelyingParty) Truthy() bool    { return true }

// WebAuthnPublicKey is an instance of WebAuthn::PublicKey, wrapping a
// go-ruby-webauthn *PublicKey (a COSE_Key encoded credential public key).
type WebAuthnPublicKey struct {
	cls *RClass
	pk  *libwebauthn.PublicKey
}

func (p *WebAuthnPublicKey) ToS() string     { return "#<WebAuthn::PublicKey>" }
func (p *WebAuthnPublicKey) Inspect() string { return p.ToS() }
func (p *WebAuthnPublicKey) Truthy() bool    { return true }

// WebAuthnCredential is an instance of WebAuthn::Credential, the verified result
// of a ceremony. Exactly one of reg/auth is non-nil: reg for a registration
// (verify_registration), auth for an authentication (verify_authentication).
type WebAuthnCredential struct {
	cls  *RClass
	reg  *libwebauthn.RegistrationCredential
	auth *libwebauthn.AuthenticationCredential
}

func (c *WebAuthnCredential) ToS() string     { return "#<WebAuthn::Credential>" }
func (c *WebAuthnCredential) Inspect() string { return c.ToS() }
func (c *WebAuthnCredential) Truthy() bool    { return true }

// WebAuthnCreateOptions is an instance of
// WebAuthn::PublicKeyCredentialCreationOptions, the options_for_create result.
type WebAuthnCreateOptions struct {
	cls  *RClass
	opts *protocol.PublicKeyCredentialCreationOptions
}

func (o *WebAuthnCreateOptions) ToS() string {
	return "#<WebAuthn::PublicKeyCredentialCreationOptions>"
}
func (o *WebAuthnCreateOptions) Inspect() string { return o.ToS() }
func (o *WebAuthnCreateOptions) Truthy() bool    { return true }

// WebAuthnGetOptions is an instance of
// WebAuthn::PublicKeyCredentialRequestOptions, the options_for_get result.
type WebAuthnGetOptions struct {
	cls  *RClass
	opts *protocol.PublicKeyCredentialRequestOptions
}

func (o *WebAuthnGetOptions) ToS() string     { return "#<WebAuthn::PublicKeyCredentialRequestOptions>" }
func (o *WebAuthnGetOptions) Inspect() string { return o.ToS() }
func (o *WebAuthnGetOptions) Truthy() bool    { return true }

// registerWebAuthn installs the WebAuthn module (require "webauthn"). It runs
// eagerly at boot; the error tree needs StandardError in place.
func (vm *VM) registerWebAuthn() {
	mod := newClass("WebAuthn", nil)
	mod.isModule = true
	vm.consts["WebAuthn"] = mod

	vm.registerWebAuthnErrors(mod)

	mk := func(name string) *RClass {
		full := "WebAuthn::" + name
		cls := newClass(full, vm.cObject)
		mod.consts[name] = cls
		vm.consts[full] = cls
		return cls
	}

	rpCls := mk("RelyingParty")
	pkCls := mk("PublicKey")
	credCls := mk("Credential")
	createOptsCls := mk("PublicKeyCredentialCreationOptions")
	getOptsCls := mk("PublicKeyCredentialRequestOptions")

	vm.registerWebAuthnRelyingParty(rpCls, credCls, pkCls, createOptsCls, getOptsCls)
	vm.registerWebAuthnPublicKey(pkCls)
	vm.registerWebAuthnCredential(credCls, pkCls)
	vm.registerWebAuthnCreateOptions(createOptsCls)
	vm.registerWebAuthnGetOptions(getOptsCls)
}

// registerWebAuthnErrors installs the WebAuthn::Error exception tree mirroring the
// gem: Error < StandardError, and every specific verification failure < Error.
// Each class is registered both as a nested constant of WebAuthn and under its
// qualified name in the top-level table, so a re-raised library error (whose
// Class() names the unqualified Ruby class) rescues as the matching class.
func (vm *VM) registerWebAuthnErrors(mod *RClass) {
	std := vm.consts["StandardError"].(*RClass)
	reg := func(simple string, super *RClass) *RClass {
		qualified := "WebAuthn::" + simple
		c := newClass(qualified, super)
		mod.consts[simple] = c
		vm.consts[qualified] = c
		return c
	}
	base := reg("Error", std)
	for _, name := range []string{
		"ChallengeVerificationError",
		"OriginVerificationError",
		"TypeVerificationError",
		"RpIdVerificationError",
		"UserPresenceVerificationError",
		"UserVerificationError",
		"SignatureVerificationError",
		"SignCountVerificationError",
		"AttestationStatementVerificationError",
		"AuthenticatorDataVerificationError",
		"ClientDataMissingError",
	} {
		reg(name, base)
	}
}

// registerWebAuthnRelyingParty installs WebAuthn::RelyingParty: RelyingParty.new
// (origin:, name:, id:, algorithms:, timeout:), the origin/name/id/algorithms
// readers, and the four ceremony entry points options_for_create /
// verify_registration / options_for_get / verify_authentication.
func (vm *VM) registerWebAuthnRelyingParty(cls, credCls, pkCls, createOptsCls, getOptsCls *RClass) {
	waSMethod(cls, "new", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		h := waKwargs(args)
		rp := libwebauthn.NewRelyingParty(
			waRequiredStr(h, "origin"),
			waRequiredStr(h, "name"),
			waRequiredStr(h, "id"),
		)
		if v, ok := waHashGet(h, "algorithms"); ok && v.Truthy() {
			rp.Algorithms = waStringArray(v)
		}
		if v, ok := waHashGet(h, "timeout"); ok && v.Truthy() {
			rp.Timeout = int(intArg(v))
		}
		return &WebAuthnRelyingParty{cls: cls, rp: rp}
	})

	self := func(v object.Value) *libwebauthn.RelyingParty { return v.(*WebAuthnRelyingParty).rp }
	d := func(name string, fn NativeFn) { cls.define(name, fn) }

	d("origin", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).Origin)
	})
	d("name", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).Name)
	})
	d("id", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).ID)
	})
	d("algorithms", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		names := self(v).Algorithms
		out := make([]object.Value, len(names))
		for i, n := range names {
			out[i] = object.NewString(n)
		}
		return object.NewArrayFromSlice(out)
	})

	d("options_for_create", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		opts, err := self(v).OptionsForCreate(waCreateOptions(waKwargs(args)))
		raiseWebAuthnErr(err)
		return &WebAuthnCreateOptions{cls: createOptsCls, opts: opts}
	})

	d("options_for_get", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		opts, err := self(v).OptionsForGet(waGetOptions(waKwargs(args)))
		raiseWebAuthnErr(err)
		return &WebAuthnGetOptions{cls: getOptsCls, opts: opts}
	})

	d("verify_registration", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2) for verify_registration", len(args))
		}
		cred, err := self(v).FromCreate(waBytes(args[0]))
		raiseWebAuthnErr(err)
		raiseWebAuthnErr(cred.Verify(waBytes(args[1]), waVerifyOpts(args[2:])...))
		return &WebAuthnCredential{cls: credCls, reg: cred}
	})

	d("verify_authentication", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2) for verify_authentication", len(args))
		}
		h := waKwargs(args[2:])
		pk, ok := waHashGet(h, "public_key")
		if !ok {
			raise("ArgumentError", "missing keyword: :public_key")
		}
		sc, ok := waHashGet(h, "sign_count")
		if !ok {
			raise("ArgumentError", "missing keyword: :sign_count")
		}
		cred, err := self(v).FromGet(waBytes(args[0]))
		raiseWebAuthnErr(err)
		raiseWebAuthnErr(cred.Verify(waBytes(args[1]), waPublicKeyBytes(pk), uint32(intArg(sc)), waVerifyOptsHash(h)...))
		return &WebAuthnCredential{cls: credCls, auth: cred}
	})
}

// registerWebAuthnPublicKey installs WebAuthn::PublicKey: PublicKey.new(cose_bytes)
// (a stored COSE credential public key), the cose_key reader and #verify.
func (vm *VM) registerWebAuthnPublicKey(cls *RClass) {
	waSMethod(cls, "new", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		waArity(args, 1, "new")
		pk, err := libwebauthn.NewPublicKey(waBytes(args[0]))
		raiseWebAuthnErr(err)
		return &WebAuthnPublicKey{cls: cls, pk: pk}
	})

	self := func(v object.Value) *libwebauthn.PublicKey { return v.(*WebAuthnPublicKey).pk }
	d := func(name string, fn NativeFn) { cls.define(name, fn) }

	d("cose_key", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return waBinary(self(v).COSEKey())
	})
	d("verify", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		waArity(args, 2, "verify")
		ok, err := self(v).Verify(waBytes(args[0]), waBytes(args[1]))
		raiseWebAuthnErr(err)
		return object.Bool(ok)
	})
}

// registerWebAuthnCredential installs WebAuthn::Credential: the verified ceremony
// result. id and sign_count are available for both ceremonies; public_key,
// attestation_format and attestation_type are populated only for a registration
// (nil for an authentication).
func (vm *VM) registerWebAuthnCredential(cls, pkCls *RClass) {
	self := func(v object.Value) *WebAuthnCredential { return v.(*WebAuthnCredential) }
	d := func(name string, fn NativeFn) { cls.define(name, fn) }

	d("id", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		c := self(v)
		if c.reg != nil {
			return waBinary(c.reg.ID())
		}
		return waBinary(c.auth.ID())
	})
	d("sign_count", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		c := self(v)
		if c.reg != nil {
			return object.IntValue(int64(c.reg.SignCount()))
		}
		return object.IntValue(int64(c.auth.SignCount()))
	})
	d("public_key", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		c := self(v)
		if c.reg == nil {
			return object.NilV
		}
		return &WebAuthnPublicKey{cls: pkCls, pk: c.reg.PublicKey()}
	})
	d("attestation_format", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		c := self(v)
		if c.reg == nil {
			return object.NilV
		}
		return object.NewString(c.reg.AttestationFormat())
	})
	d("attestation_type", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		c := self(v)
		if c.reg == nil {
			return object.NilV
		}
		return object.NewString(c.reg.AttestationType())
	})
}

// registerWebAuthnCreateOptions installs the read surface of
// WebAuthn::PublicKeyCredentialCreationOptions: the challenge (raw bytes) plus
// the relying-party/user identity, timeout, algorithm ids and exclude list the
// browser needs for navigator.credentials.create().
func (vm *VM) registerWebAuthnCreateOptions(cls *RClass) {
	self := func(v object.Value) *protocol.PublicKeyCredentialCreationOptions {
		return v.(*WebAuthnCreateOptions).opts
	}
	d := func(name string, fn NativeFn) { cls.define(name, fn) }

	d("challenge", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return waBinary(self(v).Challenge)
	})
	d("rp_id", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).RelyingParty.ID)
	})
	d("rp_name", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).RelyingParty.Name)
	})
	d("user_id", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		// OptionsForCreate always stores the user handle as []byte (a nil handle
		// stays a typed nil), so the assertion always succeeds.
		id, _ := self(v).User.ID.([]byte)
		return waBinary(id)
	})
	d("timeout", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).Timeout))
	})
	d("algorithms", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		params := self(v).Parameters
		out := make([]object.Value, len(params))
		for i, p := range params {
			out[i] = object.IntValue(int64(p.Algorithm))
		}
		return object.NewArrayFromSlice(out)
	})
	d("exclude_credentials", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return waCredentialIDs(self(v).CredentialExcludeList)
	})
}

// registerWebAuthnGetOptions installs the read surface of
// WebAuthn::PublicKeyCredentialRequestOptions: the challenge (raw bytes), RP ID,
// timeout, allow list and user-verification requirement the browser needs for
// navigator.credentials.get().
func (vm *VM) registerWebAuthnGetOptions(cls *RClass) {
	self := func(v object.Value) *protocol.PublicKeyCredentialRequestOptions {
		return v.(*WebAuthnGetOptions).opts
	}
	d := func(name string, fn NativeFn) { cls.define(name, fn) }

	d("challenge", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return waBinary(self(v).Challenge)
	})
	d("rp_id", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).RelyingPartyID)
	})
	d("timeout", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).Timeout))
	})
	d("allow_credentials", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return waCredentialIDs(self(v).AllowedCredentials)
	})
	d("user_verification", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(string(self(v).UserVerification))
	})
}
