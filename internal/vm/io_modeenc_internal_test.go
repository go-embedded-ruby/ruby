// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"fmt"
	"testing"
)

// ioFdProg wraps a Ruby body with a tmpdir that exposes three live synthetic
// descriptors: `wo` (write-only), `ro` (read-only, pre-filled), and `rw`
// (read+write). Used to drive IO.new / IO.open / IO.for_fd mode-and-encoding
// resolution against real fds.
func ioFdProg(body string) string {
	return `require "tmpdir"
Dir.mktmpdir do |d|
  File.write(File.join(d, "ro"), "read-data")
  wo = File.open(File.join(d, "wo"), "w").fileno
  ro = File.open(File.join(d, "ro"), "r").fileno
  rw = File.open(File.join(d, "rw"), "w+").fileno
  ` + body + `
end`
}

// TestIOResolveModeEnc covers ioResolveModeEnc and its helpers (resolveVmode,
// flagsModeSpec, parseModeString, parseEncPart) via IO.new: integer/string mode
// coercion, the fopen mode grammar, and the ":ext:int" encoding suffix. Asserted
// against MRI Ruby 4.0.6.
func TestIOResolveModeEnc(t *testing.T) {
	cases := []struct{ body, want string }{
		// Integer open-flag modes (flagsModeSpec): WRONLY, RDONLY, RDWR, APPEND.
		{`p IO.new(wo, File::WRONLY).instance_of?(IO)`, "true\n"},
		{`p IO.new(ro, File::RDONLY).instance_of?(IO)`, "true\n"},
		{`p IO.new(rw, File::RDWR).instance_of?(IO)`, "true\n"},
		{`p IO.new(wo, File::WRONLY | File::APPEND).instance_of?(IO)`, "true\n"},
		// #to_int coercion of the mode argument.
		{`m = Object.new; def m.to_int; File::WRONLY; end; p IO.new(wo, m).instance_of?(IO)`, "true\n"},
		// #to_str coercion of the mode argument.
		{`m = Object.new; def m.to_str; "w"; end; p IO.new(wo, m).instance_of?(IO)`, "true\n"},
		// :mode option alone (no positional mode).
		{`p IO.new(wo, mode: "w").instance_of?(IO)`, "true\n"},
		// The fopen mode grammar: "+", "x" (exclusive flag, no fmode effect), append.
		{`p IO.new(rw, "w+").instance_of?(IO)`, "true\n"},
		{`p IO.new(wo, "wx").instance_of?(IO)`, "true\n"},
		{`p IO.new(wo, "a").instance_of?(IO)`, "true\n"},
		// Encoding suffix: ext only, ext:int, "-" internal, and int==ext collapse.
		{`io = IO.new(wo, "w:utf-8"); p io.external_encoding.to_s`, "\"UTF-8\"\n"},
		{`io = IO.new(wo, "w:utf-8:-"); p io.internal_encoding.to_s`, "\"\"\n"},
		{`io = IO.new(wo, "w:utf-8:utf-8"); p io.internal_encoding.to_s`, "\"\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, ioFdProg(c.body)); got != c.want {
			t.Errorf("body=%q got=%q want=%q", c.body, got, c.want)
		}
	}
	// TypeError for a mode that is neither Integer- nor String-coercible.
	if cls, _ := evalErr(t, ioFdProg(`IO.new(wo, Object.new)`)); cls != "TypeError" {
		t.Errorf("non-coercible mode: got %q want TypeError", cls)
	}
	// Malformed mode strings raise ArgumentError (empty base, bad first char, bad
	// flag char).
	for _, body := range []string{`IO.new(wo, "")`, `IO.new(wo, "z")`, `IO.new(wo, "wq")`} {
		if cls, _ := evalErr(t, ioFdProg(body)); cls != "ArgumentError" {
			t.Errorf("body=%q: got %q want ArgumentError", body, cls)
		}
	}
}

// TestIOModeEncErrors covers the ArgumentError conflicts ioResolveModeEnc raises:
// mode/encoding specified twice and the binmode/textmode clashes (extractBinmode),
// plus the b/t clash inside a single mode string.
func TestIOModeEncErrors(t *testing.T) {
	cases := []struct{ body, msg string }{
		{`IO.new(wo, "w", mode: "w")`, "mode specified twice"},
		{`IO.new(wo, "w:utf-8", encoding: "utf-8")`, "encoding specified twice"},
		{`IO.new(wo, "wt", textmode: true)`, "textmode specified twice"},
		{`IO.new(wo, "wb", binmode: true)`, "binmode specified twice"},
		{`IO.new(wo, "wb", textmode: true)`, "both textmode and binmode specified"},
		{`IO.new(wo, "wt", binmode: true)`, "both textmode and binmode specified"},
		{`IO.new(wo, "w", textmode: true, binmode: true)`, "both textmode and binmode specified"},
		{`IO.new(wo, "wbt")`, "invalid access mode wbt"},
		{`IO.new(wo, "wtb")`, "invalid access mode wtb"},
	}
	for _, c := range cases {
		cls, msg := evalErr(t, ioFdProg(c.body))
		if cls != "ArgumentError" || msg != c.msg {
			t.Errorf("body=%q: got %s/%q want ArgumentError/%q", c.body, cls, msg, c.msg)
		}
	}
}

// TestIOBinmodeTextmodeAutoclose covers the :binmode / :textmode / :autoclose
// options and the #binmode / #binmode? / #autoclose? descriptor state they drive.
func TestIOBinmodeTextmodeAutoclose(t *testing.T) {
	cases := []struct{ body, want string }{
		// binmode from a mode string and from the :binmode option; the default false.
		{`io = IO.new(wo, "wb"); p [io.binmode?, io.external_encoding.to_s]`, "[true, \"ASCII-8BIT\"]\n"},
		{`io = IO.new(wo, "w", binmode: true); p io.binmode?`, "true\n"},
		{`p IO.new(wo, "w").binmode?`, "false\n"},
		// A present-but-falsy :binmode / :textmode does not set the flag.
		{`p IO.new(wo, "w", binmode: false).binmode?`, "false\n"},
		{`p IO.new(wo, "w", textmode: false).instance_of?(IO)`, "true\n"},
		{`p IO.new(wo, "w", textmode: true).instance_of?(IO)`, "true\n"},
		// #binmode marks the stream binary and sets ASCII-8BIT.
		{`io = IO.new(wo, "w"); io.binmode; p [io.binmode?, io.external_encoding.to_s]`, "[true, \"ASCII-8BIT\"]\n"},
		// :autoclose option and the #autoclose? / #autoclose= accessors.
		{`p IO.new(wo, "w").autoclose?`, "true\n"},
		{`p IO.new(wo, "w", autoclose: false).autoclose?`, "false\n"},
		{`p IO.new(wo, "w", autoclose: 42).autoclose?`, "true\n"},
		{`io = IO.new(wo, "w"); io.autoclose = false; a = io.autoclose?; io.autoclose = true; p [a, io.autoclose?]`, "[false, true]\n"},
		// StringIO#binmode sets the binary flag too (and returns self).
		{`io = StringIO.new("x"); p io.binmode.class`, "StringIO\n"},
	}
	for _, c := range cases {
		if got := eval(t, ioFdProg(c.body)); got != c.want {
			t.Errorf("body=%q got=%q want=%q", c.body, got, c.want)
		}
	}
	// #binmode and #binmode? raise IOError on a closed stream.
	for _, body := range []string{
		`io = IO.new(wo, "w"); io.close; io.binmode`,
		`io = IO.new(wo, "w"); io.close; io.binmode?`,
	} {
		cls, msg := evalErr(t, ioFdProg(body))
		if cls != "IOError" || msg != "closed stream" {
			t.Errorf("body=%q: got %s/%q want IOError/closed stream", body, cls, msg)
		}
	}
}

// TestIOEncodingOptions covers extractEncodingOption / isDashString / rbWarn: the
// :external_encoding / :internal_encoding / :encoding options, the nil and "-"
// internal-encoding forms, the int==ext collapse, and the "Ignoring encoding
// parameter" override (whose external/internal wording is exercised both with
// $VERBOSE set, so the warning fires, and unset, so rbWarn early-returns).
func TestIOEncodingOptions(t *testing.T) {
	cases := []struct{ body, want string }{
		{`io = IO.new(wo, "w", external_encoding: "utf-8", internal_encoding: "ibm866"); p [io.external_encoding.to_s, io.internal_encoding.to_s]`, "[\"UTF-8\", \"IBM866\"]\n"},
		{`io = IO.new(wo, "w", internal_encoding: "-"); p io.internal_encoding.to_s`, "\"\"\n"},
		{`io = IO.new(wo, "w", internal_encoding: nil); p io.internal_encoding.to_s`, "\"\"\n"},
		// A nil :encoding / :external_encoding is treated as absent (not an error).
		{`p IO.new(wo, "w", encoding: nil).instance_of?(IO)`, "true\n"},
		{`p IO.new(wo, "w", external_encoding: nil).instance_of?(IO)`, "true\n"},
		{`io = IO.new(wo, "w", external_encoding: "utf-8", internal_encoding: "utf-8"); p io.internal_encoding.to_s`, "\"\"\n"},
		{`io = IO.new(wo, "w", encoding: "utf-8:utf-8"); p io.internal_encoding.to_s`, "\"\"\n"},
		// :encoding is ignored when :external_encoding is present (external wording).
		{`io = IO.new(wo, "w", external_encoding: "ibm866", encoding: "utf-8"); p io.external_encoding.to_s`, "\"IBM866\"\n"},
		{`$VERBOSE = true; io = IO.new(wo, "w", external_encoding: "ibm866", encoding: "utf-8"); p io.external_encoding.to_s`, "Ignoring encoding parameter 'utf-8': external_encoding is used\n\"IBM866\"\n"},
		// :encoding is ignored when :internal_encoding is present (internal wording).
		{`$VERBOSE = true; io = IO.new(wo, "w", internal_encoding: "ibm866", encoding: "utf-8"); p io.internal_encoding.to_s`, "Ignoring encoding parameter 'utf-8': internal_encoding is used\n\"IBM866\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, ioFdProg(c.body)); got != c.want {
			t.Errorf("body=%q got=%q want=%q", c.body, got, c.want)
		}
	}
}

// TestIOForFdModeCompat covers the descriptor-compatibility logic in forFd: an
// explicit mode incompatible with the fd's current access half raises
// Errno::EINVAL; no explicit mode inherits the fd's half; and a closed fd raises
// IOError.
func TestIOForFdModeCompat(t *testing.T) {
	// Explicit read on a write-only fd, and explicit write on a read-only fd, EINVAL.
	for _, body := range []string{`IO.new(wo, "r")`, `IO.new(ro, "w")`} {
		if cls, _ := evalErr(t, ioFdProg(body)); cls != "Errno::EINVAL" {
			t.Errorf("body=%q: got %q want Errno::EINVAL", body, cls)
		}
	}
	// No explicit mode inherits the descriptor's half: a wrapper of a write-only fd
	// writes but is not readable.
	if got := eval(t, ioFdProg(`io = IO.new(wo); n = io.write("hi"); r = (io.read rescue $!.message); p [n, r]`)); got != "[2, \"not opened for reading\"]\n" {
		t.Errorf("inherit write-only: got %q", got)
	}
	// A closed descriptor raises IOError when re-wrapped.
	cls, msg := evalErr(t, `require "tmpdir"
Dir.mktmpdir do |d|
  f = File.open(File.join(d, "c"), "w")
  fd = f.fileno
  f.close
  IO.new(fd, "w")
end`)
	if cls != "IOError" || msg != "closed stream" {
		t.Errorf("closed fd: got %s/%q want IOError/closed stream", cls, msg)
	}
}

// TestIOOpenBlock covers IO.open's block form and its ensure-close propagation
// (recoverAny): the block value is returned, #close runs even when the block
// raises, a #close exception propagates, a block exception takes precedence over
// a #close exception, and an IOError "closed stream" from #close is swallowed.
func TestIOOpenBlock(t *testing.T) {
	cases := []struct{ body, want string }{
		// Block value is returned; no-block form returns the IO.
		{`p IO.open(wo, "w") { |io| 42 }`, "42\n"},
		{`p IO.open(wo, "w").instance_of?(IO)`, "true\n"},
		// #close runs even when the block raises.
		{`$ran = false; (IO.open(wo, "w") { |io| def io.close; $ran = true; end; raise "blk" } rescue nil); p $ran`, "true\n"},
		// A "closed stream" IOError from #close is swallowed; the block value returns.
		{`p(IO.open(wo, "w") { |io| def io.close; raise IOError, "closed stream"; end; 7 })`, "7\n"},
	}
	for _, c := range cases {
		if got := eval(t, ioFdProg(c.body)); got != c.want {
			t.Errorf("body=%q got=%q want=%q", c.body, got, c.want)
		}
	}
	// A non-"closed stream" #close exception propagates.
	if cls, msg := evalErr(t, ioFdProg(`IO.open(wo, "w") { |io| def io.close; raise "boom"; end }`)); cls != "RuntimeError" || msg != "boom" {
		t.Errorf("close raise: got %s/%q want RuntimeError/boom", cls, msg)
	}
	// The block's exception takes precedence over a #close exception.
	if cls, msg := evalErr(t, ioFdProg(`IO.open(wo, "w") { |io| def io.close; raise "fromclose"; end; raise "fromblock" }`)); cls != "RuntimeError" || msg != "fromblock" {
		t.Errorf("block precedence: got %s/%q want RuntimeError/fromblock", cls, msg)
	}
}

// TestIORbWarnSilentByDefault confirms rbWarn's $VERBOSE gate: with $VERBOSE nil
// (the default) the ignored-encoding override is silent, yet still resolves the
// external encoding to the winning option.
func TestIORbWarnSilentByDefault(t *testing.T) {
	got := eval(t, ioFdProg(`io = IO.new(wo, "w", external_encoding: "ibm866", encoding: "utf-8"); p [$VERBOSE, io.external_encoding.to_s]`))
	if want := fmt.Sprintf("[nil, %q]\n", "IBM866"); got != want {
		t.Errorf("silent warn: got %q want %q", got, want)
	}
}
