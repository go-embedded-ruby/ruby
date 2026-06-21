package vm

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"hash"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerDigest installs the Digest module (require "digest") with the common
// algorithms as Digest::MD5/SHA1/SHA256/SHA512, each offering hexdigest, digest
// and base64digest. Backed by Go's crypto/* — byte-identical to MRI.
func (vm *VM) registerDigest() {
	d := newClass("Digest", nil)
	d.isModule = true
	vm.consts["Digest"] = d

	algo := func(name string, newHash func() hash.Hash) {
		c := newClass("Digest::"+name, vm.cObject)
		d.consts[name] = c
		sum := func(args []object.Value) []byte {
			h := newHash()
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
	algo("MD5", md5.New)
	algo("SHA1", sha1.New)
	algo("SHA256", sha256.New)
	algo("SHA512", sha512.New)
}
