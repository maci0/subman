package sandbox

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestLandlockRefusesAWriteOutside drives the re-exec path with this test
// binary, which is what Probe does with the subman binary. The child writes
// inside the granted directory and attempts a write outside it.
func TestLandlockRefusesAWriteOutside(t *testing.T) {
	if os.Getenv("SUBMAN_SANDBOX_CHILD") == "1" {
		landlockChild()
		return
	}
	if !Check().OK {
		t.Skip(Check().Note)
	}
	dir := t.TempDir()
	granted := filepath.Join(dir, "agent")
	denied := filepath.Join(dir, "host")
	if err := os.MkdirAll(granted, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(denied, 0o700); err != nil {
		t.Fatal(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(self, "-test.run", "TestLandlockRefusesAWriteOutside")
	cmd.Env = []string{
		"SUBMAN_SANDBOX_CHILD=1",
		"SUBMAN_SANDBOX=" + granted,
		"SUBMAN_DENIED=" + denied,
		"PATH=" + os.Getenv("PATH"),
	}
	out, err := cmd.CombinedOutput()
	text := string(out)
	if err != nil {
		t.Fatalf("child: %v\n%s", err, text)
	}
	if !strings.Contains(text, "inside-ok") || !strings.Contains(text, "outside-refused") {
		t.Fatalf("confinement did not hold:\n%s", text)
	}
	if _, err := os.Stat(filepath.Join(denied, "nope")); !os.IsNotExist(err) {
		t.Fatal("a file was written outside the grant")
	}
}

func landlockChild() {
	if err := applyChild(); err != nil {
		os.Stderr.WriteString("confine: " + err.Error() + "\n")
		os.Exit(1)
	}
	granted := os.Getenv("SUBMAN_SANDBOX")
	if err := os.WriteFile(filepath.Join(granted, "ok"), []byte("x"), 0o600); err != nil {
		os.Stderr.WriteString("inside write failed: " + err.Error() + "\n")
		os.Exit(1)
	}
	os.Stdout.WriteString("inside-ok\n")
	err := os.WriteFile(filepath.Join(os.Getenv("SUBMAN_DENIED"), "nope"), []byte("x"), 0o600)
	if err == nil {
		os.Stdout.WriteString("outside-written\n")
		os.Exit(1)
	}
	os.Stdout.WriteString("outside-refused\n")
	os.Exit(0)
}
