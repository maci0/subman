package app

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/maci0/subman/internal/model"
	"github.com/maci0/subman/internal/quota"
	"github.com/maci0/subman/internal/store"
)

// Poll asks every account's provider at once. Done one after another, the
// poll would take the sum of the round trips; the providers do not depend on
// each other, so it should take about as long as the slowest one.
func TestPollFetchesAccountsTogether(t *testing.T) {
	const accounts = 4
	const each = 300 * time.Millisecond

	var inflight, peak atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		now := inflight.Add(1)
		for {
			old := peak.Load()
			if now <= old || peak.CompareAndSwap(old, now) {
				break
			}
		}
		time.Sleep(each)
		inflight.Add(-1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"five_hour":{"utilization":10,"resets_at":"2026-07-01T00:00:00Z"}}`))
	}))
	defer srv.Close()
	restore := quota.TestingClaudeURL(srv.URL)
	defer restore()

	dir := t.TempDir()
	root, err := store.Open(filepath.Join(dir, "data"))
	if err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "cred.json")
	if err := os.WriteFile(src, []byte(`{"claudeAiOauth":{"accessToken":"tok"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	a := New(root)
	for range accounts {
		if _, err := a.Import(model.ImportRequest{Provider: model.ProviderClaude, CredentialPath: src}); err != nil {
			t.Fatal(err)
		}
	}

	start := time.Now()
	ov, err := a.Poll()
	elapsed := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	if len(ov.Accounts) != accounts {
		t.Fatalf("accounts: %d", len(ov.Accounts))
	}
	for _, v := range ov.Accounts {
		if v.Reading == nil || !v.Reading.OK {
			t.Fatalf("reading not stored: %+v", v.Reading)
		}
	}
	// All four overlap. Each account makes two calls in sequence, the usage
	// and then its reset list, so together the poll is those two waits.
	// One account after another would be four of them.
	if peak.Load() < accounts {
		t.Fatalf("at most %d accounts were in flight at once", peak.Load())
	}
	if elapsed >= 3*each {
		t.Fatalf("poll took %s, which is the accounts one after another", elapsed)
	}
}
