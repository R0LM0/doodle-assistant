import { defineConfig } from "vite";

// The API is served by the Go binary on :8080; in dev, Vite proxies to it so the
// UI can run on :5173 with hot reload and no CORS.
export default defineConfig({
  server: {
    port: 5173,
    proxy: {
      "/api": "http://localhost:8080",
    },
  },
  build: {
    outDir: "dist",
    emptyOutDir: true,
  },
});
