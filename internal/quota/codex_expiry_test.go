package quota

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/maci0/subman/internal/model"
)

// A Codex refresh answer carries expires_in. The copy must keep it as the
// instant expired reads, and keep the fields the file already had.
func TestRenewCodexStoresExpiry(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		if r.PostForm.Get("grant_type") != "refresh_token" || r.PostForm.Get("refresh_token") != "old" || r.PostForm.Get("client_id") == "" {
			w.WriteHeader(400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"new","refresh_token":"rotated","expires_in":3600,"id_token":"id"}`))
	}))
	t.Cleanup(srv.Close)
	saved := codexTokenURL
	codexTokenURL = srv.URL
	t.Cleanup(func() { codexTokenURL = saved })

	dir := t.TempDir()
	path := filepath.Join(dir, "credential.json")
	orig := []byte(`{"tokens":{"access_token":"old","refresh_token":"old","account_id":"acct-1"}}` + "\n")
	if err := os.WriteFile(path, orig, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Renew(t.Context(), model.Account{Provider: model.ProviderCodex, CredentialFile: path}); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cred map[string]any
	if err := json.Unmarshal(body, &cred); err != nil {
		t.Fatal(err)
	}
	tokens := asMap(cred["tokens"])
	if tokens["access_token"] != "new" || tokens["refresh_token"] != "rotated" || tokens["id_token"] != "id" || tokens["account_id"] != "acct-1" {
		t.Fatalf("copy not updated: %v", cred)
	}
	when, perr := time.Parse(time.RFC3339, asString(tokens["expires_at"]))
	if perr != nil || time.Until(when) < 50*time.Minute {
		t.Fatalf("expires_at %v", tokens["expires_at"])
	}
}
