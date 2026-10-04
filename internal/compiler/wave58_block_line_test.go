// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package compiler_test

import (
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/compiler"
	"github.com/go-ruby-parser/parser"
)

// blockLines returns the FirstLine of every child ISeq, depth-first, which for
// these sources is the blocks in source order.
func blockLines(t *testing.T, src string) []int {
	t.Helper()
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	iseq, err := compiler.Compile(prog)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	var out []int
	var walk func(*bytecode.ISeq)
	walk = func(is *bytecode.ISeq) {
		for _, ch := range is.Children {
			out = append(out, ch.FirstLine)
			walk(ch)
		}
	}
	walk(iseq)
	return out
}

// TestBlockFirstLineIsItsOpener: a block ISeq's FirstLine is the line its `{`,
// `do` or `->` sits on -- MRI's location.first_lineno, which Ruby shows through
// Proc#source_location. The compiler inherited it from the enclosing scope's
// current line instead, which is the same answer only when the block opens on
// the enclosing statement's first line, so every block inside a multi-line
// literal reported the literal's opening line.
//
// Each want is ruby 4.0.5's own #source_location, measured. The parser had to
// record this first (ast.Block.Line, parser v0.11.0): the line was not in the
// tree at all, so no amount of work here could have produced it.
func TestBlockFirstLineIsItsOpener(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		want []int
	}{
		{
			// The discriminating case: the bodies' first lines are 3, 6 and 7, and
			// ruby answers 2, 5 and 6 -- the openers. The third block is empty, so
			// it has no body line at all.
			"multi-line hash",
			"h = {\n  :a => proc {\n    1\n  },\n  :b => proc { 2 },\n  :c => proc {\n  },\n}\n",
			[]int{2, 5, 6},
		},
		{
			"multi-line array with do...end",
			"x = [\n  proc do\n    3\n  end,\n]\n",
			[]int{2},
		},
		{
			// The arrow's own line, not its `{`.
			"arrow lambda",
			"h = {\n  :a =>\n    -> {\n      1\n    },\n  :b => ->() { 2 },\n}\n",
			[]int{3, 6},
		},
		{
			"single line, unchanged",
			"h = { :a => proc { 1 } }\n",
			[]int{1},
		},
		{
			"method call whose do lands on a later line",
			"foo(1,\n    2) do\n  3\nend\n",
			[]int{2},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := blockLines(t, tc.src)
			if len(got) != len(tc.want) {
				t.Fatalf("found %d block ISeqs %v, want %d", len(got), got, len(tc.want))
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("block %d: FirstLine = %d, want %d (ruby 4.0.5)", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// TestBlockInstructionLinesAreUnchanged is the control. The fix also seeds the
// block builder's curLine, so the lines its INSTRUCTIONS are attributed to
// could have shifted -- and those feed backtraces, __LINE__ and Kernel#caller,
// which were already right. The block below opens on line 2 and its one
// statement is on line 3: the scope's FirstLine must be 2 and the instruction's
// line 3, not both the same.
func TestBlockInstructionLinesAreUnchanged(t *testing.T) {
	src := "h = {\n  :a => proc {\n    raise \"x\"\n  },\n}\n"
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	iseq, err := compiler.Compile(prog)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	blk := iseq.Children[0]
	if blk.FirstLine != 2 {
		t.Errorf("block FirstLine = %d, want 2 (its `{`)", blk.FirstLine)
	}
	if len(blk.Lines) == 0 {
		t.Fatalf("block recorded no instruction lines")
	}
	if got := blk.Lines[0].Line; got != 3 {
		t.Errorf("first instruction attributed to line %d, want 3 (the raise); "+
			"the scope's opening line must not overwrite the body's", got)
	}
}
