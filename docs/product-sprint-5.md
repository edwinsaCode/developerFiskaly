# Product Sprint 5 — Accounting Workspace — SHIPPED

> Status: **SHIPPED + verifikasi visual Chrome + E2E API.** 2026-07-21.
> Pengalaman akuntansi setara Jurnal.id: satu workspace, drill-down, jurnal berulang.

## 1. Accounting Workspace Nav

`AccountingNav` — bar tab konsisten di seluruh halaman akuntansi inti:
**Jurnal · Buku Besar · Neraca Saldo · Daftar Akun · Saldo Awal · Jurnal
Berulang · Periode & Tutup Buku · Pajak** — terpasang di jurnal/coa/
opening-balance/periods/gl/recurring. Akuntan berpindah antar area 1 klik.

## 2. Buku Besar (`/accounting/gl`) — inti drill-down PS-6

Akun (dropdown seluruh COA) + rentang tanggal + cari deskripsi/referensi →
tabel mutasi dengan **saldo berjalan** (dihitung backend sesuai normal balance)
→ setiap baris **klik → jurnal sumber** (`/accounting/jurnal/{id}`). Saldo
akhir di header. Deep-link `?account={id}` (dipakai drill dari laporan).
Rantai PS-6 kini hidup: laporan → akun → transaksi → jurnal → dokumen (ref).

## 3. Jurnal Berulang (`/accounting/recurring`)

**Backend** (`internal/ledger/recurring.go` + migration `000048`):
- Template = KONFIGURASI (bukan catatan akuntansi); hasil eksekusi = jurnal
  **POSTED biasa via PostingService** — balanced dijamin engine, period-checker
  tetap menjaga, `source='recurring'`, reference `RJ-{id}-{YYYY-MM}`.
- Validasi template: Σ debit == Σ kredit > 0, akun wajib, day 1–28.
- `RunDue` sweep **idempoten per (template, bulan)** via `last_run_ym`
  (guard pinned) — aman dipanggil berulang / via cron.
- API: `GET/POST /ledger/recurring` · `POST /ledger/recurring/run-due` ·
  `PUT /ledger/recurring/{id}/active`.
- Catatan teknis: `lines` reserved word MySQL 8 → backtick di migration
  (sempat dirty state 48, di-force clean + re-migrate, terverifikasi).

**Frontend**: daftar template (nama, tanggal posting, akun, total, terakhir
jalan, toggle aktif), tombol **[Jalankan yang Jatuh Tempo]**, modal template
dengan baris jurnal dinamis (akun + debit/kredit saling-eksklusif, + tambah baris).

## 4. Verifikasi
- E2E: buat template Sewa 15jt → run-due `{created:1}` → run kedua `{created:0}`
  (idempoten) → `last_run_ym=2026-07` → template unbalanced ditolak dgn pesan jelas.
- `tsc` hijau · ledger+reporting suite hijau · screenshot GL & Recurring
  (AccountingNav tampil, data template nyata).

## 5. Backlog PS-5
| # | Item |
|---|---|
| PS5-B1 | GL: link ke Buku Besar dari Neraca Saldo & COA (drill dari laporan → `?account=`) — bagian PS-6 |
| PS5-B2 | Recurring: auto-run via cron harian (kini tombol manual — endpoint sudah cron-safe) |
| PS5-B3 | GL: filter per proyek (param `project_id` backend sudah ada) |
| PS5-B4 | GL initialAccountId lintas-tenant guard (deep-link akun tenant lain → placeholder) |
| PS5-B5 | Jurnal berulang: edit template (kini create+toggle; koreksi = nonaktifkan + buat baru) |
