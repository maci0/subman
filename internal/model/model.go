// Package model is the document subman stores and the window reads.
//
// A subscription is one paid login at one provider. Its config lives in its
// own directory and never in a vendor CLI's home. Quota, renewal, and the
// ladder decision are computed values, not config.
package model

import (
	"fmt"
	"time"
	"unicode/utf8"
)

// Provider is a vendor subman knows how to read.
type Provider string

const (
	ProviderClaude      Provider = "claude"
	ProviderCodex       Provider = "codex"
	ProviderGrok        Provider = "grok"
	ProviderAntigravity Provider = "antigravity"
)

// Providers is the fixed set, in dashboard order.
var Providers = []Provider{ProviderClaude, ProviderCodex, ProviderGrok, ProviderAntigravity}

func (p Provider) Known() bool {
	for _, known := range Providers {
		if p == known {
			return true
		}
	}
	return false
}

func (p Provider) Title() string {
	switch p {
	case ProviderClaude:
		return "Claude"
	case ProviderCodex:
		return "Codex"
	case ProviderGrok:
		return "Grok"
	case ProviderAntigravity:
		return "Antigravity"
	default:
		return string(p)
	}
}

// Tier is one paid plan, ordered cheapest first.
type Tier struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Rank  int    `json:"rank"`
}

// Tiers returns the paid ladder for a provider, cheapest first.
//
// Ranks are the order a change walks, not a price. Prices move and are not
// stored. Free plans are absent: the ladder never targets one.
func Tiers(p Provider) []Tier {
	switch p {
	case ProviderClaude:
		return []Tier{{"pro", "Pro", 0}, {"max", "Max", 1}, {"ultra", "Ultra", 2}}
	case ProviderCodex:
		return []Tier{{"plus", "Plus", 0}, {"pro", "Pro", 1}, {"prolite", "Pro Lite", 1}}
	case ProviderGrok:
		return []Tier{{"plus", "Plus", 0}, {"premium", "Premium", 1}, {"super", "Super", 2}, {"ultra", "Ultra", 3}}
	case ProviderAntigravity:
		return []Tier{{"pro", "AI Pro", 0}, {"ultra", "AI Ultra", 1}}
	default:
		return nil
	}
}

// Account is one subscription's config. Secrets are paths, never values.
type Account struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Provider Provider  `json:"provider"`
	Created  time.Time `json:"created"`

	// CredentialFile is copied into the account directory at import.
	// The vendor CLI's own file is left untouched after that copy.
	CredentialFile string `json:"credentialFile"`

	// ActivatedAt is when this subscription started.
	// RenewalAt is the next billing date: typed by the operator, or adopted
	// from a poll when the provider states it. An estimate fills an empty
	// date only, and is marked with RenewalEstimated.
	ActivatedAt *time.Time `json:"activatedAt,omitempty"`
	RenewalAt   *time.Time `json:"renewalAt,omitempty"`
	// RenewalEstimated is true while RenewalAt is an estimate adopted from a
	// poll. A date the operator types, or one a provider states, clears it.
	RenewalEstimated bool `json:"renewalEstimated,omitempty"`

	// TierID is the plan the operator says is active. A poll can correct
	// it when the usage payload names a plan.
	TierID string `json:"tierId"`

	// Ladder runs the min-max cycle. It stays idle until ChangeCommand is
	// set, because no provider publishes a plan-change API.
	Ladder        bool   `json:"ladder"`
	ChangeCommand string `json:"changeCommand,omitempty"`

	// AutoReset redeems one banked reset when the primary meter is full
	// and the provider reports a reset credit.
	AutoReset bool `json:"autoReset"`

	// Home is the account directory. AgentHome is the directory a sandboxed
	// agent run uses instead of ~/.claude, ~/.codex, ~/.grok, or ~/.gemini.
	Home      string `json:"home"`
	AgentHome string `json:"agentHome"`

	// AgentBin is the CLI used by Test. Empty means the provider default
	// looked up on PATH inside the sandbox.
	AgentBin string `json:"agentBin,omitempty"`

	// UsageThreshold is the percent used that counts as "spent" for a
	// ladder step. Zero means DefaultUsageThreshold.
	UsageThreshold float64 `json:"usageThreshold,omitempty"`
}

const (
	// DefaultUsageThreshold is the percent used at which a ladder step fires.
	DefaultUsageThreshold = 95
	// MaxNameLen bounds a label copied into the window, counted in runes.
	// A multi-byte character counts as one, and a cut never splits one.
	MaxNameLen = 64
)

// TruncateRunes shortens s to at most n runes, ending on a rune boundary.
// n <= 0 returns "". A short string is returned unchanged.
func TruncateRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	i := 0
	for range n {
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
	}
	return s[:i]
}

// TruncateBytes shortens b to at most n bytes without ending inside a
// multi-byte character. n <= 0 returns nil. When n lands on a rune start it
// is already the right index, so the cut never reads past the end of b.
func TruncateBytes(b []byte, n int) []byte {
	if n <= 0 || len(b) == 0 {
		return nil
	}
	if n > len(b) {
		n = len(b)
	}
	for n > 0 && n < len(b) && !utf8.RuneStart(b[n]) {
		n--
	}
	return b[:n]
}

func (a Account) Threshold() float64 {
	if a.UsageThreshold <= 0 || a.UsageThreshold > 100 {
		return DefaultUsageThreshold
	}
	return a.UsageThreshold
}

// Window is one usage meter: a percent used and when it resets.
type Window struct {
	Label    string     `json:"label"`
	Util     *float64   `json:"util"`
	ResetsAt *time.Time `json:"resetsAt,omitempty"`
}

// Credits is extra paid usage beside the plan meters.
type Credits struct {
	Enabled      bool     `json:"enabled"`
	Used         *float64 `json:"used,omitempty"`
	Limit        *float64 `json:"limit,omitempty"`
	Currency     string   `json:"currency,omitempty"`
	Balance      *float64 `json:"balance,omitempty"`
	Unlimited    bool     `json:"unlimited"`
	ResetCount   *int     `json:"resetCount,omitempty"`
	ResetApplies *int     `json:"resetApplies,omitempty"`
	// Resets lists each banked reset when the provider itemises them.
	Resets []ResetCredit `json:"resets,omitempty"`
}

// Reading is one poll of one account.
type Reading struct {
	OK       bool      `json:"ok"`
	Error    string    `json:"error,omitempty"`
	Stale    bool      `json:"stale"`
	PolledAt time.Time `json:"polledAt"`
	Plan     string    `json:"plan,omitempty"`
	TierID   string    `json:"tierId,omitempty"`
	Windows  []Window  `json:"windows"`
	Credits  Credits   `json:"credits"`
	// RenewalAt and ActivatedAt are RFC 3339 instants, set only when the
	// provider states them: in the usage payload or in the login itself.
	RenewalAt   string `json:"renewalAt,omitempty"`
	ActivatedAt string `json:"activatedAt,omitempty"`
	// RenewalEstimated marks RenewalAt as worked out, not stated: the next
	// monthly anniversary of the subscription start, for a provider that
	// states when a subscription began and not when it renews.
	RenewalEstimated bool `json:"renewalEstimated,omitempty"`
}

// PrimaryUtil is the fullest meter, which is what the ladder watches.
// A reading with no meter returns nil: missing is missing.
func (r Reading) PrimaryUtil() *float64 {
	var best *float64
	for i := range r.Windows {
		u := r.Windows[i].Util
		if u == nil {
			continue
		}
		if best == nil || *u > *best {
			best = u
		}
	}
	return best
}

// LadderPhase is where an account sits in the cycle.
type LadderPhase string

const (
	PhaseOff      LadderPhase = "off"
	PhaseUnarmed  LadderPhase = "unarmed"
	PhaseClimbing LadderPhase = "climbing"
	PhaseWaiting  LadderPhase = "waiting"
	PhaseActing   LadderPhase = "acting"
)

// LadderState is the last decision for one account.
type LadderState struct {
	Phase     LadderPhase `json:"phase"`
	TierID    string      `json:"tierId"`
	Note      string      `json:"note"`
	LastStep  time.Time   `json:"lastStep,omitempty"`
	LastError string      `json:"lastError,omitempty"`
	// Cycles is how many times the account has returned to the lowest tier.
	Cycles int `json:"cycles"`
}

// Step is one ladder move: which tier to ask for, or a banked reset to use, and why.
type Step struct {
	From    string `json:"from"`
	To      string `json:"to"`
	Reason  string `json:"reason"`
	Command string `json:"command,omitempty"`
	// Reset marks a step that changes no plan: it uses a banked reset on
	// the tier named in From and To. CreditID is the reset to use, when
	// the provider itemises them.
	Reset    bool   `json:"reset,omitempty"`
	CreditID string `json:"creditId,omitempty"`
}

// AccountView is what the dashboard draws for one subscription.
type AccountView struct {
	Account Account     `json:"account"`
	Reading *Reading    `json:"reading,omitempty"`
	Ladder  LadderState `json:"ladder"`
	Next    *Step       `json:"next,omitempty"`
	Tiers   []Tier      `json:"tiers"`
	// Threshold is Account.Threshold: the percent used at which a step fires.
	Threshold float64 `json:"threshold"`
	// History is the last plan changes, newest first.
	History []HistoryEntry `json:"history,omitempty"`
}

// Overview is the whole window.
type Overview struct {
	Version     string        `json:"version"`
	DataDir     string        `json:"dataDir"`
	Accounts    []AccountView `json:"accounts"`
	PolledAt    time.Time     `json:"polledAt,omitempty"`
	Update      UpdateStatus  `json:"update"`
	SandboxOK   bool          `json:"sandboxOk"`
	SandboxNote string        `json:"sandboxNote,omitempty"`
	Settings    Settings      `json:"settings"`
	// Problems names each account directory that could not be loaded and
	// is therefore missing from Accounts.
	Problems []string `json:"problems"`
	// Advice is every account's proposed next steps, soonest first.
	Advice []Advice `json:"advice"`
	// AutoPollChoices and ResetExpiryWarningMs carry the constants of the
	// same names, so the window does not keep its own copy.
	AutoPollChoices      []int `json:"autoPollChoices"`
	ResetExpiryWarningMs int64 `json:"resetExpiryWarningMs"`
}

// UpdateStatus is the last self-update check.
type UpdateStatus struct {
	Current   string `json:"current"`
	Latest    string `json:"latest,omitempty"`
	Available bool   `json:"available"`
	Ahead     bool   `json:"ahead"`
	Note      string `json:"note,omitempty"`
	CheckedAt string `json:"checkedAt,omitempty"`
}

// ProbeResult is one sandboxed agent test.
type ProbeResult struct {
	AccountID string `json:"accountId"`
	OK        bool   `json:"ok"`
	Exit      int    `json:"exit"`
	Output    string `json:"output"`
	Sandboxed bool   `json:"sandboxed"`
	Note      string `json:"note,omitempty"`
}

// ImportRequest copies a credential into a new account directory.
type ImportRequest struct {
	Name           string   `json:"name"`
	Provider       Provider `json:"provider"`
	CredentialPath string   `json:"credentialPath"`
	TierID         string   `json:"tierId"`
	ActivatedAt    string   `json:"activatedAt"`
	RenewalAt      string   `json:"renewalAt"`
}

// DefaultCredential is where the provider's own CLI writes its login,
// relative to the home directory. Empty for an unknown provider.
func (p Provider) DefaultCredential() string {
	switch p {
	case ProviderClaude:
		return ".claude/.credentials.json"
	case ProviderCodex:
		return ".codex/auth.json"
	case ProviderGrok:
		return ".grok/auth.json"
	case ProviderAntigravity:
		return ".gemini/antigravity-cli/antigravity-oauth-token"
	default:
		return ""
	}
}

// HistoryEntry is one attempted plan change. Error is empty when the change
// command exited 0.
type HistoryEntry struct {
	At    time.Time `json:"at"`
	From  string    `json:"from"`
	To    string    `json:"to"`
	Error string    `json:"error,omitempty"`
	// Reset is true for a banked reset used on the tier in From and To.
	Reset bool `json:"reset,omitempty"`
}

// KnownTier reports whether id is one of the provider's paid tiers.
func KnownTier(p Provider, id string) bool {
	for _, t := range Tiers(p) {
		if t.ID == id {
			return true
		}
	}
	return false
}

// Settings are the window's preferences, shared by every account.
type Settings struct {
	// AutoPollMinutes is how often the window polls on its own. 0 is never.
	AutoPollMinutes int `json:"autoPollMinutes"`
	// Notify raises a desktop notification when a poll moves an account's
	// fullest meter across its spend threshold.
	Notify bool `json:"notify"`
	// FoldedProviders are the board sections the operator folded shut.
	FoldedProviders []Provider `json:"foldedProviders"`
}

// AutoPollChoices are the intervals Settings.AutoPollMinutes may hold.
var AutoPollChoices = []int{0, 5, 15, 60}

// DefaultSettings is what a data directory with no settings file uses.
func DefaultSettings() Settings {
	return Settings{AutoPollMinutes: 15, Notify: true, FoldedProviders: []Provider{}}
}

// Validate reports the first field that holds a value outside its set.
func (s Settings) Validate() error {
	known := false
	for _, m := range AutoPollChoices {
		known = known || m == s.AutoPollMinutes
	}
	if !known {
		return fmt.Errorf("autoPollMinutes must be one of %v, got %d", AutoPollChoices, s.AutoPollMinutes)
	}
	for _, p := range s.FoldedProviders {
		if !p.Known() {
			return fmt.Errorf("unknown provider %q in foldedProviders", p)
		}
	}
	return nil
}

// ResetCredit is one banked rate-limit reset, as the provider lists it.
type ResetCredit struct {
	ID string `json:"id"`
	// Type is the provider's own name for what the reset applies to.
	Type      string     `json:"type"`
	Title     string     `json:"title,omitempty"`
	Status    string     `json:"status,omitempty"`
	GrantedAt *time.Time `json:"grantedAt,omitempty"`
	// ExpiresAt is nil for a reset that does not expire.
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
	// Left is how many uses the reset still has. One entry can hold several.
	Left int `json:"left"`
	// Clears names the limits this reset clears, in the provider's own words.
	Clears []string `json:"clears,omitempty"`
	// Applies is true when the reset can be used now on the limit that is
	// fullest. A reset for another limit is banked and does not apply.
	Applies bool `json:"applies"`
}

// ResetExpiryWarning is how long before a banked reset expires the window
// starts warning about it.
const ResetExpiryWarning = 7 * 24 * time.Hour

// UsableResets is how many banked resets apply to the limit in the way: the
// provider's applicable count when it gives one, else the total. Zero when
// the reading reports no reset credits.
func (c Credits) UsableResets() int {
	if c.ResetApplies != nil {
		return *c.ResetApplies
	}
	if c.ResetCount != nil {
		return *c.ResetCount
	}
	return 0
}

// Advice is one proposed step toward spending every paid window in full,
// and when to take it.
type Advice struct {
	AccountID string `json:"accountId"`
	// At is when to act: now for a step that is due, else the moment the
	// thing it is about happens (a window resets, a credit expires, a
	// subscription renews).
	At time.Time `json:"at"`
	// Kind is step, reset, drop, spend, expiring, renewal, or login.
	Kind string `json:"kind"`
	Text string `json:"text"`
}
