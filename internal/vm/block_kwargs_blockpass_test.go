package vm_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/compiler"
	"github.com/go-embedded-ruby/ruby/internal/vm"
	"github.com/go-ruby-parser/parser"
)

// TestYieldSplatNoBlock covers the LocalJumpError raised when `yield(*args)`
// runs in a method that was given no block.
func TestYieldSplatNoBlock(t *testing.T) {
	src := `def m; yield(*[1, 2]); end; m`
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	iseq, err := compiler.Compile(prog)
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	_, err = vm.New(&bytes.Buffer{}).Run(iseq)
	if err == nil || !strings.Contains(err.Error(), "no block given") {
		t.Fatalf("expected a no-block-given LocalJumpError, got %v", err)
	}
}

// TestBlockKeywordParams covers block/lambda keyword parameters: defaulted,
// required, **rest, and the define_singleton_method (callProcMethod) path. The
// VM binds them through exec exactly as a method's keyword params.
func TestBlockKeywordParams(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{"kw_default_absent", `f = proc { |a, b: 9| [a, b] }; p f.call(1)`, "[1, 9]\n"},
		{"kw_default_present", `f = proc { |a, b: 9| [a, b] }; p f.call(1, b: 2)`, "[1, 2]\n"},
		{"kw_required", `f = proc { |a, k:| [a, k] }; p f.call(1, k: 7)`, "[1, 7]\n"},
		{"kw_rest", `f = proc { |a, **rest| [a, rest] }; p f.call(1, x: 2, y: 3)`, "[1, {x: 2, y: 3}]\n"},
		{"kw_rest_only", `f = proc { |**rest| rest }; p f.call(x: 1)`, "{x: 1}\n"},
		{"kw_with_splat", `f = proc { |a, *b, k: 1| [a, b, k] }; p f.call(1, 2, 3, k: 9)`, "[1, [2, 3], 9]\n"},
		{"kw_default_and_rest", `f = proc { |a, b: 9, **r| [a, b, r] }; p f.call(1, b: 2, z: 3)`, "[1, 2, {z: 3}]\n"},
		{"define_singleton_kw_absent", `o=Object.new; o.define_singleton_method(:m){ |a, k: 5| [a,k] }; p o.m(1)`, "[1, 5]\n"},
		{"define_singleton_kw_present", `o=Object.new; o.define_singleton_method(:m){ |a, k: 5| [a,k] }; p o.m(1, k: 9)`, "[1, 9]\n"},
		{"autosplat_with_kw", `f = proc { |a, b, k: 0| [a, b, k] }; p f.call([1, 2])`, "[1, 2, 0]\n"},
		{"lambda_kw", `f = lambda { |a, k: 4| [a, k] }; p f.call(2)`, "[2, 4]\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := eval(t, tt.src); got != tt.want {
				t.Fatalf("eval(%q) = %q, want %q", tt.src, got, tt.want)
			}
		})
	}
}

// TestArgumentForwardingExtras covers `...` forwarding through super, and the
// anonymous `*` / `**` parameter forms forwarded with bare `*` / `**`.
func TestArgumentForwardingExtras(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{"super_forward_dots", `
class A; def initialize(*a, **k); @v = [a, k]; end; def v; @v; end; end
class B < A; def initialize(...); super(...); end; end
p B.new(1, x: 2).v`, "[[1], {x: 2}]\n"},
		{"super_forward_leading", `
class A; def initialize(a, *r, **k); @v = [a, r, k]; end; def v; @v; end; end
class B < A; def initialize(x, ...); super(x, ...); end; end
p B.new(9, 1, x: 2).v`, "[9, [1], {x: 2}]\n"},
		{"super_forward_explicit_lead", `
class A; def initialize(*a); @v = a; end; def v; @v; end; end
class B < A; def initialize(...); super(0, ...); end; end
p B.new(1, 2).v`, "[0, 1, 2]\n"},
		{"super_forward_block", `
class A; def m(*a, &b); [a, (b && b.call)]; end; end
class B < A; def m(...); super(...); end; end
p B.new.m(1, &proc { 7 })`, "[[1], 7]\n"},
		{"anon_splat_forward", `def g(*a); a; end; def m(*); g(*); end; p m(1, 2)`, "[1, 2]\n"},
		{"anon_kwsplat_forward", `def g(**k); k; end; def m(**); g(**); end; p m(x: 1, y: 2)`, "{x: 1, y: 2}\n"},
		{"anon_both_forward", `def g(*a, **k); [a, k]; end; def m(*, **); g(*, **); end; p m(1, x: 2)`, "[[1], {x: 2}]\n"},
		{"anon_splat_with_explicit", `def g(*a); a; end; def m(*); g(0, *, 9); end; p m(1, 2)`, "[0, 1, 2, 9]\n"},
		{"yield_splat", `def m; yield(*[1, 2]); end; m { |a, b| p [a, b] }`, "[1, 2]\n"},
		{"yield_splat_mixed", `def m; yield(1, *[2, 3], 4); end; m { |*a| p a }`, "[1, 2, 3, 4]\n"},
		{"yield_splat_empty", `def m; yield(*[]); end; m { p 99 }`, "99\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := eval(t, tt.src); got != tt.want {
				t.Fatalf("eval(%q) = %q, want %q", tt.src, got, tt.want)
			}
		})
	}
}

// TestSplatInRescueAndCase covers `rescue *classes` and `case … when *array`,
// including the no-subject `when *array` truthiness form and a mixed clause.
func TestSplatInRescueAndCase(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{"rescue_splat_match", `EX = [ArgumentError, TypeError]
begin; raise TypeError, "t"; rescue *EX => e; p e.message; end`, "\"t\"\n"},
		{"rescue_splat_nomatch", `EX = [KeyError]
begin; raise ArgumentError, "a"; rescue *EX; p :caught; rescue => e; p :fallback; end`, ":fallback\n"},
		{"rescue_splat_param", `def h(*errs); begin; raise ArgumentError, "x"; rescue *errs => e; e.message; end; end
p h(ArgumentError)`, "\"x\"\n"},
		{"when_splat_hit", `A = [1, 2]; case 2; when *A; p :hit; else; p :no; end`, ":hit\n"},
		{"when_splat_miss", `A = [1, 2]; case 5; when *A; p :hit; else; p :no; end`, ":no\n"},
		{"when_splat_regexp", `case "ab"; when *[/x/, /a/]; p :rx; else; p :no; end`, ":rx\n"},
		{"when_splat_mixed", `A = [1]; case 1; when 0, *A, 9; p :hit; else; p :no; end`, ":hit\n"},
		{"when_splat_no_subject_hit", `A = [nil, false, 7]; case; when *A; p :hit; else; p :no; end`, ":hit\n"},
		{"when_splat_no_subject_miss", `A = [nil, false]; case; when *A; p :hit; else; p :no; end`, ":no\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := eval(t, tt.src); got != tt.want {
				t.Fatalf("eval(%q) = %q, want %q", tt.src, got, tt.want)
			}
		})
	}
}

// TestBlockPassAfterKwargs covers a `&blk` block-pass that the parser places
// before a trailing keyword hash (so it is not the last argument), plus the
// anonymous `&` forwarding form, in both command and parenthesised calls and
// through super.
func TestBlockPassAfterKwargs(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{"command_pos_kw_block", `def sc(*a, **k, &b); [a,k,(b&&b.call)]; end; p(sc 1, x: 2, &proc{9})`, "[[1], {x: 2}, 9]\n"},
		{"paren_pos_kw_block", `def sc(*a, **k, &b); [a,k,(b&&b.call)]; end; p sc(1, x: 2, &proc{9})`, "[[1], {x: 2}, 9]\n"},
		{"kw_only_block", `def sc(**k, &b); [k,(b&&b.call)]; end; p(sc x: 2, &proc{9})`, "[{x: 2}, 9]\n"},
		{"anon_block_forward", `def a(&); b(&); end; def b; yield; end; a{p 7}`, "7\n"},
		{"anon_block_with_arg", `def a(x, &); b(x, &); end; def b(y); yield y; end; a(5){|z| p z}`, "5\n"},
		{"block_pass_no_kw", `def sc(*a, &b); [a,(b&&b.call)]; end; p(sc 1, &proc{8})`, "[[1], 8]\n"},
		{"super_block_after_kw", `
class A
  def m(*a, **k, &b); [a, k, (b && b.call)]; end
end
class B < A
  def m(*a, **k, &b); super; end
end
p B.new.m(1, x: 2, &proc{4})`, "[[1], {x: 2}, 4]\n"},
		{"super_explicit_block_after_kw", `
class A
  def m(*a, **k, &b); [a, k, (b && b.call)]; end
end
class B < A
  def m; super(1, x: 2, &proc{6}); end
end
p B.new.m`, "[[1], {x: 2}, 6]\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := eval(t, tt.src); got != tt.want {
				t.Fatalf("eval(%q) = %q, want %q", tt.src, got, tt.want)
			}
		})
	}
}
