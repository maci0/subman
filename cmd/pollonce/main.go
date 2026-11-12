// One-off poll used to exercise a real credential. Not part of the tool.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/maci0/subman/internal/model"
	"github.com/maci0/subman/internal/quota"
	"github.com/maci0/subman/internal/sandbox"
	"github.com/maci0/subman/internal/store"
)

func main() {
	// A sandboxed child is this binary started again: confine and exec it.
	if sandbox.MaybeReexec(os.Args) {
		return
	}
	root, err := store.Open(os.Getenv("SUBMAN_POLL_DATA"))
	if err != nil {
		fail(err)
	}
	acc, err := root.Import(model.ImportRequest{
		Name: "claude", Provider: model.ProviderClaude,
		CredentialPath: os.Getenv("SUBMAN_POLL_CRED"), TierID: "ultra",
	})
	if err != nil {
		fail(err)
	}
	if os.Getenv("SUBMAN_POLL_RAW") == "1" {
		raw(acc)
		return
	}
	reading := quota.Fetch(context.Background(), acc, time.Now().UTC())
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(reading); err != nil {
		fail(err)
	}
	if !reading.OK {
		os.Exit(1)
	}
}

func raw(acc model.Account) {
	reading := quota.Fetch(context.Background(), acc, time.Now().UTC())
	_ = reading
	fmt.Fprintln(os.Stderr, "raw mode needs the body; see fetch")
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
