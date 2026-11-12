package quota

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/maci0/subman/internal/model"
)

func TestNextMonthly(t *testing.T) {
	at := func(s string) time.Time {
		v, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	cases := []struct{ began, now, want string }{
		{"2026-11-05T16:12:11Z", "2026-11-12T00:00:00Z", "2026-12-05T16:12:11Z"},
		{"2026-01-16T02:40:20Z", "2026-11-12T00:00:00Z", "2026-11-16T02:40:20Z"},
		{"2026-01-16T02:40:20Z", "2026-11-16T02:40:20Z", "2026-12-16T02:40:20Z"},
		// The 31st lands on the last day of a shorter month, and December rolls the year.
		{"2026-01-31T10:00:00Z", "2026-02-01T00:00:00Z", "2026-02-28T10:00:00Z"},
		{"2026-10-31T10:00:00Z", "2026-11-01T00:00:00Z", "2026-11-30T10:00:00Z"},
		{"2026-11-30T10:00:00Z", "2026-12-31T00:00:00Z", "2027-01-30T10:00:00Z"},
	}
	for _, c := range cases {
		if got := nextMonthly(at(c.began), at(c.now)).Format(time.RFC3339); got != c.want {
			t.Errorf("began %s, now %s: %s, want %s", c.began, c.now, got, c.want)
		}
	}
}

// Claude's usage payload names no plan. The profile does, with the start of
// the subscription; the renewal is an estimate and is marked as one.
func TestClaudeProfileStatesPlanAndStart(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("authorization %q", r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case "/usage":
			io.WriteString(w, `{"limits":[{"group":"weekly","kind":"weekly_all","percent":96,"resets_at":"2026-11-16T16:00:00Z"}]}`)
		case "/profile":
			io.WriteString(w, `{"organization":{"plan_display_name":"Ultra","organization_type":"claude_max","rate_limit_tier":"default_claude_max_20x","subscription_status":"active","subscription_created_at":"2026-01-16T02:40:20.034075Z"}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	oldUsage, oldProfile := claudeUsageURL, claudeProfileURL
	claudeUsageURL, claudeProfileURL = srv.URL+"/usage", srv.URL+"/profile"
	defer func() { claudeUsageURL, claudeProfileURL = oldUsage, oldProfile }()

	now := time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC)
	r := fetchClaude(context.Background(), map[string]any{"access_token": "tok"}, now)
	if !r.OK || r.Plan != "Ultra" || r.TierID != "ultra" {
		t.Fatalf("plan: %+v", r)
	}
	if r.ActivatedAt != "2026-01-16T02:40:20Z" || r.RenewalAt != "2026-11-16T02:40:20Z" || !r.RenewalEstimated {
		t.Fatalf("dates: %+v", r)
	}

	// With no profile the plan stays unstated: nothing is defaulted.
	claudeProfileURL = srv.URL + "/missing"
	r = fetchClaude(context.Background(), map[string]any{"access_token": "tok"}, now)
	if !r.OK || r.Plan != "" || r.TierID != "" || r.RenewalAt != "" {
		t.Fatalf("without a profile: %+v", r)
	}
}

func TestCodexItemisesAvailableResets(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/usage":
			io.WriteString(w, `{"plan_type":"pro","rate_limit":{"primary_window":{"used_percent":40,"limit_window_seconds":604800,"reset_at":1795061903}},"rate_limit_reset_credits":{"available_count":2,"applicable_available_count":1}}`)
		case "/resets":
			io.WriteString(w, `{"available_count":2,"credits":[
				{"id":"c-never","reset_type":"weekly","status":"available","granted_at":"2026-11-01T00:00:00Z","expires_at":null,"title":"Weekly reset"},
				{"id":"c-used","reset_type":"weekly","status":"redeemed","granted_at":"2026-10-01T00:00:00Z","expires_at":"2026-11-20T00:00:00Z"},
				{"id":"c-soon","reset_type":"five_hour","status":"available","granted_at":"2026-11-02T00:00:00Z","expires_at":"2026-11-15T00:00:00Z","title":"Session reset"}]}`)
		}
	}))
	defer srv.Close()
	oldUsage, oldResets := codexUsageURL, codexResetCreditsURL
	codexUsageURL, codexResetCreditsURL = srv.URL+"/usage", srv.URL+"/resets"
	defer func() { codexUsageURL, codexResetCreditsURL = oldUsage, oldResets }()

	r := fetchCodex(context.Background(), map[string]any{"tokens": map[string]any{"access_token": "tok"}}, time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC))
	resets := r.Credits.Resets
	if len(resets) != 2 || resets[0].ID != "c-soon" || resets[0].Type != "five_hour" || resets[0].Title != "Session reset" || resets[0].ExpiresAt == nil || resets[1].ID != "c-never" || resets[1].ExpiresAt != nil {
		t.Fatalf("resets %+v", resets)
	}
	if r.Credits.UsableResets() != 1 || *r.Credits.ResetCount != 2 {
		t.Fatalf("counts %+v", r.Credits)
	}
}

func TestGrokReadsEachProductWithThePeriodReset(t *testing.T) {
	r := Parse(model.ProviderGrok, []byte(`{"config":{"creditUsagePercent":4.0,"productUsage":[{"product":"GrokBuild","usagePercent":3.0},{"product":"GrokBot","usagePercent":1.0}],"billingPeriodEnd":"2026-11-14T16:47:10Z","prepaidBalance":{"val":2006}}}`), time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC))
	if len(r.Windows) != 3 || r.Windows[0].Label != "Credits" || r.Windows[1].Label != "GrokBuild" || *r.Windows[1].Util != 3 || r.Windows[2].ResetsAt == nil {
		t.Fatalf("windows %+v", r.Windows)
	}
	if r.RenewalAt != "" || r.TierID != "" {
		t.Fatalf("grok states no renewal and no tier: %+v", r)
	}
}
