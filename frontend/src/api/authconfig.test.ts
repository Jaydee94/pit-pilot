import { afterEach, expect, test, vi } from "vitest";
import { getAuthConfig } from "./client";

afterEach(() => vi.restoreAllMocks());

test("getAuthConfig returns the google client id", async () => {
  vi.stubGlobal("fetch", vi.fn(async () =>
    new Response(JSON.stringify({ google_client_id: "gid.apps.googleusercontent.com" }), { status: 200 })));
  expect((await getAuthConfig()).google_client_id).toBe("gid.apps.googleusercontent.com");
});
