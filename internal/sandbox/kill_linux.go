//go:build linux

package sandbox

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// killOnCancel makes a cancelled context kill child and every process it
// starts. The helper execs the agent, so signalling only the helper would
// leave the agent running after the poll or the plan change gave up.
func killOnCancel(child *exec.Cmd) {
	child.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	child.WaitDelay = 3 * time.Second
	child.Cancel = func() error {
		if child.Process == nil {
			return os.ErrProcessDone
		}
		// The group id is the helper's pid. Negative targets the group.
		err := syscall.Kill(-child.Process.Pid, syscall.SIGKILL)
		if err == nil || errors.Is(err, syscall.ESRCH) {
			return nil
		}
		return child.Process.Kill()
	}
}
