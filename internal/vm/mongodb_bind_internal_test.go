// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"
	"testing"

	mongodb "github.com/go-ruby-mongodb/mongodb"
)

// The Mongo binding is driven through rbgo against an in-process fake that
// satisfies the mongoClientAPI / mongoCollectionAPI / mongoCursorAPI seam, so the
// deterministic suite runs the real driver's argument coercion, BSON encode /
// decode, ObjectId and Time round-tripping, result mapping and error taxonomy
// with no live mongod, no socket and no leaked goroutine. A separate test drives
// the real library forwarders (realMongo*) against an unreachable address with a
// 1 ms server-selection timeout so every production line runs too.

// fakeMongoCursor is an in-memory cursor: ToArray returns its canned documents (or
// a canned drain error) and Close is a no-op.
type fakeMongoCursor struct {
	docs []mongodb.Document
	err  error
}

func (f *fakeMongoCursor) ToArray() ([]mongodb.Document, error) { return f.docs, f.err }
func (f *fakeMongoCursor) Close() error                         { return nil }

// fakeMongoColl is an in-memory collection: it keeps a document store so an
// insert followed by a find round-trips through both value bridges, and an errs
// map lets a test make any single operation return a canned error.
type fakeMongoColl struct {
	name  string
	store []mongodb.Document
	errs  map[string]error
}

func (f *fakeMongoColl) err(op string) error { return f.errs[op] }

func (f *fakeMongoColl) Name() string { return f.name }

func (f *fakeMongoColl) InsertOne(doc mongodb.Document) (*mongodb.InsertOneResult, error) {
	if e := f.err("insert_one"); e != nil {
		return nil, e
	}
	f.store = append(f.store, doc)
	id := mongodb.NewObjectId()
	if v, ok := doc.Get("_id"); ok {
		return &mongodb.InsertOneResult{InsertedID: v}, nil
	}
	return &mongodb.InsertOneResult{InsertedID: id}, nil
}

func (f *fakeMongoColl) InsertMany(docs []mongodb.Document) (*mongodb.InsertManyResult, error) {
	if e := f.err("insert_many"); e != nil {
		return nil, e
	}
	out := &mongodb.InsertManyResult{}
	for range docs {
		out.InsertedIDs = append(out.InsertedIDs, mongodb.NewObjectId())
	}
	f.store = append(f.store, docs...)
	return out, nil
}

func (f *fakeMongoColl) Find(_ mongodb.Document, _ *mongodb.FindOptions) (mongoCursorAPI, error) {
	if e := f.err("find"); e != nil {
		return nil, e
	}
	return &fakeMongoCursor{docs: f.store, err: f.err("drain")}, nil
}

func (f *fakeMongoColl) FindOne(_ mongodb.Document, _ *mongodb.FindOptions) (mongodb.Document, error) {
	if e := f.err("find_one"); e != nil {
		return nil, e
	}
	if len(f.store) == 0 {
		return nil, nil
	}
	return f.store[0], nil
}

func (f *fakeMongoColl) UpdateOne(_, _ mongodb.Document, _ *mongodb.UpdateOptions) (*mongodb.UpdateResult, error) {
	return f.updateResult("update_one")
}
func (f *fakeMongoColl) UpdateMany(_, _ mongodb.Document, _ *mongodb.UpdateOptions) (*mongodb.UpdateResult, error) {
	return f.updateResult("update_many")
}
func (f *fakeMongoColl) ReplaceOne(_, _ mongodb.Document, _ *mongodb.UpdateOptions) (*mongodb.UpdateResult, error) {
	return f.updateResult("replace_one")
}

func (f *fakeMongoColl) updateResult(op string) (*mongodb.UpdateResult, error) {
	if e := f.err(op); e != nil {
		return nil, e
	}
	return &mongodb.UpdateResult{
		MatchedCount:  1,
		ModifiedCount: 1,
		UpsertedCount: 0,
		UpsertedID:    mongodb.NewObjectId(),
	}, nil
}

func (f *fakeMongoColl) DeleteOne(_ mongodb.Document) (*mongodb.DeleteResult, error) {
	return f.deleteResult("delete_one")
}
func (f *fakeMongoColl) DeleteMany(_ mongodb.Document) (*mongodb.DeleteResult, error) {
	return f.deleteResult("delete_many")
}

func (f *fakeMongoColl) deleteResult(op string) (*mongodb.DeleteResult, error) {
	if e := f.err(op); e != nil {
		return nil, e
	}
	return &mongodb.DeleteResult{DeletedCount: 2}, nil
}

func (f *fakeMongoColl) CountDocuments(_ mongodb.Document, _ *mongodb.CountOptions) (int64, error) {
	if e := f.err("count_documents"); e != nil {
		return 0, e
	}
	return int64(len(f.store)), nil
}

func (f *fakeMongoColl) Aggregate(_ []mongodb.Document) (mongoCursorAPI, error) {
	if e := f.err("aggregate"); e != nil {
		return nil, e
	}
	return &fakeMongoCursor{docs: f.store}, nil
}

func (f *fakeMongoColl) Distinct(field string, _ mongodb.Document) ([]any, error) {
	if e := f.err("distinct"); e != nil {
		return nil, e
	}
	return []any{"a", int32(1), field}, nil
}

func (f *fakeMongoColl) CreateIndex(_ mongodb.Document, opts *mongodb.IndexOptions) (string, error) {
	if e := f.err("create_index"); e != nil {
		return "", e
	}
	if opts != nil && opts.Name != "" {
		return opts.Name, nil
	}
	return "idx_1", nil
}

func (f *fakeMongoColl) Indexes() ([]mongodb.Document, error) {
	if e := f.err("indexes"); e != nil {
		return nil, e
	}
	return []mongodb.Document{mongodb.Doc("name", "_id_"), mongodb.Doc("name", "idx_1")}, nil
}

// fakeMongoDatabase and fakeMongoClient hand out the one shared fake collection.
type fakeMongoDatabase struct {
	name string
	coll *fakeMongoColl
}

func (f fakeMongoDatabase) Name() string                         { return f.name }
func (f fakeMongoDatabase) Collection(string) mongoCollectionAPI { return f.coll }

type fakeMongoClient struct {
	dbName string
	coll   *fakeMongoColl
	closed *bool
}

func (f fakeMongoClient) Database(name string) mongoDatabaseAPI {
	if name == "" {
		name = f.dbName
	}
	return fakeMongoDatabase{name: name, coll: f.coll}
}
func (f fakeMongoClient) Collection(string) mongoCollectionAPI { return f.coll }
func (f fakeMongoClient) Close() error {
	*f.closed = true
	return nil
}

// withFakeMongo installs a fake-returning mongoDial for the duration of the test.
func withFakeMongo(t *testing.T, coll *fakeMongoColl) (*bool, func()) {
	t.Helper()
	closed := false
	orig := mongoDial
	mongoDial = func(uri string, opts ...mongodb.Option) (mongoClientAPI, error) {
		return fakeMongoClient{dbName: "testdb", coll: coll, closed: &closed}, nil
	}
	return &closed, func() { mongoDial = orig }
}

// mongoRun drives a Ruby program against the fake, returning its trimmed stdout.
func mongoRun(t *testing.T, coll *fakeMongoColl, body string) string {
	t.Helper()
	_, restore := withFakeMongo(t, coll)
	defer restore()
	return runSrc(t, "require \"mongo\"\n"+body)
}

func newFakeColl() *fakeMongoColl {
	return &fakeMongoColl{name: "things", errs: map[string]error{}}
}

// opErr is a library *mongodb.Error of the given class, as the library returns.
func opErr(class mongodb.ErrorClass) error {
	return &mongodb.Error{Class: class, Message: string(class) + " happened"}
}

func TestMongoInsertFindRoundTrip(t *testing.T) {
	got := mongoRun(t, newFakeColl(), `
client = Mongo::Client.new("mongodb://localhost:27017", database: "testdb", app_name: "rbgo")
coll = client[:things]
res = coll.insert_one({"name" => "Ada", "age" => 36})
puts res.inserted_id.class
doc = coll.find.first
puts doc["name"]
puts doc["age"]
puts client.database.name
puts coll.name
client.close
`)
	want := "BSON::ObjectId\nAda\n36\ntestdb\nthings"
	if got != want {
		t.Fatalf("round trip:\n got %q\nwant %q", got, want)
	}
}

func TestMongoBSONTypeRoundTrip(t *testing.T) {
	got := mongoRun(t, newFakeColl(), `
coll = Mongo::Client.new("mongodb://x").database.collection("things")
oid = BSON::ObjectId.new
coll.insert_one({
  "_id" => oid,
  "s" => "str", "i32" => 7, "i64" => 5_000_000_000, "f" => 1.5,
  "b" => true, "nil" => nil, "sym" => :green,
  "arr" => [1, "two", [3]],
  "nested" => {"k" => "v"},
  "t" => Time.at(1000),
})
d = coll.find_one
puts d["_id"] == oid
puts d["s"]
puts d["i32"]
puts d["i64"]
puts d["f"]
puts d["b"]
puts d["nil"].inspect
puts d["sym"]
puts d["arr"].inspect
puts d["nested"]["k"]
puts d["t"].class
`)
	want := "true\nstr\n7\n5000000000\n1.5\ntrue\nnil\ngreen\n[1, \"two\", [3]]\nv\nTime"
	if got != want {
		t.Fatalf("bson round trip:\n got %q\nwant %q", got, want)
	}
}

func TestMongoDecodeUnmodelledType(t *testing.T) {
	fc := newFakeColl()
	fc.store = []mongodb.Document{mongodb.Doc("bin", mongodb.Binary{Subtype: 0, Data: []byte("hi")}, "n", 3)}
	got := mongoRun(t, fc, `
d = Mongo::Client.new("mongodb://x")[:things].find_one
puts d["bin"].class
puts d["n"]
`)
	if got == "" {
		t.Fatalf("expected rendered binary + int, got empty")
	}
}

func TestMongoAllWrites(t *testing.T) {
	got := mongoRun(t, newFakeColl(), `
coll = Mongo::Client.new("mongodb://x")[:things]
im = coll.insert_many([{a: 1}, {a: 2}])
puts im.inserted_ids.length
puts im.inserted_count
u = coll.update_one({a: 1}, {"$set" => {a: 9}}, upsert: true)
puts [u.matched_count, u.modified_count, u.upserted_count].inspect
puts u.upserted_id.class
coll.update_many({a: 1}, {"$set" => {a: 9}})
coll.replace_one({a: 1}, {a: 2})
d1 = coll.delete_one({a: 1})
puts d1.deleted_count
puts coll.delete_many({}).deleted_count
puts coll.count_documents
puts coll.count_documents({a: 1}, limit: 5, skip: 1)
puts coll.distinct("a").inspect
puts coll.distinct("a", {b: 2}).inspect
puts coll.create_index({a: 1})
puts coll.create_index({a: 1}, name: "custom", unique: true)
puts coll.indexes.map { |i| i["name"] }.inspect
puts d1.matched_count.inspect
puts d1.ok?
`)
	want := "2\n2\n[1, 1, 0]\nBSON::ObjectId\n2\n2\n2\n2\n[\"a\", 1, \"a\"]\n[\"a\", 1, \"a\"]\nidx_1\ncustom\n[\"_id_\", \"idx_1\"]\nnil\ntrue"
	if got != want {
		t.Fatalf("writes:\n got %q\nwant %q", got, want)
	}
}

func TestMongoFindOptionsAndCursor(t *testing.T) {
	fc := newFakeColl()
	fc.store = []mongodb.Document{mongodb.Doc("i", 1), mongodb.Doc("i", 2)}
	got := mongoRun(t, fc, `
coll = Mongo::Client.new("mongodb://x")[:things]
cur = coll.find({i: {"$gte" => 1}}, sort: {i: 1}, projection: {i: 1}, limit: 10, skip: 0, batch_size: 100)
puts cur.to_a.length
puts cur.map { |d| d["i"] }.inspect
seen = []
coll.find.each { |d| seen << d["i"] }
puts seen.inspect
puts coll.find.first["i"]
puts coll.aggregate([{"$match" => {}}, {"$sort" => {i: 1}}]).map { |d| d["i"] }.inspect
`)
	want := "2\n[1, 2]\n[1, 2]\n1\n[1, 2]"
	if got != want {
		t.Fatalf("find options/cursor:\n got %q\nwant %q", got, want)
	}
}

func TestMongoCursorFirstEmpty(t *testing.T) {
	got := mongoRun(t, newFakeColl(), `
puts Mongo::Client.new("mongodb://x")[:things].find.first.inspect
`)
	if got != "nil" {
		t.Fatalf("empty first: got %q want nil", got)
	}
}

func TestMongoErrorTaxonomy(t *testing.T) {
	cases := []struct {
		op   string
		ruby string
		want string
	}{
		{"insert_one", `c.insert_one({a: 1})`, "Mongo::Error::OperationFailure"},
		{"insert_many", `c.insert_many([{a: 1}])`, "Mongo::Error::BulkWriteError"},
		{"find", `c.find.to_a`, "Mongo::Error::ConnectionFailure"},
		{"drain", `c.find.to_a`, "Mongo::Error::OperationFailure"},
		{"find_one", `c.find_one`, "Mongo::Error::OperationFailure"},
		{"update_one", `c.update_one({a: 1}, {a: 2})`, "Mongo::Error::OperationFailure"},
		{"update_many", `c.update_many({a: 1}, {a: 2})`, "Mongo::Error::OperationFailure"},
		{"replace_one", `c.replace_one({a: 1}, {a: 2})`, "Mongo::Error::OperationFailure"},
		{"delete_one", `c.delete_one({a: 1})`, "Mongo::Error::OperationFailure"},
		{"delete_many", `c.delete_many({a: 1})`, "Mongo::Error::OperationFailure"},
		{"count_documents", `c.count_documents`, "Mongo::Error::OperationFailure"},
		{"aggregate", `c.aggregate([{"$match" => {}}])`, "Mongo::Error::NoServerAvailable"},
		{"distinct", `c.distinct("a")`, "Mongo::Error::OperationFailure"},
		{"create_index", `c.create_index({a: 1})`, "Mongo::Error::OperationFailure"},
		{"indexes", `c.indexes`, "Mongo::Error::InvalidDocument"},
	}
	for _, tc := range cases {
		t.Run(tc.op, func(t *testing.T) {
			fc := newFakeColl()
			// Derive the class each op should map to from the expected name.
			fc.errs[tc.op] = opErr(mongodb.ErrorClass(tc.want))
			got := mongoRun(t, fc, `
c = Mongo::Client.new("mongodb://x")[:things]
begin
  `+tc.ruby+`
  puts "no-raise"
rescue Mongo::Error => e
  puts e.class
  puts e.message
end
`)
			lines := got
			if lines == "no-raise" {
				t.Fatalf("%s did not raise", tc.op)
			}
			if got == "" {
				t.Fatalf("%s empty output", tc.op)
			}
			// First line is the class name.
			if want := tc.want; !hasFirstLine(got, want) {
				t.Fatalf("%s: got %q want first line %q", tc.op, got, want)
			}
		})
	}
}

func TestMongoErrorFallback(t *testing.T) {
	fc := newFakeColl()
	fc.errs["insert_one"] = errors.New("raw non-mongo error")
	got := mongoRun(t, fc, `
begin
  Mongo::Client.new("mongodb://x")[:things].insert_one({a: 1})
rescue Mongo::Error => e
  puts e.class
end
`)
	if got != "Mongo::Error" {
		t.Fatalf("fallback: got %q want Mongo::Error", got)
	}
}

func TestMongoArgumentErrors(t *testing.T) {
	cases := []struct{ ruby, wantClass string }{
		{`Mongo::Client.new`, "ArgumentError"},
		{`Mongo::Client.new("mongodb://x")[]`, "ArgumentError"},
		{`Mongo::Client.new("mongodb://x").database.collection`, "ArgumentError"},
		{`Mongo::Client.new("mongodb://x")[:c].insert_one`, "ArgumentError"},
		{`Mongo::Client.new("mongodb://x")[:c].insert_one(5)`, "TypeError"},
		{`Mongo::Client.new("mongodb://x")[:c].insert_many`, "ArgumentError"},
		{`Mongo::Client.new("mongodb://x")[:c].insert_many(5)`, "TypeError"},
		{`Mongo::Client.new("mongodb://x")[:c].insert_many([5])`, "TypeError"},
		{`Mongo::Client.new("mongodb://x")[:c].find(5)`, "TypeError"},
		{`Mongo::Client.new("mongodb://x")[:c].find({}, sort: 5)`, "TypeError"},
		{`Mongo::Client.new("mongodb://x")[:c].distinct`, "ArgumentError"},
		{`Mongo::Client.new("mongodb://x")[:c].insert_one({1 => 2})`, "Mongo::Error::InvalidDocument"},
		{`Mongo::Client.new("mongodb://x")[:c].insert_one({a: Object.new})`, "Mongo::Error::InvalidDocument"},
		{`Mongo::Client.new("mongodb://x")[:c].find.each`, "LocalJumpError"},
		{`Mongo::Client.new("mongodb://x")[:c].find.map`, "LocalJumpError"},
	}
	for _, tc := range cases {
		t.Run(tc.wantClass+"_"+tc.ruby, func(t *testing.T) {
			got := mongoRun(t, newFakeColl(), `
begin
  `+tc.ruby+`
  puts "no-raise"
rescue Exception => e
  puts e.class
end
`)
			if !hasFirstLine(got, tc.wantClass) {
				t.Fatalf("got %q want first line %q", got, tc.wantClass)
			}
		})
	}
}

func TestMongoObjectId(t *testing.T) {
	got := mongoRun(t, newFakeColl(), `
a = BSON::ObjectId.from_string("64b8f1e2c3d4e5f6a7b8c9d0")
puts a.to_s
puts a.inspect
b = BSON::ObjectId.from_string("64b8f1e2c3d4e5f6a7b8c9d0")
puts a.hash.is_a?(Integer)
puts a.hash == b.hash
puts a == b
puts(a == BSON::ObjectId.new)
puts(a == "nope")
c = BSON::ObjectId.new("64b8f1e2c3d4e5f6a7b8c9d0")
puts c.to_s
`)
	want := "64b8f1e2c3d4e5f6a7b8c9d0\nBSON::ObjectId('64b8f1e2c3d4e5f6a7b8c9d0')\ntrue\ntrue\ntrue\nfalse\nfalse\n64b8f1e2c3d4e5f6a7b8c9d0"
	if got != want {
		t.Fatalf("objectid:\n got %q\nwant %q", got, want)
	}
}

func TestMongoObjectIdErrors(t *testing.T) {
	got := mongoRun(t, newFakeColl(), `
begin
  BSON::ObjectId.from_string("nothex")
rescue Mongo::Error::InvalidDocument => e
  puts "invalid"
end
begin
  BSON::ObjectId.from_string
rescue ArgumentError
  puts "argerr"
end
`)
	if got != "invalid\nargerr" {
		t.Fatalf("objectid errors: got %q", got)
	}
}

func TestMongoClientDatabaseNamed(t *testing.T) {
	closed := mongoRunClosed(t, newFakeColl(), `
c = Mongo::Client.new("mongodb://x")
puts c.database("other").name
puts c.database.name
c.close
`)
	if !closed {
		t.Fatalf("expected client closed")
	}
}

// mongoRunClosed drives a program and reports whether the fake client was closed.
func mongoRunClosed(t *testing.T, coll *fakeMongoColl, body string) bool {
	t.Helper()
	closed, restore := withFakeMongo(t, coll)
	defer restore()
	out := runSrc(t, "require \"mongo\"\n"+body)
	if out != "other\ntestdb" {
		t.Fatalf("database named: got %q", out)
	}
	return *closed
}

func TestMongoValueReprAndTruthiness(t *testing.T) {
	got := mongoRun(t, newFakeColl(), `
c = Mongo::Client.new("mongodb://x")
db = c.database
coll = c[:things]
res = coll.insert_one({a: 1})
cur = coll.find
oid = BSON::ObjectId.new
[c, db, coll, res, cur, oid].each do |o|
  print(o ? "t" : "f")
  print "|"
  print o.to_s.empty? ? "empty" : "s"
  print "|"
  print o.inspect.empty? ? "empty" : "i"
  puts
end
`)
	// Six wrappers, each truthy with a non-empty to_s and inspect.
	want := "t|s|i\nt|s|i\nt|s|i\nt|s|i\nt|s|i\nt|s|i"
	if got != want {
		t.Fatalf("repr/truthiness:\n got %q\nwant %q", got, want)
	}
}

func TestMongoNilFilterAndBadName(t *testing.T) {
	got := mongoRun(t, newFakeColl(), `
c = Mongo::Client.new("mongodb://x")
coll = c[:things]
puts coll.find(nil).to_a.length          # nil filter -> empty document
puts coll.find_one.inspect               # no match -> nil
puts coll.count_documents(nil)           # nil filter path
puts coll.find({}, 5).to_a.length        # non-Hash options ignored
begin
  c[5]
rescue TypeError
  puts "name-type"
end
`)
	want := "0\nnil\n0\n0\nname-type"
	if got != want {
		t.Fatalf("nil filter/bad name:\n got %q\nwant %q", got, want)
	}
}

// TestMongoRealForwarding drives every realMongo* forwarder against an
// unreachable address with a 1 ms server-selection timeout, so the production
// lines run and return quickly. The client is disconnected on cleanup, leaving no
// goroutine behind.
func TestMongoRealForwarding(t *testing.T) {
	uri := "mongodb://127.0.0.1:1/?serverSelectionTimeoutMS=1&connectTimeoutMS=1"
	cli, err := mongoDial(uri, mongodb.WithDatabase("testdb"), mongodb.AppName("rbgo"))
	if err != nil {
		t.Fatalf("mongoDial: %v", err)
	}
	t.Cleanup(func() { _ = cli.Close() })

	db := cli.Database("testdb")
	if db.Name() != "testdb" {
		t.Fatalf("db name = %q", db.Name())
	}
	if got := cli.Collection("things").Name(); got != "things" {
		t.Fatalf("client collection name = %q", got)
	}
	coll := db.Collection("things")
	if coll.Name() != "things" {
		t.Fatalf("db collection name = %q", coll.Name())
	}

	doc := mongodb.Doc("x", int32(1))
	// Every operation dials and returns a quick server-selection error; the point
	// is to run the forwarder, not to succeed.
	_, _ = coll.InsertOne(doc)
	_, _ = coll.InsertMany([]mongodb.Document{doc})
	_, _ = coll.Find(doc, nil)
	_, _ = coll.FindOne(doc, nil)
	_, _ = coll.UpdateOne(doc, doc, nil)
	_, _ = coll.UpdateMany(doc, doc, nil)
	_, _ = coll.ReplaceOne(doc, doc, nil)
	_, _ = coll.DeleteOne(doc)
	_, _ = coll.DeleteMany(doc)
	_, _ = coll.CountDocuments(doc, nil)
	_, _ = coll.Aggregate([]mongodb.Document{doc})
	_, _ = coll.Distinct("x", doc)
	_, _ = coll.CreateIndex(doc, nil)
	_, _ = coll.Indexes()
}

// TestMongoDialBadURI covers mongoDial's error return and Client.new's re-raise
// through the real library (no fake), with a malformed connection string.
func TestMongoDialBadURI(t *testing.T) {
	got := runSrc(t, `require "mongo"
begin
  Mongo::Client.new("not-a-mongodb-uri://::")
  puts "no-raise"
rescue Mongo::Error => e
  puts "raised"
end`)
	if got != "raised" {
		t.Fatalf("bad uri: got %q want raised", got)
	}
}

// hasFirstLine reports whether s's first line equals want.
func hasFirstLine(s, want string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			return s[:i] == want
		}
	}
	return s == want
}
