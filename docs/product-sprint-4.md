# Product Sprint 4 — Collection Command Center — SHIPPED

> Status: **SHIPPED + verifikasi visual Chrome (data nyata).** 2026-07-21.
> `/accounting/collection` kini pusat operasional tim collection — bukan daftar piutang.

## Backend (additive pada engine aging existing — satu SoT)

`internal/reporting` ARAging diperluas (pure function `BuildARAging`, test lama tetap hijau):
- **Kontak buyer** per baris (`buyer_phone/email` via JOIN customers dari `sale_contracts.customer_id`) — bahan aksi reminder.
- **`due_today_count/amount`** (kartu KPI "HARI INI").
- **Prediksi kas masuk `expected_30/60/90`** — Σ outstanding jatuh tempo dalam N hari ke depan (kumulatif, asumsi bayar tepat waktu). Terverifikasi: boundary hari ke-30/90 benar.

## Frontend (CollectionView → Command Center)

- **5 KPI**: Total Outstanding · **Jatuh Tempo HARI INI (n)** · Menunggak · Minggu Ini · Collection Rate (basis transparan).
- **Prediksi Kas Masuk 30/60/90 hari** (strip hijau).
- **Umur Piutang** — stacked bar berwarna per bucket + legend (count + nominal).
- Filter + tab baru **"Hari Ini"**; tombol **"Tandai Menunggak (sweep)"** (endpoint mark-overdue existing, idempoten).
- **Aksi per baris (one-click)**: **WA** (`wa.me/62…` + template reminder berisi nama/unit/nominal/jatuh tempo/telat-hari; disabled dgn tooltip bila nomor kosong) · **Email** (mailto + subjek/isi) · **Bayar** (RecordPaymentButton existing → jurnal+kwitansi otomatis) · **Invoice** (halaman invoice unit) · **Statement**.
- Export CSV existing dipertahankan.

## Verifikasi
`tsc` hijau · reporting test hijau · E2E seed (kontrak scheme + 4 jadwal: overdue 50 hari, due-today, +30d, +90d) → semua angka KPI/prediksi/aging/filter/aksi tampil benar di screenshot.

## Backlog PS-4
| # | Item |
|---|---|
| PS4-B1 | Log aktivitas reminder (kapan WA/email dikirim, oleh siapa) — butuh tabel kecil |
| PS4-B2 | Template reminder configurable per tenant (Pengaturan) |
| PS4-B3 | Prediksi kas keluar (hutang usaha + komisi payable) → net cashflow forecast |
| PS4-B4 | Bulk reminder (kirim WA berurutan utk semua overdue) |
