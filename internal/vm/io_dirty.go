package vm

import "os"

// Pending writes on a file stream are RANGES, not the whole buffer (#974).
//
// A File here holds its whole content in buf, and a flush used to put buf back
// with os.WriteFile: truncate, then write everything. MRI's io_fflush writes
// only fptr->wbuf — the bytes this stream was asked to write, at the offsets
// it wrote them — so whatever another writer put elsewhere in the file since
// the open survives, as does a truncation another process made. Writing the
// snapshot back erased both, at every flush, every #pread with a write
// pending, and every close.
//
// So writeBytes and #pwrite's buffer fallback record the span they changed,
// in file offsets (no pushback is in buf at that moment: a write unreads
// first, see io_pushback.go), and ioFlush writes those spans and nothing else,
// without truncating. A stream opened for appending writes them with O_APPEND
// instead, so they land after whatever the file has grown to, as MRI's
// O_APPEND descriptor does.
//
// Spans are merged when they touch. Past ioDirtyMax separate spans they are
// coalesced into their union, which bounds the bookkeeping of a program that
// writes at many scattered offsets; the cost is that the bytes between them
// are written back too, which is the old behaviour confined to that range.
const ioDirtyMax = 256

// ioSpan is a half-open byte range [lo, hi) of a file.
type ioSpan struct{ lo, hi int }

// pending reports whether a write is waiting to reach the file: MRI's
// fptr->wbuf.len != 0.
func (o *IOObj) pending() bool { return len(o.dirty) > 0 }

// markDirty records that [lo, hi) of the file holds bytes not yet written.
func (o *IOObj) markDirty(lo, hi int) {
	if hi <= lo {
		return
	}
	merged := ioSpan{lo, hi}
	kept := o.dirty[:0]
	for _, s := range o.dirty {
		if s.hi < merged.lo || s.lo > merged.hi {
			kept = append(kept, s)
			continue
		}
		merged.lo, merged.hi = min(merged.lo, s.lo), max(merged.hi, s.hi)
	}
	o.dirty = append(kept, merged)
	if len(o.dirty) > ioDirtyMax {
		u := o.dirty[0]
		for _, s := range o.dirty[1:] {
			u.lo, u.hi = min(u.lo, s.lo), max(u.hi, s.hi)
		}
		o.dirty = append(o.dirty[:0], u)
	}
}

// clipDirty drops what lies at or past n: #truncate has cut the file there on
// disk, so a pending byte beyond it must not bring the length back.
func (o *IOObj) clipDirty(n int) {
	kept := o.dirty[:0]
	for _, s := range o.dirty {
		if s.lo < n {
			kept = append(kept, ioSpan{s.lo, min(s.hi, n)})
		}
	}
	o.dirty = kept
}

// writeDirty puts the pending spans of content on disk, at their offsets, or
// appended for an append-mode stream. They are written only into the file the
// stream opened (io_identity.go): when the path no longer names it, MRI's
// bytes go to the orphaned inode and nothing at the path changes, so nothing
// is written and the bytes stay in the buffer. Only a stream that recorded no
// file creates one at a missing path, as it did before.
func (o *IOObj) writeDirty(content []byte) error {
	flags := os.O_WRONLY
	if o.ident == nil {
		flags |= os.O_CREATE
	}
	if o.appendMode {
		flags |= os.O_APPEND
	}
	f, err := openOpenedFile(o.path, o.ident, flags) // errNotTheOpenedFile: ioFlush writes nothing
	if err != nil {
		return err
	}
	for _, s := range o.dirty {
		hi := min(s.hi, len(content))
		if s.lo >= hi {
			continue
		}
		if o.appendMode {
			_, err = f.Write(content[s.lo:hi])
		} else {
			_, err = f.WriteAt(content[s.lo:hi], int64(s.lo))
		}
		if err != nil {
			_ = f.Close()
			return err
		}
	}
	return f.Close()
}

// raiseFlushErr raises a failed flush as MRI's rb_io_flush_raw does, through
// rb_sys_fail_on_write: the errno the system answered, not one guessed from
// the path. ENOSPC from a full disk, EISDIR, EROFS all keep their class. On
// Windows errnoOfError has mapped the Win32 code as rb_w32_map_errno does.
func raiseFlushErr(err error, path string) {
	if eno, ok := errnoOfError(err); ok {
		if name, ok := errnoClasses[eno]; ok {
			raise("Errno::"+name, "%s @ rb_io_flush_raw - %s", errnoStrerror(eno), path)
		}
		raise("SystemCallError", "%s @ rb_io_flush_raw - %s", errnoStrerror(eno), path)
	}
	raise("Errno::EIO", "Input/output error @ rb_io_flush_raw - %s", path)
}
