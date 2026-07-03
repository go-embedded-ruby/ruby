// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"bytes"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// withScriptedClock pins benchmarkMonotonic to a counter that advances by 1.5
// seconds on every read, so MeasureWith (which samples it twice) reports a real
// time of exactly 1.5 per measurement. This makes Benchmark's formatted output
// byte-for-byte deterministic, matching `ruby -rbenchmark` driven by the same
// scripted Process.clock_gettime(CLOCK_MONOTONIC). It returns a restore func.
func withScriptedClock() func() {
	prev := benchmarkMonotonic
	mono := 0.0
	benchmarkMonotonic = func() float64 {
		v := mono
		mono += 1.5
		return v
	}
	return func() { benchmarkMonotonic = prev }
}

// TestBenchmarkValues checks Tms construction, the readers, total, label,
// arithmetic, to_a, to_s and format — all asserted against MRI 4.0.5.
func TestBenchmarkValues(t *testing.T) {
	tests := []struct{ name, src, want string }{
		// Module constants (CAPTION is six leading spaces, the MRI value).
		{"caption", `print Benchmark::CAPTION`, "      user     system      total        real\n"},
		{"format_const", `p Benchmark::FORMAT`, "\"%10.6u %10.6y %10.6t %10.6r\\n\"\n"},
		{"version", `p Benchmark::BENCHMARK_VERSION`, "\"2002-04-25\"\n"},

		// Tms construction and readers.
		{"tms_defaults", `t = Benchmark::Tms.new; p [t.utime, t.stime, t.cutime, t.cstime, t.real, t.total, t.label]`,
			"[0.0, 0.0, 0.0, 0.0, 0.0, 0.0, \"\"]\n"},
		{"tms_fields", `t = Benchmark::Tms.new(1.0, 2.0, 3.0, 4.0, 5.0, "x"); p [t.utime, t.stime, t.cutime, t.cstime, t.real, t.total, t.label]`,
			"[1.0, 2.0, 3.0, 4.0, 5.0, 10.0, \"x\"]\n"},
		{"tms_int_args", `p Benchmark::Tms.new(1, 2, 0, 0, 3).total`, "3.0\n"},
		{"tms_nil_label", `p Benchmark::Tms.new(1, 2, 3, 4, 5, nil).label`, "\"\"\n"},
		{"tms_class", `p Benchmark::Tms.new.class.name`, "\"Benchmark::Tms\"\n"},

		// Arithmetic: memberwise (Tms operand) and scalar (numeric operand).
		{"tms_add", `a = Benchmark::Tms.new(1.0,2.0,3.0,4.0,5.0,"a"); b = Benchmark::Tms.new(0.5,0.5,0.5,0.5,0.5,"b"); p((a+b).to_a)`,
			"[\"\", 1.5, 2.5, 3.5, 4.5, 5.5]\n"},
		{"tms_sub", `a = Benchmark::Tms.new(1.0,2.0,3.0,4.0,5.0,"a"); b = Benchmark::Tms.new(0.5,0.5,0.5,0.5,0.5,"b"); p((a-b).to_a)`,
			"[\"\", 0.5, 1.5, 2.5, 3.5, 4.5]\n"},
		{"tms_mul_scalar", `a = Benchmark::Tms.new(1.0,2.0,3.0,4.0,5.0,"a"); p((a*2).to_a)`,
			"[\"\", 2.0, 4.0, 6.0, 8.0, 10.0]\n"},
		{"tms_div_scalar", `a = Benchmark::Tms.new(1.0,2.0,3.0,4.0,5.0,"a"); p((a/2).to_a)`,
			"[\"\", 0.5, 1.0, 1.5, 2.0, 2.5]\n"},
		{"tms_mul_tms", `a = Benchmark::Tms.new(1.0,2.0,3.0,4.0,5.0,"a"); b = Benchmark::Tms.new(2,2,2,2,2,"b"); p((a*b).to_a)`,
			"[\"\", 2.0, 4.0, 6.0, 8.0, 10.0]\n"},

		// Formatting.
		{"tms_to_s", `p Benchmark::Tms.new(1.0,2.0,3.0,4.0,5.0,"lbl").to_s`,
			"\"  1.000000   2.000000  10.000000 (  5.000000)\\n\"\n"},
		{"tms_format_default", `p Benchmark::Tms.new(1.0,2.0,3.0,4.0,5.0,"lbl").format`,
			"\"  1.000000   2.000000  10.000000 (  5.000000)\\n\"\n"},
		{"tms_format_ext", `p Benchmark::Tms.new(1.0,2.0,3.0,4.0,5.0,"lbl").format("%u %y %t %r %n")`,
			"\"1.000000 2.000000 10.000000 (5.000000) lbl\"\n"},
		{"tms_format_children", `p Benchmark::Tms.new(1.0,2.0,3.0,4.0,5.0,"lbl").format("%U %Y")`,
			"\"3.000000 4.000000\"\n"},
		{"tms_format_args", `p Benchmark::Tms.new(1.0,2.0,3.0,4.0,5.0,"lbl").format("custom %5.2u tail=%s", "x")`,
			"\"custom  1.00 tail=x\"\n"},
		{"tms_format_nil", `p Benchmark::Tms.new(1.0,2.0,3.0,4.0,5.0,"lbl").format(nil)`,
			"\"  1.000000   2.000000  10.000000 (  5.000000)\\n\"\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := eval(t, tt.src); got != tt.want {
				t.Fatalf("src=%q\n got %q\nwant %q", tt.src, got, tt.want)
			}
		})
	}
}

// TestBenchmarkMeasure exercises measure/realtime/ms with the scripted clock,
// asserting the byte-exact MRI rendering and the float results.
func TestBenchmarkMeasure(t *testing.T) {
	restore := withScriptedClock()
	defer restore()

	tests := []struct{ name, src, want string }{
		{"measure_label", `t = Benchmark.measure("lbl") { }; p [t.utime, t.stime, t.real, t.total, t.label]`,
			"[0.0, 0.0, 1.5, 0.0, \"lbl\"]\n"},
		{"measure_no_label", `p Benchmark.measure { }.label`, "\"\"\n"},
		{"measure_to_s", `print Benchmark.measure("lbl") { }.to_s`,
			"  0.000000   0.000000   0.000000 (  1.500000)\n"},
		{"measure_is_tms", `p Benchmark.measure { }.is_a?(Benchmark::Tms)`, "true\n"},
		{"realtime", `p Benchmark.realtime { }`, "1.5\n"},
		{"ms", `p Benchmark.ms { }`, "1500.0\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := eval(t, tt.src); got != tt.want {
				t.Fatalf("src=%q\n got %q\nwant %q", tt.src, got, tt.want)
			}
		})
	}
}

// TestBenchmarkReports exercises bm / benchmark / bmbm rendering and return
// values, all asserted byte-for-byte against MRI 4.0.5 with the scripted clock.
func TestBenchmarkReports(t *testing.T) {
	restore := withScriptedClock()
	defer restore()

	const caption = "              user     system      total        real\n"

	tests := []struct{ name, src, want string }{
		{
			"bm_basic",
			`Benchmark.bm(7) { |x| x.report("first:") { }; x.report("second:") { } }`,
			caption +
				"first:    0.000000   0.000000   0.000000 (  1.500000)\n" +
				"second:   0.000000   0.000000   0.000000 (  1.500000)\n",
		},
		{
			"bm_return",
			`p(Benchmark.bm(7) { |x| x.report("a:") { } }.map { |t| t.real })`,
			caption + "a:        0.000000   0.000000   0.000000 (  1.500000)\n[1.5]\n",
		},
		{
			"bm_labels",
			`Benchmark.bm(7, ">total:", ">avg:") { |x| tf = x.report("first:") { }; ts = x.report("second:") { }; [tf+ts, (tf+ts)/2] }`,
			caption +
				"first:    0.000000   0.000000   0.000000 (  1.500000)\n" +
				"second:   0.000000   0.000000   0.000000 (  1.500000)\n" +
				">total:   0.000000   0.000000   0.000000 (  3.000000)\n" +
				">avg:     0.000000   0.000000   0.000000 (  1.500000)\n",
		},
		{
			"benchmark_general",
			`Benchmark.benchmark(Benchmark::CAPTION, 7, Benchmark::FORMAT, "more:") { |x| t1 = x.report("a:") { }; [t1] }`,
			caption +
				"a:        0.000000   0.000000   0.000000 (  1.500000)\n" +
				"more:     0.000000   0.000000   0.000000 (  1.500000)\n",
		},
		{
			"benchmark_no_caption",
			`Benchmark.benchmark { |x| x.report("a:") { } }`,
			"a:  0.000000   0.000000   0.000000 (  1.500000)\n",
		},
		{
			"benchmark_extras_non_tms",
			`Benchmark.benchmark("", 0) { |x| x.report("a:") { }; [42] }`,
			"a:  0.000000   0.000000   0.000000 (  1.500000)\n",
		},
		{
			"benchmark_extras_nil",
			`Benchmark.benchmark("", 0) { |x| x.report("a:") { }; nil }`,
			"a:  0.000000   0.000000   0.000000 (  1.500000)\n",
		},
		{
			"bmbm",
			`Benchmark.bmbm(7) { |x| x.report("alpha:") { }; x.report("beta:") { } }`,
			"Rehearsal -------------------------------------------\n" +
				"alpha:    0.000000   0.000000   0.000000 (  1.500000)\n" +
				"beta:     0.000000   0.000000   0.000000 (  1.500000)\n" +
				"---------------------------------- total: 0.000000sec\n\n" +
				caption +
				"alpha:    0.000000   0.000000   0.000000 (  1.500000)\n" +
				"beta:     0.000000   0.000000   0.000000 (  1.500000)\n",
		},
		{
			"bmbm_return",
			`p(Benchmark.bmbm(7) { |x| x.report("a:") { } }.map { |t| t.label })`,
			"Rehearsal -------------------------------------------\n" +
				"a:        0.000000   0.000000   0.000000 (  1.500000)\n" +
				"---------------------------------- total: 0.000000sec\n\n" +
				caption +
				"a:        0.000000   0.000000   0.000000 (  1.500000)\n" +
				"[\"a:\"]\n",
		},
		{
			"report_item_alias",
			`Benchmark.bm(7) { |x| x.item("a:") { } }`,
			caption + "a:        0.000000   0.000000   0.000000 (  1.500000)\n",
		},
		{
			"job_item_alias",
			`p(Benchmark.bmbm(7) { |x| x.item("a:") { } }.size)`,
			"Rehearsal -------------------------------------------\n" +
				"a:        0.000000   0.000000   0.000000 (  1.500000)\n" +
				"---------------------------------- total: 0.000000sec\n\n" +
				caption +
				"a:        0.000000   0.000000   0.000000 (  1.500000)\n" +
				"1\n",
		},
		{
			"report_list",
			`Benchmark.bm(7) { |x| x.report("a:") { }; p x.list.size }`,
			caption + "a:        0.000000   0.000000   0.000000 (  1.500000)\n1\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := eval(t, tt.src); got != tt.want {
				t.Fatalf("src=%q\n got %q\nwant %q", tt.src, got, tt.want)
			}
		})
	}
}

// TestBenchmarkErrors covers the error branches: arithmetic and Tms.new on a
// non-numeric operand, and the no-block paths of every block-taking method.
func TestBenchmarkErrors(t *testing.T) {
	tests := []struct{ name, src, class, msgPart string }{
		{"tms_add_bad", `Benchmark::Tms.new + "x"`, "TypeError", "coerced"},
		{"tms_new_bad", `Benchmark::Tms.new("x")`, "TypeError", "Float"},
		{"measure_no_block", `Benchmark.measure`, "LocalJumpError", "no block given (measure)"},
		{"realtime_no_block", `Benchmark.realtime`, "LocalJumpError", "no block given (realtime)"},
		{"ms_no_block", `Benchmark.ms`, "LocalJumpError", "no block given (ms)"},
		{"bm_no_block", `Benchmark.bm`, "LocalJumpError", "no block given (bm)"},
		{"benchmark_no_block", `Benchmark.benchmark`, "LocalJumpError", "no block given (benchmark)"},
		{"bmbm_no_block", `Benchmark.bmbm`, "LocalJumpError", "no block given (bmbm)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			class, msg := evalErr(t, tt.src)
			if class != tt.class {
				t.Fatalf("src=%q: class=%q want %q (msg=%q)", tt.src, class, tt.class, msg)
			}
			if !containsBench(msg, tt.msgPart) {
				t.Fatalf("src=%q: msg=%q want to contain %q", tt.src, msg, tt.msgPart)
			}
		})
	}
}

// TestBenchmarkInternals covers the Go-level wrappers directly (their ToS /
// Inspect renderings, the format-arg coercion of a non-numeric, and the
// Tms#inspect path) so every line of benchmark.go is exercised.
func TestBenchmarkInternals(t *testing.T) {
	tms := &Tms{}
	want := "  0.000000   0.000000   0.000000 (  0.000000)\n"
	if got := tms.Inspect(); got != want {
		t.Fatalf("Tms.Inspect = %q", got)
	}
	if got := tms.ToS(); got != want {
		t.Fatalf("Tms.ToS = %q", got)
	}
	if !tms.Truthy() {
		t.Fatalf("Tms.Truthy = false")
	}
	br := &benchReport{}
	if br.ToS() != "#<Benchmark::Report>" || br.Inspect() != "#<Benchmark::Report>" || !br.Truthy() {
		t.Fatalf("benchReport stringers: %q %q %v", br.ToS(), br.Inspect(), br.Truthy())
	}
	bj := &benchJob{}
	if bj.ToS() != "#<Benchmark::Job>" || bj.Inspect() != "#<Benchmark::Job>" || !bj.Truthy() {
		t.Fatalf("benchJob stringers: %q %q %v", bj.ToS(), bj.Inspect(), bj.Truthy())
	}
	// benchFormatArgs maps a non-numeric to its ToS() and a numeric to float64.
	got := benchFormatArgs([]object.Value{object.Wrap(object.NewString("hi")), object.IntValue(int64(object.Integer(3)))})
	if len(got) != 2 || got[0] != "hi" || got[1] != 3.0 {
		t.Fatalf("benchFormatArgs = %#v", got)
	}
	// benchLjust returns s unchanged when already at/over width.
	if benchLjust("toolong", 3) != "toolong" {
		t.Fatalf("benchLjust over-width")
	}
	// labelToS coerces a non-string/non-nil via to_s and nil to "".
	if labelToS(object.IntValue(int64(object.Integer(7)))) != "7" || labelToS(object.NilVal()) != "" {
		t.Fatalf("labelToS coercion")
	}

	// The default benchmarkMonotonic reads the real process clock (the same
	// CLOCK_MONOTONIC source Process uses); it returns a non-negative, monotonic
	// reading. Exercise it directly (the report tests override it for
	// determinism, so this is the only path covering the real-clock body).
	if a := benchmarkMonotonic(); a < 0 {
		t.Fatalf("benchmarkMonotonic = %v, want >= 0", a)
	}

	// benchPrint falls back to curStdout() (writing to vm.out) when $stdout has
	// been cleared from the globals, rather than panicking on a nil send target.
	var buf bytes.Buffer
	vm := New(&buf)
	delete(vm.globals, "$stdout")
	vm.benchPrint("fallback")
	if buf.String() != "fallback" {
		t.Fatalf("benchPrint fallback wrote %q", buf.String())
	}
}

// containsBench is a tiny substring check, kept local so the test file has no
// extra imports.
func containsBench(s, sub string) bool {
	if sub == "" {
		return true
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
