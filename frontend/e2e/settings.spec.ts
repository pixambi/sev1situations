import { test, expect } from "@playwright/test";
import { readFileSync, statSync } from "node:fs";
import { join } from "node:path";

// These save real settings through the real Go service, into the run's own data folder.
const settingsFile = () => join(process.env.S1S_DATA_DIR!, "settings.json");

test.describe.configure({ mode: "serial" });

test("saves the instructor's name, which survives a reload", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("button", { name: "Settings" }).click();
  await page.getByLabel("Instructor's name").fill("  Ada Lovelace ");
  await page.getByRole("button", { name: "Save name" }).click();
  await expect(page.getByText("The instructor's name saved.")).toBeVisible();

  await page.reload();
  await page.getByRole("button", { name: "Settings" }).click();
  await expect(page.getByLabel("Instructor's name")).toHaveValue("Ada Lovelace");
  expect(JSON.parse(readFileSync(settingsFile(), "utf8")).instructor).toBe("Ada Lovelace");
});

test("keeps a license private: masked in the page, owner-only on disk, never read back", async ({ page }) => {
  const license = "02MV4UU43BK5.PRIVATE-LICENSE-VALUE";
  await page.goto("/");
  await page.getByRole("button", { name: "Settings" }).click();
  // Each product brings its own license field, so the controls are found inside the Vault one.
  const vault = () => page.locator("form").filter({ has: page.getByLabel("Vault Enterprise license") });
  await page.getByLabel("Vault Enterprise license").fill(license);
  await vault().getByRole("button", { name: "Save", exact: true }).click();
  await expect(page.getByText("Vault Enterprise license saved.")).toBeVisible();

  // The page never holds it: not in the text, not in any field, not after a reload.
  await page.reload();
  await page.getByRole("button", { name: "Settings" }).click();
  await expect(page.getByLabel("Vault Enterprise license")).toHaveValue("");
  await expect(vault().getByPlaceholder("Saved. Enter a new one to replace it.")).toBeVisible();
  expect(await page.content()).not.toContain("PRIVATE-LICENSE-VALUE");

  // On disk it is there for the core to use, in a file only the owner can read (R-SET-3).
  expect(readFileSync(settingsFile(), "utf8")).toContain("PRIVATE-LICENSE-VALUE");
  expect(statSync(settingsFile()).mode & 0o777).toBe(0o600);

  await vault().getByRole("button", { name: "Remove" }).click();
  await expect(vault().getByText("Not set")).toBeVisible();
  expect(readFileSync(settingsFile(), "utf8")).not.toContain("PRIVATE-LICENSE-VALUE");
});

test("the theme choice applies at once and is remembered", async ({ page }) => {
  await page.emulateMedia({ colorScheme: "light" });
  await page.goto("/");
  await expect(page.locator("html")).toHaveAttribute("theme", "white");

  await page.getByRole("button", { name: "Settings" }).click();
  await page.getByRole("combobox", { name: "Theme" }).selectOption("dark");
  await expect(page.locator("html")).toHaveAttribute("theme", "g100");

  await page.reload();
  await expect(page.locator("html")).toHaveAttribute("theme", "g100"); // the system is light, the choice is dark

  await page.getByRole("button", { name: "Settings" }).click();
  await page.getByRole("combobox", { name: "Theme" }).selectOption({ label: "Follow the system" });
  await expect(page.locator("html")).toHaveAttribute("theme", "white");
});
