package quota

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/maci0/subman/internal/model"
)

// codexConsumeResetURL spends one banked reset. The path and the request are
// the Codex CLI's (backend-client, consume_rate_limit_reset_credit). It is a
// var so a test can point it at a local server.
var codexConsumeResetURL = "https://chatgpt.com/backend-api/wham/rate-limit-reset-credits/consume"

// RedeemReset uses one banked reset on the account, the one named by
// creditID or, when that is empty, whichever the provider picks. It returns
// nil only when the provider answers that a window was reset. Only Codex has
// a redeem call.
func RedeemReset(ctx context.Context, acc model.Account, creditID string) error {
	if acc.Provider != model.ProviderCodex {
		return fmt.Errorf("%s has no call to use a banked reset", acc.Provider)
	}
	body, err := os.ReadFile(acc.CredentialFile)
	if err != nil {
		return fmt.Errorf("read credential: %w", err)
	}
	cred := map[string]any{}
	if err := json.Unmarshal(body, &cred); err != nil {
		return fmt.Errorf("credential is not json")
	}
	tokens := asMap(cred["tokens"])
	token := asString(tokens["access_token"])
	if token == "" {
		return fmt.Errorf("no access token in the login")
	}
	// The request id makes a retry of the same redeem spend one reset, not two.
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		return err
	}
	request := map[string]string{"redeem_request_id": hex.EncodeToString(id)}
	if creditID != "" {
		request["credit_id"] = creditID
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, codexConsumeResetURL, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	if account := asString(tokens["account_id"]); account != "" {
		req.Header.Set("ChatGPT-Account-Id", account)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("%s: %w", hostOf(codexConsumeResetURL), err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return err
	}
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("%s answered %d", hostOf(codexConsumeResetURL), res.StatusCode)
	}
	var answer struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(raw, &answer); err != nil {
		return fmt.Errorf("%s answered something that is not JSON", hostOf(codexConsumeResetURL))
	}
	switch answer.Code {
	case "reset":
		return nil
	case "nothing_to_reset":
		return fmt.Errorf("the provider found no window to reset; the reset was not used")
	case "no_credit":
		return fmt.Errorf("the provider has no banked reset for this account")
	case "already_redeemed":
		return fmt.Errorf("that reset was already used")
	default:
		return fmt.Errorf("the provider answered %q", answer.Code)
	}
}
