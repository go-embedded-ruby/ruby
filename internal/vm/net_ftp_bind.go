// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !rbgo_core

package vm

import (
	"bufio"
	"errors"
	"net"
	"strconv"
	"strings"
	"time"

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
	// openTimeout and readTimeout are Net::FTP#open_timeout and #read_timeout in
	// SECONDS, held as Ruby values because nil is a meaningful setting ("no
	// bound") and distinct from 0.
	//
	// ⛔ THE DEFAULTS ARE NOT SYMMETRIC, AND THAT ASYMMETRY IS THE WHOLE POINT.
	// ruby/spec pins them (library/net-ftp/initialize_spec.rb): open_timeout
	// defaults to nil ("sets open_timeout to nil", :272) and read_timeout to 60
	// ("sets read_timeout to the default value", :291 — `should == 60`). So MRI
	// leaves the connect unbounded and bounds every read.
	//
	// Without them this binding dialled with net.Dial and set no read deadline
	// anywhere, so a peer that accepted the connection and then said nothing
	// held the interpreter for ever: 39 of 56 library/net-ftp spec files had to
	// be killed by the clock under rbgo and none under MRI (#916). That is a
	// denial of service needing no exploit, only silence — which is why the
	// default is a bound and not "whatever the caller remembers to set".
	openTimeout object.Value
	readTimeout object.Value
	// vm is held so every blocking socket call can release the GVL around
	// itself. See ftpIO below for why that is not optional.
	vm *VM
}

// ftpDefaultReadTimeout is Net::FTP's documented default, asserted by
// library/net-ftp/initialize_spec.rb.
const ftpDefaultReadTimeout = 60

// newFTPObj builds a Net::FTP with MRI's timeout defaults in place. Every
// construction site goes through it so a new one cannot be born unbounded.
func newFTPObj(vm *VM, cls *RClass) *ftpObj {
	return &ftpObj{
		cls:         cls,
		binary:      true,
		passive:     true,
		openTimeout: object.NilV,
		readTimeout: object.IntValue(ftpDefaultReadTimeout),
		vm:          vm,
	}
}

// ftpIO runs a blocking socket call with the GVL RELEASED, which is what every
// other binding here does through ioBlock and this one did not.
//
// ⛔ WITHOUT IT, A RUBY PROGRAM THAT SERVES ITSELF DEADLOCKS. rbgo emulates a
// GVL: one Ruby thread runs VM code at a time, and a thread that blocks in a
// native call is expected to drop it (vm.go:772, threadRelease). TCPServer#accept
// already does -- socket.go:174 wraps it in ioBlock -- but it must RE-ACQUIRE
// the GVL to return into Ruby, and these reads held it for the whole of a
// blocking read(2). So the server thread's Accept returned and then parked
// forever waiting for a lock the client was sitting on.
//
// Measured: every library/net-ftp spec stands up its server in the same
// process, so client and server are two threads of one VM. 39 of 56 files had
// to be killed by the clock, none under MRI (#916). With a read deadline in
// place the shape is visible in a timestamp: accept returned 1 ms after the
// client's read_timeout fired, and moving that timeout to 5 s moved the accept
// with it.
//
// Net::HTTP has used ioBlock all along (nethttp_bind.go:574). This binding
// simply never got it -- the same rule reached through one caller and not its
// neighbour.
func (f *ftpObj) ftpIO(fn func()) {
	if f.vm == nil {
		// A direct-constructed ftpObj in a unit test has no VM; running fn
		// inline is correct there and keeps those tests working.
		fn()
		return
	}
	ioBlock(f.vm, fn)
}

// timeoutSeconds reads a timeout value as a Duration; nil (or a non-number)
// means no bound, which is what MRI's nil open_timeout asks for.
func ftpTimeoutSeconds(v object.Value) time.Duration {
	switch n := v.(type) {
	case object.Integer:
		if n <= 0 {
			return 0
		}
		return time.Duration(float64(n) * float64(time.Second))
	case object.Float:
		if n <= 0 {
			return 0
		}
		return time.Duration(float64(n) * float64(time.Second))
	}
	return 0
}

// armRead puts the read_timeout deadline on the control socket before a read.
// It is called before EVERY read rather than once at connect, because a
// deadline is absolute: set once, it would expire mid-session and turn every
// later read into a timeout.
func (f *ftpObj) armRead() {
	if f.conn == nil {
		return
	}
	d := ftpTimeoutSeconds(f.readTimeout)
	if d <= 0 {
		// nil read_timeout: MRI waits indefinitely, and so do we. Clear any
		// deadline a previous setting left behind.
		_ = f.conn.SetReadDeadline(time.Time{})
		return
	}
	_ = f.conn.SetReadDeadline(time.Now().Add(d))
}

// ftpTimeoutErr re-raises a net timeout as the MRI exception for the phase,
// following the shape nethttp_bind.go's raiseTransportErr already uses — the
// HTTP binding has bounded its reads all along, and this one simply never got
// the same treatment.
func ftpTimeoutErr(err error, phase string) {
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		if phase == "open" {
			raise("Net::OpenTimeout", "execution expired")
		}
		raise("Net::ReadTimeout", "execution expired")
	}
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
		// The deadline is armed per line, not per reply: GetMultiline calls this
		// repeatedly for a multi-line reply, and read_timeout bounds each read
		// rather than the whole exchange.
		f.armRead()
		var line string
		var err error
		f.ftpIO(func() { line, err = f.r.ReadString('\n') })
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
	var werr error
	f.ftpIO(func() { _, werr = f.conn.Write([]byte(bytes)) })
	if werr != nil {
		ftpConnErr(werr)
	}
}

// getResp reads one assembled reply, records it as last_response, and classifies
// it: a 4yz/5yz/malformed reply re-raises as the matching Net::FTP error, a
// transport read failure as Net::FTPConnectionError.
func (f *ftpObj) getResp() string {
	resp, err := netftp.GetMultiline(f.reader())
	if err != nil {
		// A deadline expiry is Net::ReadTimeout, not a connection error: the
		// socket is fine, the peer simply said nothing in time, and the two are
		// rescued by different classes.
		ftpTimeoutErr(err, "read")
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
	var conn net.Conn
	var err error
	// Only the dial goes inside: ftpConnErr raises, and a panic crossing a
	// released GVL would unwind without re-acquiring it.
	f.ftpIO(func() { conn, err = net.Dial("tcp", net.JoinHostPort(host, strconv.Itoa(port))) })
	if err != nil {
		ftpConnErr(err)
	}
	return conn
}

// acceptData accepts the server's active-mode data callback, raising
// Net::FTPConnectionError when the accept fails.
// ⛔ THIS IS THE ONE #916 IS ABOUT. In ACTIVE mode the client listens and the
// server dials back, so a Ruby program that serves itself has both ends in one
// process. Holding the GVL here parks every other Ruby thread -- including the
// one that would make the call this Accept is waiting for. ruby/spec's net-ftp
// fixtures run exactly that shape, and their spec_helper puts Net::FTP into
// active mode.
func (f *ftpObj) acceptData(ln net.Listener) net.Conn {
	var conn net.Conn
	var err error
	f.ftpIO(func() { conn, err = ln.Accept() })
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
		// A write blocks once the peer stops reading and the window fills, so it
		// releases the GVL like every other call on this connection.
		var err error
		f.ftpIO(func() { _, err = conn.Write(data[off:end]) })
		if err != nil {
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
		var line string
		var err error
		f.ftpIO(func() { line, err = br.ReadString('\n') })
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
		var n int
		var err error
		f.ftpIO(func() { n, err = conn.Read(buf) })
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
	if n, ok := args[i].(object.Integer); ok && clampInt(n) > 0 {
		return clampInt(n)
	}
	return ftpDefaultBlocksize
}
