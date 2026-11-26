import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, cleanup, waitFor, within } from "@testing-library/svelte";
import userEvent from "@testing-library/user-event";

const svc = vi.hoisted(() => ({ List: vi.fn(), Delete: vi.fn(), Export: vi.fn(), Page: vi.fn() }));
vi.mock("../../bindings/github.com/pixambi/sev1situations/internal/gui/reportservice.js", () => svc);

import ReportsPage from "./ReportsPage.svelte";

const summary = (id: string, over: Record<string, unknown> = {}) => ({
  id, scenarioName: "Lost Quorum", instructor: "Ada", trainee: "Grace Hopper",
  startedAt: "2026-11-26T09:15:30Z", endedAt: "2026-11-26T10:00:00Z", result: "completed", ...over,
});
const list = (reports: unknown[] = [], Problems: string[] = []) => ({ Reports: reports, Problems });

beforeEach(() => {
  svc.List.mockResolvedValue(list([summary("a"), summary("b", { trainee: "Alan Turing", scenarioName: "Sandbox", result: "ended early" })]));
  svc.Delete.mockResolvedValue(undefined);
  svc.Export.mockResolvedValue({ Path: "/Users/me/Desktop/report.html", Cancelled: false });
});
afterEach(() => {
  cleanup();
  vi.resetAllMocks();
});

async function setup() {
  const onopen = vi.fn();
  render(ReportsPage, { onopen });
  await screen.findByRole("heading", { name: "Reports" });
  return { onopen };
}

describe("ReportsPage", () => {
  it("lists each report with its scenario, people and outcome", async () => {
    await setup();
    expect(await screen.findByText("Grace Hopper")).toBeInTheDocument();
    expect(screen.getByText("Alan Turing")).toBeInTheDocument();
    expect(screen.getByText("Completed")).toBeInTheDocument();
    expect(screen.getByText("Ended early")).toBeInTheDocument();
    expect(screen.getAllByText("Ada")).toHaveLength(2);
  });

  it("says when there are no reports and when to expect one", async () => {
    svc.List.mockResolvedValue(list());
    await setup();
    expect(await screen.findByText(/no reports yet/)).toBeInTheDocument();
    expect(screen.getByText(/saved when a scenario ends, even one ended early/)).toBeInTheDocument();
  });

  it("opens the report that was chosen", async () => {
    const { onopen } = await setup();
    await userEvent.click(await screen.findByRole("button", { name: /Open the report for Alan Turing/ }));
    expect(onopen).toHaveBeenCalledExactlyOnceWith("b");
  });

  it("exports the page or the raw data and says where it was saved", async () => {
    await setup();
    await userEvent.click(await screen.findByRole("button", { name: /More for Grace Hopper/ }));
    await userEvent.click(screen.getByRole("menuitem", { name: "Export the page" }));
    await waitFor(() => expect(svc.Export).toHaveBeenCalledWith("a", "html"));
    expect(await screen.findByText(/saved to \/Users\/me\/Desktop\/report.html/)).toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: /More for Grace Hopper/ }));
    await userEvent.click(screen.getByRole("menuitem", { name: "Export the raw data" }));
    await waitFor(() => expect(svc.Export).toHaveBeenLastCalledWith("a", "data"));
  });

  it("says nothing when the save dialog is cancelled, and explains a failed export", async () => {
    svc.Export.mockResolvedValueOnce({ Path: "", Cancelled: true });
    await setup();
    await userEvent.click(await screen.findByRole("button", { name: /More for Grace Hopper/ }));
    await userEvent.click(screen.getByRole("menuitem", { name: "Export the page" }));
    await waitFor(() => expect(svc.Export).toHaveBeenCalledOnce());
    expect(screen.queryByText("Saved")).not.toBeInTheDocument();

    svc.Export.mockRejectedValueOnce(new Error("the folder is read-only"));
    await userEvent.click(screen.getByRole("button", { name: /More for Grace Hopper/ }));
    await userEvent.click(screen.getByRole("menuitem", { name: "Export the page" }));
    expect(await screen.findByText(/the folder is read-only/)).toBeInTheDocument();
  });

  it("asks before deleting, names the report, and deletes only when confirmed", async () => {
    await setup();
    await userEvent.click(await screen.findByRole("button", { name: /More for Alan Turing/ }));
    await userEvent.click(screen.getByRole("menuitem", { name: "Delete" }));

    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText(/the report of Alan Turing on Sandbox/)).toBeInTheDocument();
    expect(svc.Delete).not.toHaveBeenCalled();
    await userEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
    expect(svc.Delete).not.toHaveBeenCalled();

    await userEvent.click(screen.getByRole("button", { name: /More for Alan Turing/ }));
    await userEvent.click(screen.getByRole("menuitem", { name: "Delete" }));
    svc.List.mockResolvedValue(list([summary("a")]));
    await userEvent.click(within(await screen.findByRole("dialog")).getByRole("button", { name: "Delete report" }));

    await waitFor(() => expect(svc.Delete).toHaveBeenCalledExactlyOnceWith("b"));
    await waitFor(() => expect(screen.queryByText("Alan Turing")).not.toBeInTheDocument());
    expect(screen.getByText("Grace Hopper")).toBeInTheDocument();
  });

  it("reads the list again and says why when a delete fails", async () => {
    svc.Delete.mockRejectedValueOnce(new Error("That report no longer exists. Refresh the list."));
    await setup();
    await userEvent.click(await screen.findByRole("button", { name: /More for Grace Hopper/ }));
    await userEvent.click(screen.getByRole("menuitem", { name: "Delete" }));
    await userEvent.click(within(await screen.findByRole("dialog")).getByRole("button", { name: "Delete report" }));
    expect(await screen.findByText(/no longer exists/)).toBeInTheDocument();
    expect(svc.List).toHaveBeenCalledTimes(2);
  });

  it("reports a file it could not read without hiding the others", async () => {
    svc.List.mockResolvedValue(list([summary("a")], ["broken.json: report is damaged: syntax error at byte 9"]));
    await setup();
    expect(await screen.findByText(/broken.json: report is damaged/)).toBeInTheDocument();
    expect(screen.getByText("Grace Hopper")).toBeInTheDocument();
  });

  it("explains when the list cannot be read and tries again", async () => {
    svc.List.mockRejectedValueOnce(new Error("the folder cannot be read"));
    await setup();
    expect(await screen.findByText(/the folder cannot be read/)).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(await screen.findByText("Grace Hopper")).toBeInTheDocument();
  });
});
