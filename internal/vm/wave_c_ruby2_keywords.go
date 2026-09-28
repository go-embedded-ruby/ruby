package vm

import (
	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// ruby2_keywords is a delegation device, not a calling convention: it exists so
// that `def m(*args); target(*args); end` can forward keywords it never declared.
// Two halves make it work, and either one alone is invisible.
//
//  1. MARKING. When a method (or proc) whose ISeq carries
//     param.flags.ruby2_keywords is called WITH keywords, MRI copies the trailing
//     keyword Hash and sets RHASH_PASS_AS_KEYWORDS on the copy before binding it
//     into the *rest (setup_parameters_complex, vm_args.c at tag ruby_4_0 — the
//     `ISEQ_BODY(iseq)->param.flags.ruby2_keywords && kw_flag` arm, which calls
//     rb_hash_dup + RHASH_SET_PASS_AS_KEYWORDS). The ORIGINAL is left alone, which
//     is what core/module/ruby2_keywords_spec.rb states as "makes a copy of the
//     hash and only marks the copy as keyword hash".
//
//  2. RE-SPLAT. When an argument Array is splatted at a call site and its last
//     element is a marked Hash, that call is treated as carrying a keyword splat
//     (vm_caller_setup_arg_splat sets VM_CALL_KW_SPLAT). The hash handed to the
//     callee is a fresh UNMARKED copy, so the flag does not travel further than
//     one delegation hop unless the next callee is itself ruby2_keywords.
//
// Marking without re-splatting passes Hash.ruby2_keywords_hash? and still fails
// every delegation, so both halves are tested together in
// wave_c_ruby2_keywords_test.go.
//
// The one shape that must NOT be touched is a marked Hash passed as an ordinary
// positional argument — `obj.single(marked)` — which keeps its identity and its
// flag. That is why the re-splat lives in applyKWSplat, which only the array
// dispatch opcodes reach, and not in the generic send path.

// ruby2KeywordsMark returns the Hash to bind into a ruby2_keywords *rest in
// place of h: a copy of h carrying the pass-as-keywords flag. MRI's
// rb_hash_dup + RHASH_SET_PASS_AS_KEYWORDS.
func ruby2KeywordsMark(h *object.Hash) *object.Hash {
	out := copyHashPreservingShape(h)
	out.Ruby2Keywords = true
	return out
}

// ruby2KeywordsUnmark returns a copy of h with the flag cleared, which is what a
// re-splatted keyword hash is delivered as.
func ruby2KeywordsUnmark(h *object.Hash) *object.Hash {
	out := copyHashPreservingShape(h)
	out.Ruby2Keywords = false
	return out
}

// copyHashPreservingShape duplicates a Hash the way MRI's rb_hash_dup does:
// entries in insertion order, plus the compare_by_identity flag, the default
// value and the default proc. The frozen bit is deliberately NOT copied — a dup
// of a frozen Hash is mutable.
func copyHashPreservingShape(h *object.Hash) *object.Hash {
	out := newHashLike(h) // preserves compare_by_identity
	out.Default = h.Default
	out.DefaultProc = h.DefaultProc
	for _, k := range h.Keys {
		v, _ := h.Get(k)
		out.Set(k, v)
	}
	return out
}

// ruby2KeywordsBindRest applies half 1 to a *rest that has just been filled.
// It is a no-op unless the callee's ISeq is marked, the call site passed its
// last argument AS keywords (noKW == false — see bytecode.FlagSendNoKW), and
// that last argument is a Hash.
//
// rest is the Array already stored in the splat slot; the replacement is written
// back in place, so the caller needs no return value.
func ruby2KeywordsBindRest(iseq *bytecode.ISeq, rest object.Value, noKW bool) {
	if !iseq.Ruby2Keywords || noKW {
		return
	}
	arr, ok := rest.(*object.Array)
	if !ok || len(arr.Elems) == 0 {
		return
	}
	last := len(arr.Elems) - 1
	h, ok := arr.Elems[last].(*object.Hash)
	if !ok {
		return
	}
	arr.Elems[last] = ruby2KeywordsMark(h)
}

// ruby2KeywordsResplat applies half 2. It reports whether the last element of a
// splatted argument list is a marked Hash and, when it is, returns the list with
// that element replaced by an unmarked copy. The second result is the verdict to
// hand the callee: false means "these ARE keywords", matching the sense of
// bytecode.FlagSendNoKW.
//
// The element is replaced rather than left in place because the callee must not
// be able to observe the caller's flagged hash: core/module/ruby2_keywords_spec.rb
// requires `after_usage.should_not.equal?(marked)` and
// `Hash.ruby2_keywords_hash?(after_usage).should == false` for every delegation
// shape, while `marked` itself stays flagged.
func ruby2KeywordsResplat(elems []object.Value) ([]object.Value, bool) {
	n := len(elems)
	if n == 0 {
		return elems, false
	}
	h, ok := elems[n-1].(*object.Hash)
	if !ok || !h.Ruby2Keywords {
		return elems, false
	}
	out := make([]object.Value, n)
	copy(out, elems)
	out[n-1] = ruby2KeywordsUnmark(h)
	return out, true
}

// markRuby2KeywordsISeq sets the flag on a method's or proc's ISeq. It is the
// write MRI performs as ISEQ_BODY(...)->param.flags.ruby2_keywords = 1; the
// caller has already checked that the shape can carry it
// (procRuby2KeywordsMarkable).
func markRuby2KeywordsISeq(is *bytecode.ISeq) {
	if is != nil {
		is.Ruby2Keywords = true
	}
}
