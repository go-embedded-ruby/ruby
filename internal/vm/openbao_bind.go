// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"net/url"
	"strconv"

	openbao "github.com/go-ruby-openbao/openbao"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin binding between rbgo's Ruby object graph (object.Value)
// and the interpreter-independent github.com/go-ruby-openbao/openbao library —
// the pure-Go core of the `vault` gem (Vault::Client). The library owns the
// whole client abstraction around the wire: the config/token/namespace
// resolution, the /v1/ path building, the JSON request/response bodies, and the
// HTTP-status→error mapping. Only the round-trip itself is a host seam (an
// openbao.Doer). rbgo wraps each library object as a Ruby object reporting the
// matching Vault::* class (see openbao.go for the class + method registration)
// and converts values across the boundary here. The production Doer is wired to
// rbgo's own bound Net::HTTP (vaultNetHTTPDoer), so a real request flows through
// the interpreter's HTTP stack; tests inject an in-process stub Doer through the
// openbaoTransport seam so the suite touches no network and leaks no goroutine.

// The wrapper types. Each holds a pointer into the library plus the Ruby class
// it reports (classOf returns the held cls), and the methods registered in
// openbao.go operate on the held value.

// VaultClient wraps a *openbao.Client (Vault::Client / OpenBao::Client).
type VaultClient struct {
	cls *RClass
	c   *openbao.Client
}

// VaultLogical wraps a *openbao.Logical (Vault::Logical).
type VaultLogical struct {
	cls *RClass
	l   *openbao.Logical
}

// VaultKVv1 wraps a *openbao.KVv1 (Vault::KVv1).
type VaultKVv1 struct {
	cls *RClass
	k   *openbao.KVv1
}

// VaultKVv2 wraps a *openbao.KVv2 (Vault::KVv2).
type VaultKVv2 struct {
	cls *RClass
	k   *openbao.KVv2
}

// VaultTransit wraps a *openbao.Transit (Vault::Transit).
type VaultTransit struct {
	cls *RClass
	t   *openbao.Transit
}

// VaultSys wraps a *openbao.Sys (Vault::Sys).
type VaultSys struct {
	cls *RClass
	s   *openbao.Sys
}

// VaultAuth wraps a *openbao.Auth (Vault::Auth).
type VaultAuth struct {
	cls *RClass
	a   *openbao.Auth
}

// VaultTokenAuth wraps a *openbao.TokenAuth (Vault::TokenAuth).
type VaultTokenAuth struct {
	cls *RClass
	t   *openbao.TokenAuth
}

// VaultAppRole wraps a *openbao.AppRoleAuth (Vault::AppRole).
type VaultAppRole struct {
	cls *RClass
	a   *openbao.AppRoleAuth
}

// VaultUserpass wraps a *openbao.UserpassAuth (Vault::Userpass).
type VaultUserpass struct {
	cls *RClass
	u   *openbao.UserpassAuth
}

// VaultSecret wraps a *openbao.Secret (Vault::Secret).
type VaultSecret struct {
	cls *RClass
	s   *openbao.Secret
}

func (v *VaultClient) ToS() string        { return "#<Vault::Client address=" + v.c.Address() + ">" }
func (v *VaultClient) Inspect() string    { return v.ToS() }
func (v *VaultClient) Truthy() bool       { return true }
func (v *VaultLogical) ToS() string       { return "#<Vault::Logical>" }
func (v *VaultLogical) Inspect() string   { return v.ToS() }
func (v *VaultLogical) Truthy() bool      { return true }
func (v *VaultKVv1) ToS() string          { return "#<Vault::KVv1>" }
func (v *VaultKVv1) Inspect() string      { return v.ToS() }
func (v *VaultKVv1) Truthy() bool         { return true }
func (v *VaultKVv2) ToS() string          { return "#<Vault::KVv2>" }
func (v *VaultKVv2) Inspect() string      { return v.ToS() }
func (v *VaultKVv2) Truthy() bool         { return true }
func (v *VaultTransit) ToS() string       { return "#<Vault::Transit>" }
func (v *VaultTransit) Inspect() string   { return v.ToS() }
func (v *VaultTransit) Truthy() bool      { return true }
func (v *VaultSys) ToS() string           { return "#<Vault::Sys>" }
func (v *VaultSys) Inspect() string       { return v.ToS() }
func (v *VaultSys) Truthy() bool          { return true }
func (v *VaultAuth) ToS() string          { return "#<Vault::Auth>" }
func (v *VaultAuth) Inspect() string      { return v.ToS() }
func (v *VaultAuth) Truthy() bool         { return true }
func (v *VaultTokenAuth) ToS() string     { return "#<Vault::TokenAuth>" }
func (v *VaultTokenAuth) Inspect() string { return v.ToS() }
func (v *VaultTokenAuth) Truthy() bool    { return true }
func (v *VaultAppRole) ToS() string       { return "#<Vault::AppRole>" }
func (v *VaultAppRole) Inspect() string   { return v.ToS() }
func (v *VaultAppRole) Truthy() bool      { return true }
func (v *VaultUserpass) ToS() string      { return "#<Vault::Userpass>" }
func (v *VaultUserpass) Inspect() string  { return v.ToS() }
func (v *VaultUserpass) Truthy() bool     { return true }
func (v *VaultSecret) ToS() string        { return "#<Vault::Secret>" }
func (v *VaultSecret) Inspect() string    { return v.ToS() }
func (v *VaultSecret) Truthy() bool       { return true }

// --- transport seam: the Net::HTTP-backed Doer -----------------------------

// vaultNetHTTPDoer is the production openbao.Doer: it performs the HTTP
// round-trip through rbgo's own bound Net::HTTP transport (nethttp_bind.go), so
// a Vault request flows through the interpreter's HTTP stack rather than a
// separate Go net/http client. The library builds the transport-agnostic
// openbao.Request (URL / method / headers / JSON body); this Doer replays it on
// the VM's Net::HTTP and decodes the resulting Net::HTTPResponse back into an
// openbao.Response.
type vaultNetHTTPDoer struct{ vm *VM }

// Do runs one openbao.Request through Net::HTTP and returns the decoded
// openbao.Response (status / body / headers). A malformed request URL is
// returned as a transport error, which the library maps onto a
// Vault::HTTPConnectionError.
func (d *vaultNetHTTPDoer) Do(req *openbao.Request) (*openbao.Response, error) {
	u, err := url.Parse(req.URL)
	if err != nil {
		return nil, err
	}
	hdr := make([][2]string, 0, len(req.Headers))
	for k, v := range req.Headers {
		hdr = append(hdr, [2]string{k, v})
	}
	respVal := d.vm.nethttpExecURL(u, req.Method, req.Body, hdr)
	code, _ := strconv.Atoi(strArg(getIvar(respVal, "@code")))
	resp := &openbao.Response{StatusCode: code, Header: map[string][]string{}}
	if b := getIvar(respVal, "@body"); !object.IsNil(b) {
		resp.Body = []byte(strArg(b))
	}
	h := getIvar(respVal, "@header").(*object.Hash)
	for _, k := range h.Keys {
		hv, _ := h.Get(k)
		resp.Header[k.ToS()] = []string{hv.ToS()}
	}
	return resp, nil
}

// --- error bridge -----------------------------------------------------------

// raiseVaultError re-raises a library *openbao.VaultError as its matching Ruby
// exception (named by the error kind — Vault::HTTPClientError, …), carrying the
// message and, for an HTTP-status error, the #code and #errors context. Every
// error the library returns is a *openbao.VaultError whose Kind is one of the
// six registered subclasses, so both assertions are total.
func (vm *VM) raiseVaultError(err error) {
	ve := err.(*openbao.VaultError)
	cls := vm.consts[string(ve.Kind)].(*RClass)
	exc := &RObject{class: cls, ivars: map[string]object.Value{}}
	exc.ivars["@message"] = object.NewString(ve.Message)
	exc.ivars["@code"] = object.IntValue(int64(ve.StatusCode))
	exc.ivars["@errors"] = vaultStrArray(ve.Errors)
	panic(vm.excError(exc))
}

// vaultRaiseIf raises the mapped Vault exception when err is non-nil.
func (vm *VM) vaultRaiseIf(err error) {
	if err != nil {
		vm.raiseVaultError(err)
	}
}

// --- value conversion -------------------------------------------------------

// vaultSecretValue wraps a library *openbao.Secret as a Ruby Vault::Secret, or
// returns nil when the secret is nil (a 404 read or a 204 write/delete, which
// the gem surfaces as nil).
func (vm *VM) vaultSecretValue(cls *RClass, s *openbao.Secret) object.Value {
	if s == nil {
		return object.NilV
	}
	return &VaultSecret{cls: cls, s: s}
}

// vaultMapValue converts a raw library map (sys health / mounts / seal status)
// to a Ruby Hash, or nil when the map is nil.
func vaultMapValue(m map[string]any) object.Value {
	if m == nil {
		return object.NilV
	}
	return goValueToRuby(m)
}

// vaultDataArg reads a request-body argument as the library's map[string]any: a
// Ruby Hash converts recursively, anything else (a missing argument) yields an
// empty map.
func vaultDataArg(v object.Value) map[string]any {
	if h, ok := v.(*object.Hash); ok {
		if m, ok := rubyToGoValue(h).(map[string]any); ok {
			return m
		}
	}
	return map[string]any{}
}

// vaultStrArray renders a Go string slice as a Ruby Array of Strings (an empty
// Array for a nil slice).
func vaultStrArray(ss []string) object.Value {
	elems := make([]object.Value, len(ss))
	for i, s := range ss {
		elems[i] = object.NewString(s)
	}
	return object.NewArrayFromSlice(elems)
}

// vaultSecretAuthHash renders a *openbao.SecretAuth as a Ruby Hash (the gem's
// Vault::Secret#auth), or nil when the secret carries no auth block.
func vaultSecretAuthHash(a *openbao.SecretAuth) object.Value {
	if a == nil {
		return object.NilV
	}
	h := object.NewHash()
	h.Set(object.Symbol("client_token"), object.NewString(a.ClientToken))
	h.Set(object.Symbol("accessor"), object.NewString(a.Accessor))
	h.Set(object.Symbol("policies"), vaultStrArray(a.Policies))
	h.Set(object.Symbol("token_policies"), vaultStrArray(a.TokenPolicies))
	h.Set(object.Symbol("metadata"), vaultMapValue(a.Metadata))
	h.Set(object.Symbol("lease_duration"), object.IntValue(int64(a.LeaseDuration)))
	h.Set(object.Symbol("renewable"), object.Bool(a.Renewable))
	return h
}

// --- argument helpers -------------------------------------------------------

// vaultArgAt returns args[i], or nil when the index is out of range.
func vaultArgAt(args []object.Value, i int) object.Value {
	if i < len(args) {
		return args[i]
	}
	return object.NilV
}

// vaultStrAt returns args[i] as a String, or "" when the argument is absent or
// nil (used for optional mount / context arguments).
func vaultStrAt(args []object.Value, i int) string {
	v := vaultArgAt(args, i)
	if object.IsNil(v) {
		return ""
	}
	return strArg(v)
}

// vaultIntAt returns args[i] as an int, or def when the argument is absent or
// nil.
func vaultIntAt(args []object.Value, i int, def int) int {
	v := vaultArgAt(args, i)
	if object.IsNil(v) {
		return def
	}
	return int(intArg(v))
}

// vaultVersions reads a trailing list of version-number arguments starting at i.
func vaultVersions(args []object.Value, i int) []int {
	var out []int
	for _, a := range args[min(i, len(args)):] {
		out = append(out, int(intArg(a)))
	}
	return out
}

// vaultContext reads an optional transit context argument (args[i]) as bytes, or
// nil when absent, returned as the variadic [][]byte the library expects.
func vaultContext(args []object.Value, i int) [][]byte {
	s := vaultStrAt(args, i)
	if s == "" {
		return nil
	}
	return [][]byte{[]byte(s)}
}

// vaultKwHash returns the trailing keyword Hash of a call, or nil when the last
// argument is not a Hash.
func vaultKwHash(args []object.Value) *object.Hash {
	if len(args) == 0 {
		return nil
	}
	if h, ok := args[len(args)-1].(*object.Hash); ok {
		return h
	}
	return nil
}

// vaultKwStr reads a Symbol-keyed String option from a keyword Hash, or "".
func vaultKwStr(h *object.Hash, key string) string {
	if v, ok := h.Get(object.Symbol(key)); ok {
		return v.ToS()
	}
	return ""
}
