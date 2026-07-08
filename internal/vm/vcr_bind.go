// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	nethttp "github.com/go-ruby-net-http/net-http"
	vcr "github.com/go-ruby-vcr/vcr"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the seam between the bound Net::HTTP transport (nethttp_bind.go)
// and the VCR cassette store (vcr.go). nethttpDoXfer calls vcrRoute whenever a
// VCR.use_cassette block is active (vm.vcrCassette != nil):
//
//	vcrRoute builds a vcr.Request from the resolved transfer (method + full URI +
//	body + headers) and hands it, with a "doer", to Cassette.Interact:
//	  - on replay the cassette matches the request against a recorded interaction
//	    and returns its response WITHOUT calling the doer (no network);
//	  - on record the cassette calls the doer, which performs the REAL request
//	    through rbgo's Net::HTTP transport (nethttpRawExchange), and stores the
//	    result. The doer is the HTTP seam the library documents.
//
// Either way Interact yields a vcr.Response, which vcrResponseToRuby turns back
// into the same Net::HTTPResponse object the direct path would have produced (by
// re-serialising it to raw HTTP/1.1 bytes and running it through the existing
// ParseResponse + nethttpBuildResponse), so a caller cannot tell a replayed
// response from a live one.

// vcrRoute records or replays one Net::HTTP request through the active cassette
// and returns the resulting Net::HTTPResponse. The doer performs the real request
// (only invoked when the cassette must record); vm.vcrTestDoer overrides it with a
// canned response so the record/replay machine is drivable in-process.
func (vm *VM) vcrRoute(cfg *nethttpXfer, method, path string, body []byte, hdr [][2]string) object.Value {
	req := vcr.Request{
		Method:  method,
		URI:     cfg.scheme + "://" + cfg.hostHdr + path,
		Body:    vcr.Body{String: string(body)},
		Headers: pairsToVCRHeaders(hdr),
	}
	doer := vm.vcrTestDoer
	if doer == nil {
		doer = func(_ vcr.Request) (vcr.Response, error) {
			raw, noBody := vm.nethttpRawExchange(cfg, method, path, body, hdr)
			resp, err := nethttp.ParseResponse(raw)
			if err != nil {
				raise("Net::HTTPBadResponse", "%s", err.Error())
			}
			return nethttpToVCRResponse(resp, noBody), nil
		}
	}
	resp, err := vm.vcrCassette.Interact(req, doer)
	if err != nil {
		// An unmatched request under a non-recording mode (:none) surfaces as the
		// library's *UnhandledRequestError; raise the Ruby error a caller rescues.
		raise("VCR::Errors::UnhandledHTTPRequestError", "%s", err.Error())
	}
	return vm.vcrResponseToRuby(resp)
}

// nethttpToVCRResponse converts a parsed *nethttp.Response into the library's
// vcr.Response for recording: the status code + reason, the (downcased,
// per-field-joined) headers, the decoded body and the HTTP version. A no-body
// response (HEAD etc.) records an empty body.
func nethttpToVCRResponse(resp *nethttp.Response, noBody bool) vcr.Response {
	code, _ := strconv.Atoi(resp.Code())
	headers := map[string][]string{}
	resp.EachHeader(func(k, v string) {
		headers[k] = append(headers[k], v)
	})
	var bodyStr string
	if !noBody {
		bodyStr = string(resp.Body())
	}
	return vcr.Response{
		Status:      vcr.Status{Code: code, Message: resp.Message()},
		Headers:     headers,
		Body:        vcr.Body{String: bodyStr},
		HTTPVersion: resp.HTTPVersion(),
	}
}

// vcrResponseToRuby turns a vcr.Response (replayed from a cassette or freshly
// recorded) back into the Net::HTTPResponse the direct transport would have built,
// by re-serialising it to a raw HTTP/1.1 response and running it through the same
// ParseResponse + nethttpBuildResponse the live path uses.
func (vm *VM) vcrResponseToRuby(resp vcr.Response) object.Value {
	return vm.nethttpResponseFromRaw(vcrResponseRaw(resp), false)
}

// vcrResponseRaw serialises a vcr.Response to a raw HTTP/1.1 response byte stream.
// The recorded body is emitted verbatim under a freshly-computed Content-Length
// (the original framing headers — Content-Length / Transfer-Encoding — are dropped
// so ParseResponse frames exactly the stored bytes, byte-for-byte, regardless of
// how the response was originally chunked). Remaining headers are emitted in sorted
// order for a deterministic reconstruction.
func vcrResponseRaw(resp vcr.Response) []byte {
	version := resp.HTTPVersion
	if version == "" {
		version = "1.1"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "HTTP/%s %d %s\r\n", version, resp.Status.Code, resp.Status.Message)

	keys := make([]string, 0, len(resp.Headers))
	for k := range resp.Headers {
		switch strings.ToLower(k) {
		case "content-length", "transfer-encoding":
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		for _, v := range resp.Headers[k] {
			fmt.Fprintf(&b, "%s: %s\r\n", k, v)
		}
	}
	fmt.Fprintf(&b, "Content-Length: %d\r\n", len(resp.Body.String))
	b.WriteString("\r\n")
	b.WriteString(resp.Body.String)
	return []byte(b.String())
}

// pairsToVCRHeaders converts the ordered request-header pairs the codec uses into
// the vcr.Request header map (name → ordered values), preserving repeated headers.
func pairsToVCRHeaders(hdr [][2]string) map[string][]string {
	if len(hdr) == 0 {
		return nil
	}
	out := make(map[string][]string, len(hdr))
	for _, p := range hdr {
		out[p[0]] = append(out[p[0]], p[1])
	}
	return out
}
