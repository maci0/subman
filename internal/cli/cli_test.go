package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maci0/subman/internal/model"
	"github.com/maci0/subman/internal/store"
)

func buf(term bool) (IO, *bytes.Buffer, *bytes.Buffer) {
	out, err := &bytes.Buffer{}, &bytes.Buffer{}
	return IO{Out: out, Err: err, IsTerm: term}, out, err
}

func TestHelpMentionsSchema(t *testing.T) {
	io, out, _ := buf(true)
	if code := Run([]string{"--help"}, io); code != 0 {
		t.Fatal(code)
	}
	if !strings.Contains(out.String(), "schema") {
		t.Fatalf("help does not mention schema:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "--output") {
		t.Fatal("help does not mention --output")
	}
}

func TestVersionJSONWhenPiped(t *testing.T) {
	io, out, err := buf(false)
	if code := Run([]string{"version"}, io); code != 0 {
		t.Fatal(code, err.String())
	}
	var doc map[string]string
	if e := json.Unmarshal(out.Bytes(), &doc); e != nil {
		t.Fatal(e, out.String())
	}
	if doc["version"] != Version || doc["clispec"] != "0.3" {
		t.Fatal(doc)
	}
	if err.Len() != 0 {
		t.Fatal("version wrote to stderr:", err.String())
	}
}

func TestVersionTextOnTerminal(t *testing.T) {
	io, out, _ := buf(true)
	if code := Run([]string{"version"}, io); code != 0 {
		t.Fatal(code)
	}
	if strings.TrimSpace(out.String()) != Version {
		t.Fatalf("%q", out.String())
	}
}

func TestOutputFlagWinsOverTerminal(t *testing.T) {
	io, out, _ := buf(true)
	if code := Run([]string{"--output", "json", "version"}, io); code != 0 {
		t.Fatal(code)
	}
	if !json.Valid(out.Bytes()) {
		t.Fatal(out.String())
	}
}

func TestUnknownCommandIsUsage(t *testing.T) {
	io, out, err := buf(false)
	code := Run([]string{"nope"}, io)
	if code != 2 {
		t.Fatal(code)
	}
	if out.Len() != 0 {
		t.Fatal("usage wrote stdout:", out.String())
	}
	var env struct {
		Error struct {
			Kind string `json:"kind"`
		} `json:"error"`
	}
	if e := json.Unmarshal(err.Bytes(), &env); e != nil {
		t.Fatal(e, err.String())
	}
	if env.Error.Kind != "usage" {
		t.Fatal(env.Error.Kind)
	}
}

func TestSchemaValidates(t *testing.T) {
	io, out, _ := buf(false)
	if code := Run([]string{"schema"}, io); code != 0 {
		t.Fatal(code)
	}
	var doc map[string]any
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if doc["clispec"] != "0.3" || doc["name"] != "subman" {
		t.Fatal(doc["clispec"], doc["name"])
	}
	cmds, _ := doc["commands"].([]any)
	if len(cmds) != len(commands) {
		t.Fatalf("commands: %d", len(cmds))
	}
	for _, raw := range cmds {
		c := raw.(map[string]any)
		if c["description"] == "" || c["effects"] == "" {
			t.Fatal(c["name"])
		}
		if c["output_kind"] == "opaque" {
			if c["media_type"] == "" {
				t.Fatal(c["name"], "opaque without media type")
			}
			continue
		}
		if c["cardinality"] == "" {
			t.Fatal(c["name"], "no cardinality")
		}
		if c["output_fields"] == nil {
			t.Fatal(c["name"], "no output fields")
		}
	}
	errs, _ := doc["errors"].([]any)
	seen := map[float64]string{}
	for _, raw := range errs {
		e := raw.(map[string]any)
		code := e["exit_code"].(float64)
		if code < 1 || code > 255 {
			t.Fatal(e)
		}
		// Two error kinds may share a code. An outcome must not, and this
		// tool declares none, so the only check left is that the code the
		// emitter uses is the code the schema prints.
		if got := exitCode(e["kind"].(string)); got != int(code) {
			t.Fatalf("kind %s exits %d in schema and %d in emit", e["kind"], int(code), got)
		}
		seen[code] = e["kind"].(string)
	}
	if len(seen) == 0 {
		t.Fatal("no errors")
	}
}

func TestSchemaFiltersOneCommand(t *testing.T) {
	io, out, _ := buf(false)
	if code := Run([]string{"schema", "accounts", "list"}, io); code != 0 {
		t.Fatal(code)
	}
	var doc struct {
		Commands []struct{ Name string } `json:"commands"`
	}
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Commands) != 1 || doc.Commands[0].Name != "accounts list" {
		t.Fatal(doc.Commands)
	}
}

func TestAccountsListRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SUBMAN_DATA", dir)
	root, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "cred.json")
	if err := os.WriteFile(src, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := root.Import(model.ImportRequest{Name: "work", Provider: model.ProviderClaude, CredentialPath: src, TierID: "pro"}); err != nil {
		t.Fatal(err)
	}
	io, out, errb := buf(false)
	if code := Run([]string{"accounts", "list"}, io); code != 0 {
		t.Fatal(code, errb.String())
	}
	var doc struct {
		Accounts []struct {
			Name     string `json:"name"`
			Provider string `json:"provider"`
		} `json:"accounts"`
	}
	if e := json.Unmarshal(out.Bytes(), &doc); e != nil {
		t.Fatal(e, out.String())
	}
	if len(doc.Accounts) != 1 || doc.Accounts[0].Name != "work" || doc.Accounts[0].Provider != "claude" {
		t.Fatal(doc)
	}
}

func TestShowMissingIsNotFound(t *testing.T) {
	t.Setenv("SUBMAN_DATA", t.TempDir())
	io, _, errb := buf(false)
	code := Run([]string{"accounts", "show", "missing"}, io)
	if code != 4 {
		t.Fatal(code, errb.String())
	}
	var env struct {
		Error struct{ Kind string } `json:"error"`
	}
	if e := json.Unmarshal(errb.Bytes(), &env); e != nil || env.Error.Kind != "not_found" {
		t.Fatal(e, errb.String())
	}
}

func TestHelpIsTextEvenWhenPiped(t *testing.T) {
	io, out, _ := buf(false)
	if code := Run([]string{"help"}, io); code != 0 {
		t.Fatal(code)
	}
	if !strings.Contains(out.String(), "Usage:") {
		t.Fatal(out.String())
	}
}

// run executes one command with piped (JSON) output and decodes stdout.
func run(t *testing.T, wantCode int, args ...string) map[string]any {
	t.Helper()
	io, out, errb := buf(false)
	if code := Run(args, io); code != wantCode {
		t.Fatalf("%v exited %d, want %d: %s", args, code, wantCode, errb.String())
	}
	doc := map[string]any{}
	if out.Len() > 0 {
		if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
			t.Fatal(err, out.String())
		}
	}
	return doc
}

func TestAccountsAddSetRemove(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SUBMAN_DATA", filepath.Join(dir, "data"))
	cred := filepath.Join(dir, "cred.json")
	if err := os.WriteFile(cred, []byte(`{"token":"old"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	run(t, 2, "accounts", "add", "--name", "work")
	// Antigravity has no sign-in here, so it cannot be added without a file.
	run(t, 2, "accounts", "add", "--name", "work", "--provider", "antigravity")
	run(t, 2, "accounts", "add", "--name", "work", "--provider", "claude", "--credential", cred, "--tier", "nope")
	// With no --name the account is named after the login in the file.
	named := filepath.Join(dir, "named.json")
	if err := os.WriteFile(named, []byte(`{"email":"dev@example.com"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	auto := run(t, 0, "accounts", "add", "--provider", "claude", "--credential", named)["account"].(map[string]any)
	if auto["name"] != "dev@example.com" {
		t.Fatal(auto)
	}
	run(t, 0, "accounts", "remove", auto["id"].(string))
	added := run(t, 0, "accounts", "add", "--name", "work", "--provider", "claude", "--credential", cred, "--renewal", "2026-12-01")
	acc := added["account"].(map[string]any)
	id := acc["id"].(string)
	if acc["tierId"] != "pro" || acc["renewalAt"] != "2026-12-01T00:00:00Z" {
		t.Fatal(acc)
	}

	run(t, 2, "accounts", "set", id)
	run(t, 2, "accounts", "set", id, "--tier", "nope")
	run(t, 2, "accounts", "set", id, "--threshold", "250")
	run(t, 2, "accounts", "set", id, "--threshold", "NaN")
	// A bad credential path changes nothing, the name included.
	run(t, 1, "accounts", "set", id, "--name", "renamed", "--credential", filepath.Join(dir, "missing.json"))
	if shown := run(t, 0, "accounts", "show", id)["account"].(map[string]any); shown["name"] != "work" {
		t.Fatal("a refused set renamed the account:", shown["name"])
	}
	run(t, 2, "accounts", "add", "--name", "work", "--provider", "claude", "--renewal", "soon")
	run(t, 2, "accounts", "set", id, "--ladder", "maybe")
	run(t, 4, "accounts", "set", "c00000000", "--name", "x")
	fresh := filepath.Join(dir, "fresh.json")
	if err := os.WriteFile(fresh, []byte(`{"token":"new"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	set := run(t, 0, "accounts", "set", id, "--name", "day job", "--tier=max", "--ladder", "on", "--threshold", "80", "--renewal", "", "--credential", fresh)
	acc = set["account"].(map[string]any)
	if acc["name"] != "day job" || acc["tierId"] != "max" || acc["ladder"] != true || acc["usageThreshold"] != 80.0 || acc["renewalAt"] != nil {
		t.Fatal(acc)
	}
	copied, err := os.ReadFile(acc["credentialFile"].(string))
	if err != nil || string(copied) != `{"token":"new"}` {
		t.Fatalf("credential copy %q, %v", copied, err)
	}

	// The ladder is on with no change command: a dry run names the phase and runs nothing.
	dry := run(t, 0, "ladder", "run", "--dry-run")
	rows := dry["accounts"].([]any)
	if dry["dryRun"] != true || len(rows) != 1 || rows[0].(map[string]any)["phase"] != "unarmed" {
		t.Fatal(dry)
	}
	run(t, 2, "ladder", "run", "--now")

	removed := run(t, 0, "accounts", "remove", id)
	if removed["removed"] != true {
		t.Fatal(removed)
	}
	run(t, 4, "accounts", "remove", id)
	if _, err := os.Stat(acc["home"].(string)); !os.IsNotExist(err) {
		t.Fatal("account directory survived remove:", err)
	}
}
