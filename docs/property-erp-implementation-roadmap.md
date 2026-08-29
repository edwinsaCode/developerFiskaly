# Property ERP — Implementation Roadmap (from Domain Audit)

> Status: **ARCHITECTURE PLAN — no code until approved.** Derived from
> `docs/property-erp-domain-audit.md`. Each change lists: DB impact · API impact ·
> migration risk · backward compatibility · user workflow before/after. Date 2026-07-01.

---

## ★ DECISIONS LOCKED (2026-07-01) — read this first

| Decision | Choice |
|---|---|
| **A — when cost counts** | **Posted-only everywhere.** Reports/allocation/HPP all read *posted* ledger; the draft/post step is dropped (post-on-create) or draft is excluded from every report. |
| **B — HPP basis** | **B2 — Budgeted total cost (PSAK 44).** HPP = unit's share of the **approved RAB total**, allocated by basis, recognized at BAST; **true-up** vs actual at project completion. |
| **RAB role** | **Soft control** (warn on overrun) **+ B2 gate**: an **approved active RAB is required before a unit can be sold/BAST'd**. |

**These supersede the "fork" notes in the sections below.** The finalized B2 design follows.

### What B2 re-architects (the core change)

```
 ACTUAL COST (unchanged)            BUDGET (elevated)            REVENUE RECOGNITION (changed)
 ───────────────────────           ─────────────────            ─────────────────────────────
 Cost → Dr Persediaan 1-3xxx  ┐     RAB approved →         ┌───► BAST: HPP = unit's share of
        / Cr Bank|Hutang      │     per-category BUDGET    │     APPROVED RAB total (by basis),
 (posted-only accumulates     │     totals = the HPP pool  │     per capitalizable category
  ACTUAL in inventory)        │     (land/construction=    │     Event 4: Dr HPP 5-1000
        │                     │      hard/soft/financing;  │       / Cr Persediaan 1-3xxx
        │ feeds               │      marketing/other are   │       at BUDGETED amount
        ▼                     │      period expense, NOT   │            │
 ACTUAL cost tracker /        │      HPP)                  │     Snapshot (P0-2/P0-3) stores
 RAB-vs-actual variance ──────┘                            │      RAB version + basis + weight
 (soft-control warnings)                                   │      + budgeted HPP per unit → SoT
        │                                                  │      for HPP audit
        └──────────► P0-4 TRUE-UP at project completion ◄──┘
                     actual total vs budgeted total →
                     adjust recognized HPP + relieve/settle inventory
```

**Key consequences:**
- Inventory (Persediaan) accrues **actual**; HPP relieves **budgeted** → a per-project
  actual-vs-budget balance builds up, settled by the **true-up (P0-4)** at completion.
- The existing live allocation of *actual* posted cost is **retained** but repurposed as the
  **actual cost tracker** (RAB-vs-actual, soft-control). A **second** allocation of the
  **budgeted** RAB pool drives HPP.
- **RAB becomes mandatory + approved before sale** (the gate). No RAB ⇒ cannot BAST.

### Finalized designs (replace the forks below)

**P0-3 + P0-2 (snapshot + auditable HPP), B2 form:** the `cost_allocation_snapshots` header
gains `source='budget'`, `budget_plan_id` (which approved RAB version), and stores **budgeted**
per-category pool + per-unit budgeted HPP. `sale_records.allocation_snapshot_id` links HPP to
this row. Drill-down shows "HPP A-01 = 500jt = share of RAB 10M by luas jual 5%, RAB v3".
*DB:* 2 tables + 1 nullable FK column (additive, low risk). *API:* snapshot reads + BAST writes.
*Compat:* old sale_records = NULL snapshot. *Workflow:* HPP auditable, stable across sale timing.

**P0-4 — Completion true-up (NEW, required by B2). DESIGN v2 APPROVED-IN-DIRECTION,
awaiting sign-off — see `docs/p0-4-completion-true-up-design.md`.**

Revised after architecture review into a **production-grade accounting closing process**
(not a one-shot function). Scope:
- *What:* at project completion, compute **actual posted cost** (`A_c`) vs **budgeted HPP
  recognized** per unit/class; post an adjusting journal moving variance for **already-sold**
  units only (`Δ>0` Dr HPP/Cr Persediaan; `Δ<0` reverse). Unsold = **no journal** — residual
  Persediaan algebraically equals unsold units' actual cost (proof §8). Append-only; BAST
  journals & P0-3 snapshots never edited.
- *Closing process (Req #2):* `hpp_trueup_runs` **state machine** `draft → calculated →
  approved → posted` (+ `cancelled`). Calculate writes variance lines (no journal); a separate
  **approval** step precedes posting. `hpp_trueup_lines` (unit_id, unit_name_snapshot, category,
  budgeted/actual/variance, journal_entry_id NULL for unsold).
- *Completion lifecycle (Req #3):* `project_completion_events` (completed_at, completed_by,
  actual_cost_finalized_at) — completion is a business event, gates true-up.
- *Snapshot identity (Req #1):* add `unit_id` + `unit_name_snapshot` to `allocation_snapshot_lines`
  so snapshots are readable years later without master-data joins (historical source of truth).
- *Variance preview (Req #4):* `GET /projects/{id}/hpp-variance` read-only impact review before posting.
- *4 decisions LOCKED (design v3 §0):* **D1** basis immutable after first snapshot → versioned
  `allocation_configs`, snapshot pins config-version; **D2** post-completion sales use *finalized*
  HPP (no live recalculation); **D3** schema carries `phase_id` (nullable) but first impl is
  project-level; **D4** current-period adjustment (no reopen; closed period → posts current with
  `"Prior period HPP adjustment"` reference).
- *DB (Req #7):* migrations `000029` allocation_config versioning, `000030` snapshot identity +
  config-version ref, `000031` project_completion_events, `000032` hpp_trueup (runs+lines).
  Dependency order 29→30→31→32. All additive + reversible.
- *API:* `POST /projects/{id}/completion` (+ `:finalize`), `GET /projects/{id}/hpp-variance`,
  `POST /projects/{id}/hpp-trueup:calculate`, `:approve`, `:post`, `:cancel`.
- *State machines:* completion `draft→completed→finalized`; true-up `draft→calculated→approved→posted`
  (+`cancelled`); only `approved→post` posts a journal; one non-cancelled run per project/phase (idempotent).
- *Migration risk:* **medium** — posts adjusting journals; atomic + idempotent + guarded (completion
  finalized + basis-version consistent within scope).
- *Remaining pre-coding gate:* sign-off on design v3 §18 checklist + the multi-basis-version guard
  default (reject, scope per phase). One `TODO(tax/accounting-policy)` left = disclosure label only.
- *Workflow before:* budget↔actual variance invisible/unsettled. *After:* completion → preview →
  calculate → approve → post; P&L final at **actual** HPP, inventory at actual for unsold units.

**P1-1 (foundation — now a PREREQUISITE for B2, do first):** centralize the category taxonomy
(one enum + one mapping; kill `construction`↔`hard` raw-string risk); expose approved-RAB
per-category totals as the HPP budget pool; give `marketing`/`other` a real **expense** posting
path (Dr Beban 5-xxx / Cr Bank|Hutang, linkable to `budget_item_id`) so RAB-vs-actual is complete;
fix RAB-realisasi to posted-only (Decision A). *DB:* expense path may add a `cost_type` column or
`expense_entries`. *Risk:* medium (touches posting). *Compat:* existing capitalized costs untouched.

**P1-3 (RAB control), B2 form:** soft warn on category/item overrun **+** BAST requires an approved
active RAB (the B2 gate; returns a clear error if missing). *DB:* none. *API:* cost preview returns
`budget_remaining`/`over_budget`; BAST validates approved RAB exists. *Risk:* low. *Compat:* the gate
is new behavior for BAST — but B2 cannot compute HPP without it, so it's intrinsic (guard with a
clear "Approve RAB dulu" message + link).

### Revised sequencing (B2)

1. ✅ **P0-1** decided (A=posted-only, B=B2, RAB=soft+gate).
2. ✅ **P1-1 foundation** — taxonomy unify (`AllCostCategories` + `Amount()`), RAB per-category
   HPP pool, posted-only realisasi. *(marketing/other expense-posting path still DEFERRED.)*
3. ✅ **P0-3 + P0-2 + B2 HPP** — budgeted-allocation, atomic snapshot at BAST, auditable HPP,
   RAB-approved gate + `ErrAllocationBasisMissing`. Migrations 000027 (snapshots) + 000028
   (basis evidence: basis_type/value/percentage).
4. **P0-4** — completion true-up. **Design v3 FINAL (`docs/p0-4-completion-true-up-design.md`),
   4 decisions LOCKED (D1 basis-freeze/versioned, D2 finalized post-completion HPP, D3
   project-level-first/phase-ready, D4 current-period adjustment). Awaiting §18 checklist
   sign-off before coding.** Migrations 000029–000032.
5. **P1-3** — soft-control warnings.
6. **P1-2** — project workspace navigation (frontend, safe, high value).
7. **P2** — UX polish.

Each phase ships behind in-memory + integration tests (FE-2/FE-3 pattern) and stops for review.

---

## Dependency graph (why order matters)

```
P0-1 Cost accounting POLICY (decision)
        │  drives HPP basis, snapshot contents, RAB's role
        ├──────────────┬───────────────────┐
        ▼              ▼                     ▼
P0-3 Persist      P0-2 Auditable      P1-3 RAB role
allocation        HPP (drill-down)    (planning vs control)
snapshot          (uses P0-3 data)          │
        │              │                     │
        └──────┬───────┘                     │
               ▼                             ▼
        P1-1 Connect RAB→Cost→Allocation ◄───┘
               │
               ▼
        P1-2 Project workspace navigation
               │
               ▼
        P2  UX polish
```

**P0-1 must be decided first** — everything else forks on it. P0-2 and P0-3 are one
mechanism (a persisted snapshot) and ship together.

---

# P0-1 — Define cost accounting policy  *(DECISION, no code)*

Two sub-decisions. The audit found both are currently undefined/inconsistent.

## Decision A — "When does a cost count?" (draft vs posted)
Today RAB-vs-Realisasi counts **unposted drafts**; HPP/allocation count **posted only**
(audit P0-1). They disagree.

| Option | Rule | Trade-off |
|---|---|---|
| **A1 (recommend)** | **Posted-only everywhere.** Cost entry posts on create (drop the draft step), or draft is excluded from *all* reports until posted. | One number everywhere; simplest. Loses the "review before post" step (add an explicit approval later if wanted). |
| A2 | Include drafts everywhere. | Reports move on unposted data; weaker audit trail. Not recommended. |

## Decision B — Cost cutoff / HPP basis  *(the big one)*
How is a unit's HPP determined, and what happens to project-wide costs recorded **after**
a unit's BAST? (audit P0-2/P0-3)

| Option | HPP basis | Post-BAST costs | RAB's role | Fits |
|---|---|---|---|---|
| **B1 — Actual + reallocate remaining** | Actual posted cost allocated at BAST; frozen per sold unit | Re-spread over **unsold units only** | variance report only | Strict actual-cost shops |
| **B2 — Budgeted total cost (PSAK 44 style)** | Unit's share of **approved RAB total** (estimated), allocated by basis, recognized at BAST; **true-up** at project completion | Absorbed into actual; trued-up against budget at completion | **Central** — RAB *is* the HPP basis | Indonesian property developers (industry standard) |
| B3 — Status quo | Actual at BAST instant, no cutoff | Silently distort everyone | none | (current — flawed, reject) |

**Recommendation: B2** for a property developer. It (a) matches PSAK 44 real-estate practice,
(b) gives every unit of the same type a **consistent** HPP regardless of sale timing (B1 makes
late buyers absorb more cost → margins wobble by timing), and (c) **connects RAB → HPP**, which
resolves P1 organically. Cost: requires RAB approved before selling + a completion true-up.
**B1** is a valid simpler alternative if you want pure actual-cost and accept timing variance.

> **This choice forks P0-2, P0-3, P1-1, P1-3.** See the fork notes below. I need this answer
> before finalizing those designs.

**DB impact:** none (policy). **API/migration/compat:** none. **Workflow:** defines the rules
the rest implement.

---

# P0-3 — Persist allocation snapshot  *(ships with P0-2)*

**Why:** allocation is recomputed live and never stored; `allocation_executions` holds only a
scalar total (audit P0-2, P1-4). You cannot answer "what did we allocate to unit A-01 when we
BAST'd it?"

**Design:** new snapshot header + lines, written at each BAST (and optionally each explicit
"Run Allocation"). Policy-agnostic mechanism; only the *contents* differ by Decision B.

- `cost_allocation_snapshots` — id, tenant_id, project_id, basis, `reason` (bast|manual|period_close),
  `pool_land/hard/soft/financing` (project-wide pool used), `total_weight`, `taken_by`, `taken_at`.
- `cost_allocation_snapshot_lines` — snapshot_id, unit_id, weight, `direct_*`, `allocated_*`,
  `total_*` (per 4 categories).

**Fork by Decision B:**
- **B1** → snapshot stores *actual* pool at BAST; excludes already-sold units from `total_weight`.
- **B2** → snapshot stores *budgeted* pool (from approved RAB) + which units; true-up snapshot at completion.

| Dimension | Detail |
|---|---|
| **Database** | 2 new tables (additive). No change to existing tables except P0-2's FK column below. |
| **API** | New: `GET /projects/{id}/allocation/snapshots`, `GET /allocation/snapshots/{id}`. BAST tx now also writes a snapshot. |
| **Migration risk** | **Low** — additive tables. The BAST write path changes (new insert inside the existing atomic BAST tx) — must stay in the same transaction (mirror the existing BAST atomicity). |
| **Backward compat** | Existing `sale_records` have no snapshot → historical HPP not retro-auditable (acceptable; optional best-effort backfill from current ledger, flagged as reconstructed). Live allocation endpoint unchanged. |
| **Workflow before** | HPP is a black box; allocation recomputes silently. |
| **Workflow after** | Every BAST captures an immutable "how HPP was computed" record; cost controller can audit any unit's cost basis at its recognition date. |

---

# P0-2 — Make HPP auditable  *(uses P0-3 snapshot)*

**Why:** `sale_records.hpp_*` stores only the 4 result numbers; no record of direct-vs-allocated,
basis, pool, or weight used (audit P0-3).

**Design:** link the BAST's recognized HPP to its snapshot line.

- `sale_records` **+ column** `allocation_snapshot_id BIGINT UNSIGNED NULL` (FK →
  `cost_allocation_snapshots`). The snapshot *line* for that unit shows the full derivation.
- New read endpoint: `GET /units/{unitId}/hpp-detail` → returns HPP breakdown + its snapshot
  (direct/allocated/basis/pool/weight) for drill-down.

| Dimension | Detail |
|---|---|
| **Database** | 1 nullable column on `sale_records` + FK. Additive. |
| **API** | New `GET /units/{id}/hpp-detail`. BAST response may include `allocation_snapshot_id`. |
| **Migration risk** | **Low** — nullable column; no rewrite of existing rows. |
| **Backward compat** | Old sale_records keep `NULL` snapshot → drill-down shows "detail tidak tersedia (pra-fitur)". |
| **Workflow before** | Accountant sees a lump HPP number, can't defend it in audit. |
| **Workflow after** | Drill-down: "HPP unit A-01 = Direct 300jt + Allocated 100jt (basis luas jual, dari pool 400jt ÷ bobot 25%)". Auditable. |

---

# P1-1 — Connect RAB → Cost → Allocation

**Why:** RAB link is optional metadata; `marketing`/`other` are dead-ends; `construction`↔`hard`
is a fragile alias (audit P1-1/P1-2/P1-3).

**Design (independent of Decision B, but B2 makes it central):**
1. **Centralize the category taxonomy** — one enum + one mapping function used by budget, cost,
   HPP, reports. Rename or alias `construction`↔`hard` in exactly one place; ban raw-string compares.
2. **Give `marketing`/`other` an actuals path** — either (a) add an expense-posting cost type that
   links to `budget_item_id` (Dr Beban Pemasaran 5-xxx / Cr Bank|Hutang), so marketing budget-vs-actual
   works, or (b) drop marketing/other from RAB if out of scope. **Decision needed.**
3. **B2 only:** allocation/HPP reads the approved RAB total as the HPP basis (the connection becomes
   structural, not just a report).

| Dimension | Detail |
|---|---|
| **Database** | If (a): `cost_entries` gains a `cost_type` (capitalized\|expense) or a new `expense_entries` table; expense posts to 5-xxx not 1-3xxx. If (b): none. B2: no schema, changes allocation source. |
| **API** | Cost create accepts marketing/other (expense path); RAB-realisasi query fixed to reconcile posted-only + include expense actuals. |
| **Migration risk** | **Medium** — touches cost posting logic (accounts differ) + the RAB-realisasi query (P0-1 filter fix). Must not corrupt existing capitalized costs. |
| **Backward compat** | Existing cost_entries unchanged (all capitalized). New expense entries are additive. |
| **Workflow before** | Marketing cost recorded via manual Jurnal, invisible to RAB; realisasi wrong. |
| **Workflow after** | Marketing/soft/hard all recorded in one Biaya flow, each linkable to a RAB line; RAB-vs-Realisasi complete and reconciles with the ledger. |

---

# P1-3 — Define RAB role (control)  *(decision + light impl)*

**Why:** approval gates nothing (audit P1-1).

| Option | Behavior | Fork |
|---|---|---|
| **Planning + correct variance** | RAB stays advisory; only fix the variance report (P1-1). | pairs with Decision **B1** |
| **Soft control** | Warn (non-blocking) when posting a cost would push a category/item over approved budget. | either B |
| **Hard control** | Block cost exceeding approved budget without override; **B2 requires** an approved RAB before a unit can be sold. | required by **B2** |

**Recommendation:** **Soft control** minimum (warn on overrun); **B2 ⇒ Hard-ish** (RAB approval
required before sale, since HPP basis = RAB).

| Dimension | Detail |
|---|---|
| **Database** | None (computed: Σ posted actual per category/item vs budget). |
| **API** | Cost preview/create returns `budget_remaining` per category/item + `over_budget` flag; BAST (B2) checks an approved RAB exists. |
| **Migration risk** | **Low** (read-side computation) — unless hard-block, which can reject previously-allowed actions (guard behind a setting). |
| **Backward compat** | Warn = compatible. Hard-block = behavior change → make it a tenant setting, default warn. |
| **Workflow before** | Costs recorded with no budget awareness. |
| **Workflow after** | Cost controller sees "Rp X / Rp Y budget (80%)" while recording; overrun flagged. |

---

# P1-2 — Add project workspace navigation

**Why:** RAB has no home in the project; "Biaya" overloaded; forward chain unlinked; user bounces
5–6× to the sidebar (audit P2-1..P2-4). **Frontend-only.**

**Design:** make `/proyek/[id]` the hub.
1. Add a **RAB tab** to project detail (reuse `/proyek/[id]/rab` content).
2. Unify **Biaya**: one tab with cost *entry* + cost *results* (or clearly linked sub-views).
3. Add forward **"next step"** links: RAB→"Catat Biaya", Biaya→"Jalankan Alokasi", Alokasi→"Jual Unit".
4. Keep top-level sidebar RAB/Biaya as **project pickers** (already built) → redirect into the tab.
5. Consolidate or clearly delineate the two unit-detail pages (`/proyek/[id]/unit/[id]` vs `/penjualan/[id]`).

| Dimension | Detail |
|---|---|
| **Database** | **None.** |
| **API** | **None** (reuses existing endpoints). |
| **Migration risk** | **None** (routing/UI only). |
| **Backward compat** | Old routes kept as redirects; no broken links. |
| **Workflow before** | Sidebar → RAB picker → project → back to sidebar → Biaya picker → project → … (5–6 hops). |
| **Workflow after** | Enter project once; do RAB → Cost → Allocation → Sell via tabs + forward links, without leaving `/proyek/[id]`. |

---

# P2 — UX improvements  *(frontend-only, low risk)*

Success toast on project create; consolidate duplicate unit-detail pages; empty-state polish
already largely done. **DB/API/migration: none. Compat: full.** Deferred until P0/P1 land.

---

## Proposed sequencing

1. **P0-1 decision** (you) → unblocks everything.
2. **P0-3 + P0-2** together (snapshot + auditable HPP) — the correctness core.
3. **P1-1** (taxonomy + marketing path + RAB-realisasi filter fix) — resolves the divergence.
4. **P1-3** (RAB control, per Decision B).
5. **P1-2** (project workspace nav) — frontend, safe, high user value.
6. **P2** polish.

Each ships behind tests (in-memory + integration, per the FE-2/FE-3 pattern) and stops for review.

> **STOP — no code.** Approve the roadmap + answer P0-1 (Decisions A & B). Decision B (B1 vs B2)
> is the fork that shapes P0-2/P0-3/P1-1/P1-3.
