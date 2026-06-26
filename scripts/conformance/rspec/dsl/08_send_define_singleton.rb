# rspec-mocks stubs methods by defining singleton methods on objects at runtime
# and dispatching via __send__/public_send. Reproduce stub/expect-message.
class Stubber
  def self.stub(obj, name, value)
    obj.define_singleton_method(name) { value }
  end
end

class Plain; end
p = Plain.new
Stubber.stub(p, :greeting, "stubbed")
puts p.greeting            # stubbed
puts p.__send__(:greeting) # stubbed
puts p.public_send(:greeting) # stubbed
puts p.respond_to?(:greeting) # true
