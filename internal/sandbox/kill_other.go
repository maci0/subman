//go:build !linux

package sandbox

import (
	"os/exec"
	"time"
)

// killOnCancel bounds how long Wait stays behind a child that survived the
// context's own kill. Process groups are a Linux concern: that is where the
// helper execs an agent that starts children of its own.
func killOnCancel(child *exec.Cmd) {
	child.WaitDelay = 3 * time.Second
}
