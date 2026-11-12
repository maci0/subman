// Package app is the API the window calls. Every method is safe to run
// from the UI thread: network and process work uses a short timeout.
package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/maci0/subman/internal/desktop"
	"github.com/maci0/subman/internal/ladder"
	"github.com/maci0/subman/internal/model"
	"github.com/maci0/subman/internal/quota"
	"github.com/maci0/subman/internal/sandbox"
	"github.com/maci0/subman/internal/store"
	"github.com/maci0/subman/internal/update"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// Version is set from main.
var Version = "0.1.1"

// App is the bound Wails object.
type App struct {
	ctx  context.Context
	root *store.Root
	mu   sync.Mutex

	// loginMu guards the one sign-in that may be in progress.
	loginMu       sync.Mutex
	login         *quota.Login
	loginProvider model.Provider
	// loginOwner is the account the sign-in replaces the login of, or "" for a new account.
	loginOwner string
}

func New(root *store.Root) *App {
	return &App{root: root}
}

func (a *App) Startup(ctx context.Context) {
	a.ctx = ctx
}

func (a *App) Overview() (model.Overview, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.overview()
}

func (a *App) overview() (model.Overview, error) {
	accounts, problems, err := a.root.Accounts()
	if err != nil {
		return model.Overview{}, err
	}
	views := make([]model.AccountView, 0, len(accounts))
	advice := []model.Advice{}
	now := time.Now().UTC()
	var latest time.Time
	for _, acc := range accounts {
		view, err := a.view(acc)
		if err != nil {
			return model.Overview{}, err
		}
		if view.Reading != nil && view.Reading.PolledAt.After(latest) {
			latest = view.Reading.PolledAt
		}
		views = append(views, view)
		advice = append(advice, ladder.Advise(acc, view.Reading, now)...)
	}
	sort.SliceStable(advice, func(i, j int) bool { return advice[i].At.Before(advice[j].At) })
	upd, err := a.root.Update()
	if err != nil {
		upd = model.UpdateStatus{Note: err.Error()}
	}
	upd.Current = Version
	sup := sandbox.Check()
	settings, err := a.root.Settings()
	if err != nil {
		return model.Overview{}, err
	}
	return model.Overview{
		Version:     Version,
		DataDir:     a.root.Path,
		Accounts:    views,
		PolledAt:    latest,
		Update:      upd,
		SandboxOK:   sup.OK,
		SandboxNote: sup.Note,
		Settings:    settings,
		Problems:    problems,
		Advice:      advice,

		AutoPollChoices:      model.AutoPollChoices,
		ResetExpiryWarningMs: model.ResetExpiryWarning.Milliseconds(),
	}, nil
}

// stored reads an account's last reading and ladder state. A file that cannot
// be read does not take the account, or the window, down with it. An
// unreadable reading comes back as a failed reading that says so; the next
// poll replaces it. An unreadable ladder state comes back as ladderErr, and
// the caller must not act on the ladder then: when the last plan change
// happened is unknown.
func (a *App) stored(acc model.Account) (rp *model.Reading, st model.LadderState, ladderErr error) {
	reading, ok, err := a.root.Reading(acc.ID)
	switch {
	case err != nil:
		rp = &model.Reading{Error: "the stored reading is unreadable; poll to replace it", Windows: []model.Window{}}
	case ok:
		rp = &reading
	}
	st, ladderErr = a.root.Ladder(acc.ID)
	return rp, st, ladderErr
}

func (a *App) view(acc model.Account) (model.AccountView, error) {
	rp, st, ladderErr := a.stored(acc)
	now := time.Now().UTC()
	nextState, step := ladder.Decide(ladder.Input{Account: acc, Reading: rp, State: st, Now: now})
	// Decide is a preview here. Persisted state changes only when RunLadder
	// actually applies a step, so opening the window never moves a counter.
	shown := st
	shown.Phase = nextState.Phase
	shown.Note = nextState.Note
	shown.TierID = nextState.TierID
	if shown.Phase == "" {
		shown.Phase = model.PhaseOff
	}
	if ladderErr != nil {
		// No step is offered from a state that could not be read.
		step = nil
		shown = model.LadderState{
			Phase:     model.PhaseOff,
			TierID:    acc.TierID,
			Note:      "the ladder is held: its stored state is unreadable",
			LastError: "ladder.json: " + ladderErr.Error(),
		}
	}
	history, err := a.root.History(acc.ID)
	if err != nil {
		return model.AccountView{}, err
	}
	return model.AccountView{
		Account:   acc,
		Reading:   rp,
		Ladder:    shown,
		Next:      step,
		Tiers:     model.Tiers(acc.Provider),
		Threshold: acc.Threshold(),
		History:   history,
	}, nil
}

// StoreReading stores a poll, adopts the tier and dates it states, and, unless Settings.Notify is off, raises a desktop notification when the poll
// moves the account's fullest meter from under its spend threshold to at or
// over it. A notification that cannot be shown does not fail the poll.
func (a *App) StoreReading(acc model.Account, reading model.Reading) error {
	prev, had, prevErr := a.root.Reading(acc.ID)
	if err := a.root.PutReading(acc.ID, reading); err != nil {
		return err
	}
	// The reading is stored. What it states about the account is adopted next.
	if err := a.adopt(acc, reading); err != nil {
		return err
	}
	// The reading is stored. The notification is extra: if the previous
	// reading or the settings cannot be read, there is nothing to compare
	// or no permission to ask, so it is skipped and said on stderr.
	settings, err := a.root.Settings()
	if prevErr != nil || err != nil {
		fmt.Fprintln(os.Stderr, "subman: threshold notification skipped:", errors.Join(prevErr, err))
		return nil
	}
	var before *model.Reading
	if had {
		before = &prev
	}
	if label, util, ok := crossed(before, reading, acc.Threshold()); ok && settings.Notify {
		title := fmt.Sprintf("%s is at %.0f%%", acc.Name, util)
		body := fmt.Sprintf("%s reached the %.0f%% spend threshold.", label, acc.Threshold())
		if err := desktop.Notify(title, body); err != nil && !errors.Is(err, desktop.ErrUnsupported) {
			fmt.Fprintln(os.Stderr, "subman: notification not shown:", err)
		}
	}
	if settings.Notify && reading.OK {
		for _, r := range expiring(before, reading, reading.PolledAt) {
			title := fmt.Sprintf("%s: a banked reset expires %s", acc.Name, r.ExpiresAt.Local().Format("Mon 2 Jan 15:04"))
			body := "Use it before then. A reset is only worth using on the highest tier."
			if err := desktop.Notify(title, body); err != nil && !errors.Is(err, desktop.ErrUnsupported) {
				fmt.Fprintln(os.Stderr, "subman: notification not shown:", err)
			}
		}
	}
	return nil
}

// adopt copies into the account what an ok reading states about it: the paid
// tier, and the activation and renewal dates. What the provider states wins
// over what was typed; what it does not state is left alone.
func (a *App) adopt(acc model.Account, reading model.Reading) error {
	if !reading.OK {
		return nil
	}
	changed := false
	if reading.TierID != acc.TierID && model.KnownTier(acc.Provider, reading.TierID) {
		acc.TierID = reading.TierID
		changed = true
	}
	// The reading's dates were written by this program as RFC 3339; one that
	// does not parse is not adopted.
	if t, err := store.ParseTime(reading.ActivatedAt); err == nil && t != nil && !sameTime(acc.ActivatedAt, t) {
		acc.ActivatedAt = t
		changed = true
	}
	// A stated renewal always wins. An estimated one only fills an empty
	// date or replaces an earlier estimate: it never overwrites a date the
	// operator typed or a provider stated.
	if t, err := store.ParseTime(reading.RenewalAt); err == nil && t != nil {
		mayAdopt := !reading.RenewalEstimated || acc.RenewalAt == nil || acc.RenewalEstimated
		if mayAdopt && (!sameTime(acc.RenewalAt, t) || acc.RenewalEstimated != reading.RenewalEstimated) {
			acc.RenewalAt, acc.RenewalEstimated = t, reading.RenewalEstimated
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return a.root.Save(acc)
}

// sameTime reports whether both are unset or both name the same instant.
func sameTime(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(*b)
}

// expiring names the banked resets of next that are inside the warning
// window now and were not yet inside it when prev was polled: the ones to
// warn about once, as they cross into it.
func expiring(prev *model.Reading, next model.Reading, now time.Time) []model.ResetCredit {
	var out []model.ResetCredit
	for _, r := range next.Credits.Resets {
		if r.ExpiresAt == nil || !r.ExpiresAt.After(now) || r.ExpiresAt.Sub(now) > model.ResetExpiryWarning {
			continue
		}
		warned := false
		if prev != nil {
			for _, old := range prev.Credits.Resets {
				if old.ID == r.ID && old.ExpiresAt != nil && old.ExpiresAt.Sub(prev.PolledAt) <= model.ResetExpiryWarning {
					warned = true
				}
			}
		}
		if !warned {
			out = append(out, r)
		}
	}
	return out
}

// viewOf loads the account again before drawing it, for callers whose copy
// predates a poll that may have adopted a tier or a date.
func (a *App) viewOf(id string) (model.AccountView, error) {
	acc, err := a.root.Load(id)
	if err != nil {
		return model.AccountView{}, err
	}
	return a.view(acc)
}

// NewRequest fills what the operator may leave out when adding an account:
// the name becomes the login's identity, or the provider's name when the
// login names nobody. The tier stays empty, which reads as "not reported",
// until a poll states it or the operator sets it.
func NewRequest(req model.ImportRequest, identity string) model.ImportRequest {
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		req.Name = identity
	}
	if req.Name == "" {
		req.Name = req.Provider.Title()
	}
	req.Name = model.TruncateRunes(req.Name, model.MaxNameLen)
	return req
}

// crossed reports the fullest meter of next when it is at or over threshold
// and the fullest meter of prev was under it or absent. A failed poll never
// crosses: its meters, if any, are the stale ones PutReading carried over.
func crossed(prev *model.Reading, next model.Reading, threshold float64) (label string, util float64, ok bool) {
	if !next.OK {
		return "", 0, false
	}
	now := next.PrimaryUtil()
	if now == nil || *now < threshold {
		return "", 0, false
	}
	if prev != nil {
		if before := prev.PrimaryUtil(); before != nil && *before >= threshold {
			return "", 0, false
		}
	}
	for _, w := range next.Windows {
		if w.Util != nil && *w.Util == *now {
			return w.Label, *now, true
		}
	}
	return "", 0, false
}

// Providers is the fixed provider list, each one's tiers, and where its CLI
// keeps the login the operator would import.
func (a *App) Providers() []ProviderInfo {
	home, err := os.UserHomeDir()
	out := make([]ProviderInfo, 0, len(model.Providers))
	for _, p := range model.Providers {
		info := ProviderInfo{ID: p, Title: p.Title(), Tiers: model.Tiers(p), Login: quota.CanLogin(p)}
		if err == nil {
			info.CredentialPath = filepath.Join(home, p.DefaultCredential())
		}
		out = append(out, info)
	}
	return out
}

type ProviderInfo struct {
	ID    model.Provider `json:"id"`
	Title string         `json:"title"`
	Tiers []model.Tier   `json:"tiers"`
	// CredentialPath is the provider CLI's default login file. Empty when
	// the home directory is unknown. The file may not exist.
	CredentialPath string `json:"credentialPath,omitempty"`
	// Login reports whether BeginLogin can sign in to this provider.
	Login bool `json:"login"`
}

// PickCredential opens the native file dialog at startAt's directory and
// returns the chosen path, or "" when the operator cancelled.
func (a *App) PickCredential(startAt string) (string, error) {
	opts := runtime.OpenDialogOptions{Title: "Credential file", ShowHiddenFiles: true}
	if dir := filepath.Dir(startAt); filepath.IsAbs(dir) {
		if st, err := os.Stat(dir); err == nil && st.IsDir() {
			opts.DefaultDirectory = dir
		}
	}
	return runtime.OpenFileDialog(a.ctx, opts)
}

// LoginStart is what the operator needs to finish a sign-in in the browser.
type LoginStart struct {
	URL string `json:"url"`
	// UserCode is the code the provider's page shows, for a device sign-in.
	UserCode string `json:"userCode,omitempty"`
}

// loginTimeout is how long a sign-in may sit unfinished in the browser.
const loginTimeout = 10 * time.Minute

// BeginLogin checks req, starts its provider's sign-in, and opens the page in
// the browser. One sign-in runs at a time: starting another ends the first.
// The caller follows with CompleteLogin(req), or CancelLogin.
func (a *App) BeginLogin(req model.ImportRequest) (LoginStart, error) {
	if err := store.CheckRequest(NewRequest(req, "")); err != nil {
		return LoginStart{}, err
	}
	return a.beginLogin(req.Provider, "")
}

// BeginRelogin starts a sign-in that will replace the login of account id.
// The caller follows with CompleteRelogin(id), or CancelLogin.
func (a *App) BeginRelogin(id string) (LoginStart, error) {
	acc, err := a.root.Load(id)
	if err != nil {
		return LoginStart{}, err
	}
	return a.beginLogin(acc.Provider, id)
}

// beginLogin starts the sign-in. owner is the account it is for, or "" for a
// new account, so a Complete call cannot pick up a sign-in started for
// something else.
func (a *App) beginLogin(provider model.Provider, owner string) (LoginStart, error) {
	a.loginMu.Lock()
	defer a.loginMu.Unlock()
	if a.login != nil {
		a.login.Close()
		a.login = nil
	}
	login, err := quota.StartLogin(context.Background(), provider)
	if err != nil {
		return LoginStart{}, err
	}
	a.login, a.loginProvider, a.loginOwner = login, provider, owner
	if a.ctx != nil {
		runtime.BrowserOpenURL(a.ctx, login.URL)
	}
	return LoginStart{URL: login.URL, UserCode: login.UserCode}, nil
}

// CancelLogin ends the sign-in in progress, if any. A Complete call that is
// waiting on it returns an error.
func (a *App) CancelLogin() {
	a.loginMu.Lock()
	defer a.loginMu.Unlock()
	if a.login != nil {
		a.login.Close()
		a.login = nil
	}
}

// awaitLogin waits for the sign-in begun for provider and owner and returns
// the credential it produced. A sign-in begun for something else is ended.
func (a *App) awaitLogin(provider model.Provider, owner string) ([]byte, error) {
	a.loginMu.Lock()
	login := a.login
	if login != nil && (a.loginProvider != provider || a.loginOwner != owner) {
		login.Close()
		a.login, login = nil, nil
	}
	a.loginMu.Unlock()
	if login == nil {
		return nil, fmt.Errorf("no %s sign-in is in progress", provider)
	}
	ctx, cancel := context.WithTimeout(context.Background(), loginTimeout)
	defer cancel()
	credential, err := login.Wait(ctx)
	a.loginMu.Lock()
	if a.login == login {
		a.login = nil
	}
	a.loginMu.Unlock()
	if errors.Is(err, context.Canceled) {
		return nil, errors.New("sign-in cancelled")
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return nil, errors.New("sign-in timed out; start it again")
	}
	return credential, err
}

// Add stores credential as a new account and polls it. The name defaults to
// the login's identity and the tier to the provider's lowest; the poll then
// adopts the tier and dates the provider states. req.CredentialPath is not used.
func (a *App) Add(req model.ImportRequest, credential []byte) (model.AccountView, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	acc, err := a.root.Create(NewRequest(req, identityOf(credential)), credential)
	if err != nil {
		return model.AccountView{}, err
	}
	return a.pollAndView(acc)
}

// CompleteLogin waits for the sign-in BeginLogin(req) started, stores what it
// returned as a new account, and polls it. req.CredentialPath is not used.
func (a *App) CompleteLogin(req model.ImportRequest) (model.AccountView, error) {
	credential, err := a.awaitLogin(req.Provider, "")
	if err != nil {
		return model.AccountView{}, err
	}
	return a.Add(req, credential)
}

// CompleteRelogin waits for the sign-in BeginRelogin(id) started, replaces
// the account's credential with what it returned, and polls. A sign-in as a
// different identity than the stored one is refused and replaces nothing:
// the readings and ladder state belong to the stored login.
func (a *App) CompleteRelogin(id string) (model.AccountView, error) {
	acc, err := a.root.Load(id)
	if err != nil {
		a.CancelLogin()
		return model.AccountView{}, err
	}
	credential, err := a.awaitLogin(acc.Provider, id)
	if err != nil {
		return model.AccountView{}, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	before, after := identityOfFile(acc.CredentialFile), identityOf(credential)
	if before != "" && after != "" && !quota.SameIdentity(before, after) {
		// The two emails are the logins themselves. Naming them here would
		// put them on the window's error line and in the CLI error envelope.
		return model.AccountView{}, errors.New("that sign-in is a different login than this account stores; add it as its own account")
	}
	if err := a.root.PutCredential(id, credential); err != nil {
		return model.AccountView{}, err
	}
	return a.pollAndView(acc)
}

// identityOf names the login in a credential file's bytes, or "" when it
// names nobody or is not JSON.
func identityOf(credential []byte) string {
	cred := map[string]any{}
	if err := json.Unmarshal(credential, &cred); err != nil {
		return ""
	}
	return quota.Identity(cred)
}

func identityOfFile(path string) string {
	body, err := os.ReadFile(path)
	if err != nil {
		// No readable stored login means there is no identity to protect.
		return ""
	}
	return identityOf(body)
}

// pollAndView fetches the account's usage, stores it, and returns the view.
func (a *App) pollAndView(acc model.Account) (model.AccountView, error) {
	reading := quota.Fetch(context.Background(), acc, time.Now().UTC())
	if err := a.StoreReading(acc, reading); err != nil {
		return model.AccountView{}, err
	}
	return a.viewOf(acc.ID)
}

// ReplaceCredential copies a new credential file over the account's own copy
// and polls with it.
func (a *App) ReplaceCredential(id, path string) (model.AccountView, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.root.ReplaceCredential(id, path); err != nil {
		return model.AccountView{}, err
	}
	acc, err := a.root.Load(id)
	if err != nil {
		return model.AccountView{}, err
	}
	reading := quota.Fetch(context.Background(), acc, time.Now().UTC())
	if err := a.StoreReading(acc, reading); err != nil {
		return model.AccountView{}, err
	}
	return a.viewOf(acc.ID)
}

func (a *App) Import(req model.ImportRequest) (model.AccountView, error) {
	credential, err := store.ReadCredential(req.CredentialPath)
	if err != nil {
		return model.AccountView{}, err
	}
	return a.Add(req, credential)
}

// SaveSettings stores the window's preferences. A value outside its set is
// refused and nothing is saved.
func (a *App) SaveSettings(s model.Settings) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.root.PutSettings(s)
}

// ErrInvalid wraps a field the caller sent that UpdateAccount refuses.
var ErrInvalid = errors.New("invalid account")

// UpdateAccount saves the editable fields of acc. The name must be 1 to
// model.MaxNameLen characters, the tier one of the provider's, and the
// threshold 0 (the default) to 100; anything else returns ErrInvalid and
// saves nothing.
func (a *App) UpdateAccount(acc model.Account) (model.AccountView, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	current, err := a.root.Load(acc.ID)
	if err != nil {
		return model.AccountView{}, err
	}
	acc.Name = strings.TrimSpace(acc.Name)
	switch {
	case acc.Name == "" || utf8.RuneCountInString(acc.Name) > model.MaxNameLen:
		return model.AccountView{}, fmt.Errorf("%w: name must be 1 to %d characters", ErrInvalid, model.MaxNameLen)
	case acc.TierID != "" && !model.KnownTier(current.Provider, acc.TierID):
		return model.AccountView{}, fmt.Errorf("%w: %s has no tier %q", ErrInvalid, current.Provider, acc.TierID)
	// Written so that NaN, which compares false to everything, is refused too.
	case !(acc.UsageThreshold >= 0 && acc.UsageThreshold <= 100):
		return model.AccountView{}, fmt.Errorf("%w: threshold must be 1 to 100", ErrInvalid)
	}
	// Identity and the copied credential stay. The form edits the rest.
	current.Name = acc.Name
	current.TierID = acc.TierID
	current.ActivatedAt = acc.ActivatedAt
	// A renewal the operator changed is theirs, not an estimate any more.
	if !sameTime(current.RenewalAt, acc.RenewalAt) {
		current.RenewalEstimated = false
	}
	current.RenewalAt = acc.RenewalAt
	current.Ladder = acc.Ladder
	current.ChangeCommand = acc.ChangeCommand
	current.AutoReset = acc.AutoReset
	current.AgentBin = acc.AgentBin
	current.UsageThreshold = acc.UsageThreshold
	if err := a.root.Save(current); err != nil {
		return model.AccountView{}, err
	}
	return a.view(current)
}

func (a *App) Delete(id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.root.Delete(id)
}

// Poll fetches every account. One failure does not stop the others.
func (a *App) Poll() (model.Overview, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	accounts, err := a.root.List()
	if err != nil {
		return model.Overview{}, err
	}
	now := time.Now().UTC()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	for _, acc := range accounts {
		reading := quota.Fetch(ctx, acc, now)
		_ = a.StoreReading(acc, reading)
		if a.ctx != nil {
			runtime.EventsEmit(a.ctx, "polled", acc.ID)
		}
	}
	return a.overview()
}

// PollOne fetches a single account.
func (a *App) PollOne(id string) (model.AccountView, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	acc, err := a.root.Load(id)
	if err != nil {
		return model.AccountView{}, err
	}
	reading := quota.Fetch(context.Background(), acc, time.Now().UTC())
	if err := a.StoreReading(acc, reading); err != nil {
		return model.AccountView{}, err
	}
	return a.viewOf(acc.ID)
}

// RunLadder evaluates every account and applies a step only when the
// account has a change command. The command is not a shell line: it is a
// single executable plus the tokens {from} and {to}, split on spaces.
// Anything else is refused, and the refusal is the account's last error.
func (a *App) RunLadder() (model.Overview, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	accounts, err := a.root.List()
	if err != nil {
		return model.Overview{}, err
	}
	now := time.Now().UTC()
	for _, acc := range accounts {
		rp, st, ladderErr := a.stored(acc)
		if ladderErr != nil {
			// Held, and the file is left as it is for the operator to look at.
			continue
		}
		next, step := ladder.Decide(ladder.Input{Account: acc, Reading: rp, State: st, Now: now})
		if step != nil {
			// A reset step spends a banked reset; any other step runs the
			// account's change command.
			var err error
			if step.Reset {
				err = quota.RedeemReset(context.Background(), acc, step.CreditID)
			} else {
				err = applyStep(acc, *step)
			}
			entry := model.HistoryEntry{At: now, From: step.From, To: step.To, Reset: step.Reset}
			if err != nil {
				entry.Error = err.Error()
			}
			if err != nil {
				next.LastError = err.Error()
				next.Phase = model.PhaseClimbing
			} else {
				// LastStep is what stops Decide from repeating this change on
				// the same reading, so it is set before anything that can fail.
				next.LastStep = now
				next.LastError = ""
				if !step.Reset {
					acc.TierID = step.To
					if err := a.root.Save(acc); err != nil {
						next.LastError = "the plan changed, but the new tier was not saved: " + err.Error()
					}
				}
			}
			// A history line that cannot be written must not lose the ladder
			// state below; the failure is kept as the account's last error.
			if err := a.root.AppendHistory(acc.ID, entry); err != nil && next.LastError == "" {
				next.LastError = "the plan change was not recorded in the history: " + err.Error()
			}
		}
		if err := a.root.PutLadder(acc.ID, next); err != nil {
			return model.Overview{}, err
		}
	}
	return a.overview()
}

// Renew refreshes the account's own credential copy and polls again.
func (a *App) Renew(id string) (model.AccountView, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	acc, err := a.root.Load(id)
	if err != nil {
		return model.AccountView{}, err
	}
	if err := quota.Renew(context.Background(), acc); err != nil {
		return model.AccountView{}, err
	}
	reading := quota.Fetch(context.Background(), acc, time.Now().UTC())
	if err := a.StoreReading(acc, reading); err != nil {
		return model.AccountView{}, err
	}
	return a.viewOf(acc.ID)
}

func (a *App) TestAccount(id string) (model.ProbeResult, error) {
	acc, err := a.root.Load(id)
	if err != nil {
		return model.ProbeResult{}, err
	}
	return sandbox.Probe(context.Background(), acc), nil
}

func (a *App) CheckUpdate() (model.UpdateStatus, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	repo := os.Getenv("SUBMAN_UPDATE_REPO")
	if repo == "" {
		repo = update.DefaultRepo
	}
	rel, err := update.FetchLatest(ctx, repo)
	st := model.UpdateStatus{Current: Version}
	if errors.Is(err, update.ErrNoRelease) {
		st.Note = "no release published"
	} else if err != nil {
		st.Note = err.Error()
	} else {
		st = update.Check(Version, rel.Tag, rel.Assets).Status
		st.CheckedAt = time.Now().UTC().Format(time.RFC3339)
	}
	_ = a.root.PutUpdate(st)
	return st, nil
}

// applyStep is intentionally not a shell. The change command is an
// executable path the operator set, run with the from-tier and to-tier as
// two arguments. subman does not invent a provider's billing call.
func applyStep(acc model.Account, step model.Step) error {
	if acc.ChangeCommand == "" {
		return errNoCommand
	}
	return runChange(acc.ChangeCommand, step.From, step.To, acc.Home)
}

var errNoCommand = errString("no change command")

type errString string

func (e errString) Error() string { return string(e) }
