import { Link, useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { groupMembers, listConcerts } from "../api/client";

export default function GroupDetail() {
  const { id = "" } = useParams();
  const { data: members } = useQuery({ queryKey: ["members", id], queryFn: () => groupMembers(id) });
  const { data: concerts } = useQuery({ queryKey: ["concerts", id], queryFn: () => listConcerts(id) });
  return (
    <main>
      <h1>Gruppe</h1>
      <section><h2>Mitglieder</h2><ul>{members?.map((m) => <li key={m.id}>{m.display_name} ({m.role})</li>)}</ul></section>
      <section>
        <h2>Konzerte</h2>
        <Link to={`/groups/${id}/concerts/new`}>Konzert anlegen</Link>
        <ul>{concerts?.map((c) => <li key={c.id}><Link to={`/concerts/${c.id}`}>{c.artist} — {new Date(c.event_at).toLocaleDateString()}</Link></li>)}</ul>
      </section>
    </main>
  );
}
