import { useState } from "react";
import { Link } from "react-router-dom";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { listGroups, createGroup, joinGroup } from "../api/client";
import PushToggle from "../components/PushToggle";

export default function Groups() {
  const qc = useQueryClient();
  const { data: groups } = useQuery({ queryKey: ["groups"], queryFn: listGroups });
  const [name, setName] = useState("");
  const [code, setCode] = useState("");
  const create = useMutation({ mutationFn: () => createGroup(name), onSuccess: () => qc.invalidateQueries({ queryKey: ["groups"] }) });
  const join = useMutation({ mutationFn: () => joinGroup(code), onSuccess: () => qc.invalidateQueries({ queryKey: ["groups"] }) });
  return (
    <main>
      <h1>Meine Gruppen</h1>
      <PushToggle />
      <ul>{groups?.map((g) => <li key={g.id}><Link to={`/groups/${g.id}`}>{g.name}</Link></li>)}</ul>
      <form onSubmit={(e) => { e.preventDefault(); create.mutate(); }}>
        <input aria-label="Gruppenname" value={name} onChange={(e) => setName(e.target.value)} placeholder="Neue Gruppe" />
        <button type="submit">Anlegen</button>
      </form>
      <form onSubmit={(e) => { e.preventDefault(); join.mutate(); }}>
        <input aria-label="Einladungscode" value={code} onChange={(e) => setCode(e.target.value)} placeholder="Code" />
        <button type="submit">Beitreten</button>
      </form>
    </main>
  );
}
