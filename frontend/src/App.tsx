import { BrowserRouter, Routes, Route, Navigate } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { AuthProvider, useAuth } from "./auth/AuthContext";
import Login from "./routes/Login";
import Groups from "./routes/Groups";
import GroupDetail from "./routes/GroupDetail";
import ConcertForm from "./routes/ConcertForm";
import ConcertDetail from "./routes/ConcertDetail";

const qc = new QueryClient();

function Protected({ children }: { children: ReactNode }) {
  const { userId, loading } = useAuth();
  if (loading) return <p>Lädt…</p>;
  return userId ? <>{children}</> : <Navigate to="/login" replace />;
}

export default function App() {
  return (
    <QueryClientProvider client={qc}>
      <AuthProvider>
        <BrowserRouter>
          <Routes>
            <Route path="/login" element={<Login />} />
            <Route path="/groups" element={<Protected><Groups /></Protected>} />
            <Route path="/groups/:id" element={<Protected><GroupDetail /></Protected>} />
            <Route path="/groups/:id/concerts/new" element={<Protected><ConcertForm /></Protected>} />
            <Route path="/concerts/:id" element={<Protected><ConcertDetail /></Protected>} />
            <Route path="*" element={<Navigate to="/groups" replace />} />
          </Routes>
        </BrowserRouter>
      </AuthProvider>
    </QueryClientProvider>
  );
}
