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
def be_computed_by(*a, &b); raise SpecSkip, "be_computed_by unsupported"; end
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
def fixture(file, *parts)
  File.join(File.dirname(file), "fixtures", *parts)
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
  def raise(*args)
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
  # mspec installs mock expectations on arbitrary receivers
  def should_receive(sym)
    e = MockExpect.new(sym)
    $mock_registry << e if $mock_registry
    mc = (class << self; self; end)
    mc.send(:define_method, sym) { |*a, &b| e.invoke(*a, &b) }
    e
  end
  def should_not_receive(sym)
    e = MockExpect.new(sym).forbid!
    mc = (class << self; self; end)
    mc.send(:define_method, sym) { |*a, &b| e.invoke(*a, &b) }
    e
  end
  def stub!(sym)
    e = MockExpect.new(sym).any_number_of_times
    mc = (class << self; self; end)
    mc.send(:define_method, sym) { |*a, &b| e.invoke(*a, &b) }
    e
  end
end
CODE_LOADING_DIR = (File.expand_path("fixtures/code", __dir__) rescue "fixtures/code")

# ---------------- mocks ----------------
class MockExpect
  def initialize(sym); @sym = sym; @ret = nil; @count = 0; @min = 1; @exact = nil; @raise = nil; @yield = nil; @forbidden = false; @has_ret = false; end
  def and_return(*v); @has_ret = true; @ret = v.size == 1 ? v[0] : v; @multi = v.size > 1 ? v.dup : nil; self; end
  def and_raise(e); @raise = e; self; end
  def and_yield(*a); @yield = a; self; end
  def with(*a); @with = a; self; end
  def exactly(n); @min = n; @exact = n; self; end
  def times; self; end
  def at_least(n); @min = (n == :once ? 1 : n); self; end
  def at_most(n); @max = n; self; end
  def any_number_of_times; @min = 0; self; end
  def once; @min = 1; @exact = 1; self; end
  def twice; @min = 2; @exact = 2; self; end
  def never; @min = 0; @exact = 0; @forbidden = true; self; end
  def forbid!; @forbidden = true; self; end
  def invoke(*a, &b)
    @count += 1
    if @forbidden
      ::Kernel.raise(SpecFail, "mock received forbidden #{@sym}")
    end
    ::Kernel.raise(@raise) if @raise
    if @yield && b; b.call(*@yield); end
    if @multi && @count <= @multi.size; return @multi[@count - 1]; end
    @ret
  end
  def verify
    return false if @exact && @count != @exact
    @count >= @min
  end
  def desc; "#{@sym} (got #{@count}, need #{@exact || @min}+)"; end
end

class MockObject
  def initialize(name); @__name = name; @__exp = {}; end
  def should_receive(sym)
    e = MockExpect.new(sym)
    @__exp[sym] = e
    $mock_registry << e if $mock_registry
    e
  end
  def should_not_receive(sym)
    e = MockExpect.new(sym).forbid!
    @__exp[sym] = e
    e
  end
  def stub!(sym); e = MockExpect.new(sym).any_number_of_times; @__exp[sym] = e; e; end
  def method_missing(sym, *a, &b)
    e = @__exp[sym]
    if e
      e.invoke(*a, &b)
    else
      ::Kernel.raise(NoMethodError, "mock '#{@__name}' got unexpected #{sym}")
    end
  end
  def respond_to_missing?(sym, *); @__exp.key?(sym); end
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
        $mock_registry = nil
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
