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
go 1.25

// No dependencies yet, and none arrive without an ADR (I-14: fewer than 15 direct
// dependencies at v1.0). The locked choices in BUILD_PROMPT §3 — cobra, koanf,
// modernc.org/sqlite, client_golang — land in the phase that first needs them,
// each with its ADR merged first.
