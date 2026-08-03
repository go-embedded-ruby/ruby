package vm

import (
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
)

// xtextEncodings maps rbgo canonical encoding names to the pure-Go
// golang.org/x/text codec that transcodes them, for the encodings beyond the
// core set (UTF-*/ASCII/Latin-1) that String#encode handles directly. Encodings
// absent here are a named residual: String#encode raises
// Encoding::ConverterNotFoundError for them.
var xtextEncodings = map[string]encoding.Encoding{
	"ISO-8859-2":   charmap.ISO8859_2,
	"ISO-8859-3":   charmap.ISO8859_3,
	"ISO-8859-4":   charmap.ISO8859_4,
	"ISO-8859-5":   charmap.ISO8859_5,
	"ISO-8859-6":   charmap.ISO8859_6,
	"ISO-8859-7":   charmap.ISO8859_7,
	"ISO-8859-8":   charmap.ISO8859_8,
	"ISO-8859-9":   charmap.ISO8859_9,
	"ISO-8859-10":  charmap.ISO8859_10,
	"ISO-8859-13":  charmap.ISO8859_13,
	"ISO-8859-14":  charmap.ISO8859_14,
	"ISO-8859-15":  charmap.ISO8859_15,
	"ISO-8859-16":  charmap.ISO8859_16,
	"KOI8-R":       charmap.KOI8R,
	"KOI8-U":       charmap.KOI8U,
	"Windows-1250": charmap.Windows1250,
	"Windows-1251": charmap.Windows1251,
	"Windows-1252": charmap.Windows1252,
	"Windows-1253": charmap.Windows1253,
	"Windows-1254": charmap.Windows1254,
	"Windows-1255": charmap.Windows1255,
	"Windows-1256": charmap.Windows1256,
	"Windows-1257": charmap.Windows1257,
	"Windows-1258": charmap.Windows1258,
	"Windows-874":  charmap.Windows874,
	"Windows-31J":  japanese.ShiftJIS,
	"Shift_JIS":    japanese.ShiftJIS,
	"EUC-JP":       japanese.EUCJP,
	"EUC-KR":       korean.EUCKR,
	"GBK":          simplifiedchinese.GBK,
	"GB18030":      simplifiedchinese.GB18030,
	"Big5":         traditionalchinese.Big5,
	"macRoman":     charmap.Macintosh,
}

// xtextDecode decodes src (in encoding `from`) to a UTF-8 string using x/text.
// ok is false when `from` has no x/text codec.
func xtextDecode(src []byte, from string) (string, bool) {
	enc, ok := xtextEncodings[from]
	if !ok {
		return "", false
	}
	// x/text's decoders substitute undecodable input with U+FFFD rather than
	// erroring, so a byte sequence that is invalid in `from` becomes U+FFFD in the
	// UTF-8 intermediate (a documented residual: MRI would raise without :invalid).
	out, _ := enc.NewDecoder().Bytes(src)
	return string(out), true
}

// xtextEncode encodes the UTF-8 string u into encoding `to` using x/text. ok is
// false when `to` has no x/text codec. A character absent from the target raises
// Encoding::UndefinedConversionError unless undefReplace substitutes it.
func xtextEncode(u, to string, undefReplace bool, repl string) ([]byte, bool) {
	enc, ok := xtextEncodings[to]
	if !ok {
		return nil, false
	}
	if undefReplace {
		// ReplaceUnsupported substitutes unrepresentable runes with SUB (0x1A) and
		// never errors; swap SUB for the requested replacement string afterwards.
		out, _ := encoding.ReplaceUnsupported(enc.NewEncoder()).Bytes([]byte(u))
		return replaceBytes(out, 0x1a, repl), true
	}
	out, err := enc.NewEncoder().Bytes([]byte(u))
	if err != nil {
		raise("Encoding::UndefinedConversionError", "from UTF-8 to %s", to)
	}
	return out, true
}

// replaceBytes replaces every occurrence of the single byte b in s with repl.
func replaceBytes(s []byte, b byte, repl string) []byte {
	out := make([]byte, 0, len(s))
	for _, c := range s {
		if c == b {
			out = append(out, repl...)
		} else {
			out = append(out, c)
		}
	}
	return out
}
