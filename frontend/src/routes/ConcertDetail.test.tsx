import { render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Routes, Route } from "react-router-dom";
import { afterEach, expect, test, vi } from "vitest";
import ConcertDetail from "./ConcertDetail";

afterEach(() => vi.restoreAllMocks());

test("shows concert artist and rsvp list", async () => {
  vi.stubGlobal("fetch", vi.fn(async (url: string) => {
    if (url.endsWith("/rsvps"))
      return new Response(JSON.stringify([{ id: "u1", display_name: "Ann", status: "yes" }]), { status: 200 });
    return new Response(JSON.stringify({ id: "c1", artist: "Tool", event_at: "2026-09-01T20:00:00Z", rsvp_deadline: "2026-08-20T20:00:00Z" }), { status: 200 });
  }));
  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <MemoryRouter initialEntries={["/concerts/c1"]}>
        <Routes><Route path="/concerts/:id" element={<ConcertDetail />} /></Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
  await waitFor(() => expect(screen.getByText("Tool")).toBeInTheDocument());
  expect(screen.getByText("Ann")).toBeInTheDocument();
});
