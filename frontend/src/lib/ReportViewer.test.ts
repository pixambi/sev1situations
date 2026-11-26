import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, cleanup, waitFor } from "@testing-library/svelte";
import userEvent from "@testing-library/user-event";

const svc = vi.hoisted(() => ({ Page: vi.fn(), Export: vi.fn(), List: vi.fn(), Delete: vi.fn() }));
vi.mock("../../bindings/github.com/pixambi/sev1situations/internal/gui/reportservice.js", () => svc);

import ReportViewer from "./ReportViewer.svelte";

beforeEach(() => {
  svc.Page.mockResolvedValue("/_reports/20261126-091530-abc.html");
  svc.Export.mockResolvedValue({ Path: "/tmp/r.html", Cancelled: false });
});
afterEach(() => {
  cleanup();
  vi.resetAllMocks();
});

describe("ReportViewer", () => {
  it("shows the report in a frame that has no permissions at all", async () => {
    render(ReportViewer, { id: "20261126-091530-abc", onback: vi.fn() });
    const frame = (await screen.findByTitle("The report")) as HTMLIFrameElement;
    expect(frame.getAttribute("src")).toBe("/_reports/20261126-091530-abc.html");
    expect(frame.getAttribute("sandbox")).toBe(""); // an empty value forbids scripts, forms, popups and same-origin access
    expect(svc.Page).toHaveBeenCalledWith("20261126-091530-abc");
  });

  it("goes back", async () => {
    const onback = vi.fn();
    render(ReportViewer, { id: "x", onback });
    await userEvent.click(screen.getByRole("button", { name: "Back to the reports" }));
    expect(onback).toHaveBeenCalledOnce();
  });

  it("exports the page or the data from the viewer", async () => {
    render(ReportViewer, { id: "x", onback: vi.fn() });
    await screen.findByTitle("The report");
    await userEvent.click(screen.getByRole("button", { name: "Export the page" }));
    await waitFor(() => expect(svc.Export).toHaveBeenCalledWith("x", "html"));
    expect(await screen.findByText("Saved to /tmp/r.html.")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Export the raw data" }));
    await waitFor(() => expect(svc.Export).toHaveBeenLastCalledWith("x", "data"));
  });

  it("says why a report cannot be shown, and offers no export for it", async () => {
    svc.Page.mockRejectedValueOnce(new Error("That report no longer exists. Refresh the list."));
    render(ReportViewer, { id: "gone", onback: vi.fn() });
    expect(await screen.findByText(/no longer exists/)).toBeInTheDocument();
    expect(screen.queryByTitle("The report")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Export the page" })).toBeDisabled();
  });

  it("shows the next report when the ID changes, not the old one", async () => {
    svc.Page.mockImplementation(async (id: string) => `/_reports/${id}.html`);
    const { rerender } = render(ReportViewer, { id: "one", onback: vi.fn() });
    expect(((await screen.findByTitle("The report")) as HTMLIFrameElement).getAttribute("src")).toBe("/_reports/one.html");
    await rerender({ id: "two", onback: vi.fn() });
    await waitFor(() => expect(((screen.getByTitle("The report")) as HTMLIFrameElement).getAttribute("src")).toBe("/_reports/two.html"));
  });
});
