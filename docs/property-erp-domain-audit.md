# Property ERP — Domain Workflow Audit

> Status: **REVIEW — no code changes.** Read-only analysis of entity relationships across
> the property-development lifecycle. Perspectives: owner, project manager, accountant,
> cost controller. Date: 2026-07-01.

---

## 0. TL;DR — the core problem

Features were built vertically (each screen works in isolation) but the **horizontal
business relationships between them are weak or missing**. Specifically:

1. **RAB (budget) is an island** — planning-only, optional, category-level, with no
   enforced link into cost recording or HPP. Budget approval is *not* a gate for anything.
2. **Two "cost" numbers that disagree** — RAB-vs-Realisasi counts **unposted draft** cost
   entries; HPP/allocation counts only **posted** ones. Same cost, two answers.
3. **Allocation is recomputed live, HPP is frozen at BAST** — with no exclusion of
   already-sold units, so post-BAST costs distort every downstream number.
4. **The workflow is scattered across 3 unrelated navigation surfaces** — the user must
   jump between project tabs and top-level sidebar pickers 5–6 times per lifecycle, and the
   forward chain (RAB → Cost → Allocation → Sell) is entirely unlinked.
5. **Taxonomy is inconsistent** — RAB has 6 categories, Cost/HPP have 4; `construction`↔`hard`
   is bridged by one alias function; `marketing`/`other` RAB lines can **never** be realized.

These are architectural, not cosmetic. Building FE-3 (or any feature) on top without fixing
the relationships means future features (project P&L, cost cutoff, budget control) will
require rewriting these foundations.

---

## 1. Entity ownership map

Legend: **SoT** = source of truth.

| # | Entity | Owner (writes) | Parent | Consumers | SoT | On change |
|---|--------|----------------|--------|-----------|-----|-----------|
| 1 | **Project** | project svc | — (tenant) | everything | `projects` row | rename only; cascades nothing financial |
| 2 | **Phase** | project svc | Project | budget_plans, units, cost_entries | `project_phases` | optional grouping |
| 3 | **RAB / BudgetPlan** | budget svc | **Project (or Phase)** | RAB-vs-Realisasi report only | `budget_plans`/`budget_items` | versioned; approve → supersede old (Invariant #8) |
| 4 | **Budget approval** | budget svc | BudgetPlan | — (status flag) | `budget_plans.status/active_key` | draft→active→superseded; **gates nothing downstream** |
| 5 | **Cost entry** | cost svc | **Project** (+ optional Unit/Phase/BudgetItem) | allocation, HPP, RAB-realisasi, reports | **`journal_entries` (posted)**; `cost_entries.amount` is a display copy | draft→posted; only *posted* feeds HPP |
| 6 | **Cost allocation** | allocation svc | Project | HPP at BAST, unit cost breakdown, dashboards | **computed LIVE** (no table) | recomputed every call from posted ledger |
| 7 | **Unit** | project svc | **Project** (+ optional Phase) | sale, allocation weight, HPP | `units` row | status available→reserved→sold |
| 8 | **Sales contract** | sale svc | **Unit** | payment schedule, statement, AR, receipt | `sale_contracts` | one active per unit |
| 9 | **Customer payment** | sale svc | **Contract/Unit** | schedule cache, allocations, AR, receipt, ledger | **`journal_entries`** + `payment_allocations` (sub-ledger) | posts Dr Bank / Cr Uang Muka or Piutang |
| 10 | **Revenue recognition (BAST)** | sale svc | **Unit** | P&L, unit status=sold | **`journal_entries`** (Event 3) + `sale_records` (snapshot) | atomic, immutable; unit→sold |
| 11 | **HPP** | sale svc @ BAST | **Unit/SaleRecord** | P&L (gross profit) | `sale_records.hpp_*` **snapshot** (frozen) + Event 4 journal | frozen at BAST; **does not follow later cost** |
| 12 | **Journal** | ledger svc | (per source event) | ALL reports, allocation, HPP, AR | **`journal_entries`/`journal_lines`** — the master SoT | append-only; correction via reversing entry |
| 13 | **Financial reports** | reporting svc | — | user | derived from posted journals | live query |

**Master source of truth = `journal_entries` / `journal_lines` (posted).** Everything
financial should derive from it. `cost_entries.amount`, `sale_records.hpp_*`,
`payment_schedules.paid_amount` are *display copies / snapshots* of ledger truth — mostly
fine, except where a consumer reads the copy with different filters than the ledger (see §3).

---

## 2. Current architecture diagram

```
                              ┌─────────────┐
                              │   PROJECT   │ (root of everything)
                              └──────┬──────┘
             ┌───────────────┬───────┼─────────────┬──────────────────┐
             ▼               ▼       ▼             ▼                  ▼
       ┌──────────┐   ┌───────────┐ ┌──────┐  ┌──────────┐   ┌────────────────┐
       │  PHASE   │   │  RAB /    │ │ UNIT │  │  COST    │   │ ALLOCATION_CFG │
       │(optional)│   │BudgetPlan │ │      │  │  ENTRY   │   │ (1/project;    │
       └──────────┘   │(proj/phase│ └──┬───┘  │(proj +   │   │  basis)        │
                      │ CATEGORY) │    │      │ opt unit)│   └───────┬────────┘
                      └─────┬─────┘    │      └────┬─────┘           │
                            │          │           │ posts           │ basis
                    budget_ │          │           ▼                 ▼
                    item_id │          │      ┌─────────────────────────────┐
                    (OPTIONAL,│        │      │  JOURNAL (Dr Persediaan 1-3x │
                     metadata)│        │      │   / Cr Bank|Hutang) DRAFT→POST│
                            │  │       │      └───────────────┬─────────────┘
                            ▼  ▼       │                      │ posted lines
                   ┌──────────────┐    │        ┌─────────────▼──────────────┐
                   │ RAB vs       │    │        │  ALLOCATION (LIVE compute) │
                   │ REALISASI    │    │        │  Direct(unit) + Allocated  │
                   │ (report)     │    │        │  (project-wide ÷ weight)   │
                   │ reads        │◄───┼────────│  → per-unit UnitCostBreakdn│
                   │ cost_entries │  DISAGREE   └─────────────┬──────────────┘
                   │ (incl DRAFT) │  (draft vs                │ GetUnitCost
                   └──────────────┘   posted)                 ▼
                                            SALE ┌────────────────────────┐
                                      CONTRACT──►│ BAST: Event 3 Revenue  │
                                       PAYMENT──►│ Event 4 HPP = SNAPSHOT  │
                                                 │ of live alloc → frozen  │
                                                 │ sale_records.hpp_*      │
                                                 └────────────┬───────────┘
                                                              ▼
                                                 ┌────────────────────────┐
                                                 │ REPORTS (Neraca, P&L,  │
                                                 │ Pipeline, RAB-Real...) │
                                                 └────────────────────────┘
```

**Notice**: RAB has **no arrow into HPP or allocation**. Its only consumer is the
RAB-vs-Realisasi report — and that report reads cost from a *different filter* than HPP does.

---

## 3. Business flow diagram (with breaks marked)

```
  OWNER/PM              COST CONTROLLER            ACCOUNTANT              SYSTEM RESULT
  ────────              ───────────────            ──────────              ─────────────
1 Create Project ──────────────────────────────────────────────────────► projects row
2 Create RAB (per project/phase, by category) ─────────────────────────► budget_plans (draft)
3 Approve RAB ─────────────────────────────────────────────────────────► status=active
        │
        │  ✗ BREAK #A: approval gates NOTHING. Cost can be recorded with
        │              no RAB, no approval, no budget_item link.
        ▼
4 Add Units ───────────────────────────────────────────────────────────► units (available)
5              Record Cost (Dr Persediaan / Cr Bank|Hutang) ────────────► cost_entry + DRAFT journal
        │
        │  ✗ BREAK #B: cost is DRAFT until a separate "Post" click.
        │              RAB-vs-Realisasi counts the draft; HPP does NOT.
        ▼
6              Post Cost ──────────────────────────────────────────────► journal posted
7 Set allocation basis + Run allocation ───────────────────────────────► LIVE compute (nothing saved)
        │
        │  ✗ BREAK #C: allocation result is never persisted. "Run" only
        │              writes an audit log with a single scalar total.
        ▼
8              Create Contract → Receive Payment ─────────────────────► sale_contract, termin, allocations
9 Process BAST ────────────────────────────────────────────────────────► Event 3 revenue + Event 4 HPP
        │                                                                  HPP = snapshot of live alloc
        │  ✗ BREAK #D: costs recorded AFTER this BAST re-spread over ALL
        │              units (incl. this sold one) on the next live
        │              allocation, but the frozen HPP never updates →
        │              downstream unit costs drift; no cost cutoff.
        ▼
10 View Reports (P&L, RAB-Realisasi) ──────────────────────────────────► may not reconcile (Break B/D)
```

**Navigation reality (Break #E — the user's journey is scattered):**

```
 Step:   Create   RAB      Approve  Add     Record   Post    Run      Sell/    View
         Project  create   RAB      Unit    Cost     Cost    Alloc    Pay/BAST Report
 Menu:   Proyek → RAB    → RAB    → Proyek→ Biaya  → Biaya → Proyek → Penjual→ Laporan
         (side)   (side    (same)   (tab)   (side    (same)  (tab     (side)   (side)
                  picker→          detail   picker→          "Alokasi
                  proj/rab)        tab)     proj/biaya)      HPP")
         └── 5–6 top-level menu switches; RAB & Biaya reached via project PICKERS,
             not from the project itself. Project detail has NO RAB tab. ──┘
```

---

## 4. Problems found

### P0 — correctness / accounting integrity

**P0-1. Draft-vs-posted cost divergence (two disagreeing "cost" numbers).**
`RAB-vs-Realisasi` sums `cost_entries.amount` where `journal_entry_id > 0` — this **includes
unposted drafts** (`budget/repository.go:269-289`). Allocation & accumulated-cost/HPP sum
**posted** `journal_lines` where `je.posted_at IS NOT NULL` (`allocation/repository.go:99-133`).
→ A drafted-but-unposted cost shows in RAB realisasi but is invisible to HPP. The two reports
will not reconcile. An accountant cannot trust "realisasi vs actual cost of goods."

**P0-2. Post-BAST cost cutoff / live-allocation drift.**
Allocation is a **live recompute** (`allocation/service.go:112-129`; no persisted result).
HPP is a **frozen snapshot** at BAST (`sale_records.hpp_*` + Event 4). The allocation pool has
**no exclusion of already-sold units** — a cost recorded after a unit's BAST re-spreads over
all units including the sold one, but the sold unit's recognized HPP never updates. Result:
(a) unsold units' reported cost shifts whenever a sibling sells + new costs land; (b) the sold
unit's frozen HPP no longer reconciles with a fresh live allocation; (c) there is no concept of
"cost cutoff at BAST." This is the classic real-estate cost-recognition problem, unhandled.

**P0-3. HPP correctness silently depends on ledger state + allocation config at the BAST instant.**
Because allocation is never persisted, HPP is only as correct as (a) which costs happen to be
*posted* at that instant and (b) whether an allocation-config exists for project-wide costs.
Record a cost late, or post it late, and the HPP is simply wrong — with no audit of "what the
allocation was when we recognized this unit."

### P1 — missing business relationships

**P1-1. RAB is optional and disconnected.** `cost_entries.budget_item_id` is nullable and
documented "metadata saja" (`cost/service.go:71`). Budget approval gates nothing. You can run
the entire cost→allocation→HPP chain without ever creating or approving a RAB. There is no
budget control (e.g., warn/block when actual exceeds approved budget).

**P1-2. `marketing` / `other` RAB categories are permanent dead-ends.** Budget has 6 categories;
`cost_entries.category` only accepts `land|hard|soft|financing` (`domain/cost_category.go`).
`ToCostCategory()` returns `("",false)` for marketing/other, so their realisasi is **structurally
always 0**, and cost entries can never link to those budget items. Marketing spend has **no
posting path in the cost module at all** (the Biaya form tells users to use a manual Journal —
which cannot link to `budget_item_id`). So a developer can never see "marketing budget vs actual."

**P1-3. `construction`↔`hard` bridged by a single alias.** RAB says `construction`, cost/HPP say
`hard`; reconciled only via `ToCostCategory()` (`budget/model.go:55`). Any raw-string comparison
elsewhere silently mismatches. Fragile naming for the same concept.

**P1-4. Allocation result is not auditable.** `allocation_executions` stores only a scalar total
(`allocation/model.go`), not per-unit/per-category amounts. You cannot answer "what cost did we
allocate to unit A-01 on the day we BAST'd it?" — critical for a cost controller and for audit.

### P2 — navigation / workflow UX (relationship visible to user)

**P2-1. RAB has no home in the project.** Project detail (`/proyek/[id]`) tabs are Papan Unit /
Fase / Biaya / Alokasi HPP — **no RAB tab**. RAB is only a top-level sidebar item → project
*picker* → `/proyek/[id]/rab`. From a project you cannot reach its own RAB.

**P2-2. "Biaya" is overloaded.** The project "Biaya" *tab* shows allocation **results** (read-only);
the cost-**entry** form is a *different* route `/proyek/[id]/biaya` reached via the sidebar Biaya
picker. Same word, two routes, no link between them.

**P2-3. Forward chain unlinked.** After creating RAB → no link to record cost; after recording cost
→ no link to run allocation; after allocation → no link to sell. Every hop relies on the sidebar.

**P2-4. Two overlapping unit-detail pages.** `/proyek/[id]/unit/[unitId]` (accounting view) and
`/penjualan/[unitId]` (transaction view) overlap in purpose and are only loosely cross-linked.

---

## 5. Answers to the specific RAB questions

- **Is RAB attached to project or unit?** → **Project (or Phase). Never unit.** RAB is a
  category-level plan (land/construction/soft/financing/marketing/other), versioned, one active
  per (project, phase).
- **How does RAB affect unit HPP?** → **It does not.** HPP = actual accumulated cost (posted
  cost_entries → allocation → snapshot at BAST). RAB never feeds HPP. *(This is correct GAAP —
  HPP is actual, not budget — but the product currently gives RAB no operational role beyond a
  variance report, and that report is itself broken by P0-1/P1-2.)*
- **How does actual cost compare against RAB?** → RAB-vs-Realisasi report: budget by category vs
  `cost_entries.amount` by category (incl. drafts — P0-1); per-item via optional `budget_item_id`.
- **How does the Cost module consume RAB?** → Only an **optional metadata link** (`budget_item_id`),
  category-validated at entry. Not required; not a control.
- **How does Accounting consume Cost?** → Cost posts a balanced journal **Dr Persediaan Real Estat
  (1-3xxx) / Cr Bank (1-13xx) or Hutang Usaha (2-1000)**; draft until `PostCostEntry`. The journal
  is the SoT; `cost_entries.amount` is a display copy. Cost never touches expense (5-xxxx); HPP
  (5-1000) is debited only later at BAST Event 4.

---

## 6. Recommended changes (no code yet — direction for approval)

Prioritized. These are **architectural relationship fixes**, not new features.

### Must-fix (P0 — data integrity)
1. **Unify the cost filter.** Pick one rule everywhere: RAB-Realisasi and HPP/allocation must both
   read **posted** ledger only (recommended), OR both include drafts. Recommendation: **posted-only
   everywhere**, and make cost entry **post-on-create** (drop the draft step) unless a real approval
   workflow is wanted. Resolves P0-1.
2. **Persist allocation snapshots + define a cost cutoff.** Store per-unit/per-category allocated
   amounts at the moment of BAST (and exclude already-sold units from the live pool, or adopt an
   explicit "reallocate remaining pool over unsold units" policy). This makes HPP auditable and
   stops post-BAST drift. Resolves P0-2, P0-3, P1-4. **This is the biggest and should be a design
   discussion of its own** (cost-cutoff policy is an accounting-policy decision — flag for the
   accountant/owner).

### Should-fix (P1 — relationships)
3. **Decide RAB's operational role.** Either (a) keep RAB as pure planning but make the variance
   report correct and complete, or (b) elevate RAB to a control (warn when posted actual exceeds
   approved budget per category/item). Pick one; today it's neither.
4. **Fix the taxonomy.** Reconcile `construction`↔`hard` (rename one, or centralize all mapping
   through one enum), and give `marketing`/`other` a real actuals path (a cost/expense posting that
   links to `budget_item_id`) — or remove them from RAB if they're truly out of scope. Resolves
   P1-2, P1-3.

### Nice-to-fix (P2 — workflow navigation)
5. **Make the project the hub.** Add a **RAB tab** to `/proyek/[id]`; unify the "Biaya" tab (entry +
   results in one place, or clearly linked); add forward "next step" links along RAB → Cost →
   Allocation → Sell so the user stops bouncing to the sidebar. Consolidate the two unit-detail
   pages or clearly delineate them.

---

## 7. Suggested workflow (target state)

```
 Project ─► [tab] RAB (create → approve) ─► [tab] Biaya (record cost, sees RAB budget inline,
   │                                                    warns on overrun) ─► post
   │                                                            │
   │                                                            ▼
   ├─► [tab] Alokasi HPP (set basis, preview per-unit cost, SNAPSHOT on run) 
   │                                                            │
   ├─► [tab] Papan Unit (add unit) ─► Unit ─► "Jual" ──────────┼──► Kontrak → Bayar → BAST
   │                                                            │       (HPP = frozen snapshot)
   └─────────────────────────────── all within /proyek/[id] ───┘
                                                                        ▼
                                                          Piutang / Invoice / Laporan (read)
```

Everything project-scoped lives under the project; only cross-project reads (AR, Invoice,
Jurnal, Reports, Pajak) stay top-level.

> **STOP — no code.** Awaiting review/approval of §6 before any implementation. The P0-2 cost-cutoff
> decision in particular needs an explicit accounting-policy call from you.
