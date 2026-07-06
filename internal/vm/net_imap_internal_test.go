// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"io"
	"strings"
	"testing"

	imap "github.com/go-ruby-net-imap/net-imap"

	"github.com/go-embedded-ruby/ruby/internal/compiler"
	"github.com/go-embedded-ruby/ruby/internal/object"
	"github.com/go-ruby-parser/parser"
)

// imapRun runs src on a fresh VM and returns its trimmed stdout, failing on a
// parse / compile / run error.
func imapRun(t *testing.T, src string) string {
	t.Helper()
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v\nsrc:\n%s", err, src)
	}
	iseq, err := compiler.Compile(prog)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	var buf strings.Builder
	if _, rerr := New(&buf).Run(iseq); rerr != nil {
		t.Fatalf("run: %v\nsrc:\n%s", rerr, src)
	}
	return strings.TrimRight(buf.String(), "\n")
}

// imapRunErr runs src and returns the uncaught error's class, or "" if clean.
func imapRunErr(t *testing.T, src string) string {
	t.Helper()
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	iseq, err := compiler.Compile(prog)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	_, rerr := New(io.Discard).Run(iseq)
	if rerr == nil {
		return ""
	}
	if i := strings.Index(rerr.Error(), ": "); i > 0 {
		return rerr.Error()[:i]
	}
	return rerr.Error()
}

// imapFakeSock is a duplex Ruby duck-typed socket: reads drain the canned server
// stream (greeting + responses), writes are captured. It is the injected IMAP
// transport the binding drives its protocol over — deterministic, no network.
const imapFakeSock = `
class FakeSock
  def initialize(reply) ; @in = reply.dup.force_encoding("ASCII-8BIT") ; @pos = 0 ; @out = "".b ; @closed = false ; end
  def write(s) ; @out << s ; s.bytesize ; end
  def read(n = nil)
    avail = @in.bytesize - @pos
    return "".b if avail <= 0
    n = avail if n.nil? || n > avail
    chunk = @in.byteslice(@pos, n)
    @pos += n
    chunk
  end
  def out ; @out ; end
  def close ; @closed = true ; end
  def closed? ; @closed ; end
end
`

// greet is the standard server greeting prefix.
const greet = "* OK [CAPABILITY IMAP4rev1] ready\r\n"

// rbLit encodes a raw wire byte string as a Ruby double-quoted string literal,
// escaping backslash / quote / CR / LF so it survives being embedded in Ruby
// source verbatim.
func rbLit(wire string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(wire); i++ {
		switch c := wire[i]; c {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\r':
			b.WriteString(`\r`)
		case '\n':
			b.WriteString(`\n`)
		default:
			b.WriteByte(c)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// sock returns the Ruby expression FakeSock.new(<literal>) for a wire stream.
func sock(wire string) string { return "FakeSock.new(" + rbLit(wire) + ")" }

// imapEval runs body (with net/imap required and FakeSock defined), returning
// trimmed stdout.
func imapEval(t *testing.T, body string) string {
	t.Helper()
	return imapRun(t, imapFakeSock+"require \"net/imap\"\n"+body)
}

func TestNetIMAPRequireAndErrors(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "net/imap"; p require "net/imap"`, "false"},
		{`p require "net/imap"`, "true"},
		{`require "net/imap"; p Net::IMAP.is_a?(Class)`, "true"},
		{`require "net/imap"; p Net::IMAP::Error < StandardError`, "true"},
		{`require "net/imap"; p Net::IMAP::ResponseError < Net::IMAP::Error`, "true"},
		{`require "net/imap"; p Net::IMAP::NoResponseError < Net::IMAP::ResponseError`, "true"},
		{`require "net/imap"; p Net::IMAP::BadResponseError < Net::IMAP::ResponseError`, "true"},
		{`require "net/imap"; p Net::IMAP::ByeResponseError < Net::IMAP::ResponseError`, "true"},
		{`require "net/imap"; p Net::IMAP::ResponseParseError < Net::IMAP::Error`, "true"},
		{`require "net/imap"; p Net::IMAP::DataFormatError < Net::IMAP::Error`, "true"},
	}
	for _, c := range cases {
		if got := imapRun(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

func TestNetIMAPGreetingAndConnect(t *testing.T) {
	got := imapEval(t, `
c = Net::IMAP.new(connection: `+sock(greet)+`)
g = c.greeting
p g.class
p g.name
p g.data.text
p g.data.code.name
p c.disconnected?`)
	want := "Net::IMAP::UntaggedResponse\n\"OK\"\n\"ready\"\n\"CAPABILITY\"\nfalse"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestNetIMAPConnectionRequired(t *testing.T) {
	if got := imapRunErr(t, `require "net/imap"; Net::IMAP.new("imap.example.com")`); got != "ArgumentError" {
		t.Errorf("missing connection: got %q", got)
	}
	// A positional IO seam (not a String host) is accepted.
	if got := imapEval(t, `p Net::IMAP.new(`+sock(greet)+`).greeting.name`); got != "\"OK\"" {
		t.Errorf("positional conn: got %q", got)
	}
	// The conn: keyword alias is accepted.
	if got := imapEval(t, `p Net::IMAP.new(conn: `+sock(greet)+`).greeting.name`); got != "\"OK\"" {
		t.Errorf("conn: keyword: got %q", got)
	}
	// A positional IO seam plus a trailing (non-connection) options Hash.
	if got := imapEval(t, `p Net::IMAP.new(`+sock(greet)+`, {}).greeting.name`); got != "\"OK\"" {
		t.Errorf("positional + opts hash: got %q", got)
	}
}

func TestNetIMAPFetchLiteral(t *testing.T) {
	// A server-sent literal ({n}\r\n) exercises the Reader's literal callback.
	stream := greet + "* 1 FETCH (BODY[TEXT] {5}\r\nhello)\r\nRUBY0001 OK FETCH\r\n"
	got := imapEval(t, `
c = Net::IMAP.new(connection: `+sock(stream)+`)
p c.fetch(1, "BODY[TEXT]")[0].attr["BODY[TEXT]"]`)
	if got != "\"hello\"" {
		t.Errorf("literal fetch got %q", got)
	}
	// A literal whose byte count exceeds the remaining stream raises Net::IMAP::Error
	// (the literal read hits EOF).
	trunc := greet + "* 1 FETCH (BODY[TEXT] {100}\r\nshort"
	if got := imapRunErr(t, imapFakeSock+`require "net/imap"
Net::IMAP.new(connection: `+sock(trunc)+`).fetch(1, "BODY[TEXT]")`); got != "Net::IMAP::Error" {
		t.Errorf("truncated literal: got %q", got)
	}
}

func TestNetIMAPLoginAndSimpleCommands(t *testing.T) {
	stream := greet +
		"RUBY0001 OK LOGIN completed\r\n" +
		"RUBY0002 OK NOOP\r\n" +
		"RUBY0003 OK CHECK\r\n" +
		"RUBY0004 OK CLOSE\r\n" +
		"RUBY0005 OK UNSELECT\r\n" +
		"RUBY0006 OK begin TLS\r\n"
	got := imapEval(t, `
s = `+sock(stream)+`
c = Net::IMAP.new(connection: s)
r = c.login("joe", "secret")
p r.class
p r.tag
p r.name
p c.noop.name
p c.check.name
p c.close.name
p c.unselect.name
p c.starttls.name
p s.out.include?("RUBY0001 LOGIN joe secret")`)
	want := "Net::IMAP::TaggedResponse\n\"RUBY0001\"\n\"OK\"\n\"OK\"\n\"OK\"\n\"OK\"\n\"OK\"\n\"OK\"\ntrue"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestNetIMAPMailboxCommands(t *testing.T) {
	stream := greet +
		"* 3 EXISTS\r\n* FLAGS (\\Seen \\Deleted)\r\nRUBY0001 OK [READ-WRITE] SELECT\r\n" +
		"RUBY0002 OK EXAMINE\r\nRUBY0003 OK CREATE\r\nRUBY0004 OK DELETE\r\n" +
		"RUBY0005 OK RENAME\r\nRUBY0006 OK SUBSCRIBE\r\nRUBY0007 OK UNSUBSCRIBE\r\n"
	got := imapEval(t, `
c = Net::IMAP.new(connection: `+sock(stream)+`)
p c.select("INBOX").data.code.name
p c.examine("INBOX").name
p c.create("x").name
p c.delete("x").name
p c.rename("x", "y").name
p c.subscribe("y").name
p c.unsubscribe("y").name
p c.responses["EXISTS"]
p c.responses["FLAGS"]`)
	want := "\"READ-WRITE\"\n\"OK\"\n\"OK\"\n\"OK\"\n\"OK\"\n\"OK\"\n\"OK\"\n[3]\n[[:Seen, :Deleted]]"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestNetIMAPListAndStatus(t *testing.T) {
	stream := greet +
		"* LIST (\\HasNoChildren) \"/\" \"INBOX\"\r\n* LIST (\\Noselect) NIL \"Top\"\r\nRUBY0001 OK LIST\r\n" +
		"* LSUB () \"/\" \"INBOX\"\r\nRUBY0002 OK LSUB\r\n" +
		"* STATUS \"INBOX\" (MESSAGES 42 UNSEEN 3)\r\nRUBY0003 OK STATUS\r\n"
	got := imapEval(t, `
c = Net::IMAP.new(connection: `+sock(stream)+`)
l = c.list("", "*")
p l.length
p l[0].name
p l[0].delim
p l[0].attr
p l[1].delim
p c.lsub("", "*")[0].name
st = c.status("INBOX", ["MESSAGES", "UNSEEN"])
p st.mailbox
p st.attr["MESSAGES"]
p st.attr["UNSEEN"]`)
	want := "2\n\"INBOX\"\n\"/\"\n[:HasNoChildren]\nnil\n\"INBOX\"\n\"INBOX\"\n42\n3"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestNetIMAPFetchEnvelopeBody(t *testing.T) {
	fetch := "* 12 FETCH (FLAGS (\\Seen) UID 4827 RFC822.SIZE 998 INTERNALDATE \"17-Jul-1996 02:44:25 -0700\" " +
		"ENVELOPE (\"Wed, 17 Jul 1996\" \"subj\" ((\"Joe\" NIL \"joe\" \"ex.com\")) NIL NIL ((NIL NIL \"to\" \"ex.com\")) NIL NIL NIL \"<id>\") " +
		"BODY (\"TEXT\" \"PLAIN\" (\"CHARSET\" \"US-ASCII\") NIL NIL \"7BIT\" 3028 92))\r\n"
	got := imapEval(t, `
c = Net::IMAP.new(connection: `+sock(greet+fetch+"RUBY0001 OK FETCH\r\n")+`)
fd = c.fetch(12, ["FLAGS", "UID", "ENVELOPE", "BODY"])[0]
p fd.seqno
p fd.attr["FLAGS"]
p fd.attr["UID"]
p fd.attr["RFC822.SIZE"]
p fd.attr["INTERNALDATE"]
env = fd.attr["ENVELOPE"]
p env.subject
p env.from[0].mailbox
p env.from[0].host
p env.sender
p env.to[0].name
p env.message_id
b = fd.attr["BODY"]
p b.class
p b.media_type
p b.subtype
p b.lines
p b.multipart?
p b.param["CHARSET"]`)
	want := "12\n[:Seen]\n4827\n998\n\"17-Jul-1996 02:44:25 -0700\"\n\"subj\"\n\"joe\"\n\"ex.com\"\nnil\nnil\n\"<id>\"\n" +
		"Net::IMAP::BodyTypeText\n\"TEXT\"\n\"PLAIN\"\n92\nfalse\n\"US-ASCII\""
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestNetIMAPBodyStructureVariants(t *testing.T) {
	multi := "* 1 FETCH (BODYSTRUCTURE ((\"TEXT\" \"PLAIN\" NIL NIL NIL \"7BIT\" 10 1)(\"IMAGE\" \"GIF\" NIL NIL NIL \"BASE64\" 20) \"MIXED\"))\r\n"
	got := imapEval(t, `
c = Net::IMAP.new(connection: `+sock(greet+multi+"RUBY0001 OK FETCH\r\n")+`)
bs = c.fetch(1, "BODYSTRUCTURE")[0].attr["BODYSTRUCTURE"]
p bs.class
p bs.multipart?
p bs.subtype
p bs.parts.length
p bs.parts[0].class
p bs.parts[1].class`)
	want := "Net::IMAP::BodyTypeMultipart\ntrue\n\"MIXED\"\n2\nNet::IMAP::BodyTypeText\nNet::IMAP::BodyTypeBasic"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestNetIMAPStoreSearchExpungeCopyCapability(t *testing.T) {
	stream := greet +
		"* 1 FETCH (FLAGS (\\Seen \\Deleted))\r\nRUBY0001 OK STORE\r\n" +
		"* SEARCH 2 3 5 8\r\nRUBY0002 OK SEARCH\r\n" +
		"* 3 EXPUNGE\r\n* 3 EXPUNGE\r\nRUBY0003 OK EXPUNGE\r\n" +
		"RUBY0004 OK COPY\r\n" +
		"* CAPABILITY IMAP4rev1 IDLE\r\nRUBY0005 OK CAPABILITY\r\n"
	got := imapEval(t, `
s = `+sock(stream)+`
c = Net::IMAP.new(connection: s)
p c.store(1, "+FLAGS", [:Deleted])[0].attr["FLAGS"]
p c.search(["FROM", "joe"])
p c.expunge
p c.copy(1..3, "Archive").name
p c.capability
p s.out.include?("RUBY0001 STORE 1 +FLAGS")
p s.out.include?("RUBY0003 EXPUNGE")
p s.out.include?("RUBY0004 COPY 1:3 Archive")`)
	want := "[:Seen, :Deleted]\n[2, 3, 5, 8]\n[3, 3]\n\"OK\"\n[\"IMAP4REV1\", \"IDLE\"]\ntrue\ntrue\ntrue"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestNetIMAPUIDVariants(t *testing.T) {
	stream := greet +
		"* 1 FETCH (UID 42)\r\nRUBY0001 OK UID FETCH\r\n" +
		"* 1 FETCH (FLAGS (\\Seen))\r\nRUBY0002 OK UID STORE\r\n" +
		"RUBY0003 OK UID COPY\r\n" +
		"* SEARCH 7\r\nRUBY0004 OK UID SEARCH\r\n"
	got := imapEval(t, `
s = `+sock(stream)+`
c = Net::IMAP.new(connection: s)
p c.uid_fetch("1:3", "UID")[0].attr["UID"]
p c.uid_store([1,2], "FLAGS", [:Seen])[0].attr["FLAGS"]
p c.uid_copy("1,2,3", "X").name
p c.uid_search("ALL")
p s.out.include?("RUBY0001 UID FETCH 1:3 (UID)")
p s.out.include?("RUBY0004 UID SEARCH ALL")`)
	want := "42\n[:Seen]\n\"OK\"\n[7]\ntrue\ntrue"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestNetIMAPAppendWithLiteral(t *testing.T) {
	stream := greet + "+ go ahead\r\nRUBY0001 OK [APPENDUID 38505 3955] APPEND\r\n"
	got := imapEval(t, `
s = `+sock(stream)+`
c = Net::IMAP.new(connection: s)
r = c.append("INBOX", "From: a\r\n\r\nbody", [:Seen])
p r.name
code = r.data.code
p code.name
p code.data.class
p code.data.uidvalidity
p code.data.assigned_uids
p s.out.include?("RUBY0001 APPEND INBOX")
p s.out.include?("{15}")
p s.out.end_with?("From: a\r\n\r\nbody\r\n")`)
	want := "\"OK\"\n\"APPENDUID\"\nNet::IMAP::AppendUIDData\n38505\n\"3955\"\ntrue\ntrue\ntrue"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestNetIMAPAuthenticate(t *testing.T) {
	cases := []struct{ call, stream, mech string }{
		{`cc.authenticate("PLAIN", "joe", "pw")`, "+ \r\nRUBY0001 OK AUTH\r\n", "PLAIN"},
		{`cc.authenticate("PLAIN", "", "joe", "pw")`, "+ \r\nRUBY0001 OK AUTH\r\n", "PLAIN"},
		{`cc.authenticate("LOGIN", "joe", "pw")`, "+ VXNlcg==\r\n+ UGFzcw==\r\nRUBY0001 OK AUTH\r\n", "LOGIN"},
		{`cc.authenticate("CRAM-MD5", "joe", "pw")`, "+ PDEyMzQ1QGV4LmNvbT4=\r\nRUBY0001 OK AUTH\r\n", "CRAM-MD5"},
		{`cc.authenticate("XOAUTH2", "joe", "tok")`, "+ \r\nRUBY0001 OK AUTH\r\n", "XOAUTH2"},
	}
	for _, c := range cases {
		got := imapEval(t, `
s = `+sock(greet+c.stream)+`
cc = Net::IMAP.new(connection: s)
r = `+c.call+`
p r.name
p s.out.include?("AUTHENTICATE `+c.mech+`")`)
		if got != "\"OK\"\ntrue" {
			t.Errorf("call %q got:\n%s", c.call, got)
		}
	}
	// An unsupported mechanism raises ArgumentError.
	if got := imapRunErr(t, imapFakeSock+`require "net/imap"
Net::IMAP.new(connection: `+sock(greet+"+ \r\n")+`).authenticate("SCRAM", "a", "b")`); got != "ArgumentError" {
		t.Errorf("unsupported mech: got %q", got)
	}
	// Wrong PLAIN / user-pass arg counts raise ArgumentError.
	for _, call := range []string{
		`c.authenticate("PLAIN", "only")`,
		`c.authenticate("LOGIN", "only")`,
	} {
		src := imapFakeSock + `require "net/imap"
c = Net::IMAP.new(connection: ` + sock(greet+"+ \r\n") + `)
` + call
		if got := imapRunErr(t, src); got != "ArgumentError" {
			t.Errorf("bad creds %q: got %q", call, got)
		}
	}
	// A bad base64 CRAM-MD5 challenge raises ResponseParseError.
	if got := imapRunErr(t, imapFakeSock+`require "net/imap"
Net::IMAP.new(connection: `+sock(greet+"+ !!!not-base64!!!\r\n")+`).authenticate("CRAM-MD5", "a", "b")`); got != "Net::IMAP::ResponseParseError" {
		t.Errorf("bad challenge: got %q", got)
	}
	// A non-continuation where a continuation is expected raises ResponseParseError.
	if got := imapRunErr(t, imapFakeSock+`require "net/imap"
Net::IMAP.new(connection: `+sock(greet+"RUBY0001 OK too soon\r\n")+`).authenticate("PLAIN", "a", "b")`); got != "Net::IMAP::ResponseParseError" {
		t.Errorf("no continuation: got %q", got)
	}
}

func TestNetIMAPTaggedErrors(t *testing.T) {
	no := imapEval(t, `
c = Net::IMAP.new(connection: `+sock(greet+"RUBY0001 NO login failed\r\n")+`)
begin
  c.login("a","b")
rescue Net::IMAP::NoResponseError => e
  p e.message
end`)
	if no != "\"login failed\"" {
		t.Errorf("NO: got %q", no)
	}
	bad := imapRunErr(t, imapFakeSock+`require "net/imap"
Net::IMAP.new(connection: `+sock(greet+"RUBY0001 BAD syntax\r\n")+`).noop`)
	if bad != "Net::IMAP::BadResponseError" {
		t.Errorf("BAD: got %q", bad)
	}
}

func TestNetIMAPByeAndDisconnect(t *testing.T) {
	// A BYE greeting raises ByeResponseError.
	bye := imapRunErr(t, imapFakeSock+`require "net/imap"
Net::IMAP.new(connection: `+sock("* BYE server down\r\n")+`)`)
	if bye != "Net::IMAP::ByeResponseError" {
		t.Errorf("bye greeting: got %q", bye)
	}
	// LOGOUT: a BYE during a command marks disconnected; the tagged OK follows;
	// #disconnect then closes the seam (idempotently).
	got := imapEval(t, `
s = `+sock(greet+"* BYE logging out\r\nRUBY0001 OK LOGOUT\r\n")+`
c = Net::IMAP.new(connection: s)
p c.logout.name
p c.disconnected?
c.disconnect
p c.disconnected?
p s.closed?
c.disconnect`)
	want := "\"OK\"\ntrue\ntrue\ntrue"
	if got != want {
		t.Errorf("logout/disconnect got:\n%s\nwant:\n%s", got, want)
	}
}

func TestNetIMAPRespTextCodeVariants(t *testing.T) {
	stream := greet +
		"RUBY0001 OK [UIDVALIDITY 3857529045] done\r\n" +
		"RUBY0002 OK [PERMANENTFLAGS (\\Deleted \\Seen \\*)] done\r\n" +
		"RUBY0003 OK [CAPABILITY IMAP4rev1 STARTTLS] done\r\n" +
		"RUBY0004 OK [BADCHARSET (utf-8 us-ascii)] done\r\n" +
		"RUBY0005 OK [ALERT] system going down\r\n" +
		"RUBY0006 OK [COPYUID 1 1:2 3:4] done\r\n" +
		"RUBY0007 OK [UNKNOWNCODE some text] done\r\n"
	got := imapEval(t, `
c = Net::IMAP.new(connection: `+sock(stream)+`)
p c.noop.data.code.data
p c.noop.data.code.data
p c.noop.data.code.data
p c.noop.data.code.data
r5 = c.noop.data.code
p r5.name
p r5.data
r6 = c.noop.data.code
p r6.data.class
p r6.data.uidvalidity
p r6.data.source_uids
p r6.data.assigned_uids
p c.noop.data.code.data`)
	want := "3857529045\n[:Deleted, :Seen, :*]\n[\"IMAP4REV1\", \"STARTTLS\"]\n[\"utf-8\", \"us-ascii\"]\n" +
		"\"ALERT\"\nnil\nNet::IMAP::CopyUIDData\n1\n\"1:2\"\n\"3:4\"\n\"some text\""
	if got != want {
		t.Errorf("code variants got:\n%s\nwant:\n%s", got, want)
	}
}

func TestNetIMAPUTF7Helpers(t *testing.T) {
	// Round-trip: decode(encode) equals the original for a non-ASCII mailbox.
	rt := imapEval(t, `
orig = "Ph\xC3\xA9nix".force_encoding("UTF-8")
enc = Net::IMAP.encode_utf7(orig)
p enc.include?("&")
p Net::IMAP.decode_utf7(enc) == orig`)
	if rt != "true\ntrue" {
		t.Errorf("utf7 round-trip: %q", rt)
	}
}

func TestNetIMAPResponsesAndGreetingAccessors(t *testing.T) {
	got := imapEval(t, `
c = Net::IMAP.new(connection: `+sock(greet+"* 5 EXISTS\r\n* 2 RECENT\r\nRUBY0001 OK NOOP\r\n")+`)
c.noop
h = c.responses
p h["EXISTS"]
p h["RECENT"]
p h.key?("OK")`)
	want := "[5]\n[2]\ntrue"
	if got != want {
		t.Errorf("responses got:\n%s\nwant:\n%s", got, want)
	}
}

func TestNetIMAPDataFormatError(t *testing.T) {
	if got := imapRunErr(t, imapFakeSock+`require "net/imap"
Net::IMAP.new(connection: `+sock(greet)+`).fetch(-1, "UID")`); got != "Net::IMAP::DataFormatError" {
		t.Errorf("negative seq: got %q", got)
	}
}

func TestNetIMAPParseErrors(t *testing.T) {
	// A malformed response line raises ResponseParseError.
	if got := imapRunErr(t, imapFakeSock+`require "net/imap"
Net::IMAP.new(connection: `+sock(greet+"* 1 BOGUS junk\r\n")+`).noop`); got != "Net::IMAP::ResponseParseError" {
		t.Errorf("bad line: got %q", got)
	}
	// A truncated stream (EOF before a full response) raises Net::IMAP::Error.
	if got := imapRunErr(t, imapFakeSock+`require "net/imap"
Net::IMAP.new(connection: `+sock(greet+"RUBY0001 OK partial")+`).noop`); got != "Net::IMAP::Error" {
		t.Errorf("truncated: got %q", got)
	}
	// A non-untagged greeting raises ResponseParseError.
	if got := imapRunErr(t, imapFakeSock+`require "net/imap"
Net::IMAP.new(connection: `+sock("RUBY0001 OK notgreeting\r\n")+`)`); got != "Net::IMAP::ResponseParseError" {
		t.Errorf("bad greeting: got %q", got)
	}
	// An unexpected continuation mid-command raises ResponseParseError.
	if got := imapRunErr(t, imapFakeSock+`require "net/imap"
Net::IMAP.new(connection: `+sock(greet+"+ surprise\r\n")+`).noop`); got != "Net::IMAP::ResponseParseError" {
		t.Errorf("stray continuation: got %q", got)
	}
}

func TestNetIMAPArityErrors(t *testing.T) {
	for _, call := range []string{
		`c.login("a")`, `c.select`, `c.list("")`, `c.status("x")`,
		`c.fetch(1)`, `c.store(1, "x")`, `c.copy(1)`, `c.append("x")`,
		`c.authenticate`, `Net::IMAP.encode_utf7("a", "b")`, `Net::IMAP.decode_utf7`,
	} {
		src := imapFakeSock + `require "net/imap"
c = Net::IMAP.new(connection: ` + sock(greet) + `)
` + call
		if got := imapRunErr(t, src); got != "ArgumentError" {
			t.Errorf("call %q: got %q, want ArgumentError", call, got)
		}
	}
}

// --- Go-level unit tests for defensive branches ----------------------------

func TestIMAPObjStringSurface(t *testing.T) {
	o := &IMAPObj{}
	if o.ToS() != "#<Net::IMAP>" || o.Inspect() != "#<Net::IMAP>" || !o.Truthy() {
		t.Errorf("surface: ToS=%q Inspect=%q Truthy=%v", o.ToS(), o.Inspect(), o.Truthy())
	}
}

func TestIMAPMappingDefaults(t *testing.T) {
	vm := New(io.Discard)
	if !object.IsNil(vm.imapDataValue(struct{}{})) {
		t.Error("imapDataValue default should be nil")
	}
	if !object.IsNil(vm.imapCodeData(nil)) {
		t.Error("imapCodeData default should be nil")
	}
	if !object.IsNil(vm.imapAttrValue(struct{}{})) {
		t.Error("imapAttrValue default should be nil")
	}
	if !object.IsNil(vm.imapParams(nil)) {
		t.Error("imapParams(nil) should be nil")
	}
	if !object.IsNil(vm.imapAddressList(nil)) {
		t.Error("imapAddressList(nil) should be nil")
	}
	if !object.IsNil(vm.imapRespText(nil)) {
		t.Error("imapRespText(nil) should be nil")
	}
	// The string arm (defensive; no untagged Data is a bare string).
	if s, ok := vm.imapDataValue("hi").(*object.String); !ok || s.Str() != "hi" {
		t.Error("imapDataValue string arm")
	}
}

func TestIMAPArgCoercion(t *testing.T) {
	// imapFlags accepts a single (non-Array) flag as well as an Array.
	if fs := imapFlags(object.Symbol("Seen")); len(fs) != 1 || fs[0] != imap.Flag("Seen") {
		t.Errorf("imapFlags single: %v", fs)
	}
	// imapAtts accepts a single (non-Array) attribute.
	if as := imapAtts(object.NewString("UID")); len(as) != 1 || as[0] != "UID" {
		t.Errorf("imapAtts single: %v", as)
	}
	// imapSearchArgs maps Integer / Array / String keys to the right argument
	// types.
	args := imapSearchArgs([]object.Value{
		object.Integer(5),
		object.NewArrayFromSlice([]object.Value{object.Integer(1)}),
		object.NewString("ALL"),
	})
	if len(args) != 3 {
		t.Fatalf("imapSearchArgs len: %d", len(args))
	}
	if n, ok := args[0].(int64); !ok || n != 5 {
		t.Errorf("imapSearchArgs int arm: %v", args[0])
	}
	if _, ok := args[1].(*imap.SequenceSet); !ok {
		t.Errorf("imapSearchArgs array arm: %T", args[1])
	}
	if s, ok := args[2].(string); !ok || s != "ALL" {
		t.Errorf("imapSearchArgs string arm: %v", args[2])
	}
}

func TestIMAPBodyTypeMessageAndBasic(t *testing.T) {
	vm := New(io.Discard)
	msg := vm.imapBodyStructure(&imap.BodyStructure{MediaType: "MESSAGE", Subtype: "RFC822", Lines: 5})
	if vm.classOf(msg).name != "Net::IMAP::BodyTypeMessage" {
		t.Errorf("message body: %s", vm.classOf(msg).name)
	}
	basic := vm.imapBodyStructure(&imap.BodyStructure{MediaType: "MESSAGE", Subtype: "OTHER"})
	if vm.classOf(basic).name != "Net::IMAP::BodyTypeBasic" {
		t.Errorf("message/other body: %s", vm.classOf(basic).name)
	}
}

func TestIMAPHelperEdgeCases(t *testing.T) {
	if respTextStr(nil) != "" {
		t.Error("respTextStr(nil)")
	}
	if untaggedText(&imap.UntaggedResponse{Data: int64(3)}) != "" {
		t.Error("untaggedText non-ResponseText")
	}
	if !object.IsNil(imapNilGreeting(nil)) {
		t.Error("imapNilGreeting(nil)")
	}
	if !object.IsNil(imapFirst(object.NewArrayFromSlice(nil))) {
		t.Error("imapFirst empty")
	}
	if a, ok := imapFirstOrEmpty(object.NewArrayFromSlice(nil)).(*object.Array); !ok || len(a.Elems) != 0 {
		t.Error("imapFirstOrEmpty empty")
	}
	if imapConnArg(nil) != nil {
		t.Error("imapConnArg(nil)")
	}
	if imapConnArg([]object.Value{object.NewString("host")}) != nil {
		t.Error("imapConnArg(host string)")
	}
	if got := imapSeqItems(object.Float(1.5)); len(got) != 1 || got[0].(int64) != 0 {
		t.Errorf("imapSeqItems default: %v", got)
	}
	if imapSeqNum("bad") != imap.Star {
		t.Error("imapSeqNum bad token")
	}
	if imapSeqNum("*") != imap.Star {
		t.Error("imapSeqNum star")
	}
	if imapStrArg(object.Symbol("Seen")) != "Seen" {
		t.Errorf("imapStrArg symbol: %q", imapStrArg(object.Symbol("Seen")))
	}
	if imapSeqBound(object.NilV) != imap.Star {
		t.Error("imapSeqBound nil")
	}
}

func TestIMAPMustCmdError(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("mustCmd with error should raise")
		}
	}()
	mustCmd(imap.Command{}, imap.ErrInvalidData)
}
