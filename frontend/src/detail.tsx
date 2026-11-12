import { useEffect, useRef } from "preact/hooks";
import { ArrowDown, ArrowLeft, ArrowUp, CircleCheck, CircleX, Pencil, TriangleAlert } from "lucide-preact";
import { num, pct, relative, shortDay, tone, when } from "./format";
import type { AccountView, Advice, HistoryEntry, ProbeResult, Reading, Tier } from "./types";
import { AgentMark, Meter, NextSteps, PHASE_HELP, expiringSoon, labelOf, phaseLabel, planOf, primary, rungs, stepLabel, title } from "./shared";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { NativeSelect, NativeSelectOption } from "@/components/ui/native-select";

/** Ladder drawing: height of the lowest step and the rise per rank, in px. */
const STEP_BASE_PX = 64;

const STEP_RISE_PX = 36;

export function Detail({
  view, warningMs, accounts, advice, busy, onBack, onShow, onEdit, onPoll, onRenew, onTest, probe,
}: {
  view: AccountView;
  /** Overview.resetExpiryWarningMs. */
  warningMs: number;
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
  const threshold = view.threshold;
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
          <h2 ref={heading} tabIndex={-1} className="flex min-w-0 items-center gap-2.5 text-[2rem] font-bold tracking-tight outline-none [font-stretch:78%]">
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
        {view.account.renewalAt && view.account.renewalEstimated ? (
          <p className="mt-1.5 text-micro text-muted-foreground">The renewal date is an estimate: the provider states when the subscription began, not when it renews. Set the real date in Edit.</p>
        ) : null}
      </div>

      <div className={`mt-2 grid gap-x-14 gap-y-8 ${aside ? "min-[1100px]:grid-cols-account" : "max-w-[48rem]"}`}>
        <div className="flex min-w-0 flex-col gap-8">
          <Ladder view={view} threshold={threshold} onEdit={onEdit} />
          <NextSteps advice={advice} />

          <section>
            <div className="mb-4 flex items-baseline justify-between">
              <h3 className="text-intro font-semibold">Usage</h3>
              <span className={`text-micro ${reading && !reading.ok ? "text-destructive" : "text-muted-foreground"}`} title={reading ? when(reading.polledAt) : undefined}>
                {reading ? `${reading.ok ? "polled" : "poll failed"} ${relative(reading.polledAt)}` : "never polled"}
              </span>
            </div>
            {!reading ? <p className="text-data text-muted-foreground">No reading yet. Poll fetches one.</p> : null}
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
                <div className="overflow-x-auto">
                  <ul className="flex min-w-[36rem] flex-col gap-3.5">
                    {reading.windows.map((w) => (
                      <li key={w.label} className="grid grid-cols-meters items-center gap-4">
                        <span className="truncate" title={w.label}>{w.label}</span>
                        <Meter label={w.label} util={w.util} threshold={threshold} stale={reading.stale} />
                        <span className="text-right font-semibold">{pct(w.util)}</span>
                        <span className="text-micro text-muted-foreground" title={w.resetsAt ? when(w.resetsAt) : undefined}>
                          {w.resetsAt ? `${w.util === null ? "back" : "resets"} ${relative(w.resetsAt)}` : w.util === null ? "not reported" : ""}
                        </span>
                      </li>
                    ))}
                  </ul>
                </div>
                <p className="mt-4 text-micro text-muted-foreground">
                  The tick on each bar is the {threshold}% spend threshold.
                  {reading.stale ? " These numbers are from the last poll that worked." : ""}
                </p>
              </>
            ) : null}
            {reading ? <Credits reading={reading} /> : null}
            {reading ? <ResetList reading={reading} warningMs={warningMs} /> : null}
          </section>
        </div>

        {aside ? (
          <div className="flex min-w-0 flex-col gap-8">
            {tested ? (
              <section>
                <h3 className="flex items-center gap-2 text-intro font-semibold">
                  {tested.ok ? <CircleCheck className="size-5 text-accent" /> : <CircleX className="size-5 text-destructive" />}
                  Agent test {tested.ok ? "passed" : "failed"}
                </h3>
                {tested.sandboxed ? null : <p className="mt-1 text-data text-st-warn">The sandbox did not apply, so the agent did not run.</p>}
                {tested.note ? <p className="mt-1 text-data text-muted-foreground">{tested.note}</p> : null}
                {tested.output ? <pre className="mt-3 max-h-48 overflow-auto rounded-control border bg-card p-3 font-mono text-micro text-muted-foreground">{tested.output}</pre> : null}
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
    <section className="flex flex-wrap items-end gap-8">
      <ol className="flex min-w-0 max-w-full items-end gap-1.5 overflow-x-auto" aria-label="Tier ladder">
        {ranks.map((rank, i) => {
          const here = view.tiers.filter((t) => t.rank === rank);
          const on = rank === currentRank;
          const next = rank === nextRank;
          const state = on ? "on" : next ? "next" : currentRank !== undefined && rank < currentRank ? "below" : "above";
          return (
            <li key={rank} className="flex w-[120px] shrink-0 flex-col gap-2" aria-current={on ? "step" : undefined}>
              <div
                className={`step ${on && util === null ? "absent" : ""}`}
                data-state={state}
                style={{ height: STEP_BASE_PX + i * STEP_RISE_PX, "--tone": util === null ? "var(--color-text-muted)" : tone(util, threshold) }}
              >
                {on && util !== null ? (
                  <>
                    <div className="step-fill" style={{ height: `${Math.max(0, Math.min(100, util))}%` }} />
                    <div className="step-tick" style={{ bottom: `${threshold}%` }} />
                    <span className="absolute inset-0 flex items-center justify-center text-figure font-bold">{pct(util)}</span>
                  </>
                ) : null}
                {next ? (
                  <span className="absolute inset-0 flex items-center justify-center">
                    {currentRank !== undefined && rank < currentRank ? <ArrowDown className="size-5" /> : <ArrowUp className="size-5" />}
                    <span className="sr-only">next step</span>
                  </span>
                ) : null}
              </div>
              <span className={`truncate text-center text-micro ${on ? "font-semibold" : "text-muted-foreground"}`}>
                {here.map((t) => t.label).join(" / ")}
              </span>
            </li>
          );
        })}
      </ol>
      <div className="min-w-0 pb-7 text-data leading-prose">
        <p className="text-muted-foreground">{view.ladder.note || "The ladder has not decided anything yet."}</p>
        {view.next ? (
          <p className="mt-1">
            Next: {stepLabel(view.tiers, view.next)}. {view.next.reason}
          </p>
        ) : null}
        {view.ladder.lastError ? <p className="mt-1 break-words text-destructive">{view.ladder.lastError}</p> : null}
        {stepped || view.ladder.cycles > 0 ? (
          <p className="mt-1 text-micro text-muted-foreground">
            {stepped ? `Last plan change ${when(view.ladder.lastStep)}. ` : ""}
            {view.ladder.cycles > 0 ? `Returned to the lowest tier ${view.ladder.cycles} time${view.ladder.cycles === 1 ? "" : "s"}.` : ""}
          </p>
        ) : null}
        {view.ladder.phase === "off" || view.ladder.phase === "unarmed" ? (
          <Button variant="link" size="sm" className="mt-1 h-auto px-0 py-0.5" onClick={onEdit}>
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
      <h3 className="mb-2 text-intro font-semibold">Plan changes</h3>
      <ul className="flex flex-col text-data">
        {entries.map((e) => (
          <li key={e.at} className="grid grid-cols-history gap-4 border-t py-1.5">
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

/** Every banked reset by type, with when it expires. Ones inside the warning window are marked. */
function ResetList({ reading, warningMs }: { reading: Reading; warningMs: number }) {
  const resets = reading.credits.resets ?? [];
  if (resets.length === 0) return null;
  const soon = new Set(expiringSoon(reading, warningMs).map((r) => r.id));
  return (
    <div className="mt-6">
      <h4 className="mb-1 font-semibold">Banked resets</h4>
      <ul className="flex flex-col text-data">
        {resets.map((r) => (
          <li key={r.id} className="grid grid-cols-resets items-baseline gap-4 border-t py-1.5">
            <span>
              {r.title || r.type}
              <span className="text-micro text-muted-foreground">
                {r.title && r.type ? ` (${r.type})` : ""}{r.left > 1 ? `, ${r.left} left` : ""}{r.applies ? "" : ", for a limit that is not the fullest"}
              </span>
            </span>
            {r.expiresAt ? (
              <span className={`flex items-center gap-1 text-micro ${soon.has(r.id) ? "text-st-warn" : "text-muted-foreground"}`} title={when(r.expiresAt)}>
                {soon.has(r.id) ? <TriangleAlert className="size-3.5" /> : null}
                expires {relative(r.expiresAt)}
              </span>
            ) : (
              <span className="text-micro text-muted-foreground">does not expire</span>
            )}
          </li>
        ))}
      </ul>
      <p className="mt-2 text-micro text-muted-foreground">A reset is only used on the highest tier. Below it, stepping up a tier resets the window and keeps the reset.</p>
    </div>
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
    <dl className="mt-6 flex w-fit divide-x rounded-control border">
      {rows.map(([label, value]) => (
        <div key={label} className="px-4 py-2.5">
          <dt className="text-micro text-muted-foreground">{label}</dt>
          <dd className="font-semibold">{value}</dd>
        </div>
      ))}
    </dl>
  );
}
