# W-11 — FINAL DECISION LOCK

**Status:** PRE-IMPLEMENTATION CONTRACT. Belum ada kode, migrasi, seed, tabel, API, atau frontend.
**Tanggal:** 2026-08-14
**Dasar:** `docs/w11-hutang-usaha-design.md` Revisi 2 (di-approve pemilik sebagai DESIGN REVIEW).
**Sifat dokumen:** apa yang tertulis di sini **mengikat implementasi**. Perubahan atasnya = perubahan kontrak, bukan detail teknis.

> ✅ **SIAP CODING.** Empat keputusan terbuka (O-1..O-4) dan R-1 sudah dijawab pemilik 2026-08-14 dan dikunci sebagai **D-16 … D-20**. Tidak ada keputusan akuntansi yang tersisa.

---

# A. FINAL DECISION MATRIX

| # | Keputusan | Status | Isi yang dikunci | Konsekuensi implementasi |
|---|---|---|---|---|
| **D-1** | **BD-1 — titik pengakuan hutang** | 🔒 **LOCKED** | Siklus existing: `Create` (draft) → Approval → `Post`. **Tidak** membuat workflow approval/posting baru | Pakai `PostingService.Create` + `approval.RequireApproved` + `PostingService.Post`. Nol mesin baru |
| **D-2** | **Draft belum kewajiban** | 🔒 **LOCKED** | Draft: nol GL, nol RAB, nol AP outstanding, nol aging | Dijamin `posted_at IS NULL` + semua reader posted-only. Diuji INV-AP-9 |
| **D-3** | **Setelah approved+posted** | 🔒 **LOCKED** | Kewajiban lahir; **seluruh reader existing melihatnya konsisten** pada peristiwa yang sama | Satu perubahan (`posted_at` terisi) menyalakan 6 pembacaan serentak. Diuji INV-AP-9 |
| **D-4** | **T-2 — sumber biaya** | 🔒 **HARD CONSTRAINT** | Biaya AP/proyek **wajib** ada di `cost_entries`. **Dilarang** tabel baris AP yang jadi sumber biaya/RAB tersendiri | `ap_invoice_lines` **TIDAK DIBUAT**. Baris biaya = baris `cost_entries` ber-`ap_invoice_id`. Diuji INV-AP-8 & INV-AP-11 |
| **D-5** | **BD-2 / W-10 — reader** | 🔒 **LOCKED** | `GetRealisasiByProject` dan `GetRealisasiPerItem` **tidak diubah sama sekali** | W-11 tidak menyentuh `internal/budget` maupun `internal/ledger/actual_cost.go`. Pengakuan masuk reader lewat mekanisme existing (debit taksonomi + `cost_entries.amount`) |
| **D-6** | **BD-3(b) — model retensi** | 🔒 **LOCKED** | **Akun kewajiban terpisah**, bukan atribut tersembunyi di 2-1000 | Butuh 1 akun COA baru (B-1). Aging membedakan keduanya lewat akun, bukan lewat flag |
| **D-7** | **BD-5 — vendor** | 🔒 **LOCKED (Revisi 2)** | Vendor Master minimal ber-tenant; `cost_entries.vendor VARCHAR(200)` tetap untuk kompatibilitas historis. **Tidak ada vendor master tambahan** | Satu tabel `vendors`. `charge_payouts.vendor` **tidak** disentuh W-11 |
| **D-8** | **Uang muka vendor dalam scope** | 🔒 **LOCKED (turunan)** | Pemilik mensyaratkan skenario **F** dan **G** dalam Journal Matrix ⇒ uang muka vendor masuk **P0** | Naik dari P1 (Revisi 2) ke P0. Butuh 1 akun COA baru (B-2) |
| **D-9** | **Guard periode saat post** | 🔒 **LOCKED** | Approve→post **wajib** memeriksa periode dengan `IsPeriodClosed` yang sudah ada | Menutup gap U-2. Bukan guard kedua — memanggil satu-satunya implementasi |
| **D-10** | **Reversal** | 🔒 **LOCKED** | Hanya lewat `PostingService.Reverse`; append-only; **tidak ada `DELETE`/`UPDATE`** atas jurnal terposting | JR terbit otomatis. Diuji INV-AP-13 |
| **D-11** | **Dokumen pembayaran** | 🔒 **LOCKED** | Satu pergerakan kas = satu **BKK**. Tanpa jenis dokumen baru, tanpa mekanisme nomor baru | `DefaultCashSpec` sudah memilih BKK. Diuji INV-AP-5 |
| **D-12** | **Status & outstanding** | 🔒 **LOCKED** | **Turunan**, bukan kolom tersimpan | Tidak ada kolom `status`/`outstanding` di skema |
| **D-13** | **Aging** | 🔒 **LOCKED** | Pakai ulang **aturan bucket** `internal/receivable`; laporan AP punya struct sendiri | `bucketFor` diekspor. `receivable.Row` **tidak** dipakai (bentuknya customer-shaped) |
| **D-14** | **Payable warisan** | 🔒 **LOCKED (opsi A)** | Dibiarkan sebagai lapisan `system` di rekonsiliasi, terlihat & terberi label. Tanpa migrasi, tanpa hapus | INV-AP-1 dirumuskan per-source, bukan kesamaan mutlak |
| **D-15** | **Guard Tahap 1** | 🔒 **LOCKED** | Guard `payable` di `internal/cost/handler.go` **tetap dipertahankan** setelah W-11 hidup | Pintu AP satu-satunya: `POST /ap/invoices` |
| **D-16** | **BD-7 — PPN Masukan** | 🔒 **LOCKED** (2026-08-14) | **PPN Masukan MASUK P0.** PPh dipotong **DITUNDA**. Jurnal pengakuan bertambah `Dr 1-5100 PPN Masukan`; kewajiban ke vendor **bruto termasuk PPN**; `cost_entries.amount` **tetap DPP tanpa PPN** | Akun 1-5100 sudah ada ⇒ **akun COA baru tetap 2**. `ppn_amount` & `faktur_pajak_number` aktif. Lihat C.PPN & INV-AP-17 |
| **D-17** | **BD-6 — withholding PPh** | 🔒 **LOCKED — DITUNDA** | Tidak dicatat di P0. Hanya **seam kosong**, tanpa akun, tanpa kolom | Akun **2-1200 TIDAK DIBUAT** (B.3 jadi arsip). Jurnal pembayaran tetap 2 baris. `TODO(tax-advisor)` tetap berdiri |
| **D-18** | **BD-12 — lebih bayar vendor** | 🔒 **LOCKED** | **Tolak secara default**, tetapi sediakan aksi eksplisit "catat kelebihan sebagai uang muka" dalam satu langkah | Aset (1-5300) hanya lahir dari keputusan sadar admin, tidak pernah otomatis. Lihat E.2 & INV-AP-18 |
| **D-19** | **BD-14 — gate approval** | 🔒 **LOCKED** | **OPT-IN**, mengikuti pola existing. Tenant tanpa workflow aktif boleh post langsung | `RequireApproved` dipakai apa adanya. Nol mesin baru, nol penyimpangan pola repo |
| **D-20** | **R-1 — asimetri reversal** | 🔒 **LOCKED — DIKELUARKAN** | Diangkat jadi pekerjaan terpisah **W-11b**, termasuk perbaikan reader-nya | **D-5 tetap penuh berlaku di W-11.** Kolom `cost_entries.reversed_at` **TIDAK DIBUAT** di W-11. Lihat H.3 |

**Keputusan non-akuntansi yang di-default** (bukan blocker; koreksi kapan saja tanpa mengubah skema):

| BD | Default yang dipakai | Alasan |
|---|---|---|
| BD-2 | `budget_item_id` **opsional**, didorong kuat di UI | Mewajibkan akan memblokir tagihan lintas-item; kosong ⇒ hilang dari drill-down item (ditampilkan sebagai peringatan di UI) |
| BD-3(c) | Persentase retensi **per tagihan**, default dari vendor bila diisi | Praktik berbeda antar kontrak |
| BD-3(d) | Pelepasan retensi **manual + tercatat** (bukan otomatis by date) | Pelepasan bergantung pemeriksaan cacat, bukan kalender |
| BD-8 | Nomor invoice vendor ganda → **peringatan lunak**, bukan tolak keras | Sebagian vendor mengulang nomor lintas tahun |
| BD-9 | Approval pembayaran = `RequireApproved(TargetPayment)`, OPT-IN | Nol mesin baru |
| BD-11 | Pembayaran sebagian **bebas** | Realitas lapangan |
| BD-13 | Kontrak = teks bebas (`contract_ref`) + `progress_percent`. Entitas kontrak **P1** | Kontrol pengadaan, bukan akuntansi |

---

# B. FINAL COA MATRIX

## B.0 Akun existing yang dipakai apa adanya (tanpa perubahan)

| Kebutuhan | Kode | Nama | Role existing |
|---|---|---|---|
| Hutang usaha vendor | **2-1000** | Hutang Usaha | `RolePayable` |
| Kas/bank pembayaran | **1-1100 … 1-1500** | Kas & Bank | dipilih lewat `accounts.category` (cash/bank), endpoint `GET /ledger/accounts/cash-bank` |
| Debit biaya proyek | **1-3000 / 1-3100 / 1-3200 / 1-3300** | Persediaan Real Estat | lewat `Category.InventoryAccountCode()` |
| Debit beban overhead | **5-3000 / 5-4000 dst** | Beban | lewat `Category.ExpenseAccountCode()` |
| PPN Masukan *(bila O-1 = ya)* | **1-5100** | PPN Masukan | `RoleVATInput` |

## B.1 — Akun baru #1 (WAJIB, P0)

| Atribut | Nilai |
|---|---|
| **Code** | `2-1100` |
| **Name** | `Hutang Retensi Kontraktor` |
| **Role** | `RoleRetentionPayable` (baru di `AccountRoleRegistry`) |
| **Account category** | `domain.AccountLiability` |
| **Normal balance** | `domain.NormalBalanceCredit` |
| **System account** | `true` (tidak boleh dihapus tenant) |

**Alasan akun existing tidak cocok:**

| Kandidat | Kenapa ditolak |
|---|---|
| **2-1000 Hutang Usaha** | Ditolak oleh D-6 secara eksplisit. Secara akuntansi: retensi punya **jatuh tempo dan syarat pelepasan yang berbeda**. Menggabungkannya membuat laporan aging melaporkan kewajiban yang **sah ditahan** sebagai terlambat bayar, dan vendor tertagih atas sesuatu yang belum boleh dibayar |
| **2-6000 Biaya Akrual** | **Populasi berlawanan.** 2-6000 untuk beban yang sudah terjadi **tetapi belum ditagih**. Retensi kebalikannya: sudah ditagih, biaya sudah diakui, hanya **pembayaran** yang ditahan. Mencampur merusak analisis cut-off periode |
| **2-2400 Titipan Realisasi** | **Arah ekonomi terbalik.** 2-2400 adalah uang **pembeli** yang kita pegang sebagai titipan. Retensi adalah uang **kita sendiri** yang belum kita bayarkan. Mencampur melanggar aturan charge group K-1..K-5 |
| **2-2300 Titipan Notaris** | Sama — titipan pihak ketiga, bukan kewajiban dagang kita |
| **2-5000 Hutang Bank** | Kewajiban pembiayaan, bukan kewajiban dagang |
| **2-6200 Utang Komisi** | Kewajiban kepada tenaga penjual, populasi berbeda |

## B.2 — Akun baru #2 (WAJIB, P0 — turunan D-8)

| Atribut | Nilai |
|---|---|
| **Code** | `1-5300` |
| **Name** | `Uang Muka Vendor` |
| **Role** | `RoleVendorAdvance` (baru di `AccountRoleRegistry`) |
| **Account category** | `domain.AccountAsset` |
| **Normal balance** | `domain.NormalBalanceDebit` |
| **System account** | `true` |

**Alasan akun existing tidak cocok:**

| Kandidat | Kenapa ditolak |
|---|---|
| **1-5000 Biaya Dibayar di Muka** | **Sifat ekonomi berbeda.** *Prepaid expense* adalah pembayaran di muka atas **beban** yang manfaatnya terbagi antar periode (sewa, asuransi) dan diamortisasi ke laba rugi. Uang muka konstruksi tidak pernah jadi beban periodik — ia menjadi **persediaan/WIP** lalu HPP saat unit terjual. Mencampurnya salah klasifikasi di neraca dan salah pola pelepasannya |
| **1-2100 Piutang Lain-lain** | **Bukan piutang.** Klaim kita atas vendor adalah klaim atas **barang/jasa**, bukan atas uang kembali. Menyebutnya piutang melebih-lebihkan aset lancar yang dapat ditagih tunai |
| **1-3000 / 1-3100 Persediaan RE** | **Fatal.** Akun ini ada dalam daftar taksonomi yang dibaca `ActualCostByCode`, sehingga uang muka akan **langsung masuk realisasi RAB sebelum ada pekerjaan**, lalu dihitung lagi saat termin diakui — total 120 juta untuk pekerjaan 100 juta (skenario U.4) |
| **1-5100 PPN Masukan** | Pajak, bukan uang muka pembelian |

## B.3 — Akun baru #3 — ❌ **TIDAK DIBUAT** (D-17: withholding ditunda)

> Bagian ini disimpan sebagai **arsip analisis** untuk saat withholding diaktifkan nanti. **Tidak ada akun yang dibuat di W-11.**

| Atribut | Nilai |
|---|---|
| **Code** | `2-1200` |
| **Name** | `Hutang PPh Dipotong` |
| **Role** | `RoleWithholdingPayable` |
| **Account category** | `domain.AccountLiability` |
| **Normal balance** | `domain.NormalBalanceCredit` |

**Alasan akun existing tidak cocok:**

| Kandidat | Kenapa ditolak |
|---|---|
| **2-4000 Hutang PPh Final Pengalihan** | **Beda subjek pajak.** 2-4000 adalah PPh final atas pengalihan hak atas tanah/bangunan — kewajiban kita **sebagai penjual atas penghasilan kita sendiri**. PPh 23/4(2) atas jasa kontraktor adalah kewajiban kita **sebagai pemotong atas penghasilan pihak lain**: beda SPT masa, beda bukti potong, beda pelaporan. Mencampur membuat SPT salah |
| **2-3000 PPN Keluaran** | PPN ≠ PPh |
| **2-1000 Hutang Usaha** | Kewajiban kepada negara, bukan kepada vendor |

`// TODO(tax-advisor): tarif dan kualifikasi PPh 23 vs PPh 4(2) untuk termin kontraktor tidak ditebak. Wajib dikonfirmasi sebelum implementasi.`

## B.4 Ringkasan

| | Jumlah |
|---|---|
| Kebutuhan tercukupi akun existing | **6** — termasuk 1-5100 PPN Masukan (D-16) |
| Akun baru wajib (P0) | **2** — 2-1100, 1-5300 |
| Akun baru kondisional | **0** — 2-1200 dibatalkan oleh D-17 |
| Jenis dokumen baru | **0** — BKK & JR sudah ada |
| Role registry baru | **2** (atau 3) |

**Tidak ada akun dibuat, di-seed, atau dimigrasikan pada tahap ini.**

## B.5 Nilai `journal_entries.source` (bukan akun, tapi mengikat)

**[FAKTA]** `source varchar(20) NOT NULL DEFAULT 'system'`, terindeks, **tanpa CHECK** ⇒ nilai baru tidak butuh migrasi. **Batas 20 karakter mengikat penamaan.**

| Source | Panjang | Dipakai untuk |
|---|---:|---|
| `ap_invoice` | 10 | Pengakuan kewajiban |
| `ap_payment` | 10 | Pembayaran kewajiban |
| `ap_advance` | 10 | Pembayaran uang muka vendor |
| `ap_retention` | 12 | Pelepasan retensi |
| `reversal` | 8 | Existing — dipakai apa adanya |

> ⚠️ `ap_retention_release` = **tepat 20 karakter** — mepet batas kolom. Dipakai `ap_retention` untuk menghindari pemotongan senyap.

---

# C. FINAL JOURNAL MATRIX

Notasi kolom dampak: **CE** = `cost_entries` · **GL** · **RAB** = realisasi RAB · **AP** = outstanding · **DOC** = dokumen · **AGE** = aging.
Contoh dasar: termin Rp100.000.000, proyek P, dua item RAB (Struktur 80 jt, Finishing 20 jt), vendor V, jatuh tempo 30 hari.

## Skenario A — Draft termin (dicatat, belum disetujui)

| Aspek | Isi |
|---|---|
| **Source transaction** | `POST /ap/invoices` → `ap_invoices` + `cost_entries` (2 baris) + `PostingService.Create` |
| **journal source** | `ap_invoice` — **DRAFT**, `posted_at` NULL |
| **Debit** | 1-3100 Persediaan RE — Hard Cost · `project_id = P` · **100.000.000** |
| **Credit** | 2-1000 Hutang Usaha · **100.000.000** |

| Dampak | Nilai |
|---|---|
| **CE** | 2 baris tertulis (80 jt + 20 jt), `payment_method='payable'`, `ap_invoice_id` terisi, `journal_entry_id` → jurnal draft |
| **GL** | **NOL** — `posted_at IS NULL` |
| **RAB** | **NOL** — kedua reader posted-only |
| **AP** | **NOL** — belum kewajiban |
| **DOC** | **Tidak ada** — tidak ada kas bergerak; `Create` mengabaikan `DocumentSpec` |
| **AGE** | **NOL** — jam aging belum jalan |

> **[TEMUAN]** Baris `cost_entries` **sudah tertulis** saat draft, tetapi tidak terbaca reader mana pun karena `GetRealisasiPerItem` men-*join* `journal_entries` dan mensyaratkan `posted_at IS NOT NULL`. Menulis CE lebih dulu diperlukan agar `ap_invoice_id`/`journal_entry_id` konsisten dalam satu transaksi.

## Skenario B — Approved + posted

| Aspek | Isi |
|---|---|
| **Source transaction** | `POST /ap/invoices/{id}/post` → `RequireApproved(TargetCost, id)` → `IsPeriodClosed(tanggal)` → `PostingService.Post` |
| **Perubahan data** | **hanya `posted_at`**. Tidak ada jurnal baru, tidak ada CE baru |
| **Debit / Credit** | sama dengan A (jurnal yang sama, kini terposting) |

| Dampak | Nilai |
|---|---|
| **CE** | Tidak berubah — baris yang sama kini terbaca |
| **GL** | Dr 1-3100 **100.000.000** / Cr 2-1000 **100.000.000** |
| **RAB** | proyek **100.000.000**; per item **Struktur 80 jt**, **Finishing 20 jt** |
| **AP** | **100.000.000** |
| **DOC** | **Tidak ada** — tetap tidak ada kas bergerak |
| **AGE** | Mulai berjalan dari `due_date`; bucket `current` |

## Skenario C — Partial payment (Rp60 juta dari Rp100 juta)

| Aspek | Isi |
|---|---|
| **Source transaction** | `POST /ap/payments` (ber-`Idempotency-Key`) → row lock kewajiban → `CreateAndPost` |
| **journal source** | `ap_payment` |
| **Debit** | 2-1000 Hutang Usaha · **60.000.000** |
| **Credit** | 1-1300 Bank — BCA · **60.000.000** |

| Dampak | Nilai |
|---|---|
| **CE** | **TIDAK BERUBAH** — pembayaran tidak menulis `cost_entries` |
| **GL** | 2-1000 → 40.000.000 Cr; Bank −60.000.000 |
| **RAB** | **TIDAK BERUBAH** (100 jt) — 2-1000 & 1-1300 di luar daftar akun taksonomi |
| **AP** | 100.000.000 − 60.000.000 = **40.000.000** (turunan) |
| **DOC** | **BKK** #1 |
| **AGE** | Sisa 40 jt tetap ter-aging dari `due_date` yang sama |

Ditulis: `ap_payments` 1 baris (60 jt) + `ap_payment_allocations` 1 baris (`allocation_type='invoice'`).

## Skenario D — Full payment (sisa Rp40 juta)

| Aspek | Isi |
|---|---|
| **Debit** | 2-1000 Hutang Usaha · **40.000.000** |
| **Credit** | 1-1300 Bank — BCA · **40.000.000** |

| Dampak | Nilai |
|---|---|
| **CE** | **TIDAK BERUBAH** |
| **GL** | 2-1000 → **0**; Bank kumulatif −100.000.000 |
| **RAB** | **TIDAK BERUBAH** (100 jt) |
| **AP** | **0** |
| **DOC** | **BKK** #2 — dua pergerakan kas = dua dokumen (INV-DOC-1) |
| **AGE** | Keluar dari aging; status turunan `paid` |

## Skenario E — Retention (termin Rp100 juta, retensi 5%)

### E1 — Pengakuan (posted)

| Aspek | Isi |
|---|---|
| **journal source** | `ap_invoice` |
| **Debit** | 1-3100 · `project_id = P` · **100.000.000** |
| **Credit** | 2-1000 Hutang Usaha · **95.000.000** |
| **Credit** | **2-1100 Hutang Retensi** · **5.000.000** |

| Dampak | Nilai |
|---|---|
| **CE** | `amount` total **100.000.000** ← **bruto**, retensi tidak menguranginya |
| **GL** | 2-1000 = 95 jt Cr; 2-1100 = 5 jt Cr |
| **RAB** | **100.000.000** ← penuh |
| **AP** | Hutang usaha 95 jt + retensi 5 jt = total kewajiban vendor 100 jt |
| **DOC** | Tidak ada |
| **AGE** | **95 jt** masuk aging AP dengan `due_date` termin; **5 jt** masuk aging retensi dengan `due_date` retensi sendiri — **dua baris terpisah** |

### E2 — Pembayaran termin (di luar retensi)

| **Debit** | 2-1000 · **95.000.000** · **Credit** 1-1300 Bank · **95.000.000** |
|---|---|

| Dampak | Nilai |
|---|---|
| **CE / RAB** | **TIDAK BERUBAH** (100 jt) |
| **GL** | 2-1000 → **0**; 2-1100 tetap 5 jt |
| **AP** | AP current **0**; retensi **5.000.000** |
| **DOC** | **BKK** |
| **AGE** | Baris AP keluar dari aging. Baris retensi **tetap**, dengan jatuh temponya sendiri — **tidak** ikut terhitung terlambat |

### E3 — Pelepasan retensi

| Aspek | Isi |
|---|---|
| **journal source** | `ap_retention` |
| **Debit** | **2-1100 Hutang Retensi** · **5.000.000** |
| **Credit** | 1-1300 Bank · **5.000.000** |

| Dampak | Nilai |
|---|---|
| **CE / RAB** | **TIDAK BERUBAH** (100 jt — sejak E1) |
| **GL** | 2-1100 → **0**; kas kumulatif −100.000.000 |
| **AP** | **0** |
| **DOC** | **BKK** |
| **AGE** | Baris retensi keluar |

## Skenario F — Vendor advance (Rp20 juta dibayar di muka)

| Aspek | Isi |
|---|---|
| **Source transaction** | `POST /ap/advances` → `CreateAndPost` |
| **journal source** | `ap_advance` |
| **Debit** | **1-5300 Uang Muka Vendor** · **20.000.000** |
| **Credit** | 1-1300 Bank · **20.000.000** |

| Dampak | Nilai |
|---|---|
| **CE** | **TIDAK ADA BARIS** ← belum ada biaya |
| **GL** | 1-5300 = 20 jt Dr; Bank −20.000.000 |
| **RAB** | **NOL** ← 1-5300 **bukan** akun taksonomi, tidak terbaca `ActualCostByCode` |
| **AP** | **NOL** ← uang muka bukan kewajiban; ia **aset** |
| **DOC** | **BKK** |
| **AGE** | Tidak masuk aging AP (bukan hutang). Muncul di panel "Uang muka belum terkompensasi" |

## Skenario G — Advance settlement terhadap termin (termin Rp100 juta)

| Aspek | Isi |
|---|---|
| **journal source** | `ap_invoice` (satu jurnal, tiga baris) |
| **Debit** | 1-3100 · `project_id = P` · **100.000.000** |
| **Credit** | **1-5300 Uang Muka Vendor** · **20.000.000** ← kompensasi |
| **Credit** | 2-1000 Hutang Usaha · **80.000.000** |

| Dampak | Nilai |
|---|---|
| **CE** | `amount` total **100.000.000** ← **bruto**, offset tidak menguranginya |
| **GL** | 1-5300 → **0**; 2-1000 = 80 jt Cr |
| **RAB** | **100.000.000** ← sekali, bukan 120 juta |
| **AP** | **80.000.000** |
| **DOC** | Tidak ada — tidak ada kas bergerak di sini (kas sudah bergerak di F) |
| **AGE** | 80 jt masuk aging |

Ditulis: `ap_payment_allocations` 1 baris `allocation_type='advance'` (20 jt) menautkan uang muka F ke kewajiban G.

**Total debit akun taksonomi sepanjang F+G:** 0 + 100.000.000 = **100.000.000, tepat sekali.**
**Total kas keluar F + pelunasan sisa:** 20.000.000 + 80.000.000 = **100.000.000, tepat sekali.**

## Skenario H — Reversal

### H1 — Membalik pengakuan kewajiban (belum ada pembayaran)

| Aspek | Isi |
|---|---|
| **Source transaction** | `POST /ap/invoices/{id}/reverse` → `PostingService.Reverse(tanggal_pembalik)` |
| **journal source** | `reversal` |
| **Debit** | 2-1000 Hutang Usaha · **100.000.000** |
| **Credit** | 1-3100 Persediaan RE · `project_id = P` · **100.000.000** |

| Dampak | Nilai |
|---|---|
| **CE** | **Baris asli TIDAK DIHAPUS.** Tidak ada baris CE baru |
| **GL** | 2-1000 → 0; 1-3100 → 0 |
| **RAB** | → **0**, lewat **netting** `Σ debit(non-reversal) − Σ kredit(reversal)` (`actual_cost.go:106`) — bukan lewat penghapusan |
| **AP** | **0**; status turunan `reversed` |
| **DOC** | **JR** menunjuk jurnal asal |
| **AGE** | Keluar dari aging |
| **Guard** | Periode dicek pada **tanggal pembalik**, bukan tanggal asal |

> ⚠️ **[TEMUAN — asimetri yang harus disadari]** `GetRealisasiPerItem` menjumlahkan `cost_entries.amount` dan **tidak mengenal netting reversal**. Setelah H1, realisasi **tingkat proyek** menjadi 0 tetapi realisasi **per item** tetap 80 jt / 20 jt — **kedua reader berselisih**. Ini perilaku **existing** (berlaku sama untuk pembalikan `cost_entries` biasa hari ini), bukan cacat yang W-11 perkenalkan, dan D-5 melarang memperbaikinya di W-11. **Mitigasi wajib:** pembalikan kewajiban AP **menandai baris `cost_entries` terkait sebagai terbalik** (kolom `reversed_at` pada `cost_entries` — lihat D.3 dan Risiko R-1) sehingga selisih ini tidak melebar. Kalau kolom itu tidak disetujui, selisihnya **harus dilaporkan sebagai keterbatasan yang diketahui**, bukan dibiarkan senyap.

### H2 — Membalik pembayaran

| Aspek | Isi |
|---|---|
| **Debit** | 1-1300 Bank · **60.000.000** |
| **Credit** | 2-1000 Hutang Usaha · **60.000.000** |

| Dampak | Nilai |
|---|---|
| **CE / RAB** | **TIDAK BERUBAH** — pembayaran tidak pernah menyentuh biaya, pembalikannya pun tidak |
| **GL** | 2-1000 naik kembali 60 jt; kas kembali |
| **AP** | Outstanding **naik kembali** otomatis — alokasi ditandai `reversed_at`, dan outstanding adalah turunan |
| **DOC** | **JR** (+ dokumen kas pembalik sesuai `reversalDocument`) |
| **AGE** | Baris kembali masuk aging |

**Aturan urutan (mengikat):** kewajiban dengan alokasi non-terbalik **tidak boleh** dibalik. H2 harus mendahului H1. Diuji INV-AP-14.

## Skenario I — Overdue

| Aspek | Isi |
|---|---|
| **Source transaction** | **TIDAK ADA** — overdue bukan peristiwa ekonomi |
| **Debit / Credit** | **TIDAK ADA JURNAL** |

| Dampak | Nilai |
|---|---|
| **CE / GL / RAB / AP / DOC** | **TIDAK BERUBAH** |
| **AGE** | Bucket berpindah `current` → `1_30` → `31_60` → `61_90` → `90_plus` seiring `asOf`; status turunan `overdue` |

> **[TEMUAN]** Overdue adalah **fungsi dari tanggal**, bukan state tersimpan. Tidak ada job, tidak ada kolom, tidak ada jurnal. Konsisten dengan keputusan T-4 di sisi piutang ("overdue turunan tanggal").

## Skenario J — Closed / settled payable

| Aspek | Isi |
|---|---|
| **Source transaction** | **TIDAK ADA** — pelunasan sudah dijurnal di C/D/E |
| **Debit / Credit** | **TIDAK ADA JURNAL TAMBAHAN** |

| Dampak | Nilai |
|---|---|
| **CE / RAB** | **TIDAK BERUBAH** — biaya tetap 100 jt selamanya |
| **GL** | 2-1000 (dan 2-1100) = 0 untuk kewajiban ini |
| **AP** | Outstanding **0** |
| **DOC** | Tidak ada dokumen baru |
| **AGE** | Keluar dari laporan aging |
| **Status** | `paid` — **turunan**, tidak ada kolom yang di-`UPDATE` |

> **[TEMUAN]** Tidak ada operasi "close". Kewajiban lunas karena Σ alokasi mencapai nilainya, bukan karena seseorang menandainya lunas. Tidak ada kolom yang bisa berbohong.

## C.99 Ringkasan dampak lintas skenario

| Skenario | Jurnal? | CE | RAB | AP | DOC |
|---|:---:|:---:|:---:|:---:|:---:|
| A Draft | draft | tulis | — | — | — |
| B Posted | post | — | **+100** | **+100** | — |
| C Partial | ✅ | — | — | −60 | BKK |
| D Full | ✅ | — | — | −40 | BKK |
| E1 Retensi akui | ✅ | tulis | **+100** | +95 AP, +5 ret | — |
| E2 Bayar termin | ✅ | — | — | −95 | BKK |
| E3 Lepas retensi | ✅ | — | — | −5 ret | BKK |
| F Uang muka | ✅ | **—** | **—** | **—** | BKK |
| G Offset+termin | ✅ | tulis | **+100** | +80 | — |
| H1 Balik kewajiban | ✅ | tandai | −100 *(lihat catatan)* | −100 | JR |
| H2 Balik bayar | ✅ | — | — | +60 | JR |
| I Overdue | ❌ | — | — | — | — |
| J Settled | ❌ | — | — | — | — |

**RAB hanya bergerak di B, E1, G, dan H1** — yaitu hanya saat **pengakuan** dan **pembalikan pengakuan**. Tidak satu pun peristiwa pembayaran (C, D, E2, E3, F) menggerakkannya.

## C.PPN — Varian vendor PKP (D-16)

Matriks A–J di atas **tetap berlaku apa adanya** untuk vendor **non-PKP** (tanpa faktur pajak) — kasus yang umum untuk kontraktor kecil. Untuk vendor **PKP**, jurnal **pengakuan** bertambah satu baris debit; seluruh peristiwa pembayaran, retensi, uang muka, dan pembalikan **tidak berubah bentuknya**.

**Aturan yang dikunci:**

| Aturan | Isi |
|---|---|
| **PPN-1** | PPN Masukan didebit ke **1-5100**, **tanpa `project_id`** — pajak yang dapat dikreditkan bukan biaya proyek. Baris ini tidak boleh ditandai proyek/fase/unit |
| **PPN-2** | `cost_entries.amount` = **DPP**, tidak pernah termasuk PPN. Realisasi RAB naik sebesar DPP saja |
| **PPN-3** | Kewajiban ke vendor (2-1000) = **bruto termasuk PPN**. Itulah jumlah yang benar-benar ditransfer |
| **PPN-4** | Retensi dihitung atas **DPP**, bukan atas nilai bruto |
| **PPN-5** | `faktur_pajak_number` wajib diisi bila `ppn_amount > 0`; kosong ⇒ vendor diperlakukan non-PKP |
| **PPN-6** | 1-5100 **bukan** akun taksonomi CostCategory ⇒ tidak terbaca `ActualCostByCode` ⇒ **INV-AP-8 tetap berlaku tanpa perubahan rumus** |

### B′ — Pengakuan termin, vendor PKP (DPP 100 jt, PPN 11% = 11 jt)

| Baris | Akun | Proyek | Debit | Kredit |
|---|---|---|---:|---:|
| 1 | 1-3100 Persediaan RE — Hard Cost | **P** | 100.000.000 | |
| 2 | **1-5100 PPN Masukan** | **—** | 11.000.000 | |
| 3 | 2-1000 Hutang Usaha | — | | **111.000.000** |

| Dampak | Nilai |
|---|---|
| **CE** | **100.000.000** (DPP) — 80 jt + 20 jt per item |
| **RAB** | **100.000.000** — PPN tidak menaikkan realisasi |
| **AP** | **111.000.000** — yang akan ditransfer |
| **VATReport** | PPN masukan **11.000.000** mulai terisi |

### D′ — Pelunasan penuh vendor PKP

`Dr 2-1000 **111.000.000** / Cr 1-1300 Bank **111.000.000**` · BKK · **CE & RAB tidak berubah**.

### E1′ — Pengakuan + retensi 5% (atas DPP), vendor PKP

| Baris | Akun | Debit | Kredit |
|---|---|---:|---:|
| 1 | 1-3100 Persediaan RE (proyek P) | 100.000.000 | |
| 2 | 1-5100 PPN Masukan | 11.000.000 | |
| 3 | **2-1100 Hutang Retensi** | | **5.000.000** |
| 4 | 2-1000 Hutang Usaha | | **106.000.000** |

CE = 100 jt · RAB = 100 jt · AP current 106 jt + retensi 5 jt.

### G′ — Kompensasi uang muka 20 jt, vendor PKP

| Baris | Akun | Debit | Kredit |
|---|---|---:|---:|
| 1 | 1-3100 Persediaan RE (proyek P) | 100.000.000 | |
| 2 | 1-5100 PPN Masukan | 11.000.000 | |
| 3 | **1-5300 Uang Muka Vendor** | | **20.000.000** |
| 4 | 2-1000 Hutang Usaha | | **91.000.000** |

CE = 100 jt · RAB = **100 jt, sekali** · AP = 91 jt · kas total F + pelunasan = 20 + 91 = **111 jt, tepat**.

> `// TODO(tax-advisor): perlakuan PPN atas pembayaran uang muka ke vendor PKP (skenario F) belum dikonfirmasi. Default P0 yang dipakai: uang muka dicatat sebesar kas keluar ke 1-5300 tanpa memecah PPN, dan seluruh PPN diakui saat termin diakui (G′). Bila vendor menerbitkan faktur pajak atas uang muka, perlakuan ini harus ditinjau. Tidak ditebak — lihat R-9.`

---

# D. FINAL DATA MODEL

## D.1 Tabel baru (4)

Semua: `id BIGINT UNSIGNED AUTO_INCREMENT PK`, `tenant_id BIGINT UNSIGNED NOT NULL` + index, `created_at`/`updated_at DATETIME(3)`, kolom uang `DECIMAL(20,4) NOT NULL DEFAULT '0.0000'`.

```
vendors
  name VARCHAR(200) NOT NULL
  npwp VARCHAR(30), address VARCHAR(255)
  bank_name VARCHAR(100), bank_account_number VARCHAR(50)
  bank_account_name VARCHAR(200)          -- boleh dicoret (D-7, catatan Revisi 2)
  payment_terms_days INT NOT NULL DEFAULT 0
  is_active BOOL NOT NULL DEFAULT TRUE
  UNIQUE (tenant_id, name)
  INDEX (tenant_id)

ap_invoices                                -- kewajiban / termin
  vendor_id, vendor_name_snapshot VARCHAR(200)
  invoice_number VARCHAR(100), invoice_date DATE, due_date DATE
  contract_ref VARCHAR(100), progress_percent DECIMAL(9,4)
  dpp_amount, retention_amount, ppn_amount, payable_amount
      -- dpp_amount     = dasar pengenaan pajak = Σ cost_entries.amount (PPN-2)
      -- ppn_amount     = 0 untuk vendor non-PKP
      -- payable_amount = dpp + ppn − retensi  → yang masuk 2-1000 (PPN-3, PPN-4)
  retention_due_date DATE NULL             -- jatuh tempo retensi (E2)
  faktur_pajak_number VARCHAR(50)          -- wajib bila ppn_amount > 0 (PPN-5)
  journal_entry_id BIGINT UNSIGNED NULL    -- jurnal pengakuan (draft/posted)
  description VARCHAR(255), created_by BIGINT UNSIGNED NULL
  INDEX (tenant_id), (tenant_id, vendor_id), (tenant_id, due_date)

ap_payments
  vendor_id, payment_date DATE
  bank_account_code VARCHAR(20), amount
  payment_kind VARCHAR(20)   -- invoice | advance | retention
  journal_entry_id, document_id BIGINT UNSIGNED NULL
  idempotency_key VARCHAR(100) NULL, notes VARCHAR(255)
  created_by BIGINT UNSIGNED NULL
  UNIQUE (tenant_id, idempotency_key)
  INDEX (tenant_id), (tenant_id, vendor_id)

ap_payment_allocations
  ap_payment_id, ap_invoice_id
  allocation_type VARCHAR(20)   -- invoice | retention | advance
  amount
  reversed_at DATETIME(3) NULL
  INDEX (tenant_id), (tenant_id, ap_invoice_id), (tenant_id, ap_payment_id)
```

> `ap_payment_allocations.allocation_type='advance'` menautkan pembayaran uang muka (F) ke kewajiban yang dikompensasinya (G). Uang muka yang belum terkompensasi = pembayaran `payment_kind='advance'` tanpa alokasi.

## D.2 Kolom baru pada tabel existing — semua NULLABLE, **backfill NOL**

```
cost_entries.vendor_id      BIGINT UNSIGNED NULL  FK vendors     ON DELETE SET NULL
cost_entries.ap_invoice_id  BIGINT UNSIGNED NULL  FK ap_invoices ON DELETE SET NULL
```

## D.3 `cost_entries.reversed_at` — ❌ **TIDAK DIBUAT di W-11** (D-20)

Asimetri reversal antar reader diangkat menjadi pekerjaan terpisah **W-11b**, berikut perbaikan reader-nya. W-11 **tidak** menambah kolom ini dan **tidak** menyentuh reader mana pun — D-5 berlaku penuh. Konsekuensi yang diterima secara sadar: sampai W-11b selesai, pembalikan kewajiban AP membuat realisasi tingkat proyek nol sementara realisasi per item tetap terisi. Ini perilaku **existing** yang sudah berlaku hari ini di luar W-11, bukan regresi yang W-11 perkenalkan.

## D.4 Yang sengaja TIDAK dibuat

| | Alasan |
|---|---|
| `ap_invoice_lines` | **D-4 melarangnya.** Baris biaya = `cost_entries` |
| Kolom `status` | D-12 — turunan |
| Kolom `outstanding` | D-12 — turunan |
| Kolom `approved_by` di `ap_invoices` | Audit trail milik `approval_actions` |
| Kolom nomor dokumen di tabel AP | Milik engine W-2 lewat `journal_entries.document_id` |
| `purchase_orders`, `goods_receipts` | Di luar scope |
| `vendor_contracts` | P1 (BD-13) |
| Perubahan pada `charge_payouts.vendor` | Di luar scope W-11 |

## D.5 Pemetaan field `cost_entries` (D-4)

| Field | Isi dari AP |
|---|---|
| `project_id` | Wajib untuk tier `direct`/`shared`; wajib NULL untuk `overhead` (`chk_ce_tier_project` menegakkan) |
| `phase_id`, `unit_id` | Sesuai aturan tier existing |
| `budget_item_id` | Per baris, opsional (BD-2). Kosong ⇒ hilang dari drill-down item |
| `category`, `cost_tier` | Menentukan akun debit |
| `amount` | **Biaya bruto**: sebelum retensi, sebelum offset uang muka, **tanpa PPN** |
| `vendor` | Snapshot nama saat transaksi |
| `vendor_id` | FK kanonik (baru) |
| `payment_method` | `'payable'` |
| `journal_entry_id` | Jurnal pengakuan — N baris CE → 1 jurnal |
| `ap_invoice_id` | FK kewajiban (baru) |

---

# E. FINAL API / WORKFLOW PLAN

**Rencana saja — tidak ada endpoint yang dibuat pada tahap ini.**

## E.1 Workflow pengakuan

```
1. POST /ap/invoices/preview   → jurnal pratinjau (fungsi resolusi yang SAMA dgn posting)
2. POST /ap/invoices           → ap_invoices + cost_entries[] + jurnal DRAFT   (1 transaksi)
3. [opsional] approval.Request TargetCost / TargetID = ap_invoice.id
4. POST /ap/invoices/{id}/post → RequireApproved → IsPeriodClosed → Post       (1 transaksi)
```

## E.2 Workflow pembayaran

```
1. POST /ap/payments/preview  → alokasi otomatis (tertua dulu) + jurnal pratinjau
2. POST /ap/payments          → [Idempotency-Key]
     BEGIN
       lookup idempotency  → hit: kembalikan hasil lama; konflik: 409
       SELECT ... FOR UPDATE pada kewajiban yang dialokasikan
       hitung outstanding; validasi Σ alokasi ≤ outstanding
       CreateAndPost (Dr 2-1000/2-1100 / Cr kas) + DocumentSpec{BKK}
       tulis ap_payments + ap_payment_allocations
     COMMIT
```

### Lebih bayar (D-18)

```
Σ alokasi > outstanding  →  422 dengan payload:
     { error: "overpayment", outstanding, requested, excess }

Admin memilih secara eksplisit:
  (a) perbaiki nominal, atau
  (b) POST /ap/payments  dengan  allow_excess_as_advance = true
        → satu jurnal, dua sisi kredit-debit:
            Dr 2-1000            <outstanding>
            Dr 1-5300 Uang Muka  <excess>
            Cr 1-1300 Bank       <total transfer>
        → satu BKK; ap_payment_allocations dapat baris 'invoice' + baris 'advance'
```

**Kelebihan tidak pernah menjadi uang muka secara otomatis.** Tanpa flag eksplisit dari admin, permintaan ditolak (INV-AP-18).

## E.3 Endpoint P0

| Method | Path |
|---|---|
| GET / POST | `/vendors` |
| PATCH | `/vendors/{id}` |
| GET | `/ap/invoices` (filter: vendor, proyek, status, rentang jatuh tempo) |
| POST | `/ap/invoices/preview` |
| POST | `/ap/invoices` |
| GET | `/ap/invoices/{id}` |
| POST | `/ap/invoices/{id}/post` |
| POST | `/ap/invoices/{id}/reverse` |
| POST | `/ap/payments/preview` |
| POST | `/ap/payments` |
| POST | `/ap/payments/{id}/reverse` |
| POST | `/ap/advances` |
| POST | `/ap/retentions/{id}/release` |
| GET | `/ap/aging` |

**Tidak ada** endpoint approve — milik `internal/approval`.

## E.4 Frontend P0

Menu **Keuangan → Hutang Usaha** (setelah *Pengeluaran*): dashboard (KPI + aging + jatuh tempo terdekat) · daftar hutang · master vendor · detail kewajiban · modal bayar 2 langkah (Input → Pratinjau alokasi & jurnal → Simpan) · panel uang muka belum terkompensasi · penanda sumber "Termin →" di halaman biaya proyek. Empty state informatif + CTA. Warna hanya lewat token design system.

---

# F. INVARIANT & TEST MATRIX

| Kode | Invariant | Permintaan pemilik (item 8) | Uji |
|---|---|---|---|
| **INV-AP-1** | Σ outstanding AP == Σ pergerakan 2-1000 & 2-1100 ber-source `{ap_invoice, ap_payment, ap_retention}`; selisih vs saldo GL **wajib terjelaskan penuh** oleh `opening_balance` + `manual` + `system` (warisan) | tie-out sub-ledger↔GL | integration: posting campuran + `AccountMovementBySource` |
| **INV-AP-2** | Σ alokasi non-terbalik ≤ nilai kewajiban, **selalu**, termasuk di bawah konkurensi | — | integration: N goroutine bayar kewajiban sama |
| **INV-AP-3** | Pembayaran AP **tidak pernah** mendebit/mengkredit akun taksonomi CostCategory | ✅ pembayaran tidak menambah RAB | unit: periksa baris jurnal vs `RoleCodeList` taksonomi |
| **INV-AP-4** | Realisasi RAB **identik** sebelum & sesudah pembayaran | ✅ pembayaran tidak menambah RAB | integration: snapshot `GetRealisasiByProject` + `GetRealisasiPerItem` (skenario C, D, E2, E3) |
| **INV-AP-5** | Setiap jurnal pembayaran AP punya **tepat satu** BKK | ✅ payment ikut BKK/document invariant | integration: coba post tanpa dokumen → wajib gagal |
| **INV-AP-6** | Kewajiban dengan alokasi non-terbalik tidak dapat dibalik | — | unit + integration |
| **INV-AP-7** | Query AP lintas tenant mengembalikan **nol** baris | — | integration (wajib CLAUDE.md) |
| **INV-AP-8** | Per kewajiban: `Σ cost_entries.amount` == `Σ debit akun taksonomi` di jurnal pengakuannya. Retensi/PPN/offset uang muka **tidak** mengurangi `amount` | ✅ `cost_entries.amount` konsisten dgn debit biaya/WIP · ✅ retensi tidak mengurangi biaya/RAB | integration: skenario B, E1, G |
| **INV-AP-9** | Kewajiban ber-jurnal **draft**: nol GL, nol RAB (proyek **dan** item), nol outstanding, nol aging | ✅ D-2 | integration: 4 pembacaan sebelum & sesudah `Post` (skenario A→B) |
| **INV-AP-10** | Draft bertanggal di periode tertutup **tidak dapat** di-post | ✅ posting ikut period guard | integration: draft → tutup periode → post → wajib gagal |
| **INV-AP-11** | Setiap biaya proyek yang diakui lewat AP **muncul** di `GetRealisasiPerItem` bila `budget_item_id` terisi | ✅ biaya AP tetap muncul di GetRealisasiPerItem | integration: skenario B, cek per item 80/20 |
| **INV-AP-12** | Biaya AP diakui **tepat sekali**: Σ debit taksonomi sepanjang seluruh siklus == nilai biaya | ✅ biaya AP tidak dihitung dua kali | integration: jalankan A→B→C→D dan F→G→bayar; hitung total debit taksonomi |
| **INV-AP-13** | Reversal **tidak menghapus** baris mana pun; `cost_entries` & `journal_entries` asal tetap ada | ✅ reversal tidak menghapus data | integration: hitung baris sebelum/sesudah H1 & H2 |
| **INV-AP-14** | Pembayaran sebagian **tidak mengubah** nilai biaya awal maupun realisasi RAB | ✅ partial payment tidak ubah biaya awal | integration: skenario C |
| **INV-AP-15** | Uang muka vendor **tidak** menjadi biaya/RAB saat dibayar; baru masuk biaya saat termin diakui | ✅ vendor advance tidak langsung jadi biaya/RAB | integration: skenario F (RAB=0) lalu G (RAB=100 jt, sekali) |
| **INV-AP-16** | Jurnal seimbang di setiap jalur (`Σ debit == Σ kredit`) | invariant #1 CLAUDE.md | ditegakkan `validateLines`; diuji per skenario |
| **INV-AP-17** | **PPN tidak pernah menjadi biaya proyek.** Baris 1-5100 tidak boleh ber-`project_id`/`phase_id`/`unit_id`; `Σ cost_entries.amount` == DPP, bukan bruto; realisasi RAB identik antara vendor PKP dan non-PKP untuk DPP yang sama | D-16 / PPN-1, PPN-2 | integration: skenario B vs B′ — RAB wajib sama persis 100 jt |
| **INV-AP-18** | **Lebih bayar tidak pernah otomatis jadi aset.** `Σ alokasi > outstanding` ditolak, kecuali `allow_excess_as_advance` diminta eksplisit; bila diminta, kelebihan mendarat **tepat** di 1-5300 dan tidak di akun lain | D-18 | integration: transfer berlebih tanpa flag → 422; dengan flag → 1-5300 naik sebesar kelebihan |
| **INV-AP-19** | Tenant **tanpa** workflow aktif dapat mem-post kewajiban tanpa approval; tenant **dengan** workflow aktif tidak dapat | D-19 | integration: dua tenant, satu berworkflow satu tidak |

**Regresi:** `go test ./...` hijau; integration `-tags integration` **wajib `-p 1`** (F-4: bentrok tenant 9900777). Frontend: Node 22 built-in runner, tanpa dependency baru. Gerbang selesai juga mensyaratkan verifikasi browser di `http://localhost:PORT` (loopback, dibuktikan `ss -ltn`) — bukan `tsc` saja.

---

# G. DEPENDENCY MAP

Fungsi existing yang **dipakai ulang** (bukan ditulis ulang):

| Kebutuhan | Fungsi/tipe existing | Lokasi | Cara pakai |
|---|---|---|---|
| **Approval — gate** | `approval.Service.RequireApproved(ctx, tenantID, target, targetID)` | `internal/approval/service.go:408-427` | Dipanggil sebelum `Post`. OPT-IN |
| **Approval — kosakata** | `TargetCost`, `TargetPayment` | `internal/approval/model.go:29-30` | Sudah ada; tidak menambah TargetType |
| **Approval — status** | `StatusPending…StatusCancelled` | `model.go:52-56` | — |
| **Audit trail** | `approval.Action{ActorID, ActorRole, Decision, Comment, CreatedAt}` — **append-only** | `model.go:164-176` | Siapa-kapan-apa untuk D-1 |
| **Anti ubah-setelah-approve** | `Request.TargetSnapshotJS`, `WorkflowSnapshotJS`, `WorkflowRevision` | `model.go:147-150` | Snapshot beku Increment 5.1 |
| **Posting — draft** | `PostingService.Create(ctx, CreateJournalRequest)` | `internal/ledger/posting_service.go:132` | Skenario A. `DocumentSpec` **diabaikan** di jalur ini |
| **Posting — post** | `PostingService.Post(ctx, tenantID, entryID)` | `posting_service.go:178` | Skenario B. Menegakkan INV-DOC-1 di dalam tx |
| **Posting — satu fase** | `PostingService.CreateAndPost` | `posting_service.go:288` | Pembayaran (C, D, E2, E3, F) |
| **Posting — draft+dokumen** | `PostingService.PostDraft(…, spec)` | `posting_service.go:224` | Bila pembayaran perlu jalur dua fase |
| **Posting — input** | `CreateJournalRequest`, `LineInput{AccountID, Debit, Credit, ProjectID, PhaseID, UnitID}` | `posting_service.go:32-56` | **`ProjectID` per baris** ⇒ satu kewajiban boleh lintas proyek |
| **Period guard** | `IsPeriodClosed(ctx, tenantID, date)` | `internal/ledger/repository.go:176` | Dipanggil di approve→post (D-9). **Satu-satunya implementasi** |
| **Numbering** | `document_sequences` engine W-2 — `last_val`, fiscal year dari waktu terbit | `internal/document/engine.go:152` | Tidak dipanggil langsung; lewat `DocumentSpec` |
| **BKK** | `ledger.DocCashOut`, `DefaultCashSpec`, `enforceDocument` | `internal/ledger/document_issuer.go:63` | Kas keluar otomatis BKK |
| **Dokumen — penerbitan** | `document.IssueForJournal`, `LinkJournal`, `IssueReversal` | `internal/document/journal_link.go:79,47,111` | Dipakai `PostingService`, bukan langsung oleh W-11 |
| **Reversal** | `PostingService.Reverse(ctx, tenantID, entryID, reverseDate)` | `posting_service.go:330` | H1 & H2. JR + cek periode pada tanggal pembalik |
| **Aging — aturan bucket** | `bucketFor(daysOverdue)` — **saat ini unexported** | `internal/receivable/receivable.go:117` | **Perlu diekspor** agar AP memakai aturan yang sama (satu-satunya perubahan pada package existing) |
| **Aging — hari & status** | `receivable.DaysBetween`, `EffectiveStatus` | `receivable.go:135,142` | Sudah publik; dipakai apa adanya |
| **Aging — struct baris** | `receivable.Row` | `receivable.go:53` | ❌ **TIDAK dipakai** — bentuknya customer-shaped (`BuyerName`, `UnitCode`, `ContractID`, `BuyerPhone`). AP punya struct sendiri |
| **Aging — enum source** | `receivable.Source` (`house`/`realization`/`legacy`, ber-`Valid()`) | `receivable.go:29-46` | ❌ **TIDAK ditambah** — hutang bukan piutang |
| **Saldo GL** | `LedgerBalanceService.AccountBalance(ctx, tenantID, code, asOf)` | `internal/ledger/balance_service.go:78` | Tie-out INV-AP-1 |
| **Rekonsiliasi kupas-lapis** | `AccountMovementBySource(ctx, tenantID, code, asOf)` | `balance_service.go:121` | INV-AP-1 per source |
| **Rekonsiliasi per jurnal** | `AccountMovementForJournals(…, journalIDs, asOf)` | `balance_service.go:183` | Menaut kewajiban ke pergerakan GL-nya |
| **Registry akun** | `RoleCodeList`, `AccountInRole`, `RolePayable`, `RoleVATInput` | `internal/ledger/account_role.go:29,34,48,53` | **Wajib** — dilarang `code IN (...)` / `LIKE` |
| **Realisasi (dibaca, tidak diubah)** | `ActualCostByCode`, `GetRealisasiByProject`, `GetRealisasiPerItem` | `actual_cost.go:106`, `budget/repository.go:309,331` | **READ-ONLY. D-5 melarang perubahan** |
| **Idempotency + row lock** | pola `charge.RecordPayout`: lookup key **di dalam** tx → `ErrIdempotencyConflict`; `gormLockingUpdate()` = `clause.Locking{Strength:"UPDATE"}` | `internal/charge/service.go:18,618-730` | Dicerminkan untuk `POST /ap/payments` |
| **Isolasi tenant** | `auth.TenantIDFrom(ctx)` + `WHERE tenant_id = ?` eksplisit di setiap query | `internal/platform/auth/jwt.go:72` | Pola aktual repo (463 kemunculan) — **bukan** GORM global scope |
| **Aktor (audit)** | `auth.UserIDFrom(ctx)` | `jwt.go:81` | Isi `created_by` |
| **Pola sub-ledger tanpa jurnal** | `internal/legacyar` — INV-LAR-4 | `legacyar/service.go:200-320` | Cadangan bila BD-15 berubah ke opsi B |
| **Uang (wajib)** | `domain.Money`, `NewMoney(string)`, `FromInt(int64)` | `internal/domain` | **Dilarang `float64`** |

## G.1 Satu-satunya perubahan pada package existing

| Perubahan | Sifat | Risiko |
|---|---|---|
| Ekspor `receivable.bucketFor` → `BucketFor` | Aditif murni; tidak ada logika berubah | Sangat rendah |
| Tambah `RoleRetentionPayable`, `RoleVendorAdvance` ke `AccountRoleRegistry` | Aditif | Sangat rendah |
| Tambah 2 kolom nullable di `cost_entries` | Aditif, tanpa backfill | Rendah |

**Tidak ada perubahan** pada `internal/budget`, `internal/ledger/actual_cost.go`, `internal/cost` (selain kolom), `internal/charge`, `internal/legacyar`, `internal/tax`.

## G.2 Graph

```
W-2 Numbering ─┐
W-3 INV-DOC-1 ─┤ (keras: kas wajib berdokumen)
W-4 Aging ─────┤ (aturan bucket dipakai ulang)
W-5 Approval ──┤ (gate + audit trail)
W-10 Expense ──┘ (taksonomi & INV-EXP-2)
        │
        v
   ┌─────────┐
   │  W-11   │  vendor · kewajiban · pembayaran · alokasi · retensi · uang muka
   └────┬────┘
        │
   ┌────┴─────┬───────────┬────────────┬───────────┐
   v          v           v            v           v
AP Aging  Rekonsiliasi  Seam       W-12 Bank   W-13 Fixed
          AP↔GL         potongan   Reconcile   Asset
                                   (kelengkapan) (seam kas non-beban)

Realisasi RAB ← konsumen OTOMATIS, nol perubahan kode (D-5)
```

W-12 dan W-13 paralel; keduanya hanya butuh W-11.

---

# H. RISIKO YANG MASIH TERSISA

## H.1 ✅ KEPUTUSAN TERBUKA — SUDAH DIJAWAB PEMILIK (2026-08-14)

| # | Keputusan | Jawaban pemilik | Terkunci sebagai |
|---|---|---|---|
| **O-1** | Ruang lingkup pajak P0 | **PPN Masukan saja**; PPh dipotong ditunda | **D-16**, **D-17** |
| **O-2** | Withholding PPh | Ditunda — seam kosong, tanpa akun 2-1200 | **D-17** |
| **O-3** | Lebih bayar vendor | **Tolak default + aksi eksplisit** "catat sebagai uang muka" | **D-18** |
| **O-4** | Gate approval | **OPT-IN**, mengikuti pola existing | **D-19** |
| **R-1** | Asimetri reversal | Diangkat jadi **W-11b** terpisah, termasuk perbaikan reader | **D-20** |

**Tidak ada keputusan akuntansi yang tersisa untuk W-11.**

## H.2 Risiko teknis yang teridentifikasi

| # | Risiko | Dampak | Mitigasi | Status |
|---|---|---|---|---|
| **R-1** | **Asimetri reversal antar reader.** `GetRealisasiPerItem` menjumlahkan `cost_entries.amount` dan **tidak mengenal netting reversal**; `ActualCostByCode` mengenalnya. Setelah H1, realisasi proyek = 0 tapi per item tetap terisi | Dua laporan realisasi berselisih setelah pembalikan | **Dikeluarkan dari W-11 → W-11b** (D-20). W-11 tidak menambah kolom apa pun dan tidak menyentuh reader | ✅ **Ditutup untuk W-11.** Lihat H.3 |
| **R-2** | `journal_entries.source` = `varchar(20)`; `ap_retention_release` tepat 20 karakter | Pemotongan senyap | Dipakai `ap_retention` (12 karakter) | ✅ Ditutup di B.5 |
| **R-3** | Uang muka (F) tidak punya kewajiban induk, sehingga tidak masuk INV-AP-1 | Uang muka bisa "hilang" dari rekonsiliasi | Laporan terpisah "uang muka belum terkompensasi" + saldo 1-5300 sebagai kontrol | ✅ Ditutup di E.4 |
| **R-4** | Pembalikan kewajiban yang uang mukanya sudah dikompensasi (G) mengembalikan uang muka ke posisi belum terpakai | Perlu pembalikan alokasi `advance` juga | Aturan urutan H2→H1 diperluas mencakup alokasi `advance`; diuji INV-AP-6 | ✅ Tercakup |
| **R-5** | `cmd/demo-seed` memanggil `CreateCostEntry` langsung dan **melewati** guard transport | Tenant demo baru tetap melahirkan payable tak terlunasi | Perbaiki seed (perubahan kode, bukan migrasi); 4 baris existing tidak disentuh | ⚠️ Terbuka, non-blocking |
| **R-6** | 3 test integration `internal/legacyar` gagal sejak sebelum W-11 (F-4) | Regresi tidak sepenuhnya hijau | **Dilarang diperbaiki di W-11** oleh pemilik. Jalankan dengan `-p 1`; catat sebagai pre-existing | ⚠️ Diketahui, di luar scope |
| **R-7** | Vendor bebas ditulis di `cost_entries.vendor` untuk baris non-AP (jalur bank) | Dua ejaan vendor yang sama tetap mungkin | Di luar scope W-11; normalisasi = P2 | ⚠️ Diterima |
| **R-8** | Satu kewajiban lintas proyek dimungkinkan (`LineInput.ProjectID` per baris) | Kompleksitas UI & validasi tier per baris | Didukung skema; UI P0 boleh membatasi satu proyek per kewajiban | ℹ️ Catatan desain |
| **R-9** | **PPN atas uang muka vendor PKP belum dikonfirmasi.** Bila vendor menerbitkan faktur pajak atas uang muka, PPN semestinya diakui saat itu, bukan saat termin | Waktu pengakuan PPN masukan bisa bergeser satu periode | Default P0 tercatat eksplisit di C.PPN + `TODO(tax-advisor)`. **Tidak ditebak** | ⚠️ Terbuka — perlu penasihat pajak sebelum dipakai dengan vendor PKP yang memberi faktur uang muka |
| **R-10** | PPh dipotong ditunda (D-17) | Bila kantor pajak mempersoalkan kewajiban pemotongan, pencatatan dilakukan di luar sistem sementara | Seam disiapkan; pengaktifan = tambah 1 akun + 1 baris kredit, tanpa perubahan skema | ℹ️ Diterima secara sadar |

## H.3 Ruang lingkup W-11b (dikeluarkan dari W-11 oleh D-20)

Pekerjaan terpisah, direview tersendiri, **mencabut D-5 khusus untuk lingkupnya**:

1. Buat penanda pembalikan pada `cost_entries` (`reversed_at` atau setara).
2. Selaraskan `GetRealisasiPerItem` dengan semantik netting `ActualCostByCode`, sehingga kedua reader realisasi memberi jawaban yang sama setelah pembalikan.
3. Audit data existing: `cost_entries` yang jurnalnya sudah dibalik sebelum W-11 (bukan hanya dari AP) — berapa banyak, tenant mana, seberapa besar selisihnya hari ini.
4. Regresi: realisasi proyek == Σ realisasi per item, sebelum **dan** sesudah pembalikan.

**Selama W-11b belum jalan**, drill-down item RAB wajib memberi peringatan bila proyeknya punya jurnal biaya yang dibalik — supaya selisihnya terlihat, bukan senyap.

---

# I. KESIAPAN CODING

## Sudah terkunci dan siap

| Area | Status |
|---|---|
| Titik pengakuan & siklus draft→approve→post | ✅ D-1, D-2, D-3 — dipetakan penuh ke fungsi existing |
| Sumber biaya (`cost_entries`) | ✅ D-4 — hard constraint, dijaga INV-AP-8 & INV-AP-11 |
| Reader realisasi tidak diubah | ✅ D-5 |
| Model retensi | ✅ D-6 — akun terpisah |
| Vendor master | ✅ D-7 |
| Uang muka vendor | ✅ D-8 — P0 |
| Guard periode, reversal, dokumen, status turunan, aging, payable warisan | ✅ D-9 … D-15 |
| Ruang lingkup pajak (PPN masuk, PPh ditunda) | ✅ D-16, D-17 |
| Kebijakan lebih bayar | ✅ D-18 |
| Gate approval OPT-IN | ✅ D-19 |
| Asimetri reversal dikeluarkan ke W-11b | ✅ D-20 |
| COA baru wajib | ✅ 2 akun (2-1100, 1-5300) dengan alasan penolakan per kandidat |
| Journal matrix A–J + varian PKP | ✅ 10 skenario × 6 dimensi, plus B′/D′/E1′/G′ |
| Data model | ✅ 4 tabel + 2 kolom nullable, backfill nol |
| API/workflow plan | ✅ 14 endpoint P0 |
| Invariant & test matrix | ✅ 19 invariant, seluruh 10 permintaan item 8 tercakup |
| Dependency map | ✅ 27 titik pakai-ulang; hanya 3 perubahan aditif pada package existing |

## ✅ SIAP CODING

Tidak ada keputusan akuntansi yang tersisa. Seluruh pertanyaan terbuka sudah dijawab pemilik dan dikunci sebagai D-16 … D-20.

**Batas yang mengikat implementasi:**

| | |
|---|---|
| Akun COA baru | **tepat 2** — 2-1100, 1-5300. Tidak lebih |
| Tabel baru | **tepat 4** — vendors, ap_invoices, ap_payments, ap_payment_allocations |
| Kolom baru di tabel existing | **tepat 2**, nullable, backfill nol |
| Jenis dokumen baru | **0** |
| Perubahan pada package existing | **3, semuanya aditif** — ekspor `BucketFor`, 2 role baru, 2 kolom |
| Package yang **tidak boleh** disentuh | `internal/budget`, `internal/ledger/actual_cost.go`, `internal/charge`, `internal/legacyar`, `internal/tax` |
| Data yang dimigrasikan | **nol baris** |

**Urutan implementasi yang disarankan:** migrasi (tabel + 2 akun + 2 kolom) → domain & repository vendor/AP → service pengakuan (A, B) → service pembayaran + alokasi (C, D) → retensi (E) → uang muka (F, G) → reversal (H) → laporan aging → frontend.

**Gerbang selesai:** `go test ./...` hijau · integration `-p 1` · INV-AP-1..19 berwarna hijau · verifikasi browser di `http://localhost:PORT` dengan bukti `ss -ltn` menunjukkan `127.0.0.1:PORT` — bukan `tsc` saja.

---

**Akhir dokumen.**
**Tidak dibuat pada tahap ini: migration · seed · API · frontend · tabel · kode Go/TS.**
**Menunggu perintah "mulai W-11" dari pemilik.**
