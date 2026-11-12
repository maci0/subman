import { useCallback, useEffect, useId, useMemo, useRef, useState } from "preact/hooks";
import { Plus, RefreshCw, ShieldAlert, ShieldCheck, TriangleAlert, X } from "lucide-preact";
import { api, onPolled, whenBridge } from "./api";
import type { AccountView, ImportRequest, LoginStart, Overview, ProbeResult, ProviderInfo, Settings } from "./types";
import { MINUTE_MS, expiringSoon, messageOf, stepLabel } from "./shared";
import { Board } from "./board";
import { Detail } from "./detail";
import { AddDialog, SettingsSheet } from "./forms";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { AlertDialog, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { NativeSelect, NativeSelectOption } from "@/components/ui/native-select";
import { Switch } from "@/components/ui/switch";

const emptyImport: ImportRequest = {
  name: "",
  provider: "claude",
  credentialPath: "",
  tierId: "",
  activatedAt: "",
  renewalAt: "",
};

/** The message App.awaitLogin returns when CancelLogin ended the sign-in. */
const SIGN_IN_CANCELLED = "sign-in cancelled";

/** Who a sign-in in progress belongs to: the Add dialog, or the account being edited. */
const ADD_OWNER = "add";

export function App() {
  const [overview, setOverview] = useState<Overview | null>(null);
  const [providers, setProviders] = useState<ProviderInfo[]>([]);
  // Preferences are their own state: an overview that a poll returns was read
  // before a change made during that poll, and must not put the old value back.
  const [settings, setSettings] = useState<Settings | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState("");
  const [adding, setAdding] = useState(false);
  const [draft, setDraft] = useState<ImportRequest>(emptyImport);
  const [probe, setProbe] = useState<ProbeResult | null>(null);
  /** The account shown alone; empty shows the board of all accounts. */
  const [selected, setSelected] = useState<string>("");
  /** The account the board gives focus back to when the account view closes. */
  const lastViewed = useRef("");
  /** The account whose settings sheet is open. */
  const [editing, setEditing] = useState<string>("");
  const [confirmLadder, setConfirmLadder] = useState(false);
  const pollId = useId();
  const notifyId = useId();
  /** The browser sign-in in progress and its owner (ADD_OWNER or an account id). */
  const [login, setLogin] = useState<{ start: LoginStart; owner: string } | null>(null);
  // Bumped by every sign-in and every cancel, so a sign-in that was closed
  // while its page was still being prepared knows it is no longer wanted.
  const loginToken = useRef(0);
  // Relative times ("in 1h 24m") are computed at render; this redraws them each minute.
  const [, setMinute] = useState(0);
  useEffect(() => {
    const timer = setInterval(() => setMinute((n) => n + 1), MINUTE_MS);
    return () => clearInterval(timer);
  }, []);

  const load = useCallback(async () => {
    const ready = await whenBridge();
    if (!ready) {
      setError("The window did not connect to the subman process.");
      return;
    }
    try {
      const [ov, ps] = await Promise.all([api.overview(), api.providers()]);
      setOverview(ov);
      setProviders(ps);
      setSettings((cur) => cur ?? ov.settings);
      setSelected((cur) => (ov.accounts.some((v) => v.account.id === cur) ? cur : ""));
    } catch (err) {
      setError(messageOf(err));
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  // One action at a time. busyRef is set in the same tick as the click, so a
  // second click, or a poll tick, cannot start beside it.
  const busyRef = useRef("");
  /**
   * Runs fn as the action named label. Resolves true when it finished, false
   * when it threw (the message goes to the error line) or another action was
   * running. keepError leaves a message already on screen in place.
   */
  const run = useCallback(async (label: string, fn: () => Promise<void>, keepError = false): Promise<boolean> => {
    if (busyRef.current) return false;
    busyRef.current = label;
    setBusy(label);
    if (!keepError) setError("");
    try {
      await fn();
      return true;
    } catch (err) {
      setError(messageOf(err));
      return false;
    } finally {
      busyRef.current = "";
      setBusy("");
    }
  }, []);

  const selectedView = useMemo(
    () => overview?.accounts.find((v) => v.account.id === selected) ?? null,
    [overview, selected],
  );
  const editingView = overview?.accounts.find((v) => v.account.id === editing) ?? null;
  const pollOne = (id: string) => run(`poll:${id}`, async () => { await api.pollOne(id); await load(); });
  /** Accounts the running "Poll all" has finished, so each row can stop spinning on its own. */
  const [polled, setPolled] = useState<ReadonlySet<string>>(new Set());
  useEffect(() => onPolled((id) => setPolled((done) => new Set(done).add(id))), []);
  // A poll the operator did not ask for leaves the error line alone.
  const pollAll = useCallback((unasked = false) => {
    setPolled(new Set());
    return run("poll", async () => setOverview(await api.poll()), unasked);
  }, [run]);

  const autoPoll = settings?.autoPollMinutes ?? 0;
  const folded = useMemo(() => new Set(settings?.foldedProviders ?? []), [settings]);
  const saveSettings = async (change: Partial<Settings>) => {
    if (!settings) return;
    const next = { ...settings, ...change };
    setSettings(next);
    try {
      await api.saveSettings(next);
    } catch (err) {
      // What is on disk is the truth again; the message goes up after the reload clears nothing.
      setSettings(null);
      await load();
      setError(messageOf(err));
    }
  };

  // Poll when the window opens and then on the chosen interval. A tick that
  // lands while another action runs is taken as soon as that action ends.
  const ready = overview !== null;
  const missedTick = useRef(false);
  useEffect(() => {
    if (!ready || autoPoll === 0) return;
    const tick = () => {
      if (busyRef.current) missedTick.current = true;
      else void pollAll(true);
    };
    tick();
    const timer = setInterval(tick, autoPoll * MINUTE_MS);
    return () => clearInterval(timer);
  }, [ready, autoPoll, pollAll]);
  useEffect(() => {
    if (busy || !missedTick.current) return;
    missedTick.current = false;
    if (autoPoll > 0) void pollAll(true);
  }, [busy, autoPoll, pollAll]);

  const cancelSignIn = () => {
    loginToken.current++;
    setLogin(null);
    api.cancelLogin().catch((err: unknown) => setError(messageOf(err)));
  };
  /**
   * Starts a sign-in with begin, then runs complete, which waits for the
   * browser and does the rest. A sign-in the operator cancelled is not an error.
   */
  const signIn = (owner: string, begin: () => Promise<LoginStart>, complete: () => Promise<void>) =>
    run(owner === ADD_OWNER ? "add" : "relogin", async () => {
      const token = ++loginToken.current;
      try {
        const start = await begin();
        if (loginToken.current !== token) {
          // Closed while the page was being prepared: end what just started.
          await api.cancelLogin();
          return;
        }
        setLogin({ start, owner });
        await complete();
      } catch (err) {
        if (!messageOf(err).includes(SIGN_IN_CANCELLED)) throw err;
      } finally {
        if (loginToken.current === token) setLogin(null);
      }
    });
  const added = async (view: AccountView) => {
    setAdding(false);
    setDraft(emptyImport);
    setSelected(view.account.id);
    await load();
  };
  const openAdd = () => {
    setError("");
    setDraft((d) => (d.credentialPath ? d : { ...d, credentialPath: providers.find((p) => p.id === d.provider)?.credentialPath ?? "" }));
    setAdding(true);
  };
  const openEdit = (id: string) => {
    setError("");
    setEditing(id);
  };
  const closeEdit = () => {
    if (login?.owner === editing) cancelSignIn();
    setEditing("");
  };
  const show = (id: string) => {
    if (id) lastViewed.current = id;
    setSelected(id);
  };
  const due = overview?.accounts.filter((v) => v.next) ?? [];
  const update = overview?.update;
  const sandbox = overview?.sandboxOk ?? false;
  const errorLine = error ? <ErrorLine message={error} onDismiss={() => setError("")} /> : null;

  // main keeps at least 20rem, which grows with the text size. When the
  // header and footer leave less, the whole window scrolls instead.
  return (
    <div className="flex h-full flex-col overflow-y-auto">
      <header className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2 px-5 py-3">
        <div className="flex min-w-0 items-center gap-3">
          <h1 className="flex shrink-0 items-center gap-2 text-intro font-bold tracking-logo"><img src="/logo.svg" alt="" className="size-6" />subman</h1>
          <p className="truncate text-data text-muted-foreground">{summarize(overview)}</p>
        </div>
        <div className="flex min-w-0 flex-wrap items-center justify-end gap-2">
          {overview ? (
            <span
              className={`mr-2 flex min-w-0 items-center gap-1.5 text-micro ${sandbox ? "text-muted-foreground" : "text-st-warn"}`}
              title={sandbox ? "Every agent and plan-change command runs confined by Landlock" : overview.sandboxNote}
            >
              {sandbox ? <ShieldCheck className="size-4 shrink-0 text-accent" /> : <ShieldAlert className="size-4 shrink-0" />}
              <span className="truncate">{sandbox ? "Sandbox on" : overview.sandboxNote || "Sandbox unavailable"}</span>
            </span>
          ) : null}
          <Button variant="outline" size="sm" title="Fetch usage for every account" disabled={!!busy || !overview} onClick={() => pollAll()}>
            <RefreshCw className={busy === "poll" ? "motion-safe:animate-spin motion-reduce:text-accent" : ""} />
            {busy === "poll" ? "Polling" : "Poll all"}
          </Button>
          <Button variant="outline" size="sm" title="Review the plan changes that are due, then run them" disabled={!!busy || !overview} onClick={() => setConfirmLadder(true)}>
            {busy === "ladder" ? "Running" : "Run ladder"}
            {due.length > 0 ? <Badge>{due.length}</Badge> : null}
          </Button>
          <Button size="sm" disabled={!overview} onClick={openAdd}>
            <Plus />
            Add account
          </Button>
        </div>
      </header>

      {errorLine ? <div className="px-5 pb-3">{errorLine}</div> : null}
      {overview && overview.problems.length > 0 ? (
        <div className="px-5 pb-3">
          <Alert className="border-st-warn/60">
            <TriangleAlert className="text-st-warn" />
            <AlertTitle>{overview.problems.length === 1 ? "One account could not be loaded" : `${overview.problems.length} accounts could not be loaded`}</AlertTitle>
            <AlertDescription>
              <span>Each has a directory under <span className="font-mono text-micro">{overview.dataDir}/accounts</span> that subman cannot read. Nothing was deleted.</span>
              <ul className="font-mono text-micro">{overview.problems.map((p) => <li key={p}>{p}</li>)}</ul>
            </AlertDescription>
          </Alert>
        </div>
      ) : null}

      <main className="min-h-80 flex-1 overflow-auto border-t px-6 py-5">
        {selectedView ? (
          <Detail
            key={selectedView.account.id}
            view={selectedView}
            warningMs={overview?.resetExpiryWarningMs ?? 0}
            accounts={overview?.accounts ?? []}
            advice={(overview?.advice ?? []).filter((a) => a.accountId === selectedView.account.id)}
            busy={busy}
            probe={probe}
            onBack={() => show("")}
            onShow={show}
            onEdit={() => openEdit(selectedView.account.id)}
            onPoll={() => pollOne(selectedView.account.id)}
            onRenew={() => run("renew", async () => { await api.renew(selectedView.account.id); await load(); })}
            onTest={() => run("test", async () => setProbe(await api.testAccount(selectedView.account.id)))}
          />
        ) : overview && overview.accounts.length > 0 ? (
          <Board
            accounts={overview.accounts}
            busy={busy}
            polled={polled}
            folded={folded}
            order={providers.map((p) => p.id)}
            warningMs={overview.resetExpiryWarningMs}
            focusId={lastViewed.current}
            advice={overview.advice}
            onFold={(foldedProviders) => saveSettings({ foldedProviders })}
            onSelect={show}
            onEdit={openEdit}
            onPoll={pollOne}
          />
        ) : overview ? (
          <Empty onAdd={openAdd} />
        ) : error ? null : (
          <p className="text-data text-muted-foreground" role="status" aria-live="polite">Reading accounts</p>
        )}
      </main>

      {editingView ? (
        <SettingsSheet
          // A tier the ladder changed underneath starts the form over, so a
          // save cannot write the old tier back.
          key={`${editingView.account.id}:${editingView.account.tierId}`}
          view={editingView}
          busy={busy}
          errorLine={errorLine}
          defaultCredential={providers.find((p) => p.id === editingView.account.provider)?.credentialPath ?? ""}
          login={login?.owner === editingView.account.id ? login.start : null}
          canSignIn={providers.find((p) => p.id === editingView.account.provider)?.login ?? false}
          onClose={closeEdit}
          onSave={async (acc) => {
            if (await run("save", async () => { await api.updateAccount(acc); await load(); })) setEditing("");
          }}
          onReplace={(path) => run("replace", async () => { await api.replaceCredential(editingView.account.id, path); await load(); })}
          onSignIn={() => signIn(
            editingView.account.id,
            () => api.beginRelogin(editingView.account.id),
            async () => { await api.completeRelogin(editingView.account.id); await load(); },
          )}
          onCancelSignIn={cancelSignIn}
          onDelete={async () => {
            if (await run("delete", async () => { await api.deleteAccount(editingView.account.id); await load(); })) setEditing("");
          }}
        />
      ) : null}

      <footer className="flex flex-wrap items-center justify-between gap-x-4 border-t px-5 py-1.5 text-micro text-muted-foreground">
        <span className="truncate font-mono text-micro">{overview?.dataDir || ""}</span>
        <span className="flex flex-wrap items-center justify-end gap-1">
          <span className="mr-3 flex min-h-6 items-center gap-1.5 whitespace-nowrap">
            <label htmlFor={pollId}>Poll automatically</label>
            <span className="w-[8.75rem]">
            <NativeSelect
              id={pollId}
              className="h-7 py-0 pr-8 pl-2 text-micro text-foreground"
              value={autoPoll}
              disabled={!settings}
              onChange={(e) => saveSettings({ autoPollMinutes: Number(e.currentTarget.value) })}
            >
              {(overview?.autoPollChoices ?? []).map((m) => <NativeSelectOption key={m} value={m}>{m === 0 ? "never" : m < 60 ? `every ${m} min` : "every hour"}</NativeSelectOption>)}
            </NativeSelect>
            </span>
          </span>
          <span className="mr-3 flex min-h-6 items-center gap-1.5" title="Show a desktop notification when an account's fullest meter reaches its spend threshold">
            <label htmlFor={notifyId}>Notify at threshold</label>
            <Switch id={notifyId} size="sm" checked={settings?.notify ?? false} disabled={!settings} onCheckedChange={(notify) => saveSettings({ notify })} />
          </span>
          {overview ? `v${overview.version}` : null}
          <Button
            variant="link"
            size="sm"
            className="text-muted-foreground"
            disabled={!!busy || !overview}
            onClick={() => run("update", async () => { await api.checkUpdate(); await load(); })}
          >
            {busy === "update" ? "Checking" : update?.available ? `${update.latest} is out: run "subman update"` : update?.note || "Check for updates"}
          </Button>
        </span>
      </footer>

      <AlertDialog open={confirmLadder} onOpenChange={(open) => { if (!open && busy === "ladder") return; setConfirmLadder(open); }}>
        <AlertDialogContent aria-busy={busy === "ladder" || undefined}>
          <AlertDialogHeader>
            <AlertDialogTitle>{busy === "ladder" ? "Running the ladder" : due.length > 0 ? `Run ${due.length} step${due.length === 1 ? "" : "s"}?` : "Nothing is due"}</AlertDialogTitle>
            <AlertDialogDescription asChild>
              {due.length > 0 ? (
                <div>
                  <p>A plan change runs that account's plan-change command. A reset spends one banked reset. Both happen now.</p>
                  <ul className="mt-3 flex max-h-60 flex-col gap-2 overflow-auto text-foreground">
                    {due.map((v) => (
                      <li key={v.account.id}>
                        <span className="font-semibold">{v.account.name}</span>: {v.next ? stepLabel(v.tiers, v.next) : ""}
                        {v.next?.command ? <code className="mt-0.5 block font-mono text-micro break-all text-muted-foreground">{v.next.command}</code> : null}
                      </li>
                    ))}
                  </ul>
                </div>
              ) : (
                <p>Running the ladder now records where each account stands and changes nothing. A plan change becomes due when an account with the ladder on and a plan-change command reaches its spend threshold. A reset becomes due on the highest tier, when that tier is spent and the account is set to use banked resets.</p>
              )}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={busy === "ladder"}>Cancel</AlertDialogCancel>
            {/* Action closes the dialog itself, before the run starts. A plain button keeps it open so the run is visible and cannot be confirmed twice. */}
            <Button disabled={busy === "ladder"} onClick={() => run("ladder", async () => { setOverview(await api.runLadder()); setConfirmLadder(false); })}>
              {busy === "ladder" ? "Running" : "Run ladder"}
            </Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      {adding ? (
        <AddDialog
          draft={draft}
          providers={providers}
          busy={busy}
          login={login?.owner === ADD_OWNER ? login.start : null}
          errorLine={errorLine}
          onChange={setDraft}
          onClose={() => {
            if (busy === "add") cancelSignIn();
            setAdding(false);
          }}
          onSignIn={() => signIn(ADD_OWNER, () => api.beginLogin(draft), async () => added(await api.completeLogin(draft)))}
          onCancelSignIn={cancelSignIn}
          onImport={() => run("add", async () => {
            const view = await api.importAccount(draft);
            await added(view);
          })}
        />
      ) : null}
    </div>
  );
}

function summarize(overview: Overview | null): string {
  if (!overview) return "";
  const accounts = overview.accounts;
  let resets = 0;
  let saw = false;
  for (const view of accounts) {
    const n = view.reading?.credits.resetCount;
    if (n !== undefined && n !== null) {
      saw = true;
      resets += n;
    }
  }
  const n = accounts.length;
  if (n === 0) return "no accounts";
  const resetBit = !saw ? "no reset credits reported" : resets === 0 ? "no reset credits" : `${resets} reset credit${resets === 1 ? "" : "s"}`;
  const soon = accounts.reduce((sum, view) => sum + expiringSoon(view.reading, overview.resetExpiryWarningMs).length, 0);
  return `${n} account${n === 1 ? "" : "s"}, ${resetBit}${soon > 0 ? `, ${soon} expiring soon` : ""}`;
}

function ErrorLine({ message, onDismiss }: { message: string; onDismiss: () => void }) {
  // An error that appears after an action has to be reachable: focus moves to
  // its dismiss control, which a screen reader then announces (WCAG 4.1.3).
  const dismiss = useRef<HTMLButtonElement>(null);
  useEffect(() => { dismiss.current?.focus(); }, []);
  return (
    <Alert variant="destructive" className="border-destructive/50 pr-10">
      <TriangleAlert />
      <AlertDescription className="break-words text-destructive">{message}</AlertDescription>
      <Button ref={dismiss} variant="ghost" size="icon-xs" className="absolute top-2.5 right-2.5" aria-label={`Dismiss: ${message}`} onClick={onDismiss}>
        <X />
      </Button>
    </Alert>
  );
}

function Empty({ onAdd }: { onAdd: () => void }) {
  return (
    <div className="mt-12 max-w-form">
      <h2 className="text-title font-bold tracking-title leading-title">Nothing to watch yet</h2>
      <p className="mt-3 leading-prose text-muted-foreground">
        Add a Claude, Codex, Grok, or Antigravity login. subman copies the credential into its own directory and reads the usage the vendor CLI already reads.
      </p>
      <Button className="mt-6" onClick={onAdd}>
        <Plus />
        Add account
      </Button>
    </div>
  );
}
