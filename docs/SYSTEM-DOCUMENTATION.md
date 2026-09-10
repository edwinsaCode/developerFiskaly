# Dokumentasi Sistem — PT Nata Alam Raya ERP / Property Accounting

**Status: dokumentasi final berdasarkan implementasi aktual per 2026-09-10 (commit "first commit", branch `main`).**
Ditulis dari pembacaan langsung source code (`backend/internal/**`, migrasi, live database schema, dan COA
live 53 akun), bukan dari asumsi desain lama. Setiap klaim di dokumen ini tertaut ke file:baris kode aktual.

**Audiens**: Accounting/Finance, Admin/Operasional, Management/Client, Developer/Technical.

**Prinsip penulisan dokumen ini**: bila rule bisnis di implementasi berbeda dari asumsi umum/desain lama,
dokumen ini mengikuti **implementasi aktual** dan menandai perbedaannya secara eksplisit (lihat Bagian 0.2) —
tidak ada rule yang didiamkan atau dikarang.

---

## Daftar Isi

- [Bagian 0 — Aturan Final & Aturan yang Sudah Dicabut (WAJIB dibaca duluan)](#bagian-0)
  - [0.1 Aturan Final (Frozen Rules)](#01-aturan-final-frozen-rules)
  - [0.2 Discrepancy: Implementasi vs Asumsi/Dokumentasi Lama](#02-discrepancy-implementasi-vs-asumsidokumentasi-lama)
- [1. System Overview](#1-system-overview)
- [2. Accounting Foundation](#2-accounting-foundation)
- [3. Persediaan & HPP Property](#3-persediaan--hpp-property)
- [4. RAB vs Realisasi](#4-rab-vs-realisasi)
- [5. Project → Unit → HPP Flow](#5-project--unit--hpp-flow)
- [6. Sales Flow](#6-sales-flow)
- [7. Kelebihan Tanah](#7-kelebihan-tanah)
- [8. Tax (PPh Final & PPN)](#8-tax-pph-final--ppn)
- [9. Expense / Cost Entry](#9-expense--cost-entry)
- [10. Bank / Cash / Payment](#10-bank--cash--payment)
- [11. Commission](#11-commission)
- [12. Fixed Asset & Depreciation](#12-fixed-asset--depreciation)
- [13. Financial Reports](#13-financial-reports)
- [14. End-to-End User Manual (A–T)](#14-end-to-end-user-manual-at)
- [15. Accounting Journal Reference](#15-accounting-journal-reference)
- [16. Troubleshooting & Business Rules](#16-troubleshooting--business-rules)
- [17. Technical Architecture](#17-technical-architecture)
- [18. Glossary](#18-glossary)
- [Appendix A — Full Chart of Accounts (live, 53 akun)](#appendix-a--full-chart-of-accounts-live-53-akun)

---

<a id="bagian-0"></a>
## Bagian 0 — Aturan Final & Aturan yang Sudah Dicabut

Bagian ini diletakkan di paling depan **secara sengaja**: agar developer baru, auditor, atau siapa pun yang
membaca cepat tidak menghidupkan kembali rule lama yang sudah sengaja dicabut oleh klien.

### 0.1 Aturan Final (Frozen Rules)

| # | Area | ATURAN AKTIF SAAT INI | ATURAN LAMA YANG DICABUT |
|---|---|---|---|
| F-1 | Alokasi HPP Tanah | Proporsional terhadap `land_area` unit (`ComputeLandPool`, `internal/allocation/land_pool.go`) — largest-remainder, rekonsiliasi persis (Invariant #3) | Pembagian rata/50-50 antar unit — **dicabut UAT 2026-09-07 ("Item 9")** |
| F-2 | Cakupan kapitalisasi HPP | **HANYA** `land` + `hard` (Konstruksi) yang masuk Persediaan (1-3000 / 1-3100). `soft` → beban (5-4700), `operational` → beban (5-4600), `marketing` → beban (5-3xxx), `other` → beban (5-4xxx) | Kapitalisasi penuh RAB (`budget_plan_capitalization`) — **ditambahkan migrasi 000098, lalu dicabut eksplisit migrasi 000102** — bukti kode bahwa rule ini pernah ada lalu sengaja dihapus |
| F-3 | Dasar pengakuan HPP saat Akad/BAST | **Budgeted-first**: memakai pool RAB yang **approved** (`BudgetedHPPResolver`/`ComputeBudgeted`) selama proyek punya RAB aktif — bukan biaya aktual yang sudah terpakai saat itu. Selisih direkonsiliasi belakangan lewat **True-Up** (`internal/closing`, lihat §3.6) saat proyek selesai/finalized. Ini pelaksanaan dua-tahap dari Invariant #4 (HPP = biaya terakumulasi), bukan pelanggarannya. | — (lihat catatan discrepancy 0.2-B; ini bukan rule lama yang dicabut, melainkan koreksi asumsi awal task ini) |
| F-4 | Kelebihan Tanah — model produk | Modul standalone `internal/land` (`LandStock`), HPP = `PurchasePrice`/m² yang di-input admin langsung (bukan hasil pool), harga jual = `UnitPrice`/m² independen | Desain lama: Kelebihan Tanah sebagai item "addon" bebas-ketik yang mendarat di `2-2400 Titipan Realisasi` (akun kewajiban) — **kode mati, diblokir fail-closed** (`ErrProductNotAddon`) |
| F-5 | Booking fee | `booking_fee >= 0` — Rp0 valid (tanpa jurnal/kwitansi/termin). Fee > 0 default → `4-2100 Pendapatan Booking` langsung, final, tidak pernah direfund/direklas | — (perluasan aturan, bukan pencabutan; jalur `refundable`→`2-2100 Titipan Booking` legacy tetap hidup untuk kasus yang ditandai eksplisit) |
| F-6 | Dana Jaminan Bank (KPR) | Split dua arah **saat Akad** (`min(bankApproved, piutangTotal)` → `1-2200`, sisa → `1-2000`); setiap tahap pencairan KPR mengkredit `1-2200` langsung (dibatasi saldo tersisa) | Reklas otomatis saat status berubah ke `disbursed` — **bug lama yang sudah dicabut**, dikonfirmasi komentar kode eksplisit di `internal/scheme/policies.go` dan `internal/sale/scheme_flow.go` |
| F-7 | Konstruksi — subkategori | 4 nilai taksonomi (`produksi_subsidi`, `produksi_komersial`, `sarana_prasarana`, `perizinan`), tapi **alokasi** memecahnya jadi **3 pool**: ProduksiSubsidi (khusus unit subsidi), ProduksiKomersial (khusus unit komersial), General (gabungan `sarana_prasarana`+`perizinan`+baris legacy, dialokasikan ke SEMUA unit HPP-eligible) | — |
| F-8 | Satu BudgetPlan aktif | Satu `active` per proyek/fase; versi baru disetujui → versi lama otomatis `superseded` (bukan dihapus) | — (CLAUDE.md Invariant #8, tidak ada discrepancy ditemukan) |

### 0.2 Discrepancy: Implementasi vs Asumsi/Dokumentasi Lama

Ditandai eksplisit sesuai instruksi task ini — jangan didiamkan.

**A. Isolasi tenant (CLAUDE.md vs kode).** CLAUDE.md menyatakan mekanismenya "GORM global scope." Kode aktual
memakai **filter manual eksplisit `tenant_id = ?` di setiap repository query** — bukan GORM global scope.
Secara fungsional keduanya sama-sama mencegah kebocoran lintas tenant (dibuktikan oleh integration test lintas
banyak package), tapi mekanismenya berbeda dari yang tertulis. Lihat §17.5.

**B. Dasar HPP saat Akad — budgeted, bukan actual-langsung.** Prompt awal task ini mengasumsikan "Konstruksi:
berdasarkan ACTUAL posted cost, BUKAN full RAB." Implementasi aktual memakai **RAB approved (budgeted pool)**
sebagai dasar HPP saat Akad selama RAB aktif ada — bukan biaya aktual yang sudah terposting saat itu juga.
Ini BUKAN kembalinya rule "full-RAB capitalization" yang sudah dicabut (F-2) — bedanya: (1) hanya `land`+`hard`
yang dipakai, tidak seluruh RAB; (2) ada mekanisme **True-Up** yang secara eksplisit merekonsiliasi ke biaya
aktual saat proyek selesai (`internal/closing`, TrueupRun draft→calculated→approved→posted, lihat §3.6). Desain
ini adalah pelaksanaan "budget now, true-up later" yang selaras PSAK 44 dan sudah disetujui klien — bukan
pelanggaran Invariant #4, melainkan cara dua-tahap untuk memenuhinya.

**C. Komentar kode basi (stale comment).** `internal/scheme/policies.go` masih menyisakan komentar yang
mendeskripsikan perilaku reklas-otomatis-saat-disbursed yang sudah dicabut (lihat F-6) — komentar ini
membingungkan bila dibaca terpisah dari `scheme_flow.go` yang sudah diperbaiki (Item 7C). Dokumentasi ini
mengikuti kode yang berjalan (`scheme_flow.go:897-942`), bukan komentar basi tersebut.

**D. Test suite — beberapa integration test RED per 2026-09-10.** Hasil full regression run sesi ini
(`go test -tags integration ./... -p 1`) menunjukkan beberapa test gagal. Rinciannya di §16.9 — mayoritas
adalah **utang teknis pada fixture test** (belum menyertakan field `hard_subcategory` yang diwajibkan migrasi
000103), BUKAN bug bisnis; satu test (`TestIntegration_Refund_FromBooking`) tampak seperti **defect nyata** di
jalur pembatalan booking *refundable* (bukan jalur default `recognized`) dan perlu investigasi terpisah sebelum
jalur itu diklaim berfungsi.

**E. Akun Persediaan Soft Cost/Financing (1-3200/1-3300) — legacy, mati.** COA live masih menyimpan kedua akun
ini agar histori jurnal lama tetap terbaca, tapi **tidak ada kode produksi yang menulis ke sana lagi** sejak
freeze 2026-09-04 (F-2). Jangan dokumentasikan sebagai jalur kapitalisasi aktif.

---

<a id="1-system-overview"></a>
## 1. System Overview

### 1.1 Tujuan sistem

Sistem akuntansi & ERP internal untuk pengembang perumahan (PT Nata Alam Raya, nama teknis `esaproperti`) yang
menggantikan pencatatan manual/Jurnal.id dengan satu mesin akuntansi property-aware: RAB & realisasi biaya,
alokasi HPP tanah+konstruksi per unit, siklus penjualan (booking→Akad→KPR→pelunasan), piutang & penagihan,
pajak PPh Final, komisi, aset tetap, dan laporan keuangan — semua bersumber dari **satu ledger** (`internal/ledger`).

### 1.2 Modul utama & hubungan antar modul

```
                              ┌────────────────────┐
                              │   internal/ledger   │  ← SATU-SATUNYA mesin posting jurnal
                              │  (COA, posting,      │    (PostingService: Create/Post/Reverse)
                              │   AccountRoleRegistry│
                              │   trial balance)      │
                              └─────────▲────────────┘
                                        │ semua modul di bawah memanggil ledger.PostingService
        ┌───────────────┬──────────────┼──────────────┬───────────────┬───────────────┐
        │               │              │               │               │               │
   internal/budget  internal/cost  internal/sale   internal/land   internal/ap    internal/commission
   (RAB)             (realisasi)   (booking/Akad/   (Kelebihan      (Hutang        (komisi sales/
                                    KPR/termin)       Tanah)          Usaha)         admin marketing)
        │               │              │               │
        └───────┬───────┴──────────────┴───────────────┘
                │
         internal/allocation   ← menghitung alokasi HPP per unit (land pool + hard pool)
                │
         internal/receivable   ← SATU mesin aging piutang (house/realization/addon/legacy)
                │
         internal/reporting    ← Neraca, L/R, Arus Kas, Neraca Saldo, AR Aging, Pipeline, Dashboard
                │
         internal/closing      ← (nama menyesatkan) sebenarnya HPP True-Up + Project Completion,
                                  BUKAN tutup buku bulanan/tahunan
```

Penutupan periode punya **tiga konsep berbeda** yang sering tertukar — lihat §2.9 untuk disambiguasi penuh.

### 1.3 Source of truth akuntansi

- **Ledger (`journal_entries`+`journal_lines`) adalah satu-satunya sumber kebenaran finansial.** Semua saldo
  (Neraca, L/R, Persediaan, HPP per unit) dihitung ulang dari baris jurnal ber-status `posted` — tidak ada
  kolom saldo yang "dipercaya" independen dari ledger.
- Klasifikasi akun ke Pendapatan/HPP/Beban Operasional/Pendapatan-Beban Luar Usaha/Pajak dipusatkan di **satu**
  registry: `internal/ledger/account_role.go` (`AccountRoleRegistry`) — didokumentasikan di kode sebagai
  rujukan kanonik `docs/financial-canonical-registry.md §R-1`. Ini mencegah dua laporan menghitung "Pendapatan"
  dengan definisi berbeda.
- `internal/receivable` adalah satu-satunya mesin agregasi piutang (house/realization/addon/legacy) — dipakai
  ulang oleh `reporting`, `billing`, `legacyar`, `land`.

### 1.4 Isolasi tenant

Setiap baris tabel bisnis memiliki `tenant_id`. **Implementasi aktual**: setiap repository memfilter eksplisit
`WHERE tenant_id = ?` (bukan GORM global scope seperti tertulis di CLAUDE.md — lihat discrepancy 0.2-A).
Dibuktikan benar oleh integration test yang memverifikasi query lintas-tenant mengembalikan nol hasil.

### 1.5 Ledger append-only, reversal, idempotency, atomic transaction

- **Append-only**: jurnal yang sudah `posted` tidak pernah di-`UPDATE`/`DELETE`. Satu-satunya koreksi sah adalah
  **jurnal pembalik** via `ledger.PostingService.Reverse(...)` — dipakai konsisten di seluruh modul (Akad,
  cancellation, Kelebihan Tanah, tax, true-up).
- **Idempotency**: kunci idempotensi bersifat **per-alur**, bukan global satu tabel — mis. `ReceivePayment`
  mengecek idempotency key di awal sebelum resolusi apa pun; key yang berulang mengembalikan hasil termin yang
  sudah ada, bukan membuat duplikat.
- **Atomic transaction**: pola standar `db.Transaction(func(tx *gorm.DB) error {...})` dipakai di seluruh alur
  yang menyentuh >1 tabel (mis. Akad rumah membundel Kelebihan Tanah dalam **satu** `*gorm.DB` transaksi yang
  sama — bukan saga/eventual consistency).
- **Dokumen bernomor wajib untuk setiap pergerakan kas** (INV-DOC-1): jurnal yang menyentuh akun kas/bank harus
  mendapat nomor dokumen dalam transaksi yang SAMA dengan posting-nya, atau seluruh jurnal batal (rollback).
  11 jenis dokumen: `KWT/KWB/KWR/MTI/INV/KWD/BKM/BKK/BTP/RFC/JR`, format `{prefix}/{tahun}/{nomor 6 digit}`,
  reset tahunan. Lihat §10.5.

---

<a id="2-accounting-foundation"></a>
## 2. Accounting Foundation

### 2.1 Chart of Accounts (COA)

53 akun live, dikelompokkan Aset (1-x), Kewajiban (2-x), Ekuitas (3-x), Pendapatan (4-x), Beban (5-x). Daftar
lengkap di [Appendix A](#appendix-a--full-chart-of-accounts-live-53-akun). **Setiap kode akun di dokumen ini
diambil langsung dari COA live — tidak ada kode yang dikarang.**

### 2.2 Debit/Kredit & Journal Entry

Setiap `JournalEntry` memiliki satu atau lebih `JournalLine`. Setiap `JournalLine` punya kolom `Debit` DAN
`Credit` terpisah (bukan satu kolom signed) — satu baris normalnya hanya mengisi salah satu, nol di sisi lain.
**Invariant #1 (tidak bisa ditawar)**: `Σ Debit == Σ Credit` per entry, diperiksa di level aplikasi oleh
`PostingService` sebelum entry boleh berstatus `posted`. Entry yang tidak balance ditolak — tanpa pengecualian.

### 2.3 Posting

`internal/ledger/posting_service.go` adalah **satu-satunya** pintu masuk untuk menulis jurnal ke seluruh sistem
— seluruh modul lain (sale, land, cost, ap, commission, fixedasset, tax) memanggil fungsi yang sama, tidak ada
jalur alternatif menulis jurnal. Metode inti:
- `Create` — bangun entry draft, validasi balance (`validateLines`).
- `Post` — pindahkan draft → `posted` (immutable setelah ini).
- `CreateAndPost` — gabungan, dipakai kebanyakan alur (Akad, cost entry, commission).
- `PostDraft` — posting entry yang sebelumnya dibuat sebagai draft.
- `Reverse` — satu-satunya jalur koreksi sah, membuat entry pembalik baru bertaut ke entry asal.

### 2.4 Accounting date vs system timestamp

`JournalEntry` menyimpan `EntryDate` (tanggal akuntansi — dipilih user/proses bisnis, mis. tanggal Akad) secara
terpisah dari `CreatedAt` (timestamp sistem saat baris dibuat). Laporan (Neraca, L/R, Trial Balance) selalu
memfilter berdasar `EntryDate`, bukan `CreatedAt` — penting untuk entry yang diposting mundur (backdated) dalam
periode yang masih terbuka.

### 2.5 Period / Periode Tertutup

`AccountingPeriod` (bulanan) — status terkunci/terbuka. Periode terkunci **tidak menghasilkan jurnal apa pun**
(bukan jurnal penutup) — ia hanya **memblokir** posting baru dengan `EntryDate` jatuh di bulan tersebut. Dicek
oleh `WithPeriodChecker` yang dipasang pada hampir setiap `PostingService` di seluruh modul (termasuk true-up,
commission). Ini konsep BERBEDA dari tutup buku tahunan (§2.9).

### 2.6 Piutang (AR)

Satu mesin: `internal/receivable`. Empat sumber piutang berbeda **bermuara ke satu daftar** (desain eksplisit:
"seorang customer berutang satu jumlah, walau tagihannya lahir dari dua proses berbeda"):

| Source | Asal | Akun terkait |
|---|---|---|
| `house` | cicilan rumah (`payment_schedules`) | `1-2000` Piutang Usaha / `1-2200` Dana Jaminan Bank |
| `realization` | tagihan biaya realisasi (`charge_items`) | `2-2400` Titipan Realisasi → `1-2000` saat invoice |
| `addon` | produk tambahan termasuk **Kelebihan Tanah** — pendapatan, BUKAN titipan | `1-2000` |
| `legacy` | saldo piutang proyek lama yang diimpor tanpa jurnal | `1-2000` (via mesin yang sama) |

Aging bucket (sama persis dengan aging Hutang/AP — desain lintas-pakai W-11): **Current** (≤0 hari),
**1–30**, **31–60**, **61–90**, **90+** hari lewat jatuh tempo. Status per baris dihitung di backend:
`scheduled` / `due_today` / `overdue` / `paid`.

### 2.7 Kas/Bank

5 akun kas/bank live: `1-1100` Kas Besar, `1-1200` Petty Cash, `1-1300` Bank BCA, `1-1400` Bank Mandiri,
`1-1500` Bank BRI. Dipilih di form manapun via selector `CashBankSelect` yang membaca daftar dari endpoint
`GET /accounts/cash-bank` (sumber: `accounts.category IN ('cash','bank')` — bukan daftar hardcode di frontend).

### 2.8 Dana Jaminan Bank (KPR) — `1-2200`

Akun aset yang mewakili **plafon KPR yang sudah disetujui bank tapi belum sepenuhnya dicairkan**, dipakai
khusus kontrak berskema KPR. Siklus lengkap Dr/Cr ada di §6.6c–6.8 (bagian Sales Flow) karena mekanismenya
menyatu dengan Akad dan pencairan bertahap.

### 2.9 Tiga konsep "closing" yang SERING TERTUKAR — disambiguasi wajib

| Konsep | Package | Yang sebenarnya terjadi | Jurnal? |
|---|---|---|---|
| **Periode Akuntansi (bulanan)** | `ledger.AccountingPeriod` | Kunci/buka bulan — memblokir posting baru di bulan terkunci | **Tidak ada** |
| **Tutup Buku Tahunan (fiscal year close)** | `ledger.ClosingService` | Menutup saldo Pendapatan+Beban ke `3-3000`, lalu `3-3000`→`3-2000` | **Ya, 2 jurnal** (lihat contoh di bawah) |
| **`internal/closing`** (nama package menyesatkan) | `internal/closing` | Sebenarnya adalah **HPP True-Up & Project Completion** — merekonsiliasi HPP budgeted ke HPP aktual saat proyek selesai (§3.6) | **Ya, kondisional** (1 jurnal per run, hanya jika ada varian) |

**Contoh angka — Tutup Buku Tahunan** (`internal/ledger/closing.go`):
Misal tahun berjalan: total Pendapatan = Rp1.000.000.000, total HPP+Beban (semua akun 5-x) = Rp700.000.000 →
laba bersih = Rp300.000.000.

*Entry A — tutup akun nominal ke `3-3000`:*
```
Dr  4-1000 Pendapatan Penjualan Unit          1.000.000.000
    Cr  5-1000 HPP                                              500.000.000
    Cr  5-3000 Beban Pemasaran                                  100.000.000
    Cr  5-4000 Beban Umum & Administrasi                        100.000.000
    Cr  3-3000 Laba/Rugi Tahun Berjalan (selisih=laba, Cr)      300.000.000
```
(Σ Dr = 1.000.000.000 = Σ Cr — balanced)

*Entry B — tutup `3-3000` ke `3-2000` (laba → Dr 3-3000 / Cr 3-2000):*
```
Dr  3-3000 Laba/Rugi Tahun Berjalan            300.000.000
    Cr  3-2000 Laba Ditahan                                     300.000.000
```
Bila tahun berjalan justru **rugi**, arah Entry B dibalik: `Dr 3-2000 / Cr 3-3000`.

### 2.10 Reversal & Bukti/Receipt

Reversal: satu-satunya koreksi sah (Invariant #5) — dipakai identik di Akad-cancel, land-sale-cancel, tax
obligation cancel, true-up (implisit via jurnal koreksi berlawanan, bukan `Reverse()` literal — lihat §3.6).

Bukti (Receipt/Kwitansi): setiap pergerakan kas WAJIB satu dokumen bernomor dalam transaksi yang sama
(INV-DOC-1, §1.5). Nomor dokumen tidak pernah "diterbitkan otomatis tanpa nomor" — kegagalan penerbitan
nomor membatalkan seluruh transaksi kas terkait (fail-closed).

---

<a id="3-persediaan--hpp-property"></a>
## 3. Persediaan & HPP Property

Ini bagian paling sensitif secara bisnis — baca §0.1 (F-2, F-3, F-7) dan §0.2-B sebelum lanjut.

### 3.1 Aturan final: HPP = Tanah + Konstruksi SAJA

`internal/domain/cost_category.go` — `AllCostCategories = [land, hard]`. **Hanya dua kategori ini yang masuk
Persediaan Real Estat dan pada akhirnya menjadi HPP saat unit dijual.** Empat kategori lain (`soft`,
`marketing`, `other`, `operational`) SELALU beban periode berjalan — tidak pernah menyentuh akun Persediaan:

| CostCategory | Kapitalisasi? | Akun tujuan |
|---|---|---|
| `land` | **Ya — Persediaan** | `1-3000` Persediaan Real Estat — Tanah |
| `hard` (Konstruksi) | **Ya — Persediaan** | `1-3100` Persediaan Real Estat — Hard Cost |
| `soft` | Tidak — beban | `5-4700` Beban Soft Cost (Desain & Legal) |
| `marketing` | Tidak — beban | `5-3xxx` Beban Pemasaran |
| `other` | Tidak — beban | `5-4xxx` Beban Lain-lain (mapping via Expense Type, lihat §9) |
| `operational` | Tidak — beban | `5-4600` Beban Operasional |

`1-3200` (Persediaan — Soft Cost) dan `1-3300` (Persediaan — Biaya Pembiayaan) **masih ada di COA live tapi
mati** — dipertahankan hanya agar jurnal historis pra-2026-09-04 tetap terbaca. Tidak ada kode produksi yang
menulis ke sana lagi.

### 3.2 CostTier — direct / shared / overhead

`internal/domain/cost_tier.go`, dengan matriks kompatibilitas kategori yang ditegakkan ketat:

| CostTier | `unit_id` | Kategori yang diizinkan | Efek |
|---|---|---|---|
| `direct` | wajib diisi | `land` \| `hard` | Dr langsung ke Persediaan unit tsb |
| `shared` | wajib NULL | `land` \| `hard` | masuk pool project-wide, dialokasikan lewat `internal/allocation` |
| `overhead` | (tidak relevan) | `marketing` \| `other` \| `operational` \| `soft` | Dr Beban langsung, tidak pernah dikapitalisasi |

### 3.3 Alokasi Tanah — contoh angka lengkap

Basis alokasi Tanah **selalu** `land_area` per unit (Item 9, UAT 2026-09-07) — tidak pernah basis lain, dan
tidak pernah rata/50-50 (rule lama yang dicabut, F-1).

**Kasus**: Proyek "Grand Asri" — pool biaya tanah project-wide (shared) = **Rp900.000.000**. Ada satu stok
Kelebihan Tanah: `TotalQuantityM2 = 200`, `PurchasePrice = Rp500.000/m²`.

1. **Carve-out tetap** untuk Kelebihan Tanah (fixed, BUKAN proporsional):
   `200 m² × Rp500.000 = Rp100.000.000` — dikurangi LEBIH DULU dari pool.
2. **Sisa pool** untuk unit = `900.000.000 − 100.000.000 = Rp800.000.000`.
3. Tiga unit dengan `land_area`: Unit A = 100 m², Unit B = 150 m², Unit C = 150 m² (total 400 m²).
   - Unit A: `800.000.000 × 100/400 = Rp200.000.000`
   - Unit B: `800.000.000 × 150/400 = Rp300.000.000`
   - Unit C: `800.000.000 × 150/400 = Rp300.000.000`
   - Kontrol: `200jt + 300jt + 300jt = 800jt` — persis, tidak ada sisa (Invariant #3).

**Guard fail-closed**: bila carve-out Kelebihan Tanah > pool total → `ErrLandStockCostExceedsPool` (ditolak,
bukan diam-diam jadi negatif). Bila ada unit dengan `land_area <= 0` → `ErrLandAreaMissing` (ditolak). Bila
sisa pool > 0 tapi tidak ada satu pun unit penerima → `ErrNoLandPoolRecipient`.

**Guard anti-dobel-hitung vs Kelebihan Tanah**: karena carve-out Kelebihan Tanah dikurangi LEBIH DULU (jumlah
tetap, bukan porsi), rupiah yang sama tidak pernah muncul sekaligus di HPP unit DAN di HPP Kelebihan Tanah —
keduanya mempartisi pool yang sama, tidak berdua-duanya menarik dari pool penuh.

**Catatan pembulatan**: bila pembagian tidak habis (mis. Rp100 dibagi 3 bobot sama → 33,33,33 sisa 1), sisa
rupiah dialokasikan ke peserta dengan sisa desimal terbesar (`Money.Allocate`, largest-remainder deterministic)
— bukan dibuang, bukan random.

### 3.4 Alokasi Konstruksi (Hard) — 3 pool, bukan 4

Taksonomi `ConstructionSubcategory` punya 4 nilai (`produksi_subsidi`, `produksi_komersial`,
`sarana_prasarana`, `perizinan`), **wajib diisi** untuk `CostTier=shared` (opsional untuk `direct`). Tapi
mesin alokasi (`internal/allocation/hard_pool.go`, `ComputeHardPool`) memecahnya jadi **3 pool alokasi**:

- **Pool ProduksiSubsidi** — HANYA dialokasikan ke unit yang `TaxCategory == subsidi`, proporsional bobot
  (`saleable_area` atau `sales_value`, tergantung basis alokasi proyek — lihat §3.5).
- **Pool ProduksiKomersial** — HANYA ke unit `TaxCategory == komersial`, proporsional bobot.
- **Pool General** — gabungan `sarana_prasarana` + `perizinan` + baris legacy belum berklasifikasi — dialokasikan
  ke **SEMUA** unit HPP-eligible tanpa memandang subsidi/komersial (perilaku alokasi lama, tidak berubah).

**Guard fail-closed**: bila `ProduksiSubsidi > 0` tapi tidak ada satu pun unit subsidi di proyek tsb →
`ErrHardSubpoolNoSubsidiUnit`. Simetris untuk komersial.

**Contoh angka**: Proyek dengan 2 unit subsidi (D, E) dan 1 unit komersial (F), basis alokasi =
`saleable_area`. Biaya Konstruksi shared project-wide sudah dipecah per subkategori:
`produksi_subsidi = Rp200.000.000`, `produksi_komersial = Rp150.000.000`,
`sarana_prasarana + perizinan (General) = Rp90.000.000`.

`saleable_area`: D=36m², E=45m², F=54m² (total 135m²).

- Pool ProduksiSubsidi (Rp200jt) dibagi HANYA antara D & E, proporsional `saleable_area` (36:45):
  D = `200jt × 36/81 = Rp88.888.889`, E = `200jt × 45/81 = Rp111.111.111` (rekonsiliasi persis via
  largest-remainder, total = 200jt).
- Pool ProduksiKomersial (Rp150jt) → 100% ke F (satu-satunya unit komersial) = Rp150.000.000.
- Pool General (Rp90jt) dibagi ke SEMUA (D, E, F), proporsional `saleable_area` (36:45:54, total 135):
  D=`90jt×36/135=Rp24.000.000`, E=`90jt×45/135=Rp30.000.000`, F=`90jt×54/135=Rp36.000.000`.

**Total HPP Konstruksi per unit** (Direct + Shared):
- D = 88.888.889 + 24.000.000 = **Rp112.888.889**
- E = 111.111.111 + 30.000.000 = **Rp141.111.111**
- F = 150.000.000 + 36.000.000 = **Rp186.000.000**
- Kontrol total: 112.888.889+141.111.111+186.000.000 = **Rp440.000.000** = 200jt+150jt+90jt ✓ persis.

### 3.5 Basis alokasi (Hard/Soft/Financing — bukan Land)

`AllocationBasis` (`internal/allocation/model.go`) punya 2 nilai valid: `saleable_area` (luas jual m²) dan
`sales_value` (nilai jual). Dipilih per proyek. **Land selalu pakai `land_area`, tidak pernah ikut basis
proyek** (Item 9). Kategori `soft`/`financing` masih dihitung bobotnya di model (warisan struktur lama) tapi
sejak freeze F-2, kategori tersebut tidak lagi menghasilkan baris Persediaan — hanya `hard` yang efektif
terpengaruh basis ini pasca-freeze.

### 3.6 Kapan Persediaan menjadi HPP: resolusi 3-tingkat saat Akad/BAST

`internal/sale/service.go` (`RecordAkad`), kaskade (dari yang paling diprioritaskan):

1. `ParticipatesInHPP()` — bila produk bukan properti (mis. non-property addon) → `HPPMethodNone`, HPP nol.
2. `FinalizedUnitHPP` — bila proyek sudah **finalized** (sudah lewat True-Up, P0-4) → pakai HPP hasil
   true-up (biaya aktual final), bukan lagi budgeted.
3. `hppResolver.ResolveHPP` (**jalur normal, mayoritas kasus**) — `BudgetedHPPResolver`: memakai **pool RAB
   approved** (`ComputeBudgeted`) sebagai HPP, BUKAN biaya aktual yang sudah terposting saat itu (lihat §0.2-B).
4. `costProvider.GetUnitCost` — fallback legacy bila resolver di atas tidak tersedia.

**Jurnal HPP saat Akad** (`buildEvent4Lines`, sama transaksi dengan pengakuan pendapatan — lihat §6.6d):
```
Dr  5-1000 Harga Pokok Penjualan (HPP)     = Total HPP unit
    Cr  1-3000 Persediaan — Tanah              = porsi Land   (bila > 0)
    Cr  1-3100 Persediaan — Hard Cost          = porsi Hard   (bila > 0)
```
(Baris `1-3200`/`1-3300` hanya muncul untuk data historis pra-freeze — tidak pernah untuk transaksi baru.)

### 3.7 True-Up (rekonsiliasi Budgeted → Actual)

Package `internal/closing` (nama menyesatkan — bukan tutup buku, lihat §2.9). Siklus:
`draft → calculated → approved → posted`, dijalankan saat proyek dinyatakan selesai (Project Completion).

**Perhitungan** (`Calculate`): untuk setiap unit yang sudah `sold`, per kategori (`land`/`hard`):
`Variance = ActualCost − BudgetedHPP_yang_sudah_diakui`.

**Posting** (`Post`, `internal/closing/service.go:439-520`) — satu jurnal per run, satu pasang baris per
(unit, kategori) yang variance-nya ≠ 0:
```
Δ > 0 (aktual > budgeted, HPP kurang diakui):
    Dr  5-1000 HPP                              = Δ
        Cr  1-3xxx Persediaan (kategori terkait)    = Δ

Δ < 0 (aktual < budgeted, HPP kelebihan diakui):
    Dr  1-3xxx Persediaan (kategori terkait)    = |Δ|
        Cr  5-1000 HPP                               = |Δ|
```
Unit yang belum terjual (`unsold`) tidak menghasilkan jurnal — HPP-nya tetap "belum final" sampai unit itu
sendiri terjual. Idempoten 3 lapis: row-lock run, `journal_id` unique per run, status pinned setelah `posted`.

**Contoh angka**: Unit A diakui HPP budgeted saat Akad = Rp200.000.000 (kategori Land). Setelah proyek
selesai, biaya tanah aktual yang benar-benar terpakai untuk Unit A ternyata Rp215.000.000 → `Δ = +15.000.000`:
```
Dr  5-1000 HPP                              15.000.000
    Cr  1-3000 Persediaan — Tanah                          15.000.000
```

---

<a id="4-rab-vs-realisasi"></a>
## 4. RAB vs Realisasi

### 4.1 Membuat RAB (BudgetPlan)

`internal/budget` — satu `BudgetPlan` per proyek/fase berisi banyak `BudgetItem` (mis. per kategori/subkategori
biaya). Status: `draft → approved → superseded` (invariant #8, CLAUDE.md — satu `active` per proyek/fase,
dikonfirmasi tidak ada discrepancy).

### 4.2 Approve RAB sebagai anggaran

`ApprovePlan()` (`internal/budget/service.go`) adalah **murni transisi status** — **TIDAK ADA dampak akuntansi
sama sekali** (tidak ada jurnal). RAB yang disetujui hanyalah *anggaran* yang nantinya jadi dasar HPP budgeted
(§3.6) — persetujuan itu sendiri tidak memindahkan uang atau mengubah Persediaan.

### 4.3 Input biaya aktual & kaitkan ke RAB

Biaya aktual dicatat via `internal/cost` (Cost Entry, §9). Cost entry `direct`/`shared` yang berkategori
`land`/`hard` boleh (opsional) menyertakan `BudgetItemID` untuk menautkannya ke baris RAB tertentu — inilah
yang menjadi dasar perhitungan realisasi.

### 4.4 Realisasi = actual linked / RAB approved

```
Realisasi% = Σ(cost_entries.amount yang BudgetItemID = X) / BudgetItem[X].ApprovedAmount
```

**Contoh 1**: RAB item "Konstruksi Pondasi" disetujui Rp100.000.000. Biaya aktual yang sudah ditautkan ke item
ini sejauh ini = Rp30.000.000 → **Realisasi = 30%**.

**Contoh 2**: RAB item "Sarana Jalan" disetujui Rp50.000.000, belum ada satu pun biaya aktual ditautkan →
**Realisasi = 0%** (bukan error, bukan N/A — nol yang valid).

### 4.5 Klasifikasi biaya menentukan HPP vs Beban, bukan RAB

Penting: **RAB itu sendiri tidak menentukan apakah sebuah biaya jadi HPP atau Beban** — yang menentukan adalah
`CostCategory` pada setiap Cost Entry (§3.1). RAB `land`/`hard` yang direalisasikan → berpotensi jadi HPP (via
alokasi). RAB kategori `soft`/`marketing`/`other`/`operational` — walau tercatat di RAB sebagai rencana biaya —
saat direalisasikan tetap jadi **beban periode**, tidak pernah masuk Persediaan.

### 4.6 Alokasi tidak otomatis / bukan gate keras

`POST /projects/{id}/allocation/execute` adalah endpoint **eksplisit** (dipicu admin) — alokasi HPP TIDAK
otomatis berjalan setiap kali cost entry baru masuk, dan **bukan gate wajib** sebelum unit boleh dibooking atau
dijual (karena HPP saat Akad memakai budgeted pool, §3.6 — tidak butuh hasil alokasi realisasi aktual untuk
bisa closing penjualan).

---

<a id="5-project--unit--hpp-flow"></a>
## 5. Project → Unit → HPP Flow

Alur 11 langkah dari pembuatan proyek sampai unit siap dijual, dengan prasyarat per langkah:

1. **Buat Project** — data dasar proyek, `tax_category` default (`subsidi`/`komersial`) di-set di sini.
   *Prasyarat*: tidak ada.
2. **Buat Unit/Block** — unit dengan `land_area`, `saleable_area`, `unit_type`, opsional override `tax_category`
   per unit/blok. *Prasyarat*: Project ada. `land_area` **wajib > 0** bila unit ini akan ikut alokasi Tanah
   (§3.3 guard `ErrLandAreaMissing`).
3. **Buat RAB (BudgetPlan draft)** — susun `BudgetItem` per kategori/subkategori. *Prasyarat*: Project ada
   (tidak wajib unit sudah lengkap).
4. **Approve RAB** — status `draft→approved`, tanpa jurnal. *Prasyarat*: RAB draft ada, minimal 1 item.
5. **Input Cost Entry (realisasi aktual)** — dicatat dengan `CostCategory`+`CostTier`(+`ConstructionSubcategory`
   bila hard/shared), opsional tautan `BudgetItemID`. *Prasyarat*: RAB approved tidak wajib untuk mencatat cost
   entry — realisasi bisa dicatat sebelum atau sesudah approval, tapi %realisasi hanya terhitung terhadap RAB
   yang approved.
6. **(Opsional) Jalankan Alokasi** — `POST /allocation/execute` menghasilkan `AllocationResult` per unit dari
   pool shared land+hard yang sudah terposting. *Prasyarat*: ada baris shared cost terposting.
7. **Booking unit** — fee ≥0, unit → status `booked`. *Prasyarat*: unit `available`.
8. **Konversi ke Contract / PPJB** — unit → `ppjb`. *Prasyarat*: booking aktif & belum kedaluwarsa.
9. **Akad (BAST)** — unit → `sold`; **inilah titik HPP diresolusi & diakui** (budgeted-first, §3.6), bersamaan
   dengan pengakuan pendapatan. *Prasyarat*: gate skema pembayaran terpenuhi (`CanRecognize`, §6.5) + resolusi
   produk (unit_type terdaftar di Product Catalog, fail-closed bila tidak).
10. **Serah Terima Fisik (opsional, terpisah)** — unit → `occupied`, **tanpa jurnal, tanpa resolusi HPP ulang**
    (murni catatan fisik). *Prasyarat*: unit `sold`.
11. **(Bila proyek selesai) Project Completion + True-Up** — HPP budgeted direkonsiliasi ke aktual, jurnal
    koreksi diposting bila ada varian (§3.7). *Prasyarat*: proyek dinyatakan selesai (semua unit sold/tersisa
    tidak akan menerima biaya tambahan).

"Unit siap dijual" secara status sebenarnya sudah tercapai di langkah 7 (`booked`) — langkah 8-9 adalah proses
closing penjualan, bukan prasyarat "siap dijual".

---

<a id="6-sales-flow"></a>
## 6. Sales Flow

### 6.1 Status unit (state machine)

```
available → booked → (ppjb) → sold (= Akad/BAST) → occupied (serah terima fisik, tanpa jurnal)
                    ↘ (reserved — legacy)
   + hold / blocked / maintenance (status administratif terpisah)
```
"Unit Sold" = Akad/BAST tercatat, **bukan** serah terima fisik (dua peristiwa berbeda, lihat langkah 9 vs 10 §5).

### 6.2 Booking — fee Rp0 vs fee > 0

Rule final klien: booking fee = **Pendapatan Booking (`4-2100`) saat diterima**, final, tidak pernah
direfund/direklas untuk booking baru (`FeeDisposition = recognized`).

- **Fee = 0** (migrasi `000104`): valid, murni reservasi unit. **Tidak ada termin, tidak ada jurnal, tidak ada
  kwitansi.** `fee_disposition` tetap `recognized` (nominal saja, tidak ada apa pun untuk dipegang/direfund).
  Frontend: field bank disembunyikan, tidak wajib diisi.
- **Fee > 0**, checkbox *Refundable* TIDAK dicentang (default): `Dr Kas/Bank / Cr 4-2100 Pendapatan Booking`.
- **Fee > 0**, checkbox *Refundable* dicentang (jalur legacy yang tetap hidup untuk kasus yang ditandai
  eksplisit): `Dr Kas/Bank / Cr 2-2100 Titipan Booking` (kewajiban, belum pendapatan).
- Setiap booking fee > 0 otomatis menerbitkan kwitansi (KWB) dalam transaksi yang sama (atomic, fail-closed —
  kegagalan terbit kwitansi membatalkan seluruh booking).

**Contoh angka** — booking fee Rp5.000.000, tidak refundable:
```
Dr  1-1300 Bank BCA                5.000.000
    Cr  4-2100 Pendapatan Booking                5.000.000
```

### 6.3 Booking cancellation / refund

Untuk booking `refundable` (jalur legacy): `refundNet = fee_diterima − penalti`. Bila `refundNet > 0`:
```
Dr  2-2100 Titipan Booking          refundNet
    Cr  2-2200 Hutang Refund                     refundNet
```
(pencairan refund ke buyer adalah langkah terpisah, bukan bagian jurnal pembatalan ini). Untuk booking
`recognized` (default, non-refundable): **tidak ada dampak akuntansi sama sekali** saat dibatalkan — fee sudah
final diakui sebagai pendapatan saat diterima, tidak ada saldo kewajiban tersisa untuk dibalik.

> ⚠️ **Defect diketahui (per 2026-09-10)**: `TestIntegration_Refund_FromBooking` FAIL di regression run sesi
> ini (`2-2100 = -10000000.0000, want 0.0000`). Jalur *refundable*-booking-cancellation ADA dan terpasang,
> tapi test integrasinya sendiri merah — **jangan nyatakan jalur ini terverifikasi berfungsi** sampai
> diinvestigasi. Jalur default (`recognized`, non-refundable) TIDAK terdampak temuan ini. Lihat §16.9.

### 6.4 Booking → Contract conversion

`ConvertWithContractAtomic` membundel reservasi Kelebihan Tanah (bila ada) dengan pembuatan kontrak dalam SATU
transaksi. Konversi **tidak pernah** mengurangi harga kontrak dengan jumlah fee booking yang sudah dibayar
(fee dan harga jual adalah dua hal terpisah, sesuai rule `recognized`).

### 6.5 Skema pembayaran (Payment Scheme Policy)

5 skema konkret terdaftar: **Tunai Keras** (kode 8999), **Tunai Bertahap** (9000), **In-House** (9003),
**KPR Komersial** (9002), **KPR Subsidi** (9001). Setiap skema mengimplementasikan interface yang sama:
`AllowedEvents` (state machine transisi sah), `ResolveReceivableAccount`, `BuildSchedulePlan`, `CanRecognize`
(gate sebelum Akad boleh terjadi), `ValidateParams`/`ValidateContract`.

### 6.6 Akad — event tunggal yang memposting pendapatan + HPP + split KPR sekaligus

`RecordAkad` (`internal/sale/service.go:612-936`) adalah **satu fungsi, satu transaksi database** yang
menghasilkan SEMUA jurnal berikut sekaligus (bukan beberapa event terpisah di waktu berbeda):

**6.6a — Guard**: `unit.Status == "sold"` → ditolak (`ErrUnitAlreadySold`, satu-satunya guard status mentah di
level ini). Prasyarat state-machine sesungguhnya (mis. harus sudah `dp_paid`/kontrak tertentu tergantung skema)
ditegakkan oleh `policy.CanRecognize()` milik skema masing-masing — kontrak legacy tanpa skema terdaftar
melewati gate ini dan jatuh ke perilaku lama (`receivableCode` default `1-2000`).

**6.6b — Resolusi HPP**: kaskade 3-tingkat, lihat §3.6.

**6.6c — Split Dana Jaminan Bank SAAT Akad** (untuk kontrak berskema KPR dengan `FinancingReceivableAccount`
dikonfigurasi):
```
financingAmount  = min(bankApprovedAmount, piutangTotal)   → Dr 1-2200 Dana Jaminan Bank
receivableAmount = piutangTotal − financingAmount           → Dr 1-2000 Piutang Usaha (bila > 0)
```
Ini terjadi **saat Akad**, TIDAK direklas belakangan saat pencairan (F-6, bug lama sudah dicabut).

**6.6d — Jurnal pendapatan (Event 3)**:
```
Dr  2-2000 Uang Muka Penjualan            = houseAdvance     (bila > 0)
Dr  1-2200 Dana Jaminan Bank              = financingAmount  (bila kontrak KPR-financed, > 0)
Dr  1-2000 Piutang Usaha                  = receivableAmount (bila > 0)
    Cr  4-1000* Pendapatan Penjualan Unit     = harga jual (DPP)
    Cr  2-3000 PPN Keluaran                   = PPN         (bila PKP)
```
(*akun pendapatan sebenarnya diresolusi per-produk lewat Product Catalog, bukan hardcode `4-1000` — bisa
berbeda per jenis produk.) Debit total = Credit total secara konstruksi (dijamin balance).

**6.6e — Jurnal HPP (Event 4)**, transaksi yang SAMA — lihat §3.6.

**Contoh angka lengkap** — Unit tipe 36 (subsidi), harga jual Rp180.000.000 (tidak PKP, PPN=0), skema KPR
Subsidi. Uang muka yang sudah diterima sebelum Akad (house advance) = Rp10.000.000. Bank approved KPR =
Rp165.000.000. HPP budgeted unit ini: Land Rp20.000.000 + Hard Rp95.000.000 = Rp115.000.000.

`piutangTotal = 180.000.000 − 10.000.000 = 170.000.000`.
`financingAmount = min(165.000.000, 170.000.000) = 165.000.000`.
`receivableAmount = 170.000.000 − 165.000.000 = 5.000.000`.

*Event 3 — pendapatan:*
```
Dr  2-2000 Uang Muka Penjualan             10.000.000
Dr  1-2200 Dana Jaminan Bank              165.000.000
Dr  1-2000 Piutang Usaha                    5.000.000
    Cr  4-1000 Pendapatan Penjualan Unit                    180.000.000
```
(Σ Dr = 180.000.000 = Σ Cr ✓)

*Event 4 — HPP, transaksi yang sama:*
```
Dr  5-1000 Harga Pokok Penjualan (HPP)     115.000.000
    Cr  1-3000 Persediaan — Tanah                            20.000.000
    Cr  1-3100 Persediaan — Hard Cost                        95.000.000
```
(Σ Dr = 115.000.000 = Σ Cr ✓)

**6.6f — Netting Kelebihan Tanah pada kontrak tunai** (bila unit ini bundel dengan penjualan Kelebihan Tanah
dan uang mukanya diterima sebagai satu lump-sum): jurnal tambahan `Dr Uang Muka Penjualan / Cr Piutang
Kelebihan Tanah` menetralkan porsi tanah dari lump-sum tersebut (Event 3 di atas hanya menolkan porsi rumah).

**6.6g — Serah terima fisik** (`RecordPhysicalHandover`) — `sold→occupied`, **tanpa jurnal, tanpa resolusi HPP
ulang** — murni pencatatan fisik terpisah dari pengakuan finansial yang sudah selesai di Akad.

### 6.7 Payment routing — pra-BAST vs pasca-BAST

- **Pra-BAST** (belum ada `SaleRecord`): setiap pembayaran diterima → `Cr 2-2000 Uang Muka Penjualan`
  (kewajiban) — sesuai Invariant #7 CLAUDE.md. Overpayment pra-BAST **diizinkan eksplisit** → jadi Saldo
  Kredit Buyer (tetap di dalam `2-2000`).
- **Pasca-BAST**: default `Cr 1-2000 Piutang Usaha`, KECUALI untuk pembayaran `source = KPRDisbursement`
  pada kontrak berskema, yang **selalu** mengkredit `1-2200 Dana Jaminan Bank` (dibatasi saldo tersisa —
  `ErrDisbursementExceedsFinancing` bila melebihi) — **tidak peduli** `scheme_state` sudah `disbursed` atau
  belum (perbaikan Item 7C, F-6). Overpayment pasca-BAST **ditolak** (`ErrPaymentExceedsReceivable`).
- Pembayaran non-disbursement pasca-Akad (mis. customer melunasi kekurangan langsung) memakai resolusi
  berbasis `scheme_state` seperti biasa (bisa `1-2200` atau `1-2000` tergantung state).

### 6.8 KPR bertahap & kekurangan (shortfall)

Setiap tahap pencairan KPR mengkredit `1-2200` secara langsung, dibatasi saldo yang tersisa. **Tidak ada
reklas otomatis** saat status berubah ke `disbursed` — saldo `1-2200` hanya bergerak lewat kredit per-tahap
yang dibatasi tepat (F-6).

**Invoice "KEKURANGAN"**: bila pencairan bank < piutang yang di-expect, sisa ditagihkan ke buyer via invoice
kekurangan yang berpiutang ke `1-2000` (karena `scheme_state` sudah `disbursed`, yang menurut kebijakan skema
mereklas billing residual ke Piutang Customer). Default **OFF** di level kebijakan tenant — admin memicu manual
via tombol di UI kecuali diaktifkan eksplisit. Tidak bisa terbit dobel (`ErrShortfallInvoiceExists`).

### 6.9 Customer statement, AR, receipt

Statement pelanggan menggabungkan seluruh sumber piutang (§2.6) per customer. Setiap penerimaan pembayaran
memakai kunci idempotensi terlebih dulu — key berulang mengembalikan hasil termin yang sudah ada, tidak
membuat duplikat jurnal/kwitansi. Penerbitan kwitansi atomic dengan commit pembayaran (INV-DOC-1).

### 6.10 Pembatalan pasca-Akad (kontrak-level)

`internal/cancellation` — mengembalikan uang muka yang sudah diterima (dikurangi penalti) ke `2-2200 Hutang
Refund`, membalik seluruh jurnal terkait (revenue, HPP) via `PostingService.Reverse` (append-only, Invariant
#5). Modul ini tidak perlu tahu BAGAIMANA HPP tadinya dihitung (budgeted vs true-up) — ia cukup membalik semua
jurnal yang tertaut ke Akad tersebut secara utuh (INV-COGS-SUM).

---

<a id="7-kelebihan-tanah"></a>
## 7. Kelebihan Tanah

Kelebihan Tanah adalah **produk/stok yang bisa dijual berdiri sendiri** — BUKAN unit properti, dan BUKAN lagi
desain lama "Charge Group/addon" (§0.1 F-4). Modulnya standalone: `internal/land`.

### 7.1 Model data

`LandStock` (tabel `land_stock`): `TotalQuantityM2`, `ReservedQuantityM2`, `SoldQuantityM2` (semua
`decimal.Decimal`, DECIMAL(20,4)), `UnitPrice` (harga jual/m², `domain.Money`), `PurchasePrice` (basis
biaya/m², `domain.Money`). `AvailableQuantityM2 = Total − Reserved − Sold`, **selalu dihitung saat dibaca**,
tidak pernah disimpan sebagai kolom (satu sumber kebenaran).

`TotalQuantityM2` dan `PurchasePrice` **di-input admin** — bukan diturunkan otomatis dari sisa alokasi HPP
unit. Namun stok ini tetap ikut sebagai peserta pool tanah project-wide bersama unit-unit (§3.3) — carve-out
tetapnya dikurangi lebih dulu dari pool sebelum sisa dibagi ke unit.

### 7.2 Harga jual vs basis HPP

Dua field independen, tidak saling diturunkan:
- `UnitPrice` — harga jual/m², bisa di-override per reservasi/snapshot Akad.
- `PurchasePrice` — basis biaya/m², dipakai **langsung** sebagai tarif HPP (`PurchasePriceLandHPPResolver`,
  resolver produksi saat ini — menggantikan `BudgetedLandHPPResolver` berbasis pool per koreksi klien
  2026-08-31; kode lama masih ada di file tapi mati di wiring produksi).

### 7.3 Reservasi & guard oversell

`ReserveTx`/`RecordAkadTx` mengunci baris `land_stock` (`SELECT ... FOR UPDATE`) di dalam transaksi, lalu
memeriksa `QuantityM2 <= AvailableQuantityM2()` — ditolak (`ErrCapacityExceeded`) bila melebihi. Dilindungi
ganda oleh CHECK constraint di level database sebagai pertahanan tambahan terhadap race condition.

### 7.4 Siklus hidup lengkap

1. **Reservasi** — soft-lock murni. **Tanpa booking fee, tanpa transaksi finansial, tanpa jurnal** (keputusan
   klien 2026-08-20). Status: `active → converted | expired | cancelled`.
2. **Akad / konversi** — dua jalur, satu mesin inti (`RecordAkadTx`):
   - **Standalone** (tunai/lunas): mendebit kas/bank langsung.
   - **Bundled** (menyatu dengan booking unit rumah): mendebit `1-2000 Piutang Customer` — SELALU akun ini,
     tidak pernah akun financing unit rumahnya (perbaikan bug 2026-08-31 — sebelumnya bisa salah masuk ke
     `1-2200` pada kontrak KPR dan tidak pernah direklas).
3. **Jurnal** (transaksi yang sama, lihat §7.5).
4. **Pajak** — PPh Final dihitung & diposting otomatis dalam transaksi yang sama bila berlaku (§8).
5. **Piutang** — untuk jalur bundled, baris debit itu SENDIRI adalah AR-nya — tidak ada langkah invoice
   terpisah; piutang Kelebihan Tanah ikut ditagih lewat mesin `payment_schedules`/`internal/receivable` yang
   sama dengan unit (migrasi `000095`).
6. **Snapshot alokasi** dibekukan saat Akad (`HPPRatePerM2Snapshot`, `HPPTotal`, `Basis`, versi konfigurasi) —
   pola yang sama dengan `allocation_snapshots` sisi unit.
7. **Pembayaran/pelunasan** pasca-Akad memakai mesin piutang/pembayaran generik yang sama seperti piutang lain.
8. **Pembatalan** — lihat §7.6.

### 7.5 Jurnal Kelebihan Tanah (akun COA tetap, BUKAN dari Product Catalog)

```go
landRevenueAccountCode   = "4-1100"  // Pendapatan Penjualan Tanah
landPPNKeluarAccountCode = "2-3000"  // PPN Keluaran
landHPPAccountCode       = "5-1000"  // Harga Pokok Penjualan (HPP)
landInventoryAccountCode = "1-3000"  // Persediaan Real Estat — Tanah
```

**Jurnal pendapatan**:
```
Dr  <akun pembayaran>            gross    (kas/bank bila standalone; 1-2000 bila bundled)
    Cr  4-1100 Pendapatan Penjualan Tanah      DPP
    Cr  2-3000 PPN Keluaran                    PPN   (hanya bila PKP)
```
**Jurnal HPP** (hanya bila HPP total ≠ 0):
```
Dr  5-1000 Harga Pokok Penjualan (HPP)     HPP total
    Cr  1-3000 Persediaan — Tanah                HPP total
```
**Jurnal akrual PPh** — lihat §8.

**Contoh angka**: Jual Kelebihan Tanah 50m² @ harga jual Rp600.000/m² = Rp30.000.000 (DPP), tidak PKP.
`PurchasePrice` = Rp500.000/m² → HPP = 50 × 500.000 = Rp25.000.000. Jalur standalone, dibayar tunai ke Bank BCA.

*Pendapatan:*
```
Dr  1-1300 Bank BCA                 30.000.000
    Cr  4-1100 Pendapatan Penjualan Tanah                   30.000.000
```
*HPP:*
```
Dr  5-1000 Harga Pokok Penjualan (HPP)     25.000.000
    Cr  1-3000 Persediaan — Tanah                            25.000.000
```
Laba kotor transaksi ini = 30.000.000 − 25.000.000 = **Rp5.000.000**.

### 7.6 Pembatalan / reversal

`CancelLandSaleTx`: syarat status `Akad` (`ErrLandSaleNotAkad` bila bukan). Bila sudah ada pembayaran diterima
DAN tidak diizinkan (`AllowPaidSchedule=false`, default jalur standalone) → `ErrLandSaleHasReceivedPayment`
(jalur bundled via `internal/cancellation` mengizinkan ini karena ia menghitung refund/settlement penuh secara
terpisah). Membalik (bukan menghapus) hingga TIGA jurnal independen — pendapatan, HPP, PPh — masing-masing
lewat `PostingService.Reverse()` bila jurnalnya ada. `sold_quantity_m2` dikembalikan ke **available** (bukan
`reserved`). Kuantitas dijaga dengan `WHERE sold_quantity_m2 >= qty` — bila `RowsAffected==0`, dianggap
inkonsistensi keras (error, bukan diam-diam dilewati).

**Catatan desain**: pembatalan Kelebihan Tanah standalone BUKAN memakai ulang `internal/cancellation` — karena
Event pendapatannya langsung mendebit Kas/Bank (bukan Uang Muka), jurnal pembalik pendapatan itu SENDIRI sudah
menjadi "pengembalian" — tidak perlu Uang Muka/Hutang Refund terpisah seperti pembatalan unit properti
pasca-BAST.

### 7.7 Desain lama — status: kode mati, diblokir aktif

Desain sebelumnya memperlakukan Kelebihan Tanah sekaligus sebagai UNIT yang bisa dijual dan sebagai item
"addon" bebas-ketik yang uangnya mendarat di `2-2400 Titipan Realisasi` — akun **kewajiban**, padahal Kelebihan
Tanah bukan titipan siapa pun; selama duduk di sana, penjualannya tidak pernah muncul di Laba/Rugi. Rule
produk `kelebihan_tanah` masih ada di katalog produk (untuk referensi/pelaporan), tapi jalur addon-nya
**diblokir fail-closed**: `domain.ProductCategory.ParticipatesInHPP()` bernilai `true` untuk kategori Tanah,
sehingga upaya memasukkan item berkode `kelebihan_tanah` lewat jalur addon (`internal/charge`) gagal dengan
`ErrProductNotAddon`. **Kesimpulan: desain lama benar-benar mati, bukan sekadar "digantikan konvensi" — aman
didokumentasikan sebagai sepenuhnya tergantikan.**

---

<a id="8-tax-pph-final--ppn"></a>
## 8. Tax (PPh Final & PPN)

**Instruksi tegas: tarif di bawah ini adalah yang benar-benar ter-seed di database (migrasi `000039`). Jangan
menambah/mengubah tarif tanpa verifikasi ulang terhadap `tax_rules` — dokumentasi ini tidak mengarang aturan.**

### 8.1 Tarif PPh Final Pengalihan (verified live)

| Kategori | Tarif | `rate_code` |
|---|---|---|
| Subsidi | **1,0%** | `pph_final_pengalihan` |
| Komersial | **2,5%** | `pph_final_pengalihan` |

Disimpan sebagai baris `tax_rules` per-tenant (config-driven, `DECIMAL(10,6)`) — bukan konstanta Go hardcode.

### 8.2 Resolusi tarif efektif (versi/tanggal)

`ResolveRule(tenantID, rateCode, category, referenceDate)`:
```sql
WHERE effective_from <= ref AND (effective_to IS NULL OR effective_to >= ref)
  AND applies_to IN (category, 'all') AND is_active
ORDER BY (applies_to = category ? 0 : 1), effective_from DESC
```
Spesifisitas menang dulu (kategori persis mengalahkan fallback `all`), lalu `effective_from` terbaru menang.
`effective_to = NULL` = masih berlaku. Setiap perubahan konfigurasi menaikkan `Revision` — dicatat sebagai
provenance (`TaxRuleID`+`TaxRuleRevision`) pada `AccrualPlan` dan akhirnya pada `TaxObligation` — sehingga
"kenapa transaksi ini kena tarif X%" selalu bisa dijawab dari record kewajiban itu sendiri.

### 8.3 Trigger & jurnal akrual PPh

Trigger: `Service.ResolveAccrualPlan(AccrueTaxRequest{...})` — dipanggil saat **Akad unit (BAST)** dan saat
**Akad Kelebihan Tanah**, pola identik. `TransferValue = DPP` (sebelum PPN).

Formula (satu-satunya yang aktif — lihat §8.6): `tax = round(DPP × tarif, 0)` — bulat rupiah, tidak negatif.

```
Dr  5-2000 Beban PPh Final Pengalihan     taxAmount
    Cr  2-4000 Hutang PPh Final Pengalihan     taxAmount
```
Satu baris `TaxObligation` tercatat, tertaut ke jurnal dan ke sale/land_sale terkait.

**Contoh angka**: unit subsidi, DPP = Rp150.000.000, tarif 1,0% → PPh = `round(150.000.000 × 0,01) =
Rp1.500.000`.
```
Dr  5-2000 Beban PPh Final Pengalihan       1.500.000
    Cr  2-4000 Hutang PPh Final Pengalihan                  1.500.000
```

### 8.4 Reversal saat pembatalan

Sama seperti pendapatan/HPP: dibalik via `PostingService.Reverse()`, dan `TaxObligation.Status` diset
`Cancelled`. Ketiga jurnal (pendapatan, HPP, PPh) independen — masing-masing hanya dibalik bila jurnal aslinya
ada.

### 8.5 PPN (VAT) — dipakai di penjualan properti & tanah, bukan cuma AP

`vat = round(DPP × VATRateSnapshot, 0)` — rumus yang **sama persis** dipakai penjualan unit maupun Kelebihan
Tanah. Diposting ke `2-3000 PPN Keluaran`, kondisional pada flag `IsPKP` (per kontrak/per Akad — status
PKP pembeli/penjual), **bukan** ditentukan oleh kategori subsidi/komersial. Rumah subsidi yang umumnya
bebas PPN dalam praktiknya diekspresikan sederhana sebagai `IsPKP = false` untuk penjualan tersebut, bukan
jalur kode terpisah.

### 8.6 Kelebihan Tanah — kategori pajak mengikuti PROYEK, bukan unit yang dibundel

**Temuan paling penting bagian ini**: kategori pajak Kelebihan Tanah **selalu** mengikuti `tax_category`
proyek (default konservatif: `komersial` bila proyek tidak punya kategori yang bisa diresolusi) — **tidak
pernah** override per-unit, karena request pajak Kelebihan Tanah tidak pernah menyertakan `UnitID` (cabang
override produk-per-unit secara struktural tidak pernah tercapai untuk penjualan tanah). Untuk proyek dengan
unit campuran (ada unit subsidi dan komersial dalam satu proyek), parsel Kelebihan Tanah akan **selalu**
dikenai tarif kategori default proyek — TIDAK PEDULI unit rumah mana yang kebetulan dibundel dalam Akad
tersebut. Ini poin yang wajib dipahami tim pajak/accounting agar tidak salah ekspektasi.

### 8.7 Formula pajak — hanya proporsional yang aktif

`TaxFormula` punya 5 nilai kosakata (`proportional`, `progressive`, `threshold`, `fixed_amount`, `exemption`),
tapi **hanya `proportional` yang terimplementasi** dan terdaftar di registry produksi. Memanggil formula lain
mengembalikan `ErrFormulaNotImplemented` — ini adalah *seam* yang sudah disiapkan untuk masa depan, bukan fitur
yang bisa dipakai sekarang. **Jangan dokumentasikan/janjikan tarif progresif atau threshold sebagai fitur yang
sudah jalan.**

---

<a id="9-expense--cost-entry"></a>
## 9. Expense / Cost Entry

Satu pintu masuk: `internal/cost` (dan turunannya `internal/cost/expense_handler.go` untuk sisi beban
operasional, W-10). Enam kategori:

| Kategori | HPP/Beban | Sumber dana | Bukti | Tag proyek | Tautan RAB (`BudgetItemID`) | Jurnal |
|---|---|---|---|---|---|---|
| Produksi Subsidi (`hard`, subkategori `produksi_subsidi`) | **HPP** | Kas/Bank/Hutang | wajib | wajib (bila direct) | opsional | `Dr Persediaan/Beban... Cr Kas-Bank/Hutang` |
| Produksi Komersial (`hard`, `produksi_komersial`) | **HPP** | idem | wajib | wajib (bila direct) | opsional | idem |
| Sarana & Prasarana (`hard`, `sarana_prasarana`) | **HPP** | idem | wajib | opsional (project-wide) | opsional | idem |
| Perizinan (`hard`, `perizinan`) | **HPP** | idem | wajib | opsional (project-wide) | opsional | idem |
| Pemasaran (`marketing`, overhead) | **Beban** (`5-3xxx`) | idem | wajib | **dilarang** (INV-EXP-2) | **dilarang** | `Dr Beban Pemasaran / Cr Kas-Bank` |
| Lain-lain (`other`, overhead) | **Beban** (`5-4xxx`) | idem | wajib | **dilarang** | **dilarang** | `Dr Beban Lain-lain / Cr Kas-Bank` |

Kategori `operational` (Beban Operasional umum, `5-4600`) juga overhead-only, sama aturannya dengan
Pemasaran/Lain-lain.

**INV-EXP-2** (enforced di `internal/cost/service.go:303-310`): biaya operasional (tier `overhead`) yang
akunnya kebetulan sama dengan akun taksonomi biaya proyek, DAN request tersebut membawa `ProjectID` bukan-nol
→ **ditolak** (`ErrExpenseTypeTaxonomyAccount`). Alasan: realisasi RAB dihitung dari SEMUA baris ledger di akun
taksonomi yang bertag `project_id` — men-tag biaya operasional dengan proyek akan diam-diam menggelembungkan
realisasi RAB (melanggar aturan "tag proyek ≠ realisasi RAB").

Aturan tambahan: request tipe-beban pada tier non-overhead ditolak total (`ErrExpenseTypeNotForProjectCost`) —
tipe beban hanya untuk beban periode, tidak pernah untuk biaya yang bisa dikapitalisasi. Request tipe-beban
yang membawa `BudgetItemID` ditolak (`ErrExpenseTypeWithBudgetItem`) — item RAB hidup di taksonomi
`CostCategory`, beban operasional tidak pernah jadi realisasi RAB.

---

<a id="10-bank--cash--payment"></a>
## 10. Bank / Cash / Payment

5 akun kas/bank: `1-1100` Kas Besar, `1-1200` Petty Cash, `1-1300` Bank BCA, `1-1400` Bank Mandiri,
`1-1500` Bank BRI.

| Transaksi | Dr | Cr | Contoh |
|---|---|---|---|
| Terima fee booking (>0, non-refundable) | Kas/Bank | `4-2100` Pendapatan Booking | §6.2 |
| Terima DP (pra-BAST) | Kas/Bank | `2-2000` Uang Muka Penjualan | — |
| Terima cicilan (pasca-BAST) | Kas/Bank | `1-2000` Piutang Usaha (atau `1-2200` bila disbursement KPR) | — |
| Pencairan KPR (tahap) | Kas/Bank | `1-2200` Dana Jaminan Bank (dibatasi saldo tersisa) | §6.8 |
| Bayar vendor (invoice) | `2-1000` Hutang Usaha | Kas/Bank | §10.1 |
| Bayar vendor (retensi) | `2-1100` Hutang Retensi Kontraktor | Kas/Bank | §10.1 |
| Uang muka vendor | `1-5300` Uang Muka Vendor (aset, BUKAN pelunasan kewajiban) | Kas/Bank | §10.1 |
| Refund booking | `2-2200` Hutang Refund | Kas/Bank | (pencairan, langkah terpisah dari jurnal pembatalan §6.3) |
| Bayar komisi | `2-6200` Utang Komisi | Kas/Bank | §11 |
| Akuisisi aset tetap | `1-4xxx` (per kategori) | Kas/Bank | §12 |

### 10.1 Pembayaran vendor (AP)

`internal/ap/payment_journal.go` (`composePayment`), satu fungsi dipakai identik untuk preview maupun posting
final (tidak ada jalur ganda yang bisa berbeda hasil):
```
Dr  <akun kewajiban, sesuai PaymentKind>      Σ nilai pembayaran
    Cr  <akun kas/bank>                            nilai pembayaran
```
Akun kewajiban ditentukan oleh **jenis pembayaran** (`invoice`→`2-1000`, `retention`→`2-1100`,
`advance`→`1-5300`, ini justru mendebit ASET bukan melunasi kewajiban), bukan dari isi invoice.

**INV-AP-3**: sebelum baris ditulis, akun kewajiban DAN akun kas yang teresolusi dicek terhadap
`cost.IsCostTaxonomyAccount()` — bila salah satu ternyata bertabrakan dengan akun taksonomi biaya proyek,
seluruh komposisi ditolak (`ErrPaymentTouchesCost`). Ini mencegah pembayaran vendor pernah dobel-hitung ke
realisasi RAB — dan pengecekannya dilakukan pada **akun hasil resolusi**, bukan pada jalur kode, sehingga
perubahan taksonomi di masa depan tidak bisa diam-diam melanggar aturan ini. Baris kewajiban TIDAK ditag
`ProjectID` (kewajiban bukan biaya, tagging akan membuatnya ikut terbaca di laporan biaya per-proyek).

### 10.2 Idempotency

Kunci idempotensi dicek di paling awal alur `ReceivePayment` — key berulang mengembalikan hasil termin yang
sudah ada, bukan membuat jurnal/kwitansi ganda.

### 10.3 Dana Jaminan Bank (`1-2200`) — ringkasan siklus lengkap

| Event | Dr | Cr |
|---|---|---|
| Akad (kontrak KPR) | `1-2200` = min(bankApproved, piutangTotal) | (dipasangkan baris Event 3 lain) |
| Setiap tahap pencairan KPR | Kas/Bank | `1-2200` (dibatasi saldo tersisa) |
| Kekurangan pasca-pencairan | `1-2000` (via invoice KEKURANGAN otomatis/manual) | — |
| Pembayaran pasca-Akad non-disbursement | Kas/Bank | resolusi berbasis `scheme_state` (`1-2200` atau `1-2000`) |

Tidak ada reklas otomatis saat status berubah ke `disbursed` (F-6).

### 10.4 Refund

Lihat §6.3 (booking) dan §6.10 (kontrak pasca-Akad) — refund selalu dua langkah: (1) jurnal pengakuan
kewajiban `Cr 2-2200 Hutang Refund`, (2) pencairan aktual `Dr 2-2200 / Cr Kas-Bank` sebagai transaksi terpisah.

### 10.5 Dokumen bernomor (INV-DOC-1)

Setiap jurnal yang menyentuh akun kas/bank WAJIB mendapat nomor dokumen dalam transaksi yang sama, atau
seluruh jurnal batal. 11 jenis: `KWT` (kwitansi termin), `KWB` (kwitansi booking), `KWR` (kwitansi realisasi),
`MTI` (memo titipan notaris), `INV` (invoice), `KWD` (kwitansi lain), `BKM` (bukti kas masuk), `BKK` (bukti
kas keluar), `BTP` (bukti transfer), `RFC` (refund), `JR` (jurnal umum non-kas — pengecualian, tidak wajib
nomor dokumen kas). Format `{prefix}/{tahun}/{nomor 6 digit}`, reset tiap tahun. Pengecualian: posting saldo
awal, dan test store non-GORM di memori.

---

<a id="11-commission"></a>
## 11. Commission

`internal/commission` — **sistem jurnal double-entry sungguhan**, bukan pencatatan internal saja, terintegrasi
penuh dengan `ledger.PostingService` dan aturan INV-DOC-1.

### 11.1 Basis komisi

- `percent_of_sale` — `rate × basis_amount`, dibulatkan ke rupiah penuh.
- `flat_per_unit` — jumlah tetap.
- `tiered` — **kosakata terdaftar, belum terimplementasi** (`ErrBasisNotImplemented`) — jangan dokumentasikan
  sebagai fitur berjalan.

### 11.2 Trigger

Hanya **`at_bast`** yang terimplementasi (komisi dihitung saat Akad/BAST). `at_collection`/`at_lunas` adalah
seam kosakata, belum terhubung ke mesin kalkulasi.

### 11.3 Scope aturan komisi

`Rule` bisa NULL-scope (berlaku semua) atau dibatasi kombinasi `SalesPersonID`/`ProjectID`/`UnitType`/rentang
tanggal efektif. Tarif & akun **di-snapshot ke entry Commission saat dihitung** (`RateSnapshot`) — perubahan
Rule di kemudian hari tidak pernah mengubah komisi yang sudah dihitung sebelumnya secara retroaktif.

### 11.4 Siklus status & jurnal

`calculated → approved → payable → paid`; `cancelled` bisa dari `calculated`/`approved`/`payable`;
`clawed_back` HANYA dari `paid`. Keduanya status terminal.

```
Payable (accrual):    Dr  5-3100 Beban Komisi Penjualan        Cr  2-6200 Utang Komisi
Paid (settlement):    Dr  2-6200 Utang Komisi                  Cr  Kas/Bank
Cancel:                jurnal pembalik akrual (bila sudah payable)
Clawback (dari paid): Dr  1-2100 Piutang Lain-lain             Cr  5-3100 Beban Komisi Penjualan
```

**Contoh angka** — komisi sales 2% dari harga jual Rp180.000.000 = Rp3.600.000:
```
Dr  5-3100 Beban Komisi Penjualan     3.600.000
    Cr  2-6200 Utang Komisi                          3.600.000     [accrual — status payable]
```
Saat dibayar:
```
Dr  2-6200 Utang Komisi                3.600.000
    Cr  1-1300 Bank BCA                              3.600.000     [status paid]
```

### 11.5 Admin Marketing

Paralel dengan komisi Sales (rule scope/perhitungan/jurnal sama persis), dibedakan hanya dari peran penerima.

---

<a id="12-fixed-asset--depreciation"></a>
## 12. Fixed Asset & Depreciation

`internal/fixedasset` — tidak membuat mesin posting baru, memakai ulang `ledger.PostingService` lewat adapter
lokal (pola yang sama dengan cost/sale/land/charge).

### 12.1 Kategori aset

3 akun aset live: `1-4000` Peralatan Kantor, `1-4100` Kendaraan, `1-4200` Gedung/Bangunan, semuanya
diakumulasikan penyusutannya di **satu** akun kontra `1-4900` Akumulasi Penyusutan (per kategori aset — akun
akumulasi & akun beban penyusutan dipetakan dari `Category`, bukan hardcode global). Setiap kategori memetakan
3 akun: aset, akumulasi penyusutan, beban penyusutan + `DefaultUsefulLifeMonths`.

### 12.2 Akuisisi

```
Dr  <akun aset kategori>          AcquisitionCost
    Cr  <akun Kas/Bank>                AcquisitionCost
```
`PaymentMethod`: v1 **hanya `bank`** yang terimplementasi — `payable` (beli via hutang) ditolak, belum ada
jalur settlement AP dari titik ini.

### 12.3 Penyusutan

v1 hanya metode `straight_line`. Jadwal bulanan dihitung dari `DepreciableAmount = AcquisitionCost −
ResidualValue`, dibagi merata via largest-remainder allocation (`Money.Allocate`) — Σ(jadwal) = jumlah
disusutkan persis, tanpa sisa yang hilang.

```
Setiap periode:  Dr  <Beban Penyusutan kategori>       Cr  <Akumulasi Penyusutan kategori>
```
Idempoten: `UNIQUE(tenant_id, fixed_asset_id, period_year, period_month)` — menjalankan ulang untuk periode
yang sudah diposting adalah no-op, bukan jurnal duplikat.

**Contoh angka**: Aset Kendaraan, harga perolehan Rp240.000.000, nilai residu Rp0, masa manfaat 48 bulan
→ penyusutan bulanan = Rp5.000.000:
```
Dr  5-4500 Beban Penyusutan     5.000.000
    Cr  1-4900 Akumulasi Penyusutan             5.000.000
```

`accumulated_depreciation` dan `book_value` **tidak pernah disimpan** — selalu diturunkan dari
`SUM(fixed_asset_depreciation_lines.amount)` saat dibaca.

### 12.4 Disposal

**BELUM diimplementasikan** — hanya groundwork (`AssetStatus` punya nilai `disposed`, field `DisposedAt` ada
di model) tapi tidak ada satu pun endpoint/service yang menulis status tersebut atau memposting jurnal
disposal. **Dokumentasikan sebagai roadmap, bukan fitur berjalan.**

`ProjectID` pada aset bersifat nullable — aset kantor pusat boleh tidak terikat proyek mana pun.

---

<a id="13-financial-reports"></a>
## 13. Financial Reports

### 13.1 Neraca (Balance Sheet)

- **Tujuan**: posisi keuangan per tanggal (snapshot life-to-date).
- **Sumber data**: `ledger.TrialBalance` (agregat debit/kredit per akun sejak awal sampai tanggal terpilih).
- **Formula**: Aset = Kewajiban + Ekuitas + Laba/Rugi Tahun Berjalan (life-to-date, dilipat ke ekuitas).
- **Filter tanggal**: parameter `From` HANYA menambah baris informasi `LabaRugiPeriodeTerpilih` (dihitung ulang
  via `ComputePL` untuk window tsb) — **tidak pernah** mengubah `LabaRugiTahunBerjalan`/`TotalEkuitas`/
  `IsBalanced`. Mencampur L/R berwindow ke dalam identitas neraca akan merusak Aset=Kewajiban+Ekuitas karena
  laba sebelum `From` akan hilang dari perhitungan.
- **`IsBalanced`**: invariant check yang dibangun ke dalam laporan itu sendiri — harus selalu `true`; bila
  `false`, artinya ADA jurnal yang tidak balance (bug serius, harus diinvestigasi segera).
- **Interpretasi contoh**: bila `TotalAset = 5.000.000.000` dan `TotalKewajiban + TotalEkuitasEfektif =
  5.000.000.000`, `IsBalanced = true` → neraca sehat secara struktural.

### 13.2 Laba/Rugi (P&L) — struktur 8-bagian, terverifikasi dari kode

```
Pendapatan − HPP                                       = Laba Kotor
Laba Kotor − Beban Operasional                         = Laba Operasional        ← subtotal eksplisit
Laba Operasional + Pendapatan Luar Usaha − Beban Luar Usaha
                                                        = Laba Bersih Sebelum Pajak
Laba Bersih Sebelum Pajak − Beban Pajak                = Laba Bersih Setelah Pajak (Laba Bersih)
```
(Struktur ini SAMA formanya dengan yang diasumsikan awal task, tapi kode punya satu subtotal eksplisit
tambahan — "Laba Operasional" — di antara Laba Kotor dan langkah luar-usaha. Dokumentasikan persis begini,
jangan hilangkan baris "Laba Operasional".)

**Klasifikasi akun BUKAN sekadar cocokkan prefix "4-"/"5-"** — dipusatkan di `AccountRoleRegistry`
(`internal/ledger/account_role.go`, rujukan kanonik `docs/financial-canonical-registry.md §R-1`):

| Role | Akun |
|---|---|
| `RoleCOGS` (HPP) | `5-1000` |
| `RoleOtherIncome` (Pendapatan Luar Usaha) | `4-2000` |
| `RoleOtherExpense` (Beban Luar Usaha) | `5-5000` (Beban Bunga) |
| `RoleTaxExpense` (Beban Pajak) | `5-2000` |

Semua akun `4-x` LAINNYA (`4-1000`, `4-1100`, `4-2100`) → Pendapatan Operasional (fallback default). Semua
akun `5-x` LAINNYA (`5-3000`, `5-3100`, `5-3200`, `5-4000`..`5-4700`) → Beban Operasional (fallback default).

**Contoh angka** — periode berjalan:
```
Pendapatan Penjualan Unit (4-1000)          800.000.000
Pendapatan Penjualan Tanah (4-1100)          30.000.000
Pendapatan Booking (4-2100)                   5.000.000
                                    Total Pendapatan = 835.000.000

HPP (5-1000)                                425.000.000
                                    Laba Kotor       = 410.000.000

Beban Pemasaran (5-3000)                     20.000.000
Beban Komisi Penjualan (5-3100)              16.600.000
Beban Umum & Administrasi (5-4000)           30.000.000
Beban Soft Cost (5-4700)                     12.000.000
                                    Beban Operasional = 78.600.000
                                    Laba Operasional  = 331.400.000

Pendapatan Lain-lain (4-2000)                 2.000.000
Beban Bunga (5-5000)                          4.000.000
                                    Laba Bersih Sebelum Pajak = 329.400.000

Beban PPh Final Pengalihan (5-2000)           9.900.000
                                    Laba Bersih Setelah Pajak = 319.500.000
```

`ProjectID = nil` → laporan konsolidasi seluruh proyek; diisi → P&L satu proyek.

### 13.3 Neraca Saldo (Trial Balance)

Wrapper tipis di atas `ledger.TrialBalance` — total debit/kredit per akun, dasar dari Neraca dan P&L.

### 13.4 Arus Kas — metode LANGSUNG, bukan tidak langsung

Membaca pergerakan kas/bank aktual (`CashMovement`), bukan rekonsiliasi dari laba bersih. Klasifikasi:
`IsInvesting` = jurnal menyentuh baris akun tetap `1-4xxx`; `IsPendanaan` = jurnal menyentuh `2-5000` (Hutang
Bank) atau `3-xxx` (ekuitas); selain itu → Operasi (default).

### 13.5 AR Aging / Piutang Customer

Satu mesin (`internal/receivable.BuildAging`) mengagregasi 4 sumber sekaligus: `house`, `realization`,
`addon` (termasuk Kelebihan Tanah — **bug historis diperbaiki 2026-08-31**: sebelumnya `land_sales` sama
sekali tidak pernah terbaca ke dalam AR; sekarang sudah diperbaiki, jadi dokumentasikan Kelebihan Tanah SEBAGAI
sumber AR yang aktif agar celah itu tidak diam-diam terulang), dan `legacy`. Baris dari seluruh sumber
**sengaja dicampur/di-interleave** urut jatuh tempo, bukan dikelompokkan per sumber (desain W-4).

### 13.6 Cash/Bank

Tidak ada laporan berdiri sendiri — saldo per akun kas/bank diturunkan dari Trial Balance (filter kategori
`cash`/`bank`), mutasi detail dari Arus Kas atau drill-down `journal_lines` per akun.

### 13.7 Persediaan / HPP

Tidak ada "Laporan Persediaan/HPP" tersendiri. Visibilitasnya tersebar di tiga tempat: (a) baris Persediaan di
Neraca, (b) baris HPP di P&L, (c) figur HPP per-unit/per-proyek di Dashboard (`UnitPLRow` — pendapatan & HPP
per unit dari baris ledger ber-tag unit yang sudah posted).

### 13.8 Revenue & Expense report

Tidak ada halaman laporan berdiri sendiri untuk keduanya — visibilitas datang dari bagian Pendapatan/Beban di
P&L, plus `RevenueByPaymentTypeRow` (breakdown by `kpr`/`tunai`) yang memberi makan Dashboard "Cash vs KPR",
dan breakdown per-proyek via P&L per-proyek.

### 13.9 Fixed Asset / Depreciation report

Bukan bagian `internal/reporting` — halaman Register + Run Penyusutan sendiri dimiliki `internal/fixedasset`.

### 13.10 Sales / Performance / Pipeline

- **Pipeline** — funnel unit (Available/Reserved/Sold, jumlah & nilai), `TotalAdvance` (fee booking diterima),
  `ProjectedRevenue` (Σ harga list unit available+reserved). Snapshot, bukan time-series.
- **Sales Performance** & **Admin Marketing Performance** — figur per-orang (paralel satu sama lain).
- **Contract Workload** — jumlah kontrak per admin/salesperson.
- **KPR Pipeline** — funnel khusus KPR (draft→signed→akad→disbursed), terpisah dari Pipeline umum.

Set PDF export: Neraca, Laba/Rugi (+ per-proyek), Arus Kas, Neraca Saldo, Pajak, Pipeline.

---

<a id="14-end-to-end-user-manual-at"></a>
## 14. End-to-End User Manual (A–T)

| # | Langkah | Menu/Halaman | Yang diisi | Prasyarat | Dampak akuntansi | Jurnal |
|---|---|---|---|---|---|---|
| A | Buat Proyek | Proyek → Buat Baru | Nama, lokasi, `tax_category` default | — | Tidak ada | — |
| B | Buat Unit/Block | Proyek → tab Unit | Kode unit, `land_area`, `saleable_area`, `unit_type`, harga | Proyek ada | Tidak ada | — |
| C | Susun RAB | RAB → Buat Rencana | Item per kategori/subkategori biaya | Proyek ada | Tidak ada | — |
| D | Approve RAB | RAB → Detail → Approve | — | RAB draft lengkap | **Tidak ada** (status-flip murni) | — |
| E | Catat Biaya Realisasi | Biaya → Cost Entry | Kategori, tier, (subkategori bila hard/shared), jumlah, sumber dana, opsional tautan RAB | Sumber dana valid | Ya | `Dr Persediaan/Beban / Cr Kas-Bank-Hutang` |
| F | Jalankan Alokasi (opsional) | Proyek → Alokasi → Execute | — | Ada shared cost terposting | Tidak ada (hanya hitung snapshot) | — |
| G | Booking Unit | Penjualan → Booking | Customer, sales person, fee (boleh 0), refundable y/t, tanggal | Unit `available` | Ya (bila fee>0) | §6.2 |
| H | Konversi ke Kontrak | Booking → Konversi | Skema pembayaran, data kontrak | Booking aktif | Tidak langsung (bundel Kelebihan Tanah bila ada) | — |
| I | (Opsional) Tambah Kelebihan Tanah | Kontrak → Kelebihan Tanah | Luas m², harga jual | Stok tersedia | Bundled dalam Akad | §7.5 |
| J | Rekam DP/Cicilan pra-Akad | Penjualan → Pembayaran | Jumlah, sumber dana | Kontrak ada | Ya | `Dr Kas-Bank / Cr 2-2000` |
| K | Rekam Akad (BAST) | Penjualan → Akad | Tanggal, konfirmasi skema | Gate skema terpenuhi | Ya, pendapatan+HPP+split KPR sekaligus | §6.6 |
| L | Ajukan/Setujui KPR | KPR → Pipeline | Data pengajuan bank | Akad tercatat (skema KPR) | Tidak ada sampai dicairkan | — |
| M | Rekam Pencairan KPR (tahap) | Penjualan → Pembayaran (sumber: Pencairan KPR) | Jumlah tahap | `scheme_state` = akad/disbursed | Ya | §6.8 |
| N | Terbitkan Invoice Kekurangan | Billing → Invoice | (otomatis/manual CTA) | Pencairan < piutang | Ya, invoice | — |
| O | Rekam Pelunasan Kekurangan | Penjualan → Pembayaran | Jumlah, sumber dana | Invoice kekurangan terbit | Ya | `Dr Kas-Bank / Cr 1-2000` |
| P | Serah Terima Fisik | Unit → Handover | Tanggal | Unit `sold` | **Tidak ada** (murni catatan) | — |
| Q | Bayar Vendor | Hutang Usaha → Pembayaran | Invoice/retensi/uang muka, sumber dana | Invoice AP ada | Ya | §10.1 |
| R | Hitung & Bayar Komisi | Komisi → Kalkulasi | Rule komisi, sale record | Akad tercatat | Ya | §11.4 |
| S | Akuisisi & Susutkan Aset Tetap | Aset Tetap → Register / Run Penyusutan | Kategori, harga perolehan, masa manfaat | — | Ya | §12.2, §12.3 |
| T | Tutup Buku Tahunan | Accounting → Periode/Tutup Tahun | Tahun fiskal | Semua bulan tahun tsb sudah posting | Ya, 2 jurnal | §2.9 |

---

<a id="15-accounting-journal-reference"></a>
## 15. Accounting Journal Reference

Semua kode akun di tabel ini diambil dari COA live — lihat Appendix A untuk daftar lengkap.

| Transaksi | Debit | Credit | Dampak |
|---|---|---|---|
| Booking fee (>0, default) | Kas/Bank (`1-1xxx`/`1-1xxx`) | `4-2100` Pendapatan Booking | Pendapatan langsung, final |
| Booking fee (>0, refundable) | Kas/Bank | `2-2100` Titipan Booking | Kewajiban, belum pendapatan |
| Booking fee = 0 | — | — | Tanpa jurnal |
| DP / cicilan pra-Akad | Kas/Bank | `2-2000` Uang Muka Penjualan | Kewajiban |
| Cicilan pasca-Akad (non-KPR) | Kas/Bank | `1-2000` Piutang Usaha | Turunkan piutang |
| Akad — split KPR | `2-2000` (house advance) / `1-2200` (financing) / `1-2000` (sisa) | `4-1000`* / `2-3000` (PPN bila PKP) | Pengakuan pendapatan + split piutang |
| Akad — HPP | `5-1000` HPP | `1-3000` Tanah / `1-3100` Hard Cost | Pengakuan HPP, kurangi Persediaan |
| Pencairan KPR (tahap) | Kas/Bank | `1-2200` Dana Jaminan Bank (dibatasi saldo) | Turunkan saldo Dana Jaminan Bank |
| Kekurangan KPR (invoice) | `1-2000` Piutang Usaha | (tidak ada jurnal kas sampai dibayar) | Tagihan tambahan ke buyer |
| Biaya vendor — HPP (direct/shared, land/hard) | `1-3000`/`1-3100` Persediaan | `2-1000` Hutang Usaha / Kas-Bank | Tambah Persediaan |
| Biaya operasional (overhead) | `5-3xxx`/`5-4xxx`/`5-4600`/`5-4700` Beban | Kas-Bank / `2-1000` | Beban periode langsung |
| Alokasi Tanah (shared→unit) | (internal, tidak menghasilkan jurnal baru — hanya snapshot alokasi) | — | Menentukan porsi HPP per unit saat Akad |
| Pengakuan HPP saat penjualan | `5-1000` HPP | `1-3000`/`1-3100` Persediaan | Lihat §3.6, §6.6e |
| True-Up HPP (Δ>0) | `5-1000` HPP | `1-3xxx` Persediaan | Koreksi tambah HPP |
| True-Up HPP (Δ<0) | `1-3xxx` Persediaan | `5-1000` HPP | Koreksi kurangi HPP |
| PPh Final (akrual) | `5-2000` Beban PPh Final Pengalihan | `2-4000` Hutang PPh Final Pengalihan | Beban pajak + kewajiban |
| PPN Keluaran (bila PKP) | (bagian dari Dr gross penjualan) | `2-3000` PPN Keluaran | Kewajiban PPN |
| Refund booking (pengakuan) | `2-2100` Titipan Booking | `2-2200` Hutang Refund | Kewajiban refund |
| Refund (pencairan) | `2-2200` Hutang Refund | Kas/Bank | Kurangi kewajiban refund |
| Komisi (accrual) | `5-3100` Beban Komisi Penjualan | `2-6200` Utang Komisi | Beban komisi |
| Komisi (bayar) | `2-6200` Utang Komisi | Kas/Bank | Lunasi komisi |
| Komisi (clawback) | `1-2100` Piutang Lain-lain | `5-3100` Beban Komisi Penjualan | Tarik kembali komisi |
| Akuisisi aset tetap | `1-4000`/`1-4100`/`1-4200` (per kategori) | Kas/Bank | Tambah aset |
| Penyusutan (bulanan) | `5-4500` Beban Penyusutan | `1-4900` Akumulasi Penyusutan | Beban periode |
| Kelebihan Tanah — pendapatan | Kas/Bank (standalone) atau `1-2000` (bundled) | `4-1100` Pendapatan Penjualan Tanah / `2-3000` PPN | Pendapatan |
| Kelebihan Tanah — HPP | `5-1000` HPP | `1-3000` Persediaan Tanah | HPP |
| Tutup buku tahunan (Entry A) | `4-1xxx`/`4-2xxx` (nolkan semua akun nominal) | `5-1xxx`..`5-5xxx` (nolkan) / `3-3000` (selisih laba, Cr) | Pindahkan saldo nominal ke ekuitas sementara |
| Tutup buku tahunan (Entry B) | `3-3000` (bila laba) | `3-2000` Laba Ditahan | Pindahkan laba tahun berjalan ke laba ditahan |

*akun pendapatan diresolusi per-produk lewat Product Catalog, `4-1000` adalah default properti rumah.

---

<a id="16-troubleshooting--business-rules"></a>
## 16. Troubleshooting & Business Rules

### 16.1 `land_area` wajib untuk alokasi HPP Tanah

Error: `ErrLandAreaMissing`. Penyebab: unit ikut alokasi Tanah shared tapi `land_area <= 0`. Solusi: isi
`land_area` unit sebelum menjalankan alokasi atau sebelum Akad (bila HPP-nya bergantung pool shared).

### 16.2 Periode tertutup

Posting dengan `EntryDate` jatuh di bulan yang sudah dikunci (`AccountingPeriod`) akan ditolak oleh
`WithPeriodChecker`. Solusi: gunakan tanggal di periode terbuka, atau buka kembali periode (kewenangan admin).

### 16.3 Duplikasi posting / idempotency

Kunci idempotensi yang berulang pada `ReceivePayment` TIDAK menghasilkan jurnal/kwitansi baru — mengembalikan
hasil yang sudah ada. Bila melihat "tidak ada perubahan" pada retry, ini perilaku BENAR, bukan bug.

### 16.4 Oversell Kelebihan Tanah

`ErrCapacityExceeded` — kuantitas yang diminta melebihi `AvailableQuantityM2()`. Dilindungi row-lock +
CHECK constraint DB — tidak bisa di-race meski dua request bersamaan.

### 16.5 Isolasi tenant

Query lintas tenant harus selalu nol hasil (dijaga filter manual `tenant_id = ?` per repository — lihat
discrepancy 0.2-A vs CLAUDE.md). Bila menemukan repository baru yang lupa filter ini, itu bug serius yang
melanggar invariant #6.

### 16.6 Sumber dana tidak valid/tidak cukup

Setiap pemilihan akun kas/bank divalidasi terhadap `ledger.ValidatePaymentAccount()` (berbasis role COA, bukan
hardcode) — kode akun yang bukan `category IN (cash,bank)` ditolak di titik input, tidak lolos ke posting.

### 16.7 KPR: committed vs actual disbursement

`bankApprovedAmount` (plafon disetujui) ≠ jumlah yang benar-benar sudah cair. `1-2200` mewakili plafon yang
belum sepenuhnya cair; setiap pencairan mengurangi saldo `1-2200` sampai `ErrDisbursementExceedsFinancing`
bila mencoba mencairkan lebih dari sisa saldo — jangan asumsikan `1-2200` otomatis nol saat status
`disbursed` tercapai (F-6).

### 16.8 Booking fee = 0

Ingat: fee 0 **tidak menghasilkan termin/jurnal/kwitansi apa pun** — ini BUKAN error/silent-failure, ini
perilaku yang disengaja (migrasi `000104`).

### 16.9 RAB approved ≠ actual cost; actual cost ≠ otomatis seluruh RAB

- RAB approved hanyalah anggaran (§4.2) — tidak pernah otomatis jadi biaya aktual.
- Biaya aktual yang ditautkan ke RAB item bisa 0%, sebagian, atau melebihi 100% dari yang disetujui — sistem
  tidak memaksa keduanya sama, hanya menghitung rasio realisasi (§4.4).
- HPP saat Akad memakai POOL RAB approved (budgeted, §3.6/§0.2-B) — BUKAN Σ biaya aktual yang sudah tertaut
  saat itu. Jangan bingungkan "realisasi 30%" (§4.4, ukuran progress fisik/biaya) dengan "HPP yang diakui saat
  Akad" (§3.6, selalu pool penuh budgeted, direkonsiliasi belakangan via True-Up).

### 16.10 Pemasaran/Lain-lain BUKAN HPP

Ditegaskan berkali-kali di dokumen ini karena ini area yang paling mudah salah asumsi: `marketing`, `other`,
`operational`, dan `soft` **tidak pernah** masuk Persediaan — selalu beban periode (§3.1, §9).

### 16.11 Status test suite per 2026-09-10 (hasil regression run sesi ini)

Hasil `go test -tags integration ./... -count=1 -p 1`:

| Test | Status | Penjelasan |
|---|---|---|
| `TestIntegration_Refund_FromBooking` | **FAIL** — `2-2100 = -10000000.0000, want 0.0000` | Kemungkinan **defect nyata** di jalur pembatalan booking *refundable* (bukan jalur default). Perlu investigasi terpisah sebelum jalur ini diklaim berfungsi. |
| `TestW10_BatasJalurOperasional/...` | FAIL — pesan error tidak cocok ekspektasi | Test mengharapkan `ErrExpenseTypeNotForProjectCost`, tapi validasi `hard_subcategory` (ditambahkan migrasi 000103) menembak lebih dulu. Tampak seperti **utang teknis fixture test**, bukan bug bisnis — perilaku produksi (menolak request tanpa subkategori) justru BENAR. |
| `TestResolvePrintTarget_BKKMenembusKePemilikJurnal` | FAIL — FK constraint `ap_payments.journal_entry_id` | Isu **setup fixture test** (seed data tidak menyediakan journal_entry_id valid), bukan bug logika bisnis. |
| `TestAcceptance_FullScenario_8Rules`, `TestIntegration_BAST_Budgeted_SnapshotAndPosting`, `TestIntegration_BAST_WritesUnitTransitionLog`, `TestIntegration_BAST_RejectedWhenUnitNotBASTReady`, `TestIntegration_NonProperty_DoesNotDiluteHPP`, `TestIntegration_NonProperty_RevenueWithoutCOGS`, `TestIntegration_UnregisteredUnitType_BASTRejected` | FAIL — semua dengan pesan sama: "subcategory Konstruksi wajib salah satu dari: produksi_subsidi\|produksi_komersial\|sarana_prasarana\|perizinan" | **Utang teknis fixture test** — semua ditulis sebelum migrasi 000103 mewajibkan subkategori Konstruksi. Kode produksi justru BENAR menolaknya (itulah sebabnya test gagal) — bukan indikasi fitur BAST/HPP rusak. |

**Kesimpulan untuk pembaca dokumen ini**: mayoritas kegagalan test adalah fixture yang perlu diperbarui
mengikuti aturan subkategori baru — bukan indikasi bug produksi. Satu-satunya yang layak dicurigai sebagai
defect produksi sungguhan adalah jalur refund booking *refundable*.

### 16.12 Catatan kode kecil (bukan bug fungsional, tapi berpotensi membingungkan)

- `internal/land/akad.go` — validasi `DPPAmount` non-negatif mengembalikan error bernama `ErrQuantityNegative`
  (nama yang terbaca seolah tentang `QuantityM2`, bukan `DPPAmount`) — kemungkinan artefak copy-paste, tidak
  memengaruhi perilaku.
- `internal/scheme/policies.go` masih menyimpan komentar yang mendeskripsikan perilaku reklas-otomatis lama
  yang sudah dicabut (lihat §0.2-C) — jangan jadikan rujukan, ikuti `scheme_flow.go` yang aktif.

---

<a id="17-technical-architecture"></a>
## 17. Technical Architecture

### 17.1 Struktur backend (29 package `internal/*`)

```
allocation  — Mesin alokasi HPP Tanah+Konstruksi (land pool + hard pool), hasilkan snapshot per unit.
ap          — Hutang Usaha: vendor payable lahir dari cost entry terposting; invoice+pembayaran vendor.
approval    — Mesin approval workflow generik (quorum/threshold, evaluasi dari snapshot beku) — konsumen: RAB.
billing     — Invoice, kwitansi, pencatatan penagihan/pembayaran, dokumen cetak.
budget      — RAB (BudgetPlan/BudgetItem): siklus draft→approved→superseded; tagging sumber jurnal kapitalisasi.
cancellation— Pembatalan booking & pasca-BAST, reversal siklus hidup unit.
charge      — Charge Group: tagihan realisasi (Titipan Realisasi) & produk tambahan non-properti.
closing     — (nama menyesatkan) HPP True-Up + Project Completion; BUKAN tutup buku periode.
commission  — Mesin komisi sales/admin-marketing.
cost        — Cost entry (realisasi aktual), taksonomi cost_tier/category, sumber routing akun.
crm         — Lead/Prospect ringan (pra-penjualan).
customer    — Master data Customer/Buyer.
document    — Mesin penomoran dokumen terpusat (KWT/BKK/KWB dll) + klasifikasi jurnal-kas→kwitansi.
domain      — Value object murni (Money/decimal, enum CostCategory) — tanpa DB/HTTP, diimpor semua, tidak
              mengimpor apa pun secara internal (akar layering ketat).
fixedasset  — Register Aset Tetap + penyusutan garis lurus (v1).
histfin     — Snapshot laporan keuangan historis, di luar ledger hidup (laporan akhir tahun beku).
land        — Kelebihan Tanah sebagai produk/stok yang bisa dijual berdiri sendiri.
ledger      — Chart of Accounts, posting/query jurnal, AccountRoleRegistry (SSOT klasifikasi), trial balance.
legacyar    — Saldo piutang proyek lama yang diimpor (tanpa jurnal), diselesaikan lewat mesin piutang yang sama.
notary      — Pelacakan titipan notaris (jalur intake sudah ditutup, hanya historis/settlement).
platform    — Cross-cutting: koneksi DB, config, HTTP server/middleware, auth.
printkit    — Satu sumber styling cetak/PDF untuk semua dokumen.
project     — Master data Project/Unit/Block + Katalog Produk (taksonomi tipe produk).
receivable  — Satu pemilik perhitungan AR/aging — dipakai reporting, billing, legacyar, land.
reporting   — Seluruh pembuatan laporan finansial: Neraca, L/R, Arus Kas, Neraca Saldo, AR Aging, Pipeline, Dashboard.
sale        — Booking, konversi kontrak, Akad, termin, jadwal tagihan (pra-BAST).
salesorg    — Master data Tim Sales/Sales Person.
scheme      — Registry PaymentSchemePolicy (Tunai Keras/Bertahap, In-House, KPR Komersial/Subsidi) + TermsSnapshot.
tax         — TaxRuleResolver + seam TaxFormula (perhitungan PPh Final).
tenant      — Model/plumbing scope tenant.
```

### 17.2 Layering & aturan import

```
internal/domain    → tidak boleh import package lain di internal (akar)
internal/platform   → DB, config, HTTP server, middleware
internal/*/repository.go → akses DB, filter tenant_id manual eksplisit
internal/*/service.go    → business logic; tidak akses DB langsung
internal/*/handler.go    → HTTP handler; tidak ada business logic
```
Semua package boleh import `domain`. Tidak ada import siklik.

### 17.3 Struktur frontend

`frontend/app/(app)/` (Next.js App Router, grouped layout): `accounting`, `biaya`, `dashboard`, `laporan`,
`pajak`, `pengaturan`, `penjualan`, `proyek`, `rab`, `styleguide` — plus route publik (`login`, `register`,
`api`) di luar grup `(app)`. Komponen diorganisasi per fitur di `frontend/components/<domain>/`, mencerminkan
batas package backend.

### 17.4 Alur posting jurnal (end-to-end)

Handler → Service (business logic + validasi domain) → memanggil `ledger.PostingService.Create`/`CreateAndPost`
di dalam `db.Transaction(...)` yang sama dengan perubahan tabel bisnis lain → `validateLines` (Σ Dr = Σ Cr) →
opsional `WithPeriodChecker` (tolak bila periode terkunci) → tulis `journal_entries`+`journal_lines` →
(bila menyentuh kas/bank) `document_issuer` menerbitkan nomor dokumen dalam transaksi yang sama → commit
tunggal. Kegagalan di titik mana pun me-rollback SELURUH transaksi (termasuk baris bisnis non-jurnal).

### 17.5 Isolasi tenant (implementasi aktual)

**Discrepancy vs CLAUDE.md** (lihat §0.2-A): bukan GORM global scope, melainkan filter eksplisit
`WHERE tenant_id = ?` di setiap query repository. Terverifikasi benar oleh integration test yang memeriksa
query lintas-tenant selalu mengembalikan nol baris.

### 17.6 File & titik-masuk penting bagi developer baru

- `internal/domain` — Money/decimal wrapper, enum `CostCategory` — baca ini duluan, diimpor semua modul.
- `internal/ledger/posting_service.go` — satu-satunya mesin posting, baca sebelum menyentuh modul apa pun
  yang menghasilkan jurnal.
- `internal/ledger/account_role.go` — SSOT klasifikasi P&L/Neraca; rujuk `docs/financial-canonical-registry.md`.
- `internal/receivable` — SSOT AR/aging lintas modul.
- `internal/printkit` — satu sumber styling semua dokumen cetak.

### 17.7 Migrasi penting yang relevan akuntansi (dari 104 migrasi, 208 file up/down)

```
000027_allocation_snapshots         — bekukan breakdown HPP unit di satu titik waktu
000029-000032 (closing/P0-4)        — tabel internal/closing (Project Completion + True-Up)
000058                              — hardening Katalog Produk / aturan ProductCategory kanonik
000059                              — Billing Batch 2 Charge Group
000061/000062                       — Master Jenis Biaya Realisasi (intake notaris ditutup)
000065                              — Mesin Penomoran Dokumen
000066                              — INV-DOC-1 unique key bukti kas
000072                              — Transaksi Pengeluaran, satu pintu masuk
000074                              — Identity & RBAC (users.name)
000098_budget_plan_capitalization   — kapitalisasi full-RAB DITAMBAHKAN
000099_hpp_2_category_freeze        — HPP dibekukan ke aturan 2-kategori (Tanah + Konstruksi)
000102_drop_budget_plan_capitalization — kapitalisasi full-RAB DIHAPUS EKSPLISIT (bukti kode F-2)
000103_construction_subcategory     — tambah produksi_subsidi/produksi_komersial/sarana_prasarana/perizinan
000104_booking_fee_zero             — izinkan booking_fee = 0
```
Pasangan `000098→000102` adalah bukti paling jelas di level migrasi bahwa kapitalisasi full-RAB pernah
diimplementasikan lalu sengaja dibatalkan — mendukung F-2.

### 17.8 Strategi testing

Backend: `go test ./...` untuk unit test. Integration test digerbang `//go:build integration`, dijalankan via
`go test -tags integration ./... -p 1`. **`-p 1` wajib** — dikonfirmasi live sesi ini: package seed tenant ID
tetap (bukan acak) yang bisa bentrok lintas package bila dijalankan paralel terhadap satu instance MySQL yang
sama. Frontend: `node --experimental-strip-types --test components/**/*.test.ts` — unit test fungsi murni saja
(tanpa bundler/Jest), file `*.test.ts` bersanding dengan source-nya. Tidak ditemukan direktori
`.github/workflows` di backend/root repo — tidak terkonfirmasi ada CI (bukan bukti definitif tidak ada, hanya
tidak ditemukan lewat pengecekan sederhana).

---

<a id="18-glossary"></a>
## 18. Glossary

**Akad / BAST** — peristiwa serah-terima legal/finansial unit; titik pengakuan pendapatan & HPP sekaligus.
Berbeda dari serah terima FISIK (§6.6g).

**AR (Accounts Receivable) / Piutang** — kewajiban pembeli ke perusahaan; satu mesin agregasi
(`internal/receivable`) untuk 4 sumber (house/realization/addon/legacy).

**BAST** — lihat Akad.

**Basis Alokasi** — atribut unit yang dipakai sebagai bobot pembagian pool biaya shared: `saleable_area` atau
`sales_value` (Land selalu `land_area`, tidak ikut pilihan basis proyek).

**BudgetItem / RAB item** — satu baris rencana anggaran biaya di dalam `BudgetPlan`.

**BudgetPlan (RAB)** — rencana anggaran biaya proyek, siklus `draft→approved→superseded`.

**Cost Entry** — satu baris pencatatan biaya aktual, punya `CostCategory`+`CostTier`(+`ConstructionSubcategory`).

**CostCategory** — klasifikasi jenis biaya: `land`, `hard`, `soft`, `marketing`, `other`, `operational`. Hanya
`land`+`hard` yang jadi HPP.

**CostTier** — `direct` (per unit), `shared` (pool project-wide), `overhead` (beban, tidak pernah dikapitalisasi).

**Dana Jaminan Bank** — akun `1-2200`, plafon KPR yang sudah disetujui bank tapi belum sepenuhnya dicairkan.

**Document Number / Nomor Dokumen** — nomor bukti transaksi kas wajib (INV-DOC-1), format
`{prefix}/{tahun}/{nomor}`.

**HPP (Harga Pokok Penjualan)** — biaya yang diakui sebagai beban saat unit/tanah terjual; sama dengan biaya
terakumulasi unit pada saat itu (budgeted-first, direkonsiliasi via True-Up — §0.2-B).

**Kelebihan Tanah** — sisa tanah proyek yang dijual sebagai produk/stok berdiri sendiri (bukan unit properti).

**Kwitansi (KWT/KWB/dll)** — dokumen bukti kas bernomor, terbit atomic dengan jurnal kas terkait.

**Ledger** — kumpulan `journal_entries`+`journal_lines`, satu-satunya sumber kebenaran finansial.

**PPh Final Pengalihan** — pajak final atas pengalihan hak properti; 1,0% (subsidi) / 2,5% (komersial) dari DPP.

**PPN Keluaran** — pajak pertambahan nilai atas penjualan, diposting ke `2-3000` bila kontrak berstatus PKP.

**Persediaan Real Estat** — akun aset (`1-3000` Tanah, `1-3100` Hard Cost) yang menampung biaya yang sudah
dikapitalisasi sebelum unit terjual.

**Posting** — proses menuliskan jurnal ke ledger secara permanen (immutable) lewat `PostingService`.

**PostingService** — satu-satunya mesin posting jurnal di seluruh sistem (`internal/ledger`).

**RAB** — lihat BudgetPlan.

**Realisasi** — rasio biaya aktual yang sudah ditautkan terhadap RAB item yang approved.

**Reversal** — jurnal pembalik; satu-satunya cara sah mengoreksi jurnal yang sudah posted (Invariant #5).

**Skema Pembayaran (Payment Scheme)** — kebijakan yang mengatur state-machine & routing piutang kontrak:
Tunai Keras, Tunai Bertahap, In-House, KPR Komersial, KPR Subsidi.

**True-Up** — proses rekonsiliasi HPP budgeted → HPP aktual saat proyek selesai (`internal/closing`).

**Uang Muka Penjualan** — akun `2-2000`, kewajiban atas kas yang diterima sebelum kriteria pengakuan
pendapatan terpenuhi (Invariant #7).

---

## Appendix A — Full Chart of Accounts (live, 53 akun)

```
1-1100 Kas — Kas Besar                              (asset/cash)
1-1200 Kas — Petty Cash                              (asset/cash)
1-1300 Bank — BCA                                    (asset/bank)
1-1400 Bank — Mandiri                                (asset/bank)
1-1500 Bank — BRI                                    (asset/bank)
1-2000 Piutang Usaha                                 (asset/other_asset)
1-2100 Piutang Lain-lain                             (asset/other_asset)
1-2200 Dana Jaminan Bank (KPR)                       (asset)
1-3000 Persediaan Real Estat — Tanah                 (asset/other_asset)
1-3100 Persediaan Real Estat — Hard Cost             (asset/other_asset)
1-3200 Persediaan Real Estat — Soft Cost             (asset/other_asset)  [LEGACY/MATI]
1-3300 Persediaan Real Estat — Biaya Pembiayaan      (asset/other_asset)  [LEGACY/MATI]
1-4000 Aset Tetap — Peralatan Kantor                 (asset/other_asset)
1-4100 Aset Tetap — Kendaraan                        (asset/other_asset)
1-4200 Aset Tetap — Gedung/Bangunan                  (asset/other_asset)
1-4900 Akumulasi Penyusutan                          (asset/other_asset, kontra)
1-5000 Biaya Dibayar di Muka                         (asset/other_asset)
1-5100 PPN Masukan                                   (asset/other_asset)
1-5300 Uang Muka Vendor                              (asset/other_asset)
2-1000 Hutang Usaha                                  (liability)
2-1100 Hutang Retensi Kontraktor                     (liability)
2-2000 Uang Muka Penjualan                           (liability)
2-2100 Titipan Booking                               (liability)
2-2200 Hutang Refund                                 (liability)
2-2300 Titipan Notaris                               (liability)
2-2400 Titipan Realisasi                             (liability)
2-3000 PPN Keluaran                                  (liability)
2-4000 Hutang PPh Final Pengalihan                   (liability)
2-5000 Hutang Bank                                   (liability)
2-6000 Biaya Akrual                                  (liability)
2-6100 Hutang Gaji & Tunjangan                       (liability)
2-6200 Utang Komisi                                  (liability)
3-1000 Modal Disetor                                 (equity)
3-2000 Laba Ditahan                                  (equity)
3-3000 Laba/Rugi Tahun Berjalan                      (equity, akun penutup)
4-1000 Pendapatan Penjualan Unit                     (revenue)
4-1100 Pendapatan Penjualan Tanah                    (revenue)
4-2000 Pendapatan Lain-lain                          (revenue)
4-2100 Pendapatan Booking                            (revenue)
5-1000 Harga Pokok Penjualan / HPP                   (expense)
5-2000 Beban PPh Final Pengalihan                    (expense)
5-3000 Beban Pemasaran                               (expense)
5-3100 Beban Komisi Penjualan                        (expense)
5-3200 Beban Provisi & Administrasi Bank KPR         (expense)
5-4000 Beban Umum & Administrasi                     (expense)
5-4100 Beban Gaji & Tunjangan                        (expense)
5-4200 Beban Sewa Kantor                             (expense)
5-4300 Beban Utilitas                                (expense)
5-4400 Beban Perjalanan Dinas                        (expense)
5-4500 Beban Penyusutan                              (expense)
5-4600 Beban Operasional                             (expense)
5-4700 Beban Soft Cost (Desain & Legal)              (expense)
5-5000 Beban Bunga                                   (expense)
```

---

*Dokumen ini disusun murni dari pembacaan source code, migrasi, dan database live — tanpa perubahan kode
apa pun. Setiap temuan discrepancy ditandai eksplisit di Bagian 0.2 dan di badge ⚠️ pada bagian terkait. Bila
kode berubah di kemudian hari, bagian yang paling rentan basi lebih dulu adalah: tarif pajak (§8.1), daftar
akun (Appendix A), dan status implementasi fitur groundwork-only (disposal aset §12.4, formula pajak non-
proporsional §8.7) — verifikasi ulang terhadap kode sebelum mempercayai bagian tersebut di masa depan.*
