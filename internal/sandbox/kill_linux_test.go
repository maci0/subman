//go:build linux

package sandbox

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A cancelled Run must reap the agent and anything it spawned. The helper
// execs the agent, so signalling only that process would leave the grandchild.
func TestRunKillsTheProcessGroupOnCancel(t *testing.T) {
	if !Check().OK {
		t.Skip(Check().Note)
	}
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "child.pid")
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		_, err := Run(ctx, Command{Dir: dir, Writable: dir, Args: []string{
			"sh", "-c", "sleep 30 & echo $! > '" + pidFile + "'; wait",
		}})
		done <- err
	}()
	var pid int
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		body, err := os.ReadFile(pidFile)
		if err == nil {
			if _, scanErr := fmt.Sscanf(strings.TrimSpace(string(body)), "%d", &pid); scanErr == nil && pid > 1 {
				break
			}
			pid = 0
		}
		time.Sleep(20 * time.Millisecond)
	}
	if pid == 0 {
		cancel()
		<-done
		t.Fatal("grandchild never wrote its pid")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled run returned success")
		}
	case <-time.After(8 * time.Second):
		t.Fatal("cancelled run did not return")
	}
	time.Sleep(200 * time.Millisecond)
	if err := syscall.Kill(pid, 0); err == nil {
		_ = syscall.Kill(pid, syscall.SIGKILL)
		t.Fatalf("grandchild %d still running after cancel", pid)
	}
}
