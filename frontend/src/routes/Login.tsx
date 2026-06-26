import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { login } from "../api/client";
import { useAuth } from "../auth/AuthContext";

export default function Login() {
  const [token, setToken] = useState("");
  const [err, setErr] = useState("");
  const nav = useNavigate();
  const { refresh } = useAuth();
  const submit = async (provider: "google" | "apple") => {
    try {
      await login(provider, token);
      refresh();
      nav("/groups");
    } catch (e: unknown) {
      const msg = e instanceof Error ? e.message : (e as { message?: string })?.message;
      setErr(msg ?? "Login fehlgeschlagen");
    }
  };
  return (
    <main>
      <h1>pit-pilot</h1>
      <button onClick={() => submit("google")}>Mit Google anmelden</button>
      <button onClick={() => submit("apple")}>Mit Apple anmelden</button>
      <details>
        <summary>Dev: ID-Token einfügen</summary>
        <textarea value={token} onChange={(e) => setToken(e.target.value)} />
      </details>
      {err && <p role="alert">{err}</p>}
    </main>
  );
}
