package ladder

import (
	"testing"
	"time"
)

// An account created without a tier, and a poll that names none, must not
// be treated as the cheapest plan: stepping it would change a plan nobody stated.
func TestUnknownTierDoesNotStep(t *testing.T) {
	a := acc()
	a.TierID = ""
	st, step := Decide(Input{Account: a, Reading: reading(100, ""), Now: time.Now()})
	if step != nil || st.TierID != "" || st.Note != "the tier is not known, so the ladder will not act" {
		t.Fatalf("%+v %+v", st, step)
	}
	a.TierID = "retired"
	if _, step := Decide(Input{Account: a, Reading: reading(100, ""), Now: time.Now()}); step != nil {
		t.Fatalf("a tier the provider does not have stepped: %+v", step)
	}
}
