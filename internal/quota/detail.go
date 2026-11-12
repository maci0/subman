package quota

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"github.com/maci0/subman/internal/model"
)

// Second calls a poll makes after the usage call, for what the usage payload
// does not carry. Each is read-only, and a failure leaves the reading as the
// usage call made it. They are vars so a test can point them at a local server.
var (
	// claudeProfileURL states the plan and when the subscription began.
	claudeProfileURL = "https://api.anthropic.com/api/oauth/profile"
	// codexResetCreditsURL itemises banked resets: type, title, expiry. The
	// path is the Codex CLI's (backend-client, rate_limit_reset_credits_url).
	codexResetCreditsURL = "https://chatgpt.com/backend-api/wham/rate-limit-reset-credits"
)

// claudeProfile reads Claude's profile and returns the plan, the tier, and the
// subscription start it states. Claude states no renewal date, so one is
// estimated as the next monthly anniversary of the start and marked as an
// estimate. A failure returns a zero reading.
func claudeProfile(ctx context.Context, token string, now time.Time) model.Reading {
	status, body, err := get(ctx, claudeProfileURL, token, map[string]string{
		"anthropic-beta":    "oauth-2025-04-20",
		"anthropic-version": "2023-06-01",
		"User-Agent":        claudeUA,
		"x-app":             claudeApp,
	})
	var r model.Reading
	if err != nil || status != 200 {
		return r
	}
	var doc map[string]any
	if json.Unmarshal(body, &doc) != nil {
		return r
	}
	org := asMap(doc["organization"])
	if asString(org["subscription_status"]) != "active" {
		return r
	}
	if plan := claudePlan(asString(org["plan_display_name"])+" "+asString(org["organization_type"]), asString(org["rate_limit_tier"])); plan != "" {
		r.Plan = plan
		r.TierID = matchTier(model.ProviderClaude, plan)
	}
	if began := isoTime(org["subscription_created_at"]); began != nil {
		r.ActivatedAt = began.Format(time.RFC3339)
		r.RenewalAt = nextMonthly(*began, now).Format(time.RFC3339)
		r.RenewalEstimated = true
	}
	return r
}

// nextMonthly is the first monthly anniversary of began that is after now.
// A start on the 31st lands on the last day of a shorter month.
func nextMonthly(began, now time.Time) time.Time {
	for months := 1; ; months++ {
		year, month := began.Year(), began.Month()+time.Month(months)
		next := time.Date(year, month, began.Day(), began.Hour(), began.Minute(), began.Second(), 0, time.UTC)
		// time.Date rolls 31 February into March; step back to the month's last day.
		if want := (int(began.Month())-1+months)%12 + 1; int(next.Month()) != want {
			next = time.Date(next.Year(), next.Month(), 0, began.Hour(), began.Minute(), began.Second(), 0, time.UTC)
		}
		if next.After(now) {
			return next
		}
	}
}

// codexResets itemises the banked resets that can still be used and returns
// them. A failure returns nil. Credits that expire first come first, and ones
// that never expire last.
func codexResets(ctx context.Context, token string, headers map[string]string) []model.ResetCredit {
	status, body, err := get(ctx, codexResetCreditsURL, token, headers)
	if err != nil || status != 200 {
		return nil
	}
	var doc struct {
		Credits []struct {
			ID        string `json:"id"`
			ResetType string `json:"reset_type"`
			Status    string `json:"status"`
			GrantedAt any    `json:"granted_at"`
			ExpiresAt any    `json:"expires_at"`
			Title     string `json:"title"`
		} `json:"credits"`
	}
	if json.Unmarshal(body, &doc) != nil {
		return nil
	}
	resets := []model.ResetCredit{}
	for _, c := range doc.Credits {
		// The list keeps redeemed and expired credits for its history.
		if c.Status != "" && c.Status != "available" {
			continue
		}
		resets = append(resets, model.ResetCredit{
			ID: c.ID, Type: c.ResetType, Title: trimLabel(c.Title), Status: c.Status,
			GrantedAt: isoTime(c.GrantedAt), ExpiresAt: isoTime(c.ExpiresAt), Left: 1, Applies: true,
		})
	}
	sort.SliceStable(resets, func(i, j int) bool {
		a, b := resets[i].ExpiresAt, resets[j].ExpiresAt
		return a != nil && (b == nil || a.Before(*b))
	})
	return resets
}

// claudeResetProgram is the name Claude gives its banked-reset programme in
// the usage query and in the claim body.
const claudeResetProgram = "cedar_ember"

// maxResetTitle caps a reset's title. The providers' own labels are whole
// sentences, longer than an account name.
const maxResetTitle = 160

// claudeResets itemises Claude's banked resets and returns them. The usage
// endpoint lists them only when asked with the programme flag and as the CLI
// surface; the query and the fields are the Claude Code CLI's own. A reset
// "applies" when it can be used now and clears the limit that is fullest. A
// failure returns a zero list.
func claudeResets(ctx context.Context, token string) model.Credits {
	status, body, err := get(ctx, claudeUsageURL+"?"+claudeResetProgram+"=1&skip_spend=1", token, map[string]string{
		"anthropic-beta":    "oauth-2025-04-20",
		"anthropic-version": "2023-06-01",
		"User-Agent":        claudeUA,
		"x-app":             claudeApp,
	})
	if err != nil || status != 200 {
		return model.Credits{}
	}
	var doc struct {
		Status *struct {
			Eligible bool `json:"eligible"`
			Grants   []struct {
				ID          string             `json:"id"`
				Label       string             `json:"label"`
				ResetsLeft  int                `json:"resets_left"`
				EndsAt      any                `json:"ends_at"`
				StartsAt    any                `json:"starts_at"`
				Clears      []string           `json:"clears"`
				Paused      bool               `json:"paused"`
				UsableNow   bool               `json:"usable_now"`
				Type        string             `json:"type"`
				PercentUsed map[string]float64 `json:"percent_used"`
			} `json:"grants"`
		} `json:"cedar_ember"`
	}
	if json.Unmarshal(body, &doc) != nil || doc.Status == nil || !doc.Status.Eligible {
		return model.Credits{}
	}
	resets := []model.ResetCredit{}
	total, applies := 0, 0
	for _, g := range doc.Status.Grants {
		if g.ResetsLeft < 1 {
			continue
		}
		// The fullest of the limits the provider reports for this grant.
		fullest, most := "", -1.0
		for limit, pct := range g.PercentUsed {
			if pct > most || (pct == most && limit < fullest) {
				fullest, most = limit, pct
			}
		}
		clearsFullest := false
		for _, limit := range g.Clears {
			clearsFullest = clearsFullest || limit == fullest
		}
		title := model.TruncateRunes(g.Label, maxResetTitle)
		credit := model.ResetCredit{
			ID: g.ID, Type: g.Type, Title: title, GrantedAt: isoTime(g.StartsAt), ExpiresAt: isoTime(g.EndsAt),
			Left: g.ResetsLeft, Clears: g.Clears, Applies: g.UsableNow && !g.Paused && clearsFullest,
		}
		resets = append(resets, credit)
		total += g.ResetsLeft
		if credit.Applies {
			applies += g.ResetsLeft
		}
	}
	sort.SliceStable(resets, func(i, j int) bool {
		a, b := resets[i].ExpiresAt, resets[j].ExpiresAt
		return a != nil && (b == nil || a.Before(*b))
	})
	return model.Credits{Resets: resets, ResetCount: &total, ResetApplies: &applies}
}
