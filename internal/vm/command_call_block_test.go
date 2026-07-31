package vm_test

import "testing"

// TestCommandCallBlock is the regression guard for gap G2 (see
// CONFORMANCE-RSPEC.md): a paren-less command call whose trailing argument is
// followed by a do…end block — `foo bar do … end`, `describe "x" do … end`,
// `config.expect_with :rspec do |x| … end`. The block binds to the *outermost*
// command call, not to the last argument (the classic do-vs-brace precedence
// difference). Every expected value is byte-identical to MRI Ruby 4.0.5.
//
// The parse gap closed in go-ruby-parser via commit 0fe341b ("Attach do…end
// block to a receiver command call"); these cases prove rbgo also *executes*
// the construct with MRI semantics (parse → compile → run through eval).
func TestCommandCallBlock(t *testing.T) {
	cases := []struct{ name, src, want string }{
		// yield through a command-call block.
		{"yield_through_cmd", `def foo(x); yield x * 2; end; foo 3 do |n| p n end`, "6\n"},

		// do…end binds to the OUTER command (m1), so m1 sees the block and m2
		// does not. This is the headline precedence rule.
		{"do_binds_outer",
			`def m1(x); "m1:#{block_given?}"; end
def m2; "m2:#{block_given?}"; end
puts(m1 m2 do end)`, "m1:true\n"},

		// {…} binds to the NEAREST call (m2), so m1 sees no block — the
		// contrast that fixes do…end binding must NOT break.
		{"brace_binds_nearest",
			`def m1(x); "m1:#{block_given?}"; end
def m2; "m2:#{block_given?}"; end
puts(m1 m2 { })`, "m1:false\n"},

		// do…end binds to the outer command even when the argument is itself a
		// (paren'd) method call.
		{"do_outer_when_arg_is_call",
			`def inner(v); "inner(#{block_given?})=#{v}"; end
def outer(a); "outer(#{block_given?}):#{a}"; end
puts(outer inner(9) do end)`, "outer(true):inner(false)=9\n"},

		// RSpec DSL shapes.
		{"describe_string_block",
			`def describe(n); "D:#{n}:#{yield}"; end
puts(describe "x" do 7 end)`, "D:x:7\n"},
		{"it_string_flag_block",
			`def it(n, f); "#{n},#{f},#{block_given?}"; end
puts(it "does", :flag do end)`, "does,flag,true\n"},
		{"nested_describe_it",
			`def describe(n); "D:#{n}(#{yield})"; end
def it(n); "I:#{n}=#{yield}"; end
puts(describe "o" do
  it "i" do 42 end
end)`, "D:o(I:i=42)\n"},

		// Receiver command call: recv.meth arg do…end.
		{"receiver_command_block",
			`obj = [1, 2, 3]; obj.each do |x| print x end; puts`, "123\n"},
		{"map_command_block", `p([1, 2, 3].map do |e| e * 2 end)`, "[2, 4, 6]\n"},

		// Block parameter shapes.
		{"no_block_params", `def g; yield; end; p(g do 99 end)`, "99\n"},
		{"two_block_params",
			`def h; yield 3, 4; end; p(h do |a, b| a + b end)`, "7\n"},

		// Command-call block with splat / keyword arguments.
		{"splat_arg_block",
			`def cap(*a); "#{a.inspect}:#{yield}"; end; puts(cap *[1, 2] do 9 end)`, "[1, 2]:9\n"},
		{"kwarg_block",
			`def cap(**k); "#{k.inspect}:#{yield}"; end; puts(cap a: 1 do 8 end)`, "{a: 1}:8\n"},

		// The command-call block is passed as the call's &block and is callable.
		{"block_pass_capture",
			`def grab(&b); b.call(5); end; p(grab do |z| z + 1 end)`, "6\n"},

		// The block closes over the caller's locals across two command calls.
		{"closure_over_outer",
			`total = 0; def run(n); yield n; end
run 5 do |x| total += x end
run 3 do |x| total += x end
p total`, "8\n"},

		// The block body may itself call other methods.
		{"block_body_calls_method",
			`def each_pair; yield 1; yield 2; end
def dbl(n); n * 2; end
each_pair do |n| p dbl(n) end`, "2\n4\n"},

		// Error branch: a command call whose method never yields, then the
		// callee yields with no block passed → LocalJumpError (rescued).
		{"local_jump_error",
			`def g(x); yield x; end
begin
  g 1
rescue LocalJumpError
  puts "caught"
end`, "caught\n"},

		// Regression: the parenthesised form recv.meth(arg) do…end still works.
		{"paren_form_still_works",
			`def foo(x); yield x; end; foo(5) do |n| p n end`, "5\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("%s: src=%q\n got=%q\nwant=%q", c.name, c.src, got, c.want)
		}
	}
}
