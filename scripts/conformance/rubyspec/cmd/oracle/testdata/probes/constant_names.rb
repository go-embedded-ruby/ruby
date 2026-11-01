# Which first characters start a CONSTANT? rb_sym_constant_char_p
# (symbol.c:508-542 at v4.0.9) decides, and it has FIVE branches:
#
#   ASCII                     -> ISUPPER
#   non-ASCII, rb_enc_isupper -> constant
#   non-ASCII, rb_enc_islower -> local
#   titlecase letter, Unicode -> constant
#   anything else (caseless)  -> local
#
# parse.y does NOT decide this: it assumes tCONSTANT provisionally and
# downgrades with `if (result == tCONSTANT && is_local_id(ident)) result =
# tIDENTIFIER` (parse.y:10453), so the ID system is the authority.
#
# Object.const_defined? raises NameError for a name that is not a constant
# name, so it exposes rb_is_const_name (symbol.c:1287) directly.
#
# MRI 2.6.10 answers: A, Ἅ, Д, İ, ǅ, ǈ are CONSTANT names; a, _, μ, 日, א, ب
# are not. ⛔ The titlecase rows and the caseless rows are the two that make
# "not lowercase" the wrong shortcut, in opposite directions.
[
  ["ASCII upper   A",        "A"],
  ["ASCII lower   a",        "a"],
  ["ASCII under   _",        "_"],
  ["Lu  GREEK     Ἅ",   "Ἅ"],
  ["Lu  CYRILLIC  Д",   "Д"],
  ["Ll  GREEK mu  μ",   "μ"],
  ["Lt  DZ digr   ǅ",   "ǅ"],
  ["Lt  DZE digr  ǈ",   "ǈ"],
  ["Lo  CJK       日",   "日"],
  ["Lo  HEBREW    א",   "א"],
  ["Lo  ARABIC    ب",   "ب"],
  ["Lu  TURKISH I İ",   "İ"],
].each do |label, ch|
  name = "#{ch}Xyz"
  begin
    Object.const_defined?(name.to_sym)
    STDERR.puts "#{label} -> CONSTANT name"
  rescue NameError
    STDERR.puts "#{label} -> not a constant name"
  rescue => e
    STDERR.puts "#{label} -> #{e.class}: #{e.message}"
  end
end
