package vm_test

import (
	"strconv"
	"strings"
	"testing"
)

// TestStdlibProvidedModules exercises the pure-Ruby stdlib modules added for the
// Puppet require graph (Gem, English, ostruct, benchmark, forwardable, delegate,
// pathname, uri) plus the supporting VM features (special globals, user-defined
// operator dispatch, to_s/inspect dispatch in puts/print/p). Each case asserts
// the observable behaviour verified against MRI.
func TestStdlibProvidedModules(t *testing.T) {
	tests := []struct{ name, src, want string }{
		// --- Gem::Version -------------------------------------------------------
		{"gem_version_to_s", `p Gem::Version.new("1.2.3").to_s`, "\"1.2.3\"\n"},
		{"gem_version_cmp", `p(Gem::Version.new("1.2.3") <=> Gem::Version.new("1.2.10"))`, "-1\n"},
		{"gem_version_lt", `p(Gem::Version.new("1.2.3") < Gem::Version.new("1.10.0"))`, "true\n"},
		{"gem_version_eq_pad", `p(Gem::Version.new("1.0") == Gem::Version.new("1.0.0"))`, "true\n"},
		{"gem_version_segments", `p Gem::Version.new("3.4.1").segments`, "[3, 4, 1]\n"},
		{"gem_version_cmp_string", `p(Gem::Version.new("1.2.3") <=> "1.2.4")`, "-1\n"},
		{"gem_version_eq_string", `p(Gem::Version.new("1.0.0") == "1.0")`, "true\n"},
		{"gem_version_bump_short", `p Gem::Version.new("1.0").bump.to_s`, "\"2\"\n"},
		{"gem_version_bump_three", `p Gem::Version.new("1.2.3").bump.to_s`, "\"1.3\"\n"},
		{"gem_version_bump_one", `p Gem::Version.new("5").bump.to_s`, "\"6\"\n"},
		{"gem_version_prerelease_false", `p Gem::Version.new("1.2.3").prerelease?`, "false\n"},
		{"gem_version_prerelease_true", `p Gem::Version.new("1.2.a").prerelease?`, "true\n"},
		{"gem_version_correct", `p Gem::Version.correct?("1.2.3")`, "true\n"},
		{"gem_version_correct_nil", `p Gem::Version.correct?(nil)`, "false\n"},
		{"gem_version_release", `p Gem::Version.new("1.2.a").release.to_s`, "\"1.2\"\n"},
		{"gem_version_release_self", `p Gem::Version.new("1.2.3").release.to_s`, "\"1.2.3\"\n"},
		// self.class.name is the fully-qualified "Gem::Version" now that nested
		// constants carry their lexical namespace (matches MRI).
		{"gem_version_inspect", `p Gem::Version.new("1.2.3").inspect`, "\"#<Gem::Version \\\"1.2.3\\\">\"\n"},
		{"gem_version_eql", `p Gem::Version.new("1.2.3").eql?(Gem::Version.new("1.2.3"))`, "true\n"},
		{"gem_version_eql_false", `p Gem::Version.new("1.2.3").eql?(Gem::Version.new("1.2"))`, "false\n"},
		{"gem_version_hash", `p(Gem::Version.new("1.2").hash == Gem::Version.new("1.2").hash)`, "true\n"},
		{"gem_version_create_self", `v = Gem::Version.new("1.0"); p(Gem::Version.create(v).equal?(v))`, "true\n"},
		{"gem_version_create_nil", `p Gem::Version.create(nil)`, "nil\n"},
		{"gem_version_eq_nonversion", `p(Gem::Version.new("1.0") == 5)`, "false\n"},
		{"gem_version_empty", `p Gem::Version.new("").to_s`, "\"0\"\n"},

		// --- Gem::Requirement ---------------------------------------------------
		{"gem_req_ge", `p Gem::Requirement.new(">= 1.2").satisfied_by?(Gem::Version.new("1.5"))`, "true\n"},
		{"gem_req_ge_false", `p Gem::Requirement.new(">= 1.2").satisfied_by?(Gem::Version.new("1.0"))`, "false\n"},
		{"gem_req_twiddle", `p Gem::Requirement.new("~> 1.2").satisfied_by?(Gem::Version.new("1.9"))`, "true\n"},
		{"gem_req_twiddle_false", `p Gem::Requirement.new("~> 1.2").satisfied_by?(Gem::Version.new("2.0"))`, "false\n"},
		{"gem_req_default", `p Gem::Requirement.default.satisfied_by?(Gem::Version.new("0.1"))`, "true\n"},
		{"gem_req_bare", `p Gem::Requirement.new("1.2.3").satisfied_by?(Gem::Version.new("1.2.3"))`, "true\n"},
		{"gem_req_lt", `p Gem::Requirement.new("< 2").satisfied_by?(Gem::Version.new("1.9"))`, "true\n"},
		{"gem_req_le", `p Gem::Requirement.new("<= 2").satisfied_by?(Gem::Version.new("2"))`, "true\n"},
		{"gem_req_gt", `p Gem::Requirement.new("> 1").satisfied_by?(Gem::Version.new("2"))`, "true\n"},
		{"gem_req_ne", `p Gem::Requirement.new("!= 1").satisfied_by?(Gem::Version.new("2"))`, "true\n"},
		{"gem_req_eq", `p Gem::Requirement.new("= 1.2").satisfied_by?(Gem::Version.new("1.2"))`, "true\n"},
		{"gem_req_to_s", `p Gem::Requirement.new(">= 1.2").to_s`, "\">= 1.2\"\n"},
		{"gem_req_triple_equal", `p(Gem::Requirement.new(">= 1") === Gem::Version.new("2"))`, "true\n"},
		{"gem_req_create_self", `r = Gem::Requirement.new(">= 1"); p(Gem::Requirement.create(r).equal?(r))`, "true\n"},
		{"gem_req_parse_version", `p Gem::Requirement.new(Gem::Version.new("1.2")).satisfied_by?(Gem::Version.new("1.2"))`, "true\n"},
		{"gem_ruby_version", `p Gem.ruby_version.is_a?(Gem::Version)`, "true\n"},
		{"gem_win_platform", `p Gem.win_platform?`, "false\n"},
		{"require_rubygems", `p require("rubygems")`, "false\n"},
		{"require_set_preloaded", `p require("set")`, "false\n"},

		// --- English / special globals -----------------------------------------
		{"english_error_info", `require "English"; begin; raise "x"; rescue; p $ERROR_INFO.message; end`, "\"x\"\n"},
		{"english_pid", `require "English"; p($PID == $$)`, "true\n"},
		{"english_process_id", `require "English"; p($PROCESS_ID == $$)`, "true\n"},
		{"english_program_name", `require "English"; p($PROGRAM_NAME == $0)`, "true\n"},
		{"english_match", `require "English"; "ab cd" =~ /(\w+) (\w+)/; p $MATCH`, "\"ab cd\"\n"},
		{"english_prematch", `require "English"; "xab" =~ /ab/; p $PREMATCH`, "\"x\"\n"},
		{"english_postmatch", `require "English"; "abx" =~ /ab/; p $POSTMATCH`, "\"x\"\n"},
		{"english_last_match", `require "English"; "ab cd" =~ /(\w+) (\w+)/; p $LAST_MATCH_INFO[1]`, "\"ab\"\n"},
		{"require_english", `p require("English")`, "true\n"},
		{"gvar_bang_nil", `p $!`, "nil\n"},
		{"gvar_pid_class", `p $$.class`, "Integer\n"},
		{"program_name_assign", `$PROGRAM_NAME = "prog"; p $0`, "\"prog\"\n"},
		{"program_name_assign_via0", `$0 = "z"; p $PROGRAM_NAME`, "\"z\"\n"},
		{"error_info_assign", `$ERROR_INFO = nil; p $!`, "nil\n"},
		{"error_info_assign_value", `require "English"; $ERROR_INFO = RuntimeError.new("boom"); p $!.message`, "\"boom\"\n"},

		// --- OpenStruct ---------------------------------------------------------
		{"ostruct_read", `require "ostruct"; p OpenStruct.new(name: "Bob").name`, "\"Bob\"\n"},
		{"ostruct_write", `require "ostruct"; o = OpenStruct.new; o.city = "NYC"; p o.city`, "\"NYC\"\n"},
		{"ostruct_bracket", `require "ostruct"; o = OpenStruct.new(a: 1); p o[:a]`, "1\n"},
		{"ostruct_bracket_set", `require "ostruct"; o = OpenStruct.new; o[:z] = 9; p o.z`, "9\n"},
		{"ostruct_to_h", `require "ostruct"; p OpenStruct.new(a: 1, b: 2).to_h`, "{a: 1, b: 2}\n"},
		{"ostruct_unset", `require "ostruct"; p OpenStruct.new.foo`, "nil\n"},
		{"ostruct_respond", `require "ostruct"; p OpenStruct.new(a: 1).respond_to?(:a)`, "true\n"},
		{"ostruct_respond_setter", `require "ostruct"; p OpenStruct.new.respond_to?(:x=)`, "true\n"},
		{"ostruct_members", `require "ostruct"; p OpenStruct.new(a: 1, b: 2).members`, "[:a, :b]\n"},
		{"ostruct_eq", `require "ostruct"; p(OpenStruct.new(a: 1) == OpenStruct.new(a: 1))`, "true\n"},
		{"ostruct_eq_false", `require "ostruct"; p(OpenStruct.new(a: 1) == 5)`, "false\n"},
		{"ostruct_inspect_empty", `require "ostruct"; p OpenStruct.new.inspect`, "\"#<OpenStruct>\"\n"},
		{"ostruct_inspect", `require "ostruct"; p OpenStruct.new(a: 1).inspect`, "\"#<OpenStruct a=1>\"\n"},
		{"ostruct_each_pair", `require "ostruct"; o = OpenStruct.new(a: 1, b: 2); s = []; o.each_pair { |k, v| s << [k, v] }; p s`, "[[:a, 1], [:b, 2]]\n"},

		// --- Benchmark ----------------------------------------------------------
		{"bench_realtime", `require "benchmark"; p Benchmark.realtime { 1 + 1 }.is_a?(Float)`, "true\n"},
		{"bench_measure", `require "benchmark"; p Benchmark.measure { 1 + 1 }.is_a?(Benchmark::Tms)`, "true\n"},
		{"bench_total", `require "benchmark"; p Benchmark.measure { 1 }.total.is_a?(Float)`, "true\n"},
		{"bench_tms_to_s", `require "benchmark"; p Benchmark::Tms.new(1.0, 2.0, 0.0, 0.0, 3.0, "x").total`, "3.0\n"},
		{"require_benchmark", `p require("benchmark")`, "true\n"},

		// --- Forwardable --------------------------------------------------------
		{"forwardable", `require "forwardable"
class Q
  extend Forwardable
  def initialize; @a = []; end
  def_delegator :@a, :push, :add
  def_delegators :@a, :size, :first
end
q = Q.new; q.add(1); q.add(2); p [q.size, q.first]`, "[2, 1]\n"},
		{"forwardable_method_accessor", `require "forwardable"
class R
  extend Forwardable
  def inner; [10, 20]; end
  def_delegator :inner, :first, :head
end
p R.new.head`, "10\n"},
		{"single_forwardable", `require "forwardable"
obj = Object.new
obj.extend(SingleForwardable)
obj.instance_variable_set(:@s, "hello")
obj.def_single_delegator :@s, :upcase
p obj.upcase`, "\"HELLO\"\n"},
		{"require_forwardable", `p require("forwardable")`, "true\n"},

		// --- delegate -----------------------------------------------------------
		{"simple_delegator", `require "delegate"
class D < SimpleDelegator; end
p D.new([1, 2, 3]).size`, "3\n"},
		{"simple_delegator_block", `require "delegate"
class D < SimpleDelegator; end
p D.new([1, 2, 3]).map { |x| x * 2 }`, "[2, 4, 6]\n"},
		{"simple_delegator_setobj", `require "delegate"
d = SimpleDelegator.new("a"); d.__setobj__("bye"); p d.upcase`, "\"BYE\"\n"},
		{"simple_delegator_respond", `require "delegate"
p SimpleDelegator.new([1]).respond_to?(:push)`, "true\n"},
		{"simple_delegator_eq_self", `require "delegate"
d = SimpleDelegator.new(1); p(d == d)`, "true\n"},
		{"simple_delegator_eq_target", `require "delegate"
p(SimpleDelegator.new(5) == 5)`, "true\n"},
		{"delegate_class", `require "delegate"
K = DelegateClass(Array); p K.new([9, 8]).size`, "2\n"},
		{"delegate_class_method", `require "delegate"
K = DelegateClass(Array); p K.new([9, 8]).first`, "9\n"},
		{"require_delegate", `p require("delegate")`, "true\n"},

		// --- Pathname -----------------------------------------------------------
		{"pathname_to_s", `require "pathname"; puts Pathname.new("/usr/bin")`, "/usr/bin\n"},
		{"pathname_basename", `require "pathname"; puts Pathname.new("/usr/bin").basename`, "bin\n"},
		{"pathname_basename_suffix", `require "pathname"; puts Pathname.new("a/b.txt").basename(".txt")`, "b\n"},
		{"pathname_basename_anyext", `require "pathname"; puts Pathname.new("a/b.txt").basename(".*")`, "b\n"},
		{"pathname_dirname", `require "pathname"; puts Pathname.new("/usr/bin").dirname`, "/usr\n"},
		{"pathname_dirname_root", `require "pathname"; puts Pathname.new("/bin").dirname`, "/\n"},
		{"pathname_dirname_none", `require "pathname"; puts Pathname.new("bin").dirname`, ".\n"},
		{"pathname_extname", `require "pathname"; p Pathname.new("a/b.txt").extname`, "\".txt\"\n"},
		{"pathname_extname_none", `require "pathname"; p Pathname.new("/usr/bin").extname`, "\"\"\n"},
		{"pathname_plus", `require "pathname"; puts (Pathname.new("/usr") + "bin")`, "/usr/bin\n"},
		{"pathname_slash", `require "pathname"; puts (Pathname.new("/usr") / "bin")`, "/usr/bin\n"},
		{"pathname_join", `require "pathname"; puts Pathname.new("/a").join("b", "c")`, "/a/b/c\n"},
		{"pathname_absolute", `require "pathname"; p Pathname.new("/x").absolute?`, "true\n"},
		{"pathname_relative", `require "pathname"; p Pathname.new("x").relative?`, "true\n"},
		{"pathname_root", `require "pathname"; p Pathname.new("/").root?`, "true\n"},
		{"pathname_root_false", `require "pathname"; p Pathname.new("/a").root?`, "false\n"},
		{"pathname_cleanpath", `require "pathname"; puts Pathname.new("/a/b/../c").cleanpath`, "/a/c\n"},
		{"pathname_cleanpath_rel", `require "pathname"; puts Pathname.new("a/./b").cleanpath`, "a/b\n"},
		{"pathname_cleanpath_dotdot_rel", `require "pathname"; puts Pathname.new("../a").cleanpath`, "../a\n"},
		{"pathname_eq", `require "pathname"; p(Pathname.new("/a") == Pathname.new("/a"))`, "true\n"},
		{"pathname_cmp", `require "pathname"; p(Pathname.new("/a") < Pathname.new("/b"))`, "true\n"},
		{"pathname_cmp_nonpath", `require "pathname"; p(Pathname.new("/a") <=> 5)`, "nil\n"},
		{"pathname_hash", `require "pathname"; p(Pathname.new("/a").hash == Pathname.new("/a").hash)`, "true\n"},
		{"pathname_split", `require "pathname"; d, b = Pathname.new("/a/b").split; puts d; puts b`, "/a\nb\n"},
		{"pathname_each_filename", `require "pathname"; s = []; Pathname.new("/a/b").each_filename { |f| s << f }; p s`, "[\"a\", \"b\"]\n"},
		{"pathname_sub_ext", `require "pathname"; puts Pathname.new("a.txt").sub_ext(".md")`, "a.md\n"},
		{"pathname_to_str", `require "pathname"; p Pathname.new("/a").to_str`, "\"/a\"\n"},
		{"pathname_inspect", `require "pathname"; p Pathname.new("/a").inspect`, "\"#<Pathname:/a>\"\n"},
		{"pathname_from_pathname", `require "pathname"; p Pathname.new(Pathname.new("/a")).to_s`, "\"/a\"\n"},
		{"pathname_parent", `require "pathname"; puts Pathname.new("/a/b").parent`, "/a\n"},
		{"require_pathname", `p require("pathname")`, "true\n"},

		// --- URI ----------------------------------------------------------------
		{"uri_scheme", `require "uri"; p URI.parse("https://h.com/p").scheme`, "\"https\"\n"},
		{"uri_host", `require "uri"; p URI.parse("https://h.com/p").host`, "\"h.com\"\n"},
		{"uri_port", `require "uri"; p URI.parse("http://h.com:8080/").port`, "8080\n"},
		{"uri_path", `require "uri"; p URI.parse("http://h.com/a/b").path`, "\"/a/b\"\n"},
		{"uri_query", `require "uri"; p URI.parse("http://h.com/?q=1").query`, "\"q=1\"\n"},
		{"uri_fragment", `require "uri"; p URI.parse("http://h.com/#f").fragment`, "\"f\"\n"},
		{"uri_userinfo", `require "uri"; p URI.parse("http://u:p@h.com/").userinfo`, "\"u:p\"\n"},
		{"uri_to_s", `require "uri"; p URI.parse("https://u:p@h.com:8080/x?q=1#f").to_s`, "\"https://u:p@h.com:8080/x?q=1#f\"\n"},
		{"uri_to_s_default_port", `require "uri"; p URI.parse("http://h.com:80/x").to_s`, "\"http://h.com/x\"\n"},
		{"uri_join", `require "uri"; p URI.join("http://a.com/foo/", "bar").to_s`, "\"http://a.com/foo/bar\"\n"},
		{"uri_join_absolute_path", `require "uri"; p URI.join("http://a.com/foo/x", "/bar").to_s`, "\"http://a.com/bar\"\n"},
		{"uri_join_absolute_ref", `require "uri"; p URI.join("http://a.com/", "http://b.com/").to_s`, "\"http://b.com/\"\n"},
		{"uri_kernel", `require "uri"; p URI("http://x.com").host`, "\"x.com\"\n"},
		{"uri_kernel_passthrough", `require "uri"; u = URI("http://x.com"); p URI(u).equal?(u)`, "true\n"},
		{"uri_hostname", `require "uri"; p URI.parse("http://h.com/").hostname`, "\"h.com\"\n"},
		{"uri_eq", `require "uri"; p(URI.parse("http://h.com/") == URI.parse("http://h.com/"))`, "true\n"},
		{"uri_eq_false", `require "uri"; p(URI.parse("http://h.com/") == 5)`, "false\n"},
		{"uri_inspect", `require "uri"; p URI.parse("http://h.com/").inspect.include?("http://h.com/")`, "true\n"},
		{"uri_default_port", `require "uri"; p URI.parse("https://h.com/").default_port`, "443\n"},
		{"uri_merge_no_path", `require "uri"; p URI.parse("http://a.com/x").merge("?q=2").to_s`, "\"http://a.com/x?q=2\"\n"},
		{"uri_no_scheme", `require "uri"; p URI.parse("/just/a/path").path`, "\"/just/a/path\"\n"},
		{"uri_to_str", `require "uri"; p URI.parse("http://h.com/").to_str.class`, "String\n"},
		{"uri_build_hash", `require "uri"; p URI::Generic.build(scheme: "http", host: "h.com", port: 8080, path: "/p", query: "q=1", fragment: "f").to_s`, "\"http://h.com:8080/p?q=1#f\"\n"},
		{"uri_build_string_keys", `require "uri"; p URI::Generic.build("scheme" => "http", "host" => "h.com").to_s`, "\"http://h.com\"\n"},
		{"uri_build_subclass", `require "uri"; p URI::HTTP.build(host: "x.com").class`, "URI::HTTP\n"},
		{"uri_build_array", `require "uri"; p URI::Generic.build([nil, nil, "h.com", 80, "/p", "q", "f"]).to_s`, "\"//h.com:80/p?q#f\"\n"},
		{"uri_build_array_arity", `require "uri"; begin; URI::Generic.build([1, 2]); rescue => e; p e.class; end`, "ArgumentError\n"},
		{"uri_build_bad_type", `require "uri"; begin; URI::Generic.build(5); rescue => e; p e.class; end`, "ArgumentError\n"},
		{"require_uri", `p require("uri")`, "true\n"},

		// --- user-defined operator dispatch ------------------------------------
		{"op_plus", `class C; def +(o); "p#{o}"; end; end; p(C.new + "x")`, "\"px\"\n"},
		{"op_minus", `class C; def -(o); "m#{o}"; end; end; p(C.new - "x")`, "\"mx\"\n"},
		{"op_mul", `class C; def *(o); "t#{o}"; end; end; p(C.new * "x")`, "\"tx\"\n"},
		{"op_div", `class C; def /(o); "d#{o}"; end; end; p(C.new / "x")`, "\"dx\"\n"},
		{"op_mod", `class C; def %(o); "r#{o}"; end; end; p(C.new % "x")`, "\"rx\"\n"},

		// --- to_s / inspect dispatch in puts/print/p ---------------------------
		{"puts_user_to_s", `class Z; def to_s; "ZZ"; end; end; puts Z.new`, "ZZ\n"},
		{"print_user_to_s", `class Z; def to_s; "PZ"; end; end; print Z.new; puts`, "PZ\n"},
		{"p_user_inspect", `class Z; def inspect; "<Z>"; end; end; p Z.new`, "<Z>\n"},
		{"puts_array_flatten", `puts [1, [2, 3]]`, "1\n2\n3\n"},
		{"puts_empty_array", `puts []; puts "after"`, "after\n"},
		// to_s/inspect whose result is non-String falls back to the native string.
		{"puts_nonstring_to_s", `class Z; def to_s; 5; end; end; puts Z.new`, "#<Z>\n"},
		{"p_nonstring_inspect", `class Z; def inspect; 5; end; end; p Z.new`, "#<Z>\n"},

		// --- Kernel#hash for the value types and objects -----------------------
		{"hash_integer", `p 5.hash`, "5\n"},
		{"hash_nil", `p nil.hash`, "8\n"},
		{"hash_true", `p true.hash`, "1\n"},
		{"hash_false", `p false.hash`, "0\n"},
		{"hash_float_eq", `p(1.5.hash == 1.5.hash)`, "true\n"},
		{"hash_string_eq", `p("ab".hash == "ab".hash)`, "true\n"},
		{"hash_string_ne", `p("ab".hash == "cd".hash)`, "false\n"},
		{"hash_symbol_eq", `p(:x.hash == :x.hash)`, "true\n"},
		{"hash_array_eq", `p([1, 2].hash == [1, 2].hash)`, "true\n"},
		{"hash_array_ne", `p([1, 2].hash == [2, 1].hash)`, "false\n"},
		{"hash_bignum", `p((10**40).hash == (10**40).hash)`, "true\n"},
		{"hash_object_stable", `o = Object.new; p(o.hash == o.hash)`, "true\n"},
		{"hash_object_distinct", `p(Object.new.hash == Object.new.hash)`, "false\n"},
		{"hash_object_nonneg", `p(Object.new.hash >= 0)`, "true\n"},

		// --- gvar write paths ---------------------------------------------------
		{"gvar_user_set", `$x = 7; p $x`, "7\n"},

		// A built-in value subclass renders through its wrapped value's native
		// ToS/Inspect (RObject.ToS / RObject.Inspect builtin branch).
		{"string_subclass_inspect_in_array", `class MyStr < String; end; p [MyStr.new("hi")]`, "[\"hi\"]\n"},
		{"string_subclass_puts", `class MyStr < String; end; puts MyStr.new("hi")`, "hi\n"},
		{"int_subclass_to_s", `class MyArr < Array; end; p MyArr.new`, "[]\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := eval(t, tt.src)
			if out != tt.want {
				t.Fatalf("src=%q\n got %q\nwant %q", tt.src, out, tt.want)
			}
		})
	}
}

// TestStdlibErrors covers the error branches of the new modules.
func TestStdlibErrors(t *testing.T) {
	tests := []struct{ name, src, class, msgPart string }{
		{"gem_version_malformed", `Gem::Version.new("not a version!!")`, "ArgumentError", "Malformed"},
		{"gem_req_illformed", `Gem::Requirement.new("?? 1")`, "ArgumentError", "Illformed"},
		{"pathname_non_string", `require "pathname"; Pathname.new(5)`, "TypeError", "conversion"},
		// InvalidURIError reports its fully-qualified name now that nested constants
		// are namespaced (matches MRI).
		{"uri_invalid", `require "uri"; raise URI::InvalidURIError, "bad"`, "URI::InvalidURIError", "bad"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			class, msg := evalErr(t, tt.src)
			if class != tt.class {
				t.Fatalf("src=%q: got class %q, want %q", tt.src, class, tt.class)
			}
			if !strings.Contains(msg, tt.msgPart) {
				t.Fatalf("src=%q: msg %q missing %q", tt.src, msg, tt.msgPart)
			}
		})
	}
}

// TestProgramNameIsScriptPath checks $0 reflects the host-set script path when no
// explicit assignment overrides it.
func TestProgramNameIsScriptPath(t *testing.T) {
	// strconv is imported to keep the helper list aligned with phaseb_test usage.
	_ = strconv.Itoa(0)
	out, err := runProg(t, `p defined?($0)`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "\"global-variable\"\n" {
		t.Fatalf("got %q", out)
	}
}
