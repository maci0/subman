package quota

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maci0/subman/internal/model"
)

func TestClaudeRefreshFindsBothShapes(t *testing.T) {
	flat := map[string]any{"access_token": "a", "refresh_token": "r"}
	if got, _ := claudeRefresh(flat); got != "r" {
		t.Fatalf("flat: %q", got)
	}
	nested := map[string]any{"claudeAiOauth": map[string]any{"refreshToken": "n"}}
	if got, _ := claudeRefresh(nested); got != "n" {
		t.Fatalf("nested: %q", got)
	}
	if got, _ := claudeRefresh(map[string]any{}); got != "" {
		t.Fatal("empty cred yielded a token")
	}
}

func TestExpiredFlatAndNested(t *testing.T) {
	past := time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC)
	flat := map[string]any{"expired": "2026-11-10T14:04:52Z"}
	if !expired(model.ProviderClaude, flat, now) {
		t.Fatal("flat past")
	}
	future := map[string]any{"expired": "2026-12-01T00:00:00Z"}
	if expired(model.ProviderClaude, future, now) {
		t.Fatal("flat future")
	}
	nested := map[string]any{"claudeAiOauth": map[string]any{"expiresAt": float64(past.UnixMilli())}}
	if !expired(model.ProviderClaude, nested, now) {
		t.Fatal("nested past")
	}
	if expired(model.ProviderClaude, map[string]any{"access_token": "x"}, now) {
		t.Fatal("no expiry counted as expired")
	}
}

func TestFetchRenewsBeforePolling(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path == "/token" {
			w.Write([]byte(`{"access_token":"fresh","refresh_token":"rotated","expires_in":3600}`))
			return
		}
		if r.Header.Get("Authorization") != "Bearer fresh" {
			w.WriteHeader(401)
			return
		}
		w.Write([]byte(`{"five_hour":{"utilization":10,"resets_at":"2026-11-12T00:00:00Z"}}`))
	}))
	t.Cleanup(srv.Close)
	savedURL, savedTokens := claudeUsageURL, claudeTokenURLs
	claudeUsageURL = srv.URL + "/usage"
	claudeTokenURLs = []string{srv.URL + "/token"}
	t.Cleanup(func() { claudeUsageURL, claudeTokenURLs = savedURL, savedTokens })

	dir := t.TempDir()
	path := filepath.Join(dir, "credential.json")
	body := []byte(`{"access_token":"stale","refresh_token":"old","expired":"2026-11-10T00:00:00Z"}` + "\n")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	reading := Fetch(t.Context(), model.Account{Provider: model.ProviderClaude, CredentialFile: path}, time.Now())
	if !reading.OK || len(reading.Windows) != 1 || *reading.Windows[0].Util != 10 {
		t.Fatalf("%+v", reading)
	}
	if calls < 2 {
		t.Fatalf("expected a refresh and a poll, got %d calls", calls)
	}
	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), `"fresh"`) {
		t.Fatal("the copy was not renewed")
	}
}

func TestRenewWritesTheFlatCopy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		if r.PostForm.Get("grant_type") != "refresh_token" || r.PostForm.Get("refresh_token") != "old" {
			w.WriteHeader(400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"access_token":"new","refresh_token":"rotated","expires_in":3600}`))
	}))
	t.Cleanup(srv.Close)
	saved := claudeTokenURLs
	claudeTokenURLs = []string{srv.URL}
	t.Cleanup(func() { claudeTokenURLs = saved })

	dir := t.TempDir()
	path := filepath.Join(dir, "credential.json")
	orig := []byte(`{"access_token":"expired","refresh_token":"old","email":"a@b.c"}` + "\n")
	if err := os.WriteFile(path, orig, 0o600); err != nil {
		t.Fatal(err)
	}
	acc := model.Account{Provider: model.ProviderClaude, CredentialFile: path}
	if err := Renew(t.Context(), acc); err != nil {
		t.Fatal(err)
	}
	var cred map[string]any
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(body, &cred); err != nil {
		t.Fatal(err)
	}
	if cred["access_token"] != "new" || cred["refresh_token"] != "rotated" {
		t.Fatalf("copy not updated: %v", cred)
	}
	if cred["email"] != "a@b.c" {
		t.Fatal("renew dropped a field")
	}
	exp, _ := cred["expired"].(string)
	when, err := time.Parse(time.RFC3339, exp)
	if err != nil || time.Until(when) < 50*time.Minute {
		t.Fatalf("expiry %q", exp)
	}
	if strings.Contains(string(body), "expired-token") {
		t.Fatal("old token survived")
	}
}
