import "@testing-library/jest-dom/vitest";
import { afterEach } from "vitest";
import { cleanup } from "@testing-library/svelte";

afterEach(() => cleanup());

// jsdom doesn't implement innerText; App.svelte writes the toast message with it.
if (!("innerText" in HTMLElement.prototype)) {
  Object.defineProperty(HTMLElement.prototype, "innerText", {
    get() {
      return this.textContent;
    },
    set(value: string) {
      this.textContent = value;
    },
  });
}
