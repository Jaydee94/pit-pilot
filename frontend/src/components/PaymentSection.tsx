import { useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import {
  getPayments, activatePayments, setPaymentLink, deactivatePayments,
  setPaymentAmount, removePaymentItem,
  reportPayment, unreportPayment, confirmPayment, unconfirmPayment,
  type PaymentView, type ApiError,
} from "../api/client";

const eur = (cents: number) =>
  (cents / 100).toLocaleString("de-DE", { minimumFractionDigits: 2, maximumFractionDigits: 2 }) + " €";

export default function PaymentSection({ concertId }: { concertId: string }) {
  const qc = useQueryClient();
  const key = ["payments", concertId];
  const invalidate = () => qc.invalidateQueries({ queryKey: key });
  const { data, error, isLoading } = useQuery<PaymentView, ApiError>({
    queryKey: key, queryFn: () => getPayments(concertId), retry: false,
  });
  const [amount, setAmount] = useState("45");
  const [link, setLink] = useState("");

  const activate = useMutation({
    mutationFn: () => activatePayments(concertId, Math.round(parseFloat(amount || "0") * 100), link || undefined),
    onSuccess: invalidate,
  });

  if (isLoading) return <section><h2>Bezahlung</h2><p>Lädt…</p></section>;

  if (error && error.code === "payment_not_active") {
    return (
      <section>
        <h2>Bezahlung</h2>
        <form onSubmit={(e) => { e.preventDefault(); activate.mutate(); }}>
          <input aria-label="Standardbetrag (€)" value={amount} onChange={(e) => setAmount(e.target.value)} />
          <input aria-label="PayPal-Link (optional)" value={link} onChange={(e) => setLink(e.target.value)} placeholder="https://paypal.me/…" />
          <button type="submit">Ich kümmere mich um die Tickets</button>
        </form>
      </section>
    );
  }
  if (!data) return null;

  return (
    <section>
      <h2>Bezahlung</h2>
      {data.is_responsible
        ? <ResponsibleView concertId={concertId} data={data} onChange={invalidate} />
        : <MemberView concertId={concertId} data={data} onChange={invalidate} />}
    </section>
  );
}

function MemberView({ concertId, data, onChange }: { concertId: string; data: PaymentView; onChange: () => void }) {
  const item = data.items[0];
  const report = useMutation({ mutationFn: () => reportPayment(concertId, item!.id), onSuccess: onChange });
  const unreport = useMutation({ mutationFn: () => unreportPayment(concertId, item!.id), onSuccess: onChange });
  if (!item) return <p>Kein Posten für dich.</p>;
  return (
    <div>
      <p>Dein Anteil: <strong>{eur(item.amount_cents)}</strong> — Status: {item.status}</p>
      {item.status === "open" && <button onClick={() => report.mutate()}>bezahlt</button>}
      {item.status === "reported" && <button onClick={() => unreport.mutate()}>zurücknehmen</button>}
      {item.status === "confirmed" && <span>✅ bestätigt</span>}
      {data.payment_link && (
        <a href={data.payment_link} target="_blank" rel="noreferrer">Per PayPal zahlen</a>
      )}
    </div>
  );
}

function ResponsibleView({ concertId, data, onChange }: { concertId: string; data: PaymentView; onChange: () => void }) {
  const confirm = useMutation({ mutationFn: (id: string) => confirmPayment(concertId, id), onSuccess: onChange });
  const unconfirm = useMutation({ mutationFn: (id: string) => unconfirmPayment(concertId, id), onSuccess: onChange });
  const remove = useMutation({ mutationFn: (id: string) => removePaymentItem(concertId, id), onSuccess: onChange });
  const setAmt = useMutation({ mutationFn: (v: { id: string; cents: number }) => setPaymentAmount(concertId, v.id, v.cents), onSuccess: onChange });
  const deactivate = useMutation({ mutationFn: () => deactivatePayments(concertId), onSuccess: onChange });
  const [linkEdit, setLinkEdit] = useState(data.payment_link ?? "");
  const saveLink = useMutation({ mutationFn: () => setPaymentLink(concertId, linkEdit || null), onSuccess: onChange });

  return (
    <div>
      {data.summary && (
        <p>{eur(data.summary.outstanding_cents)} ausstehend · {data.summary.confirmed_count} bestätigt</p>
      )}
      <label>PayPal-Link:
        <input aria-label="PayPal-Link" value={linkEdit} onChange={(e) => setLinkEdit(e.target.value)} />
        <button onClick={() => saveLink.mutate()}>speichern</button>
      </label>
      <ul>
        {data.items.map((it) => (
          <li key={it.id}>
            {it.display_name}: {eur(it.amount_cents)} ({it.status})
            <input aria-label={`Betrag ${it.display_name}`} defaultValue={(it.amount_cents / 100).toString()}
              onBlur={(e) => setAmt.mutate({ id: it.id, cents: Math.round(parseFloat(e.target.value || "0") * 100) })} />
            {it.status !== "confirmed"
              ? <button onClick={() => confirm.mutate(it.id)}>bestätigen</button>
              : <button onClick={() => unconfirm.mutate(it.id)}>Bestätigung zurücknehmen</button>}
            <button onClick={() => remove.mutate(it.id)}>entfernen</button>
          </li>
        ))}
      </ul>
      <button onClick={() => deactivate.mutate()}>Sammeln beenden</button>
    </div>
  );
}
