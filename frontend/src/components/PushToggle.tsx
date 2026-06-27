// frontend/src/components/PushToggle.tsx
import { useState } from "react";
import { getVapidPublicKey, savePushSubscription, deletePushSubscription } from "../api/client";

function urlBase64ToUint8Array(base64: string): Uint8Array<ArrayBuffer> {
  const padding = "=".repeat((4 - (base64.length % 4)) % 4);
  const b64 = (base64 + padding).replace(/-/g, "+").replace(/_/g, "/");
  const raw = atob(b64);
  const buffer = new ArrayBuffer(raw.length);
  const out = new Uint8Array(buffer);
  for (let i = 0; i < raw.length; i++) out[i] = raw.charCodeAt(i);
  return out;
}

const supported = () =>
  typeof navigator !== "undefined" && "serviceWorker" in navigator &&
  typeof Notification !== "undefined" && "PushManager" in (globalThis as unknown as Record<string, unknown>);

export default function PushToggle() {
  const [status, setStatus] = useState("");
  if (!supported()) {
    return <p>Push-Benachrichtigungen werden von diesem Browser nicht unterstützt.</p>;
  }

  const enable = async () => {
    try {
      const perm = await Notification.requestPermission();
      if (perm !== "granted") { setStatus("Erlaubnis verweigert"); return; }
      const reg = await navigator.serviceWorker.ready;
      const { public_key } = await getVapidPublicKey();
      const sub = await reg.pushManager.subscribe({
        userVisibleOnly: true,
        applicationServerKey: urlBase64ToUint8Array(public_key),
      });
      const j = sub.toJSON() as { endpoint?: string; keys?: { p256dh?: string; auth?: string } };
      await savePushSubscription({
        endpoint: j.endpoint ?? "",
        keys: { p256dh: j.keys?.p256dh ?? "", auth: j.keys?.auth ?? "" },
      });
      setStatus("Benachrichtigungen aktiv");
    } catch {
      setStatus("Aktivierung fehlgeschlagen");
    }
  };

  const disable = async () => {
    const reg = await navigator.serviceWorker.ready;
    const sub = await reg.pushManager.getSubscription();
    if (sub) { await deletePushSubscription(sub.endpoint); await sub.unsubscribe(); }
    setStatus("Benachrichtigungen aus");
  };

  return (
    <div>
      <button onClick={enable}>Benachrichtigungen aktivieren</button>
      <button onClick={disable}>aus</button>
      {status && <span role="status">{status}</span>}
    </div>
  );
}
