# Product Sprint 6 — R1 KPR Realization — SHIPPED

> Status: **SHIPPED + E2E API penuh + verifikasi visual Chrome.** 2026-07-22.
> Flow KPR developer properti end-to-end tanpa workaround:
> Booking → Kontrak → Pengajuan → SP3K → Akad → Pencairan → Invoice Kekurangan → Pelunasan → BAST.

## 1. Backend

- **Guard urutan flow** (`sale/collection.go`): pencairan (`financing_source_id` → source
  `kpr_disbursement`) hanya sah pada state `akad`/`disbursed` (pencairan bertahap
  diizinkan). Pelanggaran → `ErrDisbursementRequiresAkad`, HTTP **422** (bukan 500).
- **Atomik uang + state**: `ReceivePayment` → jurnal Event 2 + kwitansi otomatis;
  milestone `disbursed`/`fully_paid` diproyeksikan best-effort (state BUKAN jurnal —
  jika proyeksi gagal, termin tetap sah; retry idempoten).
- **Invoice KEKURANGAN** (`billing`): tipe baru non-schedule; nominal SELALU dari
  `ContractFinancialSummary.Outstanding` (satu rumus, H-2). Dedup: satu KEKURANGAN
  terbuka per kontrak (422). Kebijakan tenant `auto_shortfall_invoice`
  (migration `000050`, default OFF → CTA manual).
- **Settle otomatis** (fix E2E sesi ini): `SettleShortfallIfPaid` — setiap pembayaran
  kontrak yang membuat outstanding = 0 menandai invoice KEKURANGAN terbuka → `paid`
  (invoice non-schedule tak terjangkau sinkron `MarkPaidByScheduleID`).
- **Gate BAST** (fix E2E sesi ini, `scheme/policy.go`): `GateAkad` kini menerima
  `fully_paid` — pelunasan penuh adalah kepastian LEBIH kuat dari akad (flow nyata:
  akad → cair → lunas → BAST; proyeksi milestone memajukan state melewati akad).
  `bank_approved` + lunas-facts tetap ditolak (state otoritatif).
- **Snapshot harga kontrak** (migration `000049`): `unit_price_snapshot` +
  `price_is_snapshot` di summary.
- **KPR pipeline report**: `GET /reports/kpr-pipeline` — rows per kontrak KPR
  (state, plafon, Σ cair per `financing_source`, outstanding, umur state) + agregat
  count per tahap + `total_disbursed`/`total_outstanding`.
- **Dashboard**: seksi `kpr` additive di `GET /reports/dashboard` (R5 tinggal membaca).
- **Statement** diperluas (R2 tinggal menyusun UI): `payments[]` per sumber
  (booking_fee/collection/kpr_disbursement), `timeline[]` milestone (signed→handed_over).

## 2. Frontend

- **`DisbursementModal`** — SATU aksi staf keuangan: tanggal, bank penyalur (terkunci
  dari kontrak), nominal, rekening tujuan (COA-driven), no. referensi SP2D, catatan +
  **pratinjau real-time dari engine** (harga, dibayar customer, dana bank, total
  diterima, outstanding setelah pencairan). Sukses → kwitansi + bila outstanding > 0
  muncul CTA **"Buat Invoice Kekurangan"** satu-klik.
- **`FinancingMilestones`** — stepper Kontrak→Pengajuan→**SP3K**→Akad→Dana Cair
  (relabel SP3K ✓); state `disbursed` → tombol "Catat Pencairan Lagi" (bertahap);
  tampil juga PASCA-BAST (pencairan sering terjadi setelah serah terima);
  `onPaymentRecorded` → kartu kontrak refresh tanpa reload (fix friction sesi ini).
- **`KPRPipelineBoard`** (`/penjualan/kpr` + sidebar "KPR") — KPI strip (Total Dana
  Cair, Outstanding KPR, Lunas), kolom Belum Diajukan/Pengajuan/SP3K/Akad/Dana Cair,
  kartu: customer, unit, harga, bank, cair, outstanding, umur tahap (warning menua),
  baris LUNAS ringkas. Empty state informatif.

## 3. E2E verifikasi (tenant fresh, COA seeded)

Skenario klien persis: **harga 500jt · customer 50jt · bank cair 430jt**
→ summary `total_paid 480jt / outstanding 20jt` ✓ → invoice KEKURANGAN 20jt ✓
→ pelunasan → state `fully_paid`, invoice otomatis `paid` ✓ → BAST: jurnal
revenue + COGS (HPP budgeted 300jt = RAB 600jt × luas 72/144) ✓.
Guard pra-akad ditolak 422 ✓. Pencairan parsial kedua (skenario 300jt & 650jt) ✓.
**Neraca saldo balanced persis: Σ 1.622.500.000 dua sisi** (PPh final 2,5%,
reklas titipan booking, uang muka → pendapatan; Invariant #1 ✓).
Visual Chrome: board, modal + pratinjau, CTA kekurangan, statement — screenshot arsip.

## 4. Temuan E2E yang diperbaiki di sesi ini

| # | Temuan | Fix |
|---|---|---|
| 1 | Guard akad balas HTTP 500 | mapping 422 (`sale/handler.go`) |
| 2 | Invoice KEKURANGAN selamanya `issued` setelah lunas | seam `SettleShortfallIfPaid` + test |
| 3 | Gate BAST menolak `fully_paid` (blokir flow akad→cair→lunas→BAST) | `GateAkad` terima `fully_paid` + test regresi |
| 4 | Kartu kontrak stale pasca-pencairan | `onSuccess`/`onPaymentRecorded` refresh |

## 5. Backlog R1

| # | Item |
|---|---|
| R1-B1 | Statement 360 UI (payload `payments[]`+`timeline[]` SUDAH ada) — inti R2 |
| R1-B2 | Dashboard: render seksi `kpr` (payload SUDAH ada) — inti R5 |
| R1-B3 | Toggle kebijakan `auto_shortfall_invoice` di halaman Pengaturan (backend siap; kini via DB) |
| R1-B4 | Pipeline board: filter proyek + ambang warning umur konfigurabel (kini heuristik) |
| R1-B5 | Invoice KEKURANGAN partial-settle: tandai `paid` sebagian bila dibayar < nominal (kini settle penuh saat outstanding=0) |
| R1-B6 | Kwitansi pencairan: cantumkan bank penyalur + no. SP2D di cetakan |
| R1-B7 | `financing_source_id` di `RecordTermin` UI lama (TerminForm) — kini hanya via modal pencairan |
