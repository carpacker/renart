package main

import (
	"context"
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
		},
		DisableSliceFlagSeparator: true,
	}

	err := app.Run(context.Background(), os.Args)
	if err != nil {
		cli.HandleExitCoder(err)
		os.Exit(1) //nolint:gocritic
	}
}
