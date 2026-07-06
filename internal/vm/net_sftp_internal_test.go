// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"fmt"
	"strings"
	"testing"

	sftp "github.com/go-ruby-net-sftp/net-sftp"
)

// This is the internal test suite for the Net::SFTP binding (require "net/sftp").
// SFTP runs over an SSH channel; rbgo injects that channel as a Ruby duck
// responding to #write / #read, so the tests drive the binding over an in-process
// canned channel (the Chan class) whose reply stream is built here with the very
// same wire codec the binding parses. Every response frame is produced by the
// go-ruby-net-sftp Writer / FramePacket, so the suite exercises the full driver
// (init/version negotiation, request framing, response correlation) without any
// SSH, network, or goroutine — the round-trip is fully deterministic.

// --- wire-frame builders (server side) -------------------------------------

func sftpVersionFrame(v uint32) []byte {
	w := sftp.NewWriter()
	w.WriteUint32(v)
	return sftp.FramePacket(sftp.FXP_VERSION, w.Bytes())
}

func sftpStatusFrame(id, code uint32, msg string, version int) []byte {
	w := sftp.NewWriter()
	w.WriteUint32(id)
	w.WriteUint32(code)
	if version >= 3 {
		w.WriteString(msg)
		w.WriteString("")
	}
	return sftp.FramePacket(sftp.FXP_STATUS, w.Bytes())
}

func sftpHandleFrame(id uint32, handle []byte) []byte {
	w := sftp.NewWriter()
	w.WriteUint32(id)
	w.WriteBytes(handle)
	return sftp.FramePacket(sftp.FXP_HANDLE, w.Bytes())
}

func sftpDataFrame(id uint32, data []byte) []byte {
	w := sftp.NewWriter()
	w.WriteUint32(id)
	w.WriteBytes(data)
	return sftp.FramePacket(sftp.FXP_DATA, w.Bytes())
}

func sftpAttrsFrame(id uint32, a *sftp.Attributes, version int) []byte {
	w := sftp.NewWriter()
	w.WriteUint32(id)
	w.WriteRaw(a.Encode(version))
	return sftp.FramePacket(sftp.FXP_ATTRS, w.Bytes())
}

func sftpNameFrame(id uint32, version int, names []sftp.Name) []byte {
	w := sftp.NewWriter()
	w.WriteUint32(id)
	w.WriteUint32(uint32(len(names)))
	for _, n := range names {
		w.WriteString(n.Filename)
		if version < 4 {
			w.WriteString(n.Longname)
		}
		w.WriteRaw(n.Attributes.Encode(version))
	}
	return sftp.FramePacket(sftp.FXP_NAME, w.Bytes())
}

// u32p / u64p / u8p / strp build the optional-field pointers Attributes uses.
func u32p(v uint32) *uint32 { return &v }
func u64p(v uint64) *uint64 { return &v }

// --- Ruby channel duck + driver harness ------------------------------------

// sftpChanClass is the injected SSH-channel duck: writes captured in @out, reads
// drain the canned reply buffer. @chunk caps each read so the packet-reassembly
// loop can be exercised.
const sftpChanClass = `
class Chan
  def initialize(reply, chunk = nil)
    @in = reply.dup.force_encoding("ASCII-8BIT"); @pos = 0; @out = "".b; @chunk = chunk
  end
  def write(s) ; @out << s ; s.bytesize ; end
  def read(n = nil)
    avail = @in.bytesize - @pos
    return "".b if avail <= 0
    n = avail if n.nil? || n > avail
    n = @chunk if @chunk && @chunk < n
    c = @in.byteslice(@pos, n) ; @pos += n ; c
  end
  def out ; @out ; end
end
`

// rubyBin renders bytes as a Ruby ASCII-8BIT string literal.
func rubyBin(b []byte) string {
	var sb strings.Builder
	sb.WriteString(`"`)
	for _, c := range b {
		fmt.Fprintf(&sb, `\x%02x`, c)
	}
	sb.WriteString(`".b`)
	return sb.String()
}

// concat joins the version frame and each response frame into a reply stream.
func concat(serverVersion uint32, frames ...[]byte) []byte {
	out := sftpVersionFrame(serverVersion)
	for _, f := range frames {
		out = append(out, f...)
	}
	return out
}

// sftpSrc assembles a runnable script: the Chan class, require, a started session
// bound to `sftp`, then body.
func sftpSrc(serverVersion uint32, reply []byte, body string) string {
	return sftpChanClass + "require \"net/sftp\"\n" +
		"ch = Chan.new(" + rubyBin(reply) + ")\n" +
		"sftp = Net::SFTP.start(\"host\", \"user\", channel: ch)\n" + body
}

func sftpEval(t *testing.T, serverVersion uint32, reply []byte, body string) string {
	t.Helper()
	return eval(t, sftpSrc(serverVersion, reply, body))
}

func sftpErr(t *testing.T, serverVersion uint32, reply []byte, body string) (class, msg string) {
	t.Helper()
	return evalErr(t, sftpSrc(serverVersion, reply, body))
}

// --- loadability + surface -------------------------------------------------

func TestNetSFTPLoadable(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p require "net/sftp"`, "true\n"},
		{`require "net/sftp"; p require "net/sftp"`, "false\n"},
		{`require "net/sftp"; p Net::SFTP.is_a?(Module)`, "true\n"},
		{`require "net/sftp"; p Net::SFTP::Session::HIGHEST_PROTOCOL_VERSION_SUPPORTED`, "6\n"},
		{`require "net/sftp"; p Net::SFTP::Exception < StandardError`, "true\n"},
		{`require "net/sftp"; p Net::SFTP::StatusException < Net::SFTP::Exception`, "true\n"},
		{`require "net/sftp"; p Net::SFTP::Constants::StatusCodes::FX_OK`, "0\n"},
		{`require "net/sftp"; p Net::SFTP::Constants::StatusCodes::FX_NO_SUCH_FILE`, "2\n"},
		{`require "net/sftp"; p Net::SFTP::Constants::PacketTypes::FXP_INIT`, "1\n"},
		{`require "net/sftp"; p Net::SFTP::Constants::PacketTypes::FXP_DATA`, "103\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

func TestNetSFTPConnectAndVersion(t *testing.T) {
	// Server offers v6 -> negotiated min(6,6)=6.
	if got := sftpEval(t, 6, concat(6), "p sftp.protocol_version\np sftp.version"); got != "6\n6\n" {
		t.Errorf("v6 got=%q", got)
	}
	// Server offers v3 -> negotiated 3.
	if got := sftpEval(t, 3, concat(3), "p sftp.protocol_version"); got != "3\n" {
		t.Errorf("v3 got=%q", got)
	}
	// The FXP_INIT frame the client wrote advertises version 6.
	got := sftpEval(t, 3, concat(3), `p ch.out.bytesize > 0`)
	if got != "true\n" {
		t.Errorf("init written got=%q", got)
	}
}

// --- channel resolution + start forms --------------------------------------

func TestNetSFTPChannelResolution(t *testing.T) {
	reply := rubyBin(concat(3))
	// Session.new(channel) positional form.
	src := sftpChanClass + "require \"net/sftp\"\np Net::SFTP::Session.new(Chan.new(" + reply + ")).version"
	if got := eval(t, src); got != "3\n" {
		t.Errorf("Session.new positional got=%q", got)
	}
	// A leading Hash positional is skipped, the channel is the next positional.
	src = sftpChanClass + "require \"net/sftp\"\np Net::SFTP::Session.new({}, Chan.new(" + reply + ")).version"
	if got := eval(t, src); got != "3\n" {
		t.Errorf("Session.new hash-then-channel got=%q", got)
	}
	// connection: keyword (symbol).
	src = sftpChanClass + "require \"net/sftp\"\np Net::SFTP.start(connection: Chan.new(" + reply + ")).version"
	if got := eval(t, src); got != "3\n" {
		t.Errorf("connection: symbol got=%q", got)
	}
	// String-keyed channel.
	src = sftpChanClass + "require \"net/sftp\"\np Net::SFTP.start(\"h\", \"u\", {\"channel\" => Chan.new(" + reply + ")}).version"
	if got := eval(t, src); got != "3\n" {
		t.Errorf("string channel got=%q", got)
	}
	// String-keyed connection.
	src = sftpChanClass + "require \"net/sftp\"\np Net::SFTP.start(\"h\", \"u\", {\"connection\" => Chan.new(" + reply + ")}).version"
	if got := eval(t, src); got != "3\n" {
		t.Errorf("string connection got=%q", got)
	}
	// Trailing hash without a channel keyword -> stripped, positional scanned.
	src = sftpChanClass + "require \"net/sftp\"\np Net::SFTP::Session.new(Chan.new(" + reply + "), {db: 1}).version"
	if got := eval(t, src); got != "3\n" {
		t.Errorf("trailing non-channel hash got=%q", got)
	}
}

func TestNetSFTPStartYieldsBlock(t *testing.T) {
	got := sftpEval(t, 3, concat(3), "") // start already ran with body="" and a session bound
	_ = got
	src := sftpChanClass + "require \"net/sftp\"\n" +
		"Net::SFTP.start(\"h\", \"u\", channel: Chan.new(" + rubyBin(concat(3)) + ")) { |s| p s.version }"
	if got := eval(t, src); got != "3\n" {
		t.Errorf("start block got=%q", got)
	}
}

func TestNetSFTPNoChannel(t *testing.T) {
	cls, _ := evalErr(t, `require "net/sftp"; Net::SFTP.start("h", "u")`)
	if cls != "ArgumentError" {
		t.Errorf("no channel: class=%q", cls)
	}
}

// --- open / close / read / write -------------------------------------------

func TestNetSFTPOpenCloseReadWrite(t *testing.T) {
	handle := []byte("H1")
	// open! (v3, OpenFlagsV1 path) returns the handle bytes.
	reply := concat(3, sftpHandleFrame(0, handle))
	if got := sftpEval(t, 3, reply, `p sftp.open!("/f").bytesize`); got != "2\n" {
		t.Errorf("open! got=%q", got)
	}
	// open! on a v6 server drives the OpenFlagsV5 path.
	if got := sftpEval(t, 6, concat(6, sftpHandleFrame(0, handle)), `p sftp.open!("/f", "w").bytesize`); got != "2\n" {
		t.Errorf("open! v6 got=%q", got)
	}
	// open! with a mode and permissions attribute Hash.
	if got := sftpEval(t, 3, concat(3, sftpHandleFrame(0, handle)), `p sftp.open!("/f", "w", permissions: 0644).bytesize`); got != "2\n" {
		t.Errorf("open! attrs got=%q", got)
	}
	// open! bad mode -> ArgumentError.
	if cls, _ := sftpErr(t, 3, concat(3), `sftp.open!("/f", "z")`); cls != "ArgumentError" {
		t.Errorf("open! bad mode class=%q", cls)
	}
	// open! failure -> StatusException.
	cls, _ := sftpErr(t, 3, concat(3, sftpStatusFrame(0, sftp.FX_NO_SUCH_FILE, "nope", 3)), `sftp.open!("/f")`)
	if cls != "Net::SFTP::StatusException" {
		t.Errorf("open! fail class=%q", cls)
	}
	// close! ok.
	if got := sftpEval(t, 3, concat(3, sftpStatusFrame(0, sftp.FX_OK, "", 3)), `p sftp.close!("H")`); got != "nil\n" {
		t.Errorf("close! got=%q", got)
	}
	// close! error.
	if cls, _ := sftpErr(t, 3, concat(3, sftpStatusFrame(0, sftp.FX_FAILURE, "bad", 3)), `sftp.close!("H")`); cls != "Net::SFTP::StatusException" {
		t.Errorf("close! err class=%q", cls)
	}
	// read! returns data.
	if got := sftpEval(t, 3, concat(3, sftpDataFrame(0, []byte("abc"))), `p sftp.read!("H", 0, 10)`); got != "\"abc\"\n" {
		t.Errorf("read! got=%q", got)
	}
	// read! at EOF returns nil.
	if got := sftpEval(t, 3, concat(3, sftpStatusFrame(0, sftp.FX_EOF, "", 3)), `p sftp.read!("H", 0, 10)`); got != "nil\n" {
		t.Errorf("read! eof got=%q", got)
	}
	// read! error status raises.
	if cls, _ := sftpErr(t, 3, concat(3, sftpStatusFrame(0, sftp.FX_FAILURE, "x", 3)), `sftp.read!("H", 0, 10)`); cls != "Net::SFTP::StatusException" {
		t.Errorf("read! err class=%q", cls)
	}
	// write! ok + error.
	if got := sftpEval(t, 3, concat(3, sftpStatusFrame(0, sftp.FX_OK, "", 3)), `p sftp.write!("H", 0, "data")`); got != "nil\n" {
		t.Errorf("write! got=%q", got)
	}
	if cls, _ := sftpErr(t, 3, concat(3, sftpStatusFrame(0, sftp.FX_PERMISSION_DENIED, "", 3)), `sftp.write!("H", 0, "d")`); cls != "Net::SFTP::StatusException" {
		t.Errorf("write! err class=%q", cls)
	}
}

// --- stat family -----------------------------------------------------------

func TestNetSFTPStat(t *testing.T) {
	a := &sftp.Attributes{Size: u64p(1234), Permissions: u32p(0o100644), UID: u32p(7), GID: u32p(8), Atime: u64p(111), Mtime: u64p(222)}
	reply := concat(3, sftpAttrsFrame(0, a, 3))
	body := `st = sftp.stat!("/f")
p st.size
p st.permissions
p st.uid
p st.gid
p st.atime
p st.mtime
p st.file?
p st.directory?
p st.symlink?
p st.type`
	want := "1234\n33188\n7\n8\n111\n222\ntrue\nfalse\nfalse\n1\n"
	if got := sftpEval(t, 3, reply, body); got != want {
		t.Errorf("stat! got=%q want=%q", got, want)
	}
	// lstat!.
	if got := sftpEval(t, 3, concat(3, sftpAttrsFrame(0, a, 3)), `p sftp.lstat!("/f").size`); got != "1234\n" {
		t.Errorf("lstat! got=%q", got)
	}
	// fstat!.
	if got := sftpEval(t, 3, concat(3, sftpAttrsFrame(0, a, 3)), `p sftp.fstat!("H").size`); got != "1234\n" {
		t.Errorf("fstat! got=%q", got)
	}
	// stat! failure raises.
	if cls, _ := sftpErr(t, 3, concat(3, sftpStatusFrame(0, sftp.FX_NO_SUCH_FILE, "", 3)), `sftp.stat!("/x")`); cls != "Net::SFTP::StatusException" {
		t.Errorf("stat! err class=%q", cls)
	}
	if cls, _ := sftpErr(t, 3, concat(3, sftpStatusFrame(0, sftp.FX_NO_SUCH_FILE, "", 3)), `sftp.lstat!("/x")`); cls != "Net::SFTP::StatusException" {
		t.Errorf("lstat! err class=%q", cls)
	}
	if cls, _ := sftpErr(t, 3, concat(3, sftpStatusFrame(0, sftp.FX_NO_SUCH_FILE, "", 3)), `sftp.fstat!("H")`); cls != "Net::SFTP::StatusException" {
		t.Errorf("fstat! err class=%q", cls)
	}
}

func TestNetSFTPAttributesNilFields(t *testing.T) {
	// A directory with only the type bit set: optional numeric fields are nil.
	a := &sftp.Attributes{Permissions: u32p(0o040755)}
	body := `st = sftp.stat!("/d")
p st.size
p st.uid
p st.gid
p st.owner
p st.group
p st.directory?`
	want := "nil\nnil\nnil\nnil\nnil\ntrue\n"
	if got := sftpEval(t, 3, concat(3, sftpAttrsFrame(0, a, 3)), body); got != want {
		t.Errorf("nil-fields got=%q want=%q", got, want)
	}
	// A v4 attrs with owner/group strings and a symlink type.
	owner, group := "root", "wheel"
	sa := &sftp.Attributes{Type: func() *uint8 { v := uint8(sftp.TSymlink); return &v }(), Owner: &owner, Group: &group, Permissions: u32p(0o120777)}
	body = `st = sftp.stat!("/l")
p st.owner
p st.group
p st.symlink?`
	if got := sftpEval(t, 6, concat(6, sftpAttrsFrame(0, sa, 6)), body); got != "\"root\"\n\"wheel\"\ntrue\n" {
		t.Errorf("v6 owner/group got=%q", got)
	}
}

// --- setstat / mkdir attrs -------------------------------------------------

func TestNetSFTPSetstat(t *testing.T) {
	// A full attribute Hash covers every sftpAttrsArg branch; one key is a String
	// so the symbol->string fallback lookup runs too.
	attrsHash := `{size: 10, permissions: 0644, uid: 1, gid: 2, owner: "a", group: "b", atime: 3, "mtime" => 4}`
	if got := sftpEval(t, 6, concat(6, sftpStatusFrame(0, sftp.FX_OK, "", 6)), `p sftp.setstat!("/f", `+attrsHash+`)`); got != "nil\n" {
		t.Errorf("setstat! got=%q", got)
	}
	if cls, _ := sftpErr(t, 3, concat(3, sftpStatusFrame(0, sftp.FX_FAILURE, "", 3)), `sftp.setstat!("/f", {})`); cls != "Net::SFTP::StatusException" {
		t.Errorf("setstat! err class=%q", cls)
	}
	if got := sftpEval(t, 3, concat(3, sftpStatusFrame(0, sftp.FX_OK, "", 3)), `p sftp.fsetstat!("H", {size: 1})`); got != "nil\n" {
		t.Errorf("fsetstat! got=%q", got)
	}
	if cls, _ := sftpErr(t, 3, concat(3, sftpStatusFrame(0, sftp.FX_FAILURE, "", 3)), `sftp.fsetstat!("H", {})`); cls != "Net::SFTP::StatusException" {
		t.Errorf("fsetstat! err class=%q", cls)
	}
}

// --- directory ops ---------------------------------------------------------

func TestNetSFTPDirEntryOps(t *testing.T) {
	// mkdir!/rmdir!/remove! ok + error.
	for _, m := range []string{"mkdir!", "rmdir!", "remove!"} {
		call := m
		arg := `"/d"`
		if m == "mkdir!" {
			arg = `"/d", {permissions: 0755}`
		}
		if got := sftpEval(t, 3, concat(3, sftpStatusFrame(0, sftp.FX_OK, "", 3)), `p sftp.`+call+`(`+arg+`)`); got != "nil\n" {
			t.Errorf("%s got=%q", m, got)
		}
		if cls, _ := sftpErr(t, 3, concat(3, sftpStatusFrame(0, sftp.FX_FAILURE, "", 3)), `sftp.`+call+`(`+arg+`)`); cls != "Net::SFTP::StatusException" {
			t.Errorf("%s err class=%q", m, cls)
		}
	}
	// opendir! returns handle, error raises.
	if got := sftpEval(t, 3, concat(3, sftpHandleFrame(0, []byte("D"))), `p sftp.opendir!("/d")`); got != "\"D\"\n" {
		t.Errorf("opendir! got=%q", got)
	}
	if cls, _ := sftpErr(t, 3, concat(3, sftpStatusFrame(0, sftp.FX_NO_SUCH_PATH, "", 3)), `sftp.opendir!("/d")`); cls != "Net::SFTP::StatusException" {
		t.Errorf("opendir! err class=%q", cls)
	}
}

func TestNetSFTPReaddir(t *testing.T) {
	fa := &sftp.Attributes{Permissions: u32p(0o100644), Size: u64p(5), Owner: strPtr("u"), Group: strPtr("g"), Atime: u64p(900), Mtime: u64p(1000)}
	da := &sftp.Attributes{Permissions: u32p(0o040755)}
	names := []sftp.Name{
		{Filename: "file.txt", Longname: "-rw-r--r-- file.txt", Attributes: fa},
		{Filename: "sub", Longname: "drwxr-xr-x sub", Attributes: da},
	}
	// readdir! returns an array of Name entries with accessors.
	body := `es = sftp.readdir!("D")
p es.length
p es[0].name
p es[0].filename
p es[0].longname
p es[0].file?
p es[1].directory?
p es[0].attributes.size`
	want := "2\n\"file.txt\"\n\"file.txt\"\n\"-rw-r--r-- file.txt\"\ntrue\ntrue\n5\n"
	if got := sftpEval(t, 3, concat(3, sftpNameFrame(0, 3, names)), body); got != want {
		t.Errorf("readdir! got=%q want=%q", got, want)
	}
	// readdir! at EOF returns nil.
	if got := sftpEval(t, 3, concat(3, sftpStatusFrame(0, sftp.FX_EOF, "", 3)), `p sftp.readdir!("D")`); got != "nil\n" {
		t.Errorf("readdir! eof got=%q", got)
	}
	// readdir! error raises.
	if cls, _ := sftpErr(t, 3, concat(3, sftpStatusFrame(0, sftp.FX_FAILURE, "", 3)), `sftp.readdir!("D")`); cls != "Net::SFTP::StatusException" {
		t.Errorf("readdir! err class=%q", cls)
	}
	// v6 longname is synthesised from attributes (no wire longname).
	v6names := []sftp.Name{{Filename: "f", Attributes: fa}}
	got := sftpEval(t, 6, concat(6, sftpNameFrame(0, 6, v6names)), `p sftp.readdir!("D")[0].longname.include?("f")`)
	if got != "true\n" {
		t.Errorf("v6 longname got=%q", got)
	}
}

// --- realpath / links ------------------------------------------------------

func TestNetSFTPRealpathAndLinks(t *testing.T) {
	da := &sftp.Attributes{Permissions: u32p(0o040755)}
	name := []sftp.Name{{Filename: "/abs/path", Attributes: da}}
	// realpath! returns the canonical name.
	if got := sftpEval(t, 3, concat(3, sftpNameFrame(0, 3, name)), `p sftp.realpath!("x")`); got != "\"/abs/path\"\n" {
		t.Errorf("realpath! got=%q", got)
	}
	// realpath! error raises.
	if cls, _ := sftpErr(t, 3, concat(3, sftpStatusFrame(0, sftp.FX_NO_SUCH_PATH, "", 3)), `sftp.realpath!("x")`); cls != "Net::SFTP::StatusException" {
		t.Errorf("realpath! err class=%q", cls)
	}
	// realpath! with an empty NAME response raises the codec exception.
	if cls, _ := sftpErr(t, 3, concat(3, sftpNameFrame(0, 3, nil)), `sftp.realpath!("x")`); cls != "Net::SFTP::Exception" {
		t.Errorf("realpath! empty class=%q", cls)
	}

	// rename! ok (v3) + error (server failure).
	if got := sftpEval(t, 3, concat(3, sftpStatusFrame(0, sftp.FX_OK, "", 3)), `p sftp.rename!("a", "b")`); got != "nil\n" {
		t.Errorf("rename! got=%q", got)
	}
	if cls, _ := sftpErr(t, 3, concat(3, sftpStatusFrame(0, sftp.FX_FAILURE, "", 3)), `sftp.rename!("a", "b")`); cls != "Net::SFTP::StatusException" {
		t.Errorf("rename! err class=%q", cls)
	}
	// rename! unavailable in v1 -> the library gate raises Net::SFTP::Exception.
	if cls, _ := sftpErr(t, 1, concat(1), `sftp.rename!("a", "b")`); cls != "Net::SFTP::Exception" {
		t.Errorf("rename! v1 class=%q", cls)
	}

	// readlink! ok returns target; error raises; v2 gate raises.
	tn := []sftp.Name{{Filename: "target", Attributes: da}}
	if got := sftpEval(t, 3, concat(3, sftpNameFrame(0, 3, tn)), `p sftp.readlink!("l")`); got != "\"target\"\n" {
		t.Errorf("readlink! got=%q", got)
	}
	if cls, _ := sftpErr(t, 3, concat(3, sftpStatusFrame(0, sftp.FX_FAILURE, "", 3)), `sftp.readlink!("l")`); cls != "Net::SFTP::StatusException" {
		t.Errorf("readlink! err class=%q", cls)
	}
	if cls, _ := sftpErr(t, 2, concat(2), `sftp.readlink!("l")`); cls != "Net::SFTP::Exception" {
		t.Errorf("readlink! v2 class=%q", cls)
	}

	// symlink! ok (v3) + error + v2 gate.
	if got := sftpEval(t, 3, concat(3, sftpStatusFrame(0, sftp.FX_OK, "", 3)), `p sftp.symlink!("l", "t")`); got != "nil\n" {
		t.Errorf("symlink! got=%q", got)
	}
	if cls, _ := sftpErr(t, 3, concat(3, sftpStatusFrame(0, sftp.FX_FAILURE, "", 3)), `sftp.symlink!("l", "t")`); cls != "Net::SFTP::StatusException" {
		t.Errorf("symlink! err class=%q", cls)
	}
	if cls, _ := sftpErr(t, 2, concat(2), `sftp.symlink!("l", "t")`); cls != "Net::SFTP::Exception" {
		t.Errorf("symlink! v2 class=%q", cls)
	}

	// link! ok (v6) + error + v3 gate; block! / unblock! same.
	if got := sftpEval(t, 6, concat(6, sftpStatusFrame(0, sftp.FX_OK, "", 6)), `p sftp.link!("n", "e", true)`); got != "nil\n" {
		t.Errorf("link! got=%q", got)
	}
	if cls, _ := sftpErr(t, 6, concat(6, sftpStatusFrame(0, sftp.FX_FAILURE, "", 6)), `sftp.link!("n", "e", false)`); cls != "Net::SFTP::StatusException" {
		t.Errorf("link! err class=%q", cls)
	}
	if cls, _ := sftpErr(t, 3, concat(3), `sftp.link!("n", "e")`); cls != "Net::SFTP::Exception" {
		t.Errorf("link! v3 class=%q", cls)
	}
	if got := sftpEval(t, 6, concat(6, sftpStatusFrame(0, sftp.FX_OK, "", 6)), `p sftp.block!("H", 0, 10, 1)`); got != "nil\n" {
		t.Errorf("block! got=%q", got)
	}
	if cls, _ := sftpErr(t, 6, concat(6, sftpStatusFrame(0, sftp.FX_LOCK_CONFLICT, "", 6)), `sftp.block!("H", 0, 10, 1)`); cls != "Net::SFTP::StatusException" {
		t.Errorf("block! err class=%q", cls)
	}
	if cls, _ := sftpErr(t, 3, concat(3), `sftp.block!("H", 0, 10, 1)`); cls != "Net::SFTP::Exception" {
		t.Errorf("block! v3 class=%q", cls)
	}
	if got := sftpEval(t, 6, concat(6, sftpStatusFrame(0, sftp.FX_OK, "", 6)), `p sftp.unblock!("H", 0, 10)`); got != "nil\n" {
		t.Errorf("unblock! got=%q", got)
	}
	if cls, _ := sftpErr(t, 6, concat(6, sftpStatusFrame(0, sftp.FX_FAILURE, "", 6)), `sftp.unblock!("H", 0, 10)`); cls != "Net::SFTP::StatusException" {
		t.Errorf("unblock! err class=%q", cls)
	}
	if cls, _ := sftpErr(t, 3, concat(3), `sftp.unblock!("H", 0, 10)`); cls != "Net::SFTP::Exception" {
		t.Errorf("unblock! v3 class=%q", cls)
	}
}

// --- upload / download -----------------------------------------------------

func TestNetSFTPTransfer(t *testing.T) {
	// download!: open -> read (data) -> read (EOF) -> close.
	reply := concat(3,
		sftpHandleFrame(0, []byte("H")),
		sftpDataFrame(1, []byte("hello world")),
		sftpStatusFrame(2, sftp.FX_EOF, "", 3),
		sftpStatusFrame(3, sftp.FX_OK, "", 3),
	)
	if got := sftpEval(t, 3, reply, `p sftp.download!("/remote")`); got != "\"hello world\"\n" {
		t.Errorf("download! got=%q", got)
	}
	// download! read error raises.
	badReply := concat(3,
		sftpHandleFrame(0, []byte("H")),
		sftpStatusFrame(1, sftp.FX_PERMISSION_DENIED, "", 3),
	)
	if cls, _ := sftpErr(t, 3, badReply, `sftp.download!("/r")`); cls != "Net::SFTP::StatusException" {
		t.Errorf("download! err class=%q", cls)
	}
	// upload! from a String: open -> write -> close.
	up := concat(3,
		sftpHandleFrame(0, []byte("H")),
		sftpStatusFrame(1, sftp.FX_OK, "", 3),
		sftpStatusFrame(2, sftp.FX_OK, "", 3),
	)
	if got := sftpEval(t, 3, up, `p sftp.upload!("payload", "/remote")`); got != "nil\n" {
		t.Errorf("upload! got=%q", got)
	}
	// upload! from an IO-like source (responds to #read).
	src := sftpChanClass + "require \"net/sftp\"\n" +
		"require \"stringio\"\n" +
		"ch = Chan.new(" + rubyBin(up) + ")\n" +
		"sftp = Net::SFTP.start(channel: ch)\n" +
		`p sftp.upload!(StringIO.new("io-payload"), "/remote")`
	if got := eval(t, src); got != "nil\n" {
		t.Errorf("upload! io got=%q", got)
	}
}

// --- dir / file operation helpers ------------------------------------------

func TestNetSFTPDirHelper(t *testing.T) {
	fa := &sftp.Attributes{Permissions: u32p(0o100644), Size: u64p(3), Atime: u64p(1), Mtime: u64p(1)}
	names := []sftp.Name{{Filename: "a", Longname: "a", Attributes: fa}, {Filename: "b", Longname: "b", Attributes: fa}}
	// dir.entries: opendir -> readdir(names) -> readdir(EOF) -> close.
	reply := concat(3,
		sftpHandleFrame(0, []byte("D")),
		sftpNameFrame(1, 3, names),
		sftpStatusFrame(2, sftp.FX_EOF, "", 3),
		sftpStatusFrame(3, sftp.FX_OK, "", 3),
	)
	if got := sftpEval(t, 3, reply, `p sftp.dir.entries("/d").map(&:name)`); got != "[\"a\", \"b\"]\n" {
		t.Errorf("dir.entries got=%q", got)
	}
	// dir.foreach yields each entry.
	reply = concat(3,
		sftpHandleFrame(0, []byte("D")),
		sftpNameFrame(1, 3, names),
		sftpStatusFrame(2, sftp.FX_EOF, "", 3),
		sftpStatusFrame(3, sftp.FX_OK, "", 3),
	)
	if got := sftpEval(t, 3, reply, `sftp.dir.foreach("/d") { |e| puts e.name }`); got != "a\nb\n" {
		t.Errorf("dir.foreach got=%q", got)
	}
	// dir.foreach without a block raises.
	reply = concat(3, sftpHandleFrame(0, []byte("D")), sftpStatusFrame(1, sftp.FX_EOF, "", 3), sftpStatusFrame(2, sftp.FX_OK, "", 3))
	if cls, _ := sftpErr(t, 3, reply, `sftp.dir.foreach("/d")`); cls != "LocalJumpError" {
		t.Errorf("dir.foreach no-block class=%q", cls)
	}
}

func TestNetSFTPFileHelper(t *testing.T) {
	// file.open block form: open -> (read data, read EOF) -> close.
	reply := concat(3,
		sftpHandleFrame(0, []byte("F")),
		sftpDataFrame(1, []byte("abc")),
		sftpStatusFrame(2, sftp.FX_EOF, "", 3),
		sftpStatusFrame(3, sftp.FX_OK, "", 3),
	)
	if got := sftpEval(t, 3, reply, `sftp.file.open("/f") { |f| p f.read }`); got != "\"abc\"\n" {
		t.Errorf("file.open block read got=%q", got)
	}
	// file.open non-block form + explicit read(len)/pos/close (with a redundant
	// second close to hit the already-closed guard).
	reply = concat(3,
		sftpHandleFrame(0, []byte("F")),
		sftpDataFrame(1, []byte("xy")),
		sftpStatusFrame(2, sftp.FX_OK, "", 3),
	)
	body := `f = sftp.file.open("/f", "r")
p f.read(2)
p f.pos
p f.close
p f.close`
	if got := sftpEval(t, 3, reply, body); got != "\"xy\"\n2\nnil\nnil\n" {
		t.Errorf("file.open explicit got=%q", got)
	}
	// file read(len) at EOF returns nil.
	reply = concat(3, sftpHandleFrame(0, []byte("F")), sftpStatusFrame(1, sftp.FX_EOF, "", 3))
	if got := sftpEval(t, 3, reply, `p sftp.file.open("/f").read(4)`); got != "nil\n" {
		t.Errorf("file read eof got=%q", got)
	}
	// file.write advances pos and returns the byte count.
	reply = concat(3, sftpHandleFrame(0, []byte("F")), sftpStatusFrame(1, sftp.FX_OK, "", 3))
	body = `f = sftp.file.open("/f", "w")
p f.write("hello")
p f.pos`
	if got := sftpEval(t, 3, reply, body); got != "5\n5\n" {
		t.Errorf("file.write got=%q", got)
	}
	// file.open with attributes Hash (mode + permissions).
	reply = concat(3, sftpHandleFrame(0, []byte("F")), sftpStatusFrame(1, sftp.FX_OK, "", 3))
	if got := sftpEval(t, 3, reply, `p sftp.file.open("/f", "w", permissions: 0600).close`); got != "nil\n" {
		t.Errorf("file.open attrs got=%q", got)
	}
	// file.directory? true and false.
	da := &sftp.Attributes{Permissions: u32p(0o040755)}
	if got := sftpEval(t, 3, concat(3, sftpAttrsFrame(0, da, 3)), `p sftp.file.directory?("/d")`); got != "true\n" {
		t.Errorf("file.directory? true got=%q", got)
	}
	if got := sftpEval(t, 3, concat(3, sftpStatusFrame(0, sftp.FX_NO_SUCH_FILE, "", 3)), `p sftp.file.directory?("/x")`); got != "false\n" {
		t.Errorf("file.directory? false got=%q", got)
	}
}

// --- StatusException surface -----------------------------------------------

func TestNetSFTPStatusException(t *testing.T) {
	// The raised StatusException exposes code / description / text / message.
	reply := concat(3, sftpStatusFrame(0, sftp.FX_NO_SUCH_FILE, "", 3))
	body := `begin
  sftp.stat!("/missing")
rescue Net::SFTP::StatusException => e
  p e.code
  p e.description
  p e.text
  p e.message.include?("stat")
  p e.to_s.include?("no such file")
end`
	want := "2\n\"no such file\"\n\"stat\"\ntrue\ntrue\n"
	if got := sftpEval(t, 3, reply, body); got != want {
		t.Errorf("StatusException got=%q want=%q", got, want)
	}
	// A server-supplied message overrides the canonical description.
	reply = concat(3, sftpStatusFrame(0, sftp.FX_FAILURE, "custom boom", 3))
	body = `begin
  sftp.remove!("/x")
rescue Net::SFTP::StatusException => e
  p e.description
end`
	if got := sftpEval(t, 3, reply, body); got != "\"custom boom\"\n" {
		t.Errorf("StatusException custom msg got=%q", got)
	}
}

// --- driver / protocol error paths -----------------------------------------

func TestNetSFTPProtocolErrors(t *testing.T) {
	// Channel closes before a complete packet -> Net::SFTP::Exception.
	if cls, _ := evalErr(t, sftpChanClass+"require \"net/sftp\"\nNet::SFTP.start(channel: Chan.new(\"\".b))"); cls != "Net::SFTP::Exception" {
		t.Errorf("empty channel class=%q", cls)
	}
	// First packet is not FXP_VERSION.
	notVersion := sftpStatusFrame(0, sftp.FX_OK, "", 3)
	src := sftpChanClass + "require \"net/sftp\"\nNet::SFTP.start(channel: Chan.new(" + rubyBin(notVersion) + "))"
	if cls, _ := evalErr(t, src); cls != "Net::SFTP::Exception" {
		t.Errorf("non-version class=%q", cls)
	}
	// A truncated FXP_VERSION payload (no version word) -> ParseVersion error.
	badVer := sftp.FramePacket(sftp.FXP_VERSION, []byte{0x00})
	src = sftpChanClass + "require \"net/sftp\"\nNet::SFTP.start(channel: Chan.new(" + rubyBin(badVer) + "))"
	if cls, _ := evalErr(t, src); cls != "Net::SFTP::Exception" {
		t.Errorf("bad-version class=%q", cls)
	}
	// Server version 0 -> NegotiateVersion error.
	if cls, _ := evalErr(t, sftpChanClass+"require \"net/sftp\"\nNet::SFTP.start(channel: Chan.new("+rubyBin(sftpVersionFrame(0))+"))"); cls != "Net::SFTP::Exception" {
		t.Errorf("version-0 class=%q", cls)
	}
	// Unknown response packet type -> ParseResponse error.
	unknown := sftp.FramePacket(sftp.FXP_EXTENDED_REPLY, []byte{0, 0, 0, 0})
	if cls, _ := sftpErr(t, 3, concat(3, unknown), `sftp.stat!("/f")`); cls != "Net::SFTP::Exception" {
		t.Errorf("unknown-response class=%q", cls)
	}
	// Response id mismatch (a HANDLE echoing id 9 for request id 0).
	if cls, _ := sftpErr(t, 3, concat(3, sftpHandleFrame(9, []byte("H"))), `sftp.open!("/f")`); cls != "Net::SFTP::Exception" {
		t.Errorf("id-mismatch class=%q", cls)
	}
	// Packet reassembly across multiple chunked reads (@chunk = 1).
	reply := concat(3, sftpHandleFrame(0, []byte("H")))
	src = sftpChanClass + "require \"net/sftp\"\n" +
		"ch = Chan.new(" + rubyBin(reply) + ", 1)\n" +
		"sftp = Net::SFTP.start(channel: ch)\n" +
		`p sftp.open!("/f")`
	if got := eval(t, src); got != "\"H\"\n" {
		t.Errorf("chunked read got=%q", got)
	}
}

// --- argument type errors --------------------------------------------------

func TestNetSFTPArgTypeErrors(t *testing.T) {
	cases := []struct{ body, class string }{
		{`sftp.close!(123)`, "TypeError"},        // handle not a String
		{`sftp.stat!(123)`, "TypeError"},         // path not a String
		{`sftp.read!("H", "x", 1)`, "TypeError"}, // offset not an Integer
		{`sftp.read!("H", 0, "x")`, "TypeError"}, // length not an Integer
	}
	for _, c := range cases {
		if cls, _ := sftpErr(t, 3, concat(3), c.body); cls != c.class {
			t.Errorf("body=%q got class=%q want=%q", c.body, cls, c.class)
		}
	}
}

// --- wrapper reprs + remaining branches ------------------------------------

func TestNetSFTPWrapperReprs(t *testing.T) {
	// Session / dir / file-factory to_s / inspect / truthiness.
	reply := concat(3, sftpHandleFrame(0, []byte("F")), sftpStatusFrame(1, sftp.FX_OK, "", 3))
	body := `p sftp.to_s
p sftp.inspect
p(sftp ? "y" : "n")
d = sftp.dir
p d.to_s
p d.inspect
p(d ? 1 : 0)
ff = sftp.file
p ff.to_s
p ff.inspect
p(ff ? 1 : 0)
f = ff.open("/f", "w")
p f.to_s
p f.inspect
p(f ? 1 : 0)`
	want := "\"#<Net::SFTP::Session>\"\n\"#<Net::SFTP::Session>\"\n\"y\"\n" +
		"\"#<Net::SFTP::Operations::Dir>\"\n\"#<Net::SFTP::Operations::Dir>\"\n1\n" +
		"\"#<Net::SFTP::Operations::FileFactory>\"\n\"#<Net::SFTP::Operations::FileFactory>\"\n1\n" +
		"\"#<Net::SFTP::Operations::File>\"\n\"#<Net::SFTP::Operations::File>\"\n1\n"
	if got := sftpEval(t, 3, reply, body); got != want {
		t.Errorf("wrapper reprs got=%q want=%q", got, want)
	}

	// Attributes wrapper to_s / inspect / truthiness.
	a := &sftp.Attributes{Permissions: u32p(0o100644)}
	abody := `st = sftp.stat!("/f")
p st.to_s
p st.inspect
p(st ? 1 : 0)`
	awant := "\"#<Net::SFTP::Protocol::V01::Attributes>\"\n\"#<Net::SFTP::Protocol::V01::Attributes>\"\n1\n"
	if got := sftpEval(t, 3, concat(3, sftpAttrsFrame(0, a, 3)), abody); got != awant {
		t.Errorf("attrs reprs got=%q", got)
	}

	// Name wrapper to_s / inspect / truthiness / symlink?.
	sa := &sftp.Attributes{Permissions: u32p(0o120777)}
	names := []sftp.Name{{Filename: "l", Longname: "l", Attributes: sa}}
	nbody := `n = sftp.readdir!("D")[0]
p n.to_s
p n.inspect
p(n ? 1 : 0)
p n.symlink?`
	nwant := "\"#<Net::SFTP::Name>\"\n\"#<Net::SFTP::Name>\"\n1\ntrue\n"
	if got := sftpEval(t, 3, concat(3, sftpNameFrame(0, 3, names)), nbody); got != nwant {
		t.Errorf("name reprs got=%q", got)
	}
}

func TestNetSFTPDirEntriesError(t *testing.T) {
	// dir.entries whose readdir yields an error status raises (readdirAll branch).
	reply := concat(3,
		sftpHandleFrame(0, []byte("D")),
		sftpStatusFrame(1, sftp.FX_FAILURE, "boom", 3),
	)
	if cls, _ := sftpErr(t, 3, reply, `sftp.dir.entries("/d")`); cls != "Net::SFTP::StatusException" {
		t.Errorf("dir.entries error class=%q", cls)
	}
}

func TestNetSFTPChannelReadNil(t *testing.T) {
	// A channel whose #read returns nil (not a String) is treated as a closed
	// stream (sftpReadBytes nil branch -> Net::SFTP::Exception at connect).
	src := "class NilChan\n  def write(s); s.bytesize; end\n  def read(n = nil); nil; end\nend\n" +
		"require \"net/sftp\"\nNet::SFTP.start(channel: NilChan.new)"
	if cls, _ := evalErr(t, src); cls != "Net::SFTP::Exception" {
		t.Errorf("nil-read class=%q", cls)
	}
}

func strPtr(s string) *string { return &s }
