# Stage 4b (data-backed via the ORM) — ActiveRecord + sqlite3 + ERB.
# The idiomatic Rails-style data route: an ActiveRecord model over an in-memory
# SQLite database, queried with the AR query interface and rendered through ERB.
# The Go harness only runs this file when `active_record` is loadable; otherwise
# it records the exact gap. This is the app that must go green once the
# go-ruby-activerecord binding lands (PR #102, bind/batch7).
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
