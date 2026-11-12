import { useEffect } from "preact/hooks";
import { ChevronRight, Pencil, RefreshCw, TriangleAlert } from "lucide-preact";
import { pct, relative, shortDay, when } from "./format";
import type { AccountView, Advice, Provider, Reading } from "./types";
import { AgentMark, Meter, NextSteps, PHASE_HELP, expiringSoon, labelOf, phaseLabel, planOf, primary, renewalTitle, rungs, title } from "./shared";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";

/** The tier staircase in a board row: height of the lowest step and the rise per rank, in px. */
const MINI_STEP_BASE_PX = 6;

const MINI_STEP_RISE_PX = 4;

/** Every account in one table per provider, so accounts of one provider line up meter by meter. */
export function Board({
  accounts, order, warningMs, advice, busy, polled, folded, focusId, onFold, onSelect, onEdit, onPoll,
}: {
  accounts: AccountView[];
  /** The providers in the order their sections appear: the order App.Providers lists them. */
  order: Provider[];
  /** Overview.resetExpiryWarningMs. */
  warningMs: number;
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
      {order.filter((p) => groups.has(p)).map((provider) => {
        const views = groups.get(provider) ?? [];
        const labels = [...new Set(views.flatMap((v) => v.reading?.windows.map((w) => w.label) ?? []))];
        const shut = folded.has(provider);
        // What a folded provider still says: its fullest meter, and whether a poll failed.
        const utils = views.map((v) => primary(v.reading)).filter((u) => u !== null);
        const fullest = utils.length > 0 ? Math.max(...utils) : null;
        const failures = views.filter((v) => v.reading !== undefined && !v.reading.ok).length;
        return (
          <details key={provider} className="min-w-0" open={!shut} onToggle={(e) => fold(provider, !e.currentTarget.open)}>
            <summary className="mb-1 flex cursor-pointer list-none items-center gap-2 rounded-chip outline-none focus-visible:outline-2 focus-visible:outline-solid focus-visible:outline-offset-2 focus-visible:outline-ring [&::-webkit-details-marker]:hidden">
              <ChevronRight className={`size-4 text-muted-foreground motion-safe:transition-transform ${shut ? "" : "rotate-90"}`} />
              <AgentMark provider={provider} className="size-4" />
              <h2 className="text-intro font-bold">{title(provider)}</h2>
              <span className="text-micro text-muted-foreground">
                {views.length}
                {shut && fullest !== null ? `, fullest meter ${pct(fullest)}` : ""}
              </span>
              {shut && failures > 0 ? <span className="text-micro text-destructive">{failures} poll{failures === 1 ? "" : "s"} failed</span> : null}
            </summary>
            {/* Columns are in rem, so enlarged text widens the table and it scrolls here instead of clipping its cells. */}
            <div className="relative overflow-x-auto">
            <table className="w-full min-w-[56rem] table-fixed text-data" aria-label={`${title(provider)} accounts`}>
              <thead>
                <tr className="text-left text-micro text-muted-foreground [&>th]:py-1.5 [&>th]:pr-5 [&>th]:font-normal">
                  <th scope="col" className="w-[18%] pl-2">Account</th>
                  <th scope="col" className="w-[13%]">Tier</th>
                  {labels.map((label) => <th key={label} scope="col" className="truncate" title={label}>{label}</th>)}
                  {labels.length === 0 ? <th scope="col">Usage</th> : null}
                  <th scope="col" className="w-[12%]">Resets</th>
                  <th scope="col" className="w-[13%]">Ladder</th>
                  <th scope="col" className="w-[76px]"><span className="sr-only">Actions</span></th>
                </tr>
              </thead>
              <tbody>
                {views.map((view) => (
                  <BoardRow
                    key={view.account.id}
                    view={view}
                    labels={labels}
                    warningMs={warningMs}
                    busy={busy}
                    polling={busy === `poll:${view.account.id}` || (busy === "poll" && !polled.has(view.account.id))}
                    onSelect={() => onSelect(view.account.id)}
                    onEdit={() => onEdit(view.account.id)}
                    onPoll={() => onPoll(view.account.id)}
                  />
                ))}
              </tbody>
            </table>
            </div>
          </details>
        );
      })}
      <NextSteps advice={advice} nameOf={(id) => accounts.find((v) => v.account.id === id)?.account.name ?? ""} onSelect={onSelect} />
    </div>
  );
}

/** The DOM id of an account's row button, so focus can come back to it. */
function rowId(accountId: string): string {
  return `row-${accountId}`;
}

function BoardRow({
  view, labels, warningMs, busy, polling, onSelect, onEdit, onPoll,
}: {
  view: AccountView;
  labels: string[];
  warningMs: number;
  busy: string;
  /** A poll of this account is in flight. */
  polling: boolean;
  onSelect: () => void;
  onEdit: () => void;
  onPoll: () => void;
}) {
  const reading = view.reading;
  const threshold = view.threshold;
  const failed = reading !== undefined && !reading.ok;
  return (
    <tr className="cursor-pointer border-t hover:bg-secondary/50 focus-within:bg-secondary/50 [&>*]:py-2 [&>*]:pr-5 [&>*]:align-top" onClick={onSelect}>
      <th scope="row" className="pl-2 text-left font-normal">
        {/* The click bubbles to the row; the button is there for the keyboard. */}
        <button
          id={rowId(view.account.id)}
          type="button"
          aria-label={`Open ${view.account.name}`}
          className="flex max-w-full items-center gap-1.5 rounded-chip py-0.5 font-semibold outline-none focus-visible:outline-2 focus-visible:outline-solid focus-visible:outline-offset-2 focus-visible:outline-ring"
          onClick={(e) => { e.stopPropagation(); onSelect(); }}
          onKeyDown={(e) => {
            if (e.key !== "Enter" && e.key !== " ") return;
            e.preventDefault();
            onSelect();
          }}
        >
          <AgentMark provider={view.account.provider} className="size-3.5" />
          <span className="truncate" title={view.account.name}>{view.account.name}</span>
        </button>
        <span
          className={`block text-micro ${failed ? "line-clamp-2 break-words" : "truncate"} ${failed ? "text-destructive" : reading?.stale ? "text-st-warn" : "text-muted-foreground"}`}
          title={failed ? reading.error : reading ? `polled ${when(reading.polledAt)}` : undefined}
        >
          {failed ? `poll failed${reading.stale ? ", old numbers" : ""}${reading.error ? `: ${reading.error}` : ""}` : reading ? `polled ${relative(reading.polledAt)}` : "never polled"}
        </span>
      </th>
      <td>
        <span className="flex items-end gap-2">
          <MiniLadder view={view} />
          <span className="truncate" title={planOf(view)}>{planOf(view)}</span>
        </span>
      </td>
      {labels.map((label) => {
        const w = reading?.windows.find((x) => x.label === label);
        return (
          <td key={label}>
            <span className="block pt-1.5"><Meter label={label} util={w?.util} threshold={threshold} stale={reading?.stale} /></span>
            <span className="mt-1 flex justify-between gap-2 text-micro">
              <span className="font-semibold">{pct(w?.util)}</span>
              <span className="truncate text-muted-foreground" title={w?.resetsAt ? when(w.resetsAt) : undefined}>{relative(w?.resetsAt)}</span>
            </span>
          </td>
        );
      })}
      {labels.length === 0 ? <td className="text-muted-foreground">no meters reported</td> : null}
      <td><ResetsCell reading={reading} warningMs={warningMs} /></td>
      <td>
        {view.next ? (
          <Badge className="max-w-full" title={view.next.reason}><span className="truncate">{view.next.reset ? "reset is due" : `${labelOf(view.tiers, view.next.to)} is due`}</span></Badge>
        ) : (
          <Badge variant="secondary" className="max-w-full" title={PHASE_HELP[view.ladder.phase]}><span className="truncate">{phaseLabel(view.ladder.phase)}</span></Badge>
        )}
        {view.account.renewalAt ? (
          <span className="mt-0.5 block truncate text-micro text-muted-foreground" title={renewalTitle(view.account)}>renews {view.account.renewalEstimated ? "about " : ""}{shortDay(view.account.renewalAt)}</span>
        ) : null}
      </td>
      {/* The row opens the account; these two act in place. */}
      <td className="pr-1! text-right whitespace-nowrap" onClick={(e) => e.stopPropagation()}>
        <Button variant="ghost" size="icon-sm" aria-label={`Poll ${view.account.name}`} aria-busy={polling || undefined} title="Poll" disabled={!!busy} onClick={onPoll}>
          <RefreshCw className={polling ? "motion-safe:animate-spin motion-reduce:text-accent" : ""} />
        </Button>
        <Button variant="ghost" size="icon-sm" aria-label={`Edit ${view.account.name}`} title="Edit" onClick={onEdit}>
          <Pencil />
        </Button>
      </td>
    </tr>
  );
}

/** The staircase at table size: the current rank filled, the next one outlined. */
function MiniLadder({ view }: { view: AccountView }) {
  const { ranks, currentRank, nextRank } = rungs(view);
  return (
    <span className="flex shrink-0 items-end gap-0.5" aria-hidden="true">
      {ranks.map((rank, i) => (
        <span
          key={rank}
          className={`w-1.5 rounded-hair ${rank === currentRank ? "bg-primary" : rank === nextRank ? "border border-primary" : "bg-border-strong"}`}
          style={{ height: MINI_STEP_BASE_PX + i * MINI_STEP_RISE_PX }}
        />
      ))}
    </span>
  );
}

/** A board cell: this account's banked resets, or n/a when its provider reports none. */
function ResetsCell({ reading, warningMs }: { reading: Reading | undefined; warningMs: number }) {
  const c = reading?.credits;
  if (!c || c.resetCount === undefined || c.resetCount === null) {
    return <span className="text-muted-foreground" title="This provider's usage payload reports no reset credits">n/a</span>;
  }
  const soon = expiringSoon(reading, warningMs);
  const usable = c.resetApplies !== undefined && c.resetApplies !== null && c.resetApplies !== c.resetCount;
  return (
    <>
      <span className="font-semibold">{c.resetCount}</span>
      {usable ? <span className="text-micro text-muted-foreground"> ({c.resetApplies} usable)</span> : null}
      {soon.length > 0 ? (
        <span className="mt-0.5 flex items-center gap-1 text-micro text-st-warn" title={soon.map((r) => `${r.title || r.type}: ${when(r.expiresAt)}`).join("\n")}>
          <TriangleAlert className="size-3.5 shrink-0" role="img" aria-label="Expiring soon" />
          <span className="truncate">{soon.length === 1 ? "expires" : `${soon.length} expire`} {relative(soon[0]?.expiresAt)}</span>
        </span>
      ) : null}
    </>
  );
}
