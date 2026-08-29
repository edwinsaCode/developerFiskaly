# Product Sprint 9 — R5 Executive Dashboard 2.0 — SHIPPED

> Status: **SHIPPED + smoke test Chrome.** 2026-07-22.
> Dashboard menjawab pertanyaan owner — semua angka derived dari satu engine, read-only.

## Backend (`reporting/dashboard.go`, semua additive)

- **`receivables`**: `outstanding_total` (rumus CERMIN ContractFinancialSummary:
  gross − Σ termin unit, kontrak aktif, hanya positif — satu definisi),
  `forecast_30/60/90` (Σ jadwal belum lunas jatuh tempo dalam horizon, kumulatif),
  `top_debtors` (top 5 overdue: buyer, unit, nominal, jumlah cicilan).
- **`unit_profits`**: profit per unit BAST dari `sale_records`
  (sale_price − Σ HPP komponen; margin desc; top 10).
- **`kpr.stuck_count`**: kontrak >30 hari diam di state proses
  (pengajuan/SP3K/akad) — dihitung dari `state_age_days` pipeline R1.

## Frontend (`DashboardV2`)

- **KPI baris 1**: kartu "Outstanding Kontrak" (angka bisnis yang owner tanya jam 8
  pagi; ledger 1-2xxx tetap di laporan).
- **Attention strip** diperluas: "KPR macet >30 hari di satu tahap" → `/penjualan/kpr`,
  "proyek biaya mendahului fisik" (count `cost_ahead_warning` R3) → `/proyek`.
- **Seksi KPR** ("Bagaimana posisi KPR?"): pipeline per tahap (6 kotak klik → board),
  Dana Bank (Σ cair hijau / outstanding merah), banner macet, link board.
- **Seksi Profit per Unit** ("Berapa profit per unit yang sudah BAST?"): tabel
  pendapatan/HPP/margin/margin% per unit, link ke unit.
- **Forecast Kas Masuk** 30/60/90 + **Top Penunggak** (link ke statement kontrak)
  di seksi penagihan.
- **Sales Funnel**: conversion rate % antar stage (angka di samping count).

## Verifikasi

Payload live: outstanding 25jt (= kontrak 386) ✓ · unit_profits A-01 margin 40% ✓ ·
stuck 0 ✓. Visual: semua seksi render, forecast empty state jelas ("belum ada jadwal").
`go test` hijau · `tsc` hijau.

## Backlog R5

| # | Item |
|---|---|
| R5-B1 | Margin waterfall (Pendapatan→−HPP→−Beban→Margin) dari income statement |
| R5-B2 | Lazy-load per seksi bila latency komposit membengkak (ukur dulu) |
| R5-B3 | Ambang "macet" 30 hari → konfigurasi tenant |
