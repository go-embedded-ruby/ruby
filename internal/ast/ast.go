// Package ast defines the Phase 0 abstract syntax tree.
//
// Everything in Ruby is an expression, so there is no statement/expression
// split: a body is a slice of Node and its value is the value of its last node.
package ast

// Node is any AST node.
type Node interface{ node() }

// Program is the top-level sequence of expressions.
type Program struct{ Body []Node }

// IntLit is an integer literal.
type IntLit struct{ Value int64 }

// FloatLit is a floating-point literal.
type FloatLit struct{ Value float64 }

// StringLit is a (Phase 0: non-interpolated) string literal.
type StringLit struct{ Value string }

// SymbolLit is a symbol literal (:name); Name excludes the leading colon.
type SymbolLit struct{ Name string }

// ArrayLit is an array literal [a, b, c].
type ArrayLit struct{ Elems []Node }

// HashLit is a hash literal {k => v, …}; Keys[i] maps to Values[i].
type HashLit struct {
	Keys   []Node
	Values []Node
}

// RangeLit is a range literal: Lo..Hi (inclusive) or Lo...Hi (Exclusive).
type RangeLit struct {
	Lo, Hi    Node
	Exclusive bool
}

// BoolLit is true or false.
type BoolLit struct{ Value bool }

// NilLit is nil.
type NilLit struct{}

// SelfLit is self.
type SelfLit struct{}

// VarRef references a local variable known to the current scope.
type VarRef struct{ Name string }

// Assign is `Name = Value` (local assignment).
type Assign struct {
	Name  string
	Value Node
}

// BinaryExpr is `Left Op Right` for the Phase 0 fast-path operators.
type BinaryExpr struct {
	Op    string
	Left  Node
	Right Node
}

// UnaryExpr is `Op Operand` (- or !).
type UnaryExpr struct {
	Op      string
	Operand Node
}

// Call is a method call. Recv is nil for a self/funcall (e.g. `puts x`, `fib(n)`).
// Block is an optional literal block ({…} or do…end) attached to the call.
type Call struct {
	Recv  Node
	Name  string
	Args  []Node
	Block *Block
}

// Block is a literal block: parameters and a body. It is a closure over the
// scope in which it appears.
type Block struct {
	Params []string
	Body   []Node
}

// Yield invokes the block passed to the enclosing method.
type Yield struct {
	Args []Node
}

// If is an if/elsif/else expression.
type If struct {
	Cond   Node
	Then   []Node
	Elsifs []Elsif
	Else   []Node // nil if absent
}

// Elsif is one elsif branch.
type Elsif struct {
	Cond Node
	Body []Node
}

// While is a while loop. Its value is nil.
type While struct {
	Cond Node
	Body []Node
}

// MethodDef defines a method on the current self.
type MethodDef struct {
	Name   string
	Params []string
	Body   []Node
}

// Return is an explicit return.
type Return struct{ Value Node } // Value may be nil

// ConstRef references a constant (e.g. a class name) by name.
type ConstRef struct{ Name string }

// IvarRef reads an instance variable (@name) of self.
type IvarRef struct{ Name string }

// IvarAssign is `@Name = Value`.
type IvarAssign struct {
	Name  string
	Value Node
}

// ClassDef defines or reopens a class. Super is the optional superclass name.
type ClassDef struct {
	Name  string
	Super string // "" if none
	Body  []Node
}

// ModuleDef defines or reopens a module.
type ModuleDef struct {
	Name string
	Body []Node
}

// Super calls the same-named method in the ancestor chain. Forward is true for a
// bare `super` (passes the enclosing method's own arguments); otherwise Args are
// the explicit arguments of `super(...)`.
type Super struct {
	Args    []Node
	Forward bool
}

// Break exits the innermost block (terminating its iterator) or loop. Value may
// be nil.
type Break struct{ Value Node }

// Next skips to the next iteration of the innermost block or loop. Value may be
// nil.
type Next struct{ Value Node }

// OpAssign is compound assignment to a local: `Name Op= Value` (e.g. x += 1,
// x ||= 5). Compiled so a fresh local is allocated before its read.
type OpAssign struct {
	Name  string
	Op    string
	Value Node
}

func (*Program) node()    {}
func (*IntLit) node()     {}
func (*FloatLit) node()   {}
func (*StringLit) node()  {}
func (*SymbolLit) node()  {}
func (*ArrayLit) node()   {}
func (*HashLit) node()    {}
func (*RangeLit) node()   {}
func (*BoolLit) node()    {}
func (*NilLit) node()     {}
func (*SelfLit) node()    {}
func (*VarRef) node()     {}
func (*Assign) node()     {}
func (*BinaryExpr) node() {}
func (*UnaryExpr) node()  {}
func (*Call) node()       {}
func (*If) node()         {}
func (*While) node()      {}
func (*MethodDef) node()  {}
func (*Return) node()     {}
func (*ConstRef) node()   {}
func (*IvarRef) node()    {}
func (*IvarAssign) node() {}
func (*ClassDef) node()   {}
func (*ModuleDef) node()  {}
func (*Super) node()      {}
func (*Yield) node()      {}
func (*Break) node()      {}
func (*Next) node()       {}
func (*OpAssign) node()   {}
