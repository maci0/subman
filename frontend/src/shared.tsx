import { relative, tone, when } from "./format";
import type { Account, AccountView, Advice, Phase, Provider, Reading, ResetCredit, Step, Tier } from "./types";

export const MINUTE_MS = 60_000;

/** What each ladder phase means, shown on hover. */
export const PHASE_HELP: Record<Phase, string> = {
  off: "The ladder is switched off for this account.",
  unarmed: "The ladder is on but has no plan-change command, so it names the next plan change and does not take it. A banked reset still runs, since it needs no command.",
  climbing: "The ladder steps up one tier when the fullest meter reaches the spend threshold.",
  waiting: "Parked on the lowest tier until the renewal date.",
  acting: "A plan change is in progress.",
};

export function messageOf(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

/**
 * One bar. A null util is drawn hatched and never as an empty 0% track.
 * stale fades the bar of a reading kept from before a failed poll; the text
 * beside it stays at full contrast.
 */
export function Meter({ label, util, threshold, stale }: { label: string; util: number | null | undefined; threshold: number; stale?: boolean }) {
  if (util === null || util === undefined) {
    return <div className="meter absent" role="img" aria-label={`${label}: not reported`} />;
  }
  const width = Math.max(0, Math.min(100, util));
  return (
    <div className={`meter ${stale ? "meter-stale" : ""}`} role="meter" aria-label={stale ? `${label}, from an earlier poll` : label} aria-valuemin={0} aria-valuemax={100} aria-valuenow={Math.round(width)}>
      <div className="meter-fill" style={{ width: `${width}%`, "--tone": tone(util, threshold) }} />
      <span className="meter-tick" style={{ left: `${threshold}%` }} />
    </div>
  );
}

/** The tier ranks of an account, the rank it is on, and the rank the ladder would move to. */
export function rungs(view: AccountView): { ranks: number[]; currentRank: number | undefined; nextRank: number | undefined } {
  const current = view.reading?.tierId || view.account.tierId;
  return {
    ranks: [...new Set(view.tiers.map((t) => t.rank))].sort((a, b) => a - b),
    currentRank: view.tiers.find((t) => t.id === current)?.rank,
    nextRank: view.next ? view.tiers.find((t) => t.id === view.next?.to)?.rank : undefined,
  };
}

/** The banked resets that expire within warningMs (Overview.resetExpiryWarningMs), soonest first. */
export function expiringSoon(reading: Reading | undefined, warningMs: number): ResetCredit[] {
  const now = Date.now();
  return (reading?.credits.resets ?? []).filter((r) => {
    if (!r.expiresAt) return false;
    const left = new Date(r.expiresAt).getTime() - now;
    return left > 0 && left <= warningMs;
  });
}

export function renewalTitle(acc: Account): string {
  const at = when(acc.renewalAt);
  return acc.renewalEstimated ? `${at}. Estimated: the provider states when the subscription began, not when it renews. Set the real date in Edit.` : at;
}

/**
 * The proposed next steps, soonest first: what to do with which account and
 * when, to spend every paid window in full. onSelect opens an account; it is
 * left out where the list is already for one account.
 */
export function NextSteps({ advice, nameOf, onSelect }: { advice: Advice[]; nameOf?: (id: string) => string; onSelect?: (id: string) => void }) {
  if (advice.length === 0) return null;
  const now = Date.now();
  return (
    <section>
      <h2 className="mb-1 text-intro font-bold">Next steps</h2>
      <ol className="flex flex-col text-data">
        {advice.map((a) => {
          const due = new Date(a.at).getTime() <= now + MINUTE_MS;
          return (
            <li key={`${a.accountId}:${a.kind}:${a.at}:${a.text}`} className={`grid items-baseline gap-4 border-t py-1.5 ${onSelect ? "grid-cols-steps-account" : "grid-cols-steps"}`}>
              <span className={due ? "font-semibold text-accent" : "text-muted-foreground"} title={when(a.at)}>{due ? "now" : relative(a.at)}</span>
              {onSelect ? (
                <button className="min-h-6 truncate rounded-chip py-0.5 text-left font-semibold outline-none hover:underline focus-visible:outline-2 focus-visible:outline-solid focus-visible:outline-offset-2 focus-visible:outline-ring" title={nameOf?.(a.accountId) || a.accountId} onClick={() => onSelect(a.accountId)}>{nameOf?.(a.accountId) || a.accountId}</button>
              ) : null}
              <span className={a.kind === "login" ? "text-destructive" : a.kind === "expiring" ? "text-st-warn" : ""}>{a.text}</span>
            </li>
          );
        })}
      </ol>
    </section>
  );
}

/** The highest reported meter, the same value Reading.PrimaryUtil gives the ladder. */
export function primary(reading: Reading | undefined): number | null {
  let best: number | null = null;
  for (const w of reading?.windows ?? []) {
    if (w.util === null || w.util === undefined) continue;
    if (best === null || w.util > best) best = w.util;
  }
  return best;
}

export function planOf(view: AccountView): string {
  if (view.reading?.plan) return view.reading.plan;
  return view.tiers.find((t) => t.id === view.account.tierId)?.label || "not reported";
}

/** A due step in words: a plan change from one tier to another, or a banked reset. */
export function stepLabel(tiers: Tier[], step: Step): string {
  return step.reset ? `use a banked reset on ${labelOf(tiers, step.from)}` : `${labelOf(tiers, step.from)} to ${labelOf(tiers, step.to)}`;
}

export function labelOf(tiers: Tier[], id: string): string {
  return tiers.find((t) => t.id === id)?.label || id;
}

export function title(p: Provider): string {
  switch (p) {
    case "claude": return "Claude";
    case "codex": return "Codex";
    case "grok": return "Grok";
    case "antigravity": return "Antigravity";
  }
}

export function phaseLabel(phase: Phase): string {
  switch (phase) {
    case "off": return "ladder off";
    case "unarmed": return "unarmed";
    case "climbing": return "climbing";
    case "waiting": return "waiting on renewal";
    case "acting": return "acting";
  }
}

/** The vendor's mark in the text colour. The files under public/agents are single-colour, so they work as masks. */
export function AgentMark({ provider, className }: { provider: Provider; className: string }) {
  const mask = `url(/agents/${provider}.svg) center / contain no-repeat`;
  return <span aria-hidden="true" className={`inline-block shrink-0 bg-current ${className}`} style={{ mask, WebkitMask: mask }} />;
}
