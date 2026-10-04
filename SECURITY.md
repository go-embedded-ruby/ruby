# Security policy

## Reporting

Please report anything you believe is a security issue through GitHub's private
vulnerability reporting, from the **Security** tab of this repository
("Report a vulnerability"). That keeps the report out of the public issue
tracker until there is something to say about it.

This is a small project without a security team, so please do not expect a
same-day answer. What you can expect is that a report will be read, that the
reply will say plainly whether it is in scope, and that if a fix lands the
report will be credited unless you ask otherwise.

If private reporting is unavailable to you for any reason, open a normal issue
describing the *shape* of the problem without a working trigger, and say that you
have more to share.

## What is in scope

`rbgo` aims at MRI-Ruby conformance and is also a Go library (`ruby.Run`) that
other programs embed. Both of those shape what counts.

In scope:

- **The host process dying, or hanging without bound, because of Ruby input.**
  A Go `panic` that reaches an embedder, a `fatal error:` that `recover()` cannot
  contain, an unbounded allocation, or a call that never returns. `Run` returns
  an `error`; anything that takes the process down instead is a defect, because
  an embedder has no way to defend against it from the outside.
- **Failing to raise where MRI raises**, when the difference is what a program
  relies on to stay safe. A check that reads as "passed" because a limit was hit,
  an allow-list that permits more than it says, a `rescue` that cannot fire
  because the exception class is never raised. These fail *open*, which is worse
  than failing loudly, and they are not visible to the caller.
- **Reaching further than the documented capability set**, including any path
  from Ruby to the embedding Go program's own state, or writing outside the
  `io.Writer` the embedder passed.
- Secrets or credentials committed to this repository, and anything in the
  release or CI path that would let someone else's code run with this
  repository's permissions.

## What is not a vulnerability

**`Run` is not a sandbox, and that is documented, not accidental.** See
[*`Run` is not a sandbox*](README.md#run-is-not-a-sandbox) in the README, which
carries the measurements. Ruby source passed to `Run` can spawn `/bin/sh`, read
and write the filesystem as the host's uid, open sockets, `require` any absolute
path, and change the host's `ENV` and working directory — exactly as MRI can,
because conformance is the goal. Do not pass untrusted Ruby to it.

So a report that "`system("...")` executes a command" is working as intended. A
report that a *documented restriction* does not hold is not.

By the same reasoning, faithfully reproducing an MRI behaviour that is dangerous
in MRI — `Marshal.load` on attacker-controlled bytes, for instance — is
conformance rather than a defect here. It is still worth telling us if the
documentation does not warn about it.

## Already known

Please check the open issues before reporting; several classes are already filed
with measurements, and a duplicate costs you more than it costs us:

- no call-depth limit, so runaway recursion is a fatal Go stack overflow (#768,
  and go-ruby-parser/parser#48 for the same thing in the parser)
- `Net::HTTP` to a server in the program's own `Thread` hangs without bound (#771)
- `Run` returns `nil` for `exit`/`exit!`/`abort` (#773)
- `YAML.safe_load` instantiates arbitrary classes (#775)
- `Regexp.timeout=` is never applied, and a fired limit reads as "no match" (#776)
- `Array#*`/`String#*` reach `make()` with a rejected size (#777)

## Supported versions

There is no released version yet and no backport branch: fixes land on `main`.
If you are embedding this, pin a commit and expect to move it.
