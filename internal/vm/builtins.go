package vm

import (
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
	// Top-level constants ARE Object's constants in Ruby; share the one table so
	// a bare top-level `X = 1` and `Object::X` refer to the same slot and so that
	// constant lookup terminating at Object reaches the top level.
	vm.consts = vm.cObject.consts
	vm.cModule = newClass("Module", vm.cObject)
	vm.cClass = newClass("Class", vm.cModule)
	cNumeric := newClass("Numeric", vm.cObject) // Integer/Float/Complex/Rational < Numeric
	vm.cInteger = newClass("Integer", cNumeric)
	vm.cFloat = newClass("Float", cNumeric)
	vm.cComplex = newClass("Complex", cNumeric)
	vm.cRational = newClass("Rational", cNumeric)
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

	// Kernel is a module included into Object — its methods are defined directly
	// on Object below, but modelling the module makes it appear in ancestors and
	// satisfies is_a?(Kernel)/Object.include?(Kernel), as in MRI.
	kernel := newClass("Kernel", nil)
	kernel.isModule = true
	vm.cObject.includes = append(vm.cObject.includes, kernel)

	for _, c := range []*RClass{
		vm.cBasicObject, vm.cObject, vm.cModule, vm.cClass, cNumeric, vm.cInteger,
		vm.cFloat, vm.cComplex, vm.cRational, vm.cString, vm.cSymbol, vm.cArray, vm.cHash, vm.cRange,
		vm.cProc, vm.cTrueClass, vm.cFalseClass, vm.cNilClass,
		vm.cRegexp, vm.cMatchData, kernel,
	} {
		vm.consts[c.name] = c
	}
	// Float::INFINITY / Float::NAN.
	vm.cFloat.consts["INFINITY"] = object.Float(math.Inf(1))
	vm.cFloat.consts["NAN"] = object.Float(math.NaN())

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
	vm.registerBinding()
	vm.registerRequire()
	vm.registerSingleton()
	vm.registerMethod()
	vm.registerModuleExtras()
	vm.registerReflection()
	vm.registerVersionConstants()
	vm.registerKernelIntrospection()
	vm.registerEncoding()
	vm.registerStringEncoding()
	vm.registerJSBridge() // browser DOM/Canvas access (wasm only; a no-op natively)
	vm.registerBase64()
	vm.registerPackUnpack()
	vm.registerSecureRandom()
	vm.registerDigest()
	vm.registerJSON()
	vm.registerMarshal()
	vm.registerRandom()

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
	vm.cProc.define("curry", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		p := self.(*Proc)
		need := p.arityVal()
		if need < 0 { // optional/splat parameters: the required-argument count
			need = -need - 1
		}
		if len(args) > 0 {
			need = int(intArg(args[0]))
		}
		return vm.curried(p, need, nil)
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
	vm.cObject.define("catch", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) (result object.Value) {
		if blk == nil {
			raise("LocalJumpError", "no block given (yield)")
		}
		// Default tag is a fresh object passed to the block, so `catch { |t| throw t }`
		// targets exactly this catch.
		tag := object.Value(&RObject{class: vm.cObject, ivars: map[string]object.Value{}})
		if len(args) > 0 {
			tag = args[0]
		}
		defer func() {
			if r := recover(); r != nil {
				// Tags match by identity (== on the interface): a Symbol by value, a
				// reference by pointer — exactly Ruby's equal?.
				if sig, ok := r.(throwSignal); ok && sig.tag == tag {
					result = sig.value
					return
				}
				panic(r)
			}
		}()
		return vm.callBlock(blk, []object.Value{tag})
	})
	vm.cObject.define("throw", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		var val object.Value = object.NilV
		if len(args) > 1 {
			val = args[1]
		}
		panic(throwSignal{tag: args[0], value: val})
	})
	vm.cObject.define("equal?", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		// Object identity: reference types compare by pointer, the immutable
		// value types by value (Go interface equality gives exactly this).
		return object.Bool(self == args[0])
	})
	vm.cObject.define("eql?", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		// Like ==, but with no numeric coercion (see valueEql); the value types
		// reach this through Object since none override it.
		return object.Bool(valueEql(self, args[0]))
	})
	objectIDFn := func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.objectID(self)
	}
	vm.cObject.define("object_id", objectIDFn)
	vm.cObject.define("__id__", objectIDFn)
	vm.cObject.define("hash", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(vm.hashValue(self))
	})
	vm.cObject.define("methods", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		c := vm.classOf(self)
		if o, ok := self.(*RObject); ok && o.singleton != nil {
			c = o.singleton // its super is the real class, so the walk picks up both
		}
		return &object.Array{Elems: vm.methodNames(c, true)}
	})
	vm.cObject.define("singleton_methods", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		// Default includes singleton methods inherited from the receiver's
		// superclasses (a class's class methods); a false argument restricts to the
		// receiver's own. For a plain object the singleton methods are those on its
		// per-object singleton class.
		all := len(args) == 0 || args[0].Truthy()
		return &object.Array{Elems: vm.singletonMethodNames(self, all)}
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
	exc("UncaughtThrowError", "ArgumentError")
	exc("NotImplementedError", "StandardError")
	exc("FrozenError", "RuntimeError")
	exc("IOError", "StandardError")
	exc("EOFError", "IOError")
	exc("RegexpError", "StandardError")
	exc("NoMatchingPatternError", "StandardError")
	exc("NoMatchingPatternKeyError", "NoMatchingPatternError")
	exc("Math::DomainError", "StandardError")
	// ScriptError / SyntaxError sit under Exception (NOT StandardError), so a bare
	// `rescue` does not catch them — matching MRI. eval raises SyntaxError.
	exc("ScriptError", "Exception")
	exc("SyntaxError", "ScriptError")
	exc("LoadError", "ScriptError")
	// Exceptions that sit directly under Exception (NOT StandardError), so a bare
	// `rescue` does not catch them — matching MRI. SystemExit additionally carries
	// an exit status (defined below).
	exc("NoMemoryError", "Exception")
	exc("SecurityError", "Exception")
	exc("SignalException", "Exception")
	exc("Interrupt", "SignalException")
	systemExit := exc("SystemExit", "Exception")
	// SystemExit#initialize accepts (status=0, message=nil) or (message); it stores
	// @status (the process exit code) and the message. SystemExit#status returns it.
	systemExit.define("initialize", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o := self.(*RObject)
		status := object.Value(object.Integer(0))
		switch {
		case len(args) >= 2:
			status = args[0]
			o.ivars["@message"] = object.NewString(args[1].ToS())
		case len(args) == 1:
			if i, ok := args[0].(object.Integer); ok {
				status = i
			} else {
				o.ivars["@message"] = object.NewString(args[0].ToS())
			}
		}
		o.ivars["@status"] = status
		return object.NilV
	})
	systemExit.define("status", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		if s := getIvar(self, "@status"); s != object.NilV {
			return s
		}
		return object.Integer(0)
	})
	systemExit.define("success?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		s, ok := getIvar(self, "@status").(object.Integer)
		return object.Bool(!ok || s == 0)
	})

	vm.registerFile()       // needs the exception hierarchy (Errno::ENOENT < StandardError)
	vm.registerIO()         // IO/StringIO + $stdout/$stderr/$stdin (needs IOError/EOFError)
	vm.registerDir()        // Dir (reuses the Errno module set up by registerFile)
	vm.registerTmpdir()     // Dir.tmpdir / Dir.mktmpdir (layers onto Dir; require "tmpdir")
	vm.registerProcess()    // Process module — identity + clock_gettime
	vm.registerOpenSSL()    // OpenSSL (real digest/HMAC/random + PKI/TLS shell); needs StandardError
	vm.registerNetHTTP()    // net/http + net/https loadable shell; needs StandardError
	vm.registerResolv()     // Resolv (real IPv4/IPv6 parse; DNS sockets stubbed); needs StandardError
	vm.registerTimeout()    // Timeout module (loadable shell); needs RuntimeError
	vm.registerYAML()       // YAML/Psych loadable shell; needs StandardError
	vm.registerFileUtils()  // FileUtils (real fs ops over os); needs Errno (registerFile)
	vm.registerGetoptLong() // GetoptLong loadable shell; needs StandardError
	vm.registerOptParse()   // optparse loadable shell (declares; parse raises); needs StandardError
	vm.registerRipper()     // ripper loadable shell (Ripper.sexp etc. raise); needs StandardError
	vm.registerSyslog()     // Syslog loadable shell (feature probe)
	vm.registerCGI()        // CGI.escape/unescape (real over net/url) + HTML helpers
	vm.registerMonitor()    // Monitor/MonitorMixin (single-thread synchronize); needs StandardError
	vm.registerZlib()       // needs the exception hierarchy (Zlib::Error < StandardError)
	vm.registerFiber()      // needs the exception hierarchy (FiberError < StandardError)
	vm.registerThread()     // needs StandardError/StopIteration (ThreadError, ClosedQueueError)

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

	// backtrace: the captured frame list (Array of String), or nil when the
	// exception has never been raised — matching MRI, which fills the backtrace in
	// at raise time, not at construction.
	cException.define("backtrace", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		if bt := getIvar(self, backtraceIvar); bt != object.NilV {
			return bt
		}
		return object.NilV
	})
	// backtrace_locations: best-effort — this VM has no Thread::Backtrace::Location
	// objects, so it returns the same String array as #backtrace (callers that only
	// stringify locations still work).
	cException.define("backtrace_locations", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		if bt := getIvar(self, backtraceIvar); bt != object.NilV {
			return bt
		}
		return object.NilV
	})
	// set_backtrace: replace the backtrace with a String, an Array of String, or
	// nil (clearing it). Anything else is a TypeError, as MRI.
	cException.define("set_backtrace", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		var v object.Value = object.NilV
		if len(args) > 0 {
			v = args[0]
		}
		setIvar(self, backtraceIvar, normalizeBacktrace(v))
		return getIvar(self, backtraceIvar)
	})
	// full_message: the MRI-shaped multi-line report — the first frame, the message
	// and class, then "\tfrom <frame>" for each remaining frame. With no captured
	// backtrace it degrades to just the detailed message (message + class).
	cException.define("full_message", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(vm.exceptionFullMessage(self))
	})
	// detailed_message: "<message> (<ClassName>)", the body full_message embeds.
	cException.define("detailed_message", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(vm.exceptionDetailedMessage(self))
	})

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
			base := 0 // no explicit base: auto-detect a 0x/0b/0o/0 prefix (and allow _)
			if len(args) > 1 {
				base = int(intArg(args[1]))
			}
			// Go's ParseInt only accepts a radix prefix (0x/0b/0o/0d) with base 0,
			// so strip a prefix that matches the explicit base, as MRI allows.
			n, err := strconv.ParseInt(stripRadixPrefix(strings.TrimSpace(v.Str()), base), base, 64)
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
	sendFn := func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		return vm.send(self, args[0].ToS(), args[1:], blk)
	}
	vm.cObject.define("send", sendFn)
	// __send__ is the can't-be-overridden alias of send; both ignore visibility.
	vm.cObject.define("__send__", sendFn)
	// public_send dispatches only public methods: a private/protected target
	// raises NoMethodError just as an explicit-receiver call would.
	vm.cObject.define("public_send", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		name := args[0].ToS()
		if m := vm.findMethod(self, name); m != nil {
			vm.checkVisibility(self, name, m, nil)
		}
		return vm.send(self, name, args[1:], blk)
	})
	vm.cObject.define("respond_to?", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		name := args[0].ToS()
		includePrivate := object.Value(object.False)
		if len(args) > 1 {
			includePrivate = args[1]
		}
		if m := vm.findMethod(self, name); m != nil {
			// A private or protected method answers respond_to? only when the second
			// argument (include_private) is truthy, matching MRI.
			if includePrivate.Truthy() || vm.sendVisibilityOf(self, name, m) == visPublic {
				return object.True
			}
			return object.False
		}
		// Fall back to respond_to_missing?(name, include_private): a truthy return
		// means the object answers the method dynamically (method_missing).
		if vm.findMethod(self, "respond_to_missing?") != nil {
			return object.Bool(vm.send(self, "respond_to_missing?", []object.Value{object.Symbol(name), includePrivate}, nil).Truthy())
		}
		return object.False
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
			bumpMethodSerial()
			// Hook: module.included(base), fired per included module if it defines
			// the hook (singleton method).
			if hook := lookupSMethod(mod, "included"); hook != nil {
				vm.invoke(hook, mod, []object.Value{target}, nil)
			}
		}
		return target
	})
	vm.cModule.define("prepend", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		target := self.(*RClass)
		for _, a := range args {
			mod := a.(*RClass)
			target.prepends = append(target.prepends, mod)
			bumpMethodSerial()
			// Hook: module.prepended(base), mirroring included.
			if hook := lookupSMethod(mod, "prepended"); hook != nil {
				vm.invoke(hook, mod, []object.Value{target}, nil)
			}
		}
		return target
	})
	vm.cModule.define("ancestors", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		anc := vm.ancestors(self.(*RClass))
		out := make([]object.Value, len(anc))
		for i, k := range anc {
			out[i] = k
		}
		return &object.Array{Elems: out}
	})
	vm.cModule.define("include?", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		mod, ok := args[0].(*RClass)
		if !ok {
			raise("TypeError", "wrong argument type %s (expected Module)", classNameOf(args[0]))
		}
		me := self.(*RClass)
		for _, k := range vm.ancestors(me) {
			if k == mod && k != me { // a module never includes itself
				return object.Bool(true)
			}
		}
		return object.Bool(false)
	})
	vm.cModule.define("name", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		if c := self.(*RClass); c.name != "" {
			return object.NewString(c.name)
		}
		return object.NilV // anonymous class/module
	})
	vm.cModule.define("instance_methods", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		all := len(args) == 0 || args[0].Truthy() // instance_methods(false) = own only
		return &object.Array{Elems: vm.methodNames(self.(*RClass), all)}
	})
	vm.cModule.define("const_get", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.scopedConst(self.(*RClass), constNameArg(args[0]))
	})
	vm.cModule.define("const_set", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		name := constNameArg(args[0])
		cls := self.(*RClass)
		// Top-level (Object) constants live in the flat namespace that a bare
		// constant reference reads; everything else is scoped to the class.
		if cls == vm.cObject {
			vm.consts[name] = args[1]
		} else {
			cls.consts[name] = args[1]
		}
		return args[1]
	})
	vm.cModule.define("const_defined?", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		name := constNameArg(args[0])
		for c := self.(*RClass); c != nil; c = c.super {
			if _, ok := c.consts[name]; ok {
				return object.True
			}
		}
		_, ok := vm.consts[name]
		return object.Bool(ok)
	})
	// Module#< <= > >= compare by the inheritance/inclusion hierarchy: A < B is
	// true if A is a proper descendant of B, false if a proper ancestor (or, for
	// <=/>=, equal), and nil when the two are unrelated.
	classCmp := func(self, other object.Value) object.Value {
		a := self.(*RClass)
		b, ok := other.(*RClass)
		if !ok {
			raise("TypeError", "compared with non class/module")
		}
		switch {
		case a == b:
			return object.Integer(0)
		case classIsA(a, b):
			return object.Integer(-1)
		case classIsA(b, a):
			return object.Integer(1)
		}
		return object.NilV
	}
	classCmpOp := func(want func(int) bool) NativeFn {
		return func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
			if c, ok := classCmp(self, args[0]).(object.Integer); ok {
				return object.Bool(want(int(c)))
			}
			return object.NilV
		}
	}
	vm.cModule.define("<", classCmpOp(func(c int) bool { return c < 0 }))
	vm.cModule.define("<=", classCmpOp(func(c int) bool { return c <= 0 }))
	vm.cModule.define(">", classCmpOp(func(c int) bool { return c > 0 }))
	vm.cModule.define(">=", classCmpOp(func(c int) bool { return c >= 0 }))
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
		cls := self.(*RClass)
		name := nameArg(args[0])
		// A Method / UnboundMethod second argument transplants that method's body
		// under the new name and owner (Ruby allows define_method(:m, other_method)).
		if len(args) > 1 {
			switch src := args[1].(type) {
			case *BoundMethod:
				cm := *src.m
				cm.name, cm.owner = name, cls
				cls.methods[name] = &cm
				bumpMethodSerial()
				return object.Symbol(name)
			case *UnboundMethod:
				cm := *src.m
				cm.name, cm.owner = name, cls
				cls.methods[name] = &cm
				bumpMethodSerial()
				return object.Symbol(name)
			}
		}
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
		cls.methods[name] = &Method{name: name, proc: body, owner: cls}
		bumpMethodSerial()
		return object.Symbol(name)
	})

	// Symbol.
	vm.cSymbol.define("to_sym", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return self
	})
	vm.cSymbol.define("intern", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return self // MRI: Symbol#intern is an alias of Symbol#to_sym (returns self)
	})
	symStr := func(self object.Value) string { return string(self.(object.Symbol)) }
	vm.cSymbol.define("<=>", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o, ok := args[0].(object.Symbol)
		if !ok { // incomparable with a non-Symbol
			return object.NilV
		}
		return object.Integer(int64(strings.Compare(symStr(self), string(o))))
	})
	symLen := func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(int64(utf8.RuneCountInString(symStr(self))))
	}
	vm.cSymbol.define("length", symLen)
	vm.cSymbol.define("size", symLen)
	vm.cSymbol.define("empty?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(symStr(self) == "")
	})
	vm.cSymbol.define("upcase", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Symbol(strings.ToUpper(symStr(self)))
	})
	vm.cSymbol.define("downcase", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Symbol(strings.ToLower(symStr(self)))
	})
	vm.cSymbol.define("capitalize", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Symbol(capitalizeStr(symStr(self)))
	})
	vm.cSymbol.define("swapcase", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Symbol(swapcaseStr(symStr(self)))
	})
	symSucc := func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Symbol(succString(symStr(self)))
	}
	vm.cSymbol.define("succ", symSucc)
	vm.cSymbol.define("next", symSucc)
	symIndex := func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return stringIndex(symStr(self), args) // [] / slice yield a String, like MRI
	}
	vm.cSymbol.define("[]", symIndex)
	vm.cSymbol.define("slice", symIndex)
	vm.cSymbol.define("start_with?", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		s := symStr(self)
		for _, a := range args {
			if strings.HasPrefix(s, strArg(a)) {
				return object.True
			}
		}
		return object.False
	})
	vm.cSymbol.define("end_with?", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		s := symStr(self)
		for _, a := range args {
			if strings.HasSuffix(s, strArg(a)) {
				return object.True
			}
		}
		return object.False
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
	strLen := func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		// A binary (ASCII-8BIT) string counts bytes; otherwise characters.
		s := self.(*object.String)
		if s.IsBinary() {
			return object.Integer(int64(len(s.B)))
		}
		return object.Integer(int64(utf8.RuneCountInString(string(s.B))))
	}
	vm.cString.define("length", strLen)
	vm.cString.define("size", strLen)
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
	vm.cString.define("casecmp", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o, ok := args[0].(*object.String)
		if !ok { // like <=>, a non-String operand compares to nil
			return object.NilV
		}
		return object.Integer(int64(strings.Compare(strings.ToLower(strOf(self)), strings.ToLower(o.Str()))))
	})
	vm.cString.define("casecmp?", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o, ok := args[0].(*object.String)
		if !ok {
			return object.NilV
		}
		return object.Bool(strings.EqualFold(strOf(self), o.Str()))
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
	succStr := func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(succString(strOf(self)))
	}
	vm.cString.define("succ", succStr)
	vm.cString.define("next", succStr)
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
			return enumFor(self, "each_line")
		}
		for _, seg := range splitLines(strOf(self)) {
			vm.callBlock(blk, []object.Value{object.NewString(seg)})
		}
		return self
	})
	vm.cString.define("each_char", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			return enumFor(self, "each_char")
		}
		for _, r := range strOf(self) {
			vm.callBlock(blk, []object.Value{object.NewString(string(r))})
		}
		return self
	})
	vm.cString.define("each_byte", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			return enumFor(self, "each_byte")
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
		s := strOf(self)
		for _, a := range args { // true if any prefix matches; a Regexp must match at offset 0
			if re, ok := a.(*Regexp); ok {
				if md := re.re.Match(s); md != nil && md.Begin(0) == 0 {
					return object.True
				}
				continue
			}
			if strings.HasPrefix(s, strArg(a)) {
				return object.True
			}
		}
		return object.False
	})
	vm.cString.define("end_with?", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(strings.HasSuffix(strOf(self), strArg(args[0])))
	})
	vm.cString.define("index", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		s, needle := strOf(self), strArg(args[0])
		r := []rune(s)
		start := 0
		if len(args) > 1 { // optional character offset (negative counts from the end)
			start = int(intArg(args[1]))
			if start < 0 {
				start += len(r)
			}
			if start < 0 {
				return object.NilV
			}
		}
		if start > len(r) {
			return object.NilV
		}
		byteStart := len(string(r[:start]))
		byteIdx := strings.Index(s[byteStart:], needle)
		if byteIdx < 0 {
			return object.NilV
		}
		return object.Integer(utf8.RuneCountInString(s[:byteStart+byteIdx]))
	})
	vm.cString.define("rindex", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		byteIdx := strings.LastIndex(strOf(self), strArg(args[0]))
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
	vm.cString.define("to_i", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		base := 10
		if len(args) > 0 {
			base = int(intArg(args[0]))
		}
		return stringToInt(strOf(self), base)
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
	strToSym := func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Symbol(strOf(self))
	}
	vm.cString.define("to_sym", strToSym)
	vm.cString.define("intern", strToSym) // MRI alias of String#to_sym
	// scrub replaces invalid byte sequences with a replacement string (the Unicode
	// replacement char U+FFFD by default), returning a valid UTF-8 string.
	vm.cString.define("scrub", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		repl := "�"
		if len(args) > 0 {
			if _, isNil := args[0].(object.Nil); !isNil {
				repl = strArg(args[0])
			}
		}
		return object.NewString(scrubUTF8(strOf(self), repl))
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
	vm.cString.define("squeeze", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		sets := make([]string, len(args))
		for i, a := range args {
			sets[i] = strArg(a)
		}
		return &object.String{B: []byte(squeezeStr(strOf(self), sets...))}
	})
	strIndexFn := func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		var res object.Value
		if re, ok := args[0].(*Regexp); ok { // s[/re/] / s[/re/, group]
			res = vm.stringRegexpIndex(strOf(self), re, args[1:])
		} else {
			res = stringIndex(strOf(self), args)
		}
		if sub, ok := res.(*object.String); ok { // a slice keeps the receiver's encoding
			sub.Enc = self.(*object.String).Enc
		}
		return res
	}
	vm.cString.define("[]", strIndexFn)
	vm.cString.define("slice", strIndexFn)
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
	vm.cString.define("squeeze!", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		sets := make([]string, len(args))
		for i, a := range args {
			sets[i] = strArg(a)
		}
		return strBang(self, func(s string) string { return squeezeStr(s, sets...) })
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
	// Array#initialize fills the receiver: empty / a copy of an Array argument /
	// n copies of a value / n elements from a block. Reused by Array.new for both
	// Array and its subclasses.
	arrayInit := func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		// Array.new / Array.new(other) / Array.new(n[, val]) / Array.new(n) { |i| }
		arr := self.(*object.Array)
		if len(args) == 1 {
			if a, ok := args[0].(*object.Array); ok {
				arr.Elems = append([]object.Value{}, a.Elems...)
				return self
			}
		}
		if len(args) == 0 {
			arr.Elems = nil
			return self
		}
		n := intArg(args[0])
		if n < 0 {
			raise("ArgumentError", "negative array size")
		}
		out := make([]object.Value, n)
		for i := range out {
			switch {
			case blk != nil:
				out[i] = vm.callBlock(blk, []object.Value{object.Integer(int64(i))})
			case len(args) >= 2:
				out[i] = args[1]
			default:
				out[i] = object.NilV
			}
		}
		arr.Elems = out
		return self
	}
	vm.cArray.define("initialize", arrayInit)
	vm.cArray.smethods["new"] = &Method{name: "new", owner: vm.cArray,
		native: func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
			if recv := self.(*RClass); recv != vm.cArray {
				return vm.newBuiltinSubclass(recv, &object.Array{}, args, blk)
			}
			arr := &object.Array{}
			arrayInit(vm, arr, args, blk)
			return arr
		}}
	vm.cArray.define("values_at", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := self.(*object.Array).Elems
		out := make([]object.Value, len(args))
		for i, idxV := range args {
			idx := int(intArg(idxV))
			if idx < 0 {
				idx += len(a)
			}
			if idx >= 0 && idx < len(a) {
				out[i] = a[idx]
			} else {
				out[i] = object.NilV
			}
		}
		return &object.Array{Elems: out}
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
	vm.cArray.define("pop", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := self.(*object.Array)
		if len(args) > 0 { // pop(n) removes and returns the last n as an array
			n := int(intArg(args[0]))
			if n < 0 {
				raise("ArgumentError", "negative array size")
			}
			if n > len(a.Elems) {
				n = len(a.Elems)
			}
			start := len(a.Elems) - n
			out := append([]object.Value{}, a.Elems[start:]...)
			a.Elems = a.Elems[:start]
			return &object.Array{Elems: out}
		}
		if len(a.Elems) == 0 {
			return object.NilV
		}
		v := a.Elems[len(a.Elems)-1]
		a.Elems = a.Elems[:len(a.Elems)-1]
		return v
	})
	vm.cArray.define("shift", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := self.(*object.Array)
		if len(args) > 0 { // shift(n) removes and returns the first n as an array
			n := int(intArg(args[0]))
			if n < 0 {
				raise("ArgumentError", "negative array size")
			}
			if n > len(a.Elems) {
				n = len(a.Elems)
			}
			out := append([]object.Value{}, a.Elems[:n]...)
			a.Elems = a.Elems[n:]
			return &object.Array{Elems: out}
		}
		if len(a.Elems) == 0 {
			return object.NilV
		}
		v := a.Elems[0]
		a.Elems = a.Elems[1:]
		return v
	})
	unshift := func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := self.(*object.Array)
		a.Elems = append(append([]object.Value{}, args...), a.Elems...)
		return a
	}
	vm.cArray.define("unshift", unshift)
	vm.cArray.define("prepend", unshift)
	vm.cArray.define("delete", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		// Remove every element == the argument; return it, or (a block's result,
		// else nil) when nothing matched.
		a := self.(*object.Array)
		found := false
		var out []object.Value
		for _, e := range a.Elems {
			if valueEqual(e, args[0]) {
				found = true
			} else {
				out = append(out, e)
			}
		}
		a.Elems = out
		if found {
			return args[0]
		}
		if blk != nil {
			return vm.callBlock(blk, nil)
		}
		return object.NilV
	})
	vm.cArray.define("delete_if", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			return enumFor(self, "delete_if")
		}
		a := self.(*object.Array)
		var out []object.Value
		for _, e := range a.Elems {
			if !vm.callBlock(blk, []object.Value{e}).Truthy() {
				out = append(out, e)
			}
		}
		a.Elems = out
		return a
	})
	vm.cArray.define("concat", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := self.(*object.Array)
		for _, arg := range args {
			other, ok := arg.(*object.Array)
			if !ok {
				raise("TypeError", "no implicit conversion of %s into Array", classNameOf(arg))
			}
			a.Elems = append(a.Elems, other.Elems...)
		}
		return a
	})
	vm.cArray.define("clear", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		a := self.(*object.Array)
		a.Elems = nil
		return a
	})
	vm.cArray.define("rotate!", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := self.(*object.Array)
		if n := len(a.Elems); n > 0 {
			k := 1
			if len(args) > 0 {
				k = int(intArg(args[0]))
			}
			k = ((k % n) + n) % n
			a.Elems = append(append([]object.Value{}, a.Elems[k:]...), a.Elems[:k]...)
		}
		return a
	})
	vm.cArray.define("reverse_each", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			return enumFor(self, "reverse_each")
		}
		a := self.(*object.Array)
		for i := len(a.Elems) - 1; i >= 0; i-- {
			vm.callBlock(blk, []object.Value{a.Elems[i]})
		}
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
			return enumFor(self, "each")
		}
		a := self.(*object.Array)
		for _, e := range a.Elems {
			vm.callBlock(blk, []object.Value{e})
		}
		return a
	})
	vm.cArray.define("map", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			return enumFor(self, "map")
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
	vm.cArray.define("dig", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.digValue(self, args)
	})
	vm.cArray.define("uniq", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		return &object.Array{Elems: vm.arrayUniq(self.(*object.Array).Elems, blk)}
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
			return enumFor(self, "map!")
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
	vm.cArray.define("sort!", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		vm.sortSlice(self.(*object.Array).Elems, blk)
		return self
	})
	selectBang := func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			return enumFor(self, "select!")
		}
		return arrayKeepIf(vm, self.(*object.Array), blk, true)
	}
	vm.cArray.define("select!", selectBang)
	vm.cArray.define("filter!", selectBang)
	vm.cArray.define("reject!", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			return enumFor(self, "reject!")
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
	vm.cArray.define("uniq!", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		a := self.(*object.Array)
		out := vm.arrayUniq(a.Elems, blk)
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
	vm.cArray.define("sum", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		acc := object.Value(object.Integer(0))
		if len(args) > 0 {
			acc = args[0]
		}
		for _, e := range self.(*object.Array).Elems {
			if blk != nil { // sum { |x| ... } maps each element before adding
				e = vm.callBlock(blk, []object.Value{e})
			}
			acc = vm.binaryOp(bytecode.OpAdd, acc, e)
		}
		return acc
	})
	vm.cArray.define("to_h", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		h := object.NewHash()
		for i, e := range self.(*object.Array).Elems {
			if blk != nil { // to_h { |x| [k, v] } maps each element to a pair
				e = vm.callBlock(blk, []object.Value{e})
			}
			pair, ok := e.(*object.Array)
			if !ok {
				raise("TypeError", "wrong element type %s at %d (expected array)", vm.classOf(e).name, i)
			}
			if len(pair.Elems) != 2 {
				raise("ArgumentError", "wrong array length at %d (expected 2, was %d)", i, len(pair.Elems))
			}
			h.Set(pair.Elems[0], pair.Elems[1])
		}
		return h
	})
	vm.cArray.define("each_slice", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		if len(args) < 1 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		if blk == nil {
			return enumFor(self, "each_slice", args...)
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
		if len(args) < 1 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		if blk == nil {
			return enumFor(self, "each_cons", args...)
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
	vm.cArray.define("transpose", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		rows := self.(*object.Array).Elems
		if len(rows) == 0 {
			return &object.Array{}
		}
		var width int
		for i, r := range rows {
			ra, ok := r.(*object.Array)
			if !ok {
				raise("TypeError", "no implicit conversion of %s into Array", vm.classOf(r).name)
			}
			if i == 0 {
				width = len(ra.Elems)
			} else if len(ra.Elems) != width {
				raise("IndexError", "element size differs (%d should be %d)", len(ra.Elems), width)
			}
		}
		out := make([]object.Value, width)
		for j := 0; j < width; j++ {
			col := make([]object.Value, len(rows))
			for i, r := range rows {
				col[i] = r.(*object.Array).Elems[j]
			}
			out[j] = &object.Array{Elems: col}
		}
		return &object.Array{Elems: out}
	})
	vm.cArray.define("product", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		lists := [][]object.Value{self.(*object.Array).Elems}
		for _, a := range args {
			la, ok := a.(*object.Array)
			if !ok {
				raise("TypeError", "no implicit conversion of %s into Array", vm.classOf(a).name)
			}
			lists = append(lists, la.Elems)
		}
		// Cartesian product, last list varying fastest (MRI order).
		out := []object.Value{&object.Array{}}
		for _, list := range lists {
			var next []object.Value
			for _, prefix := range out {
				for _, e := range list {
					row := append(append([]object.Value{}, prefix.(*object.Array).Elems...), e)
					next = append(next, &object.Array{Elems: row})
				}
			}
			out = next
		}
		return &object.Array{Elems: out}
	})
	vm.cArray.define("combination", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		k := int(intArg(args[0]))
		elems := self.(*object.Array).Elems
		var combos []object.Value
		if k >= 0 && k <= len(elems) {
			idx := make([]int, k)
			for i := range idx {
				idx[i] = i
			}
			for {
				pick := make([]object.Value, k)
				for i, j := range idx {
					pick[i] = elems[j]
				}
				combos = append(combos, &object.Array{Elems: pick})
				// advance the index combination (lexicographic)
				i := k - 1
				for i >= 0 && idx[i] == i+len(elems)-k {
					i--
				}
				if i < 0 {
					break
				}
				idx[i]++
				for j := i + 1; j < k; j++ {
					idx[j] = idx[j-1] + 1
				}
			}
		}
		if blk == nil {
			return enumFor(&object.Array{Elems: combos}, "each")
		}
		for _, c := range combos {
			vm.callBlock(blk, []object.Value{c})
		}
		return self
	})
	vm.cArray.define("permutation", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		elems := self.(*object.Array).Elems
		k := len(elems)
		if len(args) > 0 {
			k = int(intArg(args[0]))
		}
		var perms []object.Value
		if k >= 0 && k <= len(elems) {
			used := make([]bool, len(elems))
			pick := make([]object.Value, k)
			var gen func(depth int)
			gen = func(depth int) {
				if depth == k {
					out := make([]object.Value, k)
					copy(out, pick)
					perms = append(perms, &object.Array{Elems: out})
					return
				}
				for i := range elems {
					if used[i] {
						continue
					}
					used[i] = true
					pick[depth] = elems[i]
					gen(depth + 1)
					used[i] = false
				}
			}
			gen(0)
		}
		if blk == nil {
			return enumFor(&object.Array{Elems: perms}, "each")
		}
		for _, pr := range perms {
			vm.callBlock(blk, []object.Value{pr})
		}
		return self
	})
	vm.cArray.define("take_while", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			return enumFor(self, "take_while")
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
			return enumFor(self, "drop_while")
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
	vm.cArray.define("sort", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		a := self.(*object.Array)
		out := make([]object.Value, len(a.Elems))
		copy(out, a.Elems)
		vm.sortSlice(out, blk)
		return &object.Array{Elems: out}
	})
	vm.cArray.define("<=>", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := self.(*object.Array).Elems
		b, ok := args[0].(*object.Array)
		if !ok {
			return object.NilV
		}
		be := b.Elems
		n := len(a)
		if len(be) < n {
			n = len(be)
		}
		for i := 0; i < n; i++ {
			c, ok := vm.send(a[i], "<=>", []object.Value{be[i]}, nil).(object.Integer)
			if !ok {
				return object.NilV // an incomparable pair makes the arrays incomparable
			}
			if c != 0 {
				return c
			}
		}
		switch { // equal prefixes: the shorter array sorts first
		case len(a) < len(be):
			return object.Integer(-1)
		case len(a) > len(be):
			return object.Integer(1)
		}
		return object.Integer(0)
	})
	vm.cNilClass.define("to_a", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return &object.Array{}
	})
	vm.cArray.define("sort_by", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			return enumFor(self, "sort_by")
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
		if len(args) < 1 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		if blk == nil {
			return enumFor(self, "each_with_object", args...)
		}
		memo := args[0]
		for _, e := range self.(*object.Array).Elems {
			vm.callBlock(blk, []object.Value{e, memo})
		}
		return memo
	})

	// Hash.
	// Hash.new — Hash.new, Hash.new(default), or Hash.new { |hash, key| … }.
	// Hash#initialize sets the default: a static default value, or a default block.
	// Reused by Hash.new for both Hash and its subclasses.
	hashInit := func(_ *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		h := self.(*object.Hash)
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
		return self
	}
	vm.cHash.define("initialize", hashInit)
	vm.cHash.smethods["new"] = &Method{name: "new", owner: vm.cHash,
		native: func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
			if recv := self.(*RClass); recv != vm.cHash {
				return vm.newBuiltinSubclass(recv, object.NewHash(), args, blk)
			}
			h := object.NewHash()
			hashInit(vm, h, args, blk)
			return h
		}}
	// String#initialize sets the receiver's content: "" with no argument, a copy
	// of a String argument; a keyword-only call (capacity:/encoding:) arrives as a
	// Hash and is ignored. Reused by String.new for both String and its subclasses.
	stringInit := func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		s := self.(*object.String)
		content := ""
		if len(args) > 0 {
			switch a := args[0].(type) {
			case *object.String:
				content = a.Str()
			case *object.Hash: // keyword-only arguments
			default:
				raise("TypeError", "no implicit conversion of %s into String", vm.classOf(args[0]).name)
			}
		}
		s.B = []byte(content)
		return self
	}
	vm.cString.define("initialize", stringInit)
	// String.new builds a real String (it was falling through to the
	// instance-allocating Class#new and producing a bogus object). A subclass
	// instead wraps a String in an RObject so its class identity is preserved.
	vm.cString.smethods["new"] = &Method{name: "new", owner: vm.cString,
		native: func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
			if recv := self.(*RClass); recv != vm.cString {
				return vm.newBuiltinSubclass(recv, object.NewString(""), args, blk)
			}
			s := object.NewString("")
			stringInit(vm, s, args, blk)
			return s
		}}
	vm.cHash.smethods["[]"] = &Method{name: "[]", owner: vm.cHash,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			h := object.NewHash()
			// Hash[[[k,v],…]] / Hash[existing_hash] / Hash[k1,v1,k2,v2,…].
			if len(args) == 1 {
				switch a := args[0].(type) {
				case *object.Array:
					for i, e := range a.Elems {
						pair, ok := e.(*object.Array)
						if !ok {
							raise("ArgumentError", "wrong element type %s at %d (expected array)", vm.classOf(e).name, i)
						}
						if len(pair.Elems) < 1 || len(pair.Elems) > 2 {
							raise("ArgumentError", "invalid number of elements (%d for 1..2)", len(pair.Elems))
						}
						v := object.Value(object.NilV)
						if len(pair.Elems) == 2 {
							v = pair.Elems[1]
						}
						h.Set(pair.Elems[0], v)
					}
					return h
				case *object.Hash:
					for _, k := range a.Keys {
						v, _ := a.Get(k)
						h.Set(k, v)
					}
					return h
				}
			}
			if len(args)%2 != 0 {
				raise("ArgumentError", "odd number of arguments for Hash")
			}
			for i := 0; i < len(args); i += 2 {
				h.Set(args[i], args[i+1])
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
			return enumFor(self, "each")
		}
		h := self.(*object.Hash)
		for _, k := range h.Keys {
			v, _ := h.Get(k)
			vm.callBlock(blk, []object.Value{hashPair(k, v)})
		}
		return h
	})
	vm.cHash.methods["each_pair"] = vm.cHash.methods["each"]
	bumpMethodSerial()
	// mergeInto folds each other hash into dst. On a key already present, a block
	// (|key, old, new|) decides the value; without one the new value wins. Several
	// hashes may be merged left to right.
	mergeInto := func(vm *VM, dst *object.Hash, others []object.Value, blk *Proc) {
		for _, o := range others {
			other, ok := o.(*object.Hash)
			if !ok {
				raise("TypeError", "no implicit conversion into Hash")
			}
			for _, k := range other.Keys {
				v, _ := other.Get(k)
				if blk != nil {
					if old, exists := dst.Get(k); exists {
						v = vm.callBlock(blk, []object.Value{k, old, v})
					}
				}
				dst.Set(k, v)
			}
		}
	}
	vm.cHash.define("merge", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		h := self.(*object.Hash)
		out := object.NewHash()
		for _, k := range h.Keys {
			v, _ := h.Get(k)
			out.Set(k, v)
		}
		mergeInto(vm, out, args, blk)
		return out
	})
	mergeBang := func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		h := self.(*object.Hash)
		mergeInto(vm, h, args, blk)
		return h
	}
	vm.cHash.define("merge!", mergeBang)
	vm.cHash.define("update", mergeBang) // update is an alias for merge!
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
		// Copy then drop each key by value (Delete keys by hashKey, not by the
		// argument's object identity — a previous identity-keyed Go map dropped
		// nothing, since stored keys are distinct objects from the arguments).
		h := self.(*object.Hash)
		out := object.NewHash()
		for _, k := range h.Keys {
			v, _ := h.Get(k)
			out.Set(k, v)
		}
		for _, k := range args {
			out.Delete(k)
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
			return enumFor(self, "transform_values")
		}
		h := self.(*object.Hash)
		out := object.NewHash()
		for _, k := range h.Keys {
			v, _ := h.Get(k)
			out.Set(k, vm.callBlock(blk, []object.Value{v}))
		}
		return out
	})
	vm.cHash.define("transform_keys", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		var mapping *object.Hash
		if len(args) > 0 {
			mapping, _ = args[0].(*object.Hash)
		}
		if mapping == nil && blk == nil {
			return enumFor(self, "transform_keys")
		}
		h := self.(*object.Hash)
		out := object.NewHash()
		for _, k := range h.Keys {
			v, _ := h.Get(k)
			out.Set(vm.transformKey(k, mapping, blk), v)
		}
		return out
	})
	vm.cHash.define("transform_values!", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			return enumFor(self, "transform_values!")
		}
		h := self.(*object.Hash)
		for _, k := range h.Keys {
			v, _ := h.Get(k)
			h.Set(k, vm.callBlock(blk, []object.Value{v}))
		}
		return h
	})
	vm.cHash.define("transform_keys!", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		var mapping *object.Hash
		if len(args) > 0 {
			mapping, _ = args[0].(*object.Hash)
		}
		if mapping == nil && blk == nil {
			return enumFor(self, "transform_keys!")
		}
		h := self.(*object.Hash)
		// Compute the new keys first, then rebuild in place so a new key never
		// collides with an old one mid-iteration.
		keys := append([]object.Value{}, h.Keys...)
		newKeys := make([]object.Value, len(keys))
		vals := make([]object.Value, len(keys))
		for i, k := range keys {
			vals[i], _ = h.Get(k)
			newKeys[i] = vm.transformKey(k, mapping, blk)
		}
		for _, k := range keys {
			h.Delete(k)
		}
		for i := range newKeys {
			h.Set(newKeys[i], vals[i])
		}
		return h
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
			return enumFor(self, "select")
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
			return enumFor(self, "reject")
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
	vm.cRange.define("min", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		r := self.(*object.Range)
		if len(args) > 0 { // min(n): the n smallest (the range is ascending)
			elems := rangeElems(r)
			n := clampCount(intArg(args[0]), len(elems))
			out := make([]object.Value, n)
			copy(out, elems[:n])
			return &object.Array{Elems: out}
		}
		lo, _, ok := rangeInts(r)
		if !ok { // non-integer (e.g. String) range: the first iterated element
			elems := rangeElems(r)
			if len(elems) == 0 {
				return object.NilV
			}
			return elems[0]
		}
		if rangeSize(r) == 0 {
			return object.NilV
		}
		return object.Integer(lo)
	})
	vm.cRange.define("max", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		r := self.(*object.Range)
		if len(args) > 0 { // max(n): the n largest, descending
			elems := rangeElems(r)
			n := clampCount(intArg(args[0]), len(elems))
			out := make([]object.Value, n)
			for i := 0; i < n; i++ {
				out[i] = elems[len(elems)-1-i]
			}
			return &object.Array{Elems: out}
		}
		_, hi, ok := rangeInts(r)
		if !ok { // non-integer range: the last iterated element
			elems := rangeElems(r)
			if len(elems) == 0 {
				return object.NilV
			}
			return elems[len(elems)-1]
		}
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
			return enumFor(self, "each")
		}
		r := self.(*object.Range)
		for _, e := range rangeElems(r) {
			vm.callBlock(blk, []object.Value{e})
		}
		return r
	})
	vm.cRange.define("map", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			return enumFor(self, "map")
		}
		elems := rangeElems(self.(*object.Range))
		out := make([]object.Value, len(elems))
		for i, e := range elems {
			out[i] = vm.callBlock(blk, []object.Value{e})
		}
		return &object.Array{Elems: out}
	})
	vm.cRange.define("step", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		if blk == nil {
			return enumFor(self, "step", args...)
		}
		step := object.Value(object.Integer(1))
		if len(args) > 0 {
			step = args[0]
		}
		r := self.(*object.Range)
		vm.numericStep(blk, r.Lo, r.Hi, step, r.Exclusive)
		return r
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
	fdiv := func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a, _ := toFloat(self)
		b, ok := toFloat(args[0])
		if !ok {
			// MRI names the receiver's class in the coercion error (Integer#fdiv
			// reports "into Integer", Float#fdiv "into Float").
			raise("TypeError", "%s can't be coerced into %s", vm.classOf(args[0]).name, vm.classOf(self).name)
		}
		return object.Float(a / b)
	}
	vm.cInteger.define("fdiv", fdiv)
	vm.cFloat.define("fdiv", fdiv)
	coerce := func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		other := args[0]
		_, selfInt := self.(object.Integer)
		_, otherInt := other.(object.Integer)
		if selfInt && otherInt {
			return &object.Array{Elems: []object.Value{other, self}}
		}
		sf, _ := toFloat(self) // self is always numeric here
		of, ok := toFloat(other)
		if !ok { // MRI coerces via Float(other), so mirror its errors
			if s, isStr := other.(*object.String); isStr {
				raise("ArgumentError", "invalid value for Float(): %s", s.Inspect())
			}
			// MRI names nil/true/false by value, everything else by class.
			name := vm.classOf(other).name
			switch other.(type) {
			case object.Nil:
				name = "nil"
			case object.Bool:
				name = other.ToS()
			}
			raise("TypeError", "can't convert %s into Float", name)
		}
		return &object.Array{Elems: []object.Value{object.Float(of), object.Float(sf)}}
	}
	vm.cInteger.define("coerce", coerce)
	vm.cFloat.define("coerce", coerce)
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
	vm.cInteger.define("gcdlcm", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a, b := intOf(self), intArg(args[0])
		g := gcdInt(a, b)
		var lcm int64
		if a != 0 && b != 0 {
			lcm = absInt(a / g * b)
		}
		return &object.Array{Elems: []object.Value{object.Integer(g), object.Integer(lcm)}}
	})
	vm.cInteger.define("remainder", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		// remainder truncates toward zero (keeping the dividend's sign), unlike %
		// which floors — exactly Go's % operator.
		a, b := intOf(self), intArg(args[0])
		if b == 0 {
			raise("ZeroDivisionError", "divided by 0")
		}
		return object.Integer(a % b)
	})
	// round / truncate with ndigits >= 0 leave an Integer unchanged; with ndigits
	// < 0 they round/truncate to the nearest 10**(-ndigits) (round is half away
	// from zero, truncate toward zero).
	vm.cInteger.define("round", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		n := intArgOr(args, 0)
		if n >= 0 {
			return self
		}
		pow, ok := pow10(-n)
		if !ok {
			return object.Integer(0) // 10**(-n) exceeds any int64, so it rounds to 0
		}
		a, neg := absSign(intOf(self))
		r := ((a + pow/2) / pow) * pow
		if neg {
			r = -r
		}
		return object.Integer(r)
	})
	vm.cInteger.define("truncate", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		n := intArgOr(args, 0)
		if n >= 0 {
			return self
		}
		pow, ok := pow10(-n)
		if !ok {
			return object.Integer(0)
		}
		a, neg := absSign(intOf(self))
		r := (a / pow) * pow
		if neg {
			r = -r
		}
		return object.Integer(r)
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
		if len(args) < 1 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		if blk == nil {
			return enumFor(self, "upto", args...)
		}
		for i := intOf(self); i <= intArg(args[0]); i++ {
			vm.callBlock(blk, []object.Value{object.Integer(i)})
		}
		return self
	})
	vm.cInteger.define("downto", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		if len(args) < 1 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		if blk == nil {
			return enumFor(self, "downto", args...)
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
	vm.cFloat.define("ceil", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return floatRound(floatOf(self), args, math.Ceil)
	})
	vm.cFloat.define("floor", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return floatRound(floatOf(self), args, math.Floor)
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
	vm.cFloat.define("divmod", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := floatOf(self)
		b, ok := toFloat(args[0])
		if !ok {
			raise("TypeError", "%s can't be coerced into Float", vm.classOf(args[0]).name)
		}
		if b == 0 {
			raise("ZeroDivisionError", "divided by 0")
		}
		// Floored division: the quotient is an Integer, the modulo a Float.
		q := math.Floor(a / b)
		return &object.Array{Elems: []object.Value{object.Integer(int64(q)), object.Float(a - b*q)}}
	})
	vm.cFloat.define("truncate", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		// Truncate toward zero. ndigits > 0 keeps a Float; otherwise an Integer.
		return floatRound(floatOf(self), args, math.Trunc)
	})
	vm.cFloat.define("to_r", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		f := floatOf(self)
		r := new(big.Rat).SetFloat64(f)
		if r == nil { // NaN or ±Infinity has no rational value
			msg := "NaN"
			if math.IsInf(f, 1) {
				msg = "Infinity"
			} else if math.IsInf(f, -1) {
				msg = "-Infinity"
			}
			raise("FloatDomainError", "%s", msg)
		}
		return &object.Rational{R: r}
	})
	vm.cFloat.define("rationalize", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		f := floatOf(self)
		if math.IsNaN(f) || math.IsInf(f, 0) {
			msg := "NaN"
			if math.IsInf(f, 1) {
				msg = "Infinity"
			} else if math.IsInf(f, -1) {
				msg = "-Infinity"
			}
			raise("FloatDomainError", "%s", msg)
		}
		return &object.Rational{R: rationalizeFloat(f)}
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
	// allocate creates an uninitialized instance (no initialize call), as MRI's
	// Class#allocate — used by frameworks that construct then initialize manually.
	vm.cClass.define("allocate", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return &RObject{class: self.(*RClass), ivars: map[string]object.Value{}}
	})
	// Class.new([superclass]) { body } builds an anonymous class (super defaults
	// to Object); the block, if any, runs as the class body. Dispatched only for
	// the Class receiver itself (a normal Foo.new still allocates an instance).
	vm.cClass.smethods["new"] = &Method{name: "new", owner: vm.cClass,
		native: func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
			super := vm.cObject
			if len(args) > 0 {
				s, ok := args[0].(*RClass)
				if !ok {
					raise("TypeError", "superclass must be an instance of Class (given an instance of %s)", vm.classOf(args[0]).name)
				}
				super = s
			}
			c := newClass("", super)
			if blk != nil {
				vm.classEval(c, blk, nil)
			}
			return c
		}}
	vm.cClass.define("superclass", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		if c := self.(*RClass); c.super != nil {
			return c.super
		}
		return object.NilV
	})

	vm.cInteger.define("step", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		if len(args) < 1 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
		}
		if blk == nil {
			return enumFor(self, "step", args...)
		}
		step := object.Value(object.Integer(1))
		if len(args) > 1 {
			step = args[1]
		}
		vm.numericStep(blk, self, args[0], step, false)
		return self
	})

	// Integer#times — the first block-driven iterator.
	vm.cInteger.define("times", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			return enumFor(self, "times")
		}
		n := int64(self.(object.Integer))
		// One reused arg slice across the whole loop: callBlock → exec copies the
		// args into the block's env slots synchronously before any user code runs
		// (and before the next iteration overwrites elem[0]), so the backing array
		// can be shared — this removes the per-iteration slice allocation on the
		// hot times-block path.
		arg := make([]object.Value, 1)
		for i := int64(0); i < n; i++ {
			arg[0] = object.Integer(i)
			vm.callBlock(blk, arg)
		}
		return self
	})

	vm.installRegexp()
	setupStruct(vm)
	// These depend on Struct (Etc::Passwd/Group) and the core collections, so they
	// run after setupStruct and the prelude-defined Enumerable/Hash/Array.
	vm.registerEtc()        // Etc module (real pw/grp via os/user; systmpdir); needs Struct + Enumerable
	vm.registerConcurrent() // concurrent-ruby shell (collections alias core; ThreadLocalVar)
	vm.registerENV()        // ENV: Hash-like view of the process environment over os
}

// nativeNew allocates an instance of the receiver class and runs initialize,
// forwarding any block.
func nativeNew(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
	class := self.(*RClass)
	obj := &RObject{class: class, ivars: map[string]object.Value{}}
	vm.send(obj, "initialize", args, blk)
	return obj
}

// newBuiltinSubclass allocates an instance of a user subclass recv of a built-in
// value type, wrapping a fresh zero value, then runs initialize — which
// populates the wrapped value from args (dispatched onto it via callNative's
// unwrap), so each value type's own constructor semantics are reused unchanged.
func (vm *VM) newBuiltinSubclass(recv *RClass, zero object.Value, args []object.Value, blk *Proc) object.Value {
	obj := &RObject{class: recv, ivars: map[string]object.Value{}, builtin: zero}
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

// intArgOr returns the first integer argument, or def when there is none.
func intArgOr(args []object.Value, def int64) int64 {
	if len(args) > 0 {
		return intArg(args[0])
	}
	return def
}

// pow10 returns 10**n, with ok=false when it would overflow an int64.
func pow10(n int64) (int64, bool) {
	p := int64(1)
	for i := int64(0); i < n; i++ {
		if p > math.MaxInt64/10 {
			return 0, false
		}
		p *= 10
	}
	return p, true
}

// absSign returns |a| and whether a was negative.
func absSign(a int64) (int64, bool) {
	if a < 0 {
		return -a, true
	}
	return a, false
}

// floatRound applies Float#floor/#ceil (and shares Float#round's contract): with
// no argument or ndigits <= 0 it returns an Integer; with ndigits > 0 it rounds
// to that many decimals and stays a Float. fn is math.Floor or math.Ceil.
func floatRound(f float64, args []object.Value, fn func(float64) float64) object.Value {
	ndigits := 0
	if len(args) > 0 {
		ndigits = int(intArg(args[0]))
	}
	pow := math.Pow(10, float64(ndigits))
	r := fn(f*pow) / pow
	if ndigits > 0 {
		return object.Float(r)
	}
	return object.Integer(int64(r))
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

// Kernel#puts/print/p write through the current $stdout (an IOObj), so a host
// or program that reassigns $stdout — e.g. to a StringIO — captures the output,
// as in MRI. The puts array-flattening/newline logic lives in io.go (ioPuts).
func nativePuts(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
	vm.ioPuts(vm.curStdout(), args)
	return object.NilV
}

func nativePrint(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
	o := vm.curStdout()
	for _, a := range args {
		o.writeStr(vm.displayStr(a))
	}
	return object.NilV
}

func nativeP(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
	o := vm.curStdout()
	for _, a := range args {
		o.writeStr(vm.inspectStr(a) + "\n")
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

// stringToInt mimics String#to_i(base): optional whitespace and sign, an optional
// radix prefix matching the base (or auto-detected when base is 0), then the
// longest run of valid digits (underscores allowed between digits); 0 when there
// is no leading integer. The result promotes to a Bignum when it overflows int64.
func stringToInt(s string, base int) object.Value {
	if base != 0 && (base < 2 || base > 36) {
		raise("ArgumentError", "invalid radix %d", base)
	}
	s = strings.TrimLeft(s, wsCutset)
	neg := false
	if len(s) > 0 && (s[0] == '+' || s[0] == '-') {
		neg = s[0] == '-'
		s = s[1:]
	}
	if len(s) >= 2 && s[0] == '0' { // strip a prefix consistent with the base
		switch s[1] | 0x20 {
		case 'x':
			if base == 16 || base == 0 {
				base, s = 16, s[2:]
			}
		case 'b':
			if base == 2 || base == 0 {
				base, s = 2, s[2:]
			}
		case 'o':
			if base == 8 || base == 0 {
				base, s = 8, s[2:]
			}
		case 'd':
			if base == 10 || base == 0 {
				base, s = 10, s[2:]
			}
		}
	}
	if base == 0 {
		base = 10
	}
	var digits []byte
	prevDigit := false
	for i := 0; i < len(s); i++ {
		if s[i] == '_' {
			if !prevDigit { // an underscore must sit between two digits
				break
			}
			prevDigit = false
			continue
		}
		if digitValue(s[i]) < base {
			digits = append(digits, s[i])
			prevDigit = true
		} else {
			break
		}
	}
	if len(digits) == 0 {
		return object.Integer(0)
	}
	z, _ := new(big.Int).SetString(string(digits), base)
	if neg {
		z.Neg(z)
	}
	return object.NormInt(z)
}

// digitValue maps a base-36 digit character to its value (>= 36 if not a digit).
func digitValue(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'z':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'Z':
		return int(c-'A') + 10
	}
	return 99
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
	if sub, ok := args[0].(*object.String); ok { // s[substr] -> the substring if present, else nil
		if strings.Contains(s, sub.Str()) {
			return object.NewString(sub.Str())
		}
		return object.NilV
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

// rationalizeFloat returns the simplest rational that rounds back to f, matching
// Ruby's Float#rationalize. It searches the interval [f-delta, f+delta] where
// delta is half the distance to the neighbouring representable doubles, then
// finds the rational in that interval with the smallest denominator via the
// classic Stern-Brocot / continued-fraction mediant search.
func rationalizeFloat(f float64) *big.Rat {
	if f == 0 {
		return new(big.Rat)
	}
	if f < 0 {
		r := rationalizeFloat(-f)
		return r.Neg(r)
	}
	lo := new(big.Rat).SetFloat64(math.Nextafter(f, math.Inf(-1)))
	hi := new(big.Rat).SetFloat64(math.Nextafter(f, math.Inf(1)))
	exact := new(big.Rat).SetFloat64(f)
	// Use the midpoints to the neighbours as the inclusive search bounds.
	half := big.NewRat(1, 2)
	lo.Add(lo, exact).Mul(lo, half)
	hi.Add(hi, exact).Mul(hi, half)
	return simplestRatBetween(lo, hi)
}

// simplestRatBetween returns the rational with the smallest denominator lying in
// the closed interval [lo, hi] (0 <= lo <= hi), using a mediant
// (continued-fraction) descent. The interval is required to be non-negative;
// rationalizeFloat handles the sign before calling in.
func simplestRatBetween(lo, hi *big.Rat) *big.Rat {
	// For non-negative lo, integer truncation equals the floor, and an integer in
	// [lo, hi] is the simplest answer.
	loFloor := new(big.Int).Quo(lo.Num(), lo.Denom())
	floorRat := new(big.Rat).SetInt(loFloor)
	if floorRat.Cmp(lo) >= 0 && floorRat.Cmp(hi) <= 0 {
		return floorRat
	}
	ceil := new(big.Int).Add(loFloor, big.NewInt(1))
	ceilRat := new(big.Rat).SetInt(ceil)
	if ceilRat.Cmp(lo) >= 0 && ceilRat.Cmp(hi) <= 0 {
		return ceilRat
	}
	// No integer in range: peel off the common integer part and recurse on the
	// reciprocal interval (continued-fraction step).
	one := big.NewRat(1, 1)
	loFrac := new(big.Rat).Sub(lo, floorRat)
	hiFrac := new(big.Rat).Sub(hi, floorRat)
	inner := simplestRatBetween(new(big.Rat).Quo(one, hiFrac), new(big.Rat).Quo(one, loFrac))
	return floorRat.Add(floorRat, new(big.Rat).Quo(one, inner))
}

// transformKey computes a replacement key for Hash#transform_keys(!): a mapping
// hash takes precedence when it contains the key, otherwise the block (if any)
// is applied, and failing both the key is left unchanged.
func (vm *VM) transformKey(k object.Value, mapping *object.Hash, blk *Proc) object.Value {
	if mapping != nil {
		if nk, ok := mapping.Get(k); ok {
			return nk
		}
	}
	if blk != nil {
		return vm.callBlock(blk, []object.Value{k})
	}
	return k
}

// squeezeStr collapses each run of identical bytes to a single byte. When one
// or more character-set arguments are given only runs of bytes that belong to
// the intersection of those sets are collapsed (matching String#squeeze).
func squeezeStr(s string, sets ...string) string {
	var set []byte
	squeezeAll := len(sets) == 0
	if !squeezeAll {
		set = expandCharSet(sets[0])
		for _, extra := range sets[1:] {
			next := expandCharSet(extra)
			keep := set[:0]
			for _, b := range set {
				if byteIndex(next, b) >= 0 {
					keep = append(keep, b)
				}
			}
			set = keep
		}
	}
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if i > 0 && s[i] == s[i-1] && (squeezeAll || byteIndex(set, s[i]) >= 0) {
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
	// gsub!(pattern) with no replacement and no block yields an Enumerator bound
	// to gsub! on this receiver (so materialising it mutates the string); sub!
	// raises ArgumentError, as MRI does.
	if blk == nil && len(args) < 2 {
		if !global {
			raise("ArgumentError", "wrong number of arguments (given 1, expected 2)")
		}
		return enumFor(s, "gsub!", args[0])
	}
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

// pathArg coerces a path-like argument to a String the way MRI's File/IO entry
// points do: a String is taken directly; otherwise the value is converted via
// #to_path (Pathname and friends), falling back to #to_str. Anything that
// responds to neither raises TypeError, matching MRI.
func pathArg(vm *VM, v object.Value) string {
	if s, ok := v.(*object.String); ok {
		return s.Str()
	}
	for _, m := range []string{"to_path", "to_str"} {
		if vm.respondsTo(v, m) {
			r := vm.send(v, m, nil, nil)
			s, ok := r.(*object.String)
			if !ok {
				raise("TypeError", "can't convert %s to String (%s#%s gives %s)",
					vm.classOf(v).name, vm.classOf(v).name, m, vm.classOf(r).name)
			}
			return s.Str()
		}
	}
	raise("TypeError", "no implicit conversion of %s into String", vm.classOf(v).name)
	return ""
}

// scrubUTF8 returns s with every invalid UTF-8 byte sequence replaced by repl,
// so the result is valid UTF-8 (backing String#scrub). Valid runs are copied
// verbatim; each invalid byte collapses to repl, as MRI does.
func scrubUTF8(s, repl string) string {
	if utf8.ValidString(s) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			b.WriteString(repl)
			i++
			continue
		}
		b.WriteString(s[i : i+size])
		i += size
	}
	return b.String()
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

// sortSlice stably sorts out in place: by <=> when blk is nil, otherwise using
// blk as an MRI comparator (it returns a negative/zero/positive Integer; a
// non-Integer result raises ArgumentError).
func (vm *VM) sortSlice(out []object.Value, blk *Proc) {
	if blk == nil {
		sort.SliceStable(out, func(i, j int) bool { return vm.spaceship(out[i], out[j]) < 0 })
		return
	}
	sort.SliceStable(out, func(i, j int) bool {
		r := vm.callBlock(blk, []object.Value{out[i], out[j]})
		c, ok := r.(object.Integer)
		if !ok {
			// MRI compares the block's result against 0, so a non-Integer fails as
			// "comparison of <result class> with 0 failed".
			raise("ArgumentError", "comparison of %s with 0 failed", vm.classOf(r).name)
		}
		return c < 0
	})
}

// arrayByExtreme implements min_by/max_by: the element whose block key is
// smallest (want=-1) or largest (want=1). nil for an empty array.
func (vm *VM) arrayByExtreme(a *object.Array, blk *Proc, name string, want int) object.Value {
	if blk == nil {
		return enumFor(a, name)
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

// methodNames returns the method names (as sorted Symbols) defined on c when all
// is false, or across its whole ancestor chain (super + included/prepended
// modules) when all is true. The order is sorted for determinism — MRI uses
// definition order, which the spec leaves implementation-defined.
func (vm *VM) methodNames(c *RClass, all bool) []object.Value {
	seen := map[string]bool{}
	var names []string
	add := func(n string) {
		if !seen[n] {
			seen[n] = true
			names = append(names, n)
		}
	}
	classes := []*RClass{c}
	if all {
		classes = vm.ancestors(c)
	}
	undef := map[string]bool{} // a nearer `undef` tombstone hides an inherited name
	for _, k := range classes {
		for n, m := range k.methods {
			if m.undefined {
				undef[n] = true
				continue
			}
			if !undef[n] {
				add(n)
			}
		}
	}
	sort.Strings(names)
	out := make([]object.Value, len(names))
	for i, n := range names {
		out[i] = object.Symbol(n)
	}
	return out
}

// singletonMethodNames returns the singleton-method names of self as sorted
// Symbols. For a class/module these are its class methods (def self.foo) — walked
// up the superclass chain when all is true, restricted to its own when false. For
// any other object they are the methods on its per-object singleton class, if it
// has one.
func (vm *VM) singletonMethodNames(self object.Value, all bool) []object.Value {
	seen := map[string]bool{}
	var names []string
	add := func(n string) {
		if !seen[n] {
			seen[n] = true
			names = append(names, n)
		}
	}
	collect := func(tbl map[string]*Method) {
		for n, m := range tbl {
			if !m.undefined {
				add(n)
			}
		}
	}
	if c, ok := self.(*RClass); ok {
		collect(c.smethods)
		if all {
			for s := c.super; s != nil; s = s.super {
				collect(s.smethods)
			}
		}
	} else if sc := vm.objSingleton(self); sc != nil {
		collect(sc.methods)
	}
	sort.Strings(names)
	out := make([]object.Value, len(names))
	for i, n := range names {
		out[i] = object.Symbol(n)
	}
	return out
}

// constNameArg coerces a const_get/const_set/const_defined? name (a Symbol or
// String) to its text, rejecting a name that does not begin with an uppercase
// letter — as Ruby does.
func constNameArg(v object.Value) string {
	var name string
	switch n := v.(type) {
	case object.Symbol:
		name = string(n)
	case *object.String:
		name = n.Str()
	default:
		raise("TypeError", "%s is not a symbol nor a string", v.Inspect())
	}
	if r := []rune(name); len(r) == 0 || !unicode.IsUpper(r[0]) {
		raise("NameError", "wrong constant name %s", name)
	}
	return name
}

// stripRadixPrefix removes a leading 0x/0b/0o/0d (after an optional sign) when it
// matches base, so strconv.ParseInt — which only honours the prefix with base 0 —
// accepts e.g. Integer("0xff", 16).
func stripRadixPrefix(s string, base int) string {
	sign := ""
	if len(s) > 0 && (s[0] == '-' || s[0] == '+') {
		sign, s = s[:1], s[1:]
	}
	pfx := map[int]string{16: "0x", 2: "0b", 8: "0o", 10: "0d"}[base]
	if pfx != "" && len(s) >= 2 && strings.ToLower(s[:2]) == pfx {
		s = s[2:]
	}
	return sign + s
}

// classIsA reports whether class c is, inherits from, or includes/prepends
// target.
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
		for _, m := range c.prepends {
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
			panic(vm.excError(vm.captureBacktrace(vm.curExc)))
		}
		panic(vm.excError(vm.captureBacktrace(vm.send(vm.consts["RuntimeError"].(*RClass), "new",
			[]object.Value{object.NewString("unhandled exception")}, nil))))
	case 1:
		switch a := args[0].(type) {
		case *object.String:
			panic(vm.excError(vm.captureBacktrace(vm.send(vm.consts["RuntimeError"].(*RClass), "new", []object.Value{a}, nil))))
		case *RClass:
			panic(vm.excError(vm.captureBacktrace(vm.send(a, "new", nil, nil))))
		case *RObject:
			panic(vm.excError(vm.captureBacktrace(a)))
		}
		raise("TypeError", "exception class/object expected")
		return object.NilV
	default:
		panic(vm.excError(vm.captureBacktrace(vm.send(classArg(args[0]), "new", []object.Value{args[1]}, nil))))
	}
}

// captureBacktrace stamps the current frame stack onto exc as its backtrace, the
// way MRI fills in a backtrace at the point an exception is raised. A re-raise of
// an exception that already carries a backtrace keeps the original (MRI does not
// overwrite it), so an exception rescued and re-raised still points at its first
// raise site. exc is returned for call-site convenience.
func (vm *VM) captureBacktrace(exc object.Value) object.Value {
	if bt := getIvar(exc, backtraceIvar); bt != object.NilV {
		return exc // already has a backtrace (re-raise) — preserve the original
	}
	frames := vm.backtraceFrames(0)
	if frames == nil {
		// Nothing on the frame stack (e.g. a raise from a native context with the
		// stack already unwound): use an empty array rather than nil so #backtrace
		// reports "raised" with no frames, distinct from a never-raised nil.
		frames = []object.Value{}
	}
	setIvar(exc, backtraceIvar, &object.Array{Elems: frames})
	return exc
}

// backtraceIvar is the instance variable that holds an exception's captured
// backtrace (an Array of String, or absent when never raised / explicitly
// cleared). Its leading underscores keep it out of casual user introspection.
const backtraceIvar = "@__backtrace__"

// normalizeBacktrace coerces a #set_backtrace argument into the stored value:
// nil clears it, a single String becomes a one-element Array, and an Array of
// String is taken as-is. Anything else (a non-String element included) is a
// TypeError, matching MRI's "backtrace must be an Array of String ..." message.
func normalizeBacktrace(v object.Value) object.Value {
	switch a := v.(type) {
	case object.Nil:
		return object.NilV
	case *object.String:
		return &object.Array{Elems: []object.Value{a}}
	case *object.Array:
		for _, e := range a.Elems {
			if _, ok := e.(*object.String); !ok {
				raise("TypeError", "backtrace must be an Array of String or an Array of Thread::Backtrace::Location")
			}
		}
		return &object.Array{Elems: append([]object.Value(nil), a.Elems...)}
	default:
		raise("TypeError", "backtrace must be an Array of String or an Array of Thread::Backtrace::Location")
		return object.NilV
	}
}

// exceptionMessageText returns the exception's message string — its @message, or
// the class name when unset — the same text Exception#message yields.
func (vm *VM) exceptionMessageText(self object.Value) string {
	if m := getIvar(self, "@message"); m != object.NilV {
		return m.ToS()
	}
	return vm.classOf(self).name
}

// exceptionDetailedMessage renders "<message> (<ClassName>)", the body MRI's
// Exception#detailed_message produces (highlight off) and that full_message and
// the uncaught-exception printer embed.
func (vm *VM) exceptionDetailedMessage(self object.Value) string {
	return vm.exceptionMessageText(self) + " (" + vm.classOf(self).name + ")"
}

// exceptionFullMessage renders the MRI-shaped multi-line report:
//
//	<file>:<line>:in '<label>': <message> (<ClassName>)
//		from <frame>
//		from ...
//
// The leading "<frame>: " prefix and the "\tfrom" tail come from the captured
// backtrace; with no backtrace it degrades to just the detailed message. Line
// numbers are 0 (the parser carries no positions) and the source-snippet/caret
// lines MRI prints are omitted, but the file+label chain matches.
func (vm *VM) exceptionFullMessage(self object.Value) string {
	detailed := vm.exceptionDetailedMessage(self)
	bt, ok := getIvar(self, backtraceIvar).(*object.Array)
	if !ok || len(bt.Elems) == 0 {
		return detailed
	}
	var b strings.Builder
	b.WriteString(bt.Elems[0].ToS())
	b.WriteString(": ")
	b.WriteString(detailed)
	for _, f := range bt.Elems[1:] {
		b.WriteString("\n\tfrom ")
		b.WriteString(f.ToS())
	}
	return b.String()
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
				return getIvar(self, ivar)
			})
		}
		if writer {
			cls.define(n.ToS()+"=", func(_ *VM, self object.Value, a []object.Value, _ *Proc) object.Value {
				setIvar(self, ivar, a[0])
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
	// String ranges iterate by String#succ from begin up to end (MRI semantics).
	if loS, ok := r.Lo.(*object.String); ok {
		if hiS, ok := r.Hi.(*object.String); ok {
			return strRangeElems(loS.Str(), hiS.Str(), r.Exclusive)
		}
	}
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

// strRangeElems materialises a String range (begin..end / begin...end): it yields
// begin, begin.succ, … (byte-wise comparison, like String#<=>) and stops once the
// value passes end or — after the next succ — grows longer than end. That post-
// succ length guard is what makes ("aa".."b") yield just ["aa"] (MRI semantics).
func strRangeElems(lo, hi string, exclusive bool) []object.Value {
	var out []object.Value
	cur := lo
	for {
		cmp := strings.Compare(cur, hi)
		if cmp > 0 {
			break
		}
		if !(exclusive && cmp == 0) {
			out = append(out, object.NewString(cur))
		}
		if cmp == 0 {
			break
		}
		next := succString(cur)
		if len(next) > len(hi) || next == cur { // overshoots / no progress
			break
		}
		cur = next
	}
	return out
}

func isAlnumByte(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// succString implements String#succ/#next: increment the rightmost alphanumeric
// with carry (9->0, z->a, Z->A) propagating left; a full carry inserts a fresh
// '1'/'a'/'A' at the leftmost alphanumeric ("zz" -> "aaa", "Zz" -> "AAa",
// "99" -> "100"). With no alphanumeric, the rightmost byte is incremented with
// carry. Matches MRI.
func succString(s string) string {
	if s == "" {
		return ""
	}
	b := []byte(s)
	leftmost := -1
	var carry byte
	done := false
	for i := len(b) - 1; i >= 0; i-- {
		c := b[i]
		if !isAlnumByte(c) {
			continue
		}
		leftmost = i
		switch c {
		case '9':
			b[i], carry = '0', '1'
		case 'z':
			b[i], carry = 'a', 'a'
		case 'Z':
			b[i], carry = 'A', 'A'
		default:
			b[i], done = c+1, true
		}
		if done {
			break
		}
	}
	if done {
		return string(b)
	}
	if leftmost >= 0 { // full alphanumeric carry: insert the carry char
		out := make([]byte, 0, len(b)+1)
		out = append(out, b[:leftmost]...)
		out = append(out, carry)
		out = append(out, b[leftmost:]...)
		return string(out)
	}
	// No alphanumeric: increment the rightmost byte, carrying on 0xff.
	for i := len(b) - 1; i >= 0; i-- {
		if b[i] != 0xff {
			b[i]++
			return string(b)
		}
		b[i] = 0
	}
	return string(append([]byte{1}, b...)) // every byte overflowed
}

// numericStep drives Range#step / Integer#step: it yields lo, lo+step, … toward
// hi (inclusive unless exclusive). All-integer operands keep an integer walk
// (exact); any float operand switches to an index-based float walk that avoids
// accumulated drift. step must be non-zero.
func (vm *VM) numericStep(blk *Proc, loV, hiV, stepV object.Value, exclusive bool) {
	li, loInt := loV.(object.Integer)
	hi2, hiInt := hiV.(object.Integer)
	si, stepInt := stepV.(object.Integer)
	if loInt && hiInt && stepInt {
		step := int64(si)
		if step == 0 {
			raise("ArgumentError", "step can't be 0")
		}
		lo, hi := int64(li), int64(hi2)
		for i := lo; stepInRange(float64(i), float64(hi), float64(step), exclusive); i += step {
			vm.callBlock(blk, []object.Value{object.Integer(i)})
		}
		return
	}
	lo, ok1 := toFloat(loV)
	hi, ok2 := toFloat(hiV)
	step, ok3 := toFloat(stepV)
	if !ok1 || !ok2 || !ok3 {
		raise("TypeError", "can't iterate from %s", loV.Inspect())
	}
	if step == 0 {
		raise("ArgumentError", "step can't be 0")
	}
	for i := 0; ; i++ {
		v := lo + float64(i)*step
		if !stepInRange(v, hi, step, exclusive) {
			break
		}
		vm.callBlock(blk, []object.Value{object.Float(v)})
	}
}

// stepInRange reports whether v has not yet passed hi when walking by step. A
// small epsilon tolerates float accumulation so an inclusive endpoint that lands
// exactly on hi is still yielded.
func stepInRange(v, hi, step float64, exclusive bool) bool {
	const eps = 1e-12
	if step > 0 {
		if exclusive {
			return v < hi
		}
		return v <= hi+eps
	}
	if exclusive {
		return v > hi
	}
	return v >= hi-eps
}
