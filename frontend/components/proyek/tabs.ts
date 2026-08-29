// Daftar tab workspace proyek — data murni, sengaja TERPISAH dari WorkspaceNav.
//
// WorkspaceNav adalah "use client". Ketika server component mengimpor nilai
// (bukan tipe) dari modul client, yang diterimanya adalah client reference,
// bukan nilainya: `MARKETING_TABS.includes` meledak saat render di server.
// Karena itu konstanta ini tinggal di modul biasa yang boleh dibaca keduanya.

export type WorkspaceTab =
  | "overview"
  | "unit"
  | "penjualan"
  | "booking"
  | "rab"
  | "biaya"
  | "alokasi"
  | "closing"
  | "timeline"
  | "dokumen"
  | "tanah";

// W-12 — tab yang boleh dilihat marketing. Overview memuat KPI keuangan proyek,
// RAB/Biaya/Alokasi/Closing adalah data biaya dan HPP, Timeline & Dokumen bukan
// pekerjaan penjualan. Semuanya 403 di backend; di sini hanya agar tautannya
// tidak ditawarkan.
export const MARKETING_TABS: WorkspaceTab[] = ["unit", "penjualan", "booking"];
