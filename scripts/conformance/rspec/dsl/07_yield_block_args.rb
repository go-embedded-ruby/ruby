# RSpec hooks (before/after) and `it` blocks receive the example via yield with
# block args; matchers like `change { }` capture and re-invoke blocks. Exercise
# explicit block params, block.call, and yield with arguments.
def with_example
  ex = { name: "spec", run: 0 }
  yield ex
  ex
end

result = with_example do |ex|
  ex[:run] += 1
end
puts result[:run]   # 1

# change-matcher style: capture a block, call before/after.
def change(&probe)
  before = probe.call
  ->(action) {
    action.call
    after = probe.call
    [before, after]
  }
end

counter = [0]
delta = change { counter[0] }
b, a = delta.call(-> { counter[0] += 5 })
puts b   # 0
puts a   # 5
