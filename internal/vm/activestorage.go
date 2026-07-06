// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"
	"os"
	"time"

	activestorage "github.com/go-ruby-activestorage/activestorage"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file binds github.com/go-ruby-activestorage/activestorage (require
// "active_storage"): ActiveStorage::Blob.create_and_upload! /
// create_before_direct_upload! / find / find_signed, a blob's #signed_id /
// #download / #url / #purge, ActiveStorage::Service (+ the built-in disk service),
// and the has_one_attached / has_many_attached proxies
// ActiveStorage::Attached::One / ::Many.
//
// The library abstracts persistence (ModelStore), storage (Service), signing
// (Signer), randomness (RandomSource) and the clock behind a *activestorage.Config
// so the model logic has no external dependency. The binding wires a
// self-contained, deterministic process config (see asRequireConfig): an
// in-memory MemStore, a DiskService rooted at a per-VM temp directory, an
// HMAC-SHA256 signer with a fixed key, a seeded RandomSource (reproducible
// storage keys) and a fixed clock — no external services, everything in-process.

// ASBlob is the Ruby wrapper around a *activestorage.Blob — the persisted
// description of an uploaded file (its sharded storage key, filename, content
// type, byte size and base64 MD5 checksum). It mirrors ActiveStorage::Blob.
type ASBlob struct{ b *activestorage.Blob }

func (b *ASBlob) ToS() string {
	return "#<ActiveStorage::Blob key: " + b.b.Key + " filename: " + b.b.Filename + ">"
}
func (b *ASBlob) Inspect() string { return b.ToS() }
func (b *ASBlob) Truthy() bool    { return true }

// ASAttachment is the Ruby wrapper around a *activestorage.Attachment — the join
// record binding a blob to a named association on a record
// (ActiveStorage::Attachment).
type ASAttachment struct{ a *activestorage.Attachment }

func (a *ASAttachment) ToS() string     { return "#<ActiveStorage::Attachment name: " + a.a.Name + ">" }
func (a *ASAttachment) Inspect() string { return a.ToS() }
func (a *ASAttachment) Truthy() bool    { return true }

// ASService is the Ruby wrapper around an activestorage.Service — a storage
// backend (concretely the built-in DiskService). It exposes the key-addressed
// contract ActiveStorage::Service defines.
type ASService struct{ s activestorage.Service }

func (s *ASService) ToS() string     { return "#<ActiveStorage::Service name: " + s.s.Name() + ">" }
func (s *ASService) Inspect() string { return s.ToS() }
func (s *ASService) Truthy() bool    { return true }

// ASOne is the Ruby wrapper around a *activestorage.OneAttached — the
// has_one_attached proxy for a single named attachment on a record.
type ASOne struct{ o *activestorage.OneAttached }

func (o *ASOne) ToS() string     { return "#<ActiveStorage::Attached::One>" }
func (o *ASOne) Inspect() string { return o.ToS() }
func (o *ASOne) Truthy() bool    { return true }

// ASMany is the Ruby wrapper around a *activestorage.ManyAttached — the
// has_many_attached proxy for an ordered set of named attachments on a record.
type ASMany struct{ m *activestorage.ManyAttached }

func (m *ASMany) ToS() string     { return "#<ActiveStorage::Attached::Many>" }
func (m *ASMany) Inspect() string { return m.ToS() }
func (m *ASMany) Truthy() bool    { return true }

// asMkdirTemp is the temp-directory creation seam used to root the per-VM disk
// service; indirected so the suite can exercise its failure branch.
var asMkdirTemp = os.MkdirTemp

// asFixedClock is the deterministic clock the binding installs so a blob's
// CreatedAt is reproducible in tests.
func asFixedClock() time.Time { return time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC) }

// asRequireConfig returns the process ActiveStorage config, building it (and its
// per-VM disk-service temp directory) on first use. It raises
// ActiveStorage::Error if the temp directory cannot be created.
func (vm *VM) asRequireConfig() *activestorage.Config {
	if vm.asConfig != nil {
		return vm.asConfig
	}
	dir, err := asMkdirTemp("", "rbgo-activestorage-")
	if err != nil {
		raise("ActiveStorage::Error", "%s", err.Error())
	}
	reg := activestorage.NewRegistry()
	reg.Register(activestorage.NewDiskService("local", dir))
	vm.asConfig = &activestorage.Config{
		Store:    activestorage.NewMemStore(),
		Services: reg,
		Signer:   activestorage.NewHMACSigner([]byte("rbgo-activestorage-secret")),
		Random:   &asDetRandom{state: 0x1234_5678_9abc_def0},
		Clock:    asFixedClock,
	}
	return vm.asConfig
}

// asRaise maps a library error onto the ActiveStorage Ruby exception tree and
// raises it: an integrity mismatch, an invalid signed id, an unattachable value,
// a missing blob/attachment, and every other failure as the base error.
func asRaise(err error) object.Value {
	switch {
	case errors.Is(err, activestorage.ErrIntegrity):
		return raise("ActiveStorage::IntegrityError", "%s", err.Error())
	case errors.Is(err, activestorage.ErrInvalidSignature):
		return raise("ActiveStorage::InvalidSignature", "%s", err.Error())
	case errors.Is(err, activestorage.ErrUnattachable):
		return raise("ActiveStorage::UnattachableError", "%s", err.Error())
	case errors.Is(err, activestorage.ErrBlobNotFound), errors.Is(err, activestorage.ErrAttachmentNotFound):
		return raise("ActiveStorage::FileNotFoundError", "%s", err.Error())
	default:
		return raise("ActiveStorage::Error", "%s", err.Error())
	}
}

// registerActiveStorage installs the ActiveStorage module, its error tree, and
// the Blob / Service / Attachment / Attached::One / Attached::Many surface
// (require "active_storage"). Persistence, storage, signing, randomness and the
// clock are wired to the deterministic in-process config asRequireConfig builds.
func (vm *VM) registerActiveStorage() {
	mod := newClass("ActiveStorage", nil)
	mod.isModule = true
	vm.consts["ActiveStorage"] = mod

	vm.registerActiveStorageErrors(mod)
	vm.registerActiveStorageBlob(mod)
	vm.registerActiveStorageService(mod)
	vm.registerActiveStorageAttachment(mod)
	vm.registerActiveStorageAttached(mod)
}

// registerActiveStorageErrors installs the ActiveStorage error tree:
// ActiveStorage::Error < StandardError, with IntegrityError, InvalidSignature,
// UnattachableError and FileNotFoundError under it.
func (vm *VM) registerActiveStorageErrors(mod *RClass) {
	std := vm.consts["StandardError"].(*RClass)
	base := newClass("ActiveStorage::Error", std)
	mod.consts["Error"] = base
	vm.consts["ActiveStorage::Error"] = base

	for _, name := range []string{"IntegrityError", "InvalidSignature", "UnattachableError", "FileNotFoundError"} {
		c := newClass("ActiveStorage::"+name, base)
		mod.consts[name] = c
		vm.consts["ActiveStorage::"+name] = c
	}
}
