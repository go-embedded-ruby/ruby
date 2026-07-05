package webapp

import "testing"

// sinatraGem421Golden is the response dump of apps/sinatra_oracle.rb captured
// from the REAL `sinatra` gem 4.2.1 (MRI ruby 4.0.5): seven requests through the
// classic Sinatra::Base #call(env) adapter, dumping [status, headers, body] with
// the two known library-level headers excluded (Content-Length, X-Cascade — see
// the fixture header comment). This is the golden oracle: rbgo must reproduce it
// byte-for-byte. It is a captured constant so the suite stays hermetic (no ruby,
// no gem, runs under qemu on every 64-bit target); regenerate by running the
// fixture under MRI + the sinatra gem, per apps/sinatra_oracle.rb.
const sinatraGem421Golden = `== req 0 ==
status=200
H content-type: application/json
H x-after: seen
body={"hello":"amy","uid":"u42","q":"1"}
== req 1 ==
status=200
H content-type: text/html;charset=utf-8
H x-after: seen
body=splat=a/b/c.txt
== req 2 ==
status=200
H content-type: text/html;charset=utf-8
H x-after: seen
body=a=x b=nil
== req 3 ==
status=303
H content-type: text/html;charset=utf-8
H location: http://ex/greet/there
H x-after: seen
body=
== req 4 ==
status=418
H content-type: text/html;charset=utf-8
H x-after: seen
body=teapot
== req 5 ==
status=500
H content-type: text/html;charset=utf-8
H x-after: seen
body=custom-error
== req 6 ==
status=404
H content-type: text/html;charset=utf-8
H x-after: seen
body=no-such-route
`

// TestSinatraGemOracle proves the go-ruby-sinatra binding is MRI-identical
// *through the rbgo binary*: it runs the same apps/sinatra_oracle.rb the sinatra
// gem 4.2.1 ran and asserts the response is byte-for-byte the captured gem
// output. This is the run-conformance headline for the web phase — routing, a
// :name capture, a query param, before-filter instance-variable persistence into
// the route body (before/route/after share one instance), a splat, an optional
// param, redirect + Location, halt, a custom error(code) handler, a custom
// not_found handler and an after-filter mutating the response — all answered by
// rbgo exactly as by the reference gem.
func TestSinatraGemOracle(t *testing.T) {
	if ok, detail := featureLoadable("sinatra/base"); !ok {
		t.Fatalf("sinatra/base must be loadable for the oracle: %s", detail)
	}
	out := mustRun(t, "sinatra_oracle.rb")
	if out != sinatraGem421Golden {
		t.Fatalf("rbgo output is not MRI-identical to sinatra gem 4.2.1\n got:\n%s\nwant:\n%s", out, sinatraGem421Golden)
	}
}
