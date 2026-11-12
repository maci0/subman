package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunChangeAppendsTiers(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "rec")
	script := "#!/bin/sh\nprintf '%s' \"$*\" > \"$HOME/args\"\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(dir, "work")
	if err := os.MkdirAll(work, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := runChange(bin, "pro", "max", work); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(work, "args"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "pro max" {
		t.Fatalf("args %q", got)
	}
}

func TestRunChangeReplacesPlaceholders(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "rec")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nprintf '%s' \"$1>$2\" > \"$HOME/args\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(dir, "work")
	os.MkdirAll(work, 0o700)
	if err := runChange(bin+" {from} {to}", "ultra", "pro", work); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(work, "args"))
	if string(got) != "ultra>pro" {
		t.Fatalf("args %q", got)
	}
}

// rejectedAsRelative reports whether runChange refuses cmd for not being an
// absolute path. A missing program fails later, inside the sandbox, and that
// is a different error.
func rejectedAsRelative(t *testing.T, cmd string) bool {
	t.Helper()
	err := runChange(cmd, "a", "b", t.TempDir())
	return err != nil && strings.Contains(err.Error(), "absolute path")
}

func TestRunChangePathForm(t *testing.T) {
	if rejectedAsRelative(t, filepath.Join(t.TempDir(), "change")) {
		t.Fatal("an absolute path was rejected")
	}
	if !rejectedAsRelative(t, "foo/bar") {
		t.Fatal("a relative path was accepted")
	}
	// A backslash only separates on Windows. Elsewhere "foo\bar" is one name.
	if rejectedAsRelative(t, `foo\bar`) != (filepath.Separator == '\\') {
		t.Fatal(`"foo\bar" judged against the wrong separator`)
	}
	// Windows accepts a drive path with forward slashes, and the check used to
	// reject every path that contained "/" but did not begin with one. Where
	// that string is absolute it must pass; elsewhere it stays relative.
	win := "C:/Tools/change.exe"
	if rejectedAsRelative(t, win) == filepath.IsAbs(win) {
		t.Fatalf("%q: IsAbs %v but the command was judged the other way", win, filepath.IsAbs(win))
	}
}
