// Package bytecode defines the instruction set and the compiled unit (ISeq).
//
// Phase 0 is a stack VM in the YARV lineage (plan-rbgo.md §6), kept minimal.
// Arithmetic and comparison have dedicated opcodes as a fast path; Phase 1
// generalizes them to OpSend over the real object model so that monkey-patching
// Integer#+ works. Until then there is no method dispatch on receivers.
package bytecode

import "github.com/go-embedded-ruby/ruby/internal/object"

// Op is a single opcode.
type Op uint8

const (
	OpNop Op = iota

	OpPushConst // A = index into Consts
	OpPushNil
	OpPushTrue
	OpPushFalse
	OpPushSelf
	OpNewArray // A = element count; pops that many values into a new array
	OpNewHash  // A = pair count; pops 2*A values (k0,v0,…) into a new hash

	OpPop
	OpDup

	OpGetLocal // A = local slot
	OpSetLocal // A = local slot (leaves the value on the stack)

	// Arithmetic / comparison fast paths (binary: pop b, pop a, push a OP b).
	OpAdd
	OpSub
	OpMul
	OpDiv
	OpMod
	OpLt
	OpGt
	OpLe
	OpGe
	OpEq
	OpNeq

	// Unary (pop a, push OP a).
	OpNeg
	OpNot

	OpJump         // A = target pc
	OpBranchIf     // A = target pc; pops, jumps if truthy
	OpBranchUnless // A = target pc; pops, jumps unless truthy

	OpSend         // A = Names index (selector), B = argc; stack: recv, args… → result
	OpGetIvar      // A = Names index; pushes @name from self (nil if unset)
	OpSetIvar      // A = Names index; sets @name on self, leaves the value
	OpGetConst     // A = Names index; pushes the named constant
	OpDefineClass  // A = Names index, B = Children index; defines/reopens a class
	OpDefineModule // A = Names index, B = Children index; defines/reopens a module
	OpDefineMethod // A = Names index, B = Children index; defines on the current class
	OpInvokeSuper  // A = argc, B = 1 to forward the frame's args (bare super) else 0
	OpInvokeBlock  // A = argc; yields to the block passed to the current method
	OpBlockGiven   // pushes true if a block was passed to the current method
	OpReturn       // returns top of stack from the current ISeq
)

var opNames = map[Op]string{
	OpNop: "nop", OpPushConst: "push_const", OpPushNil: "push_nil",
	OpPushTrue: "push_true", OpPushFalse: "push_false", OpPushSelf: "push_self",
	OpNewArray: "new_array", OpNewHash: "new_hash",
	OpPop: "pop", OpDup: "dup", OpGetLocal: "get_local", OpSetLocal: "set_local",
	OpAdd: "add", OpSub: "sub", OpMul: "mul", OpDiv: "div", OpMod: "mod",
	OpLt: "lt", OpGt: "gt", OpLe: "le", OpGe: "ge", OpEq: "eq", OpNeq: "neq",
	OpNeg: "neg", OpNot: "not", OpJump: "jump", OpBranchIf: "branch_if",
	OpBranchUnless: "branch_unless", OpSend: "send", OpGetIvar: "get_ivar",
	OpSetIvar: "set_ivar", OpGetConst: "get_const", OpDefineClass: "define_class",
	OpDefineModule: "define_module", OpDefineMethod: "define_method",
	OpInvokeSuper: "invoke_super", OpInvokeBlock: "invoke_block",
	OpBlockGiven: "block_given", OpReturn: "return",
}

func (o Op) String() string {
	if s, ok := opNames[o]; ok {
		return s
	}
	return "op?"
}

// Instr is one instruction. A, B and C are operands whose meaning depends on Op.
// For OpSend, C is the block: 0 means none, otherwise Children[C-1] is the
// literal block compiled for the call.
type Instr struct {
	Op      Op
	A, B, C int
}

// ISeq is a compiled instruction sequence: a method body, or the program top
// level. Catch tables, full arity, and source maps (plan §6) arrive with the
// later phases.
type ISeq struct {
	Name      string
	Insns     []Instr
	Consts    []object.Value // literal pool: integers, floats, strings
	Names     []string       // method-call and definition names
	Params    []string       // parameter names (Phase 0: required only)
	NumLocals int            // total local slots (params first, then assigns)
	Children  []*ISeq        // nested ISeqs (method bodies / class bodies defined here)
	Super     string         // for a class body: the superclass name ("" → Object)
}
