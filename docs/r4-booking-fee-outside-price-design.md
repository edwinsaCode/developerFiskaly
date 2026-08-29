# Design Review — R4: Booking Fee di Luar Harga Unit

> ## ⛔ OBSOLETE (2026-07-29) — digantikan business rule final KLIEN
> Booking fee = **PENDAPATAN BOOKING** saat diterima (Dr Kas/Bank / Cr 4-2100),
> BUKAN liability + disposisi (Opsi A dokumen ini). Tidak ada refund/reversal.
> Lihat `docs/booking-revenue-audit-plan-2026-07.md` (audit + rencana + status).
> Yang MASIH berlaku dari dokumen ini: kolom `counts_toward_price` (migration
> 000052) sebagai SoT "tidak mengurangi harga", dan jalur legacy untuk baris
> histori (reklas pra-R4 / disposisi held era-R4).
>
> 2026-07-26 · (arsip) MENUNGGU APPROVAL — tidak ada kode diubah.
> Digerbang sejak awal karena menyentuh posting jurnal (product-architecture-review-2026-07.md §4).
> Semua temuan diverifikasi terhadap kode aktual (path + baris disebut per klaim).

## 0. Kebijakan bisnis baru (dari UAT)

1. Booking Fee **di luar** harga unit — tidak pernah mengurangi outstanding harga rumah.
2. Booking Fee = **transaksi terpisah** (liability sendiri, bukan bagian DP/uang muka).
3. Booking tidak pernah mengurangi harga rumah.
4. Histori data lama tetap kompatibel (append-only, jurnal lama tak disentuh).
5. Tidak ada duplicate source of truth.

---

## 1. Kondisi kode SEKARANG (grounded)

### 1.1 Titik akar masalah
`SumTerminsByUnit` (`internal/sale/repository.go:138`) menjumlahkan **SEMUA** `termin_payments`
milik unit — **termasuk termin ber-`payment_source='booking_fee'`**:

```sql
SELECT COALESCE(SUM(amount),0) FROM termin_payments WHERE tenant_id=? AND unit_id=?
```

`Outstanding = GrossAmount − SumTerminsByUnit` → **booking fee ikut mengurangi outstanding.**

### 1.2 Lifecycle booking fee (jurnal)
- Terima fee (`booking_service.go:112`, `booking_repo.go:86`): **Dr Bank / Cr 2-2100 Titipan
  Booking** (kewajiban) + termin `source=booking_fee` + kwitansi. `booking.TerminPaymentID`
  menunjuk termin ini.
- **Konversi ke kontrak** (`booking_repo.go:168-195`) — INI sumber "fee mengurangi harga":
  1. Jurnal reklas **Dr 2-2100 / Cr 2-2000 Uang Muka** (fee pindah jadi uang muka penjualan).
  2. Insert **buyer_credit** sub-ledger (fee jadi saldo kredit buyer → diaplikasikan ke DP).
  → Termin `booking_fee` tetap ada di unit, kini "didukung" saldo 2-2000; ikut `SumTerminsByUnit`.
  → Terverifikasi: kontrak 250jt + fee 5jt → outstanding 245jt.
- Hangus (`booking_repo.go:281`): Dr 2-2100 / Cr 4-2000 Pendapatan Lain-lain.
- Refund (`refund_service.go:70-81`): Dr 2-2100 / Cr 2-2200 Hutang Refund → bayar Dr 2-2200 / Cr Bank.

### 1.3 Inkonsistensi yang SUDAH ada di kode
`reporting/sales_performance.go:86` menghitung "collected" **DENGAN mengecualikan booking_fee**
(`WHERE payment_source <> 'booking_fee'`), tapi `financial_summary.go`, `statement.go`,
`collection.go`, `dashboard.go` **memasukkannya**. Jadi definisi "sudah dibayar" sudah
bercabang hari ini — perbaikan ini justru menyatukannya.

### 1.4 Semua titik yang menghitung outstanding/collected (peta SoT)
| # | Lokasi | Rumus | Basis |
|---|---|---|---|
| 1 | `financial_summary.go:81` | Gross − paid | `SumTerminsByUnit` (KANONIK) |
| 2 | `collection.go:111,116` | Gross − collected | `SumTerminsByUnit` |
| 3 | `statement.go:114,217` | Gross − collected | `SumTerminsByUnit` |
| 4 | `scheme_flow.go:415,419` | saleGross − collected (pasca-BAST) | `SumTerminsByUnit` |
| 5 | `service.go:277-281,399` | pasca-BAST + total advance @BAST | `SumTerminsByUnit` |
| 6 | `dashboard.go:511` receivables | `gross_amount − SUM(tp.amount)` | **SQL mentah** |
| 7 | `dashboard.go` collected/KPI | Σ termin | **SQL mentah** |

**Kabar baik**: #1–#5 semuanya bermuara ke satu fungsi `SumTerminsByUnit`. Mengubah satu fungsi
itu memperbaiki lima call-site sekaligus. Hanya SQL mentah dashboard (#6, #7) yang perlu filter
tambahan terpisah.

---

## 2. Perubahan MINIMAL — satu SoT: flag `counts_toward_price`

**Keputusan inti:** tambah kolom **`termin_payments.counts_toward_price BOOL NOT NULL DEFAULT TRUE`**
sebagai satu-satunya penanda "apakah pembayaran ini mengurangi harga rumah". Ini persis rancangan §4.

Kenapa ini memenuhi SEMUA syarat:

- **Outstanding hanya dari harga kontrak** → `SumTerminsByUnit` diubah menjadi
  `SUM(amount) WHERE counts_toward_price = TRUE`. Satu baris, memperbaiki 5 call-site.
- **Histori lama kompatibel** → `DEFAULT TRUE` membuat SELURUH baris lama = TRUE → nilai lama
  100% identik. Jurnal lama tak disentuh (append-only). Kontrak lama hasil konversi (fee sudah
  di 2-2000, flag TRUE) tetap menghitung fee sebagaimana adanya.
- **Booking fee transaksi terpisah** → termin `booking_fee` BARU di-insert `counts_toward_price=FALSE`;
  fee tetap sebagai liability 2-2100, tidak pernah masuk 2-2000.
- **Booking tak pernah mengurangi harga** → konversi **branch pada flag**: jika fee termin
  `FALSE` (booking baru) → **lewati reklas + lewati buyer_credit**; jika `TRUE` (booking lama
  in-flight) → jalur lama (reklas) — jadi booking yang terlanjur berjalan tetap konsisten.
- **Tidak ada duplicate SoT** → flag adalah atribut pembayaran; Outstanding tetap SATU rumus
  turunan yang membacanya. Cancellation membaca saldo ledger nyata (2-2000/2-2100), bukan flag,
  sehingga otomatis konsisten (lihat §3).

**Flag drive DUA hal sekaligus** (elegan, satu SoT): (a) matematika outstanding, (b) cabang jurnal
konversi. Tidak perlu enum `bookings.fee_policy` terpisah — status "outside price" cukup diturunkan
dari flag pada fee termin (`booking.TerminPaymentID`). Menambah enum kedua = risiko drift (ditolak).

---

## 3. Dampak per flow

| Flow | Dampak | Perubahan |
|---|---|---|
| **Booking** (terima fee) | Termin fee di-insert `counts_toward_price=FALSE`. Jurnal Dr Bank/Cr 2-2100 **tak berubah**. | `booking_service.go` / `booking_repo.go` set flag FALSE |
| **Contract** (konversi) | Booking baru: **TIDAK** reklas ke 2-2000, **TIDAK** buyer_credit. Fee tetap di 2-2100. Outstanding = gross penuh. Booking lama (flag TRUE): jalur lama tetap jalan. | `ConvertWithContractAtomic` branch pada flag fee |
| **Payment** (DP/cicilan) | Tak berubah — default TRUE, tetap mengurangi outstanding. | — |
| **KPR Disbursement** | Tak berubah (source `kpr_disbursement`, TRUE). Outstanding & shortfall otomatis benar via rumus tunggal. | — |
| **Invoice** (KEKURANGAN) | Nominal dari `ContractFinancialSummary.Outstanding` → **otomatis benar** begitu rumus difilter (manfaat satu rumus). | — (ikut otomatis) |
| **Customer Statement** | `total_paid`/`remaining_balance` otomatis benar (via `SumTerminsByUnit`). Fee tetap tampil di **Riwayat Pembayaran** sebagai baris "Booking Fee" berlabel *di luar harga* (transparansi), TIDAK masuk Total Diterima harga. | `statement.go`: tandai baris booking_fee `counts_toward_price=false` di payload; UI kelompokkan terpisah |
| **Receipt** (kwitansi) | Kwitansi fee tetap terbit saat terima fee (bukti sah penerimaan). Relabel "Kwitansi Booking Fee (di luar harga unit)". | Label saja |
| **Dashboard** | `outstanding_total` (receivables) & `collected`/KPI: tambah `AND counts_toward_price=TRUE` di SQL mentah. `booking_fee_held` (2-2100) tetap benar (dan kini bertahan lebih lama). | `dashboard.go` 2-3 query |
| **Collection** | Guard overpayment pakai `outstandingForContract` → otomatis benar. Fee tak lagi bikin "overpay" palsu. | — (ikut otomatis) |
| **Ledger posting** | Booking baru: 1 jurnal saja (terima fee, Cr 2-2100). Konversi tak menambah jurnal reklas. Saldo 2-2100 = kewajiban fee riil sampai disposisi. | Lebih sedikit jurnal, bukan lebih banyak |
| **Cancellation** | Pra-BAST: fee tetap di 2-2100 → refund/forfeit lewat jalur existing. Pasca-BAST: reversal membaca **saldo 2-2000 nyata** (`service.go:97 receivedAdvance`) — fee tak di 2-2000 lagi → otomatis TIDAK ikut dibalik (benar). | — (self-consistent via ledger) |
| **Refund** | Dr 2-2100/Cr 2-2200 → Dr 2-2200/Cr Bank. Sudah beroperasi tepat di 2-2100 tempat fee kini menetap → **lebih bersih**, tak perlu ubah. | — |

---

## 4. Dampak jurnal (ringkas)

**Terima booking fee** (tak berubah):
```
Dr Bank .................. X
   Cr 2-2100 Titipan Booking ... X     (KEWAJIBAN)
```
**Konversi — kebijakan LAMA (flag TRUE, data in-flight/histori):**
```
Dr 2-2100 ............. fee
   Cr 2-2000 Uang Muka ...... fee      (fee → bagian pembayaran)  [JALUR LAMA, tetap ada]
```
**Konversi — kebijakan BARU (flag FALSE):**
```
(tidak ada jurnal konversi — fee tetap di 2-2100)
Outstanding kontrak = GrossAmount penuh
```
**Disposisi fee pasca-akad (BARU, keputusan PO — lihat §8):**
- Jadi pendapatan administrasi: `Dr 2-2100 / Cr 4-2100 Pendapatan Administrasi`
  (akun via COA master, bukan hardcode). `TODO(tax-advisor): PPN atas fee administrasi.`
- Hangus: `Dr 2-2100 / Cr 4-2000` (jalur existing). Refund: jalur existing.

**Invariant baru**: kontrak `outside_price` → fee **tidak pernah** menyentuh 2-2000 atau 4-1000
harga; GrossAmount penuh ditagihkan.

---

## 5. Database & migration strategy

**Migration additive (satu file, mis. `000052_booking_fee_outside_price`):**
```sql
ALTER TABLE termin_payments
  ADD COLUMN counts_toward_price BOOLEAN NOT NULL DEFAULT TRUE;
-- opsional indeks bila query filter jadi panas:
-- CREATE INDEX idx_tp_counts ON termin_payments(tenant_id, unit_id, counts_toward_price);
```

- **Backfill: NIHIL.** `DEFAULT TRUE` sudah membuat semua baris lama = TRUE (perilaku identik).
- **Tidak ada** rewrite jurnal, tidak ada perubahan histori (append-only terjaga).
- **Down migration**: `DROP COLUMN counts_toward_price` — aman (kolom additive; rumus fallback
  ke "semua termin", perilaku pra-R4).
- Tidak ada tabel/saldo baru → tidak ada duplicate SoT.

**Catatan cutover in-flight**: booking yang dibuat SEBELUM deploy (fee termin flag TRUE) akan
konversi via jalur lama (reklas) — konsisten & benar. Booking SETELAH deploy (FALSE) via jalur
baru. Tidak ada booking yang "setengah jalan rusak".

---

## 6. Dampak API

Semua **additive / tanpa breaking**:
- `GET /sale-contracts/{id}/financial-summary` — nilai `outstanding`/`total_paid` berubah untuk
  kontrak BARU outside_price (memang tujuannya); field tak berubah.
- `GET /sale-contracts/{id}/statement` — `payments[]` tiap baris dapat field baru
  `counts_toward_price: bool` (additive) agar UI bisa memisah "Booking Fee (di luar harga)".
- `GET /reports/dashboard` — angka `receivables.outstanding_total` konsisten; bentuk payload tetap.
- (Opsional, disposisi fee) endpoint baru additive, mis.
  `POST /bookings/{id}/fee-disposition` `{action: recognize_admin|forfeit|refund}` — hanya bila
  §8 di-approve.
- Tidak ada endpoint dihapus/diubah kontraknya.

---

## 7. Dampak UI

- **UnitSalePanel / kartu kontrak**: "Sisa Tagihan" kini = harga rumah penuh untuk kontrak baru.
  Tambah baris kecil "Booking fee Rp X — di luar harga (kewajiban terpisah)".
- **Customer Statement 360**: baris booking_fee pindah ke sub-bagian "Di luar harga unit" atau
  diberi badge; TIDAK dijumlahkan ke "Total Diterima" harga. (`counts_toward_price=false` dari payload.)
- **DisbursementModal / preview**: pratinjau outstanding sudah dari summary → otomatis benar.
- **Booking board**: label disposisi fee (held/di luar harga) — kosmetik.
- **Kwitansi**: judul "Kwitansi Booking Fee (di luar harga unit)".
- Tidak ada layar baru wajib (kecuali §8 disposisi bila di-approve).

---

## 8. Keputusan terbuka untuk Product Owner (disposisi fee pasca-konversi)

Setelah booking outside_price dikonversi, fee **menetap di 2-2100** sampai ada disposisi. Pilihan:

| Opsi | Perlakuan | Rekomendasi |
|---|---|---|
| **A. Biarkan liability** sampai aksi manual (recognize/forfeit/refund) | fee tetap 2-2100 | **Default aman** — minimal, tak menyentuh pengakuan pendapatan otomatis |
| **B. Auto-akui admin income saat akad** | Dr 2-2100 / Cr 4-2100 saat konversi | butuh kepastian pajak (PPN admin) → `TODO(tax-advisor)` |
| **C. Kebijakan tenant** (`auto_recognize_booking_fee`) | pilih A/B per tenant | fleksibel, sedikit lebih kompleks |

Core R4 (poin 1-5 kebijakan) **tidak butuh** disposisi otomatis. Rekomendasi: **kirim Core R4
dengan Opsi A**; jadikan B/C fast-follow terpisah agar keputusan PPN tak memblok.

---

## 9. Risiko kompatibilitas & mitigasi

1. **Perubahan makna "sudah dibayar"** untuk kontrak baru — DISENGAJA. Kontrak lama identik
   (flag TRUE). Mitigasi: uji regresi summary kontrak lama = nilai sebelum migrasi.
2. **SQL mentah dashboard terlewat** — jika hanya `SumTerminsByUnit` difilter tapi dashboard
   `#6/#7` lupa, dashboard vs statement bisa beda. Mitigasi: checklist "semua titik SoT di §1.4"
   + integration test outstanding dashboard == summary.
3. **Booking in-flight saat deploy** — ditangani flag per-termin (TRUE lama → jalur lama). Tak ada
   state rusak. Mitigasi: test konversi booking pra-migrasi (TRUE) & pasca-migrasi (FALSE).
4. **`total_advance_at_bast`** (`service.go:399`) ikut memakai `SumTerminsByUnit` → fee tak lagi
   dihitung sebagai advance saat BAST. Ini BENAR (fee di luar harga) tapi wajib disadari &
   di-test (pengakuan pendapatan/HPP pakai advance harga saja).
5. **Buyer_credit lama** dari booking yang sudah dikonversi tetap ada & valid (tak diubah).
6. Semua lainnya additive & backward compatible; tidak ada perubahan ledger/histori.

---

## 10. Rencana test (sebelum merge)

- Unit: `SumTerminsByUnit` mengecualikan `counts_toward_price=FALSE`; menghitung TRUE.
- Integration: booking baru → fee FALSE → konversi TANPA jurnal reklas → outstanding = gross penuh;
  balanced.
- Regresi histori: kontrak lama (semua TRUE) → summary == snapshot nilai pra-migrasi.
- Integration: dashboard `outstanding_total` == Σ `ContractFinancialSummary.Outstanding` (satu rumus).
- Cancellation pasca-BAST kontrak outside_price: reversal 2-2000 TIDAK menyertakan fee (fee di 2-2100).
- Refund booking fee outside_price: Dr 2-2100/Cr 2-2200 → Cr Bank; 2-2100 nol setelah bayar.
- Cutover: konversi booking pra-migrasi (TRUE) tetap jalur lama.

## 11. Ringkas perubahan (bila di-approve)

| Layer | Perubahan | Besar |
|---|---|---|
| DB | +1 kolom `counts_toward_price` (default TRUE), migration additive | XS |
| Backend | `SumTerminsByUnit` +filter; booking insert flag FALSE; konversi branch; 2-3 SQL dashboard +filter; statement payload +field | S |
| API | additive field statement; (opsional) endpoint disposisi | XS |
| UI | pisah tampilan fee di statement/kartu; label kwitansi | XS-S |
| Test | unit + integration + regresi histori | S |

**Total: perubahan KECIL & terisolasi**, karena semua outstanding bermuara ke satu fungsi dan
flag default TRUE menjaga histori. Tidak ada rewrite jurnal, tidak ada duplicate SoT.
