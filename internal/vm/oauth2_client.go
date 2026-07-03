// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	oauth2 "github.com/go-ruby-oauth2/oauth2"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerOAuth2Client installs the OAuth2::Client instance surface: the grant
// strategy accessors (auth_code / password / client_credentials / refresh /
// assertion), authorize_url, id / site / token_url, and get_token / parse_token,
// which map a token Response into an OAuth2::AccessToken.
func (vm *VM) registerOAuth2Client(cls *RClass) {
	d := func(name string, fn NativeFn) { cls.define(name, fn) }

	strat := func(grant, className string) NativeFn {
		return func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
			return object.Wrap(&OAuth2Strategy{client: object.Kind[*OAuth2Client](self).c, grant: grant, className: className})
		}
	}
	d("auth_code", strat("auth_code", "OAuth2::Strategy::AuthCode"))
	d("password", strat("password", "OAuth2::Strategy::Password"))
	d("client_credentials", strat("client_credentials", "OAuth2::Strategy::ClientCredentials"))
	d("refresh", strat("refresh", "OAuth2::Strategy::Refresh"))
	d("assertion", strat("assertion", "OAuth2::Strategy::Assertion"))

	d("id", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(object.Kind[*OAuth2Client](self).c.ID()))
	})
	d("authorize_url", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(object.Kind[*OAuth2Client](self).c.AuthorizeURL()))
	})
	d("token_url", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(object.Kind[*OAuth2Client](self).c.TokenURL()))
	})

	// get_token(response) / parse_token parses a token Response into an
	// OAuth2::AccessToken, raising OAuth2::Error on an error response — mirroring
	// the gem's get_token once the host has performed the round-trip.
	parse := func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		resp, ok := object.KindOK[*OAuth2Response](args[0])
		if !ok {
			raise("TypeError", "no implicit conversion of %s into OAuth2::Response", args[0].Inspect())
		}
		tok, err := object.Kind[*OAuth2Client](self).c.ParseToken(resp.r)
		if err != nil {
			raise("OAuth2::Error", "%s", err.Error())
		}
		return object.Wrap(&OAuth2AccessToken{t: tok})
	}
	d("get_token", parse)
	d("parse_token", parse)
}

// registerOAuth2Strategy installs the grant-strategy surface under
// OAuth2::Strategy: authorize_url (auth_code only) and get_token, which returns the
// OAuth2::Request the host performs — the request-building half of get_token.
func (vm *VM) registerOAuth2Strategy(mod *RClass) {
	ns := newClass("OAuth2::Strategy", vm.cObject)
	ns.isModule = true
	mod.consts["Strategy"] = object.Wrap(ns)
	vm.consts["OAuth2::Strategy"] = object.Wrap(ns)

	cls := newClass("OAuth2::Strategy::Base", vm.cObject)
	// Each grant reports a distinct class name for classOf; register them all as
	// the same behavioural class so their methods dispatch.
	for _, name := range []string{"AuthCode", "Password", "ClientCredentials", "Refresh", "Assertion"} {
		sub := newClass("OAuth2::Strategy::"+name, cls)
		ns.consts[name] = object.Wrap(sub)
		vm.consts["OAuth2::Strategy::"+name] = object.Wrap(sub)
		vm.registerOAuth2StrategyMethods(sub)
	}
}

// registerOAuth2StrategyMethods installs authorize_url and get_token on a strategy
// class. authorize_url is meaningful for the auth_code grant; get_token builds the
// grant's token Request.
func (vm *VM) registerOAuth2StrategyMethods(cls *RClass) {
	cls.define("authorize_url", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		s := object.Kind[*OAuth2Strategy](self)
		if s.grant != "auth_code" {
			raise("NoMethodError", "undefined method `authorize_url' for %s", s.className)
		}
		return object.Wrap(object.NewString(s.client.AuthCode().AuthorizeURL(oauth2Params(args))))
	})
	cls.define("get_token", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Wrap(&OAuth2Request{r: oauth2GetTokenRequest(object.Kind[*OAuth2Strategy](self), args)})
	})
	cls.define("token_url", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(object.Kind[*OAuth2Strategy](self).client.TokenURL()))
	})
}

// oauth2GetTokenRequest builds the token Request for a strategy from its
// positional arguments (a trailing Hash supplies extra params): auth_code takes a
// code, password a username+password, refresh a refresh_token, assertion a
// grant_type+assertion, and client_credentials none.
func oauth2GetTokenRequest(s *OAuth2Strategy, args []object.Value) *oauth2.Request {
	extra := oauth2ExtraParams(args)
	switch s.grant {
	case "auth_code":
		return s.client.AuthCode().GetTokenRequest(oauth2ArgAt(args, 0), extra)
	case "password":
		return s.client.Password().GetTokenRequest(oauth2ArgAt(args, 0), oauth2ArgAt(args, 1), extra)
	case "refresh":
		return s.client.Refresh().GetTokenRequest(oauth2ArgAt(args, 0), extra)
	case "assertion":
		return s.client.Assertion().GetTokenRequest(oauth2ArgAt(args, 0), oauth2ArgAt(args, 1), extra)
	default: // client_credentials
		return s.client.ClientCredentials().GetTokenRequest(extra)
	}
}

// oauth2ArgAt reads the i-th positional argument as a string, skipping a trailing
// options Hash; a missing argument yields "".
func oauth2ArgAt(args []object.Value, i int) string {
	n := 0
	for _, a := range args {
		if _, ok := object.KindOK[*object.Hash](a); ok {
			continue
		}
		if n == i {
			return oauth2Str(a)
		}
		n++
	}
	return ""
}
