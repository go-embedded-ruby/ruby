// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"hash"
	"strings"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// digestConstructors maps an MRI digest algorithm name (as OpenSSL::Digest.new
// accepts it) to the Go crypto/* constructor producing a byte-identical hash. The
// OpenSSL surface adds SHA224 over the plain Digest set and needs a raw hash.Hash
// (for HMAC and for #block_length / #digest_length), so it keeps this crypto/*
// table; the require "digest" Digest module is instead backed by the standalone
// go-ruby-digest library (see digest_bind.go).
var digestConstructors = map[string]func() hash.Hash{
	"MD5":    md5.New,
	"SHA1":   sha1.New,
	"SHA224": sha256.New224,
	"SHA256": sha256.New,
	"SHA384": sha512.New384,
	"SHA512": sha512.New,
}

// digestNewByName returns a fresh hash.Hash for an MRI algorithm name, or nil
// when the name is unknown (the caller raises the right Ruby error). The lookup
// is case-insensitive and tolerant of the dashed spellings OpenSSL accepts
// ("SHA-256"), matching MRI's OpenSSL::Digest.new.
func digestNewByName(name string) hash.Hash {
	key := strings.ToUpper(strings.ReplaceAll(name, "-", ""))
	if ctor, ok := digestConstructors[key]; ok {
		return ctor()
	}
	return nil
}

// opensslDigest backs an OpenSSL::Digest instance — a running hash created with
// OpenSSL::Digest.new("SHA256") (or the named subclasses). It carries its exact
// Ruby class so classOf / is_a? report e.g. OpenSSL::Digest::SHA256, and an
// incremental hash.Hash fed by #update / #<<, matching MRI byte for byte
// (crypto/* under the hood — real crypto, no cgo).
type opensslDigest struct {
	cls *RClass
	h   hash.Hash
}

func (o *opensslDigest) ToS() string     { return "#<OpenSSL::Digest>" }
func (o *opensslDigest) Inspect() string { return o.ToS() }
func (o *opensslDigest) Truthy() bool    { return true }

// registerOpenSSL installs the OpenSSL module (require "openssl"): real digest /
// HMAC / random primitives over Go's crypto/*, plus the class-and-constant shell
// that Puppet's SSL stack references at load time. The genuinely-hard PKI / TLS
// methods exist but raise NotImplementedError if actually called — local
// `puppet apply` never performs SSL networking, but Puppet eagerly requires the
// SSL stack at load, so the shell must resolve.
func (vm *VM) registerOpenSSL() {
	mod := newClass("OpenSSL", nil)
	mod.isModule = true
	vm.consts["OpenSSL"] = mod

	// OpenSSL::OpenSSLError < StandardError is the root of the OpenSSL error tree;
	// the per-namespace error classes below descend from it, matching MRI.
	errRoot := newClass("OpenSSL::OpenSSLError", object.Kind[*RClass](vm.consts["StandardError"]))
	mod.consts["OpenSSLError"] = errRoot

	// Version-identification constants. rbgo's OpenSSL surface is a pure-Go shim,
	// so it reports its own banner rather than a linked libcrypto's; consumers that
	// merely log these (e.g. Puppet's log_runtime_environment, which reads
	// OPENSSL_VERSION) get a stable, non-empty string instead of a NameError.
	mod.consts["VERSION"] = object.NewString("4.0.0")
	mod.consts["OPENSSL_VERSION"] = object.NewString("rbgo pure-Go OpenSSL shim")
	mod.consts["OPENSSL_LIBRARY_VERSION"] = object.NewString("rbgo pure-Go OpenSSL shim")
	mod.consts["OPENSSL_VERSION_NUMBER"] = object.IntValue(0)
	mod.consts["OPENSSL_FIPS"] = object.Bool(false)

	vm.registerOpenSSLDigest(mod)
	vm.registerOpenSSLHMAC(mod, errRoot)
	vm.registerOpenSSLRandom(mod)
	vm.registerOpenSSLPKI(mod, errRoot)
}

// registerOpenSSLDigest installs OpenSSL::Digest with real incremental hashing.
// OpenSSL::Digest.new("SHA256") and the OpenSSL::Digest::SHA256 subclasses both
// produce a running digest; the class also offers the one-shot class methods
// digest/hexdigest/base64digest, matching MRI.
func (vm *VM) registerOpenSSLDigest(mod *RClass) {
	dig := newClass("OpenSSL::Digest", vm.cObject)
	mod.consts["Digest"] = dig

	// newInstance builds a running OpenSSL::Digest for algorithm `name` on class
	// `cls`, optionally seeding it with an initial data string (MRI's optional
	// second argument). An unknown algorithm raises RuntimeError as MRI does.
	newInstance := func(cls *RClass, name string, seed []object.Value) object.Value {
		h := digestNewByName(name)
		if h == nil {
			return raise("RuntimeError", "Unsupported digest algorithm (%s).", name)
		}
		if len(seed) > 0 {
			h.Write([]byte(base64Arg(seed[0])))
		}
		return &opensslDigest{cls: cls, h: h}
	}

	// OpenSSL::Digest.new(name, [data]) — name selects the algorithm.
	dig.smethods["new"] = &Method{name: "new", owner: dig,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			if len(args) == 0 {
				raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
			}
			return newInstance(dig, base64Arg(args[0]), args[1:])
		}}

	// Class-level one-shots: OpenSSL::Digest.digest(name, data) etc.
	dig.smethods["digest"] = &Method{name: "digest", owner: dig,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			return object.NewString(string(opensslOneShot(args)))
		}}
	dig.smethods["hexdigest"] = &Method{name: "hexdigest", owner: dig,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			return object.NewString(hex.EncodeToString(opensslOneShot(args)))
		}}
	dig.smethods["base64digest"] = &Method{name: "base64digest", owner: dig,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			return object.NewString(base64.StdEncoding.EncodeToString(opensslOneShot(args)))
		}}

	// Named subclasses (OpenSSL::Digest::SHA256.new -> that algorithm), plus their
	// own one-shot class methods that need no algorithm argument.
	for _, name := range []string{"MD5", "SHA1", "SHA224", "SHA256", "SHA384", "SHA512"} {
		name := name
		sub := newClass("OpenSSL::Digest::"+name, dig)
		dig.consts[name] = sub
		sub.smethods["new"] = &Method{name: "new", owner: sub,
			native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
				return newInstance(sub, name, args)
			}}
		sub.smethods["digest"] = &Method{name: "digest", owner: sub,
			native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
				return object.NewString(string(opensslSubOneShot(name, args)))
			}}
		sub.smethods["hexdigest"] = &Method{name: "hexdigest", owner: sub,
			native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
				return object.NewString(hex.EncodeToString(opensslSubOneShot(name, args)))
			}}
	}

	// Instance protocol: update/<< feed data; digest/hexdigest/base64digest emit
	// the current digest without consuming the running state, exactly like MRI.
	dig.define("update", opensslDigestUpdate)
	dig.define("<<", opensslDigestUpdate)
	dig.define("reset", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		object.Kind[*opensslDigest](self).h.Reset()
		return self
	})
	dig.define("digest", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(string(opensslDigestSum(self, args)))
	})
	dig.define("hexdigest", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(hex.EncodeToString(opensslDigestSum(self, args)))
	})
	dig.define("base64digest", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(base64.StdEncoding.EncodeToString(opensslDigestSum(self, args)))
	})
	dig.define("digest_length", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(object.Kind[*opensslDigest](self).h.Size()))
	})
	dig.define("block_length", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(object.Kind[*opensslDigest](self).h.BlockSize()))
	})

	vm.cOpenSSLDigest = dig
}

// opensslDigestSum returns the current digest of a running OpenSSL::Digest. With
// an optional data argument it first resets and feeds that data — matching MRI,
// where d.digest(s) == d.reset.update(s).digest and leaves d holding only s.
func opensslDigestSum(self object.Value, args []object.Value) []byte {
	d := object.Kind[*opensslDigest](self)
	if len(args) > 0 {
		d.h.Reset()
		d.h.Write([]byte(base64Arg(args[0])))
	}
	return d.h.Sum(nil)
}

// opensslDigestUpdate appends data to a running OpenSSL::Digest and returns self
// (so `d << a << b` chains), shared by #update and #<<.
func opensslDigestUpdate(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
	object.Kind[*opensslDigest](self).h.Write([]byte(base64Arg(args[0])))
	return self
}

// opensslOneShot computes the digest for the class-level OpenSSL::Digest.digest
// form: args = [name, data]. An unknown algorithm raises RuntimeError, as MRI.
func opensslOneShot(args []object.Value) []byte {
	if len(args) < 2 {
		raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
	}
	return opensslSubOneShot(base64Arg(args[0]), args[1:])
}

// opensslSubOneShot computes the digest of args[0] under the named algorithm for
// the subclass one-shots (OpenSSL::Digest::SHA256.digest(data)).
func opensslSubOneShot(name string, args []object.Value) []byte {
	h := digestNewByName(name)
	if h == nil {
		raise("RuntimeError", "Unsupported digest algorithm (%s).", name)
	}
	if len(args) == 0 {
		raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
	}
	h.Write([]byte(base64Arg(args[0])))
	return h.Sum(nil)
}

// registerOpenSSLHMAC installs OpenSSL::HMAC.digest / .hexdigest over
// crypto/hmac. The digest argument may be a name String or an OpenSSL::Digest
// instance, matching MRI; both select the same underlying hash.
func (vm *VM) registerOpenSSLHMAC(mod, errRoot *RClass) {
	h := newClass("OpenSSL::HMAC", vm.cObject)
	mod.consts["HMAC"] = h
	mac := func(args []object.Value) []byte {
		if len(args) < 3 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 3)", len(args))
		}
		ctor := hmacConstructor(args[0])
		m := hmac.New(ctor, []byte(base64Arg(args[1])))
		m.Write([]byte(base64Arg(args[2])))
		return m.Sum(nil)
	}
	h.smethods["digest"] = &Method{name: "digest", owner: h,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			return object.NewString(string(mac(args)))
		}}
	h.smethods["hexdigest"] = &Method{name: "hexdigest", owner: h,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			return object.NewString(hex.EncodeToString(mac(args)))
		}}
	_ = errRoot
}

// hmacConstructor resolves the HMAC digest selector (a name String or an
// OpenSSL::Digest instance) to a hash constructor, raising RuntimeError for an
// unknown algorithm.
func hmacConstructor(sel object.Value) func() hash.Hash {
	{
		__sw114 := sel
		switch {
		case object.IsKind[*opensslDigest](__sw114):
			d := object.Kind[*opensslDigest](__sw114)
			_ = d
			return func() hash.Hash { h := digestCloneCtor(d); return h }
		default:
			d := __sw114
			_ = d
			name := base64Arg(sel)
			if digestNewByName(name) == nil {
				raise("RuntimeError", "Unsupported digest algorithm (%s).", name)
			}
			return func() hash.Hash { return digestNewByName(name) }
		}
	}
}

// digestCloneCtor returns a fresh hash of the same algorithm as the given
// running OpenSSL::Digest, recovered from its class name (set at construction).
func digestCloneCtor(d *opensslDigest) hash.Hash {
	name := d.cls.name
	if i := lastColonIndex(name); i >= 0 {
		name = name[i+1:]
	}
	if h := digestNewByName(name); h != nil {
		return h
	}
	// The base OpenSSL::Digest class carries no algorithm in its name; fall back to
	// the running hash's own size to pick a matching constructor.
	return digestBySize(d.h.Size())
}

// lastColonIndex returns the index of the last ':' in s, or -1.
func lastColonIndex(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == ':' {
			return i
		}
	}
	return -1
}

// digestBySize maps a digest output size to a constructor (used only for the
// rare base-class OpenSSL::Digest fed to HMAC).
func digestBySize(n int) hash.Hash {
	switch n {
	case 16:
		return digestNewByName("MD5")
	case 20:
		return digestNewByName("SHA1")
	case 28:
		return digestNewByName("SHA224")
	case 48:
		return digestNewByName("SHA384")
	case 64:
		return digestNewByName("SHA512")
	default:
		return digestNewByName("SHA256")
	}
}

// registerOpenSSLRandom installs OpenSSL::Random.random_bytes over crypto/rand
// (the secureBytes seam), matching MRI's binary (ASCII-8BIT) result.
func (vm *VM) registerOpenSSLRandom(mod *RClass) {
	r := newClass("OpenSSL::Random", nil)
	r.isModule = true
	mod.consts["Random"] = r
	r.smethods["random_bytes"] = &Method{name: "random_bytes", owner: r,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			return object.NewStringBytesEnc(secureBytes(int(intArg(args[0]))), "ASCII-8BIT")
		}}
}

// registerOpenSSLPKI installs the class-and-constant shell of the PKI / TLS
// surface (X509, PKey, SSL, BN, Cipher, ASN1) that Puppet references at load
// time. The classes exist with the correct nesting and constants so load-time
// references resolve; the methods that need real PKI / a TLS handshake raise
// NotImplementedError if actually called (local apply never reaches them).
func (vm *VM) registerOpenSSLPKI(mod, errRoot *RClass) {
	// notImpl returns a native method that raises NotImplementedError naming the
	// construct, so a stubbed PKI path fails loudly and rescuably rather than
	// silently misbehaving.
	notImpl := func(what string) NativeFn {
		return func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
			return raise("NotImplementedError", "OpenSSL::%s is not yet supported (pure-Go PKI/TLS pending)", what)
		}
	}
	// subErr defines a namespaced OpenSSL error class under errRoot.
	subErr := func(ns *RClass, qname, key string) {
		ns.consts[key] = newClass(qname, errRoot)
	}
	// shell defines an empty class under ns whose .new raises NotImplementedError.
	shell := func(ns *RClass, qname, key string) *RClass {
		c := newClass(qname, vm.cObject)
		c.smethods["new"] = &Method{name: "new", owner: c, native: notImpl(qname[len("OpenSSL::"):])}
		ns.consts[key] = c
		return c
	}

	// --- OpenSSL::X509 -----------------------------------------------------------
	x509 := newClass("OpenSSL::X509", nil)
	x509.isModule = true
	mod.consts["X509"] = x509
	shell(x509, "OpenSSL::X509::Certificate", "Certificate")
	shell(x509, "OpenSSL::X509::Name", "Name")
	shell(x509, "OpenSSL::X509::Store", "Store")
	shell(x509, "OpenSSL::X509::StoreContext", "StoreContext")
	shell(x509, "OpenSSL::X509::Request", "Request")
	shell(x509, "OpenSSL::X509::CRL", "CRL")
	shell(x509, "OpenSSL::X509::Extension", "Extension")
	shell(x509, "OpenSSL::X509::ExtensionFactory", "ExtensionFactory")
	shell(x509, "OpenSSL::X509::Attribute", "Attribute")
	subErr(x509, "OpenSSL::X509::CertificateError", "CertificateError")
	subErr(x509, "OpenSSL::X509::CRLError", "CRLError")
	subErr(x509, "OpenSSL::X509::RequestError", "RequestError")
	subErr(x509, "OpenSSL::X509::NameError", "NameError")
	subErr(x509, "OpenSSL::X509::StoreError", "StoreError")
	subErr(x509, "OpenSSL::X509::ExtensionError", "ExtensionError")
	subErr(x509, "OpenSSL::X509::AttributeError", "AttributeError")
	// Verification result codes Puppet's verifier switches on at runtime.
	for k, v := range map[string]int{
		"V_OK": 0, "V_ERR_HOSTNAME_MISMATCH": 62,
		"V_ERR_CRL_NOT_YET_VALID": 11, "V_ERR_CRL_HAS_EXPIRED": 12,
		"V_ERR_CERT_NOT_YET_VALID": 9, "V_ERR_CERT_HAS_EXPIRED": 10,
	} {
		x509.consts[k] = object.IntValue(int64(v))
	}

	// --- OpenSSL::PKey -----------------------------------------------------------
	pkey := newClass("OpenSSL::PKey", nil)
	pkey.isModule = true
	mod.consts["PKey"] = pkey
	shell(pkey, "OpenSSL::PKey::PKey", "PKey")
	shell(pkey, "OpenSSL::PKey::RSA", "RSA")
	shell(pkey, "OpenSSL::PKey::EC", "EC")
	shell(pkey, "OpenSSL::PKey::DSA", "DSA")
	subErr(pkey, "OpenSSL::PKey::PKeyError", "PKeyError")
	subErr(pkey, "OpenSSL::PKey::RSAError", "RSAError")
	subErr(pkey, "OpenSSL::PKey::ECError", "ECError")

	// --- OpenSSL::SSL ------------------------------------------------------------
	ssl := newClass("OpenSSL::SSL", nil)
	ssl.isModule = true
	mod.consts["SSL"] = ssl
	shell(ssl, "OpenSSL::SSL::SSLSocket", "SSLSocket")
	subErr(ssl, "OpenSSL::SSL::SSLError", "SSLError")
	ssl.consts["VERIFY_NONE"] = object.IntValue(0)
	ssl.consts["VERIFY_PEER"] = object.IntValue(1)
	ssl.consts["VERIFY_FAIL_IF_NO_PEER_CERT"] = object.IntValue(2)
	ssl.consts["VERIFY_CLIENT_ONCE"] = object.IntValue(4)
	// TLS option bit flags Puppet's monkey-patch ORs into DEFAULT_PARAMS.
	for k, v := range map[string]int{
		"OP_NO_SSLv2": 0x01000000, "OP_NO_SSLv3": 0x02000000,
		"OP_NO_TLSv1": 0x04000000, "OP_ALL": 0x80000bff,
	} {
		ssl.consts[k] = object.IntValue(int64(v))
	}

	// OpenSSL::SSL::SSLContext is constructible (Puppet reopens it and builds one),
	// but TLS handshakes are a later round. It carries the DEFAULT_PARAMS Hash that
	// Puppet's monkey-patch reads/mutates at load, a real initialize (so the
	// `alias __original_initialize initialize` patch resolves) and a no-op
	// set_params storing the params for inspection.
	sslCtx := newClass("OpenSSL::SSL::SSLContext", vm.cObject)
	ssl.consts["SSLContext"] = sslCtx
	defaultParams := object.NewHash()
	defaultParams.Set(object.Symbol("ciphers"), object.NilV)
	defaultParams.Set(object.Symbol("verify_mode"), ssl.consts["VERIFY_PEER"])
	sslCtx.consts["DEFAULT_PARAMS"] = defaultParams
	sslCtx.smethods["new"] = &Method{name: "new", owner: sslCtx,
		native: func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
			o := &RObject{class: sslCtx, ivars: map[string]object.Value{}}
			vm.send(o, "initialize", args, blk)
			return o
		}}
	sslCtx.define("initialize", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NilV
	})
	sslCtx.define("set_params", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) > 0 {
			setIvar(self, "@params", args[0])
		}
		return getIvar(self, "@params")
	})
	ssl.smethods["verify_certificate_identity"] = &Method{name: "verify_certificate_identity", owner: ssl,
		native: notImpl("SSL.verify_certificate_identity")}

	// --- OpenSSL::ASN1 (referenced by certificate_request at load) ---------------
	asn1 := newClass("OpenSSL::ASN1", nil)
	asn1.isModule = true
	mod.consts["ASN1"] = asn1
	shell(asn1, "OpenSSL::ASN1::UTF8String", "UTF8String")
	shell(asn1, "OpenSSL::ASN1::Sequence", "Sequence")
	shell(asn1, "OpenSSL::ASN1::Set", "Set")
	shell(asn1, "OpenSSL::ASN1::ObjectId", "ObjectId")
	subErr(asn1, "OpenSSL::ASN1::ASN1Error", "ASN1Error")

	// --- OpenSSL::BN / OpenSSL::Cipher ------------------------------------------
	shell(mod, "OpenSSL::BN", "BN")
	cipher := shell(mod, "OpenSSL::Cipher", "Cipher")
	subErr(cipher, "OpenSSL::Cipher::CipherError", "CipherError")
}
