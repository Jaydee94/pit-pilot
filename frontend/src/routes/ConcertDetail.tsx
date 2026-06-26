import { useParams } from "react-router-dom";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { getConcert, listRsvps, setRsvp } from "../api/client";

export default function ConcertDetail() {
  const { id = "" } = useParams();
  const qc = useQueryClient();
  const { data: concert } = useQuery({ queryKey: ["concert", id], queryFn: () => getConcert(id) });
  const { data: rsvps } = useQuery({ queryKey: ["rsvps", id], queryFn: () => listRsvps(id) });
  const mutate = useMutation({
    mutationFn: (status: "yes" | "no") => setRsvp(id, status),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["rsvps", id] }),
  });
  if (!concert) return <p>Lädt…</p>;
  return (
    <main>
      <h1>{concert.artist}</h1>
      <p>{new Date(concert.event_at).toLocaleString()} — {concert.venue} {concert.city}</p>
      <div>
        <button onClick={() => mutate.mutate("yes")}>Bin dabei</button>
        <button onClick={() => mutate.mutate("no")}>Kann nicht</button>
      </div>
      <h2>Wer kommt mit</h2>
      <ul>{rsvps?.map((r) => <li key={r.id}><span>{r.display_name}</span>: {r.status === "yes" ? "✅" : "❌"}</li>)}</ul>
    </main>
  );
}
