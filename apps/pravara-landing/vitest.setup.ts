import "@testing-library/jest-dom/vitest";

// IntersectionObserver isn't implemented in jsdom; the reveal + active-section
// hooks call observe() during mount. A no-op shim keeps tests green without
// pulling in an extra polyfill package.
class IntersectionObserverMock {
  observe() {}
  unobserve() {}
  disconnect() {}
  takeRecords() {
    return [];
  }
  root = null;
  rootMargin = "";
  thresholds = [];
}

globalThis.IntersectionObserver =
  IntersectionObserverMock as unknown as typeof IntersectionObserver;
