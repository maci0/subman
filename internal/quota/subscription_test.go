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

// Codex states the renewal, the plan it changes to, and the list price of
// both in the billing currency. The id token's period end does not replace
// the renewal the subscription states.
func TestCodexStatesRenewalNextPlanAndPrices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/usage":
			io.WriteString(w, `{"plan_type":"pro","rate_limit":{"primary_window":{"used_percent":40,"limit_window_seconds":604800,"reset_at":1795061903}}}`)
		case "/subs":
			if r.URL.Query().Get("account_id") != "acct-1" || r.Header.Get("ChatGPT-Account-Id") != "acct-1" {
				w.WriteHeader(400)
				return
			}
			io.WriteString(w, `{"plan_type":"promax","is_active":true,"billing_period":"monthly","billing_currency":"SGD","entitlement":{"renews_at":"2026-11-13T00:36:52+00:00","cancels_at":null,"discount":null,"applied_discounts":[],"scheduled_plan_change":{"changes_at":"2026-11-13T00:36:52+00:00","plan_type":"prolite"}}}`)
		case "/prices/SGD":
			io.WriteString(w, `{"currency_config":{"promax":{"month":{"amount":690,"tax":"inclusive"}},"prolite":{"month":{"amount":138.0,"tax":"inclusive"}},"tax_percent":9.0}}`)
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	old := []string{codexUsageURL, codexResetCreditsURL, codexSubscriptionURL, codexPricesURL}
	codexUsageURL, codexResetCreditsURL, codexSubscriptionURL, codexPricesURL = srv.URL+"/usage", srv.URL+"/resets", srv.URL+"/subs?account_id=", srv.URL+"/prices/"
	defer func() {
		codexUsageURL, codexResetCreditsURL, codexSubscriptionURL, codexPricesURL = old[0], old[1], old[2], old[3]
	}()

	now := time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC)
	cred := map[string]any{"tokens": map[string]any{"access_token": "tok", "account_id": "acct-1"}}
	r := fetchCodex(context.Background(), cred, now)
	stateDates(model.ProviderCodex, cred, &r, now)
	if !r.OK || r.RenewalAt != "2026-11-13T00:36:52Z" || r.NextPlan != "Pro Lite" || r.NextPlanAt != "2026-11-13T00:36:52Z" || r.Ends {
		t.Fatalf("reading %+v", r)
	}
	if r.Price == nil || *r.Price != (model.Price{Amount: 690, Currency: "SGD"}) || r.NextPrice == nil || *r.NextPrice != (model.Price{Amount: 138, Currency: "SGD"}) {
		t.Fatalf("prices %+v %+v", r.Price, r.NextPrice)
	}
}

// A discounted or yearly plan is not charged the monthly list price, so it is
// left unpriced. A cancellation ends the subscription instead of renewing it.
func TestCodexSubscriptionEdges(t *testing.T) {
	_, _, _, cur := parseCodexSubscription([]byte(`{"is_active":true,"billing_period":"monthly","billing_currency":"SGD","entitlement":{"applied_discounts":[{"id":"d"}]}}`))
	if cur != "" {
		t.Fatalf("discounted plan priced in %q", cur)
	}
	_, _, _, cur = parseCodexSubscription([]byte(`{"is_active":true,"billing_period":"yearly","billing_currency":"SGD","entitlement":{}}`))
	if cur != "" {
		t.Fatalf("yearly plan priced in %q", cur)
	}
	// A currency that is not a plain ISO code never reaches the price URL.
	_, _, _, cur = parseCodexSubscription([]byte(`{"is_active":true,"billing_period":"monthly","billing_currency":"../x","entitlement":{}}`))
	if cur != "" {
		t.Fatalf("currency %q passed", cur)
	}
	r, _, _, _ := parseCodexSubscription([]byte(`{"is_active":true,"entitlement":{"renews_at":"2026-12-01T00:00:00Z","cancels_at":"2026-12-01T00:00:00Z","scheduled_plan_change":{"changes_at":"2026-12-01T00:00:00Z","plan_type":"plus"}}}`))
	if !r.Ends || r.NextPlan != "" || r.NextPlanAt != "2026-12-01T00:00:00Z" {
		t.Fatalf("cancelled %+v", r)
	}
	if r, _, _, _ := parseCodexSubscription([]byte(`{"is_active":false,"entitlement":{"renews_at":"2026-12-01T00:00:00Z"}}`)); r.RenewalAt != "" {
		t.Fatalf("inactive %+v", r)
	}
	if p := codexPrice([]byte(`{"currency_config":{"pro":{"month":{"amount":300,"tax":"exclusive"}}}}`), "pro", "SGD"); p != nil {
		t.Fatalf("price before tax %+v", p)
	}
}

// Grok's subscription list keeps every past period and the X subscription.
// The active paid one gives the plan, the renewal, and the scheduled change.
func TestGrokStatesPlanRenewalAndScheduledChange(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/billing":
			io.WriteString(w, `{"config":{"creditUsagePercent":11.0,"billingPeriodEnd":"2026-11-14T16:47:10Z"}}`)
		case "/subs":
			io.WriteString(w, `{"subscriptions":[
				{"tier":"SUBSCRIPTION_TIER_SUPER_GROK_PRO","status":"SUBSCRIPTION_STATUS_ACTIVE","billingPeriodEnd":"2026-11-17T16:46:30.960234Z","cancelAtPeriodEnd":false,"scheduledChange":{"type":"SCHEDULED_CHANGE_TYPE_DOWNGRADE","targetPlan":"supergrok.lite"},"plan":"supergrok.heavy"},
				{"tier":"SUBSCRIPTION_TIER_GROK_PRO","status":"SUBSCRIPTION_STATUS_INACTIVE","billingPeriodEnd":"2026-04-02T11:13:05Z"},
				{"x":{},"tier":"SUBSCRIPTION_TIER_X_PREMIUM_PLUS","status":"SUBSCRIPTION_STATUS_ACTIVE"}],
				"dominantPlan":{"plan":"supergrok.heavy","surfaceNames":{"SURFACE_DISPLAY":"SuperGrok Heavy"}}}`)
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	oldBilling, oldSubs := grokBillingURL, grokSubscriptionsURL
	grokBillingURL, grokSubscriptionsURL = srv.URL+"/billing", srv.URL+"/subs"
	defer func() { grokBillingURL, grokSubscriptionsURL = oldBilling, oldSubs }()

	cred := map[string]any{"s": map[string]any{"key": "tok", "expires_at": "2099-01-01T00:00:00Z"}}
	r := fetchGrok(context.Background(), cred, time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC))
	if !r.OK || r.Plan != "SuperGrok Heavy" || r.RenewalAt != "2026-11-17T16:46:30Z" || r.NextPlan != "Plus" || r.NextPlanAt != r.RenewalAt || r.Price != nil {
		t.Fatalf("reading %+v", r)
	}
	if c := parseGrokSubscription([]byte(`{"subscriptions":[{"status":"SUBSCRIPTION_STATUS_ACTIVE","billingPeriodEnd":"2026-11-17T00:00:00Z","cancelAtPeriodEnd":true,"plan":"p"}],"dominantPlan":{"plan":"p"}}`)); !c.Ends || c.NextPlan != "" {
		t.Fatalf("cancelled %+v", c)
	}
	legacy := parseGrokSubscription([]byte(`{"subscriptions":[{"tier":"SUBSCRIPTION_TIER_SUPER_GROK_PRO","status":"SUBSCRIPTION_STATUS_ACTIVE","billingPeriodEnd":"2026-11-18T17:03:06Z","billingSystem":"BILLING_SYSTEM_LEGACY"}],"dominantPlan":{"plan":"supergrok.heavy","surfaceNames":{"SURFACE_DISPLAY":"SuperGrok Heavy"}}}`))
	if legacy.Plan != "SuperGrok Heavy" || legacy.RenewalAt != "2026-11-18T17:03:06Z" || legacy.NextPlan != "" {
		t.Fatalf("legacy %+v", legacy)
	}
}

// A plan change whose moment has passed has happened; it is not shown.
func TestStateDatesDropsAPassedPlanChange(t *testing.T) {
	r := model.Reading{NextPlan: "Plus", NextPlanAt: "2026-11-01T00:00:00Z", NextPrice: &model.Price{Amount: 1, Currency: "USD"}}
	stateDates(model.ProviderGrok, nil, &r, time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC))
	if r.NextPlan != "" || r.NextPlanAt != "" || r.NextPrice != nil {
		t.Fatalf("reading %+v", r)
	}
}
