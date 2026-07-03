package vm

import (
	"fmt"
	"runtime"
	"strings"

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
	if o, ok := object.KindOK[*RObject](v); ok {
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
	{
		__sw108 := v
		switch {
		case object.IsKind[*RObject](__sw108):
			t := object.Kind[*RObject](__sw108)
			_ = t
			return vm.singletonClass(t), true
		case object.IsKind[*RClass](__sw108):
			t := object.Kind[*RClass](__sw108)
			_ = t
			return nil, false
		}
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
	{
		__sw109 := v
		switch {
		case object.IsInt(__sw109) || object.IsKind[*object.Bignum](__sw109) || object.IsFloat(__sw109) || object.IsKind[object.Symbol](__sw109) || object.IsBool(__sw109) || object.IsNilObj(__sw109):
			return false
		}
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
	if c, ok := object.KindOK[*RClass](target); ok {
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
	return object.NilVal()
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
	{
		__sw110 := v
		switch {
		case object.IsKind[*RObject](__sw110):
			x := object.Kind[*RObject](__sw110)
			_ = x
			return x.class
		case object.IsKind[*RClass](__sw110):
			x := object.Kind[*RClass](__sw110)
			_ = x
			if x.isModule {
				return vm.cModule
			}
			return vm.cClass
		case object.IsInt(__sw110):
			x := object.AsInteger(__sw110)
			_ = x
			return vm.cInteger
		case object.IsKind[*object.Bignum](__sw110):
			x := object.Kind[*object.Bignum](__sw110)
			_ = x
			return vm.cInteger
		case object.IsFloat(__sw110):
			x := object.AsFloatV(__sw110)
			_ = x
			return vm.cFloat
		case object.IsKind[*object.Complex](__sw110):
			x := object.Kind[*object.Complex](__sw110)
			_ = x
			return vm.cComplex
		case object.IsKind[*object.Rational](__sw110):
			x := object.Kind[*object.Rational](__sw110)
			_ = x
			return vm.cRational
		case object.IsKind[*NDArray](__sw110):
			x := object.Kind[*NDArray](__sw110)
			_ = x
			return vm.cNDArray
		case object.IsKind[*Image](__sw110):
			x := object.Kind[*Image](__sw110)
			_ = x
			return vm.cImage
		case object.IsKind[*StringScanner](__sw110):
			x := object.Kind[*StringScanner](__sw110)
			_ = x
			return vm.cStringScanner
		case object.IsKind[*OptionParser](__sw110):
			x := object.Kind[*OptionParser](__sw110)
			_ = x
			return vm.cOptionParser
		case object.IsKind[*URI](__sw110):
			x := object.Kind[*URI](__sw110)
			_ = x
			return x.cls
		case object.IsKind[*CSVRow](__sw110):
			x := object.Kind[*CSVRow](__sw110)
			_ = x
			return vm.cCSVRow
		case object.IsKind[*CSVTable](__sw110):
			x := object.Kind[*CSVTable](__sw110)
			_ = x
			return vm.cCSVTable
		case object.IsKind[*RackRequest](__sw110):
			x := object.Kind[*RackRequest](__sw110)
			_ = x
			return x.cls
		case object.IsKind[*RackResponse](__sw110):
			x := object.Kind[*RackResponse](__sw110)
			_ = x
			return x.cls
		case object.IsKind[*SinatraCtx](__sw110):
			x := object.Kind[*SinatraCtx](__sw110)
			_ = x
			return x.cls
		case object.IsKind[*SinatraSettings](__sw110):
			x := object.Kind[*SinatraSettings](__sw110)
			_ = x
			return x.cls
		case object.IsKind[*REXMLDocument](__sw110):
			x := object.Kind[*REXMLDocument](__sw110)
			_ = x
			return vm.cREXMLDocument
		case object.IsKind[*REXMLElement](__sw110):
			x := object.Kind[*REXMLElement](__sw110)
			_ = x
			return vm.cREXMLElement
		case object.IsKind[*REXMLElements](__sw110):
			x := object.Kind[*REXMLElements](__sw110)
			_ = x
			return vm.cREXMLElements
		case object.IsKind[*REXMLAttributes](__sw110):
			x := object.Kind[*REXMLAttributes](__sw110)
			_ = x
			return vm.cREXMLAttributes
		case object.IsKind[*REXMLText](__sw110):
			x := object.Kind[*REXMLText](__sw110)
			_ = x
			return vm.cREXMLText
		case object.IsKind[*REXMLComment](__sw110):
			x := object.Kind[*REXMLComment](__sw110)
			_ = x
			return vm.cREXMLComment
		case object.IsKind[*REXMLCData](__sw110):
			x := object.Kind[*REXMLCData](__sw110)
			_ = x
			return vm.cREXMLCData
		case object.IsKind[*REXMLInstruction](__sw110):
			x := object.Kind[*REXMLInstruction](__sw110)
			_ = x
			return vm.cREXMLInstruction
		case object.IsKind[*REXMLDocType](__sw110):
			x := object.Kind[*REXMLDocType](__sw110)
			_ = x
			return vm.cREXMLDocType
		case object.IsKind[*csvSink](__sw110):
			x := object.Kind[*csvSink](__sw110)
			_ = x
			return vm.cCSV
		case object.IsKind[*Logger](__sw110):
			x := object.Kind[*Logger](__sw110)
			_ = x
			return vm.cLogger
		case object.IsKind[*LoggerFormatter](__sw110):
			x := object.Kind[*LoggerFormatter](__sw110)
			_ = x
			return vm.cLoggerFormatter
		case object.IsKind[*GetoptLong](__sw110):
			x := object.Kind[*GetoptLong](__sw110)
			_ = x
			return vm.cGetoptLong
		case object.IsKind[*Set](__sw110):
			x := object.Kind[*Set](__sw110)
			_ = x
			return vm.cSet
		case object.IsKind[*PStore](__sw110):
			x := object.Kind[*PStore](__sw110)
			_ = x
			return vm.cPStore
		case object.IsKind[*Jbuilder](__sw110):
			x := object.Kind[*Jbuilder](__sw110)
			_ = x
			return object.Kind[*RClass](vm.consts["Jbuilder"])
		case object.IsKind[*DryType](__sw110):
			x := object.Kind[*DryType](__sw110)
			_ = x
			return object.Kind[*RClass](vm.consts["Dry::Types::Type"])
		case object.IsKind[*DryResult](__sw110):
			x := object.Kind[*DryResult](__sw110)
			_ = x
			return object.Kind[*RClass](vm.consts["Dry::Types::Result"])
		case object.IsKind[*DryStruct](__sw110):
			x := object.Kind[*DryStruct](__sw110)
			_ = x
			return x.cls
		case object.IsKind[*DrySchema](__sw110):
			x := object.Kind[*DrySchema](__sw110)
			_ = x
			return object.Kind[*RClass](vm.consts["Dry::Schema::Params"])
		case object.IsKind[*DrySchemaBuilder](__sw110):
			x := object.Kind[*DrySchemaBuilder](__sw110)
			_ = x
			return object.Kind[*RClass](vm.consts["Dry::Schema::DSL"])
		case object.IsKind[*DryKey](__sw110):
			x := object.Kind[*DryKey](__sw110)
			_ = x
			return object.Kind[*RClass](vm.consts["Dry::Schema::Key"])
		case object.IsKind[*DryContract](__sw110):
			x := object.Kind[*DryContract](__sw110)
			_ = x
			return x.cls
		case object.IsKind[*DryValidationResult](__sw110):
			x := object.Kind[*DryValidationResult](__sw110)
			_ = x
			return object.Kind[*RClass](vm.consts["Dry::Validation::Result"])
		case object.IsKind[*DryRuleCtx](__sw110):
			x := object.Kind[*DryRuleCtx](__sw110)
			_ = x
			return object.Kind[*RClass](vm.consts["Dry::Validation::Rule"])
		case object.IsKind[*DryRuleKey](__sw110):
			x := object.Kind[*DryRuleKey](__sw110)
			_ = x
			return object.Kind[*RClass](vm.consts["Dry::Validation::RuleKey"])
		case object.IsKind[*OAuth2Client](__sw110):
			x := object.Kind[*OAuth2Client](__sw110)
			_ = x
			return object.Kind[*RClass](vm.consts["OAuth2::Client"])
		case object.IsKind[*OAuth2Strategy](__sw110):
			x := object.Kind[*OAuth2Strategy](__sw110)
			_ = x
			return object.Kind[*RClass](vm.consts[x.className])
		case object.IsKind[*OAuth2AccessToken](__sw110):
			x := object.Kind[*OAuth2AccessToken](__sw110)
			_ = x
			return object.Kind[*RClass](vm.consts["OAuth2::AccessToken"])
		case object.IsKind[*OAuth2Response](__sw110):
			x := object.Kind[*OAuth2Response](__sw110)
			_ = x
			return object.Kind[*RClass](vm.consts["OAuth2::Response"])
		case object.IsKind[*OAuth2Request](__sw110):
			x := object.Kind[*OAuth2Request](__sw110)
			_ = x
			return object.Kind[*RClass](vm.consts["OAuth2::Request"])
		case object.IsKind[*KramdownDoc](__sw110):
			x := object.Kind[*KramdownDoc](__sw110)
			_ = x
			return object.Kind[*RClass](vm.consts["Kramdown::Document"])
		case object.IsKind[*RQRCode](__sw110):
			x := object.Kind[*RQRCode](__sw110)
			_ = x
			return object.Kind[*RClass](vm.consts["RQRCode::QRCode"])
		case object.IsKind[*XmlMarkup](__sw110):
			x := object.Kind[*XmlMarkup](__sw110)
			_ = x
			return object.Kind[*RClass](vm.consts["Builder::XmlMarkup"])
		case object.IsKind[*SQLite3Database](__sw110):
			x := object.Kind[*SQLite3Database](__sw110)
			_ = x
			return object.Kind[*RClass](vm.consts["SQLite3::Database"])
		case object.IsKind[*SQLite3Statement](__sw110):
			x := object.Kind[*SQLite3Statement](__sw110)
			_ = x
			return object.Kind[*RClass](vm.consts["SQLite3::Statement"])
		case object.IsKind[*RedisObj](__sw110):
			x := object.Kind[*RedisObj](__sw110)
			_ = x
			return x.cls
		case object.IsKind[*RedisBatch](__sw110):
			x := object.Kind[*RedisBatch](__sw110)
			_ = x
			return x.cls
		case object.IsKind[*PGConnObj](__sw110):
			x := object.Kind[*PGConnObj](__sw110)
			_ = x
			return x.cls
		case object.IsKind[*PGResultObj](__sw110):
			x := object.Kind[*PGResultObj](__sw110)
			_ = x
			return x.cls
		case object.IsKind[*SequelDBObj](__sw110):
			x := object.Kind[*SequelDBObj](__sw110)
			_ = x
			return x.cls
		case object.IsKind[*SequelDatasetObj](__sw110):
			x := object.Kind[*SequelDatasetObj](__sw110)
			_ = x
			return x.cls
		case object.IsKind[*SequelSchemaObj](__sw110):
			x := object.Kind[*SequelSchemaObj](__sw110)
			_ = x
			return x.cls
		case object.IsKind[*NokogiriDocument](__sw110):
			x := object.Kind[*NokogiriDocument](__sw110)
			_ = x
			return object.Kind[*RClass](vm.consts["Nokogiri::XML::Document"])
		case object.IsKind[*NokogiriNode](__sw110):
			x := object.Kind[*NokogiriNode](__sw110)
			_ = x
			return object.Kind[*RClass](vm.consts["Nokogiri::XML::Node"])
		case object.IsKind[*NokogiriNodeSet](__sw110):
			x := object.Kind[*NokogiriNodeSet](__sw110)
			_ = x
			return object.Kind[*RClass](vm.consts["Nokogiri::XML::NodeSet"])
		case object.IsKind[*NokogiriXSLTStylesheet](__sw110):
			x := object.Kind[*NokogiriXSLTStylesheet](__sw110)
			_ = x
			return object.Kind[*RClass](vm.consts["Nokogiri::XSLT::Stylesheet"])
		case object.IsKind[*RuboCopRunner](__sw110):
			x := object.Kind[*RuboCopRunner](__sw110)
			_ = x
			return object.Kind[*RClass](vm.consts["RuboCop::Runner"])
		case object.IsKind[*RuboCopConfig](__sw110):
			x := object.Kind[*RuboCopConfig](__sw110)
			_ = x
			return object.Kind[*RClass](vm.consts["RuboCop::Config"])
		case object.IsKind[*RuboCopOffense](__sw110):
			x := object.Kind[*RuboCopOffense](__sw110)
			_ = x
			return object.Kind[*RClass](vm.consts["RuboCop::Cop::Offense"])
		case object.IsKind[*RuboCopLocation](__sw110):
			x := object.Kind[*RuboCopLocation](__sw110)
			_ = x
			return object.Kind[*RClass](vm.consts["RuboCop::Cop::Offense::Location"])
		case object.IsKind[*GrapeRouter](__sw110):
			x := object.Kind[*GrapeRouter](__sw110)
			_ = x
			return object.Kind[*RClass](vm.consts["Grape::Router"])
		case object.IsKind[*GrapeRoute](__sw110):
			x := object.Kind[*GrapeRoute](__sw110)
			_ = x
			return object.Kind[*RClass](vm.consts["Grape::Router::Route"])
		case object.IsKind[*GrapeMatch](__sw110):
			x := object.Kind[*GrapeMatch](__sw110)
			_ = x
			return object.Kind[*RClass](vm.consts["Grape::Router::Match"])
		case object.IsKind[*GrapeValidator](__sw110):
			x := object.Kind[*GrapeValidator](__sw110)
			_ = x
			return object.Kind[*RClass](vm.consts["Grape::Validator"])
		case object.IsKind[*GrapeParamsBuilder](__sw110):
			x := object.Kind[*GrapeParamsBuilder](__sw110)
			_ = x
			return object.Kind[*RClass](vm.consts["Grape::Validations::ParamsScope::DSL"])
		case object.IsKind[*GrapeFormatter](__sw110):
			x := object.Kind[*GrapeFormatter](__sw110)
			_ = x
			return object.Kind[*RClass](vm.consts["Grape::Formatter"])
		case object.IsKind[*ActiveRecordModel](__sw110):
			x := object.Kind[*ActiveRecordModel](__sw110)
			_ = x
			return object.Kind[*RClass](vm.consts["ActiveRecord::Model"])
		case object.IsKind[*ActiveRecordModelBuilder](__sw110):
			x := object.Kind[*ActiveRecordModelBuilder](__sw110)
			_ = x
			return object.Kind[*RClass](vm.consts["ActiveRecord::Model::DSL"])
		case object.IsKind[*ActiveRecordRelation](__sw110):
			x := object.Kind[*ActiveRecordRelation](__sw110)
			_ = x
			return object.Kind[*RClass](vm.consts["ActiveRecord::Relation"])
		case object.IsKind[*ActiveRecordRecord](__sw110):
			x := object.Kind[*ActiveRecordRecord](__sw110)
			_ = x
			return object.Kind[*RClass](vm.consts["ActiveRecord::Record"])
		case object.IsKind[*ActiveRecordErrors](__sw110):
			x := object.Kind[*ActiveRecordErrors](__sw110)
			_ = x
			return object.Kind[*RClass](vm.consts["ActiveRecord::Errors"])
		case object.IsKind[*ActiveRecordSchemaDSL](__sw110):
			x := object.Kind[*ActiveRecordSchemaDSL](__sw110)
			_ = x
			return object.Kind[*RClass](vm.consts["ActiveRecord::Schema::Definition"])
		case object.IsKind[*ActiveRecordTableDSL](__sw110):
			x := object.Kind[*ActiveRecordTableDSL](__sw110)
			_ = x
			return object.Kind[*RClass](vm.consts["ActiveRecord::Schema::TableDefinition"])
		case object.IsKind[*RSpecMatcher](__sw110):
			x := object.Kind[*RSpecMatcher](__sw110)
			_ = x
			return object.Kind[*RClass](vm.consts["RSpec::Matchers::BuiltIn::BaseMatcher"])
		case object.IsKind[*RSpecExpectation](__sw110):
			x := object.Kind[*RSpecExpectation](__sw110)
			_ = x
			return object.Kind[*RClass](vm.consts["RSpec::Expectations::ExpectationTarget"])
		case object.IsKind[*PrettyPrint](__sw110):
			x := object.Kind[*PrettyPrint](__sw110)
			_ = x
			return vm.cPrettyPrint
		case object.IsKind[*PrettyPrintGroup](__sw110):
			x := object.Kind[*PrettyPrintGroup](__sw110)
			_ = x
			return object.Kind[*RClass](vm.consts["PrettyPrint::Group"])
		case object.IsKind[*SingleLine](__sw110):
			x := object.Kind[*SingleLine](__sw110)
			_ = x
			return object.Kind[*RClass](vm.consts["PrettyPrint::SingleLine"])
		case object.IsKind[*IPAddr](__sw110):
			x := object.Kind[*IPAddr](__sw110)
			_ = x
			return vm.cIPAddr
		case object.IsKind[*Matrix](__sw110):
			x := object.Kind[*Matrix](__sw110)
			_ = x
			return vm.cMatrix
		case object.IsKind[*Vector](__sw110):
			x := object.Kind[*Vector](__sw110)
			_ = x
			return vm.cVector
		case object.IsKind[*SpellChecker](__sw110):
			x := object.Kind[*SpellChecker](__sw110)
			_ = x
			return vm.cSpellChecker
		case object.IsKind[*Time](__sw110):
			x := object.Kind[*Time](__sw110)
			_ = x
			return vm.cTime
		case object.IsKind[*Timezone](__sw110):
			x := object.Kind[*Timezone](__sw110)
			_ = x
			return object.Kind[*RClass](vm.consts["TZInfo::Timezone"])
		case object.IsKind[*TimezonePeriod](__sw110):
			x := object.Kind[*TimezonePeriod](__sw110)
			_ = x
			return object.Kind[*RClass](vm.consts["TZInfo::TimezonePeriod"])
		case object.IsKind[*TimezoneOffset](__sw110):
			x := object.Kind[*TimezoneOffset](__sw110)
			_ = x
			return object.Kind[*RClass](vm.consts["TZInfo::TimezoneOffset"])
		case object.IsKind[*Country](__sw110):
			x := object.Kind[*Country](__sw110)
			_ = x
			return object.Kind[*RClass](vm.consts["TZInfo::Country"])
		case object.IsKind[*Money](__sw110):
			x := object.Kind[*Money](__sw110)
			_ = x
			return object.Kind[*RClass](vm.consts["Money"])
		case object.IsKind[*Currency](__sw110):
			x := object.Kind[*Currency](__sw110)
			_ = x
			return object.Kind[*RClass](vm.consts["Money::Currency"])
		case object.IsKind[*AddressableURI](__sw110):
			x := object.Kind[*AddressableURI](__sw110)
			_ = x
			return object.Kind[*RClass](vm.consts["Addressable::URI"])
		case object.IsKind[*AddressableTemplate](__sw110):
			x := object.Kind[*AddressableTemplate](__sw110)
			_ = x
			return object.Kind[*RClass](vm.consts["Addressable::Template"])
		case object.IsKind[*PublicSuffixDomain](__sw110):
			x := object.Kind[*PublicSuffixDomain](__sw110)
			_ = x
			return object.Kind[*RClass](vm.consts["PublicSuffix::Domain"])
		case object.IsKind[*MIMEType](__sw110):
			x := object.Kind[*MIMEType](__sw110)
			_ = x
			return object.Kind[*RClass](vm.consts["MIME::Type"])
		case object.IsKind[*MailMessage](__sw110):
			x := object.Kind[*MailMessage](__sw110)
			_ = x
			return object.Kind[*RClass](vm.consts["Mail::Message"])
		case object.IsKind[*MailBody](__sw110):
			x := object.Kind[*MailBody](__sw110)
			_ = x
			return object.Kind[*RClass](vm.consts["Mail::Body"])
		case object.IsKind[*MailField](__sw110):
			x := object.Kind[*MailField](__sw110)
			_ = x
			return object.Kind[*RClass](vm.consts["Mail::Field"])
		case object.IsKind[*FileStat](__sw110):
			x := object.Kind[*FileStat](__sw110)
			_ = x
			return vm.cFileStat
		case object.IsKind[*BigDecimal](__sw110):
			x := object.Kind[*BigDecimal](__sw110)
			_ = x
			return vm.cBigDecimal
		case object.IsKind[*Tms](__sw110):
			x := object.Kind[*Tms](__sw110)
			_ = x
			return vm.cBenchmarkTms
		case object.IsKind[*benchReport](__sw110):
			x := object.Kind[*benchReport](__sw110)
			_ = x
			return vm.cBenchmarkReport
		case object.IsKind[*benchJob](__sw110):
			x := object.Kind[*benchJob](__sw110)
			_ = x
			return vm.cBenchmarkJob
		case object.IsKind[*Date](__sw110):
			x := object.Kind[*Date](__sw110)
			_ = x
			if x.d.IsDateTime() {
				return vm.cDateTime
			}
			return vm.cDate
		case object.IsKind[*Bag](__sw110):
			x := object.Kind[*Bag](__sw110)
			_ = x
			return vm.cBag
		case object.IsKind[*object.String](__sw110):
			x := object.Kind[*object.String](__sw110)
			_ = x
			return vm.cString
		case object.IsKind[object.Symbol](__sw110):
			x := object.Kind[object.Symbol](__sw110)
			_ = x
			return vm.cSymbol
		case object.IsKind[*object.Array](__sw110):
			x := object.Kind[*object.Array](__sw110)
			_ = x
			return vm.cArray
		case object.IsKind[*object.Hash](__sw110):
			x := object.Kind[*object.Hash](__sw110)
			_ = x
			return vm.cHash
		case object.IsKind[*object.Range](__sw110):
			x := object.Kind[*object.Range](__sw110)
			_ = x
			return vm.cRange
		case object.IsKind[*Proc](__sw110):
			x := object.Kind[*Proc](__sw110)
			_ = x
			return vm.cProc
		case object.IsKind[*BoundMethod](__sw110):
			x := object.Kind[*BoundMethod](__sw110)
			_ = x
			return vm.cMethod
		case object.IsKind[*UnboundMethod](__sw110):
			x := object.Kind[*UnboundMethod](__sw110)
			_ = x
			return object.Kind[*RClass](vm.consts["UnboundMethod"])
		case object.IsKind[*Enumerator](__sw110):
			x := object.Kind[*Enumerator](__sw110)
			_ = x
			return vm.cEnumerator
		case object.IsKind[*yielder](__sw110):
			x := object.Kind[*yielder](__sw110)
			_ = x
			return vm.cYielder
		case object.IsKind[*encodingObj](__sw110):
			x := object.Kind[*encodingObj](__sw110)
			_ = x
			return vm.cEncoding
		case object.IsKind[*LazyEnum](__sw110):
			x := object.Kind[*LazyEnum](__sw110)
			_ = x
			return vm.cLazy
		case object.IsKind[*RandomObj](__sw110):
			x := object.Kind[*RandomObj](__sw110)
			_ = x
			return object.Kind[*RClass](vm.consts["Random"])
		case object.IsKind[*Fiber](__sw110):
			x := object.Kind[*Fiber](__sw110)
			_ = x
			return object.Kind[*RClass](vm.consts["Fiber"])
		case object.IsKind[*RThread](__sw110):
			x := object.Kind[*RThread](__sw110)
			_ = x
			return object.Kind[*RClass](vm.consts["Thread"])
		case object.IsKind[*RMutex](__sw110):
			x := object.Kind[*RMutex](__sw110)
			_ = x
			return object.Kind[*RClass](vm.consts["Mutex"])
		case object.IsKind[*RQueue](__sw110):
			x := object.Kind[*RQueue](__sw110)
			_ = x
			return object.Kind[*RClass](vm.consts["Queue"])
		case object.IsKind[*IOObj](__sw110):
			x := object.Kind[*IOObj](__sw110)
			_ = x
			return x.cls
		case object.IsKind[*opensslDigest](__sw110):
			x := object.Kind[*opensslDigest](__sw110)
			_ = x
			return x.cls
		case object.IsKind[*DigestObj](__sw110):
			x := object.Kind[*DigestObj](__sw110)
			_ = x
			return x.cls
		case object.IsKind[*BCryptPassword](__sw110):
			x := object.Kind[*BCryptPassword](__sw110)
			_ = x
			return x.cls
		case object.IsKind[*Binding](__sw110):
			x := object.Kind[*Binding](__sw110)
			_ = x
			return object.Kind[*RClass](vm.consts["Binding"])
		case object.IsKind[*Regexp](__sw110):
			x := object.Kind[*Regexp](__sw110)
			_ = x
			return vm.cRegexp
		case object.IsKind[*MatchData](__sw110):
			x := object.Kind[*MatchData](__sw110)
			_ = x
			return vm.cMatchData
		case object.IsBool(__sw110):
			x := object.AsBoolV(__sw110)
			_ = x
			if x {
				return vm.cTrueClass
			}
			return vm.cFalseClass
		case object.IsNilObj(__sw110):
			x := object.NilObj()
			_ = x
			return vm.cNilClass
		case object.IsKind[*object.Main](__sw110):
			x := object.Kind[*object.Main](__sw110)
			_ = x
			return vm.cObject
		}
	}
	return nil // unreachable for the closed set of value types
}

// send is the dispatch core (our objc_msgSend): find the method on the
// receiver's class chain, else route to method_missing (Object's default
// raises NoMethodError). blk is the block passed to the call (nil if none).
// findMethod resolves name on recv exactly as send dispatch would (class
// singleton methods, then a per-object singleton class, then the class chain),
// returning nil when nothing but method_missing would handle it. It drives
// respond_to? so a singleton or class method is seen.
func (vm *VM) findMethod(recv object.Value, name string) *Method {
	if cls, ok := object.KindOK[*RClass](recv); ok {
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
	if cls, ok := object.KindOK[*RClass](recv); ok {
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
	if o, ok := object.KindOK[*RObject](self); ok && !object.IsNil(o.builtin) && vm.isBuiltinValueMethod(m, o.builtin) {
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
	{
		__sw111 := v
		switch {
		case object.IsNilObj(__sw111):
			p := object.NilObj()
			_ = p
			return nil
		case object.IsKind[*Proc](__sw111):
			p := object.Kind[*Proc](__sw111)
			_ = p
			return p
		default:
			p := __sw111
			_ = p
			conv := vm.send(v, "to_proc", nil, nil)
			cp, ok := object.KindOK[*Proc](conv)
			if !ok {
				raise("TypeError", "no implicit conversion of %s into Proc", vm.classOf(v).name)
			}
			return cp
		}
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
	return vm.exec(p.iseq, object.Wrap(cls), vm.bindBlockArgs(p, args), cls, "", p.env, p.block, p, nil)
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
	return vm.exec(iseq, object.Wrap(cls), nil, cls, "", nil, nil, nil, nil)
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
			if _, ok := object.KindOK[*object.Hash](args[n-1]); ok {
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
		if arr, ok := object.KindOK[*object.Array](args[0]); ok {
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
				padded[i] = object.NilVal()
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
				out[i] = object.NilVal()
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
			bargs[i] = object.NilVal()
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
	return object.NilVal()
}

func setIvar(self object.Value, name string, v object.Value) {
	if t := ivarTable(self); t != nil {
		t[name] = v
	}
}

// ivarTable returns the instance-variable map backing self, or nil for values
// (Integer, String, …) that cannot hold ivars in this phase.
func ivarTable(self object.Value) map[string]object.Value {
	{
		__sw112 := self
		switch {
		case object.IsKind[*RObject](__sw112):
			o := object.Kind[*RObject](__sw112)
			_ = o
			return o.ivars
		case object.IsKind[*RClass](__sw112):
			o := object.Kind[*RClass](__sw112)
			_ = o
			return o.ivars
		case object.IsKind[*object.Main](__sw112):
			o := object.Kind[*object.Main](__sw112)
			_ = o
			return o.IvarTable()
		}
	}
	return nil
}
