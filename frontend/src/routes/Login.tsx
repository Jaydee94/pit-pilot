// frontend/src/routes/Login.tsx
import { useEffect, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import { login, getAuthConfig } from "../api/client";
import { useAuth } from "../auth/AuthContext";
import { initGoogleSignIn } from "../auth/google";

export default function Login() {
  const [clientId, setClientId] = useState<string | null>(null);
  const [configured, setConfigured] = useState<boolean | null>(null);
  const [err, setErr] = useState("");
  const btnRef = useRef<HTMLDivElement>(null);
  const nav = useNavigate();
  const { refresh } = useAuth();

  useEffect(() => {
    getAuthConfig()
      .then((c) => {
        setClientId(c.google_client_id || null);
        setConfigured(Boolean(c.google_client_id));
      })
      .catch(() => setConfigured(false));
  }, []);

  useEffect(() => {
    if (!clientId || !btnRef.current) return;
    initGoogleSignIn({
      clientId,
      buttonParent: btnRef.current,
      onCredential: async (idToken) => {
        try {
          await login("google", idToken);
          refresh();
          nav("/groups");
        } catch (e: unknown) {
          setErr((e as { message?: string })?.message ?? "Login fehlgeschlagen");
        }
      },
    }).catch(() => setErr("Google-Login konnte nicht geladen werden"));
  }, [clientId, refresh, nav]);

  return (
    <main>
      <h1>pit-pilot</h1>
      {configured === false && <p>Google-Login ist nicht konfiguriert.</p>}
      <div ref={btnRef} />
      {err && <p role="alert">{err}</p>}
    </main>
  );
}
