/// <reference types="vitest/config" />
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import { VitePWA } from "vite-plugin-pwa";

export default defineConfig({
  plugins: [
    react(),
    VitePWA({
      strategies: "injectManifest",
      srcDir: "src",
      filename: "sw.ts",
      registerType: "autoUpdate",
      injectManifest: { globPatterns: ["**/*.{js,css,html,svg,png,ico}"] },
    }),
  ],
  server: { proxy: { "/api": "http://localhost:8080", "/auth": "http://localhost:8080" } },
  test: { environment: "jsdom", globals: true, setupFiles: "./src/setupTests.ts" },
});
