// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerSyslog installs a loadable shell for the Syslog standard library
// (require "syslog"). Puppet probes for syslog as a feature (the require alone
// determining availability) and only writes to it when configured to; the
// module and its severity/facility constants exist so the feature is detected,
// while open / log raise NotImplementedError until the syslog transport round.
func (vm *VM) registerSyslog() {
	mod := newClass("Syslog", nil)
	mod.isModule = true
	vm.consts["Syslog"] = mod

	// Severity levels (LOG_EMERG..LOG_DEBUG) and a couple of common facilities,
	// the values app code ORs and compares against.
	for k, v := range map[string]int{
		"LOG_EMERG": 0, "LOG_ALERT": 1, "LOG_CRIT": 2, "LOG_ERR": 3,
		"LOG_WARNING": 4, "LOG_NOTICE": 5, "LOG_INFO": 6, "LOG_DEBUG": 7,
		"LOG_PID": 0x01, "LOG_CONS": 0x02, "LOG_NDELAY": 0x08,
		"LOG_USER": 1 << 3, "LOG_DAEMON": 3 << 3, "LOG_LOCAL0": 16 << 3,
	} {
		mod.consts[k] = object.IntValue(int64(v))
	}

	notImpl := func(what string) NativeFn {
		return func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
			return raise("NotImplementedError", "Syslog.%s not yet supported (syslog transport pending)", what)
		}
	}
	for _, m := range []string{"open", "log", "close", "info", "warning", "err", "debug", "notice"} {
		mod.smethods[m] = &Method{name: m, owner: mod, native: notImpl(m)}
	}
}
