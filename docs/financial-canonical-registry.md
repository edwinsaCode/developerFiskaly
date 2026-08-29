# Financial Canonical Registry — Konstitusi Angka Keuangan esaProperti

> 2026-07-27 · **DOKUMEN RESMI (Phase 0)** — satu-satunya rujukan definisi angka keuangan.
> Basis bukti: `docs/financial-consistency-audit-2026-07.md` (file:line per 2026-07-26).
> Aturan tertinggi: **setiap angka keuangan hanya boleh punya SATU implementasi bisnis.**
> Perubahan pada dokumen ini = perubahan konstitusi → wajib approval Product Owner.
>
> ## ✅ STATUS 2026-07-29 — PHASE 2 SELESAI (S1–S9 + Zero-Divergence Audit)
> Seluruh entri ⚠️/🔴 di bawah sudah DIHAPUS/DIALIHKAN ke kanonik. Suite
> kesetaraan: **21 PASS, 0 skip, 0 knownDivergence — strict == normal**.
> Keputusan PO FINAL (2026-07-29):
> 1. **Revenue** = SEMUA akun 4-xxxx (ikut Laba Rugi; 4-2000 termasuk).
>    Pemisahan "Pendapatan Lain" hanya presentasi. Margin = laba P&L
>    (pendapatan − beban) di SEMUA layar.
> 2. **Actual Cost** = ledger, netting `debit(non-reversal) − kredit(reversal)`
>    (`ledger.ActualCostByCode` — SATU ekspresi). Relief HPP BAST TIDAK
>    mengurangi. Dipakai dashboard, RAB-vs-Realisasi, cost, allocation, closing.
> 3. **Booking fee** — DIREVISI RULE KLIEN 2026-07-29 (menggantikan R4 Opsi A):
>    fee = **PENDAPATAN BOOKING** saat diterima (Dr Kas/Bank / Cr `4-2100`,
>    migration 000053; disposisi `recognized` — FINAL). Bukan liability/deposit/
>    DP/buyer credit; tidak pernah mengurangi harga/outstanding (flag
>    `counts_toward_price=FALSE`, migration 000052 tetap fondasi); batal =
>    TANPA refund/reversal; konversi = TANPA reklas; tampil di Laba Rugi
>    sebagai akun tersendiri; statement kontrak = murni pembayaran rumah.
>    2-2100 + disposisi manual + refund booking = JALUR LEGACY baris histori.
>    Rekonsiliasi: saldo 4-2100 == Σ fee `recognized` (EQ_BookingRevenue);
>    saldo 2-2100 == Σ fee `held` legacy (EQ_BookingLiability).
> Laporan lengkap: `docs/ssot-phase2-final-report-2026-07.md` +
> `docs/booking-revenue-audit-plan-2026-07.md`.

Legenda status per implementasi:
- ✅ **KANONIK** — satu-satunya implementasi yang sah.
- 🟢 **AMAN** — konsumen yang benar (membaca kanonik, tidak menghitung ulang).
- ⚠️ **DUPLIKAT — HARUS DIHAPUS** — implementasi kedua; dihapus/dialihkan pada langkah R-x.
- 🔴 **INKONSISTEN** — duplikat yang definisinya SUDAH menyimpang (bukan sekadar redundan).

Dua fondasi baru yang disahkan registry ini (belum ada di kode, dibangun di Phase 2):
- **`AccountRoleRegistry`** (R-1): satu peta peran akun → himpunan kode
  (`cash_bank`, `receivable`, `payable`, `inventory`, `revenue`, `cogs`, `booking_liability`,
  `refund_liability`, `commission_liability`, `tax_liability`, `vat_in/out`). Melarang
  hardcode kode/kategori/prefix tersebar.
- **`LedgerBalanceService`** (R-2): satu-satunya pembaca saldo posted
  (`BalanceOf(role|codes, asOf)`), dibangun di atas `ComputeTrialBalance`.

---

## 1 · Cash (Kas)  &  2 · Bank

**Kanonik:** `LedgerBalanceService.BalanceOf(role=cash_bank, asOf)` ← `ledger.ComputeTrialBalance`
(`internal/ledger/query.go:45`, `TrialBalance` `:322` — posted-only, `date <= asOf`).
Keanggotaan akun dari `AccountRoleRegistry` (COA category `cash`/`bank`).

| Implementasi | Lokasi | Status |
|---|---|---|
| `ComputeTrialBalance`/`TrialBalance` | `ledger/query.go:45,322` | ✅ KANONIK (mesin) |
| Neraca `ComputeNeraca` | `reporting/service.go:83` | 🟢 AMAN (baca TrialBalance) |
| Tutup buku `planClosing` | `ledger/closing.go:104,205` | 🟢 AMAN (baca TrialBalance) |
| Dashboard KPI `cash` | `reporting/dashboard.go:186` | 🟢 **S2 DONE: via LedgerBalanceService** (asOf nil = perilaku existing) |
| Dashboard CashIn/Out MTD + seri 6 bulan | `dashboard.go:216,388` | ⚠️ DUPLIKAT (category-based) → R-2/R-6 |
| Arus kas `GetCashMovements` | `reporting/repository.go:252` | 🔴 INKONSISTEN (**kode bank hardcoded** `1-1100..1-1500` ≠ category) → R-1/R-6 |
| Validasi akun bayar `ListCashBankAccounts` | `ledger/query.go:252` | 🟢 AMAN (COA-driven; jadi sumber `AccountRoleRegistry`) |

**Aturan:** tidak boleh ada `SUM(journal_lines...)` manual untuk kas di luar `LedgerBalanceService`.
Tidak boleh ada daftar kode bank hardcoded.

---

## 3 · AR — Piutang (saldo ledger)

**Kanonik:** `LedgerBalanceService.BalanceOf(role=receivable, asOf)` — peran `receivable` =
`1-2000, 1-2100, 1-2200` via registry (bukan hardcode).

| Implementasi | Lokasi | Status |
|---|---|---|
| TrialBalance → Neraca (folded Aset) | `ledger/query.go` → `reporting/service.go:89` | ✅/🟢 |
| Dashboard KPI `receivable` | `dashboard.go:189` | 🟢 **S2 DONE: via LedgerBalanceService(RoleReceivable)** |

**Catatan pemisahan konsep:** *AR-ledger* (saldo akun piutang, lahir pasca-BAST) ≠ *Outstanding
kontrak* (dokumen; entri #7). Keduanya angka SAH yang berbeda; dashboard wajib MELABELI mana
yang ditampilkan, tidak boleh mencampur.

---

## 4 · AP — Hutang Usaha

**Kanonik:** `LedgerBalanceService.BalanceOf(role=payable, asOf)` (peran = `2-1000`; registry).

| Implementasi | Lokasi | Status |
|---|---|---|
| TrialBalance → Neraca (folded Kewajiban) | `reporting/service.go:97` | ✅/🟢 |
| Dashboard KPI `payable` | `dashboard.go:190` | 🟢 **S2 DONE: via LedgerBalanceService(RolePayable)** |
| Kredit biaya `resolveDebitCreditCodes` | `cost/service.go:139` | 🟢 AMAN (menulis jurnal, bukan membaca saldo) |

---

## 5 · Booking Liability (Titipan Booking 2-2100)

**Kanonik saldo:** `LedgerBalanceService.BalanceOf(role=booking_liability)` (= 2-2100).
**Kanonik dokumen (fee held per booking):** `bookings.fee_disposition='held'` — sub-ledger
operasional; WAJIB rekonsiliasi Σfee(held) == saldo 2-2100.

| Implementasi | Lokasi | Status |
|---|---|---|
| TrialBalance 2-2100 | ledger | ✅ |
| Dashboard KPI `booking_deposits` | `dashboard.go:191` | 🟢 **S2 DONE: via LedgerBalanceService** |
| Dashboard `BookingFeeHeld` Σ`bookings.booking_fee` aktif | `dashboard.go:255` | 🟢 AMAN **sebagai angka dokumen** — wajib diberi label berbeda dari saldo ledger + test rekonsiliasi |
| Posting fee/reklas/forfeit/refund | `sale/booking_repo.go`, `cancellation/refund_service.go` | 🟢 AMAN (penulis jurnal) |

---

## 6 · Buyer Credit (Saldo Kredit Buyer)

**Kanonik:** `creditBalance` — Σ `payment_allocations(type=buyer_credit)` − Σ `credit_applications`
(`sale/credit_repository.go:20`, exposed `GetBuyerCredit`).

| Implementasi | Lokasi | Status |
|---|---|---|
| `creditBalance` | `credit_repository.go:20` | ✅ KANONIK |
| Sisa overpay per-transaksi `planAllocation` | `sale/collection.go:524` | 🔴 INKONSISTEN — angka BERBEDA disajikan di key JSON yang SAMA `buyer_credit` (`collection.go:171,401`) → R-8: ganti nama field `overpayment_unapplied` |

**Aturan:** satu key = satu makna. `buyer_credit` HANYA untuk saldo terakumulasi kanonik.

---

## 7 · Outstanding (sisa tagihan kontrak)  &  8 · Total Paid

**Kanonik:** `sale.ContractFinancialSummary` (`sale/financial_summary.go`, definisi LOCKED di header)
← primitif `SumTerminsByUnit` (`sale/repository.go:138`).
`Outstanding = GrossAmount − TotalPaid`; `TotalPaid = Σ termin_payments unit`.
(Pasca-R4 booking-fee: `TotalPaid = Σ termin WHERE counts_toward_price=TRUE` — tetap SATU definisi.)

| Implementasi | Lokasi | Status |
|---|---|---|
| `summarizeContract` / `SumTerminsByUnit` | `financial_summary.go:69` / `repository.go:138` | ✅ KANONIK |
| `outstandingForContract` (collection preview/guard/result) | `collection.go:110` | 🟢 AMAN (delegasi primitif) — konsolidasikan agar memanggil summary |
| Statement `TotalPaid/RemainingBalance` | `statement.go:114,217` | 🟢 AMAN (delegasi primitif) |
| Guard & reklas pasca-BAST | `scheme_flow.go:415`, `service.go:277` | 🟢 AMAN (basis gross sale_record — didokumentasikan) |
| Cek lunas milestone | `scheme_flow.go:707` | 🟢 AMAN |
| Invoice KEKURANGAN | `billing/service.go` (via `ContractSummaryProvider`) | 🟢 AMAN (contoh pola benar) |
| Kwitansi (ringkasan) | `billing/receipt_service.go` (via provider) | 🟢 AMAN |
| Dashboard `receivables.outstanding_total` | `dashboard.go:509` | 🟢 **S3 DONE: via sale.PortfolioOutstanding** (ContractFinancialSummary) |
| KPR pipeline `outstanding`/`total_paid` | `kpr_pipeline.go:96,119` | 🟢 **S3 DONE: via sale.ContractOutstandingPaid** |
| Dashboard `collected` per proyek | `dashboard.go:361` | 🔴 INKONSISTEN (**exclude booking_fee** ≠ kanonik) → R-4 |
| Sales-perf `collected` | `sales_performance.go:83` | 🔴 INKONSISTEN (exclude booking_fee) → R-4 |
| `TotalAdvance` pipeline stats | `reporting/repository.go:151` | ⚠️ DUPLIKAT → R-4 |
| FE proyeksi outstanding | `DisbursementModal.tsx:79` | ⚠️ baseline duplikat → R-9 (preview boleh, baseline wajib pakai `summary.outstanding`) |

**Aturan:** dashboard, collection, statement, invoice, KPR, sales-performance **wajib** membaca
`ContractFinancialSummary` (atau read-model yang dibangun darinya). Tidak boleh ada
`gross_amount − SUM(termin)` di SQL mana pun.

---

## 9 · Revenue (Pendapatan)

**Kanonik:** `reporting.ComputePL` ← `queryPLRows` (`reporting/service.go:140`,
`repository.go:36`) — ledger posted, akun peran `revenue` dari registry.
**Keputusan definisi (PO, Phase 2):** status `4-2000 Pendapatan Lain-lain` — masuk "Pendapatan
Operasional" atau baris "Pendapatan Lain" terpisah. Satu keputusan, semua layar mengikuti.

| Implementasi | Lokasi | Status |
|---|---|---|
| `ComputePL` (P&L, project-PL, neraca laba berjalan) | `service.go:140,114` | ✅ KANONIK |
| Dashboard SalesMTD | `dashboard.go:212` (semua `4-%`) | ⚠️ DUPLIKAT → R-3 |
| Dashboard revenue per proyek | `dashboard.go:334` (**exclude `4-2000`**) | 🔴 INKONSISTEN → R-3 |
| `unit_profits.revenue` = `sale_records.sale_price` | `dashboard.go:582` | 🔴 INKONSISTEN (tabel dokumen, bukan ledger) → R-3 |
| Sales-perf `bast_value`; `SoldContractSum` | `sales_performance.go:76`; `repository.go:162` | ⚠️ DUPLIKAT (sale_records) — sah HANYA bila dilabeli "nilai BAST (dokumen)", bukan "revenue" |

**Aturan:** tidak boleh ada `SUM(4-...)` di SQL reporting di luar `queryPLRows`.

---

## 10 · COGS / HPP

**Kanonik pengakuan:** jurnal COGS `5-1000` yang diposting saat BAST/true-up — nilainya berasal
dari **mesin alokasi** (`allocation/engine.go`) via resolver (`sale/hpp_resolver.go`), dibekukan di
`allocation_snapshots` + `sale_records.hpp_*`, di-true-up oleh `closing/service.go:185`
(`hpp_trueup_lines`, INV-COGS-SUM: Σ jurnal COGS unit == HPP diakui unit).
**Kanonik baca agregat:** `ComputePL` (beban `5-*` per peran registry).
**Kanonik baca per unit:** `LedgerBalanceService`/query ledger `5-1000` ber-tag unit (== snapshot + true-up, dijaga INV-COGS-SUM).

| Implementasi | Lokasi | Status |
|---|---|---|
| Mesin alokasi + resolver + snapshot + true-up | `allocation/`, `sale/hpp_*`, `closing/` | ✅ KANONIK (penulis) |
| `ComputePL` beban | `service.go:140` | ✅ KANONIK (pembaca agregat) |
| Dashboard HPP MTD & per proyek (`5-1000` saja) | `dashboard.go:212,335` | ⚠️ DUPLIKAT → R-3 |
| `unit_profits.hpp` = Σ `sr.hpp_*` | `dashboard.go:582` | 🔴 INKONSISTEN (beku; menyimpang pasca true-up dari ledger) → R-3 |
| FE `SaleRecordCard` Σ hpp (BigInt truncate) | `SaleRecordCard.tsx:16` | ⚠️ HARUS DIHAPUS → R-9 (backend kirim `hpp_total`) |

---

## 11 · Margin

**Kanonik:** turunan `ComputePL`: `Laba = Pendapatan − Beban`. Margin% = Laba/Pendapatan (satu
formula di satu tempat, disajikan backend).

| Implementasi | Lokasi | Status |
|---|---|---|
| `ComputePL` laba | `service.go:140` | ✅ KANONIK |
| Dashboard MarginMTD; margin per proyek | `dashboard.go:227,313` | 🔴 INKONSISTEN (revenue&HPP beda definisi → margin beda) → R-3 |
| `unit_profits.margin/margin_pct` | `dashboard.go:583` | 🔴 INKONSISTEN → R-3 |

---

## 12 · Budget (RAB)

**Kanonik:** Σ `budget_items.budgeted_amount` plan **active** — `budget.SumItemsByCategory`
(`budget/repository.go:184`) / `GetRABvsRealisasi.TotalBudgeted` (`budget/service.go:343`).
Invariant #8: satu plan active per proyek/fase.

| Implementasi | Lokasi | Status |
|---|---|---|
| `SumItemsByCategory` + service | `budget/repository.go:184` | ✅ KANONIK |
| Dashboard `budget_total` | `dashboard.go:326` | ⚠️ DUPLIKAT (SQL sama diimplementasi ulang) → R-5 |
| FE RAB usage% + status sehat/waspada | `charts.tsx:504` | ⚠️ HARUS DIHAPUS → R-9 (backend kirim `usage_pct`+status) |

---

## 13 · Actual Cost (Biaya Aktual / Realisasi RAB)

**Kanonik (keputusan registry):** **LEDGER** — satu fungsi
`cost.ActualByProject(role=inventory, netting = debit − kredit-reversal, posted-only)`
(disempurnakan dari `cost/repository.go:98` + aturan netting `closing/service.go:150`;
kode akun dari registry, bukan `LIKE '1-3%'` / daftar hardcoded).
`cost_entries.amount` = **dokumen sumber** (audit trail), BUKAN basis agregat keuangan.

| Implementasi | Lokasi | Status |
|---|---|---|
| `cost.AccumulatedByProject` (debit-only, 4 kode) | `cost/repository.go:98` | ✅ basis KANONIK (＋samakan netting) |
| `closing.actualCapitalizedCost` (debit − kredit reversal) | `closing/service.go:150` | ✅ aturan netting KANONIK — konsolidasi dgn atas → R-5 |
| Budget realisasi dari `cost_entries.amount` | `budget/repository.go:271,305` | 🔴 INKONSISTEN (tabel dokumen; tanpa netting; scope kategori beda) → R-5: alihkan ke ledger |
| Dashboard `actual_cost` (`LIKE '1-3%'`, net semua kredit) | `dashboard.go:332` | 🔴 INKONSISTEN (prefix + netting beda) → R-5 |
| Alokasi `queryCosts` (debit-only, 4 kode) | `allocation/repository.go:109` | 🟢 AMAN — **S1 DONE: kode via `ledger.RoleCodeList(RoleInventory)`** |
| `cost.accumulatedQuery` (debit-only, 4 kode) | `cost/repository.go` | 🟢 AMAN — **S1 DONE: kode via registry** (dulu kembar dgn allocation) |

**Empat definisi hari ini → SATU:** ledger, kode dari registry, netting kanonik (debit −
kredit-reversal), kategori expense (marketing/other) dilaporkan TERPISAH dari kapitalisasi.

---

## 14 · PPh Final

**Kanonik hitung:** `tax.AccrueTax` + `TaxFormula` (`tax/service.go:132`, `formula.go:50`) —
satu-satunya penghitung; hasil = `tax_obligations` (sub-ledger resmi) + jurnal `5-2000/2-4000`.
**Kanonik laporan:** `tax.GetTaxReport` (`tax/service.go:488`).
**Invariant rekonsiliasi:** Σ obligations outstanding == saldo `2-4000` (ledger).

| Implementasi | Lokasi | Status |
|---|---|---|
| `AccrueTax`/`GetTaxReport` | `tax/service.go` | ✅ KANONIK |
| `reporting.GetTaxLiabilityReport` | `reporting/repository.go:60` | ⚠️ DUPLIKAT (agregasi ulang tabel yang sama) → R-7: panggil `tax.GetTaxReport` |

## 15 · PPN

**Kanonik:** `tax.GetVATReport` (`tax/service.go:436`) — dari ledger `2-3000`/`1-5100`
(via `LedgerBalanceService` pasca R-2); PPN output dihitung sekali di BAST (`sale/service.go:407`).

| Implementasi | Lokasi | Status |
|---|---|---|
| `GetVATReport` | `tax/service.go:436` | ✅ KANONIK |
| FE `SaleRecordCard` PPN = price×rate | `SaleRecordCard.tsx:42` | ⚠️ HARUS DIHAPUS → R-9 (backend kirim `vat_amount`) |

---

## 16 · Commission ✅ (satu-satunya yang sudah bersih — jadikan teladan)

**Kanonik:** `commission.Rule.AmountFor` (`commission/model.go:101`) → disimpan
`commissions.amount`; SEMUA konsumen (jurnal akrual/bayar/clawback, `sales_performance.go:90`,
dashboard) membaca nilai tersimpan.
**Invariant rekonsiliasi:** Σ akrual − Σ bayar − Σ clawback == saldo `2-6200`.

| Implementasi | Lokasi | Status |
|---|---|---|
| `AmountFor` + stored amount | `commission/` | ✅ KANONIK |
| Dashboard `commission_payable` (ledger 2-6200) | `dashboard.go:192` | ⚠️ raw SQL → R-2 (nilai benar, jalur salah) |

---

## 17 · Collection (rate & agregat penagihan)

**Dua metrik SAH yang berbeda — wajib label berbeda, masing-masing SATU implementasi:**
- **Collection-vs-kontrak** (kas diterima / nilai kontrak): kanonik = turunan
  `ContractFinancialSummary` (Σ TotalPaid / Σ NetContract). Duplikat: `dashboard.go:361,320`
  (⚠️🔴 exclude booking_fee) → R-4.
- **Collection-vs-jadwal** (tertagih / jatuh tempo): kanonik = `BuildARAging.CollectionRate`
  (`reporting/ar_aging.go:171`). 🟢 AMAN.

## 18 · KPR (dana cair, outstanding KPR)

**Kanonik:** dana cair = Σ termin `source='kpr_disbursement'` (sub-agregat primitif kanonik #8,
satu-satunya definisi); outstanding KPR = `ContractFinancialSummary.Outstanding` kontrak KPR (#7).

| Implementasi | Lokasi | Status |
|---|---|---|
| `kpr_pipeline.go` disbursed/total_paid/outstanding | `kpr_pipeline.go:96,119` | ⚠️ DUPLIKAT raw SQL → R-4 (pindahkan ke read-model di atas primitif sale) |
| Dashboard seksi KPR | `dashboard.go:441` | 🟢 AMAN (delegasi kpr querier — ikut benar setelah R-4) |

## 19 · Forecast Cash (30/60/90)

**Kanonik:** `BuildARAging.Expected30/60/90` (`reporting/ar_aging.go:132`) dari
`payment_schedules (amount − paid_amount)`, filter status kanonik.

| Implementasi | Lokasi | Status |
|---|---|---|
| `BuildARAging` | `ar_aging.go:56` | ✅ KANONIK |
| Dashboard forecast + top debtors | `dashboard.go:525,544` | ⚠️🔴 DUPLIKAT (filter status beda) → R-6 |
| FE burn-rate/runway | `charts.tsx:547` | ⚠️ HARUS DIHAPUS → R-9 (backend kirim `burn`,`runway`) |

## 20 · Dashboard KPI (komposit)

**Aturan konstitusi:** dashboard TIDAK memiliki angka sendiri. Setiap field payload wajib
menunjuk implementasi kanonik di atas:
`cash/receivable/payable/booking/commission` → #1–#5 (R-2) · `sales_mtd/margin/hpp` → #9–#11 (R-3)
· `outstanding_total/collected/kpr` → #7/#8/#18 (R-4) · `budget/actual_cost/progress` → #12/#13 (R-5)
· `forecast/top_debtors` → #19 (R-6) · `booking_fee_held` → #5 dokumen (＋rekonsiliasi)
· `unit_profits` → #9–#11 per-unit dari ledger (R-3). FE murni display (R-9).

---

## 21 · Titipan Notaris (UAT Batch 2 §3 — 2026-07-30)

**Kanonik saldo:** `LedgerBalanceService`/registry peran `notary_liability` (= `2-2300`).
**Kanonik dokumen:** `notary_deposits` (sub-ledger; SATU penulis `notary.Service`:
terima Dr Kas/Cr 2-2300; payout Dr 2-2300/Cr Kas — TIDAK PERNAH menyentuh 4-xxxx).
**Rekonsiliasi (dikunci suite):** saldo 2-2300 == Σ deposit `held` (EQ_NotaryDeposit);
Σ debit kas == Σ termin + Σ titipan notaris (EQ_Payments_vs_LedgerCashIn).

## 22 · Product Catalog & akun pendapatan per produk (UAT Batch 2 §2)

**Kanonik mapping:** master `product_types` per tenant (`revenue_account_code`,
COA-driven, editable) — jurnal BAST me-resolve akun pendapatan via
`project.RevenueAccountForUnitType` (seam `sale.ProductAccountResolver`);
unit legacy tanpa entri master → fallback `4-1000`. P&L tetap konsisten apa pun
mappingnya (revenue kanonik = semua 4-%). Peran registry per-akun: `unit_sales_revenue`
(4-1000) utk laporan per-unit; `booking_revenue` (4-2100).

## 23 · Kwitansi (dokumen — bukan angka keuangan)

Tipe kwitansi DERIVED dari `termin.payment_source` (SoT tunggal): `booking_fee` →
tipe `booking`, nomor **KWB**/{yyyy}/{seq}; lainnya → `house_payment`, **KWT**.
Sequence per (tenant, doc_type). Dikunci DOC_BookingReceipt_Separate di suite.

## Aturan penegakan (berlaku sejak dokumen ini disahkan)

1. **Dilarang** menambah SQL/kode yang menghitung angka keuangan di luar kolom "KANONIK".
   PR yang melakukannya ditolak dengan rujukan registry ini.
2. Setiap entri kanonik dijaga **test kesetaraan Phase 1**
   (`internal/reporting/consistency_integration_test.go`) — selisih Rp1 = gagal.
3. Perubahan definisi (mis. status 4-2000, kebijakan booking fee R4) diputuskan di registry
   DULU, baru kode.
4. Frontend: nol aritmetika bisnis; semua agregat/persentase dikirim backend (R-9).
