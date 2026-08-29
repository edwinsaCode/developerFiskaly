# esaProperti — Database Schema (Flowchart & ERD)

Skema aktual per migrasi `000001`–`000032`. Dua diagram:
- **§1 Flowchart** — semua tabel + relasi, dikelompokkan per domain (Mermaid `flowchart`).
- **§2 ER diagram** — atribut kunci tiap tabel (Mermaid `erDiagram`).

Konvensi:
- **`tenant_id` ada di SETIAP tabel** → isolasi multi-tenant via GORM global scope
  (Invariant #6). Untuk kejelasan, relasi ke `tenants` tidak digambar ke semua tabel.
- **FK** = foreign key fisik (ditegakkan DB). **logical** = relasi tenant-scoped tanpa
  FK fisik (konvensi proyek; dibuktikan integration test).
- Uang `DECIMAL(20,4)` (Inv #2); `journal_*` append-only (Inv #1/#5).
- `schema_migrations` = bookkeeping golang-migrate (tidak digambar).

---

## 1. Flowchart — semua tabel & relasi

```mermaid
flowchart TD
  %% ---------- TENANCY ----------
  subgraph TEN[Tenancy & Auth]
    tenants
    users
  end
  tenants -->|FK| users

  %% ---------- MASTER PROYEK ----------
  subgraph MST[Master Proyek]
    projects
    project_phases
    units
  end
  projects -->|FK| project_phases
  projects -->|FK| units
  project_phases -->|FK| units

  %% ---------- LEDGER INTI ----------
  subgraph LED[Ledger Inti - source of truth]
    accounts
    journal_entries
    journal_lines
    accounting_periods
  end
  accounts -->|FK| journal_lines
  journal_entries -->|FK| journal_lines
  accounting_periods -.->|gate open/closed| journal_entries

  %% ---------- BIAYA: RAB & REALISASI ----------
  subgraph COST[Biaya: RAB & Realisasi]
    budget_plans
    budget_items
    cost_entries
    allocation_configs
    allocation_config_versions
    allocation_executions
  end
  budget_plans -->|FK| budget_items
  projects -->|FK| cost_entries
  journal_entries -->|FK| cost_entries
  budget_items -->|FK opsional| cost_entries
  projects -->|logical| budget_plans
  projects -->|logical| allocation_configs
  allocation_configs -->|logical: versions D1| allocation_config_versions
  projects -->|logical| allocation_executions

  %% ---------- PENJUALAN & HPP ----------
  subgraph SALE[Penjualan, Pembayaran & HPP]
    sale_contracts
    payment_schedules
    termin_payments
    payment_allocations
    credit_applications
    sale_records
    allocation_snapshots
    allocation_snapshot_lines
  end
  units -->|logical| sale_contracts
  sale_contracts -->|logical| payment_schedules
  units -->|logical| termin_payments
  termin_payments -->|logical| payment_allocations
  payment_schedules -->|logical| payment_allocations
  termin_payments -->|logical| credit_applications
  payment_schedules -->|logical| credit_applications
  units -->|logical: 1 BAST/unit| sale_records
  journal_entries -->|logical: revenue+COGS| sale_records
  sale_records -->|logical: snapshot_id| allocation_snapshots
  units -->|logical| allocation_snapshots
  budget_plans -->|logical: pin RAB+version| allocation_snapshots
  allocation_config_versions -->|logical: pin basis| allocation_snapshots
  allocation_snapshots -->|FK| allocation_snapshot_lines

  %% ---------- COMPLETION & TRUE-UP (P0-4) ----------
  subgraph TU[Completion & True-up - P0-4]
    project_completion_events
    hpp_trueup_runs
    hpp_trueup_lines
  end
  projects -->|logical: scope| project_completion_events
  projects -->|logical: scope| hpp_trueup_runs
  allocation_config_versions -->|logical: basis IMPL-1| hpp_trueup_runs
  journal_entries -->|logical: adj journal UNIQUE| hpp_trueup_runs
  hpp_trueup_runs -->|FK| hpp_trueup_lines
  units -->|logical| hpp_trueup_lines
  allocation_snapshots -->|logical: asal budgeted| hpp_trueup_lines
  journal_entries -->|logical: adj line| hpp_trueup_lines

  %% ---------- BILLING & PAJAK ----------
  subgraph BILL[Billing & Pajak]
    invoices
    invoice_sequences
    receipts
    receipt_sequences
    tax_rates
    tax_obligations
    tax_payments
  end
  units -->|logical| invoices
  payment_schedules -->|logical| invoices
  termin_payments -->|logical| receipts
  units -->|logical| tax_obligations
  tax_rates -.->|logical| tax_obligations
  tax_obligations -->|logical| tax_payments
```

---

## 2. ER diagram — atribut kunci

```mermaid
erDiagram
  tenants ||--o{ users : "has"
  projects ||--o{ project_phases : "phases (FK)"
  projects ||--o{ units : "units (FK)"
  project_phases ||--o{ units : "phase (FK)"

  accounts ||--o{ journal_lines : "account (FK)"
  journal_entries ||--o{ journal_lines : "lines (FK)"
  journal_entries ||--o{ cost_entries : "posting (FK)"
  projects ||--o{ cost_entries : "project (FK)"
  budget_plans ||--o{ budget_items : "items (FK)"
  budget_items ||--o{ cost_entries : "link (FK, opt)"
  allocation_configs ||--o{ allocation_config_versions : "versions"

  units ||--o| sale_records : "1 BAST (logical)"
  sale_records ||--o| allocation_snapshots : "snapshot (logical)"
  allocation_snapshots ||--o{ allocation_snapshot_lines : "lines (FK)"
  budget_plans ||--o{ allocation_snapshots : "pin RAB (logical)"
  allocation_config_versions ||--o{ allocation_snapshots : "pin basis (logical)"

  sale_contracts ||--o{ payment_schedules : "schedule (logical)"
  termin_payments ||--o{ payment_allocations : "alloc (logical)"
  payment_schedules ||--o{ payment_allocations : "target (logical)"

  projects ||--o| project_completion_events : "completion (logical)"
  projects ||--o{ hpp_trueup_runs : "runs (logical)"
  hpp_trueup_runs ||--o{ hpp_trueup_lines : "lines (FK)"
  allocation_config_versions ||--o{ hpp_trueup_runs : "basis (logical)"
  allocation_snapshots ||--o{ hpp_trueup_lines : "budgeted src (logical)"
  journal_entries ||--o| hpp_trueup_runs : "adj journal (logical)"

  tax_obligations ||--o{ tax_payments : "payments (logical)"

  projects {
    bigint id PK
    bigint tenant_id
    string status
  }
  units {
    bigint id PK
    bigint tenant_id
    bigint project_id FK
    bigint phase_id FK
    decimal saleable_area
    decimal list_price
    string status
  }
  journal_entries {
    bigint id PK
    bigint tenant_id
    datetime posted_at
    string source
    string reference
  }
  journal_lines {
    bigint id PK
    bigint journal_entry_id FK
    bigint account_id FK
    decimal debit
    decimal credit
    bigint unit_id
  }
  budget_plans {
    bigint id PK
    bigint project_id
    int version
    string status
    string active_key
  }
  allocation_config_versions {
    bigint id PK
    bigint project_id
    int version
    string basis
    string active_key
  }
  sale_records {
    bigint id PK
    bigint unit_id
    string hpp_method
    bigint allocation_snapshot_id
    bigint budget_plan_id
    int budget_plan_version
    decimal hpp_land_hard_soft_financing
  }
  allocation_snapshots {
    bigint id PK
    bigint unit_id
    bigint budget_plan_id
    bigint allocation_config_version_id
    string basis
    decimal hpp_total
  }
  allocation_snapshot_lines {
    bigint id PK
    bigint snapshot_id FK
    bigint unit_id
    string unit_name_snapshot
    string accounting_class
    string basis_type
    decimal basis_value
    decimal allocation_percentage
    decimal amount
  }
  project_completion_events {
    bigint id PK
    bigint project_id
    bigint phase_id
    string status
    datetime completed_at
    datetime actual_cost_finalized_at
  }
  hpp_trueup_runs {
    bigint id PK
    bigint project_id
    bigint phase_id
    string status
    bigint allocation_config_version_id
    decimal variance_total
    bigint journal_id
  }
  hpp_trueup_lines {
    bigint id PK
    bigint run_id FK
    bigint unit_id
    string unit_name_snapshot
    string category
    bool is_sold
    decimal budgeted_amount
    decimal actual_amount
    decimal variance_amount
    bigint journal_entry_id
  }
```

---

## 3. Physical FK (ditegakkan DB) — ringkas

| Child | Kolom | → Parent |
|---|---|---|
| users | tenant_id | tenants |
| project_phases | project_id | projects |
| units | project_id, phase_id | projects, project_phases |
| journal_lines | journal_entry_id, account_id | journal_entries, accounts |
| cost_entries | project_id, journal_entry_id, budget_item_id | projects, journal_entries, budget_items |
| budget_items | budget_plan_id | budget_plans |
| allocation_snapshot_lines | snapshot_id | allocation_snapshots |
| hpp_trueup_lines | run_id | hpp_trueup_runs |

Selebihnya = **logical FK** (lihat label "logical" di diagram).
