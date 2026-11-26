import { test, expect } from "@playwright/test";
import { audit } from "./axe";

// An automated audit of every page that does not need a running scenario, in both themes (R-UI-6).
// The run page, the Cluster, Logs and Terminals tabs are audited in real-deploy.spec.ts, which has a run.
// Automated rules find about a third of the problems, so keyboard.spec.ts checks the rest by hand-written steps.

for (const scheme of ["light", "dark"] as const) {
  test.describe(`${scheme} theme`, () => {
    test.beforeEach(async ({ page }) => {
      await page.emulateMedia({ colorScheme: scheme });
    });

    test("scenarios page", async ({ page }) => {
      await page.goto("/");
      await expect(page.getByRole("heading", { name: "Scenarios", level: 2 })).toBeVisible();
      await expect(page.getByText("Vault Sandbox")).toBeVisible();
      await audit(page, `the scenarios page (${scheme})`);
    });

    test("settings page", async ({ page }) => {
      await page.goto("/");
      await page.getByRole("button", { name: "Settings" }).click();
      await expect(page.getByRole("heading", { name: "Settings", level: 2 })).toBeVisible();
      await audit(page, `the settings page (${scheme})`);
    });

    test("start form, with its errors showing", async ({ page }) => {
      await page.goto("/");
      const start = page.getByRole("button", { name: "Start" });
      test.skip(await start.isDisabled(), "Podman is not ready here, so the form cannot be opened");
      await start.click();
      await expect(page.getByRole("heading", { name: /^Start / })).toBeVisible();
      await expect(page.getByLabel("Vault version")).not.toHaveValue("");
      await page.getByRole("button", { name: "Deploy" }).click();
      await expect(page.getByText("Enter the trainee's name.")).toBeVisible();
      await audit(page, `the start form (${scheme})`);
    });
  });
}
