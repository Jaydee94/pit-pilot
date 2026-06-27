import { createContext, useCallback, useContext, useEffect, useState, ReactNode } from "react";
import { me } from "../api/client";

type AuthState = { userId: string | null; loading: boolean; refresh: () => void };
const Ctx = createContext<AuthState>({ userId: null, loading: true, refresh: () => {} });

export function AuthProvider({ children }: { children: ReactNode }) {
  const [userId, setUserId] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const refresh = useCallback(() => {
    setLoading(true);
    me().then((u) => setUserId(u.id)).catch(() => setUserId(null)).finally(() => setLoading(false));
  }, []);
  useEffect(refresh, [refresh]);
  return <Ctx.Provider value={{ userId, loading, refresh }}>{children}</Ctx.Provider>;
}
export const useAuth = () => useContext(Ctx);
