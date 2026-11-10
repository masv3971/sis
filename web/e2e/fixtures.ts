import { readFileSync } from "node:fs";
import { test as base, expect } from "@playwright/test";
import { tokensFile } from "../playwright.config";

interface Tokens {
  reader: string;
  writer: string;
}

// The server writes the file right after it starts listening.
async function readTokens(): Promise<Tokens> {
  let tokens: Tokens | undefined;
  await expect
    .poll(() => {
      try {
        tokens = JSON.parse(readFileSync(tokensFile, "utf8")) as Tokens;
      } catch {
        tokens = undefined;
      }
      return tokens !== undefined;
    })
    .toBe(true);
  return tokens!;
}

export const test = base.extend<{
  tokens: Tokens;
  // signIn stores a token the way a finished sign-in does, before the app loads.
  signIn: (role: keyof Tokens) => Promise<void>;
}>({
  tokens: async ({}, use) => {
    await use(await readTokens());
  },
  signIn: async ({ page, tokens }, use) => {
    await use(async (role) => {
      await page.addInitScript((token) => localStorage.setItem("sis_token", token), tokens[role]);
    });
  },
});

export { expect };
