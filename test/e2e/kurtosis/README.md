# Kurtosis devnet

The devnet `tools/faultinjector` and `test/e2e` run against. Two participants —
one Lighthouse+Geth, one Prysm+Geth — matching BUILD_PROMPT §3's "initial client
support: Lighthouse and Prysm only."

## Bring it up

```sh
make devnet.up      # launch the enclave (takes a few minutes: image pulls + genesis)
make devnet.info     # list service endpoints
make devnet.down     # tear it down
```

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
container's own `/sys/fs/cgroup` is mounted read-only from inside it (correctly —
delegating writes to a container's own resource limits would defeat the point),
so `tools/faultinjector`'s cgroup_io fault reaches the VM's real cgroup
filesystem via a short-lived privileged helper:

```sh
docker run --rm --privileged --pid=host alpine sh -c \
  'apk add --no-cache util-linux; nsenter -t 1 -m -u -n -i sh -c "<command>"'
```

This also works unmodified on a native Linux host — it just has less to reach
through there.

**Network faults (tc netem) do not work this way on Docker Desktop for Mac.**
Its networking is not a plain Linux bridge+veth topology, so host-side-veth-based
`tc netem` (the standard technique for unprivileged Docker containers) could not
be made to actually delay traffic when prototyped there. It works cleanly on
native Linux: verified end to end on a GCP `e2-standard-4` Ubuntu 22.04 VM —
`hostVethFor` resolved the correct host veth from the container's own
`eth0@if<N>` naming, and `tc qdisc add ... netem delay 300ms` measurably added
~300ms of round-trip latency, reverting cleanly. Run
`tools/faultinjector/fault_netem_verify_test.go`'s
`TestNetemFaultAgainstRealDevnet` (root, `WHYMISS_NETEM_INTEGRATION=1`) to
reproduce. `clock_skew` remains unimplemented for an unrelated, platform-
independent reason — see `fault_clock.go`'s doc comment.

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
