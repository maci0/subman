import type { ComponentChildren } from "preact";
import { useCallback, useEffect, useId, useMemo, useRef, useState } from "preact/hooks";
import { ArrowDown, ArrowLeft, ArrowUp, ChevronRight, CircleCheck, CircleX, FolderOpen, Pencil, Plus, RefreshCw, ShieldAlert, ShieldCheck, TriangleAlert, X } from "lucide-preact";
import { api, onPolled, openExternal, whenBridge } from "./api";
import { day, fromDay, num, pct, relative, shortDay, tone, when } from "./format";
import type { Account, AccountView, Advice, HistoryEntry, ImportRequest, LoginStart, Overview, Phase, ProbeResult, Provider, ProviderInfo, Reading, ResetCredit, Settings, Step, Tier } from "./types";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import {
  AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription,
  AlertDialogFooter, AlertDialogHeader, AlertDialogTitle, AlertDialogTrigger,
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { NativeSelect, NativeSelectOption } from "@/components/ui/native-select";
import { Sheet, SheetContent, SheetDescription, SheetFooter, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Switch } from "@/components/ui/switch";

const emptyImport: ImportRequest = {
  name: "",
  provider: "claude",
  credentialPath: "",
  tierId: "",
  activatedAt: "",
  renewalAt: "",
};

/** Mirrors model.DefaultUsageThreshold: the percent used at which a ladder step fires. */
const DEFAULT_USAGE_THRESHOLD = 95;

/** Ladder drawing: height of the lowest step and the rise per rank, in px. */
const STEP_BASE_PX = 64;
const STEP_RISE_PX = 36;
/** The same staircase in a board row. */
const MINI_STEP_BASE_PX = 6;
const MINI_STEP_RISE_PX = 4;

const MINUTE_MS = 60_000;
/** The message App.awaitLogin returns when CancelLogin ended the sign-in. */
const SIGN_IN_CANCELLED = "sign-in cancelled";

/** Mirrors model.Providers: the order provider sections appear in. */
const PROVIDER_ORDER: readonly Provider[] = ["claude", "codex", "grok", "antigravity"];

/** Mirrors model.AutoPollChoices: the automatic poll intervals, in minutes. 0 is never. */
const AUTO_POLL_MINUTES = [0, 5, 15, 60];

/** What each ladder phase means, shown on hover. */
const PHASE_HELP: Record<Phase, string> = {
  off: "The ladder is switched off for this account.",
  unarmed: "The ladder is on but has no plan-change command, so it names the next step and does not take it.",
  climbing: "The ladder steps up one tier when the fullest meter reaches the spend threshold.",
  waiting: "Parked on the lowest tier until the renewal date.",
  acting: "A plan change is in progress.",
};

/** Who a sign-in in progress belongs to: the Add dialog, or the account being edited. */
const ADD_OWNER = "add";

function messageOf(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

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

  return (
    <div className="flex h-full flex-col">
      <header className="flex items-center justify-between gap-4 px-5 py-3">
        <div className="flex min-w-0 items-center gap-3">
          <h1 className="flex items-center gap-2 text-xl font-bold tracking-tight [font-stretch:80%]"><img src="/logo.svg" alt="" className="size-6" />subman</h1>
          <p className="truncate text-sm text-muted-foreground">{summarize(overview)}</p>
        </div>
        <div className="flex min-w-0 items-center gap-2">
          {overview ? (
            <span
              className={`mr-2 flex min-w-0 items-center gap-1.5 text-[13px] ${sandbox ? "text-muted-foreground" : "text-warn"}`}
              title={sandbox ? "Every agent and plan-change command runs confined by Landlock" : overview.sandboxNote}
            >
              {sandbox ? <ShieldCheck className="size-4 shrink-0 text-primary" /> : <ShieldAlert className="size-4 shrink-0" />}
              <span className="truncate">{sandbox ? "Sandbox on" : overview.sandboxNote || "Sandbox unavailable"}</span>
            </span>
          ) : null}
          <Button variant="outline" size="sm" title="Fetch usage for every account" disabled={!!busy || !overview} onClick={() => pollAll()}>
            <RefreshCw className={busy === "poll" ? "animate-spin" : ""} />
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
          <Alert className="border-warn/60">
            <TriangleAlert className="text-warn" />
            <AlertTitle>{overview.problems.length === 1 ? "One account could not be loaded" : `${overview.problems.length} accounts could not be loaded`}</AlertTitle>
            <AlertDescription>
              <span>Each has a directory under <span className="font-mono text-xs">{overview.dataDir}/accounts</span> that subman cannot read. Nothing was deleted.</span>
              <ul className="font-mono text-xs">{overview.problems.map((p) => <li key={p}>{p}</li>)}</ul>
            </AlertDescription>
          </Alert>
        </div>
      ) : null}

      <main className="min-h-0 flex-1 overflow-auto border-t px-6 py-5">
        {selectedView ? (
          <Detail
            key={selectedView.account.id}
            view={selectedView}
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
          <p className="text-sm text-muted-foreground" role="status">Reading accounts</p>
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

      <footer className="flex items-center justify-between gap-4 border-t px-5 py-1.5 text-[13px] text-muted-foreground">
        <span className="truncate font-mono text-xs">{overview?.dataDir || ""}</span>
        <span className="flex shrink-0 items-center gap-1">
          <label className="mr-3 flex items-center gap-1.5 whitespace-nowrap">
            Poll automatically
            <span className="w-[7.5rem]">
            <NativeSelect
              className="h-7 py-0 pr-8 pl-2 text-[13px] text-foreground"
              value={autoPoll}
              disabled={!settings}
              onChange={(e) => saveSettings({ autoPollMinutes: Number(e.currentTarget.value) })}
            >
              {AUTO_POLL_MINUTES.map((m) => <NativeSelectOption key={m} value={m}>{m === 0 ? "never" : m < 60 ? `every ${m} min` : "every hour"}</NativeSelectOption>)}
            </NativeSelect>
            </span>
          </label>
          <label className="mr-3 flex items-center gap-1.5" title="Show a desktop notification when an account's fullest meter reaches its spend threshold">
            Notify at threshold
            <Switch size="sm" checked={settings?.notify ?? false} disabled={!settings} onCheckedChange={(notify) => saveSettings({ notify })} />
          </label>
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

      <AlertDialog open={confirmLadder} onOpenChange={setConfirmLadder}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{due.length > 0 ? `Run ${due.length} step${due.length === 1 ? "" : "s"}?` : "Nothing is due"}</AlertDialogTitle>
            <AlertDialogDescription asChild>
              {due.length > 0 ? (
                <div>
                  <p>A plan change runs that account's plan-change command. A reset spends one banked reset. Both happen now.</p>
                  <ul className="mt-3 flex max-h-60 flex-col gap-2 overflow-auto text-foreground">
                    {due.map((v) => (
                      <li key={v.account.id}>
                        <span className="font-semibold">{v.account.name}</span>: {v.next ? stepLabel(v.tiers, v.next) : ""}
                        {v.next?.command ? <code className="mt-0.5 block font-mono text-xs break-all text-muted-foreground">{v.next.command}</code> : null}
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
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction onClick={() => run("ladder", async () => setOverview(await api.runLadder()))}>Run ladder</AlertDialogAction>
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
  const resetBit = saw ? `${resets} reset credit${resets === 1 ? "" : "s"}` : "no reset credits reported";
  const soon = accounts.reduce((sum, view) => sum + expiringSoon(view.reading).length, 0);
  return `${n} account${n === 1 ? "" : "s"}, ${resetBit}${soon > 0 ? `, ${soon} expiring soon` : ""}`;
}

function ErrorLine({ message, onDismiss }: { message: string; onDismiss: () => void }) {
  return (
    <Alert variant="destructive" className="border-destructive/50 pr-10">
      <TriangleAlert />
      <AlertDescription className="break-words text-destructive">{message}</AlertDescription>
      <Button variant="ghost" size="icon-xs" className="absolute top-2.5 right-2.5" aria-label="Dismiss" onClick={onDismiss}>
        <X />
      </Button>
    </Alert>
  );
}

function Empty({ onAdd }: { onAdd: () => void }) {
  return (
    <div className="mt-12 max-w-md">
      <h2 className="text-3xl font-bold tracking-tight [font-stretch:80%]">Nothing to watch yet</h2>
      <p className="mt-3 leading-relaxed text-muted-foreground">
        Add a Claude, Codex, Grok, or Antigravity login. subman copies the credential into its own directory and reads the usage the vendor CLI already reads.
      </p>
      <Button className="mt-6" onClick={onAdd}>
        <Plus />
        Add account
      </Button>
    </div>
  );
}

/** One bar. A null util is drawn hatched and never as an empty 0% track. */
function Meter({ label, util, threshold }: { label: string; util: number | null | undefined; threshold: number }) {
  if (util === null || util === undefined) {
    return <div className="meter absent" role="img" aria-label={`${label}: not reported`} />;
  }
  const width = Math.max(0, Math.min(100, util));
  return (
    <div className="meter" role="meter" aria-label={label} aria-valuemin={0} aria-valuemax={100} aria-valuenow={Math.round(width)}>
      <div className="meter-fill" style={{ width: `${width}%`, "--tone": tone(util, threshold) }} />
      <span className="meter-tick" style={{ left: `${threshold}%` }} />
    </div>
  );
}

/** Every account in one table per provider, so accounts of one provider line up meter by meter. */
function Board({
  accounts, advice, busy, polled, folded, focusId, onFold, onSelect, onEdit, onPoll,
}: {
  accounts: AccountView[];
  busy: string;
  /** Providers whose section is folded shut. */
  folded: ReadonlySet<Provider>;
  onFold: (folded: Provider[]) => void;
  /** The account whose row takes focus when the board appears: the one just viewed. */
  focusId: string;
  /** Every account's proposed next steps, soonest first. */
  advice: Advice[];
  /** Accounts a running "Poll all" has already finished. */
  polled: ReadonlySet<string>;
  onSelect: (id: string) => void;
  onEdit: (id: string) => void;
  onPoll: (id: string) => void;
}) {
  const groups = new Map<Provider, AccountView[]>();
  for (const view of accounts) {
    groups.set(view.account.provider, [...(groups.get(view.account.provider) ?? []), view]);
  }
  useEffect(() => {
    if (focusId) document.getElementById(rowId(focusId))?.focus();
  }, [focusId]);
  const fold = (provider: Provider, shut: boolean) => {
    if (folded.has(provider) === shut) return;
    onFold(shut ? [...folded, provider] : [...folded].filter((p) => p !== provider));
  };
  return (
    <div className="flex flex-col gap-6">
      {PROVIDER_ORDER.filter((p) => groups.has(p)).map((provider) => {
        const views = groups.get(provider) ?? [];
        const labels = [...new Set(views.flatMap((v) => v.reading?.windows.map((w) => w.label) ?? []))];
        const shut = folded.has(provider);
        // What a folded provider still says: its fullest meter, and whether a poll failed.
        const utils = views.map((v) => primary(v.reading)).filter((u) => u !== null);
        const fullest = utils.length > 0 ? Math.max(...utils) : null;
        const failures = views.filter((v) => v.reading !== undefined && !v.reading.ok).length;
        return (
          <details key={provider} open={!shut} onToggle={(e) => fold(provider, !e.currentTarget.open)}>
            <summary className="mb-1 flex cursor-pointer list-none items-center gap-2 rounded-sm outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50 [&::-webkit-details-marker]:hidden">
              <ChevronRight className={`size-4 text-muted-foreground transition-transform ${shut ? "" : "rotate-90"}`} />
              <AgentMark provider={provider} className="size-4" />
              <h2 className="text-lg font-bold [font-stretch:80%]">{title(provider)}</h2>
              <span className="text-[13px] text-muted-foreground">
                {views.length}
                {shut && fullest !== null ? `, fullest meter ${pct(fullest)}` : ""}
              </span>
              {shut && failures > 0 ? <span className="text-[13px] text-destructive">{failures} poll{failures === 1 ? "" : "s"} failed</span> : null}
            </summary>
            <table className="w-full table-fixed text-sm">
              <thead>
                <tr className="text-left text-[13px] text-muted-foreground [&>th]:py-1.5 [&>th]:pr-5 [&>th]:font-normal">
                  <th className="w-[18%] pl-2">Account</th>
                  <th className="w-[13%]">Tier</th>
                  {labels.map((label) => <th key={label} className="truncate" title={label}>{label}</th>)}
                  {labels.length === 0 ? <th>Usage</th> : null}
                  <th className="w-[12%]">Resets</th>
                  <th className="w-[13%]">Ladder</th>
                  <th className="w-[76px]"><span className="sr-only">Actions</span></th>
                </tr>
              </thead>
              <tbody>
                {views.map((view) => (
                  <BoardRow
                    key={view.account.id}
                    view={view}
                    labels={labels}
                    busy={busy}
                    polling={busy === `poll:${view.account.id}` || (busy === "poll" && !polled.has(view.account.id))}
                    onSelect={() => onSelect(view.account.id)}
                    onEdit={() => onEdit(view.account.id)}
                    onPoll={() => onPoll(view.account.id)}
                  />
                ))}
              </tbody>
            </table>
          </details>
        );
      })}
      <NextSteps advice={advice} onSelect={onSelect} />
    </div>
  );
}
/** The DOM id of an account's row button, so focus can come back to it. */
function rowId(accountId: string): string {
  return `row-${accountId}`;
}


function BoardRow({
  view, labels, busy, polling, onSelect, onEdit, onPoll,
}: {
  view: AccountView;
  labels: string[];
  busy: string;
  /** A poll of this account is in flight. */
  polling: boolean;
  onSelect: () => void;
  onEdit: () => void;
  onPoll: () => void;
}) {
  const reading = view.reading;
  const threshold = thresholdOf(view.account);
  const failed = reading !== undefined && !reading.ok;
  return (
    <tr className="cursor-pointer border-t hover:bg-secondary/50 [&>td]:py-2 [&>td]:pr-5 [&>td]:align-top" onClick={onSelect}>
      <td className="pl-2">
        {/* The click bubbles to the row; the button is there for the keyboard. */}
        <button id={rowId(view.account.id)} className="flex max-w-full items-center gap-1.5 rounded-sm font-semibold outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50">
          <AgentMark provider={view.account.provider} className="size-3.5" />
          <span className="truncate">{view.account.name}</span>
        </button>
        <span
          className={`block truncate text-[13px] ${failed ? "text-destructive" : reading?.stale ? "text-warn" : "text-muted-foreground"}`}
          title={failed ? reading.error : reading ? `polled ${when(reading.polledAt)}` : undefined}
        >
          {failed ? (reading.stale ? "poll failed, old numbers" : "poll failed") : reading ? reading.account || `polled ${relative(reading.polledAt)}` : "never polled"}
        </span>
      </td>
      <td>
        <span className="flex items-end gap-2">
          <MiniLadder view={view} />
          <span className="truncate">{planOf(view)}</span>
        </span>
      </td>
      {labels.map((label) => {
        const w = reading?.windows.find((x) => x.label === label);
        return (
          <td key={label}>
            <span className="block pt-1.5"><Meter label={label} util={w?.util} threshold={threshold} /></span>
            <span className="mt-1 flex justify-between gap-2 text-[13px]">
              <span className="font-semibold">{pct(w?.util)}</span>
              <span className="truncate text-muted-foreground" title={w?.resetsAt ? when(w.resetsAt) : undefined}>{relative(w?.resetsAt)}</span>
            </span>
          </td>
        );
      })}
      {labels.length === 0 ? <td className="text-muted-foreground">no meters reported</td> : null}
      <td><ResetsCell reading={reading} /></td>
      <td>
        {view.next ? (
          <Badge className="max-w-full" title={view.next.reason}><span className="truncate">{view.next.reset ? "reset is due" : `${labelOf(view.tiers, view.next.to)} is due`}</span></Badge>
        ) : (
          <Badge variant="secondary" className="max-w-full" title={PHASE_HELP[view.ladder.phase]}><span className="truncate">{phaseLabel(view.ladder.phase)}</span></Badge>
        )}
        {view.account.renewalAt ? (
          <span className="mt-0.5 block truncate text-[13px] text-muted-foreground" title={renewalTitle(view.account)}>renews {view.account.renewalEstimated ? "about " : ""}{shortDay(view.account.renewalAt)}</span>
        ) : null}
      </td>
      {/* The row opens the account; these two act in place. */}
      <td className="pr-1! text-right whitespace-nowrap" onClick={(e) => e.stopPropagation()}>
        <Button variant="ghost" size="icon-sm" aria-label={`Poll ${view.account.name}`} title="Poll" disabled={!!busy} onClick={onPoll}>
          <RefreshCw className={polling ? "animate-spin" : ""} />
        </Button>
        <Button variant="ghost" size="icon-sm" aria-label={`Edit ${view.account.name}`} title="Edit" onClick={onEdit}>
          <Pencil />
        </Button>
      </td>
    </tr>
  );
}

/** The tier ranks of an account, the rank it is on, and the rank the ladder would move to. */
function rungs(view: AccountView): { ranks: number[]; currentRank: number | undefined; nextRank: number | undefined } {
  const current = view.reading?.tierId || view.account.tierId;
  return {
    ranks: [...new Set(view.tiers.map((t) => t.rank))].sort((a, b) => a - b),
    currentRank: view.tiers.find((t) => t.id === current)?.rank,
    nextRank: view.next ? view.tiers.find((t) => t.id === view.next?.to)?.rank : undefined,
  };
}

/** The staircase at table size: the current rank filled, the next one outlined. */
function MiniLadder({ view }: { view: AccountView }) {
  const { ranks, currentRank, nextRank } = rungs(view);
  return (
    <span className="flex shrink-0 items-end gap-0.5" aria-hidden="true">
      {ranks.map((rank, i) => (
        <span
          key={rank}
          className={`w-1.5 rounded-[1px] ${rank === currentRank ? "bg-primary" : rank === nextRank ? "border border-primary" : "bg-border"}`}
          style={{ height: MINI_STEP_BASE_PX + i * MINI_STEP_RISE_PX }}
        />
      ))}
    </span>
  );
}

function Detail({
  view, accounts, advice, busy, onBack, onShow, onEdit, onPoll, onRenew, onTest, probe,
}: {
  view: AccountView;
  /** This account's proposed next steps, soonest first. */
  advice: Advice[];
  /** Every account, for the switcher. */
  accounts: AccountView[];
  busy: string;
  onBack: () => void;
  onShow: (id: string) => void;
  onEdit: () => void;
  onPoll: () => void;
  onRenew: () => void;
  onTest: () => void;
  probe: ProbeResult | null;
}) {
  const reading = view.reading;
  const threshold = thresholdOf(view.account);
  // Focus follows the view: the heading takes it when an account opens.
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => heading.current?.focus(), []);
  const tested = probe && probe.accountId === view.account.id ? probe : null;
  const history = view.history ?? [];
  const aside = history.length > 0 || tested !== null;

  return (
    <div className="flex flex-col gap-5">
      <div className="flex items-center justify-between gap-4">
        <Button variant="ghost" size="sm" className="-ml-2.5 text-muted-foreground" onClick={onBack}>
          <ArrowLeft />
          All accounts
        </Button>
        {accounts.length > 1 ? (
          <div className="w-64">
            <NativeSelect aria-label="Show another account" className="h-8 py-1" value={view.account.id} onChange={(e) => onShow(e.currentTarget.value)}>
              {accounts.map((v) => <NativeSelectOption key={v.account.id} value={v.account.id}>{title(v.account.provider)}: {v.account.name}</NativeSelectOption>)}
            </NativeSelect>
          </div>
        ) : null}
      </div>
      <div>
        <div className="flex flex-wrap items-center justify-between gap-x-6 gap-y-3">
          <h2 ref={heading} tabIndex={-1} className="flex min-w-0 items-center gap-2.5 text-[32px] leading-none font-bold tracking-tight outline-none [font-stretch:78%]">
            <AgentMark provider={view.account.provider} className="size-7" />
            <span className="truncate">{view.account.name}</span>
          </h2>
          <div className="flex shrink-0 gap-2">
            <Button variant="outline" size="sm" title="Fetch this account's usage now" disabled={!!busy} onClick={onPoll}>{busy === `poll:${view.account.id}` ? "Polling" : "Poll"}</Button>
            <Button variant="outline" size="sm" title="Refresh the login subman stores for this account, then poll" disabled={!!busy} onClick={onRenew}>{busy === "renew" ? "Renewing" : "Renew login"}</Button>
            <Button variant="outline" size="sm" title="Run this account's agent CLI inside the sandbox and show what it prints" disabled={!!busy} onClick={onTest}>{busy === "test" ? "Testing" : "Test agent"}</Button>
            <Button variant="outline" size="sm" onClick={onEdit}><Pencil />Edit</Button>
          </div>
        </div>
        <p className="mt-2.5 flex flex-wrap items-center gap-2 text-muted-foreground">
          <span className="mr-1 text-foreground">{title(view.account.provider)} {planOf(view)}</span>
          {reading?.account ? <span className="mr-1" title="The login subman stores for this account">{reading.account}</span> : null}
          <Badge variant={view.ladder.phase === "acting" ? "default" : "secondary"} title={PHASE_HELP[view.ladder.phase]}>{phaseLabel(view.ladder.phase)}</Badge>
          {view.account.renewalAt ? (
            <Badge variant="outline" title={when(view.account.renewalAt)}>
              renews {view.account.renewalEstimated ? "about " : ""}{shortDay(view.account.renewalAt)}, {relative(view.account.renewalAt)}
            </Badge>
          ) : null}
        </p>
      </div>

      <div className={`mt-2 grid gap-x-14 gap-y-8 ${aside ? "min-[1100px]:grid-cols-[minmax(0,3fr)_minmax(0,2fr)]" : "max-w-3xl"}`}>
        <div className="flex min-w-0 flex-col gap-8">
          <Ladder view={view} threshold={threshold} onEdit={onEdit} />
          <NextSteps advice={advice} />

          <section>
            <div className="mb-4 flex items-baseline justify-between">
              <h3 className="text-lg font-semibold">Usage</h3>
              <span className={`text-[13px] ${reading && !reading.ok ? "text-destructive" : "text-muted-foreground"}`} title={reading ? when(reading.polledAt) : undefined}>
                {reading ? `${reading.ok ? "polled" : "poll failed"} ${relative(reading.polledAt)}` : "never polled"}
              </span>
            </div>
            {!reading ? <p className="text-sm text-muted-foreground">No reading yet. Poll fetches one.</p> : null}
            {reading && !reading.ok ? (
              <Alert variant="destructive" className="mb-5 border-destructive/50">
                <TriangleAlert />
                <AlertTitle>Last poll failed</AlertTitle>
                <AlertDescription className="break-words">
                  {reading.error || "The provider gave no reason."}
                  <span className="text-muted-foreground">
                    An expired login is the usual cause. Renew it here. If that fails too, open Edit and put a fresh login in.
                  </span>
                  <Button variant="outline" size="sm" className="mt-1 text-foreground" disabled={!!busy} onClick={onRenew}>{busy === "renew" ? "Renewing" : "Renew login"}</Button>
                </AlertDescription>
              </Alert>
            ) : null}
            {reading && reading.windows.length > 0 ? (
              <>
                <ul className={`flex flex-col gap-3.5 ${reading.stale ? "opacity-60" : ""}`}>
                  {reading.windows.map((w) => (
                    <li key={w.label} className="grid grid-cols-[11rem_1fr_3rem_9.5rem] items-center gap-4">
                      <span className="truncate" title={w.label}>{w.label}</span>
                      <Meter label={w.label} util={w.util} threshold={threshold} />
                      <span className="text-right font-semibold">{pct(w.util)}</span>
                      <span className="text-[13px] text-muted-foreground" title={w.resetsAt ? when(w.resetsAt) : undefined}>
                        {w.resetsAt ? `${w.util === null ? "back" : "resets"} ${relative(w.resetsAt)}` : w.util === null ? "not reported" : ""}
                      </span>
                    </li>
                  ))}
                </ul>
                <p className="mt-4 text-[13px] text-muted-foreground">
                  The tick on each bar is the {threshold}% spend threshold.
                  {reading.stale ? " These numbers are from the last poll that worked." : ""}
                </p>
              </>
            ) : null}
            {reading ? <Credits reading={reading} /> : null}
            {reading ? <ResetList reading={reading} /> : null}
          </section>
        </div>

        {aside ? (
          <div className="flex min-w-0 flex-col gap-8">
            {tested ? (
              <section>
                <h3 className="flex items-center gap-2 text-lg font-semibold">
                  {tested.ok ? <CircleCheck className="size-5 text-primary" /> : <CircleX className="size-5 text-destructive" />}
                  Agent test {tested.ok ? "passed" : "failed"}
                </h3>
                {tested.sandboxed ? null : <p className="mt-1 text-sm text-warn">The sandbox did not apply, so the agent did not run.</p>}
                {tested.note ? <p className="mt-1 text-sm text-muted-foreground">{tested.note}</p> : null}
                {tested.output ? <pre className="mt-3 max-h-48 overflow-auto rounded-md border bg-card p-3 font-mono text-xs text-muted-foreground">{tested.output}</pre> : null}
              </section>
            ) : null}
            {history.length > 0 ? <History entries={history} tiers={view.tiers} /> : null}
          </div>
        ) : null}
      </div>
    </div>
  );
}

/**
 * The tiers as a staircase, one step per rank. The current step fills with the
 * highest meter, the value the ladder compares against the threshold.
 */
function Ladder({ view, threshold, onEdit }: { view: AccountView; threshold: number; onEdit: () => void }) {
  const { ranks, currentRank, nextRank } = rungs(view);
  const util = primary(view.reading);
  // Go sends a ladder that never stepped as the year-1 zero time.
  const stepped = view.ladder.lastStep !== undefined && new Date(view.ladder.lastStep).getFullYear() > 1;
  return (
    <section className="flex items-end gap-8">
      <ol className="flex min-w-0 items-end gap-1.5" aria-label="Tier ladder">
        {ranks.map((rank, i) => {
          const here = view.tiers.filter((t) => t.rank === rank);
          const on = rank === currentRank;
          const next = rank === nextRank;
          const state = on ? "on" : next ? "next" : currentRank !== undefined && rank < currentRank ? "below" : "above";
          return (
            <li key={rank} className="flex w-[120px] min-w-14 shrink flex-col gap-2" aria-current={on ? "step" : undefined}>
              <div
                className={`step ${on && util === null ? "absent" : ""}`}
                data-state={state}
                style={{ height: STEP_BASE_PX + i * STEP_RISE_PX, "--tone": util === null ? "var(--primary)" : tone(util, threshold) }}
              >
                {on && util !== null ? (
                  <>
                    <div className="step-fill" style={{ height: `${Math.max(0, Math.min(100, util))}%` }} />
                    <div className="step-tick" style={{ bottom: `${threshold}%` }} />
                    <span className="absolute inset-0 flex items-center justify-center text-2xl font-bold">{pct(util)}</span>
                  </>
                ) : null}
                {next ? (
                  <span className="absolute inset-0 flex items-center justify-center">
                    {currentRank !== undefined && rank < currentRank ? <ArrowDown className="size-5" /> : <ArrowUp className="size-5" />}
                    <span className="sr-only">next step</span>
                  </span>
                ) : null}
              </div>
              <span className={`truncate text-center text-[13px] ${on ? "font-semibold" : "text-muted-foreground"}`}>
                {here.map((t) => t.label).join(" / ")}
              </span>
            </li>
          );
        })}
      </ol>
      <div className="min-w-0 pb-7 text-sm leading-relaxed">
        <p className="text-muted-foreground">{view.ladder.note || "The ladder has not decided anything yet."}</p>
        {view.next ? (
          <p className="mt-1">
            Next: {stepLabel(view.tiers, view.next)}. {view.next.reason}
          </p>
        ) : null}
        {view.ladder.lastError ? <p className="mt-1 break-words text-destructive">{view.ladder.lastError}</p> : null}
        {stepped || view.ladder.cycles > 0 ? (
          <p className="mt-1 text-[13px] text-muted-foreground">
            {stepped ? `Last plan change ${when(view.ladder.lastStep)}. ` : ""}
            {view.ladder.cycles > 0 ? `Returned to the lowest tier ${view.ladder.cycles} time${view.ladder.cycles === 1 ? "" : "s"}.` : ""}
          </p>
        ) : null}
        {view.ladder.phase === "off" || view.ladder.phase === "unarmed" ? (
          <Button variant="link" size="sm" className="mt-1 h-auto p-0" onClick={onEdit}>
            {view.ladder.phase === "off" ? "Turn the ladder on in Edit" : "Set a plan-change command in Edit"}
          </Button>
        ) : null}
      </div>
    </section>
  );
}

function History({ entries, tiers }: { entries: HistoryEntry[]; tiers: Tier[] }) {
  return (
    <section>
      <h3 className="mb-2 text-lg font-semibold">Plan changes</h3>
      <ul className="flex flex-col text-sm">
        {entries.map((e) => (
          <li key={e.at} className="grid grid-cols-[9.5rem_1fr] gap-4 border-t py-1.5">
            <span className="text-muted-foreground">{when(e.at)}</span>
            <span>
              {e.reset ? `Used a banked reset on ${labelOf(tiers, e.from)}` : `${labelOf(tiers, e.from)} to ${labelOf(tiers, e.to)}`}
              {e.error ? <span className="block break-words text-destructive">failed: {e.error}</span> : null}
            </span>
          </li>
        ))}
      </ul>
    </section>
  );
}

/** Mirrors model.ResetExpiryWarning: how long before a banked reset expires the window warns. */
const RESET_EXPIRY_WARNING_MS = 7 * 24 * 60 * MINUTE_MS;

/** The banked resets that expire within the warning window, soonest first. */
function expiringSoon(reading: Reading | undefined): ResetCredit[] {
  const now = Date.now();
  return (reading?.credits.resets ?? []).filter((r) => {
    if (!r.expiresAt) return false;
    const left = new Date(r.expiresAt).getTime() - now;
    return left > 0 && left <= RESET_EXPIRY_WARNING_MS;
  });
}

function renewalTitle(acc: Account): string {
  const at = when(acc.renewalAt);
  return acc.renewalEstimated ? `${at}. Estimated: the provider states when the subscription began, not when it renews. Set the real date in Edit.` : at;
}

/** A board cell: this account's banked resets, or n/a when its provider reports none. */
function ResetsCell({ reading }: { reading: Reading | undefined }) {
  const c = reading?.credits;
  if (!c || c.resetCount === undefined || c.resetCount === null) {
    return <span className="text-muted-foreground" title="This provider's usage payload reports no reset credits">n/a</span>;
  }
  const soon = expiringSoon(reading);
  const usable = c.resetApplies !== undefined && c.resetApplies !== null && c.resetApplies !== c.resetCount;
  return (
    <>
      <span className="font-semibold">{c.resetCount}</span>
      {usable ? <span className="text-[13px] text-muted-foreground"> ({c.resetApplies} usable)</span> : null}
      {soon.length > 0 ? (
        <span className="mt-0.5 flex items-center gap-1 text-[13px] text-warn" title={soon.map((r) => `${r.title || r.type}: ${when(r.expiresAt)}`).join("\n")}>
          <TriangleAlert className="size-3.5 shrink-0" />
          <span className="truncate">{soon.length === 1 ? "expires" : `${soon.length} expire`} {relative(soon[0]?.expiresAt)}</span>
        </span>
      ) : null}
    </>
  );
}

/** Every banked reset by type, with when it expires. Ones inside the warning window are marked. */
function ResetList({ reading }: { reading: Reading }) {
  const resets = reading.credits.resets ?? [];
  if (resets.length === 0) return null;
  const soon = new Set(expiringSoon(reading).map((r) => r.id));
  return (
    <div className="mt-6">
      <h4 className="mb-1 font-semibold">Banked resets</h4>
      <ul className="flex flex-col text-sm">
        {resets.map((r) => (
          <li key={r.id} className="grid grid-cols-[1fr_auto] items-baseline gap-4 border-t py-1.5">
            <span>
              {r.title || r.type}
              <span className="text-[13px] text-muted-foreground">
                {r.title && r.type ? ` (${r.type})` : ""}{r.left > 1 ? `, ${r.left} left` : ""}{r.applies ? "" : ", for a limit that is not the fullest"}
              </span>
            </span>
            {r.expiresAt ? (
              <span className={`flex items-center gap-1 text-[13px] ${soon.has(r.id) ? "text-warn" : "text-muted-foreground"}`} title={when(r.expiresAt)}>
                {soon.has(r.id) ? <TriangleAlert className="size-3.5" /> : null}
                expires {relative(r.expiresAt)}
              </span>
            ) : (
              <span className="text-[13px] text-muted-foreground">does not expire</span>
            )}
          </li>
        ))}
      </ul>
      <p className="mt-2 text-[13px] text-muted-foreground">A reset is only used on the highest tier. Below it, stepping up a tier resets the window and keeps the reset.</p>
    </div>
  );
}

/**
 * The proposed next steps, soonest first: what to do with which account and
 * when, to spend every paid window in full. onSelect opens an account; it is
 * left out where the list is already for one account.
 */
function NextSteps({ advice, onSelect }: { advice: Advice[]; onSelect?: (id: string) => void }) {
  if (advice.length === 0) return null;
  const now = Date.now();
  return (
    <section>
      <h2 className="mb-1 text-lg font-bold [font-stretch:80%]">Next steps</h2>
      <ol className="flex flex-col text-sm">
        {advice.map((a) => {
          const due = new Date(a.at).getTime() <= now + MINUTE_MS;
          return (
            <li key={`${a.accountId}:${a.kind}:${a.at}:${a.text}`} className={`grid items-baseline gap-4 border-t py-1.5 ${onSelect ? "grid-cols-[7rem_11rem_1fr]" : "grid-cols-[7rem_1fr]"}`}>
              <span className={due ? "font-semibold text-primary" : "text-muted-foreground"} title={when(a.at)}>{due ? "now" : relative(a.at)}</span>
              {onSelect ? (
                <button className="truncate rounded-sm text-left font-semibold outline-none hover:underline focus-visible:ring-[3px] focus-visible:ring-ring/50" onClick={() => onSelect(a.accountId)}>{a.account}</button>
              ) : null}
              <span className={a.kind === "login" ? "text-destructive" : a.kind === "expiring" ? "text-warn" : ""}>{a.text}</span>
            </li>
          );
        })}
      </ol>
    </section>
  );
}

function Credits({ reading }: { reading: Reading }) {
  const c = reading.credits;
  const rows: [string, string][] = [];
  if (c.resetCount !== undefined && c.resetCount !== null) {
    // Some banked resets only apply to a limit that is not the one in the way.
    const usable = c.resetApplies !== undefined && c.resetApplies !== null && c.resetApplies !== c.resetCount ? `, ${c.resetApplies} usable now` : "";
    rows.push(["Reset credits", `${c.resetCount}${usable}`]);
  }
  if (c.balance !== undefined) rows.push(["Balance", num(c.balance)]);
  if (c.enabled && c.used !== undefined) {
    rows.push(["Extra usage", `${num(c.used)}${c.limit !== undefined ? ` of ${num(c.limit)}` : ""} ${c.currency || ""}`.trim()]);
  }
  if (c.unlimited) rows.push(["Credits", "unlimited"]);
  if (rows.length === 0) return null;
  return (
    <dl className="mt-6 flex w-fit divide-x rounded-md border">
      {rows.map(([label, value]) => (
        <div key={label} className="px-4 py-2.5">
          <dt className="text-[13px] text-muted-foreground">{label}</dt>
          <dd className="font-semibold">{value}</dd>
        </div>
      ))}
    </dl>
  );
}

function SettingsSheet({
  view, busy, errorLine, defaultCredential, login, canSignIn, onClose, onSave, onReplace, onSignIn, onCancelSignIn, onDelete,
}: {
  view: AccountView;
  busy: string;
  errorLine: ComponentChildren;
  /** Where the provider CLI keeps its login, offered as the file to copy from. */
  defaultCredential: string;
  onClose: () => void;
  onSave: (acc: Account) => void;
  onReplace: (path: string) => void;
  /** The sign-in in progress, when there is one. */
  login: LoginStart | null;
  /** subman can sign in to this account's provider itself. */
  canSignIn: boolean;
  onSignIn: () => void;
  onCancelSignIn: () => void;
  onDelete: () => void;
}) {
  const [form, setForm] = useState(view.account);
  const [credential, setCredential] = useState(defaultCredential);
  const [confirmDiscard, setConfirmDiscard] = useState(false);
  // The account as it was when the sheet opened; an edit is anything that differs from it.
  const opened = useRef(view.account);
  // Dates are edited as text and turned into instants on save; an empty field clears the date.
  const [activated, setActivated] = useState(day(view.account.activatedAt));
  const [renewal, setRenewal] = useState(day(view.account.renewalAt));
  // The threshold is edited as text too, so the field can be emptied while typing.
  const [threshold, setThreshold] = useState(String(thresholdOf(view.account)));
  const [formError, setFormError] = useState("");
  // "" and a missing field are the same thing here: typing into an optional
  // field and emptying it again is not an edit.
  const settled = (acc: Account) => JSON.stringify(acc, (_key, value: unknown) => (value === "" ? undefined : value));
  const dirty =
    settled(form) !== settled(opened.current) ||
    activated !== day(opened.current.activatedAt) ||
    renewal !== day(opened.current.renewalAt) ||
    threshold !== String(thresholdOf(opened.current));

  return (
    <Sheet
      open
      onOpenChange={(v) => {
        if (v) return;
        if (dirty) setConfirmDiscard(true);
        else onClose();
      }}
    >
      <SheetContent
        className="gap-0 sm:max-w-md"
        // Coming back from the browser, the first click lands outside the sheet. It must not end the sign-in.
        onInteractOutside={(e) => { if (login) e.preventDefault(); }}
      >
        <SheetHeader>
          <SheetTitle>Edit {view.account.name}</SheetTitle>
          <SheetDescription className="break-all font-mono text-xs">{view.account.agentHome}</SheetDescription>
        </SheetHeader>
        <form
          className="flex min-h-0 flex-1 flex-col"
          onSubmit={(e) => {
            e.preventDefault();
            const dates = { activatedAt: fromDay(activated), renewalAt: fromDay(renewal) };
            if ((activated && !dates.activatedAt) || (renewal && !dates.renewalAt)) {
              setFormError("Activated and Renewal each need a real date as YYYY-MM-DD, or nothing.");
              return;
            }
            const usageThreshold = Number(threshold);
            if (!(usageThreshold >= 1 && usageThreshold <= 100)) {
              setFormError("The spend threshold is a number from 1 to 100.");
              return;
            }
            setFormError("");
            onSave({ ...form, ...dates, usageThreshold });
          }}
        >
          <div className="flex flex-1 flex-col gap-4 overflow-auto px-4 pb-4">
            <div className="grid grid-cols-2 gap-3">
              <Field label="Name"><Input required value={form.name} onInput={(e) => setForm({ ...form, name: e.currentTarget.value })} /></Field>
              <Field label="Tier">
                <NativeSelect value={form.tierId} onChange={(e) => setForm({ ...form, tierId: e.currentTarget.value })}>
                  <NativeSelectOption value="">not reported</NativeSelectOption>
                  {view.tiers.map((t) => <NativeSelectOption key={t.id} value={t.id}>{t.label}</NativeSelectOption>)}
                </NativeSelect>
              </Field>
              <Field label="Activated"><DayInput value={activated} onChange={setActivated} /></Field>
              <Field label="Renewal"><DayInput value={renewal} onChange={setRenewal} /></Field>
              <Field label="Spend threshold, %"><Input inputMode="numeric" value={threshold} onInput={(e) => setThreshold(e.currentTarget.value)} /></Field>
              <Field label="Agent binary"><Input placeholder="the one on PATH" value={form.agentBin || ""} onInput={(e) => setForm({ ...form, agentBin: e.currentTarget.value })} /></Field>
            </div>
            <p className="-mt-2 text-[13px] leading-relaxed text-muted-foreground">A good poll replaces the tier, and the dates where the provider states them, with the provider's own.</p>
            <Field label="Plan-change command">
              <Input className="font-mono text-[13px]" placeholder="/path/to/change {from} {to}" value={form.changeCommand || ""} onInput={(e) => setForm({ ...form, changeCommand: e.currentTarget.value })} />
            </Field>
            <p className="-mt-2 text-[13px] leading-relaxed text-muted-foreground">One executable, not a shell line. With none set, the ladder names the next step and does not take it.</p>
            <Label className="justify-between rounded-md border px-3 py-2.5">
              Ladder
              <Switch checked={form.ladder} onCheckedChange={(v) => setForm({ ...form, ladder: v })} />
            </Label>
            <Label className="justify-between rounded-md border px-3 py-2.5">
              Use a banked reset when the highest tier is spent
              <Switch checked={form.autoReset} onCheckedChange={(v) => setForm({ ...form, autoReset: v })} />
            </Label>
            <div className="mt-2 flex flex-col gap-3 border-t pt-4">
              <h3 className="font-semibold">Login</h3>
              {login ? (
                <SignInWaiting login={login} provider={title(view.account.provider)} onCancel={onCancelSignIn} />
              ) : (
                <>
                  {canSignIn ? (
                    <>
                      <p className="text-[13px] leading-relaxed text-muted-foreground">
                        For a login that Renew can no longer refresh. A new sign-in replaces the stored login at once and does not wait for Save.
                      </p>
                      <Button type="button" variant="outline" size="sm" className="self-start" disabled={!!busy} onClick={onSignIn}>Sign in again</Button>
                    </>
                  ) : null}
                  <CredentialInput label={canSignIn ? "Or copy a credential file over it" : "Copy a fresh credential file over the stored one"} value={credential} onChange={setCredential} />
                  <Button type="button" variant="outline" size="sm" className="self-start" disabled={!!busy || !credential} onClick={() => onReplace(credential)}>
                    {busy === "replace" ? "Replacing" : "Replace credential"}
                  </Button>
                </>
              )}
            </div>
          </div>
          {/* Above the footer and outside the scroll area, so a failure is seen wherever the form is scrolled. */}
          {errorLine || formError ? (
            <div className="flex flex-col gap-2 border-t px-4 py-3">
              {errorLine}
              {formError ? <p className="text-[13px] text-destructive" role="alert">{formError}</p> : null}
            </div>
          ) : null}
          <SheetFooter className="flex-row justify-between border-t">
            <AlertDialog>
              <AlertDialogTrigger asChild>
                <Button type="button" variant="ghost" className="text-destructive hover:text-destructive" disabled={!!busy}>Remove account</Button>
              </AlertDialogTrigger>
              <AlertDialogContent>
                <AlertDialogHeader>
                  <AlertDialogTitle>Remove {view.account.name}?</AlertDialogTitle>
                  <AlertDialogDescription>
                    This deletes <span className="break-all font-mono text-xs">{view.account.home}</span>, including the login subman stores for it. The vendor CLI's own login is not touched.
                  </AlertDialogDescription>
                </AlertDialogHeader>
                <AlertDialogFooter>
                  <AlertDialogCancel>Keep account</AlertDialogCancel>
                  <AlertDialogAction variant="destructive" onClick={onDelete}>Remove account</AlertDialogAction>
                </AlertDialogFooter>
              </AlertDialogContent>
            </AlertDialog>
            <Button type="submit" disabled={!!busy}>{busy === "save" ? "Saving" : "Save changes"}</Button>
          </SheetFooter>
        </form>
        <AlertDialog open={confirmDiscard} onOpenChange={setConfirmDiscard}>
          <AlertDialogContent size="sm">
            <AlertDialogHeader>
              <AlertDialogTitle>Discard your changes?</AlertDialogTitle>
              <AlertDialogDescription>The edits to {view.account.name} have not been saved.</AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>Keep editing</AlertDialogCancel>
              <AlertDialogAction variant="destructive" onClick={onClose}>Discard changes</AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      </SheetContent>
    </Sheet>
  );
}

/** What a sign-in shows while the operator is in the browser. */
function SignInWaiting({ login, provider, onCancel }: { login: LoginStart; provider: string; onCancel: () => void }) {
  return (
    <div className="flex flex-col gap-3" role="status">
      <p>Finish signing in to {provider} in your browser. This window picks it up when you are done.</p>
      {login.userCode ? (
        <p>
          The page should show this code: <span className="font-mono text-base font-semibold tracking-wider">{login.userCode}</span>
        </p>
      ) : null}
      <p className="text-[13px] leading-relaxed text-muted-foreground">
        No page opened? Open this address yourself:
        <span className="mt-1 block max-h-16 overflow-auto rounded-md border bg-card px-2 py-1 font-mono text-xs break-all select-all">{login.url}</span>
      </p>
      <div className="flex gap-2">
        <Button type="button" variant="outline" size="sm" onClick={() => openExternal(login.url)}>Open the page again</Button>
        <Button type="button" variant="outline" size="sm" onClick={onCancel}>Cancel sign-in</Button>
      </div>
    </div>
  );
}

function AddDialog({
  draft, providers, busy, login, errorLine, onChange, onClose, onSignIn, onCancelSignIn, onImport,
}: {

  draft: ImportRequest;
  providers: ProviderInfo[];
  /** The action in flight, if any. Submitting waits for it. */
  busy: string;
  /** The sign-in in progress, when there is one. */
  login: LoginStart | null;
  errorLine: ComponentChildren;
  onChange: (d: ImportRequest) => void;
  onClose: () => void;
  onSignIn: () => void;
  onCancelSignIn: () => void;
  onImport: () => void;
}) {
  const current = providers.find((p) => p.id === draft.provider);
  // A provider subman cannot sign in to takes a credential file; the others may.
  const [preferFile, setPreferFile] = useState(false);
  const fromFile = preferFile || !current?.login;
  return (
    <Dialog open onOpenChange={(v) => { if (!v) onClose(); }}>
      {/* Coming back from the browser, the first click lands outside the dialog. It must not end the sign-in. */}
      <DialogContent onInteractOutside={(e) => { if (login) e.preventDefault(); }}>
        <DialogHeader>
          <DialogTitle>Add account</DialogTitle>
          <DialogDescription>
            {fromFile ? "The credential file is copied. The vendor CLI keeps its own." : "Sign in from here. subman keeps its own login and leaves the vendor CLI's alone."}
          </DialogDescription>
        </DialogHeader>
        {login ? (
          <SignInWaiting login={login} provider={current?.title ?? draft.provider} onCancel={onCancelSignIn} />
        ) : (
          <form className="flex flex-col gap-4" onSubmit={(e) => { e.preventDefault(); if (fromFile) onImport(); else onSignIn(); }}>
            {errorLine}
            <div className="grid grid-cols-2 gap-3">
              <Field label="Name, optional"><Input placeholder="the login's email" value={draft.name} onInput={(e) => onChange({ ...draft, name: e.currentTarget.value })} /></Field>
              <Field label="Provider">
                <NativeSelect
                  value={draft.provider}
                  onChange={(e) => {
                    const next = providers.find((p) => p.id === e.currentTarget.value);
                    if (!next) return;
                    // The path follows the provider unless the operator typed their own.
                    const untouched = draft.credentialPath === "" || draft.credentialPath === (current?.credentialPath ?? "");
                    onChange({
                      ...draft,
                      provider: next.id,
                      tierId: "",
                      credentialPath: untouched ? next.credentialPath ?? "" : draft.credentialPath,
                    });
                  }}
                >
                  {providers.map((p) => <NativeSelectOption key={p.id} value={p.id}>{p.title}</NativeSelectOption>)}
                </NativeSelect>
              </Field>
            </div>
            <p className="-mt-1 text-[13px] leading-relaxed text-muted-foreground">
              The tier is read from the provider on the first poll, and so are the activation and renewal dates where the provider states them. Anything it does not state can be set in Edit.
            </p>
            {fromFile ? (
              <>
                <CredentialInput label="Credential file" required value={draft.credentialPath} onChange={(credentialPath) => onChange({ ...draft, credentialPath })} />
                <p className="-mt-2 text-[13px] leading-relaxed text-muted-foreground">
                  {current?.login
                    ? `The file the ${current.title} CLI wrote when you signed in there.`
                    : `${current?.title ?? "This provider"} has no sign-in here, so point at the file its CLI wrote when you signed in there.`}
                </p>
              </>
            ) : null}
            <DialogFooter className="sm:justify-between">
              {current?.login ? (
                <Button type="button" variant="link" className="px-0 text-muted-foreground" onClick={() => setPreferFile(!preferFile)}>
                  {preferFile ? "Sign in with the browser instead" : "Use a credential file instead"}
                </Button>
              ) : <span />}
              <span className="flex gap-2">
                <Button type="button" variant="outline" onClick={onClose}>Cancel</Button>
                <Button type="submit" disabled={!!busy}>{fromFile ? (busy === "add" ? "Adding" : "Add account") : `Sign in to ${current?.title ?? ""}`}</Button>
              </span>
            </DialogFooter>
          </form>
        )}
      </DialogContent>
    </Dialog>
  );
}

/**
 * A labelled path field with a button that opens the native file dialog
 * beside the current path. The label names the input alone: the button and
 * the failure text sit outside it.
 */
function CredentialInput({ label, value, required, onChange }: { label: string; value: string; required?: boolean; onChange: (path: string) => void }) {
  const id = useId();
  const [failure, setFailure] = useState("");
  return (
    <div className="flex flex-col gap-1.5">
      <Label htmlFor={id} className="font-normal text-muted-foreground">{label}</Label>
      <div className="flex gap-2">
        <Input id={id} className="font-mono text-[13px]" required={required} value={value} onInput={(e) => onChange(e.currentTarget.value)} />
        <Button
          type="button"
          variant="outline"
          className="shrink-0"
          onClick={async () => {
            setFailure("");
            try {
              const picked = await api.pickCredential(value);
              if (picked) onChange(picked);
            } catch (err) {
              setFailure(messageOf(err));
            }
          }}
        >
          <FolderOpen />
          Browse
        </Button>
      </div>
      {failure ? <p className="text-[13px] text-destructive" role="alert">The file dialog did not open: {failure}. Type the path instead.</p> : null}
    </div>
  );
}

/**
 * A day as YYYY-MM-DD text. Not type="date": WebKitGTK draws an empty date
 * input as today's date, which reads as a value that was never entered.
 */
function DayInput({ value, onChange }: { value: string; onChange: (day: string) => void }) {
  return (
    <Input
      type="text"
      inputMode="numeric"
      placeholder="YYYY-MM-DD"
      pattern="\d{4}-\d{2}-\d{2}"
      title="A date as YYYY-MM-DD"
      value={value}
      onInput={(e) => onChange(e.currentTarget.value)}
    />
  );
}

function Field({ label, children }: { label: string; children: ComponentChildren }) {
  return (
    <Label className="flex-col items-stretch gap-1.5 font-normal text-muted-foreground">
      {label}
      {children}
    </Label>
  );
}

function thresholdOf(acc: Account): number {
  const t = acc.usageThreshold;
  return t === undefined || t <= 0 || t > 100 ? DEFAULT_USAGE_THRESHOLD : t;
}

/** The highest reported meter, the same value Reading.PrimaryUtil gives the ladder. */
function primary(reading: Reading | undefined): number | null {
  let best: number | null = null;
  for (const w of reading?.windows ?? []) {
    if (w.util === null || w.util === undefined) continue;
    if (best === null || w.util > best) best = w.util;
  }
  return best;
}

function planOf(view: AccountView): string {
  if (view.reading?.plan) return view.reading.plan;
  return view.tiers.find((t) => t.id === view.account.tierId)?.label || "not reported";
}

/** A due step in words: a plan change from one tier to another, or a banked reset. */
function stepLabel(tiers: Tier[], step: Step): string {
  return step.reset ? `use a banked reset on ${labelOf(tiers, step.from)}` : `${labelOf(tiers, step.from)} to ${labelOf(tiers, step.to)}`;
}

function labelOf(tiers: Tier[], id: string): string {
  return tiers.find((t) => t.id === id)?.label || id;
}

function title(p: Provider): string {
  switch (p) {
    case "claude": return "Claude";
    case "codex": return "Codex";
    case "grok": return "Grok";
    case "antigravity": return "Antigravity";
  }
}

function phaseLabel(phase: Phase): string {
  switch (phase) {
    case "off": return "ladder off";
    case "unarmed": return "unarmed";
    case "climbing": return "climbing";
    case "waiting": return "waiting on renewal";
    case "acting": return "acting";
  }
}

/** The vendor's mark in the text colour. The files under public/agents are single-colour, so they work as masks. */
function AgentMark({ provider, className }: { provider: Provider; className: string }) {
  const mask = `url(/agents/${provider}.svg) center / contain no-repeat`;
  return <span aria-hidden="true" className={`inline-block shrink-0 bg-current ${className}`} style={{ mask, WebkitMask: mask }} />;
}
