# Stage 3 — Sinatra DSL.
# A minimal classic Sinatra::Base application exercised through its Rack `call`
# interface. This app is only run by the Go harness when `sinatra/base` is
# loadable; the harness feature-detects first and, when Sinatra is absent,
# records the exact gap instead of executing this file.
#
# When a go-ruby-sinatra binding lands, this is the app that must go green:
#   GET /hi?n=amy  =>  200, body "hi amy".

require "sinatra/base"

class App < Sinatra::Base
  get "/hi" do
    "hi #{params['n']}"
  end
end

status, headers, body = App.new.call(
  "PATH_INFO"      => "/hi",
  "QUERY_STRING"   => "n=amy",
  "REQUEST_METHOD" => "GET"
)

# Rack bodies respond to #each; join whatever the adapter returns.
rendered = +""
body.each { |chunk| rendered << chunk.to_s }

puts "status=#{status}"
puts "body=#{rendered}"
