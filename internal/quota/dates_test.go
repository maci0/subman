package quota

import (
	"testing"
	"time"

	"github.com/maci0/subman/internal/model"
)

func TestStateDates(t *testing.T) {
	now := time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC)
	codex := func(until string) map[string]any {
		return map[string]any{"tokens": map[string]any{"id_token": jwtWith(
			`{"https://api.openai.com/auth":{"chatgpt_subscription_active_start":"2026-09-29T22:52:42+00:00","chatgpt_subscription_active_until":"` + until + `"}}`)}}
	}
	r := model.Reading{}
	stateDates(model.ProviderCodex, codex("2026-11-13T00:36:52+00:00"), &r, now)
	if r.ActivatedAt != "2026-09-29T22:52:42Z" || r.RenewalAt != "2026-11-13T00:36:52Z" || r.RenewalEstimated {
		t.Fatalf("codex: %+v", r)
	}
	// A period that already ended is not a renewal date.
	r = model.Reading{}
	stateDates(model.ProviderCodex, codex("2026-11-01T00:00:00+00:00"), &r, now)
	if r.RenewalAt != "" || r.ActivatedAt == "" {
		t.Fatalf("stale codex period: %+v", r)
	}
	// Grok's billing periods are not a renewal, so none is stated.
	r = parseGrok(map[string]any{"config": map[string]any{"billingPeriodEnd": "2026-12-01T00:00:00Z", "creditUsagePercent": 4.0}}, now)
	stateDates(model.ProviderGrok, map[string]any{}, &r, now)
	if r.RenewalAt != "" {
		t.Fatalf("grok: %+v", r)
	}
	// A renewal already in the past is cleared, whoever set it.
	r = model.Reading{RenewalAt: "2026-11-01T00:00:00Z", RenewalEstimated: true}
	stateDates(model.ProviderClaude, map[string]any{}, &r, now)
	if r.RenewalAt != "" || r.RenewalEstimated {
		t.Fatalf("past renewal kept: %+v", r)
	}
	// The login file alone states nothing for Claude.
	r = model.Reading{}
	stateDates(model.ProviderClaude, map[string]any{"email": "a@example.com"}, &r, now)
	if r.RenewalAt != "" || r.ActivatedAt != "" {
		t.Fatalf("claude: %+v", r)
	}
}
