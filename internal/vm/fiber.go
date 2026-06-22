package vm

import "github.com/go-embedded-ruby/ruby/internal/object"

// Fiber is a cooperative coroutine backed by a goroutine. resume and Fiber.yield
// hand control back and forth over a pair of channels, so exactly one side runs
// at a time (a strict handoff — no real concurrency, and the channel operations
// give the race detector the happens-before it needs).
type Fiber struct {
	blk      *Proc
	resumeCh chan []object.Value
	yieldCh  chan fiberMsg
	started  bool
	alive    bool
}

type fiberMsg struct {
	val  object.Value
	done bool
	err  *RubyError
}

func (f *Fiber) ToS() string     { return "#<Fiber>" }
func (f *Fiber) Inspect() string { return f.ToS() }
func (f *Fiber) Truthy() bool    { return true }

func (vm *VM) registerFiber() {
	cFiber := newClass("Fiber", vm.cObject)
	vm.consts["Fiber"] = cFiber
	if _, ok := vm.consts["FiberError"]; !ok {
		fe := newClass("FiberError", vm.consts["StandardError"].(*RClass))
		vm.consts["FiberError"] = fe
	}

	cFiber.smethods["new"] = &Method{name: "new", owner: cFiber, native: func(_ *VM, _ object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("ArgumentError", "tried to create a Fiber without a block")
		}
		return &Fiber{blk: blk}
	}}
	cFiber.smethods["yield"] = &Method{name: "yield", owner: cFiber, native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.fiberYield(args)
	}}
	cFiber.define("resume", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.fiberResume(self.(*Fiber), args)
	})
	cFiber.define("alive?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		f := self.(*Fiber)
		return object.Bool(!f.started || f.alive)
	})
}

// fiberResume transfers control into the fiber with args and returns the value
// it yields or finally produces. A second resume of a finished fiber raises.
func (vm *VM) fiberResume(f *Fiber, args []object.Value) object.Value {
	if f.started && !f.alive {
		raise("FiberError", "dead fiber called")
	}
	if !f.started {
		f.started, f.alive = true, true
		f.resumeCh = make(chan []object.Value)
		f.yieldCh = make(chan fiberMsg)
		go func() {
			in := <-f.resumeCh
			msg := fiberMsg{done: true}
			func() {
				defer func() {
					if r := recover(); r != nil {
						if re, ok := r.(RubyError); ok {
							msg.err = &re
						} else {
							e := RubyError{Class: "FiberError", Message: "fiber terminated abnormally"}
							msg.err = &e
						}
					}
				}()
				msg.val = vm.callBlock(f.blk, in)
			}()
			f.alive = false
			f.yieldCh <- msg
		}()
	}
	prev := vm.currentFiber
	vm.currentFiber = f
	f.resumeCh <- args
	msg := <-f.yieldCh
	vm.currentFiber = prev
	if msg.err != nil {
		panic(*msg.err)
	}
	return msg.val
}

// fiberYield suspends the running fiber, handing val back to resume; it returns
// the arguments of the next resume.
func (vm *VM) fiberYield(args []object.Value) object.Value {
	f := vm.currentFiber
	if f == nil {
		raise("FiberError", "can't yield from root fiber")
	}
	f.yieldCh <- fiberMsg{val: yieldValue(args)}
	return yieldValue(<-f.resumeCh)
}

// yieldValue packs resume/yield arguments into the single value Ruby exposes: the
// bare value for one argument, nil for none, an array for several.
func yieldValue(args []object.Value) object.Value {
	switch len(args) {
	case 0:
		return object.NilV
	case 1:
		return args[0]
	default:
		return &object.Array{Elems: append([]object.Value{}, args...)}
	}
}
