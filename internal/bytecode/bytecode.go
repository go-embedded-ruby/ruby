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
	OpNewRange // A = 1 if exclusive; pops Hi then Lo into a new range

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
	OpGetGVar      // A = Names index; pushes the named global (match-data specials)
	OpSetConst     // A = Names index; sets the named constant to top of stack (kept)
	OpDefineClass  // A = Names index, B = Children index; defines/reopens a class
	OpDefineModule // A = Names index, B = Children index; defines/reopens a module
	OpDefineMethod // A = Names index, B = Children index; defines on the current class
	OpDefineSMethod // A = Names index, B = Children index; defines a singleton (class) method
	OpInvokeSuper  // A = argc, B = 1 to forward the frame's args (bare super) else 0
	OpInvokeBlock  // A = argc; yields to the block passed to the current method
	OpBlockGiven   // pushes true if a block was passed to the current method
	OpReturn       // returns top of stack from the current ISeq
	OpArgGiven     // A = param index; pushes true if that argument was supplied
	OpBreak        // unwinds a block `break`: pops the value, signals the call site
	OpPushHandler  // A = rescue handler pc; pushes a begin/rescue handler
	OpPopHandler   // pops the innermost handler (begin body completed normally)
	OpReThrow      // re-raises the exception object on top of the stack
	OpSplatToArray // pops a value; pushes it if an Array else wrapped in a 1-array
	OpConcatArray  // pops two Arrays b,a; pushes a concatenated with b
	OpSendArray    // like OpSend but args come from an Array: stack recv, argsArray
	OpKwGiven      // A = keyword-param index; pushes true if that keyword was supplied
	OpHashSetPair  // stack acc,k,v → acc with k→v set (incremental hash build)
	OpHashMerge    // stack acc,other → acc with other (a Hash) merged in (** splat)
	OpSendBlockArg // like OpSend but a &block-pass value sits on top of the args
	OpSendArrayBlockArg // like OpSendArray with a &block-pass value on top
	OpRegexp            // A = Names index (source), B = Names index (flags); pushes a compiled Regexp
)

var opNames = map[Op]string{
	OpNop: "nop", OpPushConst: "push_const", OpPushNil: "push_nil",
	OpPushTrue: "push_true", OpPushFalse: "push_false", OpPushSelf: "push_self",
	OpNewArray: "new_array", OpNewHash: "new_hash", OpNewRange: "new_range",
	OpPop: "pop", OpDup: "dup", OpGetLocal: "get_local", OpSetLocal: "set_local",
	OpAdd: "add", OpSub: "sub", OpMul: "mul", OpDiv: "div", OpMod: "mod",
	OpLt: "lt", OpGt: "gt", OpLe: "le", OpGe: "ge", OpEq: "eq", OpNeq: "neq",
	OpNeg: "neg", OpNot: "not", OpJump: "jump", OpBranchIf: "branch_if",
	OpBranchUnless: "branch_unless", OpSend: "send", OpGetIvar: "get_ivar",
	OpSetIvar: "set_ivar", OpGetConst: "get_const", OpSetConst: "set_const", OpGetGVar: "get_gvar", OpDefineClass: "define_class",
	OpDefineModule: "define_module", OpDefineMethod: "define_method", OpDefineSMethod: "define_smethod",
	OpInvokeSuper: "invoke_super", OpInvokeBlock: "invoke_block",
	OpBlockGiven: "block_given", OpReturn: "return", OpBreak: "break", OpArgGiven: "arg_given",
	OpPushHandler: "push_handler", OpPopHandler: "pop_handler", OpReThrow: "rethrow",
	OpSplatToArray: "splat_to_array", OpConcatArray: "concat_array", OpSendArray: "send_array",
	OpKwGiven: "kw_given", OpHashSetPair: "hash_set_pair", OpHashMerge: "hash_merge",
	OpSendBlockArg: "send_block_arg", OpSendArrayBlockArg: "send_array_block_arg",
	OpRegexp: "regexp",
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
	Params    []string       // parameter names
	NumRequired int          // count of required (non-defaulted) leading params
	SplatIndex  int          // index of the *splat param, or -1
	KwNames     []string     // keyword-param names; slots follow the positionals
	KwRequired  []bool       // parallel to KwNames; true = required (no default)
	KwRestSlot  int          // slot of the **rest keyword-splat param, or -1
	BlockSlot   int          // slot of the &block param, or -1
	NumLocals int            // total local slots (params first, then assigns)
	Children  []*ISeq        // nested ISeqs (method bodies / class bodies defined here)
	Super     string         // for a class body: the superclass name ("" → Object)
}
