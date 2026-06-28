// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestSyslogConstants covers the severity/facility constants the module exposes
// for feature detection, asserted against MRI's values.
func TestSyslogConstants(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "syslog"; p Syslog::LOG_EMERG`, "0"},
		{`require "syslog"; p Syslog::LOG_ERR`, "3"},
		{`require "syslog"; p Syslog::LOG_DEBUG`, "7"},
		{`require "syslog"; p Syslog::LOG_PID`, "1"},
		{`require "syslog"; p Syslog::LOG_CONS`, "2"},
		{`require "syslog"; p Syslog::LOG_NDELAY`, "8"},
		{`require "syslog"; p Syslog::LOG_USER`, "8"},
		{`require "syslog"; p Syslog::LOG_DAEMON`, "24"},
		{`require "syslog"; p Syslog::LOG_LOCAL0`, "128"},
	}
	for _, c := range cases {
		if got := runSrc(t, c.src); got != c.want {
			t.Fatalf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestSyslogNotImplemented covers every method, all of which raise
// NotImplementedError until the syslog transport round.
func TestSyslogNotImplemented(t *testing.T) {
	vm := New(nil)
	mod := vm.consts["Syslog"].(*RClass)
	for _, name := range []string{"open", "log", "close", "info", "warning", "err", "debug", "notice"} {
		m := mod.smethods[name]
		if m == nil {
			t.Fatalf("Syslog.%s not found", name)
		}
		got := catchRaise(func() { m.native(vm, mod, []object.Value{object.NewString("x")}, nil) })
		if got != "NotImplementedError" {
			t.Fatalf("Syslog.%s: got %q, want NotImplementedError", name, got)
		}
	}
}
