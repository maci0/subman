import { expect, test } from "bun:test";
import { day, fromDay, num, parsePrice, perMonth, pct, priceText, relative, tone } from "./format";

const NOW = Date.parse("2026-11-11T12:00:00Z");

test("relative counts forward and back from now", () => {
  expect(relative("2026-11-11T12:59:00Z", NOW)).toBe("in 59m");
  expect(relative("2026-11-11T13:24:00Z", NOW)).toBe("in 1h 24m");
  expect(relative("2026-11-14T20:00:00Z", NOW)).toBe("in 3d 8h");
  expect(relative("2026-11-11T11:56:00Z", NOW)).toBe("4m ago");
  expect(relative(undefined, NOW)).toBe("");
  expect(relative("not a time", NOW)).toBe("");
});

test("a date input value round-trips through the RFC 3339 form", () => {
  const iso = fromDay("2026-11-20");
  expect(iso).toMatch(/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$/);
  expect(day(iso)).toBe("2026-11-20");
  expect(fromDay("")).toBeUndefined();
  expect(fromDay("  ")).toBeUndefined();
  expect(fromDay("2026-02-31")).toBeUndefined();
  expect(fromDay("2026-13-01")).toBeUndefined();
  expect(fromDay("tomorrow")).toBeUndefined();
  // A date copied from the account arrives with its time and must not be refused.
  expect(fromDay("2026-11-20T00:00:00.000Z")).toBe("2026-11-20T00:00:00.000Z");
  expect(fromDay("2026-11-20T18:30:00Z")).toBe("2026-11-20T18:30:00Z");
  expect(fromDay("2026-11-20T18:30:00")).toBe("2026-11-20T18:30:00");
  expect(fromDay("2026-02-31T00:00:00Z")).toBeUndefined();
});

// A meter the payload did not carry is absent. Rounding it, or printing it as
// 0%, draws a healthy empty meter over a limit the provider never reported.
test("a missing number stays n/a and a present one is exact", () => {
  expect(pct(undefined)).toBe("n/a");
  expect(pct(null)).toBe("n/a");
  expect(pct(Number.NaN)).toBe("n/a");
  expect(pct(0)).toBe("0%");
  expect(pct(12.5)).toBe("13%");
  expect(pct(99.4)).toBe("99%");
  expect(num(undefined)).toBe("n/a");
  expect(num(Number.NaN)).toBe("n/a");
  expect(num(0)).toBe("0");
  expect(num(20.06)).toBe("20.06");
  expect(num(3)).toBe("3");
});

test("tone follows the account threshold", () => {
  expect(tone(95, 95)).toBe("var(--color-st-fail)");
  expect(tone(75, 95)).toBe("var(--color-st-warn)");
  expect(tone(74, 95)).toBe("var(--color-accent)");
  expect(tone(60, 60)).toBe("var(--color-st-fail)");
});

test("a typed price is an amount and a currency code in either order", () => {
  expect(parsePrice("10.98 SGD")).toEqual({ amount: 10.98, currency: "SGD" });
  expect(parsePrice(" usd 30 ")).toEqual({ amount: 30, currency: "USD" });
  expect(parsePrice("")).toBeNull();
  for (const bad of ["10.98", "-1 USD", "1e3 USD", "10 dollars", "1 2 USD", "NaN USD"]) {
    expect(parsePrice(bad)).toBeUndefined();
  }
  expect(parsePrice(priceText({ amount: 10.98, currency: "SGD" }))).toEqual({ amount: 10.98, currency: "SGD" });
  expect(perMonth({ amount: 690, currency: "SGD" })).toMatch(/^SGD\s?690\.00\/month$/);
});
