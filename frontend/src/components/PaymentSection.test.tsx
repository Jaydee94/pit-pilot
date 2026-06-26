import type { ReactNode } from "react";
import { render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, expect, test, vi } from "vitest";
import PaymentSection from "./PaymentSection";

afterEach(() => vi.restoreAllMocks());

function wrap(ui: ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={qc}>{ui}</QueryClientProvider>;
}

test("member with an item sees amount and a PayPal pay link", async () => {
  vi.stubGlobal("fetch", vi.fn(async () =>
    new Response(JSON.stringify({
      responsible_user_id: "u1", default_amount_cents: 4500, payment_link: "https://paypal.me/o/45",
      is_responsible: false,
      items: [{ id: "i1", user_id: "me", display_name: "Me", amount_cents: 4500, status: "open" }],
      summary: null,
    }), { status: 200 }),
  ));
  render(wrap(<PaymentSection concertId="c1" />));
  await waitFor(() => expect(screen.getByText(/45,00/)).toBeInTheDocument());
  const link = screen.getByRole("link", { name: /paypal/i });
  expect(link).toHaveAttribute("href", "https://paypal.me/o/45");
});

test("inactive state shows the activate button", async () => {
  vi.stubGlobal("fetch", vi.fn(async () =>
    new Response(JSON.stringify({ error: { code: "payment_not_active", message: "no" } }), { status: 404 }),
  ));
  render(wrap(<PaymentSection concertId="c1" />));
  await waitFor(() => expect(screen.getByText(/Tickets/i)).toBeInTheDocument());
});
