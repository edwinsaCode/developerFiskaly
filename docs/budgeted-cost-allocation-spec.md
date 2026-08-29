# Budgeted Cost Allocation — Frozen Spec

> Status: **FROZEN (2026-07-02).** Design contract for the HPP method chosen in the roadmap
> (`docs/property-erp-implementation-roadmap.md`, Decision B). Implementation must not deviate
> from this without re-freezing. Companion to the Domain Audit.

This document freezes four things required before implementing the HPP change (P0-2):
naming, allocation basis, HPP-vs-expense category split, and backward compatibility.

---

## 1. Naming — "Budgeted Cost Allocation" (not "B2 / PSAK 44")

- Internal name everywhere (code, docs, UI): **Budgeted Cost Allocation (BCA)** /
  *"alokasi biaya teranggarkan"*.
- **Do NOT** claim PSAK 44 / hard regulatory compliance in code, UI, or comments. PSAK 44 is the
  *inspiration* for the method; we do not certify compliance. Reason: avoid an assertion we
  cannot stand behind in an audit.
- Method identifier for data/APIs: `hpp_method = "budgeted"` (vs legacy `"actual"`).

## 2. Allocation basis — explicit definition (frozen)

Basis is a **per-project** config: `allocation_configs.basis` (one row per project).

| Basis value | Unit weight `w_u` | Status |
|---|---|---|
| `saleable_area` | `units.saleable_area` (m²) | **supported** |
| `sales_value` | `units.list_price` (Rp) | **supported** |
| `weighted` | explicit per-unit manual weight | **DEFINED, NOT built this phase** (would need a `units.alloc_weight` column; add only if a customer needs it) |

**Formula (frozen).** For a project with approved-RAB budgeted total `B_c` per HPP-category
`c ∈ {land, hard, soft, financing}`:

```
budgeted_HPP(unit u, category c) = B_c × ( w_u / Σ_all-units w_v )
budgeted_HPP(unit u)             = Σ_c budgeted_HPP(u, c)
```

- Distribution uses **largest-remainder** (existing `Money.Allocate`) so `Σ_units == B_c` exactly
  per category (Invariant #3). Reuse the existing allocation engine; only the **pool source**
  changes from *actual posted cost* to *approved-RAB budgeted total*.
- **Which units are in the denominator:** ALL units of the project that exist at BAST time
  (available + reserved + sold), so a unit's budgeted HPP is stable regardless of sale order.
  *(This is the key B2 property; contrast with actual-cost B1 which shifts by timing.)*
- **No config ⇒ hard error at BAST** (`ErrAllocationBasisMissing`), same shape as the RAB gate.
  We never silently default a basis.

## 3. Which RAB categories enter HPP vs Expense (frozen)

| RAB category (`budget_items.category`) | Treatment | Account chain |
|---|---|---|
| `land` | **HPP (capitalized)** | Dr Persediaan 1-3000 → BAST Dr HPP 5-1000 / Cr 1-3000 |
| `construction` (≡ cost `hard`) | **HPP (capitalized)** | 1-3100 → HPP |
| `soft` | **HPP (capitalized)** | 1-3200 → HPP |
| `financing` | **HPP (capitalized)** | 1-3300 → HPP |
| `marketing` | **Period EXPENSE (never HPP)** | Dr **5-3000 Beban Pemasaran** / Cr Bank\|Hutang, when incurred |
| `other` | **Period EXPENSE (never HPP)** | Dr **5-4000 Beban Umum & Administrasi** / Cr Bank\|Hutang |

- **Budgeted HPP pool** = `Σ(approved-RAB land + construction + soft + financing)` ONLY.
  Marketing/other are excluded from the HPP pool by definition.
- Expense categories are recognized as **period cost when incurred** (not capitalized, not
  deferred to BAST). They flow to P&L directly, below gross profit.
- `construction` ↔ `hard` remains an **alias**, but must be resolved through the single
  canonical mapping (`budget.ToCostCategory` / new `budget.HPPAccount`) — no raw-string compares.

## 4. Backward compatibility for existing sale_records (frozen)

- Existing `sale_records` were recognized under the **legacy actual-cost** method. They are
  **NOT recomputed, NOT migrated.** Their frozen `hpp_*` values stand.
- Distinguisher: new nullable column `sale_records.allocation_snapshot_id` (added in P0-2).
  `NULL ⇒ legacy actual-cost HPP`; non-null ⇒ budgeted method (drill-down available).
- Optional convenience column `sale_records.hpp_method VARCHAR(20)` defaulting `'actual'` for
  existing rows, `'budgeted'` for new — for clear reporting. (Nullable/defaulted → additive.)
- Reports must render both: legacy rows show their stored HPP as-is; no reconciliation attempted
  against the new snapshot for legacy rows.
- The **budgeted method applies only to BASTs recorded after the feature ships.** No dual-run,
  no retroactive re-recognition.

---

## Locked invariants added by this method

- **BCA-1:** `Σ_units budgeted_HPP(·, c) == approved-RAB budgeted total for c`, per category, exactly
  (largest-remainder). Marketing/other never appear here.
- **BCA-2:** A unit cannot be BAST'd unless (a) an **approved active RAB** exists for its
  project/phase and (b) an **allocation basis** is configured. Both are hard gates.
- **BCA-3:** Legacy `sale_records` (snapshot_id NULL) are immutable and untouched.
- **BCA-4:** HPP snapshot at BAST is immutable; corrections only via the completion **true-up**
  (P0-4), never by editing the snapshot.

> This spec is FROZEN. P0-2/P0-3/P0-4 implement exactly this. Any change requires re-approval.
