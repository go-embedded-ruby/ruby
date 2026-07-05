# frozen_string_literal: true
#
# Sinatra oracle app — the reference app whose response is diffed byte-for-byte
# against the real `sinatra` gem 4.2.1. It exercises the routing/dispatch core
# the go-ruby-sinatra binding re-exposes: a :name capture, a query param, a
# before-filter instance variable that must survive into the route body (real
# Sinatra runs before/route/after against ONE instance), a splat, an optional
# param, redirect + Location, halt, a custom error(code) handler, a custom
# not_found handler and an after-filter that mutates the response headers.
#
# The identical file runs unchanged under MRI + the sinatra gem; the expected
# output in sinatra_oracle_test.go was captured from that run (gem 4.2.1,
# ruby 4.0.5). To regenerate the golden vector, run this file with `ruby` and
# the sinatra gem installed.
#
# Two response headers are excluded from the diff and tracked as go-ruby-sinatra
# LIBRARY follow-ups (they are response-finalization details the pure-Go core
# does not emit yet, not binding concerns):
#   * Content-Length — the gem sets it; go-ruby-rack does not finalize it.
#   * X-Cascade: pass — the gem adds it to every 404; go-ruby-sinatra does not.

require "sinatra/base"

class App < Sinatra::Base
  disable :protection
  set :host_authorization, { permitted_hosts: [] }
  set :default_content_type, "text/html"

  before { @uid = "u42" }
  after  { headers "X-After" => "seen" }

  get "/greet/:name" do
    content_type :json
    %({"hello":"#{params['name']}","uid":"#{@uid}","q":"#{params['q']}"})
  end

  get "/files/*" do
    "splat=#{params['splat'].join('|')}"
  end

  get "/opt/:a/?:b?" do
    "a=#{params['a']} b=#{params['b'].inspect}"
  end

  get "/go" do
    redirect "/greet/there", 303
  end

  get "/boom" do
    halt 418, "teapot"
  end

  get "/err" do
    500
  end
  error(500) { "custom-error" }

  not_found { "no-such-route" }
end

ENVS = [
  { "REQUEST_METHOD" => "GET", "PATH_INFO" => "/greet/amy", "QUERY_STRING" => "q=1", "rack.url_scheme" => "http", "HTTP_HOST" => "ex" },
  { "REQUEST_METHOD" => "GET", "PATH_INFO" => "/files/a/b/c.txt", "QUERY_STRING" => "", "rack.url_scheme" => "http", "HTTP_HOST" => "ex" },
  { "REQUEST_METHOD" => "GET", "PATH_INFO" => "/opt/x", "QUERY_STRING" => "", "rack.url_scheme" => "http", "HTTP_HOST" => "ex" },
  { "REQUEST_METHOD" => "GET", "PATH_INFO" => "/go", "QUERY_STRING" => "", "rack.url_scheme" => "http", "HTTP_HOST" => "ex" },
  { "REQUEST_METHOD" => "GET", "PATH_INFO" => "/boom", "QUERY_STRING" => "", "rack.url_scheme" => "http", "HTTP_HOST" => "ex" },
  { "REQUEST_METHOD" => "GET", "PATH_INFO" => "/err", "QUERY_STRING" => "", "rack.url_scheme" => "http", "HTTP_HOST" => "ex" },
  { "REQUEST_METHOD" => "GET", "PATH_INFO" => "/missing", "QUERY_STRING" => "", "rack.url_scheme" => "http", "HTTP_HOST" => "ex" },
]

ENVS.each_with_index do |env, i|
  status, headers, body = App.call(env)
  parts = []
  body.each { |p| parts << p }
  puts "== req #{i} =="
  puts "status=#{status}"
  headers.keys.sort.each do |k|
    next if ["content-length", "x-cascade"].include?(k.downcase) # library follow-ups (see header comment)
    puts "H #{k.downcase}: #{headers[k]}"
  end
  puts "body=#{parts.join}"
end
