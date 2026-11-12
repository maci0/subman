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
	"github.com/maci0/subman/internal/version"
)

// release is the release this binary reports. It defaults to dev, and the
// release workflow overrides it with -X main.release, the same variable the
// window binary stamps, so the two builds of one tag report one number.
var release = "dev"

func main() {
	// A sandboxed child is this binary started again: confine and exec it.
	if sandbox.MaybeReexec(os.Args) {
		return
	}
	version.Release = release
	cli.Version = release
	os.Exit(cli.Run(os.Args[1:], cli.Std()))
}
