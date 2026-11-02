# A BasicObject does not include Kernel, so it has NO #respond_to? at all —
# and MRI's conversion path copes: vm_respond_to opens with `if (!cme) return
# -1`, where -1 means UNDECIDED rather than false, so rb_check_funcall falls
# through to a plain method lookup.
#
# ⛔ `findMethod(v, "respond_to?") != Kernel's entry` is TRUE for nil, which is
# how rbgo came to send #respond_to? to an object that has none. Two of these
# four shapes were broken on rbgo's main through the masgn (#to_ary) path with
# no spec covering them; the splat (#to_a) ones are asserted by
# language/array_spec.rb.
#
# MRI 2.6.10 answers: all four "no error" (188 bytes of stderr).
def shape(label)
  yield
  STDERR.puts "#{label.ljust(34)} -> no error"
rescue NoMethodError => e
  STDERR.puts "#{label.ljust(34)} -> NoMethodError: #{e.message}"
rescue => e
  STDERR.puts "#{label.ljust(34)} -> #{e.class}: #{e.message}"
end

shape("splat a bare BasicObject") { a = *BasicObject.new }

klass = Class.new(BasicObject) do
  def to_a
    [1, 2]
  end
end
shape("splat a BasicObject with to_a") { a = *klass.new }

shape("masgn a bare BasicObject") { a, b = BasicObject.new }

aryklass = Class.new(BasicObject) do
  def to_ary
    [1, 2]
  end
end
shape("masgn a BasicObject with to_ary") { a, b = aryklass.new }
