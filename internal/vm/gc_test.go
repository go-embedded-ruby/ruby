// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import "testing"

// TestGCModule covers the GC module observable contract: start/enable/disable,
// count, stat (shape, single key, hash-fill, error branches), latest_gc_info,
// total_time, measure_total_time, stress, auto_compact, compact and config.
// rbgo runs on Go's garbage collector, so these assert MRI's return types,
// transitions and error classes rather than reclamation. Asserted against MRI
// Ruby 4.0.5.
func TestGCModule(t *testing.T) {
	cases := []struct{ src, want string }{
		// start / garbage_collect always return nil; kwargs and block ignored.
		{`p GC.start`, "nil\n"},
		{`p GC.start(full_mark: true, immediate_sweep: true)`, "nil\n"},
		{`p GC.garbage_collect`, "nil\n"},
		{`p(GC.start { })`, "nil\n"},
		// garbage_collect is a module_function: an object that extends GC answers it.
		{`o = Object.new; o.extend(GC); p o.garbage_collect`, "nil\n"},

		// enable/disable return the previous *disabled* state, exercising all four
		// transitions (start enabled): enable->false, disable->false, disable->true,
		// enable->true, enable->false. Ends enabled.
		{`p [GC.enable, GC.disable, GC.disable, GC.enable, GC.enable]`, "[false, false, true, true, false]\n"},

		// count is a monotonic Integer that advances across collections.
		{`p GC.count.is_a?(Integer)`, "true\n"},
		{`c = GC.count; GC.start; p GC.count >= c`, "true\n"},

		// stat: Hash of Integer values including :count; single-key form; hash-fill
		// preserving unrelated keys; nil behaves as no argument.
		{`s = GC.stat; p [s.is_a?(Hash), s.keys.include?(:count)]`, "[true, true]\n"},
		{`p GC.stat.values.all? { |v| v.is_a?(Integer) }`, "true\n"},
		{`p GC.stat(:count).is_a?(Integer)`, "true\n"},
		{`p GC.stat(:heap_free_slots).is_a?(Integer)`, "true\n"},
		{`p GC.stat(:total_allocated_objects).is_a?(Integer)`, "true\n"},
		{`p (GC.stat[:count] == GC.count)`, "true\n"},
		{`h = {count: "hello", __other__: "world"}; r = GC.stat(h); p [r.equal?(h), r[:count].is_a?(Integer), r[:__other__]]`, `[true, true, "world"]` + "\n"},
		{`p GC.stat(nil).is_a?(Hash)`, "true\n"},
		{`c = GC.stat(:major_gc_count); GC.start; p GC.stat(:major_gc_count) >= c`, "true\n"},
		// stat error branches: unknown key -> ArgumentError; non-hash/symbol -> TypeError.
		{`begin; GC.stat(:foo); rescue => e; p [e.class, e.message]; end`, `[ArgumentError, "unknown key: foo"]` + "\n"},
		{`begin; GC.stat(7); rescue => e; p [e.class, e.message]; end`, `[TypeError, "non-hash or symbol given"]` + "\n"},

		// latest_gc_info: Hash, single-key value, unknown key -> nil, hash-fill.
		{`p GC.latest_gc_info.is_a?(Hash)`, "true\n"},
		{`p GC.latest_gc_info(:state)`, ":none\n"},
		{`p GC.latest_gc_info(:nonexistent)`, "nil\n"},
		{`h = {}; r = GC.latest_gc_info(h); p [r.equal?(h), r.key?(:state)]`, "[true, true]\n"},

		// total_time is a monotonic Integer.
		{`p GC.total_time.is_a?(Integer)`, "true\n"},
		{`t = GC.total_time; GC.start; p GC.total_time >= t`, "true\n"},

		// measure_total_time defaults to true and round-trips a boolean; the
		// argumentless writer form resets it to false.
		{`p GC.measure_total_time`, "true\n"},
		{`GC.measure_total_time = false; p GC.measure_total_time`, "false\n"},
		{`GC.measure_total_time = true; p GC.measure_total_time`, "true\n"},
		{`p GC.send(:measure_total_time=)`, "nil\n"},
		{`GC.send(:measure_total_time=); p GC.measure_total_time`, "false\n"},

		// stress defaults to false and round-trips; argumentless writer resets it.
		{`p GC.stress`, "false\n"},
		{`GC.stress = true; p GC.stress`, "true\n"},
		{`GC.stress = 3; p GC.stress`, "3\n"},
		{`p GC.send(:stress=)`, "nil\n"},
		{`GC.stress = true; GC.send(:stress=); p GC.stress`, "false\n"},

		// auto_compact defaults to false and round-trips; argumentless writer resets.
		{`p GC.auto_compact`, "false\n"},
		{`GC.auto_compact = true; p GC.auto_compact`, "true\n"},
		{`GC.auto_compact = false; p GC.auto_compact`, "false\n"},
		{`p GC.send(:auto_compact=)`, "nil\n"},
		{`GC.auto_compact = true; GC.send(:auto_compact=); p GC.auto_compact`, "false\n"},

		// compact / verify_compaction_references report MRI's move-stats shape.
		{`r = GC.compact; p [r.class, r.keys]`, "[Hash, [:considered, :moved, :moved_up, :moved_down]]\n"},
		{`p GC.compact[:moved]`, "{}\n"},
		{`r = GC.verify_compaction_references; p [r.class, r.keys.sort]`, "[Hash, [:considered, :moved, :moved_down, :moved_up]]\n"},

		// Constants.
		{`p GC::INTERNAL_CONSTANTS.is_a?(Hash)`, "true\n"},
		{`p GC::OPTS.is_a?(Array)`, "true\n"},

		// config: no-arg and nil return {implementation: "rbgo"}; an options Hash
		// returns the (empty) writable settings and ignores unknown keys; writing the
		// read-only :implementation (Symbol or String) raises; a non-hash raises.
		{`c = GC.config; p [c.is_a?(Hash), c[:implementation]]`, `[true, "rbgo"]` + "\n"},
		{`p GC.config(nil) == GC.config`, "true\n"},
		{`p GC.config({})`, "{}\n"},
		{`p (GC.config(foo: "bar") == {})`, "true\n"},
		{`begin; GC.config(implementation: "x"); rescue => e; p [e.class, e.message]; end`, `[ArgumentError, "Attempting to set read-only key \"Implementation\""]` + "\n"},
		{`begin; GC.config("implementation" => "x"); rescue => e; p e.class; end`, "ArgumentError\n"},
		{`begin; GC.config([]); rescue => e; p e.class; end`, "ArgumentError\n"},
		{`begin; GC.config(1); rescue => e; p e.class; end`, "ArgumentError\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestGCProfiler covers GC::Profiler: enable/disable/enabled?, clear, total_time
// (Float), raw_data (nil when disabled, Array when enabled), report (to an IO
// and to $stdout) and result (String). Asserted against MRI Ruby 4.0.5.
func TestGCProfiler(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p GC::Profiler.enabled?`, "false\n"},
		{`p GC::Profiler.enable`, "nil\n"},
		{`GC::Profiler.enable; p GC::Profiler.enabled?`, "true\n"},
		{`GC::Profiler.enable; p GC::Profiler.disable; p GC::Profiler.enabled?`, "nil\nfalse\n"},
		{`p GC::Profiler.clear`, "nil\n"},
		{`p GC::Profiler.total_time.is_a?(Float)`, "true\n"},
		{`p GC::Profiler.result.is_a?(String)`, "true\n"},
		{`p GC::Profiler.result.include?("invokes")`, "true\n"},
		// raw_data: nil while disabled, an Array while enabled.
		{`GC::Profiler.disable; p GC::Profiler.raw_data`, "nil\n"},
		{`GC::Profiler.enable; p GC::Profiler.raw_data.class`, "Array\n"},
		// report(io) writes to the IO and returns nil.
		{`require "stringio"; io = StringIO.new; r = GC::Profiler.report(io); p [r, io.string.include?("invokes")]`, "[nil, true]\n"},
		// report with no argument writes to $stdout.
		{`require "stringio"; $stdout = StringIO.new; GC::Profiler.report; out = $stdout.string; $stdout = STDOUT; p out.include?("invokes")`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}
