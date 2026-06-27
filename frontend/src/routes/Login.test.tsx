// frontend/src/routes/Login.test.tsx
import { render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { afterEach, expect, test, vi } from "vitest";
import Login from "./Login";
import { AuthProvider } from "../auth/AuthContext";

afterEach(() => {
  vi.restoreAllMocks();
  delete window.google;
});

function wrap() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return (
    <QueryClientProvider client={qc}>
      <AuthProvider>
        <MemoryRouter><Login /></MemoryRouter>
      </AuthProvider>
    </QueryClientProvider>
  );
}

test("initializes the Google button when a client id is configured", async () => {
  const initialize = vi.fn();
  (window as unknown as { google: unknown }).google = {
    accounts: { id: { initialize, renderButton: vi.fn(), prompt: vi.fn() } },
  };
  // AuthProvider calls me() on mount, then Login calls /auth/config
  vi.stubGlobal("fetch", vi.fn(async (url: string) => {
    if (String(url).endsWith("/auth/config")) {
      return new Response(JSON.stringify({ google_client_id: "gid" }), { status: 200 });
    }
    return new Response(JSON.stringify({ error: { code: "x", message: "x" } }), { status: 401 });
  }));
  render(wrap());
  await waitFor(() => expect(initialize).toHaveBeenCalled());
});

test("POSTs to /auth/google when the GIS credential callback fires", async () => {
  let capturedCallback: ((r: { credential: string }) => void) | undefined;
  const initialize = vi.fn((cfg: { callback: (r: { credential: string }) => void }) => {
    capturedCallback = cfg.callback;
  });
  (window as unknown as { google: unknown }).google = {
    accounts: { id: { initialize, renderButton: vi.fn(), prompt: vi.fn() } },
  };

  const fetchedUrls: string[] = [];
  vi.stubGlobal("fetch", vi.fn(async (url: string) => {
    fetchedUrls.push(String(url));
    if (String(url).endsWith("/auth/config")) {
      return new Response(JSON.stringify({ google_client_id: "gid" }), { status: 200 });
    }
    if (String(url).endsWith("/auth/google")) {
      return new Response(JSON.stringify({ id: "u1", display_name: "U" }), { status: 200 });
    }
    return new Response(JSON.stringify({ error: { code: "x", message: "x" } }), { status: 401 });
  }));

  render(wrap());
  await waitFor(() => expect(capturedCallback).toBeDefined());

  capturedCallback!({ credential: "id-token-xyz" });

  await waitFor(() => expect(fetchedUrls.some((u) => u.endsWith("/auth/google"))).toBe(true));
});

test("shows a not-configured note when the client id is empty", async () => {
  vi.stubGlobal("fetch", vi.fn(async (url: string) => {
    if (String(url).endsWith("/auth/config")) {
      return new Response(JSON.stringify({ google_client_id: "" }), { status: 200 });
    }
    return new Response(JSON.stringify({ error: { code: "x", message: "x" } }), { status: 401 });
  }));
  render(wrap());
  await waitFor(() => expect(screen.getByText(/nicht konfiguriert/i)).toBeInTheDocument());
});
