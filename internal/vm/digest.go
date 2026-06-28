// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
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

// digestConstructors maps an MRI digest algorithm name (as used by both the
// Digest module — Digest::SHA256 — and OpenSSL::Digest.new("SHA256")) to the
// Go crypto/* constructor producing a byte-identical hash. The OpenSSL family
// adds SHA224/SHA384 over the Digest set, so both surfaces share one table.
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

// registerDigest installs the Digest module (require "digest") with the common
// algorithms as Digest::MD5/SHA1/SHA224/SHA256/SHA384/SHA512, each offering
// hexdigest, digest and base64digest. Backed by Go's crypto/* — byte-identical
// to MRI.
func (vm *VM) registerDigest() {
	d := newClass("Digest", nil)
	d.isModule = true
	vm.consts["Digest"] = d

	algo := func(name string) {
		c := newClass("Digest::"+name, vm.cObject)
		d.consts[name] = c
		sum := func(args []object.Value) []byte {
			h := digestNewByName(name)
			h.Write([]byte(base64Arg(args[0])))
			return h.Sum(nil)
		}
		c.smethods["hexdigest"] = &Method{name: "hexdigest", owner: c,
			native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
				return object.NewString(hex.EncodeToString(sum(args)))
			}}
		c.smethods["digest"] = &Method{name: "digest", owner: c,
			native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
				return object.NewString(string(sum(args)))
			}}
		c.smethods["base64digest"] = &Method{name: "base64digest", owner: c,
			native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
				return object.NewString(base64.StdEncoding.EncodeToString(sum(args)))
			}}
	}
	for _, name := range []string{"MD5", "SHA1", "SHA224", "SHA256", "SHA384", "SHA512"} {
		algo(name)
	}
}
