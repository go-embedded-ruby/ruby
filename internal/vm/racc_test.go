// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// calcParserSrc is a real racc-generated-style parser for the tiny calculator
// grammar
//
//	target: exp
//	exp: exp '+' exp | exp '*' exp | '(' exp ')' | NUMBER
//
// The Racc_arg table is the exact one `racc` emits for that grammar (transcribed
// from the go-ruby-racc calc fixture), and the class drives the engine exactly as
// a generated parser does: `include Racc::Parser`, the parse tables in Racc_arg,
// the reduce actions as _reduce_N, and a next_token lexer. It exercises all three
// binding seams (next_token / _reduce_N / on_error) end to end. NAME parametrises
// the class name so a test can install its own on_error override or omit a table.
const calcParserSrc = `
require "racc/parser"
class NAME
  include Racc::Parser
  Racc_arg = [
    [6,7,3,12,4,3,3,4,4,3,5,4,6,7,6,7,6,7,9],
    [8,8,0,8,0,3,6,3,6,7,1,7,2,2,10,10,11,11,5],
    [-6,-6,-1,-6,-5,-6,-6,-6,-6,13,-2,-3,-4],
    [-2,10,10,1,nil,18,2,5,-2,nil,12,14,nil],
    [2,1,nil,8,nil,nil,10,11],
    [2,1,nil,2,nil,nil,2,2],
    [nil,nil,nil],
    [nil,1,0],
    7,
    [0,0,:racc_error, 1,8,:_reduce_1, 3,9,:_reduce_2, 3,9,:_reduce_3, 3,9,:_reduce_4, 1,9,:_reduce_5],
    {false=>0, :error=>1, "+"=>2, "*"=>3, "("=>4, ")"=>5, :NUMBER=>6},
    13, 6, true
  ]
  Racc_token_to_s_table = ["$end","error","\"+\"","\"*\"","\"(\"","\")\"","NUMBER"]
  def initialize(toks = []); @toks = toks; end
  def next_token; @toks.shift || [false,false]; end
  def _reduce_1(val,_v,r); val[0]; end
  def _reduce_2(val,_v,r); val[0] + val[2]; end
  def _reduce_3(val,_v,r); val[0] * val[2]; end
  def _reduce_4(val,_v,r); val[1]; end
  def _reduce_5(val,_v,r); val[0]; end
  def _reduce_none(val,_v,r); r; end
  EXTRA
end
`

// calc builds a runnable program from calcParserSrc with the given class name,
// optional extra method definitions, and a trailing driver line.
func calc(name, extra, driver string) string {
	s := strings.ReplaceAll(calcParserSrc, "NAME", name)
	s = strings.ReplaceAll(s, "EXTRA", extra)
	return s + "\n" + driver + "\n"
}

// TestRaccFeature covers the require probe and the module/error tree shape.
func TestRaccFeature(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p require "racc/parser"`, "true\n"},
		{`require "racc/parser"; p require "racc/parser"`, "false\n"},
		{`p require "racc"`, "true\n"},
		{`require "racc/parser"; p Racc.is_a?(Module)`, "true\n"},
		{`require "racc/parser"; p Racc::Parser.is_a?(Module)`, "true\n"},
		{`require "racc/parser"; p Racc::ParseError < StandardError`, "true\n"},
		{`require "racc/parser"; p Racc::ParseError.ancestors.include?(StandardError)`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestRaccDoParse drives a full shift/reduce/accept parse through do_parse for
// each calc case, mirroring the go-ruby-racc engine oracle (1+2*3 == 7 by racc's
// default right-reduce, (1+2)*3 == 9, a bare NUMBER, and a nested paren).
func TestRaccDoParse(t *testing.T) {
	cases := []struct{ toks, want string }{
		{`[[:NUMBER,1],["+","+"],[:NUMBER,2],["*","*"],[:NUMBER,3]]`, "7\n"},
		{`[["(","("],[:NUMBER,1],["+","+"],[:NUMBER,2],[")",")"],["*","*"],[:NUMBER,3]]`, "9\n"},
		{`[[:NUMBER,42]]`, "42\n"},
		{`[["(","("],[:NUMBER,5],[")",")"]]`, "5\n"},
	}
	for _, c := range cases {
		src := calc("Calc", "", `p Calc.new(`+c.toks+`).do_parse`)
		if got := eval(t, src); got != c.want {
			t.Errorf("toks=%s got=%q want=%q", c.toks, got, c.want)
		}
	}
}

// TestRaccYyparse drives the same grammar through yyparse(recv, mid): the tokens
// come from a separate lexer object whose method yields each [tok, val] (plus a
// final EOF), exercising the iterator seam instead of next_token.
func TestRaccYyparse(t *testing.T) {
	lexer := `
class Lex
  def initialize(toks); @toks = toks; end
  def scan
    @toks.each { |t| yield t }
    yield [false, false]
  end
end
`
	src := calc("Calc", "", lexer+
		`p Calc.new.yyparse(Lex.new([[:NUMBER,1],["+","+"],[:NUMBER,2],["*","*"],[:NUMBER,3]]), :scan)`)
	if got := eval(t, src); got != "7\n" {
		t.Errorf("yyparse got=%q want %q", got, "7\n")
	}
}

// TestRaccParseError covers the default on_error: a syntax error raises
// Racc::ParseError with MRI's "parse error on value <val> (<token>)" message,
// the token display coming from token_to_str / Racc_token_to_s_table.
func TestRaccParseError(t *testing.T) {
	src := calc("Calc", "",
		`p(begin; Calc.new([[:NUMBER,1],["+","+"],["+","+"],[:NUMBER,2]]).do_parse; rescue Racc::ParseError => e; e.message; end)`)
	want := "\"parse error on value \\\"+\\\" (\\\"+\\\")\"\n"
	if got := eval(t, src); got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}

// TestRaccOnErrorRecovery covers the on_error seam returning normally: the engine
// enters error-recovery mode, and with no error production the calc parser unwinds
// to a nil result rather than raising.
func TestRaccOnErrorRecovery(t *testing.T) {
	src := calc("CalcR", `def on_error(t,v,s); nil; end`,
		`p CalcR.new([[:NUMBER,1],["+","+"],["+","+"],[:NUMBER,2]]).do_parse`)
	if got := eval(t, src); got != "nil\n" {
		t.Errorf("recovery got=%q want %q", got, "nil\n")
	}
}

// TestRaccTokenToStr covers token_to_str directly: a present table entry, an
// out-of-range id, and the no-argument form all mirror MRI (element or nil).
func TestRaccTokenToStr(t *testing.T) {
	cases := []struct{ call, want string }{
		{`token_to_str(6)`, "\"NUMBER\"\n"},
		{`token_to_str(0)`, "\"$end\"\n"},
		{`token_to_str(99)`, "nil\n"},
		{`token_to_str`, "nil\n"},
	}
	for _, c := range cases {
		src := calc("Calc", "", `p Calc.new.`+c.call)
		if got := eval(t, src); got != c.want {
			t.Errorf("%s got=%q want=%q", c.call, got, c.want)
		}
	}
}

// TestRaccOnErrorDirect covers on_error's argument-defaulting arms by calling it
// directly with fewer than three arguments.
func TestRaccOnErrorDirect(t *testing.T) {
	src := calc("Calc", "",
		`p(begin; Calc.new.on_error; rescue Racc::ParseError => e; e.message; end)`)
	// tok defaults to 0 ($end), val to nil.
	want := "\"parse error on value nil ($end)\"\n"
	if got := eval(t, src); got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}

// TestRaccNoTokenTable covers token_to_str's absent-table arm: a parser without a
// Racc_token_to_s_table constant renders the error token as '?'.
func TestRaccNoTokenTable(t *testing.T) {
	// A minimal valid Racc_arg (calc tables) but no Racc_token_to_s_table.
	src := `
require "racc/parser"
class Bare
  include Racc::Parser
  Racc_arg = [
    [6,7,3,12,4,3,3,4,4,3,5,4,6,7,6,7,6,7,9],
    [8,8,0,8,0,3,6,3,6,7,1,7,2,2,10,10,11,11,5],
    [-6,-6,-1,-6,-5,-6,-6,-6,-6,13,-2,-3,-4],
    [-2,10,10,1,nil,18,2,5,-2,nil,12,14,nil],
    [2,1,nil,8,nil,nil,10,11],
    [2,1,nil,2,nil,nil,2,2],
    [nil,nil,nil],
    [nil,1,0],
    7,
    [0,0,:racc_error, 1,8,:_reduce_1, 3,9,:_reduce_2, 3,9,:_reduce_3, 3,9,:_reduce_4, 1,9,:_reduce_5],
    {false=>0, :error=>1, "+"=>2, "*"=>3, "("=>4, ")"=>5, :NUMBER=>6},
    13, 6, true
  ]
  def initialize(toks); @toks = toks; end
  def next_token; @toks.shift || [false,false]; end
  def _reduce_1(val,_v,r); val[0]; end
  def _reduce_2(val,_v,r); val[0] + val[2]; end
  def _reduce_3(val,_v,r); val[0] * val[2]; end
  def _reduce_4(val,_v,r); val[1]; end
  def _reduce_5(val,_v,r); val[0]; end
end
p(begin; Bare.new([[:NUMBER,1],["+","+"],["+","+"]]).do_parse; rescue Racc::ParseError => e; e.message; end)
`
	got := eval(t, src)
	if !strings.Contains(got, "(?)") {
		t.Errorf("expected '?' token in message, got %q", got)
	}
}

// TestRaccErrors covers the error arms of the do_parse entry: a parser class with
// no Racc_arg constant, a malformed Racc_arg, and a parser that never defines
// next_token (the default raises NotImplementedError).
func TestRaccErrors(t *testing.T) {
	// The next_token-undefined case needs a class that keeps the tables but drops
	// the lexer; build it explicitly.
	noTok := `
require "racc/parser"
class NoTok
  include Racc::Parser
  Racc_arg = [
    [6,7,3,12,4,3,3,4,4,3,5,4,6,7,6,7,6,7,9],
    [8,8,0,8,0,3,6,3,6,7,1,7,2,2,10,10,11,11,5],
    [-6,-6,-1,-6,-5,-6,-6,-6,-6,13,-2,-3,-4],
    [-2,10,10,1,nil,18,2,5,-2,nil,12,14,nil],
    [2,1,nil,8,nil,nil,10,11],
    [2,1,nil,2,nil,nil,2,2],
    [nil,nil,nil],
    [nil,1,0],
    7,
    [0,0,:racc_error, 1,8,:_reduce_1, 3,9,:_reduce_2, 3,9,:_reduce_3, 3,9,:_reduce_4, 1,9,:_reduce_5],
    {false=>0, :error=>1, "+"=>2, "*"=>3, "("=>4, ")"=>5, :NUMBER=>6},
    13, 6, true
  ]
end
p(begin; NoTok.new.do_parse; rescue NotImplementedError => e; "NotImplementedError"; end)`
	cases := []struct{ src, want string }{
		{`require "racc/parser"
class NoArg; include Racc::Parser; end
p(begin; NoArg.new.do_parse; rescue NameError => e; "NameError"; end)`, "\"NameError\"\n"},
		{`require "racc/parser"
class Bad; include Racc::Parser; Racc_arg = [1,2,3]; end
p(begin; Bad.new.do_parse; rescue ArgumentError => e; "ArgumentError"; end)`, "\"ArgumentError\"\n"},
		{noTok, "\"NotImplementedError\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}
