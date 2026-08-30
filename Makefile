# whymiss — the single interface every agent and human uses.
# If a workflow is not here, add it here. Do not run ad-hoc shell.

MODULE  := github.com/tiepnguyen-sg/whymiss
BIN     := whymiss
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
GOFLAGS := -trimpath
LDFLAGS := -s -w -X main.version=$(VERSION)

CORPUS_OUT          ?= ./test/corpus
CORPUS_DIR          ?= ./test/corpus
EVAL_OUT            ?= docs/evaluation.md
NTP_SERVER          ?= time.cloudflare.com
FAULTINJECTOR_PREFIX ?=
DEVNET_ENCLAVE       ?= whymiss-devnet
RECORD_ID             ?=

CORPUS_SCENARIOS := \
	cl-slow-cpu:cl-2-prysm-geth \
	cl-slow-cpu-lighthouse:cl-1-lighthouse-geth \
	p2p-degraded-lighthouse:cl-1-lighthouse-geth \
	p2p-degraded-prysm:cl-2-prysm-geth \
	p2p-ambiguous-no-baseline:cl-1-lighthouse-geth \
	p2p-ambiguous-no-baseline-prysm:cl-2-prysm-geth \
	proposer-missed-concurrent-vc-pause:cl-1-lighthouse-geth \
	proposer-missed-upstream:cl-1-lighthouse-geth \
	network-late-block:cl-1-lighthouse-geth \
	proposer-missed-concurrent-vc-pause-prysm:cl-2-prysm-geth \
	vc-frozen-lighthouse:cl-1-lighthouse-geth \
	vc-frozen-lighthouse-2:cl-1-lighthouse-geth \
	vc-frozen-prysm:cl-2-prysm-geth \
	vc-frozen-prysm-2:cl-2-prysm-geth \
	vc-slow-cpu:cl-1-lighthouse-geth \
	el-slow-cpu:cl-1-lighthouse-geth

# recipe:beacon:additional-live-record-count. The campaign was run and most of
# what it produced had to be thrown away: of 40 record directories on the devnet
# host, six were empty, and fourteen more were generated after the devnet's two
# consensus nodes silently stopped peering, which made every slot the other node
# proposed look skipped. Those fourteen are deleted; tools/faultinjector now
# refuses to record on a node with zero peers so the same run cannot happen
# twice. What survives is 20 scenarios. One further record
# (p2p-degraded-prysm-r02) produced a shape no rule covers: propagation 5.38s
# and validation 4.77s, neither dominant, with no Engine samples to separate
# them, so the engine correctly returns unknown.no_rule_matched. That record is
# a taxonomy-gap report, not a corpus expectation, and is left out rather than
# labelled with the gap it exposes.
#
# 20 of 50 leaves 30 to find, and they must not come from more rounds of these
# recipes: 6 of the 20 already expect unknown.*, and 8 of the 14 causes in
# docs/causes.md still have no scenario at all. More rounds of what works would
# raise the count while measuring less. host-memory-pressure and
# vc-slow-cpu-prysm remain absent for the same reason as before: their
# bisections never once reproduced their labelled phenomenon.
# Weighted toward the causes with the thinnest coverage rather than spread
# evenly. network.late_block and network.proposer_missed each hold exactly one
# record, and both only became reproducible on the three-node devnet, so they
# get the most rounds here; local.p2p_degraded already has eight and gets none.
# The point of a round is an independent recording of the same cause under a
# different draw of proposer, validator, and timing — not a bigger number.
# Rounds are weighted toward the recipes with the fewest records rather than the
# ones already well covered. Additional rounds are honest coverage only when they
# improve the weakest part of the corpus; used to substitute for a cause nobody
# has reproduced, they inflate the count while measuring less.
CORPUS_CAMPAIGN := \
	network-late-block:cl-1-lighthouse-geth:3 \
	proposer-missed-upstream:cl-1-lighthouse-geth:3 \
	vc-slow-cpu:cl-1-lighthouse-geth:3 \
	cl-slow-cpu:cl-2-prysm-geth:2 \
	cl-slow-cpu-lighthouse:cl-1-lighthouse-geth:2 \
	proposer-missed-concurrent-vc-pause:cl-1-lighthouse-geth:2 \
	proposer-missed-concurrent-vc-pause-prysm:cl-2-prysm-geth:1 \
	vc-frozen-lighthouse:cl-1-lighthouse-geth:1 \
	vc-frozen-prysm:cl-2-prysm-geth:1 \
	vc-frozen-prysm-2:cl-2-prysm-geth:1

export CGO_ENABLED := 0

.DEFAULT_GOAL := help

## help: list available targets
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## //' | column -t -s ':'

## build: build the binary for the host platform
build:
	@mkdir -p bin
	go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o bin/$(BIN) ./cmd/$(BIN)

## build.all: cross-compile for supported platforms (I-13)
build.all:
	@mkdir -p bin
	GOOS=linux GOARCH=amd64 go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o bin/$(BIN)-linux-amd64 ./cmd/$(BIN)
	GOOS=linux GOARCH=arm64 go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o bin/$(BIN)-linux-arm64 ./cmd/$(BIN)

## build.faultinjector: build the devnet-only fault-injection tool
build.faultinjector:
	@mkdir -p bin
	go build $(GOFLAGS) -o bin/faultinjector ./tools/faultinjector

## test: run unit tests with the race detector
# -race needs cgo. The global CGO_ENABLED=0 above is for I-13's shipped
# static binary (build/build.all) — override it back to 1 for this target
# only (verified on Linux: CGO_ENABLED=0 makes `go test -race` refuse to
# run at all with "requires cgo", not merely disable the detector).
test: export CGO_ENABLED := 1
test:
	go test -race -count=1 ./...

## test.golden: replay every test/corpus/* scenario against rca.Analyze and
## diff cause/sub_cause against manifest.yaml's expect: block (no snapshot
## file exists to regenerate — TestGolden_Corpus compares live against the
## manifest directly, so there is no -update flag to wire up)
test.golden:
	go test ./internal/rca/... -run TestGolden_Corpus -v

## test.faults.darwin: verify Docker Desktop netem/cgroup apply and rollback against the devnet
test.faults.darwin:
	@test "$$(uname -s)" = "Darwin" || (echo "test.faults.darwin requires Docker Desktop on macOS" && exit 1)
	WHYMISS_NETEM_INTEGRATION=1 \
	WHYMISS_NETEM_TARGET_CONTAINER=cl-1-lighthouse-geth \
	WHYMISS_NETEM_PEER_CONTAINER=cl-2-prysm-geth \
	WHYMISS_NETEM_TARGET_URL="$$(kurtosis port print "$(DEVNET_ENCLAVE)" cl-1-lighthouse-geth http)" \
	WHYMISS_CGROUP_INTEGRATION=1 \
	WHYMISS_CGROUP_TARGET_CONTAINER=validator-key-generation-cl-validator-keystore \
	WHYMISS_CGROUP_MEM_INTEGRATION=1 \
	WHYMISS_CGROUP_MEM_TARGET_CONTAINER=el-1-geth-lighthouse \
	go test -count=1 ./tools/faultinjector -run 'Test(NetemFault|Cgroup.*)AgainstDockerDesktopDevnet$$'

## test.faults.clock: prove live libfaketime apply and exact rollback
test.faults.clock: devnet.image
	@set -eu; \
	target="whymiss-clock-test-$$$$"; \
	container="$$target--case"; \
	cleanup() { docker rm --force "$$container" >/dev/null 2>&1 || true; }; \
	trap cleanup EXIT INT TERM; \
	docker run --detach --rm --name "$$container" whymiss/lighthouse-faketime:local \
		sh -c 'while :; do sleep 60; done' >/dev/null; \
	WHYMISS_CLOCK_INTEGRATION=1 WHYMISS_CLOCK_TARGET_CONTAINER="$$target" \
		go test -count=1 ./tools/faultinjector -run '^TestClockSkewAgainstPreloadedContainer$$'

## test.freshinstall: build and verify the isolated Docker Compose quickstart
test.freshinstall:
	sh test/freshinstall/run.sh

## test.image: build both release container architectures without publishing
test.image:
	docker buildx build \
		--platform linux/amd64,linux/arm64 \
		--file deploy/docker/Dockerfile \
		--build-arg VERSION="$(VERSION)" \
		.

## test.soak: run the bounded-resource Hoodi soak (Linux; defaults to 72 hours)
test.soak: build
	sh test/soak/run.sh

## lint: run golangci-lint under the supported Linux target
# GOOS=linux is I-13's only real target, and
# tools/faultinjector/fault_netem_verify_test.go is linux-only-tagged —
# linting under the host OS silently skips it; see CHANGELOG.md.
lint:
	GOOS=linux golangci-lint run

## fmt: format all Go source
fmt:
	gofumpt -l -w .

## check.format: verify formatting without changing the worktree
#
# Prints the gofumpt version on failure. gofumpt v0.10.0 and v0.11.0 disagree
# about closing parens on multiline calls, so a green local run and a red CI run
# once meant nothing more than two different binaries on two different machines —
# `gofmt` from the same Go release considered the tree clean throughout. If this
# fails on a tree you did not touch, compare the version below against the pin in
# .github/workflows/ci.yml before running `make fmt` and reformatting 23 files.
check.format:
	@test -z "$$(gofumpt -l .)" || ( \
		gofumpt -l .; \
		echo "gofumpt: $$(gofumpt --version 2>&1) — CI pins the version in .github/workflows/ci.yml"; \
		echo "run: make fmt"; \
		exit 1)

## check.tidy: verify go.mod/go.sum without changing the worktree
check.tidy:
	go mod tidy -diff

## vuln: scan dependencies for known vulnerabilities
vuln:
	govulncheck ./...

## check: enforce invariants that lint cannot express
check: check.purity check.isolation check.egress check.globals check.nonroot check.placeholders check.workflows check.toolchain release.check

## check.globals: reject mutable package-level production state
check.globals:
	@matches=$$(grep -Rn '^var ' internal cmd tools --include='*.go' \
		| grep -v '_test.go' \
		| grep -vE '^cmd/whymiss/main.go:[0-9]+:var version = "dev"$$' || true); \
	if [ -n "$$matches" ]; then \
		echo "$$matches"; \
		echo "FAIL: mutable package-level production state"; \
		exit 1; \
	fi

## check.placeholders: reject unresolved public-release placeholders
check.placeholders:
	@placeholder=$$(printf 'CHANGE%s' 'ME'); \
	if git grep -n "$$placeholder" -- . ':!CHANGELOG.md'; then \
		echo "FAIL: replace public-release placeholders"; \
		exit 1; \
	fi

## check.workflows: validate GitHub Actions syntax and expressions
check.workflows:
	actionlint
	sh -n test/freshinstall/run.sh
	sh -n test/soak/run.sh

## check.toolchain: CI must build with the compiler go.mod's toolchain line names
#
# This exists because the two drifted silently and CI was red for three days
# before anyone read the reason. `go-version-file: go.mod` makes setup-go install
# the *language minimum* (the `go` line), not the `toolchain` line — it ignores
# the latter entirely — so the runner built with a compiler the release soak
# never exercised while `make ci` stayed green locally. Both halves are checked:
# no workflow may go back to reading the file, and every pinned version must be
# the toolchain go.mod names.
check.toolchain:
	@want=$$(sed -n 's/^toolchain go//p' go.mod); \
	if [ -z "$$want" ]; then \
		echo "FAIL: go.mod has no toolchain directive to check against"; \
		exit 1; \
	fi; \
	if grep -rn 'go-version-file:' .github/workflows/*.yml; then \
		echo "FAIL: setup-go reads the go directive from go.mod, not toolchain — pin go-version: \"$$want\" instead"; \
		exit 1; \
	fi; \
	bad=$$(grep -rn 'go-version:' .github/workflows/*.yml | grep -v "go-version: \"$$want\"" || true); \
	if [ -n "$$bad" ]; then \
		echo "$$bad"; \
		echo "FAIL: workflow Go version must be $$want, the toolchain go.mod names"; \
		exit 1; \
	fi

## release.check: validate the GoReleaser configuration
release.check:
	goreleaser check

# I-6: internal/rca must import only stdlib and internal/domain.
# I-6: internal/domain must import only stdlib.
# depguard in .golangci.yml is the primary gate; this is the belt-and-braces check.
check.purity:
	@echo ">> I-6 purity: internal/domain"
	@! grep -rE '^\s*"?[a-z0-9.-]+\.[a-z]{2,}/' --include='*.go' internal/domain \
		| grep -v '_test.go' \
		|| (echo "FAIL: internal/domain imports a third-party package" && exit 1)
	@echo ">> I-6 purity: internal/rca"
	@! grep -rE '^\s*"?[a-z0-9.-]+\.[a-z]{2,}/' --include='*.go' internal/rca \
		| grep -v '_test.go' \
		| grep -v '$(MODULE)/internal/domain' \
		| grep -v '$(MODULE)/internal/rca/' \
		|| (echo "FAIL: internal/rca imports outside stdlib + internal/domain" && exit 1)
	@echo "OK"

# I-11: no client-specific identifier outside internal/source/**
check.isolation:
	@echo ">> I-11 client isolation"
	@! grep -rniE 'lighthouse|prysm|teku|nimbus|lodestar|nethermind|erigon|besu|reth' \
		--include='*.go' internal cmd tools \
		| grep -v '^internal/source/' \
		| grep -v '_test.go' \
		| grep -vE ':[0-9]+:[[:space:]]*//' \
		|| (echo "FAIL: client name referenced outside internal/source/" && exit 1)
	@echo "OK"

# I-4: no outbound HTTP client construction outside adapters
check.egress:
	@echo ">> I-4 egress boundary"
	@! grep -rnE 'http\.(Get|Post|DefaultClient|NewRequest|NewRequestWithContext|Client)' --include='*.go' internal cmd \
		| grep -v '^internal/source/' \
		|| (echo "FAIL: outbound HTTP outside internal/source/" && exit 1)
	@echo "OK"

# I-3: the binary must run without root and without any Linux capability
# (BUILD_PROMPT §10.3 Phase 2 DoD: "runs as non-root, no capabilities,
# verified in CI"). --help is enough: it exercises the binary starting up
# and exiting cleanly without touching the network or the filesystem beyond
# reading its own flags, so this check works with no beacon node or store
# available, which is what CI has.
check.nonroot: build
	@echo ">> I-3 non-root, no capabilities"
	@if [ "$$(id -u)" = "0" ]; then \
		echo "FAIL: this check must not itself run as root — it proves the binary doesn't need to" && exit 1; \
	fi
	@if command -v getcap >/dev/null 2>&1; then \
		caps=$$(getcap bin/$(BIN) 2>/dev/null); \
		if [ -n "$$caps" ]; then echo "FAIL: binary carries Linux capabilities: $$caps" && exit 1; fi; \
	fi
	@./bin/$(BIN) --help >/dev/null
	@echo "OK"

## ci: the gate. Every task must pass this before being declared done.
ci: check.format check.tidy lint check test vuln build.all eval.check
	@echo "CI PASSED"

## release.snapshot: build unsigned local release archives, checksums, and SBOMs
release.snapshot:
	goreleaser release --snapshot --clean --skip=sign,publish
	@test "$$(find dist -maxdepth 1 -name '*_linux_amd64.tar.gz' | wc -l | tr -d ' ')" = 1
	@test "$$(find dist -maxdepth 1 -name '*_linux_arm64.tar.gz' | wc -l | tr -d ' ')" = 1
	@test -s dist/checksums.txt
	@test "$$(find dist -maxdepth 1 -name '*.sbom.json' | wc -l | tr -d ' ')" = 2
	@cd dist && if command -v sha256sum >/dev/null 2>&1; then sha256sum -c checksums.txt; else shasum -a 256 -c checksums.txt; fi

## devnet.image: build the devnet-only Lighthouse VC image with libfaketime
devnet.image:
	docker build \
		--file test/e2e/kurtosis/Dockerfile.lighthouse-faketime \
		--tag whymiss/lighthouse-faketime:local \
		test/e2e/kurtosis

## devnet.check: dry-run and validate the Kurtosis package configuration
devnet.check: devnet.image
	@set -u; \
	check_enclave="$(DEVNET_ENCLAVE)-check"; \
	cleanup() { kurtosis enclave rm --force "$$check_enclave" >/dev/null 2>&1; }; \
	trap 'cleanup || true' EXIT INT TERM; \
	run_status=0; \
	kurtosis run github.com/ethpandaops/ethereum-package \
		--args-file test/e2e/kurtosis/network_params.yaml \
		--enclave "$$check_enclave" \
		--dry-run \
		--verbosity output_only || run_status=$$?; \
	cleanup_status=0; cleanup || cleanup_status=$$?; \
	trap - EXIT INT TERM; \
	if [ "$$cleanup_status" -ne 0 ]; then \
		echo "failed to remove dry-run enclave $$check_enclave" >&2; \
		exit "$$cleanup_status"; \
	fi; \
	exit "$$run_status"

## devnet.up: start the Kurtosis Ethereum devnet faultinjector runs against
devnet.up: devnet.image
	kurtosis run github.com/ethpandaops/ethereum-package \
		--args-file test/e2e/kurtosis/network_params.yaml \
		--enclave "$(DEVNET_ENCLAVE)"

## devnet.down: tear down the devnet and free its resources
devnet.down:
	kurtosis enclave rm --force "$(DEVNET_ENCLAVE)"

## devnet.info: print the devnet's service endpoints as JSON
devnet.info:
	kurtosis enclave inspect "$(DEVNET_ENCLAVE)" --full-uuids

## corpus.validate: validate every labelled failure scenario
corpus.validate:
	go run ./tools/corpusctl validate "$(CORPUS_DIR)"

## corpus.generate: generate one scenario against the running devnet (optional RECORD_ID=<unique-id>)
# (privileged faults: FAULTINJECTOR_PREFIX='sudo env PATH=<kurtosis-path>')
corpus.generate: build.faultinjector
	@test -n "$(SCENARIO)" || (echo "usage: make corpus.generate SCENARIO=<id> BEACON=<cl-service-name>" && exit 1)
	@test -n "$(BEACON)" || (echo "usage: make corpus.generate SCENARIO=<id> BEACON=<cl-service-name>" && exit 1)
	@record_id="$(RECORD_ID)"; \
	out_id="$(SCENARIO)"; \
	record_arg=""; \
	if [ -n "$$record_id" ]; then out_id="$$record_id"; record_arg="--record-id $$record_id"; fi; \
	mkdir -p "$(CORPUS_OUT)/$$out_id"; \
	$(FAULTINJECTOR_PREFIX) ./bin/faultinjector run \
		--scenario "$(SCENARIO)" $$record_arg \
		--out "$(CORPUS_OUT)/$$out_id" \
		--enclave "$(DEVNET_ENCLAVE)" \
		--beacon-api "$$(kurtosis port print "$(DEVNET_ENCLAVE)" "$(BEACON)" http)" \
		--ntp-server "$(NTP_SERVER)"

## corpus.generate.all: regenerate the complete release corpus serially
# (a failing scenario is reported and skipped, not fatal: one scenario whose
# fault degrades its own node must not discard the untried scenarios after it)
corpus.generate.all: build.faultinjector
	@mkdir -p "$(CORPUS_OUT)"
	@set -u; \
	failed=""; \
	for pair in $(CORPUS_SCENARIOS); do \
		scenario=$${pair%%:*}; \
		beacon=$${pair#*:}; \
		echo ">> corpus $$scenario"; \
		mkdir -p "$(CORPUS_OUT)/$$scenario"; \
		if $(FAULTINJECTOR_PREFIX) ./bin/faultinjector run \
			--scenario "$$scenario" \
			--out "$(CORPUS_OUT)/$$scenario" \
			--enclave "$(DEVNET_ENCLAVE)" \
			--beacon-api "$$(kurtosis port print "$(DEVNET_ENCLAVE)" "$$beacon" http)" \
			--ntp-server "$(NTP_SERVER)"; then \
			echo ">> corpus $$scenario OK"; \
		else \
			echo ">> corpus $$scenario FAILED"; \
			failed="$$failed $$scenario"; \
		fi; \
	done; \
	if [ -n "$$failed" ]; then echo "FAILED scenarios:$$failed" && exit 1; fi; \
	echo "all scenarios generated"

## corpus.generate.campaign: add 35 independent live records for the 50-scenario release corpus
corpus.generate.campaign: build.faultinjector
	@mkdir -p "$(CORPUS_OUT)"; \
	set -u; \
	failed=""; \
	for spec in $(CORPUS_CAMPAIGN); do \
		old_ifs=$$IFS; IFS=:; set -- $$spec; IFS=$$old_ifs; \
		recipe=$$1; beacon=$$2; count=$$3; \
		round=2; last=$$((count + 1)); \
		while [ "$$round" -le "$$last" ]; do \
			record_id=$$(printf '%s-r%02d' "$$recipe" "$$round"); \
			echo ">> corpus $$record_id (recipe $$recipe)"; \
			mkdir -p "$(CORPUS_OUT)/$$record_id"; \
			if $(FAULTINJECTOR_PREFIX) ./bin/faultinjector run \
				--scenario "$$recipe" \
				--record-id "$$record_id" \
				--out "$(CORPUS_OUT)/$$record_id" \
				--enclave "$(DEVNET_ENCLAVE)" \
				--beacon-api "$$(kurtosis port print "$(DEVNET_ENCLAVE)" "$$beacon" http)" \
				--ntp-server "$(NTP_SERVER)"; then \
				echo ">> corpus $$record_id OK"; \
			else \
				echo ">> corpus $$record_id FAILED"; \
				failed="$$failed $$record_id"; \
			fi; \
			round=$$((round + 1)); \
		done; \
	done; \
	if [ -n "$$failed" ]; then echo "FAILED campaign records:$$failed" && exit 1; fi; \
	echo "all campaign records generated"

## eval: RCA accuracy report across the corpus (writes docs/evaluation.md)
eval:
	go run ./tools/eval "$(CORPUS_DIR)" > "$(EVAL_OUT)"
	@echo "wrote $(EVAL_OUT)"

## eval.check: enforce corpus accuracy and committed-report freshness
# The release policy's exit status must be captured, not discarded. Ending the
# eval line with `;` used to swallow it: `--check` printed "release gate failed:
# corpus has 13 scenarios, want at least 50" to stderr, the recipe carried on to
# the freshness comparison, that passed, and `make ci` reported CI PASSED with
# the gate failing in plain sight. Every corpus-size, accuracy, ambiguous-case,
# and false-high check in tools/eval was unenforced for as long as that held.
# Both checks still run before either can exit, so one run reports a stale
# report and a failed policy together rather than hiding the second behind the
# first.
eval.check: corpus.validate
	@tmp=$$(mktemp); trap 'rm -f "$$tmp"' EXIT; \
		policy=0; \
		go run ./tools/eval --check "$(CORPUS_DIR)" > "$$tmp" || policy=$$?; \
		if ! cmp -s "$$tmp" "$(EVAL_OUT)"; then \
			echo "$(EVAL_OUT) is stale; run: make eval"; \
			diff -u "$(EVAL_OUT)" "$$tmp" || true; \
			exit 1; \
		fi; \
		if [ "$$policy" -ne 0 ]; then \
			echo "release gate failed (see the eval output above); exit $$policy"; \
			exit "$$policy"; \
		fi

## clean: remove build artifacts
clean:
	rm -rf bin/

.PHONY: help build build.all build.faultinjector test test.golden test.faults.darwin test.faults.clock test.freshinstall test.image test.soak lint fmt check.format check.tidy vuln check check.workflows check.toolchain release.check release.snapshot \
        check.purity check.isolation check.egress check.globals check.nonroot check.placeholders ci \
	devnet.image devnet.check devnet.up devnet.down devnet.info \
        corpus.validate corpus.generate corpus.generate.all corpus.generate.campaign eval eval.check clean
