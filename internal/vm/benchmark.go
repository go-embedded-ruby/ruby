// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	benchmark "github.com/go-ruby-benchmark/benchmark"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file binds github.com/go-ruby-benchmark/benchmark — the pure-Go,
// MRI-4.0.5-faithful port of Ruby's `benchmark` stdlib — into rbgo. The library
// owns every interpreter-independent piece: the Tms measurement value (its
// fields, total, memberwise arithmetic and the %u/%y/%t/%r/%n/%U/%Y formatting),
// the report layout (CAPTION, label justification, the bm/bmbm tables) and the
// MeasureWith/RealtimeWith timing skeleton. The only impure ingredient, the
// clock, is injected: the library's Clock seam wants Process.times (the four CPU
// times) and a monotonic real clock, which rbgo supplies through benchmarkClock.
//
// This replaces the former pure-Ruby Benchmark module in internal/vm/prelude.rb
// (Tms, realtime, measure, bm, bmbm and the Report helper). That prelude code
// re-implemented the same arithmetic and formatting by hand, hardcoded a CAPTION
// that was a column too wide ("       user" — seven leading spaces vs MRI's six)
// and did not provide Benchmark.benchmark, the %-extension Tms#format directives
// or the bmbm rehearsal banner. Backing it with the library fixes the caption,
// adds the missing surface and makes the rendering byte-for-byte identical to
// `ruby -rbenchmark`.
//
// rbgo, like the prelude before it, exposes no per-process CPU accounting, so
// benchmarkClock.Times() reports zeros (utime/stime/cutime/cstime == 0.0) and
// the real (wall-clock) field carries the measurement — exactly the behaviour
// the prelude had, now routed through the library's Clock.

// Tms is rbgo's Ruby wrapper around a benchmark.Tms value. The same wrapper
// backs Benchmark::Tms instances; classOf reports cBenchmarkTms for it. The
// library owns the value semantics (arithmetic, total, formatting); this wrapper
// only maps them onto rbgo's object model.
type Tms struct{ t benchmark.Tms }

func (t *Tms) ToS() string     { return t.t.ToS() }
func (t *Tms) Inspect() string { return t.t.ToS() }
func (t *Tms) Truthy() bool    { return true }

// benchReport is the object yielded to a bm/benchmark block. It wraps the
// library's *benchmark.Report (which times each item with the injected clock and
// renders each row) and prints every line through the live $stdout as MRI does
// (so a reassigned $stdout — e.g. a StringIO — captures the table). It mirrors
// Benchmark::Report.
type benchReport struct {
	r  *benchmark.Report
	vm *VM
}

func (b *benchReport) ToS() string     { return "#<Benchmark::Report>" }
func (b *benchReport) Inspect() string { return b.ToS() }
func (b *benchReport) Truthy() bool    { return true }

// benchJob is the object yielded to a bmbm block. It collects (label, block)
// pairs into the library's *benchmark.Job; Benchmark.bmbm then renders the
// rehearsal-and-take report from them. It mirrors Benchmark::Job.
type benchJob struct {
	job    *benchmark.Job
	procs  []*Proc
	labels []string
}

func (b *benchJob) ToS() string     { return "#<Benchmark::Job>" }
func (b *benchJob) Inspect() string { return b.ToS() }
func (b *benchJob) Truthy() bool    { return true }

// benchmarkMonotonic is the seam supplying the library Clock's monotonic real
// clock. It defaults to the same source Process.clock_gettime(CLOCK_MONOTONIC)
// uses (Go's internally-monotonic time anchored at processClockEpoch), so
// Benchmark and Process agree; tests override it to script deterministic output.
var benchmarkMonotonic = func() float64 {
	return processMonoNow().Sub(processClockEpoch).Seconds()
}

// benchmarkClock is the rbgo Clock injected into the library. Times() reports
// zeros because this runtime exposes no per-process CPU accounting (matching the
// old prelude); Monotonic() reads benchmarkMonotonic.
type benchmarkClock struct{}

func (benchmarkClock) Times() (utime, stime, cutime, cstime float64) { return 0, 0, 0, 0 }
func (benchmarkClock) Monotonic() float64                            { return benchmarkMonotonic() }

// registerBenchmark installs the Benchmark module, its Tms / Report / Job
// classes and the module functions (measure, realtime, ms, bm, bmbm,
// benchmark). It is called eagerly at boot (like the prelude defined Benchmark
// unconditionally), so `require "benchmark"` — which already returns true — finds
// the module ready.
func (vm *VM) registerBenchmark() {
	mod := newClass("Benchmark", nil)
	mod.isModule = true
	vm.consts["Benchmark"] = object.Wrap(mod)
	mod.consts["CAPTION"] = object.Wrap(object.NewString(benchmark.CAPTION))
	mod.consts["FORMAT"] = object.Wrap(object.NewString(benchmark.FORMAT))
	mod.consts["BENCHMARK_VERSION"] = object.Wrap(object.NewString(benchmark.BenchmarkVersion))

	vm.registerBenchmarkTms(mod)
	vm.registerBenchmarkReport(mod)
	vm.registerBenchmarkJob(mod)

	def := func(name string, fn NativeFn) { mod.smethods[name] = &Method{name: name, owner: mod, native: fn} }

	// Benchmark.measure(label = "") { ... } -> Tms. Times the block with the
	// injected clock and wraps the resulting benchmark.Tms.
	def("measure", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		label := benchLabel(args)
		t := benchmark.MeasureWith(benchmarkClock{}, label, vm.benchRun(blk, "measure"))
		return object.Wrap(&Tms{t: t})
	})

	// Benchmark.realtime { ... } -> Float (elapsed real seconds).
	def("realtime", func(vm *VM, _ object.Value, _ []object.Value, blk *Proc) object.Value {
		return object.FloatValue(float64(object.Float(benchmark.RealtimeWith(benchmarkClock{}, vm.benchRun(blk, "realtime")))))
	})

	// Benchmark.ms { ... } -> Float (elapsed real milliseconds).
	def("ms", func(vm *VM, _ object.Value, _ []object.Value, blk *Proc) object.Value {
		return object.FloatValue(float64(object.Float(benchmark.MsWith(benchmarkClock{}, vm.benchRun(blk, "ms")))))
	})

	// Benchmark.bm(label_width = 0, *labels) { |x| ... } -> [Tms]. Prints CAPTION
	// then a timed line per report item, then a summary line per extra Tms the
	// block returns (labelled by *labels).
	def("bm", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		width := benchWidth(args)
		labels := benchLabels(args)
		return vm.benchDriver(blk, benchmark.CAPTION, width, "", labels, "bm")
	})

	// Benchmark.benchmark(caption = "", label_width = 0, format = nil, *labels)
	// { |x| ... } -> [Tms]. The general driver behind bm.
	def("benchmark", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		caption := ""
		if len(args) > 0 {
			caption = labelToS(args[0])
		}
		width := 0
		if len(args) > 1 {
			width = int(intArg(args[1]))
		}
		format := ""
		if len(args) > 2 {
			if _, ok := object.AsNilOK(args[2]); !ok {
				format = strArg(args[2])
			}
		}
		var labels []string
		if len(args) > 3 {
			labels = benchStrings(args[3:])
		}
		return vm.benchDriver(blk, caption, width, format, labels, "benchmark")
	})

	// Benchmark.bmbm(width = 0) { |x| ... } -> [Tms]. Runs each registered job
	// twice (rehearsal then take) and returns the take measurements.
	def("bmbm", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		width := benchWidth(args)
		return vm.benchBmbm(blk, width)
	})
}

// registerBenchmarkTms installs Benchmark::Tms: its constructor, the six time
// readers, total/label, the memberwise arithmetic, to_a/to_s and the
// %-extension format. Every value operation is delegated to the library.
func (vm *VM) registerBenchmarkTms(mod *RClass) {
	cls := newClass("Benchmark::Tms", vm.cObject)
	vm.cBenchmarkTms = cls
	mod.consts["Tms"] = object.Wrap(cls)
	vm.consts["Benchmark::Tms"] = object.Wrap(cls)

	cls.smethods["new"] = &Method{name: "new", owner: cls, native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		// Tms.new(utime = 0, stime = 0, cutime = 0, cstime = 0, real = 0, label = nil)
		f := func(i int) float64 {
			if i < len(args) {
				return benchFloat(args[i])
			}
			return 0.0
		}
		label := ""
		if len(args) > 5 {
			label = labelToS(args[5])
		}
		return object.Wrap(&Tms{t: benchmark.NewTms(f(0), f(1), f(2), f(3), f(4), label)})
	}}

	reader := func(name string, get func(benchmark.Tms) float64) {
		cls.define(name, func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
			return object.FloatValue(float64(object.Float(get(object.Kind[*Tms](self).t))))
		})
	}
	reader("utime", benchmark.Tms.Utime)
	reader("stime", benchmark.Tms.Stime)
	reader("cutime", benchmark.Tms.Cutime)
	reader("cstime", benchmark.Tms.Cstime)
	reader("real", benchmark.Tms.Real)
	reader("total", benchmark.Tms.Total)

	cls.define("label", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(object.Kind[*Tms](self).t.Label()))
	})

	// Arithmetic: each accepts another Tms (memberwise) or a numeric scalar
	// (applied to all fields), mirroring Tms#+ / #- / #* / #/.
	arith := func(name string, tms func(benchmark.Tms, benchmark.Tms) benchmark.Tms, scalar func(benchmark.Tms, float64) benchmark.Tms) {
		cls.define(name, func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
			t := object.Kind[*Tms](self).t
			if other, ok := object.KindOK[*Tms](args[0]); ok {
				return object.Wrap(&Tms{t: tms(t, other.t)})
			}
			if x, ok := toFloat(args[0]); ok {
				return object.Wrap(&Tms{t: scalar(t, x)})
			}
			raise("TypeError", "Benchmark::Tms can't be coerced into %s", classNameOf(args[0]))
			return object.NilVal()
		})
	}
	arith("+", benchmark.Tms.Add, benchmark.Tms.AddScalar)
	arith("-", benchmark.Tms.Sub, benchmark.Tms.SubScalar)
	arith("*", benchmark.Tms.Mul, benchmark.Tms.MulScalar)
	arith("/", benchmark.Tms.Div, benchmark.Tms.DivScalar)

	cls.define("to_a", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return benchToA(object.Kind[*Tms](self).t)
	})
	cls.define("to_s", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(object.Kind[*Tms](self).t.ToS()))
	})

	// format(fmt = nil, *args): a nil/absent format selects FORMAT and ignores
	// args; an explicit format is rendered with the %-extension directives and
	// the trailing args, exactly as Tms#format does.
	cls.define("format", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		fmt := ""
		var rest []object.Value
		if len(args) > 0 {
			if _, ok := object.AsNilOK(args[0]); !ok {
				fmt = strArg(args[0])
			}
			rest = args[1:]
		}
		return object.Wrap(object.NewString(object.Kind[*Tms](self).t.Format(fmt, benchFormatArgs(rest)...)))
	})
}

// registerBenchmarkReport installs Benchmark::Report and its report/item
// methods, which time a labelled block and print its line through $stdout.
func (vm *VM) registerBenchmarkReport(mod *RClass) {
	cls := newClass("Benchmark::Report", vm.cObject)
	vm.cBenchmarkReport = cls
	mod.consts["Report"] = object.Wrap(cls)
	vm.consts["Benchmark::Report"] = object.Wrap(cls)

	run := func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		br := object.Kind[*benchReport](self)
		label := benchLabel(args)
		t := br.r.Run(label, vm.benchRun(blk, "report"))
		vm.benchPrint(br.r.Line(t))
		return object.Wrap(&Tms{t: t})
	}
	cls.define("report", run)
	cls.define("item", run)

	cls.define("list", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return benchTmsList(object.Kind[*benchReport](self).r.List())
	})
}

// registerBenchmarkJob installs Benchmark::Job and its report/item methods,
// which register a labelled block for bmbm's two-pass run.
func (vm *VM) registerBenchmarkJob(mod *RClass) {
	cls := newClass("Benchmark::Job", vm.cObject)
	vm.cBenchmarkJob = cls
	mod.consts["Job"] = object.Wrap(cls)
	vm.consts["Benchmark::Job"] = object.Wrap(cls)

	add := func(_ *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		bj := object.Kind[*benchJob](self)
		label := benchLabel(args)
		bj.job.Item(label, func() {})
		bj.procs = append(bj.procs, blk)
		bj.labels = append(bj.labels, label)
		return self
	}
	cls.define("report", add)
	cls.define("item", add)
}

// benchDriver runs a bm/benchmark report: it prints the caption, yields a
// benchReport whose report items print as they run, then prints a summary line
// per extra Tms the block returns. It mirrors Benchmark.benchmark, returning the
// collected per-item measurements.
func (vm *VM) benchDriver(blk *Proc, caption string, labelWidth int, format string, labels []string, name string) object.Value {
	if blk == nil {
		raise("LocalJumpError", "no block given (%s)", name)
	}
	width := labelWidth + 1
	r := benchmark.NewReport(benchmarkClock{}, width, format)
	br := &benchReport{r: r, vm: vm}
	if caption != "" {
		vm.benchPrint(br.r.Caption())
	}
	extras := vm.callBlock(blk, []object.Value{object.Wrap(br)})
	for i, t := range benchExtras(extras) {
		label := ""
		if i < len(labels) {
			label = labels[i]
		}
		vm.benchPrint(r.ExtraLine(width, label, t))
	}
	return benchTmsList(r.List())
}

// benchBmbm runs a bmbm report: it yields a benchJob to collect the blocks, then
// renders the rehearsal banner + lines, the rehearsal total footer, the take
// caption + lines, printing each piece through $stdout as it is produced and
// returning the take measurements. The rendering and timing are the library's;
// this reproduces Benchmark.bmbm's incremental printing over them.
func (vm *VM) benchBmbm(blk *Proc, width int) object.Value {
	if blk == nil {
		raise("LocalJumpError", "no block given (bmbm)")
	}
	bj := &benchJob{job: benchmark.NewJob(width)}
	vm.callBlock(blk, []object.Value{object.Wrap(bj)})

	offset := bj.job.Width() + 1
	clock := benchmarkClock{}

	// Rehearsal pass.
	vm.benchPrint(benchmark.RehearsalHeader(offset) + "\n")
	total := benchmark.Tms{}
	for i, label := range benchJobLabels(bj) {
		res := benchmark.MeasureWith(clock, "", vm.benchRun(bj.procs[i], "report"))
		vm.benchPrint(benchLjust(label, offset) + res.Format(""))
		total = total.Add(res)
	}
	vm.benchPrint(benchmark.RehearsalFooter(offset, total))

	// Take pass.
	vm.benchPrint(benchmark.TakeCaption(offset))
	take := make([]benchmark.Tms, 0, len(bj.procs))
	for i, label := range benchJobLabels(bj) {
		res := benchmark.MeasureWith(clock, label, vm.benchRun(bj.procs[i], "report"))
		vm.benchPrint(benchLjust(label, offset) + res.Format(""))
		take = append(take, res)
	}
	return benchTmsList(take)
}

// benchRun adapts a Ruby block to the library's func() callback, raising
// LocalJumpError (named after the calling method) when no block was given.
func (vm *VM) benchRun(blk *Proc, name string) func() {
	if blk == nil {
		raise("LocalJumpError", "no block given (%s)", name)
	}
	return func() { vm.callBlock(blk, nil) }
}

// benchPrint writes s through the live $stdout (its #print), so a reassigned
// $stdout captures the report, matching MRI.
func (vm *VM) benchPrint(s string) {
	out := vm.globals["$stdout"]
	if object.IsNil(out) {
		out = object.Wrap(vm.curStdout())
	}
	vm.send(out, "print", []object.Value{object.Wrap(object.NewString(s))}, nil)
}

// benchLabel returns the first argument as a label string (MRI's label.to_s),
// defaulting to "" when absent.
func benchLabel(args []object.Value) string {
	if len(args) > 0 {
		return labelToS(args[0])
	}
	return ""
}

// labelToS converts a Ruby value to a label string the way MRI's label.to_s
// does: a String is taken directly, nil becomes "", and anything else uses its
// to_s rendering.
func labelToS(v object.Value) string {
	{
		__sw13 := v
		switch {
		case object.IsKind[*object.String](__sw13):
			s := object.Kind[*object.String](__sw13)
			_ = s
			return s.Str()
		case object.IsNilObj(__sw13):
			s := object.NilObj()
			_ = s
			return ""
		default:
			s := __sw13
			_ = s
			return v.ToS()
		}
	}
}

// benchWidth returns the leading integer label-width argument, defaulting to 0.
func benchWidth(args []object.Value) int {
	if len(args) > 0 {
		return int(intArg(args[0]))
	}
	return 0
}

// benchLabels returns the trailing *labels of bm (the arguments after the
// label-width), as strings.
func benchLabels(args []object.Value) []string {
	if len(args) <= 1 {
		return nil
	}
	return benchStrings(args[1:])
}

// benchStrings maps a slice of Ruby values to label strings.
func benchStrings(vs []object.Value) []string {
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = labelToS(v)
	}
	return out
}

// benchJobLabels returns the labels registered on a benchJob, in order.
func benchJobLabels(bj *benchJob) []string {
	labels := make([]string, len(bj.procs))
	for i := range bj.procs {
		labels[i] = bj.labelAt(i)
	}
	return labels
}

// labelAt returns the label of the i-th registered job item.
func (bj *benchJob) labelAt(i int) string { return bj.labels[i] }

// benchFloat coerces a Ruby numeric to float64 for a Tms field, raising
// TypeError on a non-numeric (matching Tms.new's Float() coercion).
func benchFloat(v object.Value) float64 {
	if f, ok := toFloat(v); ok {
		return f
	}
	raise("TypeError", "no implicit conversion of %s into Float", classNameOf(v))
	return 0
}

// benchFormatArgs maps trailing Ruby format arguments to the []any the library's
// Tms.Format consumes: numerics become float64, everything else its ToS().
func benchFormatArgs(vs []object.Value) []any {
	out := make([]any, len(vs))
	for i, v := range vs {
		if f, ok := toFloat(v); ok {
			out[i] = f
		} else {
			out[i] = v.ToS()
		}
	}
	return out
}

// benchToA renders Tms#to_a: [label, utime, stime, cutime, cstime, real].
func benchToA(t benchmark.Tms) object.Value {
	return object.Wrap(object.NewArray(object.Wrap(object.NewString(t.Label())), object.FloatValue(float64(object.Float(t.Utime()))), object.FloatValue(float64(object.Float(t.Stime()))), object.FloatValue(float64(object.Float(t.Cutime()))), object.FloatValue(float64(object.Float(t.Cstime()))), object.FloatValue(float64(object.Float(t.Real())))))
}

// benchTmsList wraps a slice of library Tms values as a Ruby Array of
// Benchmark::Tms objects.
func benchTmsList(ts []benchmark.Tms) object.Value {
	elems := make([]object.Value, len(ts))
	for i, t := range ts {
		elems[i] = object.Wrap(&Tms{t: t})
	}
	return object.Wrap(object.NewArrayFromSlice(elems))
}

// benchExtras coerces a block's return value into the slice of summary Tms the
// driver renders: nil/non-array (or an array of non-Tms) yields none; an array
// of Tms is taken as the extra rows. It mirrors how Benchmark#benchmark treats
// the block's result.
func benchExtras(v object.Value) []benchmark.Tms {
	arr, ok := object.KindOK[*object.Array](v)
	if !ok {
		return nil
	}
	out := make([]benchmark.Tms, 0, len(arr.Elems))
	for _, e := range arr.Elems {
		t, ok := object.KindOK[*Tms](e)
		if !ok {
			return nil
		}
		out = append(out, t.t)
	}
	return out
}

// benchLjust left-justifies s to width with spaces, matching the library's
// internal ljust (used for the bmbm rows it does not render as a whole table).
func benchLjust(s string, width int) string {
	if len(s) >= width {
		return s
	}
	pad := make([]byte, width-len(s))
	for i := range pad {
		pad[i] = ' '
	}
	return s + string(pad)
}
