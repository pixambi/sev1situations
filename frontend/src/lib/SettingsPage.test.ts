import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, cleanup, waitFor } from "@testing-library/svelte";
import userEvent from "@testing-library/user-event";

const svc = vi.hoisted(() => ({ Get: vi.fn(), SaveField: vi.fn(), SaveInstructor: vi.fn() }));
vi.mock("../../bindings/github.com/pixambi/sev1situations/internal/gui/settingsservice.js", () => svc);

import SettingsPage from "./SettingsPage.svelte";

const field = (over: Record<string, unknown> = {}) => ({ Key: "vault.license", Label: "Vault Enterprise license", Help: "Needed for Enterprise.", Secret: true, Set: false, Display: "", ...over });
const view = (fields: unknown[] = [field()], Instructor = "Ada") => ({ Instructor, Fields: fields });
const theme = (Display = "") => ({ Key: "app.theme", Label: "Theme", Help: "Light or dark.", Secret: false, Set: Display !== "", Display });

beforeEach(() => {
  svc.Get.mockResolvedValue(view([field(), theme()]));
  svc.SaveField.mockResolvedValue(view());
  svc.SaveInstructor.mockResolvedValue(view());
});
afterEach(() => {
  cleanup();
  vi.resetAllMocks();
});

async function setup() {
  const onchanged = vi.fn();
  render(SettingsPage, { onchanged });
  await screen.findByRole("heading", { name: "Settings" });
  await screen.findByLabelText("Instructor's name");
  return { onchanged };
}

describe("SettingsPage", () => {
  it("shows the saved instructor name and a field for each setting from the products", async () => {
    await setup();
    expect(screen.getByLabelText("Instructor's name")).toHaveValue("Ada");
    expect(screen.getByText("Vault Enterprise license")).toBeInTheDocument();
    expect(screen.getByText("Not set")).toBeInTheDocument();
    expect(screen.getByRole("combobox", { name: "Theme" })).toBeInTheDocument();
  });

  it("saves the instructor's name and says so", async () => {
    const { onchanged } = await setup();
    const name = screen.getByLabelText("Instructor's name");
    await userEvent.clear(name);
    await userEvent.type(name, "Grace Hopper");
    await userEvent.click(screen.getByRole("button", { name: "Save name" }));

    await waitFor(() => expect(svc.SaveInstructor).toHaveBeenCalledExactlyOnceWith("Grace Hopper"));
    expect(await screen.findByText("The instructor's name saved.")).toBeInTheDocument();
    expect(onchanged).toHaveBeenCalledOnce();
  });

  it("saves a license without ever showing a saved one, and forgets what was typed", async () => {
    const { onchanged } = await setup();
    const save = screen.getByRole("button", { name: "Save" });
    expect(save).toBeDisabled(); // nothing typed yet

    const input = screen.getByLabelText("Vault Enterprise license");
    await userEvent.type(input, "02MV4UU43BK5.SECRET");
    expect(save).toBeEnabled();

    svc.SaveField.mockResolvedValueOnce(view([field({ Set: true, Display: "********" }), theme()]));
    await userEvent.click(save);
    await waitFor(() => expect(svc.SaveField).toHaveBeenCalledWith("vault.license", "02MV4UU43BK5.SECRET"));
    expect(await screen.findByText("Saved", { selector: ".bx--tag *, .bx--tag" })).toBeInTheDocument();
    expect(onchanged).toHaveBeenCalledOnce();
    expect(screen.getByLabelText("Vault Enterprise license")).toHaveValue(""); // not kept in the page
    expect(document.body.textContent).not.toContain("02MV4UU43BK5.SECRET");
  });

  it("tells the instructor a license is saved without showing it, and lets them remove it", async () => {
    svc.Get.mockResolvedValue(view([field({ Set: true, Display: "********" }), theme()]));
    await setup();
    expect(screen.getByPlaceholderText("Saved. Enter a new one to replace it.")).toHaveValue("");

    svc.SaveField.mockResolvedValueOnce(view());
    await userEvent.click(screen.getByRole("button", { name: "Remove" }));
    await waitFor(() => expect(svc.SaveField).toHaveBeenCalledWith("vault.license", ""));
  });

  it("saves a theme choice as soon as it is made", async () => {
    const { onchanged } = await setup();
    await userEvent.selectOptions(screen.getByRole("combobox", { name: "Theme" }), "dark");
    await waitFor(() => expect(svc.SaveField).toHaveBeenCalledWith("app.theme", "dark"));
    expect(onchanged).toHaveBeenCalled();
  });

  it("shows the saved theme, and the choice to follow the system", async () => {
    svc.Get.mockResolvedValue(view([theme("light")]));
    await setup();
    expect(screen.getByRole("combobox", { name: "Theme" })).toHaveValue("light");
    expect(screen.getByRole("option", { name: "Follow the system" })).toBeInTheDocument();
  });

  it("says why something could not be saved", async () => {
    svc.SaveInstructor.mockRejectedValueOnce(new Error("That value cannot be saved: longer than 100 characters."));
    await setup();
    await userEvent.click(screen.getByRole("button", { name: "Save name" }));
    expect(await screen.findByText(/longer than 100 characters/)).toBeInTheDocument();
    expect(screen.getByText("Not saved")).toBeInTheDocument();
  });

  it("explains when the settings cannot be read", async () => {
    svc.Get.mockRejectedValueOnce(new Error("the settings file is damaged"));
    render(SettingsPage, { onchanged: vi.fn() });
    expect(await screen.findByText(/settings file is damaged/)).toBeInTheDocument();
  });
});
