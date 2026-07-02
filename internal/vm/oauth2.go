// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	oauth2 "github.com/go-ruby-oauth2/oauth2"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// OAuth2Client wraps a *oauth2.Client as a Ruby OAuth2::Client. The URL and
// request construction live in the github.com/go-ruby-oauth2/oauth2 library — the
// pure-Go core of the `oauth2` gem — and this shell reports the Ruby class (via
// classOf) and exposes the grant strategies. The HTTP round-trip itself is a host
// seam: get_token builds an OAuth2::Request the host performs via net/http.
type OAuth2Client struct{ c *oauth2.Client }

func (c *OAuth2Client) ToS() string     { return "#<OAuth2::Client site=" + c.c.TokenURL() + ">" }
func (c *OAuth2Client) Inspect() string { return c.ToS() }
func (c *OAuth2Client) Truthy() bool    { return true }

// OAuth2Strategy wraps a grant strategy (auth_code / password / client_credentials
// / refresh / assertion). className is the Ruby class it reports, keyed by grant.
type OAuth2Strategy struct {
	client    *oauth2.Client
	grant     string
	className string
}

func (s *OAuth2Strategy) ToS() string     { return "#<" + s.className + ">" }
func (s *OAuth2Strategy) Inspect() string { return s.ToS() }
func (s *OAuth2Strategy) Truthy() bool    { return true }

// OAuth2AccessToken wraps a *oauth2.AccessToken (token / refresh_token / expired? /
// to_hash), bound to its issuing client so #refresh can build the refresh request.
type OAuth2AccessToken struct{ t *oauth2.AccessToken }

func (t *OAuth2AccessToken) ToS() string     { return t.t.Token }
func (t *OAuth2AccessToken) Inspect() string { return "#<OAuth2::AccessToken>" }
func (t *OAuth2AccessToken) Truthy() bool    { return true }

// OAuth2Response wraps a *oauth2.Response (status / body / parsed).
type OAuth2Response struct{ r *oauth2.Response }

func (r *OAuth2Response) ToS() string     { return "#<OAuth2::Response>" }
func (r *OAuth2Response) Inspect() string { return r.ToS() }
func (r *OAuth2Response) Truthy() bool    { return true }

// OAuth2Request wraps a *oauth2.Request — the deterministic token round-trip
// specification the host performs (method / url / body / headers).
type OAuth2Request struct{ r *oauth2.Request }

func (r *OAuth2Request) ToS() string     { return r.r.Method + " " + r.r.FullURL() }
func (r *OAuth2Request) Inspect() string { return "#<OAuth2::Request>" }
func (r *OAuth2Request) Truthy() bool    { return true }

// registerOAuth2 installs the OAuth2 module (require "oauth2"): OAuth2::Client.new,
// the grant strategies (auth_code / password / client_credentials / refresh), their
// authorize_url / get_token request building, OAuth2::AccessToken, OAuth2::Response
// parsing, OAuth2::Error, and the PKCE code-challenge helper. The HTTP transport is
// a host seam; get_token returns the OAuth2::Request to perform.
func (vm *VM) registerOAuth2() {
	mod := newClass("OAuth2", nil)
	mod.isModule = true
	vm.consts["OAuth2"] = mod

	vm.registerOAuth2Error(mod)
	vm.registerOAuth2Value(mod)

	clientCls := newClass("OAuth2::Client", vm.cObject)
	mod.consts["Client"] = clientCls
	vm.consts["OAuth2::Client"] = clientCls

	// OAuth2::Client.new(id, secret, site:, authorize_url:, token_url:,
	// auth_scheme:, token_method:) builds a client. A trailing Hash supplies the
	// keyword options, matching the gem.
	clientCls.smethods["new"] = &Method{name: "new", owner: clientCls,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			if len(args) < 2 {
				raise("ArgumentError", "wrong number of arguments (given %d, expected 2..)", len(args))
			}
			id := strArg(args[0])
			secret := strArg(args[1])
			opts := oauth2Options(args[2:])
			return &OAuth2Client{c: oauth2.NewClient(id, secret, opts)}
		}}

	vm.registerOAuth2Client(clientCls)
	vm.registerOAuth2Strategy(mod)
	vm.registerOAuth2AccessToken(mod)

	// OAuth2::PKCE.code_challenge(verifier, method) computes the RFC 7636 code
	// challenge (S256 default).
	pkce := newClass("OAuth2::PKCE", vm.cObject)
	pkce.isModule = true
	mod.consts["PKCE"] = pkce
	vm.consts["OAuth2::PKCE"] = pkce
	pkce.smethods["code_challenge"] = &Method{name: "code_challenge", owner: pkce,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			if len(args) == 0 {
				raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
			}
			method := oauth2.PKCES256
			if len(args) > 1 && oauth2Name(args[1]) == "plain" {
				method = oauth2.PKCEPlain
			}
			return object.NewString(oauth2.CodeChallenge(strArg(args[0]), method))
		}}
}

// oauth2Options maps a trailing keyword Hash into oauth2.Options. Recognised keys
// (Symbol or String): site, authorize_url, token_url, auth_scheme, token_method.
func oauth2Options(args []object.Value) oauth2.Options {
	o := oauth2.Options{}
	if len(args) == 0 {
		return o
	}
	h, ok := args[0].(*object.Hash)
	if !ok {
		return o
	}
	for _, k := range h.Keys {
		v, _ := h.Get(k)
		switch oauth2Name(k) {
		case "site":
			o.Site = oauth2Str(v)
		case "authorize_url":
			o.AuthorizeURL = oauth2Str(v)
		case "token_url":
			o.TokenURL = oauth2Str(v)
		case "auth_scheme":
			o.AuthScheme = oauth2.AuthScheme(oauth2Str(v))
		case "token_method":
			o.TokenMethod = oauth2.TokenMethod(oauth2Str(v))
		}
	}
	return o
}

// registerOAuth2Error installs OAuth2::Error < StandardError, registered both as a
// nested constant and under its qualified name so a re-raised library error and the
// Ruby constant resolve to the same class.
func (vm *VM) registerOAuth2Error(mod *RClass) {
	std := vm.consts["StandardError"].(*RClass)
	c := newClass("OAuth2::Error", std)
	mod.consts["Error"] = c
	vm.consts["OAuth2::Error"] = c
}
