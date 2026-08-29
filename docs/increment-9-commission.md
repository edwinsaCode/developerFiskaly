# Increment 9 — Commission Engine — SHIPPED (Implementation Report)

> Status: **SHIPPED + tervalidasi (integration real MySQL).** 2026-07-18.
> Blueprint §3: Rule → Commission → Approval → Payment → Journal + clawback (§1).
> **Dengan ini CORE backend dinyatakan FEATURE COMPLETE** — seluruh domain CORE
> blueprint terbangun. Frontend spec: `increment-9-frontend-spec.md`.
> Mode berikutnya: **PRODUCT COMPLETION** (kualitas produk, bukan domain baru).

## 1. Model (blueprint §3, ledger-centric)

| Peristiwa | Jurnal |
|---|---|
| `payable` (akrual) | `Dr Beban Komisi 5-3100 / Cr Utang Komisi 2-6200` (tag project+unit) |
| `paid` | `Dr 2-6200 / Cr Bank` |
| `cancel` saat payable | reversing akrual (`ledger.Reverse`, chain utuh) |
| **`clawback`** (sale batal SETELAH dibayar) | `Dr Piutang Lain-lain 1-2100 / Cr Beban Komisi 5-3100` (recovery — piutang ke sales) |

**Rule (config):** basis `percent_of_sale` (rate fraksi, mis. 0.025) / `flat_per_unit`
(`tiered` = vocabulary SEAM, ditolak sampai formula terdaftar — pola TaxFormula);
trigger `at_bast` (`at_collection`/`at_lunas` = SEAM); scope NULL-able per
salesperson/proyek/tipe unit; akun konfigurable (default 5-3100/2-6200);
effective dates; provenance rate di-**snapshot** ke entri (append-only audit).

**Lifecycle:** `calculated → approved → payable → paid` + `cancelled`
(pra-paid) + `clawed_back` (paid, hanya via sale batal). Approve ber-gate
opt-in **`TargetCommission`** (enum inert Increment 5 kini hidup — konsumen
approval ke-4 setelah RAB/unit_transition/cancellation). Audit actor per transisi.

**Dua sweep idempoten (pola mark-overdue):**
- `Calculate`: scan BAST non-cancelled ber-atribusi salesperson (kontrak) × rule aktif cocok → entri `calculated`; UNIQUE (sale×rule) = guard dobel.
- `SyncCancellations`: komisi milik sale yang dibatalkan (Increment 8) → auto **cancel** (belum dibayar) atau **clawback** (sudah dibayar) — menutup backlog CX-B3 tanpa coupling antar-package.

## 2. Yang dikirim

- **Migration `000046`** (reversible, teruji down/up): `commission_rules` + `commissions` (CHECK basis/trigger/status, UNIQUE sale×rule, audit kolom) + seed akun **2-6200 Utang Komisi** semua tenant (+ `coa.go`; `5-3100 Beban Komisi Penjualan` sudah ada sejak awal).
- **`internal/commission`**: model+state machine, service (rules CRUD ringan, Calculate, Approve+gate, MakePayable, Pay dgn validasi bank COA-driven, Cancel, Clawback, SyncCancellations, reads), handler + adapter approval, wiring main.go.
- **API**: `POST/GET /commission-rules` · `GET/PUT /commission-rules/{id}(/active)` · `POST /commissions/calculate` · `POST /commissions/sync-cancellations` · `GET /commissions?status=&sales_person_id=` · `GET /commissions/{id}` · `POST .../approve|make-payable|pay|cancel`.

## 3. Test (19 paket unit + 19 integration — semua hijau)

- **Unit:** state machine penuh; `Rule.Matches` (scope×efektif×aktif, NULL=semua); `AmountFor` (persen + pembulatan rupiah bulat Invariant #2, flat, tiered→seam error).
- **Integration:** rule 2.5% → 2 BAST → calculate 2 entri 50jt (sweep kedua = 0, idempoten); A: approve→payable (saldo 5-3100/2-6200 benar)→pay (utang 0, bank −50jt); guard pay-pra-payable; B: payable→cancel (reversing akrual, saldo pulih); **sale A dibatalkan via cancellation → sync → clawed_back otomatis** (5-3100 = 0, 1-2100 = 50jt piutang sales), sync idempoten; tiered ditolak.
- Temuan kecil terdokumentasi: jalur kontrak **legacy** (tanpa scheme flow) tidak menyalin `sales_person_id` — di produksi scheme flow aktif dan mengisinya; fixture test menyetel langsung.

## 4. Migration Impact — satu migration additive+reversible (000046).
## 5. Backward Compatibility — nol perubahan pada modul lain; tenant tanpa rule = nol efek; gate opt-in.

## 6. Dashboard Impact
- Kartu Perhatian: **Komisi menunggu approval** (`status=calculated`) · **Komisi siap dibayar** (`status=payable`).
- Tile **Utang Komisi outstanding** = saldo 2-6200 (derived, trial-balance existing).
- Bahan **Sales Performance** (Product Sprint): `GET /commissions?sales_person_id=` = data bonus/komisi per sales yang owner minta (Prioritas #4) — kini tersedia dari engine yang sama.

## 7. Report Impact
- **L/R**: Beban Komisi muncul saat akrual (bertag project → ikut L/R per proyek); clawback memulihkan beban.
- **Neraca**: 2-6200 sampai dibayar; 1-2100 piutang clawback.
- Komisi per sales / per proyek = query `commissions` + ledger — no duplicate SoT.

## 8. Follow-up backlog
| # | Item |
|---|---|
| CM-B1 | Basis `tiered` (formula registry) + trigger `at_collection`/`at_lunas` |
| CM-B2 | Atribusi legacy-contract: salin sales_person di jalur non-scheme (kecil) |
| CM-B3 | Bukti kas keluar komisi (billing doc) + slip komisi per sales |
| CM-B4 | Auto-sync clawback via EventSink cancellation (kini sweep manual/cron) |

---

**CORE BACKEND: FEATURE COMPLETE.** Seluruh domain CORE blueprint terbangun &
teruji: Ledger, Tenant, Project/Unit (lifecycle), RAB, Cost 3-tier, Alokasi,
Sale/Scheme, Booking, Tax Rule, Approval, Completion/True-up, Cancellation/Refund,
Commission. Berikutnya: **PRODUCT COMPLETION SPRINT** (docs/product-ux-blueprint.md
sebagai SSOT; 15 deliverable wajib per sprint).
