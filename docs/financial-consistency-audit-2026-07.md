# Financial Consistency Audit — Single Source of Truth

> 2026-07-26 · **LAPORAN AUDIT — tidak ada kode diubah.** Semua temuan ter-grounding
> (file:line + formula/SQL verbatim), dikumpulkan lewat 6 sweep paralel + verifikasi manual.
> Target: tidak ada satu angka keuangan pun yang bisa dihitung dari dua jalur berbeda.

## Ringkasan eksekutif

Prinsip yang diniatkan (LOCKED di kode): **ledger = SoT semua saldo**; `ContractFinancialSummary`
= "SATU rumus untuk SEMUA konsumen". **Kenyataannya prinsip ini dilanggar secara sistematis oleh
lapisan `reporting/` (dashboard, kpr_pipeline, sales_performance, GetPipelineStats,
GetTaxLiabilityReport)**, yang menghitung ulang angka uang dari tabel dokumen via raw SQL — sering
dengan filter akun/sumber yang berbeda tipis, sehingga dua jalur bisa menghasilkan angka berbeda
untuk nilai yang secara nominal sama.

Laporan ledger inti **PATUH** (balance-sheet, income-statement, project-pl, cash-flow,
trial-balance, general-ledger → lewat `ledger.QueryService` + `ComputeNeraca`/`ComputePL`; closing
juga berbagi jalur ini). Yang **melanggar** adalah dashboard & read-model turunannya, plus
sejumlah recompute di frontend.

**Skor**: dari ~13 keluarga angka keuangan, **1 bersih SSOT (komisi)**, sisanya punya 2–4 jalur.

---

## Peta kanonik (yang SEHARUSNYA jadi satu-satunya sumber)

| Domain | SoT kanonik | Lokasi |
|---|---|---|
| Saldo akun / neraca / trial balance | `ComputeTrialBalance` + `TrialBalance` → `ComputeNeraca` | `ledger/query.go:45,322` · `reporting/service.go:83` |
| Laba rugi (revenue/HPP/margin) | `ComputePL` ← `queryPLRows` | `reporting/service.go:140` · `reporting/repository.go:36` |
| Outstanding & total paid kontrak | `ContractFinancialSummary` ← `SumTerminsByUnit` | `sale/financial_summary.go:69` · `sale/repository.go:138` |
| AR aging / forecast / overdue | `BuildARAging` | `reporting/ar_aging.go:56` |
| Arus kas | `GetCashMovements` → `GetArusKas` | `reporting/repository.go:252` |
| RAB vs realisasi | `GetRABvsRealisasi` ← `SumItemsByCategory` + realisasi | `budget/service.go:280` |
| Biaya kapitalisasi aktual | `cost.AccumulatedByProject` | `cost/repository.go:98` |
| HPP unit | allocation engine → snapshot BAST → true-up finalized | `allocation/engine.go` · `sale/hpp_snapshot.go` · `closing/service.go:185` |
| Pajak (PPh accrual) | `AccrueTax` → `tax_obligations` | `tax/service.go:132` |
| Komisi | `Rule.AmountFor` → `commissions.amount` | `commission/model.go:101` ✅ satu-satunya yang bersih |

---

## TEMUAN per angka keuangan
Format: **Rule → implementasi → kanonik → duplikat → inkonsistensi**.

### F1 · Outstanding kontrak (= harga − Σ pembayaran)
- **Kanonik**: `sale/financial_summary.go:81` `Outstanding = GrossAmount − SumTerminsByUnit`.
- **Call-site domain (konsisten, semua via `SumTerminsByUnit`)**: `collection.go:110,116` `outstandingForContract`; `statement.go:114,217`; `scheme_flow.go:415` (pasca-BAST, pakai sale_record gross); `service.go:277` (guard overpay); `scheme_flow.go:707` (cek lunas).
- **DUPLIKAT (raw SQL)**: `dashboard.go:509-519` receivables (`gross_amount − SUM(tp.amount)`, semua kontrak) — doc-comment-nya sendiri mengaku "MENCERMINKAN ContractFinancialSummary"; `kpr_pipeline.go:119` (`gross − total_paid` per kontrak KPR).
- **INKONSISTEN**: (a) dashboard & KPR memfilter `scheme_state<>'cancelled'`, domain tidak; (b) filter `booking_fee` berbeda (lihat F2) → **di dalam `dashboard.go` sendiri**, `collected`(:361) mengecualikan booking_fee tapi `receivables`(:511) menyertakan → `gross−collected` dihitung di dua basis pada satu laporan.

### F2 · Total paid / collected (= Σ termin_payments)
- **Kanonik primitif**: `sale/repository.go:138` `SumTerminsByUnit` = `SUM(amount)` **SEMUA source**.
- **DUPLIKAT dengan filter berbeda**:
  - `dashboard.go:361` collected → **EXCLUDES `booking_fee`**
  - `sales_performance.go:83` collected → **EXCLUDES `booking_fee`**
  - `kpr_pipeline.go:96` total_paid → **ALL sources**
  - `reporting/repository.go:151` TotalAdvance → **ALL sources**
  - `dashboard.go:511` receivables inner → **ALL sources**
- **INKONSISTEN (berat)**: TIGA varian "sudah dibayar" — all-sources vs booking_fee-excluded. (Ini juga inti design R4; setelah flag `counts_toward_price` masuk, definisi ini WAJIB satu.)

### F3 · Nilai kontrak (= Σ gross_amount aktif)
- **Kanonik**: `ContractFinancialSummary.NetContract` (= GrossAmount) `financial_summary.go:79`.
- **DUPLIKAT**: `dashboard.go:355` (`SUM(sc.gross_amount)`), `sales_performance.go:70`. Formula sama, diimplementasi ulang 2×; filter `scheme_state`.

### F4 · Saldo akun / neraca (trial balance)
- **Kanonik**: `ledger/query.go:45` `ComputeTrialBalance` (fungsi murni, posted-only, net per tipe akun) + `:322` `TrialBalance` (`date <= asOf`) → `reporting/service.go:83` `ComputeNeraca`. **Closing (`closing.go:104,205`) berbagi jalur ini ✅.**
- **DUPLIKAT (raw SQL bespoke)**: `dashboard.go:186-196` KPI Baris-1 (cash, receivable, payable, booking, commission) — direction-CASE SQL sendiri, TIDAK lewat `TrialBalance`.
- **INKONSISTEN**:
  - **Date bound**: neraca `date<=asOf`; dashboard KPI **tanpa** batas tanggal → jurnal posted bertanggal masa depan masuk dashboard tapi tidak di neraca.
  - **Seleksi akun 3 mekanisme**: cash → `category IN(cash,bank)` (dashboard) vs prefix semua `1-` (neraca) vs **kode hardcoded** `1-1100..1-1500` (cash-flow `repository.go:254`); receivable → hardcoded `1-2000/2100/2200` (dashboard) vs semua `1-` (neraca) → akun `1-2300` baru masuk neraca tapi tidak KPI; payable → hanya `2-1000` (dashboard) vs semua `2-` (neraca).

### F5 · Revenue / HPP / Margin / P&L
- **Kanonik**: `reporting/service.go:140` `ComputePL` ← `repository.go:36` `queryPLRows` (ledger, **semua `4-%`/`5-%`**).
- **DUPLIKAT**:
  - `dashboard.go:212` KPI MTD (revenue **semua `4-%`**, HPP **`5-1000` saja**)
  - `dashboard.go:334` per-proyek (revenue **`4-%` KECUALI `4-2000`**, HPP **`5-1000` saja**)
  - `dashboard.go:582` unit_profits (revenue `sale_records.sale_price`, HPP `Σ sr.hpp_*`)
  - `sales_performance.go:76` bast_value & `repository.go:162` SoldContractSum (`sale_records.sale_price`)
- **INKONSISTEN (berat)**: Revenue **3 definisi** (semua `4-%` / `4-%` minus `4-2000` / `sale_records.sale_price`). HPP **3 definisi** (semua `5-%` / `5-1000` saja / `Σ sr.hpp_*`). Setelah true-up mem-posting ke `5-1000` tapi TIDAK menulis ulang `sale_records.hpp_*` → **HPP proyek (ledger) ≠ Σ HPP unit (snapshot)**.

### F6 · Biaya aktual / RAB realisasi — **EMPAT definisi**
- `budget/repository.go:271` `GetRealisasiByProject`: `SUM(cost_entries.amount)` per kategori (**termasuk marketing/other**), join posted.
- `dashboard.go:332` actual_cost: `SUM(journal_lines debit−credit) LIKE '1-3%'` (kapitalisasi saja).
- `cost/repository.go:98` `AccumulatedByProject`: `SUM(journal_lines.debit)` SAJA, 4 kode eksak `1-3000/3100/3200/3300` — komennya eksplisit "NOT from cost_entries.amount" (**kontradiksi** modul budget).
- `closing/service.go:150` `actualCapitalizedCost`: `debit − reversal-credit`, 4 kode.
- **INKONSISTEN (sangat berat)**: beda tabel (`cost_entries` vs `journal_lines`), beda netting (tanpa / semua kredit / hanya reversal), beda cakupan kode (prefix `1-3%` vs 4 kode eksak), beda cakupan kategori.
- **Budget total** `SUM(budgeted_amount)`: kanonik `budget/repository.go:184` `SumItemsByCategory`; DUPLIKAT `dashboard.go:326`. **Progress %**: budget = realisasi/budget per kategori (`service.go:317`); dashboard = actual/total-RAB (`dashboard.go:307`).

### F7 · HPP per unit / alokasi
- **Compute**: engine `allocation/engine.go:17`; aktual dari ledger `allocation/repository.go:109` → `sale/repository.go:172` `GetUnitCost`; budgeted dari pool RAB `service.go:175`; resolver `hpp_resolver.go:77` memilih.
- **Disimpan** saat BAST: `allocation_snapshots` + `sale_records.hpp_*` + jurnal COGS `5-1000`. True-up: `closing/service.go:185` hitung ulang dari ledger, simpan `hpp_trueup_lines`, `FinalizedUnitHPP`.
- **DUPLIKAT baca**: dashboard proyek HPP dari ledger `5-1000` (`dashboard.go:335`) vs `unit_profits` dari `sale_records.hpp_*` beku (`dashboard.go:582`) → **diverge pasca true-up** (F5). Plus FE menjumlah ulang (`SaleRecordCard.tsx:16`).

### F8 · AR aging / forecast / overdue / collection rate
- **Kanonik**: `reporting/ar_aging.go:56` `BuildARAging` dari `payment_schedules` (Outstanding, Overdue, bucket, CollectionRate `:171`, Expected30/60/90 `:132`).
- **DUPLIKAT (raw SQL, tabel sama)**: `dashboard.go:525-532` forecast 30/60/90; `dashboard.go:544-556` top debtors overdue.
- **INKONSISTEN**: filter status berbeda — SQL dashboard `status NOT IN(received,superseded)` vs engine pakai flag `Received`/effectivePaid.

### F9 · Arus kas / cash-in-out
- **Kanonik**: `reporting/repository.go:252` `GetCashMovements` → `GetArusKas` — **kode bank hardcoded** `1-1100..1-1500`.
- **DUPLIKAT**: `dashboard.go:216` MTD & `dashboard.go:388` seri 6-bulan — `category IN(cash,bank)`.
- **INKONSISTEN**: semesta akun berbeda (kode hardcoded vs category) → akun bank baru di luar daftar kode masuk dashboard tapi tak masuk laporan arus kas.

### F10 · Pajak (PPh liability)
- **Accrual (single compute)**: `tax/service.go:132` `AccrueTax` (`formula.go:50`) → `tax_obligations` + posting `5-2000`/`2-4000`.
- **DUPLIKAT laporan**: `tax/service.go:488` `GetTaxReport` DAN `reporting/repository.go:60` `GetTaxLiabilityReport` — dua implementasi agregasi independen atas `tax_obligations` (komen: "query langsung, tidak import package tax").
- **INKONSISTEN (filosofi)**: PPh dari **tabel** `tax_obligations` (bukan ledger `2-4000`); PPN `GetVATReport` `tax/service.go:441` dari **ledger** `2-3000/1-5100`. Dua filosofi SoT dalam satu domain.

### F11 · Buyer credit (satu key JSON, DUA arti)
- (a) `credit_repository.go:20` `creditBalance` = Σ `payment_allocations(buyer_credit)` − Σ `credit_applications` (saldo terakumulasi).
- (b) `collection.go:524` sisa overpay per-transaksi.
- **INKONSISTEN**: dua perhitungan berbeda disajikan di bawah key `buyer_credit` yang sama (`collection.go:171,401`).

### F12 · Komisi — ✅ BERSIH (contoh SSOT yang benar)
- `commission/model.go:101` `Rule.AmountFor` (satu formula) → disimpan `commissions.amount` saat `Calculate`; semua konsumen (jurnal, `sales_performance.go:90`, dashboard) membaca nilai tersimpan. Tak ada recompute. (Dashboard `commission_payable` baca ledger `2-6200` — cocok bila accrual sinkron.)

### F13 · Frontend menghitung ulang angka backend (10 titik)
Semua VIOLATION memakai `parseFloat`/`parseInt`/`BigInt` atas string DECIMAL → **drift pembulatan** vs aritmetika desimal backend:
1. `JournalDetail.tsx:117-118` total debit/kredit (backend punya di `JournalSummary`, tipe detail tak bawa).
2. `SaleRecordCard.tsx:42` PPN = `sale_price×vat_rate`.
3. `SaleRecordCard.tsx:16` total HPP = Σ komponen (BigInt memotong desimal).
4. `DisbursementModal.tsx:79` proyeksi outstanding (baseline duplikat `summary.outstanding`).
5. `AllInvoiceList.tsx:68` total invoice belum terbayar.
6. `AllocationResultTable.tsx:12,36,70,78` grand total + share %.
7. `charts.tsx:150` conversion % funnel.
8. `RecurringJournalView.tsx:162` total template.
9. `charts.tsx:547-554` burn rate & runway.
10. `charts.tsx:504` RAB usage % + status "Over/Waspada/Sehat".
(Benar/tak melanggar: DashboardV2 memakai `margin_pct`/`collection_pct`/`outstanding_total` langsung; % lebar-bar; validasi input form.)

---

## Klasifikasi ringkas

| # | Angka | Jalur | Kanonik | Inkonsistensi nyata? |
|---|---|---|---|---|
| F1 | Outstanding kontrak | 3 (domain, dashboard, kpr) | financial_summary | **Ya** (scheme_state + booking_fee) |
| F2 | Total paid/collected | 5 | SumTerminsByUnit | **Ya** (booking_fee 3 varian) |
| F3 | Nilai kontrak | 3 | NetContract | Formula sama, reimpl |
| F4 | Saldo akun/neraca | 2 (+cashflow) | TrialBalance | **Ya** (date bound + seleksi akun) |
| F5 | Revenue/HPP/margin | 5 | ComputePL | **Ya (berat)** (rev 3, hpp 3 def) |
| F6 | Biaya aktual/realisasi | 4 | (belum ditetapkan) | **Ya (sangat berat)** |
| F7 | HPP unit | 2 baca | snapshot/ledger | **Ya** (pasca true-up) |
| F8 | AR aging/forecast | 2 | BuildARAging | **Ya** (filter status) |
| F9 | Arus kas | 2 | GetCashMovements | **Ya** (kode vs category) |
| F10 | Pajak PPh | 2 laporan | tax_obligations | **Ya** (dua impl + tabel vs ledger) |
| F11 | Buyer credit | 2 arti/1 key | creditBalance | **Ya** (semantik ganda) |
| F12 | Komisi | 1 | AmountFor | Tidak ✅ |
| F13 | FE recompute | 10 | backend | **Ya** (drift float) |

---

## Rencana refactor — satu implementasi per rule

Prinsip: **`reporting/` tidak boleh mengandung SQL uang yang menduplikasi formula domain.** Dashboard
= komposisi read-model kanonik, bukan raw SQL sendiri. Dua fondasi kanonik:

### R-1 · Registry klasifikasi akun (hapus semua kode/kategori tersebar)
Satu tempat memetakan peran akun → himpunan kode: `cash/bank`, `receivable(1-2xxx)`, `payable`,
`inventory(1-3xxx)`, `revenue(4-*)`, `cogs(5-*)`, `titipan(2-2100)`, `komisi(2-6200)`, dst.
Semua query (neraca, dashboard, cash-flow, cost, allocation) membaca dari registry ini —
menghapus hardcode `1-1100..1-1500`, `LIKE '1-3%'` vs 4-kode, `category IN` vs prefix.

### R-2 · `LedgerBalanceService` — satu-satunya pembaca saldo posted
Bungkus `ComputeTrialBalance` jadi API `BalanceOf(role|codes, asOf)`. Dashboard KPI Baris-1
(cash/receivable/payable/booking/commission), cash-flow, MTD sales/HPP **memanggil ini** —
hapus `dashboard.go:186-231,388-398`. Sertakan **date bound** yang sama (F4).

### R-3 · Revenue/HPP/margin lewat `ComputePL` saja (F5)
- Tetapkan SATU definisi revenue (rekomendasi: **semua `4-%` KECUALI akun non-operasional yang
  ditandai eksplisit** — putuskan status `4-2000`) & HPP (**semua `5-%`** atau `5-1000`+adjustments).
- Dashboard per-proyek & MTD margin **derive dari `ComputePL`** (tag project) — hapus subquery
  `led` di `dashboard.go:331-341`.
- `unit_profits`: derive dari ledger per-unit (journal_lines ber-tag unit) ATAU tandai eksplisit
  "snapshot BAST" yang direkonsiliasi ke ledger; JANGAN sajikan bersama angka ledger tanpa label.
  Idealnya: satu `UnitPnL(unitID)` dari ledger. Ini juga menutup drift pasca true-up (F7).

### R-4 · Outstanding & collected lewat domain `sale` saja (F1–F3)
- `reporting` memanggil `sale.Service` (atau read-model bersama) untuk outstanding/total_paid/
  contract_value — hapus SQL di `dashboard.go:355,361,509`, `kpr_pipeline.go`, `sales_performance.go:70,83`.
- Setelah **R4 booking-fee** (flag `counts_toward_price`), `SumTerminsByUnit` jadi satu definisi
  "paid" untuk SEMUA (hapus varian booking_fee-excluded/all-sources yang bercabang). Hapus filter
  `scheme_state` ad-hoc atau pindahkan ke domain sebagai parameter resmi.

### R-5 · Biaya aktual satu definisi (F6)
Pilih **ledger sebagai SoT** (konsisten prinsip): satu fungsi `cost.ActualByProject(role=inventory,
netting=posted−reversal)` dipakai budget-realisasi DAN dashboard. Putuskan nasib `cost_entries.amount`
(jadikan cache turunan atau hapus dari perhitungan). Samakan cakupan kode via R-1.

### R-6 · AR aging & arus kas satu engine (F8, F9)
Dashboard forecast/top-debtor **memanggil `BuildARAging`**; cash-flow MTD/seri **memanggil
`GetCashMovements`** (via registry akun R-1). Hapus SQL duplikat `dashboard.go:525-556,216,388`.

### R-7 · Pajak satu pembaca (F10)
`reporting.GetTaxLiabilityReport` **memanggil `tax.GetTaxReport`** — hapus reimplementasi
`repository.go:60-111`. Putuskan filosofi: `tax_obligations` sebagai sub-ledger resmi (rekonsiliasi
ke `2-4000`) ATAU derive dari ledger; satukan PPh & PPN.

### R-8 · Pisahkan dua arti buyer_credit (F11)
Field berbeda: `buyer_credit_balance` (akumulasi) vs `overpayment_unapplied` (per-transaksi). Satu
key = satu makna.

### R-9 · Frontend nol-recompute (F13)
Tambah field agregat di response backend (total_debit di journal detail, `vat_amount`/`hpp_total`
di SaleRecord, `conversion_pct` di funnel, `usage_pct`+status di RAB, `burn`/`runway` di dashboard,
roll-up alokasi, total invoice belum-bayar). Frontend hanya menampilkan — nol aritmetika bisnis.

### Urutan yang disarankan
R-1 (fondasi) → R-4 (butuh R4 booking-fee dulu) → R-2 → R-3 → R-5 → R-6 → R-7 → R-8 → R-9.
Setiap langkah tambahkan **integration test kesetaraan**: mis. `dashboard.outstanding ==
Σ ContractFinancialSummary.Outstanding`; `dashboard.cash == TrialBalance(cash)`;
`dashboard.projectMargin == ProjectPL.margin`. Test ini mengunci SSOT secara permanen.

---

## Catatan
- **Tidak ada kode diubah** oleh audit ini.
- Beberapa temuan bertautan dengan **R4 (booking fee di luar harga)** — F2 khususnya. Menyelesaikan
  R4 lebih dulu menyederhanakan R-4 karena "paid" jadi satu definisi.
- Prioritas dampak-bisnis: **F5 (revenue/HPP/margin) & F6 (biaya aktual)** paling berbahaya (angka
  laba & kendali biaya bisa berbeda antar layar); **F4 (date bound)** halus tapi bikin dashboard ≠
  neraca; **F2/F1** sudah diketahui via R4.
