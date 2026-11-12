package quota

import (
	"encoding/base64"
	"encoding/json"
	"sort"
	"strings"
)

// jwtClaims decodes the payload of a JWT without verifying it. Anything that
// is not a three-part token with a JSON payload returns an empty map.
func jwtClaims(token string) map[string]any {
	claims := map[string]any{}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return claims
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return claims
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return map[string]any{}
	}
	return claims
}

// Identity returns who a credential file is signed in as: its "email" field
// when it has one, else the email claim of its OpenID id_token (top level or
// under "tokens"), else the "email" of an entry one level down, which is
// where the Grok CLI's file keeps it. The token is decoded for display and
// not verified. Empty when the file names nobody.
func Identity(cred map[string]any) string {
	if email := asString(cred["email"]); email != "" {
		return email
	}
	for _, token := range []string{asString(cred["id_token"]), asString(asMap(cred["tokens"])["id_token"])} {
		if email := asString(jwtClaims(token)["email"]); email != "" {
			return email
		}
	}
	// Sorted, so a file with several entries names the same one every time.
	names := make([]string, 0, len(cred))
	for name := range cred {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if email := asString(asMap(cred[name])["email"]); email != "" {
			return email
		}
	}
	return ""
}
