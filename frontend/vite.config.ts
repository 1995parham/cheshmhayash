import { cpSync, existsSync, readFileSync } from "node:fs";
import { join, resolve } from "node:path";
import react from "@vitejs/plugin-react";
import { defineConfig, type Plugin } from "vite";

// The demo data is a recorded, renamed snapshot of the API. The demo build
// copies it next to the bundle as data/; the demo dev server serves it.
const demoData = resolve(import.meta.dirname, "demo/data");

function demo(): Plugin {
  let outDir = "";
  return {
    name: "cheshmhayash-demo-data",
    configResolved(c) {
      outDir = resolve(c.root, c.build.outDir);
    },
    configureServer(server) {
      server.middlewares.use((req, res, next) => {
        const base = server.config.base;
        const url = decodeURIComponent((req.url ?? "").split("?")[0]);
        if (!url.startsWith(`${base}data/`)) return next();
        const file = join(demoData, url.slice(base.length + 5));
        if (!file.startsWith(demoData) || !existsSync(file)) return next();
        res.setHeader("Content-Type", "application/json");
        res.end(readFileSync(file));
      });
    },
    closeBundle() {
      cpSync(demoData, join(outDir, "data"), { recursive: true });
    },
  };
}

// Vite serves the SPA in dev and produces the static bundle Go serves in
// prod. The dev server proxies API calls to the running Go backend so the
// usual `npm run dev` workflow needs only the backend on :1378.
//
// `--mode demo` builds the backend-less demo for the docs site into
// dist-demo/ under DEMO_BASE (default /cheshmhayash/demo/).
export default defineConfig(({ mode }) =>
  mode === "demo"
    ? {
        base: process.env.DEMO_BASE ?? "/cheshmhayash/demo/",
        plugins: [react(), demo()],
        build: { outDir: "dist-demo", emptyOutDir: true },
      }
    : {
        plugins: [react()],
        build: {
          outDir: "dist",
          emptyOutDir: true,
        },
        server: {
          port: 5173,
          proxy: {
            "/api": "http://127.0.0.1:1378",
            "/healthz": "http://127.0.0.1:1378",
          },
        },
      },
);
