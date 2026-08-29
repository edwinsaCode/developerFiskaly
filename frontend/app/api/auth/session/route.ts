import { cookies } from "next/headers";
import { NextResponse } from "next/server";

const BACKEND = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";
const COOKIE_NAME = "esa_session";

/**
 * GET /api/auth/session — identitas user yang sedang login.
 *
 * Sebelum W-12 route ini men-decode klaim `user_id`/`tenant_id`/`email` yang
 * TIDAK ADA di token esaProperti (klaimnya `uid`/`tid`/`role`). Hasilnya
 * selalu id 0 dan email kosong — data karangan yang tampak sah. Sekarang ia
 * meneruskan ke backend GET /auth/me, satu-satunya sumber identitas yang benar,
 * dan meneruskan 401 apa adanya supaya pemanggil tahu sesinya sudah berakhir.
 */
export async function GET() {
  const cookieStore = await cookies();
  const token = cookieStore.get(COOKIE_NAME)?.value;
  if (!token) return NextResponse.json({ user: null }, { status: 401 });

  try {
    const res = await fetch(`${BACKEND}/api/v1/auth/me`, {
      headers: { Authorization: `Bearer ${token}` },
      cache: "no-store",
    });
    if (!res.ok) {
      return NextResponse.json({ user: null }, { status: res.status === 401 ? 401 : 502 });
    }
    return NextResponse.json({ user: await res.json() });
  } catch {
    // Backend tidak terjangkau — itu bukan sesi tidak sah.
    return NextResponse.json({ user: null }, { status: 502 });
  }
}
