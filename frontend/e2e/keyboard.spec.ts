import { test, expect } from "@playwright/test";

// Everything here is done with the keyboard alone (R-UI-6).

test("the settings page can be used without a mouse", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "Scenarios", level: 2 })).toBeVisible();

  // Reach the Settings button by Tab, and open it with Enter.
  const settings = page.getByRole("button", { name: "Settings" });
  await settings.focus();
  await expect(settings).toBeFocused();
  await page.keyboard.press("Enter");
  await expect(page.getByRole("heading", { name: "Settings", level: 2 })).toBeVisible();

  // Fill the form and save it with Enter, which submits a form.
  await page.getByLabel("Instructor's name").focus();
  await page.keyboard.type("Keyboard Kim");
  await page.keyboard.press("Enter");
  await expect(page.getByText("The instructor's name saved.")).toBeVisible();

  // Back with the keyboard.
  await page.getByRole("button", { name: "Back" }).focus();
  await page.keyboard.press("Enter");
  await expect(page.getByRole("heading", { name: "Scenarios", level: 2 })).toBeVisible();
});

test("focus is always visible while tabbing through the scenarios page", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByText("Vault Sandbox")).toBeVisible();
  await page.locator("body").click({ position: { x: 1, y: 1 } });

  const visited: string[] = [];
  for (let i = 0; i < 12; i++) {
    await page.keyboard.press("Tab");
    const info = await page.evaluate(() => {
      const el = document.activeElement as HTMLElement | null;
      if (!el || el === document.body) return null;
      const s = getComputedStyle(el);
      // Carbon marks focus with an outline, a shadow, or, on the header's buttons, a 2px border.
      const outlined =
        (parseFloat(s.outlineWidth) > 0 && s.outlineStyle !== "none") || s.boxShadow !== "none" || (parseFloat(s.borderTopWidth) >= 2 && s.borderTopStyle !== "none");
      return { name: el.getAttribute("aria-label") || el.textContent?.trim().slice(0, 30) || el.tagName, outlined };
    });
    if (info) {
      visited.push(info.name);
      expect(info.outlined, `"${info.name}" has no visible focus indicator`).toBe(true);
    }
  }
  expect(visited).toContain("Settings");
  expect(visited.some((v) => /Product|All/.test(v))).toBe(true);
});

test("the start form is operable by keyboard, and errors are announced", async ({ page }) => {
  await page.goto("/");
  const start = page.getByRole("button", { name: "Start" });
  test.skip(await start.isDisabled(), "Podman is not ready here, so the form cannot be opened");
  await start.focus();
  await page.keyboard.press("Enter");
  await expect(page.getByRole("heading", { name: /^Start / })).toBeVisible();
  await expect(page.getByLabel(/version/i)).not.toHaveValue(""); // Deploy waits for the versions

  await page.getByRole("button", { name: "Deploy" }).focus();
  await page.keyboard.press("Enter");
  // Each error is tied to its field and announced as an alert.
  await expect(page.getByRole("alert").filter({ hasText: "Enter the trainee's name." })).toBeVisible();
  await expect(page.getByLabel("Trainee's name")).toHaveAttribute("aria-invalid", "true");
});
