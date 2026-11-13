# The coverage ratchet

CI said **`N function(s) dropped below 100% -- named above`** and you landed
here. This page says what that gate is, why it is not a percentage, and how to
answer it without a round trip through CI.

## What is gated

Not the total. The **set of functions below 100%**, recorded per lane in
`BELOW100.<lane>`, and that set may not grow.

A gate on the rounded total could not be calibrated. On `main` both POSIX lanes
report `coverage: 100.0%` and pass *while printing* 19 (ubuntu) and 16 (macOS)
functions below 100%, the lowest at 75.0%. The tolerance is whatever `%.1f`
absorbs at the current repository size: nobody chose it, nothing states it, and
it **shrinks as the codebase grows**. A function falling from 100% to 75% is
free until it is suddenly fatal, and the commit that trips the gate is not the
one that created the debt.

It was also arbitrary which lane you believed. On #727 the same commit gave
99.9% on ubuntu (FAIL) and 100.0% on macOS (pass): ubuntu carries three
filesystem-dependent functions macOS does not, so one further dead function
tipped only one lane over the edge.

Percentages are still *reported*. They are not gated, because coverage is
measured under `-race`, where a percentage jitters and a set does not — and a
gate that flakes is a gate people learn to rerun.

## Answering it without waiting 47 minutes for CI

The lane that keeps the record runs the whole suite. Taking a full profile
locally costs about the same, so the useful local question is narrower:

> the functions this commit **claims** to have covered — are they actually at
> 100%?

```sh
go test ./internal/vm/ -run 'FTP' -count=1 -coverprofile=/tmp/ftp.cov
go tool cover -func=/tmp/ftp.cov > /tmp/ftp.func
go run ./scripts/coverage/cmd/covratchet -profile /tmp/ftp.func \
    -require ftpApplyOpts,ftpTimeoutErr
```

```
required 2 function(s), matched 2 record(s) in a profile of 7347

all required functions at 100%
```

A selective `-run` is fine here: a function reaching 100% under *some* tests is
at 100% under all of them. `-require` takes bare names, or `file:name` when a
name occurs in more than one file.

### ⛔ Why this is a command and not a `grep`

**A name that matches nothing fails.** With `grep`, a typo, a renamed function
or a profile taken over the wrong package all print nothing — and nothing is
exactly what success looks like:

```sh
$ ... -require ftpApplyOpts,ftpTimeoutErrr      # one extra 'r'
required 2 function(s), matched 1 record(s) in a profile of 7347

-- NOT IN THIS PROFILE -- the name matched nothing (1) --
   ftpTimeoutErrr

::error::1 name(s) matched nothing; this profile cannot answer for them
```

This is the same failure mode as `go test -run` exiting `ok` on a pattern that
selected nothing. Every verdict therefore prints its **denominator** — how many
records matched, out of how big a profile — so a run that read nothing cannot be
mistaken for a clean one.

It exists because the ratchet, correct as it is, only refuses **after** the
spend. Twice in one day a commit written to answer it did not move the
measurement at all and came back red on the *same two functions*. A gate that
refuses after the spend teaches the lesson; one that refuses before it prevents
the mistake.

## When a function legitimately reaches 100%

The record is not updated by hand. On a green run of that lane:

```sh
UPDATE_COVERAGE=1 ...   # rewrites BELOW100.<lane>
```

Leaving a fixed function in the record is not harmless: a stale allowance is
where the next regression hides.

## Exit status

| code | meaning |
| --- | --- |
| 0 | no function newly below 100% (or, with `-require`, all of them at 100%) |
| 1 | a regression, or a required name that matched nothing |
| 2 | **refused to decide** — the profile or the record could not be read |

A profile with no `total:` line is refused rather than reported clean: a parser
that swallows what it does not understand turns a format change into a pass.
