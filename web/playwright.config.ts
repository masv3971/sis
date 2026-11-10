import { tmpdir } from "node:os";
import { join } from "node:path";
import { defineConfig } from "@playwright/test";

const addr = "127.0.0.1:18080";

// TestServeE2E writes the tokens it mints here; the tests read them.
export const tokensFile = join(tmpdir(), "sis-e2e-tokens.json");

// The tests run against the real SIS (in-memory catalog, local token issuer)
// serving the built SPA: run `make web-build` first, or `make web-e2e`.
export default defineConfig({
  testDir: "e2e",
  // One server holds the data of every test, so they run in order.
  workers: 1,
  fullyParallel: false,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? [["github"], ["html", { open: "never" }]] : "list",
  use: {
    baseURL: `http://${addr}`,
    trace: "retain-on-failure",
  },
  projects: [{ name: "chromium", use: { browserName: "chromium" } }],
  webServer: {
    command: "go test ./internal/httpserver -run TestServeE2E -count=1 -timeout 0",
    cwd: "..",
    env: { SIS_E2E_ADDR: addr, SIS_E2E_TOKENS: tokensFile },
    url: `http://${addr}/healthz`,
    // go test compiles first.
    timeout: 180_000,
    reuseExistingServer: false,
  },
});
