// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build unix

package vm

import (
	"errors"
	"io/fs"
	"strings"
	"syscall"
	"testing"
)

// TestRaisePwriteErrKeepsTheErrno: a pwrite(2) the file refused is raised with
// its own errno (#973), an errno with no class as SystemCallError, and an error
// that carries no errno at all as EIO, all labelled rb_io_pwrite. ENOSPC and
// EIO used to be swallowed: #pwrite put the bytes in the buffer and returned
// their count as if they were on disk.
func TestRaisePwriteErrKeepsTheErrno(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"a known errno", &fs.PathError{Op: "write", Path: "p", Err: syscall.ENOSPC}, "Errno::ENOSPC"},
		{"an errno with no class", syscall.Errno(0x7ffe), "SystemCallError"},
		{"no errno at all", errors.New("short write"), "Errno::EIO"},
	}
	for _, c := range cases {
		re := rubyErr(t, func() { raisePwriteErr(c.err, "p") })
		if re.Class != c.want || !strings.Contains(re.Message, "@ rb_io_pwrite - p") {
			t.Errorf("%s: got %s %q, want %s labelled rb_io_pwrite", c.name, re.Class, re.Message, c.want)
		}
	}
}
