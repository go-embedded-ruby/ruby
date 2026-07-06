// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package webapp

import "testing"

// sinatraSessionGolden is the transcript of apps/stage5_sinatra_session.rb: a
// Sinatra app with `enable :sessions` driving its own request sequence and
// threading each Set-Cookie back in as the next request's Cookie. Unlike the
// stage-3 oracle, this is a FUNCTIONAL golden, not a byte-capture of the sinatra
// gem's cookie: the cookie bytes are an internal detail (and depend on the Rack
// version's coder + the signing secret), but the *observable* behaviour — the
// session counter incrementing across requests, persisting across a different
// route, resetting when the signed cookie is tampered, and clearing on
// session.clear — is identical under MRI + the sinatra gem. The serialization's
// byte-parity with Rack::Session::Cookie (base64(Marshal) + HMAC-SHA1) is proven
// separately in the internal suite (TestSinatraSessionCookieByteExact), where the
// secret is fixed.
const sinatraSessionGolden = `r1 body=count=1
r1 set_cookie=true
r2 body=count=2
r3 body=count=2
r4 body=count=1
r5 body=count=3
r6 body=count=nil
`

// TestSinatraSessionStore proves `enable :sessions` end-to-end THROUGH the rbgo
// binary: two sequential requests thread the Set-Cookie -> Cookie and the
// counter increments (round-trip), a third route reads the same value
// (cross-route persistence), a tampered cookie is rejected so the counter
// restarts (signature verification), and session.clear empties the store. This
// is the run-conformance headline for the session seam PR #157 deferred.
func TestSinatraSessionStore(t *testing.T) {
	if ok, detail := featureLoadable("sinatra/base"); !ok {
		t.Fatalf("sinatra/base must be loadable for the session oracle: %s", detail)
	}
	out := mustRun(t, "stage5_sinatra_session.rb")
	if out != sinatraSessionGolden {
		t.Fatalf("rbgo session transcript mismatch\n got:\n%s\nwant:\n%s", out, sinatraSessionGolden)
	}
}
