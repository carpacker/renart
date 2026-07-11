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

	err := cmd.Root(version).Run(context.Background(), os.Args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		cli.HandleExitCoder(err)
		os.Exit(1) //nolint:gocritic
	}
}
