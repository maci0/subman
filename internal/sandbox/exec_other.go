//go:build !linux

package sandbox

import (
	"fmt"
	"os"
	"os/exec"
)

func unixExec(argv0 string, argv []string, env []string) error {
	cmd := exec.Command(argv0, argv[1:]...)
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return fmt.Errorf("exec: %w", cmd.Run())
}
