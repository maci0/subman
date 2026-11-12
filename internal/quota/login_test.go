package quota

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func jwtWith(claims string) string {
	return "h." + base64.RawURLEncoding.EncodeToString([]byte(claims)) + ".s"
}

// finish plays the browser: it opens the callback the authorize URL names.
func finish(t *testing.T, login *Login, state, code string) {
	t.Helper()
	authorize, err := url.Parse(login.URL)
	if err != nil {
		t.Fatal(err)
	}
	q := authorize.Query()
	if state == "" {
		state = q.Get("state")
	}
	res, err := http.Get(q.Get("redirect_uri") + "?code=" + url.QueryEscape(code) + "&state=" + url.QueryEscape(state))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
}

func waitCred(t *testing.T, login *Login) (map[string]any, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	body, err := login.Wait(ctx)
	if err != nil {
		return nil, err
	}
	cred := map[string]any{}
	if err := json.Unmarshal(body, &cred); err != nil {
		t.Fatal(err)
	}
	return cred, nil
}

func TestClaudeLoginExchangesCodeWithPKCE(t *testing.T) {
	var got map[string]string
	var agent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		agent = r.Header.Get("User-Agent")
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &got); err != nil {
			t.Errorf("exchange body is not JSON: %s", body)
		}
		io.WriteString(w, `{"access_token":"at","refresh_token":"rt","expires_in":3600,"account":{"uuid":"u1","email_address":"a@example.com"},"organization":{"uuid":"o1","name":"Org"}}`)
	}))
	defer srv.Close()
	login, err := startCodeLogin(codeFlow{
		authURL: "https://example.test/authorize", tokenURL: srv.URL, clientID: "cid", scope: "s1 s2",
		path: "/callback", extra: url.Values{"code": {"true"}}, exchange: exchangeClaude,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer login.Close()
	q, _ := url.Parse(login.URL)
	params := q.Query()
	if params.Get("client_id") != "cid" || params.Get("code_challenge_method") != "S256" || params.Get("code") != "true" || params.Get("scope") != "s1 s2" || len(params.Get("code_challenge")) != 43 {
		t.Fatalf("authorize url %s", login.URL)
	}
	// Anthropic appends the state to the code after a '#'.
	finish(t, login, "", "abc#"+params.Get("state"))
	cred, err := waitCred(t, login)
	if err != nil {
		t.Fatal(err)
	}
	if got["code"] != "abc" || got["grant_type"] != "authorization_code" || got["client_id"] != "cid" || got["redirect_uri"] != params.Get("redirect_uri") || got["state"] != params.Get("state") || len(got["code_verifier"]) != 43 {
		t.Fatalf("exchange %v", got)
	}
	if agent != claudeLoginAgent {
		t.Fatalf("user agent %q", agent)
	}
	if cred["access_token"] != "at" || cred["refresh_token"] != "rt" || cred["email"] != "a@example.com" || cred["organization_uuid"] != "o1" || cred["type"] != "claude" {
		t.Fatalf("credential %v", cred)
	}
	// The file is one fetchClaude, renewClaude, and Identity read.
	if refresh, _ := claudeRefresh(cred); refresh != "rt" || Identity(cred) != "a@example.com" {
		t.Fatal("the stored shape is not the one renew reads")
	}
	if exp := isoTime(cred["expired"]); exp == nil || !exp.After(time.Now()) {
		t.Fatalf("expired %v", cred["expired"])
	}
}

func TestCodexLoginStoresAccountID(t *testing.T) {
	var form url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		form = r.PostForm
		json.NewEncoder(w).Encode(map[string]string{
			"access_token": "at", "refresh_token": "rt",
			"id_token": jwtWith(`{"email":"dev@example.com","https://api.openai.com/auth":{"chatgpt_account_id":"acct-1"}}`),
		})
	}))
	defer srv.Close()
	login, err := startCodeLogin(codeFlow{authURL: "https://example.test/a", tokenURL: srv.URL, clientID: "cid", scope: "openid", path: "/auth/callback", exchange: exchangeCodex})
	if err != nil {
		t.Fatal(err)
	}
	defer login.Close()
	finish(t, login, "", "the-code")
	cred, err := waitCred(t, login)
	if err != nil {
		t.Fatal(err)
	}
	if form.Get("code") != "the-code" || form.Get("grant_type") != "authorization_code" || form.Get("code_verifier") == "" {
		t.Fatalf("exchange %v", form)
	}
	tokens := asMap(cred["tokens"])
	if tokens["access_token"] != "at" || tokens["refresh_token"] != "rt" || tokens["account_id"] != "acct-1" || Identity(cred) != "dev@example.com" {
		t.Fatalf("credential %v", cred)
	}
	if tokens["expires_at"] != nil {
		t.Fatal("an answer with no expires_in stored an expiry")
	}
}

func TestCodexLoginStoresExpiry(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": "at", "refresh_token": "rt", "expires_in": 3600,
			"id_token": jwtWith(`{"email":"dev@example.com","https://api.openai.com/auth":{"chatgpt_account_id":"acct-1"}}`),
		})
	}))
	defer srv.Close()
	login, err := startCodeLogin(codeFlow{authURL: "https://example.test/a", tokenURL: srv.URL, clientID: "cid", scope: "openid", path: "/auth/callback", exchange: exchangeCodex})
	if err != nil {
		t.Fatal(err)
	}
	defer login.Close()
	finish(t, login, "", "the-code")
	cred, err := waitCred(t, login)
	if err != nil {
		t.Fatal(err)
	}
	when, perr := time.Parse(time.RFC3339, asString(asMap(cred["tokens"])["expires_at"]))
	if perr != nil || time.Until(when) < 50*time.Minute {
		t.Fatalf("expires_at %v", cred["tokens"])
	}
}

func TestLoginIgnoresForeignStateAndReportsProviderError(t *testing.T) {
	exchanged := 0
	flow := codeFlow{authURL: "https://example.test/a", tokenURL: "http://127.0.0.1:1/unused", clientID: "c", path: "/cb",
		exchange: func(context.Context, codeFlow, string, string, string, string) (map[string]any, error) {
			exchanged++
			return map[string]any{"ok": true}, nil
		}}
	login, err := startCodeLogin(flow)
	if err != nil {
		t.Fatal(err)
	}
	authorize, _ := url.Parse(login.URL)
	redirect := authorize.Query().Get("redirect_uri")
	// A request with a state this sign-in did not issue is refused and
	// does not end the sign-in: the real browser can still come back.
	res, err := http.Get(redirect + "?code=stolen&state=not-the-state")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("foreign state answered %d", res.StatusCode)
	}
	finish(t, login, "", "abc")
	if cred, err := waitCred(t, login); err != nil || cred["ok"] != true || exchanged != 1 {
		t.Fatalf("the real callback after a foreign one: %v %v, %d exchanges", cred, err, exchanged)
	}

	// The provider's own refusal, with the right state, ends it.
	login, err = startCodeLogin(flow)
	if err != nil {
		t.Fatal(err)
	}
	authorize, _ = url.Parse(login.URL)
	q := authorize.Query()
	res, err = http.Get(q.Get("redirect_uri") + "?error=access_denied&state=" + q.Get("state"))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if _, err := waitCred(t, login); err == nil || !strings.Contains(err.Error(), "access_denied") || exchanged != 1 {
		t.Fatalf("provider error: %v, %d exchanges", err, exchanged)
	}
}

// Close may arrive from another goroutine while Wait is returning. Neither
// order may panic, and a login closed during the exchange returns nothing.
func TestLoginCloseRacesWait(t *testing.T) {
	for i := 0; i < 50; i++ {
		release := make(chan struct{})
		flow := codeFlow{authURL: "https://example.test/a", clientID: "c", path: "/cb",
			exchange: func(ctx context.Context, _ codeFlow, _, _, _, _ string) (map[string]any, error) {
				select {
				case <-release:
				case <-ctx.Done():
				}
				return map[string]any{"ok": true}, nil
			}}
		login, err := startCodeLogin(flow)
		if err != nil {
			t.Fatal(err)
		}
		finish(t, login, "", "abc")
		done := make(chan error, 1)
		go func() {
			_, err := login.Wait(context.Background())
			done <- err
		}()
		go login.Close()
		login.Close()
		if err := <-done; err != context.Canceled {
			t.Fatalf("closed during the exchange: %v", err)
		}
		close(release)
	}
}

func TestLoginReportsABusyPort(t *testing.T) {
	first, err := startCodeLogin(codeFlow{authURL: "https://example.test/a", path: "/cb"})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	u, _ := url.Parse(first.URL)
	redirect, _ := url.Parse(u.Query().Get("redirect_uri"))
	var port int
	if _, err := json.Number(redirect.Port()).Int64(); err != nil {
		t.Fatal(err)
	}
	n, _ := json.Number(redirect.Port()).Int64()
	port = int(n)
	if _, err := startCodeLogin(codeFlow{authURL: "https://example.test/a", path: "/cb", port: port}); err == nil || !strings.Contains(err.Error(), "in use") {
		t.Fatalf("second listener on %d: %v", port, err)
	}
	// Close waits until the listener is gone, so the port is free for the
	// next sign-in the moment it returns.
	first.Close()
	again, err := startCodeLogin(codeFlow{authURL: "https://example.test/a", path: "/cb", port: port})
	if err != nil {
		t.Fatalf("port %d still taken after Close: %v", port, err)
	}
	again.Close()
}

func TestDeviceLoginPollsUntilApproved(t *testing.T) {
	polls := 0
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/discovery":
			json.NewEncoder(w).Encode(map[string]string{"device_authorization_endpoint": srv.URL + "/device", "token_endpoint": srv.URL + "/token"})
		case "/device":
			r.ParseForm()
			if r.PostForm.Get("client_id") != "cid" || r.PostForm.Get("scope") != "scope" {
				t.Errorf("device request %v", r.PostForm)
			}
			io.WriteString(w, `{"device_code":"dc","user_code":"ABCD-1234","verification_uri":"https://example.test/device","verification_uri_complete":"https://example.test/device?user_code=ABCD-1234","expires_in":60,"interval":0}`)
		case "/token":
			r.ParseForm()
			polls++
			if r.PostForm.Get("grant_type") != deviceGrantType || r.PostForm.Get("device_code") != "dc" {
				t.Errorf("token request %v", r.PostForm)
			}
			if polls < 3 {
				w.WriteHeader(http.StatusBadRequest)
				io.WriteString(w, `{"error":"authorization_pending"}`)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"access_token": "at", "refresh_token": "rt", "expires_in": 3600, "id_token": jwtWith(`{"email":"g@example.com"}`)})
		}
	}))
	defer srv.Close()
	flow := deviceFlow{discoveryURL: srv.URL + "/discovery", endpointPrefix: srv.URL + "/", clientID: "cid", scope: "scope", minPoll: time.Millisecond}
	login, err := startDeviceLogin(context.Background(), flow)
	if err != nil {
		t.Fatal(err)
	}
	defer login.Close()
	if login.UserCode != "ABCD-1234" || login.URL != "https://example.test/device?user_code=ABCD-1234" {
		t.Fatalf("%+v", login)
	}
	cred, err := waitCred(t, login)
	if err != nil {
		t.Fatal(err)
	}
	if polls != 3 {
		t.Fatalf("polled %d times", polls)
	}
	// The file is one grokToken and Identity read.
	if key, exp := grokToken(cred); key != "at" || exp == "" || Identity(cred) != "g@example.com" {
		t.Fatalf("credential %v", cred)
	}

	flow.endpointPrefix = "https://auth.x.ai/"
	if _, err := startDeviceLogin(context.Background(), flow); err == nil {
		t.Fatal("endpoints outside the prefix were accepted")
	}
}

func TestAllowedEndpoint(t *testing.T) {
	const prefix = "https://auth.x.ai/"
	if !allowedEndpoint("https://auth.x.ai/oauth/token", prefix) {
		t.Fatal("the token endpoint was refused")
	}
	// Each of these shares the prefix with the trusted endpoint and names a
	// different host, so a prefix check would send the device code there.
	for _, bad := range []string{
		"https://auth.x.ai.evil.example/token",
		"https://evil.example@auth.x.ai/token",
		"http://auth.x.ai/token",
		"https://auth.x.ai.evil.example",
	} {
		if allowedEndpoint(bad, prefix) {
			t.Fatalf("accepted %s", bad)
		}
	}
	// A test server's address is not a DNS name, so the prefix stands.
	if !allowedEndpoint("http://127.0.0.1:1234/token", "http://127.0.0.1:1234/") {
		t.Fatal("local test endpoint refused")
	}
}

func TestBrowserPageAndErrorCode(t *testing.T) {
	if !browserPage("https://example.test/device?user_code=ABCD-1234") {
		t.Fatal("the verification page was refused")
	}
	for _, bad := range []string{
		"http://example.test/device",
		"https://user:pass@example.test/device",
		"javascript:alert(1)",
		"https:///device",
	} {
		if browserPage(bad) {
			t.Fatalf("accepted %s", bad)
		}
	}
	if oauthErrorCode("access_denied") != "access_denied" || oauthErrorCode("nope<script>") != "unknown" {
		t.Fatal("error code filter")
	}
}

func TestStartLoginRefusesAntigravity(t *testing.T) {
	if _, err := StartLogin(context.Background(), "antigravity"); err != ErrNoLogin {
		t.Fatal(err)
	}
	if CanLogin("antigravity") || !CanLogin("claude") || !CanLogin("codex") || !CanLogin("grok") {
		t.Fatal("CanLogin disagrees with StartLogin")
	}
}
