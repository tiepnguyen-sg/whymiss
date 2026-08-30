module github.com/tiepnguyen-sg/whymiss

// BUILD_PROMPT §3 locks the language at Go 1.23+. CI installs the toolchain from
// this line (.github/workflows/ci.yml → setup-go go-version-file: go.mod), so it
// is the single source of truth for the version.
//
// CI pins its Go tools and sets GOTOOLCHAIN=local, so an incompatible tool
// requirement fails outright instead of silently downloading another toolchain.
// Bump this line and the pinned tools deliberately, not independently.
go 1.25.14

// The toolchain CI builds with, kept separate from the language minimum above on
// purpose: `go` is the oldest release that can compile this module, `toolchain`
// is the exact compiler the release artifact is produced by.
//
// setup-go does NOT read this line — it installs the `go` directive above and
// ignores `toolchain` entirely (measured on setup-go v7.0.0: "Setup go version
// spec 1.25.14"). The workflows therefore pin `go-version:` literally, and
// `make check.toolchain` fails the build if any of them stops matching this
// line. That check is the only thing keeping the two honest.
//
// It is not cosmetic. The release soak ran a binary built by go1.26.6; the same
// source built by go1.25.14 is a different 14258360-byte binary rather than the
// 17019042-byte one that was measured for 72 hours. Building the release with a
// compiler the soak never exercised would make the soak evidence describe
// something other than the artifact. Bump this together with the pinned CI tools.
toolchain go1.26.6

// New dependencies require an ADR (I-14: fewer than 15 direct dependencies at
// v1.0). The locked choices in BUILD_PROMPT §3 — cobra, koanf,
// modernc.org/sqlite, client_golang — land in the phase that first needs them,
// each with its own ADR merged first.

require (
	go.uber.org/goleak v1.3.0 // ADR-0013: daemon lifecycle leak verification
	gopkg.in/yaml.v3 v3.0.1 // ADR-0006: tools/faultinjector scenario files, test/corpus manifest.yaml
)

require (
	github.com/knadh/koanf/v2 v2.3.6
	github.com/prometheus/client_golang v1.24.1
	github.com/spf13/cobra v1.10.2
	modernc.org/sqlite v1.57.0
)

require (
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/go-viper/mapstructure/v2 v2.4.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/knadh/koanf/maps v0.1.2 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/mitchellh/copystructure v1.2.0 // indirect
	github.com/mitchellh/reflectwalk v1.0.2 // indirect
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
