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
	if tierID == "" {
		tierID = tiers[0].ID
	}
	st.TierID = tierID

	if in.Account.ChangeCommand == "" {
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
	lowest := tiers[0]
	highest := tiers[len(tiers)-1]
	for _, t := range tiers {
		if t.Rank > highest.Rank {
			highest = t
		}
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

// ShouldReset reports whether an automatic reset credit should be redeemed.
// A reset is only offered when the account asked for it, a credit exists,
// and the primary meter is at or past the threshold.
func ShouldReset(acc model.Account, reading *model.Reading) bool {
	if !acc.AutoReset || reading == nil || !reading.OK {
		return false
	}
	if reading.Credits.ResetCount == nil || *reading.Credits.ResetCount < 1 {
		return false
	}
	util := reading.PrimaryUtil()
	if util == nil || *util < acc.Threshold() {
		return false
	}
	return true
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
