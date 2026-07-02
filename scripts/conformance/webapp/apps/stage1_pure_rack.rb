# Stage 1 — Pure Rack.
# The most minimal possible Rack app: a lambda that takes an `env` Hash and
# returns the canonical `[status, headers, body]` triple. No gem needed — this
# is the Rack "SPEC" contract expressed in plain Ruby, so it exercises procs,
# Hash access, String interpolation and multiple assignment through rbgo.
#
# The app is invoked with a synthetic Rack env (no TCP socket) and the response
# is printed deterministically for the Go harness to assert.

app = ->(env) {
  [200, { "content-type" => "text/plain" }, ["hello #{env['PATH_INFO']}"]]
}

status, headers, body = app.call("PATH_INFO" => "/x", "REQUEST_METHOD" => "GET")

puts "status=#{status}"
puts "content-type=#{headers['content-type']}"
puts "body=#{body.join}"
