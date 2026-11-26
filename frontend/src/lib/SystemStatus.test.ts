import { describe, it, expect, vi, afterEach } from "vitest";
import { render, screen, cleanup } from "@testing-library/svelte";
import userEvent from "@testing-library/user-event";

import SystemStatus from "./SystemStatus.svelte";

afterEach(cleanup);

describe("SystemStatus", () => {
  it("says it is checking before the first answer", () => {
    render(SystemStatus, { status: null, checking: true, onretry: vi.fn() });
    expect(screen.getByText("Checking that Podman is ready")).toBeInTheDocument();
  });

  it("explains what is wrong and offers a retry", async () => {
    const onretry = vi.fn();
    render(SystemStatus, {
      status: { Ready: false, Message: "The Podman machine is stopped.", Notices: [] },
      checking: false,
      onretry,
    });

    expect(screen.getByText("Podman is not ready")).toBeInTheDocument();
    expect(screen.getByText("The Podman machine is stopped.")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(onretry).toHaveBeenCalledOnce();
  });

  it("shows the cleanup of an earlier launch, and nothing else when all is well", () => {
    render(SystemStatus, {
      status: { Ready: true, Message: "", Notices: ["Removed what run abc left behind when the application last closed."] },
      checking: false,
      onretry: vi.fn(),
    });
    expect(screen.getByText(/Removed what run abc/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Retry" })).not.toBeInTheDocument();
  });

  it("shows nothing when ready with nothing to report", () => {
    const { container } = render(SystemStatus, { status: { Ready: true, Message: "", Notices: [] }, checking: false, onretry: vi.fn() });
    expect(container.textContent?.trim()).toBe("");
  });
});
