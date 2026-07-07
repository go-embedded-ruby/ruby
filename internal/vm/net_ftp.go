// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"bufio"
	"bytes"
	"net"
	"os"
	"strconv"
	"strings"
	stdtime "time"

	gotime "github.com/go-composites/time/src"
	netftp "github.com/go-ruby-net-ftp/net-ftp"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerNetFTP installs Net::FTP (require "net/ftp"): a real FTP client that
// drives control and data sockets (Go net) on top of the interpreter-independent
// FTP codec github.com/go-ruby-net-ftp/net-ftp. The codec owns every observable
// byte (command construction, reply classification, PASV/EPSV extraction, MLSx
// parsing); this binding owns the sockets and maps the Ruby surface — connect /
// login, the directory and file commands, the LIST/NLST/MLSD/MLST listings, the
// retr*/stor* transfer engines, and the get*/put* file helpers — onto it.
//
// It runs after registerNetHTTP (which creates the Net module) and after
// registerSocket / registerOpenSSL (the transport it dials through), so the Net
// module and the exception root already exist.
func (vm *VM) registerNetFTP() {
	netMod := vm.consts["Net"].(*RClass)
	std := vm.consts["StandardError"].(*RClass)

	vm.registerFTPErrors(netMod, std)

	ftp := newClass("Net::FTP", vm.cObject)
	netMod.consts["FTP"] = ftp
	vm.consts["Net::FTP"] = ftp

	entry := newClass("Net::FTP::MLSxEntry", vm.cObject)
	ftp.consts["MLSxEntry"] = entry
	vm.consts["Net::FTP::MLSxEntry"] = entry
	vm.registerFTPMLSxEntry(entry)

	vm.registerFTPClassMethods(ftp)
	vm.registerFTPMethods(ftp)
}

// registerFTPErrors installs the Net::FTP exception family (FTPError <
// StandardError; FTPReplyError / FTPTempError / FTPPermError / FTPProtoError /
// FTPConnectionError < FTPError), publishing each qualified name so raise() (and
// the codec's ClassName) resolves the real class a `rescue Net::FTPPermError`
// catches.
func (vm *VM) registerFTPErrors(netMod, std *RClass) {
	reg := func(simple string, super *RClass) *RClass {
		qualified := "Net::" + simple
		c := newClass(qualified, super)
		netMod.consts[simple] = c
		vm.consts[qualified] = c
		return c
	}
	base := reg("FTPError", std)
	reg("FTPReplyError", base)
	reg("FTPTempError", base)
	reg("FTPPermError", base)
	reg("FTPProtoError", base)
	reg("FTPConnectionError", base)
}

// --- class methods ----------------------------------------------------------

// registerFTPClassMethods installs Net::FTP.new and Net::FTP.open. new(host=nil,
// options) connects (and logs in when a username is supplied); open is new plus
// the ensured-close block form.
func (vm *VM) registerFTPClassMethods(ftp *RClass) {
	newFn := func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		f := &ftpObj{cls: ftp, binary: true, passive: true}
		vm.ftpInit(f, args)
		return f
	}
	ftp.smethods["new"] = &Method{name: "new", owner: ftp, native: newFn}

	ftp.smethods["open"] = &Method{name: "open", owner: ftp, native: func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		f := &ftpObj{cls: ftp, binary: true, passive: true}
		vm.ftpInit(f, args)
		if blk == nil {
			return f
		}
		// Net::FTP.open(...) { |ftp| ... } yields the connected client and closes it
		// afterwards, even if the block raises (the deferred close runs on unwind).
		return vm.ftpWithClose(f, blk)
	}}
}

// ftpWithClose yields f to blk and closes the control connection afterwards,
// including on an exception unwind (the deferred close runs before the panic
// propagates).
func (vm *VM) ftpWithClose(f *ftpObj, blk *Proc) (res object.Value) {
	defer f.doClose()
	return vm.callBlock(blk, []object.Value{f})
}

// ftpInit applies the constructor arguments: an optional host (connect), an
// options Hash (port / passive / username / password / account / use_pasv_ip) or
// the legacy positional user / passwd / acct, and a login when a username is
// present.
func (vm *VM) ftpInit(f *ftpObj, args []object.Value) {
	if len(args) == 0 || object.IsNil(args[0]) {
		return
	}
	host := strArg(args[0])
	port := 21
	user, passwd, acct := "", "", ""
	userGiven, passwdGiven := false, false

	if len(args) >= 2 {
		if h, ok := args[1].(*object.Hash); ok {
			port, user, userGiven, passwd, passwdGiven, acct = vm.ftpApplyOpts(f, h, port)
		}
	}

	vm.ftpConnect(f, host, port)
	if userGiven {
		vm.ftpLogin(f, user, passwd, passwdGiven, acct)
	}
}

// ftpApplyOpts reads the options Hash into f (passive / use_pasv_ip) and returns
// the connection port plus the login credentials it carries.
func (vm *VM) ftpApplyOpts(f *ftpObj, h *object.Hash, port int) (int, string, bool, string, bool, string) {
	user, passwd, acct := "", "", ""
	userGiven, passwdGiven := false, false
	if v, ok := ftpOptHash(h, "port"); ok {
		if n, ok := v.(object.Integer); ok {
			port = int(n)
		}
	}
	if v, ok := ftpOptHash(h, "passive"); ok {
		f.passive = v.Truthy()
	}
	if v, ok := ftpOptHash(h, "use_pasv_ip"); ok {
		f.usePasvIP = v.Truthy()
	}
	if v, ok := ftpOptHash(h, "username", "user"); ok {
		user, userGiven = strArg(v), true
	}
	if v, ok := ftpOptHash(h, "password", "passwd"); ok {
		passwd, passwdGiven = strArg(v), true
	}
	if v, ok := ftpOptHash(h, "account", "acct"); ok {
		acct = strArg(v)
	}
	return port, user, userGiven, passwd, passwdGiven, acct
}

// ftpOptHash looks a key up in an options Hash by any of its symbol or string
// spellings.
func ftpOptHash(h *object.Hash, names ...string) (object.Value, bool) {
	for _, n := range names {
		if v, ok := h.Get(object.Symbol(n)); ok {
			return v, true
		}
		if v, ok := h.Get(object.NewString(n)); ok {
			return v, true
		}
	}
	return nil, false
}

// ftpConnect dials the control connection, reads the server greeting (recorded
// as welcome), and selects EPSV/EPRT when the connection is IPv6
// (Net::FTP#connect).
func (vm *VM) ftpConnect(f *ftpObj, host string, port int) {
	conn, err := net.Dial("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		ftpConnErr(err)
	}
	f.conn = conn
	f.r = bufio.NewReader(conn)
	f.closed = false
	f.epsv = ftpIsIPv6(conn)
	resp := f.getResp()
	f.welcome = resp
}

// ftpLogin runs the USER → PASS → ACCT ladder (Net::FTP#login), substituting the
// anonymous password when logging in as "anonymous" with none supplied and
// raising FTPReplyError when a required credential is missing.
func (vm *VM) ftpLogin(f *ftpObj, user, passwd string, passwdGiven bool, acct string) {
	if !passwdGiven {
		if pw, ok := netftp.AnonymousLogin(user, false); ok {
			passwd, passwdGiven = pw, true
		}
	}
	resp := f.sendCmd(netftp.UserCommand(user))
	step, err := netftp.LoginNext(resp, false)
	if err != nil {
		ftpRaise(err)
	}
	if step == netftp.LoginSendPass {
		if !passwdGiven {
			raise("Net::FTPReplyError", "%s", resp)
		}
		resp = f.sendCmd(netftp.PassCommand(passwd))
		step, err = netftp.LoginNext(resp, true)
		if err != nil {
			ftpRaise(err)
		}
	}
	if step == netftp.LoginSendAcct {
		if acct == "" {
			raise("Net::FTPReplyError", "%s", resp)
		}
		resp = f.sendCmd(netftp.AcctCommand(acct))
		if !strings.HasPrefix(resp, "2") {
			raise("Net::FTPReplyError", "%s", resp)
		}
	}
}

// --- instance methods -------------------------------------------------------

// registerFTPMethods installs the Net::FTP instance surface: the connect/login
// entry points, the mode accessors, the directory and file commands, the listing
// commands, the transfer engines, and the get/put file helpers.
func (vm *VM) registerFTPMethods(ftp *RClass) {
	self := func(v object.Value) *ftpObj { return v.(*ftpObj) }
	d := func(name string, fn NativeFn) { ftp.define(name, fn) }

	d("connect", func(vm *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		ftpArity(a, 1, 2, "connect")
		port := 21
		if len(a) >= 2 {
			port = int(a[1].(object.Integer))
		}
		vm.ftpConnect(self(v), strArg(a[0]), port)
		return object.NilV
	})
	d("login", func(vm *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		user := ftpOptString(a, 0, "anonymous")
		passwd, passwdGiven := "", false
		if len(a) >= 2 && !object.IsNil(a[1]) {
			passwd, passwdGiven = strArg(a[1]), true
		}
		vm.ftpLogin(self(v), user, passwd, passwdGiven, ftpOptString(a, 2, ""))
		return object.NilV
	})

	// --- mode accessors ---
	d("passive", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).passive)
	})
	d("passive=", func(_ *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		self(v).passive = a[0].Truthy()
		return a[0]
	})
	d("binary", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).binary)
	})
	d("binary=", func(_ *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		self(v).binary = a[0].Truthy()
		return a[0]
	})
	d("use_pasv_ip", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).usePasvIP)
	})
	d("use_pasv_ip=", func(_ *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		self(v).usePasvIP = a[0].Truthy()
		return a[0]
	})
	d("welcome", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).welcome)
	})
	d("last_response", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).lastResponse)
	})
	d("last_response_code", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(netftp.ReplyCode(self(v).lastResponse))
	})
	d("lastresp", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(netftp.ReplyCode(self(v).lastResponse))
	})

	// --- generic command senders ---
	d("sendcmd", func(_ *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		ftpArity(a, 1, 1, "sendcmd")
		return object.NewString(self(v).sendCmd(strArg(a[0])))
	})
	d("voidcmd", func(_ *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		ftpArity(a, 1, 1, "voidcmd")
		self(v).voidCmd(strArg(a[0]))
		return object.NilV
	})
	d("noop", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		self(v).voidCmd(netftp.NoopCommand)
		return object.NilV
	})

	// --- directory commands ---
	d("pwd", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(vm.ftpPwd(self(v)))
	})
	d("getdir", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(vm.ftpPwd(self(v)))
	})
	d("chdir", func(_ *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		ftpArity(a, 1, 1, "chdir")
		vm.ftpChdir(self(v), strArg(a[0]))
		return object.NilV
	})
	d("mkdir", func(_ *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		ftpArity(a, 1, 1, "mkdir")
		f := self(v)
		name, err := netftp.Parse257(f.sendCmd(netftp.MkdCommand(strArg(a[0]))))
		if err != nil {
			ftpRaise(err)
		}
		return object.NewString(name)
	})
	d("rmdir", func(_ *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		ftpArity(a, 1, 1, "rmdir")
		self(v).voidCmd(netftp.RmdCommand(strArg(a[0])))
		return object.NilV
	})
	d("delete", func(_ *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		ftpArity(a, 1, 1, "delete")
		f := self(v)
		if err := netftp.CheckDelete(f.sendCmd(netftp.DeleCommand(strArg(a[0])))); err != nil {
			ftpRaise(err)
		}
		return object.NilV
	})
	d("rename", func(_ *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		ftpArity(a, 2, 2, "rename")
		f := self(v)
		if err := netftp.CheckRenameFrom(f.sendCmd(netftp.RnfrCommand(strArg(a[0])))); err != nil {
			ftpRaise(err)
		}
		f.voidCmd(netftp.RntoCommand(strArg(a[1])))
		return object.NilV
	})

	// --- metadata commands ---
	d("size", func(_ *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		ftpArity(a, 1, 1, "size")
		f := self(v)
		f.setType(true)
		n, err := netftp.SizeResult(f.sendCmd(netftp.SizeCommand(strArg(a[0]))))
		if err != nil {
			ftpRaise(err)
		}
		return object.IntValue(int64(n))
	})
	d("system", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		f := self(v)
		s, err := netftp.SystResult(f.sendCmd(netftp.SystCommand))
		if err != nil {
			ftpRaise(err)
		}
		return object.NewString(s)
	})
	d("status", func(_ *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).sendCmd(netftp.StatCommand(ftpOptString(a, 0, ""))))
	})
	d("mdtm", func(_ *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		ftpArity(a, 1, 1, "mdtm")
		f := self(v)
		raw, ok := netftp.MdtmResult(f.sendCmd(netftp.MdtmCommand(strArg(a[0]))))
		if !ok {
			raise("Net::FTPReplyError", "%s", f.lastResponse)
		}
		return object.NewString(raw)
	})
	d("mtime", func(_ *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		ftpArity(a, 1, 1, "mtime")
		return vm.ftpMtime(self(v), strArg(a[0]))
	})
	d("help", func(_ *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).sendCmd(netftp.HelpCommand(ftpOptString(a, 0, ""))))
	})
	d("site", func(_ *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		ftpArity(a, 1, 1, "site")
		self(v).voidCmd(netftp.SiteCommand(strArg(a[0])))
		return object.NilV
	})

	// --- listing commands ---
	d("nlst", func(vm *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		f := self(v)
		var out []object.Value
		f.retrlines(netftp.NlstCommand(ftpOptString(a, 0, "")), func(line string) {
			out = append(out, object.NewString(line))
		})
		return object.NewArrayFromSlice(out)
	})
	list := func(vm *VM, v object.Value, a []object.Value, blk *Proc) object.Value {
		f := self(v)
		args := make([]string, len(a))
		for i, x := range a {
			args[i] = strArg(x)
		}
		var out []object.Value
		f.retrlines(netftp.ListCommand(args...), func(line string) {
			if blk != nil {
				vm.callBlock(blk, []object.Value{object.NewString(line)})
			}
			out = append(out, object.NewString(line))
		})
		return object.NewArrayFromSlice(out)
	}
	d("list", list)
	d("ls", list)
	d("dir", list)
	d("mlsd", func(vm *VM, v object.Value, a []object.Value, blk *Proc) object.Value {
		f := self(v)
		var out []object.Value
		var perr error
		f.retrlines(netftp.MlsdCommand(ftpOptString(a, 0, "")), func(line string) {
			if perr != nil || line == "" {
				return
			}
			e, err := netftp.ParseMLSxEntry(line)
			if err != nil {
				perr = err
				return
			}
			entry := vm.ftpEntry(e)
			if blk != nil {
				vm.callBlock(blk, []object.Value{entry})
			}
			out = append(out, entry)
		})
		if perr != nil {
			ftpRaise(perr)
		}
		return object.NewArrayFromSlice(out)
	})
	d("mlst", func(vm *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		return vm.ftpMlst(self(v), ftpOptString(a, 0, ""))
	})

	// --- transfer engines ---
	d("retrlines", func(vm *VM, v object.Value, a []object.Value, blk *Proc) object.Value {
		ftpArity(a, 1, 1, "retrlines")
		self(v).retrlines(strArg(a[0]), func(line string) {
			if blk != nil {
				vm.callBlock(blk, []object.Value{object.NewString(line)})
			}
		})
		return object.NilV
	})
	d("retrbinary", func(vm *VM, v object.Value, a []object.Value, blk *Proc) object.Value {
		ftpArity(a, 1, 2, "retrbinary")
		self(v).retrbinary(strArg(a[0]), ftpBlocksizeArg(a, 1), func(chunk []byte) {
			if blk != nil {
				vm.callBlock(blk, []object.Value{object.NewStringBytesEnc(chunk, "ASCII-8BIT")})
			}
		})
		return object.NilV
	})
	d("storlines", func(_ *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		ftpArity(a, 2, 2, "storlines")
		self(v).storlines(strArg(a[0]), string(ftpBytes(a[1])))
		return object.NilV
	})
	d("storbinary", func(_ *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		ftpArity(a, 2, 3, "storbinary")
		self(v).storbinary(strArg(a[0]), ftpBytes(a[1]), ftpBlocksizeArg(a, 2))
		return object.NilV
	})

	// --- file helpers ---
	d("getbinaryfile", func(vm *VM, v object.Value, a []object.Value, blk *Proc) object.Value {
		ftpArity(a, 1, 3, "getbinaryfile")
		return vm.ftpGetBinary(self(v), a, blk)
	})
	d("gettextfile", func(vm *VM, v object.Value, a []object.Value, blk *Proc) object.Value {
		ftpArity(a, 1, 2, "gettextfile")
		return vm.ftpGetText(self(v), a, blk)
	})
	d("get", func(vm *VM, v object.Value, a []object.Value, blk *Proc) object.Value {
		ftpArity(a, 1, 3, "get")
		f := self(v)
		if f.binary {
			return vm.ftpGetBinary(f, a, blk)
		}
		return vm.ftpGetText(f, a, blk)
	})
	d("putbinaryfile", func(vm *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		ftpArity(a, 1, 3, "putbinaryfile")
		return vm.ftpPutBinary(self(v), a)
	})
	d("puttextfile", func(vm *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		ftpArity(a, 1, 2, "puttextfile")
		return vm.ftpPutText(self(v), a)
	})
	d("put", func(vm *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		ftpArity(a, 1, 3, "put")
		f := self(v)
		if f.binary {
			return vm.ftpPutBinary(f, a)
		}
		return vm.ftpPutText(f, a)
	})

	// --- teardown ---
	d("abort", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(vm.ftpAbort(self(v)))
	})
	d("quit", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		self(v).voidCmd(netftp.QuitCommand)
		return object.NilV
	})
	d("close", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		self(v).doClose()
		return object.NilV
	})
	d("closed?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		f := self(v)
		return object.Bool(f.closed || f.conn == nil)
	})
}

// ftpPwd sends PWD and decodes the quoted pathname (Net::FTP#pwd).
func (vm *VM) ftpPwd(f *ftpObj) string {
	name, err := netftp.Parse257(f.sendCmd(netftp.PwdCommand))
	if err != nil {
		ftpRaise(err)
	}
	return name
}

// ftpChdir changes directory (Net::FTP#chdir): ".." tries CDUP first and falls
// back to CWD only on a 500 reply; every other directory goes straight to CWD.
func (vm *VM) ftpChdir(f *ftpObj, dir string) {
	if dir == ".." {
		// CDUP's 500 ("command not understood") is the one reply that falls through
		// to CWD "..", so its reply is read raw (bypassing getResp's raise) to
		// inspect the code before deciding.
		f.writeLine(netftp.CdupCommand)
		resp, err := netftp.GetMultiline(f.reader())
		if err != nil {
			ftpConnErr(err)
		}
		f.lastResponse = resp
		if strings.HasPrefix(resp, "2") {
			return
		}
		if netftp.ReplyCode(resp) != "500" {
			if _, cerr := netftp.ClassifyReply(resp); cerr != nil {
				ftpRaise(cerr)
			}
			raise("Net::FTPReplyError", "%s", resp)
		}
	}
	f.voidCmd(netftp.CwdCommand(dir))
}

// ftpMtime sends MDTM and parses the "YYYYMMDDhhmmss" reply into a UTC Time
// (Net::FTP#mtime).
func (vm *VM) ftpMtime(f *ftpObj, file string) object.Value {
	raw, ok := netftp.MdtmResult(f.sendCmd(netftp.MdtmCommand(file)))
	if !ok {
		raise("Net::FTPReplyError", "%s", f.lastResponse)
	}
	t, err := stdtime.Parse("20060102150405", raw)
	if err != nil {
		raise("Net::FTPProtoError", "invalid time-val: %s", raw)
	}
	return &Time{t: gotime.FromUnix(t.Unix()).UTC()}
}

// ftpMlst sends MLST, requires the 250 multiline reply, and parses its middle
// line into a Net::FTP::MLSxEntry (Net::FTP#mlst).
func (vm *VM) ftpMlst(f *ftpObj, pathname string) object.Value {
	resp := f.sendCmd(netftp.MlstCommand(pathname))
	if !strings.HasPrefix(resp, "250") {
		raise("Net::FTPReplyError", "%s", resp)
	}
	// The assembled reply always ends in a newline, so a Split yields at least two
	// elements; a single-line 250 leaves an empty entry line, which ParseMLSxEntry
	// rejects as FTPProtoError below (no pathname).
	lines := strings.Split(resp, "\n")
	e, err := netftp.ParseMLSxEntry(strings.TrimLeft(lines[1], " "))
	if err != nil {
		ftpRaise(err)
	}
	return vm.ftpEntry(e)
}

// ftpAbort sends ABOR out of band and validates the reply against the codes MRI
// accepts (426/226/225), raising FTPProtoError otherwise (Net::FTP#abort).
func (vm *VM) ftpAbort(f *ftpObj) string {
	f.writeLine(netftp.AborCommand)
	resp, err := netftp.GetMultiline(f.reader())
	if err != nil {
		ftpConnErr(err)
	}
	f.lastResponse = resp
	if !netftp.AbortAccepted(resp) {
		raise("Net::FTPProtoError", "%s", resp)
	}
	return resp
}

// --- get / put ---------------------------------------------------------------

// ftpGetBinary implements getbinaryfile / the binary branch of get: it yields
// each block to a block, or (with no block) retrieves the file into localfile
// (defaulting to the remote basename).
func (vm *VM) ftpGetBinary(f *ftpObj, a []object.Value, blk *Proc) object.Value {
	remote := strArg(a[0])
	bs := ftpBlocksizeArg(a, 2)
	if blk != nil {
		f.retrbinary(netftp.RetrCommand(remote), bs, func(chunk []byte) {
			vm.callBlock(blk, []object.Value{object.NewStringBytesEnc(chunk, "ASCII-8BIT")})
		})
		return object.NilV
	}
	local := ftpOptString(a, 1, ftpBasename(remote))
	var buf bytes.Buffer
	f.retrbinary(netftp.RetrCommand(remote), bs, func(chunk []byte) { buf.Write(chunk) })
	if err := os.WriteFile(local, buf.Bytes(), 0o644); err != nil {
		raise("Errno::EACCES", "%s", err.Error())
	}
	return object.NilV
}

// ftpGetText implements gettextfile / the text branch of get: it yields each
// line to a block, or (with no block) retrieves the file into localfile, joining
// the received lines with newlines.
func (vm *VM) ftpGetText(f *ftpObj, a []object.Value, blk *Proc) object.Value {
	remote := strArg(a[0])
	if blk != nil {
		f.retrlines(netftp.RetrCommand(remote), func(line string) {
			vm.callBlock(blk, []object.Value{object.NewString(line)})
		})
		return object.NilV
	}
	local := ftpOptString(a, 1, ftpBasename(remote))
	var b strings.Builder
	f.retrlines(netftp.RetrCommand(remote), func(line string) {
		b.WriteString(line)
		b.WriteByte('\n')
	})
	if err := os.WriteFile(local, []byte(b.String()), 0o644); err != nil {
		raise("Errno::EACCES", "%s", err.Error())
	}
	return object.NilV
}

// ftpPutBinary implements putbinaryfile / the binary branch of put: it uploads
// localfile's bytes with STOR (remotefile defaults to the local basename).
func (vm *VM) ftpPutBinary(f *ftpObj, a []object.Value) object.Value {
	local := strArg(a[0])
	data, err := os.ReadFile(local)
	if err != nil {
		raise("Errno::ENOENT", "%s", err.Error())
	}
	remote := ftpOptString(a, 1, ftpBasename(local))
	f.storbinary(netftp.StorCommand(remote), data, ftpBlocksizeArg(a, 2))
	return object.NilV
}

// ftpPutText implements puttextfile / the text branch of put: it uploads
// localfile as ASCII lines with STOR (remotefile defaults to the local
// basename).
func (vm *VM) ftpPutText(f *ftpObj, a []object.Value) object.Value {
	local := strArg(a[0])
	data, err := os.ReadFile(local)
	if err != nil {
		raise("Errno::ENOENT", "%s", err.Error())
	}
	remote := ftpOptString(a, 1, ftpBasename(local))
	f.storlines(netftp.StorCommand(remote), string(data))
	return object.NilV
}

// --- MLSxEntry ---------------------------------------------------------------

// ftpEntry wraps a parsed codec MLSxEntry in its Ruby class.
func (vm *VM) ftpEntry(e netftp.MLSxEntry) *ftpMLSxEntry {
	return &ftpMLSxEntry{cls: vm.consts["Net::FTP::MLSxEntry"].(*RClass), e: e}
}

// ftpEntrySelf recovers the wrapper from a receiver.
func ftpEntrySelf(v object.Value) *ftpMLSxEntry { return v.(*ftpMLSxEntry) }

// registerFTPMLSxEntry installs Net::FTP::MLSxEntry: the pathname, the facts
// Hash (integer / time / string values), the type predicates, and the perm-bit
// predicates.
func (vm *VM) registerFTPMLSxEntry(cls *RClass) {
	cls.define("pathname", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(ftpEntrySelf(v).e.Pathname)
	})
	cls.define("facts", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.ftpFacts(ftpEntrySelf(v).e)
	})
	cls.define("file?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(ftpEntrySelf(v).e.IsFile())
	})
	cls.define("directory?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(ftpEntrySelf(v).e.IsDirectory())
	})
	// The perm-bit predicates all delegate to the codec's MLSxEntry queries; one
	// closure keyed by the predicate backs them all.
	perms := map[string]func(netftp.MLSxEntry) bool{
		"appendable?":        netftp.MLSxEntry.Appendable,
		"creatable?":         netftp.MLSxEntry.Creatable,
		"deletable?":         netftp.MLSxEntry.Deletable,
		"enterable?":         netftp.MLSxEntry.Enterable,
		"renamable?":         netftp.MLSxEntry.Renamable,
		"listable?":          netftp.MLSxEntry.Listable,
		"directory_makable?": netftp.MLSxEntry.DirectoryMakable,
		"purgeable?":         netftp.MLSxEntry.Purgeable,
		"readable?":          netftp.MLSxEntry.Readable,
		"writable?":          netftp.MLSxEntry.Writable,
	}
	for name, fn := range perms {
		pred := fn
		cls.define(name, func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
			return object.Bool(pred(ftpEntrySelf(v).e))
		})
	}
}

// ftpFacts maps an entry's facts (in appearance order) to a Ruby Hash: integer
// facts become Integer, time facts a UTC Time, the rest String.
func (vm *VM) ftpFacts(e netftp.MLSxEntry) object.Value {
	h := object.NewHash()
	for _, name := range e.FactOrder {
		fv := e.Facts[name]
		var val object.Value
		switch fv.Kind {
		case netftp.FactInt:
			val = object.IntValue(int64(fv.Int))
		case netftp.FactTime:
			val = vm.ftpTimeVal(fv.Time)
		default:
			val = object.NewString(fv.Str)
		}
		h.Set(object.NewString(name), val)
	}
	return h
}

// ftpTimeVal builds a UTC Ruby Time from an MLSx time-val's components.
func (vm *VM) ftpTimeVal(t netftp.MLSxTime) object.Value {
	sec := stdtime.Date(t.Year, stdtime.Month(t.Month), t.Day, t.Hour, t.Min, t.Sec, 0, stdtime.UTC).Unix()
	return &Time{t: gotime.FromUnix(sec).UTC()}
}
