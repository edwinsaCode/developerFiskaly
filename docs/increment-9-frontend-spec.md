# Increment 9 — Commission: Frontend Specification

> Konsistensi: `product-ux-blueprint.md`. Dua persona: **finance** (memproses
> komisi 8 jam/hari — batch, bukan satu-satu) dan **owner/sales manager**
> (melihat performa & beban komisi). Prinsip U1: proses bulanan komisi
> ≤ 5 menit untuk 50 penjualan.

## 1. Halaman / komponen

| # | Route | Komponen |
|---|---|---|
| M1 | `/penjualan/komisi` | **Papan Komisi** — tab per status + aksi batch |
| M2 | `/pengaturan/komisi` | **Aturan Komisi** (list + form) — bagian Pengaturan (IA-4) |
| M3 | Drawer dari M1 | **Detail Komisi** (provenance rule + jejak jurnal + timeline) |
| M4 | Dashboard | Kartu Perhatian ×2 + tile Utang Komisi |
| M5 | (Product Sprint) | Sales Performance — memakai data ini |

## 2. Wireframe — Papan Komisi (M1)

```
┌─ Komisi Sales ───────────────────────────────────────────────────────────┐
│ [Hitung Komisi Baru]  [Sinkron Pembatalan]        Filter: [Sales ▾][Proyek ▾]
│ ⓘ 2 komisi baru dari 2 BAST — hasil perhitungan otomatis aturan aktif    │
├──────────────────────────────────────────────────────────────────────────┤
│ [Dihitung 2] [Disetujui 0] [Siap Bayar 1] [Dibayar 5] [Batal/Clawback 1] │
├──────────────────────────────────────────────────────────────────────────┤
│ ☑ Sales        Unit   Proyek   Dasar          Rate   Komisi       Aksi   │
│ ☑ Rina S.      CM-A   LITHOS   2.000.000.000  2,5%   50.000.000  [Detail]│
│ ☑ Rina S.      CM-B   LITHOS   2.000.000.000  2,5%   50.000.000  [Detail]│
│                                    Terpilih: 2 — Rp 100.000.000          │
│                                              [Setujui Terpilih →]        │
└──────────────────────────────────────────────────────────────────────────┘
```
Aksi per tab: Dihitung → [Setujui]; Disetujui → [Jadikan Terutang] (posting akrual,
tanggal akrual); Siap Bayar → [Bayar] (CashBankSelect + tanggal — bisa batch per
sales: satu pembayaran banyak komisi = loop API); Dibayar → read-only + [jurnal].
Batch = checkbox + aksi massal (loop endpoint per item; progress per baris).

**Aturan Komisi (M2):** tabel {Nama, Basis (2,5% / Flat), Berlaku, Scope
(Semua/Sales/Proyek/Tipe), Status aktif toggle} + form (basis radio → field rate
ATAU flat; scope opsional dropdown; akun default terisi; tanggal efektif).
Basis "Berjenjang (tiered)" tampil disabled "Segera hadir" (seam).

**Detail (M3):** provenance "Aturan: Komisi standar 2,5% (rate saat dihitung:
2,5%) × Dasar Rp 2 M = Rp 50jt" + timeline (dihitung/disetujui/terutang [jurnal]/
dibayar [jurnal]/clawback [jurnal] — aktor & waktu) + tautan unit/sale.

## 3. State / badge

`calculated` oranye "Dihitung" · `approved` biru "Disetujui" · `payable` ungu
"Siap Bayar" · `paid` hijau "Dibayar" · `cancelled` abu "Dibatalkan" ·
`clawed_back` merah "Clawback" (tooltip: sale dibatalkan; piutang ke sales).

**Empty state** (M1): *"Belum ada komisi. Buat Aturan Komisi lalu klik Hitung —
sistem memindai semua penjualan (BAST) ber-sales dan menghitung otomatis."*
[+ Buat Aturan]. (M2): penjelasan basis + contoh.
**Loading**: skeleton tabel. **Error**: 422 approval-required → banner "menunggu
workflow"; 422 seam (tiered/trigger) → toast informatif; 409 → refresh baris.
**Success**: toast + baris pindah tab; counter "Hitung: N komisi baru".

**Permission**: viewer read-only; accountant semua aksi; approve mengikuti gate
workflow bila aktif. **Mobile**: tab + kartu ringkas per komisi (sales, unit,
nominal, aksi utama); approve/bayar dari ponsel (U7).

## 4. User Flow

1. **Bulanan (finance)**: buka Papan → [Hitung] → tab Dihitung → pilih semua →
   Setujui → Jadikan Terutang (akrual tanggal akhir bulan) → [Bayar] per sales →
   selesai. Beban komisi otomatis di L/R proyek.
2. **Sale batal**: proses pembatalan (Increment 8) → Papan Komisi menampilkan
   banner "Ada penjualan dibatalkan — [Sinkron Pembatalan]" → klik → komisi
   terkait auto-cancel/clawback; clawback memunculkan piutang ke sales (1-2100).
3. **Owner**: dashboard tile Utang Komisi + (Product Sprint) ranking sales.

## 5. Dashboard & Report Impact — lihat implementation report §6–7.

## 6. Endpoint

```
POST /api/v1/commission-rules   {name, basis, rate?|flat_amount?, trigger_event?,
     sales_person_id?, project_id?, unit_type?, effective_from, effective_to?}
GET  /api/v1/commission-rules · GET /{id} · PUT /{id}/active {active}
POST /api/v1/commissions/calculate            → {created}
POST /api/v1/commissions/sync-cancellations   → {cancelled, clawed_back}
GET  /api/v1/commissions?status=&sales_person_id=
GET  /api/v1/commissions/{id}
POST /api/v1/commissions/{id}/approve
POST /api/v1/commissions/{id}/make-payable {accrual_date?}
POST /api/v1/commissions/{id}/pay          {bank_account_code, pay_date?}
POST /api/v1/commissions/{id}/cancel       {reason}
```
Semua nominal string desimal — frontend tidak menghitung (U8).
