// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"testing"
)

// TestWaveDKernelMethodsAreOwnedByKernel pins issue #731: MRI's Object defines
// NOTHING of its own — grepping the 4.0.5 C sources for
// rb_define_method(rb_cObject, …) returns zero hits — and every Kernel method
// reports Kernel as its #owner. rbgo used to build them on Object, so dispatch
// was right (Object includes Kernel) while everything that asks WHERE a method
// lives was wrong. rehomeKernelMethods moves the records; this test is the
// control that keeps them moved.
func TestWaveDKernelMethodsAreOwnedByKernel(t *testing.T) {
	// A representative witness per family, each checked against ruby 4.0.5:
	// object.c (class/inspect/frozen?), io.c (puts/print), eval.c (raise),
	// proc.c (lambda/binding), sprintf.c (format).
	for _, name := range []string{
		"puts", "class", "inspect", "frozen?", "raise", "binding", "lambda",
		"format", "is_a?", "freeze", "object_id", "respond_to?", "require",
		"loop", "catch", "p", "send", "tap", "dup", "itself",
	} {
		src := fmt.Sprintf("p Object.instance_method(%s).owner", symLiteral(name))
		if got := eval(t, src); got != "Kernel\n" {
			t.Errorf("Object.instance_method(%s).owner = %s want Kernel", symLiteral(name), strings.TrimSpace(got))
		}
	}
}

// TestWaveDObjectDefinesNothingKernelDefines is the anti-drift guard that
// replaces the hand-maintained mirror list. The mirror failed one name at a
// time — Kernel#binding was missing from it (#727) while send(:binding) worked
// — because a copy can fall out of step with the body. A record cannot: this
// asserts that no name rehomeKernelMethods claims is still introduced on
// Object, so adding a Kernel method to the table without moving it, or moving
// it without listing it, fails here rather than silently answering Object.
func TestWaveDObjectDefinesNothingKernelDefines(t *testing.T) {
	vm := New(io.Discard)
	all := append(append(append([]string{}, kernelPublicNames...), kernelPrivateNames...), kernelModuleFunctionNames...)
	// There is NO exception left: registerEnumerator still runs from NewVM after
	// rehomeKernelMethods (it needs the prelude's Enumerable), but it now defines
	// enum_for / to_enum on vm.cKernel, so nothing re-introduces them on Object.
	// The empty set is asserted exactly, so a Kernel method built after the
	// re-homing pass — the shape that produced this residue — fails here instead
	// of quietly answering Object.
	const knownResidue = ""
	var residue []string
	for _, name := range all {
		if m := vm.cObject.methods[name]; m != nil {
			residue = append(residue, name)
			if m.owner != vm.cObject {
				t.Errorf("%q is on Object with a non-Object owner %s", name, m.owner.name)
			}
		}
	}
	// The held-back names are a SEPARATE, deliberate exception with its own exact
	// assertion: each must still be introduced on Object with an Object owner (so
	// the out-of-cluster owner comparison that needs it keeps working), and the
	// set must be exactly what builtins.go documents.
	if got := strings.Join(kernelNamesHeldBack, " "); got != "hash" {
		t.Errorf("kernelNamesHeldBack = %q, want exactly \"hash\"; every entry needs "+
			"its out-of-cluster owner comparison named in builtins.go", got)
	}
	for _, name := range kernelNamesHeldBack {
		m := vm.cObject.methods[name]
		if m == nil || m.owner != vm.cObject {
			t.Errorf("held-back %q is not on Object with an Object owner; "+
				"hasCustomHash would then engage for every key", name)
		}
		if vm.cKernel.methods[name] != nil {
			t.Errorf("held-back %q must not be on Kernel", name)
		}
	}
	sort.Strings(residue)
	if got := strings.Join(residue, " "); got != knownResidue {
		t.Errorf("Object still introduces %q; want exactly %q — a NEW name here is "+
			"a Kernel method built after rehomeKernelMethods, and a name that "+
			"DISAPPEARED means the exception can be deleted", got, knownResidue)
	}
	// And the converse: whatever the table moved must be findable on Kernel with
	// a Kernel owner. Names rbgo does not implement at all (Kernel#pp,
	// #local_variables, …) are simply absent from both, which is a missing
	// feature, not a misplaced one.
	moved := 0
	for _, name := range all {
		m := vm.cKernel.methods[name]
		if m == nil {
			continue
		}
		moved++
		if m.owner != vm.cKernel {
			t.Errorf("Kernel.methods[%q].owner = %s want Kernel", name, m.owner.name)
		}
	}
	if moved < 90 {
		t.Fatalf("only %d Kernel methods are homed on Kernel; the table or the "+
			"pass stopped working (the probe must vary with its input)", moved)
	}
}

// TestWaveDKernelModuleFunctionSplit covers the second half of MRI's placement:
// a module function is a PRIVATE instance method of Kernel and a PUBLIC method
// on the Kernel module object, while a plain public Kernel method has no
// singleton counterpart at all. On the oracle these two sets are exactly
// Kernel.private_instance_methods(false) ∩ Kernel.singleton_methods(false) (62
// names) and Kernel.public_instance_methods(false) (43).
func TestWaveDKernelModuleFunctionSplit(t *testing.T) {
	vm := New(io.Discard)
	for _, name := range kernelModuleFunctionNames {
		m := vm.cKernel.methods[name]
		if m == nil {
			continue // not built for this target (fork/exec under wasm) or unimplemented
		}
		if m.vis != visPrivate {
			t.Errorf("Kernel#%s should be a private instance method", name)
		}
		sm := vm.cKernel.smethods[name]
		if sm == nil {
			t.Errorf("Kernel.%s is missing its public module-function counterpart", name)
			continue
		}
		if sm.vis != visPublic {
			t.Errorf("Kernel.%s should be public", name)
		}
	}
	for _, name := range kernelPublicNames {
		if vm.cKernel.methods[name] == nil {
			continue
		}
		if vm.cKernel.methods[name].vis != visPrivate {
			continue
		}
		t.Errorf("Kernel#%s is a plain public method on MRI, not a module function", name)
	}
	// A built-in alias shares ONE record on both halves, so the reflected
	// Method/UnboundMethod objects compare equal, as they do on ruby 4.0.5.
	for _, src := range []string{
		`p Kernel.instance_method(:then) == Kernel.instance_method(:yield_self)`,
		`p Kernel.method(:format) == Kernel.method(:sprintf)`,
	} {
		if got := eval(t, src); got != "true\n" {
			t.Errorf("%s -> %s want true", src, strings.TrimSpace(got))
		}
	}
}

// TestWaveDRehomingDoesNotUnwrapBuiltinSubclasses is the ablation control for
// the second half of the fix. callNative dispatches a value class's own natives
// against the WRAPPED value of a user subclass of String/Array/…; Kernel had to
// join Object and BasicObject in isBuiltinValueMethod's exemption, because every
// value class includes Kernel through Object. Without that line the re-homing
// alone makes KindaClass.new.is_a?(KindaClass) answer false and #class answer
// String — measured, not assumed.
func TestWaveDRehomingDoesNotUnwrapBuiltinSubclasses(t *testing.T) {
	cases := []struct{ src, want string }{
		{`class KS < String; end; p KS.new("x").is_a?(KS)`, "true"},
		{`class KS2 < String; end; p KS2.new("x").class`, "KS2"},
		{`class KS3 < String; end; p KS3.new("x").instance_of?(KS3)`, "true"},
		{`class KS4 < String; end; p KS4.new("x").upcase`, `"X"`},
		{`class KA < Array; end; p KA.new([1]).is_a?(KA)`, "true"},
		{`class KA2 < Array; end; p KA2.new([1,2]).dup.class`, "KA2"},
		{`class KA3 < Array; end; p KA3.new([1,2]).map { |x| x * 2 }`, "[2, 4]"},
		{`class KS5 < String; end; s = KS5.new("x"); s.instance_variable_set(:@a, 1); p s.instance_variables`, "[:@a]"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}

// symLiteral renders name as a Ruby Symbol literal, quoting the operator-ish
// names (`, !~, <=>) that a bare :name cannot express in every position.
func symLiteral(name string) string {
	for _, r := range name {
		if !(r == '_' || r == '?' || r == '!' || r == '=' ||
			(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
			return `:"` + name + `"`
		}
	}
	return ":" + name
}
