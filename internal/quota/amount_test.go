package quota

import (
	"testing"
	"time"

	"github.com/maci0/subman/internal/model"
)

// A wrapped prepaid balance is cents. A bare number is already major units,
// and a fractional cent count is not money.
func TestGrokAmountKeepsUnits(t *testing.T) {
	cents := grokAmount(map[string]any{"val": 2006.0})
	if cents == nil || *cents != 20.06 {
		t.Fatalf("cents %v", cents)
	}
	bare := grokAmount(20.06)
	if bare == nil || *bare != 20.06 {
		t.Fatalf("bare dollars divided: %v", bare)
	}
	if got := grokAmount(map[string]any{"val": 10.5}); got != nil {
		t.Fatalf("fractional cents kept: %v", *got)
	}
}

// A reset count that does not fit in a nanosecond duration wraps into the
// past. It is dropped instead of stored as an instant that already passed.
func TestResetAfterSecondsDoesNotWrap(t *testing.T) {
	now := time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC)
	body := []byte(`{"plan_type":"pro","rate_limit":{"primary_window":{"used_percent":10,"limit_window_seconds":18000,"reset_after_seconds":1e11}}}`)
	r := Parse(model.ProviderCodex, body, now)
	if !r.OK || len(r.Windows) != 1 || r.Windows[0].ResetsAt != nil {
		t.Fatalf("overflowing reset stored: %+v", r.Windows)
	}
	hour := []byte(`{"plan_type":"pro","rate_limit":{"primary_window":{"used_percent":10,"limit_window_seconds":18000,"reset_after_seconds":3600}}}`)
	r = Parse(model.ProviderCodex, hour, now)
	if r.Windows[0].ResetsAt == nil || !r.Windows[0].ResetsAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("one hour reset: %+v", r.Windows)
	}
	wide := []byte(`{"plan_type":"pro","rate_limit":{"primary_window":{"used_percent":10,"limit_window_seconds":1e11}}}`)
	r = Parse(model.ProviderCodex, wide, now)
	// The label is capped at MaxNameLen, so only its start survives. A wrapped
	// int32 window length would start with a minus or a small number instead.
	if r.Windows[0].Label != "1157407-day" {
		t.Fatalf("window label wrapped: %q", r.Windows[0].Label)
	}
}

// A lifetime that overflows a nanosecond duration would store an expiry
// centuries ago. It is left unset, the same as a missing lifetime.
func TestExpiryRejectsOverflow(t *testing.T) {
	if got := expiry(3600); got == "" {
		t.Fatal("a one hour lifetime stored nothing")
	}
	if got := expiry(1 << 62); got != "" {
		t.Fatalf("overflowing lifetime stored %q", got)
	}
}
