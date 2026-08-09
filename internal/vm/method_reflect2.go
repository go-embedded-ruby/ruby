// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// The compiler encodes an anonymous / forwarded rest, keyword-rest and block
// parameter with these sentinel names (a real Ruby local can never spell them),
// which the reflection surface renders back as MRI's :* / :** / :& glyphs. They
// mirror the (unexported) constants in the compiler package; keep them in sync.
const (
	fwdRestSentinel  = "...rest"
	fwdKwSentinel    = "...kw"
	fwdBlockSentinel = "...blk"
)

// registerMethodReflect2 installs the remaining Method / UnboundMethod
// reflection surface — #parameters, #source_location, #super_method,
// #original_name, #to_s/#inspect — plus Object#public_method and
// Object#singleton_method. It runs after registerMethod and registerReflection,
// so both classes already exist. Behaviour matches MRI 4.0.
func (vm *VM) registerMethodReflect2() {
	cUnbound := vm.consts["UnboundMethod"].(*RClass)

	// #parameters.
	vm.cMethod.define("parameters", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return methodParameters(self.(*BoundMethod).m)
	})
	cUnbound.define("parameters", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return methodParameters(self.(*UnboundMethod).m)
	})

	// #source_location.
	vm.cMethod.define("source_location", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return methodSourceLocation(self.(*BoundMethod).m)
	})
	cUnbound.define("source_location", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return methodSourceLocation(self.(*UnboundMethod).m)
	})

	// #original_name.
	vm.cMethod.define("original_name", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Symbol(methodOriginalName(self.(*BoundMethod).m))
	})
	cUnbound.define("original_name", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Symbol(methodOriginalName(self.(*UnboundMethod).m))
	})

	// #super_method.
	vm.cMethod.define("super_method", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		b := self.(*BoundMethod)
		sm := vm.superMethodAfter(vm.dispatchAncestors(b.recv), b.m.owner, b.name)
		if sm == nil {
			return object.NilV
		}
		return vm.newBoundMethod(b.recv, b.name, sm)
	})
	cUnbound.define("super_method", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		u := self.(*UnboundMethod)
		sm := vm.superMethodAfter(vm.ancestors(u.owner), u.owner, u.name)
		if sm == nil {
			return object.NilV
		}
		return &UnboundMethod{name: u.name, owner: sm.owner, m: sm, vm: vm}
	})

	// #to_s and its alias #inspect (a shared record, so
	// Method.instance_method(:inspect) == Method.instance_method(:to_s), as MRI).
	toS := func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self.ToS())
	}
	vm.cMethod.define("to_s", toS)
	aliasBuiltin(vm.cMethod, "inspect", "to_s")
	cUnbound.define("to_s", toS)
	aliasBuiltin(cUnbound, "inspect", "to_s")

	// Object#public_method: like #method, but a private or protected target
	// raises NameError instead of returning a callable Method.
	vm.cObject.define("public_method", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		name := nameArg(args[0])
		m := vm.resolveMethod(self, name)
		if m == nil {
			return raise("NameError", "undefined method '%s' for %s", name, vm.classOf(self).name)
		}
		if vm.sendVisibilityOf(self, name, m) != visPublic {
			return raise("NameError", "method '%s' for %s is not public", name, vm.classOf(self).name)
		}
		return vm.newBoundMethod(self, name, m)
	})

	// Object#singleton_method: resolves only the receiver's own singleton methods
	// (an object's singleton class, or a class/module's own class methods) — never
	// an ordinary instance method — raising NameError otherwise.
	vm.cObject.define("singleton_method", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		name := nameArg(args[0])
		m := vm.singletonOnlyMethod(self, name)
		if m == nil || m.undefined {
			return raise("NameError", "undefined singleton method '%s' for %s", name, vm.classOf(self).name)
		}
		return vm.newBoundMethod(self, name, m)
	})
}

// singletonOnlyMethod resolves name among recv's singleton methods only: the
// methods on an object's singleton class (and modules mixed into it), or a
// class/module's own class methods. Returns nil when name is not a singleton
// method (e.g. it is only an ordinary instance method, or absent).
func (vm *VM) singletonOnlyMethod(recv object.Value, name string) *Method {
	if cls, ok := recv.(*RClass); ok {
		if m, ok := cls.smethods[name]; ok {
			return m
		}
	}
	if sc := vm.objSingleton(recv); sc != nil {
		return lookupOwnOrIncluded(sc, name)
	}
	return nil
}

// dispatchAncestors returns the ancestor list a method call on recv walks: recv's
// singleton class chain when it has one (so `extend`ed modules are included),
// otherwise its class chain.
func (vm *VM) dispatchAncestors(recv object.Value) []*RClass {
	if sc := vm.objSingleton(recv); sc != nil {
		return vm.ancestors(sc)
	}
	return vm.ancestors(vm.classOf(recv))
}

// superMethodAfter finds the method named name that a `super` from owner would
// reach: the first definition of name among the ancestors that follow owner in
// chain. It returns nil when owner is not in the chain, when nothing above it
// defines name, or when the next definition is an `undef` tombstone.
func (vm *VM) superMethodAfter(chain []*RClass, owner *RClass, name string) *Method {
	i := -1
	for idx, c := range chain {
		if c == owner {
			i = idx
			break
		}
	}
	if i < 0 {
		return nil
	}
	for _, c := range chain[i+1:] {
		if m := lookupOwnOrIncluded(c, name); m != nil {
			if m.undefined {
				return nil
			}
			return m
		}
	}
	return nil
}

// methodOriginalName reports the name a method was originally defined with. A
// define_method transplant of a native body records it explicitly (origName); an
// iseq-backed method — including one reached through `alias`, which shares the
// iseq — recovers it from the iseq's name; anything else is its own name.
func methodOriginalName(m *Method) string {
	if m.origName != "" {
		return m.origName
	}
	if m.iseq != nil {
		return m.iseq.Name
	}
	return m.name
}

// methodSourceLocation returns [file, line] for where a method was defined, or
// nil when the source is unknown (a native method, or an iseq compiled without a
// source file such as the prelude). As with Proc#source_location the VM tracks
// no per-instruction line, so the line is reported as 0.
func methodSourceLocation(m *Method) object.Value {
	is := methodISeq(m)
	if is == nil || is.File == "" {
		return object.NilV
	}
	return object.NewArray(object.NewString(is.File), object.IntValue(0))
}

// methodParameters builds a method's MRI #parameters array: each entry is
// [kind, name] (or a bare [kind] for a native method or a **nil marker). The
// positional shape — leading required, optionals, a *rest, then post required —
// is read from the iseq, followed by the keyword, keyword-rest and block params.
func methodParameters(m *Method) object.Value {
	is := methodISeq(m)
	if is == nil {
		// A native (or ISeq-less) method: MRI reports a single catch-all rest.
		return object.NewArray(object.NewArray(object.Symbol("rest")))
	}
	var out []object.Value
	for i, name := range is.Params {
		switch {
		case i == is.SplatIndex:
			out = append(out, paramPair("rest", displayAnon(name, fwdRestSentinel, "*")))
		case i < is.NumRequired || (is.SplatIndex >= 0 && i > is.SplatIndex):
			out = append(out, paramPair("req", name))
		default:
			out = append(out, paramPair("opt", name))
		}
	}
	for i, kn := range is.KwNames {
		if is.KwRequired[i] {
			out = append(out, paramPair("keyreq", kn))
		} else {
			out = append(out, paramPair("key", kn))
		}
	}
	if is.KwRestSlot >= 0 {
		if name := localName(is, is.KwRestSlot); name == "nil" {
			out = append(out, object.NewArray(object.Symbol("nokey")))
		} else {
			out = append(out, paramPair("keyrest", displayAnon(name, fwdKwSentinel, "**")))
		}
	}
	if is.BlockSlot >= 0 {
		out = append(out, paramPair("block", displayAnon(localName(is, is.BlockSlot), fwdBlockSentinel, "&")))
	}
	return object.NewArray(out...)
}

// paramPair builds a [kind, name] parameter descriptor.
func paramPair(kind, name string) object.Value {
	return object.NewArray(object.Symbol(kind), object.Symbol(name))
}

// displayAnon maps a parameter's stored name to the symbol MRI reports: the
// forwarding sentinel becomes the anonymous glyph (*, ** or &); a name already
// spelled as that glyph (a truly anonymous parameter) stays as-is; any real name
// is returned unchanged.
func displayAnon(name, sentinel, glyph string) string {
	if name == sentinel {
		return glyph
	}
	return name
}

// localName returns the local-variable name at the given slot, or "" when the
// iseq records no name there.
func localName(is *bytecode.ISeq, slot int) string {
	if slot >= 0 && slot < len(is.Locals) {
		return is.Locals[slot]
	}
	return ""
}

// formatCallableString renders a Method / UnboundMethod as MRI's
// #<Method: Recv(Owner)#name(params) file:line> form. kind is "Method" (recv is
// the bound receiver) or "UnboundMethod" (recv is nil, and only the owner is
// shown). A method defined on a per-object singleton class — whose owner has no
// name — is shown as `<receiver-inspect>.name`; otherwise the head is the
// receiver's class, with a "(Owner)" segment when the owner differs from it. The
// "(orig)" annotation appears only when the lookup name differs from the original
// name, and the location suffix only when the source file is known.
func (vm *VM) formatCallableString(kind string, recv object.Value, name string, m *Method) string {
	head, sep := m.owner.name, "#"
	if kind == "Method" {
		if m.owner.name == "" {
			head, sep = vm.send(recv, "inspect", nil, nil).ToS(), "."
		} else if recvClass := vm.classOf(recv); recvClass == m.owner {
			head = recvClass.name
		} else {
			head = recvClass.name + "(" + m.owner.name + ")"
		}
	}
	disp := name
	if orig := methodOriginalName(m); orig != name {
		disp = name + "(" + orig + ")"
	}
	s := "#<" + kind + ": " + head + sep + disp + formatParamList(m)
	if is := methodISeq(m); is != nil && is.File != "" {
		s += " " + is.File + ":0"
	}
	return s + ">"
}

// formatParamList renders a method's parameters as MRI's #<Method: …> signature
// fragment, e.g. "(a, b=..., *c, d, e:, f: ..., **g, &blk)". A native method,
// whose parameters are unknown, renders as "(*)".
func formatParamList(m *Method) string {
	is := methodISeq(m)
	if is == nil {
		return "(*)"
	}
	var toks []string
	for i, name := range is.Params {
		switch {
		case i == is.SplatIndex:
			toks = append(toks, "*"+dropGlyph(displayAnon(name, fwdRestSentinel, "*"), "*"))
		case i < is.NumRequired || (is.SplatIndex >= 0 && i > is.SplatIndex):
			toks = append(toks, name)
		default:
			toks = append(toks, name+"=...")
		}
	}
	for i, kn := range is.KwNames {
		if is.KwRequired[i] {
			toks = append(toks, kn+":")
		} else {
			toks = append(toks, kn+": ...")
		}
	}
	if is.KwRestSlot >= 0 {
		if name := localName(is, is.KwRestSlot); name == "nil" {
			toks = append(toks, "**nil")
		} else {
			toks = append(toks, "**"+dropGlyph(displayAnon(name, fwdKwSentinel, "**"), "**"))
		}
	}
	if is.BlockSlot >= 0 {
		toks = append(toks, "&"+dropGlyph(displayAnon(localName(is, is.BlockSlot), fwdBlockSentinel, "&"), "&"))
	}
	return "(" + joinComma(toks) + ")"
}

// dropGlyph removes a leading anonymous glyph so an anonymous *, ** or & renders
// as just the sigil (e.g. "*" not "**"): when name equals the glyph it is a
// truly anonymous parameter and contributes no trailing name.
func dropGlyph(name, glyph string) string {
	if name == glyph {
		return ""
	}
	return name
}

// joinComma joins tokens with ", " (a small dependency-free strings.Join).
func joinComma(toks []string) string {
	out := ""
	for i, t := range toks {
		if i > 0 {
			out += ", "
		}
		out += t
	}
	return out
}
