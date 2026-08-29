# esaProperti — Product UX Blueprint (Fase Product Completion)

> Status: **FONDASI PRODUK — SSOT UX untuk semua increment berikutnya.** 2026-07-17.
> Target: menggantikan **Jurnal.id** untuk perusahaan developer properti —
> accounting engine setara/lebih baik + property workflow yang mereka tak punya.
> Referensi flow: Jurnal.id (bukan copy UI). Gaya: modern SaaS, clean, minimal,
> enterprise; familiar bagi user lama Jurnal.id tapi lebih cepat dipakai.
> Semua angka dari SATU accounting engine (ledger-centric) — no duplicate SoT.

---

## 1. Prinsip produk (uji setiap keputusan)

| # | Prinsip | Arti praktis |
|---|---|---|
| U1 | **Lebih cepat dari Jurnal.id** | Aksi harian (catat biaya, terima pembayaran, cetak kwitansi) ≤ 3 klik dari dashboard; form default cerdas (tanggal hari ini, akun terakhir dipakai) |
| U2 | **Familiar** | Vocabulary Jurnal.id dipertahankan: Dashboard, Penjualan, Biaya, Laporan, Daftar Akun, Jurnal — user lama tidak tersesat |
| U3 | **Properti = first-class** | Proyek/Unit/RAB/Booking/BAST ada di navigasi utama, bukan "fitur tambahan" |
| U4 | **Setiap angka bisa diklik** | Drill-down: tile dashboard → laporan → baris jurnal sumber (ledger backbone memungkinkan ini) |
| U5 | **Empty state = onboarding** | Setiap layar kosong menjelaskan fungsi + CTA langkah pertama (sudah jadi konvensi repo) |
| U6 | **Status pakai warna konsisten** | Satu palet badge global (lihat §6) — sama di tabel, kartu, dashboard |
| U7 | **Mobile: baca & approve** | Responsive untuk konsumsi (dashboard, laporan, approve) — input berat boleh desktop-first |
| U8 | **Frontend tidak menghitung uang** | Semua agregat dari API (perbaiki 4 pelanggaran float existing — backlog FD-1) |

## 2. Information Architecture — sidebar target

Sidebar sekarang (13 item, 3 grup) sudah dekat. Target (evolusi, bukan rewrite):

```
OPERASIONAL                      KEUANGAN                    LAPORAN & ADMIN
├─ Dashboard                     ├─ Penagihan (collection)   ├─ Laporan
├─ Proyek                        ├─ Invoice Customer         ├─ Pajak
│   └─ [workspace: RAB · Biaya   ├─ Piutang Customer         ├─ Persetujuan (BARU)
│      · Unit · Alokasi · Jual]  ├─ Biaya                    └─ Pengaturan (isi!)
├─ Penjualan                     ├─ Jurnal                       ├─ Pengguna & Role
│   ├─ Booking (BARU)            ├─ Daftar Akun                  ├─ Payment Scheme
│   ├─ Kontrak & Unit            ├─ Saldo Awal                   ├─ Tax Rule
│   └─ Jadwal & Pembayaran       └─ Periode & Tutup Buku         └─ Approval Workflow
└─ Pelanggan (BARU — angkat
   customer ke navigasi utama)
```

**Perubahan kunci vs sekarang** (masing-masing = increment kecil frontend):
- **IA-1** Proyek jadi *workspace* (tab RAB/Biaya/Unit/Alokasi/Jual di `/proyek/[id]`) — menghapus 5–6 hop sidebar (roadmap lama P1-2, belum dikerjakan).
- **IA-2** `Pelanggan` masuk sidebar (backend customer master sudah ada, UI belum menonjol) — paritas Jurnal.id "Customer".
- **IA-3** `Persetujuan` (approval inbox) — engine Increment 5 belum ber-UI.
- **IA-4** `Pengaturan` diisi: Pengguna, Payment Scheme, Tax Rule, Approval Workflow (semua endpoint sudah ada, tanpa UI admin).
- **IA-5** `Booking` di bawah Penjualan (frontend spec Increment 7 sudah ditulis).

## 3. Dashboard Owner — spesifikasi target

Baris 1 — **Posisi hari ini** (semua derived dari ledger, endpoint sudah ada):
`Kas & Bank` · `Piutang (AR)` · `Uang Muka + Titipan Booking` · `Pajak Terhutang`
→ sumber: trial-balance/balance-sheet; klik → buku besar akun.

Baris 2 — **Pipeline properti**: funnel unit `Available → Booked → Reserved/PPJB → Sold`
per proyek (sales-pipeline + status unit baru) + `Nilai kontrak` + `DP terkumpul`.

Baris 3 — **Kinerja**: `L/R bulan berjalan` (income-statement) · `Margin per proyek`
(project-pl) · `Collection rate & overdue` (ar-aging/collection) · `Arus kas` (cash-flow).

Baris 4 — **Perhatian** (actionable): cicilan jatuh tempo minggu ini · booking lewat
masa berlaku · RAB over-budget · BAST tanpa PPh (endpoint compliance sudah ada) ·
approval menunggu.

## 4. Katalog laporan owner (Prioritas #4) — status aktual

| Laporan | Status | Sumber |
|---|---|---|
| L/R per proyek | ✅ ada | `GET /reports/project-pl/{id}` |
| L/R keseluruhan | ✅ ada | `GET /reports/income-statement` |
| Cash flow | ✅ ada | `GET /reports/cash-flow` |
| Stok unit (available/booked/reserved/sold) | ✅ ada (booked baru) | `GET /reports/sales-pipeline` + unit status |
| Aging piutang | ✅ ada | `GET /reports/ar-aging` |
| HPP proyek | ✅ ada (budgeted); **aktual final butuh P0-4** | project-pl + hpp snapshot |
| Margin per proyek | ✅ ada; presisi final butuh P0-4 true-up | project-pl |
| Progress proyek | 🟡 derived-able: % realisasi RAB (biaya) + % unit terjual — perlu definisi & widget | rab-vs-realisasi + pipeline |
| Penjualan per sales | 🟡 atribusi `sales_person_id` SUDAH di kontrak+booking; read-model & UI belum | → **PI-2** |
| Marketing performance (funnel, target) | ❌ blueprint §4 (READ-MODEL) belum dibangun | → **PI-2** |
| Bonus/komisi sales | ❌ Commission = SEAM blueprint §3 (Rule→Entry→Approval→Payment→Journal) | → **PI-3** |
| ROI proyek | ❌ perlu definisi (laba proyek ÷ total biaya terkapitalisasi) — read-model murni, tanpa tabel baru | → **PI-2** |

**Kesimpulan:** 7/13 sudah ada dari engine; 2 menunggu P0-4 untuk presisi;
4 adalah read-model/SEAM yang memang increment berikutnya — semuanya derived
dari ledger + atribusi yang sudah ditanam (tidak ada duplicate SoT).

## 5. Roadmap fase Product Completion

Urutan backend LOCKED tetap (P0-4 → Increment 8). Increment produk (PI) berjalan
menyisip — frontend-heavy, nol risiko ledger:

| # | Increment | Isi | Ledger risk |
|---|---|---|---|
| **Next** | **P0-4 Completion True-Up** | Engine closing HPP aktual (design v3 LOCKED) + UI closing wizard (completion → preview variance → calculate → approve → post) | Posting baru (desain terkunci) |
| PI-1 | Dashboard Owner v2 + IA-1 workspace proyek | §3 + tab proyek; komposisi endpoint existing | Nol |
| PI-2 | Sales & Property Analytics (read-model) | Penjualan per sales, funnel booking→BAST, ROI & progress proyek; `GET /reports/sales-performance` dkk (read-only) | Nol |
| Inc 8 | Cancellation (8a pra-BAST dulu; 8b setelah P0-4 valid) | + UI refund/cancel | Reversing (desain §1) |
| PI-3 | Commission (SEAM → build) | Rule konfigur. + akrual + approval + payment | Posting baru (blueprint §3) |
| PI-4 | Admin & Governance UI (IA-3/IA-4) + Booking UI (spec Inc 7) + follow-up F-1 reservasi | UI untuk engine yang sudah ada | Nol |

Setiap increment: 8 deliverable wajib (Implementation Report, Frontend Spec, User
Flow, Dashboard Impact, Report Impact, Migration Impact, Backward Compat, Backlog).

## 6. Sistem visual (ringkas — detail per spec)

- **Badge status global:** hijau=selesai/lunas/available · biru=aktif/berjalan ·
  ungu=ppjb/komitmen · oranye=perhatian/overdue-soon/hold · merah=overdue/blocked/batal ·
  abu=terminal netral (sold/superseded/expired). Uang selalu rata kanan, format `Rp 1.234.567`.
- **Pola halaman list:** header (judul + CTA primer) → filter bar (tab status + cari +
  dropdown proyek) → tabel → empty state CTA. Loading = skeleton rows; error = banner
  retry; success = toast + refresh data.
- **Permission di UI:** viewer = tanpa tombol tulis; accountant = operasional;
  owner = + approve/tutup buku/kelola user (mengikuti `RequireWrite`/`RequireRole` backend).

## 7. UX-debt backlog (dari audit — dikerjakan menyisip)

| # | Item | Asal temuan |
|---|---|---|
| FD-1 | Hapus 4 titik float money di FE (JournalDetail/CreateForm, OpeningBalance, AllocationResultTable) — pindahkan total ke API/tampilkan dari backend | Audit awal |
| FD-2 | Aksi transisi unit di UI (reservasi manual) — guard BAST 6.1 | Increment 6.1 F-1 |
| FD-3 | Approval engine tanpa UI (inbox + riwayat + konfigurasi workflow) | Audit awal / IA-3 |
| FD-4 | `Pengaturan` kosong ("Segera hadir") | Sidebar aktual |
| FD-5 | Konsolidasi 2 halaman detail unit (`/proyek/[id]/unit/[id]` vs `/penjualan/[unitId]`) | Roadmap lama P2 |
