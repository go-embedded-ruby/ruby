package vm

import (
	"io"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// The constant-table helpers carry nil-parent / nil-scope defensive arms that
// the bytecode never reaches (the lexical definee is always at least Object).
// Exercise them directly so the package keeps 100% coverage.

func TestConstTableNilParent(t *testing.T) {
	vm := New(io.Discard)
	// A nil parent resolves to the top-level table, which aliases Object's.
	if got := vm.constTable(nil); &got == nil || !mapsEqualIdentity(got, vm.consts) {
		t.Fatalf("constTable(nil) should alias vm.consts")
	}
	if got := vm.constTable(vm.cObject); !mapsEqualIdentity(got, vm.cObject.consts) {
		t.Fatalf("constTable(cObject) should be cObject's table")
	}
}

func TestAssignConstNilScope(t *testing.T) {
	vm := New(io.Discard)
	// A nil scope (defensive) writes the constant into the top level.
	vm.assignConst(nil, "WhiteboxNilScope", object.IntValue(int64(object.Integer(7))))
	if v, ok := vm.consts["WhiteboxNilScope"]; !ok || v != object.Integer(7) {
		t.Fatalf("assignConst(nil, ...) should write top-level, got %v ok=%v", v, ok)
	}
}

func TestLexParentForNil(t *testing.T) {
	if got := lexParentFor(nil); got != nil {
		t.Fatalf("lexParentFor(nil) = %v, want nil", got)
	}
	vm := New(io.Discard)
	// Object terminates the chain as nil.
	if got := lexParentFor(vm.cObject); got != nil {
		t.Fatalf("lexParentFor(Object) = %v, want nil", got)
	}
	// A real module is its own lexical parent.
	mod := newClass("M", nil)
	mod.isModule = true
	if got := lexParentFor(mod); got != mod {
		t.Fatalf("lexParentFor(module) = %v, want the module", got)
	}
}

func TestEnsureSingletonClass(t *testing.T) {
	vm := New(io.Discard)
	// Classes do not get a side-table singleton (they use metaClass); the
	// *RClass arm returns (nil, false). The bytecode callers guard this before
	// calling, so it is only reachable directly.
	if sc, ok := vm.ensureSingleton(object.Wrap(vm.cObject)); ok || sc != nil {
		t.Fatalf("ensureSingleton(class) = (%v, %v), want (nil, false)", sc, ok)
	}
}

func TestBlockDefineeNilCref(t *testing.T) {
	vm := New(io.Discard)
	// A proc with no captured cref (defensive) runs against the top level.
	if got := vm.blockDefinee(&Proc{}); got != vm.cObject {
		t.Fatalf("blockDefinee(no cref) = %v, want Object", got)
	}
	mod := newClass("M", nil)
	if got := vm.blockDefinee(&Proc{cref: mod}); got != mod {
		t.Fatalf("blockDefinee(cref) = %v, want the cref", got)
	}
}

// TestAnonymousRClassToS covers RClass.ToS's anonymous arms. An anonymous class
// (Class.new) is reachable from Ruby, but an anonymous *module* RClass is not
// (Module.new yields an RObject, and defineModuleIn always names its module), so
// the "#<Module>" arm is exercised directly here.
func TestAnonymousRClassToS(t *testing.T) {
	mod := &RClass{isModule: true}
	if got := mod.ToS(); got != "#<Module>" {
		t.Fatalf("anonymous module ToS = %q, want #<Module>", got)
	}
	cls := &RClass{}
	if got := cls.ToS(); got != "#<Class>" {
		t.Fatalf("anonymous class ToS = %q, want #<Class>", got)
	}
	named := &RClass{name: "Foo"}
	if got := named.ToS(); got != "Foo" {
		t.Fatalf("named ToS = %q, want Foo", got)
	}
}

func mapsEqualIdentity(a, b map[string]object.Value) bool {
	// Two maps alias the same backing store iff a write to one is seen in the
	// other; compare by length plus a probe write/rollback.
	if len(a) != len(b) {
		return false
	}
	const probe = "\x00wb-probe"
	a[probe] = object.NilVal()
	_, seen := b[probe]
	delete(a, probe)
	return seen
}
