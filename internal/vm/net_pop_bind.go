// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"io"
	"strings"

	netpop "github.com/go-ruby-net-pop/net-pop"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin binding between rbgo's Ruby object graph (object.Value)
// and the interpreter-independent POP3 protocol codec of
// github.com/go-ruby-net-pop/net-pop. The command bytes, the APOP digest, the
// "+OK"/"-ERR" status checks, the STAT/LIST/UIDL field parses and the multiline
// dot-unstuffing all live in that library; rbgo only supplies the host seam — the
// socket — and maps the parsed values back into the object graph.
//
// rbgo drives the library through its Transport interface (Writeline / Readline /
// ReadRawLine). Because rbgo opens no socket of its own here, the transport is an
// injected Ruby IO-like object: any object responding to #write and #gets (a
// StringIO, or a duck-typed socket a host provides). rubyPOPTransport bridges that
// object to the library's Transport, so the whole suite stays deterministic and
// Ruby-free.

// POP3Obj is the Ruby wrapper around a *netpop.Conn (Net::POP3). It owns no
// socket: the Conn drives commands over the injected rubyPOPTransport seam.
type POP3Obj struct {
	cls  *RClass
	conn *netpop.Conn
	// seam is the Ruby IO-like object backing the transport, kept so #_connection
	// can return it and so the object stays reachable.
	seam    object.Value
	address string
	port    int
	apop    bool
	started bool
	// mails memoises the POPMail list built by #mails / #each_mail; #reset clears
	// it, mirroring how MRI caches @mails and drops it on RSET.
	mails []*POPMailObj
}

func (o *POP3Obj) ToS() string     { return "#<Net::POP3 " + o.address + ">" }
func (o *POP3Obj) Inspect() string { return o.ToS() }
func (o *POP3Obj) Truthy() bool    { return true }

// POPMailObj is the Ruby wrapper around a *netpop.POPMail (Net::POPMail). It holds
// a back-reference to its POP3Obj so #pop / #top / #delete drive the session's
// Conn; the parsed number / length / uid live in the library's POPMail model.
type POPMailObj struct {
	cls  *RClass
	pop  *POP3Obj
	mail *netpop.POPMail
}

func (m *POPMailObj) ToS() string     { return "#<Net::POPMail " + numToS(m.mail.Number) + ">" }
func (m *POPMailObj) Inspect() string { return m.ToS() }
func (m *POPMailObj) Truthy() bool    { return true }

// numToS renders an int as its base-10 string (used by the wrapper #to_s).
func numToS(n int) string { return object.IntValue(int64(n)).ToS() }

// rubyPOPTransport bridges a Ruby IO-like object (responding to #write and #gets)
// to the netpop.Transport the library drives the POP3 session over. Writeline
// appends the CRLF and forwards to #write; Readline reads one line via #gets and
// strips its terminator; ReadRawLine returns the line WITH its terminator so the
// ".\r\n" multiline sentinel and the leading-dot un-stuffing stay intact. This is
// the host seam: the library owns the protocol, rbgo owns the transport.
type rubyPOPTransport struct {
	vm  *VM
	obj object.Value
}

// Writeline sends line + CRLF to the Ruby object's #write, mirroring
// Net::InternetMessageIO#writeline. A Ruby exception from #write unwinds through
// the panic-based raise, so the returned error is never a Ruby-level failure.
func (t *rubyPOPTransport) Writeline(line string) error {
	t.vm.send(t.obj, "write", []object.Value{object.NewString(line + "\r\n")}, nil)
	return nil
}

// Readline reads one response line via the Ruby object's #gets and returns it
// without the trailing CRLF, mirroring Net::BufferedIO#readline. A nil #gets reply
// (a closed socket) is EOF.
func (t *rubyPOPTransport) Readline() (string, error) {
	raw, err := t.gets()
	if err != nil {
		return "", err
	}
	return strings.TrimRight(raw, "\r\n"), nil
}

// ReadRawLine reads one line of a multiline body via #gets and returns it WITH its
// trailing CRLF intact, mirroring Net::BufferedIO#readuntil("\r\n"); the
// terminator detection and the dot-unstuffing depend on the CRLF being present.
func (t *rubyPOPTransport) ReadRawLine() (string, error) {
	return t.gets()
}

// gets pulls one line from the Ruby object's #gets: a String yields its contents,
// a nil (or anything else) yields io.EOF.
func (t *rubyPOPTransport) gets() (string, error) {
	rv := t.vm.send(t.obj, "gets", nil, nil)
	if s, ok := rv.(*object.String); ok {
		return s.Str(), nil
	}
	return "", io.EOF
}

// popRaise turns a library error into the gem-faithful Ruby exception: a
// *netpop.POPAuthenticationError rescues as Net::POPAuthenticationError, a
// *netpop.POPBadResponse as Net::POPBadResponse, a *netpop.POPError as
// Net::POPError, and any transport/EOF fault as Net::POPError carrying its message.
func popRaise(err error) {
	switch e := err.(type) {
	case *netpop.POPAuthenticationError:
		raise("Net::POPAuthenticationError", "%s", e.Response)
	case *netpop.POPBadResponse:
		raise("Net::POPBadResponse", "%s", e.Response)
	case *netpop.POPError:
		raise("Net::POPError", "%s", e.Response)
	default:
		raise("Net::POPError", "%s", err.Error())
	}
}

// --- construction argument parsing -----------------------------------------

// popConnFromHash reads the connection seam a keyword Hash names — connection: /
// socket: / conn: (Symbol or String key) — returning nil when none is present.
func popConnFromHash(h *object.Hash) object.Value {
	get := func(name string) (object.Value, bool) {
		if v, ok := h.Get(object.Symbol(name)); ok {
			return v, true
		}
		return h.Get(object.NewString(name))
	}
	for _, key := range []string{"connection", "socket", "conn"} {
		if v, ok := get(key); ok {
			return v
		}
	}
	return nil
}

// popSeamArg pops a trailing keyword Hash off args, returning the connection seam
// it names and the remaining positional arguments.
func popSeamArg(args []object.Value) (object.Value, []object.Value) {
	if len(args) > 0 {
		if h, ok := args[len(args)-1].(*object.Hash); ok {
			return popConnFromHash(h), args[:len(args)-1]
		}
	}
	return nil, args
}

// popStrAt returns args[i].to_s, or "" when the index is absent or nil.
func popStrAt(args []object.Value, i int) string {
	if i < len(args) && !object.IsNil(args[i]) {
		return strArg(args[i])
	}
	return ""
}

// popIntAt returns args[i] as an int, or def when the index is absent or nil.
func popIntAt(args []object.Value, i, def int) int {
	if i < len(args) && !object.IsNil(args[i]) {
		if n, ok := args[i].(object.Integer); ok {
			return int(n)
		}
	}
	return def
}

// popBoolAt returns args[i].truthy?, or false when the index is absent.
func popBoolAt(args []object.Value, i int) bool {
	return i < len(args) && args[i].Truthy()
}
