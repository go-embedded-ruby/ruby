// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// pgHelpers defines a Ruby FakeSock plus byte-framing helpers that build the
// PostgreSQL v3 backend messages the pg binding consumes. Framing is: a type
// byte, a big-endian Int32 length (covering the length field and the body), then
// the body. The startup/auth preamble and per-message builders mirror the wire
// format the go-ruby-pg library parses.
const pgHelpers = `
class FakeSock
  def initialize(reply) ; @in = reply.dup.force_encoding("ASCII-8BIT") ; @pos = 0 ; @out = "".b ; end
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
end

def pg_frame(type, body)
  body = body.b
  (type + [body.bytesize + 4].pack("N") + body).b
end

def pg_auth_ok ; pg_frame("R", [0].pack("N")) ; end       # AuthenticationOk
def pg_ready(st = "I") ; pg_frame("Z", st.b) ; end        # ReadyForQuery
def pg_backend_key ; pg_frame("K", [1234, 5678].pack("NN")) ; end
def pg_param(name, val) ; pg_frame("S", (name.b + "\x00".b + val.b + "\x00".b)) ; end
def pg_cmd(tag) ; pg_frame("C", (tag.b + "\x00".b)) ; end
def pg_empty ; pg_frame("I", "".b) ; end

# RowDescription with a list of [name, oid] columns.
def pg_row_desc(cols)
  body = [cols.size].pack("n").b
  cols.each do |name, oid|
    body << name.b << "\x00".b
    body << [0].pack("N")          # table oid
    body << [0].pack("n")          # column attnum
    body << [oid].pack("N")        # type oid
    body << [-1].pack("n")         # type size
    body << [-1].pack("N")         # type modifier
    body << [0].pack("n")          # format (text)
  end
  pg_frame("T", body)
end

# DataRow with a list of column value strings (nil = SQL NULL).
def pg_data_row(vals)
  body = [vals.size].pack("n").b
  vals.each do |v|
    if v.nil?
      body << [-1].pack("N")
    else
      v = v.b
      body << [v.bytesize].pack("N") << v
    end
  end
  pg_frame("D", body)
end

def pg_error(sqlstate, msg)
  body = "S".b + "ERROR\x00".b + "C".b + sqlstate.b + "\x00".b + "M".b + msg.b + "\x00".b + "\x00".b
  pg_frame("E", body)
end

def pg_parse_complete ; pg_frame("1", "".b) ; end
def pg_bind_complete ; pg_frame("2", "".b) ; end
`

// pgConnect returns Ruby that opens a PG connection over a FakeSock whose reply
// stream starts with the given preamble (auth handshake) plus more bytes.
func pgConnectSrc(reply, body string) string {
	return pgHelpers + "\nrequire \"pg\"\n" +
		"reply = " + reply + "\n" +
		"sock = FakeSock.new(reply)\n" +
		"conn = PG.connect(connection: sock, user: \"me\", dbname: \"db\")\n" + body
}

// TestPGConstants covers the PG module and its error tree.
func TestPGConstants(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "pg"; p require "pg"`, "false\n"},
		{`p require "pg"`, "true\n"},
		{`require "pg"; p PG.is_a?(Module)`, "true\n"},
		{`require "pg"; p PG::Error < StandardError`, "true\n"},
		{`require "pg"; p PG::ConnectionBad < PG::Error`, "true\n"},
		{`require "pg"; p PG::ServerError < PG::Error`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestPGConnectAndExec covers the StartupMessage + AuthenticationOk handshake and
// a simple #exec returning a PG::Result, with the row/field accessors.
func TestPGConnectAndExec(t *testing.T) {
	// Preamble: AuthOk, a ParameterStatus, BackendKeyData, ReadyForQuery.
	// Then a query result: RowDescription(2 cols), 2 DataRows, CommandComplete,
	// ReadyForQuery.
	reply := `pg_auth_ok + pg_param("server_version", "16.0") + pg_backend_key + pg_ready +
	  pg_row_desc([["id", 23], ["name", 25]]) +
	  pg_data_row(["1", "alice"]) + pg_data_row(["2", nil]) +
	  pg_cmd("SELECT 2") + pg_ready`
	body := `res = conn.exec("SELECT id, name FROM users")
p res.class
p res.ntuples
p res.nfields
p res.fields
p res.getvalue(0, 0)
p res.getvalue(0, 1)
p res.getisnull(1, 1)
p res[0]
p res.cmd_tuples
p res.cmd_status
p conn.socket_io.equal?(sock)`
	want := strings.Join([]string{
		"PG::Result", "2", "2",
		`["id", "name"]`,
		`1`,       // int4 decodes to Integer
		`"alice"`, // text
		"true",    // (1,1) is NULL
		`{"id" => 1, "name" => "alice"}`,
		"2", `"SELECT 2"`, "true", "",
	}, "\n")
	if got := eval(t, pgConnectSrc(reply, body)); got != want {
		t.Errorf("exec\n got=%q\nwant=%q", got, want)
	}
}

// TestPGExecParams covers exec_params over the extended protocol.
func TestPGExecParams(t *testing.T) {
	reply := `pg_auth_ok + pg_ready +
	  pg_parse_complete + pg_bind_complete +
	  pg_row_desc([["n", 23]]) + pg_data_row(["42"]) + pg_cmd("SELECT 1") + pg_ready`
	body := `res = conn.exec_params("SELECT $1::int", [42])
p res.ntuples
p res.getvalue(0, 0)
p sock.out.include?("SELECT $1::int")`
	want := "1\n42\ntrue\n"
	if got := eval(t, pgConnectSrc(reply, body)); got != want {
		t.Errorf("exec_params got=%q want=%q", got, want)
	}
}

// TestPGPrepareExecPrepared covers prepare + exec_prepared.
func TestPGPrepareExecPrepared(t *testing.T) {
	reply := `pg_auth_ok + pg_ready +
	  pg_parse_complete + pg_ready +
	  pg_bind_complete + pg_row_desc([["n", 23]]) + pg_data_row(["7"]) + pg_cmd("SELECT 1") + pg_ready`
	body := `conn.prepare("st1", "SELECT $1::int")
res = conn.exec_prepared("st1", [7])
p res.getvalue(0, 0)`
	if got := eval(t, pgConnectSrc(reply, body)); got != "7\n" {
		t.Errorf("prepare/exec_prepared got=%q", got)
	}
}

// TestPGExecError covers a server ErrorResponse raising PG::Error.
func TestPGExecError(t *testing.T) {
	reply := `pg_auth_ok + pg_ready + pg_error("42P01", "no table") + pg_ready`
	body := `begin
  conn.exec("SELECT bad")
rescue PG::Error => e
  p e.message.include?("no table")
end`
	if got := eval(t, pgConnectSrc(reply, body)); got != "true\n" {
		t.Errorf("exec error got=%q", got)
	}
}

// TestPGEscapes covers the pure string helpers.
func TestPGEscapes(t *testing.T) {
	reply := `pg_auth_ok + pg_ready`
	body := `p conn.escape_string("a'b")
p conn.quote_ident("we ird")
p conn.escape_literal("x'y")
p conn.escape_identifier("t")`
	want := "\"a''b\"\n\"\\\"we ird\\\"\"\n\"'x''y'\"\n\"\\\"t\\\"\"\n"
	if got := eval(t, pgConnectSrc(reply, body)); got != want {
		t.Errorf("escapes got=%q want=%q", got, want)
	}
}

// TestPGEach covers PG::Result#each, #values, #fname, #fnumber, #num_tuples.
func TestPGEach(t *testing.T) {
	reply := `pg_auth_ok + pg_ready +
	  pg_row_desc([["a", 23], ["b", 25]]) + pg_data_row(["1", "x"]) + pg_data_row(["2", "y"]) +
	  pg_cmd("SELECT 2") + pg_ready`
	body := `res = conn.exec("SELECT a, b FROM t")
res.each { |row| p row }
p res.values
p res.fname(1)
p res.fnumber("b")
p res.num_tuples
p res.num_fields`
	want := strings.Join([]string{
		`{"a" => 1, "b" => "x"}`,
		`{"a" => 2, "b" => "y"}`,
		`[[1, "x"], [2, "y"]]`,
		`"b"`, "1", "2", "2", "",
	}, "\n")
	if got := eval(t, pgConnectSrc(reply, body)); got != want {
		t.Errorf("each got=%q want=%q", got, want)
	}
}

// TestPGQueryBlock covers #exec / #query with a block yielding the result, and
// #query as an alias of #exec.
func TestPGQueryBlock(t *testing.T) {
	reply := `pg_auth_ok + pg_ready +
	  pg_row_desc([["n", 23]]) + pg_data_row(["9"]) + pg_cmd("SELECT 1") + pg_ready`
	body := `out = conn.query("SELECT 9") { |r| r.getvalue(0, 0) }
p out`
	if got := eval(t, pgConnectSrc(reply, body)); got != "9\n" {
		t.Errorf("query block got=%q", got)
	}
}

// TestPGFinishClear covers #finish (Terminate) and #clear (a no-op).
func TestPGFinishClear(t *testing.T) {
	reply := `pg_auth_ok + pg_ready +
	  pg_row_desc([["n", 23]]) + pg_data_row(["1"]) + pg_cmd("SELECT 1") + pg_ready`
	body := `res = conn.exec("SELECT 1")
p res.clear
p conn.finish
p sock.out.include?("X")`
	if got := eval(t, pgConnectSrc(reply, body)); got != "nil\nnil\ntrue\n" {
		t.Errorf("finish/clear got=%q", got)
	}
}

// TestPGNoConnection covers PG.connect without a connection raising ArgumentError.
func TestPGNoConnection(t *testing.T) {
	if err := runErr(t, `require "pg"; PG.connect(user: "x")`); err == nil ||
		!strings.Contains(err.Error(), "connection") {
		t.Errorf("want connection ArgumentError, got %v", err)
	}
}

// TestPGHandshakeError covers a startup ErrorResponse (auth failure) raising
// PG::Error during PG.connect.
func TestPGHandshakeError(t *testing.T) {
	src := pgHelpers + `
require "pg"
sock = FakeSock.new(pg_error("28P01", "auth failed"))
begin
  PG.connect(connection: sock, user: "me", password: "bad")
rescue PG::Error => e
  p e.message.include?("auth failed")
end`
	if got := eval(t, src); got != "true\n" {
		t.Errorf("handshake error got=%q", got)
	}
}
