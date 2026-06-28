// frontend/src/routes/Login.tsx
import { useEffect, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import { login, register, passwordLogin, devLogin, getAuthConfig } from "../api/client";
import { useAuth } from "../auth/AuthContext";
import { initGoogleSignIn } from "../auth/google";

export default function Login() {
  const [clientId, setClientId] = useState<string | null>(null);
  const [allowDev, setAllowDev] = useState(false);
  const [configured, setConfigured] = useState<boolean | null>(null);
  const [mode, setMode] = useState<"login" | "register">("login");
  const [email, setEmail] = useState("");
  const [pw, setPw] = useState("");
  const [name, setName] = useState("");
  const [devName, setDevName] = useState("");
  const [err, setErr] = useState("");
  const btnRef = useRef<HTMLDivElement>(null);
  const nav = useNavigate();
  const { refresh } = useAuth();

  useEffect(() => {
    getAuthConfig()
      .then((c) => {
        setClientId(c.google_client_id || null);
        setAllowDev(Boolean(c.allow_dev_login));
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
          setErr(e instanceof Error ? e.message : "Login fehlgeschlagen");
        }
      },
    }).catch(() => setErr("Google-Login konnte nicht geladen werden"));
  }, [clientId, refresh, nav]);

  const onSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setErr("");
    try {
      if (mode === "register") {
        if (pw.length < 8) {
          setErr("Passwort muss mindestens 8 Zeichen haben.");
          return;
        }
        await register(email, pw, name);
      } else {
        await passwordLogin(email, pw);
      }
      refresh();
      nav("/groups");
    } catch (e2: unknown) {
      setErr(e2 instanceof Error ? e2.message : "Anmeldung fehlgeschlagen");
    }
  };

  const onDev = async (e: React.FormEvent) => {
    e.preventDefault();
    setErr("");
    try {
      await devLogin(devName);
      refresh();
      nav("/groups");
    } catch (e2: unknown) {
      setErr(e2 instanceof Error ? e2.message : "Dev-Login fehlgeschlagen");
    }
  };

  return (
    <main>
      <h1>pit-pilot</h1>

      <div>
        <button type="button" onClick={() => setMode("login")} aria-pressed={mode === "login"}>
          Anmelden
        </button>
        <button type="button" onClick={() => setMode("register")} aria-pressed={mode === "register"}>
          Registrieren
        </button>
      </div>

      <form onSubmit={onSubmit}>
        {mode === "register" && (
          <label>
            Anzeigename
            <input value={name} onChange={(e) => setName(e.target.value)} />
          </label>
        )}
        <label>
          E-Mail
          <input type="email" value={email} onChange={(e) => setEmail(e.target.value)} />
        </label>
        <label>
          Passwort
          <input type="password" value={pw} onChange={(e) => setPw(e.target.value)} />
        </label>
        <button type="submit">{mode === "register" ? "Konto erstellen" : "Einloggen"}</button>
      </form>

      {configured === false && <p>Google-Login ist nicht konfiguriert.</p>}

      {configured && (
        <>
          <p>— oder —</p>
          <div ref={btnRef} />
        </>
      )}

      {allowDev && (
        <form onSubmit={onDev}>
          <label>
            Name
            <input value={devName} onChange={(e) => setDevName(e.target.value)} />
          </label>
          <button type="submit">Dev-Login</button>
        </form>
      )}

      {err && <p role="alert">{err}</p>}
    </main>
  );
}
