// The module path carries a placeholder. STRUCTURE.md's init step rewrites it:
//   grep -rl 'CHANGEME' . | xargs sed -i 's|CHANGEME|<your-org>|g'
module github.com/CHANGEME/whymiss

// BUILD_PROMPT §3 locks the language at Go 1.23+. CI installs the toolchain from
// this line (.github/workflows/ci.yml → setup-go go-version-file: go.mod), so it
// is the single source of truth for the version.
//
// Set above the 1.23 floor because `make fmt` installs gofumpt@latest, and
// gofumpt's own go.mod requirement climbs over time; ci.yml pins GOTOOLCHAIN=local
// so a mismatch fails outright instead of silently downloading a newer toolchain.
// Bump this line, not gofumpt's version, when that requirement moves again.
go 1.25.14

// New dependencies require an ADR (I-14: fewer than 15 direct dependencies at
// v1.0). The locked choices in BUILD_PROMPT §3 — cobra, koanf,
// modernc.org/sqlite, client_golang — land in the phase that first needs them,
// each with its own ADR merged first.

require gopkg.in/yaml.v3 v3.0.1 // ADR-0006: tools/faultinjector scenario files, test/corpus manifest.yaml

require (
	github.com/prometheus/client_golang v1.24.1
	github.com/spf13/cobra v1.10.2
	modernc.org/sqlite v1.57.0
)

require (
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/common v0.70.1 // indirect
	github.com/prometheus/procfs v0.21.1 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/spf13/pflag v1.0.9 // indirect
	golang.org/x/sys v0.47.0 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
	modernc.org/libc v1.74.4 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.11.0 // indirect
)
