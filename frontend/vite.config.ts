import { execFileSync } from "node:child_process";
import { mkdirSync, writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { defineConfig, type Plugin } from "vite";
import preact from "@preact/preset-vite";
import tailwindcss from "@tailwindcss/vite";

// frontend/dist/index.html is tracked so the root package compiles on a clean
// clone: main.go embeds all:frontend/dist, and that pattern does not compile
// when the directory is missing. The built page is gitignored. Vite empties
// the output directory before writing, so restore the committed placeholder
// afterwards unless the build already wrote the real page.
function keepEmbedDir(): Plugin {
  return {
    name: "keep-embed-dir",
    closeBundle() {
      mkdirSync("dist", { recursive: true });
      let placeholder: Buffer;
      try {
        placeholder = execFileSync("git", ["show", "HEAD:frontend/dist/index.html"], { stdio: ["ignore", "pipe", "ignore"] });
      } catch {
        return;
      }
      try {
        writeFileSync("dist/index.html", placeholder, { flag: "wx" });
      } catch (err) {
        if ((err as NodeJS.ErrnoException).code !== "EEXIST") throw err;
      }
    },
  };
}

export default defineConfig({
  plugins: [preact(), tailwindcss(), keepEmbedDir()],
  resolve: { alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) } },
});
