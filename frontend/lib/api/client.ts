// Resolusi base URL — KRITIS untuk runtime:
//   - Server (Node/SSR): hit backend langsung lewat URL internal
//     (mis. http://backend:8080 di Docker) — server bisa resolve hostname itu.
//   - Browser (client component): pakai same-origin "/api/v1" yang di-PROXY oleh
//     Next.js rewrite (lihat next.config.ts) ke backend. WAJIB karena
//     NEXT_PUBLIC_API_URL bisa berisi hostname Docker ("backend") yang TIDAK
//     resolvable dari browser, dan agar tidak kena CORS (selalu same-origin).
const INTERNAL_API = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";

/** apiBase: base URL untuk path /api/v1 sesuai lingkungan (server vs browser). */
export function apiBase(): string {
  return typeof window === "undefined" ? `${INTERNAL_API}/api/v1` : "/api/v1";
}

export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
    // Body error apa adanya. Sebagian endpoint menyertakan data yang HARUS
    // dipakai layar untuk memulihkan diri — mis. 400 posting jurnal kas
    // membawa `choices` berisi jenis bukti yang sah (W-3.6). Membuangnya
    // memaksa frontend menebak ulang aturan yang sudah dijawab server.
    public payload?: Record<string, unknown>,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

interface FetchOptions extends Omit<RequestInit, "body"> {
  body?: unknown;
  token?: string; // diisi oleh server components/route handlers saja
}

// ── Sesi berakhir: satu penanganan, satu kali ────────────────────────────────
//
// Setiap 401 dari API yang terautentikasi berarti hal yang sama: token sudah
// tidak berlaku. Halaman tidak boleh menanganinya sendiri-sendiri — dulu hanya
// SATU halaman yang melakukannya, sehingga di halaman lain pemakai melihat
// deretan pesan error tanpa pernah diberi tahu bahwa ia hanya perlu login lagi.
//
// Penjaga `loggingOut` penting: satu layar bisa menembakkan 5–6 request paralel
// yang semuanya balas 401 bersamaan. Tanpa penjaga itu, kita memicu 6 logout dan
// 6 redirect sekaligus.
let loggingOut = false;

function handleUnauthorized(): void {
  // Di server (SSR/route handler) tidak ada window untuk diarahkan. Biarkan
  // ApiError naik; middleware.ts dan layout yang mengurus redirect di sana.
  if (typeof window === "undefined") return;
  if (loggingOut) return;
  // Sudah di halaman login — jangan mengarahkan diri sendiri ke tempat yang sama.
  if (window.location.pathname.startsWith("/login")) return;
  loggingOut = true;

  void fetch("/api/auth/logout", { method: "POST" })
    .catch(() => { /* cookie tetap dibuang oleh middleware saat redirect */ })
    .finally(() => {
      window.location.href = "/login?expired=1";
    });
}

// apiFetch: wrapper utama. Digunakan dari server (route handlers) maupun client.
// Untuk client components: token dikirim via credentials:"include" + Next.js proxy.
export async function apiFetch<T>(
  path: string,
  opts: FetchOptions = {},
): Promise<T> {
  const { body, token, ...rest } = opts;

  const headers: Record<string, string> = {
    "Content-Type": "application/json",
    ...(rest.headers as Record<string, string>),
  };

  if (token) {
    headers["Authorization"] = `Bearer ${token}`;
  }

  const res = await fetch(`${apiBase()}${path}`, {
    ...rest,
    headers,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });

  if (!res.ok) {
    let message = `HTTP ${res.status}`;
    let payload: Record<string, unknown> | undefined;
    try {
      const err = (await res.json()) as { error?: string } & Record<string, unknown>;
      message = err.error ?? message;
      payload = err;
    } catch {
      // ignore parse error
    }
    if (res.status === 401) handleUnauthorized();
    throw new ApiError(res.status, message, payload);
  }

  // 204 No Content
  if (res.status === 204) return undefined as unknown as T;

  return res.json() as Promise<T>;
}
