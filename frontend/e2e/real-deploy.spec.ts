import { test, expect } from "@playwright/test";
import { execFileSync } from "node:child_process";

// This starts a real Vault sandbox on Podman through the page, then ends it. It needs a running
// Podman machine and the network (or a cached Vault binary), and takes about a minute, so it only
// runs when asked: REAL_DEPLOY=1 npx playwright test real-deploy
test.skip(!process.env.REAL_DEPLOY, "set REAL_DEPLOY=1 to run against real Podman");
test.setTimeout(300_000);

const leftovers = () =>
  execFileSync("podman", ["ps", "-a", "--filter", "label=s1s.app", "--format", "{{.Names}}"], { encoding: "utf8" }).trim();

test("deploys the Vault sandbox from the page and ends it, leaving nothing behind", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("button", { name: "Start" }).click();

  await page.getByLabel("Instructor's name").fill("Ada");
  await page.getByLabel("Trainee's name").fill("Grace");
  await page.getByLabel("Node count").fill("1");
  await expect(page.getByLabel("Vault version")).not.toHaveValue("");
  await page.getByRole("button", { name: "Deploy" }).click();

  // The run page takes over once the run exists, and the sandbox has no steps.
  await expect(page.getByText("This is an open sandbox")).toBeVisible({ timeout: 240_000 });
  await expect(page.getByRole("heading", { name: "Vault Sandbox", level: 2 })).toBeVisible();
  expect(leftovers()).not.toBe("");

  // The Cluster tab describes what was deployed. The root token is masked until it is revealed.
  await page.getByRole("tab", { name: "Cluster" }).click();
  await expect(page.getByRole("heading", { name: "Primary cluster" })).toBeVisible();
  await expect(page.getByText("http://127.0.0.1:", { exact: false }).first()).toBeVisible();
  await expect(page.getByRole("heading", { name: "node-1" })).toBeVisible();
  await expect(page.getByText("running", { exact: true })).toBeVisible({ timeout: 60_000 });
  expect(await page.locator("body").innerText()).not.toMatch(/hvs\.[A-Za-z0-9]/);
  await page.getByRole("button", { name: "Show Root token" }).click();
  await expect(page.getByText(/^hvs\./)).toBeVisible();

  // A real node action: stop the node and start it again.
  await page.getByRole("button", { name: "Stop node-1" }).click();
  await expect(page.getByText("stopped", { exact: true })).toBeVisible({ timeout: 60_000 });
  await page.getByRole("button", { name: "Start node-1" }).click();
  await expect(page.getByText("running", { exact: true })).toBeVisible({ timeout: 60_000 });

  // The Logs tab follows the node's real log, and filters it.
  await page.getByRole("tab", { name: "Logs" }).click();
  const log = page.getByRole("log", { name: "Log lines" });
  await expect(log).toContainText("[INFO]", { timeout: 60_000 });
  await page.getByRole("searchbox").fill("core");
  await expect(page.locator("mark").first()).toBeVisible();
  await page.getByRole("searchbox").fill("");

  // The audit log is a file inside the container. Opening it must not fail, and the node's own
  // requests (the cluster tab reads it) show up in it.
  await page.getByLabel("Log", { exact: true }).selectOption("audit");
  await expect(page.getByText("This log could not be read")).toHaveCount(0);
  await page.getByRole("tab", { name: "Cluster" }).click();
  await page.getByRole("button", { name: "Refresh" }).click();
  await page.getByRole("tab", { name: "Logs" }).click();
  await expect(page.getByText("This log could not be read")).toHaveCount(0);

  // Another scenario cannot be started meanwhile.
  await page.getByRole("tab", { name: "Scenarios" }).click();
  await expect(page.getByText("A scenario is running")).toBeVisible();
  await expect(page.getByRole("button", { name: "Start" })).toBeDisabled();
  await page.getByRole("tab", { name: "Current run" }).click();

  await page.getByRole("button", { name: "End scenario" }).click();
  await page.getByRole("dialog").getByRole("button", { name: "End scenario" }).click();
  await expect(page.getByRole("heading", { name: "Scenarios", level: 2 })).toBeVisible({ timeout: 120_000 });
  expect(leftovers()).toBe("");
});
