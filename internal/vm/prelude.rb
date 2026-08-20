# frozen_string_literal: true
#
# The embedded-Ruby prelude: standard library pieces that are cleaner to express
# in Ruby than in Go. Loaded once by VM.New after the native bootstrap, so every
# program sees these modules. This is the org's USP — Comparable and Enumerable
# are written *once*, in Ruby, on top of a single primitive each (`<=>` / `each`).

# Comparable derives the ordering operators from `<=>`. A class mixes it in and
# defines `<=>`; everything else follows.
module Comparable
  # __compare drives every ordering operator: it sends `<=>` and, when that
  # returns nil (the two are incomparable), raises the same ArgumentError MRI's
  # rb_cmperr produces. The right-hand operand is rendered by #inspect when it is
  # an immediate (Integer/Float/Symbol/nil/true/false) and by its class name
  # otherwise — e.g. `comparison of Foo with 1 failed` vs `... with String failed`.
  def __compare(other)
    cmp = (self <=> other)
    if cmp.nil?
      right = case other
              when Integer, Float, Symbol, nil, true, false
                other.inspect
              else
                other.class
              end
      raise ArgumentError, "comparison of #{self.class} with #{right} failed"
    end
    cmp
  end

  def <(other)
    __compare(other) < 0
  end

  def <=(other)
    __compare(other) <= 0
  end

  def >(other)
    __compare(other) > 0
  end

  def >=(other)
    __compare(other) >= 0
  end

  # Comparable#== is deliberately lenient: an incomparable pair (`<=>` returning
  # nil) is simply unequal rather than an error, matching MRI.
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

  # clamp bounds self to a [min, max] interval, accepting either two arguments or
  # a single Range. A one-argument non-Range call is a TypeError, an exclusive
  # Range or a min greater than max is an ArgumentError, and a nil bound leaves
  # that side unbounded — all exactly as MRI.
  def clamp(*args)
    if args.size == 1 && args[0].is_a?(Range)
      range = args[0]
      # An exclusive range is rejected only when it has a finite end; an endless
      # `a...` has no end, so its exclusivity is irrelevant and MRI allows it.
      if range.exclude_end? && !range.end.nil?
        raise ArgumentError, "cannot clamp with an exclusive range"
      end
      min = range.begin
      max = range.end
    elsif args.size == 2
      min, max = args
    elsif args.size == 1
      raise TypeError, "wrong argument type #{args[0].class} (expected Range)"
    else
      raise ArgumentError, "wrong number of arguments (given #{args.size}, expected 1..2)"
    end
    # Compare through <=> (like MRI), so bounds/self that only define <=> — not the
    # Comparable `<`/`>` operators — still clamp; a nil comparison is an error.
    if !min.nil? && !max.nil?
      c = (min <=> max)
      if c.nil? || c > 0
        raise ArgumentError, "min argument must be less than or equal to max argument"
      end
    end
    unless min.nil?
      c = (self <=> min)
      raise ArgumentError, "comparison of #{self.class} with #{min} failed" if c.nil?
      return min if c < 0
    end
    unless max.nil?
      c = (self <=> max)
      raise ArgumentError, "comparison of #{self.class} with #{max} failed" if c.nil?
      return max if c > 0
    end
    self
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
    each { |*a| yield(__pack(a)) }
  end

  # __pack applies CRuby's rb_enum_values_pack to one #each yield's arguments:
  # zero arguments become nil, a lone value stays scalar, several gather into an
  # Array. Collection methods iterate via __each_packed; the predicates below
  # instead forward the raw arguments to their block (so block arity governs)
  # and only pack for the pattern / no-block forms, exactly as MRI does.
  def __pack(a)
    a.empty? ? nil : (a.size == 1 ? a[0] : a)
  end

  # __enum_int_arg coerces a count argument the way MRI's rb_num2long does for
  # first/take/drop: Integers pass through, Floats truncate, anything else must
  # answer #to_int (and yield an Integer); a non-numeric argument is a TypeError.
  # Counts beyond the machine-long range are a RangeError, exactly like MRI.
  def __enum_int_arg(n)
    if n.is_a?(Integer)
      v = n
    elsif n.is_a?(Float)
      v = n.to_int
    elsif n.respond_to?(:to_int)
      v = n.to_int
      raise TypeError, "can't convert #{n.class} to Integer (#{n.class}#to_int gives #{v.class})" unless v.is_a?(Integer)
    else
      raise TypeError, "no implicit conversion of #{n.class} into Integer"
    end
    raise RangeError, "bignum too big to convert into 'long'" if v > 9223372036854775807 || v < -9223372036854775808
    v
  end

  def to_a
    r = []
    __each_packed { |x| r << x }
    r
  end

  # to_set: a new Set of the elements (each preprocessed by the block, if given),
  # as MRI's Enumerable#to_set. Defined here so every Enumerable — Array, Range,
  # Hash, … — can be turned into a Set.
  def to_set(&block)
    Set.new(self, &block)
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
    raise ArgumentError, "wrong number of arguments (given #{args.length}, expected 0..1)" if args.length > 1
    n = 0
    if !args.empty?
      item = args[0]
      each { |*a| n = n + 1 if __pack(a) == item }
    elsif block_given?
      each { |*a| n = n + 1 if yield(*a) }
    else
      each { |*a| n = n + 1 }
    end
    n
  end

  # min_by / max_by / sort_by delegate to Array's native implementations via the
  # pair/element list, so any Enumerable (Hash, Range, Struct, …) gains them.
  def min_by(*args)
    return enum_for(:min_by, *args) unless block_given?
    to_a.min_by(*args) { |x| yield(x) }
  end

  def max_by(*args)
    return enum_for(:max_by, *args) unless block_given?
    to_a.max_by(*args) { |x| yield(x) }
  end

  def sort(&block)
    to_a.sort(&block)
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
    catch(:__enum_any) do
      each { |*a|
        truth = no_pat ? (blk ? yield(*a) : __pack(a)) : (pattern === __pack(a))
        throw :__enum_any, true if truth
      }
      false
    end
  end

  def all?(pattern = (no_pat = true; nil))
    blk = block_given?
    catch(:__enum_all) do
      each { |*a|
        truth = no_pat ? (blk ? yield(*a) : __pack(a)) : (pattern === __pack(a))
        throw :__enum_all, false unless truth
      }
      true
    end
  end

  def none?(pattern = (no_pat = true; nil))
    blk = block_given?
    catch(:__enum_none) do
      each { |*a|
        truth = no_pat ? (blk ? yield(*a) : __pack(a)) : (pattern === __pack(a))
        throw :__enum_none, false if truth
      }
      true
    end
  end

  # one? is true when exactly one element (or pattern/block match) is truthy. It
  # stops as soon as a second match is seen. Like any?/all?/none? the block form
  # forwards raw #each arguments (block arity governs) while the pattern form
  # matches against the packed value.
  def one?(pattern = (no_pat = true; nil))
    blk = block_given?
    n = 0
    catch(:__enum_one) do
      each { |*a|
        truth = no_pat ? (blk ? yield(*a) : __pack(a)) : (pattern === __pack(a))
        if truth
          n = n + 1
          throw :__enum_one if n > 1
        end
      }
    end
    n == 1
  end

  # uniq returns the elements with duplicates removed, keeping first occurrence.
  # Without a block elements are compared by #hash and #eql? (through a Hash);
  # with a block the block's return value is the uniqueness key. Multi-value
  # yields are gathered into whole Arrays.
  def uniq
    result = []
    keys = []
    hashes = []
    __each_packed { |x|
      key = block_given? ? yield(x) : x
      h = key.hash
      seen = false
      i = 0
      while i < keys.length
        if hashes[i] == h && keys[i].eql?(key)
          seen = true
          break
        end
        i = i + 1
      end
      unless seen
        keys << key
        hashes << h
        result << x
      end
    }
    result
  end

  # compact returns the elements with every nil removed (a zero-argument yield
  # packs to nil and is dropped too).
  def compact
    result = []
    __each_packed { |x| result << x unless x.nil? }
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

  # collect_concat is the classic alias of flat_map.
  def collect_concat(&blk)
    return enum_for(:collect_concat) unless block_given?
    flat_map(&blk)
  end

  # each_entry yields each element as MRI's rb_enum_values_pack packs it: a lone
  # value stays scalar, a multi-value #each yield gathers into an Array, and a
  # zero-argument yield becomes nil. Unlike map/select (whose block arity governs
  # a multi-value yield), each_entry always hands the block one packed value. With
  # no block it returns a sized Enumerator; with a block it returns self.
  def each_entry
    return enum_for(:each_entry) { size if respond_to?(:size) } unless block_given?
    __each_packed { |x| yield(x) }
    self
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

  # tally counts occurrences into a Hash of element => count. With a Hash argument
  # it accumulates INTO (and returns) that hash: a missing key starts at 0, an
  # existing count must already be an Integer (else TypeError), and a non-Hash
  # argument is a TypeError. More than one argument is an ArgumentError.
  def tally(*args)
    raise ArgumentError, "wrong number of arguments (given #{args.length}, expected 0..1)" if args.length > 1
    if args.empty?
      h = {}
    else
      a = args[0]
      raise TypeError, "no implicit conversion of #{a.nil? ? "nil" : a.class} into Hash" unless a.is_a?(Hash)
      h = a
    end
    __each_packed { |x|
      if h.key?(x)
        c = h[x]
        unless c.is_a?(Integer)
          tn = c.nil? ? "nil" : (c == true ? "true" : (c == false ? "false" : c.class))
          raise TypeError, "wrong argument type #{tn} (expected Integer)"
        end
      else
        c = 0
      end
      h[x] = c + 1
    }
    h
  end

  # zip pairs each element with the correspondingly-indexed element of every other
  # collection (a shorter operand pads with nil). Each other is taken via #to_ary
  # or, failing that, #to_a so any Enumerable works. With a block each row is
  # yielded and zip returns nil; without one it returns the Array of rows.
  def zip(*others)
    others = others.map { |o| o.respond_to?(:to_ary) ? o.to_ary : o.to_a }
    blk = block_given?
    r = blk ? nil : []
    i = 0
    __each_packed { |x|
      row = [x]
      others.each { |o| row << o[i] }
      if blk
        yield(row)
      else
        r << row
      end
      i = i + 1
    }
    r
  end

  # find_index(value) / find_index { |x| } — the index of the first match, or nil.
  # With neither a value nor a block it returns an Enumerator; a second argument
  # is an ArgumentError.
  def find_index(*args)
    raise ArgumentError, "wrong number of arguments (given #{args.length}, expected 0..1)" if args.length > 1
    return enum_for(:find_index) if args.empty? && !block_given?
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

  # each_slice(n) yields consecutive, non-overlapping groups of n elements (the
  # final group may be shorter). each_cons(n) yields every overlapping window of
  # n elements. Both coerce n through #to_int, reject n <= 0 (ArgumentError), and
  # stream lazily so a break stops iteration early and unbounded sources work.
  # With no block they return a sized Enumerator (size is nil unless the receiver
  # answers #size); with a block they return self.
  def each_slice(n)
    n = __enum_int_arg(n)
    raise ArgumentError, "invalid slice size" if n <= 0
    unless block_given?
      return enum_for(:each_slice, n) {
        (sz = size if respond_to?(:size)) ? (sz + n - 1) / n : nil
      }
    end
    buf = []
    __each_packed { |x|
      buf << x
      if buf.length == n
        yield(buf)
        buf = []
      end
    }
    yield(buf) unless buf.empty?
    self
  end

  def each_cons(n)
    n = __enum_int_arg(n)
    raise ArgumentError, "invalid size" if n <= 0
    unless block_given?
      return enum_for(:each_cons, n) {
        if sz = (size if respond_to?(:size))
          sz - n + 1 < 0 ? 0 : sz - n + 1
        end
      }
    end
    window = []
    __each_packed { |x|
      window << x
      window.shift if window.length > n
      yield(window.dup) if window.length == n
    }
    self
  end

  # first / take return the leading elements, stopping iteration as soon as
  # enough have been gathered (ThrowingEach and lazy sources must not be fully
  # consumed). first with no argument returns the single leading element (or
  # nil); with a count both return an Array. drop returns everything after the
  # first n. Counts coerce through #to_int and reject negatives, like MRI.
  def first(*args)
    raise ArgumentError, "wrong number of arguments (given #{args.length}, expected 0..1)" if args.length > 1
    if args.empty?
      result = nil
      catch(:__enum_first) do
        __each_packed { |x| result = x; throw :__enum_first }
      end
      return result
    end
    n = __enum_int_arg(args[0])
    raise ArgumentError, "attempt to take negative size" if n < 0
    return [] if n == 0
    r = []
    catch(:__enum_first) do
      __each_packed { |x|
        r << x
        throw :__enum_first if r.length >= n
      }
    end
    r
  end

  def take(n)
    n = __enum_int_arg(n)
    raise ArgumentError, "attempt to take negative size" if n < 0
    return [] if n == 0
    r = []
    catch(:__enum_take) do
      __each_packed { |x|
        r << x
        throw :__enum_take if r.length >= n
      }
    end
    r
  end

  def drop(n)
    n = __enum_int_arg(n)
    raise ArgumentError, "attempt to drop negative size" if n < 0
    r = []
    i = 0
    __each_packed { |x|
      r << x if i >= n
      i = i + 1
    }
    r
  end

  # chunk_while / slice_when split the element stream into runs at each adjacent
  # pair (i, j) for which the block does not hold (chunk_while) / does hold
  # (slice_when). Both require a block (ArgumentError otherwise), call it exactly
  # length-1 times, and return an Enumerator of the runs (MRI's is lazy; the
  # materialised values match). A single element yields one run of that element.
  def chunk_while
    raise ArgumentError, "tried to create Proc object without a block" unless block_given?
    a = to_a
    return [].to_enum(:each) if a.empty?
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
    chunks.to_enum(:each)
  end

  def slice_when
    raise ArgumentError, "tried to create Proc object without a block" unless block_given?
    a = to_a
    return [].to_enum(:each) if a.empty?
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
    chunks.to_enum(:each)
  end

  # chunk groups consecutive elements sharing the block's value into
  # [value, [elements...]] runs, returning an Enumerator of those pairs (its
  # #size is nil). A block value of nil or :_separator drops the element and
  # breaks the current run; :_alone puts its element in a chunk of its own; any
  # other Symbol beginning with an underscore is reserved (RuntimeError). With no
  # block it returns an Enumerator whose #with_index (etc.) supplies the block.
  def chunk
    return enum_for(:chunk) { nil } unless block_given?
    result = []
    open = false # is there a run the current key may extend?
    __each_packed { |x|
      k = yield(x)
      if k.nil? || k == :_separator
        open = false
      elsif k == :_alone
        result << [k, [x]]
        open = false
      else
        if k.is_a?(Symbol) && k.to_s.start_with?("_")
          raise RuntimeError, "symbols beginning with an underscore are reserved"
        end
        if open && result[-1][0] == k
          result[-1][1] << x
        else
          result << [k, [x]]
          open = true
        end
      end
    }
    result.to_enum(:each) { nil }
  end

  # slice_before / slice_after split the element stream into runs, starting a new
  # run just before (slice_before) / just after (slice_after) each element that
  # matches. The boundary test is either a block over the element or `pat === x`
  # for a single pattern argument — never both, and exactly one must be given
  # (ArgumentError otherwise). Both return an Enumerator of the runs (its #size is
  # nil) and gather multi-value yields into whole Arrays. Empty runs are never
  # produced, even when the first or last element matches.
  def slice_before(*args)
    raise ArgumentError, "wrong number of arguments (given #{args.length}, expected 1)" if args.length > 1
    if block_given?
      raise ArgumentError, "both pattern and block are given" unless args.empty?
    else
      raise ArgumentError, "wrong number of arguments (given 0, expected 1)" if args.empty?
      pat = args[0]
    end
    runs = []
    cur = nil
    __each_packed { |x|
      if block_given? ? yield(x) : (pat === x)
        runs << cur unless cur.nil?
        cur = [x]
      else
        cur = [] if cur.nil?
        cur << x
      end
    }
    runs << cur unless cur.nil?
    runs.to_enum(:each) { nil }
  end

  def slice_after(*args)
    raise ArgumentError, "wrong number of arguments (given #{args.length}, expected 1)" if args.length > 1
    if block_given?
      raise ArgumentError, "both pattern and block are given" unless args.empty?
    else
      raise ArgumentError, "wrong number of arguments (given 0, expected 1)" if args.empty?
      pat = args[0]
    end
    runs = []
    cur = []
    __each_packed { |x|
      cur << x
      if block_given? ? yield(x) : (pat === x)
        runs << cur
        cur = []
      end
    }
    runs << cur unless cur.empty?
    runs.to_enum(:each) { nil }
  end

  # minmax_by returns [min, max] compared by the block's mapped value (an empty
  # collection gives [nil, nil]); with no block it returns an Enumerator.
  def minmax_by
    return enum_for(:minmax_by) unless block_given?
    [min_by { |x| yield(x) }, max_by { |x| yield(x) }]
  end

  # cycle(n) yields every element n times (forever when n is nil — use break to
  # stop). With no block it returns an Enumerator (finite only when n is given).
  def cycle(n = nil)
    unless block_given?
      # Give the Enumerator a size so #size does not materialise an endless cycle:
      # unknown when the receiver has no #size, Float::INFINITY for an unbounded
      # cycle of a non-empty receiver, else size * n (0 for an empty receiver or a
      # non-positive n). Mirrors MRI's enum_cycle_size.
      return enum_for(:cycle, n) do
        sz = respond_to?(:size) ? size : nil
        if sz.nil? || sz == 0
          sz
        elsif n.nil?
          Float::INFINITY
        else
          n <= 0 ? 0 : sz * n
        end
      end
    end
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

# Set — MRI's collection of unique members with set algebra. The membership,
# insertion order, compare_by_identity flag and the core mutators/accessors are
# native primitives (internal/vm/set.go, over an object.Hash of member => true);
# this reopening layers the rest of the API on top exactly as MRI's set.rb layers
# it over a Hash: the Enumerable mix-in, the algebra and predicate operators (each
# accepting any Enumerable), the higher-order methods and the genuine aliases.
class Set
  include Enumerable

  # __do_with_enum yields each element of an Enumerable through #each_entry when it
  # provides one (MRI's preferred protocol, which spec mocks rely on) else #each,
  # raising ArgumentError for a value that is neither — the check every seeding and
  # algebra method funnels through.
  def __do_with_enum(enum, &block)
    if enum.respond_to?(:each_entry)
      enum.each_entry(&block)
    elsif enum.respond_to?(:each)
      enum.each(&block)
    else
      raise ArgumentError, "value must be enumerable"
    end
  end
  private :__do_with_enum

  # initialize(enum = nil) seeds the (already-allocated, empty) Set from an
  # Enumerable, preprocessing each element through the block when one is given.
  def initialize(enum = nil, &block)
    return if enum.nil?
    if block
      __do_with_enum(enum) { |o| add(block.call(o)) }
    else
      __do_with_enum(enum) { |o| add(o) }
    end
  end
  private :initialize

  # each yields every member and returns self; without a block it returns an
  # Enumerator, so Set is a full Enumerable and `set.each` is chainable.
  def each(&block)
    return enum_for(:each) { size } unless block
    __each(&block)
    self
  end

  def empty?
    size == 0
  end

  # add? / delete? are the query mutators: like #add / #delete but returning nil
  # when the member was already present / already absent (no change), else self.
  def add?(o)
    include?(o) ? nil : add(o)
  end

  def delete?(o)
    include?(o) ? delete(o) : nil
  end

  # | / union / + : a new Set with the members of self and the given Enumerable.
  # Retains self's compare_by_identity flag (dup carries it).
  def |(other)
    n = dup
    __do_with_enum(other) { |o| n.add(o) }
    n
  end
  alias union |
  alias + |

  # & / intersection: a new (plain, non-identity) Set of the members shared by
  # self and the given Enumerable.
  def &(other)
    n = self.class.new
    __do_with_enum(other) { |o| n.add(o) if include?(o) }
    n
  end
  alias intersection &

  # - / difference: a new Set of self's members excluding those in the Enumerable.
  # Retains self's compare_by_identity flag.
  def -(other)
    n = dup
    __do_with_enum(other) { |o| n.delete(o) }
    n
  end
  alias difference -

  # ^ : symmetric difference — a new (plain) Set of the members in exactly one of
  # self and the Enumerable.
  def ^(other)
    n = self.class.new(other)
    each { |o| n.include?(o) ? n.delete(o) : n.add(o) }
    n
  end

  # merge(*enums): add every element of each Enumerable to self, returning self.
  def merge(*enums)
    enums.each { |enum| __do_with_enum(enum) { |o| add(o) } }
    self
  end

  # subtract(enum): remove every element of the Enumerable from self, return self.
  def subtract(other)
    __do_with_enum(other) { |o| delete(o) }
    self
  end

  # replace(other): make self hold exactly other's members. A Set argument
  # transfers its compare_by_identity flag; any other Enumerable leaves self's flag
  # untouched.
  def replace(other)
    if other.is_a?(Set)
      __replace_cbi(other.compare_by_identity?)
      other.each { |o| add(o) }
    else
      raise ArgumentError, "value must be enumerable" unless other.respond_to?(:each_entry) || other.respond_to?(:each)
      __replace_cbi(compare_by_identity?)
      __do_with_enum(other) { |o| add(o) }
    end
    self
  end

  # reset rehashes the members (call after mutating a member in place), returns
  # self.
  def reset
    vals = to_a
    __replace_cbi(compare_by_identity?)
    vals.each { |o| add(o) }
    self
  end

  # subset? / <= : self ⊆ other (other must be a Set-like object).
  def subset?(other)
    raise ArgumentError, "value must be a set" unless other.is_a?(Set)
    return false if size > other.size
    all? { |o| other.include?(o) }
  end
  alias <= subset?

  # proper_subset? / < : self ⊂ other (strict).
  def proper_subset?(other)
    raise ArgumentError, "value must be a set" unless other.is_a?(Set)
    return false if size >= other.size
    all? { |o| other.include?(o) }
  end
  alias < proper_subset?

  # superset? / >= : self ⊇ other.
  def superset?(other)
    raise ArgumentError, "value must be a set" unless other.is_a?(Set)
    return false if size < other.size
    other.all? { |o| include?(o) }
  end
  alias >= superset?

  # proper_superset? / > : self ⊃ other (strict).
  def proper_superset?(other)
    raise ArgumentError, "value must be a set" unless other.is_a?(Set)
    return false if size <= other.size
    other.all? { |o| include?(o) }
  end
  alias > proper_superset?

  # <=> : 0 when equal, -1 when a proper subset, +1 when a proper superset, nil
  # when the sets are incomparable or other is not Set-like.
  def <=>(other)
    return nil unless other.is_a?(Set)
    case size <=> other.size
    when -1 then subset?(other) ? -1 : nil
    when 1 then superset?(other) ? 1 : nil
    else self == other ? 0 : nil
    end
  end

  # == : true when other is a Set-like object with the same members and the same
  # compare_by_identity flag. eql? is the same test.
  def ==(other)
    return true if equal?(other)
    return false unless other.is_a?(Set)
    if other.respond_to?(:compare_by_identity?) && compare_by_identity? != other.compare_by_identity?
      return false
    end
    return false unless size == other.size
    all? { |o| other.include?(o) }
  end
  alias eql? ==

  # hash : equal for equal Sets, order-independent (the members' hashes are sorted
  # so ordering does not matter, then hashed as an Array).
  def hash
    to_a.map(&:hash).sort.hash
  end

  # disjoint? / intersect? : whether self and other share no / at least one member
  # (other may be any Set-like object).
  def disjoint?(other)
    if size < other.size
      none? { |o| other.include?(o) }
    else
      other.none? { |o| include?(o) }
    end
  end

  def intersect?(other)
    !disjoint?(other)
  end

  # delete_if / keep_if : remove members for which the block is truthy / falsy,
  # always returning self (an Enumerator without a block).
  def delete_if(&block)
    return enum_for(:delete_if) { size } unless block
    to_a.each { |o| delete(o) if block.call(o) }
    self
  end

  def keep_if(&block)
    return enum_for(:keep_if) { size } unless block
    to_a.each { |o| delete(o) unless block.call(o) }
    self
  end

  # select! / filter! / reject! : like keep_if / delete_if but returning nil when
  # nothing changed (an Enumerator without a block).
  def select!(&block)
    return enum_for(:select!) { size } unless block
    n = size
    keep_if(&block)
    size == n ? nil : self
  end
  alias filter! select!

  def reject!(&block)
    return enum_for(:reject!) { size } unless block
    n = size
    delete_if(&block)
    size == n ? nil : self
  end

  # map! / collect! : replace each member with the block's result, returning self.
  # Does not retain the compare_by_identity flag (the rebuilt members are new).
  def map!(&block)
    return enum_for(:map!) { size } unless block
    vals = to_a.map(&block)
    __replace_cbi(false)
    vals.each { |o| add(o) }
    self
  end
  alias collect! map!

  # classify(&block): a Hash mapping each block result to the Set of members that
  # produced it (an Enumerator without a block).
  def classify(&block)
    return enum_for(:classify) { size } unless block
    h = {}
    each do |o|
      k = block.call(o)
      (h[k] ||= self.class.new) << o
    end
    h
  end

  # divide(&block): partition self into a Set of subsets. With a 1-argument block
  # each subset shares a block value; with a 2-argument block the block is a
  # relation and the subsets are the strongly-connected components of the induced
  # digraph (an Enumerator without a block).
  def divide(&func)
    return enum_for(:divide) { size } unless func
    if func.arity == 2
      __divide_graph(&func)
    else
      self.class.new(classify(&func).values)
    end
  end

  def __divide_graph(&func)
    items = to_a
    n = items.size
    adj = Array.new(n) { [] }
    n.times do |i|
      n.times do |j|
        adj[i] << j if func.call(items[i], items[j])
      end
    end
    idx = 0
    indices = Array.new(n)
    low = Array.new(n, 0)
    onstack = Array.new(n, false)
    stack = []
    comps = []
    connect = nil
    connect = lambda do |v|
      indices[v] = idx
      low[v] = idx
      idx += 1
      stack.push(v)
      onstack[v] = true
      adj[v].each do |w|
        if indices[w].nil?
          connect.call(w)
          low[v] = low[w] if low[w] < low[v]
        elsif onstack[w]
          low[v] = indices[w] if indices[w] < low[v]
        end
      end
      if low[v] == indices[v]
        comp = []
        loop do
          w = stack.pop
          onstack[w] = false
          comp << items[w]
          break if w == v
        end
        comps << comp
      end
    end
    n.times { |v| connect.call(v) if indices[v].nil? }
    self.class.new(comps.map { |c| self.class.new(c) })
  end
  private :__divide_graph

  # flatten : a new (plain) Set with every nested Set recursively expanded.
  # flatten! flattens self in place, returning self when it changed and nil when
  # it did not. Both raise ArgumentError on a Set that (transitively) contains
  # itself.
  def flatten
    self.class.new.flatten_merge(self, {})
  end

  def flatten!
    if any? { |o| o.is_a?(Set) }
      replace(flatten)
      self
    else
      nil
    end
  end

  # flatten_merge(other, seen) merges other into self, expanding nested Sets and
  # raising ArgumentError when it revisits a Set already on the current path.
  def flatten_merge(other, seen = {})
    other.each do |o|
      if o.is_a?(Set)
        raise ArgumentError, "tried to flatten recursive Set" if seen[o.object_id]
        seen[o.object_id] = true
        flatten_merge(o, seen)
        seen.delete(o.object_id)
      else
        add(o)
      end
    end
    self
  end
  protected :flatten_merge

  # join : Array#join over the members (in insertion order).
  def join(sep = nil)
    to_a.join(sep)
  end

  # to_set returns self (a Set is already a Set).
  def to_set
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
# assignment. The ordered Symbol=>value table and the DATA methods (to_h /
# inspect / dig / delete_field / == / eql?) are backed by
# github.com/go-ruby-ostruct/ostruct through the native __data_* class helpers
# registered on this class (registerOstruct), matching MRI 4.0.5. This Ruby side
# keeps @table as the source of truth and the dynamic accessor GLUE: reads/writes
# go through method_missing (and respond_to_missing?), and bracket access mirrors
# them.
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
    OpenStruct.__data_to_h(@table)
  end

  def each_pair
    return enum_for(:each_pair) unless block_given?
    @table.each_pair { |k, v| yield(k, v) }
    self
  end

  def members
    @table.keys
  end

  def dig(*names)
    OpenStruct.__data_dig(@table, *names)
  end

  def delete_field(name)
    OpenStruct.__data_delete_field(@table, name.to_sym)
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
    OpenStruct.__data_eq(@table, other.is_a?(OpenStruct) ? other.to_h : nil)
  end
  alias eql? ==

  def respond_to?(name, include_private = false)
    respond_to_missing?(name, include_private) || super
  end

  def inspect
    OpenStruct.__data_inspect(@table)
  end
  alias to_s inspect
end

# Benchmark (require "benchmark") is now a native module backed by
# github.com/go-ruby-benchmark/benchmark (see internal/vm/benchmark.go): it owns
# the Tms value, its arithmetic and %-extension formatting, and the bm/bmbm
# report layout, with rbgo injecting the clock. It was previously implemented
# here in pure Ruby.

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

  # The lexical (no-I/O) path algebra — absolute?/cleanpath/basename/dirname/
  # extname/+/join/split/each_filename/ascend/descend/sub_ext/relative_path_from —
  # is backed by github.com/go-ruby-pathname/pathname through the native __lex_*
  # class helpers registered on this class (registerPathname), matching MRI 4.0.5.
  # This Ruby side only wraps the string results back into Pathname objects and
  # keeps @path, Comparable and the filesystem delegations.
  def absolute?
    Pathname.__lex_absolute?(@path)
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
    Pathname.new(Pathname.__lex_plus(@path, other.to_s))
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
    Pathname.new(Pathname.__lex_basename(@path, suffix))
  end

  def dirname
    Pathname.new(Pathname.__lex_dirname(@path))
  end
  alias parent dirname

  def extname
    Pathname.__lex_extname(@path)
  end

  def split
    [dirname, basename]
  end

  def each_filename
    return enum_for(:each_filename) unless block_given?
    Pathname.__lex_filenames(@path).each { |f| yield f }
  end

  # ascend yields the path then each parent up to the root (or the first relative
  # component), like MRI's Pathname#ascend. descend is the same sequence reversed.
  def ascend
    return enum_for(:ascend) unless block_given?
    Pathname.__lex_ascend_paths(@path).each { |p| yield Pathname.new(p) }
    self
  end

  def descend
    return enum_for(:descend) unless block_given?
    Pathname.__lex_ascend_paths(@path).reverse_each { |p| yield Pathname.new(p) }
    self
  end

  # cleanpath collapses "." and ".." components and redundant separators.
  def cleanpath
    Pathname.new(Pathname.__lex_cleanpath(@path))
  end

  def sub_ext(repl)
    Pathname.new(Pathname.__lex_sub_ext(@path, repl))
  end

  # relative_path_from returns self expressed relative to base_directory, using
  # only the lexical components (no filesystem access), matching MRI. Mixing an
  # absolute path with a relative one — or a ".." that escapes a relative base —
  # raises ArgumentError, as in MRI.
  def relative_path_from(base_directory)
    base_directory = Pathname.new(base_directory) unless base_directory.is_a?(Pathname)
    Pathname.new(Pathname.__lex_relative_path_from(@path, base_directory.to_s))
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

# Find (require "find") — top-down traversal of a set of file paths — is provided
# natively (internal/vm/find.go), backed by github.com/go-ruby-find/find. The
# module is installed on the first `require "find"`, mirroring MRI's lib/find.rb.

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
# Slim (require "slim"): the Slim template engine (the `slim` gem). Slim compiles
# an indentation-structured template to Ruby source that, evaluated against a set
# of locals, renders the same HTML the gem produces. The scan/compile lives in the
# go-ruby-slim library, reached through the native Slim::Template#__compile (slim.go);
# the eval — which runs the compiled source's embedded Ruby (`=` expressions, `-`
# control, interpolation) — stays here because it needs a Binding, mirroring ERB.
# The compiled source references the runtime helpers Slim::Helpers.escape_html /
# .render_attribute / .render_attributes; those are defined below (the go-ruby-slim
# reference implementations) so the rendered HTML comes from one authoritative
# source. Slim::Template / Slim::Engine are created natively; this reopens
# Slim::Template to add the public compile-to-eval API.
# ---------------------------------------------------------------------------
module Slim
  module Helpers
    # Safe wraps a value the compiled source marked HTML-safe (an "attr==expr"
    # unescaped attribute) so render_attributes skips escaping it.
    class Safe
      attr_reader :value
      def initialize(v)
        @value = v
      end
    end

    def self.safe(v)
      Safe.new(v)
    end

    # escape_html mirrors Temple::Utils.escape_html, the escaper Slim uses for "="
    # output and interpolated text: the five-character HTML entity table (' becomes
    # &#39;, '/' is left untouched).
    def self.escape_html(s)
      s.to_s.gsub(/[&<>"']/, '&' => '&amp;', '<' => '&lt;', '>' => '&gt;',
                  '"' => '&quot;', "'" => '&#39;')
    end

    # render_attribute renders a single dynamic attribute the way Slim does: a
    # nil/false value is omitted (""), a true value emits name="" (a boolean
    # attribute), and any other value emits name="escaped". A Safe-wrapped value is
    # left unescaped.
    def self.render_attribute(name, value)
      return '' if value.nil? || value == false
      return %( #{name}="") if value == true
      return %( #{name}="#{value.value}") if value.is_a?(Safe)

      %( #{name}="#{escape_html(value)}")
    end

    # render_attributes renders a merged attribute hash the way Slim does: class
    # values merged with spaces, a single id, boolean attributes (a true value)
    # emitted as name="", nil/false values omitted, every pair sorted
    # alphabetically, string values HTML-escaped (Safe-wrapped values left as-is).
    # Trailing hashes are splat sources merged on top of the base hash.
    def self.render_attributes(base, *splats)
      merged = {}
      add = lambda do |k, v|
        k = k.to_s
        if k == 'class'
          existing = merged['class']
          val = v.is_a?(Array) ? v.join(' ') : v
          merged['class'] = [existing, val].compact.reject { |x| x == '' }.join(' ')
        elsif k == 'id'
          merged['id'] = v
        else
          merged[k] = v
        end
      end
      base.each { |k, v| add.call(k, v) }
      splats.each { |h| (h || {}).each { |k, v| add.call(k, v) } }

      out = +''
      merged.keys.sort.each do |k|
        out << render_attribute(k, merged[k])
      end
      out
    end
  end

  # Slim::Template compiles a template to Ruby source (via the native __compile,
  # slim.go) and renders it by eval'ing that source in a binding carrying the
  # supplied locals — the same compile→eval seam ERB uses. new accepts the template
  # either as the first positional argument or as a block returning the source (the
  # gem's Slim::Template.new { source } form).
  class Template
    attr_reader :src

    # new(template = nil, &block) compiles the template source (the positional
    # argument, or the block's return value) into evaluable Ruby source.
    def initialize(template = nil, &block)
      template = block.call if template.nil? && block
      @src = __compile(template.to_s)
    end

    # render(scope = Object.new, locals = {}) evaluates the compiled source with the
    # entries of locals bound as local variables, so the template can reference them
    # by name. scope is accepted for gem compatibility (the object the template body
    # would run against) but the compiled Slim source only reads the bound locals.
    def render(_scope = Object.new, locals = {})
      b = binding
      locals.each { |k, v| b.local_variable_set(k, v) }
      eval(@src, b, '(slim)', 0)
    end
  end

  Engine = Template
end

# ---------------------------------------------------------------------------
# Haml (require "haml"): the Haml template engine (the `haml` gem), the same
# compile→eval design as Slim/ERB. The scan/compile lives in the go-ruby-haml
# library, reached through the native Haml::Template#__compile (haml.go); the eval
# stays here because it needs a Binding. The compiled source references the runtime
# helpers Haml::Util.escape_html and Haml::HamlAttributes.render; those are defined
# below (the go-ruby-haml reference implementations). Haml::Template / Haml::Engine
# are created natively; this reopens Haml::Template to add the public API.
# ---------------------------------------------------------------------------
module Haml
  module Util
    # escape_html mirrors Haml::Util.escape_html: the five-character HTML entity
    # table (' becomes &#39;).
    def self.escape_html(s)
      s.to_s.gsub(/[&<>"']/, '&' => '&amp;', '<' => '&lt;', '>' => '&gt;',
                  '"' => '&quot;', "'" => '&#39;')
    end
  end

  # HamlAttributes.render renders a dynamic attribute hash the way Haml does: class
  # values merged with spaces, id values merged with "_", data hashes expanded to
  # data-<k>, boolean attributes emitted bare when truthy and omitted when
  # nil/false, and every pair sorted alphabetically with escaped values.
  module HamlAttributes
    BOOL = %w[disabled readonly multiple checked selected hidden required async
              defer novalidate autofocus open reversed ismap muted
              controls loop autoplay].freeze

    def self.render(h)
      pairs = {}
      h.each do |k, v|
        k = k.to_s
        if k == 'data' && v.is_a?(Hash)
          v.each { |dk, dv| pairs["data-#{dk}"] = dv }
        elsif k == 'class'
          existing = pairs['class']
          merged = [existing, (v.is_a?(Array) ? v.join(' ') : v)].compact.join(' ')
          pairs['class'] = merged
        elsif k == 'id'
          existing = pairs['id']
          pairs['id'] = [existing, v].compact.join('_')
        else
          pairs[k] = v
        end
      end
      out = +''
      pairs.keys.sort.each do |k|
        v = pairs[k]
        if BOOL.include?(k)
          out << " #{k}" if v && v != false
        else
          next if v.nil?

          out << %( #{k}="#{Haml::Util.escape_html(v.to_s)}")
        end
      end
      out
    end
  end

  # Haml::Template compiles a Haml template to Ruby source (native __compile,
  # haml.go) and renders it by eval'ing that source in a binding carrying the
  # supplied locals — the same compile→eval seam ERB/Slim use.
  class Template
    attr_reader :src

    # new(template = nil, &block) compiles the template source (the positional
    # argument, or the block's return value) into evaluable Ruby source.
    def initialize(template = nil, &block)
      template = block.call if template.nil? && block
      @src = __compile(template.to_s)
    end

    # render(scope = Object.new, locals = {}) evaluates the compiled source with the
    # entries of locals bound as local variables. scope is accepted for gem
    # compatibility; the compiled Haml source reads only the bound locals.
    def render(_scope = Object.new, locals = {})
      b = binding
      locals.each { |k, v| b.local_variable_set(k, v) }
      eval(@src, b, '(haml)', 0)
    end
  end

  Engine = Template
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
