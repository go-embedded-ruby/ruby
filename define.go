package ruby

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/go-embedded-ruby/ruby/internal/object"
	"github.com/go-embedded-ruby/ruby/internal/vm"
)

// Func is a method written in Go. It returns the method's result, converted to
// Ruby as Value describes, or an error, which raises: an *Error raises its
// class (NewError("ArgumentError", "...")), an *Error that came from Ruby —
// out of Block.Call, say — raises that same exception again, and any other
// error raises a RuntimeError with its message.
type Func func(c *Call) (any, error)

// Call is one call of a Func.
type Call struct {
	Interp *Interp
	// Self is the receiver: the module or class for a singleton method, the
	// instance for an instance method.
	Self Value
	// Args are the positional arguments.
	Args []Value
	// Kwargs are the keyword arguments, by name; nil when there are none.
	// Ruby passes keywords to a method written in Go as a trailing Hash: a
	// final argument that is a non-empty Hash whose keys are all Symbols is
	// read as the keywords, unless the call site said it passed none.
	Kwargs map[string]Value
	// Block is the block the method was called with, or nil.
	Block *Block
}

// Block is a block passed to a Func.
type Block struct {
	in *Interp
	p  *vm.Proc
}

// Call calls the block with args converted to Ruby, and returns its value. A
// Ruby exception raised in the block comes back as an *Error; returning that
// error from the Func re-raises the same exception. A break, a throw or a
// non-local return in the block unwinds through the Go code that called it, so
// run nothing after Call that must not be skipped other than with defer.
func (b *Block) Call(args ...any) (Value, error) {
	rargs, err := b.in.toRubyAll(args)
	if err != nil {
		return Value{}, err
	}
	r, rerr := b.in.vm.Protect(func() object.Value { return b.in.vm.CallBlock(b.p, rargs) })
	if rerr != nil {
		return Value{}, b.in.newError(rerr, "")
	}
	return b.in.wrap(r), nil
}

// Arity is the block's Proc#arity.
func (b *Block) Arity() int { return b.in.vm.ProcArity(b.p) }

// Module is a Ruby module or class defined, or found, by the host.
type Module struct {
	in *Interp
	c  *vm.RClass
}

// Name is the module's qualified name, e.g. "Stats::Normal".
func (m *Module) Name() string { return m.c.Name() }

// Value is the module itself as a Ruby value.
func (m *Module) Value() Value { return m.in.wrap(m.c) }

// DefineModule returns the top-level module name, creating it if needed. name
// may be qualified ("Stats::Tests"); every part but the last must exist or is
// created as a module.
func (in *Interp) DefineModule(name string) (*Module, error) {
	return in.define(nil, name, nil, true)
}

// DefineClass returns the top-level class name, creating it as a subclass of
// super (nil: Object) if needed. name may be qualified as for DefineModule.
func (in *Interp) DefineClass(name string, super *Module) (*Module, error) {
	return in.define(nil, name, super, false)
}

// DefineModule returns the module name inside m, creating it if needed.
func (m *Module) DefineModule(name string) (*Module, error) {
	return m.in.define(m.c, name, nil, true)
}

// DefineClass returns the class name inside m, creating it as a subclass of
// super (nil: Object) if needed.
func (m *Module) DefineClass(name string, super *Module) (*Module, error) {
	return m.in.define(m.c, name, super, false)
}

func (in *Interp) define(parent *vm.RClass, name string, super *Module, module bool) (*Module, error) {
	parts := strings.Split(name, "::")
	for i, p := range parts {
		if !isConstName(p) {
			return nil, fmt.Errorf("ruby: %q is not a constant name", name)
		}
		last := i == len(parts)-1
		var (
			c  *vm.RClass
			ok bool
		)
		switch {
		case !last || module:
			c, ok = in.vm.DefineModule(parent, p)
		default:
			var sc *vm.RClass
			if super != nil {
				sc = super.c
			}
			c, ok = in.vm.DefineClass(parent, p, sc)
		}
		if !ok {
			what := "a module"
			if last && !module {
				what = "a class"
			}
			return nil, fmt.Errorf("ruby: %s is already defined and is not %s", strings.Join(parts[:i+1], "::"), what)
		}
		parent = c
	}
	return &Module{in: in, c: parent}, nil
}

func isConstName(s string) bool {
	if s == "" || s[0] < 'A' || s[0] > 'Z' {
		return false
	}
	for _, r := range s {
		if r != '_' && !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') && !(r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

// DefineMethod defines the instance method name, written in Go.
func (m *Module) DefineMethod(name string, fn Func) { m.c.DefineNative(name, m.in.native(fn)) }

// DefineSingletonMethod defines the singleton method name — Stats.mean, or a
// class method such as Normal.new — written in Go.
func (m *Module) DefineSingletonMethod(name string, fn Func) {
	m.c.DefineSingletonNative(name, m.in.native(fn))
}

// SetConst defines the constant name inside m, converted to Ruby.
func (m *Module) SetConst(name string, v any) error {
	if !isConstName(name) {
		return fmt.Errorf("ruby: %q is not a constant name", name)
	}
	rv, err := m.in.toRuby(v)
	if err != nil {
		return err
	}
	m.in.vm.SetConstIn(m.c, name, rv)
	return nil
}

// Bind makes every Go value of sample's dynamic type an instance of m when it
// crosses into Ruby, so the methods defined on m apply to it, and Value.Go
// gives the Go value back. Bind(&Normal{}) binds *Normal.
func (m *Module) Bind(sample any) { m.in.types[reflect.TypeOf(sample)] = m.c }

// Wrap makes v an instance of m, whatever its type.
func (m *Module) Wrap(v any) Value { return m.in.wrap(m.in.vm.WrapGo(v, m.c)) }

// native adapts a Func to the VM's calling convention.
func (in *Interp) native(fn Func) vm.NativeFn {
	return func(v *vm.VM, self object.Value, args []object.Value, blk *vm.Proc) object.Value {
		noKW := v.NativeNoKW()
		c := &Call{Interp: in, Self: in.wrap(self)}
		if n := len(args); n > 0 && !noKW {
			if h, ok := args[n-1].(*object.Hash); ok && h.Len() > 0 && symbolKeys(h) {
				c.Kwargs = make(map[string]Value, h.Len())
				for _, k := range h.Keys {
					e, _ := h.Get(k)
					c.Kwargs[string(k.(object.Symbol))] = in.wrap(e)
				}
				args = args[:n-1]
			}
		}
		c.Args = make([]Value, len(args))
		for i, a := range args {
			c.Args[i] = in.wrap(a)
		}
		if blk != nil {
			c.Block = &Block{in: in, p: blk}
		}
		res, err := fn(c)
		if err != nil {
			in.raise(err)
		}
		r, err := in.toRuby(res)
		if err != nil {
			vm.Raise("TypeError", err.Error())
		}
		return r
	}
}

func symbolKeys(h *object.Hash) bool {
	for _, k := range h.Keys {
		if _, ok := k.(object.Symbol); !ok {
			return false
		}
	}
	return true
}
