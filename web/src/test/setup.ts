import "@testing-library/jest-dom/vitest";
import { afterEach } from "vitest";

if (!globalThis.ResizeObserver) {
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  } as typeof ResizeObserver;
}

afterEach(() => {
  document.body.innerHTML = "";
});
