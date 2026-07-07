// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	rake "github.com/go-ruby-rake/rake"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerRake installs the Rake task-graph core (require "rake"): the Rake
// module and its Rake::Task / Rake::FileTask / Rake::Application / Rake::FileList
// classes, plus the top-level `task` / `file` / `namespace` / `desc` DSL. The
// deterministic, interpreter-independent half of Rake — the task DAG, the
// depth-first prerequisite-first invoke order, the invoke-once guard, circular
// detection, FileTask up-to-date timestamp logic, namespace/scope resolution and
// FileList filtering — lives in the github.com/go-ruby-rake/rake library; this
// file is the class + method wiring (see rake_bind.go for the wrapper types and
// the action-block seam).
//
// A task's action body (`task :foo do |t, args| … end`) is the rbgo seam: the
// library calls it INLINE, on the VM goroutine under the GVL, at the point the
// invoke walk reaches the task (after its prerequisites). The per-VM registry
// (Rake.application) and its filesystem seams (FileTask mtime via File.mtime,
// FileList glob via Dir.glob) are set up here.
func (vm *VM) registerRake() {
	vm.rakeApp = rake.NewApplication()
	vm.rakeApp.Stat = rakeStat
	vm.rakeApp.Glob = rakeGlob

	mod := newClass("Rake", nil)
	mod.isModule = true
	vm.consts["Rake"] = mod

	cTask := vm.rakeClass(mod, "Task", "Rake::Task", vm.cObject)
	vm.rakeClass(mod, "FileTask", "Rake::FileTask", cTask)
	cApp := vm.rakeClass(mod, "Application", "Rake::Application", vm.cObject)
	cFileList := vm.rakeClass(mod, "FileList", "Rake::FileList", vm.cObject)

	vm.registerRakeModule(mod)
	vm.registerRakeTask(cTask)
	vm.registerRakeApplication(cApp)
	vm.registerRakeFileList(cFileList)
	vm.registerRakeDSL()
}

// rakeClass creates a Rake::* class under super, records it flat (for classOf)
// and nests it under the Rake namespace by its simple name.
func (vm *VM) rakeClass(mod *RClass, simple, qualified string, super *RClass) *RClass {
	c := newClass(qualified, super)
	vm.consts[qualified] = c
	mod.consts[simple] = c
	return c
}

// registerRakeModule installs the Rake module singleton surface: Rake.application
// returns the per-VM task registry the top-level DSL populates.
func (vm *VM) registerRakeModule(mod *RClass) {
	mod.smethods["application"] = &Method{name: "application", owner: mod,
		native: func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
			return &RakeApplicationVal{app: vm.rakeApp}
		}}
}

// registerRakeTask installs Rake::Task (inherited by Rake::FileTask): the class
// lookup Rake::Task[name] / task_defined?, and the instance surface — name/to_s,
// invoke/execute (which drive the library's invoke walk, the action block running
// inline), the prerequisite readers, reenable/enhance/clear, the comment readers
// and the needed?/already_invoked?/scope/arg_names state. A failed invoke (a
// circular dependency or an unbuildable prerequisite) surfaces as a RuntimeError,
// as in MRI.
func (vm *VM) registerRakeTask(c *RClass) {
	c.smethods["[]"] = &Method{name: "[]", owner: c,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			if len(args) == 0 {
				raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
			}
			ti, err := vm.rakeApp.Get(rakeName(args[0]))
			if err != nil {
				raise("RuntimeError", "%s", err.Error())
			}
			return vm.wrapRakeTask(ti)
		}}
	c.smethods["task_defined?"] = &Method{name: "task_defined?", owner: c,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			if len(args) == 0 {
				raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
			}
			return object.Bool(vm.rakeApp.TaskDefined(rakeName(args[0])))
		}}

	itemOf := func(self object.Value) rake.TaskItem { return self.(*RakeTaskVal).t }
	baseOf := func(self object.Value) *rake.Task { return rakeBaseTask(itemOf(self)) }

	name := func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(itemOf(self).Name())
	}
	c.define("name", name)
	c.define("to_s", name)

	c.define("invoke", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if err := baseOf(self).Invoke(rakeStringArgs(args)...); err != nil {
			raise("RuntimeError", "%s", err.Error())
		}
		return self
	})
	c.define("execute", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		base := baseOf(self)
		// Execute's error return is discarded: a task body is a Ruby block whose only
		// failure mode is raising (a Go panic that unwinds as a Ruby exception), and
		// rakeAction never returns a Go error, so Execute here never reports one.
		_ = base.Execute(rake.NewArgs(base.ArgNames(), rakeStringArgs(args)))
		return self
	})
	c.define("prerequisites", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return thorStrArray(baseOf(self).Prerequisites())
	})
	c.define("prerequisite_tasks", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		pres, err := baseOf(self).PrerequisiteTasks()
		if err != nil {
			raise("RuntimeError", "%s", err.Error())
		}
		elems := make([]object.Value, len(pres))
		for i, p := range pres {
			elems[i] = vm.wrapRakeTask(p)
		}
		return object.NewArrayFromSlice(elems)
	})
	c.define("reenable", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		baseOf(self).Reenable()
		return self
	})
	c.define("enhance", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		var deps []string
		if len(args) > 0 {
			deps = rakeStrList(args[0])
		}
		baseOf(self).Enhance(deps, vm.rakeAction(blk))
		return self
	})
	c.define("clear", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		baseOf(self).Clear()
		return self
	})
	c.define("comment", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		if s := baseOf(self).Comment(); s != "" {
			return object.NewString(s)
		}
		return object.NilV
	})
	c.define("full_comment", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		if s := baseOf(self).FullComment(); s != "" {
			return object.NewString(s)
		}
		return object.NilV
	})
	c.define("needed?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(itemOf(self).Needed())
	})
	c.define("already_invoked?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(baseOf(self).AlreadyInvoked())
	})
	c.define("scope", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(baseOf(self).Scope().Path())
	})
	c.define("arg_names", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		out := make([]object.Value, 0)
		for _, n := range baseOf(self).ArgNames() {
			out = append(out, object.SymVal(n))
		}
		return object.NewArrayFromSlice(out)
	})
}

// registerRakeApplication installs Rake::Application: a task registry. new builds
// a fresh, filesystem-seamed manager; the instance surface exposes the task table
// (tasks / [] / lookup / task_defined?) and clear. Rake.application returns the
// per-VM registry the top-level DSL feeds; a .new registry is independent.
func (vm *VM) registerRakeApplication(c *RClass) {
	c.smethods["new"] = &Method{name: "new", owner: c,
		native: func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
			app := rake.NewApplication()
			app.Stat = rakeStat
			app.Glob = rakeGlob
			return &RakeApplicationVal{app: app}
		}}

	appOf := func(self object.Value) *rake.Application { return self.(*RakeApplicationVal).app }

	c.define("tasks", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		ts := appOf(self).Tasks()
		elems := make([]object.Value, len(ts))
		for i, t := range ts {
			elems[i] = vm.wrapRakeTask(t)
		}
		return object.NewArrayFromSlice(elems)
	})
	c.define("[]", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		ti, err := appOf(self).Get(rakeName(args[0]))
		if err != nil {
			raise("RuntimeError", "%s", err.Error())
		}
		return vm.wrapRakeTask(ti)
	})
	c.define("lookup", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		return vm.wrapRakeTask(appOf(self).Lookup(rakeName(args[0]), nil))
	})
	c.define("task_defined?", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		return object.Bool(appOf(self).TaskDefined(rakeName(args[0])))
	})
	c.define("clear", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		appOf(self).Clear()
		return self
	})
}

// registerRakeFileList installs Rake::FileList: lazy include/exclude file-name
// filtering. new seeds the default ignores; include/exclude accumulate patterns
// (globs expand through the Dir.glob seam), to_a/to_s resolve the list, ext/
// existing derive new lists, and resolve/clear_exclude expose the resolution knobs.
func (vm *VM) registerRakeFileList(c *RClass) {
	c.smethods["new"] = &Method{name: "new", owner: c,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			return &RakeFileListVal{fl: rake.NewFileList(rakeGlob, rakeStringArgs(args)...)}
		}}

	flOf := func(self object.Value) *rake.FileList { return self.(*RakeFileListVal).fl }

	c.define("to_a", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return thorStrArray(flOf(self).To())
	})
	c.define("to_s", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(flOf(self).String())
	})
	c.define("include", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		flOf(self).Include(rakeStringArgs(args)...)
		return self
	})
	c.define("exclude", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		flOf(self).Exclude(rakeStringArgs(args)...)
		return self
	})
	c.define("ext", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		ext := ""
		if len(args) > 0 {
			ext = rakeName(args[0])
		}
		return &RakeFileListVal{fl: flOf(self).Ext(ext)}
	})
	c.define("existing", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return &RakeFileListVal{fl: flOf(self).Existing(rakeExists)}
	})
	c.define("resolve", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		flOf(self).Resolve()
		return self
	})
	c.define("clear_exclude", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		flOf(self).ClearExclude()
		return self
	})
}

// registerRakeDSL installs the top-level Rake DSL (Rake::DSL, mixed into main):
// task / file define a plain or file task on the per-VM registry (the action block
// wired as the seam), namespace evaluates its block with the name pushed onto the
// scope, and desc records the description applied to the next defined task.
func (vm *VM) registerRakeDSL() {
	vm.cObject.define("task", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		name, argNames, deps := rakeResolveArgs(args)
		ti := vm.rakeApp.DefineTask(rake.PlainTask, name, argNames, deps, nil, vm.rakeAction(blk))
		return vm.wrapRakeTask(ti)
	})
	vm.cObject.define("file", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		name, argNames, deps := rakeResolveArgs(args)
		ti := vm.rakeApp.DefineTask(rake.FileKind, name, argNames, deps, nil, vm.rakeAction(blk))
		return vm.wrapRakeTask(ti)
	})
	vm.cObject.define("namespace", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		nm := ""
		if len(args) > 0 && !object.IsNil(args[0]) {
			nm = rakeName(args[0])
		}
		vm.rakeApp.InNamespace(nm, func(_ *rake.NameSpace) {
			if blk != nil {
				vm.callBlock(blk, nil)
			}
		})
		return object.NilV
	})
	vm.cObject.define("desc", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		text := rakeName(args[0])
		vm.rakeApp.Desc(text)
		return object.NewString(text)
	})
}
