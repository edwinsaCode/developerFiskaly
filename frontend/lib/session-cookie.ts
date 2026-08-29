// Satu aturan umur cookie sesi, dipakai setiap jalan masuk yang memasangnya.
//
// Sebelum W-12 cookie dipatok 7 hari sementara JWT berumur 24 jam. Selisih
// itulah sumber keluhan "tiba-tiba semua halaman error": cookie masih ada, jadi
// tidak ada yang menganggap sesi berakhir, tapi setiap panggilan API dijawab
// 401 oleh backend. Menyamakan keduanya membuat sesi berakhir sebagai satu
// peristiwa, bukan dua.
//
// Helper ini hidup terpisah karena ada DUA jalan masuk — /api/auth/login dan
// /api/auth/register. Memperbaiki satu dan melupakan yang lain persis seperti
// yang sudah terjadi sekali; menaruh aturannya di satu berkas menutup celah itu
// untuk jalan masuk berikutnya juga.

export const SESSION_COOKIE = "esa_session";

/**
 * Pesan error backend, dikupas dari amplop JSON-nya.
 *
 * Backend membalas `{"error":"email atau password salah"}`. Membungkus badan
 * itu apa adanya ke dalam `{error: <teks>}` menghasilkan amplop di dalam
 * amplop, dan yang sampai ke layar adalah `{"error":"email atau password
 * salah"}` — JSON mentah, terbaca sebagai pesan sistem yang bocor, bukan
 * kalimat untuk manusia.
 */
export function backendErrorMessage(body: string, fallback: string): string {
  const text = body.trim();
  if (!text) return fallback;
  try {
    const parsed = JSON.parse(text);
    if (parsed && typeof parsed.error === "string" && parsed.error.trim()) {
      return parsed.error;
    }
  } catch {
    // Bukan JSON — pakai teksnya apa adanya.
  }
  return text.startsWith("{") ? fallback : text;
}

/** Umur cookie dalam detik = sisa umur token, dibaca dari klaim `exp`-nya. */
export function cookieLifetime(token: string): number {
  const FALLBACK = 60 * 60 * 24; // 24 jam — sama dengan JWT TTL default backend
  try {
    const part = token.split(".")[1];
    if (!part) return FALLBACK;
    const payload = JSON.parse(Buffer.from(part, "base64url").toString("utf-8"));
    if (typeof payload.exp !== "number") return FALLBACK;
    const seconds = Math.floor(payload.exp - Date.now() / 1000);
    return seconds > 0 ? seconds : FALLBACK;
  } catch {
    return FALLBACK;
  }
}

/** Opsi cookie sesi yang sudah lengkap — termasuk umur yang mengikuti token. */
export function sessionCookieOptions(token: string) {
  return {
    httpOnly: true,
    secure: process.env.NODE_ENV === "production",
    sameSite: "lax" as const,
    path: "/",
    maxAge: cookieLifetime(token),
  };
}
