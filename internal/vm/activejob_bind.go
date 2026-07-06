// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"
	"time"

	activejob "github.com/go-ruby-activejob/activejob"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the binding between rbgo's Ruby object graph and the pure-Go
// ActiveJob engine of github.com/go-ruby-activejob/activejob. The library owns
// the job model (queue name, priority, retry/discard rules, callbacks), the
// wire-faithful ActiveJob::Arguments serialization and the queue adapters; rbgo
// supplies the interpreter-dependent seams it documents:
//
//   - PerformFunc: a job's #perform body is Ruby, so the library's perform seam
//     is a closure that sends #perform to the Ruby job instance with the decoded
//     arguments, mapping a raised Ruby exception back into an error the library's
//     retry_on / discard_on machinery classifies (ajPerform / activeJobError).
//     It runs INLINE on the VM goroutine under the GVL — no goroutines — so the
//     default adapter is the inline adapter and perform_later runs synchronously.
//   - the class dispatch: each `class … < ActiveJob::Base` subclass maps to one
//     library *activejob.Base, built lazily and keyed by its *RClass (ajBase),
//     the equivalent of the library Registry's name -> Base dispatch.
//   - the GlobalID conversion seam on Arguments: a Ruby object that responds to
//     #to_global_id serializes to its gid URI; with no locator configured a
//     gid payload deserializes to its plain URI String (the plain-type round-trip).

// activeJobError carries a raised Ruby exception's class through the Go error the
// perform seam returns, so retry_on / discard_on can match it by its Ruby class
// (ajMatcher walks the class ancestry) and ajRaise can re-raise it faithfully.
type activeJobError struct {
	cls *RClass
	msg string
}

func (e *activeJobError) Error() string { return e.msg }

// ActiveJobConfigured is the object `MyJob.set(queue:, wait:, priority:)` returns
// — a job class plus a single-enqueue option set — responding to perform_later /
// perform_now, mirroring ActiveJob's ConfiguredJob.
type ActiveJobConfigured struct {
	cls  *RClass
	opts activejob.SetOptions
}

func (c *ActiveJobConfigured) ToS() string     { return "#<ActiveJob::ConfiguredJob " + c.cls.name + ">" }
func (c *ActiveJobConfigured) Inspect() string { return c.ToS() }
func (c *ActiveJobConfigured) Truthy() bool    { return true }

// --- per-class library job class + instance binding -------------------------

// ajBase returns the library *activejob.Base backing an ActiveJob::Base subclass,
// building it on first use with the shared Ruby #perform seam, the inline adapter
// (so perform_later runs synchronously) and the GlobalID serialization seam.
func (vm *VM) ajBase(cls *RClass) *activejob.Base {
	if vm.ajBases == nil {
		vm.ajBases = map[*RClass]*activejob.Base{}
	}
	if b, ok := vm.ajBases[cls]; ok {
		return b
	}
	b := activejob.NewBase(cls.name).WithPerform(vm.ajPerform()).WithAdapter(activejob.InlineAdapter{})
	b.Args.ToGlobalID = vm.ajToGlobalID()
	vm.ajBases[cls] = b
	return b
}

// ajBind associates a Ruby job instance with its library *Job in both directions:
// ajJobOf backs the instance readers, ajInstOf lets the perform / drain paths
// find the Ruby instance for a given library job.
func (vm *VM) ajBind(inst *RObject, job *activejob.Job) {
	if vm.ajJobOf == nil {
		vm.ajJobOf = map[*RObject]*activejob.Job{}
	}
	if vm.ajInstOf == nil {
		vm.ajInstOf = map[*activejob.Job]*RObject{}
	}
	vm.ajJobOf[inst] = job
	vm.ajInstOf[job] = inst
}

// ajJobFor returns the library *Job bound to a Ruby value, or nil when the value
// is not a bound ActiveJob instance.
func (vm *VM) ajJobFor(v object.Value) *activejob.Job {
	o, ok := v.(*RObject)
	if !ok {
		return nil
	}
	return vm.ajJobOf[o]
}

// --- perform seam -----------------------------------------------------------

// ajPerform builds the library perform seam: it sends #perform to the Ruby job
// instance currently on the run stack (pushed by ajRunNow / drain) with the
// decoded arguments, recording the #perform return value and turning a raised
// Ruby exception into an activeJobError the retry machinery classifies.
func (vm *VM) ajPerform() activejob.PerformFunc {
	return func(_ string, rawArgs []any) error {
		inst := vm.ajStack[len(vm.ajStack)-1]
		args := make([]object.Value, len(rawArgs))
		for i, a := range rawArgs {
			args[i] = vm.ajArgToRuby(a)
		}
		res, cls, msg, failed := vm.ajInvokePerform(inst, args)
		if failed {
			return &activeJobError{cls: cls, msg: msg}
		}
		vm.ajLastResult = res
		return nil
	}
}

// ajInvokePerform sends #perform to inst, reporting the return value or (on a
// raise) the raised Ruby class and message. A non-Ruby Go panic propagates.
func (vm *VM) ajInvokePerform(inst *RObject, args []object.Value) (res object.Value, cls *RClass, msg string, failed bool) {
	defer func() {
		if r := recover(); r != nil {
			e := r.(RubyError)
			cls, msg, failed = vm.minitestRaisedClass(e), e.Message, true
		}
	}()
	res = vm.send(inst, "perform", args, nil)
	return res, nil, "", false
}

// ajRunNow runs a job's #perform inline (perform_now): it puts the instance on the
// run stack for the seam, drives PerformNow (which increments the execution count
// and applies retry_on / discard_on to any error) and re-raises a surviving error
// as its Ruby exception. It returns the #perform return value.
func (vm *VM) ajRunNow(inst *RObject) object.Value {
	job := vm.ajJobOf[inst]
	vm.ajStack = append(vm.ajStack, inst)
	defer func() { vm.ajStack = vm.ajStack[:len(vm.ajStack)-1] }()
	if err := job.PerformNow(); err != nil {
		vm.ajRaise(err)
	}
	return vm.ajLastResult
}

// ajRunLater enqueues a job through its adapter (perform_later): with the default
// inline adapter this performs it synchronously; with the :test adapter it only
// records it. It re-raises a serialization or perform error and returns the job.
func (vm *VM) ajRunLater(inst *RObject) object.Value {
	job := vm.ajJobOf[inst]
	vm.ajStack = append(vm.ajStack, inst)
	defer func() { vm.ajStack = vm.ajStack[:len(vm.ajStack)-1] }()
	if err := job.PerformLater(); err != nil {
		vm.ajRaise(err)
	}
	return inst
}

// ajRaise re-raises an error the library returned as the faithful Ruby exception:
// a perform failure carries its original Ruby class through activeJobError; the
// only other error the enqueue path yields is an argument serialization failure.
func (vm *VM) ajRaise(err error) {
	var aj *activeJobError
	if errors.As(err, &aj) {
		raise(aj.cls.name, "%s", aj.Error())
	}
	raise("ActiveJob::SerializationError", "%s", err.Error())
}

// ajMatcher builds an error matcher for retry_on / discard_on: it matches a
// perform failure whose raised Ruby class is, or descends from, the given class.
func (vm *VM) ajMatcher(target *RClass) activejob.ErrorMatcher {
	return func(err error) bool {
		var aj *activeJobError
		return errors.As(err, &aj) && aj.cls != nil && classIsA(aj.cls, target)
	}
}

// ajToGlobalID builds the Arguments GlobalID serialization seam: a Ruby argument
// object that responds to #to_global_id serializes to that gid URI; anything else
// falls through to the library's "unsupported argument type" error.
func (vm *VM) ajToGlobalID() func(any) (string, bool, error) {
	return func(obj any) (string, bool, error) {
		v := obj.(object.Value)
		if vm.respondsTo(v, "to_global_id") {
			return vm.send(v, "to_global_id", nil, nil).ToS(), true, nil
		}
		return "", false, nil
	}
}

// --- Ruby <-> activejob value conversions -----------------------------------

// ajFromRuby maps a Ruby argument into the activejob input value model: plain
// primitives map directly, a Symbol / Array / Hash to their activejob wrappers,
// and any other value passes through as itself for the GlobalID seam to consider.
func ajFromRuby(v object.Value) any {
	switch x := v.(type) {
	case object.Nil:
		return nil
	case object.Bool:
		return bool(x)
	case object.Integer:
		return int64(x)
	case object.Float:
		return float64(x)
	case *object.String:
		return x.Str()
	case object.Symbol:
		return activejob.Symbol(string(x))
	case *object.Array:
		out := make([]any, len(x.Elems))
		for i, e := range x.Elems {
			out[i] = ajFromRuby(e)
		}
		return out
	case *object.Hash:
		h := activejob.NewHash()
		for _, k := range x.Keys {
			val, _ := x.Get(k)
			h.Set(ajHashKey(k), ajFromRuby(val))
		}
		return h
	default:
		return v
	}
}

// ajHashKey maps a Ruby Hash key to the activejob key model: a Symbol stays a
// Symbol, a String becomes a plain string, and any other key passes through so
// the library rejects it with a SerializationError (only string/symbol keys are
// serializable, as in ActiveJob).
func ajHashKey(k object.Value) any {
	switch key := k.(type) {
	case object.Symbol:
		return activejob.Symbol(string(key))
	case *object.String:
		return key.Str()
	default:
		return k
	}
}

// ajArgToRuby maps a decoded activejob argument (either the input model held in a
// job's Arguments, or the output of Arguments.Deserialize) back into the Ruby
// object graph. A GlobalID with no locator becomes its URI String (the plain-type
// round-trip). An ActiveSupport-rich type (Time/Duration/…) is not reconstructed
// at the Ruby boundary and raises a DeserializationError.
func (vm *VM) ajArgToRuby(v any) object.Value {
	switch x := v.(type) {
	case nil:
		return object.NilV
	case bool:
		return object.Bool(x)
	case int64:
		return object.IntValue(x)
	case float64:
		return object.Float(x)
	case string:
		return object.NewString(x)
	case activejob.Symbol:
		return object.Symbol(string(x))
	case activejob.GlobalID:
		return object.NewString(x.URI)
	case []any:
		el := make([]object.Value, len(x))
		for i, e := range x {
			el[i] = vm.ajArgToRuby(e)
		}
		return object.NewArrayFromSlice(el)
	case *activejob.Hash:
		h := object.NewHash()
		for _, k := range x.Keys() {
			val, _ := x.Get(k)
			h.Set(ajHashKeyToRuby(k), vm.ajArgToRuby(val))
		}
		return h
	case *activejob.IndifferentHash:
		h := object.NewHash()
		for _, k := range x.Keys() {
			val, _ := x.Get(k)
			h.Set(object.NewString(k), vm.ajArgToRuby(val))
		}
		return h
	case object.Value:
		return x
	default:
		return raise("ActiveJob::DeserializationError", "unsupported argument type %T", v)
	}
}

// ajHashKeyToRuby maps an activejob Hash key (a string or a Symbol) back to a Ruby
// key.
func ajHashKeyToRuby(k any) object.Value {
	if s, ok := k.(activejob.Symbol); ok {
		return object.Symbol(string(s))
	}
	return object.NewString(k.(string))
}

// ajWireToRuby maps a serialized argument (the *Object / []any / primitive shape
// Arguments.Serialize produces) into the Ruby object graph, so
// ActiveJob::Arguments.serialize returns the wire form as Ruby Hashes/Arrays.
func (vm *VM) ajWireToRuby(v any) object.Value {
	switch x := v.(type) {
	case nil:
		return object.NilV
	case bool:
		return object.Bool(x)
	case int64:
		return object.IntValue(x)
	case float64:
		return object.Float(x)
	case []any:
		el := make([]object.Value, len(x))
		for i, e := range x {
			el[i] = vm.ajWireToRuby(e)
		}
		return object.NewArrayFromSlice(el)
	case *activejob.Object:
		h := object.NewHash()
		for _, k := range x.Keys() {
			val, _ := x.Get(k)
			h.Set(object.NewString(k), vm.ajWireToRuby(val))
		}
		return h
	default:
		return object.NewString(x.(string))
	}
}

// ajRawFromRuby maps a Ruby wire-form value (as returned by serialize) into the
// parsed-JSON Go shape Arguments.Deserialize consumes: Hashes become
// map[string]any (string keys), Arrays become []any, primitives map directly.
func ajRawFromRuby(v object.Value) any {
	switch x := v.(type) {
	case object.Nil:
		return nil
	case object.Bool:
		return bool(x)
	case object.Integer:
		return int64(x)
	case object.Float:
		return float64(x)
	case *object.Array:
		out := make([]any, len(x.Elems))
		for i, e := range x.Elems {
			out[i] = ajRawFromRuby(e)
		}
		return out
	case *object.Hash:
		m := map[string]any{}
		for _, k := range x.Keys {
			val, _ := x.Get(k)
			m[k.ToS()] = ajRawFromRuby(val)
		}
		return m
	default:
		return x.ToS()
	}
}

// --- option parsing ---------------------------------------------------------

// ajInt coerces an Integer argument to an int (a non-Integer reads as 0).
func ajInt(v object.Value) int {
	if n, ok := v.(object.Integer); ok {
		return int(n)
	}
	return 0
}

// ajDuration coerces a numeric seconds value (Integer or Float) to a Duration.
func ajDuration(v object.Value) time.Duration {
	if f, ok := v.(object.Float); ok {
		return time.Duration(float64(f) * float64(time.Second))
	}
	n, _ := v.(object.Integer)
	return time.Duration(int64(n)) * time.Second
}

// ajQueueName coerces a queue_as value (a Symbol or String) to a queue name.
func ajQueueName(v object.Value) string { return v.ToS() }

// ajRetryOptions reads the retry_on keyword options (wait:, attempts:) from a
// trailing options Hash, defaulting to the library's own defaults when absent.
func ajRetryOptions(rest []object.Value) activejob.RetryOptions {
	opts := activejob.RetryOptions{}
	h := ajTrailingHash(rest)
	if h == nil {
		return opts
	}
	if v, ok := hashOption(h, "attempts"); ok {
		opts.Attempts = ajInt(v)
	}
	if v, ok := hashOption(h, "wait"); ok {
		d := ajDuration(v)
		opts.Wait = func(int) time.Duration { return d }
	}
	return opts
}

// ajSetOptions reads the set(...) keyword options (queue:, priority:, wait:) from
// a trailing options Hash into a library SetOptions.
func ajSetOptions(args []object.Value) activejob.SetOptions {
	opts := activejob.SetOptions{}
	h := ajTrailingHash(args)
	if h == nil {
		return opts
	}
	if v, ok := hashOption(h, "queue"); ok {
		opts.Queue = v.ToS()
	}
	if v, ok := hashOption(h, "priority"); ok {
		p := ajInt(v)
		opts.Priority = &p
	}
	if v, ok := hashOption(h, "wait"); ok {
		opts.Wait = ajDuration(v)
	}
	return opts
}

// ajTrailingHash returns the trailing keyword-options Hash of an argument list, or
// nil when the last argument is not a Hash.
func ajTrailingHash(args []object.Value) *object.Hash {
	if len(args) == 0 {
		return nil
	}
	if h, ok := args[len(args)-1].(*object.Hash); ok {
		return h
	}
	return nil
}
