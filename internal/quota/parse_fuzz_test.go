package quota

import (
	"math"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/maci0/subman/internal/model"
)

// FuzzParse feeds arbitrary bodies to every provider parser. Vendor usage
// payloads are untrusted JSON; the reading that comes back is drawn and
// stored, so a malformed body must not panic and must keep the invariants
// the window relies on: a bounded window list, finite percents, and labels
// short enough to copy.
func FuzzParse(f *testing.F) {
	f.Add([]byte(`{"five_hour":{"utilization":12.5,"resets_at":"2026-11-12T00:00:00Z"},"seven_day":{"utilization":80},"extra_usage":{"is_enabled":true,"used_credits":3.5,"monthly_limit":20}}`))
	f.Add([]byte(`{"limits":[{"kind":"session","percent":1,"resets_at":"2026-11-12T00:00:00Z"},{"kind":"weekly_all","percent":40}]}`))
	f.Add([]byte(`{"plan_type":"pro","rate_limit":{"primary_window":{"used_percent":40,"limit_window_seconds":18000,"reset_at":1795000000},"secondary_window":{"used_percent":10,"limit_window_seconds":604800}},"rate_limit_reset_credits":{"available_count":3}}`))
	f.Add([]byte(`{"config":{"creditUsagePercent":4,"productUsage":[{"product":"GrokBuild","usagePercent":3}],"billingPeriodEnd":"2026-11-14T16:47:10Z","prepaidBalance":{"val":2006}}}`))
	f.Add([]byte(`{"command":{"data":{"groups":[{"name":"Gemini","buckets":[{"remaining_fraction":0.4,"window":"weekly","reset_time":"2026-11-16T00:00:00Z"}]}]}}}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`null`))
	f.Add([]byte(`[]`))
	f.Add([]byte(`"x"`))
	f.Add([]byte(``))
	f.Add([]byte(`not json`))
	f.Add([]byte("{\"a\":" + strings.Repeat("{\"a\":", 40) + "1" + strings.Repeat("}", 40) + "}"))
	now := time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC)
	f.Fuzz(func(t *testing.T, body []byte) {
		for _, p := range model.Providers {
			assertReading(t, Parse(p, body, now))
		}
		assertReading(t, Parse(model.Provider("nope"), body, now))
	})
}

func assertReading(t *testing.T, r model.Reading) {
	t.Helper()
	if r.PolledAt.IsZero() || r.PolledAt.Location() != time.UTC {
		t.Fatalf("polledAt not UTC: %v", r.PolledAt)
	}
	if !r.OK && r.Error == "" {
		t.Fatal("failed reading without an error")
	}
	if r.OK && r.Error != "" {
		t.Fatalf("ok reading carries error %q", r.Error)
	}
	if len(r.Windows) > maxWindows {
		t.Fatalf("windows %d over cap %d", len(r.Windows), maxWindows)
	}
	if r.OK && len(r.Windows) == 0 && r.Credits.ResetCount == nil && r.Credits.Used == nil && r.Credits.Balance == nil {
		t.Fatal("ok reading with nothing to draw")
	}
	for _, w := range r.Windows {
		if !utf8.ValidString(w.Label) || len(w.Label) > model.MaxNameLen {
			t.Fatalf("label %q", w.Label)
		}
		if w.Util != nil && (math.IsNaN(*w.Util) || math.IsInf(*w.Util, 0)) {
			t.Fatalf("non-finite util %v", *w.Util)
		}
		if w.ResetsAt != nil && w.ResetsAt.Location() != time.UTC {
			t.Fatalf("reset not UTC: %v", w.ResetsAt)
		}
	}
	for _, n := range []*float64{r.Credits.Used, r.Credits.Limit, r.Credits.Balance} {
		if n != nil && (math.IsNaN(*n) || math.IsInf(*n, 0)) {
			t.Fatalf("non-finite credit %v", *n)
		}
	}
}
