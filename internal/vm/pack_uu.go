// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

// uuChar encodes a 6-bit value as a uuencode output character: MRI writes value
// 0 as a backtick (`) rather than the traditional space, and every other value
// as value+0x20.
func uuChar(v byte) byte {
	if v &= 0x3f; v == 0 {
		return '`'
	}
	return v + 0x20
}

// packUuencode implements Array#pack's 'u' directive (uuencoding). Each output
// line begins with a length character (the count of data bytes it encodes) and
// then the bytes in groups of three encoded as four 6-bit characters, and ends
// with a newline. The count modifier sets the bytes-per-line: '*', 0, 1, 2, or
// no count means 45, otherwise it is rounded down to a multiple of three. An
// empty input produces empty output.
func packUuencode(out []byte, d packDir, b []byte) []byte {
	length := d.count
	if length <= 2 {
		length = 45
	} else {
		length = length / 3 * 3
	}
	for len(b) > 0 {
		todo := length
		if len(b) < todo {
			todo = len(b)
		}
		out = uuEncodeLine(out, b[:todo])
		b = b[todo:]
	}
	return out
}

// uuEncodeLine appends one uuencoded line (length character, encoded triples,
// trailing newline) for the data slice s, which is at most one line's worth.
func uuEncodeLine(out, s []byte) []byte {
	out = append(out, uuChar(byte(len(s))))
	for i := 0; i < len(s); i += 3 {
		var c0, c1, c2 byte
		c0 = s[i]
		if i+1 < len(s) {
			c1 = s[i+1]
		}
		if i+2 < len(s) {
			c2 = s[i+2]
		}
		out = append(out,
			uuChar(c0>>2),
			uuChar(c0<<4|c1>>4),
			uuChar(c1<<2|c2>>6),
			uuChar(c2))
	}
	return append(out, '\n')
}

// uudecode implements String#unpack's 'u' directive (uudecoding). It reads the
// whole input, one line at a time: the first character of a line gives the
// number of data bytes it holds, the rest decode in groups of four characters
// back to three bytes (each character contributing (c-0x20)&0x3f), and newlines
// separate lines. Decoding stops at the first character that cannot begin a
// line (<= space or >= 'a').
func uudecode(data []byte) []byte {
	var out []byte
	i, n := 0, len(data)
	for i < n && data[i] > ' ' && data[i] < 'a' {
		length := int((data[i] - ' ') & 0x3f)
		i++
		for length > 0 {
			var v [4]byte
			for k := 0; k < 4; k++ {
				if i < n && data[i] != '\n' && data[i] != '\r' {
					v[k] = (data[i] - ' ') & 0x3f
					i++
				}
			}
			b := [3]byte{v[0]<<2 | v[1]>>4, v[1]<<4 | v[2]>>2, v[2]<<6 | v[3]}
			for k := 0; k < 3 && length > 0; k++ {
				out = append(out, b[k])
				length--
			}
		}
		// Consume the line's terminator (CR?LF). Anything else the outer loop's
		// range test rejects, stopping decoding — MRI does not skip trailing junk.
		if i < n && data[i] == '\r' {
			i++
		}
		if i < n && data[i] == '\n' {
			i++
		}
	}
	return out
}
