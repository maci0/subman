package quota

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/maci0/subman/internal/model"
	"github.com/maci0/subman/internal/sandbox"
)

const (
	maxBody  = 4 << 20
	claudeUA = "claude-code/2.1.251"

	grokBillingURL = "https://cli-chat-proxy.grok.com/v1/billing"
	userAgent      = "subman/0.1"
	defaultTimeout = 20 * time.Second
)

// claudeUsageURL is a var so a test can point it at a local server.
var claudeUsageURL = "https://api.anthropic.com/api/oauth/usage"

// codexUsageURL is a var so a test can point it at a local server.
var codexUsageURL = "https://chatgpt.com/backend-api/wham/usage"

// Fetch reads the account's own credential file and polls its provider.
//
// When the copy's token is expired, or the usage call answers 401, Fetch
// refreshes that copy and polls once more. The refresh writes only the
// account's credential file. The vendor CLI's own file is not opened, so
// a rotated refresh token lives in the copy from then on.
func Fetch(ctx context.Context, acc model.Account, now time.Time) model.Reading {
	reading := fetchRenewing(ctx, acc, now)
	// The identity comes from the credential copy, so a failed poll still names the login.
	if body, err := os.ReadFile(acc.CredentialFile); err == nil {
		cred := map[string]any{}
		if json.Unmarshal(body, &cred) == nil {
			reading.Account = Identity(cred)
			stateDates(acc.Provider, cred, &reading, now)
		}
	}
	return reading
}

func fetchRenewing(ctx context.Context, acc model.Account, now time.Time) model.Reading {
	body, err := os.ReadFile(acc.CredentialFile)
	if err != nil {
		return fail("no-token", now)
	}
	cred := map[string]any{}
	if err := json.Unmarshal(body, &cred); err != nil {
		return fail("no-token", now)
	}
	ctx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()

	if expired(acc.Provider, cred, now) {
		if err := Renew(ctx, acc); err != nil {
			return fail("expired", now)
		}
		return fetch(ctx, acc, now)
	}
	reading := fetch(ctx, acc, now)
	if reading.Error == "http-401" {
		if err := Renew(ctx, acc); err != nil {
			return reading
		}
		return fetch(ctx, acc, now)
	}
	return reading
}

func fetch(ctx context.Context, acc model.Account, now time.Time) model.Reading {
	body, err := os.ReadFile(acc.CredentialFile)
	if err != nil {
		return fail("no-token", now)
	}
	cred := map[string]any{}
	if err := json.Unmarshal(body, &cred); err != nil {
		return fail("no-token", now)
	}
	switch acc.Provider {
	case model.ProviderClaude:
		return fetchClaude(ctx, cred, now)
	case model.ProviderCodex:
		return fetchCodex(ctx, cred, now)
	case model.ProviderGrok:
		return fetchGrok(ctx, cred, now)
	case model.ProviderAntigravity:
		return fetchAntigravity(ctx, acc, now)
	default:
		return fail("unknown-provider", now)
	}
}

// expired reports whether the copy names a moment that has already passed.
// A copy with no expiry is not expired: the 401 path covers that case.
func expired(p model.Provider, cred map[string]any, now time.Time) bool {
	switch p {
	case model.ProviderClaude:
		oauth := asMap(cred["claudeAiOauth"])
		if v := finite(oauth["expiresAt"]); v != nil {
			if t := unixMaybe(*v); t != nil {
				return !t.After(now)
			}
		}
		if t := isoTime(cred["expired"]); t != nil {
			return !t.After(now)
		}
	case model.ProviderGrok:
		_, exp := grokToken(cred)
		if t := isoTime(exp); t != nil {
			return !t.After(now)
		}
	}
	return false
}

func fetchClaude(ctx context.Context, cred map[string]any, now time.Time) model.Reading {
	oauth := asMap(cred["claudeAiOauth"])
	token := asString(oauth["accessToken"])
	if token == "" {
		// An imported proxy file keeps the token at the top level.
		token = asString(cred["access_token"])
	}
	if token == "" {
		return fail("no-token", now)
	}
	status, body, err := get(ctx, claudeUsageURL, token, map[string]string{
		"anthropic-beta":    "oauth-2025-04-20",
		"anthropic-version": "2023-06-01",
		"User-Agent":        claudeUA,
	})
	if err != nil {
		return fail("net", now)
	}
	if status != 200 {
		return fail(fmt.Sprintf("http-%d", status), now)
	}
	r := Parse(model.ProviderClaude, body, now)
	claudeProfile(ctx, token, &r, now)
	if r.Plan == "" {
		r.Plan = claudePlan(asString(oauth["subscriptionType"]), asString(oauth["rateLimitTier"]))
		r.TierID = matchTier(model.ProviderClaude, r.Plan)
		r.OK = r.OK || r.Plan != ""
	}
	return r
}

func fetchCodex(ctx context.Context, cred map[string]any, now time.Time) model.Reading {
	tokens := asMap(cred["tokens"])
	token := asString(tokens["access_token"])
	if token == "" {
		return fail("no-token", now)
	}
	headers := map[string]string{}
	if id := asString(tokens["account_id"]); id != "" {
		headers["ChatGPT-Account-Id"] = id
	}
	status, body, err := get(ctx, codexUsageURL, token, headers)
	if err != nil {
		return fail("net", now)
	}
	if status != 200 {
		return fail(fmt.Sprintf("http-%d", status), now)
	}
	r := Parse(model.ProviderCodex, body, now)
	if r.OK {
		codexResets(ctx, token, headers, &r)
	}
	return r
}

func fetchGrok(ctx context.Context, cred map[string]any, now time.Time) model.Reading {
	token, _ := grokToken(cred)
	if token == "" {
		return fail("no-token", now)
	}
	status, body, err := get(ctx, grokBillingURL+"?format=credits", token, nil)
	if err != nil {
		return fail("net", now)
	}
	if status != 200 {
		return fail(fmt.Sprintf("http-%d", status), now)
	}
	weekly := Parse(model.ProviderGrok, body, now)
	status2, body2, err := get(ctx, grokBillingURL, token, nil)
	if err == nil && status2 == 200 {
		monthly := Parse(model.ProviderGrok, body2, now)
		if monthly.OK {
			weekly.Windows = append(weekly.Windows, monthly.Windows...)
			if monthly.Plan != "" && monthly.Plan != "Grok" {
				weekly.Plan = monthly.Plan
				weekly.TierID = monthly.TierID
			}
			weekly.OK = true
		}
	}
	return weekly
}

// grokToken picks the access token with the latest expires_at.
// The auth file is a map of session name to {key, expires_at}.
func grokToken(cred map[string]any) (string, string) {
	var bestKey, bestExp, bestName string
	for name, raw := range cred {
		entry := asMap(raw)
		key := asString(entry["key"])
		if key == "" {
			continue
		}
		exp := asString(entry["expires_at"])
		// A tie on the expiry goes to the lower entry name, so two polls agree.
		if bestKey == "" || exp > bestExp || (exp == bestExp && name < bestName) {
			bestName = name
			bestKey = key
			bestExp = exp
		}
	}
	return bestKey, bestExp
}

func fetchAntigravity(ctx context.Context, acc model.Account, now time.Time) model.Reading {
	bin := acc.AgentBin
	if bin == "" {
		bin = "agy"
	}
	out, err := sandbox.Run(ctx, sandbox.Command{
		Dir: acc.AgentHome, Writable: acc.AgentHome,
		Args:  []string{bin, "--print", "/usage", "--output-format", "json", "--print-timeout", "15s"},
		Extra: []string{"AGY_CLI_DISABLE_AUTO_UPDATE=true"},
	})
	if err != nil {
		var run *sandbox.RunError
		if errors.As(err, &run) {
			return fail("sandbox", now)
		}
		return fail("net", now)
	}
	if len(out) > maxBody {
		return fail("bad-body", now)
	}
	return Parse(model.ProviderAntigravity, out, now)
}

func get(ctx context.Context, url, token string, extra map[string]string) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	for k, v := range extra {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, maxBody+1))
	if err != nil {
		return res.StatusCode, nil, err
	}
	if len(body) > maxBody {
		return res.StatusCode, nil, fmt.Errorf("body over %d bytes", maxBody)
	}
	if loc := res.Header.Get("Location"); loc != "" && !strings.HasPrefix(loc, "https://") {
		return res.StatusCode, nil, fmt.Errorf("refused redirect")
	}
	return res.StatusCode, body, nil
}
