# pinned-vc-slow-cpu

A byte-for-byte copy of `test/corpus/vc-slow-cpu/observations.jsonl` as it stood
on 2026-08-26, kept here so the two tests that assert exact values against it —
observation count, slot, validator index, inclusion delay — cannot be broken by
regenerating that corpus scenario.

They were broken exactly that way once: regenerating `vc-slow-cpu` during the
corpus-growth effort silently changed all four values, and the failures looked
like a defect in `LoadObservations`/`Replay` rather than what they were, a
fixture moving underneath a test. A corpus scenario is expected to be
regenerated; a test that pins exact values from one needs its own copy.

This is real recorded devnet data, not a hand-written response — copying it is
allowed where authoring one would not be (`AGENTS.md`: never hand-write a
beacon-node response, record a real one). It is not corpus content: it carries
no `manifest.yaml`, is not evaluated by `tools/eval`, and `corpusctl validate`
never sees it.

`TestReplay_ByteIdenticalAcrossRuns` deliberately still reads the live
`test/corpus/`, because it asserts determinism over whatever scenarios exist
rather than any scenario's exact content, and should cover new ones the moment
they land.
