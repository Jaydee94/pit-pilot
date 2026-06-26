import { useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import type { ChangeEvent, FormEvent } from "react";
import { createConcert } from "../api/client";

export default function ConcertForm() {
  const { id = "" } = useParams();
  const nav = useNavigate();
  const [f, setF] = useState({ artist: "", event_at: "", rsvp_deadline: "", venue: "", city: "", ticket_url: "", notes: "" });
  const set = (k: string) => (e: ChangeEvent<HTMLInputElement>) => setF((prev) => ({ ...prev, [k]: e.target.value }));
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const payload = {
      ...f,
      event_at: new Date(f.event_at).toISOString(),
      rsvp_deadline: new Date(f.rsvp_deadline).toISOString(),
    };
    const c = await createConcert(id, payload);
    nav(`/concerts/${c.id}`);
  };
  return (
    <form onSubmit={submit}>
      <h1>Konzert anlegen</h1>
      <input aria-label="Künstler" value={f.artist} onChange={set("artist")} required />
      <input aria-label="Datum/Zeit" type="datetime-local" value={f.event_at} onChange={set("event_at")} required />
      <input aria-label="Anmeldeschluss" type="datetime-local" value={f.rsvp_deadline} onChange={set("rsvp_deadline")} required />
      <input aria-label="Venue" value={f.venue} onChange={set("venue")} />
      <input aria-label="Stadt" value={f.city} onChange={set("city")} />
      <input aria-label="Ticket-Link" value={f.ticket_url} onChange={set("ticket_url")} />
      <input aria-label="Notizen" value={f.notes} onChange={set("notes")} />
      <button type="submit">Speichern</button>
    </form>
  );
}
