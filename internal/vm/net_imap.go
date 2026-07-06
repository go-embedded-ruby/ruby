// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	imap "github.com/go-ruby-net-imap/net-imap"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerNetIMAP installs Net::IMAP (require "net/imap"): a live IMAP4rev1
// session over the injected IO seam, driven by the command builder + response
// parser of github.com/go-ruby-net-imap/net-imap. It builds the Net::IMAP class,
// its error tree (Net::IMAP::Error and the response-error subclasses), the typed
// response value classes (TaggedResponse / UntaggedResponse / FetchData /
// Envelope / MailboxList / …), and the command surface (login / authenticate /
// select / list / fetch / store / search / append / …). It runs after
// registerSocket / registerOpenSSL so a real TCPSocket / SSLSocket can be handed
// in as the connection seam for TLS.
func (vm *VM) registerNetIMAP() {
	// Net is always registered by registerNetHTTP, which runs earlier in the
	// bootstrap (see builtins.go).
	netMod := vm.consts["Net"].(*RClass)
	cls := newClass("Net::IMAP", vm.cObject)
	netMod.consts["IMAP"] = cls
	vm.consts["Net::IMAP"] = cls

	vm.registerNetIMAPErrors(cls)
	vm.registerNetIMAPValueClasses(cls)
	vm.registerNetIMAPConstruct(cls)
	vm.registerNetIMAPModuleMethods(cls)
	vm.registerNetIMAPCommands(cls)
}

// registerNetIMAPErrors installs the Net::IMAP error hierarchy, mirroring the
// net-imap gem: Error < StandardError; ResponseError < Error; NoResponseError /
// BadResponseError / ByeResponseError < ResponseError; ResponseParseError /
// DataFormatError < Error.
func (vm *VM) registerNetIMAPErrors(cls *RClass) {
	std := vm.consts["StandardError"].(*RClass)
	reg := func(simple string, super *RClass) *RClass {
		qualified := "Net::IMAP::" + simple
		c := newClass(qualified, super)
		cls.consts[simple] = c
		vm.consts[qualified] = c
		return c
	}
	base := reg("Error", std)
	respErr := reg("ResponseError", base)
	reg("NoResponseError", respErr)
	reg("BadResponseError", respErr)
	reg("ByeResponseError", respErr)
	reg("ResponseParseError", base)
	reg("DataFormatError", base)
}

// registerNetIMAPValueClasses installs the typed response value classes, each an
// attr-reader struct mirroring the Net::IMAP::* member set. Instances are built
// by the mapping helpers in net_imap_bind.go.
func (vm *VM) registerNetIMAPValueClasses(cls *RClass) {
	def := func(simple string, fields ...string) *RClass {
		return vm.imapValueClass(cls, simple, fields...)
	}
	def("TaggedResponse", "tag", "name", "data", "raw_data")
	def("UntaggedResponse", "name", "data", "raw_data")
	def("ContinuationRequest", "data", "raw_data")
	def("ResponseText", "code", "text")
	def("ResponseCode", "name", "data")
	def("MailboxList", "attr", "delim", "name")
	def("StatusData", "mailbox", "attr")
	def("FetchData", "seqno", "attr")
	def("Envelope",
		"date", "subject", "from", "sender", "reply_to",
		"to", "cc", "bcc", "in_reply_to", "message_id")
	def("Address", "name", "route", "mailbox", "host")
	def("AppendUIDData", "uidvalidity", "assigned_uids")
	def("CopyUIDData", "uidvalidity", "source_uids", "assigned_uids")

	// The four body-structure classes share the reader surface over the loss-free
	// field tree; #multipart? reports whether the part is a multipart/*.
	for _, name := range []string{"BodyTypeText", "BodyTypeBasic", "BodyTypeMessage", "BodyTypeMultipart"} {
		bc := def(name,
			"media_type", "subtype", "param", "content_id", "description",
			"encoding", "size", "lines", "parts")
		bc.define("multipart?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
			return getIvar(self, "@multipart")
		})
	}
}

// imapValueClass creates a Net::IMAP::<simple> attr-reader class with the named
// readers and registers it under the IMAP class and the top-level const table.
func (vm *VM) imapValueClass(cls *RClass, simple string, fields ...string) *RClass {
	qualified := "Net::IMAP::" + simple
	c := newClass(qualified, vm.cObject)
	cls.consts[simple] = c
	vm.consts[qualified] = c
	names := make([]object.Value, len(fields))
	for i, f := range fields {
		names[i] = object.NewString(f)
	}
	defineAttrs(c, names, true, false)
	return c
}

// registerNetIMAPConstruct installs Net::IMAP.new: it builds a session over the
// injected IO-like connection seam and reads the server greeting. The connection
// is taken from the connection: / conn: keyword or a single positional IO
// argument (a StringIO, a TCPSocket, an OpenSSL::SSL::SSLSocket, or any object
// responding to #read/#write); rbgo has no implicit dialer here, so a missing
// connection raises ArgumentError.
func (vm *VM) registerNetIMAPConstruct(cls *RClass) {
	cls.smethods["new"] = &Method{name: "new", owner: cls, native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		conn := imapConnArg(args)
		if conn == nil {
			raise("ArgumentError", "Net::IMAP.new requires a connection: IO-like object (pass a TCPSocket/SSLSocket or any object responding to #read/#write)")
		}
		o := newIMAPObj(vm, cls, conn)
		o.readGreeting(vm)
		return o
	}}
}

// imapConnArg reads the connection seam from Net::IMAP.new's arguments: a trailing
// keyword Hash's connection: / conn:, else a single positional IO argument.
func imapConnArg(args []object.Value) object.Value {
	if len(args) > 0 {
		if h, ok := args[len(args)-1].(*object.Hash); ok {
			if v, ok := h.Get(object.Symbol("connection")); ok {
				return v
			}
			if v, ok := h.Get(object.Symbol("conn")); ok {
				return v
			}
			args = args[:len(args)-1]
		}
	}
	if len(args) > 0 {
		if _, isStr := args[0].(*object.String); !isStr {
			return args[0]
		}
	}
	return nil
}

// registerNetIMAPModuleMethods installs the stateless Net::IMAP module helpers
// (mailbox-name UTF-7 encode/decode and the date/message-set formatters) over
// the library's pure helper functions.
func (vm *VM) registerNetIMAPModuleMethods(cls *RClass) {
	sm := func(name string, fn NativeFn) {
		cls.smethods[name] = &Method{name: name, owner: cls, native: fn}
	}
	sm("encode_utf7", func(_ *VM, _ object.Value, a []object.Value, _ *Proc) object.Value {
		return object.NewString(imap.EncodeUTF7(imapStrArg(imapArg1(a, "encode_utf7"))))
	})
	sm("decode_utf7", func(_ *VM, _ object.Value, a []object.Value, _ *Proc) object.Value {
		return object.NewString(imap.DecodeUTF7(imapStrArg(imapArg1(a, "decode_utf7"))))
	})
}

// imapArg1 returns the sole argument, raising ArgumentError on a wrong count.
func imapArg1(args []object.Value, name string) object.Value {
	if len(args) != 1 {
		raise("ArgumentError", "wrong number of arguments for '%s' (given %d, expected 1)", name, len(args))
	}
	return args[0]
}

// registerNetIMAPCommands installs the Net::IMAP command surface. Each method
// builds its tagged command via the library builder, drives it over the seam,
// and maps the result: a status command returns its TaggedResponse; LIST / FETCH
// / SEARCH / EXPUNGE / STATUS return the typed untagged data.
func (vm *VM) registerNetIMAPCommands(cls *RClass) {
	d := func(name string, fn NativeFn) { cls.define(name, fn) }
	self := func(v object.Value) *IMAPObj { return v.(*IMAPObj) }

	// Session accessors.
	d("greeting", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return imapNilGreeting(self(v).greeting)
	})
	d("disconnected?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).disconnected)
	})
	d("disconnect", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		o := self(v)
		if !o.closed {
			if vm.respondsTo(o.conn, "close") {
				vm.send(o.conn, "close", nil, nil)
			}
			o.closed = true
		}
		o.disconnected = true
		return object.NilV
	})
	d("responses", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return imapResponsesHash(self(v))
	})

	// Zero-argument status commands returning their TaggedResponse.
	simple := func(build func(*imap.Builder) (imap.Command, error)) NativeFn {
		return func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
			o := self(v)
			tagged, _ := o.imapExec(vm, mustCmd(build(o.builder)))
			return vm.imapTagged(tagged)
		}
	}
	d("noop", simple((*imap.Builder).Noop))
	d("logout", simple((*imap.Builder).Logout))
	d("check", simple((*imap.Builder).Check))
	d("close", simple((*imap.Builder).Close))
	d("unselect", simple((*imap.Builder).Unselect))
	d("starttls", simple((*imap.Builder).StartTLS))

	// One-mailbox status commands.
	mailboxCmd := func(name string, build func(*imap.Builder, string) (imap.Command, error)) {
		d(name, func(vm *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
			o := self(v)
			tagged, _ := o.imapExec(vm, mustCmd(build(o.builder, imapStrArg(imapArg1(a, name)))))
			return vm.imapTagged(tagged)
		})
	}
	mailboxCmd("select", (*imap.Builder).Select)
	mailboxCmd("examine", (*imap.Builder).Examine)
	mailboxCmd("create", (*imap.Builder).Create)
	mailboxCmd("delete", (*imap.Builder).Delete)
	mailboxCmd("subscribe", (*imap.Builder).Subscribe)
	mailboxCmd("unsubscribe", (*imap.Builder).Unsubscribe)

	d("login", func(vm *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		imapArity(a, 2, "login")
		o := self(v)
		tagged, _ := o.imapExec(vm, mustCmd(o.builder.Login(imapStrArg(a[0]), imapStrArg(a[1]))))
		return vm.imapTagged(tagged)
	})
	d("authenticate", func(vm *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		imapArityMin(a, 1, "authenticate")
		return vm.imapTagged(self(v).imapAuth(vm, imapStrArg(a[0]), a[1:]))
	})
	d("rename", func(vm *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		imapArity(a, 2, "rename")
		o := self(v)
		tagged, _ := o.imapExec(vm, mustCmd(o.builder.Rename(imapStrArg(a[0]), imapStrArg(a[1]))))
		return vm.imapTagged(tagged)
	})

	// LIST / LSUB return an Array of MailboxList.
	listCmd := func(name, respName string, build func(*imap.Builder, string, string) (imap.Command, error)) {
		d(name, func(vm *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
			imapArity(a, 2, name)
			o := self(v)
			_, untagged := o.imapExec(vm, mustCmd(build(o.builder, imapStrArg(a[0]), imapStrArg(a[1]))))
			return vm.imapCollect(untagged, respName)
		})
	}
	listCmd("list", "LIST", (*imap.Builder).List)
	listCmd("lsub", "LSUB", (*imap.Builder).Lsub)

	// STATUS returns the single StatusData (or nil).
	d("status", func(vm *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		imapArity(a, 2, "status")
		o := self(v)
		_, untagged := o.imapExec(vm, mustCmd(o.builder.Status(imapStrArg(a[0]), imapAtts(a[1])...)))
		return imapFirst(vm.imapCollect(untagged, "STATUS"))
	})

	// FETCH / UID FETCH / STORE / UID STORE return an Array of FetchData.
	d("fetch", vm.imapFetchCmd((*imap.Builder).Fetch))
	d("uid_fetch", vm.imapFetchCmd((*imap.Builder).UIDFetch))
	d("store", vm.imapStoreCmd((*imap.Builder).Store))
	d("uid_store", vm.imapStoreCmd((*imap.Builder).UIDStore))

	// COPY / UID COPY return their TaggedResponse.
	copyCmd := func(name string, build func(*imap.Builder, *imap.SequenceSet, string) (imap.Command, error)) {
		d(name, func(vm *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
			imapArity(a, 2, name)
			o := self(v)
			tagged, _ := o.imapExec(vm, mustCmd(build(o.builder, imapSeqSet(a[0]), imapStrArg(a[1]))))
			return vm.imapTagged(tagged)
		})
	}
	copyCmd("copy", (*imap.Builder).Copy)
	copyCmd("uid_copy", (*imap.Builder).UIDCopy)

	// SEARCH / UID SEARCH return an Array of message numbers.
	searchCmd := func(name string, build func(*imap.Builder, ...imap.Argument) (imap.Command, error)) {
		d(name, func(vm *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
			o := self(v)
			_, untagged := o.imapExec(vm, mustCmd(build(o.builder, imapSearchArgs(imapSearchKeys(a))...)))
			return imapFirstOrEmpty(vm.imapCollect(untagged, "SEARCH"))
		})
	}
	searchCmd("search", (*imap.Builder).Search)
	searchCmd("uid_search", (*imap.Builder).UIDSearch)

	// EXPUNGE returns the Array of expunged message numbers.
	d("expunge", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		o := self(v)
		_, untagged := o.imapExec(vm, mustCmd(o.builder.Expunge()))
		return vm.imapCollect(untagged, "EXPUNGE")
	})

	// CAPABILITY returns the Array of capability tokens.
	d("capability", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		o := self(v)
		_, untagged := o.imapExec(vm, mustCmd(o.builder.Capability()))
		return imapFirstOrEmpty(vm.imapCollect(untagged, "CAPABILITY"))
	})

	// APPEND(mailbox, message[, flags[, date]]) returns its TaggedResponse.
	d("append", func(vm *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		imapArityMin(a, 2, "append")
		o := self(v)
		var flags []imap.Flag
		if len(a) >= 3 && !object.IsNil(a[2]) {
			flags = imapFlags(a[2])
		}
		tagged, _ := o.imapExec(vm, mustCmd(o.builder.Append(imapStrArg(a[0]), flags, nil, imapStrArg(a[1]))))
		return vm.imapTagged(tagged)
	})
}

// imapFetchCmd builds a FETCH-style command method over the given builder method
// (FETCH / UID FETCH), returning the Array of FetchData.
func (vm *VM) imapFetchCmd(build func(*imap.Builder, *imap.SequenceSet, ...string) (imap.Command, error)) NativeFn {
	return func(vm *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		imapArity(a, 2, "fetch")
		o := v.(*IMAPObj)
		_, untagged := o.imapExec(vm, mustCmd(build(o.builder, imapSeqSet(a[0]), imapAtts(a[1])...)))
		return vm.imapCollect(untagged, "FETCH")
	}
}

// imapStoreCmd builds a STORE-style command method over the given builder method
// (STORE / UID STORE), returning the Array of FetchData the server echoes.
func (vm *VM) imapStoreCmd(build func(*imap.Builder, *imap.SequenceSet, string, []imap.Flag) (imap.Command, error)) NativeFn {
	return func(vm *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		imapArity(a, 3, "store")
		o := v.(*IMAPObj)
		_, untagged := o.imapExec(vm, mustCmd(build(o.builder, imapSeqSet(a[0]), imapStrArg(a[1]), imapFlags(a[2]))))
		return vm.imapCollect(untagged, "FETCH")
	}
}

// imapSearchKeys flattens a single Array argument (imap.search([...])) to its
// elements, so both search("FROM", "a") and search(["FROM", "a"]) work.
func imapSearchKeys(a []object.Value) []object.Value {
	if len(a) == 1 {
		if arr, ok := a[0].(*object.Array); ok {
			return arr.Elems
		}
	}
	return a
}

// imapNilGreeting returns the stored greeting, or nil when none was recorded.
func imapNilGreeting(g object.Value) object.Value {
	if g == nil {
		return object.NilV
	}
	return g
}

// imapResponsesHash builds the Net::IMAP#responses Hash: name -> Array of the
// untagged data seen for it, in arrival order.
func imapResponsesHash(o *IMAPObj) object.Value {
	h := object.NewHash()
	for _, name := range o.responseNames {
		vals := o.responses[name]
		h.Set(object.NewString(name), object.NewArrayFromSlice(append([]object.Value(nil), vals...)))
	}
	return h
}

// imapFirst returns the first element of arr, or nil when it is empty.
func imapFirst(arr *object.Array) object.Value {
	if len(arr.Elems) == 0 {
		return object.NilV
	}
	return arr.Elems[0]
}

// imapFirstOrEmpty returns the first element of arr, or an empty Array when it is
// empty (SEARCH / CAPABILITY, which are always list-valued).
func imapFirstOrEmpty(arr *object.Array) object.Value {
	if len(arr.Elems) == 0 {
		return object.NewArrayFromSlice(nil)
	}
	return arr.Elems[0]
}

// imapArity raises ArgumentError when args does not have exactly n elements.
func imapArity(args []object.Value, n int, name string) {
	if len(args) != n {
		raise("ArgumentError", "wrong number of arguments for '%s' (given %d, expected %d)", name, len(args), n)
	}
}

// imapArityMin raises ArgumentError when args has fewer than n elements.
func imapArityMin(args []object.Value, n int, name string) {
	if len(args) < n {
		raise("ArgumentError", "wrong number of arguments for '%s' (given %d, expected %d+)", name, len(args), n)
	}
}
