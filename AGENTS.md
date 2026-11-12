# subman

Go desktop tool that tracks Claude, Codex, Grok, and Antigravity subscriptions and walks a paid-tier ladder. Parent rules: `/home/maci/Desktop/Projects/maci0/AGENTS.md`. This file only adds local facts.

## Layout

- `main.go` window, and the CLI when the arguments are a command. `cmd/subman` is the CLI without WebKit. `internal/cli` implements The CLI Spec v0.3.
- `internal/model` documents the window reads.
- `internal/store` one directory per account under the data root, plus `settings.json` (window preferences) and `update.json`.
- `internal/quota` parsers, the poll, token renewal, and the in-app sign-in (`login.go`: OAuth with PKCE for Claude and Codex, a device code for Grok). Parsers are pure.
- `internal/ladder` the tier decision. It never runs a command.
- `internal/sandbox` Landlock for every child: the agent test, the Antigravity poll, a plan-change command. `/` read-only, the account agent directory writable. No user namespace. A subman binary is its own launcher: every `main` calls `sandbox.MaybeReexec` first, and `Run` starts the running executable again. Under `go test` the launcher is built from `cmd/sandbox-helper`. A bare program name is looked up on `PATH` after confinement.
- `internal/update` GitHub release check and checksum-verified replace.
- `internal/desktop` the desktop session over D-Bus: a notification, and the light or dark preference from the settings portal. Linux only; other platforms return `ErrUnsupported`.
- `internal/app` the Wails API. The CLI calls it too for add, set, remove, and the ladder run, so both share one implementation. It stays free of cgo.
- `frontend/` Vite, Preact, Tailwind, shadcn components under `src/components/ui` (added with `bunx shadcn add`, then owned here). Radix and other React packages resolve to `preact/compat`; `bunfig.toml` keeps React itself out of `node_modules`. Light and dark tokens live in `src/index.css` and switch on `prefers-color-scheme`; there is no in-app toggle. `public/logo.svg` is the mark and the favicon. `bun` only.
- `docs/PROVENANCE.md` append-only ledger.

## Contracts

- An account holds its own login: one subman signed in for (Claude, Codex, Grok) or a copy of a vendor CLI file (required for Antigravity). subman never writes `~/.claude`, `~/.codex`, `~/.grok`, or `~/.gemini`. Token renewal rewrites the account's own file only.
- The sign-in endpoints, client ids, scopes, and callback ports in `internal/quota/login.go` are the ones each vendor CLI is registered with. They are protocol constants, not settings.
- Window preferences (poll interval, notifications, folded providers) live in `settings.json` in the data directory, never in the webview's storage.
- Dates are typed as `YYYY-MM-DD` text. WebKitGTK draws an empty `type="date"` input as today.
- The ladder acts only through an account's change command: one executable, arguments split on whitespace, `{from}` and `{to}` replaced. No shell.
- The ladder never repeats a plan change on the reading that caused it: after a step it holds until a poll newer than `lastStep` arrives.
- One action runs at a time in the window, and a sign-in belongs to the dialog that started it: closing that dialog cancels it. A callback without the state the sign-in issued is refused and does not end it.
- A meter that was not in the payload is absent. It is never drawn as 0%.
- Adding an account asks for the provider only. The name defaults to the login's email. A good poll adopts the tier the provider states, and the activation and renewal dates where it states them: Codex in its id token, Grok as the billing period end. Claude and Antigravity state no dates, so those are typed in Edit. What the provider states replaces what was typed; a date it does not state is never invented.
- A stored file that cannot be read is contained. An unreadable `reading.json` shows as a failed reading, an unreadable `ladder.json` holds that account's ladder and is left untouched, and an account whose `account.json` cannot be loaded is named in `Overview.problems`. None of them empties the window.
- A failed poll keeps the last good meters and marks them stale. `ok` stays false, and the ladder never acts on a reading that is not ok.
- Every attempted plan change is appended to the account's `history.jsonl`, failed ones with their error.
- The window polls on open and on the operator's interval. It never runs the ladder unasked: Run ladder shows the due steps and waits for a confirmation.
- `frontend/public/agents/` holds vendor marks copied from `@lobehub/icons-static-svg`; `NOTICE.txt` there names the source and license.

## CLI

The CLI Spec v0.3 contract lives in `internal/cli`. `schema` is the document, and the error table in `schema.go` is the only place an exit code is declared. A new command needs an entry in `commands` (effects, cardinality, output fields) and a test that runs it. `--output` defaults to text on a TTY and JSON otherwise. Data on stdout, the error envelope on stderr.

## Gate

`go test ./...`, `bun test` and `bun run build` (type-check, then bundle) in `frontend/`, `vnu --format text` on `frontend/dist/index.html`.
