package vm_test

import (
	"strings"
	"testing"
)

// TestEncodeResiduals covers the String#encode conformance gaps closed against
// MRI Ruby 4.0.6: the :fallback #to_str / validation rules, xml: numeric
// character references for undefined characters, the IBM437 codec, transcoding
// to Encoding.default_internal, the ASCII-compatible no-converter shortcut, and
// String#encode!'s FrozenError. Every expectation was cross-checked against MRI.
func TestEncodeResiduals(t *testing.T) {
	cases := []struct{ src, want string }{
		// :fallback converts a non-String result through #to_str (never #to_s).
		{
			`obj = Object.new; def obj.to_str; "bar"; end
			 print "B�".encode("US-ASCII", fallback: {"�" => obj})`,
			"Bbar",
		},
		// A #[]-responding object is passed the unrepresentable character.
		{`print "B�".encode("US-ASCII", fallback: proc { |c| c.bytes.inspect })`, "B[239, 191, 189]"},
		// xml: :text / :attr render an undefined character as an upper-case NCR.
		{`print "ürst".encode("US-ASCII", xml: :text)`, "&#xFC;rst"},
		{`print "ürst".encode("US-ASCII", xml: :attr)`, `"&#xFC;rst"`},
		// IBM437 now has a codec, in both directions.
		{`print "あ".encode("euc-jp", "ibm437").bytes.inspect`, "[166, 208, 143, 171, 228, 143, 171, 177]"},
		{`print "aéb".encode("IBM437").bytes.inspect`, "[97, 130, 98]"},
		// A 7-bit ASCII String transcodes to a converter-less ASCII-compatible
		// destination as a byte-identical copy.
		{`s = "\x79".b.encode("Emacs-Mule"); print s; print s.encoding.name`, "yEmacs-Mule"},
		// With no destination, #encode transcodes to Encoding.default_internal.
		{`Encoding.default_internal = Encoding::UTF_8
		  print [0xA4, 0xA2].pack("CC").force_encoding("EUC-JP").encode
		  Encoding.default_internal = nil`, "あ"},
		// An encoding argument answering #to_str is honoured.
		{`class EncName; def to_str; "utf-8"; end; end
		  print [0xA4, 0xA2].pack("CC").force_encoding("EUC-JP").encode(EncName.new)`, "あ"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}

	errCases := []struct{ src, substr string }{
		// A fallback substitute unrepresentable in the target is rejected.
		{`"B�".encode("US-ASCII", fallback: {"�" => "￮"})`, "too big fallback string"},
		{`"B�".encode("US-ASCII", fallback: -> c { "￮" })`, "too big fallback string"},
		// A fallback object with no #[] is no fallback: the normal undefined error.
		{`"B�".encode("US-ASCII", fallback: Object.new)`, "U+FFFD from UTF-8 to US-ASCII"},
		// An unregistered destination is a missing converter, not an unknown name.
		{`"abc".encode("xyz")`, "code converter not found (UTF-8 to xyz)"},
		// A registered but converter-less destination with non-ASCII data has no
		// converter either.
		{`[0x80].pack("C").force_encoding("BINARY").encode("Emacs-Mule")`, "code converter not found (ASCII-8BIT to Emacs-Mule)"},
		// #encode! refuses a frozen receiver, even for a no-op transcoding.
		{`"foo".freeze.encode!("utf-8")`, "FrozenError"},
		{`"foo".freeze.encode!("euc-jp")`, "FrozenError"},
	}
	for _, c := range errCases {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.substr) {
			t.Errorf("src=%q err=%v, want containing %q", c.src, err, c.substr)
		}
	}
}
