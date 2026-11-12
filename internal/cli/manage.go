package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/maci0/subman/internal/app"
	"github.com/maci0/subman/internal/model"
	"github.com/maci0/subman/internal/quota"
	"github.com/maci0/subman/internal/store"
)

// takeFlags splits args into flag values and positionals. known maps a flag
// name to whether it takes a value, written "--k v" or "--k=v". A flag that
// takes none is stored as "true".
func takeFlags(args []string, known map[string]bool) (map[string]string, []string, *Error) {
	flags := map[string]string{}
	var rest []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "--") {
			rest = append(rest, a)
			continue
		}
		name, value, inline := strings.Cut(a, "=")
		takesValue, ok := known[name]
		if !ok {
			return nil, nil, usageErr("unknown flag "+name, "")
		}
		switch {
		case !takesValue && inline:
			return nil, nil, usageErr(name+" takes no value", "")
		case !takesValue:
			value = "true"
		case !inline:
			if i+1 >= len(args) {
				return nil, nil, usageErr(name+" needs a value", "")
			}
			i++
			value = args[i]
		}
		flags[name] = value
	}
	return flags, rest, nil
}

func onOff(flag, value string) (bool, *Error) {
	switch value {
	case "on":
		return true, nil
	case "off":
		return false, nil
	default:
		return false, usageErr(flag+" is on or off, got "+value, "")
	}
}

// invalidOrIO maps an app error to its kind: a rejected field is the
// caller's mistake, anything else is the filesystem.
func invalidOrIO(err error) error {
	if errors.Is(err, app.ErrInvalid) {
		return usageErr(err.Error(), "")
	}
	return ioErr(err.Error())
}

const addUsage = "subman accounts add --provider claude|codex|grok|antigravity [--name NAME] [--credential /abs/path] [--tier ID] [--activated DATE] [--renewal DATE]"

func accountsAdd(root *store.Root, args []string, streams IO, format string) error {
	flags, rest, ferr := takeFlags(args, map[string]bool{
		"--name": true, "--provider": true, "--credential": true, "--tier": true, "--activated": true, "--renewal": true,
	})
	if ferr != nil {
		ferr.Hint = addUsage
		return ferr
	}
	if len(rest) != 0 {
		return usageErr("accounts add takes flags only", addUsage)
	}
	provider := model.Provider(flags["--provider"])
	if !provider.Known() {
		return usageErr("accounts add needs --provider", addUsage)
	}
	if flags["--credential"] == "" && !quota.CanLogin(provider) {
		return usageErr(fmt.Sprintf("%s has no sign-in here, so accounts add needs --credential", provider), addUsage)
	}
	req := model.ImportRequest{
		Name: flags["--name"], Provider: provider, CredentialPath: flags["--credential"],
		TierID: flags["--tier"], ActivatedAt: flags["--activated"], RenewalAt: flags["--renewal"],
	}
	// Every field is checked before a file is read or a sign-in opens the browser.
	if err := store.CheckRequest(app.NewRequest(req, "")); err != nil {
		return usageErr(err.Error(), addUsage)
	}
	var credential []byte
	if req.CredentialPath != "" {
		body, err := store.ReadCredential(req.CredentialPath)
		if err != nil {
			return ioErr(err.Error())
		}
		credential = body
	} else {
		body, err := signIn(provider, streams)
		if err != nil {
			return err
		}
		credential = body
	}
	view, err := app.New(root).Add(req, credential)
	if err != nil {
		return ioErr(err.Error())
	}
	acc := view.Account
	return emitDoc(streams, format, map[string]any{"account": acc}, func(w io.Writer) error {
		_, err := fmt.Fprintf(w, "%s  added  %s\n", acc.ID, acc.Name)
		return err
	})
}

// setFlagOrder is the order accounts set applies its flags in.
var setFlagOrder = []string{"--name", "--tier", "--activated", "--renewal", "--ladder", "--auto-reset", "--change-command", "--agent-bin", "--threshold"}

const setUsage = "subman accounts set <id> [--name NAME] [--tier ID] [--activated DATE] [--renewal DATE] [--ladder on|off] [--auto-reset on|off] [--change-command CMD] [--agent-bin PATH] [--threshold 1-100] [--credential /abs/path]"

func accountsSet(root *store.Root, args []string, streams IO, format string) error {
	flags, rest, ferr := takeFlags(args, map[string]bool{
		"--name": true, "--tier": true, "--activated": true, "--renewal": true, "--ladder": true, "--auto-reset": true,
		"--change-command": true, "--agent-bin": true, "--threshold": true, "--credential": true,
	})
	if ferr != nil {
		ferr.Hint = setUsage
		return ferr
	}
	if len(rest) != 1 || len(flags) == 0 {
		return usageErr("accounts set needs an id and at least one flag", setUsage)
	}
	id := rest[0]
	acc, err := root.Load(id)
	if err != nil {
		return missingOrIO(id, err)
	}
	// In flag-table order, so the same bad invocation reports the same error.
	for _, name := range setFlagOrder {
		value, given := flags[name]
		if !given {
			continue
		}
		switch name {
		case "--name":
			acc.Name = value
		case "--tier":
			acc.TierID = value
		case "--activated", "--renewal":
			t, err := store.ParseTime(value)
			if err != nil {
				return usageErr(name+": "+err.Error(), setUsage)
			}
			if name == "--activated" {
				acc.ActivatedAt = t
			} else {
				// The operator typed this date, so it is no longer an estimate
				// a poll worked out, even when the day is the same one.
				acc.RenewalAt = t
				acc.RenewalEstimated = false
			}
		case "--ladder", "--auto-reset":
			on, perr := onOff(name, value)
			if perr != nil {
				return perr
			}
			if name == "--ladder" {
				acc.Ladder = on
			} else {
				acc.AutoReset = on
			}
		case "--change-command":
			acc.ChangeCommand = value
		case "--agent-bin":
			acc.AgentBin = value
		case "--threshold":
			n, err := strconv.ParseFloat(value, 64)
			if err != nil {
				return usageErr("--threshold is a number from 1 to 100, got "+value, setUsage)
			}
			acc.UsageThreshold = n
		}
	}
	// The new credential is read before anything is saved, so a bad path
	// changes nothing.
	var credential []byte
	if path, ok := flags["--credential"]; ok {
		credential, err = store.ReadCredential(path)
		if err != nil {
			return ioErr(err.Error())
		}
	}
	view, err := app.New(root).UpdateAccount(acc)
	if err != nil {
		return invalidOrIO(err)
	}
	if credential != nil {
		if err := root.PutCredential(id, credential); err != nil {
			return ioErr(err.Error())
		}
	}
	return emitDoc(streams, format, map[string]any{"account": view.Account}, func(w io.Writer) error {
		_, err := fmt.Fprintf(w, "%s  saved  %s\n", view.Account.ID, view.Account.Name)
		return err
	})
}

func accountsRemove(root *store.Root, id string, streams IO, format string) error {
	if _, err := root.Load(id); err != nil {
		return missingOrIO(id, err)
	}
	if err := root.Delete(id); err != nil {
		return ioErr(err.Error())
	}
	return emitDoc(streams, format, map[string]any{"id": id, "removed": true}, func(w io.Writer) error {
		_, err := fmt.Fprintf(w, "%s  removed\n", id)
		return err
	})
}

func dispatchLadder(args []string, streams IO, format string) error {
	if len(args) == 0 || args[0] != "run" {
		return usageErr("ladder needs the subcommand run", "subman ladder run [--dry-run]")
	}
	flags, rest, ferr := takeFlags(args[1:], map[string]bool{"--dry-run": false})
	if ferr != nil {
		ferr.Hint = "subman ladder run [--dry-run]"
		return ferr
	}
	if len(rest) != 0 {
		return usageErr("ladder run takes no arguments", "subman ladder run [--dry-run]")
	}
	root, err := openRoot()
	if err != nil {
		return err
	}
	_, dry := flags["--dry-run"]
	a := app.New(root)
	var ov model.Overview
	if dry {
		ov, err = a.Overview()
	} else {
		ov, err = a.RunLadder()
	}
	if err != nil {
		return ioErr(err.Error())
	}
	type row struct {
		ID        string      `json:"id"`
		Name      string      `json:"name"`
		Phase     string      `json:"phase"`
		TierID    string      `json:"tierId"`
		Note      string      `json:"note,omitempty"`
		LastError string      `json:"lastError,omitempty"`
		Next      *model.Step `json:"next,omitempty"`
	}
	rows := make([]row, 0, len(ov.Accounts))
	for _, v := range ov.Accounts {
		rows = append(rows, row{
			ID: v.Account.ID, Name: v.Account.Name, Phase: string(v.Ladder.Phase), TierID: v.Account.TierID,
			Note: v.Ladder.Note, LastError: v.Ladder.LastError, Next: v.Next,
		})
	}
	return emitDoc(streams, format, map[string]any{"dryRun": dry, "accounts": rows}, func(w io.Writer) error {
		if len(rows) == 0 {
			_, err := fmt.Fprintln(w, "no accounts")
			return err
		}
		for _, r := range rows {
			line := fmt.Sprintf("%s  %-9s %-8s %s", r.ID, r.Phase, r.TierID, r.Note)
			if r.Next != nil {
				if r.Next.Reset {
					line += "  [due: use a banked reset]"
				} else {
					line += fmt.Sprintf("  [due: %s to %s]", r.Next.From, r.Next.To)
				}
			}
			if r.LastError != "" {
				line += "  error: " + r.LastError
			}
			fmt.Fprintln(w, line)
		}
		return nil
	})
}

// signInTimeout is how long accounts add waits for the browser.
const signInTimeout = 10 * time.Minute

// signIn runs the provider's sign-in. The page to open, and the device code
// when there is one, go to stderr so stdout stays the result document.
func signIn(provider model.Provider, streams IO) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), signInTimeout)
	defer cancel()
	login, err := quota.StartLogin(ctx, provider)
	if err != nil {
		return nil, unavailable(err.Error())
	}
	defer login.Close()
	fmt.Fprintf(streams.Err, "Open this page to sign in to %s:\n%s\n", provider, login.URL)
	if login.UserCode != "" {
		fmt.Fprintf(streams.Err, "The page should show the code %s\n", login.UserCode)
	}
	credential, err := login.Wait(ctx)
	if err != nil {
		return nil, unavailable("sign-in did not finish: " + err.Error())
	}
	return credential, nil
}
