import type { Price } from "./types";

/** A monthly price as the locale writes that currency, such as "SGD 10.98/month". */
export function perMonth(p: Price): string {
  return `${new Intl.NumberFormat(undefined, { style: "currency", currency: p.currency, currencyDisplay: "code" }).format(p.amount)}/month`;
}

/** A price as the Edit field shows it: "10.98 SGD". Empty for none. */
export function priceText(p: Price | undefined): string {
  return p ? `${p.amount} ${p.currency}` : "";
}

/**
 * The Edit field's text as a price: an amount and a three-letter currency
 * code in either order, such as "10.98 SGD". Null for an empty field, and
 * undefined for text that is not a price. The same rule as the CLI's --price.
 */
export function parsePrice(text: string): Price | null | undefined {
  const fields = text.trim().split(/\s+/).filter(Boolean);
  if (fields.length === 0) return null;
  if (fields.length !== 2) return undefined;
  const [a, b] = fields;
  const [amount, currency] = /^[A-Za-z]{3}$/.test(a) ? [b, a] : [a, b];
  const n = Number(amount);
  const code = currency.toUpperCase();
  if (!/^\d+(\.\d+)?$/.test(amount) || !(n < 1e9) || !/^[A-Z]{3}$/.test(code)) return undefined;
  return { amount: n, currency: code };
}

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
 * midnight of that day. A pasted instant (the RFC 3339 text the account
 * stores, or the same day with a time) keeps its calendar day, so the field
 * does not reject a value copied from the account. Undefined for an empty
 * string and for a day that does not exist, such as 2026-02-31.
 */
export function fromDay(value: string): string | undefined {
  const typed = value.trim();
  if (!typed) {
    return undefined;
  }
  const dateOnly = /^\d{4}-\d{2}-\d{2}$/.test(typed);
  // A bare day is local midnight. A time with no offset is local too, and one
  // with an offset is absolute. Each is checked in the calendar it was read
  // in: the UTC day of a local time is the day before or after wherever the
  // zone is not UTC, so comparing the two rejects a real day or a pasted time.
  const t = parse(dateOnly ? `${typed}T00:00:00` : typed);
  if (!t) {
    return undefined;
  }
  const absolute = /(?:Z|[+-]\d{2}:?\d{2})$/.test(typed);
  const read = absolute ? t.toISOString().slice(0, 10) : day(`${typed.slice(0, 10)}T00:00:00`);
  if (read !== typed.slice(0, 10)) {
    return undefined;
  }
  return dateOnly ? t.toISOString() : typed;
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
export const WARN_MARGIN = 20;

/** The CSS colour for a utilization against the account's spend threshold. */
export function tone(util: number, threshold: number): string {
  if (util >= threshold) {
    return "var(--color-st-fail)";
  }
  if (util >= threshold - WARN_MARGIN) {
    return "var(--color-st-warn)";
  }
  return "var(--color-accent)";
}

export function num(value: number | undefined): string {
  if (value === undefined || Number.isNaN(value)) {
    return "n/a";
  }
  return Number.isInteger(value) ? String(value) : value.toFixed(2);
}
