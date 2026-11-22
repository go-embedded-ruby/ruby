// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"
	"os"
)

// The file a stream opened (#974, its TOCTOU item).
//
// A File here holds no descriptor: flushes, #pwrite and #truncate reopen the
// stream's PATH. MRI writes through the descriptor it opened, so what the path
// names afterwards is not its concern: a file unlinked while open keeps
// receiving the stream's bytes on its orphaned inode, and a file renamed over
// the path or a symlink put in its place is never touched. Reopening by path
// did touch it. With the path swapped for a symlink to another file between
// the open and the flush, the stream's pending bytes were written into that
// other file; with the file unlinked, the flush created a new one in its
// place.
//
// So a file stream records the identity of the file it opened (ident, from
// os.Stat right after the open), and every write that goes back through the
// path checks it twice: on the path before opening it, so that a fifo or a
// device put in its place is never opened (an open for writing on a fifo
// blocks until a reader comes), and on the descriptor after, so that a rename
// between the two is caught. When the path no longer names that file, nothing
// is written there: the bytes stay in the stream's buffer, where its own reads
// still see them, which is what MRI's orphaned inode gives at the path.
//
// A stream with no regular file behind it (StringIO, a device, a fifo, IO.new
// on a descriptor) records nothing, and keeps the behaviour it had.
//
// The check on the path and the open are two system calls, so a fifo swapped
// in between them can still make the open block. Closing that window needs a
// non-blocking open, which is a per-platform flag; the descriptor check after
// the open already keeps such a file from being written.

// errNotTheOpenedFile reports that a stream's path no longer names the file the
// stream opened: unlinked, renamed over, or replaced by something else.
var errNotTheOpenedFile = errors.New("the path no longer names the file this stream opened")

// betweenCheckAndOpen, when set, runs in openOpenedFile between the check on
// the path and the open: the window a rename can use. Only tests set it, to
// replace the file there and assert that the check on the descriptor catches it.
var betweenCheckAndOpen func(path string)

// rememberFile records the identity of the regular file at o.path, which has
// just been opened.
func (o *IOObj) rememberFile() {
	if st, err := os.Stat(o.path); err == nil && st.Mode().IsRegular() {
		// On Windows a FileInfo from os.Stat fetches its file ID lazily, BY
		// PATH, at its first os.SameFile; fetched later it would be the ID of
		// whatever the path names then, and every replacement would compare
		// equal. Comparing it with itself now fetches and caches it while the
		// path still names the opened file. Elsewhere the ID is already there.
		_ = os.SameFile(st, st)
		o.ident = st
	}
}

// namesOpenedFile reports whether st is the file this stream opened. A stream
// that recorded none accepts any.
func (o *IOObj) namesOpenedFile(st os.FileInfo) bool {
	return o.ident == nil || os.SameFile(o.ident, st)
}

// truncateOpenedFile is #truncate's ftruncate(2), reached by path. When the
// path no longer names the file the stream opened, nothing is truncated: MRI's
// ftruncate changes only the orphaned inode. With no file recorded it is a
// plain truncate of the path.
func (o *IOObj) truncateOpenedFile(n int64) error {
	f, err := openOpenedFile(o.path, o.ident, os.O_WRONLY)
	if errors.Is(err, errNotTheOpenedFile) {
		return nil
	}
	if err != nil {
		return err
	}
	terr := f.Truncate(n)
	cerr := f.Close()
	if terr != nil {
		return terr
	}
	return cerr
}

// openOpenedFile opens path with flag only while it still names ident, the
// file a stream opened; see the file comment. With ident nil it is a plain
// open. A path that is gone or names another file is errNotTheOpenedFile.
func openOpenedFile(path string, ident os.FileInfo, flag int) (*os.File, error) {
	if ident != nil {
		st, err := os.Stat(path)
		if err != nil || !os.SameFile(ident, st) {
			return nil, errNotTheOpenedFile
		}
	}
	if betweenCheckAndOpen != nil {
		betweenCheckAndOpen(path)
	}
	f, err := os.OpenFile(path, flag, 0o644)
	if err != nil {
		return nil, err
	}
	if ident != nil {
		st, err := f.Stat()
		if err != nil || !os.SameFile(ident, st) {
			_ = f.Close()
			return nil, errNotTheOpenedFile
		}
	}
	return f, nil
}
