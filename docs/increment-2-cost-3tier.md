# Increment 2 — Cost 3-Tier (Direct / Shared / Overhead)

> Fase kedua implementasi CORE. Blueprint: `blueprint-domain-additions.md` §7 (CORE).
> Menutup CFO gap **G2** (marketing spend/CAC — jalur expense yang selama ini
> deferred di P1-1) dan menyiapkan fondasi **G1** (laba bersih per proyek —
> read-model-nya DITUNDA ke increment lain, lihat Non-goals).
> **BCA frozen spec tidak berubah**: pool HPP tetap `{land, hard, soft, financing}`.

## Keputusan yang dikunci (disetujui sebelum implementasi)

1. **Overhead boleh ber-project ATAU tanpa project.** `project_id` untuk tier
   overhead adalah cost-center tag reporting (opsional); tier direct/shared tetap
   wajib project (aggregate-root rule §4 blueprint review).
2. **Backfill deterministik:** `unit_id != NULL → direct`, `unit_id == NULL → shared`
   (semua entri lama adalah kapitalisasi; tidak ada yang menjadi overhead).
3. **Read-model laba bersih per proyek DITUNDA** — increment ini murni write-path
   + posting.
4. **`CostTier` = typed constant SSOT** (`internal/domain/cost_tier.go`) — tidak ada
   literal string tier tersebar di package lain.
5. **Validasi matriks CostTier × CostCategory** — kategori kapitalisasi hanya untuk
   Direct/Shared; Marketing/Other hanya untuk Overhead; kombinasi lain ditolak.

## Ruang lingkup

| Tier | Kategori sah | Posting (Dr / Cr) | unit_id | project_id |
|---|---|---|---|---|
| **direct** | land, hard, soft, financing | Dr Persediaan 1-3xxx / Cr Bank\|Hutang | **WAJIB** | wajib |
| **shared** | land, hard, soft, financing | Dr Persediaan 1-3xxx (pool) / Cr Bank\|Hutang | harus NULL | wajib |
| **overhead** | marketing, other | Dr **Beban 5-3000/5-4000** / Cr Bank\|Hutang | harus NULL | **opsional** |

- Overhead **tidak pernah** menyentuh Persediaan/HPP; pool alokasi (query 1-3xxx)
  terbukti bersih via integration test.
- Alokasi Shared→Unit tetap memakai engine BCA existing (tidak berubah); increment
  ini hanya memformalkan tier-nya.
- Alokasi Overhead→Project (management-only, laba bersih) = read-model, DITUNDA.

## Perubahan Database (migration 000035, reversible, ADD-only)

| Objek | Keterangan |
|---|---|
| `cost_entries.cost_tier` | VARCHAR(20) NOT NULL (`direct\|shared\|overhead`), + index `(tenant_id, cost_tier)`. Backfill: `unit_id IS NULL → 'shared'`, else `'direct'`. |
| `cost_entries.project_id` | NOT NULL → **NULLABLE** (NULL hanya sah untuk overhead Tenant-level; ditegakkan service). FK `fk_ce_project` tetap. |

Down migration: hapus baris `project_id IS NULL` (overhead Tenant-level), lepas-pasang
FK untuk mengembalikan NOT NULL (MySQL Error 1832), drop index + kolom. Jurnal beban
yang sudah diposting TIDAK dihapus (append-only, Invariant #5). Reversibilitas diuji
di DB dev (down→up bersih, backfill konsisten).

### Hardening — migration 000036 (CHECK constraints, ditambah pasca-review)

Defense-in-depth di level database (MySQL 8.0.16+ meng-enforce CHECK) agar direct
write yang mem-bypass service tidak bisa melanggar invariant tier:

| Constraint | Aturan |
|---|---|
| `chk_ce_tier_valid` | `cost_tier IN (direct, shared, overhead)` |
| `chk_ce_tier_project` | direct/shared wajib `project_id`; hanya overhead boleh NULL |
| `chk_ce_tier_unit` | direct wajib `unit_id`; shared/overhead harus NULL |
| `chk_ce_tier_category` | kapitalisasi hanya direct/shared; marketing/other hanya overhead |

Diverifikasi: 5 kombinasi bypass via SQL langsung ditolak MySQL; down/up reversible;
seluruh suite `-tags integration` tetap hijau dengan constraint aktif. Service tetap
lapisan validasi utama (error bertipe); CHECK hanya jaring pengaman terakhir.

## Domain & Taxonomy (SSOT)

- `domain.CostTier` — konstanta bertipe + `Valid()`, `Capitalizes()`,
  `AllowsCategory()` (matriks di satu tempat).
- `domain.CostCategory` diperluas: `marketing`, `other` + `IsCapitalizable()`,
  `IsExpense()`, `ExpenseAccountCode()` (marketing→5-3000, other→5-4000; pemetaan
  akun beban SSOT-nya kini di domain). **`AllCostCategories` tetap TEPAT 4**
  kategori kapitalisasi — snapshot/alokasi/HPP tidak berubah (BCA-1 aman).
- `budget.BudgetCategory.ExpenseAccountCode()` → delegasi ke domain.
- `budget.BudgetCategory.CostEntryCategory()` (baru) — pemetaan realisasi PENUH
  6 kategori (construction→hard, marketing→marketing, other→other);
  `ToCostCategory()` tetap khusus kapitalisasi (gate pool HPP tidak berubah).

## Service & Workflow

- **Inferensi tier (backward compat):** request tanpa `cost_tier` di-infer dengan
  aturan yang SAMA dengan backfill: kategori beban → overhead; unit terisi →
  direct; selain itu → shared. API/UI lama tetap berfungsi tanpa perubahan.
- **Validasi** (urutan): tier valid → kategori valid → matriks tier×kategori →
  konsistensi unit (direct wajib unit; shared/overhead tolak unit) → project
  (direct/shared wajib; overhead tanpa project menolak `phase_id`/`budget_item_id`)
  → amount/payment → tautan RAB (kategori harus cocok; kini termasuk marketing/other).
- **Posting:** debit mengikuti tier (`InventoryAccountCode` vs `ExpenseAccountCode`);
  kredit tetap Bank/Hutang Usaha. Deskripsi jurnal: `"Kapitalisasi …"` vs `"Beban …"`.
  Draft→Post dua langkah tidak berubah. Preview == Create (jalur resolusi sama).
- **Realisasi RAB:** `GetRABvsRealisasi` kini mengisi baris marketing/other dari
  cost entry tier overhead (posted-only, Decision A tetap) — dead-end P1-2 tertutup;
  `budget_item_id` marketing/other kini bisa ditautkan.

## API

| Method | Path | Keterangan |
|---|---|---|
| POST | `/projects/{id}/cost-entries` | seperti sebelumnya + field `cost_tier` opsional di body |
| POST | `/projects/{id}/cost-entries/preview` | idem (dry-run) |
| **POST** | **`/cost-entries`** | **baru** — pencatatan Tenant-level; `project_id` opsional di body (untuk overhead tanpa/dengan cost center) |
| **POST** | **`/cost-entries/preview`** | **baru** — dry-run pasangan di atas |
| GET/POST | `/cost-entries/{id}`, `/{id}/post` | tidak berubah |

Response `CostEntry` kini menyertakan `cost_tier`; `project_id` menjadi
`omitempty` (hilang hanya untuk overhead Tenant-level baru; data lama selalu ada).
Error baru (400): `cost_tier` tidak valid, matriks tier×kategori, unit wajib/dilarang,
phase/budget-link butuh project.

## Jurnal (contoh)

```
Overhead marketing Rp30jt via bank (project 777 sebagai cost center):
  Dr 5-3000 Beban Pemasaran            30.000.000   [project_id=777]
     Cr 1-1300 Bank                       30.000.000
  → saldo Persediaan 1-3xxx TIDAK berubah; pool HPP bersih.

Shared hard cost Rp100jt (pool proyek):
  Dr 1-3100 Persediaan — Hard Cost    100.000.000   [project_id=777, unit NULL]
     Cr 1-1300 Bank                      100.000.000
  → perilaku existing, tidak berubah.
```

## Test

- **Domain:** matriks tier×kategori penuh (18 kombinasi), disjungsi
  kapitalisasi-vs-beban, `AllCostCategories` dikunci tetap 4, pemetaan akun beban.
- **Cost (in-memory):** inferensi = aturan backfill; 8 kombinasi matriks ditolak;
  direct tanpa unit / shared+overhead dengan unit ditolak; overhead → Dr 5-3000/5-4000
  (bukti tidak menyentuh akun Persediaan), Cr bank/hutang; Tenant-level tanpa project
  (tag jurnal NULL, phase/budget-link ditolak); budget-link marketing OK & mismatch
  ditolak; preview==create; regresi direct/shared tetap ke 1-3xxx.
- **Integration (MySQL):** overhead posting → akun 5-3000 di ledger nyata +
  `AccumulatedByProject` (1-3xxx) tetap 0; Tenant-level tanpa project round-trip;
  realisasi marketing/other posted-only. Migrasi down→up diuji di DB dev.
- **Regresi:** `go test -tags integration ./...` hijau semua package
  (sale/allocation integration ikut lulus — schema change aman).

## Risiko & Backward Compatibility

- **Rendah-menengah.** Semua additive; perilaku existing dipertahankan lewat
  inferensi tier. Titik perhatian: `project_id` kini nullable — semua query
  existing memfilter `project_id = ?` sehingga baris NULL otomatis tereksklusi.
- Baris lama di-backfill deterministik; jurnal lama tidak disentuh.
- Test lama `MarketingOther_RealisasiSelalu0` sengaja diganti (perubahan perilaku
  yang disetujui): marketing/other kini TEREALISASI via tier overhead.
- Pool HPP/alokasi/snapshot/BAST/true-up: **tidak berubah** (membaca 1-3xxx dan
  `AllCostCategories` yang tetap 4).
- DB dev ditemukan dirty di migrasi 27 (kondisi pre-existing; schema nyatanya sudah
  s/d 000034) — diperbaiki dengan `migrate force 34` sebelum 000035 diterapkan.

## Non-goals (increment berikutnya)

- Read-model **laba bersih per proyek** (alokasi G&A management-only) — G1.
- UI Biaya untuk memilih tier + form overhead Tenant-level (frontend menyusul;
  API lama tetap jalan via inferensi).
- Approval Workflow untuk biaya besar; Payment Scheme; Tax Rule (increment
  berikutnya sesuai urutan).

## Next (belum dikerjakan — menunggu review)

Sesuai increment-1: berikutnya **Payment Scheme**, lalu **Tax Rule** — masing-masing
satu increment + STOP.
