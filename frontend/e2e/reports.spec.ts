import { test, expect } from "@playwright/test";
import { existsSync, mkdirSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { audit } from "./axe";

// These use real report files in the run's own data folder, read by the real Go service.
const dir = () => join(process.env.S1S_DATA_DIR!, "reports");
const fileOf = (id: string) => join(dir(), `${id}.json`);

function seed(id: string, over: Record<string, unknown> = {}) {
  mkdirSync(dir(), { recursive: true });
  writeFileSync(
    fileOf(id),
    JSON.stringify({
      version: 1, id,
      scenario: { id: "lost-quorum", name: "Lost Quorum", product: "vault" },
      instructor: "Ada Lovelace", trainee: "Grace Hopper", params: { version: "2.2.2", nodes: 3 },
      startedAt: "2026-11-26T09:15:30Z", endedAt: "2026-11-26T09:57:37Z", result: "ended early",
      steps: [{ id: "break", title: "Cause the outage", kind: "breakage", state: "complete", verified: true,
        startedAt: "2026-11-26T09:16:30Z", endedAt: "2026-11-26T09:24:30Z", durationMs: 480000, attempts: 1,
        notes: "Looked at the logs first.", rating: 4, outcome: { status: "pass", message: "the breakage is in place", at: "2026-11-26T09:24:30Z" } }],
      events: [{ at: "2026-11-26T09:57:37Z", kind: "run-ended", message: "the run ended: ended early" }],
      ...over,
    }),
    { mode: 0o600 },
  );
}

test.describe.configure({ mode: "serial" });

test("says there are no reports before there are any", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("tab", { name: "Reports" }).click();
  await expect(page.getByText(/no reports yet/)).toBeVisible();
});

test("lists reports, shows one in a sandboxed frame under its own strict policy", async ({ page, request }) => {
  seed("20261126-091530-aaaaaa");
  seed("20261125-081000-bbbbbb", {
    trainee: "Alan Turing", scenario: { id: "s", name: "Vault Sandbox", product: "vault" },
    startedAt: "2026-11-25T08:10:00Z", endedAt: "2026-11-25T08:30:00Z", result: "completed", steps: [], events: [],
  });

  await page.goto("/");
  await page.getByRole("tab", { name: "Reports" }).click();
  const rows = page.getByRole("row");
  await expect(rows.filter({ hasText: "Grace Hopper" })).toBeVisible();
  await expect(rows.filter({ hasText: "Alan Turing" })).toBeVisible();
  // Newest first.
  const names = await page.locator("tbody tr").allInnerTexts();
  expect(names[0]).toContain("Grace Hopper");

  await page.getByRole("button", { name: /Open the report for Grace Hopper/ }).click();
  const frameElement = page.locator('iframe[title="The report"]');
  await expect(frameElement).toHaveAttribute("sandbox", "");
  const frame = page.frameLocator('iframe[title="The report"]');
  await expect(frame.getByRole("heading", { name: "Lost Quorum", level: 1 })).toBeVisible();
  await expect(frame.getByText("Looked at the logs first.")).toBeVisible();
  // The page's own inline style is applied inside the frame, so its policy lets it through.
  await expect(frame.locator("h1")).toHaveCSS("font-weight", "400");
  await expect(frame.locator(".badge")).toHaveCSS("border-top-style", "solid");

  // The route, asked for directly: its own policy, never the application's, and nothing outside the folder.
  const ok = await request.get("/_reports/20261126-091530-aaaaaa.html");
  expect(ok.status()).toBe(200);
  expect(ok.headers()["content-security-policy"]).toBe("default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'self'");
  expect(ok.headers()["cache-control"]).toBe("no-store");
  expect((await request.get("/_reports/missing.html")).status()).toBe(404);
  expect((await request.get("/_reports/..%2F..%2Fsettings.html")).status()).toBeGreaterThanOrEqual(400);
  expect((await request.post("/_reports/20261126-091530-aaaaaa.html")).status()).toBe(405);
  const app = await request.get("/");
  expect(app.headers()["content-security-policy"]).toContain("default-src 'self'");
});

test("exporting says plainly that saving needs the application window, in this build", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("tab", { name: "Reports" }).click();
  await page.getByRole("button", { name: /More for Grace Hopper/ }).click();
  await page.getByRole("menuitem", { name: "Export the page" }).click();
  await expect(page.getByText(/needs the application window/)).toBeVisible();
});

for (const scheme of ["light", "dark"] as const) {
  test(`the reports list and the viewer pass the accessibility audit (${scheme})`, async ({ page }) => {
    await page.emulateMedia({ colorScheme: scheme });
    await page.goto("/");
    await page.getByRole("tab", { name: "Reports" }).click();
    await expect(page.getByText("Grace Hopper")).toBeVisible();
    await audit(page, `the reports list (${scheme})`);

    await page.getByRole("button", { name: /Open the report for Grace Hopper/ }).click();
    await expect(page.frameLocator('iframe[title="The report"]').getByRole("heading", { name: "Lost Quorum" })).toBeVisible();
    await audit(page, `the report viewer (${scheme})`, { exclude: ['iframe[title="The report"]'] });

    // The report itself, which the frame shows, as a page of its own.
    await page.goto("/_reports/20261126-091530-aaaaaa.html");
    await expect(page.getByRole("heading", { name: "Lost Quorum", level: 1 })).toBeVisible();
    await audit(page, `the report page (${scheme})`);
  });
}

test("deleting asks first, names the report, and removes only that file", async ({ page }) => {
  expect(existsSync(fileOf("20261125-081000-bbbbbb"))).toBe(true);
  await page.goto("/");
  await page.getByRole("tab", { name: "Reports" }).click();
  await page.getByRole("button", { name: /More for Alan Turing/ }).click();
  await page.getByRole("menuitem", { name: "Delete" }).click();

  const dialog = page.getByRole("dialog");
  await expect(dialog).toContainText("the report of Alan Turing on Vault Sandbox");
  await dialog.getByRole("button", { name: "Cancel" }).click();
  expect(existsSync(fileOf("20261125-081000-bbbbbb"))).toBe(true);

  await page.getByRole("button", { name: /More for Alan Turing/ }).click();
  await page.getByRole("menuitem", { name: "Delete" }).click();
  await page.getByRole("dialog").getByRole("button", { name: "Delete report" }).click();
  await expect(page.getByRole("row", { name: /Alan Turing/ })).toHaveCount(0);
  await expect(page.getByText(/was deleted/)).toBeVisible();
  expect(existsSync(fileOf("20261125-081000-bbbbbb"))).toBe(false);
  expect(existsSync(fileOf("20261126-091530-aaaaaa"))).toBe(true);
});
