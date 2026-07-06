// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"io"
	"math/big"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	netsmtp "github.com/go-ruby-net-smtp/net-smtp"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// --- in-process SMTP server ------------------------------------------------

// Canned reply fragments the scripted server sends. Multi-line EHLO replies use
// the "250-…/250 …" continuation form the codec's Capabilities parse expects.
const (
	smtpGreet     = "220 test ESMTP\r\n"
	smtpEHLO      = "250-test\r\n250 SIZE 1000\r\n" // no AUTH, no STARTTLS
	smtpEHLOAuth  = "250-test\r\n250-SIZE 1000\r\n250 AUTH PLAIN LOGIN CRAM-MD5\r\n"
	smtpOK        = "250 2.0.0 OK\r\n"
	smtpDataReady = "354 Start mail input\r\n"
	smtpQueued    = "250 2.0.0 queued\r\n"
	smtpBye       = "221 2.0.0 Bye\r\n"
)

// smtpDrive runs the request/response half of a session: for each client command
// line it sends the next scripted reply; a "354" reply enters DATA mode, draining
// client lines until a lone "." and then sending the following (final) reply.
func smtpDrive(r *bufio.Reader, w func(string), responses []string, idx int) {
	for idx < len(responses) {
		if _, err := r.ReadString('\n'); err != nil {
			return
		}
		resp := responses[idx]
		idx++
		w(resp)
		if strings.HasPrefix(resp, "354") {
			for {
				dl, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if strings.TrimRight(dl, "\r\n") == "." {
					break
				}
			}
			if idx < len(responses) {
				w(responses[idx])
				idx++
			}
		}
	}
}

// smtpDialog serves a full plaintext session: the greeting (responses[0]) then the
// command/response exchange.
func smtpDialog(conn net.Conn, responses []string) {
	if len(responses) == 0 {
		return
	}
	r := bufio.NewReader(conn)
	w := func(s string) { conn.Write([]byte(s)) }
	w(responses[0])
	smtpDrive(r, w, responses, 1)
}

// smtpServer starts a one-shot scripted plaintext SMTP server on 127.0.0.1 and
// returns its host and port. The accepted connection is closed on test cleanup,
// which unblocks the server goroutine no matter how the client left the session
// (so a mid-session error in the script never leaks a goroutine).
func smtpServer(t *testing.T, responses []string) (host, port string) {
	t.Helper()
	return smtpServeFunc(t, func(conn net.Conn) { smtpDialog(conn, responses) })
}

// smtpServeFunc starts a server that serves every accepted connection with the
// same handler (so a test may open several sessions against it), and wires the
// cleanup that stops the listener and closes any still-open connection — which
// unblocks a handler no matter how the client left the session, so a mid-session
// error in the script never leaks a goroutine.
func smtpServeFunc(t *testing.T, serve func(net.Conn)) (host, port string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	var mu sync.Mutex
	var conns []net.Conn
	var wg sync.WaitGroup
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := ln.Accept()
			if err != nil {
				break
			}
			mu.Lock()
			conns = append(conns, conn)
			mu.Unlock()
			wg.Add(1)
			go func(c net.Conn) {
				defer wg.Done()
				defer c.Close()
				serve(c)
			}(conn)
		}
		wg.Wait()
	}()
	t.Cleanup(func() {
		ln.Close()
		mu.Lock()
		for _, c := range conns {
			c.Close()
		}
		mu.Unlock()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("smtp test server did not stop")
		}
	})
	h, p, _ := net.SplitHostPort(ln.Addr().String())
	return h, p
}

// smtpTestCert generates a self-signed P-256 certificate for 127.0.0.1 (the client
// dials with VERIFY_NONE, so any valid cert is accepted).
func smtpTestCert(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("cert: %v", err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// STARTTLS server modes.
const (
	smtpTLSUpgrade   = iota // 220 then a real TLS handshake, then afterTLS over TLS
	smtpTLSReject           // 502 to STARTTLS (no upgrade)
	smtpTLSHandshake        // 220 then close before the handshake (handshake fails)
)

// smtpStartTLSServer serves the STARTTLS prologue in plaintext (greeting, EHLO →
// plainEHLO, STARTTLS → per mode) and, on a successful upgrade, the afterTLS
// exchange over TLS.
func smtpStartTLSServer(t *testing.T, plainEHLO string, afterTLS []string, mode int) (host, port string) {
	t.Helper()
	cert := smtpTestCert(t)
	return smtpServeFunc(t, func(conn net.Conn) {
		r := bufio.NewReader(conn)
		w := func(s string) { conn.Write([]byte(s)) }
		w(smtpGreet)
		if _, err := r.ReadString('\n'); err != nil { // EHLO
			return
		}
		w(plainEHLO)
		if _, err := r.ReadString('\n'); err != nil { // STARTTLS
			return
		}
		switch mode {
		case smtpTLSReject:
			w("502 5.5.1 STARTTLS not available\r\n")
			return
		case smtpTLSHandshake:
			w("220 2.0.0 Ready to start TLS\r\n")
			return // drop the connection before the handshake
		}
		w("220 2.0.0 Ready to start TLS\r\n")
		tconn := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{cert}})
		if err := tconn.Handshake(); err != nil {
			return
		}
		smtpDrive(bufio.NewReader(tconn), func(s string) { tconn.Write([]byte(s)) }, afterTLS, 0)
	})
}

// smtpImplicitTLSServer upgrades to TLS immediately on connect (SMTPS) and then
// serves the plaintext-style dialog over the TLS connection.
func smtpImplicitTLSServer(t *testing.T, responses []string) (host, port string) {
	t.Helper()
	cert := smtpTestCert(t)
	return smtpServeFunc(t, func(conn net.Conn) {
		tconn := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{cert}})
		if err := tconn.Handshake(); err != nil {
			return
		}
		smtpDialog(tconn, responses)
	})
}

// smtpScript substitutes the server host/port into a Ruby template.
func smtpScript(host, port, tmpl string) string {
	return strings.ReplaceAll(strings.ReplaceAll(tmpl, "HOST", host), "PORT", port)
}

// --- require, error tree, ports --------------------------------------------

func TestNetSMTPLoadable(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p require "net/smtp"`, "true\n"},
		{`require "net/smtp"; p require "net/smtp"`, "false\n"},
		{`require "net/smtp"; p Net::SMTP.is_a?(Class)`, "true\n"},
		{`require "net/smtp"; p Net::ProtocolError < StandardError`, "true\n"},
		{`require "net/smtp"; p Net::SMTPError < Net::ProtocolError`, "true\n"},
		{`require "net/smtp"; p Net::SMTPUnknownError < Net::SMTPError`, "true\n"},
		{`require "net/smtp"; p Net::SMTPServerBusy < Net::SMTPError`, "true\n"},
		{`require "net/smtp"; p Net::SMTPSyntaxError < Net::SMTPError`, "true\n"},
		{`require "net/smtp"; p Net::SMTPAuthenticationError < Net::SMTPError`, "true\n"},
		{`require "net/smtp"; p Net::SMTPFatalError < Net::SMTPError`, "true\n"},
		{`require "net/smtp"; p Net::SMTPUnsupportedCommand < Net::SMTPError`, "true\n"},
		{`require "net/smtp"; p Net::SMTP.default_port`, "25\n"},
		{`require "net/smtp"; p Net::SMTP.default_tls_port`, "465\n"},
		{`require "net/smtp"; p Net::SMTP.default_ssl_port`, "465\n"},
		{`require "net/smtp"; p Net::SMTP.default_submission_port`, "587\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// --- send_message / sendmail / response ------------------------------------

func TestNetSMTPSendMessage(t *testing.T) {
	host, port := smtpServer(t, []string{
		smtpGreet, smtpEHLO, smtpOK, smtpOK, smtpDataReady, smtpQueued, smtpBye,
	})
	got := eval(t, smtpScript(host, port, `
require "net/smtp"
Net::SMTP.start("HOST", PORT) do |smtp|
  r = smtp.send_message("From: a\r\nTo: b\r\n\r\nhi\r\n", "a@x.test", "b@y.test")
  puts r.status
  puts r.success?
  puts r.continue?
  puts r.message
  puts smtp.started?
end`))
	want := "250\ntrue\nfalse\n250 2.0.0 queued\ntrue\n"
	if got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}

func TestNetSMTPSendmailAliasAndArrayRcpt(t *testing.T) {
	host, port := smtpServer(t, []string{
		smtpGreet, smtpEHLO, smtpOK, smtpOK, smtpOK, smtpDataReady, smtpQueued, smtpBye,
	})
	got := eval(t, smtpScript(host, port, `
require "net/smtp"
smtp = Net::SMTP.new("HOST", PORT)
smtp.start
r = smtp.sendmail("body\r\n", "a@x.test", ["b@y.test", "c@z.test"])
puts r.status
smtp.finish
puts smtp.started?`))
	if got != "250\nfalse\n" {
		t.Errorf("got=%q", got)
	}
}

func TestNetSMTPOpenMessageStream(t *testing.T) {
	host, port := smtpServer(t, []string{
		smtpGreet, smtpEHLO, smtpOK, smtpOK, smtpDataReady, smtpQueued, smtpBye,
	})
	got := eval(t, smtpScript(host, port, `
require "net/smtp"
Net::SMTP.start("HOST", PORT) do |smtp|
  r = smtp.open_message_stream("a@x.test", ["b@y.test"]) do |f|
    f.puts "From: a"
    f.print "Sub"
    f.puts "ject: hi"
    f << "\r\n"
    n = f.write("body\r\n")
    f.puts
  end
  puts r.status
end`))
	if got != "250\n" {
		t.Errorf("got=%q", got)
	}
}

func TestNetSMTPReadyAlias(t *testing.T) {
	host, port := smtpServer(t, []string{
		smtpGreet, smtpEHLO, smtpOK, smtpOK, smtpDataReady, smtpQueued, smtpBye,
	})
	got := eval(t, smtpScript(host, port, `
require "net/smtp"
Net::SMTP.start("HOST", PORT) do |smtp|
  r = smtp.ready("a@x.test", "b@y.test") { |f| f.write "hello\r\n" }
  puts r.status
end`))
	if got != "250\n" {
		t.Errorf("got=%q", got)
	}
}

// --- data (msg + block) + low-level commands -------------------------------

func TestNetSMTPDataAndLowLevel(t *testing.T) {
	host, port := smtpServer(t, []string{
		smtpGreet, smtpEHLO,
		smtpOK,                    // mailfrom
		smtpOK,                    // rcptto
		smtpOK,                    // rset
		"250-test\r\n250 OK\r\n",  // ehlo (low-level)
		"250 hello\r\n",           // helo (low-level)
		smtpDataReady, smtpQueued, // data(msg)
		smtpDataReady, smtpQueued, // data { block }
		smtpBye,
	})
	got := eval(t, smtpScript(host, port, `
require "net/smtp"
Net::SMTP.start("HOST", PORT) do |smtp|
  puts smtp.mailfrom("a@x.test").status
  puts smtp.rcptto("b@y.test").status
  puts smtp.rset.status
  puts smtp.ehlo("me.test").status
  puts smtp.helo("me.test").status
  puts smtp.data("one\r\n").status
  puts smtp.data { |f| f.puts "two" }.status
end`))
	want := "250\n250\n250\n250\n250\n250\n250\n"
	if got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}

func TestNetSMTPRcpttoList(t *testing.T) {
	host, port := smtpServer(t, []string{
		smtpGreet, smtpEHLO, smtpOK, smtpOK, smtpOK, smtpBye,
	})
	got := eval(t, smtpScript(host, port, `
require "net/smtp"
Net::SMTP.start("HOST", PORT) do |smtp|
  a = smtp.rcptto_list(["b@y.test", "c@z.test"])
  puts a.length
  puts(smtp.rcptto_list(["d@w.test"]) { "blockval" })
end`))
	if got != "2\nblockval\n" {
		t.Errorf("got=%q", got)
	}
}

// --- authentication --------------------------------------------------------

func TestNetSMTPAuthMechanisms(t *testing.T) {
	cases := []struct {
		name    string
		replies []string
		call    string
	}{
		{"plain", []string{smtpGreet, smtpEHLOAuth, "235 2.7.0 OK\r\n", smtpBye},
			`smtp.authenticate("u", "p", :plain)`},
		{"login", []string{smtpGreet, smtpEHLOAuth, "334 VXNlcm5hbWU6\r\n", "334 UGFzc3dvcmQ6\r\n", "235 2.7.0 OK\r\n", smtpBye},
			`smtp.authenticate("u", "p", :login)`},
		{"cram", []string{smtpGreet, smtpEHLOAuth, "334 aGVsbG8=\r\n", "235 2.7.0 OK\r\n", smtpBye},
			`smtp.authenticate("u", "p", "CRAM-MD5")`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			host, port := smtpServer(t, c.replies)
			got := eval(t, smtpScript(host, port, `
require "net/smtp"
Net::SMTP.start("HOST", PORT) do |smtp|
  `+c.call+`
  puts "ok"
end`))
			if got != "ok\n" {
				t.Errorf("%s: got=%q", c.name, got)
			}
		})
	}
}

func TestNetSMTPAuthOnStart(t *testing.T) {
	host, port := smtpServer(t, []string{
		smtpGreet, smtpEHLOAuth, "235 2.7.0 OK\r\n", smtpBye,
	})
	got := eval(t, smtpScript(host, port, `
require "net/smtp"
Net::SMTP.start("HOST", PORT, "localhost", "user", "secret", :plain) do |smtp|
  puts smtp.started?
end`))
	if got != "true\n" {
		t.Errorf("got=%q", got)
	}
}

func TestNetSMTPAuthKeywordAndStringKeys(t *testing.T) {
	host, port := smtpServer(t, []string{
		smtpGreet, smtpEHLOAuth, "235 2.7.0 OK\r\n", smtpBye,
	})
	// String-keyed options exercise the smtpKw String fallback and the password:
	// alias for secret.
	got := eval(t, smtpScript(host, port, `
require "net/smtp"
Net::SMTP.start("HOST", PORT, {"helo" => "me", "user" => "u", "password" => "p", "authtype" => :plain, "starttls" => false}) do |smtp|
  puts smtp.started?
end`))
	if got != "true\n" {
		t.Errorf("got=%q", got)
	}
}

// --- capability predicates -------------------------------------------------

func TestNetSMTPCapabilities(t *testing.T) {
	host, port := smtpServer(t, []string{smtpGreet, smtpEHLOAuth, smtpBye})
	got := eval(t, smtpScript(host, port, `
require "net/smtp"
Net::SMTP.start("HOST", PORT) do |smtp|
  puts smtp.capable_plain_auth?
  puts smtp.capable_login_auth?
  puts smtp.capable_cram_md5_auth?
  puts smtp.capable_starttls?
  puts smtp.capable_auth_types.join(",")
end`))
	want := "true\ntrue\ntrue\nfalse\nPLAIN,LOGIN,CRAM-MD5\n"
	if got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}

func TestNetSMTPCapabilitiesAuthNoMatch(t *testing.T) {
	host, port := smtpServer(t, []string{smtpGreet, "250-test\r\n250 AUTH PLAIN\r\n", smtpBye})
	got := eval(t, smtpScript(host, port, `
require "net/smtp"
Net::SMTP.start("HOST", PORT) do |smtp|
  puts smtp.capable_plain_auth?
  puts smtp.capable_cram_md5_auth?
end`))
	if got != "true\nfalse\n" {
		t.Errorf("got=%q", got)
	}
}

func TestNetSMTPCapabilitiesBeforeStart(t *testing.T) {
	got := eval(t, `
require "net/smtp"
smtp = Net::SMTP.new("mail.test", 25)
puts smtp.capable_starttls?
puts smtp.capable_plain_auth?
puts smtp.capable_login_auth?
puts smtp.capable_auth_types.length
puts smtp.address
puts smtp.port`)
	if got != "false\nfalse\nfalse\n0\nmail.test\n25\n" {
		t.Errorf("got=%q", got)
	}
}

// --- ESMTP fallback and esmtp toggle ---------------------------------------

func TestNetSMTPEsmtpFallback(t *testing.T) {
	host, port := smtpServer(t, []string{
		smtpGreet, "502 5.5.1 EHLO not implemented\r\n", "250 hello\r\n", smtpBye,
	})
	got := eval(t, smtpScript(host, port, `
require "net/smtp"
Net::SMTP.start("HOST", PORT) do |smtp|
  puts smtp.esmtp?
end`))
	if got != "false\n" {
		t.Errorf("got=%q", got)
	}
}

func TestNetSMTPEsmtpDisabled(t *testing.T) {
	host, port := smtpServer(t, []string{smtpGreet, "250 hello\r\n", smtpBye})
	got := eval(t, smtpScript(host, port, `
require "net/smtp"
smtp = Net::SMTP.new("HOST", PORT)
puts smtp.esmtp
smtp.esmtp = false
smtp.start do |s|
  puts s.esmtp?
end`))
	if got != "true\nfalse\n" {
		t.Errorf("got=%q", got)
	}
}

// --- TLS: STARTTLS auto/always, implicit, exclusivity, errors --------------

func TestNetSMTPStartTLSAuto(t *testing.T) {
	after := []string{"250-test\r\n250 OK\r\n", smtpOK, smtpOK, smtpDataReady, smtpQueued, smtpBye}
	host, port := smtpStartTLSServer(t, "250-test\r\n250 STARTTLS\r\n", after, smtpTLSUpgrade)
	got := eval(t, smtpScript(host, port, `
require "net/smtp"
Net::SMTP.start("HOST", PORT) do |smtp|
  puts smtp.started?
  puts smtp.send_message("hi\r\n", "a@x.test", "b@y.test").status
end`))
	if got != "true\n250\n" {
		t.Errorf("got=%q", got)
	}
}

func TestNetSMTPStartTLSAlways(t *testing.T) {
	after := []string{"250 test\r\n", smtpBye}
	host, port := smtpStartTLSServer(t, smtpEHLO, after, smtpTLSUpgrade) // EHLO does not advertise STARTTLS
	got := eval(t, smtpScript(host, port, `
require "net/smtp"
smtp = Net::SMTP.new("HOST", PORT)
puts smtp.starttls_auto?
smtp.enable_starttls
puts smtp.starttls?
puts smtp.starttls_always?
smtp.start { |s| puts s.started? }`))
	if got != "true\ntrue\ntrue\ntrue\n" {
		t.Errorf("got=%q", got)
	}
}

func TestNetSMTPStartTLSReject(t *testing.T) {
	host, port := smtpStartTLSServer(t, smtpEHLO, nil, smtpTLSReject)
	class, _ := evalErr(t, smtpScript(host, port, `
require "net/smtp"
smtp = Net::SMTP.new("HOST", PORT)
smtp.enable_starttls
smtp.start`))
	if class != "Net::SMTPSyntaxError" {
		t.Errorf("class=%q", class)
	}
}

func TestNetSMTPStartTLSHandshakeFailure(t *testing.T) {
	host, port := smtpStartTLSServer(t, smtpEHLO, nil, smtpTLSHandshake)
	class, _ := evalErr(t, smtpScript(host, port, `
require "net/smtp"
smtp = Net::SMTP.new("HOST", PORT)
smtp.enable_starttls
smtp.start`))
	if class != "IOError" {
		t.Errorf("class=%q", class)
	}
}

func TestNetSMTPImplicitTLS(t *testing.T) {
	host, port := smtpImplicitTLSServer(t, []string{
		smtpGreet, smtpEHLO, smtpOK, smtpOK, smtpDataReady, smtpQueued, smtpBye,
	})
	got := eval(t, smtpScript(host, port, `
require "net/smtp"
Net::SMTP.start("HOST", PORT, tls: true) do |smtp|
  puts smtp.tls?
  puts smtp.ssl?
  puts smtp.send_message("hi\r\n", "a@x.test", "b@y.test").status
end`))
	if got != "true\ntrue\n250\n" {
		t.Errorf("got=%q", got)
	}
}

func TestNetSMTPImplicitTLSHandshakeFailure(t *testing.T) {
	// Implicit TLS against a plaintext server: the TLS wrap in smtpDial fails.
	host, port := smtpServer(t, []string{smtpGreet})
	class, _ := evalErr(t, smtpScript(host, port, `
require "net/smtp"
Net::SMTP.start("HOST", PORT, tls: true) { |s| }`))
	if class != "SocketError" {
		t.Errorf("class=%q", class)
	}
}

func TestNetSMTPTLSExclusivity(t *testing.T) {
	got := eval(t, `
require "net/smtp"
def excl
  yield
  "no-error"
rescue ArgumentError => e
  e.message
end
smtp = Net::SMTP.new("mail.test")
puts(excl { smtp.enable_starttls; smtp.enable_tls })
smtp2 = Net::SMTP.new("mail.test")
puts(excl { smtp2.enable_tls; smtp2.enable_starttls })
smtp3 = Net::SMTP.new("mail.test")
puts(excl { smtp3.enable_tls; smtp3.enable_starttls_auto })
smtp4 = Net::SMTP.new("mail.test")
smtp4.enable_ssl
puts smtp4.ssl?
smtp4.disable_tls
puts smtp4.tls?
smtp5 = Net::SMTP.new("mail.test")
smtp5.enable_starttls_auto
smtp5.disable_starttls
puts smtp5.starttls?
smtp5.disable_ssl
puts smtp5.ssl?`)
	want := "SMTPS and STARTTLS is exclusive\n" +
		"SMTPS and STARTTLS is exclusive\n" +
		"SMTPS and STARTTLS is exclusive\n" +
		"true\nfalse\nfalse\nfalse\n"
	if got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}

// --- timeouts --------------------------------------------------------------

func TestNetSMTPTimeouts(t *testing.T) {
	host, port := smtpServer(t, []string{smtpGreet, smtpEHLO, smtpOK, smtpOK, smtpDataReady, smtpQueued, smtpBye})
	got := eval(t, smtpScript(host, port, `
require "net/smtp"
smtp = Net::SMTP.new("HOST", PORT)
puts smtp.open_timeout.inspect
puts smtp.read_timeout.inspect
smtp.open_timeout = 5
smtp.read_timeout = 2.5
puts smtp.open_timeout
puts smtp.read_timeout
smtp.start do |s|
  puts s.send_message("hi\r\n", "a@x.test", "b@y.test").status
end`))
	want := "nil\nnil\n5\n2.5\n250\n"
	if got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}

// --- runtime error paths ---------------------------------------------------

func TestNetSMTPGreetingErrors(t *testing.T) {
	// Non-2xx greeting → the reply's exception class.
	host, port := smtpServer(t, []string{"554 5.3.2 no service\r\n"})
	if class, _ := evalErr(t, smtpScript(host, port, `require "net/smtp"; Net::SMTP.start("HOST", PORT) { |s| }`)); class != "Net::SMTPFatalError" {
		t.Errorf("greeting class=%q", class)
	}
	// Server closes before greeting → transport IOError.
	host2, port2 := smtpServer(t, []string{})
	if class, _ := evalErr(t, smtpScript(host2, port2, `require "net/smtp"; Net::SMTP.start("HOST", PORT) { |s| }`)); class != "IOError" {
		t.Errorf("no-greeting class=%q", class)
	}
}

func TestNetSMTPEhloTransportError(t *testing.T) {
	// Greeting then immediate close: EHLO read hits EOF (a non-SMTP error).
	host, port := smtpServer(t, []string{smtpGreet})
	if class, _ := evalErr(t, smtpScript(host, port, `require "net/smtp"; Net::SMTP.start("HOST", PORT) { |s| }`)); class != "IOError" {
		t.Errorf("class=%q", class)
	}
}

func TestNetSMTPHeloFallbackBothFail(t *testing.T) {
	host, port := smtpServer(t, []string{smtpGreet, "502 no ehlo\r\n", "550 no helo\r\n"})
	if class, _ := evalErr(t, smtpScript(host, port, `require "net/smtp"; Net::SMTP.start("HOST", PORT) { |s| }`)); class != "Net::SMTPFatalError" {
		t.Errorf("class=%q", class)
	}
}

func TestNetSMTPHeloDisabledError(t *testing.T) {
	host, port := smtpServer(t, []string{smtpGreet, "550 no helo\r\n"})
	class, _ := evalErr(t, smtpScript(host, port, `
require "net/smtp"
smtp = Net::SMTP.new("HOST", PORT)
smtp.esmtp = false
smtp.start`))
	if class != "Net::SMTPFatalError" {
		t.Errorf("class=%q", class)
	}
}

func TestNetSMTPMailfromError(t *testing.T) {
	host, port := smtpServer(t, []string{smtpGreet, smtpEHLO, "550 5.1.0 bad sender\r\n", smtpBye})
	got := eval(t, smtpScript(host, port, `
require "net/smtp"
smtp = Net::SMTP.new("HOST", PORT)
smtp.start
begin
  smtp.send_message("hi\r\n", "a@x.test", "b@y.test")
rescue Net::SMTPFatalError => e
  puts e.class
end
smtp.finish`))
	if got != "Net::SMTPFatalError\n" {
		t.Errorf("got=%q", got)
	}
}

func TestNetSMTPDataContinueError(t *testing.T) {
	host, port := smtpServer(t, []string{smtpGreet, smtpEHLO, "550 no data\r\n", smtpBye})
	got := eval(t, smtpScript(host, port, `
require "net/smtp"
smtp = Net::SMTP.new("HOST", PORT)
smtp.start
begin
  smtp.data("hi\r\n")
rescue Net::SMTPUnknownError => e
  puts e.class
end
smtp.finish`))
	if got != "Net::SMTPUnknownError\n" {
		t.Errorf("got=%q", got)
	}
}

func TestNetSMTPAuthFailure(t *testing.T) {
	host, port := smtpServer(t, []string{smtpGreet, smtpEHLOAuth, "535 5.7.8 bad creds\r\n", smtpBye})
	got := eval(t, smtpScript(host, port, `
require "net/smtp"
smtp = Net::SMTP.new("HOST", PORT)
smtp.start
begin
  smtp.authenticate("u", "p", :plain)
rescue Net::SMTPAuthenticationError => e
  puts e.class
end
smtp.finish`))
	if got != "Net::SMTPAuthenticationError\n" {
		t.Errorf("got=%q", got)
	}
}

func TestNetSMTPUnknownAuthType(t *testing.T) {
	host, port := smtpServer(t, []string{smtpGreet, smtpEHLOAuth, smtpBye})
	class, msg := evalErr(t, smtpScript(host, port, `
require "net/smtp"
Net::SMTP.start("HOST", PORT) { |s| s.authenticate("u", "p", :bogus) }`))
	if class != "ArgumentError" || !strings.Contains(msg, "unknown auth type") {
		t.Errorf("class=%q msg=%q", class, msg)
	}
}

func TestNetSMTPDialError(t *testing.T) {
	// Bind then release a port so nothing is listening.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	host, port, _ := net.SplitHostPort(addr)
	class, _ := evalErr(t, smtpScript(host, port, `require "net/smtp"; Net::SMTP.start("HOST", PORT) { |s| }`))
	if class != "SocketError" {
		t.Errorf("class=%q", class)
	}
}

func TestNetSMTPStartTwice(t *testing.T) {
	host, port := smtpServer(t, []string{smtpGreet, smtpEHLO, smtpBye})
	class, _ := evalErr(t, smtpScript(host, port, `
require "net/smtp"
Net::SMTP.start("HOST", PORT) { |s| s.start }`))
	if class != "IOError" {
		t.Errorf("class=%q", class)
	}
}

// --- argument / guard errors (no connection needed) ------------------------

func TestNetSMTPArgumentErrors(t *testing.T) {
	cases := []struct{ src, class, msgHas string }{
		{`require "net/smtp"; Net::SMTP.new`, "ArgumentError", "expected 1..2"},
		{`require "net/smtp"; Net::SMTP.start`, "ArgumentError", "expected 1+"},
		{`require "net/smtp"; Net::SMTP.new("h").mailfrom("a")`, "IOError", "not yet started"},
		{`require "net/smtp"; Net::SMTP.new("h").rcptto("a")`, "IOError", "not yet started"},
		{`require "net/smtp"; Net::SMTP.new("h").rset`, "IOError", "not yet started"},
		{`require "net/smtp"; Net::SMTP.new("h").ehlo("a")`, "IOError", "not yet started"},
		{`require "net/smtp"; Net::SMTP.new("h").helo("a")`, "IOError", "not yet started"},
		{`require "net/smtp"; Net::SMTP.new("h").data("x")`, "IOError", "not yet started"},
		{`require "net/smtp"; Net::SMTP.new("h").send_message("m","f","t")`, "IOError", "not yet started"},
		{`require "net/smtp"; Net::SMTP.new("h").open_message_stream("f","t"){}`, "IOError", "not yet started"},
		{`require "net/smtp"; Net::SMTP.new("h").rcptto_list(["t"])`, "IOError", "not yet started"},
		{`require "net/smtp"; Net::SMTP.new("h").authenticate("u","p")`, "IOError", "not yet started"},
		{`require "net/smtp"; Net::SMTP.new("h").finish`, "IOError", "not yet started"},
	}
	for _, c := range cases {
		class, msg := evalErr(t, c.src)
		if class != c.class || !strings.Contains(msg, c.msgHas) {
			t.Errorf("src=%q class=%q msg=%q want class=%q has=%q", c.src, class, msg, c.class, c.msgHas)
		}
	}
}

func TestNetSMTPStartedGuardArgErrors(t *testing.T) {
	host, port := smtpServer(t, []string{smtpGreet, smtpEHLO, smtpBye})
	cases := []struct{ call, class, has string }{
		{`smtp.send_message("only")`, "ArgumentError", "expected 2+"},
		{`smtp.send_message("m", "from")`, "ArgumentError", "mail destination"},
		{`smtp.open_message_stream("from")`, "LocalJumpError", "no block"},
		{`smtp.open_message_stream { |f| }`, "ArgumentError", "expected 2"},
		{`smtp.open_message_stream("from") { |f| }`, "ArgumentError", "mail destination"},
		{`smtp.rcptto_list([])`, "ArgumentError", "mail destination"},
		{`smtp.authenticate("u")`, "ArgumentError", "expected 2..3"},
		{`smtp.data`, "ArgumentError", "message or block"},
	}
	for _, c := range cases {
		class, msg := evalErr(t, smtpScript(host, port, `
require "net/smtp"
smtp = Net::SMTP.new("HOST", PORT)
smtp.start
`+c.call))
		if class != c.class || !strings.Contains(msg, c.has) {
			t.Errorf("call=%q class=%q msg=%q want=%q/%q", c.call, class, msg, c.class, c.has)
		}
	}
}

// --- Go-level unit tests for branches not reachable from Ruby --------------

// TestNetSMTPObjectStrings exercises to_s / inspect / truthiness on the three
// value types and the Response#string reader.
func TestNetSMTPObjectStrings(t *testing.T) {
	host, port := smtpServer(t, []string{
		smtpGreet, smtpEHLO,
		smtpOK, smtpOK, smtpDataReady, smtpQueued, // send_message
		smtpOK, smtpOK, smtpDataReady, smtpQueued, // open_message_stream
		smtpBye,
	})
	got := eval(t, smtpScript(host, port, `
require "net/smtp"
Net::SMTP.start("HOST", PORT) do |smtp|
  puts smtp.to_s.include?("Net::SMTP")
  puts smtp.inspect.include?("Net::SMTP")
  puts(smtp ? "t" : "f")
  r = smtp.send_message("x\r\n", "a@x.test", "b@y.test")
  puts r.to_s
  puts r.inspect.include?("Response")
  puts r.string.length > 0
  puts(r ? "t" : "f")
  smtp.open_message_stream("a@x.test", "b@y.test") do |g|
    puts g.to_s.include?("Adapter")
    puts g.inspect.include?("Adapter")
    puts(g ? "t" : "f")
    g.write "y\r\n"
  end
end`))
	want := "true\ntrue\nt\n250 2.0.0 queued\ntrue\ntrue\nt\ntrue\ntrue\nt\n"
	if got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}

// TestNetSMTPSecretKeywordAndDefaultAuth covers the secret: keyword, a two-arg
// authenticate (default :plain), and capable_plain_auth? with an AUTH-less EHLO.
func TestNetSMTPSecretKeywordAndDefaultAuth(t *testing.T) {
	h1, p1 := smtpServer(t, []string{smtpGreet, smtpEHLOAuth, "235 2.7.0 OK\r\n", smtpBye})
	if got := eval(t, smtpScript(h1, p1, `
require "net/smtp"
Net::SMTP.start("HOST", PORT, user: "u", secret: "p", authtype: :plain) { |s| puts s.started? }`)); got != "true\n" {
		t.Errorf("secret kw: got=%q", got)
	}

	h2, p2 := smtpServer(t, []string{smtpGreet, smtpEHLO, "235 2.7.0 OK\r\n", smtpBye})
	if got := eval(t, smtpScript(h2, p2, `
require "net/smtp"
Net::SMTP.start("HOST", PORT) do |smtp|
  puts smtp.capable_plain_auth?
  smtp.authenticate("u", "p")
  puts "authed"
end`)); got != "false\nauthed\n" {
		t.Errorf("default auth: got=%q", got)
	}
}

// TestNetSMTPNoArgEhloAndLowLevelError covers a no-argument ehlo (empty domain)
// and a low-level command failure (smtpCmd's error path).
func TestNetSMTPNoArgEhloAndLowLevelError(t *testing.T) {
	host, port := smtpServer(t, []string{smtpGreet, smtpEHLO, "250 ok\r\n", "550 5.1.0 bad\r\n", smtpBye})
	got := eval(t, smtpScript(host, port, `
require "net/smtp"
Net::SMTP.start("HOST", PORT) do |smtp|
  puts smtp.ehlo.status
  begin
    smtp.mailfrom("nope")
  rescue Net::SMTPFatalError => e
    puts e.class
  end
end`))
	if got != "250\nNet::SMTPFatalError\n" {
		t.Errorf("got=%q", got)
	}
}

// TestNetSMTPSendAndStreamErrors covers the RCPT/DATA failure branches of
// send_message and open_message_stream.
func TestNetSMTPSendAndStreamErrors(t *testing.T) {
	cases := []struct {
		name      string
		responses []string
		call      string
		wantClass string
	}{
		{"send_rcpt", []string{smtpGreet, smtpEHLO, smtpOK, "550 bad rcpt\r\n", smtpBye},
			`smtp.send_message("m\r\n", "a@x.test", "b@y.test")`, "Net::SMTPFatalError"},
		{"send_data", []string{smtpGreet, smtpEHLO, smtpOK, smtpOK, "550 no data\r\n", smtpBye},
			`smtp.send_message("m\r\n", "a@x.test", "b@y.test")`, "Net::SMTPUnknownError"},
		{"stream_mail", []string{smtpGreet, smtpEHLO, "550 bad from\r\n", smtpBye},
			`smtp.open_message_stream("a@x.test", "b@y.test") { |f| f.write "x" }`, "Net::SMTPFatalError"},
		{"stream_rcpt", []string{smtpGreet, smtpEHLO, smtpOK, "550 bad rcpt\r\n", smtpBye},
			`smtp.open_message_stream("a@x.test", "b@y.test") { |f| f.write "x" }`, "Net::SMTPFatalError"},
		{"stream_data", []string{smtpGreet, smtpEHLO, smtpOK, smtpOK, "550 no data\r\n", smtpBye},
			`smtp.open_message_stream("a@x.test", "b@y.test") { |f| f.write "x" }`, "Net::SMTPUnknownError"},
		{"rcptto_list", []string{smtpGreet, smtpEHLO, "550 bad rcpt\r\n", smtpBye},
			`smtp.rcptto_list(["b@y.test"])`, "Net::SMTPFatalError"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			host, port := smtpServer(t, c.responses)
			got := eval(t, smtpScript(host, port, `
require "net/smtp"
smtp = Net::SMTP.new("HOST", PORT)
smtp.start
begin
  `+c.call+`
rescue Net::SMTPError => e
  puts e.class
end
smtp.finish`))
			if got != c.wantClass+"\n" {
				t.Errorf("%s: got=%q want=%q", c.name, got, c.wantClass)
			}
		})
	}
}

func TestNetSMTPUnitHelpers(t *testing.T) {
	// smtpStartTLSMode across all argument shapes.
	if smtpStartTLSMode(object.Symbol("always")) != smtpStartTLSAlways {
		t.Error("symbol always")
	}
	if smtpStartTLSMode(object.Symbol("auto")) != smtpStartTLSAuto {
		t.Error("symbol auto")
	}
	if smtpStartTLSMode(object.Symbol("nope")) != smtpStartTLSOff {
		t.Error("symbol other")
	}
	if smtpStartTLSMode(object.Bool(true)) != smtpStartTLSAlways {
		t.Error("bool true")
	}
	if smtpStartTLSMode(object.Bool(false)) != smtpStartTLSOff {
		t.Error("bool false")
	}
	if smtpStartTLSMode(object.NewString("x")) != smtpStartTLSAlways {
		t.Error("truthy other")
	}
	if smtpStartTLSMode(object.NilV) != smtpStartTLSOff {
		t.Error("nil other")
	}

	// smtpAuthTypeName normalisation.
	if got := smtpAuthTypeName(object.Symbol("CRAM_MD5")); got != "cram_md5" {
		t.Errorf("authtype=%q", got)
	}
	if got := smtpAuthTypeName(object.NewString("Cram-MD5")); got != "cram_md5" {
		t.Errorf("authtype str=%q", got)
	}

	// smtpStr and smtpMessageString across value shapes.
	if smtpStr(object.NewString("a")) != "a" || smtpStr(object.IntValue(7)) != "7" {
		t.Error("smtpStr")
	}
	if smtpMessageString(object.NewString("x")) != "x" {
		t.Error("msg string")
	}
	if smtpMessageString(object.NewArrayFromSlice([]object.Value{object.NewString("a"), object.IntValue(1)})) != "a1" {
		t.Error("msg array")
	}
	if smtpMessageString(object.IntValue(9)) != "9" {
		t.Error("msg other")
	}

	// smtpDurationValue across ranges.
	if smtpDurationValue(0) != object.NilV {
		t.Error("dur zero")
	}
	if v := smtpDurationValue(5 * time.Second); v != object.IntValue(5) {
		t.Errorf("dur int=%v", v)
	}
	if v := smtpDurationValue(2500 * time.Millisecond); v != object.Float(2.5) {
		t.Errorf("dur float=%v", v)
	}

	// smtpRespErr keys off the reply code.
	if e := smtpRespErr(netsmtp.ParseResponse("530 auth\r\n")); e.Kind != netsmtp.KindAuthentication {
		t.Errorf("respErr kind=%v", e.Kind)
	}
}

func TestNetSMTPUnitConnAndFinish(t *testing.T) {
	// closeConn on a session with no socket, and finish on a nil-session object,
	// exercise the nil-guard branches unreachable from a live Ruby session.
	(&SMTPObj{}).closeConn()
	New(io.Discard).smtpFinishQuiet(&SMTPObj{})

	// smtpConn.StartTLS with a stream that has no net.Conn cannot upgrade.
	c := &smtpConn{stream: &tcpSocket{}}
	if err := c.StartTLS(); err == nil {
		t.Error("StartTLS on connless stream should fail")
	}

	// capableAuth / capableStartTLS on a driverless object.
	o := &SMTPObj{}
	if o.capableAuth("PLAIN") || o.capableStartTLS() {
		t.Error("capability without session should be false")
	}

	// smtpConn read/write over a net.Pipe: WriteLine appends CRLF, ReadLine chops
	// it, WriteRaw is verbatim; a closed peer surfaces ReadLine's error path.
	cli, srv := net.Pipe()
	sc := &smtpConn{stream: newTCPSocket(nil, cli), host: "127.0.0.1"}
	go func() {
		br := bufio.NewReader(srv)
		br.ReadString('\n') // consume the written line
		srv.Write([]byte("250 OK\r\n"))
		srv.Close()
	}()
	if err := sc.WriteLine("EHLO me"); err != nil {
		t.Fatalf("WriteLine: %v", err)
	}
	line, err := sc.ReadLine()
	if err != nil || line != "250 OK" {
		t.Fatalf("ReadLine=%q err=%v", line, err)
	}
	if _, err := sc.ReadLine(); err == nil {
		t.Error("ReadLine after close should error")
	}
	sc.close()
}
