// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/alicebob/miniredis/v2"
	redisv9 "github.com/redis/go-redis/v9"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestGoRedisReply covers every arm of the go-redis reply conversion, including
// the array recursion and the string-fallback default (a float reply, which the
// typed command surface never produces but the converter must still handle).
func TestGoRedisReply(t *testing.T) {
	if v := goRedisReply(nil); v != object.NilV {
		t.Errorf("nil -> %v", v)
	}
	if s, ok := goRedisReply("hi").(*object.String); !ok || s.Str() != "hi" {
		t.Errorf("string -> %v", goRedisReply("hi"))
	}
	if n, ok := goRedisReply(int64(7)).(object.Integer); !ok || int64(n) != 7 {
		t.Errorf("int64 -> %v", goRedisReply(int64(7)))
	}
	arr, ok := goRedisReply([]any{int64(1), "x"}).(*object.Array)
	if !ok || len(arr.Elems) != 2 {
		t.Fatalf("array -> %v", goRedisReply([]any{int64(1), "x"}))
	}
	if s, ok := goRedisReply(3.14).(*object.String); !ok || s.Str() != "3.14" {
		t.Errorf("float default -> %v", goRedisReply(3.14))
	}
}

// TestJobAnyToRuby covers every decoded-argument arm, including both number
// encodings (float64 from Sidekiq, json.Number from Resque), the integral/float
// split, the nested collections and the string-fallback default.
func TestJobAnyToRuby(t *testing.T) {
	if jobAnyToRuby(nil) != object.NilV {
		t.Error("nil")
	}
	if v := jobAnyToRuby(true); v != object.Bool(true) {
		t.Errorf("bool -> %v", v)
	}
	if v, ok := jobAnyToRuby(2.0).(object.Integer); !ok || int64(v) != 2 {
		t.Errorf("integral float -> %v", jobAnyToRuby(2.0))
	}
	if v, ok := jobAnyToRuby(2.5).(object.Float); !ok || float64(v) != 2.5 {
		t.Errorf("float -> %v", jobAnyToRuby(2.5))
	}
	if v, ok := jobAnyToRuby(json.Number("3")).(object.Integer); !ok || int64(v) != 3 {
		t.Errorf("json.Number int -> %v", jobAnyToRuby(json.Number("3")))
	}
	if v, ok := jobAnyToRuby(json.Number("3.5")).(object.Float); !ok || float64(v) != 3.5 {
		t.Errorf("json.Number float -> %v", jobAnyToRuby(json.Number("3.5")))
	}
	if s, ok := jobAnyToRuby("s").(*object.String); !ok || s.Str() != "s" {
		t.Errorf("string -> %v", jobAnyToRuby("s"))
	}
	if a, ok := jobAnyToRuby([]any{int64(1)}).(*object.Array); !ok || len(a.Elems) != 1 {
		t.Errorf("array -> %v", jobAnyToRuby([]any{int64(1)}))
	}
	h, ok := jobAnyToRuby(map[string]any{"k": "v"}).(*object.Hash)
	if !ok || len(h.Keys) != 1 {
		t.Fatalf("map -> %v", jobAnyToRuby(map[string]any{"k": "v"}))
	}
	if s, ok := jobAnyToRuby(42).(*object.String); !ok || s.Str() != "42" {
		t.Errorf("default -> %v", jobAnyToRuby(42))
	}
}

// TestJobRedisArg covers every argument-coercion arm plus the to_s fallback.
func TestJobRedisArg(t *testing.T) {
	if v := jobRedisArg(object.IntValue(5)); v != int64(5) {
		t.Errorf("int -> %v", v)
	}
	if v := jobRedisArg(object.Float(1.5)); v != 1.5 {
		t.Errorf("float -> %v", v)
	}
	if v := jobRedisArg(object.NewString("x")); v != "x" {
		t.Errorf("string -> %v", v)
	}
	if v := jobRedisArg(object.Symbol("s")); v != "s" {
		t.Errorf("symbol -> %v", v)
	}
	if v := jobRedisArg(object.Bool(true)); v != true {
		t.Errorf("bool -> %v", v)
	}
	if v := jobRedisArg(object.NilV); v != "" {
		t.Errorf("default -> %v", v)
	}
}

// TestJobFloat covers jobFloat's Integer and Float arms plus the non-numeric
// fallback, and resqueQueueName's String and non-name arms.
func TestJobFloat(t *testing.T) {
	if v := jobFloat(object.IntValue(5)); v != 5 {
		t.Errorf("jobFloat int -> %v", v)
	}
	if v := jobFloat(object.Float(2.5)); v != 2.5 {
		t.Errorf("jobFloat float -> %v", v)
	}
	if v := jobFloat(object.NewString("x")); v != 0 {
		t.Errorf("jobFloat default -> %v", v)
	}
	if v := resqueQueueName(object.NewString("s")); v != "s" {
		t.Errorf("resqueQueueName string -> %q", v)
	}
	if v := resqueQueueName(object.NilV); v != "" {
		t.Errorf("resqueQueueName default -> %q", v)
	}
}

// TestRetryOption covers the retry-option mapping: Bool, Integer and the default.
func TestRetryOption(t *testing.T) {
	if v := retryOption(object.Bool(false)); v != false {
		t.Errorf("bool -> %v", v)
	}
	if v := retryOption(object.IntValue(5)); v != 5 {
		t.Errorf("int -> %v", v)
	}
	if v := retryOption(object.Symbol("x")); v != true {
		t.Errorf("default -> %v", v)
	}
}

// TestResolveRedisURL covers the URL precedence: an explicit value wins, then
// ENV["REDIS_URL"], then the local default.
func TestResolveRedisURL(t *testing.T) {
	t.Setenv("REDIS_URL", "")
	if v := resolveRedisURL("redis://set"); v != "redis://set" {
		t.Errorf("configured -> %q", v)
	}
	if v := resolveRedisURL(""); v != defaultRedisURL {
		t.Errorf("default -> %q", v)
	}
	t.Setenv("REDIS_URL", "redis://env")
	if v := resolveRedisURL(""); v != "redis://env" {
		t.Errorf("env -> %q", v)
	}
}

// TestSidekiqDrainRace covers sidekiqDrain's stop branch: a queue that reports
// non-empty (LLEN) but whose fetch yields nothing (BRPOP returns nil, as when a
// concurrent consumer claimed the job) ends the drive with no job counted.
func TestSidekiqDrainRace(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redisv9.NewClient(&redisv9.Options{Addr: mr.Addr()})
	defer func() { _ = rdb.Close() }()
	if err := rdb.LPush(context.Background(), "queue:default", "{}").Err(); err != nil {
		t.Fatal(err)
	}
	// Force BRPOP to report an empty result while LLEN still sees the job.
	rdb.AddHook(jobFaultHook{cmd: "brpop", err: redisv9.Nil})
	vm := New(&bytes.Buffer{})
	if n := vm.sidekiqDrain(rdb, []string{"default"}); n != 0 {
		t.Errorf("drain race count = %d, want 0", n)
	}
}

// TestJobWrapperProtocols covers the value-protocol arms (ToS/Inspect/Truthy) of
// the job wrappers, which the Ruby surface reaches only through implicit to_s.
func TestJobWrapperProtocols(t *testing.T) {
	jr := &JobRedis{cls: newClass("X", nil)}
	rj := &ResqueJob{cls: newClass("Resque::Job", nil), queue: "q", class: "C"}
	rw := &ResqueWorker{cls: newClass("Resque::Worker", nil)}
	for _, w := range []interface {
		ToS() string
		Inspect() string
		Truthy() bool
	}{jr, rj, rw} {
		if w.ToS() == "" || w.Inspect() == "" || !w.Truthy() {
			t.Errorf("wrapper protocol: %q %q %v", w.ToS(), w.Inspect(), w.Truthy())
		}
	}
}

// jobFaultHook fails a named go-redis command with a fixed error, so a
// command-error branch is exercised without a broken server.
type jobFaultHook struct {
	cmd string
	err error
}

func (h jobFaultHook) DialHook(next redisv9.DialHook) redisv9.DialHook { return next }
func (h jobFaultHook) ProcessPipelineHook(next redisv9.ProcessPipelineHook) redisv9.ProcessPipelineHook {
	return next
}
func (h jobFaultHook) ProcessHook(next redisv9.ProcessHook) redisv9.ProcessHook {
	return func(ctx context.Context, cmd redisv9.Cmder) error {
		if cmd.Name() == h.cmd {
			cmd.SetErr(h.err)
			return h.err
		}
		return next(ctx, cmd)
	}
}

// TestSidekiqProcessFetchError covers sidekiqProcess raising when the processor's
// BRPOP fetch fails (a command error mid-drive, distinct from the empty-queue
// pre-check). A fault hook fails only BRPOP so the fetch errors.
func TestSidekiqProcessFetchError(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redisv9.NewClient(&redisv9.Options{Addr: mr.Addr()})
	defer func() { _ = rdb.Close() }()
	rdb.AddHook(jobFaultHook{cmd: "brpop", err: context.DeadlineExceeded})

	vm := New(&bytes.Buffer{})
	defer func() {
		r := recover()
		e, ok := r.(RubyError)
		if !ok || e.Class != "Sidekiq::RedisConnectionError" {
			t.Fatalf("want Sidekiq::RedisConnectionError, got %v", r)
		}
	}()
	vm.sidekiqProcess(rdb, []string{"default"})
	t.Fatal("expected a raise")
}

// TestRunJobPerformGoPanic covers runJobPerform re-raising a non-Ruby Go panic
// untouched (its recover only classifies RubyError). A class-method perform that
// panics with a plain string must propagate that string.
func TestRunJobPerformGoPanic(t *testing.T) {
	vm := New(&bytes.Buffer{})
	cls := newClass("PanicJob", vm.cObject)
	cls.smethods["perform"] = &Method{name: "perform", owner: cls, native: func(*VM, object.Value, []object.Value, *Proc) object.Value {
		panic("boom-go")
	}}
	vm.consts["PanicJob"] = cls
	defer func() {
		if r := recover(); r != "boom-go" {
			t.Fatalf("want re-panicked \"boom-go\", got %v", r)
		}
	}()
	vm.runJobPerform("PanicJob", nil, true)
	t.Fatal("expected a panic")
}
