# Increment 3 — Payment Scheme (Strategy Seam)

> Implementasi dari desain APPROVED `increment-3-payment-scheme-design.md`
> (D1–D5 + penyempurnaan §11). Blueprint: `blueprint-domain-additions.md` §5.
> Prinsip dipegang: additive-only, ledger backbone tidak berubah, Project
> aggregate root utuh, tidak ada refactor modul yang sudah di-approve.

## Ringkasan

Payment Scheme kini adalah **strategy seam**: perilaku skema (validasi kontrak,
usulan jadwal, state machine, counterparty piutang, gate BAST) hidup di
`scheme.PaymentSchemePolicy` + registry — **bukan** `if scheme == "kpr"`.
Menambah skema baru 5 tahun lagi = baris config (varian) atau satu policy baru
(perilaku baru), tanpa menyentuh Sale Contract maupun Ledger.

## Perubahan Database (migration 000037, reversible, ADD-only)

| Objek | Isi |
|---|---|
| `payment_schemes` (baru) | master per tenant: `code` immutable, `policy_type` (CHECK cash\|cash_installment\|kpr\|inhouse), `params` JSON, `is_active`. Seed 5 default (CASH, CASH-BTHP, KPR-SUB, KPR-KOM, INHOUSE) untuk tenant existing (SQL) & baru (`scheme.SeedDefaultSchemes` di registrasi) |
| `financing_sources` (baru) | master bank penyalur (mengubur `bank_kpr` free-text): `code` immutable, `type` (CHECK subsidi\|komersial\|lainnya) |
| `contract_payment_events` (baru) | **append-only** Payment Event (approval note #3): event, from/to_state, financing_source, `journal_entry_id` (reklas), created_by |
| `sale_contracts` +5 kolom nullable | `payment_scheme_id` (FK), `financing_source_id` (FK), `scheme_state`, **`scheme_params_snapshot` JSON (Terms Snapshot, note #2)**, `approval_request_id` (seam §10) |
| `termin_payments` | +`financing_source_id` (tag pencairan KPR) |
| `payment_schedules` | +`schedule_version` (supersede pattern BS-5); status baru `superseded` |
| COA | **`1-2200 Piutang Bank (KPR)`** — seed baru + INSERT tenant existing (1-2100 sudah terpakai "Piutang Lain-lain", desain dikoreksi) |
| Backfill | `payment_type` tunai→CASH, kpr→KPR-KOM (per tenant). `scheme_state`/snapshot tetap NULL = **legacy semantics** (tanpa state machine/gate — perilaku lama utuh) |

Reversibilitas diuji (down→up); down tidak menghapus jurnal (append-only).

## Domain (`internal/scheme`)

- **`PaymentSchemePolicy`**: `PolicyType()`, `ValidateParams`, `ValidateContract`,
  `BuildSchedulePlan` (Σ == GrossAmount **persis**, `Money.Allocate`
  largest-remainder), `AllowedEvents()` (state machine per event),
  **`ResolveReceivableAccount(params, state)`** (approval note #1 — akun dari
  KONFIGURASI: `receivable_account` default `1-2000`,
  `financing_receivable_account` mis. `1-2200`; satu-satunya default terpusat =
  `scheme.DefaultReceivableAccount`), `CanBAST` (gate `bast_gate` param:
  full_payment/akad/dp_paid).
- **4 policy**: Cash, CashInstallment, KPR, InHouse + **Registry** (wiring, immutable).
- **Params** JSON (string-decimal, tanpa float) + seam bunga `interest_rate_pct`
  yang MENOLAK nilai ≠ 0 (`TODO(tax-advisor)` — BS-6).
- `PolicyType.LegacyPaymentType()` — pemetaan terpusat kompat `payment_type`.
- Master CRUD (`Service`): code & policy_type immutable by construction; params
  divalidasi policy; **mengubah master tidak menyentuh kontrak berjalan**.

## Integrasi alur Sale (`internal/sale/scheme_flow*.go`)

- **Kontrak baru (scheme flow aktif di produksi): WAJIB `payment_scheme_id` +
  `customer_id` + `sales_person_id`** (kebijakan LOCKED Increment 1 jatuh tempo;
  approval note #4). Params master dibekukan ke `scheme_params_snapshot`; state
  `signed`; event `contract_signed`. `payment_type` legacy diisi otomatis.
- **Kontrak berjalan HANYA membaca snapshot** — diuji: master diubah → plan
  kontrak tidak berubah.
- **Jadwal**: `GET .../schedule-plan` = usulan policy; create memvalidasi
  Σ == Gross persis + menolak dobel jadwal aktif; reschedule/konversi =
  supersede (baris terbayar tidak disentuh) + `schedule_version` naik.
- **Milestone otomatis** (proyeksi best-effort, tidak pernah membatalkan
  pembayaran): dp_paid / installment_paid / fully_paid dari `ReceivePayment`;
  handed_over dari BAST.
- **Gate BAST**: `CanBAST` dari snapshot — menambah gate BCA-2. KPR menolak BAST
  pra-akad (diuji).
- **Counterparty piutang**: BAST men-debit akun hasil `ResolveReceivableAccount`
  (KPR pasca-akad → `1-2200`); pembayaran pasca-BAST mengkredit akun yang sama.
  **Catatan state machine KPR**: `handed_over` = event tercatat **KeepState** —
  `scheme_state` KPR melacak lifecycle PEMBIAYAAN (fakta BAST milik
  `sale_records`); tanpa ini pencairan pasca-BAST salah resolve (tertangkap test).
- **Reklas akad pasca-BAST**: `Dr 1-2200 / Cr 1-2000` sebesar outstanding,
  balanced tanpa P&L; pra-BAST TIDAK ada jurnal (BS-2). Journal id tercatat di
  event.
- **Konversi scheme** (KPR `bank_rejected` → inhouse/cash): validasi policy lama
  mengizinkan `converted`; jadwal unpaid superseded; snapshot baru; state
  `signed` (aman: gate berbasis fakta pembayaran); financing source dilepas.
- **Pencairan KPR**: `payment_source=kpr_disbursement` + `financing_source_id`
  (tervalidasi; ditolak untuk source lain).

## API

| Method | Path | Keterangan |
|---|---|---|
| GET/POST/PUT | `/payment-schemes`, `/{id}` | master scheme (params divalidasi policy) |
| GET/POST/PUT | `/financing-sources`, `/{id}` | master bank |
| POST | `/sale-contracts` | + `payment_scheme_id`, `financing_source_id`, `customer_id`, `sales_person_id` (wajib utk kontrak baru) |
| GET | `/sale-contracts/{id}/schedule-plan` | usulan jadwal dari policy (snapshot) |
| POST | `/sale-contracts/{id}/scheme-events` | manual: submitted_to_bank\|bank_approved\|bank_rejected\|akad\|disbursed\|takeover (event lain ditolak — otomatis / menunggu Cancellation) |
| POST | `/sale-contracts/{id}/scheme-convert` | konversi scheme |
| POST | `/sale-contracts/{id}/schedule-regenerate` | reschedule (supersede) |
| GET | `/sale-contracts/{id}/payment-events` | audit trail lifecycle |
| POST | `/collections/payment` | + `financing_source_id` → pencairan KPR |

## Jurnal (tidak ada engine baru)

```
DP/cicilan/pencairan PRA-BAST : Dr Bank / Cr Uang Muka 2-2000        (tidak berubah)
BAST (KPR pasca-akad)         : Dr 2-2000 + Dr 1-2200 (sisa) / Cr Pendapatan [+PPN]
Akad PASCA-BAST (reklas)      : Dr 1-2200 / Cr 1-2000                (baru; tanpa P&L)
Pencairan pasca-BAST          : Dr Bank / Cr 1-2200
Cicilan buyer pasca-BAST      : Dr Bank / Cr 1-2000                  (tidak berubah)
```

## Test (semua hijau: 15 package, `-tags integration`)

- **Unit `scheme`**: registry lengkap; plan Σ == gross persis utk 4 policy dgn
  angka ganjil (largest-remainder); matriks ValidateParams (9 penolakan + seam
  bunga); financing-source rules; state machine KPR full journey + transisi
  ilegal + KeepState takeover/handed_over; **ResolveReceivableAccount
  config-driven** (ganti params → resolusi ikut — bukti tanpa hardcode); 3 gate
  CanBAST (batas persis ±1 rupiah); JSON roundtrip snapshot.
- **Integration (MySQL, wiring produksi)**: (A) KPR full journey — enforcement,
  snapshot immunity thd perubahan master, Σ jadwal, milestone dp_paid, gate
  menolak BAST pra-akad, BAST men-debit 1-2200, pencairan mengkredit 1-2200
  hingga saldo 0, termin ber-tag bank, urutan 7 event; (B) akad pasca-BAST →
  reklas 900jt 1-2000→1-2200, jurnal hanya menyentuh 2 akun piutang; (C) bank
  menolak → konversi INHOUSE: jadwal superseded, rebind + snapshot baru,
  jadwal versi 2.
- **Regresi**: seluruh test lama lulus tanpa perubahan perilaku (scheme flow
  opsional via `WithSchemeFlow`; tanpa opsi = legacy penuh).

## Risiko & Backward Compatibility

- Kontrak lama (snapshot NULL): tanpa state machine/gate — SEMUA alur lama
  (payment, BAST, schedule) tidak berubah; backfill hanya mengisi
  `payment_scheme_id` untuk reporting.
- **Behavior change yang disengaja (approval #4): kontrak BARU wajib scheme +
  customer + sales person** → **follow-up frontend**: form kontrak perlu
  mengirim field baru (sampai itu terjadi, create kontrak dari UI lama akan 400
  dengan pesan jelas).
- Milestone events = proyeksi best-effort (SoT = ledger/termin/alokasi) —
  kegagalan proyeksi tidak membatalkan pembayaran.
- Approval Workflow belum ada → konversi/reschedule berjalan dengan audit
  `created_by` + event log; kolom `approval_request_id` siap untuk gate engine §10.

## Non-goals (increment berikutnya, seam siap)

Booking, Cancellation/Refund (event `cancelled` tidak bisa diposting manual),
transfer unit, joint buyer, bunga inhouse (seam menolak ≠ 0), Approval engine,
read-model AR-per-bank dashboard, UI frontend scheme.

## Hardening pasca-approval (2026-07-03, diminta owner)

1. **`event_date` (migration `000038`)** — `contract_payment_events.event_date`
   = tanggal KEJADIAN BISNIS, terpisah dari `created_at` (tanggal input).
   Semua jalur mengisinya dengan tanggal bisnis yang benar: `contract_signed` =
   tanggal kontrak; milestone pembayaran = tanggal pembayaran; `handed_over` =
   tanggal BAST; event manual (akad dll.) menerima `event_date` opsional di
   body (default sekarang). **Jurnal reklas akad juga bertanggal event_date.**
   Backfill baris lama = created_at. Diuji integration (contract_signed =
   tanggal kontrak, dp_paid = tanggal pembayaran).
2. **Snapshot ber-versi (`TermsSnapshot` envelope)** — snapshot kini amplop
   `{policy_type, policy_version, params}`; `PaymentSchemePolicy` mendapat
   `PolicyVersion()` (semua v1). Interpretasi kontrak **self-contained**:
   policy diresolusi dari amplop snapshot (bukan master row). Perubahan
   algoritma strategy di masa depan = daftarkan versi baru — kontrak lama tetap
   dibaca dengan interpretasi versinya. Snapshot format awal (bare params)
   tetap terbaca via fallback master (kompat). Diuji unit (roundtrip + legacy) +
   integration (amplop tersimpan `kpr` v1).

## Next

Sesuai urutan increment: **Tax Rule konfiguratif** (blueprint §7) — increment
berikutnya setelah review.
