import type { Role } from "@/lib/types/api";

// ── Kemampuan per role — cermin dari internal/domain/role.go ──────────────────
//
// Modul ini sengaja BEBAS dari next/headers dan API apa pun yang khusus server,
// supaya client component boleh mengimpornya juga.
//
// Ini HANYA untuk memutuskan apa yang dirender. Otorisasi sesungguhnya ada di
// backend (auth.Scope + RequireWrite/RequireRole); menyembunyikan tombol bukan
// pengamanan dan tidak boleh diperlakukan seperti itu.

export function canWrite(role: Role): boolean {
  return role === "owner" || role === "accountant";
}

/** Alur penjualan: prospek, pelanggan, booking, kontrak, jadwal pembayaran. */
export function canSell(role: Role): boolean {
  return role === "owner" || role === "accountant" || role === "marketing";
}

/** Kas, jurnal, COA, hutang, biaya, laporan keuangan. Marketing tidak. */
export function seesAccounting(role: Role): boolean {
  return role === "owner" || role === "accountant" || role === "viewer";
}

export function canManageUsers(role: Role): boolean {
  return role === "owner";
}

// ── W-12: halaman yang boleh dibuka marketing ────────────────────────────────
//
// Cermin dari auth.Scope di backend, dan sama-sama gagal-tertutup: halaman baru
// otomatis tertutup untuk marketing sampai ditulis di sini. Ini BUKAN
// pengamanan — datanya sudah dijaga backend dengan 403. Gunanya satu: marketing
// yang mengetik /accounting/jurnal mendapat halaman yang bisa ia pakai, bukan
// layar penuh pesan error.
//
// "*" cocok tepat satu segmen, tidak pernah prefiks — sehingga "/penjualan/*"
// tidak bisa meloloskan "/penjualan/12/invoice".
const MARKETING_PAGES = [
  "/proyek",
  "/proyek/*", // workspace proyek (tabnya sendiri sudah difilter)
  "/proyek/*/unit/*",
  "/penjualan",
  "/penjualan/*", // detail unit — kecuali yang ditolak di bawah
];

// Rute 2-segmen di bawah /penjualan yang BUKAN detail unit dan memang uang:
// komisi (beban), KPR (pencairan bank), pembatalan (refund).
const MARKETING_DENY = new Set([
  "/penjualan/komisi",
  "/penjualan/kpr",
  "/penjualan/pembatalan",
]);

export function marketingMayOpenPage(pathname: string): boolean {
  const path = pathname.replace(/\/+$/, "") || "/";
  if (MARKETING_DENY.has(path)) return false;
  const segs = path.split("/").filter(Boolean);
  return MARKETING_PAGES.some((shape) => {
    const want = shape.split("/").filter(Boolean);
    if (want.length !== segs.length) return false;
    return want.every((w, i) => w === "*" || w === segs[i]);
  });
}

/** Beranda tiap role — marketing tidak punya Dashboard. */
export function homePathFor(role: Role): string {
  return role === "marketing" ? "/penjualan" : "/dashboard";
}

export const ROLE_LABELS: Record<Role, string> = {
  owner: "Pemilik",
  accountant: "Accounting",
  marketing: "Marketing",
  viewer: "Viewer",
};

export const ALL_ROLES: Role[] = ["owner", "accountant", "marketing", "viewer"];

/** Deskripsi singkat untuk form pemilihan role. */
export const ROLE_DESCRIPTIONS: Record<Role, string> = {
  owner: "Akses penuh, termasuk mengelola pengguna.",
  accountant: "Seluruh akuntansi: kas, jurnal, hutang, biaya, laporan.",
  marketing: "Penjualan saja: prospek, pelanggan, booking, kontrak. Tanpa akses kas dan jurnal.",
  viewer: "Hanya melihat. Tidak dapat mengubah data.",
};
