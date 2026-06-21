package vm

import (
	"fmt"
	"math"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// bootstrap builds the base class hierarchy and installs the Phase 1 kernel.
// Kernel methods live on Object so every value answers them.
func (vm *VM) bootstrap() {
	vm.cBasicObject = newClass("BasicObject", nil)
	vm.cObject = newClass("Object", vm.cBasicObject)
	vm.cModule = newClass("Module", vm.cObject)
	vm.cClass = newClass("Class", vm.cModule)
	vm.cInteger = newClass("Integer", vm.cObject)
	vm.cFloat = newClass("Float", vm.cObject)
	vm.cComplex = newClass("Complex", vm.cObject)
	vm.cRational = newClass("Rational", vm.cObject)
	vm.cString = newClass("String", vm.cObject)
	vm.cSymbol = newClass("Symbol", vm.cObject)
	vm.cArray = newClass("Array", vm.cObject)
	vm.cHash = newClass("Hash", vm.cObject)
	vm.cRange = newClass("Range", vm.cObject)
	vm.cProc = newClass("Proc", vm.cObject)
	vm.cTrueClass = newClass("TrueClass", vm.cObject)
	vm.cFalseClass = newClass("FalseClass", vm.cObject)
	vm.cNilClass = newClass("NilClass", vm.cObject)
	vm.cRegexp = newClass("Regexp", vm.cObject)
	vm.cMatchData = newClass("MatchData", vm.cObject)

	for _, c := range []*RClass{
		vm.cBasicObject, vm.cObject, vm.cModule, vm.cClass, vm.cInteger,
		vm.cFloat, vm.cComplex, vm.cRational, vm.cString, vm.cSymbol, vm.cArray, vm.cHash, vm.cRange,
		vm.cProc, vm.cTrueClass, vm.cFalseClass, vm.cNilClass,
		vm.cRegexp, vm.cMatchData,
	} {
		vm.consts[c.name] = c
	}

	vm.registerComplex()
	vm.registerRational()
	vm.registerMath()
	vm.registerFFT()
	vm.registerNDArray()
	vm.registerImage()
	vm.registerSet()
	vm.registerTime()
	vm.registerBigDecimal()
	vm.registerDate()
	vm.registerBag()
	vm.registerEval()
	vm.registerRequire()
	vm.registerSingleton()
	vm.registerMethod()

	procCall := func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.callBlock(self.(*Proc), args)
	}
	vm.cProc.define("call", procCall)
	vm.cProc.define("[]", procCall)
	vm.cProc.define("yield", procCall)
	vm.cProc.define("arity", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(self.(*Proc).arityVal())
	})
	vm.cProc.define("lambda?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self.(*Proc).isLambda)
	})
	dupFn := func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return dupValue(self)
	}
	vm.cObject.define("dup", dupFn)
	vm.cObject.define("clone", dupFn)
	vm.cObject.define("freeze", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		if s, ok := self.(*object.String); ok {
			s.Frozen = true
		}
		return self
	})
	vm.cObject.define("loop", func(vm *VM, _ object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (loop)")
		}
		// Runs forever; a `break` in the block unwinds to the call site (its
		// value becomes loop's result) via the enclosing sendCatchBreak.
		for {
			vm.callBlock(blk, nil)
		}
	})
	vm.cObject.define("equal?", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		// Object identity: reference types compare by pointer, the immutable
		// value types by value (Go interface equality gives exactly this).
		return object.Bool(self == args[0])
	})
	vm.cObject.define("frozen?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(isFrozen(self))
	})
	vm.cObject.define("instance_variable_get", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return getIvar(self, args[0].ToS())
	})
	vm.cObject.define("instance_variable_set", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		setIvar(self, args[0].ToS(), args[1])
		return args[1]
	})
	vm.cObject.define("instance_variable_defined?", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		t := ivarTable(self)
		if t == nil {
			return object.False
		}
		_, ok := t[args[0].ToS()]
		return object.Bool(ok)
	})
	vm.cObject.define("instance_eval", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (yield)")
		}
		return vm.callBlockSelf(blk, self, nil)
	})
	vm.cObject.define("instance_exec", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (yield)")
		}
		return vm.callBlockSelf(blk, self, args)
	})
	formatFn := func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		fmtStr, ok := args[0].(*object.String)
		if !ok {
			raise("TypeError", "no implicit conversion of %s into String", classNameOf(args[0]))
		}
		return object.NewString(formatString(fmtStr.Str(), args[1:]))
	}
	vm.cObject.define("format", formatFn)
	vm.cObject.define("sprintf", formatFn)
	vm.cObject.define("proc", func(_ *VM, _ object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("ArgumentError", "tried to create Proc object without a block")
		}
		return blk
	})
	vm.cObject.define("lambda", func(_ *VM, _ object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("ArgumentError", "tried to create Proc object without a block")
		}
		blk.isLambda = true
		return blk
	})
	vm.cSymbol.define("to_proc", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		name := string(self.(object.Symbol))
		// :sym.to_proc is { |recv, *rest| recv.sym(*rest) } — arity -2 as in MRI.
		return &Proc{nativeArity: -2, native: func(vm *VM, args []object.Value) object.Value {
			return vm.send(args[0], name, args[1:], nil)
		}}
	})

	// Exception hierarchy. Each is registered as a constant so it can be raised
	// and matched by rescue.
	exc := func(name, super string) *RClass {
		c := newClass(name, vm.consts[super].(*RClass))
		vm.consts[name] = c
		return c
	}
	cException := newClass("Exception", vm.cObject)
	vm.consts["Exception"] = cException
	vm.cException = cException
	exc("StandardError", "Exception")
	exc("RuntimeError", "StandardError")
	exc("ArgumentError", "StandardError")
	exc("TypeError", "StandardError")
	exc("NameError", "StandardError")
	exc("NoMethodError", "NameError")
	exc("ZeroDivisionError", "StandardError")
	exc("RangeError", "StandardError")
	exc("IndexError", "StandardError")
	exc("KeyError", "IndexError")
	exc("StopIteration", "IndexError")
	exc("LocalJumpError", "StandardError")
	exc("NotImplementedError", "StandardError")
	exc("FrozenError", "RuntimeError")
	exc("IOError", "StandardError")
	exc("RegexpError", "StandardError")
	exc("NoMatchingPatternError", "StandardError")
	exc("NoMatchingPatternKeyError", "NoMatchingPatternError")
	exc("Math::DomainError", "StandardError")
	// ScriptError / SyntaxError sit under Exception (NOT StandardError), so a bare
	// `rescue` does not catch them — matching MRI. eval raises SyntaxError.
	exc("ScriptError", "Exception")
	exc("SyntaxError", "ScriptError")
	exc("LoadError", "ScriptError")

	// Exception instance protocol: initialize stores @message; message/to_s
	// return it (or the class name when unset).
	cException.define("initialize", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) > 0 {
			self.(*RObject).ivars["@message"] = object.NewString(args[0].ToS())
		}
		return object.NilV
	})
	excMessage := func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		if m := getIvar(self, "@message"); m != object.NilV {
			return m
		}
		return object.NewString(vm.classOf(self).name)
	}
	cException.define("message", excMessage)
	cException.define("to_s", excMessage)

	// Kernel (on Object).
	vm.cObject.define("puts", nativePuts)
	vm.cObject.define("print", nativePrint)
	vm.cObject.define("p", nativeP)
	vm.cObject.define("class", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.classOf(self)
	})
	vm.cObject.define("to_s", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self.ToS())
	})
	vm.cObject.define("inspect", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self.Inspect())
	})
	vm.cObject.define("nil?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		_, isNil := self.(object.Nil)
		return object.Bool(isNil)
	})
	vm.cObject.define("initialize", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NilV
	})
	vm.cObject.define("method_missing", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		name := args[0].ToS()
		return raise("NoMethodError", "undefined method '%s' for %s", name, vm.classOf(self).name)
	})
	isAFn := func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(classIsA(vm.classOf(self), classArg(args[0])))
	}
	vm.cObject.define("is_a?", isAFn)
	vm.cObject.define("kind_of?", isAFn)
	vm.cObject.define("instance_of?", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(vm.classOf(self) == classArg(args[0]))
	})
	vm.cObject.define("raise", nativeRaise)
	vm.cObject.define("Integer", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		switch v := args[0].(type) {
		case object.Integer:
			return v
		case object.Float:
			return object.Integer(int64(v))
		case *object.String:
			base := 10
			if len(args) > 1 {
				base = int(intArg(args[1]))
			}
			n, err := strconv.ParseInt(strings.TrimSpace(v.Str()), base, 64)
			if err != nil {
				raise("ArgumentError", "invalid value for Integer(): %s", v.Inspect())
			}
			return object.Integer(n)
		}
		raise("TypeError", "can't convert %s into Integer", args[0].Inspect())
		return object.NilV
	})
	vm.cObject.define("Float", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		switch v := args[0].(type) {
		case object.Float:
			return v
		case object.Integer:
			return object.Float(float64(v))
		case *object.String:
			f, err := strconv.ParseFloat(strings.TrimSpace(v.Str()), 64)
			if err != nil {
				raise("ArgumentError", "invalid value for Float(): %s", v.Inspect())
			}
			return object.Float(f)
		}
		raise("TypeError", "can't convert %s into Float", args[0].Inspect())
		return object.NilV
	})
	vm.cObject.define("String", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.send(args[0], "to_s", nil, nil)
	})
	vm.cObject.define("Array", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		switch v := args[0].(type) {
		case object.Nil:
			return &object.Array{}
		case *object.Array:
			return v
		default:
			return &object.Array{Elems: []object.Value{v}}
		}
	})
	vm.cObject.define("send", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		return vm.send(self, args[0].ToS(), args[1:], blk)
	})
	vm.cObject.define("public_send", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		return vm.send(self, args[0].ToS(), args[1:], blk)
	})
	vm.cObject.define("respond_to?", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(lookupMethod(vm.classOf(self), args[0].ToS()) != nil)
	})
	vm.cObject.define("itself", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return self
	})
	vm.cObject.define("tap", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (tap)")
		}
		vm.callBlock(blk, []object.Value{self})
		return self
	})
	thenFn := func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (then)")
		}
		return vm.callBlock(blk, []object.Value{self})
	}
	vm.cObject.define("then", thenFn)
	vm.cObject.define("yield_self", thenFn)
	// Default equality: object identity for instances, structural for value
	// types (Comparable#== and user-defined == override this via dispatch).
	vm.cObject.define("==", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(rubyEqual(self, args[0]))
	})
	// Default <=>: 0 when equal (by ==), nil otherwise — the MRI Object#<=>.
	vm.cObject.define("<=>", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if vm.send(self, "==", []object.Value{args[0]}, nil).Truthy() {
			return object.Integer(0)
		}
		return object.NilV
	})
	// Case equality. Object#=== defaults to ==; Module/Class#=== is is_a?;
	// Range#=== is membership. These drive `case`/`when`.
	vm.cObject.define("===", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(vm.send(self, "==", []object.Value{args[0]}, nil).Truthy())
	})
	vm.cModule.define("===", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(classIsA(vm.classOf(args[0]), self.(*RClass)))
	})

	// Module (Class inherits these).
	vm.cModule.define("include", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		target := self.(*RClass)
		for _, a := range args {
			mod := a.(*RClass)
			target.includes = append(target.includes, mod)
			// Hook: module.included(base), fired per included module if it defines
			// the hook (singleton method).
			if hook := lookupSMethod(mod, "included"); hook != nil {
				vm.invoke(hook, mod, []object.Value{target}, nil)
			}
		}
		return target
	})
	vm.cModule.define("attr_reader", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		defineAttrs(self.(*RClass), args, true, false)
		return object.NilV
	})
	vm.cModule.define("attr_writer", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		defineAttrs(self.(*RClass), args, false, true)
		return object.NilV
	})
	vm.cModule.define("attr_accessor", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		defineAttrs(self.(*RClass), args, true, true)
		return object.NilV
	})
	classEvalFn := func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (yield)")
		}
		return vm.classEval(self.(*RClass), blk, nil)
	}
	vm.cModule.define("class_eval", classEvalFn)
	vm.cModule.define("module_eval", classEvalFn)
	vm.cModule.define("class_exec", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (yield)")
		}
		return vm.classEval(self.(*RClass), blk, args)
	})
	vm.cModule.define("define_method", func(_ *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		body := blk
		if body == nil {
			if len(args) > 1 {
				p, ok := args[1].(*Proc)
				if !ok {
					raise("TypeError", "wrong argument type %s (expected Proc)", classNameOf(args[1]))
				}
				body = p
			} else {
				raise("ArgumentError", "tried to create a method without a block")
			}
		}
		name := args[0].ToS()
		self.(*RClass).methods[name] = &Method{name: name, proc: body, owner: self.(*RClass)}
		return object.Symbol(name)
	})

	// Symbol.
	vm.cSymbol.define("to_sym", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return self
	})
	// Spaceship (<=>) for the built-in ordered types; numerics compare across
	// Integer/Float, strings lexically, and a mismatched type yields nil.
	vm.cInteger.define("<=>", spaceshipNumeric)
	vm.cFloat.define("<=>", spaceshipNumeric)
	vm.cInteger.define("**", powNumeric)
	vm.cInteger.define("pow", powNumeric)
	vm.cFloat.define("**", powNumeric)
	vm.cFloat.define("pow", powNumeric)
	vm.cString.define("<=>", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := self.(*object.String)
		b, ok := args[0].(*object.String)
		if !ok {
			return object.NilV
		}
		return object.Integer(strings.Compare(a.Str(), b.Str()))
	})

	// String. Methods over the mutable byte-based String (length/chars/index are
	// rune-aware for UTF-8). strOf reads the receiver's current contents.
	strOf := func(self object.Value) string { return self.(*object.String).Str() }
	vm.cString.define("length", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(utf8.RuneCountInString(strOf(self)))
	})
	vm.cString.define("size", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(utf8.RuneCountInString(strOf(self)))
	})
	vm.cString.define("bytesize", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(len(strOf(self)))
	})
	vm.cString.define("empty?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(len(strOf(self)) == 0)
	})
	vm.cString.define("upcase", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(strings.ToUpper(strOf(self)))
	})
	vm.cString.define("downcase", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(strings.ToLower(strOf(self)))
	})
	vm.cString.define("capitalize", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(capitalizeStr(strOf(self)))
	})
	vm.cString.define("swapcase", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(swapcaseStr(strOf(self)))
	})
	vm.cString.define("reverse", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(reverseStr(strOf(self)))
	})
	vm.cString.define("strip", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(strings.Trim(strOf(self), wsCutset))
	})
	vm.cString.define("lstrip", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(strings.TrimLeft(strOf(self), wsCutset))
	})
	vm.cString.define("rstrip", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(strings.TrimRight(strOf(self), wsCutset))
	})
	vm.cString.define("chomp", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(chompStr(strOf(self)))
	})
	vm.cString.define("chop", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(chopStr(strOf(self)))
	})
	vm.cString.define("chars", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		var out []object.Value
		for _, r := range strOf(self) {
			out = append(out, object.NewString(string(r)))
		}
		return &object.Array{Elems: out}
	})
	vm.cString.define("bytes", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		s := strOf(self)
		out := make([]object.Value, len(s))
		for i := 0; i < len(s); i++ {
			out[i] = object.Integer(s[i])
		}
		return &object.Array{Elems: out}
	})
	vm.cString.define("lines", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		segs := splitLines(strOf(self))
		out := make([]object.Value, len(segs))
		for i, seg := range segs {
			out[i] = object.NewString(seg)
		}
		return &object.Array{Elems: out}
	})
	vm.cString.define("each_line", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (each_line)")
		}
		for _, seg := range splitLines(strOf(self)) {
			vm.callBlock(blk, []object.Value{object.NewString(seg)})
		}
		return self
	})
	vm.cString.define("each_char", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (each_char)")
		}
		for _, r := range strOf(self) {
			vm.callBlock(blk, []object.Value{object.NewString(string(r))})
		}
		return self
	})
	vm.cString.define("each_byte", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (each_byte)")
		}
		s := strOf(self)
		for i := 0; i < len(s); i++ {
			vm.callBlock(blk, []object.Value{object.Integer(s[i])})
		}
		return self
	})
	vm.cString.define("split", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.stringSplit(strOf(self), args)
	})
	vm.cString.define("include?", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(strings.Contains(strOf(self), strArg(args[0])))
	})
	vm.cString.define("start_with?", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(strings.HasPrefix(strOf(self), strArg(args[0])))
	})
	vm.cString.define("end_with?", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(strings.HasSuffix(strOf(self), strArg(args[0])))
	})
	vm.cString.define("index", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		byteIdx := strings.Index(strOf(self), strArg(args[0]))
		if byteIdx < 0 {
			return object.NilV
		}
		return object.Integer(utf8.RuneCountInString(strOf(self)[:byteIdx]))
	})
	vm.cString.define("=~", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		re, ok := args[0].(*Regexp)
		if !ok {
			raise("TypeError", "type mismatch: %s given", classNameOf(args[0]))
		}
		return vm.regexpMatchIndex(re, self)
	})
	vm.cString.define("match?", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(strMatchRegexp(args[0]).re.MatchString(strOf(self)))
	})
	vm.cString.define("match", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.runMatch(strMatchRegexp(args[0]), strOf(self))
	})
	vm.cString.define("scan", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		return vm.scan(scanRegexp(args[0]), strOf(self), self, blk)
	})
	vm.cString.define("sub", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		return vm.stringSub(strOf(self), args, blk, false)
	})
	vm.cString.define("gsub", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		return vm.stringSub(strOf(self), args, blk, true)
	})
	vm.cString.define("to_i", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(parseLeadingInt(strOf(self)))
	})
	vm.cString.define("to_f", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Float(parseLeadingFloat(strOf(self)))
	})
	vm.cString.define("to_s", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return self
	})
	vm.cString.define("to_str", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return self
	})
	vm.cString.define("to_sym", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Symbol(strOf(self))
	})
	vm.cString.define("ljust", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(padString(strOf(self), args, 'l'))
	})
	vm.cString.define("rjust", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(padString(strOf(self), args, 'r'))
	})
	vm.cString.define("center", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(padString(strOf(self), args, 'c'))
	})
	vm.cString.define("tr", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		from := expandCharSet(strArg(args[0]))
		to := expandCharSet(strArg(args[1]))
		out := make([]byte, 0, len(strOf(self)))
		for i := 0; i < len(strOf(self)); i++ {
			b := strOf(self)[i]
			if idx := byteIndex(from, b); idx >= 0 {
				if len(to) == 0 {
					continue // empty replacement deletes
				}
				if idx >= len(to) {
					idx = len(to) - 1
				}
				out = append(out, to[idx])
			} else {
				out = append(out, b)
			}
		}
		return &object.String{B: out}
	})
	vm.cString.define("count", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		set := expandCharSet(strArg(args[0]))
		n := 0
		for i := 0; i < len(strOf(self)); i++ {
			if byteIndex(set, strOf(self)[i]) >= 0 {
				n++
			}
		}
		return object.Integer(n)
	})
	vm.cString.define("delete", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		set := expandCharSet(strArg(args[0]))
		out := make([]byte, 0, len(strOf(self)))
		for i := 0; i < len(strOf(self)); i++ {
			if byteIndex(set, strOf(self)[i]) < 0 {
				out = append(out, strOf(self)[i])
			}
		}
		return &object.String{B: out}
	})
	vm.cString.define("squeeze", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		str := strOf(self)
		out := make([]byte, 0, len(str))
		for i := 0; i < len(str); i++ {
			if i > 0 && str[i] == str[i-1] {
				continue
			}
			out = append(out, str[i])
		}
		return &object.String{B: out}
	})
	vm.cString.define("[]", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return stringIndex(strOf(self), args)
	})
	vm.cString.define("slice", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return stringIndex(strOf(self), args)
	})
	vm.cString.define("ord", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		s := strOf(self)
		if s == "" {
			raise("ArgumentError", "empty string")
		}
		return object.Integer([]rune(s)[0])
	})
	vm.cString.define("partition", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		s, sep := strOf(self), strArg(args[0])
		if i := strings.Index(s, sep); i >= 0 {
			return &object.Array{Elems: []object.Value{object.NewString(s[:i]), object.NewString(sep), object.NewString(s[i+len(sep):])}}
		}
		return &object.Array{Elems: []object.Value{object.NewString(s), object.NewString(""), object.NewString("")}}
	})
	vm.cString.define("rpartition", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		s, sep := strOf(self), strArg(args[0])
		if i := strings.LastIndex(s, sep); i >= 0 {
			return &object.Array{Elems: []object.Value{object.NewString(s[:i]), object.NewString(sep), object.NewString(s[i+len(sep):])}}
		}
		return &object.Array{Elems: []object.Value{object.NewString(""), object.NewString(""), object.NewString(s)}}
	})

	// String mutation (in-place). Every mutator guards against a frozen receiver.
	// `<<` and concat append each argument: a String contributes its bytes, an
	// Integer its UTF-8 code point.
	strConcatFn := func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		s := self.(*object.String)
		checkFrozen(s)
		for _, a := range args {
			s.B = append(s.B, strAppendBytes(a)...)
		}
		return s
	}
	vm.cString.define("<<", strConcatFn)
	vm.cString.define("concat", strConcatFn)
	vm.cString.define("replace", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		s := self.(*object.String)
		checkFrozen(s)
		s.B = []byte(strArg(args[0]))
		return s
	})
	vm.cString.define("prepend", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		s := self.(*object.String)
		checkFrozen(s)
		var head []byte
		for _, a := range args {
			head = append(head, strAppendBytes(a)...)
		}
		s.B = append(head, s.B...)
		return s
	})
	vm.cString.define("insert", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		s := self.(*object.String)
		checkFrozen(s)
		r := []rune(s.Str())
		at := int(intArg(args[0]))
		if at < 0 {
			at += len(r) + 1
		}
		if at < 0 || at > len(r) {
			raise("IndexError", "index %d out of string", intArg(args[0]))
		}
		ins := []rune(strArg(args[1]))
		out := append(append(append([]rune{}, r[:at]...), ins...), r[at:]...)
		s.B = []byte(string(out))
		return s
	})
	vm.cString.define("clear", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		s := self.(*object.String)
		checkFrozen(s)
		s.B = s.B[:0]
		return s
	})
	vm.cString.define("upcase!", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return strBang(self, strings.ToUpper)
	})
	vm.cString.define("downcase!", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return strBang(self, strings.ToLower)
	})
	vm.cString.define("capitalize!", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return strBang(self, capitalizeStr)
	})
	vm.cString.define("swapcase!", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return strBang(self, swapcaseStr)
	})
	vm.cString.define("reverse!", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		s := self.(*object.String)
		checkFrozen(s)
		s.B = []byte(reverseStr(s.Str()))
		return s
	})
	vm.cString.define("strip!", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return strBang(self, func(x string) string { return strings.Trim(x, wsCutset) })
	})
	vm.cString.define("lstrip!", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return strBang(self, func(x string) string { return strings.TrimLeft(x, wsCutset) })
	})
	vm.cString.define("rstrip!", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return strBang(self, func(x string) string { return strings.TrimRight(x, wsCutset) })
	})
	vm.cString.define("chomp!", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return strBang(self, chompStr)
	})
	vm.cString.define("chop!", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return strBang(self, chopStr)
	})
	vm.cString.define("squeeze!", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return strBang(self, squeezeStr)
	})
	vm.cString.define("sub!", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		return vm.strSubBang(self, args, blk, false)
	})
	vm.cString.define("gsub!", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		return vm.strSubBang(self, args, blk, true)
	})
	vm.cString.define("[]=", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return stringIndexAssign(self.(*object.String), args)
	})
	vm.cString.define("slice!", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return stringSliceBang(self.(*object.String), args)
	})

	// Array.
	vm.cArray.define("length", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(len(self.(*object.Array).Elems))
	})
	vm.cArray.define("size", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(len(self.(*object.Array).Elems))
	})
	vm.cArray.define("empty?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(len(self.(*object.Array).Elems) == 0)
	})
	vm.cArray.define("first", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := self.(*object.Array)
		if len(args) == 0 {
			if len(a.Elems) == 0 {
				return object.NilV
			}
			return a.Elems[0]
		}
		n := clampCount(intArg(args[0]), len(a.Elems))
		out := make([]object.Value, n)
		copy(out, a.Elems[:n])
		return &object.Array{Elems: out}
	})
	vm.cArray.define("last", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := self.(*object.Array)
		if len(args) == 0 {
			if len(a.Elems) == 0 {
				return object.NilV
			}
			return a.Elems[len(a.Elems)-1]
		}
		n := clampCount(intArg(args[0]), len(a.Elems))
		out := make([]object.Value, n)
		copy(out, a.Elems[len(a.Elems)-n:])
		return &object.Array{Elems: out}
	})
	vm.cArray.define("push", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := self.(*object.Array)
		a.Elems = append(a.Elems, args...)
		return a
	})
	vm.cArray.define("<<", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := self.(*object.Array)
		a.Elems = append(a.Elems, args[0])
		return a
	})
	vm.cArray.define("include?", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		for _, e := range self.(*object.Array).Elems {
			if valueEqual(e, args[0]) {
				return object.True
			}
		}
		return object.False
	})
	vm.cArray.define("[]", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := self.(*object.Array)
		if rng, ok := args[0].(*object.Range); ok {
			start, length, ok := sliceRange(len(a.Elems), rng)
			if !ok {
				return object.NilV
			}
			out := make([]object.Value, length)
			copy(out, a.Elems[start:start+length])
			return &object.Array{Elems: out}
		}
		if len(args) == 2 { // a[start, len]
			start := normIndex(intArg(args[0]), len(a.Elems))
			length := int(intArg(args[1]))
			if start < 0 || start > len(a.Elems) || length < 0 {
				return object.NilV
			}
			end := start + length
			if end > len(a.Elems) {
				end = len(a.Elems)
			}
			out := make([]object.Value, end-start)
			copy(out, a.Elems[start:end])
			return &object.Array{Elems: out}
		}
		if i, ok := arrayIndex(a, intArg(args[0])); ok {
			return a.Elems[i]
		}
		return object.NilV
	})
	vm.cArray.define("[]=", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := self.(*object.Array)
		i, n := intArg(args[0]), int64(len(a.Elems))
		if i < 0 {
			i += n
		}
		if i < 0 || i > n {
			raise("IndexError", "index %d out of array", intArg(args[0]))
		}
		if i == n {
			a.Elems = append(a.Elems, args[1])
		} else {
			a.Elems[i] = args[1]
		}
		return args[1]
	})
	vm.cArray.define("each", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (each)")
		}
		a := self.(*object.Array)
		for _, e := range a.Elems {
			vm.callBlock(blk, []object.Value{e})
		}
		return a
	})
	vm.cArray.define("map", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (map)")
		}
		a := self.(*object.Array)
		out := make([]object.Value, len(a.Elems))
		for i, e := range a.Elems {
			out[i] = vm.callBlock(blk, []object.Value{e})
		}
		return &object.Array{Elems: out}
	})
	vm.cArray.define("reverse", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		a := self.(*object.Array)
		out := make([]object.Value, len(a.Elems))
		for i, e := range a.Elems {
			out[len(a.Elems)-1-i] = e
		}
		return &object.Array{Elems: out}
	})
	vm.cArray.define("uniq", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		var out []object.Value
		for _, e := range self.(*object.Array).Elems {
			dup := false
			for _, k := range out {
				if valueEqual(e, k) {
					dup = true
					break
				}
			}
			if !dup {
				out = append(out, e)
			}
		}
		return &object.Array{Elems: out}
	})
	// Set intersection (&) and union (|): both deduplicate, keeping first-seen
	// order, matching Ruby.
	vm.cArray.define("&", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := self.(*object.Array)
		b := arrArg(args[0])
		var out []object.Value
		for _, e := range a.Elems {
			if arrayIncludes(b.Elems, e) && !arrayIncludes(out, e) {
				out = append(out, e)
			}
		}
		return &object.Array{Elems: out}
	})
	vm.cArray.define("|", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := self.(*object.Array)
		b := arrArg(args[0])
		var out []object.Value
		for _, e := range append(append([]object.Value{}, a.Elems...), b.Elems...) {
			if !arrayIncludes(out, e) {
				out = append(out, e)
			}
		}
		return &object.Array{Elems: out}
	})
	vm.cArray.define("map!", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (map!)")
		}
		a := self.(*object.Array)
		for i := range a.Elems {
			a.Elems[i] = vm.callBlock(blk, []object.Value{a.Elems[i]})
		}
		return self
	})
	vm.cArray.define("reverse!", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		a := self.(*object.Array)
		for i, j := 0, len(a.Elems)-1; i < j; i, j = i+1, j-1 {
			a.Elems[i], a.Elems[j] = a.Elems[j], a.Elems[i]
		}
		return self
	})
	vm.cArray.define("sort!", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		a := self.(*object.Array)
		sort.SliceStable(a.Elems, func(i, j int) bool { return vm.spaceship(a.Elems[i], a.Elems[j]) < 0 })
		return self
	})
	selectBang := func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given")
		}
		return arrayKeepIf(vm, self.(*object.Array), blk, true)
	}
	vm.cArray.define("select!", selectBang)
	vm.cArray.define("filter!", selectBang)
	vm.cArray.define("reject!", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (reject!)")
		}
		return arrayKeepIf(vm, self.(*object.Array), blk, false)
	})
	vm.cArray.define("compact!", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		a := self.(*object.Array)
		var out []object.Value
		for _, e := range a.Elems {
			if _, isNil := e.(object.Nil); !isNil {
				out = append(out, e)
			}
		}
		if len(out) == len(a.Elems) {
			return object.NilV
		}
		a.Elems = out
		return self
	})
	vm.cArray.define("uniq!", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		a := self.(*object.Array)
		var out []object.Value
		for _, e := range a.Elems {
			dup := false
			for _, k := range out {
				if valueEqual(e, k) {
					dup = true
					break
				}
			}
			if !dup {
				out = append(out, e)
			}
		}
		if len(out) == len(a.Elems) {
			return object.NilV
		}
		a.Elems = out
		return self
	})
	vm.cArray.define("compact", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		var out []object.Value
		for _, e := range self.(*object.Array).Elems {
			if _, isNil := e.(object.Nil); !isNil {
				out = append(out, e)
			}
		}
		return &object.Array{Elems: out}
	})
	vm.cArray.define("flatten", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		depth := -1
		if len(args) > 0 {
			depth = int(intArg(args[0]))
		}
		return &object.Array{Elems: flattenDepth(self.(*object.Array).Elems, depth)}
	})
	vm.cArray.define("sum", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		acc := object.Value(object.Integer(0))
		if len(args) > 0 {
			acc = args[0]
		}
		for _, e := range self.(*object.Array).Elems {
			acc = vm.binaryOp(bytecode.OpAdd, acc, e)
		}
		return acc
	})
	vm.cArray.define("each_slice", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (each_slice)")
		}
		n := int(intArg(args[0]))
		if n <= 0 {
			raise("ArgumentError", "invalid slice size")
		}
		a := self.(*object.Array)
		for i := 0; i < len(a.Elems); i += n {
			end := i + n
			if end > len(a.Elems) {
				end = len(a.Elems)
			}
			slice := make([]object.Value, end-i)
			copy(slice, a.Elems[i:end])
			vm.callBlock(blk, []object.Value{&object.Array{Elems: slice}})
		}
		return self
	})
	vm.cArray.define("each_cons", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (each_cons)")
		}
		n := int(intArg(args[0]))
		if n <= 0 {
			raise("ArgumentError", "invalid size")
		}
		a := self.(*object.Array)
		for i := 0; i+n <= len(a.Elems); i++ {
			window := make([]object.Value, n)
			copy(window, a.Elems[i:i+n])
			vm.callBlock(blk, []object.Value{&object.Array{Elems: window}})
		}
		return self
	})
	vm.cArray.define("take_while", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (take_while)")
		}
		var out []object.Value
		for _, e := range self.(*object.Array).Elems {
			if !vm.callBlock(blk, []object.Value{e}).Truthy() {
				break
			}
			out = append(out, e)
		}
		return &object.Array{Elems: out}
	})
	vm.cArray.define("drop_while", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (drop_while)")
		}
		a := self.(*object.Array)
		i := 0
		for i < len(a.Elems) && vm.callBlock(blk, []object.Value{a.Elems[i]}).Truthy() {
			i++
		}
		out := make([]object.Value, len(a.Elems)-i)
		copy(out, a.Elems[i:])
		return &object.Array{Elems: out}
	})
	vm.cArray.define("rotate", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := self.(*object.Array)
		n := len(a.Elems)
		if n == 0 {
			return &object.Array{}
		}
		shift := 1
		if len(args) > 0 {
			shift = int(intArg(args[0]))
		}
		shift = ((shift % n) + n) % n
		out := make([]object.Value, n)
		for i := 0; i < n; i++ {
			out[i] = a.Elems[(i+shift)%n]
		}
		return &object.Array{Elems: out}
	})
	vm.cArray.define("join", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		sep := ""
		if len(args) > 0 {
			sep = strArg(args[0])
		}
		return object.NewString(joinArray(self.(*object.Array), sep))
	})
	vm.cArray.define("index", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		for i, e := range self.(*object.Array).Elems {
			if valueEqual(e, args[0]) {
				return object.Integer(i)
			}
		}
		return object.NilV
	})
	vm.cArray.define("take", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := self.(*object.Array)
		n := int(intArg(args[0]))
		if n < 0 {
			raise("ArgumentError", "attempt to take negative size")
		}
		if n > len(a.Elems) {
			n = len(a.Elems)
		}
		out := make([]object.Value, n)
		copy(out, a.Elems[:n])
		return &object.Array{Elems: out}
	})
	vm.cArray.define("drop", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := self.(*object.Array)
		n := int(intArg(args[0]))
		if n < 0 {
			raise("ArgumentError", "attempt to drop negative size")
		}
		if n > len(a.Elems) {
			n = len(a.Elems)
		}
		out := make([]object.Value, len(a.Elems)-n)
		copy(out, a.Elems[n:])
		return &object.Array{Elems: out}
	})
	vm.cArray.define("sort", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		a := self.(*object.Array)
		out := make([]object.Value, len(a.Elems))
		copy(out, a.Elems)
		sort.SliceStable(out, func(i, j int) bool { return vm.spaceship(out[i], out[j]) < 0 })
		return &object.Array{Elems: out}
	})
	vm.cArray.define("sort_by", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (sort_by)")
		}
		a := self.(*object.Array)
		keys := make([]object.Value, len(a.Elems))
		for i, e := range a.Elems {
			keys[i] = vm.callBlock(blk, []object.Value{e})
		}
		// Sort an index permutation so each element stays paired with its key.
		idx := make([]int, len(a.Elems))
		for i := range idx {
			idx[i] = i
		}
		sort.SliceStable(idx, func(i, j int) bool { return vm.spaceship(keys[idx[i]], keys[idx[j]]) < 0 })
		out := make([]object.Value, len(idx))
		for i, k := range idx {
			out[i] = a.Elems[k]
		}
		return &object.Array{Elems: out}
	})
	vm.cArray.define("min_by", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		return vm.arrayByExtreme(self.(*object.Array), blk, "min_by", -1)
	})
	vm.cArray.define("max_by", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		return vm.arrayByExtreme(self.(*object.Array), blk, "max_by", 1)
	})
	vm.cArray.define("each_with_object", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (each_with_object)")
		}
		memo := args[0]
		for _, e := range self.(*object.Array).Elems {
			vm.callBlock(blk, []object.Value{e, memo})
		}
		return memo
	})

	// Hash.
	// Hash.new — Hash.new, Hash.new(default), or Hash.new { |hash, key| … }.
	vm.cHash.smethods["new"] = &Method{name: "new", owner: vm.cHash,
		native: func(_ *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
			h := object.NewHash()
			switch {
			case blk != nil:
				if len(args) != 0 {
					raise("ArgumentError", "wrong number of arguments (given %d, expected 0)", len(args))
				}
				h.DefaultProc = blk
			case len(args) == 1:
				h.Default = args[0]
			case len(args) > 1:
				raise("ArgumentError", "wrong number of arguments (given %d, expected 0..1)", len(args))
			}
			return h
		}}
	vm.cHash.define("[]", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		h := self.(*object.Hash)
		if v, ok := h.Get(args[0]); ok {
			return v
		}
		return vm.hashDefault(h, args[0])
	})
	vm.cHash.define("[]=", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		self.(*object.Hash).Set(args[0], args[1])
		return args[1]
	})
	vm.cHash.define("length", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(self.(*object.Hash).Len())
	})
	vm.cHash.define("size", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(self.(*object.Hash).Len())
	})
	vm.cHash.define("empty?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self.(*object.Hash).Len() == 0)
	})
	hashKeyP := func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		_, ok := self.(*object.Hash).Get(args[0])
		return object.Bool(ok)
	}
	vm.cHash.define("key?", hashKeyP)
	vm.cHash.define("has_key?", hashKeyP)
	vm.cHash.define("include?", hashKeyP)
	vm.cHash.define("keys", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		h := self.(*object.Hash)
		ks := make([]object.Value, len(h.Keys))
		copy(ks, h.Keys)
		return &object.Array{Elems: ks}
	})
	vm.cHash.define("values", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		h := self.(*object.Hash)
		vs := make([]object.Value, 0, len(h.Keys))
		for _, k := range h.Keys {
			v, _ := h.Get(k)
			vs = append(vs, v)
		}
		return &object.Array{Elems: vs}
	})
	vm.cHash.define("each", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (each)")
		}
		h := self.(*object.Hash)
		for _, k := range h.Keys {
			v, _ := h.Get(k)
			vm.callBlock(blk, []object.Value{hashPair(k, v)})
		}
		return h
	})
	vm.cHash.methods["each_pair"] = vm.cHash.methods["each"]
	vm.cHash.define("merge", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		h := self.(*object.Hash)
		other, ok := args[0].(*object.Hash)
		if !ok {
			raise("TypeError", "no implicit conversion into Hash")
		}
		out := object.NewHash()
		for _, k := range h.Keys {
			v, _ := h.Get(k)
			out.Set(k, v)
		}
		for _, k := range other.Keys {
			v, _ := other.Get(k)
			out.Set(k, v)
		}
		return out
	})
	vm.cHash.define("merge!", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		h := self.(*object.Hash)
		other, ok := args[0].(*object.Hash)
		if !ok {
			raise("TypeError", "no implicit conversion into Hash")
		}
		for _, k := range other.Keys {
			v, _ := other.Get(k)
			h.Set(k, v)
		}
		return h
	})
	vm.cHash.define("slice", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		h := self.(*object.Hash)
		out := object.NewHash()
		for _, k := range args {
			if v, ok := h.Get(k); ok {
				out.Set(k, v)
			}
		}
		return out
	})
	vm.cHash.define("except", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		h := self.(*object.Hash)
		drop := map[object.Value]bool{}
		for _, k := range args {
			drop[k] = true
		}
		out := object.NewHash()
		for _, k := range h.Keys {
			if !drop[k] {
				v, _ := h.Get(k)
				out.Set(k, v)
			}
		}
		return out
	})
	vm.cHash.define("fetch", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if v, ok := self.(*object.Hash).Get(args[0]); ok {
			return v
		}
		if len(args) > 1 {
			return args[1]
		}
		raise("KeyError", "key not found: %s", args[0].Inspect())
		return object.NilV
	})
	vm.cHash.define("dig", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.digValue(self, args)
	})
	vm.cHash.define("values_at", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		h := self.(*object.Hash)
		out := make([]object.Value, len(args))
		for i, k := range args {
			v, _ := h.Get(k)
			out[i] = v
		}
		return &object.Array{Elems: out}
	})
	vm.cHash.define("transform_values", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (transform_values)")
		}
		h := self.(*object.Hash)
		out := object.NewHash()
		for _, k := range h.Keys {
			v, _ := h.Get(k)
			out.Set(k, vm.callBlock(blk, []object.Value{v}))
		}
		return out
	})
	vm.cHash.define("transform_keys", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (transform_keys)")
		}
		h := self.(*object.Hash)
		out := object.NewHash()
		for _, k := range h.Keys {
			v, _ := h.Get(k)
			out.Set(vm.callBlock(blk, []object.Value{k}), v)
		}
		return out
	})
	vm.cHash.define("invert", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		h := self.(*object.Hash)
		out := object.NewHash()
		for _, k := range h.Keys {
			v, _ := h.Get(k)
			out.Set(v, k)
		}
		return out
	})
	vm.cHash.define("to_h", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return self
	})
	vm.cHash.define("store", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		self.(*object.Hash).Set(args[0], args[1])
		return args[1]
	})
	vm.cHash.define("delete", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		v, _ := self.(*object.Hash).Delete(args[0])
		return v
	})
	hashHasValue := func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		h := self.(*object.Hash)
		for _, k := range h.Keys {
			v, _ := h.Get(k)
			if valueEqual(v, args[0]) {
				return object.True
			}
		}
		return object.False
	}
	vm.cHash.define("has_value?", hashHasValue)
	vm.cHash.define("value?", hashHasValue)
	// select/reject return a Hash (unlike Enumerable's Array forms), so they are
	// native rather than inherited.
	vm.cHash.define("select", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (select)")
		}
		h := self.(*object.Hash)
		out := object.NewHash()
		for _, k := range h.Keys {
			v, _ := h.Get(k)
			if vm.callBlock(blk, []object.Value{hashPair(k, v)}).Truthy() {
				out.Set(k, v)
			}
		}
		return out
	})
	vm.cHash.define("reject", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (reject)")
		}
		h := self.(*object.Hash)
		out := object.NewHash()
		for _, k := range h.Keys {
			v, _ := h.Get(k)
			if !vm.callBlock(blk, []object.Value{hashPair(k, v)}).Truthy() {
				out.Set(k, v)
			}
		}
		return out
	})

	// Range.
	vm.cRange.define("begin", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return self.(*object.Range).Lo
	})
	vm.cRange.define("first", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		r := self.(*object.Range)
		if len(args) == 0 {
			return r.Lo
		}
		n := int(intArg(args[0]))
		if n < 0 {
			raise("ArgumentError", "negative array size")
		}
		// An endless range generates its first n elements directly; a bounded one
		// caps the count to its materialised size.
		if _, isNil := r.Hi.(object.Nil); isNil {
			lo, ok := r.Lo.(object.Integer)
			if !ok {
				raise("TypeError", "can't iterate from %s", r.Lo.Inspect())
			}
			out := make([]object.Value, n)
			for i := range out {
				out[i] = object.Integer(int64(lo) + int64(i))
			}
			return &object.Array{Elems: out}
		}
		elems := rangeElems(r)
		n = clampCount(int64(n), len(elems))
		out := make([]object.Value, n)
		copy(out, elems[:n])
		return &object.Array{Elems: out}
	})
	vm.cRange.define("end", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return self.(*object.Range).Hi
	})
	vm.cRange.define("last", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		r := self.(*object.Range)
		if _, isNil := r.Hi.(object.Nil); isNil {
			raise("RangeError", "cannot get the last element of endless range")
		}
		if len(args) == 0 {
			return r.Hi
		}
		elems := rangeElems(r)
		n := clampCount(intArg(args[0]), len(elems))
		out := make([]object.Value, n)
		copy(out, elems[len(elems)-n:])
		return &object.Array{Elems: out}
	})
	vm.cRange.define("exclude_end?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self.(*object.Range).Exclusive)
	})
	rangeCover := func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		r := self.(*object.Range)
		v := args[0]
		// cover? is comparison-based: an incomparable member is simply not
		// covered (Ruby returns false rather than raising). A nil bound is open.
		if _, isNil := r.Lo.(object.Nil); !isNil {
			lc, lok := rangeCmp(v, r.Lo)
			if !lok || lc < 0 {
				return object.False
			}
		}
		if _, isNil := r.Hi.(object.Nil); isNil {
			return object.True
		}
		hc, hok := rangeCmp(v, r.Hi)
		if !hok {
			return object.False
		}
		if r.Exclusive {
			return object.Bool(hc < 0)
		}
		return object.Bool(hc <= 0)
	}
	vm.cRange.define("include?", rangeCover)
	vm.cRange.define("cover?", rangeCover)
	vm.cRange.define("member?", rangeCover)
	vm.cRange.define("===", rangeCover)
	vm.cRange.define("min", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		r := self.(*object.Range)
		lo, _, _ := rangeInts(r)
		if rangeSize(r) == 0 {
			return object.NilV
		}
		return object.Integer(lo)
	})
	vm.cRange.define("max", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		r := self.(*object.Range)
		_, hi, _ := rangeInts(r)
		if rangeSize(r) == 0 {
			return object.NilV
		}
		if r.Exclusive {
			return object.Integer(hi - 1)
		}
		return object.Integer(hi)
	})
	rangeSizeFn := func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(rangeSize(self.(*object.Range)))
	}
	vm.cRange.define("size", rangeSizeFn)
	vm.cRange.define("count", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		// Bare count is the range size; with a block or argument it counts
		// matching elements (Enumerable#count).
		if blk == nil && len(args) == 0 {
			return rangeSizeFn(vm, self, args, blk)
		}
		arr := vm.send(self, "to_a", nil, nil).(*object.Array)
		var n int64
		for _, e := range arr.Elems {
			if blk != nil {
				if vm.callBlock(blk, []object.Value{e}).Truthy() {
					n++
				}
			} else if valueEqual(e, args[0]) {
				n++
			}
		}
		return object.Integer(n)
	})
	vm.cRange.define("to_a", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return &object.Array{Elems: rangeElems(self.(*object.Range))}
	})
	// take(n) mirrors first(n) (it works on endless ranges); drop(n) needs the
	// full materialised range, so it is bounded only.
	vm.cRange.define("take", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		r := self.(*object.Range)
		n := int(intArg(args[0]))
		if n < 0 {
			raise("ArgumentError", "attempt to take negative size")
		}
		if _, isNil := r.Hi.(object.Nil); isNil {
			lo, ok := r.Lo.(object.Integer)
			if !ok {
				raise("TypeError", "can't iterate from %s", r.Lo.Inspect())
			}
			out := make([]object.Value, n)
			for i := range out {
				out[i] = object.Integer(int64(lo) + int64(i))
			}
			return &object.Array{Elems: out}
		}
		elems := rangeElems(r)
		n = clampCount(int64(n), len(elems))
		out := make([]object.Value, n)
		copy(out, elems[:n])
		return &object.Array{Elems: out}
	})
	vm.cRange.define("drop", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		elems := rangeElems(self.(*object.Range))
		n := int(intArg(args[0]))
		if n < 0 {
			raise("ArgumentError", "attempt to drop negative size")
		}
		if n > len(elems) {
			n = len(elems)
		}
		out := make([]object.Value, len(elems)-n)
		copy(out, elems[n:])
		return &object.Array{Elems: out}
	})
	vm.cRange.define("each", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (each)")
		}
		r := self.(*object.Range)
		for _, e := range rangeElems(r) {
			vm.callBlock(blk, []object.Value{e})
		}
		return r
	})
	vm.cRange.define("map", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (map)")
		}
		elems := rangeElems(self.(*object.Range))
		out := make([]object.Value, len(elems))
		for i, e := range elems {
			out[i] = vm.callBlock(blk, []object.Value{e})
		}
		return &object.Array{Elems: out}
	})

	// Integer methods.
	// intOf coerces a receiver to int64; a Bignum is genuinely out of range for
	// the methods that need a machine int (raising rather than panicking).
	intOf := func(self object.Value) int64 {
		if i, ok := self.(object.Integer); ok {
			return int64(i)
		}
		raise("RangeError", "bignum too big to convert into `long'")
		return 0
	}
	vm.cInteger.define("abs", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NormInt(new(big.Int).Abs(bigVal(self)))
	})
	vm.cInteger.define("even?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(bigVal(self).Bit(0) == 0)
	})
	vm.cInteger.define("odd?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(bigVal(self).Bit(0) == 1)
	})
	vm.cInteger.define("zero?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(bigVal(self).Sign() == 0)
	})
	vm.cInteger.define("positive?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(bigVal(self).Sign() > 0)
	})
	vm.cInteger.define("negative?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(bigVal(self).Sign() < 0)
	})
	intSucc := func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NormInt(new(big.Int).Add(bigVal(self), big.NewInt(1)))
	}
	vm.cInteger.define("succ", intSucc)
	vm.cInteger.define("next", intSucc)
	vm.cInteger.define("pred", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NormInt(new(big.Int).Sub(bigVal(self), big.NewInt(1)))
	})
	vm.cInteger.define("to_i", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return self
	})
	vm.cInteger.define("to_int", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return self
	})
	vm.cInteger.define("to_f", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		f, _ := toFloat(self)
		return object.Float(f)
	})
	vm.cInteger.define("to_s", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		base := int64(10)
		if len(args) > 0 {
			base = intArg(args[0])
		}
		if base < 2 || base > 36 {
			raise("ArgumentError", "invalid radix %d", base)
		}
		return object.NewString(bigVal(self).Text(int(base)))
	})
	// Bitwise / shift operators (arbitrary precision via big.Int, so a left shift
	// promotes to a Bignum and bitwise ops work on Bignums too).
	vm.cInteger.define("<<", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return shiftInt(bigVal(self), intArg(args[0]))
	})
	vm.cInteger.define(">>", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return shiftInt(bigVal(self), -intArg(args[0]))
	})
	vm.cInteger.define("&", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NormInt(new(big.Int).And(bigVal(self), bigArg(args[0])))
	})
	vm.cInteger.define("|", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NormInt(new(big.Int).Or(bigVal(self), bigArg(args[0])))
	})
	vm.cInteger.define("^", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NormInt(new(big.Int).Xor(bigVal(self), bigArg(args[0])))
	})
	vm.cInteger.define("~", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NormInt(new(big.Int).Not(bigVal(self)))
	})
	vm.cInteger.define("gcd", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Integer(gcdInt(intOf(self), intArg(args[0])))
	})
	vm.cInteger.define("lcm", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a, b := intOf(self), intArg(args[0])
		if a == 0 || b == 0 {
			return object.Integer(0)
		}
		return object.Integer(absInt(a / gcdInt(a, b) * b))
	})
	vm.cInteger.define("bit_length", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		n := intOf(self)
		if n < 0 {
			n = ^n
		}
		var c int64
		for n > 0 {
			c++
			n >>= 1
		}
		return object.Integer(c)
	})
	vm.cInteger.define("divmod", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a, b := intOf(self), intArg(args[0])
		if b == 0 {
			raise("ZeroDivisionError", "divided by 0")
		}
		return &object.Array{Elems: []object.Value{object.Integer(floorDiv(a, b)), object.Integer(floorMod(a, b))}}
	})
	vm.cInteger.define("digits", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		n := intOf(self)
		base := int64(10)
		if len(args) > 0 {
			base = intArg(args[0])
			if base < 2 {
				raise("ArgumentError", "invalid radix %d", base)
			}
		}
		if n < 0 {
			raise("Math::DomainError", "out of domain")
		}
		if n == 0 {
			return &object.Array{Elems: []object.Value{object.Integer(0)}}
		}
		var out []object.Value
		for n > 0 {
			out = append(out, object.Integer(n%base))
			n /= base
		}
		return &object.Array{Elems: out}
	})
	vm.cInteger.define("chr", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		n := intOf(self)
		if n < 0 || n > 255 {
			raise("RangeError", "%d out of char range", n)
		}
		return object.NewString(string([]byte{byte(n)}))
	})
	vm.cInteger.define("upto", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (upto)")
		}
		for i := intOf(self); i <= intArg(args[0]); i++ {
			vm.callBlock(blk, []object.Value{object.Integer(i)})
		}
		return self
	})
	vm.cInteger.define("downto", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (downto)")
		}
		for i := intOf(self); i >= intArg(args[0]); i-- {
			vm.callBlock(blk, []object.Value{object.Integer(i)})
		}
		return self
	})

	// Float methods.
	floatOf := func(self object.Value) float64 { return float64(self.(object.Float)) }
	vm.cFloat.define("abs", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Float(math.Abs(floatOf(self)))
	})
	vm.cFloat.define("zero?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(floatOf(self) == 0)
	})
	vm.cFloat.define("positive?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(floatOf(self) > 0)
	})
	vm.cFloat.define("negative?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(floatOf(self) < 0)
	})
	vm.cFloat.define("to_f", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return self
	})
	vm.cFloat.define("to_i", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(int64(floatOf(self)))
	})
	vm.cFloat.define("to_int", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(int64(floatOf(self)))
	})
	vm.cFloat.define("ceil", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(int64(math.Ceil(floatOf(self))))
	})
	vm.cFloat.define("floor", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(int64(math.Floor(floatOf(self))))
	})
	vm.cFloat.define("round", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		f := floatOf(self)
		ndigits := 0
		if len(args) > 0 {
			ndigits = int(intArg(args[0]))
		}
		// ndigits > 0 rounds to that many decimals and stays a Float; ndigits <= 0
		// rounds to an integer (or a power of ten) and returns an Integer — MRI's
		// Float#round contract.
		pow := math.Pow(10, float64(ndigits))
		r := math.Round(f*pow) / pow
		if ndigits > 0 {
			return object.Float(r)
		}
		return object.Integer(int64(r))
	})
	vm.cFloat.define("nan?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(math.IsNaN(floatOf(self)))
	})
	vm.cFloat.define("finite?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		f := floatOf(self)
		return object.Bool(!math.IsInf(f, 0) && !math.IsNaN(f))
	})
	vm.cFloat.define("infinite?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		f := floatOf(self)
		if math.IsInf(f, 1) {
			return object.Integer(1)
		}
		if math.IsInf(f, -1) {
			return object.Integer(-1)
		}
		return object.NilV
	})

	// Class.
	vm.cClass.define("new", nativeNew)

	// Integer#times — the first block-driven iterator.
	vm.cInteger.define("times", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (times)")
		}
		n := int64(self.(object.Integer))
		for i := int64(0); i < n; i++ {
			vm.callBlock(blk, []object.Value{object.Integer(i)})
		}
		return self
	})

	vm.installRegexp()
	setupStruct(vm)
}

// nativeNew allocates an instance of the receiver class and runs initialize,
// forwarding any block.
func nativeNew(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
	class := self.(*RClass)
	obj := &RObject{class: class, ivars: map[string]object.Value{}}
	vm.send(obj, "initialize", args, blk)
	return obj
}

// hashDefault returns the value a missing key reads as: the default proc's
// result (called with the hash and key), else the static default, else nil.
func (vm *VM) hashDefault(h *object.Hash, key object.Value) object.Value {
	if h.DefaultProc != nil {
		return vm.callBlock(h.DefaultProc.(*Proc), []object.Value{h, key})
	}
	if h.Default != nil {
		return h.Default
	}
	return object.NilV
}

// intArg coerces an argument used as an array index to int64, or raises.
func intArg(v object.Value) int64 {
	if i, ok := v.(object.Integer); ok {
		return int64(i)
	}
	raise("TypeError", "no implicit conversion of %s into Integer", v.Inspect())
	return 0
}

// clampCount validates a `first(n)`/`last(n)` count: it must be non-negative
// (Ruby raises ArgumentError otherwise) and is capped to max so callers can
// slice safely.
func clampCount(n int64, max int) int {
	if n < 0 {
		raise("ArgumentError", "negative array size")
	}
	if n > int64(max) {
		return max
	}
	return int(n)
}

// arrayIndex normalizes a (possibly negative) index and reports whether it is in
// range.
func arrayIndex(a *object.Array, i int64) (int, bool) {
	n := int64(len(a.Elems))
	if i < 0 {
		i += n
	}
	if i < 0 || i >= n {
		return 0, false
	}
	return int(i), true
}

func nativePuts(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
	if len(args) == 0 {
		fmt.Fprintln(vm.out)
		return object.NilV
	}
	for _, a := range args {
		putsValue(vm, a)
	}
	return object.NilV
}

// putsValue prints one value the way Kernel#puts does: arrays are flattened (an
// empty array prints nothing), everything else prints its to_s plus a newline.
func putsValue(vm *VM, v object.Value) {
	if arr, ok := v.(*object.Array); ok {
		for _, e := range arr.Elems {
			putsValue(vm, e)
		}
		return
	}
	// puts does not double a trailing newline already present in the string.
	if s := v.ToS(); strings.HasSuffix(s, "\n") {
		fmt.Fprint(vm.out, s)
	} else {
		fmt.Fprintln(vm.out, s)
	}
}

func nativePrint(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
	for _, a := range args {
		fmt.Fprint(vm.out, a.ToS())
	}
	return object.NilV
}

func nativeP(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
	for _, a := range args {
		fmt.Fprintln(vm.out, a.Inspect())
	}
	switch len(args) {
	case 0:
		return object.NilV
	case 1:
		return args[0]
	default:
		return object.NilV // Ruby returns the args array; arrays arrive in Phase 2
	}
}

// wsCutset is the whitespace stripped by String#strip and friends, matching
// Ruby (space, tab, newline, carriage return, form feed, vertical tab, NUL).
const wsCutset = " \t\n\r\f\v\x00"

func capitalizeStr(s string) string {
	if s == "" {
		return ""
	}
	r := []rune(strings.ToLower(s))
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

func swapcaseStr(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		switch {
		case unicode.IsUpper(r):
			out = append(out, unicode.ToLower(r))
		case unicode.IsLower(r):
			out = append(out, unicode.ToUpper(r))
		default:
			out = append(out, r)
		}
	}
	return string(out)
}

func reverseStr(s string) string {
	r := []rune(s)
	for i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 {
		r[i], r[j] = r[j], r[i]
	}
	return string(r)
}

// chompStr removes one trailing line ending (\r\n, \n, or \r), as in Ruby.
func chompStr(s string) string {
	if strings.HasSuffix(s, "\r\n") {
		return s[:len(s)-2]
	}
	if strings.HasSuffix(s, "\n") || strings.HasSuffix(s, "\r") {
		return s[:len(s)-1]
	}
	return s
}

// chopStr removes the last character (\r\n counts as one), as in Ruby.
func chopStr(s string) string {
	if strings.HasSuffix(s, "\r\n") {
		return s[:len(s)-2]
	}
	r := []rune(s)
	if len(r) == 0 {
		return ""
	}
	return string(r[:len(r)-1])
}

// parseLeadingInt mimics String#to_i: optional whitespace and sign, then digits;
// 0 when there is no leading integer.
func parseLeadingInt(s string) int64 {
	s = strings.TrimLeft(s, wsCutset)
	i := 0
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		i++
	}
	j := i
	for j < len(s) && s[j] >= '0' && s[j] <= '9' {
		j++
	}
	if j == i {
		return 0
	}
	n, err := strconv.ParseInt(s[:j], 10, 64)
	if err != nil {
		return 0
	}
	return n
}

// parseLeadingFloat mimics String#to_f: parse the longest leading float, 0.0 if
// none.
func parseLeadingFloat(s string) float64 {
	s = strings.TrimLeft(s, wsCutset)
	i := 0
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		i++
	}
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i < len(s) && s[i] == '.' {
		i++
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
	}
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		j := i + 1
		if j < len(s) && (s[j] == '+' || s[j] == '-') {
			j++
		}
		k := j
		for k < len(s) && s[k] >= '0' && s[k] <= '9' {
			k++
		}
		if k > j {
			i = k
		}
	}
	f, err := strconv.ParseFloat(s[:i], 64)
	if err != nil {
		return 0
	}
	return f
}

// stringIndex implements String#[]: s[i], s[i, len], and s[range], all
// rune-indexed, returning nil for an out-of-range start.
func stringIndex(s string, args []object.Value) object.Value {
	r := []rune(s)
	n := len(r)
	if len(args) == 2 {
		start := normIndex(intArg(args[0]), n)
		length := intArg(args[1])
		if start < 0 || start > n || length < 0 {
			return object.NilV
		}
		end := start + int(length)
		if end > n {
			end = n
		}
		return object.NewString(string(r[start:end]))
	}
	if rng, ok := args[0].(*object.Range); ok {
		start, length, ok := sliceRange(n, rng)
		if !ok {
			return object.NilV
		}
		return object.NewString(string(r[start : start+length]))
	}
	i := normIndex(intArg(args[0]), n)
	if i < 0 || i >= n {
		return object.NilV
	}
	return object.NewString(string(r[i]))
}

// sliceRange resolves a Range against a collection of length n into a start
// index and length. Beginless/endless bounds (nil) default to 0 and n. ok is
// false when the start is out of range (Ruby returns nil).
func sliceRange(n int, r *object.Range) (int, int, bool) {
	lo := 0
	if _, isNil := r.Lo.(object.Nil); !isNil {
		lo = normIndex(intArg(r.Lo), n)
		if lo < 0 || lo > n {
			return 0, 0, false
		}
	}
	hi := n
	if _, isNil := r.Hi.(object.Nil); !isNil {
		hi = normIndex(intArg(r.Hi), n)
		if !r.Exclusive {
			hi++
		}
		if hi > n {
			hi = n
		}
	}
	if hi < lo {
		hi = lo
	}
	return lo, hi - lo, true
}

// normIndex resolves a possibly-negative index against length n (no clamping of
// the upper bound; callers range-check).
func normIndex(i int64, n int) int {
	if i < 0 {
		return int(i) + n
	}
	return int(i)
}

// checkFrozen raises FrozenError when a mutator is applied to a frozen string.
func checkFrozen(s *object.String) {
	if s.Frozen {
		raise("FrozenError", "can't modify frozen String: %s", s.Inspect())
	}
}

// strAppendBytes is the byte contribution of a `<<`/concat/prepend argument: a
// String contributes its bytes, an Integer its UTF-8 code point.
func strAppendBytes(a object.Value) []byte {
	switch v := a.(type) {
	case *object.String:
		return v.B
	case object.Integer:
		return []byte(string(rune(v)))
	}
	raise("TypeError", "no implicit conversion of %s into String", classNameOf(a))
	return nil
}

// strBang applies a pure transform to the receiver in place. As a Ruby bang
// method it returns the (mutated) receiver when the content changed, else nil.
func strBang(self object.Value, fn func(string) string) object.Value {
	s := self.(*object.String)
	checkFrozen(s)
	out := fn(s.Str())
	if out == string(s.B) {
		return object.NilV
	}
	s.B = []byte(out)
	return s
}

// squeezeStr collapses each run of identical bytes to a single byte.
func squeezeStr(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if i > 0 && s[i] == s[i-1] {
			continue
		}
		out = append(out, s[i])
	}
	return string(out)
}

// strSubBang backs String#sub!/#gsub!: it applies the same substitution as
// sub/gsub and writes the result back, returning the receiver when it changed
// and nil otherwise.
func (vm *VM) strSubBang(self object.Value, args []object.Value, blk *Proc, global bool) object.Value {
	s := self.(*object.String)
	checkFrozen(s)
	res := vm.stringSub(s.Str(), args, blk, global).(*object.String)
	if string(res.B) == string(s.B) {
		return object.NilV
	}
	s.B = res.B
	return s
}

// stringIndexAssign backs String#[]=: it replaces the indexed slice (an index,
// a start+length, or a Range) with the replacement string and returns the
// replacement (Ruby's result for an assignment).
func stringIndexAssign(s *object.String, args []object.Value) object.Value {
	checkFrozen(s)
	r := []rune(s.Str())
	n := len(r)
	rhs := args[len(args)-1]
	repl := strArg(rhs)
	start, length := stringAssignSpan(args, n)
	out := append(append(append([]rune{}, r[:start]...), []rune(repl)...), r[start+length:]...)
	s.B = []byte(string(out))
	return rhs
}

// stringAssignSpan resolves the [index] / [start, len] / [range] target of a
// String#[]= into a (start, length) span, raising the IndexError/RangeError
// Ruby raises for an out-of-range target.
func stringAssignSpan(args []object.Value, n int) (start, length int) {
	if len(args) == 3 {
		start = normIndex(intArg(args[0]), n)
		length = int(intArg(args[1]))
		if start < 0 || start > n {
			raise("IndexError", "index %d out of string", intArg(args[0]))
		}
		if length < 0 {
			raise("IndexError", "negative length %d", length)
		}
		if start+length > n {
			length = n - start
		}
		return start, length
	}
	if rng, ok := args[0].(*object.Range); ok {
		st, ln, ok := sliceRange(n, rng)
		if !ok {
			raise("RangeError", "%s out of range", rng.Inspect())
		}
		return st, ln
	}
	start = normIndex(intArg(args[0]), n)
	if start < 0 || start >= n {
		raise("IndexError", "index %d out of string", intArg(args[0]))
	}
	return start, 1
}

// stringSliceBang backs String#slice!: it removes the indexed slice from the
// receiver and returns it (nil when the index does not select anything).
func stringSliceBang(s *object.String, args []object.Value) object.Value {
	checkFrozen(s)
	r := []rune(s.Str())
	n := len(r)
	start, length, ok := sliceSpan(args, n)
	if !ok {
		return object.NilV
	}
	removed := string(r[start : start+length])
	out := append(append([]rune{}, r[:start]...), r[start+length:]...)
	s.B = []byte(string(out))
	return object.NewString(removed)
}

// sliceSpan resolves the [index] / [start, len] / [range] argument of slice!
// into a (start, length) span, reporting ok=false for an out-of-range selector
// (slice! then returns nil rather than raising).
func sliceSpan(args []object.Value, n int) (start, length int, ok bool) {
	if len(args) == 2 {
		start = normIndex(intArg(args[0]), n)
		length = int(intArg(args[1]))
		if start < 0 || start > n || length < 0 {
			return 0, 0, false
		}
		if start+length > n {
			length = n - start
		}
		return start, length, true
	}
	if rng, isR := args[0].(*object.Range); isR {
		return sliceRange(n, rng)
	}
	start = normIndex(intArg(args[0]), n)
	if start < 0 || start >= n {
		return 0, 0, false
	}
	return start, 1, true
}

// strArg coerces a String argument, raising TypeError otherwise.
// arrArg coerces an argument to an *Array, raising TypeError otherwise.
func arrArg(v object.Value) *object.Array {
	if a, ok := v.(*object.Array); ok {
		return a
	}
	raise("TypeError", "no implicit conversion of %s into Array", v.Inspect())
	return nil
}

func strArg(v object.Value) string {
	if s, ok := v.(*object.String); ok {
		return s.Str()
	}
	raise("TypeError", "no implicit conversion of %s into String", v.Inspect())
	return ""
}

// hashPair builds the [key, value] array Hash#each yields; block auto-splat then
// binds a two-parameter block element-wise, while a one-parameter block sees the
// pair (matching Ruby).
func hashPair(k, v object.Value) *object.Array {
	return &object.Array{Elems: []object.Value{k, v}}
}

// spaceship compares two values via their <=> method, raising ArgumentError if
// they are incomparable (a nil result).
func (vm *VM) spaceship(a, b object.Value) int {
	r := vm.send(a, "<=>", []object.Value{b}, nil)
	n, ok := r.(object.Integer)
	if !ok {
		raise("ArgumentError", "comparison of %s with %s failed", vm.classOf(a).name, vm.classOf(b).name)
	}
	return int(n)
}

// arrayByExtreme implements min_by/max_by: the element whose block key is
// smallest (want=-1) or largest (want=1). nil for an empty array.
func (vm *VM) arrayByExtreme(a *object.Array, blk *Proc, name string, want int) object.Value {
	if blk == nil {
		raise("LocalJumpError", "no block given (%s)", name)
	}
	if len(a.Elems) == 0 {
		return object.NilV
	}
	best := a.Elems[0]
	bestKey := vm.callBlock(blk, []object.Value{best})
	for _, e := range a.Elems[1:] {
		k := vm.callBlock(blk, []object.Value{e})
		if sign(vm.spaceship(k, bestKey)) == want {
			best, bestKey = e, k
		}
	}
	return best
}

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	default:
		return 0
	}
}

// flattenDepth flattens nested arrays up to depth levels (-1 = fully).
func flattenDepth(elems []object.Value, depth int) []object.Value {
	var out []object.Value
	for _, e := range elems {
		if sub, ok := e.(*object.Array); ok && depth != 0 {
			out = append(out, flattenDepth(sub.Elems, depth-1)...)
		} else {
			out = append(out, e)
		}
	}
	return out
}

// joinArray renders an array as a string, recursively joining nested arrays.
func joinArray(a *object.Array, sep string) string {
	var b strings.Builder
	for i, e := range a.Elems {
		if i > 0 {
			b.WriteString(sep)
		}
		if sub, ok := e.(*object.Array); ok {
			b.WriteString(joinArray(sub, sep))
		} else {
			b.WriteString(e.ToS())
		}
	}
	return b.String()
}

// classArg coerces an argument expected to be a class/module, else TypeError.
func classArg(v object.Value) *RClass {
	if c, ok := v.(*RClass); ok {
		return c
	}
	raise("TypeError", "class or module required")
	return nil
}

// classIsA reports whether class c is, inherits from, or includes target.
func classIsA(c, target *RClass) bool {
	for ; c != nil; c = c.super {
		if c == target {
			return true
		}
		for _, m := range c.includes {
			if classIsA(m, target) {
				return true
			}
		}
	}
	return false
}

// excError builds the RubyError carrying a raised Ruby exception object.
func (vm *VM) excError(exc object.Value) RubyError {
	cls := vm.classOf(exc)
	msg := cls.name
	if m := getIvar(exc, "@message"); m != object.NilV {
		msg = m.ToS()
	}
	return RubyError{Class: cls.name, Message: msg, Obj: exc}
}

// nativeRaise implements Kernel#raise: a message string (RuntimeError), an
// exception class (instantiated), an exception instance (re-raised), or a
// class + message pair.
func nativeRaise(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
	switch len(args) {
	case 0:
		// Bare `raise` re-raises the exception currently being handled, else a
		// fresh RuntimeError.
		if vm.curExc != nil {
			panic(vm.excError(vm.curExc))
		}
		panic(vm.excError(vm.send(vm.consts["RuntimeError"].(*RClass), "new",
			[]object.Value{object.NewString("unhandled exception")}, nil)))
	case 1:
		switch a := args[0].(type) {
		case *object.String:
			panic(vm.excError(vm.send(vm.consts["RuntimeError"].(*RClass), "new", []object.Value{a}, nil)))
		case *RClass:
			panic(vm.excError(vm.send(a, "new", nil, nil)))
		case *RObject:
			panic(vm.excError(a))
		}
		raise("TypeError", "exception class/object expected")
		return object.NilV
	default:
		panic(vm.excError(vm.send(classArg(args[0]), "new", []object.Value{args[1]}, nil)))
	}
}

// digValue implements Hash#dig: walk nested Hashes/Arrays by successive keys,
// returning nil as soon as a step is missing.
func (vm *VM) digValue(cur object.Value, keys []object.Value) object.Value {
	for _, k := range keys {
		switch c := cur.(type) {
		case object.Nil:
			return object.NilV
		case *object.Hash:
			v, ok := c.Get(k)
			if !ok {
				return object.NilV
			}
			cur = v
		case *object.Array:
			if i, ok := arrayIndex(c, intArg(k)); ok {
				cur = c.Elems[i]
			} else {
				cur = object.NilV
			}
		default:
			raise("TypeError", "%s does not have #dig method", vm.classOf(cur).name)
		}
	}
	return cur
}

// expandCharSet expands a tr/count/delete character set, turning `a-z` ranges
// into their bytes (ASCII).
func expandCharSet(s string) []byte {
	var out []byte
	for i := 0; i < len(s); i++ {
		if i+2 < len(s) && s[i+1] == '-' {
			for ch := s[i]; ch <= s[i+2]; ch++ {
				out = append(out, ch)
			}
			i += 2
		} else {
			out = append(out, s[i])
		}
	}
	return out
}

// byteIndex returns the index of b in set, or -1.
func byteIndex(set []byte, b byte) int {
	for i, c := range set {
		if c == b {
			return i
		}
	}
	return -1
}

// padString implements ljust/rjust/center ('l'/'r'/'c'): pad s with the pad
// string (default " ") to a rune width. Extra padding for center goes right.
func padString(s string, args []object.Value, side byte) string {
	width := int(intArg(args[0]))
	pad := " "
	if len(args) > 1 {
		pad = strArg(args[1])
	}
	if pad == "" {
		raise("ArgumentError", "zero width padding")
	}
	n := utf8.RuneCountInString(s)
	if n >= width {
		return s
	}
	total := width - n
	switch side {
	case 'r':
		return makePad(pad, total) + s
	case 'c':
		left := total / 2
		return makePad(pad, left) + s + makePad(pad, total-left)
	default: // 'l'
		return s + makePad(pad, total)
	}
}

// makePad builds n runes from the (non-empty) pad string, repeating/truncating.
func makePad(pad string, n int) string {
	runes := []rune(pad)
	out := make([]rune, n)
	for i := 0; i < n; i++ {
		out[i] = runes[i%len(runes)]
	}
	return string(out)
}

// powNumeric implements ** / pow: integer base and non-negative integer
// exponent stay integer; a negative integer exponent or any float yields a
// float (no Rational in this phase).
func powNumeric(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
	// Integer#pow(exp, mod) is modular exponentiation: base**exp mod m.
	if len(args) > 1 {
		base, ok1 := object.BigOf(self)
		e, ok2 := object.BigOf(args[0])
		m, ok3 := object.BigOf(args[1])
		if !ok1 || !ok2 || !ok3 {
			raise("TypeError", "Integer#pow with a modulus requires integer arguments")
		}
		if e.Sign() < 0 {
			raise("RangeError", "Integer#pow() 1st argument cannot be negative when 2nd argument specified")
		}
		if m.Sign() == 0 {
			raise("ZeroDivisionError", "divided by 0")
		}
		return object.NormInt(new(big.Int).Exp(base, e, m))
	}
	if base, ok := object.BigOf(self); ok {
		if ei, ok := args[0].(object.Integer); ok {
			if ei < 0 {
				bf, _ := toFloat(self)
				return object.Float(math.Pow(bf, float64(ei)))
			}
			// Arbitrary-precision exponentiation, demoting if it fits int64.
			return object.NormInt(new(big.Int).Exp(base, big.NewInt(int64(ei)), nil))
		}
	}
	a, _ := toFloat(self)
	b, ok := toFloat(args[0])
	if !ok {
		raise("TypeError", "%s can't be coerced for **", args[0].Inspect())
	}
	return object.Float(math.Pow(a, b))
}

// absInt is the absolute value of an int64.
func absInt(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

// splitLines splits on "\n", keeping each separator attached to its line (Ruby
// String#lines semantics). An empty string yields no lines.
func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i+1])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

// defineAttrs installs reader and/or writer accessors on cls for each named
// attribute (the symbols/strings passed to attr_reader/writer/accessor).
func defineAttrs(cls *RClass, names []object.Value, reader, writer bool) {
	for _, n := range names {
		ivar := "@" + n.ToS()
		if reader {
			cls.define(n.ToS(), func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
				if v, ok := self.(*RObject).ivars[ivar]; ok {
					return v
				}
				return object.NilV
			})
		}
		if writer {
			cls.define(n.ToS()+"=", func(_ *VM, self object.Value, a []object.Value, _ *Proc) object.Value {
				self.(*RObject).ivars[ivar] = a[0]
				return a[0]
			})
		}
	}
}

// dupValue shallow-copies a value (Object#dup/#clone). Reference types get a
// fresh container with the same elements; immutable value types are their own
// copy.
func dupValue(v object.Value) object.Value {
	switch x := v.(type) {
	case *object.String:
		return x.Dup()
	case *object.Array:
		elems := make([]object.Value, len(x.Elems))
		copy(elems, x.Elems)
		return &object.Array{Elems: elems}
	case *object.Hash:
		h := object.NewHash()
		for _, k := range x.Keys {
			val, _ := x.Get(k)
			h.Set(k, val)
		}
		return h
	case *RObject:
		ivars := make(map[string]object.Value, len(x.ivars))
		for k, val := range x.ivars {
			ivars[k] = val
		}
		return &RObject{class: x.class, ivars: ivars}
	default:
		return v
	}
}

// isFrozen backs Object#frozen?: the immutable value types report frozen,
// everything mutable reports not-frozen (we do not track explicit freezes).
func isFrozen(v object.Value) bool {
	switch x := v.(type) {
	case object.Integer, object.Float, object.Symbol, object.Bool, object.Nil:
		return true
	case *object.String:
		return x.Frozen
	}
	return false
}

// arrayKeepIf mutates a in place, keeping the elements for which the block's
// truthiness equals keep (select!/reject!). It returns the array, or nil when
// nothing was removed (Ruby's "no change" signal).
func arrayKeepIf(vm *VM, a *object.Array, blk *Proc, keep bool) object.Value {
	var out []object.Value
	for _, e := range a.Elems {
		if vm.callBlock(blk, []object.Value{e}).Truthy() == keep {
			out = append(out, e)
		}
	}
	if len(out) == len(a.Elems) {
		return object.NilV
	}
	a.Elems = out
	return a
}

// bigVal returns an integer receiver (Integer or Bignum) as a *big.Int.
func bigVal(v object.Value) *big.Int {
	b, _ := object.BigOf(v)
	return b
}

// bigArg returns an integer argument as a *big.Int, raising TypeError when the
// argument is not an Integer/Bignum (as Ruby's bitwise operators do).
func bigArg(v object.Value) *big.Int {
	if b, ok := object.BigOf(v); ok {
		return b
	}
	raise("TypeError", "%s can't be coerced into Integer", classNameOf(v))
	return nil
}

// shiftInt shifts a left by n bits (right by -n when n is negative), promoting
// to a Bignum as needed and demoting a result that fits back into an Integer.
func shiftInt(a *big.Int, n int64) object.Value {
	if n >= 0 {
		return object.NormInt(new(big.Int).Lsh(a, uint(n)))
	}
	return object.NormInt(new(big.Int).Rsh(a, uint(-n)))
}

// gcdInt is the (non-negative) greatest common divisor by Euclid's algorithm.
func gcdInt(a, b int64) int64 {
	a, b = absInt(a), absInt(b)
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

// rubyEqual is the default Object#== : pointer identity for instances, and
// structural equality for the immutable value types.
func rubyEqual(a, b object.Value) bool {
	if ao, ok := a.(*RObject); ok {
		bo, ok := b.(*RObject)
		return ok && ao == bo
	}
	return valueEqual(a, b)
}

// spaceshipNumeric implements Integer#<=> and Float#<=>: -1/0/1 across the
// numeric tower, nil for a non-numeric argument.
func spaceshipNumeric(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
	// Compare two integers (Integer or Bignum) exactly; only fall back to float
	// when one side is a Float (where the precision loss is intrinsic).
	if ai, ok := self.(object.Integer); ok {
		if bi, ok := args[0].(object.Integer); ok {
			return object.Integer(int64(cmpInt64(int64(ai), int64(bi))))
		}
	}
	if ab, ok := object.BigOf(self); ok {
		if bb, ok := object.BigOf(args[0]); ok {
			return object.Integer(int64(ab.Cmp(bb)))
		}
	}
	a, _ := toFloat(self)
	b, ok := toFloat(args[0])
	if !ok {
		return object.NilV
	}
	return object.Integer(int64(cmpFloat(a, b)))
}

func cmpInt64(a, b int64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

func cmpFloat(a, b float64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

// rangeCmp orders two values for Range membership tests: numerics compare
// numerically, strings lexically; any other pairing is incomparable (ok=false,
// mirroring Ruby's <=> returning nil).
func rangeCmp(a, b object.Value) (ord int, ok bool) {
	if af, aok := toFloat(a); aok {
		if bf, bok := toFloat(b); bok {
			switch {
			case af < bf:
				return -1, true
			case af > bf:
				return 1, true
			default:
				return 0, true
			}
		}
		return 0, false
	}
	as, aok := a.(*object.String)
	bs, bok := b.(*object.String)
	if aok && bok {
		return strings.Compare(as.Str(), bs.Str()), true
	}
	return 0, false
}

// rangeInts extracts integer endpoints. ok is false when either endpoint is not
// an Integer (string/float ranges are not iterable in this phase).
func rangeInts(r *object.Range) (lo, hi int64, ok bool) {
	li, lok := r.Lo.(object.Integer)
	hi2, hok := r.Hi.(object.Integer)
	if !lok || !hok {
		return 0, 0, false
	}
	return int64(li), int64(hi2), true
}

// rangeSize is the element count of an integer range (0 if empty or
// non-integer), matching Ruby's Range#size.
func rangeSize(r *object.Range) int64 {
	lo, hi, ok := rangeInts(r)
	if !ok {
		raise("TypeError", "can't iterate from %s", r.Lo.Inspect())
	}
	n := hi - lo
	if !r.Exclusive {
		n++
	}
	if n < 0 {
		return 0
	}
	return n
}

// rangeElems materializes an integer range to a slice, raising TypeError on
// non-integer endpoints (Ruby: "can't iterate from String").
func rangeElems(r *object.Range) []object.Value {
	lo, hi, ok := rangeInts(r)
	if !ok {
		raise("TypeError", "can't iterate from %s", r.Lo.Inspect())
	}
	if r.Exclusive {
		hi--
	}
	if hi < lo {
		return nil
	}
	out := make([]object.Value, 0, hi-lo+1)
	for i := lo; i <= hi; i++ {
		out = append(out, object.Integer(i))
	}
	return out
}
