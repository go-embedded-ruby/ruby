# Stage 4b (data-backed via the ORM) — ActiveRecord + sqlite3 + ERB.
# The idiomatic Rails-style data route: an ActiveRecord model over an in-memory
# SQLite database, queried with the AR query interface and rendered through ERB.
# The Go harness only runs this file when the AR ORM chain it uses actually
# works (it probes establish_connection + Schema.define + create! +
# where(...).order.to_a first); otherwise it records the exact missing method.
# The go-ruby-activerecord binding landed (PR #102) but does not yet implement
# ActiveRecord::Schema.define, so this app is skipped-with-gap until it does.
#
#   GET /users?min=26  =>  200, "<ul><li>amy (30)</li><li>cat (40)</li></ul>"

require "active_record"
require "erb"

ActiveRecord::Base.establish_connection(adapter: "sqlite3", database: ":memory:")

ActiveRecord::Schema.define do
  create_table :users do |t|
    t.string  :name
    t.integer :age
  end
end

class User < ActiveRecord::Base
end

[["amy", 30], ["bob", 25], ["cat", 40]].each do |name, age|
  User.create!(name: name, age: age)
end

LIST = ERB.new(
  "<ul><% rows.each do |u| %><li><%= u.name %> (<%= u.age %>)</li><% end %></ul>"
)

app = ->(env) {
  min  = (env["QUERY_STRING"].to_s.split("=").last || "0").to_i
  rows = User.where("age >= ?", min).order(:name).to_a
  [200, { "content-type" => "text/html" }, [LIST.result(binding)]]
}

status, _, body = app.call(
  "PATH_INFO"      => "/users",
  "QUERY_STRING"   => "min=26",
  "REQUEST_METHOD" => "GET"
)

puts "status=#{status}"
puts "body=#{body.join}"
