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

// claudeClaimURL spends one of Claude's banked resets for an organization.
// The path, the body, and the answers are the Claude Code CLI's own. It is a
// var so a test can point it at a local server; %s is the organization uuid.
var claudeClaimURL = "https://api.anthropic.com/api/organizations/%s/reset_rate_limits"

// RedeemReset uses one banked reset on the account. creditID names the one
// to use; Claude requires it, and for Codex an empty one lets the provider
// pick. It returns nil only when the provider answers that a limit was
// reset. Claude and Codex have the call; the others report no banked resets.
func RedeemReset(ctx context.Context, acc model.Account, creditID string) error {
	body, err := os.ReadFile(acc.CredentialFile)
	if err != nil {
		return fmt.Errorf("read credential: %w", err)
	}
	cred := map[string]any{}
	if err := json.Unmarshal(body, &cred); err != nil {
		return fmt.Errorf("credential is not json")
	}
	// The request id makes a retry of the same redeem spend one reset, not two.
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		return err
	}
	requestID := hex.EncodeToString(id)
	switch acc.Provider {
	case model.ProviderCodex:
		return redeemCodex(ctx, cred, creditID, requestID)
	case model.ProviderClaude:
		return redeemClaude(ctx, cred, creditID, requestID)
	default:
		return fmt.Errorf("%s has no call to use a banked reset", acc.Provider)
	}
}

// postRedeem sends one redeem request and decodes the JSON answer into out.
func postRedeem(ctx context.Context, endpoint, token string, headers map[string]string, request any, out any) error {
	payload, err := json.Marshal(request)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	res, err := noRedirect.Do(req)
	if err != nil {
		return fmt.Errorf("%s: %w", hostOf(endpoint), err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return err
	}
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("%s answered %d", hostOf(endpoint), res.StatusCode)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("%s answered something that is not JSON", hostOf(endpoint))
	}
	return nil
}

func redeemClaude(ctx context.Context, cred map[string]any, grantID, requestID string) error {
	token := asString(asMap(cred["claudeAiOauth"])["accessToken"])
	if token == "" {
		token = asString(cred["access_token"])
	}
	if token == "" {
		return fmt.Errorf("no access token in the login")
	}
	if grantID == "" {
		return fmt.Errorf("Claude needs the reset to use named, and the last poll itemised none that applies")
	}
	headers := map[string]string{
		"anthropic-beta": "oauth-2025-04-20", "anthropic-version": "2023-06-01",
		"User-Agent": claudeUA, "x-app": claudeApp,
	}
	// The organization is in a login subman signed in for. A copied Claude
	// Code file has none, so the profile is asked.
	org := asString(cred["organization_uuid"])
	if org == "" {
		status, body, err := get(ctx, claudeProfileURL, token, headers)
		if err != nil || status != 200 {
			return fmt.Errorf("could not read which organization the login belongs to")
		}
		var profile map[string]any
		if json.Unmarshal(body, &profile) == nil {
			org = asString(asMap(profile["organization"])["uuid"])
		}
	}
	if org == "" {
		return fmt.Errorf("the login names no organization to reset limits for")
	}
	var answer struct {
		Result string `json:"result"`
		Reason string `json:"reason"`
	}
	request := map[string]string{"program": claudeResetProgram, "grant_id": grantID, "request_id": requestID}
	if err := postRedeem(ctx, fmt.Sprintf(claudeClaimURL, org), token, headers, request, &answer); err != nil {
		return err
	}
	why := ""
	if answer.Reason != "" {
		why = " (" + answer.Reason + ")"
	}
	switch answer.Result {
	case "reset":
		return nil
	case "already_used":
		return fmt.Errorf("that reset was already used")
	case "not_limited":
		return fmt.Errorf("the provider found no limit to reset; the reset was not used")
	case "cooldown":
		return fmt.Errorf("a reset was used too recently; the provider asks to wait")
	default:
		return fmt.Errorf("the provider answered %q%s", answer.Result, why)
	}
}

func redeemCodex(ctx context.Context, cred map[string]any, creditID, requestID string) error {
	tokens := asMap(cred["tokens"])
	token := asString(tokens["access_token"])
	if token == "" {
		return fmt.Errorf("no access token in the login")
	}
	request := map[string]string{"redeem_request_id": requestID}
	if creditID != "" {
		request["credit_id"] = creditID
	}
	headers := map[string]string{}
	if account := asString(tokens["account_id"]); account != "" {
		headers["ChatGPT-Account-Id"] = account
	}
	var answer struct {
		Code string `json:"code"`
	}
	if err := postRedeem(ctx, codexConsumeResetURL, token, headers, request, &answer); err != nil {
		return err
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
