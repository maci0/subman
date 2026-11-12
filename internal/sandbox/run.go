package sandbox

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"unicode/utf8"

	"github.com/maci0/subman/internal/model"
)

// RunError is a child that was not started because it could not be confined.
type RunError struct{ Err error }

func (e *RunError) Error() string { return "sandbox: " + e.Err.Error() }
func (e *RunError) Unwrap() error { return e.Err }

// Command is one confined child. Writable is the only directory it may
// write. Extra is appended to an environment that already has PATH, HOME,
// and TERM. Args are the program and its arguments, never a shell line.
type Command struct {
	Dir      string
	Writable string
	Args     []string
	Extra    []string
}

// Run starts cmd confined and returns its combined output. There is no
// unconfined path: a kernel without Landlock, or a child that fails to
// confine itself, returns an error and the program is not executed.
func Run(ctx context.Context, cmd Command) ([]byte, error) {
	if len(cmd.Args) == 0 || cmd.Args[0] == "" {
		return nil, &RunError{Err: errors.New("no program")}
	}
	if cmd.Writable == "" {
		return nil, &RunError{Err: errors.New("no writable directory")}
	}
	if err := os.MkdirAll(cmd.Writable, 0o700); err != nil {
		return nil, &RunError{Err: err}
	}
	sup := Check()
	if !sup.OK {
		return nil, &RunError{Err: errors.New(sup.Note)}
	}
	// A dedicated helper confines and execs. Re-execing whatever binary is
	// running fails under go test, which wraps the test binary.
	bin, err := helper()
	if err != nil {
		return nil, &RunError{Err: err}
	}
	argv := append([]string{bin}, cmd.Args...)
	child := exec.CommandContext(ctx, argv[0], argv[1:]...)
	// The helper execs the agent, which may itself start children. On Linux
	// the child is a process group so a cancelled context kills all of them.
	killOnCancel(child)
	child.Dir = cmd.Dir
	if child.Dir == "" {
		child.Dir = cmd.Writable
	}
	env := []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + cmd.Writable,
		"TERM=dumb",
		"SUBMAN_SANDBOX=" + cmd.Writable,
		"SUBMAN_REEXEC=1",
	}
	env = append(env, cmd.Extra...)
	child.Env = env
	var buf bytes.Buffer
	child.Stdout = &buf
	child.Stderr = &buf
	err = child.Run()
	out := buf.Bytes()
	if len(out) > maxOutput {
		out = append(model.TruncateBytes(out, maxOutput), []byte("\n…truncated")...)
	}
	if err != nil && bytes.HasPrefix(out, []byte("sandbox:")) {
		line := string(out)
		if i := bytes.IndexByte(out, '\n'); i >= 0 {
			line = string(out[:i])
		}
		return out, &RunError{Err: errors.New(line)}
	}
	return out, err
}

func exitCode(err error) int {
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode()
	}
	return 1
}

func outputString(out []byte) string {
	if !utf8.Valid(out) {
		// A child can write any bytes. Dropping the bad ones here keeps
		// every later reader, which stores and draws this as text, on valid
		// UTF-8 instead of a replacement character standing in for a byte.
		out = []byte(strings.ToValidUTF8(string(out), ""))
	}
	if len(out) <= maxOutput {
		return string(out)
	}
	return string(model.TruncateBytes(out, maxOutput)) + "\n…truncated"
}

func noteFor(err error, out []byte) string {
	var run *RunError
	if errors.As(err, &run) {
		return run.Error()
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timed out"
	}
	if err == nil {
		return ""
	}
	return fmt.Sprintf("agent exited %d", exitCode(err))
}
