# RSpec shared_examples store named blocks in a registry and replay them inside
# a group via instance_exec (include_examples). Exercise a proc registry +
# replay with parameters.
module SharedRegistry
  @store = {}
  class << self
    def register(name, &block); @store[name] = block; end
    def fetch(name); @store[name]; end
  end
end

SharedRegistry.register("a collection") do |size|
  puts "size is #{size}"
end

class Host
  def run_shared(name, *args)
    block = SharedRegistry.fetch(name)
    instance_exec(*args, &block)
  end
end

Host.new.run_shared("a collection", 3)   # size is 3
