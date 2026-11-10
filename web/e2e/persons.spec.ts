import { test, expect } from "./fixtures";

test("without a token the sign-in page is shown", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "Sign in" })).toBeVisible();
  // The test server has no sis.adapter.inbound.authapi.issuer configured.
  await expect(page.getByText("Sign-in is not configured for this SIS.")).toBeVisible();
  await expect(page.getByRole("button", { name: "Sign in" })).toBeDisabled();
});

test("a writer creates, edits and deletes a person", async ({ page, signIn }) => {
  await signIn("writer");
  await page.goto("/");
  await expect(page.getByText("read/write")).toBeVisible();

  // Create opens the new student's page.
  await page.getByRole("button", { name: "New person" }).click();
  await page.getByLabel("Given name").fill("Ada");
  await page.getByLabel("Family name").fill("Lovelace");
  await page.getByRole("button", { name: "Create" }).click();
  await expect(page.getByRole("heading", { name: /Ada Lovelace/ })).toBeVisible();

  await page.getByRole("button", { name: "Edit", exact: true }).click();
  await page.getByLabel("Given name").fill("Augusta");
  await page.getByRole("button", { name: "Save" }).click();
  await expect(page.getByRole("heading", { name: /Augusta Lovelace/ })).toBeVisible();

  await page.getByRole("link", { name: /All people/ }).click();
  await expect(page.getByRole("heading", { name: /People \(1\)/ })).toBeVisible();
  await expect(page.getByRole("row", { name: /Augusta Lovelace/ })).toBeVisible();

  await page.getByRole("link", { name: /Augusta Lovelace/ }).click();
  page.once("dialog", (dialog) => void dialog.accept());
  await page.getByRole("button", { name: "Delete", exact: true }).click();
  await expect(page.getByText("No people yet.")).toBeVisible();
  await expect(page.getByRole("row", { name: /Augusta Lovelace/ })).toHaveCount(0);
});

test("a reader sees the data but cannot write", async ({ page, signIn, request, tokens }) => {
  // A person made through the API, so the reader has something to read.
  const created = await request.post("/api/v1/persons", {
    headers: { Authorization: `Bearer ${tokens.writer}` },
    data: { givenName: "Grace", familyName: "Hopper", status: "active" },
  });
  expect(created.status()).toBe(201);

  await signIn("reader");
  await page.goto("/");
  await expect(page.getByText("read only")).toBeVisible();
  await expect(page.getByRole("row", { name: /Grace Hopper/ })).toBeVisible();

  // The UI offers no write actions, on the list or on the student page.
  await expect(page.getByRole("button", { name: "New person" })).toHaveCount(0);
  await page.getByRole("link", { name: /Grace Hopper/ }).click();
  await expect(page.getByRole("heading", { name: /Grace Hopper/ })).toBeVisible();
  await expect(page.getByRole("button", { name: "Edit", exact: true })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Delete", exact: true })).toHaveCount(0);

  // And SIS rejects the write itself.
  const rejected = await request.post("/api/v1/persons", {
    headers: { Authorization: `Bearer ${tokens.reader}` },
    data: { givenName: "Eve", familyName: "Intruder", status: "active" },
  });
  expect(rejected.status()).toBe(403);
});

test("a search keeps + and & from the link", async ({ page, signIn }) => {
  await signIn("reader");
  await page.goto("/#/students?q=anna%2Bstudent%40liu.se%20R%26D");
  await expect(page.getByRole("searchbox", { name: "Search people" })).toHaveValue("anna+student@liu.se R&D");
  // A link rewriter may encode the route delimiter itself.
  await page.goto("/#%2Fstudents%3Fq%3Dgrace");
  await expect(page.getByRole("searchbox", { name: "Search people" })).toHaveValue("grace");
});

test("log out drops the token and returns to sign-in", async ({ page, signIn }) => {
  await signIn("writer");
  await page.goto("/");
  await page.getByRole("button", { name: "Log out" }).click();
  await expect(page.getByRole("heading", { name: "Sign in" })).toBeVisible();
  expect(await page.evaluate(() => localStorage.getItem("sis_token"))).toBeNull();
});
