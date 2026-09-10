// Struktur navigasi & index pencarian untuk halaman Panduan Sistem.
// Murni data (tanpa JSX) supaya bisa dipakai baik dari komponen server
// maupun client tanpa membawa serta konten React.

export interface PanduanNavItem {
  id: string;
  label: string;
  /** kata kunci tambahan untuk pencarian, tidak ditampilkan */
  keywords?: string;
}

export interface PanduanNavGroup {
  label: string;
  items: PanduanNavItem[];
}

export const PANDUAN_NAV: PanduanNavGroup[] = [
  {
    label: "Mulai di Sini",
    items: [
      { id: "alur", label: "Alur End-to-End Bisnis", keywords: "flow proses akad kpr hpp laba rugi visual step by step" },
      { id: "accounting-hpp", label: "Accounting & HPP", keywords: "hpp tanah konstruksi subsidi komersial sarana prasarana perizinan pemasaran lain-lain rab persediaan true-up" },
      { id: "jurnal-referensi", label: "Jurnal Referensi Cepat", keywords: "dr cr debit kredit jurnal akun coa referensi" },
    ],
  },
  {
    label: "Panduan Lengkap",
    items: [
      { id: "overview", label: "1. Overview Sistem", keywords: "pengantar audiens arsitektur domain platform" },
      { id: "setup-project", label: "2. Setup Project", keywords: "buat proyek tax category coa" },
      { id: "rab-realisasi", label: "3. RAB & Realisasi", keywords: "budget plan draft active superseded tier direct shared overhead realisasi persen" },
      { id: "unit-block", label: "4. Unit & Block", keywords: "land area saleable area status available booked sold cancelled bulk wizard" },
      { id: "biaya", label: "5. Biaya / Cost Entry", keywords: "cost entry kategori tier ap hutang usaha pengeluaran" },
      { id: "persediaan-hpp", label: "6. Persediaan & HPP", keywords: "inventory hpp budgeted true-up 1-3000 1-3100" },
      { id: "alokasi-hpp", label: "7. Alokasi HPP", keywords: "allocation snapshot largest remainder pool proporsional" },
      { id: "booking", label: "8. Booking", keywords: "booking fee non-refundable pendapatan booking" },
      { id: "penjualan-akad", label: "9. Penjualan / Kontrak / Akad", keywords: "akad bast gate pembatalan cancellation reversal komisi" },
      { id: "pembayaran-receipt", label: "10. Pembayaran & Receipt", keywords: "receive payment kwitansi kwt payment allocations termin cicilan partial" },
      { id: "kpr-jaminan-bank", label: "11. KPR & Dana Jaminan Bank", keywords: "kpr dana jaminan bank pencairan disbursement fully paid shortfall piutang" },
      { id: "kelebihan-tanah", label: "12. Kelebihan Tanah", keywords: "addon land tanah lebih produk tambahan" },
      { id: "pajak", label: "13. Pajak", keywords: "pph final ppn tarif subsidi komersial tax rule" },
      { id: "sales-commission", label: "14. Sales & Commission", keywords: "komisi salesperson clawback persen" },
      { id: "fixed-asset", label: "15. Fixed Asset & Depreciation", keywords: "aset tetap penyusutan depresiasi register disposal" },
      { id: "laporan-keuangan", label: "16. Laporan Keuangan", keywords: "neraca laba rugi arus kas trial balance pipeline pdf export ssot" },
      { id: "accounting-jurnal", label: "17. Accounting & Jurnal", keywords: "closing tutup buku append only dokumen numbering tenant isolation" },
      { id: "troubleshooting", label: "18. Troubleshooting", keywords: "error bug kenapa tidak bisa faq masalah" },
      { id: "glossary", label: "19. Glossary", keywords: "istilah definisi akronim kamus" },
    ],
  },
];

export const PANDUAN_FLAT_NAV: PanduanNavItem[] = PANDUAN_NAV.flatMap((g) => g.items);
