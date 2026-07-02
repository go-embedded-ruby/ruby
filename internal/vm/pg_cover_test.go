// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// TestPGToS covers the wrapper #to_s / #inspect / truthiness for both
// PG::Connection and PG::Result.
func TestPGToS(t *testing.T) {
	reply := `pg_auth_ok + pg_ready +
	  pg_row_desc([["n", 23]]) + pg_data_row(["1"]) + pg_cmd("SELECT 1") + pg_ready`
	body := `p conn.to_s
p conn.inspect
p(conn ? :y : :n)
res = conn.exec("SELECT 1")
p res.to_s
p res.inspect
p(res ? :y : :n)`
	want := "\"#<PG::Connection>\"\n\"#<PG::Connection>\"\n:y\n\"#<PG::Result>\"\n\"#<PG::Result>\"\n:y\n"
	if got := eval(t, pgConnectSrc(reply, body)); got != want {
		t.Errorf("to_s got=%q want=%q", got, want)
	}
}

// TestPGValueTypes covers the pgValue decoder mappings: bool, float8, bytea
// (binary String), a date/timestamp (String), a NULL, and a text[] array.
func TestPGValueTypes(t *testing.T) {
	// OIDs: bool=16, float8=701, bytea=17, timestamp=1114, text[]=1009, int4=23.
	reply := `pg_auth_ok + pg_ready +
	  pg_row_desc([["b", 16], ["f", 701], ["by", 17], ["ts", 1114], ["arr", 1009], ["nul", 23]]) +
	  pg_data_row(["t", "3.5", "\\x6869", "2026-01-02 03:04:05", "{a,b}", nil]) +
	  pg_cmd("SELECT 1") + pg_ready`
	body := `res = conn.exec("SELECT *")
p res.getvalue(0, 0)
p res.getvalue(0, 1)
p res.getvalue(0, 2)
p res.getvalue(0, 3).class
p res.getvalue(0, 4)
p res.getvalue(0, 5)`
	want := strings.Join([]string{
		"true", "3.5",
		`"hi"`, // bytea \x6869 -> "hi" (binary String)
		"String",
		`["a", "b"]`, // text[] -> Array
		"nil",
	}, "\n") + "\n"
	if got := eval(t, pgConnectSrc(reply, body)); got != want {
		t.Errorf("value types\n got=%q\nwant=%q", got, want)
	}
}

// TestPGParamCoercion covers the pgArg branches: nil, bool, Integer, Bignum,
// Float, a binary String (bytea), a Symbol, and a generic to_s fallback all
// encode into the extended-protocol Bind message.
func TestPGParamCoercion(t *testing.T) {
	reply := `pg_auth_ok + pg_ready + pg_parse_complete + pg_bind_complete +
	  pg_cmd("SELECT 0") + pg_ready`
	body := `bin = "AB".b
conn.exec_params("SELECT $1,$2,$3,$4,$5,$6,$7,$8",
  [nil, true, 7, 10000000000000000000000, 1.5, bin, :sym, [1,2]])
out = conn.socket_io.out
p out.include?("7")
p out.include?("10000000000000000000000")
p out.include?("1.5")
p out.include?("sym")`
	want := "true\ntrue\ntrue\ntrue\n"
	if got := eval(t, pgConnectSrc(reply, body)); got != want {
		t.Errorf("param coercion got=%q want=%q", got, want)
	}
}

// TestPGExecParamsSpread covers exec_params accepting the params both as a single
// Array and as trailing positional arguments (pgParamArray both branches).
func TestPGExecParamsSpread(t *testing.T) {
	reply := `pg_auth_ok + pg_ready + pg_parse_complete + pg_bind_complete +
	  pg_row_desc([["n", 23]]) + pg_data_row(["5"]) + pg_cmd("SELECT 1") + pg_ready`
	// Trailing positional params (not wrapped in an Array).
	body := `res = conn.exec_params("SELECT $1::int", 5)
p res.getvalue(0, 0)`
	if got := eval(t, pgConnectSrc(reply, body)); got != "5\n" {
		t.Errorf("exec_params spread got=%q", got)
	}
}

// TestPGConnectDatabaseKeyword covers pgConnectArgs reading "database" (vs
// "dbname"), skipping transport keywords (host/port), and String keyword keys.
func TestPGConnectDatabaseKeyword(t *testing.T) {
	src := pgHelpers + `
require "pg"
sock = FakeSock.new(pg_auth_ok + pg_ready)
conn = PG.connect("connection" => sock, "user" => "me", "database" => "db",
                  "host" => "ignored", "port" => 5432, "application_name" => "app")
p sock.out.include?("db")
p sock.out.include?("app")
p sock.out.include?("ignored")`
	if got := eval(t, src); got != "true\ntrue\nfalse\n" {
		t.Errorf("connect db keyword got=%q", got)
	}
}

// TestPGArgErrors covers the argument guards: exec/exec_params/prepare/
// exec_prepared with too few arguments, and getvalue out of range.
func TestPGArgErrors(t *testing.T) {
	reply := `pg_auth_ok + pg_ready +
	  pg_row_desc([["n", 23]]) + pg_data_row(["1"]) + pg_cmd("SELECT 1") + pg_ready`
	body := `begin; conn.exec; rescue ArgumentError; p :a; end
begin; conn.exec_params; rescue ArgumentError; p :b; end
begin; conn.prepare("x"); rescue ArgumentError; p :c; end
begin; conn.exec_prepared; rescue ArgumentError; p :d; end
res = conn.exec("SELECT 1")
begin; res.getvalue(9, 0); rescue PG::Error; p :e; end
begin; res.getvalue(0); rescue ArgumentError; p :f; end
begin; res.getisnull(0); rescue ArgumentError; p :g; end
begin; res.fname(9); rescue PG::Error; p :h; end`
	want := ":a\n:b\n:c\n:d\n:e\n:f\n:g\n:h\n"
	if got := eval(t, pgConnectSrc(reply, body)); got != want {
		t.Errorf("arg errors got=%q want=%q", got, want)
	}
}

// TestPGConnectNoHash covers PG.connect called with a non-Hash last argument
// (no keywords) raising ArgumentError for the missing connection.
func TestPGConnectNoHash(t *testing.T) {
	if err := runErr(t, `require "pg"; PG.connect("plainstring")`); err == nil ||
		!strings.Contains(err.Error(), "connection") {
		t.Errorf("want connection ArgumentError, got %v", err)
	}
}

// TestPGConnectNoArgs covers PG.connect() with no arguments at all.
func TestPGConnectNoArgs(t *testing.T) {
	if err := runErr(t, `require "pg"; PG.connect`); err == nil ||
		!strings.Contains(err.Error(), "connection") {
		t.Errorf("want connection ArgumentError, got %v", err)
	}
}

// TestPGExtendedErrors covers exec_params / prepare / exec_prepared surfacing a
// server ErrorResponse as PG::Error, and the each-without-block LocalJumpError.
func TestPGExtendedErrors(t *testing.T) {
	// exec_params server error.
	if got := eval(t, pgConnectSrc(
		`pg_auth_ok + pg_ready + pg_error("22012", "div0") + pg_ready`,
		`begin; conn.exec_params("SELECT 1/0", []); rescue PG::Error; p :ep; end`)); got != ":ep\n" {
		t.Errorf("exec_params err got=%q", got)
	}
	// prepare server error.
	if got := eval(t, pgConnectSrc(
		`pg_auth_ok + pg_ready + pg_error("42601", "syntax") + pg_ready`,
		`begin; conn.prepare("s", "BAD"); rescue PG::Error; p :pp; end`)); got != ":pp\n" {
		t.Errorf("prepare err got=%q", got)
	}
	// exec_prepared server error.
	if got := eval(t, pgConnectSrc(
		`pg_auth_ok + pg_ready + pg_error("42P01", "notable") + pg_ready`,
		`begin; conn.exec_prepared("s", []); rescue PG::Error; p :xp; end`)); got != ":xp\n" {
		t.Errorf("exec_prepared err got=%q", got)
	}
	// each without a block.
	eachReply := `pg_auth_ok + pg_ready +
	  pg_row_desc([["n", 23]]) + pg_data_row(["1"]) + pg_cmd("SELECT 1") + pg_ready`
	if got := eval(t, pgConnectSrc(eachReply,
		`res = conn.exec("SELECT 1"); begin; res.each; rescue LocalJumpError; p :nb; end`)); got != ":nb\n" {
		t.Errorf("each no-block got=%q", got)
	}
}

// TestPGTransportError covers raisePGError's non-*pg.Error (transport) branch:
// a truncated backend stream during exec raises PG::ConnectionBad.
func TestPGTransportError(t *testing.T) {
	// After ReadyForQuery, feed a truncated row-description frame.
	reply := `pg_auth_ok + pg_ready + ("T" + [100].pack("N") + "short")`
	body := `begin
  conn.exec("SELECT 1")
rescue PG::ConnectionBad
  p :ce
end`
	if got := eval(t, pgConnectSrc(reply, body)); got != ":ce\n" {
		t.Errorf("transport err got=%q", got)
	}
}

// TestPGResultRangeAndCoercion covers getvalue column-out-of-range (PG::Error),
// a non-integer index (TypeError via pgIntArg), and a non-string SQL argument
// (pgStringArg to_s) plus a Symbol table name in pgKeyName's default path.
func TestPGResultRangeAndCoercion(t *testing.T) {
	reply := `pg_auth_ok + pg_ready +
	  pg_row_desc([["n", 23]]) + pg_data_row(["1"]) + pg_cmd("SELECT 1") + pg_ready`
	body := `res = conn.exec("SELECT 1")
begin; res.getvalue(0, 9); rescue PG::Error; p :col; end
begin; res.getisnull(0, 9); rescue PG::Error; p :cn; end
begin; res.getvalue(0, "x"); rescue TypeError; p :ty; end
begin; res.getvalue(0, 10000000000000000000000); rescue PG::Error; p :big; end`
	if got := eval(t, pgConnectSrc(reply, body)); got != ":col\n:cn\n:ty\n:big\n" {
		t.Errorf("range/coercion got=%q", got)
	}
}

// TestPGTupleRangeAndArg0 covers #[] / #tuple out-of-range (PG::Error), a plain
// (non-binary) String bind param (pgArg's Str branch), and pgArg0's empty-args
// guard via #escape_string with no argument.
func TestPGTupleRangeAndArg0(t *testing.T) {
	reply := `pg_auth_ok + pg_ready + pg_parse_complete + pg_bind_complete +
	  pg_row_desc([["n", 23]]) + pg_data_row(["1"]) + pg_cmd("SELECT 1") + pg_ready`
	body := `conn.exec_params("SELECT $1", ["plain"])
p conn.socket_io.out.include?("plain")
res = conn.exec("SELECT 1") rescue nil`
	// The exec_params reply is consumed above; re-run over a second connection for
	// the range + arg0 checks with their own stream.
	if got := eval(t, pgConnectSrc(reply, body)); got != "true\n" {
		t.Errorf("plain param got=%q", got)
	}

	reply2 := `pg_auth_ok + pg_ready +
	  pg_row_desc([["n", 23]]) + pg_data_row(["1"]) + pg_cmd("SELECT 1") + pg_ready`
	body2 := `res = conn.exec("SELECT 1")
begin; res[9]; rescue PG::Error; p :tr; end
begin; conn.escape_string; rescue ArgumentError; p :a0; end`
	if got := eval(t, pgConnectSrc(reply2, body2)); got != ":tr\n:a0\n" {
		t.Errorf("tuple range / arg0 got=%q", got)
	}
}

// TestPGConnectIntegerKey covers pgKeyName's default (ToS) path via a non-Symbol,
// non-String keyword-hash key.
func TestPGConnectIntegerKey(t *testing.T) {
	src := pgHelpers + `
require "pg"
sock = FakeSock.new(pg_auth_ok + pg_ready)
# A Hash with an Integer key exercises pgKeyName's to_s fallback; it is not a
# recognised keyword, so it lands in the StartupMessage params harmlessly.
conn = PG.connect({:connection => sock, :user => "me", 7 => "x"})
p sock.out.include?("me")`
	if got := eval(t, src); got != "true\n" {
		t.Errorf("integer key got=%q", got)
	}
}

// TestPGNonStringSQL covers pgStringArg's to_s fallback: a non-String exec
// argument is stringified.
func TestPGNonStringSQL(t *testing.T) {
	reply := `pg_auth_ok + pg_ready + pg_empty + pg_ready`
	// A Symbol SQL argument -> "SELECT 1" via to_s is contrived; use an Integer.
	body := `conn.exec(42) rescue nil
p conn.socket_io.out.include?("42")`
	if got := eval(t, pgConnectSrc(reply, body)); got != "true\n" {
		t.Errorf("non-string sql got=%q", got)
	}
}

// TestPGConnectSymbolKeys covers pgConnectArgs with Symbol keyword keys (the
// pgKeyName Symbol branch) — the default connect form.
func TestPGConnectSymbolKeys(t *testing.T) {
	src := pgHelpers + `
require "pg"
sock = FakeSock.new(pg_auth_ok + pg_ready)
conn = PG.connect(connection: sock, user: "me", dbname: "sym_db")
p sock.out.include?("sym_db")`
	if got := eval(t, src); got != "true\n" {
		t.Errorf("symbol keys got=%q", got)
	}
}

// TestPGGetisnullFalse covers getisnull returning false for a present value and
// the non-string / int coercion helpers via getvalue with Integer indices.
func TestPGGetisnullFalse(t *testing.T) {
	reply := `pg_auth_ok + pg_ready +
	  pg_row_desc([["n", 23]]) + pg_data_row(["1"]) + pg_cmd("SELECT 1") + pg_ready`
	body := `res = conn.exec("SELECT 1")
p res.getisnull(0, 0)
p res.fnumber("missing")`
	if got := eval(t, pgConnectSrc(reply, body)); got != "false\n-1\n" {
		t.Errorf("getisnull got=%q", got)
	}
}
