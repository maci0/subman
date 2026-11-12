package quota

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/maci0/subman/internal/model"
)

const (
	claudeClientID   = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
	codexClientID    = "app_EMoamEEZ73f0CkXaXp7hrann"
	grokDiscoveryURL = "https://auth.x.ai/.well-known/openid-configuration"
)

// codexTokenURL is a var so a test can point the refresh at a local server,
// the same way claudeTokenURLs is.
var codexTokenURL = "https://auth.openai.com/oauth/token"

// claudeTokenURLs is tried in order. Anthropic moved the token endpoint;
// a refresh minted before the move still answers at the old one.
var claudeTokenURLs = []string{
	"https://platform.claude.com/v1/oauth/token",
	"https://console.anthropic.com/v1/oauth/token",
}

// Renew exchanges the refresh token in the account's own credential copy
// and writes the new tokens back to that copy. The vendor CLI's file is
// not opened. A provider rotates the refresh token in the answer, so the
// copy and the CLI's file diverge after this: the next renew has to use
// the copy.
func Renew(ctx context.Context, acc model.Account) error {
	body, err := os.ReadFile(acc.CredentialFile)
	if err != nil {
		return fmt.Errorf("read credential: %w", err)
	}
	var cred map[string]any
	if err := json.Unmarshal(body, &cred); err != nil {
		return fmt.Errorf("credential is not json")
	}
	switch acc.Provider {
	case model.ProviderClaude:
		return renewClaude(ctx, acc, cred)
	case model.ProviderCodex:
		return renewCodex(ctx, acc, cred)
	case model.ProviderGrok:
		return renewGrok(ctx, acc, cred)
	default:
		return fmt.Errorf("%s has no token refresh", acc.Provider)
	}
}

func renewClaude(ctx context.Context, acc model.Account, cred map[string]any) error {
	refresh, write := claudeRefresh(cred)
	if refresh == "" {
		return fmt.Errorf("no refresh token in the copy")
	}
	var last error
	for _, url := range claudeTokenURLs {
		tok, err := postForm(ctx, url, map[string]string{
			"grant_type":    "refresh_token",
			"refresh_token": refresh,
			"client_id":     claudeClientID,
		})
		if err != nil {
			last = err
			continue
		}
		return write(acc, cred, tok)
	}
	if last == nil {
		last = fmt.Errorf("no token endpoint answered")
	}
	return last
}

// claudeRefresh finds the refresh token in either shape subman stores:
// the Claude Code file (claudeAiOauth.refreshToken) or an EasyCLI oauth
// file (refresh_token at the top). write puts the answer back in the
// same shape.
func claudeRefresh(cred map[string]any) (string, func(model.Account, map[string]any, token) error) {
	if oauth := asMap(cred["claudeAiOauth"]); asString(oauth["refreshToken"]) != "" {
		return asString(oauth["refreshToken"]), writeClaudeCode
	}
	if asString(cred["refresh_token"]) != "" {
		return asString(cred["refresh_token"]), writeFlat
	}
	return "", nil
}

func writeClaudeCode(acc model.Account, cred map[string]any, tok token) error {
	oauth := asMap(cred["claudeAiOauth"])
	oauth["accessToken"] = tok.Access
	if tok.Refresh != "" {
		oauth["refreshToken"] = tok.Refresh
	}
	if when := expiresAt(tok.ExpiresIn); when != nil {
		oauth["expiresAt"] = float64(when.UnixMilli())
	}
	cred["claudeAiOauth"] = oauth
	return saveCred(acc, cred)
}

func writeFlat(acc model.Account, cred map[string]any, tok token) error {
	cred["access_token"] = tok.Access
	if tok.Refresh != "" {
		cred["refresh_token"] = tok.Refresh
	}
	if exp := expiry(tok.ExpiresIn); exp != "" {
		cred["expired"] = exp
	}
	return saveCred(acc, cred)
}

func renewCodex(ctx context.Context, acc model.Account, cred map[string]any) error {
	tokens := asMap(cred["tokens"])
	refresh := asString(tokens["refresh_token"])
	if refresh == "" {
		return fmt.Errorf("no refresh token in the copy")
	}
	tok, err := postForm(ctx, codexTokenURL, map[string]string{
		"grant_type":    "refresh_token",
		"refresh_token": refresh,
		"client_id":     codexClientID,
	})
	if err != nil {
		return err
	}
	tokens["access_token"] = tok.Access
	// The id token carries the subscription period, so a renewed login keeps it current.
	if tok.IDToken != "" {
		tokens["id_token"] = tok.IDToken
	}
	if tok.Refresh != "" {
		tokens["refresh_token"] = tok.Refresh
	}
	// The same instant exchangeCodex stored, so the next poll can see it has passed.
	if exp := expiry(tok.ExpiresIn); exp != "" {
		tokens["expires_at"] = exp
	}
	cred["tokens"] = tokens
	return saveCred(acc, cred)
}

func renewGrok(ctx context.Context, acc model.Account, cred map[string]any) error {
	var name string
	var entry map[string]any
	var best string
	for key, raw := range cred {
		e := asMap(raw)
		if asString(e["refresh_token"]) == "" {
			continue
		}
		exp := asString(e["expires_at"])
		if entry == nil || exp > best {
			name, entry, best = key, e, exp
		}
	}
	if entry == nil {
		return fmt.Errorf("no refresh token in the copy")
	}
	clientID := asString(entry["oidc_client_id"])
	if clientID == "" {
		if _, id, ok := strings.Cut(name, "::"); ok {
			clientID = id
		}
	}
	if clientID == "" {
		return fmt.Errorf("no client id in the grok entry")
	}
	tokenURL, err := grokTokenURL(ctx)
	if err != nil {
		return err
	}
	tok, err := postForm(ctx, tokenURL, map[string]string{
		"grant_type":    "refresh_token",
		"refresh_token": asString(entry["refresh_token"]),
		"client_id":     clientID,
	})
	if err != nil {
		return err
	}
	entry["key"] = tok.Access
	if tok.Refresh != "" {
		entry["refresh_token"] = tok.Refresh
	}
	if exp := expiry(tok.ExpiresIn); exp != "" {
		entry["expires_at"] = exp
	}
	cred[name] = entry
	return saveCred(acc, cred)
}

func grokTokenURL(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, grokDiscoveryURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	res, err := noRedirect.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return "", err
	}
	var doc struct {
		TokenEndpoint string `json:"token_endpoint"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return "", err
	}
	if !strings.HasPrefix(doc.TokenEndpoint, "https://auth.x.ai/") {
		return "", fmt.Errorf("discovery named a token endpoint subman will not use")
	}
	return doc.TokenEndpoint, nil
}

type token struct {
	Access    string
	Refresh   string
	ExpiresIn int
	// IDToken is set when the provider reissues it with the access token.
	IDToken string
}

func postForm(ctx context.Context, url string, fields map[string]string) (token, error) {
	form := urlEncode(fields)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(form))
	if err != nil {
		return token{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	res, err := noRedirect.Do(req)
	if err != nil {
		return token{}, fmt.Errorf("%s: %w", hostOf(url), err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return token{}, err
	}
	if res.StatusCode != 200 {
		return token{}, fmt.Errorf("%s answered %d", hostOf(url), res.StatusCode)
	}
	var raw struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		IDToken      string `json:"id_token"`
	}
	if err := json.Unmarshal(body, &raw); err != nil || raw.AccessToken == "" {
		return token{}, fmt.Errorf("%s answered no access token", hostOf(url))
	}
	return token{Access: raw.AccessToken, Refresh: raw.RefreshToken, ExpiresIn: raw.ExpiresIn, IDToken: raw.IDToken}, nil
}

// urlEncode encodes the three fields a refresh posts, in that order.
func urlEncode(fields map[string]string) string {
	form := url.Values{}
	for _, key := range []string{"grant_type", "refresh_token", "client_id"} {
		form.Set(key, fields[key])
	}
	return form.Encode()
}

func hostOf(url string) string {
	rest := strings.TrimPrefix(url, "https://")
	host, _, _ := strings.Cut(rest, "/")
	return host
}

func saveCred(acc model.Account, cred map[string]any) error {
	body, err := json.MarshalIndent(cred, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	tmp := acc.CredentialFile + ".tmp"
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, acc.CredentialFile); err != nil {
		// The copy in place is still the last good credential. A leftover
		// temp would be what the next renewal renames over it.
		os.Remove(tmp)
		return err
	}
	return nil
}
