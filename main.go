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
	configureManagedInstallerEnvironment()

	err := cmd.Root(version).Run(context.Background(), argsWithDefaultCommand(os.Args))
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		cli.HandleExitCoder(err)
		os.Exit(1) //nolint:gocritic
	}
}

// configureManagedInstallerEnvironment keeps installers used internally by
// Renart from editing the user's shell startup files. The uv installer honors
// UV_NO_MODIFY_PATH; the dependency checker's older variable spelling is not
// recognized by current uv releases, so set the supported variable in the
// parent process where every Python and load-asset execution inherits it.
func configureManagedInstallerEnvironment() {
	_ = os.Setenv("UV_NO_MODIFY_PATH", "1")
}

// argsWithDefaultCommand makes the desktop app the natural entry point while
// preserving the explicit CLI surface (`renart --help`, `renart run`, and so
// on). Subcommand-specific arguments remain explicit so command typos are not
// silently interpreted as workspace paths.
func argsWithDefaultCommand(args []string) []string {
	if len(args) != 1 {
		return args
	}
	return append(append([]string(nil), args...), "standalone")
}
