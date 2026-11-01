# Where is a constant assignment refused? MRI's rule is purely SYNTACTIC: an
# enclosing `def` keyword, reached through any number of blocks, stopped by a
# class or module body or the top level.
#
# MRI 2.6.10 refuses the first ten with `dynamic constant assignment`, except
# `class I5` inside a def which is `class definition in method body` — a
# different message from a different check. It allows all six below `-- allowed`.
#
# ⛔ Two rows are the ones that show the rule is not "will this run as a
# method?": `Class.new { AA = 1 }` inside a def IS refused, while
# `define_method(:m) { AA = 1 }` at class level is NOT. Reasoning from execution
# gets both backwards. A third: a SCOPED assignment `C8::AA8 = 1` is refused
# inside a def too — being explicit about the namespace does not buy it.
[
  ["def body",                      "def d1; AA1 = 1; end"],
  ["block inside a def",            "def d2; [1].each { AA2 = 1 }; end"],
  ["nested block inside a def",     "def d3; [1].each { [2].each { AA3 = 1 } }; end"],
  ["lambda inside a def",           "def d4; -> { AA4 = 1 }; end"],
  ["class body inside a def",       "def d5; class I5; AA5 = 1; end; end"],
  ["Class.new block inside a def",  "def d6; Class.new { AA6 = 1 }; end"],
  ["define_method at class level",  "class C7; define_method(:m) { AA7 = 1 }; end"],
  ["scoped assign inside a def",    "class C8; def m; C8::AA8 = 1; end; end"],
  ["op-assign inside a def",        "def d9; AA9 ||= 1; end"],
  ["masgn inside a def",            "def d10; AA10, x = 1, 2; end"],
  ["singleton def body",            "class S1; def self.m; AC1 = 1; end; end"],
  ["def inside class << self",      "class S2; class << self; def m; AC2 = 1; end; end; end"],
  ["const in class << self body",   "class S3; class << self; AC3 = 1; end; end"],
  ["def on an object",              "o = Object.new; def o.m; AC4 = 1; end"],
  ["module_eval block",             "class S5; end; S5.module_eval { AC5 = 1 }"],
  ["instance_eval block in def",    "def d6b; Object.new.instance_eval { AC6 = 1 }; end"],
  ["const in a def's rescue",       "def d7; raise; rescue; AC7 = 1; end"],
  ["const in a def's ensure",       "def d8; ensure; AC8 = 1; end"],
  ["const in a def default arg",    "def d9b(x = (AC9 = 1)); end"],
  ["nested def",                    "def d10b; def inner; AC10 = 1; end; end"],
  ["-- allowed --",                 "nil"],
  ["top level",                     "AB1 = 1"],
  ["class body",                    "class C12; AB2 = 1; end"],
  ["module body",                   "module M13; AB3 = 1; end"],
  ["block at top level",            "[1].each { AB4 = 1 }"],
  ["block in a class body",         "class C14; [1].each { AB5 = 1 }; end"],
  ["singleton def body (no const)", "class C15; class << self; def m; end; end; end"],
].each do |label, src|
  begin
    eval(src)
    STDERR.puts "#{label.ljust(30)} -> NO ERROR"
  rescue SyntaxError => e
    STDERR.puts "#{label.ljust(30)} -> SyntaxError: #{e.message.lines.first.to_s.strip}"
  rescue => e
    STDERR.puts "#{label.ljust(30)} -> #{e.class}: #{e.message.lines.first.to_s.strip}"
  end
end
