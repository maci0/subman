package app

import (
	"os"
	"path/filepath"
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

func TestRunChangeRejectsRelativePath(t *testing.T) {
	if err := runChange("foo/bar", "a", "b", t.TempDir()); err == nil {
		t.Fatal("relative path accepted")
	}
}
