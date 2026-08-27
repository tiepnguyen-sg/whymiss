# Kurtosis devnet

The devnet `tools/faultinjector` and `test/e2e` run against. Three participants —
one Lighthouse+Geth and two Prysm+Geth — matching BUILD_PROMPT §3's "initial
client support: Lighthouse and Prysm only." Validator ranges follow the node
order: `0-31` on node 1, `32-63` on node 2, `64-95` on node 3.

The third node is topology, not client coverage. R-110 (`network.late_block`)
needs the watched node and an independent baseline node to agree that a block
arrived late, and a consensus client records no gossip arrival for a block it
produced itself. With two nodes the proposer is always one of the two observers,
so one of the two measurements is always missing and that cause could never be
reproduced. With three, a scenario can put the proposer outside both
measurements — see `scenarios/network-late-block.yaml`.

## Bring it up

```sh
make devnet.up      # launch the enclave (takes a few minutes: image pulls + genesis)
make devnet.info     # list service endpoints
make devnet.down     # tear it down
```

Set `DEVNET_ENCLAVE=<name>` on every target to use an isolated enclave name.

`kurtosis cluster set docker` first if `kurtosis engine status` complains about a
Kubernetes backend — see below.

## Things that will bite you

**`network_params.network` must be the literal string `"kurtosis"`.** Any other
value routes into the package's "fetch a pre-built public devnet's genesis from a
remote repo" code path instead of generating genesis locally, and crashes with an
out-of-range index because the name doesn't match that path's
`{category}-devnet-{number}` convention. This cost real debugging time — do not
"fix" it by renaming the network again. The enclave name (`--enclave` /
`whymiss-devnet`) is a separate, unrelated identifier and can be anything.

**Fulu (PeerDAS) is on by default from genesis** in the upstream package
(`fulu_fork_epoch: 0`), and refuses to start without a supernode or 128+
validators per node. whymiss cares about attestation/proposer timing, not
PeerDAS, so `network_params.yaml` pins `fulu_fork_epoch` far in the future. BPO
(blob parameter override) forks require Fulu, so `bpo_1_epoch` and `bpo_2_epoch`
(the two the package defaults to `0`) had to be pushed out too, or genesis
validation fails with a explicit message naming the conflict.

**Kurtosis's cluster config may already point at Kubernetes.** If this machine
has another project's Kubernetes-backed Kurtosis cluster configured (`kurtosis
cluster ls` shows more than just `docker`), `kurtosis engine start` fails trying
to reach it. `kurtosis cluster set docker` switches the active cluster without
touching the other one.

## Reaching a container's cgroup / host network namespace (Docker Desktop for Mac)

Docker Desktop runs containers inside a Linux VM, not directly on macOS. A
container's own `/sys/fs/cgroup` is mounted read-only from inside it, so the
development-only fault injector uses a digest-pinned, short-lived privileged
helper to enter the VM host namespaces. It uses this path for CPU and memory
cgroup limits and for `tc netem` on the container's VM-side veth:

```sh
docker run --rm --privileged --pid=host alpine@sha256:<pinned> sh -c \
  'nsenter -t 1 -m -u -n -i sh -c "<command>"'
```

Native Linux uses the host cgroup filesystem and veth directly and therefore
requires root (or the equivalent capabilities). `cgroup_io` remains native-
Linux-only; the current release corpus does not use it.

The opt-in Docker Desktop integration tests prove both paths against a live
devnet and verify rollback: `TestNetemFaultAgainstDockerDesktopDevnet` measures
the injected latency, and `TestCgroupFaultAgainstDockerDesktopDevnet` reads the
changed `cpu.max` and its restored value, including rollback after the apply
context is canceled. Run `make test.faults.darwin
DEVNET_ENCLAVE=whymiss-release`.

`make devnet.up` first builds a devnet-only Lighthouse validator image with
libfaketime preloaded. The `clock_skew` fault updates its offset file live,
verifies that PID 1 loaded the library, and restores the exact original file.
The Prysm images are static Go binaries, which libfaketime cannot intercept, so
clock-skew recipes must target `vc-1-geth-lighthouse`. Run
`make test.faults.clock` to prove the live apply and rollback path independently
of a duty slot.

## Validator index ranges

`num_validator_keys_per_node: 32` means validators `0-31` are keyed to the first
participant (Lighthouse) and `32-63` to the second (Prysm) — confirmed via
`kurtosis enclave inspect`'s files-artifact names
(`1-lighthouse-geth-0-31`, `2-prysm-geth-32-63`). A fault that affects an entire
node's validator client container (e.g. `pause`) affects every validator in that
range at once — see `Scenario.AvoidProposerValidators` in
`tools/faultinjector/scenario.go` for why a scenario watching one specific
validator's attester duty needs to avoid slots where the same range also holds
the proposer duty, or the recorded outcome gets confounded with
`network.proposer_missed`.

## Release corpus campaign

Regenerate the 15 canonical recipes first, then collect 35 additional live
records. Every additional record re-runs its source recipe against a newly
selected slot and validator; `recipe_id` in its manifest preserves that
provenance. No fixture is copied or relabelled.

```sh
make corpus.generate.all \
  DEVNET_ENCLAVE=whymiss-release \
  NTP_SERVER=time.cloudflare.com
make corpus.generate.campaign \
  DEVNET_ENCLAVE=whymiss-release \
  NTP_SERVER=time.cloudflare.com
make corpus.validate
make eval
make eval.check
```

The fixed campaign contains exactly 50 live records: seven for each of six
positive causes and eight adversarial `unknown` records. Runs are serial so a
fault is never contaminated by another active fault. A failed record is kept
for diagnosis and reported at the end; it is not counted as a passing fixture.

Before starting, measure the chosen fixed NTP server repeatedly. The injector
also samples before the duty and after the full inclusion window and rejects an
absolute offset above 100 ms. Do not disable or loosen that gate: synchronize
the host clock, then rerun the failed record with `make corpus.generate` and a
unique `RECORD_ID` if needed.
