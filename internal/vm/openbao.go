// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"time"

	openbao "github.com/go-ruby-openbao/openbao"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// openbaoTransport builds the openbao.Doer rbgo wires into every Vault::Client —
// a Net::HTTP-backed transport in production (vaultNetHTTPDoer), so a real Vault
// request flows through the interpreter's own HTTP stack (nethttp_bind.go). Tests
// override it with an in-process stub Doer returning canned Vault JSON, so the
// suite touches no network and leaks no goroutine. The library performs the whole
// client abstraction around this seam; only the round-trip is host-provided.
var openbaoTransport = func(vm *VM) openbao.Doer { return &vaultNetHTTPDoer{vm: vm} }

// vaultClasses is the resolved set of Vault::* value classes the bridges
// construct from; each is registered both scoped (Vault::Client) and flat in
// vm.consts so classOf resolves it by qualified name.
type vaultClasses struct {
	client    *RClass
	logical   *RClass
	kvv1      *RClass
	kvv2      *RClass
	transit   *RClass
	sys       *RClass
	auth      *RClass
	tokenAuth *RClass
	appRole   *RClass
	userpass  *RClass
	secret    *RClass
}

// registerVault installs the Vault module (require "vault") and its OpenBao alias
// (require "openbao"): the vault-gem-flavoured OpenBao / HashiCorp Vault client,
// reimplemented in pure Go (CGO=0) by github.com/go-ruby-openbao/openbao. The
// library owns the protocol; this file is the thin shell mapping its surface onto
// rbgo classes:
//
//	Vault::Client.new(address:, token:, namespace:) — the connection: #logical,
//	                              #kv / #kv_v1 / #kv_v2, #transit, #sys, #auth,
//	                              #token(=), #namespace(=), #adopt_token
//	Vault::Logical                — read/write/list/delete over an arbitrary path
//	Vault::KVv1 / Vault::KVv2     — the KV secrets-engine helpers (v2 versioning)
//	Vault::Transit                — encrypt/decrypt/rewrap/sign/verify/data-key
//	Vault::Sys                    — health/seal_status/mounts/policies/leases
//	Vault::Auth                   — token self-mgmt + AppRole / Userpass login
//	Vault::Secret                 — the decoded response envelope
//	Vault::VaultError (< StandardError) — the gem's HTTP error tree
//
// Vault and OpenBao name the same classes, so a value's #class is stable however
// it was required. The value types, the transport Doer (wired to rbgo's bound
// Net::HTTP) and the error bridge live in openbao_bind.go.
func (vm *VM) registerVault() {
	mod := newClass("Vault", nil)
	mod.isModule = true
	vm.consts["Vault"] = mod

	cl := vm.registerVaultClasses(mod)
	vm.registerVaultErrors(mod)
	vm.registerVaultClient(cl)
	vm.registerVaultLogical(cl)
	vm.registerVaultKV(cl)
	vm.registerVaultTransit(cl)
	vm.registerVaultSys(cl)
	vm.registerVaultAuth(cl)
	vm.registerVaultSecret(cl)

	// OpenBao is a second module naming exactly the same classes, so
	// require "openbao" and require "vault" are interchangeable and a value's
	// #class is the same object either way.
	alias := newClass("OpenBao", nil)
	alias.isModule = true
	alias.consts = mod.consts
	vm.consts["OpenBao"] = alias
}

// registerVaultClasses creates every Vault value class under the module and
// returns the resolved set the bridges construct from.
func (vm *VM) registerVaultClasses(mod *RClass) *vaultClasses {
	mk := func(name string) *RClass {
		full := "Vault::" + name
		cls := newClass(full, vm.cObject)
		mod.consts[name] = cls
		vm.consts[full] = cls
		return cls
	}
	return &vaultClasses{
		client:    mk("Client"),
		logical:   mk("Logical"),
		kvv1:      mk("KVv1"),
		kvv2:      mk("KVv2"),
		transit:   mk("Transit"),
		sys:       mk("Sys"),
		auth:      mk("Auth"),
		tokenAuth: mk("TokenAuth"),
		appRole:   mk("AppRole"),
		userpass:  mk("Userpass"),
		secret:    mk("Secret"),
	}
}

// registerVaultErrors installs the Vault exception tree, mirroring the gem:
// Vault::VaultError < StandardError, Vault::HTTPError beneath it, and the
// client/server/connection subclasses beneath HTTPError (plus
// MissingRequiredStateError). Each class name equals the library's ErrorKind
// string, so a raised *openbao.VaultError maps to its Ruby class by name.
// Vault::HTTPError#code and #errors expose the status/API-errors context a raised
// response error carries.
func (vm *VM) registerVaultErrors(mod *RClass) {
	std := vm.consts["StandardError"].(*RClass)
	defs := []struct{ qualified, parent string }{
		{"Vault::VaultError", "StandardError"},
		{"Vault::HTTPError", "Vault::VaultError"},
		{"Vault::MissingRequiredStateError", "Vault::VaultError"},
		{"Vault::HTTPConnectionError", "Vault::HTTPError"},
		{"Vault::HTTPClientError", "Vault::HTTPError"},
		{"Vault::HTTPServerError", "Vault::HTTPError"},
	}
	for _, d := range defs {
		parent := std
		if d.parent != "StandardError" {
			parent = vm.consts[d.parent].(*RClass)
		}
		cls := newClass(d.qualified, parent)
		vm.consts[d.qualified] = cls
		mod.consts[d.qualified[len("Vault::"):]] = cls
	}
	base := vm.consts["Vault::VaultError"].(*RClass)
	base.define("code", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return getIvar(self, "@code")
	})
	base.define("errors", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return getIvar(self, "@errors")
	})
}

// vaultClientNew builds a Vault::Client from Vault::Client.new's keyword options
// (address:/token:/namespace:/timeout:), wiring the transport through the
// openbaoTransport seam. Any unset field is resolved by the library from the
// environment (VAULT_ADDR/BAO_ADDR, …) and the defaults.
func (vm *VM) vaultClientNew(cl *vaultClasses, args []object.Value) object.Value {
	cfg := openbao.Config{Doer: openbaoTransport(vm)}
	if h := vaultKwHash(args); h != nil {
		cfg.Address = vaultKwStr(h, "address")
		cfg.Token = vaultKwStr(h, "token")
		cfg.Namespace = vaultKwStr(h, "namespace")
		if v, ok := h.Get(object.Symbol("timeout")); ok {
			cfg.Timeout = time.Duration(intArg(v)) * time.Second
		}
	}
	return &VaultClient{cls: cl.client, c: openbao.NewClient(cfg)}
}

// registerVaultClient installs Vault::Client and its endpoint-group accessors and
// token / namespace management.
func (vm *VM) registerVaultClient(cl *vaultClasses) {
	cls := cl.client
	cls.smethods["new"] = &Method{name: "new", owner: cls,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			return vm.vaultClientNew(cl, args)
		}}
	self := func(v object.Value) *openbao.Client { return v.(*VaultClient).c }
	d := func(name string, fn NativeFn) { cls.define(name, fn) }

	d("logical", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return &VaultLogical{cls: cl.logical, l: self(v).Logical()}
	})
	d("kv_v1", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return &VaultKVv1{cls: cl.kvv1, k: self(v).KVv1(vaultStrAt(args, 0))}
	})
	kvv2 := func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return &VaultKVv2{cls: cl.kvv2, k: self(v).KVv2(vaultStrAt(args, 0))}
	}
	d("kv_v2", kvv2)
	d("kv", kvv2)
	d("transit", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return &VaultTransit{cls: cl.transit, t: self(v).Transit(vaultStrAt(args, 0))}
	})
	d("sys", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return &VaultSys{cls: cl.sys, s: self(v).Sys()}
	})
	d("auth", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return &VaultAuth{cls: cl.auth, a: self(v).Auth()}
	})
	d("address", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).Address())
	})
	d("token", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).Token())
	})
	d("token=", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		self(v).SetToken(strArg(args[0]))
		return args[0]
	})
	d("namespace", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).Namespace())
	})
	d("namespace=", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		self(v).SetNamespace(strArg(args[0]))
		return args[0]
	})
	d("adopt_token", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if s, ok := vaultArgAt(args, 0).(*VaultSecret); ok {
			self(v).AdoptToken(s.s)
		}
		return v
	})
}

// registerVaultLogical installs the Vault::Logical read/write/list/delete surface.
func (vm *VM) registerVaultLogical(cl *vaultClasses) {
	cls := cl.logical
	self := func(v object.Value) *openbao.Logical { return v.(*VaultLogical).l }
	cls.define("read", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		s, err := self(v).Read(strArg(args[0]))
		vm.vaultRaiseIf(err)
		return vm.vaultSecretValue(cl.secret, s)
	})
	cls.define("write", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		s, err := self(v).Write(strArg(args[0]), vaultDataArg(vaultArgAt(args, 1)))
		vm.vaultRaiseIf(err)
		return vm.vaultSecretValue(cl.secret, s)
	})
	cls.define("list", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		s, err := self(v).List(strArg(args[0]))
		vm.vaultRaiseIf(err)
		return vm.vaultSecretValue(cl.secret, s)
	})
	cls.define("delete", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		s, err := self(v).Delete(strArg(args[0]))
		vm.vaultRaiseIf(err)
		return vm.vaultSecretValue(cl.secret, s)
	})
}

// registerVaultKV installs the Vault::KVv1 and Vault::KVv2 secrets-engine helpers.
func (vm *VM) registerVaultKV(cl *vaultClasses) {
	v1 := cl.kvv1
	k1 := func(v object.Value) *openbao.KVv1 { return v.(*VaultKVv1).k }
	ret := func(vm *VM, s *openbao.Secret, err error) object.Value {
		vm.vaultRaiseIf(err)
		return vm.vaultSecretValue(cl.secret, s)
	}
	v1.define("read", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		s, err := k1(v).Read(strArg(args[0]))
		return ret(vm, s, err)
	})
	v1.define("write", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		s, err := k1(v).Write(strArg(args[0]), vaultDataArg(vaultArgAt(args, 1)))
		return ret(vm, s, err)
	})
	v1.define("list", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		s, err := k1(v).List(strArg(args[0]))
		return ret(vm, s, err)
	})
	v1.define("delete", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		s, err := k1(v).Delete(strArg(args[0]))
		return ret(vm, s, err)
	})

	v2 := cl.kvv2
	k2 := func(v object.Value) *openbao.KVv2 { return v.(*VaultKVv2).k }
	v2.define("read", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		s, err := k2(v).Read(strArg(args[0]))
		return ret(vm, s, err)
	})
	v2.define("read_version", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		s, err := k2(v).ReadVersion(strArg(args[0]), vaultIntAt(args, 1, 0))
		return ret(vm, s, err)
	})
	v2.define("write", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		s, err := k2(v).Write(strArg(args[0]), vaultDataArg(vaultArgAt(args, 1)))
		return ret(vm, s, err)
	})
	v2.define("list", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		s, err := k2(v).List(strArg(args[0]))
		return ret(vm, s, err)
	})
	v2.define("delete", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		s, err := k2(v).Delete(strArg(args[0]))
		return ret(vm, s, err)
	})
	v2.define("delete_versions", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		s, err := k2(v).DeleteVersions(strArg(args[0]), vaultVersions(args, 1)...)
		return ret(vm, s, err)
	})
	v2.define("undelete", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		s, err := k2(v).Undelete(strArg(args[0]), vaultVersions(args, 1)...)
		return ret(vm, s, err)
	})
	v2.define("destroy", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		s, err := k2(v).Destroy(strArg(args[0]), vaultVersions(args, 1)...)
		return ret(vm, s, err)
	})
	v2.define("read_metadata", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		s, err := k2(v).ReadMetadata(strArg(args[0]))
		return ret(vm, s, err)
	})
	v2.define("write_metadata", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		s, err := k2(v).WriteMetadata(strArg(args[0]), vaultDataArg(vaultArgAt(args, 1)))
		return ret(vm, s, err)
	})
	v2.define("delete_metadata", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		s, err := k2(v).DeleteMetadata(strArg(args[0]))
		return ret(vm, s, err)
	})
}

// registerVaultTransit installs the Vault::Transit cryptographic-operation
// surface (encrypt/decrypt/rewrap/sign/verify/generate_data_key).
func (vm *VM) registerVaultTransit(cl *vaultClasses) {
	cls := cl.transit
	self := func(v object.Value) *openbao.Transit { return v.(*VaultTransit).t }
	ret := func(vm *VM, s *openbao.Secret, err error) object.Value {
		vm.vaultRaiseIf(err)
		return vm.vaultSecretValue(cl.secret, s)
	}
	cls.define("encrypt", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		s, err := self(v).Encrypt(strArg(args[0]), []byte(strArg(args[1])), vaultContext(args, 2)...)
		return ret(vm, s, err)
	})
	cls.define("decrypt", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		s, err := self(v).Decrypt(strArg(args[0]), strArg(args[1]), vaultContext(args, 2)...)
		return ret(vm, s, err)
	})
	cls.define("rewrap", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		s, err := self(v).Rewrap(strArg(args[0]), strArg(args[1]), vaultContext(args, 2)...)
		return ret(vm, s, err)
	})
	cls.define("sign", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		s, err := self(v).Sign(strArg(args[0]), []byte(strArg(args[1])))
		return ret(vm, s, err)
	})
	cls.define("verify", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		s, err := self(v).Verify(strArg(args[0]), []byte(strArg(args[1])), strArg(args[2]))
		return ret(vm, s, err)
	})
	cls.define("generate_data_key", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		s, err := self(v).GenerateDataKey(strArg(args[0]), strArg(args[1]))
		return ret(vm, s, err)
	})
}

// registerVaultSys installs the Vault::Sys system-backend surface (health,
// seal status, mount management, policy management, lease management).
func (vm *VM) registerVaultSys(cl *vaultClasses) {
	cls := cl.sys
	self := func(v object.Value) *openbao.Sys { return v.(*VaultSys).s }
	secret := func(vm *VM, s *openbao.Secret, err error) object.Value {
		vm.vaultRaiseIf(err)
		return vm.vaultSecretValue(cl.secret, s)
	}
	rawM := func(vm *VM, m map[string]any, err error) object.Value {
		vm.vaultRaiseIf(err)
		return vaultMapValue(m)
	}
	cls.define("health", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		m, err := self(v).Health()
		return rawM(vm, m, err)
	})
	cls.define("seal_status", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		m, err := self(v).SealStatus()
		return rawM(vm, m, err)
	})
	cls.define("mounts", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		m, err := self(v).Mounts()
		return rawM(vm, m, err)
	})
	cls.define("enable_mount", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		err := self(v).EnableMount(strArg(args[0]), strArg(args[1]), vaultDataArg(vaultArgAt(args, 2)))
		vm.vaultRaiseIf(err)
		return object.Bool(true)
	})
	cls.define("disable_mount", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		vm.vaultRaiseIf(self(v).DisableMount(strArg(args[0])))
		return object.Bool(true)
	})
	cls.define("policies", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		s, err := self(v).Policies()
		return secret(vm, s, err)
	})
	cls.define("policy", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		s, err := self(v).Policy(strArg(args[0]))
		return secret(vm, s, err)
	})
	cls.define("put_policy", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		vm.vaultRaiseIf(self(v).PutPolicy(strArg(args[0]), strArg(args[1])))
		return object.Bool(true)
	})
	cls.define("delete_policy", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		vm.vaultRaiseIf(self(v).DeletePolicy(strArg(args[0])))
		return object.Bool(true)
	})
	cls.define("renew_lease", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		s, err := self(v).RenewLease(strArg(args[0]), vaultIntAt(args, 1, 0))
		return secret(vm, s, err)
	})
	cls.define("revoke_lease", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		vm.vaultRaiseIf(self(v).RevokeLease(strArg(args[0])))
		return object.Bool(true)
	})
	cls.define("lookup_lease", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		s, err := self(v).LookupLease(strArg(args[0]))
		return secret(vm, s, err)
	})
}

// registerVaultAuth installs the Vault::Auth group and its token / AppRole /
// Userpass login helpers.
func (vm *VM) registerVaultAuth(cl *vaultClasses) {
	auth := cl.auth
	self := func(v object.Value) *openbao.Auth { return v.(*VaultAuth).a }
	auth.define("token", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return &VaultTokenAuth{cls: cl.tokenAuth, t: self(v).Token()}
	})
	auth.define("app_role", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return &VaultAppRole{cls: cl.appRole, a: self(v).AppRole(vaultStrAt(args, 0))}
	})
	auth.define("userpass", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return &VaultUserpass{cls: cl.userpass, u: self(v).Userpass(vaultStrAt(args, 0))}
	})

	secret := func(vm *VM, s *openbao.Secret, err error) object.Value {
		vm.vaultRaiseIf(err)
		return vm.vaultSecretValue(cl.secret, s)
	}
	tok := cl.tokenAuth
	tself := func(v object.Value) *openbao.TokenAuth { return v.(*VaultTokenAuth).t }
	tok.define("lookup_self", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		s, err := tself(v).LookupSelf()
		return secret(vm, s, err)
	})
	tok.define("renew_self", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		s, err := tself(v).RenewSelf(vaultIntAt(args, 0, 0))
		return secret(vm, s, err)
	})
	tok.define("revoke_self", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		vm.vaultRaiseIf(tself(v).RevokeSelf())
		return object.Bool(true)
	})
	cl.appRole.define("login", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		s, err := v.(*VaultAppRole).a.Login(strArg(args[0]), strArg(args[1]))
		return secret(vm, s, err)
	})
	cl.userpass.define("login", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		s, err := v.(*VaultUserpass).u.Login(strArg(args[0]), strArg(args[1]))
		return secret(vm, s, err)
	})
}

// registerVaultSecret installs the read-only Vault::Secret accessor surface
// (data / lease_id / lease_duration / renewable? / warnings / auth / wrap_info /
// request_id / token).
func (vm *VM) registerVaultSecret(cl *vaultClasses) {
	cls := cl.secret
	self := func(v object.Value) *openbao.Secret { return v.(*VaultSecret).s }
	cls.define("data", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return vaultMapValue(self(v).Data)
	})
	cls.define("request_id", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).RequestID)
	})
	cls.define("lease_id", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).LeaseID)
	})
	cls.define("lease_duration", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).LeaseDuration))
	})
	renew := func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).Renewable)
	}
	cls.define("renewable?", renew)
	cls.define("renewable", renew)
	cls.define("warnings", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return vaultStrArray(self(v).Warnings)
	})
	cls.define("auth", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return vaultSecretAuthHash(self(v).Auth)
	})
	cls.define("wrap_info", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return vaultMapValue(self(v).WrapInfo)
	})
	cls.define("token", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).TokenID())
	})
}
