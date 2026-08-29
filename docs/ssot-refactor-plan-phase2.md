# Phase 2 — Rencana Refactor SSOT (R-1..R-9)

> 2026-07-27 · disetujui PO · **STATUS 2026-07-29: SELESAI SEMUA (S1–S9)** —
> termasuk R4 booking-fee (keputusan PO final) + Zero-Divergence Audit.
> Suite akhir: 21 PASS, 0 skip, 0 knownDivergence (strict == normal).
> Laporan: `docs/ssot-phase2-final-report-2026-07.md`.
> Prasyarat TERPENUHI: Phase 0 (`financial-canonical-registry.md`) ✅ ·
> Phase 1 suite HIJAU (15 PASS, 3 KNOWN-DIVERGENCE terbukti merah di strict, 1 unscoped) ✅.

## Protokol per langkah (berlaku untuk SEMUA refactor)

1. Satu refactor = satu langkah = satu PR. Tidak dicampur.
2. Sebelum mulai: `make test-consistency` HIJAU (baseline).
3. Selesai: `make test-consistency` tetap HIJAU + `go test ./...` hijau +
   **tidak ada perubahan angka/jurnal/saldo/histori** (refactor pembaca — penulis jurnal tak disentuh).
4. Bila langkahnya menyatukan definisi yang tadinya menyimpang: **hapus pembungkus
   `knownDivergence()`** pada test terkait → kesetaraan ditegakkan permanen.
5. Bila selama implementasi ditemukan implementasi baru yang menghitung ulang angka
   keuangan: **BERHENTI, laporkan dulu** (aturan PO).
6. Keputusan definisi diputuskan di registry DULU (butuh approval PO), baru kode.

## Urutan langkah

| # | Langkah | Isi ringkas | Test yang mengunci | Keputusan PO dibutuhkan? |
|---|---|---|---|---|
| S1 | **R-1 AccountRoleRegistry** | Satu peta peran akun→kode (cash_bank, receivable, inventory, revenue, …); ganti semua hardcode (`1-1100..`, `LIKE '1-3%'`, `IN ('1-2000'…)`) menjadi pembacaan registry. Murni penggantian sumber konstanta — nol perubahan angka. | Seluruh suite (tetap hijau) | Tidak |
| S2 | **R-2 LedgerBalanceService** | `BalanceOf(role, asOf)` di atas `ComputeTrialBalance`; dashboard KPI Baris-1 + CashIn/Out + seri 6-bulan memanggil ini; hapus SQL `dashboard.go:186-231,388`. Menutup drift date-bound. | EQ_Cash / EQ_AR / EQ_AP / EQ_BookingLiability / EQ_CommissionPayable | Tidak |
| S3 | **R-4 Outstanding/Paid via domain sale** | Dashboard receivables, KPR pipeline, sales-perf collected, TotalAdvance → read-model dari `ContractFinancialSummary`/`SumTerminsByUnit`; hapus semua `gross − SUM(termin)` SQL. **Prasyarat: keputusan R4 booking-fee** (flag `counts_toward_price`) agar "paid" satu definisi. | EQ_Outstanding_* / EQ_TotalPaid_* + test baru collected | **Ya** — approve design R4 booking-fee dulu (docs/r4-booking-fee-outside-price-design.md) |
| S4 | **R-3 Revenue/HPP/Margin via ComputePL** | Dashboard MTD & per-proyek derive dari `ComputePL`; `unit_profits` dari ledger per-unit; hapus subquery `led`. | EQ_Revenue/EQ_Margin (hapus knownDivergence F5) + EQ_HPP | **Ya** — status 4-2000: operasional vs "Pendapatan Lain" (registry #9) |
| S5 | **R-5 Actual cost satu definisi** | `cost.ActualByProject` (ledger, netting debit−kredit-reversal, kode dari registry) dipakai budget-realisasi + dashboard; `cost_entries.amount` berhenti jadi basis agregat. | EQ_ActualCost (hapus knownDivergence F6) + test baru budget-realisasi | **Ya** — konfirmasi netting kanonik & pemisahan expense (marketing/other) |
| S6 | **R-6 AR/forecast + arus kas satu engine** | Dashboard forecast/top-debtor → `BuildARAging`; cash-flow → `GetCashMovements` via registry. | EQ_Forecast + EQ_Cash | Tidak |
| S7 | **R-7 Pajak satu pembaca** | `reporting.GetTaxLiabilityReport` → panggil `tax.GetTaxReport`; tambah rekonsiliasi obligations ↔ 2-4000; wire tax ke suite (buka EQ_TaxLiability). | EQ_TaxLiability (unskip) | Tidak |
| S8 | **R-8 Pisah makna buyer_credit** | Field `overpayment_unapplied` utk sisa per-transaksi; `buyer_credit` hanya saldo kanonik. API additive + FE ikut. | EQ_BuyerCredit diperluas | Tidak |
| S9 | **R-9 FE nol-recompute** | Backend kirim agregat (hpp_total, vat_amount, usage_pct+status, burn/runway, conversion_pct, total invoice unpaid, roll-up alokasi, total jurnal detail); FE hapus 10 titik recompute. | tsc + smoke visual (angka FE == payload) | Tidak |

Estimasi: S1–S2 kecil (fondasi), S3–S5 sedang (perubahan definisi ber-gate PO), S6–S9 kecil.

## Tiga keputusan PO yang memblok (bisa diputuskan sekarang, sebelum S3–S5)

1. **R4 booking fee** — approve design `r4-booking-fee-outside-price-design.md` (rekomendasi: Core + Opsi A).
2. **Status 4-2000** — masuk pendapatan operasional ATAU baris "Pendapatan Lain" terpisah (mempengaruhi dashboard vs P&L; suite membuktikan selisih 10jt).
3. **Netting biaya aktual** — kanonik = debit − kredit-reversal (rekomendasi registry #13), BAST relief TIDAK mengurangi realisasi RAB.
