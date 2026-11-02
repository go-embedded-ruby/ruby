# Oracle probes

Questions put to the reference interpreter, with its answers written down.

```sh
go run ./scripts/conformance/rubyspec/cmd/oracle \
  -script scripts/conformance/rubyspec/cmd/oracle/testdata/probes/globals.rb \
  ./rbgo-main ./rbgo-branch /usr/bin/ruby
```

Each file is one question, each row inside it one shape, and the header comment
records what MRI answered and which upstream function decides. They are kept
because **the answer is the expensive part** — the shapes take a minute to write
and the reference run takes a second, but working out *which* function in MRI
owns the rule, and which rows distinguish the plausible-but-wrong
implementations from the right one, is where the time goes. A probe that is
thrown away is that work thrown away: three of these were written, lost to a
reboot, and written again.

| probe | question | the row that matters |
|---|---|---|
| `globals.rb` | when does reading a global warn? | silence after `$x = nil` — the subject is "never assigned", not "reads as nil" |
| `constant_names.rb` | which first characters start a constant? | the **titlecase** digraphs are constants, the **caseless** letters are not, so "not lowercase" is wrong in both directions |
| `dynamic_constant.rb` | where is a constant assignment refused? | `Class.new { X = 1 }` in a `def` is refused but `define_method(:m) { X = 1 }` at class level is not — the rule is syntactic, not about execution |
| `basicobject_conversion.rb` | how is a conversion reached on an object outside Kernel? | a BasicObject has no `#respond_to?` at all, which is not the same as having an overridden one |

## Reading a disagreement

`oracle` compares **bytes**, not lengths, and the reason is in these probes'
history: a branch that had just gained the uninitialized-global warning reported
`stderr 1008 vs 1008 bytes` against MRI — the same length, differing only in the
line number and the quote style. A comparison by size would have called that
parity.

Two differences against the 2.6.10 interpreter on this machine are **expected**
and are not rbgo defects:

- **the quote style.** 2.6 writes ``global variable `$x' not initialized``;
  3.4 and later write `'$x'`. ruby/spec matches on `` [`'] `` for exactly this
  reason.
- **the line number of a warning.** 2.6 reports the line BEFORE the read when
  the read is a simple statement, and the right line when it is an argument to a
  call. rbgo reports the true line in all three shapes. Replicating a stale line
  is not parity worth having, and no spec asserts the position prefix.

## What this reference interpreter cannot answer

`/usr/bin/ruby` on macOS is **2.6.10**. It predates numbered parameters (2.7),
`Set` as a core class, `Range#overlap?` and everything else 3.x–4.0 added, so a
probe about those gets silence rather than an answer — which reads exactly like
a negative. Say which case you are in: a probe whose rule is older than 2.6 (the
constant and dynamic-constant rules here) has a valid oracle; one whose rule is
newer does not, and the spec file is then the authority.
