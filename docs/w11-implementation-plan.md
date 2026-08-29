# W-11 — IMPLEMENTATION PLAN (file-by-file)

**Status:** RENCANA. Belum ada kode, belum ada migrasi, belum ada mutasi data.
**Tanggal:** 2026-08-14
**Source of truth:** `docs/w11-final-decision-lock.md` (D-1 … D-20). `docs/w11-hutang-usaha-design.md` = bukti/analisis.
**Basis audit:** hanya kode yang benar-benar disentuh W-11 (hasil di §0).

---

## 0. HASIL AUDIT TARGET FILES

Yang dibaca ulang, dan apa yang berubah dari asumsi decision lock:

| Temuan | Bukti | Akibat pada rencana |
|---|---|---|
| **Migrasi terakhir = `000072_expense_types`** | `backend/migrations/` | Migrasi W-11 = **`000073`**. Satu migrasi, bukan beberapa |
| **Pola transaksi kanonik** = service pegang `*gorm.DB`, buka `db.WithContext(ctx).Transaction(...)`, di dalamnya bangun `ledger.NewPostingService(txLedger, txLedger).WithPeriodChecker(txLedger)` | `internal/legacyar/payment.go:90-190`, `internal/charge/service.go` (15 tempat) | `internal/ap` mengikuti pola ini persis. **Tidak** memakai `cost.TxRunner` |
| **`legacyar.ReceivePayment` adalah cetakan langsung untuk pembayaran AP** — idempotensi dicek di luar tx, row-lock via `FindReceivable(..., true)`, sisa dihitung dari **sub-ledger** bukan kolom cache, dokumen diambil `document.FindByJournal` setelah `CreateAndPost` | `internal/legacyar/payment.go:67-240` | Rencana pembayaran AP menyalin struktur ini, arah kas dibalik (BKK, bukan BKM) |
| **`cost.CostEntry.JournalEntryID` NOT NULL + FK ke `journal_entries`** | `SHOW CREATE TABLE cost_entries` | Skenario A sah: jurnal **draft** sudah punya baris `journal_entries`, jadi `cost_entries` boleh ditulis lebih dulu di tx yang sama |
| **3 CHECK constraint aktif di `cost_entries`**: `chk_ce_tier_category`, `chk_ce_tier_project`, `chk_ce_tier_unit` | idem | Baris AP wajib lolos matriks tier×kategori yang sama. **Tidak boleh** divalidasi ulang secara terpisah di `ap` |
| **Validasi baris biaya terpusat di `cost.Service.plan()` + `validate()`** (~110 baris: tier×kategori, unit milik proyek lain, budget item, product catalog, INV-EXP-2) | `internal/cost/service.go:259-433` | ⚠️ **Keputusan desain baru (A-1)** — lihat §2.1. AP **tidak boleh** menyalin validasi ini |
| **`isCostTaxonomyAccount` diturunkan dari `domain.AllCostCategories` + `ExpenseCostCategories`**, bukan daftar literal | `internal/cost/accounts.go:37-52` | AP dapat menurunkan himpunan yang sama **langsung dari `domain`** untuk menjaga INV-AP-17 — tanpa menyentuh `cost` |
| **`RolePayable` = `{2-1000}`, dikonsumsi 2 tempat**: `cost/accounts.go:19` dan **`reporting/dashboard.go:234` (`KPI.Payable`)** | `grep RolePayable` | ⚠️ **Risiko baru R-11** — lihat §7. Retensi tidak akan muncul di KPI Hutang kecuali ditangani |
| **`RoleVATInput` = `{1-5100}` sudah ada**, dikonsumsi `balance_service.go:48` (RoleBalances) | `grep RoleVATInput` | PPN Masukan langsung terbaca `RoleBalances` tanpa perubahan apa pun |
| **`RequireApproved` dipakai lewat adapter sempit per package** (`budget/handler.go:44`, `cancellation/handler.go:36`, `commission/handler.go:30`) | grep | `ap` memakai pola yang sama dengan `approval.TargetCost` |
| **`receivable.bucketFor` unexported**, `BucketCurrent..Bucket90Plus` sudah exported | `receivable.go:117` | Cukup ekspor **satu fungsi**; konstanta bucket sudah publik |
| **Penambahan akun COA untuk tenant existing = INSERT…SELECT idempoten meng-clone atribut dari akun sejenis** | `migrations/000056_notary_deposit.up.sql:7-13` | Cetakan langsung untuk 2-1100 (clone dari 2-1000) dan 1-5300 (clone dari 1-5000) |
| **Routing**: chi, `xxxHandler := xxx.NewHandler(gdb)` lalu `xxxHandler.Mount(r)` di grup ber-JWT | `cmd/api/main.go:164-210` | 2 baris perubahan di `main.go` |
| **Guard Tahap 1 hidup di transport**, bukan domain: `handler.go:329` menolak `payment_method=payable`; `PaymentMethodPayable` & `resolveDebitCreditCodes` tetap utuh | `internal/cost/handler.go:318-330` | Sesuai perintah: dukungan domain tidak dihapus, guard transport dipertahankan |
| **Frontend**: `lib/api/<modul>.ts` + `app/(app)/accounting/<modul>/page.tsx`, menu di `components/layout/Sidebar.tsx` grup `KEUANGAN` | — | Tidak perlu route proxy baru (`app/api/` hanya untuk auth & csv) |

**Tidak ditemukan contradiction terhadap D-1..D-20.** Satu ketegangan redaksional dan dua celah desain baru dilaporkan di §7 (R-11, R-12, A-1).

---

## 1. FILE YANG AKAN BERUBAH

### 1.1 Package baru — `backend/internal/ap/` (semua file BARU)

| File | Isi | ~baris |
|---|---|---:|
| `model.go` | `Vendor`, `Invoice`, `Payment`, `Allocation`, enum `PaymentKind`, `AllocationType`, konstanta `SourceAPInvoice/Payment/Advance/Retention` | 180 |
| `errors.go` | `ErrVendorNotFound`, `ErrOverpayment`, `ErrExcessNotAllowed`, `ErrAlreadySettled`, `ErrHasAllocations`, `ErrFakturRequired`, `ErrPPNProjectTagged`, `ErrAdvanceExhausted`, `ErrIdempotencyConflict`, … | 120 |
| `repository.go` | `GORMRepository` + `WithTx(tx)`, `accountByCode`, CRUD vendor/invoice/payment/allocation, `SumAllocations`, `SumAdvanceUnapplied`, `FindInvoice(..., lock bool)` | 380 |
| `vendor.go` | `CreateVendor`, `UpdateVendor`, `ListVendors` — tanpa jurnal | 140 |
| `invoice.go` | `PreviewInvoice`, `RecordInvoice` (draft), `PostInvoice` (gate → periode → Post), `ReverseInvoice` | 420 |
| `journal.go` | Komposisi baris jurnal pengakuan: debit taksonomi per baris + `1-5100` + kredit `2-1000`/`2-1100`/`1-5300`. Satu-satunya tempat aturan PPN-1..PPN-6 hidup | 220 |
| `payment.go` | `PreviewPayment`, `RecordPayment` (incl. `allow_excess_as_advance`), `RecordAdvance`, `ReleaseRetention`, `ReversePayment` | 480 |
| `aging.go` | `BuildAPAging` memakai `receivable.BucketFor`; struct **milik AP** (vendor-shaped) | 180 |
| `query.go` | `GetInvoice` (outstanding turunan), `ListInvoices`, `ListUnappliedAdvances`, `TieOut` (AP↔GL) | 280 |
| `handler.go` | chi `Mount`, DTO, adapter `approvalGate`, `NewHandler(gdb)` | 520 |
| `service.go` | Konstruktor `Service`, wiring dependensi, seam interface | 160 |

Test (BARU): `invoice_test.go`, `payment_test.go`, `aging_test.go`, `journal_test.go` (unit) + `ap_integration_test.go`, `ap_ppn_integration_test.go`, `ap_concurrency_integration_test.go`, `ap_tenant_isolation_test.go` (`-tags integration`).

### 1.2 File existing yang DISENTUH — semuanya aditif

| File | Perubahan | Kenapa aman |
|---|---|---|
| `internal/ledger/coa.go` | +2 entri di `defaultCOA`: `2-1100`, `1-5300` | `SeedCOA` idempoten (`FirstOrCreate` per (tenant,code)); tenant baru langsung dapat |
| `internal/ledger/account_role.go` | +2 konstanta `RoleRetentionPayable`, `RoleVendorAdvance` dan 2 entri `roleCodes` | Map lookup; peran baru tidak mengubah keanggotaan peran lama |
| `internal/ledger/balance_service.go` | +2 peran di daftar `RoleBalances` (`:48-49`) | Aditif — menambah key pada map hasil, tidak mengubah nilai key lain |
| `internal/receivable/receivable.go` | `bucketFor` → **`BucketFor`** (ekspor), 5 call-site internal disesuaikan | Rename murni, logika identik; `receivable_test.go` tetap hijau |
| `internal/cost/model.go` | +2 field: `VendorID *uint64`, `APInvoiceID *uint64` (nullable, `json:",omitempty"`) | Tidak mengubah field lama; baris historis NULL |
| `internal/cost/service.go` | **+1 tipe & +1 method exported** (`CostLinePlan`, `PlanAPLine`) yang membungkus `plan()` — lihat A-1 §2.1 | Membungkus, bukan mengubah. `plan()`/`validate()`/`resolveDebitCreditCodes` tidak disentuh |
| `cmd/api/main.go` | +1 konstruktor, +1 `Mount` | Pola identik 20 modul lain |
| `migrations/000073_ap.up.sql` / `.down.sql` | BARU | §3 |

### 1.3 Frontend (BARU kecuali dua terakhir)

`app/(app)/accounting/hutang/page.tsx` (dashboard + daftar + aging) · `hutang/baru/page.tsx` (form tagihan, 2 langkah) · `hutang/[id]/page.tsx` (detail + riwayat bayar) · `accounting/vendor/page.tsx` · `components/ap/APInvoiceForm.tsx` · `APLineTable.tsx` · `APPaymentModal.tsx` (Input → Pratinjau → Simpan) · `APAgingTable.tsx` · `VendorForm.tsx` · `UnappliedAdvancePanel.tsx` · `lib/api/ap.ts` · `lib/types/api.ts` (+tipe AP) · `components/layout/Sidebar.tsx` (+2 entri grup KEUANGAN).

Warna hanya lewat token design system (`--chart-1..6`, ink) — nol kelas warna tailwind mentah.

---

## 2. KEPUTUSAN DESAIN YANG PERLU PERSETUJUAN ANDA

### 2.1 A-1 — Bagaimana `internal/ap` memvalidasi baris biaya

**Masalah nyata.** D-4 mewajibkan baris biaya AP mendarat di `cost_entries`. Tetapi seluruh aturan yang membuat baris `cost_entries` sah hidup di `cost.Service.plan()`/`validate()`: matriks tier×kategori, unit harus milik proyek yang sama, kategori produk fail-closed, tautan budget item, INV-EXP-2, dan 3 CHECK constraint DB. Menyalinnya ke `ap` berarti **dua definisi aturan biaya** — persis pola yang C-1 lahir untuk mencegah.

| Opsi | Konsekuensi |
|---|---|
| **(a) — REKOMENDASI.** Ekspor pembungkus tipis dari `cost`: `PlanAPLine(ctx, tenantID, req) (CostLinePlan{Tier, DebitCode}, error)` yang memanggil `plan()` apa adanya dengan `PaymentMethod=payable`. `internal/ap` import `internal/cost` untuk tipe `CostEntry` + pembungkus ini | Satu definisi aturan. Tambah ±25 baris di `cost`, nol perubahan logika. Edge import baru `ap → cost` (tidak ada siklus: `cost` tidak import `ap`) |
| (b) Duplikasi validasi di `ap` | Dua definisi yang akan menyimpang. Ditolak |
| (c) Taruh AP di dalam `internal/cost` | Package cost jadi 2× lipat dan mencampur biaya proyek dengan hutang vendor. Ditolak |

**Butuh persetujuan Anda** karena menambah baris exported pada `internal/cost` — package yang tidak ada di daftar terlarang, tapi juga tidak disebut akan berubah di decision lock.

### 2.2 A-2 — Nilai PPN: input, bukan hitungan

`ppn_amount` **diisi user** (dari faktur pajak vendor), tidak dihitung sistem dari tarif. Alasan: `TODO(tax-advisor)` melarang menebak tarif/kualifikasi, dan nilai di faktur adalah otoritasnya. Sistem hanya memvalidasi `ppn_amount ≥ 0` dan `faktur_pajak_number` wajib bila `> 0` (PPN-5). Frontend boleh menampilkan bantuan hitung 11% sebagai **saran yang bisa ditimpa**, tidak mengikat.

### 2.3 A-3 — Kolom tambahan di tabel BARU

`ap_invoices` perlu `reversal_journal_entry_id BIGINT UNSIGNED NULL` supaya status `reversed` turunan tanpa query balik ke `journal_entries`. Ini kolom pada tabel yang memang baru — **masih di dalam batas "4 tabel"**, bukan kolom tambahan pada tabel existing. Dilaporkan agar tidak terbaca melanggar §9.

---

## 3. MIGRASI — `000073_ap.up.sql`

**Satu migrasi. Tidak dijalankan sebelum Anda approve.**

```sql
-- 1) AKUN COA (idempoten, clone atribut dari akun sejenis — pola 000056)
INSERT INTO accounts (tenant_id, code, name, type, normal_balance, is_system, description, is_active, category)
SELECT a.tenant_id, '2-1100', 'Hutang Retensi Kontraktor', a.type, a.normal_balance, 1,
       'Bagian termin kontraktor yang ditahan sampai masa pemeliharaan selesai (W-11 D-6). '
       'Kewajiban tersendiri: jatuh tempo & syarat pelepasannya berbeda dari 2-1000.',
       1, a.category
FROM accounts a
WHERE a.code = '2-1000'
  AND NOT EXISTS (SELECT 1 FROM accounts b WHERE b.tenant_id = a.tenant_id AND b.code = '2-1100');

INSERT INTO accounts (tenant_id, code, name, type, normal_balance, is_system, description, is_active, category)
SELECT a.tenant_id, '1-5300', 'Uang Muka Vendor', a.type, a.normal_balance, 1,
       'Pembayaran di muka ke vendor sebelum pekerjaan diakui (W-11 D-8). BUKAN biaya dan '
       'BUKAN realisasi RAB sampai dikompensasi ke termin.',
       1, a.category
FROM accounts a
WHERE a.code = '1-5000'
  AND NOT EXISTS (SELECT 1 FROM accounts b WHERE b.tenant_id = a.tenant_id AND b.code = '1-5300');

-- 2) EMPAT TABEL BARU: vendors, ap_invoices, ap_payments, ap_payment_allocations
--    (kolom persis §D.1 decision lock + A-3; uang DECIMAL(20,4);
--     tenant_id + index wajib; created_at/updated_at DATETIME(3))

-- 3) DUA KOLOM NULLABLE pada cost_entries — tanpa backfill
ALTER TABLE cost_entries
  ADD COLUMN vendor_id     BIGINT UNSIGNED NULL,
  ADD COLUMN ap_invoice_id BIGINT UNSIGNED NULL,
  ADD INDEX idx_ce_vendor (tenant_id, vendor_id),
  ADD INDEX idx_ce_ap_invoice (tenant_id, ap_invoice_id),
  ADD CONSTRAINT fk_ce_vendor     FOREIGN KEY (vendor_id)     REFERENCES vendors(id)     ON DELETE SET NULL,
  ADD CONSTRAINT fk_ce_ap_invoice FOREIGN KEY (ap_invoice_id) REFERENCES ap_invoices(id) ON DELETE SET NULL;
```

**`000073_ap.down.sql`**: drop 2 FK + 2 index + 2 kolom, drop 4 tabel (urutan alokasi→payment→invoice→vendor), hapus akun `2-1100`/`1-5300` **hanya bila tak ada baris jurnal yang memakainya** (fail-safe, tidak memaksa).

**Nol baris data existing yang diubah.** `UPDATE`/`DELETE` atas data bisnis: tidak ada.

---

## 4. ENDPOINT

| Method | Path | Baru/Ubah | Catatan |
|---|---|---|---|
| GET/POST | `/api/v1/ap/vendors` | BARU | |
| PATCH | `/api/v1/ap/vendors/{id}` | BARU | nonaktifkan, bukan hapus |
| GET | `/api/v1/ap/invoices` | BARU | filter vendor/proyek/status/jatuh tempo |
| POST | `/api/v1/ap/invoices/preview` | BARU | fungsi komposisi jurnal yang **sama** dengan post |
| POST | `/api/v1/ap/invoices` | BARU | draft (skenario A) |
| GET | `/api/v1/ap/invoices/{id}` | BARU | outstanding turunan |
| POST | `/api/v1/ap/invoices/{id}/post` | BARU | gate → `IsPeriodClosed` → `Post` |
| POST | `/api/v1/ap/invoices/{id}/reverse` | BARU | tolak bila ada alokasi non-terbalik |
| POST | `/api/v1/ap/payments/preview` | BARU | alokasi otomatis tertua-dulu |
| POST | `/api/v1/ap/payments` | BARU | `Idempotency-Key`; **422** bila lebih bayar tanpa flag |
| POST | `/api/v1/ap/payments/{id}/reverse` | BARU | |
| POST | `/api/v1/ap/advances` | BARU | Dr 1-5300 / Cr kas, BKK |
| POST | `/api/v1/ap/retentions/{id}/release` | BARU | Dr 2-1100 / Cr kas, BKK |
| GET | `/api/v1/ap/aging` | BARU | |
| GET | `/api/v1/ap/tie-out` | BARU | sub-ledger ↔ GL per source (INV-AP-1) |

**Tidak ada endpoint existing yang berubah.** `POST /costs` tetap menolak `payment_method=payable` (D-15).

---

## 5. INVARIANT YANG DIJAGA (dan di mana)

| Invariant | Ditegakkan di | Mekanisme |
|---|---|---|
| INV-AP-1 tie-out AP↔GL | `query.go` | `AccountMovementBySource` per source |
| INV-AP-2 Σ alokasi ≤ nilai | `payment.go` | row lock `FOR UPDATE` + hitung dari sub-ledger |
| INV-AP-3/4 pembayaran ≠ RAB | `journal.go` | baris pembayaran hanya 2-1000/2-1100/1-5300/kas — nol akun taksonomi |
| INV-AP-5 satu kas satu BKK | `ledger.enforceDocument` | `DocumentSpec{DocCashOut}` di setiap `CreateAndPost` |
| INV-AP-6 urutan reversal | `invoice.go` | tolak bila ada alokasi non-terbalik |
| INV-AP-7 isolasi tenant | `repository.go` | `WHERE tenant_id = ?` eksplisit di setiap query |
| **INV-AP-8** Σ CE == Σ debit taksonomi | `journal.go` | dihitung dan **di-assert sebelum** `Create` — bukan hanya diuji |
| INV-AP-9 draft tak terlihat | `invoice.go` | `Create` tanpa post; tidak ada kolom status yang bocor |
| INV-AP-10 periode | `invoice.go` | `IsPeriodClosed` existing dipanggil ulang di post (D-9) |
| INV-AP-11 muncul di per-item | `cost.PlanAPLine` + tulis `budget_item_id` | |
| INV-AP-12 sekali hitung | `journal.go` | uang muka hanya memecah sisi kredit |
| INV-AP-13 append-only | `invoice.go` | `PostingService.Reverse` saja; nol DELETE/UPDATE jurnal |
| INV-AP-14 partial tak ubah biaya | `payment.go` | pembayaran tidak menyentuh `cost_entries` |
| INV-AP-15 uang muka ≠ RAB | `payment.go` | `RecordAdvance` tidak menulis `cost_entries` sama sekali |
| INV-AP-16 balanced | `ledger.validateLines` | |
| **INV-AP-17** PPN bukan biaya proyek | `journal.go` | baris 1-5100 di-*assert* tanpa Project/Phase/Unit; himpunan taksonomi diturunkan dari `domain` |
| **INV-AP-18** lebih bayar | `payment.go` | 422 default; 1-5300 hanya via flag eksplisit |
| INV-AP-19 gate opt-in | `handler.go` adapter | `RequireApproved(TargetCost, id)` |

---

## 6. TEST PLAN

**Unit** (tanpa DB): komposisi jurnal 4 bentuk (polos/PPN/retensi/uang muka) — balanced & Σ debit taksonomi == Σ CE · aturan PPN-1..PPN-6 · bucket AP == bucket AR untuk umur yang sama · derivasi status/outstanding · penolakan lebih bayar.

**Integration** (`-tags integration`, **wajib `-p 1`**): skenario A→B (9 pembacaan sebelum/sesudah post) · C/D partial+full · E1/E2/E3 retensi · F→G uang muka (RAB 100 jt **sekali**) · B vs B′ PKP (RAB identik) · H1/H2 reversal + urutan · periode ditutup · dua tenant approval on/off · isolasi tenant lintas AP · konkurensi N goroutine ke satu tagihan · idempotensi kunci sama & kunci sama target beda (409) · tie-out AP↔GL.

**Regresi wajib hijau tanpa perubahan**: `internal/cost/...` (termasuk `payable_guard_test.go`), `internal/receivable/...`, `internal/budget/realisasi_integration_test.go`, `internal/reporting/consistency_integration_test.go`, `internal/ledger/...`.

**Frontend**: Node 22 built-in runner, tanpa dependency baru.

**Browser**: `http://localhost:3005`, bukti `ss -ltn` menunjukkan `127.0.0.1` untuk 3005 & 8085.

---

## 7. RISIKO YANG DITEMUKAN SAAT AUDIT

| # | Risiko | Usulan |
|---|---|---|
| **R-11** | **KPI "Hutang" di dashboard akan understate.** `reporting/dashboard.go:234` mengisi `KPI.Payable` dari `RolePayable` = `{2-1000}` saja. Dengan retensi di 2-1100, total kewajiban vendor tidak lagi sama dengan KPI itu | Tambah KPI **terpisah** "Hutang Retensi" — **jangan** menggabungkan 2-1100 ke `RolePayable` (itu akan mengubah angka historis di seluruh konsumen `RolePayable`, termasuk `cost/accounts.go` yang memakainya untuk memilih akun kredit). Perubahan aditif di `reporting`; `reporting` tidak ada di daftar terlarang. **Perlu konfirmasi Anda** |
| **R-12** | **Konkurensi uang muka.** Dua tagihan yang mengompensasi uang muka yang sama secara bersamaan bisa lolos berdua | Row lock juga pada baris `ap_payments` uang muka, bukan hanya pada `ap_invoices`. Diuji di test konkurensi |
| R-9 | PPN atas pembayaran uang muka vendor PKP (dari decision lock) | Tetap `TODO(tax-advisor)`; default P0 sesuai C.PPN |
| R-5 | `cmd/demo-seed` memanggil `CreateCostEntry` langsung, melewati guard transport | Perbaiki di tahap terakhir; 4 baris payable existing **tidak disentuh** |
| R-6 | 3 test integration `legacyar` gagal sejak sebelum W-11 (F-4) | Tidak diperbaiki (perintah Anda). Dijalankan `-p 1`, dilaporkan sebagai pre-existing |
| — | Ketegangan redaksional: perintah Anda "guard Tahap 1 dipertahankan **sampai jalur AP siap**" vs D-15 "guard dipertahankan **permanen**" | Rencana mengikuti **D-15** (permanen; pintu AP satu-satunya `POST /ap/invoices`). Koreksi bila maksud Anda berbeda |

---

## 8. TAHAPAN EKSEKUSI

Setiap tahap: `go build ./...` → unit test → integration test relevan → audit diff (`git`-less: perbandingan daftar file tersentuh vs §1) → verifikasi nol perubahan di area terlarang.

| # | Tahap | Selesai bila |
|---|---|---|
| 1 | Migrasi + domain/model + role + COA | migrasi naik-turun bersih di DB dev; `SeedCOA` idempoten; regresi ledger hijau |
| 2 | Repository + vendor | CRUD vendor hijau; isolasi tenant hijau |
| 3 | Komposisi jurnal (`journal.go`) + `cost.PlanAPLine` | unit test 4 bentuk jurnal hijau; INV-AP-8 & 17 hijau |
| 4 | Pengakuan: draft → post → reverse | skenario A/B/H1 hijau; INV-AP-9/10/13 hijau |
| 5 | Approval gate | INV-AP-19 hijau (dua tenant) |
| 6 | Pembayaran + alokasi + lebih bayar | C/D + INV-AP-2/5/14/18 hijau; konkurensi & idempotensi hijau |
| 7 | Retensi | E1/E2/E3 hijau |
| 8 | Uang muka + kompensasi | F/G hijau; INV-AP-15 hijau; R-12 tertutup |
| 9 | Aging + tie-out | bucket == AR; INV-AP-1 hijau |
| 10 | HTTP handler + wiring `main.go` | seluruh endpoint terjawab; regresi penuh hijau |
| 11 | Frontend + `lib/api/ap.ts` + menu | `npm test` & `tsc` hijau |
| 12 | Verifikasi browser + adversarial review §12 | bukti `ss -ltn`; 12 titik serang §12 semua tertutup |

---

## 9. DATABASE SAFETY

- Migrasi `000073` **dilaporkan dan menunggu approval sebelum dijalankan** — isinya sudah di §3.
- Verifikasi memakai **DB dev**, tenant uji **baru** yang dibuat khusus (mis. `9911001`), **bukan** tenant produksi, **bukan** LombokGuru.
- Test integration memakai transaksi + rollback / tenant terisolasi per test — pola `-p 1` yang sudah dipakai repo.
- **Tidak ada transaksi bisnis nyata** yang dibuat tanpa approval Anda.
- Cleanup proses: PID/service spesifik, **tanpa `pkill` berbasis pola**.

---

## 10. KONFIRMASI HARD CONSTRAINTS

| Constraint | Status dalam rencana |
|---|---|
| Jangan sentuh `internal/budget` | ✅ nol file |
| Jangan ubah `ledger/actual_cost.go` | ✅ nol perubahan |
| Jangan ubah semantics W-5/W-6 | ✅ `receivable`, `histfin` — hanya 1 rename ekspor di `receivable`, logika identik |
| Jangan sentuh `charge` | ✅ nol file |
| Jangan sentuh `legacyar` | ✅ nol file (hanya **dibaca** sebagai cetakan) |
| Jangan sentuh `tax` untuk PPh | ✅ nol file; PPh tidak diimplementasikan |
| Jangan buat `cost_entries.reversed_at` | ✅ tidak ada di migrasi |
| Jangan buat jenis dokumen baru | ✅ hanya BKK & JR existing |
| Jangan buat workaround W-11b | ✅ asimetri reversal dibiarkan apa adanya |
| Jangan migrasi data existing | ✅ hanya INSERT akun baru (idempoten) + ALTER kolom nullable |
| Pertahankan domain `payment_method=payable` + guard Tahap 1 | ✅ `PaymentMethodPayable` & `resolveDebitCreditCodes` tidak disentuh; guard `handler.go:329` tetap |
| 2 akun COA | ✅ 2-1100, 1-5300 |
| 4 tabel | ✅ vendors, ap_invoices, ap_payments, ap_payment_allocations |
| 2 kolom nullable di tabel existing | ✅ `cost_entries.vendor_id`, `.ap_invoice_id` |
| Perubahan aditif pada package existing | ✅ 7 file, semuanya aditif (§1.2) |

---

**Menunggu approval Anda untuk 3 hal sebelum Tahap 1 dieksekusi:**
1. **A-1** — ekspor `cost.PlanAPLine` (agar aturan baris biaya tetap satu definisi).
2. **R-11** — KPI Hutang Retensi terpisah di dashboard (agar total kewajiban vendor tidak understate).
3. **Migrasi `000073`** — isinya sudah tertulis lengkap di §3.
