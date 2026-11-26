import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, cleanup, waitFor } from "@testing-library/svelte";
import { tick } from "svelte";

// A fake of Go's event stream: the test pushes events, and the store receives them as it would.
const bus = vi.hoisted(() => {
  const handlers = new Map<string, Set<(e: { data: unknown }) => void>>();
  return {
    handlers,
    on: (name: string, fn: (e: { data: unknown }) => void) => {
      if (!handlers.has(name)) handlers.set(name, new Set());
      handlers.get(name)!.add(fn);
      return () => handlers.get(name)!.delete(fn);
    },
    emit: (name: string, data: unknown) => handlers.get(name)?.forEach((fn) => fn({ data })),
  };
});
const current = vi.hoisted(() => vi.fn());
const guide = vi.hoisted(() => vi.fn());
vi.mock("@wailsio/runtime", () => ({ Events: { On: bus.on }, Browser: { OpenURL: vi.fn() } }));
vi.mock("../../bindings/github.com/pixambi/sev1situations/internal/gui/runservice.js", () => ({ Current: current, Guide: guide }));

import { followRun, run, runActive } from "./run.svelte";
import RunPage from "./RunPage.svelte";

const snapshot = (over: Record<string, unknown> = {}) => ({
  Status: "active", RunID: "abc", ScenarioID: "demo", ScenarioName: "Demo", Instructor: "Ada", Trainee: "Grace",
  StartedAt: "2026-11-26T09:00:00Z", EndedAt: "0001-01-01T00:00:00Z", DeployError: "", ActiveStep: -1, Steps: [], Events: [], Nodes: [], ...over,
});

beforeEach(() => {
  run.snapshot = null;
  current.mockResolvedValue(snapshot({ Status: "idle" }));
  guide.mockResolvedValue({ Briefing: "", Hints: "", Solution: "", References: "" });
});
afterEach(() => {
  cleanup();
  bus.handlers.clear();
  vi.resetAllMocks();
});

describe("followRun", () => {
  it("loads the current state, then follows each run:state", async () => {
    const stop = followRun();
    await waitFor(() => expect(run.snapshot?.Status).toBe("idle"));

    bus.emit("run:state", snapshot({ Status: "deploying" }));
    expect(run.snapshot?.Status).toBe("deploying");
    bus.emit("run:state", snapshot({ Status: "active" }));
    expect(run.snapshot?.Status).toBe("active");
    stop();
  });

  it("keeps an event that arrives before the first load finishes, because it is newer", async () => {
    let finishLoad!: (s: unknown) => void;
    current.mockReturnValue(new Promise((resolve) => (finishLoad = resolve)));

    const stop = followRun();
    bus.emit("run:state", snapshot({ Status: "deploying" }));
    finishLoad(snapshot({ Status: "idle" })); // the slow answer describes an older moment
    await tick();
    await Promise.resolve();

    expect(run.snapshot?.Status).toBe("deploying");
    stop();
  });

  it("stops following when asked", async () => {
    const stop = followRun();
    await waitFor(() => expect(run.snapshot).not.toBeNull());
    stop();
    bus.emit("run:state", snapshot({ Status: "active" }));
    expect(run.snapshot?.Status).toBe("idle");
  });

  it("carries on when the first load fails, and the next event fills the state in", async () => {
    current.mockRejectedValue(new Error("not connected"));
    const stop = followRun();
    await tick();
    expect(run.snapshot).toBeNull();

    bus.emit("run:state", snapshot({ Status: "active" }));
    expect(run.snapshot?.Status).toBe("active");
    stop();
  });
});

describe("runActive", () => {
  it.each([
    [null, false],
    ["idle", false],
    ["deploying", true],
    ["deploy-failed", true],
    ["active", true],
    ["finished", true],
  ])("%s -> %s", (status, want) => expect(runActive(status === null ? null : (snapshot({ Status: status }) as never))).toBe(want));
});

describe("the run page follows the stream", () => {
  it("shows deployment progress as it arrives, then the steps once the run is up", async () => {
    // A tiny host that renders the page from the store, as the application does.
    const Host = (await import("./RunHostForTest.svelte")).default;
    followRun();
    render(Host);

    bus.emit("run:state", snapshot({ Status: "deploying", Events: [{ Kind: "deploy-progress", Message: "creating the network" }] }));
    expect(await screen.findByText("creating the network")).toBeInTheDocument();

    bus.emit("run:state", snapshot({
      Status: "deploying",
      Events: [{ Kind: "deploy-progress", Message: "creating the network" }, { Kind: "deploy-progress", Message: "starting node-1" }],
    }));
    expect(await screen.findByText("starting node-1")).toBeInTheDocument();

    bus.emit("run:state", snapshot({
      Status: "active", ActiveStep: 0,
      Steps: [{ ID: "break", Title: "Break it", Kind: "breakage", State: "active", Notes: "", Rating: 0, Last: null, CanApply: true }],
    }));
    expect(await screen.findByRole("heading", { name: "Break it" })).toBeInTheDocument();
    expect(screen.queryByText("starting node-1")).not.toBeInTheDocument();

    bus.emit("run:state", snapshot({
      Status: "active", ActiveStep: 0,
      Steps: [{ ID: "break", Title: "Break it", Kind: "breakage", State: "active", Notes: "", Rating: 0, CanVerify: true, CanContinue: true,
        Last: { Status: "pass", Message: "the breakage is in place", Evidence: "", At: "" } }],
    }));
    expect(await screen.findByText("Passed")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Continue" })).toBeEnabled();
  });
});
