// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import "testing"

// TestThorFeature covers the require "thor" feature probe and the module/class +
// error tree shape.
func TestThorFeature(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p require "thor"`, "true\n"},
		{`require "thor"; p require "thor"`, "false\n"},
		{`require "thor"; p Thor.is_a?(Class)`, "true\n"},
		{`require "thor"; p Thor::Option.is_a?(Class)`, "true\n"},
		{`require "thor"; p Thor::Options.is_a?(Class)`, "true\n"},
		{`require "thor"; p Thor::Command.is_a?(Class)`, "true\n"},
		{`require "thor"; p Thor::Base.is_a?(Class)`, "true\n"},
		{`require "thor"; p Thor::Error < StandardError`, "true\n"},
		{`require "thor"; p Thor::InvocationError < Thor::Error`, "true\n"},
		{`require "thor"; p Thor::RequiredArgumentMissingError < Thor::InvocationError`, "true\n"},
		{`require "thor"; p Thor::MalformattedArgumentError < Thor::InvocationError`, "true\n"},
		{`require "thor"; p Thor::UnknownArgumentError < Thor::Error`, "true\n"},
		{`require "thor"; p Thor::UndefinedCommandError < Thor::Error`, "true\n"},
		{`require "thor"; p Thor::AmbiguousCommandError < Thor::Error`, "true\n"},
		{`require "thor"; p Thor::ExclusiveArgumentError < Thor::InvocationError`, "true\n"},
		{`require "thor"; p Thor::AtLeastOneRequiredArgumentError < Thor::InvocationError`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestThorOption covers Thor::Option.new and its readers.
func TestThorOption(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "thor"; o = Thor::Option.new("force", type: :boolean, aliases: ["-f"], desc: "force it"); p o.class`, "Thor::Option\n"},
		{`require "thor"; o = Thor::Option.new("force", type: :boolean); p o.name`, "\"force\"\n"},
		{`require "thor"; o = Thor::Option.new("force", type: :boolean); p o.human_name`, "\"force\"\n"},
		{`require "thor"; o = Thor::Option.new("force", type: :boolean); p o.switch_name`, "\"--force\"\n"},
		{`require "thor"; o = Thor::Option.new("force", type: :boolean); p o.boolean?`, "true\n"},
		{`require "thor"; o = Thor::Option.new("name"); p o.string?`, "true\n"},
		{`require "thor"; o = Thor::Option.new("force", type: :boolean, aliases: ["-f"], desc: "force it"); p o.usage(0)`, "\"-f, [--force]\"\n"},
		{`require "thor"; o = Thor::Option.new("force", type: :boolean, aliases: "-f"); p o.aliases`, "[\"-f\"]\n"},
		{`require "thor"; o = Thor::Option.new("force", type: :boolean, desc: "d"); p o.description`, "\"d\"\n"},
		{`require "thor"; o = Thor::Option.new("name", required: true); p o.required?`, "true\n"},
		{`require "thor"; o = Thor::Option.new("count", type: :numeric, default: 3); p o.default`, "3\n"},
		{`require "thor"; o = Thor::Option.new("mode", type: :string, enum: ["a", "b"]); p o.enum_to_s`, "\"a, b\"\n"},
		{`require "thor"; o = Thor::Option.new("name", type: :string, default: "x", desc: "d"); p o.print_default`, "\"x\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestThorOptionErrors covers Thor::Option.new argument guards and the
// boolean+required rejection Thor performs.
func TestThorOptionErrors(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "thor"
begin
  Thor::Option.new
rescue ArgumentError => e
  puts e.message
end`, "wrong number of arguments (given 0, expected 1..2)\n"},
		{`require "thor"
begin
  Thor::Option.new("force", type: :boolean, required: true)
rescue ArgumentError => e
  puts "rejected"
end`, "rejected\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestThorOptionsParse covers Thor::Options#parse: the parsed-options Hash, the
// value-model mapping (bool / numeric / string / array / hash), and #remaining.
func TestThorOptionsParse(t *testing.T) {
	setup := `require "thor"
opts = Thor::Options.new([
  Thor::Option.new("force", type: :boolean, aliases: ["-f"]),
  Thor::Option.new("count", type: :numeric),
  Thor::Option.new("name", type: :string),
])
`
	cases := []struct{ src, want string }{
		{setup + `p opts.parse(["--force", "--count", "3", "rest"])`, "{\"force\" => true, \"count\" => 3}\n"},
		{setup + `opts.parse(["--force", "rest", "more"]); p opts.remaining`, "[\"rest\", \"more\"]\n"},
		{setup + `p opts.parse(["--name", "bob"])`, "{\"name\" => \"bob\"}\n"},
		{`require "thor"
o = Thor::Options.new([Thor::Option.new("tags", type: :array)])
p o.parse(["--tags", "a", "b"])`, "{\"tags\" => [\"a\", \"b\"]}\n"},
		{`require "thor"
o = Thor::Options.new([Thor::Option.new("props", type: :hash)])
p o.parse(["--props", "k:v"])`, "{\"props\" => {\"k\" => \"v\"}}\n"},
		{`require "thor"
o = Thor::Options.new
p o.parse(["leftover"])`, "{}\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestThorOptionsParseError covers the mapped Thor parse error.
func TestThorOptionsParseError(t *testing.T) {
	src := `require "thor"
o = Thor::Options.new([Thor::Option.new("count", type: :numeric)])
begin
  o.parse(["--count", "notanum"])
rescue Thor::MalformattedArgumentError => e
  puts e.message
end`
	if got := eval(t, src); got != "Expected numeric value for '--count'; got \"notanum\"\n" {
		t.Errorf("got %q", got)
	}
}

// TestThorBase covers the command registry: add_command, commands, help,
// command_help, dispatch and normalize_command_name.
func TestThorBase(t *testing.T) {
	setup := `require "thor"
base = Thor::Base.new("myapp", basename: "myapp")
cmd = Thor::Command.new("hello", "say hi", "hello NAME", [Thor::Option.new("force", type: :boolean, aliases: ["-f"], desc: "force it")])
base.add_command(cmd)
`
	cases := []struct{ src, want string }{
		{setup + `p cmd.class`, "Thor::Command\n"},
		{setup + `p cmd.name`, "\"hello\"\n"},
		{setup + `p cmd.description`, "\"say hi\"\n"},
		{setup + `p cmd.usage`, "\"hello NAME\"\n"},
		{setup + `p cmd.options.map { |o| o.human_name }`, "[\"force\"]\n"},
		{setup + `p base.commands.map { |c| c.name }`, "[\"hello\"]\n"},
		{setup + `puts base.help`, "Commands:\n  myapp hello NAME  # say hi\n\n"},
		{setup + `puts base.command_help("hello")`, "Usage:\n  myapp hello NAME\n\nOptions:\n  -f, [--force]  # force it\n\nsay hi\n"},
		{setup + `p base.normalize_command_name("hel")`, "\"hello\"\n"},
		{setup + `p base.normalize_command_name(nil)`, "\"help\"\n"},
		{setup + `p base.dispatch(["hello", "--force", "Bob"])`, "[\"hello\", {\"force\" => true}, [\"Bob\"]]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestThorBaseErrors covers the dispatch/help error paths and argument guards.
func TestThorBaseErrors(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "thor"
base = Thor::Base.new("myapp")
begin
  base.command_help("nope")
rescue Thor::UndefinedCommandError => e
  puts "undefined"
end`, "undefined\n"},
		{`require "thor"
base = Thor::Base.new("myapp")
begin
  base.dispatch(["nope"])
rescue Thor::UndefinedCommandError => e
  puts "undefined"
end`, "undefined\n"},
		{`require "thor"
base = Thor::Base.new("myapp")
base.add_command(Thor::Command.new("list", "l", "list", []))
base.add_command(Thor::Command.new("listen", "l2", "listen", []))
begin
  base.normalize_command_name("lis")
rescue Thor::AmbiguousCommandError => e
  puts e.message
end`, "Ambiguous command lis matches [list, listen]\n"},
		{`require "thor"
base = Thor::Base.new("myapp")
begin
  base.add_command("notacommand")
rescue TypeError => e
  puts e.message
end`, "wrong argument type String (expected Thor::Command)\n"},
		{`require "thor"
begin
  Thor::Command.new("x")
rescue ArgumentError => e
  puts e.message
end`, "wrong number of arguments (given 1, expected 3..4)\n"},
		{`require "thor"
begin
  Thor::Options.new(["notanoption"])
rescue TypeError => e
  puts e.message
end`, "wrong element type String (expected Thor::Option)\n"},
		{`require "thor"
begin
  Thor::Options.new("notanarray")
rescue TypeError => e
  puts e.message
end`, "wrong argument type String (expected Array of Thor::Option)\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}
