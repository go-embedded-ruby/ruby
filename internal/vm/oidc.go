// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"

	oidc "github.com/go-ruby-oidc/oidc"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file binds github.com/go-ruby-oidc/oidc — the pure-Go (CGO-free) OpenID
// Connect layer that sits on top of the go-ruby OAuth2 and JWT building blocks —
// into rbgo as the OpenIDConnect module (require "openid_connect" / "oidc").
//
// It reuses the two siblings rbgo already binds: the code-flow token exchange is
// go-ruby-oauth2 (the same library behind rbgo's OAuth2::Client), and the ID
// token is a JWS parsed and signature-verified by go-ruby-jwt (the JWT module).
// The OIDC-specific work — discovery parsing, JWKS→key selection, and the ID
// token claim rules (iss/aud/azp/exp/iat/nbf/nonce/at_hash/c_hash) — lives in the
// oidc library and is exposed here.
//
// The network round-trip is a host seam, exactly as OAuth2's is: the oidc library
// performs every fetch (discovery / JWKS / token / userinfo) through an injectable
// Doer, and this binding wires that Doer to a Ruby callable (a Proc or any object
// responding to #call). So the transport is supplied by Ruby — mockable in tests
// and, in production, backed by a Ruby net/http doer — and the interpreter core
// never opens a socket. This mirrors how OAuth2's get_token returns an
// OAuth2::Request for the host to perform; here the seam is a Ruby object instead
// of a returned request, because the oidc library drives the multi-step flow
// (discover → JWKS → token → verify) itself.
//
// Surface note: the Ruby `openid_connect` gem's public shape (SWD discovery,
// ActiveModel-backed request objects) does not map cleanly onto the deterministic
// protocol core the oidc library provides, so this binding exposes the library's
// own faithful surface (OpenIDConnect::Client / Verifier / ProviderMetadata /
// KeySet / IDTokenClaims / Tokens / UserInfo) rather than the gem's ActiveModel
// DSL. Where a name diverges from the gem it favours the library's semantics.

// OIDCProviderMetadata wraps a parsed *oidc.ProviderMetadata
// (.well-known/openid-configuration), reported as OpenIDConnect::ProviderMetadata.
type OIDCProviderMetadata struct{ pm *oidc.ProviderMetadata }

func (m *OIDCProviderMetadata) ToS() string {
	return "#<OpenIDConnect::ProviderMetadata issuer=" + m.pm.Issuer + ">"
}
func (m *OIDCProviderMetadata) Inspect() string { return m.ToS() }
func (m *OIDCProviderMetadata) Truthy() bool    { return true }

// OIDCKeySet wraps a parsed *oidc.KeySet (a provider's JWKS), reported as
// OpenIDConnect::KeySet. It is a KeySource a Verifier resolves signing keys from.
type OIDCKeySet struct{ ks *oidc.KeySet }

func (k *OIDCKeySet) ToS() string     { return "#<OpenIDConnect::KeySet>" }
func (k *OIDCKeySet) Inspect() string { return k.ToS() }
func (k *OIDCKeySet) Truthy() bool    { return true }

// OIDCVerifier wraps a configured *oidc.Verifier (issuer/aud/keys/secret + the
// optional nonce/at_hash/c_hash inputs), reported as OpenIDConnect::Verifier.
type OIDCVerifier struct{ v *oidc.Verifier }

func (v *OIDCVerifier) ToS() string     { return "#<OpenIDConnect::Verifier>" }
func (v *OIDCVerifier) Inspect() string { return v.ToS() }
func (v *OIDCVerifier) Truthy() bool    { return true }

// OIDCClaims wraps the validated *oidc.IDTokenClaims of a verified ID token,
// reported as OpenIDConnect::IDTokenClaims.
type OIDCClaims struct{ c *oidc.IDTokenClaims }

func (c *OIDCClaims) ToS() string     { return "#<OpenIDConnect::IDTokenClaims sub=" + c.c.Subject() + ">" }
func (c *OIDCClaims) Inspect() string { return c.ToS() }
func (c *OIDCClaims) Truthy() bool    { return true }

// OIDCClient wraps an *oidc.Client driving the Authorization-Code + PKCE flow,
// reported as OpenIDConnect::Client.
type OIDCClient struct{ c *oidc.Client }

func (c *OIDCClient) ToS() string     { return "#<OpenIDConnect::Client>" }
func (c *OIDCClient) Inspect() string { return c.ToS() }
func (c *OIDCClient) Truthy() bool    { return true }

// OIDCTokens wraps the *oidc.Tokens result of a successful code exchange
// (access token + raw ID token + validated claims), reported as
// OpenIDConnect::Tokens.
type OIDCTokens struct{ t *oidc.Tokens }

func (t *OIDCTokens) ToS() string     { return "#<OpenIDConnect::Tokens>" }
func (t *OIDCTokens) Inspect() string { return t.ToS() }
func (t *OIDCTokens) Truthy() bool    { return true }

// OIDCUserInfo wraps a parsed *oidc.UserInfo (the userinfo endpoint claims),
// reported as OpenIDConnect::UserInfo.
type OIDCUserInfo struct{ u *oidc.UserInfo }

func (u *OIDCUserInfo) ToS() string     { return "#<OpenIDConnect::UserInfo sub=" + u.u.Subject() + ">" }
func (u *OIDCUserInfo) Inspect() string { return u.ToS() }
func (u *OIDCUserInfo) Truthy() bool    { return true }

// oidcDoer adapts a Ruby callable (a Proc or any object responding to #call) to
// the oidc.Doer HTTP seam. Do renders the built request as a Ruby Hash (String
// keys: "method"/"url"/"headers"/"body"), invokes the callable, and reads the
// returned Ruby Hash back as the raw response ("status"/"headers"/"body"). A
// return value that is not a Hash raises OpenIDConnect::HTTPError.
type oidcDoer struct {
	vm       *VM
	callable object.Value
}

// Do performs one round-trip through the Ruby callable.
func (d oidcDoer) Do(req *oidc.HTTPRequest) (*oidc.HTTPResponse, error) {
	reqH := object.NewHash()
	reqH.Set(object.NewString("method"), object.NewString(req.Method))
	reqH.Set(object.NewString("url"), object.NewString(req.URL))
	hh := object.NewHash()
	for k, v := range req.Header {
		hh.Set(object.NewString(k), object.NewString(v))
	}
	reqH.Set(object.NewString("headers"), hh)
	reqH.Set(object.NewString("body"), object.NewString(req.Body))

	res := d.vm.send(d.callable, "call", []object.Value{reqH}, nil)
	rh, ok := res.(*object.Hash)
	if !ok {
		raise("OpenIDConnect::HTTPError", "HTTP doer must return a Hash, got %s", res.Inspect())
	}
	resp := &oidc.HTTPResponse{Header: map[string]string{}}
	if v, ok := rh.Get(object.NewString("status")); ok {
		resp.Status = int(intArg(v))
	}
	if v, ok := rh.Get(object.NewString("body")); ok {
		resp.Body = oidcStr(v)
	}
	if v, ok := rh.Get(object.NewString("headers")); ok {
		if hm, ok := v.(*object.Hash); ok {
			for _, k := range hm.Keys {
				val, _ := hm.Get(k)
				resp.Header[oidcName(k)] = oidcStr(val)
			}
		}
	}
	return resp, nil
}

// registerOIDC installs the OpenIDConnect module (require "openid_connect" /
// "oidc"): the error tree, OpenIDConnect.discover, ProviderMetadata, KeySet,
// Verifier, IDTokenClaims, Client, Tokens and UserInfo. Every fetch is performed
// by a Ruby callable Doer, so the module is fully exercisable without a socket.
func (vm *VM) registerOIDC() {
	mod := newClass("OpenIDConnect", nil)
	mod.isModule = true
	vm.consts["OpenIDConnect"] = mod

	vm.registerOIDCErrors(mod)

	// OpenIDConnect.discover(issuer, doer) fetches and parses a provider's
	// configuration document over the Ruby HTTP seam.
	mod.smethods["discover"] = &Method{name: "discover", owner: mod,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			if len(args) < 2 {
				raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
			}
			pm, err := oidc.Discover(oidcDoer{vm: vm, callable: args[1]}, strArg(args[0]))
			if err != nil {
				raiseOIDCError(err)
			}
			return &OIDCProviderMetadata{pm: pm}
		}}

	vm.registerOIDCProviderMetadata(mod)
	vm.registerOIDCKeySet(mod)
	vm.registerOIDCClaims(mod)
	vm.registerOIDCVerifier(mod)
	vm.registerOIDCUserInfo(mod)
	vm.registerOIDCTokens(mod)
	vm.registerOIDCClient(mod)
}

// registerOIDCErrors installs OpenIDConnect::Error < StandardError and one
// subclass per library error Kind (OpenIDConnect::<Kind>Error), registered both as
// a nested constant and under the qualified name so a re-raised library error and
// the Ruby constant resolve to the same class.
func (vm *VM) registerOIDCErrors(mod *RClass) {
	std := vm.consts["StandardError"].(*RClass)
	base := newClass("OpenIDConnect::Error", std)
	mod.consts["Error"] = base
	vm.consts["OpenIDConnect::Error"] = base
	for _, kind := range oidcErrorKinds {
		c := newClass("OpenIDConnect::"+kind+"Error", base)
		mod.consts[kind+"Error"] = c
		vm.consts["OpenIDConnect::"+kind+"Error"] = c
	}
}

// oidcErrorKinds are the library's error Kind names (oidc.Error.Kind), each mapped
// to an OpenIDConnect::<Kind>Error class.
var oidcErrorKinds = []string{
	"Discovery", "JWKS", "HTTP", "InvalidToken", "InvalidIssuer",
	"InvalidAudience", "InvalidAzp", "Expired", "InvalidIat", "NotYetValid",
	"InvalidNonce", "InvalidHash", "Config", "NoIDToken", "Token", "UserInfo",
}

// raiseOIDCError re-raises a library error as the matching Ruby exception. An
// *oidc.Error carries its category in Kind (e.g. "InvalidNonce"), mapped to
// OpenIDConnect::InvalidNonceError; any other error raises OpenIDConnect::Error.
// It never returns (raise panics); the any return lets callers write it in a
// value position.
func raiseOIDCError(err error) any {
	var oe *oidc.Error
	if errors.As(err, &oe) {
		return raise("OpenIDConnect::"+oe.Kind+"Error", "%s", oe.Message)
	}
	return raise("OpenIDConnect::Error", "%s", err.Error())
}

// registerOIDCProviderMetadata installs OpenIDConnect::ProviderMetadata:
// ParseProviderMetadata via .parse, the promoted endpoint/string readers, the
// array readers, [] (raw member) and to_h (the whole decoded document).
func (vm *VM) registerOIDCProviderMetadata(mod *RClass) {
	cls := newClass("OpenIDConnect::ProviderMetadata", vm.cObject)
	mod.consts["ProviderMetadata"] = cls
	vm.consts["OpenIDConnect::ProviderMetadata"] = cls

	cls.smethods["parse"] = &Method{name: "parse", owner: cls,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			if len(args) == 0 {
				raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
			}
			pm, err := oidc.ParseProviderMetadata([]byte(strArg(args[0])))
			if err != nil {
				raiseOIDCError(err)
			}
			return &OIDCProviderMetadata{pm: pm}
		}}

	str := func(name string, get func(*oidc.ProviderMetadata) string) {
		cls.define(name, func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
			return object.NewString(get(self.(*OIDCProviderMetadata).pm))
		})
	}
	str("issuer", func(p *oidc.ProviderMetadata) string { return p.Issuer })
	str("authorization_endpoint", func(p *oidc.ProviderMetadata) string { return p.AuthorizationEndpoint })
	str("token_endpoint", func(p *oidc.ProviderMetadata) string { return p.TokenEndpoint })
	str("userinfo_endpoint", func(p *oidc.ProviderMetadata) string { return p.UserinfoEndpoint })
	str("jwks_uri", func(p *oidc.ProviderMetadata) string { return p.JWKSURI })
	str("registration_endpoint", func(p *oidc.ProviderMetadata) string { return p.RegistrationEndpoint })
	str("end_session_endpoint", func(p *oidc.ProviderMetadata) string { return p.EndSessionEndpoint })

	list := func(name string, get func(*oidc.ProviderMetadata) []string) {
		cls.define(name, func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
			return oidcStringArray(get(self.(*OIDCProviderMetadata).pm))
		})
	}
	list("scopes_supported", func(p *oidc.ProviderMetadata) []string { return p.ScopesSupported })
	list("response_types_supported", func(p *oidc.ProviderMetadata) []string { return p.ResponseTypesSupported })
	list("response_modes_supported", func(p *oidc.ProviderMetadata) []string { return p.ResponseModesSupported })
	list("grant_types_supported", func(p *oidc.ProviderMetadata) []string { return p.GrantTypesSupported })
	list("subject_types_supported", func(p *oidc.ProviderMetadata) []string { return p.SubjectTypesSupported })
	list("id_token_signing_alg_values_supported", func(p *oidc.ProviderMetadata) []string { return p.IDTokenSigningAlgValuesSupported })
	list("claims_supported", func(p *oidc.ProviderMetadata) []string { return p.ClaimsSupported })
	list("code_challenge_methods_supported", func(p *oidc.ProviderMetadata) []string { return p.CodeChallengeMethodsSupported })

	cls.define("[]", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		v, ok := self.(*OIDCProviderMetadata).pm.Raw[oidcName(args[0])]
		if !ok {
			return object.NilV
		}
		return oidcAnyToRuby(v)
	})
	cls.define("to_h", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return oidcMapToRuby(self.(*OIDCProviderMetadata).pm.Raw)
	})
}

// registerOIDCKeySet installs OpenIDConnect::KeySet: ParseJWKS via .parse, plus
// #kids (the provider-assigned key ids) and #size. A KeySet is passed as the
// keys: option of a Verifier.
func (vm *VM) registerOIDCKeySet(mod *RClass) {
	cls := newClass("OpenIDConnect::KeySet", vm.cObject)
	mod.consts["KeySet"] = cls
	vm.consts["OpenIDConnect::KeySet"] = cls

	cls.smethods["parse"] = &Method{name: "parse", owner: cls,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			if len(args) == 0 {
				raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
			}
			ks, err := oidc.ParseJWKS([]byte(strArg(args[0])))
			if err != nil {
				raiseOIDCError(err)
			}
			return &OIDCKeySet{ks: ks}
		}}

	cls.define("kids", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		keys := self.(*OIDCKeySet).ks.Keys()
		out := make([]object.Value, len(keys))
		for i, k := range keys {
			out[i] = object.NewString(k.Kid)
		}
		return object.NewArrayFromSlice(out)
	})
	cls.define("size", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(len(self.(*OIDCKeySet).ks.Keys())))
	})
}
