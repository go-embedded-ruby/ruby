# RSpec matchers lean on method_missing for dynamic predicate matchers
# (be_empty -> calls empty? on the subject). Reproduce the be_<predicate>
# dispatch via method_missing + respond_to_missing?.
class BePredicate
  def initialize(subject)
    @subject = subject
  end

  def method_missing(name, *args)
    str = name.to_s
    if str.start_with?("be_")
      pred = (str[3..-1] + "?").to_sym
      @subject.send(pred)
    else
      super
    end
  end

  def respond_to_missing?(name, include_private = false)
    name.to_s.start_with?("be_") || super
  end
end

m = BePredicate.new([])
puts m.be_empty          # true
puts m.respond_to?(:be_empty)  # true
m2 = BePredicate.new([1])
puts m2.be_empty         # false
