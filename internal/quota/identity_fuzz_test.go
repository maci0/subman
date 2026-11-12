package quota

import (
	"encoding/base64"
	"strings"
	"testing"
	"unicode/utf8"
)

// FuzzIdentity feeds arbitrary credential documents and token text to the
// login identity reader. A vendor credential file and the id token inside it
// are untrusted: the email that comes back is the account name, so a
// malformed file must not panic and the name must stay valid UTF-8, bounded,
// and equal to a value the document actually carries.
func FuzzIdentity(f *testing.F) {
	jwt := func(payload string) string {
		return "h." + base64.RawURLEncoding.EncodeToString([]byte(payload)) + ".sig"
	}
	f.Add("a@example.com", jwt(`{"email":"dev@example.com"}`), "", "")
	f.Add("", jwt(`{"email":"dev@example.com"}`), "", "")
	f.Add("", "", jwt(`{"email":"nested@example.com"}`), "")
	f.Add("", "", "", "g@example.com")
	f.Add("", "opaque", "", "")
	f.Add("", "h.!!!.s", "", "")
	f.Add("", "h."+base64.RawURLEncoding.EncodeToString([]byte("x"))+".s", "", "")
	f.Add("", jwt(`{"email":1}`), "", "")
	f.Add("", jwt(`{"email":"`+strings.Repeat("a", 400)+`@x"}`), "", "")
	f.Add("", "", "", "")
	f.Fuzz(func(t *testing.T, email, idToken, nestedToken, entryEmail string) {
		cred := map[string]any{}
		if email != "" {
			cred["email"] = email
		}
		if idToken != "" {
			cred["id_token"] = idToken
		}
		if nestedToken != "" {
			cred["tokens"] = map[string]any{"id_token": nestedToken}
		}
		if entryEmail != "" {
			cred["entry"] = map[string]any{"email": entryEmail}
		}
		assertIdentity(t, cred, Identity(cred))

		// The token itself is the untrusted input: claims must come only
		// from a payload that decodes as a JSON object.
		claims := jwtClaims(idToken)
		if email, ok := claims["email"].(string); ok {
			if !utf8.ValidString(email) {
				t.Fatalf("email claim is not valid UTF-8: %q", email)
			}
		}
	})
}

func assertIdentity(t *testing.T, cred map[string]any, got string) {
	t.Helper()
	if !utf8.ValidString(got) {
		t.Fatalf("identity %q is not valid UTF-8", got)
	}
	if utf8.RuneCountInString(got) > 4096 {
		t.Fatalf("identity is %d runes", utf8.RuneCountInString(got))
	}
	if got == "" {
		return
	}
	if got == asString(cred["email"]) {
		return
	}
	for _, token := range []string{asString(cred["id_token"]), asString(asMap(cred["tokens"])["id_token"])} {
		if got == asString(jwtClaims(token)["email"]) {
			return
		}
	}
	for _, v := range cred {
		if got == asString(asMap(v)["email"]) {
			return
		}
	}
	t.Fatalf("identity %q is not a value the credential carries", got)
}
