import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/svelte";

import App from "./App.svelte";

describe("App", () => {
  it("shows the application name", () => {
    render(App);

    expect(screen.getByRole("heading", { name: "Sev1 Situations" })).toBeInTheDocument();
  });
});
