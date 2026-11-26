import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, cleanup, waitFor, within } from "@testing-library/svelte";
import userEvent from "@testing-library/user-event";
import type { Snapshot, StepView } from "../../bindings/github.com/pixambi/sev1situations/internal/engine/models.js";

const svc = vi.hoisted(() => ({
  Guide: vi.fn(), RunAction: vi.fn(), Verify: vi.fn(), Continue: vi.fn(), SaveNotes: vi.fn(),
  SetRating: vi.fn(), Rollback: vi.fn(), End: vi.fn(), Retry: vi.fn(),
}));
vi.mock("../../bindings/github.com/pixambi/sev1situations/internal/gui/runservice.js", () => svc);
vi.mock("@wailsio/runtime", () => ({ Browser: { OpenURL: vi.fn() }, Events: { On: () => () => {} } }));

import RunPage from "./RunPage.svelte";

// The generated types use enums for states, which a test spells as plain strings.
const step = (id: string, over: Record<string, unknown> = {}): StepView => ({
  ID: id, Title: `Step ${id}`, Kind: "breakage", Guide: `steps/${id}.md`, State: "locked", Verified: false, ResetPoint: false,
  Started: "", Ended: "", Elapsed: 0, Attempts: 0, Notes: "", Rating: 0, Last: null,
  CanApply: false, CanVerify: false, CanContinue: false, CanRollBack: false, ...over,
}) as StepView;

const snap = (over: Record<string, unknown> = {}): Snapshot => ({
  Status: "active", RunID: "abc", ScenarioID: "demo", ScenarioName: "Demo scenario", Instructor: "Ada", Trainee: "Grace", Params: {},
  StartedAt: "2026-11-26T09:00:00Z", EndedAt: "0001-01-01T00:00:00Z", Result: "", DeployError: "", ActiveStep: 0,
  Nodes: [], Steps: [], Events: [], ...over,
}) as Snapshot;

beforeEach(() => {
  svc.Guide.mockResolvedValue({ Briefing: "Tell them Vault is down.", Hints: "Check the peers.", Solution: "Recover.", References: "" });
  for (const k of ["RunAction", "Verify", "Continue", "SaveNotes", "SetRating", "Rollback", "End", "Retry"] as const) svc[k].mockResolvedValue({});
});
afterEach(() => {
  cleanup();
  vi.resetAllMocks();
});

const withActive = (over: Record<string, unknown> = {}, extra: Record<string, unknown> = {}) =>
  snap({ Steps: [step("break", { State: "active", CanApply: true, ...over }), step("fix")], ...extra });

describe("RunPage header", () => {
  it("shows the scenario, the people and the elapsed time", () => {
    render(RunPage, { snapshot: withActive() });
    expect(screen.getByRole("heading", { name: "Demo scenario" })).toBeInTheDocument();
    expect(screen.getByText("Ada")).toBeInTheDocument();
    expect(screen.getByText("Grace")).toBeInTheDocument();
    expect(screen.getByText("Elapsed")).toBeInTheDocument();
  });

  it("offers Roll back only when a step allows it", () => {
    const { unmount } = render(RunPage, { snapshot: withActive() });
    expect(screen.queryByRole("button", { name: "Roll back" })).not.toBeInTheDocument();
    unmount();
    render(RunPage, { snapshot: snap({ Steps: [step("a", { State: "complete", CanRollBack: true }), step("b", { State: "active" })], ActiveStep: 1 }) });
    expect(screen.getByRole("button", { name: "Roll back" })).toBeInTheDocument();
  });
});

describe("RunPage deployment", () => {
  it("shows deployment progress while deploying, with the buttons off", () => {
    render(RunPage, {
      snapshot: snap({
        Status: "deploying", Steps: [step("break")], ActiveStep: -1,
        Events: [{ Kind: "deploy-progress", Message: "creating the network" }, { Kind: "deploy-progress", Message: "starting node-1" }] as never,
      }),
    });
    expect(screen.getByText("starting node-1")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "End scenario" })).toBeDisabled();
  });

  it("shows why a deployment failed and offers Retry and End", async () => {
    render(RunPage, { snapshot: snap({ Status: "deploy-failed", DeployError: "the image could not be pulled", Steps: [step("break")], ActiveStep: -1 }) });
    expect(screen.getByText("The deployment failed")).toBeInTheDocument();
    expect(screen.getByText("the image could not be pulled")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(svc.Retry).toHaveBeenCalledOnce();
    expect(screen.getByRole("button", { name: "End scenario" })).toBeEnabled();
  });

  it("says a scenario with no steps is an open sandbox", () => {
    render(RunPage, { snapshot: snap({ Steps: [], ActiveStep: -1 }) });
    expect(screen.getByText("This is an open sandbox")).toBeInTheDocument();
  });

  it("says every step is complete once the last one is", () => {
    render(RunPage, { snapshot: snap({ Status: "finished", ActiveStep: -1, Steps: [step("break", { State: "complete" })] }) });
    expect(screen.getByText("Every step is complete")).toBeInTheDocument();
    expect(screen.getByText("Completed steps")).toBeInTheDocument();
  });
});

describe("the active step", () => {
  it("shows the briefing, and keeps hints and the solution behind their own headings", async () => {
    render(RunPage, { snapshot: withActive() });
    expect(await screen.findByText("Tell them Vault is down.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Hints" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Solution" })).toBeInTheDocument();
    expect(svc.Guide).toHaveBeenCalledWith("break");
  });

  it("explains when the guidance cannot be read", async () => {
    svc.Guide.mockRejectedValue(new Error("That step has not been reached yet."));
    render(RunPage, { snapshot: withActive() });
    expect(await screen.findByText(/has not been reached/)).toBeInTheDocument();
  });

  it("applies the breakage and verifies, naming the step each time", async () => {
    render(RunPage, { snapshot: withActive({ CanVerify: true }) });
    await userEvent.click(screen.getByRole("button", { name: "Apply breakage" }));
    await userEvent.click(screen.getByRole("button", { name: "Verify" }));
    expect(svc.RunAction).toHaveBeenCalledWith("break");
    expect(svc.Verify).toHaveBeenCalledWith("break");
  });

  it.each([
    ["a locked-in state with nothing allowed", { CanApply: false }, { apply: false, verify: false, cont: false }],
    ["a breakage ready to apply", { CanApply: true }, { apply: true, verify: false, cont: false }],
    ["a breakage that is verified", { CanVerify: true, CanContinue: true, Verified: true }, { apply: false, verify: true, cont: true }],
  ])("enables only what the engine allows: %s", (_name, over, want) => {
    render(RunPage, { snapshot: withActive({ CanApply: false, ...over }) });
    const state = (n: string) => !(screen.getByRole("button", { name: n }) as HTMLButtonElement).disabled;
    expect({ apply: state("Apply breakage"), verify: state("Verify"), cont: state("Continue") }).toEqual(want);
  });

  it("has no Apply button on a verification step", () => {
    render(RunPage, { snapshot: snap({ Steps: [step("fix", { Kind: "verification", State: "active", CanVerify: true })] }) });
    expect(screen.queryByRole("button", { name: "Apply breakage" })).not.toBeInTheDocument();
  });

  it("says the breakage is being applied", () => {
    render(RunPage, { snapshot: withActive({ State: "applying", CanApply: false }) });
    expect(screen.getByText("Applying the breakage")).toBeInTheDocument();
  });

  it("continues", async () => {
    render(RunPage, { snapshot: withActive({ CanContinue: true, Verified: true }) });
    await userEvent.click(screen.getByRole("button", { name: "Continue" }));
    expect(svc.Continue).toHaveBeenCalledWith("break");
  });

  it("shows a pass in green, a failure in red and an error differently, with the output behind a heading", () => {
    const outcome = (Status: string) => ({ Status, Message: `it ${Status}`, Evidence: "vault status: sealed", At: "" });

    const pass = render(RunPage, { snapshot: withActive({ Last: outcome("pass") }) });
    expect(screen.getByText("Passed")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Output" })).toBeInTheDocument();
    pass.unmount();

    const fail = render(RunPage, { snapshot: withActive({ Last: outcome("fail") }) });
    expect(screen.getByText("Not as expected")).toBeInTheDocument();
    fail.unmount();

    render(RunPage, { snapshot: withActive({ Last: outcome("error") }) });
    expect(screen.getByText("It could not run")).toBeInTheDocument();
  });

  it("saves notes when the field is left, and only when they changed", async () => {
    render(RunPage, { snapshot: withActive({ Notes: "saved" }) });
    const notes = screen.getByLabelText("Notes");
    await userEvent.click(notes);
    await userEvent.tab();
    expect(svc.SaveNotes).not.toHaveBeenCalled();

    await userEvent.type(notes, " more");
    await userEvent.tab();
    expect(svc.SaveNotes).toHaveBeenCalledExactlyOnceWith("break", "saved more");
  });

  it("saves a rating and clears it", async () => {
    render(RunPage, { snapshot: withActive({ Rating: 2 }) });
    await userEvent.click(screen.getByLabelText("4"));
    expect(svc.SetRating).toHaveBeenCalledWith("break", 4);
    await userEvent.click(screen.getByRole("button", { name: "Clear rating" }));
    expect(svc.SetRating).toHaveBeenLastCalledWith("break", 0);
  });

  it("shows what went wrong when an action fails, and carries on", async () => {
    svc.Verify.mockRejectedValueOnce(new Error("Another action is still running."));
    render(RunPage, { snapshot: withActive({ CanVerify: true }) });
    await userEvent.click(screen.getByRole("button", { name: "Verify" }));
    expect(await screen.findByText(/Another action is still running/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Verify" })).toBeEnabled();
  });

  it("keeps later steps out of sight, and lists finished ones read-only", () => {
    render(RunPage, {
      snapshot: snap({
        ActiveStep: 1,
        Steps: [step("a", { State: "complete", Notes: "went well", Rating: 5, Last: { Status: "pass", Message: "all good", Evidence: "", At: "" } }), step("b", { State: "active" }), step("c")],
      }),
    });
    expect(screen.getByRole("article")).toHaveTextContent("Step b");
    expect(screen.queryByRole("heading", { name: "Step c" })).not.toBeInTheDocument();
    expect(screen.getByText("Completed steps")).toBeInTheDocument();
  });
});

describe("ending and rolling back", () => {
  it("asks before ending, says what will be destroyed, and ends only when confirmed", async () => {
    render(RunPage, { snapshot: withActive() });
    await userEvent.click(screen.getByRole("button", { name: "End scenario" }));

    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText(/removes its containers and network/)).toBeInTheDocument();
    expect(svc.End).not.toHaveBeenCalled();

    await userEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
    expect(svc.End).not.toHaveBeenCalled();

    await userEvent.click(screen.getByRole("button", { name: "End scenario" }));
    await userEvent.click(within(await screen.findByRole("dialog")).getByRole("button", { name: "End scenario" }));
    await waitFor(() => expect(svc.End).toHaveBeenCalledOnce());
  });

  it("rolls back to the chosen step after confirming", async () => {
    render(RunPage, {
      snapshot: snap({ ActiveStep: 1, Steps: [step("a", { State: "complete", CanRollBack: true, Title: "Cause the outage" }), step("b", { State: "active" })] }),
    });
    await userEvent.click(screen.getByRole("button", { name: "Roll back" }));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByLabelText("Roll back to the start of")).toHaveValue("a");
    expect(within(dialog).getByText(/Anything the trainee changed since is lost/)).toBeInTheDocument();

    await userEvent.click(within(dialog).getByRole("button", { name: "Roll back" }));
    await waitFor(() => expect(svc.Rollback).toHaveBeenCalledExactlyOnceWith("a"));
  });

  it("keeps the dialog open and says why when ending fails", async () => {
    svc.End.mockRejectedValueOnce(new Error("could not remove run abc"));
    render(RunPage, { snapshot: withActive() });
    await userEvent.click(screen.getByRole("button", { name: "End scenario" }));
    await userEvent.click(within(await screen.findByRole("dialog")).getByRole("button", { name: "End scenario" }));
    expect(await screen.findByText(/could not remove run abc/)).toBeInTheDocument();
  });
});
