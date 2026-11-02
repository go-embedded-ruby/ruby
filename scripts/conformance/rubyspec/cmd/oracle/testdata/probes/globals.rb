# Uninitialized-global warning: rb_gvar_undef_getter (variable.c:649 at v4.0.9).
#
# $VERBOSE is set here rather than through -w so the comparison does not depend
# on a flag either engine may spell differently -- and because
# `complain(verbose: true)` is how language/variables_spec.rb arranges it.
# Setting it at runtime also keeps MRI's compile-time void-context warnings out
# of the output.
#
# MRI 2.6.10 answers (1008 bytes of stderr): silence for every predefined name,
# ONE warning for the never-assigned name, silence after an explicit `= nil`,
# silence in a rescue body, TWO warnings for a name read twice, and silence
# while $VERBOSE is false.
$VERBOSE = true

STDERR.puts "-- interpreter-defined names (expect SILENCE) --"
$; ; $\ ; $, ; $. ; $_ ; $* ; $" ; $: ; $?
$! ; $@ ; $~ ; $& ; $` ; $' ; $+ ; $1
$0 ; $$ ; $stdout ; $stderr ; $stdin ; $> ; $<
$/ ; $-0 ; $DEBUG ; $VERBOSE ; $-W ; $-a ; $-l ; $-p
$LOAD_PATH ; $LOADED_FEATURES ; $PROGRAM_NAME ; $FILENAME
STDERR.puts "-- end --"

STDERR.puts "-- never assigned (expect ONE warning naming it) --"
$specs_uninitialized_global_variable
STDERR.puts "-- end --"

STDERR.puts "-- assigned nil, then read (expect SILENCE) --"
$assigned_to_nil = nil
$assigned_to_nil
STDERR.puts "-- end --"

STDERR.puts "-- a rescue body (expect SILENCE: the errinfo cell is not a global) --"
begin
  raise "x"
rescue => e
  e.message
end
STDERR.puts "-- end --"

STDERR.puts "-- read twice (MRI warns EACH time, the getter is still undef) --"
$read_twice
$read_twice
STDERR.puts "-- end --"

STDERR.puts "-- silent when $VERBOSE is false (rb_warning, not rb_warn) --"
$VERBOSE = false
$never_assigned_while_quiet
$VERBOSE = true
STDERR.puts "-- end --"
