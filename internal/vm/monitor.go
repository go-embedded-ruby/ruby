// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerMonitor installs the Monitor class and MonitorMixin module (require
// "monitor"). Under this VM's single-threaded emulated GVL, a monitor's
// re-entrant lock is uncontended, so #synchronize (and the mon_* aliases) simply
// runs the block and MonitorMixin#new_cond yields a condition object whose
// wait/signal are no-ops. This is behaviourally correct for the single-thread
// case Puppet's load path exercises; real cross-thread blocking is a later round.
func (vm *VM) registerMonitor() {
	std := object.Kind[*RClass](vm.consts["StandardError"])

	// MonitorMixin: included into classes that want monitor semantics.
	mixin := newClass("MonitorMixin", nil)
	mixin.isModule = true
	vm.consts["MonitorMixin"] = mixin
	defMonitorMethods(mixin)

	// A ConditionVariable returned by new_cond; wait/signal/broadcast are no-ops
	// under the single-thread model (no other thread can hold the lock).
	cond := newClass("MonitorMixin::ConditionVariable", vm.cObject)
	mixin.consts["ConditionVariable"] = cond
	cond.smethods["new"] = &Method{name: "new", owner: cond,
		native: func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
			return &RObject{class: cond, ivars: map[string]object.Value{}}
		}}
	cond.define("wait", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return self })
	cond.define("wait_while", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return self })
	cond.define("wait_until", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return self })
	cond.define("signal", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return self })
	cond.define("broadcast", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return self })

	// Monitor is the standalone object form (Monitor.new.synchronize { ... }).
	mon := newClass("Monitor", vm.cObject)
	mon.includes = append(mon.includes, mixin)
	vm.consts["Monitor"] = mon
	mon.smethods["new"] = &Method{name: "new", owner: mon,
		native: func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
			return &RObject{class: mon, ivars: map[string]object.Value{}}
		}}

	mixin.consts["ConditionVariable"] = cond
	_ = std
}

// defMonitorMethods installs the monitor protocol on a module/class: synchronize
// (and the mon_* spellings) run the block; the enter/exit pair and new_cond are
// trivially satisfied under the single-thread GVL.
func defMonitorMethods(c *RClass) {
	sync := func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			return self
		}
		return vm.callBlock(blk, nil)
	}
	c.define("synchronize", sync)
	c.define("mon_synchronize", sync)
	noop := func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return self }
	c.define("mon_enter", noop)
	c.define("mon_exit", noop)
	c.define("mon_initialize", noop)
	c.define("new_cond", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		cond := object.Kind[*RClass](object.Kind[*RClass](vm.consts["MonitorMixin"]).consts["ConditionVariable"])
		return &RObject{class: cond, ivars: map[string]object.Value{}}
	})
}
