package vm_test

import "testing"

// A `return` inside a block is a non-local return: it unwinds to the method the
// block was written in, past any iterator or intermediate method frames. A
// lambda's `return` stays local. ensure bodies run on every unwind path.
func TestNonLocalReturn(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		// return from a block passed to a native iterator
		{"each", `def f; [1,2,3].each { |v| return "r#{v}" if v == 2 }; "fell"; end; p f`, "\"r2\"\n"},
		// return through a pure-Ruby iterator (each_with_index → yield)
		{"each_with_index", `def f; [1,2,3].each_with_index { |v, i| return "r#{v}" if v == 2 }; "fell"; end; p f`, "\"r2\"\n"},
		// return through a user method that yields
		{"yield_chain", `def y; [1, 2].each { |x| yield x }; end
def f; y { |v| return "got#{v}" }; "fell"; end; p f`, "\"got1\"\n"},
		// return through map keeps the returned value, not the partial array
		{"map", `def f; [1,2,3].map { |x| return "early" if x == 2; x }; end; p f`, "\"early\"\n"},
		// deeply nested blocks
		{"nested", `def f; [1].each { [2].each { return "deep" } }; "fell"; end; p f`, "\"deep\"\n"},
		// a lambda's return is local: it returns from the lambda only
		{"lambda_local", `l = lambda { return 10; 20 }; p l.call`, "10\n"},
		{"lambda_in_method", `def f; l = lambda { return 99 }; x = l.call; "m#{x}"; end; p f`, "\"m99\"\n"},
		// ensure runs when a non-local return unwinds through it
		{"ensure_on_nonlocal", `def f
  [1].each { begin; return "r"; ensure; $g = "ran"; end }
end
f; p $g`, "\"ran\"\n"},
		// ensure runs on an ordinary method return too
		{"ensure_on_method_return", `def f; begin; return 1; ensure; $g = "ran"; end; end; f; p $g`, "\"ran\"\n"},
		// ensure value never overrides the returned value
		{"ensure_value_ignored", `def f; begin; return "real"; ensure; "ens"; end; end; p f`, "\"real\"\n"},
		// nested ensures both run, innermost first, on a non-local return
		{"nested_ensure", `def f
  $g = []
  [1].each do
    begin
      begin
        return "ret"
      ensure
        $g << "in"
      end
    ensure
      $g << "out"
    end
  end
end
f; p $g`, "[\"in\", \"out\"]\n"},
		// next with ensure: the ensure runs each iteration, map collects each value
		{"next_with_ensure", `def f; [1, 2].map { |x| begin; next x * 10; ensure; $g = (($g || 0) + 1); end }; end
r = f; p [r, $g]`, "[[10, 20], 2]\n"},
		// break with ensure: ensure runs, break value is the iterator result
		{"break_with_ensure", `def f; [1, 2].each { begin; break "brk"; ensure; $g = "ran"; end }; end
p [f, $g]`, "[\"brk\", \"ran\"]\n"},
		// throw with ensure: ensure runs while unwinding to the catch
		{"throw_with_ensure", `r = catch(:t) { begin; throw :t, "v"; ensure; $g = "ran"; end }; p [r, $g]`, "[\"v\", \"ran\"]\n"},
		// a plain begin/rescue still catches an exception after these changes
		{"rescue_still_works", `def f; begin; raise "x"; rescue => e; "got-#{e.message}"; end; end; p f`, "\"got-x\"\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("eval(%q) = %q, want %q", tc.src, got, tc.want)
			}
		})
	}
}
