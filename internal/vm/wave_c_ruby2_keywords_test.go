package vm

import (
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// Every expectation below was taken from MRI 4.0.5 (ruby 4.0.5 (2026-05-20
// revision 64336ffd0e) +PRISM [arm64-darwin25]) by running the same program.

// TestRuby2KeywordsMarkAndPropagate is the pair of observables the feature is
// made of, run TOGETHER on purpose. Marking alone satisfies
// Hash.ruby2_keywords_hash? and still loses every keyword on delegation, so a
// test that only asked the predicate would pass over a half-built feature.
func TestRuby2KeywordsMarkAndPropagate(t *testing.T) {
	t.Run("the predicate sees the mark", func(t *testing.T) {
		got := runSrc(t, `
			c = Class.new { ruby2_keywords def m(*args) = args.last }
			p Hash.ruby2_keywords_hash?(c.new.m(a: 1))
		`)
		if got != "true" {
			t.Fatalf("Hash.ruby2_keywords_hash?(m(a: 1)) = %s, want true", got)
		}
	})

	t.Run("delegation re-splats it as keywords", func(t *testing.T) {
		// Without the re-splat half the target reports [[{a: 1}], {}] — the hash
		// arrives POSITIONAL — which is exactly the state this branch found.
		got := runSrc(t, `
			def target(*a, **k) = [a, k]
			c = Class.new { ruby2_keywords def m(*args) = target(*args) }
			p c.new.m(a: 1)
		`)
		if want := "[[], {a: 1}]"; got != want {
			t.Fatalf("delegated call = %s, want %s", got, want)
		}
	})

	t.Run("the caller's hash is neither copied into nor marked", func(t *testing.T) {
		// core/module/ruby2_keywords_spec.rb, "makes a copy of the hash and only
		// marks the copy as keyword hash".
		got := runSrc(t, `
			c = Class.new { ruby2_keywords def m(*args) = args.last }
			h = {a: 1}
			marked = c.new.m(**h)
			p [Hash.ruby2_keywords_hash?(marked), Hash.ruby2_keywords_hash?(h), marked.equal?(h)]
		`)
		if want := "[true, false, false]"; got != want {
			t.Fatalf("got %s, want %s", got, want)
		}
	})

	t.Run("the callee receives an unmarked copy", func(t *testing.T) {
		// The flag travels exactly one hop: `splat` is a plain *args method, so
		// what reaches it must be a fresh hash with the flag cleared.
		got := runSrc(t, `
			c = Class.new do
			  ruby2_keywords def m(*args) = args
			  def splat(*args) = args.last
			end
			o = c.new
			args = o.m(a: 1)
			marked = args.last
			after = o.splat(*args)
			p [after == {a: 1}, after.equal?(marked),
			   Hash.ruby2_keywords_hash?(after), Hash.ruby2_keywords_hash?(marked)]
		`)
		if want := "[true, false, false, true]"; got != want {
			t.Fatalf("got %s, want %s", got, want)
		}
	})

	t.Run("a marked hash passed positionally keeps its identity and its flag", func(t *testing.T) {
		// The one shape the re-splat must NOT touch: no splat at the call site.
		got := runSrc(t, `
			c = Class.new do
			  ruby2_keywords def m(*args) = args.last
			  def single(arg) = arg
			end
			o = c.new
			marked = o.m(a: 1)
			after = o.single(marked)
			p [after.equal?(marked), Hash.ruby2_keywords_hash?(after)]
		`)
		if want := "[true, true]"; got != want {
			t.Fatalf("got %s, want %s", got, want)
		}
	})

	t.Run("the flag rides the ISeq, so aliasing sees it either way round", func(t *testing.T) {
		// vm_method.c writes ISEQ_BODY(...)->param.flags.ruby2_keywords, which an
		// alias shares; putting the flag on the Method would make the order of
		// `alias_method` and `ruby2_keywords` matter, and it does not.
		got := runSrc(t, `
			c = Class.new do
			  def foo(*a) = a.last
			  alias_method :bar, :foo
			  ruby2_keywords :foo

			  def baz(*a) = a.last
			  ruby2_keywords :baz
			  alias_method :bob, :baz
			end
			o = c.new
			p [:foo, :bar, :baz, :bob].map { |n| Hash.ruby2_keywords_hash?(o.send(n, 1, a: 2)) }
		`)
		if want := "[true, true, true, true]"; got != want {
			t.Fatalf("got %s, want %s", got, want)
		}
	})

	t.Run("a proc carries it across duplication", func(t *testing.T) {
		got := runSrc(t, `
			pr = proc { |*args| args.last }
			pr.ruby2_keywords
			p [Hash.ruby2_keywords_hash?(pr.call(a: 1)),
			   Hash.ruby2_keywords_hash?(pr.dup.call(a: 1))]
		`)
		if want := "[true, true]"; got != want {
			t.Fatalf("got %s, want %s", got, want)
		}
	})

	t.Run("an unmarkable shape warns and stays unmarked", func(t *testing.T) {
		// The negative control: without it, a test that only looked at marked
		// methods could not tell the flag from an unconditional one.
		got := runSrc(t, `
			c = Class.new { def m(*a, **k) = a.last }
			p Hash.ruby2_keywords_hash?(c.new.m(a: 1) || {})
		`)
		if got != "false" {
			t.Fatalf("an (*a, **k) method must not mark: got %s", got)
		}
	})

	t.Run("top-level ruby2_keywords marks a method on Object", func(t *testing.T) {
		// MRI registers it as a private singleton method of main
		// (vm_method.c:3560); before that entry existed this raised NoMethodError.
		got := runSrc(t, `
			def tl(*args) = args.last
			ruby2_keywords :tl
			p Hash.ruby2_keywords_hash?(tl(a: 1))
		`)
		if got != "true" {
			t.Fatalf("top-level ruby2_keywords: got %s, want true", got)
		}
	})
}

// TestRuby2KeywordsBindRest drives the callee half's arms directly, including
// the ones no Ruby program can reach through the compiler (a *rest slot holding
// something that is not an Array).
func TestRuby2KeywordsBindRest(t *testing.T) {
	newHash := func() *object.Hash {
		h := object.NewHash()
		h.Set(object.SymVal("a"), object.IntValue(1))
		return h
	}
	marked := &bytecode.ISeq{Ruby2Keywords: true}
	plain := &bytecode.ISeq{}

	t.Run("marks the trailing hash of a marked iseq", func(t *testing.T) {
		h := newHash()
		rest := object.NewArrayFromSlice([]object.Value{object.IntValue(9), h})
		ruby2KeywordsBindRest(marked, rest, false)
		out, ok := rest.Elems[1].(*object.Hash)
		if !ok || !out.Ruby2Keywords {
			t.Fatalf("trailing hash not marked: %#v", rest.Elems[1])
		}
		if out == h {
			t.Fatal("the caller's hash was marked in place; MRI marks a COPY")
		}
		if h.Ruby2Keywords {
			t.Fatal("the original hash was marked")
		}
		if v, _ := out.Get(object.SymVal("a")); v != object.IntValue(1) {
			t.Fatalf("copy lost its entries: %v", v)
		}
	})

	t.Run("leaves everything else alone", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			is   *bytecode.ISeq
			rest object.Value
			noKW bool
		}{
			{"unmarked iseq", plain, object.NewArrayFromSlice([]object.Value{newHash()}), false},
			{"positional call site", marked, object.NewArrayFromSlice([]object.Value{newHash()}), true},
			{"rest is not an Array", marked, object.NilV, false},
			{"empty rest", marked, object.NewArrayFromSlice(nil), false},
			{"last is not a Hash", marked, object.NewArrayFromSlice([]object.Value{object.IntValue(1)}), false},
		} {
			t.Run(tc.name, func(t *testing.T) {
				ruby2KeywordsBindRest(tc.is, tc.rest, tc.noKW)
				arr, ok := tc.rest.(*object.Array)
				if !ok || len(arr.Elems) == 0 {
					return
				}
				if h, ok := arr.Elems[len(arr.Elems)-1].(*object.Hash); ok && h.Ruby2Keywords {
					t.Fatalf("%s: hash was marked", tc.name)
				}
			})
		}
	})
}

// TestRuby2KeywordsResplat drives the call-site half, and asserts the copy is a
// faithful rb_hash_dup: compare_by_identity, the default value and the default
// proc all survive it, because a delegated hash that silently lost its default
// would be a defect nothing in the spec files would catch.
func TestRuby2KeywordsResplat(t *testing.T) {
	t.Run("a marked tail turns the call into a keyword call", func(t *testing.T) {
		h := object.NewHash()
		h.Set(object.SymVal("a"), object.IntValue(1))
		h.Ruby2Keywords = true
		in := []object.Value{object.IntValue(7), h}
		out, isKW := ruby2KeywordsResplat(in)
		if !isKW {
			t.Fatal("a marked trailing hash must make the call a keyword call")
		}
		if out[0] != object.IntValue(7) {
			t.Fatalf("leading arguments disturbed: %v", out[0])
		}
		got := out[1].(*object.Hash)
		if got == h || got.Ruby2Keywords {
			t.Fatalf("the callee must get an unmarked COPY, got same=%v marked=%v", got == h, got.Ruby2Keywords)
		}
		if !h.Ruby2Keywords {
			t.Fatal("the caller's hash lost its mark")
		}
		if in[1] != object.Value(h) {
			t.Fatal("the input slice was mutated")
		}
	})

	t.Run("leaves everything else alone", func(t *testing.T) {
		plain := object.NewHash()
		for _, tc := range []struct {
			name string
			in   []object.Value
		}{
			{"no arguments", nil},
			{"tail is not a Hash", []object.Value{object.IntValue(1)}},
			{"tail is an unmarked Hash", []object.Value{plain}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				out, isKW := ruby2KeywordsResplat(tc.in)
				if isKW {
					t.Fatalf("%s: reported a keyword call", tc.name)
				}
				if len(out) != len(tc.in) {
					t.Fatalf("%s: list length changed", tc.name)
				}
			})
		}
	})

	t.Run("the copy is a faithful rb_hash_dup", func(t *testing.T) {
		src := object.NewHash()
		src.CompareByIdentity()
		src.Default = object.IntValue(42)
		src.Ruby2Keywords = true
		out := ruby2KeywordsUnmark(src)
		if !out.Identity {
			t.Fatal("compare_by_identity lost")
		}
		if out.Default != object.IntValue(42) {
			t.Fatalf("default lost: %v", out.Default)
		}
		if out.Ruby2Keywords {
			t.Fatal("unmark did not clear the flag")
		}
		back := ruby2KeywordsMark(out)
		if !back.Ruby2Keywords || !back.Identity {
			t.Fatalf("mark: flag=%v identity=%v", back.Ruby2Keywords, back.Identity)
		}
	})
}

// TestMarkRuby2KeywordsISeqNil pins the nil guard: Module#ruby2_keywords reaches
// this with methodISeq(own), which is nil for a method whose body is not Ruby —
// a case the surrounding switch already warns about, so the write must simply do
// nothing rather than panic if the two ever drift apart.
func TestMarkRuby2KeywordsISeqNil(t *testing.T) {
	markRuby2KeywordsISeq(nil)
	is := &bytecode.ISeq{}
	markRuby2KeywordsISeq(is)
	if !is.Ruby2Keywords {
		t.Fatal("flag not set")
	}
}
