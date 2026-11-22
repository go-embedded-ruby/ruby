// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"fmt"
	"os"
	"testing"
)

// TestAFlushThatCannotOpenRaisesTheOpensError: the open's own errno is what is
// raised. A missing directory would not show it — the errno is recovered from
// the path either way — but a directory answers EISDIR only to the open.
func TestAFlushThatCannotOpenRaisesTheOpensError(t *testing.T) {
	o := &IOObj{isStr: true, writable: true, dirty: []ioSpan{{0, 4}}, path: t.TempDir(), buf: []byte("data")}
	if got := catchRaise(func() { ioFlush(o) }); got != "Errno::EISDIR" {
		t.Errorf("ioFlush to a directory raised %q, want Errno::EISDIR", got)
	}
}

// TestAFlushThatCannotWriteRaises: the file opened, and the write itself was
// refused — /dev/full answers every write with ENOSPC. The error must be
// raised, not dropped while the next span is tried and Close reports success.
// Linux only: no other CI platform has /dev/full.
func TestAFlushThatCannotWriteRaises(t *testing.T) {
	if _, err := os.Stat("/dev/full"); err != nil {
		t.Skip("no /dev/full on this platform")
	}
	o := &IOObj{isStr: true, writable: true, dirty: []ioSpan{{0, 4}}, path: "/dev/full", buf: []byte("data")}
	if got := catchRaise(func() { ioFlush(o) }); got != "Errno::ENOSPC" {
		t.Errorf("ioFlush to /dev/full raised %q, want Errno::ENOSPC", got)
	}
}

// TestMarkDirtyMergesTouchingSpans: spans that overlap or touch become one;
// spans with a gap stay apart, because the gap may be another writer's.
func TestMarkDirtyMergesTouchingSpans(t *testing.T) {
	cases := []struct {
		name  string
		marks [][2]int
		want  string
	}{
		{"empty span ignored", [][2]int{{3, 3}}, "[]"},
		{"adjacent", [][2]int{{0, 2}, {2, 4}}, "[{0 4}]"},
		{"adjacent, other order", [][2]int{{2, 4}, {0, 2}}, "[{0 4}]"},
		{"overlapping", [][2]int{{0, 3}, {2, 6}}, "[{0 6}]"},
		{"contained", [][2]int{{0, 6}, {2, 3}}, "[{0 6}]"},
		{"a gap of one byte stays", [][2]int{{0, 2}, {3, 4}}, "[{0 2} {3 4}]"},
		{"a bridge joins three", [][2]int{{0, 2}, {5, 6}, {2, 5}}, "[{0 6}]"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			o := &IOObj{}
			for _, m := range c.marks {
				o.markDirty(m[0], m[1])
			}
			if got := fmt.Sprint(o.dirty); got != c.want {
				t.Errorf("dirty = %s, want %s", got, c.want)
			}
		})
	}
}

// TestMarkDirtyCoalescesPastTheBound: one span more than ioDirtyMax collapses
// the list into its union, so scattered writes cannot grow it without bound.
func TestMarkDirtyCoalescesPastTheBound(t *testing.T) {
	o := &IOObj{}
	for i := range ioDirtyMax {
		o.markDirty(10*i, 10*i+1)
	}
	if len(o.dirty) != ioDirtyMax {
		t.Fatalf("%d spans after %d separate marks, want %d", len(o.dirty), ioDirtyMax, ioDirtyMax)
	}
	o.markDirty(10*ioDirtyMax, 10*ioDirtyMax+1)
	if want := fmt.Sprint([]ioSpan{{0, 10*ioDirtyMax + 1}}); fmt.Sprint(o.dirty) != want {
		t.Errorf("dirty = %v, want %s", o.dirty, want)
	}
}

// TestClipDirtyCutsAtTheTruncation: a span past n goes, one across n is cut.
func TestClipDirtyCutsAtTheTruncation(t *testing.T) {
	o := &IOObj{dirty: []ioSpan{{0, 2}, {3, 8}, {8, 9}, {12, 14}}}
	o.clipDirty(5)
	if got, want := fmt.Sprint(o.dirty), "[{0 2} {3 5}]"; got != want {
		t.Errorf("dirty = %s, want %s", got, want)
	}
}
