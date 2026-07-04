// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestGraphQLCore drives the whole happy-path surface end to end through rbgo:
// scalar / list / non-null field types, field arguments, query variables, the
// request context and root value, a nested object type, and a mutation root —
// each read back out of the returned data Hash.
func TestGraphQLCore(t *testing.T) {
	src := `
require "graphql"

Child = GraphQL::ObjectType.define(name: "Child") do |t|
  t.field("n", GraphQL::Types::Int) { |o, a, c| o["n"] }
end

Query = GraphQL::ObjectType.define(name: "Query", description: "root") do |t|
  t.field("hello", GraphQL::Types::String, description: "greeting") { |o, a, c| "world" }
  t.field("sum", GraphQL::Types::Int, args: { "a" => GraphQL::Types::Int, "b" => GraphQL::Types::Int }) { |o, a, c| a["a"] + a["b"] }
  t.field("flag", GraphQL::Types::Boolean) { |o, a, c| true }
  t.field("pi", GraphQL::Types::Float) { |o, a, c| 3.5 }
  t.field("tags", GraphQL.list_of(GraphQL::Types::String)) { |o, a, c| ["x", "y"] }
  t.field("nn", GraphQL.non_null(GraphQL::Types::ID)) { |o, a, c| "id1" }
  t.field("who", GraphQL::Types::String) { |o, a, c| c["user"] }
  t.field("root", GraphQL::Types::String) { |o, a, c| o["seed"] }
  t.field("obj", Child) { |o, a, c| { "n" => 7 } }
end

Mutation = GraphQL::ObjectType.define(name: "Mutation") do |t|
  t.field("noop", GraphQL::Types::String) { |o, a, c| "ok" }
end

Schema = GraphQL::Schema.define(query: Query, mutation: Mutation)

out = []
out << Schema.execute("{ hello }")["data"]["hello"]
out << Schema.execute('query($x:Int!,$y:Int!){ sum(a:$x, b:$y) }', variables: { "x" => 2, "y" => 3 })["data"]["sum"]
out << Schema.execute("{ flag }")["data"]["flag"]
out << Schema.execute("{ pi }")["data"]["pi"]
out << Schema.execute("{ tags }")["data"]["tags"].join(",")
out << Schema.execute("{ nn }")["data"]["nn"]
out << Schema.execute("{ who }", context: { "user" => "ada" })["data"]["who"]
out << Schema.execute("{ root }", root_value: { "seed" => "S" })["data"]["root"]
out << Schema.execute("{ obj { n } }")["data"]["obj"]["n"]
out << Schema.execute("mutation { noop }")["data"]["noop"]
puts out.join("|")
`
	want := "world|5|true|3.5|x,y|id1|ada|S|7|ok"
	if got := runSrc(t, src); got != want {
		t.Fatalf("graphql core = %q want %q", got, want)
	}
}

// TestGraphQLErrors drives the error surface: a resolver returning a
// GraphQL::ExecutionError and a resolver raising a Ruby exception both collect a
// field error (with a null field and a path) while the "data" key stays present,
// a static validation failure yields only "errors" with no "data" key, and the
// VM keeps working after a resolver has raised.
func TestGraphQLErrors(t *testing.T) {
	src := `
require "graphql"
Q = GraphQL::ObjectType.define(name: "Query") do |t|
  t.field("boom", GraphQL::Types::String) { |o, a, c| GraphQL::ExecutionError.new("kaboom") }
  t.field("bang", GraphQL::Types::String) { |o, a, c| raise "oops" }
  t.field("ok", GraphQL::Types::String) { |o, a, c| "fine" }
end
S = GraphQL::Schema.define(query: Q)

out = []
r = S.execute("{ boom }")
out << r["data"]["boom"].inspect
out << r["errors"][0]["message"]
out << r["errors"][0]["path"].join("/")
out << r["errors"][0]["locations"][0]["line"].to_s
out << S.execute("{ bang }")["errors"][0]["message"]
r = S.execute("{ nope }")
out << r.key?("data").to_s
out << r["errors"][0]["message"].include?("nope").to_s
out << S.execute("{ ok }")["data"]["ok"]
out << GraphQL::ExecutionError.new.is_a?(GraphQL::Error).to_s
puts out.join("|")
`
	want := "nil|kaboom|boom|1|oops|false|true|fine|true"
	if got := runSrc(t, src); got != want {
		t.Fatalf("graphql errors = %q want %q", got, want)
	}
}

// TestGraphQLRaises exercises every raising path on the Go side of the binding —
// the field DSL's arity/block/argument/type checks, the object-type and schema
// builders' missing-keyword and validation failures, the wrapper helpers' type
// checks, and execute's arity — by rescuing each and recording its exception
// class.
func TestGraphQLRaises(t *testing.T) {
	src := `
require "graphql"
def cls
  begin
    yield
    "no-raise"
  rescue => e
    e.class.to_s
  end
end
S = GraphQL::Types::String
out = []
out << cls { GraphQL::ObjectType.define(name: "A") { |t| t.field("x") } }
out << cls { GraphQL::ObjectType.define(name: "A") { |t| t.field("x", S) } }
out << cls { GraphQL::ObjectType.define(name: "A") { |t| t.field("x", S, args: "nope") { |o, a, c| 1 } } }
out << cls { GraphQL::ObjectType.define(name: "A") { |t| t.field("x", 5) { |o, a, c| 1 } } }
out << cls { GraphQL::ObjectType.define(name: "A") { |t| t.field(5, S) { |o, a, c| 1 } } }
out << cls { GraphQL::ObjectType.define { |t| } }
out << cls { GraphQL::ObjectType.define(name: "A") }
out << cls { GraphQL::Schema.define }
out << cls { GraphQL::Schema.define(query: S) }
out << cls { q = GraphQL::ObjectType.define(name: "Q") { |t| }; GraphQL::Schema.define(query: q) }
out << cls { GraphQL.list_of(5) }
out << cls { GraphQL.non_null(5) }
q2 = GraphQL::ObjectType.define(name: "Q2") { |t| t.field("a", S) { |o, a, c| "z" } }
out << cls { GraphQL::Schema.define(query: q2).execute }
out << cls { GraphQL::ObjectType.define(name: "Bad", description: "d") { |t| t.field("a", S) { |o, a, c| 1 } }; GraphQL::Schema.define(query: GraphQL::Types::String, mutation: q2) }
puts out.join(",")
`
	want := "ArgumentError,ArgumentError,TypeError,TypeError,TypeError,ArgumentError,ArgumentError,GraphQL::Error,GraphQL::Error,GraphQL::Error,TypeError,TypeError,ArgumentError,GraphQL::Error"
	if got := runSrc(t, src); got != want {
		t.Fatalf("graphql raises = %q want %q", got, want)
	}
}

// TestGraphQLNameSymbol proves a field / type name may be given as a Symbol as
// well as a String, matching graphql-ruby.
func TestGraphQLNameSymbol(t *testing.T) {
	src := `
require "graphql"
Q = GraphQL::ObjectType.define(name: :Query) do |t|
  t.field(:hi, GraphQL::Types::String) { |o, a, c| "yo" }
end
S = GraphQL::Schema.define(query: Q)
r = S.execute("query A { hi } query B { hi }", operation_name: "A")
puts [S.execute("{ hi }")["data"]["hi"], r["data"]["hi"]].join("|")
`
	if got := runSrc(t, src); got != "yo|yo" {
		t.Fatalf("graphql symbol name = %q want %q", got, "yo|yo")
	}
}
