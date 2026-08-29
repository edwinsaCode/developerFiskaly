# Increment 8 — Cancellation & Refund — SHIPPED (Implementation Report)

> Status: **SHIPPED + tervalidasi (3 siklus integration real MySQL, run pertama).**
> 2026-07-17. Blueprint §1 (CORE). Seluruh prerequisite terpakai: Unit Lifecycle
> (jalur resmi `sold→available`), Booking (payout `pending_refund`), Approval
> Workflow (gate opt-in `TargetCancellation`), P0-4 (porsi true-up ikut dibalik).
> Frontend spec: `increment-8-frontend-spec.md`.
> Urutan LOCKED berikutnya: **Commission Engine** → Product Sprint.

## 1. Model akuntansi (blueprint §1, append-only — Invariant #5)

| Stage | Perlakuan |
|---|---|
| **Pra-BAST** | Satu jurnal settlement: `Dr Uang Muka 2-2000 (Σ diterima)` / `Cr Pendapatan Lain 4-2000 (penalti)` / `Cr Hutang Refund 2-2200 (sisa)`. Unit → `available` |
| **Pasca-BAST** | (1) `ledger.Reverse(Event 3)` — pendapatan+PPN+piutang batal, uang buyer kembali ke 2-2000; (2) **pembalikan COGS = mirror himpunan jurnal COGS unit** (`cogs_journal_id` + baris jurnal `source='hpp_trueup'` ber-tag unit) — **INV-COGS-SUM ditegakkan literal**: Cancellation tidak tahu cara HPP terbentuk, ia membalik semua yang ter-posting; (3) `ledger.Reverse(akrual PPh)` + obligation → `cancelled`; **PPh sudah disetor → DIBLOKIR** (`ErrTaxAlreadyPaid`, TODO(tax-advisor) restitusi); (4) settlement sama dengan pra-BAST. Unit kembali stok **pada nilai aktual** (1-3xxx pulih penuh — terbukti test) |
| **Refund** | Dokumen payout: `Dr 2-2200 / Cr Bank`. Sumber kedua: **booking fee `pending_refund`** (Increment 7) → reklas `Dr 2-2100 / Cr 2-2200` saat refund dibuat; disposisi → `refunded` saat dibayar |

**Σ diterima = derived dari ledger** (`Σ credit−debit` 2-2000 ber-tag unit, posted) — bukan tabel saldo; untuk pasca-BAST ditambah kembali porsi Uang Muka yang didebit Event 3 (karena reversal memulihkannya). Penalti > Σ diterima → ditolak.

## 2. Lifecycle & governance

`requested → approved → processed | rejected` (blueprint persis). Satu cancellation
AKTIF per unit (UNIQUE `active_key`); rejected melepas slot. **Approve ber-gate
opt-in** Generic Approval Workflow (`TargetCancellation` — enum yang sejak
Increment 5 inert, kini hidup): tanpa workflow aktif → langsung; dengan workflow →
butuh request APPROVED (`ErrApprovalRequired`, 422). Audit actor per transisi.
Process = SATU transaksi (rencana dihitung ulang otoritatif di dalam tx; row-lock;
konflik unit → 409).

## 3. Yang dikirim

- **Migration `000045_cancellation_refund`** (reversible, teruji down/up): tabel `cancellations` (CHECK stage/status, UNIQUE one-active-per-unit, jejak 4 jurnal + refund) + `refunds` (CHECK source/status + source-ref); `sale_records` += `cancelled_at`, `cancellation_id` (baris BAST tetap); `bookings` CHECK += disposisi `refunded`; seed akun **2-2200 Hutang Refund** semua tenant + `coa.go`.
- **`internal/cancellation`**: `Service` (Request auto-deteksi stage, Approve+gate, Reject, **Preview** = Journal Preview akun-per-akun dari plan builder yang SAMA dengan eksekusi, Process atomik 9 langkah, reads) + `refund_service.go` (CreateBookingRefund, PayRefund dengan validasi bank COA-driven, reads) + handler + adapter approval.
- **Efek samping proses**: unit release pinned + lifecycle log (`reservation_cancelled`/`contract_cancelled`/`cancelled_post_bast`, ref `cancellation` — event vocabulary Increment 6 kini terpakai penuh); kontrak `scheme_state='cancelled'` + jadwal belum dibayar → `superseded`; `tax.ObligationStatusCancelled` (konstanta baru additive).
- **API**: `POST /units/{id}/cancellations` · `GET /cancellations?status=` · `GET /cancellations/{id}` · **`GET /cancellations/{id}/preview`** · `POST .../approve|reject|process` · `POST /refunds/from-booking/{bookingID}` · `GET /refunds?status=` · `GET /refunds/{id}` · `POST /refunds/{id}/pay`.

## 4. Test (18 paket unit + 18 paket integration — semua hijau)

- **Unit:** state machine (legal/ilegal penuh, terminal immutable).
- **Integration (3 siklus, real MySQL):**
  1. **Pra-BAST**: termin 300jt → cancel penalti 50jt → preview settlement benar → process → 2-2000 nol, 4-2000=50jt, 2-2200=250jt, unit available + log, refund pending → pay → 2-2200 nol, bank netto 50jt (penalti tertahan); duplikat request 409; process pra-approve 409; double-pay 409.
  2. **Pasca-BAST + true-up** (bukti INV-COGS-SUM): skenario P0-4 penuh (BAST budgeted 100jt + true-up +20jt posted + PPh akrual 50jt) → cancel penalti 100jt → preview {received 500jt, COGS 120jt, revenue 2M, tax 50jt, refund 400jt} semua benar → process → **5-1000 = 0** (Event 4 + true-up terbalik), **1-3000 = 480jt** (unit kembali stok nilai aktual), 4-1000/2-4000/1-2000 = 0, penalti & payable benar, unit available (`cancelled_post_bast`), sale_record ditandai, obligation `cancelled`.
  3. **Booking refund**: pending_refund → reklas 2-2100→2-2200 → pay → semua nol, disposisi `refunded`, duplikat ditolak.
- Regresi seluruh suite existing hijau; migration 000045 down/up bersih.

## 5. Migration Impact
Satu migration additive+reversible (000045). Tanpa perubahan tabel ledger.

## 6. Backward Compatibility
`sale_records` lama utuh (kolom baru NULL); booking tanpa refund tak tersentuh; unit-only legacy sale (tanpa kontrak) didukung (kontrak nullable); semua endpoint existing tak berubah; enum `TargetCancellation` yang inert kini berfungsi tanpa menyentuh engine approval.

## 7. Dashboard Impact
- Kartu "Perhatian": **Cancellation menunggu approval/proses** (`GET /cancellations?status=requested|approved`) + **Refund menunggu pembayaran** (`GET /refunds?status=pending`).
- Tile "Hutang Refund outstanding" = saldo 2-2200 (derived, trial-balance existing).
- Pipeline unit: unit batal otomatis kembali ke segmen `available` (status + log).

## 8. Report Impact
- **L/R**: pendapatan & HPP unit batal keluar dari L/R (reversal bertag project/unit — periode berjalan); penalti muncul sebagai Pendapatan Lain-lain.
- **Neraca**: 2-2000 bersih; 2-2200 muncul sampai refund dibayar; 1-3xxx pulih ke aktual (unit kembali stok).
- **Pajak**: obligation `cancelled` keluar dari outstanding; laporan PPh konsisten dengan akrual terbalik.
- **AR aging**: piutang unit batal hilang (kredit reversal Event 3).
- Semua dari ledger yang sama — nol duplicate SoT.

## 9. Follow-up backlog

| # | Item |
|---|---|
| CX-B1 | Gate `TargetRefund` pada PayRefund (vocabulary sudah ada) |
| CX-B2 | PPh sudah disetor: alur restitusi/pemindahbukuan (kini diblokir dengan pesan jelas; TODO(tax-advisor)) |
| CX-B3 | Clawback komisi saat cancel (blueprint §1↔§3) — menunggu Commission Engine (increment berikutnya) |
| CX-B4 | Kwitansi/bukti kas keluar untuk pembayaran refund (billing doc) |
| CX-B5 | PPN pasca-BAST: reversal Event 3 sudah membalik PPN keluaran; faktur pajak pengganti/pembatalan e-Faktur = proses eksternal (dokumentasi user) |
