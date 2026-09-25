package vm

import (
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestApplyKWSplat drives ignore_keyword_hash_p's rbgo half (vm_args.c
// v3_4_0:506) over each arm: an empty keyword splat is dropped AND its keyword
// bit cleared, so what is left behind is positional; a non-empty one stays and
// keeps the call a keyword call; a site with no keyword splat at all answers
// with whatever the compiler flagged.
func TestApplyKWSplat(t *testing.T) {
	empty := object.NewHash()
	full := object.NewHash()
	full.Set(object.SymVal("k"), object.IntValue(1))
	one := object.IntValue(1)

	t.Run("no kwsplat flag passes the compiled verdict through", func(t *testing.T) {
		in := []object.Value{one, full}
		got, noKW := applyKWSplat(in, 0)
		if len(got) != 2 || noKW {
			t.Fatalf("plain site: got %d args noKW=%v, want 2/false", len(got), noKW)
		}
		got, noKW = applyKWSplat(in, bytecode.FlagSendNoKW)
		if len(got) != 2 || !noKW {
			t.Fatalf("flagged positional site: got %d args noKW=%v, want 2/true", len(got), noKW)
		}
	})

	t.Run("an empty kwsplat is dropped and leaves a positional tail", func(t *testing.T) {
		got, noKW := applyKWSplat([]object.Value{one, empty}, bytecode.FlagSendKWSplat)
		if len(got) != 1 || got[0] != one || !noKW {
			t.Fatalf("f(1, **{}): got %v noKW=%v, want [1]/true", got, noKW)
		}
		// The point of the `true`: f(h, **{}) passes ONE POSITIONAL h, even though
		// h is itself a Hash.
		got, noKW = applyKWSplat([]object.Value{full, empty}, bytecode.FlagSendKWSplat)
		if len(got) != 1 || got[0] != object.Value(full) || !noKW {
			t.Fatalf("f(h, **{}): got %v noKW=%v, want [h]/true", got, noKW)
		}
		// f(**{}) passes no argument at all.
		got, noKW = applyKWSplat([]object.Value{empty}, bytecode.FlagSendKWSplat)
		if len(got) != 0 || !noKW {
			t.Fatalf("f(**{}): got %v noKW=%v, want []/true", got, noKW)
		}
	})

	t.Run("a non-empty kwsplat stays and keeps the call a keyword call", func(t *testing.T) {
		got, noKW := applyKWSplat([]object.Value{one, full}, bytecode.FlagSendKWSplat)
		if len(got) != 2 || noKW {
			t.Fatalf("f(1, **{k: 1}): got %v noKW=%v, want 2 args/false", got, noKW)
		}
	})

	t.Run("a kwsplat site whose tail is not a Hash", func(t *testing.T) {
		// Reachable through `def m(...); g(...); end` when the forwarded **kw was
		// already spliced away: nothing to drop, and the call stays a keyword call.
		got, noKW := applyKWSplat([]object.Value{one}, bytecode.FlagSendKWSplat)
		if len(got) != 1 || noKW {
			t.Fatalf("tail is not a Hash: got %v noKW=%v, want 1 arg/false", got, noKW)
		}
		// An empty argument list cannot carry a keyword hash either.
		got, noKW = applyKWSplat(nil, bytecode.FlagSendKWSplat)
		if len(got) != 0 || noKW {
			t.Fatalf("no arguments: got %v noKW=%v, want []/false", got, noKW)
		}
	})
}

// TestISeqPostCount: post parameters bind from the tail in both shapes MRI
// records them in — after a *splat, and (with none) as ISeq.PostCount.
func TestISeqPostCount(t *testing.T) {
	withSplat := &bytecode.ISeq{Params: []string{"a", "b", "c", "d"}, SplatIndex: 2}
	if got := iseqPostCount(withSplat); got != 1 {
		t.Errorf("def m(a, b, *c, d): iseqPostCount = %d, want 1", got)
	}
	noSplat := &bytecode.ISeq{Params: []string{"a", "b"}, SplatIndex: -1, PostCount: 1}
	if got := iseqPostCount(noSplat); got != 1 {
		t.Errorf("def m(a=1, b): iseqPostCount = %d, want 1", got)
	}
	plain := &bytecode.ISeq{Params: []string{"a", "b"}, SplatIndex: -1}
	if got := iseqPostCount(plain); got != 0 {
		t.Errorf("def m(a, b): iseqPostCount = %d, want 0", got)
	}
}

// TestSetSendNoKW is the seam AOT-lowered Go uses to say what an OpSend says
// through its flags (internal/aot/level2.go, codegen.go).
func TestSetSendNoKW(t *testing.T) {
	vm := New(nil)
	vm.setSendNoKW(true)
	if !vm.sendNoKW {
		t.Error("setSendNoKW(true) did not set the verdict")
	}
	vm.setSendNoKW(false)
	if vm.sendNoKW {
		t.Error("setSendNoKW(false) did not clear the verdict")
	}
}

// TestKeywordsAreDecidedAtTheCallSite runs the separation end to end. Every
// expectation was taken from MRI 4.0.5.
func TestKeywordsAreDecidedAtTheCallSite(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{
			"a positional Hash is not converted to keywords",
			`def foo(a, b, c, **hsh); hsh; end
			 h = {key: 42}
			 begin; foo(1, 2, 3, h); rescue ArgumentError => e; puts e.message; end`,
			"wrong number of arguments (given 4, expected 3)",
		},
		{
			"a keyword splat still is",
			`def foo(a, b, c, **hsh); hsh; end
			 h = {key: 42}
			 p foo(1, 2, 3, **h)`,
			"{key: 42}",
		},
		{
			"literal keywords still are",
			"def foo(a, b, c, **hsh); hsh; end\np foo(1, 2, 3, key: 42)",
			"{key: 42}",
		},
		{
			"a positional Hash against a key: parameter",
			`def foo(key: 1); key; end
			 h = {key: 42}
			 begin; foo(h); rescue ArgumentError => e; puts e.message; end`,
			"wrong number of arguments (given 1, expected 0)",
		},
		{
			"separated for (*args, **kwargs)",
			"def m(*args, **kwargs); [args, kwargs]; end\nempty = {}\np m(empty)",
			"[[{}], {}]",
		},
		{
			"an empty keyword splat passes no argument",
			"def m(*args, **kwargs); [args, kwargs]; end\nempty = {}\np m(**empty)",
			"[[], {}]",
		},
		{
			"an empty keyword splat leaves the argument before it positional",
			"def m(*a); a; end\nempty = {}\nh = {k: 1}\np m(h, **empty)",
			"[{k: 1}]",
		},
		{
			"delegation through (*args, **kwargs)",
			`def target(*args, **kwargs); [args, kwargs]; end
			 def m(*args, **kwargs); target(*args, **kwargs); end
			 empty = {}
			 p m(empty)`,
			"[[{}], {}]",
		},
		{
			"delegation through (...)",
			`def target(*args, **kwargs); [args, kwargs]; end
			 def m(...); target(...); end
			 empty = {}
			 p m(empty)`,
			"[[{}], {}]",
		},
		{
			"yield carries the same verdict",
			`def y(*args, **kwargs); yield(*args, **kwargs); end
			 empty = {}
			 p(y(empty) { |*a| a })`,
			"[{}]",
		},
		{
			"super carries the same verdict",
			`class SP; def go(*a, **k); [a, k]; end; end
			 class SC < SP; def go(*a, **k); super(*a, **k); end; end
			 empty = {}
			 p SC.new.go(empty)`,
			"[[{}], {}]",
		},
		{
			"a bare super forwards a positional Hash as positional",
			`class ZP; def go(*a, **k); [a, k]; end; end
			 class ZC < ZP; def go(*a, **k); super; end; end
			 empty = {}
			 p ZC.new.go(empty)`,
			"[[{}], {}]",
		},
		{
			"a bare super still forwards real keywords as keywords",
			`class YP; def go(*a, **k); [a, k]; end; end
			 class YC < YP; def go(*a, **k); super; end; end
			 p YC.new.go(1, k: 2)`,
			"[[1], {k: 2}]",
		},
		{
			"super(...) forwards a positional Hash as positional",
			`class FP; def go(*a, **k); [a, k]; end; end
			 class FC < FP; def go(...); super(...); end; end
			 empty = {}
			 p FC.new.go(empty)`,
			"[[{}], {}]",
		},
		{
			"Object#send keeps the older behaviour rather than losing keywords",
			"def m(k: 0); k; end\np 1.instance_eval { }; p send(:m, k: 7)",
			"nil\n7",
		},
	} {
		if got := runSrc(t, tc.src); got != tc.want {
			t.Errorf("%s:\ngot  %q\nwant %q", tc.name, got, tc.want)
		}
	}
}

// TestPostParametersWithoutASplat runs the binding end to end, against MRI 4.0.5.
func TestPostParametersWithoutASplat(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"def m(a=1, b) given one", "def m(a=1, b); [a, b]; end\np m(2)", "[1, 2]"},
		{"def m(a=1, b) given two", "def m(a=1, b); [a, b]; end\np m(2, 3)", "[2, 3]"},
		{
			"def m(a=1, b) is not callable with none",
			"def m(a=1, b); end\nbegin; m(); rescue ArgumentError => e; puts e.message; end",
			"wrong number of arguments (given 0, expected 1..2)",
		},
		{
			"def m(a=1, b) is not callable with three",
			"def m(a=1, b); end\nbegin; m(1, 2, 3); rescue ArgumentError => e; puts e.message; end",
			"wrong number of arguments (given 3, expected 1..2)",
		},
		{"def n(a, b=2, c)", "def n(a, b=2, c); [a, b, c]; end\np n(1, 3)", "[1, 2, 3]"},
		{"a block pads to lead+post", "p([[1, 2]].map { |a=5, b, c, d| [a, b, c, d] })", "[[5, 1, 2, nil]]"},
		{"a block with everything supplied", "p(proc { |a=5, b, c, d| [a, b, c, d] }.call(1, 2, 3))", "[5, 1, 2, 3]"},
		{"Proc#arity counts lead+post", "p(proc { |a=5, b, c, d| }.arity)", "3"},
		{"a lambda's arity is negative", "p(->(a=1, b) {}.arity)", "-2"},
		{"a lambda binds post first", "p(->(a=1, b) { [a, b] }.call(5))", "[1, 5]"},
		// The splat shape: post binds from the tail, and the optional in front of
		// it falls back to its DEFAULT rather than eating the post argument.
		{"def q(a, b=9, *c, d)", "def q(a, b=9, *c, d); [a, b, c, d]; end\np q(1, 2)", "[1, 9, [], 2]"},
		{"def q(a, b=9, *c, d) with more", "def q(a, b=9, *c, d); [a, b, c, d]; end\np q(1, 2, 3, 4)", "[1, 2, [3], 4]"},
		{
			"a block's optionals fill left to right after the post reservation",
			"p(proc { |a, b=5, c=6, *d, e, f| [a, b, c, d, e, f] }.call(1))",
			"[1, 5, 6, [], nil, nil]",
		},
	} {
		if got := runSrc(t, tc.src); got != tc.want {
			t.Errorf("%s:\ngot  %q\nwant %q", tc.name, got, tc.want)
		}
	}
}

// TestImplicitItIsInvisible runs the `it` parameter's visibility end to end,
// against MRI 4.0.5. `it` resolves lexically inside the block and is invisible
// to every reflective route out of it; a WRITTEN |it| is an ordinary parameter.
func TestImplicitItIsInvisible(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"a lambda's it is nameless", "p(-> { it }.parameters)", "[[:req]]"},
		{"a proc's it is nameless", "p(proc { it }.parameters)", "[[:opt]]"},
		{"a written |it| keeps its name", "p(proc { |it| it }.parameters)", "[[:opt, :it]]"},
		{"it is not a binding local", `p(-> { it; binding.local_variables }.call("a"))`, "[]"},
		{
			"binding.local_variable_get cannot reach it",
			`begin
			   proc { a = it; binding.local_variable_get(:it) }.call(1)
			 rescue NameError => e
			   puts "NameError"
			 end`,
			"NameError",
		},
		{
			"binding.local_variable_set cannot reach it",
			`p(-> { a = it; binding.local_variable_set(:it, :b); [a, it] }.call(:a))`,
			"[:a, :a]",
		},
		{"defined?(it) is still resolved lexically", `p(-> { it; defined?(it) }.call(1))`, `"local-variable"`},
		{"it still binds the argument", `p(-> { it }.call(7))`, "7"},
		{"it affects arity", "p(-> { it }.arity)", "1"},
		// The numbered parameters are NOT hidden: they are named in #parameters,
		// and for the Ruby version rbgo reports they are visible to a Binding.
		{"_1 keeps its name", "p(proc { _1 }.parameters)", "[[:opt, :_1]]"},
		{"_1 is a binding local", `p(-> { _1; binding.local_variables }.call("a"))`, "[:_1]"},
	} {
		if got := runSrc(t, tc.src); got != tc.want {
			t.Errorf("%s:\ngot  %q\nwant %q", tc.name, got, tc.want)
		}
	}
}

// TestNumberedParameterNameIsReservedAtRuntime: the refusal reaches Ruby as a
// SyntaxError, which is the shape ruby/spec asks the question in.
func TestNumberedParameterNameIsReservedAtRuntime(t *testing.T) {
	src := `begin
	          eval("_1 = 0")
	        rescue SyntaxError => e
	          puts e.message.include?("reserved for numbered parameters")
	        end`
	if got := runSrc(t, src); got != "true" {
		t.Errorf("eval(\"_1 = 0\"): got %q, want a SyntaxError naming the reservation", got)
	}
}
