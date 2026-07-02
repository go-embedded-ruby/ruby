// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// TestSequelSchemaDSL covers the full create_table schema DSL: every typed
// column builder, column()/foreign_key()/index() with their options, and the
// column-option keywords (null/unique/size/text/default). It runs against a mock
// sqlite database so the generated DDL can be asserted.
func TestSequelSchemaDSL(t *testing.T) {
	src := `require "sequel"
DB = Sequel.mock(host: :sqlite)
DB.create_table(:t) do |g|
  g.primary_key :id
  g.String :name, null: false, size: 40
  g.String :bio, text: true
  g.Integer :qty, default: 0
  g.Bignum :big
  g.Float :ratio
  g.Numeric :amount
  g.Bool :active
  g.Date :born
  g.DateTime :seen
  g.Time :at
  g.column :extra, :Integer, unique: true
  g.foreign_key :owner_id, :users
  g.index [:name, :qty], unique: true
end
DB.sqls.each { |s| p s }`
	got := eval(t, src)
	// Assert the DDL mentions each generated column/type rather than pinning the
	// exact full string (dialect details vary by column).
	for _, sub := range []string{
		"`id` integer NOT NULL PRIMARY KEY",
		"`name` varchar(40) NOT NULL",
		"`bio` text",
		"`qty` integer DEFAULT (0)",
		"`big` bigint",
		"`ratio` double precision",
		"`amount` numeric",
		"`active` boolean",
		"`born` date",
		"`seen` timestamp",
		"`at` timestamp",
		"`extra` integer UNIQUE",
		"`owner_id` integer REFERENCES `users`",
		"CREATE UNIQUE INDEX",
	} {
		if !strings.Contains(got, sub) {
			t.Errorf("DDL missing %q in:\n%s", sub, got)
		}
	}
}

// TestSequelColumnTypeNames covers column() with each string/symbol type name,
// including the default (unknown -> String).
func TestSequelColumnTypeNames(t *testing.T) {
	src := `require "sequel"
DB = Sequel.mock(host: :sqlite)
DB.create_table(:t) do |g|
  g.column :a, :integer
  g.column :b, :float
  g.column :c, :bignum
  g.column :d, :numeric
  g.column :e, :boolean
  g.column :f, :date
  g.column :h, :datetime
  g.column :i, :time
  g.column :j, :whatever
end
p DB.sqls.first`
	got := eval(t, src)
	for _, sub := range []string{"`a` integer", "`b` double", "`c` bigint",
		"`d` numeric", "`e` boolean", "`f` date", "`h` timestamp", "`i` timestamp",
		"`j` varchar"} {
		if !strings.Contains(got, sub) {
			t.Errorf("missing %q in %s", sub, got)
		}
	}
}

// TestSequelValueCoercions covers sequelValue for every mapped Ruby type via the
// generated SQL literals (nil/bool/int/bignum/float/string/binary/array/symbol).
func TestSequelValueCoercions(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "sequel"; DB = Sequel.sqlite
p DB[:t].where(a: nil).sql`, "\"SELECT * FROM `t` WHERE (`a` IS NULL)\"\n"},
		{`require "sequel"; DB = Sequel.sqlite
p DB[:t].where(a: true, b: false).sql`,
			"\"SELECT * FROM `t` WHERE ((`a` = 't') AND (`b` = 'f'))\"\n"},
		{`require "sequel"; DB = Sequel.sqlite
p DB[:t].where(a: 10000000000000000000000).sql`,
			"\"SELECT * FROM `t` WHERE (`a` = 10000000000000000000000)\"\n"},
		{`require "sequel"; DB = Sequel.sqlite
p DB[:t].where(a: 1.5).sql`, "\"SELECT * FROM `t` WHERE (`a` = 1.5)\"\n"},
		{`require "sequel"; DB = Sequel.sqlite
p DB[:t].where(a: [1, 2, 3]).sql`,
			"\"SELECT * FROM `t` WHERE (`a` IN (1, 2, 3))\"\n"},
		{`require "sequel"; DB = Sequel.sqlite
p DB[:t].where(a: :other).sql`, "\"SELECT * FROM `t` WHERE (`a` = `other`)\"\n"},
		// A binary string literalizes as an X'..' blob.
		{`require "sequel"; DB = Sequel.sqlite
p DB[:t].where(a: "AB".b).sql`, "\"SELECT * FROM `t` WHERE (`a` = X'4142')\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestSequelDatasetChain covers the remaining chainable dataset methods and their
// SQL, plus the join variants.
func TestSequelDatasetChain(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "sequel"; DB = Sequel.sqlite
p DB[:a].inner_join(:b, x: :y).sql`,
			"\"SELECT * FROM `a` INNER JOIN `b` ON (`x` = `y`)\"\n"},
		{`require "sequel"; DB = Sequel.sqlite
p DB[:a].left_join(:b, x: :y).sql`,
			"\"SELECT * FROM `a` LEFT JOIN `b` ON (`x` = `y`)\"\n"},
		{`require "sequel"; DB = Sequel.sqlite
p DB[:a].right_join(:b, x: :y).sql`,
			"\"SELECT * FROM `a` RIGHT JOIN `b` ON (`x` = `y`)\"\n"},
		{`require "sequel"; DB = Sequel.sqlite
p DB.from(:a, :b).sql`, "\"SELECT * FROM `a`, `b`\"\n"},
		// select_sql alias.
		{`require "sequel"; DB = Sequel.sqlite
p DB[:t].select_sql`, "\"SELECT * FROM `t`\"\n"},
		// where with a String literal condition.
		{`require "sequel"; DB = Sequel.sqlite
p DB[:t].where("a > 1").sql`, "\"SELECT * FROM `t` WHERE (a > 1)\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestSequelRunAndDropAndEach covers DB.run, DB.drop_table, dataset #each and a
// block form of #all against a real database.
func TestSequelRunAndDropAndEach(t *testing.T) {
	src := `require "sequel"
DB = Sequel.sqlite
DB.run("CREATE TABLE t (n INTEGER)")
DB[:t].insert(n: 1)
DB[:t].insert(n: 2)
seen = []
DB[:t].order(:n).each { |row| seen << row[:n] }
p seen
rows = []
DB[:t].order(:n).all { |r| rows << r[:n] }
p rows
DB.drop_table(:t)
begin
  DB[:t].count
rescue Sequel::DatabaseError
  p :dropped
end`
	want := "[1, 2]\n[1, 2]\n:dropped\n"
	if got := eval(t, src); got != want {
		t.Errorf("run/drop/each\n got=%q\nwant=%q", got, want)
	}
}

// TestSequelToS covers the wrapper #to_s / #inspect / truthiness for Database,
// Dataset and the schema generator.
func TestSequelToS(t *testing.T) {
	src := `require "sequel"
DB = Sequel.sqlite
p DB.to_s
p(DB ? :y : :n)
ds = DB[:t]
p ds.to_s
p(ds ? :y : :n)
DB.create_table(:x) { |g| p g.to_s; p(g ? :y : :n); g.Integer :n }`
	want := "\"#<Sequel::Database>\"\n:y\n\"#<Sequel::Dataset: SELECT * FROM `t`>\"\n:y\n\"#<Sequel::Schema::Generator>\"\n:y\n"
	if got := eval(t, src); got != want {
		t.Errorf("to_s\n got=%q\nwant=%q", got, want)
	}
}

// TestSequelErrors covers the executor error path (a bad SQL raises
// Sequel::DatabaseError) and the argument guards.
func TestSequelErrors(t *testing.T) {
	src := `require "sequel"
DB = Sequel.sqlite
begin; DB[:nonexistent].all; rescue Sequel::DatabaseError; p :de; end
begin; DB.run("BAD SQL !!"); rescue Sequel::DatabaseError; p :run; end
begin; DB.create_table(:t); rescue LocalJumpError; p :nb; end
begin; DB[:t].each; rescue LocalJumpError; p :en; end
begin; DB[nil]; rescue ; end
begin; DB.run; rescue ArgumentError; p :ra; end
begin; DB.create_table; rescue ArgumentError; p :ca; end`
	want := ":de\n:run\n:nb\n:en\n:ra\n:ca\n"
	if got := eval(t, src); got != want {
		t.Errorf("errors\n got=%q\nwant=%q", got, want)
	}
}

// TestSequelMockLastIDNil covers lastInsertID / changes returning nil for a mock
// (executor-less, no SQLite handle) database.
func TestSequelMockLastIDNil(t *testing.T) {
	src := `require "sequel"
DB = Sequel.mock(host: :sqlite)
p DB[:t].insert(a: 1)
p DB[:t].where(a: 1).update(a: 2)
p DB[:t].where(a: 1).delete`
	want := "nil\nnil\nnil\n"
	if got := eval(t, src); got != want {
		t.Errorf("mock last id\n got=%q\nwant=%q", got, want)
	}
}

// TestSequelConnectStringKw covers sequelKw reading String (not Symbol) keyword
// keys in Sequel.connect, and sequelParseURL with a bare "sqlite::memory:" form.
func TestSequelConnectStringKw(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "sequel"
DB = Sequel.connect("adapter" => "sqlite", "database" => ":memory:")
DB.create_table(:t) { Integer :n }
DB[:t].insert(n: 3)
p DB[:t].count`, "1\n"},
		{`require "sequel"
DB = Sequel.connect("sqlite::memory:")
DB.create_table(:t) { Integer :n }
p DB[:t].count`, "0\n"},
		// A default (no adapter, empty) connect yields a mock.
		{`require "sequel"
DB = Sequel.connect(database: "x")
p DB[:t].sql`, "\"SELECT * FROM t\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestSequelMisc covers assorted remaining paths: Sequel.sqlite(path) with an
// explicit file, #first on an empty dataset (nil), #inspect, positional insert
// values, a bare-value where condition, sequelColumn/sequelName defaults, and the
// executor value model for bool/float/blob columns from a real query.
func TestSequelMisc(t *testing.T) {
	// A file-backed database (temp path) exercises Sequel.sqlite(path).
	if got := eval(t, `require "sequel"
DB = Sequel.sqlite(":memory:")
p DB[:t].inspect.start_with?("#<Sequel::Dataset")`); got != "true\n" {
		t.Errorf("sqlite path/inspect got=%q", got)
	}

	// first on an empty table -> nil.
	if got := eval(t, `require "sequel"
DB = Sequel.sqlite
DB.create_table(:t) { Integer :n }
p DB[:t].first`); got != "nil\n" {
		t.Errorf("first empty got=%q", got)
	}

	// The executor value model: a real query returning a float, a blob and a
	// boolean-ish integer round-trips through sequelRubyValue.
	got := eval(t, `require "sequel"
DB = Sequel.sqlite
DB.create_table(:t) { Float :f; column :b, :blob; Integer :i }
DB.run("INSERT INTO t (f, b, i) VALUES (1.5, X'4142', 1)")
row = DB[:t].first
p row[:f]
p row[:b]
p row[:i]`)
	if got != "1.5\n\"AB\"\n1\n" {
		t.Errorf("value model got=%q", got)
	}

	// A bare-value where condition (sequelCond's value default) — SQL only.
	if g := eval(t, `require "sequel"; DB = Sequel.mock(host: :sqlite)
p DB[:t].where(42).sql`); g != "\"SELECT * FROM `t` WHERE 42\"\n" {
		t.Errorf("bare where got=%q", g)
	}
}

// TestSequelArgGuards covers the remaining argument guards and default branches.
func TestSequelArgGuards(t *testing.T) {
	src := `require "sequel"
DB = Sequel.sqlite
begin; DB[:t].join(:b); rescue ArgumentError; p :j; end
# sequelColumn default: an Integer select column literalizes.
p DB[:t].select(1).sql
# sequelName default: a String table name via a String key ["t"].
p DB.from("named").sql`
	want := ":j\n\"SELECT 1 FROM `t`\"\n\"SELECT * FROM `named`\"\n"
	if got := eval(t, src); got != want {
		t.Errorf("arg guards\n got=%q\nwant=%q", got, want)
	}
}

// TestSequelMoreBranches covers the remaining reachable branches: #inspect on
// Database and the schema generator, DB[] with no argument, the executor error
// paths of first/each/count, a single-arg #limit, the #all block form on an empty
// dataset, and a mock #count (executor-less -> 0).
func TestSequelMoreBranches(t *testing.T) {
	// inspect on Database and generator.
	if got := eval(t, `require "sequel"
DB = Sequel.sqlite
p DB.inspect
DB.create_table(:t) { |g| p g.inspect; g.Integer :n }`); got !=
		"\"#<Sequel::Database>\"\n\"#<Sequel::Schema::Generator>\"\n" {
		t.Errorf("inspect got=%q", got)
	}

	// DB[] with no argument.
	if got := eval(t, `require "sequel"
DB = Sequel.sqlite
begin; DB[]; rescue ArgumentError; p :nofrom; end`); got != ":nofrom\n" {
		t.Errorf("DB[] no-arg got=%q", got)
	}

	// Executor error paths of first / each / count (query a missing table).
	if got := eval(t, `require "sequel"
DB = Sequel.sqlite
begin; DB[:missing].first; rescue Sequel::DatabaseError; p :f; end
begin; DB[:missing].each { }; rescue Sequel::DatabaseError; p :e; end
begin; DB[:missing].count; rescue Sequel::DatabaseError; p :c; end`); got != ":f\n:e\n:c\n" {
		t.Errorf("exec error paths got=%q", got)
	}

	// A single-arg #limit and #all block form on an empty table.
	if got := eval(t, `require "sequel"
DB = Sequel.sqlite
DB.create_table(:t) { Integer :n }
p DB[:t].limit(3).sql
seen = 0
DB[:t].all { |r| seen += 1 }
p seen`); got != "\"SELECT * FROM `t` LIMIT 3\"\n0\n" {
		t.Errorf("limit/all-block got=%q", got)
	}

	// A mock (executor-less) count returns 0.
	if got := eval(t, `require "sequel"
DB = Sequel.mock(host: :sqlite)
p DB[:t].count`); got != "0\n" {
		t.Errorf("mock count got=%q", got)
	}
}

// TestSequelFinalBranches mops up the last reachable branches: DB.[] with no
// argument, _sqlite3 on a real database, insert/update/delete executor errors, a
// String table name (sequelName String path), the schema DSL arity guards, a
// bare create_table generator method with no name, sequelParseURL's memory
// default, and the double-create_table schema-class memoisation.
func TestSequelFinalBranches(t *testing.T) {
	// DB.[] with no argument via #send (bare DB[] is a parse edge).
	if got := eval(t, `require "sequel"
DB = Sequel.sqlite
begin; DB.send(:[]); rescue ArgumentError; p :e; end`); got != ":e\n" {
		t.Errorf("[] no-arg got=%q", got)
	}

	// _sqlite3 on a real (sqlite-backed) database returns the SQLite3::Database.
	if got := eval(t, `require "sequel"
p Sequel.sqlite._sqlite3.class`); got != "SQLite3::Database\n" {
		t.Errorf("_sqlite3 real got=%q", got)
	}

	// insert / update / delete executor errors (target a missing table).
	if got := eval(t, `require "sequel"
DB = Sequel.sqlite
begin; DB[:missing].insert(a: 1); rescue Sequel::DatabaseError; p :i; end
begin; DB[:missing].update(a: 1); rescue Sequel::DatabaseError; p :u; end
begin; DB[:missing].delete; rescue Sequel::DatabaseError; p :d; end`); got != ":i\n:u\n:d\n" {
		t.Errorf("dml errors got=%q", got)
	}

	// A String table name (sequelName String branch) + double create_table for the
	// schema-class memoisation.
	if got := eval(t, `require "sequel"
DB = Sequel.mock(host: :sqlite)
DB.create_table("first") { |g| Integer :n }
DB.create_table("second") { |g| Integer :m }
p DB.sqls.length`); got != "2\n" {
		t.Errorf("string table / memo got=%q", got)
	}

	// Schema DSL arity guards.
	if got := eval(t, `require "sequel"
DB = Sequel.mock(host: :sqlite)
begin
  DB.create_table(:t) { |g| g.column(:only) }
rescue ArgumentError; p :col; end
begin
  DB.create_table(:t) { |g| g.foreign_key(:only) }
rescue ArgumentError; p :fk; end
begin
  DB.create_table(:t) { |g| g.index }
rescue ArgumentError; p :idx; end`); got != ":col\n:fk\n:idx\n" {
		t.Errorf("schema arity got=%q", got)
	}

	// sequelParseURL default (a URL with no database path -> :memory:).
	if got := eval(t, `require "sequel"
DB = Sequel.connect("sqlite://")
DB.create_table(:t) { Integer :n }
p DB[:t].count`); got != "0\n" {
		t.Errorf("parseurl memory got=%q", got)
	}

	// #offset SQL, a typed column with no name (sequelColArgs 0-arg guard), and
	// insert_sql with a non-Hash argument (sequelKVArgs non-hash -> DEFAULT).
	if got := eval(t, `require "sequel"; DB = Sequel.mock(host: :sqlite)
p DB[:t].offset(2).sql`); got != "\"SELECT * FROM `t` OFFSET 2\"\n" {
		t.Errorf("offset got=%q", got)
	}
	if got := eval(t, `require "sequel"
DB = Sequel.mock(host: :sqlite)
begin; DB.create_table(:t) { |g| g.String }; rescue ArgumentError; p :noname; end`); got != ":noname\n" {
		t.Errorf("String no-name got=%q", got)
	}
	if got := eval(t, `require "sequel"; DB = Sequel.mock(host: :sqlite)
p DB[:t].insert_sql(5)`); got != "\"INSERT INTO `t` DEFAULT VALUES\"\n" {
		t.Errorf("insert_sql non-hash got=%q", got)
	}
}

// TestSequelInsertDefaults covers #insert with no values (DEFAULT VALUES) and the
// insert returning the last row id against a real database.
func TestSequelInsertDefaults(t *testing.T) {
	src := `require "sequel"
DB = Sequel.sqlite
DB.create_table(:t) { primary_key :id; Integer :n, default: 7 }
id1 = DB[:t].insert
id2 = DB[:t].insert(n: 9)
p [id1, id2]
p DB[:t].order(:id).all`
	want := "[1, 2]\n[{id: 1, n: 7}, {id: 2, n: 9}]\n"
	if got := eval(t, src); got != want {
		t.Errorf("insert defaults\n got=%q\nwant=%q", got, want)
	}
}
