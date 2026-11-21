// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"
	"io"
	"io/fs"
	"os"
)

// Positional I/O on the FILE, not on the stream's buffer (#949).
//
// MRI's IO#pwrite and IO#pread are pwrite(2) and pread(2) on the descriptor:
// they neither read nor move the stream's buffer or position. Measured on MRI
// 4.0.7, a file holding "0123456789":
//
//	f.pwrite("AB", 0); File.read(path)        # "AB23456789", at once
//	f.write("QQ"); f.pwrite("AB", 5)          # disk "01234AB789": the buffered
//	                                          # "QQ" is NOT pushed out with it
//	(another writer changes the file); f.pread(3, 0)   # sees the new bytes
//	f.write("QQ"); f.pread(2, 0)              # "QQ": rb_io_pread flushes wbuf first
//	f.pwrite("Z", 12)                         # "0123456789\0\0Z": a hole
//
// A File stream here is buffer-backed and holds no descriptor of its own, so it
// reaches the file by its path. Two limits come with that and are this model's,
// not new: a path that has since been unlinked or replaced is not the file the
// stream opened (the same holds for ioFlush, which writes the whole buffer back
// by path), and O_APPEND's effect on pwrite(2) — Linux appends whatever the
// offset — is not reproduced. When the path cannot be reached, both methods
// fall back to the buffer, which is what they did before.

// ioPositionalPath reports the regular file a positional call on o can reach,
// or false for a stream that has none: a StringIO, a pipe end, a popen stream, a
// device or fifo, or a path that no longer resolves.
func ioPositionalPath(o *IOObj) (string, bool) {
	if o.pipe != nil || o.popen != nil {
		return "", false
	}
	st, err := os.Stat(o.path) // "" (a StringIO) does not resolve either
	if err != nil || !st.Mode().IsRegular() {
		return "", false
	}
	return o.path, true
}

// pwriteFile writes data at off in the file at path and touches nothing else:
// no other byte of the file, and nothing of the stream that opened it. A write
// past the end leaves a hole, as pwrite(2) does.
func pwriteFile(path string, data []byte, off int) error {
	err := writeAtPath(path, data, off)
	if errors.Is(err, fs.ErrPermission) {
		err = writeAtWithOwnerWrite(path, data, off, err)
	}
	return err
}

// writeAtWithOwnerWrite retries a write refused for permission with the
// owner-write bit put back for the length of the write, for the reason ioFlush
// gives: the stream already holds the descriptor's grant
// (`File.new(path, "w", 0444)` writes through), and reopening by path must not
// take it away. It never widens the file's mode beyond the write. When the
// mode cannot be read or changed, the original refusal stands.
func writeAtWithOwnerWrite(path string, data []byte, off int, refused error) error {
	st, err := os.Stat(path)
	if err != nil {
		return refused
	}
	mode := st.Mode().Perm()
	if os.Chmod(path, mode|0o200) != nil {
		return refused
	}
	err = writeAtPath(path, data, off)
	_ = os.Chmod(path, mode)
	return err
}

func writeAtPath(path string, data []byte, off int) error {
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	_, werr := f.WriteAt(data, int64(off))
	cerr := f.Close()
	if werr != nil {
		return werr
	}
	return cerr
}

// preadFile reads up to n bytes at off from the file at path. It returns io.EOF
// when off is at or past the end, which IO#pread raises as EOFError.
//
// What it allocates is bounded by what the file holds past off, not by n: n
// comes from Ruby, and `f.pread(2**40, 0)` must not be an allocation of 2**40
// bytes. io.ReadAll grows with what is actually read, and the section reader
// caps that at n (it clamps off+n itself when the sum would overflow).
func preadFile(path string, n, off int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.NewSectionReader(f, int64(off), int64(n)))
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, io.EOF
	}
	return data, nil
}
