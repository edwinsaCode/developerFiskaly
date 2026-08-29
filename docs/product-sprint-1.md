# Product Sprint 1 — UI untuk Increment 6–9 — SHIPPED

> Status: **SHIPPED + smoke-tested E2E (browser session riil).** 2026-07-21.
> Fase Product Completion. Referensi spec: `increment-7/8/9-frontend-spec.md`,
> `p0-4-frontend-spec.md`, SSOT desain `product-ux-blueprint.md`.

## 1. Screen inventory (yang dibangun)

| Route / lokasi | Komponen | Spec asal |
|---|---|---|
| `/penjualan/booking` | **BookingBoard** — tab status, banner sweep kedaluwarsa, aksi konversi/batal/refund | Inc 7 P1 |
| Modal dari halaman unit | **BookingFormModal** — customer (+quick-create), sales, fee, refundable, expiry, CashBankSelect | Inc 7 P3 |
| `/penjualan/[unitId]` | **UnitSalePanel diperluas**: kartu Booking Aktif (konversi), aksi Booking/Reservasi (unit available), tombol Batalkan Penjualan (sold), tombol batal pra-BAST, **UnitTimelineCard** (riwayat lifecycle) | Inc 6/7/8 |
| Modal Reservasi | transisi `available→reserved` via lifecycle API (menutup follow-up F-1) | Inc 6 FD-2 |
| `/proyek/[id]` tab **Penyelesaian** | **ClosingPanel** — stepper 5 langkah (Tandai Selesai → Finalisasi → Hitung → Setujui → Posting), ringkasan variance, tabel per unit×kategori + link jurnal, 3 ConfirmModal ireversibel | P0-4 C1–C4 |
| `/penjualan/pembatalan` | **CancellationCenter** — tab Pembatalan (tabel + drawer detail: ringkasan dampak L/R + **Journal Preview** akun-per-akun + approve/reject/process) & tab Refund (bayar via CashBankSelect, link jurnal) | Inc 8 X1–X4 |
| Modal dari halaman unit | **CancelRequestModal** — alasan, penalti, tanggal kejadian; deteksi stage otomatis | Inc 8 wizard step 1 |
| `/penjualan/komisi` | **CommissionBoard** — tab status, **aksi batch** (setujui/terutang/bayar terpilih), Hitung (+sync pembatalan otomatis), modal Aturan Komisi (list+form+toggle aktif) | Inc 9 M1–M3 |
| Sidebar | +3 menu: Booking, Komisi Sales, Pembatalan & Refund | IA |

**ContractForm ditulis ulang** (perbaikan penting): kini mengirim
`payment_scheme_id + customer_id + sales_person_id (+financing_source_id KPR)`
— kebijakan WAJIB Increment 3 yang sebelumnya tidak pernah dikirim UI (kontrak
via UI akan ditolak backend ber-scheme-flow); + `booking_id` untuk konversi.

**Infrastruktur**: `lib/api/{booking,closing,cancellation,commission,party}.ts`
(+ ekstensi `projects.ts` lifecycle, `sale.ts` kontrak), `lib/hooks/useUnitMap`,
`UnitStatus` 9 nilai + `StatusBadge` label/warna baru, badge khusus booking/
cancellation/commission mengikuti palet global.

## 2. 🐛 Bug produksi ditemukan & diperbaiki (backend)

`closing.Handler.Mount` memakai `r.Route("/projects/{projectID}", …)` yang
**menimpa subtree** `r.Route("/projects/{id}")` milik project handler (chi mount
collision) → SEMUA route detail proyek (GET detail, units, phases, transition)
menjadi 404 di server berjalan. Tidak tertangkap integration test (bypass mux).
Fix: registrasi **flat** (pola cost/budget). Regresi unit test hijau; endpoint
detail proyek + closing terverifikasi hidup via HTTP.

## 3. Verifikasi

- `tsc --noEmit` hijau (perbaikan ikutan: `countByStatus` 9 status di 2 halaman).
- Smoke sesi riil (register tenant → login cookie → fetch): `/penjualan/booking`,
  `/penjualan/komisi`, `/penjualan/pembatalan`, `/proyek/[id]`, `/penjualan/[unitId]`,
  `/laporan` semua 200 tanpa error marker; sidebar memuat 3 menu baru.
- **E2E kontrak API = payload UI persis**: create booking (fee `held`, kwitansi,
  unit `booked`) → board list → active-by-unit → timeline `booking_created` →
  cancel (`pending_refund`) → refund from-booking → pay (jurnal #763, status `paid`)
  → timeline `booking_cancelled`. Commission rule create (fraksi 0.025) + calculate
  + sync OK. Backend suite closing/cancellation/commission/project/sale hijau.

## 4. User flow yang kini hidup di UI

1. **Booking→Kontrak→DP**: Unit available → [Booking Unit] → unit `booked` + kartu
   booking → [Konversi ke Kontrak] (customer terkunci) → fee jadi Saldo Kredit
   Buyer → jadwal → apply-credit (existing FE-3).
2. **Reservasi manual** (non-booking): [Reservasi] → `reserved` → BAST lolos guard 6.1.
3. **Closing**: tab Penyelesaian → 5 langkah wizard → margin proyek jadi aktual.
4. **Pembatalan**: dari unit (pra/pasca-BAST) → tinjau Journal Preview → setujui →
   proses → refund dibayar dari tab Refund.
5. **Komisi bulanan**: Hitung → pilih semua → Setujui → Jadikan Terutang → Bayar
   (batch); pembatalan sale otomatis tersinkron (cancel/clawback).

## 5. Dashboard/Report impact
Belum ada perubahan dashboard di sprint ini (tile Titipan/Hutang Refund/Utang
Komisi + kartu Perhatian = backlog PS-2 bersama Dashboard Owner v2 — angka
sudah tersedia dari trial-balance existing).

## 6. Backlog follow-up

| # | Item |
|---|---|
| PS1-B1 | Endpoint list ber-denormalisasi (booking/cancellation/commission bawa kode unit+nama) → hapus `useUnitMap` N+1 |
| PS1-B2 | Dashboard Owner v2 (tile + kartu Perhatian) — PS-2 |
| PS1-B3 | `window.location.reload()` di 2 titik → `router.refresh()` granular |
| PS1-B4 | Halaman Pengaturan (payment scheme, tax rule, approval workflow, komisi rules pindah ke sana) |
| PS1-B5 | Approval workflow inbox UI (gate cancellation/commission tampil "menunggu workflow" saja) |
| PS1-B6 | FD-1 float money existing (JournalDetail dkk) — belum disentuh |
