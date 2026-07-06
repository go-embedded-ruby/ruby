package webapp

import "testing"

// sinatraErbGem421Golden is the response dump of apps/sinatra_erb_oracle.rb
// captured from the REAL `sinatra` gem 4.2.1 (MRI ruby 4.0.5) with its ERB view
// engine — which is erubi (Tilt::ErubiTemplate), the erubi gem 1.13.1: fourteen
// requests through the classic Sinatra::Base #call(env) adapter, each rendering an
// ERB view (six inline/symbol templates, then eight exercising the layout rule,
// the last a standalone `<%= yield %>` line whose trailing newline erubi keeps but
// classic ERB trim "<>" drops) and dumping [status, body]. This is the golden oracle for the
// templating half of the Sinatra binding: rbgo, rendering through the bound
// go-ruby-erb compiler, must reproduce it byte-for-byte. It is a captured constant
// so the suite stays hermetic (no ruby, no gem, runs under qemu on every 64-bit
// target); regenerate by running the fixture under MRI + the sinatra gem, per
// apps/sinatra_erb_oracle.rb.
const sinatraErbGem421Golden = `== req 0 ==
status=200
body<<<h1>Hello amy (3 visits)</h1>>>
== req 1 ==
status=200
body<<<ul><li>a</li><li>b</li><li>c</li></ul>>>
== req 2 ==
status=200
body<<<p>Hi, bob!</p>>>
== req 3 ==
status=200
body<<<p>eve</p>>>
== req 4 ==
status=200
body<<line1
kept
line2>>
== req 5 ==
status=200
body<<<h2>Hey World</h2>
<ul>
  <li>x</li>
  <li>y</li>
</ul>
>>
== req 6 ==
status=200
body<<<!DOCTYPE html>
<title>Site</title>
<main><h2>Hey World</h2>
<ul>
  <li>x</li>
  <li>y</li>
</ul>
</main>
<footer>by World</footer>
>>
== req 7 ==
status=200
body<<<h2>Hey World</h2>
<ul>
  <li>x</li>
  <li>y</li>
</ul>
>>
== req 8 ==
status=200
body<<<section class="box"><h2>Hi World</h2>
<ul>
  <li>a</li>
</ul>
</section>
>>
== req 9 ==
status=200
body<<<!DOCTYPE html>
<title>Site</title>
<main><h2>Yo World</h2>
<ul>
  <li>q</li>
</ul>
</main>
<footer>by World</footer>
>>
== req 10 ==
status=200
body<<<wrap><h2>In World</h2>
<ul>
  <li>z</li>
</ul>
</wrap>
>>
== req 11 ==
status=200
body<<Errno::ENOENT>>
== req 12 ==
status=200
body<<Errno::ENOENT>>
== req 13 ==
status=200
body<<<header>H</header>
<h2>So World</h2>
<ul>
  <li>m</li>
</ul>

<footer>F</footer>
>>
`

// TestSinatraErbGemOracle proves the Sinatra `erb` view helper is MRI-identical
// *through the rbgo binary*: it runs the same apps/sinatra_erb_oracle.rb the
// sinatra gem 4.2.1 ran and asserts the rendered responses are byte-for-byte the
// captured gem output. This is the run-conformance headline for the templating
// half of the web phase — inline-String and :symbol/file templates, <%= %>
// interpolation of a query param and an @ivar set in a before-filter, <% %>
// control flow, positional and options[:locals] locals, ERB trim behaviour, and
// the layout rule (default views/layout.erb with `<%= yield %>` returning the
// view, layout: false, a custom layout: :name, layout: true, an inline-String
// layout, a layout with a standalone `<%= yield %>` line — the erubi
// newline-fidelity case — and the Errno::ENOENT a missing named layout / view
// raises) — all rendered by rbgo (via the bound go-ruby-erb compiler in erubi
// mode, evaluated in the handler's binding) exactly as by the reference gem.
func TestSinatraErbGemOracle(t *testing.T) {
	if ok, detail := featureLoadable("sinatra/base"); !ok {
		t.Fatalf("sinatra/base must be loadable for the ERB oracle: %s", detail)
	}
	out := mustRun(t, "sinatra_erb_oracle.rb")
	if out != sinatraErbGem421Golden {
		t.Fatalf("rbgo output is not MRI-identical to sinatra gem 4.2.1 (erb)\n got:\n%s\nwant:\n%s", out, sinatraErbGem421Golden)
	}
}
