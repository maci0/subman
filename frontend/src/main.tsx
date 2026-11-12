import { render } from "preact";
import "./index.css";
import { App } from "./App";

const root = document.getElementById("root");
if (!root) {
  throw new Error("missing #root");
}
render(<App />, root);
