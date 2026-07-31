package vm_test

import "testing"

// These tests close four compiler-lowering gaps, each verified byte-for-byte
// against MRI (ruby 4.0.5): a `begin…end while/until` post-loop, named-capture
// `=~` local pre-declaration, `def <expr>.method` on a method-call receiver, and
// anonymous `**` forwarding mixed with explicit keywords or through `yield`.
// The final case pins the go-ruby-parser bump (line-continuation folding).

// --- Gap: begin…end while/until post-loop -----------------------------------

func TestDoWhilePostLoop(t *testing.T) {
	tests := []struct{ name, src, want string }{
		// The trailing condition sees a local first assigned in the body.
		{"until_body_local", `begin; x = (i ||= 0; i += 1); end until x >= 3; p x`, "3\n"},
		{"while_body_local", `i = 0; begin; y = (i += 1); end while y < 2; p [i, y]`, "[2, 2]\n"},
		// The body runs at least once even when the condition is initially false.
		{"body_runs_once_while", `i = 5; begin; i += 1; end while i < 3; p i`, "6\n"},
		{"body_runs_once_until", `i = 5; begin; i += 1; end until i > 3; p i`, "6\n"},
		// `next` re-evaluates the condition; `break` leaves the loop.
		{"next_rechecks_cond", `i = 0; r = []; begin; i += 1; next if i == 2; r << i; end while i < 4; p [i, r]`, "[4, [1, 3, 4]]\n"},
		{"break_exits", `i = 0; begin; i += 1; break if i == 2; end while i < 10; p i`, "2\n"},
		// The post-loop expression itself evaluates to nil.
		{"value_is_nil", `x = (begin; 1; end while false); p x`, "nil\n"},
		// A begin carrying a rescue clause is still a valid post-loop body.
		{"rescue_body", `n = 0; begin; n += 1; raise "x"; rescue; end while n < 2; p n`, "2\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := eval(t, tt.src); got != tt.want {
				t.Fatalf("eval(%q) = %q, want %q", tt.src, got, tt.want)
			}
		})
	}
}

// --- Gap: named-capture regexp `=~` pre-declares capture locals -------------

func TestRegexpNamedCaptureLocals(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"matched", `/(?<a>.)(?<b>.)/ =~ "xy"; p [a, b]`, `["x", "y"]` + "\n"},
		{"optional_group_nil", `/(?<a>.)(?<b>.)?/ =~ "z"; p [a, b]`, `["z", nil]` + "\n"},
		{"non_participating_nil", `/(?<a>x)|(?<b>y)/ =~ "y"; p [a, b]`, `[nil, "y"]` + "\n"},
		{"no_match_all_nil", `/(?<a>z)/ =~ "q"; p a`, "nil\n"},
		// Static declaration: the local exists (nil) even when the match never runs.
		{"static_decl_unrun", `if false; /(?<a>.)/ =~ "z"; end; p a`, "nil\n"},
		// An existing local of the same name is reused (overwritten), not shadowed.
		{"reuses_existing_local", `a = 99; /(?<a>.)/ =~ "z"; p a`, `"z"` + "\n"},
		// Lookbehind is not a capture; the real capture still binds.
		{"lookbehind_not_capture", `/(?<=x)(?<a>.)/ =~ "xy"; p a`, `"y"` + "\n"},
		// The `(?'name'…)` spelling works too.
		{"quote_form", `/(?<a>.)(?'b'.)/ =~ "xy"; p [a, b]`, `["x", "y"]` + "\n"},
		// The whole task example, byte-for-byte with MRI.
		{"task_example", "def parse(s)\n" +
			`  /\bON\b\s*(?<expr>.+)(?:\s*WHERE\b\s*(?<where>.+))?\z/ =~ s` + "\n" +
			`  where = where.sub(/x/, "") if where` + "\n" +
			"  { expr: expr, where: where }\nend\np parse(\"ON a WHERE b\")",
			`{expr: "a WHERE b", where: nil}` + "\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := eval(t, tt.src); got != tt.want {
				t.Fatalf("eval(%q) = %q, want %q", tt.src, got, tt.want)
			}
		})
	}
}

// A named-capture regexp on the RIGHT of `=~` does NOT introduce capture locals;
// the bare name stays an ordinary (here undefined) method reference.
func TestRegexpNamedCaptureRightSideNoLocals(t *testing.T) {
	src := `"ab" =~ /(?<a>.)/; p(defined?(a) ? "local" : "not-local")`
	if got := eval(t, src); got != "\"not-local\"\n" {
		t.Fatalf("eval(%q) = %q, want %q", src, got, "\"not-local\"\n")
	}
}

// An interpolated regexp literal is not a static literal, so it introduces no
// capture locals — matching MRI.
func TestRegexpNamedCaptureInterpolatedNoLocals(t *testing.T) {
	src := `x = "."; (/#{x}(?<a>.)/ =~ "ab"); p(defined?(a) ? "local" : "not-local")`
	if got := eval(t, src); got != "\"not-local\"\n" {
		t.Fatalf("eval(%q) = %q, want %q", src, got, "\"not-local\"\n")
	}
}

// --- Gap: def <method-call receiver>.method ---------------------------------

func TestSingletonDefMethodCallReceiver(t *testing.T) {
	src := `def foo; $obj ||= Object.new; end
def foo.bar; "b"; end
p foo.bar
p foo.equal?(foo)`
	if got := eval(t, src); got != "\"b\"\ntrue\n" {
		t.Fatalf("eval = %q, want %q", got, "\"b\"\ntrue\n")
	}
}

func TestSingletonDefLocalReceiverNoLeak(t *testing.T) {
	src := `a = Object.new
b = Object.new
def a.greet; "hi"; end
p a.greet
p a.respond_to?(:greet)
p b.respond_to?(:greet)`
	want := "\"hi\"\ntrue\nfalse\n"
	if got := eval(t, src); got != want {
		t.Fatalf("eval = %q, want %q", got, want)
	}
}

func TestSingletonDefConstReceiver(t *testing.T) {
	src := `module M; end
def M.hi; "yo"; end
p M.hi`
	if got := eval(t, src); got != "\"yo\"\n" {
		t.Fatalf("eval = %q, want %q", got, "\"yo\"\n")
	}
}

// --- Gap: anonymous ** forwarding (mixed with kwargs, and via yield) --------

func TestAnonKwSplatForwarding(t *testing.T) {
	tests := []struct{ name, src, want string }{
		// Anonymous ** mixed with an explicit keyword.
		{"mixed_with_kwarg",
			`def g(**h); h; end
def f(**); g(inline: "hi", **); end
p f(a: 1, b: 2)`,
			`{inline: "hi", a: 1, b: 2}` + "\n"},
		// A lone anonymous ** still forwards.
		{"lone_anon_kwsplat",
			`def g(**h); h; end
def f(**); g(**); end
p f(x: 9)`,
			"{x: 9}\n"},
		// yield(*, **) forwards both anonymous rest and keywords.
		{"yield_star_kwsplat",
			`def cap(*, **, &b); yield(*, **); end
p cap(1, 2, x: 3) { |*a, **k| [a, k] }`,
			"[[1, 2], {x: 3}]\n"},
		// yield(**) with no keywords passes nothing (empty splat dropped).
		{"yield_empty_kwsplat",
			`def cap(**); yield(**); end
p cap { |*a| a }`,
			"[]\n"},
		// yield(*) forwards positionals only.
		{"yield_star_only",
			`def cap(*); yield(*); end
p cap(1, 2) { |*a| a }`,
			"[1, 2]\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := eval(t, tt.src); got != tt.want {
				t.Fatalf("eval(%q) = %q, want %q", tt.src, got, tt.want)
			}
		})
	}
}

// --- go-ruby-parser bump: `\`+newline line continuation folds to nothing -----

func TestStringLineContinuationBump(t *testing.T) {
	src := "p \"a\\\nb\"" // p "a\<newline>b"  ==> "ab"
	if got := eval(t, src); got != "\"ab\"\n" {
		t.Fatalf("eval(%q) = %q, want %q", src, got, "\"ab\"\n")
	}
}
