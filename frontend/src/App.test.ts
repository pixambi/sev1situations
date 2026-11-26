import { describe, it, expect, vi, afterEach } from "vitest";
import { render, screen, cleanup } from "@testing-library/svelte";
import { tick } from "svelte";

import App from "./App.svelte";

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

afterEach(() => {
  cleanup();
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
    expect(screen.getByText("No scenario is running.")).toBeInTheDocument();
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
});
