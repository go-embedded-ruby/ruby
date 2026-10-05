// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"
	"strings"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/object"
	onig "github.com/go-ruby-regexp/regexp"
)

// catchRaiseFull runs fn and returns the RubyError it raised, or nil. The
// package already has catchRaise, which returns only the class name; the message
// matters here because MRI's exact wording is part of the contract.
func catchRaiseFull(fn func()) (e *RubyError) {
	defer func() {
		if r := recover(); r != nil {
			if re, ok := r.(RubyError); ok {
				e = &re
				return
			}
			panic(r)
		}
	}()
	fn()
	return nil
}

// TestRaiseMatchLimitInternal exercises raiseMatchLimit directly, because two of
// its arms cannot be reached through the interpreter:
//
//   - the nil arm is the no-op every successful match takes, and raising nothing
//     leaves no observable trace at the Ruby level;
//   - the default arm is defensive. The engine returns only ErrTimeout and
//     ErrBudget today, so no Ruby program can produce a third error here. It is
//     kept rather than dropped because falling THROUGH on an unrecognised error
//     would return nil to the caller — which is the exact fail-open #776 is
//     about, reintroduced for whatever limit the engine grows next.
func TestRaiseMatchLimitInternal(t *testing.T) {
	// nil: no raise, no panic.
	if e := catchRaiseFull(func() { raiseMatchLimit(nil) }); e != nil {
		t.Errorf("raiseMatchLimit(nil) raised %s: %s, want no raise", e.Class, e.Message)
	}

	for _, tc := range []struct {
		name          string
		err           error
		class         string
		messageSubstr string
	}{
		{"timeout", onig.ErrTimeout, "Regexp::TimeoutError", "regexp match timeout"},
		{"budget", onig.ErrBudget, "RegexpError", "step budget exceeded"},
		// Wrapped, so the arms must use errors.Is and not ==.
		{"wrapped_timeout", wrapErr(onig.ErrTimeout), "Regexp::TimeoutError", "regexp match timeout"},
		{"wrapped_budget", wrapErr(onig.ErrBudget), "RegexpError", "step budget exceeded"},
		// The defensive arm: an error the engine does not currently produce.
		{"unknown", errors.New("engine grew a third limit"), "RegexpError", "engine grew a third limit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := catchRaiseFull(func() { raiseMatchLimit(tc.err) })
			if e == nil {
				t.Fatalf("raiseMatchLimit(%v) did not raise; a limit that is not reported is the defect", tc.err)
			}
			if e.Class != tc.class {
				t.Errorf("raised %s, want %s", e.Class, tc.class)
			}
			if !strings.Contains(e.Message, tc.messageSubstr) {
				t.Errorf("message = %q, want it to contain %q", e.Message, tc.messageSubstr)
			}
		})
	}
}

func wrapErr(err error) error { return &wrapped{err} }

type wrapped struct{ err error }

func (w *wrapped) Error() string { return "wrapped: " + w.err.Error() }
func (w *wrapped) Unwrap() error { return w.err }

// TestMatcherResolutionInternal exercises matcher's precedence table directly,
// including the vm == nil case, which only the Grape regexp: binding reaches
// (grapeRegexp builds its adapter with no VM in scope) and which therefore has
// no Ruby-level probe.
//
// It asserts the resolved limit by reading Timeout() off the returned engine
// Regexp, so it measures the RESOLUTION and not a match's wall clock.
func TestMatcherResolutionInternal(t *testing.T) {
	m := New(&strings.Builder{})
	re, ok := m.compileRegexp("a+", "").(*Regexp)
	if !ok {
		t.Fatal("compileRegexp did not yield a *Regexp")
	}

	const sec = float64(1e9) // one second in nanoseconds, the Duration unit

	for _, tc := range []struct {
		name      string
		perRegexp object.Value
		class     object.Value
		useVM     bool
		wantSecs  float64
	}{
		// MRI: the per-Regexp limit wins outright when set, in either direction.
		{"per_regexp_only", object.Float(0.25), nil, true, 0.25},
		{"class_only", nil, object.Float(0.5), true, 0.5},
		{"per_regexp_beats_smaller_class", object.Float(2), object.Float(0.5), true, 2},
		{"per_regexp_beats_larger_class", object.Float(0.5), object.Float(2), true, 0.5},
		// "Unset" arrives in two Go shapes depending on the construction path: a
		// nil interface and an object.Nil. Both must fall through to the class
		// default, which is the bug that survived the first attempt at this fix.
		{"go_nil_falls_back", nil, object.Float(0.5), true, 0.5},
		{"object_nil_falls_back", object.NilV, object.Float(0.5), true, 0.5},
		{"both_unset", object.NilV, object.NilV, true, 0},
		{"class_object_nil", object.Float(0.25), object.NilV, true, 0.25},
		// vm == nil: only the per-Regexp limit can apply.
		{"no_vm_per_regexp", object.Float(0.25), nil, false, 0.25},
		{"no_vm_unset", object.NilV, nil, false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			re.timeout = tc.perRegexp
			var host *VM
			if tc.useVM {
				host = m
				m.regexpTimeout = tc.class
			}
			got := re.matcher(host).Timeout()
			if want := tc.wantSecs * sec; float64(got) != want {
				t.Errorf("matcher().Timeout() = %v, want %v (%.2fs)", got, want, tc.wantSecs)
			}
		})
	}

	re.timeout = nil
	m.regexpTimeout = nil
}
