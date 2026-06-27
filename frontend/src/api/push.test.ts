import { afterEach, expect, test, vi } from "vitest";
import { getVapidPublicKey, savePushSubscription } from "./client";

afterEach(() => vi.restoreAllMocks());

test("getVapidPublicKey returns the key", async () => {
  vi.stubGlobal("fetch", vi.fn(async () =>
    new Response(JSON.stringify({ public_key: "PUB" }), { status: 200 })));
  expect((await getVapidPublicKey()).public_key).toBe("PUB");
});

test("savePushSubscription posts the subscription json", async () => {
  const fetchMock = vi.fn(async (_url: string, _init?: RequestInit) =>
    new Response(JSON.stringify({ id: "1" }), { status: 201 }));
  vi.stubGlobal("fetch", fetchMock);
  await savePushSubscription({ endpoint: "e", keys: { p256dh: "k", auth: "a" } });
  const init = fetchMock.mock.calls[0]?.[1];
  if (!init) throw new Error("no init");
  expect(init.method).toBe("POST");
  expect(JSON.parse(init.body as string)).toMatchObject({ endpoint: "e", keys: { p256dh: "k", auth: "a" } });
});
