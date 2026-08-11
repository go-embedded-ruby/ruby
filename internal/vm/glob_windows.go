// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build windows

package vm

// globStart is the Windows form of the POSIX seam (see glob_posix.go). Besides
// the POSIX "/…" root it recognises a drive-letter root such as "C:/foo", so an
// absolute Windows pattern globs from that drive's root rather than being taken
// for a relative name under base. Only forward-slash patterns are treated as
// separators, matching MRI on Windows, where a backslash in a glob pattern is an
// escape (a "C:\\dir\\*" pattern matches nothing, exactly as ruby reports) — so
// Ruby callers use forward slashes and this handles the "C:/dir/*" they pass.
func globStart(base string, segs []string) (fsDir, outPrefix string, rest []string) {
	fsDir, outPrefix = base, ""
	if fsDir == "" {
		fsDir = "."
	}
	switch {
	case len(segs) > 0 && segs[0] == "": // "/…" — root of the current drive
		fsDir, outPrefix = "/", "/"
		segs = segs[1:]
	case len(segs) > 0 && isDriveRoot(segs[0]): // "C:/…" — drive-absolute
		fsDir, outPrefix = segs[0]+"/", segs[0]+"/"
		segs = segs[1:]
	}
	return fsDir, outPrefix, segs
}

// isDriveRoot reports whether s is a bare drive designator like "C:" — a
// single ASCII letter followed by a colon.
func isDriveRoot(s string) bool {
	return len(s) == 2 && s[1] == ':' &&
		((s[0] >= 'A' && s[0] <= 'Z') || (s[0] >= 'a' && s[0] <= 'z'))
}
