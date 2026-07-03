// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"os"
	"os/user"
	"strconv"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// Etc database lookups are routed through these seams (over Go's pure-Go
// os/user) so the not-found / error branches are reachable identically on every
// platform without depending on the host's real passwd/group database.
var (
	etcTempDir    = os.TempDir
	etcLookupID   = user.LookupId
	etcLookup     = user.Lookup
	etcLookupGID  = user.LookupGroupId
	etcLookupGrp  = user.LookupGroup
	etcCurrentUID = func() string { return strconv.Itoa(os.Getuid()) }
)

// registerEtc installs the Etc module (require "etc"). The passwd/group lookups
// are real over Go's pure-Go os/user (getpwuid/getpwnam/getgrgid/getgrnam
// return Etc::Passwd / Etc::Group structs), and systmpdir is real over
// os.TempDir. Puppet's POSIX feature detection (Etc.getpwuid(0)) and user/group
// resource handling drive these. The enumeration cursors (getpwent/getgrent and
// the set*/end* pairs) are not backed by a streaming database and raise
// NotImplementedError.
func (vm *VM) registerEtc() {
	mod := newClass("Etc", nil)
	mod.isModule = true
	vm.consts["Etc"] = mod
	sdef := func(name string, fn NativeFn) { mod.smethods[name] = &Method{name: name, owner: mod, native: fn} }

	// Etc::Passwd / Etc::Group are Struct classes with MRI's member layout, so
	// Puppet's Etc::Passwd.members / struct.each_pair / is_a?(Etc::Passwd) work.
	// They are built lazily on first lookup because newStructClass mixes in
	// Enumerable, which the prelude installs after this bootstrap step runs.
	ensureClasses := func() (passwd, group *RClass) {
		cStruct := vm.consts["Struct"].(*RClass)
		if p, ok := mod.consts["Passwd"].(*RClass); ok {
			return p, mod.consts["Group"].(*RClass)
		}
		passwd = vm.newStructClass(cStruct,
			[]string{"name", "passwd", "uid", "gid", "gecos", "dir", "shell"}, false)
		passwd.name, passwd.named = "Etc::Passwd", true
		mod.consts["Passwd"] = passwd
		group = vm.newStructClass(cStruct,
			[]string{"name", "passwd", "gid", "mem"}, false)
		group.name, group.named = "Etc::Group", true
		mod.consts["Group"] = group
		return passwd, group
	}

	newPasswd := func(u *user.User) object.Value {
		passwd, _ := ensureClasses()
		o := &RObject{class: passwd, ivars: map[string]object.Value{}}
		o.ivars["@name"] = object.NewString(u.Username)
		o.ivars["@passwd"] = object.NewString("x")
		o.ivars["@uid"] = atoiOr0(u.Uid)
		o.ivars["@gid"] = atoiOr0(u.Gid)
		o.ivars["@gecos"] = object.NewString(u.Name)
		o.ivars["@dir"] = object.NewString(u.HomeDir)
		o.ivars["@shell"] = object.NewString("/bin/sh")
		return o
	}
	newGroup := func(g *user.Group) object.Value {
		_, group := ensureClasses()
		o := &RObject{class: group, ivars: map[string]object.Value{}}
		o.ivars["@name"] = object.NewString(g.Name)
		o.ivars["@passwd"] = object.NewString("x")
		o.ivars["@gid"] = atoiOr0(g.Gid)
		o.ivars["@mem"] = object.NewArray()
		return o
	}

	sdef("getpwuid", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		// No argument means the current process's user, as MRI does.
		id := etcCurrentUID()
		if len(args) > 0 {
			if _, isNil := args[0].(object.Nil); !isNil {
				id = strconv.FormatInt(intArg(args[0]), 10)
			}
		}
		u, err := etcLookupID(id)
		if err != nil {
			raise("ArgumentError", "can't find user for %s", id)
		}
		return newPasswd(u)
	})
	sdef("getpwnam", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		name := strArg(args[0])
		u, err := etcLookup(name)
		if err != nil {
			raise("ArgumentError", "can't find user for %s", name)
		}
		return newPasswd(u)
	})
	sdef("getgrgid", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		id := strconv.FormatInt(intArgOr(args, 0), 10)
		g, err := etcLookupGID(id)
		if err != nil {
			raise("ArgumentError", "can't find group for %s", id)
		}
		return newGroup(g)
	})
	sdef("getgrnam", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		name := strArg(args[0])
		g, err := etcLookupGrp(name)
		if err != nil {
			raise("ArgumentError", "can't find group for %s", name)
		}
		return newGroup(g)
	})

	sdef("systmpdir", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(etcTempDir())
	})

	notImpl := func(what string) NativeFn {
		return func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
			return raise("NotImplementedError", "Etc.%s not yet supported (streaming database pending)", what)
		}
	}
	for _, m := range []string{
		"getpwent", "setpwent", "endpwent", "getgrent", "setgrent", "endgrent",
		"group", "passwd", "getlogin", "confstr", "sysconf", "nprocessors",
	} {
		sdef(m, notImpl(m))
	}
}

// atoiOr0 parses a decimal id string to an Integer value, yielding 0 when it is
// not numeric (os/user returns numeric ids on the platforms we target).
func atoiOr0(s string) object.Value {
	n, err := strconv.Atoi(s)
	if err != nil {
		return object.IntValue(0)
	}
	return object.IntValue(int64(n))
}
