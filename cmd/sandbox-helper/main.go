// Command sandbox-helper confines itself with Landlock and execs the
// program named in its arguments. subman starts it. It is not a user command.
package main

import (
	"fmt"
	"os"

	"github.com/maci0/subman/internal/sandbox"
)

func main() {
	if !sandbox.MaybeReexec(os.Args) {
		fmt.Fprintln(os.Stderr, "sandbox: helper started without a command")
		os.Exit(1)
	}
}
