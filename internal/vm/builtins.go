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
	vm.cKernel = kernel
	vm.cObject.includes = append(vm.cObject.includes, kernel)

	for _, c := range []*RClass{
		vm.cBasicObject, vm.cObject, vm.cModule, vm.cClass, cNumeric, vm.cInteger,
		vm.cFloat, vm.cComplex, vm.cRational, vm.cString, vm.cSymbol, vm.cArray, vm.cHash, vm.cRange,
		vm.cProc, vm.cTrueClass, vm.cFalseClass, vm.cNilClass,
		vm.cRegexp, vm.cMatchData, kernel,
	} {
		vm.consts[c.name] = c
	}
	// Float::INFINITY / Float::NAN and the IEEE-754 double-precision limits, matching
	// MRI's Float constants exactly (see core/float/constants_spec).
	vm.cFloat.consts["INFINITY"] = object.Float(math.Inf(1))
	vm.cFloat.consts["NAN"] = object.Float(math.NaN())
	vm.cFloat.consts["MAX"] = object.Float(math.MaxFloat64)
	vm.cFloat.consts["MIN"] = object.Float(math.Float64frombits(0x0010000000000000)) // 2**-1022, smallest positive normal
	vm.cFloat.consts["EPSILON"] = object.Float(math.Nextafter(1, 2) - 1)             // 2**-52
	vm.cFloat.consts["DIG"] = object.IntValue(15)
	vm.cFloat.consts["MANT_DIG"] = object.IntValue(53)
	vm.cFloat.consts["MAX_EXP"] = object.IntValue(1024)
	vm.cFloat.consts["MIN_EXP"] = object.IntValue(-1021)
	vm.cFloat.consts["MAX_10_EXP"] = object.IntValue(308)
	vm.cFloat.consts["MIN_10_EXP"] = object.IntValue(-307)
	vm.cFloat.consts["RADIX"] = object.IntValue(2)

	vm.registerComplex()
	vm.registerRational()
	registerNumericComplexCompat(vm, cNumeric)
	vm.registerMath()
	vm.registerFFT()
	vm.registerNDArray()
	vm.registerArrayEdges()
	vm.registerImage()
	vm.registerSet()
	vm.registerPrettyPrint()
	vm.registerSingleLine()
	vm.registerCMath()
	vm.registerDidYouMean()
	vm.registerTime()
	vm.registerBigDecimal()
	vm.registerDate()
	vm.registerBag()
	vm.registerEval()
	vm.registerBinding()
	vm.registerRequire()
	vm.registerAutoload()
	vm.registerSingleton()
	vm.registerMethod()
	vm.registerModuleExtras()
	vm.registerModuleResiduals()
	vm.registerRefinements()
	vm.registerReflection()
	vm.registerMethodReflect()
	vm.registerMethodReflect2()
	vm.registerProcMethods()
	vm.registerModuleReflect()
	vm.registerVersionConstants()
	vm.registerKernelIntrospection()
	vm.registerEncoding()
	vm.registerStringEncoding()
	vm.registerStringUnicodeNormalize() // String#unicode_normalize / #unicode_normalized? (core-ext, always available), backed by go-ruby-unicode-normalize
	vm.registerJSBridge()               // browser DOM/Canvas access (wasm only; a no-op natively)
	vm.registerBase64()
	vm.registerPackUnpack()
	vm.registerSecureRandom()
	vm.registerDigest()
	vm.registerMarshal()
	vm.registerRandom()
	vm.registerPrime()
	vm.registerTSort()
	vm.registerAbbrev()
	vm.registerFind()      // Find.find / Find.prune (require "find"), backed by go-ruby-find
	vm.registerBenchmark() // Benchmark module (require "benchmark"), backed by go-ruby-benchmark
	vm.registerScanf()     // String#scanf / IO#scanf / Kernel#scanf (require "scanf"), backed by go-ruby-scanf

	procCall := func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.callBlock(self.(*Proc), args)
	}
	vm.cProc.smethods["new"] = &Method{name: "new", owner: vm.cProc,
		native: func(_ *VM, _ object.Value, _ []object.Value, blk *Proc) object.Value {
			if blk == nil {
				raise("ArgumentError", "tried to create Proc object without a block")
			}
			return blk
		}}
	// Proc#call/[]/yield are non-retaining: an ISeq block body copies its args
	// into env slots synchronously (exec) before returning, and callBlockSelf
	// copies for the rarer native-block target, so the OpSend fast path may hand
	// them the live operand-stack region without a defensive copy (defineNR).
	vm.cProc.defineNR("call", procCall)
	vm.cProc.defineNR("[]", procCall)
	vm.cProc.defineNR("yield", procCall)
	// Proc#=== is an alias of #call, so a proc/lambda can be used as a case /
	// grep pattern (pattern === element).
	vm.cProc.define("===", procCall)
	vm.cProc.define("arity", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self.(*Proc).arityVal()))
	})
	vm.cProc.define("lambda?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self.(*Proc).isLambda)
	})
	// Proc#source_location returns [file, line] for where the block was written,
	// or nil when no source is known (a synthesized/native Proc, or compiled-in
	// code such as the prelude whose ISeq carries no File). The VM does not track
	// per-instruction line numbers, so the line is reported as 0 — the array
	// shape ([String, Integer]) is what callers (e.g. Puppet building a type URI)
	// depend on.
	vm.cProc.define("source_location", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		p := self.(*Proc)
		if p.iseq == nil || p.iseq.File == "" {
			return object.NilV
		}
		return object.NewArray(object.NewString(p.iseq.File), object.IntValue(0))
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
		switch o := self.(type) {
		case *object.String:
			o.Frozen = true
		case *object.Array:
			o.Frozen = true
		case *object.Hash:
			o.Frozen = true
		case *RObject:
			o.frozen = true
		case *RClass:
			o.frozen = true
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
		// Snapshot the per-frame tracking-stack depths so that, when a matching
		// throw unwinds the block's frames straight to this recover (bypassing each
		// frame's normal pop), __FILE__/require_relative resolution, caller and
		// backtraces see this catch's state rather than the abandoned deep-frame
		// entries. Without this the pushes leak (racc's catch(:racc_end_parse) +
		// throw drives the Pops parser, so every type parse leaked ~6 entries —
		// corrupting __FILE__ and breaking a later require_relative).
		fileStackDepth := len(vm.fileStack)
		frameNamesDepth := len(vm.frameNames)
		frameFilesDepth := len(vm.frameFiles)
		requireDirsDepth := len(vm.requireDirs)
		// Register this catch's tag so a Kernel#throw can tell a matched throw (an
		// unwind to here) from an unmatched one (an UncaughtThrowError). The depth
		// is restored in the defer, covering both the normal return and a throw
		// unwinding straight past this frame.
		catchTagsDepth := len(vm.catchTags)
		vm.catchTags = append(vm.catchTags, tag)
		defer func() {
			vm.catchTags = vm.catchTags[:catchTagsDepth]
			if r := recover(); r != nil {
				// Tags match by identity (== on the interface): a Symbol by value, a
				// reference by pointer — exactly Ruby's equal?.
				if sig, ok := r.(throwSignal); ok && sig.tag == tag {
					vm.fileStack = vm.fileStack[:fileStackDepth]
					vm.frameNames = vm.frameNames[:frameNamesDepth]
					vm.frameFiles = vm.frameFiles[:frameFilesDepth]
					vm.requireDirs = vm.requireDirs[:requireDirsDepth]
					result = sig.value
					return
				}
				panic(r)
			}
		}()
		return vm.callBlock(blk, []object.Value{tag})
	})
	vm.cObject.define("throw", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
		}
		var val object.Value = object.NilV
		if len(args) > 1 {
			val = args[1]
		}
		// A throw whose tag matches an active catch unwinds to it; a throw with no
		// matching catch is an UncaughtThrowError raised here — a real (rescuable)
		// exception carrying the tag and value, as MRI.
		for i := len(vm.catchTags) - 1; i >= 0; i-- {
			if vm.catchTags[i] == args[0] {
				panic(throwSignal{tag: args[0], value: val})
			}
		}
		vm.raiseWithIvars("UncaughtThrowError", "uncaught throw "+args[0].Inspect(),
			map[string]object.Value{"@tag": args[0], "@value": val})
		return object.NilV
	})
	vm.cBasicObject.define("equal?", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
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
	vm.cBasicObject.define("__id__", objectIDFn)
	vm.cObject.define("hash", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(vm.hashValue(self))
	})
	vm.cObject.define("methods", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		// #methods lists the receiver's public and protected methods (private
		// excluded, as MRI). With a false argument it lists only the receiver's own
		// singleton methods; otherwise its instance methods including inherited ones.
		keep := func(v visibility) bool { return v != visPrivate }
		if len(args) > 0 && !args[0].Truthy() {
			return vm.filterVisibility(self, vm.singletonMethodNames(self, false), keep)
		}
		c := vm.classOf(self)
		if o, ok := self.(*RObject); ok && o.singleton != nil {
			c = o.singleton // its super is the real class, so the walk picks up both
		}
		return vm.filterVisibility(self, vm.methodNames(c, true), keep)
	})
	vm.cObject.define("public_methods", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		// Like #methods, but restricted to PUBLIC methods (excluding private and
		// protected). An `all`/`false` argument selects inherited vs own.
		return vm.reflectMethodNames(self, args, visPublic)
	})
	vm.cObject.define("private_methods", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		// The names of the receiver's privately-accessible methods (an object's
		// private instance methods, or a class's private singleton methods). An
		// `all`/`false` argument selects inherited vs own, as MRI.
		return vm.reflectMethodNames(self, args, visPrivate)
	})
	vm.cObject.define("protected_methods", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		// The names of the receiver's protected methods, mirroring #private_methods.
		return vm.reflectMethodNames(self, args, visProtected)
	})
	vm.cObject.define("singleton_methods", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		// Default includes singleton methods inherited from the receiver's
		// superclasses (a class's class methods); a false argument restricts to the
		// receiver's own. For a plain object the singleton methods are those on its
		// per-object singleton class. Like Ruby's singleton_methods, PRIVATE
		// singleton methods are excluded (public and protected are returned), so the
		// candidate set is filtered by effective send-time visibility just as
		// public_methods/protected_methods do via reflectMethodNames.
		all := len(args) == 0 || args[0].Truthy()
		candidates := vm.singletonMethodNames(self, all)
		return vm.filterVisibility(self, candidates, func(v visibility) bool { return v != visPrivate })
	})
	vm.cObject.define("frozen?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(isFrozen(self))
	})
	vm.cObject.define("instance_variable_get", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return getIvar(self, vm.ivarNameArg(args[0]))
	})
	vm.cObject.define("instance_variable_set", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		name := vm.ivarNameArg(args[0])
		if isFrozen(self) {
			vm.raiseFrozen(self)
		}
		setIvar(self, name, args[1])
		return args[1]
	})
	vm.cObject.define("instance_variable_defined?", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		name := vm.ivarNameArg(args[0])
		t := ivarTable(self)
		if t == nil {
			return object.False
		}
		_, ok := t[name]
		return object.Bool(ok)
	})
	vm.cObject.define("remove_instance_variable", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		name := vm.ivarNameArg(args[0])
		if isFrozen(self) {
			vm.raiseFrozen(self)
		}
		t := ivarTable(self)
		if t != nil {
			if v, ok := t[name]; ok {
				delete(t, name)
				return v
			}
		}
		return raise("NameError", "instance variable %s not defined", name)
	})
	vm.cObject.define("instance_variables", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewArrayFromSlice(ivarNamesInOrder(self))
	})
	vm.cBasicObject.define("instance_eval", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (yield)")
		}
		return vm.callBlockSelf(blk, self, nil)
	})
	vm.cBasicObject.define("instance_exec", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (yield)")
		}
		return vm.callBlockSelf(blk, self, args)
	})
	formatFn := func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1+)")
		}
		return object.NewString(vm.formatString(vm.coerceFormatString(args[0]), args[1:]))
	}
	vm.cObject.define("format", formatFn)
	vm.cObject.define("sprintf", formatFn)
	vm.cObject.define("printf", nativePrintf)
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
		return &Proc{nativeArity: -2, symName: name, native: func(vm *VM, args []object.Value) object.Value {
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
	// FloatDomainError < RangeError: a non-finite Float (Infinity/NaN) coerced to
	// an exact value, e.g. Integer(Float::INFINITY) or (1.0/0).to_r.
	exc("FloatDomainError", "RangeError")
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
	// Math::DomainError must also be reachable through the Math module's own
	// constant table so Ruby-level `Math::DomainError` resolves (registerMath
	// created the module earlier), mirroring the Encoding::* error nesting.
	vm.consts["Math"].(*RClass).consts["DomainError"] = exc("Math::DomainError", "StandardError")
	// EncodingError < StandardError and Encoding's transcoding errors under it.
	vm.registerEncodingErrors()
	// Encoding::Converter, the stateful transcoder (needs the error classes above).
	vm.registerConverter()
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
		status := object.Value(object.IntValue(0))
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
		return object.IntValue(0)
	})
	systemExit.define("success?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		s, ok := getIvar(self, "@status").(object.Integer)
		return object.Bool(!ok || s == 0)
	})

	vm.registerFile()             // needs the exception hierarchy (Errno::ENOENT < StandardError)
	vm.registerIO()               // IO/StringIO + $stdout/$stderr/$stdin (needs IOError/EOFError)
	vm.registerDir()              // Dir (reuses the Errno module set up by registerFile)
	vm.registerTmpdir()           // Dir.tmpdir / Dir.mktmpdir (layers onto Dir; require "tmpdir")
	vm.registerProcess()          // Process module — identity + clock_gettime
	vm.registerSpawn()            // IO.pipe / read_nonblock / select + Process.spawn/waitpid2 + Kernel.fork/exec
	vm.registerObjectSpace()      // ObjectSpace module — finalizer API + reflective no-ops
	vm.registerGC()               // GC module + GC::Profiler — observable-contract shim over Go's GC
	vm.registerOpenSSL()          // OpenSSL (real digest/HMAC/random + PKI/TLS shell); needs StandardError
	vm.registerSocket()           // TCPSocket/TCPServer (net) + OpenSSL::SSL::SSLSocket (crypto/tls); after registerOpenSSL (upgrades its SSL shell)
	vm.registerNetHTTP()          // net/http + net/https loadable shell; needs StandardError
	vm.registerNetHTTPTransport() // real Net::HTTP over the socket transport; after registerNetHTTP + registerSocket
	vm.registerWebMock()          // WebMock stub registry (require "webmock") intercepting the bound Net::HTTP transport; after registerNetHTTP + registerNetHTTPTransport
	vm.registerVCR()              // VCR record/replay of the bound Net::HTTP via cassettes (require "vcr"), backed by go-ruby-vcr; after registerNetHTTP + registerNetHTTPTransport (it routes the real transport)
	vm.registerNetPOP()           // Net::POP3/Net::POPMail (require "net/pop"), backed by go-ruby-net-pop codec; socket = injected IO seam; after registerNetHTTP (Net module) + registerSocket/registerOpenSSL
	vm.registerNetSFTP()          // Net::SFTP client (require "net/sftp"), backed by go-ruby-net-sftp codec; SSH channel = injected IO seam; nests under Net, after registerNetHTTP
	vm.registerNetFTP()           // real Net::FTP over the socket transport; after registerNetHTTP (Net module) + registerSocket/registerOpenSSL
	vm.registerNetLDAP()          // Net::LDAP client (require "net/ldap"/"net-ldap"/"ldap"), backed by go-ruby-ldap over github.com/go-ldap/ldap/v3; nests under Net, after registerNetHTTP (Net module); needs StandardError for Net::LDAP::Error
	vm.registerActiveLdap()       // ActiveLdap ORM: Connection/Model/Record + ldap_mapping/find/save/to_ldif (require "active_ldap"), backed by go-ruby-activeldap; Directory seam is the library MockDirectory or a Net::LDAP-shaped object (go-ruby-ldap); after registerNetLDAP; needs StandardError for ActiveLdap::Error
	vm.registerResolv()           // Resolv (real IPv4/IPv6 parse; DNS sockets stubbed); needs StandardError
	vm.registerTimeout()          // Timeout module (loadable shell); needs RuntimeError
	vm.registerTimecop()          // Timecop module (require "timecop"), backed by go-ruby-timecop; drives vm.clock behind Time.now/Date.today/DateTime.now
	vm.registerDateErrors()       // Date::Error < ArgumentError (Date class itself registered early); needs ArgumentError
	vm.registerJSON()             // JSON module (go-ruby-json backend); needs StandardError for JSON::JSONError
	vm.registerMultiJson()        // MultiJson module (require "multi_json"), backed by go-ruby-multi-json for the adapter registry + errors; parse/generate route through rbgo's ordered JSON so key order + symbol keys survive; needs StandardError + ArgumentError
	vm.registerYAML()             // YAML/Psych loadable shell; needs StandardError
	vm.registerBCrypt()           // BCrypt (require "bcrypt"), backed by go-ruby-bcrypt; needs StandardError + String
	vm.registerJWT()              // JWT (require "jwt"), backed by go-ruby-jwt; needs StandardError
	vm.registerRbNaCl()           // RbNaCl (require "rbnacl"), backed by go-ruby-sodium; needs StandardError
	vm.registerAge()              // Age (require "age"), backed by go-ruby-age; needs StandardError
	vm.registerPrawn()            // Prawn::Document (require "prawn"), backed by go-ruby-prawn (fpdf); needs StandardError for Prawn::Errors
	vm.registerBleve()            // Bleve module (require "bleve"), backed by go-ruby-bleve; needs StandardError (Bleve::Error)
	vm.registerFaraday()          // Faraday HTTP client (require "faraday"), backed by go-ruby-faraday; needs StandardError for Faraday::Error
	vm.registerPuma()             // Puma module (require "puma"), backed by go-ruby-puma; threaded Rack web server over net/http; needs StandardError (Puma::Error) + StringIO (rack.input)
	vm.registerBolt()             // Bolt::DB/Tx/Bucket/Cursor (require "bolt"), backed by go-ruby-bbolt; needs StandardError for Bolt::Error
	vm.registerSimpleCov()        // SimpleCov module + SimpleCov::Result/SourceFile/Formatter (require "simplecov"), backed by go-ruby-simplecov result engine; live line-coverage collection is a deferred VM feature (coverage supplied via SimpleCov.add_coverage); needs Object + Time
	vm.registerSAML()             // SAML / OneLogin::RubySaml (require "saml"/"ruby-saml"), backed by go-ruby-saml; needs StandardError for SAML::Error
	vm.registerWebAuthn()         // WebAuthn module (require "webauthn"), backed by go-ruby-webauthn; needs StandardError (WebAuthn::Error)
	vm.registerACME()             // Acme::Client / Order / Authorization / Challenge / CertificateRequest (require "acme" / "acme/client"), backed by go-ruby-acme (x/crypto/acme); transport is a host seam; needs StandardError for the Acme::Error tree
	vm.registerGRPC()             // GRPC module (require "grpc"), backed by go-ruby-grpc over google.golang.org/grpc; in-process bufconn transport; needs StandardError (GRPC::Error)
	vm.registerNATS()             // NATS module (require "nats"), backed by go-ruby-nats over nats.go; needs StandardError for the NATS::Error tree
	vm.registerKafka()            // Kafka module (require "kafka"), backed by go-ruby-kafka (twmb/franz-go); needs StandardError for Kafka::Error
	vm.registerEtcd()             // Etcd/Etcdv3 client (require "etcd"/"etcdv3"), backed by go-ruby-etcd over go.etcd.io/etcd/client/v3; needs StandardError for Etcd::Error
	vm.registerVault()            // Vault/OpenBao client (require "vault"/"openbao"), backed by go-ruby-openbao; transport wired to the bound Net::HTTP; needs StandardError for Vault::VaultError; after registerNetHTTP + registerNetHTTPTransport
	vm.registerRolify()           // rolify / resourcify class macros (require "rolify"), backed by go-ruby-rolify; role store seam = the reference MemoryStore
	vm.registerMySQL()            // Mysql2::Client/Result/Statement (require "mysql2" / "mysql"), backed by go-ruby-mysql over go-sql-driver; needs Enumerable for Result and StandardError for Mysql2::Error
	vm.registerMongo()            // Mongo::Client/Database/Collection/Cursor + BSON::ObjectId (require "mongo"), backed by go-ruby-mongodb; needs StandardError for Mongo::Error
	vm.registerParquet()          // Parquet::ArrowFileReader/ArrowFileWriter (require "parquet"), backed by go-ruby-parquet; needs StandardError for Parquet::Error
	vm.registerHTTParty()         // HTTParty HTTP client (require "httparty"), backed by go-ruby-httparty; needs StandardError for HTTParty::Error
	vm.registerConnectionPool()   // ConnectionPool + ConnectionPool::Wrapper (require "connection_pool"), backed by go-ruby-connection-pool; needs RuntimeError + Timeout::Error
	vm.registerErubi()            // Erubi::Engine / CaptureEndEngine + Erubi.h (require "erubi"), template->Ruby-source compiler backed by go-ruby-erubi
	vm.registerReline()           // Reline module + Reline::HISTORY (require "reline"), line-editor backed by go-ruby-reline; the terminal I/O is wired to rbgo IO objects via Reline.input=/output=
	vm.registerHTTPrb()           // HTTP module — chainable http.rb client (require "http"), backed by go-ruby-http; needs StandardError for HTTP::Error
	vm.registerExcon()            // Excon module — persistent HTTP client (require "excon"), backed by go-ruby-excon; needs StandardError for Excon::Error
	vm.registerTyphoeus()         // Typhoeus module — parallel HTTP client + Hydra (require "typhoeus"), backed by go-ruby-typhoeus; net/http+goroutines
	vm.registerPundit()           // Pundit mixin (require "pundit"), backed by go-ruby-pundit; policy dispatch + safe_constantize are rbgo seams
	vm.registerFriendlyId()       // FriendlyId slug macro (require "friendly_id"), backed by go-ruby-friendly-id; base-attr read / uniqueness / history are rbgo seams
	vm.registerCanCanCan()        // CanCan::Ability mixin (require "cancancan" / "cancan"), backed by go-ruby-cancancan; attr read + block eval are rbgo seams
	vm.registerMsgpack()          // MessagePack module (go-ruby-msgpack backend); needs StandardError for MessagePack::Error
	vm.registerTOML()             // TOML/TomlRB module (go-ruby-toml backend); needs StandardError for TomlRB::ParseError
	vm.registerTZInfo()           // TZInfo module (go-ruby-tzinfo backend); needs StandardError for TZInfo::InvalidTimezoneIdentifier
	vm.registerChronic()          // Chronic module (go-ruby-chronic backend); needs StandardError
	vm.registerMoney()            // Money module (go-ruby-money backend); needs StandardError for Money::Currency::UnknownCurrency
	vm.registerAddressable()      // Addressable module (go-ruby-addressable backend); needs StandardError
	vm.registerPublicSuffix()     // PublicSuffix module (go-ruby-public-suffix backend); needs StandardError for PublicSuffix::Error tree
	vm.registerMIMETypes()        // MIME::Types module (go-ruby-mime-types backend)
	vm.registerCommonmark()       // Commonmark.render_html / String#to_html (require "commonmark"), backed by go-commonmark
	vm.registerMustache()         // Mustache.render + Mustache view class (require "mustache"); needs StandardError for Mustache::Error
	vm.registerJbuilder()         // Jbuilder.encode / json.<name> DSL (require "jbuilder"), backed by go-ruby-jbuilder
	vm.registerBuilder()          // Builder::XmlMarkup (require "builder"), backed by go-ruby-builder
	vm.registerSQLite3()          // SQLite3::Database/Statement (require "sqlite3"), backed by go-ruby-sqlite3 (modernc); needs StandardError for SQLite3::Exception
	vm.registerPG()               // PG::Connection/Result (require "pg"), backed by go-ruby-pg v3 protocol; socket = injected IO seam; needs StandardError for PG::Error
	vm.registerNetIMAP()          // Net::IMAP (require "net/imap"), backed by go-ruby-net-imap builder+parser; socket = injected IO seam; after socket/openssl so a TCPSocket/SSLSocket can be the TLS seam
	vm.registerNetSMTP()          // Net::SMTP (require "net/smtp"), backed by go-ruby-net-smtp codec over rbgo's real socket/TLS transport; after registerSocket/registerNetHTTP (Net module + transport)
	vm.registerSidekiq()          // Sidekiq::Client/Job(Worker) + module (require "sidekiq"), backed by go-ruby-sidekiq over go-redis/v9; Redis client dialled from Sidekiq.redis/ENV; perform body is Ruby seam; needs StandardError for Sidekiq::Error
	vm.registerResque()           // Resque module + Job/Worker/Failure (require "resque"), backed by go-ruby-resque over go-redis/v9; @queue resolver + self.perform are Ruby seams; needs StandardError for Resque::Error
	vm.registerSequel()           // Sequel query builder + Database (require "sequel"), backed by go-ruby-sequel; executor seam wired to SQLite3::Database (real execution)
	vm.registerNokogiri()         // Nokogiri::HTML/XML -> Document/Node/NodeSet (require "nokogiri"), backed by go-nokogiri; needs StandardError for Nokogiri::SyntaxError
	vm.registerNokogiriXSLT()     // Nokogiri::XSLT(str) -> Stylesheet#transform/apply_to (require "nokogiri"), backed by go-xslt over go-nokogiri; needs registerNokogiri first
	vm.registerRSpec()            // RSpec matcher + expect surface (require "rspec"), backed by go-ruby-rspec; needs Exception for ExpectationNotMetError
	vm.registerFactoryBot()       // FactoryBot.define/build/create/attributes_for/build_list/create_list/generate (require "factory_bot"), backed by go-ruby-factory-bot; the class-instantiation+attribute-assignment (BuildFunc), persistence (PersistFunc, save!) and dynamic-attribute/sequence/callback blocks are the rbgo object-model seams run INLINE under the GVL; needs StandardError for KeyError/ArgumentError raised on unknown/duplicate factories
	vm.registerRuboCop()          // RuboCop::Runner#inspect/autocorrect + Cop::Offense (require "rubocop"), backed by go-ruby-rubocop over go-ruby-parser; needs StandardError for RuboCop::Error
	vm.registerFacter()           // Facter.value/[]/fact/add/to_hash/clear (require "facter"), backed by go-facter; per-VM Collection; Facter.add … setcode block runs INLINE under the GVL when the custom fact resolves
	vm.registerHocon()            // Hocon.parse / Hocon::ConfigFactory + Config accessors (require "hocon"), backed by go-ruby-hocon over go-hocon; needs StandardError for Hocon::ConfigError
	vm.registerConfd()            // Confd.render(template, vars) (require "confd"), backed by go-ruby-confd over abtreece/confd; seeds confd's in-memory backend from a Ruby Hash ("/"-path keys, nested Hashes flattened) and runs confd's real text/template engine (getv/getvs/exists/base64Encode/json/…); needs StandardError for Confd::Error
	vm.registerFastGettext()      // FastGettext translation module (require "fast_gettext"), backed by go-ruby-fast-gettext; per-VM Instance holds domains/locale; _/n_/s_/p_ helpers
	vm.registerDeepMerge()        // DeepMerge.deep_merge!/deep_merge + Hash#deep_merge!/#deep_merge (require "deep_merge"), backed by go-ruby-deep-merge; Ruby key identity preserved; bad option combos raise DeepMerge::InvalidParameter
	vm.registerRack()             // Rack::Request/Response/Utils (require "rack" / "rack/utils"), backed by go-ruby-rack; deterministic env/query/escape, no socket
	vm.registerWEBrick()          // WEBrick::HTTPServer/HTTPRequest/HTTPResponse/HTTPServlet/HTTPStatus (require "webrick"), backed by go-ruby-webrick; deterministic parse/build/mount-dispatch, no socket; the mount_proc block / servlet do_* method is the rbgo seam run inline under the GVL; needs StandardError (WEBrick::HTTPStatus family)
	vm.registerGrape()            // Grape::Router/Validator/Formatter (require "grape"), backed by go-ruby-grape; endpoint-block exec + Rack env are host seams; needs StandardError for Grape::Exceptions
	vm.registerSinatra()          // Sinatra::Base class-DSL + #call Rack adapter (require "sinatra/base"), backed by go-ruby-sinatra over go-ruby-rack; route/filter block eval is the rbgo seam; needs Rack + StandardError (Sinatra::NotFound), so registered after registerRack
	vm.registerRoda()             // Roda class routing-tree DSL (route do |r| … end) + #call Rack adapter (require "roda"), backed by go-ruby-roda over go-ruby-rack; the route/matcher block eval is the rbgo seam, the routing tree is the library; needs Rack + StandardError (Roda::RodaError)
	vm.registerAsync()            // Async{} structured concurrency + Async::Task/Barrier/Semaphore/Condition/Notification/Queue/LimitedQueue/Waiter (require "async"), backed by go-ruby-async on its CoScheduler; the task-body block eval is the rbgo seam; needs StandardError (Async::Stop / Async::TimeoutError)
	vm.registerWarden()           // Warden::Manager/Proxy/Strategies (require "warden"), backed by go-ruby-warden over go-ruby-rack; strategy valid?/authenticate! bodies are the rbgo seam; needs Rack + StandardError (Warden::NotAuthenticated), so registered after registerRack
	vm.registerOmniAuth()         // OmniAuth::Builder/Strategy/AuthHash + OmniAuth.config (require "omniauth"), backed by go-ruby-omniauth over go-ruby-rack; provider request_phase/uid/info bodies are the rbgo seam; needs Rack + StandardError (OmniAuth::Error), so registered after registerRack
	vm.registerActiveModel()      // ActiveModel::Validations mixin + Errors/Error + Naming/Name + Validator/EachValidator (require "active_model"), backed by go-ruby-activemodel; Attr/Dispatcher/Condition seams wired to rbgo (ivar get/set + method dispatch + Ruby if/unless procs)
	vm.registerAASM()             // AASM mixin: `include AASM` + `aasm do state/event/transitions … end` DSL, per-event fire/fire!/may_fire? + per-state predicates, obj.aasm reader (require "aasm"), backed by go-ruby-aasm; guards/callbacks run INLINE under the GVL, state/persist seams wired to the object's column ivar + save; needs StandardError for AASM::Error
	vm.registerActiveStorage()    // ActiveStorage::Blob/Service/Attachment + Attached::One/Many (require "active_storage"), backed by go-ruby-activestorage; ModelStore/Service/Signer/Random/Clock seams wired to a deterministic in-process config (MemStore + DiskService temp dir); needs StandardError for ActiveStorage::Error
	vm.registerActionCable()      // ActionCable::Server/Connection::Base/Channel::Base + ActionCable.server (require "action_cable"), backed by go-ruby-actioncable; the transport/channel-action/factory seams eval Ruby bodies + the async adapter default keep the byte-exact actioncable-v1-json wire protocol in the library; needs StandardError (ActionCable::Error)
	vm.registerActionPack()       // ActionController::Base + ActionController::Parameters + ActionDispatch::Routing::RouteSet/Request/Response (require "action_controller" / "action_dispatch"), backed by go-ruby-actionpack; the action body (RunAction), the before/after/around filters, rescue_from and the view render (Renderer -> an ActionView-style context) are the rbgo seams run inline under the GVL; the route compiler/recognizer/generator, filter/rescue pipeline and strong-params semantics are the library; needs StandardError + Rack, so registered after registerRack/registerActionCable
	vm.registerDevise()           // Devise::Config/Resource/Encryptor/TokenGenerator + Strategies::DatabaseAuthenticatable (require "devise"), backed by go-ruby-devise over go-ruby-bcrypt + go-ruby-warden; the model #[]/#[]=/#save + finder callable are the rbgo seams, and the strategy plugs into the bound Warden registry; needs StandardError (Devise::Error). Registered after registerWarden + registerBCrypt (dependency order)
	vm.registerActiveRecord()     // ActiveRecord::Model/Relation/Record + Base.establish_connection (require "active_record"), backed by go-ruby-activerecord; adapter seam wired to go-ruby-sqlite3 so queries run; needs StandardError for ActiveRecordError
	vm.registerKaminari()         // Kaminari::PaginatableArray/PaginatableRelation + Array#page + Model/relation #page (require "kaminari"), backed by go-ruby-kaminari; the Relation seam wires #count/#offset/#limit to an ActiveRecord relation, so registered after registerActiveRecord
	vm.registerPaperTrail()       // PaperTrail::Version/RecordTrail/Request + has_paper_trail macro (require "paper_trail"), backed by go-ruby-paper-trail; snapshot=model @ivars, store=shared MemoryStore, clock=time.Now, whodunnit/enabled=PaperTrail.request seams
	vm.registerRansack()          // Ransack module + Ransack::Search/Sort (require "ransack"), backed by go-ruby-ransack; Model.ransack(q).result / Ransack.search(subject, q); the record-source Backend seam fetches the subject's rows via #all and runs the engine's in-memory Match/Apply evaluator; must run after registerActiveRecord so ActiveRecord::Base gains .ransack/.search
	vm.registerRQRCode()          // RQRCode::QRCode (require "rqrcode"), backed by go-ruby-rqrcode; needs StandardError for RQRCode::QRCode*Error
	vm.registerPagy()             // Pagy (require "pagy"), backed by go-ruby-pagy; needs StandardError for Pagy::OverflowError / Pagy::VariableError
	vm.registerDotenv()           // Dotenv module (require "dotenv"), backed by go-ruby-dotenv; wires ENV read/write + shell seams
	vm.registerHCL2()             // HCL2 module (require "hcl2"), backed by go-ruby-hcl2; needs StandardError for HCL2::Error
	vm.registerI18n()             // I18n module (require "i18n"), backed by go-ruby-i18n; needs ArgumentError for the I18n::ArgumentError tree
	vm.registerZeitwerk()         // Zeitwerk::Loader/Inflector (require "zeitwerk"), backed by go-ruby-zeitwerk; wires DefineAutoload->Module#autoload, Load->require; needs StandardError/NameError for the Zeitwerk error tree
	vm.registerRSS()              // RSS::Parser.parse -> RSS::Rss / RSS::RDF / RSS::Atom::Feed (require "rss"), backed by go-ruby-rss; needs StandardError for RSS::Error
	vm.registerRDoc()             // RDoc::Markup + ToHtml/ToMarkdown/ToRdoc formatters (require "rdoc"), backed by go-ruby-rdoc; needs StandardError for RDoc::Error
	vm.registerThor()             // Thor CLI framework: option parsing + dispatch + help (require "thor"), backed by go-ruby-thor; needs StandardError for Thor::Error
	vm.registerRacc()             // Racc::Parser LALR(1) runtime (require "racc/parser"), backed by go-ruby-racc; needs StandardError for Racc::ParseError
	vm.registerKramdown()         // Kramdown::Document (require "kramdown"), backed by go-kramdown
	vm.registerImages()           // Images::Image/Canvas/Color (require "images"), backed by go-ruby-images (MiniMagick/ruby-vips processing + chunky_png canvas + scikit-image ops); needs StandardError for Images::Error
	vm.registerOpentype()         // Opentype module + Opentype::Font/Face (require "opentype"), backed by go-ruby-opentype over the go-opentype text stack (font parsing, sized faces, complex-script shaping, Unicode Bidi); every adapter method installed via its Methods()/Call() surface; needs StandardError for Opentype::Error
	vm.registerWidgets()          // Widgets module (require "widgets"), backed by go-ruby-widgets/widgets over the go-widgets pixel-blitting toolkit: constructors + flex/fixed/grid/border/card composition + layout/render (RGBA buffer)/dispatch, addressed by integer handles; needs StandardError for Widgets::Error
	vm.registerTui()              // Tui module + Tui::Widget (require "tui"), backed by go-ruby-widgets/tui over the go-widgets terminal-cell toolkit: constructors/composition + render (ANSI)/render_cells/decode_cells/dispatch; needs StandardError for Tui::Error
	vm.registerMvvm()             // Mvvm module + Mvvm::Observable/Command/ObservableList (require "mvvm"), backed by go-ruby-widgets/mvvm over the go-widgets data-binding layer; notifications pull through drain_events; needs StandardError for Mvvm::Error
	vm.registerShrine()           // Shrine file-attachment (require "shrine"), backed by go-ruby-shrine; Storage(Memory/FileSystem)/Uploader/UploadedFile/Attacher; needs StandardError for Shrine::Error + StringIO for #open
	vm.registerLiquid()           // Liquid::Template.parse(...).render (require "liquid"), backed by go-liquid; needs StandardError for Liquid::Error
	vm.registerSass()             // Sass.compile / compile_string / compile_file + SassC::Engine (require "sass"), backed by go-ruby-sass over the pure-Go go-scss engine; needs StandardError for Sass::CompileError / SassC::SyntaxError
	vm.registerRouge()            // Rouge.highlight / Rouge::Lexer.find (require "rouge"), backed by go-rouge; needs StandardError for Rouge::Error
	vm.registerSlim()             // Slim::Template.new{src}.render (require "slim"), compile-to-source via go-ruby-slim; needs StandardError for Slim::Error
	vm.registerHaml()             // Haml::Template.new(src).render (require "haml"), compile-to-source via go-ruby-haml; needs StandardError for Haml::Error
	vm.registerDryTypes()         // Dry::Types type system (require "dry/types"), backed by go-ruby-dry-types; needs StandardError for Dry::Types::CoercionError
	vm.registerDryStruct()        // Dry::Struct base class (require "dry/struct"), backed by go-ruby-dry-struct; pins go-ruby-dry-types
	vm.registerDryValidation()    // Dry::Schema / Dry::Validation::Contract (require "dry/validation"), backed by go-ruby-dry-validation; pins go-ruby-dry-types
	vm.registerOAuth2()           // OAuth2::Client / AccessToken / Response (require "oauth2"), backed by go-ruby-oauth2; HTTP round-trip is a net-http host seam
	vm.registerOIDC()             // OpenIDConnect::Client / Verifier / ProviderMetadata (require "openid_connect" / "oidc"), backed by go-ruby-oidc; reuses OAuth2+JWT; HTTP seam is a Ruby callable Doer
	vm.registerOpenStack()        // OpenStack.connect -> OpenStack::Connection + the six service accessors (compute/network/block_storage/object_storage/image/identity) with per-resource CRUD (require "openstack"), backed by go-ruby-openstack over gophercloud; resources cross as Ruby Hashes, the HTTP transport is the vm.osTransport injection seam; needs StandardError for the OpenStack::Error tree
	vm.registerFileUtils()        // FileUtils (real fs ops over os); needs Errno (registerFile)
	vm.registerGetoptLong()       // GetoptLong loadable shell; needs StandardError
	vm.registerSignal()           // Signal.trap/list/signame + Kernel#trap (handlers recorded, not delivered)
	vm.registerOpen3()            // Open3 loadable shell (popen/capture raise; spawning pending)
	vm.registerRipper()           // ripper loadable shell (Ripper.sexp etc. raise); needs StandardError
	vm.registerSyslog()           // Syslog loadable shell (feature probe)
	vm.registerCGI()              // CGI.escape/unescape (real over net/url) + HTML helpers
	vm.registerERB()              // ERB class skeleton + ERB::Util (backed by go-ruby-erb); prelude adds the Ruby API
	vm.registerIRB()              // IRB module + REPL (require "irb"), backed by go-ruby-irb; evaluates each statement through rbgo; needs Binding + IO
	vm.registerStringScanner()    // StringScanner (require "strscan"), backed by go-ruby-strscan; needs StandardError
	vm.registerOptionParser()     // OptionParser (require "optparse"), backed by go-ruby-optparse; prelude adds the ParseError tree
	vm.registerURI()              // URI module (require "uri"), backed by go-ruby-uri; needs StandardError (URI::Error) + Regexp (make_regexp)
	vm.registerCSV()              // CSV class (require "csv"), backed by go-ruby-csv; needs StandardError (CSV::MalformedCSVError) + Date/Time
	vm.registerREXML()            // REXML module (require "rexml/document"), backed by go-ruby-rexml; needs StandardError (REXML::ParseException)
	vm.registerMatrix()           // Matrix/Vector (require "matrix"), backed by go-ruby-matrix; needs StandardError (ExceptionForMatrix::Err*)
	vm.registerArrow()            // Arrow module (require "arrow"), backed by go-ruby-arrow; needs StandardError (Arrow::Error)
	vm.registerShellwords()       // Shellwords (require "shellwords"), backed by go-ruby-shellwords; installs Shellwords + String/Array core extensions lazily on require
	vm.registerMonitor()          // Monitor/MonitorMixin (single-thread synchronize); needs StandardError
	vm.registerObservable()       // Observable mixin (require "observer"), backed by go-ruby-observer; rbgo wires dispatch + respond_to?
	vm.registerZlib()             // needs the exception hierarchy (Zlib::Error < StandardError)
	vm.registerFiber()            // needs the exception hierarchy (FiberError < StandardError)
	vm.registerThread()           // needs StandardError/StopIteration (ThreadError, ClosedQueueError)
	vm.registerServerGems()       // browser-pointless server/ops/testing gems, guarded off on GOOS=js GOARCH=wasm (see servergems_*.go)

	// Exception instance protocol: initialize stores @message; message/to_s
	// return it (or the class name when unset).
	cException.define("initialize", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		// A nil (or absent) argument leaves @message unset, so #message falls back
		// to the class name (Exception.new(nil).message == "Exception"); any other
		// argument is coerced to its string form via #to_s.
		if len(args) > 0 && !object.IsNil(args[0]) {
			setIvar(self, "@message", object.NewString(vm.exceptionMessageArg(args[0])))
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
	// backtrace_locations: the captured frames as Thread::Backtrace::Location
	// value objects (parsed from the stored "path:lineno:in 'label'" strings), or
	// nil when the exception has never been raised — matching MRI.
	cException.define("backtrace_locations", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		bt, ok := getIvar(self, backtraceIvar).(*object.Array)
		if !ok {
			return object.NilV
		}
		locs := make([]object.Value, len(bt.Elems))
		for i, f := range bt.Elems {
			locs[i] = vm.backtraceLocation(f.ToS())
		}
		return object.NewArrayFromSlice(locs)
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
	// NameError#name: the name (Symbol) that could not be resolved, stamped into
	// the exception's @name ivar at raise time (see raiseNameError, invoked by the
	// default Module#const_missing). nil when no name was recorded.
	vm.consts["NameError"].(*RClass).define("name", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return getIvar(self, "@name")
	})
	// full_message(highlight:, order:): the MRI-shaped multi-line report. order:
	// :top (default) leads with the raise-site frame + detailed message then the
	// "\tfrom <frame>" tail; order: :bottom prints a "Traceback (most recent call
	// last):" header, the frames outermost-first, and the raise-site + message
	// last. highlight: true wraps the message and class name in ANSI bold/underline
	// (the terminal form); highlight defaults to false here (no TTY).
	cException.define("full_message", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		highlight, order := parseFullMessageOpts(args)
		return object.NewString(vm.exceptionFullMessage(self, highlight, order))
	})
	// detailed_message(highlight:, **opts): "<message> (<ClassName>)", the body
	// full_message embeds. highlight: true adds the ANSI emphasis; extra keywords
	// are accepted and ignored (MRI passes library-specific opts through).
	cException.define("detailed_message", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		highlight, _ := parseFullMessageOpts(args)
		return object.NewString(vm.exceptionDetailedMessage(self, highlight))
	})

	vm.registerExceptionMethods(cException)

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
	// #initialize and #method_missing are defined on BasicObject (as in MRI), so a
	// class descending directly from BasicObject — which does not inherit Object —
	// still allocates (new → initialize) and reports NoMethodError through
	// method_missing rather than dereferencing a nil method record.
	vm.cBasicObject.define("initialize", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NilV
	})
	// ! and != are BasicObject instance methods (dispatchable via send, and
	// overridable), alongside the operators the bytecode also lowers them to.
	// !obj is the boolean negation of obj's truthiness; a != b is !(a == b),
	// dispatching == so a user-defined == is honoured.
	vm.cBasicObject.define("!", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(!self.Truthy())
	})
	vm.cBasicObject.define("!=", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(!vm.send(self, "==", args, nil).Truthy())
	})
	vm.cBasicObject.define("method_missing", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		// args[0] is the missing method name (a Symbol), args[1:] the call's own
		// arguments. Stamp them so NoMethodError#name/#receiver/#args report the
		// failed call, as MRI.
		nameSym, ok := args[0].(object.Symbol)
		if !ok {
			nameSym = object.Symbol(args[0].ToS())
		}
		var callArgs object.Value
		if len(args) > 1 {
			callArgs = object.NewArrayFromSlice(append([]object.Value(nil), args[1:]...))
		}
		vm.raiseWithIvars("NoMethodError",
			"undefined method '"+string(nameSym)+"' for "+vm.classOf(self).name,
			map[string]object.Value{"@name": nameSym, "@receiver": self, "@args": callArgs})
		return object.NilV
	})
	// initialize and method_missing are PRIVATE instance methods of BasicObject
	// (as in MRI): callable via new/super and the dispatch fallback, never listed
	// in instance_methods, only in private_instance_methods.
	vm.setInstanceVisibility(vm.cBasicObject, "initialize", visPrivate)
	vm.setInstanceVisibility(vm.cBasicObject, "method_missing", visPrivate)
	isAFn := func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		target := classArg(args[0])
		if classIsA(vm.classOf(self), target) {
			return object.True
		}
		// A class/module that was `extend`ed with a module M (directly, or on any
		// of its superclasses) is_a? M, because M is mixed into the receiver's
		// singleton (meta) class ancestry. classOf returns Class/Module for an
		// RClass, so the metaclass chain must be consulted separately. This also
		// covers per-object singleton-class extends for non-class receivers.
		if classSingletonIsA(vm, self, target) {
			return object.True
		}
		return object.False
	}
	vm.cObject.define("is_a?", isAFn)
	vm.cObject.define("kind_of?", isAFn)
	vm.cObject.define("instance_of?", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(vm.classOf(self) == classArg(args[0]))
	})
	vm.cObject.define("raise", nativeRaise)
	vm.cObject.define("Integer", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		args, doRaise := popExceptionKwarg(args)
		// fail either raises (the default) or, under `exception: false`, yields nil.
		fail := func(class, format string, a ...interface{}) object.Value {
			if doRaise {
				raise(class, format, a...)
			}
			return object.NilV
		}
		base := 0 // no explicit base: auto-detect a 0x/0b/0o/0 prefix (and allow _)
		hasBase := len(args) > 1
		if hasBase {
			// The base itself is coerced with #to_int, and a valid radix is 0
			// (auto-detect) or 2..36 — anything else is MRI's "invalid radix".
			base = int(vm.toIntCoerce(args[1]))
			if base != 0 && (base < 2 || base > 36) {
				return fail("ArgumentError", "invalid radix %d", base)
			}
		}
		// A base only makes sense for a String (or #to_str) value; giving one for a
		// numeric argument is an ArgumentError in MRI. nil is rejected as a
		// TypeError first, matching MRI's ordering.
		if hasBase {
			switch args[0].(type) {
			case object.Nil:
				return fail("TypeError", "can't convert nil into Integer")
			case object.Integer, *object.Bignum, object.Float:
				return fail("ArgumentError", "base specified for non string value")
			}
		}
		switch v := args[0].(type) {
		case object.Nil:
			// nil defines #to_i (→ 0) but Integer(nil) still raises, so it is
			// rejected before the coercion protocol below would call it.
			return fail("TypeError", "can't convert nil into Integer")
		case object.Integer:
			return v
		case *object.Bignum:
			return v
		case object.Float:
			// A non-finite Float has no Integer value: Integer(Infinity)/Integer(NaN)
			// is a FloatDomainError in MRI (suppressed to nil under exception: false).
			f := float64(v)
			if math.IsInf(f, 0) {
				if f > 0 {
					return fail("FloatDomainError", "Infinity")
				}
				return fail("FloatDomainError", "-Infinity")
			}
			if math.IsNaN(f) {
				return fail("FloatDomainError", "NaN")
			}
			// A Float beyond int64's range truncates to a Bignum (int64(f) would
			// overflow to a bogus value), matching Integer(2e100).
			if f >= -9223372036854775808.0 && f < 9223372036854775808.0 {
				return object.IntValue(int64(f))
			}
			bi, _ := new(big.Float).SetFloat64(f).Int(nil)
			return object.NormInt(bi)
		case *object.String:
			if r, ok := intFromString(v.Str(), base); ok {
				return r
			}
			return fail("ArgumentError", "invalid value for Integer(): %s", v.Inspect())
		}
		// Any other object converts through MRI's protocol: #to_int (must yield an
		// Integer), else #to_str (parsed like a String literal), else #to_i.
		other := args[0]
		if vm.respondsToDynamic(other, "to_int") {
			if r := vm.send(other, "to_int", nil, nil); isIntegerVal(r) {
				return r
			}
		}
		if vm.respondsToDynamic(other, "to_str") {
			if s, ok := vm.send(other, "to_str", nil, nil).(*object.String); ok {
				if r, ok := intFromString(s.Str(), base); ok {
					return r
				}
				return fail("ArgumentError", "invalid value for Integer(): %s", s.Inspect())
			}
		}
		if vm.respondsToDynamic(other, "to_i") {
			r := vm.send(other, "to_i", nil, nil)
			if isIntegerVal(r) {
				return r
			}
			// #to_i answered with a non-Integer: MRI names the offending result's
			// class in the message ("... to Integer (X#to_i gives NilClass)").
			return fail("TypeError", "can't convert %s to Integer (%s#to_i gives %s)",
				vm.convErrName(other), vm.convErrName(other), vm.classOf(r).name)
		}
		return fail("TypeError", "can't convert %s into Integer", vm.convErrName(other))
	})
	vm.cObject.define("Float", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		args, doRaise := popExceptionKwarg(args)
		fail := func(class, format string, a ...interface{}) object.Value {
			if doRaise {
				raise(class, format, a...)
			}
			return object.NilV
		}
		switch v := args[0].(type) {
		case object.Nil:
			// nil defines #to_f (→ 0.0) but Float(nil) still raises, so it is
			// rejected before the coercion protocol below would call it.
			return fail("TypeError", "can't convert nil into Float")
		case object.Float:
			return v
		case object.Integer:
			return object.Float(float64(v))
		case *object.Bignum:
			f, _ := new(big.Float).SetInt(v.I).Float64()
			return object.Float(f)
		case *object.String:
			norm, ok := normalizeFloatLiteral(v.Str())
			if !ok {
				// An illegal underscore that Go's ParseFloat would nonetheless accept
				// (e.g. one adjacent to the 0x prefix) — MRI rejects it.
				return fail("ArgumentError", "invalid value for Float(): %s", v.Inspect())
			}
			f, err := strconv.ParseFloat(norm, 64)
			if err != nil {
				// An out-of-range literal is not malformed: MRI yields ±Infinity
				// (overflow) or 0.0 (underflow), which is exactly what ParseFloat
				// returns alongside ErrRange.
				if ne, ok := err.(*strconv.NumError); ok && ne.Err == strconv.ErrRange {
					return object.Float(f)
				}
				return fail("ArgumentError", "invalid value for Float(): %s", v.Inspect())
			}
			return object.Float(f)
		}
		// Any other object converts through MRI's protocol via #to_f.
		other := args[0]
		if vm.respondsToDynamic(other, "to_f") {
			if f, ok := vm.send(other, "to_f", nil, nil).(object.Float); ok {
				return f
			}
		}
		return fail("TypeError", "can't convert %s into Float", vm.convErrName(other))
	})
	vm.cObject.define("String", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.send(args[0], "to_s", nil, nil)
	})
	vm.cObject.define("Array", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		switch v := args[0].(type) {
		case object.Nil:
			return object.NewArray()
		case *object.Array:
			return v
		default:
			return object.NewArray(v)
		}
	})
	sendFn := func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		return vm.send(self, vm.methodNameArg(args[0]), args[1:], blk)
	}
	vm.cObject.define("send", sendFn)
	// __send__ is the can't-be-overridden alias of send; both ignore visibility.
	vm.cBasicObject.define("__send__", sendFn)
	// public_send dispatches only public methods: a private/protected target
	// raises NoMethodError just as an explicit-receiver call would.
	vm.cObject.define("public_send", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		name := vm.methodNameArg(args[0])
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
			// A public method (or any method when include_private is truthy) answers
			// respond_to? immediately. A private/protected method that is not accessible
			// falls through to respond_to_missing?, matching MRI — which gives the
			// object the same dynamic-dispatch chance it gets for a missing method.
			if includePrivate.Truthy() || vm.sendVisibilityOf(self, name, m) == visPublic {
				return object.True
			}
		}
		// Fall back to respond_to_missing?(name, include_private): a truthy return
		// means the object answers the method dynamically (method_missing).
		if vm.findMethod(self, "respond_to_missing?") != nil {
			return object.Bool(vm.send(self, "respond_to_missing?", []object.Value{object.Symbol(name), includePrivate}, nil).Truthy())
		}
		return object.False
	})
	// respond_to_missing?(name, include_private=false) is Kernel's default hook: a
	// private method returning false that user classes override to answer methods
	// handled by method_missing. Defining it (rather than leaving it absent) makes
	// `undef`/`alias`/introspection of it resolve, as in MRI where it is a real
	// private Kernel method; the default false keeps respond_to?'s result unchanged.
	vm.cObject.define("respond_to_missing?", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.False
	})
	vm.setInstanceVisibility(vm.cObject, "respond_to_missing?", visPrivate)
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
	vm.cBasicObject.define("==", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(rubyEqual(self, args[0]))
	})
	// Default <=>: 0 when the two are the same object, nil otherwise — the MRI
	// Object#<=>. It compares by identity (like #equal?) rather than sending #==,
	// because a class that includes Comparable defines #== AS `(self <=> other)==0`;
	// sending #== here would recurse into that #== and back into <=> forever.
	vm.cObject.define("<=>", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if self == args[0] {
			return object.IntValue(0)
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
		return object.NewArrayFromSlice(out)
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
		return object.False
	})
	vm.cModule.define("name", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		if c := self.(*RClass); c.name != "" {
			return object.NewString(c.name)
		}
		return object.NilV // anonymous class/module
	})
	vm.cModule.define("instance_methods", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		all := len(args) == 0 || args[0].Truthy() // instance_methods(false) = own only
		// MRI's instance_methods lists the public and protected methods, never the
		// private ones (those are private_instance_methods).
		return object.NewArrayFromSlice(vm.methodNamesMatching(self.(*RClass), all,
			func(v visibility) bool { return v != visPrivate }))
	})
	// Module#const_get / #const_defined? live in module_residuals.go
	// (registerModuleResiduals), which supports scoped paths, the inherit flag,
	// #to_str coercion and the #const_missing hook.
	// Class-variable reflection. Names arrive as a Symbol or String (e.g. :@@x);
	// the @@-prefixed name is the key in the cvars table. Lookups walk the
	// superclass chain via cvarOwner, mirroring how @@name resolves at runtime.
	vm.cModule.define("class_variable_get", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		name := cvarNameArg(args[0])
		if c := cvarOwner(self.(*RClass), name); c != nil {
			return c.cvars[name]
		}
		raise("NameError", "uninitialized class variable %s in %s", name, self.(*RClass).name)
		return object.NilV
	})
	vm.cModule.define("class_variable_set", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		name := cvarNameArg(args[0])
		cls := self.(*RClass)
		if c := cvarOwner(cls, name); c != nil {
			c.cvars[name] = args[1]
		} else {
			cls.cvars[name] = args[1]
		}
		return args[1]
	})
	vm.cModule.define("class_variable_defined?", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(cvarOwner(self.(*RClass), cvarNameArg(args[0])) != nil)
	})
	vm.cModule.define("class_variables", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		// class_variables(inherit=true): own variables, then ancestors', each
		// name only once and in first-seen order. inherit=false stops at self.
		inherit := len(args) == 0 || args[0].Truthy()
		seen := map[string]bool{}
		var names []string
		for c := self.(*RClass); c != nil; c = c.super {
			level := make([]string, 0, len(c.cvars))
			for name := range c.cvars {
				if !seen[name] {
					seen[name] = true
					level = append(level, name)
				}
			}
			// Go map iteration is unordered; sort within each level so the result
			// is deterministic (windows-latest runs the same test).
			sort.Strings(level)
			names = append(names, level...)
			if !inherit {
				break
			}
		}
		out := make([]object.Value, len(names))
		for i, n := range names {
			out[i] = object.Symbol(n)
		}
		return object.NewArrayFromSlice(out)
	})
	vm.cModule.define("const_set", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		name := constNameArg(args[0])
		cls := self.(*RClass)
		// Route through assignConstIn so an anonymous class/module bound here gains
		// the qualified name of its constant (Ruby's "permanent name on first
		// constant binding" rule) — the same path a `Foo::Bar = ...` literal takes.
		// Top-level (Object) constants live in the flat namespace that a bare
		// constant reference reads, which assignConstIn handles for Object.
		vm.assignConstIn(cls, name, args[1])
		return args[1]
	})
	// Module#remove_const deletes a constant defined directly on the receiver and
	// returns its value, raising NameError when it is absent (MRI's contract). It
	// only ever touches the receiver's own table — not ancestors — so a class
	// generator that redefines a constant (Puppet's classgen does this) removes the
	// stale binding before installing the new one.
	vm.cModule.define("remove_const", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		name := constNameArg(args[0])
		cls := self.(*RClass)
		table := cls.consts
		if cls == vm.cObject {
			table = vm.consts
		}
		val, ok := table[name]
		if !ok {
			return raise("NameError", "constant %s::%s not defined", cls.name, name)
		}
		delete(table, name)
		return val
	})
	// Module#constants returns the names (as symbols) of the constants defined
	// directly in the module/class. With inherit (the default, true) the constants
	// of ancestor modules/classes are included too, excluding Object's. Names are
	// returned in sorted order; MRI leaves the order unspecified, and sorting keeps
	// results deterministic across platforms.
	vm.cModule.define("constants", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		inherit := true
		if len(args) > 0 {
			inherit = args[0].Truthy()
		}
		seen := map[string]bool{}
		var names []string
		add := func(c *RClass) {
			for name := range c.consts {
				if !seen[name] {
					seen[name] = true
					names = append(names, name)
				}
			}
		}
		cls := self.(*RClass)
		if inherit {
			for _, anc := range vm.ancestors(cls) {
				if anc == vm.cObject || anc == vm.cBasicObject {
					continue
				}
				add(anc)
			}
		} else {
			add(cls)
		}
		sort.Strings(names)
		out := make([]object.Value, len(names))
		for i, n := range names {
			out[i] = object.Symbol(n)
		}
		return object.NewArrayFromSlice(out)
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
			return object.IntValue(0)
		case classIsA(a, b):
			return object.IntValue(-1)
		case classIsA(b, a):
			return object.IntValue(1)
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
	vm.cModule.define("attr_reader", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewArrayFromSlice(vm.defineAttrs(self.(*RClass), args, true, false))
	})
	vm.cModule.define("attr", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		// Module#attr(:name, writable=false) is the deprecated boolean form when
		// exactly two arguments are given and the second is true/false; otherwise
		// every argument is an attribute name (a reader each), like attr_reader.
		cls := self.(*RClass)
		if len(args) == 2 {
			if b, ok := args[1].(object.Bool); ok {
				return object.NewArrayFromSlice(vm.defineAttrs(cls, args[:1], true, bool(b)))
			}
		}
		return object.NewArrayFromSlice(vm.defineAttrs(cls, args, true, false))
	})
	vm.cModule.define("attr_writer", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewArrayFromSlice(vm.defineAttrs(self.(*RClass), args, false, true))
	})
	vm.cModule.define("attr_accessor", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewArrayFromSlice(vm.defineAttrs(self.(*RClass), args, true, true))
	})
	classEvalFn := func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		// String form — class_eval("def m; end", file, line) — compiles the source
		// and runs it with the class as both self and the method-definition target,
		// so a `def` in the source becomes one of the class's instance methods. The
		// optional file/line trailing arguments are accepted for MRI compatibility
		// (they steer error reporting only) and otherwise ignored.
		if blk == nil {
			if len(args) > 0 {
				if s, ok := args[0].(*object.String); ok {
					return vm.classEvalString(self.(*RClass), string(s.Bytes()))
				}
			}
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
				cm.origName = methodOriginalName(src.m)
				cm.name, cm.owner = name, cls
				cls.methods[name] = &cm
				bumpMethodSerial()
				return object.Symbol(name)
			case *UnboundMethod:
				cm := *src.m
				cm.origName = methodOriginalName(src.m)
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
		return object.IntValue(int64(strings.Compare(symStr(self), string(o))))
	})
	symLen := func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(utf8.RuneCountInString(symStr(self))))
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
		return object.IntValue(int64(strings.Compare(a.Str(), b.Str())))
	})

	// String. Methods over the mutable byte-based String (length/chars/index are
	// rune-aware for UTF-8). strOf reads the receiver's current contents.
	strOf := func(self object.Value) string { return self.(*object.String).Str() }
	// strEncOf builds a result String carrying the receiver's encoding, for the
	// transforms (upcase, reverse, strip, …) that MRI keeps in the same encoding
	// as self. An empty Enc is the UTF-8 default, so UTF-8 receivers are unaffected.
	strEncOf := func(self object.Value, s string) object.Value {
		return object.NewStringBytesEnc([]byte(s), self.(*object.String).Enc)
	}
	strLen := func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		// A binary (ASCII-8BIT) string counts bytes; otherwise characters.
		s := self.(*object.String)
		if s.IsBinary() {
			return object.IntValue(int64(len(s.Bytes())))
		}
		return object.IntValue(int64(utf8.RuneCountInString(string(s.Bytes()))))
	}
	vm.cString.define("length", strLen)
	// String#size is a genuine alias of String#length (shared record), so
	// "abc".method(:size) == "abc".method(:length), matching MRI.
	aliasBuiltin(vm.cString, "size", "length")
	vm.cString.define("bytesize", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(len(strOf(self))))
	})
	vm.cString.define("empty?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(len(strOf(self)) == 0)
	})
	vm.cString.define("upcase", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return strEncOf(self, strings.ToUpper(strOf(self)))
	})
	vm.cString.define("downcase", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return strEncOf(self, strings.ToLower(strOf(self)))
	})
	vm.cString.define("casecmp", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o, ok := args[0].(*object.String)
		if !ok { // like <=>, a non-String operand compares to nil
			return object.NilV
		}
		return object.IntValue(int64(strings.Compare(strings.ToLower(strOf(self)), strings.ToLower(o.Str()))))
	})
	vm.cString.define("casecmp?", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o, ok := args[0].(*object.String)
		if !ok {
			return object.NilV
		}
		return object.Bool(strings.EqualFold(strOf(self), o.Str()))
	})
	vm.cString.define("capitalize", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return strEncOf(self, capitalizeStr(strOf(self)))
	})
	vm.cString.define("swapcase", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return strEncOf(self, swapcaseStr(strOf(self)))
	})
	vm.cString.define("reverse", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return strEncOf(self, reverseStr(strOf(self)))
	})
	succStr := func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(succString(strOf(self)))
	}
	vm.cString.define("succ", succStr)
	vm.cString.define("next", succStr)
	succBang := func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		s := self.(*object.String)
		vm.checkFrozen(s)
		s.SetBytes([]byte(succString(s.Str())))
		return s
	}
	vm.cString.define("succ!", succBang)
	vm.cString.define("next!", succBang)
	vm.cString.define("chr", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(stringChr(strOf(self)))
	})
	vm.cString.define("setbyte", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		s := self.(*object.String)
		vm.checkFrozen(s)
		b := s.MutableBytes()
		i := toInt(args[0])
		if i < 0 {
			i += int64(len(b)) // negative indexes count from the end
		}
		if i < 0 || i >= int64(len(b)) {
			raise("IndexError", "index %d out of string", toInt(args[0]))
		}
		b[i] = byte(toInt(args[1]))
		return args[1]
	})
	vm.cString.define("sum", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		bits := 16
		if len(args) > 0 {
			bits = int(toInt(args[0]))
		}
		return object.IntValue(stringSum(strOf(self), bits))
	})
	vm.cString.define("upto", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		if blk == nil {
			return enumFor(self, "upto", args...)
		}
		excl := len(args) > 1 && truthyValue(args[1])
		stringUpto(strOf(self), strArg(args[0]), excl, func(cur string) {
			vm.callBlock(blk, []object.Value{object.NewString(cur)})
		})
		return self
	})
	vm.cString.define("strip", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return strEncOf(self, strings.Trim(strOf(self), wsCutset))
	})
	vm.cString.define("lstrip", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return strEncOf(self, strings.TrimLeft(strOf(self), wsCutset))
	})
	vm.cString.define("rstrip", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return strEncOf(self, strings.TrimRight(strOf(self), wsCutset))
	})
	vm.cString.define("chomp", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return strEncOf(self, chompSep(strOf(self), args))
	})
	vm.cString.define("chop", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return strEncOf(self, chopStr(strOf(self)))
	})
	vm.cString.define("chars", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		enc := self.(*object.String).Enc // each character keeps the receiver's encoding
		if blk != nil {
			// The block form yields each character and returns the receiver (MRI).
			for _, r := range strOf(self) {
				vm.callBlock(blk, []object.Value{object.NewStringViewEnc(string(r), enc)})
			}
			return self
		}
		var out []object.Value
		for _, r := range strOf(self) {
			out = append(out, object.NewStringViewEnc(string(r), enc))
		}
		return object.NewArrayFromSlice(out)
	})
	vm.cString.define("bytes", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		s := strOf(self)
		out := make([]object.Value, len(s))
		for i := 0; i < len(s); i++ {
			out[i] = object.IntValue(int64(s[i]))
		}
		return object.NewArrayFromSlice(out)
	})
	vm.cString.define("getbyte", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		s := strOf(self)
		i := toInt(args[0])
		if i < 0 {
			i += int64(len(s)) // negative indexes count from the end
		}
		if i < 0 || i >= int64(len(s)) {
			return object.NilV
		}
		return object.IntValue(int64(s[i]))
	})
	vm.cString.define("byteslice", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return byteslice(self.(*object.String), args)
	})
	vm.cString.define("lines", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		segs := vm.stringLineSegs(self, args)
		if blk != nil { // with a block #lines yields each line and returns self
			for _, seg := range segs {
				vm.callBlock(blk, []object.Value{seg})
			}
			return self
		}
		return object.NewArrayFromSlice(segs)
	})
	vm.cString.define("each_line", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		if blk == nil {
			return enumFor(self, "each_line", args...)
		}
		for _, seg := range vm.stringLineSegs(self, args) {
			vm.callBlock(blk, []object.Value{seg})
		}
		return self
	})
	vm.cString.define("each_char", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			return enumFor(self, "each_char")
		}
		enc := self.(*object.String).Enc // each character keeps the receiver's encoding
		for _, r := range strOf(self) {
			vm.callBlock(blk, []object.Value{object.NewStringViewEnc(string(r), enc)})
		}
		return self
	})
	vm.cString.define("grapheme_clusters", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		s := self.(*object.String)
		pieces := graphemeClusters(s.Str())
		out := make([]object.Value, len(pieces))
		for i, g := range pieces {
			out[i] = graphemePiece(g, s.Enc)
		}
		return object.NewArrayFromSlice(out)
	})
	vm.cString.define("each_grapheme_cluster", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			return enumFor(self, "each_grapheme_cluster")
		}
		s := self.(*object.String)
		for _, g := range graphemeClusters(s.Str()) {
			vm.callBlock(blk, []object.Value{graphemePiece(g, s.Enc)})
		}
		return self
	})
	vm.cString.define("byteindex", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.strByteindex(self.(*object.String), args)
	})
	vm.cString.define("byterindex", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.strByterindex(self.(*object.String), args)
	})
	vm.cString.define("bytesplice", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.strBytesplice(self.(*object.String), args)
	})
	vm.cString.define("each_byte", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			return enumFor(self, "each_byte")
		}
		s := strOf(self)
		for i := 0; i < len(s); i++ {
			vm.callBlock(blk, []object.Value{object.IntValue(int64(s[i]))})
		}
		return self
	})
	vm.cString.define("each_codepoint", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			return enumFor(self, "each_codepoint")
		}
		for _, r := range strOf(self) {
			vm.callBlock(blk, []object.Value{object.IntValue(int64(r))})
		}
		return self
	})
	vm.cString.define("codepoints", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		var out []object.Value
		for _, r := range strOf(self) {
			out = append(out, object.IntValue(int64(r)))
		}
		return object.NewArrayFromSlice(out)
	})
	vm.cString.define("split", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		res := vm.stringSplit(strOf(self), self.(*object.String).Enc, args)
		if blk == nil {
			return res
		}
		// The block form yields each split substring and returns the receiver
		// itself (MRI); an empty receiver yields nothing and still returns self.
		for _, sub := range res.(*object.Array).Elems {
			vm.callBlock(blk, []object.Value{sub})
		}
		return self
	})
	vm.cString.define("include?", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(strings.Contains(strOf(self), vm.strPatternCompat(self, args[0])))
	})
	vm.cString.define("start_with?", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		s := strOf(self)
		for _, a := range args { // true if any prefix matches; a Regexp must match at offset 0
			if re, ok := a.(*Regexp); ok {
				if md := re.re.Match(s); md != nil && md.Begin(0) == 0 {
					return object.True
				}
				continue
			}
			if strings.HasPrefix(s, vm.strPatternCompat(self, a)) {
				return object.True
			}
		}
		return object.False
	})
	vm.cString.define("end_with?", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(strings.HasSuffix(strOf(self), vm.strPatternCompat(self, args[0])))
	})
	vm.cString.define("delete_prefix", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		s := self.(*object.String)
		b := s.Bytes()
		n := vm.deletedAffixLen(s, args[0], false)
		return object.NewStringBytesEnc(append([]byte(nil), b[n:]...), s.EncName())
	})
	vm.cString.define("delete_suffix", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		s := self.(*object.String)
		b := s.Bytes()
		n := vm.deletedAffixLen(s, args[0], true)
		return object.NewStringBytesEnc(append([]byte(nil), b[:len(b)-n]...), s.EncName())
	})
	vm.cString.define("delete_prefix!", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		s := self.(*object.String)
		vm.checkFrozen(s)
		n := vm.deletedAffixLen(s, args[0], false)
		if n == 0 {
			return object.NilV
		}
		s.SetBytes(append([]byte(nil), s.Bytes()[n:]...))
		return s
	})
	vm.cString.define("delete_suffix!", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		s := self.(*object.String)
		vm.checkFrozen(s)
		n := vm.deletedAffixLen(s, args[0], true)
		if n == 0 {
			return object.NilV
		}
		b := s.Bytes()
		s.SetBytes(append([]byte(nil), b[:len(b)-n]...))
		return s
	})
	vm.cString.define("index", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		s, needle := strOf(self), vm.strPatternCompat(self, args[0])
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
		return object.IntValue(int64(utf8.RuneCountInString(s[:byteStart+byteIdx])))
	})
	vm.cString.define("rindex", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		byteIdx := strings.LastIndex(strOf(self), vm.strPatternCompat(self, args[0]))
		if byteIdx < 0 {
			return object.NilV
		}
		return object.IntValue(int64(utf8.RuneCountInString(strOf(self)[:byteIdx])))
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
		// match(pattern, pos): start scanning at character offset pos (default 0).
		if len(args) >= 2 {
			return vm.runMatchFrom(strMatchRegexp(args[0]), strOf(self), intArg(args[1]))
		}
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
	vm.cString.define("oct", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return strOct(strOf(self))
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
	// scrub replaces each ill-formed byte sequence with a replacement (the encoding's
	// U+FFFD, or an explicit String / block result), returning a valid copy in the
	// receiver's encoding. scrub! does the same in place, returning self.
	vm.cString.define("scrub", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		return vm.stringScrub(self, args, blk, false)
	})
	vm.cString.define("scrub!", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		return vm.stringScrub(self, args, blk, true)
	})
	// -@ returns a frozen copy (self when already frozen); +@ returns a mutable copy
	// (self when already mutable), matching MRI's String#-@ / #+@.
	vm.cString.define("-@", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		s := self.(*object.String)
		if s.Frozen {
			return s
		}
		d := s.Dup()
		d.Frozen = true
		return d
	})
	vm.cString.define("+@", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		s := self.(*object.String)
		if s.Frozen {
			return s.Dup()
		}
		return s
	})
	vm.cString.define("ljust", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return strEncOf(self, vm.padString(strOf(self), args, 'l'))
	})
	vm.cString.define("rjust", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return strEncOf(self, vm.padString(strOf(self), args, 'r'))
	})
	vm.cString.define("center", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return strEncOf(self, vm.padString(strOf(self), args, 'c'))
	})
	trFn := func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return strEncOf(self, trString(strOf(self), strArg(args[0]), strArg(args[1]), false))
	}
	vm.cString.define("tr", trFn)
	trSFn := func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return strEncOf(self, trString(strOf(self), strArg(args[0]), strArg(args[1]), true))
	}
	vm.cString.define("tr_s", trSFn)
	vm.cString.define("tr!", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.strBang(self, func(s string) string { return trString(s, strArg(args[0]), strArg(args[1]), false) })
	})
	vm.cString.define("tr_s!", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.strBang(self, func(s string) string { return trString(s, strArg(args[0]), strArg(args[1]), true) })
	})
	vm.cString.define("count", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(stringCount(strOf(self), args)))
	})
	vm.cString.define("delete", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return strEncOf(self, stringDelete(strOf(self), args))
	})
	vm.cString.define("delete!", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.strBang(self, func(s string) string { return stringDelete(s, args) })
	})
	vm.cString.define("squeeze", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return strEncOf(self, stringSqueeze(strOf(self), args))
	})
	strIndexFn := func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		var res object.Value
		if re, ok := args[0].(*Regexp); ok { // s[/re/] / s[/re/, group]
			res = vm.stringRegexpIndex(strOf(self), re, args[1:])
		} else {
			res = stringIndexEnc(strOf(self), args, self.(*object.String).IsBinary())
		}
		if sub, ok := res.(*object.String); ok { // a slice keeps the receiver's encoding
			sub.Enc = self.(*object.String).Enc
		}
		return res
	}
	vm.cString.define("[]", strIndexFn)
	// #slice is a true alias of #[] (shares the exact method record).
	aliasBuiltin(vm.cString, "slice", "[]")
	vm.cString.define("ord", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		s := strOf(self)
		if s == "" {
			raise("ArgumentError", "empty string")
		}
		return object.IntValue(int64([]rune(s)[0]))
	})
	vm.cString.define("partition", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		s, sep := strOf(self), strArg(args[0])
		if i := strings.Index(s, sep); i >= 0 {
			return object.NewArray(object.NewString(s[:i]), object.NewString(sep), object.NewString(s[i+len(sep):]))
		}
		return object.NewArray(object.NewString(s), object.NewString(""), object.NewString(""))
	})
	vm.cString.define("rpartition", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		s, sep := strOf(self), strArg(args[0])
		if i := strings.LastIndex(s, sep); i >= 0 {
			return object.NewArray(object.NewString(s[:i]), object.NewString(sep), object.NewString(s[i+len(sep):]))
		}
		return object.NewArray(object.NewString(""), object.NewString(""), object.NewString(s))
	})

	// String mutation (in-place). Every mutator guards against a frozen receiver.
	// `<<` and concat append each argument: a String contributes its bytes, an
	// Integer its UTF-8 code point.
	strConcatFn := func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		s := self.(*object.String)
		vm.checkFrozen(s)
		for _, a := range args {
			if u, ok := a.(object.KeyUnwrapper); ok { // a String subclass instance
				if w, wrapped := u.HashUnwrap(); wrapped {
					a = w
				}
			}
			switch v := a.(type) {
			case *object.String:
				s.Enc = vm.combinedEncName(s, v) // negotiate encodings (raises if incompatible)
				s.SetBytes(append(s.MutableBytes(), v.Bytes()...))
			case object.Integer:
				b, enc := codepointAppend(int64(v), s.EncName())
				s.Enc = enc
				s.SetBytes(append(s.MutableBytes(), b...))
			case *object.Bignum:
				raise("RangeError", "bignum out of char range")
			default:
				// Any other argument converts via #to_str (a NoMethodError raised inside
				// #to_str propagates; a missing #to_str is a TypeError).
				if vm.respondsToDynamic(a, "to_str") {
					if rs, ok := vm.send(a, "to_str", nil, nil).(*object.String); ok {
						s.Enc = vm.combinedEncName(s, rs)
						s.SetBytes(append(s.MutableBytes(), rs.Bytes()...))
						continue
					}
				}
				raise("TypeError", "no implicit conversion of %s into String", classNameOf(a))
			}
		}
		return s
	}
	vm.cString.define("<<", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		if len(args) != 1 { // #<< takes exactly one argument (unlike #concat)
			raise("ArgumentError", "wrong number of arguments (given %d, expected 1)", len(args))
		}
		return strConcatFn(vm, self, args, blk)
	})
	vm.cString.define("concat", strConcatFn)
	vm.cString.define("replace", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		s := self.(*object.String)
		vm.checkFrozen(s)
		s.SetBytes([]byte(strArg(args[0])))
		return s
	})
	vm.cString.define("prepend", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		s := self.(*object.String)
		vm.checkFrozen(s)
		var head []byte
		for _, a := range args {
			head = append(head, strAppendBytes(a)...)
		}
		s.SetBytes(append(head, s.Bytes()...))
		return s
	})
	vm.cString.define("insert", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		s := self.(*object.String)
		vm.checkFrozen(s)
		// The inserted string converts via #to_str before the index is checked (MRI
		// raises TypeError for an unconvertible value even when the index is out of
		// range); the index converts via #to_int.
		ins := []rune(vm.coerceFormatString(args[1]))
		r := []rune(s.Str())
		idx := vm.repeatLong(args[0])
		at := int(idx)
		if at < 0 {
			at += len(r) + 1
		}
		if at < 0 || at > len(r) {
			raise("IndexError", "index %d out of string", idx)
		}
		out := append(append(append([]rune{}, r[:at]...), ins...), r[at:]...)
		s.SetBytes([]byte(string(out)))
		return s
	})
	vm.cString.define("clear", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		s := self.(*object.String)
		vm.checkFrozen(s)
		s.SetBytes(nil)
		return s
	})
	vm.cString.define("upcase!", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.strBang(self, strings.ToUpper)
	})
	vm.cString.define("downcase!", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.strBang(self, strings.ToLower)
	})
	vm.cString.define("capitalize!", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.strBang(self, capitalizeStr)
	})
	vm.cString.define("swapcase!", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.strBang(self, swapcaseStr)
	})
	vm.cString.define("reverse!", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		s := self.(*object.String)
		vm.checkFrozen(s)
		s.SetBytes([]byte(reverseStr(s.Str())))
		return s
	})
	vm.cString.define("strip!", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.strBang(self, func(x string) string { return strings.Trim(x, wsCutset) })
	})
	vm.cString.define("lstrip!", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.strBang(self, func(x string) string { return strings.TrimLeft(x, wsCutset) })
	})
	vm.cString.define("rstrip!", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.strBang(self, func(x string) string { return strings.TrimRight(x, wsCutset) })
	})
	vm.cString.define("chomp!", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.strBang(self, func(s string) string { return chompSep(s, args) })
	})
	vm.cString.define("chop!", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.strBang(self, chopStr)
	})
	vm.cString.define("squeeze!", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.strBang(self, func(s string) string { return stringSqueeze(s, args) })
	})
	vm.cString.define("sub!", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		return vm.strSubBang(self, args, blk, false)
	})
	vm.cString.define("gsub!", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		return vm.strSubBang(self, args, blk, true)
	})
	vm.cString.define("[]=", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.stringIndexAssign(self.(*object.String), args)
	})
	vm.cString.define("slice!", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.stringSliceBang(self.(*object.String), args)
	})

	// Array.
	vm.cArray.define("length", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(len(self.(*object.Array).Elems)))
	})
	vm.cArray.define("size", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(len(self.(*object.Array).Elems)))
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
		vm.checkArrayFrozen(arr) // re-initialising a frozen array raises (a fresh one is not frozen)
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
				out[i] = vm.callBlock(blk, []object.Value{object.IntValue(int64(i))})
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
				return vm.newBuiltinSubclass(recv, object.NewArray(), args, blk)
			}
			arr := object.NewArray()
			arrayInit(vm, arr, args, blk)
			return arr
		}}
	// Array[a, b, c] (and any subclass's inherited []) builds an array from its
	// arguments. On a subclass it wraps the elements in a subclass instance without
	// calling #initialize, matching MRI's Array.[].
	vm.cArray.smethods["[]"] = &Method{name: "[]", owner: vm.cArray,
		native: func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
			arr := object.NewArrayFromSlice(append([]object.Value{}, args...))
			if recv, ok := self.(*RClass); ok && recv != vm.cArray {
				return &RObject{class: recv, ivars: map[string]object.Value{}, builtin: arr}
			}
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
		return object.NewArrayFromSlice(out)
	})
	vm.cArray.define("fetch", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		if len(args) < 1 || len(args) > 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 1..2)", len(args))
		}
		a := self.(*object.Array).Elems
		v, orig, ok := vm.arrayFetchAt(a, args[0])
		if ok {
			return v
		}
		if blk != nil {
			// The block supersedes a default argument. MRI also warns "block
			// supersedes default value argument" on that clash, but rbgo's
			// Kernel#warn currently writes to stdout rather than stderr, so
			// emitting it here would pollute program output (and the only spec
			// asserting it uses the stderr-based `complain` matcher); omit it
			// until warn is routed to stderr. MRI passes the ORIGINAL index
			// object to the block, not the #to_int result.
			return vm.callBlock(blk, []object.Value{args[0]})
		}
		if len(args) == 2 {
			return args[1]
		}
		raise("IndexError", "index %d outside of array bounds: %d...%d", orig, -int64(len(a)), int64(len(a)))
		return object.NilV
	})
	vm.cArray.define("fetch_values", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		a := self.(*object.Array).Elems
		out := make([]object.Value, 0, len(args))
		for _, idxV := range args {
			v, orig, ok := vm.arrayFetchAt(a, idxV)
			switch {
			case ok:
				out = append(out, v)
			case blk != nil:
				out = append(out, vm.callBlock(blk, []object.Value{idxV}))
			default:
				raise("IndexError", "index %d outside of array bounds: %d...%d", orig, -int64(len(a)), int64(len(a)))
			}
		}
		return object.NewArrayFromSlice(out)
	})
	// Class.try_convert: coerce the argument to the class via its implicit
	// conversion method, or return nil when it does not respond.
	for _, tc := range []struct {
		cls  *RClass
		meth string
	}{
		{vm.cArray, "to_ary"},
		{vm.cHash, "to_hash"},
		{vm.cString, "to_str"},
		{vm.cInteger, "to_int"},
	} {
		cls, meth := tc.cls, tc.meth
		cls.smethods["try_convert"] = &Method{name: "try_convert", owner: cls,
			native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
				return vm.tryConvert(args[0], cls, meth)
			}}
	}
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
		return object.NewArrayFromSlice(out)
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
		return object.NewArrayFromSlice(out)
	})
	vm.cArray.define("fill", arrayFill)
	vm.cArray.define("push", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := self.(*object.Array)
		vm.checkArrayFrozen(a)
		a.Elems = append(a.Elems, args...)
		return a
	})
	vm.cArray.define("<<", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := self.(*object.Array)
		vm.checkArrayFrozen(a)
		a.Elems = append(a.Elems, args[0])
		return a
	})
	vm.cArray.define("pop", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := self.(*object.Array)
		vm.checkArrayFrozen(a)
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
			return object.NewArrayFromSlice(out)
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
		vm.checkArrayFrozen(a)
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
			return object.NewArrayFromSlice(out)
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
		vm.checkArrayFrozen(a)
		a.Elems = append(append([]object.Value{}, args...), a.Elems...)
		return a
	}
	vm.cArray.define("unshift", unshift)
	vm.cArray.define("prepend", unshift)
	// Array#insert(index, *objects): insert the objects before the element at
	// index (or, for a negative index, after the element index counts back to —
	// so -1 appends). Inserting past the end pads the gap with nil, as in MRI.
	// With no objects the array is returned unchanged.
	vm.cArray.define("insert", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := self.(*object.Array)
		vm.checkArrayFrozen(a)
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1+)")
		}
		idx := int(intArg(args[0]))
		ins := args[1:]
		if len(ins) == 0 {
			return a
		}
		// A negative index inserts after the element it points at: pos = len+idx+1.
		if idx < 0 {
			idx += len(a.Elems) + 1
			if idx < 0 {
				raise("IndexError", "index %d too small for array; minimum: %d", int(intArg(args[0])), -len(a.Elems)-1)
			}
		}
		if idx > len(a.Elems) {
			pad := make([]object.Value, idx-len(a.Elems))
			for i := range pad {
				pad[i] = object.NilV
			}
			a.Elems = append(a.Elems, pad...)
		}
		tail := append([]object.Value{}, a.Elems[idx:]...)
		a.Elems = append(append(a.Elems[:idx:idx], ins...), tail...)
		return a
	})
	vm.cArray.define("delete", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		// Remove every element == the argument; return it, or (a block's result,
		// else nil) when nothing matched.
		a := self.(*object.Array)
		vm.checkArrayFrozen(a)
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
		vm.checkArrayFrozen(a)
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
		vm.checkArrayFrozen(a)
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
		vm.checkArrayFrozen(a)
		a.Elems = nil
		return a
	})
	vm.cArray.define("replace", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := self.(*object.Array)
		vm.checkArrayFrozen(a)
		other, ok := args[0].(*object.Array)
		if !ok {
			raise("TypeError", "no implicit conversion of %s into Array", classNameOf(args[0]))
		}
		a.Elems = append([]object.Value(nil), other.Elems...)
		return a
	})
	vm.cArray.define("rotate!", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := self.(*object.Array)
		vm.checkArrayFrozen(a)
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
	arrayInclude := func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		for _, e := range self.(*object.Array).Elems {
			if valueEqual(e, args[0]) {
				return object.True
			}
		}
		return object.False
	}
	vm.cArray.define("include?", arrayInclude)
	// member? is an alias of include? (the Enumerable name), used by Puppet's
	// settings initialization.
	vm.cArray.define("member?", arrayInclude)
	// #[] and #[]= only read args and copy element *values* into the array (or a
	// freshly allocated result); they never retain the args slice, so the OpSend
	// fast path may hand them the live operand-stack region (defineNR).
	arrayAref := func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := self.(*object.Array)
		start, length, isSpan, ok := vm.arrayArefSpan(a, args)
		if !ok {
			return object.NilV
		}
		if !isSpan {
			return a.Elems[start]
		}
		out := make([]object.Value, length)
		copy(out, a.Elems[start:start+length])
		return object.NewArrayFromSlice(out)
	}
	vm.cArray.defineNR("[]", arrayAref)
	// #slice is a true alias of #[] (shares the exact method record, so
	// Array.instance_method(:slice) == Array.instance_method(:[]), as in MRI);
	// #at takes a single integer index only.
	aliasBuiltin(vm.cArray, "slice", "[]")
	vm.cArray.define("at", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) != 1 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 1)", len(args))
		}
		a := self.(*object.Array)
		if i, ok := arrayIndex(a, vm.repeatLong(args[0])); ok {
			return a.Elems[i]
		}
		return object.NilV
	})
	// #slice! removes and returns the addressed element or span (#[] semantics).
	vm.cArray.define("slice!", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := self.(*object.Array)
		vm.checkArrayFrozen(a)
		start, length, isSpan, ok := vm.arrayArefSpan(a, args)
		if !ok {
			return object.NilV
		}
		if !isSpan {
			v := a.Elems[start]
			a.Elems = append(a.Elems[:start], a.Elems[start+1:]...)
			return v
		}
		out := make([]object.Value, length)
		copy(out, a.Elems[start:start+length])
		a.Elems = append(a.Elems[:start], a.Elems[start+length:]...)
		return object.NewArrayFromSlice(out)
	})
	// #delete_at removes and returns the element at index, nil when out of range.
	vm.cArray.define("delete_at", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := self.(*object.Array)
		vm.checkArrayFrozen(a)
		i, ok := arrayIndex(a, vm.repeatLong(args[0]))
		if !ok {
			return object.NilV
		}
		v := a.Elems[i]
		a.Elems = append(a.Elems[:i], a.Elems[i+1:]...)
		return v
	})
	// #keep_if is #select! that always returns self (not nil when unchanged).
	vm.cArray.define("keep_if", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			return enumFor(self, "keep_if")
		}
		a := self.(*object.Array)
		vm.checkArrayFrozen(a)
		arrayKeepIf(vm, a, blk, true)
		return a
	})
	// #sort_by! sorts in place by the block's key and returns self.
	vm.cArray.define("sort_by!", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			return enumFor(self, "sort_by!")
		}
		a := self.(*object.Array)
		vm.checkArrayFrozen(a)
		// Snapshot the elements: the block may mutate the array while keys are
		// computed, so the sort must read from the fixed original, not live a.Elems.
		elems := append([]object.Value(nil), a.Elems...)
		keys := make([]object.Value, len(elems))
		for i, e := range elems {
			keys[i] = vm.callBlock(blk, []object.Value{e})
		}
		idx := make([]int, len(elems))
		for i := range idx {
			idx[i] = i
		}
		sort.SliceStable(idx, func(i, j int) bool { return vm.spaceship(keys[idx[i]], keys[idx[j]]) < 0 })
		out := make([]object.Value, len(idx))
		for i, k := range idx {
			out[i] = elems[k]
		}
		a.Elems = out
		return a
	})
	// #shuffle returns a new array with the elements in random order; #shuffle!
	// shuffles the receiver in place. A random: keyword supplies a custom RNG.
	vm.cArray.define("shuffle", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := self.(*object.Array)
		out := append([]object.Value(nil), a.Elems...)
		vm.fisherYates(out, vm.rngKwarg(args))
		return object.NewArrayFromSlice(out)
	})
	vm.cArray.define("shuffle!", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := self.(*object.Array)
		vm.checkArrayFrozen(a)
		vm.fisherYates(a.Elems, vm.rngKwarg(args))
		return a
	})
	// #sample returns one random element (nil when empty) or, given a count, an
	// array of up to count distinct elements. A random: keyword supplies a custom
	// RNG; the count converts via #to_int and must be non-negative.
	vm.cArray.define("sample", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := self.(*object.Array)
		rng := vm.rngKwarg(args)
		pos := args
		if trailingKwHash(args) != nil {
			pos = args[:len(args)-1]
		}
		n := len(a.Elems)
		if len(pos) == 0 {
			if n == 0 {
				return object.NilV
			}
			return a.Elems[vm.drawIndex(rng, n)]
		}
		count := int(vm.repeatLong(pos[0]))
		if count < 0 {
			raise("ArgumentError", "negative sample number")
		}
		if count > n {
			count = n
		}
		// Partial Fisher-Yates over a copy: the first count slots hold the sample.
		pool := append([]object.Value(nil), a.Elems...)
		for i := 0; i < count; i++ {
			j := i + vm.drawIndex(rng, n-i)
			pool[i], pool[j] = pool[j], pool[i]
		}
		return object.NewArrayFromSlice(pool[:count])
	})
	vm.cArray.defineNR("[]=", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := self.(*object.Array)
		vm.checkArrayFrozen(a)
		// Range form: a[range] = value.
		if rng, ok := args[0].(*object.Range); ok {
			start, length, ok := sliceRange(len(a.Elems), rng)
			if !ok {
				raise("RangeError", "%s out of range", rng.Inspect())
			}
			val := args[1]
			arraySpliceAssign(a, start, length, val)
			return val
		}
		// Start/length form: a[start, len] = value.
		if len(args) == 3 {
			start := normIndex(intArg(args[0]), len(a.Elems))
			length := int(intArg(args[1]))
			if start < 0 {
				raise("IndexError", "index %d out of array", intArg(args[0]))
			}
			if length < 0 {
				raise("IndexError", "negative length (%d)", length)
			}
			val := args[2]
			arraySpliceAssign(a, start, length, val)
			return val
		}
		// Single-index form: a[i] = value.
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
		if blk.native != nil {
			// A synthesized native block (e.g. &:to_s, a Go-compiled AOT closure)
			// runs opaque Go code that could retain the args slice, so each yield
			// gets its own fresh slice.
			for _, e := range a.Elems {
				vm.callBlock(blk, []object.Value{e})
			}
			return a
		}
		// An interpreted block: exec copies the yielded value into the block's env
		// slots (or, for a *splat/auto-splat param, into a freshly built rest Array)
		// synchronously at frame entry, before any block bytecode runs, and never
		// aliases the passed slice. So a single 1-element scratch slice, private to
		// this call (re-entrant each gets its own), can be reused across iterations
		// without a capturing block observing the next iteration's overwrite.
		scratch := make([]object.Value, 1)
		for _, e := range a.Elems {
			scratch[0] = e
			vm.callBlock(blk, scratch)
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
		return object.NewArrayFromSlice(out)
	})
	// select is native (Array includes Enumerable, whose #select routes every
	// element through __each_packed — a splat-array allocation and a second block
	// dispatch per element). Iterating a.Elems directly, with the result slice
	// pre-sized to the input length so it never re-grows, is observably identical
	// (single-value yields, first-seen order) but skips that per-element overhead.
	// #filter delegates here through the prelude, so it inherits the fast path.
	vm.cArray.define("select", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			return enumFor(self, "select")
		}
		a := self.(*object.Array)
		out := make([]object.Value, 0, len(a.Elems))
		for _, e := range a.Elems {
			if vm.callBlock(blk, []object.Value{e}).Truthy() {
				out = append(out, e)
			}
		}
		return object.NewArrayFromSlice(out)
	})
	// reduce/inject are native for the same reason (and #inject delegates here via
	// the prelude). The fold mirrors Enumerable#reduce exactly — the (init, sym),
	// (sym), (init) and bare-block forms, the "no block given" yield error, and the
	// nil result of an empty fold — so behaviour is byte-identical, only faster.
	vm.cArray.define("reduce", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		return arrayReduce(vm, self.(*object.Array), args, blk)
	})
	vm.cArray.define("reverse", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		a := self.(*object.Array)
		out := make([]object.Value, len(a.Elems))
		for i, e := range a.Elems {
			out[len(a.Elems)-1-i] = e
		}
		return object.NewArrayFromSlice(out)
	})
	vm.cArray.define("dig", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.digValue(self, args)
	})
	vm.cArray.define("uniq", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		return object.NewArrayFromSlice(vm.arrayUniq(self.(*object.Array).Elems, blk))
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
		return object.NewArrayFromSlice(out)
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
		return object.NewArrayFromSlice(out)
	})
	vm.cArray.define("map!", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			return enumFor(self, "map!")
		}
		a := self.(*object.Array)
		vm.checkArrayFrozen(a)
		for i := range a.Elems {
			a.Elems[i] = vm.callBlock(blk, []object.Value{a.Elems[i]})
		}
		return self
	})
	// collect! is the classic alias of map! (as collect is of map).
	vm.aliasMethod(vm.cArray, "collect!", "map!")
	vm.cArray.define("reverse!", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		a := self.(*object.Array)
		vm.checkArrayFrozen(a)
		for i, j := 0, len(a.Elems)-1; i < j; i, j = i+1, j-1 {
			a.Elems[i], a.Elems[j] = a.Elems[j], a.Elems[i]
		}
		return self
	})
	vm.cArray.define("sort!", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		a := self.(*object.Array)
		vm.checkArrayFrozen(a)
		vm.sortSlice(a.Elems, blk)
		return self
	})
	selectBang := func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			return enumFor(self, "select!")
		}
		a := self.(*object.Array)
		vm.checkArrayFrozen(a)
		return arrayKeepIf(vm, a, blk, true)
	}
	vm.cArray.define("select!", selectBang)
	vm.cArray.define("filter!", selectBang)
	vm.cArray.define("reject!", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			return enumFor(self, "reject!")
		}
		a := self.(*object.Array)
		vm.checkArrayFrozen(a)
		return arrayKeepIf(vm, a, blk, false)
	})
	vm.cArray.define("compact!", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		a := self.(*object.Array)
		vm.checkArrayFrozen(a)
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
		vm.checkArrayFrozen(a)
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
		return object.NewArrayFromSlice(out)
	})
	vm.cArray.define("flatten", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		depth := -1
		if len(args) > 0 {
			depth = int(intArg(args[0]))
		}
		return object.NewArrayFromSlice(flattenDepth(self.(*object.Array).Elems, depth))
	})
	vm.cArray.define("flatten!", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		depth := -1
		if len(args) > 0 {
			depth = int(intArg(args[0]))
		}
		a := self.(*object.Array)
		vm.checkArrayFrozen(a)
		out, changed := flattenDepthChanged(a.Elems, depth)
		if !changed {
			return object.NilV
		}
		a.Elems = out
		return self
	})
	vm.cArray.define("sum", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		acc := object.Value(object.IntValue(0))
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
			vm.callBlock(blk, []object.Value{object.NewArrayFromSlice(slice)})
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
			vm.callBlock(blk, []object.Value{object.NewArrayFromSlice(window)})
		}
		return self
	})
	vm.cArray.define("transpose", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		rows := self.(*object.Array).Elems
		if len(rows) == 0 {
			return object.NewArray()
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
			out[j] = object.NewArrayFromSlice(col)
		}
		return object.NewArrayFromSlice(out)
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
		// How long the result will be, worked out before any of it is built.
		//
		// MRI does this and raises rather than trying: the product of eleven
		// hundred-element arrays has 10^22 entries, which overflows the length
		// of an array long before it exhausts memory. Without the check the
		// interpreter simply allocates until the machine dies — which is what
		// it did to a CI runner, taking the ruby/spec ratchet lane with it.
		// core/array/product_spec.rb calls this "an unreasonable number of
		// products" and expects RangeError.
		total := 1
		for _, list := range lists {
			if len(list) == 0 {
				total = 0 // an empty list makes an empty product
				break
			}
			if total > math.MaxInt/len(list) {
				raise("RangeError", "too big to product")
			}
			total *= len(list)
		}

		// Cartesian product, last list varying fastest (MRI order).
		out := make([]object.Value, 0, 1)
		out = append(out, object.NewArray())
		for _, list := range lists {
			next := make([]object.Value, 0, len(out)*len(list))
			for _, prefix := range out {
				for _, e := range list {
					row := append(append([]object.Value{}, prefix.(*object.Array).Elems...), e)
					next = append(next, object.NewArrayFromSlice(row))
				}
			}
			out = next
		}
		return object.NewArrayFromSlice(out)
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
				combos = append(combos, object.NewArrayFromSlice(pick))
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
			return enumFor(object.NewArrayFromSlice(combos), "each")
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
					perms = append(perms, object.NewArrayFromSlice(out))
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
			return enumFor(object.NewArrayFromSlice(perms), "each")
		}
		for _, pr := range perms {
			vm.callBlock(blk, []object.Value{pr})
		}
		return self
	})
	vm.cArray.define("repeated_combination", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		k := int(vm.repeatLong(args[0]))
		// No block: a lazy Enumerator that re-reads self when iterated (MRI sees
		// later mutations of the array), with an explicit combinatorial #size.
		if blk == nil {
			return enumForSized(self, "repeated_combination", func(vm *VM) object.Value {
				kk := int(vm.repeatLong(args[0]))
				if kk < 0 {
					return object.IntValue(0)
				}
				n := len(self.(*object.Array).Elems)
				return object.NormInt(binomialBig(n+kk-1, kk))
			}, args...)
		}
		// Work from a defensive copy so a block mutating self mid-iteration can
		// neither be seen nor panic (MRI generates every tuple up front).
		elems := append([]object.Value(nil), self.(*object.Array).Elems...)
		n := len(elems)
		switch {
		case k == 0:
			vm.callBlock(blk, []object.Value{object.NewArray()})
		case k > 0 && n > 0:
			idx := make([]int, k) // non-decreasing indices, all starting at 0
			for {
				pick := make([]object.Value, k)
				for i, j := range idx {
					pick[i] = elems[j]
				}
				vm.callBlock(blk, []object.Value{object.NewArrayFromSlice(pick)})
				i := k - 1
				for i >= 0 && idx[i] == n-1 {
					i--
				}
				if i < 0 {
					break
				}
				idx[i]++
				for j := i + 1; j < k; j++ {
					idx[j] = idx[i] // repetition allowed: keep non-decreasing
				}
			}
		}
		// k < 0, or k > 0 with an empty receiver, yields nothing.
		return self
	})
	vm.cArray.define("repeated_permutation", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		k := int(vm.repeatLong(args[0]))
		if blk == nil {
			return enumForSized(self, "repeated_permutation", func(vm *VM) object.Value {
				kk := int(vm.repeatLong(args[0]))
				if kk < 0 {
					return object.IntValue(0)
				}
				n := len(self.(*object.Array).Elems)
				return object.NormInt(new(big.Int).Exp(big.NewInt(int64(n)), big.NewInt(int64(kk)), nil))
			}, args...)
		}
		elems := append([]object.Value(nil), self.(*object.Array).Elems...)
		n := len(elems)
		switch {
		case k == 0:
			vm.callBlock(blk, []object.Value{object.NewArray()})
		case k > 0 && n > 0:
			idx := make([]int, k) // base-n counter, rightmost digit fastest
			for {
				pick := make([]object.Value, k)
				for i, j := range idx {
					pick[i] = elems[j]
				}
				vm.callBlock(blk, []object.Value{object.NewArrayFromSlice(pick)})
				i := k - 1
				for i >= 0 && idx[i] == n-1 {
					idx[i] = 0
					i--
				}
				if i < 0 {
					break
				}
				idx[i]++
			}
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
		return object.NewArrayFromSlice(out)
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
		return object.NewArrayFromSlice(out)
	})
	vm.cArray.define("rotate", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := self.(*object.Array)
		n := len(a.Elems)
		if n == 0 {
			return object.NewArray()
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
		return object.NewArrayFromSlice(out)
	})
	vm.cArray.define("join", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := self.(*object.Array)
		// An empty Array joins to an empty US-ASCII string without ever touching the
		// separator (MRI does not call #to_str on it), regardless of $,.
		if len(a.Elems) == 0 {
			return object.NewStringBytesEnc(nil, "US-ASCII")
		}
		var sep *object.String
		if len(args) > 0 && !object.IsNil(args[0]) {
			sep = vm.joinSeparator(args[0])
		} else if v, ok := vm.globals["$,"]; ok {
			// A nil/omitted separator falls back to the field separator $, when set.
			if s, ok := v.(*object.String); ok {
				sep = s
			}
		}
		return vm.arrayJoin(a, sep, map[*object.Array]bool{})
	})
	vm.cArray.define("index", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		for i, e := range self.(*object.Array).Elems {
			if valueEqual(e, args[0]) {
				return object.IntValue(int64(i))
			}
		}
		return object.NilV
	})
	vm.cArray.define("assoc", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.arrayAssoc(self.(*object.Array), args[0], 0)
	})
	vm.cArray.define("rassoc", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.arrayAssoc(self.(*object.Array), args[0], 1)
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
		return object.NewArrayFromSlice(out)
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
		return object.NewArrayFromSlice(out)
	})
	vm.cArray.define("sort", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		a := self.(*object.Array)
		out := make([]object.Value, len(a.Elems))
		copy(out, a.Elems)
		vm.sortSlice(out, blk)
		return object.NewArrayFromSlice(out)
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
			return object.IntValue(-1)
		case len(a) > len(be):
			return object.IntValue(1)
		}
		return object.IntValue(0)
	})
	vm.cNilClass.define("to_a", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewArray()
	})
	// NilClass conversions mirror MRI: nil.to_i → 0, nil.to_f → 0.0, nil.to_h → {},
	// nil.to_r → (0/1), nil.to_c → (0+0i). nil.to_a/to_s/inspect already exist.
	vm.cNilClass.define("to_i", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(0)
	})
	vm.cNilClass.define("to_f", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Float(0)
	})
	vm.cNilClass.define("to_h", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewHash()
	})
	vm.cNilClass.define("to_r", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return &object.Rational{R: big.NewRat(0, 1)}
	})
	vm.cNilClass.define("to_c", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return &object.Complex{Re: object.IntValue(0), Im: object.IntValue(0)}
	})
	// nil & obj is always false; nil | obj and nil ^ obj are true unless obj is
	// nil or false (MRI treats only nil/false as falsey here).
	vm.cNilClass.define("&", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) != 1 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 1)", len(args))
		}
		return object.False
	})
	vm.cNilClass.define("|", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) != 1 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 1)", len(args))
		}
		return object.Bool(args[0].Truthy())
	})
	vm.cNilClass.define("^", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) != 1 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 1)", len(args))
		}
		return object.Bool(args[0].Truthy())
	})
	// nil =~ anything is always nil in MRI (NilClass#=~), so `value =~ /re/`
	// short-circuits when value is nil — Puppet's useradd provider relies on this
	// when building its command from possibly-absent properties.
	vm.cNilClass.define("=~", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) != 1 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 1)", len(args))
		}
		return object.NilV
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
		return object.NewArrayFromSlice(out)
	})
	vm.cArray.define("min_by", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		a := self.(*object.Array)
		if blk != nil && len(args) > 0 && !object.IsNil(args[0]) {
			return vm.arrayByExtremeN(a, blk, int(coerceInt(vm, args[0])), -1)
		}
		return vm.arrayByExtreme(a, blk, "min_by", -1)
	})
	vm.cArray.define("max_by", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		a := self.(*object.Array)
		if blk != nil && len(args) > 0 && !object.IsNil(args[0]) {
			return vm.arrayByExtremeN(a, blk, int(coerceInt(vm, args[0])), 1)
		}
		return vm.arrayByExtreme(a, blk, "max_by", 1)
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
		vm.checkHashFrozen(h) // re-initialising a frozen hash raises (a fresh one is not frozen)
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
		s.SetBytes([]byte(content))
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
	// #[] reads args[0] and returns a stored/default value; #[]= copies element
	// values into the hash. Neither retains the args slice, so both take the
	// no-copy OpSend fast path (defineNR).
	vm.cHash.defineNR("[]", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		h := self.(*object.Hash)
		if v, ok := h.Get(args[0]); ok {
			return v
		}
		return vm.hashDefault(h, args[0])
	})
	vm.cHash.defineNR("[]=", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		h := self.(*object.Hash)
		vm.checkHashFrozen(h)
		h.Set(args[0], args[1])
		return args[1]
	})
	vm.cHash.define("length", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self.(*object.Hash).Len()))
	})
	vm.cHash.define("size", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self.(*object.Hash).Len()))
	})
	vm.cHash.define("empty?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self.(*object.Hash).Len() == 0)
	})
	vm.cHash.define("clear", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		h := self.(*object.Hash)
		vm.checkHashFrozen(h)
		h.Clear()
		return h
	})
	hashKeyP := func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		_, ok := self.(*object.Hash).Get(args[0])
		return object.Bool(ok)
	}
	vm.cHash.define("key?", hashKeyP)
	vm.cHash.define("has_key?", hashKeyP)
	vm.cHash.define("include?", hashKeyP)
	vm.cHash.define("member?", hashKeyP)
	vm.cHash.define("keys", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		h := self.(*object.Hash)
		ks := make([]object.Value, len(h.Keys))
		copy(ks, h.Keys)
		return object.NewArrayFromSlice(ks)
	})
	vm.cHash.define("values", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		h := self.(*object.Hash)
		vs := make([]object.Value, 0, len(h.Keys))
		for _, k := range h.Keys {
			v, _ := h.Get(k)
			vs = append(vs, v)
		}
		return object.NewArrayFromSlice(vs)
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
	vm.cHash.define("each_key", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			return enumFor(self, "each_key")
		}
		h := self.(*object.Hash)
		for _, k := range h.Keys {
			vm.callBlock(blk, []object.Value{k})
		}
		return h
	})
	vm.cHash.define("each_value", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			return enumFor(self, "each_value")
		}
		h := self.(*object.Hash)
		for _, k := range h.Keys {
			v, _ := h.Get(k)
			vm.callBlock(blk, []object.Value{v})
		}
		return h
	})
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
		vm.checkHashFrozen(h)
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
	vm.cHash.define("fetch", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		if v, ok := self.(*object.Hash).Get(args[0]); ok {
			return v
		}
		if blk != nil {
			return vm.callBlock(blk, []object.Value{args[0]})
		}
		if len(args) > 1 {
			return args[1]
		}
		vm.raiseWithIvars("KeyError", "key not found: "+args[0].Inspect(),
			map[string]object.Value{"@key": args[0], "@receiver": self})
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
		return object.NewArrayFromSlice(out)
	})
	// fetch_values(*keys) returns an Array of the values for the given keys, in
	// the order requested. A missing key uses the block's result (called with the
	// key) when a block is given, else raises KeyError with MRI's "key not found:
	// %p" message. With no keys it returns []. Unlike values_at, a bare missing
	// key is an error rather than nil.
	vm.cHash.define("fetch_values", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		h := self.(*object.Hash)
		out := make([]object.Value, 0, len(args))
		for _, k := range args {
			if v, ok := h.Get(k); ok {
				out = append(out, v)
				continue
			}
			if blk != nil {
				out = append(out, vm.callBlock(blk, []object.Value{k}))
				continue
			}
			vm.raiseWithIvars("KeyError", "key not found: "+k.Inspect(),
				map[string]object.Value{"@key": k, "@receiver": self})
		}
		return object.NewArrayFromSlice(out)
	})
	// replace(other) discards the receiver's contents and copies other's pairs,
	// default value, default proc and compare_by_identity flag, returning self.
	// other is coerced with #to_hash (a non-convertible argument raises TypeError).
	vm.cHash.define("replace", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		h := self.(*object.Hash)
		vm.checkHashFrozen(h)
		h.ReplaceWith(vm.toHash(args[0]))
		return h
	})
	// compare_by_identity switches the receiver to identity-based key comparison
	// (distinct objects with equal content become distinct keys) and returns self,
	// rehashing existing entries so they stay reachable by their original objects.
	vm.cHash.define("compare_by_identity", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		h := self.(*object.Hash)
		vm.checkHashFrozen(h)
		h.CompareByIdentity()
		return h
	})
	// compare_by_identity? reports whether the receiver compares keys by identity.
	vm.cHash.define("compare_by_identity?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self.(*object.Hash).Identity)
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
		vm.checkHashFrozen(h)
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
		vm.checkHashFrozen(h)
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
	vm.cHash.define("to_h", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		h := self.(*object.Hash)
		if blk == nil {
			return self
		}
		// With a block, each (key, value) pair is mapped through the block, which
		// must return a two-element [new_key, new_value] array; the results build a
		// fresh hash (later pairs overwrite earlier ones on key collision).
		out := object.NewHash()
		for _, k := range h.Keys {
			v, _ := h.Get(k)
			res := vm.callBlock(blk, []object.Value{k, v})
			pair, ok := res.(*object.Array)
			if !ok {
				raise("TypeError", "wrong element type %s (expected array)", vm.classOf(res).name)
			}
			if len(pair.Elems) != 2 {
				raise("ArgumentError", "element has wrong array length (expected 2, was %d)", len(pair.Elems))
			}
			out.Set(pair.Elems[0], pair.Elems[1])
		}
		return out
	})
	vm.cHash.define("store", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		h := self.(*object.Hash)
		vm.checkHashFrozen(h)
		h.Set(args[0], args[1])
		return args[1]
	})
	// default / default= and default_proc / default_proc= manage the value (or
	// block) returned for an absent key. The static default and the default block
	// are mutually exclusive in MRI: setting one clears the other.
	vm.cHash.define("default", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		h := self.(*object.Hash)
		// default(key) invokes the default block (passing the hash and key); with
		// no argument it returns the static default, ignoring any block.
		if len(args) == 1 && !object.IsNil(h.DefaultProc) {
			return vm.callBlock(h.DefaultProc.(*Proc), []object.Value{h, args[0]})
		}
		if h.Default != nil {
			return h.Default
		}
		return object.NilV
	})
	vm.cHash.define("default=", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		h := self.(*object.Hash)
		vm.checkHashFrozen(h)
		h.Default = args[0]
		h.DefaultProc = nil
		return args[0]
	})
	vm.cHash.define("default_proc", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		h := self.(*object.Hash)
		if !object.IsNil(h.DefaultProc) {
			return h.DefaultProc
		}
		return object.NilV
	})
	vm.cHash.define("default_proc=", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		h := self.(*object.Hash)
		vm.checkHashFrozen(h)
		switch p := args[0].(type) {
		case object.Nil:
			h.DefaultProc = nil
		case *Proc:
			h.DefaultProc = p
			h.Default = nil
		default:
			// MRI coerces via #to_proc; without that, only a Proc or nil is valid.
			raise("TypeError", "no implicit conversion of %s into Proc", vm.classOf(args[0]).name)
		}
		return args[0]
	})
	vm.cHash.define("delete", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		h := self.(*object.Hash)
		vm.checkHashFrozen(h)
		v, _ := h.Delete(args[0])
		return v
	})
	// #shift removes the first inserted pair and returns it as [key, value], or
	// nil when the hash is empty (ruby 3.4+).
	vm.cHash.define("shift", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		h := self.(*object.Hash)
		vm.checkHashFrozen(h)
		if h.Len() == 0 {
			return object.NilV
		}
		k := h.Keys[0]
		v, _ := h.Get(k)
		h.Delete(k)
		return object.NewArrayFromSlice([]object.Value{k, v})
	})
	// #rehash recomputes every key's hash (e.g. after a mutable key changed) and
	// returns self; keys that have become #eql? collapse to the first inserted.
	vm.cHash.define("rehash", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		h := self.(*object.Hash)
		vm.checkHashFrozen(h)
		h.Rehash()
		return h
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
	// hashDeleteWhere deletes each pair for which the block's truthiness matches
	// `want`, returning the count removed. It snapshots the keys first so deleting
	// during iteration is safe. delete_if/keep_if and the reject!/select! bang
	// forms (which return nil when nothing changed) all share it.
	hashDeleteWhere := func(vm *VM, h *object.Hash, blk *Proc, want bool) int {
		keys := make([]object.Value, len(h.Keys))
		copy(keys, h.Keys)
		removed := 0
		for _, k := range keys {
			v, _ := h.Get(k)
			if vm.callBlock(blk, []object.Value{hashPair(k, v)}).Truthy() == want {
				h.Delete(k)
				removed++
			}
		}
		return removed
	}
	// delete_if / reject! delete the pairs the block accepts; keep_if / select!
	// keep them (delete the rest). delete_if and keep_if always return the Hash;
	// reject! and select! return nil when they removed nothing, matching MRI.
	vm.cHash.define("delete_if", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			return enumFor(self, "delete_if")
		}
		vm.checkHashFrozen(self.(*object.Hash))
		hashDeleteWhere(vm, self.(*object.Hash), blk, true)
		return self
	})
	vm.cHash.define("reject!", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			return enumFor(self, "reject!")
		}
		vm.checkHashFrozen(self.(*object.Hash))
		if hashDeleteWhere(vm, self.(*object.Hash), blk, true) == 0 {
			return object.NilV
		}
		return self
	})
	vm.cHash.define("keep_if", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			return enumFor(self, "keep_if")
		}
		vm.checkHashFrozen(self.(*object.Hash))
		hashDeleteWhere(vm, self.(*object.Hash), blk, false)
		return self
	})
	vm.cHash.define("select!", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			return enumFor(self, "select!")
		}
		vm.checkHashFrozen(self.(*object.Hash))
		if hashDeleteWhere(vm, self.(*object.Hash), blk, false) == 0 {
			return object.NilV
		}
		return self
	})
	vm.cHash.define("filter!", vm.cHash.methods["select!"].native)
	// assoc/rassoc scan the pairs in insertion order and return the first
	// [key, value] whose key (assoc) or value (rassoc) is Ruby-== to the
	// argument, or nil when none matches. They never consult the default.
	vm.cHash.define("assoc", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		h := self.(*object.Hash)
		for _, k := range h.Keys {
			if valueEqual(k, args[0]) {
				v, _ := h.Get(k)
				return hashPair(k, v)
			}
		}
		return object.NilV
	})
	vm.cHash.define("rassoc", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		h := self.(*object.Hash)
		for _, k := range h.Keys {
			v, _ := h.Get(k)
			if valueEqual(v, args[0]) {
				return hashPair(k, v)
			}
		}
		return object.NilV
	})
	// Hash#<= is subset-by-pair: true when every (key, value) pair of the
	// receiver is present in other, values compared with valueEqual (MRI's
	// rb_equal, so 1 and 1.0 match). A non-Hash argument is coerced via #to_hash;
	// one without #to_hash raises TypeError "no implicit conversion of X into
	// Hash" (matching MRI's rb_check_hash_type + Check_Type).
	vm.cHash.define("<=", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(hashSubset(self.(*object.Hash), vm.toHash(args[0])))
	})
	// Hash#< is proper-subset: <= and strictly smaller (fewer pairs). Because a
	// subset with equal size is an equal hash, the size test alone distinguishes
	// proper from improper containment.
	vm.cHash.define("<", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		h, other := self.(*object.Hash), vm.toHash(args[0])
		return object.Bool(h.Len() < other.Len() && hashSubset(h, other))
	})
	// Hash#>= is superset-by-pair: every pair of other is present in the receiver.
	vm.cHash.define(">=", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(hashSubset(vm.toHash(args[0]), self.(*object.Hash)))
	})
	// Hash#> is proper-superset: >= and strictly larger.
	vm.cHash.define(">", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		h, other := self.(*object.Hash), vm.toHash(args[0])
		return object.Bool(h.Len() > other.Len() && hashSubset(other, h))
	})
	// Hash#to_proc returns a unary lambda that looks a key up in the hash:
	// pr.call(k) == h[k]. Lookup goes through #[], so a default value or
	// default_proc is honored just as for a direct index. It captures the hash by
	// reference, so later mutations are visible through the proc. As a lambda it is
	// arity-strict (exactly 1 argument), raising ArgumentError otherwise, and
	// Proc#lambda? is true.
	vm.cHash.define("to_proc", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return &Proc{isLambda: true, nativeArity: 1, native: func(vm *VM, args []object.Value) object.Value {
			if len(args) != 1 {
				raise("ArgumentError", "wrong number of arguments (given %d, expected 1)", len(args))
			}
			return vm.send(self, "[]", args, nil)
		}}
	})
	// flatten returns the key/value pairs as one flat Array. The default depth is
	// 1 (the pair wrappers are removed but Array values are left intact); a depth
	// >= 2 recurses that many further levels into nested Array values. A
	// non-Integer argument raises TypeError (via intArg).
	vm.cHash.define("flatten", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		depth := 1
		if len(args) > 0 {
			depth = int(intArg(args[0]))
		}
		h := self.(*object.Hash)
		pairs := make([]object.Value, 0, len(h.Keys))
		for _, k := range h.Keys {
			v, _ := h.Get(k)
			pairs = append(pairs, hashPair(k, v))
		}
		return object.NewArrayFromSlice(flattenDepth(pairs, depth))
	})
	// compact returns a copy without the nil-valued pairs, preserving the
	// receiver's default value and default proc. compact! removes them in place,
	// returning self, or nil when nothing was removed.
	vm.cHash.define("compact", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		h := self.(*object.Hash)
		out := object.NewHash()
		for _, k := range h.Keys {
			v, _ := h.Get(k)
			if !object.IsNil(v) {
				out.Set(k, v)
			}
		}
		out.Default = h.Default
		out.DefaultProc = h.DefaultProc
		return out
	})
	vm.cHash.define("compact!", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		h := self.(*object.Hash)
		vm.checkHashFrozen(h)
		var drop []object.Value
		for _, k := range h.Keys {
			if v, _ := h.Get(k); object.IsNil(v) {
				drop = append(drop, k)
			}
		}
		if len(drop) == 0 {
			return object.NilV
		}
		for _, k := range drop {
			h.Delete(k)
		}
		return self
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
				out[i] = object.IntValue(int64(lo) + int64(i))
			}
			return object.NewArrayFromSlice(out)
		}
		elems := rangeElems(r)
		n = clampCount(int64(n), len(elems))
		out := make([]object.Value, n)
		copy(out, elems[:n])
		return object.NewArrayFromSlice(out)
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
		n := clampCount(vm.repeatLong(args[0]), len(elems))
		out := make([]object.Value, n)
		copy(out, elems[len(elems)-n:])
		return object.NewArrayFromSlice(out)
	})
	vm.cRange.define("exclude_end?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self.(*object.Range).Exclusive)
	})
	// include?/member?/=== treat their argument as a plain value to test for
	// membership (comparison-based here). cover? additionally accepts a Range and
	// answers range-in-range containment (see rangeCoverRange).
	rangeCover := func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(rangeCoverValue(self.(*object.Range), args[0]))
	}
	vm.cRange.define("include?", rangeCover)
	vm.cRange.define("cover?", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		r := self.(*object.Range)
		if o, ok := args[0].(*object.Range); ok {
			return object.Bool(rangeCoverRange(r, o))
		}
		return object.Bool(rangeCoverValue(r, args[0]))
	})
	vm.cRange.define("member?", rangeCover)
	vm.cRange.define("===", rangeCover)
	vm.cRange.define("min", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		r := self.(*object.Range)
		if len(args) > 0 { // min(n): the n smallest (the range is ascending)
			if object.IsNil(r.Lo) {
				raise("RangeError", "cannot get the minimum of beginless range")
			}
			elems := rangeElems(r)
			n := clampCount(vm.repeatLong(args[0]), len(elems))
			out := make([]object.Value, n)
			copy(out, elems[:n])
			return object.NewArrayFromSlice(out)
		}
		if blk != nil {
			return vm.rangeExtremeByBlock(r, blk, true)
		}
		// The minimum is the begin, computed from the endpoints without iterating
		// (so Float/Time/Comparable ranges work): beginless raises, an empty range
		// is nil, otherwise the begin.
		if object.IsNil(r.Lo) {
			raise("RangeError", "cannot get the minimum of beginless range")
		}
		if rangeEmpty(vm, r) {
			return object.NilV
		}
		return r.Lo
	})
	vm.cRange.define("max", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		r := self.(*object.Range)
		if len(args) > 0 { // max(n): the n largest, descending
			if object.IsNil(r.Hi) {
				raise("RangeError", "cannot get the maximum of endless range")
			}
			elems := rangeElems(r)
			n := clampCount(vm.repeatLong(args[0]), len(elems))
			out := make([]object.Value, n)
			for i := 0; i < n; i++ {
				out[i] = elems[len(elems)-1-i]
			}
			return object.NewArrayFromSlice(out)
		}
		if blk != nil {
			return vm.rangeExtremeByBlock(r, blk, false)
		}
		// The maximum comes from the endpoints without iterating: endless raises, an
		// empty range is nil, an inclusive range is the end. An exclusive range with
		// an Integer end is end-1; with any other end it is the last element reached
		// by #succ (a String range yields it, a Float range raises TypeError).
		if object.IsNil(r.Hi) {
			raise("RangeError", "cannot get the maximum of endless range")
		}
		if rangeEmpty(vm, r) {
			return object.NilV
		}
		if r.Exclusive {
			// An Integer (or Bignum) end excludes by yielding its predecessor.
			switch r.Hi.(type) {
			case object.Integer, *object.Bignum:
				return vm.send(r.Hi, "-", []object.Value{object.IntValue(1)}, nil)
			}
			// A numeric (Float/Rational) end, or a beginless range, has no
			// predecessor reachable by #succ.
			if object.IsNil(r.Lo) || isNumericValue(r.Hi) {
				raise("TypeError", "cannot exclude non Integer end value")
			}
			// A non-empty iterable range (e.g. String): the last element before
			// the excluded end. Emptiness was already handled by rangeEmpty above.
			elems := rangeElems(r)
			return elems[len(elems)-1]
		}
		return r.Hi
	})
	rangeSizeFn := func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(rangeSize(self.(*object.Range)))
	}
	vm.cRange.define("size", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.rangeSizeVal(self.(*object.Range))
	})
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
		return object.IntValue(n)
	})
	vm.cRange.define("to_a", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewArrayFromSlice(rangeElems(self.(*object.Range)))
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
				out[i] = object.IntValue(int64(lo) + int64(i))
			}
			return object.NewArrayFromSlice(out)
		}
		elems := rangeElems(r)
		n = clampCount(int64(n), len(elems))
		out := make([]object.Value, n)
		copy(out, elems[:n])
		return object.NewArrayFromSlice(out)
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
		return object.NewArrayFromSlice(out)
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
		return object.NewArrayFromSlice(out)
	})
	vm.cRange.define("step", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		step := object.Value(object.IntValue(1))
		if len(args) > 0 {
			step = args[0]
		}
		if blk == nil {
			// With a size, for the reason Integer#step has one: without it
			// Enumerator#size counts by walking, and (1..Float::INFINITY).step
			// never finishes walking.
			r := self.(*object.Range)
			return enumForSized(self, "step", func(*VM) object.Value {
				return rangeStepSize(r, step)
			}, args...)
		}
		r := self.(*object.Range)
		// A String range steps by #succ: MRI requires an Integer step and yields
		// every step-th element of the begin..end succ-walk. An endless String
		// range (e.g. "A"..) walks forever — the caller's block breaks out.
		_, hiIsStr := r.Hi.(*object.String)
		if loS, ok := r.Lo.(*object.String); ok && (object.IsNil(r.Hi) || hiIsStr) {
			n, isInt := step.(object.Integer)
			if !isInt {
				raise("TypeError", "no implicit conversion of %s into Integer", vm.classOf(step).name)
			}
			switch {
			case n == 0:
				raise("ArgumentError", "step can't be 0")
			case n < 0:
				raise("ArgumentError", "step can't be negative")
			}
			if object.IsNil(r.Hi) {
				for cur := loS.Str(); ; {
					vm.callBlock(blk, []object.Value{object.NewString(cur)})
					for k := int64(0); k < int64(n); k++ {
						cur = succString(cur)
					}
				}
			}
			elems := strRangeElems(loS.Str(), r.Hi.(*object.String).Str(), r.Exclusive)
			for i := 0; i < len(elems); i += int(n) {
				vm.callBlock(blk, []object.Value{elems[i]})
			}
			return r
		}
		// An endless numeric range (m..) steps forever; the block breaks out.
		if object.IsNil(r.Hi) {
			vm.numericStepEndless(blk, r.Lo, step)
			return r
		}
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
		return object.IntValue(gcdInt(intOf(self), intArg(args[0])))
	})
	vm.cInteger.define("lcm", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a, b := intOf(self), intArg(args[0])
		if a == 0 || b == 0 {
			return object.IntValue(0)
		}
		return object.IntValue(absInt(a / gcdInt(a, b) * b))
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
			return object.NewArray(other, self)
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
		return object.NewArray(object.Float(of), object.Float(sf))
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
		return object.IntValue(c)
	})
	vm.cInteger.define("divmod", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a, b := intOf(self), intArg(args[0])
		if b == 0 {
			raise("ZeroDivisionError", "divided by 0")
		}
		return object.NewArray(object.IntValue(floorDiv(a, b)), object.IntValue(floorMod(a, b)))
	})
	vm.cInteger.define("gcdlcm", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a, b := intOf(self), intArg(args[0])
		g := gcdInt(a, b)
		var lcm int64
		if a != 0 && b != 0 {
			lcm = absInt(a / g * b)
		}
		return object.NewArray(object.IntValue(g), object.IntValue(lcm))
	})
	vm.cInteger.define("remainder", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		// remainder truncates toward zero (keeping the dividend's sign), unlike %
		// which floors — exactly Go's % operator.
		a, b := intOf(self), intArg(args[0])
		if b == 0 {
			raise("ZeroDivisionError", "divided by 0")
		}
		return object.IntValue(a % b)
	})
	// truncate with ndigits >= 0 leaves an Integer unchanged; with ndigits < 0 it
	// truncates toward zero to the nearest 10**(-ndigits). Integer#round is defined
	// (with the half: keyword) in registerNumericEdges, which runs later.
	vm.cInteger.define("truncate", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		n := intArgOr(args, 0)
		if n >= 0 {
			return self
		}
		pow, ok := pow10(-n)
		if !ok {
			return object.IntValue(0)
		}
		a, neg := absSign(intOf(self))
		r := (a / pow) * pow
		if neg {
			r = -r
		}
		return object.IntValue(r)
	})
	vm.cInteger.define("floor", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		// floor(n>=0) is self; floor(n<0) rounds toward negative infinity to the
		// nearest multiple of 10**(-n).
		n := intArgOr(args, 0)
		if n >= 0 {
			return self
		}
		pow, ok := pow10(-n)
		if !ok {
			return object.IntValue(0) // 10**(-n) exceeds int64; the result is not int64-representable
		}
		return object.IntValue(floorDiv(intOf(self), pow) * pow)
	})
	vm.cInteger.define("ceil", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		// ceil(n>=0) is self; ceil(n<0) rounds toward positive infinity.
		n := intArgOr(args, 0)
		if n >= 0 {
			return self
		}
		pow, ok := pow10(-n)
		if !ok {
			return object.IntValue(0)
		}
		return object.IntValue(-floorDiv(-intOf(self), pow) * pow)
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
			return object.NewArray(object.IntValue(0))
		}
		var out []object.Value
		for n > 0 {
			out = append(out, object.IntValue(n%base))
			n /= base
		}
		return object.NewArrayFromSlice(out)
	})
	vm.cInteger.define("chr", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if _, big := self.(*object.Bignum); big {
			raise("RangeError", "bignum out of char range")
		}
		n := intOf(self)
		if len(args) == 0 {
			// No encoding: a 7-bit value is US-ASCII, an 8-bit value is ASCII-8BIT.
			if n < 0 || n > 255 {
				raise("RangeError", "%d out of char range", n)
			}
			enc := "US-ASCII"
			if n > 127 {
				enc = "ASCII-8BIT"
			}
			return object.NewStringBytesEnc([]byte{byte(n)}, enc)
		}
		enc := vm.encodingName(args[0])
		return object.NewStringBytesEnc(chrEncode(n, enc), enc)
	})
	vm.cInteger.define("upto", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		if len(args) < 1 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		if blk == nil {
			return enumFor(self, "upto", args...)
		}
		for i := intOf(self); i <= intArg(args[0]); i++ {
			vm.callBlock(blk, []object.Value{object.IntValue(i)})
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
			vm.callBlock(blk, []object.Value{object.IntValue(i)})
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
		return object.IntValue(int64(floatOf(self)))
	})
	vm.cFloat.define("to_int", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(floatOf(self)))
	})
	vm.cFloat.define("ceil", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return floatRound(floatOf(self), args, math.Ceil)
	})
	vm.cFloat.define("floor", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return floatRound(floatOf(self), args, math.Floor)
	})
	// Float#round is defined (with the half: keyword) in registerNumericEdges,
	// which runs later and overrides any definition here.
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
		return object.NewArray(object.IntValue(int64(q)), object.Float(a-b*q))
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
	// Integer#to_r / #rationalize are exact — self is already an integer, so both
	// return self/1 (rationalize ignoring any precision argument).
	intToR := func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return &object.Rational{R: new(big.Rat).SetInt(bigVal(self))}
	}
	vm.cInteger.define("to_r", intToR)
	vm.cInteger.define("rationalize", intToR)
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
			return object.IntValue(1)
		}
		if math.IsInf(f, -1) {
			return object.IntValue(-1)
		}
		return object.NilV
	})

	// Class.
	vm.cClass.define("new", nativeNew)
	// allocate creates an uninitialized instance (no initialize call), as MRI's
	// Class#allocate — used by frameworks that construct then initialize manually.
	vm.cClass.define("allocate", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		obj := &RObject{class: self.(*RClass), ivars: map[string]object.Value{}}
		vm.registerLiveObject(obj)
		return obj
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
			vm.registerLiveClass(c)
			if blk != nil {
				vm.classEval(c, blk, nil)
			}
			return c
		}}
	// Module.new { body } builds an anonymous module (no superclass, like every
	// module), running the block as its body — so `def self.foo` adds a singleton
	// method and `def foo` an instance method that mixes into includers. Without
	// this dedicated handler, `Module.new` would fall through to Class#new and
	// allocate a plain instance (dropping the block), so the result could not be
	// included or have methods defined into it.
	vm.cModule.smethods["new"] = &Method{name: "new", owner: vm.cModule,
		native: func(vm *VM, _ object.Value, _ []object.Value, blk *Proc) object.Value {
			m := newClass("", nil)
			m.isModule = true
			m.defaultVis, m.funcMode = visPublic, false
			vm.registerLiveClass(m)
			if blk != nil {
				vm.classEval(m, blk, nil)
			}
			return m
		}}
	// Module.nesting returns the list of Modules nested at the point of call,
	// innermost first (MRI: the lexical cref chain, excluding Object). The caller's
	// frame is on top of frameCrefs (this native pushes no frame of its own).
	vm.cModule.smethods["nesting"] = &Method{name: "nesting", owner: vm.cModule,
		native: func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
			var cref *RClass
			if n := len(vm.frameCrefs); n > 0 {
				cref = vm.frameCrefs[n-1]
			}
			arr := object.NewArray()
			for _, c := range vm.nesting(cref) {
				arr.Elems = append(arr.Elems, c)
			}
			return arr
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
		step := object.Value(object.IntValue(1))
		if len(args) > 1 {
			step = args[1]
		}
		if blk == nil {
			// With a size, so that Enumerator#size can answer without walking
			// the sequence. Asked to count 1.step(Float::INFINITY) it never
			// would: it allocated 30.9 GB before a CI runner died under it.
			limit := args[0]
			return enumForSized(self, "step", func(*VM) object.Value {
				return stepSize(self, limit, step)
			}, args...)
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
			arg[0] = object.IntValue(i)
			vm.callBlock(blk, arg)
		}
		return self
	})

	vm.installRegexp()
	setupStruct(vm)
	setupData(vm) // Data.define immutable value objects (Ruby 3.2+); shares structVals storage
	// These depend on Struct (Etc::Passwd/Group) and the core collections, so they
	// run after setupStruct and the prelude-defined Enumerable/Hash/Array.
	vm.registerEtc()        // Etc module (real pw/grp via os/user; systmpdir); needs Struct + Enumerable
	vm.registerConcurrent() // concurrent-ruby shell (collections alias core; ThreadLocalVar)
	vm.registerENV()        // ENV: Hash-like view of the process environment over os
	// Numeric edge methods (Integer#[]/#size/#ord/#round, Float#round(half:),
	// #next_float/#prev_float): run last so the round overrides win.
	vm.registerNumericEdges()
	// Range edge methods (#reverse_each, #entries); runs after the prelude so the
	// range-specific definitions win over any inherited Enumerable ones.
	vm.registerRangeEdges()
	// Split the Kernel module functions into a private instance method + a public
	// Kernel-module method, as MRI does — runs last, once every listed method is
	// defined above.
	vm.registerKernelModuleFunctions()
}

// registerKernelModuleFunctions applies MRI's module_function split to the Kernel
// methods that carry it: each becomes a PRIVATE instance method (so `Integer(x)`
// works but `obj.Integer(x)` does not) and a PUBLIC method on the Kernel module
// itself (so `Kernel.Integer(x)` works and it appears in Kernel.public_methods).
// The bodies live on Object; this only reflects the visibility split MRI reports
// through Kernel.private_instance_methods / Kernel.public_methods, mirroring the
// records onto cKernel exactly as __method__/__callee__ already do. The set is
// Kernel.private_instance_methods(false) from ruby 4.0.6 intersected with what
// rbgo defines; a name absent on this build (e.g. fork/exec/system under wasm) is
// simply skipped. Runs after every listed method is registered.
func (vm *VM) registerKernelModuleFunctions() {
	names := []string{
		"Array", "Complex", "Float", "Integer", "Rational", "String",
		"__dir__", "abort", "at_exit", "autoload", "autoload?", "caller",
		"catch", "eval", "exec", "exit", "exit!", "fork", "format", "lambda",
		"load", "loop", "open", "p", "print", "printf", "proc", "puts",
		"raise", "rand", "require", "require_relative", "sleep", "sprintf",
		"srand", "system", "throw", "trap", "warn",
	}
	for _, name := range names {
		if m := vm.cObject.methods[name]; m != nil {
			m.vis = visPrivate
			priv := *m
			priv.owner, priv.vis = vm.cKernel, visPrivate
			vm.cKernel.methods[name] = &priv
			pub := *m
			pub.owner, pub.vis = vm.cKernel, visPublic
			vm.cKernel.smethods[name] = &pub
		}
	}
}

// nativeNew allocates an instance of the receiver class and runs initialize,
// forwarding any block.
func nativeNew(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
	class := self.(*RClass)
	obj := &RObject{class: class, ivars: map[string]object.Value{}}
	vm.registerLiveObject(obj)
	vm.send(obj, "initialize", args, blk)
	return obj
}

// newBuiltinSubclass allocates an instance of a user subclass recv of a built-in
// value type, wrapping a fresh zero value, then runs initialize — which
// populates the wrapped value from args (dispatched onto it via callNative's
// unwrap), so each value type's own constructor semantics are reused unchanged.
func (vm *VM) newBuiltinSubclass(recv *RClass, zero object.Value, args []object.Value, blk *Proc) object.Value {
	obj := &RObject{class: recv, ivars: map[string]object.Value{}, builtin: zero}
	vm.registerLiveObject(obj)
	vm.send(obj, "initialize", args, blk)
	return obj
}

// hashDefault returns the value a missing key reads as: the default proc's
// result (called with the hash and key), else the static default, else nil.
func (vm *VM) hashDefault(h *object.Hash, key object.Value) object.Value {
	if !object.IsNil(h.DefaultProc) {
		return vm.callBlock(h.DefaultProc.(*Proc), []object.Value{h, key})
	}
	if h.Default != nil {
		return h.Default
	}
	return object.NilV
}

// intArg coerces an argument used as an array index to int64, or raises.
// chrEncode encodes the codepoint n as the bytes of a one-character string in
// encoding enc, backing Integer#chr(enc). UTF-8 accepts any scalar value (a
// surrogate or out-of-range value raises RangeError); US-ASCII accepts 0..127;
// ASCII-8BIT and other single-byte encodings accept 0..255.
func chrEncode(n int64, enc string) []byte {
	switch enc {
	case "UTF-8":
		if n < 0 || n > 0x10FFFF || (n >= 0xD800 && n <= 0xDFFF) {
			raise("RangeError", "%d out of char range", n)
		}
		return []byte(string(rune(n)))
	case "US-ASCII":
		if n < 0 || n > 127 {
			raise("RangeError", "%d out of char range", n)
		}
		return []byte{byte(n)}
	default: // ASCII-8BIT/BINARY and single-byte encodings hold one 0..255 byte
		if n < 0 || n > 255 {
			raise("RangeError", "%d out of char range", n)
		}
		return []byte{byte(n)}
	}
}

// codepointAppend encodes the codepoint n for `String#<<`/#concat with an Integer
// argument, returning its bytes and the resulting string encoding. It mirrors
// chrEncode but, like MRI, promotes a US-ASCII receiver to ASCII-8BIT when the
// codepoint is 128..255 (an 8-bit byte that US-ASCII cannot hold).
func codepointAppend(n int64, enc string) ([]byte, string) {
	if enc == "US-ASCII" && n >= 128 && n <= 255 {
		return []byte{byte(n)}, "ASCII-8BIT"
	}
	return chrEncode(n, enc), enc
}

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

// sqlite3IntArg coerces an argument to an int64, raising a TypeError for a
// non-integer. It also accepts a Bignum (narrowed to int64). It lives here, in
// the build-tag-free core, rather than with the SQLite3 binding because it is
// shared with a non-guarded consumer (Nokogiri::XML::NodeSet#[]); the SQLite3
// binding is behind //go:build !(js && wasm), so keeping this helper there would
// break the js/wasm build.
func sqlite3IntArg(v object.Value) int64 {
	switch n := v.(type) {
	case object.Integer:
		return int64(n)
	case *object.Bignum:
		return n.I.Int64()
	}
	raise("TypeError", "no implicit conversion to Integer")
	return 0
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
	return object.IntValue(int64(r))
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

// arrayArefSpan resolves Array#[]/#slice/#slice! arguments to a location in a.
// A lone in-range index yields isSpan=false with length 1; a Range or (start,
// length) pair yields isSpan=true spanning [start, start+length). ok is false
// only when the request falls entirely outside the array (aref returns nil) —
// an empty-but-valid span (e.g. a[1..0]) returns ok=true, isSpan=true, length 0.
// coerceRangeBounds returns a Range whose endpoints are Integers (or nil for an
// endless/beginless bound), converting any other endpoint through #to_int as MRI
// does for slice/aref range arguments. When both endpoints are already Integer
// or nil the original range is returned unchanged.
func (vm *VM) coerceRangeBounds(r *object.Range) *object.Range {
	conv := func(v object.Value) object.Value {
		switch v.(type) {
		case object.Nil, object.Integer:
			return v
		default:
			return object.IntValue(vm.repeatLong(v))
		}
	}
	lo, hi := conv(r.Lo), conv(r.Hi)
	if lo == r.Lo && hi == r.Hi {
		return r
	}
	return object.NewRange(lo, hi, r.Exclusive)
}

// rngKwarg returns the object passed as the random: keyword of a shuffle/sample
// call, or nil when absent (then the VM's default generator is used).
func (vm *VM) rngKwarg(args []object.Value) object.Value {
	if h := trailingKwHash(args); h != nil {
		if v, ok := h.Get(object.SymVal("random")); ok && !object.IsNil(v) {
			return v
		}
	}
	return nil
}

// drawIndex returns a random index in [0, bound) (bound must be >= 1). With rng
// nil it draws from the VM's default generator; otherwise it calls rng.rand(bound)
// and interprets the result MRI-style — a Float scales to floor(f*bound); any
// other value converts via #to_int and must land in [0, bound) or RangeError.
func (vm *VM) drawIndex(rng object.Value, bound int) int {
	if rng == nil {
		return int(vm.defaultRandom.limitedRand(uint64(bound) - 1))
	}
	v := vm.send(rng, "rand", []object.Value{object.IntValue(int64(bound))}, nil)
	if f, ok := v.(object.Float); ok {
		return int(float64(f) * float64(bound))
	}
	idx := vm.repeatLong(v)
	if idx < 0 || idx >= int64(bound) {
		raise("RangeError", "random number too big %d", idx)
	}
	return int(idx)
}

// fisherYates shuffles s in place using rng (nil = the default generator),
// drawing each swap partner from the unshuffled prefix as MRI's Array#shuffle.
func (vm *VM) fisherYates(s []object.Value, rng object.Value) {
	for i := len(s) - 1; i >= 1; i-- {
		j := vm.drawIndex(rng, i+1)
		s[i], s[j] = s[j], s[i]
	}
}

func (vm *VM) arrayArefSpan(a *object.Array, args []object.Value) (start, length int, isSpan, ok bool) {
	if rng, isR := args[0].(*object.Range); isR {
		// A non-nil, non-Integer endpoint converts via #to_int (e.g. a[obj..obj]).
		s, l, r := sliceRange(len(a.Elems), vm.coerceRangeBounds(rng))
		return s, l, true, r
	}
	if len(args) == 2 { // a[start, len] — start and length convert via #to_int.
		s := normIndex(vm.repeatLong(args[0]), len(a.Elems))
		l := int(vm.repeatLong(args[1]))
		if s < 0 || s > len(a.Elems) || l < 0 {
			return 0, 0, true, false
		}
		if s+l > len(a.Elems) {
			l = len(a.Elems) - s
		}
		return s, l, true, true
	}
	if i, isok := arrayIndex(a, vm.repeatLong(args[0])); isok {
		return i, 1, false, true
	}
	return 0, 0, false, false
}

// Kernel#puts/print/p write through the current $stdout (an IOObj), so a host
// or program that reassigns $stdout — e.g. to a StringIO — captures the output,
// as in MRI. The puts array-flattening/newline logic lives in io.go (ioPuts).
func nativePuts(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
	vm.ioPuts(vm.curStdout(), args)
	return object.NilV
}

// nativePrintf implements Kernel#printf. With a String first argument it formats
// the remaining arguments and writes to the current $stdout; otherwise the first
// argument is the output target (any object answering #write, e.g. an IO,
// StringIO, or a mock) and the format string follows. The formatted text is
// always delivered through #write so a reassigned or mocked target captures it,
// matching MRI.
func nativePrintf(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
	if len(args) == 0 {
		return object.NilV
	}
	var target object.Value
	var fmtArgs []object.Value
	if _, ok := args[0].(*object.String); ok {
		target = vm.stdoutValue()
		fmtArgs = args
	} else {
		target = args[0]
		fmtArgs = args[1:]
	}
	if len(fmtArgs) == 0 {
		raise("ArgumentError", "wrong number of arguments (given 0, expected 1+)")
	}
	str := vm.formatString(vm.coerceFormatString(fmtArgs[0]), fmtArgs[1:])
	vm.send(target, "write", []object.Value{object.NewString(str)}, nil)
	return object.NilV
}

// stdoutValue returns the object currently bound to $stdout (an IOObj, a
// StringIO, or a host-supplied object), falling back to the default stdout IO
// when the global is unset.
func (vm *VM) stdoutValue() object.Value {
	if v, ok := vm.globals["$stdout"]; ok {
		return v
	}
	return vm.curStdout()
}

// coerceFormatString returns the format string for Kernel#sprintf/#format/#printf:
// a String directly, otherwise the result of the argument's #to_str (MRI coerces
// the format argument with #to_str, never #to_s). A missing #to_str or a
// non-String #to_str result raises TypeError with MRI's message.
func (vm *VM) coerceFormatString(v object.Value) string {
	if s, ok := v.(*object.String); ok {
		return s.Str()
	}
	if o, ok := v.(*RObject); ok && vm.respondsToDynamic(o, "to_str") {
		r := vm.send(o, "to_str", nil, nil)
		if s, ok := r.(*object.String); ok {
			return s.Str()
		}
		raise("TypeError", "can't convert %s to String (%s#to_str gives %s)",
			vm.classOf(o).name, vm.classOf(o).name, vm.classOf(r).name)
	}
	raise("TypeError", "no implicit conversion of %s into String", classNameOf(v))
	return ""
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

// chompSep implements String#chomp's optional separator argument, matching MRI:
//   - no argument (or an explicit nil): remove one trailing line ending.
//   - "" (empty): paragraph mode — remove ALL trailing newlines (\r\n / \n).
//   - any other string: remove that exact suffix once, if present.
func chompSep(s string, args []object.Value) string {
	if len(args) == 0 || args[0] == object.NilV {
		return chompStr(s)
	}
	sep := strArg(args[0])
	if sep == "" {
		// Paragraph mode: strip every trailing \n (treating \r\n as one), so a run
		// of blank lines at the end collapses away.
		for {
			switch {
			case strings.HasSuffix(s, "\r\n"):
				s = s[:len(s)-2]
			case strings.HasSuffix(s, "\n"):
				s = s[:len(s)-1]
			default:
				return s
			}
		}
	}
	return strings.TrimSuffix(s, sep)
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
		return object.IntValue(0)
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

// strOct implements String#oct: it reads a leading integer — optional whitespace,
// an optional sign, an optional 0x/0b/0o/0d base prefix (default octal), then the
// digits valid for that base (with the single-underscore separator rule) — and
// stops at the first invalid character, returning 0 when no digits are present.
func strOct(s string) object.Value {
	i, n := 0, len(s)
	for i < n {
		switch s[i] {
		case ' ', '\t', '\n', '\r', '\v', '\f':
			i++
			continue
		}
		break
	}
	neg := false
	if i < n && (s[i] == '+' || s[i] == '-') {
		neg = s[i] == '-'
		i++
	}
	base := 8
	if i+1 < n && s[i] == '0' {
		switch s[i+1] {
		case 'x', 'X':
			base, i = 16, i+2
		case 'b', 'B':
			base, i = 2, i+2
		case 'o', 'O':
			base, i = 8, i+2
		case 'd', 'D':
			base, i = 10, i+2
		}
	}
	var digits []byte
	prevUnderscore := false
	for i < n {
		if s[i] == '_' {
			if len(digits) == 0 || prevUnderscore {
				break
			}
			prevUnderscore = true
			i++
			continue
		}
		if digitValue(s[i]) >= base {
			break
		}
		digits = append(digits, s[i])
		prevUnderscore = false
		i++
	}
	if len(digits) == 0 {
		return object.IntValue(0)
	}
	z, _ := new(big.Int).SetString(string(digits), base)
	if neg {
		z.Neg(z)
	}
	return object.NormInt(z)
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
	return stringIndexEnc(s, args, false)
}

// stringIndexEnc implements String#[] / #slice indexing. When binary is true the
// receiver is ASCII-8BIT, so the integer index/length forms count BYTES (not
// characters) and the sliced result stays binary — matching MRI, where index
// and length on an ASCII-8BIT string are byte offsets. For the default (UTF-8)
// case it indexes by character, unchanged. The substring form (s[sub]) is a
// byte-wise Contains either way, so it needs no encoding branch.
func stringIndexEnc(s string, args []object.Value, binary bool) object.Value {
	// units is the addressable sequence: bytes for a binary string, runes
	// otherwise. take(lo, hi) rebuilds the corresponding substring.
	var n int
	var take func(lo, hi int) string
	if binary {
		b := []byte(s)
		n = len(b)
		take = func(lo, hi int) string { return string(b[lo:hi]) }
	} else {
		r := []rune(s)
		n = len(r)
		take = func(lo, hi int) string { return string(r[lo:hi]) }
	}
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
		return object.NewStringView(take(start, end))
	}
	if rng, ok := args[0].(*object.Range); ok {
		start, length, ok := sliceRange(n, rng)
		if !ok {
			return object.NilV
		}
		return object.NewStringView(take(start, start+length))
	}
	if sub, ok := args[0].(*object.String); ok { // s[substr] -> the substring if present, else nil
		if strings.Contains(s, sub.Str()) {
			return object.NewStringView(sub.Str())
		}
		return object.NilV
	}
	i := normIndex(intArg(args[0]), n)
	if i < 0 || i >= n {
		return object.NilV
	}
	return object.NewStringView(take(i, i+1))
}

// byteslice returns a substring by BYTE offsets (not characters), the way MRI's
// String#byteslice does: byteslice(i) is the 1-byte string at i (nil if out of
// range), byteslice(i, len) is len bytes from i (clamped to the end; nil for a
// negative start out of range or a negative length), and byteslice(range) slices
// by byte range. The result keeps the receiver's encoding.
func byteslice(self *object.String, args []object.Value) object.Value {
	b := []byte(self.Str())
	n := len(b)
	mk := func(sub []byte) object.Value {
		s := object.NewStringView(string(sub))
		s.Enc = self.Enc
		return s
	}
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
		return mk(b[start:end])
	}
	if rng, ok := args[0].(*object.Range); ok {
		start, length, ok := sliceRange(n, rng)
		if !ok {
			return object.NilV
		}
		return mk(b[start : start+length])
	}
	i := normIndex(intArg(args[0]), n)
	if i < 0 || i >= n {
		return object.NilV
	}
	return mk(b[i : i+1])
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

// arraySpliceAssign implements Ruby's a[start, length] = value slice assignment.
// start is already resolved to a non-negative offset; length is non-negative.
// If start is beyond the current end, the array is padded with nil. The replaced
// span is the `length` elements at start (clamped to the end). When value is an
// Array its elements are spliced in; any other value (including nil) becomes a
// single element.
func arraySpliceAssign(a *object.Array, start, length int, value object.Value) {
	// Pad with nil so the array reaches start.
	for len(a.Elems) < start {
		a.Elems = append(a.Elems, object.NilV)
	}
	end := start + length
	if end > len(a.Elems) {
		end = len(a.Elems)
	}
	var repl []object.Value
	if arr, ok := value.(*object.Array); ok {
		repl = arr.Elems
	} else {
		repl = []object.Value{value}
	}
	tail := append([]object.Value{}, a.Elems[end:]...)
	out := append(a.Elems[:start:start], repl...)
	a.Elems = append(out, tail...)
}

// normIndex resolves a possibly-negative index against length n (no clamping of
// the upper bound; callers range-check).
func normIndex(i int64, n int) int {
	if i < 0 {
		return int(i) + n
	}
	return int(i)
}

// affixString coerces a String#delete_prefix/#delete_suffix argument to a
// String via #to_str, raising TypeError when it is not a String and has no
// (String-returning) #to_str.
func (vm *VM) affixString(v object.Value) *object.String {
	if s, ok := v.(*object.String); ok {
		return s
	}
	if vm.respondsToDynamic(v, "to_str") {
		r := vm.send(v, "to_str", nil, nil)
		if s, ok := r.(*object.String); ok {
			return s
		}
		raise("TypeError", "can't convert %s to String (%s#to_str gives %s)",
			vm.classOf(v).name, vm.classOf(v).name, vm.classOf(r).name)
	}
	raise("TypeError", "no implicit conversion of %s into String", classNameOf(v))
	return nil
}

// deletedAffixLen returns how many bytes String#delete_prefix/#delete_suffix
// should strip (suffix selects the end): the argument is coerced via #to_str,
// then a broken (invalid-encoding) or empty affix strips nothing, the encodings
// must be compatible (rb_enc_check, which raises otherwise), and the affix must
// match at the chosen end on a full byte boundary.
func (vm *VM) deletedAffixLen(s *object.String, arg object.Value, suffix bool) int {
	affix := vm.affixString(arg)
	ab := affix.Bytes()
	if len(ab) == 0 || !validInEncoding(ab, affix.EncName()) {
		return 0
	}
	vm.combinedEncName(s, affix) // rb_enc_check: raises Encoding::CompatibilityError on incompatibility
	sb := s.Bytes()
	if len(sb) < len(ab) {
		return 0
	}
	if suffix {
		if string(sb[len(sb)-len(ab):]) == string(ab) {
			return len(ab)
		}
		return 0
	}
	if string(sb[:len(ab)]) == string(ab) {
		return len(ab)
	}
	return 0
}

// checkFrozen raises FrozenError when a mutator is applied to a frozen string,
// stamping the string as the exception's #receiver (as MRI).
func (vm *VM) checkFrozen(s *object.String) {
	if s.Frozen {
		vm.raiseWithIvars("FrozenError", "can't modify frozen String: "+s.Inspect(),
			map[string]object.Value{"@receiver": s})
	}
}

// strAppendBytes is the byte contribution of a #prepend argument: a String
// contributes its bytes; any other value is a TypeError (unlike #<<, #prepend
// does not take an Integer codepoint).
func strAppendBytes(a object.Value) []byte {
	if v, ok := a.(*object.String); ok {
		return v.Bytes()
	}
	raise("TypeError", "no implicit conversion of %s into String", classNameOf(a))
	return nil
}

// strBang applies a pure transform to the receiver in place. As a Ruby bang
// method it returns the (mutated) receiver when the content changed, else nil.
func (vm *VM) strBang(self object.Value, fn func(string) string) object.Value {
	s := self.(*object.String)
	vm.checkFrozen(s)
	out := fn(s.Str())
	if out == s.Str() {
		return object.NilV
	}
	s.SetBytes([]byte(out))
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

// strSubBang backs String#sub!/#gsub!: it applies the same substitution as
// sub/gsub and writes the result back, returning the receiver when it changed
// and nil otherwise.
func (vm *VM) strSubBang(self object.Value, args []object.Value, blk *Proc, global bool) object.Value {
	s := self.(*object.String)
	vm.checkFrozen(s)
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
	if res.Str() == s.Str() {
		return object.NilV
	}
	s.TakeFrom(res)
	return s
}

// stringIndexAssign backs String#[]=: it replaces the indexed slice (an index,
// a start+length, or a Range) with the replacement string and returns the
// replacement (Ruby's result for an assignment).
// strPatternCompat coerces a search pattern to its Go string; when the pattern is
// a String whose encoding is incompatible with self.s, it raises
// Encoding::CompatibilityError, matching MRI.s search methods.
func (vm *VM) strPatternCompat(self, pat object.Value) string {
	if ps, ok := pat.(*object.String); ok {
		vm.combinedEncName(self.(*object.String), ps) // raises if the encodings are incompatible
		return ps.Str()
	}
	return strArg(pat)
}

func (vm *VM) stringIndexAssign(s *object.String, args []object.Value) object.Value {
	vm.checkFrozen(s)
	rhs := args[len(args)-1]
	// The replacement.s encoding is negotiated with the receiver.s (before any
	// mutation, so an incompatible pair raises without changing the string).
	if rs, ok := rhs.(*object.String); ok {
		s.Enc = vm.combinedEncName(s, rs)
	}
	// A String subclass selector is unwrapped to the value it wraps.
	arg0 := args[0]
	if u, ok := arg0.(object.KeyUnwrapper); ok {
		if w, wrapped := u.HashUnwrap(); wrapped {
			arg0 = w
		}
	}
	// Regexp form: replace the whole match, or a capture group.
	if re, ok := arg0.(*Regexp); ok {
		return vm.stringAssignRegexp(s, re, args[1:len(args)-1], rhs)
	}
	// String form: replace the first occurrence of the substring.
	if sub, ok := arg0.(*object.String); ok && len(args) == 2 {
		return vm.stringAssignSubstr(s, sub.Str(), rhs)
	}
	repl := vm.coerceFormatString(rhs)
	r := []rune(s.Str())
	n := len(r)
	start, length := vm.stringAssignSpan(args, n)
	out := append(append(append([]rune{}, r[:start]...), []rune(repl)...), r[start+length:]...)
	s.SetBytes([]byte(string(out)))
	return rhs
}

// stringAssignRegexp replaces the match (or capture group) of re in s with rhs,
// setting $~; a missing match, or an out-of-range/non-participating group, raises
// IndexError (unlike #[], which reads nil).
func (vm *VM) stringAssignRegexp(s *object.String, re *Regexp, groupArgs []object.Value, rhs object.Value) object.Value {
	subject := s.Str()
	md := re.matcher().Match(subject)
	if md == nil {
		vm.lastMatch = object.NilV
		raise("IndexError", "regexp not matched")
	}
	m := &MatchData{md: md, subject: subject, re: re}
	vm.lastMatch = m
	gi := 0
	if len(groupArgs) > 0 {
		gi = int(vm.repeatLong(groupArgs[0]))
		ng := md.NGroups()
		if gi < 0 {
			gi += ng + 1
			if gi <= 0 { // a negative index cannot reach the whole-match group 0
				raise("IndexError", "index %d out of regexp", gi)
			}
		}
		if gi > ng {
			raise("IndexError", "index %d out of regexp", gi)
		}
	}
	bstart, bend := md.Begin(gi), md.End(gi)
	if bstart < 0 {
		raise("IndexError", "regexp group %d not matched", gi)
	}
	repl := vm.coerceFormatString(rhs)
	s.SetBytes([]byte(subject[:bstart] + repl + subject[bend:]))
	return rhs
}

// stringAssignSubstr replaces the first occurrence of sub in s with rhs, raising
// IndexError when sub is absent.
func (vm *VM) stringAssignSubstr(s *object.String, sub string, rhs object.Value) object.Value {
	subject := s.Str()
	i := strings.Index(subject, sub)
	if i < 0 {
		raise("IndexError", "string not matched")
	}
	repl := vm.coerceFormatString(rhs)
	s.SetBytes([]byte(subject[:i] + repl + subject[i+len(sub):]))
	return rhs
}

// stringAssignSpan resolves the [index] / [start, len] / [range] target of a
// String#[]= into a (start, length) span, raising the IndexError/RangeError
// Ruby raises for an out-of-range target. Indices and Range bounds convert
// through #to_int.
func (vm *VM) stringAssignSpan(args []object.Value, n int) (start, length int) {
	if len(args) == 3 {
		idx := vm.repeatLong(args[0]) // #to_int is invoked exactly once
		start = normIndex(idx, n)
		length = int(vm.repeatLong(args[1]))
		if start < 0 || start > n {
			raise("IndexError", "index %d out of string", idx)
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
		st, ln, ok := sliceRange(n, vm.coerceRangeBounds(rng))
		if !ok {
			raise("RangeError", "%s out of range", rng.Inspect())
		}
		return st, ln
	}
	idx := vm.repeatLong(args[0])
	start = normIndex(idx, n)
	if start < 0 || start > n {
		raise("IndexError", "index %d out of string", idx)
	}
	if start == n { // index == length appends: replace no characters
		return start, 0
	}
	return start, 1
}

// stringSliceBang backs String#slice!: it removes the indexed slice from the
// receiver and returns it (nil when the index does not select anything).
func (vm *VM) stringSliceBang(s *object.String, args []object.Value) object.Value {
	vm.checkFrozen(s)
	// A String subclass instance is unwrapped to its underlying value so a
	// substring argument dispatches by the string it wraps.
	arg0 := args[0]
	if u, ok := arg0.(object.KeyUnwrapper); ok {
		if w, wrapped := u.HashUnwrap(); wrapped {
			arg0 = w
		}
	}
	// Regexp form: delete and return the whole match, or a capture group, of re.
	if re, ok := arg0.(*Regexp); ok {
		return vm.stringSliceBangRegexp(s, re, args[1:])
	}
	// String form: delete and return the first occurrence of the substring.
	if sub, ok := arg0.(*object.String); ok && len(args) == 1 {
		return vm.stringSliceBangSubstr(s, sub.Str())
	}
	// A binary (ASCII-8BIT) string slices by BYTES and the removed span stays
	// binary, matching MRI; a UTF-8 string slices by characters. Index and length
	// convert through #to_int.
	if s.IsBinary() {
		b := s.Bytes()
		n := len(b)
		start, length, ok := vm.sliceSpan(args, n)
		if !ok {
			return object.NilV
		}
		removed := append([]byte{}, b[start:start+length]...)
		s.SetBytes(append(append([]byte{}, b[:start]...), b[start+length:]...))
		return object.NewStringBytesEnc(removed, s.Enc)
	}
	r := []rune(s.Str())
	n := len(r)
	start, length, ok := vm.sliceSpan(args, n)
	if !ok {
		return object.NilV
	}
	removed := string(r[start : start+length])
	out := append(append([]rune{}, r[:start]...), r[start+length:]...)
	s.SetBytes([]byte(string(out)))
	return object.NewStringBytesEnc([]byte(removed), s.Enc)
}

// stringSliceBangRegexp deletes and returns the match (or capture group) of re in
// s, setting $~; it returns nil (with $~ = nil) when there is no match.
func (vm *VM) stringSliceBangRegexp(s *object.String, re *Regexp, rest []object.Value) object.Value {
	subject := s.Str()
	md := re.matcher().Match(subject)
	if md == nil {
		vm.lastMatch = object.NilV
		return object.NilV
	}
	m := &MatchData{md: md, subject: subject, re: re}
	vm.lastMatch = m
	gi := 0
	if len(rest) > 0 {
		// The capture index converts via #to_int; a negative index counts back
		// from the last group; an out-of-range index yields nil (not IndexError).
		gi = int(vm.repeatLong(rest[0]))
		ng := md.NGroups()
		if gi < 0 {
			gi += ng + 1
			if gi <= 0 { // a negative index cannot reach the whole-match group 0
				return object.NilV
			}
		}
		if gi > ng {
			return object.NilV
		}
	}
	bstart, bend := md.Begin(gi), md.End(gi)
	if bstart < 0 {
		// The capture group is in range but did not participate: slice! returns an
		// empty string and deletes nothing (unlike #[], which returns nil).
		return object.NewStringBytesEnc(nil, s.Enc)
	}
	removed := subject[bstart:bend]
	s.SetBytes([]byte(subject[:bstart] + subject[bend:]))
	return object.NewStringBytesEnc([]byte(removed), s.Enc)
}

// stringSliceBangSubstr deletes and returns the first occurrence of sub in s, or
// nil when sub is absent.
func (vm *VM) stringSliceBangSubstr(s *object.String, sub string) object.Value {
	subject := s.Str()
	i := strings.Index(subject, sub)
	if i < 0 {
		return object.NilV
	}
	s.SetBytes([]byte(subject[:i] + subject[i+len(sub):]))
	return object.NewStringBytesEnc([]byte(sub), s.Enc)
}

// sliceSpan resolves the [index] / [start, len] / [range] argument of slice!
// into a (start, length) span, reporting ok=false for an out-of-range selector
// (slice! then returns nil rather than raising). Integer arguments and Range
// bounds convert through #to_int.
func (vm *VM) sliceSpan(args []object.Value, n int) (start, length int, ok bool) {
	if len(args) == 2 {
		start = normIndex(vm.repeatLong(args[0]), n)
		length = int(vm.repeatLong(args[1]))
		if start < 0 || start > n || length < 0 {
			return 0, 0, false
		}
		if start+length > n {
			length = n - start
		}
		return start, length, true
	}
	if rng, isR := args[0].(*object.Range); isR {
		return sliceRange(n, vm.coerceRangeBounds(rng))
	}
	start = normIndex(vm.repeatLong(args[0]), n)
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
		if vm.respondsToDynamic(v, m) {
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

// scrubUTF8 returns s with every ill-formed UTF-8 byte sequence replaced by repl,
// so the result is valid UTF-8 (the encode :invalid=>:replace path). Valid runs are
// copied verbatim; each *maximal ill-formed subpart* collapses to a single repl,
// matching MRI — so a truncated multibyte lead followed by valid continuation bytes
// (e.g. "\xE3\x81") counts as one replacement, not one per byte. Callers pass only
// already-invalid input, so no fast path for wholly-valid strings is needed.
func scrubUTF8(s, repl string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if !(r == utf8.RuneError && size == 1) {
			b.WriteString(s[i : i+size])
			i += size
			continue
		}
		b.WriteString(repl)
		i += illFormedUTF8Len(s[i:])
	}
	return b.String()
}

// illFormedUTF8Len returns the length of the maximal ill-formed UTF-8 subpart at
// the start of s (whose first byte is not the start of a valid sequence): a lead
// byte plus the run of valid continuation bytes (0x80–0xBF) it could begin, capped
// at the length its lead implies. A lone continuation or invalid lead is length 1.
func illFormedUTF8Len(s string) int {
	var need int
	switch c := s[0]; {
	case c >= 0xC2 && c <= 0xDF:
		need = 2
	case c >= 0xE0 && c <= 0xEF:
		need = 3
	case c >= 0xF0 && c <= 0xF4:
		need = 4
	default:
		return 1
	}
	n := 1
	for n < need && n < len(s) && s[n]&0xC0 == 0x80 {
		n++
	}
	return n
}

// hashPair builds the [key, value] array Hash#each yields; block auto-splat then
// binds a two-parameter block element-wise, while a one-parameter block sees the
// pair (matching Ruby).
func hashPair(k, v object.Value) *object.Array {
	return object.NewArray(k, v)
}

// hashSubset reports whether every (key, value) pair of sub is present in sup,
// with values compared by valueEqual (MRI's rb_equal). It backs Hash#<= / #< /
// #>= / #>: a hash is a subset of another when all its pairs are contained.
func hashSubset(sub, sup *object.Hash) bool {
	if sub.Len() > sup.Len() {
		return false
	}
	for _, k := range sub.Keys {
		sv, _ := sub.Get(k)
		pv, present := sup.Get(k)
		if !present || !valueEqual(sv, pv) {
			return false
		}
	}
	return true
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

// arrayByExtremeN implements the n-argument form of min_by/max_by: it returns an
// Array of the n elements with the smallest (want=-1) or largest (want=1) block
// value, ordered by that value (ties keep their original order). n is clamped to
// the collection size; a negative n raises ArgumentError.
func (vm *VM) arrayByExtremeN(a *object.Array, blk *Proc, n, want int) object.Value {
	if n < 0 {
		raise("ArgumentError", "negative size (%d)", n)
	}
	type keyed struct {
		key, val object.Value
	}
	pairs := make([]keyed, len(a.Elems))
	for i, e := range a.Elems {
		pairs[i] = keyed{vm.callBlock(blk, []object.Value{e}), e}
	}
	sort.SliceStable(pairs, func(i, j int) bool {
		c := sign(vm.spaceship(pairs[i].key, pairs[j].key))
		if want < 0 {
			return c < 0 // min_by: ascending key
		}
		return c > 0 // max_by: descending key
	})
	if n > len(pairs) {
		n = len(pairs)
	}
	out := make([]object.Value, n)
	for i := 0; i < n; i++ {
		out[i] = pairs[i].val
	}
	return object.NewArrayFromSlice(out)
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

// flattenDepthChanged flattens like flattenDepth but also reports whether any
// nested array was actually expanded. flatten!/flatten share semantics, but the
// bang form must return nil when nothing changed, which length alone cannot
// detect (e.g. [[1]] flattens to [1] with the same length).
func flattenDepthChanged(elems []object.Value, depth int) ([]object.Value, bool) {
	var out []object.Value
	changed := false
	for _, e := range elems {
		if sub, ok := e.(*object.Array); ok && depth != 0 {
			changed = true
			inner, _ := flattenDepthChanged(sub.Elems, depth-1)
			out = append(out, inner...)
		} else {
			out = append(out, e)
		}
	}
	return out, changed
}

// maxFillSize caps how large Array#fill may grow an array. MRI raises when the
// requested size is unreasonable; rejecting here keeps a pathological length
// (e.g. fill(x, 1, fixnum_max)) from attempting a doomed allocation.
const maxFillSize = 1 << 40

// arrayFill implements Array#fill in every MRI form: fill(obj), fill(obj, start),
// fill(obj, start, length), fill(obj, range) and the block variants
// fill { |i| }, fill(start) { }, fill(start, length) { }, fill(range) { }. It
// fills the exclusive index interval [beg, end), growing the array with nils when
// end (or start) is past the current length, and returns self. The block form
// stores block(i) at each index, so a block raising mid-iteration leaves the
// already-filled elements in place (the array is never truncated).
func arrayFill(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
	a := self.(*object.Array)
	vm.checkArrayFrozen(a)
	var item object.Value
	rest := args
	if blk == nil {
		if len(args) < 1 || len(args) > 3 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 1..3)", len(args))
		}
		item = args[0]
		rest = args[1:]
	} else if len(args) > 2 {
		raise("ArgumentError", "wrong number of arguments (given %d, expected 0..2)", len(args))
	}

	alen := len(a.Elems)
	var beg, end int
	if len(rest) == 1 {
		if r, ok := rest[0].(*object.Range); ok {
			// Range form: fill(obj, m..n) / fill(m..n) { }.
			beg, end = vm.fillRangeBounds(r, alen)
			return vm.arrayFillRange(a, beg, end, item, blk)
		}
	}
	// start / length form. A nil (or absent) start is 0; a nil (or absent) length
	// fills through to the current end.
	if len(rest) >= 1 && !object.IsNil(rest[0]) {
		beg = int(vm.toIntCoerce(rest[0]))
		if beg < 0 {
			if beg += alen; beg < 0 {
				beg = 0
			}
		}
	}
	if len(rest) >= 2 && !object.IsNil(rest[1]) {
		length := vm.fillLength(rest[1])
		end = beg + length
	} else {
		end = alen
	}
	return vm.arrayFillRange(a, beg, end, item, blk)
}

// fillLength coerces an Array#fill length argument to an int, raising RangeError
// for a Bignum (an array that large cannot be built) and reusing the standard
// #to_int coercion (with its TypeError) otherwise.
func (vm *VM) fillLength(v object.Value) int {
	if _, ok := v.(*object.Bignum); ok {
		raise("RangeError", "array size too big")
	}
	return int(vm.toIntCoerce(v))
}

// fillRangeBounds turns a Range into the exclusive [beg, end) index interval for
// Array#fill, resolving negative endpoints against alen (MRI's rb_range_beg_len
// with the err flag): a begin still negative after adjustment is a RangeError.
func (vm *VM) fillRangeBounds(r *object.Range, alen int) (beg, end int) {
	// A beginless range (..n) starts at 0; an endless range (m..) runs to the end.
	if object.IsNil(r.Lo) {
		beg = 0
	} else {
		beg = int(vm.toIntCoerce(r.Lo))
		if beg < 0 {
			if beg += alen; beg < 0 {
				raise("RangeError", "%s out of range", r.Inspect())
			}
		}
	}
	if object.IsNil(r.Hi) {
		return beg, alen
	}
	end = int(vm.toIntCoerce(r.Hi))
	if end < 0 {
		end += alen
	}
	if !r.Exclusive {
		end++
	}
	return beg, end
}

// arrayFillRange fills a.Elems[beg:end) with item (block==nil) or block(i),
// growing the array with nils when end exceeds the current length. An empty or
// inverted interval is a no-op. It returns a.
func (vm *VM) arrayFillRange(a *object.Array, beg, end int, item object.Value, blk *Proc) object.Value {
	if end > len(a.Elems) {
		if end > maxFillSize {
			raise("ArgumentError", "array size too big")
		}
		grow := make([]object.Value, end-len(a.Elems))
		for i := range grow {
			grow[i] = object.NilV
		}
		a.Elems = append(a.Elems, grow...)
	}
	for i := beg; i < end; i++ {
		if blk != nil {
			a.Elems[i] = vm.callBlock(blk, []object.Value{object.IntValue(int64(i))})
		} else {
			a.Elems[i] = item
		}
	}
	return a
}

// joinSeparator coerces an Array#join / Array#* separator argument to a String:
// a String is taken directly, otherwise #to_str is tried (and must return a
// String). Anything else — including false — raises TypeError, matching MRI.
func (vm *VM) joinSeparator(v object.Value) *object.String {
	if s, ok := v.(*object.String); ok {
		return s
	}
	if vm.respondsToDynamic(v, "to_str") {
		r := vm.send(v, "to_str", nil, nil)
		if s, ok := r.(*object.String); ok {
			return s
		}
	}
	raise("TypeError", "no implicit conversion of %s into String", vm.classOf(v).name)
	return nil
}

// arrayJoin renders an array as a String, MRI-faithfully: nested arrays (and
// #to_ary-convertible elements) are joined recursively with the same separator,
// elements are coerced via #to_str → #to_ary → #to_s, the running result
// encoding is negotiated across the string pieces (raising EncodingError on an
// incompatible pair), and a self-referential array raises ArgumentError. The
// path set holds the arrays currently being joined so a cycle is detected at any
// depth.
func (vm *VM) arrayJoin(a *object.Array, sep *object.String, path map[*object.Array]bool) *object.String {
	if path[a] {
		raise("ArgumentError", "recursive array join")
	}
	path[a] = true
	defer delete(path, a)

	var buf []byte
	resEnc := ""
	have := false
	negotiate := func(p *object.String) string {
		cur := object.NewStringBytesEnc(buf, resEnc)
		r := vm.encodingCompatible(cur, p)
		e, ok := r.(*encodingObj)
		if !ok {
			raise("EncodingError", "incompatible character encodings: %s and %s",
				cur.EncName(), p.EncName())
		}
		return e.name
	}
	add := func(p *object.String) {
		if !have {
			buf = append(buf, p.Bytes()...)
			resEnc = p.EncName()
			have = true
			return
		}
		if sep != nil {
			resEnc = negotiate(sep)
			buf = append(buf, sep.Bytes()...)
		}
		resEnc = negotiate(p)
		buf = append(buf, p.Bytes()...)
	}
	for _, e := range a.Elems {
		add(vm.joinElement(e, sep, path))
	}
	return object.NewStringBytesEnc(buf, resEnc)
}

// joinElement converts one array element to its String piece for Array#join: a
// String is used as-is; an Array (or Array subclass) is joined recursively;
// otherwise MRI's conversion order applies — #to_str, then #to_ary (whose result
// is joined recursively), then #to_s. An object answering none of these (its
// #to_s undefined) surfaces the NoMethodError from the #to_s dispatch.
func (vm *VM) joinElement(e object.Value, sep *object.String, path map[*object.Array]bool) *object.String {
	if s, ok := e.(*object.String); ok {
		return s
	}
	if arr, ok := asArray(e); ok {
		return vm.arrayJoin(arr, sep, path)
	}
	// Immediates (numbers, symbols, nil, booleans) have no #to_str/#to_ary and use
	// their plain #to_s rendering — take the fast path rather than three dispatches.
	if isJoinImmediate(e) {
		return object.NewString(e.ToS())
	}
	if vm.respondsToDynamic(e, "to_str") {
		if s, ok := vm.send(e, "to_str", nil, nil).(*object.String); ok {
			return s
		}
	}
	if vm.respondsToDynamic(e, "to_ary") {
		if arr, ok := asArray(vm.send(e, "to_ary", nil, nil)); ok {
			return vm.arrayJoin(arr, sep, path)
		}
	}
	if s, ok := vm.send(e, "to_s", nil, nil).(*object.String); ok {
		return s
	}
	return object.NewString(e.ToS())
}

// asArray unwraps a value to an *object.Array, accepting both a plain Array and an
// Array-subclass instance (an RObject whose builtin backing is an Array).
func asArray(v object.Value) (*object.Array, bool) {
	if a, ok := v.(*object.Array); ok {
		return a, true
	}
	if o, ok := v.(*RObject); ok {
		if a, ok := o.builtin.(*object.Array); ok {
			return a, true
		}
	}
	return nil, false
}

// arrayAssoc backs Array#assoc (idx 0) and Array#rassoc (idx 1): it returns the
// first contained array whose element at idx == key. Genuine arrays are used
// as-is (identity preserved); other elements are coerced via #to_ary, and any
// element that is neither an array nor #to_ary-convertible (or is too short) is
// skipped. Returns nil when nothing matches.
func (vm *VM) arrayAssoc(a *object.Array, key object.Value, idx int) object.Value {
	for _, e := range a.Elems {
		arr, ok := asArray(e)
		if !ok {
			if !vm.respondsToDynamic(e, "to_ary") {
				continue
			}
			if arr, ok = asArray(vm.send(e, "to_ary", nil, nil)); !ok {
				continue
			}
		}
		if len(arr.Elems) <= idx {
			continue
		}
		if vm.binaryOp(bytecode.OpEq, arr.Elems[idx], key).Truthy() {
			return arr
		}
	}
	return object.NilV
}

// tryConvert backs Array/Hash/String/Integer.try_convert: an object that is
// already a kind of target is returned unchanged; otherwise, if it responds to
// the named conversion method (#to_ary/#to_hash/#to_str/#to_int), that is called
// and its result must be nil or a kind of target — anything else is a TypeError.
// An object that does not respond to the conversion method yields nil.
func (vm *VM) tryConvert(obj object.Value, target *RClass, meth string) object.Value {
	if classIsA(vm.classOf(obj), target) {
		return obj
	}
	if !vm.respondsToDynamic(obj, meth) {
		return object.NilV
	}
	res := vm.send(obj, meth, nil, nil)
	if object.IsNil(res) {
		return object.NilV
	}
	if classIsA(vm.classOf(res), target) {
		return res
	}
	raise("TypeError", "can't convert %s into %s (%s#%s gives %s)",
		vm.classOf(obj).name, target.name, vm.classOf(obj).name, meth, vm.classOf(res).name)
	return object.NilV
}

// arrayFetchAt resolves one index for Array#fetch/#fetch_values: it coerces idxV
// via #to_int (NUM2LONG), counts a negative index back from the end, and returns
// the element with ok=true when in bounds. orig is the (pre-adjustment) coerced
// index, reused for the out-of-bounds IndexError message so #to_int runs once.
func (vm *VM) arrayFetchAt(a []object.Value, idxV object.Value) (elem object.Value, orig int64, ok bool) {
	orig = vm.repeatLong(idxV)
	idx := orig
	if idx < 0 {
		idx += int64(len(a))
	}
	if idx >= 0 && idx < int64(len(a)) {
		return a[idx], orig, true
	}
	return object.NilV, orig, false
}

// binomialBig returns the binomial coefficient C(n, k) as a big.Int, backing the
// #size of Array#repeated_combination. The sole caller only ever passes k >= 0;
// C(n, 0) is 1 for every n (including the n = -1 that an empty receiver with
// k = 0 produces), and C(n, k) is 0 once k exceeds n.
func binomialBig(n, k int) *big.Int {
	if k == 0 {
		return big.NewInt(1)
	}
	if k > n {
		return big.NewInt(0)
	}
	res := big.NewInt(1)
	for i := 1; i <= k; i++ {
		res.Mul(res, big.NewInt(int64(n-k+i)))
		res.Div(res, big.NewInt(int64(i)))
	}
	return res
}

// isJoinImmediate reports whether a value is a simple immediate whose #to_s
// rendering can be taken directly during Array#join (no #to_str/#to_ary path).
func isJoinImmediate(v object.Value) bool {
	switch v.(type) {
	case object.Integer, object.Float, *object.Bignum, object.Symbol, object.Nil, object.Bool:
		return true
	}
	return false
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

// reflectMethodNames backs #public_methods / #private_methods / #protected_methods:
// it lists the receiver's applicable method names filtered to the target
// visibility want. The candidate set is the receiver's singleton (class) methods
// when self is a class/module — as with #singleton_methods — otherwise its
// instance methods (through its per-object singleton class when it has one). A
// leading `all`/`false` argument (default true) selects inherited methods vs the
// receiver's own. A name whose Method cannot be resolved is treated as public, so
// #public_methods keeps it while the other two drop it.
func (vm *VM) reflectMethodNames(self object.Value, args []object.Value, want visibility) object.Value {
	all := len(args) == 0 || args[0].Truthy()
	var candidates []object.Value
	if _, isClass := self.(*RClass); isClass {
		candidates = vm.singletonMethodNames(self, all)
	} else {
		c := vm.classOf(self)
		if o, ok := self.(*RObject); ok && o.singleton != nil {
			c = o.singleton
		}
		candidates = vm.methodNames(c, all)
	}
	return vm.filterVisibility(self, candidates, func(v visibility) bool { return v == want })
}

// filterVisibility keeps the candidate method names whose effective send-time
// visibility on self satisfies keep, resolving each name against self's class
// methods (when self is a class/module) or its instance-method chain. A name
// whose Method cannot be resolved is treated as public, so it survives a keep
// that accepts public and is dropped by one that does not.
func (vm *VM) filterVisibility(self object.Value, candidates []object.Value, keep func(visibility) bool) object.Value {
	cls, isClass := self.(*RClass)
	var out []object.Value
	for _, n := range candidates {
		name := string(n.(object.Symbol))
		var m *Method
		if isClass {
			m = vm.resolveClassMethod(cls, name)
		} else {
			m = undefAsNil(lookupMethod(vm.dispatchClass(self), name))
		}
		// A candidate whose Method cannot be resolved counts as public (defensive:
		// the candidate sets only carry resolvable, non-undef names, so m is set in
		// practice — the default just keeps the walk nil-safe).
		vis := visPublic
		if m != nil {
			vis = vm.sendVisibilityOf(self, name, m)
		}
		if keep(vis) {
			out = append(out, n)
		}
	}
	return object.NewArrayFromSlice(out)
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
	// collectMeta gathers the class methods a class/module receives from modules
	// mixed into its singleton class via `extend` (and those modules' own
	// prepends/includes) — the enumeration counterpart of lookupSMethod's walk of
	// c.meta.{prepends,includes}.
	var collectTree func(m *RClass)
	collectTree = func(m *RClass) {
		for i := len(m.prepends) - 1; i >= 0; i-- {
			collectTree(m.prepends[i])
		}
		collect(m.methods)
		for i := len(m.includes) - 1; i >= 0; i-- {
			collectTree(m.includes[i])
		}
	}
	collectMeta := func(c *RClass) {
		if c.meta == nil {
			return
		}
		for i := len(c.meta.prepends) - 1; i >= 0; i-- {
			collectTree(c.meta.prepends[i])
		}
		for i := len(c.meta.includes) - 1; i >= 0; i-- {
			collectTree(c.meta.includes[i])
		}
	}
	if c, ok := self.(*RClass); ok {
		collect(c.smethods)
		// Class methods received from `extend`ed modules (c.meta.includes) sit on the
		// singleton class's ancestors, so like the superclass chain below they are
		// inherited: collected only when all is true. singleton_methods(false)
		// returns just the class's own `def self.x` methods.
		if all {
			collectMeta(c)
			for s := c.super; s != nil; s = s.super {
				collect(s.smethods)
				collectMeta(s)
			}
		}
	} else if sc := vm.objSingleton(self); sc != nil {
		collect(sc.methods)
		// Methods gained from `extend`ed modules live on the singleton class's
		// ancestors, so they count as inherited: included only when all is true.
		// singleton_methods(false) returns just the object's own singleton methods.
		if all {
			for i := len(sc.includes) - 1; i >= 0; i-- {
				collectTree(sc.includes[i])
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

// cvarNameArg coerces a class-variable name argument (Symbol or String) to its
// @@-prefixed key, raising TypeError for other types and NameError when the name
// is not a well-formed class variable, matching MRI's Module#class_variable_*.
func cvarNameArg(v object.Value) string {
	var name string
	switch n := v.(type) {
	case object.Symbol:
		name = string(n)
	case *object.String:
		name = n.Str()
	default:
		raise("TypeError", "%s is not a symbol nor a string", v.Inspect())
	}
	if !strings.HasPrefix(name, "@@") || len(name) == 2 {
		raise("NameError", "`%s' is not allowed as a class variable name", name)
	}
	return name
}

// stripRadixPrefix removes a leading 0x/0b/0o/0d (after an optional sign) when it
// matches base, so strconv.ParseInt — which only honours the prefix with base 0 —
// accepts e.g. Integer("0xff", 16).
// popExceptionKwarg splits a trailing keyword Hash off the positional arguments
// of Integer()/Float() and reports whether a conversion failure should raise.
// A literal `exception: false` suppresses the error (the caller returns nil);
// the default, and any other value, keeps the raising behaviour. A trailing
// Hash is only treated as keywords when at least one positional argument
// precedes it, so `Integer({})` still reaches the TypeError path.
func popExceptionKwarg(args []object.Value) ([]object.Value, bool) {
	if len(args) <= 1 {
		return args, true
	}
	h, ok := args[len(args)-1].(*object.Hash)
	if !ok {
		return args, true
	}
	doRaise := true
	var unknown []string
	for _, k := range h.Keys {
		if sym, isSym := k.(object.Symbol); isSym && sym == object.Symbol("exception") {
			v, _ := h.Get(k)
			doRaise = v.Truthy()
			continue
		}
		unknown = append(unknown, k.Inspect())
	}
	// Only exception: is accepted; any other key is MRI's unknown-keyword error
	// ("unknown keyword: :foo" / "unknown keywords: :a, :b").
	if len(unknown) > 0 {
		word := "keyword"
		if len(unknown) > 1 {
			word = "keywords"
		}
		raise("ArgumentError", "unknown %s: %s", word, strings.Join(unknown, ", "))
	}
	return args[:len(args)-1], doRaise
}

func stripRadixPrefix(s string, base int) string {
	sign := ""
	if len(s) > 0 && (s[0] == '-' || s[0] == '+') {
		sign, s = s[:1], s[1:]
	}
	// For an explicit base, strip a matching radix prefix (Go's ParseInt reads a
	// prefix only with base 0). For base 0 Go already understands 0x/0b/0o and a
	// leading-zero octal, but NOT Ruby's 0d decimal prefix — so strip just that,
	// leaving the remainder to parse as decimal under base 0.
	var pfx string
	if base == 0 {
		pfx = "0d"
	} else {
		pfx = map[int]string{16: "0x", 2: "0b", 8: "0o", 10: "0d"}[base]
	}
	if pfx != "" && len(s) >= 2 && strings.ToLower(s[:2]) == pfx {
		s = s[2:]
	}
	return sign + s
}

// intFromString parses a Kernel#Integer string argument in the given base — 0
// auto-detects a 0x/0b/0o/0d prefix (and a leading-zero octal), 2..36 forces the
// radix. It matches MRI: underscores may separate digits, surrounding whitespace
// is ignored, and a syntactically valid literal beyond int64's range yields a
// Bignum rather than failing. ok is false for a malformed value, which the caller
// turns into "invalid value for Integer()".
func intFromString(raw string, base int) (object.Value, bool) {
	cleaned := stripRadixPrefix(strings.TrimSpace(raw), base)
	// Go's ParseInt accepts an underscore digit-separator only with base 0; for an
	// explicit base, apply MRI's rule (a single '_' strictly between two digits)
	// and strip them so "1_1" parses in base 10 as it does base-0.
	if base != 0 {
		s, ok := stripDigitUnderscores(cleaned)
		if !ok {
			return nil, false
		}
		cleaned = s
	}
	n, err := strconv.ParseInt(cleaned, base, 64)
	if err == nil {
		return object.IntValue(n), true
	}
	// Only a genuine overflow (valid digits, out of int64 range) falls back to
	// big.Int; a syntax error stays a failure so "1__1"/"_1" are still rejected.
	// ErrRange guarantees the digits are well formed, so SetString cannot fail on
	// the same cleaned string and base.
	if ne, ok := err.(*strconv.NumError); ok && ne.Err == strconv.ErrRange {
		z, _ := new(big.Int).SetString(cleaned, base)
		return object.NormInt(z), true
	}
	return nil, false
}

// normalizeFloatLiteral rewrites a Kernel#Float string argument into the form
// Go's strconv.ParseFloat accepts while preserving MRI's semantics. A decimal
// literal is returned trimmed (ParseFloat already honours its underscores). A
// hexadecimal float has its digit-separator underscores stripped and, when it
// carries no binary exponent (e.g. "0x10", "0x0.8", "-0x1.8"), gains a "p0" —
// MRI reads a bare hex float as scaled by 2**0, whereas ParseFloat requires the
// p-exponent. ok is false for a hex literal whose underscore is misplaced —
// which the caller must reject, because Go's ParseFloat is more lenient than MRI
// there (it accepts an underscore adjacent to the 0x prefix).
func normalizeFloatLiteral(raw string) (string, bool) {
	s := strings.TrimSpace(raw)
	sign, body := "", s
	if len(body) > 0 && (body[0] == '+' || body[0] == '-') {
		sign, body = body[:1], body[1:]
	}
	if len(body) < 2 || body[0] != '0' || (body[1] != 'x' && body[1] != 'X') {
		return s, true // decimal: ParseFloat handles it (including underscores)
	}
	stripped, ok := hexFloatStripUnderscores(body)
	if !ok {
		return s, false // an illegal underscore MRI rejects
	}
	body = stripped
	if !strings.ContainsAny(body, "pP") {
		body += "p0" // a bare hex float is scaled by 2**0
	}
	return sign + body, true
}

// hexFloatStripUnderscores removes digit-separator underscores from a hex-float
// body ("0x…"), accepting a '_' only strictly between two hexadecimal digits
// (0-9a-fA-F) — so an underscore adjacent to the "0x" prefix, the radix point or
// the p/P exponent marker is rejected (MRI: "0x1_0"→16, "0xa_b"→171, but
// "0x1_p0"/"0x_1" raise). ok is false for an illegal placement, so the caller
// rejects the literal. A body without an underscore is returned unchanged.
func hexFloatStripUnderscores(body string) (string, bool) {
	if !strings.Contains(body, "_") {
		return body, true
	}
	isHex := func(c byte) bool {
		return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
	}
	var b strings.Builder
	for i := 0; i < len(body); i++ {
		if body[i] == '_' {
			if i == 0 || i == len(body)-1 || !isHex(body[i-1]) || !isHex(body[i+1]) {
				return "", false
			}
			continue
		}
		b.WriteByte(body[i])
	}
	return b.String(), true
}

// stripDigitUnderscores removes MRI's digit-separator underscores from a numeric
// literal, accepting a '_' only strictly between two alphanumeric digits (so a
// leading, trailing, doubled or sign/prefix-adjacent underscore is rejected).
// ok is false for a misplaced underscore. A string without any underscore is
// returned unchanged.
func stripDigitUnderscores(s string) (string, bool) {
	if !strings.Contains(s, "_") {
		return s, true
	}
	isDigit := func(c byte) bool {
		return c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '_' {
			if i == 0 || i == len(s)-1 || !isDigit(s[i-1]) || !isDigit(s[i+1]) {
				return "", false
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String(), true
}

// isIntegerVal reports whether v is an Integer or Bignum — the results Kernel#
// Integer's #to_int / #to_i coercion protocol accepts as a successful conversion.
func isIntegerVal(v object.Value) bool {
	switch v.(type) {
	case object.Integer, *object.Bignum:
		return true
	}
	return false
}

// convErrName names v for the "can't convert %s into ..." TypeError Kernel#Integer
// and Kernel#Float raise for an unconvertible argument: MRI prints the keyword for
// true/false and the class name for everything else (Object, Symbol, a user
// class's own name), never the value's inspect form. nil is handled by its own
// switch case in the callers (it is rejected before the coercion protocol), so it
// never reaches here.
func (vm *VM) convErrName(v object.Value) string {
	if b, ok := v.(object.Bool); ok {
		if bool(b) {
			return "true"
		}
		return "false"
	}
	return vm.classOf(v).name
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

// classSingletonIsA reports whether self's singleton (meta) class ancestry
// includes target — the case produced by `extend M`. For a class/module
// receiver it walks the superclass chain (mirroring MRI, where a subclass's
// metaclass super-chains through its parent's metaclass) and checks each
// level's metaclass includes/prepends. For any other object it checks the
// per-object singleton class. Returns false when there is no singleton class.
func classSingletonIsA(vm *VM, self object.Value, target *RClass) bool {
	if c, ok := self.(*RClass); ok {
		for ; c != nil; c = c.super {
			if c.meta == nil {
				continue
			}
			for _, m := range c.meta.includes {
				if classIsA(m, target) {
					return true
				}
			}
			for _, m := range c.meta.prepends {
				if classIsA(m, target) {
					return true
				}
			}
		}
		return false
	}
	if sc := vm.objSingleton(self); sc != nil {
		for _, m := range sc.includes {
			if classIsA(m, target) {
				return true
			}
		}
		for _, m := range sc.prepends {
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
// class + message pair — plus the cause: keyword, which sets the raised
// exception's #cause (nil suppressing the automatic link to $!). With no
// positional argument the exception being handled is re-raised (or a fresh
// RuntimeError); a lone cause: with no exception is an ArgumentError, as MRI.
func nativeRaise(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
	args, causeGiven, causeVal := popCauseKwarg(args)
	switch len(args) {
	case 0:
		if causeGiven {
			raise("ArgumentError", "only cause is given with no arguments")
		}
		// Bare `raise` re-raises the exception currently being handled, else a
		// fresh RuntimeError.
		if !object.IsNil(vm.curExc) {
			panic(vm.excError(vm.captureBacktrace(vm.curExc)))
		}
		panic(vm.excError(vm.captureBacktrace(vm.send(vm.consts["RuntimeError"].(*RClass), "new",
			[]object.Value{object.NewString("unhandled exception")}, nil))))
	default:
		exc := vm.raiseExceptionObject(args)
		vm.applyRaiseCause(exc, causeGiven, causeVal)
		panic(vm.excError(vm.captureBacktrace(exc)))
	}
}

// raiseExceptionObject builds the exception object for a raise from a non-empty
// argument list, matching MRI's Kernel#raise coercion: a String becomes a
// RuntimeError with that message; an exception Class is instantiated (with the
// message when a class+message pair is given); an exception instance is used
// as-is. Anything else is a TypeError. It does NOT capture a backtrace or
// panic — callers do that (Kernel#raise raises here and now; Thread#raise queues
// the object for the target thread, where the backtrace is captured). Shared so
// Thread#raise coerces its arguments exactly like Kernel#raise.
func (vm *VM) raiseExceptionObject(args []object.Value) object.Value {
	// A lone String is shorthand for RuntimeError with that message.
	if len(args) == 1 {
		if s, ok := args[0].(*object.String); ok {
			return vm.send(vm.consts["RuntimeError"].(*RClass), "new", []object.Value{s}, nil)
		}
	}
	// Otherwise the first argument must be an exception class (instantiated, with
	// the message when a second argument is present) or an exception instance
	// (used as-is). Anything else — a String with extra arguments, a non-exception
	// class or object, true/false/nil — is a TypeError, matching MRI.
	switch a := args[0].(type) {
	case *RClass:
		if classIsA(a, vm.consts["Exception"].(*RClass)) {
			var ctorArgs []object.Value
			if len(args) >= 2 {
				ctorArgs = []object.Value{args[1]}
			}
			return vm.send(a, "new", ctorArgs, nil)
		}
	case *RObject:
		if classIsA(vm.classOf(a), vm.consts["Exception"].(*RClass)) {
			return a
		}
	}
	raise("TypeError", "exception class/object expected")
	return object.NilV
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
	setIvar(exc, backtraceIvar, object.NewArrayFromSlice(frames))
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
		return object.NewArray(a)
	case *object.Array:
		for _, e := range a.Elems {
			if _, ok := e.(*object.String); !ok {
				raise("TypeError", "backtrace must be an Array of String or an Array of Thread::Backtrace::Location")
			}
		}
		return object.NewArrayFromSlice(append([]object.Value(nil), a.Elems...))
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

// parseFullMessageOpts reads the highlight:/order: keywords of full_message /
// detailed_message from a trailing options Hash. highlight defaults to false (no
// TTY under the test/CLI harness) and order to "top". Unknown keywords are
// ignored, matching MRI's tolerance of library-specific detailed_message opts.
func parseFullMessageOpts(args []object.Value) (highlight bool, order string) {
	order = "top"
	if len(args) == 0 {
		return highlight, order
	}
	h, ok := args[len(args)-1].(*object.Hash)
	if !ok {
		return highlight, order
	}
	if v, ok := h.Get(object.Symbol("highlight")); ok {
		highlight = v.Truthy()
	}
	if v, ok := h.Get(object.Symbol("order")); ok {
		if s, ok := v.(object.Symbol); ok {
			order = string(s)
		}
	}
	return highlight, order
}

// exceptionDetailedMessage renders "<message> (<ClassName>)", the body MRI's
// Exception#detailed_message produces and that full_message embeds. With
// highlight it wraps the message bold and the class name bold-underline, matching
// MRI's terminal form.
func (vm *VM) exceptionDetailedMessage(self object.Value, highlight bool) string {
	msg := vm.exceptionMessageText(self)
	cls := vm.classOf(self).name
	if highlight {
		return "\x1b[1m" + msg + " (\x1b[1;4m" + cls + "\x1b[m\x1b[1m)\x1b[m"
	}
	return msg + " (" + cls + ")"
}

// exceptionFullMessage renders the MRI-shaped multi-line report. order: :top (the
// default) leads with the raise-site frame and the detailed message, then a
// "\tfrom <frame>" line per remaining frame:
//
//	<file>:<line>:in '<label>': <message> (<ClassName>)
//		from <frame>
//
// order: :bottom prints a "Traceback (most recent call last):" header, the outer
// frames numbered outermost-first, and the raise-site + message last. With no
// backtrace it degrades to just the detailed message. Line numbers are 0 (the
// parser carries no positions) and the source-snippet/caret lines MRI prints are
// omitted, but the file+label chain matches.
func (vm *VM) exceptionFullMessage(self object.Value, highlight bool, order string) string {
	detailed := vm.exceptionDetailedMessage(self, highlight)
	bt, ok := getIvar(self, backtraceIvar).(*object.Array)
	if !ok || len(bt.Elems) == 0 {
		return detailed
	}
	frames := bt.Elems
	var b strings.Builder
	if order == "bottom" {
		b.WriteString("Traceback (most recent call last):\n")
		for i := len(frames) - 1; i >= 1; i-- {
			b.WriteString("\t")
			b.WriteString(strconv.Itoa(i))
			b.WriteString(": from ")
			b.WriteString(frames[i].ToS())
			b.WriteString("\n")
		}
		b.WriteString(frames[0].ToS())
		b.WriteString(": ")
		b.WriteString(detailed)
		b.WriteString("\n")
		return b.String()
	}
	b.WriteString(frames[0].ToS())
	b.WriteString(": ")
	b.WriteString(detailed)
	b.WriteString("\n")
	for _, f := range frames[1:] {
		b.WriteString("\tfrom ")
		b.WriteString(f.ToS())
		b.WriteString("\n")
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

// padString implements ljust/rjust/center ('l'/'r'/'c'): pad s with the pad
// string (default " ") to a rune width. Extra padding for center goes right.
func (vm *VM) padString(s string, args []object.Value, side byte) string {
	width := int(vm.repeatLong(args[0])) // #to_int coercion of the width
	pad := " "
	if len(args) > 1 {
		pad = vm.coerceFormatString(args[1]) // #to_str coercion of the pad string
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

// stringLineSegs splits the receiver into line segments for String#lines /
// #each_line, honouring the optional record separator (default "\n"; nil = the
// whole string; "" = paragraph mode) and a chomp: keyword that strips the
// separator from each segment. Every segment keeps the receiver's encoding.
func (vm *VM) stringLineSegs(self object.Value, args []object.Value) []object.Value {
	chomp := false
	pos := args
	if h := trailingKwHash(args); h != nil {
		if v, ok := h.Get(object.SymVal("chomp")); ok {
			chomp = v.Truthy()
		}
		pos = args[:len(args)-1]
	}
	str := self.(*object.String)
	s := str.Str()
	enc := str.Enc
	var segs []string
	switch {
	case s == "":
		// no segments
	case len(pos) > 0 && object.IsNil(pos[0]):
		// A nil separator yields the whole string as a single line.
		whole := s
		if chomp {
			whole = chompSeg(whole, "\n")
		}
		segs = []string{whole}
	default:
		sep := "\n"
		if len(pos) > 0 {
			sep = vm.coerceFormatString(pos[0])
		}
		if sep == "" {
			segs = splitParagraphs(s, chomp)
		} else {
			segs = splitLinesSep(s, sep, chomp)
		}
	}
	out := make([]object.Value, len(segs))
	for i, seg := range segs {
		out[i] = object.NewStringViewEnc(seg, enc)
	}
	return out
}

// splitLinesSep splits s on the record separator sep (non-empty), keeping the
// separator attached to each line unless chomp strips it; a trailing chunk with
// no separator is emitted as-is.
func splitLinesSep(s, sep string, chomp bool) []string {
	var out []string
	start := 0
	for {
		i := strings.Index(s[start:], sep)
		if i < 0 {
			break
		}
		end := start + i + len(sep)
		seg := s[start:end]
		if chomp {
			seg = chompSeg(seg, sep)
		}
		out = append(out, seg)
		start = end
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

// termLen reports the length of the line terminator at s[i] — 2 for "\r\n", 1 for
// "\n", 0 for anything else — so paragraph mode can treat CRLF and LF uniformly.
func termLen(s string, i int) int {
	if i >= len(s) {
		return 0
	}
	if s[i] == '\n' {
		return 1
	}
	if s[i] == '\r' && i+1 < len(s) && s[i+1] == '\n' {
		return 2
	}
	return 0
}

// splitParagraphs implements paragraph mode (a "" separator): each paragraph runs
// up to and including the first blank line — two consecutive line terminators
// (each "\n" or "\r\n") — keeping exactly those two; any further terminators are
// then skipped. Leading blank lines are NOT dropped: they form their own
// paragraph, matching MRI. chomp strips trailing newlines from each paragraph.
func splitParagraphs(s string, chomp bool) []string {
	var out []string
	i := 0
	for i < len(s) {
		start := i
		segEnd := len(s)
		next := len(s)
		for i < len(s) {
			t := termLen(s, i)
			if t == 0 {
				i++
				continue
			}
			if t2 := termLen(s, i+t); t2 > 0 { // a blank line ends the paragraph
				segEnd = i + t + t2
				next = segEnd
				for n := termLen(s, next); n > 0; n = termLen(s, next) {
					next += n
				}
				break
			}
			i += t
		}
		seg := s[start:segEnd]
		if chomp {
			seg = strings.TrimRight(seg, "\r\n")
		}
		out = append(out, seg)
		i = next
	}
	return out
}

// chompSeg removes a trailing record separator from seg. For the default "\n" it
// strips a trailing "\r\n" or "\n" (as String#lines(chomp: true) does — a lone
// "\r" is kept); otherwise it strips exactly sep.
func chompSeg(seg, sep string) string {
	if sep == "\n" {
		if strings.HasSuffix(seg, "\r\n") {
			return seg[:len(seg)-2]
		}
		return strings.TrimSuffix(seg, "\n")
	}
	return strings.TrimSuffix(seg, sep)
}

// defineAttrs installs reader and/or writer accessors on cls for each named
// attribute (the symbols/strings passed to attr_reader/writer/accessor).
// defineAttrs installs the reader and/or writer accessors for each name on cls,
// giving them the class body's current default visibility (so attr_* written
// under `private` produce private accessors). Each name is coerced through
// #to_str (TypeError otherwise), matching MRI. It returns the created method
// names as Symbols — attr_reader/#attr_writer/#attr_accessor's Ruby-3 result.
func (vm *VM) defineAttrs(cls *RClass, names []object.Value, reader, writer bool) []object.Value {
	if isFrozen(cls) {
		vm.raiseFrozen(cls)
	}
	vis := cls.defaultVis
	var created []object.Value
	for _, n := range names {
		base := vm.methodNameArg(n)
		ivar := "@" + base
		if reader {
			cls.define(base, func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
				return getIvar(self, ivar)
			})
			cls.methods[base].vis = vis
			cls.methods[base].attrKind = attrReaderMethod
			created = append(created, object.Symbol(base))
		}
		if writer {
			w := base + "="
			cls.define(w, func(vm *VM, self object.Value, a []object.Value, _ *Proc) object.Value {
				if isFrozen(self) {
					vm.raiseFrozen(self)
				}
				setIvar(self, ivar, a[0])
				return a[0]
			})
			cls.methods[w].vis = vis
			cls.methods[w].attrKind = attrWriterMethod
			created = append(created, object.Symbol(w))
		}
	}
	return created
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
		return object.NewArrayFromSlice(elems)
	case *object.Hash:
		h := object.NewHash()
		// A compare_by_identity hash keeps its identity comparison across dup/clone
		// (MRI persists the flag), so switch the copy into identity mode before
		// inserting so keys are stored by object identity like the original.
		h.Identity = x.Identity
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
		dup := &RObject{class: x.class, ivars: ivars}
		// A Struct instance carries its members in structVals (not ivars), so the
		// copy must clone that slice too or the duplicate would report all-nil
		// members (and Struct#hash/#== would diverge from the original).
		if x.structVals != nil {
			dup.structVals = make([]object.Value, len(x.structVals))
			copy(dup.structVals, x.structVals)
		}
		return dup
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
	case *object.Array:
		return x.Frozen
	case *object.Hash:
		return x.Frozen
	case *Regexp:
		return x.frozen
	case *RObject:
		return x.frozen
	case *RClass:
		return x.frozen
	}
	return false
}

// checkArrayFrozen raises FrozenError (with the MRI message and @receiver) when a
// is frozen, for the in-place Array mutators to call before modifying.
func (vm *VM) checkArrayFrozen(a *object.Array) {
	if a.Frozen {
		vm.raiseFrozen(a)
	}
}

// checkHashFrozen is the Hash counterpart of checkArrayFrozen.
func (vm *VM) checkHashFrozen(h *object.Hash) {
	if h.Frozen {
		vm.raiseFrozen(h)
	}
}

// raiseFrozen raises FrozenError for a modification attempt on a frozen object,
// mirroring MRI's "can't modify frozen <class>: <inspect>" message.
func (vm *VM) raiseFrozen(self object.Value) {
	vm.raiseWithIvars("FrozenError",
		"can't modify frozen "+vm.classOf(self).name+": "+vm.inspectStr(self),
		map[string]object.Value{"@receiver": self})
}

// arrayKeepIf mutates a in place, keeping the elements for which the block's
// truthiness equals keep (select!/reject!). It returns the array, or nil when
// nothing was removed (Ruby's "no change" signal).
// arrayReduce folds a over blk (or a symbol operator), mirroring the prelude's
// Enumerable#reduce for every argument form: reduce { |a,b| }, reduce(init) { },
// reduce(sym), reduce(init, sym) and the two-argument form whose operator is not
// a Symbol (which defers to acc.send(op, x) so its coercion error matches). A
// bare-block fold with no block raises LocalJumpError only when it actually
// reaches a yield step (so [].reduce is nil and [x].reduce is x), exactly as the
// interpreted version did.
func arrayReduce(vm *VM, a *object.Array, args []object.Value, blk *Proc) object.Value {
	var symVal object.Value
	var symName string
	hasSym, symKnown, hasInit := false, false, false
	var init object.Value = object.NilV
	switch {
	case len(args) == 2:
		init, symVal = args[0], args[1]
		hasSym, hasInit = true, true
	case len(args) == 1:
		if s, ok := args[0].(object.Symbol); ok {
			symVal, symName, hasSym, symKnown = s, string(s), true, true
		} else {
			init, hasInit = args[0], true
		}
	}
	if hasSym && !symKnown {
		if s, ok := symVal.(object.Symbol); ok {
			symName, symKnown = string(s), true
		}
	}
	acc := init
	started := hasInit
	for _, x := range a.Elems {
		switch {
		case !started:
			acc, started = x, true
		case hasSym && symKnown:
			acc = vm.send(acc, symName, []object.Value{x}, nil)
		case hasSym:
			// Two-argument form with a non-Symbol operator: mirror acc.send(op, x).
			acc = vm.send(acc, "send", []object.Value{symVal, x}, nil)
		default:
			if blk == nil {
				raise("LocalJumpError", "no block given (yield)")
			}
			acc = vm.callBlock(blk, []object.Value{acc, x})
		}
	}
	return acc
}

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
func spaceshipNumeric(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
	other := args[0]
	// Compare two integers (Integer or Bignum) exactly; only fall back to float
	// when one side is a Float (where the precision loss is intrinsic).
	if ai, ok := self.(object.Integer); ok {
		if bi, ok := args[0].(object.Integer); ok {
			return object.IntValue(int64(cmpInt64(int64(ai), int64(bi))))
		}
	}
	if ab, ok := object.BigOf(self); ok {
		if bb, ok := object.BigOf(other); ok {
			return object.IntValue(int64(ab.Cmp(bb)))
		}
		// Integer <=> Float: compare exactly (no precision loss) so that a Bignum
		// larger than any representable double still orders correctly against the
		// rounded float value — MRI does the same via a rational comparison.
		if bf, ok := other.(object.Float); ok {
			if c, ok := cmpBigFloat(ab, float64(bf)); ok {
				return object.IntValue(int64(c))
			}
			return object.NilV // NaN
		}
	}
	// Float <=> Integer (self is Float, other is a big/small Integer): exact too.
	if af, ok := self.(object.Float); ok {
		if bb, ok := object.BigOf(other); ok {
			if c, ok := cmpBigFloat(bb, float64(af)); ok {
				return object.IntValue(int64(-c))
			}
			return object.NilV
		}
	}
	a, _ := toFloat(self)
	if b, ok := toFloat(other); ok {
		return object.IntValue(int64(cmpFloat(a, b)))
	}
	// Non-numeric argument: MRI runs the numeric coercion protocol — other.coerce(self)
	// — and re-dispatches <=> on the returned pair. Any exception raised by #coerce
	// propagates; a missing #coerce or a non-Array result yields nil (not an error).
	if vm != nil && vm.respondsToDynamic(other, "coerce") {
		pair := vm.send(other, "coerce", []object.Value{self}, nil)
		if arr, ok := pair.(*object.Array); ok && len(arr.Elems) == 2 {
			return vm.send(arr.Elems[0], "<=>", []object.Value{arr.Elems[1]}, nil)
		}
	}
	return object.NilV
}

// cmpBigFloat compares an exact integer a against the exact rational value of the
// float f, returning (-1, 0, or 1). ok is false only when f is NaN. It converts f
// to a big.Rat (which represents any finite double exactly) so that no precision is
// lost — the whole point being that a Bignum outside the double range still orders
// correctly against the rounded float.
func cmpBigFloat(a *big.Int, f float64) (int, bool) {
	if math.IsNaN(f) {
		return 0, false
	}
	if math.IsInf(f, 1) {
		return -1, true
	}
	if math.IsInf(f, -1) {
		return 1, true
	}
	ar := new(big.Rat).SetInt(a)
	fr := new(big.Rat).SetFloat64(f) // exact for any finite double
	return ar.Cmp(fr), true
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

// rangeEmpty reports whether r contains no elements: its begin compares greater
// than its end (or equal, when the end is excluded). A beginless or endless range
// is never empty in this sense.
func rangeEmpty(vm *VM, r *object.Range) bool {
	if object.IsNil(r.Lo) || object.IsNil(r.Hi) {
		return false
	}
	c := vm.spaceship(r.Lo, r.Hi)
	return c > 0 || (r.Exclusive && c == 0)
}

// rangeExtremeByBlock returns the minimum (isMin) or maximum element of r using a
// comparison block, iterating the range as Enumerable#min/#max do.
func (vm *VM) rangeExtremeByBlock(r *object.Range, blk *Proc, isMin bool) object.Value {
	elems := rangeElems(r)
	if len(elems) == 0 {
		return object.NilV
	}
	best := elems[0]
	for _, e := range elems[1:] {
		c, _ := vm.callBlock(blk, []object.Value{e, best}).(object.Integer)
		if (isMin && c < 0) || (!isMin && c > 0) {
			best = e
		}
	}
	return best
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
		out = append(out, object.IntValue(i))
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
			vm.callBlock(blk, []object.Value{object.IntValue(i)})
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

// numericStepEndless walks an endless numeric Range#step (m.. with no upper
// bound): it yields lo, lo+step, lo+2*step, … forever, and the caller's block is
// expected to break out. Integer begin and Integer step yield Integers; any
// Float participant yields Floats computed as lo+i*step (not accumulated, so
// float error does not drift). A non-numeric begin raises the same TypeError as
// the bounded path.
func (vm *VM) numericStepEndless(blk *Proc, loV, stepV object.Value) {
	li, loInt := loV.(object.Integer)
	si, stepInt := stepV.(object.Integer)
	if loInt && stepInt {
		step := int64(si)
		if step == 0 {
			raise("ArgumentError", "step can't be 0")
		}
		for i := int64(li); ; i += step {
			vm.callBlock(blk, []object.Value{object.IntValue(i)})
		}
	}
	lo, ok1 := toFloat(loV)
	step, ok2 := toFloat(stepV)
	if !ok1 || !ok2 {
		raise("TypeError", "can't iterate from %s", loV.Inspect())
	}
	if step == 0 {
		raise("ArgumentError", "step can't be 0")
	}
	for i := 0; ; i++ {
		vm.callBlock(blk, []object.Value{object.Float(lo + float64(i)*step)})
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

// stepSize is how many values a step sequence yields, worked out rather than
// counted.
//
// Enumerator#size falls back to enumerating whatever cannot tell it its own
// length, which is right for a sequence that ends and fatal for one that does
// not: 1.step(Float::INFINITY).size allocated 30.9 GB and took a CI runner with
// it. MRI computes this and so does this.
//
// A step of zero raises, as MRI does — asking the size is enough to get the
// error, not only running the sequence. An argument that is not a number has no
// size to give and reports the nil MRI reports for an unknown one.
func stepSize(from, limit, step object.Value) object.Value {
	f, okF := toFloat(from)
	l, okL := toFloat(limit)
	s, okS := toFloat(step)
	if !okF || !okL || !okS {
		return object.NilV
	}
	if s == 0 {
		raise("ArgumentError", "step can't be 0")
	}
	if math.IsInf(l, 0) {
		// Travelling towards an end that never comes yields for ever; away from
		// it yields nothing at all.
		if (s > 0) == (l > 0) {
			return object.Float(math.Inf(1))
		}
		return object.IntValue(0)
	}
	n := math.Floor((l-f)/s) + 1
	if math.IsNaN(n) || n < 0 {
		return object.IntValue(0)
	}
	return object.IntValue(int64(n))
}

// rangeStepSize is how many values a Range#step sequence yields.
//
// Only a numeric range can say: a String range steps by #succ and MRI reports
// nil for its size rather than counting the walk, which is also the honest
// answer for an endless one.
func rangeStepSize(r *object.Range, step object.Value) object.Value {
	if object.IsNil(r.Hi) {
		if s, ok := toFloat(step); ok && s == 0 {
			raise("ArgumentError", "step can't be 0")
		}
		return object.Float(math.Inf(1)) // endless, so endlessly many
	}
	if _, ok := toFloat(r.Lo); !ok {
		return object.NilV
	}
	if _, ok := toFloat(r.Hi); !ok {
		return object.NilV
	}
	hi := r.Hi
	if r.Exclusive {
		// An excluded end is one short, which stepSize counts by moving the
		// limit a whole step back rather than by subtracting one from a count
		// that may not be integral.
		if h, ok := toFloat(hi); ok {
			if s, ok := toFloat(step); ok && s != 0 {
				hi = object.Float(math.Nextafter(h, h-s))
			}
		}
	}
	return stepSize(r.Lo, hi, step)
}
