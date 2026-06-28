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

// DigestObj is an instance of a Digest algorithm class (Digest::SHA256.new):
// an incremental hasher that accumulates input through #update / #<< and yields
// its result with #hexdigest / #digest. Puppet's checksum_stream feeds file
// content through one of these before reading the hex digest.
type DigestObj struct {
	cls *RClass
	h   hash.Hash
	new func() hash.Hash // to reset / produce a fresh state
}

func (d *DigestObj) ToS() string     { return "#<" + d.cls.name + ">" }
func (d *DigestObj) Inspect() string { return d.ToS() }
func (d *DigestObj) Truthy() bool    { return true }

// registerDigest installs the Digest module (require "digest") with the common
// algorithms as Digest::MD5/SHA1/SHA224/SHA256/SHA384/SHA512. Each class offers
// the one-shot class methods hexdigest/digest/base64digest and the incremental
// instance protocol (new, update/<<, hexdigest/digest, reset). Backed by Go's
// crypto/* — byte-identical to MRI.
func (vm *VM) registerDigest() {
	d := newClass("Digest", nil)
	d.isModule = true
	vm.consts["Digest"] = d

	algo := func(name string) {
		c := newClass("Digest::"+name, vm.cObject)
		d.consts[name] = c
		ctor := digestConstructors[name]
		sum := func(args []object.Value) []byte {
			h := ctor()
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
		// Digest::SHA256.new → a fresh incremental hasher.
		c.smethods["new"] = &Method{name: "new", owner: c,
			native: func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
				return &DigestObj{cls: c, h: ctor(), new: ctor}
			}}

		// Incremental instance protocol. update / << feed bytes and return self so
		// they chain; hexdigest / digest read the running result (without resetting,
		// matching MRI when called with no argument); reset starts over.
		feed := func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
			o := self.(*DigestObj)
			o.h.Write([]byte(strArg(args[0])))
			return o
		}
		c.define("update", feed)
		c.define("<<", feed)
		c.define("hexdigest", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
			o := self.(*DigestObj)
			if len(args) > 0 {
				o.h.Write([]byte(strArg(args[0])))
			}
			return object.NewString(hex.EncodeToString(o.h.Sum(nil)))
		})
		c.define("digest", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
			o := self.(*DigestObj)
			if len(args) > 0 {
				o.h.Write([]byte(strArg(args[0])))
			}
			return object.NewString(string(o.h.Sum(nil)))
		})
		c.define("base64digest", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
			o := self.(*DigestObj)
			return object.NewString(base64.StdEncoding.EncodeToString(o.h.Sum(nil)))
		})
		c.define("reset", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
			o := self.(*DigestObj)
			o.h = o.new()
			return o
		})
	}
	for _, name := range []string{"MD5", "SHA1", "SHA224", "SHA256", "SHA384", "SHA512"} {
		algo(name)
	}
}
