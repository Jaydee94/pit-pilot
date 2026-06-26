export type ApiError = { code: string; message: string };

export async function apiFetch<T>(path: string, init: RequestInit = {}): Promise<T> {
  const res = await fetch(path, {
    credentials: "include",
    headers: { "Content-Type": "application/json", ...(init.headers ?? {}) },
    ...init,
  });
  if (!res.ok) {
    let err: ApiError = { code: "unknown", message: res.statusText };
    try {
      const body = await res.json();
      if (body?.error) err = body.error;
    } catch {
      /* keep default */
    }
    throw err;
  }
  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}

export type Group = { id: string; name: string; invite_code: string };
export type Concert = {
  id: string; group_id: string; artist: string; event_at: string;
  venue?: string; city?: string; ticket_url?: string; price_cents?: number;
  notes?: string; rsvp_deadline: string;
};
export type Member = { id: string; display_name: string; avatar_url?: string; role: string };
export type Rsvp = { id: string; display_name: string; avatar_url?: string; status: "yes" | "no" };

export const login = (provider: "google" | "apple", idToken: string) =>
  apiFetch<{ id: string }>(`/auth/${provider}`, { method: "POST", body: JSON.stringify({ id_token: idToken }) });
export const me = () => apiFetch<{ id: string }>("/api/me");
export const listGroups = () => apiFetch<Group[]>("/api/groups");
export const createGroup = (name: string) =>
  apiFetch<Group>("/api/groups", { method: "POST", body: JSON.stringify({ name }) });
export const joinGroup = (code: string) =>
  apiFetch<Group>("/api/groups/join", { method: "POST", body: JSON.stringify({ code }) });
export const getGroup = (id: string) => apiFetch<Group>(`/api/groups/${id}`);
export const groupMembers = (id: string) => apiFetch<Member[]>(`/api/groups/${id}/members`);
export const regenerateInvite = (id: string) =>
  apiFetch<{ invite_code: string }>(`/api/groups/${id}/invite`, { method: "POST" });
export const listConcerts = (groupId: string) => apiFetch<Concert[]>(`/api/groups/${groupId}/concerts`);
export const createConcert = (groupId: string, input: Record<string, unknown>) =>
  apiFetch<Concert>(`/api/groups/${groupId}/concerts`, { method: "POST", body: JSON.stringify(input) });
export const getConcert = (id: string) => apiFetch<Concert>(`/api/concerts/${id}`);
export const setRsvp = (concertId: string, status: "yes" | "no") =>
  apiFetch<unknown>(`/api/concerts/${concertId}/rsvp`, { method: "PUT", body: JSON.stringify({ status }) });
export const listRsvps = (concertId: string) => apiFetch<Rsvp[]>(`/api/concerts/${concertId}/rsvps`);
