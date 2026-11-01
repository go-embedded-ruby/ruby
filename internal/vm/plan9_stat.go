// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "strings"

// plan9StatFields builds the stat numbers File::Stat reports from the Dir fields
// a Plan 9 stat carries (syscall.Dir: a server Type and Dev, and a Qid whose Path
// is unique within the server). Plan 9 has no inode, device or link-count model of
// the POSIX kind, so:
//
//   - ino is the Qid path, the server's unique id for the file;
//   - dev packs the server type (high 32 bits) over its subtype (low 32 bits);
//   - nlink is 1 (there are no hard links);
//   - uid and gid are 0: owners are NAMES (Dir.Uid, Dir.Gid), compared by name in
//     filestat_plan9.go's statOwned and statGrpowned rather than by number;
//   - blksize, rdev and blocks are 0, which File::Stat reports as such.
//
// It takes plain integers so the mapping is tested on every platform; only
// filestat_plan9.go (GOOS=plan9) has a *syscall.Dir to read them from.
func plan9StatFields(qidPath uint64, typ uint16, dev uint32) statFields {
	return statFields{
		ino:    int64(qidPath),
		dev:    int64(typ)<<32 | int64(dev),
		nlink:  1,
		hasSys: true,
	}
}

// plan9Owner reports whether a Dir owner name is the current user. /dev/user holds
// the user name with no newline; an empty name on either side is never a match, so
// a failed read of /dev/user reports "not owned" rather than "owned".
func plan9Owner(owner, user string) bool {
	user = strings.TrimSpace(user)
	return owner != "" && owner == user
}
