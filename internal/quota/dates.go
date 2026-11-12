package quota

import (
	"time"

	"github.com/maci0/subman/internal/model"
)

// stateDates writes into reading the subscription dates found in the login
// itself, and drops a renewal that has already passed:
//
//   - Codex puts the current period's start and end in the id token
//     (chatgpt_subscription_active_start and _until). The end fills only a
//     renewal the subscription call (codexSubscription) did not state.
//   - Claude's dates come from its profile (claudeProfile), Grok's from its
//     subscription list (grokSubscription), not from here.
//   - Antigravity states none.
//
// A renewal or plan change that is not after now is the end of a period that
// has passed, read from a login not refreshed since, so it is cleared.
func stateDates(p model.Provider, cred map[string]any, reading *model.Reading, now time.Time) {
	if p == model.ProviderCodex {
		auth := asMap(jwtClaims(asString(asMap(cred["tokens"])["id_token"]))[codexAuthClaim])
		if start := isoTime(auth["chatgpt_subscription_active_start"]); start != nil {
			reading.ActivatedAt = start.Format(time.RFC3339)
		}
		if until := isoTime(auth["chatgpt_subscription_active_until"]); until != nil && reading.RenewalAt == "" {
			reading.RenewalAt = until.Format(time.RFC3339)
			reading.RenewalEstimated = false
		}
	}
	if end := isoTime(reading.RenewalAt); end == nil || !end.After(now) {
		reading.RenewalAt = ""
		reading.RenewalEstimated = false
	}
	if at := isoTime(reading.NextPlanAt); at == nil || !at.After(now) {
		reading.NextPlan, reading.NextPlanAt, reading.Ends, reading.NextPrice = "", "", false, nil
	}
}
