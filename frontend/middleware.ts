import { NextResponse, type NextRequest } from "next/server";
import { marketingMayOpenPage, homePathFor } from "@/lib/roles";

const COOKIE_NAME = "esa_session";
// /api/v1 dan /health adalah proxy ke backend (auth via Bearer di backend) —
// JANGAN redirect ke halaman /login; biarkan transparan agar backend membalas
// 401 JSON yang benar, bukan HTML redirect.
const PUBLIC_PATHS = ["/login", "/register", "/api/auth", "/api/v1", "/health"];

/**
 * Berkas statis di /public tidak pernah dilindungi sesi.
 *
 * Matcher di bawah hanya mengecualikan /_next dan favicon, jadi /logo.png ikut
 * masuk middleware — dan tanpa cookie ia dijawab 307 ke /login. Akibatnya logo
 * pecah persis di satu layar yang menurut definisinya belum punya sesi: halaman
 * login. Gambar di /public bukan data tenant; melindunginya tidak mengamankan
 * apa pun, hanya merusak tampilan.
 */
const STATIC_FILE = /\.(?:png|jpe?g|gif|svg|webp|avif|ico|txt|xml|webmanifest|woff2?)$/i;

/**
 * Masa berlaku token, dibaca dari klaim `exp` JWT.
 *
 * Middleware berjalan di Edge runtime: tidak ada Buffer, jadi base64url
 * di-decode manual dengan atob. Verifikasi tanda tangan TIDAK dilakukan di
 * sini dan memang tidak perlu — backend tetap memverifikasinya pada setiap
 * request. Yang dikerjakan di sini hanya menghindari satu hal: mengantar
 * pemakai ke halaman yang pasti gagal memuat karena tokennya sudah mati.
 */
function decodeClaims(token: string): { exp?: number; role?: string } | null {
  try {
    const part = token.split(".")[1];
    if (!part) return null;
    const b64 = part.replace(/-/g, "+").replace(/_/g, "/");
    return JSON.parse(atob(b64.padEnd(b64.length + ((4 - (b64.length % 4)) % 4), "=")));
  } catch {
    // Token tak terbaca sama sekali = tidak bisa dipercaya.
    return null;
  }
}

function isExpired(claims: { exp?: number } | null): boolean {
  if (!claims || typeof claims.exp !== "number") return true;
  return claims.exp * 1000 <= Date.now();
}

function toLogin(req: NextRequest, expired: boolean) {
  const url = req.nextUrl.clone();
  url.pathname = "/login";
  url.search = "";
  if (expired) url.searchParams.set("expired", "1");
  const res = NextResponse.redirect(url);
  // Cookie mati ikut dibuang di sini, supaya pemakai tidak berputar-putar
  // membawa token yang sudah tidak berlaku.
  if (expired) res.cookies.set(COOKIE_NAME, "", { path: "/", maxAge: 0 });
  return res;
}

export function middleware(req: NextRequest) {
  const { pathname } = req.nextUrl;

  const isPublic = PUBLIC_PATHS.some(p => pathname.startsWith(p))
    || pathname.startsWith("/_next")
    || pathname.startsWith("/favicon")
    || STATIC_FILE.test(pathname);

  if (isPublic) return NextResponse.next();

  const token = req.cookies.get(COOKIE_NAME)?.value;
  if (!token) return toLogin(req, false);

  const claims = decodeClaims(token);
  if (isExpired(claims)) return toLogin(req, true);

  // W-12 — batas halaman per role. Otorisasi sesungguhnya ada di backend
  // (auth.Scope membalas 403); ini hanya mengantar marketing ke halaman yang
  // memang bisa ia pakai alih-alih layar error.
  if (claims?.role === "marketing" && !marketingMayOpenPage(pathname)) {
    const url = req.nextUrl.clone();
    url.pathname = homePathFor("marketing");
    url.search = "";
    return NextResponse.redirect(url);
  }

  return NextResponse.next();
}

export const config = {
  matcher: ["/((?!_next/static|_next/image|favicon.ico).*)"],
};
