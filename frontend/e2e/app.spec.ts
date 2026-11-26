import { test, expect } from "@playwright/test";

test("opens on the application name", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "Sev1 Situations" })).toBeVisible();
  await expect(page).toHaveTitle("Sev1 Situations");
});
