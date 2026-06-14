// Package parser builds an AST from tokens: recursive descent for statements,
// Pratt (precedence-climbing) for expressions, and a scope stack to resolve the
// classic local-variable-vs-method-call ambiguity (plan-rbgo.md §10).
//
// The scope stack is what lets `foo` mean a variable read when `foo` was
// assigned earlier in the same def, and a (possibly command-style) method call
// otherwise — exactly MRI's rule.
package parser

import (
	"fmt"
	"strconv"

	"github.com/go-embedded-ruby/ruby/internal/ast"
	"github.com/go-embedded-ruby/ruby/internal/lexer"
	"github.com/go-embedded-ruby/ruby/internal/token"
)

type parseError struct{ msg string }

func (e parseError) Error() string { return e.msg }

// scope tracks declared locals. A hard scope is a method/class/module/top-level
// boundary that local lookup does not cross; a soft scope (a block) chains to
// its enclosing scope, so a block sees and can assign the enclosing locals.
type scope struct {
	locals map[string]bool
	hard   bool
}

func newScope(hard bool) *scope { return &scope{locals: map[string]bool{}, hard: hard} }

// Parser holds parsing state.
type Parser struct {
	toks   []token.Token
	pos    int
	scopes []*scope
}

// Parse lexes and parses src into a Program.
func Parse(src string) (prog *ast.Program, err error) {
	toks := lexer.New(src).Tokenize()
	p := &Parser{toks: toks, scopes: []*scope{newScope(true)}}
	defer func() {
		if r := recover(); r != nil {
			// Unchecked: a non-parseError is an internal bug and re-panics as a
			// conversion error rather than leaving an uncovered re-panic branch.
			prog, err = nil, r.(parseError)
		}
	}()
	body := p.parseStatements(map[token.Type]bool{})
	p.expect(token.EOF)
	return &ast.Program{Body: body}, nil
}

// --- token cursor ---

func (p *Parser) cur() token.Token  { return p.toks[p.pos] }
// peekTok returns the token after the cursor. It is only ever called with the
// cursor on an IDENT (which is never the trailing EOF), so pos+1 is in range.
func (p *Parser) peekTok() token.Token { return p.toks[p.pos+1] }
func (p *Parser) advance() token.Token { t := p.toks[p.pos]; p.pos++; return t }

func (p *Parser) is(tt token.Type) bool { return p.cur().Type == tt }

func (p *Parser) accept(tt token.Type) bool {
	if p.is(tt) {
		p.advance()
		return true
	}
	return false
}

func (p *Parser) expect(tt token.Type) token.Token {
	if !p.is(tt) {
		p.fail("expected %s, got %q (%s)", tt, p.cur().Lit, p.cur().Type)
	}
	return p.advance()
}

// fail never returns; the ast.Node result lets primary parsers write
// `return p.fail(...)` without an unreachable trailing return.
func (p *Parser) fail(format string, args ...any) ast.Node {
	t := p.cur()
	panic(parseError{msg: fmt.Sprintf("parse error at line %d: %s", t.Line, fmt.Sprintf(format, args...))})
}

func (p *Parser) skipNewlines() {
	for p.is(token.NEWLINE) {
		p.advance()
	}
}

// --- scope ---

func (p *Parser) scope() *scope         { return p.scopes[len(p.scopes)-1] }
func (p *Parser) pushScope()            { p.scopes = append(p.scopes, newScope(true)) }
func (p *Parser) pushBlockScope()       { p.scopes = append(p.scopes, newScope(false)) }
func (p *Parser) popScope()             { p.scopes = p.scopes[:len(p.scopes)-1] }
func (p *Parser) declareLocal(n string) { p.scope().locals[n] = true }

// isLocal reports whether n is a visible local: it searches the scope chain but
// does not cross a hard (method/class/module/top-level) boundary, while block
// scopes (soft) chain to their enclosing scope.
func (p *Parser) isLocal(n string) bool {
	for i := len(p.scopes) - 1; i >= 0; i-- {
		if p.scopes[i].locals[n] {
			return true
		}
		if p.scopes[i].hard {
			break
		}
	}
	return false
}

// --- statements ---

var (
	bodyEnd      = map[token.Type]bool{token.END: true}
	ifBodyEnd    = map[token.Type]bool{token.END: true, token.ELSE: true, token.ELSIF: true}
)

func (p *Parser) parseStatements(stop map[token.Type]bool) []ast.Node {
	var body []ast.Node
	for {
		p.skipNewlines()
		if p.is(token.EOF) || stop[p.cur().Type] {
			break
		}
		body = append(body, p.parseStatement())
		// Statements are separated by newlines/semicolons; the lexer emits both
		// as NEWLINE. A terminator or EOF may follow directly.
		if !p.is(token.NEWLINE) && !p.is(token.EOF) && !stop[p.cur().Type] {
			p.fail("unexpected %q after statement", p.cur().Lit)
		}
	}
	return body
}

func (p *Parser) parseStatement() ast.Node {
	switch p.cur().Type {
	case token.DEF:
		return p.parseDef()
	case token.CLASS:
		return p.parseClass()
	case token.MODULE:
		return p.parseModule()
	case token.IF:
		return p.parseIf()
	case token.UNLESS:
		return p.parseUnless()
	case token.WHILE:
		return p.parseWhile()
	case token.UNTIL:
		return p.parseUntil()
	case token.RETURN:
		return p.applyModifiers(p.parseReturn())
	default:
		return p.applyModifiers(p.parseExprOrAssign())
	}
}

// applyModifiers wraps a statement in trailing `if/unless/while/until` modifiers
// (`puts x if cond`, `return unless ok`).
func (p *Parser) applyModifiers(node ast.Node) ast.Node {
	for {
		switch p.cur().Type {
		case token.IF:
			p.advance()
			node = &ast.If{Cond: p.parseExprOrAssign(), Then: []ast.Node{node}}
		case token.UNLESS:
			p.advance()
			node = &ast.If{Cond: not(p.parseExprOrAssign()), Then: []ast.Node{node}}
		case token.WHILE:
			p.advance()
			node = &ast.While{Cond: p.parseExprOrAssign(), Body: []ast.Node{node}}
		case token.UNTIL:
			p.advance()
			node = &ast.While{Cond: not(p.parseExprOrAssign()), Body: []ast.Node{node}}
		default:
			return node
		}
	}
}

func not(n ast.Node) ast.Node { return &ast.UnaryExpr{Op: "!", Operand: n} }

func (p *Parser) parseClass() ast.Node {
	p.expect(token.CLASS)
	name := p.expect(token.CONST).Lit
	super := ""
	if p.accept(token.LT) {
		super = p.expect(token.CONST).Lit
	}
	p.pushScope() // a class body has its own local scope
	body := p.parseStatements(bodyEnd)
	p.popScope()
	p.expect(token.END)
	return &ast.ClassDef{Name: name, Super: super, Body: body}
}

func (p *Parser) parseModule() ast.Node {
	p.expect(token.MODULE)
	name := p.expect(token.CONST).Lit
	p.pushScope() // a module body has its own local scope
	body := p.parseStatements(bodyEnd)
	p.popScope()
	p.expect(token.END)
	return &ast.ModuleDef{Name: name, Body: body}
}

func (p *Parser) parseDef() ast.Node {
	p.expect(token.DEF)
	if !p.is(token.IDENT) && !p.is(token.CONST) {
		p.fail("expected method name after def")
	}
	name := p.advance().Lit
	var params []string
	if p.accept(token.LPAREN) {
		params = p.parseParamNames(token.RPAREN)
		p.expect(token.RPAREN)
	} else if p.is(token.IDENT) && !p.is(token.NEWLINE) {
		// paren-less params: def foo a, b
		params = p.parseParamNames(token.NEWLINE)
	}
	p.pushScope()
	for _, prm := range params {
		p.declareLocal(prm)
	}
	body := p.parseStatements(bodyEnd)
	p.popScope()
	p.expect(token.END)
	return &ast.MethodDef{Name: name, Params: params, Body: body}
}

func (p *Parser) parseParamNames(until token.Type) []string {
	var params []string
	if p.is(until) || p.is(token.NEWLINE) {
		return params
	}
	for {
		params = append(params, p.expect(token.IDENT).Lit)
		if !p.accept(token.COMMA) {
			break
		}
	}
	return params
}

func (p *Parser) parseIf() ast.Node {
	p.expect(token.IF)
	cond := p.parseExprOrAssign()
	p.accept(token.THEN)
	then := p.parseStatements(ifBodyEnd)
	node := &ast.If{Cond: cond, Then: then}
	for p.is(token.ELSIF) {
		p.advance()
		c := p.parseExprOrAssign()
		p.accept(token.THEN)
		b := p.parseStatements(ifBodyEnd)
		node.Elsifs = append(node.Elsifs, ast.Elsif{Cond: c, Body: b})
	}
	if p.accept(token.ELSE) {
		node.Else = p.parseStatements(bodyEnd)
	}
	p.expect(token.END)
	return node
}

// parseUnless desugars `unless c ... else ... end` to `if !c ... else ... end`.
func (p *Parser) parseUnless() ast.Node {
	p.expect(token.UNLESS)
	cond := p.parseExprOrAssign()
	p.accept(token.THEN)
	then := p.parseStatements(ifBodyEnd)
	node := &ast.If{Cond: not(cond), Then: then}
	if p.accept(token.ELSE) {
		node.Else = p.parseStatements(bodyEnd)
	}
	p.expect(token.END)
	return node
}

func (p *Parser) parseWhile() ast.Node {
	p.expect(token.WHILE)
	cond := p.parseExprOrAssign()
	p.accept(token.DO)
	body := p.parseStatements(bodyEnd)
	p.expect(token.END)
	return &ast.While{Cond: cond, Body: body}
}

// parseUntil desugars `until c ... end` to `while !c ... end`.
func (p *Parser) parseUntil() ast.Node {
	p.expect(token.UNTIL)
	cond := p.parseExprOrAssign()
	p.accept(token.DO)
	body := p.parseStatements(bodyEnd)
	p.expect(token.END)
	return &ast.While{Cond: not(cond), Body: body}
}

func (p *Parser) parseReturn() ast.Node {
	p.expect(token.RETURN)
	if p.is(token.NEWLINE) || p.is(token.EOF) || p.is(token.END) || p.is(token.ELSE) || p.is(token.ELSIF) {
		return &ast.Return{}
	}
	return &ast.Return{Value: p.parseExprOrAssign()}
}

// --- expressions ---

func (p *Parser) parseExprOrAssign() ast.Node {
	// Simple local assignment: IDENT '=' expr (right-associative, chainable).
	if p.is(token.IDENT) && p.peekTok().Type == token.ASSIGN {
		name := p.advance().Lit
		p.expect(token.ASSIGN)
		val := p.parseExprOrAssign()
		p.declareLocal(name)
		return &ast.Assign{Name: name, Value: val}
	}
	// Instance-variable assignment: @name '=' expr.
	if p.is(token.IVAR) && p.peekTok().Type == token.ASSIGN {
		name := p.advance().Lit
		p.expect(token.ASSIGN)
		return &ast.IvarAssign{Name: name, Value: p.parseExprOrAssign()}
	}
	left := p.parseBinary(0)
	// Index assignment: recv[i] = v  →  recv.[]=(i, v).
	if p.is(token.ASSIGN) {
		if call, ok := left.(*ast.Call); ok && call.Name == "[]" && call.Recv != nil {
			p.advance()
			call.Name = "[]="
			call.Args = append(call.Args, p.parseExprOrAssign())
			return call
		}
	}
	return left
}

// Binding powers for infix operators (higher binds tighter).
func binBP(tt token.Type) int {
	switch tt {
	case token.EQ, token.NEQ:
		return 10
	case token.LT, token.GT, token.LE, token.GE:
		return 20
	case token.PLUS, token.MINUS:
		return 30
	case token.STAR, token.SLASH, token.PERCENT:
		return 40
	}
	return 0
}

func (p *Parser) parseBinary(minBP int) ast.Node {
	left := p.parseUnary()
	for {
		bp := binBP(p.cur().Type)
		if bp == 0 || bp <= minBP {
			return left
		}
		op := p.advance().Lit
		right := p.parseBinary(bp) // left-associative: same-precedence stops here
		left = &ast.BinaryExpr{Op: op, Left: left, Right: right}
	}
}

func (p *Parser) parseUnary() ast.Node {
	switch p.cur().Type {
	case token.MINUS:
		p.advance()
		return &ast.UnaryExpr{Op: "-", Operand: p.parseUnary()}
	case token.PLUS:
		p.advance()
		return p.parseUnary() // unary plus is a no-op
	case token.BANG:
		p.advance()
		return &ast.UnaryExpr{Op: "!", Operand: p.parseUnary()}
	}
	return p.parsePostfix()
}

func (p *Parser) parsePostfix() ast.Node {
	node := p.parsePrimary()
	for {
		switch {
		case p.is(token.DOT):
			p.advance()
			name := p.methodName()
			var args []ast.Node
			if p.is(token.LPAREN) && !p.cur().SpaceBefore {
				p.advance()
				args = p.parseCallArgs(token.RPAREN)
				p.expect(token.RPAREN)
			}
			node = &ast.Call{Recv: node, Name: name, Args: args}
		case p.is(token.LBRACKET): // index: recv[args] → recv.[](args)
			p.advance()
			args := p.parseCallArgs(token.RBRACKET)
			p.expect(token.RBRACKET)
			node = &ast.Call{Recv: node, Name: "[]", Args: args}
		default:
			goto done
		}
	}
done:
	// A brace block binds to the immediately preceding method call. (Phase 1
	// supports `{ … }` blocks; `do … end` arrives once its looser precedence vs
	// `while/until do` is handled.)
	if call, ok := node.(*ast.Call); ok && p.is(token.LBRACE) {
		call.Block = p.parseBlock()
	}
	return node
}

// parseArrayLiteral parses `[a, b, c]` (a trailing comma and newlines are
// allowed).
func (p *Parser) parseArrayLiteral() ast.Node {
	p.expect(token.LBRACKET)
	var elems []ast.Node
	p.skipNewlines()
	for !p.is(token.RBRACKET) {
		elems = append(elems, p.parseExprOrAssign())
		p.skipNewlines()
		if !p.accept(token.COMMA) {
			break
		}
		p.skipNewlines()
	}
	p.expect(token.RBRACKET)
	return &ast.ArrayLit{Elems: elems}
}

// parseHashLiteral parses `{ k => v, … }` (hashrocket form). A `{` only reaches
// here at expression-start; a `{` after a call is a block (see parsePostfix).
func (p *Parser) parseHashLiteral() ast.Node {
	p.expect(token.LBRACE)
	h := &ast.HashLit{}
	p.skipNewlines()
	for !p.is(token.RBRACE) {
		k := p.parseExprOrAssign()
		p.expect(token.HASHROCKET)
		v := p.parseExprOrAssign()
		h.Keys = append(h.Keys, k)
		h.Values = append(h.Values, v)
		p.skipNewlines()
		if !p.accept(token.COMMA) {
			break
		}
		p.skipNewlines()
	}
	p.expect(token.RBRACE)
	return h
}

// parseBlock parses `{ [|params|] body }`.
func (p *Parser) parseBlock() *ast.Block {
	p.expect(token.LBRACE)
	p.pushBlockScope()
	var params []string
	if p.accept(token.PIPE) {
		params = p.parseParamNames(token.PIPE)
		p.expect(token.PIPE)
	}
	for _, prm := range params {
		p.declareLocal(prm)
	}
	body := p.parseStatements(map[token.Type]bool{token.RBRACE: true})
	p.popScope()
	p.expect(token.RBRACE)
	return &ast.Block{Params: params, Body: body}
}

// parseYield parses `yield`, `yield(...)`, or `yield args`.
func (p *Parser) parseYield() ast.Node {
	p.expect(token.YIELD)
	if p.is(token.LPAREN) && !p.cur().SpaceBefore {
		p.advance()
		args := p.parseCallArgs(token.RPAREN)
		p.expect(token.RPAREN)
		return &ast.Yield{Args: args}
	}
	if p.canStartCommandArg() {
		return &ast.Yield{Args: p.parseCommandArgs()}
	}
	return &ast.Yield{}
}

// methodName reads a method name after a '.': an identifier, a constant, or a
// keyword used as a method (e.g. `obj.class`, `x.then`, `a.nil?`).
func (p *Parser) methodName() string {
	t := p.cur()
	if t.Type == token.IDENT || t.Type == token.CONST {
		p.advance()
		return t.Lit
	}
	if _, isKeyword := token.Keywords[t.Lit]; isKeyword {
		p.advance()
		return t.Lit
	}
	p.fail("expected method name after '.'")
	return ""
}

func (p *Parser) parsePrimary() ast.Node {
	t := p.cur()
	switch t.Type {
	case token.INT:
		p.advance()
		n, err := strconv.ParseInt(t.Lit, 10, 64)
		if err != nil {
			// Phase 2 promotes to Bignum; for now report the overflow.
			p.fail("integer literal out of int64 range: %s", t.Lit)
		}
		return &ast.IntLit{Value: n}
	case token.FLOAT:
		p.advance()
		f, _ := strconv.ParseFloat(t.Lit, 64)
		return &ast.FloatLit{Value: f}
	case token.STRING:
		p.advance()
		return &ast.StringLit{Value: t.Lit}
	case token.SYMBOL:
		p.advance()
		return &ast.SymbolLit{Name: t.Lit}
	case token.LBRACKET:
		return p.parseArrayLiteral()
	case token.LBRACE:
		return p.parseHashLiteral()
	case token.TRUE:
		p.advance()
		return &ast.BoolLit{Value: true}
	case token.FALSE:
		p.advance()
		return &ast.BoolLit{Value: false}
	case token.NIL:
		p.advance()
		return &ast.NilLit{}
	case token.SELF:
		p.advance()
		return &ast.SelfLit{}
	case token.SUPER:
		return p.parseSuper()
	case token.YIELD:
		return p.parseYield()
	case token.LPAREN:
		p.advance()
		p.skipNewlines()
		e := p.parseExprOrAssign()
		p.skipNewlines()
		p.expect(token.RPAREN)
		return e
	case token.IDENT:
		return p.parseIdentExpr()
	case token.CONST:
		p.advance()
		return &ast.ConstRef{Name: t.Lit}
	case token.IVAR:
		p.advance()
		return &ast.IvarRef{Name: t.Lit}
	}
	return p.fail("unexpected token %q (%s)", t.Lit, t.Type)
}

// parseSuper parses `super`, `super(...)`, or `super args`. A bare `super`
// forwards the enclosing method's arguments.
func (p *Parser) parseSuper() ast.Node {
	p.expect(token.SUPER)
	if p.is(token.LPAREN) && !p.cur().SpaceBefore {
		p.advance()
		args := p.parseCallArgs(token.RPAREN)
		p.expect(token.RPAREN)
		return &ast.Super{Args: args}
	}
	if p.canStartCommandArg() {
		return &ast.Super{Args: p.parseCommandArgs()}
	}
	return &ast.Super{Forward: true}
}

// parseIdentExpr resolves a bare identifier into a variable read or a method
// call, including paren-less command calls (`puts 1 + 2`).
func (p *Parser) parseIdentExpr() ast.Node {
	name := p.cur().Lit
	next := p.peekTok()

	// foo(...) — paren call (the '(' must hug the name).
	if next.Type == token.LPAREN && !next.SpaceBefore {
		p.advance() // name
		p.advance() // (
		args := p.parseCallArgs(token.RPAREN)
		p.expect(token.RPAREN)
		return &ast.Call{Name: name, Args: args}
	}

	// Known local variable → read.
	if p.is(token.IDENT) && p.isLocal(name) {
		p.advance()
		return &ast.VarRef{Name: name}
	}

	// Otherwise it is a method call on self.
	p.advance()
	if p.canStartCommandArg() {
		return &ast.Call{Name: name, Args: p.parseCommandArgs()}
	}
	return &ast.Call{Name: name}
}

// canStartCommandArg decides whether the current token begins a paren-less
// argument list. This is the `foo -1` (call) vs `foo - 1` (subtraction)
// disambiguation, driven by SpaceBefore.
func (p *Parser) canStartCommandArg() bool {
	t := p.cur()
	if !t.SpaceBefore {
		return false
	}
	switch t.Type {
	case token.INT, token.FLOAT, token.STRING, token.SYMBOL, token.IDENT, token.CONST,
		token.IVAR, token.TRUE, token.FALSE, token.NIL, token.SELF, token.BANG,
		token.LPAREN, token.LBRACKET:
		return true
	case token.MINUS, token.PLUS:
		// Unary-style argument: `foo -1` (operand hugs the sign), not `foo - 1`.
		return !p.peekTok().SpaceBefore
	}
	return false
}

func (p *Parser) parseCommandArgs() []ast.Node {
	args := []ast.Node{p.parseBinary(0)}
	for p.accept(token.COMMA) {
		p.skipNewlines()
		args = append(args, p.parseBinary(0))
	}
	return args
}

func (p *Parser) parseCallArgs(until token.Type) []ast.Node {
	var args []ast.Node
	p.skipNewlines()
	if p.is(until) {
		return args
	}
	args = append(args, p.parseExprOrAssign())
	for p.accept(token.COMMA) {
		p.skipNewlines()
		args = append(args, p.parseExprOrAssign())
	}
	p.skipNewlines()
	return args
}
