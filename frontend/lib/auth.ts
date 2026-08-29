import { cookies } from "next/headers";
import type { Role } from "@/lib/types/api";
import { canWrite } from "@/lib/roles";

// Helper kemampuan role tinggal di lib/roles.ts (bebas server-only import)
// dan di-re-export di sini supaya pemanggil lama tidak perlu diubah.
export { canWrite, canSell, seesAccounting, canManageUsers, ROLE_LABELS } from "@/lib/roles";

const COOKIE_NAME = "esa_session";

export async function getToken(): Promise<string> {
  const store = await cookies();
  return store.get(COOKIE_NAME)?.value ?? "";
}

// JWT Claims dari backend (internal/platform/auth/jwt.go): uid, tid, role, exp.
// TIDAK ada email dan TIDAK ada nama di dalam token — identitas tampilan
// dibaca dari GET /auth/me, bukan ditebak dari token.
interface JwtPayload {
  uid?: number;
  tid?: number;
  role?: string;
  exp?: number;
}

function decodeJwtPayload(token: string): JwtPayload {
  try {
    const parts = token.split(".");
    if (parts.length < 2) return {};
    const payload = Buffer.from(parts[1], "base64url").toString("utf-8");
    return JSON.parse(payload) as JwtPayload;
  } catch {
    return {};
  }
}

/** True bila token sudah lewat masa berlakunya (atau tidak punya exp sama sekali). */
export function isTokenExpired(token: string, nowMs: number = Date.now()): boolean {
  if (!token) return true;
  const { exp } = decodeJwtPayload(token);
  if (typeof exp !== "number") return true;
  return exp * 1000 <= nowMs;
}

/**
 * getTokenAndRole — dipakai server component untuk menentukan token + hak tulis.
 *
 * `email` selalu string kosong: klaim JWT tidak memuatnya. Field ini
 * dipertahankan agar pemanggil lama tidak pecah, tetapi JANGAN dipakai untuk
 * menampilkan identitas — pakai useUser()/GET /auth/me.
 */
export async function getTokenAndRole(): Promise<{
  token: string;
  role: Role;
  email: string;
  canWrite: boolean;
}> {
  const token = await getToken();
  const payload = decodeJwtPayload(token);
  const role = (payload.role ?? "viewer") as Role;
  return { token, role, email: "", canWrite: canWrite(role) };
}
