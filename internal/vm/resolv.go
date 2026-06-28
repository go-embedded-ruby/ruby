// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"net"
	"strings"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerResolv installs the Resolv standard library (require "resolv"). The
// address-parsing surface is real and cheap: Resolv::IPv4 / Resolv::IPv6 parse,
// validate (via the ::Regex constants) and render dotted/colon addresses, and
// Resolv.getaddress resolves a literal IP without any networking. The DNS
// resolver itself needs real UDP/TCP sockets (a later round), so Resolv::DNS is
// constructable but its query methods (getresource(s)/getaddress/getname/each_*)
// raise NotImplementedError, and Resolv.getaddress raises ResolvError for a name
// that is not already a literal address. The constant/class tree Puppet walks
// (Resolv::DNS::Resource::IN::{A,AAAA,SRV,...} and the error classes) exists so
// load-time references resolve.
func (vm *VM) registerResolv() {
	std := vm.consts["StandardError"].(*RClass)

	resolv := newClass("Resolv", nil)
	resolv.isModule = true
	vm.consts["Resolv"] = resolv

	// --- Error classes ----------------------------------------------------------
	resolvErr := newClass("Resolv::ResolvError", std)
	resolv.consts["ResolvError"] = resolvErr
	resolv.consts["ResolvTimeout"] = newClass("Resolv::ResolvTimeout", resolvErr)

	// --- Resolv::IPv4 -----------------------------------------------------------
	ipv4 := newClass("Resolv::IPv4", vm.cObject)
	resolv.consts["IPv4"] = ipv4
	// Regex matches a dotted-quad with each octet 0..255 (anchored), accepting the
	// same set as MRI's Resolv::IPv4::Regex without reproducing its byte layout.
	ipv4.consts["Regex"] = vm.compileRegexp(`\A(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})\z`, "")
	ipv4.smethods["create"] = &Method{name: "create", owner: ipv4,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			return newIPv4(ipv4, parseIPv4(strArg(args[0])))
		}}
	ipv4.smethods["new"] = ipv4.smethods["create"]
	ipv4.define("to_s", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return getIvar(self, "@addr")
	})
	ipv4.define("to_str", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return getIvar(self, "@addr")
	})
	ipv4.define("inspect", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString("#<Resolv::IPv4 " + strArg(getIvar(self, "@addr")) + ">")
	})
	ipv4.define("==", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o, ok := args[0].(*RObject)
		if !ok || o.class != ipv4 {
			return object.Bool(false)
		}
		return object.Bool(strArg(getIvar(self, "@addr")) == strArg(getIvar(o, "@addr")))
	})

	// --- Resolv::IPv6 -----------------------------------------------------------
	ipv6 := newClass("Resolv::IPv6", vm.cObject)
	resolv.consts["IPv6"] = ipv6
	// Regex accepts the textual IPv6 forms; the create path re-validates via Go's
	// net.ParseIP, so the Regex only needs to be permissive over the valid set.
	ipv6.consts["Regex"] = vm.compileRegexp(`\A[0-9A-Fa-f:]+(?:%[-0-9A-Za-z._~]+)?\z`, "")
	ipv6.smethods["create"] = &Method{name: "create", owner: ipv6,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			return newIPv6(ipv6, parseIPv6(strArg(args[0])))
		}}
	ipv6.smethods["new"] = ipv6.smethods["create"]
	ipv6.define("to_s", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return getIvar(self, "@addr")
	})
	ipv6.define("to_str", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return getIvar(self, "@addr")
	})
	ipv6.define("inspect", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString("#<Resolv::IPv6 " + strArg(getIvar(self, "@addr")) + ">")
	})
	ipv6.define("==", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o, ok := args[0].(*RObject)
		if !ok || o.class != ipv6 {
			return object.Bool(false)
		}
		return object.Bool(strArg(getIvar(self, "@addr")) == strArg(getIvar(o, "@addr")))
	})

	// --- Resolv::Hosts (constructable shell; lookups need /etc/hosts I/O) --------
	hosts := newClass("Resolv::Hosts", vm.cObject)
	resolv.consts["Hosts"] = hosts
	hosts.consts["DefaultFileName"] = object.NewString("/etc/hosts")
	hosts.smethods["new"] = &Method{name: "new", owner: hosts,
		native: func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
			return &RObject{class: hosts, ivars: map[string]object.Value{}}
		}}

	// --- Resolv::DNS and its Resource constant tree -----------------------------
	registerResolvDNS(vm, resolv)

	// --- Module-level resolution helpers ----------------------------------------
	resolveNotImpl := func(what string) NativeFn {
		return func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
			return raise("NotImplementedError", "Resolv.%s needs real DNS sockets (not yet supported)", what)
		}
	}
	// getaddress is real for a literal IP — pure computation, no networking — and
	// raises ResolvError for a name that would require an actual DNS query.
	resolv.smethods["getaddress"] = &Method{name: "getaddress", owner: resolv,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			name := strArg(args[0])
			if isLiteralIP(name) {
				return object.NewString(name)
			}
			return raise("Resolv::ResolvError", "no address for %s", name)
		}}
	resolv.smethods["getaddresses"] = &Method{name: "getaddresses", owner: resolv,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			name := strArg(args[0])
			if isLiteralIP(name) {
				return &object.Array{Elems: []object.Value{object.NewString(name)}}
			}
			return &object.Array{Elems: nil} // MRI returns [] when nothing resolves
		}}
	for _, m := range []string{"getname", "getnames", "each_address", "each_name"} {
		resolv.smethods[m] = &Method{name: m, owner: resolv, native: resolveNotImpl(m)}
	}
}

// registerResolvDNS builds Resolv::DNS (a constructable resolver whose query
// methods raise NotImplementedError) and the Resource constant tree that Puppet
// names (Resolv::DNS::Resource::IN::{A,AAAA,SRV,...}).
func registerResolvDNS(vm *VM, resolv *RClass) {
	dns := newClass("Resolv::DNS", vm.cObject)
	resolv.consts["DNS"] = dns
	dns.smethods["new"] = &Method{name: "new", owner: dns,
		native: func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
			return &RObject{class: dns, ivars: map[string]object.Value{}}
		}}
	dns.smethods["open"] = dns.smethods["new"]
	queryNotImpl := func(what string) NativeFn {
		return func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
			return raise("NotImplementedError", "Resolv::DNS#%s needs real DNS sockets (not yet supported)", what)
		}
	}
	for _, m := range []string{
		"getresource", "getresources", "getaddress", "getaddresses",
		"getname", "getnames", "each_resource", "each_address", "each_name", "close",
	} {
		dns.define(m, queryNotImpl(m))
	}

	// Resource hierarchy: Resolv::DNS::Resource and the IN class set. Each leaf is
	// a plain class so `is_a?`/`==` against the constant works; instances are not
	// constructed by the (stubbed) query path yet.
	resource := newClass("Resolv::DNS::Resource", vm.cObject)
	dns.consts["Resource"] = resource
	in := newClass("Resolv::DNS::Resource::IN", resource)
	resource.consts["IN"] = in
	for _, rec := range []string{"A", "AAAA", "SRV", "CNAME", "NS", "MX", "PTR", "TXT", "SOA"} {
		in.consts[rec] = newClass("Resolv::DNS::Resource::IN::"+rec, resource)
	}

	// Resolv::DNS::Name — constructable from a dotted name (real, cheap).
	name := newClass("Resolv::DNS::Name", vm.cObject)
	dns.consts["Name"] = name
	name.smethods["create"] = &Method{name: "create", owner: name,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			o := &RObject{class: name, ivars: map[string]object.Value{}}
			o.ivars["@name"] = object.NewString(strArg(args[0]))
			return o
		}}
	name.define("to_s", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return getIvar(self, "@name")
	})
}

// newIPv4 builds a Resolv::IPv4 RObject storing the canonical dotted string.
func newIPv4(cls *RClass, addr string) object.Value {
	o := &RObject{class: cls, ivars: map[string]object.Value{}}
	o.ivars["@addr"] = object.NewString(addr)
	return o
}

// newIPv6 builds a Resolv::IPv6 RObject storing the canonical colon string.
func newIPv6(cls *RClass, addr string) object.Value {
	o := &RObject{class: cls, ivars: map[string]object.Value{}}
	o.ivars["@addr"] = object.NewString(addr)
	return o
}

// parseIPv4 validates a dotted-quad and returns its canonical form, raising
// ArgumentError on anything that is not a valid IPv4 address (matching MRI's
// Resolv::IPv4.create, which rejects malformed input).
func parseIPv4(s string) string {
	ip := net.ParseIP(s)
	if ip == nil || ip.To4() == nil || strings.Contains(s, ":") {
		raise("ArgumentError", "cannot interpret as IPv4 address: %s", s)
	}
	return ip.To4().String()
}

// parseIPv6 validates a textual IPv6 address and returns its canonical form,
// raising ArgumentError otherwise.
func parseIPv6(s string) string {
	// Strip an optional zone id (%eth0) before parsing, re-appending it after.
	base, zone := s, ""
	if i := strings.IndexByte(s, '%'); i >= 0 {
		base, zone = s[:i], s[i:]
	}
	ip := net.ParseIP(base)
	if ip == nil || ip.To4() != nil || ip.To16() == nil {
		raise("ArgumentError", "cannot interpret as IPv6 address: %s", s)
	}
	return ip.To16().String() + zone
}

// isLiteralIP reports whether name is already a numeric IPv4 or IPv6 address, in
// which case Resolv.getaddress can answer without any DNS query.
func isLiteralIP(name string) bool {
	return net.ParseIP(name) != nil
}
