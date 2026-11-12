# Provenance

Append-only. A reversal is a new entry.

## 2026-11-11

Asked for a single-binary Go tool that manages Claude, Codex, Grok, and Antigravity subscriptions: quotas, renewal, activation, tier, reset credits, a ladder that drops to the cheapest paid tier after the top tier is activated and climbs as each tier is spent, an auto-reset toggle, a dashboard, a sandboxed test that does not touch the host agent directories, and self-update.

Evidence used:

- `quota-widget` `package/contents/code/fetch_quota.py` for the four endpoints, the credential files, and the payload fields (Claude `limits` / `five_hour` / `extra_usage`, Codex `rate_limit` windows and `rate_limit_reset_credits`, Grok `cli-chat-proxy` billing, Antigravity `agy --print /usage` `remaining_fraction`).
- Claude, Codex, and xAI docs, checked 2026-11-11: consumer plan changes happen in the billing page. No documented plan-change API. Codex reset redemption is `POST /wham/rate-limit-reset-credits/consume`, undocumented, so subman records the credit and does not redeem it.
- `modelfs` `src/update.zig` for the self-update shape: semver compare, asset name per target, SHA-256 against `SHA256SUMS`, GitHub URLs only.

Built: Wails window, per-account directory, parsers, ladder, `unshare` sandbox, update command.

Verified: `go test ./internal/...` (pass), `gofmt`, `go vet ./internal/...`, `bun run build`, `vnu --format text frontend/dist/index.html` (no errors), `go build` of the window and of `./cmd/subman`. `subman update --check` against `maci0/subman` returns "no release published" and exits 0.

Not verified here: a live poll against a signed-in account, and the sandbox entering a user namespace. This session's `unshare --user` and `bwrap --unshare-user` both return EPERM even though `kernel.unprivileged_userns_clone` is 1, so the sandbox test skips. The probe still reports that failure to the window instead of running unsandboxed.

Not built, and why: a provider billing client. Shipping a guessed checkout flow would change a paid plan on an undocumented endpoint. The change command is the extension point.

## 2026-11-11 CLI Spec

The command line was rewritten to The CLI Spec v0.3 (https://clispec.dev, schema `docs/schema/v0.3.json` in rvben/clispec, fetched this date). `subman schema` emits the document. `jsonschema` Draft 2020-12 validated `.scratch/schema.json` against that schema with no errors.

Commands: `accounts list` (read only, bounded), `accounts show` (read only, single), `accounts poll` (idempotent, bounded), `accounts test` (read only, single), `update` (idempotent, single), `schema`, `version`, `help` (opaque text/plain). `--output auto|text|json` is global. Piped stdout defaults to JSON. A bad invocation exits 2 with kind `usage`. A missing account exits 4 with kind `not_found`. `io` and `internal` share exit code 1, which the spec allows, and the kind separates them.

Verified: `go test ./internal/...`, `subman schema | jsonschema`, `subman --help` mentions schema, `subman version` prints JSON when stdout is not a terminal and text with `--output text`. The window still builds. `cmd/subman` remains the CLI without WebKit. The window binary now dispatches to the same CLI when the arguments are a command.

## 2026-11-11 release workflow

`.github/workflows/release.yml` runs on a tag `v*`. It tests, builds the six release assets on native runners (linux amd64 and arm64 with WebKitGTK 4.1, windows amd64 and arm64, darwin amd64 and arm64), writes `SHA256SUMS`, and publishes them with `gh release create --verify-tag`. `AssetName` now returns the darwin and windows names the workflow uploads, so `subman update` can install them.

The workflow has not run: this directory is not a git repository, and `gh` on this machine has an invalid token, so no release was created from here. The local binary installed to `~/.local/bin/subman` is a `webkit2_41` build of this tree.

## 2026-11-11 sandbox always on

The header read "sandbox off" while the page had no overview, and the Antigravity poll and the plan-change command started a process with no confinement. `sandbox.Run` is now the only way a child starts. It re-execs subman, applies Landlock, and execs the program. A failure to confine returns `RunError` and the program does not run. Probe, `agy --print /usage`, and the change command all go through it. The header says "sandbox on" when Landlock is available and names the reason when it is not. There is no off state.

## 2026-11-11 window bridge

The window showed "Open this page from the subman binary. The dev server has no Go bridge." Wails publishes a bound type at `window.go.<package>.<Type>`. The page read `window.go.main.App`. The type lives in package `app`, so the object is `window.go.app.App` (`internal/frontend/runtime/desktop/bindings.js` builds the path from the package name). The page also read it once, during the first effect, and the runtime installs the object after load. `whenBridge` waits up to 4 seconds and the lookup is `go.app.App`.

## 2026-11-11 Landlock

`unshare --user` from this session returns EPERM. `/proc/self/status` shows `CapEff` all zero, `NoNewPrivs: 1`, and one seccomp filter. `kernel.unprivileged_userns_clone` is 1, so the sysctl is not the cause. gauntlet (`internal/runner/sandbox_linux.go`) and microagent (`src/sandbox.zig`) do not call unshare. Both apply Landlock: a ruleset for the filesystem rights the ABI knows, a read-only grant on `/`, read-write grants on the writable roots, `PR_SET_NO_NEW_PRIVS`, then `landlock_restrict_self`.

subman's Test now does that. The child is a re-exec of the subman binary (`--subman-landlock`), which confines itself and then execs the agent. `HOME` is the account agent directory. `TestLandlockRefusesAWriteOutside` writes inside the grant and is refused outside it, and it passed on this machine. `golang.org/x/sys` v0.31 names the ruleset field `Access_fs`; gauntlet's copy names it `Handled_access_fs`.

## 2026-11-11 window redesign

Asked for: review the UI and make it a lot better, on Preact and shadcn, and a logo.

Changed: `frontend/` moved from React 19 to Preact 11 (`@preact/preset-vite` aliases `react` to `preact/compat`, `tsconfig.json` maps the types the same way, `bunfig.toml` sets `peer = false` so React is not installed). Button, badge, input, label, native-select, switch, dialog, sheet, alert-dialog, and alert came from `bunx shadcn@latest add`. Three of them were edited to pass Preact's element types: `Button` lost `asChild`, the alert-dialog buttons take `buttonVariants` as a class, and `Input` and `NativeSelect` no longer pull `type` and `size` out of the props. The font is Bricolage Grotesque, bundled from `@fontsource-variable/bricolage-grotesque`, so the window looks the same on a machine without Fira Sans. `build` now runs `tsc --noEmit` before `vite build`. `main.go` paints the window in the page background, rgb(4, 25, 30), before the page loads.

What the review found and the rewrite fixes:

- Saving settings after a date edit sent `YYYY-MM-DD` in `activatedAt` or `renewalAt`. `model.Account` holds `*time.Time`, which decodes RFC 3339 only. `fromDay` in `format.ts` now sends the instant.
- A window with a null `util` drew an empty track, the same picture as 0%. It is hatched now, in the list, the usage rows, and the ladder.
- Remove deleted the account directory on one click. It asks first and names the directory.
- The detail pane said "Nothing to watch yet" while the first overview was still loading.
- The hand-built dialog had no focus trap and did not close on Escape. Add account and Settings are Radix dialogs.
- An error raised behind an open dialog was hidden by it. The same line renders inside the dialog, and it can be dismissed.
- Meter colours used fixed 70 and 90 cut-offs although the ladder acts at the account's own threshold. The colour and a tick on each bar follow `usageThreshold`.
- Reset and poll times were absolute. They read "in 1h 24m" and "6m ago", with the absolute time in the tooltip, computed at render.
- The tier ladder is a staircase, one step per rank. The current step fills with the highest meter, the value `Reading.PrimaryUtil` gives the ladder, and the next step is dashed with an arrow.
- `frontend/public/logo.svg`: three rising steps, the last one an outline. It is the favicon and sits beside the name in the header.

Verified: `bun test` (3 pass, `src/format.test.ts`), `bun run build`, `vnu --format text frontend/dist/index.html` (no errors), `go test ./...` (pass), `go build -buildvcs=false -tags webkit2_41`. The page was driven in Chromium through Playwright against a stand-in for `window.go.app.App` (`.scratch/ui/mock.js`) at 1280x840 and 960x640: account list, a failed poll, an account with no reading, no accounts, Add account, Settings, Remove, Test. Editing name, renewal, and the ladder switch and saving handed `UpdateAccount` `renewalAt: "2026-11-30T16:00:00.000Z"` for 2026-12-01 in UTC+8. No console errors.

Not verified: the page inside the WebKitGTK window. The binary was built and not launched, so the redesign has not been seen in WebKit, and the date fix has not run against the Go bridge.

Not built, and why: a light theme (the window was dark only before, and nothing asked for one) and a Wails application icon (`build/appicon.png` does not exist in this tree; the SVG is the source when one is wanted).

## 2026-11-11 light theme

Asked for after the redesign, which had left it out. `frontend/src/index.css` holds light tokens on `:root` and the dark ones under `@media (prefers-color-scheme: dark)`. The shadcn `dark:` variants read the same media query, and `<html>` no longer carries `class="dark"`. The window follows the system setting; there is no in-app toggle.

Verified: `bun run build`, `vnu --format text frontend/dist/index.html` (no errors), and Chromium through Playwright with `emulateMedia` in both schemes at 1280x840: main view, Settings, and the Remove confirmation. No page errors.

Not verified: that WebKitGTK reports the desktop's scheme to the page. `main.go` still paints rgb(4, 25, 30) before the page loads, so a light desktop sees a dark frame first.

## 2026-11-11 board, missing features, journey review

Asked for, in order: more compact views that show every account of a provider at once; editing from there; a review for what is missing, with the features added; each agent's logo next to the name; a more intuitive UI reviewed against user journeys.

Board. The sidebar and the one-account-at-a-time layout are gone. The window opens on one table per provider. A row carries the name, a small tier staircase, one meter per window label that provider reports, the ladder phase or "<tier> is due", the renewal day, and Poll and Edit buttons. A row click opens the account. The account view has "All accounts" and a switcher.

Journeys walked and what each one changed:

- First account. The credential field was an empty text box. `ProviderInfo.credentialPath` now carries the provider CLI's default file (`Provider.DefaultCredential`, the same four paths as the README table), the field starts there and follows the provider until it is edited, and Browse opens the native dialog (`App.PickCredential`). A new account is polled as it is added, so it does not open blank.
- Daily glance. Nothing polled until a button was pressed. The window polls when it opens and on an interval chosen in the footer: never, 5, 15 (default), or 60 minutes. A tick that lands during another action is skipped. Relative times redraw each minute. The interval is kept in the webview's localStorage.
- Running the ladder. One click ran every due plan-change command. Run ladder now opens a confirmation that lists each due step and its command, and the button shows the count of due steps. Polling never runs the ladder.
- Expired login. The failed-poll notice now says what to do and holds a Renew login button. Edit has "Replace credential" (`App.ReplaceCredential`, `store.Root.ReplaceCredential`): it copies a fresh vendor file over the account's copy through a rename and polls. Before this the only way was to remove the account and add it again.
- Understanding the ladder. Phase badges explain themselves on hover, an off or unarmed ladder links to Edit, and the last plan change and cycle count are shown when there are any.
- Labels. Test is "Test agent", the gear is "Edit" with a pencil, action buttons have tooltips, optional dates say so, and an available update names `subman update`.

Logos: `frontend/public/agents/{claude,codex,grok,antigravity}.svg`, copied unmodified from `@lobehub/icons-static-svg` 1.102.0 (MIT per its package.json), drawn as CSS masks in the text colour. `NOTICE.txt` beside them records the source and that the marks belong to their vendors.

`store.Import` now shares `readCredential` with the replace path; its checks are unchanged.

Verified: `go vet ./...`, `go test ./...` (pass, with the new `TestReplaceCredential`), `bun test` (3 pass), `bun run build`, `vnu --format text frontend/dist/index.html` (no errors), `go build -buildvcs=false -tags webkit2_41`. In Chromium through Playwright against `.scratch/ui/mock.js`: the board with three Claude accounts; one `Poll` call on open; Run ladder made no `RunLadder` call before the confirmation; Add account started at the Claude path, followed a switch to Codex, took the picked path, and called `Import` then `PollOne`; the switcher; Edit opened from a row; Replace credential called `ReplaceCredential("g1", "/home/you/.grok/auth.json")` and closed the sheet; the footer select wrote `subman.autoPollMinutes`. No console errors.

Not verified: anything inside the WebKitGTK window. That covers the native file dialog, whether localStorage survives a restart there, and CSS masks for the logos.

Found and not built: the CLI cannot add, edit, or remove an account or run the ladder; a plan change leaves no history beyond the last one; `Reading.Stale` and `Reading.Account` are in the model and nothing sets them, so "stale" never shows and the signed-in identity is not displayed; `Poll` emits a `polled` event per account that the page does not listen to, so Poll all shows no per-row progress; there are no desktop notifications when a meter crosses the threshold; closing Edit with unsaved changes discards them without asking.

## 2026-11-11 the gaps from the journey review, and the first real-window run

Asked for: build everything the previous entry listed as found and not built, and fix all problems; then fold and unfold each provider on the board.

Built:

- CLI. `accounts add`, `accounts set`, `accounts remove`, and `ladder run [--dry-run]`, each with a `commands` entry. They call `internal/app`, the same code the window calls. `subman schema` still validates against CLI Spec v0.3 (`uv run .scratch/check_schema.py`: "schema valid against clispec 0.3"), and `CGO_ENABLED=0 go build ./cmd/subman` still builds.
- Validation. `App.UpdateAccount` refuses a name outside 1 to 64 characters, a tier the provider does not have, and a threshold outside 0 to 100, with `ErrInvalid`. The window could send any of these before; the CLI maps it to a usage error.
- Stale readings. A failed poll replaced `reading.json` and blanked the account's meters. `store.PutReading` now carries the last meters, credits, and plan into a failed reading and sets `stale`. `ok` stays false, so `ladder.Decide` still holds. This is the first code that sets `Reading.Stale`.
- History. `RunLadder` appends each attempted plan change to `history.jsonl`. `AccountView.history` carries the last 50, newest first, and the account view lists them.
- Notifications. `internal/notify` calls `org.freedesktop.Notifications.Notify` through `godbus/dbus/v5`, which was already in the module graph for Wails and is now a direct requirement. The window sends one when a poll moves an account's fullest meter from under its threshold to at or over it. Linux only.
- Poll progress. The page subscribes to the `polled` event `Poll` already emitted; each row stops spinning as its account finishes.
- Unsaved edits. Closing Edit with changes asks before discarding them.
- Folding. Each provider section on the board is a `<details>` element. A folded one still shows its account count, its fullest meter, and how many polls failed. The folded set is kept in localStorage under `subman.foldedProviders`.

Not built: `Reading.Account`. No usage payload parsed in `internal/quota/parse.go` carries the signed-in identity, so there is nothing true to show.

First run in the real window. `go build -tags production,webkit2_41`, launched with `GDK_BACKEND=x11 --data .scratch/ui/data` on three accounts made through the new CLI with an empty-object credential, one of them given a `reading.json` by hand. Captured with `import -window subman` (`.scratch/ui/real-1.png`). Seen: the light theme, so WebKitGTK does pass the desktop's scheme through; the bundled font; the four vendor marks drawn as masks; the board; the poll on open, which failed with `no-token` for the fake credentials and left the hand-written meters in place marked "poll failed, last good numbers". `notify.Send` was called once by hand and returned nil.

Verified otherwise: `go vet ./...`, `go test ./...` (new: `TestCrossed`, `TestRunLadderStepsAndRecords`, `TestFailedPollKeepsLastMetersAsStale`, `TestHistoryNewestFirst`, `TestAccountsAddSetRemove`), `bun test`, `bun run build`, `vnu --format text frontend/dist/index.html` (no errors). In Chromium against the mock: five rows spinning mid "Poll all" and none after; the plan-change list; Escape on an untouched Edit closes it, on an edited one asks, "Keep editing" keeps the text, "Discard changes" closes; folding two providers hid their rows, survived a reload, and Enter on the summary unfolded one.

Not verified: clicks inside the real window (no input automation on this machine), so the native file dialog and localStorage across a restart are still unseen there. A successful poll against a real provider was not run from this session.

## 2026-11-11 outstanding issues, and sign-in without a credential file

Asked for: fix every outstanding issue from the previous entry; then stop requiring a credential file and sign in the way EasyCLI and CLIProxyAPI do.

Sign-in. `internal/quota/login.go` runs each provider's OAuth flow itself. The parameters were read from `router-for-me/CLIProxyAPI` at `main` on this date (`internal/auth/claude/anthropic_auth.go`, `internal/auth/codex/openai_auth.go`, `internal/auth/codex/jwt_parser.go`, `internal/auth/xai/types.go` and `xai.go`), fetched from raw.githubusercontent.com:

- Claude: authorize at `https://claude.ai/oauth/authorize` with `code=true`, PKCE S256, callback `http://localhost:54545/callback`; the code is exchanged as JSON at `https://platform.claude.com/v1/oauth/token`. The stored file is the flat shape `renewClaude` and `fetchClaude` already read.
- Codex: authorize at `https://auth.openai.com/oauth/authorize`, callback `http://localhost:1455/auth/callback`, form exchange at `https://auth.openai.com/oauth/token`. The stored file is the Codex CLI's `auth.json` shape, with `tokens.account_id` from the id token's `chatgpt_account_id` claim.
- Grok: device code through discovery at `https://auth.x.ai/.well-known/openid-configuration`, client `b1a00492-073a-47ea-816f-4c329264a828`. The stored file is the Grok CLI's one-entry-per-issuer shape.
- Antigravity: none. subman reads its usage by running `agy`, and CLIProxyAPI's Google flow needs a client secret this tree does not carry. It keeps the credential file.

The callback listens on 127.0.0.1 only, checks the state it issued, takes the first answer, and escapes what it prints. `App.BeginLogin`, `CompleteLogin`, `CompleteRelogin`, and `CancelLogin` drive it from the window; Add account signs in by default with "Use a credential file instead" beside it, and Edit has "Sign in again". `subman accounts add` without `--credential` prints the page on stderr and waits.

Outstanding issues closed:

- Preferences moved from the webview's localStorage to `settings.json` (`model.Settings`, `store.Settings`, `App.SaveSettings`): poll interval, folded providers, and a new "Notify at threshold" switch.
- Identity. `quota.Identity` reads the signed-in email from the credential copy (an `email` field, an id token's claim, or a Grok entry), and `Fetch` puts it in `Reading.Account`. The board shows it under the name. A Claude Code `claudeAiOauth` file names nobody, so those rows show the poll time instead.
- The startup frame follows the desktop: `desktop.PrefersDark` reads `org.freedesktop.appearance color-scheme` from the settings portal, and `main.go` paints rgb(241, 248, 249) or rgb(4, 25, 30). `internal/notify` became `internal/desktop`.
- The light theme's warning fill was brown because one token served bars and text. Fills use `--warn-fill`.
- `AGENTS.md` said subman never refreshes a token while `quota.Renew` does. The contract now says renewal rewrites the account's own file only.
- The window reads `SUBMAN_DATA` as the default for `--data`, like the CLI.

Found by running the real window, and fixed:

- An empty `type="date"` input shows today's date in WebKitGTK (Chromium shows a placeholder), so an account with no renewal looked like it renewed today. Dates are `YYYY-MM-DD` text inputs now. `fromDay` also refuses days that do not exist, such as 2026-02-31, instead of rolling them forward.
- Provider sections followed account order. They follow `model.Providers`.
- A file dialog that failed to open was only logged to the console. The field says so.

Verified:

- `go vet ./...`, `go test ./...` (new: `TestClaudeLoginExchangesCodeWithPKCE`, `TestCodexLoginStoresAccountID`, `TestLoginRefusesForeignStateAndProviderError`, `TestLoginReportsABusyPort`, `TestDeviceLoginPollsUntilApproved`, `TestStartLoginRefusesAntigravity`, `TestIdentity`, `TestSettingsDefaultRoundTripAndRefusal`), `bun test`, `bun run build`, `vnu` (no errors), schema valid against CLI Spec v0.3, both binaries build.
- Real window (`-tags production,webkit2_41`, `GDK_BACKEND=x11`, driven with `xdotool`, captured with `import -window`): Add account opened; Browse opened the native dialog in `~/.claude` with hidden files shown; folding Claude and switching notifications off wrote `{"autoPollMinutes": 15, "notify": false, "foldedProviders": ["claude"]}` to `settings.json`, and a restart came back folded with the switch off; date fields show `YYYY-MM-DD`.
- Real sign-in starts: `subman accounts add --provider claude` and `--provider codex` listened on 54545 and 1455 and printed authorize addresses; a callback with a foreign state ended the Claude one with exit 8; `--provider grok` got a real device code from auth.x.ai.
- Real poll: `subman accounts poll` on this machine's five accounts returned `ok` for all of them, with an identity on three.

Not verified: a sign-in carried through to the end. That needs a person in the browser, and `curl` gets 403 from both authorize pages, so nothing here shows that Anthropic and OpenAI accept the redirect and the code exchange from this build. The Grok poll loop was only run against the test server. `BeginLogin` opening the browser from the real window was not triggered.

## 2026-11-11 adversarial review

Asked for: fix all remaining issues and eyesores, reviewed adversarially. Two reviewers that had not seen the work read the Go code and the frontend and reported defects; a third pass was mine, over screenshots of every state in both themes.

Go, found and fixed:

- `ladder.Decide` acted again on the reading that caused the last plan change, so two ladder runs with no poll between ran the paid change command twice. It now holds until `Reading.PolledAt` is after `LadderState.LastStep` (`TestHoldsUntilAPollAfterTheLastStep`, `TestRunLadderDoesNotRepeatOnTheSameReading`). `RunLadder` sets `LastStep` before the history append or the account save can fail, and a failed append is kept as the account's last error instead of dropping the ladder state.
- Any request to the callback port with a wrong state ended the sign-in. It is answered 400 and ignored (`TestLoginIgnoresForeignStateAndReportsProviderError`).
- `Login.Close` was a check-then-close that could close its channel twice and panic. It is a `sync.Once`. Close during the token exchange now cancels the exchange, so a cancelled sign-in returns no credential (`TestLoginCloseRacesWait`, under `-race`). The callback server shuts down with a drain instead of cutting the response.
- A bad name, tier, or date was only refused after the browser sign-in. `store.CheckRequest` runs first, in `App.BeginLogin` and in `accounts add` (`TestBeginLoginRefusesABadRequestBeforeTheBrowser`).
- `BeginLogin` took a provider. It takes the request, and `BeginRelogin` takes the account, so a Complete call cannot pick up a sign-in started for something else. `CompleteRelogin` refuses a sign-in whose identity differs from the stored one.
- `subman ladder run` on the window binary opened the window: `wantsCLI` did not list `ladder`.
- One torn line in `history.jsonl` emptied the whole window. `History` skips it.
- `CredentialFile` was a stored absolute path, so a moved data directory read the old place. `Load` derives it like `Home`.
- D-Bus calls had no deadline and ran under the app lock. They take 2 seconds at most.
- `accounts set` saved the other fields before it read `--credential`, applied flags in map order, and let `--threshold NaN` through to a JSON error. The file is read first, flags apply in a fixed order, and NaN is a usage error.
- `Identity` and `grokToken` depended on map order for files with several entries.
- A missing `expires_in` stored "expires now". No expiry is stored.
- A threshold notification repeated when the previous reading could not be read, and a settings error failed a poll whose reading was already stored. The notification is skipped and said on stderr.

Frontend, found and fixed:

- A poll or ladder result carried the settings it had read before a change made meanwhile, and put the old value back on screen and then on disk. Settings are their own state.
- A sign-in outlived its dialog: closing Edit left `busy` set for up to ten minutes, and another dialog showed the foreign sign-in. A sign-in has an owner, closing its dialog cancels it, and a dialog closed while the page was being prepared cancels what then starts. A click outside no longer closes a dialog that is waiting on the browser.
- `busy` was one string that overlapping runs cleared for each other, which re-enabled Add mid-import. `run` refuses a second action, a missed poll tick is taken when the first action ends, and an unasked poll leaves the error line alone.
- A failed settings save cleared its own error. Sheet errors sat at the top of a scrolled form; they sit above the footer.
- The threshold field could not be emptied while typing. Typing into an optional field and erasing it asked to discard changes.
- A successful "Sign in again" closed the sheet and dropped unsaved edits. Add reopened in file mode after it was used once.
- The credential label wrapped the input, the Browse button, and the error text. It names the input alone.
- Focus fell to the document on every view change. It goes to the account heading, and back to the account's row.
- Layout: the account view used half the window, so plan changes and the agent test sit in a second column from 1100px; the meta line no longer wraps under the action buttons; the footer select matches the rest; long sandbox notes, ladder badges, and many tiers truncate or shrink; the due list in the ladder confirmation scrolls. The light theme's warning text is darker for contrast.

Left as it is: a corrupt `reading.json` or `ladder.json` still fails the overview. Both are written through a rename, so a torn one needs more than a crash, and hiding it would hide the cause.

Verified: `go vet ./...`, `go test -race ./...`, `bun test`, `bun run build`, `vnu` (no errors), schema valid against CLI Spec v0.3, both binaries build. In Chromium against the mock: a fold made during "Poll all" stayed folded; focus landed on the heading and returned to `row-c1`; the threshold field emptied and saving it reported the range; type-then-erase closed without a prompt; an outside click during "Sign in again" kept the sheet, finishing kept it open, and closing it called `CancelLogin` and released `busy`; Add reopened in sign-in mode. The real window was built and shown the board and footer once more. No console errors.

Not verified: the account view in the real window after this change (the click sent to it did not open the account in that run), and still no sign-in carried through to the end.

## 2026-11-11 the last open items, and the real window driven without input

Asked for: fix what the previous entry left open (a corrupt stored file failing the overview, the account view unseen in the real window, no sign-in carried through).

Corrupt files. `App.stored` contains them: an unreadable `reading.json` is shown as a failed reading and the next poll replaces it; an unreadable `ladder.json` holds that account's ladder, offers no step, and is not overwritten, because when the last plan change happened is unknown; `store.Unreadable` names account directories whose `account.json` does not load, `Overview.problems` carries them, and the window lists them above the board. `TestUnreadableStateIsContained` covers all three and checks that the change command did not run.

Driving the real window. Pointer injection through `xdotool` stopped working when the window lost focus, and it is not sent blind. A build with `-tags production,webkit2_41,devtools` and `WEBKIT_INSPECTOR_HTTP_SERVER=127.0.0.1:9333` exposes WebKit's remote inspector; `.scratch/ui/drive.ts` evaluates JavaScript in the page and saves viewport snapshots over its socket. That reaches the real WebKitGTK page and the real Go bridge with no input events, including while the window is hidden. Snapshots taken while hidden show controls with CSS transitions in a stale state; the DOM was read for those.

Found that way, and fixed:

- Test agent always failed with "sandbox exec: no such file or directory". `MaybeReexec` passed a bare program name ("claude") to `execve`, which does not search `PATH`. Every default agent is a bare name, so this also broke the Antigravity poll (`agy`) and a change command given by name. The name is resolved with `exec.LookPath` after confinement (`TestRunResolvesABareNameOnPath`).
- A binary outside the source tree had no sandbox launcher: `ensureHelper` wanted `subman-sandbox` beside it or the source to build from at run time, and the release workflow ships neither. A subman binary now starts itself again (`cmd/subman` and `cmd/pollonce` call `MaybeReexec` too), and only `go test` builds the helper. A CLI copied to `~/.cache/subman-verify` with nothing beside it ran `accounts test` and printed `passed 2.1.322 (Claude Code)`.

Verified in the real window, through the inspector: the account opened from the board with focus on its heading and the two-column layout; Test agent reported "Agent test passed"; Edit changed the name, renewal to 2026-12-01, and threshold to 80, and `account.json` then held `"renewalAt":"2026-11-30T16:00:00Z"`, `"usageThreshold":80`, `"name":"day job"`, which is the date fix running against Go's `time.Time` for the first time; a Sign in to Grok with an unset name was refused by `BeginLogin` before anything opened; with a name it showed the waiting panel with a real device code and address, Cancel sign-in brought the form back with no error and no account, and the header buttons were enabled again.

Sign-in pages in a browser engine. The authorize addresses from two real `accounts add` starts were opened in Chromium, signed out. Claude answered 200 at `claude.ai/login` with "Continue with your Claude.ai account to authenticate connections". OpenAI answered 200 at `auth.openai.com/log-in` with "Your ChatGPT training controls apply to Codex". Neither reported an unknown client or a redirect mismatch.

Not verified: the step after the provider's login, for Claude and Codex (approval, the redirect to localhost, the code exchange) and for Grok (the poll after approval). Each needs the account holder to sign in; it was not done on their behalf.

## 2026-11-12 release workflow against the new gate

Nothing in the tree had changed since the last entry, and the gate was rerun clean. The release workflow ran `bun run build` and not `bun test`, which the gate names; `.github/workflows/release.yml` now runs it before the build. `bun install --frozen-lockfile` passes here against the committed `bun.lock`. `CGO_ENABLED=0 go build ./cmd/subman` and `go vet ./internal/...` are clean for `darwin/arm64`, `windows/amd64`, and `linux/arm64`, so the sign-in, desktop, and sandbox changes compile on every release target. The workflow itself has still never run: this directory is not a git repository.

## 2026-11-12 README against the code

The README still said a token is never refreshed (`quota.Renew` does, on the account's own file), described Test as a user and mount namespace (it is Landlock), and listed none of sign-in, the board, polling, notifications, history, or the added commands. It had no picture. The top of it was rewritten from the code as it is, with two screenshots in `docs/img/` taken in Chromium from the built frontend against `.scratch/ui/mock.js`, and it says they show sample data. "What does not work yet" now states how far sign-in has been run. Install, First run, Sandbox, and Development were already accurate and are unchanged.

## 2026-11-12 the Add dialog asks for the provider only

Asked for, with a screenshot of the Add dialog: it should detect the tier and the dates instead of asking.

What each provider states was checked first. The Codex id token on this machine carries `chatgpt_subscription_active_start` and `chatgpt_subscription_active_until` under `https://api.openai.com/auth`. Grok's billing payload carries `billingPeriodEnd`, which `parseGrok` already read for the credits meter. Claude's usage payload carries a plan and no date, and Antigravity's carries neither.

Changed:

- `quota.stateDates` writes those into `Reading.ActivatedAt` and `Reading.RenewalAt`. A period end that is not after now is dropped: it is a past period read from a login not refreshed since. `renewCodex` now keeps the reissued id token, so the period stays current.
- `App.StoreReading` (was `putReading`) adopts from a good reading the tier, when it is one of the provider's, and the stated dates, and saves the account. A failed poll adopts nothing. The CLI's poll and renew go through it too.
- `App.Add` is the one way an account is created, from the window's sign-in, its file import, and `accounts add`. `NewRequest` defaults the name to the login's identity, then the provider's name, and the tier to the provider's lowest until the first poll.
- The Add dialog has a provider, an optional name, and the sign-in button. Tier and date fields are gone from it. Edit keeps them and says a good poll replaces them where the provider states its own.
- `accounts add` no longer needs `--name`.

Verified: `go test -race ./...` (new: `TestStateDates`, `TestAddNamesFromTheLoginAndAdoptsWhatThePollStates`, and `accounts add` without `--name` in `TestAccountsAddSetRemove`), `bun test`, `bun run build`, `vnu` (no errors), schema valid. In Chromium against the mock, the dialog signed in with the name left empty. On this machine's Codex account, `accounts poll` then `accounts show` gave `tierId: pro`, `activatedAt: 2026-09-29T22:52:42Z`, `renewalAt: 2026-11-13T00:36:52Z`, none of them typed.

Not built: dates for Claude. Its OAuth profile endpoint may state when the subscription began; no Claude login was available here to read what it returns, and a field name from memory is not evidence.

## 2026-11-12 every kind of reset

Asked for: there are different types of resets; capture all of them for all providers.

What the payloads carry was read first. The live Codex usage payload on this machine has, beside `rate_limit`, the keys `code_review_rate_limit`, `additional_rate_limits`, `model_usage` (each model with `available` and `available_at`), and `rate_limit_reset_credits` with `available_count` and `applicable_available_count`. The first two were null on this account, so their shapes come from the Codex CLI's own models (`openai/codex`, `codex-rs/codex-backend-openapi-models`, read this date): `RateLimitStatusDetails` with `primary_window` and `secondary_window`, and `AdditionalRateLimitDetails` with `limit_name`, `metered_feature`, and `rate_limit`.

Changed in `internal/quota/parse.go`:

- Codex: `parseCodex` read `rate_limit` only. It now reads the code review limit and every additional limit as windows with their own reset times, labelled "<limit>, <window>", and a model that is unavailable with an `available_at` as a window with a reset time and no percent.
- Claude, older shape: the weekly blocks were a fixed list of four keys. Every `seven_day` and `seven_day_<what>` block is read. The newer `limits` shape was already read item by item.
- Grok and Antigravity already read every window their payloads carry; nothing changed.

The window shows a reset credit that does not apply to the limit in the way as "3, 1 usable now" (`Credits.resetApplies` was parsed and not shown), and a model's comeback time as "back in 2h".

Verified: `TestCodexCapturesEveryLimitAndResetCredit`, `TestClaudeReadsEveryWeeklyBlock`, `go test -race ./...`, `bun test`, `bun run build`, `vnu` (no errors), schema valid.

Not verified: a live payload with a non-null code review or additional limit, or an unavailable model. A live Claude or Grok payload was not available on this machine at the time; only the Codex account is signed in.

## 2026-11-12 first push

Asked to push to GitHub as a public repository. The directory was not a git repository until now. `git init -b main`, one commit, then `gh repo create maci0/subman --public --source . --push`: https://github.com/maci0/subman.

Before the commit, the staged files were searched for email addresses, token-shaped strings, account ids, and attribution lines. The only hits were the dummy tokens in `internal/quota/*_test.go`.

`.gitignore` had a bare `subman` line meant for the built binary, which also ignored the `cmd/subman` directory. It is `/subman` now, and `cmd/subman/main.go` is tracked.

No tag was pushed then; see "first release" below for the workflow run.

## 2026-11-12 local install

Asked to update the local install. `~/.local/bin/subman` was a build from 2026-11-11 16:51 with none of the later commands. It was copied to `~/.cache/subman/subman-before-2026-11-12`, and a `-trimpath -tags production,webkit2_41` build of this tree was moved into its place. From the installed binary: `subman version` prints 0.1.0, `accounts list` shows the Codex account with its detected renewal, and `accounts test` passed under Landlock with nothing beside the binary. The help line for `accounts add` still named `--name`, which is optional now; it names `--provider`.

## 2026-11-12 real sign-ins, and what each provider really states

Sign-in is verified end to end. The operator signed in from the window with two Claude accounts and two Grok accounts, and all four were stored and polled. That closes the open item from the earlier entries for Claude and Grok. Codex was not signed in again this way.

Correction to the entry "the Add dialog asks for the provider only": it listed `TestStateDates` as run. The file was never written, because the command that created it stopped at an earlier step. It exists now and passes.

Asked for, in this order: banked resets only on the highest tier; warnings about expiring resets; Claude shows "pro" for two subscriptions that are not Pro; Claude shows no renewal date; reset credit counts per account, not only in the header; the proposed next steps and their timing at the bottom.

What the live payloads state, read from this machine's accounts with identifying strings redacted:

- Claude `GET api.anthropic.com/api/oauth/usage` names no plan. `GET api.anthropic.com/api/oauth/profile` has `organization.plan_display_name` ("Ultra"), `rate_limit_tier` ("default_claude_max_20x"), `subscription_status`, and `subscription_created_at`. It has no renewal date. Neither payload has a reset credit.
- Codex `GET chatgpt.com/backend-api/wham/rate-limit-reset-credits` answers `{"available_count": 0, "credits": [], "total_earned_count": 0, ...}` for this account. The item fields (`id`, `reset_type`, `status`, `granted_at`, `expires_at`, `title`) are from the Codex CLI's `backend-client/src/types.rs`, read this date.
- Grok `/v1/billing?format=credits` has a weekly `currentPeriod` whose end is also `billingPeriodEnd`, and a `productUsage` list. Plain `/v1/billing` has a calendar-month `billingPeriodEnd`. Neither names a tier or a subscription renewal.

Fixed:

- "pro" was never stated by anyone. The account was created with the provider's lowest tier as a default and shown as fact. `NewRequest` no longer defaults the tier; an empty tier is "not reported". `claudeProfile` reads the real plan. After a poll both Claude accounts read Ultra.
- The Grok renewal adopted in the earlier entry was the end of the weekly usage period, not a renewal. `parseGrok` no longer sets one, and the two dates it had written to this machine's Grok accounts were cleared with `accounts set --renewal ""`, as were their defaulted tiers.
- Claude's renewal is estimated as the next monthly anniversary of `subscription_created_at` and carried as `renewalEstimated`. The window says "renews about". An estimate fills an empty date or replaces an earlier estimate and never a typed or stated one (`TestEstimatedRenewalNeverOverwritesARealOne`). An annual subscription would make it wrong, which is why it is marked.
- `ladder.ShouldReset` requires the highest tier and a reset that applies, and `Decide` holds at the top while one is usable instead of dropping (`TestResetOnlyAtTheHighestTier`).
- `codexResets` itemises banked resets. The board has a Resets column per account ("n/a" where the provider reports none), the account view lists each reset with its expiry, a reset inside `model.ResetExpiryWarning` (7 days) is marked, and a notification goes out once as it enters that window (`TestExpiringWarnsOnceAsACreditEntersTheWindow`).
- `ladder.Advise` produces the next steps, shown at the bottom of the board and in each account (`TestAdvise`).
- Grok's per-product usage is read as windows.

Verified: `go test -race ./...`, `bun test`, `bun run build`, `vnu` (no errors), schema valid. From the installed binary on the real accounts: Claude Ultra on both, estimated renewals 2026-12-05 and 2026-11-16, activations from the profile; Codex Pro with its stated dates and 0 resets; Grok with three and two windows, no tier, no renewal.

Not verified: an itemised reset with a real expiry (no account here has one), so the Resets column and the expiry warning were seen with sample data only. Reset credits are not redeemed by subman; the Codex consume endpoint is now known from the CLI source and is not called.

Open question for the operator: they report the reset counts as wrong. The APIs above return 0 for Codex and nothing for Claude and Grok. Where a different number is shown to them was not established.

## 2026-11-12 the open items

Asked to fix everything still open.

Reset counts. The operator had called them wrong. Codex was read again three ways for this account: `wham/usage`, the same with `x-openai-codex-luna-reserve: 1`, and `wham/rate-limit-reset-credits`. All say 0 available, 0 applicable, 0 ever earned. Claude's and Grok's payloads carry no reset credit. No other number was found, so nothing was changed; the window shows what the providers return.

Redemption, which the previous entries left out. With auto-reset on, a spent highest tier, and a usable reset, `ladder.Decide` now returns a step marked `Reset` (it needs no change command and is offered even to an unarmed ladder), and `RunLadder` spends it with `quota.RedeemReset`: `POST chatgpt.com/backend-api/wham/rate-limit-reset-credits/consume` with a `redeem_request_id` and, when the resets are itemised, the `credit_id` of the one that expires first. Path, body, and answer codes are the Codex CLI's (`backend-client/src/client/rate_limit_resets.rs`, `types.rs`). It goes through the same confirmation as a plan change, is recorded in the history, and is not repeated on the reading that caused it. The note that told the operator to redeem by hand is gone.

Verified: `TestRedeemReset`, `TestRunLadderUsesABankedResetAtTheTop`, the extended `TestResetOnlyAtTheHighestTier`, `go test -race ./...`, `bun test`, `bun run build`, `vnu` (no errors), schema valid. Against the live endpoint, for the Codex account with zero banked resets and guarded to run only then: the call answered 200 with `no_credit`, reported as "the provider has no banked reset for this account". A redeem that actually spends a reset has not been run; this machine has none to spend.

Also closed: `LICENSE` is MIT, the licence of the sibling tools this one draws on (`quota-widget`, `microagent`, `toktop`), with the same copyright line as `quota-widget`. The empty `web/` directories and their two `.gitignore` lines are gone.

Still open, and not fixable from here: Codex has not been signed in to through the window (its account came from a credential file); no reset with a real expiry has been seen.

## 2026-11-12 first release

`v0.1.0` was tagged and the release workflow ran for the first time. It took three runs; the tag was moved to the fixing commit each time, since no release had been published from it.

1. The test job ran `go test ./...`, which fails on a clean checkout: `main.go` embeds `frontend/dist`, and only the build job makes it. The test job now runs `go test ./internal/... ./cmd/...`. README and AGENTS now say to build the frontend before `go test ./...`.
2. Both macOS builds failed to link: `Undefined symbols: _OBJC_CLASS_$_UTType`. Wails' darwin dialog code uses UTType from UniformTypeIdentifiers, which the `wails` CLI links and a plain `go build` does not. The compile step sets `CGO_LDFLAGS=-framework UniformTypeIdentifiers` for darwin.
3. Run 43180317823 passed every job and published https://github.com/maci0/subman/releases/tag/v0.1.0 with the six binaries and `SHA256SUMS`.

Verified: `subman-linux-amd64` downloaded from the release matched `SHA256SUMS`, printed `0.1.0`, listed this machine's accounts, and `update --check` said "up to date". The macOS and Windows binaries were built by the workflow and not run here.

## 2026-11-12 Claude's banked resets

The operator said the reset counts were wrong: they hold one Claude reset to spend and Fable resets. They were right, and the previous entry's "Claude's payload carries no reset credit" was wrong. It carries none unless asked the way the Claude Code CLI asks.

How it was found: strings in the installed CLI (`~/.local/share/claude/versions/2.1.322`) name a reset programme, `cedar_ember`, and show the CLI reading `/api/oauth/usage?cedar_ember=1&skip_spend=1` and claiming with `POST /api/organizations/<org>/reset_rate_limits` and a body of `program`, `grant_id`, `request_id`. Asked with subman's old User-Agent, the status came back `eligible: false, ineligible_reason: "surface"`. Asked as the CLI names itself (`claude-cli/2.1.322 (external, cli)`, `x-app: cli`), it listed the grants.

What this machine's two Claude accounts hold, from that call:

- one has "One full limit reset (November 2026)", weekly, 1 left, ends 2026-12-10, clearing `seven_day` and `seven_day_overage_included`, and a "New model launch: one Fable reset" grant, 1 left, ends 2027-03-09, clearing `seven_day_overage_included`;
- the other has spent its weekly one and holds two Fable grants.

Changed:

- `quota.claudeResets` itemises the grants with uses left into `Credits.Resets`, with `ResetCount` the uses left and `ResetApplies` those that apply. A grant applies when it is usable now, not paused, and clears the limit that is fullest by the grant's own `percent_used`. `ResetCredit` gained `Left`, `Clears`, and `Applies`.
- `claudeUA` is the CLI's own string and `x-app: cli` goes with it, on the usage, profile, and claim calls.
- `quota.RedeemReset` claims a Claude grant by id. The organization comes from the login, or from the profile for a copied Claude Code file. The ladder's reset step names the first grant that applies, which is the soonest to expire of those.

Verified: `TestClaudeItemisesBankedResets`, `TestRedeemClaudeReset`, `go test -race ./...`, `bun test`, `bun run build`, `vnu` (no errors). From the installed binary, `accounts poll` lists the grants above with their labels and expiries, 2 resets with 1 usable on the first account. Against the live claim endpoint: the second account's already-spent weekly grant was claimed on purpose and the answer was `already_used`; a poll afterwards showed both of its Fable grants still at 1 left. A claim that spends a grant has not been run: that is the operator's to confirm in Run ladder.

## 2026-11-12 audit fixes in the window

The operator ran a technical audit of the window (contrast, target sizes, semantics, text scaling, motion, structure) and asked for every finding to be fixed. The audit scored 15/20 with 0 P0, 2 P1, 5 P2, 2 P3.

Changed:

- Stale readings: the usage list was dimmed with `opacity-60`, which put its text at 3.95:1 (dark) and 2.69:1 (light). The opacity now sits on the bar alone (`meter-stale`); the text keeps its colour. The meter's accessible name says "from an earlier poll".
- Light `--destructive` went from `oklch(0.56 0.2 25)` to `oklch(0.5 0.19 25)`; error text on the card measured 4.08:1 before.
- Text sizes are rem: body `0.9375rem`, and the 31 `text-[13px]` uses are one token, `text-fine` (`0.8125rem`).
- Information that lived only in a hover tooltip is on the page: the board row shows the poll error after "poll failed", and the account view says in a sentence that a renewal date is an estimate. A truncated account name and plan carry their full text as a title.
- Board tables: an `aria-label` per provider, `scope="col"` on every header, and the account cell is a row header.
- Targets: the dialog and sheet Close buttons are 24px, switches take clicks 6px above and below, and the row, next-step, and ladder links are 24px high.
- The poll spinner runs only under `motion-safe`; with reduced motion the icon turns the accent colour instead.
- `App.tsx` (1,444 lines) is five files: `App.tsx`, `board.tsx`, `detail.tsx`, `forms.tsx`, `shared.tsx`. No behaviour moved with them.
- The four constants the window mirrored from Go are gone. `AccountView.threshold`, `Overview.autoPollChoices`, and `Overview.resetExpiryWarningMs` carry them, and the provider order is the order `App.Providers` returns. `theme.test.ts` compares the frame colours in `main.go` with `--background`; they were already equal (`#f1f8f9`, `#04191e`).

Verified in Chromium against the built frontend and sample data, both themes, 960x640, on the board, a stale failed account, and the Edit sheet: no text under 4.5:1 (3:1 for large text), no control under 24px, no horizontal scroll, every table labelled with column and row headers, a root font size of 200% gives 30px body and 26px fine text, no animation running during Poll all with reduced motion, no console errors. `TestOverviewCarriesWhatTheWindowWouldOtherwiseMirror`, `go vet ./...`, `go test -race ./...`, `bun test`, `bun run build`, `oxlint` (no warnings), `vnu` (no errors).

Not verified: the real WebKitGTK window was not rebuilt or opened for this change, and no screen reader was run.

## 2026-11-12 0.1.1 and new screenshots

The operator asked to push the audit fixes, update the local install, release, and bring the README screenshots up to date.

- The version is 0.1.1 in `main.go`, `cmd/subman/main.go`, `internal/cli`, `internal/app`, and `wails.json`. `v0.1.1` was tagged at that commit and pushed, which starts the release workflow.
- `~/.local/bin/subman` is a `-trimpath -tags production,webkit2_41` build of that commit; the previous binary is at `~/.cache/subman/subman-before-0.1.1`. From the installed binary, `subman version` prints 0.1.1 and `accounts list` lists this machine's accounts.
- `docs/img/board.png` and `docs/img/account.png` were retaken in Chromium from the built frontend against `.scratch/ui/mock.js` (sample data, light theme, 1280 wide, tall enough to show the whole view). They now show per-account resets, the expiry warning, next steps, and the banked reset list, none of which existed when the old ones were taken. The README captions name them.

## 2026-11-12 re-audit fixes and graphics

A re-audit after 0.1.1 scored 17/20 and found one P1 and three P3s. The operator asked for all of them fixed, and for graphics.

Changed:

- Reflow at enlarged text (P1, WCAG 1.4.4). At a 150% root size the title was drawn over the account summary, "Add account" ran off the window, and board cells clipped to an ellipsis. Now the header and footer wrap and the title does not shrink. Each board table has `min-w-[56rem]` and scrolls inside a `relative overflow-x-auto` container; `relative` was needed because the absolutely placed "Actions" label otherwise widened the page. The board's `details` are `min-w-0`. The usage row keeps an 8rem floor for the bar (`minmax(8rem, 1fr)`): at 200% it had been 6px wide. A window shorter than 36em scrolls as one page instead of leaving the content a strip between header and footer.
- The poll-interval select was 7.5rem and clipped "every 15 min" at every size; it is 8.75rem.
- Next steps account names carry their full text as a title (P3).
- Board tooltips (P3): the reason a step is due and an expiring reset are already written out in Next steps on the same page. The poll error was the one left hover-only; it now wraps to two lines under "poll failed" instead of truncating.
- The six inline grid templates are named theme values in `index.css` (P3).
- Graphics: `docs/img/banner.png` heads the README, `docs/img/ladder-cycle.png` sits under Why, and `docs/img/social-preview.png` (1280x640) is for the repository's social preview. They are drawn by `docs/img/art.html` in the window's palette and font. The cycle follows `ladder.Decide`: from renewal it climbs Pro, Max, Ultra, each to the threshold; a banked reset refills Ultra once; then it drops to Pro and waits for the renewal.
- `board.png` and `account.png` were retaken for the select and the wrapped poll error.

Verified in Chromium against the built frontend and sample data at 960x640, root sizes 100%, 150%, and 200%, board and account view: no page-level horizontal scroll, header, footer, and main do not overflow, no overlapping header items, no truncated text without a title, usage bars 478px at 100% and 256px at 200%. At 960x500 `main` stops scrolling and the document scrolls. `bun test`, `bun run build`, `oxlint`, `vnu` on `frontend/dist/index.html` and `docs/img/art.html` (no errors, no warnings), `go test -race ./...`.

Not verified: the real WebKitGTK window and a screen reader. The social preview is not set on GitHub; that is a repository setting with no API.

## 2026-11-12 dialog and detail reflow

The operator asked to fix the modal overflow, detail overflow, and reduced motion findings from the Impeccable audit.

- `frontend/src/components/ui/dialog.tsx` and `alert-dialog.tsx` cap content at the viewport height and scroll it. `sheet.tsx` uses the full window width below `sm`; `forms.tsx` stacks fields there.
- `frontend/src/detail.tsx` contains usage-row scrolling within that section and lets the tier ladder wrap beside its explanation.
- All three modal components run entry and exit animations only when reduced motion is off.

Verified with the built frontend and `.scratch/ui/mock.js` in headless Chromium at 960x640 with 200% root text: no page or main horizontal overflow and the usage list scrolls inside its section. At 960x320, the Add and ladder confirmation dialogs fit and scroll. With reduced motion, the sheet and both dialogs report `animation-name: none`. `bun test`, `bun run build`, `go test ./...`, `vnu --format text frontend/dist/index.html`, and `git diff --check` passed.

Not verified: the WebKitGTK window and a screen reader.

## 2026-11-12 full test pass

The operator asked to test all changes from the dialog and detail reflow. Testing found that the desktop `sm:max-w-lg` rule let the Add and confirmation dialogs touch the window edges at 200% text. `frontend/src/components/ui/dialog.tsx` and `alert-dialog.tsx` now cap desktop width at the smaller of 32rem and the viewport minus 2rem.

Verified: `bun test` (4 pass), `bun run build`, `go test ./...`, `go test -race ./...`, `go vet ./...`, `oxlint frontend/src`, `vnu --format text frontend/dist/index.html`, and `git diff --check`. In Chromium with `.scratch/ui/mock.js`, light and dark board and account views had no page or main horizontal overflow at 960x640 with 100%, 150%, and 200% root text. At 200%, usage scrolled inside its section. At 960x320 with 200% text, Add and confirmation dialogs fit and scrolled; the Edit sheet fit. Their animations were `none` under reduced motion and `enter` otherwise.

A `production,webkit2_41,devtools` build ran against an isolated empty data directory. In the real WebKitGTK window at 960x640 and 200% text, the Add dialog's rectangle stayed 32px from each side, fit vertically, and scrolled. After scrolling 53px to the bottom, the submit button was fully visible. The configured window minimum is 960x640 (`main.go:67-68`); a 400px Chromium stress run showed page overflow outside that supported width.

Not verified: a screen reader, and WebKitGTK with populated account data.

## 2026-11-12 relumea visual system, main area height, dialog width

The operator asked to fix the two findings of the third Impeccable audit (18/20) and to use the relumea brand guide. Asked how far, they chose the visual system only (subman keeps its name and mark) and a copy of the brand files with no source record.

- `frontend/src/system/tokens.css` is an unchanged copy of `ai.relumea.brand` `src/system/tokens.css` at `c47cfe5`; `frontend/public/fonts/` holds that commit's `archivo.woff2`, `jetbrains-mono.woff2`, and their OFL licenses. `index.css` imports the tokens, declares the fonts as relumea-site `src/styles.css` does, and drops subman's own colour tokens and `@fontsource-variable/bricolage-grotesque`.
- Every stock Tailwind class the tokens clear was remapped (`.scratch/rebrand.ts`, not committed): `text-sm` to `text-data`, `text-xs` and `text-fine` to `text-micro`, `rounded-md` to `rounded-control`, `shadow-lg` to `shadow-lift`, hover `bg-accent` to `bg-surface-2`, `dark:` variants removed, focus rings to a 2px accent outline with a 2px offset (BRAND.md section 10). Container widths: `max-w-md` to `max-w-form`, the account view's `max-w-3xl` to `max-w-[48rem]`. A scan of every class in `frontend/src` against the built CSS finds none that generates nothing.
- Dialogs and sheets sit on `surface` with `shadow-lift` and no border (one elevation per element). Meter and step tones (`format.ts`) are `accent` below the warning margin, `st-warn` near the threshold, `st-fail` at it. `logo.svg` keeps its shape in brand ink, grey, and accent, inverted under a dark scheme. `main.go` paints the frame in `--color-bg` (`#fafafa`, `#0b0b0c`); `theme.test.ts` now reads `tokens.css`.
- Audit P2: at 200% root text in a 960x640 window, `main` was 191px tall, because the short-window rule used a `36em` media query, which does not follow the root text size. `main` now has `min-h-80` (20rem) and the frame scrolls when the header and footer leave less. Measured: 539px at 100%, 640px at 200%, no horizontal page scroll.
- Audit P3: the Add dialog scrolled sideways by 43px (17px after the font change) at 200% text, because its implicit grid column was sized before the vertical scrollbar took its width. `dialog.tsx` and `alert-dialog.tsx` set `grid-cols-[minmax(0,1fr)]`; tested live first, scroll width then equals client width (886px).
- `docs/img/art.html` redraws the banner, ladder cycle, and social preview on the brand's light ground in Archivo, with the window's step fills, no dot grid and no gradient (BRAND.md sections 8 and 11). All five images in `docs/img/` were re-rendered.

Verified in Chromium with `.scratch/ui/mock.js`, 960x640, light and dark: every text node clears AA contrast on the board, account view, Edit sheet, and Add dialog; no control is under 24px (the two 32x18 inputs are Radix's aria-hidden form inputs); every focus stop shows a 2px `rgb(15, 122, 66)` outline at a 2px offset; Archivo and JetBrains Mono load. `bun test`, `bun run build`, `oxlint`, `vnu --format text` on `frontend/dist/index.html` and `docs/img/art.html`, the Impeccable detector on `frontend/src` (no findings), `go vet ./...`, `go test -race ./...`, `git diff --check`.

Not verified: the WebKitGTK window and a screen reader. Lucide icons stay; relumea's icon set was not adopted.

## 2026-11-12 release workflow pins

`.github/workflows/release.yml` referenced every action by a moving major tag, and the workflow-wide `contents: write` token reached the test and build jobs, which only read the checkout. Each action is now pinned to the commit of its current release on the same major (`actions/checkout` v4.3.1, `actions/setup-go` v5.6.0, `oven-sh/setup-bun` v2.2.0, `actions/cache` v4.3.0, `actions/upload-artifact` v4.6.2, `actions/download-artifact` v4.3.0), with the tag in a trailing comment. Both checkouts set `persist-credentials: false`. The test and build jobs set `contents: read`; publish keeps the workflow default so `gh release create` can still write. `actions/setup-go` keys its module cache on `go.sum`, and the build job caches `~/.bun/install/cache` against `frontend/bun.lock`. `.github/dependabot.yml` watches the `github-actions` ecosystem weekly so the pins move by pull request.

Not verified: the workflow has not been run from this change.

## 2026-11-12 help on every command

`--help` and `-h` were help only at the root and on `accounts` and `update`. On the rest the parser read them as an account id or an unknown flag: `accounts show --help` exited 4 with `no account --help`, and `ladder --help` exited 2. `dispatch` now treats `--help`, `-h`, and `help` anywhere in the arguments as a request for the command list, before a subcommand runs or the data directory opens, and answers on stdout with exit 0.

The window binary routed only known commands to the CLI, so `subman --bogus` fell through to `flag.Parse`, which writes the standard flag package's own usage to stderr and exits 2 with no error envelope. A flag the window does not own now reaches the CLI, which reports kind `usage`. `--data` stays the window's flag. `cli.WantsCLI` is what both the window and the tests use.

Verified: `go test ./internal/cli/` (pass), `gofmt`, `go vet ./internal/cli/`, and `go build ./cmd/subman`. From that binary, `--help` and `-h` on `accounts show`, `accounts remove`, `accounts add`, `ladder`, `ladder run`, `schema`, `update`, and `version` exit 0 with the usage on stdout and nothing on stderr, and `--bogus` exits 2 with a `usage` envelope on stderr and nothing on stdout. `go test` of the window package was not run: it embeds `frontend/dist`, which is not built here.

## 2026-11-12 JetBrains Mono subset

`jetbrains-mono.woff2` was the variable face at 28,300 bytes, and every open of the window fetched it: the footer prints the data directory in it. The window only sets `font-normal` and `font-semibold` on that family, and only for paths, commands and codes, which are ASCII. It is now two static faces, `jetbrains-mono-400.woff2` (4,708 bytes) and `jetbrains-mono-600.woff2` (4,768 bytes), instanced at those weights and subset to U+0020-007E, covering the same text in 9,476 bytes. `archivo.woff2` is unchanged. The OFL license in `frontend/public/fonts/licenses/` still covers both.

## 2026-11-13 dependency updates

`.github/dependabot.yml` watched only `github-actions`, so a new release of a Go module or a frontend package never opened a pull request. It now also watches the `gomod` ecosystem at `/` and the `bun` ecosystem at `/frontend`, weekly, with the `build` commit prefix. The `github-actions` entry is unchanged.

## 2026-11-12 one release number

The release number was written out in five places: `main.go`, `cmd/subman/main.go`, `internal/cli`, `internal/app`, and `wails.json`, and the release workflow stamped only `main.version`. The CLI binary sets `cli.Version` from its own copy before anything reads it, and the window reads `app.Version` while drawing the overview, so a tagged build kept reporting 0.1.1 from both after that tag was no longer the tree. `internal/version.Release` is now the only declaration, and both binaries copy their stamped `main.release` into it before they serve a request. An unstamped build reports `dev`, which the update check already refuses to compare, so it cannot claim to be a release. `wails.json` names that same default; the workflow does not read it.

Verified: `go test ./internal/version/ ./internal/cli/ ./internal/app/ ./cmd/subman/`, `gofmt`, `go vet` on those packages, and `go build` of `./cmd/subman` with `-X main.release` set, which then prints that version. The window package was not built: it embeds `frontend/dist`. Not verified: the release workflow has not been run from this change.

## 2026-11-13 plan at renewal and price

The operator asked for the plan a subscription renews at and its cost, for every provider, from the stored logins. Each provider was probed with read-only GETs using the account's stored token:

- Codex: `chatgpt.com/backend-api/subscriptions?account_id=` states `renews_at`, `cancels_at`, `scheduled_plan_change` (`changes_at`, `plan_type`), `billing_period`, `billing_currency`, and discounts. `backend-api/checkout_pricing_config/configs/<CUR>` lists each plan type's monthly price, tax included (promax 690 SGD, prolite 138 SGD on the probed account).
- Grok: `grok.com/rest/subscriptions` accepts the CLI token and lists every subscription, with `billingPeriodEnd`, `cancelAtPeriodEnd`, `scheduledChange.targetPlan`, and `dominantPlan.surfaceNames.SURFACE_DISPLAY`. It states no amount; past records carry only a Stripe price id. A subscription on the legacy billing system names no `plan` code. The name `Plus` for `supergrok.lite` is from the operator's screenshot of grok.com's billing page for the same account and change date.
- Claude: `/api/oauth/profile` states the plan and start only. `/api/organizations/<org>/subscription_details` answers 403 `oauth_token_not_accepted`, and the paths found in the Claude Code 2.1.322 binary carry no renewal or price.
- Antigravity: not probed; no Antigravity account is stored here.

Changes: `model.Reading` gains `nextPlan`, `nextPlanAt`, `ends`, `price`, `nextPrice`; `model.Account` gains `price` with `model.Price.Valid`. `quota.codexSubscription` and `quota.grokSubscription` run beside the usage calls; a failure leaves the reading as the usage call made it. The id token's period end now fills only a renewal the subscription call did not state, and `stateDates` drops a plan change whose moment has passed. `UpdateAccount` refuses an invalid price; `accounts set --price` takes "AMOUNT CUR" in either order, empty to clear; `accounts show` prints `next`, `ends`, and `price`. The board row and account view show "renews 17 Nov as Plus" or "ends 17 Nov" and the monthly price; the Edit sheet has a Monthly price field.

Verified: `go test -race ./...`, `go vet ./...`, `bun test`, `bun run build`, `oxlint`, `vnu --format text` on `frontend/dist/index.html`, `git diff --check`. Live polls with the built CLI: the Codex account shows renewal 2026-11-13, next Pro Lite from 2026-11-13 at 138.00 SGD/month, price 690.00 SGD/month; one Grok account SuperGrok Heavy, renewal 2026-11-17, next Plus; the legacy-billing Grok account SuperGrok Heavy, renewal 2026-11-18. Rendered in Chromium with a mock carrying these fields, light and dark: the board, the account view, and a refused price in Edit (field marked invalid and focused).

Not verified: the WebKitGTK window; a Codex cancellation or discount and a Grok cancellation against a real account (covered by unit tests on the observed field names only).

## 2026-11-13 dependency updates landed, release 0.2.0

The operator asked for every branch merged into `main` and a new minor release. Dependabot had opened twelve branches. Merged one by one they conflict on `go.mod`, `go.sum`, and `bun.lock`, so the same versions were applied in one change:

- Go: `github.com/godbus/dbus/v5` 5.2.4, `github.com/wailsapp/wails/v2` 2.17.1, `golang.org/x/mod` 0.42.0, `golang.org/x/sys` 0.50.0, `golang.org/x/term` 0.47.0, then `go mod tidy`. The `go` line stays 1.27.3.
- `golang.org/x/sys` 0.50.0 renamed `LandlockRulesetAttr.Access_fs` to `Handled_access_fs` and gave the struct six fields. The kernel accepts the larger size because the added fields are zero (`copy_struct_from_user`); the comment at `internal/sandbox/landlock_linux.go` now says so.
- Frontend: `typescript` 7.0.2 and `vite` 8.3.7. TypeScript 7 removed `baseUrl` (every `paths` entry was already `./`-relative), no longer loads `@types/*` on its own, and checks side-effect imports, so `tsconfig.json` drops `baseUrl` and sets `"types": ["bun", "vite/client"]`.
- Actions in `release.yml`, pinned by commit as before: `actions/checkout` 7.0.1, `actions/setup-go` 7.0.0, `actions/cache` 6.1.0, `actions/upload-artifact` 7.0.2, `actions/download-artifact` 8.0.2. Download still takes `path` with `merge-multiple`.
- README's build line stamped `-X main.version`, a variable that does not exist since the release number moved to `main.release`; a build from it reported `dev`. It now stamps `main.release=0.2.0`.

The release is 0.2.0, not 0.1.3, because it adds the plan at renewal and the price (2026-11-13 entry above).

Verified: `go vet ./...`, `go test -race ./...` (the sandbox tests create a ruleset with the new struct), `go build -tags production,webkit2_41 .`, `bun run build` (`tsc --noEmit` on 7.0.2, then Vite 8), `bun test`, `oxlint`, `vnu --format text` on `frontend/dist/index.html`, `git diff --check`. With the built CLI, `accounts test` on the Codex account passed under Landlock.

Not verified before the tag: the release workflow on the new action majors, which runs only on a tag push.
