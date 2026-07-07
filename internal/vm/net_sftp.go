// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	sftp "github.com/go-ruby-net-sftp/net-sftp"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file binds the pure-Go SFTP wire codec github.com/go-ruby-net-sftp/net-sftp
// into rbgo as require "net/sftp" — the Ruby Net::SFTP surface (Session#open!/
// read!/write!/stat!/…, upload!/download!, the dir and file operation helpers).
//
// SFTP runs over an SSH channel. The library owns only the deterministic packet
// codec (framing, the version-aware ATTRS struct, request-id correlation, version
// negotiation); the encrypted SSH transport is the host's job — the "channel
// seam". rbgo has no go-ruby-ssh yet, so Net::SFTP.start takes an injected channel
// object responding to #write(bytes) / #read(n) (a duck-typed SSH channel, e.g. a
// StringIO in tests), exactly as Redis.new takes an injected IO connection. The
// binding drives the library's request builders and response parsers over that
// channel: it writes the FXP_INIT frame, negotiates the version from FXP_VERSION,
// then for each operation writes the request frame and reads the correlated
// response back. sftpChannel (net_sftp_bind.go) bridges the Ruby duck to the byte
// stream a PacketParser reassembles.

// sftpReadChunk is the number of bytes the driver requests from the channel's
// #read per fill; the PacketParser reassembles whole packets from the chunks.
const sftpReadChunk = 32768

// sftpDownloadChunk is the per-FXP_READ request size used by download!/file read.
const sftpDownloadChunk = 32000

// sftpSession is a live Net::SFTP session: the injected channel plus the codec
// state (the negotiated protocol builder and the packet reassembler) the driver
// runs the request/response loop over.
type sftpSession struct {
	cls     *RClass
	vm      *VM
	channel object.Value // the Ruby SSH-channel duck (#write / #read)
	proto   *sftp.Protocol
	parser  *sftp.PacketParser
	version int
}

func (s *sftpSession) ToS() string     { return "#<Net::SFTP::Session>" }
func (s *sftpSession) Inspect() string { return "#<Net::SFTP::Session>" }
func (s *sftpSession) Truthy() bool    { return true }

// sftpDir is the Session#dir helper (Net::SFTP::Operations::Dir): directory
// enumeration (foreach / entries / glob) over its owning session.
type sftpDir struct {
	cls *RClass
	s   *sftpSession
}

func (d *sftpDir) ToS() string     { return "#<Net::SFTP::Operations::Dir>" }
func (d *sftpDir) Inspect() string { return "#<Net::SFTP::Operations::Dir>" }
func (d *sftpDir) Truthy() bool    { return true }

// sftpFileFactory is the Session#file helper (Net::SFTP::Operations::FileFactory):
// it opens remote files (#open) and answers metadata queries (#directory?).
type sftpFileFactory struct {
	cls *RClass
	s   *sftpSession
}

func (f *sftpFileFactory) ToS() string     { return "#<Net::SFTP::Operations::FileFactory>" }
func (f *sftpFileFactory) Inspect() string { return "#<Net::SFTP::Operations::FileFactory>" }
func (f *sftpFileFactory) Truthy() bool    { return true }

// sftpFile is an open remote file (Net::SFTP::Operations::File): a handle plus a
// read/write cursor the #read / #write / #close methods advance.
type sftpFile struct {
	cls    *RClass
	s      *sftpSession
	handle []byte
	pos    uint64
	closed bool
}

func (f *sftpFile) ToS() string     { return "#<Net::SFTP::Operations::File>" }
func (f *sftpFile) Inspect() string { return "#<Net::SFTP::Operations::File>" }
func (f *sftpFile) Truthy() bool    { return true }

// --- the channel-seam driver -----------------------------------------------

// writeFrame writes framed request bytes to the channel via the duck's #write.
func (s *sftpSession) writeFrame(frame []byte) {
	s.vm.send(s.channel, "write", []object.Value{object.NewStringBytesEnc(append([]byte(nil), frame...), "ASCII-8BIT")}, nil)
}

// nextPacket reassembles and returns the next complete packet, pulling more bytes
// from the channel's #read whenever the parser needs them. An empty read before a
// packet completes means the channel closed mid-stream.
func (s *sftpSession) nextPacket() sftp.Packet {
	for {
		if pkt, ok := s.parser.Next(); ok {
			return pkt
		}
		rv := s.vm.send(s.channel, "read", []object.Value{object.IntValue(sftpReadChunk)}, nil)
		data := sftpReadBytes(rv)
		if len(data) == 0 {
			raise("Net::SFTP::Exception", "the SFTP channel closed before a complete packet arrived")
		}
		s.parser.Feed(data)
	}
}

// connect performs the FXP_INIT / FXP_VERSION handshake: it advertises the
// client's highest version, parses the server's reply, and builds the negotiated
// request builder. It raises Net::SFTP::Exception on any protocol fault.
func (s *sftpSession) connect() {
	s.writeFrame(sftp.InitPacket(sftp.HighestProtocolVersionSupported))
	pkt := s.nextPacket()
	if pkt.Type != sftp.FXP_VERSION {
		raise("Net::SFTP::Exception", "expected FXP_VERSION, got packet type %d", pkt.Type)
	}
	info, err := sftp.ParseVersion(pkt.Payload)
	if err != nil {
		raise("Net::SFTP::Exception", "%s", err.Error())
	}
	v, err := sftp.NegotiateVersion(info.Version, sftp.HighestProtocolVersionSupported)
	if err != nil {
		raise("Net::SFTP::Exception", "%s", err.Error())
	}
	s.version = v
	s.proto = sftp.NewProtocol(v)
}

// response writes a request frame and returns the parsed, id-correlated response.
func (s *sftpSession) response(id uint32, frame []byte) any {
	s.writeFrame(frame)
	pkt := s.nextPacket()
	resp, err := sftp.ParseResponse(pkt, s.version)
	if err != nil {
		raise("Net::SFTP::Exception", "%s", err.Error())
	}
	if got := sftpRespID(resp); got != id {
		raise("Net::SFTP::Exception", "response id mismatch: got %d, expected %d", got, id)
	}
	return resp
}

// okOrRaise interprets a status-only response: FX_OK returns nil, any other code
// raises the gem-faithful Net::SFTP::StatusException.
func (s *sftpSession) okOrRaise(resp any, op string) object.Value {
	st := resp.(*sftp.StatusResponse)
	if st.OK() {
		return object.NilV
	}
	return s.raiseStatus(st, op)
}

// handleOf interprets an open/opendir response: an FXP_HANDLE yields the handle
// string, an FXP_STATUS raises.
func (s *sftpSession) handleOf(resp any, op string) []byte {
	if st, ok := resp.(*sftp.StatusResponse); ok {
		s.raiseStatus(st, op)
	}
	return resp.(*sftp.HandleResponse).Handle
}

// openHandle opens path in the given mode and returns the raw handle bytes. It
// translates the Ruby mode string into the protocol-version-specific open flags.
func (s *sftpSession) openHandle(path, mode string, attrs *sftp.Attributes) []byte {
	flags, err := sftp.NormalizeOpenFlags(mode)
	if err != nil {
		raise("ArgumentError", "%s", err.Error())
	}
	var id uint32
	var frame []byte
	if s.version >= 5 {
		sf, da := sftp.OpenFlagsV5(flags)
		id, frame = s.proto.Open(path, sf, da, attrs)
	} else {
		id, frame = s.proto.Open(path, sftp.OpenFlagsV1(flags), 0, attrs)
	}
	return s.handleOf(s.response(id, frame), "open")
}

// closeHandle closes an open file or directory handle.
func (s *sftpSession) closeHandle(h []byte) object.Value {
	id, frame := s.proto.Close(h)
	return s.okOrRaise(s.response(id, frame), "close")
}

// readAt issues one FXP_READ and returns the data bytes, or ok=false at EOF.
func (s *sftpSession) readAt(h []byte, offset uint64, length uint32) (data []byte, ok bool) {
	id, frame := s.proto.Read(h, offset, length)
	resp := s.response(id, frame)
	if st, isStatus := resp.(*sftp.StatusResponse); isStatus {
		if st.EOF() {
			return nil, false
		}
		s.raiseStatus(st, "read")
	}
	return resp.(*sftp.DataResponse).Data, true
}

// readdirAll enumerates a directory handle, returning every Name entry.
func (s *sftpSession) readdirAll(h []byte) []sftp.Name {
	var out []sftp.Name
	for {
		id, frame := s.proto.Readdir(h)
		resp := s.response(id, frame)
		if st, isStatus := resp.(*sftp.StatusResponse); isStatus {
			if st.EOF() {
				return out
			}
			s.raiseStatus(st, "readdir")
		}
		nr := resp.(*sftp.NameResponse)
		out = append(out, nr.Names...)
	}
}

// --- registration ----------------------------------------------------------

// registerNetSFTP installs the Net::SFTP surface (require "net/sftp"): the
// Session client driven over an injected SSH channel, the dir/file operation
// helpers, the version-aware Attributes and Name value objects, the
// Net::SFTP::Constants tables, and the Exception / StatusException tree. It nests
// under the existing Net module, so it must run after registerNetHTTP (which
// creates Net) and after registerSocket / registerOpenSSL for consistency with
// the other injected-transport clients.
func (vm *VM) registerNetSFTP() {
	net := vm.consts["Net"].(*RClass)
	std := vm.consts["StandardError"].(*RClass)

	mod := newClass("Net::SFTP", vm.cObject)
	mod.isModule = true
	net.consts["SFTP"] = mod
	vm.consts["Net::SFTP"] = mod

	// The exception tree: Net::SFTP::Exception < StandardError, and
	// Net::SFTP::StatusException < Exception (carrying code / description / text).
	exc := newClass("Net::SFTP::Exception", std)
	mod.consts["Exception"] = exc
	vm.consts["Net::SFTP::Exception"] = exc
	statusExc := newClass("Net::SFTP::StatusException", exc)
	mod.consts["StatusException"] = statusExc
	vm.consts["Net::SFTP::StatusException"] = statusExc
	vm.defineStatusExceptionMethods(statusExc)

	vm.registerNetSFTPConstants(mod)

	// The value classes returned by stat!/readdir!/dir.entries.
	attrsCls := vm.registerNetSFTPAttributes(mod)
	nameCls := newClass("Net::SFTP::Name", vm.cObject)
	mod.consts["Name"] = nameCls
	vm.consts["Net::SFTP::Name"] = nameCls
	vm.defineNetSFTPNameMethods(nameCls)

	session := newClass("Net::SFTP::Session", vm.cObject)
	mod.consts["Session"] = session
	vm.consts["Net::SFTP::Session"] = session
	session.consts["HIGHEST_PROTOCOL_VERSION_SUPPORTED"] = object.IntValue(int64(sftp.HighestProtocolVersionSupported))

	dirCls := newClass("Net::SFTP::Operations::Dir", vm.cObject)
	vm.consts["Net::SFTP::Operations::Dir"] = dirCls
	fileFactoryCls := newClass("Net::SFTP::Operations::FileFactory", vm.cObject)
	vm.consts["Net::SFTP::Operations::FileFactory"] = fileFactoryCls
	fileCls := newClass("Net::SFTP::Operations::File", vm.cObject)
	vm.consts["Net::SFTP::Operations::File"] = fileCls

	vm.sftpClasses = &sftpClassSet{
		attrs:       attrsCls,
		name:        nameCls,
		statusExc:   statusExc,
		dir:         dirCls,
		fileFactory: fileFactoryCls,
		file:        fileCls,
	}

	// Net::SFTP.start(host, user, options={}) { |sftp| ... } and
	// Net::SFTP::Session.new(channel) both build a connected session over the
	// injected channel.
	starter := func(_ *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		return vm.newSFTPSession(session, args, blk)
	}
	mod.smethods["start"] = &Method{name: "start", owner: mod, native: starter}
	session.smethods["new"] = &Method{name: "new", owner: session, native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.newSFTPSession(session, args, nil)
	}}

	vm.defineNetSFTPSessionMethods(session)
	vm.defineNetSFTPDirMethods(dirCls)
	vm.defineNetSFTPFileFactoryMethods(fileFactoryCls)
	vm.defineNetSFTPFileMethods(fileCls)
}

// registerNetSFTPConstants installs Net::SFTP::Constants: the StatusCodes and
// PacketTypes tables plus HighestProtocolVersionSupported, mirroring the gem's
// Net::SFTP::Constants module so a script can reference FX_* / FXP_* by name.
func (vm *VM) registerNetSFTPConstants(mod *RClass) {
	c := newClass("Net::SFTP::Constants", vm.cObject)
	c.isModule = true
	mod.consts["Constants"] = c
	vm.consts["Net::SFTP::Constants"] = c

	status := newClass("Net::SFTP::Constants::StatusCodes", vm.cObject)
	status.isModule = true
	c.consts["StatusCodes"] = status
	for name, val := range map[string]int{
		"FX_OK": sftp.FX_OK, "FX_EOF": sftp.FX_EOF, "FX_NO_SUCH_FILE": sftp.FX_NO_SUCH_FILE,
		"FX_PERMISSION_DENIED": sftp.FX_PERMISSION_DENIED, "FX_FAILURE": sftp.FX_FAILURE,
		"FX_BAD_MESSAGE": sftp.FX_BAD_MESSAGE, "FX_NO_CONNECTION": sftp.FX_NO_CONNECTION,
		"FX_CONNECTION_LOST": sftp.FX_CONNECTION_LOST, "FX_OP_UNSUPPORTED": sftp.FX_OP_UNSUPPORTED,
	} {
		status.consts[name] = object.IntValue(int64(val))
	}

	types := newClass("Net::SFTP::Constants::PacketTypes", vm.cObject)
	types.isModule = true
	c.consts["PacketTypes"] = types
	for name, val := range map[string]int{
		"FXP_INIT": sftp.FXP_INIT, "FXP_VERSION": sftp.FXP_VERSION, "FXP_OPEN": sftp.FXP_OPEN,
		"FXP_CLOSE": sftp.FXP_CLOSE, "FXP_READ": sftp.FXP_READ, "FXP_WRITE": sftp.FXP_WRITE,
		"FXP_STATUS": sftp.FXP_STATUS, "FXP_HANDLE": sftp.FXP_HANDLE, "FXP_DATA": sftp.FXP_DATA,
		"FXP_NAME": sftp.FXP_NAME, "FXP_ATTRS": sftp.FXP_ATTRS,
	} {
		types.consts[name] = object.IntValue(int64(val))
	}
}

// sftpClassSet caches the wrapper classes so the value-mapping helpers can stamp
// the right Ruby class on a returned Attributes / Name / File without threading
// the VM's const table through every call.
type sftpClassSet struct {
	attrs       *RClass
	name        *RClass
	statusExc   *RClass
	dir         *RClass
	fileFactory *RClass
	file        *RClass
}

// newSFTPSession builds a session over the injected channel, runs the version
// handshake, and (for start with a block) yields the session to the block. The
// channel is the first positional non-Hash argument or the channel: / connection:
// keyword; host / user positionals are accepted and ignored (there is no SSH).
func (vm *VM) newSFTPSession(cls *RClass, args []object.Value, blk *Proc) object.Value {
	ch := sftpChannelArg(args)
	if ch == nil {
		raise("ArgumentError", "Net::SFTP requires a channel: IO-like object responding to #read/#write (rbgo has no native SSH transport yet)")
	}
	s := &sftpSession{cls: cls, vm: vm, channel: ch, parser: sftp.NewPacketParser()}
	s.connect()
	if blk != nil {
		vm.callBlock(blk, []object.Value{s})
	}
	return s
}
