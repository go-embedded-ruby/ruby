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
func pwriteFile(path string, data []byte, off int64) error {
	err := writeAtPath(path, data, off)
	if errors.Is(err, fs.ErrPermission) {
		err = writeAtWithOwnerWrite(path, data, off, err)
	}
	return err
}

// writeAtWithOwnerWrite retries a write refused for permission with the
// owner-write bit put back for the length of the write (withOwnerWrite): the
// stream already holds the descriptor's grant (`File.new(path, "w", 0444)`
// writes through), and reopening by path must not take it away.
func writeAtWithOwnerWrite(path string, data []byte, off int64, refused error) error {
	return withOwnerWrite(path, refused, func() error { return writeAtPath(path, data, off) })
}

func writeAtPath(path string, data []byte, off int64) error {
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	_, werr := f.WriteAt(data, off)
	cerr := f.Close()
	if werr != nil {
		return werr
	}
	return cerr
}

// ioHoleMax bounds the hole one #pwrite may open past the end of a stream's
// buffer (#973). The buffer is a byte slice, so a hole in it is memory:
// `f.pwrite("AB", 2**40)` asked for a terabyte, and 2**62 reached the
// allocator. pwrite(2) leaves a sparse hole on disk instead, and this model
// cannot. When the file was reached, the bytes are on disk and only this
// stream's in-memory copy stops short of them, the same staleness another
// writer's bytes already have. When it was not, the buffer is the only copy,
// and #pwrite raises EFBIG — the errno pwrite(2) gives for a write past the
// largest size the file may have, which this bound is for a buffer.
const ioHoleMax = 1 << 26

// mirrorPwrite copies data into the stream's buffer at off, so this stream's
// own later reads see it, growing the buffer to hold it. It reports false and
// changes nothing when that would open a hole larger than ioHoleMax.
//
// Only the hole is bounded: the bytes of data are already in memory, and the
// buffer's length is what earlier writes made it. On a 32-bit target, a
// buffer and data that together come within ioHoleMax of 2**31 could still
// overflow end; both would then have to be resident in a 4 GiB address space.
func (o *IOObj) mirrorPwrite(data []byte, off int64) bool {
	if off-int64(len(o.buf)) > ioHoleMax {
		return false
	}
	if end := int(off) + len(data); end > len(o.buf) {
		o.buf = append(o.buf, make([]byte, end-len(o.buf))...)
	}
	copy(o.buf[off:], data)
	return true
}

// raisePwriteErr reports a pwrite(2) that reached the file and was refused,
// the way rb_io_pwrite's rb_sys_fail_path does: the real errno, labelled
// with the C function. MRI 4.0.7 on darwin: `f.pwrite("AB", 2**63-1)` is
// `Errno::EFBIG: File too large @ rb_io_pwrite - <path>`.
func raisePwriteErr(err error, path string) {
	if eno, ok := errnoOfError(err); ok {
		if name, ok := errnoClasses[eno]; ok {
			raise("Errno::"+name, "%s @ rb_io_pwrite - %s", errnoStrerror(eno), path)
		}
		raise("SystemCallError", "%s @ rb_io_pwrite - %s", errnoStrerror(eno), path)
	}
	raise("Errno::EIO", "Input/output error @ rb_io_pwrite - %s", path)
}

// preadFile reads up to n bytes at off from the file at path. It returns io.EOF
// when off is at or past the end, which IO#pread raises as EOFError.
//
// What it allocates is bounded by what the file holds past off, not by n: n
// comes from Ruby, and `f.pread(2**40, 0)` must not be an allocation of 2**40
// bytes. io.ReadAll grows with what is actually read, and the section reader
// caps that at n (it clamps off+n itself when the sum would overflow).
func preadFile(path string, n int, off int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.NewSectionReader(f, off, int64(n)))
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, io.EOF
	}
	return data, nil
}
