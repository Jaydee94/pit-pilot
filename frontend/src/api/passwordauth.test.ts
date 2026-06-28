import { afterEach, expect, test, vi } from "vitest";
import { register, passwordLogin, devLogin } from "./client";

afterEach(() => vi.restoreAllMocks());

function captureFetch() {
  const calls: Array<{ url: string; init?: RequestInit }> = [];
  vi.stubGlobal("fetch", vi.fn(async (url: string, init?: RequestInit) => {
    calls.push({ url: String(url), init });
    return new Response(JSON.stringify({ id: "u1", display_name: "U", avatar_url: null }), { status: 200 });
  }));
  return calls;
}

test("register posts email/password/display_name to /auth/register", async () => {
  const calls = captureFetch();
  await register("a@b.de", "secret12", "Alice");
  expect(calls[0].url).toContain("/auth/register");
  expect(calls[0].init?.method).toBe("POST");
  expect(JSON.parse(String(calls[0].init?.body))).toEqual({ email: "a@b.de", password: "secret12", display_name: "Alice" });
});

test("passwordLogin posts to /auth/login", async () => {
  const calls = captureFetch();
  await passwordLogin("a@b.de", "secret12");
  expect(calls[0].url).toContain("/auth/login");
  expect(JSON.parse(String(calls[0].init?.body))).toEqual({ email: "a@b.de", password: "secret12" });
});

test("devLogin posts the name to /auth/dev-login", async () => {
  const calls = captureFetch();
  await devLogin("Tester");
  expect(calls[0].url).toContain("/auth/dev-login");
  expect(JSON.parse(String(calls[0].init?.body))).toEqual({ name: "Tester" });
});
