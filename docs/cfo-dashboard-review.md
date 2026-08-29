# esaProperti — CFO/CEO Dashboard Readiness Review

> Uji blueprint dari **perspektif Direktur/CFO**, bukan database. 45 pertanyaan
> bisnis yang lazim ditanyakan → verifikasi apakah blueprint **bisa menjawab tanpa
> ubah arsitektur**. Tidak ada coding. Melengkapi `erp-blueprint-review.md`.

**Legenda verdict:**
- ✅ **Bisa sekarang** — data ada (ledger + entity CORE existing).
- 🟡 **Bisa via addition yang SUDAH di roadmap** (Customer master, Sales Person/Team,
  Vendor/AP, Bank Account, Tax Rule, marketing-expense path) — tanpa ubah arsitektur inti.
- 🔴 **GAP** — butuh entity/relasi yang **belum** ada di blueprint → harus ditambah.

---

## A. Profitability & Margin
| # | Pertanyaan Direktur | Verdict | Dijawab dari |
|---|---|---|---|
| 1 | Berapa **laba kotor per proyek**? | ✅ | Ledger tagged project: Pendapatan 4-xxx − HPP 5-1000 |
| 2 | Berapa **margin per tipe unit** (tipe 36/45/ruko)? | ✅ | `Unit.unit_type` × (sale_record revenue − HPP) |
| 3 | Berapa **laba per fase**? | ✅ | Journal lines tag `phase_id` |
| 4 | **Profit per unit** (harga jual − HPP unit)? | ✅ | Sale Record (revenue) − HPP Snapshot/lines |
| 5 | **Laba BERSIH per proyek setelah alokasi G&A/overhead**? | 🔴 | Overhead kini di Tenant, **tak dialokasikan** ke proyek → butuh **Cost Allocation Policy** |
| 6 | **Realisasi diskon / price realization** (list vs jual)? | ✅ | `Unit.list_price` vs `sale_record.sale_price` |

## B. Sales Performance & Marketing
| # | Pertanyaan | Verdict | Sumber / gap |
|---|---|---|---|
| 7 | **Siapa yang menjual** unit ini? | 🟡 | Sales Person (P2) — reservasi `sales_person_id` di kontrak |
| 8 | Jumlah **closing per sales/tim** periode ini? | 🟡 | Atribusi Sales Person + BAST count |
| 9 | **Nilai penjualan per sales**? | 🟡 | Σ kontrak/BAST per Sales Person |
| 10 | **Collection rate per sales** (tertagih ÷ tagihan)? | 🟡 | AR (ledger) + atribusi Sales Person |
| 11 | **Sales velocity / absorption rate** (unit terjual per bulan)? | ✅ | Unit status + `bast_date`/`sale_date` |
| 12 | **Revenue backlog** (kontrak sudah tanda tangan, belum BAST)? | ✅ | Sale Contract tanpa Sale Record |
| 13 | **Biaya marketing per closing (CAC)**? | 🔴 | Butuh **marketing expense posting path** (masih deferred P1-1) + atribusi |
| 14 | **Konversi lead → booking → kontrak** (funnel)? | 🔴 | Tidak ada **Lead/CRM/Booking** entity |

## C. AR / Collection
| # | Pertanyaan | Verdict | Sumber / gap |
|---|---|---|---|
| 15 | **AR aging** (0-30/31-60/60+)? | ✅ | Payment Schedule due date + AR (saldo 1-2000) |
| 16 | **Outstanding per buyer**? | 🟡 | AR per Buyer — mantap dengan **Customer master** (CORE-refine) |
| 17 | **Uang muka diterima vs nilai kontrak**? | ✅ | Payment/Schedule vs Sale Contract |
| 18 | **Deferred revenue / Uang Muka Penjualan outstanding**? | ✅ | Saldo 2-2000 (derived) |
| 19 | **Cicilan jatuh tempo & overdue** bulan ini? | ✅ | Payment Schedule status (scheduled/due/overdue) |

## D. AP / Payables
| # | Pertanyaan | Verdict | Sumber / gap |
|---|---|---|---|
| 20 | **AP aging** ke vendor/kontraktor? | 🟡 | Vendor Invoice + AP (derived 2-1xxx) — P2 |
| 21 | **Sisa komitmen PO** yang belum ditagih? | 🟡 | Purchase Order (P2) |
| 22 | **Hutang jatuh tempo** ke vendor minggu ini? | 🟡 | Vendor Invoice due date (P2) |

## E. Cash Flow & Treasury
| # | Pertanyaan | Verdict | Sumber / gap |
|---|---|---|---|
| 23 | **Posisi kas per rekening bank** saat ini? | 🟡 | Bank Account master (CORE) + saldo 1-1xxx (derived) |
| 24 | **Arus kas masuk/keluar aktual** (periode)? | ✅ | Journal lines akun kas/bank |
| 25 | **Proyeksi penerimaan** 3 bulan ke depan? | ✅ | Payment Schedule due dates |
| 26 | **Proyeksi pengeluaran** (bayar vendor + sisa RAB)? | 🟡 | PO/Vendor Invoice (P2) + RAB remaining |
| 27 | **Arus kas per proyek**? | ✅ | Journal lines kas tag `project_id` |

## F. Cost / HPP / Budget
| # | Pertanyaan | Verdict | Sumber / gap |
|---|---|---|---|
| 28 | **RAB vs Realisasi** per kategori? | ✅ | Budget Item vs Cost Entry posted (posted-only) |
| 29 | Proyek/kategori mana **over budget**? | ✅ | RAB vs realisasi + soft-control |
| 30 | **HPP per unit** akurat (budgeted → aktual)? | ✅ | HPP Snapshot (budgeted) + True-up (aktual) |
| 31 | **Variance True-up** (dampak ke laba)? | ✅ | hpp_trueup_lines: budgeted/actual/variance |
| 32 | **Biaya per m²** proyek? | ✅ | Cost Entry total ÷ Σ saleable_area |
| 33 | **Sisa persediaan (nilai unit belum terjual, at cost)**? | ✅ | Inventory derived (saldo 1-3xxx) = act_HPP unsold |

## G. Tax
| # | Pertanyaan | Verdict | Sumber / gap |
|---|---|---|---|
| 34 | **PPh Final terutang & terbayar** (subsidi 1% / komersial 2,5%)? | 🟡 | Tax Obligation/Payment + **Tax Rule** (CORE) |
| 35 | **Exposure pajak** (jatuh tempo) bulan ini? | ✅ | Tax Obligation outstanding |
| 36 | **PPN Keluaran** periode? | ✅ | Saldo PPN Keluaran (BAST) |
| 37 | **PPN Masukan & PPN kurang/lebih bayar (net)**? | 🔴 | PPN Masukan butuh **Vendor Invoice + input-tax** (belum eksplisit di Tax Rule) |

## H. Inventory / Stock
| # | Pertanyaan | Verdict | Sumber / gap |
|---|---|---|---|
| 38 | **Stok unit** (available/reserved/sold) per proyek? | ✅ | Unit.status |
| 39 | **Nilai stok belum terjual** (at cost)? | ✅ | Inventory derived |
| 40 | **Nilai stok belum terjual (at selling price)**? | ✅ | Σ `list_price` unit available |
| 41 | **Unit slow-moving / time-on-market**? | 🔴 | Butuh **`listed_at`/available_since** di Unit (aging stok) |
| 42 | **Reservasi & booking fee** aktif? | 🔴 | Booking/Reservation + booking fee belum dimodelkan |

## I. Financial Statements & Consolidation
| # | Pertanyaan | Verdict | Sumber / gap |
|---|---|---|---|
| 43 | **L/R & Neraca per proyek / company**? | ✅ | Ledger (Account Balance) tag project |
| 44 | **Dashboard konsolidasi multi-proyek**? | ✅ | Agregasi lintas Project (satu Tenant) |
| 45 | **Konsolidasi multi-company (grup PT)**? | 🟡 | Organization/Group parent (FUTURE, additive) |

---

## Rekapitulasi
- **Bisa sekarang (✅):** 22 pertanyaan — inti profit/margin/HPP/AR/cashflow aktual/stok/pajak keluaran/laporan sudah tercakup **karena ledger-centric**.
- **Via addition yang sudah di roadmap (🟡):** 12 — Sales Person, Customer master, Vendor/AP, Bank Account, Tax Rule, multi-company. **Tidak** butuh ubah arsitektur.
- **GAP baru yang perlu ditambah ke blueprint (🔴):** 6 — di bawah.

## GAP baru (delta ke blueprint — domain saja)
| # | Gap | Entity/relasi yang kurang | Dampak jika diabaikan | Rekomendasi |
|---|---|---|---|---|
| G1 | **Laba bersih per proyek** (Q5) | **Cost Allocation Policy** — alokasikan overhead/G&A Tenant ke proyek (basis: pendapatan/luas/jumlah unit) sebagai read-model | Direktur hanya lihat margin kotor, bukan net per proyek | **CORE-flag**: definisikan policy alokasi (mirip HPP allocation), read-only, tidak menyentuh ledger transaksi |
| G2 | **Marketing spend & CAC** (Q13) | **Marketing/Other expense posting path** (deferred P1-1) + link ke proyek/sales | Biaya pemasaran tak terukur; CAC & opex per proyek buta | **Un-defer**: aktifkan expense posting (Dr Beban Pemasaran 5-3xxx) — sudah ada `ExpenseAccountCode()` |
| G3 | **PPN Masukan / net PPN** (Q37) | **Vendor Invoice input-tax** + Tax Rule dimensi masukan; PPN payable = keluaran − masukan | Posisi PPN tak lengkap; hanya keluaran | **P2** bersama AP: Tax Rule dukung `direction=in/out`; Vendor Invoice bawa PPN masukan |
| G4 | **Lead → Booking funnel & conversion** (Q14, Q42) | **Lead/Prospect + Booking/Reservation (+ booking fee)** | Funnel marketing & konversi tak terukur; reservasi tak resmi | **P2/boundary**: bisa modul CRM ringan; Booking sbg state pra-kontrak (Unit `reserved` + Booking doc) |
| G5 | **Refund / Cancellation** (blind spot) | **Cancellation flow**: state transition + jurnal pembalik + refund liability | Pembatalan tak tercatat; laba & AR salah | **CORE-flag**: desain state (contract cancelled, unit → available, refund payable) — append-only via reversing |
| G6 | **Stock aging / time-on-market** (Q41) | Atribut **`available_since`/`listed_at`** di Unit | Tak tahu unit mandek berapa lama | **CORE-minor**: 1 atribut waktu di Unit |

## Putusan CFO-readiness
Blueprint **~88% siap** menjawab pertanyaan Direktur **tanpa ubah arsitektur inti**
— berkat prinsip **ledger-centric + Project aggregate root**. Sisanya (🟡) sudah
terencana additive. **6 gap baru (G1–G6)** bukan cacat arsitektur, melainkan
**entity/atribut/policy tambahan** yang menempel di seam yang sudah ada:
- G1 & G5 sebaiknya **di-CORE-kan** (net profit per proyek & pembatalan adalah
  pertanyaan Direktur sehari-hari).
- G2 tinggal **un-defer** (kodenya sebagian sudah ada).
- G3, G4 mengikuti modul **P2** (AP & CRM).
- G6 sepele (1 atribut).

Tidak ada satu pun gap yang memaksa **refactor ledger atau aggregate root**. Blueprint
siap jadi fondasi ERP jangka panjang setelah G1–G6 dimasukkan ke domain (belum coding).
