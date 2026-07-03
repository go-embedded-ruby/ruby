// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"math/big"

	libipaddr "github.com/go-ruby-ipaddr/ipaddr"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// IPAddr binds github.com/go-ruby-ipaddr/ipaddr — the pure-Go, MRI-4.0.5-faithful
// port of Ruby's `ipaddr` standard library — into rbgo. The library owns all of
// the parsing, big.Int-backed masking, the to_s / to_string / cidr / inspect /
// netmask rendering, the set predicates, the bitwise operators and the
// Comparable comparison semantics byte-for-byte; this file is only the thin
// shell that maps Ruby values onto the library's any-typed operands and exposes
// the class/method surface MRI's `require "ipaddr"` provides.
//
// A few methods are extensions beyond MRI 4.0.5 and are documented as such where
// they are defined: `multicast?` (IPAddr#multicast? does not exist in MRI 4.0.5),
// `^` / `xor` (MRI has no IPAddr#^) and `each` (MRI has no IPAddr#each — the
// library exposes it as the idiomatic Go iteration over to_range). MRI's
// IPAddr#family returns an OS-dependent integer (AF_INET6 is 10 on Linux but 30
// on the BSDs/macOS); the library exposes Linux's canonical value, so callers
// should prefer ipv4? / ipv6? for a portable family test.

// IPAddr is the Ruby wrapper around a go-ruby-ipaddr IPAddr.
type IPAddr struct{ ip *libipaddr.IPAddr }

func (a *IPAddr) ToS() string     { return a.ip.ToS() }
func (a *IPAddr) Inspect() string { return a.ip.Inspect() }
func (a *IPAddr) Truthy() bool    { return true }

// raiseIPAddrErr re-raises a library error as the matching Ruby exception under
// IPAddr, reproducing MRI's message verbatim. The Go error types map one-to-one
// onto the IPAddr:: error classes (InvalidPrefixError < InvalidAddressError <
// Error < ArgumentError in MRI). Anything else becomes a plain RuntimeError. It
// never returns when err is non-nil.
func raiseIPAddrErr(err error) {
	if err == nil {
		return
	}
	switch err.(type) {
	case *libipaddr.InvalidPrefixError:
		raise("IPAddr::InvalidPrefixError", "%s", err.Error())
	case *libipaddr.InvalidAddressError:
		raise("IPAddr::InvalidAddressError", "%s", err.Error())
	case *libipaddr.AddressFamilyError:
		raise("IPAddr::AddressFamilyError", "%s", err.Error())
	default:
		// The library only ever returns the four IPAddr error kinds; a bare *Error
		// (the base, e.g. an uncoercible include? operand) and any unforeseen error
		// surface as IPAddr::Error, the base class.
		raise("IPAddr::Error", "%s", err.Error())
	}
}

// ipOK wraps a (result, error) library call: it re-raises any error as the
// matching Ruby exception, then returns the wrapped Ruby value.
func ipOK(ip *libipaddr.IPAddr, err error) object.Value {
	raiseIPAddrErr(err)
	return &IPAddr{ip: ip}
}

// ipOperand maps a Ruby operand for the bitwise/comparison combinators onto a
// value the library accepts: an IPAddr stays an IPAddr, a String stays a string,
// an Integer/Bignum becomes a *big.Int (the library coerces it against the
// receiver's family). Anything else raises TypeError, as MRI's coercion does.
func ipOperand(v object.Value) any {
	switch x := v.(type) {
	case *IPAddr:
		return x.ip
	case *object.String:
		return string(x.Bytes())
	case object.Integer:
		return big.NewInt(int64(x))
	case *object.Bignum:
		return new(big.Int).Set(x.I)
	}
	raise("TypeError", "value must be an IPAddr, a String or an Integer")
	panic("unreachable")
}

// ipIncludeOperand maps the operand of IPAddr#include? / #=== onto a value the
// library coerces. The four known kinds map as in ipOperand; any other Ruby
// value is forwarded as an unsupported operand so the library reports the bad
// coercion as an IPAddr::Error (MRI also raises from include? for an operand it
// cannot coerce), rather than the binding pre-judging it.
func ipIncludeOperand(v object.Value) any {
	switch x := v.(type) {
	case *IPAddr:
		return x.ip
	case *object.String:
		return string(x.Bytes())
	case object.Integer:
		return big.NewInt(int64(x))
	case *object.Bignum:
		return new(big.Int).Set(x.I)
	}
	return v // an unsupported operand -> the library raises IPAddr::Error
}

// ipCmpOperand maps the operand of IPAddr#<=> onto a value the library's Cmp
// coerces (an IPAddr, a String or an Integer/Bignum), reporting ok=false for an
// uncoercible operand so #<=> can return nil (MRI's Comparable contract) rather
// than raising — the difference from ipOperand, which raises for the bitwise
// operators.
func ipCmpOperand(v object.Value) (any, bool) {
	switch x := v.(type) {
	case *IPAddr:
		return x.ip, true
	case *object.String:
		return string(x.Bytes()), true
	case object.Integer:
		return big.NewInt(int64(x)), true
	case *object.Bignum:
		return new(big.Int).Set(x.I), true
	}
	return nil, false
}

// ipBytes maps a Ruby String of packed network bytes (the new_ntoh / ntop
// argument) into a []byte, raising TypeError for a non-String.
func ipBytes(v object.Value) []byte {
	s, ok := v.(*object.String)
	if !ok {
		raise("TypeError", "value must be a String of packed bytes")
	}
	return append([]byte(nil), s.Bytes()...)
}

// ipaddrOp implements the IPAddr operator fast path reached from binary(): ip + n
// and ip - n shift the address by a whole-number offset (mirroring MRI's
// IPAddr#+ / IPAddr#-). The right operand is an Integer/Bignum offset.
func ipaddrOp(op bytecode.Op, a *IPAddr, b object.Value) object.Value {
	switch op {
	case bytecode.OpAdd:
		return ipOK(a.ip.Add(ipOffset(b)))
	case bytecode.OpSub:
		return ipOK(a.ip.Sub(ipOffset(b)))
	}
	return raise("NoMethodError", "undefined method '%s' for an IPAddr", op)
}

// ipOffset reads the integer offset operand of IPAddr#+ / IPAddr#-. The library's
// Add/Sub take an int64, so a Bignum offset (which rbgo only produces when the
// value is outside int64 range) cannot be applied and raises RangeError rather
// than silently truncating. A non-integer raises TypeError.
func ipOffset(v object.Value) int64 {
	switch v.(type) {
	case object.Integer:
		return int64(v.(object.Integer))
	case *object.Bignum:
		raise("RangeError", "offset out of range")
	}
	raise("TypeError", "offset must be an Integer")
	panic("unreachable")
}

// registerIPAddr installs the IPAddr class, its nested error classes, its
// constructors and instance methods (require "ipaddr"). It runs eagerly at boot,
// after the exception hierarchy is in place (IPAddr::Error < ArgumentError).
func (vm *VM) registerIPAddr() {
	vm.registerIPAddrErrors()

	cls := newClass("IPAddr", vm.cObject)
	vm.cIPAddr = cls
	vm.consts["IPAddr"] = cls
	// Comparable gives <, <=, >, >= from #<=> (IPAddr includes Comparable in MRI);
	// the prelude registered Comparable before this runs.
	if cmp, ok := vm.consts["Comparable"].(*RClass); ok {
		cls.includes = append(cls.includes, cmp)
	}
	// Re-attach the error classes as nested constants so Ruby `IPAddr::Error`
	// (etc.) resolves them, the CSV::Row / ExceptionForMatrix pattern.
	cls.consts["Error"] = vm.cIPAddrError
	cls.consts["InvalidAddressError"] = vm.cIPAddrInvalidAddressError
	cls.consts["InvalidPrefixError"] = vm.cIPAddrInvalidPrefixError
	cls.consts["AddressFamilyError"] = vm.cIPAddrAddressFamilyError

	sm := func(name string, fn NativeFn) { cls.smethods[name] = &Method{name: name, owner: cls, native: fn} }

	// IPAddr.new(addr) parses a string ("addr", "addr/prefixlen", "addr/netmask");
	// IPAddr.new(integer, family) builds from a packed integer and a family int
	// (Socket::AF_INET == 2 is IPv4, anything else is IPv6 — MRI's AF_INET6 is
	// OS-dependent, so only the AF_INET case is matched exactly).
	sm("new", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
		}
		var n *big.Int
		switch first := args[0].(type) {
		case *object.String:
			return ipOK(libipaddr.New(string(first.Bytes())))
		case object.Integer:
			n = big.NewInt(int64(first))
		case *object.Bignum:
			n = new(big.Int).Set(first.I)
		default:
			raise("TypeError", "IPAddr.new expects a String or an Integer")
		}
		// IPAddr.new(integer, family): a missing or AF_INET (2) family is IPv4, any
		// other family int is IPv6 (MRI's AF_INET6 is OS-dependent).
		fam := libipaddr.AFInet
		if len(args) > 1 && int64(args[1].(object.Integer)) != int64(libipaddr.AFInet) {
			fam = libipaddr.AFInet6
		}
		return ipOK(libipaddr.NewFromInt(n, fam))
	})
	// IPAddr.new_ntoh(packed) builds from a packed network-byte-ordered address.
	sm("new_ntoh", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return ipOK(libipaddr.NewNtoh(ipBytes(args[0])))
	})
	// IPAddr.ntop(packed) converts a packed address to its readable form.
	// Honours MRI's encoding precedence (a non-BINARY String raises
	// InvalidAddressError before any length check) via the String's encoding.
	sm("ntop", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		str, ok := args[0].(*object.String)
		if !ok {
			raise("TypeError", "value must be a String of packed bytes")
		}
		s, err := libipaddr.NtopString(string(str.Bytes()), str.EncName())
		raiseIPAddrErr(err)
		return object.NewString(s)
	})

	d := func(name string, fn NativeFn) { cls.define(name, fn) }
	self := func(v object.Value) *IPAddr { return v.(*IPAddr) }

	// String / rendering.
	d("to_s", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).ip.ToS())
	})
	d("to_string", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).ip.ToString())
	})
	d("cidr", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).ip.Cidr())
	})
	d("inspect", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).ip.Inspect())
	})
	d("netmask", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).ip.Netmask())
	})

	// Prefix / masking.
	d("prefix", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).ip.Prefix()))
	})
	d("prefix=", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		_, err := self(v).ip.SetPrefix(int(ipInt(args[0])))
		raiseIPAddrErr(err)
		return args[0]
	})
	d("mask", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		switch x := args[0].(type) {
		case object.Integer:
			return ipOK(self(v).ip.MaskLen(int(x)))
		case *object.String:
			return ipOK(self(v).ip.Mask(string(x.Bytes())))
		}
		raise("TypeError", "mask expects an Integer prefix length or a String netmask")
		return object.NilV
	})

	// Membership / iteration.
	includeFn := func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		ok, err := self(v).ip.Include(ipIncludeOperand(args[0]))
		raiseIPAddrErr(err)
		return object.Bool(ok)
	}
	d("include?", includeFn)
	d("===", includeFn)
	d("to_range", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		lo, hi, err := self(v).ip.ToRange()
		raiseIPAddrErr(err)
		return &object.Range{Lo: &IPAddr{ip: lo}, Hi: &IPAddr{ip: hi}, Exclusive: false}
	})
	// each is an EXTENSION beyond MRI 4.0.5 (MRI's IPAddr has no #each); the
	// library exposes it as iteration over to_range, lowest address first.
	d("each", func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (each)")
		}
		err := self(v).ip.Each(func(e *libipaddr.IPAddr) error {
			vm.callBlock(blk, []object.Value{&IPAddr{ip: e}})
			return nil
		})
		raiseIPAddrErr(err)
		return v
	})

	// Bitwise operators / arithmetic.
	d("&", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return ipOK(self(v).ip.And(ipOperand(args[0])))
	})
	d("|", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return ipOK(self(v).ip.Or(ipOperand(args[0])))
	})
	d("~", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return ipOK(self(v).ip.Not())
	})
	// ^ / xor are EXTENSIONS beyond MRI 4.0.5 (MRI has no IPAddr#^).
	xorFn := func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return ipOK(self(v).ip.Xor(ipOperand(args[0])))
	}
	d("^", xorFn)
	d("xor", xorFn)
	d("+", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return ipOK(self(v).ip.Add(ipOffset(args[0])))
	})
	d("-", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return ipOK(self(v).ip.Sub(ipOffset(args[0])))
	})
	d("succ", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return ipOK(self(v).ip.Succ())
	})

	// Comparison / identity.
	d("<=>", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		// MRI's IPAddr#<=> coerces an IPAddr, a String or an Integer operand (an
		// uncoercible operand, or a different family, yields nil). The library's Cmp
		// performs the same coercion, so the operand is forwarded as-is.
		o, ok := ipCmpOperand(args[0])
		if !ok {
			return object.NilV
		}
		res, comparable := self(v).ip.Cmp(o)
		if !comparable {
			return object.NilV
		}
		return object.IntValue(int64(res))
	})
	d("==", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		o, ok := args[0].(*IPAddr)
		return object.Bool(ok && self(v).ip.Eql(o.ip))
	})
	d("eql?", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		o, ok := args[0].(*IPAddr)
		return object.Bool(ok && self(v).ip.Eql(o.ip))
	})
	d("hash", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NormInt(new(big.Int).SetUint64(self(v).ip.Hash()))
	})

	// Predicates.
	d("ipv4?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).ip.Ipv4())
	})
	d("ipv6?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).ip.Ipv6())
	})
	d("loopback?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).ip.Loopback())
	})
	d("private?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).ip.Private())
	})
	d("link_local?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).ip.LinkLocal())
	})
	// multicast? is an EXTENSION beyond MRI 4.0.5 (MRI's IPAddr has no #multicast?).
	d("multicast?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).ip.Multicast())
	})
	d("ipv4_mapped?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).ip.IsIpv4Mapped())
	})
	d("ipv4_compat?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).ip.IsIpv4Compat())
	})

	// Conversions.
	d("to_i", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NormInt(self(v).ip.ToI())
	})
	d("family", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(int(self(v).ip.Family())))
	})
	d("native", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return ipOK(self(v).ip.Native())
	})
	d("ipv4_mapped", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return ipOK(self(v).ip.Ipv4Mapped())
	})
	d("ipv4_compat", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return ipOK(self(v).ip.Ipv4Compat())
	})
	d("hton", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		b, err := self(v).ip.HtonString()
		raiseIPAddrErr(err)
		return object.NewString(string(b))
	})
}

// ipInt reads an Integer argument as an int64, raising TypeError otherwise.
func ipInt(v object.Value) int64 {
	i, ok := v.(object.Integer)
	if !ok {
		raise("TypeError", "expected an Integer")
	}
	return int64(i)
}

// registerIPAddrErrors installs the IPAddr error classes, mirroring MRI's
// hierarchy: IPAddr::Error < ArgumentError, IPAddr::InvalidAddressError < Error,
// IPAddr::InvalidPrefixError < InvalidAddressError, IPAddr::AddressFamilyError <
// Error. Each is registered under its qualified top-level name (so a re-raised
// library error's exceptionObject lookup finds the same class) and re-attached
// as a nested IPAddr constant in registerIPAddr.
func (vm *VM) registerIPAddrErrors() {
	arg := vm.consts["ArgumentError"].(*RClass)

	mk := func(short string, parent *RClass) *RClass {
		cls := newClass("IPAddr::"+short, parent)
		vm.consts["IPAddr::"+short] = cls
		return cls
	}
	vm.cIPAddrError = mk("Error", arg)
	vm.cIPAddrInvalidAddressError = mk("InvalidAddressError", vm.cIPAddrError)
	vm.cIPAddrInvalidPrefixError = mk("InvalidPrefixError", vm.cIPAddrInvalidAddressError)
	vm.cIPAddrAddressFamilyError = mk("AddressFamilyError", vm.cIPAddrError)
}
