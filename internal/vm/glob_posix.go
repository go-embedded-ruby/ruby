// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !windows

package vm

// globStart resolves the directory a glob walk begins in and the prefix its
// matches carry, consuming any leading absolute-root segment from segs. On
// POSIX the only absolute form is a leading empty segment (from a "/foo"
// pattern), which roots the walk at "/"; otherwise the walk starts at base
// (the cwd when base is empty) and matches are reported relative to it. The
// windows build overrides this to also recognise a drive-letter root.
func globStart(base string, segs []string) (fsDir, outPrefix string, rest []string) {
	fsDir, outPrefix = base, ""
	if fsDir == "" {
		fsDir = "."
	}
	if len(segs) > 0 && segs[0] == "" { // absolute pattern "/…"
		fsDir, outPrefix = "/", "/"
		segs = segs[1:]
	}
	return fsDir, outPrefix, segs
}
