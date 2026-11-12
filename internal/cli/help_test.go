package cli

import (
	"strings"
	"testing"
)

func TestHelpOnEveryCommand(t *testing.T) {
	// --help and -h are help wherever they appear, never an account id or an
	// unknown flag, and they do not need the data directory.
	t.Setenv("SUBMAN_DATA", "/no/such/data")
	for _, args := range [][]string{
		{"accounts", "show", "--help"},
		{"accounts", "remove", "-h"},
		{"accounts", "add", "--help"},
		{"ladder", "--help"},
		{"ladder", "run", "--help"},
		{"schema", "--help"},
		{"update", "--help"},
		{"version", "--help"},
	} {
		io, out, err := buf(false)
		if code := Run(args, io); code != 0 || !strings.Contains(out.String(), "Usage:") || err.Len() != 0 {
			t.Fatalf("%v exited %d out %q err %q", args, code, out.String(), err.String())
		}
	}
}
