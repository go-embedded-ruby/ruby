// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"time"

	sftp "github.com/go-ruby-net-sftp/net-sftp"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the value-mapping half of the Net::SFTP binding: the channel-seam
// helpers, the Ruby argument decoding, the Attributes / Name value objects, the
// StatusException raise, and the Session / dir / file method surfaces. net_sftp.go
// owns the driver and registration; here every Ruby method closure maps its
// arguments onto the session's driver and its reply back into the object graph.

// --- channel seam ----------------------------------------------------------

// sftpReadBytes extracts the byte payload a Ruby channel #read returned: a String
// yields its bytes, anything else (nil at EOF) yields none.
func sftpReadBytes(v object.Value) []byte {
	if s, ok := v.(*object.String); ok {
		return s.Bytes()
	}
	return nil
}

// sftpChannelArg resolves the injected SSH-channel object from the call
// arguments: the channel: / connection: keyword of a trailing Hash (the
// Net::SFTP.start form, where the positionals are host / user strings), else the
// first non-String positional (the Session.new(channel) form). String positionals
// are the ignored host / user; a Hash is the keyword set. Returns nil when no
// channel is supplied.
func sftpChannelArg(args []object.Value) object.Value {
	if len(args) > 0 {
		if h, ok := args[len(args)-1].(*object.Hash); ok {
			if v, ok := h.Get(object.Symbol("channel")); ok {
				return v
			}
			if v, ok := h.Get(object.NewString("channel")); ok {
				return v
			}
			if v, ok := h.Get(object.Symbol("connection")); ok {
				return v
			}
			if v, ok := h.Get(object.NewString("connection")); ok {
				return v
			}
			args = args[:len(args)-1]
		}
	}
	for _, a := range args {
		if _, isStr := a.(*object.String); isStr {
			continue
		}
		if _, isHash := a.(*object.Hash); isHash {
			continue
		}
		return a
	}
	return nil
}

// --- argument helpers ------------------------------------------------------

// sftpBytes returns the raw bytes of a Ruby String argument (a handle or data
// payload), raising TypeError otherwise.
func sftpBytes(v object.Value) []byte {
	if s, ok := v.(*object.String); ok {
		return s.Bytes()
	}
	raise("TypeError", "expected a String")
	return nil
}

// sftpStr returns the contents of a Ruby String argument, raising TypeError
// otherwise.
func sftpStr(v object.Value) string {
	if s, ok := v.(*object.String); ok {
		return s.Str()
	}
	raise("TypeError", "expected a String")
	return ""
}

// sftpUint64 coerces a Ruby Integer to uint64, raising TypeError otherwise.
func sftpUint64(v object.Value) uint64 {
	if n, ok := v.(object.Integer); ok {
		return uint64(int64(n))
	}
	raise("TypeError", "expected an Integer")
	return 0
}

// sftpUint32 coerces a Ruby Integer to uint32, raising TypeError otherwise.
func sftpUint32(v object.Value) uint32 {
	if n, ok := v.(object.Integer); ok {
		return uint32(int64(n))
	}
	raise("TypeError", "expected an Integer")
	return 0
}

// sftpArg returns args[i] or nil when the slice is shorter, so optional trailing
// arguments (flags, mode, attribute Hash) collapse to their defaults.
func sftpArg(args []object.Value, i int) object.Value {
	if i < len(args) {
		return args[i]
	}
	return nil
}

// sftpModeArg returns the open-mode string of an optional argument, defaulting to
// "r" when absent or nil.
func sftpModeArg(v object.Value) string {
	if v == nil || v == object.NilV {
		return "r"
	}
	return sftpStr(v)
}

// sftpAttrsArg builds an Attributes value from an optional Ruby Hash of
// attribute keywords, or an empty structure when absent.
func sftpAttrsArg(v object.Value) *sftp.Attributes {
	a := &sftp.Attributes{}
	h, ok := v.(*object.Hash)
	if !ok {
		return a
	}
	get := func(name string) (object.Value, bool) {
		if hv, ok := h.Get(object.Symbol(name)); ok {
			return hv, true
		}
		return h.Get(object.NewString(name))
	}
	if v, ok := get("size"); ok {
		n := sftpUint64(v)
		a.Size = &n
	}
	if v, ok := get("permissions"); ok {
		n := sftpUint32(v)
		a.Permissions = &n
	}
	if v, ok := get("uid"); ok {
		n := sftpUint32(v)
		a.UID = &n
	}
	if v, ok := get("gid"); ok {
		n := sftpUint32(v)
		a.GID = &n
	}
	if v, ok := get("owner"); ok {
		s := sftpStr(v)
		a.Owner = &s
	}
	if v, ok := get("group"); ok {
		s := sftpStr(v)
		a.Group = &s
	}
	if v, ok := get("atime"); ok {
		n := sftpUint64(v)
		a.Atime = &n
	}
	if v, ok := get("mtime"); ok {
		n := sftpUint64(v)
		a.Mtime = &n
	}
	return a
}

// --- response id + status --------------------------------------------------

// sftpRespID returns the echoed request id carried by any parsed response, so the
// driver can correlate it to the request.
func sftpRespID(resp any) uint32 {
	switch r := resp.(type) {
	case *sftp.StatusResponse:
		return r.ID
	case *sftp.HandleResponse:
		return r.ID
	case *sftp.DataResponse:
		return r.ID
	case *sftp.NameResponse:
		return r.ID
	default:
		return resp.(*sftp.AttrsResponse).ID
	}
}

// raiseStatus builds and raises the gem-faithful Net::SFTP::StatusException from a
// non-success FXP_STATUS, carrying its code / description / text. It never returns.
func (s *sftpSession) raiseStatus(st *sftp.StatusResponse, op string) object.Value {
	se := sftp.NewStatusException(st, op)
	obj := &RObject{class: s.vm.sftpClasses.statusExc, ivars: map[string]object.Value{
		"@code":        object.IntValue(int64(se.Code)),
		"@description": object.NewString(se.Description),
		"@text":        object.NewString(se.Text),
		"@message":     object.NewString(se.Error()),
	}}
	panic(s.vm.excError(obj))
}

// defineStatusExceptionMethods installs code / description / text / message /
// to_s on Net::SFTP::StatusException, each reading the ivar set at raise time.
func (vm *VM) defineStatusExceptionMethods(cls *RClass) {
	cls.define("code", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return getIvar(self, "@code")
	})
	cls.define("description", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return getIvar(self, "@description")
	})
	cls.define("text", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return getIvar(self, "@text")
	})
	msg := func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return getIvar(self, "@message")
	}
	cls.define("message", msg)
	cls.define("to_s", msg)
}

// --- value objects: Attributes ---------------------------------------------

// sftpAttrs wraps a decoded *sftp.Attributes as a Ruby
// Net::SFTP::Protocol::V01::Attributes value.
type sftpAttrs struct {
	cls *RClass
	a   *sftp.Attributes
}

func (a *sftpAttrs) ToS() string     { return "#<Net::SFTP::Protocol::V01::Attributes>" }
func (a *sftpAttrs) Inspect() string { return "#<Net::SFTP::Protocol::V01::Attributes>" }
func (a *sftpAttrs) Truthy() bool    { return true }

// newAttrs stamps the Attributes wrapper class on a decoded attribute structure.
func (s *sftpSession) newAttrs(a *sftp.Attributes) object.Value {
	return &sftpAttrs{cls: s.vm.sftpClasses.attrs, a: a}
}

// optUint64 maps an optional 64-bit field to a Ruby Integer or nil.
func optUint64(p *uint64) object.Value {
	if p == nil {
		return object.NilV
	}
	return object.IntValue(int64(*p))
}

// optUint32 maps an optional 32-bit field to a Ruby Integer or nil.
func optUint32(p *uint32) object.Value {
	if p == nil {
		return object.NilV
	}
	return object.IntValue(int64(*p))
}

// optString maps an optional string field to a Ruby String or nil.
func optString(p *string) object.Value {
	if p == nil {
		return object.NilV
	}
	return object.NewString(*p)
}

// registerNetSFTPAttributes installs the Attributes value class and its accessors
// (size/uid/gid/owner/group/permissions/atime/mtime plus the type predicates).
func (vm *VM) registerNetSFTPAttributes(mod *RClass) *RClass {
	cls := newClass("Net::SFTP::Protocol::V01::Attributes", vm.cObject)
	vm.consts["Net::SFTP::Protocol::V01::Attributes"] = cls
	mod.consts["Attributes"] = cls
	at := func(v object.Value) *sftp.Attributes { return v.(*sftpAttrs).a }
	cls.define("size", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return optUint64(at(self).Size)
	})
	cls.define("uid", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return optUint32(at(self).UID)
	})
	cls.define("gid", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return optUint32(at(self).GID)
	})
	cls.define("owner", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return optString(at(self).Owner)
	})
	cls.define("group", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return optString(at(self).Group)
	})
	cls.define("permissions", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return optUint32(at(self).Permissions)
	})
	cls.define("atime", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return optUint64(at(self).Atime)
	})
	cls.define("mtime", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return optUint64(at(self).Mtime)
	})
	cls.define("type", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(at(self).FileType()))
	})
	cls.define("directory?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		val, _ := at(self).IsDirectory()
		return object.Bool(val)
	})
	cls.define("symlink?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		val, _ := at(self).IsSymlink()
		return object.Bool(val)
	})
	cls.define("file?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		val, _ := at(self).IsFile()
		return object.Bool(val)
	})
	return cls
}

// --- value objects: Name ---------------------------------------------------

// sftpName wraps one FXP_NAME entry as a Ruby Net::SFTP::Name value.
type sftpName struct {
	cls     *RClass
	n       sftp.Name
	version int
}

func (n *sftpName) ToS() string     { return "#<Net::SFTP::Name>" }
func (n *sftpName) Inspect() string { return "#<Net::SFTP::Name>" }
func (n *sftpName) Truthy() bool    { return true }

// newName stamps the Name wrapper class on a directory entry.
func (s *sftpSession) newName(n sftp.Name) object.Value {
	return &sftpName{cls: s.vm.sftpClasses.name, n: n, version: s.version}
}

// defineNetSFTPNameMethods installs the Name accessors (name/filename/longname/
// attributes plus the type predicates).
func (vm *VM) defineNetSFTPNameMethods(cls *RClass) {
	nm := func(v object.Value) *sftpName { return v.(*sftpName) }
	name := func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(nm(self).n.Filename)
	}
	cls.define("name", name)
	cls.define("filename", name)
	cls.define("longname", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		e := nm(self)
		if e.n.Longname != "" {
			return object.NewString(e.n.Longname)
		}
		return object.NewString(e.n.LongnameFor(time.Local))
	})
	cls.define("attributes", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return &sftpAttrs{cls: vm.sftpClasses.attrs, a: nm(self).n.Attributes}
	})
	cls.define("directory?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		val, _ := nm(self).n.IsDirectory()
		return object.Bool(val)
	})
	cls.define("symlink?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		val, _ := nm(self).n.IsSymlink()
		return object.Bool(val)
	})
	cls.define("file?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		val, _ := nm(self).n.IsFile()
		return object.Bool(val)
	})
}

// --- Session methods -------------------------------------------------------

// asSession unwraps a Session receiver.
func asSession(v object.Value) *sftpSession { return v.(*sftpSession) }

// defineNetSFTPSessionMethods installs the full Session request surface: the
// version accessor, the primitive bang-methods (open!/close!/read!/write!/…), the
// higher-level upload!/download!, and the dir / file operation-helper accessors.
func (vm *VM) defineNetSFTPSessionMethods(cls *RClass) {
	cls.define("protocol_version", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(asSession(self).version))
	})
	cls.define("version", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(asSession(self).version))
	})

	cls.define("open!", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		s := asSession(self)
		path := sftpStr(sftpArg(args, 0))
		attrs := sftpAttrsArg(sftpArg(args, 2))
		h := s.openHandle(path, sftpModeArg(sftpArg(args, 1)), attrs)
		return object.NewStringBytesEnc(append([]byte(nil), h...), "ASCII-8BIT")
	})
	cls.define("close!", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return asSession(self).closeHandle(sftpBytes(sftpArg(args, 0)))
	})
	cls.define("read!", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		s := asSession(self)
		h := sftpBytes(sftpArg(args, 0))
		off := sftpUint64(sftpArg(args, 1))
		length := sftpUint32(sftpArg(args, 2))
		data, ok := s.readAt(h, off, length)
		if !ok {
			return object.NilV
		}
		return object.NewStringBytesEnc(append([]byte(nil), data...), "ASCII-8BIT")
	})
	cls.define("write!", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		s := asSession(self)
		h := sftpBytes(sftpArg(args, 0))
		off := sftpUint64(sftpArg(args, 1))
		data := sftpBytes(sftpArg(args, 2))
		id, frame := s.proto.Write(h, off, data)
		return s.okOrRaise(s.response(id, frame), "write")
	})

	vm.defineNetSFTPStatMethods(cls)
	vm.defineNetSFTPDirEntryMethods(cls)
	vm.defineNetSFTPLinkMethods(cls)
	vm.defineNetSFTPTransferMethods(cls)

	cls.define("dir", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return &sftpDir{cls: vm.sftpClasses.dir, s: asSession(self)}
	})
	cls.define("file", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return &sftpFileFactory{cls: vm.sftpClasses.fileFactory, s: asSession(self)}
	})
}

// defineNetSFTPStatMethods installs stat!/lstat!/fstat! and setstat!/fsetstat!.
func (vm *VM) defineNetSFTPStatMethods(cls *RClass) {
	statPath := func(self object.Value, args []object.Value, op string, build func(*sftpSession, string) (uint32, []byte)) object.Value {
		s := asSession(self)
		id, frame := build(s, sftpStr(sftpArg(args, 0)))
		resp := s.response(id, frame)
		if st, ok := resp.(*sftp.StatusResponse); ok {
			return s.raiseStatus(st, op)
		}
		return s.newAttrs(resp.(*sftp.AttrsResponse).Attrs)
	}
	cls.define("stat!", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return statPath(self, args, "stat", func(s *sftpSession, p string) (uint32, []byte) { return s.proto.Stat(p, nil) })
	})
	cls.define("lstat!", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return statPath(self, args, "lstat", func(s *sftpSession, p string) (uint32, []byte) { return s.proto.Lstat(p, nil) })
	})
	cls.define("fstat!", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		s := asSession(self)
		id, frame := s.proto.Fstat(sftpBytes(sftpArg(args, 0)), nil)
		resp := s.response(id, frame)
		if st, ok := resp.(*sftp.StatusResponse); ok {
			return s.raiseStatus(st, "fstat")
		}
		return s.newAttrs(resp.(*sftp.AttrsResponse).Attrs)
	})
	cls.define("setstat!", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		s := asSession(self)
		id, frame := s.proto.Setstat(sftpStr(sftpArg(args, 0)), sftpAttrsArg(sftpArg(args, 1)))
		return s.okOrRaise(s.response(id, frame), "setstat")
	})
	cls.define("fsetstat!", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		s := asSession(self)
		id, frame := s.proto.Fsetstat(sftpBytes(sftpArg(args, 0)), sftpAttrsArg(sftpArg(args, 1)))
		return s.okOrRaise(s.response(id, frame), "fsetstat")
	})
}

// defineNetSFTPDirEntryMethods installs opendir!/readdir!/mkdir!/rmdir!/remove!/
// realpath!.
func (vm *VM) defineNetSFTPDirEntryMethods(cls *RClass) {
	cls.define("opendir!", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		s := asSession(self)
		id, frame := s.proto.Opendir(sftpStr(sftpArg(args, 0)))
		h := s.handleOf(s.response(id, frame), "opendir")
		return object.NewStringBytesEnc(append([]byte(nil), h...), "ASCII-8BIT")
	})
	cls.define("readdir!", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		s := asSession(self)
		h := sftpBytes(sftpArg(args, 0))
		id, frame := s.proto.Readdir(h)
		resp := s.response(id, frame)
		if st, ok := resp.(*sftp.StatusResponse); ok {
			if st.EOF() {
				return object.NilV
			}
			return s.raiseStatus(st, "readdir")
		}
		nr := resp.(*sftp.NameResponse)
		arr := object.NewArray()
		for _, n := range nr.Names {
			arr.Elems = append(arr.Elems, s.newName(n))
		}
		return arr
	})
	cls.define("remove!", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		s := asSession(self)
		id, frame := s.proto.Remove(sftpStr(sftpArg(args, 0)))
		return s.okOrRaise(s.response(id, frame), "remove")
	})
	cls.define("mkdir!", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		s := asSession(self)
		id, frame := s.proto.Mkdir(sftpStr(sftpArg(args, 0)), sftpAttrsArg(sftpArg(args, 1)))
		return s.okOrRaise(s.response(id, frame), "mkdir")
	})
	cls.define("rmdir!", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		s := asSession(self)
		id, frame := s.proto.Rmdir(sftpStr(sftpArg(args, 0)))
		return s.okOrRaise(s.response(id, frame), "rmdir")
	})
	cls.define("realpath!", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		s := asSession(self)
		id, frame := s.proto.Realpath(sftpStr(sftpArg(args, 0)))
		resp := s.response(id, frame)
		if st, ok := resp.(*sftp.StatusResponse); ok {
			return s.raiseStatus(st, "realpath")
		}
		return s.firstName(resp.(*sftp.NameResponse), "realpath")
	})
}

// firstName returns the first Name entry of a single-entry FXP_NAME response
// (realpath/readlink), raising when the server returned none.
func (s *sftpSession) firstName(nr *sftp.NameResponse, op string) object.Value {
	if len(nr.Names) == 0 {
		raise("Net::SFTP::Exception", "%s returned no name entry", op)
	}
	return object.NewString(nr.Names[0].Filename)
}

// defineNetSFTPLinkMethods installs rename!/readlink!/symlink!/link!/block!/
// unblock! — the operations the library gates by protocol version.
func (vm *VM) defineNetSFTPLinkMethods(cls *RClass) {
	cls.define("rename!", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		s := asSession(self)
		id, frame, err := s.proto.Rename(sftpStr(sftpArg(args, 0)), sftpStr(sftpArg(args, 1)), nil)
		if err != nil {
			raise("Net::SFTP::Exception", "%s", err.Error())
		}
		return s.okOrRaise(s.response(id, frame), "rename")
	})
	cls.define("readlink!", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		s := asSession(self)
		id, frame, err := s.proto.Readlink(sftpStr(sftpArg(args, 0)))
		if err != nil {
			raise("Net::SFTP::Exception", "%s", err.Error())
		}
		resp := s.response(id, frame)
		if st, ok := resp.(*sftp.StatusResponse); ok {
			return s.raiseStatus(st, "readlink")
		}
		return s.firstName(resp.(*sftp.NameResponse), "readlink")
	})
	cls.define("symlink!", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		s := asSession(self)
		id, frame, err := s.proto.Symlink(sftpStr(sftpArg(args, 0)), sftpStr(sftpArg(args, 1)))
		if err != nil {
			raise("Net::SFTP::Exception", "%s", err.Error())
		}
		return s.okOrRaise(s.response(id, frame), "symlink")
	})
	cls.define("link!", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		s := asSession(self)
		sym := sftpArg(args, 2)
		symlink := !object.IsNil(sym) && sym.Truthy()
		id, frame, err := s.proto.Link(sftpStr(sftpArg(args, 0)), sftpStr(sftpArg(args, 1)), symlink)
		if err != nil {
			raise("Net::SFTP::Exception", "%s", err.Error())
		}
		return s.okOrRaise(s.response(id, frame), "link")
	})
	cls.define("block!", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		s := asSession(self)
		id, frame, err := s.proto.Block(sftpBytes(sftpArg(args, 0)), sftpUint64(sftpArg(args, 1)), sftpUint64(sftpArg(args, 2)), sftpUint32(sftpArg(args, 3)))
		if err != nil {
			raise("Net::SFTP::Exception", "%s", err.Error())
		}
		return s.okOrRaise(s.response(id, frame), "block")
	})
	cls.define("unblock!", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		s := asSession(self)
		id, frame, err := s.proto.Unblock(sftpBytes(sftpArg(args, 0)), sftpUint64(sftpArg(args, 1)), sftpUint64(sftpArg(args, 2)))
		if err != nil {
			raise("Net::SFTP::Exception", "%s", err.Error())
		}
		return s.okOrRaise(s.response(id, frame), "unblock")
	})
}

// defineNetSFTPTransferMethods installs upload!/download! — whole-file transfers
// built over the open/read/write/close primitives.
func (vm *VM) defineNetSFTPTransferMethods(cls *RClass) {
	cls.define("download!", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		s := asSession(self)
		h := s.openHandle(sftpStr(sftpArg(args, 0)), "r", &sftp.Attributes{})
		var buf []byte
		var off uint64
		for {
			data, ok := s.readAt(h, off, sftpDownloadChunk)
			if !ok {
				break
			}
			buf = append(buf, data...)
			off += uint64(len(data))
		}
		s.closeHandle(h)
		return object.NewStringBytesEnc(buf, "ASCII-8BIT")
	})
	cls.define("upload!", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		s := asSession(self)
		data := sftpUploadData(vm, sftpArg(args, 0))
		h := s.openHandle(sftpStr(sftpArg(args, 1)), "w", &sftp.Attributes{})
		id, frame := s.proto.Write(h, 0, data)
		s.okOrRaise(s.response(id, frame), "write")
		return s.closeHandle(h)
	})
}

// sftpUploadData reads the upload source: a String is its bytes; any object
// responding to #read is drained via #read.
func sftpUploadData(vm *VM, v object.Value) []byte {
	if s, ok := v.(*object.String); ok {
		return s.Bytes()
	}
	rv := vm.send(v, "read", nil, nil)
	return sftpBytes(rv)
}

// --- dir operations --------------------------------------------------------

// defineNetSFTPDirMethods installs Net::SFTP::Operations::Dir#foreach / #entries /
// #glob over the owning session.
func (vm *VM) defineNetSFTPDirMethods(cls *RClass) {
	entries := func(self object.Value, path string) []sftp.Name {
		d := self.(*sftpDir)
		id, frame := d.s.proto.Opendir(path)
		h := d.s.handleOf(d.s.response(id, frame), "opendir")
		names := d.s.readdirAll(h)
		d.s.closeHandle(h)
		return names
	}
	cls.define("entries", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		d := self.(*sftpDir)
		arr := object.NewArray()
		for _, n := range entries(self, sftpStr(sftpArg(args, 0))) {
			arr.Elems = append(arr.Elems, d.s.newName(n))
		}
		return arr
	})
	cls.define("foreach", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (yield)")
		}
		d := self.(*sftpDir)
		for _, n := range entries(self, sftpStr(sftpArg(args, 0))) {
			vm.callBlock(blk, []object.Value{d.s.newName(n)})
		}
		return object.NilV
	})
}

// --- file operations -------------------------------------------------------

// defineNetSFTPFileFactoryMethods installs Net::SFTP::Operations::FileFactory#open
// and #directory?.
func (vm *VM) defineNetSFTPFileFactoryMethods(cls *RClass) {
	cls.define("open", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		ff := self.(*sftpFileFactory)
		path := sftpStr(sftpArg(args, 0))
		attrs := sftpAttrsArg(sftpArg(args, 2))
		h := ff.s.openHandle(path, sftpModeArg(sftpArg(args, 1)), attrs)
		f := &sftpFile{cls: vm.sftpClasses.file, s: ff.s, handle: h}
		if blk == nil {
			return f
		}
		res := vm.callBlock(blk, []object.Value{f})
		f.doClose()
		return res
	})
	cls.define("directory?", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		ff := self.(*sftpFileFactory)
		id, frame := ff.s.proto.Stat(sftpStr(sftpArg(args, 0)), nil)
		resp := ff.s.response(id, frame)
		if _, ok := resp.(*sftp.StatusResponse); ok {
			return object.Bool(false)
		}
		val, _ := resp.(*sftp.AttrsResponse).Attrs.IsDirectory()
		return object.Bool(val)
	})
}

// doClose closes the file's handle once, tolerating a repeated #close.
func (f *sftpFile) doClose() object.Value {
	if f.closed {
		return object.NilV
	}
	f.closed = true
	return f.s.closeHandle(f.handle)
}

// defineNetSFTPFileMethods installs Net::SFTP::Operations::File#read / #write /
// #close / #pos / #eof?.
func (vm *VM) defineNetSFTPFileMethods(cls *RClass) {
	fl := func(v object.Value) *sftpFile { return v.(*sftpFile) }
	cls.define("read", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		f := fl(self)
		if n := sftpArg(args, 0); n != nil && n != object.NilV {
			data, ok := f.s.readAt(f.handle, f.pos, sftpUint32(n))
			if !ok {
				return object.NilV
			}
			f.pos += uint64(len(data))
			return object.NewStringBytesEnc(append([]byte(nil), data...), "ASCII-8BIT")
		}
		// No length: read to EOF.
		var buf []byte
		for {
			data, ok := f.s.readAt(f.handle, f.pos, sftpDownloadChunk)
			if !ok {
				break
			}
			buf = append(buf, data...)
			f.pos += uint64(len(data))
		}
		return object.NewStringBytesEnc(buf, "ASCII-8BIT")
	})
	cls.define("write", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		f := fl(self)
		data := sftpBytes(sftpArg(args, 0))
		id, frame := f.s.proto.Write(f.handle, f.pos, data)
		f.s.okOrRaise(f.s.response(id, frame), "write")
		f.pos += uint64(len(data))
		return object.IntValue(int64(len(data)))
	})
	cls.define("pos", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(fl(self).pos))
	})
	cls.define("close", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return fl(self).doClose()
	})
}
