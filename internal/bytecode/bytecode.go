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
	OpBranchNil    // A = target pc; pops, jumps if the value is nil (safe nav)

	OpSend                  // A = Names index (selector), B = argc; stack: recv, args… → result
	OpGetIvar               // A = Names index; pushes @name from self (nil if unset)
	OpSetIvar               // A = Names index; sets @name on self, leaves the value
	OpGetConst              // A = Names index; pushes the named constant
	OpGetScopedConst        // A = Names index; pops a module/class, pushes its named constant
	OpSetScopedConst        // A = Names index; stack: module/class, value → value (sets recv::name)
	OpGetGVar               // A = Names index; pushes the named global (match-data specials + user globals)
	OpSetGVar               // A = Names index; sets the named global to top of stack (kept)
	OpGetCVar               // A = Names index; pushes the class variable @@name (NameError if unset)
	OpGetCVarQuiet          // A = Names index; like OpGetCVar but pushes nil (no NameError) if unset — for @@name ||= …
	OpSetCVar               // A = Names index; sets the class variable @@name to top of stack (kept)
	OpSetConst              // A = Names index; sets the named constant to top of stack (kept)
	OpDefineClass           // A = Names index, B = Children index; defines/reopens a class
	OpDefineModule          // A = Names index, B = Children index; defines/reopens a module
	OpDefineMethod          // A = Names index, B = Children index; defines on the current class
	OpDefineSMethod         // A = Names index, B = Children index; defines a singleton (class) method
	OpDefineSingletonMethod // A = Names index, B = Children index; pops a receiver, defines a singleton method on it (def recv.foo)
	OpInvokeSuper           // A = argc, B = 1 to forward the frame's args (bare super) else 0; C = 1 when an explicit block value sits on top of the stack (overriding the frame block)
	OpInvokeBlock           // A = argc; yields to the block passed to the current method
	OpInvokeBlockArray      // pops an Array of args; yields it to the block passed to the current method (yield(*args))
	OpExcMatchAny           // pops an Array of classes then an exception; pushes true if the exception is_a? any of them (rescue *classes)
	OpCaseMatchAny          // pops a subject then an Array of candidates; pushes true if any candidate === subject (case … when *array)
	OpBlockGiven            // pushes true if a block was passed to the current method
	OpReturn                // returns top of stack from the current ISeq
	OpArgGiven              // A = param index; pushes true if that argument was supplied
	OpBreak                 // unwinds a block `break`: pops the value, signals the call site
	OpPushHandler           // A = rescue handler pc; pushes a begin/rescue handler
	OpPopHandler            // pops the innermost handler (begin body completed normally)
	OpReThrow               // re-raises the exception object on top of the stack
	OpExpandArray           // A=pre, B=post, C=hasSplat; pops an Array, pushes the multi-assign target values in reverse order
	OpSplatToArray          // pops a value; pushes it if an Array else wrapped in a 1-array
	OpConcatArray           // pops two Arrays b,a; pushes a concatenated with b
	OpSendArray             // like OpSend but args come from an Array: stack recv, argsArray
	OpKwGiven               // A = keyword-param index; pushes true if that keyword was supplied
	OpHashSetPair           // stack acc,k,v → acc with k→v set (incremental hash build)
	OpHashMerge             // stack acc,other → acc with other (a Hash) merged in (** splat)
	OpSendBlockArg          // like OpSend but a &block-pass value sits on top of the args
	OpSendArrayBlockArg     // like OpSendArray with a &block-pass value on top
	OpRegexp                // A = Names index (source), B = Names index (flags); pushes a compiled Regexp (memoised per occurrence, frozen)
	OpRegexpDyn             // A = 0, or (for /o) 1+the cache-slot pc to store the once-compiled Regexp into; B = Names index (flags); pops the interpolated source String, pushes a compiled Regexp
	OpTruthy                // pops a value, pushes true if it is truthy else false (normalize a === / is_a? result)
	OpRaiseNoMatch          // pops the subject value, raises NoMatchingPatternError naming it (case/in fell through)
	OpBinding               // pushes a Binding capturing the current frame (locals, self, definee)
	OpDefineClassScoped     // A = Names index (trailing name), B = Children index, C = flags (1=parent on stack, 2=super-expr on stack); defines/reopens a class at a `::` path / with a `::`-expression superclass
	OpDefineModuleScoped    // A = Names index (trailing name), B = Children index; pops a parent module/class, defines/reopens the module there
	OpXStr                  // A = Names index (command); runs the shell command and pushes its stdout as a String (`%x{…}` / backticks)
	OpInvokeSuperArray      // super(*a, **k, &b): stack is argsArray (then a &block-pass value when C==1); C>1 means a literal block from child C-2; dispatches super with the array's elements
	OpOpenSingletonClass    // A = Children index; pops a target, runs the child ISeq with the target's singleton (meta) class as the definee (`class << target`)
	OpAlias                 // A = Names index (new name), B = Names index (old name); aliases an existing method (or global variable) on the current definee
	OpUndef                 // A = Names index; undefines the named method on the current definee

	// defined? support. Each pushes the matching tag String, or nil when the
	// operand is not defined, and never raises. The compiler routes each
	// `defined?` operand to the right one by its syntactic kind; method/receiver
	// cases run under OpDefinedGuard so an undefined sub-expression yields nil.
	OpDefinedConst       // A = Names index; "constant" if the top-level constant exists, else nil
	OpDefinedScopedConst // A = Names index; pops a module/class, "constant" if it has that constant, else nil
	OpDefinedIvar        // A = Names index; "instance-variable" if self has @name set, else nil
	OpDefinedCVar        // A = Names index; "class variable" if @@name is visible in the definee chain, else nil
	OpDefinedGVar        // A = Names index; "global-variable" if $name is set, else nil
	OpDefinedMethod      // A = Names index; pops a receiver, "method" if it responds to name, else nil
	OpDefinedYield       // "yield" if a block was passed to the current method, else nil
	OpDefinedGuard       // A = Children index; runs the child ISeq; any raise inside maps to nil

	// Added after the original opcode block to preserve existing opcode numbers
	// (and the frozen prelude bytecode).
	OpGetConstTop     // A = Names index; leading `::Name` — top-level (Object) constant only, ignoring lexical nesting
	OpDefinedConstTop // A = Names index; "constant" if the top-level `::Name` exists, else nil
	OpRegexpOnce      // A = target pc past the interpolation build; guards a /o literal: if the occurrence's Regexp is already memoised, push it and jump to A, else fall through to (re)build once
	OpDefinedSuper    // "super" if the method `super` would reach from the current frame exists, else nil; never evaluates the super arguments
)

// Defined tag Strings.
//
// MRI builds every `defined?` answer with rb_iseq_defined_string (iseq.c
// v3_4_0:3692), which returns rb_fstring_cstr(...) — a FROZEN, deduplicated
// String. ruby/spec pins that: language/defined_spec.rb asserts
// `defined?(nil).frozen?.should == true` for every tag it names.
//
// The tags live here, in the package both halves of the implementation import,
// because both halves must agree: the compiler bakes a tag into the literal
// pool for the syntactically-decided cases (nil/self/expression/…), while the
// VM builds one at run time for the cases only it can decide
// (constant/method/yield/…). One table, one constructor, so the two cannot
// drift apart and leave defined? half-frozen.
const (
	DefinedNil     = "nil"
	DefinedIvar    = "instance-variable"
	DefinedLvar    = "local-variable"
	DefinedGvar    = "global-variable"
	DefinedCvar    = "class variable"
	DefinedConst   = "constant"
	DefinedMethod  = "method"
	DefinedYield   = "yield"
	DefinedZSuper  = "super"
	DefinedSelf    = "self"
	DefinedTrue    = "true"
	DefinedFalse   = "false"
	DefinedAsgn    = "assignment"
	DefinedExprTag = "expression"
)

// DefinedTag returns the frozen String a `defined?` answer is made of. It
// mirrors rb_iseq_defined_string: the result is frozen, so a caller may share
// it freely, and `defined?(nil).frozen?` is true as in MRI.
func DefinedTag(tag string) *object.String { return object.NewFrozenStringView(tag) }

var opNames = map[Op]string{
	OpNop: "nop", OpPushConst: "push_const", OpPushNil: "push_nil",
	OpPushTrue: "push_true", OpPushFalse: "push_false", OpPushSelf: "push_self",
	OpNewArray: "new_array", OpNewHash: "new_hash", OpNewRange: "new_range",
	OpPop: "pop", OpDup: "dup", OpGetLocal: "get_local", OpSetLocal: "set_local",
	OpAdd: "add", OpSub: "sub", OpMul: "mul", OpDiv: "div", OpMod: "mod",
	OpLt: "lt", OpGt: "gt", OpLe: "le", OpGe: "ge", OpEq: "eq", OpNeq: "neq",
	OpNeg: "neg", OpNot: "not", OpJump: "jump", OpBranchIf: "branch_if",
	OpBranchUnless: "branch_unless", OpBranchNil: "branch_nil", OpSend: "send", OpGetIvar: "get_ivar",
	OpSetIvar: "set_ivar", OpGetConst: "get_const", OpGetScopedConst: "get_scoped_const", OpSetScopedConst: "set_scoped_const", OpSetConst: "set_const", OpGetGVar: "get_gvar", OpSetGVar: "set_gvar", OpGetCVar: "get_cvar", OpGetCVarQuiet: "get_cvar_quiet", OpSetCVar: "set_cvar", OpDefineClass: "define_class",
	OpDefineModule: "define_module", OpDefineMethod: "define_method", OpDefineSMethod: "define_smethod", OpDefineSingletonMethod: "define_singleton_method",
	OpInvokeSuper: "invoke_super", OpInvokeBlock: "invoke_block", OpInvokeBlockArray: "invoke_block_array", OpExcMatchAny: "exc_match_any", OpCaseMatchAny: "case_match_any",
	OpBlockGiven: "block_given", OpReturn: "return", OpBreak: "break", OpArgGiven: "arg_given",
	OpPushHandler: "push_handler", OpPopHandler: "pop_handler", OpReThrow: "rethrow",
	OpExpandArray:  "expand_array",
	OpSplatToArray: "splat_to_array", OpConcatArray: "concat_array", OpSendArray: "send_array",
	OpKwGiven: "kw_given", OpHashSetPair: "hash_set_pair", OpHashMerge: "hash_merge",
	OpSendBlockArg: "send_block_arg", OpSendArrayBlockArg: "send_array_block_arg",
	OpRegexp: "regexp", OpRegexpDyn: "regexp_dyn", OpRegexpOnce: "regexp_once", OpTruthy: "truthy", OpRaiseNoMatch: "raise_no_match",
	OpBinding:            "binding",
	OpDefineClassScoped:  "define_class_scoped",
	OpDefineModuleScoped: "define_module_scoped",
	OpXStr:               "xstr",
	OpInvokeSuperArray:   "invoke_super_array",
	OpOpenSingletonClass: "open_singleton_class",
	OpAlias:              "alias",
	OpUndef:              "undef",
	OpDefinedConst:       "defined_const",
	OpDefinedScopedConst: "defined_scoped_const",
	OpDefinedIvar:        "defined_ivar",
	OpDefinedCVar:        "defined_cvar",
	OpDefinedGVar:        "defined_gvar",
	OpDefinedMethod:      "defined_method",
	OpDefinedYield:       "defined_yield",
	OpDefinedGuard:       "defined_guard",
	OpGetConstTop:        "get_const_top",
	OpDefinedConstTop:    "defined_const_top",
	OpDefinedSuper:       "defined_super",
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
//
// Flags is a bitfield of per-instruction flags (see the FlagSend* constants).
// For the send opcodes it records whether the call had an explicit receiver, so
// the VM can enforce private/protected method visibility (a private method is
// callable only with an implicit — or `self.` — receiver).
type Instr struct {
	Op      Op
	A, B, C int
	Flags   int
}

const (
	// FlagSendExplicit marks a send whose receiver was written explicitly
	// (`obj.foo`), as opposed to an implicit-receiver call (`foo`). A bare
	// `self.foo` is treated as implicit for the private-visibility check (MRI
	// permits private calls through an explicit self), so the compiler does NOT
	// set this flag when the receiver is `self`.
	FlagSendExplicit = 1 << iota

	// FlagSendNoKW marks a send whose LAST argument is syntactically a POSITIONAL
	// value, so a Hash arriving there must NOT be turned into keyword arguments.
	//
	// MRI decides this at the call site, not in the callee: setup_parameters_complex
	// (vm_args.c v3_4_0:591) only ever peels a trailing hash when the call info
	// carries VM_CALL_KWARG (literal `k: v` arguments) or VM_CALL_KW_SPLAT (`**h`).
	// With neither flag, `keyword_hash` stays Qnil and the hash counts as an
	// ordinary positional argument — which is why, since Ruby 3.0,
	//
	//	def foo(a, b, c, **hsh); end
	//	h = {key: 42}
	//	foo(1, 2, 3, h)     # ArgumentError (given 4, expected 3)
	//	foo(1, 2, 3, **h)   # hsh == {key: 42}
	//
	// rbgo materialises keyword arguments into a trailing Hash in the compiler, so
	// by the time the VM binds parameters the two forms look identical. This flag
	// carries the call site's answer through. It is NEGATIVE on purpose: it is set
	// only where the compiler can PROVE the last argument is positional, so every
	// path that cannot say (a native re-dispatch such as Object#send, Proc#call, or
	// a call site whose last argument is a Hash LITERAL — see below) keeps the
	// older "peel a trailing hash" behaviour rather than silently losing keywords.
	//
	// The one shape it cannot yet decide is a braced hash literal: go-ruby-parser
	// v0.3.0 parses `foo(1, 2, 3, {key: 42})` and `foo(1, 2, 3, key: 42)` into the
	// SAME *ast.HashLit, with no record of the braces, so the compiler cannot tell
	// them apart. Those sites are left unflagged (keywords), which is what rbgo
	// did before. Distinguishing them needs a `Braced` bit on ast.HashLit upstream.
	FlagSendNoKW

	// FlagSendKWSplat marks an array-dispatch send (OpSendArray, OpSendArrayBlockArg,
	// OpInvokeBlockArray, OpInvokeSuperArray) whose LAST written argument is a
	// keyword splat — `**h`, or a keyword list containing one — so the last element
	// of the argument Array is that keyword Hash and nothing else is.
	//
	// It is MRI's VM_CALL_KW_SPLAT, and it exists because the hash may have to
	// disappear: ignore_keyword_hash_p (vm_args.c v3_4_0:506) drops an EMPTY
	// keyword splat from the argument list entirely, so `f(**{})` passes no
	// argument at all and `f(x, **{})` passes exactly one — a POSITIONAL x, even
	// when x is itself a Hash. rbgo used to make that decision in the compiler, by
	// emitting an `empty?` test that skipped the concatenation; but a call site
	// that drops its own last argument can no longer say whether what is now last
	// was written as keywords, which is what FlagSendNoKW has to answer. So the
	// hash is always appended and the VM drops it, exactly where MRI does.
	FlagSendKWSplat
)

// LineEntry maps the first instruction of a run to the source line that run
// came from. It is one half of MRI's pair: insns_info[i].line_no beside
// positions[i], the instruction index the entry starts at (iseq.c v3_4_0:673,
// rb_iseq_insns_info_encode_positions).
type LineEntry struct {
	PC   int // first instruction index this line covers
	Line int // 1-based source line
}

// ISeq is a compiled instruction sequence: a method body, or the program top
// level. Catch tables and full arity arrive with the later phases.
type ISeq struct {
	Name string
	// File is the source-file path this ISeq (and its nested children) was loaded
	// from, for Kernel#__FILE__ / #__dir__. doRequire propagates it to Children so
	// a method defined in one file reports that file even when called from another.
	// Empty for compiled-in code (the prelude).
	File        string
	Insns       []Instr
	Consts      []object.Value // literal pool: integers, floats, strings
	Names       []string       // method-call and definition names
	Params      []string       // parameter names
	NumRequired int            // count of required (non-defaulted) leading params
	SplatIndex  int            // index of the *splat param, or -1

	// PostCount is the number of trailing REQUIRED positional parameters when
	// there is NO *splat — the post parameters of `def m(a=1, b)` or
	// `{ |a=5, b, c, d| }`. It is 0 whenever SplatIndex >= 0, where the post
	// parameters are already derivable as len(Params)-SplatIndex-1, and 0 for the
	// ordinary shape where every required parameter leads.
	//
	// MRI keeps post_num beside lead_num and opt_num in every iseq
	// (rb_iseq_param, vm_core.h), independent of has_rest, because the two counts
	// answer different questions: min_argc is lead_num + post_num, and post
	// parameters bind from the TAIL before the optionals are filled from what is
	// left (setup_parameters_complex, vm_args.c v3_4_0:878-892). With only
	// NumRequired and SplatIndex the no-splat case cannot be said at all: rbgo
	// read `def m(a=1, b)` as two optionals and bound b from the front.
	//
	// NumRequired stays the LEADING required count, so a reader that predates this
	// field keeps the meaning it had.
	PostCount  int
	KwNames    []string // keyword-param names; slots follow the positionals
	KwRequired []bool   // parallel to KwNames; true = required (no default)
	KwRestSlot int      // slot of the **rest keyword-splat param, or -1
	BlockSlot  int      // slot of the &block param, or -1
	NumLocals  int      // total local slots (params first, then assigns)
	Locals     []string // local-variable names by slot (for Binding); "" for anonymous slots
	Children   []*ISeq  // nested ISeqs (method bodies / class bodies defined here)
	Super      string   // for a class body: the superclass name ("" → Object)

	// Lines is the source map: which source line each instruction came from,
	// COMPRESSED to one entry per line CHANGE and kept sorted by PC.
	//
	// That compression is MRI's, not a shortcut. MRI records a line for every
	// instruction while compiling and then encodes the result as an
	// insns_info[] of distinct entries beside a positions[] of the instruction
	// index each one starts at (iseq.c v3_4_0:673), because a line spans a run
	// of instructions and storing it per instruction would multiply every ISeq
	// for nothing. Reading it back is a search for the last entry at or before
	// the pc — get_insn_info_binary_search (iseq.c v3_4_0:2160), which is the
	// implementation MRI ships as VM_INSN_INFO_TABLE_IMPL == 1. LineAt is that
	// search.
	//
	// Empty for an ISeq compiled from a source with no position information
	// (the AOT-frozen prelude of an older build); LineAt then answers 0, exactly
	// as rb_iseq_line_no does when get_insn_info finds no entry
	// (iseq.c v3_4_0:2314).
	Lines []LineEntry

	// FirstLine is the line the ISeq's defining construct sits on — MRI's
	// body->location.first_lineno. rb_vm_get_sourceline (vm_backtrace.c
	// v3_4_0:105) falls back to it when the per-instruction lookup yields 0, so
	// a frame that has not reached a mapped instruction still reports where it
	// was defined rather than line 0.
	FirstLine int

	// Caches backs the per-call-site inline method caches, one slot per
	// instruction (only OpSend slots are ever used). It is opaque to this package
	// — the vm package allocates it and gives it meaning — so the field is typed
	// `any` to keep bytecode free of a vm import (vm imports bytecode, not the
	// reverse). The vm fills it lazily on first execution of the ISeq.
	Caches any

	// handlerState memoises whether this ISeq contains any rescue handler
	// (OpPushHandler): 0 = not scanned, 1 = none, 2 = has one. The vm reads it to
	// skip the per-frame recover defer for the common no-rescue method. Lazily
	// filled under the GVL via the accessors below.
	handlerState uint8
}

// LineAt returns the 1-based source line the instruction at pc came from, or 0
// when this ISeq carries no line covering it.
//
// It is MRI's get_insn_info_binary_search (iseq.c v3_4_0:2160) over the pair
// (positions, insns_info): find the last entry whose position is at or before
// pc. A pc before the first entry has no line — MRI's search likewise starts at
// index 1 and can only return an entry it has passed — and answers 0, which is
// what rb_iseq_line_no returns for an unplaceable pc (iseq.c v3_4_0:2314).
func (s *ISeq) LineAt(pc int) int {
	if len(s.Lines) == 0 || pc < s.Lines[0].PC {
		return 0
	}
	// Invariant: Lines[lo].PC <= pc. Halve until lo is the last such entry.
	lo, hi := 0, len(s.Lines)
	for hi-lo > 1 {
		mid := int(uint(lo+hi) >> 1)
		if s.Lines[mid].PC <= pc {
			lo = mid
		} else {
			hi = mid
		}
	}
	return s.Lines[lo].Line
}

// HandlerState reports the memoised rescue-handler flag (0/1/2; see the field).
func (s *ISeq) HandlerState() uint8 { return s.handlerState }

// SetHandlerState records the rescue-handler flag computed by the vm. Kept off
// the exported field set so the AOT freeze emitter (which writes only the
// source-level fields) is unaffected.
func (s *ISeq) SetHandlerState(v uint8) { s.handlerState = v }

// IsLineStart reports whether pc is the FIRST instruction of a line run — the
// place a `line` trace event belongs.
//
// MRI does not answer this at run time at all: its compiler emits a distinct
// `trace` instruction wherever a new line begins (iseq_compile_each keeps
// last_line and only adds one when the line changes), and rb_iseq_trace_set
// (iseq.c v3_4_0:3981) flips those encodings on when a hook wants
// ISEQ_TRACE_EVENTS, so a program with no TracePoint executes no trace
// instruction and pays nothing. rbgo has no trace instruction to flip, so the
// same question is asked of the line map instead — and the map already holds
// exactly the same information, because Lines carries one entry per line CHANGE
// (see the field comment), which is the set of positions MRI would have marked.
//
// The search is LineAt's, narrowed to an exact hit: the entry found must start
// at pc, not merely cover it. Callers gate this on tracing being enabled, so
// the binary search never runs for an untraced program.
func (s *ISeq) IsLineStart(pc int) bool {
	if len(s.Lines) == 0 || pc < s.Lines[0].PC {
		return false
	}
	lo, hi := 0, len(s.Lines)
	for hi-lo > 1 {
		mid := int(uint(lo+hi) >> 1)
		if s.Lines[mid].PC <= pc {
			lo = mid
		} else {
			hi = mid
		}
	}
	return s.Lines[lo].PC == pc
}

// HasLine reports whether any instruction in this ISeq is mapped to the given
// 1-based source line. It is what decides MRI's "can not enable any hooks"
// ArgumentError for a `target_line:` that names no line of the target's code
// (rb_tracepoint_enable_for_target, vm_trace.c v3_4_0:1234, whose count n stays
// 0 when rb_iseq_add_local_tracepoint_recursively matches nothing).
func (s *ISeq) HasLine(line int) bool {
	for _, e := range s.Lines {
		if e.Line == line {
			return true
		}
	}
	return false
}
