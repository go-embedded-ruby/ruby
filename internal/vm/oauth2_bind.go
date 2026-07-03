// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"sort"

	oauth2 "github.com/go-ruby-oauth2/oauth2"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file installs the OAuth2::AccessToken / OAuth2::Response / OAuth2::Request
// value surfaces and the small argument bridges (Params ordering, Response
// construction, string coercion) between rbgo's object graph and the
// github.com/go-ruby-oauth2/oauth2 library.

// registerOAuth2AccessToken installs OAuth2::AccessToken: .new / .from_hash, plus
// token / refresh_token / expires? / expired? / token_type / scope / to_hash / []
// / refresh (which rebuilds the refresh Request).
func (vm *VM) registerOAuth2AccessToken(mod *RClass) {
	cls := newClass("OAuth2::AccessToken", vm.cObject)
	mod.consts["AccessToken"] = object.Wrap(cls)
	vm.consts["OAuth2::AccessToken"] = object.Wrap(cls)

	// AccessToken.new(client, token, params = {}) builds a token bound to client.
	cls.smethods["new"] = &Method{name: "new", owner: cls,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			if len(args) < 2 {
				raise("ArgumentError", "wrong number of arguments (given %d, expected 2..)", len(args))
			}
			client, ok := object.KindOK[*OAuth2Client](args[0])
			if !ok {
				raise("TypeError", "no implicit conversion of %s into OAuth2::Client", args[0].Inspect())
			}
			tok := oauth2.NewAccessToken(client.c, strArg(args[1]))
			if len(args) > 2 {
				if h, ok := object.KindOK[*object.Hash](args[2]); ok {
					for _, k := range h.Keys {
						v, _ := h.Get(k)
						tok.Params.Set(oauth2Name(k), oauth2Str(v))
					}
				}
			}
			return object.Wrap(&OAuth2AccessToken{t: tok})
		}}
	// AccessToken.from_hash(client, hash) rebuilds a token from its serialised form.
	cls.smethods["from_hash"] = &Method{name: "from_hash", owner: cls,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			if len(args) < 2 {
				raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
			}
			client, ok := object.KindOK[*OAuth2Client](args[0])
			if !ok {
				raise("TypeError", "no implicit conversion of %s into OAuth2::Client", args[0].Inspect())
			}
			h, ok := object.KindOK[*object.Hash](args[1])
			if !ok {
				raise("TypeError", "no implicit conversion of %s into Hash", args[1].Inspect())
			}
			m := map[string]any{}
			order := make([]string, 0, h.Len())
			for _, k := range h.Keys {
				v, _ := h.Get(k)
				name := oauth2Name(k)
				m[name] = oauth2Str(v)
				order = append(order, name)
			}
			return object.Wrap(&OAuth2AccessToken{t: oauth2.AccessTokenFromHash(client.c, m, order)})
		}}

	d := func(name string, fn NativeFn) { cls.define(name, fn) }
	d("token", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(object.Kind[*OAuth2AccessToken](self).t.Token))
	})
	d("to_s", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(object.Kind[*OAuth2AccessToken](self).t.Token))
	})
	d("refresh_token", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		if r := object.Kind[*OAuth2AccessToken](self).t.RefreshToken; r != "" {
			return object.Wrap(object.NewString(r))
		}
		return object.NilVal()
	})
	d("expires?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.BoolValue(bool(object.Bool(object.Kind[*OAuth2AccessToken](self).t.Expires())))
	})
	d("expired?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.BoolValue(bool(object.Bool(object.Kind[*OAuth2AccessToken](self).t.Expired())))
	})
	d("expires_at", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		if ea := object.Kind[*OAuth2AccessToken](self).t.ExpiresAt; ea != 0 {
			return object.IntValue(ea)
		}
		return object.NilVal()
	})
	d("token_type", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(object.Kind[*OAuth2AccessToken](self).t.TokenType()))
	})
	d("scope", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(object.Kind[*OAuth2AccessToken](self).t.Scope()))
	})
	d("[]", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		if v, ok := object.Kind[*OAuth2AccessToken](self).t.Get(oauth2Name(args[0])); ok {
			return object.Wrap(object.NewString(v))
		}
		return object.NilVal()
	})
	d("to_hash", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return oauth2MapToHash(object.Kind[*OAuth2AccessToken](self).t.ToHash())
	})
	d("refresh", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		req, err := object.Kind[*OAuth2AccessToken](self).t.RefreshRequest(oauth2ExtraParams(args)...)
		if err != nil {
			raise("OAuth2::Error", "%s", err.Error())
		}
		return object.Wrap(&OAuth2Request{r: req})
	})
}

// registerOAuth2Value installs the OAuth2::Response and OAuth2::Request surfaces:
// Response.new(status, headers, body) with #status / #body / #parsed / #content_type,
// and Request with #method / #url / #body / #headers / #to_h.
func (vm *VM) registerOAuth2Value(mod *RClass) {
	resp := newClass("OAuth2::Response", vm.cObject)
	mod.consts["Response"] = object.Wrap(resp)
	vm.consts["OAuth2::Response"] = object.Wrap(resp)

	resp.smethods["new"] = &Method{name: "new", owner: resp,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			if len(args) == 0 {
				raise("ArgumentError", "wrong number of arguments (given 0, expected 1..3)")
			}
			status := int(intArg(args[0]))
			headers := oauth2.NewMap()
			if len(args) > 1 {
				if h, ok := object.KindOK[*object.Hash](args[1]); ok {
					for _, k := range h.Keys {
						v, _ := h.Get(k)
						headers.Set(oauth2Name(k), oauth2Str(v))
					}
				}
			}
			body := ""
			if len(args) > 2 {
				body = oauth2Str(args[2])
			}
			return object.Wrap(&OAuth2Response{r: oauth2.NewResponse(status, headers, body)})
		}}
	rd := func(name string, fn NativeFn) { resp.define(name, fn) }
	rd("content_type", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(object.Kind[*OAuth2Response](self).r.ContentType()))
	})
	rd("parsed", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return oauth2ParsedToHash(vm, object.Kind[*OAuth2Response](self).r.Parsed())
	})

	req := newClass("OAuth2::Request", vm.cObject)
	mod.consts["Request"] = object.Wrap(req)
	vm.consts["OAuth2::Request"] = object.Wrap(req)
	qd := func(name string, fn NativeFn) { req.define(name, fn) }
	qd("method", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(object.Kind[*OAuth2Request](self).r.Method))
	})
	qd("url", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(object.Kind[*OAuth2Request](self).r.FullURL()))
	})
	qd("body", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(object.Kind[*OAuth2Request](self).r.EncodedBody()))
	})
	qd("params", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return oauth2MapToHash(object.Kind[*OAuth2Request](self).r.Body)
	})
	qd("headers", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return oauth2MapToHash(object.Kind[*OAuth2Request](self).r.Headers)
	})
}

// --- bridges ----------------------------------------------------------------

// oauth2Params maps a trailing Hash argument into ordered oauth2.Params (the
// authorize_url query parameters).
func oauth2Params(args []object.Value) oauth2.Params {
	return oauth2ExtraParams(args)
}

// oauth2ExtraParams collects the trailing Hash argument's entries into ordered
// oauth2.Params; positional string arguments are ignored.
func oauth2ExtraParams(args []object.Value) oauth2.Params {
	var ps oauth2.Params
	for _, a := range args {
		if h, ok := object.KindOK[*object.Hash](a); ok {
			for _, k := range h.Keys {
				v, _ := h.Get(k)
				ps = append(ps, oauth2.Param{Key: oauth2Name(k), Val: oauth2Str(v)})
			}
		}
	}
	return ps
}

// oauth2MapToHash maps a *oauth2.Map (string→string, ordered) to a Ruby Hash.
func oauth2MapToHash(m *oauth2.Map) object.Value {
	h := object.NewHash()
	for _, p := range m.Pairs() {
		h.Set(object.Wrap(object.NewString(p.Key)), object.Wrap(object.NewString(p.Val)))
	}
	return object.Wrap(h)
}

// oauth2ParsedToHash maps a Response.Parsed result (map[string]any) to a Ruby
// Hash, sorting keys for a deterministic order. A nil result (an unparseable or
// empty body) yields nil.
func oauth2ParsedToHash(vm *VM, m map[string]any) object.Value {
	if m == nil {
		return object.NilVal()
	}
	return oauth2AnyToRuby(m)
}

// oauth2AnyToRuby maps a decoded JSON/form value (from Response.Parsed) into the
// rbgo object graph: scalars, nested map[string]any (sorted for determinism) and
// []any recurse.
func oauth2AnyToRuby(v any) object.Value {
	switch n := v.(type) {
	case nil:
		return object.NilVal()
	case bool:
		return object.BoolValue(bool(object.Bool(n)))
	case string:
		return object.Wrap(object.NewString(n))
	case float64:
		return object.FloatValue(float64(object.Float(n)))
	case int:
		return object.IntValue(int64(n))
	case int64:
		return object.IntValue(n)
	case []any:
		arr := object.NewArrayFromSlice(make([]object.Value, len(n)))
		for i, el := range n {
			arr.Elems[i] = oauth2AnyToRuby(el)
		}
		return object.Wrap(arr)
	case map[string]any:
		keys := make([]string, 0, len(n))
		for k := range n {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		h := object.NewHash()
		for _, k := range keys {
			h.Set(object.Wrap(object.NewString(k)), oauth2AnyToRuby(n[k]))
		}
		return object.Wrap(h)
	}
	return object.NilVal()
}

// oauth2Name renders a key (Symbol or String) as its bare name.
func oauth2Name(v object.Value) string {
	{
		__sw107 := v
		switch {
		case object.IsKind[object.Symbol](__sw107):
			n := object.Kind[object.Symbol](__sw107)
			_ = n
			return string(n)
		case object.IsKind[*object.String](__sw107):
			n := object.Kind[*object.String](__sw107)
			_ = n
			return n.Str()
		}
	}
	return v.ToS()
}

// oauth2Str renders a value as its request string: a String verbatim, anything
// else its to_s.
func oauth2Str(v object.Value) string {
	if s, ok := object.KindOK[*object.String](v); ok {
		return s.Str()
	}
	return v.ToS()
}
