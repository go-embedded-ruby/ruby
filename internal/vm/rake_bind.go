// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"os"
	"path/filepath"
	"time"

	rake "github.com/go-ruby-rake/rake"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin binding between rbgo's Ruby object graph (object.Value)
// and the interpreter-independent github.com/go-ruby-rake/rake library. The
// library owns the whole deterministic Rake core — the task DAG, the depth-first
// prerequisite-first invoke order, the invoke-once guard, circular detection,
// FileTask up-to-date timestamp logic, namespace/scope resolution and FileList
// filtering; rbgo wraps each library object as a Ruby object reporting the
// matching Rake::* class (see rake.go for the class + method registration) and
// converts values across the boundary here.
//
// The one seam this binding drives is a task's action body: the `do ... end`
// block of `task :foo do |t, args| … end`. It is captured as a *Proc and wrapped
// in a rake.Action (rakeAction); the library calls that Action INLINE, on the VM
// goroutine under the GVL, at the exact point Task#invoke reaches the task (after
// its prerequisites), so the block runs with the same serialization guarantees as
// any other Ruby code. The FileTask mtime seam (Application.Stat) and the
// FileList glob seam (Application.Glob / NewFileList's glob) are wired to the real
// filesystem (rakeStat / rakeGlob), matching MRI's File.mtime / Dir.glob.

// The wrapper types. Each holds a pointer into the library and reports the
// matching Rake::* class (see classOf); the methods registered in rake.go operate
// on the held value.

// RakeTaskVal wraps a rake.TaskItem — a *rake.Task (Rake::Task) or a *rake.FileTask
// (Rake::FileTask); classOf tells the two apart by the concrete type.
type RakeTaskVal struct{ t rake.TaskItem }

// RakeApplicationVal wraps a *rake.Application task registry (Rake::Application).
type RakeApplicationVal struct{ app *rake.Application }

// RakeFileListVal wraps a *rake.FileList (Rake::FileList).
type RakeFileListVal struct{ fl *rake.FileList }

func (v *RakeTaskVal) ToS() string     { return v.t.Name() }
func (v *RakeTaskVal) Inspect() string { return "#<Rake::Task " + v.t.Name() + ">" }
func (v *RakeTaskVal) Truthy() bool    { return true }

func (v *RakeApplicationVal) ToS() string     { return "#<Rake::Application>" }
func (v *RakeApplicationVal) Inspect() string { return "#<Rake::Application>" }
func (v *RakeApplicationVal) Truthy() bool    { return true }

func (v *RakeFileListVal) ToS() string     { return v.fl.String() }
func (v *RakeFileListVal) Inspect() string { return "#<Rake::FileList>" }
func (v *RakeFileListVal) Truthy() bool    { return true }

// rakeStat is the FileTask mtime seam (Application.Stat → File.mtime): it maps a
// file name to its modification time and whether it exists.
func rakeStat(name string) (time.Time, bool) {
	fi, err := os.Stat(name)
	if err != nil {
		return time.Time{}, false
	}
	return fi.ModTime(), true
}

// rakeGlob is the directory-glob seam (Application.Glob / FileList's glob →
// Dir.glob): it returns the names matching pattern, or nil on a bad pattern.
func rakeGlob(pattern string) []string {
	m, err := filepath.Glob(pattern)
	if err != nil {
		return nil
	}
	return m
}

// rakeExists is the File.exist? seam FileList#existing consults.
func rakeExists(name string) bool {
	_, err := os.Stat(name)
	return err == nil
}

// rakeBaseTask returns the embedded *rake.Task of a task item, so the binding can
// reach the exported Invoke/Execute/Enhance/Reenable/… methods that live on the
// base type (a FileTask embeds a *Task).
func rakeBaseTask(ti rake.TaskItem) *rake.Task {
	if ft, ok := ti.(*rake.FileTask); ok {
		return ft.Task
	}
	return ti.(*rake.Task)
}

// wrapRakeTask wraps a resolved task item as a Ruby value (nil → Ruby nil).
func (vm *VM) wrapRakeTask(ti rake.TaskItem) object.Value {
	if ti == nil {
		return object.NilV
	}
	return &RakeTaskVal{t: ti}
}

// rakeAction turns a task's Ruby action block into a rake.Action seam. The block
// is invoked INLINE (callBlock) when the library reaches the task in the invoke
// walk, passing the task and a Ruby Hash of its arguments (name → value). A Ruby
// exception raised in the block unwinds as a Go panic through the invoke walk
// (matching MRI, where an exception escaping a task body aborts the run), so the
// Action itself never returns an error. A nil block yields a nil Action (a task
// with no body).
func (vm *VM) rakeAction(blk *Proc) rake.Action {
	if blk == nil {
		return nil
	}
	return func(ti rake.TaskItem, a rake.Args) error {
		vm.callBlock(blk, []object.Value{vm.wrapRakeTask(ti), rakeArgsHash(a)})
		return nil
	}
}

// rakeArgsHash renders a task's invocation arguments as a Ruby Hash keyed by
// Symbol (the argument name) → String (the bound value), the shape a task body
// reads as `args[:name]`.
func rakeArgsHash(a rake.Args) object.Value {
	h := object.NewHash()
	names := a.Names()
	for _, n := range names {
		h.Set(object.SymVal(n), object.NewString(a.Lookup(n)))
	}
	return h
}

// rakeName coerces a task-name / symbol argument (a Symbol or String, or any
// value via #to_s) to its plain string, matching Rake's Symbol-or-String names.
func rakeName(v object.Value) string {
	switch x := v.(type) {
	case object.Symbol:
		return string(x)
	case *object.String:
		return x.Str()
	}
	return v.ToS()
}

// rakeStrList maps a dependency / argument-name value to a []string: an Array's
// elements by name, or a single scalar wrapped as one entry (Rake accepts either).
func rakeStrList(v object.Value) []string {
	if arr, ok := v.(*object.Array); ok {
		out := make([]string, len(arr.Elems))
		for i, e := range arr.Elems {
			out[i] = rakeName(e)
		}
		return out
	}
	if object.IsNil(v) {
		return nil
	}
	return []string{rakeName(v)}
}

// rakeStringArgs maps positional invocation arguments to their string values
// (task argument values are strings).
func rakeStringArgs(args []object.Value) []string {
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = rakeName(a)
	}
	return out
}

// rakeResolveArgs is the port of Rake's Task.resolve_args — it decodes the
// task/file DSL argument forms into (name, argNames, deps). The recognised forms
// mirror the gem:
//
//	task :t                       → name t,  no args, no deps
//	task :t, :a, :b               → name t,  args [a b], no deps
//	task :t => :d                 → name t,  no args, deps [d]      (sole Hash arg)
//	task :t => [:d1, :d2]         → name t,  no args, deps [d1 d2]
//	task :t, [:a] => :d           → name t,  args [a], deps [d]     (name + Hash)
//
// A trailing Hash carries the dependencies; it must hold exactly one pair
// (name → deps or argNames → deps), else it is a Task Argument Error. With no
// arguments at all it raises ArgumentError.
func rakeResolveArgs(args []object.Value) (name string, argNames, deps []string) {
	if len(args) == 0 {
		raise("ArgumentError", "wrong number of arguments (given 0, expected 1+)")
	}
	last := args[len(args)-1]
	if h, ok := last.(*object.Hash); ok {
		rest := args[:len(args)-1]
		if len(h.Keys) != 1 {
			raise("RuntimeError", "Task Argument Error")
		}
		key := h.Keys[0]
		val, _ := h.Get(key)
		if len(rest) == 0 {
			// task :t => deps — the key is the task name.
			return rakeName(key), nil, rakeStrList(val)
		}
		// task :t, [args] => deps — first positional is the name, the Hash key is
		// the argument-name list, the value the dependencies.
		return rakeName(rest[0]), rakeStrList(key), rakeStrList(val)
	}
	// No dependency Hash: first positional is the name, the rest — flattened, as
	// MRI's resolve_args_without_dependencies flattens — are the argument names.
	var flat []string
	for _, a := range args[1:] {
		flat = append(flat, rakeStrList(a)...)
	}
	return rakeName(args[0]), flat, nil
}
