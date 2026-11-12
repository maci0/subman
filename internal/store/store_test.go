package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maci0/subman/internal/model"
)

func TestImportRoundTrip(t *testing.T) {
	dir := t.TempDir()
	root, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "src.json")
	if err := os.WriteFile(src, []byte(`{"claudeAiOauth":{"accessToken":"x"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	acc, err := root.Import(model.ImportRequest{
		Name: "work", Provider: model.ProviderClaude, CredentialPath: src,
		TierID: "pro", ActivatedAt: "2026-11-01", RenewalAt: "2026-12-01",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(acc.Home, "credential.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(acc.AgentHome); err != nil {
		t.Fatal(err)
	}
	loaded, err := root.Load(acc.ID)
	if err != nil || loaded.Name != "work" || loaded.TierID != "pro" || loaded.RenewalAt == nil {
		t.Fatalf("%+v %v", loaded, err)
	}
	list, err := root.List()
	if err != nil || len(list) != 1 {
		t.Fatal(err, len(list))
	}
	body, err := os.ReadFile(src)
	if err != nil || string(body) != `{"claudeAiOauth":{"accessToken":"x"}}` {
		t.Fatal("source credential was modified")
	}
}

func TestImportRejectsRelativeAndUnknown(t *testing.T) {
	root, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := root.Import(model.ImportRequest{Name: "a", Provider: "nope", CredentialPath: "/tmp/x"}); err == nil {
		t.Fatal("unknown provider")
	}
	if _, err := root.Import(model.ImportRequest{Name: "a", Provider: model.ProviderGrok, CredentialPath: "rel.json"}); err == nil {
		t.Fatal("relative path")
	}
}

func TestReadingSurvivesReload(t *testing.T) {
	root, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(t.TempDir(), "c.json")
	os.WriteFile(src, []byte(`{}`), 0o600)
	acc, err := root.Import(model.ImportRequest{Name: "g", Provider: model.ProviderGrok, CredentialPath: src, TierID: "plus"})
	if err != nil {
		t.Fatal(err)
	}
	u := 40.0
	if err := root.PutReading(acc.ID, model.Reading{OK: true, Windows: []model.Window{{Label: "Weekly", Util: &u}}}); err != nil {
		t.Fatal(err)
	}
	got, ok, err := root.Reading(acc.ID)
	if err != nil || !ok || !got.OK || len(got.Windows) != 1 {
		t.Fatalf("%+v %v %v", got, ok, err)
	}
}

func TestReplaceCredential(t *testing.T) {
	root, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(t.TempDir(), "old.json")
	if err := os.WriteFile(src, []byte(`{"token":"old"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	acc, err := root.Import(model.ImportRequest{Name: "c", Provider: model.ProviderClaude, CredentialPath: src, TierID: "pro"})
	if err != nil {
		t.Fatal(err)
	}
	fresh := filepath.Join(t.TempDir(), "new.json")
	if err := os.WriteFile(fresh, []byte(`{"token":"new"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := root.ReplaceCredential(acc.ID, fresh); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(acc.CredentialFile)
	if err != nil || string(got) != `{"token":"new"}` {
		t.Fatalf("copy holds %q, %v", got, err)
	}

	empty := filepath.Join(t.TempDir(), "empty.json")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"rel.json", empty, filepath.Join(t.TempDir(), "missing.json")} {
		if err := root.ReplaceCredential(acc.ID, bad); err == nil {
			t.Fatalf("%s was accepted", bad)
		}
	}
	got, err = os.ReadFile(acc.CredentialFile)
	if err != nil || string(got) != `{"token":"new"}` {
		t.Fatalf("a refused replace changed the copy: %q, %v", got, err)
	}
	if err := root.ReplaceCredential("nope", fresh); err == nil {
		t.Fatal("unknown account was accepted")
	}
}

func TestFailedPollKeepsLastMetersAsStale(t *testing.T) {
	root, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(t.TempDir(), "c.json")
	if err := os.WriteFile(src, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	acc, err := root.Import(model.ImportRequest{Name: "c", Provider: model.ProviderClaude, CredentialPath: src, TierID: "pro"})
	if err != nil {
		t.Fatal(err)
	}
	// A failure with nothing before it stays empty: there is nothing to carry.
	if err := root.PutReading(acc.ID, model.Reading{Error: "http-500"}); err != nil {
		t.Fatal(err)
	}
	got, _, err := root.Reading(acc.ID)
	if err != nil || got.Stale || len(got.Windows) != 0 {
		t.Fatalf("%+v %v", got, err)
	}
	u := 61.0
	if err := root.PutReading(acc.ID, model.Reading{OK: true, Plan: "Pro", Windows: []model.Window{{Label: "5-hour", Util: &u}}}); err != nil {
		t.Fatal(err)
	}
	if err := root.PutReading(acc.ID, model.Reading{Error: "http-500"}); err != nil {
		t.Fatal(err)
	}
	got, _, err = root.Reading(acc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.OK || got.Error != "http-500" || !got.Stale || got.Plan != "Pro" || len(got.Windows) != 1 || *got.Windows[0].Util != 61 {
		t.Fatalf("%+v", got)
	}
	// The next good poll replaces the carried meters and clears stale.
	v := 5.0
	if err := root.PutReading(acc.ID, model.Reading{OK: true, Windows: []model.Window{{Label: "5-hour", Util: &v}}}); err != nil {
		t.Fatal(err)
	}
	got, _, _ = root.Reading(acc.ID)
	if !got.OK || got.Stale || *got.Windows[0].Util != 5 {
		t.Fatalf("%+v", got)
	}
}

func TestHistoryNewestFirst(t *testing.T) {
	root, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(t.TempDir(), "c.json")
	if err := os.WriteFile(src, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	acc, err := root.Import(model.ImportRequest{Name: "c", Provider: model.ProviderClaude, CredentialPath: src, TierID: "pro"})
	if err != nil {
		t.Fatal(err)
	}
	if h, err := root.History(acc.ID); err != nil || len(h) != 0 {
		t.Fatalf("%v %v", h, err)
	}
	for _, e := range []model.HistoryEntry{{From: "pro", To: "max"}, {From: "max", To: "ultra", Error: "exit status 3"}} {
		if err := root.AppendHistory(acc.ID, e); err != nil {
			t.Fatal(err)
		}
	}
	h, err := root.History(acc.ID)
	if err != nil || len(h) != 2 {
		t.Fatalf("%+v %v", h, err)
	}
	if h[0].To != "ultra" || h[0].Error != "exit status 3" || h[1].To != "max" {
		t.Fatalf("%+v", h)
	}

	// Far more than maxHistory, each line short, so the tail read is what is tested.
	// Written straight to the file: AppendHistory now trims, which would leave
	// nothing past the window for the tail read to skip.
	var body strings.Builder
	for i := range 5000 {
		fmt.Fprintf(&body, "{\"from\":\"pro\",\"to\":\"t%d\"}\n", i)
	}
	if err := os.WriteFile(filepath.Join(root.Path, "accounts", acc.ID, "history.jsonl"), []byte(body.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err = root.History(acc.ID)
	if err != nil || len(h) != 50 || h[0].To != "t4999" || h[49].To != "t4950" {
		t.Fatalf("len %d first %+v err %v", len(h), h[0], err)
	}
}

func TestHistoryFileStaysBounded(t *testing.T) {
	root, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(t.TempDir(), "c.json")
	if err := os.WriteFile(src, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	acc, err := root.Import(model.ImportRequest{Name: "c", Provider: model.ProviderClaude, CredentialPath: src, TierID: "pro"})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < maxHistory+10; i++ {
		if err := root.AppendHistory(acc.ID, model.HistoryEntry{From: "pro", To: "max"}); err != nil {
			t.Fatal(err)
		}
	}
	h, err := root.History(acc.ID)
	if err != nil || len(h) != maxHistory {
		t.Fatalf("returned %d %v", len(h), err)
	}
	body, err := os.ReadFile(filepath.Join(root.Path, "accounts", acc.ID, "history.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(body), "\n"), "\n")
	if len(lines) != maxHistory {
		t.Fatalf("file kept %d lines", len(lines))
	}
}

func TestSettingsDefaultRoundTripAndRefusal(t *testing.T) {
	root, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s, err := root.Settings()
	if err != nil || s.AutoPollMinutes != 15 || !s.Notify || len(s.FoldedProviders) != 0 {
		t.Fatalf("defaults %+v %v", s, err)
	}
	want := model.Settings{AutoPollMinutes: 0, Notify: false, FoldedProviders: []model.Provider{model.ProviderCodex}}
	if err := root.PutSettings(want); err != nil {
		t.Fatal(err)
	}
	got, err := root.Settings()
	if err != nil || got.AutoPollMinutes != 0 || got.Notify || len(got.FoldedProviders) != 1 || got.FoldedProviders[0] != model.ProviderCodex {
		t.Fatalf("%+v %v", got, err)
	}
	if err := root.PutSettings(model.Settings{AutoPollMinutes: 7}); err == nil {
		t.Fatal("7 minutes was accepted")
	}
	if err := root.PutSettings(model.Settings{FoldedProviders: []model.Provider{"nope"}}); err == nil {
		t.Fatal("unknown provider was accepted")
	}
	if got, _ := root.Settings(); got.AutoPollMinutes != 0 {
		t.Fatalf("a refused save changed the file: %+v", got)
	}
}
