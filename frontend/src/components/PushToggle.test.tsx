// frontend/src/components/PushToggle.test.tsx
import { render, screen } from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";
import PushToggle from "./PushToggle";

afterEach(() => vi.restoreAllMocks());

test("renders an unsupported note when push is unavailable", () => {
  vi.stubGlobal("navigator", {});             // no serviceWorker
  render(<PushToggle />);
  expect(screen.getByText(/nicht unterstützt/i)).toBeInTheDocument();
});

test("renders an enable button when supported and not yet granted", () => {
  vi.stubGlobal("navigator", { serviceWorker: { ready: Promise.resolve({}) } });
  vi.stubGlobal("Notification", { permission: "default" } as unknown as typeof Notification);
  vi.stubGlobal("PushManager", function() {});
  render(<PushToggle />);
  expect(screen.getByRole("button", { name: /benachrichtigungen aktivieren/i })).toBeInTheDocument();
});
