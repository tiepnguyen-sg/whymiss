# whymiss — the single interface every agent and human uses.
# If a workflow is not here, add it here. Do not run ad-hoc shell.

MODULE  := github.com/CHANGEME/whymiss
BIN     := whymiss
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
GOFLAGS := -trimpath
LDFLAGS := -s -w -X main.version=$(VERSION)

export CGO_ENABLED := 0

.DEFAULT_GOAL := help

## help: list available targets
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## //' | column -t -s ':'

## build: build the binary for the host platform
build:
	go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o bin/$(BIN) ./cmd/$(BIN)

## build.all: cross-compile for supported platforms (I-13)
build.all:
	GOOS=linux GOARCH=amd64 go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o bin/$(BIN)-linux-amd64 ./cmd/$(BIN)
	GOOS=linux GOARCH=arm64 go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o bin/$(BIN)-linux-arm64 ./cmd/$(BIN)

## test: run unit tests with the race detector
test:
	go test -race -count=1 ./...

## test.golden: regenerate golden files (review every diff as code)
test.golden:
	go test ./internal/rca/... -update

## lint: run golangci-lint
lint:
	golangci-lint run

## fmt: format all Go source
fmt:
	gofumpt -l -w .

## vuln: scan dependencies for known vulnerabilities
vuln:
	govulncheck ./...

## check: enforce invariants that lint cannot express
check: check.purity check.isolation check.egress

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
		|| (echo "FAIL: internal/rca imports outside stdlib + internal/domain" && exit 1)
	@echo "OK"

# I-11: no client-specific identifier outside internal/source/**
check.isolation:
	@echo ">> I-11 client isolation"
	@! grep -rniE 'lighthouse|prysm|teku|nimbus|lodestar|nethermind|erigon|besu|reth' \
		--include='*.go' internal cmd \
		| grep -v '^internal/source/' \
		|| (echo "FAIL: client name referenced outside internal/source/" && exit 1)
	@echo "OK"

# I-4: no outbound HTTP client construction outside adapters
check.egress:
	@echo ">> I-4 egress boundary"
	@! grep -rnE 'http\.(Get|Post|DefaultClient)' --include='*.go' internal cmd \
		| grep -v '^internal/source/' \
		|| (echo "FAIL: outbound HTTP outside internal/source/" && exit 1)
	@echo "OK"

## ci: the gate. Every task must pass this before being declared done.
ci: fmt lint check test vuln build.all
	@echo "CI PASSED"

## devnet.up: start the Kurtosis Ethereum devnet faultinjector runs against
devnet.up:
	kurtosis run github.com/ethpandaops/ethereum-package \
		--args-file test/e2e/kurtosis/network_params.yaml \
		--enclave whymiss-devnet

## devnet.down: tear down the devnet and free its resources
devnet.down:
	kurtosis enclave rm --force whymiss-devnet

## devnet.info: print the devnet's service endpoints as JSON
devnet.info:
	kurtosis enclave inspect whymiss-devnet --full-uuids

## corpus.validate: validate every labelled failure scenario
corpus.validate:
	go run ./tools/corpusctl validate ./test/corpus

## corpus.generate: generate one scenario against the running devnet
## (usage: make corpus.generate SCENARIO=vc-frozen-lighthouse BEACON=cl-1-lighthouse-geth)
corpus.generate:
	@test -n "$(SCENARIO)" || (echo "usage: make corpus.generate SCENARIO=<id> BEACON=<cl-service-name>" && exit 1)
	@test -n "$(BEACON)" || (echo "usage: make corpus.generate SCENARIO=<id> BEACON=<cl-service-name>" && exit 1)
	go run ./tools/faultinjector run --scenario $(SCENARIO) --out ./test/corpus/$(SCENARIO) \
		--beacon-api $$(kurtosis port print whymiss-devnet $(BEACON) http)

## eval: RCA accuracy report across the corpus (writes docs/evaluation.md)
eval:
	go run ./tools/eval ./test/corpus > docs/evaluation.md
	@echo "wrote docs/evaluation.md"

## clean: remove build artifacts
clean:
	rm -rf bin/

.PHONY: help build build.all test test.golden lint fmt vuln check \
        check.purity check.isolation check.egress ci \
        devnet.up devnet.down devnet.info \
        corpus.validate corpus.generate eval clean
