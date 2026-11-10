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

test("a reference picker loads more records past the fetch cap", async ({ page, signIn }) => {
  // The server hands out one record per page, so 25 records exceed the 20
  // pages a picker fetches at once.
  const total = 25;
  await page.route("**/api/v1/organizations?*", async (route) => {
    const offset = Number(new URL(route.request().url()).searchParams.get("offset") ?? "0");
    const items = offset < total ? [{ id: `org-${offset}`, sourcedId: `org-${offset}`, name: `Org ${offset}`, status: "active" }] : [];
    await route.fulfill({ json: { items, total, limit: 1, offset } });
  });
  await signIn("writer");
  await page.goto("/#/courses");

  const picker = page.getByLabel("Organization");
  await expect(page.getByText("Only the first records are listed.")).toBeVisible();
  await expect(picker.locator("option", { hasText: "Org 19" })).toHaveCount(1);
  await expect(picker.locator("option", { hasText: "Org 24" })).toHaveCount(0);

  await page.getByRole("button", { name: "Load more" }).click();
  await expect(picker.locator("option", { hasText: "Org 24" })).toHaveCount(1);
  await expect(page.getByText("Only the first records are listed.")).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Load more" })).toHaveCount(0);
});

test("two reference pickers can load more at the same time", async ({ page, signIn }) => {
  // One record per page, 25 records each. The last page of both lists is held
  // until both requests are in, so both loads finish together.
  const total = 25;
  let waiting = 0;
  let release!: () => void;
  const gate = new Promise<void>((resolve) => (release = resolve));
  const paged = (path: string, name: string) =>
    page.route(`**/api/v1/${path}?*`, async (route) => {
      const offset = Number(new URL(route.request().url()).searchParams.get("offset") ?? "0");
      if (offset === total - 1) {
        if (++waiting === 2) release();
        await gate;
      }
      const items = offset < total ? [{ id: `${path}-${offset}`, sourcedId: `${path}-${offset}`, [name]: `${path} ${offset}`, status: "active" }] : [];
      await route.fulfill({ json: { items, total, limit: 1, offset } });
    });
  await paged("courses", "title");
  await paged("organizations", "name");
  await signIn("writer");
  await page.goto("/#/offerings");

  // The label also wraps the options and the hint, so match by substring.
  const course = page.getByLabel("Course");
  const organization = page.getByLabel("Organization");
  await expect(page.getByRole("button", { name: "Load more" })).toHaveCount(2);
  await page.getByRole("button", { name: "Load more" }).first().click();
  await page.getByRole("button", { name: "Load more" }).last().click();

  await expect(course.locator("option", { hasText: `courses ${total - 1}` })).toHaveCount(1);
  await expect(organization.locator("option", { hasText: `organizations ${total - 1}` })).toHaveCount(1);
  await expect(page.getByRole("button", { name: "Load more" })).toHaveCount(0);
});

test("Back from a person returns to the same search page", async ({ page, signIn, request, tokens }) => {
  // 30 people: with 25 to a page, the second page holds Fam25 to Fam29.
  for (let i = 0; i < 30; i++) {
    const created = await request.post("/api/v1/persons", {
      headers: { Authorization: `Bearer ${tokens.writer}` },
      data: { givenName: "P", familyName: `Fam${String(i).padStart(2, "0")}`, status: "active" },
    });
    expect(created.status()).toBe(201);
  }
  await signIn("reader");
  await page.goto("/");
  await expect(page.getByText("1–25 of 30")).toBeVisible();

  await page.getByRole("button", { name: "Next" }).click();
  await expect(page.getByText("26–30 of 30")).toBeVisible();
  expect(page.url()).toContain("offset=25");

  await page.getByRole("link", { name: /Fam29/ }).click();
  await expect(page.getByRole("heading", { name: /Fam29/ })).toBeVisible();
  await page.getByRole("link", { name: /All people/ }).click();
  await expect(page.getByText("26–30 of 30")).toBeVisible();

  // The browser's Back button keeps the page too.
  await page.getByRole("link", { name: /Fam29/ }).click();
  await page.goBack();
  await expect(page.getByText("26–30 of 30")).toBeVisible();

  await page.getByRole("button", { name: "Previous" }).click();
  await expect(page.getByText("1–25 of 30")).toBeVisible();
  expect(page.url()).not.toContain("offset");
});

test("the requested link is restored after sign-in", async ({ page, tokens }) => {
  const issuer = "https://issuer.test";
  const cors = { "access-control-allow-origin": "*" };
  await page.route("**/auth/config", (route) =>
    route.fulfill({ json: { enabled: true, issuer, client_id: "sis-web", scope: "openid sis.read" } }),
  );
  await page.route(`${issuer}/.well-known/openid-configuration`, (route) =>
    route.fulfill({
      headers: cors,
      json: { authorization_endpoint: `${issuer}/authorize`, token_endpoint: `${issuer}/token` },
    }),
  );
  // The issuer sends the browser straight back with a code and the state it got.
  await page.route(`${issuer}/authorize?*`, (route) => {
    const state = new URL(route.request().url()).searchParams.get("state") ?? "";
    return route.fulfill({
      status: 302,
      headers: { location: `http://127.0.0.1:18080/?code=abc&state=${encodeURIComponent(state)}` },
    });
  });
  await page.route(`${issuer}/token`, (route) => route.fulfill({ headers: cors, json: { access_token: tokens.reader } }));

  await page.goto("/#/students?q=grace");
  await page.getByRole("button", { name: "Sign in" }).click();

  await expect(page.getByText("read only")).toBeVisible();
  await expect(page.getByRole("searchbox", { name: "Search people" })).toHaveValue("grace");
  expect(new URL(page.url()).search).toBe("");
});

test("log out drops the token and returns to sign-in", async ({ page, signIn }) => {
  await signIn("writer");
  await page.goto("/");
  await page.getByRole("button", { name: "Log out" }).click();
  await expect(page.getByRole("heading", { name: "Sign in" })).toBeVisible();
  expect(await page.evaluate(() => localStorage.getItem("sis_token"))).toBeNull();
});
