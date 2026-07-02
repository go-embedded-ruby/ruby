// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"

	jwt "github.com/go-ruby-jwt/jwt"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin binding between rbgo's Ruby object graph and the
// interpreter-independent JWS core of github.com/go-ruby-jwt/jwt (a pure-Go,
// no-cgo port of Ruby's jwt gem). registerJWT wires the JWT module — JWT.encode
// and JWT.decode and the JWT::*Error exception tree — onto the library's Encode /
// Decode. Ruby payload/header Hashes bridge to the library's ordered value model
// (an *OrderedMap) and back; HS keys pass through as strings while RS/ES keys are
// PEM strings parsed here into the crypto/* key types the library signs with.

// registerJWT installs the JWT module (require "jwt"): JWT.encode(payload, key,
// algorithm, header_fields) and JWT.decode(token, key, verify, options), plus the
// JWT::DecodeError exception hierarchy. All signing/verification is delegated to
// go-ruby-jwt — a token rbgo emits verifies under the gem and vice-versa.
func (vm *VM) registerJWT() {
	mod := newClass("JWT", nil)
	mod.isModule = true
	vm.consts["JWT"] = mod

	vm.registerJWTErrors(mod)

	// JWT.encode(payload, key, algorithm = "HS256", header_fields = nil) builds a
	// signed compact token. The payload/header Hashes are converted to the library's
	// ordered value model; the key is an HS secret string or an RS/ES PEM string.
	mod.smethods["encode"] = &Method{name: "encode", owner: mod,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			payload := jwtFromRuby(args[0])
			alg := "HS256"
			if len(args) > 2 && args[2] != object.NilV {
				alg = strArg(args[2])
			}
			var header any
			if len(args) > 3 && args[3] != object.NilV {
				header = jwtFromRuby(args[3])
			}
			tok, err := jwt.Encode(payload, jwtKey(args[1], alg, false), alg, header)
			if err != nil {
				raiseJWTError(err)
			}
			return object.NewString(tok)
		}}

	// JWT.decode(token, key, verify = true, options = {}) verifies and parses a
	// token, returning [payload, header]. options is the gem's decode-options Hash
	// (algorithm:/algorithms:, leeway:, verify_expiration:, iss:/verify_iss:, …).
	mod.smethods["decode"] = &Method{name: "decode", owner: mod,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			verify := true
			if len(args) > 2 && args[2] != object.NilV {
				verify = args[2].Truthy()
			}
			opts, alg := jwtDecodeOpts(args[3:])
			payload, header, err := jwt.Decode(strArg(args[0]), jwtKey(args[1], alg, true), verify, opts)
			if err != nil {
				raiseJWTError(err)
			}
			return &object.Array{Elems: []object.Value{jwtToRuby(payload), jwtToRuby(header)}}
		}}
}

// registerJWTErrors installs the JWT exception tree mirroring the gem
// (DecodeError < StandardError; EncodeError < StandardError; the specific
// decode failures < DecodeError). Each class is registered both as a nested
// constant of JWT and under its qualified name in the top-level table (so a
// re-raised library error's exception lookup finds the same class), as the JSON
// error tree is.
func (vm *VM) registerJWTErrors(mod *RClass) {
	std := vm.consts["StandardError"].(*RClass)
	reg := func(simple, qualified string, super *RClass) *RClass {
		c := newClass(qualified, super)
		mod.consts[simple] = c
		vm.consts[qualified] = c
		return c
	}
	reg("EncodeError", "JWT::EncodeError", std)
	decode := reg("DecodeError", "JWT::DecodeError", std)
	reg("VerificationError", "JWT::VerificationError", decode)
	reg("IncorrectAlgorithm", "JWT::IncorrectAlgorithm", decode)
	reg("ExpiredSignature", "JWT::ExpiredSignature", decode)
	reg("ImmatureSignature", "JWT::ImmatureSignature", decode)
	reg("InvalidIatError", "JWT::InvalidIatError", decode)
	reg("InvalidIssuerError", "JWT::InvalidIssuerError", decode)
	reg("InvalidAudError", "JWT::InvalidAudError", decode)
	reg("InvalidSubError", "JWT::InvalidSubError", decode)
	reg("InvalidJtiError", "JWT::InvalidJtiError", decode)
	reg("InvalidPayload", "JWT::InvalidPayload", decode)
	reg("MissingRequiredClaim", "JWT::MissingRequiredClaim", decode)
	reg("Base64DecodeError", "JWT::Base64DecodeError", decode)
}

// jwtKey adapts a Ruby key to the type go-ruby-jwt signs/verifies with, keyed on
// the algorithm family. HS* takes the secret string as-is; RS*/PS*/ES* take a PEM
// string parsed into a crypto/* key (a private key for signing, a public key for
// verification, though the library accepts a private key on verify too); "none"
// and anything else pass through. A malformed PEM raises the encode/decode error.
func jwtKey(v object.Value, alg string, verify bool) any {
	if len(alg) == 0 {
		return jwtRawKey(v)
	}
	switch alg[0] {
	case 'H', 'h': // HS256/384/512 — HMAC secret string
		return jwtRawKey(v)
	case 'R', 'P', 'E', 'r', 'p', 'e': // RS/PS (RSA) and ES (ECDSA) — PEM key
		return jwtPEMKey(strArg(v), verify)
	default:
		return jwtRawKey(v)
	}
}

// jwtRawKey extracts the raw HMAC secret: a String's bytes, or nil for a nil key
// ("none"). Any other type is coerced through strArg (raising TypeError).
func jwtRawKey(v object.Value) any {
	switch k := v.(type) {
	case object.Nil:
		return nil
	case *object.String:
		return k.Str()
	default:
		return strArg(v)
	}
}

// jwtPEMKey parses a PEM-encoded RSA/ECDSA key into the crypto/* key type the
// library expects. On verify a public key is parsed (falling back to a private
// key, whose public half the library uses); on sign a private key is parsed. A
// key that does not parse raises the JWT error.
func jwtPEMKey(pemStr string, verify bool) any {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		raiseJWTError(errors.New("invalid PEM key"))
	}
	if verify {
		if k := jwtParsePublic(block); k != nil {
			return k
		}
	}
	if k := jwtParsePrivate(block); k != nil {
		return k
	}
	// A verify with only a public key available still returns it (the private parse
	// failed because it *is* a public key); otherwise the key is unusable.
	if k := jwtParsePublic(block); k != nil {
		return k
	}
	raiseJWTError(errors.New("invalid PEM key"))
	return nil
}

// jwtParsePrivate parses a PEM block as an RSA or ECDSA private key across the
// PKCS#1, SEC1 and PKCS#8 encodings, returning nil when none apply.
func jwtParsePrivate(block *pem.Block) any {
	if k, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return k
	}
	if k, err := x509.ParseECPrivateKey(block.Bytes); err == nil {
		return k
	}
	if k, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		switch key := k.(type) {
		case *rsa.PrivateKey:
			return key
		case *ecdsa.PrivateKey:
			return key
		}
	}
	return nil
}

// jwtParsePublic parses a PEM block as an RSA or ECDSA public key across the
// PKCS#1 and PKIX encodings, returning nil when none apply.
func jwtParsePublic(block *pem.Block) any {
	if k, err := x509.ParsePKCS1PublicKey(block.Bytes); err == nil {
		return k
	}
	if k, err := x509.ParsePKIXPublicKey(block.Bytes); err == nil {
		switch key := k.(type) {
		case *rsa.PublicKey:
			return key
		case *ecdsa.PublicKey:
			return key
		}
	}
	return nil
}

// jwtDecodeOpts maps a JWT.decode options Hash to the library's Options, also
// returning the first allowed algorithm (used to select the key family for PEM
// parsing). An absent Hash yields the zero Options and an empty algorithm.
func jwtDecodeOpts(rest []object.Value) (jwt.Options, string) {
	var opts jwt.Options
	h, ok := jwtLastHash(rest)
	if !ok {
		return opts, ""
	}
	if algs := jwtAlgorithms(h); len(algs) > 0 {
		opts.Algorithms = algs
	}
	if v, ok := h.Get(object.Symbol("leeway")); ok {
		opts.Leeway = intArg(v)
	}
	if v, ok := h.Get(object.Symbol("verify_expiration")); ok {
		opts.VerifyExpiration, opts.VerifyExpirationSet = v.Truthy(), true
	}
	if v, ok := h.Get(object.Symbol("verify_not_before")); ok {
		opts.VerifyNotBefore, opts.VerifyNotBeforeSet = v.Truthy(), true
	}
	if v, ok := h.Get(object.Symbol("verify_iat")); ok {
		opts.VerifyIat = v.Truthy()
	}
	if v, ok := h.Get(object.Symbol("iss")); ok {
		opts.Issuer = jwtGoScalarOrList(v)
	}
	if v, ok := h.Get(object.Symbol("verify_iss")); ok {
		opts.VerifyIss = v.Truthy()
	}
	if v, ok := h.Get(object.Symbol("aud")); ok {
		opts.Audience = jwtGoScalarOrList(v)
	}
	if v, ok := h.Get(object.Symbol("verify_aud")); ok {
		opts.VerifyAud = v.Truthy()
	}
	if v, ok := h.Get(object.Symbol("sub")); ok {
		opts.Subject, opts.VerifySub = strArg(v), true
	}
	if v, ok := h.Get(object.Symbol("required_claims")); ok {
		opts.RequiredClaims = jwtStringList(v)
	}
	alg := ""
	if len(opts.Algorithms) > 0 {
		alg = opts.Algorithms[0]
	}
	return opts, alg
}

// jwtAlgorithms reads the algorithm: (a single String) or algorithms: (an Array)
// option into the allow-list.
func jwtAlgorithms(h *object.Hash) []string {
	if v, ok := h.Get(object.Symbol("algorithm")); ok {
		return []string{strArg(v)}
	}
	if v, ok := h.Get(object.Symbol("algorithms")); ok {
		return jwtStringList(v)
	}
	return nil
}

// jwtStringList coerces an Array of Strings (or a single String) to a []string.
func jwtStringList(v object.Value) []string {
	if arr, ok := v.(*object.Array); ok {
		out := make([]string, len(arr.Elems))
		for i, e := range arr.Elems {
			out[i] = strArg(e)
		}
		return out
	}
	return []string{strArg(v)}
}

// jwtGoScalarOrList maps a Ruby String or Array-of-Strings option value to the
// Go any (string or []string) the Options issuer/audience fields accept.
func jwtGoScalarOrList(v object.Value) any {
	if arr, ok := v.(*object.Array); ok {
		return jwtStringList(arr)
	}
	return strArg(v)
}

// jwtLastHash returns the trailing options Hash of JWT.decode, or ok=false when
// the last argument is not a Hash.
func jwtLastHash(rest []object.Value) (*object.Hash, bool) {
	if len(rest) == 0 {
		return nil, false
	}
	h, ok := rest[len(rest)-1].(*object.Hash)
	return h, ok
}

// raiseJWTError re-raises a library error as the matching Ruby exception. A
// jwt.Error carries its gem class name in Kind (e.g. "JWT::ExpiredSignature"),
// which is exactly the qualified name the error tree is registered under; a plain
// error (e.g. a PEM parse failure) raises JWT::DecodeError.
func raiseJWTError(err error) {
	var je *jwt.Error
	if errors.As(err, &je) {
		raise(je.Kind, "%s", je.Message)
	}
	raise("JWT::DecodeError", "%s", err.Error())
}

// jwtFromRuby converts a Ruby value to the library's value model for encoding: a
// Hash becomes an *OrderedMap (key order preserved), an Array a []any, and
// scalars their Go equivalents. It mirrors emitValue in json_bind.go but targets
// go-ruby-jwt's json.go value shapes.
func jwtFromRuby(v object.Value) any {
	switch n := v.(type) {
	case nil, object.Nil:
		return nil
	case object.Bool:
		return bool(n)
	case object.Integer:
		return int64(n)
	case object.Float:
		return float64(n)
	case *object.String:
		return n.Str()
	case object.Symbol:
		return string(n)
	case *object.Array:
		out := make([]any, len(n.Elems))
		for i, e := range n.Elems {
			out[i] = jwtFromRuby(e)
		}
		return out
	case *object.Hash:
		m := jwt.NewOrderedMap()
		for _, k := range n.Keys {
			val, _ := n.Get(k)
			m.Set(jsonKeyString(k), jwtFromRuby(val))
		}
		return m
	default:
		return v.ToS()
	}
}

// jwtToRuby converts a decoded library value back to the Ruby object graph: an
// *OrderedMap becomes a String-keyed Hash (preserving order), a []any an Array,
// and the JSON scalars their Ruby equivalents (numbers arrive as float64 from the
// library's decoder; a whole number becomes an Integer, matching MRI's JSON).
func jwtToRuby(v any) object.Value {
	switch n := v.(type) {
	case nil:
		return object.NilV
	case bool:
		return object.Bool(n)
	case string:
		return object.NewString(n)
	case json.Number:
		// The library decodes with UseNumber(), so JSON numbers arrive as a
		// json.Number (a decimal string): an integer value becomes an Integer,
		// anything else a Float, matching MRI's JSON.parse.
		if i, err := n.Int64(); err == nil {
			return object.IntValue(i)
		}
		f, _ := n.Float64()
		return object.Float(f)
	case float64:
		if n == float64(int64(n)) {
			return object.IntValue(int64(n))
		}
		return object.Float(n)
	case int64:
		return object.IntValue(n)
	case int:
		return object.IntValue(int64(n))
	case []any:
		elems := make([]object.Value, len(n))
		for i, e := range n {
			elems[i] = jwtToRuby(e)
		}
		return &object.Array{Elems: elems}
	case *jwt.OrderedMap:
		h := object.NewHashCap(n.Len())
		for _, k := range n.Keys() {
			val, _ := n.Get(k)
			h.Set(object.NewString(k), jwtToRuby(val))
		}
		return h
	default:
		return object.NilV
	}
}
