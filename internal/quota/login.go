package quota

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/maci0/subman/internal/model"
)

// Sign-in runs the provider's own OAuth flow from inside subman, the way
// CLIProxyAPI does (github.com/router-for-me/CLIProxyAPI, internal/auth,
// read 2026-11-11): Claude and Codex through an authorization code with
// PKCE and a callback on localhost, Grok through a device code. The
// endpoints, client ids, scopes, and callback addresses below are the ones
// each vendor's CLI is registered with, so they are fixed.
//
// Antigravity has no flow here: subman reads its usage by running the agy
// CLI, not with a token it could mint.

const (
	claudeAuthURL       = "https://claude.ai/oauth/authorize"
	claudeScope         = "org:create_api_key user:profile user:inference user:sessions:claude_code user:mcp_servers user:file_upload"
	claudeCallbackPort  = 54545
	claudeCallbackPath  = "/callback"
	codexAuthURL        = "https://auth.openai.com/oauth/authorize"
	codexScope          = "openid email profile offline_access"
	codexCallbackPort   = 1455
	codexCallbackPath   = "/auth/callback"
	grokClientID        = "b1a00492-073a-47ea-816f-4c329264a828"
	grokScope           = "openid profile email offline_access grok-cli:access api:access"
	deviceGrantType     = "urn:ietf:params:oauth:grant-type:device_code"
	codexAuthClaim      = "https://api.openai.com/auth"
	grokEndpointPrefix  = "https://auth.x.ai/"
	grokCredentialIssue = "https://auth.x.ai"
	// claudeLoginAgent is the User-Agent CLIProxyAPI sends on the code exchange,
	// matching the HTTP client Claude Code itself uses there.
	claudeLoginAgent = "axios/1.15.2"

	// loginRequestTimeout bounds one HTTP exchange with a provider.
	loginRequestTimeout = 30 * time.Second
	// minDevicePoll is the shortest wait between device-code polls when the
	// provider names none (RFC 8628 section 3.5 default).
	minDevicePoll = 5 * time.Second
	// maxDevicePoll caps how far a slow_down may stretch the wait between
	// device-code polls. The server names the step and could otherwise push
	// the interval without a bound for as long as the device code stays valid.
	maxDevicePoll = 60 * time.Second
	// callbackDrain is how long a closing callback server may finish a response.
	callbackDrain = 2 * time.Second
	// maxLoginBody caps a token or discovery answer.
	maxLoginBody = 1 << 20
)

// ErrNoLogin is returned for a provider subman cannot sign in to itself.
var ErrNoLogin = errors.New("this provider has no sign-in here; import its credential file instead")

// CanLogin reports whether StartLogin supports the provider.
func CanLogin(p model.Provider) bool {
	return p == model.ProviderClaude || p == model.ProviderCodex || p == model.ProviderGrok
}

// Login is one sign-in in progress. URL is the page the operator opens.
// UserCode is set for a device flow only: the code that page shows, so the
// operator can check they are approving this request. Close it when done.
type Login struct {
	URL      string
	UserCode string

	wait  func(context.Context) (map[string]any, error)
	close func()
}

// Wait blocks until the operator finishes in the browser, the context ends,
// or the login is closed, and returns the credential file to store.
func (l *Login) Wait(ctx context.Context) ([]byte, error) {
	cred, err := l.wait(ctx)
	if err != nil {
		return nil, err
	}
	body, err := json.MarshalIndent(cred, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(body, '\n'), nil
}

// Close stops the callback listener or the device poll. Safe to call twice.
func (l *Login) Close() { l.close() }

// StartLogin begins a sign-in for the provider. For Claude and Codex it
// listens on the provider's registered localhost port, which fails when
// another program (a vendor CLI mid-login, CLIProxyAPI) holds it.
func StartLogin(ctx context.Context, p model.Provider) (*Login, error) {
	switch p {
	case model.ProviderClaude:
		return startCodeLogin(codeFlow{
			authURL: claudeAuthURL, tokenURL: claudeTokenURLs[0], clientID: claudeClientID, scope: claudeScope,
			port: claudeCallbackPort, path: claudeCallbackPath,
			extra:    url.Values{"code": {"true"}},
			exchange: exchangeClaude,
		})
	case model.ProviderCodex:
		return startCodeLogin(codeFlow{
			authURL: codexAuthURL, tokenURL: codexTokenURL, clientID: codexClientID, scope: codexScope,
			port: codexCallbackPort, path: codexCallbackPath,
			extra:    url.Values{"prompt": {"login"}, "id_token_add_organizations": {"true"}, "codex_cli_simplified_flow": {"true"}},
			exchange: exchangeCodex,
		})
	case model.ProviderGrok:
		return startDeviceLogin(ctx, deviceFlow{
			discoveryURL: grokDiscoveryURL, endpointPrefix: grokEndpointPrefix, clientID: grokClientID, scope: grokScope, minPoll: minDevicePoll,
		})
	default:
		return nil, ErrNoLogin
	}
}

// codeFlow is an authorization-code flow with PKCE and a localhost callback.
type codeFlow struct {
	authURL, tokenURL string
	clientID, scope   string
	// port 0 takes any free port; only tests do that, a provider accepts
	// its registered port alone.
	port     int
	path     string
	extra    url.Values
	exchange func(ctx context.Context, flow codeFlow, code, state, verifier, redirect string) (map[string]any, error)
}

func randomURLSafe(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func startCodeLogin(flow codeFlow) (*Login, error) {
	verifier, err := randomURLSafe(32)
	if err != nil {
		return nil, err
	}
	stateBytes := make([]byte, 16)
	if _, err := rand.Read(stateBytes); err != nil {
		return nil, err
	}
	state := hex.EncodeToString(stateBytes)
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", flow.port))
	if err != nil {
		return nil, fmt.Errorf("the sign-in callback port %d is in use; close the other sign-in and try again: %w", flow.port, err)
	}
	redirect := fmt.Sprintf("http://localhost:%d%s", ln.Addr().(*net.TCPAddr).Port, flow.path)

	type callback struct {
		code string
		err  error
	}
	got := make(chan callback, 1)
	mux := http.NewServeMux()
	mux.HandleFunc(flow.path, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		// A request without the state this sign-in issued is not its browser:
		// another page or a local process guessing the port. It is refused
		// and does not end the sign-in.
		if q.Get("state") != state {
			http.Error(w, "This is not the sign-in subman started.", http.StatusBadRequest)
			return
		}
		var res callback
		switch {
		case q.Get("error") != "":
			res.err = fmt.Errorf("the provider refused the sign-in: %s", q.Get("error"))
		case q.Get("code") == "":
			res.err = errors.New("the browser came back with no code")
		default:
			res.code = q.Get("code")
		}
		message := "Signed in. You can close this tab and go back to subman."
		if res.err != nil {
			message = "Sign-in did not finish: " + res.err.Error() + ". Go back to subman and try again."
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, "<!doctype html><html lang=\"en\"><head><meta charset=\"utf-8\"><title>subman</title></head><body><p>%s</p></body></html>", html.EscapeString(message))
		// Only the first answer counts; a reload of the tab is ignored.
		select {
		case got <- res:
		default:
		}
	})
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: loginRequestTimeout}
	// served closes when Serve returns, which is when the port is actually
	// free again. Close waits for it, so a sign-in started straight after
	// one ends does not find its own callback port still taken.
	served := make(chan struct{})
	go func() {
		defer close(served)
		// Serve returns ErrServerClosed on Close; any other error ends the
		// listener, and Wait then runs into its context.
		_ = srv.Serve(ln)
	}()

	params := url.Values{
		"client_id":             {flow.clientID},
		"response_type":         {"code"},
		"redirect_uri":          {redirect},
		"scope":                 {flow.scope},
		"state":                 {state},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}
	for key, values := range flow.extra {
		params[key] = values
	}
	closed := make(chan struct{})
	var once sync.Once
	closeOnce := func() {
		once.Do(func() {
			close(closed)
			// Shutdown lets the handler finish writing the page the browser
			// is waiting for; Close would cut it off. It runs beside the
			// caller: wait defers closeOnce and returns as soon as the
			// credential is ready, and Shutdown would otherwise wait for the
			// handler that is still on its way out.
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), callbackDrain)
				defer cancel()
				if err := srv.Shutdown(ctx); err != nil {
					_ = srv.Close()
				}
			}()
		})
	}
	return &Login{
		URL: flow.authURL + "?" + params.Encode(),
		wait: func(ctx context.Context) (map[string]any, error) {
			defer closeOnce()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-closed:
				return nil, context.Canceled
			case res := <-got:
				if res.err != nil {
					return nil, res.err
				}
				// Close during the exchange cancels it too, so a cancelled
				// sign-in never returns a credential.
				ctx, cancel := context.WithCancel(ctx)
				defer cancel()
				go func() {
					select {
					case <-closed:
						cancel()
					case <-ctx.Done():
					}
				}()
				cred, err := flow.exchange(ctx, flow, res.code, state, verifier, redirect)
				if ctx.Err() != nil {
					return nil, context.Canceled
				}
				return cred, err
			}
		},
		close: func() {
			closeOnce()
			// Close is the call that ends a sign-in so another can start, and
			// the next one binds this same port. Wait until Serve has
			// returned, which is when the port is actually free.
			<-served
		},
	}, nil
}

// postLogin sends one token or device request and decodes a JSON answer.
// A non-200 answer is an error that carries the OAuth "error" field when the
// body has one, and never the body itself.
func postLogin(ctx context.Context, endpoint, contentType, agent string, body io.Reader, out any) (oauthError string, err error) {
	ctx, cancel := context.WithTimeout(ctx, loginRequestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", agent)
	res, err := noRedirect.Do(req)
	if err != nil {
		return "", fmt.Errorf("%s: %w", hostOf(endpoint), err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxLoginBody))
	if err != nil {
		return "", err
	}
	if res.StatusCode != http.StatusOK {
		var failure struct {
			Error string `json:"error"`
		}
		// A body that is not JSON leaves the field empty; the status still reports.
		_ = json.Unmarshal(raw, &failure)
		if failure.Error != "" {
			return failure.Error, fmt.Errorf("%s answered %d: %s", hostOf(endpoint), res.StatusCode, failure.Error)
		}
		return "", fmt.Errorf("%s answered %d", hostOf(endpoint), res.StatusCode)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return "", fmt.Errorf("%s answered something that is not JSON", hostOf(endpoint))
	}
	return "", nil
}

// expiry is the RFC 3339 instant expiresIn seconds from now, or "" when the
// provider named no lifetime: a file with no expiry is renewed on a 401,
// not on every poll.
func expiry(expiresIn int) string {
	if expiresIn <= 0 {
		return ""
	}
	return time.Now().Add(time.Duration(expiresIn) * time.Second).UTC().Format(time.RFC3339)
}

// exchangeClaude trades the code for tokens and returns the flat file
// renewClaude and fetchClaude already read (the EasyCLI shape).
func exchangeClaude(ctx context.Context, flow codeFlow, code, state, verifier, redirect string) (map[string]any, error) {
	// Anthropic's endpoint takes JSON, and the callback code can carry the
	// state after a '#'.
	code, _, _ = strings.Cut(code, "#")
	request, err := json.Marshal(struct {
		GrantType    string `json:"grant_type"`
		Code         string `json:"code"`
		RedirectURI  string `json:"redirect_uri"`
		ClientID     string `json:"client_id"`
		CodeVerifier string `json:"code_verifier"`
		State        string `json:"state"`
	}{"authorization_code", code, redirect, flow.clientID, verifier, state})
	if err != nil {
		return nil, err
	}
	var tok struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		Account      struct {
			Email string `json:"email_address"`
		} `json:"account"`
		Organization struct {
			UUID string `json:"uuid"`
		} `json:"organization"`
	}
	if _, err := postLogin(ctx, flow.tokenURL, "application/json", claudeLoginAgent, strings.NewReader(string(request)), &tok); err != nil {
		return nil, err
	}
	if tok.AccessToken == "" {
		return nil, fmt.Errorf("%s answered no access token", hostOf(flow.tokenURL))
	}
	return map[string]any{
		"type":              "claude",
		"access_token":      tok.AccessToken,
		"refresh_token":     tok.RefreshToken,
		"email":             tok.Account.Email,
		"organization_uuid": tok.Organization.UUID,
		"expired":           expiry(tok.ExpiresIn),
		"last_refresh":      time.Now().UTC().Format(time.RFC3339),
	}, nil
}

// exchangeCodex trades the code for tokens and returns the Codex CLI's
// auth.json shape, which renewCodex and fetchCodex read.
func exchangeCodex(ctx context.Context, flow codeFlow, code, _, verifier, redirect string) (map[string]any, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {flow.clientID},
		"code":          {code},
		"redirect_uri":  {redirect},
		"code_verifier": {verifier},
	}
	var tok struct {
		IDToken      string `json:"id_token"`
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if _, err := postLogin(ctx, flow.tokenURL, "application/x-www-form-urlencoded", userAgent, strings.NewReader(form.Encode()), &tok); err != nil {
		return nil, err
	}
	if tok.AccessToken == "" {
		return nil, fmt.Errorf("%s answered no access token", hostOf(flow.tokenURL))
	}
	tokens := map[string]any{
		"id_token":      tok.IDToken,
		"access_token":  tok.AccessToken,
		"refresh_token": tok.RefreshToken,
	}
	// The instant expired reads. exchangeClaude stores the same instant as "expired".
	if exp := expiry(tok.ExpiresIn); exp != "" {
		tokens["expires_at"] = exp
	}
	// fetchCodex sends the ChatGPT account id when the file has one.
	if id := asString(asMap(jwtClaims(tok.IDToken)[codexAuthClaim])["chatgpt_account_id"]); id != "" {
		tokens["account_id"] = id
	}
	return map[string]any{"tokens": tokens, "last_refresh": time.Now().UTC().Format(time.RFC3339)}, nil
}

// deviceFlow is an RFC 8628 device authorization, found through discovery.
type deviceFlow struct {
	discoveryURL string
	// endpointPrefix is what both discovered endpoints must start with.
	endpointPrefix  string
	clientID, scope string
	minPoll         time.Duration
}

func startDeviceLogin(ctx context.Context, flow deviceFlow) (*Login, error) {
	ctx, cancel := context.WithTimeout(ctx, loginRequestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, flow.discoveryURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	res, err := noRedirect.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", hostOf(flow.discoveryURL), err)
	}
	defer res.Body.Close()
	var discovery struct {
		DeviceEndpoint string `json:"device_authorization_endpoint"`
		TokenEndpoint  string `json:"token_endpoint"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, maxLoginBody)).Decode(&discovery); err != nil {
		return nil, fmt.Errorf("%s answered something that is not JSON", hostOf(flow.discoveryURL))
	}
	if !strings.HasPrefix(discovery.DeviceEndpoint, flow.endpointPrefix) || !strings.HasPrefix(discovery.TokenEndpoint, flow.endpointPrefix) {
		return nil, errors.New("discovery named an endpoint subman will not use")
	}

	var device struct {
		DeviceCode  string `json:"device_code"`
		UserCode    string `json:"user_code"`
		URL         string `json:"verification_uri"`
		URLComplete string `json:"verification_uri_complete"`
		ExpiresIn   int    `json:"expires_in"`
		Interval    int    `json:"interval"`
	}
	form := url.Values{"client_id": {flow.clientID}, "scope": {flow.scope}}
	if _, err := postLogin(ctx, discovery.DeviceEndpoint, "application/x-www-form-urlencoded", userAgent, strings.NewReader(form.Encode()), &device); err != nil {
		return nil, err
	}
	if device.DeviceCode == "" || device.UserCode == "" || device.URL == "" {
		return nil, fmt.Errorf("%s answered an incomplete device code", hostOf(discovery.DeviceEndpoint))
	}
	page := device.URLComplete
	if page == "" {
		page = device.URL
	}
	interval := time.Duration(device.Interval) * time.Second
	if interval < flow.minPoll {
		interval = flow.minPoll
	}

	closed := make(chan struct{})
	var once sync.Once
	closeOnce := func() { once.Do(func() { close(closed) }) }
	return &Login{
		URL:      page,
		UserCode: device.UserCode,
		close:    closeOnce,
		wait: func(ctx context.Context) (map[string]any, error) {
			defer closeOnce()
			if device.ExpiresIn > 0 {
				var stop context.CancelFunc
				ctx, stop = context.WithTimeout(ctx, time.Duration(device.ExpiresIn)*time.Second)
				defer stop()
			}
			// One timer for the whole poll. time.After in the loop would
			// leave a timer alive until each interval elapsed, and a
			// slow_down that lengthens the wait would leak one per poll.
			timer := time.NewTimer(interval)
			defer timer.Stop()
			poll := url.Values{"grant_type": {deviceGrantType}, "device_code": {device.DeviceCode}, "client_id": {flow.clientID}}
			for {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-closed:
					return nil, context.Canceled
				case <-timer.C:
				}
				var tok struct {
					AccessToken  string `json:"access_token"`
					RefreshToken string `json:"refresh_token"`
					IDToken      string `json:"id_token"`
					ExpiresIn    int    `json:"expires_in"`
				}
				oauthErr, err := postLogin(ctx, discovery.TokenEndpoint, "application/x-www-form-urlencoded", userAgent, strings.NewReader(poll.Encode()), &tok)
				switch {
				case oauthErr == "authorization_pending":
					timer.Reset(interval)
					continue
				case oauthErr == "slow_down":
					interval += flow.minPoll
					if interval > maxDevicePoll {
						interval = maxDevicePoll
					}
					timer.Reset(interval)
					continue
				case err != nil:
					return nil, err
				case tok.AccessToken == "":
					return nil, fmt.Errorf("%s answered no access token", hostOf(discovery.TokenEndpoint))
				}
				// The Grok CLI's auth.json shape: one entry per issuer and
				// client, which grokToken and renewGrok read.
				entry := map[string]any{
					"key":            tok.AccessToken,
					"refresh_token":  tok.RefreshToken,
					"expires_at":     expiry(tok.ExpiresIn),
					"oidc_client_id": flow.clientID,
					"oidc_issuer":    grokCredentialIssue,
				}
				if email := asString(jwtClaims(tok.IDToken)["email"]); email != "" {
					entry["email"] = email
				}
				return map[string]any{grokCredentialIssue + "::" + flow.clientID: entry}, nil
			}
		},
	}, nil
}
