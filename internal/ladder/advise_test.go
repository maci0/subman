package ladder

import (
	"strings"
	"testing"
	"time"

	"github.com/maci0/subman/internal/model"
)

func kinds(advice []model.Advice) string {
	var out []string
	for _, a := range advice {
		out = append(out, a.Kind)
	}
	return strings.Join(out, ",")
}

func TestAdvise(t *testing.T) {
	now := time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC)
	in := func(d time.Duration) *time.Time { v := now.Add(d); return &v }
	window := func(label string, util float64, resets *time.Time) model.Window {
		return model.Window{Label: label, Util: &util, ResetsAt: resets}
	}
	a := model.Account{ID: "c1", Name: "work", Provider: model.ProviderClaude, TierID: "max", RenewalAt: in(9 * 24 * time.Hour)}

	if got := Advise(a, nil, now); got != nil {
		t.Fatalf("no reading: %+v", got)
	}
	if got := Advise(a, &model.Reading{Error: "http-401"}, now); kinds(got) != "login" {
		t.Fatalf("failed poll: %+v", got)
	}

	// Under the threshold: spend what is left before each window resets, soonest first, then the renewal.
	under := &model.Reading{OK: true, TierID: "max", Windows: []model.Window{
		window("All models", 61, in(80*time.Hour)), window("5-hour", 20, in(2*time.Hour)), window("Fable", 99, in(80*time.Hour)),
	}}
	under.Windows[2].Util = nil
	got := Advise(a, under, now)
	if kinds(got) != "spend,spend,renewal" || !strings.Contains(got[0].Text, "80% of 5-hour") || !got[0].At.Equal(now.Add(2*time.Hour)) || !strings.Contains(got[2].Text, "Be on Ultra") {
		t.Fatalf("under the threshold: %+v", got)
	}

	// Full below the top: step up.
	full := &model.Reading{OK: true, TierID: "max", Windows: []model.Window{window("All models", 99, in(80*time.Hour))}}
	if got := Advise(a, full, now); kinds(got) != "step,renewal" || !strings.Contains(got[0].Text, "Step up to Ultra") || !got[0].At.Equal(now) {
		t.Fatalf("full below the top: %+v", got)
	}

	// Full at the top: a banked reset if one applies, else drop.
	full.TierID = "ultra"
	two, none := 2, 0
	full.Credits.ResetCount = &two
	full.Credits.Resets = []model.ResetCredit{{Type: "weekly", Title: "Weekly reset", ExpiresAt: in(3 * 24 * time.Hour)}, {Type: "weekly"}}
	got = Advise(a, full, now)
	if kinds(got) != "reset,expiring,renewal" || !strings.Contains(got[1].Text, "Weekly reset expires. Use it") || !strings.Contains(got[2].Text, "It is on Ultra") {
		t.Fatalf("full at the top with resets: %+v", got)
	}
	full.Credits.ResetApplies = &none
	if got := Advise(a, full, now); kinds(got) != "drop,expiring,renewal" || !strings.Contains(got[0].Text, "Drop to Pro and climb again after the renewal") {
		t.Fatalf("full at the top with no usable reset: %+v", got)
	}

	// An expiring reset below the top says to climb first.
	full.TierID = "pro"
	if got := Advise(a, full, now); !strings.Contains(got[1].Text, "only worth using on Ultra") {
		t.Fatalf("expiring reset below the top: %+v", got)
	}

	// An unknown tier is not guessed.
	b := a
	b.TierID = ""
	unknown := &model.Reading{OK: true, Windows: []model.Window{window("Credits", 100, in(time.Hour))}}
	if got := Advise(b, unknown, now); kinds(got) != "step" || !strings.Contains(got[0].Text, "tier is not known") {
		t.Fatalf("unknown tier: %+v", got)
	}
}
