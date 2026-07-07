// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	vcr "github.com/go-ruby-vcr/vcr"

	"github.com/go-embedded-ruby/ruby/internal/compiler"
	"github.com/go-embedded-ruby/ruby/internal/object"
	"github.com/go-ruby-parser/parser"
)

// memVCRFS is an in-memory vcr.FS so cassette record/replay stays hermetic (no
// disk). ReadFile returns fs.ErrNotExist for an absent cassette, which is exactly
// what the library treats as "record a new cassette".
type memVCRFS struct{ files map[string][]byte }

func newMemVCRFS() *memVCRFS { return &memVCRFS{files: map[string][]byte{}} }

func (m *memVCRFS) fs() vcr.FS {
	return vcr.FS{
		ReadFile: func(name string) ([]byte, error) {
			b, ok := m.files[name]
			if !ok {
				return nil, fs.ErrNotExist
			}
			return append([]byte(nil), b...), nil
		},
		WriteFile: func(name string, data []byte, _ os.FileMode) error {
			m.files[name] = append([]byte(nil), data...)
			return nil
		},
		MkdirAll: func(string, os.FileMode) error { return nil },
	}
}

// vcrExec compiles and runs src through vm, returning trimmed stdout. The buffer
// is reset first so successive runs on one VM read cleanly.
func vcrExec(t *testing.T, vm *VM, buf *bytes.Buffer, src string) string {
	t.Helper()
	buf.Reset()
	full := "require \"vcr\"\nrequire \"net/http\"\nrequire \"uri\"\n" + src
	prog, err := parser.Parse(full)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	iseq, err := compiler.Compile(prog)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if _, err := vm.Run(iseq); err != nil {
		t.Fatalf("run: %v\nsrc=%s", err, src)
	}
	return strings.TrimRight(buf.String(), "\n")
}

// TestVCRRecordReplayRealTransport is the headline proof: with an in-memory FS, a
// VCR.use_cassette block records real Net::HTTP requests against a loopback server
// (GET, a chunked GET, and a POST with a header+body), then a SECOND VM replays the
// same requests from the recorded cassette with the server SHUT DOWN — so a
// byte-faithful body coming back proves the replay used the cassette, not the
// network. This drives the real-transport record doer + every value conversion.
func TestVCRRecordReplayRealTransport(t *testing.T) {
	srv := httptest.NewServer(nethttpTestMux())
	base := srv.URL
	mem := newMemVCRFS()

	script := `
VCR.configure { |c| c.cassette_library_dir = "cass"; c.default_record_mode = :once }
VCR.use_cassette("api") do
  puts Net::HTTP.get_response(URI(BASE + "/")).body
  puts Net::HTTP.get_response(URI(BASE + "/chunked")).body
  r = Net::HTTP.post(URI(BASE + "/echo"), "payload", {"X-Probe" => "p"})
  puts r.code
  puts r.body
end`

	// Record.
	vm1, buf1 := newVCRTestVM(mem)
	rec := vcrExec(t, vm1, buf1, "BASE="+fmt.Sprintf("%q", base)+"\n"+script)
	wantRec := "hello net/http\nchunk-body\n200\nPOST|/echo|p|payload"
	if rec != wantRec {
		t.Fatalf("record output:\n got=%q\nwant=%q", rec, wantRec)
	}
	if len(mem.files) != 1 {
		t.Fatalf("expected one cassette written, got %d: %v keys", len(mem.files), mem.files)
	}

	// Replay from the cassette with the server closed: no network is available.
	srv.Close()
	vm2, buf2 := newVCRTestVM(mem)
	rep := vcrExec(t, vm2, buf2, "BASE="+fmt.Sprintf("%q", base)+"\n"+script)
	if rep != wantRec {
		t.Fatalf("replay output (server closed):\n got=%q\nwant=%q", rep, wantRec)
	}
}

// newVCRTestVM builds a fully-bootstrapped VM with an in-memory VCR filesystem.
func newVCRTestVM(mem *memVCRFS) (*VM, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	vm := New(buf)
	vm.vcrConfig.FS = mem.fs()
	return vm, buf
}

// TestVCRStubDoerRecordReplay drives the record/replay state machine entirely
// in-process with a stub doer (a canned response) and an in-memory FS: it records
// a cassette, replays it asserting byte-faithful playback (the doer must NOT run on
// replay), and asserts a :none record mode errors on an unknown request.
func TestVCRStubDoerRecordReplay(t *testing.T) {
	mem := newMemVCRFS()
	vm, buf := newVCRTestVM(mem)

	canned := vcr.Response{
		Status:      vcr.Status{Code: 201, Message: "Created"},
		Headers:     map[string][]string{"content-type": {"application/json"}},
		Body:        vcr.Body{String: `{"ok":true}`},
		HTTPVersion: "1.1",
	}

	// Record: the stub doer supplies the response.
	calls := 0
	vm.vcrTestDoer = func(vcr.Request) (vcr.Response, error) {
		calls++
		return canned, nil
	}
	out := vcrExec(t, vm, buf, `
VCR.configure { |c| c.cassette_library_dir = "cass"; c.default_record_mode = :once }
VCR.use_cassette("stub") do
  r = Net::HTTP.get_response(URI("http://example.test/hi"))
  puts r.code
  puts r.body
  puts r["content-type"]
end`)
	if out != "201\n{\"ok\":true}\napplication/json" {
		t.Fatalf("record via stub doer:\n got=%q", out)
	}
	if calls != 1 {
		t.Fatalf("record: expected doer called once, got %d", calls)
	}

	// Replay: the doer must not run; the cassette supplies the byte-faithful body.
	vm2, buf2 := newVCRTestVM(mem)
	vm2.vcrTestDoer = func(vcr.Request) (vcr.Response, error) {
		t.Fatalf("doer must not be called on replay")
		return vcr.Response{}, nil
	}
	rep := vcrExec(t, vm2, buf2, `
VCR.configure { |c| c.cassette_library_dir = "cass" }
VCR.use_cassette("stub") do
  r = Net::HTTP.get_response(URI("http://example.test/hi"))
  puts r.code
  puts r.body
end`)
	if rep != "201\n{\"ok\":true}" {
		t.Fatalf("replay from cassette:\n got=%q", rep)
	}

	// :none on an unknown request raises VCR::Errors::UnhandledHTTPRequestError.
	vm3, buf3 := newVCRTestVM(mem)
	unh := vcrExec(t, vm3, buf3, `
VCR.configure { |c| c.cassette_library_dir = "cass" }
begin
  VCR.use_cassette("stub", record: :none) do
    Net::HTTP.get_response(URI("http://example.test/unknown"))
  end
rescue VCR::Errors::UnhandledHTTPRequestError => e
  puts "unhandled:" + (e.message.length > 0 ? "yes" : "no")
end`)
	if unh != "unhandled:yes" {
		t.Fatalf(":none unknown request:\n got=%q", unh)
	}
}

// TestVCRConfigurationSurface exercises the configuration accessors, the option
// parsing (record:/allow_playback_repeats:, a non-Hash options argument), the
// record-mode parsing (Symbol, String, invalid-name and non-symbol errors) and the
// use_cassette arity/no-block guards.
func TestVCRConfigurationSurface(t *testing.T) {
	mem := newMemVCRFS()
	vm, buf := newVCRTestVM(mem)
	vm.vcrTestDoer = func(vcr.Request) (vcr.Response, error) {
		return vcr.Response{Status: vcr.Status{Code: 200, Message: "OK"}, Body: vcr.Body{String: "x"}, HTTPVersion: "1.1"}, nil
	}

	out := vcrExec(t, vm, buf, `
VCR.configure do |c|
  c.cassette_library_dir = "cass"
  c.default_record_mode = :new_episodes
  c.allow_playback_repeats = true
  puts c.cassette_library_dir
  puts c.default_record_mode
  puts c.allow_playback_repeats
end
c = VCR.configure                       # no-block form returns the configuration
c.default_record_mode = "all"           # String mode
puts c.default_record_mode
VCR.use_cassette("s", record: :once, allow_playback_repeats: false) { Net::HTTP.get_response(URI("http://a.test/")) }
VCR.use_cassette("s2", 5) { Net::HTTP.get_response(URI("http://a.test/")) }  # non-Hash options ignored
puts "ok"`)
	if out != "cass\nnew_episodes\ntrue\nall\nok" {
		t.Fatalf("configuration surface:\n got=%q", out)
	}

	// Error arms: invalid mode name, non-symbol mode, use_cassette arity + no-block.
	cases := []struct{ src, want string }{
		{`begin; VCR.configure { |c| c.default_record_mode = :bogus }; rescue ArgumentError; puts "badmode"; end`, "badmode"},
		{`begin; VCR.configure { |c| c.default_record_mode = 5 }; rescue ArgumentError; puts "notsym"; end`, "notsym"},
		{`begin; VCR.use_cassette { 1 }; rescue ArgumentError; puts "arity"; end`, "arity"},
		{`begin; VCR.use_cassette("x"); rescue ArgumentError; puts "noblock"; end`, "noblock"},
	}
	for _, c := range cases {
		vmc, bufc := newVCRTestVM(mem)
		if got := vcrExec(t, vmc, bufc, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}

	// A corrupt cassette on disk fails to load: UseCassette returns an error which
	// vcrUseCassette surfaces as VCR::Errors::Error.
	corrupt := newMemVCRFS()
	corrupt.files["cass/corrupt.yml"] = []byte("- not a mapping\n")
	vmb, bufb := newVCRTestVM(corrupt)
	got := vcrExec(t, vmb, bufb, `
VCR.configure { |c| c.cassette_library_dir = "cass" }
begin
  VCR.use_cassette("corrupt") { 1 }
rescue VCR::Errors::Error
  puts "loadfail"
end`)
	if got != "loadfail" {
		t.Fatalf("corrupt cassette:\n got=%q", got)
	}
}

// TestVCRBadResponseUnderCassette covers the record doer's parse-error arm: a
// non-HTTP reply from the real transport, recorded through a cassette, surfaces as
// Net::HTTPBadResponse just as on the direct path.
func TestVCRBadResponseUnderCassette(t *testing.T) {
	bad, _ := net.Listen("tcp", "127.0.0.1:0")
	defer bad.Close()
	go func() {
		for {
			c, err := bad.Accept()
			if err != nil {
				return
			}
			c.Read(make([]byte, 1024))
			io.WriteString(c, "this is not a valid HTTP response\r\n\r\n")
			c.Close()
		}
	}()

	mem := newMemVCRFS()
	vm, buf := newVCRTestVM(mem)
	src := fmt.Sprintf(`
VCR.configure { |c| c.cassette_library_dir = "cass" }
begin
  VCR.use_cassette("bad") { Net::HTTP.get_response(URI("http://%s/")) }
rescue Net::HTTPBadResponse
  puts "badresp"
end`, bad.Addr().String())
	if got := vcrExec(t, vm, buf, src); got != "badresp" {
		t.Fatalf("bad response under cassette:\n got=%q", got)
	}
}

// TestVCRConversionHelpers pins the pure conversion helpers directly, including a
// response carrying chunked framing (whose Transfer-Encoding header is dropped and
// re-framed under a fresh Content-Length so the reconstructed body is byte-exact)
// and multi-valued request headers.
func TestVCRConversionHelpers(t *testing.T) {
	// pairsToVCRHeaders: empty → nil; repeated names preserved in order.
	if got := pairsToVCRHeaders(nil); got != nil {
		t.Errorf("pairsToVCRHeaders(nil) = %v, want nil", got)
	}
	h := pairsToVCRHeaders([][2]string{{"accept", "a"}, {"accept", "b"}})
	if len(h["accept"]) != 2 || h["accept"][0] != "a" || h["accept"][1] != "b" {
		t.Errorf("pairsToVCRHeaders repeated = %v", h)
	}

	// vcrResponseRaw drops framing headers and re-frames the body verbatim.
	resp := vcr.Response{
		Status:      vcr.Status{Code: 200, Message: "OK"},
		Headers:     map[string][]string{"content-type": {"text/plain"}, "transfer-encoding": {"chunked"}, "content-length": {"999"}},
		Body:        vcr.Body{String: "hello"},
		HTTPVersion: "",
	}
	raw := string(vcrResponseRaw(resp))
	if !strings.HasPrefix(raw, "HTTP/1.1 200 OK\r\n") {
		t.Errorf("status line: %q", raw)
	}
	if strings.Contains(raw, "Transfer-Encoding") || strings.Contains(strings.ToLower(raw), "999") {
		t.Errorf("framing headers not dropped: %q", raw)
	}
	if !strings.Contains(raw, "content-type: text/plain\r\n") || !strings.HasSuffix(raw, "Content-Length: 5\r\n\r\nhello") {
		t.Errorf("reconstructed raw = %q", raw)
	}

	// vcrResponseToRuby round-trips through ParseResponse + nethttpBuildResponse.
	vm := New(&bytes.Buffer{})
	ruby := vm.vcrResponseToRuby(resp)
	if body := getIvar(ruby, "@body"); body.(*object.String).Str() != "hello" {
		t.Errorf("vcrResponseToRuby body = %q", body.ToS())
	}
}
