// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"testing"
)

// TestSQLite3Constants covers the SQLite3 loadable module and its exception tree
// (require "sqlite3").
func TestSQLite3Constants(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "sqlite3"; p SQLite3.is_a?(Module)`, "true\n"},
		{`p require "sqlite3"`, "true\n"},
		{`require "sqlite3"; p require "sqlite3"`, "false\n"},
		// Exception tree: every code-mapped subclass < SQLite3::Exception < StandardError.
		{`require "sqlite3"; p SQLite3::Exception < StandardError`, "true\n"},
		{`require "sqlite3"; p SQLite3::SQLException < SQLite3::Exception`, "true\n"},
		{`require "sqlite3"; p SQLite3::BusyException < SQLite3::Exception`, "true\n"},
		{`require "sqlite3"; p SQLite3::ConstraintException < SQLite3::Exception`, "true\n"},
		{`require "sqlite3"; p SQLite3::CantOpenException < SQLite3::Exception`, "true\n"},
		// Class identity of an opened database / statement.
		{`require "sqlite3"; p SQLite3::Database.new(":memory:").class`, "SQLite3::Database\n"},
		{`require "sqlite3"; db = SQLite3::Database.new(":memory:"); p db.prepare("SELECT 1").class`, "SQLite3::Statement\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestSQLite3CRUD covers a real in-memory database: CREATE / INSERT / SELECT via
// #execute, the type map (INTEGER/REAL/TEXT/BLOB/NULL), binds, last_insert_row_id
// and changes. This is a functional database (modernc backend), not a seam.
func TestSQLite3CRUD(t *testing.T) {
	cases := []struct{ src, want string }{
		// A full round-trip: create, insert with binds, select back.
		{`require "sqlite3"
db = SQLite3::Database.new(":memory:")
db.execute("CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT, score REAL)")
db.execute("INSERT INTO t (name, score) VALUES (?, ?)", ["ada", 9.5])
p db.execute("SELECT id, name, score FROM t")`, "[[1, \"ada\", 9.5]]\n"},
		// last_insert_row_id and changes reflect the INSERT.
		{`require "sqlite3"
db = SQLite3::Database.new(":memory:")
db.execute("CREATE TABLE t (id INTEGER PRIMARY KEY, n TEXT)")
db.execute("INSERT INTO t (n) VALUES (?)", ["x"])
p db.last_insert_row_id
p db.changes`, "1\n1\n"},
		// NULL maps to nil, INTEGER to Integer.
		{`require "sqlite3"
db = SQLite3::Database.new(":memory:")
db.execute("CREATE TABLE t (a INTEGER, b TEXT)")
db.execute("INSERT INTO t (a, b) VALUES (?, ?)", [7, nil])
p db.execute("SELECT a, b FROM t")`, "[[7, nil]]\n"},
		// BLOB (bound as a binary String) comes back as an ASCII-8BIT String.
		{`require "sqlite3"
db = SQLite3::Database.new(":memory:")
db.execute("CREATE TABLE t (b BLOB)")
db.execute("INSERT INTO t (b) VALUES (?)", ["\xff\x00".b])
row = db.execute("SELECT b FROM t").first
p row.first.encoding.name`, "\"ASCII-8BIT\"\n"},
		// get_first_row / get_first_value.
		{`require "sqlite3"
db = SQLite3::Database.new(":memory:")
db.execute("CREATE TABLE t (x INTEGER)")
db.execute("INSERT INTO t VALUES (42)")
p db.get_first_row("SELECT x FROM t")
p db.get_first_value("SELECT x FROM t")`, "[42]\n42\n"},
		// get_first_row / get_first_value on an empty result yield nil.
		{`require "sqlite3"
db = SQLite3::Database.new(":memory:")
db.execute("CREATE TABLE t (x INTEGER)")
p db.get_first_row("SELECT x FROM t")
p db.get_first_value("SELECT x FROM t")`, "nil\nnil\n"},
		// results_as_hash: rows come back keyed by column name.
		{`require "sqlite3"
db = SQLite3::Database.new(":memory:")
db.results_as_hash = true
p db.results_as_hash
db.execute("CREATE TABLE t (id INTEGER, name TEXT)")
db.execute("INSERT INTO t VALUES (1, 'ada')")
p db.execute("SELECT id, name FROM t")`, "true\n[{\"id\" => 1, \"name\" => \"ada\"}]\n"},
		// #execute with a block yields each row.
		{`require "sqlite3"
db = SQLite3::Database.new(":memory:")
db.execute("CREATE TABLE t (x INTEGER)")
db.execute("INSERT INTO t VALUES (1), (2)")
seen = []
db.execute("SELECT x FROM t") { |r| seen << r.first }
p seen`, "[1, 2]\n"},
		// #execute2 returns the header row then the data rows.
		{`require "sqlite3"
db = SQLite3::Database.new(":memory:")
db.execute("CREATE TABLE t (a INTEGER, b INTEGER)")
db.execute("INSERT INTO t VALUES (1, 2)")
p db.execute2("SELECT a, b FROM t")`, "[[\"a\", \"b\"], [1, 2]]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\ngot =%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestSQLite3Statement covers the prepared-statement cursor API: prepare,
// bind_param, step, columns, types, reset, close.
func TestSQLite3Statement(t *testing.T) {
	cases := []struct{ src, want string }{
		// bind_param (positional) + step walks the rows, then nil at the end.
		{`require "sqlite3"
db = SQLite3::Database.new(":memory:")
db.execute("CREATE TABLE t (x INTEGER)")
db.execute("INSERT INTO t VALUES (10), (20)")
st = db.prepare("SELECT x FROM t WHERE x >= ?")
st.bind_param(1, 10)
p st.step
p st.step
p st.step`, "[10]\n[20]\nnil\n"},
		// columns / sql / types.
		{`require "sqlite3"
db = SQLite3::Database.new(":memory:")
db.execute("CREATE TABLE t (id INTEGER, name TEXT)")
st = db.prepare("SELECT id, name FROM t")
p st.columns
p st.sql`, "[\"id\", \"name\"]\n\"SELECT id, name FROM t\"\n"},
		// reset lets a statement be stepped again.
		{`require "sqlite3"
db = SQLite3::Database.new(":memory:")
db.execute("CREATE TABLE t (x INTEGER)")
db.execute("INSERT INTO t VALUES (5)")
st = db.prepare("SELECT x FROM t")
a = st.step
st.reset
b = st.step
p [a, b]`, "[[5], [5]]\n"},
		// Statement#execute returns all rows; bind_params binds a whole list.
		{`require "sqlite3"
db = SQLite3::Database.new(":memory:")
db.execute("CREATE TABLE t (x INTEGER)")
db.execute("INSERT INTO t VALUES (1), (2), (3)")
st = db.prepare("SELECT x FROM t WHERE x <> ?")
st.bind_params(2)
p st.execute`, "[[1], [3]]\n"},
		// query returns a live cursor; step + close.
		{`require "sqlite3"
db = SQLite3::Database.new(":memory:")
db.execute("CREATE TABLE t (x INTEGER)")
db.execute("INSERT INTO t VALUES (7)")
st = db.query("SELECT x FROM t")
p st.step
p st.closed?
st.close
p st.closed?`, "[7]\nfalse\ntrue\n"},
		// A named bind_param.
		{`require "sqlite3"
db = SQLite3::Database.new(":memory:")
db.execute("CREATE TABLE t (x INTEGER)")
db.execute("INSERT INTO t VALUES (99)")
st = db.prepare("SELECT x FROM t WHERE x = :v")
st.bind_param(":v", 99)
p st.step`, "[99]\n"},
		// clear_bindings! then re-bind.
		{`require "sqlite3"
db = SQLite3::Database.new(":memory:")
db.execute("CREATE TABLE t (x INTEGER)")
db.execute("INSERT INTO t VALUES (1)")
st = db.prepare("SELECT x FROM t WHERE x = ?")
st.bind_param(1, 2)
st.clear_bindings!
st.bind_param(1, 1)
p st.execute`, "[[1]]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\ngot =%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestSQLite3HashMode covers the results_as_hash statement-cursor and block
// paths (step and #execute { } yielding Hash rows, and the standalone query
// cursor).
func TestSQLite3HashMode(t *testing.T) {
	cases := []struct{ src, want string }{
		// A stepped statement yields Hash rows when results_as_hash is set.
		{`require "sqlite3"
db = SQLite3::Database.new(":memory:")
db.results_as_hash = true
db.execute("CREATE TABLE t (id INTEGER, name TEXT)")
db.execute("INSERT INTO t VALUES (1, 'ada')")
st = db.prepare("SELECT id, name FROM t")
p st.step`, "{\"id\" => 1, \"name\" => \"ada\"}\n"},
		// Statement#execute with a block yields Hash rows in hash mode.
		{`require "sqlite3"
db = SQLite3::Database.new(":memory:")
db.results_as_hash = true
db.execute("CREATE TABLE t (x INTEGER)")
db.execute("INSERT INTO t VALUES (5)")
seen = []
db.prepare("SELECT x FROM t").execute { |r| seen << r }
p seen`, "[{\"x\" => 5}]\n"},
		// #execute with a block in hash mode yields Hash rows.
		{`require "sqlite3"
db = SQLite3::Database.new(":memory:")
db.results_as_hash = true
db.execute("CREATE TABLE t (x INTEGER)")
db.execute("INSERT INTO t VALUES (5)")
seen = []
db.execute("SELECT x FROM t") { |r| seen << r }
p seen`, "[{\"x\" => 5}]\n"},
		// Float and Symbol binds through Ruby (Symbol binds as TEXT).
		{`require "sqlite3"
db = SQLite3::Database.new(":memory:")
db.execute("CREATE TABLE t (a REAL, b TEXT, c INTEGER)")
db.execute("INSERT INTO t VALUES (?, ?, ?)", [1.5, :sym, true])
p db.execute("SELECT a, b, c FROM t")`, "[[1.5, \"sym\", 1]]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\ngot =%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestSQLite3ErrorPaths covers the raise arms of the individual methods by
// triggering real SQLite failures (invalid SQL, misuse, arity).
func TestSQLite3ErrorPaths(t *testing.T) {
	cases := []struct{ src, want string }{
		// prepare of invalid SQL raises.
		{`require "sqlite3"
db = SQLite3::Database.new(":memory:")
begin; db.prepare("SELECT bogus syntax !!"); rescue SQLite3::Exception; p :prep; end`, ":prep\n"},
		// query of invalid SQL raises.
		{`require "sqlite3"
db = SQLite3::Database.new(":memory:")
begin; db.query("SELECT bogus !!"); rescue SQLite3::Exception; p :q; end`, ":q\n"},
		// get_first_row / get_first_value on invalid SQL raise.
		{`require "sqlite3"
db = SQLite3::Database.new(":memory:")
begin; db.get_first_row("SELECT bogus !!"); rescue SQLite3::Exception; p :gfr; end
begin; db.get_first_value("SELECT bogus !!"); rescue SQLite3::Exception; p :gfv; end`, ":gfr\n:gfv\n"},
		// execute2 on invalid SQL raises.
		{`require "sqlite3"
db = SQLite3::Database.new(":memory:")
begin; db.execute2("SELECT bogus !!"); rescue SQLite3::Exception; p :e2; end`, ":e2\n"},
		// bind_param with too few arguments raises ArgumentError.
		{`require "sqlite3"
db = SQLite3::Database.new(":memory:")
db.execute("CREATE TABLE t (x INTEGER)")
st = db.prepare("SELECT x FROM t WHERE x = ?")
begin; st.bind_param(1); rescue ArgumentError; p :arity; end`, ":arity\n"},
		// commit with no active transaction raises.
		{`require "sqlite3"
db = SQLite3::Database.new(":memory:")
begin; db.commit; rescue SQLite3::Exception; p :commit; end`, ":commit\n"},
		// rollback with no active transaction raises.
		{`require "sqlite3"
db = SQLite3::Database.new(":memory:")
begin; db.rollback; rescue SQLite3::Exception; p :rollback; end`, ":rollback\n"},
		// A statement that errors at step time (division by zero in SQLite raises
		// nothing; use a bad prepared expression via prepare succeeding but exec
		// failing is hard, so use a malformed statement types path instead).
		// types on a valid statement.
		{`require "sqlite3"
db = SQLite3::Database.new(":memory:")
db.execute("CREATE TABLE t (id INTEGER, name TEXT)")
st = db.prepare("SELECT id, name FROM t")
p st.types.length`, "2\n"},
		// Database.new with no argument raises ArgumentError.
		{`require "sqlite3"
begin; SQLite3::Database.new; rescue ArgumentError; p :noarg; end`, ":noarg\n"},
		// prepare with no argument raises ArgumentError.
		{`require "sqlite3"
db = SQLite3::Database.new(":memory:")
begin; db.prepare; rescue ArgumentError; p :noprep; end`, ":noprep\n"},
		// execute with no argument raises ArgumentError.
		{`require "sqlite3"
db = SQLite3::Database.new(":memory:")
begin; db.execute; rescue ArgumentError; p :noexec; end`, ":noexec\n"},
		// #next walks the cursor like #step.
		{`require "sqlite3"
db = SQLite3::Database.new(":memory:")
db.execute("CREATE TABLE t (x INTEGER)")
db.execute("INSERT INTO t VALUES (3)")
st = db.prepare("SELECT x FROM t")
p st.next
p st.next`, "[3]\nnil\n"},
		// Calling a connection method after #close hits the error arm.
		{`require "sqlite3"
db = SQLite3::Database.new(":memory:")
db.close
begin; db.last_insert_row_id; rescue SQLite3::Exception; p :lirid; end
begin; db.changes; rescue SQLite3::Exception; p :chg; end
begin; db.total_changes; rescue SQLite3::Exception; p :tot; end
begin; db.execute("SELECT 1"); rescue SQLite3::Exception; p :exec; end
begin; db.busy_timeout = 10; rescue SQLite3::Exception; p :busy; end`,
			":lirid\n:chg\n:tot\n:exec\n:busy\n"},
		// close is idempotent (a second close is a no-op, not an error).
		{`require "sqlite3"
db = SQLite3::Database.new(":memory:")
db.close
db.close
p db.closed?`, "true\n"},
		// A closed statement errors on execute / step / columns / types.
		{`require "sqlite3"
db = SQLite3::Database.new(":memory:")
db.execute("CREATE TABLE t (x INTEGER)")
st = db.prepare("SELECT x FROM t")
st.close
begin; st.execute; rescue SQLite3::Exception; p :ex; end
begin; st.step; rescue SQLite3::Exception; p :st; end
begin; st.columns; rescue SQLite3::Exception; p :co; end
begin; st.next; rescue SQLite3::Exception; p :nx; end`,
			":ex\n:st\n:co\n:nx\n"},
		// A syntax error while results_as_hash is set raises through the hash path.
		{`require "sqlite3"
db = SQLite3::Database.new(":memory:")
db.results_as_hash = true
begin; db.execute("SELECT bogus !!"); rescue SQLite3::Exception; p :hasherr; end`, ":hasherr\n"},
		// bind_params accepts an explicit Array (the spread path).
		{`require "sqlite3"
db = SQLite3::Database.new(":memory:")
db.execute("CREATE TABLE t (a INTEGER, b INTEGER)")
db.execute("INSERT INTO t VALUES (1, 2)")
st = db.prepare("SELECT a FROM t WHERE a = ? AND b = ?")
st.bind_params([1, 2])
p st.execute`, "[[1]]\n"},
		// #transaction whose commit fails (the block closes the connection) raises
		// through the deferred rollback.
		{`require "sqlite3"
db = SQLite3::Database.new(":memory:")
db.execute("CREATE TABLE t (x INTEGER)")
begin
  db.transaction { db.close }
rescue SQLite3::Exception
  p :commitfail
end`, ":commitfail\n"},
		// Statement#execute(args) binds its positional arguments then runs.
		{`require "sqlite3"
db = SQLite3::Database.new(":memory:")
db.execute("CREATE TABLE t (x INTEGER)")
db.execute("INSERT INTO t VALUES (1), (2)")
st = db.prepare("SELECT x FROM t WHERE x = ?")
p st.execute(2)`, "[[2]]\n"},
		// #types on a closed statement raises.
		{`require "sqlite3"
db = SQLite3::Database.new(":memory:")
db.execute("CREATE TABLE t (x INTEGER)")
st = db.prepare("SELECT x FROM t")
st.close
begin; st.types; rescue SQLite3::Exception; p :ty; end`, ":ty\n"},
		// #transaction on a closed connection raises when it cannot BEGIN (both the
		// block and no-block forms go through the same Begin).
		{`require "sqlite3"
db = SQLite3::Database.new(":memory:")
db.close
begin; db.transaction { 1 }; rescue SQLite3::Exception; p :txblk; end
begin; db.transaction; rescue SQLite3::Exception; p :txnoblk; end`,
			":txblk\n:txnoblk\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\ngot =%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestSQLite3Transaction covers #transaction commit / rollback and the explicit
// begin/commit/rollback trio.
func TestSQLite3Transaction(t *testing.T) {
	cases := []struct{ src, want string }{
		// Block form commits on success.
		{`require "sqlite3"
db = SQLite3::Database.new(":memory:")
db.execute("CREATE TABLE t (x INTEGER)")
db.transaction { db.execute("INSERT INTO t VALUES (1)") }
p db.get_first_value("SELECT COUNT(*) FROM t")`, "1\n"},
		// Block form rolls back when the block raises; the exception propagates.
		{`require "sqlite3"
db = SQLite3::Database.new(":memory:")
db.execute("CREATE TABLE t (x INTEGER)")
begin
  db.transaction do
    db.execute("INSERT INTO t VALUES (1)")
    raise "boom"
  end
rescue RuntimeError => e
  p e.message
end
p db.get_first_value("SELECT COUNT(*) FROM t")`, "\"boom\"\n0\n"},
		// Explicit transaction_active? / commit.
		{`require "sqlite3"
db = SQLite3::Database.new(":memory:")
db.execute("CREATE TABLE t (x INTEGER)")
db.transaction
p db.transaction_active?
db.execute("INSERT INTO t VALUES (1)")
db.commit
p db.transaction_active?`, "true\nfalse\n"},
		// Explicit rollback discards the insert.
		{`require "sqlite3"
db = SQLite3::Database.new(":memory:")
db.execute("CREATE TABLE t (x INTEGER)")
db.transaction(:immediate)
db.execute("INSERT INTO t VALUES (1)")
db.rollback
p db.get_first_value("SELECT COUNT(*) FROM t")`, "0\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\ngot =%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestSQLite3Errors covers the gem-faithful exception classes a bad statement
// raises, plus path/close/misc surface.
func TestSQLite3Errors(t *testing.T) {
	cases := []struct{ src, want string }{
		// A syntax error raises SQLite3::SQLException (< SQLite3::Exception).
		{`require "sqlite3"
db = SQLite3::Database.new(":memory:")
begin
  db.execute("NOT SQL AT ALL")
rescue SQLite3::Exception => e
  p e.is_a?(SQLite3::SQLException)
end`, "true\n"},
		// A UNIQUE violation raises SQLite3::ConstraintException.
		{`require "sqlite3"
db = SQLite3::Database.new(":memory:")
db.execute("CREATE TABLE t (id INTEGER PRIMARY KEY)")
db.execute("INSERT INTO t VALUES (1)")
begin
  db.execute("INSERT INTO t VALUES (1)")
rescue SQLite3::ConstraintException
  p :constraint
end`, ":constraint\n"},
		// path / closed? / close.
		{`require "sqlite3"
db = SQLite3::Database.new(":memory:")
p db.path
p db.closed?
db.close
p db.closed?`, "\":memory:\"\nfalse\ntrue\n"},
		// total_changes accumulates.
		{`require "sqlite3"
db = SQLite3::Database.new(":memory:")
db.execute("CREATE TABLE t (x INTEGER)")
db.execute("INSERT INTO t VALUES (1), (2)")
p db.total_changes`, "2\n"},
		// Block form of Database.new yields and closes.
		{`require "sqlite3"
r = SQLite3::Database.new(":memory:") { |db| db.execute("CREATE TABLE t (x INTEGER)"); 7 }
p r`, "7\n"},
		// busy_timeout=.
		{`require "sqlite3"
db = SQLite3::Database.new(":memory:")
db.busy_timeout = 100
p db.path`, "\":memory:\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\ngot =%q\nwant=%q", c.src, got, c.want)
		}
	}
}
