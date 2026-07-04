// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"context"

	acme "github.com/go-ruby-acme/acme"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file installs the Acme module (require "acme" / "acme/client") — a
// pure-Go, acme-client-gem-flavoured surface over github.com/go-ruby-acme/acme
// (itself a shell over golang.org/x/crypto/acme). The Ruby object graph mirrors
// the acme-client gem: Acme::Client, its Order / Authorization / Challenge /
// CertificateRequest resources and the Acme::Client::Error tree. The ACME
// transport is a host seam (see acme_bind.go) so the whole flow runs against an
// in-process mock CA with no real Let's Encrypt and no network.

// ACMEClient wraps an *acme.Client as a Ruby Acme::Client. It is the entry
// point for account registration and order creation. The RFC 8555 / JWS
// protocol lives entirely in the go-ruby-acme library; this shell reports the
// Ruby class (via classOf) and bridges keyword arguments.
type ACMEClient struct {
	c   *acme.Client
	cls *RClass
}

func (c *ACMEClient) ToS() string     { return "#<Acme::Client>" }
func (c *ACMEClient) Inspect() string { return c.ToS() }
func (c *ACMEClient) Truthy() bool    { return true }

// ACMEOrder wraps an *acme.Order as a Ruby Acme::Client::Order.
type ACMEOrder struct {
	o   *acme.Order
	cls *RClass
}

func (o *ACMEOrder) ToS() string     { return "#<Acme::Client::Order url=" + o.o.URL() + ">" }
func (o *ACMEOrder) Inspect() string { return o.ToS() }
func (o *ACMEOrder) Truthy() bool    { return true }

// ACMEAuthorization wraps an *acme.Authorization as Acme::Client::Authorization.
type ACMEAuthorization struct {
	a   *acme.Authorization
	cls *RClass
}

func (a *ACMEAuthorization) ToS() string {
	return "#<Acme::Client::Authorization domain=" + a.a.Domain() + ">"
}
func (a *ACMEAuthorization) Inspect() string { return a.ToS() }
func (a *ACMEAuthorization) Truthy() bool    { return true }

// ACMEChallenge wraps an *acme.Challenge as a Ruby Acme::Client::Challenge.
type ACMEChallenge struct {
	ch  *acme.Challenge
	cls *RClass
}

func (c *ACMEChallenge) ToS() string     { return "#<Acme::Client::Challenge type=" + c.ch.Type() + ">" }
func (c *ACMEChallenge) Inspect() string { return c.ToS() }
func (c *ACMEChallenge) Truthy() bool    { return true }

// ACMECSR wraps a DER-encoded PKCS#10 request as Acme::Client::CertificateRequest.
type ACMECSR struct {
	der   []byte
	names []string
	cls   *RClass
}

func (r *ACMECSR) ToS() string     { return "#<Acme::Client::CertificateRequest>" }
func (r *ACMECSR) Inspect() string { return r.ToS() }
func (r *ACMECSR) Truthy() bool    { return true }

// registerACME installs the Acme module and its resource classes (require
// "acme"). Acme::Client.new(directory:, private_key:) builds a client; the
// account / order / authorization / challenge / certificate flow mirrors the
// acme-client gem, and problem documents surface through the Acme::Client::Error
// tree. Every network call goes through the go-ruby-acme library over the
// injectable transport seam installed in acme_bind.go.
func (vm *VM) registerACME() {
	mod := newClass("Acme", nil)
	mod.isModule = true
	vm.consts["Acme"] = mod

	std := vm.consts["StandardError"].(*RClass)

	// Acme::Error < StandardError — the root the binding raises for non-ACME
	// (transport / key / argument) failures.
	acmeErr := newClass("Acme::Error", std)
	mod.consts["Error"] = acmeErr
	vm.consts["Acme::Error"] = acmeErr

	clientCls := newClass("Acme::Client", vm.cObject)
	mod.consts["Client"] = clientCls
	vm.consts["Acme::Client"] = clientCls

	// Acme::Client::Error < StandardError and its problem-document subclasses,
	// mirroring the acme-client gem's error tree.
	clientErr := newClass("Acme::Client::Error", std)
	clientCls.consts["Error"] = clientErr
	vm.consts["Acme::Client::Error"] = clientErr
	for _, name := range []string{"Unauthorized", "BadNonce", "Malformed", "RateLimited"} {
		sub := newClass("Acme::Client::Error::"+name, clientErr)
		clientErr.consts[name] = sub
		vm.consts["Acme::Client::Error::"+name] = sub
	}

	orderCls := newClass("Acme::Client::Order", vm.cObject)
	clientCls.consts["Order"] = orderCls
	vm.consts["Acme::Client::Order"] = orderCls

	authzCls := newClass("Acme::Client::Authorization", vm.cObject)
	clientCls.consts["Authorization"] = authzCls
	vm.consts["Acme::Client::Authorization"] = authzCls

	challengeCls := newClass("Acme::Client::Challenge", vm.cObject)
	clientCls.consts["Challenge"] = challengeCls
	vm.consts["Acme::Client::Challenge"] = challengeCls

	csrCls := newClass("Acme::Client::CertificateRequest", vm.cObject)
	clientCls.consts["CertificateRequest"] = csrCls
	vm.consts["Acme::Client::CertificateRequest"] = csrCls

	// --- Acme::Client.new(directory:, private_key: nil) --------------------------
	clientCls.smethods["new"] = &Method{name: "new", owner: clientCls,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			kw := acmeKwargs(args)
			dir := acmeKwString(kw, "directory")
			if dir == "" {
				raise("ArgumentError", "missing keyword: :directory")
			}
			key, err := acmeAccountKey(acmeKwGet(kw, "private_key"))
			if err != nil {
				raise("Acme::Error", "%s", err.Error())
			}
			return &ACMEClient{c: newACMEClient(key, dir), cls: clientCls}
		}}

	newAccount := func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		c := self.(*ACMEClient)
		kw := acmeKwargs(args)
		contact := acmeKwStrings(kw, "contact")
		tos := acmeKwBool(kw, "terms_of_service_agreed")
		acct, err := c.c.NewAccount(context.Background(), contact, tos)
		if err != nil {
			acmeRaise(err)
		}
		return acmeAccountHash(acct)
	}
	clientCls.define("new_account", newAccount)
	clientCls.define("register", newAccount)

	clientCls.define("account", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		acct, err := self.(*ACMEClient).c.Account(context.Background())
		if err != nil {
			acmeRaise(err)
		}
		return acmeAccountHash(acct)
	})

	clientCls.define("new_order", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		kw := acmeKwargs(args)
		ids := acmeKwStrings(kw, "identifiers")
		if len(ids) == 0 {
			raise("ArgumentError", "missing keyword: :identifiers")
		}
		o, err := self.(*ACMEClient).c.NewOrder(context.Background(), ids)
		if err != nil {
			acmeRaise(err)
		}
		return &ACMEOrder{o: o, cls: orderCls}
	})

	// --- Acme::Client::Order ------------------------------------------------------
	orderCls.define("url", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self.(*ACMEOrder).o.URL())
	})
	orderCls.define("status", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self.(*ACMEOrder).o.Status())
	})
	orderCls.define("identifiers", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return acmeStringArray(self.(*ACMEOrder).o.Identifiers())
	})
	orderCls.define("reload", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		if err := self.(*ACMEOrder).o.Reload(context.Background()); err != nil {
			acmeRaise(err)
		}
		return self
	})
	orderCls.define("authorizations", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		azs, err := self.(*ACMEOrder).o.Authorizations(context.Background())
		if err != nil {
			acmeRaise(err)
		}
		out := make([]object.Value, len(azs))
		for i, az := range azs {
			out[i] = &ACMEAuthorization{a: az, cls: authzCls}
		}
		return object.NewArrayFromSlice(out)
	})
	orderCls.define("finalize", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		kw := acmeKwargs(args)
		csr, ok := acmeKwGet(kw, "csr").(*ACMECSR)
		if !ok {
			raise("ArgumentError", "finalize requires a csr: Acme::Client::CertificateRequest")
		}
		if err := self.(*ACMEOrder).o.Finalize(context.Background(), csr.der); err != nil {
			acmeRaise(err)
		}
		return self
	})
	orderCls.define("certificate", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		pem, err := self.(*ACMEOrder).o.Certificate(context.Background())
		if err != nil {
			acmeRaise(err)
		}
		return object.NewString(pem)
	})

	// --- Acme::Client::Authorization ---------------------------------------------
	authzCls.define("url", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self.(*ACMEAuthorization).a.URL())
	})
	authzCls.define("status", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self.(*ACMEAuthorization).a.Status())
	})
	authzCls.define("domain", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self.(*ACMEAuthorization).a.Domain())
	})
	authzCls.define("wildcard?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self.(*ACMEAuthorization).a.Wildcard())
	})
	authzCls.define("challenges", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		chs := self.(*ACMEAuthorization).a.Challenges()
		out := make([]object.Value, len(chs))
		for i, ch := range chs {
			out[i] = &ACMEChallenge{ch: ch, cls: challengeCls}
		}
		return object.NewArrayFromSlice(out)
	})
	authzChal := func(ch *acme.Challenge) object.Value {
		if ch == nil {
			return object.NilV
		}
		return &ACMEChallenge{ch: ch, cls: challengeCls}
	}
	authzCls.define("http01", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return authzChal(self.(*ACMEAuthorization).a.HTTP01())
	})
	authzCls.define("dns01", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return authzChal(self.(*ACMEAuthorization).a.DNS01())
	})
	authzCls.define("tls_alpn01", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return authzChal(self.(*ACMEAuthorization).a.TLSALPN01())
	})

	// --- Acme::Client::Challenge -------------------------------------------------
	challengeCls.define("type", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self.(*ACMEChallenge).ch.Type())
	})
	challengeCls.define("token", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self.(*ACMEChallenge).ch.Token())
	})
	challengeCls.define("url", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self.(*ACMEChallenge).ch.URL())
	})
	challengeCls.define("status", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self.(*ACMEChallenge).ch.Status())
	})
	challengeCls.define("key_authorization", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		// KeyAuthorization derives "token.thumbprint" offline from the account
		// key. That key is either an auto-generated P-256 key or a user PEM that
		// has already signed account registration by the time any challenge
		// exists, so it is always a supported JWS key here and this cannot error.
		ka, _ := self.(*ACMEChallenge).ch.KeyAuthorization()
		return object.NewString(ka)
	})
	challengeCls.define("request_validation", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		if err := self.(*ACMEChallenge).ch.RequestValidation(context.Background()); err != nil {
			acmeRaise(err)
		}
		return self
	})
	challengeCls.define("reload", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		if err := self.(*ACMEChallenge).ch.Reload(context.Background()); err != nil {
			acmeRaise(err)
		}
		return self
	})
	challengeCls.define("error", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		err := self.(*ACMEChallenge).ch.Error()
		if err == nil {
			return object.NilV
		}
		return object.NewString(err.Error())
	})

	// --- Acme::Client::CertificateRequest.new(names:, common_name:) --------------
	csrCls.smethods["new"] = &Method{name: "new", owner: csrCls,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			kw := acmeKwargs(args)
			names := acmeKwStrings(kw, "names")
			if cn := acmeKwString(kw, "common_name"); cn != "" {
				names = append([]string{cn}, names...)
			}
			if len(names) == 0 {
				raise("ArgumentError", "CertificateRequest requires names: or common_name:")
			}
			der, err := acmeBuildCSR(names)
			if err != nil {
				raise("Acme::Error", "%s", err.Error())
			}
			return &ACMECSR{der: der, names: names, cls: csrCls}
		}}
	csrCls.define("names", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return acmeStringArray(self.(*ACMECSR).names)
	})
	csrCls.define("common_name", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self.(*ACMECSR).names[0])
	})
}
