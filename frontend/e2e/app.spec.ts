import { test, expect } from "@playwright/test";

// This runs against the production server build, so the real Content-Security-Policy is applied.

test("opens on the application name without breaking the policy or touching the network", async ({ page, baseURL }) => {
  const problems: string[] = [];
  const external: string[] = [];
  const origin = new URL(baseURL!).origin;

  page.on("console", (m) => m.type() === "error" && problems.push("console: " + m.text()));
  page.on("pageerror", (e) => problems.push("page error: " + e.message));
  page.on("request", (r) => {
    if (!r.url().startsWith(origin) && !r.url().startsWith("data:")) external.push(r.url());
  });
  await page.addInitScript(() => {
    (window as any).__violations = [] as string[];
    document.addEventListener("securitypolicyviolation", (e) =>
      (window as any).__violations.push(`${e.violatedDirective} blocked ${e.blockedURI}`));
  });

  const response = await page.goto("/");
  await expect(page.getByRole("heading", { name: "Sev1 Situations" })).toBeVisible();
  await expect(page).toHaveTitle("Sev1 Situations");
  await expect(page.getByRole("banner")).toContainText("Situations");

  // The built-in scenarios are listed, grouped under their product.
  await expect(page.getByRole("heading", { name: "Scenarios", level: 2 })).toBeVisible();
  await expect(page.getByRole("heading", { name: "Vault", level: 3 })).toBeVisible();
  await expect(page.getByRole("heading", { name: "Vault sandbox" })).toBeVisible();

  // The policy is on the page, and nothing the page does breaks it.
  const csp = response!.headers()["content-security-policy"];
  expect(csp).toContain("default-src 'self'");
  expect(csp).not.toContain("unsafe-inline");
  expect(await page.evaluate(() => (window as any).__violations)).toEqual([]);

  // Nothing is fetched from outside the application, and the fonts it asked for are really loaded.
  const loaded = await page.evaluate(async () => {
    await document.fonts.ready;
    return [...document.fonts].filter((f) => f.status === "loaded").map((f) => f.family.replace(/"/g, ""));
  });
  expect(loaded).toContain("IBM Plex Sans");
  expect(external).toEqual([]);
  expect(problems).toEqual([]);
});

test("follows the system's light and dark setting", async ({ page }) => {
  await page.emulateMedia({ colorScheme: "dark" });
  await page.goto("/");
  await expect(page.locator("html")).toHaveAttribute("theme", "g100");

  await page.emulateMedia({ colorScheme: "light" });
  await expect(page.locator("html")).toHaveAttribute("theme", "white");
});

test("Start opens the form for the scenario, and Cancel returns to the catalogue", async ({ page }) => {
  await page.goto("/");
  // Podman may be absent where this runs, which disables Start, so the form is opened through it only when enabled.
  const start = page.getByRole("listitem").filter({ hasText: "Vault Sandbox" }).getByRole("button", { name: "Start" });
  await expect(start).toBeVisible();
  test.skip(await start.isDisabled(), "Podman is not ready here, so Start is disabled");

  await start.click();
  await expect(page.getByRole("heading", { name: "Start Vault Sandbox" })).toBeVisible();
  await expect(page.getByLabel("Instructor's name")).toBeVisible();
  await expect(page.getByLabel("Trainee's name")).toBeVisible();
  await expect(page.getByLabel("Node count")).toHaveValue("3");

  await page.getByRole("button", { name: "Deploy" }).click();
  await expect(page.getByText("Enter the trainee's name.")).toBeVisible();

  await page.getByRole("button", { name: "Cancel" }).click();
  await expect(page.getByRole("heading", { name: "Scenarios", level: 2 })).toBeVisible();
});
