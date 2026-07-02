// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// arSchemaSetup opens an in-memory connection so the Schema.define DDL and the
// Base-subclass models below run against a real database.
const arSchemaSetup = `require "active_record"
ActiveRecord::Base.establish_connection(adapter: "sqlite3", database: ":memory:")
`

// TestActiveRecordSchemaDefine covers ActiveRecord::Schema.define + the
// create_table column DSL end to end: the schema builds the table through the
// connected adapter and a Base subclass then reads the seeded rows back — the
// idiomatic Rails route the stage-4b conformance app runs.
func TestActiveRecordSchemaDefine(t *testing.T) {
	src := arSchemaSetup + `
ActiveRecord::Schema.define do
  create_table :users do |t|
    t.string  :name
    t.integer :age
  end
end

class User < ActiveRecord::Base
end

[["amy", 30], ["bob", 25], ["cat", 40]].each { |n, a| User.create!(name: n, age: a) }

rows = User.where("age >= ?", 26).order(:name).to_a
puts rows.map { |u| "#{u.name} (#{u.age})" }.join(",")
p User.count
p User.where("age >= ?", 26).to_a.length
`
	want := "amy (30),cat (40)\n3\n2\n"
	if got := eval(t, src); got != want {
		t.Errorf("schema.define route got=%q want=%q", got, want)
	}
}

// TestActiveRecordSchemaColumnTypes covers every create_table column shortcut,
// timestamps, references/belongs_to, the generic column, primary_key, t.index,
// the create_table id:/primary_key: options, and the column option Hash
// (null:/default:/limit:). Each declaration executes against sqlite, so a clean
// run proves the generated DDL is valid.
func TestActiveRecordSchemaColumnTypes(t *testing.T) {
	src := arSchemaSetup + `
ActiveRecord::Schema.define do
  create_table :things do |t|
    t.string  :name, null: false, limit: 40
    t.integer :qty, default: 0
    t.float   :ratio
    t.boolean :active
    t.text    :body
    t.datetime :seen_at
    t.timestamp :touched_at
    t.date    :on
    t.time    :at
    t.binary  :blob
    t.decimal :price
    t.bigint  :big
    t.column  :extra, :string
    t.references :author
    t.belongs_to :editor
    t.timestamps
    t.index :name, unique: true, name: "idx_things_name"
    t.index [:qty, :active]
  end
  create_table :nopk, id: false do |t|
    t.string :k
  end
  create_table :docs, primary_key: :uuid do |t|
    t.text :body
  end
  create_table :evt, id: :eid do |t|
    t.primary_key :eid
    t.string :kind
  end
  add_column :things, :note, :string, limit: 10
  add_index :things, [:qty], unique: false
  execute "INSERT INTO nopk (k) VALUES ('x')"
end
class Nopk < ActiveRecord::Base
  self.table_name = "nopk"
end
p Nopk.count
`
	if got := eval(t, src); got != "1\n" {
		t.Errorf("column types got=%q", got)
	}
}

// TestActiveRecordSchemaMisc covers the create_table id:true (default PK kept)
// and non-Hash options arms, a non-Hash index option, and the schema/table DSL
// value objects' to_s / inspect / truthiness.
func TestActiveRecordSchemaMisc(t *testing.T) {
	src := arSchemaSetup + `
ActiveRecord::Schema.define do
  p self.to_s
  p self.inspect
  p(self ? :truthy : :falsy)
  create_table :a, id: true do |t|
    p t.to_s
    p t.inspect
    p(t ? :truthy : :falsy)
    t.string :n
    t.index :n, "notahash"
  end
  create_table :b, "notahash" do |t|
    t.string :n
  end
end
p ActiveRecord::Base.connected?
`
	want := "\"#<ActiveRecord::Schema::Definition>\"\n" +
		"\"#<ActiveRecord::Schema::Definition>\"\n" +
		":truthy\n" +
		"\"#<ActiveRecord::Schema::TableDefinition>\"\n" +
		"\"#<ActiveRecord::Schema::TableDefinition>\"\n" +
		":truthy\n" +
		"true\n"
	if got := eval(t, src); got != want {
		t.Errorf("schema misc got=%q want=%q", got, want)
	}
}

// TestActiveRecordBaseModel covers the Base-subclass ORM class methods:
// table_name inference, the table_name= override, all/where/order/first/find,
// and the dynamic attribute accessors (reader, writer, respond_to?).
func TestActiveRecordBaseModel(t *testing.T) {
	base := arSchemaSetup + `
ActiveRecord::Schema.define do
  create_table :people do |t|
    t.string :name
    t.integer :age
  end
end
class Person < ActiveRecord::Base
end
Person.create!(name: "amy", age: 30)
`
	cases := []struct{ src, want string }{
		// table name inferred from the class name (Person -> people).
		{base + `p Person.table_name`, "\"people\"\n"},
		// all + first + attribute readers.
		{base + `u = Person.all.first; puts "#{u.name}/#{u.age}"`, "amy/30\n"},
		{base + `p Person.first.name`, "\"amy\"\n"},
		// find by primary key.
		{base + `p Person.find(1).name`, "\"amy\"\n"},
		// where + order + to_a.
		{base + `p Person.where(name: "amy").order(:age).to_a.length`, "1\n"},
		// attribute writer + respond_to? via respond_to_missing?.
		{base + `u = Person.first; u.name = "zoe"; p u.name; p u.respond_to?(:age); p u.respond_to?(:nope)`,
			"\"zoe\"\n" + "true\n" + "false\n"},
		// count.
		{base + `p Person.count`, "1\n"},
		// order called directly on the model class.
		{base + `p Person.order(:name).first.name`, "\"amy\"\n"},
		// non-bang create inserts and returns the record.
		{base + `Person.create(name: "bea", age: 22); p Person.count`, "2\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestActiveRecordBaseTableNameOverride covers self.table_name = (the explicit
// override, invalidating the inferred-name cache) on an anonymous Class.new
// subclass — the exact form the conformance harness probe uses.
func TestActiveRecordBaseTableNameOverride(t *testing.T) {
	src := arSchemaSetup + `
ActiveRecord::Schema.define { create_table(:ar_probe) { |t| t.string :name } }
klass = Class.new(ActiveRecord::Base) { self.table_name = "ar_probe" }
p klass.table_name
klass.create!(name: "x")
p klass.where("name = ?", "x").order(:name).to_a.length
`
	if got := eval(t, src); got != "\"ar_probe\"\n1\n" {
		t.Errorf("table_name override got=%q", got)
	}
}

// TestActiveRecordCreateValidation covers arCreateRecord's validation arms
// (shared by the factory model and Base): create! raises RecordInvalid on an
// invalid record while create returns the unsaved record.
func TestActiveRecordCreateValidation(t *testing.T) {
	model := `require "active_record"
V = ActiveRecord::Model.new("V", "vs") do
  column :id, :integer
  column :name, :string
  validates_presence_of :name
end
ActiveRecord::Base.establish_connection(database: ":memory:")
c = ActiveRecord::Base.connection
c.execute("CREATE TABLE vs (id INTEGER PRIMARY KEY, name TEXT)")
`
	cases := []struct{ src, want string }{
		{model + `begin; V.create!(name: nil); rescue ActiveRecord::RecordInvalid => e; p :invalid; end`, ":invalid\n"},
		{model + `r = V.create(name: nil); p r.valid?`, "false\n"},
		{model + `r = V.create!(name: "ok"); p r.valid?`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestActiveRecordSchemaErrors covers the schema-DSL error arms: the not-found /
// bad-DDL StatementInvalid paths, the argument-arity ArgumentError arms of every
// schema/table DSL method, Schema.define without a block, and find on a missing
// row raising RecordNotFound.
func TestActiveRecordSchemaErrors(t *testing.T) {
	cases := []struct{ src, want string }{
		// Schema.define requires a block.
		{`require "active_record"; begin; ActiveRecord::Schema.define; rescue ArgumentError; p :a; end`, ":a\n"},
		// create_table needs a name.
		{arSchemaSetup + `ActiveRecord::Schema.define { begin; create_table; rescue ArgumentError; p :a; end }`, ":a\n"},
		// A create_table whose DDL is invalid raises StatementInvalid (duplicate table).
		{arSchemaSetup + `ActiveRecord::Base.connection.execute("CREATE TABLE dup (id INTEGER)")
ActiveRecord::Schema.define { begin; create_table(:dup) { |t| t.string :x }; rescue ActiveRecord::StatementInvalid; p :se; end }`, ":se\n"},
		// A create_table index whose DDL is invalid (a duplicate index name)
		// raises StatementInvalid from the index-execution arm.
		{arSchemaSetup + `ActiveRecord::Base.connection.execute("CREATE TABLE pre (x TEXT)")
ActiveRecord::Base.connection.execute("CREATE INDEX shared ON pre(x)")
ActiveRecord::Schema.define { begin; create_table(:idxbad) { |t| t.string :x; t.index :x, name: "shared" }; rescue ActiveRecord::StatementInvalid; p :se; end }`, ":se\n"},
		// add_index / add_column / execute error + arity arms.
		{arSchemaSetup + `ActiveRecord::Schema.define { begin; add_index("t"); rescue ArgumentError; p :a; end }`, ":a\n"},
		{arSchemaSetup + `ActiveRecord::Schema.define { begin; add_column("t", "c"); rescue ArgumentError; p :a; end }`, ":a\n"},
		{arSchemaSetup + `ActiveRecord::Schema.define { begin; execute; rescue ArgumentError; p :a; end }`, ":a\n"},
		{arSchemaSetup + `ActiveRecord::Schema.define { begin; add_index("ghost", :c); rescue ActiveRecord::StatementInvalid; p :se; end }`, ":se\n"},
		{arSchemaSetup + `ActiveRecord::Schema.define { begin; add_column("ghost", "c", "string"); rescue ActiveRecord::StatementInvalid; p :se; end }`, ":se\n"},
		{arSchemaSetup + `ActiveRecord::Schema.define { begin; execute("NOT SQL"); rescue ActiveRecord::StatementInvalid; p :se; end }`, ":se\n"},
		// Table-DSL arity arms.
		{arSchemaSetup + `ActiveRecord::Schema.define { create_table(:t) { |t| begin; t.string; rescue ArgumentError; p :a; end } }`, ":a\n"},
		{arSchemaSetup + `ActiveRecord::Schema.define { create_table(:t) { |t| begin; t.column(:c); rescue ArgumentError; p :a; end } }`, ":a\n"},
		{arSchemaSetup + `ActiveRecord::Schema.define { create_table(:t) { |t| begin; t.references; rescue ArgumentError; p :a; end } }`, ":a\n"},
		{arSchemaSetup + `ActiveRecord::Schema.define { create_table(:t) { |t| begin; t.index; rescue ArgumentError; p :a; end } }`, ":a\n"},
		{arSchemaSetup + `ActiveRecord::Schema.define { create_table(:t) { |t| begin; t.primary_key; rescue ArgumentError; p :a; end } }`, ":a\n"},
		// Base model arity + not-found + accessor arms.
		{arSchemaSetup + `begin; Class.new(ActiveRecord::Base).send(:table_name=); rescue ArgumentError; p :a; end`, ":a\n"},
		{arSchemaSetup + `begin; Class.new(ActiveRecord::Base).find; rescue ArgumentError; p :a; end`, ":a\n"},
		{arSchemaSetup + `ActiveRecord::Schema.define { create_table(:widgets) { |t| t.string :n } }
class Widget < ActiveRecord::Base; end
begin; Widget.find(99); rescue ActiveRecord::RecordNotFound; p :nf; end`, ":nf\n"},
		{arSchemaSetup + `ActiveRecord::Schema.define { create_table(:gadgets) { |t| t.string :n } }
class Gadget < ActiveRecord::Base; end
p Gadget.first`, "nil\n"},
		{arSchemaSetup + `ActiveRecord::Schema.define { create_table(:gizmos) { |t| t.string :n } }
class Gizmo < ActiveRecord::Base; end
Gizmo.create!(n: "x")
begin; Gizmo.first.nope; rescue NoMethodError; p :nm; end`, ":nm\n"},
		// count / first / find against a missing table raise StatementInvalid.
		{arSchemaSetup + `g = Class.new(ActiveRecord::Base) { self.table_name = "no_such" }
begin; g.count; rescue ActiveRecord::StatementInvalid; p :se; end`, ":se\n"},
		{arSchemaSetup + `g = Class.new(ActiveRecord::Base) { self.table_name = "no_such" }
begin; g.first; rescue ActiveRecord::StatementInvalid; p :se; end`, ":se\n"},
		{arSchemaSetup + `g = Class.new(ActiveRecord::Base) { self.table_name = "no_such" }
begin; g.find(1); rescue ActiveRecord::StatementInvalid; p :se; end`, ":se\n"},
		// method_missing / respond_to_missing? defensive arms (via send, since the
		// dispatcher always supplies the name): no name, and a bare setter.
		{arSchemaSetup + `ActiveRecord::Schema.define { create_table(:rows) { |t| t.string :n } }
class Row < ActiveRecord::Base; end
Row.create!(n: "x")
r = Row.first
begin; r.send(:method_missing); rescue ArgumentError; p :a; end
begin; r.send(:method_missing, :n=); rescue ArgumentError; p :a; end
p r.send(:respond_to_missing?)`, ":a\n:a\nfalse\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}
