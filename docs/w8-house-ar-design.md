# W-8 — Piutang Harga Rumah: satu sumber kebenaran, di-tie-out dengan Buku Besar

Status: DESAIN (pra-implementasi) · Tanggal audit data: 2026-08-11
Prasyarat yang dibaca ulang: `docs/post-w7-architecture-accounting-audit.md`,
`docs/w7-legacy-ar-design.md`, memori `w4-receivable-ownership`, `w5-j14a-piutang-realisasi`,
`t3-kpr-shortfall-customer-receivable`.

---

## 0. Keputusan yang MENGIKAT dan tidak dibuka lagi

- **BD-1 (FINAL, owner).** Piutang harga rumah lahir secara akuntansi **saat BAST**.
  Jadwal pembayaran sebelum BAST adalah **jadwal penagihan**, bukan piutang.
- **T-3 (FINAL, klien).** Antara akad dan pencairan, sisa tagihan KPR adalah piutang
  **BANK** (`1-2200`). Setelah pencairan, bank selesai pada nilai cairnya dan sisanya
  menjadi piutang **CUSTOMER** (`1-2000`).
- **INV-AR-1.** Hanya ada satu mesin aging: `internal/receivable`. `Source` adalah
  dimensi, bukan alasan membuat laporan kedua.
- **BD-2.** Allowance/impairment/write-off **di luar cakupan W-8**.

---

## 1. Lifecycle piutang harga rumah — dari sumber sampai GL (FACT)

| # | Peristiwa | Kode | Jurnal |
|---|---|---|---|
| 1 | Terima uang pra-BAST | `sale.ReceivePayment` (`service.go:349`) | Dr Kas/Bank / **Cr 2-2000 Uang Muka** |
| 2 | **BAST** | `sale.RecordBAST` (`service.go:425-589`), baris jurnal di `buildEvent3Lines` (`:662-670`) | Dr 2-2000 sebesar uang muka · **Dr akun piutang sebesar `gross − advance`** · Cr 4-1000 |
| 3 | Akun piutang mana | `schemeBASTGuard` (`service.go:526`) → `scheme.ResolveReceivableAccount` (`scheme/policies.go:237`) | KPR di state `akad` → **1-2200**; selain itu → **1-2000** |
| 4 | Pencairan KPR | `ReceivePayment` + `schemeCreditAccountForPayment` (`service.go:357`) | Dr Bank / Cr 1-2200 |
| 5 | Reklas T-3 | `reclassOnStateChange` → `reclassReceivableIfBAST` (`scheme_flow.go:418-459`) | Dr 1-2000 / Cr 1-2200 sebesar **`gross − Σ termin`** |
| 6 | Pelunasan pasca-BAST | `ReceivePayment` (`service.go:350-372`) | Dr Kas/Bank / Cr akun piutang efektif |

**Rumus outstanding yang SUDAH dipakai ledger hari ini** — dua tempat, identik:

```go
// service.go:364-371  (guard overpayment)
// scheme_flow.go:426-431 (nilai reklas T-3)
collected, _ := s.store.SumTerminsByUnit(ctx, tenantID, unitID) // counts_toward_price = TRUE
outstanding := saleRecordGross(saleRec).Sub(collected)
```

`SumTerminsByUnit` (`repository.go:147-158`) menjumlahkan `termin_payments` dengan
`counts_toward_price = TRUE`. Setiap baris `termin_payments` ditulis **dalam transaksi
yang sama** dengan jurnalnya (`journal_entry_id NOT NULL`).

> **INFERENCE (terbukti angkanya di §2):** `gross(sale_record) − Σ termin(counts_toward_price)`
> adalah persis saldo yang GL tahan di akun piutang untuk unit itu. Piutang harga rumah
> **sudah punya sub-ledger transaksional** — hanya tidak pernah dibaca oleh laporan.

**Yang dibaca laporan hari ini** (`reporting/repository.go:214-236`):

```sql
FROM payment_schedules ps ... WHERE ps.tenant_id = ? AND ps.status <> 'superseded'
```

Tidak menyentuh `sale_records`, tidak menyentuh `termin_payments`, tidak menyentuh jurnal.
Itulah retaknya.

---

## 2. Audit data existing (5 langkah wajib) — angka nyata, 2026-08-11

Semua angka dari database dev/UAT yang berjalan. Tidak ada data yang diubah.

### 2.1 Tie-out konsolidasi per tenant

`house_derived` = `Σ (gross(sale_record) − Σ termin counts_toward_price)` untuk
`sale_records` yang belum dibatalkan.

| tenant | GL 1-2000 | GL 1-2200 | GL AR total | sub legacy | sub realisasi | **house_derived** | **selisih** | aging (query lama) |
|---|---|---|---|---|---|---|---|---|
| 1 | 3.500.000.000 | 0 | 3.500.000.000 | 0 | 0 | **3.500.000.000** | **0** | 0 |
| 3 | 0 | 0 | 0 | 0 | 0 | **0** | **0** | 2.800.000.000 |
| 4 | 0 | 0 | 0 | 0 | 0 | **0** | **0** | 1.300.000.000 |
| 5 | 0 | 0 | 0 | 0 | 0 | **0** | **0** | 2.200.000.000 |
| 6 | 0 | 0 | 0 | 0 | 0 | **0** | **0** | 300.000.000 |
| 10 | 0 | 20.000.000 | 20.000.000 | 0 | 0 | **20.000.000** | **0** | 30.000.000 |
| 11 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 |
| 9900245 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 |
| 9900246 | 0 | 0 | 0 | 0 | 0 | **0** | **0** | 200.000.000 |
| 9900777 | 100.000.000 | 0 | 100.000.000 | 100.000.000 | 0 | 0 | **0** | 0 |
| 9901005 | 675.725.000 | 0 | 675.725.000 | 650.000.000 | 25.725.000 | 0 | **0** | 0 |

**FACT — hasil pokok audit:** dengan definisi turunan di atas,
`GL(1-2000) + GL(1-2200) == legacy + realisasi + house` **persis nol selisih di SEMUA
tenant, termasuk tenant 1 dan tenant 10.** Formula tie-out yang di audit post-W7
dinyatakan FALSE ternyata benar — yang salah bukan formulanya, melainkan **angka house
yang dipakai** (jadwal, bukan pengakuan BAST).

### 2.2 Selisih laporan lama vs GL, dan sebabnya (langkah 4 & 5)

| Tenant/unit | Aging lama | GL | Selisih | Klasifikasi sebab |
|---|---|---|---|---|
| 3, 4, 5, 6 | 6.600.000.000 | 0 | +6.600.000.000 | **C-1** Jadwal pra-BAST ditampilkan sebagai piutang. Tidak ada `sale_records`. |
| 9900246 unit 1638 | 200.000.000 | 0 | +200.000.000 | **C-1** idem (`scheme_state = signed`, belum BAST). |
| 10 unit 467 | 5.000.000 | 0 | +5.000.000 | **C-1** idem (`fully_paid`, tanpa BAST). |
| 10 unit 468 | 25.000.000 | 20.000.000 | +5.000.000 | **C-2** Booking fee 5jt (`termin 643`, `counts_toward_price=1`, Cr 2-2100→2-2000) mengurangi harga di GL tetapi tidak pernah dialokasikan ke baris jadwal mana pun. Jadwal (25+225=250jt) mencatat harga penuh **dan** booking fee-nya. |
| 1 unit 1 & 2 | 0 | 3.500.000.000 | −3.500.000.000 | **C-3** BAST sudah diakui di GL tetapi unit **tidak punya `payment_schedules` sama sekali** → piutang 3,5 M tidak terlihat di laporan piutang mana pun. |

Total: laporan lama menampilkan **6.830.000.000**; eksposur GL yang sesungguhnya
**3.520.000.000**. Kedua himpunan hampir tidak beririsan.

### 2.3 Temuan data yang PERLU keputusan (tidak disentuh W-8)

**E-1 — tenant 10, unit 468: 20.000.000 tertahan di `1-2200` padahal `scheme_state = disbursed`.**
Jurnal BAST 768 (2026-07-21) mendebit 1-2200 220jt, pencairan 775 (2026-07-22) mengkredit
200jt, dan **tidak ada jurnal reklas T-3**. Bandingkan tenant 9900246 unit 1637 yang
punya jurnal reklas 2998. Sebabnya: data ini dibuat **sebelum T-3 dirilis (2026-08-05)**;
`reclassOnStateChange` di kode sekarang akan memicunya. Ini **data historis, bukan bug
kode berjalan**. W-8 **tidak** memperbaikinya diam-diam — koreksinya adalah jurnal reklas
yang harus diputuskan dan diposting operator.

**E-2 — booking fee ganda di jadwal (tenant 10 unit 468, 5jt).** Jadwal pembayaran
menjumlahkan harga penuh tanpa memperhitungkan booking fee yang `counts_toward_price`.
Setelah W-8 angka aging mengikuti GL, jadi **tidak berdampak pada laporan piutang**;
tetapi generator jadwal tetap menghasilkan rencana yang over-stated 5jt. Dicatat sebagai
utang teknis, bukan cakupan W-8.

---

## 3. Audit `1-2100` dan `1-2200` (diminta sebelum memutuskan cakupan)

**`1-2100` "Piutang Lain-lain" — BUKAN piutang customer. Dikeluarkan dari W-8.**
Satu-satunya penulisnya adalah `commission/service.go:363-371` (clawback komisi:
Dr 1-2100 / Cr Beban Komisi). **Debiturnya salesperson, bukan pembeli.** Di database:
**nol baris jurnal**. Tidak butuh sub-ledger customer, tidak butuh keputusan baru.

**`1-2200` "Piutang Bank (KPR)" — bagian dari house AR, tetapi debiturnya BANK.**
Didebit saat BAST bila kontrak KPR berada di `StateAkad`; dikredit saat pencairan;
sisanya direklas ke `1-2000` per T-3. Karena T-3 sudah FINAL, tidak ada keputusan baru
yang dibutuhkan. Konsekuensinya untuk W-8 dinyatakan eksplisit di D-W8-4 dan D-W8-6.

---

## 4. Keputusan desain W-8

### D-W8-1 — House AR adalah **pembaca turunan**, bukan tabel sub-ledger baru

Tabel baru akan menjadi tempat kedua yang bisa menyimpang, dan §2.1 membuktikan
sumbernya sudah ada dan sudah tie-out sempurna. W-7 butuh tabel karena piutang lama
memang tidak punya jejak transaksi; house AR punya — `sale_records` (peristiwa
pengakuan) dan `termin_payments` (peristiwa pembayaran), keduanya ditulis satu
transaksi dengan jurnalnya.

**Konsekuensi: tidak ada migrasi angka, tidak ada backfill.** Migrasi yang dibuat W-8
hanya untuk hal yang belum terekam sama sekali (lihat D-W8-8), bukan untuk memindahkan
saldo.

### D-W8-2 — Piutang lahir tepat saat BAST

Predikat lahirnya piutang: **ada baris `sale_records` untuk unit itu dan
`cancelled_at IS NULL`**. Ini predikat yang sama yang dipakai `ReceivePayment`
(`service.go:337-341`) untuk memutuskan akun kredit — jadi tidak ada definisi kedua.
Jadwal pra-BAST **tidak pernah** menjadi `receivable.Row`.

### D-W8-3 — Nilai outstanding = rumus yang sudah dipakai ledger

`gross(sale_record) − Σ termin_payments(counts_toward_price = TRUE)`.
Dipindahkan menjadi satu fungsi ber-nama di `internal/sale` dan dipakai bertiga oleh
guard overpayment, reklas T-3, dan pembaca AR. Tiga pemakai, satu rumus.

### D-W8-4 — Akun kontrol per unit dari `scheme.ResolveReceivableAccount`

Sumber yang sama yang menentukan ke mana BAST mendebit. Kontrak tanpa scheme → `1-2000`.
Setiap baris house AR membawa `ControlAccountCode`, seperti `legacy_receivables` (W-7).

### D-W8-5 — Struktur jatuh tempo: outstanding GL **dialokasikan** ke jadwal pasca-BAST

Aging butuh tanggal; GL hanya tahu satu angka per unit. Aturannya:

1. Ambil jadwal `status <> 'superseded'` milik unit, urut `due_date, id`.
2. Alokasikan outstanding GL ke jadwal-jadwal itu, **sisa jadwal (`amount − paid_amount`)
   sebagai kapasitas**, berurutan dari jatuh tempo terawal.
3. Bila outstanding habis sebelum jadwal habis → jadwal sisanya tidak menghasilkan baris.
4. Bila jadwal habis tetapi outstanding masih ada, **atau unit tidak punya jadwal sama
   sekali** → sisanya menjadi **satu baris** dengan `due_date = tanggal BAST`.

Dengan konstruksi ini `Σ outstanding baris house == outstanding GL` **selalu**, tanpa
pembulatan (semua nilai `domain.Money`, penyerapan sisa deterministik di baris terakhir).
Kasus (4) adalah jawaban untuk tenant 1 (3,5 M tanpa jadwal): piutang yang sudah
diserahterimakan dan tidak punya rencana penagihan **jatuh tempo pada hari serah terima**
— ia tidak boleh diam-diam dianggap "belum jatuh tempo" selamanya.

### D-W8-6 — Layar "Piutang Customer" vs tie-out: dua pertanyaan berbeda

Konsekuensi langsung T-3: eksposur yang duduk di `1-2200` **debiturnya bank**.

- **Daftar aging `/accounting/receivable` (`source=house`)** menampilkan baris yang akun
  kontrolnya adalah piutang **customer** (`1-2000`). Menaruh piutang bank di layar
  bernama "Piutang Customer" akan menagih orang yang salah.
- **Tie-out** membandingkan **seluruh** house AR (semua akun kontrol) dengan GL, dipecah
  **lapis per lapis** persis pola W-7: lapis `1-2000` (customer) dan lapis `1-2200` (bank).

Keduanya turunan dari himpunan baris yang sama; tidak ada dua sub-ledger.

### D-W8-7 — `Reverse` tidak boleh membuat GL dan sub-ledger berbeda (R-1, R-2, R-4)

Membalik jurnal tetap **satu-satunya** cara sah mengoreksi ledger (Invariant #5). Yang
ditambahkan bukan jalan pintas mutasi, melainkan dua penjaga:

- **R-2 — periode tertutup.** `reverseWithin` (`ledger/posting_service.go:348`) tidak
  pernah memanggil `IsPeriodClosed`; hanya `Create` (`:133`) yang memanggilnya. Pembalik
  bertanggal di dalam periode tertutup karena itu lolos. Ditambahkan pengecekan yang sama
  atas `reverseDate`.
- **R-1 — jurnal bertuan.** Endpoint generik `POST /ledger/journals/{id}/reverse`
  (`handler.go:121`, `:597-632`) hanya memvalidasi tenant, id, dan format tanggal. Jurnal
  BAST dan jurnal penerimaan pembayaran adalah **milik domain `sale`**: membalikkannya
  dari layar Jurnal memindahkan GL tanpa menyentuh `sale_records`/`termin_payments`,
  sehingga tie-out pecah. Ditambahkan seam `JournalOwnership` di `ledger`: bila id jurnal
  dirujuk sebuah sub-ledger, pembalikan generik **ditolak 409** dengan pesan yang
  menyebut jalur domain yang benar. Bukan berdasarkan `source` (semua jurnal `sale`
  ber-`source = "system"`), melainkan berdasarkan **kepemilikan data** —
  `sale_records.revenue_journal_id`, `termin_payments.journal_entry_id`, dan seterusnya.

### D-W8-9 — Sisi GL diklaim lewat **kepemilikan data**, bukan `source` dan bukan sisa

*(Ditulis saat implementasi P3; MENGGANTIKAN asumsi "kupas lapis pakai
`AccountMovementBySource`" yang tertulis di §5 P3 dan tersirat di D-W8-1..8.)*

Pola W-7 mengupas akun kontrol menurut `journal_entries.source`. Untuk house AR pola itu
**tidak cukup**, dan cara gagalnya berbahaya:

- Query atas seluruh tenant hanya menemukan tiga `source` yang menyentuh akun kontrol
  piutang: `system`, `opening_balance`, `legacy_ar_payment`. Jurnal penjualan **dan**
  jurnal pengakuan biaya realisasi sama-sama `system` — terbukti pada tenant 9901005 yang
  seluruh 25.725.000 `system`-nya adalah realisasi, bukan rumah.
- Menghitung sisi rumah sebagai **sisa** (`saldo − saldo awal − realisasi`) membuat setiap
  jurnal manual yang menyentuh `1-2000` **diam-diam terhitung sebagai piutang rumah**.
  Tie-out lalu **COCOK persis pada kasus yang justru harus ia tangkap**. Alat kontrol yang
  melaporkan "cocok" ketika ada yang salah lebih buruk daripada tidak ada alat sama sekali.

Karena itu klaim atas mutasi GL berasal dari **data yang dimiliki domain penjualan**:

```
sale.ListOwnedJournalIDs  →  sale_records.revenue_journal_id, sale_records.cogs_journal_id,
                             termin_payments.journal_entry_id,
                             contract_payment_events.journal_entry_id
ledger.AccountMovementForJournals(akun, jurnal-jurnal itu + PEMBALIKNYA)
```

Sisa saldo akun kontrol tidak diklaim siapa pun: ia tampil apa adanya sebagai
`ledger_other` (saldo awal piutang lama, tagihan biaya realisasi, jurnal manual) supaya
orang yang membuka neraca tidak menyimpulkan laporan ini salah. Peta kepemilikan yang
sama dipakai dua kali — untuk tie-out dan untuk penjaga reverse R-1 — jadi tidak ada dua
definisi "jurnal milik penjualan" yang bisa menua sendiri-sendiri.

### D-W8-10 — Penjaga R-1 dipasang di **handler**, bukan di `PostingService`

`cancellation/service.go:612,638` dan `commission/service.go:324` memanggil
`posting.Reverse` **secara sah**: di sana pembalik dan sub-ledgernya bergerak dalam satu
transaksi. Menaruh penjaga di dalam `PostingService.Reverse` akan mematikan pembatalan
penjualan — memperbaiki satu lubang dengan merusak fitur yang benar.

Yang ditutup adalah **pintu umumnya**: `POST /ledger/journals/{id}/reverse`. Handler
bertanya ke seam kepemilikan lebih dulu; jurnal bertuan ditolak `409` dengan pesan yang
menyebut jalur domain yang benar. Kegagalan pembaca kepemilikan **menutup**, tidak
membuka: gangguan DB sesaat tidak boleh cukup untuk melubangi ledger.

`ErrPeriodClosed` pada jalur yang sama dipetakan ke `409` dengan jalan keluarnya
(beri tanggal pembalik pada periode berjalan), bukan `500`.

### D-W8-8 — Invariant baru

> **INV-AR-4.** Untuk setiap tenant: `Σ outstanding baris house AR (semua akun kontrol)`
> == `Σ pergerakan akun kontrol piutang di GL yang berasal dari penjualan rumah`.
> Dibuktikan integration test terhadap MySQL nyata melalui jalur produksi
> (`RecordBAST` → `ReceivePayment` → `Reverse`), bukan mock.

---

## 5. Rencana kerja

| Tahap | Isi |
|---|---|
| P1 | `internal/sale/receivable.go` — `ReceivableRows` + `HouseARSummary` (tie-out), rumus outstanding jadi satu fungsi bernama |
| P2 | `internal/reporting` — pasang `WithHouseReader`; aging **dan** KPI tunggakan dashboard (`dashboard.go:305`) memakai pembaca yang sama; query `payment_schedules` lama dihentikan sebagai sumber AR |
| P3 | Tie-out: `GET /sales/receivables/house/reconciliation` — kupas lapis GL vs sub-ledger per akun kontrol lewat **kepemilikan jurnal** (D-W8-9), bukan `AccountMovementBySource` |
| P4 | Penjaga reverse: periode tertutup (`reverseWithin`) + seam kepemilikan jurnal di handler (D-W8-10) |
| P5 | Integration test MySQL nyata: BAST→lahir, bayar sebagian, lunas, reversal, BAST idempoten, rollback, isolasi tenant |
| P6 | Frontend: `/accounting/receivable` (label & empty state sesuai BD-1), panel rekonsiliasi, layar "Jadwal Penagihan" untuk jadwal pra-BAST |

**Catatan penempatan endpoint (P3).** Tie-out tinggal di router `sale`, mengikuti
preseden W-7 (`legacyar` memiliki rekonsiliasinya sendiri): yang memiliki sub-ledger
adalah yang bertanggung jawab membuktikannya cocok. Frontend memanggilnya dari layar
`/accounting/receivable`, tetapi kepemilikan datanya tidak berpindah karena itu.

### 5.1 Bukti P5 — `internal/sale/w8_house_ar_integration_test.go`

Semua angka lahir dari jalur produksi (`CreateContract` → `ReceivePayment` → `RecordBAST`
→ `ApplySchemeEvent`), wiring identik `sale.NewHandler`, MySQL nyata.

| Skenario | Yang dibuktikan |
|---|---|
| A. Lifecycle in-house 500jt | pra-BAST: jadwal ada, house AR **0** baris, GL rumah 0, DP 100jt duduk di `2-2000` (Inv. #7) → BAST: sub-ledger **dan** GL sama-sama 400jt → bayar 150jt: keduanya 250jt → BAST ulang ditolak, angka tak bergeser → pembayaran melebihi sisa ditolak **tanpa** menyisakan termin/jurnal/kwitansi → lunas: keduanya 0 |
| B. KPR 1 M | pasca-akad BAST: 900jt di lapis `1-2200`, `Piutang Customer` tetap **0** (D-W8-6) → pencairan sebagian 800jt: `1-2200`→0 dan `1-2000`→100jt (T-3) — dua lapis bergerak berlawanan, kasus yang lolos bila hanya total yang dibandingkan → kekurangan dilunasi: keduanya 0 |
| C. Penjaga reverse | jurnal termin **dan** jurnal BAST ditolak 409 dari pintu umum; jurnal manual tetap boleh dibalik; periode tertutup ditolak (service + HTTP 409) sementara periode berjalan tetap bisa; **dan** pembalikan yang melewati sub-ledger benar-benar membuat `Matched=false` dengan selisih −150jt |
| D. Isolasi tenant | dua tenant, kode unit sama, angka berbeda: rekonsiliasi masing-masing hanya melihat miliknya |
| E. Komplemen Jadwal Penagihan (P6) | kontrak pra-BAST muncul **hanya** di billing plan — `Piutang Customer` 0 dan tie-out 0 di `1-2000` → setelah BAST unit kedua, unit itu **pindah**: hilang dari billing plan (bukan tampil di dua tempat), sub-ledger & GL sama-sama 320jt → tenant lain melihat 0 unit |

Subtest E menutup celah yang tidak bisa dilihat oleh A–D: keduanya bisa hijau sementara
satu unit dihitung dua kali (sebagai jadwal *dan* sebagai piutang) atau hilang dari
keduanya. Yang diuji adalah sifat komplemennya — predikat SQL billing plan adalah negasi
persis dari predikat house AR — karena itulah yang akan patah bila salah satu query
diubah sendiri-sendiri di kemudian hari.

Subtest C terakhir adalah yang membuat tiga subtest sebelumnya bermakna: tanpa bukti
bahwa tie-out **bisa gagal**, `Matched: true` tidak membuktikan apa pun.

Efek samping yang ditemukan saat menjalankan suite: `cgCleanup` (`internal/charge`) tidak
menghapus `sale_records`, sehingga fixture BAST W-8 menumpuk antar-run dan piutang naik
setiap kali test dijalankan. Diperbaiki di daftar cleanup, bukan dengan melonggarkan
assertion.

## 6. Spesifikasi frontend (P6)

### 6.0 Masalah yang harus diselesaikan layar, bukan hanya angka

BD-1 memindahkan ±6,83 M jadwal pra-BAST KELUAR dari laporan piutang dan memasukkan
±3,52 M eksposur ber-BAST yang sebelumnya tidak terlihat (§2.2). Dua konsekuensi
operasional yang tidak boleh ditemukan sendiri oleh pemakai:

1. **Tim penagihan kehilangan daftar kerjanya.** `/accounting/collection` membaca
   `fetchARAging`. Pasca-W-8 cicilan pra-BAST hilang dari sana — padahal cicilan itu
   *tetap wajib ditagih*. BD-1 mengizinkan menampilkannya sebagai jadwal penagihan;
   yang dilarang adalah **menghitungnya sebagai piutang**. Karena itu layar terpisah,
   bukan tab di dalam laporan piutang.
2. **Angka piutang berubah drastis tanpa penjelasan.** Owner yang membuka layar lama
   akan menyimpulkan data hilang. Layar harus mengatakan definisinya, di tempat angka
   itu muncul — bukan di dokumen ini.

### 6.1 Peta halaman

| Halaman | Sumber | Perubahan |
|---|---|---|
| `/accounting/receivable` | `GET /reports/ar-aging` (sudah memakai pembaca W-8) | copy & empty state sesuai BD-1; **panel rekonsiliasi** baru; tautan ke Jadwal Penagihan |
| `/accounting/receivable` (panel) | `GET /sales/receivables/house/reconciliation` | baru |
| `/accounting/billing-schedule` ("Jadwal Penagihan") | `GET /sales/receivables/house/billing-plan` | halaman baru |
| `/accounting/collection` | `fetchARAging` (tetap) | banner + tautan ke Jadwal Penagihan supaya pekerjaan pra-BAST tidak hilang diam-diam |

### 6.2 Endpoint baru — `GET /sales/receivables/house/billing-plan`

Alasan endpoint (bukan memakai `/schedules/due`): `/schedules/due` menjawab "apa yang
jatuh tempo pada rentang tanggal", bukan "kontrak mana yang belum BAST". Yang dibutuhkan
layar ini adalah **komplemen** house AR — dan komplemen itu harus dihitung di tempat yang
sama dengan house AR agar keduanya tidak bisa berbeda definisi.

Param: `as_of` (opsional, default hari ini). Bentuk:

```jsonc
{
  "as_of": "2026-08-11",
  "total_scheduled": "6830000000",   // Σ nominal cicilan pra-BAST
  "total_outstanding": "5980000000", // Σ sisa (amount − paid)
  "total_overdue": "410000000",
  "unit_count": 24,
  "note": "Belum BAST — belum diakui sebagai piutang (BD-1).",
  "units": [{
    "contract_id": 12, "unit_id": 34, "unit_code": "A-12",
    "buyer_name": "…", "buyer_phone": "…",
    "contract_value": "500000000", "paid": "100000000", "outstanding": "400000000",
    "overdue_amount": "0", "overdue_count": 0, "next_due_date": "2026-09-01",
    "scheme_state": "booking",
    "schedules": [{
      "schedule_id": 88, "installment_number": 1, "type": "dp",
      "due_date": "2026-06-01", "amount": "100000000", "paid": "100000000",
      "outstanding": "0", "status": "paid", "days_overdue": 0,
      "invoice_number": "INV/2026/000012"
    }]
  }]
}
```

`status` memakai **kosakata yang sama** dengan statement (`paid | overdue | due_today |
scheduled`) dan dihitung backend dari satu fungsi yang sama (`scheduleStatusAt`) — bukan
salinan logika kedua di frontend maupun di query baru.

### 6.3 Wireframe

**A. `/accounting/receivable` — panel rekonsiliasi (di bawah kartu ringkasan).**

```
┌─ Rekonsiliasi Buku Besar ↔ Sub-ledger ──────────── per 11 Agu 2026 ─┐
│  ● COCOK   Sub-ledger 3.520.000.000 = Buku Besar (penjualan) 3.520.000.000 │
│                                                                     │
│  Akun kontrol          Sub-ledger    BB (penjualan)  Selisih   Unit  │
│  1-2000 Piutang Cust.  2.720.000.000  2.720.000.000       0     18  │
│  1-2200 Piutang Bank     800.000.000    800.000.000       0      3  │
│                                                                     │
│  Saldo akun 1-2000 seluruhnya 4.100.000.000; 1.380.000.000 di       │
│  antaranya bukan piutang harga rumah (saldo awal, biaya realisasi).  │
│  Dihitung dari 214 jurnal milik penjualan.                          │
└─────────────────────────────────────────────────────────────────────┘
```

Keadaan **tidak cocok**: bilah merah, selisih per lapis ditebalkan, dan kalimat tindakan
— "ada jurnal yang menggerakkan akun kontrol tanpa melewati alur penjualan; periksa Buku
Jurnal pada akun tersebut". Tidak ada tombol "perbaiki": ledger append-only, koreksi hanya
lewat jurnal pembalik (D-W8-7).

**Keadaan tidak cocok bertotal nol.** Merender panel dengan data nyata (tenant 10 — temuan
E-1) memunculkan tampilan yang tidak diantisipasi wireframe: header berkata "TIDAK cocok"
sementara angka ringkasannya "selisih Rp 0", karena `1-2000` dan `1-2200` bergerak
berlawanan 20jt. Tie-out memang dinilai **per akun kontrol** (D-W8-6/T-3) — justru itu yang
membuatnya menangkap kasus ini — tetapi ringkasan yang berbunyi nol terbaca sebagai panel
yang rusak, dan panel yang terbaca rusak akan diabaikan. Header karena itu berganti menjadi
"total sama, N akun kontrol tidak cocok", disertai satu paragraf yang menyebut pasangan akun
yang berlawanan. Ini murni perbaikan tampilan: tidak ada angka, ambang, atau definisi
`matched` yang berubah.

**B. `/accounting/billing-schedule` — Jadwal Penagihan (pra-BAST).**

```
Jadwal Penagihan · belum diserah­terimakan            [ per: 11-08-2026 ]

ⓘ Ini RENCANA PENAGIHAN, bukan piutang. Piutang harga rumah baru diakui
  saat BAST (serah terima). Setelah BAST, unitnya pindah ke Piutang Customer. →

┌ Total dijadwalkan ┐┌ Sudah dibayar ┐┌ Sisa tagihan ┐┌ Lewat jatuh tempo ┐
│  6.830.000.000    ││   850.000.000 ││ 5.980.000.000││  410.000.000 (3)  │

▾ A-12 · Budi Santoso · Rp 500.000.000            sisa 400.000.000   [Statement]
    #1 Uang Muka (DP)   01-06-2026   100.000.000  lunas
    #2 Termin 1         01-09-2026   200.000.000  dijadwalkan
    #3 Pelunasan        01-12-2026   200.000.000  dijadwalkan
▾ B-03 · …                                        ⚠ telat 41 hari
```

Baris unit dapat dibuka/tutup; unit dengan tunggakan tampil terbuka dan diurut lebih
dulu — layar penagihan harus menaruh pekerjaan paling mendesak di atas.

### 6.4 Copy yang diperbaiki (BD-1)

| Tempat | Sebelum | Sesudah |
|---|---|---|
| Tab filter sumber `house` | "Harga Rumah" | "Harga Rumah (pasca-BAST)" — kualifikasinya di tempat orang memilih ruang lingkup; badge per baris tetap "Harga Rumah" supaya tabel tidak berisik |
| Empty state `source=house` | "seluruh jadwal pembayaran harga rumah sudah diterima" — **salah**: jadwal pra-BAST memang tidak pernah masuk sini | "Belum ada piutang harga rumah. Piutang baru diakui saat BAST; jadwal sebelum serah terima ada di Jadwal Penagihan." + CTA |
| Footnote laporan | hanya catatan W-5 (invoice realisasi) | tambah kalimat BD-1 + tautan Jadwal Penagihan |
| Collection | — | banner "Cicilan pra-BAST tidak muncul di sini sejak piutang diakui saat BAST — lihat Jadwal Penagihan" |
| Empty state Collection | "Semua cicilan sudah diterima … Tidak ada yang perlu ditagih" — **salah** dan baru terlihat saat halaman dirender dengan data nyata: tenant uji punya cicilan menunggak 60jt pada kontrak pra-BAST, yang memang tidak pernah masuk layar ini | "Tidak ada piutang outstanding … Cicilan pada kontrak yang belum BAST tidak dihitung di sini — periksa Jadwal Penagihan sebelum menyimpulkan tidak ada tagihan." + CTA |
| Empty state laporan piutang (`all`) | "Semua tagihan lunas" | "Tidak ada piutang outstanding" — klaimnya dibatasi pada piutang, ditutup kalimat ke Jadwal Penagihan |

### 6.5 State & aksi

| State | Perilaku |
|---|---|
| Panel rekonsiliasi gagal dimuat | laporan piutang **tetap tampil**; panel diganti pesan ringkas + tombol muat ulang. Tie-out adalah alat kontrol, bukan syarat membaca laporan. |
| `matched=false` | bilah merah + selisih per lapis; tidak ada aksi mutasi. |
| Jadwal Penagihan kosong | "Semua kontrak sudah diserahterimakan" + tautan ke Piutang Customer (bukan sekadar "tidak ada data"). |
| `as_of` | query param `?as_of=`, sama seperti laporan piutang; server component, tanpa state klien. |

Tidak ada aksi tulis di kedua layar. Pembayaran tetap lewat pintu yang sudah ada
(`RecordPaymentButton` di statement/collection) — W-8 tidak menambah jalur tulis baru.
