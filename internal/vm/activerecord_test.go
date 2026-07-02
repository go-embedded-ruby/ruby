// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"path/filepath"
	"testing"
)

// modelSrc declares a User model with columns and a presence validation, shared
// by the query tests below.
const arModelSrc = `require "active_record"
User = ActiveRecord::Model.new("User", "users") do
  column :id, :integer
  column :name, :string
  column :age, :integer
  validates_presence_of :name
end
`

// arConnSrc opens an in-memory sqlite3 connection and seeds the users table.
const arConnSrc = `ActiveRecord::Base.establish_connection(database: ":memory:")
c = ActiveRecord::Base.connection
c.execute("CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, age INTEGER)")
c.execute("INSERT INTO users (name, age) VALUES ('bob', 30), ('amy', 25)")
`

// TestActiveRecordConstants covers the ActiveRecord module, its Model / Relation
// / Record / Errors / Base classes and the error tree (require "active_record").
func TestActiveRecordConstants(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "active_record"; p ActiveRecord.is_a?(Module)`, "true\n"},
		{`p require "active_record"`, "true\n"},
		{`require "active_record"; p require "active_record"`, "false\n"},
		{`require "active_record"; p ActiveRecord::RecordInvalid < ActiveRecord::ActiveRecordError`, "true\n"},
		{`require "active_record"; p ActiveRecord::ActiveRecordError < StandardError`, "true\n"},
		{arModelSrc + `p User.class`, "ActiveRecord::Model\n"},
		{arModelSrc + `p User.table_name`, "\"users\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestActiveRecordToSQL covers the chainable relation surface and byte-faithful
// #to_sql (no database connection needed).
func TestActiveRecordToSQL(t *testing.T) {
	cases := []struct{ src, want string }{
		{arModelSrc + `puts User.where(age: 30).order(:name).limit(5).to_sql`,
			"SELECT \"users\".* FROM \"users\" WHERE \"users\".\"age\" = 30 ORDER BY \"users\".\"name\" ASC LIMIT 5\n"},
		{arModelSrc + `puts User.select(:name).where("age > ?", 18).to_sql`,
			"SELECT \"users\".\"name\" FROM \"users\" WHERE (age > 18)\n"},
		{arModelSrc + `puts User.all.to_sql`, "SELECT \"users\".* FROM \"users\"\n"},
		{arModelSrc + `puts User.where(age: 30).not(name: "x").to_sql`,
			"SELECT \"users\".* FROM \"users\" WHERE \"users\".\"age\" = 30 AND \"users\".\"name\" != 'x'\n"},
		{arModelSrc + `puts User.all.offset(10).distinct.to_sql`,
			"SELECT DISTINCT \"users\".* FROM \"users\" OFFSET 10\n"},
		{arModelSrc + `puts User.all.group(:age).having("count(*) > ?", 1).to_sql`,
			"SELECT \"users\".* FROM \"users\" GROUP BY \"users\".\"age\" HAVING (count(*) > 1)\n"},
		// to_s aliases to_sql; the relation's inspect is stable.
		{arModelSrc + `p User.all.to_s`, "\"SELECT \\\"users\\\".* FROM \\\"users\\\"\"\n"},
		// #insert_sql renders the INSERT.
		{arModelSrc + `puts User.insert_sql(name: "bob", age: 30)`,
			"INSERT INTO \"users\" (\"age\", \"name\") VALUES (30, 'bob')\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestActiveRecordValidations covers the validating Record surface.
func TestActiveRecordValidations(t *testing.T) {
	cases := []struct{ src, want string }{
		{arModelSrc + `rec = User.build(age: 30); p rec.valid?`, "false\n"},
		{arModelSrc + `rec = User.build(age: 30); p rec.errors.full_messages`, "[\"Name can't be blank\"]\n"},
		{arModelSrc + `rec = User.build(name: "bob", age: 30); p rec.valid?`, "true\n"},
		{arModelSrc + `rec = User.build(age: 30); p rec.errors.empty?; p rec.errors.count`, "false\n1\n"},
		{arModelSrc + `rec = User.build(age: 30); p rec.errors[:name]`, "[\"can't be blank\"]\n"},
		{arModelSrc + `rec = User.build(age: 30); p rec.errors.messages`, "{name: [\"can't be blank\"]}\n"},
		// Record attribute access + dirty tracking.
		{arModelSrc + `rec = User.new(name: "bob"); p rec["name"]; rec["name"] = "amy"; p rec["name"]; p rec.changed?`,
			"\"bob\"\n\"amy\"\ntrue\n"},
		{arModelSrc + `rec = User.build(name: "bob", age: 30); p rec.attributes["age"]`, "30\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestActiveRecordQueries covers the adapter-backed execution methods running
// real queries against a seeded in-memory sqlite3 database.
func TestActiveRecordQueries(t *testing.T) {
	cases := []struct{ src, want string }{
		{arModelSrc + arConnSrc + `p ActiveRecord::Base.connected?`, "true\n"},
		{arModelSrc + arConnSrc + `rows = User.where(age: 30).to_a; p rows.length; p rows.first["name"]`, "1\n\"bob\"\n"},
		{arModelSrc + arConnSrc + `p User.all.count`, "2\n"},
		{arModelSrc + arConnSrc + `p User.where(name: "amy").exists?`, "true\n"},
		{arModelSrc + arConnSrc + `p User.where(name: "nope").exists?`, "false\n"},
		{arModelSrc + arConnSrc + `p User.order(:age).pluck(:name)`, "[\"amy\", \"bob\"]\n"},
		{arModelSrc + arConnSrc + `p User.order(:age).pluck(:name, :age)`, "[[\"amy\", 25], [\"bob\", 30]]\n"},
		{arModelSrc + arConnSrc + `r = User.order(:age).first; p r["name"]`, "\"amy\"\n"},
		{arModelSrc + arConnSrc + `p User.where(name: "nope").first`, "nil\n"},
		// #create validates then inserts; an invalid record is not persisted.
		{arModelSrc + arConnSrc + `User.create(name: "cid", age: 40); p User.all.count`, "3\n"},
		{arModelSrc + arConnSrc + `rec = User.create(age: 1); p rec.valid?; p User.all.count`, "false\n2\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestActiveRecordConnErrors covers the connection-not-established arm and the
// establish_connection path variants.
func TestActiveRecordConnErrors(t *testing.T) {
	cases := []struct{ src, want string }{
		// Executing without a connection raises ConnectionNotEstablished.
		{arModelSrc + `begin; User.all.to_a; rescue ActiveRecord::ConnectionNotEstablished; p :noconn; end`, ":noconn\n"},
		{arModelSrc + `begin; User.all.count; rescue ActiveRecord::ConnectionNotEstablished; p :noconn; end`, ":noconn\n"},
		{`require "active_record"; begin; ActiveRecord::Base.connection; rescue ActiveRecord::ConnectionNotEstablished; p :noconn; end`, ":noconn\n"},
		// establish_connection accepts a String path and a String "database" key too.
		{`require "active_record"; ActiveRecord::Base.establish_connection(":memory:"); p ActiveRecord::Base.connected?`, "true\n"},
		{`require "active_record"; ActiveRecord::Base.establish_connection("database" => ":memory:"); p ActiveRecord::Base.connected?`, "true\n"},
		{`require "active_record"; ActiveRecord::Base.establish_connection; p ActiveRecord::Base.connected?`, "true\n"},
		{`require "active_record"; ActiveRecord::Base.establish_connection({}); p ActiveRecord::Base.connected?`, "true\n"},
		{`require "active_record"; p ActiveRecord::Base.connected?`, "false\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestActiveRecordWrappers covers each wrapper value's #to_s / #inspect /
// truthiness (Model / Relation / Record / Errors and the model-block builder).
func TestActiveRecordWrappers(t *testing.T) {
	cases := []struct{ src, want string }{
		// Model renders its name and is always truthy.
		{arModelSrc + `p User`, "#<ActiveRecord::Model User>\n"},
		{arModelSrc + `p User.to_s`, "\"#<ActiveRecord::Model User>\"\n"},
		{arModelSrc + `p(User ? :y : :n)`, ":y\n"},
		// Relation: #to_s renders the SQL (Object#to_s -> the wrapper), #inspect is
		// the stable class tag, and it is truthy.
		{arModelSrc + `p User.all`, "#<ActiveRecord::Relation>\n"},
		{arModelSrc + `p(User.all ? :y : :n)`, ":y\n"},
		// Record renders a stable tag for both to_s and inspect, and is truthy.
		{arModelSrc + `rec = User.build(name: "x"); p rec`, "#<ActiveRecord::Record>\n"},
		{arModelSrc + `rec = User.build(name: "x"); p rec.to_s`, "\"#<ActiveRecord::Record>\"\n"},
		{arModelSrc + `rec = User.build(name: "x"); p(rec ? :y : :n)`, ":y\n"},
		// Errors renders a stable tag and is truthy even when empty.
		{arModelSrc + `e = User.build(name: "x").errors; p e`, "#<ActiveRecord::Errors>\n"},
		{arModelSrc + `e = User.build(name: "x").errors; p e.to_s`, "\"#<ActiveRecord::Errors>\"\n"},
		{arModelSrc + `e = User.build(name: "x").errors; p(e ? :y : :n)`, ":y\n"},
		// The model-block builder (the DSL self) renders its own tag and is truthy.
		{`require "active_record"
ActiveRecord::Model.new("B", "bs") do
  p self
  p self.to_s
  p(self ? :y : :n)
end`, "#<ActiveRecord::Model::DSL>\n\"#<ActiveRecord::Model::DSL>\"\n:y\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestActiveRecordRelationExtra covers the relation methods and helper coercions
// not reached by the headline #to_sql cases: model/relation #joins, relation
// #select / #order with a String and a non-name argument, #or, #build with no or
// a non-Hash argument, and the #limit coercion arms.
func TestActiveRecordRelationExtra(t *testing.T) {
	cases := []struct{ src, want string }{
		// #joins on the model and on a relation both yield relations.
		{arModelSrc + `p User.joins(:accounts).class`, "ActiveRecord::Relation\n"},
		{arModelSrc + `p User.all.joins(:accounts).class`, "ActiveRecord::Relation\n"},
		// Relation #select (String column) and #order (non-name arg -> arToGo).
		{arModelSrc + `puts User.all.select("name").to_sql`,
			"SELECT \"users\".\"name\" FROM \"users\"\n"},
		{arModelSrc + `p User.all.order(1).class`, "ActiveRecord::Relation\n"},
		// #or unions two relations; with no argument it is an ArgumentError.
		{arModelSrc + `puts User.where(age: 30).or(User.where(age: 25)).to_sql`,
			"SELECT \"users\".* FROM \"users\" WHERE (\"users\".\"age\" = 30 OR \"users\".\"age\" = 25)\n"},
		{arModelSrc + `begin; User.all.or; rescue ArgumentError; p :a; end`, ":a\n"},
		// #build with no argument / a non-Hash argument both yield a Record.
		{arModelSrc + `p User.build.class`, "ActiveRecord::Record\n"},
		{arModelSrc + `p User.build(5).class`, "ActiveRecord::Record\n"},
		// #limit with no / a non-Integer argument coerces to 0 and stays a relation.
		{arModelSrc + `p User.all.limit("x").class`, "ActiveRecord::Relation\n"},
		// #[] with a non-String/Symbol key coerces the key through to_s.
		{arModelSrc + `p User.build(name: "x")[123]`, "nil\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestActiveRecordCondValueTypes covers arToGo mapping every Ruby value kind a
// where-condition can carry into the query builder (nil / bool / Bignum / Float /
// Symbol / Array / an unmapped value's to_s fallback).
func TestActiveRecordCondValueTypes(t *testing.T) {
	cases := []string{
		`p User.where(id: nil).class`,
		`p User.where(active: true).class`,
		`p User.where(id: 100000000000000000000).class`,
		`p User.where(age: 1.5).class`,
		`p User.where(status: :active).class`,
		`p User.where(id: [1, 2, 3]).class`,
		`p User.where(id: (1..3)).class`,
	}
	for _, call := range cases {
		src := arModelSrc + call
		if got := eval(t, src); got != "ActiveRecord::Relation\n" {
			t.Errorf("call=%q got=%q", call, got)
		}
	}
}

// TestActiveRecordScanTypes covers the Float / NULL values a real query returns
// flowing back through arValueToRuby, running against a seeded in-memory database
// (the exhaustive per-type mapping is unit-tested white-box).
func TestActiveRecordScanTypes(t *testing.T) {
	src := `require "active_record"
T = ActiveRecord::Model.new("T", "t") do
  column :id, :integer
  column :r, :float
  column :n, :string
end
ActiveRecord::Base.establish_connection(database: ":memory:")
c = ActiveRecord::Base.connection
c.execute("CREATE TABLE t (id INTEGER, r REAL, n TEXT)")
c.execute("INSERT INTO t (id, r, n) VALUES (1, 1.5, NULL)")
row = T.all.to_a.first
p row["r"]
p row["n"]
`
	want := "1.5\nnil\n"
	if got := eval(t, src); got != want {
		t.Errorf("scan types got=%q want=%q", got, want)
	}
}

// TestActiveRecordExecErrors covers the StatementInvalid arms of every
// adapter-backed execution method (a query against a table that does not exist)
// and the #create INSERT-failure arm (an attribute with no matching column).
func TestActiveRecordExecErrors(t *testing.T) {
	const ghost = `require "active_record"
Ghost = ActiveRecord::Model.new("Ghost", "ghosts") do
  column :id, :integer
end
ActiveRecord::Base.establish_connection(database: ":memory:")
`
	cases := []struct{ src, want string }{
		{ghost + `begin; Ghost.all.to_a; rescue ActiveRecord::StatementInvalid; p :se; end`, ":se\n"},
		{ghost + `begin; Ghost.all.count; rescue ActiveRecord::StatementInvalid; p :se; end`, ":se\n"},
		{ghost + `begin; Ghost.all.exists?; rescue ActiveRecord::StatementInvalid; p :se; end`, ":se\n"},
		{ghost + `begin; Ghost.all.first; rescue ActiveRecord::StatementInvalid; p :se; end`, ":se\n"},
		{ghost + `begin; Ghost.all.pluck(:id); rescue ActiveRecord::StatementInvalid; p :se; end`, ":se\n"},
		// #create whose INSERT targets a column the table lacks raises too.
		{`require "active_record"
Bad = ActiveRecord::Model.new("Bad", "users") do
  column :nope, :integer
end
ActiveRecord::Base.establish_connection(database: ":memory:")
c = ActiveRecord::Base.connection
c.execute("CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)")
begin; Bad.create(nope: 5); rescue ActiveRecord::StatementInvalid; p :se; end`, ":se\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestActiveRecordConnPathError covers arConnect raising StatementInvalid when
// the database file cannot be opened (a path under a directory that does not
// exist). The path is built with filepath.ToSlash so the embedded Ruby source is
// Windows-safe.
func TestActiveRecordConnPathError(t *testing.T) {
	bad := filepath.ToSlash(filepath.Join(t.TempDir(), "no-such-subdir", "x.db"))
	src := `require "active_record"
begin
  ActiveRecord::Base.establish_connection("` + bad + `")
  p :opened
rescue ActiveRecord::StatementInvalid
  p :badconn
end`
	if got := eval(t, src); got != ":badconn\n" {
		t.Errorf("bad conn got=%q", got)
	}
}

// TestActiveRecordDSLExtra covers the model-block DSL arg-arity error arms
// (validates_length_of / validates_inclusion_of / belongs_to / has_many with no
// argument) and the option-parsing arms of arLengthOpts / arInList (missing,
// non-Hash and non-Integer / non-Array options, and the maximum: / is: cases).
func TestActiveRecordDSLExtra(t *testing.T) {
	arity := func(call string) (string, string) {
		return `require "active_record"
begin
  ActiveRecord::Model.new("X", "xs") { ` + call + ` }
rescue ArgumentError
  p :a
end`, ":a\n"
	}
	cases := []struct{ src, want string }{}
	for _, call := range []string{"validates_length_of", "validates_inclusion_of", "belongs_to", "has_many"} {
		s, w := arity(call)
		cases = append(cases, struct{ src, want string }{s, w})
	}
	// arLengthOpts / arInList option arms: every branch parses without error, so
	// the model builds and #class confirms it.
	cases = append(cases, struct{ src, want string }{`require "active_record"
X = ActiveRecord::Model.new("X", "xs") do
  column :t, :string
  validates_length_of :t
  validates_length_of :t, "notahash"
  validates_length_of :t, minimum: "notanint"
  validates_length_of :t, maximum: 8
  validates_length_of :t, is: 4
  validates_inclusion_of :t
  validates_inclusion_of :t, "notahash"
  validates_inclusion_of :t, foo: 1
end
p X.class`, "ActiveRecord::Model\n"})
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestActiveRecordSchema covers the schema DDL string generators and the
// associations / additional validators declared in a model block.
func TestActiveRecordSchema(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "active_record"; puts ActiveRecord::Schema.add_column_sql("users", "email", "string")`,
			"ALTER TABLE \"users\" ADD \"email\" varchar\n"},
		{`require "active_record"; puts ActiveRecord::Schema.add_index_sql("users", ["name"], true)`,
			"CREATE UNIQUE INDEX \"index_users_on_name\" ON \"users\" (\"name\")\n"},
		{`require "active_record"; puts ActiveRecord::Schema.add_index_sql("users", "name")`,
			"CREATE INDEX \"index_users_on_name\" ON \"users\" (\"name\")\n"},
		// Associations + length/inclusion validators declare without error.
		{`require "active_record"
Post = ActiveRecord::Model.new("Post", "posts") do
  column :id, :integer
  column :title, :string
  column :user_id, :integer
  belongs_to :user
  validates_length_of :title, minimum: 3
  validates_inclusion_of :title, in: ["a", "abc"]
end
p Post.class`, "ActiveRecord::Model\n"},
		{`require "active_record"
U = ActiveRecord::Model.new("U", "us") do
  column :id, :integer
  has_many :posts, class_name: "Post"
end
p U.table_name`, "\"us\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestActiveRecordArgErrors covers the argument-arity error arms.
func TestActiveRecordArgErrors(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "active_record"; begin; ActiveRecord::Model.new("U"); rescue ArgumentError; p :a; end`, ":a\n"},
		{`require "active_record"; begin; ActiveRecord::Model.new("U", "us") { column }; rescue ArgumentError; p :a; end`, ":a\n"},
		{arModelSrc + `begin; User.build(name: "x").errors[]; rescue ArgumentError; p :a; end`, ":a\n"},
		{arModelSrc + `begin; User.build(name: "x")[]; rescue ArgumentError; p :a; end`, ":a\n"},
		{arModelSrc + `begin; User.build(name: "x")[:x] = 1 rescue nil; rec = User.build(name: "x"); rec.send(:[]=, "only"); rescue ArgumentError; p :a; end`, ":a\n"},
		{`require "active_record"; begin; ActiveRecord::Schema.add_column_sql("t"); rescue ArgumentError; p :a; end`, ":a\n"},
		{`require "active_record"; begin; ActiveRecord::Schema.add_index_sql("t"); rescue ArgumentError; p :a; end`, ":a\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}
