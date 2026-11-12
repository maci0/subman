package cli

import "strings"

// WantsCLI reports whether the arguments are a command rather than a request
// to open the window. The window keeps one flag of its own, --data, and
// parses it with the standard flag package. Anything else starting with a
// dash is a CLI flag that parser does not know: it would answer with its
// stock usage text and an exit this command line does not declare, so the
// command line answers instead.
func WantsCLI(args []string) bool {
	for _, a := range args {
		switch a {
		case "--output", "-o", "--help", "-h", "help", "version", "--version", "-v",
			"schema", "accounts", "ladder", "update":
			return true
		}
		if strings.HasPrefix(a, "--output=") {
			return true
		}
		if strings.HasPrefix(a, "-") && a != "--data" && a != "-data" && !strings.HasPrefix(a, "--data=") && !strings.HasPrefix(a, "-data=") {
			return true
		}
	}
	return false
}
