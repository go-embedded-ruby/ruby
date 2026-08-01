# CI architecture

rbgo is **pure-Go, CGO=0, with no hand-written assembly**, so architecture-specific
risk is low: a bug that reproduces on one 64-bit target virtually always reproduces
on `amd64` and `arm64` too. CI is layered to exploit that — a fast native gate on
every PR, and slower exotic-arch validation moved off the per-PR critical path.

## 1. Per-PR / push gate — `ci.yml` (blocking)

Runs on every `pull_request` and `push` to `main`. This is the only workflow that
gates merges:

| Job          | Runners                                   | What it enforces                                   |
|--------------|-------------------------------------------|----------------------------------------------------|
| `test`       | ubuntu-latest, macos-latest, windows-latest | `go vet` + `go build` + **`-race` + 100 % coverage** gate |
| `arch-native`| ubuntu-latest (amd64), ubuntu-24.04-arm (arm64) | `go test ./...` on both **native** GitHub arches |

The four exotic 64-bit targets — `riscv64`, `loong64`, `ppc64le`, `s390x` — are **no
longer emulated on every PR**. They previously ran under `qemu-user` (~50–100× slower
than native, ~10 min/lane) and were the CI bottleneck. Their coverage is preserved by
the two scheduled workflows below.

## 2. Nightly QEMU fallback — `arch-qemu-nightly.yml`

`schedule` (03:17 UTC daily) + `workflow_dispatch`, **never** on PR/push. Runs the
exact former per-PR QEMU matrix (`riscv64`/`loong64`/`ppc64le`/`s390x`) under
`qemu-user-static`. This keeps exotic-arch coverage green **even before** the
real-hardware secrets below are configured, and remains a cheap emulated safety net
afterwards. No secrets required.

## 3. Real-hardware validation — `native-arch.yml` (secret-gated, inert by default)

`schedule` (04:42 UTC daily) + `workflow_dispatch`, **never** on PR/push. SSHes into
real silicon and runs the suite **natively** (no emulation).

This aligns with the ecosystem-wide real-HW convention documented in
[go-asmgen's `toolkit/bench-hw/README.md`](https://github.com/go-asmgen/asmgen/blob/main/toolkit/bench-hw/README.md)
— the authoritative inventory of what hardware is actually reachable — and
reuses go-simd's `z15-bench.yml` SSH pattern (and its `LINUX1_*` secret names)
for the s390x lane rather than inventing a separate one:

| Arch     | Host                         | Where                                              |
|----------|------------------------------|-----------------------------------------------------|
| ppc64le  | `cfarm433.cfarm.net`         | GCC Compile Farm — IBM POWER10                       |
| riscv64  | `cfarm95.cfarm.net`          | GCC Compile Farm — SpacemiT X60 (RVV 1.0)             |
| s390x    | `148.100.85.193` (`linux1@`) | IBM LinuxONE Community Cloud (direct, **not** cfarm) |

**loong64 has no real-HW lane here.** cfarm400/401 — the only Loongson nodes cfarm
ever had — have been down for roughly three years, so there is no free CI-able
loong64 silicon on the Compile Farm. Per `bench-hw/README.md` the two live options
are the Debian LoongArch porter box (`shenzhou.debian.net`, needs Debian-maintainer
standing) or a self-hosted runner built from the
[github.com/loong64](https://github.com/loong64) org's images — neither is wired
up yet. Until one is, loong64 correctness coverage comes exclusively from the
QEMU nightly (`arch-qemu-nightly.yml` above).

On each host it ensures a recent Go (uses the system `go` if new enough, else
downloads `go$GO_VERSION.linux-$GOARCH.tar.gz`), clones the triggering commit, and runs
`GOWORK=off CGO_ENABLED=0 go vet ./... && go test ./...`. The `s390x` lane is the
real **big-endian** check for the binary codecs.

To retarget nodes, edit only the `env:` block and the `matrix.include` tables at the top
of `native-arch.yml`.

### Acceptable-use policy (important)

The **GCC Compile Farm is a shared academic resource**. Its AUP **forbids** using it as
a per-PR CI backend hammered on every push. This workflow is therefore **scheduled once
per day, off-peak, with single-run concurrency**, and is manually dispatchable — never
triggered on `pull_request`/`push`. Please keep it courteous. Account: `delavennat`
(request access at <https://portal.cfarm.net/>). The LinuxONE host is a dedicated VM,
not shared, but stays on the same schedule-only cadence for consistency.

### Activation — repo secrets to add

`native-arch.yml` is **inert until the relevant secrets exist**. A `guard` job reads
each pair into a boolean output, and the cfarm lane (`native-cfarm`) and the s390x
lane (`native-s390x`) gate on them **independently** — either can be activated without
the other. (The `secrets` context is unavailable in job/step `if:` conditions, which is
why the guard-job pattern is used instead of `if: ${{ secrets.CFARM_SSH_KEY != '' }}`.)

| Secret               | Lane  | Required | Purpose                                                                                              |
|----------------------|-------|----------|--------------------------------------------------------------------------------------------------------|
| `CFARM_SSH_KEY`      | cfarm | yes      | Private SSH key registered on the cfarm account `delavennat`. OpenSSH/PEM, no passphrase. Covers ppc64le + riscv64 — cfarm is one SSH account for every machine. |
| `CFARM_KNOWN_HOSTS`  | cfarm | yes      | `ssh-keyscan` output pinning cfarm433 + cfarm95 (and the gateway, if used) — no auto-accept of unknown keys. |
| `CFARM_GATEWAY`      | cfarm | no       | `user@bastion[:port]` jump host for the cfarm nodes, if your account reaches them via a gateway. Leave unset for direct access. |
| `LINUX1_SSH_KEY`     | s390x | yes      | Private SSH key authorized on the IBM LinuxONE Community Cloud host `linux1@148.100.85.193`. Same secret name as go-simd's `z15-bench.yml` — reuse the existing key rather than minting a new one. |
| `LINUX1_KNOWN_HOSTS` | s390x | yes      | `ssh-keyscan` output for `148.100.85.193`. |

```sh
# cfarm lane (ppc64le + riscv64) — private key on the cfarm account (delavennat):
gh secret set CFARM_SSH_KEY < ~/.ssh/id_cfarm

# Pin cfarm host keys (include the gateway host too if you set CFARM_GATEWAY):
ssh-keyscan cfarm433.cfarm.net cfarm95.cfarm.net | gh secret set CFARM_KNOWN_HOSTS

# Optional bastion for the cfarm nodes (omit entirely for direct access):
printf 'delavennat@gateway.cfarm.net' | gh secret set CFARM_GATEWAY

# s390x lane — the SAME key already authorized on linux1@148.100.85.193 for
# go-simd's z15-bench.yml (reuse it, do not mint a new key):
gh secret set LINUX1_SSH_KEY < ~/.ssh/id_linux1

# Pin the LinuxONE host key:
ssh-keyscan 148.100.85.193 | gh secret set LINUX1_KNOWN_HOSTS
```

Trigger a run on demand from the Actions tab (**Run workflow**) or:

```sh
gh workflow run native-arch.yml
```
