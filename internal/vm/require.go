package vm

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// preloadedFeatures are already loaded at startup (like MRI 4.x's preloaded
// gems), so require returns false. rubygems is preloaded by default in MRI, and
// the Gem module lives in the prelude, so its constants are usable with no
// require at all (Puppet references Gem::Version before requiring rubygems).
var preloadedFeatures = map[string]bool{"set": true, "rubygems": true}

// providedFeatures are standard-library names the VM supplies — either as
// built-in Go classes or as prelude pure-Ruby modules. The file never needs
// loading, but require still returns true on the first call and false
// afterwards, matching a normal gem load. English is listed under its MRI
// filename ("English", capital E); the lookup is case-sensitive like MRI's.
var providedFeatures = map[string]bool{
	"date": true, "time": true, "bigdecimal": true, "bag": true,
	"base64": true, "digest": true, "json": true, "multi_json": true, "zlib": true,
	"digest/md5": true, "digest/sha1": true, "digest/sha2": true,
	"digest/rmd160": true, "digest/bubblebabble": true,
	"stringio": true, "securerandom": true, "random/formatter": true,
	"English": true, "ostruct": true, "benchmark": true,
	"forwardable": true, "delegate": true, "pathname": true, "uri": true,
	"tmpdir": true, "openssl": true, "timeout": true, "rbconfig": true,
	"yaml": true, "fileutils": true, "getoptlong": true, "etc": true,
	"concurrent": true, "syslog": true, "cgi": true, "monitor": true,
	"observer": true,
	"socket":   true,
	"net/http": true, "net/https": true, "net/pop": true, "net/imap": true, "net/sftp": true, "net/ftp": true, "net/smtp": true, "resolv": true, "singleton": true,
	"net/ldap": true, "net-ldap": true, "ldap": true,
	"optparse": true, "ripper": true, "erb": true, "irb": true, "find": true,
	"tempfile": true, "open3": true,
	"strscan": true, "fiber": true, "objspace": true, "csv": true,
	"shellwords": true, "prime": true, "tsort": true, "abbrev": true,
	"did_you_mean": true, "cmath": true, "matrix": true, "ipaddr": true,
	"arrow":             true,
	"unicode_normalize": true, "scanf": true, "prettyprint": true,
	"rexml": true, "rexml/document": true,
	"logger": true, "pstore": true,
	"bcrypt": true, "jwt": true, "rbnacl": true,
	"age": true, "prawn": true,
	"google/protobuf": true, "protobuf": true, "bleve": true, "graphql": true,
	"opentelemetry": true, "faraday": true, "puma": true, "bolt": true,
	"httparty": true, "connection_pool": true, "concurrent-ruby": true,
	"erubi": true, "erubi/capture_end": true,
	"reline": true,
	"http":   true, "excon": true, "typhoeus": true,
	"pundit": true, "cancancan": true, "cancan": true,
	"friendly_id":    true,
	"ransack":        true,
	"active_support": true, "active_support/all": true, "activesupport": true,
	"active_support/core_ext": true, "active_support/inflector": true,
	"active_support/core_ext/string": true, "active_support/core_ext/array": true,
	"active_support/core_ext/hash": true, "active_support/core_ext/object": true,
	"active_support/core_ext/integer": true, "active_support/core_ext/enumerable": true,
	"active_support/core_ext/string/inflections": true,
	"saml": true, "ruby-saml": true, "webauthn": true,
	"acme": true, "acme/client": true,
	"grpc": true, "nats": true, "kafka": true, "etcd": true, "etcdv3": true,
	"vault": true, "openbao": true,
	"rolify": true,
	"vcr":    true,
	"mysql2": true, "mysql": true, "mongo": true, "bson": true, "parquet": true,
	"msgpack": true, "toml": true,
	"tzinfo": true, "chronic": true, "money": true, "timecop": true,
	"addressable": true, "addressable/uri": true, "addressable/template": true,
	"commonmark": true, "mustache": true, "jbuilder": true, "builder": true,
	"sqlite3": true, "nokogiri": true,
	"redis": true, "pg": true, "sequel": true,
	"sidekiq": true, "resque": true,
	"rspec": true, "rspec/expectations": true, "rspec/matchers": true,
	"factory_bot":         true,
	"facter":              true,
	"hiera":               true,
	"puppet":              true,
	"puppet/resource_api": true,
	"semantic_puppet":     true,
	"augeas":              true,
	"hocon":               true,
	"confd":               true,
	"fast_gettext":        true,
	"deep_merge":          true,
	"rubocop":             true,
	"simplecov":           true,
	"grape":               true,
	"rack":                true, "rack/utils": true,
	"webrick": true,
	"sinatra": true, "sinatra/base": true,
	"capybara": true, "capybara/dsl": true,
	"active_record": true, "activerecord": true,
	"active_ldap": true, "activeldap": true,
	"kaminari":     true,
	"paper_trail":  true,
	"active_model": true, "activemodel": true,
	"active_job": true, "activejob": true,
	"active_storage": true, "activestorage": true,
	"action_cable": true, "actioncable": true,
	"action_view": true, "actionview": true,
	"action_controller": true, "action_dispatch": true, "actionpack": true, "abstract_controller": true,
	"rails": true, "rails/railtie": true, "rails/engine": true, "rails/application": true,
	"rails/all": true,
	"devise":    true,
	"hanami":    true, "hanami/router": true, "hanami/action": true,
	"warden": true, "omniauth": true,
	"public_suffix": true, "mime/types": true, "mail": true, "faker": true,
	"action_mailer": true, "actionmailer": true,
	"rqrcode": true, "dotenv": true, "hcl2": true, "kramdown": true,
	"pagy":     true,
	"images":   true,
	"opentype": true,
	"widgets":  true, "tui": true, "mvvm": true,
	"shrine": true,
	"liquid": true, "rouge": true, "slim": true, "haml": true,
	"sass": true, "jekyll": true,
	"dry/types": true, "dry-types": true,
	"dry/struct": true, "dry-struct": true,
	"dry/validation": true, "dry-validation": true, "dry/schema": true, "dry-schema": true,
	"oauth2":         true,
	"openid_connect": true, "oidc": true,
	"i18n":     true,
	"zeitwerk": true,
	"rss":      true,
	"rdoc":     true, "rdoc/markup": true,
	"thor": true,
	"rake": true, "rake/dsl_definition": true, "rake/task": true,
	"rake/file_task": true, "rake/file_list": true,
	"capistrano": true, "capistrano/all": true, "capistrano/setup": true, "capistrano/deploy": true,
	"bundler":     true,
	"roda":        true,
	"async":       true,
	"racc/parser": true, "racc": true,
	"minitest": true, "minitest/autorun": true, "minitest/spec": true,
	"minitest/test": true, "minitest/unit": true,
	"aasm":    true,
	"webmock": true, "webmock/minitest": true,
	"openstack": true,
}

// registerRequire installs Kernel#require and #require_relative — the runtime
// loader half of the embedded front-end (the eval half is in eval.go). A file is
// parsed, compiled and run once at the top level; a second require returns false.
func (vm *VM) registerRequire() {
	vm.cObject.define("require", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.doRequire(requireName(vm, args), false)
	})
	vm.cObject.define("require_relative", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.doRequire(requireName(vm, args), true)
	})
	// Kernel#load re-executes a file every call (unlike require, which loads once)
	// and returns true. Exposed as a private instance method (a bare `load`) and as
	// a module method on Kernel (`Kernel.load`, the form Puppet's autoloader uses).
	loadFn := func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.doLoad(requireName(vm, args))
	}
	vm.cObject.define("load", loadFn)
	if kernel, ok := vm.consts["Kernel"].(*RClass); ok {
		kernel.smethods["load"] = &Method{name: "load", owner: kernel, native: loadFn}
	}
}

// doLoad implements Kernel#load: it locates name (an explicit path, or a file
// searched on $LOAD_PATH and the CWD) and executes it, always re-running it
// regardless of any prior load/require. The .rb suffix is NOT auto-appended (MRI
// loads the literal name), and a missing file raises LoadError.
func (vm *VM) doLoad(name string) object.Value {
	for _, cand := range vm.requireCandidates(name, false) {
		src, err := os.ReadFile(cand)
		if err != nil {
			continue // not found / unreadable — try the next candidate
		}
		abs := featurePath(cand)
		iseq, cerr := parseCompileFn(string(src))
		if cerr != nil {
			return raise("SyntaxError", "%s", cerr.Error())
		}
		iseq.Name = abs
		setISeqFile(iseq, abs)
		vm.requireDirs = append(vm.requireDirs, filepath.Dir(abs))
		defer func() { vm.requireDirs = vm.requireDirs[:len(vm.requireDirs)-1] }()
		vm.exec(iseq, vm.main, nil, vm.cObject, "", nil, nil, nil, nil, nil)
		return object.Bool(true)
	}
	return vm.raiseLoadError(name)
}

// requireName coerces the feature argument to its path string exactly as MRI's
// require/require_relative/load do: load.c v3_4_0 runs every one of them through
// rb_get_path (file.c), which is rb_get_path_check_to_string followed by
// rb_get_path_check_convert.
//
// rb_get_path_check_to_string takes a String as-is; otherwise it calls #to_path
// through rb_check_funcall_default(obj, to_path, 0, 0, obj) — the default is the
// object itself, so an object with no #to_path passes through unchanged — and
// then runs StringValue over the RESULT. That second step is why a #to_path that
// yields a non-String has #to_str called on THAT value rather than raising: the
// conversion chain is #to_path then #to_str, not one or the other.
func requireName(vm *VM, args []object.Value) string {
	s := vm.requirePathStr(args[0])
	// rb_get_path_check_convert: an ASCII-incompatible encoding is a
	// CompatibilityError and an embedded NUL an ArgumentError, before the name is
	// ever looked at as a path.
	vm.checkPathEncoding(s)
	name := string(s.Bytes())
	if strings.IndexByte(name, 0) >= 0 {
		raise("ArgumentError", "path name contains null byte")
	}
	return name
}

// requirePathStr is MRI's rb_get_path_check_to_string (file.c v3_4_0). It differs
// from the File-side pathStr in the one case the require specs pin: when #to_path
// returns a non-String, MRI feeds that result to StringValue (so #to_str runs on
// it) instead of failing on the spot.
func (vm *VM) requirePathStr(v object.Value) *object.String {
	if s, ok := v.(*object.String); ok {
		return s
	}
	if vm.respondsToDynamic(v, "to_path") {
		v = vm.send(v, "to_path", nil, nil)
		if s, ok := v.(*object.String); ok {
			return s
		}
	}
	// StringValue(tmp): #to_str, or TypeError.
	if vm.respondsToDynamic(v, "to_str") {
		r := vm.send(v, "to_str", nil, nil)
		if s, ok := r.(*object.String); ok {
			return s
		}
		raise("TypeError", "can't convert %s to String (%s#to_str gives %s)",
			vm.builtinClassName(v), vm.builtinClassName(v), vm.builtinClassName(r))
	}
	// rb_builtin_class_name spells nil/true/false as those words: MRI answers
	// `require nil` with "no implicit conversion of nil into String".
	raise("TypeError", "no implicit conversion of %s into String", vm.builtinClassName(v))
	return nil
}

// raiseLoadError raises the "cannot load such file" LoadError carrying the
// unresolved name in @path, so LoadError#path reports it. MRI: load.c
// load_failed -> rb_load_fail -> error.c raise_loaderror, which sets the
// exception's path ivar to the fname as it was PASSED IN, not as expanded.
func (vm *VM) raiseLoadError(name string) object.Value {
	vm.raiseWithIvars("LoadError", "cannot load such file -- "+name,
		map[string]object.Value{"@path": object.NewString(name)})
	return object.NilV
}

func (vm *VM) doRequire(name string, relative bool) object.Value {
	// Features the VM provides as built-in classes need no file.
	if !relative {
		if preloadedFeatures[name] {
			return object.Bool(false) // already loaded at startup
		}
		if providedFeatures[name] {
			key := "feature:" + name
			if vm.loaded[key] {
				return object.Bool(false)
			}
			vm.loaded[key] = true
			// Some features install their Ruby surface lazily on first require
			// (MRI-style), not eagerly at startup — e.g. shellwords creates the
			// Shellwords module and the String/Array core extensions here.
			if hook := vm.featureHooks[name]; hook != nil {
				hook()
			}
			return object.Bool(true)
		}
	}

	// load.c v3_4_0 search_required asks rb_feature_p BEFORE it touches the
	// filesystem: a feature $LOADED_FEATURES already carries — under the name as
	// written, or under a $LOAD_PATH prefix — is provided, and require reports
	// false without looking for a file at all.
	if !relative && vm.featureProvided(name) {
		return object.Bool(false)
	}

	file := name
	if !strings.HasSuffix(file, ".rb") {
		file += ".rb"
	}
	// rb_f_require_relative (load.c v3_4_0) expands the name against the requiring
	// file's directory BEFORE handing it to require_internal, so the failing name
	// load_failed reports -- and the @path it stamps -- is the expanded, cleaned
	// absolute path of the name AS PASSED, with no ".rb" appended.
	errName := name
	if relative {
		errName = vm.requireRelativeBase(name)
	}
	// Try each candidate by reading it directly — the read is the existence test.
	for _, cand := range vm.requireCandidates(file, relative) {
		src, err := os.ReadFile(cand)
		if err != nil {
			continue // not found / unreadable — try the next candidate
		}
		abs := featurePath(cand)
		// $LOADED_FEATURES is the authority on what has been required (MRI's
		// rb_feature_provided reads that list), so a program that removes an entry
		// — ruby/spec saves and restores $" around every example — makes the next
		// require run the file again.
		if vm.featureDropped(abs) {
			delete(vm.loaded, abs)
		}
		if vm.loaded[abs] {
			// A require of a feature whose own load has not finished yet is MRI's
			// circular require: load_lock (load.c v3_4_0) finds the feature in the
			// loading table, warns when the caller asked for warnings, then lets the
			// second require return without running the file again.
			if vm.loaded[vm.requireLoadingKey(abs)] {
				vm.warnCircularRequire(abs)
			}
			return object.Bool(false)
		}
		iseq, cerr := parseCompileFn(string(src))
		if cerr != nil {
			return raise("SyntaxError", "%s", cerr.Error())
		}
		iseq.Name = abs
		// Stamp the file on this ISeq and every nested child so __FILE__ in a method
		// reports the file the method was defined in, regardless of where it is
		// called from.
		setISeqFile(iseq, abs)
		vm.loaded[abs] = true
		loadingKey := vm.requireLoadingKey(abs)
		vm.loaded[loadingKey] = true
		defer delete(vm.loaded, loadingKey)
		vm.noteLoadedFeature(abs)
		// A require whose file raises is NOT a completed require: MRI's
		// require_internal unregisters the feature when the load unwinds, so the
		// next require of the same file runs it again. Reference: ruby/ruby v3_4_0
		// load.c require_internal / rb_provide_feature's rollback on exception.
		ok := false
		defer func() {
			if !ok {
				vm.forgetLoadedFeature(abs)
			}
		}()
		// Push the file's directory so a nested require_relative resolves against it.
		vm.requireDirs = append(vm.requireDirs, filepath.Dir(abs))
		defer func() { vm.requireDirs = vm.requireDirs[:len(vm.requireDirs)-1] }()
		vm.exec(iseq, vm.main, nil, vm.cObject, "", nil, nil, nil, nil, nil)
		ok = true
		return object.Bool(true)
	}
	// Nothing on disk. MRI still returns false when the feature is ALREADY
	// PROVIDED under the bare name: search_required (load.c v3_4_0) consults
	// rb_feature_p before rb_find_file_ext, and for a name carrying no extension
	// an entry in $LOADED_FEATURES equal to that name answers 'u' --
	//
	//	if (!*(e = f + len)) { if (ext) continue; return 'u'; }
	//
	// -- whereupon `case 0: if (ft) goto feature_present;` returns with *path == 0
	// and require_internal reports false rather than failing to load. Only the
	// verbatim form is matched here; MRI also accepts an entry spelled
	// "<$LOAD_PATH entry>/<name>", and its loaded-features index carries a
	// "distractor" rule for entries with an unusable extension, neither of which
	// this check reproduces -- it can only turn a LoadError into false, never the
	// other way round, so it cannot hide a require that would otherwise work.
	if !relative && !strings.Contains(filepath.Base(name), ".") {
		if arr, ok := vm.globals["$LOADED_FEATURES"].(*object.Array); ok && featureListed(arr, name) {
			return object.Bool(false)
		}
	}
	return vm.raiseLoadError(errName)
}

// requireLoadingKey names the vm.loaded entry marking a feature whose load is
// still running ON THIS THREAD. MRI keeps that in a loading table (load.c
// get_loading_table) holding a thread shield per feature, and load_lock warns
// only when rb_thread_shield_owned says the CURRENT thread already holds it --
// another thread arriving at a feature mid-load is not a circular require, it is
// a concurrent one, and it waits instead of warning. Keying the marker by thread
// reproduces that test; the entry goes in vm.loaded as the "feature:" entries for
// built-in features already do, so no VM field is needed.
func (vm *VM) requireLoadingKey(abs string) string {
	return fmt.Sprintf("loading:%p:%s", vm.currentThread, abs)
}

// warnCircularRequire emits MRI's circular-require warning. load_lock issues it
// through rb_warning, not rb_warn, so it appears only in verbose mode ($VERBOSE
// true) -- `require_internal(ec, fname, 1, RTEST(ruby_verbose))` passes the
// verbose flag down as load_lock's `warn` argument. It goes to the current
// $stderr so a reassigned $stderr (mspec's `complain` matcher) captures it.
func (vm *VM) warnCircularRequire(path string) {
	if v, ok := vm.globals["$VERBOSE"].(object.Bool); !ok || !bool(v) {
		return
	}
	vm.curStderr().writeStr("warning: loading in progress, circular require considered harmful - " + path + "\n")
}

// setISeqFile stamps path onto iseq and all of its nested children, so a method
// or block body compiled from this file reports the file via __FILE__ even when
// invoked from another file. Already-stamped children are left alone (an ISeq is
// only ever loaded from one file).
func setISeqFile(iseq *bytecode.ISeq, path string) {
	if iseq == nil || iseq.File == path {
		return
	}
	iseq.File = path
	for _, c := range iseq.Children {
		setISeqFile(c, path)
	}
}

// requireRelativeBase expands name the way rb_f_require_relative does --
// rb_file_absolute_path(fname, dirname(rb_current_realfilepath())) -- leaving an
// already-absolute name alone and cleaning the result, which is what MRI names in
// the LoadError of a require_relative that finds nothing.
func (vm *VM) requireRelativeBase(name string) string {
	if filepath.IsAbs(name) {
		return featurePath(name)
	}
	dir := vm.currentDir()
	if f := vm.currentFile(); f != "" {
		dir = filepath.Dir(f)
	}
	return featurePath(filepath.Join(dir, name))
}

// requireCandidates lists the paths to try for file. require_relative resolves
// against the requiring file's directory; a plain require searches that
// directory, the process CWD, then each $LOAD_PATH entry; an absolute path is
// used as-is.
func (vm *VM) requireCandidates(file string, relative bool) []string {
	switch {
	case relative:
		// rb_f_require_relative (load.c v3_4_0) is
		// rb_require_string_internal(rb_file_absolute_path(fname, dirname(base))):
		// rb_file_absolute_path IGNORES the base when fname is already absolute, so
		// an absolute require_relative argument is used verbatim and must not be
		// joined onto the requiring file's directory.
		if filepath.IsAbs(file) {
			return []string{file}
		}
		// Otherwise resolve against the directory of the file where the call is
		// written — the executing ISeq's file — so a require_relative inside a
		// method works even when that method is called from another file. Fall back
		// to the require stack's directory when no file is stamped (e.g. a -e script).
		if f := vm.currentFile(); f != "" {
			return []string{filepath.Join(filepath.Dir(f), file)}
		}
		return []string{filepath.Join(vm.currentDir(), file)}
	case strings.HasPrefix(file, "~"):
		// rb_find_file (file.c v3_4_0) expands a leading ~ through HOME first and
		// then takes the expanded name as the ONLY candidate: `if (!rb_file_load_ok(f))
		// return 0;` returns before the $LOAD_PATH walk, so a ~ path is never
		// searched on the load path.
		return []string{expandTildePath(file)}
	case filepath.IsAbs(file), isExplicitRelative(file):
		// Same early return in rb_find_file for an absolute path and for an
		// "explicitly relative" one — is_explicit_relative(f) is true for "./x" and
		// "../x". Both resolve against the process working directory alone; neither
		// is ever joined onto a $LOAD_PATH entry.
		return []string{file}
	default:
		cands := []string{filepath.Join(vm.currentDir(), file), file}
		for _, dir := range vm.loadPathDirs() {
			cands = append(cands, filepath.Join(dir, file))
		}
		return cands
	}
}

// isExplicitRelative reports whether a path is "explicitly relative" in MRI's
// sense — is_explicit_relative (file.c v3_4_0):
//
//	if (*path++ != '.') return 0;
//	if (*path == '.') path++;
//	return isdirsep(*path);
//
// i.e. exactly a leading "./" or "../". A name that merely begins with a dot
// ("..foo", ".hidden") is NOT explicitly relative and is still searched on
// $LOAD_PATH.
//
// isdirsep is PLATFORM-DEPENDENT in MRI: on POSIX it is `(x) == '/'`, and only
// on Windows does it also accept a backslash. os.IsPathSeparator draws exactly
// that line, so ".\x" is explicitly relative on the windows lane and an ordinary
// $LOAD_PATH name everywhere else -- as it is under MRI on each.
func isExplicitRelative(p string) bool {
	if len(p) == 0 || p[0] != '.' {
		return false
	}
	p = p[1:]
	if len(p) > 0 && p[0] == '.' {
		p = p[1:]
	}
	return len(p) > 0 && os.IsPathSeparator(p[0])
}

// loadPathDirs returns the directory strings currently in $LOAD_PATH, coercing
// each entry through MRI's rb_get_path as rb_find_file does.
func (vm *VM) loadPathDirs() []string {
	lp, ok := vm.globals["$LOAD_PATH"].(*object.Array)
	if !ok {
		return nil
	}
	dirs := make([]string, 0, len(lp.Elems))
	for _, e := range lp.Elems {
		// rb_find_file (file.c v3_4_0) runs every $LOAD_PATH entry through
		// rb_get_path(str) as it walks the list, so an entry that is not a String
		// but answers #to_path (a Pathname, say) is a usable load-path directory.
		dirs = append(dirs, string(vm.requirePathStr(e).Bytes()))
	}
	return dirs
}

// currentDir is the directory of the file currently being required, falling back
// to the script directory and then the process CWD.
func (vm *VM) currentDir() string {
	if n := len(vm.requireDirs); n > 0 {
		return vm.requireDirs[n-1]
	}
	return "."
}

// featurePath is the absolute path of a file as Ruby names it. MRI expands a
// required file through rb_file_expand_path, which on Windows also turns the
// separators into forward slashes — so $LOADED_FEATURES, __FILE__ and the
// backtrace all carry forward slashes there, and a program that builds a path
// itself and looks for it in $" finds it. filepath.Abs alone hands back
// backslashes on Windows, where none of those comparisons would hold. On POSIX
// ToSlash is the identity, so both lanes run the same code.
//
// Abs only fails when the process has no working directory, and every caller
// has just stat'ed or read the file it is asking about; the error is ignored
// here exactly as it was at each call site before.
func featurePath(cand string) string {
	abs, _ := filepath.Abs(cand)
	return filepath.ToSlash(abs)
}

// rbExts and soExts are MRI's IS_RBEXT / IS_SOEXT|IS_DLEXT sets as far as the
// loaded-features bookkeeping is concerned. rbgo only ever executes Ruby source,
// but an entry in $LOADED_FEATURES carrying a native extension still SATISFIES a
// require in MRI, so the match has to recognise those spellings.
var (
	rbExts = []string{".rb"}
	soExts = []string{".so", ".o", ".bundle", ".dylib", ".dll"}
)

func extIn(e string, set []string) bool {
	for _, x := range set {
		if strings.EqualFold(e, x) {
			return true
		}
	}
	return false
}

// featureExt returns the extension of a required name the way search_required
// picks it: the last '.' of the name, and only when no '/' follows it (so
// "a.b/c" has no extension). An empty result means the name carries none.
func featureExt(name string) string {
	i := strings.LastIndex(name, ".")
	if i < 0 || strings.ContainsAny(name[i:], "/") {
		return ""
	}
	return name[i:]
}

// featureProvided is load.c v3_4_0 rb_feature_p with expanded = FALSE: it reports
// whether $LOADED_FEATURES already carries an entry that satisfies a require of
// name, WITHOUT touching the filesystem. MRI looks for an entry equal either to
//
//	"#{name}#{e}"                       or
//	"#{load_path[j]}/#{name}#{e}"
//
// for an acceptable (possibly empty) extension e, which is why a feature stays
// loaded once when $LOAD_PATH is rearranged under it, and why an entry a program
// pushed by hand — ruby/spec pushes "./load_fixture.rb" — suppresses the require
// of that same spelling even though nothing expanded it.
//
// rbgo's own "have I run this file?" cache is keyed by absolute path, which
// answers neither question: two different spellings of one file share an
// absolute path (so the cache catches them) but one spelling of a feature
// recorded under another prefix does not.
func (vm *VM) featureProvided(name string) bool {
	arr, ok := vm.globals["$LOADED_FEATURES"].(*object.Array)
	if !ok {
		return false
	}
	ext := featureExt(name)
	rb := extIn(ext, rbExts)
	if ext != "" && !rb && !extIn(ext, soExts) {
		// An unrecognised extension is not one search_required dispatches on: it
		// falls through to the extension-less form, where the whole name (dot and
		// all) is the feature.
		ext = ""
	}
	stem := name[:len(name)-len(ext)]
	var loadPath []string
	for _, v := range arr.Elems {
		s, isStr := v.(*object.String)
		if !isStr {
			continue
		}
		f := s.Str()
		if len(f) < len(stem) {
			continue
		}
		if !strings.HasPrefix(f, stem) {
			if loadPath == nil {
				loadPath = vm.expandedLoadPath()
			}
			p := loadedFeaturePath(f, stem, ext, loadPath)
			if p < 0 {
				continue
			}
			f = f[p+1:]
			if len(f) < len(stem) || !strings.HasPrefix(f, stem) {
				continue
			}
		}
		e := f[len(stem):]
		if e == "" {
			// An entry spelled exactly like the feature answers 'u' (already
			// provided) only when the require carried no extension of its own.
			if ext == "" {
				return true
			}
			continue
		}
		if e[0] != '.' {
			continue
		}
		if (!rb || ext == "") && extIn(e, soExts) {
			return true
		}
		if (rb || ext == "") && extIn(e, rbExts) {
			return true
		}
	}
	return false
}

// loadedFeaturePath is load.c v3_4_0 loaded_feature_path: it reports the length
// of the $LOAD_PATH prefix of entry that makes it "#{prefix}/#{stem}#{e}" for an
// extension e acceptable to the requested type, or -1 when the entry has no such
// shape or its prefix is not on the load path. MRI returns the matching load-path
// String; the length is all the caller needs.
func loadedFeaturePath(entry, stem, ext string, loadPath []string) int {
	if len(entry) < len(stem)+1 {
		return -1
	}
	var plen int
	if strings.Contains(stem, ".") && strings.HasSuffix(entry, stem) {
		plen = len(entry) - len(stem)
	} else {
		// Scan back from the end for the dot of the entry's own extension,
		// stopping at a directory separator. The C loop starts ON the terminating
		// NUL (which is neither '.' nor '/'), so an entry whose last segment has
		// no dot cannot match here.
		e := len(entry)
		for e != 0 {
			c := byte(0)
			if e < len(entry) {
				c = entry[e]
			}
			if c == '.' || c == '/' {
				break
			}
			e--
		}
		if e >= len(entry) || entry[e] != '.' || e < len(stem) || entry[e-len(stem):e] != stem {
			return -1
		}
		plen = e - len(stem)
	}
	if plen > 0 && entry[plen-1] != '/' {
		return -1
	}
	tail := entry[plen+len(stem):]
	switch {
	case extIn(ext, soExts) && !extIn(tail, soExts):
		return -1
	case extIn(ext, rbExts) && !extIn(tail, rbExts):
		return -1
	}
	if plen > 0 {
		plen--
	}
	for _, p := range loadPath {
		if len(p) == plen && (plen == 0 || entry[:plen] == p) {
			return plen
		}
	}
	return -1
}

// expandedLoadPath is MRI's get_expanded_load_path: each $LOAD_PATH entry as an
// absolute, cleaned path, which is the form the loaded-features entries carry.
func (vm *VM) expandedLoadPath() []string {
	dirs := vm.loadPathDirs()
	out := make([]string, len(dirs))
	for i, d := range dirs {
		out[i] = featurePath(d)
	}
	return out
}
