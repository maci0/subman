package sandbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maci0/subman/internal/model"
)

func TestResolveProgramLeavesAPathAlone(t *testing.T) {
	// A path must not be looked up on PATH. On Windows that includes a
	// drive path, which contains no "/".
	for _, program := range []string{filepath.Join(t.TempDir(), "agy"), `C:\Tools\agy.exe`, "C:/Tools/agy.exe"} {
		if !strings.ContainsRune(program, filepath.Separator) && !strings.ContainsRune(program, '/') {
			continue
		}
		got, err := resolveProgram(program)
		if err != nil || got != program {
			t.Fatalf("%q resolved to %q (%v)", program, got, err)
		}
	}
	if _, err := resolveProgram("subman-no-such-program"); err == nil {
		t.Fatal("a missing bare name resolved")
	}
}

func TestCheckReportsLandlock(t *testing.T) {
	sup := Check()
	if !sup.OK {
		t.Skip(sup.Note)
	}
	if !strings.Contains(sup.Note, "landlock") {
		t.Fatal(sup.Note)
	}
}

func TestProbeConfinesWrites(t *testing.T) {
	if !Check().OK {
		t.Skip(Check().Note)
	}
	dir := t.TempDir()
	agent := filepath.Join(dir, "agent")
	outside := filepath.Join(dir, "outside")
	if err := os.MkdirAll(agent, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	// The helper writes one file inside the agent dir and tries one outside.
	// Landlock must allow the first and refuse the second.
	bin := filepath.Join(dir, "probe")
	script := "#!/bin/sh\nprintf inside > \"$1/inside\"\nprintf outside > \"$2/outside\" 2>/dev/null && echo WROTE || echo REFUSED\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	res := Probe(t.Context(), model.Account{
		ID: "t1", Provider: model.ProviderClaude, AgentHome: agent, AgentBin: bin + " " + outside,
	})
	// A space is rejected before any exec: the binary is one path.
	if res.OK || res.Sandboxed {
		t.Fatalf("a path with a space ran: %+v", res)
	}
}

func TestProbeRejectsShellMetacharacters(t *testing.T) {
	res := Probe(t.Context(), model.Account{ID: "t2", AgentHome: t.TempDir(), AgentBin: "claude;id"})
	if res.OK || res.Sandboxed {
		t.Fatalf("ran a metacharacter binary: %+v", res)
	}
}

// A program given by bare name is found on PATH. execve alone would not:
// every default agent ("claude", "codex", "grok", "agy") is a bare name.
func TestRunResolvesABareNameOnPath(t *testing.T) {
	if !Check().OK {
		t.Skip(Check().Note)
	}
	dir := t.TempDir()
	out, err := Run(t.Context(), Command{Dir: dir, Writable: dir, Args: []string{"sh", "-c", "printf found"}})
	if err != nil || string(out) != "found" {
		t.Fatalf("%q %v", out, err)
	}
	out, err = Run(t.Context(), Command{Dir: dir, Writable: dir, Args: []string{"subman-no-such-program"}})
	if err == nil || !strings.Contains(string(out), "subman-no-such-program is not on PATH") {
		t.Fatalf("%q %v", out, err)
	}
}
