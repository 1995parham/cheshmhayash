import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// Vite serves the SPA in dev and produces the static bundle Go serves in
// prod. The dev server proxies API calls to the running Go backend so the
// usual `npm run dev` workflow needs only the backend on :1378.
export default defineConfig({
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
});
