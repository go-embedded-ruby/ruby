// Package token defines the lexical tokens for the Phase 0 subset.
//
// The token set is intentionally richer than strictly necessary (it carries
// SpaceBefore, the seed of MRI's spaceSeen) so the parser can disambiguate
// command calls (`foo -1` vs `foo - 1`) without the lexer being rewritten as
// the grammar grows (plan-rbgo.md §10).
package token

type Type int

const (
	EOF Type = iota
	ILLEGAL
	NEWLINE // \n or ;

	INT
	FLOAT
	STRING
	STRBEG // "…#{  (start of an interpolated string; Lit = literal prefix)
	STRMID // }…#{  (literal between two interpolations)
	STREND // }…"   (literal suffix ending an interpolated string)
	IDENT  // local variable or method name (lowercase / _ leading)
	CONST  // Capitalized identifier
	IVAR   // @instance_variable
	SYMBOL // :name
	LABEL  // name: in a hash literal

	// Keywords.
	DEF
	CLASS
	MODULE
	END
	IF
	ELSIF
	ELSE
	UNLESS
	WHILE
	UNTIL
	RETURN
	BREAK
	NEXT
	BEGIN
	RESCUE
	ENSURE
	CASE
	WHEN
	RETRY
	THEN
	DO
	TRUE
	FALSE
	NIL
	SELF
	SUPER
	YIELD

	// Operators and delimiters.
	PLUS
	MINUS
	STAR
	POW // **
	SLASH
	PERCENT
	ASSIGN
	EQ
	EQQ // ===
	NEQ
	LT
	GT
	LE
	GE
	SPACESHIP // <=>
	OPASSIGN  // compound assignment (+=, -=, ||=, …); Lit holds the operator
	SHOVEL    // <<
	ANDAND    // &&
	OROR      // ||
	BANG
	QUESTION // ?
	COLON    // : (ternary separator)
	LPAREN
	RPAREN
	LBRACE
	RBRACE
	LBRACKET
	RBRACKET
	PIPE
	HASHROCKET // =>
	COMMA
	DOT
	DOTDOT    // ..
	DOTDOTDOT // ...
	AMPER     // & (block-pass / block param)
	ARROW     // -> (stabby lambda)
)

var typeNames = map[Type]string{
	EOF: "EOF", ILLEGAL: "ILLEGAL", NEWLINE: "NEWLINE", INT: "INT", FLOAT: "FLOAT",
	STRING: "STRING", STRBEG: "STRBEG", STRMID: "STRMID", STREND: "STREND", IDENT: "IDENT", CONST: "CONST", IVAR: "IVAR", SYMBOL: "SYMBOL", LABEL: "LABEL",
	DEF: "def", CLASS: "class", MODULE: "module", END: "end",
	IF: "if", ELSIF: "elsif", ELSE: "else", UNLESS: "unless", WHILE: "while",
	UNTIL: "until", RETURN: "return", BREAK: "break", NEXT: "next", BEGIN: "begin", RESCUE: "rescue", ENSURE: "ensure", CASE: "case", WHEN: "when", RETRY: "retry",
	THEN: "then", DO: "do", TRUE: "true", FALSE: "false", NIL: "nil", SELF: "self",
	SUPER: "super", YIELD: "yield",
	PLUS: "+", MINUS: "-", STAR: "*", POW: "**", SLASH: "/", PERCENT: "%", ASSIGN: "=",
	EQ: "==", EQQ: "===", NEQ: "!=", LT: "<", GT: ">", LE: "<=", GE: ">=", BANG: "!",
	SPACESHIP: "<=>", SHOVEL: "<<", ANDAND: "&&", OROR: "||", OPASSIGN: "op=", QUESTION: "?", COLON: ":",
	LPAREN: "(", RPAREN: ")", LBRACE: "{", RBRACE: "}", LBRACKET: "[", RBRACKET: "]",
	PIPE: "|", HASHROCKET: "=>", COMMA: ",", DOT: ".", DOTDOT: "..", DOTDOTDOT: "...",
	AMPER: "&", ARROW: "->",
}

func (t Type) String() string {
	if s, ok := typeNames[t]; ok {
		return s
	}
	return "Type?"
}

// Keywords maps reserved words to their token type.
var Keywords = map[string]Type{
	"def": DEF, "class": CLASS, "module": MODULE, "end": END,
	"if": IF, "elsif": ELSIF, "else": ELSE,
	"unless": UNLESS, "while": WHILE, "until": UNTIL, "return": RETURN,
	"break": BREAK, "next": NEXT,
	"begin": BEGIN, "rescue": RESCUE, "ensure": ENSURE,
	"case": CASE, "when": WHEN, "retry": RETRY,
	"then": THEN, "do": DO,
	"true": TRUE, "false": FALSE, "nil": NIL, "self": SELF, "super": SUPER,
	"yield": YIELD,
}

// Token is a single lexed token.
type Token struct {
	Type        Type
	Lit         string
	Line        int
	Col         int
	SpaceBefore bool // whitespace immediately preceded this token (MRI spaceSeen)
}

// LookupIdent returns the keyword type for s, or IDENT/CONST otherwise.
func LookupIdent(s string) Type {
	if kw, ok := Keywords[s]; ok {
		return kw
	}
	if c := s[0]; c >= 'A' && c <= 'Z' {
		return CONST
	}
	return IDENT
}
