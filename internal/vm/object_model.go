package vm

import (
	"fmt"
	"runtime"
	"strings"

	rake "github.com/go-ruby-rake/rake"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// NativeFn is a method implemented in Go. blk is the block passed at the call
// site (nil if none).
type NativeFn func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value

// Env is one lexical frame of local-variable slots. parent links to the
// enclosing frame so a block (closure) can read and write the locals of the
// method (or outer block) it was defined in.
type Env struct {
	slots  []object.Value
	parent *Env
	kwargs *object.Hash    // keyword arguments bound for this frame (nil if none)
	inline [4]object.Value // backs slots for the common small-frame case (no 2nd alloc)
	// captured is set when a Proc or Binding closes over this env (or a lexical
	// descendant whose chain reaches it), meaning the env outlives its frame and
	// must NOT be returned to the env free-list when exec finishes. A frame that
	// creates no escaping closure (e.g. plain recursion like fib) stays false and
	// is recycled. See markEnvCaptured.
	captured bool
}

// markEnvCaptured marks e and its lexical ancestors as captured, so none of them
// is recycled. Block envs chain to their defining env, so a closure created deep
// inside nested blocks pins the whole enclosing chain; method envs have no parent
// and stop the walk.
func markEnvCaptured(e *Env) {
	for ; e != nil && !e.captured; e = e.parent {
		e.captured = true
	}
}

// ancestor returns the env depth levels up the parent chain.
func (e *Env) ancestor(depth int) *Env {
	for ; depth > 0; depth-- {
		e = e.parent
	}
	return e
}

// Proc is a captured block: its compiled body, the env it closes over, and the
// self it runs against. It is also a first-class Ruby value (implements
// object.Value), so a `&block` param can reify it and Proc#call can invoke it.
type Proc struct {
	iseq *bytecode.ISeq
	env  *Env
	self object.Value
	// block is the method-level block in scope where this block literal was
	// written. Blocks are transparent to `yield`: a `yield` inside a block
	// reaches the enclosing method's block, which is what lets Enumerable
	// methods iterate via `each { ... yield ... }`.
	block *Proc
	// native, when non-nil, makes this a synthesized Proc (e.g. Symbol#to_proc)
	// whose body is Go rather than an ISeq; nativeArity backs Proc#arity for it.
	native      func(vm *VM, args []object.Value) object.Value
	nativeArity int
	isLambda    bool // true for lambda { } / ->(){}: backs Proc#lambda?
	// cref is the lexical scope (enclosing class/module) where this block literal
	// was written, so a bare constant inside the block resolves by the same
	// lexical nesting as the surrounding method/class body. nil means top level.
	cref *RClass
	// home identifies the method (or top-level) activation a non-local `return`
	// inside this block unwinds to — the activation where the block literal was
	// written. nil for synthesized procs that never carry an explicit return.
	home *returnTarget
	// superName / superDefinee / superArgs capture the enclosing method's super
	// anchor (its name, defining class, and arguments) at block-creation time, so
	// a `super` written inside the block resolves against that method — matching
	// MRI, where a block is transparent to super just as it is to yield. Empty
	// superName means the block was written outside any method (super is illegal).
	superName    string
	superDefinee *RClass
	superArgs    []object.Value
	// dmBody marks this Proc activation as the body of a define_method-created
	// method. Such a body anchors an *explicit* `super(args)`, but MRI forbids a
	// bare `super` (implicit argument forwarding) from it, so the frame raises in
	// that case rather than silently forwarding.
	dmBody bool
	// dmDirect marks the proc that IS a define_method body being invoked as a
	// method (set by callProcMethod on its anchored copy), as opposed to dmBody —
	// which also propagates to ordinary blocks nested *inside* such a body. Only
	// the direct body is a return target: a `return` in it returns from the method
	// invocation, while a `return` in a block nested inside it is still non-local
	// (unwinds to the define_method method), matching MRI.
	dmDirect bool
}

func (p *Proc) ToS() string     { return "#<Proc>" }
func (p *Proc) Inspect() string { return "#<Proc>" }
func (p *Proc) Truthy() bool    { return true }

// visibility is a Ruby method's access level: public (the default), private or
// protected. It is recorded on the Method and may be overridden per receiver
// for an inherited method (see RClass.visOverrides), and is enforced on the
// explicit-receiver send path.
type visibility uint8

const (
	visPublic visibility = iota
	visPrivate
	visProtected
)

// Method is a Ruby method: either native (Go) or an ISeq (compiled Ruby).
type Method struct {
	name     string
	native   NativeFn
	iseq     *bytecode.ISeq
	proc     *Proc          // for define_method: a block-backed method body
	compiled CompiledMethod // AOT-lowered native body (rbgo build); preferred when set
	owner    *RClass
	// lexScope is the lexical scope a bare constant in this method's body resolves
	// against — where the `def` was textually written. It usually equals owner,
	// but under class_eval/module_eval (where the def lands on the eval receiver
	// while its textual nesting is the block's) it is the block's lexical scope, so
	// e.g. a `def m; File.chmod … end` written at top level inside `provide do … end`
	// resolves File to ::File rather than the receiver class. nil means "same as
	// owner" (the common case), keeping normal defs unaffected.
	lexScope *RClass
	// vis is the method's access level (public/private/protected). The zero value
	// is visPublic, so every method is public unless a visibility directive marks
	// it otherwise. A per-receiver override (RClass.visOverrides) takes precedence
	// over this when set, so marking an inherited method private does not mutate
	// the shared ancestor Method.
	vis visibility
	// undefined marks an `undef`-ed method: a tombstone that halts ancestor
	// lookup so an inherited method stays hidden, while dispatch, respond_to? and
	// instance_methods all treat the name as absent (a call routes to
	// method_missing → NoMethodError).
	undefined bool
	// nonRetaining marks a native method whose body provably does NOT retain the
	// args slice beyond the call (it never stores the slice in a field, returns
	// it, or hands it to something that keeps it — it only reads elements and
	// copies element *values*). Such a method is safe to call with the caller's
	// live operand-stack region in invokeInPlace without the defensive per-call
	// copy. The zero value (false) is safe-by-default: an unmarked native always
	// gets the copy. Only set via defineNR after auditing the body.
	nonRetaining bool
}

// RClass is a class (the live, mutable method table that makes monkey-patching,
// define_method and method_missing fall out for free).
type RClass struct {
	name     string
	super    *RClass
	methods  map[string]*Method
	smethods map[string]*Method // singleton (class) methods: def self.foo
	consts   map[string]object.Value
	cvars    map[string]object.Value // class variables (@@name), shared down the hierarchy
	ivars    map[string]object.Value // the class object's OWN instance variables (@name with self = the class)
	includes []*RClass               // modules mixed in via include (most recent last)
	prepends []*RClass               // modules mixed in via prepend (most recent last), ahead of own methods
	// lexParent is the lexically-enclosing class/module this one was defined in
	// (its Module.nesting parent), used to resolve bare constants by lexical
	// scope. nil for top-level definitions and for anonymous classes that have
	// no lexical home. cObject's lexParent is nil (it terminates the chain).
	lexParent *RClass
	// named reports whether this class/module has acquired a permanent name yet.
	// An anonymous class (Class.new / Module.new) starts unnamed; the first time
	// it is assigned to a constant it takes that constant's qualified name.
	named    bool
	isModule bool
	meta     *RClass // lazily-created metaclass for `class << SomeClass`; its methods map IS this class's smethods
	metaOf   *RClass // when this RClass is a metaclass, the class it is the metaclass of (else nil); routes `class << c` visibility directives to c's class methods
	funcMode bool    // module_function (no-arg) mode: subsequent instance defs are also copied as module/singleton methods
	// defaultVis is the access level applied to subsequent `def`s in this body,
	// set by a bare `private` / `protected` / `public` with no args. It resets to
	// visPublic at the start of each class/module body (a fresh RClass starts at
	// the zero value, and reopening resets it in OpDefineClass/Module).
	defaultVis visibility
	// visOverrides records a per-receiver visibility for an INHERITED instance
	// method — one whose Method lives on an ancestor — so e.g. `private :foo`
	// where foo is defined in a superclass marks it private on this class without
	// mutating the ancestor's shared Method. A key present here wins over the
	// resolved Method's own vis. Own (re)defined methods carry their vis on the
	// Method itself and are not recorded here. nil until first use.
	visOverrides map[string]visibility
	// svisOverrides is visOverrides for class (singleton) methods — notably
	// `private_class_method :new`, where `new` is inherited from Class rather than
	// defined on the receiver. nil until first use.
	svisOverrides map[string]visibility
	// autoloads maps a (still-undefined) constant name to the path that should be
	// required the first time the constant is resolved. Set by Module#autoload /
	// Kernel#autoload; consumed (and cleared) when the constant is first resolved
	// through this class's table. nil until first use.
	autoloads map[string]string
}

func newClass(name string, super *RClass) *RClass {
	return &RClass{name: name, super: super, named: name != "", methods: map[string]*Method{}, smethods: map[string]*Method{}, consts: map[string]object.Value{}, cvars: map[string]object.Value{}, ivars: map[string]object.Value{}}
}

func (c *RClass) ToS() string {
	if c.name != "" {
		return c.name
	}
	// Anonymous class/module: MRI renders #<Class:0x...> / #<Module:0x...>.
	if c.isModule {
		return "#<Module>"
	}
	return "#<Class>"
}
func (c *RClass) Inspect() string { return c.ToS() }
func (c *RClass) Truthy() bool    { return true }

// define installs a native method on the class.
func (c *RClass) define(name string, fn NativeFn) {
	c.methods[name] = &Method{name: name, native: fn, owner: c}
	bumpMethodSerial()
}

// defineNR is define for a native whose body has been audited not to retain its
// args slice (see Method.nonRetaining). Only the OpSend fast path (invokeInPlace)
// reads the flag, to elide its defensive per-call args copy; every other caller
// is unaffected. Restrict this to bodies that merely read arguments and copy
// element values — never store, return, or forward the slice itself.
func (c *RClass) defineNR(name string, fn NativeFn) {
	c.define(name, fn)
	c.methods[name].nonRetaining = true
}

// RObject is an ordinary instance: a class plus instance variables, and an
// optional singleton class holding per-object methods (def obj.x,
// define_singleton_method, extend).
type RObject struct {
	class     *RClass
	ivars     map[string]object.Value
	singleton *RClass // lazily created; its super is `class`
	// builtin holds the wrapped value (a *object.String / *object.Array /
	// *object.Hash) when this object is an instance of a user subclass of a
	// built-in value type; nil otherwise. Value-class native methods are
	// dispatched against it (see callNative), while user methods, ivars and
	// identity stay with the RObject.
	builtin object.Value
}

// singletonClass returns o's singleton class, creating it on first use. Its
// super is the object's class, so a normal method lookup through it finds the
// per-object methods first, then the class chain.
func (vm *VM) singletonClass(o *RObject) *RClass {
	if o.singleton == nil {
		o.singleton = newClass("", o.class)
	}
	return o.singleton
}

// objSingleton returns the existing per-object singleton class for any value, or
// nil. For *RObject it is the inline field; for other reference values it is the
// side-table entry. Immediate values never have one.
func (vm *VM) objSingleton(v object.Value) *RClass {
	if o, ok := v.(*RObject); ok {
		return o.singleton
	}
	if vm.extSingletons == nil {
		return nil
	}
	return vm.extSingletons[v]
}

// ensureSingleton returns v's singleton class, creating it on first use. It
// handles *RObject (inline field) and other reference values (side table). A
// second bool reports success; immediate values (Integer/Symbol/true/false/nil)
// and classes/modules (which use a metaclass instead) are not eligible here.
func (vm *VM) ensureSingleton(v object.Value) (*RClass, bool) {
	switch t := v.(type) {
	case *RObject:
		return vm.singletonClass(t), true
	case *RClass:
		return nil, false // classes use metaClass()
	}
	if !hasIdentitySingleton(v) {
		return nil, false
	}
	if vm.extSingletons == nil {
		vm.extSingletons = map[object.Value]*RClass{}
	}
	if sc := vm.extSingletons[v]; sc != nil {
		return sc, true
	}
	sc := newClass("", vm.classOf(v))
	vm.extSingletons[v] = sc
	return sc, true
}

// hasIdentitySingleton reports whether v is a reference value that can carry a
// side-table singleton class. Immediate / value-semantics types cannot.
func hasIdentitySingleton(v object.Value) bool {
	switch v.(type) {
	case object.Integer, *object.Bignum, object.Float, object.Symbol,
		object.Bool, object.Nil:
		return false
	}
	return true
}

// metaClass returns the metaclass of a class/module — the class whose method
// table holds c's class methods (def self.foo). Its methods map aliases c's
// smethods map, so methods defined into the metaclass become c's class methods
// and class-method lookup (lookupSMethod) finds them. Created lazily.
func (c *RClass) metaClass() *RClass {
	if c.meta == nil {
		mc := newClass("#<Class:"+c.name+">", nil)
		mc.methods = c.smethods // alias: defs here become class methods of c
		mc.metaOf = c           // back-pointer: lets `private :foo` in `class << c` reach c's class methods
		// The metaclass superclass is the superclass's metaclass, so a class-method
		// `super` (def self.foo / class << self) walks to the inherited class method:
		// #<Class:Child> -> #<Class:Base> -> ... This mirrors MRI's metaclass chain.
		if c.super != nil {
			mc.super = c.super.metaClass()
		}
		c.meta = mc
	}
	return c.meta
}

// singletonDefinee returns the class that a `class << target` body should run
// with as its definee, so the body's method/constant defs attach to target's
// singleton (meta) class. A second bool reports success; an immediate value
// (Integer, Symbol, true/false/nil, …) has no singleton class in MRI.
func (vm *VM) singletonDefinee(target object.Value) (*RClass, bool) {
	if c, ok := target.(*RClass); ok {
		return c.metaClass(), true
	}
	return vm.ensureSingleton(target)
}

func (o *RObject) ToS() string {
	if !object.IsNil(o.builtin) { // a built-in value subclass renders as its wrapped value
		return o.builtin.ToS()
	}
	return "#<" + o.class.name + ">"
}
func (o *RObject) Inspect() string {
	if !object.IsNil(o.builtin) {
		return o.builtin.Inspect()
	}
	return o.ToS()
}

// HashUnwrap exposes the wrapped value so a built-in value subclass instance used
// as a Hash key hashes and compares as that value (object.KeyUnwrapper).
func (o *RObject) HashUnwrap() (object.Value, bool) {
	if !object.IsNil(o.builtin) {
		return o.builtin, true
	}
	return object.NilVal(), false
}
func (o *RObject) Truthy() bool { return true }

// lookupMethod walks the ancestor chain: at each class, its own methods then
// its included modules (most-recently-included first), then up to its super.
func lookupMethod(c *RClass, name string) *Method {
	for ; c != nil; c = c.super {
		if m := lookupOwnOrIncluded(c, name); m != nil {
			return m
		}
	}
	return nil
}

// constInAncestors searches name in cls's own constant table and up its ancestor
// chain (superclass chain plus each ancestor's included modules), as Ruby's
// scoped constant lookup does. Object/BasicObject are skipped for a non-Object
// receiver so that Recv::Foo does not leak top-level constants in through the
// implicit Object ancestor — matching MRI, where Foo::Bar only inherits a
// top-level constant when Foo itself is Object.
func (vm *VM) constInAncestors(cls *RClass, name string) (object.Value, bool) {
	for _, c := range vm.ancestors(cls) {
		if c == vm.cObject || c == vm.cBasicObject {
			if cls != vm.cObject && cls != vm.cBasicObject {
				continue
			}
		}
		if v, ok := c.consts[name]; ok {
			return v, true
		}
	}
	return object.NilVal(), false
}

// scopedConst resolves Recv::name (OpGetScopedConst, const_get): the class or
// module's own constant table, then its ancestors. It no longer falls back to
// the flat top-level table, so M::File is distinct from the top-level File.
func (vm *VM) scopedConst(cls *RClass, name string) object.Value {
	if v, ok := vm.constInAncestors(cls, name); ok {
		return v
	}
	// A pending autoload on the receiver or an ancestor: require it and retry.
	if vm.autoloadInAncestors(cls, name) {
		if v, ok := vm.constInAncestors(cls, name); ok {
			return v
		}
	}
	raise("NameError", "uninitialized constant %s::%s", cls.ToS(), name)
	return object.NilV
}

// nesting returns the lexical nesting list for a cref (Module.nesting): the cref
// itself followed by each enclosing lexParent, innermost first. cObject (which
// terminates the lexParent chain) is not part of nesting.
func (vm *VM) nesting(cref *RClass) []*RClass {
	var out []*RClass
	for c := cref; c != nil && c != vm.cObject; c = c.lexParent {
		out = append(out, c)
	}
	return out
}

// resolveConst implements Ruby's bare-constant lookup for OpGetConst, using cref
// as the current lexical scope: (1) the lexical nesting (cref and its enclosing
// lexParents), then (2) the innermost lexical scope's ancestors, then (3) the
// top-level (Object's table). This preserves lexical access to an outer
// constant while giving each class/module its own namespace.
func (vm *VM) resolveConst(cref *RClass, name string) (object.Value, bool) {
	if v, ok := vm.resolveConstNoAutoload(cref, name); ok {
		return v, true
	}
	// A miss may be served by a pending autoload registered up the lexical/ancestor
	// chain: require the recorded file, then re-resolve once.
	if vm.autoloadInLexical(cref, name) {
		return vm.resolveConstNoAutoload(cref, name)
	}
	return object.NilVal(), false
}

// resolveConstNoAutoload is resolveConst without the autoload retry: the raw
// lexical-then-ancestor-then-top-level lookup.
func (vm *VM) resolveConstNoAutoload(cref *RClass, name string) (object.Value, bool) {
	for _, c := range vm.nesting(cref) {
		if v, ok := c.consts[name]; ok {
			return v, true
		}
	}
	// Ancestors of the innermost lexical class/module (and Object's own table is
	// reached as Object is every class's ancestor, covering the top level).
	if cref != nil {
		if v, ok := vm.constInAncestors(cref, name); ok {
			return v, true
		}
	}
	if v, ok := vm.cObject.consts[name]; ok {
		return v, true
	}
	return object.NilVal(), false
}

// checkCVarScope rejects class-variable access whose lexical class is Object —
// i.e. at the top level or in a method defined directly on Object. MRI raises a
// RuntimeError there rather than treating Object as the owning class.
func (vm *VM) checkCVarScope(definee *RClass) {
	if definee == vm.cObject {
		raise("RuntimeError", "class variable access from toplevel")
	}
}

// cvarOwner returns the nearest class in c's superclass chain that already
// defines the class variable name, or nil if none does — class variables are
// shared down the hierarchy.
func cvarOwner(c *RClass, name string) *RClass {
	for ; c != nil; c = c.super {
		if _, ok := c.cvars[name]; ok {
			return c
		}
	}
	return nil
}

// lookupSMethod finds a singleton (class) method, walking the superclass chain
// so a subclass inherits its ancestors' class methods. At each level it also
// consults the modules mixed into that class's singleton (metaclass) — i.e.
// `class << self; include M; end` / `extend M` — so a module's instance methods
// become class methods, matching MRI's singleton-class ancestor order
// (prepends, then own smethods, then includes).
func lookupSMethod(c *RClass, name string) *Method {
	for ; c != nil; c = c.super {
		if c.meta != nil {
			for i := len(c.meta.prepends) - 1; i >= 0; i-- {
				if m := lookupOwnOrIncluded(c.meta.prepends[i], name); m != nil {
					return m
				}
			}
		}
		if m, ok := c.smethods[name]; ok {
			return m
		}
		if c.meta != nil {
			for i := len(c.meta.includes) - 1; i >= 0; i-- {
				if m := lookupOwnOrIncluded(c.meta.includes[i], name); m != nil {
					return m
				}
			}
		}
	}
	return nil
}

// aliasMethod implements `alias newName oldName` on definee. Names beginning
// with `$` alias a global variable (the new global takes the old's current
// value); otherwise newName becomes an alias of an existing method resolved up
// definee's ancestor chain. A missing method raises NameError, as in MRI.
func (vm *VM) aliasMethod(definee *RClass, newName, oldName string) {
	if strings.HasPrefix(oldName, "$") || strings.HasPrefix(newName, "$") {
		vm.globals[newName] = vm.gvar(oldName)
		return
	}
	m := lookupMethod(definee, oldName)
	if m == nil || m.undefined {
		raise("NameError", "undefined method '%s' for class '%s'", oldName, definee.name)
	}
	// Copy the method record under the new name, retargeting its name while
	// keeping the original body, owner and any AOT-compiled form.
	clone := *m
	clone.name = newName
	clone.undefined = false
	definee.methods[newName] = &clone
	bumpMethodSerial()
}

// undefMethod implements `undef name` on definee: it installs a tombstone so the
// name resolves to "undefined" — hiding any inherited definition and making a
// call route to method_missing (NoMethodError). Undefining a name that resolves
// nowhere raises NameError, as in MRI.
func (vm *VM) undefMethod(definee *RClass, name string) {
	if m := lookupMethod(definee, name); m == nil || m.undefined {
		raise("NameError", "undefined method '%s' for class '%s'", name, definee.name)
	}
	definee.methods[name] = &Method{name: name, owner: definee, undefined: true}
	bumpMethodSerial()
}

func lookupOwnOrIncluded(c *RClass, name string) *Method {
	// Prepended modules take priority over the class's own methods; own methods
	// take priority over included modules (Ruby's ancestor order).
	for i := len(c.prepends) - 1; i >= 0; i-- {
		if m := lookupOwnOrIncluded(c.prepends[i], name); m != nil {
			return m
		}
	}
	if m, ok := c.methods[name]; ok {
		return m
	}
	for i := len(c.includes) - 1; i >= 0; i-- {
		if m := lookupOwnOrIncluded(c.includes[i], name); m != nil {
			return m
		}
	}
	return nil
}

// ancestors returns c's method-resolution order: for each class up the
// superclass chain, its prepended modules (most-recent first) before the class
// itself, then its included modules — every module expanded for its own
// prepends/includes, and each ancestor appearing once (first occurrence wins).
// This is the basis of both Module#ancestors and super.
func (vm *VM) ancestors(c *RClass) []*RClass {
	var out []*RClass
	seen := map[*RClass]bool{}
	push := func(k *RClass) {
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	var addTree func(m *RClass)
	addTree = func(m *RClass) {
		for i := len(m.prepends) - 1; i >= 0; i-- {
			addTree(m.prepends[i])
		}
		push(m)
		for i := len(m.includes) - 1; i >= 0; i-- {
			addTree(m.includes[i])
		}
	}
	for k := c; k != nil; k = k.super {
		for i := len(k.prepends) - 1; i >= 0; i-- {
			addTree(k.prepends[i])
		}
		push(k)
		for i := len(k.includes) - 1; i >= 0; i-- {
			addTree(k.includes[i])
		}
	}
	return out
}

// classOf returns the dynamic class of any value — the basis of dispatch.
func (vm *VM) classOf(v object.Value) *RClass {
	switch x := v.(type) {
	case *RObject:
		return x.class
	case *RClass:
		// A module's class is Module; a class's class is Class. (Matches MRI:
		// `String.class == Class`, `Comparable.class == Module`.)
		if x.isModule {
			return vm.cModule
		}
		return vm.cClass
	case object.Integer:
		return vm.cInteger
	case *object.Bignum:
		return vm.cInteger
	case object.Float:
		return vm.cFloat
	case *object.Complex:
		return vm.cComplex
	case *object.Rational:
		return vm.cRational
	case *NDArray:
		return vm.cNDArray
	case *Image:
		return vm.cImage
	case *StringScanner:
		return vm.cStringScanner
	case *OptionParser:
		return vm.cOptionParser
	case *URI:
		// A URI wrapper carries its own Ruby class (URI::Generic or a scheme
		// subclass such as URI::HTTP), so it reports that class for `class` /
		// `is_a?` while every instance method lives on URI::Generic.
		return x.cls
	case *CSVRow:
		// A CSV::Row wrapper reports CSV::Row; its instance methods (field/[]/
		// to_a/to_h/headers/…) live on that class.
		return vm.cCSVRow
	case *CSVTable:
		return vm.cCSVTable
	case *RackRequest:
		// A Rack::Request wrapper reports the class stamped on it at construction.
		return x.cls
	case *RackResponse:
		return x.cls
	case *SinatraCtx:
		// The self a Sinatra route/filter block runs against.
		return x.cls
	case *SinatraSettings:
		return x.cls
	case *RodaReq:
		// The self a Roda route/matcher block runs against (Roda::RodaRequest).
		return x.cls
	case *RodaResp:
		return x.cls
	case *AsyncTask:
		return x.cls
	case *AsyncBarrier:
		return x.cls
	case *AsyncSemaphore:
		return x.cls
	case *AsyncCondition:
		return x.cls
	case *AsyncNotification:
		return x.cls
	case *AsyncQueue:
		return x.cls
	case *AsyncLimitedQueue:
		return x.cls
	case *AsyncWaiter:
		return x.cls
	case *REXMLDocument:
		return vm.cREXMLDocument
	case *REXMLElement:
		return vm.cREXMLElement
	case *REXMLElements:
		return vm.cREXMLElements
	case *REXMLAttributes:
		return vm.cREXMLAttributes
	case *REXMLText:
		return vm.cREXMLText
	case *REXMLComment:
		return vm.cREXMLComment
	case *REXMLCData:
		return vm.cREXMLCData
	case *REXMLInstruction:
		return vm.cREXMLInstruction
	case *REXMLDocType:
		return vm.cREXMLDocType
	case *csvSink:
		// The writer CSV.generate yields to its block: it reports CSV so its << /
		// push methods (defined on the CSV class) dispatch.
		return vm.cCSV
	case *Logger:
		// A Logger wrapper reports Logger; its instance methods (add/info/<</…)
		// live on that class.
		return vm.cLogger
	case *LoggerFormatter:
		// A standalone Logger::Formatter (Logger::Formatter.new) reports
		// Logger::Formatter, whose #call renders one line through the library.
		return vm.cLoggerFormatter
	case *GetoptLong:
		return vm.cGetoptLong
	case *Set:
		return vm.cSet
	case *PStore:
		return vm.cPStore
	case *Jbuilder:
		// A Jbuilder builder reports Jbuilder so its method_missing DSL and bang
		// methods (defined on that class) dispatch.
		return vm.consts["Jbuilder"].(*RClass)
	case *DryType:
		return vm.consts["Dry::Types::Type"].(*RClass)
	case *DryResult:
		return vm.consts["Dry::Types::Result"].(*RClass)
	case *DryStruct:
		return x.cls
	case *DrySchema:
		return vm.consts["Dry::Schema::Params"].(*RClass)
	case *DrySchemaBuilder:
		return vm.consts["Dry::Schema::DSL"].(*RClass)
	case *DryKey:
		return vm.consts["Dry::Schema::Key"].(*RClass)
	case *DryContract:
		return x.cls
	case *DryValidationResult:
		return vm.consts["Dry::Validation::Result"].(*RClass)
	case *DryRuleCtx:
		return vm.consts["Dry::Validation::Rule"].(*RClass)
	case *DryRuleKey:
		return vm.consts["Dry::Validation::RuleKey"].(*RClass)
	case *OAuth2Client:
		return vm.consts["OAuth2::Client"].(*RClass)
	case *OAuth2Strategy:
		return vm.consts[x.className].(*RClass)
	case *OAuth2AccessToken:
		return vm.consts["OAuth2::AccessToken"].(*RClass)
	case *OAuth2Response:
		return vm.consts["OAuth2::Response"].(*RClass)
	case *OAuth2Request:
		return vm.consts["OAuth2::Request"].(*RClass)
	case *OIDCProviderMetadata:
		return vm.consts["OpenIDConnect::ProviderMetadata"].(*RClass)
	case *OIDCKeySet:
		return vm.consts["OpenIDConnect::KeySet"].(*RClass)
	case *OIDCVerifier:
		return vm.consts["OpenIDConnect::Verifier"].(*RClass)
	case *OIDCClaims:
		return vm.consts["OpenIDConnect::IDTokenClaims"].(*RClass)
	case *OIDCClient:
		return vm.consts["OpenIDConnect::Client"].(*RClass)
	case *OIDCTokens:
		return vm.consts["OpenIDConnect::Tokens"].(*RClass)
	case *OIDCUserInfo:
		return vm.consts["OpenIDConnect::UserInfo"].(*RClass)
	case *KramdownDoc:
		// A Kramdown::Document reports Kramdown::Document so its #to_html renders
		// the held source through the library.
		return vm.consts["Kramdown::Document"].(*RClass)
	case *I18nBackend:
		// The handle I18n.backend returns reports I18n::Backend::Simple so its
		// store_translations / available_locales dispatch.
		return vm.consts["I18n::Backend::Simple"].(*RClass)
	case *ZeitwerkLoader:
		// A Zeitwerk::Loader.new / .for_gem handle reports Zeitwerk::Loader so its
		// push_dir / setup / eager_load / … instance methods dispatch.
		return vm.consts["Zeitwerk::Loader"].(*RClass)
	case *ZeitwerkInflector:
		// A Zeitwerk::Inflector handle reports Zeitwerk::Inflector so its
		// camelize / inflect dispatch.
		return vm.consts["Zeitwerk::Inflector"].(*RClass)
	case *RSSRss:
		return vm.consts["RSS::Rss"].(*RClass)
	case *RSSChannel:
		return vm.consts["RSS::Rss::Channel"].(*RClass)
	case *RSSItem:
		return vm.consts["RSS::Rss::Channel::Item"].(*RClass)
	case *RSSImage:
		return vm.consts["RSS::Rss::Channel::Image"].(*RClass)
	case *RSSGuid:
		return vm.consts["RSS::Rss::Channel::Item::Guid"].(*RClass)
	case *RSSRDF:
		return vm.consts["RSS::RDF"].(*RClass)
	case *RSSRDFChannel:
		return vm.consts["RSS::RDF::Channel"].(*RClass)
	case *RSSRDFItem:
		return vm.consts["RSS::RDF::Item"].(*RClass)
	case *RSSRDFImage:
		return vm.consts["RSS::RDF::Image"].(*RClass)
	case *RSSRDFTextinput:
		return vm.consts["RSS::RDF::Textinput"].(*RClass)
	case *RSSAtomFeed:
		return vm.consts["RSS::Atom::Feed"].(*RClass)
	case *RSSAtomEntry:
		return vm.consts["RSS::Atom::Feed::Entry"].(*RClass)
	case *RSSAtomLink:
		return vm.consts["RSS::Atom::Link"].(*RClass)
	case *RSSAtomPerson:
		return vm.consts["RSS::Atom::Person"].(*RClass)
	case *RSSAtomCategory:
		return vm.consts["RSS::Atom::Category"].(*RClass)
	case *RQRCode:
		// An RQRCode::QRCode reports RQRCode::QRCode so its renderer methods
		// (as_svg / as_ansi / as_html / to_s / checked? / to_a) dispatch.
		return vm.consts["RQRCode::QRCode"].(*RClass)
	case *ImagesImage:
		// An Images::Image reports Images::Image so its processing + scikit-image
		// methods (resize / crop / rotate / convert / blur / edges / at / to_blob)
		// dispatch.
		return vm.consts["Images::Image"].(*RClass)
	case *ImagesCanvas:
		// An Images::Canvas reports Images::Canvas so its chunky_png-style pixel,
		// drawing and PNG-output methods dispatch.
		return vm.consts["Images::Canvas"].(*RClass)
	case *XmlMarkup:
		// A Builder::XmlMarkup emitter reports Builder::XmlMarkup so its
		// method_missing element DSL and special methods dispatch.
		return vm.consts["Builder::XmlMarkup"].(*RClass)
	case *SQLite3Database:
		return vm.consts["SQLite3::Database"].(*RClass)
	case *SQLite3Statement:
		return vm.consts["SQLite3::Statement"].(*RClass)
	case *BoltDB:
		return x.cls
	case *BoltTx:
		return x.cls
	case *BoltBucket:
		return x.cls
	case *BoltCursor:
		return x.cls
	case *SAMLSettings:
		return x.cls
	case *SAMLAuthrequest:
		return x.cls
	case *SAMLResponse:
		return x.cls
	case *SAMLMetadata:
		return x.cls
	case *SAMLIdpMetadataParser:
		return x.cls
	case *SAMLLogoutrequest:
		return x.cls
	case *SAMLSloLogoutresponse:
		return x.cls
	case *WebAuthnRelyingParty:
		return x.cls
	case *WebAuthnPublicKey:
		return x.cls
	case *WebAuthnCredential:
		return x.cls
	case *WebAuthnCreateOptions:
		return x.cls
	case *WebAuthnGetOptions:
		return x.cls
	case *ACMEClient:
		return x.cls
	case *ACMEOrder:
		return x.cls
	case *ACMEAuthorization:
		return x.cls
	case *ACMEChallenge:
		return x.cls
	case *ACMECSR:
		return x.cls
	case *GRPCServer:
		return vm.consts["GRPC::RpcServer"].(*RClass)
	case *GRPCStub:
		return vm.consts["GRPC::ClientStub"].(*RClass)
	case *GRPCService:
		return vm.consts["GRPC::Service"].(*RClass)
	case *GRPCActiveCall:
		return vm.consts["GRPC::ActiveCall"].(*RClass)
	case *GRPCStatus:
		return vm.consts["GRPC::Status"].(*RClass)
	case *NATSX:
		return x.cls
	case *KafkaClient:
		return x.cls
	case *KafkaProducer:
		return x.cls
	case *KafkaConsumer:
		return x.cls
	case *KafkaMessage:
		return x.cls
	case *KafkaBatch:
		return x.cls
	case *EtcdClient:
		return x.cls
	case *EtcdKeyValue:
		return x.cls
	case *EtcdGetResult:
		return x.cls
	case *EtcdPutResult:
		return x.cls
	case *EtcdDelResult:
		return x.cls
	case *EtcdLease:
		return x.cls
	case *EtcdEvent:
		return x.cls
	case *EtcdTxn:
		return x.cls
	case *EtcdCmp:
		return x.cls
	case *EtcdOp:
		return x.cls
	case *EtcdTxnResult:
		return x.cls
	case *EtcdLock:
		return x.cls
	case *EtcdMember:
		return x.cls
	case *EtcdStatus:
		return x.cls
	case *VaultClient:
		return x.cls
	case *VaultLogical:
		return x.cls
	case *VaultKVv1:
		return x.cls
	case *VaultKVv2:
		return x.cls
	case *VaultTransit:
		return x.cls
	case *VaultSys:
		return x.cls
	case *VaultAuth:
		return x.cls
	case *VaultTokenAuth:
		return x.cls
	case *VaultAppRole:
		return x.cls
	case *VaultUserpass:
		return x.cls
	case *VaultSecret:
		return x.cls
	case *MySQLClient:
		return x.cls
	case *MySQLResult:
		return x.cls
	case *MySQLStatement:
		return x.cls
	case *MongoClient:
		return x.cls
	case *MongoDatabase:
		return x.cls
	case *MongoCollection:
		return x.cls
	case *MongoCursor:
		return x.cls
	case *MongoResult:
		return x.cls
	case *MongoObjectId:
		return x.cls
	case *ParquetReader:
		return vm.consts["Parquet::ArrowFileReader"].(*RClass)
	case *ParquetWriter:
		return vm.consts["Parquet::ArrowFileWriter"].(*RClass)
	case *HTTPartyResponse:
		return vm.consts["HTTParty::Response"].(*RClass)
	case *RConnectionPool:
		return x.cls
	case *RConnectionPoolWrapper:
		return x.cls
	case *ConcurrentAtomicRef:
		return x.cls
	case *ConcurrentAtomicFixnum:
		return x.cls
	case *ConcurrentAtomicBool:
		return x.cls
	case *ConcurrentMap:
		return x.cls
	case *ConcurrentFuture:
		return x.cls
	case *ConcurrentPromise:
		return x.cls
	case *ConcurrentPool:
		return x.cls
	case *ConcurrentLatch:
		return x.cls
	case *ConcurrentSemaphore:
		return x.cls
	case *ConcurrentBarrier:
		return x.cls
	case *ASInflections:
		return vm.consts["ActiveSupport::Inflector::Inflections"].(*RClass)
	case *ErubiEngine:
		return x.cls
	case *SafeBufferVal:
		return x.cls
	case *ActionViewBase:
		return x.cls
	case *AVPartialIter:
		return x.cls
	case *FormBuilderVal:
		return x.cls
	case *HTTPrbClient:
		return vm.consts["HTTP::Client"].(*RClass)
	case *HTTPrbResponse:
		return vm.consts["HTTP::Response"].(*RClass)
	case *HTTPrbStatus:
		return vm.consts["HTTP::Response::Status"].(*RClass)
	case *ExconConnection:
		return vm.consts["Excon::Connection"].(*RClass)
	case *ExconResponse:
		return vm.consts["Excon::Response"].(*RClass)
	case *TyphoeusRequest:
		return vm.consts["Typhoeus::Request"].(*RClass)
	case *TyphoeusResponse:
		return vm.consts["Typhoeus::Response"].(*RClass)
	case *TyphoeusHydra:
		return vm.consts["Typhoeus::Hydra"].(*RClass)
	case *IMAPObj:
		return x.cls
	case *sftpSession:
		return x.cls
	case *sftpDir:
		return x.cls
	case *sftpFileFactory:
		return x.cls
	case *sftpFile:
		return x.cls
	case *sftpAttrs:
		return x.cls
	case *sftpName:
		return x.cls
	case *RedisObj:
		return x.cls
	case *RedisBatch:
		return x.cls
	case *POP3Obj:
		return x.cls
	case *POPMailObj:
		return x.cls
	case *ftpObj:
		return x.cls
	case *ftpMLSxEntry:
		return x.cls
	case *PGConnObj:
		return x.cls
	case *PGResultObj:
		return x.cls
	case *SMTPObj:
		return x.cls
	case *SMTPResponseObj:
		return x.cls
	case *SMTPStreamObj:
		return x.cls
	case *SequelDBObj:
		return x.cls
	case *SequelDatasetObj:
		return x.cls
	case *SequelSchemaObj:
		return x.cls
	case *NokogiriDocument:
		return vm.consts["Nokogiri::XML::Document"].(*RClass)
	case *NokogiriNode:
		return vm.consts["Nokogiri::XML::Node"].(*RClass)
	case *NokogiriNodeSet:
		return vm.consts["Nokogiri::XML::NodeSet"].(*RClass)
	case *NokogiriXSLTStylesheet:
		return vm.consts["Nokogiri::XSLT::Stylesheet"].(*RClass)
	case *RuboCopRunner:
		return vm.consts["RuboCop::Runner"].(*RClass)
	case *RuboCopConfig:
		return vm.consts["RuboCop::Config"].(*RClass)
	case *RuboCopOffense:
		return vm.consts["RuboCop::Cop::Offense"].(*RClass)
	case *RuboCopLocation:
		return vm.consts["RuboCop::Cop::Offense::Location"].(*RClass)
	case *GrapeRouter:
		return vm.consts["Grape::Router"].(*RClass)
	case *GrapeRoute:
		return vm.consts["Grape::Router::Route"].(*RClass)
	case *GrapeMatch:
		return vm.consts["Grape::Router::Match"].(*RClass)
	case *GrapeValidator:
		return vm.consts["Grape::Validator"].(*RClass)
	case *GrapeParamsBuilder:
		return vm.consts["Grape::Validations::ParamsScope::DSL"].(*RClass)
	case *GrapeFormatter:
		return vm.consts["Grape::Formatter"].(*RClass)
	case *ActiveRecordModel:
		return vm.consts["ActiveRecord::Model"].(*RClass)
	case *ActiveRecordModelBuilder:
		return vm.consts["ActiveRecord::Model::DSL"].(*RClass)
	case *ActiveRecordRelation:
		return vm.consts["ActiveRecord::Relation"].(*RClass)
	case *ActiveRecordRecord:
		return vm.consts["ActiveRecord::Record"].(*RClass)
	case *ActiveRecordErrors:
		return vm.consts["ActiveRecord::Errors"].(*RClass)
	case *ActiveRecordSchemaDSL:
		return vm.consts["ActiveRecord::Schema::Definition"].(*RClass)
	case *ActiveRecordTableDSL:
		return vm.consts["ActiveRecord::Schema::TableDefinition"].(*RClass)
	case *ASModelName:
		return vm.consts["ActiveModel::Name"].(*RClass)
	case *ASModelErrors:
		return vm.consts["ActiveModel::Errors"].(*RClass)
	case *ASModelError:
		return vm.consts["ActiveModel::Error"].(*RClass)
	case *ActiveJobConfigured:
		return vm.consts["ActiveJob::ConfiguredJob"].(*RClass)
	case *ASBlob:
		return vm.consts["ActiveStorage::Blob"].(*RClass)
	case *ASAttachment:
		return vm.consts["ActiveStorage::Attachment"].(*RClass)
	case *ASService:
		return vm.consts["ActiveStorage::Service"].(*RClass)
	case *ASOne:
		return vm.consts["ActiveStorage::Attached::One"].(*RClass)
	case *ASMany:
		return vm.consts["ActiveStorage::Attached::Many"].(*RClass)
	case *RailtieVal:
		return x.cls
	case *EngineVal:
		return x.cls
	case *RailsAppVal:
		return x.cls
	case *RailsConfigVal:
		return x.cls
	case *RailsPathsVal:
		return x.cls
	case *RailsPathVal:
		return x.cls
	case *RailsRouteSetVal:
		return x.cls
	case *RailsInitializerVal:
		return x.cls
	case *StringInquirerVal:
		return x.cls
	case *RailsEnvVal:
		return x.cls
	case *DeviseConfig:
		return x.cls
	case *DeviseResource:
		return x.cls
	case *DeviseTokenGenerator:
		return x.cls
	case *HanamiRouter:
		return x.cls
	case *HanamiReq:
		return x.cls
	case *HanamiResp:
		return x.cls
	case *HanamiFlash:
		return x.cls
	case *RSpecMatcher:
		return vm.consts["RSpec::Matchers::BuiltIn::BaseMatcher"].(*RClass)
	case *RSpecExpectation:
		return vm.consts["RSpec::Expectations::ExpectationTarget"].(*RClass)
	case *PrettyPrint:
		return vm.cPrettyPrint
	case *PrettyPrintGroup:
		return vm.consts["PrettyPrint::Group"].(*RClass)
	case *SingleLine:
		return vm.consts["PrettyPrint::SingleLine"].(*RClass)
	case *IPAddr:
		return vm.cIPAddr
	case *Matrix:
		return vm.cMatrix
	case *Vector:
		return vm.cVector
	case *ArrowArray:
		return vm.consts["Arrow::Array"].(*RClass)
	case *ArrowArrayBuilder:
		return vm.consts["Arrow::ArrayBuilder"].(*RClass)
	case *ArrowDataType:
		return vm.consts["Arrow::DataType"].(*RClass)
	case *ArrowField:
		return vm.consts["Arrow::Field"].(*RClass)
	case *ArrowSchema:
		return vm.consts["Arrow::Schema"].(*RClass)
	case *ArrowRecordBatch:
		return vm.consts["Arrow::RecordBatch"].(*RClass)
	case *ArrowTable:
		return vm.consts["Arrow::Table"].(*RClass)
	case *SpellChecker:
		return vm.cSpellChecker
	case *Time:
		return vm.cTime
	case *Timezone:
		return vm.consts["TZInfo::Timezone"].(*RClass)
	case *TimezonePeriod:
		return vm.consts["TZInfo::TimezonePeriod"].(*RClass)
	case *TimezoneOffset:
		return vm.consts["TZInfo::TimezoneOffset"].(*RClass)
	case *Country:
		return vm.consts["TZInfo::Country"].(*RClass)
	case *Pagy:
		return vm.consts["Pagy"].(*RClass)
	case *Money:
		return vm.consts["Money"].(*RClass)
	case *Currency:
		return vm.consts["Money::Currency"].(*RClass)
	case *AddressableURI:
		return vm.consts["Addressable::URI"].(*RClass)
	case *AddressableTemplate:
		return vm.consts["Addressable::Template"].(*RClass)
	case *PublicSuffixDomain:
		return vm.consts["PublicSuffix::Domain"].(*RClass)
	case *MIMEType:
		return vm.consts["MIME::Type"].(*RClass)
	case *MailMessage:
		return vm.consts["Mail::Message"].(*RClass)
	case *MailBody:
		return vm.consts["Mail::Body"].(*RClass)
	case *MailField:
		return vm.consts["Mail::Field"].(*RClass)
	case *ActionMailerDelivery:
		return x.cls
	case *ActionMailerAttachments:
		return x.cls
	case *FileStat:
		return vm.cFileStat
	case *BigDecimal:
		return vm.cBigDecimal
	case *Tms:
		return vm.cBenchmarkTms
	case *benchReport:
		return vm.cBenchmarkReport
	case *benchJob:
		return vm.cBenchmarkJob
	case *Date:
		// A DateTime is the same wrapper with the IsDateTime flag set; it reports the
		// DateTime class (a subclass of Date), so it inherits Date's methods yet has
		// its own identity for `class` / `is_a?`.
		if x.d.IsDateTime() {
			return vm.cDateTime
		}
		return vm.cDate
	case *Bag:
		return vm.cBag
	case *object.String:
		return vm.cString
	case object.Symbol:
		return vm.cSymbol
	case *object.Array:
		return vm.cArray
	case *object.Hash:
		return vm.cHash
	case *object.Range:
		return vm.cRange
	case *Proc:
		return vm.cProc
	case *BoundMethod:
		return vm.cMethod
	case *UnboundMethod:
		return vm.consts["UnboundMethod"].(*RClass)
	case *Enumerator:
		return vm.cEnumerator
	case *yielder:
		return vm.cYielder
	case *encodingObj:
		return vm.cEncoding
	case *LazyEnum:
		return vm.cLazy
	case *RandomObj:
		return vm.consts["Random"].(*RClass)
	case *Fiber:
		return vm.consts["Fiber"].(*RClass)
	case *RThread:
		return vm.consts["Thread"].(*RClass)
	case *RMutex:
		return vm.consts["Mutex"].(*RClass)
	case *RQueue:
		return vm.consts["Queue"].(*RClass)
	case *IOObj:
		return x.cls
	case *RelineHistory:
		return x.cls
	case *tcpSocket:
		return x.cls
	case *rawSocket:
		return x.cls
	case *tcpServer:
		return x.cls
	case *udpSocket:
		return x.cls
	case *sslSocket:
		return x.cls
	case *sslServer:
		return x.cls
	case *addrinfo:
		return x.cls
	case *opensslDigest:
		return x.cls
	case *DigestObj:
		return x.cls
	case *BCryptPassword:
		return x.cls
	case *naclObj:
		return x.cls
	case *AgeX25519Identity:
		return x.cls
	case *AgeX25519Recipient:
		return x.cls
	case *AgeScryptIdentity:
		return x.cls
	case *AgeScryptRecipient:
		return x.cls
	case *PrawnDocument:
		return x.cls
	case *ProtobufPool:
		return x.cls
	case *ProtobufBuilder:
		return x.cls
	case *ProtobufMessageBuilder:
		return x.cls
	case *ProtobufOneofBuilder:
		return x.cls
	case *ProtobufEnumBuilder:
		return x.cls
	case *ProtobufDescriptor:
		return x.cls
	case *ProtobufEnumDescriptor:
		return x.cls
	case *ProtobufFieldDescriptor:
		return x.cls
	case *ProtobufMsgClass:
		return x.cls
	case *ProtobufMessage:
		return x.cls
	case *ProtobufRepeatedField:
		return x.cls
	case *ProtobufMap:
		return x.cls
	case *BleveIndex:
		return x.cls
	case *BleveMapping:
		return x.cls
	case *BleveQuery:
		return x.cls
	case *BleveSearchResult:
		return x.cls
	case *BleveHit:
		return x.cls
	case *BleveBatch:
		return x.cls
	case *BleveFacet:
		return x.cls
	case *GraphQLType:
		return x.cls
	case *GraphQLSchema:
		return x.cls
	case *GraphQLObjectDSL:
		return x.cls
	case *OTelTracerProvider:
		return x.cls
	case *OTelTracer:
		return vm.consts["OpenTelemetry::Trace::Tracer"].(*RClass)
	case *OTelSpan:
		return vm.consts["OpenTelemetry::Trace::Span"].(*RClass)
	case *OTelSpanContext:
		return vm.consts["OpenTelemetry::Trace::SpanContext"].(*RClass)
	case *OTelStatus:
		return vm.consts["OpenTelemetry::Trace::Status"].(*RClass)
	case *OTelExporter:
		return vm.consts["OpenTelemetry::SDK::Trace::Export::InMemorySpanExporter"].(*RClass)
	case *OTelProcessor:
		return x.cls
	case *OTelFinishedSpan:
		return vm.consts["OpenTelemetry::SDK::Trace::SpanData"].(*RClass)
	case *RDocMarkup:
		return vm.consts["RDoc::Markup"].(*RClass)
	case *RDocFormatter:
		return vm.consts[x.clsName].(*RClass)
	case *ThorOption:
		return vm.consts["Thor::Option"].(*RClass)
	case *ThorOptions:
		return vm.consts["Thor::Options"].(*RClass)
	case *ThorCommand:
		return vm.consts["Thor::Command"].(*RClass)
	case *ThorBase:
		return vm.consts["Thor::Base"].(*RClass)
	case *RakeTaskVal:
		if _, isFile := x.t.(*rake.FileTask); isFile {
			return vm.consts["Rake::FileTask"].(*RClass)
		}
		return vm.consts["Rake::Task"].(*RClass)
	case *RakeApplicationVal:
		return vm.consts["Rake::Application"].(*RClass)
	case *RakeFileListVal:
		return vm.consts["Rake::FileList"].(*RClass)
	case *CapServerVal:
		return vm.consts["Capistrano::Server"].(*RClass)
	case *CapSessionVal:
		return vm.consts["Capistrano::Session"].(*RClass)
	case *CapTaskVal:
		return vm.consts["Capistrano::Task"].(*RClass)
	case *CapBackendVal:
		return vm.consts["Capistrano::TestBackend"].(*RClass)
	case *BundlerLockfile:
		return vm.consts["Bundler::LockfileParser"].(*RClass)
	case *BundlerSpec:
		return vm.consts["Bundler::LazySpecification"].(*RClass)
	case *BundlerDependency:
		return vm.consts["Bundler::Dependency"].(*RClass)
	case *BundlerGemfile:
		return vm.consts["Bundler::Dsl"].(*RClass)
	case *BundlerIndex:
		return vm.consts["Bundler::Index"].(*RClass)
	case *MinitestAssertionsBox:
		return vm.consts["Minitest::Assertions"].(*RClass)
	case *MinitestResult:
		return vm.consts["Minitest::Result"].(*RClass)
	case *MinitestMock:
		return vm.consts["Minitest::Mock"].(*RClass)
	case *FaradayConnection:
		return vm.consts["Faraday::Connection"].(*RClass)
	case *FaradayRequest:
		return vm.consts["Faraday::Request"].(*RClass)
	case *FaradayResponse:
		return vm.consts["Faraday::Response"].(*RClass)
	case *FaradayParams:
		return vm.consts["Faraday::Utils::ParamsHash"].(*RClass)
	case *FaradayHeaders:
		return vm.consts["Faraday::Utils::Headers"].(*RClass)
	case *PumaServer:
		return vm.consts["Puma::Server"].(*RClass)
	case *PumaThreadPool:
		return vm.consts["Puma::ThreadPool"].(*RClass)
	case *PumaConfiguration:
		return vm.consts["Puma::Configuration"].(*RClass)
	case *PumaDSL:
		return vm.consts["Puma::DSL"].(*RClass)
	case *WEBrickServer:
		return vm.consts["WEBrick::HTTPServer"].(*RClass)
	case *WEBrickRequest:
		return vm.consts["WEBrick::HTTPRequest"].(*RClass)
	case *WEBrickResponse:
		return vm.consts["WEBrick::HTTPResponse"].(*RClass)
	case *WardenManager:
		return x.cls
	case *WardenProxy:
		return x.cls
	case *WardenStrategy:
		return x.cls
	case *OmniAuthBuilder:
		return x.cls
	case *OmniAuthConfig:
		return x.cls
	case *OmniAuthMockAuth:
		return x.cls
	case *OmniAuthStrategy:
		return x.cls
	case *OmniAuthHash:
		return x.cls
	case *ACRouteSet:
		return x.cls
	case *ACMapper:
		return x.cls
	case *ACParams:
		return x.cls
	case *ACRequest:
		return x.cls
	case *ACResponse:
		return x.cls
	case *Binding:
		return vm.consts["Binding"].(*RClass)
	case *JobRedis:
		// A Sidekiq.redis / Resque.redis block connection reports the class stamped
		// on it at construction (Sidekiq::RedisConnection or Resque::RedisConnection).
		return x.cls
	case *ResqueJob:
		return x.cls
	case *ResqueWorker:
		return x.cls
	case *Regexp:
		return vm.cRegexp
	case *MatchData:
		return vm.cMatchData
	case object.Bool:
		if x {
			return vm.cTrueClass
		}
		return vm.cFalseClass
	case object.Nil:
		return vm.cNilClass
	case *object.Main:
		return vm.cObject
	}
	// Platform-gated value types (e.g. the AF_UNIX sockets, compiled only on
	// non-Windows) report their Ruby class through classOfPlatform rather than a
	// case above, so the shared switch stays platform-neutral. On platforms with
	// no such types it is a plain `return nil`, so there is no dead branch.
	return vm.classOfPlatform(v)
}

// send is the dispatch core (our objc_msgSend): find the method on the
// receiver's class chain, else route to method_missing (Object's default
// raises NoMethodError). blk is the block passed to the call (nil if none).
// findMethod resolves name on recv exactly as send dispatch would (class
// singleton methods, then a per-object singleton class, then the class chain),
// returning nil when nothing but method_missing would handle it. It drives
// respond_to? so a singleton or class method is seen.
func (vm *VM) findMethod(recv object.Value, name string) *Method {
	if cls, ok := recv.(*RClass); ok {
		if m := lookupSMethod(cls, name); m != nil {
			return m
		}
	}
	c := vm.classOf(recv)
	if sc := vm.objSingleton(recv); sc != nil {
		c = sc
	}
	if m := lookupMethod(c, name); m != nil && !m.undefined {
		return m
	}
	return nil
}

func (vm *VM) send(recv object.Value, name string, args []object.Value, blk *Proc) object.Value {
	// A class receiver consults its singleton-method chain (def self.foo, and
	// inherited class methods) before the generic Class instance methods.
	if cls, ok := recv.(*RClass); ok {
		if m := lookupSMethod(cls, name); m != nil {
			return vm.invokeInPlace(m, recv, args, blk)
		}
	}
	c := vm.classOf(recv)
	// An object with a singleton class dispatches through it first (per-object
	// methods + extended modules), then its class chain (the singleton's super).
	if sc := vm.objSingleton(recv); sc != nil {
		c = sc
	}
	if m := lookupMethod(c, name); m != nil && !m.undefined {
		return vm.invokeInPlace(m, recv, args, blk)
	}
	// The arithmetic/comparison operators are a compiler fast path rather than
	// real methods, so send(:+, x), reduce(:+) and respond_to-style dispatch
	// route them through the same operator logic here.
	if len(args) == 1 {
		if op, ok := operatorOpcode(name); ok {
			return vm.binaryOp(op, recv, args[0])
		}
	}
	mm := lookupMethod(c, "method_missing")
	mmArgs := append([]object.Value{object.SymVal(name)}, args...)
	return vm.invoke(mm, recv, mmArgs, blk)
}

// callNative runs a Go-implemented method, converting a genuine Go runtime
// fault (e.g. a binding indexing past a missing argument) into a rescuable Ruby
// ArgumentError so arbitrary input from an embedding host — the wasm playground
// REPL, say — can never crash the process. Ruby-level raises (RubyError) and
// control-flow signals (break/return) pass through untouched, and a broken VM
// invariant still panics elsewhere so real bugs stay loud.
func (vm *VM) callNative(m *Method, self object.Value, args []object.Value, blk *Proc) (res object.Value) {
	// For an instance of a user subclass of a built-in value type, dispatch the
	// value type's own native methods (String#upcase, Array#map, …) against the
	// wrapped value, while Object/Kernel natives (object_id, freeze, ==, ivar
	// accessors) keep operating on the wrapper.
	if o, ok := self.(*RObject); ok && !object.IsNil(o.builtin) && vm.isBuiltinValueMethod(m, o.builtin) {
		self = o.builtin
	}
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(runtime.Error); ok {
				panic(RubyError{Class: "ArgumentError", Message: fmt.Sprintf("%s: %v", m.name, r)})
			}
			panic(r)
		}
	}()
	return m.native(vm, self, args, blk)
}

// isBuiltinValueMethod reports whether m is one of the wrapped value's own
// value-class methods (defined on its class or an included module such as
// Comparable/Enumerable), as opposed to a generic Object/BasicObject method
// (object_id, freeze, ==, ivar accessors, class) that must keep operating on the
// wrapper so identity and instance variables behave.
func (vm *VM) isBuiltinValueMethod(m *Method, builtin object.Value) bool {
	if m.owner == vm.cObject || m.owner == vm.cBasicObject {
		return false
	}
	return classIsA(vm.classOf(builtin), m.owner)
}

func (vm *VM) invoke(m *Method, self object.Value, args []object.Value, blk *Proc) object.Value {
	if m.native != nil {
		return vm.callNative(m, self, args, blk)
	}
	return vm.invokeBody(m, self, args, blk)
}

// invokeInPlace is invoke for the OpSend fast path (and the general-send
// fallback), where args is a live region of the caller's operand stack rather
// than a private copy. The interpreted / AOT / define_method bodies consume args
// synchronously (exec copies them into the callee's env slots before the call
// returns, so the caller's later reuse of the region is safe). A native body, by
// contrast, can retain its args slice (e.g. Array#push stores them), so only that
// case copies into a fresh slice.
func (vm *VM) invokeInPlace(m *Method, self object.Value, args []object.Value, blk *Proc) object.Value {
	if m.native != nil {
		// A native audited as non-retaining (Array/Hash#[], #[]=, Proc#call, …)
		// consumes args synchronously without keeping the slice, so it can read the
		// caller's live operand-stack region directly — eliding the defensive copy
		// that every other native still gets.
		if m.nonRetaining {
			return vm.callNative(m, self, args, blk)
		}
		cp := make([]object.Value, len(args))
		copy(cp, args)
		return vm.callNative(m, self, cp, blk)
	}
	return vm.invokeBody(m, self, args, blk)
}

// invokeBody dispatches the non-native method kinds shared by invoke and
// invokeInPlace: an AOT-compiled body, a define_method proc body, or an
// interpreted ISeq. Each copies its args into the callee's env slots
// synchronously (exec, or the compiled body's prologue) before returning, so it
// never retains the caller's slice — which is why the OpSend fast path may hand
// it the live operand-stack region.
func (vm *VM) invokeBody(m *Method, self object.Value, args []object.Value, blk *Proc) object.Value {
	if m.compiled != nil {
		return m.compiled(vm, self, args, blk)
	}
	if m.proc != nil {
		// A define_method body runs the block with self rebound to the receiver,
		// keeping its closure environment; the block passed to the method binds to
		// the body's &block param. The method's name/owner anchor `super` so a
		// define_method body may call super (matching MRI).
		return vm.callProcMethod(m.proc, self, args, blk, m.name, m.owner)
	}
	return vm.exec(m.iseq, self, args, m.owner, m.name, nil, blk, nil, m.lexScope)
}

// callBlock invokes a captured block with args. Block arity is lenient: extra
// arguments are dropped and missing ones default to nil (Ruby semantics).
// arityVal backs Proc#arity: a synthesized proc reports nativeArity; an ISeq
// block reports its parameter count.
func (p *Proc) arityVal() int {
	if p.native != nil {
		return p.nativeArity
	}
	// A *splat is always variadic: -(required + 1). Optional params are variadic
	// for a lambda too, but a non-lambda proc reports the positive required count.
	if p.iseq.SplatIndex >= 0 {
		return -(p.iseq.NumRequired + 1)
	}
	if p.iseq.NumRequired < len(p.iseq.Params) {
		if p.isLambda {
			return -(p.iseq.NumRequired + 1)
		}
		return p.iseq.NumRequired
	}
	return len(p.iseq.Params)
}

// toBlock coerces a &block-pass value into a *Proc: nil for nil, the Proc
// itself, else whatever its to_proc returns (which must be a Proc).
func (vm *VM) toBlock(v object.Value) *Proc {
	switch p := v.(type) {
	case object.Nil:
		return nil
	case *Proc:
		return p
	default:
		conv := vm.send(v, "to_proc", nil, nil)
		cp, ok := conv.(*Proc)
		if !ok {
			raise("TypeError", "no implicit conversion of %s into Proc", vm.classOf(v).name)
		}
		return cp
	}
}

// dispatchSend sends, routing a literal/passed block through sendCatchBreak so a
// `break` unwinds to the call site.
func (vm *VM) dispatchSend(recv object.Value, name string, args []object.Value, blk *Proc) object.Value {
	if blk != nil {
		return vm.sendCatchBreak(recv, name, args, blk)
	}
	return vm.send(recv, name, args, nil)
}

func (vm *VM) callBlock(p *Proc, args []object.Value) object.Value {
	return vm.callBlockSelf(p, p.self, args)
}

// callBlockSelf is callBlock with an explicit self, used by define_method to
// rebind the block's self to the method's receiver.
func (vm *VM) callBlockSelf(p *Proc, self object.Value, args []object.Value) object.Value {
	if p.native != nil {
		// Proc#call is nonRetaining, so the OpSend fast path hands us the caller's
		// live operand-stack region directly. An ISeq block body copies its args
		// into env slots synchronously (exec), but a native block body may retain
		// its args slice, so it gets a private copy before the region is reused.
		cp := make([]object.Value, len(args))
		copy(cp, args)
		return p.native(vm, cp)
	}
	return vm.exec(p.iseq, self, vm.bindBlockArgs(p, args), vm.blockDefinee(p), "", p.env, p.block, p, nil)
}

// blockDefinee returns the definee a block body runs with: the block's captured
// lexical scope (so bare constants resolve by the surrounding nesting), or Object
// at the top level.
func (vm *VM) blockDefinee(p *Proc) *RClass {
	if p.cref != nil {
		return p.cref
	}
	return vm.cObject
}

// callProcMethod invokes a proc that is the body of a define_method /
// define_singleton_method method `name` on `owner`. It is callBlockSelf plus
// threading the block passed to the method (blk) into the body, so a `&b` block
// param there binds it (its BlockSlot is wired by the compiler); the body's own
// captured block (p.block) still backs a bare `yield`. The method's name+owner
// anchor `super`, so the body may call an *explicit*-argument super against
// owner's superclass — matching MRI, where `define_method(:m) { … super(x) … }`
// resolves super to the next definition of m. A shallow copy of the proc carries
// the anchor (and a dmBody flag, so the frame rejects a bare implicit-argument
// super exactly as MRI does) while keeping the proc's closure env, cref, home,
// and yield-block.
func (vm *VM) callProcMethod(p *Proc, self object.Value, args []object.Value, blk *Proc, name string, owner *RClass) object.Value {
	if p.native != nil {
		// invokeInPlace passes the caller's live operand-stack region for a
		// define_method body (m.proc). An ISeq body copies into env slots (exec);
		// a native body may retain its args slice, so copy before handing it over.
		cp := make([]object.Value, len(args))
		copy(cp, args)
		return p.native(vm, cp)
	}
	body := p.block
	if blk != nil {
		body = blk
	}
	anchored := *p
	anchored.superName, anchored.superDefinee, anchored.superArgs = name, owner, args
	anchored.dmBody = true
	anchored.dmDirect = true // this frame IS the method body: its `return` is a return target
	return vm.exec(p.iseq, self, vm.bindBlockArgs(p, args), vm.blockDefinee(p), "", p.env, body, &anchored, nil)
}

// classEval runs a block as class_eval/module_eval would: self and the method
// definition target are both cls, so a `def` inside the block adds an instance
// method to cls.
func (vm *VM) classEval(cls *RClass, p *Proc, args []object.Value) object.Value {
	// class_eval/module_eval always receive a literal (ISeq) block, never a
	// synthesized native Proc, so no native fast path is needed here.
	return vm.exec(p.iseq, cls, vm.bindBlockArgs(p, args), cls, "", p.env, p.block, p, nil)
}

// classEvalString runs Ruby source as class_eval/module_eval would for the
// string form: the class is both self and the method-definition target, with a
// fresh local scope, so a top-level `def` in the source becomes an instance
// method of cls — the mechanism racc's runtime uses to graft do_parse/yyparse.
func (vm *VM) classEvalString(cls *RClass, src string) object.Value {
	iseq, cerr := parseCompileFn(src)
	if cerr != nil {
		return raise("SyntaxError", "%s", cerr.Error())
	}
	iseq.Name = "(eval)"
	return vm.exec(iseq, cls, nil, cls, "", nil, nil, nil, nil)
}

// bindBlockArgs maps call args onto a block's parameters, with the auto-splat a
// multi-parameter block applies to a single Array argument.
func (vm *VM) bindBlockArgs(p *Proc, args []object.Value) []object.Value {
	// A block with keyword params consumes a trailing keyword hash exactly as a
	// method does. Peel it off before positional shaping (auto-splat, padding,
	// arity truncation) and re-append it so exec's keyword binding still sees it.
	if len(p.iseq.KwNames) > 0 || p.iseq.KwRestSlot >= 0 {
		var kw object.Value
		if n := len(args); n > 0 {
			if _, ok := args[n-1].(*object.Hash); ok {
				kw, args = args[n-1], args[:n-1]
			}
		}
		shaped := vm.bindBlockPositionals(p, args)
		if !object.IsNil(kw) {
			shaped = append(shaped[:len(shaped):len(shaped)], kw)
		}
		return shaped
	}
	return vm.bindBlockPositionals(p, args)
}

// bindBlockPositionals shapes the positional arguments of a block call onto its
// positional parameters (auto-splat of a lone Array, padding, fixed-arity
// truncation).
func (vm *VM) bindBlockPositionals(p *Proc, args []object.Value) []object.Value {
	np := len(p.iseq.Params)
	if np > 1 && len(args) == 1 {
		if arr, ok := args[0].(*object.Array); ok {
			args = arr.Elems
		}
	}
	if p.iseq.SplatIndex >= 0 {
		// A *rest block param has variable arity: pad up to the required
		// positionals, then pass every argument through so exec's splat binding
		// collects the rest (instead of the fixed-arity truncation below).
		if len(args) < p.iseq.NumRequired {
			padded := make([]object.Value, p.iseq.NumRequired)
			copy(padded, args)
			for i := len(args); i < len(padded); i++ {
				padded[i] = object.NilV
			}
			args = padded
		}
		return args
	}
	if p.iseq.NumRequired < np {
		// Optional params: pad up to the required count and pass the rest through
		// (capped at np, dropping extras), leaving len(args) reflecting how many
		// were actually supplied so the default-filling prologue (OpArgGiven) fires
		// for the absent ones.
		n := len(args)
		if n < p.iseq.NumRequired {
			n = p.iseq.NumRequired
		}
		if n > np {
			n = np
		}
		out := make([]object.Value, n)
		for i := range out {
			if i < len(args) {
				out[i] = args[i]
			} else {
				out[i] = object.NilV
			}
		}
		return out
	}
	// Exact arity (the common case, e.g. a 1-param each block): the caller's args
	// already have the right shape, so hand them straight to exec (which copies
	// them into the block's env slots and never mutates the slice). This skips the
	// per-yield rebind allocation on the hot block path.
	if len(args) == np {
		return args
	}
	bargs := make([]object.Value, np)
	for i := range bargs {
		if i < len(args) {
			bargs[i] = args[i]
		} else {
			bargs[i] = object.NilV
		}
	}
	return bargs
}

func getIvar(self object.Value, name string) object.Value {
	if t := ivarTable(self); t != nil {
		if v, ok := t[name]; ok {
			return v
		}
	}
	return object.NilV
}

func setIvar(self object.Value, name string, v object.Value) {
	if t := ivarTable(self); t != nil {
		t[name] = v
	}
}

// ivarTable returns the instance-variable map backing self, or nil for values
// (Integer, String, …) that cannot hold ivars in this phase.
func ivarTable(self object.Value) map[string]object.Value {
	switch o := self.(type) {
	case *RObject:
		return o.ivars
	case *RClass:
		return o.ivars
	case *object.Main:
		return o.IvarTable()
	case *SinatraCtx:
		if o.ivars == nil {
			o.ivars = map[string]object.Value{}
		}
		return o.ivars
	case *ActionViewBase:
		if o.ivars == nil {
			o.ivars = map[string]object.Value{}
		}
		return o.ivars
	}
	return nil
}
