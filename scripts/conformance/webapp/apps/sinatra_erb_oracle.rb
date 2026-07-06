# frozen_string_literal: true
#
# Sinatra + ERB oracle app — the reference app whose response is diffed
# byte-for-byte against the real `sinatra` gem 4.2.1 (+ its ERB view engine). It
# exercises the templating half of the Sinatra binding the go-ruby-erb-backed
# `erb` helper re-exposes: <%= %> interpolation of a request param, an @ivar set
# in a before-filter (real Sinatra runs before/route against ONE instance, so it
# is visible to the view), <% %> control flow, `locals:` passed both positionally
# and inside the options Hash, ERB trim behaviour, a :symbol template read from
# the app's :views directory, and — via a second app whose :views dir also holds
# a layout.erb — Sinatra's layout rule: the default layout wrapping a view with
# `<%= yield %>` returning the view's String, `layout: false` (no layout), a
# custom `layout: :name`, `layout: true`, an inline-String layout, and the
# Errno::ENOENT a missing named layout / view raises (rescued to its class name so
# the byte-exact diff does not depend on Sinatra's environment-specific error page).
#
# The identical file runs unchanged under MRI + the sinatra gem; the expected
# output in sinatra_erb_oracle_test.go was captured from that run (gem 4.2.1,
# ruby 4.0.5). To regenerate the golden vector, run this file with `ruby` and the
# sinatra gem installed.
#
# Dialect note: Sinatra 4.x maps :erb to Tilt::ErubiTemplate (erubi), and rbgo now
# renders Sinatra views through go-ruby-erb's ModeErubi, which reproduces
# Erubi::Engine byte-for-byte. So this fixture exercises erubi's own whitespace
# semantics directly — including a standalone `<%= yield %>` line (route
# /l_standalone) whose trailing newline erubi keeps but classic ERB trim "<>" would
# drop. That case was previously kept inline to dodge the classic-ERB divergence;
# it is now asserted end-to-end, matching the real gem.

require "sinatra/base"
require "tmpdir"

# The :symbol view is written to a temp dir the app owns, so the whole proof stays
# hermetic (no repo file, no working-directory dependence) — it runs unchanged
# under the coverage job, on Windows and under qemu on every 64-bit target.
VIEWS = Dir.mktmpdir("erb-oracle-views")
File.write(
  File.join(VIEWS, "card.erb"),
  "<h2><%= greeting %> <%= @who %></h2>\n<ul>\n<% items.each do |i| %>\n  <li><%= i %></li>\n<% end %>\n</ul>\n"
)

class App < Sinatra::Base
  disable :protection
  set :host_authorization, { permitted_hosts: [] }
  set :views, VIEWS

  before { @n = 3; @who = "World" }

  # The headline: <%= %> interpolation of a query param and an @ivar the action
  # (before-filter) set, rendered from an inline String template.
  get "/inline" do
    erb "<h1>Hello <%= params['name'] %> (<%= @n %> visits)</h1>"
  end

  # <% %> control flow over a local collection, inline (single line — trim-neutral).
  get "/loop" do
    erb "<ul><% %w[a b c].each do |x| %><li><%= x %></li><% end %></ul>"
  end

  # locals passed positionally (the 3rd argument).
  get "/locals" do
    erb "<p><%= greeting %>, <%= who %>!</p>", {}, greeting: "Hi", who: params['name']
  end

  # locals passed inside the options Hash (Sinatra also accepts options[:locals]).
  get "/opts_locals" do
    erb "<p><%= who %></p>", locals: { who: params['name'] }
  end

  # Trim behaviour: code-only lines standing alone have their surrounding newlines
  # removed (ERB "<>" and erubi agree here).
  get "/trim" do
    erb "line1\n<% if true %>\nkept\n<% end %>\nline2"
  end

  # The :symbol path — render views/card.erb, reading the @ivar and positional
  # locals, from the app's :views directory.
  get "/view" do
    erb :card, {}, greeting: "Hey", items: %w[x y]
  end
end

# A second views dir that DOES hold a layout.erb (plus a custom alt.erb), so the
# second app exercises the layout rule. Keeping it separate from VIEWS leaves the
# first app's :symbol view (/view) un-wrapped, proving the no-layout path too.
LVIEWS = Dir.mktmpdir("erb-oracle-lviews")
File.write(
  File.join(LVIEWS, "card.erb"),
  "<h2><%= greeting %> <%= @who %></h2>\n<ul>\n<% items.each do |i| %>\n  <li><%= i %></li>\n<% end %>\n</ul>\n"
)
# The default layout: surrounding markup with `<%= yield %>` interpolating the
# wrapped view and an @ivar the before-filter set (the layout renders in the same
# handler binding as the view). Here `yield` is kept inline within <main>…</main>.
File.write(
  File.join(LVIEWS, "layout.erb"),
  "<!DOCTYPE html>\n<title>Site</title>\n<main><%= yield %></main>\n<footer>by <%= @who %></footer>\n"
)
# A layout with `<%= yield %>` STANDALONE on its own line — the erubi-specific case
# the classic-ERB oracle used to avoid. Under erubi the yield line keeps its
# trailing newline, so a blank-looking break appears between the wrapped view and
# the footer; classic ERB trim "<>" would have swallowed that newline. Asserting it
# byte-for-byte proves rbgo renders Sinatra views in erubi mode.
File.write(
  File.join(LVIEWS, "standalone.erb"),
  "<header>H</header>\n<%= yield %>\n<footer>F</footer>\n"
)
# A custom layout selected with `layout: :alt`, using a render local (proving the
# locals reach the layout, not just the view) around `<%= yield %>`.
File.write(
  File.join(LVIEWS, "alt.erb"),
  "<section class=\"<%= css %>\"><%= yield %></section>\n"
)

class LApp < Sinatra::Base
  disable :protection
  set :host_authorization, { permitted_hosts: [] }
  set :views, LVIEWS

  before { @who = "World" }

  # Default layout: `erb :card` renders views/card.erb and wraps it in
  # views/layout.erb, whose `<%= yield %>` interpolates the view's String.
  get "/l_default" do
    erb :card, {}, greeting: "Hey", items: %w[x y]
  end

  # `layout: false` — render the view with no layout.
  get "/l_none" do
    erb :card, { layout: false }, greeting: "Hey", items: %w[x y]
  end

  # `layout: :alt` — a custom layout file; the render locals (css) reach it.
  get "/l_custom" do
    erb :card, { layout: :alt }, greeting: "Hi", items: %w[a], css: "box"
  end

  # `layout: true` — the default layout, explicitly requested.
  get "/l_true" do
    erb :card, { layout: true }, greeting: "Yo", items: %w[q]
  end

  # An inline-String layout, whose `<%= yield %>` wraps the view.
  get "/l_inline" do
    erb :card, { layout: "<wrap><%= yield %></wrap>\n" }, greeting: "In", items: %w[z]
  end

  # A layout whose `<%= yield %>` stands ALONE on its own line: erubi keeps that
  # line's trailing newline (classic ERB trim "<>" would drop it), so a bare
  # newline separates the wrapped view from the footer. The newline-fidelity proof.
  get "/l_standalone" do
    erb :card, { layout: :standalone }, greeting: "So", items: %w[m]
  end

  # A missing NAMED layout raises Errno::ENOENT (MRI eats the error only for the
  # implicit default layout, not an explicitly requested one).
  get "/l_missing_layout" do
    begin
      erb :card, { layout: :nope }, greeting: "X", items: []
    rescue => e
      e.class.to_s
    end
  end

  # A missing view raises Errno::ENOENT.
  get "/l_missing_view" do
    begin
      erb :ghost
    rescue => e
      e.class.to_s
    end
  end
end

# [app, env] pairs — the first six keep App's original requests (and golden)
# byte-identical; the rest exercise LApp's layout rule.
REQS = [
  [App, { "REQUEST_METHOD" => "GET", "PATH_INFO" => "/inline", "QUERY_STRING" => "name=amy", "rack.url_scheme" => "http", "HTTP_HOST" => "ex" }],
  [App, { "REQUEST_METHOD" => "GET", "PATH_INFO" => "/loop", "QUERY_STRING" => "", "rack.url_scheme" => "http", "HTTP_HOST" => "ex" }],
  [App, { "REQUEST_METHOD" => "GET", "PATH_INFO" => "/locals", "QUERY_STRING" => "name=bob", "rack.url_scheme" => "http", "HTTP_HOST" => "ex" }],
  [App, { "REQUEST_METHOD" => "GET", "PATH_INFO" => "/opts_locals", "QUERY_STRING" => "name=eve", "rack.url_scheme" => "http", "HTTP_HOST" => "ex" }],
  [App, { "REQUEST_METHOD" => "GET", "PATH_INFO" => "/trim", "QUERY_STRING" => "", "rack.url_scheme" => "http", "HTTP_HOST" => "ex" }],
  [App, { "REQUEST_METHOD" => "GET", "PATH_INFO" => "/view", "QUERY_STRING" => "", "rack.url_scheme" => "http", "HTTP_HOST" => "ex" }],
  [LApp, { "REQUEST_METHOD" => "GET", "PATH_INFO" => "/l_default", "QUERY_STRING" => "", "rack.url_scheme" => "http", "HTTP_HOST" => "ex" }],
  [LApp, { "REQUEST_METHOD" => "GET", "PATH_INFO" => "/l_none", "QUERY_STRING" => "", "rack.url_scheme" => "http", "HTTP_HOST" => "ex" }],
  [LApp, { "REQUEST_METHOD" => "GET", "PATH_INFO" => "/l_custom", "QUERY_STRING" => "", "rack.url_scheme" => "http", "HTTP_HOST" => "ex" }],
  [LApp, { "REQUEST_METHOD" => "GET", "PATH_INFO" => "/l_true", "QUERY_STRING" => "", "rack.url_scheme" => "http", "HTTP_HOST" => "ex" }],
  [LApp, { "REQUEST_METHOD" => "GET", "PATH_INFO" => "/l_inline", "QUERY_STRING" => "", "rack.url_scheme" => "http", "HTTP_HOST" => "ex" }],
  [LApp, { "REQUEST_METHOD" => "GET", "PATH_INFO" => "/l_missing_layout", "QUERY_STRING" => "", "rack.url_scheme" => "http", "HTTP_HOST" => "ex" }],
  [LApp, { "REQUEST_METHOD" => "GET", "PATH_INFO" => "/l_missing_view", "QUERY_STRING" => "", "rack.url_scheme" => "http", "HTTP_HOST" => "ex" }],
  [LApp, { "REQUEST_METHOD" => "GET", "PATH_INFO" => "/l_standalone", "QUERY_STRING" => "", "rack.url_scheme" => "http", "HTTP_HOST" => "ex" }],
]

REQS.each_with_index do |(app, env), i|
  status, _headers, body = app.call(env)
  parts = []
  body.each { |p| parts << p }
  puts "== req #{i} =="
  puts "status=#{status}"
  puts "body<<#{parts.join}>>"
end
