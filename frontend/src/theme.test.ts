import { expect, test } from "bun:test";

// main.go paints the frame before the page loads. If the two drift apart the
// window flashes a different colour on open.
test("the window frame is painted in the page background of each theme", async () => {
  const css = await Bun.file(`${import.meta.dir}/system/tokens.css`).text();
  const go = await Bun.file(`${import.meta.dir}/../../main.go`).text();
  const bg = css.match(/--color-bg: light-dark\(#([0-9a-f]{6}), #([0-9a-f]{6})\)/);
  expect(bg).not.toBeNull();
  const hex = (h: string) => [0, 2, 4].map((i) => parseInt(h.slice(i, i + 2), 16));
  const frame = [...go.matchAll(/RGBA\{R: (\d+), G: (\d+), B: (\d+)/g)].map((m) => [Number(m[1]), Number(m[2]), Number(m[3])]);
  // tokens.css states light then dark; main.go returns dark first.
  expect(frame).toEqual([hex(bg![2]), hex(bg![1])]);
});
