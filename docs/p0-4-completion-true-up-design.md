# P0-4 — Completion True-Up (Design Note **v3 — FINAL**)

> **✅ SHIPPED (2026-07-17)** sesuai design ini (D1–D4 + IMPL-1..5 semua
> diimplementasikan; TU-1..TU-6 terverifikasi integration real MySQL).
> Implementation report: `p0-4-completion-true-up.md`; UI: `p0-4-frontend-spec.md`.
>
> Status historis: FINAL — arah & 4 keputusan terakhir DIKUNCI.
> Siap implementasi setelah sign-off **Pre-Coding Checklist (§18)**.
> Terkait: `budgeted-cost-allocation-spec.md`, `property-erp-implementation-roadmap.md`,
> `posting-rules.md`.

Deliverable review: **§4 ERD**, **§11 Migration order**, **§17 State machines**,
**§10 Journal examples**, **§3 & §16 business flow**, **§18 checklist**.

---

## 0. Keputusan terkunci (LOCKED)

| # | Keputusan | Nilai terkunci |
|---|---|---|
| **D1** | **Freeze basis alokasi** | Basis **immutable** untuk sebuah project setelah snapshot BAST pertama ada. Perubahan hanya via **allocation_config version baru** (deliberate, audited). Snapshot **pin** version yang dipakai saat BAST. |
| **D2** | **HPP penjualan pasca-completion** | **Opsi A** — pakai **HPP finalized** (dari true-up run), bukan alokasi live baru. Setelah completion: **no live recalculation**. |
| **D3** | **Granularitas** | Skema mendukung **phase** (kolom `phase_id` nullable / `completion_scope`), tapi **implementasi pertama = project level** (`phase_id = NULL`). Skema tidak mengunci. |
| **D4** | **Periode akuntansi** | **Opsi A — current period adjustment.** Periode lama closed → jurnal masuk **periode berjalan** dengan reference `"Prior period HPP adjustment"`. Periode masih open → boleh masuk periode itu. **Tidak reopen periode lama.** |

Detail masing-masing di §13.1–§13.4.

### 0.1 Implementation locks (LOCKED — detail coding)

| # | Lock | Aturan final |
|---|---|---|
| **IMPL-1** | **Satu basis version per scope** | Calculate true-up **GAGAL** (`ErrMixedAllocationBasisVersion`) bila snapshot terjual dalam scope punya `allocation_config_version` berbeda. Error jelas + minta user pisah scope. **Tidak ada auto-merge.** (§13.1) |
| **IMPL-2** | **Migration additive, legacy aman** | `sale_records` lama (`hpp_method='actual'`, `allocation_snapshot_id=NULL`) **tidak ikut** true-up, **tidak** direcalculate, jurnal lama **tidak** berubah. Scope true-up hanya `hpp_method='budgeted' AND allocation_snapshot_id IS NOT NULL`. Semua migrasi **ADD-only** (tanpa DROP/ubah kolom lama). (§11) |
| **IMPL-3** | **State machine guard keras** | Completion: tak boleh calculate sebelum `finalized`; `finalized` **tak bisa** balik ke `draft`/`completed`. True-up: `calculate` & `approve` **tak** membuat jurnal; **hanya** `posted` membuat jurnal; `posted` **immutable**. Transisi ilegal → `ErrInvalidStateTransition`. (§6, §7, §17) |
| **IMPL-4** | **Idempotency posting** | Klik `POST` dua kali ⇒ **satu** jurnal. Ditegakkan 3 lapis: (a) transisi `approved→posted` di dalam `tx` + **row lock** run; (b) `hpp_trueup_runs.journal_id` **UNIQUE** (sekali isi); (c) `journal_entries.reference = "HPP-TRUEUP-{run_id}"` **UNIQUE** per tenant. Post pada run non-`approved` → error. (§11, §12) |
| **IMPL-5** | **Audit actor** | `hpp_trueup_runs`: `created_by`, `calculated_by`, `approved_by`, `posted_by`, `cancelled_by` (+ masing-masing `*_at`). Transaksi accounting wajib jejak siapa-kapan di tiap transisi. |

---

## 1. Masalah yang diselesaikan

Metode **Budgeted Cost Allocation**: saat BAST, HPP unit diakui sebesar **porsi
RAB** (budgeted), bukan biaya aktual.

```
BAST unit terjual:   Dr HPP 5-1000            (budgeted)
                        Cr Persediaan 1-3xxx   (budgeted)
```

`Persediaan Real Estat 1-3xxx` menampung biaya **aktual** yang dikapitalisasi
(`Dr Persediaan / Cr Bank|Hutang`). Karena budgeted ≠ aktual, setelah proyek
selesai ada **variance** di 1-3xxx. **True-up** merekonsiliasi variance sebagai
**proses closing yang di-approve** supaya:

- HPP unit **terjual** akhirnya mencerminkan biaya **aktual** (semangat Invariant #4).
- Sisa Persediaan = biaya **aktual** unit **belum terjual** (inventory wajar).
- Unit yang terjual **pasca-completion** memakai HPP aktual yang sudah **finalized**.

True-up **tidak pernah** mengubah jurnal/snapshot BAST lama (Invariant #5 +
immutabilitas P0-3) — hanya memposting **jurnal penyesuaian baru**.

---

## 2. Definisi

Per **accounting_class** (land/hard/soft/financing), saat completion:

| Simbol | Arti | Sumber |
|---|---|---|
| `A_c` | biaya **aktual** dikapitalisasi kelas `c` | Σ debit terposting `1-3xxx` (posted-only, Decision A), dikunci saat `actual_cost_finalized_at` |
| `budg_HPP(u,c)` | HPP **budgeted** unit `u` | `allocation_snapshot_lines.amount` (unit terjual) |
| `act_HPP(u,c)` | HPP **aktual** unit `u` | alokasi `A_c` ke **semua** unit via **basis dari snapshot / config-version** (largest-remainder) |
| `Δ(u,c)` | variance | `act_HPP(u,c) − budg_HPP(u,c)` (hanya unit terjual di-jurnal) |

Basis yang dipakai = **basis config-version yang dibekukan** (D1) — bukan config
saat ini. Largest-remainder menjamin `Σ_u act_HPP(u,c) == A_c` persis (Invariant #3).

---

## 3. Rantai bisnis end-to-end (§16 versi ringkas di bawah)

```
RAB (budget_plans/items)   →  baseline biaya, di-approve, versioned; gate BCA-2
   ▼
Cost Entry (cost_entries)  →  biaya aktual terjadi
   ▼
Posting (journal_*)        →  Dr Persediaan / Cr Bank|Hutang (posted-only)
   ▼
Inventory (1-3xxx)         →  saldo biaya AKTUAL per class
   ▼
BAST (sale_records +       →  akui HPP BUDGETED; Dr 5-1000 / Cr 1-3xxx
   allocation_snapshots)      + snapshot immutable (identity + basis + config-version)
   ▼
HPP Snapshot               →  bukti historis; pin allocation_config_version (D1)
   ▼
Completion (project_       →  event akuntansi: konstruksi selesai → biaya final
   completion_events)
   ▼
True-Up (hpp_trueup_*)     →  closing: variance → jurnal adj (approved);
                              finalize act_HPP SEMUA unit (unsold utk D2)
   ▼
Financial Report           →  L/R & Neraca dengan HPP AKTUAL
```

---

## 4. ERD final

```
        ┌───────────────────────────────┐
        │ projects                      │
        └───┬───────────────┬───────────┘
            │               │
   ┌────────▼────────┐  ┌───▼──────────────────────────┐
   │ project_        │  │ hpp_trueup_runs      (BARU)  │
   │ completion_     │  │  id, tenant_id, project_id,  │
   │ events   (BARU) │  │  phase_id NULL, status,      │
   │  id, tenant_id, │  │  allocation_config_ver_id,   │
   │  project_id,    │  │  budget_hpp_total,           │
   │  phase_id NULL, │  │  actual_cost_total,          │
   │  status,        │  │  variance_total,             │
   │  completed_at,  │  │  created_by,                 │
   │  completed_by,  │  │  calculated_at/by,           │
   │  actual_cost_   │  │  approved_at/by,             │
   │  finalized_at,  │  │  posted_at, posted_by,       │
   │  created_by     │  │  journal_id  UNIQUE,         │
   │                 │  │  cancelled_at/by             │
   └─────────────────┘  └───────────┬──────────────────┘
                                    │1..N
                        ┌───────────▼──────────────────┐
                        │ hpp_trueup_lines      (BARU) │
                        │  id, tenant_id, run_id,      │
                        │  unit_id, unit_name_snapshot,│
                        │  category, is_sold,          │
                        │  budgeted_amount,            │
                        │  actual_amount,              │
                        │  variance_amount,            │
                        │  snapshot_id NULL,           │
                        │  journal_entry_id NULL       │  (NULL utk unsold/Δ=0)
                        └──────────────────────────────┘

  allocation_configs  ──►  DIVERSIONKAN (D1): version, active_key
     (mirror budget_plans one-active pattern)
        │
        └── allocation_snapshots (P0-3) ── DITAMBAH: allocation_config_version_id (+ version no)
              └── allocation_snapshot_lines (P0-3) ── DITAMBAH: unit_id, unit_name_snapshot
                    (identity historis — snapshot = source of truth)
```

---

## 5. Snapshot identity & config-version (immutabilitas historis)

`allocation_snapshot_lines` **+= `unit_id`, `unit_name_snapshot`** (nama/kode unit
**saat BAST**, disalin saat penulisan). `allocation_snapshots` **+= `allocation_config_version_id`**
(pin basis version, D1).

**Rule:**
> Snapshot = **sumber kebenaran historis**. Master data (`units`,
> `allocation_configs`) hanya kondisi **sekarang**. Auditor membaca snapshot tanpa
> join ke master. `hpp_trueup_lines.unit_name_snapshot` disalin dari snapshot.

---

## 6. True-up sebagai closing process — state machine

`hpp_trueup_runs.status`: **`draft → calculated → approved → posted`** (+ `cancelled`).

| Status | Arti | Transisi sah |
|---|---|---|
| `draft` | run dibuat, variance belum dihitung | → calculated, cancelled |
| `calculated` | variance + lines tersimpan; **belum ada jurnal** | → approved, cancelled |
| `approved` | direview & disetujui accounting | → posted, cancelled |
| `posted` | jurnal adjustment terposting (final) | *(terminal)* |
| `cancelled` | dibatalkan sebelum posting | *(terminal)* |

- `calculate` & `approve` **tidak** membuat jurnal; **hanya** `approved → posted`
  yang memposting jurnal; `posted` **immutable**. Transisi ilegal →
  `ErrInvalidStateTransition` (IMPL-3).
- **Idempoten (TU-5, IMPL-4)**: maksimal **satu run non-cancelled** per (tenant,
  project, phase) — UNIQUE partial. Recalculate hanya saat `draft`/`calculated`.
  Double-`post` ⇒ satu jurnal (row-lock + `journal_id` UNIQUE + reference UNIQUE).
- **Calculate menghitung `act_HPP` untuk SEMUA unit** (sold → di-jurnal saat post;
  unsold → disimpan sebagai HPP **finalized** untuk dipakai BAST pasca-completion, D2).
- Snapshot BAST lama tak pernah diubah.

---

## 7. Project completion lifecycle — state machine

`project_completion_events.status`: **`draft → completed → finalized`**.

| Status | Arti | Efek |
|---|---|---|
| `draft` | proses completion dimulai | — |
| `completed` | konstruksi dinyatakan selesai (`completed_at` diisi) | `projects.status='completed'` |
| `finalized` | biaya aktual dikunci (`actual_cost_finalized_at` diisi) | **membuka** true-up (`A_c` final) |

```
Project active → (Mark completion) → draft → completed → finalized → [True-up boleh calculate]
```

Guards (IMPL-3):
- true-up `:calculate` **hanya** setelah completion `finalized`.
- `finalized` **tidak boleh** kembali ke `draft`/`completed` (terminal maju).
- Transisi ilegal → `ErrInvalidStateTransition`.

---

## 8. Matriks 4 kasus + post-completion sale (D2)

| # | Kondisi | Unit | Jurnal (per class, `|Δ|`) | Efek |
|---|---|---|---|---|
| **1** | `actual > budget` (`Δ>0`) | **SOLD** | `Dr 5-1000 HPP` / `Cr 1-3xxx` | tambah COGS; Persediaan→aktual |
| **2** | `actual < budget` (`Δ<0`) | **SOLD** | `Dr 1-3xxx` / `Cr 5-1000 HPP` | kurangi COGS berlebih; kembalikan Persediaan |
| **3** | `actual > budget` | **UNSOLD** | *(tidak ada)* | variance tetap di inventory; `act_HPP` **difinalize** |
| **4** | `actual < budget` | **UNSOLD** | *(tidak ada)* | idem |
| **5 (D2)** | **BAST setelah completion** | (unit unsold yg baru dijual) | `Dr 5-1000 HPP` / `Cr 1-3xxx` sebesar **finalized `act_HPP`** | pakai HPP finalized, **tanpa** alokasi live |

- Baris `Δ=0` tidak dibuat (Invariant #1).
- Kasus 5: HPP = `hpp_trueup_lines.actual_amount` unit itu (is_sold=false → nanti true saat BAST).

---

## 9. Bukti rekonsiliasi (unsold otomatis benar)

```
saldo_c = A_c − Σ_{sold} budg_HPP − Σ_{sold} Δ = A_c − Σ_{sold} act_HPP = Σ_{unsold} act_HPP
```

**Invarian test:** TU-1 `saldo 1-3xxx per class == Σ act_HPP(unsold)`; TU-2 jurnal
balanced & hanya sentuh `5-1000`+`1-3xxx`; TU-3 `Σ HPP sold (BAST+trueup) == Σ act_HPP(sold)`;
TU-4 semua terjual ⇒ saldo `1-3xxx`=0; TU-5 idempoten; **TU-6 (D2)** BAST pasca-completion
memakai finalized `act_HPP`, relief Persediaan tepat.

---

## 10. Journal examples

Basis area; A 100 m² (25%), B 300 m² (75%); RAB land 400jt. BAST A saat proyek jalan:

```
BAST A (budgeted):
  Dr 5-1000 HPP                 100.000.000
     Cr 1-3000 Persediaan-Tanah   100.000.000
```

Completion, biaya tanah **aktual 480jt** → A_act=120jt, B_act=360jt.

```
True-up (run posted) — CASE 1 (A sold, Δ=+20jt):
  Dr 5-1000 HPP                  20.000.000
     Cr 1-3000 Persediaan-Tanah    20.000.000
  (B unsold: no journal; act_HPP(B)=360jt difinalize)
  Cek saldo 1-3000 = 480−100−20 = 360jt = act_HPP(B) ✔
```

Bila **aktual 360jt** → A_act=90jt (Δ=−10jt):

```
True-up — CASE 2 (A sold, Δ=−10jt):
  Dr 1-3000 Persediaan-Tanah     10.000.000
     Cr 5-1000 HPP                 10.000.000
```

Unit B **terjual setelah completion** (D2 / CASE 5), pakai finalized act_HPP(B):

```
BAST B (finalized):
  Dr 5-1000 HPP                 360.000.000   (= act_HPP(B), bukan alokasi live)
     Cr 1-3000 Persediaan-Tanah   360.000.000
  Setelah ini saldo 1-3000 = 0 (semua terjual, TU-4).
```

Bila jurnal true-up jatuh setelah periode BAST closed (D4): tetap di **periode
berjalan**, `reference = "Prior period HPP adjustment"`.

---

## 11. Migration dependency order (§18.2)

Berurutan (tiap migrasi bergantung pada sebelumnya):

| # | Migration | Isi | Depends on |
|---|---|---|---|
| `000029` | `allocation_config_versioning` | `allocation_configs` += `version`, `active_key`; UNIQUE `(tenant,project,active_key)` (mirror budget_plans). Baris lama → version 1 active. | 000008 |
| `000030` | `snapshot_identity_config_ref` | `allocation_snapshots` += `allocation_config_version_id` (+version no); `allocation_snapshot_lines` += `unit_id`, `unit_name_snapshot`. | 000027, 000029 |
| `000031` | `project_completion_events` | tabel completion (§7) + status + `created_by`. | 000005 |
| `000032` | `hpp_trueup` | `hpp_trueup_runs` (+ audit actor IMPL-5, `journal_id` **UNIQUE** IMPL-4) + `hpp_trueup_lines` (FK→runs CASCADE). UNIQUE partial run non-cancelled per tenant/project/phase (idempotensi TU-5). | 000030, 000031 |

Semua uang `DECIMAL(20,4)`; tiap tabel `id, tenant_id, created_at, updated_at`,
index `tenant_id`. Semua reversible. `000030` boleh backfill dev (unit_name dari
`units`, config version=1).

**IMPL-2 (additive & legacy-safe):** semua migrasi **ADD-only** — tidak ada
`DROP`/ubah kolom existing. `sale_records` lama tak tersentuh; scope true-up
memfilter `hpp_method='budgeted' AND allocation_snapshot_id IS NOT NULL`.
**IMPL-4 (idempotency):** jurnal true-up memakai `reference = "HPP-TRUEUP-{run_id}"`;
tambahkan UNIQUE `(tenant_id, reference)` di `journal_entries` **hanya** untuk
reference true-up (via index parsial atau guard aplikasi bila UNIQUE penuh tak
diinginkan — putuskan di P1). `hpp_trueup_runs.journal_id` UNIQUE menutup celah kedua.

---

## 12. API flow

| Langkah | Endpoint | Efek |
|---|---|---|
| Tandai selesai | `POST /projects/{id}/completion` | completion event `draft`→`completed` |
| Finalize biaya | `POST /projects/{id}/completion:finalize` | `finalized`; kunci `A_c` |
| **Preview variance** | `GET /projects/{id}/hpp-variance` | read-only; dampak sebelum posting |
| Hitung run | `POST /projects/{id}/hpp-trueup:calculate` | run `calculated` + lines (no journal) |
| Setujui | `POST /hpp-trueup/{runId}:approve` | `approved` |
| Posting | `POST /hpp-trueup/{runId}:post` | jurnal adj + `posted` |
| Batalkan | `POST /hpp-trueup/{runId}:cancel` | `cancelled` (pre-post) |
| Riwayat | `GET /projects/{id}/hpp-trueup` | daftar run |

`GET hpp-variance` response: `budget_hpp`, `actual_capitalized_cost`, `variance`,
`adjustment_required`, `basis_type`, `by_category[]`, `sold_units[]`
(budget/actual/adjustment), `unsold_units[]` (actual_hpp, "stays in inventory").

---

## 13. Detail keputusan terkunci

### 13.1 D1 — Freeze basis (versioned)
- Sebelum snapshot pertama: basis bebas diubah (update version aktif).
- Setelah snapshot pertama ada: basis version aktif **immutable**; ganti = **version
  baru** (supersede yang lama, pola budget_plans). UI **wajib memperingatkan** bahwa
  ini mengubah alokasi unit mendatang.
- Snapshot pin `allocation_config_version_id`. True-up memakai basis dari version itu.
- **Guard true-up**: semua snapshot terjual dalam scope harus 1 basis version; kalau
  span >1 version → tolak dengan panduan (pisah per phase). Diharapkan langka.

### 13.2 D2 — Post-completion sales
> **After completion: no live allocation recalculation; use finalized HPP snapshot.**
- True-up men-finalize `act_HPP` **semua** unit (termasuk unsold) di `hpp_trueup_lines`.
- Unit unsold yang **terjual setelah completion**: BAST memakai `act_HPP` finalized
  itu (metode `finalized`), `Dr 5-1000 / Cr 1-3xxx`. Tidak ada alokasi budgeted/live baru.
- Resolver BAST: jika project sudah `finalized` → ambil dari true-up line, **bukan**
  `BudgetedHPPResolver`.

### 13.3 D3 — Granularity
- Skema membawa `phase_id` (nullable) di `project_completion_events` &
  `hpp_trueup_runs`. `phase_id = NULL` ⇒ scope = whole project.
- **Implementasi pertama: project level** (`phase_id` selalu NULL). Phase-level =
  aktivasi belakangan tanpa perubahan skema.

### 13.4 D4 — Accounting period
- **Current period adjustment** (change-in-estimate). **Tidak reopen** periode lama.
- Periode BAST sudah closed → jurnal true-up di **periode berjalan**,
  `reference = "Prior period HPP adjustment"`, tanggal = tanggal posting/completion.
- Periode masih open → boleh di periode itu.
- `ledger.PostingService.WithPeriodChecker` menjamin jurnal jatuh di periode terbuka.
- `// TODO(tax/accounting-policy): konfirmasi label/penyajian "prior period adjustment"
  di laporan (disclosure), bukan mekanisme posting.`

---

## 14. Immutabilitas & audit
- Jurnal BAST + snapshot P0-3 **tidak diedit**. True-up = jurnal baru
  (`source='hpp_trueup'`, `reference=run#id`). Koreksi pasca-`posted` = run baru +
  jurnal pembalik.
- Jejak lengkap: `hpp_trueup_lines` → `snapshot_id` (asal budgeted) + `journal_entry_id`
  (adjustment) + `unit_name_snapshot`.

---

## 15. Non-goals
- Tidak mengubah pengakuan pendapatan; tidak menyentuh pajak (murni reklas HPP↔Persediaan).
- UI penuh menyusul (P2); P0-4 = backend + endpoint + test.

---

## 16. Business flow (target ERP, bukan kumpulan endpoint)

```
RAB → Cost Entry → Posting → Inventory → BAST → HPP Snapshot → Completion → True-up → Financial Report
 │        │           │          │         │          │             │           │            │
 gate   aktual     posted     saldo     budgeted   historis      event      variance      HPP
 BCA-2  biaya      only       aktual     HPP        (pin basis)   akuntansi  → jurnal adj   aktual
```

Setiap panah = relasi data nyata (§3). True-up menutup loop antara **rencana (RAB)**,
**realisasi (Inventory)**, dan **pengakuan (HPP)**.

---

## 17. State transition diagrams (deliverable checklist #3)

```
COMPLETION (project_completion_events.status)
  draft ──(mark completed)──► completed ──(finalize actual cost)──► finalized
                                                                       │
                                                                       ▼ enables
TRUE-UP (hpp_trueup_runs.status)
  draft ──(calculate)──► calculated ──(approve)──► approved ──(post)──► posted
             │                │                        │
             └────────────────┴────────(cancel)───────┘──► cancelled
  (posted & cancelled = terminal; hanya approve→post yang memposting jurnal)
```

---

## 18. Pre-Coding Checklist (sign-off sebelum implementasi)

1. **Final ERD** — §4. ✔ tabel baru: `project_completion_events`, `hpp_trueup_runs`,
   `hpp_trueup_lines`; perubahan: `allocation_configs` (versioned),
   `allocation_snapshots` (+config_version), `allocation_snapshot_lines` (+identity).
2. **Migration dependency order** — §11: `000029 → 000030 → 000031 → 000032`.
3. **State transitions** — §17: completion `draft→completed→finalized`; true-up
   `draft→calculated→approved→posted` (+`cancelled`).
4. **Journal examples** — §10: BAST (Dr HPP/Cr Inv), true-up CASE 1/2, post-completion
   BAST finalized (CASE 5).
5. **End-to-end business flow** — §16.

**Blocking sebelum coding:** konfirmasi D1–D4 terkunci sudah sesuai, dan jawab
guard-edge §13.1 (multi-basis-version dalam satu scope) — default: **tolak & minta
scope per phase**.

### Test plan (saat coding disetujui)
- Unit: alokasi aktual (`Σ act_HPP==A_c`), matriks 5 kasus, state machine (transisi
  legal/ilegal), idempotensi, basis-freeze guard.
- Integration: RAB + BAST sebagian + biaya aktual beda → completion(finalize) →
  calculate → approve → post → assert **TU-1..TU-6**, immutabilitas snapshot/jurnal,
  `unit_name_snapshot` tetap walau unit di-rename, **BAST unit unsold pasca-completion
  pakai finalized act_HPP**.
- Preview `GET hpp-variance` cocok dengan lines yang diposting.
