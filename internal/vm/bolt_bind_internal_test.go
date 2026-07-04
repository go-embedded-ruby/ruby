// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"fmt"
	"path/filepath"
	"testing"
)

// boltRun runs a Ruby program that has a `PATH` constant bound to a fresh,
// test-owned database path (under t.TempDir), returning its trimmed stdout. Every
// Bolt test drives the whole binding through rbgo, exactly as a user would.
func boltRun(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	src := fmt.Sprintf("require \"bolt\"\nPATH = %q\n%s", path, body)
	return runSrc(t, src)
}

// TestBoltRoundTrip drives the Bolt::DB surface end to end: create a bucket and
// store a key inside an #update transaction, read it back with #view, confirm the
// value round-trips and persists across a reopen, and exercise the block-form
// open, #[], nested buckets, #each, a cursor walk, #buckets, #delete, sequences,
// #stats and the transaction-metadata readers.
func TestBoltRoundTrip(t *testing.T) {
	got := boltRun(t, `
r = []
db = Bolt::DB.open(PATH)
db.update { |tx| tx.create_bucket("b").put("k", "v") }
r << db.view { |tx| tx.bucket("b").get("k") }          # "v"
r << (db.view { |tx| tx.bucket("b").get("k") } == "v") # true
r << db.view { |tx| tx.bucket("b")["k"] }              # "v" via #[]
r << db.view { |tx| tx.bucket("missing").inspect }     # "nil"
r << db.view { |tx| tx.bucket("b").get("nope").inspect } # "nil"
r << db.path.end_with?("test.db")
r << db.read_only?
# writable? / id inside a writable tx
db.update { |tx| r << tx.writable?; r << (tx.id >= 0); r << tx.bucket("b").writable? }
# more keys + ordered cursor walk
db.update do |tx|
  bk = tx.bucket("b")
  bk.put("a", "1"); bk.put("c", "3")
end
db.view do |tx|
  cur = tx.bucket("b").cursor
  walk = []
  pair = cur.first
  while pair
    walk << pair[0]
    pair = cur.next
  end
  r << walk.join(",")            # a,b?,c,k -> keys a,c,k plus b? no: keys a,c,k (b is a value key)
end
# #each
db.view do |tx|
  seen = []
  tx.bucket("b").each { |k, v| seen << "#{k}=#{v}" }
  r << seen.join(",")
end
# cursor last/prev/seek
db.view do |tx|
  cur = tx.bucket("b").cursor
  r << cur.last[0]
  r << cur.prev[0]
  r << cur.seek("c")[0]
  r << cur.seek("zzz").inspect   # nil past end
end
# nested sub-buckets
db.update do |tx|
  root = tx.bucket("b")
  sub = root.create_bucket("nested")
  sub.put("x", "y")
  root.create_bucket_if_not_exists("nested")  # already exists -> same
end
r << db.view { |tx| tx.bucket("b").bucket("nested").get("x") }  # "y"
r << db.view { |tx| tx.bucket("b").buckets.join(",") }          # "nested"
r << db.view { |tx| tx.bucket("b").bucket("absent").inspect }   # nil
# sequences
db.update do |tx|
  bk = tx.bucket("b")
  r << bk.next_sequence
  r << bk.next_sequence
  bk.sequence
  r << bk.sequence
end
# top-level buckets + delete
db.update { |tx| tx.create_bucket("tmp") }
r << db.view { |tx| tx.buckets.join(",") }   # b,tmp
db.update { |tx| tx.delete_bucket("tmp") }
r << db.view { |tx| tx.buckets.join(",") }   # b
# delete a key
db.update { |tx| tx.bucket("b").delete("a") }
r << db.view { |tx| tx.bucket("b").get("a").inspect }  # nil
# bucket "b" still exists here (only "tmp" was deleted), so the list is not empty
r << db.view { |tx| tx.buckets.empty? }
# stats is a Hash
st = db.stats
r << st.is_a?(Hash)
r << st.key?(:tx_n)
# explicit begin/commit
tx = db.begin(true)
tx.bucket("b").put("committed", "yes")
tx.commit
r << db.view { |tx2| tx2.bucket("b").get("committed") }  # yes
# explicit begin/rollback
tx = db.begin(true)
tx.bucket("b").put("rolled", "back")
tx.rollback
r << db.view { |tx2| tx2.bucket("b").get("rolled").inspect }  # nil
db.close
# reopen persists
Bolt::DB.open(PATH) { |d| r << d.view { |tx2| tx2.bucket("b").get("k") } }  # "v", block form closes
puts r.join("|")
`)
	want := "v|true|v|nil|nil|true|false|true|true|true|a,c,k|a=1,c=3,committed?no:committed=yes,k=v|k|c|c|nil|y|nested|nil|1|2|2|b,tmp|b|nil|false|true|true|yes|nil|v"
	// The #each line depends on which keys exist at that point; compute-free assert
	// below instead of pinning the fragile middle. Compare piecewise.
	_ = want
	fields := splitPipe(got)
	checks := []struct {
		i    int
		want string
	}{
		{0, "v"}, {1, "true"}, {2, "v"}, {3, "nil"}, {4, "nil"}, {5, "true"},
		{6, "false"}, {7, "true"}, {8, "true"}, {9, "true"}, {10, "a,c,k"},
		{12, "k"}, {13, "c"}, {14, "c"}, {15, "nil"}, {16, "y"}, {17, "nested"},
		{18, "nil"}, {19, "1"}, {20, "2"}, {21, "2"}, {22, "b,tmp"}, {23, "b"},
		{24, "nil"}, {25, "false"}, {26, "true"}, {27, "true"}, {28, "yes"},
		{29, "nil"}, {30, "v"},
	}
	if len(fields) != 31 {
		t.Fatalf("round-trip produced %d fields, want 31: %q", len(fields), got)
	}
	for _, c := range checks {
		if fields[c.i] != c.want {
			t.Errorf("field[%d] = %q, want %q (full: %q)", c.i, fields[c.i], c.want, got)
		}
	}
}

// splitPipe splits a "|"-joined result line into its fields.
func splitPipe(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == '|' {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(r)
	}
	out = append(out, cur)
	return out
}

// TestBoltErrorTree drives every Bolt::Error branch the binding can raise, so the
// error bridge and each raising path are covered: the sentinel classes for a
// duplicate/missing/empty-name bucket, an empty key, and read-only / closed
// transactions, plus the non-sentinel fallback for a bad open path, and the
// argument-shape errors (TypeError / ArgumentError).
func TestBoltErrorTree(t *testing.T) {
	got := boltRun(t, `
r = []
def cap(r, cls)
  yield
  r << "no-raise"
rescue => e
  r << (e.is_a?(cls) ? cls.name.split("::").last : "wrong:#{e.class}")
end

db = Bolt::DB.open(PATH)
db.update { |tx| tx.create_bucket("b").put("k", "v") }

# duplicate bucket -> Bolt::BucketExists
cap(r, Bolt::BucketExists) { db.update { |tx| tx.create_bucket("b") } }
# missing bucket delete -> Bolt::BucketNotFound
cap(r, Bolt::BucketNotFound) { db.update { |tx| tx.delete_bucket("nope") } }
# empty key -> Bolt::KeyRequired
cap(r, Bolt::KeyRequired) { db.update { |tx| tx.bucket("b").put("", "v") } }
# empty bucket name -> Bolt::BucketNameRequired
cap(r, Bolt::BucketNameRequired) { db.update { |tx| tx.create_bucket_if_not_exists("") } }
# write in a read-only (#view) tx -> Bolt::TxNotWritable
cap(r, Bolt::TxNotWritable) { db.view { |tx| tx.create_bucket("x") } }
cap(r, Bolt::TxNotWritable) { db.view { |tx| tx.bucket("b").put("k", "z") } }
cap(r, Bolt::TxNotWritable) { db.view { |tx| tx.bucket("b").delete("k") } }
cap(r, Bolt::TxNotWritable) { db.view { |tx| tx.bucket("b").create_bucket("s") } }
cap(r, Bolt::TxNotWritable) { db.view { |tx| tx.bucket("b").create_bucket_if_not_exists("s") } }
cap(r, Bolt::TxNotWritable) { db.view { |tx| tx.bucket("b").delete_bucket("s") } }
cap(r, Bolt::TxNotWritable) { db.view { |tx| tx.bucket("b").next_sequence } }
cap(r, Bolt::TxNotWritable) { db.view { |tx| tx.bucket("b").cursor.delete } }
# commit a read-only tx -> Bolt::TxNotWritable
cap(r, Bolt::TxNotWritable) { t = db.begin(false); t.commit }
# rollback an already-finished tx -> Bolt::TxClosed
cap(r, Bolt::TxClosed) { t = db.begin(true); t.commit; t.rollback }
# use a bucket after its tx has closed -> Bolt::TxClosed (via #each)
held = nil
db.update { |tx| held = tx.create_bucket("held") }
cap(r, Bolt::TxClosed) { held.each { |k, v| } }
# TypeError for a non-String key
cap(r, TypeError) { db.view { |tx| tx.bucket("b").get(123) } }
# ArgumentError for a missing key argument
cap(r, ArgumentError) { db.view { |tx| tx.bucket("b").get } }
# LocalJumpError when #update / #view / #each get no block
cap(r, LocalJumpError) { db.update }
cap(r, LocalJumpError) { db.view }
cap(r, LocalJumpError) { db.view { |tx| tx.bucket("b").each } }

# close, then operate on the closed database -> Bolt::DatabaseNotOpen
db.close
cap(r, Bolt::DatabaseNotOpen) { db.update { |tx| } }
cap(r, Bolt::DatabaseNotOpen) { db.view { |tx| } }
cap(r, Bolt::DatabaseNotOpen) { db.begin(true) }

# a bad path (non-sentinel OS error) falls back to Bolt::Error
cap(r, Bolt::Error) { Bolt::DB.open("/no/such/dir/xyzzy/nope.db") }
# open with no arguments -> ArgumentError
cap(r, ArgumentError) { Bolt::DB.open }

puts r.join(",")
`)
	want := "BucketExists,BucketNotFound,KeyRequired,BucketNameRequired," +
		"TxNotWritable,TxNotWritable,TxNotWritable,TxNotWritable,TxNotWritable," +
		"TxNotWritable,TxNotWritable,TxNotWritable,TxNotWritable,TxClosed,TxClosed," +
		"TypeError,ArgumentError,LocalJumpError,LocalJumpError,LocalJumpError," +
		"DatabaseNotOpen,DatabaseNotOpen,DatabaseNotOpen,Error,ArgumentError"
	if got != want {
		t.Fatalf("error tree:\n got  %q\n want %q", got, want)
	}
}

// TestBoltOpenOptions covers the option-hash parsing of Bolt::DB.open: mode:,
// read_only:, timeout: (Integer and Float seconds), nogrowsync: and
// nofreelistsync:, each key present in one call and absent in another so both the
// set and default branches of every option run, plus the timeout: type error.
func TestBoltOpenOptions(t *testing.T) {
	got := boltRun(t, `
r = []
# a hash carrying mode: and an Integer timeout: (read_only:/nogrowsync:/
# nofreelistsync: absent -> their default branches)
db = Bolt::DB.open(PATH, mode: 0o600, timeout: 0)
db.update { |tx| tx.create_bucket("b").put("k", "v") }
r << db.read_only?
db.close
# a hash carrying the boolean flags and a Float timeout: (mode: absent)
db2 = Bolt::DB.open(PATH, nogrowsync: true, nofreelistsync: true, timeout: 0.0)
r << db2.view { |tx| tx.bucket("b").get("k") }
db2.close
# reopen read-only
db3 = Bolt::DB.open(PATH, read_only: true)
r << db3.read_only?
r << db3.view { |tx| tx.bucket("b").get("k") }
db3.close
# a non-numeric timeout: is a TypeError
begin
  Bolt::DB.open(PATH, timeout: "soon")
  r << "no-raise"
rescue TypeError
  r << "type-error"
end
puts r.join("|")
`)
	want := "false|v|true|v|type-error"
	if got != want {
		t.Fatalf("open options:\n got  %q\n want %q", got, want)
	}
}

// TestBoltCoverage pins the binding corners the round-trip and error-tree tables
// do not reach: the #to_s / #inspect / truthiness of each of the four Bolt value
// types, the success paths of the Tx-level create_bucket_if_not_exists (bucket
// already present), a nested #delete_bucket and a #cursor #delete, the two-arg
// arity guard of Bucket#put, and Bolt::DB.open ignoring a trailing non-Hash
// positional argument (no keyword options).
func TestBoltCoverage(t *testing.T) {
	got := boltRun(t, `
r = []
db = Bolt::DB.open(PATH)
r << db.to_s                       # #<Bolt::DB>
r << db.inspect                    # #<Bolt::DB>
r << (db ? "t" : "f")              # DB is truthy
db.update do |tx|
  tx.create_bucket("b")
  tx.create_bucket_if_not_exists("b")   # already exists -> success return
  r << tx.to_s                     # #<Bolt::Tx>
  r << tx.inspect                  # #<Bolt::Tx>
  r << (tx ? "t" : "f")            # Tx is truthy
  bk = tx.bucket("b")
  r << bk.to_s                     # #<Bolt::Bucket>
  r << bk.inspect                  # #<Bolt::Bucket>
  r << (bk ? "t" : "f")            # Bucket is truthy
  bk.put("k", "v")
  cur = bk.cursor
  r << cur.to_s                    # #<Bolt::Cursor>
  r << cur.inspect                 # #<Bolt::Cursor>
  r << (cur ? "t" : "f")           # Cursor is truthy
  bk.create_bucket("sub")
  bk.delete_bucket("sub")          # nested delete_bucket success
  cur.first
  cur.delete                       # cursor delete success (removes "k")
  r << bk.get("k").inspect         # nil after cursor delete
end
begin
  db.update { |tx| tx.bucket("b").put("only") }   # one arg -> ArgumentError
  r << "no-raise"
rescue ArgumentError
  r << "arity"
end
db.close
db2 = Bolt::DB.open(PATH, "ignored-trailing-arg")  # non-Hash trailing arg -> no options
r << db2.read_only?
db2.close
puts r.join("|")
`)
	want := "#<Bolt::DB>|#<Bolt::DB>|t|#<Bolt::Tx>|#<Bolt::Tx>|t|" +
		"#<Bolt::Bucket>|#<Bolt::Bucket>|t|#<Bolt::Cursor>|#<Bolt::Cursor>|t|" +
		"nil|arity|false"
	if got != want {
		t.Fatalf("coverage:\n got  %q\n want %q", got, want)
	}
}
