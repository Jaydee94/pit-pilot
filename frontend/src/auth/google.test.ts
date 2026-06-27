// frontend/src/auth/google.test.ts
import { afterEach, expect, test, vi } from "vitest";
import { initGoogleSignIn } from "./google";

afterEach(() => {
  vi.restoreAllMocks();
  // @ts-expect-error cleanup injected global — delete on readonly property
  delete (window as unknown as { readonly google: unknown }).google;
});

test("initializes GIS and forwards the credential", async () => {
  const initialize = vi.fn();
  const renderButton = vi.fn();
  const prompt = vi.fn();
  (window as unknown as { google: unknown }).google = { accounts: { id: { initialize, renderButton, prompt } } };

  const received: string[] = [];
  const parent = document.createElement("div");
  await initGoogleSignIn({ clientId: "gid", buttonParent: parent, onCredential: (t) => received.push(t) });

  expect(initialize).toHaveBeenCalledTimes(1);
  const cfg = initialize.mock.calls[0]?.[0] as { client_id: string; callback: (r: { credential: string }) => void };
  expect(cfg.client_id).toBe("gid");
  expect(renderButton).toHaveBeenCalledWith(parent, expect.any(Object));
  expect(prompt).toHaveBeenCalled();

  cfg.callback({ credential: "id-token-123" });
  expect(received).toEqual(["id-token-123"]);
});
