// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"bufio"
	"errors"
	"net"
	"strconv"
	"strings"

	netftp "github.com/go-ruby-net-ftp/net-ftp"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the transport half of the Net::FTP binding: it drives real
// control and data sockets (Go net, exactly as socket.go / nethttp_bind.go do)
// on top of the interpreter-independent FTP codec
// github.com/go-ruby-net-ftp/net-ftp. The codec owns every observable byte —
// command lines, reply classification, PASV/EPSV host:port extraction, MLSx
// parsing; rbgo owns only the sockets. net_ftp.go builds the Ruby class surface
// and calls into the helpers here.
//
// The control connection is a single net.Conn wrapped in a bufio.Reader (so the
// codec's LineReader can pull whole reply lines). Each data transfer opens a
// second connection: in passive mode rbgo dials the host:port the codec
// extracts from the PASV/EPSV reply; in active mode rbgo listens on the control
// socket's local interface, sends PORT/EPRT, and accepts the server's callback.
// A transport read/write failure surfaces as Net::FTPConnectionError; a codec
// FTPError (reply/temp/perm/proto) re-raises as its matching Ruby class.

// ftpDefaultBlocksize is Net::FTP::DEFAULT_BLOCKSIZE — the chunk size the binary
// transfer helpers read and write in.
const ftpDefaultBlocksize = 4096

// ftpObj is the Ruby wrapper around a live FTP control connection (Net::FTP). It
// owns the control socket and the transfer state (binary/passive/EPSV mode); the
// codec supplies the protocol, rbgo supplies the sockets.
type ftpObj struct {
	cls  *RClass
	conn net.Conn
	r    *bufio.Reader
	// binary is the current transfer type (true = TYPE I, false = TYPE A). It
	// tracks the last TYPE the host actually sent so a transfer only re-sends TYPE
	// when the mode changes, mirroring Net::FTP.
	binary bool
	// curType is the TYPE last negotiated with the server ("" before any transfer,
	// "I" or "A" after), distinct from binary so the first transfer always sends
	// TYPE.
	curType string
	// passive selects passive (PASV/EPSV) vs active (PORT/EPRT) data connections;
	// Net::FTP defaults to passive.
	passive bool
	// epsv selects EPSV/EPRT over PASV/PORT. Net::FTP uses the extended commands
	// when the control connection is IPv6; ftpIsIPv6 sets it at connect time.
	epsv bool
	// usePasvIP mirrors Net::FTP#use_pasv_ip: when true a PASV reply's encoded host
	// is dialed, otherwise the control socket's address is used.
	usePasvIP bool
	// lastResponse is the most recent assembled reply (Net::FTP#last_response).
	lastResponse string
	// welcome is the server's connect greeting (Net::FTP#welcome).
	welcome string
	// closed reports whether the control connection has been closed.
	closed bool
}

func (f *ftpObj) ToS() string     { return "#<Net::FTP>" }
func (f *ftpObj) Inspect() string { return "#<Net::FTP>" }
func (f *ftpObj) Truthy() bool    { return true }

// doClose closes the control connection once (Net::FTP#close); a second call is
// a no-op.
func (f *ftpObj) doClose() {
	if f.closed || f.conn == nil {
		return
	}
	f.conn.Close()
	f.closed = true
}

// ftpMLSxEntry is the Ruby wrapper around a parsed MLSD/MLST entry
// (Net::FTP::MLSxEntry): the codec's MLSxEntry plus the class it reports.
type ftpMLSxEntry struct {
	cls *RClass
	e   netftp.MLSxEntry
}

func (m *ftpMLSxEntry) ToS() string     { return "#<Net::FTP::MLSxEntry " + m.e.Pathname + ">" }
func (m *ftpMLSxEntry) Inspect() string { return m.ToS() }
func (m *ftpMLSxEntry) Truthy() bool    { return true }

// --- codec error mapping ----------------------------------------------------

// ftpRaise re-raises a codec error as its Ruby class: an *netftp.FTPError uses
// the reply/temp/perm/proto/connection class name the codec assigns, and any
// other (transport) error becomes Net::FTPConnectionError.
func ftpRaise(err error) {
	var fe *netftp.FTPError
	if errors.As(err, &fe) {
		raise(fe.ClassName(), "%s", fe.Message)
	}
	raise("Net::FTPConnectionError", "%s", err.Error())
}

// ftpConnErr raises Net::FTPConnectionError for a transport (dial/read/write)
// failure.
func ftpConnErr(err error) {
	raise("Net::FTPConnectionError", "%s", err.Error())
}

// --- control-connection primitives ------------------------------------------

// reader returns the codec LineReader over the buffered control connection; it
// yields whole reply lines (terminator included — the codec strips it).
func (f *ftpObj) reader() netftp.LineReader {
	return func() (string, error) {
		line, err := f.r.ReadString('\n')
		if err != nil && line == "" {
			return "", err
		}
		return line, nil
	}
}

// writeLine builds the command bytes (PutLine appends CRLF and rejects an
// embedded CR/LF) and writes them to the control socket.
func (f *ftpObj) writeLine(line string) {
	bytes, err := netftp.PutLine(line)
	if err != nil {
		raise("ArgumentError", "%s", err.Error())
	}
	if _, werr := f.conn.Write([]byte(bytes)); werr != nil {
		ftpConnErr(werr)
	}
}

// getResp reads one assembled reply, records it as last_response, and classifies
// it: a 4yz/5yz/malformed reply re-raises as the matching Net::FTP error, a
// transport read failure as Net::FTPConnectionError.
func (f *ftpObj) getResp() string {
	resp, err := netftp.GetMultiline(f.reader())
	if err != nil {
		ftpConnErr(err)
	}
	f.lastResponse = resp
	got, cerr := netftp.ClassifyReply(resp)
	if cerr != nil {
		ftpRaise(cerr)
	}
	return got
}

// sendCmd writes a command and returns its classified reply (Net::FTP#sendcmd).
func (f *ftpObj) sendCmd(line string) string {
	f.writeLine(line)
	return f.getResp()
}

// voidResp reads a reply and requires it to begin with '2' (Net::FTP#voidresp).
func (f *ftpObj) voidResp() {
	resp := f.getResp()
	if !strings.HasPrefix(resp, "2") {
		raise("Net::FTPReplyError", "%s", resp)
	}
}

// voidCmd writes a command and requires a 2yz reply (Net::FTP#voidcmd).
func (f *ftpObj) voidCmd(line string) {
	f.writeLine(line)
	f.voidResp()
}

// --- addressing helpers -----------------------------------------------------

// remoteHost is the control socket's remote address host — the host EPSV (which
// encodes none) and a use_pasv_ip=false PASV dial through.
func (f *ftpObj) remoteHost() string {
	host, _, err := net.SplitHostPort(f.conn.RemoteAddr().String())
	if err != nil {
		return f.conn.RemoteAddr().String()
	}
	return host
}

// localHost is the control socket's local address host, the interface an
// active-mode data listener binds.
func (f *ftpObj) localHost() string {
	host, _, err := net.SplitHostPort(f.conn.LocalAddr().String())
	if err != nil {
		return f.conn.LocalAddr().String()
	}
	return host
}

// ftpIsIPv6 reports whether a connection's remote address is an IPv6 TCP
// address, the condition under which Net::FTP uses EPSV/EPRT.
func ftpIsIPv6(c net.Conn) bool {
	if a, ok := c.RemoteAddr().(*net.TCPAddr); ok {
		return a.IP.To4() == nil
	}
	return false
}

// --- data connections -------------------------------------------------------

// makepasv negotiates a passive data address, mirroring Net::FTP#makepasv: EPSV
// (parse229) for an IPv6 control connection, PASV (parse227) otherwise.
func (f *ftpObj) makepasv() (string, int) {
	if f.epsv {
		host, port, err := netftp.Parse229(f.sendCmd(netftp.EpsvCommand), f.remoteHost())
		if err != nil {
			ftpRaise(err)
		}
		return host, port
	}
	host, port, err := netftp.Parse227(f.sendCmd(netftp.PasvCommand), f.usePasvIP, f.remoteHost())
	if err != nil {
		ftpRaise(err)
	}
	return host, port
}

// makeport opens an active-mode data listener on the control socket's local
// interface and announces it with PORT (SendPort) or EPRT (SendEPort).
func (f *ftpObj) makeport() net.Listener {
	host := f.localHost()
	ln, err := net.Listen("tcp", net.JoinHostPort(host, "0"))
	if err != nil {
		ftpConnErr(err)
	}
	_, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port, _ := strconv.Atoi(portStr)
	if f.epsv {
		f.voidCmd(netftp.SendEPort(host, port))
	} else {
		f.voidCmd(netftp.SendPort(host, port))
	}
	return ln
}

// ftpMarkOK reports whether a transfer command's reply is a preliminary mark
// (1yz) or an immediate completion (2yz) — the replies Net::FTP#transfercmd
// accepts before reading the data connection.
func ftpMarkOK(resp string) bool {
	return strings.HasPrefix(resp, "1") || strings.HasPrefix(resp, "2")
}

// transfercmd opens a data connection and issues the transfer command over the
// control connection, returning the ready data socket (Net::FTP#transfercmd).
// The caller reads or writes the data, closes it, and then calls voidResp for
// the completion reply. A command reply that is an error (or not a mark) closes
// the just-opened data socket/listener before raising, so no fd or goroutine
// leaks on the failure path.
func (f *ftpObj) transfercmd(cmd string) net.Conn {
	if f.passive {
		host, port := f.makepasv()
		conn := f.dialData(host, port)
		f.sendTransferCmd(cmd, func() { conn.Close() })
		return conn
	}
	ln := f.makeport()
	f.sendTransferCmd(cmd, func() { ln.Close() })
	conn := f.acceptData(ln)
	ln.Close()
	return conn
}

// dialData dials a passive data address, raising Net::FTPConnectionError when the
// connection cannot be made.
func (f *ftpObj) dialData(host string, port int) net.Conn {
	conn, err := net.Dial("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		ftpConnErr(err)
	}
	return conn
}

// acceptData accepts the server's active-mode data callback, raising
// Net::FTPConnectionError when the accept fails.
func (f *ftpObj) acceptData(ln net.Listener) net.Conn {
	conn, err := ln.Accept()
	if err != nil {
		ftpConnErr(err)
	}
	return conn
}

// sendData writes data to the data connection in blocksize chunks, closing it
// and raising Net::FTPConnectionError on a write failure.
func (f *ftpObj) sendData(conn net.Conn, data []byte, blocksize int) {
	for off := 0; off < len(data); off += blocksize {
		end := min(off+blocksize, len(data))
		if _, err := conn.Write(data[off:end]); err != nil {
			conn.Close()
			ftpConnErr(err)
		}
	}
}

// sendTransferCmd writes the transfer command and reads its reply raw (bypassing
// getResp's raise) so cleanup — closing the already-opened data socket or
// listener — runs before any error is raised: a read failure or 4yz/5yz reply
// re-raises the codec error, and a non-mark reply (a stray 3yz) raises
// FTPReplyError.
func (f *ftpObj) sendTransferCmd(cmd string, cleanup func()) {
	f.writeLine(cmd)
	resp, err := netftp.GetMultiline(f.reader())
	if err != nil {
		cleanup()
		ftpConnErr(err)
	}
	f.lastResponse = resp
	if _, cerr := netftp.ClassifyReply(resp); cerr != nil {
		cleanup()
		ftpRaise(cerr)
	}
	if !ftpMarkOK(resp) {
		cleanup()
		raise("Net::FTPReplyError", "%s", resp)
	}
}

// setType negotiates the transfer TYPE (I/A) when it differs from the last one
// sent, mirroring Net::FTP's lazy send_type_command.
func (f *ftpObj) setType(binary bool) {
	want := "A"
	if binary {
		want = "I"
	}
	if f.curType == want {
		return
	}
	f.voidCmd(netftp.TypeCommand(binary))
	f.curType = want
}

// --- transfer engines -------------------------------------------------------

// retrlines runs an ASCII retrieval, yielding each received line (its trailing
// CRLF/LF removed) to fn, then reads the completion reply
// (Net::FTP#retrlines).
func (f *ftpObj) retrlines(cmd string, fn func(string)) {
	f.setType(false)
	conn := f.transfercmd(cmd)
	br := bufio.NewReader(conn)
	for {
		line, err := br.ReadString('\n')
		if line != "" {
			fn(ftpChomp(line))
		}
		if err != nil {
			break
		}
	}
	conn.Close()
	f.voidResp()
}

// retrbinary runs a binary retrieval, yielding each block of at most blocksize
// bytes to fn, then reads the completion reply (Net::FTP#retrbinary).
func (f *ftpObj) retrbinary(cmd string, blocksize int, fn func([]byte)) {
	f.setType(true)
	conn := f.transfercmd(cmd)
	buf := make([]byte, blocksize)
	for {
		n, err := conn.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			fn(chunk)
		}
		if err != nil {
			break
		}
	}
	conn.Close()
	f.voidResp()
}

// storbinary runs a binary store, writing data in blocksize chunks over the data
// connection, then reads the completion reply (Net::FTP#storbinary).
func (f *ftpObj) storbinary(cmd string, data []byte, blocksize int) {
	f.setType(true)
	conn := f.transfercmd(cmd)
	f.sendData(conn, data, blocksize)
	conn.Close()
	f.voidResp()
}

// storlines runs an ASCII store, writing data (each bare LF promoted to CRLF)
// over the data connection, then reads the completion reply
// (Net::FTP#storlines).
func (f *ftpObj) storlines(cmd string, data string) {
	f.setType(false)
	conn := f.transfercmd(cmd)
	f.sendData(conn, []byte(ftpToCRLF(data)), ftpDefaultBlocksize)
	conn.Close()
	f.voidResp()
}

// --- small text helpers -----------------------------------------------------

// ftpChomp removes a single trailing CRLF / LF / CR record separator, matching
// the line boundaries Net::FTP#retrlines yields.
func ftpChomp(s string) string {
	if strings.HasSuffix(s, "\r\n") {
		return s[:len(s)-2]
	}
	if n := len(s); n > 0 {
		if c := s[n-1]; c == '\n' || c == '\r' {
			return s[:n-1]
		}
	}
	return s
}

// ftpToCRLF promotes each bare LF (one not already preceded by CR) to CRLF, the
// line-ending normalisation Net::FTP#storlines applies to ASCII uploads.
func ftpToCRLF(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\n' && (i == 0 || s[i-1] != '\r') {
			b.WriteByte('\r')
		}
		b.WriteByte(c)
	}
	return b.String()
}

// ftpBasename returns the final path element of p (File.basename), the default
// remote/local name the get/put helpers derive from their counterpart. It splits
// on either separator so a host filesystem path survives on Windows too (where a
// local path from filepath.Join uses '\\'), matching Ruby's File.basename, which
// treats both '/' and '\\' as separators on Windows.
func ftpBasename(p string) string {
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		return p[i+1:]
	}
	return p
}

// --- argument helpers -------------------------------------------------------

// ftpArity raises an ArgumentError when args has fewer than min or more than max
// elements.
func ftpArity(args []object.Value, min, max int, name string) {
	if len(args) < min || len(args) > max {
		raise("ArgumentError", "wrong number of arguments for '%s' (given %d)", name, len(args))
	}
}

// ftpOptString returns the i-th argument as a String, or def when it is absent
// or nil (the optional pathname arguments Net::FTP commands accept).
func ftpOptString(args []object.Value, i int, def string) string {
	if i >= len(args) {
		return def
	}
	if _, ok := args[i].(object.Nil); ok {
		return def
	}
	return strArg(args[i])
}

// ftpBytes returns the raw bytes of a String argument (for a binary upload
// body).
func ftpBytes(v object.Value) []byte {
	if s, ok := v.(*object.String); ok {
		return s.Bytes()
	}
	raise("TypeError", "no implicit conversion of %s into String", v.Inspect())
	return nil
}

// ftpBlocksizeArg reads an optional trailing blocksize argument, defaulting to
// ftpDefaultBlocksize.
func ftpBlocksizeArg(args []object.Value, i int) int {
	if i >= len(args) {
		return ftpDefaultBlocksize
	}
	if n, ok := args[i].(object.Integer); ok && int(n) > 0 {
		return int(n)
	}
	return ftpDefaultBlocksize
}
