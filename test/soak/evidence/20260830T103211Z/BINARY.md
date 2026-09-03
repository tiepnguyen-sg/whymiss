# Binary under soak

sha256: 99210b69c79b73f2245066db9afe0102804e832c5a62dd98429a40787967cf6d
version: v0.2.1-9-gf0bcbb3
commit: f0bcbb3c1f142e8f09154444e31d480704abd7cf (clean tree, not -dirty)

Candidate for v0.3.0. Verified against a fresh local `make build.all` on the
host itself before the run started.

Carries the post-ePBS collection fix, ADR-0026 schedule adoption, and ADR-0027
network.payload_late with rule R-120. Against this gateway all three are inert —
Hoodi has Gloas unscheduled, so the node-derived schedule is exactly
domain.MainnetPreEPBS() and R-120 declines on every duty. That inertness is
itself the property worth soaking: none of it may change behaviour on a pre-fork
network.
