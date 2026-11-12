// Package cli is the command line, written to The CLI Spec v0.3
// (https://clispec.dev).
//
// Data goes to stdout. Diagnostics and the error envelope go to stderr.
// With no --output, a terminal gets text and a pipe gets JSON. schema
// describes every command and needs no config and no network. Exit codes
// come from the error table in schema.go, so a consumer can branch before
// it parses anything.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/maci0/subman/internal/app"
	"github.com/maci0/subman/internal/model"
	"github.com/maci0/subman/internal/quota"
	"github.com/maci0/subman/internal/sandbox"
	"github.com/maci0/subman/internal/store"
	"github.com/maci0/subman/internal/update"
	"github.com/maci0/subman/internal/version"
	"golang.org/x/term"
)

const specVersion = "0.3"

// Version is the release this binary reports. main sets it from its own
// version before Run, which a release build stamps with -X; left unset it
// is dev, so the CLI cannot report a release this tree no longer is.
var Version = version.Release

// IO is where a run reads and writes. Tests pass buffers. A real run uses
// the process streams, and Format auto then checks whether stdout is a TTY.
type IO struct {
	In     io.Reader
	Out    io.Writer
	Err    io.Writer
	IsTerm bool
}

func Std() IO {
	return IO{In: os.Stdin, Out: os.Stdout, Err: os.Stderr, IsTerm: term.IsTerminal(int(os.Stdout.Fd()))}
}

// Error is a failure with a declared kind. The kind's exit code lives in
// the error table, not on the value, so the two cannot drift.
type Error struct {
	Kind    string
	Message string
	Hint    string
}

func (e *Error) Error() string { return e.Message }

func usageErr(msg, hint string) *Error { return &Error{Kind: "usage", Message: msg, Hint: hint} }
func notFound(msg string) *Error       { return &Error{Kind: "not_found", Message: msg} }
func unavailable(msg string) *Error    { return &Error{Kind: "unavailable", Message: msg} }
func ioErr(msg string) *Error          { return &Error{Kind: "io", Message: msg} }

// Run parses args (without argv0) and returns the process exit code.
func Run(args []string, streams IO) int {
	format, rest, err := takeGlobals(args)
	if err != nil {
		return emit(streams, format, err)
	}
	resolved := format
	if resolved == "auto" {
		if streams.IsTerm {
			resolved = "text"
		} else {
			resolved = "json"
		}
	}
	if err := dispatch(rest, streams, resolved); err != nil {
		return emit(streams, resolved, err)
	}
	return 0
}

func takeGlobals(args []string) (string, []string, *Error) {
	format := "auto"
	rest := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			rest = append(rest, args[i+1:]...)
			return format, rest, nil
		case a == "--output" || a == "-o":
			if i+1 >= len(args) {
				return "", nil, usageErr("--output needs a value", "auto, text, or json")
			}
			i++
			format = args[i]
		case strings.HasPrefix(a, "--output="):
			format = strings.TrimPrefix(a, "--output=")
		default:
			rest = append(rest, a)
		}
	}
	switch format {
	case "auto", "text", "json":
		return format, rest, nil
	default:
		return "", nil, usageErr("unknown output format "+format, "auto, text, or json")
	}
}

// wantsHelp reports a help request. --help and -h are help wherever they
// appear, so a subcommand never reads one as an account id or an unknown
// flag. A value that merely contains the letters, such as --help=me, is not.
func wantsHelp(args []string) bool {
	for _, a := range args {
		if a == "--help" || a == "-h" || a == "help" {
			return true
		}
	}
	return false
}

func dispatch(args []string, streams IO, format string) error {
	if len(args) == 0 {
		return usageErr("a command is required", "subman --help")
	}
	if wantsHelp(args) {
		return writeHelp(streams.Out)
	}
	switch args[0] {
	case "version", "--version", "-v":
		return writeVersion(streams, format)
	case "schema":
		return writeSchema(streams, args[1:])
	case "accounts":
		return dispatchAccounts(args[1:], streams, format)
	case "ladder":
		return dispatchLadder(args[1:], streams, format)
	case "update":
		return dispatchUpdate(args[1:], streams, format)
	default:
		return usageErr("unknown command "+args[0], "subman --help")
	}
}

func writeHelp(w io.Writer) error {
	_, err := fmt.Fprintf(w, `subman %s

Track Claude, Codex, Grok, and Antigravity subscriptions.

Usage:
  subman [--output auto|text|json] <command>

Commands:
  accounts list                 list subscriptions
  accounts show <id>            one subscription, with its last reading
  accounts add --provider P     sign in, or copy a credential, into a new subscription
  accounts set <id> --...       change a subscription's settings
  accounts remove <id>          delete a subscription and its credential copy
  accounts poll [<id>]          fetch usage
  accounts renew <id>           refresh the login in this account's copy
  accounts test <id>            run the agent inside the sandbox
  ladder run [--dry-run]        take the due plan changes and banked resets, or list them
  update [--check]              compare with the latest release
  schema [command]              machine-readable description of this CLI
  version                       print the version

--output defaults to text on a terminal and json when stdout is a pipe.
Data is written to stdout. Errors are written to stderr.
schema lists every command, its effects, and the exit code for each error.
`, Version)
	return err
}

func writeVersion(streams IO, format string) error {
	doc := map[string]string{"name": "subman", "version": Version, "clispec": specVersion}
	return emitDoc(streams, format, doc, func(w io.Writer) error {
		_, err := fmt.Fprintln(w, Version)
		return err
	})
}

func dispatchAccounts(args []string, streams IO, format string) error {
	if len(args) == 0 {
		return usageErr("accounts needs a subcommand", "list, show, add, set, remove, poll, renew, test")
	}
	root, err := openRoot()
	if err != nil {
		return err
	}
	switch args[0] {
	case "list":
		if len(args) != 1 {
			return usageErr("accounts list takes no arguments", "")
		}
		return accountsList(root, streams, format)
	case "show":
		if len(args) != 2 {
			return usageErr("accounts show needs an id", "subman accounts show <id>")
		}
		return accountsShow(root, args[1], streams, format)
	case "poll":
		switch len(args) {
		case 1:
			return accountsPoll(root, "", streams, format)
		case 2:
			return accountsPoll(root, args[1], streams, format)
		default:
			return usageErr("accounts poll takes at most one id", "")
		}
	case "renew":
		if len(args) != 2 {
			return usageErr("accounts renew needs an id", "subman accounts renew <id>")
		}
		return accountsRenew(root, args[1], streams, format)
	case "add":
		return accountsAdd(root, args[1:], streams, format)
	case "set":
		return accountsSet(root, args[1:], streams, format)
	case "remove":
		if len(args) != 2 {
			return usageErr("accounts remove needs an id", "subman accounts remove <id>")
		}
		return accountsRemove(root, args[1], streams, format)
	case "test":
		if len(args) != 2 {
			return usageErr("accounts test needs an id", "subman accounts test <id>")
		}
		return accountsTest(root, args[1], streams, format)
	default:
		return usageErr("unknown accounts command "+args[0], "list, show, add, set, remove, poll, renew, test")
	}
}

func accountsList(root *store.Root, streams IO, format string) error {
	accs, err := root.List()
	if err != nil {
		return ioErr(err.Error())
	}
	type row struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Provider string `json:"provider"`
		TierID   string `json:"tierId"`
		Ladder   bool   `json:"ladder"`
		Renewal  string `json:"renewalAt,omitempty"`
	}
	rows := make([]row, 0, len(accs))
	for _, a := range accs {
		r := row{ID: a.ID, Name: a.Name, Provider: string(a.Provider), TierID: a.TierID, Ladder: a.Ladder}
		if a.RenewalAt != nil {
			r.Renewal = a.RenewalAt.UTC().Format(time.RFC3339)
		}
		rows = append(rows, r)
	}
	return emitDoc(streams, format, map[string]any{"accounts": rows}, func(w io.Writer) error {
		if len(rows) == 0 {
			_, err := fmt.Fprintln(w, "no accounts")
			return err
		}
		fmt.Fprintf(w, "%-10s %-16s %-14s %-8s %s\n", "ID", "NAME", "PROVIDER", "TIER", "RENEWAL")
		for _, r := range rows {
			renew := r.Renewal
			if renew == "" {
				renew = "n/a"
			}
			fmt.Fprintf(w, "%-10s %-16s %-14s %-8s %s\n", r.ID, r.Name, r.Provider, r.TierID, renew)
		}
		return nil
	})
}

func accountsShow(root *store.Root, id string, streams IO, format string) error {
	acc, err := root.Load(id)
	if err != nil {
		return missingOrIO(id, err)
	}
	reading, ok, err := root.Reading(id)
	if err != nil {
		return ioErr(err.Error())
	}
	doc := map[string]any{"account": acc}
	if ok {
		doc["reading"] = reading
	}
	return emitDoc(streams, format, doc, func(w io.Writer) error {
		fmt.Fprintf(w, "%s  %s  %s\n", acc.Name, acc.Provider, acc.TierID)
		fmt.Fprintf(w, "id        %s\n", acc.ID)
		fmt.Fprintf(w, "home      %s\n", acc.Home)
		if acc.RenewalAt != nil {
			fmt.Fprintf(w, "renewal   %s\n", acc.RenewalAt.UTC().Format("2006-01-02"))
		}
		if !ok {
			_, err := fmt.Fprintln(w, "reading   none")
			return err
		}
		fmt.Fprintf(w, "plan      %s\n", orNA(reading.Plan))
		if !reading.OK {
			fmt.Fprintf(w, "error     %s\n", reading.Error)
		}
		for _, win := range reading.Windows {
			util := "n/a"
			if win.Util != nil {
				util = fmt.Sprintf("%.0f%%", *win.Util)
			}
			fmt.Fprintf(w, "  %-16s %s\n", win.Label, util)
		}
		if reading.Credits.ResetCount != nil {
			fmt.Fprintf(w, "resets    %d\n", *reading.Credits.ResetCount)
		}
		return nil
	})
}

func accountsPoll(root *store.Root, id string, streams IO, format string) error {
	var accs []model.Account
	if id == "" {
		var err error
		accs, err = root.List()
		if err != nil {
			return ioErr(err.Error())
		}
	} else {
		acc, err := root.Load(id)
		if err != nil {
			return missingOrIO(id, err)
		}
		accs = []model.Account{acc}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	now := time.Now().UTC()
	type row struct {
		ID    string        `json:"id"`
		OK    bool          `json:"ok"`
		Error string        `json:"error,omitempty"`
		Plan  string        `json:"plan,omitempty"`
		Read  model.Reading `json:"reading"`
	}
	rows := make([]row, 0, len(accs))
	for _, acc := range accs {
		reading := quota.Fetch(ctx, acc, now)
		if err := app.New(root).StoreReading(acc, reading); err != nil {
			return ioErr(err.Error())
		}
		rows = append(rows, row{ID: acc.ID, OK: reading.OK, Error: reading.Error, Plan: reading.Plan, Read: reading})
	}
	return emitDoc(streams, format, map[string]any{"readings": rows}, func(w io.Writer) error {
		if len(rows) == 0 {
			_, err := fmt.Fprintln(w, "no accounts")
			return err
		}
		for _, r := range rows {
			if r.OK {
				fmt.Fprintf(w, "%s  ok  %s\n", r.ID, orNA(r.Plan))
			} else {
				fmt.Fprintf(w, "%s  failed  %s\n", r.ID, orNA(r.Error))
			}
		}
		return nil
	})
}

func accountsRenew(root *store.Root, id string, streams IO, format string) error {
	acc, err := root.Load(id)
	if err != nil {
		return missingOrIO(id, err)
	}
	if err := quota.Renew(context.Background(), acc); err != nil {
		return unavailable(err.Error())
	}
	reading := quota.Fetch(context.Background(), acc, time.Now().UTC())
	if err := app.New(root).StoreReading(acc, reading); err != nil {
		return ioErr(err.Error())
	}
	return emitDoc(streams, format, map[string]any{"id": id, "renewed": true, "reading": reading}, func(w io.Writer) error {
		state := "renewed"
		if !reading.OK {
			state = "renewed, poll failed: " + reading.Error
		}
		_, err := fmt.Fprintf(w, "%s  %s\n", id, state)
		return err
	})
}

func accountsTest(root *store.Root, id string, streams IO, format string) error {
	acc, err := root.Load(id)
	if err != nil {
		return missingOrIO(id, err)
	}
	res := sandbox.Probe(context.Background(), acc)
	return emitDoc(streams, format, res, func(w io.Writer) error {
		state := "failed"
		if res.OK {
			state = "passed"
		}
		fmt.Fprintf(w, "%s  %s\n", acc.ID, state)
		if res.Note != "" {
			fmt.Fprintln(w, res.Note)
		}
		if res.Output != "" {
			fmt.Fprintln(w, res.Output)
		}
		return nil
	})
}

func dispatchUpdate(args []string, streams IO, format string) error {
	checkOnly := false
	for _, a := range args {
		switch a {
		case "--check":
			checkOnly = true
		default:
			return usageErr("unknown flag "+a, "subman update [--check]")
		}
	}
	repo := os.Getenv("SUBMAN_UPDATE_REPO")
	if repo == "" {
		repo = update.DefaultRepo
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	rel, err := update.FetchLatest(ctx, repo)
	st := model.UpdateStatus{Current: Version}
	if errors.Is(err, update.ErrNoRelease) {
		st.Note = "no release published"
	} else if err != nil {
		return unavailable(err.Error())
	} else {
		plan := update.Check(Version, rel.Tag, rel.Assets)
		st = plan.Status
		st.CheckedAt = time.Now().UTC().Format(time.RFC3339)
		if !checkOnly && plan.Status.Available {
			exe, err := os.Executable()
			if err != nil {
				return ioErr(err.Error())
			}
			if err := update.Install(ctx, plan, exe); err != nil {
				return ioErr(err.Error())
			}
			st.Note = "replaced " + exe
		}
	}
	return emitDoc(streams, format, st, func(w io.Writer) error {
		fmt.Fprintf(w, "current %s\n", st.Current)
		if st.Latest != "" {
			fmt.Fprintf(w, "latest  %s\n", st.Latest)
		}
		if st.Note != "" {
			fmt.Fprintln(w, st.Note)
		}
		return nil
	})
}

func missingOrIO(id string, err error) error {
	if os.IsNotExist(err) {
		return notFound("no account " + id)
	}
	return ioErr(err.Error())
}

func openRoot() (*store.Root, error) {
	path := os.Getenv("SUBMAN_DATA")
	root, err := store.Open(path)
	if err != nil {
		return nil, ioErr(err.Error())
	}
	return root, nil
}

func emitDoc(streams IO, format string, doc any, text func(io.Writer) error) error {
	if format == "json" {
		enc := json.NewEncoder(streams.Out)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		return enc.Encode(doc)
	}
	return text(streams.Out)
}

func emit(streams IO, format string, err error) int {
	var ce *Error
	if !errors.As(err, &ce) {
		ce = &Error{Kind: "internal", Message: err.Error()}
	}
	code := exitCode(ce.Kind)
	if format == "json" {
		body := map[string]string{"kind": ce.Kind, "message": ce.Message}
		if ce.Hint != "" {
			body["hint"] = ce.Hint
		}
		enc := json.NewEncoder(streams.Err)
		enc.SetEscapeHTML(false)
		_ = enc.Encode(map[string]any{"error": body})
		return code
	}
	fmt.Fprintf(streams.Err, "error: %s\n", ce.Message)
	if ce.Hint != "" {
		fmt.Fprintf(streams.Err, "%s\n", ce.Hint)
	}
	return code
}

func orNA(s string) string {
	if s == "" {
		return "n/a"
	}
	return s
}
