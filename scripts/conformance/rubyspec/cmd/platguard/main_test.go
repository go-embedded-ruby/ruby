// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"strconv"
	"testing"

	gc "github.com/go-embedded-ruby/ruby/scripts/conformance/rubyspec/internal/guardcensus"
)

func site(file string, line int) gc.Disagreement {
	return gc.Disagreement{Site: gc.PlatformSite{File: file, Line: line}}
}

// TestTheListingIsOrderedByFileThenLine holds the report's order, which is part
// of the report.
//
// ⛔ EACH OF THE THREE ROWS BELOW FAILS A DIFFERENT HALF. Without the file
// comparison the rows stay in input order; without the line comparison two
// sites in one file come back in input order; and a comparator that sorted on
// line alone would interleave files. A sweep found the file comparison held by
// nothing, which is why this file exists: `platguard` had no test at all, so
// the order this census is diffed by was never checked.
func TestTheListingIsOrderedByFileThenLine(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   []gc.Disagreement
		want []string
	}{
		{
			"files are ordered against each other",
			[]gc.Disagreement{site("core/io/seek_spec.rb", 80), site("core/array/pack/j_spec.rb", 7)},
			[]string{"core/array/pack/j_spec.rb:7", "core/io/seek_spec.rb:80"},
		},
		{
			"two sites in one file are ordered by line",
			[]gc.Disagreement{site("core/process/constants_spec.rb", 46), site("core/process/constants_spec.rb", 4)},
			[]string{"core/process/constants_spec.rb:4", "core/process/constants_spec.rb:46"},
		},
		{
			// A comparator keyed on the line alone would put 4 first and split
			// the two constants_spec rows apart.
			"a low line in a late file does not jump ahead",
			[]gc.Disagreement{
				site("core/process/constants_spec.rb", 46),
				site("core/array/pack/j_spec.rb", 7),
				site("core/process/constants_spec.rb", 4),
			},
			[]string{
				"core/array/pack/j_spec.rb:7",
				"core/process/constants_spec.rb:4",
				"core/process/constants_spec.rb:46",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := inCorpusOrder(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("got %d row(s), want %d", len(got), len(tc.want))
			}
			for i, w := range tc.want {
				if key(got[i]) != w {
					t.Fatalf("row %d is %s, want %s (full order: %v)", i, key(got[i]), w, keys(got))
				}
			}
		})
	}
}

// TestInCorpusOrderLeavesItsArgumentAlone matters because the caller prints the
// totals from `ds` after this runs. A sort in place would reorder the slice the
// counts were taken from — harmless today, and exactly the kind of coupling
// that stops being harmless when someone moves a line.
func TestInCorpusOrderLeavesItsArgumentAlone(t *testing.T) {
	in := []gc.Disagreement{site("z_spec.rb", 1), site("a_spec.rb", 2)}
	_ = inCorpusOrder(in)
	if key(in[0]) != "z_spec.rb:1" {
		t.Fatalf("the caller's slice was reordered: %v", keys(in))
	}
}

func key(d gc.Disagreement) string {
	return d.Site.File + ":" + strconv.Itoa(d.Site.Line)
}

func keys(ds []gc.Disagreement) []string {
	out := make([]string, len(ds))
	for i, d := range ds {
		out[i] = key(d)
	}
	return out
}
