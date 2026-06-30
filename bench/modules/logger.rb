# logger module benchmark: format many log records to an in-memory StringIO
# (NOT a file — keeps it CPU-bound on the formatting/severity path, not disk).
# rbgo binds this to go-ruby-logger. Deterministic: a fixed message stream and a
# fixed (frozen) datetime_format, so the output is byte-identical across runtimes
# (we strip the volatile timestamp by using a constant formatter). Prints the
# size of the produced log buffer.
require "logger"
require "stringio"

N = (ENV["N"] || "90000").to_i

buf = StringIO.new
log = Logger.new(buf)
log.level = Logger::DEBUG
# Deterministic formatter: no wall-clock timestamp, only severity + message,
# so the produced buffer is identical on every runtime.
log.formatter = proc { |sev, _time, _prog, msg| "#{sev[0]} #{msg}\n" }

N.times do |i|
  case i % 4
  when 0 then log.debug("debug message #{i % 100}")
  when 1 then log.info { "info message #{i % 100}" }
  when 2 then log.warn("warn message #{i % 100}")
  else        log.error("error message #{i % 100}")
  end
end

puts buf.string.bytesize
