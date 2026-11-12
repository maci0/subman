package quota

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maci0/subman/internal/model"
)

func TestRedeemReset(t *testing.T) {
	var got map[string]string
	var auth, account string
	answer := `{"code":"reset","windows_reset":1}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth, account = r.Header.Get("Authorization"), r.Header.Get("ChatGPT-Account-Id")
		body, _ := io.ReadAll(r.Body)
		got = map[string]string{}
		if err := json.Unmarshal(body, &got); err != nil {
			t.Errorf("request body is not JSON: %s", body)
		}
		io.WriteString(w, answer)
	}))
	defer srv.Close()
	old := codexConsumeResetURL
	codexConsumeResetURL = srv.URL
	defer func() { codexConsumeResetURL = old }()

	cred := filepath.Join(t.TempDir(), "credential.json")
	if err := os.WriteFile(cred, []byte(`{"tokens":{"access_token":"tok","account_id":"acct-1"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	acc := model.Account{ID: "codex-01", Provider: model.ProviderCodex, CredentialFile: cred}
	polled := time.Date(2026, 11, 12, 15, 0, 0, 0, time.UTC)

	if err := RedeemReset(context.Background(), acc, "credit-7", polled); err != nil {
		t.Fatal(err)
	}
	first := got["redeem_request_id"]
	if auth != "Bearer tok" || account != "acct-1" || got["credit_id"] != "credit-7" || first != redeemRequestID(acc.ID, "credit-7", polled) {
		t.Fatalf("request: auth %q account %q body %v", auth, account, got)
	}
	// The same reading retried, after a lost response, is the same redeem.
	if err := RedeemReset(context.Background(), acc, "credit-7", polled); err != nil {
		t.Fatal(err)
	}
	if got["redeem_request_id"] != first {
		t.Fatalf("retry minted a new request id: %q then %q", first, got["redeem_request_id"])
	}
	// A later reading is a new redeem, so a second banked reset can be spent.
	if err := RedeemReset(context.Background(), acc, "credit-7", polled.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if got["redeem_request_id"] == first {
		t.Fatal("a newer reading reused the request id")
	}
	// With no credit named, none is sent and the provider picks.
	if err := RedeemReset(context.Background(), acc, "", polled); err != nil {
		t.Fatal(err)
	}
	if _, named := got["credit_id"]; named {
		t.Fatalf("an empty credit id was sent: %v", got)
	}
	// Every answer but "reset" is an error that says what happened.
	for code, want := range map[string]string{"nothing_to_reset": "no window", "no_credit": "no banked reset", "already_redeemed": "already used", "surprise": "surprise"} {
		answer = `{"code":"` + code + `"}`
		if err := RedeemReset(context.Background(), acc, "", polled); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v", code, err)
		}
	}
	// A provider with no banked resets has no call.
	if err := RedeemReset(context.Background(), model.Account{Provider: model.ProviderGrok, CredentialFile: cred}, "", polled); err == nil || !strings.Contains(err.Error(), "no call") {
		t.Fatal("a Grok account was sent a redeem:", err)
	}
}

func TestRedeemClaudeReset(t *testing.T) {
	var got map[string]string
	var path, agent, app string
	answer := `{"result":"reset","resets_left":0}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			io.WriteString(w, `{"organization":{"uuid":"org-from-profile"}}`)
			return
		}
		path, agent, app = r.URL.Path, r.Header.Get("User-Agent"), r.Header.Get("x-app")
		body, _ := io.ReadAll(r.Body)
		got = map[string]string{}
		if err := json.Unmarshal(body, &got); err != nil {
			t.Errorf("request body is not JSON: %s", body)
		}
		io.WriteString(w, answer)
	}))
	defer srv.Close()
	oldClaim, oldProfile := claudeClaimURL, claudeProfileURL
	claudeClaimURL, claudeProfileURL = srv.URL+"/api/organizations/%s/reset_rate_limits", srv.URL+"/profile"
	defer func() { claudeClaimURL, claudeProfileURL = oldClaim, oldProfile }()

	dir := t.TempDir()
	write := func(name, body string) model.Account {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return model.Account{Provider: model.ProviderClaude, CredentialFile: p}
	}
	signedIn := write("a.json", `{"access_token":"tok","organization_uuid":"org-1"}`)
	signedIn.ID = "claude-01"
	polled := time.Date(2026, 11, 12, 15, 0, 0, 0, time.UTC)
	if err := RedeemReset(context.Background(), signedIn, "full-reset-2611", polled); err != nil {
		t.Fatal(err)
	}
	if path != "/api/organizations/org-1/reset_rate_limits" || got["program"] != "cedar_ember" || got["grant_id"] != "full-reset-2611" || got["request_id"] != redeemRequestID(signedIn.ID, "full-reset-2611", polled) || agent != claudeUA || app != claudeApp {
		t.Fatalf("claim: path %q body %v agent %q app %q", path, got, agent, app)
	}
	// A copied Claude Code file names no organization; the profile does.
	copied := write("b.json", `{"claudeAiOauth":{"accessToken":"tok"}}`)
	copied.ID = "claude-02"
	if err := RedeemReset(context.Background(), copied, "g1", polled); err != nil || path != "/api/organizations/org-from-profile/reset_rate_limits" {
		t.Fatalf("organization from the profile: %v, path %q", err, path)
	}
	// Claude will not pick a reset itself.
	if err := RedeemReset(context.Background(), signedIn, "", polled); err == nil || !strings.Contains(err.Error(), "named") {
		t.Fatalf("no grant named: %v", err)
	}
	for result, want := range map[string]string{"already_used": "already used", "not_limited": "no limit", "cooldown": "too recently", "ineligible": "ineligible"} {
		answer = `{"result":"` + result + `","reason":"tier"}`
		if err := RedeemReset(context.Background(), signedIn, "g1", polled); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v", result, err)
		}
	}
}
