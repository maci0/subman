package ladder

import (
	"testing"
	"time"

	"github.com/maci0/subman/internal/model"
)

// Parked means the drop landed on the lowest tier. A later poll that still
// shows a higher tier means the plan did not change, and holding until
// renewal would leave a spent top tier untouched for the rest of the period.
func TestWaitingDoesNotHoldATierAboveTheLowest(t *testing.T) {
	renew := time.Now().Add(48 * time.Hour)
	a := acc()
	a.RenewalAt = &renew
	parked := model.LadderState{Phase: model.PhaseWaiting, TierID: "pro", Cycles: 1, LastStep: time.Now().Add(-time.Hour)}
	r := reading(99, "ultra")
	r.PolledAt = time.Now()
	st, step := Decide(Input{Account: a, Reading: r, State: parked, Now: time.Now()})
	if step == nil || step.From != "ultra" || step.To != "pro" || st.Phase != model.PhaseWaiting {
		t.Fatalf("a poll still on the top tier should drop again, got %+v %+v", st, step)
	}
	low := reading(99, "pro")
	low.PolledAt = time.Now()
	held, again := Decide(Input{Account: a, Reading: low, State: parked, Now: time.Now()})
	if again != nil || held.Phase != model.PhaseWaiting || held.Cycles != 1 {
		t.Fatalf("the lowest tier should stay parked: %+v %+v", held, again)
	}
}
