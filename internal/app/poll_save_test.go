package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maci0/subman/internal/model"
	"github.com/maci0/subman/internal/store"
)

// A poll whose reading cannot be written is a failure, not a finished poll
// that quietly kept the previous numbers.
func TestPollReportsAReadingThatWasNotSaved(t *testing.T) {
	dir := t.TempDir()
	root, err := store.Open(filepath.Join(dir, "data"))
	if err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "cred.json")
	if err := os.WriteFile(src, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	a := New(root)
	view, err := a.Import(model.ImportRequest{Name: "work", Provider: model.ProviderClaude, CredentialPath: src})
	if err != nil {
		t.Fatal(err)
	}
	// The reading's path is a directory, so the poll can reach no provider
	// and has nowhere to write. The save is what must be reported.
	blocked := filepath.Join(view.Account.Home, "reading.json")
	if err := os.Remove(blocked); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err := os.Mkdir(blocked, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Poll(); err == nil || !strings.Contains(err.Error(), "work") {
		t.Fatalf("poll hid the unsaved reading: %v", err)
	}
}
