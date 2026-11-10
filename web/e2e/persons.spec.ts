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

  await page.getByLabel("Given name").fill("Ada");
  await page.getByLabel("Family name").fill("Lovelace");
  await page.getByRole("button", { name: "Create" }).click();
  const row = page.getByRole("row", { name: /Ada Lovelace/ });
  await expect(row).toBeVisible();
  await expect(page.getByRole("heading", { name: /People \(1\)/ })).toBeVisible();

  await row.getByRole("button", { name: "Edit" }).click();
  await expect(page.getByRole("heading", { name: "Edit person" })).toBeVisible();
  await page.getByLabel("Given name").fill("Augusta");
  await page.getByRole("button", { name: "Save" }).click();
  await expect(page.getByRole("row", { name: /Augusta Lovelace/ })).toBeVisible();
  await expect(page.getByRole("row", { name: /Ada Lovelace/ })).toHaveCount(0);

  await page.getByRole("row", { name: /Augusta Lovelace/ }).getByRole("button", { name: "Delete" }).click();
  await expect(page.getByRole("row", { name: /Augusta Lovelace/ })).toHaveCount(0);
  await expect(page.getByText("No people yet.")).toBeVisible();
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

  await page.getByLabel("Given name").fill("Eve");
  await page.getByLabel("Family name").fill("Intruder");
  await page.getByRole("button", { name: "Create" }).click();
  await expect(page.getByRole("alert")).toContainText("sis.write");
  await expect(page.getByRole("row", { name: /Eve Intruder/ })).toHaveCount(0);
});

test("log out drops the token and returns to sign-in", async ({ page, signIn }) => {
  await signIn("writer");
  await page.goto("/");
  await page.getByRole("button", { name: "Log out" }).click();
  await expect(page.getByRole("heading", { name: "Sign in" })).toBeVisible();
  expect(await page.evaluate(() => localStorage.getItem("sis_token"))).toBeNull();
});
