// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	stdtime "time"

	gotime "github.com/go-composites/time/src"
	mail "github.com/go-ruby-mail/mail"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin binding between rbgo's Ruby object graph and the
// interpreter-independent github.com/go-ruby-mail/mail parser/generator. The RFC
// 5322 / MIME parse and generation live in that library; rbgo only maps a raw
// message String (or a builder block that calls the setters) to a mail.New /
// mail.Read call and each result to a Mail::Message / Mail::Body / Mail::Field
// value object, so the mail-gem-faithful behaviour the Mail module relies on is
// preserved by construction. Delivery is a host seam and is not bound here.

// mailMsg is the accessor/builder view a Mail::Message shell delegates to,
// wrapping a *mail.Message so mail.go never imports the library type directly.
type mailMsg struct{ m *mail.Message }

func (m mailMsg) From() []string                  { return m.m.From() }
func (m mailMsg) To() []string                    { return m.m.To() }
func (m mailMsg) Cc() []string                    { return m.m.Cc() }
func (m mailMsg) Bcc() []string                   { return m.m.Bcc() }
func (m mailMsg) ReplyTo() []string               { return m.m.ReplyTo() }
func (m mailMsg) Subject() string                 { return m.m.Subject() }
func (m mailMsg) MessageID() string               { return m.m.MessageID() }
func (m mailMsg) InReplyTo() string               { return m.m.InReplyTo() }
func (m mailMsg) ContentType() string             { return m.m.ContentType() }
func (m mailMsg) MimeType() string                { return m.m.MimeType() }
func (m mailMsg) ContentTransferEncoding() string { return m.m.Field("Content-Transfer-Encoding") }
func (m mailMsg) ContentDescription() string      { return m.m.Field("Content-Description") }
func (m mailMsg) ContentDisposition() string      { return m.m.ContentDisposition() }
func (m mailMsg) ContentID() string               { return m.m.ContentID() }
func (m mailMsg) Charset() string                 { return m.m.Charset() }
func (m mailMsg) Filename() string                { return m.m.Filename() }
func (m mailMsg) Body() mailBody                  { return mailBody{b: m.m.Body()} }
func (m mailMsg) Multipart() bool                 { return m.m.Multipart() }
func (m mailMsg) IsAttachment() bool              { return m.m.IsAttachment() }
func (m mailMsg) Parts() []*mail.Part             { return m.m.Parts() }
func (m mailMsg) Attachments() []*mail.Part       { return m.m.Attachments() }
func (m mailMsg) TextPart() *mail.Part            { return m.m.TextPart() }
func (m mailMsg) HTMLPart() *mail.Part            { return m.m.HTMLPart() }
func (m mailMsg) Field(name string) string        { return m.m.Field(name) }
func (m mailMsg) Header() *mail.Header            { return m.m.Header() }
func (m mailMsg) Encoded() string                 { return m.m.Encoded() }
func (m mailMsg) Decoded() []byte                 { return m.m.Decoded() }
func (m mailMsg) Date() (stdtime.Time, bool)      { return m.m.Date() }
func (m mailMsg) DateString() string              { return m.m.Field("Date") }

func (m mailMsg) SetFrom(v string)        { m.m.SetFrom(v) }
func (m mailMsg) SetTo(v string)          { m.m.SetTo(v) }
func (m mailMsg) SetCc(v string)          { m.m.SetCc(v) }
func (m mailMsg) SetBcc(v string)         { m.m.SetBcc(v) }
func (m mailMsg) SetReplyTo(v string)     { m.m.SetReplyTo(v) }
func (m mailMsg) SetSubject(v string)     { m.m.SetSubject(v) }
func (m mailMsg) SetBody(v string)        { m.m.SetBody(v) }
func (m mailMsg) SetMessageID(v string)   { m.m.SetMessageID(v) }
func (m mailMsg) SetContentType(v string) { m.m.SetContentType(v) }
func (m mailMsg) SetDateString(v string)  { m.m.SetDateString(v) }

// mailBody is the accessor view a Mail::Body shell delegates to.
type mailBody struct{ b *mail.Body }

func (b mailBody) DecodedString() string { return b.b.DecodedString() }
func (b mailBody) Raw() string           { return b.b.Raw }
func (b mailBody) Encoding() string      { return b.b.Encoding }

// mailField is the accessor view a Mail::Field shell delegates to.
type mailField struct{ f *mail.Field }

func (f mailField) Name() string    { return f.f.Name }
func (f mailField) Value() string   { return f.f.Value }
func (f mailField) Decoded() string { return f.f.Decoded() }

// mailNew implements Mail.new: with a raw String argument it parses it; with a
// block it builds an empty message and runs the block against it (the from/to/
// subject/body DSL, evaluated with the message as self); with both it parses
// then runs the block. With neither it yields an empty message.
func mailNew(vm *VM, args []object.Value, blk *Proc) object.Value {
	raw := ""
	if len(args) > 0 {
		if _, isNil := object.AsNilOK(args[0]); !isNil {
			raw = strArg(args[0])
		}
	}
	msg := &MailMessage{m: mailMsg{m: mail.New(raw)}}
	if blk != nil {
		vm.callBlockSelf(blk, object.Wrap(msg), nil)
	}
	return object.Wrap(msg)
}

// mailRead implements Mail.read: it reads and parses a message from a file,
// raising Errno::ENOENT when the file cannot be opened (matching the gem).
func mailRead(path string) object.Value {
	m, err := mail.Read(path)
	if err != nil {
		raise("Errno::ENOENT", "No such file or directory @ rb_sysopen - %s", path)
	}
	return object.Wrap(&MailMessage{m: mailMsg{m: m}})
}

// mailAddressValue renders an address field: nil when empty, the single String
// when the field carries exactly one address (the gem's convenience for a
// single-recipient field), the Array of Strings otherwise.
func mailAddressValue(addrs []string) object.Value {
	switch len(addrs) {
	case 0:
		return object.NilVal()
	case 1:
		return object.Wrap(object.NewString(addrs[0]))
	default:
		return strSliceToArray(addrs)
	}
}

// mailOptStr maps a possibly-empty field String to a Ruby String, or Ruby nil
// when empty (an absent field reads as nil in the gem).
func mailOptStr(s string) object.Value {
	if s == "" {
		return object.NilVal()
	}
	return object.Wrap(object.NewString(s))
}

// mailDateValue maps the Date: header to a Ruby Time, or Ruby nil when it is
// absent or unparseable.
func mailDateValue(m mailMsg) object.Value {
	t, ok := m.Date()
	if !ok {
		return object.NilVal()
	}
	return object.Wrap(&Time{t: gotime.FromUnix(t.Unix())})
}

// mailPartsArray wraps a slice of parts into a Ruby Array of Mail::Message
// objects (a part is modelled as a message), preserving order.
func mailPartsArray(parts []*mail.Part) object.Value {
	arr := object.NewArrayFromSlice(make([]object.Value, len(parts)))
	for i, p := range parts {
		arr.Elems[i] = object.Wrap(&MailMessage{m: mailMsg{m: p}})
	}
	return object.Wrap(arr)
}

// mailPartOrNil wraps a single part as a Mail::Message, or Ruby nil when the
// message has no such part.
func mailPartOrNil(p *mail.Part) object.Value {
	if p == nil {
		return object.NilVal()
	}
	return object.Wrap(&MailMessage{m: mailMsg{m: p}})
}

// mailFieldsArray wraps a message header's fields into a Ruby Array of
// Mail::Field value objects, preserving field order.
func mailFieldsArray(h *mail.Header) object.Value {
	fields := h.Fields()
	arr := object.NewArrayFromSlice(make([]object.Value, len(fields)))
	for i, f := range fields {
		arr.Elems[i] = object.Wrap(&MailField{f: mailField{f: f}})
	}
	return object.Wrap(arr)
}

// mailHeaderHash renders the message header as a Ruby Hash of field name to
// unfolded value, preserving field order (the last value wins for a repeated
// field, matching header[name] access). This backs Mail::Message#header for the
// common read-a-field-by-name use.
func mailHeaderHash(m mailMsg) object.Value {
	h := object.NewHash()
	for _, f := range m.Header().Fields() {
		h.Set(object.Wrap(object.NewString(f.Name)), object.Wrap(object.NewString(f.Value)))
	}
	return object.Wrap(h)
}
