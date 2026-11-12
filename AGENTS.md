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
- `frontend/` Vite, Preact, Tailwind, shadcn components under `src/components/ui` (added with `bunx shadcn add`, then owned here). Radix and other React packages resolve to `preact/compat`; `bunfig.toml` keeps React itself out of `node_modules`. The visual system is relumea's brand (`~/Desktop/Projects/relumea/repos/ai.relumea.brand`, `docs/brand/BRAND.md`): `src/system/tokens.css` is an unchanged copy of its `tokens.css`, and `public/fonts/` holds its Archivo file and the JetBrains Mono weights the window uses, with their licenses. Never edit the copy; recopy it. The tokens clear Tailwind's stock colours, text sizes, radii, shadows, leading, easing, and container widths, so use the brand utilities (`text-data`, `text-micro`, `rounded-control`, `shadow-lift`, `bg-surface-2`, `text-st-warn`); a stock class such as `text-sm` generates nothing. Dialogs and sheets take `shadow-lift` and no border. Light and dark come from `light-dark()` in the tokens and follow `prefers-color-scheme`; there is no `dark:` variant and no in-app toggle. subman keeps its own name and mark: `public/logo.svg` is the mark and the favicon, drawn in brand ink and accent. `bun` only. `src/App.tsx` holds the window state and frame, `board.tsx` the provider tables, `detail.tsx` the account view, `forms.tsx` the Edit sheet and Add dialog, `shared.tsx` what more than one of them draws. Body text is `text-data`; secondary text is `text-micro`. Grid row layouts are named in `index.css` (`grid-cols-meters`, `-history`, `-resets`, `-steps`, `-steps-account`, `-account`). The window reflows at 200% text: the header and footer wrap, each board table and the detail usage list scroll inside their own containers, and the detail ladder wraps beside its explanation. Dialogs fit short windows and scroll their content; their animations run only without reduced motion. `main` keeps at least 20rem, so it grows with the text size; when the header and footer leave less, the whole window scrolls.
- `docs/PROVENANCE.md` append-only ledger.
- `docs/img/` README images. `board.png` and `account.png` are screenshots of the built frontend with sample data; `banner.png`, `ladder-cycle.png`, and `social-preview.png` are drawn by `docs/img/art.html`, which says how to render them.

## Contracts

- An account holds its own login: one subman signed in for (Claude, Codex, Grok) or a copy of a vendor CLI file (required for Antigravity). subman never writes `~/.claude`, `~/.codex`, `~/.grok`, or `~/.gemini`. Token renewal rewrites the account's own file only.
- The sign-in endpoints, client ids, scopes, and callback ports in `internal/quota/login.go` are the ones each vendor CLI is registered with. They are protocol constants, not settings.
- Window preferences (poll interval, notifications, folded providers) live in `settings.json` in the data directory, never in the webview's storage.
- Dates are typed as `YYYY-MM-DD` text. WebKitGTK draws an empty `type="date"` input as today.
- The ladder acts only through an account's change command: one executable, arguments split on whitespace, `{from}` and `{to}` replaced. No shell.
- The ladder never repeats a plan change on the reading that caused it: after a step it holds until a poll newer than `lastStep` arrives.
- One action runs at a time in the window, and a sign-in belongs to the dialog that started it: closing that dialog cancels it. A callback without the state the sign-in issued is refused and does not end it.
- A meter that was not in the payload is absent. It is never drawn as 0%.
- Adding an account asks for the provider only. The name defaults to the login's email. The tier is never guessed: it is empty, shown as "not reported", until a poll states it or the operator sets it. A good poll adopts the tier and the dates the provider states: Codex states both dates in its id token; Claude's profile states the plan and the subscription start, and the renewal is then estimated as the next monthly anniversary and marked as an estimate; Grok and Antigravity state none. A stated date replaces what was typed. An estimate only fills an empty date or replaces an earlier estimate.
- A banked reset is only for the highest tier. Below it the ladder steps up, which resets the window and keeps the credit. At the top with a usable reset and auto-reset on, the due step is the reset (`Step.reset`), which needs no change command; `RunLadder` spends it through `quota.RedeemReset` after the same confirmation as a plan change, and it is not repeated on the reading that caused it. Claude and Codex have the call. Claude lists its resets only for the CLI surface (`claudeUA`, `x-app`) and with the `cedar_ember` flag on the usage query; a reset applies when it can be used now and clears the limit that is fullest. A reset that expires within `model.ResetExpiryWarning` is flagged in the window and notified once.
- `Overview.advice` is the proposed next steps for every account, soonest first, from `ladder.Advise`. It reads the same facts the ladder does and runs nothing.
- A stored file that cannot be read is contained. An unreadable `reading.json` shows as a failed reading, an unreadable `ladder.json` holds that account's ladder and is left untouched, and an account whose `account.json` cannot be loaded is named in `Overview.problems`. None of them empties the window.
- A failed poll keeps the last good meters and marks them stale. `ok` stays false, and the ladder never acts on a reading that is not ok.
- Every attempted plan change is appended to the account's `history.jsonl`, failed ones with their error.
- The window polls on open and on the operator's interval. It never runs the ladder unasked: Run ladder shows the due steps and waits for a confirmation.
- The window keeps no copy of a Go constant. The provider order is the order of `App.Providers`, an account's spend threshold is `AccountView.threshold`, and the poll intervals and the reset expiry warning are `Overview.autoPollChoices` and `Overview.resetExpiryWarningMs`. `frontend/src/theme.test.ts` fails when the frame colours in `main.go` and `--color-bg` in `src/system/tokens.css` differ.
- A reading kept from before a failed poll fades its bars only. Text is never dimmed with opacity.
- `frontend/public/agents/` holds vendor marks copied from `@lobehub/icons-static-svg`; `NOTICE.txt` there names the source and license.

## CLI

The CLI Spec v0.3 contract lives in `internal/cli`. `schema` is the document, and the error table in `schema.go` is the only place an exit code is declared. A new command needs an entry in `commands` (effects, cardinality, output fields) and a test that runs it. `--output` defaults to text on a TTY and JSON otherwise. Data on stdout, the error envelope on stderr.

## Gate

`bun test` and `bun run build` (type-check, then bundle) in `frontend/` first, because `main.go` embeds `frontend/dist`; then `go test ./...`, `vnu --format text` on `frontend/dist/index.html`.
