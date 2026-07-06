# frozen_string_literal: true
#
# Sinatra + ERB oracle app — the reference app whose response is diffed
# byte-for-byte against the real `sinatra` gem 4.2.1 (+ its ERB view engine). It
# exercises the templating half of the Sinatra binding the go-ruby-erb-backed
# `erb` helper re-exposes: <%= %> interpolation of a request param, an @ivar set
# in a before-filter (real Sinatra runs before/route against ONE instance, so it
# is visible to the view), <% %> control flow, `locals:` passed both positionally
# and inside the options Hash, ERB trim behaviour, and a :symbol template read
# from the app's :views directory.
#
# The identical file runs unchanged under MRI + the sinatra gem; the expected
# output in sinatra_erb_oracle_test.go was captured from that run (gem 4.2.1,
# ruby 4.0.5). To regenerate the golden vector, run this file with `ruby` and the
# sinatra gem installed.
#
# Trim note (a precisely-scoped go-ruby-erb LIBRARY follow-up, NOT a binding gap):
# Sinatra 4.x maps :erb to Tilt::ErubiTemplate (erubi), whereas go-ruby-erb
# mirrors MRI's ERB::Compiler. Rendered with trim "<>" (Tilt::ERBTemplate's own
# default) the two agree byte-for-byte for single-line templates and idiomatic
# multiline templates (a code-only `<% … %>` alone on its line). They diverge only
# for the unusual line that BOTH opens a code block and ends with an expression
# tag with trailing content (`<% each %>x <%= v %>`), where MRI ERB trims the
# trailing newline and erubi keeps it. This fixture stays inside the agreeing
# subset, so the diff is exact; full erubi trim-parity is tracked upstream in
# go-ruby-erb.

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

ENVS = [
  { "REQUEST_METHOD" => "GET", "PATH_INFO" => "/inline", "QUERY_STRING" => "name=amy", "rack.url_scheme" => "http", "HTTP_HOST" => "ex" },
  { "REQUEST_METHOD" => "GET", "PATH_INFO" => "/loop", "QUERY_STRING" => "", "rack.url_scheme" => "http", "HTTP_HOST" => "ex" },
  { "REQUEST_METHOD" => "GET", "PATH_INFO" => "/locals", "QUERY_STRING" => "name=bob", "rack.url_scheme" => "http", "HTTP_HOST" => "ex" },
  { "REQUEST_METHOD" => "GET", "PATH_INFO" => "/opts_locals", "QUERY_STRING" => "name=eve", "rack.url_scheme" => "http", "HTTP_HOST" => "ex" },
  { "REQUEST_METHOD" => "GET", "PATH_INFO" => "/trim", "QUERY_STRING" => "", "rack.url_scheme" => "http", "HTTP_HOST" => "ex" },
  { "REQUEST_METHOD" => "GET", "PATH_INFO" => "/view", "QUERY_STRING" => "", "rack.url_scheme" => "http", "HTTP_HOST" => "ex" },
]

ENVS.each_with_index do |env, i|
  status, _headers, body = App.call(env)
  parts = []
  body.each { |p| parts << p }
  puts "== req #{i} =="
  puts "status=#{status}"
  puts "body<<#{parts.join}>>"
end
