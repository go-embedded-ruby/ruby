# frozen_string_literal: true
#
# Sinatra session-store oracle app — proves `enable :sessions` (the cookie
# session store, Rack::Session::Cookie semantics) end-to-end THROUGH rbgo.
#
# It drives its own request sequence, threading each response's Set-Cookie back
# in as the next request's Cookie, exactly as a browser does. The observable
# behaviour it prints (a session counter that increments across requests,
# persists across a different route, resets when the cookie is tampered, and
# clears on session.clear) is identical under MRI + the sinatra gem — the cookie
# *bytes* are an internal detail, so the printed transcript is an MRI-portable
# functional oracle (see stage5's test for the byte-exact serialization proof,
# which lives in the internal suite where the secret is fixed).

require "sinatra/base"

class App < Sinatra::Base
  enable :sessions
  set :session_secret, "conformance-secret-please-change"

  get "/inc" do
    session[:count] = (session[:count] || 0) + 1
    "count=#{session[:count]}"
  end

  # A DIFFERENT route reads the same session — persistence is not route-local.
  get "/read" do
    "count=#{session[:count].inspect}"
  end

  get "/clear" do
    session.clear
    "count=#{session[:count].inspect}"
  end
end

# cookie_from extracts the "name=value" a browser would store and echo back from
# a response's Set-Cookie (dropping the attributes and any extra set-cookies).
def cookie_from(headers)
  set_cookie = nil
  headers.each { |k, v| set_cookie = v if k.downcase == "set-cookie" }
  return nil unless set_cookie
  set_cookie.to_s.split("\n").first.split(";").first
end

# req dispatches one GET through the Rack #call adapter, optionally with a Cookie.
def req(path, cookie = nil)
  env = {
    "REQUEST_METHOD" => "GET", "PATH_INFO" => path, "QUERY_STRING" => "",
    "rack.url_scheme" => "http", "HTTP_HOST" => "ex"
  }
  env["HTTP_COOKIE"] = cookie if cookie
  App.call(env)
end

# 1) First increment — no cookie in; a session cookie must come out.
_, h1, b1 = req("/inc")
c1 = cookie_from(h1)
puts "r1 body=#{b1.join}"
puts "r1 set_cookie=#{!c1.nil?}"

# 2) Second increment — thread the cookie; the count must carry over.
_, h2, b2 = req("/inc", c1)
c2 = cookie_from(h2)
puts "r2 body=#{b2.join}"

# 3) Read via a DIFFERENT route — the value persists across routes.
_, _, b3 = req("/read", c2)
puts "r3 body=#{b3.join}"

# 4) Tamper — corrupt the signed cookie; the bad HMAC must be rejected and a
#    fresh empty session used, so the counter restarts at 1.
name, value = c2.split("=", 2)
tampered = "#{name}=#{value}00"
_, _, b4 = req("/inc", tampered)
puts "r4 body=#{b4.join}"

# 5) A genuine cookie still round-trips (count keeps climbing to 3), then clear
#    empties the store.
_, h5, b5 = req("/inc", c2)
puts "r5 body=#{b5.join}"
_, _, b6 = req("/clear", cookie_from(h5))
puts "r6 body=#{b6.join}"
