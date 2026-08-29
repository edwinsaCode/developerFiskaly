"use client";

// Izin peran untuk keputusan tampilan.
//
// Ini BUKAN batas keamanan — backend tetap satu-satunya yang menolak. Yang
// dijaga di sini adalah janji layar: tombol yang pasti berakhir 403 sebaiknya
// tidak ditawarkan, karena pemakai baru tahu setelah mengisi seluruh formulir.
//
// Dipakai lewat hook supaya `useUserSafe()` + predikat peran tidak disalin
// belasan kali; salinan seperti itu yang dulu membuat satu peran punya dua
// perlakuan berbeda di dua layar.

import { useUserSafe } from "@/lib/context/UserContext";
import { canManageUsers, canSell, canWrite } from "@/lib/roles";

/** Boleh menulis transaksi & master data akuntansi: Pemilik, Accounting. */
export function useMayWrite(): boolean {
  const user = useUserSafe();
  return user ? canWrite(user.role) : false;
}

/** Boleh menulis data penjualan (lead, booking, kontrak): + Marketing. */
export function useMaySell(): boolean {
  const user = useUserSafe();
  return user ? canSell(user.role) : false;
}

/** Boleh menambah/mengubah pengguna: Pemilik saja. */
export function useMayManageUsers(): boolean {
  const user = useUserSafe();
  return user ? canManageUsers(user.role) : false;
}
