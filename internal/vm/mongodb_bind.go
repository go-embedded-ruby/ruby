// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"
	"fmt"
	"math"
	stdtime "time"

	gotime "github.com/go-composites/time/src"
	mongodb "github.com/go-ruby-mongodb/mongodb"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin binding between rbgo's Ruby object graph and the
// interpreter-independent MongoDB client of github.com/go-ruby-mongodb/mongodb — a
// pure-Go (CGO=0) MRI-faithful facade over go.mongodb.org/mongo-driver/v2, the
// database's own driver and canonical BSON codec. It carries the instance value
// types the Mongo module wraps (a client, a database, a collection, a cursor, an
// operation result and a BSON::ObjectId), the BSON <-> Ruby value bridge, the
// Mongo::Error re-raise, and the connection seam. See mongodb.go for the class
// and method wiring.
//
// # The connection seam (why an interface, not mtest)
//
// Everything the binding does — argument coercion, BSON encode/decode, ObjectId
// and Time round-tripping, result-to-Ruby mapping and the error taxonomy — is
// deterministic and needs no server. The driver's mtest mock deployment can only
// be injected through the library's unexported withDeployment option, so it is
// unreachable from this package. The binding therefore talks to the small
// mongoClientAPI / mongoCollectionAPI / mongoCursorAPI interfaces, satisfied in
// production by thin forwarders over the real library types (realMongo*) and in
// the deterministic suite by in-process fakes. A fake has no client, no
// goroutine and no socket, so the suite touches no external mongod and leaks
// nothing; the mongoDial seam is the single point where a fake replaces the real
// library. The real forwarders are exercised (against an unreachable address with
// a 1 ms server-selection timeout) so every production line runs too.

// mongoClientAPI is the subset of the library's *mongodb.Client the binding uses.
type mongoClientAPI interface {
	Database(name string) mongoDatabaseAPI
	Collection(name string) mongoCollectionAPI
	Close() error
}

// mongoDatabaseAPI is the subset of the library's *mongodb.Database the binding
// uses.
type mongoDatabaseAPI interface {
	Name() string
	Collection(name string) mongoCollectionAPI
}

// mongoCollectionAPI is the subset of the library's *mongodb.Collection the
// binding uses — the gem's CRUD, aggregation and index surface.
type mongoCollectionAPI interface {
	Name() string
	InsertOne(doc mongodb.Document) (*mongodb.InsertOneResult, error)
	InsertMany(docs []mongodb.Document) (*mongodb.InsertManyResult, error)
	Find(filter mongodb.Document, opts *mongodb.FindOptions) (mongoCursorAPI, error)
	FindOne(filter mongodb.Document, opts *mongodb.FindOptions) (mongodb.Document, error)
	UpdateOne(filter, update mongodb.Document, opts *mongodb.UpdateOptions) (*mongodb.UpdateResult, error)
	UpdateMany(filter, update mongodb.Document, opts *mongodb.UpdateOptions) (*mongodb.UpdateResult, error)
	ReplaceOne(filter, replacement mongodb.Document, opts *mongodb.UpdateOptions) (*mongodb.UpdateResult, error)
	DeleteOne(filter mongodb.Document) (*mongodb.DeleteResult, error)
	DeleteMany(filter mongodb.Document) (*mongodb.DeleteResult, error)
	CountDocuments(filter mongodb.Document, opts *mongodb.CountOptions) (int64, error)
	Aggregate(pipeline []mongodb.Document) (mongoCursorAPI, error)
	Distinct(field string, filter mongodb.Document) ([]any, error)
	CreateIndex(keys mongodb.Document, opts *mongodb.IndexOptions) (string, error)
	Indexes() ([]mongodb.Document, error)
}

// mongoCursorAPI is the subset of the library's *mongodb.Cursor the binding uses.
// *mongodb.Cursor satisfies it directly (ToArray / Close), so the real Find /
// Aggregate forwarders need no wrapper branch.
type mongoCursorAPI interface {
	ToArray() ([]mongodb.Document, error)
	Close() error
}

// mongoDial opens a client, the single seam the deterministic suite overrides
// with a fake. In production it wraps the library's Mongo::Client.new.
var mongoDial = func(uri string, opts ...mongodb.Option) (mongoClientAPI, error) {
	c, err := mongodb.NewClient(uri, opts...)
	return realMongoClient{c}, err
}

// realMongoClient forwards to a library *mongodb.Client.
type realMongoClient struct{ c *mongodb.Client }

func (r realMongoClient) Database(name string) mongoDatabaseAPI {
	return realMongoDatabase{r.c.Database(name)}
}
func (r realMongoClient) Collection(name string) mongoCollectionAPI {
	return realMongoCollection{r.c.Collection(name)}
}
func (r realMongoClient) Close() error { return r.c.Close() }

// realMongoDatabase forwards to a library *mongodb.Database.
type realMongoDatabase struct{ d *mongodb.Database }

func (r realMongoDatabase) Name() string { return r.d.Name() }
func (r realMongoDatabase) Collection(name string) mongoCollectionAPI {
	return realMongoCollection{r.d.Collection(name)}
}

// realMongoCollection forwards to a library *mongodb.Collection. Every method is
// a single forward so an error-returning call covers the whole body.
type realMongoCollection struct{ c *mongodb.Collection }

func (r realMongoCollection) Name() string { return r.c.Name() }
func (r realMongoCollection) InsertOne(doc mongodb.Document) (*mongodb.InsertOneResult, error) {
	return r.c.InsertOne(doc)
}
func (r realMongoCollection) InsertMany(docs []mongodb.Document) (*mongodb.InsertManyResult, error) {
	return r.c.InsertMany(docs)
}
func (r realMongoCollection) Find(filter mongodb.Document, opts *mongodb.FindOptions) (mongoCursorAPI, error) {
	cur, err := r.c.Find(filter, opts)
	return cur, err
}
func (r realMongoCollection) FindOne(filter mongodb.Document, opts *mongodb.FindOptions) (mongodb.Document, error) {
	return r.c.FindOne(filter, opts)
}
func (r realMongoCollection) UpdateOne(filter, update mongodb.Document, opts *mongodb.UpdateOptions) (*mongodb.UpdateResult, error) {
	return r.c.UpdateOne(filter, update, opts)
}
func (r realMongoCollection) UpdateMany(filter, update mongodb.Document, opts *mongodb.UpdateOptions) (*mongodb.UpdateResult, error) {
	return r.c.UpdateMany(filter, update, opts)
}
func (r realMongoCollection) ReplaceOne(filter, replacement mongodb.Document, opts *mongodb.UpdateOptions) (*mongodb.UpdateResult, error) {
	return r.c.ReplaceOne(filter, replacement, opts)
}
func (r realMongoCollection) DeleteOne(filter mongodb.Document) (*mongodb.DeleteResult, error) {
	return r.c.DeleteOne(filter)
}
func (r realMongoCollection) DeleteMany(filter mongodb.Document) (*mongodb.DeleteResult, error) {
	return r.c.DeleteMany(filter)
}
func (r realMongoCollection) CountDocuments(filter mongodb.Document, opts *mongodb.CountOptions) (int64, error) {
	return r.c.CountDocuments(filter, opts)
}
func (r realMongoCollection) Aggregate(pipeline []mongodb.Document) (mongoCursorAPI, error) {
	cur, err := r.c.Aggregate(pipeline)
	return cur, err
}
func (r realMongoCollection) Distinct(field string, filter mongodb.Document) ([]any, error) {
	return r.c.Distinct(field, filter)
}
func (r realMongoCollection) CreateIndex(keys mongodb.Document, opts *mongodb.IndexOptions) (string, error) {
	return r.c.CreateIndex(keys, opts)
}
func (r realMongoCollection) Indexes() ([]mongodb.Document, error) { return r.c.Indexes() }

// MongoClient is an instance of Mongo::Client: a handle to a MongoDB deployment.
type MongoClient struct {
	cls *RClass
	c   mongoClientAPI
}

func (v *MongoClient) ToS() string     { return "#<Mongo::Client>" }
func (v *MongoClient) Inspect() string { return "#<Mongo::Client>" }
func (v *MongoClient) Truthy() bool    { return true }

// MongoDatabase is an instance of Mongo::Database: a handle to a named database.
type MongoDatabase struct {
	cls *RClass
	db  mongoDatabaseAPI
}

func (v *MongoDatabase) ToS() string     { return "#<Mongo::Database>" }
func (v *MongoDatabase) Inspect() string { return "#<Mongo::Database>" }
func (v *MongoDatabase) Truthy() bool    { return true }

// MongoCollection is an instance of Mongo::Collection: a handle to a named
// collection with the gem's CRUD, aggregation and index methods.
type MongoCollection struct {
	cls  *RClass
	coll mongoCollectionAPI
}

func (v *MongoCollection) ToS() string     { return "#<Mongo::Collection>" }
func (v *MongoCollection) Inspect() string { return "#<Mongo::Collection>" }
func (v *MongoCollection) Truthy() bool    { return true }

// MongoCursor is an instance of Mongo::Cursor: the Enumerable result of #find /
// #aggregate. The underlying library cursor is drained once, lazily, into an
// ordered slice of documents (which closes it, releasing its server-side
// resources), so the Ruby surface is re-iterable and leaks nothing.
type MongoCursor struct {
	cls     *RClass
	cur     mongoCursorAPI
	docs    []mongodb.Document
	drained bool
}

func (v *MongoCursor) ToS() string     { return "#<Mongo::Cursor>" }
func (v *MongoCursor) Inspect() string { return "#<Mongo::Cursor>" }
func (v *MongoCursor) Truthy() bool    { return true }

// MongoResult is an instance of Mongo::Operation::Result: the return value of a
// write operation, exposing the gem's count and inserted-id readers. Only the
// fields relevant to the operation that produced it are populated; the others
// read as nil.
type MongoResult struct {
	cls    *RClass
	fields map[string]object.Value
}

func (v *MongoResult) ToS() string     { return "#<Mongo::Operation::Result>" }
func (v *MongoResult) Inspect() string { return "#<Mongo::Operation::Result>" }
func (v *MongoResult) Truthy() bool    { return true }

// MongoObjectId is an instance of BSON::ObjectId: a 12-byte MongoDB identifier.
type MongoObjectId struct {
	cls *RClass
	id  mongodb.ObjectId
}

func (v *MongoObjectId) ToS() string     { return v.id.Hex() }
func (v *MongoObjectId) Inspect() string { return fmt.Sprintf("BSON::ObjectId('%s')", v.id.Hex()) }
func (v *MongoObjectId) Truthy() bool    { return true }

// raiseMongoError re-raises a library *mongodb.Error as the matching Ruby
// exception (Mongo::Error::OperationFailure, ...ConnectionFailure, etc.). The
// error's Class string equals the Ruby class name registered by registerMongo, so
// the lookup is total; any other error (which the library never returns) raises
// the Mongo::Error base. It never returns (raise panics).
func raiseMongoError(err error) {
	var me *mongodb.Error
	if errors.As(err, &me) {
		raise(string(me.Class), "%s", me.Message)
	}
	raise(string(mongodb.ErrBase), "%s", err.Error())
}

// rubyToBSON maps an outbound Ruby value to the driver's BSON value space:
// ordered Hashes become ordered Documents, Arrays become []any, and the scalars
// map to the BSON wire types the gem uses (an Integer that fits in 32 bits to
// int32, otherwise int64; a Float to double; a Time to a UTC BSON datetime; a
// BSON::ObjectId to its 12-byte id). A value the codec cannot represent raises
// Mongo::Error::InvalidDocument, the gem's client-side serialisation error.
func (vm *VM) rubyToBSON(v object.Value) any {
	switch x := v.(type) {
	case object.Nil:
		return nil
	case object.Bool:
		return bool(x)
	case object.Integer:
		n := int64(x)
		if n >= math.MinInt32 && n <= math.MaxInt32 {
			return int32(n)
		}
		return n
	case object.Float:
		return float64(x)
	case *object.String:
		return x.Str()
	case object.Symbol:
		return string(x)
	case *object.Array:
		out := make([]any, len(x.Elems))
		for i, e := range x.Elems {
			out[i] = vm.rubyToBSON(e)
		}
		return out
	case *object.Hash:
		return vm.rubyHashToDocument(x)
	case *Time:
		return mongodb.NewDateTime(stdtime.Unix(x.t.ToUnix(), 0).UTC())
	case *MongoObjectId:
		return x.id
	default:
		raise(string(mongodb.ErrInvalidDocument), "cannot serialise %s into a BSON value", v.Inspect())
		return nil
	}
}

// rubyHashToDocument converts a Ruby Hash into an ordered BSON Document, keeping
// insertion order and coercing each key to a String (a Symbol or String key is
// accepted, matching how the gem symbolises document keys).
func (vm *VM) rubyHashToDocument(h *object.Hash) mongodb.Document {
	doc := make(mongodb.Document, 0, h.Len())
	for _, k := range h.Keys {
		val, _ := h.Get(k)
		doc = append(doc, mongodb.Element{Key: mongoDocKey(k), Value: vm.rubyToBSON(val)})
	}
	return doc
}

// mongoDocKey coerces a document key to its String form, raising
// Mongo::Error::InvalidDocument for a non-String/Symbol key.
func mongoDocKey(k object.Value) string {
	switch x := k.(type) {
	case object.Symbol:
		return string(x)
	case *object.String:
		return x.Str()
	}
	raise(string(mongodb.ErrInvalidDocument), "document key must be a String or Symbol, got %s", k.Inspect())
	return ""
}

// bsonToRuby maps an inbound BSON value to the Ruby object graph: Documents
// become ordered Hashes (String-keyed, matching the gem's default), arrays become
// Arrays, an ObjectId becomes a BSON::ObjectId, a BSON datetime becomes a Time,
// and the scalars map to Integer / Float / String / true / false / nil. A BSON
// value class the binding does not model (a rarely-used Binary, Timestamp, ...)
// renders as its String form so no value is ever dropped.
func (vm *VM) bsonToRuby(v any) object.Value {
	switch x := v.(type) {
	case nil:
		return object.NilV
	case bool:
		return object.Bool(x)
	case int32:
		return object.IntValue(int64(x))
	case int64:
		return object.IntValue(x)
	case int:
		return object.IntValue(int64(x))
	case float64:
		return object.Float(x)
	case string:
		return object.NewString(x)
	case mongodb.Document:
		return vm.documentToRubyHash(x)
	case []any:
		out := make([]object.Value, len(x))
		for i, e := range x {
			out[i] = vm.bsonToRuby(e)
		}
		return object.NewArrayFromSlice(out)
	case mongodb.ObjectId:
		return vm.newObjectId(x)
	case mongodb.DateTime:
		return &Time{t: gotime.FromUnix(int64(x) / 1000)}
	default:
		return object.NewString(fmt.Sprintf("%v", x))
	}
}

// documentToRubyHash renders an ordered BSON Document as a Ruby Hash whose keys
// are Strings, in document order.
func (vm *VM) documentToRubyHash(d mongodb.Document) *object.Hash {
	h := object.NewHash()
	for _, e := range d {
		h.Set(object.NewString(e.Key), vm.bsonToRuby(e.Value))
	}
	return h
}

// newObjectId wraps a driver ObjectId as a BSON::ObjectId value.
func (vm *VM) newObjectId(id mongodb.ObjectId) *MongoObjectId {
	return &MongoObjectId{cls: vm.consts["BSON::ObjectId"].(*RClass), id: id}
}

// anySliceToRubyArray maps a slice of arbitrary BSON values (an insert's inserted
// ids, a #distinct result) into a Ruby Array.
func (vm *VM) anySliceToRubyArray(vals []any) *object.Array {
	out := make([]object.Value, len(vals))
	for i, e := range vals {
		out[i] = vm.bsonToRuby(e)
	}
	return object.NewArrayFromSlice(out)
}

// documentsToRubyArray maps a slice of ordered Documents into a Ruby Array of
// Hashes.
func (vm *VM) documentsToRubyArray(docs []mongodb.Document) *object.Array {
	out := make([]object.Value, len(docs))
	for i, d := range docs {
		out[i] = vm.documentToRubyHash(d)
	}
	return object.NewArrayFromSlice(out)
}

// newResult builds a Mongo::Operation::Result carrying the given named fields.
func (vm *VM) newResult(fields map[string]object.Value) *MongoResult {
	return &MongoResult{cls: vm.consts["Mongo::Operation::Result"].(*RClass), fields: fields}
}

// mongoDocArg coerces an optional positional argument to a BSON Document: a Hash
// converts, a missing / nil argument becomes an empty document (the gem's default
// filter), and any other value raises TypeError.
func (vm *VM) mongoDocArg(args []object.Value, i int) mongodb.Document {
	if i >= len(args) {
		return mongodb.Document{}
	}
	switch x := args[i].(type) {
	case object.Nil:
		return mongodb.Document{}
	case *object.Hash:
		return vm.rubyHashToDocument(x)
	}
	raise("TypeError", "no implicit conversion of %s into a BSON document", args[i].Inspect())
	return nil
}

// mongoOptsHash returns the trailing keyword Hash of an operation (its sort: /
// limit: / upsert: / unique: ... options) when the argument at i is a Hash, or nil
// otherwise.
func mongoOptsHash(args []object.Value, i int) *object.Hash {
	if i >= len(args) {
		return nil
	}
	h, ok := args[i].(*object.Hash)
	if !ok {
		return nil
	}
	return h
}
