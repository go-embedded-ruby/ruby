// Package lexer turns source bytes into tokens.
//
// Phase 0 carries the seeds of MRI's stateful lexer — SpaceBefore on every
// token and a lexState field — without yet exercising the hard cases (regex vs
// division, heredocs, interpolation). Those land in later phases (plan §10);
// the state plumbing is here from the start so they slot in without a rewrite.
package lexer

import (
	"github.com/go-embedded-ruby/ruby/internal/token"
)

// lexState mirrors MRI's EXPR_* family. Phase 0 only distinguishes "a value /
// operand may come next" (begin) from "a value just ended" (end); future
// phases add the rest to disambiguate ambiguous characters.
type lexState int

const (
	exprBegin lexState = iota // expecting an operand (start of expression)
	exprEnd                   // just finished an operand
)

type Lexer struct {
	src   []byte
	pos   int
	line  int
	col   int
	state lexState
}

func New(src string) *Lexer {
	return &Lexer{src: []byte(src), line: 1, col: 0, state: exprBegin}
}

func (l *Lexer) peek() byte {
	if l.pos >= len(l.src) {
		return 0
	}
	return l.src[l.pos]
}

func (l *Lexer) peek2() byte {
	if l.pos+1 >= len(l.src) {
		return 0
	}
	return l.src[l.pos+1]
}

func (l *Lexer) advance() byte {
	c := l.src[l.pos]
	l.pos++
	if c == '\n' {
		l.line++
		l.col = 0
	} else {
		l.col++
	}
	return c
}

// Tokenize returns the full token stream, terminated by an EOF token.
func (l *Lexer) Tokenize() []token.Token {
	var toks []token.Token
	for {
		t := l.next()
		toks = append(toks, t)
		if t.Type == token.EOF {
			return toks
		}
	}
}

func (l *Lexer) next() token.Token {
	spaceBefore := l.skipSpaceAndComments()
	line, col := l.line, l.col+1
	mk := func(tt token.Type, lit string) token.Token {
		return token.Token{Type: tt, Lit: lit, Line: line, Col: col, SpaceBefore: spaceBefore}
	}

	c := l.peek()
	switch {
	case c == 0:
		return mk(token.EOF, "")
	case c == '\n' || c == ';':
		l.advance()
		l.state = exprBegin
		return mk(token.NEWLINE, "\\n")
	case isDigit(c):
		return l.lexNumber(spaceBefore, line, col)
	case isIdentStart(c):
		return l.lexIdent(spaceBefore, line, col)
	case c == '"':
		return l.lexString(spaceBefore, line, col)
	case c == '@':
		return l.lexIvar(spaceBefore, line, col)
	}

	// Operators and delimiters.
	l.advance()
	switch c {
	case '+':
		l.state = exprBegin
		return mk(token.PLUS, "+")
	case '-':
		l.state = exprBegin
		return mk(token.MINUS, "-")
	case '*':
		l.state = exprBegin
		return mk(token.STAR, "*")
	case '/':
		l.state = exprBegin
		return mk(token.SLASH, "/")
	case '%':
		l.state = exprBegin
		return mk(token.PERCENT, "%")
	case '(':
		l.state = exprBegin
		return mk(token.LPAREN, "(")
	case ')':
		l.state = exprEnd
		return mk(token.RPAREN, ")")
	case '{':
		l.state = exprBegin
		return mk(token.LBRACE, "{")
	case '}':
		l.state = exprEnd
		return mk(token.RBRACE, "}")
	case '|':
		l.state = exprBegin
		return mk(token.PIPE, "|")
	case ',':
		l.state = exprBegin
		return mk(token.COMMA, ",")
	case '.':
		l.state = exprBegin
		return mk(token.DOT, ".")
	case '=':
		if l.peek() == '=' {
			l.advance()
			l.state = exprBegin
			return mk(token.EQ, "==")
		}
		l.state = exprBegin
		return mk(token.ASSIGN, "=")
	case '!':
		if l.peek() == '=' {
			l.advance()
			l.state = exprBegin
			return mk(token.NEQ, "!=")
		}
		l.state = exprBegin
		return mk(token.BANG, "!")
	case '<':
		if l.peek() == '=' {
			l.advance()
			l.state = exprBegin
			return mk(token.LE, "<=")
		}
		l.state = exprBegin
		return mk(token.LT, "<")
	case '>':
		if l.peek() == '=' {
			l.advance()
			l.state = exprBegin
			return mk(token.GE, ">=")
		}
		l.state = exprBegin
		return mk(token.GT, ">")
	}
	return mk(token.ILLEGAL, string(c))
}

// skipSpaceAndComments consumes spaces, tabs, comments and line continuations,
// returning whether any whitespace was seen (feeds SpaceBefore). Newlines are
// significant and are NOT skipped here.
func (l *Lexer) skipSpaceAndComments() bool {
	seen := false
	for {
		c := l.peek()
		switch {
		case c == ' ' || c == '\t' || c == '\r':
			l.advance()
			seen = true
		case c == '\\' && l.peek2() == '\n': // line continuation
			l.advance()
			l.advance()
			seen = true
		case c == '#': // comment to end of line
			for l.peek() != '\n' && l.peek() != 0 {
				l.advance()
			}
			seen = true
		default:
			return seen
		}
	}
}

func (l *Lexer) lexNumber(spaceBefore bool, line, col int) token.Token {
	start := l.pos
	for isDigit(l.peek()) || l.peek() == '_' {
		l.advance()
	}
	isFloat := false
	if l.peek() == '.' && isDigit(l.peek2()) {
		isFloat = true
		l.advance() // '.'
		for isDigit(l.peek()) || l.peek() == '_' {
			l.advance()
		}
	}
	lit := stripUnderscores(string(l.src[start:l.pos]))
	l.state = exprEnd
	tt := token.INT
	if isFloat {
		tt = token.FLOAT
	}
	return token.Token{Type: tt, Lit: lit, Line: line, Col: col, SpaceBefore: spaceBefore}
}

func (l *Lexer) lexIdent(spaceBefore bool, line, col int) token.Token {
	start := l.pos
	for isIdentPart(l.peek()) {
		l.advance()
	}
	// Trailing ? or ! is part of a method name (e.g. empty?, save!).
	if c := l.peek(); c == '?' || c == '!' {
		l.advance()
	}
	lit := string(l.src[start:l.pos])
	tt := token.LookupIdent(lit)
	// After a value-like keyword/identifier, the next state is "end"; after a
	// keyword that introduces an expression it stays "begin".
	switch tt {
	case token.IDENT, token.CONST, token.NIL, token.TRUE, token.FALSE, token.SELF, token.END:
		l.state = exprEnd
	default:
		l.state = exprBegin
	}
	return token.Token{Type: tt, Lit: lit, Line: line, Col: col, SpaceBefore: spaceBefore}
}

func (l *Lexer) lexIvar(spaceBefore bool, line, col int) token.Token {
	l.advance() // '@'
	start := l.pos
	for isIdentPart(l.peek()) {
		l.advance()
	}
	if start == l.pos { // a bare '@' with no name is illegal
		return token.Token{Type: token.ILLEGAL, Lit: "@", Line: line, Col: col, SpaceBefore: spaceBefore}
	}
	l.state = exprEnd
	return token.Token{Type: token.IVAR, Lit: "@" + string(l.src[start:l.pos]), Line: line, Col: col, SpaceBefore: spaceBefore}
}

func (l *Lexer) lexString(spaceBefore bool, line, col int) token.Token {
	l.advance() // opening quote
	var b []byte
	for {
		c := l.peek()
		if c == 0 || c == '"' {
			break
		}
		if c == '\\' {
			l.advance()
			esc := l.advance()
			switch esc {
			case 'n':
				b = append(b, '\n')
			case 't':
				b = append(b, '\t')
			case 'r':
				b = append(b, '\r')
			case '\\':
				b = append(b, '\\')
			case '"':
				b = append(b, '"')
			case 'e':
				b = append(b, 0x1b)
			case '0':
				b = append(b, 0)
			default:
				b = append(b, esc)
			}
			continue
		}
		b = append(b, l.advance())
	}
	if l.peek() == '"' {
		l.advance() // closing quote
	}
	l.state = exprEnd
	return token.Token{Type: token.STRING, Lit: string(b), Line: line, Col: col, SpaceBefore: spaceBefore}
}

func isDigit(c byte) bool      { return c >= '0' && c <= '9' }
func isIdentStart(c byte) bool { return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }
func isIdentPart(c byte) bool  { return isIdentStart(c) || isDigit(c) }

func stripUnderscores(s string) string {
	if !hasUnderscore(s) {
		return s
	}
	b := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] != '_' {
			b = append(b, s[i])
		}
	}
	return string(b)
}

func hasUnderscore(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '_' {
			return true
		}
	}
	return false
}
