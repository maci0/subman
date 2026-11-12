// Package ladder decides the next plan change for one subscription.
//
// Cycle, once the highest tier has been activated:
//
//  1. Drop to the lowest paid tier.
//  2. When the watched meter reaches the threshold, step up one tier.
//     A tier change resets the provider's usage window, which is the
//     whole point of the walk.
//  3. At the highest tier, when the meter fills again, drop to the lowest
//     and wait for RenewalAt before climbing again.
//
// Decide never runs a command and never touches the network. Apply is the
// caller's job, and only when the account has a change command. With no
// command the phase is "unarmed": the dashboard shows the next step and
// does not take it.
package ladder

import (
	"time"

	"github.com/maci0/subman/internal/model"
)

// Input is everything Decide needs. Now is injected so tests pin the clock.
type Input struct {
	Account model.Account
	Reading *model.Reading
	State   model.LadderState
	Now     time.Time
}

// Decide returns the phase to store and the step to take, or nil when the
// account should hold.
func Decide(in Input) (model.LadderState, *model.Step) {
	st := in.State
	tiers := model.Tiers(in.Account.Provider)
	if len(tiers) == 0 {
		st.Phase = model.PhaseOff
		st.Note = "this provider has no tier list"
		return st, nil
	}
	if !in.Account.Ladder {
		st.Phase = model.PhaseOff
		st.Note = "ladder is off"
		return st, nil
	}
	tierID := in.Account.TierID
	if in.Reading != nil && in.Reading.OK && in.Reading.TierID != "" {
		tierID = in.Reading.TierID
	}
	// An empty tier is "not reported", never the cheapest plan. Guessing one
	// would step a subscription whose plan nobody has stated.
	if !model.KnownTier(in.Account.Provider, tierID) {
		st.Phase = model.PhaseClimbing
		st.TierID = ""
		st.Note = "the tier is not known, so the ladder will not act"
		return st, nil
	}
	st.TierID = tierID

	if in.Account.ChangeCommand == "" {
		// A banked reset needs no plan-change command, so it is still offered.
		if reset := resetStep(in, tierID); reset != nil {
			st.Phase = model.PhaseClimbing
			st.Note = reset.Reason
			return st, reset
		}
		st.Phase = model.PhaseUnarmed
		st.Note = "no plan-change command is set, so the ladder will not act"
		return st, nil
	}

	// A renewal in the future while we are parked at the bottom means hold.
	if st.Phase == model.PhaseWaiting {
		if in.Account.RenewalAt != nil && in.Now.Before(*in.Account.RenewalAt) {
			st.Note = "parked at the lowest tier until renewal"
			st.TierID = tiers[0].ID
			return st, nil
		}
		st.Phase = model.PhaseClimbing
		st.Note = "renewal passed"
	}

	if in.Reading == nil || !in.Reading.OK {
		st.Phase = model.PhaseClimbing
		st.Note = "waiting for a usage reading"
		return st, nil
	}
	// A reading taken before the last plan change still shows the meter that
	// caused it. Acting on it again would run the change command twice.
	if !st.LastStep.IsZero() && !in.Reading.PolledAt.After(st.LastStep) {
		st.Phase = model.PhaseClimbing
		st.Note = "waiting for a poll taken after the last plan change"
		return st, nil
	}
	util := in.Reading.PrimaryUtil()
	if util == nil {
		st.Phase = model.PhaseClimbing
		st.Note = "reading has no meter yet"
		return st, nil
	}
	if *util < in.Account.Threshold() {
		st.Phase = model.PhaseClimbing
		st.Note = "usage is under the threshold"
		return st, nil
	}

	current := findTier(tiers, tierID)
	lowest, highest := bounds(tiers)

	// At the top with a reset banked: the reset refills this tier, so the
	// ladder uses it instead of dropping.
	if reset := resetStep(in, tierID); reset != nil {
		st.Phase = model.PhaseClimbing
		st.Note = reset.Reason
		return st, reset
	}
	// Already at the top and spent: drop, then wait out the billing period.
	if current.Rank >= highest.Rank && sameTier(current, tierID) {
		st.Phase = model.PhaseWaiting
		st.TierID = lowest.ID
		st.Cycles++
		st.Note = "highest tier is spent; dropping to the lowest until renewal"
		return st, step(current.ID, lowest.ID, st.Note, in.Account.ChangeCommand)
	}

	next := nextHigher(tiers, current.Rank)
	if next.ID == "" || next.ID == current.ID {
		st.Phase = model.PhaseClimbing
		st.Note = "no higher tier to step to"
		return st, nil
	}
	st.Phase = model.PhaseClimbing
	st.Note = "meter is full; stepping up resets the window"
	return st, step(current.ID, next.ID, st.Note, in.Account.ChangeCommand)
}

// AtHighest reports whether the account is on the top tier of its provider.
// The tier is the one a good reading states, else the one on the account.
func AtHighest(acc model.Account, reading *model.Reading) bool {
	tiers := model.Tiers(acc.Provider)
	if len(tiers) == 0 {
		return false
	}
	id := acc.TierID
	if reading != nil && reading.OK && reading.TierID != "" {
		id = reading.TierID
	}
	_, highest := bounds(tiers)
	for _, t := range tiers {
		if t.ID == id {
			return t.Rank == highest.Rank
		}
	}
	return false
}

// bounds is the cheapest tier and the highest rank. The last entry is not
// the highest rank: Codex lists Pro Lite after Pro at the same rank, and the
// first of a shared rank is the one a drop names.
func bounds(tiers []model.Tier) (lowest, highest model.Tier) {
	lowest, highest = tiers[0], tiers[0]
	for _, t := range tiers[1:] {
		if t.Rank > highest.Rank {
			highest = t
		}
	}
	return lowest, highest
}

// ShouldReset reports whether a banked reset should be used now. A reset is
// only for the highest tier: below it, stepping up a tier resets the window
// and keeps the credit. So it takes all of: the account asked for it, it is
// on the highest tier, a reset that applies is banked, and the fullest meter
// is at or past the threshold.
func ShouldReset(acc model.Account, reading *model.Reading) bool {
	if !acc.AutoReset || reading == nil || !reading.OK || !AtHighest(acc, reading) {
		return false
	}
	if reading.Credits.UsableResets() < 1 {
		return false
	}
	util := reading.PrimaryUtil()
	return util != nil && *util >= acc.Threshold()
}

// resetStep is the step that uses a banked reset, or nil when none is due.
// One is due when ShouldReset says so and the reading is newer than the
// last step: the meter stays full until the next poll, and acting on the
// same reading again would spend a second reset.
func resetStep(in Input, tierID string) *model.Step {
	if !ShouldReset(in.Account, in.Reading) {
		return nil
	}
	if !in.State.LastStep.IsZero() && !in.Reading.PolledAt.After(in.State.LastStep) {
		return nil
	}
	s := &model.Step{From: tierID, To: tierID, Reset: true, Reason: "highest tier is spent and a reset is banked; using the reset"}
	// Resets are listed soonest to expire first, so the first that applies
	// is the one to spend. With none itemised the provider picks.
	for _, r := range in.Reading.Credits.Resets {
		if r.Applies {
			s.CreditID = r.ID
			break
		}
	}
	return s
}

func step(from, to, reason, command string) *model.Step {
	return &model.Step{From: from, To: to, Reason: reason, Command: command}
}

func findTier(tiers []model.Tier, id string) model.Tier {
	for _, t := range tiers {
		if t.ID == id {
			return t
		}
	}
	return tiers[0]
}

func sameTier(t model.Tier, id string) bool {
	return t.ID == id
}

// nextHigher is the cheapest tier whose rank is strictly above current.
// Tiers that share a rank (Codex Pro and Pro Lite) are alternatives, not
// steps, so the walk uses rank.
func nextHigher(tiers []model.Tier, rank int) model.Tier {
	var best model.Tier
	for _, t := range tiers {
		if t.Rank <= rank {
			continue
		}
		if best.ID == "" || t.Rank < best.Rank {
			best = t
		}
	}
	return best
}
