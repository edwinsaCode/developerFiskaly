# Increment 1 — Party/Actor Masters (Customer + Sales Person/Team)

> Fase pertama implementasi CORE (Foundation Accounting, dipecah per domain).
> Blueprint: `erp-blueprint-review.md` §1/§8. **Master murni — tidak menyentuh
> ledger, tidak ada jurbal.** Menyediakan seam referensi tanpa merombak alur jual.

## Ruang lingkup
- **Customer master** (`customers`) — menggantikan `buyer_ref` free-text.
- **Sales Team master** (`sales_teams`) — tipis (target/KPI = read-model P2).
- **Sales Person master** (`sales_persons`) — atribusi penjualan.
- **Seam** — kolom nullable `customer_id`, `sales_person_id` di `sale_contracts`
  (belum di-rewire; alur BAST/kontrak tidak berubah).

## Perubahan Database (migration 000033, reversible)
| Objek | Keterangan |
|---|---|
| `customers` | id, tenant_id, code, name, type(individual/company + CHECK), id_number(NIK), npwp, phone, email, address, is_active, created_by, timestamps. UNIQUE(tenant_id, code). |
| `sales_teams` | id, tenant_id, code, name, leader_sales_person_id(nullable), is_active, timestamps. UNIQUE(tenant_id, code). |
| `sales_persons` | id, tenant_id, code, name, sales_team_id(nullable), user_id(nullable), phone, email, join_date, is_active, timestamps. UNIQUE(tenant_id, code). |
| `sale_contracts` | +`customer_id` nullable, +`sales_person_id` nullable, + index. **Additive (IMPL-2)** — kolom `buyer_name`/`buyer_id`/`buyer_ref` tetap. |

Down migration menghapus kolom seam lalu drop 3 tabel. Reversibilitas diuji (down→up bersih).

## Domain & Service
- `internal/customer` — Customer + `CustomerType`(Valid); Service: Create/Get/List/Update.
  Validasi: code & name wajib; type default `individual`, tolak selain individual/company.
  Update parsial (field kosong = tak diubah); code immutable.
- `internal/salesorg` — SalesTeam + SalesPerson; Service: CRUD keduanya. Integritas:
  CreatePerson/UpdatePerson dengan `sales_team_id` **memvalidasi team ada & satu tenant**
  (tolak lintas-tenant → ErrTeamNotFound).
- Semua query **tenant-scoped manual** (`tenant_id = ?`, Invariant #6). Duplikat
  kode → error 1062 dipetakan ke `Err...CodeDup`.

## API (protected, JWT; write butuh role owner|accountant)
| Method | Path | Aksi |
|---|---|---|
| GET | `/api/v1/customers` | list |
| POST | `/api/v1/customers` | create |
| GET | `/api/v1/customers/{id}` | get |
| PUT | `/api/v1/customers/{id}` | update |
| GET/POST | `/api/v1/sales-teams` , `/{id}` GET/PUT | CRUD team |
| GET/POST | `/api/v1/sales-persons` , `/{id}` GET/PUT | CRUD person |

Error: 400 (validasi) · 404 (not found) · 409 (kode duplikat) · 401 (no auth).

## Workflow & Jurnal
- **Workflow:** tidak ada (master data; tidak masuk Approval Workflow / lifecycle).
- **Jurnal:** **tidak ada** — tidak menyentuh ledger sama sekali.

## Test
- Unit (in-memory): customer (create/validasi/duplikat/isolasi tenant/update parsial),
  salesorg (validasi, person butuh team valid, isolasi tenant, team lintas-tenant ditolak).
- Integration (real MySQL): round-trip + UNIQUE(tenant,code) via error 1062 + isolasi
  tenant + persist update + set leader team.
- Regressi: `go test ./...` hijau; **sale integration hijau** setelah ALTER sale_contracts
  (back-compat terbukti).

## Risiko & catatan
- **Rendah** — additive, tanpa ledger, tanpa perubahan perilaku existing.
- Seam `customer_id`/`sales_person_id` **belum diisi** oleh alur kontrak; wiring
  (buyer_ref → customer_id) dilakukan di increment berikutnya saat Sale Contract
  disentuh. `buyer_ref`/`buyer_name`/`buyer_id` tetap sumber saat ini.
- Sales Team sengaja disertakan tipis (parent ownership) walau P1 menyebut "Sales
  Person" — target/KPI/komisi tetap ditunda (P2/FUTURE) sesuai blueprint.

## Penyempurnaan (disetujui setelah review Increment 1)
1. **`code` immutable** (Customer & Sales Person) — ditegakkan *by construction*:
   Update request tak memiliki field `Code`, repo Update tak menyertakan kolom
   `code`. Diverifikasi test (`upd.Code` tetap). Komentar eksplisit di model.
2. **`is_active` → status enum** — **ROADMAP** (belum ubah schema). Rencana:
   ganti `is_active` menjadi `status` eksplisit (mis. `active | inactive |
   resigned | suspended`) saat lifecycle SDM/customer dibutuhkan. Migrasi additive:
   tambah `status`, backfill dari `is_active`, deprecate `is_active`.
3. **Customer `segment` seam** — kolom `segment VARCHAR(30)` (migration 000034,
   reversible). Dimensi reporting BISNIS (`subsidi | komersial | investor |
   corporate`), **berbeda** dari `type` (individual/company). **RESERVED** — belum
   dipakai logika; tanpa CHECK agar nilai bisa berkembang.
4. **Customer Merge / Duplicate Detection** — **FUTURE capability** (tanpa
   implementasi). Lihat catatan blueprint (`blueprint-domain-additions.md` §11).
5. **Contract policy (dikunci untuk increment mendatang):** setiap **Sale Contract
   BARU wajib** mengisi `customer_id` **dan** `sales_person_id`. Field legacy
   (`buyer_ref`, `buyer_name`, `buyer_id`) **hanya** dipertahankan untuk
   backward-compatibility data lama; **tidak** untuk kontrak baru. Enforcement
   ditambahkan saat alur Sale Contract di-rewire (increment kontrak/penjualan).

## Next (belum dikerjakan — nunggu review)
Increment berikutnya (Foundation Accounting): **Cost 3-tier**, lalu **Payment
Scheme**, **Tax Rule** — masing-masing satu increment + STOP.
