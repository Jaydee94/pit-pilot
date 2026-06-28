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

export type PaymentItem = {
  id: string; user_id: string; display_name: string; avatar_url?: string;
  amount_cents: number; status: "open" | "reported" | "confirmed";
  reported_at?: string | null; confirmed_at?: string | null;
};
export type PaymentSummary = {
  outstanding_cents: number; confirmed_cents: number;
  open_count: number; reported_count: number; confirmed_count: number;
};
export type PaymentView = {
  responsible_user_id: string; default_amount_cents: number; payment_link: string | null;
  is_responsible: boolean; items: PaymentItem[]; summary: PaymentSummary | null;
};

const paymentsBase = (concertId: string) => `/api/concerts/${concertId}/payments`;

export const getPayments = (concertId: string) => apiFetch<PaymentView>(paymentsBase(concertId));
export const activatePayments = (concertId: string, default_amount_cents: number, payment_link?: string) =>
  apiFetch<{ id: string }>(paymentsBase(concertId), { method: "POST", body: JSON.stringify({ default_amount_cents, payment_link: payment_link ?? null }) });
export const setPaymentLink = (concertId: string, payment_link: string | null) =>
  apiFetch<{ id: string }>(paymentsBase(concertId), { method: "PATCH", body: JSON.stringify({ payment_link }) });
export const deactivatePayments = (concertId: string) =>
  apiFetch<void>(paymentsBase(concertId), { method: "DELETE" });
export const addPaymentItem = (concertId: string, user_id: string, amount_cents?: number) =>
  apiFetch<PaymentItem>(`${paymentsBase(concertId)}/items`, { method: "POST", body: JSON.stringify({ user_id, amount_cents: amount_cents ?? null }) });
export const setPaymentAmount = (concertId: string, itemId: string, amount_cents: number) =>
  apiFetch<PaymentItem>(`${paymentsBase(concertId)}/items/${itemId}`, { method: "PATCH", body: JSON.stringify({ amount_cents }) });
export const removePaymentItem = (concertId: string, itemId: string) =>
  apiFetch<void>(`${paymentsBase(concertId)}/items/${itemId}`, { method: "DELETE" });
export const reportPayment = (concertId: string, itemId: string) =>
  apiFetch<PaymentItem>(`${paymentsBase(concertId)}/items/${itemId}/report`, { method: "POST" });
export const unreportPayment = (concertId: string, itemId: string) =>
  apiFetch<PaymentItem>(`${paymentsBase(concertId)}/items/${itemId}/report`, { method: "DELETE" });
export const confirmPayment = (concertId: string, itemId: string) =>
  apiFetch<PaymentItem>(`${paymentsBase(concertId)}/items/${itemId}/confirm`, { method: "POST" });
export const unconfirmPayment = (concertId: string, itemId: string) =>
  apiFetch<PaymentItem>(`${paymentsBase(concertId)}/items/${itemId}/confirm`, { method: "DELETE" });

export type PushSubscriptionJSON = { endpoint: string; keys: { p256dh: string; auth: string } };

export const getAuthConfig = () =>
  apiFetch<{ google_client_id: string; allow_dev_login: boolean }>("/auth/config");

export const getVapidPublicKey = () => apiFetch<{ public_key: string }>("/api/push/vapid-public-key");
export const savePushSubscription = (sub: PushSubscriptionJSON) =>
  apiFetch<{ id: string }>("/api/push/subscriptions", { method: "POST", body: JSON.stringify(sub) });
export const deletePushSubscription = (endpoint: string) =>
  apiFetch<void>("/api/push/subscriptions", { method: "DELETE", body: JSON.stringify({ endpoint }) });

export const register = (email: string, password: string, displayName: string) =>
  apiFetch<{ id: string; display_name: string; avatar_url: string | null }>("/auth/register", {
    method: "POST",
    body: JSON.stringify({ email, password, display_name: displayName }),
  });

export const passwordLogin = (email: string, password: string) =>
  apiFetch<{ id: string; display_name: string; avatar_url: string | null }>("/auth/login", {
    method: "POST",
    body: JSON.stringify({ email, password }),
  });

export const devLogin = (name: string) =>
  apiFetch<{ id: string; display_name: string; avatar_url: string | null }>("/auth/dev-login", {
    method: "POST",
    body: JSON.stringify({ name }),
  });
