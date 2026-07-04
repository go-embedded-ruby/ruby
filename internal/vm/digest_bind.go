// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"os"
	"strings"

	digest "github.com/go-ruby-digest/digest"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin binding between rbgo's Ruby object graph and the
// interpreter-independent message-digest core of github.com/go-ruby-digest/digest
// (a pure-Go, byte-for-byte port of MRI's Digest stdlib). rbgo's former
// internal/vm/digest.go computed the hashes itself with crypto/*; that logic now
// lives in the library, so registerDigest below only wires Ruby's Digest module
// — the Digest::ALGO classes, their one-shot class methods and the incremental
// instance protocol — onto the library's New / named constructors / Sum family /
// BubbleBabble. Puppet feeds file content through these for checksums; the result
// is byte-identical to MRI by construction. The shared digestNewByName helper
// that OpenSSL::Digest also uses keeps its own crypto/* table in openssl.go,
// since that surface needs hash.Hash (for HMAC) and SHA224.

// digestAlgos is the set of algorithms MRI's `require "digest"` exposes as
// top-level Digest::ALGO classes (RMD160 autoloads on first reference); each maps
// onto the like-named go-ruby-digest constructor.
var digestAlgos = []string{"MD5", "SHA1", "SHA256", "SHA384", "SHA512", "RMD160"}

// DigestObj is an instance of a Digest algorithm class (Digest::SHA256.new): an
// incremental hasher accumulating input through #update / #<< and yielding its
// result with #hexdigest / #digest, backed by a go-ruby-digest Digest. Puppet's
// checksum_stream feeds file content through one of these before reading the hex
// digest.
type DigestObj struct {
	cls *RClass
	d   digest.Digest
}

// ToS renders the running digest in hex, matching MRI's Digest::Instance#to_s.
func (d *DigestObj) ToS() string { return d.d.HexFinish() }

// Inspect renders MRI's Digest::Instance#inspect form, #<Digest::ALGO: hex>.
func (d *DigestObj) Inspect() string { return "#<" + d.cls.name + ": " + d.d.HexFinish() + ">" }

func (d *DigestObj) Truthy() bool { return true }

// registerDigest installs the Digest module (require "digest") with the common
// algorithms as Digest::MD5/SHA1/SHA256/SHA384/SHA512/RMD160. Each class offers
// the one-shot class methods digest/hexdigest/base64digest/file/bubblebabble and
// the incremental instance protocol (new, update/<<, digest/hexdigest/
// base64digest and their ! finalizers, reset, ==, length/size, block_length/
// digest_length). The Digest(name) factory and Digest.bubblebabble round out the
// surface. All hashing is delegated to go-ruby-digest — byte-identical to MRI.
func (vm *VM) registerDigest() {
	d := newClass("Digest", nil)
	d.isModule = true
	vm.consts["Digest"] = d

	// Digest.bubblebabble(data) — the digest/bubblebabble extension's babble
	// encoding of a raw string (not a digest), e.g. "ximek-domex" for "abc".
	d.smethods["bubblebabble"] = &Method{name: "bubblebabble", owner: d,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			return object.NewString(digest.BubbleBabble([]byte(strArg(args[0]))))
		}}

	for _, name := range digestAlgos {
		vm.defineDigestClass(d, name)
	}

	// Kernel#Digest(name) — the factory that returns the Digest::ALGO *class* for
	// an algorithm name (Digest("SHA256") == Digest::SHA256), raising LoadError for
	// an unknown name as MRI's autoload-based const_missing does.
	vm.cObject.define("Digest", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		name := digestCanon(strArg(args[0]))
		if cls, ok := d.consts[name]; ok {
			return cls
		}
		return raise("LoadError", "library not found for class Digest::%s -- digest/%s", strArg(args[0]), name)
	})
}

// defineDigestClass installs one Digest::ALGO class (its constructor, one-shots
// and the instance protocol) under the Digest module.
func (vm *VM) defineDigestClass(mod *RClass, name string) {
	c := newClass("Digest::"+name, vm.cObject)
	mod.consts[name] = c

	// Digest::ALGO.new → a fresh incremental hasher. The algorithm is fixed by the
	// class, so New never errors here (name is one of digestAlgos).
	c.smethods["new"] = &Method{name: "new", owner: c,
		native: func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
			h, _ := digest.New(name)
			return &DigestObj{cls: c, d: h}
		}}

	// Class one-shots: Digest::ALGO.digest/hexdigest/base64digest(data). The
	// library's Sum family never errors for a known algorithm.
	c.smethods["digest"] = &Method{name: "digest", owner: c,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			out, _ := digest.Sum(name, []byte(base64Arg(args[0])))
			return object.NewString(string(out))
		}}
	c.smethods["hexdigest"] = &Method{name: "hexdigest", owner: c,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			out, _ := digest.HexSum(name, []byte(base64Arg(args[0])))
			return object.NewString(out)
		}}
	c.smethods["base64digest"] = &Method{name: "base64digest", owner: c,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			out, _ := digest.Base64Sum(name, []byte(base64Arg(args[0])))
			return object.NewString(out)
		}}

	// Digest::ALGO.file(path) returns a Digest::ALGO instance that has consumed the
	// file's content, so the usual reader chain (.hexdigest / .digest) works on it,
	// matching MRI. The library's SumFile reads and hashes the file in one call; we
	// feed the file's bytes into a fresh hasher so the returned instance is
	// resumable, raising Errno::ENOENT for a missing path as MRI does.
	c.smethods["file"] = &Method{name: "file", owner: c,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			path := base64Arg(args[0])
			data, err := os.ReadFile(path)
			if err != nil {
				return raise("Errno::ENOENT", "No such file or directory @ rb_sysopen - %s", path)
			}
			h, _ := digest.New(name)
			h.Update(data)
			return &DigestObj{cls: c, d: h}
		}}

	// Digest::ALGO.bubblebabble(data) — babble encoding of the algorithm's digest.
	c.smethods["bubblebabble"] = &Method{name: "bubblebabble", owner: c,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			out, _ := digest.SumBubbleBabble(name, []byte(base64Arg(args[0])))
			return object.NewString(out)
		}}

	vm.defineDigestInstance(c)
}

// defineDigestInstance wires the incremental instance protocol onto a Digest::ALGO
// class. update / << feed bytes and return self (so they chain); the readers emit
// the running digest without consuming it; the ! finalizers emit then reset; ==
// compares hex digests; length/size/block_length/digest_length report sizes.
func (vm *VM) defineDigestInstance(c *RClass) {
	// feed appends data and returns self so `d << a << b` and d.update(s).update(t)
	// both chain, matching MRI (and the existing equal?(d) self-identity test).
	feed := func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o := self.(*DigestObj)
		o.d.Update([]byte(strArg(args[0])))
		return o
	}
	c.define("update", feed)
	c.define("<<", feed)

	c.define("hexdigest", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o := digestFeedOptional(self, args)
		return object.NewString(o.d.HexFinish())
	})
	c.define("digest", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o := digestFeedOptional(self, args)
		return object.NewString(string(o.d.Finish()))
	})
	c.define("base64digest", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o := digestFeedOptional(self, args)
		return object.NewString(o.d.Base64Finish())
	})

	// The ! finalizers return the digest and then reset the hasher, matching MRI's
	// Digest::Instance#digest! / #hexdigest! / #base64digest!.
	c.define("hexdigest!", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		o := self.(*DigestObj)
		s := o.d.HexFinish()
		o.d.Reset()
		return object.NewString(s)
	})
	c.define("digest!", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		o := self.(*DigestObj)
		s := o.d.Finish()
		o.d.Reset()
		return object.NewString(string(s))
	})
	c.define("base64digest!", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		o := self.(*DigestObj)
		s := o.d.Base64Finish()
		o.d.Reset()
		return object.NewString(s)
	})

	c.define("reset", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		o := self.(*DigestObj)
		o.d.Reset()
		return o
	})

	// == compares hex digests: against another Digest instance's running digest or
	// a hex string, matching MRI's Digest::Instance#==.
	c.define("==", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o := self.(*DigestObj)
		switch other := args[0].(type) {
		case *DigestObj:
			return object.Bool(o.d.HexFinish() == other.d.HexFinish())
		case *object.String:
			return object.Bool(o.d.HexFinish() == other.Str())
		default:
			return object.Bool(false)
		}
	})

	c.define("to_s", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self.(*DigestObj).ToS())
	})
	c.define("inspect", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self.(*DigestObj).Inspect())
	})

	// length / size / digest_length all report the digest size; block_length the
	// algorithm's internal block size — Digest::Instance's size accessors.
	dlen := func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self.(*DigestObj).d.DigestLength()))
	}
	c.define("length", dlen)
	c.define("size", dlen)
	c.define("digest_length", dlen)
	c.define("block_length", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self.(*DigestObj).d.BlockLength()))
	})
}

// digestFeedOptional appends an optional data argument to the running digest
// (MRI's d.hexdigest(s) feeds s before reading), returning the DigestObj.
func digestFeedOptional(self object.Value, args []object.Value) *DigestObj {
	o := self.(*DigestObj)
	if len(args) > 0 {
		o.d.Update([]byte(strArg(args[0])))
	}
	return o
}

// hasCustomEq reports whether v is a builtin instance whose Ruby class defines
// its own `==` (rather than inheriting Object identity), so the OpEq fast path in
// binaryOp routes `==` through that method. Digest::Instance overrides `==` (it
// compares hex digests) and BCrypt::Password overrides it too (it compares a
// candidate secret against the stored hash), so both must dispatch their `==`
// rather than fall to pointer identity.
func hasCustomEq(_ *VM, v object.Value) bool {
	switch v.(type) {
	case *DigestObj, *BCryptPassword:
		return true
	case *Money:
		// Money#== compares fractional amount and currency via the go-ruby-money
		// library, not object identity, so it must dispatch its own ==.
		return true
	case *Currency:
		// Money::Currency#== compares by id, not identity.
		return true
	case *DryStruct:
		// Dry::Struct#== compares attributes by value (via go-ruby-dry-struct's
		// Struct.Eql), not object identity, so it must dispatch its own ==.
		return true
	case *ProtobufMessage, *ProtobufRepeatedField, *ProtobufMap:
		// Google::Protobuf messages and their RepeatedField / Map containers compare
		// by value (proto.Equal / element-wise) through the go-ruby-protobuf library,
		// not object identity, so each must dispatch its own ==.
		return true
	}
	return false
}

// digestCanon normalises a Digest algorithm name for the Digest(name) factory:
// uppercased and de-dashed, then mapping RIPEMD160 / RIPEMD-160 onto the RMD160
// the library and MRI register, mirroring go-ruby-digest's own New aliasing.
func digestCanon(name string) string {
	up := strings.ToUpper(strings.ReplaceAll(name, "-", ""))
	if up == "RIPEMD160" {
		return "RMD160"
	}
	return up
}
