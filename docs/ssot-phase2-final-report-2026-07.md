# Laporan Akhir — Phase 2 SSOT Refactor (S1–S9) + Zero-Divergence Audit

> 2026-07-29 · **SELESAI**. Melanjutkan S1–S3 (2026-07-27) dengan keputusan PO final:
> (1) Revenue = semua 4-xxxx ikut P&L; (2) Actual Cost = debit − kredit-reversal,
> relief BAST tidak mengurangi; (3) Booking Fee di LUAR harga (R4 Opsi A).
> Hasil: **setiap angka keuangan punya SATU sumber kebenaran**, dikunci suite
> kesetaraan permanen (21 PASS, 0 skip, 0 `knownDivergence()`).

---

## 1 · Hasil verifikasi

| Cek | Hasil |
|---|---|
| `go build ./...` + `go vet ./...` | ✅ bersih |
| `go test ./...` (unit) | ✅ hijau |
| Integration suite penuh (`-tags integration ./...`) | ✅ hijau (semua paket) |
| `make test-consistency` | ✅ **21 PASS, 0 SKIP, 0 FAIL** |
| `make test-consistency-strict` | ✅ **PASS 100%** (identik dgn normal — tak ada mode skip tersisa) |
| `knownDivergence()` | ✅ **DIHAPUS TOTAL** (fungsi + semua pemanggil) |
| Frontend `tsc --noEmit` | ✅ bersih |
| Smoke test API nyata (dashboard, invoices, tax, sale-record, allocation) | ✅ payload baru terverifikasi di server berjalan |

## 2 · Sprint yang diselesaikan sesi ini

### S4 — Revenue/HPP/Margin via ComputePL (keputusan PO #1)
- **Duplicate dihapus:** subquery `led` dashboard (revenue exclude-4-2000, HPP, margin, margin_pct), CASE-WHEN MTD sales/hpp/margin, `unit_profits` dari snapshot `sale_records.sale_price/hpp_*`.
- **Canonical:** `reporting.ComputePL` ← `queryPLRows` (kini mendukung rentang tanggal + grouped per proyek `GetPLRowsAllByProject`); HPP = baris beban ber-peran `RoleCOGS` (registry baru, `5-1000`); `unit_profits` = ledger per-unit (`GetUnitPLRows`), scope dari sale_records.
- **Makna berubah (by design):** dashboard revenue kini TERMASUK 4-2000; margin = laba P&L (pendapatan − SEMUA beban) di MTD & per-proyek.

### S5 — Actual Cost satu definisi (keputusan PO #2)
- **Canonical baru:** `ledger.ActualCostByCode` / `ActualCostTotalsAllProjects` (`ledger/actual_cost.go`) — SATU ekspresi netting: `Σ debit(je.source≠'reversal') − Σ kredit(je.source='reversal')`, posted-only, kode dari registry.
- **Duplicate dihapus/dialihkan:** `cost.accumulatedQuery` (dulu debit-only), `allocation.queryCosts` (debit-only), `closing.actualCapitalizedCost` (SQL netting sendiri), `budget.GetRealisasiByProject` (dulu Σ `cost_entries.amount` — kini LEDGER, incl. beban 5-3000/5-4000 via taxonomy), subquery actual_cost dashboard, subquery `bud` budget dashboard.
- **Canonical budget total:** `budget.TotalActiveBudgetByProject` (registry #12) — dashboard tidak menjumlah `budget_items` sendiri lagi.
- `cost_entries.amount` = dokumen sumber/drill-down per item saja, bukan basis agregat.

### S3-sisa / R4 — Booking Fee di LUAR harga (keputusan PO #3, design Opsi A)
- **Migration `000052_booking_fee_outside_price`:** kolom `termin_payments.counts_toward_price BOOL NOT NULL DEFAULT TRUE` (+index; histori 100% kompatibel, nol backfill) + pelonggaran CHECK `bookings` (converted + held sah).
- **SumTerminsByUnit** (primitif kanonik #8) kini hanya menghitung `counts_toward_price=TRUE` → 5 call-site (summary, collection, statement, scheme gate, BAST advance) benar sekaligus.
- **Booking fee baru** di-insert FALSE; **konversi branch pada flag**: jalur lama (TRUE, in-flight pra-R4) tetap reklas 2-2100→2-2000 + buyer_credit; jalur baru (FALSE) TANPA reklas/buyer_credit — fee menetap di 2-2100.
- **Disposisi Opsi A:** endpoint additive `POST /bookings/{id}/fee-disposition` `{action: forfeit|refund}` (forfeit → Dr 2-2100/Cr 4-2000; refund → pending_refund → jalur Refund existing). Guard ganda `ErrFeeNotDisposable`.
- **Satu definisi "collected":** `sale.PortfolioFinancials` (baris kanonik per kontrak aktif: NetContract/TotalPaid/Outstanding + project & salesperson) — dikonsumsi dashboard per-proyek, sales performance, pipeline TotalAdvance, PortfolioOutstanding. Dashboard/sales-perf TIDAK punya SQL `Σ gross` / `Σ termin` sendiri lagi.
- **BookingFeeHeld (dokumen)** = Σ fee `fee_disposition='held'` — rekonsiliasi ke 2-2100 tetap tegak di bawah R4.
- Statement payload: per-baris `counts_toward_price`; UI menandai "di luar harga", tidak masuk Total Diterima.

### S6 — AR/forecast + cashflow satu engine
- `GetCashMovements`: kode bank hardcoded `1-1100..1-1500` → **COA category cash|bank** (otoritas klasifikasi tunggal, registry #1).
- **`GetMonthlyCashFlow`** (kanonik): dipakai KPI CashIn/Out MTD DAN seri chart 6 bulan — satu implementasi.
- Dashboard forecast 30/60/90 + top-5 penunggak → **derived dari `BuildARAging`** (registry #19); SQL jadwal duplikat dihapus.
- `GetARScheduleRows` kini mengecualikan `superseded` (satu definisi piutang berjalan).

### S7 — Pajak satu pembaca
- `reporting.GetTaxLiabilityReport` (agregasi ulang tax_obligations) **DIHAPUS** → `reporting.Service.GetTaxLiability` delegasi ke **`tax.GetTaxReport`** (kanonik #14) via `WithTaxReader`.
- Payload + rekonsiliasi additive: `ledger_outstanding` (saldo 2-4000 via trial balance) + `reconciled` (Σ obligations outstanding seluruh histori == saldo ledger).
- Suite: tax service produksi di-wire; skenario meng-akru PPh Final di BAST; **EQ_TaxLiability di-unskip & PASS** (reporting == tax == ledger 2-4000).

### S8 — Pisah makna buyer_credit
- SATU key = SATU makna (registry #6): `buyer_credit` = **saldo kanonik terakumulasi** (`creditBalance`); field baru `overpayment_unapplied` = sisa per-transaksi. Diterapkan di preview, hasil pembayaran, hasil collection, replay idempoten (saldo kanonik, bukan nol semu). FE modal pembayaran ikut.

### S9 — FE nol-recompute (10 titik F13 — semua dihapus)
| # | Titik | Solusi backend |
|---|---|---|
| 1 | JournalDetail Σ debit/kredit | endpoint detail kirim `total_debit`/`total_credit` |
| 2 | SaleRecord PPN | `vat_amount` di payload (rumus PPN tunggal `vatAmountOf` — juga dipakai gross kontrak & BAST) |
| 3 | SaleRecord Σ HPP (BigInt truncate) | `hpp_total` di payload |
| 4 | DisbursementModal proyeksi outstanding | baseline dari `summary.outstanding` kanonik |
| 5 | Total invoice belum-bayar | `GET /invoices` → `{invoices, total_unpaid}` |
| 6 | Roll-up alokasi + share % | compute response → `totals{direct,allocated,total}` + `share_pct`/baris |
| 7 | Conversion % funnel | `booking_conv_pct`/`contract_conv_pct`/`sold_conv_pct` |
| 8 | Total template jurnal berulang | `total` per template |
| 9 | Burn rate & runway | `dashboard.burn{avg_cash_in, avg_cash_out, monthly_burn, runway_months}` |
| 10 | RAB usage % + status | `usage_pct` + `status` (ambang 80/100 di `budget.BudgetHealthStatus` — satu tempat) |
FE tersisa hanya display/format/validasi input (diizinkan audit).

## 3 · Zero-Divergence Audit final (sweep seluruh backend)
Semua `SUM(...)` atas nilai uang diperiksa satu-per-satu. Yang tersisa adalah:
- **Implementasi kanonik itu sendiri** (SumTerminsByUnit, queryPLRows, ActualCostByCode, GetMonthlyCashFlow, creditBalance, SumCredit/Debit tax-rekonsiliasi, SumItemsByCategory/SumActiveBudgetByProject, komisi stored-amount).
- **Angka dokumen berlabel** (bast_value/SoldContractSum = nilai BAST dokumen; list_price pipeline; booking_fee_held dokumen ber-rekonsiliasi; realisasi per-item cost_entries = drill-down non-kanonik).
- **Pembaca ledger satu-situs** (cancellation.receivedAdvance — saldo 2-2000 per unit utk reversal, sesuai design R4 §3).
Sisa duplikat mati (`SUM(amount)` kpr_pipeline yang sudah digantikan S3) dihapus.

## 4 · Perubahan perilaku yang disengaja (bukan bug)
1. Dashboard revenue/margin kini == Laba Rugi (termasuk 4-2000; margin dikurangi SEMUA beban — komisi/marketing/PPh ikut). Angka lama yang berbeda memang salah satu definisi ganda.
2. "Collected" di dashboard & sales performance kini == `TotalPaid` kanonik. Konversi booking jalur LAMA (fee masuk harga) kini terhitung; fee jalur BARU tidak.
3. Booking fee baru tidak lagi mengurangi outstanding; kwitansi fee tetap terbit (pembanding suite difilter flag).
4. Budget realisasi dibaca dari LEDGER — jurnal manual project-tagged kini terhitung (SoT = jurnal).
5. Forecast dashboard mengikuti definisi `BuildARAging` (jendela masa depan; overdue tidak dobel masuk forecast).
6. Test `TestIntegration_Scheme_KPR_FullJourney` diperbaiki: event `disbursed` memang tercatat (audit trail R1) — kegagalan pre-existing sejak 22 Jul, bukan regresi sesi ini.

## 5 · File berubah (ringkas)
**Backend baru:** `ledger/actual_cost.go`, `domain/portfolio.go`, `migrations/000052_booking_fee_outside_price.{up,down}.sql`.
**Backend diubah:** `ledger/{account_role,recurring,handler}.go`, `reporting/{dashboard,repository,service,handler,sales_performance,kpr_pipeline,ar_aging(-)}.go`, `sale/{model,repository,financial_summary,booking_repo,booking_service,booking_handler,handler,statement,collection,payment_allocation,service,errors}.go`, `budget/{model,service,repository,handler}.go`, `cost/repository.go`, `allocation/{repository,handler}.go`, `closing/service.go`, `billing/handler.go`, `tax/{model,service,handler}.go`, `cmd/api/main.go`.
**Test:** consistency suite (21 subtest, R4 + tax + actual-cost assertions baru), budget realisasi integration (seed ledger), booking integration (kebijakan baru + cutover legacy + disposisi), sale unit mocks.
**Frontend:** `lib/types/api.ts`, `lib/api/{billing,allocation,reports}.ts`, `components/accounting/{JournalDetail,RecurringJournalView,CustomerStatementView}.tsx`, `components/penjualan/{SaleRecordCard,DisbursementModal}.tsx`, `components/billing/{AllInvoiceList,RecordPaymentButton}.tsx`, `components/allocation/{AllocationResultTable,AllocationPreview}.tsx`, `components/dashboard/{charts,DashboardV2}.tsx`, `app/(app)/accounting/invoices/page.tsx`, `app/(app)/proyek/[id]/{page,unit/[unitId]/page}.tsx`.

## 6 · Peta kanonik final (SATU sumber per angka)
| Angka | Sumber kanonik |
|---|---|
| Saldo kas/piutang/hutang/titipan/komisi/PPN/PPh | `ledger.LedgerBalanceService` + `AccountRoleRegistry` |
| Revenue / Beban / Margin (semua layar) | `reporting.ComputePL` ← `queryPLRows` |
| HPP (agregat & per unit) | baris `RoleCOGS` dari ComputePL / ledger unit-tagged (INV-COGS-SUM) |
| Outstanding / TotalPaid / Collected / ContractValue | `sale.ContractFinancialSummary` ← `SumTerminsByUnit(counts_toward_price)`; agregat via `sale.PortfolioFinancials` |
| Actual cost / realisasi RAB / akumulasi biaya | `ledger.ActualCostByCode` (netting tunggal) |
| Budget (RAB) | `budget.SumItemsByCategory` / `TotalActiveBudgetByProject`; kesehatan: `BudgetHealthStatus` |
| Forecast/aging/overdue/collection-rate-jadwal | `reporting.BuildARAging` ← `GetARScheduleRows` (non-superseded) |
| Arus kas (laporan + dashboard) | `GetCashMovements` / `GetMonthlyCashFlow` (COA category) |
| Pajak | `tax.GetTaxReport` (+rekonsiliasi 2-4000) — reporting delegasi |
| Buyer credit | `creditBalance` (key `buyer_credit`); sisa transaksi = `overpayment_unapplied` |
| PPN keluaran | `sale.vatAmountOf` (satu rumus) |
| Komisi | `commission.Rule.AmountFor` → stored amount (tetap teladan) |
| Dana cair KPR | Σ termin `source='kpr_disbursement'` (sub-agregat primitif #8) |

**Sistem kini Single Source of Truth untuk seluruh angka keuangan** — dikunci
permanen oleh Financial Consistency Suite (selisih Rp1 = build merah).
