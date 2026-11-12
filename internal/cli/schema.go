package cli

import (
	"encoding/json"
	"fmt"
	"strings"
)

// errorSpec is one declared failure. Exit codes are unique so a consumer
// can branch on the status alone.
type errorSpec struct {
	Kind        string `json:"kind"`
	ExitCode    int    `json:"exit_code"`
	Retryable   bool   `json:"retryable"`
	Description string `json:"description"`
}

// errorSpecs is the whole set. emit looks codes up here, and schema prints
// this slice, so the two cannot diverge.
var errorSpecs = []errorSpec{
	{"usage", 2, false, "The invocation was wrong. Nothing was attempted."},
	{"not_found", 4, false, "The named account does not exist."},
	{"io", 1, false, "A local filesystem or process operation failed."},
	{"unavailable", 8, true, "The release host or a usage endpoint could not be reached."},
	{"internal", 1, false, "The tool failed in a way it does not model."},
}

func exitCode(kind string) int {
	for _, e := range errorSpecs {
		if e.Kind == kind {
			return e.ExitCode
		}
	}
	return 1
}

type commandSpec struct {
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Effects      string   `json:"effects"`
	Cardinality  string   `json:"cardinality,omitempty"`
	OutputKind   string   `json:"output_kind,omitempty"`
	MediaType    string   `json:"media_type,omitempty"`
	OutputFields []any    `json:"output_fields,omitempty"`
	Args         []any    `json:"args,omitempty"`
	Errors       []string `json:"errors,omitempty"`
}

// commands is every invocable path, in help order. A command with no entry
// here is a command schema will not admit, which the test catches by
// comparing this list to the dispatcher.
var commands = []commandSpec{
	{
		Name: "accounts list", Description: "List subscriptions.",
		Effects: "read_only", Cardinality: "bounded",
		OutputFields: []any{
			field("accounts", "array", "One row per subscription.", items(obj(
				fld("id", "string", "Account id."),
				fld("name", "string", "Display name."),
				fld("provider", "string", "claude, codex, grok, or antigravity."),
				fld("tierId", "string", "Paid tier the operator set."),
				fld("ladder", "boolean", "Whether the ladder is enabled."),
				fld("renewalAt", "string", "Renewal instant, when one is set."),
			))),
		},
		Errors: []string{"usage", "io"},
	},
	{
		Name: "accounts show", Description: "Show one subscription and its last reading.",
		Effects: "read_only", Cardinality: "single",
		Args: []any{arg("id", "string", "Account id.", true)},
		OutputFields: []any{
			field("account", "object", "The subscription.", nil),
			field("reading", "object", "The last poll, when one exists.", nil),
		},
		Errors: []string{"usage", "not_found", "io"},
	},
	{
		Name: "accounts add", Description: "Add a subscription. Without --credential it signs in through the browser: the page to open is printed on stderr and the command waits up to ten minutes. With --credential it copies that file, which is read and never written.",
		Effects: "non_idempotent", Cardinality: "single",
		Args: []any{
			flag("--name", "string", "Display name, up to 64 characters. Defaults to the login's email, or the provider's name."),
			flag("--provider", "string", "claude, codex, grok, or antigravity. Required."),
			flag("--credential", "string", "Absolute path of the provider CLI's login file. Omit to sign in through the browser (claude, codex, grok). Required for antigravity."),
			flag("--tier", "string", "Paid tier id. Empty, shown as \"not reported\", until a poll states one or this flag sets it. A good poll replaces it with the tier the provider states."),
			flag("--activated", "string", "Activation day, YYYY-MM-DD or RFC 3339."),
			flag("--renewal", "string", "Next billing day, YYYY-MM-DD or RFC 3339."),
		},
		OutputFields: []any{field("account", "object", "The new subscription.", nil)},
		Errors:       []string{"usage", "unavailable", "io"},
	},
	{
		Name: "accounts set", Description: "Change a subscription's settings. Only the flags given are changed.",
		Effects: "idempotent", Cardinality: "single",
		Args: []any{
			arg("id", "string", "Account id.", true),
			flag("--name", "string", "Display name, 1 to 64 characters."),
			flag("--tier", "string", "Paid tier id of this provider."),
			flag("--activated", "string", "Activation day, YYYY-MM-DD or RFC 3339. Empty clears it."),
			flag("--renewal", "string", "Next billing day, YYYY-MM-DD or RFC 3339. Empty clears it."),
			flag("--ladder", "string", "on or off."),
			flag("--auto-reset", "string", "on or off."),
			flag("--change-command", "string", "One executable and its arguments, with {from} and {to}. Empty disarms the ladder."),
			flag("--agent-bin", "string", "Agent CLI for accounts test. Empty means the one on PATH."),
			flag("--threshold", "number", "Percent used that counts as spent, 1 to 100."),
			flag("--credential", "string", "Absolute path of a fresh login file to copy over this account's copy."),
		},
		OutputFields: []any{field("account", "object", "The subscription after the change.", nil)},
		Errors:       []string{"usage", "not_found", "io"},
	},
	{
		Name: "accounts remove", Description: "Delete a subscription's directory, including its credential copy. The vendor CLI's file is not touched.",
		Effects: "non_idempotent", Cardinality: "single",
		Args: []any{arg("id", "string", "Account id.", true)},
		OutputFields: []any{
			fld("id", "string", "Account id."),
			fld("removed", "boolean", "True once the directory is gone."),
		},
		Errors: []string{"usage", "not_found", "io"},
	},
	{
		Name: "accounts poll", Description: "Fetch usage for every account, or for one id.",
		Effects: "idempotent", Cardinality: "bounded",
		Args: []any{arg("id", "string", "Account id. Omit to poll every account.", false)},
		OutputFields: []any{
			field("readings", "array", "One result per account polled.", items(obj(
				fld("id", "string", "Account id."),
				fld("ok", "boolean", "Whether the poll returned a reading."),
				fld("error", "string", "Failure name, when ok is false."),
				fld("plan", "string", "Plan the payload named."),
			))),
		},
		Errors: []string{"usage", "not_found", "io"},
	},
	{
		Name: "accounts renew", Description: "Refresh the login in this account's own credential copy, then poll. The vendor CLI's file is not touched.",
		Effects: "idempotent", Cardinality: "single",
		Args: []any{arg("id", "string", "Account id.", true)},
		OutputFields: []any{
			fld("id", "string", "Account id."),
			fld("renewed", "boolean", "Whether the token exchange succeeded."),
			field("reading", "object", "The usage poll taken with the new token.", nil),
		},
		Errors: []string{"usage", "not_found", "unavailable", "io"},
	},
	{
		Name: "accounts test", Description: "Run the account's agent inside the sandbox and report the result.",
		Effects: "read_only", Cardinality: "single",
		Args: []any{arg("id", "string", "Account id.", true)},
		OutputFields: []any{
			fld("accountId", "string", "Account id."),
			fld("ok", "boolean", "Whether the agent exited 0."),
			fld("exit", "integer", "The agent's exit code."),
			fld("output", "string", "Combined stdout and stderr, capped."),
			fld("sandboxed", "boolean", "Whether Landlock was applied before the agent ran."),
			fld("note", "string", "Why a run failed, when it did."),
		},
		Errors: []string{"usage", "not_found", "io"},
	},
	{
		Name: "ladder run", Description: "Take every step that is due and record the result: run an account's plan-change command, or on the highest tier use a banked reset. With --dry-run, list the due steps and take none.",
		Effects: "non_idempotent", Cardinality: "bounded",
		Args: []any{flag("--dry-run", "boolean", "List what is due. Run no command and store nothing.")},
		OutputFields: []any{
			fld("dryRun", "boolean", "Whether this was a dry run."),
			field("accounts", "array", "One row per subscription.", items(obj(
				fld("id", "string", "Account id."),
				fld("name", "string", "Display name."),
				fld("phase", "string", "off, unarmed, climbing, waiting, or acting."),
				fld("tierId", "string", "Tier after this run."),
				fld("note", "string", "Why the ladder holds or moves."),
				fld("lastError", "string", "The last failed plan change, when one failed."),
				fld("next", "object", "The step that is due: from, to, reason, command, and reset when it uses a banked reset."),
			))),
		},
		Errors: []string{"usage", "io"},
	},
	{
		Name: "update", Description: "Compare this binary with the latest GitHub release and, without --check, replace it when the digest matches.",
		Effects: "idempotent", Cardinality: "single",
		Args: []any{flag("--check", "boolean", "Report only. Do not replace the binary.")},
		OutputFields: []any{
			fld("current", "string", "This binary's version."),
			fld("latest", "string", "Published version, when a release exists."),
			fld("available", "boolean", "A newer release is published."),
			fld("ahead", "boolean", "This build is newer than the published release."),
			fld("note", "string", "What the check decided."),
			fld("checkedAt", "string", "When the check ran."),
		},
		Errors: []string{"usage", "unavailable", "io"},
	},
	{
		Name: "schema", Description: "Print the CLI Spec v0.3 description of this tool. Needs no config and no network.",
		Effects: "read_only", Cardinality: "single",
		Args: []any{arg("command", "string", "Optional command path to print on its own.", false)},
		OutputFields: []any{
			fld("clispec", "string", "Schema version."),
			fld("name", "string", "Tool name."),
			fld("version", "string", "Tool version."),
			field("commands", "array", "Invocable commands.", obj()),
			field("errors", "array", "Declared error kinds.", obj()),
		},
		Errors: []string{"usage"},
	},
	{
		Name: "version", Description: "Print the version.",
		Effects: "read_only", Cardinality: "single",
		OutputFields: []any{
			fld("name", "string", "Tool name."),
			fld("version", "string", "Tool version."),
			fld("clispec", "string", "CLI Spec version this tool claims."),
		},
		Errors: []string{"usage"},
	},
	{
		Name: "help", Description: "Print help.",
		Effects: "read_only", OutputKind: "opaque", MediaType: "text/plain",
	},
}

func writeSchema(streams IO, args []string) error {
	doc := schemaDoc()
	if len(args) > 0 {
		path := strings.Join(args, " ")
		var found *commandSpec
		for i := range commands {
			if commands[i].Name == path {
				found = &commands[i]
				break
			}
		}
		if found == nil {
			return usageErr("no command "+path, "subman schema")
		}
		doc["commands"] = []commandSpec{*found}
	}
	enc := json.NewEncoder(streams.Out)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(doc)
}

func schemaDoc() map[string]any {
	globals := []any{
		map[string]any{
			"name": "--output", "type": "string", "short": "-o",
			"default": "auto", "enum": []string{"auto", "text", "json"},
			"description": "Output format. auto is text on a terminal and json when stdout is not a terminal.",
		},
	}
	return map[string]any{
		"clispec":     specVersion,
		"name":        "subman",
		"version":     Version,
		"description": "Track Claude, Codex, Grok, and Antigravity subscriptions.",
		"output":      map[string]string{"tty": "text", "piped": "json"},
		"global_args": globals,
		"commands":    commands,
		"errors":      errorSpecs,
	}
}

func arg(name, typ, desc string, required bool) map[string]any {
	return map[string]any{"name": name, "type": typ, "required": required, "description": desc}
}

func flag(name, typ, desc string) map[string]any {
	return map[string]any{"name": name, "type": typ, "description": desc}
}

func fld(name, typ, desc string) map[string]any {
	return map[string]any{"name": name, "type": typ, "description": desc}
}

func field(name, typ, desc string, items map[string]any) map[string]any {
	f := fld(name, typ, desc)
	if items != nil {
		f["items"] = items
	}
	return f
}

func items(elem map[string]any) map[string]any { return elem }

func obj(fields ...map[string]any) map[string]any {
	o := map[string]any{"type": "object"}
	if len(fields) > 0 {
		o["fields"] = fields
	}
	return o
}

// SchemaJSON is the document schema prints, for tests.
func SchemaJSON() ([]byte, error) {
	return json.Marshal(schemaDoc())
}

// CommandNames is every declared command path.
func CommandNames() []string {
	out := make([]string, len(commands))
	for i, c := range commands {
		out[i] = c.Name
	}
	return out
}

func init() {
	// Fail the build of a command whose effects string is not one of the
	// three the spec allows. A typo here would otherwise ship.
	for _, c := range commands {
		switch c.Effects {
		case "read_only", "idempotent", "non_idempotent":
		default:
			panic(fmt.Sprintf("command %q has effects %q", c.Name, c.Effects))
		}
		if c.OutputKind == "" && c.Cardinality == "" {
			panic(fmt.Sprintf("command %q declares no cardinality", c.Name))
		}
	}
}
