import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, cleanup, waitFor } from "@testing-library/svelte";
import userEvent from "@testing-library/user-event";
import type { ScenarioDetail } from "../../bindings/github.com/pixambi/sev1situations/internal/gui/models.js";

const get = vi.hoisted(() => vi.fn());
const versions = vi.hoisted(() => vi.fn());
const start = vi.hoisted(() => vi.fn());
vi.mock("../../bindings/github.com/pixambi/sev1situations/internal/gui/scenarioservice.js", () => ({ Get: get, Versions: versions }));
vi.mock("../../bindings/github.com/pixambi/sev1situations/internal/gui/runservice.js", () => ({ Start: start, Current: vi.fn() }));
vi.mock("@wailsio/runtime", () => ({ Events: { On: () => () => {} } }));

import StartForm from "./StartForm.svelte";
import { run } from "./run.svelte.js";

const detail = (over: Partial<ScenarioDetail> = {}): ScenarioDetail => ({
  ID: "sandbox", Name: "Vault Sandbox", Summary: "A cluster to explore.", Product: "vault", Editions: ["community", "enterprise"],
  Difficulty: "beginner", EstimatedMinutes: 60, Tags: [], Source: "built-in", NeedsLicense: false,
  Objectives: ["Explore"], Prerequisites: [], Steps: [], Licensed: true, Instructor: "Ada",
  Parameters: [
    { ID: "version", Type: "version", Label: "Version", Help: "", Default: null, Product: "vault", Min: 0, Max: 0, Options: null },
    { ID: "edition", Type: "edition", Label: "Edition", Help: "", Default: "community", Product: "", Min: 0, Max: 0, Options: ["community", "enterprise"] },
    { ID: "nodes", Type: "int", Label: "Nodes", Help: "How many.", Default: 3, Product: "", Min: 1, Max: 9, Options: null },
  ],
  ...over,
});

function setup(d: ScenarioDetail = detail()) {
  get.mockResolvedValue(d);
  const handlers = { onstarted: vi.fn(), oncancel: vi.fn(), onopensettings: vi.fn() };
  render(StartForm, { scenarioId: d.ID, ...handlers });
  return handlers;
}

beforeEach(() => {
  versions.mockImplementation(async (_p: string, edition: string) => (edition === "enterprise" ? ["2.2.2+ent"] : ["2.2.2", "2.2.1"]));
  start.mockResolvedValue({});
  run.snapshot = null;
});
afterEach(() => {
  cleanup();
  vi.resetAllMocks();
});

const trainee = () => screen.findByLabelText("Trainee's name");

describe("StartForm", () => {
  it("builds the form from the scenario, with the saved instructor and the newest version chosen", async () => {
    setup();
    expect(await screen.findByRole("heading", { name: "Start Vault Sandbox" })).toBeInTheDocument();

    expect(await screen.findByLabelText("Instructor's name")).toHaveValue("Ada");
    expect(screen.getByLabelText("Nodes")).toHaveValue(3);
    expect(screen.getByLabelText("Edition")).toHaveValue("community");
    await waitFor(() => expect(screen.getByLabelText("Version")).toHaveValue("2.2.2"));
    expect(versions).toHaveBeenCalledWith("vault", "community");
  });

  it("lists the versions again when the edition changes", async () => {
    setup();
    await userEvent.selectOptions(await screen.findByLabelText("Edition"), "enterprise");
    await waitFor(() => expect(screen.getByLabelText("Version")).toHaveValue("2.2.2+ent"));
    expect(versions).toHaveBeenLastCalledWith("vault", "enterprise");
  });

  it("sends the form as typed, with the numbers as numbers", async () => {
    const h = setup();
    await userEvent.type(await trainee(), "  Grace ");
    await waitFor(() => expect(screen.getByLabelText("Version")).toHaveValue("2.2.2"));
    run.snapshot = { Status: "active", Events: [] } as never; // the engine reports a run once Start returns
    await userEvent.click(screen.getByRole("button", { name: "Deploy" }));

    await waitFor(() => expect(start).toHaveBeenCalledOnce());
    expect(start).toHaveBeenCalledWith({
      ScenarioID: "sandbox", Instructor: "Ada", Trainee: "Grace",
      Params: { version: "2.2.2", edition: "community", nodes: 3 },
    });
    await waitFor(() => expect(h.onstarted).toHaveBeenCalledOnce());
  });

  it("shows what is missing and does not start", async () => {
    setup();
    await screen.findByLabelText("Trainee's name");
    await waitFor(() => expect(screen.getByLabelText("Version")).toHaveValue("2.2.2"));
    await userEvent.click(screen.getByRole("button", { name: "Deploy" }));

    expect(await screen.findByText("Enter the trainee's name.")).toBeInTheDocument();
    expect(start).not.toHaveBeenCalled();
  });

  it("rejects a node count outside the scenario's range", async () => {
    setup();
    const nodes = await screen.findByLabelText("Nodes");
    await userEvent.clear(nodes);
    await userEvent.type(nodes, "12");
    await userEvent.type(await trainee(), "Grace");
    await userEvent.click(screen.getByRole("button", { name: "Deploy" }));

    expect(await screen.findByText("Nodes must be from 1 to 9.")).toBeInTheDocument();
    expect(start).not.toHaveBeenCalled();
  });

  it("will not deploy an enterprise edition without a license, and links to Settings", async () => {
    const h = setup(detail({ Licensed: false }));
    await userEvent.selectOptions(await screen.findByLabelText("Edition"), "enterprise");

    expect(await screen.findByText("A license is needed")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Deploy" })).toBeDisabled();
    await userEvent.click(screen.getByRole("button", { name: "Add it in Settings" }));
    expect(h.onopensettings).toHaveBeenCalledOnce();

    await userEvent.selectOptions(screen.getByLabelText("Edition"), "community");
    await waitFor(() => expect(screen.getByRole("button", { name: "Deploy" })).toBeEnabled());
  });

  it("explains a failed version lookup and tries again", async () => {
    versions.mockRejectedValueOnce(new Error("The versions could not be listed. Check that you are online."));
    setup();
    expect(await screen.findByText(/could not be listed/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Deploy" })).toBeDisabled();

    await userEvent.click(screen.getByRole("button", { name: "Try again" }));
    await waitFor(() => expect(screen.getByLabelText("Version")).toHaveValue("2.2.2"));
    expect(screen.queryByText(/could not be listed/)).not.toBeInTheDocument();
  });

  it("shows the error when starting fails, and keeps the form", async () => {
    start.mockRejectedValueOnce(new Error("Another action is still running."));
    const h = setup();
    await userEvent.type(await trainee(), "Grace");
    await waitFor(() => expect(screen.getByLabelText("Version")).toHaveValue("2.2.2"));
    await userEvent.click(screen.getByRole("button", { name: "Deploy" }));

    expect(await screen.findByText(/Another action is still running/)).toBeInTheDocument();
    expect(h.onstarted).not.toHaveBeenCalled();
    expect(screen.getByRole("button", { name: "Deploy" })).toBeEnabled();
  });

  it("hands over to the run page even when the deployment failed, because the run stays", async () => {
    start.mockRejectedValueOnce(new Error("deploy failed"));
    const h = setup();
    await userEvent.type(await trainee(), "Grace");
    await waitFor(() => expect(screen.getByLabelText("Version")).toHaveValue("2.2.2"));
    run.snapshot = { Status: "deploy-failed", Events: [] } as never;
    await userEvent.click(screen.getByRole("button", { name: "Deploy" }));

    await waitFor(() => expect(h.onstarted).toHaveBeenCalledOnce());
  });

  it("goes back on Cancel, and says so when the scenario cannot be opened", async () => {
    const h = setup();
    await userEvent.click(await screen.findByRole("button", { name: "Cancel" }));
    expect(h.oncancel).toHaveBeenCalledOnce();

    cleanup();
    get.mockRejectedValueOnce(new Error('There is no scenario "gone".'));
    render(StartForm, { scenarioId: "gone", ...h });
    expect(await screen.findByText(/There is no scenario "gone"/)).toBeInTheDocument();
  });
});
