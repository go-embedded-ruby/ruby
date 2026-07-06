// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	activejob "github.com/go-ruby-activejob/activejob"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerActiveJob installs the ActiveJob surface (require "active_job"), backed
// by the pure-Go engine of github.com/go-ruby-activejob/activejob:
//
//   - ActiveJob::Base, the class every `class MyJob < ActiveJob::Base` subclasses:
//     the class-level DSL (queue_as, retry_on / discard_on, the before/after/around
//     _enqueue and _perform callbacks, queue_adapter=), the enqueue/run entry
//     points (perform_later / perform_now / set) and the instance readers
//     (job_id / queue_name / arguments / executions / priority / serialize);
//   - ActiveJob::Arguments, the wire-faithful _aj_* argument serializer
//     (serialize / deserialize), including the GlobalID conversion seam;
//   - ActiveJob.perform_all_later for bulk enqueue.
//
// The job model, retry/discard rules, the byte-exact argument wire format and the
// queue adapters live in the library; this file is the class and method wiring,
// and activejob_bind.go holds the Ruby #perform seam, the per-class job-class
// dispatch and the Ruby <-> activejob value conversions. Jobs run INLINE on the
// VM goroutine under the GVL (the default inline adapter), so perform_later runs
// synchronously; the :test adapter records jobs for perform_enqueued_jobs.
func (vm *VM) registerActiveJob() {
	mod := newClass("ActiveJob", nil)
	mod.isModule = true
	vm.consts["ActiveJob"] = mod
	vm.ajArgs = activejob.NewArguments()
	vm.ajArgs.ToGlobalID = vm.ajToGlobalID()

	vm.registerActiveJobErrors(mod)
	vm.registerActiveJobBase(mod)
	vm.registerActiveJobConfigured(mod)
	vm.registerActiveJobArguments(mod)
	vm.registerActiveJobModuleMethods(mod)
}

// registerActiveJobErrors installs the ActiveJob error tree: SerializationError <
// ArgumentError (a bad argument on enqueue) and DeserializationError <
// StandardError (a bad payload on load), mirroring the gem.
func (vm *VM) registerActiveJobErrors(mod *RClass) {
	arg := vm.consts["ArgumentError"].(*RClass)
	std := vm.consts["StandardError"].(*RClass)
	ser := newClass("ActiveJob::SerializationError", arg)
	mod.consts["SerializationError"] = ser
	vm.consts["ActiveJob::SerializationError"] = ser
	des := newClass("ActiveJob::DeserializationError", std)
	mod.consts["DeserializationError"] = des
	vm.consts["ActiveJob::DeserializationError"] = des
}

// registerActiveJobBase installs ActiveJob::Base: its instance surface (#perform
// stub, #perform_now / #perform_later and the readers) and the inherited class
// DSL a subclass configures itself with.
func (vm *VM) registerActiveJobBase(mod *RClass) {
	base := newClass("ActiveJob::Base", vm.cObject)
	mod.consts["Base"] = base
	vm.consts["ActiveJob::Base"] = base

	vm.registerActiveJobInstance(base)
	vm.registerActiveJobDSL(base)
	vm.registerActiveJobClassRun(base)
	vm.registerActiveJobAdapter(base)
}

// registerActiveJobInstance installs the ActiveJob::Base instance methods: the
// initializer (which builds the backing library job from the arguments), the
// #perform stub a subclass overrides, the inline run entry points and the readers.
func (vm *VM) registerActiveJobInstance(base *RClass) {
	base.define("initialize", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o := self.(*RObject)
		b := vm.ajBase(vm.classOf(o))
		goArgs := make([]any, len(args))
		for i, a := range args {
			goArgs[i] = ajFromRuby(a)
		}
		vm.ajBind(o, b.New(goArgs...))
		return object.NilV
	})
	base.define("perform", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return raise("NotImplementedError", "%s", vm.classOf(self.(*RObject)).name+"#perform is not implemented")
	})
	base.define("perform_now", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.ajRunNow(self.(*RObject))
	})
	base.define("perform_later", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.ajRunLater(self.(*RObject))
	})

	reader := func(name string, fn func(*activejob.Job) object.Value) {
		base.define(name, func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
			return fn(vm.ajJobOf[self.(*RObject)])
		})
	}
	reader("job_id", func(j *activejob.Job) object.Value { return object.NewString(j.JobID) })
	reader("queue_name", func(j *activejob.Job) object.Value { return object.NewString(j.QueueName) })
	reader("executions", func(j *activejob.Job) object.Value { return object.IntValue(int64(j.Executions)) })
	reader("priority", func(j *activejob.Job) object.Value {
		if j.Priority == nil {
			return object.NilV
		}
		return object.IntValue(int64(*j.Priority))
	})
	reader("arguments", func(j *activejob.Job) object.Value {
		el := make([]object.Value, len(j.Arguments))
		for i, a := range j.Arguments {
			el[i] = vm.ajArgToRuby(a)
		}
		return object.NewArrayFromSlice(el)
	})
	base.define("serialize", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		obj, err := vm.ajJobOf[self.(*RObject)].Serialize()
		if err != nil {
			vm.ajRaise(err)
		}
		return vm.ajWireToRuby(obj)
	})
}

// registerActiveJobDSL installs the class-level declaration DSL inherited by every
// ActiveJob::Base subclass: queue_as, retry_on / discard_on and the enqueue /
// perform callbacks. Each records onto the subclass's library job class.
func (vm *VM) registerActiveJobDSL(base *RClass) {
	sm := func(name string, fn NativeFn) {
		base.smethods[name] = &Method{name: name, owner: base, native: fn}
	}

	sm("queue_as", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		b := vm.ajBase(self.(*RClass))
		if blk != nil {
			b.QueueAsFunc(func(j *activejob.Job) string {
				return ajQueueName(vm.callBlockSelf(blk, vm.ajInstOf[j], nil))
			})
			return object.NilV
		}
		if len(args) == 0 {
			raise("ArgumentError", "queue_as requires a name or a block")
		}
		b.QueueAs(ajQueueName(args[0]))
		return object.NilV
	})
	sm("queue_name", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(vm.ajBase(self.(*RClass)).New().QueueName)
	})
	sm("retry_on", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		vm.ajBase(self.(*RClass)).RetryOn(vm.ajErrorMatcherArg(args), ajRetryOptions(args[1:]))
		return object.NilV
	})
	sm("discard_on", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		vm.ajBase(self.(*RClass)).DiscardOn(vm.ajErrorMatcherArg(args), activejob.DiscardOptions{})
		return object.NilV
	})

	callback := func(name string, reg func(*activejob.Base, activejob.CallbackFunc) *activejob.Base) {
		sm(name, func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
			if blk == nil {
				raise("ArgumentError", "%s requires a block", name)
			}
			reg(vm.ajBase(self.(*RClass)), func(j *activejob.Job) error {
				vm.callBlockSelf(blk, vm.ajInstOf[j], nil)
				return nil
			})
			return object.NilV
		})
	}
	callback("before_perform", (*activejob.Base).BeforePerform)
	callback("after_perform", (*activejob.Base).AfterPerform)
	callback("before_enqueue", (*activejob.Base).BeforeEnqueue)
	callback("after_enqueue", (*activejob.Base).AfterEnqueue)

	around := func(name string, reg func(*activejob.Base, activejob.AroundFunc) *activejob.Base) {
		sm(name, func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
			if blk == nil {
				raise("ArgumentError", "%s requires a block", name)
			}
			reg(vm.ajBase(self.(*RClass)), func(j *activejob.Job, next func() error) error {
				inst := vm.ajInstOf[j]
				var innerErr error
				np := &Proc{native: func(_ *VM, _ []object.Value) object.Value {
					innerErr = next()
					return object.NilV
				}}
				vm.callBlockSelf(blk, inst, []object.Value{inst, np})
				return innerErr
			})
			return object.NilV
		})
	}
	around("around_perform", (*activejob.Base).AroundPerform)
	around("around_enqueue", (*activejob.Base).AroundEnqueue)
}

// ajErrorMatcherArg reads the leading exception-class argument of retry_on /
// discard_on and builds the matcher for it, raising on a missing or non-class arg.
func (vm *VM) ajErrorMatcherArg(args []object.Value) activejob.ErrorMatcher {
	if len(args) == 0 {
		raise("ArgumentError", "an exception class is required")
	}
	cls, ok := args[0].(*RClass)
	if !ok {
		raise("TypeError", "expected an exception class")
	}
	return vm.ajMatcher(cls)
}

// registerActiveJobClassRun installs the class-level enqueue / run entry points a
// subclass inherits: MyJob.perform_now / MyJob.perform_later (build an instance
// from the arguments and run/enqueue it) and MyJob.set (a configured single run).
func (vm *VM) registerActiveJobClassRun(base *RClass) {
	sm := func(name string, fn NativeFn) {
		base.smethods[name] = &Method{name: name, owner: base, native: fn}
	}
	sm("perform_now", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.ajRunNow(vm.send(self, "new", args, nil).(*RObject))
	})
	sm("perform_later", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.ajRunLater(vm.send(self, "new", args, nil).(*RObject))
	})
	sm("set", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return &ActiveJobConfigured{cls: self.(*RClass), opts: ajSetOptions(args)}
	})
}

// registerActiveJobAdapter installs the queue-adapter class methods: queue_adapter
// / queue_adapter= (selecting :inline or :test) and the :test-adapter inspection
// helpers enqueued_jobs / perform_enqueued_jobs.
func (vm *VM) registerActiveJobAdapter(base *RClass) {
	sm := func(name string, fn NativeFn) {
		base.smethods[name] = &Method{name: name, owner: base, native: fn}
	}
	sm("queue_adapter=", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "queue_adapter= requires an adapter name")
		}
		vm.ajSetAdapter(self.(*RClass), args[0].ToS())
		return args[0]
	})
	sm("queue_adapter", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		if vm.ajTestAdapters[self.(*RClass)] != nil {
			return object.Symbol("test")
		}
		return object.Symbol("inline")
	})
	sm("enqueued_jobs", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		ta := vm.ajRequireTestAdapter(self.(*RClass))
		el := make([]object.Value, len(ta.Enqueued))
		for i, j := range ta.Enqueued {
			el[i] = vm.ajInstOf[j]
		}
		return object.NewArrayFromSlice(el)
	})
	sm("perform_enqueued_jobs", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		ta := vm.ajRequireTestAdapter(self.(*RClass))
		pending := ta.Enqueued
		ta.Enqueued = nil
		for _, j := range pending {
			vm.ajRunNow(vm.ajInstOf[j])
			ta.Performed = append(ta.Performed, j)
		}
		return object.IntValue(int64(len(pending)))
	})
}

// ajSetAdapter installs the named queue adapter on a job class: :inline performs
// jobs synchronously, :test records them (for perform_enqueued_jobs). Any other
// name raises, since only inline execution is GVL-safe on the VM goroutine.
func (vm *VM) ajSetAdapter(cls *RClass, name string) {
	b := vm.ajBase(cls)
	switch name {
	case "inline":
		b.WithAdapter(activejob.InlineAdapter{})
		delete(vm.ajTestAdapters, cls)
	case "test":
		if vm.ajTestAdapters == nil {
			vm.ajTestAdapters = map[*RClass]*activejob.TestAdapter{}
		}
		ta := &activejob.TestAdapter{}
		b.WithAdapter(ta)
		vm.ajTestAdapters[cls] = ta
	default:
		raise("ArgumentError", "unknown queue adapter %q (only :inline and :test are supported)", name)
	}
}

// ajRequireTestAdapter returns a class's :test adapter, raising when the class is
// not using it (the inspection helpers only apply to the :test adapter).
func (vm *VM) ajRequireTestAdapter(cls *RClass) *activejob.TestAdapter {
	ta := vm.ajTestAdapters[cls]
	if ta == nil {
		raise("ArgumentError", "the :test queue adapter is required")
	}
	return ta
}

// registerActiveJobConfigured installs ActiveJob::ConfiguredJob, the object
// MyJob.set(...) returns: perform_later / perform_now build an instance, apply the
// stored single-run options and enqueue/run it.
func (vm *VM) registerActiveJobConfigured(mod *RClass) {
	cls := newClass("ActiveJob::ConfiguredJob", vm.cObject)
	mod.consts["ConfiguredJob"] = cls
	vm.consts["ActiveJob::ConfiguredJob"] = cls

	run := func(name string, drive func(vm *VM, inst *RObject) object.Value) {
		cls.define(name, func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
			c := self.(*ActiveJobConfigured)
			inst := vm.send(c.cls, "new", args, nil).(*RObject)
			vm.ajJobOf[inst].Set(c.opts)
			return drive(vm, inst)
		})
	}
	run("perform_later", func(vm *VM, inst *RObject) object.Value { return vm.ajRunLater(inst) })
	run("perform_now", func(vm *VM, inst *RObject) object.Value { return vm.ajRunNow(inst) })
}

// registerActiveJobArguments installs ActiveJob::Arguments.serialize / deserialize,
// the wire-faithful _aj_* argument coder, over the module-level serializer (whose
// GlobalID seam is wired in registerActiveJob).
func (vm *VM) registerActiveJobArguments(mod *RClass) {
	args := newClass("ActiveJob::Arguments", nil)
	args.isModule = true
	mod.consts["Arguments"] = args
	vm.consts["ActiveJob::Arguments"] = args

	args.smethods["serialize"] = &Method{name: "serialize", owner: args, native: func(vm *VM, _ object.Value, a []object.Value, _ *Proc) object.Value {
		arr := ajArgArray(a, "serialize")
		in := make([]any, len(arr.Elems))
		for i, e := range arr.Elems {
			in[i] = ajFromRuby(e)
		}
		ser, err := vm.ajArgs.Serialize(in)
		if err != nil {
			raise("ActiveJob::SerializationError", "%s", err.Error())
		}
		out := make([]object.Value, len(ser))
		for i, v := range ser {
			out[i] = vm.ajWireToRuby(v)
		}
		return object.NewArrayFromSlice(out)
	}}
	args.smethods["deserialize"] = &Method{name: "deserialize", owner: args, native: func(vm *VM, _ object.Value, a []object.Value, _ *Proc) object.Value {
		arr := ajArgArray(a, "deserialize")
		raw := make([]any, len(arr.Elems))
		for i, e := range arr.Elems {
			raw[i] = ajRawFromRuby(e)
		}
		res, err := vm.ajArgs.Deserialize(raw)
		if err != nil {
			raise("ActiveJob::DeserializationError", "%s", err.Error())
		}
		out := make([]object.Value, len(res))
		for i, v := range res {
			out[i] = vm.ajArgToRuby(v)
		}
		return object.NewArrayFromSlice(out)
	}}
}

// ajArgArray reads the single Array argument of Arguments.serialize / deserialize,
// raising ArgumentError when it is missing or not an Array.
func ajArgArray(args []object.Value, meth string) *object.Array {
	if len(args) == 0 {
		raise("ArgumentError", "ActiveJob::Arguments.%s requires an array", meth)
	}
	arr, ok := args[0].(*object.Array)
	if !ok {
		raise("TypeError", "ActiveJob::Arguments.%s expects an array", meth)
	}
	return arr
}

// registerActiveJobModuleMethods installs the ActiveJob module methods:
// perform_all_later enqueues several job instances at once.
func (vm *VM) registerActiveJobModuleMethods(mod *RClass) {
	mod.smethods["perform_all_later"] = &Method{name: "perform_all_later", owner: mod, native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		for _, a := range args {
			if vm.ajJobFor(a) == nil {
				raise("ArgumentError", "perform_all_later expects ActiveJob instances")
			}
			vm.ajRunLater(a.(*RObject))
		}
		return object.NilV
	}}
}
