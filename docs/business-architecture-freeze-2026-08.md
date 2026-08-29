# Business Architecture Freeze & Gap Analysis — esaProperti

**Tanggal:** 2026-08-06
**Status:** REVIEW — menunggu persetujuan. **Tidak ada kode, migrasi, atau perubahan file sumber yang dilakukan untuk dokumen ini.**
**Basis:** 6 blok keputusan bisnis final klien (2026-08-06) + pembacaan menyeluruh `backend/internal/**`, `backend/migrations/**`, `frontend/**`.
**Metode:** read-only. Setiap klaim di bawah ini punya rujukan `file:line`.

---

## §0 · Ringkasan eksekutif

### 0.1 Verdict

**Domain BELUM siap di-freeze.** Bukan karena arsitektur salah secara fundamental — fondasinya (ledger append-only, registry peran akun, taxonomy produk fail-closed, snapshot HPP, approval engine) justru sehat — tetapi karena **6 keputusan final klien memiliki kontradiksi langsung dengan kode yang berjalan hari ini**, dan **1 keputusan (§6 Dokumen) belum punya rumah arsitektural sama sekali.**

| # | Keputusan final klien | Status sistem hari ini | Severity |
|---|---|---|---|
| §1 | Kelebihan Tanah = produk jual, punya HPP, kurangi stok tanah | `kelebihan_tanah` = `non_property` → **tidak ikut HPP**, revenue ke `4-2000`; **tidak ada konsep stok tanah** sama sekali | 🔴 BLOCKER |
| §1 | Tidak ada Kavling | `kavling` masih di seed default product types | 🟡 Ringan |
| §2 | PDAM/Listrik/BPHTB/Notaris = TITIPAN | `pdam` masih **produk jual** (akui pendapatan saat BAST); Listrik & BPHTB **tidak ada master-nya** | 🔴 BLOCKER |
| §2 | Titipan bisa dibayar sebagian / lebih bayar / refund / dialihkan | ✅ **Sudah lengkap** (K-1..K-5, `ChargeSettlement`) | ✅ |
| §2 | Kwitansi tampilkan 4 angka | ✅ **Sudah** (KWR, `receipt_print.go:290-324`) | ✅ |
| §2 | Biaya realisasi **bukan lagi** syarat BAST | `charge.CheckBAST` + kebijakan tenant `require_realization_settled` masih hidup | 🔴 BLOCKER |
| §2 | Sisa setelah BAST → **Piutang Customer** | **Tidak ada mekanisme apa pun** — tanpa jurnal, tanpa trigger, tidak masuk aging, tidak masuk statement | 🔴 BLOCKER |
| §3 | Booking langsung Pendapatan Booking, non-refundable, batal tetap pendapatan | ✅ **Sudah** (`4-2100`, `FeeRecognized`) — sisa permukaan usang: deskripsi COA `2-2100` & field DTO `refundable` | 🟡 Ringan |
| §4 | Kekurangan pencairan KPR → Piutang Customer | ✅ **Sudah** (T-3, `ResolveReceivableAccount`) | ✅ |
| §5 | Admin kelola Master Produk & Master Jenis Biaya Realisasi tanpa programmer | Master Produk: CRUD **parsial** (tanpa DELETE/nonaktif tuntas). Master Jenis Biaya Realisasi: **tidak ada** — label teks bebas + hardcode di frontend | 🔴 BLOCKER |
| §6 | Setiap perpindahan uang wajib dokumen bernomor per jenis | Kas masuk: 4 jenis dokumen (KWT/KWB/KWR/MTI + INV). **Kas keluar: NOL dokumen.** Tidak ada entitas Dokumen; prefix hardcode di Go; 2 mesin penomoran berbeda | 🔴 BLOCKER |

**Blocker: 6. Ringan: 2. Sudah selesai: 4.**

### 0.2 Temuan struktural terbesar

Tiga hal yang, kalau tidak diputuskan sekarang, akan memaksa refactor besar lagi di kemudian hari:

1. **Tidak ada bounded context "Dokumen".** Keputusan §6 bukan fitur — itu **invariant baru setingkat "jurnal selalu balanced"**. Hari ini kwitansi adalah artefak modul Billing, bukan konsep lintas-sistem. Menambahkan dokumen kas keluar satu per satu ke 6 modul yang berbeda akan mereplikasi masalah yang sama enam kali. Ini butuh konteks sendiri (§4, §7).

2. **`internal/charge` menulis langsung ke agregat `internal/sale`.** `charge/service.go` membuat baris `sale.TerminPayment` dan `sale.PaymentAllocation` sendiri (baris 325, 440-470, 806, 907-932, 975-1054), dan mengimpor `internal/reporting` (konteks read-model) dari dalam jalur transaksional. Ini pelanggaran bounded context yang nyata, dan justru jalur inilah yang harus dimodifikasi untuk keputusan §2 (sisa titipan → piutang).

3. **Piutang customer terpecah dua dan tidak pernah bertemu.** `reporting.BuildARAging` hanya membaca `payment_schedules`; `charge.AgingReport` membangun aging kedua dari `charge_items`. `sale.CustomerStatement` tidak punya bagian titipan sama sekali. Setelah keputusan §2 ("sisa titipan → piutang customer") dan §4 ("kekurangan KPR → piutang customer"), **satu customer bisa punya tiga sumber piutang yang tidak pernah dijumlahkan di satu layar mana pun.**

---

## §1 · Final Business Domain Model

### 1.1 Peta agregat final

```
                         ┌──────────────────────────────────────┐
                         │  MASTER (dikelola admin, tanpa dev)  │
                         │  · Account (COA)                     │
                         │  · ProductType      ← produk jual     │
                         │  · RealizationChargeType ← titipan ★  │
                         │  · DocumentType ★                     │
                         │  · PaymentScheme · TaxRule            │
                         │  · CommissionRule · ApprovalWorkflow  │
                         └──────────────────────────────────────┘
                                        │ dirujuk (kode, tidak disalin)
        ┌───────────────────────────────┼───────────────────────────────┐
        ▼                               ▼                               ▼
┌───────────────┐            ┌────────────────────┐          ┌──────────────────┐
│ Project       │  1───n     │ Unit (= "Produk")  │          │ Customer         │
│ (aggregate    │            │ · product_type     │          │ · Lead           │
│  root proyek) │            │ · status (9)       │          └──────────────────┘
│ · Phase       │            │ · luas, harga      │                   │
│ · BudgetPlan  │            └────────────────────┘                   │
│ · CostEntry   │                     │ 1───1                          │
│ · Allocation  │                     ▼                                ▼
│ · Progress    │            ┌─────────────────────────────────────────────┐
└───────────────┘            │ SaleContract (aggregate root penjualan)     │
        │                    │ · PaymentScheme (KPR/tunai/cicilan)         │
        │ HPP                │ · PaymentSchedule[]                          │
        └───────────────────▶│ · TerminPayment[] → PaymentAllocation[]      │
        (AllocationSnapshot) │ · Booking (1, opsional, di luar harga)       │
                             │ · AllocationSnapshot (HPP beku saat BAST)    │
                             │ · Invoice[] · Receipt[]                      │
                             └─────────────────────────────────────────────┘
                                              │ 1───n
                                              ▼
                             ┌─────────────────────────────────────────────┐
                             │ ChargeGroup (aggregate root TITIPAN)        │
                             │ · ChargeItem[] ← RealizationChargeType ★    │
                             │ · ChargePayout[]  (bayar pihak ketiga)      │
                             │ · ChargeSettlement[] (refund/alih/void)     │
                             └─────────────────────────────────────────────┘

        Semua di atas menerbitkan → JournalEntry (append-only, balanced)
        Semua perpindahan uang    → Document ★ (bernomor, per jenis)

        ★ = belum ada / harus dibangun
```

### 1.2 Taksonomi produk — FINAL

Satu sumber kebenaran: `domain.ProductCategory` (`internal/domain/product_category.go`), yang menentukan `ParticipatesInHPP()` dan `ParticipatesInProjectProgress()`.

| Produk | Kategori final | HPP | Pendapatan | Kurangi stok | Status kode hari ini |
|---|---|---|---|---|---|
| **Rumah** | `property` | ✅ | `4-1000` | unit | ✅ benar |
| **Ruko** | `property` | ✅ | `4-1000` | unit | ✅ benar |
| **Kelebihan Tanah** | `property` ⚠️ | ✅ | akun sendiri (lihat D-3) | **luas tanah (m²)** | ❌ saat ini `non_property`, HPP dilewati |
| ~~Kavling~~ | — | — | — | — | ⚠️ masih di seed, harus dinonaktifkan (bukan dihapus) |
| ~~PDAM~~ | **bukan produk** | — | — | — | ❌ masih produk `non_property` → akui pendapatan `4-2000` saat BAST |

**Bukti kontradiksi Kelebihan Tanah** — `internal/sale/service.go:440-475`:

```go
if !policy.ParticipatesInHPP() {
    hppMethod, hppResolved = HPPMethodNone, true // hpp tetap nol
}
```
Selama `kelebihan_tanah` berkategori `non_property`, penjualannya **mengakui pendapatan penuh dengan HPP nol** — laba kotor overstated 100% pada produk itu.

**Konsekuensi arsitektur yang belum ada: stok tanah.**
`Project.LandArea` (`internal/project/model.go:60`) adalah angka statis dalam m². Tidak ada satu pun jalur kode yang menguranginya. Keputusan §1 ("mengurangi stok tanah") menuntut **inventarisasi tanah dengan kuantitas**, bukan hanya nilai rupiah di `1-3000`.

> **KEPUTUSAN TERBUKA D-1 — cara Kelebihan Tanah menyerap HPP.**
> Dua opsi, konsekuensinya berbeda jauh:
> - **D-1a (rekomendasi): Kelebihan Tanah adalah Unit ber-`saleable_area`.** Ia ikut mesin alokasi biaya yang sudah ada — HPP-nya keluar dari alokasi biaya tanah proyek berdasarkan m², persis seperti rumah. Tidak perlu mesin baru; hanya perlu kategori diubah ke `property` + basis alokasi disepakati. **Terikat langsung pada definisi `saleable_area` yang Bapak/Ibu tangguhkan** — inilah alasan penangguhan itu sekarang menjadi blocker.
> - **D-1b: buku stok tanah terpisah (m² tersedia / terjual / tersisa per proyek).** Lebih benar secara operasional (bisa laporan "sisa tanah 1.240 m²"), tetapi entitas + migrasi + rekonsiliasi baru terhadap `1-3000`.
>
> Saya perlu jawaban ini sebelum menulis kode apa pun untuk §1.

---

## §2 · Final Bounded Context

### 2.1 Peta konteks target (9 konteks)

| # | Bounded Context | Agregat root | Menerbitkan jurnal? | Package hari ini |
|---|---|---|---|---|
| 1 | **Catalog & Master** | Account, ProductType, RealizationChargeType★, DocumentType★ | ❌ tidak pernah | `ledger` (COA) + `project` (product types) — **tersebar** |
| 2 | **Project & Inventory** | Project (Phase, Unit, Progress, LandStock★) | ❌ | `project` |
| 3 | **Budget & Cost** | BudgetPlan, CostEntry, AllocationConfig/Snapshot | ✅ kapitalisasi | `budget`, `cost`, `allocation` |
| 4 | **Sales** | SaleContract, Booking, Lead, SchemeState, BAST | ✅ pendapatan, HPP | `sale` (8.631 LOC — **terlalu besar**), `scheme`, `salesorg` |
| 5 | **Billing & Receivable** | Invoice, TerminPayment, PaymentAllocation, Receipt, Aging, Statement | ✅ kas masuk, piutang | `billing` + separuh `sale` — **terbelah** |
| 6 | **Deposit (Titipan)** | ChargeGroup (item, payout, settlement) | ✅ kewajiban titipan | `charge` + `notary` — **duplikat** |
| 7 | **Ledger & Accounting** | JournalEntry, AccountingPeriod, Closing | ✅ otoritas posting | `ledger`, `closing` |
| 8 | **Document**★ | Document, DocumentSequence | ❌ (menautkan, bukan menerbitkan) | **tidak ada** |
| 9 | **Governance** | ApprovalRequest, TaxRule, TenantPolicy, AuditLog★ | ✅ pajak | `approval`, `tax` |
|  | *Reporting* (read model) | — | ❌ hanya baca ledger posted | `reporting` |

### 2.2 Pelanggaran konteks yang terbukti

Graf dependensi aktual (dari `go list`):

```
charge        → billing domain ledger platform reporting sale     ← 2 pelanggaran
sale          → allocation budget customer domain ledger platform project salesorg scheme tax
cancellation  → allocation approval budget closing domain ledger platform project sale tax
reporting     → allocation billing budget cost customer domain ledger notary platform project sale salesorg scheme tax
notary        → domain ledger platform
```

**V-1 · `charge` menulis agregat `sale`.** `charge/service.go` membuat sendiri `sale.TerminPayment` dan `sale.PaymentAllocation` (baris 325, 440-470, 806, 907-932, 975-1054). Konteks Titipan menulis ke jantung konteks Billing. Akibatnya: aturan alokasi pembayaran punya **dua penulis**, dan invariant "tidak ada jurnal tanpa allocation" (FE-2) hanya dijaga di satu sisi.

**V-2 · `charge` mengimpor `reporting`.** `charge/query.go:174-228` memanggil `reporting.BuildARAging` dari dalam konteks transaksional. Read model seharusnya bergantung pada konteks transaksional, tidak sebaliknya. Ini juga yang melahirkan aging kedua (§10 G-15).

**V-3 · `notary` menduplikasi `charge`.** Dua jalur kewajiban untuk satu kenyataan bisnis: modul `internal/notary` (akun `2-2300`, tabel `notary_deposits`, menu sendiri) **dan** charge item berlabel "Notaris" (akun `2-2400`). Konsistensi `EQ_NotaryDeposit` hanya mengunci `2-2300` — buta terhadap uang notaris yang masuk lewat `2-2400`. Setelah keputusan §2 (Notaris = jenis titipan biasa), **`internal/notary` kehilangan alasan eksistensinya.**

**V-4 · `sale` menampung ≥5 konteks.** 8.631 LOC berisi: kontrak, jadwal, termin, booking, statement, snapshot HPP, collection, alur skema, credit application, transfer deposit, akrual pajak. Bukan bug, tapi ini yang membuat setiap perubahan aturan terasa "refactor besar".

---

## §3 · Final Master Data Architecture

### 3.1 Prinsip final

> **Satu jenis, satu master, satu akun.**
> Kode Go tidak boleh memilih akun berdasarkan `switch` atas nama bisnis. Kode Go hanya boleh: (a) membaca akun dari master, atau (b) **menolak** (fail-closed) bila master belum menentukan.

### 3.2 Master target

| Master | Dikelola admin | Field kunci | Status |
|---|---|---|---|
| **Account (COA)** | ✅ ada | code, name, category (cash/bank/other) | ✅ ada, `category` sudah COA-driven |
| **ProductType** (produk jual) | ⚠️ parsial | code, name, **category** (property/non_property), revenue_account_code, is_active | Create ✅ Read ✅ Update **parsial** Delete ❌ (`project/handler.go` Mount) |
| **RealizationChargeType** (jenis titipan) ★ | ❌ **tidak ada** | code, name, deposit_account_code, default_amount, is_active | Hanya `charge_items.label` teks bebas + hardcode `ChargeGroupsPanel.tsx:532-534` |
| **DocumentType** ★ | ❌ **tidak ada** | code, name, direction (in/out), prefix, numbering_scope, is_system | Prefix hardcode di `billing/receipt_repository.go:247-288` |
| PaymentScheme | ✅ | — | ✅ |
| TaxRule | ✅ | tax_category, trigger_event, formula, akun | ✅ (Increment 4) |
| CommissionRule / ApprovalWorkflow / AllocationConfig | ✅ | — | ✅ |

### 3.3 Seam bersama: `domain.BillingTreatment`

ProductType dan RealizationChargeType adalah **dua master berbeda** (item yang dijual vs item yang dititipkan) tetapi berbagi satu pertanyaan: *ke mana uangnya masuk saat diterima?*

```
domain.BillingTreatment
  ├── revenue           → kredit akun 4-xxxx  (ProductType)
  └── deposit_liability → kredit akun 2-xxxx  (RealizationChargeType)
```

Sejajar dengan `domain.ProductCategory` yang sudah terbukti bekerja. Ini sekaligus **menutup V-3**: `deposit_account_code` per jenis titipan membuat Notaris bisa memakai `2-2300` dan BPHTB/PDAM/Listrik memakai `2-2400` **tanpa dua modul** — perbedaan akun menjadi data, bukan kode.

### 3.4 Business rule yang masih hardcode dan seharusnya master

| Rule hardcode | Lokasi | Rekomendasi |
|---|---|---|
| Prefix dokumen KWT/KWB/KWR/MTI/INV | `billing/receipt_repository.go:247-288`, `billing/repository.go:314` | → `document_types.prefix` |
| 3 baris default jenis titipan | `frontend/.../ChargeGroupsPanel.tsx:532-534` | → `RealizationChargeType` master |
| Seed produk termasuk `kavling`, `pdam` | `project/product_type.go` `defaultProductTypes` | → nonaktifkan lewat master, **jangan dihapus** (unit historis) |
| `CostCategory` → akun persediaan `1-3000/3100/3200/3300` | `domain/cost_category.go:91-97` | 🟡 **Boleh tetap hardcode** — ini taksonomi akuntansi tertutup (PSAK), bukan preferensi tenant. Tapi asimetris dengan produk yang COA-driven; catat sebagai keputusan sadar |
| Akun titipan `2-2400`, notaris `2-2300`, booking `4-2100` | `ledger/account_role.go:47-60` | 🟢 **Benar begini** — registry peran adalah otoritas tunggal sisi baca. Tapi lihat §6.4: sisi **tulis** belum ikut registry |

---

## §4 · Final Document Architecture

Ini deliverable dengan gap terbesar. Keputusan §6 klien tidak punya implementasi sama sekali di sisi kas keluar.

### 4.1 Keadaan hari ini

| Arah | Peristiwa | Dokumen bernomor? |
|---|---|---|
| **Kas masuk** | Booking fee | ✅ KWB |
| | DP / Termin / Pelunasan | ✅ KWT |
| | Titipan realisasi | ✅ KWR (+ 4 angka lengkap, `receipt_print.go:290-324`) |
| | Alih dana antar grup | ✅ MTI (memo transfer internal) |
| | Tagihan (bukan kas) | ✅ INV |
| | Penjualan produk (Kelebihan Tanah dst) | ⚠️ ikut KWT bila lewat termin |
| **Kas keluar** | Bayar Tanah | ❌ **tidak ada** |
| | Bayar Vendor | ❌ **tidak ada** |
| | Bayar Subkontraktor | ❌ **tidak ada** |
| | Bayar Notaris / pihak ketiga titipan | ❌ **tidak ada** (`charge_payouts` tanpa nomor) |
| | Refund Customer | ❌ **tidak ada** (`charge_settlements` tanpa nomor) |
| | Bayar Komisi | ❌ **tidak ada** |
| | Refund pembatalan | ❌ **tidak ada** (`refunds` tanpa nomor) |
| | Pembayaran pajak | ❌ **tidak ada** (`tax_payments` tanpa nomor) |

`cost_entries` (`internal/cost/model.go:44-62`) punya `JournalEntryID` tetapi **tidak punya field nomor dokumen sama sekali**. Delapan jalur kas keluar, nol dokumen.

### 4.2 Arsitektur target

**Dokumen menjadi bounded context tersendiri (`internal/document`)** — bukan field yang ditempel di delapan modul.

```
document_types  (master, per tenant)
  code            'BAYAR_TANAH' | 'KWT' | 'REFUND_CUSTOMER' | ...
  name            'Bukti Bayar Tanah'
  direction       'in' | 'out'
  prefix          'BT'
  numbering_scope 'yearly' | 'perpetual'
  is_system       true = tidak bisa dihapus admin

documents  (registry, append-only kecuali status)
  id, tenant_id
  doc_type_code   → document_types
  number          'BT/2026/000014'   (unik per tenant+type+periode)
  doc_date
  direction       'in' | 'out'       (disalin dari type, dibekukan)
  counterparty_type/id                customer | vendor | bank | internal
  amount          DECIMAL(20,4)
  cash_account_code                   akun kas/bank yang bergerak
  status          'posted' | 'void'
  journal_entry_id                    1:1 dengan jurnal
  created_by, created_at
```

Lalu **satu invariant baru** yang menegakkan keputusan §6 secara struktural, bukan lewat disiplin:

> **INV-DOC-1 — Tidak ada uang bergerak tanpa dokumen.**
> Setiap `JournalEntry` yang memiliki minimal satu `JournalLine` pada akun ber-`ledger.RoleCashBank` **wajib** memiliki `document_id`. Posting service menolak yang tidak punya.

Invariant ini bisa ditulis hari ini karena `RoleCashBank` sudah COA-driven (`ledger/account_role.go:76-80`) — keanggotaannya mengikuti `accounts.category`, jadi tenant yang menambah rekening bank baru otomatis ikut terjaga.

**Konsekuensi yang harus disadari:** invariant ini bersifat *fail-closed*. Begitu dinyalakan, setiap jalur kas keluar yang belum punya dokumen akan **berhenti bekerja**. Karena itu rollout-nya wajib bertahap (§12): bangun registry → backfill dokumen untuk jurnal kas historis → baru nyalakan penegakan.

### 4.3 Hubungan dengan Receipt & Invoice yang sudah ada

Kwitansi (`receipts`) dan Invoice (`invoices`) **tidak dibuang**. Keduanya menjadi *dokumen ber-jenis tertentu*:
- `Receipt` = presentasi cetak untuk `document` ber-direction `in`. Nomor pindah dari `receipt_sequences` ke `document_sequences`, format tetap `KWT/2026/000123` sehingga **nomor historis tidak berubah**.
- `Invoice` bukan perpindahan uang → tetap punya penomoran sendiri, tapi ikut master `document_types` agar prefix tidak hardcode.

---

## §5 · Final Cash Flow Architecture

### 5.1 Bentuk final

Semua uang lewat **satu pintu**, apa pun modulnya:

```
Modul bisnis                Document context              Ledger context
(sale/charge/cost/...)
      │
      │ menyusun niat:
      │  doc_type, tanggal, lawan transaksi,
      │  nominal, akun kas, baris jurnal
      ▼
  Document.Post() ─────────▶ 1. alokasi nomor (atomic, in-tx)
                             2. buat baris `documents`
                             3. panggil ledger.Post(entry)
                             4. tautkan journal_entry_id
                             ▼
                        satu transaksi DB. Gagal di mana pun → rollback penuh.
```

Modul bisnis berhenti menjadi *penerbit jurnal* dan menjadi *penyusun baris*. Itu satu-satunya cara INV-DOC-1 bisa dijamin tanpa menambal delapan tempat.

### 5.2 Katalog jenis dokumen final

**Kas masuk (direction `in`)**

| Jenis | Prefix usulan | Sumber | Ada? |
|---|---|---|---|
| Booking | KWB | `sale/booking.go` | ✅ |
| DP / Termin / Pelunasan | KWT | `sale` ReceivePayment | ✅ |
| Titipan Biaya Realisasi | KWR | `charge` | ✅ |
| Penjualan Produk (Kelebihan Tanah dll) | KWP | `sale` | ⚠️ ikut KWT |
| Pencairan KPR | KWD | `scheme` disbursement | ❌ |
| Alih dana internal | MTI | `charge` settlement | ✅ (memo, bukan kwitansi — keputusan T-1) |

**Kas keluar (direction `out`)**

| Jenis | Prefix usulan | Sumber | Ada? |
|---|---|---|---|
| Bayar Tanah | BT | `cost` (CostCategoryLand) | ❌ |
| Bayar Vendor | BV | `cost` (hard/soft) | ❌ |
| Bayar Subkontraktor | BS | `cost` (hard) | ❌ |
| Bayar Notaris / pihak ketiga titipan | BN | `charge_payouts` | ❌ |
| Refund Customer | RFC | `charge_settlements`, `refunds` | ❌ |
| Bayar Komisi | BK | `commission` | ❌ |
| Bayar Pajak | BP | `tax_payments` | ❌ |
| Pengeluaran lain | BU | `cost` (overhead) | ❌ |

> **KEPUTUSAN TERBUKA D-2 — jenis dokumen: sistem atau master?**
> Rekomendasi: jenis di atas di-seed sebagai `is_system = true` (tidak bisa dihapus, karena kode mengandalkan sebagian dari mereka untuk routing), sementara admin **boleh menambah jenis kas keluar baru sendiri** (`is_system = false`) untuk pengeluaran yang belum terpikir. Ini memenuhi §5 tanpa membuat kode bergantung pada data yang bisa dihapus admin.

---

## §6 · Final Journal Flow

### 6.1 Alur jurnal final per peristiwa

| # | Peristiwa | Debit | Kredit | Dokumen |
|---|---|---|---|---|
| J-1 | Terima booking fee | Kas/Bank | **4-2100 Pendapatan Booking** | KWB |
| J-2 | Booking batal/hangus | — | — (**tidak ada jurnal**, tetap pendapatan) | — |
| J-3 | Terima DP / termin pra-BAST | Kas/Bank | 2-2000 Uang Muka Penjualan | KWT |
| J-4 | BAST — akui pendapatan | 1-2000 Piutang Customer + 2-2000 Uang Muka | 4-1000 Pendapatan | — |
| J-5 | BAST — akui HPP | 5-1000 HPP | 1-3xxx Persediaan | — |
| J-6 | BAST — pajak | per TaxRule | 2-3000 / 2-4000 | — |
| J-7 | Terima pembayaran pasca-BAST | Kas/Bank | 1-2000 Piutang Customer | KWT |
| J-8 | Akad KPR | 1-2200 Piutang Bank | 2-2000 / 1-2000 | — |
| J-9 | Pencairan KPR | Kas/Bank | 1-2200 Piutang Bank | KWD ❌ |
| J-10 | **Kekurangan pencairan** | 1-2000 Piutang Customer | 1-2200 Piutang Bank | — ✅ (T-3) |
| J-11 | Terima titipan realisasi | Kas/Bank | 2-2400 / 2-2300 Titipan | KWR |
| J-12 | Bayar pihak ketiga titipan | 2-2400 / 2-2300 Titipan | Kas/Bank | BN ❌ |
| J-13 | Refund lebih bayar titipan | 2-2400 Titipan | Kas/Bank | RFC ❌ |
| J-14 | **Sisa titipan pasca-BAST → piutang** | **1-2000 Piutang Customer** | **2-2400 Titipan Realisasi** | — ❌ **belum ada** |
| J-15 | Belanja tanah/vendor/subkon | 1-3xxx Persediaan | Kas/Bank atau 2-1000 | BT/BV/BS ❌ |
| J-16 | Overhead | 5-3000/5-4000 | Kas/Bank | BU ❌ |
| J-17 | Komisi | 5-2xxx Beban Komisi | 2-6200 Utang Komisi | — |
| J-18 | Bayar komisi | 2-6200 | Kas/Bank | BK ❌ |
| J-19 | Pembatalan | jurnal pembalik penuh | | RFC ❌ |

### 6.2 J-14 — jurnal yang paling perlu diputuskan

Keputusan §2 berbunyi: *"Jika masih ada sisa setelah BAST maka menjadi piutang customer."* Hari ini sisa tagihan titipan hidup **hanya sebagai catatan billing** (`charge_items` dikurangi pembayaran); ledger tidak tahu apa-apa tentangnya sampai uangnya masuk.

Ada dua cara memenuhi keputusan itu:

- **J-14a (rekomendasi): akui di ledger saat BAST.** `Dr 1-2000 Piutang Customer / Cr 2-2400 Titipan Realisasi` sebesar sisa tagihan. Neraca menampilkan piutang **dan** kewajiban ke pihak ketiga secara jujur; saat customer bayar → `Dr Kas / Cr 1-2000`; saat kita bayar notaris → `Dr 2-2400 / Cr Kas`. Konsekuensi: kewajiban ke pihak ketiga diakui pada saat BAST, bukan saat tagihan pihak ketiga datang.
- **J-14b: tetap di luar ledger,** hanya dimunculkan di aging & statement sebagai "piutang titipan". Lebih ringan, tapi kata "piutang customer" jadi tidak berarti di neraca, dan total piutang di dashboard ≠ total piutang di laporan.

> **KEPUTUSAN TERBUKA D-3 — pilih J-14a atau J-14b.** Saya merekomendasikan **J-14a** karena konsisten dengan invariant #7 (penerimaan pra-pengakuan = kewajiban) dan dengan keputusan T-3 yang sudah Bapak/Ibu ambil untuk kekurangan KPR. Namun ini pengakuan kewajiban — layak dikonfirmasi ke akuntan klien.
>
> Terkait: **akun pendapatan Kelebihan Tanah.** Saat ini `non_property` → `4-2000` (Pendapatan Lain-lain). Sebagai produk penjualan dengan HPP, tempat yang benar adalah akun pendapatan tersendiri (usulan: `4-1100 Pendapatan Penjualan Tanah`) supaya margin rumah dan margin tanah tidak tercampur. `// TODO(tax-advisor)` yang sudah ada di `project/product_type.go` soal perlakuan PPN kelebihan tanah **belum terjawab** dan masih relevan.

### 6.3 Permukaan usang vs keputusan final

| Permukaan | Isi usang | Lokasi |
|---|---|---|
| Deskripsi COA `2-2100` Titipan Booking | masih berbunyi *"Konversi → reklas ke 2-2000; hangus → 4-2000"* — bertentangan dengan keputusan §3 | `ledger/coa.go` |
| DTO booking menerima `refundable` | diterima dari klien lalu dipaksa `false` (`booking_service.go:118-120`) | `sale/booking_handler.go:20` |
| Kebijakan tenant `require_realization_settled` | keputusan §2 menghapus syarat ini | `migrations/000059`, `charge/query.go:437-470` |
| Skema `hpp_trueup_runs` / `hpp_trueup_lines` | tabel ada, kode tidak ada (P0-4 belum dikerjakan) | migrasi 000029-000032 |

### 6.4 Asimetri registry akun

`ledger/account_role.go:12-14` menyatakan sendiri: registry adalah otoritas **sisi pembacaan saldo**, sedangkan **sisi posting** masih memakai taxonomy tersebar. Artinya: satu akun bisa dianggap "piutang" oleh dashboard tetapi dipilih untuk posting oleh `switch` di modul lain. Sejauh audit ini, keduanya konsisten hari ini — tetapi tidak ada yang menjamin konsistensinya besok. Rekomendasi: jadikan registry otoritas dua sisi, dan uji konsistensi (`EQ_*`) atas kesetaraan itu.

---

## §7 · Final Numbering Architecture

### 7.1 Keadaan hari ini — tiga masalah nyata

**N-1 · Dua mesin penomoran paralel.**
- `receipt_sequences` — key `(tenant_id, doc_type)` — dipakai kwitansi & memo (`billing/receipt_repository.go:247-288`)
- `invoice_sequences` — key `(tenant_id)` saja, **tanpa doc_type** — dipakai invoice (`billing/repository.go:314`)

**N-2 · Format memuat tahun, sekuens tidak pernah reset.**
```go
return fmt.Sprintf("%s/%d/%06d", prefix, time.Now().Year(), seq)
```
`next_val` bersifat abadi. Nomor terakhir 2026 misalnya `KWT/2026/001850`, maka 1 Januari 2027 nomor pertama adalah `KWT/2027/001851` — bukan `000001`. Secara teknis tetap unik, tetapi **tidak sesuai harapan "nomor urut per tahun"** dan akan ditanyakan saat audit.

**N-3 · Prefix hardcode di Go.** `switch rtype { case ReceiptTypeBooking: prefix = "KWB" ... }`. Admin tidak bisa menambah jenis dokumen tanpa programmer — bertentangan langsung dengan keputusan §5+§6.

### 7.2 Arsitektur target

```
document_sequences
  PRIMARY KEY (tenant_id, doc_type, period_key)
  period_key : '2026'  bila numbering_scope = 'yearly'
               ''      bila numbering_scope = 'perpetual'
  next_val   : BIGINT UNSIGNED

Alokasi: INSERT ... ON DUPLICATE KEY UPDATE next_val = next_val + 1
         di dalam transaksi posting yang sama (pola yang sudah dipakai — benar).
Format : dari document_types.prefix → '{prefix}/{YYYY}/{NNNNNN}'
```

Aturan final:
1. Nomor dialokasikan **di dalam** transaksi posting → tidak ada nomor tanpa jurnal, tidak ada jurnal tanpa nomor.
2. Dokumen **void tetap memegang nomornya**. Nomor tidak pernah dipakai ulang — itu jejak audit, bukan slot kosong.
3. Migrasi `receipt_sequences` + `invoice_sequences` → `document_sequences` **membawa serta `next_val` yang berjalan**, sehingga nomor historis tidak pernah bentrok.

> **KEPUTUSAN TERBUKA D-4 — reset tahunan atau berlanjut?**
> Rekomendasi: `yearly` untuk semua dokumen kas (kebiasaan pembukuan Indonesia, dan format sudah memuat tahun). Tapi ini mengubah pola nomor yang klien sudah lihat selama UAT — perlu konfirmasi eksplisit sebelum diterapkan.

---

## §8 · Final State Machine

### 8.1 Unit (`project/model.go:131-139`) — 9 state ✅ final

```
available ──▶ booked ──▶ reserved ──▶ ppjb ──▶ sold(=BAST) ──▶ occupied ──▶ maintenance
     │            │           │          │                                        │
     └────────────┴───────────┴──────────┴──▶ hold / blocked (ber-gate approval) ──┘
```
Semua transisi tercatat di `unit_status_transitions`. Tidak ada perubahan yang dituntut keputusan final. ✅

### 8.2 Booking (`sale/booking.go:38-58`) ✅ final

```
active ──▶ converted | expired | cancelled     (semua terminal, satu langkah)
```
Fee **tidak pernah** direklas di keempat cabang. Sesuai keputusan §3. ✅

### 8.3 Skema pembayaran (`scheme/types.go:53-64`)

```
KPR   : signed → submitted_to_bank → bank_approved → akad → disbursed → handed_over → fully_paid
                                   ↘ bank_rejected
Tunai : signed → dp_paid → fully_paid → handed_over
Cicil : signed → dp_paid → installment_running → fully_paid → handed_over
```
Akun piutang mengikuti state lewat `ResolveReceivableAccount`; `1-2200` hanya hidup di jendela `akad → disbursed`. Sesuai keputusan §4. ✅

### 8.4 ChargeGroup (`charge/model.go:46-55`)

```
open ──▶ settled | cancelled
```
⚠️ **Perlu ditinjau ulang.** Keputusan §2 memperkenalkan keadaan baru: *grup masih punya sisa, tetapi BAST sudah lewat dan sisanya sudah menjadi piutang customer.* State itu bukan `open` (tagihan sudah dikonversi) dan bukan `settled` (uangnya belum masuk). Usulan: state ke-4 `receivable` — atau, bila J-14b yang dipilih, cukup flag `converted_at`.

### 8.5 Document ★ (baru)

```
posted ──▶ void      (void wajib jurnal pembalik + alasan; nomor dipertahankan)
```

### 8.6 Yang tidak berubah

BudgetPlan (`draft → active → superseded`) dan ApprovalRequest (engine quorum/threshold dengan snapshot beku) sudah final dan tidak tersentuh keputusan ini. ✅

---

## §9 · Final Audit Trail

### 9.1 Sudah kuat ✅

| Objek | Mekanisme |
|---|---|
| Jurnal | append-only; koreksi hanya via `is_reversing` + `reverses_id` |
| Status unit | `unit_status_transitions` (siapa, kapan, dari→ke) |
| Kebijakan tenant | `tenant_policy_changes` (migrasi 000060) |
| Approval | `approval_actions` + snapshot beku (Increment 5.1) |
| Tagihan titipan | `charge_item_adjustments` (alasan bertipe) |
| Disposisi titipan | `charge_settlements` append-only |
| HPP | `allocation_snapshots` + `allocation_snapshot_lines` (dibekukan saat BAST) |
| RAB | versi lama → `superseded`, tidak pernah dihapus |
| Sumber jurnal | `journal_entries.source` + `created_by` (migrasi 000017) |

### 9.2 Gap ❌

**A-1 · Perubahan master data tidak diaudit.** Keputusan §5 memberi admin kuasa mengubah Master Produk dan Master Jenis Biaya Realisasi sendiri. Kalau admin mengubah `revenue_account_code` sebuah produk, jurnal lama tetap benar (append-only) — **tetapi tidak ada catatan siapa mengubah apa, kapan, dari nilai berapa.** Dua transaksi identik bulan Maret dan bulan April bisa masuk akun berbeda tanpa jejak penjelasan. Begitu master menjadi self-service, **audit master menjadi wajib**, bukan opsional. Usulkan `master_data_changes` generik (entity, entity_id, field, old, new, actor, at).

**A-2 · Jurnal tidak punya tautan dokumen terstruktur.** `journal_entries` hanya punya `description` dan `reference VARCHAR(100)` teks bebas (migrasi 000017). Tidak mungkin menjawab "tunjukkan semua jurnal untuk dokumen BT/2026/000014" lewat query yang bisa diandalkan. Diselesaikan oleh `documents.journal_entry_id` (§4.2).

**A-3 · Kas keluar tidak punya jejak dokumen** — konsekuensi langsung §4.1. Delapan jalur pengeluaran menghasilkan jurnal tanpa bukti bernomor.

**A-4 · Tidak ada audit siapa mencetak/mencetak ulang kwitansi.** Untuk dokumen bernomor yang diserahkan ke customer, reprint biasanya perlu dicatat. Prioritas rendah, tapi catat sebagai keputusan sadar.

---

## §10 · Daftar overlap, duplicate responsibility, dan bounded context yang salah

Diurutkan dari yang paling mahal bila dibiarkan.

| ID | Temuan | Bukti | Dampak |
|---|---|---|---|
| **G-1** | Kelebihan Tanah `non_property` → HPP dilewati total | `sale/service.go:440-475`, `project/product_type.go` | 🔴 Laba kotor overstated 100% pada produk itu |
| **G-2** | Tidak ada mekanisme "mengurangi stok tanah" | `project/model.go:60` (`LandArea` statis, tak pernah dikurangi) | 🔴 Keputusan §1 tak bisa dipenuhi |
| **G-3** | PDAM dimodelkan sebagai produk jual → akui pendapatan `4-2000` saat BAST | `project/product_type.go` `defaultProductTypes` | 🔴 Titipan diakui sebagai pendapatan |
| **G-4** | Listrik & BPHTB tidak punya master di mana pun | — | 🔴 Hanya hidup sebagai teks bebas |
| **G-5** | Tidak ada master Jenis Biaya Realisasi | `charge_items.label` teks bebas (migrasi 000059); default hardcode `ChargeGroupsPanel.tsx:532-534` | 🔴 Keputusan §5 tak terpenuhi; akun tak bisa diatur per jenis |
| **G-6** | Kas keluar tanpa dokumen bernomor (8 jalur) | `cost/model.go:44-62`, `charge_payouts`, `refunds`, `tax_payments` | 🔴 Keputusan §6 tak terpenuhi |
| **G-7** | Tidak ada entitas Dokumen; jurnal tak punya `document_id` | migrasi 000017 | 🔴 Keputusan §6 tak bisa ditegakkan struktural |
| **G-8** | Sisa titipan pasca-BAST → piutang customer: tak ada jurnal, trigger, aging, statement | — | 🔴 Keputusan §2 tak terpenuhi |
| **G-9** | Gate BAST atas biaya realisasi masih hidup | `charge/query.go:437-470`, tenant `require_realization_settled` | 🔴 Bertentangan dengan keputusan §2 |
| **G-10** | Dua mesin penomoran; sekuens tak reset tahunan; prefix hardcode | `billing/receipt_repository.go:247-288`, `billing/repository.go:314` | 🟠 Nomor tak sesuai harapan + admin butuh dev |
| **G-11** | Notaris punya dua jalur kewajiban dengan dua akun berbeda | `internal/notary` (2-2300) vs charge item (2-2400); `EQ_NotaryDeposit` hanya kunci 2-2300 | 🟠 Satu kenyataan bisnis, dua kebenaran; konsistensi buta sebelah |
| **G-12** | `charge` menulis langsung agregat `sale` | `charge/service.go:325,440-470,806,907-932,975-1054` | 🟠 Dua penulis untuk satu aturan alokasi |
| **G-13** | `charge` (transaksional) mengimpor `reporting` (read model) | `charge/query.go:174-228` | 🟠 Arah dependensi terbalik |
| **G-14** | Dua laporan aging terpisah, tak pernah digabung; statement buta titipan | `reporting/ar_aging.go:56` + `repository.go:211-223` vs `charge/query.go:174-228`; `sale/statement.go` | 🟠 Eksposur total customer tak terlihat di layar mana pun |
| **G-15** | `kind = addon` menghasilkan jurnal identik dengan `realization`, hanya deskripsi beda | `charge/service.go:396` | 🟠 Perlakuan akuntansi bergantung pada menu yang dipilih admin, bukan aturan. Setelah §1, `addon` kehilangan alasan hidup |
| **G-16** | Perubahan master data tidak diaudit | tidak ada tabel audit master | 🟠 Wajib begitu master jadi self-service (§5) |
| **G-17** | `sale` 8.631 LOC menampung ≥5 bounded context | `go list` deps | 🟡 Setiap perubahan aturan terasa refactor besar |
| **G-18** | Permukaan usang: deskripsi COA `2-2100`, DTO `refundable` | `ledger/coa.go`, `sale/booking_handler.go:20` | 🟡 Membingungkan, bukan salah hitung |
| **G-19** | `kavling` masih di seed produk | `project/product_type.go` | 🟡 Nonaktifkan, **jangan hapus** (resolver fail-closed) |
| **G-20** | Skema mati `hpp_trueup_runs`/`hpp_trueup_lines` tanpa kode (P0-4) | migrasi 000029-000032 | 🟡 Utang teknis terbuka |
| **G-21** | Master Produk: DELETE/nonaktif tidak lengkap | `project/handler.go` Mount — tak ada rute DELETE | 🟡 Admin tak bisa menyingkirkan kavling/pdam sendiri |
| **G-22** | Registry peran akun otoritatif hanya di sisi baca, tidak di sisi posting | `ledger/account_role.go:12-14` (dinyatakan sendiri) | 🟡 Konsisten hari ini, tak dijamin besok |

⚠️ **Peringatan operasional untuk G-3 dan G-19.** Resolver produk bersifat **fail-closed**. Menghapus product type yang masih dirujuk unit membuat BAST unit tersebut **tertolak selamanya**, dan pendapatan yang terlanjur diakui hanya boleh dikoreksi lewat jurnal pembalik (invariant #5). Karena itu: audit data dulu (adakah unit ber-tipe `pdam`/`kavling` di produksi), **nonaktifkan** (`is_active = false`), jangan hapus.

---

## §11 · Jawaban atas 5 pemeriksaan khusus

**11.1 · Apakah Product Catalog masih membawa domain yang seharusnya di Billing?**
**Ya — satu item.** `pdam` ada di `product_types` sebagai produk `non_property` yang mengakui pendapatan `4-2000` saat BAST. PDAM adalah titipan; ia tidak boleh punya siklus penjualan. Selain `pdam`, isi Product Catalog sudah benar: `revenue_account_code` memang milik katalog produk, bukan pinjaman dari Billing. Setelah `pdam` dinonaktifkan dan Kelebihan Tanah dipindah ke `property`, Product Catalog bersih.

**11.2 · Apakah Billing masih menyimpan domain Product?**
**Ya — tiga cara.**
1. `charge_items.label` adalah teks bebas: sebuah master jenis-item yang hidup di dalam tabel transaksi. Itu domain katalog yang bocor ke Billing.
2. `kind = addon` menjual produk lewat modul titipan — Kelebihan Tanah pernah dijual dengan kredit `2-2400` (kewajiban), padahal keputusan §1 menetapkannya sebagai penjualan dengan pendapatan dan HPP. Karena `charge/service.go:396` hanya mengubah deskripsi, dua perlakuan akuntansi yang berbeda memakai jurnal yang sama.
3. `charge` membuat `sale.TerminPayment` / `sale.PaymentAllocation` sendiri.

**11.3 · Apakah Document sudah cukup fleksibel untuk semua uang masuk dan keluar?**
**Tidak. Ini gap terbesar.** Yang ada hari ini adalah *penomoran kwitansi*, bukan *arsitektur dokumen*: 5 prefix hardcode di Go, dua tabel sekuens yang berbeda bentuk kuncinya, dan **nol** dokumen untuk delapan jalur kas keluar. Tidak ada entitas Dokumen, tidak ada tautan terstruktur ke jurnal, tidak ada jenis dokumen yang bisa ditambah admin. Rancangan target ada di §4 dan §7.

**11.4 · Apakah masih ada entity yang hidup di dua bounded context?**
**Ya — lima.**
| Entity | Konteks A | Konteks B |
|---|---|---|
| Titipan Notaris | `internal/notary` (2-2300) | `charge` item "Notaris" (2-2400) |
| TerminPayment / PaymentAllocation | `sale` (pemilik) | `charge` (penulis kedua) |
| Kelebihan Tanah | `project` unit → pendapatan | `charge` addon → kewajiban |
| AR Aging | `reporting` (payment_schedules) | `charge` (charge_items) |
| Penomoran dokumen | `billing` (receipts, invoices) | `charge` (memo MTI) |

**11.5 · Apakah masih ada hardcode business rule yang seharusnya master data?**
**Ya — empat yang harus dipindah, satu yang sebaiknya tetap.**
Harus dipindah: (a) prefix dokumen; (b) jenis biaya realisasi + akunnya; (c) daftar jenis dokumen kas keluar; (d) seed produk yang sudah tidak relevan (`kavling`, `pdam`) — harus bisa dinonaktifkan admin lewat UI.
Sebaiknya tetap kode: pemetaan `CostCategory → 1-3xxx`. Itu taksonomi akuntansi tertutup, bukan preferensi tenant. Catat sebagai keputusan sadar agar tidak dipertanyakan lagi nanti.
Frontend secara umum bersih dari kode akun — satu-satunya sisa adalah default `"4-1000"` di `ProductTypesSection.tsx`, yang wajar sebagai nilai awal formulir.

---

## §12 · Rekomendasi urutan kerja

Diurutkan berdasarkan ketergantungan, bukan besarnya usaha. Setiap gelombang berdiri sendiri dan bisa dihentikan.

| Wave | Isi | Menutup | Prasyarat |
|---|---|---|---|
| **W-0** | **Jawab 4 keputusan terbuka** (D-1 HPP kelebihan tanah, D-2 jenis dokumen, D-3 jurnal J-14, D-4 reset nomor) + definisi `saleable_area` yang tertunda | — | **Bapak/Ibu + klien** |
| **W-1** | Master Jenis Biaya Realisasi + `domain.BillingTreatment`; nonaktifkan `pdam`/`kavling` (audit data dulu); satukan `notary` ke dalamnya lewat `deposit_account_code` | G-3, G-4, G-5, G-11, G-19, G-21 | W-0 |
| **W-2** | Bounded context Dokumen: `document_types`, `documents`, `document_sequences`; migrasi 2 tabel sekuens lama tanpa mengubah nomor historis; backfill dokumen untuk jurnal kas historis | G-6, G-7, G-10, A-2, A-3 | W-0 (D-2, D-4) |
| **W-3** | Nyalakan **INV-DOC-1** (fail-closed) + dokumen untuk 8 jalur kas keluar | penegakan §6 | W-2 backfill selesai |
| **W-4** | Cabut gate BAST realisasi; implementasi J-14 (sisa titipan → piutang); satukan aging & statement jadi satu eksposur customer | G-8, G-9, G-14 | W-0 (D-3) |
| **W-5** | Kelebihan Tanah → `property`, akun pendapatan sendiri, ikut HPP; stok tanah | G-1, G-2, G-15 | W-0 (D-1) |
| **W-6** | Audit master data (`master_data_changes`) | G-16, A-1 | W-1 |
| **W-7** | Higienis: `charge` berhenti menulis agregat `sale`; putus impor `charge → reporting`; bersihkan permukaan usang | G-12, G-13, G-18 | — |
| **W-8** | Pecah `sale` per konteks; registry akun dua sisi; putuskan nasib skema mati P0-4 | G-17, G-20, G-22 | — |

Waves Tab Unit yang tertunda dari audit sebelumnya (PATCH unit, paginasi, aksi kartu) tidak hilang — tapi keputusannya sekarang jelas: **jangan dikerjakan sebelum W-1**, karena W-1 mengubah bentuk master yang jadi dasar layarnya.

---

## §13 · Yang saya butuhkan dari Bapak/Ibu

Empat keputusan yang memblokir. Sisanya bisa saya kerjakan sendiri begitu ini terjawab.

| ID | Pertanyaan | Rekomendasi saya |
|---|---|---|
| **D-1** | Kelebihan Tanah menyerap HPP lewat alokasi luas seperti unit biasa (D-1a), atau lewat buku stok tanah terpisah (D-1b)? Dan berapa `saleable_area`-nya? | **D-1a** — tanpa mesin baru. Tapi butuh definisi `saleable_area` yang Bapak/Ibu tangguhkan |
| **D-2** | Jenis dokumen: seluruhnya sistem, atau admin boleh menambah jenis kas keluar sendiri? | Sistem untuk 14 jenis di §5.2, **admin boleh menambah** jenis kas keluar baru |
| **D-3** | Sisa titipan pasca-BAST: jurnal `Dr 1-2000 / Cr 2-2400` (J-14a) atau catatan billing saja (J-14b)? | **J-14a** — tapi konfirmasikan ke akuntan klien, ini pengakuan kewajiban |
| **D-4** | Nomor dokumen reset tiap tahun, atau berlanjut? | **Reset tahunan** — tapi ini mengubah pola nomor yang klien lihat selama UAT |

Ditambah satu yang bukan keputusan saya: **`// TODO(tax-advisor)` soal perlakuan PPN Kelebihan Tanah** (`project/product_type.go`) masih terbuka sejak Product Catalog Hardening. Kalau Kelebihan Tanah kini produk penjualan penuh, jawabannya jadi mendesak.

---

**Catatan penutup.** Tidak ada baris kode, migrasi, atau file sumber yang diubah untuk menghasilkan dokumen ini — sesuai instruksi. Semua temuan berasal dari pembacaan. Saya tidak menjalankan test dan tidak memverifikasi perilaku runtime; klaim di sini adalah klaim tentang **struktur kode dan skema**, bukan tentang hasil eksekusi.
