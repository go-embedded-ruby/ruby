package vm

import "testing"

// frameOwner reads frameCrefs[i], which exec pushes as nil and fills in once
// the frame's lexical scope is settled. A backtrace taken while that entry is
// still nil must report the bare name rather than dereference it, so the guard
// is exercised here against a stack built by hand — the state itself is
// transient inside one exec call and cannot be held still from Ruby.
func TestFrameOwnerNilCref(t *testing.T) {
	vm := New(nil)
	vm.frameNames = []string{"foo"}
	vm.frameCrefs = []*RClass{nil}
	vm.frameMethods = []frameMethod{{orig: "foo", callee: "foo"}}
	if owner, singleton, ok := vm.frameOwner(0, "foo"); ok {
		t.Errorf("nil cref: got (%v, %v, %v), want no owner", owner, singleton, ok)
	}
	if got := vm.frameLabel(0); got != "foo" {
		t.Errorf("frameLabel with nil cref = %q, want %q", got, "foo")
	}
}

// frameCrefs and frameMethods mirror frameNames one-for-one, and a frame index
// means the same frame in all three only while they are the same length. Three
// unwind sites outside this package's ownership truncate frameNames alone
// (internal/vm/find.go, irb_bind.go, pstore.go), so a drift is reachable; when
// it happens the label must LOSE the owner prefix rather than report some other
// frame's owner.
func TestFrameStacksAlignedGuard(t *testing.T) {
	vm := New(nil)
	k := newClass("K", vm.cObject)
	vm.frameNames = []string{"foo"}
	vm.frameCrefs = []*RClass{k}
	vm.frameMethods = []frameMethod{{orig: "foo", callee: "foo"}}
	k.methods["foo"] = &Method{name: "foo", owner: k}

	if !vm.frameStacksAligned() {
		t.Fatal("stacks should start aligned")
	}
	if got := vm.frameLabel(0); got != "K#foo" {
		t.Fatalf("aligned: frameLabel = %q, want %q", got, "K#foo")
	}

	// A leaked cref entry (frameNames truncated alone) is the drift shape.
	vm.frameCrefs = append(vm.frameCrefs, k)
	if vm.frameStacksAligned() {
		t.Fatal("drifted frameCrefs still reported aligned")
	}
	if got := vm.frameLabel(0); got != "foo" {
		t.Errorf("drifted: frameLabel = %q, want the bare %q", got, "foo")
	}

	// The same for frameMethods on its own.
	vm.frameCrefs = vm.frameCrefs[:1]
	vm.frameMethods = append(vm.frameMethods, frameMethod{})
	if vm.frameStacksAligned() {
		t.Fatal("drifted frameMethods still reported aligned")
	}
	if got := vm.frameLabel(0); got != "foo" {
		t.Errorf("drifted: frameLabel = %q, want the bare %q", got, "foo")
	}
}

// genMethodName drops the prefix unless the owner's class path is PERMANENT —
// MRI takes it through rb_mod_name0, which reports permanent_classpath beside
// the path and returns the bare name when it is missing or temporary
// (variable.c v3_4_0:104).
func TestGenMethodNamePermanence(t *testing.T) {
	named := newClass("K", nil)
	named.named = true
	anon := newClass("", nil)
	// A class carrying a name it has not been permanently bound to — the
	// Module#set_temporary_name shape, which leaves name set and named false.
	// Checked on ruby 4.0.5: a Class.new given set_temporary_name("fake name")
	// still labels its method "m", not "fake name#m".
	temp := newClass("Temp", nil)
	temp.named = false

	cases := []struct {
		owner     *RClass
		singleton bool
		want      string
	}{
		{named, false, "K#m"},
		{named, true, "K.m"},
		{anon, false, "m"},
		{anon, true, "m"},
		{temp, false, "m"},
		{nil, false, "m"},
		{nil, true, "m"},
	}
	for _, c := range cases {
		if got := genMethodName(c.owner, c.singleton, "m"); got != c.want {
			t.Errorf("genMethodName(%v, %v, \"m\") = %q, want %q", c.owner, c.singleton, got, c.want)
		}
	}
	// A permanently-bound class whose name is somehow empty still takes no
	// prefix: an empty owner name would render "#m".
	empty := newClass("", nil)
	empty.named = true
	if got := genMethodName(empty, false, "m"); got != "m" {
		t.Errorf("empty permanent name: got %q, want %q", got, "m")
	}
}
