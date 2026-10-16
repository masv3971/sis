import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// Dev server proxies API calls to the Go service so the SPA and API share an origin.
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      "/api": "http://localhost:8080",
      "/auth": "http://localhost:8080",
      "/oauth": "http://localhost:8080",
      "/oeapi": "http://localhost:8080",
      "/eduapi": "http://localhost:8080",
      "/swagger": "http://localhost:8080",
    },
  },
});
