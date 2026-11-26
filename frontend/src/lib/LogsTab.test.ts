import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, cleanup, waitFor } from "@testing-library/svelte";
import userEvent from "@testing-library/user-event";

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
const svc = vi.hoisted(() => ({ Streams: vi.fn(), Open: vi.fn(), Close: vi.fn() }));
vi.mock("@wailsio/runtime", () => ({ Events: { On: bus.on } }));
vi.mock("../../bindings/github.com/pixambi/sev1situations/internal/gui/logservice.js", () => svc);

import LogsTab from "./LogsTab.svelte";

const node = (Name: string, State = "running") => ({ Name, State, Cluster: "primary", IP: "10.0.0.2", Actions: [] }) as never;

/** The ID the page chose for the stream it opened last, which is how the test addresses its events. */
const openedId = () => svc.Open.mock.calls.at(-1)![0] as string;
const send = (lines: string[], extra: Record<string, unknown> = {}) => bus.emit(`log:${openedId()}`, { Lines: lines, Ended: false, Reason: "", ...extra });

beforeEach(() => {
  svc.Streams.mockResolvedValue([{ ID: "operational", Label: "Operational log" }, { ID: "audit", Label: "Audit log" }]);
  svc.Open.mockResolvedValue(undefined);
  svc.Close.mockResolvedValue(undefined);
});
afterEach(() => {
  cleanup();
  bus.handlers.clear();
  vi.resetAllMocks();
});

async function setup(nodes = [node("node-1"), node("node-2")]) {
  const r = render(LogsTab, { nodes });
  await waitFor(() => expect(svc.Open).toHaveBeenCalled());
  return r;
}

describe("LogsTab", () => {
  it("opens the first log of the first node, with the listener in place first", async () => {
    await setup();
    expect(svc.Streams).toHaveBeenCalledWith("node-1");
    expect(svc.Open).toHaveBeenCalledWith(expect.stringMatching(/^L[a-z0-9]+$/), "node-1", "operational");
    expect(bus.handlers.get(`log:${openedId()}`)?.size).toBe(1);
    expect(screen.getByText("Waiting for the first line.")).toBeInTheDocument();
  });

  it("shows lines as they arrive, and counts them", async () => {
    await setup();
    send(["first line", "second line"]);
    expect(await screen.findByText("first line")).toBeInTheDocument();
    expect(screen.getByText("second line")).toBeInTheDocument();
    expect(screen.getByRole("status")).toHaveTextContent("2 of 2 lines");
  });

  it("lists only nodes that still have a container", async () => {
    await setup([node("node-1"), node("node-2", "removed"), node("node-3", "stopped")]);
    const options = screen.getAllByRole("option").map((o) => o.textContent);
    expect(options).toContain("node-1");
    expect(options).toContain("node-3 (stopped)");
    expect(options.join()).not.toContain("node-2");
  });

  it("switches log, closing the old stream and opening the new one", async () => {
    await setup();
    const first = openedId();
    send(["old"]);
    await screen.findByText("old");

    await userEvent.selectOptions(screen.getByLabelText("Log"), "audit");
    await waitFor(() => expect(svc.Open).toHaveBeenCalledTimes(2));
    expect(svc.Close).toHaveBeenCalledWith(first);
    expect(svc.Open).toHaveBeenLastCalledWith(expect.any(String), "node-1", "audit");
    expect(screen.queryByText("old")).not.toBeInTheDocument();
    expect(bus.handlers.get(`log:${first}`)?.size ?? 0).toBe(0);
  });

  it("switches node and asks that node for its logs", async () => {
    await setup();
    await userEvent.selectOptions(screen.getByLabelText("Node"), "node-2");
    await waitFor(() => expect(svc.Streams).toHaveBeenCalledWith("node-2"));
    await waitFor(() => expect(svc.Open).toHaveBeenLastCalledWith(expect.any(String), "node-2", "operational"));
  });

  it("closes its stream when the tab goes away", async () => {
    const { unmount } = await setup();
    const id = openedId();
    unmount();
    expect(svc.Close).toHaveBeenCalledWith(id);
    expect(bus.handlers.get(`log:${id}`)?.size ?? 0).toBe(0);
  });

  it("filters by text and highlights the match", async () => {
    await setup();
    send(["peer joined", "leader elected", "peer left"]);
    await screen.findByText("leader elected");

    await userEvent.type(screen.getByRole("searchbox"), "peer");
    await waitFor(() => expect(screen.queryByText("leader elected")).not.toBeInTheDocument());
    expect(document.querySelectorAll("mark")).toHaveLength(2);
    expect(document.querySelector("mark")?.textContent).toBe("peer");
    expect(screen.getByRole("status")).toHaveTextContent("2 of 3 lines");
  });

  it("filters by a regular expression, and says when it is not valid without losing the lines", async () => {
    await setup();
    send(["node-1 up", "node-22 up", "other"]);
    await screen.findByText("other");

    await userEvent.click(screen.getByLabelText("Regular expression"));
    await userEvent.type(screen.getByRole("searchbox"), "node-\\d+");
    await waitFor(() => expect(screen.queryByText("other")).not.toBeInTheDocument());

    await userEvent.clear(screen.getByRole("searchbox"));
    await userEvent.type(screen.getByRole("searchbox"), "(oops");
    expect(await screen.findByText(/not a valid regular expression/)).toBeInTheDocument();
    expect(await screen.findByText("other")).toBeInTheDocument();
  });

  it("filters by level", async () => {
    await setup();
    send(["[INFO] fine", "[WARN] hmm", "[ERROR] bad"]);
    await screen.findByText("[ERROR] bad");

    await userEvent.selectOptions(screen.getByLabelText("Level"), "warnings");
    await waitFor(() => expect(screen.queryByText("[INFO] fine")).not.toBeInTheDocument());
    expect(screen.getByText("[WARN] hmm")).toBeInTheDocument();

    await userEvent.selectOptions(screen.getByLabelText("Level"), "errors");
    await waitFor(() => expect(screen.queryByText("[WARN] hmm")).not.toBeInTheDocument());
    expect(screen.getByText("[ERROR] bad")).toBeInTheDocument();
  });

  it("says when no line matches", async () => {
    await setup();
    send(["something"]);
    await screen.findByText("something");
    await userEvent.type(screen.getByRole("searchbox"), "zzz");
    expect(await screen.findByText("No line matches the filter.")).toBeInTheDocument();
  });

  it("pauses so the view holds still, and catches up on resume", async () => {
    await setup();
    send(["a"]);
    await screen.findByText("a");

    await userEvent.click(screen.getByRole("button", { name: "Pause" }));
    send(["b", "c"]);
    await waitFor(() => expect(screen.getByRole("status")).toHaveTextContent("2 waiting"));
    expect(screen.queryByText("b")).not.toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "Resume" }));
    expect(await screen.findByText("b")).toBeInTheDocument();
    expect(screen.getByText("c")).toBeInTheDocument();
  });

  it("keeps only the newest lines past the cap and says how many were dropped", async () => {
    await setup();
    send(Array.from({ length: 5100 }, (_, i) => `line ${i}`));
    await waitFor(() => expect(screen.getByRole("status")).toHaveTextContent("5000 of 5000 lines, 100 older dropped"));
    expect(screen.queryByText("line 99")).not.toBeInTheDocument();
    expect(screen.getByText("line 100")).toBeInTheDocument();
    expect(screen.getByText("line 5099")).toBeInTheDocument();
  });

  it("clears the view", async () => {
    await setup();
    send(["a"]);
    await screen.findByText("a");
    await userEvent.click(screen.getByRole("button", { name: "Clear" }));
    expect(screen.queryByText("a")).not.toBeInTheDocument();
  });

  it("tells the instructor when the log ends, and why", async () => {
    await setup();
    send([], { Ended: true, Reason: "" });
    expect(await screen.findByText("This log has ended")).toBeInTheDocument();
    send([], { Ended: true, Reason: "connection reset" });
    expect(await screen.findByText("connection reset")).toBeInTheDocument();
  });

  it("says when a log could not be opened", async () => {
    svc.Open.mockRejectedValueOnce(new Error("node-1 has been removed, so it has no log."));
    render(LogsTab, { nodes: [node("node-1")] });
    expect(await screen.findByText(/has been removed/)).toBeInTheDocument();
  });

  it("says so when no node has a log", () => {
    render(LogsTab, { nodes: [node("node-1", "removed")] });
    expect(screen.getByText("There is no node with a log to show.")).toBeInTheDocument();
    expect(svc.Open).not.toHaveBeenCalled();
  });
});
