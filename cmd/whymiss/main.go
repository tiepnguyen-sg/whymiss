// Command whymiss is a read-only sidecar that explains why an Ethereum validator
// missed or was late on a duty.
//
// This file is intentionally thin (AGENTS.md: "thin: parse → wire → run → exit
// code") and is the only place in the repository permitted to call os.Exit or
// panic (I-15). Wiring lives in internal/app; this file and its sibling
// command files only call into it.
package main

import (
	"fmt"
	"os"
)

// version is stamped at build time via -ldflags (see Makefile LDFLAGS).
var version = "dev"

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
