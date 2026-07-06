// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	stdtime "time"

	netftp "github.com/go-ruby-net-ftp/net-ftp"
)

// The Net::FTP binding is exercised against an in-process FTP server (below) so
// every path — control replies, PASV/EPSV/PORT/EPRT data connections, the
// LIST/NLST/MLSD/MLST listings, retr/stor transfers, and every error branch — is
// deterministic, uses only the loopback interface, and leaks no goroutine (the
// server's connections are force-closed and joined on cleanup).

// --- in-process FTP test server ---------------------------------------------

// ftpServer is a scriptable loopback FTP server. Default handlers implement a
// tiny virtual filesystem; overrides force a specific reply (or connection
// close) for a verb, and dataOverride replaces a listing's data payload, so the
// tests can drive both the happy paths and every error branch.
type ftpServer struct {
	ln           net.Listener
	wg           sync.WaitGroup
	mu           sync.Mutex
	conns        []net.Conn
	datals       []net.Listener
	overrides    map[string]string // VERB -> forced reply line ("@close" closes)
	dataOverride map[string]string // VERB -> data-channel payload
	files        map[string]string // RETR content by path
	stored       map[string]string // STOR content captured by path
}

// connState is one control connection's transfer state.
type connState struct {
	cwd      string
	dataLn   net.Listener // passive: listener awaiting the client's data dial
	dataAddr string       // active: the client's PORT/EPRT address to dial back
	renameFr string
}

func newFTPServer(t *testing.T) *ftpServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s := &ftpServer{
		ln:           ln,
		overrides:    map[string]string{},
		dataOverride: map[string]string{},
		files:        map[string]string{},
		stored:       map[string]string{},
	}
	s.wg.Add(1)
	go s.acceptLoop()
	t.Cleanup(func() {
		ln.Close()
		s.mu.Lock()
		for _, c := range s.conns {
			c.Close()
		}
		for _, d := range s.datals {
			d.Close()
		}
		s.mu.Unlock()
		s.wg.Wait()
	})
	return s
}

func (s *ftpServer) port() int { return s.ln.Addr().(*net.TCPAddr).Port }

// The override / dataOverride maps are read by connection goroutines and mutated
// by the tests between runs, so all access goes through these mutex-guarded
// accessors (the -race detector holds the gate).
func (s *ftpServer) setOverride(verb, reply string) {
	s.mu.Lock()
	s.overrides[verb] = reply
	s.mu.Unlock()
}
func (s *ftpServer) clearOverride(verb string) {
	s.mu.Lock()
	delete(s.overrides, verb)
	s.mu.Unlock()
}
func (s *ftpServer) getOverride(verb string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.overrides[verb]
	return v, ok
}
func (s *ftpServer) setDataOverride(verb, payload string) {
	s.mu.Lock()
	s.dataOverride[verb] = payload
	s.mu.Unlock()
}
func (s *ftpServer) clearDataOverride(verb string) {
	s.mu.Lock()
	delete(s.dataOverride, verb)
	s.mu.Unlock()
}
func (s *ftpServer) getDataOverride(verb string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.dataOverride[verb]
	return v, ok
}

func (s *ftpServer) acceptLoop() {
	defer s.wg.Done()
	for {
		c, err := s.ln.Accept()
		if err != nil {
			return
		}
		s.mu.Lock()
		s.conns = append(s.conns, c)
		s.mu.Unlock()
		s.wg.Add(1)
		go s.handleConn(c)
	}
}

// run compiles and runs a Ruby program with `require "net/ftp"` and a PORT
// constant prepended, returning its trimmed stdout.
func (s *ftpServer) run(t *testing.T, body string) string {
	t.Helper()
	return runSrc(t, "require \"net/ftp\"\nPORT = "+strconv.Itoa(s.port())+"\n"+body)
}

func (s *ftpServer) handleConn(c net.Conn) {
	defer s.wg.Done()
	defer c.Close()
	br := bufio.NewReader(c)
	w := func(str string) { c.Write([]byte(str)) }
	w("220 rbgo test FTP ready\r\n")
	st := &connState{cwd: "/"}
	for {
		raw, err := br.ReadString('\n')
		if err != nil {
			return
		}
		line := strings.TrimRight(raw, "\r\n")
		verb, arg := line, ""
		if i := strings.IndexByte(line, ' '); i >= 0 {
			verb, arg = line[:i], line[i+1:]
		}
		VERB := strings.ToUpper(verb)
		if ov, ok := s.getOverride(VERB); ok {
			if st.dataLn != nil { // a forced error on a transfer verb: release the data listener
				st.dataLn.Close()
				st.dataLn = nil
			}
			if ov == "@close" {
				return
			}
			w(ov)
			if VERB == "QUIT" {
				return
			}
			continue
		}
		if s.dispatch(w, st, VERB, arg) {
			return
		}
	}
}

// dispatch handles one command; it returns true when the connection should close.
func (s *ftpServer) dispatch(w func(string), st *connState, verb, arg string) bool {
	switch verb {
	case "USER":
		w("331 need password\r\n")
	case "PASS":
		w("230 logged in\r\n")
	case "ACCT":
		w("230 account ok\r\n")
	case "TYPE":
		w("200 type set\r\n")
	case "PWD", "XPWD":
		w("257 \"" + st.cwd + "\" is current directory\r\n")
	case "CWD":
		st.cwd = arg
		w("250 directory changed\r\n")
	case "CDUP":
		w("200 cdup ok\r\n")
	case "MKD":
		w("257 \"" + arg + "\" created\r\n")
	case "RMD":
		w("250 removed\r\n")
	case "DELE":
		w("250 deleted\r\n")
	case "RNFR":
		st.renameFr = arg
		w("350 ready for RNTO\r\n")
	case "RNTO":
		w("250 renamed\r\n")
	case "SIZE":
		w("213 " + strconv.Itoa(len(s.content(arg))) + "\r\n")
	case "MDTM":
		w("213 20200102030405\r\n")
	case "SYST":
		w("215 UNIX Type: L8\r\n")
	case "STAT":
		w("211 status ok\r\n")
	case "NOOP":
		w("200 noop\r\n")
	case "HELP":
		w("214 help text\r\n")
	case "SITE":
		w("200 site ok\r\n")
	case "PASV":
		s.openPassive(w, st, false)
	case "EPSV":
		s.openPassive(w, st, true)
	case "PORT", "EPRT":
		st.dataAddr = s.parseActive(verb, arg)
		w("200 port ok\r\n")
	case "MLST":
		w("250-Listing " + arg + "\r\n type=file;size=12;modify=20200102030405; " + s.pathOf(arg) + "\r\n250 End\r\n")
	case "LIST", "NLST", "MLSD":
		s.doSend(w, st, verb, arg)
	case "RETR":
		s.doRetr(w, st, arg)
	case "STOR":
		s.doStor(w, st, arg)
	case "QUIT":
		w("221 bye\r\n")
		return true
	case "ABOR":
		w("226 abort ok\r\n")
	default:
		w("500 unknown command\r\n")
	}
	return false
}

func (s *ftpServer) pathOf(arg string) string {
	if arg == "" {
		return "/file.txt"
	}
	return arg
}

func (s *ftpServer) content(arg string) string {
	if c, ok := s.files[arg]; ok {
		return c
	}
	return "line one\nline two\n"
}

// openPassive opens a data listener and replies with the PASV (227) or EPSV
// (229) address of the loopback data port.
func (s *ftpServer) openPassive(w func(string), st *connState, epsv bool) {
	dl, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		w("425 cannot open data connection\r\n")
		return
	}
	s.mu.Lock()
	s.datals = append(s.datals, dl)
	s.mu.Unlock()
	st.dataLn = dl
	port := dl.Addr().(*net.TCPAddr).Port
	if epsv {
		w(fmt.Sprintf("229 Entering Extended Passive Mode (|||%d|)\r\n", port))
		return
	}
	w(fmt.Sprintf("227 Entering Passive Mode (127,0,0,1,%d,%d)\r\n", port/256, port%256))
}

// parseActive parses a PORT (h1,h2,h3,h4,p1,p2) or EPRT (|proto|host|port|)
// argument into the dial-back address.
func (s *ftpServer) parseActive(verb, arg string) string {
	if verb == "EPRT" {
		parts := strings.Split(arg, "|")
		return net.JoinHostPort(parts[2], parts[3])
	}
	f := strings.Split(arg, ",")
	host := strings.Join(f[:4], ".")
	hi, _ := strconv.Atoi(f[4])
	lo, _ := strconv.Atoi(f[5])
	return net.JoinHostPort(host, strconv.Itoa(hi*256+lo))
}

// openData accepts (passive) or dials (active) the data connection.
func (s *ftpServer) openData(st *connState) net.Conn {
	if st.dataLn != nil {
		conn, err := st.dataLn.Accept()
		st.dataLn.Close()
		st.dataLn = nil
		if err != nil {
			return nil
		}
		return conn
	}
	conn, err := net.Dial("tcp", st.dataAddr)
	if err != nil {
		return nil
	}
	return conn
}

func (s *ftpServer) doSend(w func(string), st *connState, verb, arg string) {
	payload, ok := s.getDataOverride(verb)
	if !ok {
		switch verb {
		case "LIST":
			payload = "drwxr-xr-x 2 u g 4096 Jan 01 00:00 dir1\r\n-rw-r--r-- 1 u g 12 Jan 01 00:00 file.txt\r\n"
		case "NLST":
			payload = "dir1\r\nfile.txt\r\n"
		default: // MLSD
			payload = "type=dir;perm=el;unix.mode=0755; dir1\r\n" +
				"type=file;size=12;perm=rw;modify=20200102030405;unix.owner=1000; file.txt\r\n"
		}
	}
	w("150 opening data connection\r\n")
	conn := s.openData(st)
	if conn != nil {
		conn.Write([]byte(payload))
		conn.Close()
	}
	w("226 transfer complete\r\n")
}

func (s *ftpServer) doRetr(w func(string), st *connState, arg string) {
	w("150 opening data connection\r\n")
	conn := s.openData(st)
	if conn != nil {
		conn.Write([]byte(s.content(arg)))
		conn.Close()
	}
	w("226 transfer complete\r\n")
}

func (s *ftpServer) doStor(w func(string), st *connState, arg string) {
	w("150 ready to receive\r\n")
	conn := s.openData(st)
	if conn != nil {
		var b strings.Builder
		buf := make([]byte, 512)
		for {
			n, err := conn.Read(buf)
			if n > 0 {
				b.Write(buf[:n])
			}
			if err != nil {
				break
			}
		}
		conn.Close()
		s.mu.Lock()
		s.stored[arg] = b.String()
		s.mu.Unlock()
	}
	w("226 transfer complete\r\n")
}

func (s *ftpServer) storedContent(path string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stored[path]
}

// --- fakeConn / fakeAddr for the addressing-helper unit tests ---------------

type fakeAddr string

func (a fakeAddr) Network() string { return "fake" }
func (a fakeAddr) String() string  { return string(a) }

type fakeConn struct {
	remote net.Addr
	local  net.Addr
}

func (c *fakeConn) Read([]byte) (int, error)            { return 0, nil }
func (c *fakeConn) Write(p []byte) (int, error)         { return len(p), nil }
func (c *fakeConn) Close() error                        { return nil }
func (c *fakeConn) LocalAddr() net.Addr                 { return c.local }
func (c *fakeConn) RemoteAddr() net.Addr                { return c.remote }
func (c *fakeConn) SetDeadline(stdtime.Time) error      { return nil }
func (c *fakeConn) SetReadDeadline(stdtime.Time) error  { return nil }
func (c *fakeConn) SetWriteDeadline(stdtime.Time) error { return nil }

// dialRaw dials the server and returns a bare ftpObj (greeting consumed), for
// the direct-helper tests that need to flip unexported state (epsv / passive).
func dialRaw(t *testing.T, s *ftpServer) *ftpObj {
	t.Helper()
	conn, err := net.Dial("tcp", s.ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	f := &ftpObj{conn: conn, r: bufio.NewReader(conn), binary: true, passive: true}
	if _, err := f.r.ReadString('\n'); err != nil { // greeting
		t.Fatalf("greeting: %v", err)
	}
	return f
}

// --- tests: happy-path Ruby surface -----------------------------------------

func TestFTPRequireAndClassTree(t *testing.T) {
	got := runSrc(t, `
p require "net/ftp"
p require "net/ftp"
p Net::FTP.is_a?(Class)
p Net::FTP::MLSxEntry.is_a?(Class)
p Net::FTPError < StandardError
p Net::FTPReplyError < Net::FTPError
p Net::FTPTempError < Net::FTPError
p Net::FTPPermError < Net::FTPError
p Net::FTPProtoError < Net::FTPError
p Net::FTPConnectionError < Net::FTPError
`)
	want := "true\nfalse\ntrue\ntrue\ntrue\ntrue\ntrue\ntrue\ntrue\ntrue"
	if got != want {
		t.Fatalf("require/class tree got:\n%q\nwant:\n%q", got, want)
	}
}

func TestFTPConnectLoginBasics(t *testing.T) {
	s := newFTPServer(t)
	got := s.run(t, `
ftp = Net::FTP.new("127.0.0.1", port: PORT)
puts ftp.welcome
ftp.login
puts ftp.system
puts ftp.pwd
puts ftp.getdir
puts ftp.last_response_code
puts ftp.lastresp
puts ftp.closed?
ftp.quit
ftp.close
puts ftp.closed?
`)
	want := "220 rbgo test FTP ready\nUNIX Type: L8\n/\n/\n257\n257\nfalse\ntrue"
	if got != want {
		t.Fatalf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestFTPLoginVariants(t *testing.T) {
	s := newFTPServer(t)
	// Anonymous default password (USER anonymous -> 331 -> PASS anonymous@).
	if got := s.run(t, `Net::FTP.new("127.0.0.1", port: PORT).login; puts "ok"`); got != "ok" {
		t.Fatalf("anonymous: %q", got)
	}
	// new via options username/password.
	if got := s.run(t, `ftp = Net::FTP.new("127.0.0.1", port: PORT, username: "bob", password: "sekret"); puts ftp.welcome[0,3]`); got != "220" {
		t.Fatalf("opts login: %q", got)
	}
}

func TestFTPLoginAccountLadder(t *testing.T) {
	s := newFTPServer(t)
	s.setOverride("PASS", "332 need account\r\n")
	got := s.run(t, `
ftp = Net::FTP.new("127.0.0.1", port: PORT)
ftp.login("bob", "pw", "acct123")
puts "ok"
`)
	if got != "ok" {
		t.Fatalf("account ladder: %q", got)
	}
	// Missing account when the server demands one -> FTPReplyError.
	got = s.run(t, `
begin
  Net::FTP.new("127.0.0.1", port: PORT).login("bob", "pw")
rescue Net::FTPReplyError => e
  puts "reply-error"
end
`)
	if got != "reply-error" {
		t.Fatalf("missing acct: %q", got)
	}
}

func TestFTPLoginMissingPassword(t *testing.T) {
	s := newFTPServer(t)
	// USER -> 331 (needs pass) but no password supplied for a non-anonymous user.
	got := s.run(t, `
begin
  Net::FTP.new("127.0.0.1", port: PORT).login("bob")
rescue Net::FTPReplyError
  puts "need-pass"
end
`)
	if got != "need-pass" {
		t.Fatalf("missing pass: %q", got)
	}
}

func TestFTPDirectoryCommands(t *testing.T) {
	s := newFTPServer(t)
	got := s.run(t, `
ftp = Net::FTP.new("127.0.0.1", port: PORT); ftp.login
ftp.chdir("sub")
ftp.chdir("..")
puts ftp.mkdir("newdir")
ftp.rmdir("newdir")
ftp.delete("x.txt")
ftp.rename("a", "b")
ftp.noop
ftp.site("chmod 755 x")
puts ftp.status
puts ftp.help
`)
	want := "newdir\n211 status ok\n214 help text"
	if got != want {
		t.Fatalf("dir commands got:\n%q\nwant:\n%q", got, want)
	}
}

func TestFTPChdirCdupFallback(t *testing.T) {
	s := newFTPServer(t)
	// CDUP not understood (500) -> chdir("..") falls back to CWD "..".
	s.setOverride("CDUP", "500 not understood\r\n")
	got := s.run(t, `
ftp = Net::FTP.new("127.0.0.1", port: PORT); ftp.login
ftp.chdir("..")
puts "fell-back"
`)
	if got != "fell-back" {
		t.Fatalf("cdup fallback: %q", got)
	}
	// CDUP hard error (550) -> raises (not a 500, so no fallback).
	s.setOverride("CDUP", "550 denied\r\n")
	got = s.run(t, `
begin
  ftp = Net::FTP.new("127.0.0.1", port: PORT); ftp.login
  ftp.chdir("..")
rescue Net::FTPPermError
  puts "perm"
end
`)
	if got != "perm" {
		t.Fatalf("cdup perm: %q", got)
	}
	// CDUP proto error (non-standard code) -> FTPProtoError via ClassifyReply.
	s.setOverride("CDUP", "600 weird\r\n")
	got = s.run(t, `
begin
  ftp = Net::FTP.new("127.0.0.1", port: PORT); ftp.login
  ftp.chdir("..")
rescue Net::FTPProtoError
  puts "proto"
end
`)
	if got != "proto" {
		t.Fatalf("cdup proto: %q", got)
	}
	// CDUP replies a stray 3yz (passes ClassifyReply, not 2yz, not 500) -> FTPReplyError.
	s.setOverride("CDUP", "350 hmm\r\n")
	got = s.run(t, `
begin
  ftp = Net::FTP.new("127.0.0.1", port: PORT); ftp.login
  ftp.chdir("..")
rescue Net::FTPReplyError
  puts "reply"
end
`)
	if got != "reply" {
		t.Fatalf("cdup reply: %q", got)
	}
}

func TestFTPMetadataCommands(t *testing.T) {
	s := newFTPServer(t)
	got := s.run(t, `
ftp = Net::FTP.new("127.0.0.1", port: PORT); ftp.login
puts ftp.size("file.txt")
puts ftp.mdtm("file.txt")
puts ftp.mtime("file.txt").strftime("%Y-%m-%d %H:%M:%S")
`)
	want := strconv.Itoa(len("line one\nline two\n")) + "\n20200102030405\n2020-01-02 03:04:05"
	if got != want {
		t.Fatalf("metadata got:\n%q\nwant:\n%q", got, want)
	}
}

func TestFTPListings(t *testing.T) {
	s := newFTPServer(t)
	got := s.run(t, `
ftp = Net::FTP.new("127.0.0.1", port: PORT); ftp.login
p ftp.nlst
p ftp.list("/pub")
collected = []
ftp.list { |l| collected << l }
p collected.size
`)
	want := `["dir1", "file.txt"]` + "\n" +
		`["drwxr-xr-x 2 u g 4096 Jan 01 00:00 dir1", "-rw-r--r-- 1 u g 12 Jan 01 00:00 file.txt"]` + "\n2"
	if got != want {
		t.Fatalf("listings got:\n%q\nwant:\n%q", got, want)
	}
}

func TestFTPMlsd(t *testing.T) {
	s := newFTPServer(t)
	got := s.run(t, `
ftp = Net::FTP.new("127.0.0.1", port: PORT); ftp.login
entries = ftp.mlsd
d, f = entries
puts d.pathname
puts d.directory?
puts d.file?
puts d.listable?
puts f.pathname
puts f.file?
puts f.readable?
puts f.writable?
puts f.deletable?
puts f.facts["size"]
puts f.facts["modify"].strftime("%Y%m%d%H%M%S")
puts d.facts["unix.mode"]
puts f.facts["unix.owner"]
`)
	want := "dir1\ntrue\nfalse\ntrue\nfile.txt\ntrue\ntrue\ntrue\nfalse\n12\n20200102030405\n493\n1000"
	if got != want {
		t.Fatalf("mlsd got:\n%q\nwant:\n%q", got, want)
	}
}

func TestFTPMlsdBlockAndPermPredicates(t *testing.T) {
	s := newFTPServer(t)
	got := s.run(t, `
ftp = Net::FTP.new("127.0.0.1", port: PORT); ftp.login
names = []
ftp.mlsd("/pub") { |e| names << e.pathname }
p names
e = ftp.mlsd.first
puts [e.appendable?, e.creatable?, e.enterable?, e.renamable?, e.directory_makable?, e.purgeable?].inspect
`)
	want := `["dir1", "file.txt"]` + "\n[false, false, true, false, false, false]"
	if got != want {
		t.Fatalf("mlsd block/perm got:\n%q\nwant:\n%q", got, want)
	}
}

func TestFTPMlst(t *testing.T) {
	s := newFTPServer(t)
	got := s.run(t, `
ftp = Net::FTP.new("127.0.0.1", port: PORT); ftp.login
e = ftp.mlst("file.txt")
puts e.pathname
puts e.file?
puts e.facts["size"]
`)
	if got != "file.txt\ntrue\n12" {
		t.Fatalf("mlst got:\n%q", got)
	}
}

func TestFTPTransferEngines(t *testing.T) {
	s := newFTPServer(t)
	got := s.run(t, `
ftp = Net::FTP.new("127.0.0.1", port: PORT); ftp.login
lines = []
ftp.retrlines("RETR file.txt") { |l| lines << l }
p lines
chunks = "".b
ftp.retrbinary("RETR file.txt", 4) { |c| chunks << c }
p chunks
ftp.storlines("STOR up.txt", "a\nb\n")
ftp.storbinary("STOR bin.dat", "xyz".b, 2)
puts "done"
`)
	want := `["line one", "line two"]` + "\n" + `"line one\nline two\n"` + "\ndone"
	if got != want {
		t.Fatalf("transfer engines got:\n%q\nwant:\n%q", got, want)
	}
	if c := s.storedContent("up.txt"); c != "a\r\nb\r\n" {
		t.Fatalf("storlines captured %q", c)
	}
	if c := s.storedContent("bin.dat"); c != "xyz" {
		t.Fatalf("storbinary captured %q", c)
	}
}

func TestFTPGetPutFiles(t *testing.T) {
	s := newFTPServer(t)
	dir := t.TempDir()
	binLocal := filepath.Join(dir, "got.bin")
	txtLocal := filepath.Join(dir, "got.txt")
	upLocal := filepath.Join(dir, "up.txt")
	if err := os.WriteFile(upLocal, []byte("upload me\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := s.run(t, fmt.Sprintf(`
ftp = Net::FTP.new("127.0.0.1", port: PORT); ftp.login
ftp.getbinaryfile("file.txt", %q)
ftp.binary = false
ftp.gettextfile("file.txt", %q)
ftp.putbinaryfile(%q, "remote.bin")
ftp.binary = true
ftp.putbinaryfile(%q)
# block forms
bin = "".b
ftp.getbinaryfile("file.txt") { |c| bin << c }
p bin.bytesize
txt = []
ftp.gettextfile("file.txt") { |l| txt << l }
p txt
puts ftp.binary
`, binLocal, txtLocal, upLocal, upLocal))
	if !strings.Contains(got, "18\n") {
		t.Fatalf("get/put block got:\n%q", got)
	}
	if b, _ := os.ReadFile(binLocal); string(b) != "line one\nline two\n" {
		t.Fatalf("getbinaryfile wrote %q", b)
	}
	if b, _ := os.ReadFile(txtLocal); string(b) != "line one\nline two\n" {
		t.Fatalf("gettextfile wrote %q", b)
	}
	if c := s.storedContent("remote.bin"); c != "upload me\n" {
		t.Fatalf("putbinaryfile captured %q", c)
	}
	if c := s.storedContent("up.txt"); c != "upload me\n" { // default remote basename
		t.Fatalf("putbinaryfile default name captured %q", c)
	}
}

func TestFTPGetPutDispatchAndText(t *testing.T) {
	s := newFTPServer(t)
	dir := t.TempDir()
	local := filepath.Join(dir, "d.txt")
	local2 := filepath.Join(dir, "d2.txt")
	up := filepath.Join(dir, "u.txt")
	if err := os.WriteFile(up, []byte("hello\nworld\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := s.run(t, fmt.Sprintf(`
ftp = Net::FTP.new("127.0.0.1", port: PORT); ftp.login
ftp.get("file.txt", %q)             # binary dispatch (default binary)
ftp.puttextfile(%q, "textup.txt")
ftp.put(%q, "binput.txt")           # binary dispatch (put)
ftp.binary = false
ftp.get("file.txt", %q)             # text dispatch (get)
ftp.put(%q, "put.txt")              # text dispatch (put)
puts "ok"
`, local, up, up, local2, up))
	if got != "ok" {
		t.Fatalf("dispatch: %q", got)
	}
	if c := s.storedContent("binput.txt"); c != "hello\nworld\n" {
		t.Fatalf("put binary captured %q", c)
	}
	if c := s.storedContent("textup.txt"); c != "hello\r\nworld\r\n" {
		t.Fatalf("puttextfile captured %q", c)
	}
	if c := s.storedContent("put.txt"); c != "hello\r\nworld\r\n" {
		t.Fatalf("put text captured %q", c)
	}
}

func TestFTPActiveMode(t *testing.T) {
	s := newFTPServer(t)
	got := s.run(t, `
ftp = Net::FTP.new("127.0.0.1", port: PORT); ftp.login
ftp.passive = false
puts ftp.passive
p ftp.nlst
ftp.storlines("STOR a.txt", "x\n")
puts "active-done"
`)
	if !strings.Contains(got, "active-done") || !strings.Contains(got, `["dir1", "file.txt"]`) {
		t.Fatalf("active mode got:\n%q", got)
	}
	if s.storedContent("a.txt") != "x\r\n" {
		t.Fatalf("active stor captured %q", s.storedContent("a.txt"))
	}
}

func TestFTPUsePasvIP(t *testing.T) {
	s := newFTPServer(t)
	got := s.run(t, `
ftp = Net::FTP.new("127.0.0.1", port: PORT, use_pasv_ip: true); ftp.login
puts ftp.use_pasv_ip
p ftp.nlst
`)
	if !strings.Contains(got, "true") || !strings.Contains(got, `["dir1", "file.txt"]`) {
		t.Fatalf("use_pasv_ip got:\n%q", got)
	}
}

func TestFTPEPSVAndEPRT(t *testing.T) {
	s := newFTPServer(t)
	// EPSV (passive + epsv): drive the helper directly with epsv flipped on.
	f := dialRaw(t, s)
	f.epsv = true
	var lines []string
	f.retrlines(netftp.NlstCommand(""), func(l string) { lines = append(lines, l) })
	f.doClose()
	if strings.Join(lines, ",") != "dir1,file.txt" {
		t.Fatalf("epsv nlst: %v", lines)
	}
	// EPRT (active + epsv).
	f2 := dialRaw(t, s)
	f2.epsv = true
	f2.passive = false
	var lines2 []string
	f2.retrlines(netftp.NlstCommand(""), func(l string) { lines2 = append(lines2, l) })
	f2.doClose()
	if strings.Join(lines2, ",") != "dir1,file.txt" {
		t.Fatalf("eprt nlst: %v", lines2)
	}
}

func TestFTPOpenBlock(t *testing.T) {
	s := newFTPServer(t)
	// open with a block yields the client and closes it afterwards.
	got := s.run(t, `
res = Net::FTP.open("127.0.0.1", port: PORT) do |ftp|
  ftp.login
  ftp.pwd
end
puts res
`)
	if got != "/" {
		t.Fatalf("open block: %q", got)
	}
	// open without a block returns the client.
	got = s.run(t, `
ftp = Net::FTP.open("127.0.0.1", port: PORT)
ftp.login
puts ftp.pwd
ftp.close
`)
	if got != "/" {
		t.Fatalf("open no-block: %q", got)
	}
	// open's block close runs even when the block raises.
	got = s.run(t, `
begin
  Net::FTP.open("127.0.0.1", port: PORT) { |ftp| ftp.login; raise "boom" }
rescue => e
  puts e.message
end
`)
	if got != "boom" {
		t.Fatalf("open block raise: %q", got)
	}
}

func TestFTPNoHostConstructor(t *testing.T) {
	s := newFTPServer(t)
	// new with no host / nil host does not connect.
	got := s.run(t, `
ftp = Net::FTP.new
puts ftp.closed?
ftp2 = Net::FTP.new(nil)
puts ftp2.passive
ftp.connect("127.0.0.1", PORT)
ftp.login
puts ftp.pwd
`)
	if got != "true\ntrue\n/" {
		t.Fatalf("no-host constructor: %q", got)
	}
}

func TestFTPGenericCommands(t *testing.T) {
	s := newFTPServer(t)
	got := s.run(t, `
ftp = Net::FTP.new("127.0.0.1", port: PORT); ftp.login
puts ftp.sendcmd("NOOP")[0,3]
ftp.voidcmd("NOOP")
puts ftp.last_response[0,3]
puts ftp.abort[0,3]
`)
	if got != "200\n200\n226" {
		t.Fatalf("generic commands: %q", got)
	}
}

// --- tests: error branches --------------------------------------------------

func TestFTPErrorMapping(t *testing.T) {
	s := newFTPServer(t)
	cases := []struct{ reply, rescue, tag string }{
		{"421 service not available\r\n", "Net::FTPTempError", "temp"},
		{"550 permission denied\r\n", "Net::FTPPermError", "perm"},
		{"600 nonstandard\r\n", "Net::FTPProtoError", "proto"},
	}
	for _, c := range cases {
		s.setOverride("NOOP", c.reply)
		got := s.run(t, fmt.Sprintf(`
begin
  ftp = Net::FTP.new("127.0.0.1", port: PORT); ftp.login
  ftp.noop
rescue %s
  puts %q
end
`, c.rescue, c.tag))
		if got != c.tag {
			t.Fatalf("%s: got %q", c.tag, got)
		}
	}
	s.clearOverride("NOOP")
}

func TestFTPConnectionErrors(t *testing.T) {
	// Dial to a closed port -> Net::FTPConnectionError.
	closed := freePort(t)
	got := runSrc(t, fmt.Sprintf(`require "net/ftp"
begin
  Net::FTP.new("127.0.0.1", port: %d)
rescue Net::FTPConnectionError
  puts "conn-err"
end`, closed))
	if got != "conn-err" {
		t.Fatalf("dial error: %q", got)
	}
	// Server closes the control connection before replying -> read failure.
	s := newFTPServer(t)
	s.setOverride("NOOP", "@close")
	got = s.run(t, `
begin
  ftp = Net::FTP.new("127.0.0.1", port: PORT); ftp.login
  ftp.noop
rescue Net::FTPConnectionError
  puts "read-err"
end`)
	if got != "read-err" {
		t.Fatalf("read error: %q", got)
	}
}

func TestFTPCommandErrorBranches(t *testing.T) {
	s := newFTPServer(t)
	// Each of these replies passes getResp (1/2/3yz) but fails the command's own
	// classifier, exercising the ftpRaise error branch in that method.
	type tc struct {
		verb, reply, body, rescue, tag string
	}
	cases := []tc{
		{"DELE", "200 ok\r\n", `ftp.delete("x")`, "Net::FTPReplyError", "dele"},
		{"RNFR", "200 ok\r\n", `ftp.rename("a", "b")`, "Net::FTPReplyError", "rnfr"},
		{"SIZE", "200 nope\r\n", `ftp.size("x")`, "Net::FTPReplyError", "size"},
		{"SYST", "200 nope\r\n", `ftp.system`, "Net::FTPReplyError", "syst"},
		{"MDTM", "200 nope\r\n", `ftp.mdtm("x")`, "Net::FTPReplyError", "mdtm"},
		{"MDTM", "200 nope\r\n", `ftp.mtime("x")`, "Net::FTPReplyError", "mtime"},
		{"MKD", "250 no-quotes here\r\n", `ftp.mkdir("d")`, "Net::FTPReplyError", "mkd"},
		{"PWD", "250 no-quotes here\r\n", `ftp.pwd`, "Net::FTPReplyError", "pwd"},
	}
	for _, c := range cases {
		s.setOverride(c.verb, c.reply)
		got := s.run(t, fmt.Sprintf(`
begin
  ftp = Net::FTP.new("127.0.0.1", port: PORT); ftp.login
  %s
rescue %s
  puts %q
end
`, c.body, c.rescue, c.tag))
		if got != c.tag {
			t.Fatalf("%s (verb %s): got %q", c.tag, c.verb, got)
		}
		s.clearOverride(c.verb)
	}
}

func TestFTPPwdParse257Empty(t *testing.T) {
	s := newFTPServer(t)
	// 257 without a quoted string decodes to "" (Parse257 no-match branch).
	s.setOverride("PWD", "257 no quoted path\r\n")
	got := s.run(t, `
ftp = Net::FTP.new("127.0.0.1", port: PORT); ftp.login
puts ftp.pwd.empty?
`)
	if got != "true" {
		t.Fatalf("pwd empty: %q", got)
	}
}

func TestFTPMtimeInvalid(t *testing.T) {
	s := newFTPServer(t)
	s.setOverride("MDTM", "213 not-a-time\r\n")
	got := s.run(t, `
begin
  ftp = Net::FTP.new("127.0.0.1", port: PORT); ftp.login
  ftp.mtime("x")
rescue Net::FTPProtoError
  puts "proto"
end
`)
	if got != "proto" {
		t.Fatalf("mtime invalid: %q", got)
	}
}

func TestFTPMlstErrors(t *testing.T) {
	s := newFTPServer(t)
	// Not a 250 reply -> FTPReplyError.
	s.setOverride("MLST", "200 not listing\r\n")
	if got := s.run(t, `
begin
  ftp = Net::FTP.new("127.0.0.1", port: PORT); ftp.login; ftp.mlst("x")
rescue Net::FTPReplyError
  puts "reply"
end`); got != "reply" {
		t.Fatalf("mlst reply: %q", got)
	}
	// 250 single-line (no entry line) -> FTPProtoError.
	s.setOverride("MLST", "250 done\r\n")
	if got := s.run(t, `
begin
  ftp = Net::FTP.new("127.0.0.1", port: PORT); ftp.login; ftp.mlst("x")
rescue Net::FTPProtoError
  puts "proto"
end`); got != "proto" {
		t.Fatalf("mlst proto: %q", got)
	}
	// 250 with a malformed entry line (no space -> no pathname) -> FTPProtoError.
	s.setOverride("MLST", "250-list\r\nbadentrynopath\r\n250 End\r\n")
	if got := s.run(t, `
begin
  ftp = Net::FTP.new("127.0.0.1", port: PORT); ftp.login; ftp.mlst("x")
rescue Net::FTPProtoError
  puts "bad"
end`); got != "bad" {
		t.Fatalf("mlst bad entry: %q", got)
	}
	s.clearOverride("MLST")
}

func TestFTPMlsdParseError(t *testing.T) {
	s := newFTPServer(t)
	// A data line with no space has no pathname -> ParseMLSxEntry FTPProtoError.
	// First line is malformed (no space -> no pathname); a following valid line
	// then hits the "already errored" short-circuit before the raise.
	s.setDataOverride("MLSD", "type=file;size=1;nopath\r\ntype=file; ok\r\n")
	got := s.run(t, `
begin
  ftp = Net::FTP.new("127.0.0.1", port: PORT); ftp.login; ftp.mlsd
rescue Net::FTPProtoError
  puts "proto"
end
`)
	if got != "proto" {
		t.Fatalf("mlsd parse error: %q", got)
	}
	s.clearDataOverride("MLSD")
}

func TestFTPTransferCmdErrorReleasesData(t *testing.T) {
	s := newFTPServer(t)
	// A 5yz reply to a transfer command (after PASV opened a data listener)
	// re-raises and the binding closes the dialed data socket (no leak).
	s.setOverride("NLST", "550 denied\r\n")
	if got := s.run(t, `
begin
  ftp = Net::FTP.new("127.0.0.1", port: PORT); ftp.login; ftp.nlst
rescue Net::FTPPermError
  puts "perm"
end`); got != "perm" {
		t.Fatalf("transfer perm: %q", got)
	}
	// A stray 3yz reply is not a mark -> FTPReplyError.
	s.setOverride("NLST", "350 hmm\r\n")
	if got := s.run(t, `
begin
  ftp = Net::FTP.new("127.0.0.1", port: PORT); ftp.login; ftp.nlst
rescue Net::FTPReplyError
  puts "reply"
end`); got != "reply" {
		t.Fatalf("transfer mark: %q", got)
	}
	// Same error in active mode closes the listener (active cleanup path).
	s.setOverride("NLST", "550 denied\r\n")
	if got := s.run(t, `
begin
  ftp = Net::FTP.new("127.0.0.1", port: PORT); ftp.login
  ftp.passive = false
  ftp.nlst
rescue Net::FTPPermError
  puts "active-perm"
end`); got != "active-perm" {
		t.Fatalf("active transfer perm: %q", got)
	}
	s.clearOverride("NLST")
}

func TestFTPCRLFRejection(t *testing.T) {
	s := newFTPServer(t)
	got := s.run(t, `
ftp = Net::FTP.new("127.0.0.1", port: PORT); ftp.login
begin
  ftp.sendcmd("FOO\nBAR")
rescue ArgumentError
  puts "argerr"
end
`)
	if got != "argerr" {
		t.Fatalf("crlf rejection: %q", got)
	}
}

func TestFTPArityErrors(t *testing.T) {
	s := newFTPServer(t)
	for _, body := range []string{
		`ftp.connect`,
		`ftp.chdir`,
		`ftp.mkdir("a", "b")`,
		`ftp.rename("a")`,
		`ftp.size`,
		`ftp.sendcmd`,
		`ftp.retrlines`,
		`ftp.storlines("STOR x")`,
		`ftp.getbinaryfile`,
	} {
		got := s.run(t, fmt.Sprintf(`
ftp = Net::FTP.new("127.0.0.1", port: PORT); ftp.login
begin
  %s
rescue ArgumentError
  puts "argerr"
end
`, body))
		if got != "argerr" {
			t.Fatalf("arity %q: got %q", body, got)
		}
	}
}

func TestFTPGetPutFileErrors(t *testing.T) {
	s := newFTPServer(t)
	// putbinaryfile / puttextfile of a nonexistent local file -> Errno::ENOENT.
	for _, m := range []string{"putbinaryfile", "puttextfile"} {
		got := s.run(t, fmt.Sprintf(`
ftp = Net::FTP.new("127.0.0.1", port: PORT); ftp.login
begin
  ftp.%s("/no/such/file/at/all", "r")
rescue Errno::ENOENT
  puts "enoent"
end
`, m))
		if got != "enoent" {
			t.Fatalf("%s enoent: %q", m, got)
		}
	}
	// getbinaryfile / gettextfile to an unwritable local path -> Errno::EACCES.
	bad := filepath.Join(t.TempDir(), "nonexistent-dir", "out")
	for _, m := range []string{"getbinaryfile", "gettextfile"} {
		got := s.run(t, fmt.Sprintf(`
ftp = Net::FTP.new("127.0.0.1", port: PORT); ftp.login
begin
  ftp.%s("file.txt", %q)
rescue Errno::EACCES
  puts "eacces"
end
`, m, bad))
		if got != "eacces" {
			t.Fatalf("%s eacces: %q", m, got)
		}
	}
}

// --- tests: direct helper units ---------------------------------------------

func TestFTPIsIPv6(t *testing.T) {
	v4 := &fakeConn{remote: &net.TCPAddr{IP: net.IPv4(1, 2, 3, 4), Port: 1}}
	v6 := &fakeConn{remote: &net.TCPAddr{IP: net.ParseIP("::1"), Port: 1}}
	other := &fakeConn{remote: fakeAddr("no-port-here")}
	if ftpIsIPv6(v4) {
		t.Fatal("ipv4 reported as ipv6")
	}
	if !ftpIsIPv6(v6) {
		t.Fatal("ipv6 not detected")
	}
	if ftpIsIPv6(other) {
		t.Fatal("non-tcp addr reported as ipv6")
	}
}

func TestFTPHostHelpersMalformedAddr(t *testing.T) {
	// A portless address exercises the SplitHostPort error fallback (whole string).
	c := &fakeConn{remote: fakeAddr("hostonly"), local: fakeAddr("localonly")}
	f := &ftpObj{conn: c}
	if h := f.remoteHost(); h != "hostonly" {
		t.Fatalf("remoteHost fallback = %q", h)
	}
	if h := f.localHost(); h != "localonly" {
		t.Fatalf("localHost fallback = %q", h)
	}
	// And the normal split path.
	ok := &fakeConn{remote: fakeAddr("1.2.3.4:5"), local: fakeAddr("6.7.8.9:10")}
	f2 := &ftpObj{conn: ok}
	if h := f2.remoteHost(); h != "1.2.3.4" {
		t.Fatalf("remoteHost = %q", h)
	}
	if h := f2.localHost(); h != "6.7.8.9" {
		t.Fatalf("localHost = %q", h)
	}
}

func TestFTPTextHelpers(t *testing.T) {
	if got := ftpChomp("a\r\n"); got != "a" {
		t.Fatalf("chomp crlf: %q", got)
	}
	if got := ftpChomp("a\n"); got != "a" {
		t.Fatalf("chomp lf: %q", got)
	}
	if got := ftpChomp("a\r"); got != "a" {
		t.Fatalf("chomp cr: %q", got)
	}
	if got := ftpChomp("a"); got != "a" {
		t.Fatalf("chomp none: %q", got)
	}
	if got := ftpChomp(""); got != "" {
		t.Fatalf("chomp empty: %q", got)
	}
	if got := ftpToCRLF("a\nb\r\nc\n"); got != "a\r\nb\r\nc\r\n" {
		t.Fatalf("toCRLF: %q", got)
	}
	if got := ftpBasename("/a/b/c.txt"); got != "c.txt" {
		t.Fatalf("basename path: %q", got)
	}
	if got := ftpBasename("c.txt"); got != "c.txt" {
		t.Fatalf("basename bare: %q", got)
	}
}

func TestFTPInspectTruthyAndClose(t *testing.T) {
	s := newFTPServer(t)
	got := s.run(t, `
ftp = Net::FTP.new("127.0.0.1", port: PORT); ftp.login
p ftp
puts ftp.to_s
puts(ftp ? "truthy" : "falsy")
e = ftp.mlsd.first
p e
puts e.to_s.start_with?("#<Net::FTP::MLSxEntry")
puts(e ? "et" : "ef")
ftp.close
ftp.close             # second close is a no-op
puts ftp.closed?
`)
	want := "#<Net::FTP>\n#<Net::FTP>\ntruthy\n" +
		"#<Net::FTP::MLSxEntry dir1>\ntrue\net\ntrue"
	if got != want {
		t.Fatalf("inspect/truthy got:\n%q\nwant:\n%q", got, want)
	}
}

func TestFTPUnconnectedClose(t *testing.T) {
	got := runSrc(t, `require "net/ftp"
ftp = Net::FTP.new
ftp.close             # conn is nil -> no-op
puts ftp.closed?`)
	if got != "true" {
		t.Fatalf("unconnected close: %q", got)
	}
}

func TestFTPVoidRespNon2(t *testing.T) {
	s := newFTPServer(t)
	s.setOverride("NOOP", "350 not a 2yz\r\n")
	got := s.run(t, `
begin
  ftp = Net::FTP.new("127.0.0.1", port: PORT); ftp.login; ftp.noop
rescue Net::FTPReplyError
  puts "reply"
end`)
	if got != "reply" {
		t.Fatalf("voidresp non-2: %q", got)
	}
}

func TestFTPLoginErrorLadders(t *testing.T) {
	s := newFTPServer(t)
	// USER reply is a 1yz (passes getResp) but not 2/3yz -> LoginNext error.
	s.setOverride("USER", "150 odd\r\n")
	if got := s.run(t, `
begin
  Net::FTP.new("127.0.0.1", port: PORT).login("u", "p")
rescue Net::FTPReplyError
  puts "user"
end`); got != "user" {
		t.Fatalf("user ladder: %q", got)
	}
	s.clearOverride("USER")
	// PASS reply is a 1yz -> LoginNext(after PASS) error.
	s.setOverride("PASS", "150 odd\r\n")
	if got := s.run(t, `
begin
  Net::FTP.new("127.0.0.1", port: PORT).login("u", "p")
rescue Net::FTPReplyError
  puts "pass"
end`); got != "pass" {
		t.Fatalf("pass ladder: %q", got)
	}
	s.clearOverride("PASS")
	// ACCT reply is a 1yz (not 2yz) -> account step FTPReplyError.
	s.setOverride("PASS", "332 need account\r\n")
	s.setOverride("ACCT", "150 odd\r\n")
	if got := s.run(t, `
begin
  Net::FTP.new("127.0.0.1", port: PORT).login("u", "p", "acct")
rescue Net::FTPReplyError
  puts "acct"
end`); got != "acct" {
		t.Fatalf("acct ladder: %q", got)
	}
	s.clearOverride("PASS")
	s.clearOverride("ACCT")
}

func TestFTPMakepasvParseErrors(t *testing.T) {
	s := newFTPServer(t)
	// Malformed 227 -> FTPProtoError (Parse227).
	s.setOverride("PASV", "227 no address here\r\n")
	if got := s.run(t, `
begin
  ftp = Net::FTP.new("127.0.0.1", port: PORT); ftp.login; ftp.nlst
rescue Net::FTPProtoError
  puts "pasv"
end`); got != "pasv" {
		t.Fatalf("pasv parse: %q", got)
	}
	s.clearOverride("PASV")
	// Malformed 229 -> FTPProtoError (Parse229), driven via the EPSV path.
	s.setOverride("EPSV", "229 no delimiters\r\n")
	f := dialRaw(t, s)
	f.epsv = true
	expectRubyError(t, "Net::FTPProtoError", func() {
		f.retrlines(netftp.NlstCommand(""), func(string) {})
	})
	f.doClose()
	s.clearOverride("EPSV")
}

func TestFTPTransferReadError(t *testing.T) {
	s := newFTPServer(t)
	// Server closes the control connection while replying to the transfer command.
	s.setOverride("NLST", "@close")
	got := s.run(t, `
begin
  ftp = Net::FTP.new("127.0.0.1", port: PORT); ftp.login; ftp.nlst
rescue Net::FTPConnectionError
  puts "conn"
end`)
	if got != "conn" {
		t.Fatalf("transfer read error: %q", got)
	}
	s.clearOverride("NLST")
}

func TestFTPChdirReadError(t *testing.T) {
	s := newFTPServer(t)
	s.setOverride("CDUP", "@close")
	got := s.run(t, `
begin
  ftp = Net::FTP.new("127.0.0.1", port: PORT); ftp.login; ftp.chdir("..")
rescue Net::FTPConnectionError
  puts "conn"
end`)
	if got != "conn" {
		t.Fatalf("chdir read error: %q", got)
	}
	s.clearOverride("CDUP")
}

func TestFTPAbortErrors(t *testing.T) {
	s := newFTPServer(t)
	// A reply the codes MRI accepts do not include -> FTPProtoError.
	s.setOverride("ABOR", "500 bad\r\n")
	if got := s.run(t, `
begin
  ftp = Net::FTP.new("127.0.0.1", port: PORT); ftp.login; ftp.abort
rescue Net::FTPProtoError
  puts "proto"
end`); got != "proto" {
		t.Fatalf("abort proto: %q", got)
	}
	// Server closes before replying to ABOR -> FTPConnectionError.
	s.setOverride("ABOR", "@close")
	if got := s.run(t, `
begin
  ftp = Net::FTP.new("127.0.0.1", port: PORT); ftp.login; ftp.abort
rescue Net::FTPConnectionError
  puts "conn"
end`); got != "conn" {
		t.Fatalf("abort conn: %q", got)
	}
	s.clearOverride("ABOR")
}

func TestFTPOptionAndAliasCoverage(t *testing.T) {
	s := newFTPServer(t)
	// String-keyed options hash, the ls/dir/getdir aliases, and the setters'
	// return values.
	got := s.run(t, `
ftp = Net::FTP.new("127.0.0.1", {"port" => PORT}); ftp.login
p ftp.ls.size
p ftp.dir.size
puts ftp.getdir
puts(ftp.binary = false)
puts ftp.binary
puts(ftp.use_pasv_ip = true)
ftp.nlst(nil)           # explicit nil pathname -> ftpOptString nil branch
ftp.retrbinary("RETR x", 0) { |c| }   # invalid blocksize -> default
puts "ok"
`)
	if !strings.Contains(got, "ok") || !strings.HasPrefix(got, "2\n2\n/\nfalse\nfalse\ntrue\n") {
		t.Fatalf("option/alias coverage: %q", got)
	}
}

func TestFTPAccountAndPassiveOptions(t *testing.T) {
	s := newFTPServer(t)
	s.setOverride("PASS", "332 need account\r\n")
	got := s.run(t, `
ftp = Net::FTP.new("127.0.0.1", port: PORT, passive: false, username: "u", password: "p", account: "acct")
puts ftp.passive
p ftp.nlst
`)
	if !strings.HasPrefix(got, "false\n") || !strings.Contains(got, `["dir1", "file.txt"]`) {
		t.Fatalf("account/passive options: %q", got)
	}
	s.clearOverride("PASS")
}

func TestFTPStorlinesTypeError(t *testing.T) {
	s := newFTPServer(t)
	got := s.run(t, `
ftp = Net::FTP.new("127.0.0.1", port: PORT); ftp.login
begin
  ftp.storbinary("STOR x", 12345)
rescue TypeError
  puts "typeerr"
end
`)
	if got != "typeerr" {
		t.Fatalf("stor type error: %q", got)
	}
}

// --- direct helper error-branch units ---------------------------------------

// expectRubyError runs fn and asserts it raised the given Ruby exception class.
func expectRubyError(t *testing.T, class string, fn func()) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("expected raise %s, got none", class)
		}
		re, ok := r.(RubyError)
		if !ok {
			t.Fatalf("expected RubyError, got %T: %v", r, r)
		}
		if re.Class != class {
			t.Fatalf("expected %s, got %s", class, re.Class)
		}
	}()
	fn()
}

func TestFTPRaiseFallback(t *testing.T) {
	// A non-FTPError (a bare transport error) maps to Net::FTPConnectionError.
	expectRubyError(t, "Net::FTPConnectionError", func() {
		ftpRaise(fmt.Errorf("boom"))
	})
}

func TestFTPWriteLineError(t *testing.T) {
	s := newFTPServer(t)
	f := dialRaw(t, s)
	f.conn.Close() // writing to a closed socket fails
	expectRubyError(t, "Net::FTPConnectionError", func() { f.writeLine("NOOP") })
}

func TestFTPDialDataError(t *testing.T) {
	f := &ftpObj{}
	expectRubyError(t, "Net::FTPConnectionError", func() {
		f.dialData("127.0.0.1", freePort(t)) // nothing is listening
	})
}

func TestFTPAcceptDataError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ln.Close() // accept on a closed listener fails
	f := &ftpObj{}
	expectRubyError(t, "Net::FTPConnectionError", func() { f.acceptData(ln) })
}

func TestFTPSendDataError(t *testing.T) {
	c1, c2 := net.Pipe()
	c2.Close() // the peer is gone, so writes fail
	f := &ftpObj{}
	expectRubyError(t, "Net::FTPConnectionError", func() {
		f.sendData(c1, []byte("abcdef"), 2) // multiple chunks, then a write error
	})
}

func TestFTPMakeportListenError(t *testing.T) {
	// An unbindable local host makes net.Listen fail.
	f := &ftpObj{conn: &fakeConn{local: fakeAddr("300.300.300.300:0")}}
	expectRubyError(t, "Net::FTPConnectionError", func() { f.makeport() })
}

// freePort returns a port number with no listener bound to it.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return p
}
