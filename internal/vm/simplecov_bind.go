// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"regexp"

	gotime "github.com/go-composites/time/src"
	simplecov "github.com/go-ruby-simplecov/simplecov"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin binding between rbgo's Ruby object graph and the
// interpreter-independent result engine of github.com/go-ruby-simplecov/simplecov
// (a pure-Go, no-cgo reimplementation of the deterministic half of Ruby's
// SimpleCov gem: the model that turns raw per-file line-hit data into filtered,
// grouped, thresholded results, serialises them as .resultset.json and formats a
// summary). It carries the two instance value types SimpleCov hands to Ruby — a
// Result and a SourceFile — plus the converters that move coverage maps, line-hit
// arrays and resultset hashes across the boundary.
//
// SimpleCov splits into a *collector* that instruments the running program and
// records how many times each line executed, and a *result engine* that models
// that raw data. Only the engine is bound here; the collector is the host's job.
// rbgo's VM does not yet track per-Ruby-line execution counts, so live
// line-coverage collection is a DEFERRED VM feature: this binding feeds the
// engine an explicitly-supplied coverage map (SimpleCov.add_coverage /
// SimpleCov.coverage=, or a hash passed straight to SimpleCov::Result.new), and
// the seam is ready for a future VM Coverage table to populate it automatically.
// No coverage source is fabricated: with no data supplied, the result is empty.
// See simplecov.go for the module wiring.

// simpleCovState holds the mutable SimpleCov module configuration: the
// go-ruby-simplecov engine config (filters, groups, thresholds, root and
// command name), the accumulated raw per-file line-coverage fed through the
// deferred VM line-coverage seam, the active formatter and the optional at_exit
// hook block. It is created once by registerSimpleCov and reset by
// SimpleCov.start.
type simpleCovState struct {
	cfg       *simplecov.SimpleCov
	coverage  map[string]simplecov.FileCoverage
	formatter object.Value // a Formatter instance responding to #format
	atExit    *Proc        // the block registered by SimpleCov.at_exit { … }
	result    *RClass      // SimpleCov::Result class (for wrapping)
	source    *RClass      // SimpleCov::SourceFile class (for wrapping)
}

// SimpleCovResult is an instance of SimpleCov::Result: a named, timestamped,
// filtered and grouped view over a set of covered files, backed by a
// go-ruby-simplecov *simplecov.Result. root is the project root used to derive
// project-relative paths for the files it hands out.
type SimpleCovResult struct {
	cls  *RClass
	r    *simplecov.Result
	root string
}

func (r *SimpleCovResult) ToS() string     { return "#<SimpleCov::Result>" }
func (r *SimpleCovResult) Inspect() string { return "#<SimpleCov::Result>" }
func (r *SimpleCovResult) Truthy() bool    { return true }

// SimpleCovSourceFile is an instance of SimpleCov::SourceFile: one covered
// file's line data and coverage metrics, backed by a go-ruby-simplecov
// *simplecov.SourceFile. root is the project root used by #project_filename.
type SimpleCovSourceFile struct {
	cls  *RClass
	sf   *simplecov.SourceFile
	root string
}

func (s *SimpleCovSourceFile) ToS() string     { return "#<SimpleCov::SourceFile>" }
func (s *SimpleCovSourceFile) Inspect() string { return "#<SimpleCov::SourceFile>" }
func (s *SimpleCovSourceFile) Truthy() bool    { return true }

// simpleCovResultValue wraps a go-ruby-simplecov Result as a Ruby
// SimpleCov::Result, carrying the project root so its source files derive
// project-relative paths.
func (vm *VM) simpleCovResultValue(r *simplecov.Result, root string) object.Value {
	return &SimpleCovResult{cls: vm.simpleCov.result, r: r, root: root}
}

// simpleCovSourceValue wraps a go-ruby-simplecov SourceFile as a Ruby
// SimpleCov::SourceFile.
func (vm *VM) simpleCovSourceValue(sf *simplecov.SourceFile, root string) object.Value {
	return &SimpleCovSourceFile{cls: vm.simpleCov.source, sf: sf, root: root}
}

// simpleCovSourceArray renders a slice of SourceFile as a Ruby Array of
// SimpleCov::SourceFile.
func (vm *VM) simpleCovSourceArray(files []*simplecov.SourceFile, root string) *object.Array {
	out := object.NewArrayFromSlice(make([]object.Value, len(files)))
	for i, sf := range files {
		out.Elems[i] = vm.simpleCovSourceValue(sf, root)
	}
	return out
}

// simpleCovTime wraps a Unix instant (whole seconds) as a Ruby Time, matching how
// SimpleCov stamps a result's created_at from a resultset timestamp.
func (vm *VM) simpleCovTime(unix int64) object.Value {
	return &Time{t: gotime.FromUnix(unix)}
}

// simpleCovLinesToHits converts a Ruby line-hit Array (one element per source
// line: an Integer hit count, or nil for a non-coverable line) into the engine's
// []Hit, raising TypeError for any other element type.
func simpleCovLinesToHits(arr *object.Array) []simplecov.Hit {
	hits := make([]simplecov.Hit, len(arr.Elems))
	for i, e := range arr.Elems {
		if object.IsNil(e) {
			hits[i] = simplecov.Uncoverable()
			continue
		}
		n, ok := e.(object.Integer)
		if !ok {
			raise("TypeError", "coverage line must be an Integer or nil, got %s", e.Inspect())
		}
		hits[i] = simplecov.Coverable(int(n))
	}
	return hits
}

// simpleCovHitsToRuby renders a []Hit back as a Ruby Array (Integer count for a
// coverable line, nil for a non-coverable one).
func simpleCovHitsToRuby(hits []simplecov.Hit) *object.Array {
	out := object.NewArrayFromSlice(make([]object.Value, len(hits)))
	for i, h := range hits {
		if h.Valid {
			out.Elems[i] = object.IntValue(int64(h.Count))
		} else {
			out.Elems[i] = object.NilV
		}
	}
	return out
}

// simpleCovFileCoverageValue reads one file's raw coverage from a Ruby value: a
// bare Array of line hits, or a Hash carrying "lines" (the resultset FileCoverage
// shape). Anything else raises TypeError.
func simpleCovFileCoverageValue(v object.Value) simplecov.FileCoverage {
	switch x := v.(type) {
	case *object.Array:
		return simplecov.FileCoverage{Lines: simpleCovLinesToHits(x)}
	case *object.Hash:
		if lv, ok := x.Get(object.NewString("lines")); ok {
			if arr, ok := lv.(*object.Array); ok {
				return simplecov.FileCoverage{Lines: simpleCovLinesToHits(arr)}
			}
		}
		raise("TypeError", "coverage entry hash must carry a \"lines\" Array, got %s", v.Inspect())
	}
	raise("TypeError", "coverage entry must be an Array or Hash, got %s", v.Inspect())
	return simplecov.FileCoverage{}
}

// simpleCovCoverageFromHash converts a Ruby coverage Hash ({filename => line-hit
// Array or {"lines"=>…}}) into the engine's map[filename]FileCoverage, raising
// TypeError when a key is not a String.
func simpleCovCoverageFromHash(h *object.Hash) map[string]simplecov.FileCoverage {
	out := make(map[string]simplecov.FileCoverage, h.Len())
	for _, k := range h.Keys {
		name, ok := k.(*object.String)
		if !ok {
			raise("TypeError", "coverage filename must be a String, got %s", k.Inspect())
		}
		v, _ := h.Get(k)
		out[name.Str()] = simpleCovFileCoverageValue(v)
	}
	return out
}

// simpleCovCoverageToRuby renders a coverage map back as a Ruby Hash of
// {filename => line-hit Array}, so SimpleCov.coverage round-trips what was fed in.
func simpleCovCoverageToRuby(cov map[string]simplecov.FileCoverage) *object.Hash {
	h := object.NewHash()
	for name, fc := range cov {
		h.Set(object.NewString(name), simpleCovHitsToRuby(fc.Lines))
	}
	return h
}

// simpleCovResultsetToRuby renders a resultset ({command => {coverage, timestamp}})
// as the Ruby Hash SimpleCov's .resultset.json models: each command maps to a Hash
// with a "coverage" Hash ({filename => {"lines" => [...] }}) and an integer
// "timestamp".
func simpleCovResultsetToRuby(rs simplecov.Resultset) *object.Hash {
	h := object.NewHash()
	for cmd, cr := range rs {
		cov := object.NewHash()
		for name, fc := range cr.Coverage {
			entry := object.NewHash()
			entry.Set(object.NewString("lines"), simpleCovHitsToRuby(fc.Lines))
			cov.Set(object.NewString(name), entry)
		}
		inner := object.NewHash()
		inner.Set(object.NewString("coverage"), cov)
		inner.Set(object.NewString("timestamp"), object.IntValue(cr.Timestamp))
		h.Set(object.NewString(cmd), inner)
	}
	return h
}

// simpleCovResultsetFromRuby parses a Ruby resultset Hash ({command => {"coverage"
// => {...}, "timestamp" => n}}) into the engine's Resultset, raising TypeError on
// a malformed shape.
func simpleCovResultsetFromRuby(h *object.Hash) simplecov.Resultset {
	rs := make(simplecov.Resultset, h.Len())
	for _, k := range h.Keys {
		cmd, ok := k.(*object.String)
		if !ok {
			raise("TypeError", "resultset command must be a String, got %s", k.Inspect())
		}
		v, _ := h.Get(k)
		inner, ok := v.(*object.Hash)
		if !ok {
			raise("TypeError", "resultset entry must be a Hash, got %s", v.Inspect())
		}
		cr := simplecov.CommandResult{Coverage: map[string]simplecov.FileCoverage{}}
		if cv, ok := inner.Get(object.NewString("coverage")); ok {
			ch, ok := cv.(*object.Hash)
			if !ok {
				raise("TypeError", "resultset coverage must be a Hash, got %s", cv.Inspect())
			}
			cr.Coverage = simpleCovCoverageFromHash(ch)
		}
		if tv, ok := inner.Get(object.NewString("timestamp")); ok {
			if n, ok := tv.(object.Integer); ok {
				cr.Timestamp = int64(n)
			}
		}
		rs[cmd.Str()] = cr
	}
	return rs
}

// simpleCovGoRegexp converts a Ruby Regexp into a Go stdlib *regexp.Regexp for a
// filter, honouring the i (ignore-case) and m (multiline) flags. A pattern the
// stdlib engine cannot compile raises RegexpError.
func simpleCovGoRegexp(r *Regexp) *regexp.Regexp {
	pat := r.source
	prefix := ""
	if containsFlag(r.flags, 'i') {
		prefix += "i"
	}
	if containsFlag(r.flags, 'm') {
		prefix += "s" // Ruby's /m dot-matches-newline is Go's (?s)
	}
	if prefix != "" {
		pat = "(?" + prefix + ")" + pat
	}
	re, err := regexp.Compile(pat)
	if err != nil {
		raise("RegexpError", "%s", err.Error())
	}
	return re
}

// containsFlag reports whether the flag string carries the given flag rune.
func containsFlag(flags string, f rune) bool {
	for _, c := range flags {
		if c == f {
			return true
		}
	}
	return false
}
