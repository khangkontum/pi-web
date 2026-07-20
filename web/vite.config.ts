import { writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { defineConfig, type Plugin } from "vite";
import { svelte } from "@sveltejs/vite-plugin-svelte";

const OUT_DIR = "../internal/piweb/ui/dist";

// emptyOutDir wipes the committed dist/.gitkeep on every build; restore it so
// the go:embed target keeps its placeholder and git stays clean.
function keepEmbedPlaceholder(): Plugin {
  return {
    name: "keep-embed-placeholder",
    closeBundle() {
      const gitkeep = fileURLToPath(new URL(`${OUT_DIR}/.gitkeep`, import.meta.url));
      writeFileSync(gitkeep, "");
    },
  };
}

// The built SPA is embedded into the Go binary via go:embed, so it must land
// in internal/piweb/ui/dist. Dev mode proxies the API to a locally running Go
// server (default 127.0.0.1:9999).
export default defineConfig({
  plugins: [svelte(), keepEmbedPlaceholder()],
  build: {
    outDir: OUT_DIR,
    emptyOutDir: true,
  },
  server: {
    proxy: {
      "/api": "http://127.0.0.1:9999",
      "/version": "http://127.0.0.1:9999",
    },
  },
  test: {
    environment: "node",
    include: ["src/**/*.test.ts"],
  },
});
