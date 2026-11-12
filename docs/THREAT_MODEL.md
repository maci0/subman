# Threat model

Living model of what subman can be attacked through, what that costs, and which control stands in the way. Point fixes belong elsewhere; this file only names the surface. Last reviewed against the tree on 2026-11-13. Owner and review cadence are not set here.

subman is a single-user desktop program (Wails window, plus a CLI) that runs as the operator. It is not a network service and has no accounts of its own. The valuable things it holds are OAuth tokens for paid Claude, Codex, Grok, and Antigravity subscriptions, and the ability to spend them: a poll, a token refresh, a banked-reset redemption, and a plan-change command the operator configured.

## Risk-ranked summary

| Rank | Boundary | Threat | What stands in the way | Gap |
| --- | --- | --- | --- | --- |
| 1 | Local process to the OAuth callback | Another local user or process completes a Claude or Codex sign-in and the token is stored as the operator's | Loopback bind, PKCE, and a state check that refuses a callback without the state this sign-in issued (`internal/quota/login.go`, `startCodeLogin`) | The callback port is the vendor's fixed port (54545, 1455), so the listener is predictable. State stops a blind hit; it does not stop a process that can read the authorization URL out of the browser or the process list |
| 2 | Operator to child process | A plan-change command or an agent binary runs with the operator's authority and writes outside the account directory | Landlock: `/` read-only, one writable directory, no user namespace, and no unconfined fallback (`internal/sandbox/landlock_linux.go`, `internal/sandbox/run.go`) | Linux only. On every other OS the child is refused, not confined (`internal/sandbox/landlock_other.go`). The writable path is the whole account directory, which holds `credential.json` |
| 3 | Operator to stored secrets | Anyone who can read the data directory can use the copied refresh tokens | Directory `0700`, files `0600` (`internal/store/store.go`) | No encryption at rest, no OS keychain, no rotation other than the provider's own refresh |
| 4 | This program to a provider | A hostile or compromised provider response is trusted for the tier, the renewal date, and the account name | Response size caps; OAuth error bodies are reduced to the `error` field (`internal/quota/login.go`, `postLogin`) | `jwtClaims` decodes an id token and does not verify it (`internal/quota/identity.go`). Codex dates and the sign-in email are taken from that payload (`internal/quota/dates.go`) |
| 5 | Release repo to this binary | A replaced release binary runs as the operator | Asset host allow-list, size caps, SHA-256 checked against `SHA256SUMS` before rename (`internal/update/update.go`) | The checksum file is fetched from the same host as the binary. Trust is GitHub plus the tag signature the release workflow asks `gh` to verify (`.github/workflows/release.yml`) |
| 6 | Local process to the Grok device code | A process that reads the verification URL finishes the sign-in first and the token is stored as the operator's | No listener. Discovery endpoints must start with `https://auth.x.ai/`, and the poll waits at least five seconds (`internal/quota/login.go`, `startDeviceLogin`) | The device code is the exchange secret and stays in the process until the sign-in ends. Only the user code is printed, to stderr (`internal/cli/manage.go`, `signIn`) |
| 7 | Webview to the bound API | Script running in the window calls `RunLadder` or `Delete` with no further check | The window confirms a ladder run before calling (`frontend/src/App.tsx`, `confirmLadder`) | Confirmation is the page. `RunLadder` is bound and spends a due reset or plan change without asking again (`main.go`, `internal/app/app.go`) |

## Entry points

Network listeners

- OAuth callback, loopback only, for the life of one sign-in. Claude binds `127.0.0.1:54545` at `/callback`, Codex binds `127.0.0.1:1455` at `/auth/callback`. Started from the window (`internal/app/app.go`, `BeginLogin`) and the CLI (`internal/cli/manage.go`, `accountsAdd`). Grok uses a device code and binds nothing (`internal/quota/login.go`, `startDeviceLogin`).
- No other listener. The window's assets are an embedded filesystem (`main.go`), not a reachable server. Wails binds `App` in-process (`main.go`, `Bind`): the page calls those methods directly and that binding opens no port.

Outbound HTTP, all as the operator

- Usage, profile, and reset reads (`internal/quota/fetch.go`, `internal/quota/detail.go`).
- Token refresh (`internal/quota/renew.go`) and banked-reset redemption (`internal/quota/redeem.go`).
- GitHub release metadata and assets (`internal/update/update.go`, `FetchLatest`, `Install`). The window only checks (`internal/app/app.go`, `CheckUpdate`); the CLI can replace the binary (`internal/cli/cli.go`, `dispatchUpdate`).
- `cmd/pollonce` is a separate binary and not part of the tool. It copies the file `SUBMAN_POLL_CRED` names into `SUBMAN_POLL_DATA` and polls it (`cmd/pollonce/main.go`). `SUBMAN_POLL_RAW=1` prints the raw provider answer. It holds the same tokens as the tool and is built from the same tree.

Child processes, all through `sandbox.Run`

- Antigravity poll: `agy --print /usage` (`internal/quota/fetch.go`, `fetchAntigravity`).
- Agent test: `<bin> --version` (`internal/sandbox/sandbox.go`, `Probe`).
- Plan change: the account's change command (`internal/app/change.go`, `runChange`).

CLI and environment

- Commands: `accounts`, `ladder`, `update`, `schema`, `version` (`internal/cli/cli.go`, `dispatch`). `ladder run` without `--dry-run` spends resets and runs change commands with no prompt (`internal/cli/manage.go`, `dispatchLadder`). The window asks first (`frontend/src/App.tsx`).
- `SUBMAN_DATA` selects the data directory (`internal/cli/cli.go`, `openRoot`; `main.go` flag `-data`). `SUBMAN_UPDATE_REPO` selects the release repo, checked by `validRepo`. `GITHUB_TOKEN` is sent as a bearer to `api.github.com` (`internal/update/update.go`). `XDG_DATA_HOME` must be absolute or it is ignored (`internal/store/store.go`, `DefaultRoot`); on Windows the fallback reads `APPDATA`, also only when absolute (`internal/store/root_windows.go`).
- `SUBMAN_POLL_DATA`, `SUBMAN_POLL_CRED`, and `SUBMAN_POLL_RAW` belong to `cmd/pollonce` only.
- `SUBMAN_REEXEC` and `SUBMAN_SANDBOX` are the sandbox re-exec channel (`internal/sandbox/sandbox.go`, `MaybeReexec`). A normal launch that happens to carry `SUBMAN_REEXEC=1` confines itself and execs `args[1]`.

Desktop IPC

- Session bus: one notification, and one read of the color scheme (`internal/desktop/desktop_linux.go`). Two-second timeout. No method is exported.

Files the operator points at

- `--credential` and the file dialog copy a vendor CLI login into the account directory. The source is read, never written (`internal/store/store.go`, `Import`). Account ids are checked before they become a path (`validID`).

## Trust boundaries

### Local process to the OAuth callback

The callback is the one place an input arrives from something that is not the operator and not a provider. `startCodeLogin` listens on `127.0.0.1`, so it is not reachable off the machine. The handler compares `state` to the value issued for this sign-in and returns 400 without ending the sign-in when it differs (`internal/quota/login.go`). PKCE means the code alone is not enough to redeem. The page written back is escaped.

What is not defended: the port is fixed and published, the listener stays up for the whole sign-in (ten minutes, `internal/app/app.go`, `loginTimeout`), and the first request that presents the right state wins. A process that can observe the authorize URL learns the state.

### Local process to the Grok device code

Grok's sign-in binds nothing. `startDeviceLogin` fetches discovery, refuses an endpoint that does not start with `https://auth.x.ai/`, and polls the token endpoint with the device code (`internal/quota/login.go`). The user code is printed to stderr (`internal/cli/manage.go`, `signIn`) and shown in the window, so the operator can check the page is approving this request.

What is not defended: the device code itself is the credential for the exchange. It lives in the process until the sign-in ends or expires (`expires_in` from the provider, else the ten-minute caller timeout). A local process that reads the URL or the process memory can complete the exchange before the operator does. The poll interval is floored at five seconds (`minDevicePoll`), so a provider that asks for a faster poll cannot turn this into a tight loop.

### Webview to the bound API

`main.go` binds the whole `App`. The page can call `RunLadder`, `Delete`, `UpdateAccount`, `Poll`, `BeginLogin`, and the rest with no token of its own. The window's confirmation dialog is the page (`frontend/src/App.tsx`, `confirmLadder`); `RunLadder` applies every due step, including a banked-reset redemption, and does not ask again (`internal/app/app.go`). A page the operator loads in this window, or a script injected into it, acts as the operator.

The binding is not a network endpoint. Reaching it means already running code inside the window.

### Operator to stored secrets

Credentials enter three ways: the OAuth exchange, a file the operator names, or a refresh answer. They live in `<data>/accounts/<id>/credential.json`, mode `0600`, under a `0700` directory. `Home` and `AgentHome` are derived at load, so a moved data directory does not keep the old path (`internal/store/store.go`, `Load`). Renewal rewrites that copy only. Nothing is written to `~/.claude`, `~/.codex`, `~/.grok`, or `~/.gemini`.

Leaving: tokens go to the provider on each poll, refresh, and redemption, in an `Authorization` header. They are not logged; a failed OAuth body is reduced to its `error` field. There is no encryption and no OS keychain. Deleting an account deletes the copy (`Delete`). A refresh rotates the provider's refresh token, so the copy and the vendor CLI's file diverge after the first renewal (`internal/quota/renew.go`).

### This program to a provider

Provider JSON is parsed and size-capped (`maxBody` in `internal/quota/fetch.go`, `maxLoginBody` in `internal/quota/login.go`). A good poll overwrites the stored tier and a stated renewal date (`internal/app/app.go`, `adopt`). An estimated renewal fills an empty date or replaces an earlier estimate only. An id token is decoded with no signature check (`internal/quota/identity.go`, `jwtClaims`), and Codex activation and renewal dates are read from its claims (`internal/quota/dates.go`). A tampered token file can therefore set those dates and the displayed email. The token still has to be accepted by the provider for a poll to succeed.

No provider call uses `http.DefaultClient`. Every request that carries a bearer token, and the sign-in exchange, refresh, and redemption, goes through `noRedirect`, whose `CheckRedirect` returns `ErrUseLastResponse` (`internal/quota/fetch.go`; used by `internal/quota/login.go`, `internal/quota/renew.go`, `internal/quota/redeem.go`). The update client refuses redirects the same way (`internal/update/update.go`). A `Location` is therefore never followed, and the token is not resent to a host the answer names.

Codex dates are the sharp case. `stateDates` reads the period start and end out of the unverified id token (`internal/quota/dates.go`), and `adopt` writes a stated renewal over a date the operator typed (`internal/app/app.go`). The ladder times its drop to the cheapest tier from that date, so a token file edited on disk can move when a plan change is due. It cannot mint a poll: `adopt` returns immediately when the reading is not ok, and ok requires the provider to accept the access token.

### Release repo to this binary

`FetchLatest` refuses a repo that is not `owner/name` of safe characters and drops any asset URL that does not start with `https://github.com/` or `https://release-assets.github.com/` (`internal/update/update.go`, `trustedURL`, `validRepo`). `Install` checks the asset against the SHA-256 line in `SHA256SUMS`, writes a temp file in the binary's directory, and renames it over the running executable. Caps: 1 MiB of release metadata, 64 KiB of checksums, 80 MiB of asset. The workflow publishes with `--verify-tag` (`.github/workflows/release.yml`). The checksum file travels with the asset, so a GitHub account that can publish a release can publish both.

### Operator to child process

`sandbox.Run` is the only start path. The helper re-execs, calls `confine`, then `execve`. `confine` grants read and execute on `/` and the full filesystem rights on `SUBMAN_SANDBOX`, then `PR_SET_NO_NEW_PRIVS` and `landlock_restrict_self`. Failure returns `RunError` and the program is not executed. The change command is split on whitespace with no shell; a relative path containing `/` is refused (`internal/app/change.go`). `Probe` rejects a binary name containing shell metacharacters or `..`.

Privilege: the child keeps the operator's uid. Landlock is the constraint, and it is Linux-only. The writable directory for a plan change is the account directory (`applyStep` passes `acc.Home`), so a change command can read and rewrite `credential.json`. The Antigravity poll and the agent test pass `acc.AgentHome` instead. The child's environment is replaced with `PATH`, `HOME`, `TERM`, and the sandbox markers, which also drops `GITHUB_TOKEN` from the child.

### Build to runtime

`main.go` embeds `frontend/dist`. The release workflow builds on a tag, runs `bun install --frozen-lockfile`, and stamps the version with `-ldflags`. `GOTOOLCHAIN=go1.27.3` keeps the compiler on the version the `go` line names. The Linux asset is built `-buildmode=pie` and linked externally with full RELRO, and its cgo code is compiled with a stack canary and `_FORTIFY_SOURCE=2`; the other assets take `-buildmode=pie`, which is already their default. No secret is baked in. The OAuth client ids in `internal/quota/login.go` and `internal/quota/renew.go` are the vendors' public CLI client ids.

## Assets

- Refresh tokens and access tokens for paid subscriptions. Theft lets the holder poll usage, refresh the login, and, for Claude and Codex, spend a banked reset (`internal/quota/redeem.go`).
- The change command. Whoever can edit `account.json` chooses an executable that later runs as the operator.
- The release binary. Replacement runs as the operator on the next launch.
- Availability of the subscription: a ladder run can change a paid tier or spend a reset credit. The window confirms; the CLI does not.
- Account email and plan dates, shown in the window and printed by the CLI.
- The operator's paid tier and banked reset credits. A ladder run spends both, and for Codex the date that times it is taken from an unverified id token.

## Mitigations present

- Callback state check and PKCE: `internal/quota/login.go`.
- Grok discovery endpoints must start with `https://auth.x.ai/`, and the device poll waits at least five seconds: `internal/quota/login.go`, `startDeviceLogin`.
- One sign-in at a time, cancelled when its dialog closes: `internal/app/app.go`, `CancelLogin`. A relogin as a different identity is refused and replaces nothing: `CompleteRelogin`.
- Credential file mode and id checks: `internal/store/store.go`.
- Landlock with no unconfined fallback: `internal/sandbox/run.go`, `internal/sandbox/landlock_linux.go`.
- Update URL allow-list and checksum before replace: `internal/update/update.go`.
- Ladder does not repeat a step on the reading that caused it: `internal/app/app.go`, `RunLadder`, `LastStep`.
- Every attempted plan change is appended to `history.jsonl`, failures included: `internal/store/store.go`, `AppendHistory`.
- Response and asset size caps: `internal/quota/fetch.go`, `internal/update/update.go`.
- No redirect is followed on a token-bearing request, the sign-in exchange, or the update download: `internal/quota/fetch.go` (`noRedirect`), `internal/update/update.go`.

## Not covered

Recorded here so a later pass can fix them. Not fixed in this document.

- No encryption of `credential.json`.
- id tokens decoded without verification (`internal/quota/identity.go`). For Codex the decoded claims set the renewal the ladder acts on (`internal/quota/dates.go`, `internal/app/app.go`, `adopt`).
- The window's ladder confirmation is the page. `RunLadder` is bound and applies due steps, including a reset redemption, with no check of its own (`main.go`, `internal/app/app.go`).
- Sandbox does not exist outside Linux, and on Linux the plan-change child may write the credential file. Landlock here is filesystem only: the child keeps the operator's network (`internal/sandbox/landlock_linux.go`).
- `ladder run` has no confirmation on the CLI.
- Callback port is fixed and the listener accepts the first matching state.
- Checksum and binary come from the same host.
- No audit trail beyond `history.jsonl` for plan changes. Sign-in, renewal, and update replacement are not recorded.
- No `SECURITY.md`. There is no published disclosure path, and none is invented here. A report has no documented route to a shipped fix.
