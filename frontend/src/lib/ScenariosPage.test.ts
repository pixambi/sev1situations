import { describe, it, expect, vi, afterEach } from "vitest";
import { render, screen, cleanup, within } from "@testing-library/svelte";
import userEvent from "@testing-library/user-event";
import type { ScenarioList, ScenarioSummary } from "../../bindings/github.com/pixambi/sev1situations/internal/gui/models.js";
import ScenariosPage from "./ScenariosPage.svelte";

afterEach(cleanup);

const scenario = (id: string, over: Partial<ScenarioSummary> = {}): ScenarioSummary => ({
  ID: id, Name: `Scenario ${id}`, Summary: `About ${id}.`, Product: "vault", Editions: ["community"], Difficulty: "beginner",
  EstimatedMinutes: 15, Tags: [], Source: "built-in", NeedsLicense: false, ...over,
});

function setup(catalogue: Partial<ScenarioList>, props: Partial<{ runActive: boolean; podmanReady: boolean }> = {}) {
  const handlers = { onstart: vi.fn(), onopenrun: vi.fn(), onopensettings: vi.fn() };
  render(ScenariosPage, {
    catalogue: { Scenarios: [], Problems: [], ...catalogue },
    runActive: false,
    podmanReady: true,
    ...props,
    ...handlers,
  });
  return handlers;
}

const startButton = (id: string) => within(screen.getByRole("heading", { name: `Scenario ${id}` }).closest("div")!).getByRole("button", { name: "Start" });

describe("ScenariosPage", () => {
  it("groups the scenarios under their product and shows what each is", () => {
    setup({ Scenarios: [scenario("b1", { Product: "boundary", Difficulty: "advanced" }), scenario("v1", { Editions: ["community", "enterprise"] })] });

    expect(screen.getByRole("heading", { name: "Boundary" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Vault" })).toBeInTheDocument();
    expect(screen.getByText("About v1.")).toBeInTheDocument();
    expect(screen.getAllByText("15 min")).toHaveLength(2);
    expect(screen.getByText("Enterprise")).toBeInTheDocument();
  });

  it("starts the scenario whose button was pressed", async () => {
    const h = setup({ Scenarios: [scenario("a"), scenario("b")] });
    await userEvent.click(screen.getAllByRole("button", { name: "Start" })[1]);
    expect(h.onstart).toHaveBeenCalledExactlyOnceWith("b");
  });

  it("blocks every Start while a run is active, and links to the run", async () => {
    const h = setup({ Scenarios: [scenario("a"), scenario("b")] }, { runActive: true });

    expect(screen.getByText("A scenario is running")).toBeInTheDocument();
    for (const b of screen.getAllByRole("button", { name: "Start" })) expect(b).toBeDisabled();
    await userEvent.click(screen.getByRole("link", { name: "Go to the current run" }));
    expect(h.onopenrun).toHaveBeenCalledOnce();
  });

  it("blocks Start while Podman is not ready", () => {
    setup({ Scenarios: [scenario("a")] }, { podmanReady: false });
    expect(screen.getByRole("button", { name: "Start" })).toBeDisabled();
  });

  it("says a license is needed, links to Settings, and blocks only that scenario", async () => {
    const h = setup({ Scenarios: [scenario("free"), scenario("ent", { Editions: ["enterprise"], NeedsLicense: true })] });

    expect(screen.getByText("License needed")).toBeInTheDocument();
    expect(startButton("ent")).toBeDisabled();
    expect(startButton("free")).toBeEnabled();
    await userEvent.click(screen.getByRole("link", { name: "Add it in Settings" }));
    expect(h.onopensettings).toHaveBeenCalledOnce();
  });

  it("reports a scenario that could not be loaded without hiding the others", () => {
    setup({ Scenarios: [scenario("a")], Problems: ["scenarios/broken/scenario.yaml:3: name must be text"] });
    expect(screen.getByText("scenarios/broken/scenario.yaml:3: name must be text")).toBeInTheDocument();
    expect(screen.getByText("Scenario a")).toBeInTheDocument();
  });

  it("filters by difficulty and says when nothing matches", async () => {
    setup({ Scenarios: [scenario("easy"), scenario("hard", { Difficulty: "advanced" })] });

    await userEvent.click(screen.getByRole("combobox", { name: "Difficulty" }));
    await userEvent.click(screen.getByRole("option", { name: "Advanced" }));
    expect(screen.queryByText("Scenario easy")).not.toBeInTheDocument();
    expect(screen.getByText("Scenario hard")).toBeInTheDocument();

    await userEvent.click(screen.getByRole("combobox", { name: "Edition" }));
    await userEvent.click(screen.getByRole("option", { name: "Community" }));
    expect(screen.getByText("Scenario hard")).toBeInTheDocument();
  });

  it("says so when there are no scenarios at all", () => {
    setup({});
    expect(screen.getByText("No scenarios were found.")).toBeInTheDocument();
  });
});
