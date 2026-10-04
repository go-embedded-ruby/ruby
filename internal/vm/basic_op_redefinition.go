// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// The operator opcodes (OpAdd … OpNeq) compute their result inline for the
// built-in value types, which is what makes arithmetic fast. MRI does the same
// -- vm_opt_plus and friends -- but every one of those specialisations is
// guarded by BASIC_OP_UNREDEFINED_P: the moment anything redefines the
// operator for that class, the opcode stops specialising and dispatches the
// method instead (vm_insnhelper.c, vm.c's vm_init_redefined_flag).
//
// rbgo had the specialisation and not the guard, so a redefinition was honoured
// through #send and ignored by the operator, in the same run:
//
//	class Integer; def +(o); :HIJACKED; end; end
//	1 + 2            ruby 4.0.5  :HIJACKED    before  3
//	1.send(:+, 2)    ruby 4.0.5  :HIJACKED    before  :HIJACKED
//
// Measured over the 88 (class, operator) pairs the opcodes can reach, 60
// diverged that way, and in all 60 #send already answered correctly -- the
// method was installed and reachable, and only the opcode could not see it.
// The one operator that did work was #!=, through the explicit hasCustomNeq
// check: the guard was known, and applied to exactly one of them.
//
// The same blindness covered subclasses and prepends, which is why the guard
// resolves on the ORIGINAL receiver rather than on the unwrapped value:
//
//	class S < String; def +(o); :SUB; end; end
//	S.new("a") + "b"                     ruby :SUB   before "ab"
//	module M; def +(o); :MOD; end; end
//	class U < String; prepend M; end
//	U.new("a") + "b"                     ruby :MOD   before "ab"
//
// basicOpSnapshot records, once at the end of bootstrap, the *Method each
// (basic class, operator) pair resolves to while nothing has touched it. After
// that, "redefined" is "resolves to something else", which covers a plain
// redefinition, a subclass, a prepended module, an alias, define_method and an
// undef with one test -- and needs no hook in any of their paths.
type basicOpCacheEntry struct {
	serial uint64
	cls    *RClass
	m      *Method
}

type basicOpSnapshot struct {
	taken bool
	// cache is indexed by opcode; see overriddenBasicOp.
	cache [bytecode.OpLast]basicOpCacheEntry
	// m[class][op] is the method the pair resolved to at bootstrap. A pair MRI
	// does not define at all (String#-, Hash#+) is absent, so anything that
	// resolves later for it is a user's.
	m map[*RClass]map[bytecode.Op]*Method
}

// basicOpClasses are the classes whose operators the opcodes specialise. It is
// rbgo's analogue of MRI's vm_init_redefined_flag list, restricted to the types
// binaryOpBuiltin actually handles inline -- a type it already dispatches needs
// no guard.
func (vm *VM) basicOpClasses() []*RClass {
	// A nil is filtered HERE rather than guarded at each use. The fields are all
	// set by the time the snapshot is taken, so a nil would mean a registration
	// was skipped -- a build-tagged one, say -- and the honest handling is to
	// leave that class unwatched, which keeps its operators on the inline path.
	all := []*RClass{
		vm.cInteger, vm.cFloat, vm.cString, vm.cArray, vm.cHash,
		vm.cSymbol, vm.cNilClass, vm.cTrueClass, vm.cFalseClass,
		vm.cRational, vm.cComplex, vm.cRange, vm.cTime,
	}
	out := all[:0]
	for _, c := range all {
		if c != nil {
			out = append(out, c)
		}
	}
	return out
}

// basicOps are the operators those opcodes carry.
var basicOps = []bytecode.Op{
	bytecode.OpAdd, bytecode.OpSub, bytecode.OpMul, bytecode.OpDiv,
	bytecode.OpMod, bytecode.OpLt, bytecode.OpGt, bytecode.OpLe,
	bytecode.OpGe, bytecode.OpEq, bytecode.OpNeq,
}

// basicOpName is the method name behind an operator opcode.
//
// It is written out rather than delegated to arithOpName and compareOpName,
// which each answer for their own group and have a default arm for it:
// arithOpName returns "%" for anything that is not one of the four arithmetic
// opcodes, as its comment says it may ("only the five arithmetic opcodes reach
// binaryOp's default branch"). Building on that gave every comparison opcode
// the name "%", so the guard compared the wrong method and the four ordering
// operators silently kept specialising -- measured, before this was written out.
func basicOpName(op bytecode.Op) string {
	switch op {
	case bytecode.OpAdd:
		return "+"
	case bytecode.OpSub:
		return "-"
	case bytecode.OpMul:
		return "*"
	case bytecode.OpDiv:
		return "/"
	case bytecode.OpMod:
		return "%"
	case bytecode.OpLt:
		return "<"
	case bytecode.OpGt:
		return ">"
	case bytecode.OpLe:
		return "<="
	case bytecode.OpGe:
		return ">="
	case bytecode.OpEq:
		return "=="
	case bytecode.OpNeq:
		return "!="
	}
	return ""
}

// snapshotBasicOperators runs at the end of bootstrap, when every built-in is
// registered and no user code has run. Whatever resolves now IS the built-in.
func (vm *VM) snapshotBasicOperators() {
	snap := &basicOpSnapshot{taken: true, m: map[*RClass]map[bytecode.Op]*Method{}}
	for _, cls := range vm.basicOpClasses() {
		per := map[bytecode.Op]*Method{}
		for _, op := range basicOps {
			// No name check: basicOpName answers for every member of basicOps, and
			// TestBasicOpNameRefusesANonOperator asserts exactly that, so a guard
			// here would be unreachable code the coverage gate then reports.
			if m := undefAsNil(lookupMethod(cls, basicOpName(op))); m != nil {
				per[op] = m
			}
		}
		snap.m[cls] = per
	}
	vm.basicOps = snap
}

// overriddenBasicOp reports the method an operator should dispatch instead of
// specialising, or nil to keep the inline path. It is the analogue of
// BASIC_OP_UNREDEFINED_P, reached through the same full dispatch chain
// findMethod walks (singleton class, extended and prepended modules, then the
// ancestry) so a `def o.+`, an `o.extend M` and a subclass are all seen.
//
// It costs one method resolution per operator evaluation on a receiver whose
// class is in the snapshot. That is the price of the answer being right; the
// inline caches the sends use are not available here because an opcode carries
// no call-site data in rbgo. A redefinition is rare, so the common result is
// "same pointer as at bootstrap" and the inline path is kept.
func (vm *VM) overriddenBasicOp(op bytecode.Op, recv object.Value) *Method {
	if vm.basicOps == nil || !vm.basicOps.taken {
		// Before the snapshot (during bootstrap, and in the AOT runtime before
		// setup finishes) nothing can have been redefined yet.
		return nil
	}
	cls := vm.classOf(recv)
	// One cache entry per operator, carrying the serial it was computed at and
	// the receiver class it was computed for -- the same shape as the send sites'
	// inlineCache, and for the same reason. An arithmetic loop is monomorphic in
	// its receiver class, so the steady state is a serial compare, a pointer
	// compare and an array load, with no method resolution at all. That matters:
	// the guard sits on every operator evaluation, so paying a findMethod per
	// iteration would be paying for an answer that cannot have changed.
	//
	// globalMethodSerial moves only when a method table does, which after
	// bootstrap is rare, so an entry survives for the life of the loop.
	if e := &vm.basicOps.cache[op]; e.serial == globalMethodSerial.Load() && e.cls == cls {
		return e.m
	}
	basicOpSlowPath.Add(1)
	m := vm.resolveBasicOpOverride(op, recv, cls)
	vm.basicOps.cache[op] = basicOpCacheEntry{serial: globalMethodSerial.Load(), cls: cls, m: m}
	return m
}

// resolveBasicOpOverride is overriddenBasicOp's slow path: the actual lookup,
// run on a cache miss.
func (vm *VM) resolveBasicOpOverride(op bytecode.Op, recv object.Value, cls *RClass) *Method {
	name := basicOpName(op)
	if name == "" {
		return nil
	}
	per, watched := vm.basicOps.m[cls]
	if !watched {
		// Not a specialised class. Either binaryOpBuiltin dispatches it anyway,
		// or it is a subclass of one -- and a subclass is exactly the case the
		// snapshot cannot answer by identity, so resolve and compare against the
		// ancestor's record.
		if base := vm.snapshotAncestor(cls); base != nil {
			per = vm.basicOps.m[base]
		} else {
			return nil
		}
	}
	m := vm.findMethod(recv, name)
	if m == per[op] {
		return nil
	}
	if m == nil {
		// per[op] is necessarily non-nil here, so this IS a removal: the compare
		// above returned for m == per[op], and m is nil, so per[op] cannot be.
		// The arm that used to test for it with `if _, had := per[op]; had` --
		// and the `return nil` under it for "no record before, none now" --
		// could not be reached. The coverage ratchet is what found that: the
		// function sat at 93.8% with exactly one unreachable block, and the
		// case the dead arm was written for (Hash#+, which MRI does not define
		// either) is already answered by the m == per[op] compare, where both
		// sides are nil. wave57_guard_internals_test.go asserts that Hash#+
		// resolves to nil, and it still does -- by the earlier return.
		//
		// The built-in record was REMOVED (remove_method :+ / undef_method).
		// MRI raises there, because the opcode's specialisation is gone along
		// with the method:
		//
		//	class Integer; remove_method :+; end
		//	1 + 2    ruby 4.0.5  NoMethodError: undefined method '+' for an
		//	                     instance of Integer
		//
		// Falling back to the inline path would make the removal silently do
		// nothing -- and installing the records is what makes remove_method
		// SUCCEED here at all, so without this the new records would have
		// turned a NameError into a no-op.
		return basicOpRemoved
	}
	return m
}

// basicOpSlowPath counts cache MISSES in overriddenBasicOp -- the only path
// that resolves a method. It exists so the cache's effect can be asserted by a
// COUNT rather than by a clock: the claim is "no method resolution per
// iteration", and a count settles that on a loaded machine where a timing
// cannot. See TestOperatorGuardCostsNoResolutionPerIteration.
var basicOpSlowPath atomic.Uint64

// basicOpRemoved is the sentinel overriddenBasicOp returns when the built-in
// operator method has been removed: there is nothing to invoke, and the opcode
// must raise rather than specialise.
var basicOpRemoved = &Method{name: "<removed basic operator>"}

// snapshotAncestor finds the nearest ancestor of cls that the snapshot covers,
// so a user subclass of String is compared against String's own operator
// records rather than treated as unwatched.
func (vm *VM) snapshotAncestor(cls *RClass) *RClass {
	for c := cls; c != nil; c = c.super {
		if _, ok := vm.basicOps.m[c]; ok {
			return c
		}
	}
	return nil
}

// defineBasicOperatorMethods installs the operator methods MRI defines and rbgo
// only ever computed in the opcode. Measured against ruby 4.0.5, fourteen were
// missing entirely:
//
//	Integer  + - * /        Float  + - * /
//	String   + * %          Array  + - *
//
//	Integer.instance_method(:+)   ruby 4.0.5  #<UnboundMethod: Integer#+(_,_)>
//	                              before      NameError
//
// Each body is the opcode's own path (binaryOpBuiltin), so there is one
// implementation of the arithmetic and not two that could drift. MRI declares
// all of them with argc 1 (rb_define_method(rb_cInteger, "+", rb_int_plus, 1)
// and its siblings, numeric.c ruby_4_0:6342-6345, 6482-6485), which is what
// #arity and #parameters report.
//
// Installing them is also what lets the redefinition guard be exact: with a
// record present, "redefined" means "resolves to a DIFFERENT record", so a
// subclass that does not override keeps the inline path.
func (vm *VM) defineBasicOperatorMethods() {
	arith := []bytecode.Op{bytecode.OpAdd, bytecode.OpSub, bytecode.OpMul, bytecode.OpDiv}
	for _, op := range arith {
		installOperatorMethod(vm.cInteger, op)
		installOperatorMethod(vm.cFloat, op)
	}
	for _, op := range []bytecode.Op{bytecode.OpAdd, bytecode.OpMul, bytecode.OpMod} {
		installOperatorMethod(vm.cString, op)
	}
	for _, op := range []bytecode.Op{bytecode.OpAdd, bytecode.OpSub, bytecode.OpMul} {
		installOperatorMethod(vm.cArray, op)
	}
}

// installOperatorMethod gives cls the operator method for op, unless it already
// has one -- a registration site elsewhere wins, and this must never replace it.
//
// It takes the OPCODE rather than the name: looking the opcode up from a name
// could fail, and that arm was unreachable for the five names this passes, so
// the gate reported it as dead. Passing the opcode removes the arm instead of
// testing an impossibility.
func installOperatorMethod(cls *RClass, op bytecode.Op) {
	name := basicOpName(op)
	if _, exists := cls.methods[name]; exists {
		return
	}
	cls.defineArgc(name, 1, func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.binaryOpBuiltin(op, self, args[0])
	})
}

// concatStringParts is MRI's concatstrings: it joins already-coerced
// interpolation parts into one fresh, unfrozen String.
//
// A part that is not a String is MRI's anytostring arm. The compiler has
// already sent #to_s to every embedded expression, so reaching here with a
// non-String means that #to_s returned one, and MRI then falls back to
// rb_any_to_s rather than failing:
//
//	class Foo; def to_s; :notastring; end; end
//	"x#{Foo.new}y"     ruby 4.0.5  "x#<Foo:0x000...>y"
//	                   before      TypeError: no implicit conversion of Symbol
//
// That is the same rb_any_to_s shape Kernel#to_s owes (#756) but a separate
// path: this one is the interpolation coercion, and it does not consult the
// #to_s method again -- MRI does not either, or a #to_s returning a non-String
// would recurse.
func (vm *VM) concatStringParts(parts []object.Value) object.Value {
	var b strings.Builder
	enc := ""
	for _, p := range parts {
		// Every part is a String by construction: a literal segment is one, and
		// an expression segment has been through OpObjToString, which returns a
		// String or the rb_any_to_s rendering of the value. A defensive arm here
		// was unreachable, and the coverage gate said so.
		s := p.(*object.String)
		if e := s.Enc; e != "" && e != "US-ASCII" && enc == "" {
			enc = e
		}
		b.Write(s.Bytes())
	}
	if enc != "" {
		return object.NewStringBytesEnc([]byte(b.String()), enc)
	}
	return object.NewString(b.String())
}

// anyToSForConcat renders MRI's rb_any_to_s: `#<ClassName:0xADDR>`, the form a
// value gets when it has no usable #to_s.
func (vm *VM) anyToSForConcat(v object.Value) string {
	return "#<" + vm.classOf(v).name + ":0x" + hex16ForConcat(uint64(vm.refID(v))) + ">"
}

func hex16ForConcat(v uint64) string {
	h := strconv.FormatUint(v, 16)
	for i := len(h); i < 16; i++ {
		h = "0" + h
	}
	return h
}

// basicOpWasDefined reports whether this operator HAD a real method for this
// receiver's class at snapshot time. When it did, "no method resolves now"
// means it was removed, and the send path must raise NoMethodError rather than
// fall back to the opcode -- that fallback exists for the operators which never
// had a record, and reaching it after a remove_method made the removal a no-op.
func (vm *VM) basicOpWasDefined(op bytecode.Op, recv object.Value) bool {
	if vm.basicOps == nil || !vm.basicOps.taken {
		return false
	}
	cls := vm.classOf(recv)
	per, ok := vm.basicOps.m[cls]
	if !ok {
		if base := vm.snapshotAncestor(cls); base != nil {
			per = vm.basicOps.m[base]
		} else {
			return false
		}
	}
	_, had := per[op]
	return had
}

// objToString is MRI's objtostring instruction plus its anytostring fallback:
//
//	if (RB_TYPE_P(recv, T_STRING)) return recv;
//	str = rb_funcall(recv, idTo_s, 0);
//	return rb_obj_as_string_result(str, recv);
//
// The T_STRING arm matters and is measured: a String is returned UNTOUCHED,
// subclasses included, so a redefined String#to_s does not change what an
// interpolation produces --
//
//	class S < String; def to_s; "SUB"; end; end
//	"#{S.new("x")}"      ruby 4.0.5  "x"
//	S.new("x").to_s      ruby 4.0.5  "SUB"
//
// -- which a plain `send :to_s` gets wrong in both directions at once.
// objToStringWith is objToString with the refinement the CALL SITE resolved, if
// any. The opcode passes it because only the interpreter loop knows the lexical
// scope a `using` activated refinements in; refined is nil everywhere else, and
// then this is a plain dispatch.
func (vm *VM) objToStringWith(v object.Value, refined *Method) object.Value {
	if s := stringTypeOf(v); s != nil {
		return s
	}
	var r object.Value
	if refined != nil {
		r = vm.invoke(refined, v, nil, nil)
	} else {
		r = vm.send(v, "to_s", nil, nil)
	}
	if s, ok := r.(*object.String); ok {
		return s
	}
	return object.NewString(vm.anyToSForConcat(v))
}

// stringTypeOf returns the String a value IS -- itself, or the built-in value a
// String subclass instance wraps -- or nil when it is not a String at all. It
// is rbgo's RB_TYPE_P(v, T_STRING): a subclass instance is an *RObject carrying
// the String in its builtin slot, so a bare type assertion misses it.
//
// Not to be confused with regexp.go's stringLike, which answers a different
// question: "String or Symbol, as text", for a regexp group key.
func stringTypeOf(v object.Value) *object.String {
	if s, ok := v.(*object.String); ok {
		return s
	}
	if o, ok := v.(*RObject); ok {
		if s, ok := o.builtin.(*object.String); ok {
			return s
		}
	}
	return nil
}
