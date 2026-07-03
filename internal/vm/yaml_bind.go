// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"math/big"
	"strings"
	stdtime "time"

	gotime "github.com/go-composites/time/src"
	yaml "github.com/go-ruby-yaml/yaml"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// yamlResolveClass resolves a (possibly `::`-qualified) class name used in a
// `!ruby/object:Name` / `!ruby/class` / `!ruby/module` tag to its RClass,
// registering a fresh placeholder class (a plain subclass of Object) under the
// top-level name when the program has not defined it — so loading an object whose
// class is unknown still yields a typed instance rather than failing.
func (vm *VM) yamlResolveClass(name string) *RClass {
	parts := strings.Split(name, "::")
	cur := vm.cObject
	var resolved *RClass
	for i, p := range parts {
		v, ok := vm.resolveConst(cur, p)
		if !ok {
			resolved = nil
			break
		}
		c, isClass := object.KindOK[*RClass](v)
		if !isClass {
			resolved = nil
			break
		}
		resolved = c
		if i < len(parts)-1 {
			cur = c
		}
	}
	if resolved != nil {
		return resolved
	}
	// Unknown class: register a placeholder under its qualified name so repeated
	// loads reuse one class object.
	if v, ok := vm.cObject.consts[name]; ok {
		if c, isClass := object.KindOK[*RClass](v); isClass {
			return c
		}
	}
	c := newClass(name, vm.cObject)
	vm.cObject.consts[name] = object.Wrap(c)
	return c
}

// This file is the thin binding between rbgo's Ruby object graph (object.Value
// and the VM's RObject / Set / Time / Regexp / RClass shells) and the
// interpreter-independent value model of github.com/go-ruby-yaml/yaml. The
// emitter and loader themselves live in that library (ported, byte-for-byte,
// from rbgo's former internal YAML); rbgo only translates its values to and from
// the library's `any` model around a single yaml.Dump / yaml.Load call, so the
// Psych-compatible behaviour Puppet's state/report persistence relies on is
// preserved by construction.

// yamlDump serialises a Ruby value to a Psych-compatible document by mapping it
// into the library's value model and calling yaml.Dump. A value with no Psych
// representation (e.g. a Proc) raises a Ruby TypeError, matching the former
// emitter's contract that the report indirector rescues.
func yamlDump(vm *VM, v object.Value) string {
	out, err := yaml.Dump(toYAML(vm, v))
	if err != nil {
		raise("TypeError", "%s", err.Error())
	}
	return out
}

// yamlLoad parses a YAML document string into a Ruby value by calling yaml.Load
// and mapping the result back into the rbgo object graph. A malformed document
// raises Psych::SyntaxError; an empty document loads as nil.
func yamlLoad(vm *VM, src string) object.Value {
	v, err := yaml.Load(src)
	if err != nil {
		raise("Psych::SyntaxError", "%s", yamlErrMessage(err))
	}
	return fromYAML(vm, v)
}

// yamlSafeLoad parses src like yamlLoad but threads the permitted_classes
// allow-list through the library's SafeLoad.
func yamlSafeLoad(vm *VM, src string, permitted []string) object.Value {
	var opts []yaml.Option
	if permitted != nil {
		opts = append(opts, yaml.WithPermittedClasses(permitted...))
	}
	v, err := yaml.SafeLoad(src, opts...)
	if err != nil {
		raise("Psych::SyntaxError", "%s", yamlErrMessage(err))
	}
	return fromYAML(vm, v)
}

// yamlErrMessage extracts the human-readable text of a library error, preferring
// the SyntaxError.Message (Psych-style) over the wrapped Error() form.
func yamlErrMessage(err error) string {
	if se, ok := err.(*yaml.SyntaxError); ok {
		return se.Message
	}
	return err.Error()
}

// --- rbgo value -> library value (for Dump) --------------------------------

// toYAML maps a Ruby value to the go-ruby-yaml value model. The shapes Puppet
// persists (Hash / Array / String / Symbol / Integer / Float / true / false /
// nil / Time, plus the report graph's RObject / Range / Set) all translate; an
// unmapped value is returned as-is so the library raises the dump TypeError.
//
// A per-dump identity cache preserves shared / cyclic references: a Ruby
// reference value (Array / Hash / RObject / Range / Set) reached more than once
// maps to the very same library value, so the library's anchor emitter writes it
// once behind "&N" and aliases it thereafter — matching the former emitter and
// terminating cycles.
func toYAML(vm *VM, v object.Value) yaml.Value {
	return (&yamlToCtx{vm: vm, seen: map[object.Value]yaml.Value{}}).conv(v)
}

// yamlToCtx carries the per-dump identity cache for the rbgo->library mapping.
type yamlToCtx struct {
	vm   *VM
	seen map[object.Value]yaml.Value
}

// conv maps one Ruby value to its library equivalent.
func (c *yamlToCtx) conv(v object.Value) yaml.Value {
	{
		__sw187 := v
		switch {
		case object.IsNil(__sw187):
			n := __sw187
			_ = n
			return nil
		case object.IsBool(__sw187):
			n := object.AsBoolV(__sw187)
			_ = n
			return bool(n)
		case object.IsInt(__sw187):
			n := object.AsInteger(__sw187)
			_ = n
			return int64(n)
		case object.IsKind[*object.Bignum](__sw187):
			n := object.Kind[*object.Bignum](__sw187)
			_ = n
			return n.I
		case object.IsFloat(__sw187):
			n := object.AsFloatV(__sw187)
			_ = n
			return float64(n)
		case object.IsKind[*object.String](__sw187):
			n := object.Kind[*object.String](__sw187)
			_ = n
			return n.Str()
		case object.IsKind[object.Symbol](__sw187):
			n := object.Kind[object.Symbol](__sw187)
			_ = n
			return yaml.Symbol(string(n))
		case object.IsKind[*object.Array](__sw187):
			n := object.Kind[*object.Array](__sw187)
			_ = n
			return c.convArray(n)
		case object.IsKind[*object.Hash](__sw187):
			n := object.Kind[*object.Hash](__sw187)
			_ = n
			return c.convHash(n)
		case object.IsKind[*Time](__sw187):
			n := object.Kind[*Time](__sw187)
			_ = n
			return stdtime.Unix(n.t.ToUnix(), 0).UTC()
		case object.IsKind[*object.Range](__sw187):
			n := object.Kind[*object.Range](__sw187)
			_ = n
			return c.convRange(n)
		case object.IsKind[*RObject](__sw187):
			n := object.Kind[*RObject](__sw187)
			_ = n
			return c.convObject(n)
		case object.IsKind[*Set](__sw187):
			n := object.Kind[*Set](__sw187)
			_ = n
			return c.convSet(n)
		case object.IsKind[*URI](__sw187):
			n := object.Kind[*URI](__sw187)
			_ = n
			return c.convURI(n)
		case object.IsKind[*RClass](__sw187):
			n := object.Kind[*RClass](__sw187)
			_ = n
			if n.isModule {
				return yaml.Module(n.ToS())
			}
			return yaml.Class(n.ToS())
		case object.IsKind[*Regexp](__sw187):
			n := object.Kind[*Regexp](__sw187)
			_ = n
			return &yaml.Regexp{Source: n.source, Flags: orderFlags(n.flags)}
		}
	}
	// An unmapped value (e.g. a Proc): hand it to the library, which returns the
	// dump error yamlDump turns into a Ruby TypeError.
	return v
}

// convBound maps a Range endpoint, where a Go-nil bound (a beginless / endless
// range) becomes a library nil.
func (c *yamlToCtx) convBound(v object.Value) yaml.Value {
	if object.IsNil(v) {
		return nil
	}
	return c.conv(v)
}

// convArray maps a Ruby Array to a library []any, registered in the identity
// cache before its elements so a cyclic / shared array maps to one slice.
func (c *yamlToCtx) convArray(a *object.Array) yaml.Value {
	if cached, ok := c.seen[object.Wrap(a)]; ok {
		return cached
	}
	out := make([]yaml.Value, len(a.Elems))
	c.seen[object.Wrap(a)] = out // the backing array is fixed; elements are filled in place
	for i, el := range a.Elems {
		out[i] = c.conv(el)
	}
	return out
}

// convHash maps an ordered Ruby Hash to the library's ordered *Map, preserving
// key insertion order and caching identity for shared / cyclic hashes.
func (c *yamlToCtx) convHash(h *object.Hash) yaml.Value {
	if cached, ok := c.seen[object.Wrap(h)]; ok {
		return cached
	}
	m := yaml.NewMap()
	c.seen[object.Wrap(h)] = m
	for _, k := range h.Keys {
		val, _ := h.Get(k)
		m.Set(c.conv(k), c.conv(val))
	}
	return m
}

// convRange maps a Ruby Range to a library *Range, caching identity.
func (c *yamlToCtx) convRange(r *object.Range) yaml.Value {
	if cached, ok := c.seen[object.Wrap(r)]; ok {
		return cached
	}
	out := &yaml.Range{Exclusive: r.Exclusive}
	c.seen[object.Wrap(r)] = out
	out.Begin = c.convBound(r.Lo)
	out.End = c.convBound(r.Hi)
	return out
}

// convObject maps an ordinary Ruby object to a !ruby/object tagged value: its
// class name and its instance variables with the leading "@" stripped (the
// library orders them lexicographically, matching the former emitter). The
// library value is cached before its ivars so a self-referential object graph
// terminates.
func (c *yamlToCtx) convObject(o *RObject) yaml.Value {
	if cached, ok := c.seen[object.Wrap(o)]; ok {
		return cached
	}
	className := ""
	if o.class != nil {
		className = o.class.name
	}
	out := &yaml.Object{Class: className, IVars: map[string]yaml.Value{}}
	c.seen[object.Wrap(o)] = out
	for name, val := range o.ivars {
		out.IVars[ivarBareName(name)] = c.conv(val)
	}
	return out
}

// convSet maps a Ruby Set to its Psych shape (!ruby/object:Set with a backing
// "hash" ivar of element->true, in insertion order), caching identity.
func (c *yamlToCtx) convSet(s *Set) yaml.Value {
	if cached, ok := c.seen[object.Wrap(s)]; ok {
		return cached
	}
	out := &yaml.Object{Class: "Set", IVars: map[string]yaml.Value{}}
	c.seen[object.Wrap(s)] = out
	inner := yaml.NewMap()
	s.each(func(m object.Value) { inner.Set(c.conv(m), true) })
	out.IVars["hash"] = inner
	return out
}

// convURI maps a Ruby URI to its Psych shape — a !ruby/object:URI::HTTP (or the
// matching scheme subclass) carrying the component ivars MRI's URI dumps
// (scheme / user / password / host / port / path / query / opaque / fragment),
// an absent component as nil. The userinfo is split into the user / password the
// MRI ivar layout uses; the parser ivar MRI also emits is omitted (a parser
// object has no data worth round-tripping and the loader rebuilds it). This keeps
// a URI in a serialised graph (e.g. a Puppet run report) dumpable, as the former
// pure-Ruby prelude URI was via its plain ivars.
func (c *yamlToCtx) convURI(u *URI) yaml.Value {
	if cached, ok := c.seen[object.Wrap(u)]; ok {
		return cached
	}
	out := &yaml.Object{Class: u.cls.name, IVars: map[string]yaml.Value{}}
	c.seen[object.Wrap(u)] = out
	str := func(s string, has bool) yaml.Value {
		if !has {
			return nil
		}
		return s
	}
	lu := u.u
	user, password := uriUserPassword(lu.Userinfo)
	out.IVars["scheme"] = str(lu.Scheme, lu.Scheme != "")
	out.IVars["user"] = str(user, lu.Userinfo != "")
	out.IVars["password"] = str(password, password != "")
	out.IVars["host"] = str(lu.Host, lu.Host != "")
	out.IVars["port"] = func() yaml.Value {
		if p, ok := lu.EffectivePort(); ok {
			return int64(p)
		}
		return nil
	}()
	out.IVars["path"] = lu.Path
	out.IVars["query"] = str(lu.Query, lu.HasQuery)
	out.IVars["opaque"] = str(lu.Opaque, lu.Opaque != "")
	out.IVars["fragment"] = str(lu.Fragment, lu.HasFrag)
	return out
}

// uriUserPassword splits a URI userinfo ("user:password") into its user and
// password halves for the YAML ivar layout; a userinfo with no ":" is all user.
func uriUserPassword(userinfo string) (string, string) {
	for i := 0; i < len(userinfo); i++ {
		if userinfo[i] == ':' {
			return userinfo[:i], userinfo[i+1:]
		}
	}
	return userinfo, ""
}

// ivarBareName strips a leading "@" from an instance-variable name (the library
// holds ivars by bare name).
func ivarBareName(name string) string {
	if len(name) > 0 && name[0] == '@' {
		return name[1:]
	}
	return name
}

// --- library value -> rbgo value (for Load) --------------------------------

// fromYAML maps a value produced by yaml.Load / yaml.SafeLoad back into the rbgo
// object graph. Shared references the library preserves (an *Object / *Map seen
// twice via an alias is the same Go pointer) are mapped once and cached, so an
// aliased node round-trips to a single rbgo instance.
func fromYAML(vm *VM, v yaml.Value) object.Value {
	return (&yamlFromCtx{vm: vm, seen: map[yaml.Value]object.Value{}}).conv(v)
}

// yamlFromCtx carries the per-load identity cache that preserves shared / aliased
// references across the conversion.
type yamlFromCtx struct {
	vm   *VM
	seen map[yaml.Value]object.Value
}

// conv maps one library value to its rbgo equivalent.
func (c *yamlFromCtx) conv(v yaml.Value) object.Value {
	switch n := v.(type) {
	case nil:
		return object.NilVal()
	case bool:
		return object.BoolValue(bool(object.Bool(n)))
	case int64:
		return object.IntValue(n)
	case *big.Int:
		return object.NormInt(n)
	case float64:
		return object.FloatValue(float64(object.Float(n)))
	case string:
		return object.Wrap(object.NewString(n))
	case yaml.Symbol:
		return object.SymVal(string(object.Symbol(string(n))))
	case []yaml.Value:
		return c.convSeq(n)
	case *yaml.Map:
		return c.convMap(n)
	case stdtime.Time:
		return object.Wrap(&Time{t: gotime.FromUnix(n.Unix())})
	case *yaml.Range:
		return c.convRange(n)
	case *yaml.Object:
		return c.convObject(n)
	case yaml.Class:
		return object.Wrap(c.vm.yamlResolveClass(string(n)))
	case yaml.Module:
		return object.Wrap(c.vm.yamlResolveClass(string(n)))
	case *yaml.Regexp:
		return c.vm.compileRegexp(n.Source, n.Flags)
	}
	// Any value the library does not model maps to nil (defensive: the loader only
	// ever produces the cases above).
	return object.NilVal()
}

// convSeq maps a library sequence to a Ruby Array, caching identity so a shared
// (aliased) sequence becomes a single Array.
func (c *yamlFromCtx) convSeq(s []yaml.Value) object.Value {
	arr := object.NewArrayFromSlice(make([]object.Value, len(s)))
	for i, el := range s {
		arr.Elems[i] = c.conv(el)
	}
	return object.Wrap(arr)
}

// convMap maps a library ordered *Map to a Ruby Hash, caching identity so a
// shared (aliased) mapping becomes a single Hash.
func (c *yamlFromCtx) convMap(m *yaml.Map) object.Value {
	if cached, ok := c.seen[m]; ok {
		return cached
	}
	h := object.NewHash()
	c.seen[m] = object.Wrap(h)
	for _, p := range m.Pairs() {
		h.Set(c.conv(p.Key), c.conv(p.Val))
	}
	return object.Wrap(h)
}

// convRange maps a library *Range to a Ruby Range, a nil bound modelling a
// beginless / endless range.
func (c *yamlFromCtx) convRange(r *yaml.Range) object.Value {
	return object.Wrap(object.NewRange(c.convBound(r.Begin), c.convBound(r.End), r.Exclusive))
}

// convBound maps a Range endpoint, where a library nil bound becomes a Go-nil
// Range bound (rbgo's beginless / endless representation).
func (c *yamlFromCtx) convBound(v yaml.Value) object.Value {
	if v == nil {
		return object.NilVal()
	}
	return c.conv(v)
}

// convObject maps a library *Object back into a Ruby instance of the named class
// (resolved through the VM, registering a placeholder for an unknown class),
// re-prefixing each ivar name with "@". Identity is cached so a shared (aliased)
// object becomes a single instance.
func (c *yamlFromCtx) convObject(o *yaml.Object) object.Value {
	if cached, ok := c.seen[o]; ok {
		return cached
	}
	name := o.Class
	if name == "" {
		name = "Object"
	}
	cls := c.vm.yamlResolveClass(name)
	obj := &RObject{class: cls, ivars: map[string]object.Value{}}
	c.seen[o] = object.Wrap(obj)
	for k, val := range o.IVars {
		obj.ivars["@"+k] = c.conv(val)
	}
	return object.Wrap(obj)
}
