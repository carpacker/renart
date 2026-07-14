// Command buildwheel exports the exact Python SDK wheel assembled by Renart.
// Release automation injects internal/pysdk.Version from the release tag.
package main

import (
	"flag"
	"fmt"
	"os"

	"renart/internal/pysdk"
)

func main() {
	outputDir := flag.String("output", "dist/python-sdk", "directory for the built wheel")
	flag.Parse()

	target, err := pysdk.BuildWheel(*outputDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(target)
}
