export function pct(value: number | null | undefined): string {
  if (value === null || value === undefined || Number.isNaN(value)) {
    return "n/a";
  }
  return `${Math.round(value)}%`;
}

function parse(iso: string | undefined): Date | null {
  if (!iso) {
    return null;
  }
  const t = new Date(iso);
  return Number.isNaN(t.getTime()) ? null : t;
}

export function when(iso: string | undefined): string {
  const t = parse(iso);
  if (!t) {
    return "n/a";
  }
  return t.toLocaleString(undefined, { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" });
}

/** Local calendar day as YYYY-MM-DD, the value a date input takes. */
export function day(iso: string | undefined): string {
  const t = parse(iso);
  if (!t) {
    return "";
  }
  const m = String(t.getMonth() + 1).padStart(2, "0");
  const d = String(t.getDate()).padStart(2, "0");
  return `${t.getFullYear()}-${m}-${d}`;
}

/**
 * A YYYY-MM-DD day as the RFC 3339 instant Go's time.Time decodes: local
 * midnight of that day. Undefined for an empty string and for a day that
 * does not exist, such as 2026-02-31.
 */
export function fromDay(value: string): string | undefined {
  const t = parse(value ? `${value}T00:00:00` : undefined);
  if (!t) {
    return undefined;
  }
  const iso = t.toISOString();
  return day(iso) === value ? iso : undefined;
}

export function shortDay(iso: string | undefined): string {
  const t = parse(iso);
  return t ? t.toLocaleDateString(undefined, { month: "short", day: "numeric" }) : "n/a";
}

const MINUTE_MS = 60_000;
const HOUR_MIN = 60;
const DAY_HOURS = 24;
/** Below this many hours a span reads "h m"; from it on, "d h". */
const DAYS_FROM_HOURS = 48;

function span(ms: number): string {
  const minutes = Math.round(ms / MINUTE_MS);
  if (minutes < HOUR_MIN) {
    return `${minutes}m`;
  }
  const hours = Math.floor(minutes / HOUR_MIN);
  if (hours < DAYS_FROM_HOURS) {
    return `${hours}h ${minutes % HOUR_MIN}m`;
  }
  return `${Math.floor(hours / DAY_HOURS)}d ${hours % DAY_HOURS}h`;
}

/** "in 1h 24m" or "4m ago", measured from now. Empty when there is no time. */
export function relative(iso: string | undefined, now: number = Date.now()): string {
  const t = parse(iso);
  if (!t) {
    return "";
  }
  const delta = t.getTime() - now;
  return delta >= 0 ? `in ${span(delta)}` : `${span(-delta)} ago`;
}

/** Points below the spend threshold at which a meter turns to the warning colour. */
const WARN_MARGIN = 20;

/** The CSS colour for a utilization against the account's spend threshold. */
export function tone(util: number, threshold: number): string {
  if (util >= threshold) {
    return "var(--destructive)";
  }
  if (util >= threshold - WARN_MARGIN) {
    return "var(--warn-fill)";
  }
  return "var(--primary)";
}

export function num(value: number | undefined): string {
  if (value === undefined || Number.isNaN(value)) {
    return "n/a";
  }
  return Number.isInteger(value) ? String(value) : value.toFixed(2);
}
