package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/maci0/subman/internal/model"
	"github.com/maci0/subman/internal/store"
)

func reading(ok bool, utils ...float64) model.Reading {
	r := model.Reading{OK: ok}
	for i := range utils {
		r.Windows = append(r.Windows, model.Window{Label: []string{"5-hour", "All models"}[i], Util: &utils[i]})
	}
	return r
}

func TestCrossed(t *testing.T) {
	under, over := reading(true, 40, 60), reading(true, 40, 97)
	if label, util, ok := crossed(&under, over, 95); !ok || label != "All models" || util != 97 {
		t.Fatalf("under to over: %q %v %v", label, util, ok)
	}
	if _, _, ok := crossed(nil, over, 95); !ok {
		t.Fatal("a first reading over the threshold did not cross")
	}
	if _, _, ok := crossed(&over, over, 95); ok {
		t.Fatal("staying over the threshold crossed again")
	}
	if _, _, ok := crossed(&under, under, 95); ok {
		t.Fatal("staying under the threshold crossed")
	}
	failed := reading(false, 40, 97)
	if _, _, ok := crossed(&under, failed, 95); ok {
		t.Fatal("a failed poll crossed")
	}
	if _, _, ok := crossed(&under, reading(true), 95); ok {
		t.Fatal("a reading with no meter crossed")
	}
}

// RunLadder with a spent meter runs the change command once, moves the tier,
// and records the change. The command is a script that writes its arguments.
func TestRunLadderStepsAndRecords(t *testing.T) {
	dir := t.TempDir()
	root, err := store.Open(filepath.Join(dir, "data"))
	if err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "cred.json")
	if err := os.WriteFile(src, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "change")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nprintf '%s' \"$*\" > \"$HOME/args\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	a := New(root)
	view, err := a.Import(model.ImportRequest{Name: "work", Provider: model.ProviderClaude, CredentialPath: src, TierID: "pro"})
	if err != nil {
		t.Fatal(err)
	}
	acc := view.Account
	acc.Ladder = true
	acc.ChangeCommand = bin
	if _, err := a.UpdateAccount(acc); err != nil {
		t.Fatal(err)
	}
	if err := root.PutReading(acc.ID, reading(true, 99)); err != nil {
		t.Fatal(err)
	}

	ov, err := a.RunLadder()
	if err != nil {
		t.Fatal(err)
	}
	got := ov.Accounts[0]
	if got.Account.TierID != "max" {
		t.Fatalf("tier %q, ladder %+v", got.Account.TierID, got.Ladder)
	}
	if len(got.History) != 1 || got.History[0].From != "pro" || got.History[0].To != "max" || got.History[0].Error != "" {
		t.Fatalf("history %+v", got.History)
	}
	args, err := os.ReadFile(filepath.Join(acc.Home, "args"))
	if err != nil || string(args) != "pro max" {
		t.Fatalf("command saw %q, %v", args, err)
	}
}

// A second run on the same reading must not run the change command again:
// the meter that caused the first change is still full until the next poll.
func TestRunLadderDoesNotRepeatOnTheSameReading(t *testing.T) {
	dir := t.TempDir()
	root, err := store.Open(filepath.Join(dir, "data"))
	if err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "cred.json")
	if err := os.WriteFile(src, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "change")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nprintf 'x' >> \"$HOME/runs\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	a := New(root)
	view, err := a.Import(model.ImportRequest{Name: "work", Provider: model.ProviderClaude, CredentialPath: src, TierID: "pro"})
	if err != nil {
		t.Fatal(err)
	}
	acc := view.Account
	acc.Ladder = true
	acc.ChangeCommand = bin
	if _, err := a.UpdateAccount(acc); err != nil {
		t.Fatal(err)
	}
	full := reading(true, 99)
	full.PolledAt = time.Now().UTC().Add(-time.Minute)
	if err := root.PutReading(acc.ID, full); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := a.RunLadder(); err != nil {
			t.Fatal(err)
		}
	}
	runs, err := os.ReadFile(filepath.Join(acc.Home, "runs"))
	if err != nil || string(runs) != "x" {
		t.Fatalf("the change command ran %d times, %v", len(runs), err)
	}
	ov, err := a.Overview()
	if err != nil || ov.Accounts[0].Account.TierID != "max" || len(ov.Accounts[0].History) != 1 || ov.Accounts[0].Next != nil {
		t.Fatalf("%+v %v", ov.Accounts[0], err)
	}
}

func TestBeginLoginRefusesABadRequestBeforeTheBrowser(t *testing.T) {
	root, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := New(root)
	for _, req := range []model.ImportRequest{
		{Name: "x", Provider: model.ProviderClaude, TierID: "nope"},
		{Name: "x", Provider: model.ProviderClaude, TierID: "pro", RenewalAt: "soon"},
		{Name: "x", Provider: "nope", TierID: "pro"},
	} {
		if _, err := a.BeginLogin(req); err == nil {
			t.Fatalf("%+v was accepted", req)
		}
	}
	if a.login != nil {
		t.Fatal("a refused request left a sign-in open")
	}
	if _, err := a.CompleteLogin(model.ImportRequest{Name: "x", Provider: model.ProviderClaude, TierID: "pro"}); err == nil {
		t.Fatal("CompleteLogin returned without a sign-in in progress")
	}
}

// A stored file that cannot be read must not empty the window, and an
// unreadable ladder state must hold the ladder: its last step is unknown.
func TestUnreadableStateIsContained(t *testing.T) {
	dir := t.TempDir()
	root, err := store.Open(filepath.Join(dir, "data"))
	if err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "cred.json")
	if err := os.WriteFile(src, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "change")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nprintf 'x' >> \"$HOME/runs\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	a := New(root)
	add := func(name string) model.Account {
		view, err := a.Import(model.ImportRequest{Name: name, Provider: model.ProviderClaude, CredentialPath: src, TierID: "pro"})
		if err != nil {
			t.Fatal(err)
		}
		acc := view.Account
		acc.Ladder = true
		acc.ChangeCommand = bin
		if _, err := a.UpdateAccount(acc); err != nil {
			t.Fatal(err)
		}
		return acc
	}
	badReading, badLadder, gone := add("bad reading"), add("bad ladder"), add("gone")
	if err := os.WriteFile(filepath.Join(badReading.Home, "reading.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := root.PutReading(badLadder.ID, reading(true, 99)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(badLadder.Home, "ladder.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gone.Home, "account.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	ov, err := a.RunLadder()
	if err != nil {
		t.Fatal(err)
	}
	if len(ov.Accounts) != 2 || len(ov.Problems) != 1 || ov.Problems[0] != gone.ID+": account.json is not valid" {
		t.Fatalf("accounts %d, problems %v", len(ov.Accounts), ov.Problems)
	}
	for _, v := range ov.Accounts {
		switch v.Account.ID {
		case badReading.ID:
			if v.Reading == nil || v.Reading.OK || v.Reading.Error == "" {
				t.Fatalf("unreadable reading shown as %+v", v.Reading)
			}
		case badLadder.ID:
			if v.Next != nil || v.Ladder.LastError == "" || v.Ladder.Phase != model.PhaseOff {
				t.Fatalf("unreadable ladder shown as %+v, next %+v", v.Ladder, v.Next)
			}
		}
	}
	// The account with a full meter and an unreadable ladder state did not step.
	if _, err := os.Stat(filepath.Join(badLadder.Home, "runs")); !os.IsNotExist(err) {
		t.Fatal("the change command ran from an unreadable ladder state:", err)
	}
	if body, _ := os.ReadFile(filepath.Join(badLadder.Home, "ladder.json")); string(body) != "{not json" {
		t.Fatalf("the unreadable ladder file was overwritten: %q", body)
	}
}

// Adding asks for nothing but the provider: the name comes from the login,
// and a good poll states the tier and the dates.
func TestAddNamesFromTheLoginAndAdoptsWhatThePollStates(t *testing.T) {
	root, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := New(root)
	view, err := a.Add(model.ImportRequest{Provider: model.ProviderClaude}, []byte(`{"email":"dev@example.com"}`))
	if err != nil {
		t.Fatal(err)
	}
	acc := view.Account
	// The tier is not guessed: it stays empty until a poll states it.
	if acc.Name != "dev@example.com" || acc.TierID != "" {
		t.Fatalf("defaults: %+v", acc)
	}
	nameless, err := a.Add(model.ImportRequest{Provider: model.ProviderGrok}, []byte(`{}`))
	if err != nil || nameless.Account.Name != "Grok" {
		t.Fatalf("a login that names nobody: %+v %v", nameless.Account, err)
	}

	typed := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	acc.RenewalAt = &typed
	if err := root.Save(acc); err != nil {
		t.Fatal(err)
	}
	// A failed poll states nothing.
	if err := a.StoreReading(acc, model.Reading{TierID: "ultra", RenewalAt: "2026-12-13T00:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := root.Load(acc.ID); got.TierID != "" || !got.RenewalAt.Equal(typed) {
		t.Fatalf("a failed poll was adopted: %+v", got)
	}
	good := reading(true, 10)
	good.TierID, good.RenewalAt, good.ActivatedAt = "max", "2026-12-13T00:36:52Z", "2026-09-29T22:52:42Z"
	if err := a.StoreReading(acc, good); err != nil {
		t.Fatal(err)
	}
	got, err := root.Load(acc.ID)
	if err != nil || got.TierID != "max" || got.RenewalAt.Format(time.RFC3339) != "2026-12-13T00:36:52Z" || got.ActivatedAt.Format(time.RFC3339) != "2026-09-29T22:52:42Z" {
		t.Fatalf("adopted: %+v %v", got, err)
	}
	// A poll that states no dates and an unknown tier leaves them alone.
	plain := reading(true, 10)
	plain.TierID = "enterprise"
	if err := a.StoreReading(got, plain); err != nil {
		t.Fatal(err)
	}
	if kept, _ := root.Load(acc.ID); kept.TierID != "max" || kept.RenewalAt == nil || kept.ActivatedAt == nil {
		t.Fatalf("a silent poll cleared the account: %+v", kept)
	}
}

// An estimated renewal fills an empty date and follows later estimates, and
// it never overwrites a date the operator typed or a provider stated.
func TestEstimatedRenewalNeverOverwritesARealOne(t *testing.T) {
	root, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := New(root)
	view, err := a.Add(model.ImportRequest{Provider: model.ProviderClaude}, []byte(`{"email":"dev@example.com"}`))
	if err != nil {
		t.Fatal(err)
	}
	estimate := func(when string) model.Reading {
		r := reading(true, 10)
		r.RenewalAt, r.RenewalEstimated = when, true
		return r
	}
	load := func() model.Account {
		acc, err := root.Load(view.Account.ID)
		if err != nil {
			t.Fatal(err)
		}
		return acc
	}
	if err := a.StoreReading(load(), estimate("2026-12-05T00:00:00Z")); err != nil {
		t.Fatal(err)
	}
	if acc := load(); acc.RenewalAt == nil || !acc.RenewalEstimated || acc.RenewalAt.Format(time.RFC3339) != "2026-12-05T00:00:00Z" {
		t.Fatalf("an estimate did not fill the empty date: %+v", acc)
	}
	if err := a.StoreReading(load(), estimate("2027-01-05T00:00:00Z")); err != nil {
		t.Fatal(err)
	}
	if acc := load(); acc.RenewalAt.Format(time.RFC3339) != "2027-01-05T00:00:00Z" {
		t.Fatalf("a later estimate did not replace the earlier one: %+v", acc)
	}
	// The operator types a date in Edit.
	acc := load()
	typed := time.Date(2026, 12, 20, 0, 0, 0, 0, time.UTC)
	acc.RenewalAt = &typed
	if _, err := a.UpdateAccount(acc); err != nil {
		t.Fatal(err)
	}
	if err := a.StoreReading(load(), estimate("2027-02-05T00:00:00Z")); err != nil {
		t.Fatal(err)
	}
	if acc := load(); acc.RenewalEstimated || !acc.RenewalAt.Equal(typed) {
		t.Fatalf("an estimate overwrote a typed date: %+v", acc)
	}
	// A provider that states the date wins over everything.
	stated := reading(true, 10)
	stated.RenewalAt = "2026-12-13T00:00:00Z"
	if err := a.StoreReading(load(), stated); err != nil {
		t.Fatal(err)
	}
	if acc := load(); acc.RenewalEstimated || acc.RenewalAt.Format(time.RFC3339) != "2026-12-13T00:00:00Z" {
		t.Fatalf("a stated date was not adopted: %+v", acc)
	}
}

// A banked reset is warned about once: on the poll where it comes within
// model.ResetExpiryWarning of expiring.
func TestExpiringWarnsOnceAsACreditEntersTheWindow(t *testing.T) {
	now := time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC)
	at := func(d time.Duration) *time.Time { v := now.Add(d); return &v }
	credits := func(polled time.Time) model.Reading {
		r := model.Reading{OK: true, PolledAt: polled}
		r.Credits.Resets = []model.ResetCredit{
			{ID: "soon", ExpiresAt: at(3 * 24 * time.Hour)},
			{ID: "later", ExpiresAt: at(30 * 24 * time.Hour)},
			{ID: "never"},
			{ID: "gone", ExpiresAt: at(-time.Hour)},
		}
		return r
	}
	next := credits(now)
	if got := expiring(nil, next, now); len(got) != 1 || got[0].ID != "soon" {
		t.Fatalf("first sight: %+v", got)
	}
	// Seen a day ago, when it was already inside the window: no second warning.
	prev := credits(now.Add(-24 * time.Hour))
	if got := expiring(&prev, next, now); len(got) != 0 {
		t.Fatalf("warned twice: %+v", got)
	}
	// Seen ten days ago, when it was still outside: this poll is the crossing.
	prev = credits(now.Add(-10 * 24 * time.Hour))
	if got := expiring(&prev, next, now); len(got) != 1 || got[0].ID != "soon" {
		t.Fatalf("crossing into the window: %+v", got)
	}
}
