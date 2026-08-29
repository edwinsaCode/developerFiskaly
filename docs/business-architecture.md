# esaProperti — Business Architecture (Single Source of Truth) — **v3 (Blueprint)**

> Blueprint implementasi ERP. **Project = Aggregate Root/HUB.**
> **Ledger (Journal Entry → Journal Line → COA → Account Balance) = backbone.**
> **Inventory & AR = derived balance** dari saldo akun COA (bukan dokumen, bukan
> langsung dari Journal Line). **Read Model membaca Account Balance / Journal Line.**
> **Snapshot = historical record**, beku saat BAST, independen dari RAB aktif.
> **Project Lifecycle** menjadi dasar seluruh state transition & business rule.

## Legend — 4 jenis panah
| Panah | Arti |
|---|---|
| `──▶` **Business Flow** | alur/relasi bisnis: membuat, memiliki, memicu, mengurangi outstanding |
| `══▶` **Accounting Posting** | transaksi memposting ke Ledger (Dr/Cr) |
| `╌╌▶` **Read-only Dependency** | membaca / derivasi saldo, tidak menulis |
| `╌❄╌▶` **Immutable Snapshot** | pembekuan satu kali, setelah itu independen |

```mermaid
flowchart TB
  %% ===================== LEGEND =====================
  subgraph LEGEND["LEGEND — jenis panah"]
    direction LR
    la1["src"] -->|Business Flow| la2["dst"]
    lb1["src"] ==>|Accounting Posting| lb2["dst"]
    lc1["src"] -.->|Read-only Dependency| lc2["dst"]
    ld1["src"] -. "❄ Immutable Snapshot" .-> ld2["dst"]
  end

  %% ===================== GLOBAL MASTER =====================
  subgraph GLOBAL["🟦 GLOBAL MASTER (lintas project)"]
    direction LR
    Tenant["Tenant"]
    User["User (peran)"]
    Buyer["Buyer"]
    Vendor["Vendor/Supplier"]
    TaxRate["Tax Rate"]
  end

  %% ===================== LEDGER BACKBONE + DERIVED BALANCES =====================
  subgraph LEDGER["🟥 ACCOUNTING — LEDGER BACKBONE (source of truth, append-only)"]
    direction TB
    AcctPeriod["Accounting Period (open/closed)"]
    JournalEntry["Journal Entry (balanced)"]
    JournalLine["Journal Line (debit/kredit)"]
    COA["Chart of Accounts (COA)"]
    AccountBalance["Account Balance<br/>(saldo per akun COA)"]
    subgraph DERIVED["Derived Balances (slice dari Account Balance)"]
      direction LR
      Inventory["Inventory / Persediaan<br/>(saldo COA 1-3xxx)"]
      AR["Piutang / AR<br/>(saldo COA 1-2000)"]
    end
  end
  JournalEntry --> JournalLine --> COA
  AcctPeriod -.->|gate periode| JournalEntry
  COA -.->|akumulasi saldo per akun| AccountBalance
  AccountBalance -.->|saldo 1-3xxx| Inventory
  AccountBalance -.->|saldo 1-2000| AR

  %% ===================== PROJECT AGGREGATE ROOT =====================
  subgraph PRJ["★ PROJECT — AGGREGATE ROOT · semua aktivitas berpusat di sini"]
    direction TB

    subgraph LIFE["🔵 PROJECT LIFECYCLE (state machine — dasar semua business rule)"]
      direction LR
      LPlan["Planning"] -->|approve RAB| LCon["Construction"]
      LCon -->|mulai pemasaran| LSell["Selling"]
      LSell -->|serah terima unit| LHO["Hand Over"]
      LHO -->|konstruksi selesai| LComp["Completed"]
      LComp -->|finalize biaya| LTU["True-up"]
      LTU -->|posting selesai| LClosed["Closed"]
    end

    Project(["★ PROJECT (HUB)"])
    Phase["Phase"]
    Unit["Unit (produk dijual)"]

    subgraph PLAN["🟧 Perencanaan (fase Planning)"]
      RAB["Budget Plan / RAB<br/>(planning · SOFT CONTROL)"]
      BudgetItem["Budget Item"]
      AllocBasis["Allocation Basis (+version)"]
    end
    subgraph COSTBOX["🟧 Realisasi Biaya (fase Construction)"]
      CostEntry["Cost Entry (biaya aktual)"]
    end
    subgraph SALES["🟧 Penjualan & Penerimaan (fase Selling)"]
      SaleContract["Sale Contract (PPJB)<br/>+ Outstanding/AR"]
      PaymentSchedule["Payment Schedule (termin)"]
      Invoice["Invoice (tagihan)"]
      Payment["Payment (uang diterima)"]
      PaymentAlloc["Payment Allocation"]
      BuyerCredit["Buyer Credit (overpay)"]
      Receipt["Receipt (kwitansi)"]
    end
    subgraph RECOG["🟧 Pengakuan & HPP (fase Hand Over)"]
      BAST["BAST (serah terima)"]
      SaleRecord["Sale Record (Pendapatan + HPP)"]
      Snapshot["🟪 Allocation Snapshot<br/>(bukti HPP beku:<br/>RAB version + basis + identitas unit)"]
      TaxObligation["Tax Obligation (PPh Final)"]
      TaxPayment["Tax Payment"]
    end
    subgraph CLOSE["🟧 Penyelesaian (fase Completed → True-up → Closed)"]
      Completion["Project Completion (kunci biaya aktual)"]
      TrueUp["HPP True-up (budgeted → aktual)"]
    end
    Report["🟩 Financial Report<br/>L/R · Neraca · RAB vs Realisasi · AR Aging"]
  end

  %% ---- Project HUB memiliki semua modul ----
  Tenant --> Project
  Project --> Phase --> Unit
  Project --> RAB
  Project --> CostEntry
  Project --> Completion
  Project --> Report

  %% ---- Perencanaan (RAB soft control) ----
  RAB --> BudgetItem
  CostEntry -.->|optional ref · SOFT control| BudgetItem
  RAB -.->|RAB vs Realisasi| Report

  %% ---- Realisasi biaya → posting ----
  Vendor --> CostEntry
  CostEntry ==>|🧾 Dr Persediaan / Cr Bank-Hutang| JournalEntry

  %% ---- Penjualan ----
  Buyer --> SaleContract
  Unit --> SaleContract
  SaleContract --> PaymentSchedule
  PaymentSchedule --> Invoice

  %% ---- FLOW PAYMENT lengkap: bisnis (kurangi AR) → posting ----
  Buyer --> Payment
  Payment --> PaymentAlloc
  PaymentAlloc --> PaymentSchedule
  PaymentAlloc --> Invoice
  PaymentAlloc -->|kurangi Outstanding / AR kontrak| SaleContract
  PaymentAlloc -->|jika overpay| BuyerCredit
  BuyerCredit -->|apply ke cicilan lain| PaymentSchedule
  Payment --> Receipt
  Payment ==>|🧾 Dr Bank · Cr Uang Muka atau Piutang| JournalEntry

  %% ---- Pengakuan BAST + Snapshot ----
  Unit --> BAST
  RAB -.->|baseline HPP budgeted (soft)| BAST
  AllocBasis -.->|basis alokasi| BAST
  TaxRate -.->|tarif| BAST
  BAST --> SaleRecord
  BAST ==>|🧾 Dr Piutang/UM · Cr Pendapatan · Dr HPP · Cr Persediaan| JournalEntry
  BAST -. "❄ freeze RAB version + basis + identitas unit" .-> Snapshot
  BAST --> TaxObligation
  TaxObligation --> TaxPayment
  TaxObligation ==>|🧾 akrual PPh Final| JournalEntry
  TaxPayment ==>|🧾 pelunasan| JournalEntry

  %% ---- Completion memicu True-up; True-up membaca input ----
  Completion -->|memicu| TrueUp
  Snapshot -.->|input budgeted (bukan RAB aktif)| TrueUp
  Inventory -.->|input biaya aktual (saldo Persediaan)| TrueUp
  SaleRecord -.->|input unit terjual| TrueUp
  TrueUp ==>|🧾 Dr/Cr HPP ↔ Persediaan| JournalEntry

  %% ---- Read Model membaca LEDGER (Account Balance / Journal Line) ----
  AccountBalance -.->|baca saldo akun| Report
  JournalLine -.->|baca detail transaksi| Report

  %% ===================== STYLING =====================
  classDef master  fill:#dbeafe,stroke:#1e40af,color:#0b1f4d;
  classDef txn     fill:#ffedd5,stroke:#c2410c,color:#4a1d05;
  classDef ledger  fill:#fee2e2,stroke:#b91c1c,color:#4a0505;
  classDef derived fill:#fff7ed,stroke:#b91c1c,color:#4a0505,stroke-dasharray:4 3;
  classDef hist    fill:#ede9fe,stroke:#6d28d9,color:#2e1065;
  classDef read    fill:#dcfce7,stroke:#15803d,color:#052e16;
  classDef hub     fill:#1e40af,stroke:#0b1f4d,color:#ffffff,font-weight:bold;
  classDef life    fill:#e0e7ff,stroke:#3730a3,color:#1e1b4b,font-weight:bold;
  classDef legend  fill:#f8fafc,stroke:#94a3b8,color:#334155;

  class Tenant,User,Buyer,Vendor,TaxRate,Phase,Unit master;
  class Project hub;
  class RAB,BudgetItem,AllocBasis,CostEntry,SaleContract,PaymentSchedule,Invoice,Payment,PaymentAlloc,BuyerCredit,Receipt,BAST,SaleRecord,Completion,TrueUp,TaxObligation,TaxPayment txn;
  class AcctPeriod,JournalEntry,JournalLine,COA,AccountBalance ledger;
  class Inventory,AR derived;
  class Snapshot hist;
  class Report read;
  class LPlan,LCon,LSell,LHO,LComp,LTU,LClosed life;
  class la1,la2,lb1,lb2,lc1,lc2,ld1,ld2 legend;
```

---

## Project Lifecycle → business rule (state machine)
| State | Aktivitas dominan | Business rule kunci |
|---|---|---|
| **Planning** | Susun & **approve RAB**, set Allocation Basis | RAB harus active sebelum HPP budgeted; basis freeze setelah snapshot pertama |
| **Construction** | **Cost Entry** berjalan → posting kapitalisasi | Cost TIDAK butuh RAB (soft control); posting-only mengisi Persediaan |
| **Selling** | Sale Contract, Payment Schedule, **Payment** | Pra-BAST → Uang Muka; Payment mengurangi Outstanding/AR kontrak |
| **Hand Over** | **BAST** per unit → akui Pendapatan+HPP, freeze Snapshot, PPh Final | HPP budgeted; unit `sold`; snapshot immutable |
| **Completed** | **Project Completion** → kunci biaya aktual (`finalized`) | True-up hanya boleh setelah biaya final |
| **True-up** | **HPP True-up** → rekonsiliasi budgeted→aktual | Hanya unit terjual dijurnal; unsold difinalize; posting immutable |
| **Closed** | Proyek terkunci | Tidak ada perubahan; penjualan unit sisa pakai HPP finalized |

> Catatan: Selling boleh **overlap** Construction (pre-sales) — lifecycle di atas
> adalah urutan *milestone akuntansi*, bukan larangan paralelisme operasional.

## Empat penyempurnaan terakhir (ditegaskan diagram)
1. **Inventory dari saldo COA 1-3xxx** — rantai: Journal Entry → Journal Line → **COA → Account Balance → Inventory**. Bukan langsung dari Journal Line.
2. **Payment mengurangi Outstanding/AR** — `PaymentAlloc ──▶ kurangi Outstanding/AR kontrak` ke Sale Contract, selain `══▶` posting ke Journal. Efek bisnis terlihat, bukan hanya akuntansi.
3. **Read Model membaca Ledger** — `Report ╌╌▶ Account Balance` + `╌╌▶ Journal Line`. **Inventory & AR hanyalah derived balance**, bukan sumber report.
4. **Project Lifecycle** — Planning → Construction → Selling → Hand Over → Completed → True-up → Closed; menjadi dasar seluruh state transition & business rule (tabel di atas).

## Prinsip yang tetap
- **Ledger backbone**: semua `🧾` posting bermuara ke Journal Entry → Journal Line → COA.
- **Snapshot historical**: `╌❄╌▶` beku saat BAST; True-up membaca Snapshot, bukan RAB aktif.
- **Project aggregate root**: semua modul + lifecycle berada dalam boundary Project.

## Cross-cutting layers (lihat `blueprint-domain-additions.md` §9–§10)
Dua lapisan berlaku **lintas modul**, bukan per-screen:
- **Unit Lifecycle (state machine)** — `Available → Booked → Reserved → PPJB → BAST →
  Occupied` (+ Hold/Blocked/Maintenance, dan Cancel → Available). Setiap transisi
  terikat event bisnis (Booking, Payment, Sale Contract, BAST, Cancellation) dan
  tercatat di transition log (audit + time-on-market).
- **Generic Approval Workflow (governance)** — Approval Workflow → Step (config) +
  Approval Request → Action (runtime, polymorphic target). Dipakai ulang oleh RAB,
  Cost, Discount, Refund, Cancellation, Commission, True-up. **Mem-gate posting**
  (mis. True-up tak boleh posting sebelum Request `approved`), **tanpa** dampak ledger.
