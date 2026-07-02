// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// TestSequelConstants covers the Sequel loadable module and its error tree.
func TestSequelConstants(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "sequel"; p require "sequel"`, "false\n"},
		{`p require "sequel"`, "true\n"},
		{`require "sequel"; p Sequel.is_a?(Module)`, "true\n"},
		{`require "sequel"; p Sequel::Error < StandardError`, "true\n"},
		{`require "sequel"; p Sequel::DatabaseError < Sequel::Error`, "true\n"},
		{`require "sequel"; p Sequel.sqlite.class`, "Sequel::Database\n"},
		{`require "sequel"; p Sequel.sqlite[:t].class`, "Sequel::Dataset\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestSequelRealExecution covers the full round-trip against a live SQLite
// database (the executor seam wired to go-ruby-sqlite3): create_table, insert,
// where/select/order/all, first, count, update, delete — all really run.
func TestSequelRealExecution(t *testing.T) {
	src := `require "sequel"
DB = Sequel.sqlite
DB.create_table(:items) do
  primary_key :id
  String :name
  Integer :qty
end
DB[:items].insert(name: "widget", qty: 5)
DB[:items].insert(name: "gadget", qty: 3)
p DB[:items].where(qty: 5).all
p DB[:items].order(:qty).select(:name).all
p DB[:items].count
p DB[:items].where(name: "gadget").first
p DB[:items].where(name: "widget").update(qty: 9)
p DB[:items].where(name: "widget").all
p DB[:items].where(name: "gadget").delete
p DB[:items].count`
	want := strings.Join([]string{
		`[{id: 1, name: "widget", qty: 5}]`,
		`[{name: "gadget"}, {name: "widget"}]`,
		"2",
		`{id: 2, name: "gadget", qty: 3}`,
		"1", // update affected 1 row
		`[{id: 1, name: "widget", qty: 9}]`,
		"1", // delete affected 1 row
		"1", // one row left
	}, "\n") + "\n"
	if got := eval(t, src); got != want {
		t.Errorf("real execution\n got=%q\nwant=%q", got, want)
	}
}

// TestSequelSQLGeneration covers the SQL-text builders (no execution needed): the
// dataset chain and the DML builders emit the exact SQL the gem does.
func TestSequelSQLGeneration(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "sequel"; DB = Sequel.sqlite
p DB[:t].where(a: 1).sql`, "\"SELECT * FROM `t` WHERE (`a` = 1)\"\n"},
		{`require "sequel"; DB = Sequel.sqlite
p DB[:t].select(:a, :b).order(:a).limit(5).sql`,
			"\"SELECT `a`, `b` FROM `t` ORDER BY `a` LIMIT 5\"\n"},
		{`require "sequel"; DB = Sequel.sqlite
p DB[:t].insert_sql(a: 1, b: "x")`, "\"INSERT INTO `t` (`a`, `b`) VALUES (1, 'x')\"\n"},
		{`require "sequel"; DB = Sequel.sqlite
p DB[:t].where(id: 1).update_sql(a: 2)`, "\"UPDATE `t` SET `a` = 2 WHERE (`id` = 1)\"\n"},
		{`require "sequel"; DB = Sequel.sqlite
p DB[:t].where(id: 1).delete_sql`, "\"DELETE FROM `t` WHERE (`id` = 1)\"\n"},
		// Chainable joins / distinct / group / having / exclude / offset / reverse.
		{`require "sequel"; DB = Sequel.sqlite
p DB[:a].join(:b, id: :a_id).sql`,
			"\"SELECT * FROM `a` INNER JOIN `b` ON (`id` = `a_id`)\"\n"},
		{`require "sequel"; DB = Sequel.sqlite
p DB[:t].distinct.group(:a).having("count(*) > 1").sql`,
			"\"SELECT DISTINCT * FROM `t` GROUP BY `a` HAVING (count(*) > 1)\"\n"},
		{`require "sequel"; DB = Sequel.sqlite
p DB[:t].exclude(a: 1).order(:a).reverse.sql`,
			"\"SELECT * FROM `t` WHERE (`a` != 1) ORDER BY `a` DESC\"\n"},
		{`require "sequel"; DB = Sequel.sqlite
p DB[:t].limit(5, 10).sql`,
			"\"SELECT * FROM `t` LIMIT 5 OFFSET 10\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestSequelMock covers the executor-less mock database: it generates SQL and
// logs DDL but runs nothing.
func TestSequelMock(t *testing.T) {
	src := `require "sequel"
DB = Sequel.mock(host: :sqlite)
DB.create_table(:t) { primary_key :id; String :name }
p DB.sqls
p DB[:t].sql
p DB._sqlite3`
	want := strings.Join([]string{
		"[\"CREATE TABLE `t` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `name` varchar(255))\"]",
		"\"SELECT * FROM `t`\"",
		"nil",
	}, "\n") + "\n"
	if got := eval(t, src); got != want {
		t.Errorf("mock\n got=%q\nwant=%q", got, want)
	}
}

// TestSequelConnect covers Sequel.connect with the sqlite adapter (real) and with
// a connection URL, plus a non-sqlite adapter falling back to a mock.
func TestSequelConnect(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "sequel"
DB = Sequel.connect(adapter: "sqlite", database: ":memory:")
DB.create_table(:t) { Integer :n }
DB[:t].insert(n: 42)
p DB[:t].all`, "[{n: 42}]\n"},
		{`require "sequel"
DB = Sequel.connect("sqlite://:memory:")
DB.create_table(:t) { Integer :n }
DB[:t].insert(n: 7)
p DB[:t].first`, "{n: 7}\n"},
		// A non-sqlite adapter yields a mock (no executor) — SQL still generates.
		{`require "sequel"
DB = Sequel.connect(adapter: "postgres", database: "x")
p DB[:t].where(a: 1).sql`, "\"SELECT * FROM \\\"t\\\" WHERE (\\\"a\\\" = 1)\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}
