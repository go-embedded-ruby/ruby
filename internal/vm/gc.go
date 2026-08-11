// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"fmt"
	"runtime"
	"runtime/debug"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// gcStatEntry names one MRI GC.stat key and derives its value from a Go
// runtime.MemStats snapshot. rbgo runs on Go's garbage collector and keeps no
// MRI-style slot heap, so only the counters that have a faithful Go analogue
// (collection count, allocated/freed object totals, live-slot delta, total
// pause time) carry real numbers; the slot/page and generational metrics that
// are meaningless here report 0 rather than a fabricated figure. Every value is
// an Integer, matching MRI's size_t-typed rb_gc_stat(), and the shape (all the
// guaranteed keys, :count == GC.count) is what the specs assert.
type gcStatEntry struct {
	key string
	val func(m *runtime.MemStats) int64
}

// gcStatEntries is the ordered MRI 3.4/4.0 GC.stat key set. The order is
// preserved into the returned Hash (insertion-ordered), matching MRI.
var gcStatEntries = []gcStatEntry{
	{"count", func(m *runtime.MemStats) int64 { return int64(m.NumGC) }},
	{"time", func(m *runtime.MemStats) int64 { return int64(m.PauseTotalNs / 1e6) }},
	{"marking_time", func(*runtime.MemStats) int64 { return 0 }},
	{"sweeping_time", func(*runtime.MemStats) int64 { return 0 }},
	{"heap_allocated_pages", func(*runtime.MemStats) int64 { return 0 }},
	{"heap_empty_pages", func(*runtime.MemStats) int64 { return 0 }},
	{"heap_allocatable_slots", func(*runtime.MemStats) int64 { return 0 }},
	{"heap_available_slots", func(m *runtime.MemStats) int64 { return int64(m.HeapObjects) }},
	{"heap_live_slots", func(m *runtime.MemStats) int64 { return int64(m.Mallocs - m.Frees) }},
	{"heap_free_slots", func(*runtime.MemStats) int64 { return 0 }},
	{"heap_final_slots", func(*runtime.MemStats) int64 { return 0 }},
	{"heap_marked_slots", func(*runtime.MemStats) int64 { return 0 }},
	{"heap_eden_pages", func(*runtime.MemStats) int64 { return 0 }},
	{"total_allocated_pages", func(*runtime.MemStats) int64 { return 0 }},
	{"total_freed_pages", func(*runtime.MemStats) int64 { return 0 }},
	{"total_allocated_objects", func(m *runtime.MemStats) int64 { return int64(m.Mallocs) }},
	{"total_freed_objects", func(m *runtime.MemStats) int64 { return int64(m.Frees) }},
	{"malloc_increase_bytes", func(*runtime.MemStats) int64 { return 0 }},
	{"malloc_increase_bytes_limit", func(*runtime.MemStats) int64 { return 0 }},
	{"minor_gc_count", func(*runtime.MemStats) int64 { return 0 }},
	{"major_gc_count", func(m *runtime.MemStats) int64 { return int64(m.NumGC) }},
	{"compact_count", func(*runtime.MemStats) int64 { return 0 }},
	{"read_barrier_faults", func(*runtime.MemStats) int64 { return 0 }},
	{"total_moved_objects", func(*runtime.MemStats) int64 { return 0 }},
	{"remembered_wb_unprotected_objects", func(*runtime.MemStats) int64 { return 0 }},
	{"remembered_wb_unprotected_objects_limit", func(*runtime.MemStats) int64 { return 0 }},
	{"old_objects", func(*runtime.MemStats) int64 { return 0 }},
	{"old_objects_limit", func(*runtime.MemStats) int64 { return 0 }},
	{"oldmalloc_increase_bytes", func(*runtime.MemStats) int64 { return 0 }},
	{"oldmalloc_increase_bytes_limit", func(*runtime.MemStats) int64 { return 0 }},
}

// gcStress reports the current GC.stress value, treating the nil zero value as
// MRI's default of false.
func (vm *VM) gcStressValue() object.Value {
	if vm.gcStress == nil {
		return object.False
	}
	return vm.gcStress
}

// registerGC installs the GC module and its nested GC::Profiler. rbgo has no
// heap of its own to sweep, so GC is an observable-contract shim over Go's
// collector: GC.start forces a Go collection (so GC.count and GC.total_time
// advance), enable/disable flip Go's GC percent and return the previous
// disabled state, and stress/measure_total_time/auto_compact round-trip a flag.
// The metrics that are meaningless on Go's GC report a spec-satisfying 0/empty
// value rather than a fabricated one; every method matches MRI's arity, return
// type and error class.
func (vm *VM) registerGC() {
	vm.gcMeasureTotalTime = true // MRI's default

	mod := newClass("GC", nil)
	mod.isModule = true
	vm.consts["GC"] = mod
	def := func(name string, fn NativeFn) { mod.smethods[name] = &Method{name: name, owner: mod, native: fn} }

	// start / garbage_collect: force a Go collection and return nil, accepting
	// and ignoring MRI's full_mark:/immediate_sweep: keyword arguments (any
	// trailing Hash) and a block. Running Go's GC advances runtime NumGC and
	// PauseTotalNs so GC.count and GC.total_time observably increase, as MRI's do.
	start := func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		runtime.GC()
		return object.NilV
	}
	def("start", start)
	def("garbage_collect", start)
	// garbage_collect is also a private instance method (module_function), so a
	// receiver that includes/extends GC answers #garbage_collect, matching MRI.
	mod.define("garbage_collect", start)

	// enable: re-enable Go's collector if this shim had disabled it and return the
	// previous disabled state (true iff GC was disabled), matching MRI.
	def("enable", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		prev := vm.gcDisabled
		if prev {
			debug.SetGCPercent(vm.gcSavedGCPercent)
			vm.gcDisabled = false
		}
		return object.Bool(prev)
	})
	// disable: turn Go's collector off (saving the prior percent to restore on
	// enable) and return the previous disabled state, matching MRI.
	def("disable", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		prev := vm.gcDisabled
		if !prev {
			vm.gcSavedGCPercent = debug.SetGCPercent(-1)
			vm.gcDisabled = true
		}
		return object.Bool(prev)
	})

	// count: the number of Go collections so far, a monotonic Integer (MRI's
	// GC.count).
	def("count", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		return object.IntValue(int64(m.NumGC))
	})

	// stat([sym | hash]): with no argument returns a Hash of every guaranteed key
	// (Integer values from the Go runtime); with a Symbol returns that one value
	// (unknown key -> ArgumentError); with a Hash fills and returns that Hash
	// (preserving any unrelated keys it already holds). A non-nil, non-symbol,
	// non-hash argument raises TypeError, matching MRI's rb_gc_stat.
	def("stat", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		if len(args) >= 1 {
			switch a := args[0].(type) {
			case object.Symbol:
				for _, e := range gcStatEntries {
					if e.key == string(a) {
						return object.IntValue(e.val(&m))
					}
				}
				return raise("ArgumentError", "unknown key: %s", string(a))
			case *object.Hash:
				for _, e := range gcStatEntries {
					a.Set(object.Symbol(e.key), object.IntValue(e.val(&m)))
				}
				return a
			case object.Nil:
				// nil is treated as "no argument": fall through to the full Hash.
			default:
				return raise("TypeError", "non-hash or symbol given")
			}
		}
		h := object.NewHash()
		for _, e := range gcStatEntries {
			h.Set(object.Symbol(e.key), object.IntValue(e.val(&m)))
		}
		return h
	})

	// latest_gc_info([sym | hash]): a best-effort Hash describing the most recent
	// collection. The trigger/marking details are not observable on Go's GC, so
	// the fields report neutral values (no major mark, swept immediately, quiesced
	// state). A Symbol selects one field; a Hash is filled and returned.
	def("latest_gc_info", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		build := func(h *object.Hash) *object.Hash {
			h.Set(object.Symbol("major_by"), object.NilV)
			h.Set(object.Symbol("need_major_by"), object.NilV)
			h.Set(object.Symbol("gc_by"), object.Symbol("method"))
			h.Set(object.Symbol("have_finalizer"), object.False)
			h.Set(object.Symbol("immediate_sweep"), object.True)
			h.Set(object.Symbol("state"), object.Symbol("none"))
			return h
		}
		if len(args) >= 1 {
			switch a := args[0].(type) {
			case object.Symbol:
				h := build(object.NewHash())
				if v, ok := h.Get(a); ok {
					return v
				}
				return object.NilV
			case *object.Hash:
				return build(a)
			}
		}
		return build(object.NewHash())
	})

	// total_time: the cumulative Go GC pause time in nanoseconds, a monotonic
	// Integer (MRI's GC.total_time).
	def("total_time", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		return object.IntValue(int64(m.PauseTotalNs))
	})

	// measure_total_time / measure_total_time=: round-trip the boolean flag. rbgo
	// always has Go's pause counters, so the flag is purely observable; it defaults
	// to true as MRI does.
	def("measure_total_time", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(vm.gcMeasureTotalTime)
	})
	def("measure_total_time=", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		vm.gcMeasureTotalTime = len(args) >= 1 && args[0].Truthy()
		if len(args) >= 1 {
			return args[0]
		}
		return object.NilV
	})

	// stress / stress=: round-trip the stress value (a bool or Integer flag set).
	// Go's GC has no equivalent stress mode, so the value is observable only; it
	// defaults to false.
	def("stress", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.gcStressValue()
	})
	def("stress=", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) >= 1 {
			vm.gcStress = args[0]
			return args[0]
		}
		vm.gcStress = object.False
		return object.NilV
	})

	// auto_compact / auto_compact=: round-trip the boolean flag. rbgo never moves
	// objects, so compaction is a no-op and the flag is observable only.
	def("auto_compact", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(vm.gcAutoCompact)
	})
	def("auto_compact=", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		vm.gcAutoCompact = len(args) >= 1 && args[0].Truthy()
		if len(args) >= 1 {
			return args[0]
		}
		return object.NilV
	})

	// compact: MRI returns a Hash of move statistics. rbgo never relocates
	// objects, so it runs a Go collection and reports the MRI shape with empty
	// per-type tallies (nothing considered, nothing moved).
	def("compact", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		runtime.GC()
		return gcCompactResult()
	})
	// verify_compaction_references: the debug variant of compact; accepts (and
	// ignores) its keyword arguments and returns the same move-stats shape.
	def("verify_compaction_references", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return gcCompactResult()
	})

	// config([nil | hash]): MRI 3.4+ get/set of the active GC implementation's
	// tunables. rbgo runs on Go's GC and exposes no tunable settings, so the
	// configuration is just the read-only :implementation name: with no argument
	// (or nil) it returns {implementation: "rbgo"}; with a Hash it applies the
	// (empty) set of writable settings — ignoring unknown keys — and returns the
	// settings without the global :implementation key, but raises ArgumentError if
	// asked to write the read-only :implementation. Any other argument type raises
	// ArgumentError, matching MRI.
	def("config", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		fullConfig := func() *object.Hash {
			h := object.NewHash()
			h.Set(object.Symbol("implementation"), object.NewString("rbgo"))
			return h
		}
		if len(args) == 0 {
			return fullConfig()
		}
		switch a := args[0].(type) {
		case object.Nil:
			return fullConfig()
		case *object.Hash:
			if _, ok := a.Get(object.Symbol("implementation")); ok {
				return raise("ArgumentError", `Attempting to set read-only key "Implementation"`)
			}
			if _, ok := a.Get(object.NewString("implementation")); ok {
				return raise("ArgumentError", `Attempting to set read-only key "Implementation"`)
			}
			// No writable settings exist on Go's GC; unknown keys are ignored. The
			// return value mirrors the settings without the global :implementation key.
			return object.NewHash()
		default:
			return raise("ArgumentError", "expected keyword arguments, got %s", vm.classOf(args[0]).name)
		}
	})

	// INTERNAL_CONSTANTS / OPTS: MRI exposes build-time tuning constants. rbgo has
	// no such heap parameters, so these are the correctly-typed empty containers
	// (Hash and Array).
	mod.consts["INTERNAL_CONSTANTS"] = object.NewHash()
	vm.consts["GC::INTERNAL_CONSTANTS"] = mod.consts["INTERNAL_CONSTANTS"]
	mod.consts["OPTS"] = object.NewArray()
	vm.consts["GC::OPTS"] = mod.consts["OPTS"]

	vm.registerGCProfiler(mod)
}

// gcCompactResult builds MRI's GC.compact / verify_compaction_references return
// shape: {considered:{}, moved:{}, moved_up:{}, moved_down:{}}. rbgo moves no
// objects, so every per-type tally is empty.
func gcCompactResult() *object.Hash {
	h := object.NewHash()
	h.Set(object.Symbol("considered"), object.NewHash())
	h.Set(object.Symbol("moved"), object.NewHash())
	h.Set(object.Symbol("moved_up"), object.NewHash())
	h.Set(object.Symbol("moved_down"), object.NewHash())
	return h
}

// registerGCProfiler installs GC::Profiler under the GC module. The profiler is
// an observable toggle over Go's GC: enabling it records nothing extra (Go
// already tracks pause time), so total_time reports Go's cumulative pause time
// in seconds while enabled and the report/result renderers emit a stable header.
func (vm *VM) registerGCProfiler(gc *RClass) {
	prof := newClass("Profiler", nil)
	prof.isModule = true
	gc.consts["Profiler"] = prof
	vm.consts["GC::Profiler"] = prof
	def := func(name string, fn NativeFn) { prof.smethods[name] = &Method{name: name, owner: prof, native: fn} }

	def("enable", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		vm.gcProfilerEnabled = true
		return object.NilV
	})
	def("disable", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		vm.gcProfilerEnabled = false
		return object.NilV
	})
	def("enabled?", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(vm.gcProfilerEnabled)
	})
	// clear: drop any accumulated profiling data. On Go's GC there is no rbgo-side
	// record beyond the pause counter, so this resets the seconds accumulator.
	def("clear", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		vm.gcProfilerTotalTime = 0
		return object.NilV
	})
	// total_time: the cumulative Go GC pause time in seconds, a Float (MRI's
	// GC::Profiler.total_time).
	def("total_time", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		return object.Float(float64(m.PauseTotalNs) / 1e9)
	})
	// raw_data: MRI returns an Array of per-collection Hashes when profiling
	// produced data, otherwise nil. rbgo records no per-collection rows, so it
	// reports nil while disabled and an empty Array while enabled.
	def("raw_data", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		if !vm.gcProfilerEnabled {
			return object.NilV
		}
		return object.NewArray()
	})
	// report([io]): write the profiling report to io (default $stdout) and return
	// nil, matching MRI's GC::Profiler.report.
	def("report", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		out := gcProfilerReport(&m)
		target := vm.stdoutValue()
		if len(args) >= 1 {
			target = args[0]
		}
		vm.send(target, "write", []object.Value{object.NewString(out)}, nil)
		return object.NilV
	})
	// result: return the profiling report as a String (MRI's
	// GC::Profiler.result).
	def("result", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		return object.NewString(gcProfilerReport(&m))
	})
}

// gcProfilerReport renders MRI's GC::Profiler report header from a MemStats
// snapshot. rbgo keeps no per-collection rows, so it reports the invocation
// count and the column header without data lines.
func gcProfilerReport(m *runtime.MemStats) string {
	return fmt.Sprintf("GC %d invokes.\n"+
		"Index    Invoke Time(sec)       Use Size(byte)     Total Size(byte)         Total Object                    GC Time(ms)\n",
		m.NumGC)
}
