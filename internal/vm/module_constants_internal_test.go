// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/compiler"
	"github.com/go-embedded-ruby/ruby/internal/object"
	"github.com/go-ruby-parser/parser"
)

// TestSortedConstKeysRevalidates covers the cache behind Module#constants: it is
// reused while the table is unchanged, and rebuilt when a name is removed and
// another added (same size, so only the membership probe can see it) or when the
// table grows.
func TestSortedConstKeysRevalidates(t *testing.T) {
	c := &RClass{consts: map[string]object.Value{"B": object.NilV, "A": object.NilV}}
	first := c.sortedConstKeys()
	if !slices.Equal(first, []string{"A", "B"}) {
		t.Fatalf("keys = %q", first)
	}
	if again := c.sortedConstKeys(); &again[0] != &first[0] {
		t.Error("an unchanged table did not reuse the cache")
	}
	delete(c.consts, "A")
	c.consts["C"] = object.NilV
	if got := c.sortedConstKeys(); !slices.Equal(got, []string{"B", "C"}) {
		t.Errorf("after swap keys = %q", got)
	}
	c.consts["D"] = object.NilV
	if got := c.sortedConstKeys(); !slices.Equal(got, []string{"B", "C", "D"}) {
		t.Errorf("after growth keys = %q", got)
	}
}

// TestPublicConstNamesAutoloadAlsoDefined covers a name that is both a constant
// and a pending autoload of one table (the autoload is being loaded): it is
// listed once, and the table's names stay sorted with the autoloads merged in.
func TestPublicConstNamesAutoloadAlsoDefined(t *testing.T) {
	c := &RClass{
		consts:    map[string]object.Value{"M": object.NilV, "Z": object.NilV},
		autoloads: map[string]string{"M": "m", "A": "a"},
	}
	if got := publicConstNames([]*RClass{c}); !slices.Equal(got, []string{"A", "M", "Z"}) {
		t.Errorf("names = %q", got)
	}
	// A pending autoload in a nearer table claims the name: the farther
	// table's constant of the same name is not listed again.
	near := &RClass{autoloads: map[string]string{"X": "x"}}
	far := &RClass{consts: map[string]object.Value{"X": object.NilV}}
	if got := publicConstNames([]*RClass{near, far}); !slices.Equal(got, []string{"X"}) {
		t.Errorf("chained names = %q", got)
	}
}

// BenchmarkObjectConstants measures Object.constants with 10 000 user-defined
// top-level constants, the size issue #833's performance check uses.
func BenchmarkObjectConstants(b *testing.B) {
	var src strings.Builder
	for i := range 10000 {
		fmt.Fprintf(&src, "BENCH_K%d = %d\n", i, i)
	}
	prog, err := parser.Parse(src.String())
	if err != nil {
		b.Fatal(err)
	}
	iseq, err := compiler.Compile(prog)
	if err != nil {
		b.Fatal(err)
	}
	v := New(io.Discard)
	if _, err := v.Run(iseq); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		symbolArray(v.constantNames(v.cObject, true))
	}
}
