import { test, expect } from "@playwright/test";
import { execFileSync } from "node:child_process";
import { audit } from "./axe";

// This starts a real Vault sandbox on Podman through the page, then ends it. It needs a running
// Podman machine and the network (or a cached Vault binary), and takes about a minute, so it only
// runs when asked: REAL_DEPLOY=1 npx playwright test real-deploy
test.skip(!process.env.REAL_DEPLOY, "set REAL_DEPLOY=1 to run against real Podman");
test.setTimeout(300_000);

const podman = (...args: string[]) => execFileSync("podman", args, { encoding: "utf8" }).trim();
const container = () => podman("ps", "--filter", "label=s1s.app", "--format", "{{.Names}}").split("\n")[0];

const leftovers = () =>
  execFileSync("podman", ["ps", "-a", "--filter", "label=s1s.app", "--format", "{{.Names}}"], { encoding: "utf8" }).trim();

test("deploys the Vault sandbox from the page and ends it, leaving nothing behind", async ({ page }) => {
  // The terminal's WebAssembly must load under the real Content-Security-Policy (R-TERM-6).
  await page.addInitScript(() => {
    (window as any).__violations = [] as string[];
    document.addEventListener("securitypolicyviolation", (e) =>
      (window as any).__violations.push(`${e.violatedDirective} blocked ${e.blockedURI}`));
  });
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

  // The run page and its tabs pass the accessibility audit in both themes (R-UI-6).
  for (const scheme of ["light", "dark"] as const) {
    await page.emulateMedia({ colorScheme: scheme });
    await page.getByRole("tab", { name: "Steps" }).click();
    await audit(page, `the run page, Steps tab (${scheme})`);
    await page.getByRole("tab", { name: "Cluster" }).click();
    await expect(page.getByText("running", { exact: true })).toBeVisible({ timeout: 60_000 });
    await audit(page, `the Cluster tab (${scheme})`);
    await page.getByRole("tab", { name: "Logs" }).click();
    await expect(page.getByRole("log", { name: "Log lines" })).toContainText("[INFO]", { timeout: 60_000 });
    await audit(page, `the Logs tab (${scheme})`);
  }
  await page.emulateMedia({ colorScheme: "light" });

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

  // A real terminal in the container. The screen is a canvas, so what is typed is proved by what it
  // does inside the container, and what comes back by the canvas being painted.
  await page.getByRole("tab", { name: "Cluster" }).click();
  await page.getByRole("button", { name: "Open a terminal on node-1" }).click();
  const screen = page.getByRole("group", { name: "Terminal on node-1" });
  await expect(screen.locator("canvas")).toBeVisible({ timeout: 30_000 });
  await page.waitForTimeout(1500); // the shell starts and prints its prompt
  await screen.click();
  await page.keyboard.type("echo typed-in-the-window > /tmp/e2e-marker\n");
  await expect
    .poll(() => { try { return podman("exec", container(), "cat", "/tmp/e2e-marker"); } catch { return ""; } }, { timeout: 15_000 })
    .toBe("typed-in-the-window");

  // Output reaches the screen: the canvas has painted text.
  await page.keyboard.type("echo painted output from the node\n");
  await expect
    .poll(() => screen.locator("canvas").evaluate((c: HTMLCanvasElement) => {
      const px = c.getContext("2d")!.getImageData(0, 0, c.width, c.height).data;
      const bg = [px[0], px[1], px[2]];
      let n = 0;
      for (let i = 0; i < px.length; i += 4) if (Math.abs(px[i] - bg[0]) + Math.abs(px[i + 1] - bg[1]) + Math.abs(px[i + 2] - bg[2]) > 120) n++;
      return n;
    }), { timeout: 15_000 })
    .toBeGreaterThan(200);

  // A resize reaches the shell as a new size.
  const sizeOf = async (tag: string) => {
    await page.keyboard.type(`stty size > /tmp/size-${tag}\n`);
    await expect.poll(() => { try { return podman("exec", container(), "cat", `/tmp/size-${tag}`); } catch { return ""; } }, { timeout: 15_000 }).toMatch(/^\d+ \d+$/);
    return podman("exec", container(), "cat", `/tmp/size-${tag}`);
  };
  const before = await sizeOf("a");
  await page.setViewportSize({ width: 1300, height: 900 });
  await page.waitForTimeout(800);
  await screen.click();
  const after = await sizeOf("b");
  expect(after).not.toBe(before);

  // The library's defect (spike W2): random binary kills a terminal. The glue rebuilds it, and the
  // session carries on: the next command still reaches the shell, and the screen paints again.
  await page.keyboard.type("head -c 3000000 /dev/urandom\n");
  await expect(page.getByText("The screen was cleared")).toBeVisible({ timeout: 30_000 }); // the guard fired
  await page.waitForTimeout(4000);
  await page.keyboard.type("clear; echo alive-after-binary > /tmp/e2e-alive\n");
  await expect
    .poll(() => { try { return podman("exec", container(), "cat", "/tmp/e2e-alive"); } catch { return ""; } }, { timeout: 20_000 })
    .toBe("alive-after-binary");
  await page.keyboard.type("echo and painting again\n");
  await expect
    .poll(() => screen.locator("canvas").evaluate((c: HTMLCanvasElement) => {
      const px = c.getContext("2d")!.getImageData(0, 0, c.width, c.height).data;
      const bg = [px[0], px[1], px[2]];
      let n = 0;
      for (let i = 0; i < px.length; i += 4) if (Math.abs(px[i] - bg[0]) + Math.abs(px[i + 1] - bg[1]) + Math.abs(px[i + 2] - bg[2]) > 120) n++;
      return n;
    }), { timeout: 15_000 })
    .toBeGreaterThan(50);

  // The terminal does not trap the keyboard: Shift+Escape leaves it, and the page can be audited with it open.
  await screen.click();
  await page.keyboard.press("Shift+Escape");
  await expect(page.getByRole("button", { name: "Back into the terminal" })).toBeFocused();
  await page.keyboard.press("Tab");
  await expect(page.getByRole("button", { name: "Back into the terminal" })).not.toBeFocused();
  await audit(page, "the Terminals tab");

  expect(await page.evaluate(() => (window as any).__violations)).toEqual([]);

  // Another scenario cannot be started meanwhile.
  await page.getByRole("tab", { name: "Scenarios" }).click();
  await expect(page.getByText("A scenario is running")).toBeVisible();
  await expect(page.getByRole("button", { name: "Start" })).toBeDisabled();
  await page.getByRole("tab", { name: "Current run" }).click();

  // The confirmation dialog takes focus, keeps it while Tab is pressed, closes on Escape and gives focus back.
  const endButton = page.getByRole("button", { name: "End scenario" });
  await endButton.focus();
  await page.keyboard.press("Enter");
  const dialog = page.getByRole("dialog");
  await expect(dialog).toBeVisible();
  await audit(page, "the End scenario dialog");
  for (let i = 0; i < 6; i++) {
    await page.keyboard.press("Tab");
    expect(await page.evaluate(() => !!document.activeElement?.closest('[role="dialog"]'))).toBe(true);
  }
  await page.keyboard.press("Escape");
  await expect(dialog).toBeHidden();
  await expect(endButton).toBeFocused();

  await endButton.click();
  await page.getByRole("dialog").getByRole("button", { name: "End scenario" }).click();
  await expect(page.getByRole("heading", { name: "Scenarios", level: 2 })).toBeVisible({ timeout: 120_000 });
  expect(leftovers()).toBe("");
});
