import { describe, it, expect, vi, afterEach } from "vitest";
import { render, screen, cleanup } from "@testing-library/svelte";
import userEvent from "@testing-library/user-event";

// The pane is tested on its own, so here it is only a marker.
vi.mock("./TerminalPane.svelte", async () => {
  const Stub = (await import("./TerminalPaneStub.svelte")).default;
  return { default: Stub };
});

import TerminalsTab from "./TerminalsTab.svelte";

afterEach(cleanup);

const sessions = [
  { id: "a", node: "node-1" },
  { id: "b", node: "node-2" },
  { id: "c", node: "node-1" },
];

function setup(over: Record<string, unknown> = {}) {
  const handlers = { onselect: vi.fn(), onclose: vi.fn() };
  render(TerminalsTab, { sessions, selected: "b", visible: true, ...handlers, ...over });
  return handlers;
}

describe("TerminalsTab", () => {
  it("points the instructor to the Cluster tab when nothing is open", () => {
    setup({ sessions: [] });
    expect(screen.getByText(/from a node's card on the Cluster tab/)).toBeInTheDocument();
  });

  it("keeps every terminal in the page and shows only the selected one", () => {
    setup();
    const panes = screen.getAllByTestId("pane");
    expect(panes.map((p) => p.dataset.id)).toEqual(["a", "b", "c"]);
    expect(panes.map((p) => p.dataset.visible)).toEqual(["false", "true", "false"]);
    expect(panes.map((p) => (p.parentElement as HTMLElement).hidden)).toEqual([true, false, true]);
  });

  it("tells two terminals on one node apart", () => {
    setup();
    expect(screen.getByRole("tab", { name: "node-1 (1)" })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "node-1 (2)" })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "node-2" })).toBeInTheDocument();
  });

  it("selects the terminal that was chosen, and closes the one on show", async () => {
    const h = setup();
    await userEvent.click(screen.getByRole("tab", { name: "node-1 (2)" }));
    expect(h.onselect).toHaveBeenCalledWith("c");
    await userEvent.click(screen.getByRole("button", { name: "Close terminal" }));
    expect(h.onclose).toHaveBeenCalledWith("b");
  });

  it("marks no pane visible while the tab is not on screen", () => {
    setup({ visible: false });
    expect(screen.getAllByTestId("pane").every((p) => p.dataset.visible === "false")).toBe(true);
  });
});
