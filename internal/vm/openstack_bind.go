// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"context"
	"io"
	"strings"

	openstack "github.com/go-ruby-openstack/openstack"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the value bridge for the OpenStack module (see openstack.go). It
// builds the library's Options from a Ruby keyword Hash, threads the optional
// per-VM HTTP transport (the test/injection seam) into it, converts each library
// Resource (map[string]any keyed by the wire snake_case names) into a Ruby Hash
// through the shared goValueToRuby engine, and maps the library's typed error
// tree onto the OpenStack::Error Ruby classes.

// osConnect implements OpenStack.connect(auth_url:, username:, password:, ...):
// it reads the trailing keyword Hash, builds openstack.Options, injects the
// per-VM transport seam when one is set (so a test can drive a mock cloud), and
// authenticates. A missing auth_url or any authentication failure raises
// OpenStack::AuthError (the library maps every Connect failure to *AuthError).
func (vm *VM) osConnect(args []object.Value) object.Value {
	h := osConnectHash(args)
	opts := openstack.Options{
		AuthURL:                     osOptStr(h, "auth_url"),
		Username:                    osOptStr(h, "username"),
		UserID:                      osOptStr(h, "user_id"),
		Password:                    osOptStr(h, "password"),
		Token:                       osOptStr(h, "token"),
		ApplicationCredentialID:     osOptStr(h, "application_credential_id"),
		ApplicationCredentialName:   osOptStr(h, "application_credential_name"),
		ApplicationCredentialSecret: osOptStr(h, "application_credential_secret"),
		ProjectName:                 osOptStr(h, "project_name"),
		ProjectID:                   osOptStr(h, "project_id"),
		DomainName:                  osOptStr(h, "domain_name"),
		DomainID:                    osOptStr(h, "domain_id"),
		Region:                      osOptStr(h, "region"),
		AllowReauth:                 osOptBool(h, "allow_reauth"),
	}
	if vm.osTransport != nil {
		opts.Transport = vm.osTransport
	}
	conn, err := openstack.Connect(context.Background(), opts)
	if err != nil {
		vm.osRaise(err)
	}
	return &OpenStackConnection{c: conn}
}

// osConnectHash returns the trailing keyword Hash of a connect call, or a fresh
// empty Hash when the caller passed nothing (or a non-Hash argument).
func osConnectHash(args []object.Value) *object.Hash {
	if len(args) > 0 {
		if h, ok := args[len(args)-1].(*object.Hash); ok {
			return h
		}
	}
	return object.NewHash()
}

// osOptStr reads a connect option by symbol or string key, stringified, defaulting
// to "" when absent.
func osOptStr(h *object.Hash, key string) string {
	if v, ok := osOptGet(h, key); ok {
		return v.ToS()
	}
	return ""
}

// osOptBool reads a connect option by symbol or string key as a truthiness,
// defaulting to false when absent.
func osOptBool(h *object.Hash, key string) bool {
	if v, ok := osOptGet(h, key); ok {
		return v.Truthy()
	}
	return false
}

// osOptGet reads an option by symbol or string key from a keyword Hash (a nil
// Hash, or an absent key, reports false).
func osOptGet(h *object.Hash, key string) (object.Value, bool) {
	if h == nil {
		return nil, false
	}
	if v, ok := h.Get(object.Symbol(key)); ok {
		return v, true
	}
	return h.Get(object.NewString(key))
}

// osArgStr reads args[i] as a resource id / name String, raising ArgumentError
// when it is missing (strArg raises TypeError when it is not a String).
func osArgStr(args []object.Value, i int) string {
	if len(args) <= i {
		raise("ArgumentError", "wrong number of arguments (given %d, expected %d)", len(args), i+1)
	}
	return strArg(args[i])
}

// osArgAttrs reads args[i] as an attribute Hash and converts it to the library's
// Resource (a map[string]any keyed by each entry's #to_s). A missing argument
// raises ArgumentError, a non-Hash TypeError.
func osArgAttrs(args []object.Value, i int) openstack.Resource {
	if len(args) <= i {
		raise("ArgumentError", "wrong number of arguments (given %d, expected %d)", len(args), i+1)
	}
	h, ok := args[i].(*object.Hash)
	if !ok {
		raise("TypeError", "no implicit conversion of %s into Hash", classNameOf(args[i]))
	}
	return rubyToGoValue(h).(map[string]any)
}

// osArgReader reads args[i] as a String and exposes it as an io.Reader for the
// object/image upload calls.
func osArgReader(args []object.Value, i int) io.Reader {
	return strings.NewReader(osArgStr(args, i))
}

// osResources maps a library list result onto a Ruby Array of Hashes, raising the
// mapped OpenStack::Error on failure.
func (vm *VM) osResources(list []openstack.Resource, err error) object.Value {
	if err != nil {
		vm.osRaise(err)
	}
	elems := make([]object.Value, len(list))
	for i, r := range list {
		elems[i] = goValueToRuby(r)
	}
	return object.NewArrayFromSlice(elems)
}

// osResource maps a single library resource onto a Ruby Hash, raising the mapped
// OpenStack::Error on failure.
func (vm *VM) osResource(r openstack.Resource, err error) object.Value {
	if err != nil {
		vm.osRaise(err)
	}
	return goValueToRuby(r)
}

// osVoid maps a side-effecting library call's error onto the OpenStack::Error
// tree, returning nil on success.
func (vm *VM) osVoid(err error) object.Value {
	if err != nil {
		vm.osRaise(err)
	}
	return object.NilV
}

// osBytes maps a raw object body onto a Ruby String, raising the mapped
// OpenStack::Error on failure.
func (vm *VM) osBytes(b []byte, err error) object.Value {
	if err != nil {
		vm.osRaise(err)
	}
	return object.NewString(string(b))
}

// osService returns a service-accessor wrapper, raising the mapped OpenStack::Error
// when the library could not build the service client (a catalog / endpoint error).
func (vm *VM) osService(wrapper object.Value, err error) object.Value {
	if err != nil {
		vm.osRaise(err)
	}
	return wrapper
}

// osRaise maps one of the library's typed errors onto the matching Ruby class and
// raises it: 404 -> OpenStack::NotFound, 401 -> OpenStack::AuthError, 403 ->
// OpenStack::Forbidden, 409 -> OpenStack::Conflict, 400 -> OpenStack::BadRequest,
// everything else (transport failures, 5xx, ...) -> OpenStack::Error.
func (vm *VM) osRaise(err error) {
	switch {
	case openstack.IsNotFound(err):
		raise("OpenStack::NotFound", "%s", err.Error())
	case openstack.IsAuth(err):
		raise("OpenStack::AuthError", "%s", err.Error())
	case openstack.IsForbidden(err):
		raise("OpenStack::Forbidden", "%s", err.Error())
	case openstack.IsConflict(err):
		raise("OpenStack::Conflict", "%s", err.Error())
	case openstack.IsBadRequest(err):
		raise("OpenStack::BadRequest", "%s", err.Error())
	default:
		raise("OpenStack::Error", "%s", err.Error())
	}
}
