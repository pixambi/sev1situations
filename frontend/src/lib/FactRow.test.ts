import { describe, it, expect, vi, afterEach } from "vitest";
import { render, screen, cleanup, waitFor } from "@testing-library/svelte";
import userEvent from "@testing-library/user-event";

const setText = vi.hoisted(() => vi.fn());
vi.mock("@wailsio/runtime", () => ({ Clipboard: { SetText: setText } }));

import FactRow from "./FactRow.svelte";

afterEach(() => {
  cleanup();
  vi.resetAllMocks();
});

describe("FactRow", () => {
  it("shows an ordinary value as it is", () => {
    render(FactRow, { label: "Version", value: "2.2.2 Community" });
    expect(screen.getByText("2.2.2 Community")).toBeInTheDocument();
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
  });

  it("keeps a secret out of the page until it is revealed, and hides it again", async () => {
    render(FactRow, { label: "Root token", value: "hvs.TopSecret", secret: true });
    expect(screen.queryByText("hvs.TopSecret")).not.toBeInTheDocument();
    expect(document.body.textContent).not.toContain("hvs.TopSecret");

    const show = screen.getByRole("button", { name: "Show Root token" });
    expect(show).toHaveAttribute("aria-pressed", "false");
    await userEvent.click(show);
    expect(screen.getByText("hvs.TopSecret")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Hide Root token" })).toHaveAttribute("aria-pressed", "true");

    await userEvent.click(screen.getByRole("button", { name: "Hide Root token" }));
    expect(screen.queryByText("hvs.TopSecret")).not.toBeInTheDocument();
  });

  it("copies the real value of a secret even while it is masked", async () => {
    setText.mockResolvedValue(undefined);
    render(FactRow, { label: "Root token", value: "hvs.TopSecret", secret: true });
    await userEvent.click(screen.getByRole("button", { name: "Copy Root token" }));
    expect(setText).toHaveBeenCalledExactlyOnceWith("hvs.TopSecret");
    expect(await screen.findByText("Copied")).toBeInTheDocument();
  });

  it("offers a copy button for a plain value only when asked", () => {
    const { unmount } = render(FactRow, { label: "API address", value: "http://127.0.0.1:8200" });
    expect(screen.queryByRole("button", { name: "Copy API address" })).not.toBeInTheDocument();
    unmount();
    render(FactRow, { label: "API address", value: "http://127.0.0.1:8200", copyable: true });
    expect(screen.getByRole("button", { name: "Copy API address" })).toBeInTheDocument();
  });

  it("does not claim it copied when the clipboard refuses", async () => {
    setText.mockRejectedValue(new Error("denied"));
    render(FactRow, { label: "Address", value: "10.0.0.2", copyable: true });
    await userEvent.click(screen.getByRole("button", { name: "Copy Address" }));
    await waitFor(() => expect(setText).toHaveBeenCalled());
    expect(screen.queryByText("Copied")).not.toBeInTheDocument();
  });
});
