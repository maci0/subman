package quota

import (
	"encoding/base64"
	"testing"
)

func TestIdentity(t *testing.T) {
	jwt := "h." + base64.RawURLEncoding.EncodeToString([]byte(`{"email":"dev@example.com","sub":"1"}`)) + ".sig"
	cases := []struct {
		name string
		cred map[string]any
		want string
	}{
		{"flat email", map[string]any{"email": "a@example.com", "id_token": jwt}, "a@example.com"},
		{"top-level id_token", map[string]any{"id_token": jwt}, "dev@example.com"},
		{"nested id_token", map[string]any{"tokens": map[string]any{"id_token": jwt}}, "dev@example.com"},
		{"grok entry", map[string]any{"https://auth.x.ai::c": map[string]any{"key": "k", "email": "g@example.com"}}, "g@example.com"},
		{"no identity", map[string]any{"claudeAiOauth": map[string]any{"accessToken": "x"}}, ""},
		{"not a jwt", map[string]any{"id_token": "opaque"}, ""},
		{"bad payload", map[string]any{"id_token": "h.!!!.s"}, ""},
		{"payload not json", map[string]any{"id_token": "h." + base64.RawURLEncoding.EncodeToString([]byte("x")) + ".s"}, ""},
		{"empty", map[string]any{}, ""},
		// encoding/json keeps raw invalid UTF-8. It is not a name.
		{"invalid utf-8 email", map[string]any{"email": "a@x\xff"}, ""},
		{"invalid utf-8 entry", map[string]any{"entry": map[string]any{"email": "\xef"}}, ""},
	}
	for _, c := range cases {
		if got := Identity(c.cred); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

func TestSameIdentity(t *testing.T) {
	// "é" as one code point, and the same letter as "e" plus a combining acute.
	nfc, nfd := "Jos\u00e9@example.com", "Jose\u0301@example.com"
	same := [][2]string{
		{nfc, nfd},
		{" " + nfc + " ", nfd},
		{"Dev@Example.com", "dev@example.com"},
		{"İ@example.com", "i\u0307@example.com"},
	}
	for _, pair := range same {
		if !SameIdentity(pair[0], pair[1]) {
			t.Errorf("%q and %q were not the same login", pair[0], pair[1])
		}
	}
	different := [][2]string{
		{"a@example.com", "b@example.com"},
		{"Jose@example.com", nfc},
		{"not-an-email", "Not-An-Email"},
		{"", "a@example.com"},
	}
	for _, pair := range different {
		if SameIdentity(pair[0], pair[1]) {
			t.Errorf("%q and %q were the same login", pair[0], pair[1])
		}
	}
}
