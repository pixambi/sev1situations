import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, cleanup, waitFor, within } from "@testing-library/svelte";
import userEvent from "@testing-library/user-event";

const get = vi.hoisted(() => vi.fn());
const nodeAction = vi.hoisted(() => vi.fn());
vi.mock("../../bindings/github.com/pixambi/sev1situations/internal/gui/clusterservice.js", () => ({ Get: get }));
vi.mock("../../bindings/github.com/pixambi/sev1situations/internal/gui/runservice.js", () => ({ NodeAction: nodeAction }));
vi.mock("@wailsio/runtime", () => ({ Clipboard: { SetText: vi.fn() } }));

import ClusterTab from "./ClusterTab.svelte";

const state = (over: Record<string, unknown> = {}) => ({
  Sections: [
    {
      Title: "Primary cluster",
      Facts: [{ Label: "API address", Value: "http://127.0.0.1:8200", Secret: false }, { Label: "Root token", Value: "hvs.TopSecret", Secret: true }],
      Nodes: [
        { Name: "node-1", IP: "10.89.1.2", State: "running", Details: [{ Label: "Sealed", Value: "no", Secret: false }], Actions: ["stop", "remove"] },
        { Name: "node-2", IP: "", State: "removed", Details: [{ Label: "Sealed", Value: "unknown", Secret: false }], Actions: ["revive"] },
      ],
    },
  ],
  ...over,
});

beforeEach(() => {
  get.mockResolvedValue(state());
  nodeAction.mockResolvedValue({});
});
afterEach(() => {
  cleanup();
  vi.resetAllMocks();
});

describe("ClusterTab", () => {
  it("shows the sections, their facts and each node with its state and address", async () => {
    render(ClusterTab, { version: 0 });
    expect(await screen.findByRole("heading", { name: "Primary cluster" })).toBeInTheDocument();
    expect(screen.getByText("http://127.0.0.1:8200")).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "node-1" })).toBeInTheDocument();
    expect(screen.getByText("10.89.1.2")).toBeInTheDocument();
    expect(screen.getByText("running")).toBeInTheDocument();
    expect(screen.getByText("removed")).toBeInTheDocument();
  });

  it("masks the credentials", async () => {
    render(ClusterTab, { version: 0 });
    await screen.findByRole("heading", { name: "Primary cluster" });
    expect(document.body.textContent).not.toContain("hvs.TopSecret");
    await userEvent.click(screen.getByRole("button", { name: "Show Root token" }));
    expect(screen.getByText("hvs.TopSecret")).toBeInTheDocument();
  });

  it("offers each node only the actions it can take now", async () => {
    render(ClusterTab, { version: 0 });
    await screen.findByRole("heading", { name: "node-1" });
    expect(screen.getByRole("button", { name: /Stop node-1/ })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Revive node-2/ })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Start node-1/ })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Remove node-2/ })).not.toBeInTheDocument();
  });

  it("stops and revives straight away", async () => {
    render(ClusterTab, { version: 0 });
    await userEvent.click(await screen.findByRole("button", { name: /Stop node-1/ }));
    expect(nodeAction).toHaveBeenCalledWith("node-1", "stop");
    await userEvent.click(screen.getByRole("button", { name: /Revive node-2/ }));
    expect(nodeAction).toHaveBeenLastCalledWith("node-2", "revive");
  });

  it("asks before removing, and removes only when confirmed", async () => {
    render(ClusterTab, { version: 0 });
    await userEvent.click(await screen.findByRole("button", { name: /Remove node-1/ }));

    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText(/Its data is lost/)).toBeInTheDocument();
    expect(nodeAction).not.toHaveBeenCalled();
    await userEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
    expect(nodeAction).not.toHaveBeenCalled();

    await userEvent.click(screen.getByRole("button", { name: /Remove node-1/ }));
    await userEvent.click(within(await screen.findByRole("dialog")).getByRole("button", { name: "Remove node" }));
    await waitFor(() => expect(nodeAction).toHaveBeenCalledExactlyOnceWith("node-1", "remove"));
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  });

  it("says what went wrong when an action fails", async () => {
    nodeAction.mockRejectedValueOnce(new Error("Another action is still running."));
    render(ClusterTab, { version: 0 });
    await userEvent.click(await screen.findByRole("button", { name: /Stop node-1/ }));
    expect(await screen.findByText(/Another action is still running/)).toBeInTheDocument();
  });

  it("reads the deployment again when the run changes, and on Refresh", async () => {
    const { rerender } = render(ClusterTab, { version: 0 });
    await screen.findByRole("heading", { name: "Primary cluster" });
    expect(get).toHaveBeenCalledTimes(1);

    await rerender({ version: 1 });
    await waitFor(() => expect(get).toHaveBeenCalledTimes(2));

    await userEvent.click(screen.getByRole("button", { name: "Refresh" }));
    await waitFor(() => expect(get).toHaveBeenCalledTimes(3));
  });

  it("keeps the newest answer when an older read finishes last", async () => {
    let slow!: (v: unknown) => void;
    get.mockReturnValueOnce(new Promise((resolve) => (slow = resolve)));
    const { rerender } = render(ClusterTab, { version: 0 });

    get.mockResolvedValueOnce(state({ Sections: [{ Title: "Newer cluster", Facts: [], Nodes: [] }] }));
    await rerender({ version: 1 });
    expect(await screen.findByRole("heading", { name: "Newer cluster" })).toBeInTheDocument();

    slow(state({ Sections: [{ Title: "Older cluster", Facts: [], Nodes: [] }] }));
    await new Promise((r) => setTimeout(r, 20));
    expect(screen.queryByRole("heading", { name: "Older cluster" })).not.toBeInTheDocument();
  });

  it("explains a failed read with a way to try again, and says when nothing is deployed", async () => {
    get.mockRejectedValueOnce(new Error("The container runtime is not ready."));
    const { unmount } = render(ClusterTab, { version: 0 });
    expect(await screen.findByText(/container runtime is not ready/)).toBeInTheDocument();
    unmount();

    get.mockResolvedValue({ Sections: [] });
    render(ClusterTab, { version: 0 });
    expect(await screen.findByText("There is nothing deployed to show yet.")).toBeInTheDocument();
  });
});
