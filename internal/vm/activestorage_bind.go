// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	activestorage "github.com/go-ruby-activestorage/activestorage"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file wires the ActiveStorage::Blob factory + instance surface, the
// ActiveStorage::Service backend surface, ActiveStorage::Attachment, and the
// has_one_attached / has_many_attached proxies (Attached::One / ::Many) over the
// go-ruby-activestorage library and the deterministic process config
// (asRequireConfig).

// asSM installs a singleton (class) method on cls.
func asSM(cls *RClass, name string, fn NativeFn) {
	cls.smethods[name] = &Method{name: name, owner: cls, native: fn}
}

// registerActiveStorageBlob installs ActiveStorage::Blob: the create/find factory
// class methods and a blob's stored-column readers, #signed_id, #download,
// #download_chunk, #url, #service and #purge.
func (vm *VM) registerActiveStorageBlob(mod *RClass) {
	cls := newClass("ActiveStorage::Blob", vm.cObject)
	mod.consts["Blob"] = cls
	vm.consts["ActiveStorage::Blob"] = cls

	// Blob.create_and_upload!(io:, filename:, content_type:, key:, service_name:)
	// builds a blob from io, persists the row, and uploads the bytes to its
	// service (computing byte_size and the base64 MD5 checksum).
	asSM(cls, "create_and_upload!", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		kw := asKwargs(args)
		b, err := vm.asRequireConfig().CreateAndUpload(asReader(asKwGet(kw, "io")), asBlobParams(kw))
		if err != nil {
			asRaise(err)
		}
		return &ASBlob{b: b}
	})
	// Blob.build_after_upload(io:, filename:, …) uploads without persisting the row.
	asSM(cls, "build_after_upload", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		kw := asKwargs(args)
		b, err := vm.asRequireConfig().BuildAfterUpload(asReader(asKwGet(kw, "io")), asBlobParams(kw))
		if err != nil {
			asRaise(err)
		}
		return &ASBlob{b: b}
	})
	// Blob.create_before_direct_upload!(filename:, byte_size:, checksum:, …)
	// persists a blob whose bytes a client will PUT directly (no upload here).
	asSM(cls, "create_before_direct_upload!", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		kw := asKwargs(args)
		p := asBlobParams(kw)
		p.ByteSize = asKwInt(kw, "byte_size")
		p.Checksum = asKwString(kw, "checksum")
		b, err := vm.asRequireConfig().CreateBeforeDirectUpload(p)
		if err != nil {
			asRaise(err)
		}
		return &ASBlob{b: b}
	})
	asSM(cls, "find", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		b, err := vm.asRequireConfig().FindBlob(asPosInt(args, 0))
		if err != nil {
			asRaise(err)
		}
		return &ASBlob{b: b}
	})
	asSM(cls, "find_signed", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		b, err := vm.asRequireConfig().FindSignedBlob(asPosStr(args, 0))
		if err != nil {
			asRaise(err)
		}
		return &ASBlob{b: b}
	})
	// Blob.service returns the default storage service.
	asSM(cls, "service", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		svc, err := vm.asRequireConfig().Services.Default()
		if err != nil {
			asRaise(err)
		}
		return &ASService{s: svc}
	})

	vm.registerActiveStorageBlobInstance(cls)
}

// registerActiveStorageBlobInstance installs a blob's instance surface.
func (vm *VM) registerActiveStorageBlobInstance(cls *RClass) {
	self := func(v object.Value) *activestorage.Blob { return v.(*ASBlob).b }

	cls.define("id", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(self(v).ID)
	})
	cls.define("key", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).Key)
	})
	cls.define("filename", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).Filename)
	})
	cls.define("content_type", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).ContentType)
	})
	cls.define("byte_size", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(self(v).ByteSize)
	})
	cls.define("checksum", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).Checksum)
	})
	cls.define("service_name", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).ServiceName)
	})
	cls.define("signed_id", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		id, err := self(v).SignedID()
		if err != nil {
			asRaise(err)
		}
		return object.NewString(id)
	})
	cls.define("download", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		data, err := self(v).Download()
		if err != nil {
			asRaise(err)
		}
		return object.NewStringBytesEnc(data, "ASCII-8BIT")
	})
	cls.define("download_chunk", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		data, err := self(v).DownloadChunk(asPosInt(args, 0), asPosInt(args, 1))
		if err != nil {
			asRaise(err)
		}
		return object.NewStringBytesEnc(data, "ASCII-8BIT")
	})
	cls.define("url", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		u, err := self(v).URL(asURLOptions(asKwargs(args)))
		if err != nil {
			asRaise(err)
		}
		return object.NewString(u)
	})
	cls.define("service", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		svc, err := self(v).Service()
		if err != nil {
			asRaise(err)
		}
		return &ASService{s: svc}
	})
	cls.define("purge", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		if err := self(v).Purge(); err != nil {
			asRaise(err)
		}
		return object.NilV
	})
}

// registerActiveStorageService installs ActiveStorage::Service (and the
// informational DiskService subclass constant) and the key-addressed backend
// operations a service answers.
func (vm *VM) registerActiveStorageService(mod *RClass) {
	cls := newClass("ActiveStorage::Service", vm.cObject)
	mod.consts["Service"] = cls
	vm.consts["ActiveStorage::Service"] = cls

	disk := newClass("ActiveStorage::Service::DiskService", cls)
	cls.consts["DiskService"] = disk
	vm.consts["ActiveStorage::Service::DiskService"] = disk

	self := func(v object.Value) activestorage.Service { return v.(*ASService).s }

	cls.define("name", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).Name())
	})
	cls.define("exist?", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		ok, err := self(v).Exist(asPosStr(args, 0))
		if err != nil {
			asRaise(err)
		}
		return object.Bool(ok)
	})
	cls.define("size", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		n, err := self(v).Size(asPosStr(args, 0))
		if err != nil {
			asRaise(err)
		}
		return object.IntValue(n)
	})
	cls.define("delete", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if err := self(v).Delete(asPosStr(args, 0)); err != nil {
			asRaise(err)
		}
		return object.NilV
	})
	cls.define("url", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		u, err := self(v).Url(asPosStr(args, 0), asURLOptions(asKwargs(args)))
		if err != nil {
			asRaise(err)
		}
		return object.NewString(u)
	})
	cls.define("upload", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if err := self(v).Upload(asPosStr(args, 0), asReader(argAt(args, 1)), asKwString(asKwargs(args), "checksum")); err != nil {
			asRaise(err)
		}
		return object.NilV
	})
}

// registerActiveStorageAttachment installs ActiveStorage::Attachment: the join
// record's readers and #purge.
func (vm *VM) registerActiveStorageAttachment(mod *RClass) {
	cls := newClass("ActiveStorage::Attachment", vm.cObject)
	mod.consts["Attachment"] = cls
	vm.consts["ActiveStorage::Attachment"] = cls

	self := func(v object.Value) *activestorage.Attachment { return v.(*ASAttachment).a }

	cls.define("id", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(self(v).ID)
	})
	cls.define("name", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).Name)
	})
	cls.define("record_type", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).RecordType)
	})
	cls.define("record_id", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(self(v).RecordID)
	})
	cls.define("blob_id", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(self(v).BlobID)
	})
	cls.define("purge", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		if err := self(v).Purge(); err != nil {
			asRaise(err)
		}
		return object.NilV
	})
}

// registerActiveStorageAttached installs the ActiveStorage::Attached module and
// the has_one_attached / has_many_attached proxies Attached::One and
// Attached::Many, each constructed for a (record_type, record_id, name) owner.
func (vm *VM) registerActiveStorageAttached(mod *RClass) {
	att := newClass("ActiveStorage::Attached", nil)
	att.isModule = true
	mod.consts["Attached"] = att
	vm.consts["ActiveStorage::Attached"] = att

	vm.registerActiveStorageOne(att)
	vm.registerActiveStorageMany(att)
}

// registerActiveStorageOne installs ActiveStorage::Attached::One — the
// has_one_attached proxy.
func (vm *VM) registerActiveStorageOne(att *RClass) {
	cls := newClass("ActiveStorage::Attached::One", vm.cObject)
	att.consts["One"] = cls
	vm.consts["ActiveStorage::Attached::One"] = cls

	asSM(cls, "new", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		kw := asKwargs(args)
		return &ASOne{o: vm.asRequireConfig().One(asRecordRef(kw), asKwString(kw, "name"))}
	})

	self := func(v object.Value) *activestorage.OneAttached { return v.(*ASOne).o }

	cls.define("attach", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		a, err := self(v).Attach(asAttachable(argAt(args, 0)))
		if err != nil {
			asRaise(err)
		}
		return &ASAttachment{a: a}
	})
	cls.define("attached?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		ok, err := self(v).Attached()
		if err != nil {
			asRaise(err)
		}
		return object.Bool(ok)
	})
	cls.define("attachment", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		a, err := self(v).Attachment()
		if err != nil {
			asRaise(err)
		}
		return asAttachmentValue(a)
	})
	cls.define("blob", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		b, err := self(v).Blob()
		if err != nil {
			asRaise(err)
		}
		return asBlobValue(b)
	})
	cls.define("detach", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		if err := self(v).Detach(); err != nil {
			asRaise(err)
		}
		return object.NilV
	})
	cls.define("purge", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		if err := self(v).Purge(); err != nil {
			asRaise(err)
		}
		return object.NilV
	})
}

// registerActiveStorageMany installs ActiveStorage::Attached::Many — the
// has_many_attached proxy.
func (vm *VM) registerActiveStorageMany(att *RClass) {
	cls := newClass("ActiveStorage::Attached::Many", vm.cObject)
	att.consts["Many"] = cls
	vm.consts["ActiveStorage::Attached::Many"] = cls

	asSM(cls, "new", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		kw := asKwargs(args)
		return &ASMany{m: vm.asRequireConfig().Many(asRecordRef(kw), asKwString(kw, "name"))}
	})

	self := func(v object.Value) *activestorage.ManyAttached { return v.(*ASMany).m }
	attachments := func(atts []*activestorage.Attachment) object.Value {
		arr := object.NewArrayFromSlice(make([]object.Value, len(atts)))
		for i, a := range atts {
			arr.Elems[i] = &ASAttachment{a: a}
		}
		return arr
	}

	cls.define("attach", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		attachables := make([]any, len(args))
		for i, a := range args {
			attachables[i] = asAttachable(a)
		}
		atts, err := self(v).Attach(attachables...)
		if err != nil {
			asRaise(err)
		}
		return attachments(atts)
	})
	cls.define("attachments", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		atts, err := self(v).Attachments()
		if err != nil {
			asRaise(err)
		}
		return attachments(atts)
	})
	cls.define("attached?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		ok, err := self(v).Attached()
		if err != nil {
			asRaise(err)
		}
		return object.Bool(ok)
	})
	cls.define("blobs", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		blobs, err := self(v).Blobs()
		if err != nil {
			asRaise(err)
		}
		arr := object.NewArrayFromSlice(make([]object.Value, len(blobs)))
		for i, b := range blobs {
			arr.Elems[i] = &ASBlob{b: b}
		}
		return arr
	})
	cls.define("detach", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		if err := self(v).Detach(); err != nil {
			asRaise(err)
		}
		return object.NilV
	})
	cls.define("purge", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		if err := self(v).Purge(); err != nil {
			asRaise(err)
		}
		return object.NilV
	})
}
