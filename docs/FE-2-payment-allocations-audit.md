# FE-2 — Audit Payment Flow sebelum `payment_allocations`

> Status: **AUDIT / DESIGN REVIEW** — belum ada kode, belum ada migration.
> Tujuan: memetakan flow pembayaran yang ada, lalu merancang sub-ledger `payment_allocations`
> tanpa membuat jalur jurnal kedua (Invariant #1 & #5 tetap dijaga).
> Tanggal: 2026-07-01.

---

## 0. Ringkasan eksekutif (temuan utama)

1. **Belum ada tabel `payment_allocations`.** "Alokasi pembayaran" hari ini adalah **fungsi runtime**
   (`planAllocation`) yang hasilnya **tidak dipersist sebagai baris**. Satu-satunya jejak persist adalah:
   - kolom cache `payment_schedules.paid_amount` (agregat), dan
   - satu FK `payment_schedules.termin_payment_id` yang **hanya diisi saat cicilan LUNAS** (last-writer).
2. **Pemetaan uang→cicilan bersifat lossy.** Satu `termin_payment` bisa menutup beberapa cicilan (waterfall),
   tapi tidak ada baris yang mencatat "termin #42 mengalokasikan 5jt ke cicilan 1 dan 3jt ke cicilan 2".
   Cicilan yang dibayar **sebagian** menyimpan `paid_amount` tapi `termin_payment_id = NULL` — tidak diketahui
   termin mana yang mengisinya.
3. **Saldo kredit buyer (overpay pra-BAST) tidak punya baris sama sekali.** Hanya bisa diturunkan sebagai
   `Σtermin − Σcicilan`. Tidak ada sub-ledger, tidak ada audit trail.
4. **Satu pembayaran = tepat satu jurnal (2 baris, balanced).** Ini benar dan harus dipertahankan. FE-2
   **tidak boleh** menambah jurnal. `payment_allocations` murni sub-ledger.
5. **Jangan bingung dengan package `allocation`.** `internal/allocation`, `allocation_configs`,
   `allocation_executions`, dan `components/allocation/*` = **alokasi BIAYA proyek→unit (RAB)**. Domain berbeda
   total. `payment_allocations` = alokasi UANG MASUK→cicilan.

---

## 1. Inventory Entity Backend (existing)

Semua path relatif ke `backend/`.

### 1.1 `TerminPayment` — record penerimaan uang (INI "payment" hari ini)

- Struct: `internal/sale/model.go:15-39` · Tabel: `termin_payments`
- Migrasi: `000009` (+ `000021` credit_account, `000022` created_by/idempotency, `000023` payment_source)

| Kolom | Tipe | Fungsi |
|---|---|---|
| `amount` | `DECIMAL(20,4)` | nominal diterima (satu event = penuh; **tidak ada `paid_amount` di sini**) |
| `journal_entry_id` | `BIGINT NOT NULL` | **referensi jurnal** (1 termin → 1 jurnal) |
| `credit_account_code` | `VARCHAR(20) NULL` | routing: `2-2000` (Uang Muka, pra-BAST) / `1-2000` (Piutang, pasca-BAST) |
| `bank_account_code` | `VARCHAR(20)` | kas/bank tujuan (COA code, bukan FK) |
| `payment_source` | `VARCHAR(20) NULL` | audit: `collection` / `schedule_received` / `unit_termin` |
| `idempotency_key` | `VARCHAR(64) NULL` | anti double-submit, `UNIQUE(tenant_id, idempotency_key)` |
| `created_by` | `BIGINT NULL` | audit "siapa input" |

**Tidak ada** kolom `sale_contract_id`, `schedule_id`, maupun `receipt_id` di `termin_payments`.

### 1.2 `PaymentSchedule` — rencana cicilan

- Struct: `internal/sale/schedule.go:78-95` · Tabel: `payment_schedules` · Migrasi: `000013` (+`000022` paid_amount)

| Kolom | Fungsi |
|---|---|
| `amount` `DECIMAL(20,4)` | nominal tagihan cicilan |
| `paid_amount` `DECIMAL(20,4)` | **cache** akumulasi bayar; `remaining = amount − paid_amount` (dihitung, tidak disimpan) |
| `termin_payment_id` `BIGINT NULL` | **link ke termin — HANYA diisi saat LUNAS** (`collection.go:489-493`, `service.go:401-405`) |
| `status` | `scheduled` / `received` / `overdue` (jadi `received` hanya saat lunas) |
| `received_at` | diisi saat lunas |

> **Titik lossy #1:** cicilan partial → `paid_amount > 0` tapi `termin_payment_id = NULL`.
> Tidak bisa direkonstruksi termin mana yang berkontribusi.

### 1.3 `Invoice` (Faktur)

- Struct: `internal/billing/model.go:27-42` · Tabel: `invoices` · Migrasi: `000019`
- Dibuat **on-demand per cicilan** (`GenerateInvoice`), maksimal 1 per `schedule_id`. **Tidak** otomatis dibuat saat bayar.
- Relasi pembayaran hanya tidak langsung: saat cicilan lunas → `MarkPaidByScheduleID` membalik `status → paid`.
- **Tidak ada FK** dari invoice ke termin/receipt. `Invoice` tidak menyimpan paid/remaining — hanya `amount` + `status`.

### 1.4 `Receipt` (Kwitansi)

- Struct: `internal/billing/receipt.go:19-33` · Tabel: `receipts` · Migrasi: `000020`
- **Arah FK: `receipts.termin_payment_id → termin_payments.id`.** Satu receipt **per termin**
  (`UNIQUE(tenant_id, termin_payment_id)`), **bukan** per cicilan / per alokasi.
- Idempoten, `KWT/{tahun}/{6-digit}`, **tidak memposting jurnal**.

### 1.5 Journal Entry

- Struct: `internal/ledger/model.go` (`JournalEntry` `:30-45`, `JournalLine` `:54-67`)
- Posting pembayaran: `recordTerminEntry` (`internal/sale/service.go:139-240`) — **tepat 1 jurnal, 2 baris balanced**:
  - Pra-BAST: `Dr <bank> / Cr 2-2000 (Uang Muka Penjualan)`
  - Pasca-BAST: `Dr <bank> / Cr 1-2000 (Piutang Usaha)`
- `journal_entry_id` disimpan balik ke `termin_payments.JournalEntryID` (`service.go:230`).
- `Σdebit == Σkredit` divalidasi `posting_service.go:205-230` (Invariant #1).

> **Titik lossy #2:** jurnal adalah 1 lump Dr/Cr. Ketika 1 pembayaran menutup banyak cicilan,
> ledger tidak punya jejak per-cicilan. Itu wajar (ledger bukan tempatnya) — justru alasan butuh sub-ledger.

### 1.6 `ReceivePayment` — pintu masuk tunggal

`internal/sale/collection.go:232-340`. Urutan:

1. Idempotency (`:237-244`)
2. Resolusi anchor: `ScheduleID` | `ContractID` | `UnitID` (`:247-275`)
3. Kebijakan overpay: pra-BAST → boleh (jadi buyer credit); pasca-BAST → ditolak `ErrPaymentExceedsReceivable`
4. **Posting jurnal tunggal** via `recordTerminEntry` (`:283-295`)
5. **Alokasi ke cicilan** (`:299-309`) → `applyToSingleSchedule` / `applyToSchedules` (waterfall `planAllocation`)
   → hanya menulis `paid_amount` (+ `termin_payment_id` bila lunas). **Breakdown `AppliedSchedule` dikembalikan
   di response tapi TIDAK dipersist.** ← **titik integrasi FE-2**
6. Kwitansi idempoten (`:312`)
7. Sinkron invoice: `MarkPaidByScheduleID` untuk tiap cicilan lunas (`:313-319`)
8. `RemainingBalance = Gross − Σtermin` (`:322-327`)

Adapter (semua delegasi, tidak memposting jurnal sendiri):
`RecordCollectionPayment` (`/collections/payment`), `RecordInstallmentPaid` (`/schedules/{id}/received`),
`recordTermin` (`/units/{id}/termins`).

### Peta jawaban pertanyaan mapping

| Pertanyaan | Jawaban |
|---|---|
| **payment sekarang = tabel apa?** | `termin_payments` |
| **journal reference dimana?** | `termin_payments.journal_entry_id` (1:1) |
| **receipt reference dimana?** | terbalik: `receipts.termin_payment_id → termin_payments.id` |
| **schedule link dimana?** | terbalik & lossy: `payment_schedules.termin_payment_id` (hanya saat lunas) + cache `paid_amount` |

---

## 2. Inventory Frontend (existing)

Semua path relatif ke `frontend/`.

| Komponen | File | Baca `paid_amount`? | Hitung `remaining` di klien? | Catatan |
|---|---|---|---|---|
| **RecordPaymentButton** | `components/billing/RecordPaymentButton.tsx` | ❌ | ❌ | terima prop `outstanding` (string server); submit field **`amount`** (bukan `paid_amount`); render `preview.applied[]`/`buyer_credit`/`lines[]` dari server |
| **Collection (Penagihan)** | `app/(app)/accounting/collection/page.tsx` → `components/accounting/CollectionView.tsx` | ❌ | ❌ | baca `row.paid`, `row.outstanding` (server) |
| **Customer Statement** | `app/(app)/accounting/receivable/[contractId]/page.tsx` → `components/accounting/CustomerStatementView.tsx` | ❌ | ❌ | baca `s.total_paid`, `s.remaining_balance` (server); `StatementScheduleLine` tak punya `paid_amount` |
| **Invoice** | `components/billing/InvoiceList.tsx`, `AllInvoiceList.tsx` | ❌ | ❌ | hanya `amount` + `status`; decoupled dari pembayaran |
| **Unit detail — tabel cicilan** | `app/(app)/proyek/[id]/unit/[unitId]/page.tsx:362-394` | ✅ **`s.paid_amount`** | ✅ **`remainingExact()`** (BigInt) | **SATU-SATUNYA** tempat klien membaca `paid_amount` & menghitung sisa |
| AR Aging / UnitSalePanel | `ARAgingView.tsx`, `UnitSalePanel.tsx` | ❌ | ❌ | semua field server (`paid`, `outstanding`, `remaining_balance`) |

**Endpoint payment yang dipakai FE** (`lib/api/`):
`GET/POST /collections/payment[/preview]`, `POST /units/{id}/termins`, `POST /schedules/{id}/received`,
`GET /sale-contracts/{id}/{balance,statement,invoices,schedule}`, `POST /sale-contracts/{id}/invoices`,
`POST /termins/{id}/receipt`, `GET /receipts/{id}/print`, `GET /ledger/accounts/cash-bank`, `GET /invoices`.

### Komponen yang "harus pindah ke allocations"
- **Wajib**: `unit/[unitId]/page.tsx` `remainingExact(s.amount, s.paid_amount)` — satu-satunya perhitungan sisa
  sisi-klien. Setelah FE-2, `paid_amount` menjadi cache yang **direkonsiliasi** dari sub-ledger; nilai yang
  ditampilkan sebaiknya tetap dari server (`amount − paid_amount`) — cukup pastikan `paid_amount` konsisten.
- **Opsional (nilai tambah)**: Statement & Unit detail dapat menampilkan **breakdown alokasi** per cicilan
  ("dibayar oleh KWT/2026/000123 sebesar Rp x pada tgl y") begitu sub-ledger tersedia — sesuatu yang **mustahil**
  hari ini karena datanya tidak dipersist.

---

## 3. Rencana Migration FE-2 (BELUM dibuat — hanya rancangan)

### 3.1 Tabel `payment_allocations`

```sql
CREATE TABLE payment_allocations (
    id                  BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    tenant_id           BIGINT UNSIGNED NOT NULL,
    termin_payment_id   BIGINT UNSIGNED NOT NULL,           -- sumber uang (1 jurnal)
    payment_schedule_id BIGINT UNSIGNED NULL,               -- target cicilan; NULL = buyer credit (unapplied)
    allocation_type     VARCHAR(20)     NOT NULL DEFAULT 'schedule', -- 'schedule' | 'buyer_credit'
    amount              DECIMAL(20,4)   NOT NULL DEFAULT '0.0000',
    created_by          BIGINT UNSIGNED NULL,
    created_at          DATETIME(3),
    updated_at          DATETIME(3),

    INDEX idx_pa_tenant      (tenant_id),
    INDEX idx_pa_termin      (tenant_id, termin_payment_id),
    INDEX idx_pa_schedule    (tenant_id, payment_schedule_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='Sub-ledger alokasi termin→cicilan; TIDAK memposting jurnal (Invariant #1/#5)';
```

**Rasional `allocation_type` + `payment_schedule_id NULL`:** memberi rumah pada **saldo kredit buyer**
(overpay pra-BAST) yang hari ini tak punya baris. Baris `buyer_credit` = sisa `unapplied` dari `planAllocation`.
Ini membuat invariant §3.3 berlaku untuk **setiap rupiah** pembayaran.

### 3.2 Foreign key — pilihan

Skema existing **tidak** memakai `FOREIGN KEY` fisik (hanya INDEX + scope aplikasi). Dua opsi:
- **A (ikut konvensi, rekomendasi):** logical FK via index saja. Integritas dijaga di service + integration test.
- **B (lebih ketat):** `FOREIGN KEY (termin_payment_id) REFERENCES termin_payments(id) ON DELETE RESTRICT`
  untuk menegakkan append-only (ledger tak boleh dihapus, Invariant #5). Trade-off: menyimpang dari konvensi.

> Rekomendasi: **Opsi A** agar konsisten dengan 24 migrasi sebelumnya, dengan test integrasi yang
> membuktikan tak ada alokasi yatim & tak ada kebocoran tenant (Invariant #6).

### 3.3 Constraint / Invariant baru (ditegakkan di service + test, bukan hanya DDL)

1. **Konservasi pembayaran:** `Σ(payment_allocations.amount WHERE termin_payment_id = X) == termin_payments.amount`
   untuk setiap X — setiap rupiah teralokasi (ke cicilan atau ke `buyer_credit`). Selaras Invariant #3.
2. **Rekonsiliasi cache:** `payment_schedules.paid_amount == Σ(alloc.amount WHERE payment_schedule_id = sch AND type='schedule')`.
   `paid_amount` berubah dari "sumber kebenaran" menjadi "cache yang bisa direkonsiliasi".
3. **No-journal:** penulisan `payment_allocations` **tidak pernah** memanggil `PostJournal`. Ditulis dalam
   transaksi yang sama dengan `UpdateSchedulePaidAmount` (`collection.go:406` & `:494`).
4. **Tenant scope:** `tenant_id` di setiap baris + GORM global scope (Invariant #6).
5. **Amount > 0** untuk tiap baris (largest-remainder tak menghasilkan nol; baris nol tidak dibuat).

### 3.4 Index
`idx_pa_tenant`, `idx_pa_termin (tenant_id, termin_payment_id)`, `idx_pa_schedule (tenant_id, payment_schedule_id)`
— mendukung query "alokasi sebuah termin" dan "alokasi sebuah cicilan".

### 3.5 Rollback (`.down.sql`)

```sql
DROP TABLE IF EXISTS payment_allocations;
```

Bersih: `payment_schedules.paid_amount` sudah ada sejak `000022` (mendahului FE-2), jadi rollback tidak
menyentuh kolom lain. Tidak ada perubahan destruktif pada tabel existing.

### 3.6 Backfill data lama (KEPUTUSAN DESAIN — perlu persetujuan)

Termin lama belum punya baris alokasi. Tiga strategi:
- **B1 — Replay best-effort:** untuk tiap kontrak, jalankan ulang `planAllocation` atas termin **urut tanggal**
  → hasilkan baris alokasi. Akurat untuk cicilan lunas; untuk partial hanya *plausible*, bukan histori asli.
- **B2 — Rekonstruksi dari yang pasti saja:** buat baris hanya untuk cicilan **lunas** (punya `termin_payment_id`).
  Partial dibiarkan tanpa baris → invariant §3.3 **tidak** berlaku surut untuk data lama.
- **B3 — Cut-over:** invariant berlaku hanya untuk pembayaran **baru**; data lama ditandai `legacy`, tidak di-backfill.

> Rekomendasi: **B1** dalam migrasi data terpisah + laporan rekonsiliasi (`log` selisih). Jika ada kontrak
> yang tidak balance saat replay, **jangan buang selisih** — tandai untuk review manual (Invariant #3 spirit).
> **Butuh keputusan Anda sebelum implement.**

---

## 4. API Contract Final

**Prinsip: tidak ada flow kedua. `recordTerminEntry` tetap satu-satunya primitif pemosting jurnal.**

### 4.1 `POST /collections/payment` (dan semua adapter) — TETAP 1 jurnal
- Body **tidak berubah**: `{contract_id, amount, date, bank_account_code, reference?, notes?, idempotency_key}`.
- Efek: `ReceivePayment` → `recordTerminEntry` (1 jurnal balanced) → **lalu tulis baris `payment_allocations`**
  di step alokasi (side-effect sub-ledger, **tanpa jurnal**).
- Response boleh diperkaya (opsional) dengan `allocation_ids`, tapi field lama tetap kompatibel.

### 4.2 Alokasi
- **Tidak membuat jurnal.** Ditulis di `applyToSchedules` / `applyToSingleSchedule`, satu transaksi dengan
  update `paid_amount`. Baris `buyer_credit` untuk `unapplied`.
- **Bukan endpoint publik penulis** — tidak ada `POST /allocations` yang berdiri sendiri. Alokasi hanyalah
  konsekuensi internal dari `ReceivePayment`. (Mencegah "flow kedua".)

### 4.3 Endpoint baca baru (read-only, opsional untuk FE)
- `GET /termins/{id}/allocations` → daftar alokasi sebuah pembayaran.
- `GET /sale-contracts/{id}/allocations` → sub-ledger untuk statement/unit detail.
- `GET /collections/payment/preview` **tetap** — sudah mengembalikan `applied[]` + `buyer_credit`.

### 4.4 Guard anti-flow-kedua (wajib di-test)
- Handler **tidak boleh** memanggil `recordTerminEntry` atau `PostJournal` langsung.
- Test: setelah `ReceivePayment`, jumlah `journal_entries` bertambah **tepat 1** per pembajaran, dan
  `Σ payment_allocations.amount == termin.amount`.

---

## 5. Risiko yang harus dicek (checklist sebelum implement)

| # | Risiko | Kondisi | Yang harus dijamin |
|---|---|---|---|
| R1 | **Pembayaran lama** | termin pra-FE-2 tanpa baris alokasi | Pilih strategi backfill (§3.6). Invariant baru tidak boleh menyalahkan histori yang tak bisa direkonstruksi |
| R2 | **Partial payment** | `paid_amount > 0`, `termin_payment_id = NULL` | Setelah FE-2: tiap termin partial menulis baris alokasi; `paid_amount` = Σ alokasi. Cicilan bisa dibayar oleh >1 termin |
| R3 | **Overpayment (buyer credit)** | pra-BAST, `unapplied > 0` | Tulis baris `allocation_type='buyer_credit'` (`payment_schedule_id NULL`). Konservasi §3.3.1 berlaku. Pasca-BAST overpay tetap ditolak (`ErrPaymentExceedsReceivable`) — belum diubah di FE-2 |
| R4 | **Pra-BAST** | credit → `2-2000` (Uang Muka) | Alokasi tidak mengubah jurnal; routing tetap di `recordTerminEntry`. Uang Muka ≠ pendapatan (Invariant #7) |
| R5 | **Pasca-BAST** | credit → `1-2000` (Piutang) | Sama; alokasi hanya sub-ledger. Sisa piutang tetap `Gross − Σtermin` |
| R6 | **Invoice sudah/belum dibuat** | invoice on-demand per cicilan | Alokasi **independen** dari invoice. `MarkPaidByScheduleID` tetap dipicu saat cicilan lunas, terlepas ada/tidaknya invoice |
| R7 | **Idempotency** | double-submit `idempotency_key` sama | Hit idempoten harus **tidak** membuat baris alokasi baru (return `existingPaymentResult`). Uji: 2× submit → 1 termin, 1 set alokasi |
| R8 | **Rekonsiliasi cache drift** | `paid_amount` vs Σ alokasi | Integration test membuktikan keduanya identik setelah tiap operasi |
| R9 | **Tenant isolation** | query lintas tenant | `tenant_id` + GORM global scope; test lintas-tenant → 0 baris (Invariant #6) |
| R10 | **Atomicity** | jurnal sukses, alokasi gagal (atau sebaliknya) | Alokasi + `paid_amount` dalam satu transaksi. Definisikan: jika alokasi gagal setelah jurnal ter-post, apakah rollback termin? (jurnal append-only → mungkin butuh kompensasi/retry). **Perlu keputusan** |
| R11 | **Whole-rupiah / pembulatan** | `planAllocation` waterfall | Konservasi eksak: `Σ apply + unapplied == amount`. Tidak ada rupiah hilang (Invariant #3 spirit) |

---

## 6. Keputusan yang menunggu persetujuan Anda (sebelum coding)

1. **Strategi backfill** (§3.6): B1 replay / B2 lunas-saja / B3 cut-over. → *rekomendasi B1*.
2. **FK fisik vs logical** (§3.2): Opsi A (ikut konvensi) / B (FK ketat). → *rekomendasi A*.
3. **Buyer credit sebagai baris `payment_allocations`** vs tabel terpisah `buyer_credits`. → *rekomendasi: baris di
   `payment_allocations` dulu (lebih sederhana, konservasi 1 tabel); pisah bila butuh lifecycle credit sendiri
   (refund/pemakaian) di fase berikut*.
4. **R10 atomicity/kompensasi**: perilaku bila alokasi gagal pasca-jurnal-terpost.

> **STOP di sini.** Tidak ada migration/kode dibuat. Menunggu review & keputusan §6 sebelum implement FE-2.
