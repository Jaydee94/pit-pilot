/// <reference types="vitest/config" />
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import { VitePWA } from "vite-plugin-pwa";

export default defineConfig({
  plugins: [react(), VitePWA({ registerType: "autoUpdate" })],
  server: { proxy: { "/api": "http://localhost:8080", "/auth": "http://localhost:8080" } },
  test: { environment: "jsdom", globals: true, setupFiles: "./src/setupTests.ts" },
});
