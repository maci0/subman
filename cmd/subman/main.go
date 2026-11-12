// Command subman is the CLI.
//
// The window is the root main package (`go build -o subman .`). This
// package is the command line, built with `go build -o subman ./cmd/subman`
// when a release needs the CLI without WebKit. Both report the same version.
//
// The command line follows The CLI Spec v0.3: https://clispec.dev
package main

import (
	"os"

	"github.com/maci0/subman/internal/cli"
	"github.com/maci0/subman/internal/sandbox"
)

var version = "0.1.2"

func main() {
	// A sandboxed child is this binary started again: confine and exec it.
	if sandbox.MaybeReexec(os.Args) {
		return
	}
	cli.Version = version
	os.Exit(cli.Run(os.Args[1:], cli.Std()))
}
