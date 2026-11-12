import { expect, test } from "bun:test";

/** oklch to 8-bit sRGB, the conversion the webview does for --background. */
function rgb(L: number, C: number, hueDeg: number): number[] {
  const h = (hueDeg * Math.PI) / 180;
  const a = C * Math.cos(h);
  const b = C * Math.sin(h);
  const l = (L + 0.3963377774 * a + 0.2158037573 * b) ** 3;
  const m = (L - 0.1055613458 * a - 0.0638541728 * b) ** 3;
  const s = (L - 0.0894841775 * a - 1.291485548 * b) ** 3;
  const linear = [
    4.0767416621 * l - 3.3077115913 * m + 0.2309699292 * s,
    -1.2684380046 * l + 2.6097574011 * m - 0.3413193965 * s,
    -0.0041960863 * l - 0.7034186147 * m + 1.707614701 * s,
  ];
  return linear.map((v) => Math.round(255 * (v <= 0.0031308 ? 12.92 * v : 1.055 * v ** (1 / 2.4) - 0.055)));
}

// main.go paints the frame before the page loads. If the two drift apart the
// window flashes a different colour on open.
test("the window frame is painted in the page background of each theme", async () => {
  const css = await Bun.file(`${import.meta.dir}/index.css`).text();
  const go = await Bun.file(`${import.meta.dir}/../../main.go`).text();
  const page = [...css.matchAll(/--background: oklch\(([\d.]+) ([\d.]+) ([\d.]+)\)/g)].map((m) => rgb(Number(m[1]), Number(m[2]), Number(m[3])));
  const frame = [...go.matchAll(/RGBA\{R: (\d+), G: (\d+), B: (\d+)/g)].map((m) => [Number(m[1]), Number(m[2]), Number(m[3])]);
  expect(page).toHaveLength(2);
  // index.css states light then dark; main.go returns dark first.
  expect(frame).toEqual([page[1], page[0]]);
});
