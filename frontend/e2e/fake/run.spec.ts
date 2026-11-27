import { test, expect, type Page } from "@playwright/test";
import { audit } from "../axe";

// These run the whole application on a fake runtime and a fake product (see playwright.fake.config.ts), so the run page, the Cluster,
// Logs and Terminals tabs and the step flow are covered with no containers. Each test ends its run, so the next starts clean.

async function start(page: Page, scenario: string) {
  await page.goto("/");
  await page.getByRole("listitem").filter({ hasText: scenario }).getByRole("button", { name: "Start" }).click();
  await page.getByLabel("Instructor's name").fill("Ada");
  await page.getByLabel("Trainee's name").fill("Grace");
  await expect(page.getByLabel("Vault version")).not.toHaveValue("");
  await page.getByRole("button", { name: "Deploy" }).click();
}

async function end(page: Page) {
  await page.getByRole("button", { name: "End scenario" }).click();
  const dialog = page.getByRole("dialog", { name: "End this scenario?" });
  await dialog.getByRole("button", { name: "End scenario" }).click();
}

test("a stepped scenario goes through breakage, a failed check, a pass, and the report", async ({ page }) => {
  await start(page, "Test Outage");
  await expect(page.getByRole("heading", { name: "Test Outage", level: 2 })).toBeVisible({ timeout: 60_000 });

  // The first step opens with its guidance, and nothing later is given away.
  await expect(page.getByText("the outage has started")).toBeVisible();
  await expect(page.getByRole("button", { name: "Continue" })).toBeDisabled();
  await expect(page.getByText("Press Verify once the trainee says it is fixed.")).toHaveCount(0);

  await page.getByRole("button", { name: "Apply breakage" }).click();
  await expect(page.getByRole("button", { name: "Continue" })).toBeEnabled({ timeout: 30_000 });
  await page.getByRole("button", { name: "Continue" }).click();

  // The first look finds it broken, so Continue stays shut. The second passes.
  await page.getByRole("button", { name: "Verify" }).click();
  await expect(page.getByText("the cluster is still broken")).toBeVisible({ timeout: 30_000 });
  await expect(page.getByRole("button", { name: "Continue" })).toBeDisabled();
  await page.getByRole("button", { name: "Verify" }).click();
  await expect(page.getByRole("button", { name: "Continue" })).toBeEnabled({ timeout: 30_000 });
  await page.getByRole("button", { name: "Continue" }).click();

  // Ending the finished run saves its report, and it is in the list.
  await end(page);
  await page.getByRole("tab", { name: "Reports" }).click();
  await expect(page.getByRole("heading", { name: "Reports", level: 2 })).toBeVisible();
  await expect(page.getByText("Test Outage").first()).toBeVisible();
});

test("the Cluster, Logs and Terminals tabs show the deployment, and ending removes it", async ({ page }) => {
  await start(page, "Vault Sandbox");
  await expect(page.getByText("This is an open sandbox")).toBeVisible({ timeout: 60_000 });

  for (const scheme of ["light", "dark"] as const) {
    await page.emulateMedia({ colorScheme: scheme });
    await page.getByRole("tab", { name: "Steps" }).click();
    await audit(page, `the run page, Steps tab (${scheme})`);
    await page.getByRole("tab", { name: "Cluster" }).click();
    await expect(page.getByRole("heading", { name: "node-1" })).toBeVisible();
    await audit(page, `the Cluster tab (${scheme})`);
    await page.getByRole("tab", { name: "Logs" }).click();
    await expect(page.getByRole("log", { name: "Log lines" })).toBeVisible();
    await audit(page, `the Logs tab (${scheme})`);
  }
  await page.emulateMedia({ colorScheme: "light" });

  // The root token is masked until it is revealed, and the page never holds it otherwise.
  await page.getByRole("tab", { name: "Cluster" }).click();
  await expect(page.getByRole("heading", { name: "Primary cluster" })).toBeVisible();
  expect(await page.locator("body").innerText()).not.toContain("fake-root-token-0000");
  await page.getByRole("button", { name: /^Show/ }).first().click();
  await expect(page.getByText("fake-root-token-0000")).toBeVisible();

  // A node can be stopped and started again.
  await page.getByRole("button", { name: "Stop node-1" }).click();
  await expect(page.getByRole("button", { name: "Start node-1" })).toBeVisible({ timeout: 30_000 });
  await page.getByRole("button", { name: "Start node-1" }).click();
  await expect(page.getByRole("button", { name: "Stop node-1" })).toBeVisible({ timeout: 30_000 });

  // The Terminals tab opens a terminal for a node, and closes it.
  await page.getByRole("button", { name: "Open a terminal on node-1" }).click();
  await expect(page.getByRole("tab", { name: "Terminals" })).toHaveAttribute("aria-selected", "true");
  await expect(page.getByRole("button", { name: "Close terminal" })).toBeVisible();
  await page.getByRole("button", { name: "Close terminal" }).click();

  await end(page);
  await expect(page.getByRole("heading", { name: "Scenarios", level: 2 })).toBeVisible({ timeout: 60_000 });
});

test("rolling back replays the breakage and undoes the fix, and ending early still saves a report", async ({ page }) => {
  test.setTimeout(150_000);
  await start(page, "Test Outage");
  await expect(page.getByRole("heading", { name: "Test Outage", level: 2 })).toBeVisible({ timeout: 60_000 });

  await page.getByRole("button", { name: "Apply breakage" }).click();
  await expect(page.getByRole("button", { name: "Continue" })).toBeEnabled({ timeout: 30_000 });
  await page.getByRole("button", { name: "Continue" }).click();

  // The trainee fixes it: the second look passes.
  await page.getByRole("button", { name: "Verify" }).click();
  await expect(page.getByText("the cluster is still broken")).toBeVisible({ timeout: 30_000 });
  await page.getByRole("button", { name: "Verify" }).click();
  await expect(page.getByRole("button", { name: "Continue" })).toBeEnabled({ timeout: 30_000 });

  // Rolling back to the breakage deploys again and replays it, so the fix is gone and the first look fails again.
  await page.getByRole("button", { name: "Roll back" }).first().click();
  const dialog = page.getByRole("dialog", { name: "Roll back?" });
  await expect(dialog).toBeVisible();
  await dialog.getByRole("button", { name: "Roll back" }).click();
  await expect(dialog).toBeHidden({ timeout: 120_000 });
  await expect(page.getByRole("button", { name: "Continue" })).toBeDisabled();
  await page.getByRole("button", { name: "Verify" }).click();
  await expect(page.getByText("the cluster is still broken")).toBeVisible({ timeout: 30_000 });

  // Ended early, the run is over and its report says so.
  await end(page);
  await page.getByRole("tab", { name: "Reports" }).click();
  await expect(page.getByText("Test Outage").first()).toBeVisible();
  await expect(page.getByText("Ended early").first()).toBeVisible();
});
