import { render } from "preact";
import { LucideProvider } from "lucide-preact";
import "./index.css";
import { App } from "./App";

const root = document.getElementById("root");
if (!root) {
  throw new Error("missing #root");
}
// Lucide ships a 2px stroke that thins as the icon shrinks. The brand token
// is 1.7px at every rendered size, so the width is absolute.
render(
  <LucideProvider strokeWidth={1.7} absoluteStrokeWidth>
    <App />
  </LucideProvider>,
  root,
);
