// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"
	"fmt"
	"strings"

	nethttp "github.com/go-ruby-net-http/net-http"
	"github.com/go-ruby-webmock/webmock"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the host half of the WebMock binding: the transport-level
// interception hook the bound Net::HTTP calls before it opens a socket, plus the
// value model wrapping a webmock.Stub as a Ruby object. The deterministic engine
// (request matching, stub sequencing, request history, the net-connect policy) is
// github.com/go-ruby-webmock/webmock, driven through the process-wide
// webmock.Default registry — the Go analogue of Ruby's global WebMock module. The
// Ruby surface (WebMock.stub_request / disable_net_connect! / assert_requested,
// the stub builder methods) lives in webmock_bind.go.

// WebMockStub is the Ruby object returned by WebMock.stub_request: a thin wrapper
// over a registered *webmock.Stub so the fluent builder (.with / .to_return /
// .to_raise / .to_timeout) can keep appending constraints and behaviours.
type WebMockStub struct {
	stub *webmock.Stub
	cls  *RClass
}

func (s *WebMockStub) ToS() string     { return "#<WebMock::RequestStub>" }
func (s *WebMockStub) Inspect() string { return s.ToS() }
func (s *WebMockStub) Truthy() bool    { return true }

// webmockRubyError is the error a to_raise stub carries: the Ruby exception class
// and message to re-raise when the stub fires. It travels through the engine as a
// plain Go error (wrapped in a *webmock.RaiseError) and is decoded back into a VM
// raise by webmockIntercept.
type webmockRubyError struct {
	class   string
	message string
}

func (e *webmockRubyError) Error() string { return e.class + ": " + e.message }

// webmockIntercept offers one outgoing request to the stub registry and, when it
// owns the outcome, returns the Ruby result and handled=true. It returns
// handled=false only when webmock has been told to allow this (unstubbed) request
// through to the real transport (WebMock.allow_net_connect!), in which case the
// caller performs the real request. A matched stub is answered entirely
// in-process — no socket is opened:
//
//   - a StubResponse becomes a Net::HTTPResponse (via the same codec path a real
//     response takes, so the subclass — Net::HTTPOK etc. — is identical);
//   - a to_timeout stub raises Net::OpenTimeout (MRI's Timeout on connect);
//   - a to_raise stub raises the class it was given;
//   - an unregistered request (net connections disabled) raises
//     WebMock::NetConnectNotAllowedError carrying webmock's request diff.
//
// Every matched request is recorded by the registry for assert_requested.
func (vm *VM) webmockIntercept(cfg *nethttpXfer, method, path string, body []byte, hdr [][2]string) (object.Value, bool) {
	req := webmock.Request{
		Method:  method,
		URI:     cfg.scheme + "://" + cfg.hostHdr + path,
		Headers: webmockHeaderMap(hdr),
		Body:    string(body),
	}
	resp, err := webmock.Match(req)
	if err == nil {
		return vm.webmockBuildResponse(resp), true
	}
	if errors.Is(err, webmock.ErrNetConnectAllowed) {
		return nil, false // fall through to the real transport
	}
	if errors.Is(err, webmock.ErrTimeout) {
		return raise("Net::OpenTimeout", "execution expired"), true
	}
	var re *webmock.RaiseError
	if errors.As(err, &re) {
		we := re.Err.(*webmockRubyError)
		return raise(we.class, "%s", we.message), true
	}
	// The only remaining outcome from Registry.Match is a *NoStubError diff.
	return raise("WebMock::NetConnectNotAllowedError", "%s", err.Error()), true
}

// webmockHeaderMap converts the codec's request header pairs into the
// case-insensitive multi-value map the registry matches against.
func webmockHeaderMap(hdr [][2]string) map[string][]string {
	if len(hdr) == 0 {
		return nil
	}
	m := make(map[string][]string, len(hdr))
	for _, kv := range hdr {
		m[kv[0]] = append(m[kv[0]], kv[1])
	}
	return m
}

// webmockBuildResponse turns a stubbed StubResponse into a Ruby Net::HTTPResponse.
// It synthesises the response's wire bytes and runs them back through the net-http
// codec + nethttpBuildResponse, so the resulting object is byte-for-byte the same
// shape (status subclass, downcased headers, framed body) a real response would
// have produced — the interception is invisible to the Ruby caller.
func (vm *VM) webmockBuildResponse(sr webmock.StubResponse) object.Value {
	var b strings.Builder
	fmt.Fprintf(&b, "HTTP/1.1 %d\r\n", sr.Status)
	hasLen := false
	for k, vs := range sr.Headers {
		if strings.EqualFold(k, "Content-Length") {
			hasLen = true
		}
		for _, v := range vs {
			fmt.Fprintf(&b, "%s: %s\r\n", k, v)
		}
	}
	if !hasLen {
		fmt.Fprintf(&b, "Content-Length: %d\r\n", len(sr.Body))
	}
	b.WriteString("\r\n")
	b.WriteString(sr.Body)
	parsed, err := nethttp.ParseResponse([]byte(b.String()))
	if err != nil {
		return raise("Net::HTTPBadResponse", "%s", err.Error())
	}
	return vm.nethttpBuildResponse(parsed)
}
