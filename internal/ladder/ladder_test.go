package ladder

import (
	"testing"
	"time"

	"github.com/maci0/subman/internal/model"
)

func acc() model.Account {
	return model.Account{
		Provider:      model.ProviderClaude,
		TierID:        "ultra",
		Ladder:        true,
		ChangeCommand: "/usr/local/bin/change-plan",
	}
}

func reading(util float64, tier string) *model.Reading {
	return &model.Reading{OK: true, TierID: tier, Windows: []model.Window{{Label: "weekly", Util: &util}}}
}

func TestOffWhenDisabled(t *testing.T) {
	a := acc()
	a.Ladder = false
	st, step := Decide(Input{Account: a, Now: time.Now()})
	if st.Phase != model.PhaseOff || step != nil {
		t.Fatalf("%+v %+v", st, step)
	}
}

func TestUnarmedWithoutCommand(t *testing.T) {
	a := acc()
	a.ChangeCommand = ""
	st, step := Decide(Input{Account: a, Reading: reading(100, "ultra"), Now: time.Now()})
	if st.Phase != model.PhaseUnarmed || step != nil {
		t.Fatalf("%+v %+v", st, step)
	}
}

func TestHoldsUnderThreshold(t *testing.T) {
	st, step := Decide(Input{Account: acc(), Reading: reading(50, "pro"), Now: time.Now()})
	if step != nil || st.Phase != model.PhaseClimbing {
		t.Fatalf("%+v %+v", st, step)
	}
}

func TestStepsUpWhenSpent(t *testing.T) {
	a := acc()
	a.TierID = "pro"
	st, step := Decide(Input{Account: a, Reading: reading(97, "pro"), Now: time.Now()})
	if step == nil || step.From != "pro" || step.To != "max" {
		t.Fatalf("%+v %+v", st, step)
	}
}

func TestDropsFromHighestAndWaits(t *testing.T) {
	renew := time.Now().Add(48 * time.Hour)
	a := acc()
	a.RenewalAt = &renew
	st, step := Decide(Input{Account: a, Reading: reading(99, "ultra"), State: model.LadderState{}, Now: time.Now()})
	if step == nil || step.To != "pro" || st.Phase != model.PhaseWaiting || st.Cycles != 1 {
		t.Fatalf("%+v %+v", st, step)
	}
	held, again := Decide(Input{Account: a, Reading: reading(0, "pro"), State: st, Now: time.Now()})
	if again != nil || held.Phase != model.PhaseWaiting {
		t.Fatalf("should hold: %+v %+v", held, again)
	}
	after := DecideInputAfter(a, st, renew.Add(time.Minute))
	if after.Phase != model.PhaseClimbing {
		t.Fatalf("after renewal: %+v", after)
	}
}

func DecideInputAfter(a model.Account, st model.LadderState, now time.Time) model.LadderState {
	out, _ := Decide(Input{Account: a, Reading: reading(0, "pro"), State: st, Now: now})
	return out
}

func TestMissingMeterDoesNotStep(t *testing.T) {
	a := acc()
	a.TierID = "pro"
	r := &model.Reading{OK: true, TierID: "pro", Windows: []model.Window{{Label: "weekly"}}}
	_, step := Decide(Input{Account: a, Reading: r, Now: time.Now()})
	if step != nil {
		t.Fatal(step)
	}
}

func TestShouldReset(t *testing.T) {
	a := acc()
	n := 2
	r := reading(99, "ultra")
	r.Credits.ResetCount = &n
	if ShouldReset(a, r) {
		t.Fatal("auto reset is off")
	}
	a.AutoReset = true
	if !ShouldReset(a, r) {
		t.Fatal("expected a reset")
	}
	zero := 0
	r.Credits.ResetCount = &zero
	if ShouldReset(a, r) {
		t.Fatal("zero credits")
	}
}

func TestCodexSkipsSameRank(t *testing.T) {
	a := model.Account{Provider: model.ProviderCodex, TierID: "plus", Ladder: true, ChangeCommand: "/bin/true"}
	_, step := Decide(Input{Account: a, Reading: reading(100, "plus"), Now: time.Now()})
	if step == nil || step.To != "pro" {
		t.Fatalf("%+v", step)
	}
}

// A plan change must not repeat on the reading that caused it: the meter
// stays full until the next poll.
func TestHoldsUntilAPollAfterTheLastStep(t *testing.T) {
	stepped := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	full := 99.0
	in := Input{
		Account: model.Account{Provider: model.ProviderClaude, TierID: "max", Ladder: true, ChangeCommand: "/bin/true"},
		Reading: &model.Reading{OK: true, PolledAt: stepped.Add(-time.Minute), Windows: []model.Window{{Label: "5-hour", Util: &full}}},
		State:   model.LadderState{LastStep: stepped},
		Now:     stepped.Add(time.Minute),
	}
	if st, step := Decide(in); step != nil || st.Note != "waiting for a poll taken after the last plan change" {
		t.Fatalf("stepped again on the old reading: %+v %+v", st, step)
	}
	in.Reading.PolledAt = stepped.Add(time.Second)
	if _, step := Decide(in); step == nil || step.To != "ultra" {
		t.Fatalf("a poll after the step did not allow the next one: %+v", step)
	}
}

// A banked reset is for the highest tier only. Below it the ladder steps up,
// which resets the window and keeps the credit.
func TestResetOnlyAtTheHighestTier(t *testing.T) {
	a := acc()
	a.AutoReset = true
	two := 2
	mid := reading(99, "max")
	mid.Credits.ResetCount = &two
	if ShouldReset(a, mid) {
		t.Fatal("a reset was offered below the highest tier")
	}
	if _, step := Decide(Input{Account: a, Reading: mid}); step == nil || step.To != "ultra" {
		t.Fatalf("below the top the ladder should step up, got %+v", step)
	}
	top := reading(99, "ultra")
	top.Credits.ResetCount = &two
	if !ShouldReset(a, top) {
		t.Fatal("no reset at the highest tier")
	}
	top.Credits.Resets = []model.ResetCredit{{ID: "other-limit"}, {ID: "soonest", Applies: true}, {ID: "later", Applies: true}}
	if st, step := Decide(Input{Account: a, Reading: top}); step == nil || !step.Reset || step.From != "ultra" || step.To != "ultra" || step.CreditID != "soonest" || st.Phase == model.PhaseWaiting {
		t.Fatalf("with a reset banked the ladder should use it at the top, got %+v %+v", st, step)
	}
	// A reset needs no plan-change command, and is not repeated on the same reading.
	unarmed := a
	unarmed.ChangeCommand = ""
	if _, step := Decide(Input{Account: unarmed, Reading: top}); step == nil || !step.Reset {
		t.Fatalf("an unarmed ladder should still use a reset, got %+v", step)
	}
	stepped := time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)
	top.PolledAt = stepped.Add(-time.Minute)
	if _, step := Decide(Input{Account: a, Reading: top, State: model.LadderState{LastStep: stepped}}); step != nil {
		t.Fatalf("a second reset was offered on the same reading: %+v", step)
	}
	// A reset that does not apply to the limit in the way is not usable.
	none := 0
	top.Credits.ResetApplies = &none
	if ShouldReset(a, top) {
		t.Fatal("a reset that does not apply was offered")
	}
	if _, step := Decide(Input{Account: a, Reading: top}); step == nil || step.To != "pro" {
		t.Fatalf("with no usable reset the ladder should drop, got %+v", step)
	}
}
