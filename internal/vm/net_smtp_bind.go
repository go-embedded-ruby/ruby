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

// This file is the transport bridge between rbgo's Ruby object graph and the
// interpreter-independent SMTP codec of github.com/go-ruby-net-smtp/net-smtp. The
// command grammar, the DATA dot-stuffing, the reply parser, the EHLO capability
// parse and the SASL auth encodings all live in that library; rbgo supplies the
// one thing it deliberately leaves out — the socket. Net::SMTP dials its own
// TCP/TLS connection, so (unlike the redis/pg bindings' injected-IO seam) the
// transport here is rbgo's real socket layer (socket.go): net_smtp.go's
// Net::SMTP#start opens a TCPSocket to the address/port, wraps it in a smtpConn,
// and hands that to netsmtp.NewSession, which sequences the codec against it
// exactly the way MRI's Net::SMTP drives its @socket. STARTTLS upgrades the same
// socket in place through the crypto/tls handshake the net/http transport already
// uses. Tests drive an in-process SMTP server (net_smtp_internal_test.go) so no
// real network is touched.

// smtpStartTLS selects the STARTTLS policy for a session, mirroring Net::SMTP's
// @starttls (false / :auto / :always).
const (
	// smtpStartTLSOff never issues STARTTLS.
	smtpStartTLSOff = iota
	// smtpStartTLSAuto issues STARTTLS only when the EHLO reply advertises it.
	smtpStartTLSAuto
	// smtpStartTLSAlways always issues STARTTLS (and fails if the server rejects it).
	smtpStartTLSAlways
)

// SMTPObj is the Ruby wrapper around a Net::SMTP session. It owns the socket it
// dials (conn) and the codec driver (sess); both are nil until #start opens them
// and are cleared again by #finish. The configuration fields mirror the Net::SMTP
// accessors (esmtp, tls, starttls, timeouts) and are read at #start time.
type SMTPObj struct {
	cls     *RClass
	address string
	port    string

	sess    *netsmtp.Session
	conn    *smtpConn
	started bool

	esmtp      bool // EHLO (true, the default) vs plain HELO
	tls        bool // implicit TLS on connect (SMTPS)
	starttls   int  // smtpStartTLSOff / Auto / Always
	verifyMode int64
	openTO     time.Duration
	readTO     time.Duration
}

func (o *SMTPObj) ToS() string     { return "#<Net::SMTP " + o.address + ":" + o.port + ">" }
func (o *SMTPObj) Inspect() string { return o.ToS() }
func (o *SMTPObj) Truthy() bool    { return true }

// requireStarted raises IOError unless the session is open, matching MRI's guard
// on every command that needs a live connection.
func (o *SMTPObj) requireStarted() {
	if !o.started || o.sess == nil {
		raise("IOError", "not yet started")
	}
}

// deadline refreshes the socket read deadline before a protocol step when a
// read_timeout is set (0 clears/leaves it unbounded).
func (o *SMTPObj) deadline() {
	if o.readTO > 0 && o.conn != nil {
		nethttpSetDeadline(o.conn.stream, o.readTO)
	}
}

// closeConn closes the underlying socket, ignoring a close error on an already
// broken connection.
func (o *SMTPObj) closeConn() {
	if o.conn != nil {
		o.conn.close()
	}
}

// capableStartTLS reports whether the last EHLO advertised STARTTLS
// (Net::SMTP#capable_starttls?).
func (o *SMTPObj) capableStartTLS() bool {
	if o.sess == nil {
		return false
	}
	_, ok := o.sess.Capabilities()["STARTTLS"]
	return ok
}

// capableAuth reports whether the last EHLO advertised the given SASL mechanism
// (backs Net::SMTP#capable_plain_auth? / #capable_login_auth? / #capable_cram_md5_auth?).
func (o *SMTPObj) capableAuth(mech string) bool {
	if o.sess == nil {
		return false
	}
	auths, ok := o.sess.Capabilities()["AUTH"]
	if !ok {
		return false
	}
	for _, a := range auths {
		if strings.EqualFold(a, mech) {
			return true
		}
	}
	return false
}

// smtpConn adapts rbgo's socket transport (a streamIO — a raw tcpSocket or a TLS
// sslSocket) to the netsmtp.Conn seam the codec drives: WriteLine appends CRLF to
// a request line, ReadLine reads one reply line and chops its CRLF, WriteRaw
// writes an already dot-stuffed DATA payload verbatim, and StartTLS upgrades the
// socket in place through the TLS handshake.
type smtpConn struct {
	stream     streamIO
	host       string
	verifyMode int64
}

// WriteLine sends a request line followed by CRLF (Net::BufferedIO#writeline).
func (c *smtpConn) WriteLine(line string) error {
	_, err := c.stream.writer().Write([]byte(line + netsmtp.CRLF))
	return err
}

// ReadLine reads one reply line and returns it with the trailing CRLF removed
// (Net::BufferedIO#readline).
func (c *smtpConn) ReadLine() (string, error) {
	line, err := c.stream.reader().ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// WriteRaw writes the given bytes to the socket unchanged (the dot-stuffed,
// terminated DATA payload).
func (c *smtpConn) WriteRaw(b string) error {
	_, err := c.stream.writer().Write([]byte(b))
	return err
}

// StartTLS upgrades the connection to TLS in place, replacing the raw stream with
// a TLS one over the same net.Conn (Net::SMTP#starttls). verifyMode 0
// (VERIFY_NONE) skips certificate verification, matching the transport default.
func (c *smtpConn) StartTLS() error {
	conn := nethttpNetConn(c.stream)
	if conn == nil {
		return &netsmtp.SMTPError{Kind: netsmtp.KindUnknown, Msg: "connection cannot be upgraded to TLS"}
	}
	s, err := nethttpTLSWrap(conn, c.host, c.verifyMode)
	if err != nil {
		return err
	}
	c.stream = s
	return nil
}

// close closes the underlying socket.
func (c *smtpConn) close() error { return c.stream.closeConn() }

// smtpDial opens the socket for a session and installs the codec driver: a raw
// TCP dial (with the open_timeout, if any), optionally wrapped in TLS straight
// away for an implicit-TLS (SMTPS) session.
func (vm *VM) smtpDial(o *SMTPObj) error {
	conn, err := nethttpRawDial(o.address, o.port, o.openTO)
	if err != nil {
		return err
	}
	var stream streamIO
	if o.tls {
		s, terr := nethttpTLSWrap(conn, o.address, o.verifyMode)
		if terr != nil {
			return terr
		}
		stream = s
	} else {
		stream = newTCPSocket(nil, conn)
	}
	o.conn = &smtpConn{stream: stream, host: o.address, verifyMode: o.verifyMode}
	o.sess = netsmtp.NewSession(o.conn)
	return nil
}

// smtpRaiseErr raises the Ruby exception a codec/transport error maps to: a
// *netsmtp.SMTPError becomes its kind's Net::SMTP* class (the exact class MRI's
// check_response would raise), any other (transport) error becomes IOError.
func smtpRaiseErr(err error) {
	if se, ok := err.(*netsmtp.SMTPError); ok {
		raise(se.Kind.RubyClass(), "%s", se.Error())
	}
	raise("IOError", "%s", err.Error())
}

// smtpRespErr builds the SMTPError a non-success reply would raise, using the
// reply's own exception_class (the lib's errorFor is unexported, but Response and
// SMTPError expose everything needed to reproduce it).
func smtpRespErr(res *netsmtp.Response) *netsmtp.SMTPError {
	return &netsmtp.SMTPError{Kind: res.ExceptionClass(), Response: res}
}

// smtpStr renders a value the way an IO write coerces it: a String yields its
// bytes, anything else its to_s.
func smtpStr(v object.Value) string {
	if s, ok := v.(*object.String); ok {
		return s.Str()
	}
	return v.ToS()
}

// smtpMessageString coerces a send_message / data payload to its wire string: a
// String verbatim, an Array joined (each element to_s), anything else its to_s.
func smtpMessageString(v object.Value) string {
	switch n := v.(type) {
	case *object.String:
		return n.Str()
	case *object.Array:
		var b strings.Builder
		for _, e := range n.Elems {
			b.WriteString(smtpStr(e))
		}
		return b.String()
	}
	return v.ToS()
}

// smtpKw reads a keyword value from a trailing options Hash by Symbol or String
// key, mirroring the redis/pg keyword lookup.
func smtpKw(h *object.Hash, name string) (object.Value, bool) {
	if v, ok := h.Get(object.Symbol(name)); ok {
		return v, true
	}
	return h.Get(object.NewString(name))
}

// smtpStartTLSMode maps a starttls: keyword to a policy: :always / true → Always,
// :auto → Auto, false / nil → Off.
func smtpStartTLSMode(v object.Value) int {
	switch n := v.(type) {
	case object.Symbol:
		switch string(n) {
		case "always":
			return smtpStartTLSAlways
		case "auto":
			return smtpStartTLSAuto
		}
		return smtpStartTLSOff
	case object.Bool:
		if bool(n) {
			return smtpStartTLSAlways
		}
		return smtpStartTLSOff
	}
	if v.Truthy() {
		return smtpStartTLSAlways
	}
	return smtpStartTLSOff
}

// smtpAuthTypeName normalises an authtype (Symbol or String) to the codec's
// mechanism key: :plain / "PLAIN" → "plain", :login → "login", :cram_md5 /
// "CRAM-MD5" → "cram_md5".
func smtpAuthTypeName(v object.Value) string {
	s := smtpStr(v)
	s = strings.ToLower(strings.ReplaceAll(s, "-", "_"))
	return s
}
