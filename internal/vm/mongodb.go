// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	mongodb "github.com/go-ruby-mongodb/mongodb"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerMongo installs the Mongo module (require "mongo"): the mongo-ruby-driver
// surface — Mongo::Client and its Database and Collection handles, the full CRUD /
// find / aggregate / index method set, the Mongo::Cursor Enumerable, the
// Mongo::Operation::Result write result and the Mongo::Error taxonomy — together
// with the BSON::ObjectId value class of the bson gem. All database work is
// delegated to github.com/go-ruby-mongodb/mongodb, a pure-Go (CGO=0) MRI-faithful
// facade over go.mongodb.org/mongo-driver/v2 (the database's own driver and
// canonical BSON codec), so the whole stack links statically on every 64-bit
// target. BSON documents are ordered Ruby Hashes; the value bridge and the
// connection seam live in mongodb_bind.go.
//
// The library exposes no arbitrary run-command entry point, so Mongo::Database
// deliberately omits #command (which the gem implements over one); every
// operation the library does provide is bound here.
func (vm *VM) registerMongo() {
	mod := newClass("Mongo", nil)
	mod.isModule = true
	vm.consts["Mongo"] = mod

	vm.registerMongoErrors(mod)
	vm.registerBSON()

	cCursor := vm.registerMongoCursor(mod)
	cResult := vm.registerMongoResult(mod)
	cColl := vm.registerMongoCollection(mod, cCursor, cResult)
	cDB := vm.registerMongoDatabase(mod, cColl)
	vm.registerMongoClient(mod, cDB, cColl)
}

// registerMongoErrors installs the Mongo::Error tree, mirroring the gem: the root
// Mongo::Error < StandardError and the operation / connection / bulk-write /
// document / server-selection subclasses beneath it. Each class name equals the
// library's ErrorClass string, so a raised *mongodb.Error maps to its Ruby class
// by name in raiseMongoError.
func (vm *VM) registerMongoErrors(mod *RClass) {
	std := vm.consts["StandardError"].(*RClass)
	base := newClass(string(mongodb.ErrBase), std)
	mod.consts["Error"] = base
	vm.consts[string(mongodb.ErrBase)] = base
	for _, name := range []mongodb.ErrorClass{
		mongodb.ErrOperationFailure, mongodb.ErrConnectionFailure,
		mongodb.ErrBulkWriteError, mongodb.ErrInvalidDocument,
		mongodb.ErrNoServerAvailable,
	} {
		cls := newClass(string(name), base)
		// The qualified name is "Mongo::Error::<Simple>"; nest it under the Error
		// class by its simple tail so Ruby Mongo::Error::OperationFailure resolves.
		simple := string(name)[len("Mongo::Error::"):]
		base.consts[simple] = cls
		vm.consts[string(name)] = cls
	}
}

// registerBSON installs the BSON module and its BSON::ObjectId value class: the
// .new / .from_string constructors and the #to_s / #inspect / #== / #hash
// instance methods, mirroring the bson gem's identifier type.
func (vm *VM) registerBSON() {
	mod := newClass("BSON", nil)
	mod.isModule = true
	vm.consts["BSON"] = mod

	cls := newClass("BSON::ObjectId", vm.cObject)
	mod.consts["ObjectId"] = cls
	vm.consts["BSON::ObjectId"] = cls

	cls.smethods["new"] = &Method{name: "new", owner: cls, native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) > 0 {
			return vm.objectIdFromString(strArg(args[0]))
		}
		return vm.newObjectId(mongodb.NewObjectId())
	}}
	cls.smethods["from_string"] = &Method{name: "from_string", owner: cls, native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		return vm.objectIdFromString(strArg(args[0]))
	}}

	self := func(v object.Value) *MongoObjectId { return v.(*MongoObjectId) }
	cls.define("to_s", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).ToS())
	})
	cls.define("inspect", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).Inspect())
	})
	cls.define("hash", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		// #hash derives an Integer from the 12 identifier bytes (FNV-1a), so two
		// #eql? ObjectIds hash together as a Ruby Hash key.
		var h uint64 = 1469598103934665603
		for _, b := range self(v).id {
			h = (h ^ uint64(b)) * 1099511628211
		}
		return object.IntValue(int64(h))
	})
	cls.define("==", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		other, ok := args[0].(*MongoObjectId)
		return object.Bool(ok && self(v).id == other.id)
	})
}

// objectIdFromString parses a 24-char hex string into a BSON::ObjectId, raising
// Mongo::Error::InvalidDocument when the string is not a valid identifier.
func (vm *VM) objectIdFromString(hex string) object.Value {
	id, err := mongodb.ObjectIdFromString(hex)
	if err != nil {
		raiseMongoError(err)
	}
	return vm.newObjectId(id)
}

// registerMongoClient installs Mongo::Client and its instance methods, wiring the
// connection through the mongoDial seam. cDB and cColl are the Database and
// Collection classes it hands out.
func (vm *VM) registerMongoClient(mod, cDB, cColl *RClass) {
	cls := newClass("Mongo::Client", vm.cObject)
	mod.consts["Client"] = cls
	vm.consts["Mongo::Client"] = cls

	// Mongo::Client.new(uri, database:, app_name:): connect to a deployment. The
	// underlying driver dials lazily, so this needs no live server.
	cls.smethods["new"] = &Method{name: "new", owner: cls, native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		uri := strArg(args[0])
		var opts []mongodb.Option
		if h := mongoOptsHash(args, 1); h != nil {
			if v, ok := h.Get(object.Symbol("database")); ok {
				opts = append(opts, mongodb.WithDatabase(v.ToS()))
			}
			if v, ok := h.Get(object.Symbol("app_name")); ok {
				opts = append(opts, mongodb.AppName(v.ToS()))
			}
		}
		c, err := mongoDial(uri, opts...)
		if err != nil {
			raiseMongoError(err)
		}
		return &MongoClient{cls: cls, c: c}
	}}

	self := func(v object.Value) mongoClientAPI { return v.(*MongoClient).c }

	// #database(name = nil) returns a database handle; a nil / omitted name resolves
	// to the client's default database (the database: option).
	cls.define("database", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		name := ""
		if len(args) > 0 {
			name = mongoName(args[0])
		}
		return &MongoDatabase{cls: cDB, db: self(v).Database(name)}
	})

	// #[](name) returns a collection in the client's default database.
	cls.define("[]", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		return &MongoCollection{cls: cColl, coll: self(v).Collection(mongoName(args[0]))}
	})

	// #close disconnects the client and releases pooled connections. A disconnect
	// error can only come from an already-closed client, so — as the SQLite3 and
	// Bolt bindings do with their handles — the result is discarded rather than
	// raised.
	cls.define("close", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		_ = self(v).Close()
		return object.NilV
	})
}

// registerMongoDatabase installs Mongo::Database, returning the class. cColl is
// the Collection class it hands out.
func (vm *VM) registerMongoDatabase(mod, cColl *RClass) *RClass {
	cls := newClass("Mongo::Database", vm.cObject)
	mod.consts["Database"] = cls
	vm.consts["Mongo::Database"] = cls

	self := func(v object.Value) mongoDatabaseAPI { return v.(*MongoDatabase).db }

	cls.define("name", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).Name())
	})
	collection := func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		return &MongoCollection{cls: cColl, coll: self(v).Collection(mongoName(args[0]))}
	}
	cls.define("collection", collection)
	cls.define("[]", collection)

	return cls
}

// registerMongoCollection installs Mongo::Collection and its CRUD / aggregation /
// index methods, returning the class. cCursor and cResult are the classes it uses
// to wrap query cursors and write results.
func (vm *VM) registerMongoCollection(mod, cCursor, cResult *RClass) *RClass {
	cls := newClass("Mongo::Collection", vm.cObject)
	mod.consts["Collection"] = cls
	vm.consts["Mongo::Collection"] = cls

	d := func(name string, fn NativeFn) { cls.define(name, fn) }
	self := func(v object.Value) mongoCollectionAPI { return v.(*MongoCollection).coll }
	cursor := func(c mongoCursorAPI) object.Value { return &MongoCursor{cls: cCursor, cur: c} }

	d("name", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).Name())
	})

	// #insert_one(document) inserts a single document, returning a result whose
	// #inserted_id is the _id the server (or the driver) assigned.
	d("insert_one", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		doc := vm.rubyHashToDocument(vm.mongoHashArg(args, 0))
		res, err := self(v).InsertOne(doc)
		if err != nil {
			raiseMongoError(err)
		}
		return vm.newResult(map[string]object.Value{"inserted_id": vm.bsonToRuby(res.InsertedID)})
	})

	// #insert_many(documents) inserts an array of documents, returning a result
	// whose #inserted_ids lists the assigned _ids in input order.
	d("insert_many", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		docs := vm.mongoDocArray(args, 0)
		res, err := self(v).InsertMany(docs)
		if err != nil {
			raiseMongoError(err)
		}
		return vm.newResult(map[string]object.Value{
			"inserted_ids":   vm.anySliceToRubyArray(res.InsertedIDs),
			"inserted_count": object.IntValue(int64(len(res.InsertedIDs))),
		})
	})

	// #find(filter = {}, **opts) runs a query and returns a Mongo::Cursor over the
	// matching documents. opts may carry sort:, projection:, limit:, skip: and
	// batch_size:.
	d("find", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		cur, err := self(v).Find(vm.mongoDocArg(args, 0), vm.mongoFindOptions(mongoOptsHash(args, 1)))
		if err != nil {
			raiseMongoError(err)
		}
		return cursor(cur)
	})

	// #find_one(filter = {}, **opts) returns the first matching document as a Hash,
	// or nil when nothing matches.
	d("find_one", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		doc, err := self(v).FindOne(vm.mongoDocArg(args, 0), vm.mongoFindOptions(mongoOptsHash(args, 1)))
		if err != nil {
			raiseMongoError(err)
		}
		if doc == nil {
			return object.NilV
		}
		return vm.documentToRubyHash(doc)
	})

	d("update_one", vm.mongoUpdateMethod(func(c mongoCollectionAPI, f, u mongodb.Document, o *mongodb.UpdateOptions) (*mongodb.UpdateResult, error) {
		return c.UpdateOne(f, u, o)
	}))
	d("update_many", vm.mongoUpdateMethod(func(c mongoCollectionAPI, f, u mongodb.Document, o *mongodb.UpdateOptions) (*mongodb.UpdateResult, error) {
		return c.UpdateMany(f, u, o)
	}))
	d("replace_one", vm.mongoUpdateMethod(func(c mongoCollectionAPI, f, u mongodb.Document, o *mongodb.UpdateOptions) (*mongodb.UpdateResult, error) {
		return c.ReplaceOne(f, u, o)
	}))

	d("delete_one", vm.mongoDeleteMethod(func(c mongoCollectionAPI, f mongodb.Document) (*mongodb.DeleteResult, error) {
		return c.DeleteOne(f)
	}))
	d("delete_many", vm.mongoDeleteMethod(func(c mongoCollectionAPI, f mongodb.Document) (*mongodb.DeleteResult, error) {
		return c.DeleteMany(f)
	}))

	// #count_documents(filter = {}, **opts) counts matching documents. opts may
	// carry limit: and skip:.
	d("count_documents", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		n, err := self(v).CountDocuments(vm.mongoDocArg(args, 0), mongoCountOptions(mongoOptsHash(args, 1)))
		if err != nil {
			raiseMongoError(err)
		}
		return object.IntValue(n)
	})

	// #aggregate(pipeline) runs an aggregation pipeline (an array of stage
	// documents) and returns a Mongo::Cursor over the output.
	d("aggregate", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		cur, err := self(v).Aggregate(vm.mongoDocArray(args, 0))
		if err != nil {
			raiseMongoError(err)
		}
		return cursor(cur)
	})

	// #distinct(field, filter = {}) returns the distinct values of a field.
	d("distinct", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		vals, err := self(v).Distinct(mongoName(args[0]), vm.mongoDocArg(args, 1))
		if err != nil {
			raiseMongoError(err)
		}
		return vm.anySliceToRubyArray(vals)
	})

	// #create_index(keys, **opts) creates an index over the key spec, returning the
	// created index name. opts may carry name: and unique:.
	d("create_index", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		keys := vm.rubyHashToDocument(vm.mongoHashArg(args, 0))
		name, err := self(v).CreateIndex(keys, mongoIndexOptions(mongoOptsHash(args, 1)))
		if err != nil {
			raiseMongoError(err)
		}
		return object.NewString(name)
	})

	// #indexes lists the collection's indexes as an array of Hashes.
	d("indexes", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		docs, err := self(v).Indexes()
		if err != nil {
			raiseMongoError(err)
		}
		return vm.documentsToRubyArray(docs)
	})

	return cls
}

// mongoUpdateMethod builds the shared body of #update_one / #update_many /
// #replace_one: filter and update/replacement documents plus an optional upsert:
// option, mapped to a Mongo::Operation::Result carrying the match/modify/upsert
// counts and any upserted _id.
func (vm *VM) mongoUpdateMethod(op func(mongoCollectionAPI, mongodb.Document, mongodb.Document, *mongodb.UpdateOptions) (*mongodb.UpdateResult, error)) NativeFn {
	return func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		filter := vm.mongoDocArg(args, 0)
		update := vm.rubyHashToDocument(vm.mongoHashArg(args, 1))
		res, err := op(v.(*MongoCollection).coll, filter, update, mongoUpdateOptions(mongoOptsHash(args, 2)))
		if err != nil {
			raiseMongoError(err)
		}
		return vm.newResult(map[string]object.Value{
			"matched_count":  object.IntValue(res.MatchedCount),
			"modified_count": object.IntValue(res.ModifiedCount),
			"upserted_count": object.IntValue(res.UpsertedCount),
			"upserted_id":    vm.bsonToRuby(res.UpsertedID),
		})
	}
}

// mongoDeleteMethod builds the shared body of #delete_one / #delete_many: a
// filter document mapped to a Mongo::Operation::Result carrying #deleted_count.
func (vm *VM) mongoDeleteMethod(op func(mongoCollectionAPI, mongodb.Document) (*mongodb.DeleteResult, error)) NativeFn {
	return func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		res, err := op(v.(*MongoCollection).coll, vm.mongoDocArg(args, 0))
		if err != nil {
			raiseMongoError(err)
		}
		return vm.newResult(map[string]object.Value{"deleted_count": object.IntValue(res.DeletedCount)})
	}
}

// registerMongoCursor installs Mongo::Cursor: the Enumerable result of #find /
// #aggregate. The library cursor is drained once into an ordered slice (closing
// it), so #each / #to_a / #map / #first are re-iterable and leak nothing.
func (vm *VM) registerMongoCursor(mod *RClass) *RClass {
	cls := newClass("Mongo::Cursor", vm.cObject)
	mod.consts["Cursor"] = cls
	vm.consts["Mongo::Cursor"] = cls

	docs := func(v object.Value) []mongodb.Document { return v.(*MongoCursor).drain() }

	cls.define("each", func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (yield)")
		}
		for _, d := range docs(v) {
			vm.callBlock(blk, []object.Value{vm.documentToRubyHash(d)})
		}
		return v
	})
	toArray := func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.documentsToRubyArray(docs(v))
	}
	cls.define("to_a", toArray)
	cls.define("to_ary", toArray)
	cls.define("map", func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (yield)")
		}
		ds := docs(v)
		out := make([]object.Value, len(ds))
		for i, d := range ds {
			out[i] = vm.callBlock(blk, []object.Value{vm.documentToRubyHash(d)})
		}
		return object.NewArrayFromSlice(out)
	})
	cls.define("first", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		ds := docs(v)
		if len(ds) == 0 {
			return object.NilV
		}
		return vm.documentToRubyHash(ds[0])
	})

	return cls
}

// drain materialises the cursor's documents on first access, closing the
// underlying library cursor. A drain error is raised as the matching Mongo::Error.
func (c *MongoCursor) drain() []mongodb.Document {
	if !c.drained {
		out, err := c.cur.ToArray()
		if err != nil {
			raiseMongoError(err)
		}
		c.docs = out
		c.drained = true
	}
	return c.docs
}

// registerMongoResult installs Mongo::Operation::Result: the value a write
// operation returns, exposing the gem's count and inserted-id readers. Only the
// fields the producing operation set are populated; the rest read as nil.
func (vm *VM) registerMongoResult(mod *RClass) *RClass {
	ns := newClass("Mongo::Operation", nil)
	ns.isModule = true
	mod.consts["Operation"] = ns
	vm.consts["Mongo::Operation"] = ns

	cls := newClass("Mongo::Operation::Result", vm.cObject)
	ns.consts["Result"] = cls
	vm.consts["Mongo::Operation::Result"] = cls

	reader := func(field string) {
		cls.define(field, func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
			if r, ok := v.(*MongoResult).fields[field]; ok {
				return r
			}
			return object.NilV
		})
	}
	for _, f := range []string{
		"inserted_id", "inserted_ids", "inserted_count",
		"matched_count", "modified_count", "upserted_count", "upserted_id",
		"deleted_count",
	} {
		reader(f)
	}
	cls.define("ok?", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.True
	})

	return cls
}

// mongoName coerces a database / collection / field name argument to a String,
// accepting a String or a Symbol (the gem takes either), and raising TypeError
// for anything else.
func mongoName(v object.Value) string {
	switch x := v.(type) {
	case *object.String:
		return x.Str()
	case object.Symbol:
		return string(x)
	}
	raise("TypeError", "no implicit conversion of %s into String", v.Inspect())
	return ""
}

// mongoHashArg coerces the required Hash argument at i (an insert document, an
// update spec, an index key spec), raising on absence or a non-Hash value.
func (vm *VM) mongoHashArg(args []object.Value, i int) *object.Hash {
	if i >= len(args) {
		raise("ArgumentError", "wrong number of arguments (given %d, expected %d)", len(args), i+1)
	}
	h, ok := args[i].(*object.Hash)
	if !ok {
		raise("TypeError", "no implicit conversion of %s into Hash", args[i].Inspect())
	}
	return h
}

// mongoDocArray coerces the required Array argument at i (an insert batch, an
// aggregation pipeline) into a slice of Documents, raising on a non-Array element
// or a non-Hash member.
func (vm *VM) mongoDocArray(args []object.Value, i int) []mongodb.Document {
	if i >= len(args) {
		raise("ArgumentError", "wrong number of arguments (given %d, expected %d)", len(args), i+1)
	}
	arr, ok := args[i].(*object.Array)
	if !ok {
		raise("TypeError", "no implicit conversion of %s into Array", args[i].Inspect())
	}
	docs := make([]mongodb.Document, len(arr.Elems))
	for j, e := range arr.Elems {
		h, ok := e.(*object.Hash)
		if !ok {
			raise("TypeError", "no implicit conversion of %s into Hash", e.Inspect())
		}
		docs[j] = vm.rubyHashToDocument(h)
	}
	return docs
}

// mongoFindOptions builds a *mongodb.FindOptions from a #find / #find_one keyword
// Hash (sort:, projection:, limit:, skip:, batch_size:), or nil when there is none.
func (vm *VM) mongoFindOptions(h *object.Hash) *mongodb.FindOptions {
	if h == nil {
		return nil
	}
	o := &mongodb.FindOptions{}
	if v, ok := h.Get(object.Symbol("sort")); ok {
		o.Sort = vm.rubyHashToDocument(mongoOptHashValue(v))
	}
	if v, ok := h.Get(object.Symbol("projection")); ok {
		o.Projection = vm.rubyHashToDocument(mongoOptHashValue(v))
	}
	if v, ok := h.Get(object.Symbol("limit")); ok {
		n := intArg(v)
		o.Limit = &n
	}
	if v, ok := h.Get(object.Symbol("skip")); ok {
		n := intArg(v)
		o.Skip = &n
	}
	if v, ok := h.Get(object.Symbol("batch_size")); ok {
		n := int32(intArg(v))
		o.BatchSize = &n
	}
	return o
}

// mongoUpdateOptions builds a *mongodb.UpdateOptions from an update/replace
// keyword Hash (upsert:), or nil when there is none.
func mongoUpdateOptions(h *object.Hash) *mongodb.UpdateOptions {
	if h == nil {
		return nil
	}
	o := &mongodb.UpdateOptions{}
	if v, ok := h.Get(object.Symbol("upsert")); ok {
		o.Upsert = v.Truthy()
	}
	return o
}

// mongoCountOptions builds a *mongodb.CountOptions from a #count_documents keyword
// Hash (limit:, skip:), or nil when there is none.
func mongoCountOptions(h *object.Hash) *mongodb.CountOptions {
	if h == nil {
		return nil
	}
	o := &mongodb.CountOptions{}
	if v, ok := h.Get(object.Symbol("limit")); ok {
		n := intArg(v)
		o.Limit = &n
	}
	if v, ok := h.Get(object.Symbol("skip")); ok {
		n := intArg(v)
		o.Skip = &n
	}
	return o
}

// mongoIndexOptions builds a *mongodb.IndexOptions from a #create_index keyword
// Hash (name:, unique:), or nil when there is none.
func mongoIndexOptions(h *object.Hash) *mongodb.IndexOptions {
	if h == nil {
		return nil
	}
	o := &mongodb.IndexOptions{}
	if v, ok := h.Get(object.Symbol("name")); ok {
		o.Name = v.ToS()
	}
	if v, ok := h.Get(object.Symbol("unique")); ok {
		o.Unique = v.Truthy()
	}
	return o
}

// mongoOptHashValue asserts an option value (a sort: / projection: spec) is a
// Hash, raising TypeError otherwise.
func mongoOptHashValue(v object.Value) *object.Hash {
	h, ok := v.(*object.Hash)
	if !ok {
		raise("TypeError", "no implicit conversion of %s into Hash", v.Inspect())
	}
	return h
}
