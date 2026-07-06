// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"bufio"
	"encoding/base64"
	"io"
	"strconv"
	"strings"

	imap "github.com/go-ruby-net-imap/net-imap"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin binding between rbgo's Ruby object graph (object.Value)
// and the interpreter-independent command builder + response parser of
// github.com/go-ruby-net-imap/net-imap. The IMAP4rev1 command encoding and the
// response grammar live in that library; rbgo only supplies the host seam — the
// socket / TLS byte transport — and maps Ruby command arguments onto the
// library's builder and the parsed responses back into the object graph
// (TaggedResponse / UntaggedResponse / FetchData / Envelope / MailboxList / … as
// Net::IMAP::* Ruby objects).
//
// Like the redis / pg bindings, the transport is an injected Ruby IO-like seam:
// any object responding to #read and #write (a StringIO, or rbgo's own
// TCPSocket / OpenSSL::SSL::SSLSocket — both respond to #read/#write, so a real
// networked Net::IMAP is Net::IMAP.new(connection: TCPSocket.new(host, port))).
// rubyConn (redis_bind.go) bridges that object to an io.ReadWriter; a bufio
// wrapper drives the library Reader's line / literal callbacks.

// IMAPObj is the Ruby wrapper around a live IMAP session (Net::IMAP). It owns no
// socket: it drives the builder + parser over the injected rubyConn seam,
// assembling and parsing responses as commands are issued.
type IMAPObj struct {
	cls     *RClass
	builder *imap.Builder
	reader  *imap.Reader
	// conn is the Ruby IO-like object backing the seam, kept so #disconnect can
	// close it and so the object stays reachable.
	conn object.Value
	rc   *rubyConn
	br   *bufio.Reader
	// greeting is the server's initial untagged response (Net::IMAP#greeting).
	greeting object.Value
	// responses records the untagged responses seen, keyed by name and preserving
	// arrival order, mirroring Net::IMAP#responses.
	responses     map[string][]object.Value
	responseNames []string
	disconnected  bool
	closed        bool
}

func (o *IMAPObj) ToS() string     { return "#<Net::IMAP>" }
func (o *IMAPObj) Inspect() string { return "#<Net::IMAP>" }
func (o *IMAPObj) Truthy() bool    { return true }

// newIMAPObj builds an IMAP session over the injected connection seam and wires
// the library Reader to the buffered line / literal readers.
func newIMAPObj(vm *VM, cls *RClass, conn object.Value) *IMAPObj {
	rc := &rubyConn{vm: vm, obj: conn}
	o := &IMAPObj{
		cls:       cls,
		builder:   imap.NewBuilder("RUBY"),
		conn:      conn,
		rc:        rc,
		br:        bufio.NewReader(rc),
		responses: map[string][]object.Value{},
	}
	o.reader = imap.NewReader(o.readLine, o.readLiteral)
	return o
}

// readLine reads the next CRLF-terminated line (including the terminator) from
// the seam, the library Reader's ReadLineFunc.
func (o *IMAPObj) readLine() (string, error) {
	return o.br.ReadString('\n')
}

// readLiteral reads exactly n literal bytes from the seam, the library Reader's
// ReadLiteralFunc.
func (o *IMAPObj) readLiteral(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := io.ReadFull(o.br, buf); err != nil {
		return "", err
	}
	return string(buf), nil
}

// write sends s to the seam (the client command bytes).
func (o *IMAPObj) write(s string) {
	_, _ = o.rc.Write([]byte(s))
}

// --- protocol driver -------------------------------------------------------

// mustCmd unwraps a builder result, raising Net::IMAP::DataFormatError for an
// argument the command encoder rejects.
func mustCmd(cmd imap.Command, err error) imap.Command {
	if err != nil {
		raise("Net::IMAP::DataFormatError", "%s", err.Error())
	}
	return cmd
}

// readOne reads and parses one response off the seam, raising the gem-faithful
// error for a transport fault (Net::IMAP::Error) or a malformed response
// (Net::IMAP::ResponseParseError).
func (o *IMAPObj) readOne() imap.Response {
	resp, err := o.reader.ReadResponse()
	if err != nil {
		o.disconnected = true
		if strings.Contains(err.Error(), "parse error") {
			raise("Net::IMAP::ResponseParseError", "%s", err.Error())
		}
		raise("Net::IMAP::Error", "%s", err.Error())
	}
	return resp
}

// imapExec writes a built command — interleaving each literal segment after the
// server's continuation request — then reads responses until the tagged response
// with cmd.Tag arrives. It returns that tagged response and the untagged
// responses collected on the way, raising for a NO / BAD tagged status.
func (o *IMAPObj) imapExec(vm *VM, cmd imap.Command) (*imap.TaggedResponse, []*imap.UntaggedResponse) {
	o.write(cmd.Bytes)
	for _, seg := range cmd.Literals {
		o.awaitContinuation()
		o.write(seg.Data)
		o.write(seg.Tail)
	}
	return o.readUntilTagged(vm, cmd.Tag)
}

// awaitContinuation reads one response, requiring it to be a continuation
// request (the server's go-ahead for a literal or SASL step).
func (o *IMAPObj) awaitContinuation() *imap.ContinuationRequest {
	cr, ok := o.readOne().(*imap.ContinuationRequest)
	if !ok {
		raise("Net::IMAP::ResponseParseError", "expected continuation request")
	}
	return cr
}

// readUntilTagged reads and records responses until the tagged response with tag
// arrives, returning it plus the untagged responses gathered.
func (o *IMAPObj) readUntilTagged(vm *VM, tag string) (*imap.TaggedResponse, []*imap.UntaggedResponse) {
	var untagged []*imap.UntaggedResponse
	for {
		switch r := o.readOne().(type) {
		case *imap.TaggedResponse:
			if r.Tag == tag {
				o.checkStatus(r)
				return r, untagged
			}
		case *imap.UntaggedResponse:
			untagged = append(untagged, r)
			o.recordUntagged(vm, r)
			if r.Name == "BYE" {
				o.disconnected = true
			}
		case *imap.ContinuationRequest:
			raise("Net::IMAP::ResponseParseError", "unexpected continuation request")
		}
	}
}

// checkStatus raises the gem-faithful error for a NO (NoResponseError) or BAD
// (BadResponseError) tagged status; OK (and any other) returns cleanly.
func (o *IMAPObj) checkStatus(r *imap.TaggedResponse) {
	switch r.Name {
	case "NO":
		raise("Net::IMAP::NoResponseError", "%s", respTextStr(r.Data))
	case "BAD":
		raise("Net::IMAP::BadResponseError", "%s", respTextStr(r.Data))
	}
}

// recordUntagged appends the mapped Data of an untagged response to the
// responses record, preserving arrival order (Net::IMAP#responses).
func (o *IMAPObj) recordUntagged(vm *VM, r *imap.UntaggedResponse) {
	if _, ok := o.responses[r.Name]; !ok {
		o.responseNames = append(o.responseNames, r.Name)
	}
	o.responses[r.Name] = append(o.responses[r.Name], vm.imapDataValue(r.Data))
}

// readGreeting reads the server's opening untagged response, storing it as the
// greeting; a BYE greeting marks the session disconnected and raises
// ByeResponseError, a non-untagged greeting raises ResponseParseError.
func (o *IMAPObj) readGreeting(vm *VM) {
	u, ok := o.readOne().(*imap.UntaggedResponse)
	if !ok {
		raise("Net::IMAP::ResponseParseError", "invalid greeting")
	}
	o.greeting = vm.imapUntagged(u)
	o.recordUntagged(vm, u)
	if u.Name == "BYE" {
		o.disconnected = true
		raise("Net::IMAP::ByeResponseError", "%s", untaggedText(u))
	}
}

// --- response value model -> Ruby ------------------------------------------

// imapDataValue maps an untagged response's Data payload to a Ruby value, per
// the library's documented per-name Data types.
func (vm *VM) imapDataValue(d any) object.Value {
	switch v := d.(type) {
	case int64:
		return object.IntValue(v)
	case string:
		return object.NewString(v)
	case []int64:
		return imapIntArray(v)
	case []string:
		return imapStrArray(v)
	case []imap.Flag:
		return vm.imapFlagArray(v)
	case *imap.ResponseText:
		return vm.imapRespText(v)
	case *imap.MailboxList:
		return vm.imapMailboxList(v)
	case *imap.StatusData:
		return vm.imapStatusData(v)
	case *imap.FetchData:
		return vm.imapFetchData(v)
	default:
		return object.NilV
	}
}

// imapUntagged builds a Net::IMAP::UntaggedResponse.
func (vm *VM) imapUntagged(r *imap.UntaggedResponse) object.Value {
	return vm.imapStruct("Net::IMAP::UntaggedResponse", map[string]object.Value{
		"name":     object.NewString(r.Name),
		"data":     vm.imapDataValue(r.Data),
		"raw_data": object.NewString(r.RawData),
	})
}

// imapTagged builds a Net::IMAP::TaggedResponse.
func (vm *VM) imapTagged(r *imap.TaggedResponse) object.Value {
	return vm.imapStruct("Net::IMAP::TaggedResponse", map[string]object.Value{
		"tag":      object.NewString(r.Tag),
		"name":     object.NewString(r.Name),
		"data":     vm.imapRespText(r.Data),
		"raw_data": object.NewString(r.RawData),
	})
}

// imapRespText builds a Net::IMAP::ResponseText (nil -> nil).
func (vm *VM) imapRespText(t *imap.ResponseText) object.Value {
	if t == nil {
		return object.NilV
	}
	var code object.Value = object.NilV
	if t.Code != nil {
		code = vm.imapRespCode(t.Code)
	}
	return vm.imapStruct("Net::IMAP::ResponseText", map[string]object.Value{
		"code": code,
		"text": object.NewString(t.Text),
	})
}

// imapRespCode builds a Net::IMAP::ResponseCode.
func (vm *VM) imapRespCode(c *imap.ResponseCode) object.Value {
	return vm.imapStruct("Net::IMAP::ResponseCode", map[string]object.Value{
		"name": object.NewString(c.Name),
		"data": vm.imapCodeData(c.Data),
	})
}

// imapCodeData maps a resp-text-code's Data (per respTextCode's grammar) to Ruby.
func (vm *VM) imapCodeData(d any) object.Value {
	switch v := d.(type) {
	case int64:
		return object.IntValue(v)
	case string:
		return object.NewString(v)
	case []string:
		return imapStrArray(v)
	case []imap.Flag:
		return vm.imapFlagArray(v)
	case *imap.AppendUIDData:
		return vm.imapStruct("Net::IMAP::AppendUIDData", map[string]object.Value{
			"uidvalidity":   object.IntValue(v.UIDValidity),
			"assigned_uids": object.NewString(v.AssignedUIDs),
		})
	case *imap.CopyUIDData:
		return vm.imapStruct("Net::IMAP::CopyUIDData", map[string]object.Value{
			"uidvalidity":   object.IntValue(v.UIDValidity),
			"source_uids":   object.NewString(v.SourceUIDs),
			"assigned_uids": object.NewString(v.AssignedUIDs),
		})
	default:
		return object.NilV
	}
}

// imapMailboxList builds a Net::IMAP::MailboxList (LIST / LSUB data).
func (vm *VM) imapMailboxList(m *imap.MailboxList) object.Value {
	return vm.imapStruct("Net::IMAP::MailboxList", map[string]object.Value{
		"attr":  vm.imapFlagArray(m.Attr),
		"delim": imapStrOrNil(m.Delim),
		"name":  object.NewString(m.Name),
	})
}

// imapStatusData builds a Net::IMAP::StatusData (STATUS data); attr is an
// insertion-ordered Hash of item -> Integer.
func (vm *VM) imapStatusData(m *imap.StatusData) object.Value {
	h := object.NewHash()
	for _, k := range m.Attr.Keys() {
		v, _ := m.Attr.Get(k)
		h.Set(object.NewString(k), object.IntValue(v))
	}
	return vm.imapStruct("Net::IMAP::StatusData", map[string]object.Value{
		"mailbox": object.NewString(m.Mailbox),
		"attr":    h,
	})
}

// imapFetchData builds a Net::IMAP::FetchData (FETCH data); attr is an
// insertion-ordered Hash of attribute name -> parsed value.
func (vm *VM) imapFetchData(m *imap.FetchData) object.Value {
	h := object.NewHash()
	for _, k := range m.Attr.Keys() {
		v, _ := m.Attr.Get(k)
		h.Set(object.NewString(k), vm.imapAttrValue(v))
	}
	return vm.imapStruct("Net::IMAP::FetchData", map[string]object.Value{
		"seqno": object.IntValue(m.Seqno),
		"attr":  h,
	})
}

// imapAttrValue maps one FETCH attribute value to Ruby, per fetch.go's per-att
// value types.
func (vm *VM) imapAttrValue(val any) object.Value {
	switch v := val.(type) {
	case int64:
		return object.IntValue(v)
	case string:
		return object.NewString(v)
	case []imap.Flag:
		return vm.imapFlagArray(v)
	case *imap.Envelope:
		return vm.imapEnvelope(v)
	case *imap.BodyStructure:
		return vm.imapBodyStructure(v)
	default:
		return object.NilV
	}
}

// imapEnvelope builds a Net::IMAP::Envelope; string members are nil for a NIL
// wire form, address-list members nil for a NIL list.
func (vm *VM) imapEnvelope(e *imap.Envelope) object.Value {
	return vm.imapStruct("Net::IMAP::Envelope", map[string]object.Value{
		"date":        imapStrOrNil(e.Date),
		"subject":     imapStrOrNil(e.Subject),
		"from":        vm.imapAddressList(e.From),
		"sender":      vm.imapAddressList(e.Sender),
		"reply_to":    vm.imapAddressList(e.ReplyTo),
		"to":          vm.imapAddressList(e.To),
		"cc":          vm.imapAddressList(e.Cc),
		"bcc":         vm.imapAddressList(e.Bcc),
		"in_reply_to": imapStrOrNil(e.InReplyTo),
		"message_id":  imapStrOrNil(e.MessageID),
	})
}

// imapAddressList maps an ENVELOPE address list to an Array of Net::IMAP::Address
// (nil for a NIL list).
func (vm *VM) imapAddressList(as []imap.Address) object.Value {
	if as == nil {
		return object.NilV
	}
	elems := make([]object.Value, len(as))
	for i, a := range as {
		elems[i] = vm.imapAddress(a)
	}
	return object.NewArrayFromSlice(elems)
}

// imapAddress builds a Net::IMAP::Address; a "" field maps to nil (a NIL wire
// form).
func (vm *VM) imapAddress(a imap.Address) object.Value {
	return vm.imapStruct("Net::IMAP::Address", map[string]object.Value{
		"name":    imapStrOrNil(a.Name),
		"route":   imapStrOrNil(a.Route),
		"mailbox": imapStrOrNil(a.Mailbox),
		"host":    imapStrOrNil(a.Host),
	})
}

// imapBodyStructure builds the Net::IMAP body-structure object of the class the
// library's BodyType() reports (BodyTypeMultipart / BodyTypeMessage /
// BodyTypeText / BodyTypeBasic) over the loss-free field tree.
func (vm *VM) imapBodyStructure(b *imap.BodyStructure) object.Value {
	qualified := "Net::IMAP::" + b.BodyType()
	if b.Multipart {
		parts := make([]object.Value, len(b.Parts))
		for i, p := range b.Parts {
			parts[i] = vm.imapBodyStructure(p)
		}
		return vm.imapStruct(qualified, map[string]object.Value{
			"media_type": object.NewString("MULTIPART"),
			"subtype":    object.NewString(b.MultipartSubtype),
			"parts":      object.NewArrayFromSlice(parts),
			"param":      object.NilV,
			"multipart":  object.Bool(true),
		})
	}
	return vm.imapStruct(qualified, map[string]object.Value{
		"media_type":  object.NewString(b.MediaType),
		"subtype":     object.NewString(b.Subtype),
		"param":       vm.imapParams(b.Params),
		"content_id":  imapStrOrNil(b.ContentID),
		"description": imapStrOrNil(b.Description),
		"encoding":    imapStrOrNil(b.Encoding),
		"size":        object.IntValue(b.Size),
		"lines":       object.IntValue(b.Lines),
		"multipart":   object.Bool(false),
	})
}

// imapParams maps a body-fld-param OrderedAttr to a Ruby Hash (nil -> nil).
func (vm *VM) imapParams(o *imap.OrderedAttr) object.Value {
	if o == nil {
		return object.NilV
	}
	h := object.NewHash()
	for _, k := range o.Keys() {
		v, _ := o.Get(k)
		h.Set(object.NewString(k), vm.imapAttrValue(v))
	}
	return h
}

// imapFlagArray maps a flag list to a Ruby Array of Symbols (the way MRI reports
// system flags, e.g. :Seen).
func (vm *VM) imapFlagArray(fs []imap.Flag) object.Value {
	elems := make([]object.Value, len(fs))
	for i, f := range fs {
		elems[i] = object.Symbol(string(f))
	}
	return object.NewArrayFromSlice(elems)
}

// imapIntArray maps an int64 slice to a Ruby Array of Integer.
func imapIntArray(ns []int64) object.Value {
	elems := make([]object.Value, len(ns))
	for i, n := range ns {
		elems[i] = object.IntValue(n)
	}
	return object.NewArrayFromSlice(elems)
}

// imapStrArray maps a string slice to a Ruby Array of String.
func imapStrArray(ss []string) object.Value {
	elems := make([]object.Value, len(ss))
	for i, s := range ss {
		elems[i] = object.NewString(s)
	}
	return object.NewArrayFromSlice(elems)
}

// imapStrOrNil maps a string to a Ruby String, or nil for the "" the library
// uses to denote a NIL wire form.
func imapStrOrNil(s string) object.Value {
	if s == "" {
		return object.NilV
	}
	return object.NewString(s)
}

// imapStruct instantiates a Net::IMAP::* value class and seeds its instance
// variables from fields (each keyed by its bare reader name).
func (vm *VM) imapStruct(qualified string, fields map[string]object.Value) object.Value {
	obj := &RObject{class: vm.consts[qualified].(*RClass), ivars: map[string]object.Value{}}
	for k, v := range fields {
		obj.ivars["@"+k] = v
	}
	return obj
}

// imapCollect maps the untagged responses named name to an Array of their mapped
// Data values (the per-command result set for LIST / FETCH / EXPUNGE / …).
func (vm *VM) imapCollect(untagged []*imap.UntaggedResponse, name string) *object.Array {
	var elems []object.Value
	for _, u := range untagged {
		if u.Name == name {
			elems = append(elems, vm.imapDataValue(u.Data))
		}
	}
	return object.NewArrayFromSlice(elems)
}

// --- Ruby argument coercion ------------------------------------------------

// respTextStr returns the human text of a status response's ResponseText, or "".
func respTextStr(t *imap.ResponseText) string {
	if t == nil {
		return ""
	}
	return t.Text
}

// untaggedText returns the text of an untagged status response (OK/NO/BYE/…).
func untaggedText(r *imap.UntaggedResponse) string {
	if rt, ok := r.Data.(*imap.ResponseText); ok {
		return rt.Text
	}
	return ""
}

// imapStrArg coerces a Ruby value to a Go string (String verbatim, else to_s).
func imapStrArg(v object.Value) string {
	if s, ok := v.(*object.String); ok {
		return s.Str()
	}
	return v.ToS()
}

// imapFlags coerces a Ruby value (a single flag or an Array of flags, each a
// Symbol or String) to a []imap.Flag.
func imapFlags(v object.Value) []imap.Flag {
	if arr, ok := v.(*object.Array); ok {
		out := make([]imap.Flag, len(arr.Elems))
		for i, e := range arr.Elems {
			out[i] = imap.Flag(imapStrArg(e))
		}
		return out
	}
	return []imap.Flag{imap.Flag(imapStrArg(v))}
}

// imapAtts coerces a Ruby value (a single attribute or an Array of them, each a
// Symbol or String) to a []string for a FETCH / STATUS attribute list.
func imapAtts(v object.Value) []string {
	if arr, ok := v.(*object.Array); ok {
		out := make([]string, len(arr.Elems))
		for i, e := range arr.Elems {
			out[i] = imapStrArg(e)
		}
		return out
	}
	return []string{imapStrArg(v)}
}

// imapSearchArgs coerces the Ruby SEARCH keys to library arguments: an Integer is
// a number, an Array is a sequence set, and anything else (String / Symbol) is a
// raw search-key token.
func imapSearchArgs(vals []object.Value) []imap.Argument {
	out := make([]imap.Argument, len(vals))
	for i, v := range vals {
		switch x := v.(type) {
		case object.Integer:
			out[i] = int64(x)
		case *object.Array:
			out[i] = imapSeqSet(v)
		default:
			out[i] = imapStrArg(v)
		}
	}
	return out
}

// imapSeqSet coerces a Ruby sequence-set argument to a *imap.SequenceSet. It
// accepts an Integer (0 or "*" meaning the last message), a Range (a..b, a.. for
// n:*), an Array of Integers / Ranges, and a String ("1", "1:3", "1,3:5,7:*").
// It raises Net::IMAP::DataFormatError for an out-of-range value.
func imapSeqSet(v object.Value) *imap.SequenceSet {
	items := imapSeqItems(v)
	ss, err := imap.NewSequenceSet(items...)
	if err != nil {
		raise("Net::IMAP::DataFormatError", "%s", err.Error())
	}
	return ss
}

// imapSeqItems flattens a Ruby sequence-set argument to the []any NewSequenceSet
// consumes.
func imapSeqItems(v object.Value) []any {
	switch x := v.(type) {
	case object.Integer:
		return []any{int64(x)}
	case *object.Range:
		return []any{imapSeqRange(x)}
	case *object.Array:
		var out []any
		for _, e := range x.Elems {
			out = append(out, imapSeqItems(e)...)
		}
		return out
	case *object.String:
		return imapSeqStringItems(x.Str())
	default:
		return []any{int64(0)}
	}
}

// imapSeqRange maps a Ruby Range to an imap.SeqRange; a nil (endless) upper bound
// becomes "*".
func imapSeqRange(r *object.Range) imap.SeqRange {
	lo := imapSeqBound(r.Lo)
	hi := imapSeqBound(r.Hi)
	return imap.SeqRange{Lo: lo, Hi: hi}
}

// imapSeqBound reads a Range endpoint as an int64 sequence number (nil -> "*").
func imapSeqBound(v object.Value) int64 {
	if n, ok := v.(object.Integer); ok {
		return int64(n)
	}
	return imap.Star
}

// imapSeqStringItems parses a sequence-set string ("1", "1:3", "1,3:5,7:*") into
// NewSequenceSet items; "*" maps to the library's Star.
func imapSeqStringItems(s string) []any {
	var out []any
	for _, part := range strings.Split(s, ",") {
		if lo, hi, ok := strings.Cut(part, ":"); ok {
			out = append(out, imap.SeqRange{Lo: imapSeqNum(lo), Hi: imapSeqNum(hi)})
		} else {
			out = append(out, imapSeqNum(part))
		}
	}
	return out
}

// imapSeqNum parses one sequence-set token ("*" -> Star, digits -> the number,
// anything else -> Star as a lenient fallback).
func imapSeqNum(tok string) int64 {
	tok = strings.TrimSpace(tok)
	if tok == "*" {
		return imap.Star
	}
	if n, err := strconv.ParseInt(tok, 10, 64); err == nil {
		return n
	}
	return imap.Star
}

// imapAuth drives an AUTHENTICATE exchange for the named SASL mechanism over the
// continuation seam, using the library's pure SASL encoders. It returns the
// tagged response, raising ArgumentError for an unsupported mechanism.
func (o *IMAPObj) imapAuth(vm *VM, mechanism string, creds []object.Value) *imap.TaggedResponse {
	cmd := mustCmd(o.builder.Authenticate(mechanism))
	o.write(cmd.Bytes)
	switch strings.ToUpper(mechanism) {
	case "PLAIN":
		authz, user, pass := imapPlainCreds(creds)
		o.awaitContinuation()
		o.write(imap.SASLEncode(imap.SASLPlain(authz, user, pass)) + imap.CRLF)
	case "LOGIN":
		user, pass := imapUserPass(creds)
		o.awaitContinuation()
		o.write(imap.SASLEncode(imap.SASLLoginUser(user)) + imap.CRLF)
		o.awaitContinuation()
		o.write(imap.SASLEncode(imap.SASLLoginPassword(pass)) + imap.CRLF)
	case "CRAM-MD5":
		user, pass := imapUserPass(creds)
		cr := o.awaitContinuation()
		challenge := imapDecodeChallenge(cr)
		o.write(imap.SASLEncode(imap.SASLCramMD5(user, pass, challenge)) + imap.CRLF)
	case "XOAUTH2":
		user, token := imapUserPass(creds)
		o.awaitContinuation()
		o.write(imap.SASLEncode(imap.SASLXOAuth2(user, token)) + imap.CRLF)
	default:
		raise("ArgumentError", "unsupported SASL mechanism: %s", mechanism)
	}
	tagged, _ := o.readUntilTagged(vm, cmd.Tag)
	return tagged
}

// imapPlainCreds reads PLAIN credentials: (username, password) or
// (authzid, username, password).
func imapPlainCreds(creds []object.Value) (authz, user, pass string) {
	switch len(creds) {
	case 3:
		return imapStrArg(creds[0]), imapStrArg(creds[1]), imapStrArg(creds[2])
	case 2:
		return "", imapStrArg(creds[0]), imapStrArg(creds[1])
	default:
		raise("ArgumentError", "PLAIN requires (username, password) or (authzid, username, password)")
		return "", "", ""
	}
}

// imapUserPass reads a two-argument credential pair (username, password/token).
func imapUserPass(creds []object.Value) (string, string) {
	if len(creds) != 2 {
		raise("ArgumentError", "mechanism requires (username, secret)")
	}
	return imapStrArg(creds[0]), imapStrArg(creds[1])
}

// imapDecodeChallenge base64-decodes the CRAM-MD5 server challenge carried in a
// continuation request's text.
func imapDecodeChallenge(cr *imap.ContinuationRequest) string {
	text := ""
	if cr.Data != nil {
		text = cr.Data.Text
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(text))
	if err != nil {
		raise("Net::IMAP::ResponseParseError", "invalid SASL challenge: %s", err.Error())
	}
	return string(raw)
}
