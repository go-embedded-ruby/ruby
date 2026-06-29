// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"testing"
)

// TestGetoptLongConstants covers the GetoptLong argument-kind and ordering
// constants, its error tree, and #new (require "getoptlong"). The constant values
// match MRI Ruby 4.0.5.
func TestGetoptLongConstants(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "getoptlong"; p GetoptLong::NO_ARGUMENT`, "0\n"},
		{`require "getoptlong"; p GetoptLong::REQUIRED_ARGUMENT`, "1\n"},
		{`require "getoptlong"; p GetoptLong::OPTIONAL_ARGUMENT`, "2\n"},
		{`require "getoptlong"; p GetoptLong::REQUIRE_ORDER`, "0\n"},
		{`require "getoptlong"; p GetoptLong::PERMUTE`, "1\n"},
		{`require "getoptlong"; p GetoptLong::RETURN_IN_ORDER`, "2\n"},
		// Error tree: Error < StandardError; the four concrete kinds < Error.
		{`require "getoptlong"; p GetoptLong::Error < StandardError`, "true\n"},
		{`require "getoptlong"; p GetoptLong::InvalidOption < GetoptLong::Error`, "true\n"},
		{`require "getoptlong"; p GetoptLong::MissingArgument < GetoptLong::Error`, "true\n"},
		{`require "getoptlong"; p GetoptLong::NeedlessArgument < GetoptLong::Error`, "true\n"},
		{`require "getoptlong"; p GetoptLong::AmbiguousOption < GetoptLong::Error`, "true\n"},
		// #new builds a GetoptLong instance.
		{`require "getoptlong"; p GetoptLong.new.is_a?(GetoptLong)`, "true\n"},
		// A GetoptLong is truthy (never nil/false) in a boolean context.
		{`require "getoptlong"; p(GetoptLong.new ? "y" : "n")`, "\"y\"\n"},
		{`require "getoptlong"; p GetoptLong.new.class`, "GetoptLong\n"},
		{`require "getoptlong"; p GetoptLong.new.inspect`, "\"#<GetoptLong>\"\n"},
		{`require "getoptlong"; puts GetoptLong.new.to_s`, "#<GetoptLong>\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

const golReq = `require "getoptlong"; `

// TestGetoptLongScan covers the scanning surface against MRI 4.0.5: each
// argument-kind, every option form (=joined / space-separated / short / clustered
// short / abbreviated long / -- terminator), #each vs #get, the optional-argument
// lookahead, the ordering modes, and ARGV mutation (the leftover operands).
func TestGetoptLongScan(t *testing.T) {
	cases := []struct{ src, want string }{
		// REQUIRED_ARGUMENT, space-separated + short flag; ARGV left with operands.
		{golReq + `
ARGV.replace(["--size","10","-v","file"])
g = GetoptLong.new(["--size",GetoptLong::REQUIRED_ARGUMENT],["--verbose","-v",GetoptLong::NO_ARGUMENT])
r=[]; g.each{|o,a| r<<[o,a]}; p r; p ARGV`,
			"[[\"--size\", \"10\"], [\"--verbose\", \"\"]]\n[\"file\"]\n"},
		// --foo=bar (=joined long argument).
		{golReq + `
ARGV.replace(["--size=10","x"])
g = GetoptLong.new(["--size",GetoptLong::REQUIRED_ARGUMENT])
r=[]; g.each{|o,a| r<<[o,a]}; p r; p ARGV`,
			"[[\"--size\", \"10\"]]\n[\"x\"]\n"},
		// -f bar (short, separate argument).
		{golReq + `
ARGV.replace(["-f","bar"])
g = GetoptLong.new(["-f",GetoptLong::REQUIRED_ARGUMENT])
r=[]; g.each{|o,a| r<<[o,a]}; p r; p ARGV`,
			"[[\"-f\", \"bar\"]]\n[]\n"},
		// -fbar (short, glued argument).
		{golReq + `
ARGV.replace(["-fbar"])
g = GetoptLong.new(["-f",GetoptLong::REQUIRED_ARGUMENT])
r=[]; g.each{|o,a| r<<[o,a]}; p r`,
			"[[\"-f\", \"bar\"]]\n"},
		// Clustered short flags -ab.
		{golReq + `
ARGV.replace(["-ab"])
g = GetoptLong.new(["-a",GetoptLong::NO_ARGUMENT],["-b",GetoptLong::NO_ARGUMENT])
r=[]; g.each{|o,a| r<<[o,a]}; p r`,
			"[[\"-a\", \"\"], [\"-b\", \"\"]]\n"},
		// Abbreviated long option --siz -> --size.
		{golReq + `
ARGV.replace(["--siz","5"])
g = GetoptLong.new(["--size",GetoptLong::REQUIRED_ARGUMENT])
r=[]; g.each{|o,a| r<<[o,a]}; p r`,
			"[[\"--size\", \"5\"]]\n"},
		// -- terminator: stops scanning, leaving the rest as operands.
		{golReq + `
ARGV.replace(["-a","--","-b","x"])
g = GetoptLong.new(["-a",GetoptLong::NO_ARGUMENT],["-b",GetoptLong::NO_ARGUMENT])
r=[]; g.each{|o,a| r<<[o,a]}; p r; p ARGV`,
			"[[\"-a\", \"\"]]\n[\"-b\", \"x\"]\n"},
		// OPTIONAL_ARGUMENT consumes the next non-option word (MRI lookahead).
		{golReq + `
ARGV.replace(["--opt","next"])
g = GetoptLong.new(["--opt",GetoptLong::OPTIONAL_ARGUMENT])
r=[]; g.each{|o,a| r<<[o,a]}; p r; p ARGV`,
			"[[\"--opt\", \"next\"]]\n[]\n"},
		// OPTIONAL_ARGUMENT =joined value, leaving the following word as an operand.
		{golReq + `
ARGV.replace(["--opt=x","next"])
g = GetoptLong.new(["--opt",GetoptLong::OPTIONAL_ARGUMENT])
r=[]; g.each{|o,a| r<<[o,a]}; p r; p ARGV`,
			"[[\"--opt\", \"x\"]]\n[\"next\"]\n"},
		// #each_option is the alias of #each.
		{golReq + `
ARGV.replace(["-a"])
g = GetoptLong.new(["-a",GetoptLong::NO_ARGUMENT])
r=[]; g.each_option{|o,a| r<<[o,a]}; p r`,
			"[[\"-a\", \"\"]]\n"},
		// #each with no block scans to the end and returns self (ARGV consumed).
		{golReq + `
ARGV.replace(["-a","y"])
g = GetoptLong.new(["-a",GetoptLong::NO_ARGUMENT])
p g.each.equal?(g); p ARGV`,
			"true\n[\"y\"]\n"},
		// #get / #get_option return [name, arg] then [nil, nil] at end; terminated?.
		{golReq + `
ARGV.replace(["-a","x"])
g = GetoptLong.new(["-a",GetoptLong::NO_ARGUMENT])
p g.terminated?
loop { o,a = g.get; break if o.nil?; p [o,a] }
p g.terminated?
p ARGV`,
			"false\n[\"-a\", \"\"]\ntrue\n[\"x\"]\n"},
		// #get_option is the alias of #get.
		{golReq + `
ARGV.replace(["-a"])
g = GetoptLong.new(["-a",GetoptLong::NO_ARGUMENT])
p g.get_option`,
			"[\"-a\", \"\"]\n"},
		// set_options after construction.
		{golReq + `
ARGV.replace(["-a"])
g = GetoptLong.new
g.set_options(["-a",GetoptLong::NO_ARGUMENT])
r=[]; g.each{|o,a| r<<[o,a]}; p r`,
			"[[\"-a\", \"\"]]\n"},
		// ordering = REQUIRE_ORDER stops at the first non-option (nothing scanned).
		{golReq + `
ARGV.replace(["file","-a"])
g = GetoptLong.new(["-a",GetoptLong::NO_ARGUMENT])
g.ordering = GetoptLong::REQUIRE_ORDER
p g.ordering
r=[]; g.each{|o,a| r<<[o,a]}; p r; p ARGV`,
			"0\n[]\n[\"file\", \"-a\"]\n"},
		// ordering = RETURN_IN_ORDER reports every word.
		{golReq + `
ARGV.replace(["file","-a"])
g = GetoptLong.new(["-a",GetoptLong::NO_ARGUMENT])
g.ordering = GetoptLong::RETURN_IN_ORDER
r=[]; g.each{|o,a| r<<[o,a]}; p r`,
			"[[\"\", \"file\"], [\"-a\", \"\"]]\n"},
		// quiet defaults false; quiet= sets it; quiet? alias reads it.
		{golReq + `
g = GetoptLong.new
p g.quiet
g.quiet = true
p [g.quiet, g.quiet?]`,
			"false\n[true, true]\n"},
		// No error before a problem: error / error? / error_message all empty.
		{golReq + `
g = GetoptLong.new
p [g.error, g.error?, g.error_message]`,
			"[nil, false, nil]\n"},
		// An option may be re-seeded: ARGV replaced after new, before the first get.
		{golReq + `
g = GetoptLong.new(["-a",GetoptLong::NO_ARGUMENT])
ARGV.replace(["-a","z"])
r=[]; g.each{|o,a| r<<[o,a]}; p r; p ARGV`,
			"[[\"-a\", \"\"]]\n[\"z\"]\n"},
		// A non-Array ARGV (reassigned) is treated as empty: nothing to scan.
		{golReq + `
ARGV = "notarray"
g = GetoptLong.new(["-a",GetoptLong::NO_ARGUMENT])
p g.get`,
			"[nil, nil]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestGetoptLongErrors covers the four error kinds raised through the matching
// nested GetoptLong::* class, their MRI-identical messages, quiet mode (still
// raises, records error / error_message), and the malformed-spec ArgumentError.
func TestGetoptLongErrors(t *testing.T) {
	cases := []struct{ src, class, msg string }{
		// InvalidOption: an unrecognised option.
		{golReq + `
ARGV.replace(["--bogus"])
GetoptLong.new(["--size",GetoptLong::REQUIRED_ARGUMENT]).get`,
			"GetoptLong::InvalidOption", "unrecognized option `--bogus'"},
		// MissingArgument: a required argument is absent.
		{golReq + `
ARGV.replace(["--size"])
GetoptLong.new(["--size",GetoptLong::REQUIRED_ARGUMENT]).each{|o,a|}`,
			"GetoptLong::MissingArgument", "option `--size' requires an argument"},
		// NeedlessArgument: a flag given an =argument.
		{golReq + `
ARGV.replace(["--flag=x"])
GetoptLong.new(["--flag",GetoptLong::NO_ARGUMENT]).get`,
			"GetoptLong::NeedlessArgument", "option `--flag' doesn't allow an argument"},
		// AmbiguousOption: an abbreviation matching more than one option.
		{golReq + `
ARGV.replace(["--ver"])
GetoptLong.new(["--verbose",GetoptLong::NO_ARGUMENT],["--version",GetoptLong::NO_ARGUMENT]).get`,
			"GetoptLong::AmbiguousOption", "option `--ver' is ambiguous between --verbose, --version"},
		// A non-array spec is an ArgumentError (MRI's GetoptLong.new surface).
		{golReq + `GetoptLong.new("oops")`,
			"ArgumentError", "the option list contains non-array argument"},
		// A spec element that is neither a name string nor an arg-kind is rejected.
		{golReq + `GetoptLong.new([:bad])`,
			"ArgumentError", "the option list contains an invalid element"},
		// A name without a leading dash is an invalid option spec (library SpecError),
		// surfaced as ArgumentError with MRI's message.
		{golReq + `GetoptLong.new(["badname",GetoptLong::NO_ARGUMENT])`,
			"ArgumentError", "an invalid option `badname'"},
		// set_options validates the same way, post-construction.
		{golReq + `GetoptLong.new.set_options(["badname",GetoptLong::NO_ARGUMENT])`,
			"ArgumentError", "an invalid option `badname'"},
		// An out-of-range ordering is rejected (ArgumentError).
		{golReq + `GetoptLong.new.ordering = 99`,
			"ArgumentError", ""},
	}
	for _, c := range cases {
		class, msg := evalErr(t, c.src)
		if class != c.class {
			t.Errorf("src=%q got class=%q want=%q", c.src, class, c.class)
		}
		if c.msg != "" && msg != c.msg {
			t.Errorf("src=%q got msg=%q want=%q", c.src, msg, c.msg)
		}
	}
}

// TestGetoptLongQuietAndErrorState covers quiet-mode scanning (the exception is
// still raised, and #error / #error_message report it after a rescue), exercising
// the error-reporting accessors on a parser that hit a problem.
func TestGetoptLongQuietAndErrorState(t *testing.T) {
	cases := []struct{ src, want string }{
		// Quiet parser: the error is still raised and rescuable; error_message /
		// error report it.
		{golReq + `
ARGV.replace(["--bogus"])
g = GetoptLong.new(["--size",GetoptLong::REQUIRED_ARGUMENT])
g.quiet = true
begin
  g.get
rescue GetoptLong::InvalidOption => e
  p [e.message, g.error.to_s, g.error_message, g.error?, g.terminated?]
end`,
			"[\"unrecognized option `--bogus'\", \"GetoptLong::InvalidOption\", \"unrecognized option `--bogus'\", true, false]\n"},
		// error reports the right class for each kind.
		{golReq + `
ARGV.replace(["--size"])
g = GetoptLong.new(["--size",GetoptLong::REQUIRED_ARGUMENT])
g.quiet = true
g.get rescue nil
p g.error`,
			"GetoptLong::MissingArgument\n"},
		{golReq + `
ARGV.replace(["--flag=x"])
g = GetoptLong.new(["--flag",GetoptLong::NO_ARGUMENT])
g.quiet = true
g.get rescue nil
p g.error`,
			"GetoptLong::NeedlessArgument\n"},
		{golReq + `
ARGV.replace(["--ver"])
g = GetoptLong.new(["--verbose",GetoptLong::NO_ARGUMENT],["--version",GetoptLong::NO_ARGUMENT])
g.quiet = true
g.get rescue nil
p g.error`,
			"GetoptLong::AmbiguousOption\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}
