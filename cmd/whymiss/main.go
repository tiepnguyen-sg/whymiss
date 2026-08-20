// Command whymiss is a read-only sidecar that explains why an Ethereum validator
// missed or was late on a duty.
//
// This file is intentionally thin (AGENTS.md: "thin: parse → wire → run → exit
// code") and is the only place in the repository permitted to call os.Exit or
// panic (I-15). Wiring lives in internal/app; this file only calls into it.
//
// The full CLI surface — whymiss <slot>, watch, timeline <slot>, doctor — arrives
// in Phase 2 task 2.7 alongside the cobra dependency it needs (BUILD_PROMPT §3,
// ADR-0004: a dependency lands in the phase that needs it, not before). Phase 1's
// scope is a repository that builds and cross-compiles; this stub satisfies that
// without pulling in either the wiring or the dependency early.
package main

import (
	"fmt"
	"os"
)

// version is stamped at build time via -ldflags (see Makefile LDFLAGS).
var version = "dev"

func main() {
	if _, err := fmt.Fprintf(os.Stdout, "whymiss %s — not yet implemented (Phase 1 scaffold)\n", version); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
