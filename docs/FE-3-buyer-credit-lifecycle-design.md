# FE-3 — Buyer Credit Lifecycle (design review)

> Status: **DESIGN — belum ada kode/migration.** Menunggu approval keputusan modeling.
> Scope (disetujui): pemakaian saldo kredit buyer (pre-BAST) ke cicilan — tanpa jurnal,
> tanpa kas, hanya sub-ledger, append-only, dengan audit (who/when/why) + idempotency.
> Di luar scope: post-BAST overpay / split-credit (ditunda). Tanggal: 2026-07-01.

---

## 1. Model akuntansi (invariant FE-3)

Saldo kredit buyer sudah **diterima kas dan diakui sebagai kewajiban** saat overpay
(jurnal `Dr Bank / Cr Uang Muka 2-2000` diposting di FE-2). "Apply credit" hanyalah
**reklasifikasi sub-ledger**: menandai bahwa sebagian Uang Muka yang tadinya *unallocated*
kini menutup cicilan #N.

- ❌ **Tidak ada cash movement** — uang sudah masuk sebelumnya.
- ❌ **Tidak ada journal entry baru** — GL tetap `Uang Muka 2-2000`, total tak berubah.
- ✅ **Sub-ledger `payment_allocations` bertambah** — baris baru (append-only).
- ✅ Saat BAST nanti, Uang Muka → Pendapatan seperti biasa (tak terpengaruh).

Enforcement: jalur apply-credit **tidak pernah** memanggil posting service. Uji: jumlah
`journal_entries` & saldo GL tidak berubah sebelum/sesudah apply.

---

## 2. Keputusan modeling (INTI — butuh approval)

### Masalah
Append-only + UNIQUE `uk_pa_termin_target(tenant_id, termin_payment_id, schedule_key)`
(guard #3, `schedule_key = COALESCE(payment_schedule_id,0)`). "Kredit terpakai" harus:
(a) menaikkan `paid_amount` cicilan S, dan (b) menurunkan saldo kredit tersedia — **tanpa
mengubah baris lama** dan **tanpa melanggar UNIQUE**.

Baris `buyer_credit` negatif (T, schedule_key=0) akan **bentrok** dengan baris buyer_credit
asli termin T. Menautkan kredit ke termin sumber juga janggal (kredit itu *fungible* — pool,
bukan milik satu termin).

### Rekomendasi: `allocation_type='credit_application'`, termin NULL, + tabel event

Kredit bersifat **pool per unit**. Satu aksi apply menghasilkan:
1. **1 baris event** `credit_applications` (audit: who/when/why + idempotency).
2. **1 baris sub-ledger** `payment_allocations`:
   `{allocation_type:'credit_application', payment_schedule_id:S, amount:X, termin_payment_id:NULL, credit_application_id:CA.id, created_by:who}`.

**Kenapa `termin_payment_id = NULL`:** kredit tidak berasal dari satu termin (pool). NULL
membuat baris ini lolos dari UNIQUE per-termin (MySQL: NULL distinct) → banyak apply ke
cicilan sama diizinkan, dan **guard #3 untuk pembayaran kas tetap utuh** (baris kas selalu
punya termin non-null). Baris `buyer_credit` asli (termin non-null) juga tak tersentuh.

### Formula saldo (derivable dari sub-ledger — sumber kebenaran)
```
available_credit(unit) = Σ(amount | type='buyer_credit', unit) − Σ(amount | type='credit_application', unit)
paid_amount(S)         = Σ(amount | schedule_id=S, type IN ('schedule','credit_application'))
```

### Alternatif yang ditolak
- **Baris buyer_credit negatif** → bentrok UNIQUE (schedule_key=0).
- **Ubah UNIQUE jadi termasuk allocation_type** → melemahkan guard #3 untuk jalur kas;
  dan tetap janggal menautkan kredit fungible ke termin.
- **Tanpa tabel event (reason/idempotency jadi kolom di payment_allocations)** → mengotori
  sub-ledger dengan kolom yang hanya relevan untuk 1 tipe baris.

---

## 3. Schema (rancangan migration 000026 — BELUM dibuat)

```sql
-- Kredit itu pool (bukan milik satu termin) → izinkan termin NULL untuk credit_application.
ALTER TABLE payment_allocations
    MODIFY COLUMN termin_payment_id BIGINT UNSIGNED NULL,
    ADD COLUMN credit_application_id BIGINT UNSIGNED NULL AFTER termin_payment_id,
    ADD INDEX idx_pa_credit_app (tenant_id, credit_application_id);

-- Event apply-credit (audit + idempotency). Analog termin_payments : payment_allocations.
CREATE TABLE credit_applications (
    id                  BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    tenant_id           BIGINT UNSIGNED NOT NULL,
    sale_contract_id    BIGINT UNSIGNED NOT NULL,
    unit_id             BIGINT UNSIGNED NOT NULL,
    payment_schedule_id BIGINT UNSIGNED NOT NULL,
    amount              DECIMAL(20,4)   NOT NULL DEFAULT '0.0000',
    reason              VARCHAR(500)    NOT NULL DEFAULT '',   -- "why"
    applied_by          BIGINT UNSIGNED NULL,                  -- "who"
    idempotency_key     VARCHAR(64)     NULL,
    created_at          DATETIME(3),                           -- "when"
    updated_at          DATETIME(3),
    INDEX idx_ca_tenant   (tenant_id),
    INDEX idx_ca_contract (tenant_id, sale_contract_id),
    INDEX idx_ca_schedule (tenant_id, payment_schedule_id),
    UNIQUE KEY uk_ca_idempotency (tenant_id, idempotency_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```
Rollback: drop tabel + kolom (aman; `credit_application` baris hanya ada bila fitur dipakai).

> `schedule_key` generated column TIDAK bergantung pada `termin_payment_id` (hanya
> `payment_schedule_id`) → mengubah termin jadi nullable tak mempengaruhinya.

---

## 4. API contract

**GET `/sale-contracts/{contractID}/buyer-credit`** — saldo kredit tersedia + rincian.
```json
{ "unit_id": 38, "available": "200000000",
  "sources":      [{ "termin_payment_id": 8, "amount": "200000000", "date": "2026-06-29", "receipt_number": "KWT/2026/000042" }],
  "applications": [{ "id": 3, "payment_schedule_id": 9, "label": "Termin 2", "amount": "50000000", "reason": "...", "applied_at": "..." }] }
```

**POST `/sale-contracts/{contractID}/apply-credit`** — pakai kredit ke cicilan. TIDAK ada jurnal.
```json
// body: { "schedule_id": 9, "amount": "50000000", "reason": "pelunasan dari titipan", "idempotency_key": "..." }
// resp: { "credit_application_id": 3, "schedule_id": 9, "applied": "50000000",
//         "schedule_paid_total": "350000000", "schedule_fully_paid": false, "remaining_credit": "150000000" }
```
Amount opsional → default: `min(available, sisa cicilan)` (pakai sebanyak mungkin).

**Validasi (semua di dalam satu transaksi, di bawah lock):**
1. `amount > 0`, whole rupiah.
2. `amount ≤ available_credit(unit)` — tak boleh melebihi saldo (else `ErrCreditExceedsAvailable`).
3. `paid_amount(S) + amount ≤ amount(S)` — cicilan tak boleh overpaid (else `ErrScheduleOverpaid`).
4. Tenant isolation (semua query tenant-scoped, guard tenant di setiap baris).
5. Idempotency (`uk_ca_idempotency`) — retry kunci sama tidak membuat aplikasi kedua.

---

## 5. Concurrency & atomicity

Satu transaksi DB (pola FE-2 `CommitPayment`):
1. **Lock** baris `sale_contracts` (unit) `FOR UPDATE` → serialisasi semua apply-credit unit
   ini (uji "concurrent apply credit"). Plus **lock cicilan target** `FOR UPDATE` → cegah
   overpay & race dengan pembayaran kas ke cicilan sama.
2. Hitung `available_credit` (read konsisten dalam tx), validasi.
3. Insert `credit_applications` + insert `payment_allocations(credit_application)` + update
   cache `paid_amount(S)` (+ status `received` bila lunas + sinkron invoice `MarkPaidByScheduleIDInTx`).
4. Gagal di langkah mana pun → **rollback penuh** (uji rollback).

Reuse: `applyScheduleAllocationTx` bisa dipakai untuk baris credit_application (kirim
`AllocationType` param) — atau helper baru `insertCreditApplicationRowTx`.

---

## 6. Reader impact (P4)

`ListAllocationsByUnit` saat ini **INNER JOIN** `termin_payments` → baris credit_application
(termin NULL) akan **terbuang**. Perlu diubah:
- `LEFT JOIN termin_payments` + `LEFT JOIN credit_applications`.
- Filter unit: `tp.unit_id = ? OR ps.unit_id = ?` (kas via termin; kredit via cicilan).
- `termin_date` → `COALESCE(tp.date, ca.created_at)`.
- Label credit_application: "Dari Saldo Kredit" (di bawah cicilan target).

---

## 7. Frontend

- **Statement** (`CustomerStatementView`): kartu "Saldo Kredit Buyer" jadi **net** (tersedia =
  sumber − terpakai); tampilkan "Dipakai ke Cicilan #x: Rp xxx". Sub-row cicilan "Dibayar oleh"
  memasukkan baris **"Dari Saldo Kredit"**.
- **Collection / RecordPayment**: bila `available_credit > 0`, tampilkan opsi **[Gunakan Saldo
  Kredit]** → panggil `apply-credit` (bukan record payment kas). Pilih cicilan + jumlah.
- Endpoint baru di `lib/api/sale.ts`: `fetchBuyerCredit`, `applyBuyerCredit`.

---

## 8. Rencana test (scope wajib)

| # | Test | Level |
|---|---|---|
| 1 | apply credit **full** (kredit == sisa cicilan → cicilan lunas) | integration |
| 2 | apply credit **partial** (kredit sebagian sisa cicilan) | integration |
| 3 | kredit **< cicilan** (kredit habis, cicilan belum lunas) | integration |
| 4 | kredit **> cicilan** (cicilan lunas, sisa kredit tetap tersedia) | integration + pure |
| 5 | **concurrent** apply credit (2 goroutine, tak overdraw saldo) | integration |
| 6 | **rollback** bila insert allocation gagal (nihil tertulis, saldo utuh) | integration |
| 7 | validasi: exceed available → `ErrCreditExceedsAvailable` | unit |
| 8 | validasi: overpay schedule → `ErrScheduleOverpaid` | unit |
| 9 | idempotency: retry kunci sama → 1 aplikasi | integration |
| 10 | **no journal**: `journal_entries` count tak berubah | integration |

---

## 9. Keputusan menunggu approval

1. **Modeling** (§2): `allocation_type='credit_application'` + `termin_payment_id` NULL + tabel
   `credit_applications` (audit/idempotency). → *rekomendasi utama*.
2. **Default amount** apply: `min(available, sisa cicilan)` bila amount tak diisi. → *rekomendasi ya*.
3. **Lock granularity**: lock `sale_contracts` row untuk serialisasi apply-credit per unit. →
   *rekomendasi ya* (cukup untuk "concurrent apply credit"; race apply-vs-overpay-kas lintas
   cicilan = edge prioritas rendah, dicatat).
4. **UI entry point**: opsi [Gunakan Saldo Kredit] di Collection/RecordPayment **dan** Statement,
   atau salah satu dulu?

> **STOP.** Tidak ada kode/migration dibuat. Menunggu review §9 sebelum implement (P1 schema dst).
