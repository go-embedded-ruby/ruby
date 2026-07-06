// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"strings"
	"time"

	netsmtp "github.com/go-ruby-net-smtp/net-smtp"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerNetSMTP installs Net::SMTP (require "net/smtp"): the session class, its
// Net::SMTP::Response view and message-stream adapter, and the Net::SMTPError
// exception family. The SMTP protocol (commands, replies, DATA dot-stuffing, SASL
// auth) is github.com/go-ruby-net-smtp/net-smtp; the byte I/O is rbgo's own
// socket transport (see net_smtp_bind.go). It runs after registerSocket /
// registerOpenSSL (the TCP + TLS layers it dials and upgrades through) and after
// registerNetHTTP (which creates the Net module and its transport helpers).
func (vm *VM) registerNetSMTP() {
	netMod := vm.consts["Net"].(*RClass)
	vm.registerNetSMTPErrors(netMod)

	smtp := newClass("Net::SMTP", vm.cObject)
	netMod.consts["SMTP"] = smtp
	vm.consts["Net::SMTP"] = smtp

	vm.registerSMTPResponse(smtp)
	vm.registerSMTPStream(smtp)
	vm.registerNetSMTPClassMethods(smtp)
	vm.registerNetSMTPConfig(smtp)
	vm.registerNetSMTPProtocol(smtp)
}

// registerNetSMTPErrors installs the Net::SMTPError family (SMTPError <
// ProtocolError < StandardError; the five reply-keyed kinds plus
// SMTPUnsupportedCommand < SMTPError), the classes Net::SMTP raises via
// check_response. Each maps one ErrorKind.RubyClass() name so a raised codec
// error rescues as its MRI-faithful class.
func (vm *VM) registerNetSMTPErrors(netMod *RClass) {
	std := vm.consts["StandardError"].(*RClass)
	proto := newClass("Net::ProtocolError", std)
	netMod.consts["ProtocolError"] = proto
	vm.consts["Net::ProtocolError"] = proto
	reg := func(simple string, super *RClass) *RClass {
		qualified := "Net::" + simple
		c := newClass(qualified, super)
		netMod.consts[simple] = c
		vm.consts[qualified] = c
		return c
	}
	base := reg("SMTPError", proto)
	reg("SMTPUnknownError", base)
	reg("SMTPServerBusy", base)
	reg("SMTPSyntaxError", base)
	reg("SMTPAuthenticationError", base)
	reg("SMTPFatalError", base)
	reg("SMTPUnsupportedCommand", base)
}

// SMTPResponseObj is the Ruby wrapper around a *netsmtp.Response (Net::SMTP::Response):
// the reply status, its full text, and the success?/continue? predicates.
type SMTPResponseObj struct {
	cls *RClass
	res *netsmtp.Response
}

func (r *SMTPResponseObj) ToS() string     { return r.res.String }
func (r *SMTPResponseObj) Inspect() string { return "#<Net::SMTP::Response " + r.res.Status + ">" }
func (r *SMTPResponseObj) Truthy() bool    { return true }

// smtpResponse wraps a codec reply in a Net::SMTP::Response Ruby object.
func (vm *VM) smtpResponse(res *netsmtp.Response) object.Value {
	return &SMTPResponseObj{cls: vm.consts["Net::SMTP::Response"].(*RClass), res: res}
}

// registerSMTPResponse installs Net::SMTP::Response (status / string / message /
// success? / continue?), mirroring the reply object MRI returns from
// send_message and the command helpers.
func (vm *VM) registerSMTPResponse(smtp *RClass) {
	rc := newClass("Net::SMTP::Response", vm.cObject)
	smtp.consts["Response"] = rc
	vm.consts["Net::SMTP::Response"] = rc
	res := func(v object.Value) *netsmtp.Response { return v.(*SMTPResponseObj).res }
	rc.define("status", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(res(self).Status)
	})
	rc.define("string", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(res(self).String)
	})
	rc.define("message", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(res(self).Message())
	})
	rc.define("success?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(res(self).Success())
	})
	rc.define("continue?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(res(self).Continue())
	})
}

// SMTPStreamObj is the writable adapter Net::SMTP#open_message_stream (and the
// block form of #data) yields: #print / #puts / #write / #<< append to a buffer
// that becomes the DATA payload once the block returns.
type SMTPStreamObj struct {
	cls *RClass
	buf *strings.Builder
}

func (s *SMTPStreamObj) ToS() string     { return "#<Net::SMTP::Adapter>" }
func (s *SMTPStreamObj) Inspect() string { return "#<Net::SMTP::Adapter>" }
func (s *SMTPStreamObj) Truthy() bool    { return true }

// newSMTPStream builds a fresh message-stream adapter.
func (vm *VM) newSMTPStream() *SMTPStreamObj {
	return &SMTPStreamObj{cls: vm.consts["Net::SMTP::Adapter"].(*RClass), buf: &strings.Builder{}}
}

// registerSMTPStream installs the Net::SMTP::Adapter message-stream class with the
// IO-like write surface open_message_stream yields.
func (vm *VM) registerSMTPStream(smtp *RClass) {
	rc := newClass("Net::SMTP::Adapter", vm.cObject)
	smtp.consts["Adapter"] = rc
	vm.consts["Net::SMTP::Adapter"] = rc
	buf := func(v object.Value) *strings.Builder { return v.(*SMTPStreamObj).buf }

	// #write(*args) appends each argument and returns the total byte count.
	rc.define("write", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		b := buf(self)
		n := 0
		for _, a := range args {
			s := smtpStr(a)
			b.WriteString(s)
			n += len(s)
		}
		return object.IntValue(int64(n))
	})
	// #print(*args) appends each argument without a separator and returns nil.
	rc.define("print", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		b := buf(self)
		for _, a := range args {
			b.WriteString(smtpStr(a))
		}
		return object.NilV
	})
	// #puts(*args) appends each argument, adding a newline where one is missing;
	// with no argument it appends a lone newline (Ruby IO#puts).
	rc.define("puts", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		b := buf(self)
		if len(args) == 0 {
			b.WriteString("\n")
			return object.NilV
		}
		for _, a := range args {
			s := smtpStr(a)
			b.WriteString(s)
			if !strings.HasSuffix(s, "\n") {
				b.WriteString("\n")
			}
		}
		return object.NilV
	})
	// #<<(obj) appends obj and returns self for chaining (Ruby IO#<<).
	rc.define("<<", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		buf(self).WriteString(smtpStr(args[0]))
		return self
	})
}

// registerNetSMTPClassMethods installs Net::SMTP.new / .start plus the default
// port constants.
func (vm *VM) registerNetSMTPClassMethods(smtp *RClass) {
	sm := func(name string, fn NativeFn) {
		smtp.smethods[name] = &Method{name: name, owner: smtp, native: fn}
	}
	sm("new", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.smtpNew(smtp, args)
	})
	sm("start", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		return vm.smtpClassStart(smtp, args, blk)
	})
	port := func(name string, n int64) {
		sm(name, func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
			return object.IntValue(n)
		})
	}
	port("default_port", 25)
	port("default_tls_port", 465)
	port("default_ssl_port", 465)
	port("default_submission_port", 587)
}

// smtpNew builds an unstarted Net::SMTP for an address and optional port
// (default 25), with the modern defaults (ESMTP on, STARTTLS auto).
func (vm *VM) smtpNew(cls *RClass, args []object.Value) *SMTPObj {
	if len(args) < 1 {
		raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
	}
	o := &SMTPObj{cls: cls, address: strArg(args[0]), port: "25", esmtp: true, starttls: smtpStartTLSAuto}
	if len(args) >= 2 && !object.IsNil(args[1]) {
		o.port = portString(args[1])
	}
	return o
}

// smtpClassStart implements Net::SMTP.start: build an instance, open the session,
// and either yield it to a block (closing it afterwards) or return it started.
// The address/port and the helo/user/secret/authtype credentials come from the
// positional arguments; a trailing options Hash may instead carry them by keyword
// (helo:, user:, secret:/password:, authtype:) along with tls: / starttls:.
func (vm *VM) smtpClassStart(cls *RClass, args []object.Value, blk *Proc) object.Value {
	kw, args := smtpSplitKw(args)
	if len(args) < 1 {
		raise("ArgumentError", "wrong number of arguments (given 0, expected 1+)")
	}
	o := &SMTPObj{cls: cls, address: strArg(args[0]), port: "25", esmtp: true, starttls: smtpStartTLSAuto}
	if len(args) >= 2 && !object.IsNil(args[1]) {
		o.port = portString(args[1])
	}
	var rest []object.Value
	if len(args) > 2 {
		rest = args[2:]
	}
	helo, user, secret, authtype := vm.smtpStartCreds(rest, kw, o)
	vm.smtpDoStart(o, helo, user, secret, authtype)
	return vm.smtpYield(o, blk)
}

// smtpSplitKw peels a trailing options Hash off an argument list.
func smtpSplitKw(args []object.Value) (*object.Hash, []object.Value) {
	if len(args) > 0 {
		if h, ok := args[len(args)-1].(*object.Hash); ok {
			return h, args[:len(args)-1]
		}
	}
	return nil, args
}

// smtpStartCreds reads the helo domain and credentials from the (post-address)
// positional tail and/or the keyword Hash, and applies the tls:/starttls:
// keywords to o. rest is the positionals after address/port: helo, user, secret,
// authtype.
func (vm *VM) smtpStartCreds(rest []object.Value, kw *object.Hash, o *SMTPObj) (helo string, user, secret, authtype object.Value) {
	helo, user, secret, authtype = "localhost", object.NilV, object.NilV, object.NilV
	if len(rest) >= 1 && !object.IsNil(rest[0]) {
		helo = strArg(rest[0])
	}
	if len(rest) >= 2 {
		user = rest[1]
	}
	if len(rest) >= 3 {
		secret = rest[2]
	}
	if len(rest) >= 4 {
		authtype = rest[3]
	}
	if kw != nil {
		if v, ok := smtpKw(kw, "helo"); ok && !object.IsNil(v) {
			helo = strArg(v)
		}
		if v, ok := smtpKw(kw, "user"); ok {
			user = v
		}
		if v, ok := smtpKw(kw, "secret"); ok {
			secret = v
		}
		if v, ok := smtpKw(kw, "password"); ok {
			secret = v
		}
		if v, ok := smtpKw(kw, "authtype"); ok {
			authtype = v
		}
		if v, ok := smtpKw(kw, "tls"); ok && v.Truthy() {
			o.tls = true
		}
		if v, ok := smtpKw(kw, "starttls"); ok {
			o.starttls = smtpStartTLSMode(v)
		}
	}
	return helo, user, secret, authtype
}

// smtpYield closes over the started session: with a block it yields the instance,
// finishes the session and returns the block's value; without one it returns the
// started instance.
func (vm *VM) smtpYield(o *SMTPObj, blk *Proc) object.Value {
	if blk != nil {
		res := vm.callBlock(blk, []object.Value{o})
		vm.smtpFinishQuiet(o)
		return res
	}
	return o
}

// smtpDoStart opens the session (Net::SMTP#do_start): dial, read and check the
// greeting, EHLO (falling back to HELO), optional STARTTLS + re-EHLO, and
// optional AUTH. Any failure closes the socket before raising.
func (vm *VM) smtpDoStart(o *SMTPObj, helo string, user, secret, authtype object.Value) {
	if o.started {
		raise("IOError", "SMTP session already started")
	}
	if err := vm.smtpDial(o); err != nil {
		raise("SocketError", "%s", err.Error())
	}
	o.deadline()
	greeting, err := o.sess.RecvResponse()
	if err != nil {
		o.closeConn()
		smtpRaiseErr(err)
	}
	if !greeting.Success() {
		o.closeConn()
		smtpRaiseErr(smtpRespErr(greeting))
	}
	vm.smtpHello(o, helo)
	if !o.tls && (o.starttls == smtpStartTLSAlways || (o.starttls == smtpStartTLSAuto && o.capableStartTLS())) {
		if _, err := o.sess.StartTLS(); err != nil {
			o.closeConn()
			smtpRaiseErr(err)
		}
		vm.smtpHello(o, helo)
	}
	if !object.IsNil(user) && !object.IsNil(secret) {
		vm.smtpAuthenticate(o, strArg(user), strArg(secret), authtype)
	}
	o.started = true
}

// smtpHello sends EHLO (Net::SMTP#do_helo): on an ESMTP session it EHLOs and, if
// the server refuses, falls back to HELO and drops out of ESMTP mode; a
// non-ESMTP session HELOs directly. A transport error (not an SMTP reply) closes
// the socket and raises.
func (vm *VM) smtpHello(o *SMTPObj, helo string) {
	o.deadline()
	if o.esmtp {
		if _, err := o.sess.Ehlo(helo); err != nil {
			if _, ok := err.(*netsmtp.SMTPError); !ok {
				o.closeConn()
				smtpRaiseErr(err)
			}
			o.esmtp = false
			if _, herr := o.sess.Helo(helo); herr != nil {
				o.closeConn()
				smtpRaiseErr(herr)
			}
		}
		return
	}
	if _, err := o.sess.Helo(helo); err != nil {
		o.closeConn()
		smtpRaiseErr(err)
	}
}

// smtpAuthenticate runs the SASL exchange for the given authtype (default :plain),
// mapping to the codec's PLAIN / LOGIN / CRAM-MD5 driver. An unknown type raises
// ArgumentError; an auth failure closes the socket and raises the codec error.
func (vm *VM) smtpAuthenticate(o *SMTPObj, user, secret string, authtype object.Value) {
	kind := "plain"
	if !object.IsNil(authtype) {
		kind = smtpAuthTypeName(authtype)
	}
	o.deadline()
	var err error
	switch kind {
	case "plain":
		_, err = o.sess.AuthPlain(user, secret)
	case "login":
		_, err = o.sess.AuthLogin(user, secret)
	case "cram_md5":
		_, err = o.sess.AuthCramMD5(user, secret)
	default:
		raise("ArgumentError", "unknown auth type: %s", kind)
	}
	if err != nil {
		o.closeConn()
		smtpRaiseErr(err)
	}
}

// smtpFinishQuiet closes a session (Net::SMTP#do_finish): QUIT best-effort, close
// the socket and clear the driver.
func (vm *VM) smtpFinishQuiet(o *SMTPObj) {
	if o.sess != nil {
		_, _ = o.sess.Quit()
	}
	o.closeConn()
	o.started = false
	o.sess = nil
	o.conn = nil
}

// registerNetSMTPConfig installs the configuration surface: the address/port
// readers, the ESMTP toggle, the TLS/STARTTLS mode setters and predicates, the
// timeouts, the started? flag, and the capability predicates.
func (vm *VM) registerNetSMTPConfig(smtp *RClass) {
	obj := func(v object.Value) *SMTPObj { return v.(*SMTPObj) }
	d := func(name string, fn NativeFn) { smtp.define(name, fn) }

	d("address", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(obj(self).address)
	})
	d("port", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(obj(self).port)
	})
	d("started?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(obj(self).started)
	})
	d("esmtp?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(obj(self).esmtp)
	})
	d("esmtp", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(obj(self).esmtp)
	})
	d("esmtp=", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		obj(self).esmtp = args[0].Truthy()
		return args[0]
	})

	// TLS / STARTTLS state.
	d("tls?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(obj(self).tls)
	})
	d("ssl?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(obj(self).tls)
	})
	d("starttls?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(obj(self).starttls != smtpStartTLSOff)
	})
	d("starttls_auto?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(obj(self).starttls == smtpStartTLSAuto)
	})
	d("starttls_always?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(obj(self).starttls == smtpStartTLSAlways)
	})

	// enable_tls / enable_ssl: implicit TLS on connect; exclusive with STARTTLS-always.
	enableTLS := func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		o := obj(self)
		if o.starttls == smtpStartTLSAlways {
			raise("ArgumentError", "SMTPS and STARTTLS is exclusive")
		}
		o.tls = true
		return object.NilV
	}
	d("enable_tls", enableTLS)
	d("enable_ssl", enableTLS)
	disableTLS := func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		obj(self).tls = false
		return object.NilV
	}
	d("disable_tls", disableTLS)
	d("disable_ssl", disableTLS)

	// enable_starttls[_auto] / disable_starttls: exclusive with implicit TLS.
	d("enable_starttls", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		o := obj(self)
		if o.tls {
			raise("ArgumentError", "SMTPS and STARTTLS is exclusive")
		}
		o.starttls = smtpStartTLSAlways
		return object.NilV
	})
	d("enable_starttls_auto", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		o := obj(self)
		if o.tls {
			raise("ArgumentError", "SMTPS and STARTTLS is exclusive")
		}
		o.starttls = smtpStartTLSAuto
		return object.NilV
	})
	d("disable_starttls", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		obj(self).starttls = smtpStartTLSOff
		return object.NilV
	})

	// Timeouts (seconds); the reader returns the last set value.
	d("open_timeout=", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		obj(self).openTO = nethttpDuration(args[0])
		return args[0]
	})
	d("open_timeout", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return smtpDurationValue(obj(self).openTO)
	})
	d("read_timeout=", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		obj(self).readTO = nethttpDuration(args[0])
		return args[0]
	})
	d("read_timeout", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return smtpDurationValue(obj(self).readTO)
	})

	// Capability predicates (valid after a started session's EHLO).
	d("capable_starttls?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(obj(self).capableStartTLS())
	})
	d("capable_plain_auth?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(obj(self).capableAuth("PLAIN"))
	})
	d("capable_login_auth?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(obj(self).capableAuth("LOGIN"))
	})
	d("capable_cram_md5_auth?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(obj(self).capableAuth("CRAM-MD5"))
	})
	d("capable_auth_types", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		o := obj(self)
		var elems []object.Value
		if o.sess != nil {
			for _, a := range o.sess.Capabilities()["AUTH"] {
				elems = append(elems, object.NewString(a))
			}
		}
		return object.NewArrayFromSlice(elems)
	})
}

// smtpDurationValue renders a stored timeout back as a Ruby value: nil when unset,
// else the whole/fractional seconds.
func smtpDurationValue(d time.Duration) object.Value {
	if d <= 0 {
		return object.NilV
	}
	secs := d.Seconds()
	if secs == float64(int64(secs)) {
		return object.IntValue(int64(secs))
	}
	return object.Float(secs)
}

// registerNetSMTPProtocol installs the session lifecycle (start / finish) and the
// command surface (send_message / sendmail / open_message_stream / data / the
// low-level ehlo/helo/mailfrom/rcptto/rset and authenticate).
func (vm *VM) registerNetSMTPProtocol(smtp *RClass) {
	obj := func(v object.Value) *SMTPObj { return v.(*SMTPObj) }
	d := func(name string, fn NativeFn) { smtp.define(name, fn) }

	// #start(helo='localhost', user=nil, secret=nil, authtype=nil) [{ |smtp| }].
	d("start", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		o := obj(self)
		kw, rest := smtpSplitKw(args)
		helo, user, secret, authtype := vm.smtpStartCreds(rest, kw, o)
		vm.smtpDoStart(o, helo, user, secret, authtype)
		return vm.smtpYield(o, blk)
	})
	d("finish", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		o := obj(self)
		if !o.started {
			raise("IOError", "not yet started")
		}
		vm.smtpFinishQuiet(o)
		return object.NilV
	})

	// #send_message(msgstr, from_addr, *to_addrs) / #sendmail alias.
	send := func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.smtpSendMessage(obj(self), args)
	}
	d("send_message", send)
	d("sendmail", send)

	// #open_message_stream(from_addr, to_addrs) { |stream| } / #ready alias.
	stream := func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		return vm.smtpOpenMessageStream(obj(self), args, blk)
	}
	d("open_message_stream", stream)
	d("ready", stream)

	// #data(msgstr=nil) [{ |stream| }].
	d("data", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		o := obj(self)
		o.requireStarted()
		o.deadline()
		var msg string
		switch {
		case len(args) >= 1 && !object.IsNil(args[0]):
			msg = smtpMessageString(args[0])
		case blk != nil:
			st := vm.newSMTPStream()
			vm.callBlock(blk, []object.Value{st})
			msg = st.buf.String()
		default:
			raise("ArgumentError", "message or block required")
		}
		res, err := o.sess.Data(msg)
		if err != nil {
			smtpRaiseErr(err)
		}
		return vm.smtpResponse(res)
	})

	// Low-level commands, each returning a Net::SMTP::Response.
	d("ehlo", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.smtpCmd(obj(self), func(s *netsmtp.Session) (*netsmtp.Response, error) {
			return s.Ehlo(smtpArgStr(args, 0))
		})
	})
	d("helo", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.smtpCmd(obj(self), func(s *netsmtp.Session) (*netsmtp.Response, error) {
			return s.Helo(smtpArgStr(args, 0))
		})
	})
	d("mailfrom", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.smtpCmd(obj(self), func(s *netsmtp.Session) (*netsmtp.Response, error) {
			return s.MailFrom(smtpArgStr(args, 0))
		})
	})
	d("rcptto", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.smtpCmd(obj(self), func(s *netsmtp.Session) (*netsmtp.Response, error) {
			return s.RcptTo(smtpArgStr(args, 0))
		})
	})
	d("rset", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.smtpCmd(obj(self), func(s *netsmtp.Session) (*netsmtp.Response, error) {
			return s.Rset()
		})
	})

	// #rcptto_list(to_addrs) sends RCPT for each recipient and returns them.
	d("rcptto_list", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		o := obj(self)
		o.requireStarted()
		to := smtpRecipients(args)
		if len(to) == 0 {
			raise("ArgumentError", "mail destination not given")
		}
		for _, t := range to {
			o.deadline()
			if _, err := o.sess.RcptTo(strArg(t)); err != nil {
				smtpRaiseErr(err)
			}
		}
		if blk != nil {
			return vm.callBlock(blk, nil)
		}
		return object.NewArrayFromSlice(to)
	})

	// #authenticate(user, secret, authtype=nil).
	d("authenticate", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o := obj(self)
		o.requireStarted()
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2..3)", len(args))
		}
		authtype := object.Value(object.NilV)
		if len(args) >= 3 {
			authtype = args[2]
		}
		vm.smtpAuthenticate(o, strArg(args[0]), strArg(args[1]), authtype)
		return object.NilV
	})
}

// smtpCmd runs a single low-level command on a started session and wraps its
// reply, raising the mapped error on failure.
func (vm *VM) smtpCmd(o *SMTPObj, fn func(*netsmtp.Session) (*netsmtp.Response, error)) object.Value {
	o.requireStarted()
	o.deadline()
	res, err := fn(o.sess)
	if err != nil {
		smtpRaiseErr(err)
	}
	return vm.smtpResponse(res)
}

// smtpArgStr returns the i-th argument as a String, or "" when absent.
func smtpArgStr(args []object.Value, i int) string {
	if i >= len(args) || object.IsNil(args[i]) {
		return ""
	}
	return strArg(args[i])
}

// smtpRecipients flattens a recipient argument list: a single Array argument is
// its elements, otherwise the positional arguments themselves.
func smtpRecipients(args []object.Value) []object.Value {
	if len(args) == 1 {
		if arr, ok := args[0].(*object.Array); ok {
			return arr.Elems
		}
	}
	return args
}

// smtpSendMessage implements Net::SMTP#send_message: MAIL FROM, a RCPT TO per
// recipient, then the DATA payload; it returns the final Net::SMTP::Response.
func (vm *VM) smtpSendMessage(o *SMTPObj, args []object.Value) object.Value {
	o.requireStarted()
	if len(args) < 2 {
		raise("ArgumentError", "wrong number of arguments (given %d, expected 2+)", len(args))
	}
	msg := smtpMessageString(args[0])
	from := strArg(args[1])
	to := smtpRecipients(args[2:])
	if len(to) == 0 {
		raise("ArgumentError", "mail destination not given")
	}
	o.deadline()
	if _, err := o.sess.MailFrom(from); err != nil {
		smtpRaiseErr(err)
	}
	for _, t := range to {
		if _, err := o.sess.RcptTo(strArg(t)); err != nil {
			smtpRaiseErr(err)
		}
	}
	res, err := o.sess.Data(msg)
	if err != nil {
		smtpRaiseErr(err)
	}
	return vm.smtpResponse(res)
}

// smtpOpenMessageStream implements Net::SMTP#open_message_stream: MAIL FROM, a
// RCPT TO per recipient, then a block that appends the message body to a stream
// adapter whose accumulated text is sent as the DATA payload.
func (vm *VM) smtpOpenMessageStream(o *SMTPObj, args []object.Value, blk *Proc) object.Value {
	o.requireStarted()
	if blk == nil {
		raise("LocalJumpError", "no block given (yield)")
	}
	if len(args) < 1 {
		raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
	}
	from := strArg(args[0])
	to := smtpRecipients(args[1:])
	if len(to) == 0 {
		raise("ArgumentError", "mail destination not given")
	}
	o.deadline()
	if _, err := o.sess.MailFrom(from); err != nil {
		smtpRaiseErr(err)
	}
	for _, t := range to {
		if _, err := o.sess.RcptTo(strArg(t)); err != nil {
			smtpRaiseErr(err)
		}
	}
	st := vm.newSMTPStream()
	vm.callBlock(blk, []object.Value{st})
	res, err := o.sess.Data(st.buf.String())
	if err != nil {
		smtpRaiseErr(err)
	}
	return vm.smtpResponse(res)
}
