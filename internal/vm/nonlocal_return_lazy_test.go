package vm_test

import "testing"

// The returnTarget identity token that a frame uses to catch a non-local exit is
// allocated lazily — a frame materialises it only when it creates a block literal
// or routes a `return` through a live ensure/rescue handler. These cases exercise
// that lazy path directly: a frame that reuses the same token across two blocks, a
// captured proc whose home is still live doing a non-local return, retry, and a
// pure leaf call that never materialises a token at all. They pin the invariant
// that lazy allocation changes no unwinding semantics.
func TestNonLocalReturnLazyToken(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		// A frame that creates two block literals must reuse one identity token: the
		// non-local return from the second block still unwinds to this same frame.
		{"two_blocks_reuse_token", `def f
  [1].each { |x| x }
  [2, 3].each { |y| return "ret#{y}" if y == 3 }
  "fell"
end
p f`, "\"ret3\"\n"},
		// A captured proc (home still live) doing a non-local return unwinds to the
		// method it was written in — the token materialised when the proc was made.
		{"captured_proc_live_home", `def g
  pr = proc { return "viaproc" }
  [1].each { pr.call }
  "notreached"
end
p g`, "\"viaproc\"\n"},
		// return through a live ensure handler is routed via this frame's own token
		// (materialised at the return), and the ensure runs before unwinding.
		{"return_through_ensure_reuses", `def f
  [1].each { |x| x }
  begin
    return "done"
  ensure
    $g = "ran"
  end
end
r = f
p [r, $g]`, "[\"done\", \"ran\"]\n"},
		// retry re-enters the begin body; the frame's handler bookkeeping (and its
		// token, if any) stays consistent across the re-entry.
		{"retry", `$tries = 0
def h
  begin
    $tries += 1
    raise "boom" if $tries < 3
    "ok#{$tries}"
  rescue
    retry if $tries < 3
    "gaveup"
  end
end
p h`, "\"ok3\"\n"},
		// A pure leaf method (no block, no ensure) never materialises a token; it
		// must still return its value normally.
		{"leaf_no_token", `def add(a, b) = a + b
p add(2, 3)`, "5\n"},
		// A leaf method returning through nothing but a normal fall-off end.
		{"leaf_bare_return", `def one; return 1; end
p one`, "1\n"},
		// break value out of a block whose method frame created the block lazily.
		{"break_value_lazy", `def f; [1, 2, 3].each { |x| break x * 100 if x == 2 }; end
p f`, "200\n"},
		// next value inside a lazily-tokened frame's block.
		{"next_value_lazy", `def f; [1, 2, 3].map { |x| next 0 if x.even?; x }; end
p f`, "[1, 0, 3]\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("eval(%q) = %q, want %q", tc.src, got, tc.want)
			}
		})
	}
}
