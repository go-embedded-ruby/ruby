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

def _version_in_range(range)
  cur = CUR_VERSION
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

class ComplainMatcher
  def initialize(*a); end
  def matches?(actual); actual.call if actual.respond_to?(:call); true; end
  def failure_message; "complain"; end
end

TOLERANCE = 0.00003
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
def complain(*a); raise SpecSkip, "complain matcher unsupported"; end
def output(*a); raise SpecSkip, "output matcher unsupported"; end
def output_to_fd(*a); raise SpecSkip, "output_to_fd matcher unsupported"; end

class RaiseMatcher
  def initialize(exc, msg); @exc = exc || Exception; @msg = msg; end
  def matches?(callable)
    begin
      callable.call
      return false
    rescue Exception => e
      return false unless e.is_a?(@exc)
      if @msg
        return @msg.is_a?(Regexp) ? !!(e.message =~ @msg) : (e.message == @msg)
      end
      return true
    end
  end
  def failure_message; "expected block to raise #{@exc}"; end
end
def raise_error(exc = Exception, msg = nil, &b); RaiseMatcher.new(exc, msg); end
def raise_consistent_error(exc, msg = nil, **k); RaiseMatcher.new(exc, msg); end

# ---- mspec helper singletons/constants ----
module ScratchPad
  def self.record(v); @record = v; end
  def self.<<(v); (@record ||= []) << v; @record; end
  def self.recorded; @record; end
  def self.clear; @record = nil; end
  def self.inspect; "ScratchPad(#{@record.inspect})"; end
end

SPEC_TMP_BASE = "/tmp/rbgo_spec_tmp"
Dir.mkdir(SPEC_TMP_BASE) rescue nil
$tmp_counter = 0
def tmp(name, uniquify = true)
  base = name.to_s
  base = "file" if base.empty?
  if uniquify
    $tmp_counter += 1
    base = base + "-#{$tmp_counter}-#{rand(1000000)}"
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
def infinity_value; 1.0/0.0; end
def nan_value; 0.0/0.0; end
def bignum_value(plus = 0); 0x8000_0000_0000_0000 + plus; end
def fixnum_max; 0x3fff_ffff_ffff_ffff; end
def fixnum_min; -0x4000_0000_0000_0000; end
def max_long; 0x7fff_ffff_ffff_ffff; end
def min_long; -0x8000_0000_0000_0000; end

# ---------------- should proxy ----------------
class ShouldProxy
  def initialize(o, neg); @o = o; @neg = neg; end
  def _chk(cond, desc)
    cond = !cond if @neg
    unless cond
      ::Kernel.raise(SpecFail, "expected #{@o.inspect} #{@neg ? 'not ' : ''}to #{desc}")
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
  def equal(x); _chk(@o.equal?(x), "equal #{x.inspect}"); end
  def eql(x); _chk(@o.eql?(x), "eql #{x.inspect}"); end
  def raise(*args, &blk)
    raised = nil
    begin
      @o.call
    rescue Exception => e
      raised = e
    end
    if @neg
      ::Kernel.raise(SpecFail, "expected no exception, got #{raised.class}: #{raised.message}") if raised
      return nil
    end
    ::Kernel.raise(SpecFail, "expected to raise #{args[0]}, nothing raised") if raised.nil?
    if args[0]
      unless raised.is_a?(args[0])
        ::Kernel.raise(SpecFail, "expected #{args[0]}, got #{raised.class}: #{raised.message}")
      end
    end
    if args[1]
      m = args[1]
      ok = m.is_a?(Regexp) ? !!(raised.message =~ m) : (raised.message == m)
      ::Kernel.raise(SpecFail, "wrong message: got #{raised.message.inspect}, want #{m.inspect}") unless ok
    end
    # `-> { ... }.should.raise(Klass) { |e| ... }` inspects the captured exception.
    blk.call(raised) if blk
    raised
  end
  def method_missing(name, *args, &blk)
    res = @o.__send__(name, *args, &blk)
    _chk(res, "#{name}(#{args.map { |a| a.inspect }.join(', ')})")
  end
  def respond_to_missing?(*); true; end
end

class Object
  MSPEC_NOMATCH = ::Object.new
  def should(matcher = MSPEC_NOMATCH)
    if matcher.equal?(MSPEC_NOMATCH)
      ShouldProxy.new(self, false)
    else
      unless matcher.matches?(self)
        ::Kernel.raise(SpecFail, matcher.respond_to?(:failure_message) ? matcher.failure_message : "matcher failed")
      end
      self
    end
  end
  def should_not(matcher = MSPEC_NOMATCH)
    if matcher.equal?(MSPEC_NOMATCH)
      ShouldProxy.new(self, true)
    else
      if matcher.matches?(self)
        ::Kernel.raise(SpecFail, "expected not to match")
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
      ::Kernel.raise(SpecFail, "mock received forbidden #{@sym}")
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
def mock_int(val)
  o = mock("fixnum #{val}")
  o.should_receive(:to_int).any_number_of_times.and_return(val)
  o
end
def mock_numeric(name, &b); o = mock(name); b.call(o) if b; o; end
def mock_to_int(val); mock_int(val); end

# ---------------- guards ----------------
def ruby_version_is(range); yield if _version_in_range(range) && block_given?; end
def ruby_bug(*a); yield if block_given?; end   # assume bug fixed
def platform_is(*args)
  opts = args.last.is_a?(Hash) ? args.pop : {}
  match = args.empty? ? true : args.any? { |s| PLATFORMS.include?(s) }
  match &&= (opts[:wordsize].nil? || opts[:wordsize] == WORDSIZE)
  match &&= (opts[:pointer_size].nil? || opts[:pointer_size] == WORDSIZE / 8)
  yield if match && block_given?
end
def platform_is_not(*args)
  opts = args.last.is_a?(Hash) ? args.pop : {}
  match = args.any? { |s| PLATFORMS.include?(s) }
  match ||= (!opts[:wordsize].nil? && opts[:wordsize] != WORDSIZE)
  yield if !match && block_given?
end
def not_supported_on(*engines); yield if !engines.include?(:ruby) && block_given?; end
def not_compliant_on(*engines); yield if !engines.include?(:ruby) && block_given?; end
def compliant_on(*engines); yield if engines.include?(:ruby) && block_given?; end
def deviates_on(*engines); yield if false; end
def conflicts_with(*consts); yield if block_given?; end
def guard(cond = nil); run = cond.respond_to?(:call) ? cond.call : !!cond; yield if run && block_given?; end
def guard_not(cond = nil); run = cond.respond_to?(:call) ? cond.call : !!cond; yield if !run && block_given?; end
def quarantine!(*a); end
def big_endian; yield if ENDIAN == :big && block_given?; end
def little_endian; yield if ENDIAN == :little && block_given?; end
def as_user; yield if block_given?; end
def as_superuser; end   # not root
def with_feature(*a); yield if block_given?; end
def without_feature(*a); end
def ruby_exe(*a, **k); ::Kernel.raise(SpecSkip, "ruby_exe subprocess unsupported"); end
def ruby_cmd(*a, **k); ::Kernel.raise(SpecSkip, "ruby_cmd unsupported"); end

# ---------------- example runner ----------------
class SpecContext
  attr_reader :desc, :examples, :before_each, :after_each
  def initialize(desc, parent)
    @desc = desc
    @examples = []
    @before_each = parent ? parent.before_each.dup : []
    @after_each = parent ? parent.after_each.dup : []
    @before_all = []
  end
  def it(d, &blk); @examples << [d, blk]; end
  def specify(d = nil, &blk); @examples << [d, blk]; end
  def before(scope = :each, &blk)
    if scope == :all; @before_all << blk; else; @before_each << blk; end
  end
  def after(scope = :each, &blk); @after_each << blk if scope == :each; end
  def describe(d, *a, &blk)
    child = SpecContext.new("#{@desc} #{d}", self)
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
    @before_all.each { |b| begin; instance_eval(&b); rescue Exception; end }
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

def it_behaves_like(desc, meth, obj = nil)
  blk = $shared[desc]
  parent = $ctx_stack.last
  if blk.nil?
    $RB_ERROR += 1
    $RB_FAILS << ["error", "shared #{desc}", "(missing)", "SharedNotFound", "no shared spec :#{desc}"]
    return
  end
  ctx = SpecContext.new("shared #{desc}", parent)
  ctx.instance_variable_set(:@method, meth)
  ctx.instance_variable_set(:@object, obj)
  # re-assert @method/@object before each example (mspec sets via before)
  ctx.before(:each) { @method = meth; @object = obj }
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
def it_should_behave_like(desc, meth, obj = nil); it_behaves_like(desc, meth, obj); end

# some specs call these at top level
def before(*a); end
def after(*a); end

at_exit do
  $stdout.puts "RBGO_RESULT pass=#{$RB_PASS} fail=#{$RB_FAIL} error=#{$RB_ERROR} skip=#{$RB_SKIP}"
  $RB_FAILS.each do |kind, dsc, itn, cls, msg|
    $stdout.puts "RBGO_DETAIL\t#{kind}\t#{cls}\t#{(dsc.to_s + ' | ' + itn.to_s).gsub(/\s+/, ' ')[0, 160]}\t#{msg.to_s.gsub(/\s+/, ' ')[0, 160]}"
  end
end
