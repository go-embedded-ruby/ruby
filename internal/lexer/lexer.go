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
	// interpBraces tracks open '{' counts per active string interpolation, so a
	// '}' that closes an interpolation is distinguished from a hash/block brace.
	interpBraces []int
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
	case c == '/' && l.state == exprBegin:
		// At expression-begin position a '/' opens a regexp literal, not division
		// (the same disambiguation MRI uses via its lexer state).
		return l.lexRegexp(spaceBefore, line, col)
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
	case c == '$':
		return l.lexGvar(spaceBefore, line, col)
	case c == ':' && isIdentStart(l.peek2()):
		return l.lexSymbol(spaceBefore, line, col)
	}

	// Operators and delimiters.
	l.advance()
	switch c {
	case '+':
		if l.peek() == '=' {
			l.advance()
			l.state = exprBegin
			return mk(token.OPASSIGN, "+")
		}
		l.state = exprBegin
		return mk(token.PLUS, "+")
	case '-':
		if l.peek() == '=' {
			l.advance()
			l.state = exprBegin
			return mk(token.OPASSIGN, "-")
		}
		if l.peek() == '>' { // -> stabby lambda
			l.advance()
			l.state = exprBegin
			return mk(token.ARROW, "->")
		}
		l.state = exprBegin
		return mk(token.MINUS, "-")
	case '*':
		if l.peek() == '*' {
			l.advance()
			l.state = exprBegin
			return mk(token.POW, "**")
		}
		if l.peek() == '=' {
			l.advance()
			l.state = exprBegin
			return mk(token.OPASSIGN, "*")
		}
		l.state = exprBegin
		return mk(token.STAR, "*")
	case '/':
		if l.peek() == '=' {
			l.advance()
			l.state = exprBegin
			return mk(token.OPASSIGN, "/")
		}
		l.state = exprBegin
		return mk(token.SLASH, "/")
	case '%':
		if l.peek() == '=' {
			l.advance()
			l.state = exprBegin
			return mk(token.OPASSIGN, "%")
		}
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
		if n := len(l.interpBraces); n > 0 {
			l.interpBraces[n-1]++
		}
		return mk(token.LBRACE, "{")
	case '}':
		if n := len(l.interpBraces); n > 0 {
			if l.interpBraces[n-1] == 0 {
				l.interpBraces = l.interpBraces[:n-1] // this '}' closes the interpolation
				return l.continueString(line, col)
			}
			l.interpBraces[n-1]--
		}
		l.state = exprEnd
		return mk(token.RBRACE, "}")
	case '[':
		l.state = exprBegin
		return mk(token.LBRACKET, "[")
	case ']':
		l.state = exprEnd
		return mk(token.RBRACKET, "]")
	case '|':
		if l.peek() == '|' {
			l.advance()
			if l.peek() == '=' {
				l.advance()
				l.state = exprBegin
				return mk(token.OPASSIGN, "||")
			}
			l.state = exprBegin
			return mk(token.OROR, "||")
		}
		l.state = exprBegin
		return mk(token.PIPE, "|")
	case '&':
		if l.peek() == '&' {
			l.advance()
			if l.peek() == '=' {
				l.advance()
				l.state = exprBegin
				return mk(token.OPASSIGN, "&&")
			}
			l.state = exprBegin
			return mk(token.ANDAND, "&&")
		}
		l.state = exprBegin
		return mk(token.AMPER, "&")
	case ',':
		l.state = exprBegin
		return mk(token.COMMA, ",")
	case '.':
		if l.peek() == '.' {
			l.advance()
			if l.peek() == '.' {
				l.advance()
				l.state = exprBegin
				return mk(token.DOTDOTDOT, "...")
			}
			l.state = exprBegin
			return mk(token.DOTDOT, "..")
		}
		l.state = exprBegin
		return mk(token.DOT, ".")
	case '=':
		if l.peek() == '=' {
			l.advance()
			if l.peek() == '=' {
				l.advance()
				l.state = exprBegin
				return mk(token.EQQ, "===")
			}
			l.state = exprBegin
			return mk(token.EQ, "==")
		}
		if l.peek() == '>' {
			l.advance()
			l.state = exprBegin
			return mk(token.HASHROCKET, "=>")
		}
		if l.peek() == '~' { // =~ match operator
			l.advance()
			l.state = exprBegin
			return mk(token.MATCH, "=~")
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
			if l.peek() == '>' { // <=>
				l.advance()
				l.state = exprBegin
				return mk(token.SPACESHIP, "<=>")
			}
			l.state = exprBegin
			return mk(token.LE, "<=")
		}
		if l.peek() == '<' { // << or <<=
			l.advance()
			if l.peek() == '=' {
				l.advance()
				l.state = exprBegin
				return mk(token.OPASSIGN, "<<")
			}
			l.state = exprBegin
			return mk(token.SHOVEL, "<<")
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
	case '?':
		l.state = exprBegin
		return mk(token.QUESTION, "?")
	case ':':
		l.state = exprBegin
		return mk(token.COLON, ":")
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
	// A plain identifier immediately followed by a single ':' is a hash label
	// (`name:`), as in Ruby. The ':' is consumed; Lit holds the name.
	if l.peek() == ':' && l.peek2() != ':' {
		lit := string(l.src[start:l.pos])
		l.advance() // ':'
		l.state = exprBegin
		return token.Token{Type: token.LABEL, Lit: lit, Line: line, Col: col, SpaceBefore: spaceBefore}
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

// lexSymbol lexes :name (the leading ':' is at the cursor and the next byte is
// known to start an identifier). Lit holds the name without the colon.
func (l *Lexer) lexSymbol(spaceBefore bool, line, col int) token.Token {
	l.advance() // ':'
	start := l.pos
	for isIdentPart(l.peek()) {
		l.advance()
	}
	if c := l.peek(); c == '?' || c == '!' { // :empty?, :save!
		l.advance()
	}
	l.state = exprEnd
	return token.Token{Type: token.SYMBOL, Lit: string(l.src[start:l.pos]), Line: line, Col: col, SpaceBefore: spaceBefore}
}

// lexGvar lexes a global variable: $name, the match-data specials $~ $& $` $',
// or $1.. (last-match group references).
func (l *Lexer) lexGvar(spaceBefore bool, line, col int) token.Token {
	l.advance() // '$'
	start := l.pos
	switch c := l.peek(); {
	case c == '~' || c == '&' || c == '`' || c == '\'':
		l.advance()
	case c >= '1' && c <= '9':
		for l.peek() >= '0' && l.peek() <= '9' {
			l.advance()
		}
	default:
		for isIdentPart(l.peek()) {
			l.advance()
		}
		if start == l.pos { // a bare '$' with no name is illegal
			return token.Token{Type: token.ILLEGAL, Lit: "$", Line: line, Col: col, SpaceBefore: spaceBefore}
		}
	}
	l.state = exprEnd
	return token.Token{Type: token.GVAR, Lit: "$" + string(l.src[start:l.pos]), Line: line, Col: col, SpaceBefore: spaceBefore}
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

// lexRegexp lexes a /pattern/flags regexp literal. The opening '/' is at the
// cursor. Escapes are preserved verbatim into the source (so \d, \. and the
// like reach the engine untouched) except that an escaped delimiter \/ becomes
// a literal '/'. Trailing flag letters i, m, x are collected into Flags; any
// other trailing letters are ignored gracefully (consumed but not recorded).
func (l *Lexer) lexRegexp(spaceBefore bool, line, col int) token.Token {
	l.advance() // opening '/'
	var src []byte
	for {
		c := l.peek()
		if c == 0 {
			break // unterminated; emit what we have (parser will still build a literal)
		}
		if c == '/' {
			l.advance() // closing '/'
			break
		}
		if c == '\\' {
			l.advance()
			esc := l.peek()
			if esc == 0 {
				src = append(src, '\\')
				break
			}
			l.advance()
			if esc == '/' {
				src = append(src, '/') // \/ → literal slash
			} else {
				src = append(src, '\\', esc) // keep the escape for the engine
			}
			continue
		}
		src = append(src, l.advance())
	}
	var flags []byte
	for {
		c := l.peek()
		if c < 'a' || c > 'z' {
			break
		}
		l.advance()
		if c == 'i' || c == 'm' || c == 'x' {
			flags = append(flags, c)
		}
	}
	l.state = exprEnd
	return token.Token{Type: token.REGEXP, Lit: string(src), Flags: string(flags), Line: line, Col: col, SpaceBefore: spaceBefore}
}

func (l *Lexer) lexString(spaceBefore bool, line, col int) token.Token {
	l.advance() // opening quote
	lit, interp := l.scanStringSegment()
	if !interp {
		l.state = exprEnd
		return token.Token{Type: token.STRING, Lit: lit, Line: line, Col: col, SpaceBefore: spaceBefore}
	}
	l.interpBraces = append(l.interpBraces, 0)
	l.state = exprBegin
	return token.Token{Type: token.STRBEG, Lit: lit, Line: line, Col: col, SpaceBefore: spaceBefore}
}

// continueString resumes lexing a string after an interpolation's closing '}',
// returning STRMID if another interpolation follows or STREND at the close.
func (l *Lexer) continueString(line, col int) token.Token {
	lit, interp := l.scanStringSegment()
	if interp {
		l.interpBraces = append(l.interpBraces, 0)
		l.state = exprBegin
		return token.Token{Type: token.STRMID, Lit: lit, Line: line, Col: col}
	}
	l.state = exprEnd
	return token.Token{Type: token.STREND, Lit: lit, Line: line, Col: col}
}

// scanStringSegment reads a run of string content (with escapes) up to the
// closing quote (consumed) or an unescaped "#{" (consumed); the bool reports
// whether an interpolation follows.
func (l *Lexer) scanStringSegment() (string, bool) {
	var b []byte
	for {
		c := l.peek()
		if c == 0 || c == '"' {
			if c == '"' {
				l.advance()
			}
			return string(b), false
		}
		if c == '#' && l.peek2() == '{' {
			l.advance()
			l.advance()
			return string(b), true
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
