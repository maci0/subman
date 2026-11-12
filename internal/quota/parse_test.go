package quota

import (
	"strings"
	"testing"
	"time"

	"github.com/maci0/subman/internal/model"
)

func TestParseClaudeWeeklyAndExtra(t *testing.T) {
	body := []byte(`{
		"five_hour": {"utilization": 12.5, "resets_at": "2026-11-12T00:00:00Z"},
		"seven_day": {"utilization": 80, "resets_at": "2026-11-16T00:00:00Z"},
		"seven_day_opus": {"utilization": 10, "resets_at": "2026-11-16T00:00:00Z"},
		"extra_usage": {"is_enabled": true, "used_credits": 3.5, "monthly_limit": 20, "currency": "USD"}
	}`)
	r := Parse(model.ProviderClaude, body, time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC))
	if !r.OK {
		t.Fatalf("not ok: %+v", r)
	}
	if len(r.Windows) != 3 {
		t.Fatalf("windows: %d", len(r.Windows))
	}
	if r.Windows[0].Label != "5-hour" || r.Windows[0].Util == nil || *r.Windows[0].Util != 12.5 {
		t.Fatalf("session: %+v", r.Windows[0])
	}
	if !r.Credits.Enabled || r.Credits.Used == nil || *r.Credits.Used != 3.5 {
		t.Fatalf("credits: %+v", r.Credits)
	}
}

func TestParseClaudeLimitsSkipInactiveSession(t *testing.T) {
	body := []byte(`{"limits": [
		{"kind": "session", "is_active": false, "percent": 90},
		{"kind": "weekly_all", "percent": 40, "resets_at": "2026-11-16T00:00:00Z"}
	]}`)
	r := Parse(model.ProviderClaude, body, time.Now())
	if !r.OK || len(r.Windows) != 1 || r.Windows[0].Label != "All models" {
		t.Fatalf("%+v", r)
	}
}

func TestParseClaudePlanFromCredentialFields(t *testing.T) {
	if got := claudePlan("max", "default_claude_max_20x"); got != "Ultra" {
		t.Fatal(got)
	}
	if got := claudePlan("pro", "default_claude_pro"); got != "Pro" {
		t.Fatal(got)
	}
	// "İ" (U+0130) is the capital of Turkish dotless i. The default case map
	// lowercases it to itself, so a plan spelled with it must still match.
	if got := matchTier(model.ProviderClaude, "İ pro"); got != "pro" {
		t.Fatalf("dotted capital I: %q", got)
	}
	// "pro" is a prefix of "prolite", and both are Codex tiers of one rank.
	// The shorter one must not win, or a Pro Lite login is adopted as Pro.
	if got := matchTier(model.ProviderCodex, "prolite"); got != "prolite" {
		t.Fatalf("prolite id: %q", got)
	}
	if got := matchTier(model.ProviderCodex, "ChatGPT Pro Lite"); got != "prolite" {
		t.Fatalf("pro lite label: %q", got)
	}
	if got := matchTier(model.ProviderCodex, "ChatGPT Pro"); got != "pro" {
		t.Fatalf("pro label: %q", got)
	}
}

func TestParseCodexResetsAndWindows(t *testing.T) {
	body := []byte(`{
		"plan_type": "plus",
		"rate_limit": {
			"primary_window": {"used_percent": 95, "limit_window_seconds": 18000, "reset_at": 1762819200},
			"secondary_window": {"used_percent": 10, "limit_window_seconds": 604800, "reset_after_seconds": 3600}
		},
		"rate_limit_reset_credits": {"available_count": 2, "applicable_available_count": 1},
		"credits": {"has_credits": true, "balance": "4.50", "unlimited": false}
	}`)
	now := time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC)
	r := Parse(model.ProviderCodex, body, now)
	if !r.OK || r.Plan != "Plus" || r.TierID != "plus" {
		t.Fatalf("plan %+v", r)
	}
	if len(r.Windows) != 2 || r.Windows[0].Label != "Current session" || r.Windows[1].Label != "Weekly" {
		t.Fatalf("windows %+v", r.Windows)
	}
	if r.Credits.ResetCount == nil || *r.Credits.ResetCount != 2 {
		t.Fatalf("resets %+v", r.Credits)
	}
	if r.Credits.Balance == nil || *r.Credits.Balance != 4.5 {
		t.Fatalf("balance %+v", r.Credits.Balance)
	}
	if r.Windows[1].ResetsAt == nil || !r.Windows[1].ResetsAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("reset %+v", r.Windows[1].ResetsAt)
	}
}

func TestParseCodexEmptyResetIsZero(t *testing.T) {
	body := []byte(`{"plan_type":"pro","rate_limit":{"primary_window":{"used_percent":1,"limit_window_seconds":18000}},"rate_limit_reset_credits":{}}`)
	r := Parse(model.ProviderCodex, body, time.Now())
	if r.Credits.ResetCount == nil || *r.Credits.ResetCount != 0 {
		t.Fatalf("%+v", r.Credits)
	}
}

func TestParseGrokCreditUsage(t *testing.T) {
	body := []byte(`{"config":{"creditUsagePercent":3,"billingPeriodEnd":"2026-11-18T00:00:00Z","prepaidBalance":{"val":2006}}}`)
	r := Parse(model.ProviderGrok, body, time.Now())
	if !r.OK || len(r.Windows) != 1 || r.Windows[0].Label != "Credits" || *r.Windows[0].Util != 3 {
		t.Fatalf("%+v", r)
	}
	if r.Credits.Balance == nil || *r.Credits.Balance != 20.06 {
		t.Fatalf("balance %+v", r.Credits)
	}
}

func TestParseGrokCreditsFraction(t *testing.T) {
	body := []byte(`{"config":{"subscription_tier_display":"Super","label":"Weekly","total":100,"remaining":25,"resets_at":"2026-11-18T00:00:00Z"}}`)
	r := Parse(model.ProviderGrok, body, time.Now())
	if !r.OK || r.Plan != "Super" || r.TierID != "super" {
		t.Fatalf("%+v", r)
	}
	if len(r.Windows) != 1 || r.Windows[0].Util == nil || *r.Windows[0].Util != 75 {
		t.Fatalf("%+v", r.Windows)
	}
}

func TestParseAntigravityComplementsRemaining(t *testing.T) {
	body := []byte(`{"command":{"data":{"groups":[
		{"name":"Gemini","buckets":[{"window":"weekly","remaining_fraction":0.4,"reset_time":"2026-11-18T00:00:00Z"}]},
		{"name":"Claude","buckets":[{"window":"5h","remaining_fraction":1}]}
	]}}}`)
	r := Parse(model.ProviderAntigravity, body, time.Now())
	if !r.OK || len(r.Windows) != 2 {
		t.Fatalf("%+v", r)
	}
	if r.Windows[0].Label != "Gemini weekly" || *r.Windows[0].Util != 60 {
		t.Fatalf("%+v", r.Windows[0])
	}
	if r.Windows[1].Label != "Claude 5h" || *r.Windows[1].Util != 0 {
		t.Fatalf("%+v", r.Windows[1])
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	for _, body := range []string{"", "null", "[]", "{}", "not json"} {
		r := Parse(model.ProviderClaude, []byte(body), time.Now())
		if r.OK || r.Error != "bad-body" {
			t.Fatalf("%q -> %+v", body, r)
		}
	}
}

func TestParseDropsNonFinite(t *testing.T) {
	body := []byte(`{"plan_type":"plus","rate_limit":{"primary_window":{"used_percent":"NaN","limit_window_seconds":18000}}}`)
	r := Parse(model.ProviderCodex, body, time.Now())
	if r.OK {
		t.Fatalf("NaN meter counted as a reading: %+v", r)
	}
}

// Every limit a Codex payload carries is a window with its own reset: the
// main one, code review, additional per-feature limits, and a model that is
// limited on its own. Both reset-credit counts are kept.
func TestCodexCapturesEveryLimitAndResetCredit(t *testing.T) {
	now := time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC)
	body := []byte(`{
		"plan_type": "pro",
		"rate_limit": {"primary_window": {"used_percent": 40, "limit_window_seconds": 18000, "reset_at": 1795000000},
		               "secondary_window": {"used_percent": 10, "limit_window_seconds": 604800, "reset_at": 1795061903}},
		"code_review_rate_limit": {"primary_window": {"used_percent": 70, "limit_window_seconds": 604800, "reset_at": 1795061903}, "secondary_window": null},
		"additional_rate_limits": [{"limit_name": "cloud_tasks", "metered_feature": "x", "rate_limit": {"primary_window": {"used_percent": 5, "limit_window_seconds": 18000, "reset_at": 1795000000}}}],
		"model_usage": {"gpt-a": {"available": true, "available_at": null}, "gpt-b": {"available": false, "available_at": "2026-11-12T06:00:00Z"}},
		"rate_limit_reset_credits": {"available_count": 3, "applicable_available_count": 1}
	}`)
	r := Parse(model.ProviderCodex, body, now)
	var labels []string
	for _, w := range r.Windows {
		labels = append(labels, w.Label)
		if w.ResetsAt == nil {
			t.Errorf("%s has no reset time", w.Label)
		}
	}
	want := []string{"Current session", "Weekly", "Code review, weekly", "Cloud Tasks, current session", "gpt-b"}
	if strings.Join(labels, " | ") != strings.Join(want, " | ") {
		t.Fatalf("windows %q, want %q", labels, want)
	}
	if last := r.Windows[len(r.Windows)-1]; last.Util != nil {
		t.Fatal("a model with only a comeback time was given a percent")
	}
	if r.Credits.ResetCount == nil || *r.Credits.ResetCount != 3 || r.Credits.ResetApplies == nil || *r.Credits.ResetApplies != 1 {
		t.Fatalf("reset credits %+v", r.Credits)
	}
}

// The older Claude shape is read block by block, so a weekly limit this
// code has never seen still shows, with its reset time.
func TestClaudeReadsEveryWeeklyBlock(t *testing.T) {
	body := []byte(`{
		"five_hour": {"utilization": 12, "resets_at": "2026-11-12T05:00:00Z"},
		"seven_day_sonnet": {"utilization": 3, "resets_at": "2026-11-16T00:00:00Z"},
		"seven_day": {"utilization": 80, "resets_at": "2026-11-16T00:00:00Z"},
		"seven_day_oauth_apps": {"utilization": 1, "resets_at": "2026-11-16T00:00:00Z"},
		"seven_day_empty": null
	}`)
	r := Parse(model.ProviderClaude, body, time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC))
	var labels []string
	for _, w := range r.Windows {
		labels = append(labels, w.Label)
		if w.ResetsAt == nil {
			t.Errorf("%s has no reset time", w.Label)
		}
	}
	if got := strings.Join(labels, " | "); got != "5-hour | All models | Oauth Apps | Sonnet" {
		t.Fatalf("window labels %q", got)
	}
}

func TestUnixMaybeRoundsMilliseconds(t *testing.T) {
	want := time.Date(2026, 11, 11, 0, 0, 1, 0, time.UTC)
	got := unixMaybe(float64(want.UnixMilli() - 400))
	if got == nil || !got.Equal(want) {
		t.Fatalf("round up: %v", got)
	}
	down := unixMaybe(float64(want.Add(-time.Second).UnixMilli() + 400))
	if down == nil || !down.Equal(want.Add(-time.Second)) {
		t.Fatalf("round down: %v", down)
	}
	if got := unixMaybe(1e20); got != nil {
		t.Fatalf("beyond range: %v", got)
	}
	// 1e12 milliseconds is 2001-09-09T01:46:40Z. The same number in seconds
	// is year 33658, which no provider sends, so the boundary is milliseconds.
	boundary := unixMaybe(1e12)
	wantBoundary := time.Unix(int64(1e9), 0).UTC()
	if boundary == nil || !boundary.Equal(wantBoundary) {
		t.Fatalf("1e12 milliseconds: %v", boundary)
	}
}
