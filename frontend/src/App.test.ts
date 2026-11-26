import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, cleanup, waitFor } from "@testing-library/svelte";
import { tick } from "svelte";
import userEvent from "@testing-library/user-event";

import App from "./App.svelte";

// The Go side is not there in a unit test, so the bindings and the event bus are replaced.
const prereq = vi.hoisted(() => vi.fn());
vi.mock("../bindings/github.com/pixambi/sev1situations/internal/gui/systemservice.js", () => ({ Prereq: prereq }));
vi.mock("../bindings/github.com/pixambi/sev1situations/internal/gui/scenarioservice.js", () => ({
  List: vi.fn().mockResolvedValue({ Scenarios: [], Problems: [] }),
}));
const current = vi.hoisted(() => vi.fn());
vi.mock("../bindings/github.com/pixambi/sev1situations/internal/gui/runservice.js", () => ({ Current: current, Guide: vi.fn() }));
vi.mock("@wailsio/runtime", () => ({ Events: { On: () => () => {} }, Browser: { OpenURL: vi.fn() } }));

// A controllable stand-in for the system's colour scheme.
function systemScheme(initiallyDark: boolean) {
  const listeners: Array<(e: MediaQueryListEvent) => void> = [];
  const mq = {
    matches: initiallyDark, media: "(prefers-color-scheme: dark)", onchange: null,
    addEventListener: (_: string, l: (e: MediaQueryListEvent) => void) => listeners.push(l),
    removeEventListener: (_: string, l: (e: MediaQueryListEvent) => void) => listeners.splice(listeners.indexOf(l), 1),
    addListener: () => {}, removeListener: () => {}, dispatchEvent: () => false,
  };
  vi.stubGlobal("matchMedia", () => mq);
  return { change: (dark: boolean) => listeners.forEach((l) => l({ matches: dark } as MediaQueryListEvent)), listeners };
}

beforeEach(() => {
  prereq.mockResolvedValue({ Ready: true, Message: "", Notices: [] });
  current.mockResolvedValue({ Status: "idle", ActiveStep: -1 });
});

afterEach(() => {
  cleanup();
  prereq.mockReset();
  current.mockReset();
  vi.unstubAllGlobals();
  document.documentElement.removeAttribute("theme");
});

describe("App", () => {
  it("shows the application name in the header and the page", () => {
    systemScheme(false);
    render(App);

    expect(screen.getByRole("banner")).toHaveTextContent("Sev1");
    expect(screen.getByRole("banner")).toHaveTextContent("Situations");
    expect(screen.getByRole("heading", { name: "Sev1 Situations" })).toBeInTheDocument();
  });

  it("checks Podman on load and says so when it is not ready, and Retry asks again", async () => {
    systemScheme(false);
    prereq.mockResolvedValueOnce({ Ready: false, Message: "The Podman machine is stopped.", Notices: [] });
    render(App);

    expect(await screen.findByText("The Podman machine is stopped.")).toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "Retry" }));
    await waitFor(() => expect(screen.queryByText("The Podman machine is stopped.")).not.toBeInTheDocument());
    expect(prereq).toHaveBeenCalledTimes(2);
  });

  it("shows the scenarios page once the catalogue has loaded", async () => {
    systemScheme(false);
    render(App);
    expect(await screen.findByRole("heading", { name: "Scenarios" })).toBeInTheDocument();
    expect(await screen.findByText("No scenarios were found.")).toBeInTheDocument();
  });

  it.each([
    [false, "white"],
    [true, "g100"],
  ])("starts in the theme the system uses (dark: %s)", (dark, theme) => {
    systemScheme(dark);
    render(App);
    expect(document.documentElement.getAttribute("theme")).toBe(theme);
  });

  it("follows the system when it changes, and stops following when it is closed", async () => {
    const system = systemScheme(false);
    render(App);
    expect(document.documentElement.getAttribute("theme")).toBe("white");

    system.change(true);
    await tick();
    expect(document.documentElement.getAttribute("theme")).toBe("g100");

    system.change(false);
    await tick();
    expect(document.documentElement.getAttribute("theme")).toBe("white");

    cleanup();
    expect(system.listeners).toHaveLength(0); // no listener is left behind
  });

  it("shows the run while one exists, and lets the instructor look at the catalogue and come back", async () => {
    systemScheme(false);
    current.mockResolvedValue({
      Status: "active", RunID: "abc", ScenarioID: "demo", ScenarioName: "Demo scenario", Instructor: "Ada", Trainee: "Grace",
      StartedAt: "2026-11-26T09:00:00Z", EndedAt: "0001-01-01T00:00:00Z", ActiveStep: -1, Steps: [], Events: [], Nodes: [],
    });
    render(App);

    expect(await screen.findByRole("heading", { name: "Demo scenario" })).toBeInTheDocument();

    await userEvent.click(screen.getByRole("tab", { name: "Scenarios" }));
    expect(await screen.findByRole("heading", { name: "Scenarios", level: 2 })).toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "Demo scenario" })).not.toBeInTheDocument();

    await userEvent.click(screen.getByRole("tab", { name: "Current run" }));
    expect(await screen.findByRole("heading", { name: "Demo scenario" })).toBeInTheDocument();
  });

  it("offers no page switcher when there is no run", async () => {
    systemScheme(false);
    render(App);
    await screen.findByRole("heading", { name: "Scenarios", level: 2 });
    expect(screen.queryByRole("tab", { name: "Current run" })).not.toBeInTheDocument();
  });
});
