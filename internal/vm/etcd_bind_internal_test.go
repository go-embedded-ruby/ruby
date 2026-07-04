// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"go.etcd.io/etcd/server/v3/embed"
)

// startEmbeddedEtcd starts a single-node etcd server in-process on ephemeral
// loopback ports, backed by a per-test temp data directory, and returns its
// bound client address. The server is stopped and its error channel drained in a
// t.Cleanup, so no etcd/raft goroutine, listener or port outlives the test — a
// leaked server would block the whole vm suite. No fixed port is ever used
// (127.0.0.1:0 lets the kernel choose), so parallel packages never collide.
func startEmbeddedEtcd(t *testing.T) string {
	t.Helper()
	cfg := embed.NewConfig()
	cfg.Dir = t.TempDir()
	cfg.LogLevel = "error"
	ephemeral := func(raw string) []url.URL {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatalf("parse url: %v", err)
		}
		return []url.URL{*u}
	}
	cfg.ListenClientUrls = ephemeral("http://127.0.0.1:0")
	cfg.ListenPeerUrls = ephemeral("http://127.0.0.1:0")
	cfg.AdvertiseClientUrls = cfg.ListenClientUrls
	cfg.AdvertisePeerUrls = cfg.ListenPeerUrls
	cfg.InitialCluster = cfg.InitialClusterFromName(cfg.Name)

	e, err := embed.StartEtcd(cfg)
	if err != nil {
		t.Fatalf("start embedded etcd: %v", err)
	}
	select {
	case <-e.Server.ReadyNotify():
	case <-time.After(30 * time.Second):
		e.Close()
		t.Fatal("embedded etcd did not become ready in time")
	}
	t.Cleanup(func() {
		e.Close()
		// Drain the server's error channel so a shutdown error surfaces and no
		// close goroutine is left blocked writing to it.
		<-e.Err()
	})
	return e.Clients[0].Addr().String()
}

// etcdRun runs a Ruby program with an ADDR constant bound to the embedded
// server's client address, returning its trimmed stdout. Every etcd test drives
// the whole binding through rbgo, exactly as a user would.
func etcdRun(t *testing.T, addr, body string) string {
	t.Helper()
	src := fmt.Sprintf("require \"etcd\"\nADDR = %q\n%s", addr, body)
	return runSrc(t, src)
}

// TestEtcd drives the entire Etcd binding end to end against one shared embedded
// etcd server. Subtests are grouped so the single server (and its single
// t.Cleanup) covers every operation without starting a fresh cluster per case.
func TestEtcd(t *testing.T) {
	addr := startEmbeddedEtcd(t)

	t.Run("PutGetRoundTrip", func(t *testing.T) {
		got := etcdRun(t, addr, `
conn = Etcdv3.new(endpoints: "http://#{ADDR}")
r = []
pr = conn.put("k1", "v1")
r << (pr.revision > 0)
r << pr.prev_kv.inspect            # nil (no prev_kv requested)
r << conn.get("k1").value          # "v1"
r << conn.get("k1").first.value    # "v1"
r << conn.get("k1").first.key      # "k1"
r << conn.get("k1").count          # 1
r << conn.get("k1").more?          # false
r << conn.get("k1").empty?         # false
r << conn.get("missing").empty?    # true
r << conn.get("missing").first.inspect  # nil
r << conn.get("missing").value.inspect  # nil
kv = conn.get("k1").first
r << kv.version
r << (kv.create_revision > 0)
r << (kv.mod_revision > 0)
r << kv.lease
r << kv.to_h[:key]
r << conn.exists?("k1")            # true
r << conn.exists?("missing")       # false
# a trailing non-Hash argument is not treated as options (etcdOptsHash)
r << conn.get("k1", "ignored").value   # "v1"
# #each without a block raises
begin; conn.get("k1").each; rescue LocalJumpError; r << "each_noblock"; end
# put attaching a lease passed as an Etcd::Lease object (not an Integer id)
lease = conn.lease_grant(60)
conn.put("leasedk", "lv", lease: lease)
r << conn.exists?("leasedk")       # true
lease.revoke
# prev_kv on overwrite
pr2 = conn.put("k1", "v2", prev_kv: true)
r << pr2.prev_kv.value             # "v1"
conn.close
puts r.join("\n")
`)
		want := []string{"true", "nil", "v1", "v1", "k1", "1", "false", "false", "true", "nil", "nil"}
		if !strings.HasPrefix(got, strings.Join(want, "\n")) {
			t.Fatalf("PutGetRoundTrip:\n%s", got)
		}
		if !strings.Contains(got, "\nv1") { // prev_kv value from overwrite
			t.Fatalf("expected prev_kv v1:\n%s", got)
		}
	})

	t.Run("RangesAndPrefixes", func(t *testing.T) {
		got := etcdRun(t, addr, `
conn = Etcdv3.new(endpoints: ADDR)
conn.put("p/a", "1"); conn.put("p/b", "2"); conn.put("p/c", "3")
r = []
r << conn.get("p/", prefix: true).count                  # 3
r << conn.get("p/", prefix: true, limit: 2).kvs.size     # 2
r << conn.get("p/", prefix: true, keys_only: true).first.value.empty?  # true
r << conn.get("p/", prefix: true, count_only: true).count # 3
r << conn.get("p/a", range_end: "p/c").count             # 2 (a,b)
r << conn.get("p/b", from_key: true).count               # 2 (b,c) at least
r << conn.get("p/", prefix: true, serializable: true).count  # 3
rev = conn.get("p/a").first.mod_revision
r << conn.get("p/a", revision: rev).first.value          # "1"
keys = []
conn.get("p/", prefix: true).each { |kv| keys << kv.key }
r << keys.join(",")
r << conn.get("p/", prefix: true).to_a.size
# delete a prefix
dr = conn.del("p/", prefix: true, prev_kv: true)
r << dr.deleted                                          # 3
r << dr.prev_kvs.size                                    # 3
r << conn.get("p/", prefix: true).count                  # 0
# single delete with prev_kv
conn.put("solo", "x")
r << conn.delete("solo", prev_kv: true).prev_kvs.first.value  # "x"
puts r.join("\n")
`)
		lines := strings.Split(got, "\n")
		expect := map[int]string{0: "3", 1: "2", 2: "true", 3: "3", 4: "2", 6: "3", 7: "1"}
		for i, w := range expect {
			if lines[i] != w {
				t.Fatalf("RangesAndPrefixes line %d = %q want %q\nfull:\n%s", i, lines[i], w, got)
			}
		}
		if !strings.Contains(got, "p/a,p/b,p/c") {
			t.Fatalf("expected ordered keys:\n%s", got)
		}
	})

	t.Run("Transaction", func(t *testing.T) {
		got := etcdRun(t, addr, `
conn = Etcdv3.new(endpoints: ADDR)
r = []
# succeeds: key absent -> create_revision == 0
res = conn.transaction do |txn|
  txn.compare = [txn.create_revision("tk", :equal, 0)]
  txn.success = [txn.put("tk", "created")]
  txn.failure = [txn.get("tk")]
end
r << res.succeeded?            # true
r << res.success?             # true
r << (res.revision > 0)       # true
r << res.responses.first.class.name   # Etcd::PutResult
r << conn.get("tk").value     # "created"
# fails: now key exists -> runs failure branch (a get)
res2 = conn.transaction do |txn|
  txn.compare = [txn.create_revision("tk", :equal, 0)]
  txn.success = [txn.put("tk", "again")]
  txn.failure = [txn.get("tk")]
end
r << res2.succeeded?          # false
r << res2.responses.first.value  # "created" (get result)
# a delete op in a branch, and single (non-array) compare, string operator
res3 = conn.transaction do |txn|
  txn.compare = txn.value("tk", "=", "created")
  txn.success = [txn.del("tk")]
end
r << res3.succeeded?          # true
r << res3.responses.first.deleted  # 1
# version / mod_revision / lease comparison builders (compile + run)
conn.put("vk", "1")
res4 = conn.transaction do |txn|
  txn.compare = [txn.version("vk", :greater, 0), txn.mod_revision("vk", :greater, 0), txn.lease("vk", :equal, 0)]
  txn.success = [txn.get("vk")]
end
r << res4.succeeded?          # true
# not_equal / less / >  operators
res5 = conn.transaction do |txn|
  txn.compare = [txn.value("vk", :not_equal, "zzz"), txn.version("vk", :less, 99), txn.version("vk", :greater, 0)]
  txn.success = [txn.get("vk")]
end
r << res5.succeeded?          # true
puts r.join("\n")
`)
		want := "true\ntrue\ntrue\nEtcd::PutResult\ncreated\nfalse\ncreated\ntrue\n1\ntrue\ntrue"
		if got != want {
			t.Fatalf("Transaction:\ngot:\n%s\nwant:\n%s", got, want)
		}
	})

	t.Run("Watch", func(t *testing.T) {
		got := etcdRun(t, addr, `
conn = Etcdv3.new(endpoints: ADDR)
# generate a deterministic history to replay
conn.put("wk", "1", prev_kv: true)
conn.put("wk", "2", prev_kv: true)
conn.del("wk", prev_kv: true)
r = []
events = []
n = conn.watch("wk", start_revision: 1, prev_kv: true, max_events: 3, timeout: 3) do |ev|
  events << ev
end
r << n                                 # 3
r << events[0].type                    # PUT
r << events[0].put?                    # true
r << events[0].delete?                 # false
r << events[0].value                   # "1"
r << events[0].key                     # "wk"
r << events[0].kv.value                # "1"
r << events[0].prev_kv.inspect         # nil (first put has no prev)
r << events[1].prev_kv.value           # "1"
r << events[2].type                    # DELETE
r << events[2].delete?                 # true
# prefix + revision alias watch
conn.put("wp/x", "a")
m = conn.watch("wp/", prefix: true, revision: 1, max_events: 1, timeout: 3) { |ev| }
r << m                                 # 1
# from_key + range_end options (compile paths); tiny timeout, no matching events -> 0
z = conn.watch("zzz", range_end: "zzz0", timeout: 0.2) { |ev| }
r << z                                 # 0
z2 = conn.watch("zzzz9", from_key: true, timeout: 0.2) { |ev| }
r << z2                                # 0 (from_key past all keys)
# no-keyword watch: nil options Hash, bounded by the client's command_timeout so
# it returns fast; exercises the nil-normalisation path.
fast = Etcdv3.new(endpoints: ADDR, command_timeout: 0.2)
r << fast.watch("never") { |ev| }      # 0
fast.close
puts r.join("\n")
`)
		want := "3\nPUT\ntrue\nfalse\n1\nwk\n1\nnil\n1\nDELETE\ntrue\n1\n0\n0\n0"
		if got != want {
			t.Fatalf("Watch:\ngot:\n%s\nwant:\n%s", got, want)
		}
	})

	t.Run("Leases", func(t *testing.T) {
		got := etcdRun(t, addr, `
conn = Etcdv3.new(endpoints: ADDR)
r = []
lease = conn.lease_grant(60)
r << (lease.id != 0)          # true
r << lease.ttl                # 60
# attach a key to the lease, then inspect TTL info with keys
conn.put("leased", "v", lease: lease.id)
info = lease.ttl_info(keys: true)
r << (info[:ttl] <= 60)       # true
r << info[:granted_ttl]       # 60
r << info[:keys].include?("leased")  # true
r << (info[:id] == lease.id)  # true
# keep-alive via object and via client with Integer id
r << (lease.keep_alive <= 60)                 # true
r << (conn.lease_keep_alive(lease.id) <= 60)  # true
# ttl info without keys
r << conn.lease_ttl(lease.id)[:keys].empty?   # true
# revoke deletes the leased key
lease.revoke
r << conn.exists?("leased")   # false
# grant + revoke via client id
l2 = conn.lease_grant(45)
conn.lease_revoke(l2.id)
r << true
puts r.join("\n")
`)
		want := "true\n60\ntrue\n60\ntrue\ntrue\ntrue\ntrue\ntrue\nfalse\ntrue"
		if got != want {
			t.Fatalf("Leases:\ngot:\n%s\nwant:\n%s", got, want)
		}
	})

	t.Run("LockAndMaintenance", func(t *testing.T) {
		got := etcdRun(t, addr, `
conn = Etcdv3.new(endpoints: ADDR)
r = []
lk = conn.lock("resource", 30)
r << lk.key.start_with?("resource/")  # true
r << (lk.lease != 0)                  # true
conn.unlock(lk)
r << true
# unlocking again revokes an already-revoked lease -> Etcd error (unlock error path)
begin; conn.unlock(lk); rescue Etcd::Error; end
# members
ms = conn.members
r << (ms.size >= 1)                   # true
m = ms.first
r << (m.id != 0)                      # true
r << (m.name.length >= 0)             # true
r << m.peer_urls.first.start_with?("http")   # true
r << m.client_urls.first.start_with?("http") # true
r << m.learner?                       # false
# status
st = conn.status(ADDR)
r << st.version.length                # > 0
r << (st.db_size >= 0)                # true
r << (st.leader != 0)                 # true
r << (st.raft_index > 0)              # true
r << (st.raft_term > 0)               # true
r << st.learner?                      # false
# endpoints reader
r << conn.endpoints.first             # ADDR
puts r.join("\n")
`)
		lines := strings.Split(got, "\n")
		for i, w := range []string{"true", "true", "true", "true", "true", "true", "true", "true", "false"} {
			if lines[i] != w {
				t.Fatalf("LockAndMaintenance line %d = %q want %q\nfull:\n%s", i, lines[i], w, got)
			}
		}
		if !strings.HasSuffix(got, addr) {
			t.Fatalf("expected endpoints to end with %q:\n%s", addr, got)
		}
	})

	t.Run("InspectAndTruthy", func(t *testing.T) {
		// Exercise every wrapper's ToS/Inspect/Truthy so each is covered.
		got := etcdRun(t, addr, `
conn = Etcdv3.new(endpoints: ADDR)
conn.put("ik", "iv")
lease = conn.lease_grant(30)
lock = conn.lock("ilock", 30)
member = conn.members.first
status = conn.status(ADDR)
getr = conn.get("ik")
putr = conn.put("ik", "iv2")
delr = conn.del("gone")
kv = getr.first
txnr = conn.transaction { |t| t.compare = []; t.success = [t.get("ik")] }
cmp_holder = []
op_holder = []
txn_holder = []
conn.transaction do |t|
  txn_holder << t
  cmp_holder << t.value("ik", :equal, "iv2")
  op_holder << t.put("ik", "iv3")
  t.compare = []; t.success = []
end
ev_holder = []
conn.watch("ik", start_revision: 1, max_events: 1, timeout: 3) { |e| ev_holder << e }
objs = [conn, kv, getr, putr, delr, lease, ev_holder.first, txn_holder.first,
        cmp_holder.first, op_holder.first, txnr, lock, member, status]
objs.each do |o|
  p o                      # Inspect
  print o.to_s, "\n"       # ToS
  raise "falsy" unless o   # Truthy
end
puts "OK #{objs.size}"
`)
		if !strings.Contains(got, "OK 14") {
			t.Fatalf("InspectAndTruthy did not complete:\n%s", got)
		}
		if !strings.Contains(got, "#<Etcd::Client>") || !strings.Contains(got, "#<Etcd::Event PUT") {
			t.Fatalf("InspectAndTruthy missing expected inspects:\n%s", got)
		}
	})

	t.Run("Errors", func(t *testing.T) {
		got := etcdRun(t, addr, `
conn = Etcdv3.new(endpoints: ADDR)
r = []
# empty key -> Etcd::InvalidArgument (< Etcd::Error < StandardError)
begin
  conn.get("")
rescue Etcd::InvalidArgument => e
  r << "invalid_arg"
  r << (e.is_a?(Etcd::Error))
  r << (e.is_a?(StandardError))
end
begin; conn.put("", "v"); rescue Etcd::Error; r << "put_empty"; end
begin; conn.del(""); rescue Etcd::Error; r << "del_empty"; end
begin; conn.exists?(""); rescue Etcd::Error; r << "exists_empty"; end
# unknown lease keep-alive -> mapped error subclass
begin
  conn.lease_keep_alive(1234567)
rescue Etcd::Error => e
  r << "lease_err"
end
puts r.join("\n")
`)
		want := "invalid_arg\ntrue\ntrue\nput_empty\ndel_empty\nexists_empty\nlease_err"
		if got != want {
			t.Fatalf("Errors:\ngot:\n%s\nwant:\n%s", got, want)
		}
	})

	t.Run("ArgumentAndTypeErrors", func(t *testing.T) {
		got := etcdRun(t, addr, `
conn = Etcdv3.new(endpoints: ADDR)
r = []
def try(r, label)
  yield
  r << "#{label}:noraise"
rescue => e
  r << "#{label}:#{e.class.name.split('::').last}"
end
try(r, "put_arity") { conn.put("k") }
try(r, "get_arity") { conn.get }
try(r, "del_arity") { conn.delete }
try(r, "exists_arity") { conn.exists? }
try(r, "lease_grant_arity") { conn.lease_grant }
try(r, "keepalive_arity") { conn.lease_keep_alive }
try(r, "revoke_arity") { conn.lease_revoke }
try(r, "ttl_arity") { conn.lease_ttl }
try(r, "lock_arity") { conn.lock("only") }
try(r, "unlock_arity") { conn.unlock }
try(r, "status_arity") { conn.status }
try(r, "unlock_type") { conn.unlock("notalock") }
try(r, "lease_id_type") { conn.lease_keep_alive("nope") }
try(r, "transaction_noblock") { conn.transaction }
try(r, "watch_noblock") { conn.watch("k") }
try(r, "cmp_arity") { conn.transaction { |t| t.compare = [t.value("k")] } }
try(r, "cmp_op_type") { conn.transaction { |t| t.compare = [t.value("k", 5, "v")] } }
try(r, "cmp_op_unknown") { conn.transaction { |t| t.compare = [t.value("k", :nope, "v")] } }
try(r, "cmp_val_type") { conn.transaction { |t| t.compare = [t.value("k", :equal, [1,2])] } }
try(r, "cmp_list_type") { conn.transaction { |t| t.compare = [42] } }
try(r, "op_list_type") { conn.transaction { |t| t.success = [42] } }
try(r, "put_op_arity") { conn.transaction { |t| t.success = [t.put("k")] } }
try(r, "get_op_arity") { conn.transaction { |t| t.success = [t.get] } }
try(r, "del_op_arity") { conn.transaction { |t| t.success = [t.del] } }
try(r, "endpoints_none") { Etcdv3.new }
try(r, "endpoints_empty") { Etcdv3.new(endpoints: []) }
try(r, "endpoints_bad_type") { Etcdv3.new(endpoints: 123) }
try(r, "no_endpoint_key") { Etcdv3.new(other: 1) }
try(r, "dial_timeout_type") { Etcdv3.new(endpoints: ADDR, dial_timeout: "x") }
puts r.join("\n")
`)
		checks := map[string]string{
			"put_arity":           "ArgumentError",
			"get_arity":           "ArgumentError",
			"exists_arity":        "ArgumentError",
			"unlock_type":         "TypeError",
			"lease_id_type":       "TypeError",
			"transaction_noblock": "LocalJumpError",
			"watch_noblock":       "LocalJumpError",
			"cmp_op_type":         "ArgumentError",
			"cmp_op_unknown":      "ArgumentError",
			"cmp_val_type":        "TypeError",
			"cmp_list_type":       "TypeError",
			"op_list_type":        "TypeError",
			"endpoints_none":      "ArgumentError",
			"endpoints_empty":     "ArgumentError",
			"endpoints_bad_type":  "TypeError",
			"no_endpoint_key":     "ArgumentError",
			"dial_timeout_type":   "TypeError",
		}
		for label, want := range checks {
			needle := label + ":" + want
			if !strings.Contains(got, needle) {
				t.Fatalf("ArgumentAndTypeErrors: missing %q\nfull:\n%s", needle, got)
			}
		}
	})

	t.Run("ConnectOptions", func(t *testing.T) {
		got := etcdRun(t, addr, `
r = []
# Array endpoints, url: alias, hosts: alias, integer + float dial_timeout, user/password
c1 = Etcd::Client.new(endpoints: [ADDR], dial_timeout: 3)
r << c1.exists?("nope")                        # false
c1.close
c2 = Etcdv3.new(url: "http://#{ADDR}", dial_timeout: 2.5, username: "", password: "")
r << c2.exists?("nope")                        # false
c2.close
c3 = Etcd.new(hosts: ADDR, user: "")
r << c3.exists?("nope")                        # false
c3.close
# timeout: as the command_timeout alias
c3b = Etcdv3.new(endpoints: ADDR, timeout: 1.5)
r << c3b.exists?("nope")                        # false
c3b.close
# connect alias
c4 = Etcdv3.connect(endpoints: ADDR)
r << c4.exists?("nope")                        # false
c4.close
puts r.join("\n")
`)
		if got != "false\nfalse\nfalse\nfalse\nfalse" {
			t.Fatalf("ConnectOptions:\n%s", got)
		}
	})
}

// TestEtcdOperationErrors covers every operation's error branch cheaply: a
// client pointed at an unreachable endpoint with a short command_timeout fails
// each request within a fraction of a second, so the mapped-exception path of
// transaction, lock, members, status, lease grant/keep-alive/revoke/ttl is
// exercised without waiting on a real cluster.
func TestEtcdOperationErrors(t *testing.T) {
	got := runSrc(t, `
require "etcd"
bad = Etcdv3.new(endpoints: "http://127.0.0.1:1", dial_timeout: 0.3, command_timeout: 0.3)
r = []
def fails(r, label)
  yield
  r << "#{label}:noraise"
rescue Etcd::Error
  r << "#{label}:ok"
end
fails(r, "put")        { bad.put("k", "v") }
fails(r, "get")        { bad.get("k") }
fails(r, "del")        { bad.del("k") }
fails(r, "exists")     { bad.exists?("k") }
fails(r, "transaction"){ bad.transaction { |t| t.success = [t.put("k", "v")] } }
fails(r, "lease_grant"){ bad.lease_grant(10) }
fails(r, "keep_alive") { bad.lease_keep_alive(1) }
fails(r, "revoke")     { bad.lease_revoke(1) }
fails(r, "lease_ttl")  { bad.lease_ttl(1) }
fails(r, "lock")       { bad.lock("n", 10) }
fails(r, "members")    { bad.members }
fails(r, "status")     { bad.status("127.0.0.1:1") }
bad.close
puts r.join("\n")
`)
	for _, label := range []string{"put", "get", "del", "exists", "transaction",
		"lease_grant", "keep_alive", "revoke", "lease_ttl", "lock", "members", "status"} {
		if !strings.Contains(got, label+":ok") {
			t.Fatalf("TestEtcdOperationErrors: %s did not raise Etcd::Error\nfull:\n%s", label, got)
		}
	}
}

// TestEtcdConnectFailure covers the etcd.New error path: a client configured with
// credentials against an unreachable endpoint fails eager authentication within
// the dial timeout, which the binding re-raises as an Etcd error. It needs no
// server, so it runs without the embedded cluster.
func TestEtcdConnectFailure(t *testing.T) {
	got := runSrc(t, `
require "etcdv3"
begin
  Etcdv3.new(endpoints: "http://127.0.0.1:1", username: "u", password: "p", dial_timeout: 0.3)
  puts "noraise"
rescue Etcd::Error => e
  puts "etcd_error"
end
`)
	if got != "etcd_error" {
		t.Fatalf("TestEtcdConnectFailure: got %q", got)
	}
}

// TestEtcdRequireAlias proves require "etcd" and require "etcdv3" are both
// provided features (a second require reports already-loaded) and name the same
// classes.
func TestEtcdRequireAlias(t *testing.T) {
	got := runSrc(t, `
a = require "etcd"
b = require "etcd"
c = require "etcdv3"
puts a           # true (first load)
puts b           # false (already loaded)
puts c           # true or false depending on provided-feature identity
puts(Etcd::Client.equal?(Etcdv3::Client))   # true — same class object
puts(Etcd::Error.equal?(Etcdv3::Error))     # true
`)
	lines := strings.Split(got, "\n")
	if lines[0] != "true" {
		t.Fatalf("first require should be true:\n%s", got)
	}
	if !strings.Contains(got, "true\ntrue") {
		t.Fatalf("Etcd and Etcdv3 should share classes:\n%s", got)
	}
}
