// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	resolv "github.com/go-ruby-resolv/resolv"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerResolv installs the Resolv standard library (require "resolv"). The
// whole pure-compute DNS surface is delegated to github.com/go-ruby-resolv/resolv
// — an MRI-4.0.5-faithful port of the address, name, message and hosts logic:
//
//   - Resolv::IPv4 / Resolv::IPv6 parse, validate (via the ::Regex constants,
//     whose source is the library's MRI-exact matcher) and render dotted/colon
//     addresses, reusing the library's canonical compression (e.g. ::/1::1).
//   - Resolv::DNS::Name carries the dotted labels plus the trailing-dot absolute
//     flag, comparing case-insensitively as MRI does.
//   - Resolv::DNS::Message encodes to and decodes from the RFC 1035 wire format
//     (with 0xC0 name compression), byte-exact against MRI's Message#encode.
//   - Resolv::DNS::Resource::IN::{A,AAAA,MX,CNAME,NS,PTR,TXT,SOA,SRV,HINFO} are
//     constructable records with their MRI accessors, populated on decode.
//   - Resolv::Hosts parses /etc/hosts content (the library does no file I/O — the
//     host supplies the string) and answers getaddress(es)/getname(s).
//
// Only the socket-backed resolution stays a stub: Resolv::DNS's query methods
// (getresource(s)/getaddress/getname/each_*) need real UDP/TCP and raise
// NotImplementedError, and Resolv.getaddress answers only a literal IP, raising
// ResolvError for a name that would require an actual DNS query.
func (vm *VM) registerResolv() {
	std := vm.consts["StandardError"].(*RClass)

	resolvMod := newClass("Resolv", nil)
	resolvMod.isModule = true
	vm.consts["Resolv"] = resolvMod

	// --- Error classes ----------------------------------------------------------
	resolvErr := newClass("Resolv::ResolvError", std)
	resolvMod.consts["ResolvError"] = resolvErr
	resolvMod.consts["ResolvTimeout"] = newClass("Resolv::ResolvTimeout", resolvErr)

	registerResolvIPv4(vm, resolvMod)
	registerResolvIPv6(vm, resolvMod)
	registerResolvHosts(vm, resolvMod)
	registerResolvDNS(vm, resolvMod)

	// --- Module-level resolution helpers ----------------------------------------
	resolveNotImpl := func(what string) NativeFn {
		return func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
			return raise("NotImplementedError", "Resolv.%s needs real DNS sockets (not yet supported)", what)
		}
	}
	// getaddress is real for a literal IP — pure computation, no networking — and
	// raises ResolvError for a name that would require an actual DNS query.
	resolvMod.smethods["getaddress"] = &Method{name: "getaddress", owner: resolvMod,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			name := strArg(args[0])
			if isLiteralIP(name) {
				return object.NewString(name)
			}
			return raise("Resolv::ResolvError", "no address for %s", name)
		}}
	resolvMod.smethods["getaddresses"] = &Method{name: "getaddresses", owner: resolvMod,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			name := strArg(args[0])
			if isLiteralIP(name) {
				return object.NewArray(object.NewString(name))
			}
			return object.NewArray() // MRI returns [] when nothing resolves
		}}
	for _, m := range []string{"getname", "getnames", "each_address", "each_name"} {
		resolvMod.smethods[m] = &Method{name: m, owner: resolvMod, native: resolveNotImpl(m)}
	}
}

// registerResolvIPv4 wires Resolv::IPv4 over resolv.IPv4: create/new parse and
// canonicalise a dotted quad, to_s/to_str/inspect render it, and the Regex
// constant is the library's MRI-exact matcher.
func registerResolvIPv4(vm *VM, resolvMod *RClass) {
	ipv4 := newClass("Resolv::IPv4", vm.cObject)
	resolvMod.consts["IPv4"] = ipv4
	ipv4.consts["Regex"] = vm.compileRegexp(resolv.IPv4Regex.String(), "")
	ipv4.smethods["create"] = &Method{name: "create", owner: ipv4,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			ip, err := resolv.CreateIPv4(strArg(args[0]))
			if err != nil {
				return raise("ArgumentError", "cannot interpret as IPv4 address: %s", strArg(args[0]))
			}
			return newResolvAddr(ipv4, ip.String())
		}}
	ipv4.smethods["new"] = ipv4.smethods["create"]
	defineResolvAddrMethods(ipv4, "Resolv::IPv4")
}

// registerResolvIPv6 wires Resolv::IPv6 over resolv.IPv6, reusing the library's
// canonical "::" compression (including the first-run rule) and Regex matcher.
func registerResolvIPv6(vm *VM, resolvMod *RClass) {
	ipv6 := newClass("Resolv::IPv6", vm.cObject)
	resolvMod.consts["IPv6"] = ipv6
	ipv6.consts["Regex"] = vm.compileRegexp(resolv.IPv6Regex.String(), "")
	ipv6.smethods["create"] = &Method{name: "create", owner: ipv6,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			// MRI's Resolv::IPv6.create accepts an optional %zone the library's
			// CreateIPv6 (matching the spec) rejects, so strip-and-reattach a zone id.
			s := strArg(args[0])
			base, zone := splitZone(s)
			ip, err := resolv.CreateIPv6(base)
			if err != nil {
				return raise("ArgumentError", "cannot interpret as IPv6 address: %s", s)
			}
			return newResolvAddr(ipv6, ip.String()+zone)
		}}
	ipv6.smethods["new"] = ipv6.smethods["create"]
	defineResolvAddrMethods(ipv6, "Resolv::IPv6")
}

// defineResolvAddrMethods installs the shared IPv4/IPv6 instance methods over the
// canonical address stored in @addr.
func defineResolvAddrMethods(cls *RClass, clsName string) {
	cls.define("to_s", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return getIvar(self, "@addr")
	})
	cls.define("to_str", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return getIvar(self, "@addr")
	})
	cls.define("inspect", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString("#<" + clsName + " " + strArg(getIvar(self, "@addr")) + ">")
	})
	cls.define("==", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o, ok := args[0].(*RObject)
		if !ok || o.class != cls {
			return object.Bool(false)
		}
		return object.Bool(strArg(getIvar(self, "@addr")) == strArg(getIvar(o, "@addr")))
	})
}

// registerResolvHosts wires Resolv::Hosts over resolv.ParseHosts. MRI reads
// /etc/hosts; the library does no file I/O, so Resolv::Hosts.new accepts the
// hosts-file *content* directly (defaulting to an empty table) and parses it.
func registerResolvHosts(vm *VM, resolvMod *RClass) {
	hosts := newClass("Resolv::Hosts", vm.cObject)
	resolvMod.consts["Hosts"] = hosts
	hosts.consts["DefaultFileName"] = object.NewString(resolv.DefaultHostsFileName)
	hosts.smethods["new"] = &Method{name: "new", owner: hosts,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			content := ""
			if len(args) > 0 {
				content = strArg(args[0])
			}
			o := &RObject{class: hosts, ivars: map[string]object.Value{}}
			o.ivars["@table"] = &resolvBox{hosts: resolv.ParseHosts(content)}
			return o
		}}
	hostsTable := func(self object.Value) *resolv.Hosts {
		if b, ok := getIvar(self, "@table").(*resolvBox); ok && b.hosts != nil {
			return b.hosts
		}
		return resolv.ParseHosts("")
	}
	hosts.define("getaddress", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		addr, err := hostsTable(self).GetAddress(strArg(args[0]))
		if err != nil {
			return raise("Resolv::ResolvError", "no address for %s", strArg(args[0]))
		}
		return object.NewString(addr)
	})
	hosts.define("getaddresses", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return strSliceToArray(hostsTable(self).GetAddresses(strArg(args[0])))
	})
	hosts.define("getname", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		name, err := hostsTable(self).GetName(strArg(args[0]))
		if err != nil {
			return raise("Resolv::ResolvError", "no name for %s", strArg(args[0]))
		}
		return object.NewString(name)
	})
	hosts.define("getnames", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return strSliceToArray(hostsTable(self).GetNames(strArg(args[0])))
	})
}

// registerResolvDNS builds Resolv::DNS (a constructable resolver whose query
// methods raise NotImplementedError, as they need real sockets) and the wired
// compute primitives below it: Resolv::DNS::Name, Resolv::DNS::Message (encode/
// decode) and the Resolv::DNS::Resource::IN::* record tree.
func registerResolvDNS(vm *VM, resolvMod *RClass) {
	dns := newClass("Resolv::DNS", vm.cObject)
	resolvMod.consts["DNS"] = dns
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

	resource := newClass("Resolv::DNS::Resource", vm.cObject)
	dns.consts["Resource"] = resource
	in := newClass("Resolv::DNS::Resource::IN", resource)
	resource.consts["IN"] = in

	registerResolvName(vm, dns)
	recordClasses := registerResolvRecords(vm, in, resource)
	registerResolvMessage(vm, dns, recordClasses)
}

// registerResolvName wires Resolv::DNS::Name over resolv.Name: create from a
// dotted string, to_s, length, absolute? and case-insensitive ==.
func registerResolvName(vm *VM, dns *RClass) {
	name := newClass("Resolv::DNS::Name", vm.cObject)
	dns.consts["Name"] = name
	name.smethods["create"] = &Method{name: "create", owner: name,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			return newResolvName(name, resolv.NewName(strArg(args[0])))
		}}
	name.define("to_s", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(resolvNameOf(self).String())
	})
	name.define("length", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(resolvNameOf(self).Length()))
	})
	name.define("absolute?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(resolvNameOf(self).Absolute)
	})
	name.define("==", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o, ok := args[0].(*RObject)
		if !ok || o.class != name {
			return object.Bool(false)
		}
		return object.Bool(resolvNameOf(self).Equal(resolvNameOf(o)))
	})
	name.define("eql?", name.methods["=="].native)
}

// resolvRecordSpec describes one Resolv::DNS::Resource::IN::* leaf: its name, its
// library TYPE value, a constructor from Ruby args, and the accessor methods that
// read a decoded record's RDATA.
type resolvRecordSpec struct {
	name      string
	typ       uint16
	construct func(vm *VM, cls *RClass, args []object.Value) object.Value
	define    func(cls *RClass)
}

// registerResolvRecords builds the Resolv::DNS::Resource::IN::* record classes,
// each constructable (new) and carrying its MRI accessors, and returns a map from
// library TYPE value to the leaf class so decode can re-wrap records.
func registerResolvRecords(vm *VM, in, resource *RClass) map[uint16]*RClass {
	byType := map[uint16]*RClass{}
	for _, spec := range resolvRecordSpecs() {
		cls := newClass("Resolv::DNS::Resource::IN::"+spec.name, resource)
		in.consts[spec.name] = cls
		byType[spec.typ] = cls
		if spec.construct != nil {
			c := spec.construct
			cls.smethods["new"] = &Method{name: "new", owner: cls,
				native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
					return c(vm, cls, args)
				}}
		}
		if spec.define != nil {
			spec.define(cls)
		}
	}
	return byType
}

// resolvRecordSpecs lists the IN record types rbgo wires, in MRI's constant set.
func resolvRecordSpecs() []resolvRecordSpec {
	return []resolvRecordSpec{
		{name: "A", typ: resolv.TypeA,
			construct: func(vm *VM, cls *RClass, args []object.Value) object.Value {
				ip := resolvIPv4Of(vm, args[0])
				return newRecord(cls, &resolv.A{Address: ip})
			},
			define: func(cls *RClass) {
				cls.define("address", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
					return newResolvAddr(vm.consts["Resolv"].(*RClass).consts["IPv4"].(*RClass),
						recordOf(self).(*resolv.A).Address.String())
				})
			}},
		{name: "AAAA", typ: resolv.TypeAAAA,
			construct: func(vm *VM, cls *RClass, args []object.Value) object.Value {
				ip := resolvIPv6Of(vm, args[0])
				return newRecord(cls, &resolv.AAAA{Address: ip})
			},
			define: func(cls *RClass) {
				cls.define("address", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
					return newResolvAddr(vm.consts["Resolv"].(*RClass).consts["IPv6"].(*RClass),
						recordOf(self).(*resolv.AAAA).Address.String())
				})
			}},
		{name: "MX", typ: resolv.TypeMX,
			construct: func(vm *VM, cls *RClass, args []object.Value) object.Value {
				return newRecord(cls, &resolv.MX{
					Preference: uint16(intArg(args[0])),
					Exchange:   resolvNameArg(vm, args[1]),
				})
			},
			define: func(cls *RClass) {
				cls.define("preference", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
					return object.IntValue(int64(recordOf(self).(*resolv.MX).Preference))
				})
				cls.define("exchange", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
					return newResolvName(vm.dnsNameClass(), recordOf(self).(*resolv.MX).Exchange)
				})
			}},
		{name: "CNAME", typ: resolv.TypeCNAME, construct: domainNameCtor(resolv.NewCNAME), define: domainNameAccessor(func(r resolv.Resource) resolv.Name { return r.(*resolv.CNAME).Name })},
		{name: "NS", typ: resolv.TypeNS, construct: domainNameCtor(resolv.NewNS), define: domainNameAccessor(func(r resolv.Resource) resolv.Name { return r.(*resolv.NS).Name })},
		{name: "PTR", typ: resolv.TypePTR, construct: domainNameCtor(resolv.NewPTR), define: domainNameAccessor(func(r resolv.Resource) resolv.Name { return r.(*resolv.PTR).Name })},
		{name: "TXT", typ: resolv.TypeTXT,
			construct: func(_ *VM, cls *RClass, args []object.Value) object.Value {
				strs := make([]string, len(args))
				for i, a := range args {
					strs[i] = strArg(a)
				}
				return newRecord(cls, &resolv.TXT{Strings: strs})
			},
			define: func(cls *RClass) {
				cls.define("strings", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
					return strSliceToArray(recordOf(self).(*resolv.TXT).Strings)
				})
				cls.define("data", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
					strs := recordOf(self).(*resolv.TXT).Strings
					if len(strs) == 0 {
						return object.NewString("")
					}
					return object.NewString(strs[0])
				})
			}},
		{name: "SOA", typ: resolv.TypeSOA,
			construct: func(vm *VM, cls *RClass, args []object.Value) object.Value {
				return newRecord(cls, &resolv.SOA{
					MName:   resolvNameArg(vm, args[0]),
					RName:   resolvNameArg(vm, args[1]),
					Serial:  uint32(intArg(args[2])),
					Refresh: uint32(intArg(args[3])),
					Retry:   uint32(intArg(args[4])),
					Expire:  uint32(intArg(args[5])),
					Minimum: uint32(intArg(args[6])),
				})
			},
			define: func(cls *RClass) {
				cls.define("mname", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
					return newResolvName(vm.dnsNameClass(), recordOf(self).(*resolv.SOA).MName)
				})
				cls.define("rname", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
					return newResolvName(vm.dnsNameClass(), recordOf(self).(*resolv.SOA).RName)
				})
				for _, f := range []struct {
					name string
					get  func(*resolv.SOA) uint32
				}{
					{"serial", func(s *resolv.SOA) uint32 { return s.Serial }},
					{"refresh", func(s *resolv.SOA) uint32 { return s.Refresh }},
					{"retry", func(s *resolv.SOA) uint32 { return s.Retry }},
					{"expire", func(s *resolv.SOA) uint32 { return s.Expire }},
					{"minimum", func(s *resolv.SOA) uint32 { return s.Minimum }},
				} {
					get := f.get
					cls.define(f.name, func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
						return object.IntValue(int64(get(recordOf(self).(*resolv.SOA))))
					})
				}
			}},
		{name: "SRV", typ: resolv.TypeSRV,
			construct: func(vm *VM, cls *RClass, args []object.Value) object.Value {
				return newRecord(cls, &resolv.SRV{
					Priority: uint16(intArg(args[0])),
					Weight:   uint16(intArg(args[1])),
					Port:     uint16(intArg(args[2])),
					Target:   resolvNameArg(vm, args[3]),
				})
			},
			define: func(cls *RClass) {
				cls.define("priority", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
					return object.IntValue(int64(recordOf(self).(*resolv.SRV).Priority))
				})
				cls.define("weight", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
					return object.IntValue(int64(recordOf(self).(*resolv.SRV).Weight))
				})
				cls.define("port", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
					return object.IntValue(int64(recordOf(self).(*resolv.SRV).Port))
				})
				cls.define("target", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
					return newResolvName(vm.dnsNameClass(), recordOf(self).(*resolv.SRV).Target)
				})
			}},
		{name: "HINFO", typ: resolv.TypeHINFO,
			construct: func(_ *VM, cls *RClass, args []object.Value) object.Value {
				return newRecord(cls, &resolv.HINFO{CPU: strArg(args[0]), OS: strArg(args[1])})
			},
			define: func(cls *RClass) {
				cls.define("cpu", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
					return object.NewString(recordOf(self).(*resolv.HINFO).CPU)
				})
				cls.define("os", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
					return object.NewString(recordOf(self).(*resolv.HINFO).OS)
				})
			}},
	}
}

// domainNameCtor builds the constructor for a single-Name record (CNAME/NS/PTR)
// from the library's New<Record>(Name) factory.
func domainNameCtor[T resolv.Resource](mk func(resolv.Name) T) func(vm *VM, cls *RClass, args []object.Value) object.Value {
	return func(vm *VM, cls *RClass, args []object.Value) object.Value {
		return newRecord(cls, mk(resolvNameArg(vm, args[0])))
	}
}

// domainNameAccessor defines the #name accessor for a single-Name record.
func domainNameAccessor(get func(resolv.Resource) resolv.Name) func(cls *RClass) {
	return func(cls *RClass) {
		cls.define("name", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
			return newResolvName(vm.dnsNameClass(), get(recordOf(self)))
		})
	}
}

// registerResolvMessage wires Resolv::DNS::Message: new(id), the header flag
// accessors, add_question/add_answer, encode (an ASCII-8BIT wire string) and the
// Message.decode class method, plus question/answer enumeration.
func registerResolvMessage(vm *VM, dns *RClass, byType map[uint16]*RClass) {
	msg := newClass("Resolv::DNS::Message", vm.cObject)
	dns.consts["Message"] = msg
	msg.smethods["new"] = &Method{name: "new", owner: msg,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			id := uint16(0)
			if len(args) > 0 {
				id = uint16(intArg(args[0]))
			}
			o := &RObject{class: msg, ivars: map[string]object.Value{}}
			o.ivars["@msg"] = &resolvBox{msg: resolv.NewMessage(id)}
			return o
		}}
	msg.smethods["decode"] = &Method{name: "decode", owner: msg,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			m, err := resolv.Decode([]byte(strArg(args[0])))
			if err != nil {
				return raise("Resolv::DNS::DecodeError", "%v", err)
			}
			o := &RObject{class: msg, ivars: map[string]object.Value{}}
			o.ivars["@msg"] = &resolvBox{msg: m}
			return o
		}}
	dns.consts["DecodeError"] = newClass("Resolv::DNS::DecodeError", vm.consts["StandardError"].(*RClass))

	mget := func(self object.Value) *resolv.Message {
		if b, ok := getIvar(self, "@msg").(*resolvBox); ok && b.msg != nil {
			return b.msg
		}
		return resolv.NewMessage(0)
	}
	// Header accessors (id and the flag bits MRI exposes).
	msg.define("id", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(mget(self).ID))
	})
	for _, fl := range []struct {
		name string
		get  func(*resolv.Message) uint16
		set  func(*resolv.Message, uint16)
	}{
		{"qr", func(m *resolv.Message) uint16 { return m.QR }, func(m *resolv.Message, v uint16) { m.QR = v }},
		{"opcode", func(m *resolv.Message) uint16 { return m.Opcode }, func(m *resolv.Message, v uint16) { m.Opcode = v }},
		{"aa", func(m *resolv.Message) uint16 { return m.AA }, func(m *resolv.Message, v uint16) { m.AA = v }},
		{"tc", func(m *resolv.Message) uint16 { return m.TC }, func(m *resolv.Message, v uint16) { m.TC = v }},
		{"rd", func(m *resolv.Message) uint16 { return m.RD }, func(m *resolv.Message, v uint16) { m.RD = v }},
		{"ra", func(m *resolv.Message) uint16 { return m.RA }, func(m *resolv.Message, v uint16) { m.RA = v }},
		{"rcode", func(m *resolv.Message) uint16 { return m.RCode }, func(m *resolv.Message, v uint16) { m.RCode = v }},
	} {
		get, set := fl.get, fl.set
		msg.define(fl.name, func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
			return object.IntValue(int64(get(mget(self))))
		})
		msg.define(fl.name+"=", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
			set(mget(self), uint16(intArg(args[0])))
			return args[0]
		})
	}
	msg.define("add_question", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		typ := uint16(resolv.TypeA)
		if len(args) > 1 {
			typ = resolvTypeArg(vm, args[1])
		}
		mget(self).AddQuestion(resolvNameArg(vm, args[0]), typ, resolv.ClassIN)
		return object.NilV
	})
	msg.define("add_answer", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		mget(self).AddAnswer(resolvNameArg(vm, args[0]), uint32(intArg(args[1])), recordOf(args[2]))
		return object.NilV
	})
	msg.define("add_authority", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		mget(self).AddAuthority(resolvNameArg(vm, args[0]), uint32(intArg(args[1])), recordOf(args[2]))
		return object.NilV
	})
	msg.define("add_additional", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		mget(self).AddAdditional(resolvNameArg(vm, args[0]), uint32(intArg(args[1])), recordOf(args[2]))
		return object.NilV
	})
	msg.define("encode", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		b := mget(self).Encode()
		// MRI's Message#encode reports the wire string's encoding as the default
		// (UTF-8) while every byte is ASCII, and as ASCII-8BIT once a high byte
		// (e.g. an address octet >= 0x80) is present; mirror that.
		enc := ""
		for _, c := range b {
			if c >= 0x80 {
				enc = "ASCII-8BIT"
				break
			}
		}
		return object.NewStringBytesEnc(b, enc)
	})
	msg.define("question", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		out := []object.Value{}
		for _, q := range mget(self).Question {
			out = append(out, object.NewArray(newResolvName(vm.dnsNameClass(), q.Name), resolvTypeClass(vm, byType, q.Type)))
		}
		return object.NewArrayFromSlice(out)
	})
	msg.define("answer", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return resolvSectionArray(vm, byType, mget(self).Answer)
	})
	msg.define("authority", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return resolvSectionArray(vm, byType, mget(self).Authority)
	})
	msg.define("additional", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return resolvSectionArray(vm, byType, mget(self).Additional)
	})
	// each_answer / each_question yield the [name, ttl, data] / [name, typeclass]
	// tuples, mirroring MRI's enumerators.
	msg.define("each_answer", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		for _, rr := range mget(self).Answer {
			vm.callBlock(blk, []object.Value{
				newResolvName(vm.dnsNameClass(), rr.Name),
				object.IntValue(int64(rr.TTL)),
				newRecord(vm.recordClass(byType, rr.Data.TypeValue()), rr.Data),
			})
		}
		return self
	})
	msg.define("each_question", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		for _, q := range mget(self).Question {
			vm.callBlock(blk, []object.Value{
				newResolvName(vm.dnsNameClass(), q.Name),
				resolvTypeClass(vm, byType, q.Type),
			})
		}
		return self
	})
}

// --- helpers ----------------------------------------------------------------

// resolvBox stashes a library Go value (a *Message or *Hosts) inside a Ruby
// object's ivar, so a constructed Resolv object retains its native state across
// method calls. It is never user-visible (no Ruby method returns it).
type resolvBox struct {
	msg    *resolv.Message
	hosts  *resolv.Hosts
	name   resolv.Name
	record resolv.Resource
}

func (b *resolvBox) ToS() string     { return "#<Resolv native>" }
func (b *resolvBox) Inspect() string { return "#<Resolv native>" }
func (b *resolvBox) Truthy() bool    { return true }

// newResolvAddr builds a Resolv::IPv4/IPv6 RObject holding the canonical address.
func newResolvAddr(cls *RClass, addr string) object.Value {
	return &RObject{class: cls, ivars: map[string]object.Value{"@addr": object.NewString(addr)}}
}

// newResolvName wraps a resolv.Name in a Resolv::DNS::Name RObject.
func newResolvName(cls *RClass, n resolv.Name) object.Value {
	return &RObject{class: cls, ivars: map[string]object.Value{"@name": &resolvBox{name: n}}}
}

// resolvNameOf returns the resolv.Name backing a Resolv::DNS::Name object.
func resolvNameOf(self object.Value) resolv.Name {
	if b, ok := getIvar(self, "@name").(*resolvBox); ok {
		return b.name
	}
	return resolv.Name{}
}

// newRecord wraps a library Resource in its Resolv::DNS::Resource::IN::* RObject.
func newRecord(cls *RClass, r resolv.Resource) object.Value {
	return &RObject{class: cls, ivars: map[string]object.Value{"@rec": &resolvBox{record: r}}}
}

// recordOf returns the library Resource backing a record object.
func recordOf(self object.Value) resolv.Resource {
	if b, ok := getIvar(self, "@rec").(*resolvBox); ok {
		return b.record
	}
	return nil
}

// dnsNameClass returns the Resolv::DNS::Name class.
func (vm *VM) dnsNameClass() *RClass {
	return vm.consts["Resolv"].(*RClass).consts["DNS"].(*RClass).consts["Name"].(*RClass)
}

// recordClass returns the Resolv::DNS::Resource::IN::* class for a TYPE value,
// falling back to the A class for an unmodelled type (Generic decode).
func (vm *VM) recordClass(byType map[uint16]*RClass, typ uint16) *RClass {
	if cls, ok := byType[typ]; ok {
		return cls
	}
	return byType[resolv.TypeA]
}

// resolvNameArg coerces a Ruby argument (a Resolv::DNS::Name or a String) into a
// resolv.Name.
func resolvNameArg(vm *VM, v object.Value) resolv.Name {
	if o, ok := v.(*RObject); ok && o.class == vm.dnsNameClass() {
		return resolvNameOf(o)
	}
	return resolv.NewName(strArg(v))
}

// resolvIPv4Of coerces a Ruby argument (a Resolv::IPv4 or a dotted String) into a
// resolv.IPv4, raising ArgumentError on a malformed address.
func resolvIPv4Of(vm *VM, v object.Value) resolv.IPv4 {
	s := strArg(resolvAddrString(vm, v, "IPv4"))
	ip, err := resolv.CreateIPv4(s)
	if err != nil {
		raise("ArgumentError", "cannot interpret as IPv4 address: %s", s)
	}
	return ip
}

// resolvIPv6Of coerces a Ruby argument (a Resolv::IPv6 or a String) into a
// resolv.IPv6, raising ArgumentError on a malformed address.
func resolvIPv6Of(vm *VM, v object.Value) resolv.IPv6 {
	s := strArg(resolvAddrString(vm, v, "IPv6"))
	base, _ := splitZone(s)
	ip, err := resolv.CreateIPv6(base)
	if err != nil {
		raise("ArgumentError", "cannot interpret as IPv6 address: %s", s)
	}
	return ip
}

// resolvAddrString returns the @addr string of a Resolv::IPv4/IPv6 object, or the
// argument coerced to a String.
func resolvAddrString(vm *VM, v object.Value, kind string) object.Value {
	if o, ok := v.(*RObject); ok {
		if cls, found := vm.consts["Resolv"].(*RClass).consts[kind].(*RClass); found && o.class == cls {
			return getIvar(o, "@addr")
		}
	}
	return object.NewString(strArg(v))
}

// resolvTypeArg maps a Resolv::DNS::Resource::IN::* class argument to its library
// TYPE value (defaulting to A).
func resolvTypeArg(vm *VM, v object.Value) uint16 {
	cls, ok := v.(*RClass)
	if !ok {
		return resolv.TypeA
	}
	for typ, c := range vm.recordClassesByType() {
		if c == cls {
			return typ
		}
	}
	return resolv.TypeA
}

// recordClassesByType rebuilds the TYPE->class map from the IN constant tree (the
// map is not stored on the VM, so it is reconstructed when a type lookup needs
// it; the tree is tiny).
func (vm *VM) recordClassesByType() map[uint16]*RClass {
	in := vm.consts["Resolv"].(*RClass).consts["DNS"].(*RClass).
		consts["Resource"].(*RClass).consts["IN"].(*RClass)
	out := map[uint16]*RClass{}
	for _, spec := range resolvRecordSpecs() {
		if c, ok := in.consts[spec.name].(*RClass); ok {
			out[spec.typ] = c
		}
	}
	return out
}

// resolvTypeClass returns the Resolv::DNS::Resource::IN::* class for a TYPE value.
func resolvTypeClass(vm *VM, byType map[uint16]*RClass, typ uint16) object.Value {
	return vm.recordClass(byType, typ)
}

// resolvSectionArray renders an RR section as MRI's [[name, ttl, data], ...].
func resolvSectionArray(vm *VM, byType map[uint16]*RClass, rrs []resolv.RR) object.Value {
	out := []object.Value{}
	for _, rr := range rrs {
		out = append(out, object.NewArray(newResolvName(vm.dnsNameClass(), rr.Name), object.IntValue(int64(rr.TTL)), newRecord(vm.recordClass(byType, rr.Data.TypeValue()), rr.Data)))
	}
	return object.NewArrayFromSlice(out)
}

// strSliceToArray builds a Ruby Array of Strings from a Go slice.
func strSliceToArray(ss []string) object.Value {
	elems := make([]object.Value, len(ss))
	for i, s := range ss {
		elems[i] = object.NewString(s)
	}
	return object.NewArrayFromSlice(elems)
}

// splitZone splits an IPv6 textual address into its base and an optional %zone
// suffix (kept so MRI's link-local %eth0 round-trips through create).
func splitZone(s string) (base, zone string) {
	for i := 0; i < len(s); i++ {
		if s[i] == '%' {
			return s[:i], s[i:]
		}
	}
	return s, ""
}

// isLiteralIP reports whether name is already a numeric IPv4 or IPv6 address, in
// which case Resolv.getaddress can answer without any DNS query.
func isLiteralIP(name string) bool {
	if _, err := resolv.CreateIPv4(name); err == nil {
		return true
	}
	base, _ := splitZone(name)
	_, err := resolv.CreateIPv6(base)
	return err == nil
}
