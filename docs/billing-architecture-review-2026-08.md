# Business Architecture Review — Billing Batch 2 (Charge Group)

**Tanggal:** 2026-08-03
**Sifat:** READ-ONLY. Tidak ada satu baris kode pun yang diubah untuk laporan ini.
**Pemicu:** business rule baru dari UAT klien — pembayaran harga rumah, biaya realisasi, dan produk tambahan adalah **billing yang terpisah**, masing-masing dengan invoice, kwitansi, dan outstanding sendiri.
**Metode:** pembacaan kode produksi (bukan asumsi) atas domain Invoice, Receipt, Collection, Statement, Outstanding, Dashboard, Aging, Collection Rate, plus schema/index DB nyata.

---

## 0. Ringkasan eksekutif

| Pertanyaan | Jawaban |
|---|---|
| Apakah arsitektur sekarang sudah mendukung 3 rule baru? | **Tidak.** Biaya Realisasi sebagai *tagihan* tidak ada sama sekali di sistem — nol baris kode. |
| Apakah model terlalu berpusat pada Sale Contract? | **Ya untuk billing, tidak untuk uang.** Detail di §2. |
| Perlu konsep Charge Group / Billing Group? | **Ya.** Ini satu-satunya cara memenuhi rule klien tanpa menduplikasi definisi outstanding. |
| Apakah ini refactor besar / bongkar ulang? | **Bukan.** ±70% mesinnya sudah ada dan benar. Yang hilang adalah **satu entitas di tengah**. |
| Risiko technical debt terbesar bila dipaksakan tanpa Charge Group | Muncul **definisi outstanding kedua** dan **mesin aging kedua** — pelanggaran langsung hasil kerja SSOT Phase 2. |

**Rekomendasi:** perkenalkan `charge_group` sebagai **agregat baru yang berdiri di samping Sale Contract, bukan menggantikannya**, dengan aturan keras: harga rumah tetap memakai rumus kanonik yang ada sekarang; charge group hanya menjadi *pembungkus* yang mendelegasikan, bukan menghitung ulang.

Ada **3 keputusan bisnis** yang harus dijawab owner/klien sebelum baris kode pertama ditulis (§8). Salah satunya menyangkut perlakuan akuntansi dan berkonsekuensi pajak — saya tidak menebaknya.

---

## 1. Apa yang benar-benar ada hari ini (bukti kode)

### 1.1 Rantai billing sekarang

```
SaleContract (1 per unit, by convention)
   └── PaymentSchedule (DP / installment / final)
         └── Invoice            invoices.sale_contract_id NOT NULL
         └── TerminPayment      (uang masuk, unit-anchored)
               ├── PaymentAllocation  (sub-ledger termin → schedule)
               └── Receipt            1 : 1 dengan termin
```

### 1.2 Satu-satunya definisi outstanding

`internal/sale/financial_summary.go:147`

```go
paid, err := s.store.SumTerminsByUnit(ctx, tenantID, c.UnitID)
...
Outstanding: c.GrossAmount.Sub(paid)
```

dengan primitif kanonik `internal/sale/repository.go:139`

```go
// SumTerminsByUnit adalah PRIMITIF KANONIK "total dibayar terhadap harga" (registry #8)
Where("tenant_id = ? AND unit_id = ? AND counts_toward_price = TRUE", tenantID, unitID)
```

13 pemanggil, semuanya lewat primitif yang sama. **Ini sehat** dan hasil kerja SSOT Phase 2 — jangan dirusak.

### 1.3 Seam yang sudah terbukti: uang yang BUKAN harga rumah

`internal/sale/model.go`:

```go
// CountsTowardPrice (R4): SATU-SATUNYA penanda "pembayaran ini mengurangi harga rumah".
CountsTowardPrice bool
```

Sudah dipakai produksi untuk booking fee (`booking_repo.go:95` → `false`). Dihormati oleh `SumTerminsByUnit`, oleh daftar pembayaran di statement (`statement.go`: `if !tp.CountsTowardPrice { continue }`), dan secara transitif oleh gate BAST (`scheme/policy.go:114` membaca `TotalReceived` dari primitif yang sama).

**Artinya:** sistem sudah punya konsep "kas masuk yang tidak mengurangi harga rumah", sudah teruji, sudah bersih. Biaya realisasi adalah kasus ke-2 dari konsep yang sama.

### 1.4 Preseden terdekat biaya realisasi: Titipan Notaris

`internal/notary/notary.go` — `NotaryDeposit` sebagai kewajiban 2-2300:

```
terima  : Dr Kas          / Cr 2-2300 Titipan Notaris
bayar   : Dr 2-2300       / Cr Kas
```

Yang **ada**: perlakuan akuntansi pass-through yang benar.
Yang **tidak ada**: invoice, dokumen kwitansi, total tagihan, sisa terutang, pembayaran sebagian. Notaris hari ini adalah *penampung uang*, bukan *tagihan*.

### 1.5 Aging & Collection Rate

`internal/reporting/ar_aging.go` — `BuildARAging(rows []ARScheduleRow, asOf)` adalah **fungsi murni** yang 100% disuapi dari `payment_schedules`. `collectionRate = collected / scheduled × 100` (decimal, bukan float). Dashboard `reporting/dashboard.go:517` memakai `PortfolioOutstanding` lalu menurunkan forecast + top-5 debitur dari `BuildARAging` yang sama.

**Konsekuensi keras:** tagihan biaya realisasi tidak akan pernah terlihat di aging/collection rate kecuali ia menghasilkan baris ber-jatuh-tempo yang disuapkan ke **mesin `BuildARAging` yang sama**. Membuat mesin aging kedua = duplicate source of truth.

### 1.6 Hasil pencarian: biaya realisasi TIDAK ADA

`grep -rn "bphtb|pdam|realisasi|listrik"` di backend + frontend → nol hit yang berhubungan dengan tagihan. Yang muncul hanya nama seed katalog produk (`pdam`, `kelebihan_tanah` sebagai `non_property → 4-2000`) dan kata "RAB vs Realisasi" di konteks anggaran biaya proyek (tidak berhubungan).

**Ini bukan fitur yang kurang matang. Ini domain yang belum ada.**

---

## 2. Fokus #1 — Apakah model terlalu berpusat pada Sale Contract?

Jawaban jujur: **campuran**, dan penting membedakannya.

| Lapisan | Anchor sebenarnya | Terlalu contract-centric? |
|---|---|---|
| Invoice | `sale_contract_id NOT NULL` (migration 000019) | **Ya — ini penghalang keras.** Tidak mungkin menerbitkan invoice untuk apa pun yang bukan kontrak jual unit. |
| Receipt | `termin_payment_id NOT NULL` UNIQUE, + `unit_id` | **Ya.** 1 kwitansi = 1 termin. Tidak ada tempat untuk "kwitansi kelompok tagihan". |
| Outstanding | `GrossAmount − Σ termin(price)` per **unit** | Ya, tapi **benar** untuk harga rumah. Jangan diubah. |
| Uang masuk (TerminPayment) | `unit_id` + flag `counts_toward_price` | **Tidak.** Ini justru sudah cukup general. |
| Aging / Collection Rate | `payment_schedules` | Ya — hanya kenal jadwal kontrak. |
| Statement | per kontrak, difilter `CountsTowardPrice` | Ya secara sengaja ("statement kontrak = murni pembayaran RUMAH"). |

Dan ada satu bahaya yang harus dicatat, karena ia menutup jalan pintas yang paling menggoda:

`internal/sale/repository.go:428`

```go
func (r *GORMRepository) FindContractByUnitID(...) (*SaleContract, error) {
    var c SaleContract
    err := r.db.WithContext(ctx).
        Where("unit_id = ? AND tenant_id = ?", unitID, tenantID).
        First(&c).Error   // ← tanpa ORDER BY
```

`sale_contracts` **tidak punya unique key pada `unit_id`** (dicek di DB: hanya `idx_sale_contracts_unit`, non-unique). "Satu kontrak per unit" adalah konvensi yang ditegakkan oleh `.First()`, bukan oleh constraint.

> **Karena itu: memodelkan biaya realisasi sebagai "SaleContract kedua di unit yang sama" adalah cara tercepat merusak seluruh sistem, secara diam-diam.** Setiap pembaca unit-anchored akan memilih kontrak yang mana saja yang muncul duluan. Opsi ini harus dicoret dari meja.

---

## 3. Fokus #2 — Apakah Charge Group konsep yang tepat?

**Ya**, dengan satu syarat yang menentukan sukses/gagalnya.

Charge Group harus menjadi **agregat penagihan** — "sekelompok tagihan yang punya total, punya invoice, punya kwitansi, punya outstanding sendiri" — dan **bukan** menjadi tempat perhitungan kedua untuk harga rumah.

Aturan pengikatnya:

> **CG-INV-1.** Outstanding sebuah charge group dihitung oleh **tepat satu** formula per `kind`. Untuk `kind = price`, formula itu **mendelegasikan ke `ContractFinancialSummary` yang ada sekarang** — charge group tidak menyimpan angkanya, tidak menghitung ulang, tidak meng-cache-nya sebagai kebenaran.

Ini persis pola yang sudah dipakai tim di `domain.ProductCategory.ParticipatesInHPP()`: satu predikat di Go, turunan lain dijaga unit test anti-duplicate-SoT. Pola yang sama dipakai di sini.

Bonus arsitektural: unique key `(tenant_id, sale_contract_id, kind='price')` sekaligus **menutup lubang §2** — mulai saat itu "satu billing harga per kontrak" ditegakkan database, bukan `.First()`.

### Kenapa bukan alternatif lain

| Alternatif | Kenapa ditolak |
|---|---|
| Kontrak kedua per unit | Merusak `FindContractByUnitID` dan 13 pemanggil unit-anchored. Fatal & senyap. |
| Tambah `InvoiceType` baru (PDAM, BPHTB, …) di invoice yang ada | Invoice tetap menempel ke schedule kontrak; tidak ada wadah "total kelompok" → kwitansi klien mustahil; outstanding realisasi jadi definisi liar. |
| Tabel `realization_costs` berdiri sendiri | Menyelesaikan rule #2 saja. Rule #3 (produk tambahan) minta tabel ketiga. Kwitansi, invoice, aging, idempotensi diduplikasi 3×. Ini definisi technical debt. |
| Perluas makna `counts_toward_price` jadi enum | Menyembunyikan kelompok tagihan di dalam baris kas. Tidak ada tempat menyimpan *total tagihan* → kwitansi klien tetap mustahil. |

---

## 4. Fokus #3 — Audit per domain

| Domain | Status hari ini | Dampak Charge Group | Ada duplicate SoT? |
|---|---|---|---|
| **Invoice** | `sale_contract_id NOT NULL`; 4 tipe (DP/TERMIN/PELUNASAN/KEKURANGAN); nomor unik per tenant | `sale_contract_id` → **nullable**, tambah `charge_group_id NOT NULL`. Seri nomor per tipe sudah ada, tinggal ditambah seri baru. Perubahan **aditif**. | Tidak, selama nomor invoice tetap satu generator. |
| **Receipt** | 1:1 dengan termin; tipe `house_payment` (KWT) & `booking` (KWB); `ReceiptSequence` per DocType | Tambah tipe `realization` (KWR) + `addon`. **Penting:** `ReceiptPrintData` **sudah** punya `HasSummary, TotalPaid, Outstanding` — bentuk kwitansi yang diminta klien sudah ada, yang berubah hanya **sumber** ringkasan: dari `ContractSummaryProvider` → `ChargeGroupSummaryProvider`. | Tidak, jika provider di-*interface*-kan, bukan di-copy. |
| **Collection** | `ReceivePayment` satu pintu masuk: idempotency → resolve anchor → prepare → commit atomik (jurnal + termin + alokasi + cache + kwitansi + sync invoice) | Anchor resolver diperluas: hari ini "unit/kontrak", nanti "charge group". Inti transaksi **tidak berubah**. Ini aset terbesar yang kita punya. | Tidak — justru satu pintu masuk dipertahankan. |
| **Statement** | Per kontrak; `TotalPaid = SumTerminsByUnit`; sengaja memfilter `CountsTowardPrice` | Statement 360 jadi **multi-section**: Harga Rumah / Biaya Realisasi / Produk Tambahan, satu section per charge group. Filter existing tetap benar untuk section harga. | Tidak, jika tiap section memanggil summary group-nya sendiri. |
| **Outstanding** | Satu formula: `GrossAmount − Σ termin(price)` | Menjadi `ChargeGroupSummary` ber-`kind`; `kind=price` **mendelegasikan** ke formula lama (CG-INV-1). | **Titik risiko #1.** Bila `price` dihitung ulang di layer baru, kita punya 2 kebenaran. Harus dijaga EQ test. |
| **Dashboard** | `receivables()` → `PortfolioOutstanding` + forecast/top-debitur dari `BuildARAging` | Tambah KPI terpisah "Tagihan Biaya Realisasi". **KPI existing tidak boleh berubah artinya.** | **Titik risiko #2.** Mencampur realisasi ke KPI piutang lama akan mengubah angka historis owner tanpa dia minta. |
| **Aging** | `BuildARAging` fungsi murni dari `payment_schedules` | Charge item ber-`due_date` disuapkan ke **mesin yang sama** lewat adapter `[]ARScheduleRow`, dengan dimensi `source`. Laporan default tetap price-only. | Tidak, **asalkan tidak ada mesin aging kedua**. |
| **Collection Rate** | `collected / scheduled` decimal | Harus punya rate per `kind`, plus rate gabungan yang eksplisit dilabeli. | **Titik risiko #3.** Rate campuran tanpa label = angka yang menyesatkan owner. |

---

## 5. Fokus #4 — Analisis duplicate source of truth

Empat kandidat duplikasi, dan penangkalnya:

1. **Outstanding harga rumah dihitung dua kali** (lama di `financial_summary.go`, baru di charge group).
   → Penangkal: CG-INV-1 delegasi + **EQ test**: untuk setiap kontrak, `ChargeGroupSummary(price).Outstanding == ContractFinancialSummary.Outstanding`, persis, tanpa toleransi. Masuk ke suite kesetaraan `internal/reporting` (tag `integration`) yang sudah jadi gate produksi.

2. **Dua ledger kas** (`termin_payments` + tabel pembayaran realisasi baru).
   → Penangkal: **jangan buat tabel kas kedua.** Semua kas masuk tetap `termin_payments` (yang sebenarnya sudah menjadi "Penerimaan Kas" umum sejak booking fee). Charge group menempel sebagai kolom, bukan sebagai tabel saingan.

3. **Dua mesin aging.**
   → Penangkal: `BuildARAging` tetap satu-satunya; charge item diadaptasi ke `ARScheduleRow`, bukan diberi mesin sendiri.

4. **`counts_toward_price` vs `charge_group.kind`** — dua penanda untuk satu makna.
   → Penangkal: target akhir `counts_toward_price` menjadi **turunan** (`kind == price`), dipertahankan sementara sebagai kolom redundan selama transisi, dengan test yang menegakkan kesetaraan — persis pola `HPPEligibleCategories()`. Dihapus di tahap terakhir.

---

## 6. ERD usulan

```
customers ────────────┐
                      │
units ──────┐         │
            │         │
sale_contracts        │
     │  1               │
     │                  │
     ▼  n               ▼
┌──────────────────────────────────────────────────────────┐
│ charge_groups                                            │
│──────────────────────────────────────────────────────────│
│ id                BIGINT UNSIGNED PK                     │
│ tenant_id         BIGINT UNSIGNED NOT NULL   (idx)       │
│ customer_id       BIGINT UNSIGNED NOT NULL               │
│ unit_id           BIGINT UNSIGNED NULL                   │
│ sale_contract_id  BIGINT UNSIGNED NULL                   │
│ kind              ENUM/VARCHAR  price|realization|       │
│                                 addon|booking            │
│ label             VARCHAR(120)   "Biaya Realisasi KPR"   │
│ status            open|settled|cancelled                 │
│ allocation_policy priority|proportional                  │
│ created_at / updated_at  DATETIME(3)                     │
│                                                          │
│ UNIQUE (tenant_id, sale_contract_id, kind)               │
│        WHERE kind IN ('price','booking')   ← menutup     │
│                       lubang FindContractByUnitID        │
└──────────────────────────────────────────────────────────┘
     │ 1
     │
     ▼ n
┌──────────────────────────────────────────────────────────┐
│ charge_items                                             │
│──────────────────────────────────────────────────────────│
│ id, tenant_id                                            │
│ charge_group_id   NOT NULL (idx)                         │
│ product_type_code VARCHAR(40) NOT NULL  → product_types  │
│                   (routing akun & pajak, BUKAN hardcode) │
│ label             "PDAM" / "BPHTB" / "Notaris"           │
│ amount            DECIMAL(20,4) NOT NULL DEFAULT 0.0000  │
│ due_date          DATE NULL      → masuk aging           │
│ settle_priority   INT NOT NULL DEFAULT 0                 │
│ status            open|settled|cancelled                 │
└──────────────────────────────────────────────────────────┘

invoices          : sale_contract_id → NULLABLE
                    + charge_group_id BIGINT UNSIGNED NULL (idx)
                    CHECK (sale_contract_id IS NOT NULL
                           OR charge_group_id IS NOT NULL)

termin_payments   : + charge_group_id BIGINT UNSIGNED NULL (idx)
                    counts_toward_price → turunan kind, transisi

payment_allocations : allocation_type + 'charge_item'
                      + charge_item_key generated column
                      UNIQUE (tenant_id, termin_payment_id, charge_item_key)

receipts          : receipt_type + 'realization' (KWR), 'addon'
                    + charge_group_id NULL
```

**Catatan desain penting:** `charge_groups` **tidak menyimpan `total_amount`**. Total = `Σ charge_items.amount`, dihitung, tidak di-cache sebagai kebenaran. Ini menghindari kandidat duplicate SoT ke-5.

**Kenapa `product_type_code` dan bukan `account_code`:** katalog produk hasil sprint kemarin sudah menjadi rule kanonik routing akun + kategori. PDAM sudah ada di katalog. Menaruh kode akun langsung di `charge_items` akan menciptakan jalur routing kedua yang lolos dari `ValidateRevenueAccount` — persis fail-open yang baru saja ditutup di M-1.

---

## 7. Business Flow

### 7.1 CASH — biaya realisasi Rp20jt, dibayar Rp15jt

```
1. Akad / setelah kontrak
   Sistem membuat charge_group(kind=realization, contract, customer)
   Item: PDAM 3jt · Notaris 10jt · BPHTB 5jt · Listrik 2jt   → total 20jt

2. Terbit invoice biaya realisasi (1 invoice untuk 1 group, Rp20jt)

3. Customer bayar Rp15jt
   ReceivePayment(anchor = charge_group_id, amount = 15jt)
     ├─ idempotency check              (mesin lama)
     ├─ alokasi 15jt ke item           (policy priority / proportional)
     │    Σ alokasi == 15jt PERSIS     (invariant #3, largest-remainder)
     ├─ jurnal: Dr Kas 15jt
     │          Cr akun per item, sesuai katalog produk
     ├─ payment_allocations (type=charge_item)   (sub-ledger lama)
     └─ Receipt KWR                    (mesin lama, provider baru)

4. Kwitansi KWR menampilkan — persis permintaan klien:
      Total tagihan     Rp 20.000.000
      Pembayaran ini    Rp 15.000.000
      Total dibayar     Rp 15.000.000
      Sisa terutang     Rp  5.000.000

5. Outstanding harga rumah TIDAK bergerak. Gate BAST TIDAK terpengaruh.
   (counts_toward_price = false, by kind)
```

### 7.2 KPR

Sama, minus BPHTB (grup berisi PDAM · Notaris · Listrik). Template item ditentukan `payment_scheme` — mesin `PaymentSchemePolicy` + registry sudah ada, tinggal menambah "template charge group" ke policy. Tidak ada if-else skema yang ditulis manual.

### 7.3 Produk tambahan (kelebihan tanah)

`charge_group(kind=addon)` dengan satu/lebih item ber-`product_type_code = kelebihan_tanah`. Hari ini hal ini "bisa" dilakukan dengan mengarang unit + kontrak sendiri — tapi itu memaksa penjual membuat unit palsu, dan menyeret HPP/persediaan ikut terlibat. Dengan charge group, add-on adalah tagihan murni.

### 7.4 Harga rumah (TIDAK BERUBAH)

Tetap `SaleContract → PaymentSchedule → Invoice → TerminPayment → KWT`. Charge group `kind=price` dibuat sebagai **bayangan** dari kontrak, hanya agar UI dan kwitansi punya satu bahasa. **Nol perubahan angka.**

---

## 8. TIGA keputusan yang harus dijawab owner/klien lebih dulu

Saya tidak menebak ketiganya. Ini yang dimaksud CLAUDE.md "celah yang ditandai lebih baik daripada angka yang salah".

### K-1 — Perlakuan akuntansi biaya realisasi (paling penting, berkonsekuensi pajak)

Saat developer menerima Rp15jt untuk PDAM/Notaris/BPHTB/Listrik, uang itu:

* **Opsi A — Titipan (kewajiban).** `Dr Kas / Cr 2-2xxx Titipan`. Developer sekadar meneruskan ke PLN/PDAM/notaris/kas negara. Tidak ada pendapatan, tidak ada laba. Konsisten dengan `NotaryDeposit` yang sudah berjalan (2-2300).
* **Opsi B — Pendapatan jasa.** `Dr Kas / Cr 4-2000`. Berlaku bila developer menagih lebih dari biaya sebenarnya dan mengambil margin. Menimbulkan objek PPN/PPh yang harus dipastikan.
* **Opsi C — Campur per item.** BPHTB & Notaris = titipan; PDAM & Listrik = pendapatan jasa bila ada margin.

**Rekomendasi saya: Opsi C, di-drive data lewat katalog produk** (tiap `product_type` sudah punya kategori + akun). Bukan hardcode per nama biaya.

`// TODO(tax-advisor): apakah penagihan PDAM/Listrik dengan margin menimbulkan objek PPN bagi developer? BPHTB dipastikan titipan (kewajiban pembeli), tapi butuh konfirmasi.`

### K-2 — Apakah biaya realisasi menahan BAST / akad?

Hari ini `GateFullPayment` membaca `TotalReceived` price-only. Bila klien mau "serah terima kunci ditahan sampai biaya realisasi lunas", itu **perubahan aturan gate**, bukan detail teknis.
**Default yang saya pakai bila tidak dijawab: TIDAK menahan** (perilaku hari ini dipertahankan).

### K-3 — Urutan alokasi pembayaran sebagian

Rp15jt dari total Rp20jt masuk ke item mana? **Prioritas** (Notaris lunas dulu, lalu BPHTB, dst.) atau **proporsional** (75% tiap item)?
Ini bukan kosmetik — ia menentukan akun mana yang dikredit dan berapa.
**Rekomendasi: prioritas berurutan** (default), karena titipan legalnya diperuntukkan spesifik; proporsional disediakan sebagai policy alternatif per group. Apa pun pilihannya, invariant #3 berlaku: `Σ alokasi == pembayaran`, persis, sisa pembulatan largest-remainder.

---

## 9. Impact Analysis

### Ditambah (file baru)

```
internal/billing/charge_group.go          model + domain rule
internal/billing/charge_group_service.go  buat group, hitung summary
internal/billing/charge_repository.go
internal/billing/charge_allocation.go     policy alokasi (priority|proportional)
migrations/000059_charge_groups.up/down.sql
migrations/000060_charge_group_backfill.up/down.sql
migrations/000061_invoice_charge_group.up/down.sql
frontend/app/(app)/penjualan/[unitId]/tagihan/…
```

### Diubah (aditif — perilaku lama dipertahankan)

| File | Perubahan | Risiko |
|---|---|---|
| `internal/billing/model.go` | `SaleContractID` → `*uint64`, `+ChargeGroupID` | **Sedang** — semua pembaca invoice ikut berubah |
| `internal/billing/receipt.go` | `+ReceiptType` realization/addon, `+ChargeGroupID` | Rendah (aditif) |
| `internal/billing/receipt_service.go` | Ringkasan cetak dari provider ber-kind | Rendah, sudah ada `HasSummary` |
| `internal/sale/collection.go` | Resolver anchor menerima charge group | **Tinggi** — jantung uang masuk, wajib EQ test |
| `internal/sale/model.go` | `+ChargeGroupID` di TerminPayment | Rendah |
| `internal/sale/payment_allocation.go` | `allocation_type = charge_item` | Sedang |
| `internal/sale/statement.go` | Statement multi-section | Rendah (presentasi) |
| `internal/reporting/ar_aging.go` | Dimensi `source`, mesin tetap satu | **Tinggi** — menyentuh KPI owner |
| `internal/reporting/dashboard.go` | KPI baru terpisah | Sedang |
| `internal/scheme/policy.go` | Template charge group per skema | Rendah |
| `internal/ledger/account_role.go` | Role `RealizationLiability` / `RealizationRevenue` | Rendah, mengikuti K-1 |

### TIDAK boleh disentuh

`financial_summary.go` · `repository.go:SumTerminsByUnit` · rumus `GrossAmount − paid` · seluruh jalur HPP/BAST/allocation_snapshots.

---

## 10. Migration Strategy

Memakai pola bertahap yang sudah **terbukti berhasil di tim ini** pada FE-2 `payment_allocations` (schema → dual-write → backfill → reader).

| Fase | Isi | Perilaku berubah? |
|---|---|---|
| **P1 Schema** | `charge_groups`, `charge_items`, kolom nullable di invoices/termin_payments/receipts. Belum ada kode yang membaca. | **Tidak** |
| **P2 Shadow backfill** | Buat charge group `kind=price` untuk **setiap** kontrak existing (1:1) dan `kind=booking` untuk booking fee. Isi `charge_group_id` di invoice & termin historis. | **Tidak** |
| **P3 Gate kesetaraan** | EQ test: untuk seluruh kontrak, `ChargeGroupSummary(price) == ContractFinancialSummary`, persis. **Fase berikutnya diblokir sampai hijau.** | **Tidak** |
| **P4 Realisasi end-to-end** | Group realization + invoice + kwitansi KWR + collection + alokasi. Fitur baru murni. | **Ya (aditif)** |
| **P5 Add-on** | `kind=addon`. | Ya (aditif) |
| **P6 Pelaporan** | Aging & dashboard multi-source. KPI lama dipertahankan artinya, KPI baru dilabeli terpisah. | Ya (presentasi) |
| **P7 Cleanup** | `counts_toward_price` menjadi turunan `kind`; kolom redundan dihapus setelah 1 rilis stabil. | Tidak |

Rollback: P1–P3 murni aditif dan dapat di-`down` tanpa kehilangan data. Titik tanpa-jalan-pulang baru muncul di P4 (kwitansi KWR sudah dicetak ke customer).

---

## 11. Risiko

| # | Risiko | Dampak | Mitigasi |
|---|---|---|---|
| R-1 | Outstanding harga rumah punya 2 kebenaran | **Kritis** — laporan owner tidak konsisten | CG-INV-1 (delegasi) + EQ test P3 sebagai gate |
| R-2 | KPI piutang lama berubah arti tanpa diminta | Tinggi — owner kehilangan kepercayaan pada angka | KPI realisasi **selalu terpisah**; rate campuran wajib berlabel |
| R-3 | K-1 dijawab salah (titipan vs pendapatan) | **Tinggi — pajak** | Blokir P4 sampai K-1 dijawab; TODO(tax-advisor) sudah dipasang |
| R-4 | `FindContractByUnitID` `.First()` tanpa unique key | Tinggi, senyap | Unique key `(tenant, contract, kind)` di P1; larangan keras "kontrak kedua per unit" |
| R-5 | `termin_payments` makin banyak makna | Sedang | `kind` sebagai sumber makna tunggal; pembaca lama tetap aman karena `counts_toward_price` dipertahankan sampai P7 |
| R-6 | Alokasi sebagian meninggalkan sisa pembulatan | Sedang | Invariant #3, largest-remainder deterministik, test `Σ parts == original` |
| R-7 | Scope creep ke gate BAST | Sedang | K-2 dijawab dulu; default = perilaku hari ini |
| R-8 | Nomor kwitansi/invoice bentrok antar seri | Rendah | `ReceiptSequence` per `DocType` sudah ada, seri KWR ikut mesin yang sama |

---

## 12. Tahapan implementasi yang saya usulkan

```
B2-0  Keputusan owner: K-1, K-2, K-3            ← blocking, tidak bisa dilewati
B2-1  P1+P2  Schema & shadow backfill           (tanpa perubahan perilaku)
B2-2  P3     Gate kesetaraan EQ                 ← wajib hijau sebelum lanjut
B2-3  P4     Biaya Realisasi end-to-end
             (group, invoice, KWR, collection, alokasi, FE)
B2-4  P5     Produk tambahan (addon billing)
B2-5  P6     Aging & Dashboard multi-source
B2-6  P7     Cleanup counts_toward_price
```

Spesifikasi frontend per increment (halaman, wireframe, flow, state, aksi, endpoint) dibuat pada awal B2-3, sesuai konvensi kerja yang berlaku — tidak dicantumkan di sini karena review ini read-only dan bentuk UI bergantung jawaban K-1..K-3.

---

## 13. Kesimpulan

Arsitektur saat ini **tidak cukup** untuk business rule baru, tetapi **tidak perlu dibongkar**. Yang hilang adalah satu entitas: agregat penagihan di antara kontrak dan invoice.

Yang membuat saya yakin ini increment yang aman, bukan refactor berisiko:

1. Mesin uang masuk (`ReceivePayment`) sudah satu pintu, atomik, dan idempoten — tidak perlu disentuh secara struktural.
2. Sub-ledger alokasi sudah ada dan sudah general (`payment_allocations`).
3. `ReceiptPrintData` **sudah** memiliki persis empat angka yang diminta klien; hanya sumbernya yang berubah.
4. Konsep "kas masuk yang bukan harga rumah" sudah hidup di produksi lewat booking fee.
5. Routing akun sudah data-driven lewat katalog produk yang baru saja dikeraskan.

Yang benar-benar baru hanyalah: **wadah tagihan yang punya total dan sisa**.

Risiko utamanya bukan teknis, melainkan **akuntansi (K-1)** dan **arti KPI (R-2)**. Keduanya keputusan bisnis, dan keduanya saya tinggalkan terbuka.

**Rekomendasi: jawab K-1/K-2/K-3, lalu jalankan B2-1 → B2-2. Jangan mulai B2-3 sebelum gate kesetaraan hijau.**
