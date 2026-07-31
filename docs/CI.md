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
real silicon and runs the suite **natively** (no emulation):

| Arch     | Host                         | Where                                         |
|----------|------------------------------|-----------------------------------------------|
| ppc64le  | `cfarm135.cfarm.net`         | GCC Compile Farm — IBM POWER9 9006-22P         |
| riscv64  | `cfarm94.cfarm.net`          | GCC Compile Farm — StarFive VisionFive 2 (JH7110) |
| loong64  | `cfarm400.cfarm.net`         | GCC Compile Farm — Loongson-3C5000L-LL         |
| s390x    | `148.100.85.193` (`linux1@`) | LinuxONE community cloud (direct, **not** cfarm) |

On each host it ensures a recent Go (uses the system `go` if new enough, else
downloads `go$GO_VERSION.linux-$GOARCH.tar.gz`), clones the triggering commit, and runs
`GOWORK=off CGO_ENABLED=0 go vet ./... && go test ./...`. The `s390x` lane is the
real **big-endian** check for the binary codecs.

To retarget nodes, edit only the `env:` block and the `matrix.include` table at the top
of `native-arch.yml`.

### Acceptable-use policy (important)

The **GCC Compile Farm is a shared academic resource**. Its AUP **forbids** using it as
a per-PR CI backend hammered on every push. This workflow is therefore **scheduled once
per day, off-peak, with single-run concurrency**, and is manually dispatchable — never
triggered on `pull_request`/`push`. Please keep it courteous. Account: `delavennat`
(request access at <https://portal.cfarm.net/>).

### Activation — repo secrets to add

`native-arch.yml` is **inert until these secrets exist**. A `guard` job reads
`CFARM_SSH_KEY` into a boolean output and every real-HW job gates on it, so with no
secrets configured the workflow simply **skips (green), never red**. (The `secrets`
context is unavailable in job/step `if:` conditions, which is why the guard-job pattern
is used instead of `if: ${{ secrets.CFARM_SSH_KEY != '' }}`.)

| Secret              | Required | Purpose                                                                                              |
|---------------------|----------|------------------------------------------------------------------------------------------------------|
| `CFARM_SSH_KEY`     | yes      | Private SSH key registered on the cfarm account `delavennat` **and** authorized on the s390x host. OpenSSH/PEM, no passphrase. |
| `CFARM_KNOWN_HOSTS` | yes      | `ssh-keyscan` output pinning every host (and the gateway, if used) — no auto-accept of unknown keys. |
| `CFARM_GATEWAY`     | no       | `user@bastion[:port]` jump host for the cfarm nodes, if your account reaches them via a gateway. Leave unset for direct access. The s390x host is always contacted directly. |

```sh
# Private key authorized on cfarm (delavennat) + the s390x host:
gh secret set CFARM_SSH_KEY     < ~/.ssh/id_cfarm

# Pin host keys (include the gateway host too if you set CFARM_GATEWAY):
ssh-keyscan cfarm135.cfarm.net cfarm94.cfarm.net cfarm400.cfarm.net 148.100.85.193 \
  | gh secret set CFARM_KNOWN_HOSTS

# Optional bastion for the cfarm nodes (omit entirely for direct access):
printf 'delavennat@gateway.cfarm.net' | gh secret set CFARM_GATEWAY
```

Trigger a run on demand from the Actions tab (**Run workflow**) or:

```sh
gh workflow run native-arch.yml
```
