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
	acc := model.Account{Provider: model.ProviderCodex, CredentialFile: cred}

	if err := RedeemReset(context.Background(), acc, "credit-7"); err != nil {
		t.Fatal(err)
	}
	if auth != "Bearer tok" || account != "acct-1" || got["credit_id"] != "credit-7" || len(got["redeem_request_id"]) != 32 {
		t.Fatalf("request: auth %q account %q body %v", auth, account, got)
	}
	// With no credit named, none is sent and the provider picks.
	if err := RedeemReset(context.Background(), acc, ""); err != nil {
		t.Fatal(err)
	}
	if _, named := got["credit_id"]; named {
		t.Fatalf("an empty credit id was sent: %v", got)
	}
	// Every answer but "reset" is an error that says what happened.
	for code, want := range map[string]string{"nothing_to_reset": "no window", "no_credit": "no banked reset", "already_redeemed": "already used", "surprise": "surprise"} {
		answer = `{"code":"` + code + `"}`
		if err := RedeemReset(context.Background(), acc, ""); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v", code, err)
		}
	}
	// Only Codex has the call.
	if err := RedeemReset(context.Background(), model.Account{Provider: model.ProviderClaude, CredentialFile: cred}, ""); err == nil {
		t.Fatal("a Claude account was sent a redeem")
	}
}
