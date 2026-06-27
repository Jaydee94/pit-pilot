// frontend/src/auth/google.ts
type GoogleCredentialResponse = { credential: string };

interface GoogleIdApi {
  initialize(cfg: { client_id: string; callback: (resp: GoogleCredentialResponse) => void }): void;
  renderButton(parent: HTMLElement, options: Record<string, unknown>): void;
  prompt(): void;
}

declare global {
  interface Window {
    google?: { accounts: { id: GoogleIdApi } };
  }
}

const GIS_SRC = "https://accounts.google.com/gsi/client";

function loadGisScript(): Promise<void> {
  if (window.google?.accounts?.id) return Promise.resolve();
  return new Promise((resolve, reject) => {
    const existing = document.querySelector<HTMLScriptElement>(`script[src="${GIS_SRC}"]`);
    if (existing) {
      existing.addEventListener("load", () => resolve());
      existing.addEventListener("error", () => reject(new Error("failed to load Google Identity Services")));
      return;
    }
    const s = document.createElement("script");
    s.src = GIS_SRC;
    s.async = true;
    s.defer = true;
    s.onload = () => resolve();
    s.onerror = () => reject(new Error("failed to load Google Identity Services"));
    document.head.appendChild(s);
  });
}

export async function initGoogleSignIn(opts: {
  clientId: string;
  buttonParent: HTMLElement;
  onCredential: (idToken: string) => void;
}): Promise<void> {
  await loadGisScript();
  const id = window.google?.accounts?.id;
  if (!id) throw new Error("Google Identity Services unavailable");
  id.initialize({
    client_id: opts.clientId,
    callback: (resp) => opts.onCredential(resp.credential),
  });
  id.renderButton(opts.buttonParent, { type: "standard", theme: "outline", size: "large", text: "signin_with" });
  id.prompt();
}
