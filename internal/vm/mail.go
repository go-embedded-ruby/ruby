// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// MailMessage wraps a *mail.Message as a Ruby Mail::Message object (the value
// Mail.new / Mail.read return and the block builder yields). The RFC 5322 / MIME
// parse and generation live in the github.com/go-ruby-mail/mail library; this
// shell only reports the Ruby class and delegates each accessor (see
// mail_bind.go). Delivery (SMTP/IMAP/POP) is a host seam and is not bound here.
type MailMessage struct{ m mailMsg }

func (m *MailMessage) ToS() string     { return m.m.Encoded() }
func (m *MailMessage) Inspect() string { return "#<Mail::Message>" }
func (m *MailMessage) Truthy() bool    { return true }

// MailBody wraps a message/part body as a Ruby Mail::Body object.
type MailBody struct{ b mailBody }

func (b *MailBody) ToS() string     { return b.b.DecodedString() }
func (b *MailBody) Inspect() string { return "#<Mail::Body>" }
func (b *MailBody) Truthy() bool    { return true }

// MailField wraps a single header field as a Ruby Mail::Field object.
type MailField struct{ f mailField }

func (f *MailField) ToS() string     { return f.f.Value() }
func (f *MailField) Inspect() string { return "#<Mail::Field 0x " + f.f.Name() + ">" }
func (f *MailField) Truthy() bool    { return true }

// registerMail installs the Mail module (require "mail"): Mail.new(raw) /
// Mail(raw) parse or build a message (a block runs the DSL builder), Mail.read
// reads one from a file, and the Mail::Message / Mail::Body / Mail::Field value
// classes carry the accessors. The parser, generator and DSL live in the
// go-ruby-mail library; this module is the thin wiring that maps a Ruby raw
// String (or builder block) to a mail.New / mail.Read call and the result to a
// Mail::Message (see mail_bind.go).
func (vm *VM) registerMail() {
	mod := newClass("Mail", nil)
	mod.isModule = true
	vm.consts["Mail"] = object.Wrap(mod)

	msgCls := newClass("Mail::Message", vm.cObject)
	mod.consts["Message"] = object.Wrap(msgCls)
	vm.consts["Mail::Message"] = object.Wrap(msgCls)
	// Mail::Part is Mail::Message (the library models a part as a message).
	mod.consts["Part"] = object.Wrap(msgCls)
	vm.consts["Mail::Part"] = object.Wrap(msgCls)
	vm.registerMailMessage(msgCls)

	bodyCls := newClass("Mail::Body", vm.cObject)
	mod.consts["Body"] = object.Wrap(bodyCls)
	vm.consts["Mail::Body"] = object.Wrap(bodyCls)
	vm.registerMailBody(bodyCls)

	fieldCls := newClass("Mail::Field", vm.cObject)
	mod.consts["Field"] = object.Wrap(fieldCls)
	vm.consts["Mail::Field"] = object.Wrap(fieldCls)
	vm.registerMailField(fieldCls)

	def := func(name string, fn NativeFn) { mod.smethods[name] = &Method{name: name, owner: mod, native: fn} }

	// Mail.new(raw = "") { … } parses a raw message String, or builds one when a
	// block is given (the block runs the from/to/subject/body DSL on the new
	// message). Mail.new with neither yields an empty message.
	newFn := func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		return mailNew(vm, args, blk)
	}
	def("new", newFn)

	// Mail.read(path) reads and parses a message from a file, raising
	// Errno::ENOENT when it cannot be opened (matching Mail.read).
	def("read", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		return mailRead(strArg(args[0]))
	})
}

// registerMailMessage installs the Mail::Message instance surface: the address /
// subject / body / date / message-id accessors, the MIME view (parts /
// attachments / multipart? / content_type / header) and the serialisers
// (encoded / to_s).
func (vm *VM) registerMailMessage(cls *RClass) {
	d := func(name string, fn NativeFn) { cls.define(name, fn) }
	self := func(v object.Value) mailMsg { return object.Kind[*MailMessage](v).m }

	// The address fields (from/to/cc/bcc/reply_to) are dual getter/setter DSL
	// methods, matching the gem: called with no argument they read the field
	// (returning a single String when it carries one address, the Array
	// otherwise, nil when absent); called with a String they set it and return
	// self (the `from "x@y"` form used inside a Mail.new block).
	addr := func(name string, get func(mailMsg) []string, set func(mailMsg, string)) {
		d(name, func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
			if len(args) > 0 {
				set(self(v), strArg(args[0]))
				return v
			}
			return mailAddressValue(get(self(v)))
		})
	}
	addr("from", mailMsg.From, mailMsg.SetFrom)
	addr("to", mailMsg.To, mailMsg.SetTo)
	addr("cc", mailMsg.Cc, mailMsg.SetCc)
	addr("bcc", mailMsg.Bcc, mailMsg.SetBcc)
	addr("reply_to", mailMsg.ReplyTo, mailMsg.SetReplyTo)

	// The single-value string fields are dual getter/setter DSL methods too:
	// a bare call reads the field (nil when absent), a call with a String sets it
	// and returns self. Fields with no setter (mime_type, filename, …) take only
	// the reader form.
	optStr := func(name string, get func(mailMsg) string, set func(mailMsg, string)) {
		d(name, func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
			if set != nil && len(args) > 0 {
				set(self(v), strArg(args[0]))
				return v
			}
			return mailOptStr(get(self(v)))
		})
	}
	optStr("subject", mailMsg.Subject, mailMsg.SetSubject)
	optStr("message_id", mailMsg.MessageID, mailMsg.SetMessageID)
	optStr("content_type", mailMsg.ContentType, mailMsg.SetContentType)
	optStr("date_string", mailMsg.DateString, mailMsg.SetDateString)
	optStr("in_reply_to", mailMsg.InReplyTo, nil)
	optStr("mime_type", mailMsg.MimeType, nil)
	optStr("content_transfer_encoding", mailMsg.ContentTransferEncoding, nil)
	optStr("content_description", mailMsg.ContentDescription, nil)
	optStr("content_disposition", mailMsg.ContentDisposition, nil)
	optStr("content_id", mailMsg.ContentID, nil)
	optStr("charset", mailMsg.Charset, nil)
	optStr("filename", mailMsg.Filename, nil)

	// body is a dual getter/setter DSL method: a bare call returns the Mail::Body
	// wrapper (decoded-on-demand), a call with a String sets the body and returns
	// self (the `body "…"` form used inside a Mail.new block).
	d("body", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) > 0 {
			self(v).SetBody(strArg(args[0]))
			return v
		}
		return object.Wrap(&MailBody{b: self(v).Body()})
	})
	// date returns a Ruby Time for the Date: header, or nil when it is absent or
	// unparseable.
	d("date", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return mailDateValue(self(v))
	})

	// The MIME view: multipart? / attachment? predicates, the parts and
	// attachments Arrays, and the text/html part convenience readers.
	d("multipart?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.BoolValue(bool(object.Bool(self(v).Multipart())))
	})
	d("attachment?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.BoolValue(bool(object.Bool(self(v).IsAttachment())))
	})
	d("parts", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return mailPartsArray(self(v).Parts())
	})
	d("attachments", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return mailPartsArray(self(v).Attachments())
	})
	d("text_part", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return mailPartOrNil(self(v).TextPart())
	})
	d("html_part", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return mailPartOrNil(self(v).HTMLPart())
	})

	// header returns the Mail::Header facade; header[name] is the raw value of
	// the named field (nil when absent) — the common gem shorthand.
	d("header", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return mailHeaderHash(self(v))
	})
	d("[]", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		return mailOptStr(self(v).Field(strArg(args[0])))
	})
	// header_fields returns the ordered Array of Mail::Field objects (the
	// name/value pairs of the header), the value-object view of the header.
	d("header_fields", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return mailFieldsArray(self(v).Header())
	})

	// encoded / to_s serialise the message back to its on-the-wire form.
	d("encoded", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(self(v).Encoded()))
	})
	d("to_s", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(self(v).Encoded()))
	})
	d("decoded", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(string(self(v).Decoded())))
	})

	// The builder setters (used inside a Mail.new block and callable directly):
	// each takes a single String and returns self, matching the gem's DSL.
	setter := func(name string, set func(mailMsg, string)) {
		d(name, func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
			if len(args) == 0 {
				raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
			}
			set(self(v), strArg(args[0]))
			return v
		})
	}
	setter("from=", mailMsg.SetFrom)
	setter("to=", mailMsg.SetTo)
	setter("cc=", mailMsg.SetCc)
	setter("bcc=", mailMsg.SetBcc)
	setter("reply_to=", mailMsg.SetReplyTo)
	setter("subject=", mailMsg.SetSubject)
	setter("body=", mailMsg.SetBody)
	setter("message_id=", mailMsg.SetMessageID)
	setter("content_type=", mailMsg.SetContentType)
	setter("date=", mailMsg.SetDateString)
}

// registerMailBody installs the Mail::Body instance surface: the decoded / raw
// views and to_s.
func (vm *VM) registerMailBody(cls *RClass) {
	d := func(name string, fn NativeFn) { cls.define(name, fn) }
	self := func(v object.Value) mailBody { return object.Kind[*MailBody](v).b }

	d("decoded", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(self(v).DecodedString()))
	})
	d("to_s", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(self(v).DecodedString()))
	})
	d("raw_source", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(self(v).Raw()))
	})
	d("encoding", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(self(v).Encoding()))
	})
}

// registerMailField installs the Mail::Field instance surface: the name / value /
// decoded readers.
func (vm *VM) registerMailField(cls *RClass) {
	d := func(name string, fn NativeFn) { cls.define(name, fn) }
	self := func(v object.Value) mailField { return object.Kind[*MailField](v).f }

	d("name", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(self(v).Name()))
	})
	d("value", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(self(v).Value()))
	})
	d("to_s", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(self(v).Value()))
	})
	d("decoded", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(self(v).Decoded()))
	})
}
