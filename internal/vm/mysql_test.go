// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"fmt"
	"net"
	"strings"
	"testing"

	sqle "github.com/dolthub/go-mysql-server"
	"github.com/dolthub/go-mysql-server/memory"
	"github.com/dolthub/go-mysql-server/server"
	gmssql "github.com/dolthub/go-mysql-server/sql"
	"github.com/sirupsen/logrus"
)

// startMySQL brings up an in-process, pure-Go MySQL-compatible server
// (github.com/dolthub/go-mysql-server) with a single in-memory database "test"
// bound to an ephemeral 127.0.0.1 port, and returns that port. It is the real
// query-execution backend the mysql2 binding tests run against — no external
// mysqld, no fixed port, no leaked server: the server (and its own listener) is
// closed in t.Cleanup, and every test closes its client, so no goroutine or
// connection outlives the test. go-mysql-server is imported only from this
// _test.go, so it never enters rbgo's runtime dependency graph.
func startMySQL(t *testing.T) int {
	t.Helper()
	logrus.SetLevel(logrus.PanicLevel) // silence the server's ready/close banner

	db := memory.NewDatabase("test")
	db.BaseDatabase.EnablePrimaryKeyIndexes()
	pro := memory.NewDBProvider(db)
	engine := sqle.NewDefault(pro)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	cfg := server.Config{Protocol: "tcp", Address: ln.Addr().String(), Listener: ln}
	s, err := server.NewServer(cfg, engine, gmssql.NewContext, memory.NewSessionBuilder(pro), nil)
	if err != nil {
		_ = ln.Close()
		t.Fatalf("new server: %v", err)
	}
	go func() { _ = s.Start() }()
	t.Cleanup(func() { _ = s.Close() })
	return ln.Addr().(*net.TCPAddr).Port
}

// mysqlSrc renders a Ruby program that requires mysql2 and opens a client to the
// in-process server on the given port (as root, database "test"), binding the
// connection to the local `client`, then runs body. Every test closes the
// client at the end (close is idempotent) so no connection leaks.
func mysqlSrc(port int, body string) string {
	return fmt.Sprintf(`require "mysql2"
client = Mysql2::Client.new(host: "127.0.0.1", port: %d, username: "root", database: "test")
%s
client.close
`, port, body)
}

// TestMySQLConstants covers the Mysql2 module, the "mysql2"/"mysql" require keys
// and the Mysql2::Error tree.
func TestMySQLConstants(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "mysql2"; p require "mysql2"`, "false\n"},
		{`p require "mysql2"`, "true\n"},
		{`p require "mysql"`, "true\n"},
		{`require "mysql2"; p Mysql2.is_a?(Module)`, "true\n"},
		{`require "mysql2"; p Mysql2::Error < StandardError`, "true\n"},
		{`require "mysql2"; p Mysql2::Error::ConnectionError < Mysql2::Error`, "true\n"},
		{`require "mysql2"; p Mysql2::Error::TimeoutError < Mysql2::Error`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestMySQLRoundTrip covers the headline path: CREATE, INSERT (a nil #query
// result), #affected_rows / #last_id / #insert_id, then a SELECT returning a
// Mysql2::Result of Hash rows with #class / #count / #fields / #each.
func TestMySQLRoundTrip(t *testing.T) {
	port := startMySQL(t)
	body := `client.query("CREATE TABLE hosts (id INT PRIMARY KEY AUTO_INCREMENT, name VARCHAR(64))")
p client.query("INSERT INTO hosts (name) VALUES ('web')")
p client.affected_rows
p client.last_id
p client.insert_id
res = client.query("SELECT id, name FROM hosts")
p res.class
p res.count
p res.fields
res.each { |row| p row }`
	want := strings.Join([]string{
		"nil", "1", "1", "1",
		"Mysql2::Result", "1",
		`["id", "name"]`,
		`{"id" => 1, "name" => "web"}`,
	}, "\n") + "\n"
	if got := eval(t, mysqlSrc(port, body)); got != want {
		t.Errorf("round trip\n got=%q\nwant=%q", got, want)
	}
}

// TestMySQLValueTypes covers the mysqlValue cast mappings: Integer (INT/BIGINT),
// a big Integer (BIGINT UNSIGNED past int64), Float, BigDecimal (DECIMAL), Date,
// Time (DATETIME), a TIME/VARCHAR/BLOB String and a NULL.
func TestMySQLValueTypes(t *testing.T) {
	port := startMySQL(t)
	body := `client.query("CREATE TABLE t (i INT PRIMARY KEY, ub BIGINT UNSIGNED, f DOUBLE, de DECIMAL(10,2), d DATE, dt DATETIME, tm TIME, s VARCHAR(20), bl BLOB, nb INT)")
client.query("INSERT INTO t VALUES (1, 18446744073709551615, 2.5, 3.14, '2026-01-02', '2026-01-02 03:04:05', '10:20:30', 'hi', 'AB', NULL)")
row = client.query("SELECT * FROM t").first
p row["i"]
p row["ub"]
p row["f"]
p row["de"].class
p row["de"].to_s
p row["d"].class
p row["d"].to_s
p row["dt"].class
p row["dt"].to_s
p row["tm"]
p row["s"]
p row["bl"]
p row["nb"]`
	want := strings.Join([]string{
		"1", "18446744073709551615", "2.5",
		"BigDecimal", `"0.314e1"`,
		"Date", `"2026-01-02"`,
		"Time", `"2026-01-02 03:04:05 +0000"`,
		`"10:20:30"`, `"hi"`, `"AB"`, "nil",
	}, "\n") + "\n"
	if got := eval(t, mysqlSrc(port, body)); got != want {
		t.Errorf("value types\n got=%q\nwant=%q", got, want)
	}
}

// TestMySQLQueryOptions covers the per-query options: as: :array (positional
// rows), symbolize_keys (Symbol Hash keys), cast: false (every value a String)
// and cast_booleans (a TINYINT -> true|false).
func TestMySQLQueryOptions(t *testing.T) {
	port := startMySQL(t)
	body := `client.query("CREATE TABLE o (id INT PRIMARY KEY, flag TINYINT, name VARCHAR(10))")
client.query("INSERT INTO o VALUES (1, 1, 'a')")
p client.query("SELECT id, name FROM o", as: :array).first
p client.query("SELECT id FROM o", as: :hash).first
h = client.query("SELECT id, name FROM o", symbolize_keys: true).first
p h.keys
p h[:name]
p client.query("SELECT id FROM o", cast: false).first["id"]
p client.query("SELECT flag FROM o", cast_booleans: true).first["flag"]`
	want := strings.Join([]string{
		`[1, "a"]`,
		`{"id" => 1}`,
		`[:id, :name]`,
		`"a"`,
		`"1"`,
		"true",
	}, "\n") + "\n"
	if got := eval(t, mysqlSrc(port, body)); got != want {
		t.Errorf("query options\n got=%q\nwant=%q", got, want)
	}
}

// TestMySQLPrepared covers prepared statements: #prepare, #sql, an INSERT
// #execute (nil result + client #affected_rows), #closed? / #close, and a SELECT
// #execute with a positional bind yielding rows.
func TestMySQLPrepared(t *testing.T) {
	port := startMySQL(t)
	body := `client.query("CREATE TABLE p (id INT PRIMARY KEY, n VARCHAR(10))")
ins = client.prepare("INSERT INTO p (id, n) VALUES (?, ?)")
p ins.sql
p ins.execute(1, "x")
p client.affected_rows
p ins.closed?
ins.close
p ins.closed?
sel = client.prepare("SELECT n FROM p WHERE id > ?")
sel.execute(0).each { |row| p row }
sel.close`
	want := strings.Join([]string{
		`"INSERT INTO p (id, n) VALUES (?, ?)"`,
		"nil", "1", "false", "true",
		`{"n" => "x"}`,
	}, "\n") + "\n"
	if got := eval(t, mysqlSrc(port, body)); got != want {
		t.Errorf("prepared\n got=%q\nwant=%q", got, want)
	}
}

// TestMySQLEnumerable covers the included Enumerable surface on Mysql2::Result:
// #count / #size / #to_a / #entries / #map / #select / #first, an empty result's
// #first (nil), and #each without a block (LocalJumpError).
func TestMySQLEnumerable(t *testing.T) {
	port := startMySQL(t)
	body := `client.query("CREATE TABLE e (id INT PRIMARY KEY)")
client.query("INSERT INTO e VALUES (1), (2), (3)")
res = client.query("SELECT id FROM e ORDER BY id", as: :array)
p res.count
p res.size
p res.to_a
p res.entries
p res.map { |r| r[0] }
p res.select { |r| r[0] > 1 }.size
p res.first
empty = client.query("SELECT id FROM e WHERE id > 100")
p empty.first
p empty.count
begin; res.each; rescue LocalJumpError; p :nb; end`
	want := strings.Join([]string{
		"3", "3",
		"[[1], [2], [3]]",
		"[[1], [2], [3]]",
		"[1, 2, 3]",
		"2",
		"[1]",
		"nil", "0",
		":nb",
	}, "\n") + "\n"
	if got := eval(t, mysqlSrc(port, body)); got != want {
		t.Errorf("enumerable\n got=%q\nwant=%q", got, want)
	}
}

// TestMySQLClientMisc covers #escape / #escape_string (including a non-String
// argument via #to_s), #ping, #query_options, #closed? and #close, plus the
// wrapper #to_s / #inspect / truthiness for Client, Result and Statement.
func TestMySQLClientMisc(t *testing.T) {
	port := startMySQL(t)
	body := `p client.escape("a'b").length
p client.escape_string("x")
p client.escape(123)
p client.ping
p client.query_options[:cast]
p client.query_options[:as]
p client.to_s
p client.inspect
p(client ? :y : :n)
res = client.query("SELECT 1")
p res.to_s
p res.inspect
p(res ? :y : :n)
st = client.prepare("SELECT 1")
p st.to_s
p st.inspect
p(st ? :y : :n)
st.close
p client.closed?
client.close
p client.closed?
p client.ping`
	want := strings.Join([]string{
		"4", `"x"`, `"123"`, "true", "true", ":hash",
		`"#<Mysql2::Client>"`, `"#<Mysql2::Client>"`, ":y",
		`"#<Mysql2::Result>"`, `"#<Mysql2::Result>"`, ":y",
		`"#<Mysql2::Statement>"`, `"#<Mysql2::Statement>"`, ":y",
		"false", "true", "false",
	}, "\n") + "\n"
	if got := eval(t, mysqlSrc(port, body)); got != want {
		t.Errorf("client misc\n got=%q\nwant=%q", got, want)
	}
}

// TestMySQLServerInfo covers #server_info (the {version:, id:} Hash) and its
// error branch (calling it on a closed client raises Mysql2::Error).
func TestMySQLServerInfo(t *testing.T) {
	port := startMySQL(t)
	src := fmt.Sprintf(`require "mysql2"
client = Mysql2::Client.new(host: "127.0.0.1", port: %d, username: "root", database: "test")
info = client.server_info
p info[:version].is_a?(String)
p info[:id].is_a?(Integer)
client.close
begin; client.server_info; rescue Mysql2::Error; p :si; end
`, port)
	want := "true\ntrue\n:si\n"
	if got := eval(t, src); got != want {
		t.Errorf("server_info got=%q want=%q", got, want)
	}
}

// TestMySQLErrors covers the error surface: a SQL error raising Mysql2::Error
// with #error_number / #errno / #sql_state; a prepare error; an execute error
// (a prepared statement whose table is dropped before execution); and the
// argument guards on #query / #prepare / #escape.
func TestMySQLErrors(t *testing.T) {
	port := startMySQL(t)
	body := `begin
  client.query("SELECT * FROM nonexistent")
rescue Mysql2::Error => e
  p e.is_a?(Mysql2::Error)
  p e.error_number.is_a?(Integer)
  p e.errno.is_a?(Integer)
  p e.sql_state.is_a?(String)
end
begin; client.prepare("NOT SQL"); rescue Mysql2::Error; p :pe; end
client.query("CREATE TABLE dz (id INT PRIMARY KEY)")
st = client.prepare("SELECT id FROM dz WHERE id = ?")
client.query("DROP TABLE dz")
begin; st.execute(1); rescue Mysql2::Error; p :xe; end
begin; client.query; rescue ArgumentError; p :qa; end
begin; client.prepare; rescue ArgumentError; p :pa; end
begin; client.escape; rescue ArgumentError; p :ea; end`
	want := strings.Join([]string{
		"true", "true", "true", "true",
		":pe", ":xe", ":qa", ":pa", ":ea",
	}, "\n") + "\n"
	if got := eval(t, mysqlSrc(port, body)); got != want {
		t.Errorf("errors\n got=%q\nwant=%q", got, want)
	}
}

// TestMySQLConnectError covers Mysql2::Client.new failing (no server on the
// port) raising Mysql2::Error::ConnectionError, and .new with no arguments (the
// nil options Hash) taking the same connection-failure path.
func TestMySQLConnectError(t *testing.T) {
	cases := []string{
		`require "mysql2"
begin
  Mysql2::Client.new(host: "127.0.0.1", port: 1, username: "root")
rescue Mysql2::Error::ConnectionError
  p :ce
end`,
		`require "mysql2"
begin; Mysql2::Client.new; rescue Mysql2::Error::ConnectionError; p :ce; end`,
	}
	for _, src := range cases {
		if got := eval(t, src); got != ":ce\n" {
			t.Errorf("connect error got=%q", got)
		}
	}
}

// TestMySQLConnectOptions covers mysqlConnectOptions: the username/password/
// database aliases (user/pass/dbname), the encoding / flags(Array) / timeout
// keywords and the default query options, a single-String :flags, String
// keyword keys, a non-Symbol/String key (ignored), a unix :socket (connection
// failure) and a non-integer :port (TypeError).
func TestMySQLConnectOptions(t *testing.T) {
	port := startMySQL(t)
	src := fmt.Sprintf(`require "mysql2"
c1 = Mysql2::Client.new(host: "127.0.0.1", port: %[1]d, user: "root", pass: "", dbname: "test")
p c1.ping
c1.close
c2 = Mysql2::Client.new(host: "127.0.0.1", port: %[1]d, username: "root", database: "test",
  encoding: "utf8mb4_general_ci", flags: ["FOUND_ROWS"], connect_timeout: 5, read_timeout: 5, write_timeout: 5,
  as: :array, symbolize_keys: true, cast: true, cast_booleans: false)
p c2.query_options[:as]
p c2.query_options[:symbolize_keys]
c2.close
c3 = Mysql2::Client.new(host: "127.0.0.1", port: %[1]d, username: "root", database: "test", flags: "MULTI_STATEMENTS")
p c3.ping
c3.close
c4 = Mysql2::Client.new("host" => "127.0.0.1", "port" => %[1]d, "username" => "root", "database" => "test")
p c4.ping
c4.close
c5 = Mysql2::Client.new(host: "127.0.0.1", port: %[1]d, username: "root", database: "test", 7 => "x")
p c5.ping
c5.close
begin; Mysql2::Client.new(socket: "/nonexistent.sock", username: "root"); rescue Mysql2::Error::ConnectionError; p :sock; end
begin; Mysql2::Client.new(host: "127.0.0.1", port: "x", username: "root"); rescue TypeError; p :bp; end
`, port)
	want := strings.Join([]string{
		"true", ":array", "true", "true", "true", "true", ":sock", ":bp",
	}, "\n") + "\n"
	if got := eval(t, src); got != want {
		t.Errorf("connect options\n got=%q\nwant=%q", got, want)
	}
}
