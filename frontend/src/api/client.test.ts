import { afterEach, expect, test, vi } from "vitest";
import { apiFetch } from "./client";

afterEach(() => vi.restoreAllMocks());

test("apiFetch returns parsed json on success", async () => {
  vi.stubGlobal("fetch", vi.fn(async () =>
    new Response(JSON.stringify([{ id: "1", name: "Crew" }]), { status: 200 }),
  ));
  const data = await apiFetch<{ id: string; name: string }[]>("/api/groups");
  expect(data[0].name).toBe("Crew");
});

test("apiFetch throws ApiError on failure", async () => {
  vi.stubGlobal("fetch", vi.fn(async () =>
    new Response(JSON.stringify({ error: { code: "not_member", message: "no" } }), { status: 403 }),
  ));
  await expect(apiFetch("/api/groups/x/members")).rejects.toMatchObject({ code: "not_member" });
});
