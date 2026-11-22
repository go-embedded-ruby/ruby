package vm

// Pushback on a file stream (#974).
//
// A File here holds its whole content in buf, and #ungetc / #ungetbyte splice
// the pushed-back bytes into buf ahead of the cursor so the next read returns
// them. Those bytes are not the file's: in MRI they live in the read buffer
// (fptr->rbuf) and never reach the descriptor. Two things followed from
// splicing them in without remembering where they were:
//
//   - a later buffered write flushed buf whole, so the pushed-back bytes landed
//     on disk ("getc; ungetc 'Z'; seek end; write" left a Z in the file);
//   - the cursor ignored them, where MRI's position is the descriptor's offset
//     MINUS what is still in the read buffer, so #pos and the place a write
//     lands were wrong by the number of bytes pushed back.
//
// The bytes stay spliced in buf, because every read method walks buf from pos
// and that must keep returning them first. What is added is the region
// [ungetLo, ungetHi) of buf they occupy, and the io.c operations that consume
// it:
//
//   - io_unread(fptr, discard_rbuf) is unreadPushback. Every write
//     (rb_io_check_writable), #flush, #seek, #rewind, #pos= and #truncate call it
//     with discard: the pushback is dropped and the cursor goes to the logical
//     position L = (where the cursor was in the file) − (pushback still unread).
//   - io_tell is the same with discard false, so #pos drops the pushback when L
//     is a valid offset and keeps it when L is negative — and answers L either
//     way, as rb_io_tell's "pos -= fptr->rbuf.len" does.
//
// L is negative when more bytes were pushed back than had been read. lseek
// then fails and the descriptor stays where the read-ahead left it, which is
// where a following write lands: 0 when nothing had been read, and otherwise
// end-of-file — MRI reads a file of up to 8192 bytes whole, and a larger one
// refuses that much pushback with "ungetbyte failed" (io_ungetbyte's capacity
// check), which this model does not reproduce.
//
// Only file streams are tracked. StringIO#ungetc writes into its string
// (strio_unget_bytes) and a pipe cannot seek (FMODE_DUPLEX makes io_unread a
// no-op), so both keep the plain splice.

// pushbackTracked reports whether o is a file stream whose pushback must be
// kept out of the file.
func (o *IOObj) pushbackTracked() bool {
	return o.isStr && o.path != "" && o.strObj == nil && o.pipe == nil && o.popen == nil
}

// compactPushback removes the pushed-back bytes reads have already consumed,
// leaving the unread rest (if any) starting exactly at pos. Consumed pushback
// is not file content either: left in buf, a later #seek back and #read would
// return it, and a flush would write it.
// With no region (both bounds 0) it falls through to the reset at the end.
func (o *IOObj) compactPushback() {
	if o.pos < o.ungetLo {
		// The cursor was moved back past the pushback by a path that did not
		// unread first; the bytes are still not the file's, so drop them.
		o.buf = append(o.buf[:o.ungetLo], o.buf[o.ungetHi:]...)
		o.ungetLo, o.ungetHi = 0, 0
		return
	}
	end := min(o.pos, o.ungetHi)
	if c := end - o.ungetLo; c > 0 {
		o.buf = append(o.buf[:o.ungetLo], o.buf[end:]...)
		o.pos -= c
		o.ungetHi -= c
	}
	if o.ungetHi <= o.pos {
		o.ungetLo, o.ungetHi = 0, 0
		return
	}
	o.ungetLo = o.pos
}

// pushbackUnread is the number of pushed-back bytes not yet read again — the
// part of fptr->rbuf.len this model can account for. Only unget on a tracked
// stream records a region, so an untracked one always answers 0.
func (o *IOObj) pushbackUnread() int {
	o.compactPushback()
	return o.ungetHi - o.ungetLo
}

// unreadPushback is io_unread: see the file comment.
func (o *IOObj) unreadPushback(discard bool) {
	u := o.pushbackUnread()
	if u == 0 {
		return
	}
	logical := o.pos - u
	if logical < 0 && !discard {
		return // lseek failed: io_unread keeps the read buffer
	}
	o.buf = append(o.buf[:o.pos], o.buf[o.ungetHi:]...)
	o.ungetLo, o.ungetHi = 0, 0
	switch {
	case logical >= 0:
		o.pos = logical
	case o.pos > 0:
		o.pos = len(o.buf) // the read-ahead reached end-of-file
	}
}

// tellPos is rb_io_tell: the logical position, which is negative when more
// was pushed back than read.
func (o *IOObj) tellPos() int {
	o.unreadPushback(false)
	return o.pos - o.pushbackUnread()
}

// diskBytes is the stream's content as the file holds it: buf without the
// pushed-back bytes. It copies only when there are some.
func (o *IOObj) diskBytes() []byte {
	u := o.pushbackUnread()
	if u == 0 {
		return o.buf
	}
	out := make([]byte, 0, len(o.buf)-u)
	out = append(out, o.buf[:o.ungetLo]...)
	return append(out, o.buf[o.ungetHi:]...)
}

// unget splices p in ahead of the cursor, recording the region on a tracked
// stream.
func (o *IOObj) unget(p []byte) {
	if len(p) == 0 {
		return
	}
	if o.strObj != nil {
		o.strioUngetBytes(p)
		return
	}
	tracked := o.pushbackTracked()
	if tracked {
		o.compactPushback()
		if o.ungetHi <= o.ungetLo {
			o.ungetLo, o.ungetHi = o.pos, o.pos
		}
	}
	out := make([]byte, 0, len(o.buf)+len(p))
	out = append(out, o.buf[:o.pos]...)
	out = append(out, p...)
	out = append(out, o.buf[o.pos:]...)
	o.buf = out
	if tracked {
		o.ungetHi += len(p)
	}
}

// strioUngetBytes is ext/stringio strio_unget_bytes: a StringIO's pushback is
// written INTO its string, over the bytes just read, and the position moves
// back by its length. Only when more is pushed back than the position allows
// does the string grow, the unread rest moving right to make room. Splicing
// the bytes in instead — what this did for StringIO before #974 — left the
// string one character longer and the position unchanged:
// StringIO.new("0123") getc, ungetc "Z", write "AB" gave "0AB23" at pos 1,
// where MRI has "AB23" at pos 0.
func (o *IOObj) strioUngetBytes(cp []byte) {
	cl, pos, n := len(cp), o.pos, len(o.buf)
	rest := pos - n
	if cl > pos {
		ex := cl - n
		if rest < 0 {
			ex = cl - pos
		}
		grown := make([]byte, n+ex)
		copy(grown, o.buf)
		if rest < 0 {
			copy(grown[cl:], o.buf[pos:])
		}
		o.buf, pos = grown, 0
	} else {
		if rest > 0 { // pos past the end: the gap is NUL-filled
			o.buf = append(o.buf, make([]byte, rest)...)
		}
		pos -= cl
	}
	copy(o.buf[pos:], cp)
	o.pos = pos
	o.syncStr()
}
