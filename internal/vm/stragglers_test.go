package vm

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/compiler"
	"github.com/go-embedded-ruby/ruby/internal/object"
	"github.com/go-ruby-parser/parser"
)

// These tests close the final coverage stragglers in the method-visibility
// helpers (visibility.go) and the require-time ISeq file stamper (require.go).
// They are white-box (package vm) because the gaps are branches the existing
// Ruby-driven suite cannot reach: a nil resolved method (every caller short-
// circuits on nil before these helpers see it) and the setISeqFile early
// returns. Where the branch IS observable as Ruby behaviour the assertion is
// pinned to MRI Ruby 4.0.5 output.

// runStragglerErr parses, compiles and runs src on a fresh VM, returning any
// runtime RubyError so a NoMethodError message can be asserted against MRI.
func runStragglerErr(t *testing.T, src string) error {
	t.Helper()
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	iseq, err := compiler.Compile(prog)
	if err != nil {
		t.Fatalf("compile %q: %v", src, err)
	}
	_, rerr := New(io.Discard).Run(iseq)
	return rerr
}

// runStragglerOut parses, compiles and runs src on a fresh VM, returning stdout.
func runStragglerOut(t *testing.T, src string) string {
	t.Helper()
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	iseq, err := compiler.Compile(prog)
	if err != nil {
		t.Fatalf("compile %q: %v", src, err)
	}
	var buf bytes.Buffer
	if _, err := New(&buf).Run(iseq); err != nil {
		t.Fatalf("run %q: %v", src, err)
	}
	return buf.String()
}

// TestInstanceVisibilityNilMethod covers visibility.go:39 — instanceVisibility's
// `m == nil → visPublic` fallback. Every production caller (checkVisibility,
// sendVisibilityOf) returns before calling with a nil method, so the branch is
// only reachable by a direct call. A class with no visibility overrides must
// report a never-resolved name as public.
func TestInstanceVisibilityNilMethod(t *testing.T) {
	vm := New(io.Discard)
	c := newClass("C", vm.cObject)
	if got := instanceVisibility(c, "ghost", nil); got != visPublic {
		t.Errorf("instanceVisibility(nil method) = %d, want visPublic(%d)", got, visPublic)
	}
}

// TestClassMethodVisibilityNilMethod covers visibility.go:58 — the same
// `m == nil → visPublic` fallback in classMethodVisibilityOf.
func TestClassMethodVisibilityNilMethod(t *testing.T) {
	vm := New(io.Discard)
	c := newClass("C", vm.cObject)
	if got := classMethodVisibilityOf(c, "ghost", nil); got != visPublic {
		t.Errorf("classMethodVisibilityOf(nil method) = %d, want visPublic(%d)", got, visPublic)
	}
}

// TestSendVisibilityClassReceiverInstanceMethod covers visibility.go:150 — the
// class-receiver-but-ordinary-instance-method path of sendVisibilityOf: the
// receiver is a class, but the resolved method is neither a singleton method of
// the class nor owned by Class itself (it is a plain Object/Kernel instance
// method such as #itself), so visibility resolves on the instance-method chain.
// Reached from Ruby via respond_to? on a class.
func TestSendVisibilityClassReceiverInstanceMethod(t *testing.T) {
	// itself is an Object instance method; a class is an object, so it answers
	// respond_to? — exercising the line-150 instance-method branch. (MRI: true.)
	for _, src := range []string{
		`class C; end; p C.respond_to?(:itself)`,
		`class C; end; p C.respond_to?(:frozen?)`,
	} {
		if got := runStragglerOut(t, src); got != "true\n" {
			t.Errorf("src=%q got=%q want %q", src, got, "true\n")
		}
	}

	// Direct assertion: the resolved Object#itself reports public on the
	// instance-method chain of the class's own class.
	vm := New(io.Discard)
	c := newClass("C", vm.cObject)
	m := vm.findMethod(object.Wrap(c), "itself")
	if m == nil {
		t.Fatal("expected to resolve Object#itself on a class receiver")
	}
	if lookupSMethod(c, "itself") != nil || m.owner == vm.cClass {
		t.Fatalf("itself unexpectedly resolved as a class method (owner=%v)", m.owner.name)
	}
	if got := vm.sendVisibilityOf(object.Wrap(c), "itself", m); got != visPublic {
		t.Errorf("sendVisibilityOf(class, itself) = %d, want visPublic(%d)", got, visPublic)
	}
}

// TestRecvDescModule covers visibility.go:201 — recvDesc's `isModule → "module"`
// wording. Observable from Ruby as a NoMethodError on a private module method,
// which MRI renders "...called for module M".
func TestRecvDescModule(t *testing.T) {
	err := runStragglerErr(t, `module M; def self.f; end; private_class_method :f; end; M.f`)
	if err == nil || !strings.Contains(err.Error(), "private method 'f' called for module M") {
		t.Errorf("got %v, want NoMethodError mentioning \"called for module M\"", err)
	}

	// Direct assertion of both recvDesc class/module wordings.
	vm := New(io.Discard)
	mod := newClass("M", vm.cObject)
	mod.isModule = true
	if got := vm.recvDesc(object.Wrap(mod)); got != "module M" {
		t.Errorf("recvDesc(module) = %q, want %q", got, "module M")
	}
	cls := newClass("C", vm.cObject)
	if got := vm.recvDesc(object.Wrap(cls)); got != "class C" {
		t.Errorf("recvDesc(class) = %q, want %q", got, "class C")
	}
}

// TestSetISeqFileEarlyReturns covers require.go:110 — the early return in
// setISeqFile when the ISeq is nil or already stamped with the target path, plus
// the children recursion that the early return must not block for fresh nodes.
func TestSetISeqFileEarlyReturns(t *testing.T) {
	// nil ISeq: must return without panicking.
	setISeqFile(nil, "/x.rb")

	// already stamped with the same path: a no-op, children left untouched even
	// when they carry a different (stale) path.
	child := &bytecode.ISeq{File: "/old.rb"}
	root := &bytecode.ISeq{File: "/x.rb", Children: []*bytecode.ISeq{child}}
	setISeqFile(root, "/x.rb")
	if child.File != "/old.rb" {
		t.Errorf("already-stamped root recursed into child: child.File=%q, want %q", child.File, "/old.rb")
	}

	// fresh root: stamps the root and recurses into every child.
	gc := &bytecode.ISeq{}
	kid := &bytecode.ISeq{Children: []*bytecode.ISeq{gc}}
	fresh := &bytecode.ISeq{Children: []*bytecode.ISeq{kid}}
	setISeqFile(fresh, "/main.rb")
	for name, got := range map[string]string{"root": fresh.File, "child": kid.File, "grandchild": gc.File} {
		if got != "/main.rb" {
			t.Errorf("setISeqFile did not stamp %s: File=%q, want %q", name, got, "/main.rb")
		}
	}
}
