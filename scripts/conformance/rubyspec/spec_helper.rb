# Copyright (c) the go-embedded-ruby/ruby authors
#
# SPDX-License-Identifier: BSD-3-Clause
#
# Minimal MSpec-compatible shim for scoring rbgo against ruby/spec.
# Loaded in place of ruby/spec's spec_helper.rb.
# Emulates: describe/it/before/after/context, should/should_not (operator +
# predicate method_missing + .raise), be_close matcher, version/platform/engine
# guards, and mspec mock objects. Prints RBGO_RESULT... at exit.

class SpecFail < Exception; end
class SpecSkip < Exception; end

$RB_PASS = 0
$RB_FAIL = 0
$RB_ERROR = 0
$RB_SKIP = 0
$RB_FAILS = []   # [kind, describe, it, exc_class, msg]
$mock_registry = nil
$mock_installs = nil

CUR_VERSION = "3.4.1"
PLATFORMS = [:darwin, :bsd, :unix]
WORDSIZE = 64
ENDIAN = :little

# mspec/lib/mspec/utils/warnings.rb turns deprecation warnings ON and
# experimental warnings OFF for every spec run, because ruby/spec asserts the
# deprecation texts. Without this, a `complain(/is deprecated/)` example cannot
# pass on an engine that honours Warning[:deprecated] — MRI 4.0.5 printed
# nothing and lost ten examples in language/predefined_spec.rb alone, while rbgo
# (which warns unconditionally) passed them. That is the judge favouring us, so
# it is the gap this exists to close.
if Object.const_defined?(:Warning) && Warning.respond_to?(:[]=)
  begin
    Warning[:deprecated] = true
    Warning[:experimental] = false
  rescue StandardError, NotImplementedError
  end
end

# mspec/lib/mspec/guards/platform.rb. Only the entry points the corpus reaches
# are provided; PLATFORMS/WORDSIZE above stay the single source of truth so the
# guard helpers and this class cannot disagree.
class PlatformGuard
  PLATFORM = RUBY_PLATFORM
  C_LONG_SIZE = WORDSIZE
  POINTER_SIZE = WORDSIZE

  def self.implementation?(*args)
    args.any? { |name| RUBY_ENGINE.start_with?(name == :rubinius ? 'rbx' : name.to_s) }
  end
  def self.standard?; implementation?(:ruby); end
  def self.os?(*oses); oses.any? { |os| os == :windows ? !!(PLATFORM =~ /(mswin|mingw)/) : PLATFORM.include?(os.to_s) }; end
  def self.windows?; os?(:windows); end
  def self.wasi?; os?(:wasi); end
  def self.wsl?; false; end
  def self.c_long_size?(size); size == C_LONG_SIZE; end
  def self.pointer_size?(size); size == POINTER_SIZE; end
  def self.wordsize?(size); size == WORDSIZE; end
  # Build-time darwin version, exactly as mspec/lib/mspec/guards/version.rb reads
  # it, so kernel_version_is needs no subprocess on this platform.
  def self.kernel_version
    @kernel_version ||= (RUBY_PLATFORM[/darwin(\d+)/, 1] || `uname -r`.chomp)
  end
end

# mspec/lib/mspec/helpers/io.rb. A write-only stand-in for an IO that collects
# what was written and then behaves like the resulting String. The corpus uses it
# directly (core/string/modulo_spec.rb, core/thread/abort_on_exception_spec.rb,
# core/io/*), and mspec's own complain/output matchers capture through it.
class IOStub
  def initialize; @buffer = []; @output = +''; end
  def write(*str); self << str.join(''); end
  def <<(str); @buffer << str; self; end
  def print(*str); write(str.join('') + $\.to_s); end
  def method_missing(name, *args, &block); to_s.send(name, *args, &block); end
  def respond_to_missing?(name, include_private = false); to_s.respond_to?(name, include_private); end
  def ==(other); to_s == other; end
  def =~(other); to_s =~ other; end
  def puts(*str)
    if str.empty?
      write "\n"
    else
      write(str.collect { |s| s.to_s.chomp }.concat([nil]).join("\n"))
    end
  end
  def printf(format, *args); self << sprintf(format, *args); end
  def flush; @output += @buffer.join(''); @buffer.clear; self; end
  def to_s; flush; @output; end
  alias_method :to_str, :to_s
  def inspect; to_s.inspect; end
end

def _ver_cmp(a, b)
  pa = a.to_s.split('.').map { |x| x.to_i }
  pb = b.to_s.split('.').map { |x| x.to_i }
  n = pa.size > pb.size ? pa.size : pb.size
  i = 0
  while i < n
    x = pa[i] || 0
    y = pb[i] || 0
    return (x <=> y) if x != y
    i += 1
  end
  0
end

def _version_in_range(range, cur = CUR_VERSION)
  if range.is_a?(String)
    return true if range.empty?
    _ver_cmp(cur, range) >= 0
  elsif range.is_a?(Range)
    lo = range.begin
    hi = range.end
    okl = lo.nil? || lo == "" || _ver_cmp(cur, lo) >= 0
    if hi.nil? || hi == ""
      okh = true
    elsif range.exclude_end?
      okh = _ver_cmp(cur, hi) < 0
    else
      okh = _ver_cmp(cur, hi) <= 0
    end
    okl && okh
  else
    true
  end
end

# ---------------- matchers ----------------
class BeCloseMatcher
  def initialize(exp, tol); @exp = exp; @tol = tol; end
  def matches?(actual)
    return false if actual.nil?
    (actual - @exp).abs <= @tol
  end
  def failure_message; "expected to be close to #{@exp} (+/- #{@tol})"; end
end

TOLERANCE = 0.00003
# Faithful to real mspec (mspec/lib/mspec/matchers/be_close.rb): a top-level
# (Object) constant, generously large "to account for GC, context switches,
# other processes, load, etc." Specs pass it as a timeout to blocking calls
# (e.g. Queue#pop(timeout: TIME_TOLERANCE)); without it those references raise
# NameError inside a spawned thread, the thread dies, and the sibling
# `Thread.pass until t.status == "sleep"` loop then spins forever, hanging the
# whole file into a timeout/FILEFAIL. Defining it lets the file run to
# completion so its non-timeout examples score.
TIME_TOLERANCE = 20.0 unless Object.const_defined?(:TIME_TOLERANCE)
def be_close(exp, tol = TOLERANCE); BeCloseMatcher.new(exp, tol); end

# be_computed_by(sym, *extra): the receiver is an Array of rows, each
# [receiver, *args, expected]; the matcher calls receiver.send(sym, *args, *extra)
# and checks it == expected for every row. This is a real MSpec matcher (used
# heavily by core/encoding), so implementing it turns those examples from skipped
# into genuinely scored.
class BeComputedByMatcher
  def initialize(sym, *extra); @sym = sym; @extra = extra; end
  def matches?(array)
    @bad = nil
    array.each do |row|
      row = row.dup
      receiver = row.shift
      expected = row.pop
      actual = receiver.send(@sym, *(row + @extra))
      unless actual == expected
        @bad = "#{receiver.inspect}.#{@sym}(#{row.map(&:inspect).join(', ')}) => #{actual.inspect}, expected #{expected.inspect}"
        return false
      end
    end
    true
  end
  def failure_message; @bad || "be_computed_by(#{@sym}) mismatch"; end
end
def be_computed_by(sym, *extra); BeComputedByMatcher.new(sym, *extra); end
# complain: the block should (or should_not) write a warning to $stderr. An
# optional String (exact) or Regexp (match) constrains the warning text; the
# verbose: keyword runs the block under that $VERBOSE. Mirrors mspec's
# ComplainMatcher (mspec/lib/mspec/matchers/complain.rb).
class ComplainMatcher
  def initialize(pat, verbose); @pat, @verbose = pat, verbose; end
  def matches?(callable)
    # mspec captures through IOStub, not StringIO (mspec/matchers/complain.rb).
    err = IOStub.new
    old_err, $stderr = $stderr, err
    # mspec runs the block under $VERBOSE = false by default, or the given
    # verbose: value; it never leaves the ambient level in place.
    old_v = $VERBOSE
    $VERBOSE = @verbose.nil? ? false : @verbose
    begin
      callable.call
    ensure
      $VERBOSE = old_v
      $stderr = old_err
      out = err.to_s
      @out = out
    end
    # A constraining pattern must match, but a warning is still required: mspec
    # ends on `warning.empty? ? false : true` even after a successful match.
    unless @pat.nil?
      if @pat.is_a?(Regexp)
        return false unless out =~ @pat
      else
        return false unless out == @pat
      end
    end
    !out.empty?
  end
  def failure_message
    "expected a warning#{@pat ? " matching #{@pat.inspect}" : ''}, got #{@out.inspect}"
  end
end
def complain(pat = nil, verbose: nil); ComplainMatcher.new(pat, verbose); end

# output: capture $stdout (and, with a second argument, $stderr) around the block
# and match each against a String (exact) or Regexp. Mirrors mspec's
# OutputMatcher. output_to_fd (real fd redirection) stays unsupported.
class OutputMatcher
  def initialize(out, err); @out, @err = out, err; end
  def matches?(callable)
    # mspec/matchers/output.rb captures through IOStub.
    so_io, se_io = IOStub.new, IOStub.new
    oo, $stdout = $stdout, so_io
    oe, $stderr = $stderr, se_io
    begin
      callable.call
    ensure
      $stdout, $stderr = oo, oe
      so, se = so_io.to_s, se_io.to_s
      @got_out, @got_err = so, se
    end
    ok = true
    ok &&= match_stream(@out, so) unless @out.nil?
    ok &&= match_stream(@err, se) unless @err.nil?
    ok
  end
  def match_stream(pat, s); pat.is_a?(Regexp) ? !!(s =~ pat) : (s == pat); end
  def failure_message
    "output did not match: stdout #{@got_out.inspect} (want #{@out.inspect}), " \
      "stderr #{@got_err.inspect} (want #{@err.inspect})"
  end
end
def output(out = nil, err = nil); OutputMatcher.new(out, err); end
def output_to_fd(*a); raise SpecSkip, "output_to_fd matcher unsupported"; end

# mspec/lib/mspec/matchers/block_caller.rb: run the proc in a thread and decide
# from Thread#status whether it blocked.
class BlockingMatcher
  def matches?(block)
    t = Thread.new { block.call }
    loop do
      case t.status
      when "sleep"   # blocked
        t.kill
        t.join
        return true
      when false     # terminated normally, so it never blocked
        t.join
        return false
      when nil       # terminated exceptionally
        t.value
      else
        Thread.pass
      end
    end
  end
  def failure_message; "expected the given Proc to block the caller"; end
end
def block_caller; BlockingMatcher.new; end

# mspec/lib/mspec/matchers/signed_zero.rb
class SignedZeroMatcher
  def initialize(sign); @sign = sign; end
  def matches?(actual); @actual = actual; (1.0 / actual).infinite? == @sign; end
  def failure_message; "expected #{@actual.inspect} to be #{'-' if @sign == -1}0.0"; end
end
def be_positive_zero; SignedZeroMatcher.new(1); end
def be_negative_zero; SignedZeroMatcher.new(-1); end

# mspec/lib/mspec/matchers/skip.rb: `skip` inside an example aborts it as skipped.
class SkippedSpecError < SpecSkip; end
def skip(reason = 'no reason'); ::Kernel.raise(SkippedSpecError, reason); end

class RaiseMatcher
  def initialize(exc, msg); @exc = exc || Exception; @msg = msg; end
  def matches?(callable)
    begin
      @result = callable.call
      @raised = nil
      return false
    rescue Exception => e
      @raised = e
      return false unless e.is_a?(@exc)
      if @msg
        return @msg.is_a?(Regexp) ? !!(e.message =~ @msg) : (e.message == @msg)
      end
      return true
    end
  end
  # mspec's RaiseErrorMatcher#failure_message names what actually happened. Ours
  # said only "expected block to raise TypeError", which made 48 examples across
  # 25 files indistinguishable between "raised nothing" and "raised the right
  # class with a different message" — the census could not classify them.
  def failure_message
    want = "#{@exc}#{@msg ? " (#{@msg.inspect})" : ''}"
    if @raised
      "expected #{want}, but got: #{@raised.class} (#{@raised.message.inspect})"
    else
      "expected #{want}, but nothing was raised (#{@result.inspect} returned)"
    end
  end
end
def raise_error(exc = Exception, msg = nil, &b); RaiseMatcher.new(exc, msg); end
# mspec/lib/mspec/matchers/raise_error.rb:127 — raise_consistent_error IGNORES the
# message on CRuby below 4.1, because CRuby's coercion errors are inconsistent
# there (https://bugs.ruby-lang.org/issues/21864). We asserted the message anyway,
# which is why MRI 4.0.5 itself failed these examples: the strictness was ours,
# not the corpus's, and it made the examples unscoreable for either engine.
def raise_consistent_error(exc = Exception, msg = nil, opts = nil, &b)
  msg = nil if RUBY_ENGINE == "ruby" && _version_in_range(""..."4.1")
  RaiseMatcher.new(exc, msg)
end

# ---- mspec helper singletons/constants ----
module ScratchPad
  def self.record(v); @record = v; end
  def self.<<(v); (@record ||= []) << v; @record; end
  def self.recorded; @record; end
  def self.clear; @record = nil; end
  def self.inspect; "ScratchPad(#{@record.inspect})"; end
end

SPEC_TMP_BASE = "/tmp/rbgo_spec_tmp"
SPEC_TEMP_DIR = SPEC_TMP_BASE
$tmp_counter = 0
# mspec/lib/mspec/helpers/tmp.rb. Two details of the upstream contract were
# missing and both cost examples:
#   * `tmp("")` must return the temp DIRECTORY itself, not a uniquified file
#     inside it. DirSpecs.mock_dir is `File.join(tmp(""), 'dir_specs_mock')`
#     (core/dir/fixtures/common.rb), so ours pointed at .../file/dir_specs_mock.
#   * the uniquifier goes before the BASENAME, so `tmp("a/b")` stays inside the
#     directory the caller asked for instead of inventing a sibling name.
# The directory is created on demand, as upstream does, because a spec that only
# builds a path and then writes to it must not depend on load order.
def tmp(name, uniquify = true)
  Dir.mkdir(SPEC_TMP_BASE) unless File.directory?(SPEC_TMP_BASE)
  base = name.to_s
  if uniquify && !base.empty?
    slash = base.rindex("/")
    index = slash ? slash + 1 : 0
    $tmp_counter += 1
    base = base.dup
    base.insert(index, "#{$tmp_counter}-")
  end
  File.join(SPEC_TMP_BASE, base)
end
# Resolve a fixture path relative to the SPEC FILE's directory, exactly as real
# mspec does (mspec/lib/mspec/helpers/fixture.rb): strip a trailing "/shared"
# segment, and DO NOT append a second "fixtures" component when the directory is
# already the fixtures directory itself. Without the latter, helpers that pass a
# path already inside .../fixtures (e.g. IOSpecs.io_fixture, whose __FILE__ is
# core/io/fixtures/classes.rb) would resolve to .../fixtures/fixtures/<name> and
# raise Errno::ENOENT, spuriously failing every fixture-backed IO/File example.
def fixture(file, *parts)
  path = File.dirname(file)
  path = path[0..-7] if path[-7..-1] == "/shared"
  fixtures = path[-9..-1] == "/fixtures" ? "" : "fixtures"
  path = File.expand_path(path)
  File.join(path, fixtures, *parts)
end
def suppress_warning; old = $VERBOSE; $VERBOSE = nil; begin; yield; ensure; $VERBOSE = old; end; end
def suppress_keyword_warning; yield if block_given?; end
def with_timezone(name, offset = nil); old = ENV['TZ']; ENV['TZ'] = name; begin; yield; ensure; ENV['TZ'] = old; end; end

# ---- filesystem / io helpers ----
def touch(file, mode = "w")
  File.open(file, mode) { |f| yield f if block_given? }
end
def mkdir_p(path); require 'fileutils'; FileUtils.mkdir_p(path); rescue Exception; system("mkdir -p '#{path}'"); end
def rm_r(*paths); require 'fileutils'; FileUtils.rm_rf(paths); rescue Exception; paths.each { |p| system("rm -rf '#{p}'") }; end
def cp(from, to); require 'fileutils'; FileUtils.cp(from, to); rescue Exception; system("cp '#{from}' '#{to}'"); end
def mock_to_path(path)
  o = mock("to_path #{path}")
  o.should_receive(:to_path).any_number_of_times.and_return(path)
  o
end
def new_io(name, mode = "w:utf-8")
  File.open(name, mode)
end
def new_fd(name, mode = "w:utf-8")
  File.open(name, mode).fileno
end
# mspec's ARGF helper: bind @argf to a fresh ARGF reading the given files for the
# duration of the block.
def argf(argv)
  @argf = ARGF.class.new(*argv)
  begin
    yield
  ensure
    @argf = nil
  end
end
def infinity_value; 1.0/0.0; end
def nan_value; 0.0/0.0; end
def bignum_value(plus = 0); (2**64) + plus; end
def fixnum_max; 0x3fff_ffff_ffff_ffff; end
def fixnum_min; -0x4000_0000_0000_0000; end
def max_long; 0x7fff_ffff_ffff_ffff; end
def min_long; -0x8000_0000_0000_0000; end

# ---------------- should proxy ----------------
# The proxy must inherit as LITTLE as possible.
#
# `x.should.equal?(y)` is mspec's spelling of an identity assertion. If the
# proxy answers `equal?` ITSELF the assertion is never made: Object#equal?
# compares the PROXY with y, returns false, and the example passes whatever the
# two values are (go-embedded-ruby/ruby#655 -- 765 `.should.equal?` uses across
# 328 files of language/ + core/, not one of which could fail). The same holds
# for every other predicate Object already answers: eql?, nil?, is_a?,
# kind_of?, instance_of?, frozen?, respond_to?, instance_variable_defined?.
#
# Upstream mspec is immune by construction: its operator matchers subclass
# BasicObject (mspec/lib/mspec/matchers/base.rb --
# `class SpecPositiveOperatorMatcher < BasicObject`). BasicObject answers only
# ==, !=, !, equal?, __send__, __id__, instance_eval, instance_exec and
# method_missing, so mspec spells out ==, != and equal? explicitly and EVERY
# other predicate falls through #method_missing onto the real receiver, where
# mspec checks its truthiness (`SpecExpectation.fail_predicate`, see
# mspec/lib/mspec/expectations/expectations.rb).
#
# Subclassing BasicObject here reproduces that, and keeps reproducing it for
# spellings the corpus has not used yet: a predicate is broken exactly when the
# proxy already answers it, so the proxy must answer as little as possible.
class ShouldProxy < BasicObject
  def initialize(o, neg); @o = o; @neg = neg; end
  def _chk(cond, desc)
    cond = !cond if @neg
    unless cond
      ::Kernel.raise(::SpecFail, "expected #{@o.inspect} #{@neg ? 'not ' : ''}to #{desc}")
    end
    @o
  end
  def ==(x); _chk(@o == x, "== #{x.inspect}"); end
  def !=(x); _chk(@o != x, "!= #{x.inspect}"); end
  def <(x); _chk(@o < x, "< #{x.inspect}"); end
  def >(x); _chk(@o > x, "> #{x.inspect}"); end
  def <=(x); _chk(@o <= x, "<= #{x.inspect}"); end
  def >=(x); _chk(@o >= x, ">= #{x.inspect}"); end
  def =~(x); _chk((@o =~ x) ? true : false, "=~ #{x.inspect}"); end
  # `x.should !~ /re/` -- 11 sites in core/exception/full_message_spec.rb.
  # Object#!~ is defined as `!(x =~ y)`, so before this commit the spelling ran
  # the proxy's POSITIVE `=~` assertion and threw the result away: it asserted
  # the exact INVERSE of what the spec wrote. Spelling it out asserts the right
  # thing and prints the right message.
  #
  # Measured caveat: rbgo does not dispatch !~ as a method at all -- it compiles
  # `a !~ b` into `!(a =~ b)` (it inlines `!` the same way), where MRI 4.0.5
  # sends :!~ and reaches this definition. So under rbgo those 11 sites still
  # run the positive `=~` assertion and this definition is dead code until the
  # VM sends :!~. The shim cannot paper over that from here; it is an rbgo/MRI
  # divergence of its own, and it is why this line moves no number.
  def !~(x); _chk(!(@o =~ x), "!~ #{x.inspect}"); end
  # BasicObject defines ==, != and equal?, so those three -- and only those
  # three -- have to be spelled out; every other predicate reaches
  # #method_missing and is asserted there.
  def equal?(x); _chk(@o.equal?(x), "equal? #{x.inspect}"); end
  # `equal` and `eql` without the question mark are mspec's deprecated MATCHER
  # spellings (mspec/lib/mspec/matchers/equal.rb); the receiver has no such
  # method to forward to, so they stay explicit.
  def equal(x); _chk(@o.equal?(x), "equal #{x.inspect}"); end
  def eql(x); _chk(@o.eql?(x), "eql #{x.inspect}"); end
  # Every constant in this class is ::-qualified: from inside a BasicObject
  # subclass MRI does NOT reach the top-level (Object) constants, so a bare
  # `Exception` here raises NameError: uninitialized constant ShouldProxy::Exception. rbgo
  # happens to resolve it, which is exactly why the shim has to be run under
  # MRI as well -- an unqualified constant would work here and break the judge.
  def raise(*args, &blk)
    raised = nil
    begin
      @o.call
    rescue ::Exception => e
      raised = e
    end
    if @neg
      ::Kernel.raise(::SpecFail, "expected no exception, got #{raised.class}: #{raised.message}") if raised
      return nil
    end
    ::Kernel.raise(::SpecFail, "expected to raise #{args[0]}, nothing raised") if raised.nil?
    if args[0]
      unless raised.is_a?(args[0])
        ::Kernel.raise(::SpecFail, "expected #{args[0]}, got #{raised.class}: #{raised.message}")
      end
    end
    if args[1]
      m = args[1]
      ok = m.is_a?(::Regexp) ? !!(raised.message =~ m) : (raised.message == m)
      ::Kernel.raise(::SpecFail, "wrong message: got #{raised.message.inspect}, want #{m.inspect}") unless ok
    end
    # `-> { ... }.should.raise(Klass) { |e| ... }` inspects the captured exception.
    blk.call(raised) if blk
    raised
  end
  def method_missing(name, *args, &blk)
    res = @o.__send__(name, *args, &blk)
    _chk(res, "#{name}(#{args.map { |a| a.inspect }.join(', ')})")
  end
  # No #respond_to_missing? here: under BasicObject the proxy does not answer
  # #respond_to? at all, so `x.should.respond_to?(:foo)` now forwards to x --
  # which is the assertion the spec is making. Defining respond_to_missing?
  # would only re-introduce a method the proxy answers itself.
end

class Object
  MSPEC_NOMATCH = ::Object.new
  def should(matcher = MSPEC_NOMATCH)
    if matcher.equal?(MSPEC_NOMATCH)
      ShouldProxy.new(self, false)
    else
      unless matcher.matches?(self)
        ::Kernel.raise(::SpecFail, matcher.respond_to?(:failure_message) ? matcher.failure_message : "matcher failed")
      end
      self
    end
  end
  def should_not(matcher = MSPEC_NOMATCH)
    if matcher.equal?(MSPEC_NOMATCH)
      ShouldProxy.new(self, true)
    else
      if matcher.matches?(self)
        ::Kernel.raise(::SpecFail, "expected not to match")
      end
      self
    end
  end
  # mspec installs mock expectations on arbitrary receivers. Real mspec
  # (mspec/lib/mspec/mocks/mock.rb #install_method) defines the mock as a
  # SINGLETON method, saving/restoring any original at example teardown — so a
  # method the receiver really defines (e.g. #to_s, #inspect on a mock object) is
  # intercepted rather than shadowing the expectation. _mock_install performs
  # that singleton install and records it for restoration in SpecContext#run.
  def should_receive(sym)
    e = MockExpect.new(sym)
    $mock_registry << e if $mock_registry
    _mock_install(self, sym, e)
    e
  end
  def should_not_receive(sym)
    e = MockExpect.new(sym).forbid!
    _mock_install(self, sym, e)
    e
  end
  def stub!(sym)
    e = MockExpect.new(sym).any_number_of_times
    _mock_install(self, sym, e)
    e
  end
end
CODE_LOADING_DIR = (File.expand_path("fixtures/code", __dir__) rescue "fixtures/code")

# ---------------- mocks ----------------
# Install a mock expectation as a SINGLETON method on obj so it intercepts even a
# method obj really defines (mspec's Mock.install_method). The receiver's original
# method (if any) is captured ONCE per [object, sym] into $mock_installs so
# SpecContext#run can restore it at example teardown (mspec's Mock.cleanup):
# removing the override and re-binding the saved original — which correctly
# handles both inherited/class methods and a pre-existing singleton method, and
# leaves the receiver clean when there was no original.
def _mock_install(obj, sym, expect)
  mc = (class << obj; self; end)
  if $mock_installs
    # Dedup on the singleton-class identity (stable per object) so the true
    # original is captured only once even across repeated should_receive on the
    # same method. This deliberately avoids calling ANY method on obj itself
    # (object_id/hash/==) — those may themselves be mocked or forbidden by the
    # spec under test (e.g. core/kernel/case_compare's should_not_receive
    # :object_id), so touching them would corrupt the example.
    seen = false
    $mock_installs.each { |ent| seen ||= (ent[0].equal?(mc) && ent[1] == sym) }
    unless seen
      # Save the original the way mspec does — an alias on the singleton class —
      # rather than dispatching obj.method(sym), which could hit a mocked #method.
      # A trailing alias captures an inherited/class method just as well as a
      # pre-existing singleton one, so restoration works for both.
      saved = nil
      if mc.method_defined?(sym)
        saved = "__mspec_saved_#{sym}".to_sym
        begin; mc.send(:alias_method, saved, sym); rescue Exception; saved = nil; end
      end
      $mock_installs << [mc, sym, saved]
    end
  end
  mc.send(:define_method, sym) { |*a, &b| expect.invoke(*a, &b) }
  expect
end

# Map a call-count argument to an integer exactly as real mspec's MockProxy does
# (mspec/lib/mspec/mocks/proxy.rb #n_times): the symbols :once/:twice, or any
# value coercible with Integer(). Anything else (e.g. :thrice, which mspec does
# NOT define) raises, matching mspec rather than silently accepting it.
def _n_times(n)
  case n
  when :once then 1
  when :twice then 2
  else Integer(n)
  end
end

class MockExpect
  # A mock expectation carries a call-count qualifier [@qual, @limit] mirroring
  # mspec's MockProxy#count: a bare should_receive defaults to [:exactly, 1];
  # stub!/any_number_of_times relax it to [:any_number_of_times, 0]. The count is
  # verified at example teardown by Mock.verify_count (here: SpecContext#run).
  def initialize(sym)
    @sym = sym; @ret = nil; @count = 0
    @qual = :exactly; @limit = 1
    @raise = nil; @yield = nil; @forbidden = false; @has_ret = false; @multi = nil
  end
  def and_return(*v)
    @has_ret = true
    if v.size <= 1
      @ret = v[0]
    else
      # Queue of return values consumed one per call; the last one sticks once
      # the queue is exhausted (mspec MockProxy#returning). mspec also bumps the
      # expected exact count up to cover every queued value.
      @multi = v.dup; @ret = v[0]
      @limit = v.size if @qual == :exactly && @limit < v.size
    end
    self
  end
  def and_raise(e); @raise = e; self; end
  def and_yield(*a); @yield = a; self; end
  def with(*a); @with = (a.size == 1 ? a[0] : a); self; end
  def exactly(n); @qual = :exactly; @limit = _n_times(n); self; end
  def at_least(n); @qual = :at_least; @limit = _n_times(n); self; end
  def at_most(n); @qual = :at_most; @limit = _n_times(n); self; end
  def times; self; end
  def once; exactly(1); end
  def twice; exactly(2); end
  def any_number_of_times; @qual = :any_number_of_times; @limit = 0; self; end
  def never; @qual = :exactly; @limit = 0; @forbidden = true; self; end
  def forbid!; @qual = :exactly; @limit = 0; @forbidden = true; self; end
  def invoke(*a, &b)
    @count += 1
    if @forbidden
      ::Kernel.raise(::SpecFail, "mock received forbidden #{@sym}")
    end
    ::Kernel.raise(@raise) if @raise
    if @yield && b; b.call(*@yield); end
    if @multi
      i = @count - 1
      return i < @multi.size ? @multi[i] : @multi[-1]
    end
    @ret
  end
  def verify
    case @qual
    when :at_least then @count >= @limit
    when :at_most then @count <= @limit
    when :exactly then @count == @limit
    else true
    end
  end
  def desc; "#{@sym} (expected #{@qual.to_s.sub('_', ' ')} #{@limit}, got #{@count})"; end
end

# A named mock object (mspec's `mock(name)`). It inherits should_receive/
# should_not_receive/stub! from Object, so expectations install as singleton
# methods that intercept even the methods MockObject really defines (#to_s,
# #inspect) — matching mspec, where mocking #to_s on a mock returns the stubbed
# value instead of the object's own #to_s. Any unexpected (truly undefined)
# message raises NoMethodError, as a non-null mspec mock does.
class MockObject
  def initialize(name); @__name = name; end
  def method_missing(sym, *a, &b)
    ::Kernel.raise(NoMethodError, "mock '#{@__name}' got unexpected #{sym}")
  end
  def inspect; "#<mock #{@__name}>"; end
  def to_s; inspect; end
end

def mock(name, opts = {}); MockObject.new(name); end

# mspec/lib/mspec/mocks/proxy.rb: NumericMockObject IS a Numeric. The C coercion
# paths check for that before calling anything, so a plain mock made MRI itself
# raise TypeError "not a real" and lose 21 examples across core/complex/* and
# core/kernel/Complex_spec.rb. singleton_method_added must be a no-op because
# Numeric forbids singleton methods and every expectation installs one.
class NumericMockObject < Numeric
  def initialize(name, opts = {}); @__name = name; end
  def method_missing(sym, *a, &b)
    ::Kernel.raise(NoMethodError, "mock '#{@__name}' got unexpected #{sym}")
  end
  def singleton_method_added(val); end
  def inspect; "#<mock #{@__name}>"; end
end
def mock_numeric(name, opts = {}); NumericMockObject.new(name, opts); end

# mspec/lib/mspec/mocks/proxy.rb: MockIntObject defines a real #to_int, counts the
# calls and registers itself as a mock with count [:at_least, 1] — so an example
# that never converts the object FAILS. Ours installed
# `should_receive(:to_int).any_number_of_times`, which can never fail: that made
# the assertion vacuous.
class MockIntObject
  def initialize(val)
    @value = val
    @calls = 0
    $mock_registry << self if $mock_registry
  end
  attr_reader :calls
  def to_int; @calls += 1; @value.to_int; end
  def verify; @calls >= 1; end
  def desc; "to_int (expected at least 1, got #{@calls})"; end
  def inspect; "#<mock_int #{@value.inspect}>"; end
end
def mock_int(val); MockIntObject.new(val); end
def mock_to_int(val); mock_int(val); end

# ---------------- guards ----------------
# mspec's SpecGuard#run_if / #run_unless (mspec/lib/mspec/guards/guard.rb) YIELD
# when a block is given and otherwise RETURN the guard's truth value. The corpus
# relies on the second form throughout:
#
#   expected = ruby_version_is("3.4") ? "{a: 1}" : "{:a=>1}"   core/hash/inspect_spec.rb
#
# and mspec's own raise_consistent_error asks `ruby_version_is ""..."4.1"` that
# way too. Our guards returned nil with no block, so every such ternary silently
# picked the OLDER branch — wording that neither MRI 4.0.5 nor rbgo produces, so
# the example could not be scored by either engine. Route every guard through
# this helper so the no-block form answers.
def _guard_run(match)
  return match unless block_given?
  yield if match
  nil
end

def ruby_version_is(range, &b); _guard_run(_version_in_range(range), &b); end
# mspec's version_is / kernel_version_is (mspec/lib/mspec/guards/version.rb)
# compare an arbitrary base version against the requirement, not RUBY_VERSION.
def version_is(base, range, &b); _guard_run(_version_in_range(range, base.to_s), &b); end
def kernel_version_is(range, &b); _guard_run(_version_in_range(range, PlatformGuard.kernel_version), &b); end
def ruby_bug(*a, &b); _guard_run(true, &b); end   # assume bug fixed
def platform_is(*args, &b)
  opts = args.last.is_a?(Hash) ? args.pop : {}
  match = args.empty? ? true : args.any? { |s| PLATFORMS.include?(s) }
  match &&= (opts[:wordsize].nil? || opts[:wordsize] == WORDSIZE)
  match &&= (opts[:pointer_size].nil? || opts[:pointer_size] == WORDSIZE / 8)
  # c_long_size guards examples written for a platform whose C long is narrower
  # than this one. Ignoring it ran a 32-bit-only example here:
  # "abc" * ((2 ** 31) - 1) is a legal six-gigabyte string on a 64-bit platform,
  # and building it took 6.5 GB out of a CI runner's sixteen.
  match &&= (opts[:c_long_size].nil? || opts[:c_long_size] == WORDSIZE)
  _guard_run(match, &b)
end
def platform_is_not(*args, &b)
  opts = args.last.is_a?(Hash) ? args.pop : {}
  match = args.any? { |s| PLATFORMS.include?(s) }
  match ||= (!opts[:wordsize].nil? && opts[:wordsize] != WORDSIZE)
  match ||= (!opts[:c_long_size].nil? && opts[:c_long_size] != WORDSIZE)
  _guard_run(!match, &b)
end
def not_supported_on(*engines, &b); _guard_run(!engines.include?(:ruby), &b); end
def not_compliant_on(*engines, &b); _guard_run(!engines.include?(:ruby), &b); end
def compliant_on(*engines, &b); _guard_run(engines.include?(:ruby), &b); end
def deviates_on(*engines, &b); _guard_run(false, &b); end
def conflicts_with(*consts, &b); _guard_run(true, &b); end
def guard(cond = nil); run = cond.respond_to?(:call) ? cond.call : !!cond; yield if run && block_given?; end
def guard_not(cond = nil); run = cond.respond_to?(:call) ? cond.call : !!cond; yield if !run && block_given?; end
def quarantine!(*a); end
def big_endian(&b); _guard_run(ENDIAN == :big, &b); end
def little_endian(&b); _guard_run(ENDIAN == :little, &b); end
# mspec/lib/mspec/guards/superuser.rb: as_user is `run_unless` the effective uid
# is root; as_superuser and as_real_superuser are `run_if` on euid/uid == 0.
def as_user(&b); _guard_run(Process.euid != 0, &b); end
def as_superuser(&b); _guard_run(Process.euid == 0, &b); end
def as_real_superuser(&b); _guard_run(Process.uid == 0, &b); end
# mspec/lib/mspec/guards/block_device.rb
def with_block_device(&b)
  $__have_block_device = !`find /dev /devices -type b 2> /dev/null`.to_s.empty? if $__have_block_device.nil?
  _guard_run($__have_block_device, &b)
end
def with_feature(*a, &b); _guard_run(true, &b); end
def without_feature(*a, &b); _guard_run(false, &b); end
def ruby_exe(*a, **k); ::Kernel.raise(SpecSkip, "ruby_exe subprocess unsupported"); end
def ruby_cmd(*a, **k); ::Kernel.raise(SpecSkip, "ruby_cmd unsupported"); end

# ---------------- example runner ----------------
class SpecContext
  attr_reader :desc, :examples, :before_each, :after_each, :before_all, :after_all
  def initialize(desc, parent)
    @desc = desc
    @parent = parent
    @examples = []
    @before_each = parent ? parent.before_each.dup : []
    @after_each = parent ? parent.after_each.dup : []
    # Inherit ancestor `before :all` blocks so their instance variables are set on
    # THIS context object too. Each shim context is a distinct object that its
    # examples instance_eval against, and a nested describe (or an it_behaves_like
    # shared context) runs its examples on its own object — so without inheriting
    # the enclosing describe's before(:all) (e.g. numeric/step's `@step = ->...`),
    # those ivars would be nil in the nested examples. Replaying an ivar-setup
    # before(:all) per descendant is idempotent; mspec likewise makes before(:all)
    # state visible to nested groups.
    @before_all = parent ? parent.before_all.dup : []
    # `after :all` was DROPPED entirely, and the corpus's Dir specs are built on
    # it: core/dir/shared/glob.rb chdir's into the fixture tree in `before :all`
    # and chdir's BACK in `after :all`. With the restore missing, the first
    # describe left the process inside a directory the next describe's
    # `DirSpecs.create_mock_dirs` then deleted, so `Dir.pwd` raised
    # "Errno::ENOENT - getcwd" for every remaining example: 81 in
    # core/dir/glob_spec.rb and 48 in core/dir/element_reference_spec.rb, for MRI
    # as much as for rbgo. mspec's ContextState#post(:all) is inherited from the
    # parents in REVERSE order (mspec/lib/mspec/runner/context.rb).
    @after_all = parent ? parent.after_all.dup : []
  end
  def it(d, &blk); @examples << [d, blk]; end
  def specify(d = nil, &blk); @examples << [d, blk]; end
  def before(scope = :each, &blk)
    if scope == :all; @before_all << blk; else; @before_each << blk; end
  end
  def after(scope = :each, &blk)
    if scope == :all; @after_all.unshift(blk); else; @after_each << blk; end
  end
  def describe(d, *a, &blk)
    child = SpecContext.new("#{@desc} #{d}", self)
    # Carry helpers written with `def` in THIS block down to the child. The block
    # is instance_eval'd, so a `def` lands on this object's singleton, and a
    # nested describe runs on a different object — which is how
    # core/string/valid_encoding/utf_8_spec lost all 28 of its examples: they call
    # an outer `def utf8`. Forward each EXISTING helper explicitly rather than
    # adding a method_missing, because a genuinely missing method must still raise
    # from the caller with no shim frame in the backtrace — language/send_spec
    # asserts precisely that, and a method_missing here broke it.
    parent = self
    sc = singleton_class
    (sc.instance_methods(false) + sc.private_instance_methods(false)).each do |m|
      child.define_singleton_method(m) { |*ar, &bl| parent.send(m, *ar, &bl) }
    end
    $ctx_stack.push(child)
    begin
      child.instance_eval(&blk) if blk
    rescue Exception => e
      _record_load_error(child.desc, e)
    ensure
      $ctx_stack.pop
    end
    child.run
  end
  alias_method :context, :describe

  def run
    # A before(:all)/after(:all) that raises is still swallowed — mspec would skip
    # the whole group instead — but it is now RECORDED, because an invisible setup
    # failure is what let the missing `after :all` above go unnoticed for 36 waves.
    @before_all.each do |b|
      begin
        instance_eval(&b)
      rescue Exception => e
        $RB_FAILS << ["hookerror", @desc, "(before :all)", e.class.to_s, e.message.to_s[0, 200]]
      end
    end
    @examples.each do |d, blk|
      if blk.nil?
        $RB_SKIP += 1
        next
      end
      $mock_registry = []
      $mock_installs = []
      begin
        @before_each.each { |b| instance_eval(&b) }
        instance_eval(&blk)
        # verify mocks
        bad = $mock_registry.reject { |m| m.verify }
        if bad.empty?
          $RB_PASS += 1
        else
          $RB_FAIL += 1
          $RB_FAILS << ["fail", @desc, d, "MockNotSatisfied", bad.map { |m| m.desc }.join('; ')]
        end
      rescue SpecFail => e
        $RB_FAIL += 1
        $RB_FAILS << ["fail", @desc, d, "SpecFail", e.message.to_s[0, 200]]
      rescue SpecSkip => e
        $RB_SKIP += 1
        # Skips are invisible to the ratchet too: they can never be scored by
        # either engine, so the census needs to see what they cost and why.
        $RB_FAILS << ["skip", @desc, d, e.class.to_s, e.message.to_s[0, 200]]
      rescue Exception => e
        $RB_ERROR += 1
        $RB_FAILS << ["error", @desc, d, e.class.to_s, e.message.to_s[0, 200]]
      ensure
        @after_each.each { |b| begin; instance_eval(&b); rescue Exception; end }
        # Restore any receiver whose real method a mock intercepted (mspec's
        # Mock.cleanup): drop the singleton override and alias the saved original
        # back (or leave it removed when the receiver had no original method).
        $mock_installs.each do |mc, sym, saved|
          begin
            mc.send(:remove_method, sym)
            if saved
              mc.send(:alias_method, sym, saved)
              mc.send(:remove_method, saved)
            end
          rescue Exception
          end
        end
        $mock_registry = nil
        $mock_installs = nil
      end
    end
    @after_all.each do |b|
      begin
        instance_eval(&b)
      rescue Exception => e
        $RB_FAILS << ["hookerror", @desc, "(after :all)", e.class.to_s, e.message.to_s[0, 200]]
      end
    end
  end
end

$ctx_stack = []

def _record_load_error(desc, e)
  $RB_ERROR += 1
  $RB_FAILS << ["loaderror", desc, "(collection)", e.class.to_s, e.message.to_s[0, 200]]
end

$shared = {}

def describe(d, *a, &blk)
  opts = a.last.is_a?(Hash) ? a.last : {}
  if opts[:shared]
    $shared[d] = blk
    return
  end
  root = SpecContext.new(d.to_s, nil)
  $ctx_stack.push(root)
  begin
    root.instance_eval(&blk) if blk
  rescue Exception => e
    _record_load_error(root.desc, e)
  ensure
    $ctx_stack.pop
  end
  root.run
end
def context(d, *a, &blk); describe(d, *a, &blk); end

def it_behaves_like(desc, meth = nil, obj = nil)
  blk = $shared[desc]
  parent = $ctx_stack.last
  if blk.nil?
    $RB_ERROR += 1
    $RB_FAILS << ["error", "shared #{desc}", "(missing)", "SharedNotFound", "no shared spec :#{desc}"]
    return
  end
  ctx = SpecContext.new("shared #{desc}", parent)
  # meth/obj are optional: a nested `it_should_behave_like :name` (no args) runs a
  # shared block INSIDE another and must inherit @method/@object from the enclosing
  # context's before hooks — setting them here (to nil) would clobber the inherited
  # values. Only install the re-assert when they are actually provided.
  if meth || obj
    ctx.instance_variable_set(:@method, meth)
    ctx.instance_variable_set(:@object, obj)
    ctx.before(:each) do
      @method = meth if meth
      @object = obj if obj
    end
  end
  $ctx_stack.push(ctx)
  begin
    ctx.instance_eval(&blk)
  rescue Exception => e
    _record_load_error(ctx.desc, e)
  ensure
    $ctx_stack.pop
  end
  ctx.run
end
def it_should_behave_like(desc, meth = nil, obj = nil); it_behaves_like(desc, meth, obj); end

# mspec/lib/mspec/runner/evaluate.rb: `evaluate <<-ruby ... ruby do ... end`
# defines ONE example that first evaluates the Ruby source against the evaluator
# and then runs the assertion block against the same object, so a method or
# constant the source defines is visible to the assertions. Its absence was a
# LOAD error (uninitialized constant SpecEvaluate), which cost whole describe
# blocks: 7 collections across core/{method,proc,unboundmethod}/arity_spec.rb and
# language/{lambda,method}_spec.rb.
class SpecEvaluate
  def self.desc=(d); @desc = d; end
  def self.desc; @desc ||= "evaluates "; end

  def initialize(ruby, desc)
    @ruby = ruby.rstrip
    @desc = desc || self.class.desc
  end

  def format(ruby)
    if ruby.include?("\n")
      lines = ruby.each_line.to_a
      if /( *)/ =~ lines.first
        if $1.size > 4
          dedent = $1.size - 4
          ruby = lines.map { |l| l[dedent..-1] }.join
        else
          indent = " " * (4 - $1.size)
          ruby = lines.map { |l| "#{indent}#{l}" }.join
        end
      end
      "\n#{ruby}"
    else
      "'#{ruby.lstrip}'"
    end
  end

  def define(ctx, &block)
    ruby = @ruby
    evaluator = self
    ctx.specify("#{@desc} #{format ruby}") do
      evaluator.instance_eval(ruby)
      evaluator.instance_eval(&block)
    end
  end
end

def evaluate(str, desc = nil, &block)
  ctx = $ctx_stack.last
  ::Kernel.raise("evaluate outside a describe block") if ctx.nil?
  SpecEvaluate.new(str, desc).define(ctx, &block)
end

# some specs call these at top level
def before(*a); end
def after(*a); end

at_exit do
  $stdout.puts "RBGO_RESULT pass=#{$RB_PASS} fail=#{$RB_FAIL} error=#{$RB_ERROR} skip=#{$RB_SKIP}"
  $RB_FAILS.each do |kind, dsc, itn, cls, msg|
    $stdout.puts "RBGO_DETAIL\t#{kind}\t#{cls}\t#{(dsc.to_s + ' | ' + itn.to_s).gsub(/\s+/, ' ')[0, 160]}\t#{msg.to_s.gsub(/\s+/, ' ')[0, 160]}"
  end
end
