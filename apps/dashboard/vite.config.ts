import path from "path"
import tailwindcss from "@tailwindcss/vite"
import react from "@vitejs/plugin-react"
import { tanstackRouter } from "@tanstack/router-plugin/vite"
import { defineConfig } from "vitest/config"

// https://vite.dev/config/
export default defineConfig({
  plugins: [
    tanstackRouter({
      target: "react",
      autoCodeSplitting: true,
      routesDirectory: "./src/routes",
      generatedRouteTree: "./src/routeTree.gen.ts",
      routeFileIgnorePattern: "\\.test\\.",
    }),
    react(),
    tailwindcss(),
  ],
  server: {
    host: "127.0.0.1",
    port: 5173,
    strictPort: true,
    // No `ws` override: the Vite client derives protocol/host/port from the
    // page URL, so HMR works both via `vite dev` (:5173) and via the gateway
    // proxy (https://127.0.0.1:9090 → TAKO_VITE_URL) with wss through TLS.
    proxy: {
      "/api": {
        target: "http://127.0.0.1:9090",
        ws: true,
      },
    },
  },
  build: {
    outDir: "../backend/internal/dashboard/dist",
    emptyOutDir: true,
  },
  resolve: {
    alias: {
      "@": path.resolve(import.meta.dirname, "./src"),
    },
  },
  test: {
    environment: "jsdom",
    setupFiles: "./src/test/setup.ts",
  },
})
