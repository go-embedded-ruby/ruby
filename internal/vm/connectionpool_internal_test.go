// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// cpRun runs a Ruby program with connection_pool already required, returning its
// trimmed stdout. Every ConnectionPool test drives the whole binding through rbgo,
// exactly as a user would.
func cpRun(t *testing.T, body string) string {
	t.Helper()
	return runSrc(t, "require \"connection_pool\"\n"+body)
}

// TestConnectionPoolBasics drives the core pool surface: lazy creation, #with
// checkout/yield/checkin, #size / #available, and the value's inspect/to_s.
func TestConnectionPoolBasics(t *testing.T) {
	got := cpRun(t, `
n = 0
pool = ConnectionPool.new(size: 3, timeout: 2) { n += 1; "c#{n}" }
puts pool.size            # 3
puts pool.available       # 3 (none created yet)
pool.with { |c| puts c }  # c1
puts n                    # 1 (created lazily, once)
puts pool.available       # 3 (returned)
puts pool.inspect
puts pool.to_s
puts(!!pool)              # true — Truthy
`)
	want := "3\n3\nc1\n1\n3\n#<ConnectionPool>\n#<ConnectionPool>\ntrue"
	if got != want {
		t.Fatalf("basics:\n got %q\nwant %q", got, want)
	}
}

// TestConnectionPoolReentrancy proves a nested #with / #checkout on the same
// thread reuses the one connection (the caller-key seam), and that #checkout /
// #checkin pair up explicitly.
func TestConnectionPoolReentrancy(t *testing.T) {
	got := cpRun(t, `
n = 0
pool = ConnectionPool.new(size: 1, timeout: 1) { n += 1; Object.new }
pool.with do |a|
  pool.with do |b|
    puts a.equal?(b)   # true — reused
  end
end
puts n                 # 1

c = pool.checkout      # explicit, reuses via key only within a with; fresh here
puts c.equal?(pool.checkout)  # true — same thread reentrant checkout
pool.checkin
pool.checkin
`)
	if got != "true\n1\ntrue" {
		t.Fatalf("reentrancy: got %q", got)
	}
}

// TestConnectionPoolCheckinError covers #checkin raising ConnectionPool::Error
// when the thread holds no connection (the raiseCPError default branch).
func TestConnectionPoolCheckinError(t *testing.T) {
	got := cpRun(t, `
pool = ConnectionPool.new(size: 1) { [] }
begin
  pool.checkin
rescue ConnectionPool::Error => e
  puts "err: #{e.message}"
end
`)
	if got != "err: no connections are checked out" {
		t.Fatalf("checkin error: got %q", got)
	}
}

// TestConnectionPoolTimeout proves an exhausted pool times out with
// ConnectionPool::TimeoutError across threads — one thread holds the only
// connection while another parks and gives up. It exercises the GVL-releasing
// blocking-checkout path.
func TestConnectionPoolTimeout(t *testing.T) {
	got := cpRun(t, `
pool = ConnectionPool.new(size: 1, timeout: 0.05) { Object.new }
c = pool.checkout
t = Thread.new do
  begin
    pool.checkout
    puts "no-timeout"
  rescue ConnectionPool::TimeoutError
    puts "timeout"
  end
end
t.join
pool.checkin
`)
	if got != "timeout" {
		t.Fatalf("timeout: got %q", got)
	}
}

// TestConnectionPoolWaitThenGet proves a parked checkout succeeds once another
// thread checks its connection back in (the success branch after the blocking
// wait), and that a :timeout override is honoured.
func TestConnectionPoolWaitThenGet(t *testing.T) {
	got := cpRun(t, `
pool = ConnectionPool.new(size: 1, timeout: 5) { Object.new }
c = pool.checkout
result = nil
t = Thread.new { pool.with(timeout: 5) { |x| result = "got" } }
sleep 0.05
pool.checkin
t.join
puts result
`)
	if got != "got" {
		t.Fatalf("wait-then-get: got %q", got)
	}
}

// TestConnectionPoolShutdownReload covers #shutdown (disposes idle connections and
// blocks later checkouts with PoolShuttingDownError) and #reload (disposes then
// reopens for reuse).
func TestConnectionPoolShutdownReload(t *testing.T) {
	got := cpRun(t, `
pool = ConnectionPool.new(size: 2, timeout: 0.1) { [] }
pool.checkin rescue nil
a = pool.checkout; pool.checkin
disposed = 0
pool.shutdown { |c| disposed += 1 }
puts disposed                    # 1 (one idle conn was created)
begin
  pool.checkout
rescue ConnectionPool::PoolShuttingDownError
  puts "blocked"
end

# reload reopens
pool2 = ConnectionPool.new(size: 1) { [] }
pool2.checkout; pool2.checkin
rd = 0
pool2.reload { |c| rd += 1 }
puts rd                          # 1
pool2.with { |c| puts "reusable" }
`)
	want := "1\nblocked\n1\nreusable"
	if got != want {
		t.Fatalf("shutdown/reload:\n got %q\nwant %q", got, want)
	}
}

// TestConnectionPoolArgErrors covers the argument-validation raises: no block on
// #new / #shutdown / #reload (ArgumentError), no block on #with (LocalJumpError),
// and a non-Numeric timeout: (TypeError from cpDuration).
func TestConnectionPoolArgErrors(t *testing.T) {
	got := cpRun(t, `
def err
  yield
  "no-raise"
rescue => e
  "#{e.class}"
end
puts(err { ConnectionPool.new(size: 1) })                 # ArgumentError (no block)
puts(err { ConnectionPool.new(size: 1) { [] }.with })     # LocalJumpError (no block)
puts(err { ConnectionPool.new(size: 1) { [] }.shutdown }) # ArgumentError
puts(err { ConnectionPool.new(size: 1) { [] }.reload })   # ArgumentError
puts(err { ConnectionPool.new(timeout: "x") { [] } })     # TypeError
`)
	want := "ArgumentError\nLocalJumpError\nArgumentError\nArgumentError\nTypeError"
	if got != want {
		t.Fatalf("arg errors:\n got %q\nwant %q", got, want)
	}
}

// TestConnectionPoolNonHashOptions proves a positional (non-Hash) argument leaves
// the pool at its defaults (cpOptions returns nil), and an Integer timeout: is
// honoured (the Integer branch of cpDuration).
func TestConnectionPoolNonHashOptions(t *testing.T) {
	got := cpRun(t, `
pool = ConnectionPool.new(5) { [] }   # positional, ignored -> defaults
puts pool.size                        # 5 default
p2 = ConnectionPool.new(size: 2, timeout: 1) { [] }  # Integer timeout branch
puts p2.size
`)
	if got != "5\n2" {
		t.Fatalf("non-hash options: got %q", got)
	}
}

// TestConnectionPoolWrapper drives ConnectionPool::Wrapper: method_missing
// delegation, respond_to?, #with, #wrapped_pool, #pool_size / #pool_available and
// the .wrap shortcut, plus its inspect/to_s.
func TestConnectionPoolWrapper(t *testing.T) {
	got := cpRun(t, `
w = ConnectionPool::Wrapper.new(size: 1, timeout: 1) { [] }
w.push(1); w.push(2)                 # method_missing -> Array#push
puts w.with { |a| a.length }         # 2
puts w.respond_to?(:push)            # true (delegated)
puts w.respond_to?(:nope_xyz)        # false
puts w.pool_size                     # 1
puts w.pool_available                # 1 (returned after each call)
puts w.wrapped_pool.is_a?(ConnectionPool)  # true
puts w.inspect
puts w.to_s
puts(!!w)                            # true

# .wrap shortcut builds a Wrapper
w2 = ConnectionPool.wrap(size: 1) { [] }
w2.push(:x)
puts w2.with { |a| a.first.inspect } # :x

# Wrapper.new(pool: existing) reuses a pool
base = ConnectionPool.new(size: 1) { [] }
w3 = ConnectionPool::Wrapper.new(pool: base)
puts w3.wrapped_pool.equal?(base)    # true

# pool_shutdown disposes the idle connection the delegated call returned
w4 = ConnectionPool::Wrapper.new(size: 1) { [] }
w4.push(:y)                          # creates a conn, returns it to the pool
disposed = 0
w4.pool_shutdown { |c| disposed += 1 }
puts disposed                        # 1
`)
	want := "2\ntrue\nfalse\n1\n1\ntrue\n#<ConnectionPool::Wrapper>\n#<ConnectionPool::Wrapper>\ntrue\n:x\ntrue\n1"
	if got != want {
		t.Fatalf("wrapper:\n got %q\nwant %q", got, want)
	}
}

// TestConnectionPoolWrapperErrors covers the Wrapper error and validation paths:
// a non-pool pool: (TypeError), a missing block on the build path (ArgumentError),
// #with / #pool_shutdown without a block, and delegation / respond_to? / #with
// after the underlying pool is shut down (raiseCPError through the Wrapper seams).
func TestConnectionPoolWrapperErrors(t *testing.T) {
	got := cpRun(t, `
def err
  yield
  "no-raise"
rescue => e
  "#{e.class}"
end
puts(err { ConnectionPool::Wrapper.new(pool: "x") })          # TypeError
puts(err { ConnectionPool::Wrapper.new(size: 1) })            # ArgumentError (no block)
puts(err { ConnectionPool::Wrapper.new(size: 1) { [] }.with }) # LocalJumpError
puts(err { ConnectionPool::Wrapper.new(size: 1) { [] }.pool_shutdown }) # ArgumentError

w = ConnectionPool::Wrapper.new(size: 1, timeout: 0.05) { [] }
w.pool_shutdown { |c| }                                        # shut the pool
puts(err { w.push(1) })                                        # PoolShuttingDownError via method_missing
puts(err { w.respond_to?(:push) })                             # PoolShuttingDownError via respond_to_missing?
puts(err { w.with { |c| } })                                   # PoolShuttingDownError via #with
`)
	want := "TypeError\nArgumentError\nLocalJumpError\nArgumentError\n" +
		"ConnectionPool::PoolShuttingDownError\nConnectionPool::PoolShuttingDownError\nConnectionPool::PoolShuttingDownError"
	if got != want {
		t.Fatalf("wrapper errors:\n got %q\nwant %q", got, want)
	}
}

// TestConnectionPoolCheckoutShutdown proves a plain #checkout on a shut-down pool
// raises PoolShuttingDownError (the non-blocking-path shutdown error in
// cpCheckout).
func TestConnectionPoolCheckoutShutdown(t *testing.T) {
	got := cpRun(t, `
pool = ConnectionPool.new(size: 1) { [] }
pool.shutdown { |c| }
begin
  pool.checkout
rescue ConnectionPool::PoolShuttingDownError
  puts "down"
end
`)
	if got != "down" {
		t.Fatalf("checkout shutdown: got %q", got)
	}
}
