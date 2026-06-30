# did_you_mean module benchmark: spell-checker edit-distance suggestion over a
# dictionary, repeated. rbgo binds this to go-ruby-did-you-mean. Uses the public
# DidYouMean::SpellChecker API. Deterministic: prints a suggestion-count checksum.
require "did_you_mean"

N = (ENV["N"] || "4000").to_i

dictionary = %w[initialize inspect instance_variable_get instance_variable_set
                instance_of object_id obj_id define_method method_missing
                respond_to_missing public_send protected_methods]
checker = DidYouMean::SpellChecker.new(dictionary: dictionary)

acc = 0
N.times do
  acc += checker.correct("instnace_varieble_get").length
  acc += checker.correct("respnd_to_mising").length
  acc += checker.correct("objct_id").length
end
puts acc
