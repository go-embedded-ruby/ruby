# frozen_string_literal: true
#
# The embedded-Ruby prelude: standard library pieces that are cleaner to express
# in Ruby than in Go. Loaded once by VM.New after the native bootstrap, so every
# program sees these modules. This is the org's USP — Comparable and Enumerable
# are written *once*, in Ruby, on top of a single primitive each (`<=>` / `each`).

# Comparable derives the ordering operators from `<=>`. A class mixes it in and
# defines `<=>`; everything else follows.
module Comparable
  def <(other)
    (self <=> other) < 0
  end

  def <=(other)
    (self <=> other) <= 0
  end

  def >(other)
    (self <=> other) > 0
  end

  def >=(other)
    (self <=> other) >= 0
  end

  def ==(other)
    (self <=> other) == 0
  end

  def between?(min, max)
    if self < min
      false
    elsif self > max
      false
    else
      true
    end
  end

  def clamp(min, max = nil)
    if min.is_a?(Range)
      raise ArgumentError, "cannot clamp with an exclusive range" if min.exclude_end?
      lo = min.begin
      hi = min.end
      return lo if !lo.nil? && self < lo
      return hi if !hi.nil? && self > hi
      self
    else
      return min if !min.nil? && self < min
      return max if !max.nil? && self > max
      self
    end
  end
end

# Enumerable derives the collection methods from `each`. A class mixes it in and
# defines `each`; map/select/reduce/min/… all follow. (Without break/&& yet, the
# scanning forms below visit every element — correct, if not short-circuiting.)
module Enumerable
  # __each_packed iterates #each, packing each yield into a single value: a lone
  # value stays scalar, but a multi-value yield (e.g. each_with_index's element +
  # index) becomes an Array. Every Enumerable method iterates through this, so a
  # multi-parameter block downstream (`map { |x, i| }`) auto-splats the packed
  # Array exactly as MRI does — without each method handling arity itself.
  def __each_packed
    each { |*a| yield(a.size == 1 ? a[0] : a) }
  end

  def to_a
    r = []
    __each_packed { |x| r << x }
    r
  end

  # to_h: each element (or each yield of the block) must be a [key, value] pair.
  def to_h
    h = {}
    __each_packed { |x|
      pair = block_given? ? yield(x) : x
      raise TypeError, "wrong element type #{pair.class} (expected array)" unless pair.is_a?(Array)
      raise ArgumentError, "element has wrong array length (expected 2, was #{pair.length})" unless pair.length == 2
      h[pair[0]] = pair[1]
    }
    h
  end

  def map
    return enum_for(:map) unless block_given?
    r = []
    __each_packed { |x| r << yield(x) }
    r
  end

  # collect/filter/detect are the classic aliases of map/select/find.
  def collect(&blk)
    return enum_for(:collect) unless block_given?
    map(&blk)
  end

  def filter(&blk)
    return enum_for(:filter) unless block_given?
    select(&blk)
  end

  def detect(&blk)
    return enum_for(:detect) unless block_given?
    find(&blk)
  end

  def count(*args)
    n = 0
    if args.empty?
      __each_packed { |x| n = n + 1 if !block_given? || yield(x) }
    else
      item = args[0]
      __each_packed { |x| n = n + 1 if x == item }
    end
    n
  end

  # min_by / max_by / sort_by delegate to Array's native implementations via the
  # pair/element list, so any Enumerable (Hash, Range, Struct, …) gains them.
  def min_by
    return enum_for(:min_by) unless block_given?
    to_a.min_by { |x| yield(x) }
  end

  def max_by
    return enum_for(:max_by) unless block_given?
    to_a.max_by { |x| yield(x) }
  end

  def sort_by
    return enum_for(:sort_by) unless block_given?
    to_a.sort_by { |x| yield(x) }
  end

  def select
    return enum_for(:select) unless block_given?
    r = []
    __each_packed { |x| r << x if yield(x) }
    r
  end

  def reject
    return enum_for(:reject) unless block_given?
    r = []
    __each_packed { |x| r << x unless yield(x) }
    r
  end

  def find
    return enum_for(:find) unless block_given?
    result = nil
    __each_packed { |x|
      if result == nil
        result = x if yield(x)
      end
    }
    result
  end

  def include?(value)
    found = false
    __each_packed { |x| found = true if x == value }
    found
  end

  def sum(init = 0)
    total = init
    __each_packed { |x| total = total + (block_given? ? yield(x) : x) }
    total
  end

  def min(n = nil)
    return to_a.sort.first(n) unless n.nil? # min(n): the n smallest, ascending
    result = nil
    first = true
    __each_packed { |x|
      if first
        result = x
        first = false
      elsif x < result
        result = x
      end
    }
    result
  end

  def max(n = nil)
    return to_a.sort.last(n).reverse unless n.nil? # max(n): the n largest, descending
    result = nil
    first = true
    __each_packed { |x|
      if first
        result = x
        first = false
      elsif x > result
        result = x
      end
    }
    result
  end

  def minmax
    [min, max]
  end

  def reduce(*args)
    # Forms: reduce { |a, b| }, reduce(init) { }, reduce(:op), reduce(init, :op).
    sym = nil
    has_init = false
    init = nil
    if args.length == 2
      init = args[0]
      sym = args[1]
      has_init = true
    elsif args.length == 1 && args[0].is_a?(Symbol)
      sym = args[0]
    elsif args.length == 1
      init = args[0]
      has_init = true
    end
    acc = init
    started = has_init
    __each_packed do |x|
      if !started
        acc = x
        started = true
      elsif sym
        acc = acc.send(sym, x)
      else
        acc = yield(acc, x)
      end
    end
    acc
  end

  def inject(*args, &blk)
    reduce(*args, &blk)
  end

  # any?/all?/none? follow MRI's three forms: with a pattern argument each element
  # is tested with `pattern === x`; with a block the block result is used; with
  # neither the element's own truthiness is used. The default-argument side effect
  # records whether a pattern was actually passed.
  def any?(pattern = (no_pat = true; nil))
    blk = block_given?
    result = false
    __each_packed do |x|
      truth = no_pat ? (blk ? yield(x) : x) : (pattern === x)
      result = true if truth
    end
    result
  end

  def all?(pattern = (no_pat = true; nil))
    blk = block_given?
    result = true
    __each_packed do |x|
      truth = no_pat ? (blk ? yield(x) : x) : (pattern === x)
      result = false unless truth
    end
    result
  end

  def none?(pattern = (no_pat = true; nil))
    blk = block_given?
    result = true
    __each_packed do |x|
      truth = no_pat ? (blk ? yield(x) : x) : (pattern === x)
      result = false if truth
    end
    result
  end

  def each_with_index
    return enum_for(:each_with_index) unless block_given?
    i = 0
    __each_packed { |x|
      yield(x, i)
      i = i + 1
    }
    self
  end

  def flat_map
    return enum_for(:flat_map) unless block_given?
    r = []
    __each_packed { |x|
      v = yield(x)
      if v.is_a?(Array)
        v.each { |e| r << e }
      else
        r << v
      end
    }
    r
  end

  def each_with_object(memo)
    return enum_for(:each_with_object, memo) unless block_given?
    __each_packed { |x| yield(x, memo) }
    memo
  end

  def filter_map
    return enum_for(:filter_map) unless block_given?
    r = []
    __each_packed { |x|
      v = yield(x)
      r << v if v
    }
    r
  end

  def partition
    return enum_for(:partition) unless block_given?
    yes = []
    no = []
    __each_packed { |x|
      if yield(x)
        yes << x
      else
        no << x
      end
    }
    [yes, no]
  end

  def group_by
    return enum_for(:group_by) unless block_given?
    h = {}
    __each_packed { |x|
      k = yield(x)
      (h[k] ||= []) << x
    }
    h
  end

  def tally
    h = {}
    __each_packed { |x|
      h[x] = (h[x] || 0) + 1
    }
    h
  end

  def zip(*others)
    r = []
    i = 0
    __each_packed { |x|
      row = [x]
      others.each { |o| row << o[i] }
      r << row
      i = i + 1
    }
    r
  end

  # find_index(value) / find_index { |x| } — the index of the first match, or nil.
  def find_index(*args)
    idx = nil
    i = 0
    __each_packed { |x|
      idx = i if idx.nil? && (args.empty? ? yield(x) : x == args[0])
      i = i + 1
    }
    idx
  end

  def find_all(&blk)
    return enum_for(:find_all) unless block_given?
    select(&blk)
  end

  # grep selects the elements that pattern === matches (and maps them through the
  # block, if given); grep_v keeps the non-matching ones.
  def grep(pattern)
    result = []
    __each_packed { |x| result << (block_given? ? yield(x) : x) if pattern === x }
    result
  end

  def grep_v(pattern)
    result = []
    __each_packed { |x| result << (block_given? ? yield(x) : x) unless pattern === x }
    result
  end

  def take_while
    return enum_for(:take_while) unless block_given?
    r = []
    taking = true
    __each_packed { |x|
      taking = false if taking && !yield(x)
      r << x if taking
    }
    r
  end

  def drop_while
    return enum_for(:drop_while) unless block_given?
    r = []
    dropping = true
    __each_packed { |x|
      dropping = false if dropping && !yield(x)
      r << x unless dropping
    }
    r
  end

  def each_slice(n)
    return enum_for(:each_slice, n) unless block_given?
    a = to_a
    i = 0
    while i < a.length
      yield(a[i, n])
      i = i + n
    end
    nil
  end

  def each_cons(n)
    return enum_for(:each_cons, n) unless block_given?
    a = to_a
    i = 0
    while i + n <= a.length
      yield(a[i, n])
      i = i + 1
    end
    nil
  end

  # chunk_while / slice_when split the element stream into runs at each pair where
  # the block (does not) hold. They return the Array of runs (MRI returns a lazy
  # Enumerator; the materialised values match).
  def chunk_while
    a = to_a
    return [] if a.empty?
    chunks = []
    cur = [a[0]]
    i = 1
    while i < a.length
      if yield(a[i - 1], a[i])
        cur << a[i]
      else
        chunks << cur
        cur = [a[i]]
      end
      i = i + 1
    end
    chunks << cur
    chunks
  end

  def slice_when
    a = to_a
    return [] if a.empty?
    chunks = []
    cur = [a[0]]
    i = 1
    while i < a.length
      if yield(a[i - 1], a[i])
        chunks << cur
        cur = [a[i]]
      else
        cur << a[i]
      end
      i = i + 1
    end
    chunks << cur
    chunks
  end

  # chunk groups consecutive elements sharing the block's value into
  # [value, [elements...]] runs.
  def chunk
    result = []
    __each_packed { |x|
      k = yield(x)
      if result.empty? || result[-1][0] != k
        result << [k, [x]]
      else
        result[-1][1] << x
      end
    }
    result
  end

  def minmax_by
    [min_by { |x| yield(x) }, max_by { |x| yield(x) }]
  end

  # cycle(n) yields every element n times (forever when n is nil — use break to
  # stop). With no block it returns an Enumerator (finite only when n is given).
  def cycle(n = nil)
    return enum_for(:cycle, n) unless block_given?
    a = to_a
    return nil if a.empty?
    if n.nil?
      loop { a.each { |x| yield(x) } }
    else
      n.times { a.each { |x| yield(x) } }
    end
    nil
  end
end

# The built-in ordered types are Comparable: each defines <=> natively, so they
# pick up <, <=, >, >=, between?, and clamp from the module above. (The
# comparison operators still take the VM's inline fast path; between?/clamp route
# through <=>.) Numeric carries Comparable for the whole numeric tower
# (Integer/Float/Rational/Complex inherit it); String mixes it in directly.
class Numeric
  include Comparable
end

class String
  include Comparable
end

class Symbol
  include Comparable
end

# Array and Range are Enumerable: each defines `each` natively, so they pick up
# select/reject/find/reduce/sum/any?/all?/none?/each_with_index from the module
# above. Their own native methods (map, include?, min, max, count, …) take
# precedence over the module's where both exist. (Hash also wants Enumerable but
# needs block auto-splat for its [k, v] pairs first.)
class Array
  include Enumerable
  # The deconstruct protocol for case/in array patterns: an Array deconstructs
  # to itself.
  def deconstruct
    self
  end
end

class Range
  include Enumerable
end

# Hash is Enumerable too: Hash#each yields a [key, value] pair, so map/find/count
# /any?/all?/none?/to_a operate on pairs. select/reject are native (they return a
# Hash, not an Array).
class Hash
  include Enumerable
  # The deconstruct_keys protocol for case/in hash patterns: a Hash returns
  # itself (the requested key list is advisory, so we ignore it).
  def deconstruct_keys(keys)
    self
  end
end

# ---------------------------------------------------------------------------
# Embedded pure-Ruby standard library
#
# Modules below are part of MRI's stdlib but are cleaner to express in Ruby than
# in Go. They are written from scratch to match MRI's observable behaviour (no
# MRI source is copied — that would import Ruby's license). The matching feature
# names are registered as "provided" so `require "<name>"` returns true/false
# like a normal gem load. require'ing them is a no-op since they are already here.
# ---------------------------------------------------------------------------

# RubyGems shim: just enough of Gem / Gem::Version / Gem::Requirement for the
# version comparisons real apps (Puppet, Rails, …) run at load time. Versions
# compare segment-by-segment with the usual prerelease rules; this is not a
# package manager.
module Gem
  # Gem::Version models a dotted version string and orders versions the way
  # RubyGems does: numeric segments compare numerically, a string (prerelease)
  # segment sorts before the release, and missing trailing segments count as 0.
  class Version
    include Comparable

    VERSION_PATTERN = '[0-9]+(\.[0-9a-zA-Z]+)*(-[0-9A-Za-z.-]+)?'
    ANCHORED_VERSION_PATTERN = Regexp.new('\A\s*(' + VERSION_PATTERN + ')?\s*\z')

    # correct? is true when str parses as a version (RubyGems uses this to guard
    # Version.new).
    def self.correct?(str)
      return false if str.nil?
      !!(str.to_s =~ ANCHORED_VERSION_PATTERN)
    end

    def self.create(input)
      if input.is_a?(Version)
        input
      elsif input.nil?
        nil
      else
        new(input)
      end
    end

    attr_reader :version

    def initialize(version)
      unless self.class.correct?(version)
        raise ArgumentError, "Malformed version number string #{version}"
      end
      @version = version.to_s.strip.gsub("-", ".pre.")
      @version = "0" if @version.empty?
    end

    def to_s
      @version
    end

    def inspect
      "#<#{self.class.name} #{@version.inspect}>"
    end

    # segments splits the version into Integer (numeric) and String (alpha)
    # parts, e.g. "1.2.a" -> [1, 2, "a"].
    def segments
      @segments ||= @version.scan(/[0-9]+|[a-zA-Z]+/).map do |s|
        s =~ /\A\d+\z/ ? s.to_i : s
      end
    end

    # prerelease? is true when any segment is non-numeric (e.g. "1.2.a").
    def prerelease?
      @version =~ /[a-zA-Z]/ ? true : false
    end

    def release
      return self unless prerelease?
      segs = segments
      segs.pop while !segs.empty? && !segs.last.is_a?(Integer)
      self.class.new(segs.join("."))
    end

    # bump drops the last segment and increments the new last numeric one, the
    # RubyGems "next minor/patch" operation: "1.0" -> "2", "1.2.3" -> "1.3".
    def bump
      segs = segments.dup
      segs.pop while !segs.empty? && !segs.last.is_a?(Integer)
      segs.pop if segs.size > 1
      segs[-1] = segs[-1] + 1
      self.class.new(segs.join("."))
    end

    def <=>(other)
      other = self.class.create(other)
      return nil if other.nil?
      lhs = segments
      rhs = other.segments
      limit = lhs.size > rhs.size ? lhs.size : rhs.size
      i = 0
      while i < limit
        l = lhs[i]
        r = rhs[i]
        # Missing trailing segment counts as 0 (release) so "1.0" == "1.0.0".
        l = 0 if l.nil?
        r = 0 if r.nil?
        c = compare_segment(l, r)
        return c unless c == 0
        i += 1
      end
      0
    end

    def ==(other)
      other = self.class.create(other)
      return false if other.nil?
      (self <=> other) == 0
    end

    def eql?(other)
      other.is_a?(Version) && segments == other.segments
    end

    def hash
      segments.hash
    end

    private

    # compare_segment orders one pair of segments: numbers numerically, strings
    # lexically, and a string (prerelease) before a number (release).
    def compare_segment(l, r)
      if l.is_a?(Integer) && r.is_a?(Integer)
        l <=> r
      elsif l.is_a?(String) && r.is_a?(String)
        l <=> r
      elsif l.is_a?(Integer)
        1 # number (release) sorts after a string (prerelease)
      else
        -1
      end
    end
  end

  # Gem::Requirement holds one or more version constraints (">= 1.2", "~> 2.0",
  # …) and tests versions against all of them.
  class Requirement
    OPS = {
      "="  => lambda { |v, r| v == r },
      "!=" => lambda { |v, r| v != r },
      ">"  => lambda { |v, r| v > r },
      "<"  => lambda { |v, r| v < r },
      ">=" => lambda { |v, r| v >= r },
      "<=" => lambda { |v, r| v <= r },
      "~>" => lambda { |v, r| v >= r && v < r.bump },
    }

    # Operators are matched longest-first so ">=" wins over ">". Built with
    # Regexp.new (not a /…/ literal with #{} interpolation, which the embedded
    # parser does not expand inside a regexp literal).
    PATTERN = Regexp.new('\A\s*(>=|<=|!=|~>|=|>|<)?\s*(' + Version::VERSION_PATTERN + ')\s*\z')

    def self.create(input)
      input.is_a?(Requirement) ? input : new(input)
    end

    def self.default
      new(">= 0")
    end

    attr_reader :requirements

    def initialize(*reqs)
      reqs = reqs.flatten
      reqs = [">= 0"] if reqs.empty?
      @requirements = reqs.map { |r| self.class.parse(r) }
    end

    # parse turns a constraint string into an [op, Version] pair. A bare version
    # means "=".
    def self.parse(obj)
      return ["=", obj] if obj.is_a?(Version)
      m = PATTERN.match(obj.to_s)
      raise ArgumentError, "Illformed requirement [#{obj.inspect}]" unless m
      op = m[1] || "="
      [op, Version.new(m[2])]
    end

    # satisfied_by? is true when version meets every constraint.
    def satisfied_by?(version)
      version = Version.create(version)
      @requirements.all? { |op, req| OPS[op].call(version, req) }
    end
    alias === satisfied_by?
    alias =~ satisfied_by?

    def to_s
      @requirements.map { |op, req| "#{op} #{req}" }.join(", ")
    end
  end

  # ruby_version is the running engine's version as a Gem::Version, used by gems
  # that gate features on the interpreter version.
  def self.ruby_version
    Version.new(RUBY_VERSION)
  end

  def self.win_platform?
    false
  end

  # clear_paths resets RubyGems' cached load paths. This runtime has no gem
  # database, so it is a no-op (Puppet calls it when probing rubygems sources).
  def self.clear_paths; end

  # Specification is the gem metadata registry. Without a gem database its stub
  # list is empty, so gem-directory discovery yields nothing.
  class Specification
    def self.stubs
      []
    end
  end
end

# The English library (require "English") is implemented natively: its long-name
# aliases ($ERROR_INFO, $PROGRAM_NAME, $PID, $MATCH, $PREMATCH, $1…) are resolved
# by the VM's global-variable reader (see specialGvar / englishAlias in Go), so
# both the cryptic and the readable spellings name the same value. No Ruby code
# is needed here.

# OpenStruct (require "ostruct"): a data object whose attributes are defined on
# assignment. Backed by a Hash; reads/writes go through method_missing, and
# bracket access mirrors them.
class OpenStruct
  def initialize(hash = nil)
    @table = {}
    if hash
      hash.each_pair { |k, v| @table[k.to_sym] = v }
    end
  end

  def [](name)
    @table[name.to_sym]
  end

  def []=(name, value)
    @table[name.to_sym] = value
  end

  def to_h
    @table.dup
  end

  def each_pair
    return enum_for(:each_pair) unless block_given?
    @table.each_pair { |k, v| yield(k, v) }
    self
  end

  def members
    @table.keys
  end

  def respond_to_missing?(name, include_private = false)
    n = name.to_s
    @table.key?(name.to_sym) || n.end_with?("=") || super
  end

  def method_missing(name, *args)
    n = name.to_s
    if n.end_with?("=")
      raise ArgumentError, "wrong number of arguments (given #{args.length}, expected 1)" unless args.length == 1
      @table[n[0..-2].to_sym] = args[0]
    elsif args.empty?
      @table[name]
    else
      super
    end
  end

  def ==(other)
    other.is_a?(OpenStruct) && to_h == other.to_h
  end

  def respond_to?(name, include_private = false)
    respond_to_missing?(name, include_private) || super
  end

  def inspect
    pairs = @table.map { |k, v| "#{k}=#{v.inspect}" }.join(", ")
    pairs.empty? ? "#<OpenStruct>" : "#<OpenStruct #{pairs}>"
  end
  alias to_s inspect
end

# Benchmark (require "benchmark"): timing helpers. CPU user/system splits need OS
# per-process accounting this runtime does not expose, so utime/stime are 0.0 and
# real (wall-clock, from Time.now) carries the measurement — enough for the
# common `realtime`/`measure`/`bm` reporting apps do.
module Benchmark
  CAPTION = "       user     system      total        real\n"
  FORMAT = "%10.6f %10.6f %10.6f (%10.6f)\n"

  # Tms holds one measurement. total is user+system; real is wall-clock seconds.
  class Tms
    attr_reader :utime, :stime, :cutime, :cstime, :real, :label

    def initialize(utime = 0.0, stime = 0.0, cutime = 0.0, cstime = 0.0, real = 0.0, label = nil)
      @utime = utime
      @stime = stime
      @cutime = cutime
      @cstime = cstime
      @real = real
      @label = label
    end

    def total
      @utime + @stime + @cutime + @cstime
    end

    def to_s
      format(FORMAT, @utime, @stime, total, @real).chomp + (@label ? " #{@label}" : "") + "\n"
    end

    def format(fmt = nil, *args)
      fmt ||= FORMAT
      Kernel.format(fmt, *args)
    end
  end

  # realtime returns the wall-clock seconds the block took.
  def self.realtime
    t0 = Time.now.to_f
    yield
    Time.now.to_f - t0
  end

  # measure times the block and returns a Tms (real time populated). The block is
  # taken explicitly so callers (Report#report) can forward their own block.
  def self.measure(label = nil, &blk)
    t0 = Time.now.to_f
    blk.call
    real = Time.now.to_f - t0
    Tms.new(0.0, 0.0, 0.0, 0.0, real, label)
  end

  # bm yields a report object; each report(label) { } prints a timed line and the
  # collected Tms list is returned.
  def self.bm(label_width = 0)
    $stdout.print(CAPTION)
    report = Report.new(label_width)
    yield report
    report.list
  end
  # bmbm runs the block twice (rehearsal then real); here it simply behaves like
  # bm, which is sufficient for callers that only need the timings.
  def self.bmbm(width = 0, &blk)
    bm(width, &blk)
  end

  # Report collects bm's per-label measurements.
  class Report
    attr_reader :list

    def initialize(label_width = 0)
      @label_width = label_width
      @list = []
    end

    def report(label = "", &blk)
      t = Benchmark.measure(label, &blk)
      padded = label.to_s.ljust(@label_width)
      $stdout.print(padded + Kernel.format(Benchmark::FORMAT, t.utime, t.stime, t.total, t.real))
      @list << t
      t
    end
  end
end

# Forwardable (require "forwardable"): adds def_delegator(s) to a class so it can
# forward methods to one of its components (an ivar, a method, a constant). A
# class `extend`s Forwardable, then declares the delegations.
module Forwardable
  # def_delegator defines `ali` (default: the same name) to call `method` on the
  # value of `accessor` (an "@ivar" name, or a reader-method/constant name).
  def def_delegator(accessor, method, ali = method)
    accessor = accessor.to_s
    define_method(ali) do |*args, &block|
      target = Forwardable.__resolve_accessor(self, accessor)
      target.__send__(method, *args, &block)
    end
    ali
  end
  alias delegate def_delegator

  # def_delegators forwards several methods to the same accessor at once.
  def def_delegators(accessor, *methods)
    methods.each { |m| def_delegator(accessor, m) }
  end

  # __resolve_accessor reads the delegation target: an "@name" ivar, otherwise a
  # method call on the object.
  def self.__resolve_accessor(obj, accessor)
    if accessor.start_with?("@")
      obj.instance_variable_get(accessor.to_sym)
    else
      obj.__send__(accessor)
    end
  end
end

# SingleForwardable mirrors Forwardable for a single object's singleton class
# (def_single_delegator / def_single_delegators).
module SingleForwardable
  def def_single_delegator(accessor, method, ali = method)
    accessor = accessor.to_s
    define_singleton_method(ali) do |*args, &block|
      target = Forwardable.__resolve_accessor(self, accessor)
      target.__send__(method, *args, &block)
    end
    ali
  end
  alias delegate def_single_delegator

  def def_single_delegators(accessor, *methods)
    methods.each { |m| def_single_delegator(accessor, m) }
  end
end

# Delegator / SimpleDelegator / DelegateClass (require "delegate"): wrap an object
# and forward unknown methods to it. SimpleDelegator wraps a target chosen at
# construction; DelegateClass(klass) builds a subclass that forwards klass's
# public instance methods.
class Delegator
  def initialize(obj)
    __setobj__(obj)
  end

  # method_missing forwards to the delegate; respond_to_missing? mirrors it so
  # respond_to? is accurate.
  def method_missing(name, *args, &block)
    target = __getobj__
    if target.respond_to?(name)
      target.__send__(name, *args, &block)
    else
      super
    end
  end

  def respond_to_missing?(name, include_private = false)
    __getobj__.respond_to?(name, include_private) || super
  end

  def respond_to?(name, include_private = false)
    respond_to_missing?(name, include_private) || super
  end

  def ==(other)
    return true if other.equal?(self)
    __getobj__ == other
  end

  def __getobj__
    raise NotImplementedError, "#{self.class}#__getobj__ is not implemented"
  end

  def __setobj__(_obj)
    raise NotImplementedError, "#{self.class}#__setobj__ is not implemented"
  end
end

# SimpleDelegator delegates to the object passed to new; the target can be
# swapped with __setobj__.
class SimpleDelegator < Delegator
  def __getobj__
    @delegate_sd_obj
  end

  def __setobj__(obj)
    @delegate_sd_obj = obj
  end
end

# DelegateClass(superclass) returns a new Delegator subclass that forwards
# superclass's public instance methods to the wrapped object. The returned class
# is subclassed by the caller (`class Foo < DelegateClass(Array)`).
def DelegateClass(superclass)
  klass = Class.new(Delegator)
  klass.class_eval do
    def __getobj__
      @delegate_dc_obj
    end

    def __setobj__(obj)
      @delegate_dc_obj = obj
    end
  end
  # Forward each of the wrapped class's PUBLIC instance methods explicitly —
  # including those it inherits — so they take precedence over a same-named method
  # the delegator would otherwise resolve first. This matters for methods like
  # IO#print / IO#flush / IO#write that File inherits: Kernel also defines a
  # private #print, so without an explicit forwarder `delegate.print` would hit
  # Kernel#print (writing to $stdout) instead of reaching the wrapped IO. MRI's
  # DelegateClass forwards superclass.public_instance_methods for exactly this
  # reason. The skip list keeps the delegator's own infrastructure (object
  # identity, send, the __getobj__/__setobj__ pair) intact.
  skip = [
    :__getobj__, :__setobj__, :__send__, :send, :public_send,
    :initialize, :initialize_copy, :initialize_clone, :initialize_dup,
    :method_missing, :respond_to_missing?, :respond_to?, :==,
    :object_id, :equal?, :instance_variable_get, :instance_variable_set,
    :instance_variables, :__id__, :class, :is_a?, :kind_of?, :instance_of?,
  ]
  superclass.instance_methods.each do |m|
    next if skip.include?(m)
    klass.send(:define_method, m) do |*args, &block|
      __getobj__.__send__(m, *args, &block)
    end
  end
  klass
end

# Pathname (require "pathname"): an object wrapper over a filesystem path string.
# This implements the pure path manipulation (no I/O): join, parent, basename,
# dirname, extname, absolute?/relative?, cleanpath and comparison. File-touching
# methods (exist?, read, …) are out of scope here.
class Pathname
  include Comparable
  SEPARATOR = "/"

  def initialize(path)
    path = path.to_s if path.is_a?(Pathname)
    raise TypeError, "no implicit conversion into String" unless path.is_a?(String)
    @path = path
  end

  def to_s
    @path
  end
  alias to_path to_s

  def inspect
    "#<Pathname:#{@path}>"
  end

  def to_str
    @path
  end

  def freeze
    @path.freeze
    super
  end

  def ==(other)
    other.is_a?(Pathname) && other.to_s == @path
  end
  alias eql? ==

  def <=>(other)
    return nil unless other.is_a?(Pathname)
    @path <=> other.to_s
  end

  def hash
    @path.hash
  end

  def absolute?
    @path.start_with?(SEPARATOR)
  end

  def relative?
    !absolute?
  end

  def root?
    @path =~ /\A\/+\z/ ? true : false
  end

  # + / join append one or more path components, MRI's Pathname#join semantics:
  # an absolute component resets to the root, otherwise components are separated
  # by a single "/".
  def +(other)
    other = Pathname.new(other) unless other.is_a?(Pathname)
    Pathname.new(Pathname.__plus(@path, other.to_s))
  end

  def /(other)
    self + other
  end

  def join(*args)
    result = self
    args.each { |a| result = result + a }
    result
  end

  # basename returns the last path component (optionally stripping a suffix, or
  # ".*" for any extension).
  def basename(suffix = "")
    base = @path.split(SEPARATOR).reject(&:empty?).last
    base = SEPARATOR if base.nil?
    if suffix == ".*"
      e = File_extname(base)
      base = base[0...(base.length - e.length)] unless e.empty?
    elsif !suffix.empty? && base.end_with?(suffix)
      base = base[0...(base.length - suffix.length)]
    end
    Pathname.new(base)
  end

  def dirname
    idx = @path.rindex(SEPARATOR)
    return Pathname.new(".") if idx.nil?
    return Pathname.new(SEPARATOR) if idx == 0
    Pathname.new(@path[0...idx])
  end
  alias parent dirname

  def extname
    File_extname(basename.to_s)
  end

  def split
    [dirname, basename]
  end

  def each_filename
    return enum_for(:each_filename) unless block_given?
    @path.split(SEPARATOR).reject(&:empty?).each { |f| yield f }
  end

  # ascend yields the path then each parent up to the root (or the first relative
  # component), like MRI's Pathname#ascend. descend is the same sequence reversed.
  def ascend
    return enum_for(:ascend) unless block_given?
    paths = ascend_paths
    paths.each { |p| yield Pathname.new(p) }
    self
  end

  def descend
    return enum_for(:descend) unless block_given?
    ascend_paths.reverse_each { |p| yield Pathname.new(p) }
    self
  end

  def ascend_paths
    out = [@path]
    cur = @path
    loop do
      idx = cur.rindex(SEPARATOR)
      break if idx.nil?
      if idx == 0
        out << SEPARATOR unless cur == SEPARATOR
        break
      end
      cur = cur[0...idx]
      out << cur
    end
    out
  end

  # cleanpath collapses "." and ".." components and redundant separators.
  def cleanpath
    abs = absolute?
    parts = @path.split(SEPARATOR).reject { |p| p.empty? || p == "." }
    out = []
    parts.each do |p|
      if p == ".."
        if !out.empty? && out.last != ".."
          out.pop
        elsif !abs
          out << p
        end
      else
        out << p
      end
    end
    cleaned = out.join(SEPARATOR)
    if abs
      Pathname.new(SEPARATOR + cleaned)
    else
      Pathname.new(cleaned.empty? ? "." : cleaned)
    end
  end

  def sub_ext(repl)
    e = File_extname(@path)
    Pathname.new(@path[0...(@path.length - e.length)] + repl)
  end

  # relative_path_from returns self expressed relative to base_directory, using
  # only the lexical components (no filesystem access), matching MRI: both paths
  # are cleaned and split, a shared prefix is dropped, each remaining base
  # component contributes a ".." and the remaining self components follow. Mixing
  # an absolute path with a relative one — or a ".." that escapes a relative
  # base — raises ArgumentError, as in MRI.
  def relative_path_from(base_directory)
    base_directory = Pathname.new(base_directory) unless base_directory.is_a?(Pathname)
    dest = cleanpath
    base = base_directory.cleanpath
    if dest.absolute? != base.absolute?
      dest_prefix = dest.absolute? ? SEPARATOR : ""
      raise ArgumentError, "different prefix: #{dest_prefix.inspect} and #{base_directory.to_s.inspect}"
    end
    dest_parts = dest.to_s.split(SEPARATOR).reject(&:empty?)
    base_parts = base.to_s.split(SEPARATOR).reject(&:empty?)
    i = 0
    i += 1 while i < dest_parts.length && i < base_parts.length && dest_parts[i] == base_parts[i]
    up = base_parts[i..-1]
    if up.any? { |p| p == ".." }
      raise ArgumentError, "base_directory has ..: #{base_directory.to_s.inspect}"
    end
    rel = up.map { ".." } + dest_parts[i..-1]
    Pathname.new(rel.empty? ? "." : rel.join(SEPARATOR))
  end

  # __plus implements the +/join append rule (class method to keep + small).
  def self.__plus(base, rel)
    return rel if rel.start_with?(SEPARATOR) # absolute resets to root
    return rel if base.empty?
    return base if rel.empty? || rel == "."
    base.end_with?(SEPARATOR) ? base + rel : base + SEPARATOR + rel
  end

  # File_extname is Pathname's own extension extractor (".txt", "" when none),
  # matching File.extname: a leading dot or trailing dot yields "".
  def File_extname(name)
    i = name.rindex(".")
    return "" if i.nil? || i == 0 || i == name.length - 1
    name[i..-1]
  end

  # File-touching delegations. Pathname#read/write/exist?/… forward to the File
  # class with the wrapped path, so callers such as Puppet::FileSystem.read
  # (path.read(**opts)) operate on disk. Keyword options accepted by File.read
  # (e.g. :encoding) are forwarded positionally where File.read expects them.
  def read(*args, **opts)
    File.read(@path, *args)
  end

  def write(content, *args, **opts)
    File.write(@path, content, *args)
  end

  def each_line(*args, &block)
    File.foreach(@path, *args, &block)
  end

  def exist?
    File.exist?(@path)
  end

  def file?
    File.file?(@path)
  end

  def directory?
    File.directory?(@path)
  end

  def open(*args, &block)
    File.open(@path, *args, &block)
  end
end

# URI (require "uri"): parse and assemble URIs. This implements the generic
# component model (scheme, userinfo, host, port, path, query, fragment) used by
# URI.parse / URI() and round-tripping via to_s, plus URI.join for relative
# resolution of the common cases. It is not a full RFC 3986 resolver.
module URI
  # Generic is the base URI; HTTP/HTTPS/FTP carry default ports.
  class Generic
    attr_accessor :scheme, :userinfo, :host, :port, :path, :query, :fragment

    def initialize(scheme: nil, userinfo: nil, host: nil, port: nil, path: "", query: nil, fragment: nil)
      @scheme = scheme
      @userinfo = userinfo
      @host = host
      @port = port
      @path = path || ""
      @query = query
      @fragment = fragment
    end

    def to_s
      s = +""
      s << "#{@scheme}:" if @scheme
      if @host || @userinfo
        s << "//"
        s << "#{@userinfo}@" if @userinfo
        s << @host.to_s
        s << ":#{@port}" if @port && !default_port?
      end
      s << @path.to_s
      s << "?#{@query}" if @query
      s << "##{@fragment}" if @fragment
      s
    end
    alias to_str to_s

    def inspect
      "#<#{self.class} #{self}>"
    end

    def ==(other)
      other.is_a?(Generic) && to_s == other.to_s
    end

    def hostname
      @host
    end

    # default_port returns the scheme's well-known port (nil for unknown schemes);
    # default_port? is true when @port equals it (so to_s can omit it).
    def default_port
      URI::DEFAULT_PORTS[@scheme]
    end

    def default_port?
      !@port.nil? && @port == default_port
    end

    # build constructs a URI from explicit components, passed as a Hash keyed by
    # component name or as an Array in COMPONENT order. Invoked on the receiver
    # class, so URI::HTTP.build returns a URI::HTTP. Mirrors URI::Generic.build.
    def self.build(args)
      case args
      when Hash
        h = {}
        args.each { |k, v| h[k.to_sym] = v }
        new(scheme: h[:scheme], userinfo: h[:userinfo], host: h[:host],
            port: h[:port], path: h[:path] || "", query: h[:query],
            fragment: h[:fragment])
      when Array
        comp = [:scheme, :userinfo, :host, :port, :path, :query, :fragment]
        unless args.length == comp.length
          raise ArgumentError,
                "expected Array of or Hash of components of #{self} (#{comp.join(', ')})"
        end
        h = {}
        comp.each_with_index { |k, i| h[k] = args[i] }
        new(scheme: h[:scheme], userinfo: h[:userinfo], host: h[:host],
            port: h[:port], path: h[:path] || "", query: h[:query],
            fragment: h[:fragment])
      else
        raise ArgumentError, "expected Array of or Hash of components of #{self}"
      end
    end

    # merge / + resolves a relative reference against this URI for the common
    # cases (absolute reference, absolute path, or last-segment replacement).
    def merge(rel)
      rel = URI.parse(rel.to_s) unless rel.is_a?(Generic)
      return rel if rel.scheme
      merged = URI.parse(to_s)
      merged.fragment = rel.fragment
      merged.query = rel.query
      if rel.path.nil? || rel.path.empty?
        return merged
      elsif rel.path.start_with?("/")
        merged.path = rel.path
      else
        base = @path.empty? ? "/" : @path
        dir = base.sub(/[^\/]*\z/, "")
        merged.path = URI.__normalize_path(dir + rel.path)
      end
      merged
    end
    alias + merge
  end

  class HTTP < Generic; end
  class HTTPS < Generic; end
  class FTP < Generic; end
  class File < Generic; end
  class LDAP < Generic; end

  DEFAULT_PORTS = { "http" => 80, "https" => 443, "ftp" => 21, "ldap" => 389 }
  SCHEME_CLASSES = { "http" => HTTP, "https" => HTTPS, "ftp" => FTP, "file" => File, "ldap" => LDAP }

  # Anchored URI grammar: scheme, authority (userinfo@host:port), path, query and
  # fragment. Built with Regexp.new (no #{} interpolation in literals here).
  PARSE_RE = Regexp.new(
    '\A' \
    '(?:([^:/?#]+):)?' \
    '(?://(?:([^/?#@]*)@)?([^/?#:]*)(?::(\d+))?)?' \
    '([^?#]*)' \
    '(?:\?([^#]*))?' \
    '(?:#(.*))?' \
    '\z'
  )

  # parse splits a URI string into a Generic (or scheme-specific subclass).
  def self.parse(uri)
    m = PARSE_RE.match(uri.to_s)
    raise InvalidURIError, "bad URI(is not URI?): #{uri.inspect}" unless m
    scheme = m[1]
    userinfo = m[2]
    host = m[3]
    host = nil if host == "" && userinfo.nil? && !uri.to_s.include?("//")
    port = m[4] ? m[4].to_i : nil
    path = m[5] || ""
    query = m[6]
    fragment = m[7]
    klass = scheme ? (SCHEME_CLASSES[scheme.downcase] || Generic) : Generic
    klass.new(scheme: scheme, userinfo: userinfo, host: (host == "" ? nil : host),
              port: port, path: path, query: query, fragment: fragment)
  end

  # join resolves each successive reference against the running base.
  def self.join(*uris)
    raise ArgumentError, "wrong number of arguments (given 0, expected 1+)" if uris.empty?
    result = parse(uris.first.to_s)
    uris[1..-1].each { |u| result = result.merge(u) }
    result
  end

  # __normalize_path collapses "." and ".." in a resolved path.
  def self.__normalize_path(path)
    parts = path.split("/", -1)
    out = []
    parts.each_with_index do |p, i|
      if p == "."
        out << "" if i == parts.length - 1
      elsif p == ".."
        out.pop unless out.empty? || out.last == ".."
        out << "" if i == parts.length - 1
      else
        out << p
      end
    end
    out.join("/")
  end

  class InvalidURIError < StandardError; end

  # Parser is a small stand-in for MRI's RFC3986_Parser: it exposes #parse (a
  # thin wrapper over URI.parse) and #make_regexp, which builds a Regexp matching
  # an absolute URI (optionally restricted to a set of schemes). DEFAULT_PARSER is
  # the shared instance MRI also exposes; code such as Puppet's Pcore references
  # URI::DEFAULT_PARSER.make_regexp to derive a URI type pattern.
  class Parser
    def parse(uri)
      URI.parse(uri)
    end

    # make_regexp returns a Regexp anchored to match a whole absolute URI. With a
    # list of schemes only those schemes match; without one any scheme is allowed.
    def make_regexp(schemes = nil)
      scheme = if schemes && !schemes.empty?
                 "(?:#{schemes.map { |s| Regexp.escape(s.to_s) }.join('|')})"
               else
                 "[a-zA-Z][a-zA-Z\\d+\\-.]*"
               end
      Regexp.new("\\A#{scheme}:(?://(?:[^/?#@]*@)?[^/?#:]*(?::\\d+)?)?[^?#]*(?:\\?[^#]*)?(?:#.*)?\\z")
    end
  end

  DEFAULT_PARSER = Parser.new
  RFC3986_PARSER = DEFAULT_PARSER
end

# URI() is the Kernel-level shorthand for URI.parse, matching MRI.
module Kernel
  def URI(uri)
    return uri if uri.is_a?(URI::Generic)
    URI.parse(uri)
  end
  module_function :URI
end

# Singleton turns its includer into a single-instance class (require "singleton").
# Mixing it in privatizes .new/.allocate and adds a memoizing .instance, matching
# MRI's lib/singleton.rb behavior on the surface code relies on (Puppet::Runtime
# is `include Singleton`).
module Singleton
  # The class methods Singleton grafts onto the includer via extend.
  module SingletonClassMethods
    # instance returns the one-and-only instance, building it on first call and
    # caching it on the class thereafter.
    def instance
      @__singleton_instance__ ||= new
    end
  end

  # included is the mix-in hook: it extends the includer with the class methods
  # and makes the constructors private, so the instance is reachable only via
  # .instance — calling .new raises NoMethodError, as in MRI.
  def self.included(klass)
    klass.extend(SingletonClassMethods)
    klass.private_class_method(:new, :allocate)
  end
end

# Find supports top-down traversal of a set of file paths (require "find"),
# written from scratch to match MRI's lib/find.rb behavior. Find.find walks each
# given path breadth-first-by-directory, yielding every file and directory it
# reaches; Find.prune (called inside the block) skips into the current directory.
module Find
  VERSION = "0.2.0"

  # find yields the name of every path given, then recursively the entries of any
  # directory among them, depth-first with sorted children (matching MRI's order).
  # With no block it returns an Enumerator. Missing top-level paths raise
  # Errno::ENOENT; per-entry errors are swallowed when ignore_error is true.
  def find(*paths, ignore_error: true) # :yield: path
    return enum_for(__method__, *paths, ignore_error: ignore_error) unless block_given?

    paths.collect! { |d| raise Errno::ENOENT, d unless File.exist?(d); d.dup }.each do |path|
      ps = [path]
      while (file = ps.shift)
        catch(:prune) do
          yield file.dup
          # A directory's children are pushed in reverse-sorted order so that
          # shifting from the front visits them in ascending order, depth-first.
          if File.directory?(file)
            children =
              begin
                Dir.children(file)
              rescue SystemCallError
                raise unless ignore_error
                next
              end
            children.sort!
            children.reverse_each { |f| ps.unshift(File.join(file, f)) }
          end
        end
      end
    end
    nil
  end

  # prune skips the current file or directory, restarting the traversal loop with
  # the next entry; for a directory it is not descended into. Only meaningful
  # inside the block passed to Find.find.
  def prune
    throw :prune
  end

  module_function :find, :prune
end

# ERB is a pure-Go embedded-Ruby template engine (require "erb"), matching MRI
# 4.0.5's observable behavior. A template mixes literal text with three tag kinds —
#
#   <% code %>      run Ruby, emit nothing
#   <%= expr %>     run Ruby, append expr.to_s to the output buffer
#   <%# comment %>  ignored entirely
#
# and the literals <%% / %%> stand for a single <% / %>. The template scan/compile
# (and the trim-mode handling) is done by the go-ruby-erb library, reached through
# the native ERB.__compile below; #result evals the compiled source in a
# caller-supplied binding, so the template sees the caller's locals (the same
# approach MRI takes). The ERB class and ERB::Util are created natively (erb.go);
# this reopens ERB to add the interpreter-bound public API.
class ERB

  attr_reader :src, :encoding
  attr_accessor :filename, :lineno

  # new compiles the template string str into evaluable Ruby source. Only the
  # modern keyword API is offered, matching MRI 4.0.5 (which dropped the legacy
  # positional safe_level/trim_mode/eoutvar arguments). trim_mode controls newline
  # trimming around tags; eoutvar names the output buffer used by the compiled src.
  def initialize(str, trim_mode: nil, eoutvar: "_erbout")
    @filename = nil
    @lineno = 0
    @encoding = "UTF-8"
    # The go-ruby-erb library scans and compiles the template, returning the Ruby
    # source (already carrying the "#coding:UTF-8\n" magic prefix) that, when
    # eval'd in a binding, builds and returns the rendered string.
    @src = __compile(str, trim_mode, eoutvar)
  end

  # result evaluates the compiled source in binding b and returns the rendered
  # string, so the template can reference the caller's local variables and methods.
  # With no argument it renders against a fresh empty top-level binding.
  def result(b = new_toplevel)
    eval(@src, b, (@filename || "(erb)"), @lineno)
  end

  # result_with_hash renders the template with the entries of hash bound as local
  # variables, mirroring MRI: each key becomes a local set to its value before the
  # template body runs.
  def result_with_hash(hash)
    b = new_toplevel
    hash.each do |key, value|
      b.local_variable_set(key, value)
    end
    result(b)
  end

  # run renders to $stdout (the convenience method MRI offers alongside result).
  def run(b = new_toplevel)
    print result(b)
  end

  private

  # new_toplevel produces a fresh binding for result_with_hash so each render gets
  # its own isolated set of locals rather than mutating a shared TOPLEVEL_BINDING.
  def new_toplevel
    binding
  end
end

# ---------------------------------------------------------------------------
# OptionParser (optparse): a command-line option parser matching MRI's surface —
# declaration (#on/#on_tail/#on_head/#separator), the parse family (#parse/#parse!/
# #order/#order!/#permute/#permute!/#getopts), type coercion, abbreviation,
# --[no-] negation, bundled shorts, #help/#to_s, and the OptionParser::ParseError
# tree. Required by Puppet's CLI.
# ---------------------------------------------------------------------------
# OptionParser's argv-parsing ENGINE now lives in the pure-Go
# github.com/go-ruby-optparse/optparse library, bound natively (internal/vm/
# optparse.go): the native OptionParser class owns new / on / on_head / on_tail /
# define / def_option / separator / accept / reject, the banner / summary /
# program_name / version / release / ver accessors, parse / parse! / order /
# order! / permute / permute! / getopts, and help / to_s / summarize. The blocks
# passed to on(...) are held in the host and dispatched against the library's
# ordered matches; a library ParseError is re-raised as the matching
# OptionParser::* exception below.
#
# Only the ParseError class tree and the Switch value struct stay in Ruby: they
# are the Ruby-object surface (reason / args / message / recover, and the
# constructible OptionParser::InvalidArgument.new(...) form) that programs and
# tests touch directly, which the interpreter-independent library does not model.
# This reopens the native OptionParser class to add them.
class OptionParser
  class ParseError < StandardError
    attr_reader :args
    def initialize(*args)
      @args = args
      # Build the full "reason: arg arg" message at construction so the raise-time
      # message (the one shown for an uncaught error) already reads like MRI's,
      # rather than only #message reconstructing it after the fact.
      super(reason + ": " + args.join(' '))
    end
    def self.reason; "parse error"; end
    def reason; self.class.reason; end
    def message
      reason + ": " + args.join(' ')
    end
    alias to_s message
    def recover(argv)
      argv[0, 0] = @args
      argv
    end
  end

  class InvalidOption < ParseError
    def self.reason; "invalid option"; end
  end
  class MissingArgument < ParseError
    def self.reason; "missing argument"; end
  end
  class InvalidArgument < ParseError
    def self.reason; "invalid argument"; end
  end
  class AmbiguousOption < ParseError
    def self.reason; "ambiguous option"; end
  end
  class AmbiguousArgument < InvalidArgument
    def self.reason; "ambiguous argument"; end
  end
  class NeedlessArgument < ParseError
    def self.reason; "needless argument"; end
  end

  Switch = Struct.new(:short, :long, :arg, :mandatory, :optional, :negatable,
                      :conv, :desc, :block)
end

OptParse = OptionParser

# ---------------------------------------------------------------------------
# Tempfile (tempfile): a from-scratch pure-Ruby temporary file built over File
# and Dir.tmpdir. It creates a uniquely-named file in the temp directory and
# delegates the IO methods (write/read/flush/print/rewind/each_line/...) to the
# underlying File, the way MRI's Tempfile delegates to its internal file. close
# leaves the file on disk; unlink/delete removes it and clears #path. Required
# by Puppet's file type at load.
# ---------------------------------------------------------------------------
class Tempfile
  attr_reader :path

  def self.counter
    @counter = (@counter || 0) + 1
  end

  def initialize(basename = "", tmpdir = nil, mode: "w+", **_opts)
    require "tmpdir"
    prefix, suffix = basename.is_a?(Array) ? basename : [basename.to_s, ""]
    dir = tmpdir || Dir.tmpdir
    @path = nil
    100.times do
      name = "#{prefix}#{Process.pid}-#{Tempfile.counter}-#{rand(0x100000000).to_s(36)}#{suffix}"
      candidate = File.join(dir, name)
      unless File.exist?(candidate)
        @path = candidate
        break
      end
    end
    @file = File.open(@path, mode)
    if block_given?
      begin
        yield self
      ensure
        close
        unlink
      end
    end
  end

  # open re-opens a closed Tempfile so a caller can keep writing (Puppet's
  # data_sync does Tempfile.new(...) then tempfile.open). It is also offered as a
  # class method mirroring Tempfile.new with a block.
  def open
    @file = File.open(@path, "r+") unless @file && !@closed
    @closed = false
    @file
  end

  def self.open(*args, &block)
    new(*args, &block)
  end

  def self.create(basename = "", tmpdir = nil, **opts)
    t = new(basename, tmpdir, **opts)
    if block_given?
      begin
        return yield(t)
      ensure
        t.close
        t.unlink
      end
    end
    t
  end

  def close(unlink_now = false)
    @file.close if @file && !@closed
    @closed = true
    unlink if unlink_now
    nil
  end

  def close!
    close(true)
  end

  def unlink
    if @path && File.exist?(@path)
      File.unlink(@path)
    end
    @path = nil
    nil
  end
  alias delete unlink

  def chmod(mode)
    File.chmod(mode, @path) if @path && File.respond_to?(:chmod)
  end

  def respond_to_missing?(name, include_private = false)
    (@file && @file.respond_to?(name, include_private)) || super
  end

  def method_missing(name, *args, &block)
    if @file && @file.respond_to?(name)
      @file.send(name, *args, &block)
    else
      super
    end
  end
end
