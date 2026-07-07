// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"time"

	simplecov "github.com/go-ruby-simplecov/simplecov"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerSimpleCov installs the SimpleCov module (require "simplecov"): the
// configuration/orchestration surface (SimpleCov.start / add_filter / add_group /
// minimum_coverage / …), the SimpleCov::Result and SimpleCov::SourceFile value
// types, the SimpleCov::Formatter::SimpleFormatter text formatter, and the
// .resultset.json (de)serialiser. Everything is delegated to
// github.com/go-ruby-simplecov/simplecov — a pure-Go, no-cgo reimplementation of
// the deterministic result engine of Ruby's SimpleCov gem.
//
// SimpleCov has two halves: a collector that instruments a running program to
// record how many times each line executed, and a result engine that models,
// filters, groups, thresholds and formats that data. Only the engine is bound
// here. rbgo's VM does not yet track per-Ruby-line execution counts, so live
// line-coverage collection is a DEFERRED VM feature (a seam left for a future VM
// Coverage table). Until then the raw line-hit source is supplied explicitly —
// SimpleCov.add_coverage(file, [hits]) / SimpleCov.coverage = {…}, or a hash
// passed straight to SimpleCov::Result.new — and the engine does the rest. With
// no data supplied, SimpleCov.result is simply empty; no coverage is fabricated.
// The instance value types and the coverage/resultset converters live in
// simplecov_bind.go.
func (vm *VM) registerSimpleCov() {
	mod := newClass("SimpleCov", nil)
	mod.isModule = true
	vm.consts["SimpleCov"] = mod

	result := vm.registerSimpleCovResult(mod)
	source := vm.registerSimpleCovSourceFile(mod)
	formatter := vm.registerSimpleCovFormatter(mod, result)

	vm.simpleCov = &simpleCovState{result: result, source: source}
	vm.simpleCovReset()
	vm.simpleCov.formatter = formatter

	sm := func(name string, fn NativeFn) {
		mod.smethods[name] = &Method{name: name, owner: mod, native: fn}
	}

	// SimpleCov.start(&block) resets the configuration and the collected coverage,
	// then instance_evals the block against the module (Ruby DSL style) so a bare
	// `add_filter "/test/"` inside it configures this run. It returns nil, matching
	// SimpleCov.start (which normally hooks Coverage.start; here the coverage source
	// is the deferred VM seam, so start only configures).
	sm("start", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		vm.simpleCovReset()
		if blk != nil {
			vm.callBlockSelf(blk, mod, nil)
		}
		return object.NilV
	})

	// SimpleCov.configure(&block) applies further configuration without discarding
	// the current coverage or filters, mirroring SimpleCov.configure.
	sm("configure", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk != nil {
			vm.callBlockSelf(blk, mod, nil)
		}
		return object.NilV
	})

	// SimpleCov.add_filter("…" | /…/ | […] ) { |sf| … } excludes matching files
	// from a result. A block filter is called with each SimpleCov::SourceFile.
	sm("add_filter", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		cfg := vm.simpleCov.cfg
		if blk != nil {
			cfg.AddBlockFilter(vm.simpleCovBlockFilterFn(blk, cfg.Root))
			return object.NilV
		}
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		vm.simpleCovAddFilterValue(args[0])
		return object.NilV
	})

	// SimpleCov.add_group(name, "…" | /…/ ) { |sf| … } selects the files that
	// belong to a named group.
	sm("add_group", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
		}
		name := strArg(args[0])
		cfg := vm.simpleCov.cfg
		if blk != nil {
			cfg.AddGroup(name, simplecov.BlockFilter{Fn: vm.simpleCovBlockFilterFn(blk, cfg.Root)})
			return object.NilV
		}
		if len(args) < 2 {
			raise("ArgumentError", "add_group requires a filter argument or a block")
		}
		switch f := args[1].(type) {
		case *object.String:
			cfg.AddStringGroup(name, f.Str())
		case *Regexp:
			cfg.AddGroup(name, simplecov.RegexFilter{Re: simpleCovGoRegexp(f)})
		default:
			raise("TypeError", "group filter must be a String or Regexp, got %s", args[1].Inspect())
		}
		return object.NilV
	})

	// SimpleCov.minimum_coverage(n | {line:, branch:}) sets the overall minimum.
	sm("minimum_coverage", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		vm.simpleCovThreshold(args, vm.simpleCov.cfg.MinimumCoverage)
		return object.NilV
	})

	// SimpleCov.minimum_coverage_by_file(n | {line:, branch:}) sets the per-file
	// minimum.
	sm("minimum_coverage_by_file", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		vm.simpleCovThreshold(args, vm.simpleCov.cfg.MinimumCoverageByFile)
		return object.NilV
	})

	// SimpleCov.maximum_coverage_drop(n | {line:, branch:}) caps how far coverage
	// may fall versus the previous run.
	sm("maximum_coverage_drop", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		vm.simpleCovThreshold(args, vm.simpleCov.cfg.MaximumCoverageDrop)
		return object.NilV
	})

	// SimpleCov.refuse_coverage_drop(*criteria) forbids any drop on the named
	// criteria (:line / :branch), or on all of them when none is given.
	sm("refuse_coverage_drop", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		crits := make([]simplecov.Criterion, len(args))
		for i, a := range args {
			crits[i] = simpleCovCriterion(a)
		}
		vm.simpleCov.cfg.RefuseCoverageDrop(crits...)
		return object.NilV
	})

	// SimpleCov.command_name(name = nil): with an argument, names this run and
	// returns it; without one, returns the current command name.
	sm("command_name", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) > 0 {
			vm.simpleCov.cfg.CommandName = strArg(args[0])
		}
		return object.NewString(vm.simpleCov.cfg.CommandName)
	})

	// SimpleCov.root(dir = nil): with an argument, sets the project root and returns
	// it; without one, returns the current root.
	sm("root", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) > 0 {
			vm.simpleCov.cfg.Root = strArg(args[0])
		}
		return object.NewString(vm.simpleCov.cfg.Root)
	})

	// SimpleCov.add_coverage(filename, lines) feeds one file's raw line-hit data
	// into the collected coverage map. This is the deferred VM line-coverage seam:
	// the caller (a future VM Coverage table, or a test) supplies the per-line hit
	// counts the engine turns into a result.
	sm("add_coverage", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
		}
		lines, ok := args[1].(*object.Array)
		if !ok {
			raise("TypeError", "coverage lines must be an Array, got %s", args[1].Inspect())
		}
		vm.simpleCov.coverage[strArg(args[0])] = simplecov.FileCoverage{Lines: simpleCovLinesToHits(lines)}
		return object.NilV
	})

	// SimpleCov.coverage returns the collected raw coverage as a Hash of
	// {filename => line-hit Array}.
	sm("coverage", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return simpleCovCoverageToRuby(vm.simpleCov.coverage)
	})

	// SimpleCov.coverage = hash replaces the collected coverage wholesale (the
	// bulk form of the deferred VM line-coverage seam).
	sm("coverage=", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		h, ok := args[0].(*object.Hash)
		if !ok {
			raise("TypeError", "coverage must be a Hash, got %s", args[0].Inspect())
		}
		vm.simpleCov.coverage = simpleCovCoverageFromHash(h)
		return args[0]
	})

	// SimpleCov.result builds a SimpleCov::Result from the collected coverage,
	// applying the configured filters and groups.
	sm("result", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.simpleCovBuildResult()
	})

	// SimpleCov.result? reports whether any coverage has been collected.
	sm("result?", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(len(vm.simpleCov.coverage) > 0)
	})

	// SimpleCov.run_checks(result, previous = nil) runs the configured threshold
	// checks and returns the SimpleCov exit code (0 success, 2 below minimum,
	// 3 excessive drop).
	sm("run_checks", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
		}
		res := simpleCovResultArg(args[0])
		var prev *simplecov.Result
		if len(args) > 1 && !object.IsNil(args[1]) {
			prev = simpleCovResultArg(args[1]).r
		}
		code, _ := vm.simpleCov.cfg.RunChecks(res.r, prev)
		return object.IntValue(int64(code))
	})

	// SimpleCov.at_exit(&block): given a block, registers it as the exit hook and
	// returns nil (mirroring SimpleCov.at_exit { … }). Called with no block, it
	// runs the hook, formats the current result through the active formatter, runs
	// the threshold checks and returns the resulting exit code.
	sm("at_exit", func(vm *VM, _ object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk != nil {
			vm.simpleCov.atExit = blk
			return object.NilV
		}
		res := vm.simpleCovBuildResult()
		if vm.simpleCov.atExit != nil {
			vm.callBlock(vm.simpleCov.atExit, nil)
		}
		vm.send(vm.simpleCov.formatter, "format", []object.Value{res}, nil)
		code, _ := vm.simpleCov.cfg.RunChecks(res.(*SimpleCovResult).r, nil)
		return object.IntValue(int64(code))
	})

	// SimpleCov.formatter returns the active formatter.
	sm("formatter", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.simpleCov.formatter
	})

	// SimpleCov.formatter = obj sets the active formatter (any object responding to
	// #format(result)).
	sm("formatter=", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		vm.simpleCov.formatter = args[0]
		return args[0]
	})

	// SimpleCov.load_resultset(path) reads a .resultset.json through the engine's
	// FS seam and returns it as a Ruby Hash.
	sm("load_resultset", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		rs, err := vm.simpleCov.cfg.LoadResultset(strArg(args[0]))
		if err != nil {
			raise("RuntimeError", "%s", err.Error())
		}
		return simpleCovResultsetToRuby(rs)
	})

	// SimpleCov.store_resultset(path, hash) writes a resultset Hash to
	// .resultset.json through the engine's FS seam.
	sm("store_resultset", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
		}
		h, ok := args[1].(*object.Hash)
		if !ok {
			raise("TypeError", "resultset must be a Hash, got %s", args[1].Inspect())
		}
		if err := vm.simpleCov.cfg.StoreResultset(strArg(args[0]), simpleCovResultsetFromRuby(h)); err != nil {
			raise("RuntimeError", "%s", err.Error())
		}
		return object.NilV
	})
}

// simpleCovReset builds a fresh engine configuration (command name "rbgo") and an
// empty coverage map, used at registration and by SimpleCov.start.
func (vm *VM) simpleCovReset() {
	vm.simpleCov.cfg = simplecov.New(simplecov.WithCommandName("rbgo"))
	vm.simpleCov.coverage = map[string]simplecov.FileCoverage{}
	vm.simpleCov.atExit = nil
}

// simpleCovBuildResult turns the collected coverage into a SimpleCov::Result
// through the configured filters and groups.
func (vm *VM) simpleCovBuildResult() object.Value {
	cfg := vm.simpleCov.cfg
	res := cfg.NewResult(cfg.CommandName, vm.simpleCov.coverage)
	return vm.simpleCovResultValue(res, cfg.Root)
}

// simpleCovAddFilterValue adds one add_filter argument to the configuration: a
// String (substring of the project path), a Regexp (over the absolute filename),
// or an Array of either. Anything else raises TypeError.
func (vm *VM) simpleCovAddFilterValue(v object.Value) {
	cfg := vm.simpleCov.cfg
	switch f := v.(type) {
	case *object.String:
		cfg.AddStringFilter(f.Str())
	case *Regexp:
		cfg.AddRegexpFilter(simpleCovGoRegexp(f))
	case *object.Array:
		for _, e := range f.Elems {
			vm.simpleCovAddFilterValue(e)
		}
	default:
		raise("TypeError", "filter must be a String, Regexp or Array, got %s", v.Inspect())
	}
}

// simpleCovBlockFilterFn adapts a Ruby block into a go-ruby-simplecov filter
// function: it wraps each candidate as a SimpleCov::SourceFile and returns the
// block's truthiness. It runs synchronously during result building.
func (vm *VM) simpleCovBlockFilterFn(blk *Proc, root string) func(*simplecov.SourceFile) bool {
	return func(sf *simplecov.SourceFile) bool {
		return vm.callBlock(blk, []object.Value{vm.simpleCovSourceValue(sf, root)}).Truthy()
	}
}

// simpleCovThreshold applies a minimum/maximum threshold argument to a setter: a
// bare Integer/Float gates the line criterion, and a Hash ({line:, branch:})
// gates each named criterion.
func (vm *VM) simpleCovThreshold(args []object.Value, apply func(simplecov.Criterion, float64)) {
	if len(args) == 0 {
		raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
	}
	if h, ok := args[0].(*object.Hash); ok {
		for _, k := range h.Keys {
			v, _ := h.Get(k)
			apply(simpleCovCriterion(k), simpleCovFloat(v))
		}
		return
	}
	apply(simplecov.Line, simpleCovFloat(args[0]))
}

// simpleCovFloat reads a numeric threshold as a float64, accepting an Integer or a
// Float and raising TypeError otherwise.
func simpleCovFloat(v object.Value) float64 {
	switch n := v.(type) {
	case object.Integer:
		return float64(n)
	case object.Float:
		return float64(n)
	}
	raise("TypeError", "coverage threshold must be numeric, got %s", v.Inspect())
	return 0
}

// simpleCovCriterion reads a coverage criterion name (a Symbol or String,
// "line"/"branch") into a simplecov.Criterion, raising for anything else.
func simpleCovCriterion(v object.Value) simplecov.Criterion {
	var s string
	switch x := v.(type) {
	case object.Symbol:
		s = string(x)
	case *object.String:
		s = x.Str()
	default:
		raise("TypeError", "coverage criterion must be a Symbol or String, got %s", v.Inspect())
	}
	switch s {
	case "line":
		return simplecov.Line
	case "branch":
		return simplecov.Branch
	}
	raise("ArgumentError", "unknown coverage criterion %q", s)
	return ""
}

// simpleCovResultArg asserts an argument is a SimpleCov::Result, raising TypeError
// otherwise.
func simpleCovResultArg(v object.Value) *SimpleCovResult {
	r, ok := v.(*SimpleCovResult)
	if !ok {
		raise("TypeError", "expected a SimpleCov::Result, got %s", v.Inspect())
	}
	return r
}

// registerSimpleCovResult installs SimpleCov::Result — its .new / .from_hash
// constructors and the read model (covered_percent, files, groups, …) — and
// returns the class.
func (vm *VM) registerSimpleCovResult(mod *RClass) *RClass {
	cls := newClass("SimpleCov::Result", vm.cObject)
	mod.consts["Result"] = cls
	vm.consts["SimpleCov::Result"] = cls

	// SimpleCov::Result.new(coverage, command_name: "rbgo") builds a result from a
	// raw coverage Hash ({filename => line-hit Array or {"lines" => …}}).
	cls.smethods["new"] = &Method{name: "new", owner: cls, native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		h, ok := args[0].(*object.Hash)
		if !ok {
			raise("TypeError", "coverage must be a Hash, got %s", args[0].Inspect())
		}
		name := "rbgo"
		if len(args) > 1 {
			if opts, ok := args[1].(*object.Hash); ok {
				if cn, ok := opts.Get(object.SymVal("command_name")); ok {
					name = strArg(cn)
				}
			}
		}
		res := simplecov.NewResult(name, simpleCovCoverageFromHash(h), time.Now())
		return vm.simpleCovResultValue(res, "")
	}}

	// SimpleCov::Result.from_hash(resultset) rebuilds a result from a resultset Hash
	// ({command => {"coverage" => …, "timestamp" => n}}), the inverse of #to_hash.
	cls.smethods["from_hash"] = &Method{name: "from_hash", owner: cls, native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		h, ok := args[0].(*object.Hash)
		if !ok {
			raise("TypeError", "resultset must be a Hash, got %s", args[0].Inspect())
		}
		results := simpleCovResultsetFromRuby(h).Results()
		if len(results) == 0 {
			raise("ArgumentError", "resultset is empty")
		}
		return vm.simpleCovResultValue(results[0], "")
	}}

	d := func(name string, fn NativeFn) { cls.define(name, fn) }
	self := func(v object.Value) *SimpleCovResult { return v.(*SimpleCovResult) }

	d("covered_percent", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Float(self(v).r.CoveredPercent())
	})
	d("covered_strength", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Float(self(v).r.CoveredStrength())
	})
	d("covered_lines", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).r.CoveredLines()))
	})
	d("missed_lines", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).r.MissedLines()))
	})
	d("total_lines", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).r.TotalLines()))
	})
	files := func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		r := self(v)
		return vm.simpleCovSourceArray(r.r.Files.Files(), r.root)
	}
	d("files", files)
	d("source_files", files)
	d("command_name", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).r.CommandName)
	})
	d("created_at", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.simpleCovTime(self(v).r.CreatedAt.Unix())
	})
	d("least_covered_file", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		r := self(v)
		sf := r.r.LeastCoveredFile()
		if sf == nil {
			return object.NilV
		}
		return vm.simpleCovSourceValue(sf, r.root)
	})
	d("groups", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		r := self(v)
		out := object.NewHash()
		for _, g := range r.r.Groups() {
			out.Set(object.NewString(g.Name), vm.simpleCovSourceArray(g.Files.Files(), r.root))
		}
		return out
	})
	toHash := func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return simpleCovResultsetToRuby(self(v).r.ToResultset())
	}
	d("to_hash", toHash)
	d("to_resultset", toHash)

	return cls
}

// registerSimpleCovSourceFile installs SimpleCov::SourceFile — one covered file's
// coverage metrics — and returns the class.
func (vm *VM) registerSimpleCovSourceFile(mod *RClass) *RClass {
	cls := newClass("SimpleCov::SourceFile", vm.cObject)
	mod.consts["SourceFile"] = cls
	vm.consts["SimpleCov::SourceFile"] = cls

	d := func(name string, fn NativeFn) { cls.define(name, fn) }
	self := func(v object.Value) *SimpleCovSourceFile { return v.(*SimpleCovSourceFile) }

	d("filename", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).sf.Filename)
	})
	d("project_filename", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		s := self(v)
		return object.NewString(s.sf.ProjectFilename(s.root))
	})
	d("covered_percent", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Float(self(v).sf.CoveredPercent())
	})
	d("covered_strength", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Float(self(v).sf.CoveredStrength())
	})
	d("covered_lines", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).sf.CoveredLinesCount()))
	})
	d("missed_lines", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).sf.MissedLinesCount()))
	})
	d("never_lines", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).sf.NeverLinesCount()))
	})
	d("lines_of_code", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).sf.LinesOfCode()))
	})
	d("relevant_lines", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).sf.RelevantLines()))
	})
	d("lines", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return simpleCovHitsToRuby(self(v).sf.Lines)
	})

	return cls
}

// registerSimpleCovFormatter installs SimpleCov::Formatter and its built-in
// SimpleCov::Formatter::SimpleFormatter, and returns a default formatter instance.
func (vm *VM) registerSimpleCovFormatter(mod *RClass, result *RClass) object.Value {
	fmtMod := newClass("SimpleCov::Formatter", nil)
	fmtMod.isModule = true
	mod.consts["Formatter"] = fmtMod
	vm.consts["SimpleCov::Formatter"] = fmtMod

	cls := newClass("SimpleCov::Formatter::SimpleFormatter", vm.cObject)
	fmtMod.consts["SimpleFormatter"] = cls
	vm.consts["SimpleCov::Formatter::SimpleFormatter"] = cls

	cls.smethods["new"] = &Method{name: "new", owner: cls, native: func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return &RObject{class: cls, ivars: map[string]object.Value{}}
	}}

	// #format(result) renders the SimpleCov::Result to text: one section per group,
	// each file with its coverage percentage (as SimpleCov's SimpleFormatter does).
	cls.define("format", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		return object.NewString(simplecov.SimpleFormatter{}.Format(simpleCovResultArg(args[0]).r))
	})

	return &RObject{class: cls, ivars: map[string]object.Value{}}
}
