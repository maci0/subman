package ladder

import (
	"fmt"
	"sort"
	"time"

	"github.com/maci0/subman/internal/model"
)

// Advise lists what to do with one account, and when, to get the most out
// of what it pays for. It reads the same facts Decide does and runs nothing:
//
//   - a failed poll: fix the login, nothing else can be said;
//   - a meter at the threshold: step up a tier, or at the top use a banked
//     reset, or with none drop to the lowest tier until renewal;
//   - a meter under the threshold: what is left is lost when its window
//     resets, so spend it before then;
//   - a banked reset with an expiry: use it by then, on the highest tier;
//   - a renewal ahead: be on the highest tier when it happens.
func Advise(acc model.Account, reading *model.Reading, now time.Time) []model.Advice {
	var out []model.Advice
	add := func(at time.Time, kind, text string) {
		out = append(out, model.Advice{AccountID: acc.ID, Account: acc.Name, At: at, Kind: kind, Text: text})
	}
	if reading == nil {
		return nil
	}
	if !reading.OK {
		add(now, "login", "The last poll failed. Renew the login, or sign in again from Edit.")
		return out
	}
	tiers := model.Tiers(acc.Provider)
	top := AtHighest(acc, reading)
	var lowest, highest model.Tier
	if len(tiers) > 0 {
		lowest, highest = tiers[0], tiers[0]
		for _, t := range tiers {
			if t.Rank > highest.Rank {
				highest = t
			}
		}
	}
	tierKnown := model.KnownTier(acc.Provider, currentTierID(acc, reading))

	util := reading.PrimaryUtil()
	switch {
	case util == nil:
	case *util >= acc.Threshold() && !tierKnown:
		add(now, "step", "A meter is full. The tier is not known, so set it in Edit to see the next step.")
	case *util >= acc.Threshold() && !top:
		next := nextHigher(tiers, findTier(tiers, currentTierID(acc, reading)).Rank)
		add(now, "step", fmt.Sprintf("A meter is full. Step up to %s: the new tier starts with a fresh window, and banked resets stay banked.", next.Label))
	case *util >= acc.Threshold() && reading.Credits.UsableResets() > 0:
		add(now, "reset", fmt.Sprintf("%s is spent and a reset is banked. Use the reset.", highest.Label))
	case *util >= acc.Threshold():
		text := fmt.Sprintf("%s is spent and no reset is banked. Drop to %s", highest.Label, lowest.Label)
		if acc.RenewalAt != nil && acc.RenewalAt.After(now) {
			text += " and climb again after the renewal."
		} else {
			text += " until the next renewal."
		}
		add(now, "drop", text)
	default:
		for _, w := range reading.Windows {
			if w.Util == nil || w.ResetsAt == nil || !w.ResetsAt.After(now) || *w.Util >= acc.Threshold() {
				continue
			}
			add(*w.ResetsAt, "spend", fmt.Sprintf("%.0f%% of %s is unused and is lost when the window resets. Spend it before then.", 100-*w.Util, w.Label))
		}
	}

	for _, r := range reading.Credits.Resets {
		if r.ExpiresAt == nil || !r.ExpiresAt.After(now) {
			continue
		}
		name := r.Title
		if name == "" {
			name = r.Type + " reset"
		}
		if top || !tierKnown {
			add(*r.ExpiresAt, "expiring", fmt.Sprintf("The banked %s expires. Use it before then.", name))
		} else {
			add(*r.ExpiresAt, "expiring", fmt.Sprintf("The banked %s expires. A reset is only worth using on %s, so climb there first or it is lost.", name, highest.Label))
		}
	}

	if acc.RenewalAt != nil && acc.RenewalAt.After(now) && tierKnown && len(tiers) > 1 {
		when := "The subscription renews."
		if acc.RenewalEstimated {
			when = "The subscription renews about now (estimated from its start date)."
		}
		if top {
			add(*acc.RenewalAt, "renewal", when+fmt.Sprintf(" It is on %s, the tier to renew on: every lower tier's window is still ahead of it.", highest.Label))
		} else {
			add(*acc.RenewalAt, "renewal", when+fmt.Sprintf(" Be on %s by then, so the new period starts at the top.", highest.Label))
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out
}

// currentTierID is the tier a good reading states, else the account's own.
func currentTierID(acc model.Account, reading *model.Reading) string {
	if reading != nil && reading.OK && reading.TierID != "" {
		return reading.TierID
	}
	return acc.TierID
}
