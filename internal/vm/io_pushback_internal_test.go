// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestCompactPushbackDropsARegionTheCursorWentBackPast: every path that moves a
// file stream's cursor backwards unreads first, so this state is not reachable
// from Ruby today. It is the one a future path that forgets would create, and
// without the guard the region would be stretched over file bytes —
// [pos, ungetHi) — and those bytes dropped from the file at the next write.
func TestCompactPushbackDropsARegionTheCursorWentBackPast(t *testing.T) {
	o := &IOObj{isStr: true, path: "f", buf: []byte("01Z234"), pos: 0, ungetLo: 2, ungetHi: 3}
	if u := o.pushbackUnread(); u != 0 {
		t.Fatalf("pushbackUnread = %d, want 0", u)
	}
	if got := string(o.buf); got != "01234" || o.pos != 0 {
		t.Errorf("buf=%q pos=%d, want \"01234\" pos=0", got, o.pos)
	}
}

// TestDiskBytesDoesNotCopyWithoutPushback: diskBytes runs on every flush, so
// with no pushback it must hand back the buffer itself rather than a copy of
// the whole file.
func TestDiskBytesDoesNotCopyWithoutPushback(t *testing.T) {
	o := &IOObj{isStr: true, path: "f", buf: make([]byte, 1<<16), pos: 10}
	if n := testing.AllocsPerRun(100, func() { _ = o.diskBytes() }); n != 0 {
		t.Errorf("diskBytes allocated %v times per call, want 0", n)
	}
}
