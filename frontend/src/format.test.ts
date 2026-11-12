import { expect, test } from "bun:test";
import { day, fromDay, relative, tone } from "./format";

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
  expect(fromDay("2026-02-31")).toBeUndefined();
  expect(fromDay("2026-13-01")).toBeUndefined();
  expect(fromDay("tomorrow")).toBeUndefined();
});

test("tone follows the account threshold", () => {
  expect(tone(95, 95)).toBe("var(--color-st-fail)");
  expect(tone(75, 95)).toBe("var(--color-st-warn)");
  expect(tone(74, 95)).toBe("var(--color-accent)");
  expect(tone(60, 60)).toBe("var(--color-st-fail)");
});
