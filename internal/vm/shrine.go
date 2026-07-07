// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"io"
	"os"
	"path/filepath"

	shrine "github.com/go-ruby-shrine/shrine"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerShrine installs the Shrine module tree (require "shrine"): the Shrine
// class with its class-level storage registry (Shrine.storages / .storages=), its
// Shrine.new(:key) uploader factory and Shrine.uploaded_file rehydrator, the
// Shrine::Storage::Memory / ::FileSystem backends, the Shrine::UploadedFile value
// and the Shrine::Attacher cache→store lifecycle, plus a Shrine::Error base. All
// attachment behaviour is delegated to github.com/go-ruby-shrine/shrine — a pure-Go,
// no-cgo reimplementation of the deterministic core of Ruby's shrine gem — so file
// storage, metadata extraction (net/http content sniffing), location generation
// (random hex + extension) and the JSON UploadedFile representation are identical on
// every supported architecture. The value types and Ruby↔Go conversions live in
// shrine_bind.go.
//
// The registry is per-VM state captured in a closure: ctx is the live *shrine.Shrine
// context and wrappers its name→Ruby-storage view. Shrine.storages= rebuilds both
// (the variables are shared by reference across every class-method closure), so a
// reassignment is visible to a later Shrine.new. The default storages are an
// in-memory cache and a filesystem store rooted under the OS temp dir (created
// lazily on first upload by the FileSystem backend).
func (vm *VM) registerShrine() {
	std := vm.consts["StandardError"].(*RClass)

	cls := newClass("Shrine", vm.cObject)
	vm.consts["Shrine"] = cls

	errCls := newClass("Shrine::Error", std)
	cls.consts["Error"] = errCls
	vm.consts["Shrine::Error"] = errCls

	// Shrine::Storage module with the Memory / FileSystem backend classes.
	storageMod := newClass("Shrine::Storage", nil)
	storageMod.isModule = true
	cls.consts["Storage"] = storageMod
	vm.consts["Shrine::Storage"] = storageMod
	memCls := newClass("Shrine::Storage::Memory", vm.cObject)
	fsCls := newClass("Shrine::Storage::FileSystem", vm.cObject)
	storageMod.consts["Memory"] = memCls
	storageMod.consts["FileSystem"] = fsCls
	vm.consts["Shrine::Storage::Memory"] = memCls
	vm.consts["Shrine::Storage::FileSystem"] = fsCls
	registerShrineStorageMethods(memCls)
	registerShrineStorageMethods(fsCls)

	// Shrine::Storage::Memory.new — a fresh in-memory backend.
	memCls.smethods["new"] = &Method{name: "new", owner: memCls,
		native: func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
			return &ShrineStorage{cls: memCls, st: shrine.NewMemory(), kind: "Memory"}
		}}
	// Shrine::Storage::FileSystem.new(directory) — a backend rooted at directory.
	fsCls.smethods["new"] = &Method{name: "new", owner: fsCls,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			if len(args) == 0 {
				raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
			}
			return &ShrineStorage{cls: fsCls, st: shrine.NewFileSystem(pathArg(vm, args[0])), kind: "FileSystem"}
		}}

	ufCls := newClass("Shrine::UploadedFile", vm.cObject)
	cls.consts["UploadedFile"] = ufCls
	vm.consts["Shrine::UploadedFile"] = ufCls

	attCls := newClass("Shrine::Attacher", vm.cObject)
	cls.consts["Attacher"] = attCls
	vm.consts["Shrine::Attacher"] = attCls

	// Live per-VM registry, captured by every class-method closure below.
	defaultDir := filepath.Join(os.TempDir(), "rbgo-shrine")
	cacheW := &ShrineStorage{cls: memCls, st: shrine.NewMemory(), kind: "Memory"}
	storeW := &ShrineStorage{cls: fsCls, st: shrine.NewFileSystem(defaultDir), kind: "FileSystem"}
	ctx := shrine.New()
	ctx.Register("cache", cacheW.st)
	ctx.Register("store", storeW.st)
	wrappers := map[string]*ShrineStorage{"cache": cacheW, "store": storeW}

	// Shrine.storages — the registry as a Hash of {name(sym) => storage}.
	cls.smethods["storages"] = &Method{name: "storages", owner: cls,
		native: func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
			h := object.NewHash()
			for name, sw := range wrappers {
				h.Set(object.Symbol(name), sw)
			}
			return h
		}}
	// Shrine.storages = { cache: …, store: … } — replace the registry, rebuilding
	// the context so a later Shrine.new / uploaded_file sees the new backends.
	cls.smethods["storages="] = &Method{name: "storages=", owner: cls,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			if len(args) == 0 {
				raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
			}
			h, ok := args[0].(*object.Hash)
			if !ok {
				raise("TypeError", "shrine: storages must be a Hash, got %s", classNameOf(args[0]))
			}
			fresh := shrine.New()
			nw := map[string]*ShrineStorage{}
			for _, k := range h.Keys {
				v, _ := h.Get(k)
				sw, ok := v.(*ShrineStorage)
				if !ok {
					raise("TypeError", "shrine: storage must be a Shrine::Storage, got %s", classNameOf(v))
				}
				name := shrineName(k)
				fresh.Register(name, sw.st)
				nw[name] = sw
			}
			ctx = fresh
			wrappers = nw
			return args[0]
		}}

	// Shrine.new(:store) — an Uploader bound to the named storage.
	cls.smethods["new"] = &Method{name: "new", owner: cls,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			if len(args) == 0 {
				raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
			}
			u, err := ctx.Uploader(shrineName(args[0]))
			if err != nil {
				raiseShrineError(err)
			}
			return &ShrineUploader{cls: cls, u: u}
		}}

	// Shrine.uploaded_file(data) — rehydrate an UploadedFile from its JSON String
	// or its {id:, storage:, metadata:} Hash representation.
	cls.smethods["uploaded_file"] = &Method{name: "uploaded_file", owner: cls,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			if len(args) == 0 {
				raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
			}
			f, err := ctx.UploadedFile(shrineDataJSON(args[0]))
			if err != nil {
				raiseShrineError(err)
			}
			return &ShrineUploadedFile{cls: ufCls, f: f}
		}}

	// Shrine::Attacher.new(cache: :cache, store: :store) — a lifecycle over the two
	// named storages (defaulting to the conventional cache/store keys).
	attCls.smethods["new"] = &Method{name: "new", owner: attCls,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			cacheKey, storeKey := "cache", "store"
			if h := shrineOptsHash(args); h != nil {
				if v, ok := shrineHashGet(h, "cache"); ok {
					cacheKey = shrineName(v)
				}
				if v, ok := shrineHashGet(h, "store"); ok {
					storeKey = shrineName(v)
				}
			}
			a, err := ctx.Attacher(cacheKey, storeKey)
			if err != nil {
				raiseShrineError(err)
			}
			return &ShrineAttacher{cls: attCls, a: a}
		}}

	registerShrineUploaderMethods(cls, ufCls)
	registerShrineUploadedFileMethods(ufCls)
	registerShrineAttacherMethods(attCls, ufCls)
}

// registerShrineUploaderMethods installs the Uploader instance surface on the
// Shrine class (Shrine.new returns an Uploader): #upload persists an IO/String
// through the bound storage and returns an UploadedFile, and #storage_key reads the
// bound storage name.
func registerShrineUploaderMethods(cls, ufCls *RClass) {
	self := func(v object.Value) *ShrineUploader { return v.(*ShrineUploader) }

	cls.define("upload", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		f, err := self(v).u.Upload(shrineReader(args[0]), shrineUploadOptions(args[1:]))
		if err != nil {
			raiseShrineError(err)
		}
		return &ShrineUploadedFile{cls: ufCls, f: f}
	})
	cls.define("storage_key", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).u.StorageKey())
	})
}

// registerShrineUploadedFileMethods installs the Shrine::UploadedFile value surface:
// its id/storage/metadata/filename/mime_type/size readers, the byte accessors
// (#download/#open/#url), the storage predicates/mutators (#exists?/#delete/#replace)
// and the serialisation pair (#data/#to_json).
func registerShrineUploadedFileMethods(ufCls *RClass) {
	self := func(v object.Value) *shrine.UploadedFile { return v.(*ShrineUploadedFile).f }

	ufCls.define("id", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).ID)
	})
	ufCls.define("storage", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).StorageKey)
	})
	ufCls.define("metadata", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return shrineMapToHash(map[string]any(self(v).Metadata))
	})
	ufCls.define("filename", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).Filename())
	})
	ufCls.define("mime_type", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).MimeType())
	})
	ufCls.define("size", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(self(v).Size())
	})
	ufCls.define("url", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).URL(shrineURLOptions(args)))
	})
	ufCls.define("download", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		data, err := self(v).Download()
		if err != nil {
			raiseShrineError(err)
		}
		return object.NewStringBytes(data)
	})
	ufCls.define("open", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		rc, err := self(v).Open()
		if err != nil {
			raiseShrineError(err)
		}
		return shrineStringIO(vm, rc)
	})
	ufCls.define("exists?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).Exists())
	})
	ufCls.define("delete", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		if err := self(v).Delete(); err != nil {
			raiseShrineError(err)
		}
		return object.NilV
	})
	ufCls.define("replace", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		nf, err := self(v).Replace(shrineReader(args[0]), shrineUploadOptions(args[1:]))
		if err != nil {
			raiseShrineError(err)
		}
		return &ShrineUploadedFile{cls: ufCls, f: nf}
	})
	ufCls.define("data", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return shrineMapToHash(self(v).Data())
	})
	ufCls.define("to_json", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		s, err := self(v).ToJSON()
		if err != nil {
			raiseShrineError(err)
		}
		return object.NewString(s)
	})
}

// registerShrineAttacherMethods installs the Shrine::Attacher lifecycle: #assign
// caches a new file, #promote/#finalize move it to the permanent store, #destroy
// removes it, #changed? reports a pending change, and #get/#set read/replace the
// attached UploadedFile.
func registerShrineAttacherMethods(attCls, ufCls *RClass) {
	self := func(v object.Value) *shrine.Attacher { return v.(*ShrineAttacher).a }

	attCls.define("assign", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		if err := self(v).Assign(shrineReader(args[0]), shrineUploadOptions(args[1:])); err != nil {
			raiseShrineError(err)
		}
		return object.NilV
	})
	attCls.define("finalize", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		if err := self(v).Finalize(); err != nil {
			raiseShrineError(err)
		}
		return object.NilV
	})
	attCls.define("promote", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		if err := self(v).Promote(); err != nil {
			raiseShrineError(err)
		}
		return object.NilV
	})
	attCls.define("destroy", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		if err := self(v).Destroy(); err != nil {
			raiseShrineError(err)
		}
		return object.NilV
	})
	attCls.define("changed?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).Changed())
	})
	attCls.define("get", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		f := self(v).Get()
		if f == nil {
			return object.NilV
		}
		return &ShrineUploadedFile{cls: ufCls, f: f}
	})
	attCls.define("set", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		uf, ok := args[0].(*ShrineUploadedFile)
		if !ok {
			raise("TypeError", "shrine: set expects a Shrine::UploadedFile, got %s", classNameOf(args[0]))
		}
		self(v).Set(uf.f)
		return object.NilV
	})
}

// registerShrineStorageMethods installs the low-level Shrine::Storage surface shared
// by Memory and FileSystem: #upload/#open move bytes, #exists?/#delete/#url query and
// mutate a stored id. These mirror the Storage contract a host implements.
func registerShrineStorageMethods(cls *RClass) {
	self := func(v object.Value) shrine.Storage { return v.(*ShrineStorage).st }

	cls.define("upload", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
		}
		if err := self(v).Upload(shrineReader(args[0]), strArg(args[1]), nil); err != nil {
			raiseShrineError(err)
		}
		return object.NilV
	})
	cls.define("open", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		rc, err := self(v).Open(strArg(args[0]))
		if err != nil {
			raiseShrineError(err)
		}
		return shrineStringIO(vm, rc)
	})
	cls.define("exists?", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		return object.Bool(self(v).Exists(strArg(args[0])))
	})
	cls.define("delete", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		if err := self(v).Delete(strArg(args[0])); err != nil {
			raiseShrineError(err)
		}
		return object.NilV
	})
	cls.define("url", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		return object.NewString(self(v).URL(strArg(args[0]), shrineURLOptions(args[1:])))
	})
}

// shrineStringIO drains a storage reader into a StringIO over the read bytes, so a
// Ruby #open/#download flows the stored bytes back as an in-memory IO. The reader is
// closed after draining.
func shrineStringIO(vm *VM, rc io.ReadCloser) object.Value {
	data, err := io.ReadAll(rc)
	rc.Close()
	if err != nil {
		raiseShrineError(err)
	}
	return &IOObj{cls: vm.consts["StringIO"].(*RClass), isStr: true, buf: data}
}
