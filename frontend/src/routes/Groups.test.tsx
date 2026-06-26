import { render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { afterEach, expect, test, vi } from "vitest";
import type { ReactNode } from "react";
import Groups from "./Groups";

afterEach(() => vi.restoreAllMocks());

function wrap(ui: ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={qc}><MemoryRouter>{ui}</MemoryRouter></QueryClientProvider>;
}

test("lists groups from the api", async () => {
  vi.stubGlobal("fetch", vi.fn(async () =>
    new Response(JSON.stringify([{ id: "1", name: "Metal Buddies", invite_code: "X" }]), { status: 200 }),
  ));
  render(wrap(<Groups />));
  await waitFor(() => expect(screen.getByText("Metal Buddies")).toBeInTheDocument());
});
