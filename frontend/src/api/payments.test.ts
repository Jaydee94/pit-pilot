import { afterEach, expect, test, vi } from "vitest";
import { getPayments, activatePayments } from "./client";

afterEach(() => vi.restoreAllMocks());

test("getPayments returns the view incl. payment_link", async () => {
  vi.stubGlobal("fetch", vi.fn(async () =>
    new Response(JSON.stringify({
      responsible_user_id: "u1", default_amount_cents: 4500, payment_link: "https://paypal.me/o/45",
      is_responsible: false, items: [], summary: null,
    }), { status: 200 }),
  ));
  const v = await getPayments("c1");
  expect(v.payment_link).toBe("https://paypal.me/o/45");
  expect(v.is_responsible).toBe(false);
});

test("activatePayments posts amount + link", async () => {
  const fetchMock = vi.fn(async (_url: string, _init?: RequestInit) =>
    new Response(JSON.stringify({ id: "col1" }), { status: 201 }));
  vi.stubGlobal("fetch", fetchMock);
  await activatePayments("c1", 4500, "https://paypal.me/o/45");
  const init = fetchMock.mock.calls[0]?.[1];
  if (!init) throw new Error("fetch was not called with init");
  expect(init.method).toBe("POST");
  expect(JSON.parse(init.body as string)).toMatchObject({ default_amount_cents: 4500, payment_link: "https://paypal.me/o/45" });
});
