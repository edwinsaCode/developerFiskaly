# Product Sprint 7 — R2 Customer Statement 360 — SHIPPED

> Status: **SHIPPED + smoke test Chrome.** 2026-07-22.
> Satu halaman perjalanan customer lengkap — dokumen yang dipegang developer saat menghadap customer.

## Backend (additive kecil)

`sale/statement.go` — payload `payments[]` + `timeline[]` + `summary` sudah ada sejak R1;
sprint ini menambah konteks pembiayaan ke header: `bank_kpr`, `loan_amount`, `scheme_state`.

## Frontend — `/accounting/receivable/[contractId]`

- **Header**: buyer, NIK, link unit, badge KPR + state scheme (Dana Cair/Lunas/dst),
  Bank KPR + Plafon Kredit (kontrak KPR), diskon (bila ada, dari summary satu-rumus).
- **Riwayat Pembayaran**: setiap penerimaan per sumber (Booking Fee / Pembayaran /
  Cicilan / **Pencairan Bank** di-highlight) + referensi SP2D + tombol Cetak Kwitansi
  per baris (idempoten) + footer Total Diterima.
- **Invoice**: nomor, jenis (badge KEKURANGAN kuning), terbit/jatuh tempo, nominal,
  status, cetak A4 (`PrintInvoiceButton` baru — pola PrintReceiptButton).
- **Perjalanan Kontrak**: timeline vertikal event lifecycle (audit trail event_date).
- Existing dipertahankan: KPI cards, jadwal + sub-ledger "Dibayar oleh", saldo kredit.

## Komponen/tipe baru

- `components/billing/PrintInvoiceButton.tsx`
- `InvoiceType` FE + label KEKURANGAN di `InvoiceList`/`AllInvoiceList` (konsistensi R1)
- Tipe `StatementPaymentLine`, `StatementTimelineEvent` di `lib/types/api.ts`

## Verifikasi

Kontrak E2E #386 (KPR BTN, cair parsial): header bank/plafon/state ✓, 3 pembayaran
per sumber + total ✓, invoice KEKURANGAN tampil ✓, timeline 6 event urut ✓.
`go test ./internal/sale` hijau · `tsc` hijau.

## Backlog R2

| # | Item |
|---|---|
| R2-B1 | Tombol "Unduh PDF Statement" (cetak satu halaman utuh utk customer) |
| R2-B2 | Nomor kwitansi inline di riwayat (kini via tombol; butuh join receipt di payload) |
| R2-B3 | Nama bank penyalur di baris pencairan (kini di keterangan saja) |
