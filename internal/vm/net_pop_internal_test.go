// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// fakePOP is a Ruby duck-typed POP3 socket (write / gets) backing the net/pop host
// seam: writes are captured in @sent, gets drains the canned server script line by
// line (each line up to and including "\n"). It is the injected transport the
// Net::POP3 binding drives its protocol over, so the whole suite is deterministic
// and network-free.
const fakePOP = `
class FakePOP
  def initialize(script) ; @buf = script.dup ; @sent = "".dup ; end
  def write(s) ; @sent << s ; s.bytesize ; end
  def gets
    return nil if @buf.empty?
    i = @buf.index("\n")
    if i
      line = @buf[0..i]
      rest = @buf[(i + 1)..-1]
      @buf = rest.nil? ? "" : rest
    else
      line = @buf
      @buf = ""
    end
    line
  end
  def sent ; @sent ; end
end
`

// TestNetPOPFeatureAndTree covers the require "net/pop" load flag and the
// Net::POP3 / Net::POPMail classes plus the error hierarchy.
func TestNetPOPFeatureAndTree(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p require "net/pop"`, "true\n"},
		{`require "net/pop"; p require "net/pop"`, "false\n"},
		{`require "net/pop"; p Net::POP3.is_a?(Class)`, "true\n"},
		{`require "net/pop"; p Net::POPMail.is_a?(Class)`, "true\n"},
		{`require "net/pop"; p Net::POPError < StandardError`, "true\n"},
		{`require "net/pop"; p Net::POPBadResponse < Net::POPError`, "true\n"},
		{`require "net/pop"; p Net::POPAuthenticationError < StandardError`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestNetPOPStartAuth covers Net::POP3.new + #start (USER/PASS), the accessors, the
// written command bytes and the wrapper #inspect / truthiness.
func TestNetPOPStartAuth(t *testing.T) {
	src := fakePOP + `require "net/pop"
io = FakePOP.new("+OK ready <a@b.com>\r\n+OK\r\n+OK\r\n")
pop = Net::POP3.new("mail.example.com", 995, connection: io)
p pop.address
p pop.port
p pop.apop?
p pop.started?
p(pop ? :y : :n)
pop.start("alice", "secret")
p pop.started?
p pop.active?
p io.sent.include?("USER alice\r\n")
p io.sent.include?("PASS secret\r\n")
p pop._connection.equal?(io)
p pop.inspect`
	want := "\"mail.example.com\"\n995\nfalse\nfalse\n:y\ntrue\ntrue\ntrue\ntrue\ntrue\n\"#<Net::POP3 mail.example.com>\"\n"
	if got := eval(t, src); got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}

// TestNetPOPStartBlock covers #start with a block (yield then finish/QUIT), the
// default port, and empty account/password args (popStrAt absent branch).
func TestNetPOPStartBlock(t *testing.T) {
	src := fakePOP + `require "net/pop"
io = FakePOP.new("+OK <a@b>\r\n+OK\r\n+OK\r\n+OK bye\r\n")
r = Net::POP3.new("h", connection: io)
p r.port
res = r.start { |c| c.started? }
p res
p r.started?
p io.sent.include?("QUIT\r\n")`
	want := "110\ntrue\nfalse\ntrue\n"
	if got := eval(t, src); got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}

// TestNetPOPClassStart covers the Net::POP3.start class convenience (new + start +
// block + finish) with positional account/password.
func TestNetPOPClassStart(t *testing.T) {
	src := fakePOP + `require "net/pop"
io = FakePOP.new("+OK <a@b>\r\n+OK\r\n+OK\r\n+OK bye\r\n")
out = Net::POP3.start("host", 110, "u", "pw", connection: io) { |pop| p pop.address; :done }
p out
p io.sent.include?("USER u\r\n")`
	want := "\"host\"\n:done\ntrue\n"
	if got := eval(t, src); got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}

// TestNetPOPApop covers APOP authentication: the greeting stamp drives an APOP
// digest line rather than USER/PASS.
func TestNetPOPApop(t *testing.T) {
	src := fakePOP + `require "net/pop"
io = FakePOP.new("+OK POP3 <1896.697@x.example>\r\n+OK maildrop\r\n")
pop = Net::POP3.new("h", 110, true, connection: io)
p pop.apop?
pop.start("u", "p")
p pop.started?
p io.sent.start_with?("APOP u ")`
	want := "true\ntrue\ntrue\n"
	if got := eval(t, src); got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}

// TestNetPOPMails covers #mails (LIST), the memoisation, #each_mail, the POPMail
// accessors, #pop (RETR + dot-unstuffing), #header/#top (TOP), #unique_id (UIDL n
// then the cached path) and #delete (DELE), plus a #pop block.
func TestNetPOPMails(t *testing.T) {
	src := fakePOP + `require "net/pop"
io = FakePOP.new(
  "+OK <a@b>\r\n+OK\r\n+OK\r\n" +
  "+OK 2 messages\r\n1 120\r\n2 200\r\n.\r\n" +
  "+OK follows\r\nFrom: a\r\n..dotted\r\nbody\r\n.\r\n" +
  "+OK\r\nFrom: a\r\n.\r\n" +
  "+OK\r\nline1\r\n.\r\n" +
  "+OK 1 UID1\r\n" +
  "+OK grab\r\nFrom: a\r\nbody2\r\n.\r\n" +
  "+OK\r\n")
pop = Net::POP3.new("h", connection: io)
pop.start("u", "p")
ms = pop.mails
p ms.length
p ms[0].equal?(pop.mails[0])
m = ms[0]
p(m ? :y : :n)
p m.number
p m.length
p m.size
p m.deleted?
p m.inspect
body = m.pop
p body.include?("body")
p body.include?(".dotted")
p m.header.include?("From: a")
p m.top(5).include?("line1")
p m.unique_id
p m.unique_id
got = nil
m.pop { |chunk| got = chunk.include?("body2") }
p got
ms[1].delete
p ms[1].deleted?`
	want := "2\ntrue\n:y\n1\n120\n120\nfalse\n\"#<Net::POPMail 1>\"\ntrue\ntrue\ntrue\ntrue\n\"1\"\n\"1\"\ntrue\ntrue\n"
	if got := eval(t, src); got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}

// TestNetPOPSessionCommands covers #n_mails / #n_bytes (STAT), #noop (NOOP),
// #stls (STLS) and #reset (RSET).
func TestNetPOPSessionCommands(t *testing.T) {
	src := fakePOP + `require "net/pop"
io = FakePOP.new("+OK <a@b>\r\n+OK\r\n+OK\r\n+OK 3 450\r\n+OK 3 450\r\n+OK\r\n+OK\r\n+OK\r\n")
pop = Net::POP3.new("h", connection: io)
pop.start("u", "p")
p pop.n_mails
p pop.n_bytes
pop.noop
pop.stls
pop.reset
p io.sent.include?("NOOP\r\n")
p io.sent.include?("STLS\r\n")
p io.sent.include?("RSET\r\n")`
	want := "3\n450\ntrue\ntrue\ntrue\n"
	if got := eval(t, src); got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}

// TestNetPOPUidsAndDeleteAll covers #each_mail (fetch), #set_all_uids (UIDL over
// the memoised list, feeding the cached #unique_id), #delete_all (with a block)
// and #each.
func TestNetPOPUidsAndDeleteAll(t *testing.T) {
	src := fakePOP + `require "net/pop"
io = FakePOP.new(
  "+OK <a@b>\r\n+OK\r\n+OK\r\n" +
  "+OK\r\n1 100\r\n2 200\r\n.\r\n" +
  "+OK\r\n1 UIDA\r\n2 UIDB\r\n.\r\n" +
  "+OK\r\n+OK\r\n")
pop = Net::POP3.new("h", connection: io)
pop.start("u", "p")
nums = []
pop.each_mail { |m| nums << m.number }
p nums
each2 = []
pop.each { |m| each2 << m.number }
p each2
pop.set_all_uids
p pop.mails[0].unique_id
seen = []
pop.delete_all { |m| seen << m.number }
p seen
p pop.mails[0].deleted?`
	want := "[1, 2]\n[1, 2]\n\"UIDA\"\n[1, 2]\ntrue\n"
	if got := eval(t, src); got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}

// TestNetPOPDeleteAllNoBlock covers #delete_all without a block (the blk==nil
// branch) and #top with no argument (popIntAt default).
func TestNetPOPDeleteAllNoBlock(t *testing.T) {
	src := fakePOP + `require "net/pop"
io = FakePOP.new(
  "+OK <a@b>\r\n+OK\r\n+OK\r\n" +
  "+OK\r\n1 100\r\n.\r\n" +
  "+OK\r\nhdr\r\n.\r\n" +
  "+OK\r\n")
pop = Net::POP3.new("h", connection: io)
pop.start("u", "p")
p pop.mails[0].top.include?("hdr")
pop.delete_all
p pop.mails[0].deleted?`
	want := "true\ntrue\n"
	if got := eval(t, src); got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}

// TestNetPOPArgErrors covers the missing-seam ArgumentError paths for both .new and
// .start, the empty-arglist branch, a keyword Hash without a connection, the
// socket:/conn:/string-key connection forms, and popIntAt's non-Integer default.
func TestNetPOPArgErrors(t *testing.T) {
	cases := []struct{ src, want string }{
		{fakePOP + `require "net/pop"
begin; Net::POP3.new("h"); rescue ArgumentError; p :a1; end`, ":a1\n"},
		{fakePOP + `require "net/pop"
begin; Net::POP3.new; rescue ArgumentError; p :a2; end`, ":a2\n"},
		{fakePOP + `require "net/pop"
begin; Net::POP3.new("h", 110, {foo: 1}); rescue ArgumentError; p :a3; end`, ":a3\n"},
		{fakePOP + `require "net/pop"
begin; Net::POP3.start("h"); rescue ArgumentError; p :a4; end`, ":a4\n"},
		{fakePOP + `require "net/pop"
p Net::POP3.new("h", socket: FakePOP.new("")).address`, "\"h\"\n"},
		{fakePOP + `require "net/pop"
p Net::POP3.new("h", conn: FakePOP.new("")).address`, "\"h\"\n"},
		{fakePOP + `require "net/pop"
p Net::POP3.new("h", "connection" => FakePOP.new("")).address`, "\"h\"\n"},
		{fakePOP + `require "net/pop"
p Net::POP3.new("h", "notint", connection: FakePOP.new("")).port`, "110\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestNetPOPEachNoBlock covers #each_mail / #each raising LocalJumpError without a
// block (before any I/O).
func TestNetPOPEachNoBlock(t *testing.T) {
	cases := []struct{ src, want string }{
		{fakePOP + `require "net/pop"
pop = Net::POP3.new("h", connection: FakePOP.new(""))
begin; pop.each_mail; rescue LocalJumpError; p :nb1; end`, ":nb1\n"},
		{fakePOP + `require "net/pop"
pop = Net::POP3.new("h", connection: FakePOP.new(""))
begin; pop.each; rescue LocalJumpError; p :nb2; end`, ":nb2\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestNetPOPErrorMapping covers popRaise's four branches (POPAuthenticationError /
// POPError / POPBadResponse / the EOF default) and every command error path.
func TestNetPOPErrorMapping(t *testing.T) {
	start := `"+OK <a@b>\r\n+OK\r\n+OK\r\n"`
	cases := []struct{ src, want string }{
		// USER/PASS auth failure -> POPAuthenticationError, message verbatim.
		{fakePOP + `require "net/pop"
io = FakePOP.new("+OK <a@b>\r\n-ERR nope\r\n")
begin; Net::POP3.new("h", connection: io).start("u", "p"); rescue Net::POPAuthenticationError => e; p e.message; end`, "\"-ERR nope\"\n"},
		// Greeting "-ERR" -> POPError.
		{fakePOP + `require "net/pop"
io = FakePOP.new("-ERR down\r\n")
begin; Net::POP3.new("h", connection: io).start("u", "p"); rescue Net::POPError => e; p e.message; end`, "\"-ERR down\"\n"},
		// APOP requested but no stamp in greeting -> POPAuthenticationError.
		{fakePOP + `require "net/pop"
io = FakePOP.new("+OK ready\r\n")
begin; Net::POP3.new("h", 110, true, connection: io).start("u", "p"); rescue Net::POPAuthenticationError => e; p e.message; end`, "\"not APOP server; cannot login\"\n"},
		// Malformed STAT -> POPBadResponse.
		{fakePOP + `require "net/pop"
io = FakePOP.new("+OK <a@b>\r\n+OK\r\n+OK\r\n+OK garbage\r\n")
pop = Net::POP3.new("h", connection: io); pop.start("u", "p")
begin; pop.n_mails; rescue Net::POPBadResponse => e; p e.class; end`, "Net::POPBadResponse\n"},
		// Empty script -> gets returns nil -> io.EOF -> POPError (default branch).
		{fakePOP + `require "net/pop"
io = FakePOP.new("")
begin; Net::POP3.new("h", connection: io).start("u", "p"); rescue Net::POPError; p :eof; end`, ":eof\n"},
		// QUIT "-ERR" -> #finish raises POPError.
		{fakePOP + `require "net/pop"
io = FakePOP.new("+OK <a@b>\r\n+OK\r\n+OK\r\n-ERR quit fail\r\n")
pop = Net::POP3.new("h", connection: io); pop.start("u", "p")
begin; pop.finish; rescue Net::POPError; p :qf; end`, ":qf\n"},
		// LIST "-ERR" -> #mails raises.
		{fakePOP + `require "net/pop"
io = FakePOP.new(` + start + ` + "-ERR no list\r\n")
pop = Net::POP3.new("h", connection: io); pop.start("u", "p")
begin; pop.mails; rescue Net::POPError; p :le; end`, ":le\n"},
		// UIDL "-ERR" -> #set_all_uids raises.
		{fakePOP + `require "net/pop"
io = FakePOP.new(` + start + ` + "+OK\r\n1 100\r\n.\r\n" + "-ERR nouidl\r\n")
pop = Net::POP3.new("h", connection: io); pop.start("u", "p"); pop.mails
begin; pop.set_all_uids; rescue Net::POPError; p :ue; end`, ":ue\n"},
		// DELE "-ERR" -> POPMail#delete raises.
		{fakePOP + `require "net/pop"
io = FakePOP.new(` + start + ` + "+OK\r\n1 100\r\n.\r\n" + "-ERR nodele\r\n")
pop = Net::POP3.new("h", connection: io); pop.start("u", "p")
begin; pop.mails[0].delete; rescue Net::POPError; p :de; end`, ":de\n"},
		// RETR "-ERR" -> POPMail#pop raises.
		{fakePOP + `require "net/pop"
io = FakePOP.new(` + start + ` + "+OK\r\n1 100\r\n.\r\n" + "-ERR noretr\r\n")
pop = Net::POP3.new("h", connection: io); pop.start("u", "p")
begin; pop.mails[0].pop; rescue Net::POPError; p :re; end`, ":re\n"},
		// TOP "-ERR" -> POPMail#header raises.
		{fakePOP + `require "net/pop"
io = FakePOP.new(` + start + ` + "+OK\r\n1 100\r\n.\r\n" + "-ERR notop\r\n")
pop = Net::POP3.new("h", connection: io); pop.start("u", "p")
begin; pop.mails[0].header; rescue Net::POPError; p :he; end`, ":he\n"},
		// TOP "-ERR" -> POPMail#top raises.
		{fakePOP + `require "net/pop"
io = FakePOP.new(` + start + ` + "+OK\r\n1 100\r\n.\r\n" + "-ERR notop\r\n")
pop = Net::POP3.new("h", connection: io); pop.start("u", "p")
begin; pop.mails[0].top(3); rescue Net::POPError; p :te; end`, ":te\n"},
		// UIDL n "-ERR" -> POPMail#unique_id raises (uncached fetch).
		{fakePOP + `require "net/pop"
io = FakePOP.new(` + start + ` + "+OK\r\n1 100\r\n.\r\n" + "-ERR nouidl\r\n")
pop = Net::POP3.new("h", connection: io); pop.start("u", "p")
begin; pop.mails[0].unique_id; rescue Net::POPError; p :uie; end`, ":uie\n"},
		// reset RSET "-ERR" -> POPError.
		{fakePOP + `require "net/pop"
io = FakePOP.new(` + start + ` + "-ERR norst\r\n")
pop = Net::POP3.new("h", connection: io); pop.start("u", "p")
begin; pop.reset; rescue Net::POPError; p :rr; end`, ":rr\n"},
		// noop NOOP "-ERR" -> POPError.
		{fakePOP + `require "net/pop"
io = FakePOP.new(` + start + ` + "-ERR nonoop\r\n")
pop = Net::POP3.new("h", connection: io); pop.start("u", "p")
begin; pop.noop; rescue Net::POPError; p :nn; end`, ":nn\n"},
		// stls STLS "-ERR" -> POPError.
		{fakePOP + `require "net/pop"
io = FakePOP.new(` + start + ` + "-ERR nostls\r\n")
pop = Net::POP3.new("h", connection: io); pop.start("u", "p")
begin; pop.stls; rescue Net::POPError; p :ss; end`, ":ss\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}
