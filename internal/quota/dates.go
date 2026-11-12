package quota

import (
	"time"

	"github.com/maci0/subman/internal/model"
)

// stateDates writes into reading the subscription dates the provider itself
// states, and nothing it does not:
//
//   - Codex puts the current period's start and end in the id token
//     (chatgpt_subscription_active_start and _until).
//   - Grok's billing payload carries the period end; parseGrok reads it.
//   - Claude's usage payload carries neither.
//
// A period end that is not after now is dropped: it is the end of a period
// that has passed, read from a login not refreshed since, not a renewal.
func stateDates(p model.Provider, cred map[string]any, reading *model.Reading, now time.Time) {
	if p == model.ProviderCodex {
		auth := asMap(jwtClaims(asString(asMap(cred["tokens"])["id_token"]))[codexAuthClaim])
		if start := isoTime(auth["chatgpt_subscription_active_start"]); start != nil {
			reading.ActivatedAt = start.Format(time.RFC3339)
		}
		if until := isoTime(auth["chatgpt_subscription_active_until"]); until != nil {
			reading.RenewalAt = until.Format(time.RFC3339)
		}
	}
	if end := isoTime(reading.RenewalAt); end == nil || !end.After(now) {
		reading.RenewalAt = ""
	}
}
