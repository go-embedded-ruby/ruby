// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"os"
	"strings"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestConstSourceLocationRecording covers the stamping half of
// Module#const_source_location: a Ruby-level assignment records the file and
// line, a re-assignment MOVES the record (MRI's setup_const_entry runs on every
// const_tbl_update), and a constant this VM defines in Go has no record at all
// and answers the empty array.
func TestConstSourceLocationRecording(t *testing.T) {
	src := `A = 1
p Object.const_source_location(:A)
A = 2
p Object.const_source_location(:A)
p Object.const_source_location(:String)
module M
  B = 1
end
p M.const_source_location(:B)
p M.const_source_location(:NOPE)
`
	// The file is empty because eval() compiles a source string with no path, so
	// every location records line-only — which is exactly the shape that must
	// read back as the EMPTY array rather than as ["", n].
	want := "[]\n[]\n[]\n[]\nnil\n"
	if got := eval(t, src); got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

// TestConstLocationValueRendersARecordedPair covers constLocationValue's
// recorded-location arm, which the source-string tests above cannot reach: they
// compile with no file, and a location with no file is deliberately rendered as
// the empty array.
func TestConstLocationValueRendersARecordedPair(t *testing.T) {
	vm := New(os.Stderr)
	m := newClass("M31", nil)
	m.isModule = true
	m.consts["K"] = nil
	m.constLocs = map[string]constSrcLoc{"K": {file: "some/file.rb", line: 42}}
	loc, ok := vm.constLocationFrom(m, "K", false)
	if !ok {
		t.Fatal("constLocationFrom did not find the entry")
	}
	if got := loc.Inspect(); got != `["some/file.rb", 42]` {
		t.Errorf("recorded location = %s, want [\"some/file.rb\", 42]", got)
	}

	// An entry with no recorded file is MRI's NIL_P(ce->file): the empty array.
	m.consts["L"] = nil
	loc, ok = vm.constLocationFrom(m, "L", false)
	if !ok {
		t.Fatal("constLocationFrom did not find the unstamped entry")
	}
	if got := loc.Inspect(); got != "[]" {
		t.Errorf("unstamped location = %s, want []", got)
	}
}

// TestConstScopeAnswersObjectForTheTopLevel covers constScope's nil arm, which
// mirrors constTable's: the two must agree about which class owns a top-level
// constant, or a `class Foo` at the top level would stamp its location and fire
// const_added somewhere other than where it filed the constant.
func TestConstScopeAnswersObjectForTheTopLevel(t *testing.T) {
	vm := New(os.Stderr)
	if got := vm.constScope(nil); got != vm.cObject {
		t.Errorf("constScope(nil) = %v, want Object", got)
	}
	m := newClass("M31b", nil)
	if got := vm.constScope(m); got != m {
		t.Errorf("constScope(m) = %v, want m", got)
	}
	// And the table it names is the one constTable hands out for the same input:
	// a key written through one must be visible through the other.
	vm.constTable(nil)["ZZ31"] = object.NilV
	if _, ok := vm.constScope(nil).consts["ZZ31"]; !ok {
		t.Error("constTable(nil) and constScope(nil).consts are not the same table")
	}
}

// TestSetNamespacePathStopsOnARing covers the cycle guard. A module reachable
// from its own constant table is a ring MRI does not guard against and this VM
// can be handed; without the guard the walk would not terminate.
func TestSetNamespacePathStopsOnARing(t *testing.T) {
	vm := New(os.Stderr)
	a := newClass("", nil)
	a.isModule = true
	b := newClass("", nil)
	b.isModule = true
	a.consts["B"] = b
	b.consts["A"] = a
	// Pre-seeding a with the seen set is how the second visit arrives: the walk
	// must return without touching it a second time.
	seen := map[*RClass]bool{a: true}
	vm.setNamespacePath(a, "X", nil, seen)
	if a.name != "" {
		t.Errorf("a already seen should be left alone, got name %q", a.name)
	}
	// From a fresh set the same shape terminates and names both.
	vm.setNamespacePath(a, "X", nil, map[*RClass]bool{})
	if a.name != "X" || b.name != "X::B" {
		t.Errorf("ring walk named %q / %q, want \"X\" / \"X::B\"", a.name, b.name)
	}
}

// TestReopenSuperclassMismatch covers vm_check_if_class: a reopen that NAMES a
// superclass must name the one the class already has. Each shape namedSuper can
// be handed is exercised, because only the ones that resolve to a class may
// raise.
func TestReopenSuperclassMismatch(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"matching bare name reopens", `class SA; end
class SB < SA; end
class SB < SA; def x; 1; end; end
p SB.new.x
`, "1\n"},
		{"no superclass named on the reopen", `class SA; end
class SB < SA; end
class SB; def x; 2; end; end
p SB.new.x
`, "2\n"},
		{"an unresolvable name is not a mismatch", `class SB; end
class SB; def x; 3; end; end
p SB.new.x
`, "3\n"},
		{"matching path expression reopens", `class SA; end
class SB < SA; end
class SB < ::SA; def x; 4; end; end
p SB.new.x
`, "4\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("got %q want %q", got, tc.want)
			}
		})
	}

	for _, tc := range []struct{ name, src, class, msg string }{
		{"a different superclass", `class SA; end
class SC; end
class SB < SA; end
class SB < SC; end
`, "TypeError", "superclass mismatch for class SB"},
		{"a different superclass by path", `class SA; end
class SC; end
class SB < SA; end
class SB < ::SC; end
`, "TypeError", "superclass mismatch for class SB"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			class, msg := evalErr(t, tc.src)
			if class != tc.class || !strings.Contains(msg, tc.msg) {
				t.Errorf("got %s: %q, want %s: %q", class, msg, tc.class, tc.msg)
			}
		})
	}
}

// TestUninheritableSuperclasses covers rb_check_inheritable's two refusals past
// the type test (ruby/ruby v3_4_0 class.c:344).
func TestUninheritableSuperclasses(t *testing.T) {
	for _, tc := range []struct{ name, src, class, msg string }{
		{"a singleton class", `o = Object.new
meta = o.singleton_class
class SD < meta; end
`, "TypeError", "can't make subclass of singleton class"},
		{"Class itself", `class SE < ::Class; end`, "TypeError", "can't make subclass of Class"},
		{"a module", `module SM31; end
class SF < ::SM31; end
`, "TypeError", "superclass must be a Class"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			class, msg := evalErr(t, tc.src)
			if class != tc.class || !strings.Contains(msg, tc.msg) {
				t.Errorf("got %s: %q, want %s: %q", class, msg, tc.class, tc.msg)
			}
		})
	}
}

// TestConstAddedFiresForEveryDefinitionForm covers the setConstant pair: a bare
// assignment, a scoped assignment and the `class` / `module` keywords all run
// the hook, and a REOPEN does not — MRI reaches const_added from rb_const_set,
// which vm_define_class only calls when declaring.
func TestConstAddedFiresForEveryDefinitionForm(t *testing.T) {
	src := `module H
  def self.const_added(n); (@seen ||= []) << n; end
  A = 1
  module B; end
  class C; end
  module B; end
  class C; end
end
H::D = 4
p H.instance_variable_get(:@seen)
`
	if got, want := eval(t, src), "[:A, :B, :C, :D]\n"; got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

// TestClassKeywordFiresAPendingAutoload covers rb_autoload_load at the head of
// vm_define_class / vm_define_module: opening an autoloaded constant with the
// keyword must REOPEN what the file defines, not shadow it.
func TestClassKeywordFiresAPendingAutoload(t *testing.T) {
	cls := writeRB(t, "auto_cls.rb", "module MA31; class E; def v; :from_file; end; end; end\n")
	mod := writeRB(t, "auto_mod.rb", "module MA31; module F; def self.v; :mod_from_file; end; end; end\n")
	src := `module MA31
  autoload :E, "` + cls + `"
  autoload :F, "` + mod + `"
end
class MA31::E
end
p MA31::E.new.v
module MA31::F
end
p MA31::F.v
`
	if got, want := eval(t, src), ":from_file\n:mod_from_file\n"; got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

// TestNamedSuperDeclinesWhatItCannotCompare covers namedSuper's two "no
// superclass to compare" arms directly, rather than through Ruby source. Both
// shapes are ones rbgo answers DIFFERENTLY from MRI at the Ruby level — MRI
// raises NameError for an unresolvable superclass and TypeError for a module,
// from the superclass expression, before defineclass runs — so asserting a
// program's output here would pin a divergence instead of the helper's
// contract, which is that a reopen only raises "superclass mismatch" when it
// actually names a class.
func TestNamedSuperDeclinesWhatItCannotCompare(t *testing.T) {
	vm := New(os.Stderr)
	mod := newClass("SMod31", nil)
	mod.isModule = true
	cls := newClass("SCls31", vm.cObject)
	for _, tc := range []struct {
		name      string
		body      *bytecode.ISeq
		superExpr object.Value
		want      *RClass
	}{
		{"names nothing", &bytecode.ISeq{}, nil, nil},
		{"names something that does not resolve", &bytecode.ISeq{Super: "NoSuchConst31"}, nil, nil},
		{"names a module", &bytecode.ISeq{}, mod, nil},
		{"names a non-class value", &bytecode.ISeq{}, object.IntValue(1), nil},
		{"names a class", &bytecode.ISeq{}, cls, cls},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := vm.namedSuper(vm.cObject, tc.body, tc.superExpr); got != tc.want {
				t.Errorf("namedSuper = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestModuleClassPathQualifiesWithAnAnonymousScope covers moduleClassPath's
// anonymous-parent arm. It is reached from Ruby only through the reopen path
// that adopts a lexical parent after the fact, so it is pinned here as the
// contract it is: a module whose recorded lexical home has no permanent name of
// its own is qualified with that home's "#<Module:0x…>" repr, which is
// rb_tmp_class_path's answer for an anonymous scope.
func TestModuleClassPathQualifiesWithAnAnonymousScope(t *testing.T) {
	vm := New(os.Stderr)
	anon := newClass("", nil)
	anon.isModule = true
	child := newClass("Q31", nil)
	child.isModule = true
	child.lexParent = anon
	got := vm.moduleClassPath(child)
	if !strings.HasPrefix(got, "#<Module:0x") || !strings.HasSuffix(got, ">::Q31") {
		t.Errorf("moduleClassPath under an anonymous scope = %q, want \"#<Module:0x…>::Q31\"", got)
	}
}
