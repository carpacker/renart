package main

import (
	"context"
	"fmt"
	"os"

	"github.com/fatih/color"
	"github.com/urfave/cli/v3"
	"renart/cmd"
)

var version = "dev"

func main() {
	color.NoColor = false

	app := &cli.Command{
		Name:    "renart",
		Version: version,
		Usage:   "Standalone Renart server",
		Commands: []*cli.Command{
			cmd.Web(),
			cmd.Standalone(),
			cmd.Fp(),
			cmd.Deploy(),
			cmd.TypeCheck(),
			cmd.SQLLSP(),
			cmd.WarmCache(),
		},
		DisableSliceFlagSeparator: true,
	}

	err := app.Run(context.Background(), os.Args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		cli.HandleExitCoder(err)
		os.Exit(1) //nolint:gocritic
	}
}
