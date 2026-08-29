import { cookies } from "next/headers";
import { redirect } from "next/navigation";
import { AppShell } from "@/components/layout/AppShell";
import { apiFetch, ApiError } from "@/lib/api/client";
import { isTokenExpired } from "@/lib/auth";
import { User, Role } from "@/lib/types/api";

const COOKIE_NAME = "esa_session";

interface MeResponse {
  id: number;
  email: string;
  name: string;
  display_name: string;
  role: Role;
  tenant_id: number;
  tenant_name: string;
}

/** Identitas dari klaim token: id, tenant, role — TIDAK ada email dan nama. */
function userFromToken(token: string): User | null {
  try {
    const [, payloadB64] = token.split(".");
    const payload = JSON.parse(Buffer.from(payloadB64, "base64url").toString("utf-8"));
    return {
      id: payload.uid ?? 0,
      tenant_id: payload.tid ?? 0,
      email: "",
      role: (payload.role ?? "viewer") as Role,
    };
  } catch {
    return null;
  }
}

export default async function AppLayout({ children }: { children: React.ReactNode }) {
  const cookieStore = await cookies();
  const token = cookieStore.get(COOKIE_NAME)?.value ?? "";
  if (!token) redirect("/login");
  if (isTokenExpired(token)) redirect("/login?expired=1");

  // Satu panggilan identitas per pemuatan halaman, di server, di satu tempat.
  // Komponen membacanya dari context (useUser) — tidak ada component yang
  // memanggil /auth/me sendiri.
  let user: User | null = null;
  try {
    const me = await apiFetch<MeResponse>("/auth/me", { token, cache: "no-store" });
    user = {
      id: me.id,
      tenant_id: me.tenant_id,
      email: me.email,
      role: me.role,
      name: me.name,
      display_name: me.display_name,
      tenant_name: me.tenant_name,
    };
  } catch (err) {
    // 401 = token ditolak backend (kedaluwarsa, dicabut, user dihapus).
    if (err instanceof ApiError && err.status === 401) redirect("/login?expired=1");
    // Kegagalan lain (backend sedang mati, jaringan) BUKAN masalah sesi.
    // Menendang pemakai ke halaman login untuk itu adalah diagnosis yang salah:
    // ia akan login lagi dan gagal lagi. Render shell dengan identitas seadanya
    // dari token; halaman di dalamnya akan melaporkan errornya sendiri.
    user = userFromToken(token);
  }

  if (!user) redirect("/login");

  return (
    <AppShell
      user={user}
      token={token}
      periodLabel={new Date().toLocaleDateString("id-ID", { month: "long", year: "numeric" })}
    >
      {children}
    </AppShell>
  );
}
