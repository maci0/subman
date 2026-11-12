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

// claudeProfile fills the plan, the tier, and the subscription start from
// Claude's profile. Claude states no renewal date, so one is estimated as the
// next monthly anniversary of the start and marked as an estimate.
func claudeProfile(ctx context.Context, token string, r *model.Reading, now time.Time) {
	status, body, err := get(ctx, claudeProfileURL, token, map[string]string{
		"anthropic-beta":    "oauth-2025-04-20",
		"anthropic-version": "2023-06-01",
		"User-Agent":        claudeUA,
	})
	if err != nil || status != 200 {
		return
	}
	var doc map[string]any
	if json.Unmarshal(body, &doc) != nil {
		return
	}
	org := asMap(doc["organization"])
	if asString(org["subscription_status"]) != "active" {
		return
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

// codexResets itemises the banked resets that can still be used. Credits
// that expire first come first, and ones that never expire last.
func codexResets(ctx context.Context, token string, headers map[string]string, r *model.Reading) {
	status, body, err := get(ctx, codexResetCreditsURL, token, headers)
	if err != nil || status != 200 {
		return
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
		return
	}
	resets := []model.ResetCredit{}
	for _, c := range doc.Credits {
		// The list keeps redeemed and expired credits for its history.
		if c.Status != "" && c.Status != "available" {
			continue
		}
		resets = append(resets, model.ResetCredit{
			ID: c.ID, Type: c.ResetType, Title: trimLabel(c.Title), Status: c.Status,
			GrantedAt: isoTime(c.GrantedAt), ExpiresAt: isoTime(c.ExpiresAt),
		})
	}
	sort.SliceStable(resets, func(i, j int) bool {
		a, b := resets[i].ExpiresAt, resets[j].ExpiresAt
		return a != nil && (b == nil || a.Before(*b))
	})
	r.Credits.Resets = resets
}
