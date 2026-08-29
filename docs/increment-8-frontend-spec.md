# Increment 8 — Cancellation & Refund: Frontend Specification

> Konsistensi: `product-ux-blueprint.md`. Persona: akuntan eks-Jurnal.id —
> wizard memandu, **Journal Preview** menunjukkan persis apa yang akan diposting
> SEBELUM eksekusi (tidak ada kejutan), setiap angka bisa diklik ke jurnal (U4).

---

## 1. Halaman / komponen

| # | Lokasi | Komponen |
|---|---|---|
| X1 | `/penjualan/pembatalan` | **Daftar Cancellation** (tab status) + **Daftar Refund** (tab kedua) |
| X2 | Dari detail unit / panel penjualan (`/penjualan/[unitId]`) | **Cancellation Wizard** (4 langkah) |
| X3 | Drawer/halaman `/penjualan/pembatalan/[id]` | **Detail Cancellation** + Journal Preview + Timeline |
| X4 | Dari X1 tab Refund / detail booking | **Refund Wizard** (2 langkah: buat → bayar) |
| X5 | Dashboard | Kartu Perhatian: *Menunggu approval pembatalan* · *Refund belum dibayar* |

## 2. Cancellation Wizard (X2) — wireframe

```
┌─ Pembatalan Unit A-01 ──────────────────────────────────────────────┐
│  ① Alasan & Penalti → ② Tinjau Dampak → ③ Persetujuan → ④ Proses   │
├─────────────────────────────────────────────────────────────────────┤
│ LANGKAH 2 — TINJAU DAMPAK (Journal Preview dari GET .../preview)    │
│                                                                     │
│  Tahap: ● Pasca-BAST (pendapatan & HPP akan dibalik)                │
│  Dana diterima buyer      Rp 500.000.000                            │
│  Penalti (hangus)         Rp 100.000.000                            │
│  Refund kepada buyer      Rp 400.000.000                            │
│  ─ Dampak Laba Rugi ─────────────────────────────────────────────   │
│  Pendapatan dibatalkan    −Rp 2.000.000.000                         │
│  HPP dibatalkan           +Rp 120.000.000 (Event 4 + true-up)       │
│  PPh Final dibatalkan     −Rp 50.000.000                            │
│  Penalti (Pendapatan Lain)+Rp 100.000.000                           │
│  ─ Dampak Unit ─────────────────────────────────────────────────    │
│  A-01: Terjual → Tersedia (kembali ke stok senilai biaya aktual)    │
│                                                                     │
│  ▸ Jurnal yang akan diposting (4)                    [expand ▾]     │
│    1. Pembalikan pengakuan pendapatan (Event 3)                     │
│       4-1000 Pendapatan Penjualan     D 2.000.000.000               │
│       2-2000 Uang Muka Penjualan              K 500.000.000         │
│       1-2000 Piutang Usaha                    K 1.500.000.000       │
│    2. Pembalikan HPP (Event 4 + porsi true-up) …                    │
│    3. Pembalikan akrual PPh Final …                                 │
│    4. Penyelesaian dana buyer (penalti + hutang refund) …           │
│                                                                     │
│  [← Ubah]                       [Ajukan Pembatalan →]               │
└─────────────────────────────────────────────────────────────────────┘
```

- **Langkah 1**: alasan (wajib), penalti (Rp, default 0), tanggal kejadian (default hari ini). Bantuan: "Penalti dipotong dari dana buyer dan diakui sebagai Pendapatan Lain-lain."
- **Langkah 2**: `GET /cancellations/{id}/preview` (atau pre-submit preview via request→preview) — blok di atas. Pra-BAST hanya menampilkan blok Dana + 1 jurnal settlement.
- **Langkah 3 — Persetujuan**: tombol [Setujui] (`POST .../approve`). Bila workflow approval aktif → tampilkan status request approval + tautan ke modul Persetujuan; 422 `ErrApprovalRequired` → banner "Menunggu persetujuan workflow" (bukan error merah).
- **Langkah 4 — Proses**: konfirmasi ireversibel → `POST .../process` → sukses: ringkasan + tautan tiap jurnal + "Refund Rp 400jt menunggu pembayaran → [Bayar Sekarang]".

## 3. Detail & Timeline (X3)

Timeline vertikal (audit — semua dari data dokumen + lifecycle log):
```
● Diajukan   10 Jul, Andi — "wanprestasi", penalti Rp 100jt
● Disetujui  11 Jul, Owner
● Diproses   11 Jul — 4 jurnal diposting [lihat]
  └ Unit A-01: Terjual → Tersedia (cancelled_post_bast)
● Refund     12 Jul — Rp 400jt via BCA [kwitansi]
```
Sumber: dokumen cancellation (aktor+waktu per transisi), `GET /units/{id}/transitions`, refund. Ditolak → node merah + alasan.

## 4. Refund Wizard (X4)

Tab Refund di X1: tabel {Penerima, Sumber (Pembatalan #N / Booking #N), Unit, Jumlah, Status, Aksi}. 
- **[Bayar]** (pending) → modal: `CashBankSelect` (existing) + tanggal → `POST /refunds/{id}/pay` → toast + saldo bank ter-update.
- **Dari booking**: di detail booking berdisposisi "Menunggu Refund" → tombol [Proses Refund] → `POST /refunds/from-booking/{id}` → masuk daftar pending.

## 5. State / badge (palet global)

| Objek | Badge |
|---|---|
| Cancellation `requested` | oranye "Menunggu Persetujuan" |
| `approved` | biru "Siap Diproses" |
| `processed` | hijau "Selesai" |
| `rejected` | abu "Ditolak" (+ alasan di tooltip) |
| Refund `pending` | oranye "Belum Dibayar" |
| `paid` | hijau "Dibayar" |
| Stage `pre_bast` | chip abu "Pra-BAST" · `post_bast` chip ungu "Pasca-BAST" |

**Empty state** (X1): *"Belum ada pembatalan. Pembatalan menangani buyer yang mundur — dana dikembalikan (dikurangi penalti) dan unit kembali ke stok, dengan seluruh jurnal dibuat otomatis."* 
**Loading**: skeleton; preview = skeleton blok dampak. 
**Error map**: 422 `ErrTaxAlreadyPaid` → banner khusus "PPh unit ini sudah disetor — tangani restitusi pajak manual dulu" (tanpa tombol proses); 422 penalti > diterima → inline di field penalti; 409 aktif-ganda → tautan ke dokumen aktif; 409 konflik → refresh state. 
**Success**: toast per langkah; setelah proses, panel unit menampilkan badge "Tersedia" kembali.

**Permission**: viewer read-only; accountant boleh request/process/pay; approve mengikuti gate (bila workflow aktif, approver dari modul Persetujuan). **Responsive**: daftar & timeline & approve nyaman di mobile (U7); Journal Preview collapse per jurnal, tabel scroll-x.

## 6. User Flow

1. **Pra-BAST umum**: Statement buyer → [Batalkan Pembelian] → wizard (penalti sesuai PPJB) → preview 1 jurnal → setujui → proses → bayar refund dari tab Refund. ≤ 6 klik.
2. **Pasca-BAST**: dari unit terjual → wizard mendeteksi otomatis "Pasca-BAST" + menampilkan dampak L/R lengkap → (workflow approval bila aktif) → proses → refund.
3. **Booking**: booking dibatalkan refundable (Increment 7) → badge "Menunggu Refund" → [Proses Refund] → [Bayar].

## 7. Dashboard & Report Impact — lihat implementation report §7–8
(Kartu perhatian ×2, tile Hutang Refund; L/R & Neraca & AR & pajak otomatis benar dari ledger yang sama.)

## 8. Endpoint

```
POST /api/v1/units/{unitID}/cancellations   {reason, penalty?, event_date?}
GET  /api/v1/cancellations?status=          → Cancellation[]
GET  /api/v1/cancellations/{id}             → Cancellation (jejak jurnal + refund_id)
GET  /api/v1/cancellations/{id}/preview     → ProcessPreview {stage, received_total,
       penalty, refund_amount, revenue_reversed, cogs_reversed, tax_reversed,
       unit_next_status, journals[{purpose,label,lines[{account_code,account_name,debit,credit}]}]}
POST /api/v1/cancellations/{id}/approve | /reject {reason} | /process
POST /api/v1/refunds/from-booking/{bookingID}
GET  /api/v1/refunds?status=pending|paid    · GET /refunds/{id}
POST /api/v1/refunds/{id}/pay               {bank_account_code, pay_date?}
```
Semua nominal string desimal dari backend — frontend tidak menghitung (U8).
