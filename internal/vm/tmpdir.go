// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"fmt"
	"os"
	"path"
	"strings"
	"time"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerTmpdir installs the tmpdir standard library — the Dir.tmpdir and
// Dir.mktmpdir class methods added by `require "tmpdir"`. Dir already exists
// (registerDir); this layers the two extra singleton methods on top, matching
// MRI's stdlib/tmpdir.rb behaviour. It is wired through providedFeatures so the
// require returns true on the first call and false thereafter.
func (vm *VM) registerTmpdir() {
	cDir := vm.consts["Dir"].(*RClass)
	def := func(name string, fn NativeFn) { cDir.smethods[name] = &Method{name: name, owner: cDir, native: fn} }

	def("tmpdir", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(toSlash(systemTmpdir()))
	})

	def("mktmpdir", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		prefix, suffix := tmpdirPrefixSuffix(args)
		base := systemTmpdir()
		if len(args) > 1 {
			if _, isNil := args[1].(object.Nil); !isNil {
				base = strArg(args[1])
			}
		}
		dir := makeTmpdir(base, prefix, suffix)
		if blk == nil {
			return object.NewString(toSlash(dir))
		}
		// Block form yields the path and removes the tree afterwards, even on a
		// non-local exit, returning the block's value (MRI semantics).
		defer os.RemoveAll(dir)
		return vm.callBlock(blk, []object.Value{object.NewString(toSlash(dir))})
	})
}

// systemTmpdir returns the first usable temporary directory, honouring the
// TMPDIR, TMP and TEMP environment variables before falling back to /tmp, like
// MRI's Dir.tmpdir. ENV in the runtime is the process environment, so the lookup
// is os.Getenv directly.
func systemTmpdir() string {
	for _, name := range []string{"TMPDIR", "TMP", "TEMP"} {
		if v := os.Getenv(name); v != "" {
			if fi, err := os.Stat(v); err == nil && fi.IsDir() {
				return strings.TrimRight(v, "/")
			}
		}
	}
	return "/tmp"
}

// tmpdirPrefixSuffix decodes the first mktmpdir argument: nil or absent ->
// default prefix "d" and empty suffix; a String -> that prefix; a two-element
// Array [prefix, suffix] -> both parts. Matches MRI's prefix_suffix handling.
func tmpdirPrefixSuffix(args []object.Value) (prefix, suffix string) {
	if len(args) == 0 {
		return "d", ""
	}
	switch a := args[0].(type) {
	case object.Nil:
		return "d", ""
	case *object.Array:
		if len(a.Elems) > 0 {
			prefix = strArg(a.Elems[0])
		}
		if len(a.Elems) > 1 {
			suffix = strArg(a.Elems[1])
		}
		return prefix, suffix
	default:
		return strArg(args[0]), ""
	}
}

// tmpRandRetries bounds the unique-name attempts; a collision after this many
// tries indicates a broken temp directory rather than ordinary contention.
const tmpRandRetries = 100

// makeTmpdir creates a fresh directory under base named like MRI's
// "<prefix><YYYYMMDD>-<pid>-<random><suffix>", retrying on collision. It raises
// the same Errno errors as Dir.mkdir when the base is unwritable or missing.
func makeTmpdir(base, prefix, suffix string) string {
	stamp := tmpNow().Format("20060102")
	pid := osGetpid()
	var lastErr error
	for i := 0; i < tmpRandRetries; i++ {
		name := fmt.Sprintf("%s%s-%d-%s%s", prefix, stamp, pid, tmpRandToken(), suffix)
		full := path.Join(base, name)
		err := osMkdir(full, 0o700)
		if err == nil {
			return full
		}
		if os.IsExist(err) {
			lastErr = err
			continue // name collision — try a new random token
		}
		if os.IsNotExist(err) {
			raise("Errno::ENOENT", "No such file or directory @ dir_s_mkdir - %s", full)
		}
		raise("Errno::EACCES", "Permission denied @ dir_s_mkdir - %s", full)
	}
	raise("Errno::EEXIST", "File exists @ dir_s_mkdir - %s", lastErr)
	return "" // unreachable: raise never returns
}

// tmpRandToken returns a short lowercase-alphanumeric token, matching the look of
// MRI's randomised suffix (base-36-ish). It uses the crypto/rand seam so the
// caller never has to seed a PRNG.
func tmpRandToken() string {
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := secureBytes(6)
	out := make([]byte, len(b))
	for i, c := range b {
		out[i] = alphabet[int(c)%len(alphabet)]
	}
	return string(out)
}

// Seams over the clock and pid so the (otherwise time/pid-dependent) name format
// can be asserted deterministically in tests.
var (
	tmpNow   = time.Now
	osGetpid = os.Getpid
	osMkdir  = os.Mkdir
)
