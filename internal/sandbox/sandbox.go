// Package sandbox runs an agent CLI confined the way gauntlet and
// microagent confine one: Linux Landlock, applied in the child after a
// re-exec.
//
// / is read-only. The account's agent directory is the one writable path.
// HOME and the provider's own home variable point there, so the CLI's
// default (~/.claude, ~/.codex, ~/.grok, ~/.gemini) is not where it writes.
//
// A user namespace is not used. unshare(CLONE_NEWUSER) returns EPERM for a
// process with no capabilities and no_new_privs already set, which is the
// state of a sandboxed agent session. Landlock does not need either.
package sandbox

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/maci0/subman/internal/model"
)

const (
	maxOutput = 64 << 10
	timeout   = 45 * time.Second
)

// Support describes whether this machine can confine a child.
type Support struct {
	OK   bool
	Note string
}

func Check() Support {
	abi, err := landlockABI()
	if err != nil {
		return Support{Note: err.Error()}
	}
	return Support{OK: true, Note: fmt.Sprintf("landlock abi %d", abi)}
}

func defaultBin(p model.Provider) string {
	switch p {
	case model.ProviderClaude:
		return "claude"
	case model.ProviderCodex:
		return "codex"
	case model.ProviderGrok:
		return "grok"
	case model.ProviderAntigravity:
		return "agy"
	default:
		return ""
	}
}

// ValidProgram reports whether program is one executable name or path.
// The poll and the probe pass it straight to exec, so a value with a space,
// a shell metacharacter, or ".." is a command line, not a program.
func ValidProgram(program string) error {
	if program == "" || strings.ContainsAny(program, " \t\n;|&$`") || strings.Contains(program, "..") {
		return errors.New("agent binary must be a single path with no shell metacharacters")
	}
	return nil
}

func homeEnv(p model.Provider) string {
	switch p {
	case model.ProviderClaude:
		return "CLAUDE_CONFIG_DIR"
	case model.ProviderCodex:
		return "CODEX_HOME"
	default:
		return ""
	}
}

// Probe runs `<bin> --version` inside the sandbox and returns what it printed.
func Probe(ctx context.Context, acc model.Account) model.ProbeResult {
	res := model.ProbeResult{AccountID: acc.ID}
	sup := Check()
	if !sup.OK {
		res.Note = sup.Note
		return res
	}
	bin := acc.AgentBin
	if bin == "" {
		bin = defaultBin(acc.Provider)
	}
	if bin == "" {
		res.Note = "no agent binary for this provider"
		return res
	}
	if err := ValidProgram(bin); err != nil {
		res.Note = err.Error()
		return res
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	extra := []string{}
	if v := homeEnv(acc.Provider); v != "" {
		extra = append(extra, v+"="+acc.AgentHome)
	}
	out, err := Run(ctx, Command{
		Dir: acc.AgentHome, Writable: acc.AgentHome,
		Args: []string{bin, "--version"}, Extra: extra,
	})
	res.Output = outputString(out)
	var run *RunError
	if errors.As(err, &run) {
		res.Note = run.Error()
		return res
	}
	res.Sandboxed = true
	if err == nil {
		res.OK = true
		return res
	}
	res.Exit = exitCode(err)
	res.Note = noteFor(err, out)
	return res
}

// MaybeReexec confines this process and replaces it with args when Run
// started it. A normal invocation returns false.
func MaybeReexec(args []string) bool {
	if os.Getenv("SUBMAN_REEXEC") != "1" || len(args) < 2 {
		return false
	}
	if err := applyChild(); err != nil {
		fmt.Fprintln(os.Stderr, "sandbox:", err)
		os.Exit(1)
	}
	// The marker must not survive into the program, or a child that is
	// itself subman would confine again.
	env := os.Environ()
	kept := env[:0]
	for _, e := range env {
		if !strings.HasPrefix(e, "SUBMAN_REEXEC=") {
			kept = append(kept, e)
		}
	}
	agent := args[1:]
	// execve does not search PATH, so a bare name ("claude", "agy") is
	// resolved here. This runs after confinement: the lookup only reads.
	// A path uses the separator of the platform this binary was built for.
	program, err := resolveProgram(agent[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "sandbox exec: %s is not on PATH\n", agent[0])
		os.Exit(1)
	}
	if err := unixExec(program, agent, kept); err != nil {
		fmt.Fprintf(os.Stderr, "sandbox exec: %s: %v\n", program, err)
		os.Exit(1)
	}
	return true
}

// resolveProgram returns program unchanged when it is already a path, and
// looks a bare name up on PATH. The separator has to be the platform's own:
// on Windows an absolute path contains no "/" and would otherwise be
// searched for on PATH.
func resolveProgram(program string) (string, error) {
	if strings.ContainsRune(program, filepath.Separator) || strings.ContainsRune(program, '/') {
		return program, nil
	}
	return exec.LookPath(program)
}
