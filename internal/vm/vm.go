// Package vm interprets bytecode.
//
// Phase 1 adds the live object model (plan §5): values dispatch through mutable
// per-class method tables (the project's objc_msgSend), so monkey-patching,
// define_method, method_missing, classes, instances and ivars all work. The
// arithmetic/comparison opcodes remain a fast path; method calls go through
// OpSend → send().
//
// Runtime errors are still fatal in Phase 1 (rescue arrives in Phase 3) and
// travel as panic(RubyError) recovered at the Run boundary.
package vm

import (
	"fmt"
	"io"
	"math"
	"math/big"
	"path/filepath"
	"strings"
	"sync"

	activejob "github.com/go-ruby-activejob/activejob"
	activestorage "github.com/go-ruby-activestorage/activestorage"
	inflector "github.com/go-ruby-activesupport/activesupport/inflector"
	async "github.com/go-ruby-async/async"
	i18n "github.com/go-ruby-i18n/i18n"
	money "github.com/go-ruby-money/money"
	sinatra "github.com/go-ruby-sinatra/sinatra"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// RubyError is a runtime error surfaced to the caller.
type RubyError struct {
	Class   string
	Message string
	Obj     object.Value   // the Ruby exception object, when raised from Ruby (else nil)
	Frames  []object.Value // backtrace (Array of String) captured at the Run boundary
}

func (e RubyError) Error() string { return e.Class + ": " + e.Message }

// Backtrace returns the exception's backtrace as plain strings, innermost-first,
// for a host (the rbgo CLI) to render an uncaught exception MRI-style. It is the
// stack captured when the exception propagated out of Run.
func (e RubyError) Backtrace() []string {
	out := make([]string, 0, len(e.Frames))
	for _, f := range e.Frames {
		out = append(out, f.ToS())
	}
	return out
}

// raise never returns; the object.Value result lets callers write
// `return raise(...)` without an unreachable trailing return.
func raise(class, format string, args ...any) object.Value {
	panic(RubyError{Class: class, Message: fmt.Sprintf(format, args...)})
}

// breakSignal unwinds a block `break` to the method the block was passed to.
// owner identifies the executing block so the matching call site catches it
// (and a break through a Ruby-level iterator like Enumerable#map lands on the
// outer call, not the inner each).
type breakSignal struct {
	owner *Proc
	value object.Value
}

// throwSignal unwinds a Kernel#throw to the matching Kernel#catch (matched by tag
// identity). An unmatched throw surfaces as an UncaughtThrowError at Run.
type throwSignal struct {
	tag   object.Value
	value object.Value
}

// returnTarget identifies a method (or top-level) activation that a non-local
// `return` from a block should unwind to. Each such activation allocates a fresh
// one; block frames inherit their creator's, so a `return` inside a block reaches
// the method the block was written in. It carries a field so it is not zero-sized
// — every &returnTarget{} must be a distinct pointer (Go gives all zero-sized
// allocations the same address), which is how frames are told apart.
type returnTarget struct{ _ bool }

// returnSignal unwinds a non-local `return` (an explicit `return` inside a block)
// to the activation identified by target. A signal whose target has no live frame
// (the home method already returned) surfaces as a LocalJumpError.
type returnSignal struct {
	target *returnTarget
	value  object.Value
}

// sendCatchBreak performs a send carrying a literal block, turning a `break`
// raised by that block into the call's result.
func (vm *VM) sendCatchBreak(recv object.Value, name string, args []object.Value, blk *Proc) (result object.Value) {
	defer func() {
		if r := recover(); r != nil {
			if sig, ok := r.(breakSignal); ok && sig.owner == blk {
				result = sig.value
				return
			}
			panic(r)
		}
	}()
	return vm.send(recv, name, args, blk)
}

// popToEnsure pops handler frames from the top until it finds an ensure handler,
// which it removes and returns. Plain rescue handlers above it do not apply to a
// non-exception unwind and are discarded. found is false when no ensure handler
// remains, leaving the slice empty.
func popToEnsure(handlers *[]handlerFrame) (handlerFrame, bool) {
	h := *handlers
	for len(h) > 0 {
		top := h[len(h)-1]
		h = h[:len(h)-1]
		if top.isEnsure {
			*handlers = h
			return top, true
		}
	}
	*handlers = h
	return handlerFrame{}, false
}

// handlerFrame is an active begin/rescue/ensure handler: where to resume and the
// operand-stack depth to restore. isEnsure marks a handler whose body must run on
// EVERY unwind (exception, non-local return, break, throw), not only on a rescued
// exception — so those signals run pending ensure blocks before continuing.
type handlerFrame struct {
	pc       int
	sp       int
	isEnsure bool
}

// exceptionObject returns the Ruby exception object for a RubyError, building
// one from the class name + message when the error did not originate from a
// Ruby `raise` (internal raises carry no object).
func (vm *VM) exceptionObject(e RubyError) object.Value {
	if !object.IsNil(e.Obj) {
		return vm.captureBacktrace(e.Obj)
	}
	cls, ok := vm.consts[e.Class].(*RClass)
	if !ok {
		cls = vm.consts["StandardError"].(*RClass)
	}
	obj := &RObject{class: cls, ivars: map[string]object.Value{"@message": object.NewString(e.Message)}}
	return vm.captureBacktrace(obj)
}

// uncaughtBacktrace returns the backtrace strings for an exception escaping Run.
// It prefers the backtrace stamped on the exception object at its raise site
// (preserved across re-raises); when none is stored — an internal raise whose
// object is built only now — it snapshots the still-intact live frame stack.
func (vm *VM) uncaughtBacktrace(e RubyError) []object.Value {
	if !object.IsNil(e.Obj) {
		if bt, ok := getIvar(e.Obj, backtraceIvar).(*object.Array); ok {
			return bt.Elems
		}
	}
	return vm.backtraceFrames(0)
}

// VM holds I/O, the top-level self, the constant table and the base classes.
type VM struct {
	out    io.Writer
	errOut io.Writer // $stderr/STDERR sink; defaults to out (no separate stream)
	main   object.Value
	consts map[string]object.Value // top-level constants (classes live here)

	// mainArmed gates the level-2 AOT top level (compiledMainFn): it is set once,
	// after the prelude and built-in registrations have loaded, so the *next*
	// top-level Run — the user program — dispatches to the compiled aotMain, while
	// the prelude's own Run (and any nested require/eval inside the program) keeps
	// interpreting. See runTop.
	mainArmed bool

	arAdapter *arSQLiteAdapter // the ActiveRecord::Base connection (require "active_record"), backed by go-ruby-sqlite3; nil until establish_connection

	// sidekiqRedisURL / resqueRedisURL are the go-redis connection URLs the job
	// bindings (require "sidekiq" / "resque") dial. Each is empty until set via
	// the module's redis= config (or a configure_client/server block); the getter
	// then falls back to ENV["REDIS_URL"] and finally the local default. A fresh
	// go-redis client is built and closed per operation (see withSidekiqRedis /
	// withResqueRedis) so no connection or goroutine leaks across the single-
	// threaded VM.
	sidekiqRedisURL string
	resqueRedisURL  string

	// arModels caches the *ActiveRecordModel lazily built for each
	// `class … < ActiveRecord::Base` subclass, and arTableNames records an
	// explicit `self.table_name = "…"` override per class (otherwise the table
	// name is inferred from the class name via activerecord.Tableize).
	arModels     map[*RClass]*ActiveRecordModel
	arTableNames map[*RClass]string

	cBasicObject, cObject, cModule, cClass *RClass
	cInteger, cFloat, cString, cSymbol     *RClass
	cComplex, cRational                    *RClass
	cNDArray, cImage                       *RClass
	cSet                                   *RClass
	cPStore                                *RClass // the PStore class (require "pstore"), backed by go-ruby-pstore
	cPStoreError                           *RClass // PStore::Error
	cPrettyPrint                           *RClass // the PrettyPrint class (require "prettyprint")
	cMatrix                                *RClass // the Matrix class (require "matrix")
	cVector                                *RClass // the Vector class (require "matrix")
	cExceptionForMatrix                    *RClass // the ExceptionForMatrix module (require "matrix")
	cErrDimensionMismatch                  *RClass // ExceptionForMatrix::ErrDimensionMismatch
	cErrNotRegular                         *RClass // ExceptionForMatrix::ErrNotRegular
	cErrOperationNotDefined                *RClass // ExceptionForMatrix::ErrOperationNotDefined
	cGetoptLong                            *RClass // the GetoptLong class (require "getoptlong"), backed by go-ruby-getoptlong
	cIPAddr                                *RClass // the IPAddr class (require "ipaddr"), backed by go-ruby-ipaddr
	cIPAddrError                           *RClass // IPAddr::Error
	cIPAddrInvalidAddressError             *RClass // IPAddr::InvalidAddressError
	cIPAddrInvalidPrefixError              *RClass // IPAddr::InvalidPrefixError
	cIPAddrAddressFamilyError              *RClass // IPAddr::AddressFamilyError
	cCMath                                 *RClass // the CMath module (require "cmath")
	cDidYouMean                            *RClass // the DidYouMean module (require "did_you_mean")
	cSpellChecker                          *RClass // DidYouMean::SpellChecker, backed by go-ruby-did-you-mean
	cTime                                  *RClass
	cFileStat                              *RClass
	cBigDecimal                            *RClass
	cBenchmarkTms                          *RClass // Benchmark::Tms (require "benchmark"), backed by go-ruby-benchmark
	cBenchmarkReport                       *RClass // Benchmark::Report (require "benchmark")
	cBenchmarkJob                          *RClass // Benchmark::Job (require "benchmark")
	cDate                                  *RClass
	cDateTime                              *RClass
	cBag                                   *RClass
	cStringScanner                         *RClass
	moneyBank                              *money.VariableExchange // the process-wide default exchange bank for Money (require "money")
	i18nInst                               *i18n.I18n              // the process-wide I18n instance (require "i18n"), backed by go-ruby-i18n
	asInflections                          *inflector.Inflections  // the ActiveSupport::Inflector inflection ruleset (require "active_support"), backed by go-ruby-activesupport; a per-VM clone of the gem's English defaults
	otelProvider                           *OTelTracerProvider     // the process-wide OpenTelemetry.tracer_provider (require "opentelemetry"), backed by go-ruby-opentelemetry
	cOptionParser                          *RClass
	cURI                                   *RClass                            // the URI module (require "uri"), backed by go-ruby-uri
	cURIGeneric                            *RClass                            // URI::Generic, the base URI class wrapping a *uri.URI
	cCSV                                   *RClass                            // the CSV class (require "csv"), backed by go-ruby-csv
	cCSVRow                                *RClass                            // CSV::Row, wrapping a *csv.Row
	cCSVTable                              *RClass                            // CSV::Table, wrapping a *csv.Table
	cLogger                                *RClass                            // the Logger class (require "logger"), backed by go-ruby-logger
	cLoggerFormatter                       *RClass                            // Logger::Formatter, wrapping a *Logger wrapper's formatter
	cLoggerSeverity                        *RClass                            // Logger::Severity, the severity-constant module
	cREXML                                 *RClass                            // the REXML module (require "rexml/document"), backed by go-ruby-rexml
	cREXMLDocument                         *RClass                            // REXML::Document, wrapping a *rexml.Document
	cREXMLElement                          *RClass                            // REXML::Element, wrapping a *rexml.Element
	cREXMLElements                         *RClass                            // REXML::Elements, the child-navigation proxy
	cREXMLAttributes                       *RClass                            // REXML::Attributes, wrapping a *rexml.Attributes
	cREXMLText                             *RClass                            // REXML::Text, wrapping a *rexml.Text
	cREXMLComment                          *RClass                            // REXML::Comment, wrapping a *rexml.Comment
	cREXMLCData                            *RClass                            // REXML::CData, wrapping a *rexml.CData
	cREXMLInstruction                      *RClass                            // REXML::Instruction, wrapping a *rexml.Instruction
	cREXMLDocType                          *RClass                            // REXML::DocType, wrapping a *rexml.DocType
	cREXMLPretty                           *RClass                            // REXML::Formatters::Pretty serialiser
	cREXMLXPath                            *RClass                            // REXML::XPath module
	cREXMLParseException                   *RClass                            // REXML::ParseException < StandardError
	cSinatraBase                           *RClass                            // Sinatra::Base (require "sinatra/base"), backed by go-ruby-sinatra
	cSinatraCtx                            *RClass                            // Sinatra::Base::Context, the self a route/filter block runs against
	cSinatraSettings                       *RClass                            // Sinatra::Base::Settings, the handler's `settings` view
	sinatraDefs                            map[*RClass]*sinatraDef            // per-Sinatra::Base-subclass route/filter/handler declarations
	sinatraCtxCache                        map[*sinatra.Context]*SinatraCtx   // per-request handler self, shared across before/route/after so @ivars persist
	sinatraSession                         *sinatraSessionState               // per-dispatch cookie session (enable :sessions); the `session` helper returns its live Hash, saved back into a Set-Cookie
	sinatraDefaultSecret                   []byte                             // per-VM fallback session-signing key when no session_secret is set (like MRI Sinatra's random default), generated once
	cRodaBase                              *RClass                            // Roda (require "roda"), the routing-tree app superclass, backed by go-ruby-roda
	cRodaRequest                           *RClass                            // Roda::RodaRequest, the self a route/matcher block runs against
	cRodaResponse                          *RClass                            // Roda::RodaResponse, the mutable response a route block writes into
	rodaRoutes                             map[*RClass]*Proc                  // per-Roda-subclass top-level route block (route do |r| … end)
	cAsyncTask                             *RClass                            // Async::Task, one node of the structured-concurrency tree, backed by go-ruby-async
	curAsyncTask                           *async.Task                        // the task whose Ruby body is currently running (backs Async::Task.current and the caller passed to blocking async ops)
	asyncTasks                             map[*async.Task]*AsyncTask         // wrapper cache so one *async.Task always maps to one Async::Task object (Ruby #equal? identity), cleared when the root reactor finishes
	wardenStrategies                       map[string]*RClass                 // Warden::Strategies.add(name){…} registry: label -> anon subclass of Warden::Strategies::Base
	omniAuthStrategies                     map[string]*RClass                 // OmniAuth provider registry: name -> strategy class (from OmniAuth::Strategies.add or provider Class)
	omniAuthProviderOpts                   map[string]map[string]any          // OmniAuth per-provider option args (provider :name, key: …), surfaced to a strategy as #options
	omniAuthConfig                         *OmniAuthConfig                    // the shared OmniAuth.config (test_mode / mock_auth / path_prefix)
	amThunks                               map[*RClass][]amThunk              // ActiveModel::Validations DSL registrations per class (require "active_model"); replayed onto a fresh activemodel.Validations at #valid? time so subclasses inherit ancestors' validators
	ajBases                                map[*RClass]*activejob.Base        // ActiveJob: library job class built per `class … < ActiveJob::Base` subclass (class-dispatch seam)
	ajJobOf                                map[*RObject]*activejob.Job        // ActiveJob: Ruby job instance -> backing library *Job
	ajInstOf                               map[*activejob.Job]*RObject        // ActiveJob: library *Job -> Ruby job instance
	ajTestAdapters                         map[*RClass]*activejob.TestAdapter // ActiveJob: per-class :test adapter
	ajStack                                []*RObject                         // ActiveJob: instance stack the inline #perform seam reads
	ajLastResult                           object.Value                       // ActiveJob: last #perform return value (perform_now's result)
	ajArgs                                 *activejob.Arguments               // ActiveJob: module-level ActiveJob::Arguments serializer (GlobalID seam wired in registerActiveJob)
	asConfig                               *activestorage.Config              // ActiveStorage process config (require "active_storage"); nil until first use, then a deterministic in-process config (MemStore + DiskService temp dir)
	cACChannelBase                         *RClass                            // ActionCable::Channel::Base, the superclass a subscription's channel subclass extends, backed by go-ruby-actioncable
	acServer                               object.Value                       // memoized ActionCable.server singleton (an ActionCable::Server over an in-process async adapter)
	railtieSeams                           map[any]*railtieSeam               // per-railtie/engine/app deferred initializer blocks, keyed by the library ctx object; run inline by the RunInitializer seam during Application#initialize!
	deviseConfig                           *DeviseConfig                      // the shared Devise.config the DatabaseAuthenticatable Warden strategy authenticates against
	cHanamiRouter                          *RClass                            // Hanami::Router (require "hanami/router"), backed by go-ruby-hanami; wraps a *hanami.Router
	cHanamiAction                          *RClass                            // Hanami::Action (require "hanami/action"), the action-lifecycle superclass a user subclasses
	cHanamiRequest                         *RClass                            // Hanami::Action::Request, the request handed to a Hanami action's #handle
	cHanamiResponse                        *RClass                            // Hanami::Action::Response, the mutable response a Hanami action's #handle writes into
	cHanamiFlash                           *RClass                            // Hanami::Action::Flash, the two-generation flash store on the request/response
	hanamiActionDefs                       map[*RClass]*hanamiActionDef       // per-Hanami::Action-subclass before/after/handle_exception/accept/config declarations
	cMinitestSpec                          *RClass                            // Minitest::Spec, the spec-DSL subclass of Minitest::Test
	minitestRunnables                      []*RClass                          // Minitest::Test subclasses registered via the inherited hook, in definition order (the autorun run set)
	minitestCurInstance                    object.Value                       // the test instance currently running (backs bare must_*/wont_* and _)
	minitestAutorunDone                    bool                               // guards the require "minitest/autorun" at_exit hook against a double run
	cOpenSSLDigest                         *RClass
	cArray, cHash, cRange                  *RClass
	cProc                                  *RClass
	cMethod                                *RClass
	cEnumerator                            *RClass
	cYielder                               *RClass
	cEncoding                              *RClass
	encodings                              map[string]*encodingObj
	cLazy                                  *RClass
	lastMatch                              object.Value            // $~: last regexp MatchData (or nil)
	globals                                map[string]object.Value // user-assigned $globals
	cTrueClass, cFalseClass, cNilClass     *RClass
	cRegexp, cMatchData                    *RClass
	cException                             *RClass
	curExc                                 object.Value // most recently rescued exception (for bare `raise`)

	loaded        map[string]bool   // require/require_relative: features loaded once
	featureHooks  map[string]func() // built-in feature -> body run once on its first require (e.g. shellwords)
	requireDirs   []string          // stack of directories of the files currently being required
	fileStack     []string          // stack of source files of the executing ISeq frames (for __FILE__)
	scriptName    string            // $0 / $PROGRAM_NAME: the running program's name
	defaultRandom *RandomObj        // process-wide generator for Kernel#rand / #srand
	fakerInst     *fakerState       // Faker instance + its seed source (Faker::Config.random)
	currentFiber  *Fiber            // the fiber currently running (nil at the root), for Fiber.yield

	// Concurrency: an emulated GVL (one Ruby thread executes VM code at a time).
	// The running goroutine holds gvl; it is released only inside blocking native
	// methods (Thread#join, Mutex#lock, Queue#pop, sleep, Thread.pass), where the
	// thread's execution context is saved and the next runnable thread's restored.
	gvl           sync.Mutex
	currentThread *RThread   // the thread holding the GVL
	mainThread    *RThread   // the root thread
	threads       []*RThread // all live threads, for Thread.list (GVL-guarded)

	// envFree recycles per-call frame environments. exec checks one out at entry
	// and returns it at normal exit unless a closure captured it (see Env.captured
	// / markEnvCaptured). Touched only while the GVL is held, so it needs no lock.
	envFree []*Env

	// stackFree recycles per-frame operand-stack backing arrays (see getStack).
	stackFree [][]object.Value

	// objIDs assigns stable object_id/__id__ values to symbols and reference
	// objects (value types get a deterministic id from their value); nextObjID is
	// the counter for the next reference id. Lazily initialised; GVL-guarded.
	objIDs    map[object.Value]int64
	nextObjID int64

	// extSingletons holds per-object singleton classes for reference values that
	// are not *RObject (Array, String, Hash, Proc, …) — the backing for
	// extend/singleton_class/define_singleton_method on builtin-backed objects
	// such as $LOAD_PATH. Keyed by object identity; GVL-guarded.
	extSingletons map[object.Value]*RClass

	// atExit holds Kernel#at_exit blocks, run in LIFO order when Run completes
	// normally. GVL-guarded.
	atExit []*Proc

	// frameNames is the running method-name stack (innermost last), maintained by
	// exec for Kernel#__method__ and #caller. GVL-guarded (VM code is serialized
	// by the GVL).
	frameNames []string

	// frameFiles is the running source-file stack, kept in lockstep with
	// frameNames (one entry per exec frame, "" when the ISeq carries no file). It
	// backs exception backtraces and Kernel#caller, which pair each frame's method
	// label with the file of the ISeq that frame is running. Unlike fileStack
	// (which only tracks required-file frames for __FILE__), every frame pushes
	// here so the two stacks stay aligned. GVL-guarded.
	frameFiles []string

	// children records finished synthetic child processes (Process.spawn /
	// Kernel.fork), so Process.waitpid2 can report each one's exit status.
	// childPidSeq assigns the next synthetic pid. GVL-guarded.
	children    []childStatus
	childPidSeq int64
}

// objectID returns the receiver's object_id / __id__. Immediate values get the
// deterministic ids MRI uses (fixnum n -> 2n+1, nil -> 4, true -> 20,
// false -> 0); symbols and reference objects get a stable id memoised in objIDs
// (so the same object always reports the same id, distinct objects differ).
func (vm *VM) objectID(self object.Value) object.Value {
	switch v := self.(type) {
	case object.Integer:
		// Fixnum id is 2n+1 (matches MRI up to its 62-bit fixnum range). Bignums
		// are heap objects in MRI, so they fall through to the memoised path below.
		return object.NormInt(new(big.Int).Add(new(big.Int).Lsh(big.NewInt(int64(v)), 1), big.NewInt(1)))
	case object.Bool:
		if v {
			return object.IntValue(20)
		}
		return object.IntValue(0)
	case object.Nil:
		return object.IntValue(4)
	}
	// Reference objects get a stable even id memoised in objIDs (never colliding
	// with the odd fixnum ids); refID assigns and remembers it.
	return object.IntValue(vm.refID(self))
}

// hashValue computes Kernel#hash for a value: equal-by-eql? value types hash
// equally (so they key Hashes / Sets consistently), and other objects fall back
// to their object_id. The exact numbers are unspecified in Ruby, so this uses a
// stable internal scheme rather than matching MRI's.
// installHashKeyHook wires object.CustomKeyHook so that a user object used as a
// Hash key follows its Ruby-level #hash/#eql? rather than Go pointer identity.
// Built-in value types (Integer/Float/String/Symbol/Array/…) never reach the hook
// — object.hashKey content-addresses them first; the hook only fires for a key
// whose class overrides #hash away from the default Object#hash. Puppet's loader
// registry keys a Hash by Pops::Loader::TypedName, which does exactly that, so
// without this a lookup with a fresh-but-#eql? key would miss (issue: Annotation
// type unresolved during pcore init).
func (vm *VM) installHashKeyHook() {
	object.CustomKeyHook = func(k object.Value) (int64, func(object.Value) bool, bool) {
		if !vm.hasCustomHash(k) {
			return 0, nil, false
		}
		hv := vm.send(k, "hash", nil, nil)
		rh, ok := hashAsInt(hv)
		if !ok {
			// MRI requires #hash to return an Integer and raises when a key whose
			// #hash yields something else is used; mirror that rather than silently
			// falling back to identity.
			raise("TypeError", "no implicit conversion of %s into Integer", vm.classOf(hv).name)
		}
		eql := func(stored object.Value) bool {
			return vm.send(k, "eql?", []object.Value{stored}, nil).Truthy()
		}
		return rh, eql, true
	}
}

// hasCustomHash reports whether v's method resolution finds a #hash defined
// somewhere other than the default Object#hash (or BasicObject), i.e. the user
// gave the type value semantics for hashing. Plain reference objects with no such
// override keep identity behaviour.
func (vm *VM) hasCustomHash(v object.Value) bool {
	// findMethod returns nil only for a receiver with no #hash at all (a bare
	// BasicObject); such a key keeps identity behaviour, same as a default
	// Object#hash, so a single expression covers both.
	m := vm.findMethod(v, "hash")
	return m != nil && m.owner != vm.cObject && m.owner != vm.cBasicObject
}

// hashAsInt coerces the result of a Ruby #hash call to an int64 bucket. MRI's
// Object#hash returns an Integer; a Bignum result is folded so any value works.
func hashAsInt(v object.Value) (int64, bool) {
	switch x := v.(type) {
	case object.Integer:
		return int64(x), true
	case *object.Bignum:
		return int64(x.I.Int64()), true
	}
	return 0, false
}

func (vm *VM) hashValue(self object.Value) int64 {
	switch v := self.(type) {
	case object.Integer:
		return int64(v)
	case object.Float:
		return int64(math.Float64bits(float64(v)))
	case object.Bool:
		if v {
			return 1
		}
		return 0
	case object.Nil:
		return 8
	case object.Symbol:
		return fnvHash("sym:" + string(v))
	case *object.String:
		return fnvHash("str:" + v.Str())
	case *object.Bignum:
		return fnvHash("big:" + v.I.String())
	case *object.Array:
		h := int64(1)
		for _, e := range v.Elems {
			h = h*31 + vm.hashValue(e)
		}
		return h
	}
	// Any other object: a stable hash derived from its identity id.
	return vm.refID(self)
}

// refID returns the stable per-object identity id used to hash and compare
// reference objects (the int64 backing objectID memoises). Distinct objects get
// distinct ids; the same object always reports the same one.
func (vm *VM) refID(self object.Value) int64 {
	if vm.objIDs == nil {
		vm.objIDs = map[object.Value]int64{}
	}
	if id, ok := vm.objIDs[self]; ok {
		return id
	}
	vm.nextObjID += 8
	vm.objIDs[self] = vm.nextObjID
	return vm.nextObjID
}

// fnvHash is a small deterministic 64-bit FNV-1a fold over s, used by hashValue
// for the content-addressed value types.
func fnvHash(s string) int64 {
	const offset = 1469598103934665603
	const prime = 1099511628211
	h := uint64(offset)
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= prime
	}
	return int64(h >> 1) // keep it non-negative, like MRI's Integer hash range
}

// envFreeMax caps the env free-list so a deep call burst doesn't pin memory.
const envFreeMax = 1024

// stackFree recycles per-frame operand-stack backing arrays, mirroring envFree.
// Each exec checks one out (getStack) and returns it (putStack) on normal exit;
// an exception unwinding past the return leaves it to the GC. GVL-guarded.
//
// The operand stack escapes to the heap (the push/pop closures capture and
// reassign it), so without pooling every frame allocated a fresh backing array;
// recycling removes that per-call allocation on the hot call-bound path.

// getStack returns a recycled operand stack (len 0), or a fresh one.
func (vm *VM) getStack() []object.Value {
	n := len(vm.stackFree)
	if n == 0 {
		return make([]object.Value, 0, 16)
	}
	s := vm.stackFree[n-1]
	vm.stackFree = vm.stackFree[:n-1]
	return s[:0]
}

// putStack returns an operand stack to the free-list. The slice must be empty
// of live references the caller still needs; exec only recycles on normal
// completion, when the stack holds nothing the frame will read again.
func (vm *VM) putStack(s []object.Value) {
	// getStack only ever hands out backing arrays with cap >= 16, so s always has
	// capacity worth recycling; the only reason to drop it is a full free-list.
	if len(vm.stackFree) >= envFreeMax {
		return
	}
	// Clear so a pooled stack pins nothing for the GC.
	s = s[:cap(s)]
	for i := range s {
		s[i] = nil
	}
	vm.stackFree = append(vm.stackFree, s[:0])
}

// getEnv returns a recycled frame env, or a fresh one if the free-list is empty.
func (vm *VM) getEnv() *Env {
	n := len(vm.envFree)
	if n == 0 {
		return &Env{}
	}
	e := vm.envFree[n-1]
	vm.envFree = vm.envFree[:n-1]
	return e
}

// putEnv returns an env to the free-list, but only if no closure captured it (so
// recycling can never alias a live env) and the list has room. References are
// cleared so a pooled env pins nothing for the GC.
func (vm *VM) putEnv(e *Env) {
	if e.captured || len(vm.envFree) >= envFreeMax {
		return
	}
	e.parent = nil
	e.kwargs = nil
	e.slots = nil
	e.inline = [4]object.Value{}
	vm.envFree = append(vm.envFree, e)
}

// New returns a VM writing program output to out.
func New(out io.Writer) *VM {
	vm := &VM{out: out, errOut: out, main: object.NewMain(), consts: map[string]object.Value{}, loaded: map[string]bool{}, globals: map[string]object.Value{}}
	// The main thread holds the GVL for the VM's lifetime, releasing it only at
	// blocking points so spawned Ruby threads can run (see thread.go).
	vm.gvl.Lock()
	vm.mainThread = &RThread{status: "run", done: make(chan struct{}), locals: map[object.Value]object.Value{}, parked: true}
	vm.currentThread = vm.mainThread
	vm.threads = []*RThread{vm.mainThread}
	vm.bootstrap()
	// $LOAD_PATH (and its alias $:) is a real, mutable Array that require /
	// require_relative search, so gems doing `$LOAD_PATH.unshift "lib"` work.
	loadPath := object.NewArray()
	vm.globals["$LOAD_PATH"] = loadPath
	vm.globals["$:"] = loadPath
	// $LOADED_FEATURES (alias $") is a real, mutable Array of the files already
	// loaded — code that consults or appends to it (Puppet's autoloader does
	// `$LOADED_FEATURES << file`) works.
	loadedFeatures := object.NewArray()
	vm.globals["$LOADED_FEATURES"] = loadedFeatures
	vm.globals[`$"`] = loadedFeatures
	// ARGV (the top-level constant) holds the program's command-line arguments and
	// is the very same Array as $* — a script doing ARGV.replace(...) and a library
	// reading $* see one mutable object, as in MRI. The embedded host does not feed
	// process args in yet, so it starts empty; SetARGV can repopulate it.
	argv := object.NewArray()
	vm.consts["ARGV"] = argv
	vm.globals["$*"] = argv
	vm.installPrelude()
	vm.registerEnumerator()     // after the prelude so it can mix in Enumerable
	vm.registerActiveSupport()  // ActiveSupport::Inflector + core extensions (require "active_support" / "active_support/all"), backed by go-ruby-activesupport; after the prelude so the Enumerable module (which its core-ext extends) exists
	vm.registerActionView()     // ActionView::Base view context (tag/url/form/text/number helpers + render) + FormBuilder + PartialIteration + ActiveSupport::SafeBuffer / String#html_safe (require "action_view"), backed by go-ruby-actionview; the URLFor (routes) and RenderTemplate seams wire to Ruby callables, the inline-render default evals ERB through the already-bound go-ruby-erb compiler; after registerActiveSupport (SafeBuffer nests under ActiveSupport) and after the bootstrap ERB/Erubi registration (escaping/compiler surface); #render stays a dispatchable method for a later actionpack/actionmailer binding
	vm.registerLazy()           // after Enumerator (Enumerator::Lazy is built on it)
	vm.registerFileStat()       // File::Stat / FileTest; after the prelude so File::Stat can mix in Comparable
	vm.registerIPAddr()         // IPAddr (require "ipaddr"), backed by go-ruby-ipaddr; after the prelude so IPAddr can mix in Comparable
	vm.registerPathname()       // Pathname lexical ops (cleanpath/relative_path_from/...), backed by go-ruby-pathname; after the prelude so it reopens the prelude-defined class
	vm.registerOstruct()        // OpenStruct data ops (to_h/inspect/dig/delete_field/==), backed by go-ruby-ostruct; after the prelude so it reopens the prelude-defined class
	vm.registerLogger()         // Logger (require "logger"), backed by go-ruby-logger; after the prelude so Logger::Error etc. can subclass the exception hierarchy
	vm.registerPStore()         // PStore (require "pstore"), backed by go-ruby-pstore; after the prelude so PStore::Error < StandardError
	vm.includeMySQLEnumerable() // Mysql2::Result mixes in Enumerable; after the prelude so the module exists
	vm.installHashKeyHook()
	// The prelude and built-ins are loaded; arm the level-2 AOT top level so the
	// next Run (the user program) dispatches to the compiled aotMain, if one was
	// linked in.
	vm.mainArmed = true
	return vm
}

// SetScriptPath records the path of the top-level program so require_relative
// (and a path-relative require) can resolve against its directory. Hosts call it
// before Run; with no script set, resolution falls back to the process CWD.
func (vm *VM) SetScriptPath(path string) {
	if path != "" {
		vm.requireDirs = []string{filepath.Dir(path)}
		vm.scriptName = path
	}
}

// SetScriptName records the program name ($0) without deriving a require search
// directory from it. Hosts use it for a program that has no on-disk path — the
// CLI's `-e` one-liner — so backtraces still label the top-level frame ("-e")
// while require_relative falls back to the process CWD.
func (vm *VM) SetScriptName(name string) {
	if name != "" {
		vm.scriptName = name
	}
}

// SetConst installs v as a top-level constant, visible to a subsequently-run
// program as a bare constant reference. Embedding hosts use it to seed a run —
// the wasm playground binds INPUT to the raw bytes of an image before
// evaluating Ruby that processes it.
func (vm *VM) SetConst(name string, v object.Value) { vm.consts[name] = v }

// Run executes the top-level ISeq (self = main, default definee = Object).
// isSystemExit reports whether className names SystemExit or one of its
// subclasses, so a Kernel#exit propagating to the top is recognised as a clean
// termination rather than an uncaught error.
func (vm *VM) isSystemExit(className string) bool {
	c, ok := vm.consts[className].(*RClass)
	if !ok {
		return className == "SystemExit"
	}
	for ; c != nil; c = c.super {
		if c.name == "SystemExit" {
			return true
		}
	}
	return false
}

func (vm *VM) Run(iseq *bytecode.ISeq) (result object.Value, err error) {
	defer func() {
		if r := recover(); r != nil {
			if sig, ok := r.(throwSignal); ok {
				r = RubyError{Class: "UncaughtThrowError", Message: "uncaught throw " + sig.tag.Inspect()}
			}
			// A top-level Kernel.exec (outside any fork) ran its command and is now
			// "replacing" the process: terminate the run cleanly, like Kernel#exit,
			// running at_exit handlers and returning without an error.
			if _, ok := r.(execSentinel); ok {
				vm.frameNames = vm.frameNames[:0]
				vm.frameFiles = vm.frameFiles[:0]
				vm.fileStack = vm.fileStack[:0]
				vm.runAtExit()
				result, err = object.NilV, nil
				return
			}
			rerr := r.(RubyError)
			// A SystemExit (Kernel#exit/abort) is a clean program termination, not a
			// crash: unwind to the top, run at_exit handlers, and return without an
			// error — so a real CLI that ends by calling exit (e.g. Puppet's
			// exit_on_fail) terminates quietly rather than printing a backtrace.
			if vm.isSystemExit(rerr.Class) {
				vm.frameNames = vm.frameNames[:0]
				vm.frameFiles = vm.frameFiles[:0]
				vm.fileStack = vm.fileStack[:0]
				vm.runAtExit()
				result, err = object.NilV, nil
				return
			}
			// Capture the backtrace for an uncaught exception while the frame stack
			// is still intact (panics do not pop frames; they are reset below). An
			// exception object that already carries one (captured at its raise site,
			// even before re-raises) wins; otherwise snapshot the live stack.
			rerr.Frames = vm.uncaughtBacktrace(rerr)
			// An exception unwinding past one or more exec frames leaves their
			// frameNames entries unpopped; reset the stack so a later top-level
			// statement (rescued program, REPL line) starts clean.
			vm.frameNames = vm.frameNames[:0]
			vm.frameFiles = vm.frameFiles[:0]
			vm.fileStack = vm.fileStack[:0]
			result, err = nil, rerr
		}
	}()
	// Stamp the top-level program with its path so __FILE__ in the main script (and
	// in methods/blocks it defines) reports it.
	if vm.scriptName != "" {
		setISeqFile(iseq, vm.scriptName)
	}
	res := vm.runTop(iseq)
	vm.runAtExit()
	return res, nil
}

// exec runs one ISeq. definee is the class that `def` targets in this frame;
// methodName is the name of the running method ("" at top level / class bodies),
// used to resolve `super`.
// bindKeywords peels the trailing keyword hash off args (Ruby's last-hash
// convention), validates it against the method's keyword params (raising on
// unknown/missing keywords), and returns it (never nil). It shortens *args by
// the consumed hash so positional arity is checked on the remaining args.
func (vm *VM) bindKeywords(iseq *bytecode.ISeq, args *[]object.Value) *object.Hash {
	kwargs := object.NewHash()
	if a := *args; len(a) > 0 {
		if h, ok := a[len(a)-1].(*object.Hash); ok {
			kwargs = h
			*args = a[:len(a)-1]
		}
	}
	valid := make(map[object.Symbol]bool, len(iseq.KwNames))
	for _, kn := range iseq.KwNames {
		valid[object.Symbol(kn)] = true
	}
	// With a **rest param, surplus keywords are captured rather than rejected.
	if iseq.KwRestSlot < 0 {
		var unknown []string
		for _, k := range kwargs.Keys {
			if sym, ok := k.(object.Symbol); ok && valid[sym] {
				continue
			}
			unknown = append(unknown, k.Inspect())
		}
		if len(unknown) > 0 {
			raise("ArgumentError", "unknown keyword%s: %s", plural(len(unknown)), strings.Join(unknown, ", "))
		}
	}
	var missing []string
	for i, kn := range iseq.KwNames {
		if iseq.KwRequired[i] {
			if _, ok := kwargs.Get(object.SymVal(kn)); !ok {
				missing = append(missing, ":"+kn)
			}
		}
	}
	if len(missing) > 0 {
		raise("ArgumentError", "missing keyword%s: %s", plural(len(missing)), strings.Join(missing, ", "))
	}
	return kwargs
}

// plural returns "s" when n > 1, for "keyword"/"keywords" in error messages.
func plural(n int) string {
	if n > 1 {
		return "s"
	}
	return ""
}

func (vm *VM) exec(iseq *bytecode.ISeq, self object.Value, args []object.Value, definee *RClass, methodName string, parentEnv *Env, block, selfBlock *Proc, methodLexScope *RClass) (execResult object.Value) {
	var kwargs *object.Hash
	if len(iseq.KwNames) > 0 || iseq.KwRestSlot >= 0 {
		kwargs = vm.bindKeywords(iseq, &args)
	}
	if len(args) < iseq.NumRequired || (iseq.SplatIndex < 0 && len(args) > len(iseq.Params)) {
		var expected string
		switch {
		case iseq.SplatIndex >= 0:
			expected = fmt.Sprintf("%d+", iseq.NumRequired)
		case iseq.NumRequired == len(iseq.Params):
			expected = fmt.Sprintf("%d", iseq.NumRequired)
		default:
			expected = fmt.Sprintf("%d..%d", iseq.NumRequired, len(iseq.Params))
		}
		raise("ArgumentError", "wrong number of arguments (given %d, expected %s)", len(args), expected)
	}
	env := vm.getEnv()
	env.parent, env.kwargs, env.captured = parentEnv, kwargs, false
	if iseq.NumLocals <= len(env.inline) {
		env.slots = env.inline[:iseq.NumLocals]
	} else {
		env.slots = make([]object.Value, iseq.NumLocals)
	}
	for i := range env.slots {
		env.slots[i] = object.NilV
	}
	if iseq.SplatIndex >= 0 {
		si := iseq.SplatIndex
		nbind := len(args)
		if nbind > si {
			nbind = si
		}
		copy(env.slots[:nbind], args[:nbind])
		rest := []object.Value{}
		if len(args) > si {
			rest = append(rest, args[si:]...)
		}
		env.slots[si] = object.NewArrayFromSlice(rest)
	} else {
		copy(env.slots, args)
	}
	// Supplied keyword args bind into the slots right after the positionals; the
	// prologue fills defaults for any absent optional ones.
	if kwargs != nil {
		base := len(iseq.Params)
		named := make(map[object.Symbol]bool, len(iseq.KwNames))
		for i, kn := range iseq.KwNames {
			named[object.Symbol(kn)] = true
			if v, ok := kwargs.Get(object.SymVal(kn)); ok {
				env.slots[base+i] = v
			}
		}
		// **rest captures every keyword not bound to a named param.
		if iseq.KwRestSlot >= 0 {
			rest := object.NewHash()
			for _, k := range kwargs.Keys {
				if sym, ok := k.(object.Symbol); ok && named[sym] {
					continue
				}
				v, _ := kwargs.Get(k)
				rest.Set(k, v)
			}
			env.slots[iseq.KwRestSlot] = rest
		}
	}
	// &block reifies the method's block as a Proc (nil → no block given).
	if iseq.BlockSlot >= 0 {
		if block != nil {
			env.slots[iseq.BlockSlot] = block
		} else {
			env.slots[iseq.BlockSlot] = object.NilV
		}
	}

	// Record this frame's method name for Kernel#caller (best-effort backtrace).
	// A real method frame has a non-empty name; top-level/class/block bodies push
	// "" so the depth still lines up. The entry is popped on normal completion;
	// an exception unwinding past here is reset wholesale at the Run boundary.
	vm.frameNames = append(vm.frameNames, methodName)
	// frameFiles mirrors frameNames one-for-one (every frame pushes, even with an
	// empty file) so backtraces can pair each frame's label with its source file.
	vm.frameFiles = append(vm.frameFiles, iseq.File)
	// Track the source file of frames that carry one (loaded files), so __FILE__
	// reports the file of the executing ISeq even across calls into other files.
	// Like frameNames, an exception unwinding past here is reset at the Run
	// boundary rather than popped per-frame.
	pushedFile := iseq.File != ""
	if pushedFile {
		vm.fileStack = append(vm.fileStack, iseq.File)
	}

	// Depths of the per-frame tracking stacks right after THIS frame pushed its own
	// entries. When this frame rescues an exception that unwound deeper frames, the
	// recover handler truncates back to these depths so __FILE__/require_relative,
	// caller and backtraces see the correct (this-frame) state rather than the
	// abandoned deep-frame entries. requireDirs is likewise restored so a
	// require_relative in a rescue resolves against the rescuing file.
	frameNamesDepth := len(vm.frameNames)
	frameFilesDepth := len(vm.frameFiles)
	fileStackDepth := len(vm.fileStack)
	requireDirsDepth := len(vm.requireDirs)

	stack := vm.getStack()
	push := func(v object.Value) { stack = append(stack, v) }
	pop := func() object.Value {
		v := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		return v
	}

	// caches is the per-send-site inline-cache slice, fetched lazily on the first
	// OpSend (iseqCaches type-asserts the bytecode.ISeq's `any` field, so a
	// send-free body — e.g. the hot `times { t += i }` block — never pays it).
	var caches []inlineCache

	// isReturnTarget is true for a frame that a `return` returns FROM: a method,
	// top-level or class body, a lambda (a lambda's `return` is local to it), or a
	// define_method body (which is a method, not a transparent block — `return`
	// inside it returns from the method invocation, matching MRI). An ordinary
	// block frame is transparent — its `return` is non-local — so it is not a
	// target.
	isReturnTarget := selfBlock == nil || selfBlock.isLambda || selfBlock.dmDirect

	// selfTarget identifies THIS activation: a distinct heap pointer that a
	// non-local exit compares against (by identity) to find the frame it must
	// unwind to. It catches a `return`/`next` routed back to this very frame
	// through an ensure (a local unwind), plus, for a return-target frame, a
	// non-local return raised by a block it created.
	//
	// It is allocated lazily — only a frame that can actually BE the target of a
	// non-local exit ever needs one: a frame that creates a block literal (the
	// block stores this token as its home, and may be captured and outlive the
	// frame, so the token must be a stable unique heap object — which is exactly
	// what lazy-alloc gives, and why a freelist/pool would be unsafe here), or a
	// frame that routes a `return` through a live ensure/rescue handler. A leaf
	// call (plain arithmetic, no block, no handler — fib, counter bumps) never
	// materialises one: selfTarget stays nil, so it matches no signal's (always
	// non-nil) target and the unwind machinery is unaffected, while paying zero
	// allocations. ensureTarget allocates on first need and caches, so every use
	// within a single frame shares the one identity.
	var selfTarget *returnTarget
	ensureTarget := func() *returnTarget {
		if selfTarget == nil {
			selfTarget = &returnTarget{}
		}
		return selfTarget
	}

	// homeTarget is where a non-local `return` (an explicit `return` in an
	// ordinary block) unwinds to: this frame for a return target, otherwise the
	// home method inherited from the block, so a `return` lexically nested through
	// several blocks still reaches the enclosing method. It is evaluated on demand
	// (at block creation and at a non-local return) so a frame that does neither
	// allocates no token.
	homeTarget := func() *returnTarget {
		if !isReturnTarget && selfBlock.home != nil {
			return selfBlock.home
		}
		return ensureTarget()
	}

	// homeSuper* capture the super anchor that a block literal created in this
	// frame should carry. A block is transparent to `super` just as it is to
	// `yield` and `return`: inside a method the anchor is the method itself; inside
	// a block the anchor is inherited from the enclosing block (so `super` nested
	// through several blocks still reaches the home method).
	homeSuperName, homeSuperDefinee, homeSuperArgs := methodName, definee, args
	homeDmBody := false
	if selfBlock != nil {
		homeSuperName, homeSuperDefinee, homeSuperArgs = selfBlock.superName, selfBlock.superDefinee, selfBlock.superArgs
		homeDmBody = selfBlock.dmBody
	}

	// lexCref is the scope a bare constant *reference* resolves against. For a
	// method/class body it is the definee. For a block it is the block's captured
	// lexical scope (where the block literal was written), which usually equals the
	// definee — except under class_eval/module_eval/instance_eval, where the
	// definee is switched to the eval receiver (so `def` and `CONST =` target it)
	// while constant lookup must still follow the block's textual nesting, as in
	// MRI. Constant *definition* keeps using definee; only lookup uses lexCref —
	// and a block literal created in this frame captures lexCref (not definee), so
	// a block written textually inside a class_eval'd block keeps the original
	// lexical scope for its own constant lookups instead of inheriting the eval
	// target. (This is what lets Puppet's `newvalue(:directory) { File.dirname }`,
	// written inside a `class_eval`'d property block, resolve File to ::File.)
	lexCref := definee
	if methodLexScope != nil {
		// A method defined under class_eval records the lexical scope its body's
		// bare constants resolve against (where the def was written), which differs
		// from the owner the method landed on.
		lexCref = methodLexScope
	}
	if selfBlock != nil && selfBlock.cref != nil {
		lexCref = selfBlock.cref
	}

	// Every frame catches a returnSignal aimed at its own selfTarget (a local
	// return/next routed through an ensure, or a non-local return whose home is
	// this frame). A signal for some other target passes through. On a catch the
	// per-frame tracking stacks are truncated back to this frame's depths (deeper
	// frames unwound without their normal pop) and the env/stack are recycled.
	{
		defer func() {
			if r := recover(); r != nil {
				sig, ok := r.(returnSignal)
				if !ok || sig.target != selfTarget {
					panic(r)
				}
				// Truncate one past this frame's own entries: this frame's normal
				// pop never ran, and the deeper frames that unwound left theirs too.
				vm.frameNames = vm.frameNames[:frameNamesDepth-1]
				vm.frameFiles = vm.frameFiles[:frameFilesDepth-1]
				vm.requireDirs = vm.requireDirs[:requireDirsDepth]
				if pushedFile {
					vm.fileStack = vm.fileStack[:fileStackDepth-1]
				} else {
					vm.fileStack = vm.fileStack[:fileStackDepth]
				}
				execResult = sig.value
			}
		}()
	}

	pc := 0
	var handlers []handlerFrame
	result := object.Value(object.NilV)
	finished := false
	// pendingSignal holds a non-exception unwind (returnSignal / breakSignal /
	// throwSignal) that was paused to run an intervening ensure body. The ensure
	// body ends in OpReThrow, which re-raises this signal so the unwind resumes.
	var pendingSignal any

	// runChunk runs the instruction loop until the frame finishes (OpReturn /
	// falling off the end) or a panic unwinds out. It is the shared loop body for
	// both the handler-bearing path (wrapped in a recover that resumes at a
	// rescue) and the common no-rescue path (run directly, so a method without a
	// begin/rescue — fib, dispatch, attr accessors — pays no per-frame defer).
	runChunk := func() {
		for pc < len(iseq.Insns) {
			in := iseq.Insns[pc]
			switch in.Op {
			case bytecode.OpNop:
			case bytecode.OpPushConst:
				// A string literal evaluates to a fresh mutable object each time
				// (Ruby semantics), so clone string constants on push; every other
				// constant is immutable and can be shared.
				if s, ok := iseq.Consts[in.A].(*object.String); ok {
					push(s.Dup())
				} else {
					push(iseq.Consts[in.A])
				}
			case bytecode.OpPushNil:
				push(object.NilV)
			case bytecode.OpPushTrue:
				push(object.True)
			case bytecode.OpPushFalse:
				push(object.False)
			case bytecode.OpPushSelf:
				push(self)
			case bytecode.OpNewArray:
				n := in.A
				elems := make([]object.Value, n)
				copy(elems, stack[len(stack)-n:])
				stack = stack[:len(stack)-n]
				push(object.NewArrayFromSlice(elems))
			case bytecode.OpNewHash:
				n := in.A * 2
				region := stack[len(stack)-n:]
				h := object.NewHash()
				for i := 0; i < n; i += 2 {
					h.Set(region[i], region[i+1])
				}
				stack = stack[:len(stack)-n]
				push(h)
			case bytecode.OpHashSetPair:
				v := pop()
				k := pop()
				// the accumulator hash is now on top of the stack; mutate in place.
				stack[len(stack)-1].(*object.Hash).Set(k, v)
			case bytecode.OpHashMerge:
				val := pop()
				other, ok := val.(*object.Hash)
				if !ok {
					raise("TypeError", "no implicit conversion of %s into Hash", vm.classOf(val).name)
				}
				acc := stack[len(stack)-1].(*object.Hash)
				for _, k := range other.Keys {
					v, _ := other.Get(k)
					acc.Set(k, v)
				}
			case bytecode.OpNewRange:
				hi := pop()
				lo := pop()
				push(object.NewRange(lo, hi, in.A == 1))
			case bytecode.OpPop:
				pop()
			case bytecode.OpDup:
				push(stack[len(stack)-1])
			case bytecode.OpGetLocal:
				push(env.ancestor(in.B).slots[in.A])
			case bytecode.OpSetLocal:
				env.ancestor(in.B).slots[in.A] = stack[len(stack)-1]
			case bytecode.OpGetIvar:
				push(getIvar(self, iseq.Names[in.A]))
			case bytecode.OpSetIvar:
				setIvar(self, iseq.Names[in.A], stack[len(stack)-1])
			case bytecode.OpGetConst:
				name := iseq.Names[in.A]
				v, ok := vm.resolveConst(lexCref, name)
				if !ok {
					raise("NameError", "uninitialized constant %s", name)
				}
				push(v)
			case bytecode.OpGetConstTop:
				// Leading `::Name`: top-level only, ignoring lexical nesting.
				name := iseq.Names[in.A]
				v, ok := vm.cObject.consts[name]
				if !ok {
					raise("NameError", "uninitialized constant %s", name)
				}
				push(v)
			case bytecode.OpGetScopedConst:
				name := iseq.Names[in.A]
				recv := pop()
				cls, ok := recv.(*RClass)
				if !ok {
					raise("TypeError", "%s is not a class/module", recv.Inspect())
				}
				push(vm.scopedConst(cls, name))
			case bytecode.OpSetScopedConst:
				// Foo::BAR = value. Stack is [recv, value]; pop the value, then the
				// receiver, set recv::name and push the value back (assignment is an
				// expression yielding its right-hand side).
				val := pop()
				recv := pop()
				cls, ok := recv.(*RClass)
				if !ok {
					raise("TypeError", "%s is not a class/module", recv.Inspect())
				}
				vm.assignConstIn(cls, iseq.Names[in.A], val)
				push(val)
			case bytecode.OpSetConst:
				// Assignment is an expression: set the constant in the current
				// lexical scope (lexCref's table; top-level writes land in Object's
				// table, which vm.consts aliases), keep its value. Using lexCref —
				// not definee — matches MRI: a `CONST = …` written textually inside a
				// class_eval'd block defines the constant in the block's *lexical*
				// scope (where it was written), so a sibling block in the same
				// class_eval body finds it by the same lexical path (Puppet's file
				// type defines CREATORS in its newtype block and a nested `validate`
				// block reads it back this way).
				vm.assignConst(lexCref, iseq.Names[in.A], stack[len(stack)-1])
			case bytecode.OpGetGVar:
				push(vm.gvar(iseq.Names[in.A]))
			case bytecode.OpSetGVar:
				// Assignment is an expression: set the global, keep its value.
				vm.setGVar(iseq.Names[in.A], stack[len(stack)-1])
			case bytecode.OpGetCVar:
				name := iseq.Names[in.A]
				vm.checkCVarScope(definee)
				if c := cvarOwner(definee, name); c != nil {
					push(c.cvars[name])
				} else {
					raise("NameError", "uninitialized class variable %s in %s", name, definee.name)
				}
			case bytecode.OpGetCVarQuiet:
				// The read side of @@name ||= …: an undefined class variable is
				// nil here rather than a NameError (Ruby's ||=/&&= semantics).
				name := iseq.Names[in.A]
				vm.checkCVarScope(definee)
				if c := cvarOwner(definee, name); c != nil {
					push(c.cvars[name])
				} else {
					push(object.NilV)
				}
			case bytecode.OpSetCVar:
				// Set where the variable already lives in the hierarchy, else
				// define it on the current class. Assignment keeps its value.
				name := iseq.Names[in.A]
				vm.checkCVarScope(definee)
				if c := cvarOwner(definee, name); c != nil {
					c.cvars[name] = stack[len(stack)-1]
				} else {
					definee.cvars[name] = stack[len(stack)-1]
				}
			case bytecode.OpAdd, bytecode.OpSub, bytecode.OpMul, bytecode.OpDiv,
				bytecode.OpMod, bytecode.OpLt, bytecode.OpGt, bytecode.OpLe,
				bytecode.OpGe, bytecode.OpEq, bytecode.OpNeq:
				b := pop()
				a := pop()
				push(vm.binaryOp(in.Op, a, b))
			case bytecode.OpNeg:
				push(negate(pop()))
			case bytecode.OpNot:
				push(object.Bool(!pop().Truthy()))
			case bytecode.OpTruthy:
				push(object.Bool(pop().Truthy()))
			case bytecode.OpRaiseNoMatch:
				subj := pop()
				raise("NoMatchingPatternError", "%s", subj.Inspect())
			case bytecode.OpJump:
				pc = in.A
				continue
			case bytecode.OpBranchIf:
				if pop().Truthy() {
					pc = in.A
					continue
				}
			case bytecode.OpBranchUnless:
				if !pop().Truthy() {
					pc = in.A
					continue
				}
			case bytecode.OpBranchNil:
				if _, isNil := pop().(object.Nil); isNil {
					pc = in.A
					continue
				}
			case bytecode.OpSend:
				argc := in.B
				name := iseq.Names[in.A]
				if caches == nil {
					caches = iseqCaches(iseq)
				}
				if in.C == 0 {
					// No literal block: take the monomorphic fast path that resolves
					// and invokes the method directly, skipping the dispatchSend→send
					// layers. The per-call-site inline cache (caches[pc]) turns the
					// method-table walk into a pointer compare on a cache hit — the
					// dominant case for call-bound code (dispatch / fib / proc). A
					// class receiver (singleton dispatch) or an unresolved name
					// (operator fallback / method_missing) falls back to send.
					base := len(stack) - argc
					recv := stack[base-1]
					if _, isClass := recv.(*RClass); !isClass {
						if m := vm.lookupCached(&caches[pc], recv, name); m != nil {
							// An explicit-receiver send enforces method visibility
							// (private/protected); an implicit or `self.` send does not.
							if in.Flags&bytecode.FlagSendExplicit != 0 {
								vm.checkVisibility(recv, name, m, self)
							}
							// Pass the args in place from the operand stack: invoke
							// (→ exec / a native method) consumes them before this frame
							// touches the region again, so no per-call args copy is
							// needed — this removes the single dominant allocation on the
							// call-bound path. invokeInPlace copies into a fresh slice
							// only when the callee might retain the args (native bodies).
							res := vm.invokeInPlace(m, recv, stack[base:], nil)
							stack = stack[:base-1]
							stack = append(stack, res)
							pc++
							continue
						}
					}
					// General-send fallback (class receiver / unresolved name →
					// method_missing / operator). send now routes its resolved-method
					// invocations through invokeInPlace, which copies only for a
					// retaining native callee, so the live operand-stack region can be
					// passed directly here — no per-call args copy. The region is read
					// (and copied into the callee's env by exec, or defensively by
					// invokeInPlace) before this frame truncates the stack below.
					vm.enforceSendVis(in.Flags, recv, name, self)
					res := vm.send(recv, name, stack[base:], nil)
					stack = stack[:base-1]
					stack = append(stack, res)
				} else {
					callArgs := make([]object.Value, argc)
					copy(callArgs, stack[len(stack)-argc:])
					stack = stack[:len(stack)-argc]
					recv := pop()
					vm.enforceSendVis(in.Flags, recv, name, self)
					// A literal block: capture this frame's env, self, block.
					markEnvCaptured(env)
					blk := &Proc{iseq: iseq.Children[in.C-1], env: env, self: self, block: block, cref: lexCref, home: homeTarget(), superName: homeSuperName, superDefinee: homeSuperDefinee, superArgs: homeSuperArgs, dmBody: homeDmBody}
					push(vm.dispatchSend(recv, name, callArgs, blk))
				}
			case bytecode.OpSendBlockArg:
				blockVal := pop()
				argc := in.B
				callArgs := make([]object.Value, argc)
				copy(callArgs, stack[len(stack)-argc:])
				stack = stack[:len(stack)-argc]
				recv := pop()
				vm.enforceSendVis(in.Flags, recv, iseq.Names[in.A], self)
				push(vm.dispatchSend(recv, iseq.Names[in.A], callArgs, vm.toBlock(blockVal)))
			case bytecode.OpDefineMethod:
				name := iseq.Names[in.A]
				m := &Method{name: name, iseq: iseq.Children[in.B], owner: definee, vis: definee.defaultVis}
				// Under class_eval/module_eval the def lands on the eval receiver
				// (definee) while its bare-constant lookup must follow the block's
				// textual nesting (lexCref) — record that scope when the two differ so
				// the method body resolves constants as MRI does.
				if lexCref != definee {
					m.lexScope = lexCref
				}
				// Attach the AOT-compiled body only on the first definition of
				// this name; a redefinition gets a fresh, interpreted Method
				// (deopt), since the compiled body matched the original source.
				if _, redef := definee.methods[name]; !redef {
					m.compiled = compiledFor(definee.name, name)
				}
				// A redefinition clears any stale per-receiver visibility override so
				// the new Method's own vis governs (MRI: redefining resets to the
				// current default visibility).
				delete(definee.visOverrides, name)
				// In module_function (no-arg) mode, a def is private as an instance
				// method but public as the module/singleton method.
				if definee.funcMode {
					m.vis = visPrivate
				}
				definee.methods[name] = m
				if definee.funcMode {
					sm := *m
					sm.vis = visPublic
					definee.smethods[name] = &sm
				}
				bumpMethodSerial() // a (re)definition can change what a cached send resolves to
				// Hook: definee.method_added(:name) for instance-method defs, if
				// the class/module defines the hook (singleton method).
				if hook := lookupSMethod(definee, "method_added"); hook != nil {
					vm.invoke(hook, definee, []object.Value{object.SymVal(name)}, nil)
				}
				// `def foo; end` evaluates to :foo (MRI), which is what makes
				// `private def foo; end` mark the just-defined method.
				push(object.SymVal(name))
			case bytecode.OpDefineSMethod:
				definee.smethods[iseq.Names[in.A]] = &Method{name: iseq.Names[in.A], iseq: iseq.Children[in.B], owner: definee}
				push(object.SymVal(iseq.Names[in.A]))
			case bytecode.OpDefineSingletonMethod:
				// def recv.foo: a class receiver gains a class method; any other
				// object gains a method on its singleton class.
				name := iseq.Names[in.A]
				recv := pop()
				switch t := recv.(type) {
				case *RClass:
					t.smethods[name] = &Method{name: name, iseq: iseq.Children[in.B], owner: t}
				case *RObject:
					sc := vm.singletonClass(t)
					sc.methods[name] = &Method{name: name, iseq: iseq.Children[in.B], owner: sc}
				default:
					raise("TypeError", "can't define singleton method %q for %s", name, vm.classOf(recv).name)
				}
				push(object.SymVal(name))
			case bytecode.OpOpenSingletonClass:
				// class << target: run the child body with target's singleton (meta)
				// class as the definee, so its method/constant defs attach there.
				target := pop()
				sc, ok := vm.singletonDefinee(target)
				if !ok {
					raise("TypeError", "can't define singleton")
				}
				// A `class << self` body's lexical nesting includes the enclosing
				// class/module, so bare constants in singleton methods resolve against
				// the surrounding scope (e.g. a sibling class defined alongside). Record
				// the current lexical scope as the singleton class's lexParent so
				// resolveConst walks the metaclass -> enclosing class -> ... chain.
				if sc.lexParent == nil && definee != nil && definee != vm.cObject {
					sc.lexParent = definee
				}
				push(vm.exec(iseq.Children[in.A], sc, nil, sc, "", nil, nil, nil, nil))
			case bytecode.OpAlias:
				vm.aliasMethod(definee, iseq.Names[in.A], iseq.Names[in.B])
				push(object.NilV)
			case bytecode.OpUndef:
				vm.undefMethod(definee, iseq.Names[in.A])
				push(object.NilV)
			case bytecode.OpDefineClass:
				// Bare `class B` nests into the current lexical scope (definee).
				push(vm.defineClassIn(definee, iseq.Names[in.A], iseq.Children[in.B], nil, false))
			case bytecode.OpDefineModule:
				push(vm.defineModuleIn(definee, iseq.Names[in.A], iseq.Children[in.B], false))
			case bytecode.OpDefineClassScoped:
				// C flags: bit 0 = parent on stack, bit 1 = super-expr on stack.
				// They were pushed parent-then-super, so pop super first.
				var superExpr object.Value
				if in.C&2 != 0 {
					superExpr = pop()
				}
				// A compact path name (class A::B) is scoped: its constant lives in the
				// parent's table, but its lexical nesting is only itself (Module.nesting
				// == [A::B], not [A::B, A]), so bare constants in the body do NOT see
				// the parent namespace. A bare `class B` (bit 0 clear) still nests into
				// the current lexical scope.
				parent, scoped := definee, false
				if in.C&1 != 0 {
					parent, scoped = vm.asModuleParent(pop()), true
				}
				push(vm.defineClassIn(parent, iseq.Names[in.A], iseq.Children[in.B], superExpr, scoped))
			case bytecode.OpDefineModuleScoped:
				parent := vm.asModuleParent(pop())
				push(vm.defineModuleIn(parent, iseq.Names[in.A], iseq.Children[in.B], true))
			case bytecode.OpInvokeSuper:
				superBlk := block
				if in.C > 0 { // an explicit `super(...) { … }` literal block overrides the frame block
					markEnvCaptured(env)
					superBlk = &Proc{iseq: iseq.Children[in.C-1], env: env, self: self, block: block, cref: lexCref, home: homeTarget(), superName: homeSuperName, superDefinee: homeSuperDefinee, superArgs: homeSuperArgs, dmBody: homeDmBody}
				}
				var superArgs []object.Value
				if in.B == 1 { // bare super forwards the home method's arguments
					if homeDmBody {
						// MRI forbids implicit-argument super from a define_method body.
						raise("RuntimeError", "implicit argument passing of super from method defined by define_method() is not supported. Specify all arguments explicitly.")
					}
					superArgs = homeSuperArgs
					// Keyword arguments were peeled off args into env.kwargs on entry;
					// re-attach them as the trailing hash so bare super forwards them too.
					// (Only the method's own frame carries kwargs; a block forwards the
					// home method's positional args as captured.)
					if selfBlock == nil && env.kwargs != nil && len(env.kwargs.Keys) > 0 {
						superArgs = append(append([]object.Value(nil), homeSuperArgs...), env.kwargs)
					}
				} else {
					superArgs = make([]object.Value, in.A)
					copy(superArgs, stack[len(stack)-in.A:])
					stack = stack[:len(stack)-in.A]
				}
				push(vm.invokeSuper(self, homeSuperDefinee, homeSuperName, superArgs, superBlk))
			case bytecode.OpInvokeSuperArray:
				superBlk := block
				switch {
				case in.C == 1: // a &block-pass value (on top of the args array) overrides the frame block
					superBlk = vm.toBlock(pop())
				case in.C > 1: // a literal `super(*a) { … }` block, from child C-2
					markEnvCaptured(env)
					superBlk = &Proc{iseq: iseq.Children[in.C-2], env: env, self: self, block: block, cref: lexCref, home: homeTarget(), superName: homeSuperName, superDefinee: homeSuperDefinee, superArgs: homeSuperArgs, dmBody: homeDmBody}
				}
				argsArr := pop().(*object.Array)
				push(vm.invokeSuper(self, homeSuperDefinee, homeSuperName, argsArr.Elems, superBlk))
			case bytecode.OpInvokeBlock:
				if block == nil {
					raise("LocalJumpError", "no block given (yield)")
				}
				yargs := make([]object.Value, in.A)
				copy(yargs, stack[len(stack)-in.A:])
				stack = stack[:len(stack)-in.A]
				push(vm.callBlock(block, yargs))
			case bytecode.OpInvokeBlockArray:
				if block == nil {
					raise("LocalJumpError", "no block given (yield)")
				}
				push(vm.callBlock(block, pop().(*object.Array).Elems))
			case bytecode.OpExcMatchAny:
				classes := pop().(*object.Array)
				exc := pop()
				match := false
				for _, ce := range classes.Elems {
					if classIsA(vm.classOf(exc), classArg(ce)) {
						match = true
						break
					}
				}
				push(object.Bool(match))
			case bytecode.OpCaseMatchAny:
				match := false
				if in.B == 1 { // any candidate === subject
					subject := pop()
					cands := pop().(*object.Array)
					for _, c := range cands.Elems {
						if vm.send(c, "===", []object.Value{subject}, nil).Truthy() {
							match = true
							break
						}
					}
				} else { // no subject: any candidate truthy
					cands := pop().(*object.Array)
					for _, c := range cands.Elems {
						if c.Truthy() {
							match = true
							break
						}
					}
				}
				push(object.Bool(match))
			case bytecode.OpBlockGiven:
				push(object.Bool(block != nil))
			case bytecode.OpDefinedConst:
				// defined? must NOT trigger autoload (MRI): a pending autoload still
				// reports "constant" without requiring the file.
				name := iseq.Names[in.A]
				if _, ok := vm.resolveConstNoAutoload(lexCref, name); ok || vm.autoloadPending(lexCref, name) {
					push(definedTag("constant"))
				} else {
					push(object.NilV)
				}
			case bytecode.OpDefinedConstTop:
				if _, ok := vm.cObject.consts[iseq.Names[in.A]]; ok {
					push(definedTag("constant"))
				} else {
					push(object.NilV)
				}
			case bytecode.OpDefinedScopedConst:
				name := iseq.Names[in.A]
				recv := pop()
				cls, ok := recv.(*RClass)
				if ok && vm.hasScopedConst(cls, name) {
					push(definedTag("constant"))
				} else {
					push(object.NilV)
				}
			case bytecode.OpDefinedIvar:
				name := iseq.Names[in.A]
				if t := ivarTable(self); t != nil {
					if _, ok := t[name]; ok {
						push(definedTag("instance-variable"))
						break
					}
				}
				push(object.NilV)
			case bytecode.OpDefinedCVar:
				name := iseq.Names[in.A]
				if definee != vm.cObject && cvarOwner(definee, name) != nil {
					push(definedTag("class variable"))
				} else {
					push(object.NilV)
				}
			case bytecode.OpDefinedGVar:
				if vm.gvarDefined(iseq.Names[in.A]) {
					push(definedTag("global-variable"))
				} else {
					push(object.NilV)
				}
			case bytecode.OpDefinedMethod:
				recv := pop()
				if vm.respondsTo(recv, iseq.Names[in.A]) {
					push(definedTag("method"))
				} else {
					push(object.NilV)
				}
			case bytecode.OpDefinedYield:
				if block != nil {
					push(definedTag("yield"))
				} else {
					push(object.NilV)
				}
			case bytecode.OpDefinedGuard:
				push(vm.runDefinedGuard(iseq.Children[in.A], self, definee, env, block))
			case bytecode.OpBinding:
				markEnvCaptured(env)
				push(&Binding{env: env, self: self, definee: definee, names: append([]string(nil), iseq.Locals...)})
			case bytecode.OpArgGiven:
				push(object.Bool(in.A < len(args)))
			case bytecode.OpKwGiven:
				_, ok := env.kwargs.Get(object.SymVal(iseq.KwNames[in.A]))
				push(object.Bool(ok))
			case bytecode.OpReturn:
				// A==1 marks an explicit `return` written inside a block body. In an
				// ordinary block it is a non-local return that unwinds to the method
				// the block was written in (homeTarget), past any iterator frames. In a
				// lambda, `return` is local — it just returns from the lambda.
				if in.A == 1 && !isReturnTarget {
					panic(returnSignal{target: homeTarget(), value: pop()})
				}
				// An explicit return from this frame with a live ensure handler must
				// run that ensure before unwinding; route it through the same signal
				// so popToEnsure fires. (At a method's natural end the ensure handler
				// has already been popped, so handlers is empty and we take the fast
				// path.) selfTarget is this frame's own target, caught by its own
				// defer. A lambda lands here too: its return is local, and its
				// selfTarget is the lambda frame itself, so it returns from the lambda.
				if len(handlers) > 0 {
					panic(returnSignal{target: ensureTarget(), value: pop()})
				}
				result = pop()
				finished = true
				return
			case bytecode.OpBreak:
				panic(breakSignal{owner: selfBlock, value: pop()})
			case bytecode.OpPushHandler:
				// Operand B=1 marks an ensure handler (runs on every unwind);
				// B=0 is a plain rescue handler (runs only on a rescued exception).
				handlers = append(handlers, handlerFrame{pc: in.A, sp: len(stack), isEnsure: in.B == 1})
			case bytecode.OpPopHandler:
				handlers = handlers[:len(handlers)-1]
			case bytecode.OpReThrow:
				// An ensure body reached here while a non-exception unwind was paused:
				// resume that unwind rather than re-raising the stack's exception.
				if pendingSignal != nil {
					sig := pendingSignal
					pendingSignal = nil
					panic(sig)
				}
				panic(vm.excError(pop()))
			case bytecode.OpRegexp:
				// Non-interpolated literal (/foo/, /\s+/i): a "once" literal in Ruby.
				// Memoise the frozen Regexp on this occurrence's cache slot so a literal
				// in a hot loop compiles ONCE and every evaluation returns the same
				// (.equal?) object, instead of recompiling through the engine each time.
				if caches == nil {
					caches = iseqCaches(iseq)
				}
				if slot := &caches[pc]; !object.IsNil(slot.regexp) {
					push(slot.regexp)
				} else {
					r := vm.compileLiteralRegexp(iseq.Names[in.A], iseq.Names[in.B])
					slot.regexp = r
					push(r)
				}
			case bytecode.OpRegexpDyn:
				// Interpolated regexp literal: the source String built from the parts
				// (each appended via #to_s, so the top of stack is always a String) is
				// on the stack; compile it with the static flags. A plain interpolated
				// literal rebuilds every evaluation (A == 0). A /o literal is "interpolate
				// once": store the compiled object in the guard's cache slot (A-1) so
				// later evaluations reuse it without re-running the interpolation.
				r := vm.compileLiteralRegexp(pop().(*object.String).Str(), iseq.Names[in.B])
				if in.A != 0 {
					// /o: store into the guard's slot. The guard (OpRegexpOnce) always
					// runs first and allocated caches, so it is non-nil here.
					caches[in.A-1].regexp = r
				}
				push(r)
			case bytecode.OpRegexpOnce:
				// Guards a /o interpolated literal: if this occurrence was already
				// compiled, push the memoised object and skip past the interpolation
				// build (jump to A). Otherwise fall through to build + compile once.
				if caches == nil {
					caches = iseqCaches(iseq)
				}
				if r := caches[pc].regexp; !object.IsNil(r) {
					push(r)
					pc = in.A
					continue
				}
			case bytecode.OpXStr:
				push(object.NewString(vm.runShellCommand(iseq.Names[in.A])))
			case bytecode.OpSplatToArray:
				push(vm.splatToArray(pop()))
			case bytecode.OpExpandArray:
				elems := pop().(*object.Array).Elems
				n := len(elems)
				pre, post, hasSplat := in.A, in.B, in.C == 1
				vals := make([]object.Value, 0, pre+post+1)
				if hasSplat && n >= pre+post {
					// Enough elements: the splat takes the middle, post the tail.
					for i := 0; i < pre; i++ {
						vals = append(vals, elems[i])
					}
					mid := make([]object.Value, n-pre-post)
					copy(mid, elems[pre:n-post])
					vals = append(vals, object.NewArrayFromSlice(mid))
					for i := 0; i < post; i++ {
						vals = append(vals, elems[n-post+i])
					}
				} else {
					// Too short (or no splat): fill targets left-to-right, the
					// splat is empty, and missing targets get nil.
					idx := 0
					nextVal := func() object.Value {
						if idx < n {
							v := elems[idx]
							idx++
							return v
						}
						idx++
						return object.NilV
					}
					for i := 0; i < pre; i++ {
						vals = append(vals, nextVal())
					}
					if hasSplat {
						vals = append(vals, object.NewArray())
					}
					for i := 0; i < post; i++ {
						vals = append(vals, nextVal())
					}
				}
				// Push in reverse so the first target's value is on top.
				for i := len(vals) - 1; i >= 0; i-- {
					push(vals[i])
				}
			case bytecode.OpConcatArray:
				b2 := pop().(*object.Array)
				a2 := pop().(*object.Array)
				elems := make([]object.Value, 0, len(a2.Elems)+len(b2.Elems))
				elems = append(elems, a2.Elems...)
				elems = append(elems, b2.Elems...)
				push(object.NewArrayFromSlice(elems))
			case bytecode.OpSendArray:
				argsArr := pop().(*object.Array)
				recv := pop()
				vm.enforceSendVis(in.Flags, recv, iseq.Names[in.A], self)
				var blk *Proc
				if in.C > 0 {
					markEnvCaptured(env)
					blk = &Proc{iseq: iseq.Children[in.C-1], env: env, self: self, block: block, cref: lexCref, home: homeTarget(), superName: homeSuperName, superDefinee: homeSuperDefinee, superArgs: homeSuperArgs, dmBody: homeDmBody}
				}
				push(vm.dispatchSend(recv, iseq.Names[in.A], argsArr.Elems, blk))
			case bytecode.OpSendArrayBlockArg:
				blockVal := pop()
				argsArr := pop().(*object.Array)
				recv := pop()
				vm.enforceSendVis(in.Flags, recv, iseq.Names[in.A], self)
				push(vm.dispatchSend(recv, iseq.Names[in.A], argsArr.Elems, vm.toBlock(blockVal)))
			default:
				raise("VMError", "unknown opcode %s", in.Op)
			}
			pc++
		}
		finished = true
	}

	if iseqHasHandler(iseq) {
		// This frame can rescue: run under a recover that, on a Ruby exception with
		// a live handler, unwinds the operand stack and resumes at the rescue pc;
		// other panics (or no handler) re-propagate. The loop re-enters after a
		// caught exception (handler set a new pc) until the frame finishes.
		for !finished {
			func() {
				defer func() {
					r := recover()
					if r == nil {
						return
					}
					// Truncate the per-frame tracking stacks back to this frame's own depth:
					// deeper frames that unwound never ran their normal pop, so their entries
					// would otherwise leak into __FILE__, require_relative resolution, caller
					// and backtraces taken from the rescue/ensure body.
					restoreFrameStacks := func() {
						vm.frameNames = vm.frameNames[:frameNamesDepth]
						vm.frameFiles = vm.frameFiles[:frameFilesDepth]
						vm.fileStack = vm.fileStack[:fileStackDepth]
						vm.requireDirs = vm.requireDirs[:requireDirsDepth]
					}
					rerr, ok := r.(RubyError)
					if !ok {
						// A non-exception unwind (non-local return / break / throw):
						// run an intervening ensure body before continuing it, then
						// resume the unwind via OpReThrow (pendingSignal). A plain
						// rescue handler does not apply, so it is skipped past.
						if h, found := popToEnsure(&handlers); found {
							stack = stack[:h.sp]
							restoreFrameStacks()
							pendingSignal = r
							pc = h.pc
							return
						}
						panic(r)
					}
					if len(handlers) == 0 {
						panic(r) // a Ruby exception with no handler in this frame
					}
					h := handlers[len(handlers)-1]
					handlers = handlers[:len(handlers)-1]
					stack = stack[:h.sp]
					restoreFrameStacks()
					exc := vm.exceptionObject(rerr)
					vm.curExc = exc
					push(exc)
					pc = h.pc
				}()
				runChunk()
			}()
		}
	} else {
		// No begin/rescue in this ISeq: run the loop directly. A panic propagates
		// to an enclosing frame's handler (or the Run boundary) with no per-frame
		// defer — the common case (fib, dispatch, accessors) skips that overhead.
		runChunk()
	}
	// Recycle this frame's env and operand stack on normal completion (putEnv is
	// a no-op if a closure captured the env). An exception unwinding past here
	// skips recycling and leaves both to the GC — correct, just not pooled.
	vm.putEnv(env)
	vm.putStack(stack)
	vm.frameNames = vm.frameNames[:len(vm.frameNames)-1]
	vm.frameFiles = vm.frameFiles[:len(vm.frameFiles)-1]
	if pushedFile {
		vm.fileStack = vm.fileStack[:len(vm.fileStack)-1]
	}
	return result
}

// constTable returns the constant table a const name is defined into for the
// given lexical parent: the parent class/module's own table, or the global
// top-level table (Object's, which vm.consts aliases) when parent is nil.
func (vm *VM) constTable(parent *RClass) map[string]object.Value {
	if parent == nil {
		return vm.consts
	}
	return parent.consts
}

// assignConst sets a bare constant assignment (`NAME = value`) into the current
// lexical scope's table. A nil definee (defensive) or Object writes top-level.
func (vm *VM) assignConst(definee *RClass, name string, val object.Value) {
	scope := definee
	if scope == nil {
		scope = vm.cObject
	}
	vm.assignConstIn(scope, name, val)
}

// assignConstIn sets name in scope's constant table and, if val is an anonymous
// class/module, gives it the qualified name of the constant it is bound to
// (Ruby's "assign a permanent name on first constant binding" rule).
func (vm *VM) assignConstIn(scope *RClass, name string, val object.Value) {
	scope.consts[name] = val
	if c, ok := val.(*RClass); ok && !c.named {
		c.name = scopedNameFor(scope, name)
		c.named = true
		c.lexParent = lexParentFor(scope)
	}
}

// lexParentFor returns the lexParent to record for a class/module nested in
// scope: scope itself, except Object (the top level), which terminates the
// chain as nil.
func lexParentFor(scope *RClass) *RClass {
	if scope == nil {
		return nil
	}
	if scope.name == "Object" && !scope.isModule {
		return nil
	}
	return scope
}

// scopedNameFor qualifies name with scope's name (Scope::Name), or returns name
// unqualified at the top level (Object) or when scope is anonymous.
func scopedNameFor(scope *RClass, name string) string {
	if scope == nil || scope.name == "" || (scope.name == "Object" && !scope.isModule) {
		return name
	}
	return scope.name + "::" + name
}

// defineClassIn creates or reopens a class named `name` in `parent`'s constant
// table (the top-level/Object table when parent is nil or Object). superExpr,
// when non-nil, is the evaluated superclass value (a `::`-path or other
// expression); otherwise body.Super (a bare name) is consulted. It records the
// class's fully-qualified name and lexical parent on first creation, runs the
// class body with self = the class, and returns the body's value.
func (vm *VM) defineClassIn(parent *RClass, name string, body *bytecode.ISeq, superExpr object.Value, scoped bool) object.Value {
	table := vm.constTable(parent)
	var class *RClass
	if existing, ok := table[name]; ok {
		var isClass bool
		class, isClass = existing.(*RClass)
		if !isClass || class.isModule {
			raise("TypeError", "%s is not a class", name)
		}
	} else {
		super := vm.cObject
		switch {
		case superExpr != nil:
			sc, ok := superExpr.(*RClass)
			if !ok || sc.isModule {
				raise("TypeError", "superclass must be a Class (%s given)", vm.classOf(superExpr).name)
			}
			super = sc
		case body.Super != "":
			sc, ok := vm.resolveConst(parent, body.Super)
			if !ok {
				raise("NameError", "uninitialized constant %s", body.Super)
			}
			super = sc.(*RClass)
		}
		class = newClass(scopedNameFor(parent, name), super)
		// A compact (scoped) definition's lexical nesting is only itself, so its
		// lexParent terminates the chain (nil); a bare nested definition records its
		// enclosing scope so the body sees the surrounding namespace.
		if !scoped {
			class.lexParent = lexParentFor(parent)
		}
		table[name] = class
		// Hook: superclass.inherited(subclass), fired when the class is created
		// (before its body runs) if the superclass defines the hook.
		if hook := lookupSMethod(super, "inherited"); hook != nil {
			vm.invoke(hook, super, []object.Value{class}, nil)
		}
	}
	// Each class body starts with public default visibility and no
	// module_function mode (MRI resets these on every (re)open).
	class.defaultVis, class.funcMode = visPublic, false
	adoptReopenLexParent(class, parent, scoped)
	return vm.exec(body, class, nil, class, "", nil, nil, nil, nil)
}

// adoptReopenLexParent upgrades a class/module's lexParent when it is reopened
// with a bare nested form (`module A; module B`) under a real enclosing scope but
// currently has no lexParent. This happens when B was first created via a compact
// path (`module A::B`, whose own nesting is just itself, so lexParent is nil) and
// is later reopened nested: MRI's Module.nesting for the nested body is [B, A], so
// B must reach A to resolve a bare constant defined in A. Only a nil→parent
// upgrade is performed (a scoped reopen, or one that already records an enclosing
// scope, is left untouched), so the common scoped-first/nested-later pattern
// resolves while never overwriting an existing lexical home.
func adoptReopenLexParent(c, parent *RClass, scoped bool) {
	if scoped || c.lexParent != nil {
		return
	}
	c.lexParent = lexParentFor(parent)
}

// defineModuleIn creates or reopens a module named `name` in `parent`'s constant
// table (the top-level/Object table when parent is nil or Object), recording its
// qualified name and lexical parent on first creation, runs its body with self =
// the module, and returns the body's value.
func (vm *VM) defineModuleIn(parent *RClass, name string, body *bytecode.ISeq, scoped bool) object.Value {
	table := vm.constTable(parent)
	var mod *RClass
	if existing, ok := table[name]; ok {
		var isClass bool
		mod, isClass = existing.(*RClass)
		if !isClass || !mod.isModule {
			raise("TypeError", "%s is not a module", name)
		}
	} else {
		mod = newClass(scopedNameFor(parent, name), nil)
		mod.isModule = true
		if !scoped {
			mod.lexParent = lexParentFor(parent)
		}
		table[name] = mod
	}
	mod.defaultVis, mod.funcMode = visPublic, false
	adoptReopenLexParent(mod, parent, scoped)
	return vm.exec(body, mod, nil, mod, "", nil, nil, nil, nil)
}

// asModuleParent coerces a popped value to the class/module that a scoped
// definition/assignment nests into, raising a TypeError otherwise.
func (vm *VM) asModuleParent(v object.Value) *RClass {
	cls, ok := v.(*RClass)
	if !ok {
		raise("TypeError", "%s is not a class/module", v.Inspect())
	}
	return cls
}

// invokeSuper dispatches `super`: it finds methodName starting above the current
// method's owner (its superclass chain, including their mixins) and invokes it,
// forwarding the current block.
func (vm *VM) invokeSuper(self object.Value, definee *RClass, methodName string, args []object.Value, blk *Proc) object.Value {
	if methodName == "" {
		raise("RuntimeError", "super called outside of method")
	}
	// super resolves to the next definition of methodName after the current
	// method's owner (definee) in the receiver's ancestor chain — so it walks
	// prepended and included modules, not just the superclass.
	anc := vm.ancestors(vm.classOf(self))
	start := -1
	for i, k := range anc {
		if k == definee {
			start = i
			break
		}
	}
	if start >= 0 {
		for _, k := range anc[start+1:] {
			if m, ok := k.methods[methodName]; ok {
				return vm.invoke(m, self, args, blk)
			}
		}
	} else if definee.metaOf != nil {
		// definee is a metaclass: the current method was defined in a `class << self`
		// body, so its class-method super walks the metaclass chain
		// (#<Class:Child> -> #<Class:Base> -> ...). Each metaclass's method table
		// aliases its class's smethods, so a plain methods lookup finds the inherited
		// class method.
		for k := definee.super; k != nil; k = k.super {
			if m, ok := k.methods[methodName]; ok {
				return vm.invoke(m, self, args, blk)
			}
		}
	} else if m := lookupSMethod(definee.super, methodName); m != nil {
		// definee is the class itself, outside the receiver's ancestry: this is a
		// class-method super (def self.foo), so walk the singleton-method chain.
		return vm.invoke(m, self, args, blk)
	}
	raise("NoMethodError", "super: no superclass method '%s'", methodName)
	return object.NilV
}
