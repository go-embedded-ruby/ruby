// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	netpop "github.com/go-ruby-net-pop/net-pop"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerNetPOP installs the Net::POP3 client (require "net/pop"): Net::POP3.new /
// .start plus the session surface (start / finish / mails / each_mail / n_mails /
// n_bytes / reset / delete_all / set_all_uids / noop / stls) and the Net::POPMail
// model (number / length / size / unique_id / pop / header / top / delete /
// deleted?), all over the POP3 protocol codec of github.com/go-ruby-net-pop/net-pop.
//
// The TCP/TLS socket is the host seam: rbgo drives the library's Transport over an
// injected Ruby IO-like object (Net::POP3.new(address, port, connection: io) — any
// object responding to #write / #gets, e.g. a StringIO or a duck-typed socket).
// The Net::POPError / Net::POPAuthenticationError / Net::POPBadResponse tree is
// registered so a server "-ERR" or a malformed reply rescues as the gem-faithful
// Ruby class.
//
// It runs after registerNetHTTP (which creates the Net module) and after
// registerSocket / registerOpenSSL, mirroring how redis/pg wire an injected seam.
func (vm *VM) registerNetPOP() {
	net := vm.consts["Net"].(*RClass)
	vm.registerNetPOPErrors(net)

	pop3 := newClass("Net::POP3", vm.cObject)
	net.consts["POP3"] = pop3
	vm.consts["Net::POP3"] = pop3

	mail := newClass("Net::POPMail", vm.cObject)
	net.consts["POPMail"] = mail
	vm.consts["Net::POPMail"] = mail

	vm.registerNetPOP3ClassMethods(pop3)
	vm.registerNetPOP3InstanceMethods(pop3)
	vm.registerNetPOPMailMethods(mail)
}

// registerNetPOPErrors installs the Net::POPError hierarchy mirroring net/pop.rb:
// POPError < StandardError, POPBadResponse < POPError, POPAuthenticationError <
// StandardError (a sibling of POPError, as in MRI's ProtoAuthError branch).
func (vm *VM) registerNetPOPErrors(net *RClass) {
	std := vm.consts["StandardError"].(*RClass)
	reg := func(simple string, super *RClass) *RClass {
		qualified := "Net::" + simple
		c := newClass(qualified, super)
		net.consts[simple] = c
		vm.consts[qualified] = c
		return c
	}
	popErr := reg("POPError", std)
	reg("POPBadResponse", popErr)
	reg("POPAuthenticationError", std)
}

// registerNetPOP3ClassMethods installs Net::POP3.new and the Net::POP3.start
// convenience (new + start, yielding the session to a block then finishing).
func (vm *VM) registerNetPOP3ClassMethods(pop3 *RClass) {
	pop3.smethods["new"] = &Method{name: "new", owner: pop3,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			seam, rest := popSeamArg(args)
			if seam == nil {
				raise("ArgumentError", "Net::POP3.new requires a connection: IO-like object (rbgo has no native POP3 socket seam here yet)")
			}
			return vm.popBuild(pop3, seam, popStrAt(rest, 0), popIntAt(rest, 1, 110), popBoolAt(rest, 2))
		}}

	// Net::POP3.start(address, port, account, password, isapop = false,
	// connection: io) { |pop| ... }: build a session, start it, and (with a block)
	// yield it and finish afterwards, mirroring MRI's POP3.start.
	pop3.smethods["start"] = &Method{name: "start", owner: pop3,
		native: func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
			seam, rest := popSeamArg(args)
			if seam == nil {
				raise("ArgumentError", "Net::POP3.start requires a connection: IO-like object (rbgo has no native POP3 socket seam here yet)")
			}
			o := vm.popBuild(pop3, seam, popStrAt(rest, 0), popIntAt(rest, 1, 110), popBoolAt(rest, 4))
			return vm.popStart(o, popStrAt(rest, 2), popStrAt(rest, 3), blk)
		}}
}

// popBuild constructs a POP3Obj over the injected seam, wiring the library's Conn
// to the rubyPOPTransport.
func (vm *VM) popBuild(cls *RClass, seam object.Value, address string, port int, apop bool) *POP3Obj {
	o := &POP3Obj{cls: cls, seam: seam, address: address, port: port, apop: apop}
	o.conn = netpop.NewConn(&rubyPOPTransport{vm: vm, obj: seam})
	return o
}

// registerNetPOP3InstanceMethods installs the Net::POP3 session surface.
func (vm *VM) registerNetPOP3InstanceMethods(pop3 *RClass) {
	self := func(v object.Value) *POP3Obj { return v.(*POP3Obj) }

	pop3.define("address", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).address)
	})
	pop3.define("port", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).port))
	})
	pop3.define("apop?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).apop)
	})
	pop3.define("started?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).started)
	})
	pop3.define("active?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).started)
	})
	pop3.define("_connection", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return self(v).seam
	})

	pop3.define("start", func(vm *VM, v object.Value, args []object.Value, blk *Proc) object.Value {
		return vm.popStart(self(v), popStrAt(args, 0), popStrAt(args, 1), blk)
	})
	pop3.define("finish", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		vm.popFinish(self(v))
		return object.NilV
	})

	pop3.define("mails", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.popMailArray(self(v))
	})
	pop3.define("each_mail", func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		o := self(v)
		if blk == nil {
			raise("LocalJumpError", "no block given (yield)")
		}
		for _, m := range vm.popMails(o) {
			vm.callBlock(blk, []object.Value{m})
		}
		return v
	})
	pop3.define("each", func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		o := self(v)
		if blk == nil {
			raise("LocalJumpError", "no block given (yield)")
		}
		for _, m := range vm.popMails(o) {
			vm.callBlock(blk, []object.Value{m})
		}
		return v
	})
	pop3.define("delete_all", func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		o := self(v)
		for _, m := range vm.popMails(o) {
			if blk != nil {
				vm.callBlock(blk, []object.Value{m})
			}
			vm.popDelete(m)
		}
		return object.NilV
	})

	pop3.define("n_mails", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		count, _ := vm.popStat(self(v))
		return object.IntValue(int64(count))
	})
	pop3.define("n_bytes", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		_, size := vm.popStat(self(v))
		return object.IntValue(int64(size))
	})

	pop3.define("reset", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		o := self(v)
		if err := o.conn.Rset(); err != nil {
			popRaise(err)
		}
		o.mails = nil
		return v
	})
	pop3.define("set_all_uids", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.popSetAllUIDs(self(v))
	})
	pop3.define("noop", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		if err := self(v).conn.Noop(); err != nil {
			popRaise(err)
		}
		return object.NilV
	})
	pop3.define("stls", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		if err := self(v).conn.Stls(); err != nil {
			popRaise(err)
		}
		return object.NilV
	})
}

// popStart reads the greeting and authenticates (APOP when so configured, else
// USER/PASS), marking the session started. With a block it yields the session and
// finishes afterwards, mirroring MRI's POP3#start.
func (vm *VM) popStart(o *POP3Obj, account, password string, blk *Proc) object.Value {
	if err := o.conn.Greet(); err != nil {
		popRaise(err)
	}
	if o.apop {
		if err := o.conn.Apop(account, password); err != nil {
			popRaise(err)
		}
	} else {
		if err := o.conn.Auth(account, password); err != nil {
			popRaise(err)
		}
	}
	o.started = true
	if blk != nil {
		res := vm.callBlock(blk, []object.Value{o})
		vm.popFinish(o)
		return res
	}
	return o
}

// popFinish issues QUIT and clears the started flag, mirroring POP3#finish.
func (vm *VM) popFinish(o *POP3Obj) {
	if err := o.conn.Quit(); err != nil {
		popRaise(err)
	}
	o.started = false
}

// popStat issues STAT and returns (count, size), raising the gem-faithful error on
// a non-"+OK" or malformed reply.
func (vm *VM) popStat(o *POP3Obj) (int, int) {
	count, size, err := o.conn.Stat()
	if err != nil {
		popRaise(err)
	}
	return count, size
}

// popMails issues (once) a multiline LIST and memoises the POPMail wrappers.
func (vm *VM) popMails(o *POP3Obj) []*POPMailObj {
	if o.mails != nil {
		return o.mails
	}
	list, err := o.conn.List()
	if err != nil {
		popRaise(err)
	}
	mailCls := vm.consts["Net::POPMail"].(*RClass)
	out := make([]*POPMailObj, len(list))
	for i, m := range list {
		out[i] = &POPMailObj{cls: mailCls, pop: o, mail: m}
	}
	o.mails = out
	return out
}

// popMailArray returns the memoised POPMail list as a Ruby Array.
func (vm *VM) popMailArray(o *POP3Obj) object.Value {
	mails := vm.popMails(o)
	elems := make([]object.Value, len(mails))
	for i, m := range mails {
		elems[i] = m
	}
	return object.NewArrayFromSlice(elems)
}

// popSetAllUIDs issues UIDL and fills each memoised mail's uid, mirroring
// POP3#set_all_uids.
func (vm *VM) popSetAllUIDs(o *POP3Obj) object.Value {
	mails := vm.popMails(o)
	table, err := o.conn.UIDL()
	if err != nil {
		popRaise(err)
	}
	models := make([]*netpop.POPMail, len(mails))
	for i, m := range mails {
		models[i] = m.mail
	}
	netpop.SetAllUIDs(models, table)
	return object.NilV
}

// popDelete marks a message for deletion on the server (DELE), mirroring
// POPMail#delete.
func (vm *VM) popDelete(m *POPMailObj) {
	if err := m.pop.conn.Dele(m.mail.Number); err != nil {
		popRaise(err)
	}
	m.mail.Deleted = true
}

// registerNetPOPMailMethods installs the Net::POPMail model surface.
func (vm *VM) registerNetPOPMailMethods(mail *RClass) {
	self := func(v object.Value) *POPMailObj { return v.(*POPMailObj) }

	mail.define("number", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).mail.Number))
	})
	mail.define("length", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).mail.Length))
	})
	mail.define("size", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).mail.Length))
	})
	mail.define("deleted?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).mail.Deleted)
	})

	// #unique_id / #uidl: return the cached UID, or fetch it with a single-message
	// "UIDL n" when it has not been populated (e.g. no prior set_all_uids).
	uid := func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		m := self(v)
		if m.mail.UID == "" {
			id, err := m.pop.conn.UIDLNum(m.mail.Number)
			if err != nil {
				popRaise(err)
			}
			m.mail.UID = id
		}
		return object.NewString(m.mail.UID)
	}
	mail.define("unique_id", uid)
	mail.define("uidl", uid)

	// #pop / #mail / #to_s: RETR the full message; with a block, yield the body.
	popBody := func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		m := self(v)
		body, err := m.pop.conn.Retr(m.mail.Number)
		if err != nil {
			popRaise(err)
		}
		s := object.NewString(body)
		if blk != nil {
			vm.callBlock(blk, []object.Value{s})
			return v
		}
		return s
	}
	mail.define("pop", popBody)
	mail.define("mail", popBody)
	mail.define("to_s", popBody)

	// #header([lines]): TOP with a zero body-line count (the header only).
	mail.define("header", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		m := self(v)
		body, err := m.pop.conn.Top(m.mail.Number, 0)
		if err != nil {
			popRaise(err)
		}
		return object.NewString(body)
	})
	// #top(lines): TOP with the given body-line count.
	mail.define("top", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		m := self(v)
		body, err := m.pop.conn.Top(m.mail.Number, popIntAt(args, 0, 0))
		if err != nil {
			popRaise(err)
		}
		return object.NewString(body)
	})

	// #delete / #delete!: DELE the message, marking it deleted this session.
	del := func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		vm.popDelete(self(v))
		return object.NilV
	}
	mail.define("delete", del)
	mail.define("delete!", del)
}
