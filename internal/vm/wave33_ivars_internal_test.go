package vm

import (
	"bytes"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/compiler"
	"github.com/go-embedded-ruby/ruby/internal/object"
	"github.com/go-ruby-parser/parser"
)

// runOnVM runs src on the given VM and returns its stdout trimmed, so a test can
// keep the VM and read what the run left behind.
func runOnVM(t *testing.T, vm *VM, out *bytes.Buffer, src string) string {
	t.Helper()
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	iseq, err := compiler.Compile(prog)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if _, err := vm.Run(iseq); err != nil {
		t.Fatalf("run: %v", err)
	}
	return strings.TrimRight(out.String(), "\n")
}

// frameDepths is the length of each of the three parallel frame stacks. They
// mirror each other one for one, so index i means the same frame in all three
// only while all three numbers agree.
type frameDepths struct{ names, crefs, methods int }

func depthsOf(vm *VM) frameDepths {
	return frameDepths{len(vm.frameNames), len(vm.frameCrefs), len(vm.frameMethods)}
}

func (d frameDepths) aligned() bool { return d.names == d.crefs && d.names == d.methods }

// installDepthProbe defines a Ruby method that records the three frame-stack
// depths at the moment Ruby calls it. The depths are per-VM state that Run
// resets on the way out, so they have to be read from INSIDE the program.
func installDepthProbe(vm *VM, name string, got *frameDepths) {
	vm.cObject.define(name, func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		*got = depthsOf(vm)
		return object.NilV
	})
}

// A natively-backed value now carries instance variables (#672). Before this,
// ivarTable answered nil for every one of these kinds, so the write was
// DISCARDED and the read answered nil — with nothing raised either way, which is
// what let a singleton attr_accessor look like it worked and store nothing.
//
// MRI stores these in the generic table: variable.c's ivar_set sends T_OBJECT to
// rb_obj_ivar_set, T_CLASS/T_MODULE to rb_class_ivar_set and everything else to
// generic_ivar_set. Every expectation below is MRI 4.0.5's own answer.
func TestGenericIvarsOnNativelyBackedValues(t *testing.T) {
	var buf bytes.Buffer
	src := `
require 'stringio'
[[], {}, "s", proc {}, StringIO.new("x"), (1..2)].each do |o|
  o.instance_variable_set(:@zz, 7)
  print o.instance_variable_get(:@zz), " ", o.instance_variable_defined?(:@zz), " "
end
puts
`
	want := "7 true 7 true 7 true 7 true 7 true 7 true "
	if got := runOnVM(t, New(&buf), &buf, src); got != want {
		t.Errorf("natively-backed ivars:\n got %q\nwant %q", got, want)
	}
}

// instance_variables reports the names of a natively-backed value in
// FIRST-ASSIGNMENT order (MRI's rb_ivar_foreach walks the shape chain in creation
// order), and remove_instance_variable takes one back out.
func TestGenericIvarNamesInAssignmentOrder(t *testing.T) {
	var buf bytes.Buffer
	src := `
s = "order"
s.instance_variable_set(:@b, 2)
s.instance_variable_set(:@a, 1)
p s.instance_variables
s.remove_instance_variable(:@b)
p s.instance_variables
p s.instance_variable_defined?(:@b)
`
	want := "[:@b, :@a]\n[:@a]\nfalse"
	if got := runOnVM(t, New(&buf), &buf, src); got != want {
		t.Errorf("order:\n got %q\nwant %q", got, want)
	}
}

// The idiom that made #672 visible: an attr_accessor on the singleton class of a
// natively-backed object (ruby/spec's FileSpecs.make_closer stores an exception
// that way) has to STORE, and `defined?` has to see it.
func TestSingletonAccessorOnNativeObjectStores(t *testing.T) {
	var buf bytes.Buffer
	src := `
s = "carrier"
class << s
  attr_accessor :payload
end
s.payload = :kept
p s.payload
p s.instance_eval { defined?(@payload) }
`
	want := ":kept\n\"instance-variable\""
	if got := runOnVM(t, New(&buf), &buf, src); got != want {
		t.Errorf("singleton accessor:\n got %q\nwant %q", got, want)
	}
}

// A read must NOT create a generic table. MRI's rb_gen_ivtbl_get only looks up,
// and rb_ivar_defined answers false for an object with no entry, so
// `defined?(@x)` and instance_variable_defined? leave nothing behind — which is
// what keeps a value that never takes an ivar free of cost.
func TestIvarTableReadCreatesNothing(t *testing.T) {
	s := object.NewString("untouched")
	if tbl := ivarTable(s); tbl != nil {
		t.Fatalf("ivarTable on a fresh String = %v, want nil", tbl)
	}
	if got := ivarNamesInOrder(s); len(got) != 0 {
		t.Fatalf("ivarNamesInOrder on a fresh String = %v, want empty", got)
	}
	if g := genericIvarsOf(s, false); g != nil {
		t.Fatal("a read left a generic-table entry behind")
	}
	setIvar(s, "@a", object.Integer(1))
	if got := getIvar(s, "@a"); got != object.Value(object.Integer(1)) {
		t.Fatalf("after setIvar, getIvar = %v, want 1", got)
	}
	if g := genericIvarsOf(s, false); g == nil {
		t.Fatal("a write did not create a generic-table entry")
	}
	// A second write reuses the entry rather than replacing it.
	first := genericIvarsOf(s, true)
	setIvar(s, "@b", object.Integer(2))
	if second := genericIvarsOf(s, true); second != first {
		t.Fatal("a second write created a second generic-table entry")
	}
	if got, want := len(ivarNamesInOrder(s)), 2; got != want {
		t.Fatalf("names after two writes = %d, want %d", got, want)
	}
}

// An immediate keys nothing: it is a Go value, so a copy would answer to the
// entry of every equal copy, and MRI screens immediates out of the ivar API
// altogether (SPECIAL_CONST_P in rb_ivar_defined / rb_ivar_count). The visible
// Ruby path raises FrozenError before reaching here (every immediate is frozen),
// so what is asserted is that nothing is stored and nothing panics.
func TestImmediatesKeyNoGenericTable(t *testing.T) {
	for _, v := range []object.Value{object.Integer(7), object.Float(1.5), object.Symbol("s"), object.Bool(true), object.NilV, nil} {
		if hasIdentity(v) {
			t.Errorf("hasIdentity(%#v) = true, want false", v)
		}
		setIvar(v, "@a", object.Integer(1))
		if got := getIvar(v, "@a"); got != object.Value(object.NilV) {
			t.Errorf("getIvar(%#v) = %v, want nil", v, got)
		}
		if got := ivarNamesInOrder(v); len(got) != 0 {
			t.Errorf("ivarNamesInOrder(%#v) = %v, want empty", v, got)
		}
	}
	if !hasIdentity(object.NewString("x")) {
		t.Error("hasIdentity(*object.String) = false, want true")
	}
}

// ivarStoreOf allocates the map of a kind that owns one lazily, so a value built
// without it (every RObject the VM makes gets one, but the zero value does not)
// takes an ivar rather than panicking on a nil map. A class records no
// assignment order, so its names come out in map order.
func TestIvarStoreOfAllocatesOwnTables(t *testing.T) {
	o := &RObject{}
	setIvar(o, "@a", object.Integer(1))
	if got := getIvar(o, "@a"); got != object.Value(object.Integer(1)) {
		t.Fatalf("RObject with no map: getIvar = %v, want 1", got)
	}
	c := &RClass{name: "K"}
	if tbl := ivarTable(c); tbl != nil {
		t.Fatalf("ivarTable on a bare RClass = %v, want nil", tbl)
	}
	setIvar(c, "@only", object.Integer(2))
	if got := getIvar(c, "@only"); got != object.Value(object.Integer(2)) {
		t.Fatalf("RClass with no map: getIvar = %v, want 2", got)
	}
	names := ivarNamesInOrder(c)
	if len(names) != 1 || names[0] != object.Value(object.Symbol("@only")) {
		t.Fatalf("class ivar names = %v, want [:@only]", names)
	}
	// The two template-context kinds allocate their map on FIRST TOUCH, read
	// included, since a template reads an @ivar the controller may never have set.
	for _, v := range []object.Value{&SinatraCtx{}, &ActionViewBase{}} {
		if tbl := ivarTable(v); tbl == nil {
			t.Errorf("%T: ivarTable did not lazily allocate the ivars map", v)
		}
		setIvar(v, "@ctx", object.Integer(3))
		if got := getIvar(v, "@ctx"); got != object.Value(object.Integer(3)) {
			t.Errorf("%T: getIvar = %v, want 3", v, got)
		}
	}
}

// Find.prune unwinds the block's frames straight to findYield's recover, which
// restores the tracking stacks by hand. It restored frameNames, frameFiles,
// fileStack and requireDirs and LEFT frameCrefs and frameMethods long (#651):
// index i then meant a different frame in each, and frameLabel — which reads all
// three since #649 — stopped naming the right method for the rest of the program.
func TestFindPruneKeepsFrameStacksAligned(t *testing.T) {
	var buf bytes.Buffer
	vm := New(&buf)
	var before, after frameDepths
	installDepthProbe(vm, "__depths_before", &before)
	installDepthProbe(vm, "__depths_after", &after)
	dir := t.TempDir()
	src := `
require 'find'
def labels; caller_locations(0, 2).map(&:label); end
class Box; def peek; labels; end; end
__depths_before
p Box.new.peek
Find.find(` + strconv.Quote(dir) + `) { |_p| Find.prune }
__depths_after
p Box.new.peek
`
	got := runOnVM(t, vm, &buf, src)
	if !before.aligned() || !after.aligned() || before != after {
		t.Errorf("frame depths before=%+v after=%+v, want equal and aligned", before, after)
	}
	want := `["Object#labels", "Box#peek"]` + "\n" + `["Object#labels", "Box#peek"]`
	if got != want {
		t.Errorf("labels across a prune:\n got %s\nwant %s", got, want)
	}
}

// PStore#transaction with #commit unwinds the block the same way, through
// pstore.go's own recover, and left the same two stacks long.
func TestPStoreCommitKeepsFrameStacksAligned(t *testing.T) {
	var buf bytes.Buffer
	vm := New(&buf)
	var before, after frameDepths
	installDepthProbe(vm, "__depths_before", &before)
	installDepthProbe(vm, "__depths_after", &after)
	path := filepath.Join(t.TempDir(), "store.dat")
	src := `
require 'pstore'
def labels; caller_locations(0, 2).map(&:label); end
class Box; def peek; labels; end; end
__depths_before
store = PStore.new(` + strconv.Quote(path) + `)
store.transaction { |s| s[:k] = 1; s.commit }
store.transaction { |s| s[:k] = 2; s.abort }
__depths_after
p Box.new.peek
`
	got := runOnVM(t, vm, &buf, src)
	if !before.aligned() || !after.aligned() || before != after {
		t.Errorf("frame depths before=%+v after=%+v, want equal and aligned", before, after)
	}
	if want := `["Object#labels", "Box#peek"]`; got != want {
		t.Errorf("labels after a commit/abort:\n got %s\nwant %s", got, want)
	}
}

// An exception raised in an IRB statement unwinds past exec's frames to
// irbEval's recover, which restored frameNames and left frameCrefs and
// frameMethods long — for the rest of the session, since the binding outlives
// the statement that raised.
func TestIRBEvalExceptionKeepsFrameStacksAligned(t *testing.T) {
	vm := New(&bytes.Buffer{})
	b := &Binding{env: &Env{}, self: vm.main, definee: vm.cObject}
	before := depthsOf(vm)
	if _, cls, _, raised := vm.irbEval(b, `def boom; raise "x"; end; boom`); !raised || cls != "RuntimeError" {
		t.Fatalf("irbEval: raised=%v cls=%q, want a RuntimeError", raised, cls)
	}
	after := depthsOf(vm)
	if !after.aligned() || after != before {
		t.Errorf("frame depths before=%+v after=%+v, want equal and aligned", before, after)
	}
}
