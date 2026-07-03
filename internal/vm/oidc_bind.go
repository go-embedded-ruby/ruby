// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"encoding/json"
	"sort"
	"time"

	oauth2 "github.com/go-ruby-oauth2/oauth2"
	oidc "github.com/go-ruby-oidc/oidc"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file installs the OpenIDConnect Verifier / IDTokenClaims / Client / Tokens
// / UserInfo surfaces and the argument bridges (keyword reading, seconds→Duration,
// value coercion) between rbgo's object graph and github.com/go-ruby-oidc/oidc.

// registerOIDCVerifier installs OpenIDConnect::Verifier.new(issuer:, client_id:,
// keys:, hmac_secret:, nonce:, leeway:, algorithms:, access_token:, code:) and
// #verify(id_token), which validates the ID token's signature (via go-ruby-jwt)
// and the OIDC claim rules, returning IDTokenClaims or raising the matching
// OpenIDConnect error.
func (vm *VM) registerOIDCVerifier(mod *RClass) {
	cls := newClass("OpenIDConnect::Verifier", vm.cObject)
	mod.consts["Verifier"] = cls
	vm.consts["OpenIDConnect::Verifier"] = cls

	cls.smethods["new"] = &Method{name: "new", owner: cls,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			return &OIDCVerifier{v: oidcBuildVerifier(args)}
		}}

	cls.define("verify", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		claims, err := self.(*OIDCVerifier).v.Verify(strArg(args[0]))
		if err != nil {
			raiseOIDCError(err)
		}
		return &OIDCClaims{c: claims}
	})
}

// oidcBuildVerifier constructs an *oidc.Verifier from a keyword Hash. issuer /
// client_id default to "" (validated at verify time); keys is an
// OpenIDConnect::KeySet; hmac_secret is the HS shared secret; leeway is seconds.
func oidcBuildVerifier(args []object.Value) *oidc.Verifier {
	v := &oidc.Verifier{
		Issuer:      oidcStrKw(args, "issuer", ""),
		ClientID:    oidcStrKw(args, "client_id", ""),
		Nonce:       oidcStrKw(args, "nonce", ""),
		AccessToken: oidcStrKw(args, "access_token", ""),
		Code:        oidcStrKw(args, "code", ""),
	}
	if kv, ok := oidcKw(args, "keys"); ok && kv != object.NilV {
		ks, ok := kv.(*OIDCKeySet)
		if !ok {
			raise("TypeError", "no implicit conversion of %s into OpenIDConnect::KeySet", kv.Inspect())
		}
		v.Keys = ks.ks
	}
	if sv, ok := oidcKw(args, "hmac_secret"); ok && sv != object.NilV {
		v.HMACSecret = []byte(strArg(sv))
	}
	if lv, ok := oidcKw(args, "leeway"); ok && lv != object.NilV {
		v.Leeway = oidcSeconds(lv)
	}
	if av, ok := oidcKw(args, "algorithms"); ok && av != object.NilV {
		v.Algorithms = oidcStringList(av)
	}
	return v
}

// registerOIDCClaims installs OpenIDConnect::IDTokenClaims: the typed OIDC claim
// readers (issuer / subject / audience / nonce / expires_at / issued_at), [] (a
// raw claim) and to_h (the whole claim set).
func (vm *VM) registerOIDCClaims(mod *RClass) {
	cls := newClass("OpenIDConnect::IDTokenClaims", vm.cObject)
	mod.consts["IDTokenClaims"] = cls
	vm.consts["OpenIDConnect::IDTokenClaims"] = cls

	cls.define("issuer", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self.(*OIDCClaims).c.Issuer())
	})
	cls.define("subject", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self.(*OIDCClaims).c.Subject())
	})
	cls.define("nonce", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self.(*OIDCClaims).c.Nonce())
	})
	cls.define("audience", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return oidcStringArray(self.(*OIDCClaims).c.Audience())
	})
	cls.define("expires_at", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(self.(*OIDCClaims).c.ExpiresAt())
	})
	cls.define("issued_at", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(self.(*OIDCClaims).c.IssuedAt())
	})
	cls.define("[]", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		v, ok := self.(*OIDCClaims).c.Get(oidcName(args[0]))
		if !ok {
			return object.NilV
		}
		return jwtToRuby(v)
	})
	cls.define("to_h", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return jwtToRuby(self.(*OIDCClaims).c.Raw())
	})
}

// registerOIDCUserInfo installs OpenIDConnect::UserInfo: subject, [] (a raw claim)
// and to_h.
func (vm *VM) registerOIDCUserInfo(mod *RClass) {
	cls := newClass("OpenIDConnect::UserInfo", vm.cObject)
	mod.consts["UserInfo"] = cls
	vm.consts["OpenIDConnect::UserInfo"] = cls

	cls.define("subject", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self.(*OIDCUserInfo).u.Subject())
	})
	cls.define("[]", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		v, ok := self.(*OIDCUserInfo).u.Get(oidcName(args[0]))
		if !ok {
			return object.NilV
		}
		return oidcAnyToRuby(v)
	})
	cls.define("to_h", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return oidcMapToRuby(self.(*OIDCUserInfo).u.Raw())
	})
}

// registerOIDCTokens installs OpenIDConnect::Tokens: access_token (the raw OAuth2
// access token string), id_token (the raw compact JWT) and claims (the validated
// IDTokenClaims).
func (vm *VM) registerOIDCTokens(mod *RClass) {
	cls := newClass("OpenIDConnect::Tokens", vm.cObject)
	mod.consts["Tokens"] = cls
	vm.consts["OpenIDConnect::Tokens"] = cls

	cls.define("access_token", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self.(*OIDCTokens).t.Access.Token)
	})
	cls.define("id_token", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self.(*OIDCTokens).t.IDToken)
	})
	cls.define("claims", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return &OIDCClaims{c: self.(*OIDCTokens).t.Claims}
	})
}

// registerOIDCClient installs OpenIDConnect::Client: .new (from a discovered
// ProviderMetadata) and .discover (issuer → configured client), plus the flow
// methods authorization_url, exchange, user_info and verifier.
func (vm *VM) registerOIDCClient(mod *RClass) {
	cls := newClass("OpenIDConnect::Client", vm.cObject)
	mod.consts["Client"] = cls
	vm.consts["OpenIDConnect::Client"] = cls

	cls.smethods["new"] = &Method{name: "new", owner: cls,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			cfg := oidcBuildConfig(vm, args)
			if mv, ok := oidcKw(args, "metadata"); ok && mv != object.NilV {
				pm, ok := mv.(*OIDCProviderMetadata)
				if !ok {
					raise("TypeError", "no implicit conversion of %s into OpenIDConnect::ProviderMetadata", mv.Inspect())
				}
				cfg.Metadata = pm.pm
			}
			c, err := oidc.NewClient(cfg)
			if err != nil {
				raiseOIDCError(err)
			}
			return &OIDCClient{c: c}
		}}

	cls.smethods["discover"] = &Method{name: "discover", owner: cls,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			if len(args) == 0 {
				raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
			}
			c, err := oidc.DiscoverClient(oidcBuildConfig(vm, args), strArg(args[0]))
			if err != nil {
				raiseOIDCError(err)
			}
			return &OIDCClient{c: c}
		}}

	cls.define("authorization_url", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(self.(*OIDCClient).c.AuthCodeURL(oidcAuthParams(args)))
	})
	cls.define("exchange", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		toks, err := self.(*OIDCClient).c.Exchange(
			strArg(args[0]),
			oidcStrKw(args, "code_verifier", ""),
			oidcStrKw(args, "nonce", ""),
		)
		if err != nil {
			raiseOIDCError(err)
		}
		return &OIDCTokens{t: toks}
	})
	cls.define("user_info", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		ui, err := self.(*OIDCClient).c.UserInfo(strArg(args[0]))
		if err != nil {
			raiseOIDCError(err)
		}
		return &OIDCUserInfo{u: ui}
	})
	cls.define("verifier", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return &OIDCVerifier{v: self.(*OIDCClient).c.Verifier()}
	})
}

// oidcBuildConfig maps the shared client keyword options (client_id / client_secret
// / redirect_uri / scopes / doer / leeway / jwks_ttl) into an oidc.Config. Metadata
// is set by the caller (.new sets it from a ProviderMetadata; .discover fetches it).
// A doer callable, when present, is wrapped as the HTTP seam; an absent one leaves
// Doer nil so the library raises a Config error.
func oidcBuildConfig(vm *VM, args []object.Value) oidc.Config {
	cfg := oidc.Config{
		ClientID:     oidcStrKw(args, "client_id", ""),
		ClientSecret: oidcStrKw(args, "client_secret", ""),
		RedirectURI:  oidcStrKw(args, "redirect_uri", ""),
	}
	if sv, ok := oidcKw(args, "scopes"); ok && sv != object.NilV {
		cfg.Scopes = oidcStringList(sv)
	}
	if dv, ok := oidcKw(args, "doer"); ok && dv != object.NilV {
		cfg.Doer = oidcDoer{vm: vm, callable: dv}
	}
	if lv, ok := oidcKw(args, "leeway"); ok && lv != object.NilV {
		cfg.Leeway = oidcSeconds(lv)
	}
	if tv, ok := oidcKw(args, "jwks_ttl"); ok && tv != object.NilV {
		cfg.JWKSTTL = oidcSeconds(tv)
	}
	return cfg
}

// oidcAuthParams maps the authorization_url keyword Hash into oidc.AuthParams. The
// recognised keys (state / nonce / code_verifier / scopes) are promoted; every
// other key is carried through as an extra authorization parameter (prompt,
// login_hint, …) in Hash order.
func oidcAuthParams(args []object.Value) oidc.AuthParams {
	p := oidc.AuthParams{
		State:        oidcStrKw(args, "state", ""),
		Nonce:        oidcStrKw(args, "nonce", ""),
		CodeVerifier: oidcStrKw(args, "code_verifier", ""),
	}
	if sv, ok := oidcKw(args, "scopes"); ok && sv != object.NilV {
		p.Scopes = oidcStringList(sv)
	}
	if h := oidcLastHash(args); h != nil {
		for _, k := range h.Keys {
			name := oidcName(k)
			switch name {
			case "state", "nonce", "code_verifier", "scopes":
				continue
			}
			v, _ := h.Get(k)
			p.Extra = append(p.Extra, oauth2.Param{Key: name, Val: oidcStr(v)})
		}
	}
	return p
}

// --- bridges ----------------------------------------------------------------

// oidcLastHash returns the trailing keyword Hash of a call, or nil when the last
// argument is not a Hash.
func oidcLastHash(args []object.Value) *object.Hash {
	if len(args) == 0 {
		return nil
	}
	h, _ := args[len(args)-1].(*object.Hash)
	return h
}

// oidcKw reads a keyword (Symbol key) from the trailing options Hash.
func oidcKw(args []object.Value, name string) (object.Value, bool) {
	h := oidcLastHash(args)
	if h == nil {
		return nil, false
	}
	return h.Get(object.Symbol(name))
}

// oidcStrKw reads a String keyword, returning def when it is absent or nil.
func oidcStrKw(args []object.Value, name, def string) string {
	if v, ok := oidcKw(args, name); ok && v != object.NilV {
		return strArg(v)
	}
	return def
}

// oidcSeconds coerces a seconds value (Integer or Float) to a time.Duration.
func oidcSeconds(v object.Value) time.Duration {
	switch n := v.(type) {
	case object.Integer:
		return time.Duration(n) * time.Second
	case object.Float:
		return time.Duration(float64(n) * float64(time.Second))
	default:
		raise("TypeError", "no implicit conversion of %s into a seconds number", v.Inspect())
		return 0
	}
}

// oidcStringList coerces an Array of Strings (or a single String) to a []string.
func oidcStringList(v object.Value) []string {
	if arr, ok := v.(*object.Array); ok {
		out := make([]string, len(arr.Elems))
		for i, e := range arr.Elems {
			out[i] = strArg(e)
		}
		return out
	}
	return []string{strArg(v)}
}

// oidcStringArray renders a []string as a Ruby Array of Strings.
func oidcStringArray(ss []string) object.Value {
	out := make([]object.Value, len(ss))
	for i, s := range ss {
		out[i] = object.NewString(s)
	}
	return object.NewArrayFromSlice(out)
}

// oidcName renders a key (Symbol or String) as its bare name.
func oidcName(v object.Value) string {
	switch n := v.(type) {
	case object.Symbol:
		return string(n)
	case *object.String:
		return n.Str()
	}
	return v.ToS()
}

// oidcStr renders a value as its string form: a String verbatim, anything else its
// to_s.
func oidcStr(v object.Value) string {
	if s, ok := v.(*object.String); ok {
		return s.Str()
	}
	return v.ToS()
}

// oidcMapToRuby maps a decoded JSON object (map[string]any) to a Ruby Hash, sorting
// keys for a deterministic order (the discovery/userinfo documents decode into an
// unordered map).
func oidcMapToRuby(m map[string]any) object.Value {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	h := object.NewHashCap(len(keys))
	for _, k := range keys {
		h.Set(object.NewString(k), oidcAnyToRuby(m[k]))
	}
	return h
}

// oidcAnyToRuby maps a decoded JSON value into the rbgo object graph: scalars, a
// json.Number (whole → Integer, else Float), nested objects (sorted) and arrays.
func oidcAnyToRuby(v any) object.Value {
	switch n := v.(type) {
	case nil:
		return object.NilV
	case bool:
		return object.Bool(n)
	case string:
		return object.NewString(n)
	case json.Number:
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
		arr := object.NewArrayFromSlice(make([]object.Value, len(n)))
		for i, el := range n {
			arr.Elems[i] = oidcAnyToRuby(el)
		}
		return arr
	case map[string]any:
		return oidcMapToRuby(n)
	}
	return object.NilV
}
