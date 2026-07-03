// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"github.com/go-embedded-ruby/ruby/internal/object"
	liburi "github.com/go-ruby-uri/uri"
)

// This file is the thin binding between rbgo's Ruby object graph (object.Value)
// and github.com/go-ruby-uri/uri — an MRI-4.0.5-byte-exact reimplementation of
// Ruby's "uri" standard library (require "uri"), a sibling of go-ruby-strscan /
// go-ruby-date / go-ruby-json. The whole URI model lives in that library: the
// RFC 3986 grammar, the 9-element URI.split tuple, opaque-vs-hierarchical paths,
// the scheme registry with its default ports, reference resolution (merge / + /
// route_to), normalization and the percent-encoding / www-form codecs. rbgo only
// wraps a *liburi.URI in its Ruby URI::Generic object (carrying the scheme
// subclass for `class` / `is_a?`) and re-raises the library's typed errors as the
// matching URI:: exceptions.
//
// This replaces rbgo's former pure-Ruby prelude URI module: the Ruby-facing API
// (URI.parse / URI() / URI.join / the component accessors, merge / + / to_s,
// URI::Generic.build, URI::DEFAULT_PARSER) is preserved byte-for-byte where the
// prelude and the tests pinned it, and extended with the rest of MRI's surface
// the library makes available (URI.split / encode_www_form / decode_www_form /
// escape / unescape, route_to / normalize / hostname / absolute? / relative?,
// the InvalidComponentError / BadURIError taxonomy and validated setters).

// URI is the Ruby wrapper around a *liburi.URI. The library value carries the
// parsed components; cls is the Ruby class the wrapper reports for `class` /
// `is_a?` — URI::Generic for an unknown (or absent) scheme, or a scheme subclass
// such as URI::HTTP, so the value inherits every instance method defined on
// URI::Generic yet keeps its own identity.
type URI struct {
	u   *liburi.URI
	cls *RClass
}

func (u *URI) ToS() string     { return u.u.String() }
func (u *URI) Inspect() string { return "#<" + u.cls.name + " " + u.u.String() + ">" }
func (u *URI) Truthy() bool    { return true }

// uriOf returns the receiver's wrapped library URI.
func uriOf(v object.Value) *liburi.URI { return v.(*URI).u }

// uriEqual implements URI::Generic#== (and the == operator fast path): two URIs
// are equal when the other is also a URI and their normalised string forms match
// — MRI compares component-wise, which for our value model is the rendered form.
func uriEqual(a *URI, other object.Value) bool {
	b, ok := other.(*URI)
	return ok && a.u.String() == b.u.String()
}

// classForScheme returns the Ruby class a parsed URI with the given scheme should
// report: the registered scheme subclass (URI::HTTP, URI::HTTPS, ...) when the
// scheme is known, else URI::Generic. The scheme is matched case-insensitively
// through the URI module's scheme_list (mirroring MRI's URI.scheme_list lookup).
func (vm *VM) classForScheme(scheme string) *RClass {
	if scheme != "" {
		if c, ok := vm.cURI.consts[uriUpcase(scheme)].(*RClass); ok {
			return c
		}
	}
	return vm.cURIGeneric
}

// uriUpcase upper-cases an ASCII scheme name for the scheme_list lookup (a URI
// scheme is ASCII by grammar, so a byte-wise fold is exact and avoids a Unicode
// dependency).
func uriUpcase(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] = c - 32
		}
	}
	return string(b)
}

// wrapURI wraps a parsed library URI in the Ruby URI object, choosing the scheme
// subclass for its `class`.
func (vm *VM) wrapURI(u *liburi.URI) *URI {
	return &URI{u: u, cls: vm.classForScheme(u.Scheme)}
}

// raiseURIErr maps a library error to the matching Ruby URI exception: an
// InvalidURIError / InvalidComponentError / BadURIError each become their
// namesake URI:: class; anything else becomes URI::Error. It never returns when
// err is non-nil. The library's Error message is reproduced verbatim (it already
// matches MRI byte-for-byte).
func raiseURIErr(err error) {
	if err == nil {
		return
	}
	switch err.(type) {
	case *liburi.InvalidURIError:
		raise("URI::InvalidURIError", "%s", err.Error())
	case *liburi.InvalidComponentError:
		raise("URI::InvalidComponentError", "%s", err.Error())
	case *liburi.BadURIError:
		raise("URI::BadURIError", "%s", err.Error())
	default:
		raise("URI::Error", "%s", err.Error())
	}
}

// uriParse parses s through the library, raising the mapped URI error on failure.
func (vm *VM) uriParse(s string) *URI {
	u, err := liburi.Parse(s)
	raiseURIErr(err)
	return vm.wrapURI(u)
}

// uriStrOf coerces a URI argument to its string form: a URI passes through its
// rendered form, a String is taken as-is, and anything else is sent #to_s — the
// coercion MRI's URI.parse / merge accept.
func (vm *VM) uriStrOf(v object.Value) string {
	switch x := v.(type) {
	case *URI:
		return x.u.String()
	case *object.String:
		return x.Str()
	default:
		return strArg(vm.send(v, "to_s", nil, nil))
	}
}

// uriMerge resolves rel against u (URI::Generic#merge / #+), returning a freshly
// wrapped URI. rel may be a URI, a String or any #to_s-able value.
func (vm *VM) uriMerge(u *URI, rel object.Value) object.Value {
	r, err := u.u.Merge(vm.uriStrOf(rel))
	raiseURIErr(err)
	return vm.wrapURI(r)
}

// nilOrStr renders an optional URI component: the empty/absent form is Ruby nil,
// a present value is a String. has distinguishes an absent component (nil) from a
// present-but-empty one, matching MRI where the query/fragment can be "" yet not
// nil ("a?" vs "a").
func nilOrStr(s string, has bool) object.Value {
	if !has {
		return object.NilV
	}
	return object.NewString(s)
}

// registerURI installs the URI module (require "uri") backed by the go-ruby-uri
// library: the URI module function family (URI() / parse / join / split /
// encode_www_form / decode_www_form / escape / unescape), the URI::Generic class
// and its scheme subclasses with their instance methods, the DEFAULT_PARSER /
// RFC3986_PARSER parser objects and the InvalidURIError / InvalidComponentError /
// BadURIError taxonomy. It runs after the exception hierarchy is in place
// (URI::Error < StandardError) and after Regexp exists (Parser#make_regexp).
func (vm *VM) registerURI() {
	mod := newClass("URI", nil)
	mod.isModule = true
	vm.cURI = mod
	vm.consts["URI"] = mod

	vm.registerURIErrors(mod)
	vm.registerURIClasses(mod)
	vm.registerURIModuleFns(mod)
	vm.registerURIParser(mod)
	vm.registerURIKernel()
}

// registerURIErrors installs the URI exception taxonomy mirroring MRI: URI::Error
// is the module/mixin MRI exposes (here a class < StandardError so it can be both
// rescued and raised); InvalidURIError / InvalidComponentError / BadURIError are
// its subclasses. Each is registered both as a nested constant of URI (so Ruby
// `URI::InvalidURIError` resolves it) and under its qualified top-level name (so a
// re-raised library error's exceptionObject lookup finds the very same class),
// exactly as the JSON:: / Date:: error classes are.
func (vm *VM) registerURIErrors(mod *RClass) {
	std := vm.consts["StandardError"].(*RClass)
	reg := func(simple, qualified string, super *RClass) *RClass {
		c := newClass(qualified, super)
		mod.consts[simple] = c
		vm.consts[qualified] = c
		return c
	}
	uriErr := reg("Error", "URI::Error", std)
	reg("InvalidURIError", "URI::InvalidURIError", uriErr)
	reg("InvalidComponentError", "URI::InvalidComponentError", uriErr)
	reg("BadURIError", "URI::BadURIError", uriErr)
}

// registerURIClasses installs URI::Generic (including the URI module so a URI
// value is `is_a?(URI)`, as in MRI) and the scheme subclasses with their default
// ports, then defines every instance method on Generic so the subclasses inherit.
// DEFAULT_PORTS / scheme_list expose the registry the library and Puppet's Pcore
// (URI.scheme_list[scheme.upcase]) read.
func (vm *VM) registerURIClasses(mod *RClass) {
	generic := newClass("URI::Generic", vm.cObject)
	generic.includes = append(generic.includes, mod) // URI::Generic includes URI -> is_a?(URI)
	mod.consts["Generic"] = generic
	vm.consts["URI::Generic"] = generic
	vm.cURIGeneric = generic

	// The scheme subclasses MRI registers, each a URI::Generic with a default
	// port. They are stored under their upper-cased scheme name so scheme_list /
	// classForScheme can resolve "HTTP" -> URI::HTTP from a parsed scheme.
	for _, scheme := range []string{"http", "https", "ftp", "ldap", "ldaps", "ws", "wss"} {
		name := uriUpcase(scheme)
		sub := newClass("URI::"+name, generic)
		mod.consts[name] = sub
		vm.consts["URI::"+name] = sub
	}
	// URI::File (file:// URIs) — registered like the port-carrying subclasses but
	// keyed FILE; the constant name keeps MRI's capitalisation.
	fileCls := newClass("URI::File", generic)
	mod.consts["File"] = fileCls
	vm.consts["URI::File"] = fileCls

	// DEFAULT_PORTS exposes the library's scheme->port table as a Ruby Hash; both
	// URI::DEFAULT_PORTS and URI::Generic::DEFAULT_PORTS resolve it (MRI defines it
	// on Generic; the prelude exposed it on URI).
	ports := object.NewHash()
	for scheme, port := range liburi.DefaultPorts {
		ports.Set(object.NewString(scheme), object.IntValue(int64(port)))
	}
	mod.consts["DEFAULT_PORTS"] = ports
	generic.consts["DEFAULT_PORTS"] = ports

	vm.registerURIInstanceMethods(generic)
	vm.registerURIBuild(generic)
	vm.registerURISchemeList(mod)
}

// registerURISchemeList installs URI.scheme_list — the Hash from an upper-cased
// scheme name to its registered subclass, the form Puppet's Pcore reads
// (URI.scheme_list[scheme.upcase]). It is rebuilt from the live constants so it
// stays in step with the registered subclasses.
func (vm *VM) registerURISchemeList(mod *RClass) {
	mod.smethods["scheme_list"] = &Method{name: "scheme_list", owner: mod,
		native: func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
			h := object.NewHash()
			for _, name := range []string{"HTTP", "HTTPS", "FTP", "LDAP", "LDAPS", "WS", "WSS", "FILE"} {
				if c, ok := mod.consts[name].(*RClass); ok {
					h.Set(object.NewString(name), c)
				}
			}
			return h
		}}
}

// registerURIInstanceMethods defines the URI::Generic instance methods: the
// component getters (effective port included, matching MRI), the validated
// setters, to_s / to_str / inspect / ==, the reference-resolution family
// (merge / + / route_to / normalize) and the absolute? / relative? / hostname /
// default_port predicates. Every method delegates to the library.
func (vm *VM) registerURIInstanceMethods(generic *RClass) {
	d := func(name string, fn NativeFn) { generic.define(name, fn) }

	// Component getters. scheme / host / path are plain strings; userinfo / query
	// / fragment / opaque are nil when absent (MRI's nilable components). port is
	// the effective port — the explicit port, or the scheme default when omitted,
	// or nil for a scheme with no default — exactly as MRI's URI#port reports it.
	d("scheme", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return nilOrStr(uriOf(v).Scheme, uriOf(v).Scheme != "")
	})
	d("host", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return nilOrStr(uriOf(v).Host, uriOf(v).Host != "")
	})
	d("path", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(uriOf(v).Path)
	})
	d("userinfo", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return nilOrStr(uriOf(v).Userinfo, uriOf(v).Userinfo != "")
	})
	d("query", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return nilOrStr(uriOf(v).Query, uriOf(v).HasQuery)
	})
	d("fragment", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return nilOrStr(uriOf(v).Fragment, uriOf(v).HasFrag)
	})
	d("opaque", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return nilOrStr(uriOf(v).Opaque, uriOf(v).Opaque != "")
	})
	d("port", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		if p, ok := uriOf(v).EffectivePort(); ok {
			return object.IntValue(int64(p))
		}
		return object.NilV
	})
	d("hostname", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		h := uriOf(v).Hostname()
		return nilOrStr(h, h != "")
	})
	d("default_port", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		if p, ok := uriOf(v).DefaultPort(); ok {
			return object.IntValue(int64(p))
		}
		return object.NilV
	})

	// Validated setters (URI#scheme= / host= / port= / userinfo=): each delegates
	// to the library, which validates the component against its grammar and raises
	// an InvalidComponentError on a malformed value. The assigned value is returned
	// (a Ruby setter yields its RHS).
	set := func(name string, fn func(*liburi.URI, string) error) {
		d(name, func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
			raiseURIErr(fn(uriOf(v), uriSetArg(args[0])))
			return args[0]
		})
	}
	set("scheme=", (*liburi.URI).SetScheme)
	set("host=", (*liburi.URI).SetHost)
	set("port=", (*liburi.URI).SetPort)
	set("userinfo=", (*liburi.URI).SetUserinfo)

	// String forms: to_s / to_str render the URI; inspect is "#<URI::HTTP url>".
	toS := func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(uriOf(v).String())
	}
	d("to_s", toS)
	d("to_str", toS)
	d("inspect", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(v.(*URI).Inspect())
	})
	d("==", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(uriEqual(v.(*URI), args[0]))
	})

	// Reference resolution: merge / + resolve a relative reference; route_to
	// computes the reference from the receiver to its argument; normalize collapses
	// the path and lower-cases the scheme/host.
	mergeFn := func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.uriMerge(v.(*URI), args[0])
	}
	d("merge", mergeFn)
	d("+", mergeFn)
	d("route_to", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		r, err := uriOf(v).RouteTo(vm.uriStrOf(args[0]))
		raiseURIErr(err)
		return vm.wrapURI(r)
	})
	d("normalize", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.wrapURI(uriOf(v).Normalize())
	})

	// Predicates: absolute? (has a scheme) / relative? (its negation).
	d("absolute?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(uriOf(v).Absolute())
	})
	d("relative?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(uriOf(v).Relative())
	})
}

// uriSetArg coerces a setter argument to a string, treating Ruby nil as the empty
// (cleared) component — MRI's URI setters accept nil to clear userinfo/port/etc.
func uriSetArg(v object.Value) string {
	if _, ok := v.(object.Nil); ok {
		return ""
	}
	return strArg(v)
}

// registerURIBuild installs URI::Generic.build (and, by inheritance through the
// shared smethods lookup, the scheme subclasses' build). It accepts a component
// Hash (keyed by symbol or string) or a 7-element Array in
// [scheme, userinfo, host, port, path, query, fragment] order — the form the
// prelude defined and rbgo's tests pin — and returns an instance of the receiver
// class. A wrong-length Array or a non-Hash/Array argument raises ArgumentError,
// as MRI's build does.
func (vm *VM) registerURIBuild(generic *RClass) {
	build := func(_ *VM, recv object.Value, args []object.Value, _ *Proc) object.Value {
		cls, _ := recv.(*RClass)
		var scheme, userinfo, host, path, query, fragment string
		var port int
		var hasPort, hasQuery, hasFrag bool
		switch a := args[0].(type) {
		case *object.Hash:
			get := func(key string) (string, bool) {
				if v, ok := a.Get(object.Symbol(key)); ok {
					return uriBuildField(v)
				}
				if v, ok := a.Get(object.NewString(key)); ok {
					return uriBuildField(v)
				}
				return "", false
			}
			scheme, _ = get("scheme")
			userinfo, _ = get("userinfo")
			host, _ = get("host")
			path, _ = get("path")
			query, hasQuery = get("query")
			fragment, hasFrag = get("fragment")
			if v, ok := a.Get(object.Symbol("port")); ok {
				port, hasPort = uriBuildPort(v)
			} else if v, ok := a.Get(object.NewString("port")); ok {
				port, hasPort = uriBuildPort(v)
			}
		case *object.Array:
			if len(a.Elems) != 7 {
				raise("ArgumentError",
					"expected Array of or Hash of components of %s (scheme, userinfo, host, port, path, query, fragment)",
					cls.name)
			}
			scheme, _ = uriBuildField(a.Elems[0])
			userinfo, _ = uriBuildField(a.Elems[1])
			host, _ = uriBuildField(a.Elems[2])
			port, hasPort = uriBuildPort(a.Elems[3])
			path, _ = uriBuildField(a.Elems[4])
			query, hasQuery = uriBuildField(a.Elems[5])
			fragment, hasFrag = uriBuildField(a.Elems[6])
		default:
			raise("ArgumentError", "expected Array of or Hash of components of %s", cls.name)
		}
		u := liburi.Build(scheme, userinfo, host, port, hasPort, path, query, hasQuery, fragment, hasFrag)
		return &URI{u: u, cls: cls}
	}
	generic.smethods["build"] = &Method{name: "build", owner: generic, native: build}
}

// uriBuildField coerces a build component value to a string, treating nil as an
// absent component. ok is false for nil so build can tell an explicit "" from an
// omitted field (the query / fragment present-but-empty distinction).
func uriBuildField(v object.Value) (string, bool) {
	if _, ok := v.(object.Nil); ok {
		return "", false
	}
	return strArg(v), true
}

// uriBuildPort coerces a build port value: an Integer is the port and hasPort is
// true; a String of digits is parsed; nil (or anything else) is an absent port.
func uriBuildPort(v object.Value) (int, bool) {
	switch p := v.(type) {
	case object.Integer:
		return int(p), true
	case *object.String:
		n := 0
		for _, c := range p.Str() {
			if c < '0' || c > '9' {
				return 0, false
			}
			n = n*10 + int(c-'0')
		}
		if p.Str() == "" {
			return 0, false
		}
		return n, true
	default:
		return 0, false
	}
}

// registerURIModuleFns installs the URI module functions: URI.parse / join /
// split / encode_www_form / decode_www_form / escape / unescape. Each is a
// singleton method on the URI module, delegating to the library and re-raising a
// library error as the matching URI:: exception.
func (vm *VM) registerURIModuleFns(mod *RClass) {
	sm := func(name string, fn NativeFn) { mod.smethods[name] = &Method{name: name, owner: mod, native: fn} }

	// URI.parse(str) -> a URI::Generic (or scheme subclass).
	sm("parse", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.uriParse(vm.uriStrOf(args[0]))
	})

	// URI.join(base, *rels) resolves each reference against the running base.
	sm("join", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1+)")
		}
		rels := make([]string, len(args)-1)
		for i, a := range args[1:] {
			rels[i] = vm.uriStrOf(a)
		}
		u, err := liburi.Join(vm.uriStrOf(args[0]), rels...)
		raiseURIErr(err)
		return vm.wrapURI(u)
	})

	// URI.split(str) -> the 9-element [scheme, userinfo, host, port, registry,
	// path, opaque, query, fragment] tuple. The library returns "" for absent
	// components; MRI uses nil for every component except the path, so the tuple is
	// rebuilt from a Parse to recover the absent/empty distinction MRI exposes.
	sm("split", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.uriSplit(vm.uriStrOf(args[0]))
	})

	// URI.encode_www_form(enum) renders an application/x-www-form-urlencoded body
	// from an Array of [key, value] pairs.
	sm("encode_www_form", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(liburi.EncodeWWWForm(vm.uriFormPairs(args[0])))
	})

	// URI.decode_www_form(str) parses a form body into an Array of [key, value]
	// String pairs.
	sm("decode_www_form", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		pairs, err := liburi.DecodeWWWForm(strArg(args[0]))
		raiseURIErr(err)
		out := make([]object.Value, len(pairs))
		for i, p := range pairs {
			out[i] = object.NewArray(object.NewString(p[0]), object.NewString(p[1]))
		}
		return object.NewArrayFromSlice(out)
	})

	// URI.escape(str[, unsafe]) / URI.unescape(str) — the legacy percent-codec.
	sm("escape", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) > 1 {
			return object.NewString(liburi.Escape(strArg(args[0]), uriUnsafe(args[1])))
		}
		return object.NewString(liburi.EscapeDefault(strArg(args[0])))
	})
	sm("unescape", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(liburi.Unescape(strArg(args[0])))
	})
}

// uriSplit builds MRI's 9-element URI.split tuple from a parse, mapping each
// absent component to nil (the path is the empty string, never nil; the others
// are nil when absent). A parse failure raises URI::InvalidURIError.
func (vm *VM) uriSplit(s string) object.Value {
	u, err := liburi.Parse(s)
	raiseURIErr(err)
	// registry has no first-class field in the value model (MRI deprecated it); it
	// is always nil, matching MRI for the URIs the grammar produces.
	elems := []object.Value{
		nilOrStr(u.Scheme, u.Scheme != ""),
		nilOrStr(u.Userinfo, u.Userinfo != ""),
		nilOrStr(u.Host, u.Host != ""),
		uriSplitPort(u),
		object.NilV, // registry
		object.NewString(u.Path),
		nilOrStr(u.Opaque, u.Opaque != ""),
		nilOrStr(u.Query, u.HasQuery),
		nilOrStr(u.Fragment, u.HasFrag),
	}
	return object.NewArrayFromSlice(elems)
}

// uriSplitPort renders the port element of URI.split: the explicit port as a
// String (MRI's split returns the raw port substring), or nil when absent. Unlike
// URI#port, split does not substitute the scheme default.
func uriSplitPort(u *liburi.URI) object.Value {
	if !u.HasPort {
		return object.NilV
	}
	return object.NewString(object.IntValue(int64(u.Port)).ToS())
}

// uriFormPairs coerces an Array of [key, value] pairs to the library's pair form,
// stringifying each side. A non-Array (or a non-pair element) raises TypeError,
// matching MRI's encode_www_form which iterates pairs.
func (vm *VM) uriFormPairs(v object.Value) [][2]string {
	arr, ok := v.(*object.Array)
	if !ok {
		raise("TypeError", "no implicit conversion of %s into Array", classNameOf(v))
	}
	pairs := make([][2]string, len(arr.Elems))
	for i, e := range arr.Elems {
		pair, ok := e.(*object.Array)
		if !ok || len(pair.Elems) != 2 {
			raise("TypeError", "wrong element type %s (expected array)", classNameOf(e))
		}
		pairs[i] = [2]string{vm.uriStrOf(pair.Elems[0]), vm.uriStrOf(pair.Elems[1])}
	}
	return pairs
}

// uriUnsafe coerces the URI.escape unsafe argument to its source string: a Regexp
// contributes its source, a String its literal text. Anything else raises
// TypeError, as MRI's escape expects a Regexp or String unsafe pattern.
func uriUnsafe(v object.Value) string {
	switch x := v.(type) {
	case *Regexp:
		return x.source
	case *object.String:
		return x.Str()
	default:
		raise("TypeError", "no implicit conversion of %s into String", classNameOf(v))
		return ""
	}
}

// registerURIParser installs the URI::Parser class and the DEFAULT_PARSER /
// RFC3986_PARSER instances MRI exposes. The parser is a thin facade over the
// library: #parse forwards to URI.parse, #split to URI.split, #escape / #unescape
// to the codecs, and #make_regexp builds a Regexp matching a whole absolute URI
// (optionally restricted to a scheme set) — the form Puppet's Pcore derives its
// URI type pattern from (URI::DEFAULT_PARSER.make_regexp).
func (vm *VM) registerURIParser(mod *RClass) {
	parser := newClass("URI::RFC3986_Parser", vm.cObject)
	mod.consts["RFC3986_Parser"] = parser
	vm.consts["URI::RFC3986_Parser"] = parser
	// URI::Parser is MRI's generic-parser alias; here it names the same class so
	// `URI::Parser.new` and the DEFAULT_PARSER share one implementation.
	mod.consts["Parser"] = parser
	vm.consts["URI::Parser"] = parser

	d := func(name string, fn NativeFn) { parser.define(name, fn) }
	d("parse", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.uriParse(vm.uriStrOf(args[0]))
	})
	d("split", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.uriSplit(vm.uriStrOf(args[0]))
	})
	d("escape", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) > 1 {
			return object.NewString(liburi.Escape(strArg(args[0]), uriUnsafe(args[1])))
		}
		return object.NewString(liburi.EscapeDefault(strArg(args[0])))
	})
	d("unescape", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(liburi.Unescape(strArg(args[0])))
	})
	d("make_regexp", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.compileRegexp(uriMakeRegexp(args), "")
	})

	inst := &RObject{class: parser, ivars: map[string]object.Value{}}
	mod.consts["DEFAULT_PARSER"] = inst
	vm.consts["URI::DEFAULT_PARSER"] = inst
	mod.consts["RFC3986_PARSER"] = inst
	vm.consts["URI::RFC3986_PARSER"] = inst
}

// uriMakeRegexp builds the source of the Regexp DEFAULT_PARSER.make_regexp
// returns: a pattern matching an absolute URI anywhere in a string (unanchored,
// like MRI's RFC3986 parser, which Puppet's Pcore embeds as its URI type regexp).
// With a non-empty scheme list only those schemes match; without one any
// grammar-valid scheme is allowed. The body is a compact form of MRI's
// absolute-URI grammar — scheme, authority (userinfo@host:port), path, query and
// fragment — sufficient to recognise a URI within surrounding text.
func uriMakeRegexp(args []object.Value) string {
	scheme := `[a-zA-Z][a-zA-Z\d+\-.]*`
	if len(args) > 0 {
		if arr, ok := args[0].(*object.Array); ok && len(arr.Elems) > 0 {
			alts := make([]string, len(arr.Elems))
			for i, e := range arr.Elems {
				alts[i] = regexpEscapeLiteral(strArg(e))
			}
			scheme = "(?:" + uriJoin(alts, "|") + ")"
		}
	}
	return scheme + `:(?://(?:[^/?#@\s]*@)?[^/?#:\s]*(?::\d+)?)?[^?#\s]*(?:\?[^#\s]*)?(?:#[^\s]*)?`
}

// uriJoin concatenates parts with sep — a local string-join so the binding stays
// dependency-light (strings.Join would be the only use of the package here).
func uriJoin(parts []string, sep string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += sep
		}
		out += p
	}
	return out
}

// registerURIKernel installs Kernel#URI — the URI() shorthand: it passes a URI
// through unchanged and parses anything else, matching MRI. It is a private
// instance method on Kernel (module_function), so a bare URI(...) call resolves it
// on any self.
func (vm *VM) registerURIKernel() {
	kernel := vm.consts["Kernel"].(*RClass)
	fn := func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if u, ok := args[0].(*URI); ok {
			return u
		}
		return vm.uriParse(vm.uriStrOf(args[0]))
	}
	kernel.define("URI", fn)
	kernel.smethods["URI"] = &Method{name: "URI", owner: kernel, native: fn}
}
