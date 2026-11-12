package app

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/maci0/subman/internal/model"
	"github.com/maci0/subman/internal/sandbox"
)

// runChange executes a single binary. The command string is a path, not a
// shell line: spaces split arguments, and there is no quoting, pipe, or
// expansion. {from} and {to} are replaced in arguments only.
func runChange(command, from, to, work string) error {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return errNoCommand
	}
	bin := fields[0]
	// A separator means a path, and only an absolute one is accepted: a
	// relative path would be resolved against the account directory the
	// child is confined to. The separator is the platform's own, so a
	// Windows build accepts "C:\Tools\agy.exe" and still rejects "foo\bar".
	// filepath, not a "/" check: the released Windows binaries take
	// "C:\Tools\change.exe".
	if strings.ContainsRune(bin, filepath.Separator) || strings.ContainsRune(bin, '/') {
		if !filepath.IsAbs(bin) {
			return fmt.Errorf("change command must be an absolute path or a name on PATH")
		}
	}
	args := make([]string, 0, len(fields)+1)
	sawPlaceholder := false
	for _, f := range fields[1:] {
		if strings.Contains(f, "{from}") || strings.Contains(f, "{to}") {
			sawPlaceholder = true
		}
		f = strings.ReplaceAll(f, "{from}", from)
		f = strings.ReplaceAll(f, "{to}", to)
		args = append(args, f)
	}
	if !sawPlaceholder {
		args = append(args, from, to)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	argv := append([]string{bin}, args...)
	out, err := sandbox.Run(ctx, sandbox.Command{Dir: work, Writable: work, Args: argv})
	if err != nil {
		msg := model.TruncateRunes(strings.TrimSpace(string(out)), 400)
		if msg == "" {
			return err
		}
		return fmt.Errorf("%w: %s", err, msg)
	}
	return nil
}
