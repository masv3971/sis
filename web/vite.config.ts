import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// Dev server proxies API calls to the Go service so the SPA and API share an origin.
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      // SIS serves https with the dev certificate (make pki). The proxy
      // verifies it: `npm run dev` sets NODE_EXTRA_CA_CERTS to the dev CA.
      "/api": "https://localhost:8080",
      "/auth": "https://localhost:8080",
      "/oeapi": "https://localhost:8080",
      "/eduapi": "https://localhost:8080",
      "/swagger": "https://localhost:8080",
    },
  },
});
