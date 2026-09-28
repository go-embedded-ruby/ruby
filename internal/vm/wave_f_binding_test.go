package vm

import (
	"bytes"
	"strings"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// Wave F pins three defects that the report handed over as ONE ("three faces, one
// cause"). Measurement against ruby 4.0.5 refuted that, and the table in
// TestWaveFScopeWitnesses is the refutation: each witness names the single fix
// that moves it, and two witnesses move only when TWO of them are present.
//
//	A  a STRING instance_eval/class_eval/module_eval runs in the CALLER'S local
//	   scope (eval.go evalUnder — eval_string_with_cref, vm_eval.c
//	   ruby_4_0:1983-2016). No block is needed to see this one fail.
//	B  a Binding carries the ENV CHAIN it was captured in, and the block that was
//	   in scope there (vm.go collectBindingLocals, binding.go retargetLocals —
//	   pm_eval_make_iseq's one compile scope per parent_iseq, vm_eval.c
//	   ruby_4_0:1702-1732, and rb_f_local_variables' cfp walk, :2755-2787).
//	C  block_given?/iterator? are real Kernel methods, not only the compiler's
//	   instruction (rb_define_global_function, vm_eval.c ruby_4_0:2879-2880).
//
// Two of the faces are SILENT — a wrong answer with no exception — which is why
// they are pinned by value and not by "it did not raise": `[1].map {
// binding.local_variables }` answered [[]] and an assignment through a Binding
// taken in a block landed in the wrong slot without complaint.

// TestWaveFScopeWitnesses runs every witness and compares against the answer
// measured on ruby 4.0.5. Each `want` in this table was read off the oracle, not
// derived from rbgo.
func TestWaveFScopeWitnesses(t *testing.T) {
	for _, tc := range []struct {
		name, src, want, fix string
	}{
		// --- A: a string instance_eval/class_eval sees the caller's locals. The
		// receiver's own scope is irrelevant and no block is involved.
		{"instance_eval_string_method_scope",
			`def m; y = 5; Object.new.instance_eval("y * 2"); end; p m`, "10", "A"},
		{"class_eval_string_method_scope",
			`def m; y = 5; String.class_eval("y * 2"); end; p m`, "10", "A"},
		{"module_eval_string_method_scope",
			`def m; y = 5; String.module_eval("y * 2"); end; p m`, "10", "A"},
		// A must not cost the definee: a `def` inside instance_eval(String) is still
		// a SINGLETON method of the receiver, and inside class_eval an instance
		// method of the class. Routing both through the caller's binding replaces
		// only self and the cref, exactly as eval_under does.
		{"instance_eval_string_defines_singleton",
			`o = Object.new; o.instance_eval("def only_mine; 1; end"); p [o.only_mine, Object.new.respond_to?(:only_mine)]`,
			"[1, false]", "A"},
		{"class_eval_string_defines_instance_method",
			`class WFA; end; WFA.class_eval("def im; 2; end"); p WFA.new.im`, "2", "A"},
		{"instance_eval_string_self",
			`o = "s"; p o.instance_eval("self")`, `"s"`, "A"},

		// --- B: a Binding taken inside a block reaches the enclosing scope.
		// SILENT face — the answer was [[]] and nothing was raised.
		{"binding_local_variables_in_block",
			`def m; y = 5; [1].map { binding.local_variables }; end; p m`, "[[:y]]", "B"},
		{"eval_reads_enclosing_local_from_block",
			`def m; y = 5; [1].map { eval("y") }; end; p m`, "[5]", "B"},
		{"eval_reads_two_blocks_out",
			`def m; w = 7; [1].map { [1].map { eval("w") } }; end; p m`, "[[7]]", "B"},
		// SILENT face — the write went nowhere and z stayed 1.
		{"eval_writes_enclosing_local_from_block",
			`def m; z = 1; [1].each { eval("z = 99") }; z; end; p m`, "99", "B"},
		// SILENT face — local_variable_set reported the value it had just stored in
		// the WRONG slot, so the read agreed with it and the real local did not move.
		{"local_variable_set_through_block_binding",
			`def m; b = nil; q = 42; [1].each { b = binding }; b.local_variable_set(:q, 43); [b.local_variable_get(:q), q]; end; p m`,
			"[43, 43]", "B"},
		{"local_variable_get_through_block_binding",
			`def m; b = nil; q = 42; [1].each { b = binding }; [b.local_variable_get(:q), b.local_variable_defined?(:q)]; end; p m`,
			"[42, true]", "B"},
		// An inner local of the same name SHADOWS the outer one: slotOf takes the
		// first match and collectBindingLocals adds the innermost scope first.
		{"inner_local_shadows_enclosing",
			`def m; y = 1; [1].map { |x| y = 2; eval("y") }; end; p m`, "[2]", "B"},
		{"block_param_reaches_binding",
			`def m; [7].map { |v| eval("v") }; end; p m`, "[7]", "B"},
		// B also carries the capture-site BLOCK, which is what an eval'd `yield` and
		// `block_given?` read.
		{"eval_string_inherits_the_frame_block",
			`def m; eval("yield"); end; p(m { 7 })`, "7", "B"},
		{"block_given_through_eval_in_block",
			`def m; [1].map { eval("block_given?") }; end; p [m, m {}]`, "[[false], [true]]", "B"},
		{"binding_eval_sees_the_captured_block",
			`def m; b = binding; b.eval("block_given?"); end; p [m, m {}]`, "[false, true]", "B"},

		// --- A and B TOGETHER: neither alone answers these.
		{"instance_eval_string_inside_a_block",
			`def m; y = 5; [1].map { Object.new.instance_eval("y * 2") }; end; p m`, "[10]", "A+B"},
		{"instance_eval_string_yields_to_the_frame_block",
			`def m; Object.new.instance_eval("yield"); end; p(m { 8 })`, "8", "A+B"},

		// --- C: block_given? / iterator? as real methods, answering about the
		// CALLING frame (rb_f_block_given_p walks to the caller's cfp).
		{"send_block_given",
			`def m; self.send(:block_given?); end; p [m, m {}]`, "[false, true]", "C"},
		{"method_object_block_given",
			`def m; method(:block_given?).call; end; p [m, m {}]`, "[false, true]", "C"},
		{"iterator_p_is_a_method",
			`def m; self.send(:iterator?); end; p [m, m {}]`, "[false, true]", "C"},
		{"kernel_lists_them_private",
			`p Kernel.private_instance_methods(false).values_at(*[]).class; p [:block_given?, :iterator?].map { |s| Kernel.private_instance_methods(false).include?(s) }`,
			"Array\n[true, true]", "C"},
		{"kernel_singleton_is_public",
			`p Kernel.block_given?`, "false", "C"},
		{"owner_is_kernel",
			`p Kernel.instance_method(:block_given?).owner`, "Kernel", "C"},
		// rb_define_global_function makes the Object-side copy PRIVATE, so an
		// explicit receiver raises the PRIVATE NoMethodError, not "undefined method".
		{"explicit_receiver_is_private",
			`begin; Object.new.block_given?; rescue NoMethodError => e; p e.message.start_with?("private method"); end`,
			"true", "C"},
		{"respond_to_include_all",
			`p [Object.new.respond_to?(:block_given?), Object.new.respond_to?(:block_given?, true)]`,
			"[false, true]", "C"},
		// The optimised instruction MUST keep working beside the method — MRI keeps
		// both. A bareword inside a block still answers about the enclosing METHOD's
		// block, because the block handler is read off the LOCAL ep (VM_CF_LEP).
		{"bareword_intrinsic_still_answers",
			`def m; block_given?; end; p [m, m {}]`, "[false, true]", "C"},
		{"bareword_in_a_block_reads_the_methods_block",
			`def m; [1].map { block_given? }; end; p [m, m {}]`, "[[false], [true]]", "C"},
		{"arity_is_zero",
			`begin; self.send(:block_given?, 1); rescue ArgumentError => e; p e.message; end`,
			`"wrong number of arguments (given 1, expected 0)"`, "C"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := runSrc(t, tc.src); got != tc.want {
				t.Errorf("fix %s: %s\n got  %q\n want %q (ruby 4.0.5)", tc.fix, tc.src, got, tc.want)
			}
		})
	}
}

// TestWaveFWitnessTableIsDiscriminating is the known-bad control for the table
// above. A table of expectations is only a measurement if a WRONG answer fails
// it, and every entry in TestWaveFScopeWitnesses is a plain string compare —
// which is exactly the shape that silently passes when the harness stops running
// the program at all. Three earlier sweeps in this campaign reported a clean
// total from a runner that read nothing.
//
// So: run a program whose answer is known to be wrong against the same helper and
// require the comparison to REJECT it, and run the fixed one and require the
// comparison to accept it. If runSrc ever returns "" for everything, the first
// half still passes and the second half fails, which is the point.
func TestWaveFWitnessTableIsDiscriminating(t *testing.T) {
	const src = `def m; y = 5; [1].map { eval("y") }; end; p m`
	const correct = "[5]"
	const knownBad = "[[]]" // what rbgo answered for the sibling witness before fix B

	got := runSrc(t, src)
	if got != correct {
		t.Fatalf("control: %s = %q, want %q — the witness itself is broken", src, got, correct)
	}
	if got == knownBad {
		t.Fatalf("control: the comparison accepted the known-bad answer %q", knownBad)
	}
	// And the negative direction: a witness whose `want` is wrong must fail.
	if runSrc(t, src) == "this can never be the output of p" {
		t.Fatal("control: the comparison accepted an impossible answer")
	}
}

// TestWaveFBindingChainResolution drives the Binding's (depth, slot) map in
// package, including the two cases the Ruby-level witnesses cannot reach on
// their own: a name resolved through a THREE-level chain, and the identity map a
// chainless binding keeps.
func TestWaveFBindingChainResolution(t *testing.T) {
	outer := &Env{slots: []object.Value{object.IntValue(1), object.IntValue(2)}}
	mid := &Env{slots: []object.Value{object.IntValue(3)}, parent: outer}
	inner := &Env{slots: []object.Value{object.IntValue(4)}, parent: mid}

	b := &Binding{
		env:   inner,
		names: []string{"i", "m", "o0", "o1"},
		locs:  []bindLoc{{0, 0}, {1, 0}, {2, 0}, {2, 1}},
	}
	for _, tc := range []struct {
		name string
		want int64
	}{{"i", 4}, {"m", 3}, {"o0", 1}, {"o1", 2}} {
		idx := b.slotOf(tc.name)
		if idx < 0 {
			t.Fatalf("slotOf(%q) = -1", tc.name)
		}
		e, slot := b.at(idx)
		if got := e.slots[slot]; got != object.IntValue(tc.want) {
			t.Errorf("%s = %v, want %d", tc.name, got, tc.want)
		}
	}
	// A write must land in the ANCESTOR env, not in the innermost one under an
	// index that names something else there — the silent face of the defect.
	i := b.slotOf("o1")
	e, slot := b.at(i)
	e.slots[slot] = object.IntValue(99)
	if outer.slots[1] != object.IntValue(99) {
		t.Errorf("write through the chain landed in %v, not in the outer env", inner.slots)
	}
	if inner.slots[0] != object.IntValue(4) {
		t.Errorf("write through the chain disturbed the inner env: %v", inner.slots)
	}

	// A binding with no locs is the identity map, which is what every
	// method-level and top-level binding carries.
	flat := &Binding{env: &Env{slots: []object.Value{object.IntValue(7), object.IntValue(8)}}, names: []string{"a", "b"}}
	if e, slot := flat.at(flat.slotOf("b")); e.slots[slot] != object.IntValue(8) {
		t.Errorf("identity map: b = %v, want 8", e.slots[slot])
	}
	// ensureLocs must materialise it BEFORE the first append, or names and slots
	// drift apart the moment a binding-only local is added.
	flat.addLocal("c", object.IntValue(9))
	if got := len(flat.locs); got != 3 {
		t.Fatalf("after addLocal, len(locs) = %d, want 3 (parallel to names)", got)
	}
	if e, slot := flat.at(flat.slotOf("a")); e.slots[slot] != object.IntValue(7) {
		t.Errorf("after addLocal, a = %v, want 7", e.slots[slot])
	}
	if e, slot := flat.at(flat.slotOf("c")); e.slots[slot] != object.IntValue(9) {
		t.Errorf("after addLocal, c = %v, want 9", e.slots[slot])
	}
}

// TestWaveFIteratorPDeprecationWarning drives rb_f_iterator_p's warning arm
// (vm_eval.c ruby_4_0:2826-2831). rb_warn_deprecated reports only when the
// :deprecated category is enabled — off by default since Ruby 3 — so the arm is
// unreachable without turning it on, which is also why `iterator?` looks silent.
// The text and the "file:line: warning: " prefix are ruby 4.0.5's, measured.
func TestWaveFIteratorPDeprecationWarning(t *testing.T) {
	got := runWarn(t, "Warning[:deprecated] = true\ndef m; iterator?; end\np m\n")
	if want := "iterator? is deprecated; use block_given? instead"; !strings.Contains(got, want) {
		t.Errorf("stderr = %q, want it to contain %q", got, want)
	}
	if !strings.Contains(got, ":2: warning: ") {
		t.Errorf("stderr = %q, want the caller's line in the prefix", got)
	}
	// And the control: with the category OFF — the default — nothing is written.
	if quiet := runWarn(t, "def m; iterator?; end\np m\n"); strings.Contains(quiet, "deprecated") {
		t.Errorf("warned with the :deprecated category off: %q", quiet)
	}
}

// TestWaveFIteratorPArity covers iterator?'s own arity check; block_given?'s is
// driven from the witness table.
func TestWaveFIteratorPArity(t *testing.T) {
	const src = `begin; self.send(:iterator?, 1); rescue ArgumentError => e; p e.message; end`
	if got, want := runSrc(t, src), `"wrong number of arguments (given 1, expected 0)"`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestWaveFCallerBlockGivenStructuralCases drives callerBlockGiven's two arms
// that no Ruby program reaches, the way TestFrameBindingWalksOutward does for
// frameBinding: frameNames deeper than frameCode (the clamp), a frame whose scope
// exec has not published (skipped, as rb_vm_get_binding_creatable_next_cfp skips
// one it cannot use), and NO Ruby frame at all — MRI's `cfp != NULL &&` arm, which
// answers false.
func TestWaveFCallerBlockGivenStructuralCases(t *testing.T) {
	vm := New(&bytes.Buffer{})
	iseq := &bytecode.ISeq{File: "f.rb"}

	// No Ruby frame at all: the walk finds nothing and answers false (cfp == NULL).
	vm.frameNames, vm.frameCode = nil, nil
	if vm.callerBlockGiven() {
		t.Error("with no frame at all, want false")
	}

	// An unpublished frame (env == nil) is skipped and the one below it answers.
	blk := &Proc{}
	vm.frameNames = []string{"a", "b"}
	vm.frameCode = []frameCode{
		{iseq: iseq, env: &Env{}, block: blk},
		{iseq: iseq}, // scope not published yet
	}
	if !vm.callerBlockGiven() {
		t.Error("an unpublished frame must be skipped, not answered from")
	}
	// Same shape with no block below: the skip must not invent one.
	vm.frameCode[0].block = nil
	if vm.callerBlockGiven() {
		t.Error("skipping to a block-less frame must answer false")
	}

	// frameNames deeper than frameCode: the index is clamped to the top of
	// frameCode rather than read past it.
	vm.frameNames = []string{"a", "b", "c"}
	vm.frameCode = []frameCode{{iseq: iseq, env: &Env{}, block: blk}}
	if !vm.callerBlockGiven() {
		t.Error("an index past frameCode must clamp to its top frame")
	}
}

// TestWaveFBindingStopsAtAReturnedFrame pins a KNOWN DIVERGENCE from ruby 4.0.5,
// deliberately, because it is the boundary of what collectBindingLocals can see
// and a later wave should find it stated rather than rediscover it.
//
// MRI's rb_f_local_variables has two arms (vm_eval.c ruby_4_0:2763-2785): walk the
// control frames while the enclosing scopes are still on the stack, and otherwise
// vm_collect_local_variables_in_heap, which reads the ESCAPED env's own local
// table. An MRI env can do that because it carries a pointer to its iseq; an rbgo
// Env carries only slots and a parent, so when the enclosing frame has already
// RETURNED there is nothing left to read a name from and the walk stops.
//
// ruby 4.0.5 answers [:q, :e, :pr] here; rbgo answers []. This is unchanged from
// before the wave — the fix neither caused it nor closed it — and closing it needs
// a name table on the Env, which is another wave's file.
func TestWaveFBindingStopsAtAReturnedFrame(t *testing.T) {
	const src = `pr = nil
[1].each { q = 1; pr = proc { binding.local_variables } }
p pr.call`
	if got, want := runSrc(t, src), "[]"; got != want {
		t.Errorf("got %q, want %q — if this now reports names, the heap arm arrived and the comment above is stale", got, want)
	}
}

// TestWaveFKernelLocalVariables covers Kernel#local_variables, which this wave
// held back until fix B landed: its body IS the enclosing-scope walk, so shipping
// it earlier would have shipped a method answering [] inside a block where ruby
// 4.0.5 answers [:y] — divergent rather than merely absent. Every `want` measured
// on ruby 4.0.5.
func TestWaveFKernelLocalVariables(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"method_scope", `def m; a = 1; b = 2; local_variables; end; p m`, "[:a, :b]"},
		// The one the deferral was about.
		{"through_a_block", `def m; y = 5; [1].map { local_variables }; end; p m`, "[[:y]]"},
		// Like Binding#local_variables it lists the whole local TABLE, so a variable
		// assigned later in the same scope is already named.
		{"names_a_later_local", `def m; c = local_variables; d = 1; c; end; p m`, "[:c, :d]"},
		{"through_send", `def m; q = 1; self.send(:local_variables); end; p m`, "[:q]"},
		{"is_private_on_kernel", `p Kernel.private_instance_methods(false).include?(:local_variables)`, "true"},
		{"explicit_receiver_is_private",
			`begin; Object.new.local_variables; rescue NoMethodError => e; p e.message.start_with?("private method"); end`, "true"},
		{"arity_is_zero",
			`begin; self.send(:local_variables, 1); rescue ArgumentError => e; p e.message; end`,
			`"wrong number of arguments (given 1, expected 0)"`},
		// A local shadowed by an inner one of the same name is listed ONCE, because
		// MRI collects into a hash keyed by name.
		{"shadowed_name_listed_once", `def m; y = 1; [1].map { |x| y = 2; local_variables }; end; p m`, "[[:x, :y]]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := runSrc(t, tc.src); got != tc.want {
				t.Errorf("%s\n got  %q\n want %q (ruby 4.0.5)", tc.src, got, tc.want)
			}
		})
	}
}
