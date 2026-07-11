package cmd

import (
	"github.com/urfave/cli/v3"
)

// Debug groups the internal tools that are not part of the user-facing CLI
// surface: fingerprint inspection, the stdio SQL language server, and the
// WASM cache warmer. The group is hidden from the root help; `renart debug
// --help` lists its commands for those who know to look.
func Debug() *cli.Command {
	return &cli.Command{
		Name:   "debug",
		Usage:  "internal debugging and integration tools",
		Hidden: true,
		Commands: []*cli.Command{
			Fp(),
			SQLLSP(),
			WarmCache(),
		},
	}
}
