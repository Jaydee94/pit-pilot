import { render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Routes, Route } from "react-router-dom";
import { afterEach, expect, test, vi } from "vitest";
import GroupDetail from "./GroupDetail";

afterEach(() => vi.restoreAllMocks());

test("shows invite code for group", async () => {
  vi.stubGlobal("fetch", vi.fn(async (url: string) => {
    if (url.endsWith("/members"))
      return new Response(JSON.stringify([]), { status: 200 });
    if (url.endsWith("/concerts"))
      return new Response(JSON.stringify([]), { status: 200 });
    // group endpoint
    return new Response(JSON.stringify({ id: "g1", name: "Crew", invite_code: "ABC123" }), { status: 200 });
  }));
  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <MemoryRouter initialEntries={["/groups/g1"]}>
        <Routes><Route path="/groups/:id" element={<GroupDetail />} /></Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
  await waitFor(() => expect(screen.getByText("ABC123")).toBeInTheDocument());
});
