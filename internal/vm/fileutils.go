// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// FileUtils operations are routed through these seams so the error branches are
// reachable identically on every platform (a test swaps in a failing stub rather
// than depending on real OS behaviour, which differs across Linux/macOS/Windows).
var (
	fuMkdirAll  = os.MkdirAll
	fuRemoveAll = os.RemoveAll
	fuRemove    = os.Remove
	fuRename    = os.Rename
	fuReadFile  = os.ReadFile
	fuWriteFile = os.WriteFile
	fuStat      = os.Stat
	fuChtimes   = os.Chtimes
	fuNow       = time.Now
	// fuWalkDir visits root and every descendant, invoking visit for each. It is a
	// seam so a test can drive the error branch of the recursive operations.
	fuWalkDir = func(root string, visit func(string)) error {
		return filepath.WalkDir(root, func(p string, _ fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			visit(p)
			return nil
		})
	}
)

// registerFileUtils installs the FileUtils module (require "fileutils"). The
// cross-platform, thin-over-os operations are implemented for real (mkdir_p, mv,
// cp, rm_rf/rmtree, rm_f, touch); the genuinely platform-specific or
// stream-oriented ones (chown, copy_stream, remove_entry_secure, uptodate?)
// raise NotImplementedError if called. Local `puppet apply` reaches these only
// for actual file resources, not at load.
func (vm *VM) registerFileUtils() {
	mod := newClass("FileUtils", nil)
	mod.isModule = true
	vm.consts["FileUtils"] = mod

	sdef := func(name string, fn NativeFn) { mod.smethods[name] = &Method{name: name, owner: mod, native: fn} }

	// pathsOf flattens the leading path argument(s): FileUtils accepts a single
	// path or an array of paths for the bulk operations.
	pathsOf := func(v object.Value) []string {
		if arr, ok := v.(*object.Array); ok {
			out := make([]string, len(arr.Elems))
			for i, e := range arr.Elems {
				out[i] = strArg(e)
			}
			return out
		}
		return []string{strArg(v)}
	}

	sdef("mkdir_p", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		list := pathsOf(args[0])
		for _, p := range list {
			if err := fuMkdirAll(p, 0o755); err != nil {
				raise("Errno::EACCES", "Permission denied @ fileutils_mkdir_p - %s", p)
			}
		}
		return args[0]
	})
	// MRI aliases: mkpath / makedirs.
	mod.smethods["mkpath"] = mod.smethods["mkdir_p"]
	mod.smethods["makedirs"] = mod.smethods["mkdir_p"]

	rmrf := func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		for _, p := range pathsOf(args[0]) {
			if err := fuRemoveAll(p); err != nil {
				raise("Errno::EACCES", "Permission denied @ fileutils_rm_rf - %s", p)
			}
		}
		return object.NilV
	}
	sdef("rm_rf", rmrf)
	sdef("rmtree", rmrf)
	sdef("rm_r", rmrf)

	sdef("rm_f", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		// rm_f ignores missing files (the "force" form); a real failure on an
		// existing file still surfaces.
		for _, p := range pathsOf(args[0]) {
			if err := fuRemove(p); err != nil && !os.IsNotExist(err) {
				raise("Errno::EACCES", "Permission denied @ fileutils_rm_f - %s", p)
			}
		}
		return object.NilV
	})
	mod.smethods["safe_unlink"] = mod.smethods["rm_f"]

	sdef("rm", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		for _, p := range pathsOf(args[0]) {
			if err := fuRemove(p); err != nil {
				raise("Errno::ENOENT", "No such file or directory @ fileutils_rm - %s", p)
			}
		}
		return object.NilV
	})

	sdef("mv", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		src, dst := strArg(args[0]), strArg(args[1])
		if err := fuRename(src, dst); err != nil {
			raise("Errno::ENOENT", "No such file or directory @ fileutils_mv - %s", src)
		}
		return object.NilV
	})
	mod.smethods["move"] = mod.smethods["mv"]

	sdef("cp", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		src, dst := strArg(args[0]), strArg(args[1])
		data, err := fuReadFile(src)
		if err != nil {
			raise("Errno::ENOENT", "No such file or directory @ fileutils_cp - %s", src)
		}
		if err := fuWriteFile(dst, data, 0o644); err != nil {
			raise("Errno::EACCES", "Permission denied @ fileutils_cp - %s", dst)
		}
		return object.NilV
	})
	mod.smethods["copy"] = mod.smethods["cp"]

	sdef("touch", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		for _, p := range pathsOf(args[0]) {
			if _, err := fuStat(p); err != nil {
				if werr := fuWriteFile(p, nil, 0o644); werr != nil {
					raise("Errno::EACCES", "Permission denied @ fileutils_touch - %s", p)
				}
				continue
			}
			now := fuNow()
			if err := fuChtimes(p, now, now); err != nil {
				raise("Errno::EACCES", "Permission denied @ fileutils_touch - %s", p)
			}
		}
		return args[0]
	})

	// FileUtils.chmod(mode, list) applies a permission mode to each path, returning
	// the list — the form Puppet's file_impl#chmod drives. Like the rest of
	// FileUtils it routes through the os seam so the error branch is reachable on
	// every platform (chmod is largely a no-op on Windows, so the seam is the only
	// way to cover the failure path there).
	sdef("chmod", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		mode := os.FileMode(intArg(args[0]) & 0o7777)
		for _, p := range pathsOf(args[1]) {
			if err := fileChmod(p, mode); err != nil {
				raise("Errno::ENOENT", "No such file or directory @ fileutils_chmod - %s", p)
			}
		}
		return args[1]
	})

	// chown(user, group, list) sets ownership of each path, reusing File.chown's
	// id coercion: a nil user/group leaves that id unchanged (POSIX -1) and it
	// routes through the fileChown seam (a no-op on Windows, as in MRI). Puppet's
	// FileSystem#replace_file calls this on the destination's existing owner when
	// rewriting state.yaml / report files, so it must succeed rather than raise.
	chownFn := func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		uid, gid := chownID(args[0]), chownID(args[1])
		list := pathsOf(args[2])
		for _, p := range list {
			if err := fileChown(p, uid, gid); err != nil {
				raise("Errno::ENOENT", "No such file or directory @ fileutils_chown - %s", p)
			}
		}
		return args[2]
	}
	sdef("chown", chownFn)
	// chown_R recurses into directories; the directory walk is shared with chmod_R.
	sdef("chown_R", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		uid, gid := chownID(args[0]), chownID(args[1])
		for _, p := range pathsOf(args[2]) {
			for _, q := range fuWalk(p) {
				if err := fileChown(q, uid, gid); err != nil {
					raise("Errno::ENOENT", "No such file or directory @ fileutils_chown - %s", q)
				}
			}
		}
		return args[2]
	})
	// chmod_R recurses chmod into directories, as Puppet's report store driver does
	// (FileUtils.chmod_R(0o750, dir)).
	sdef("chmod_R", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		mode := os.FileMode(intArg(args[0]) & 0o7777)
		for _, p := range pathsOf(args[1]) {
			for _, q := range fuWalk(p) {
				if err := fileChmod(q, mode); err != nil {
					raise("Errno::ENOENT", "No such file or directory @ fileutils_chmod - %s", q)
				}
			}
		}
		return args[1]
	})

	// Genuinely platform-specific / stream operations: a clear NotImplementedError
	// rather than a silently-wrong result.
	notImpl := func(what string) NativeFn {
		return func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
			return raise("NotImplementedError", "FileUtils.%s not yet supported", what)
		}
	}
	for _, m := range []string{"copy_stream",
		"remove_entry_secure", "uptodate?", "ln", "ln_s", "ln_sf", "compare_file", "cp_r"} {
		sdef(m, notImpl(m))
	}
}

// fuWalk returns root followed by every path beneath it (files and
// directories), for the recursive FileUtils operations. A path that cannot be
// walked yields just itself, so the caller still attempts (and reports) the op.
func fuWalk(root string) []string {
	var out []string
	if err := fuWalkDir(root, func(p string) { out = append(out, p) }); err != nil {
		return []string{root}
	}
	return out
}

// --- File.realpath / File.realdirpath -----------------------------------------
//
// MRI resolves both through one recursive walk, realpath_rec (ruby/ruby v3_4_0
// file.c:4324), driven by rb_check_realpath_emulate (file.c:4437). Go's
// filepath.EvalSymlinks is close but not close enough for two reasons the specs
// notice: it refuses a path whose LAST component is a dangling symlink (MRI's
// realdirpath answers the link's target, file.c:4390), and its errors carry
// neither MRI's errno nor the component that actually failed.

// realpathResolving is the marker MRI puts in its loopcheck hash while a symlink
// is being followed (ID2SYM(resolving), file.c:4406); meeting it again means the
// link points at itself through some chain, which is ELOOP. It is deliberately
// not a valid path.
const realpathResolving = "\x00resolving"

// realpathWalk carries the state realpath_rec threads through its recursion: the
// path resolved so far, the loop-check table mapping an already-resolved
// component to its answer, and whether an absent final component is an error
// (strict, i.e. File.realpath) or the answer (File.realdirpath).
type realpathWalk struct {
	resolved string
	loop     map[string]string
	strict   bool
}

// realpathErr is the failure realpath_rec reports: an errno class, MRI's message
// for it, and the component path the message names. It travels as an error rather
// than a raise so File.realpath can relabel it — MRI reaches realpath(3) first
// there and reports failures against the whole argument under a different
// function name (rb_check_realpath_internal, file.c:4554).
type realpathErr struct {
	class, message, path string
}

func (e *realpathErr) Error() string { return e.message + " - " + e.path }

// realpathResolve walks p — an absolute, '/'-separated, already-expanded path —
// resolving every symlink, and returns the canonical path. With strict set, every
// component must exist; without it (File.realdirpath's RB_REALPATH_DIR), only the
// final component may be absent. trailingSep withdraws even that, which is
// realpath_rec's `*unresolved_firstsep` half of the
// `mode == RB_REALPATH_STRICT || !last || *unresolved_firstsep` test: MRI walks
// the path AS WRITTEN, so "dir/absent/" is an error where "dir/absent" is not.
// It is a separate argument because expanding the path (which the caller must do
// first, to apply the base directory) drops the separator.
func realpathResolve(p string, strict, trailingSep bool) (string, *realpathErr) {
	w := &realpathWalk{resolved: "/", loop: map[string]string{}, strict: strict}
	if err := w.walk(splitPathNames(p), "", !trailingSep); err != nil {
		return "", err
	}
	return w.resolved, nil
}

// splitPathNames splits an absolute path into its non-empty components, dropping
// the separators the walk does not need.
func splitPathNames(p string) []string {
	out := make([]string, 0, 8)
	for _, n := range strings.Split(p, "/") {
		if n != "" {
			out = append(out, n)
		}
	}
	return out
}

// walk is realpath_rec's loop over one run of names. fallback is the symlink
// whose target this run expands (empty at the top level): when resolving the
// target hits ENOENT but the symlink itself stats clean, MRI answers the symlink
// instead (file.c:4383). last says whether this run ends the whole path, which is
// what decides if its final component may be absent.
func (w *realpathWalk) walk(names []string, fallback string, last bool) *realpathErr {
	for i, name := range names {
		isLast := last && i == len(names)-1
		if name == "." {
			continue
		}
		if name == ".." {
			w.resolved = realpathParent(w.resolved)
			continue
		}
		testpath := realpathJoin(w.resolved, name)
		if prev, seen := w.loop[testpath]; seen {
			if prev == realpathResolving {
				return &realpathErr{"Errno::ELOOP", "Too many levels of symbolic links", testpath}
			}
			w.resolved = prev
			continue
		}
		fi, err := osLstat(testpath)
		if err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				return realpathSyscallErr(err, testpath)
			}
			if fallback != "" {
				if _, serr := osStat(fallback); serr == nil {
					w.resolved = fallback
					return nil
				}
			}
			if w.strict || !isLast {
				return &realpathErr{"Errno::ENOENT", "No such file or directory", testpath}
			}
			w.resolved = testpath
			return nil
		}
		if fi.Mode()&fs.ModeSymlink == 0 {
			w.resolved = testpath
			continue
		}
		link, lerr := osReadlink(testpath)
		if lerr != nil {
			return realpathSyscallErr(lerr, testpath)
		}
		w.loop[testpath] = realpathResolving
		if strings.HasPrefix(link, "/") {
			w.resolved = "/"
		}
		if err := w.walk(splitPathNames(link), testpath, isLast); err != nil {
			return err
		}
		w.loop[testpath] = w.resolved
	}
	return nil
}

// realpathSyscallErr turns a stat/readlink failure that is not ENOENT into the
// errno class and message MRI's rb_syserr_fail_path would report for it
// (ENOTDIR on a path that walks through a regular file, EACCES on an unreadable
// directory), falling back to the generic SystemCallError for anything else.
func realpathSyscallErr(err error, testpath string) *realpathErr {
	var eno syscall.Errno
	if errors.As(err, &eno) {
		if name, ok := errnoClasses[int64(eno)]; ok {
			return &realpathErr{"Errno::" + name, errnoStrerror(int64(eno)), testpath}
		}
		return &realpathErr{"SystemCallError", errnoStrerror(int64(eno)), testpath}
	}
	return &realpathErr{"Errno::ENOENT", "No such file or directory", testpath}
}

// realpathParent drops the last component of an already-resolved absolute path,
// the way realpath_rec handles "..": it truncates back to the previous separator
// and never past the root.
func realpathParent(resolved string) string {
	i := strings.LastIndexByte(resolved, '/')
	if i <= 0 {
		return "/"
	}
	return resolved[:i]
}

// realpathJoin appends one component to an already-resolved absolute path without
// doubling the root's separator.
func realpathJoin(resolved, name string) string {
	if resolved == "/" {
		return "/" + name
	}
	return resolved + "/" + name
}
