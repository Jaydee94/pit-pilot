import { Link, useParams } from "react-router-dom";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { groupMembers, listConcerts, getGroup, regenerateInvite } from "../api/client";

export default function GroupDetail() {
  const { id = "" } = useParams();
  const queryClient = useQueryClient();
  const { data: group } = useQuery({ queryKey: ["group", id], queryFn: () => getGroup(id) });
  const { data: members } = useQuery({ queryKey: ["members", id], queryFn: () => groupMembers(id) });
  const { data: concerts } = useQuery({ queryKey: ["concerts", id], queryFn: () => listConcerts(id) });
  const { mutate: regen } = useMutation({
    mutationFn: () => regenerateInvite(id),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["group", id] }),
  });
  return (
    <main>
      <h1>Gruppe</h1>
      <section>
        <h2>Einladungscode</h2>
        <code>{group?.invite_code}</code>
        <button onClick={() => group && navigator.clipboard.writeText(group.invite_code)}>Kopieren</button>
        <button onClick={() => regen()}>Neuen Code generieren</button>
      </section>
      <section><h2>Mitglieder</h2><ul>{members?.map((m) => <li key={m.id}>{m.display_name} ({m.role})</li>)}</ul></section>
      <section>
        <h2>Konzerte</h2>
        <Link to={`/groups/${id}/concerts/new`}>Konzert anlegen</Link>
        <ul>{concerts?.map((c) => <li key={c.id}><Link to={`/concerts/${c.id}`}>{c.artist} — {new Date(c.event_at).toLocaleDateString()}</Link></li>)}</ul>
      </section>
    </main>
  );
}
