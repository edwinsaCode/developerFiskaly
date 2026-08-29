# Final Financial Architecture Audit — 2026-07-30

> Cakupan: Booking Revenue, Booking Receipt, Product Catalog, Notary Deposit,
> Bulk Unit Generator + seluruh refactor SSOT (S1–S9, Zero-Divergence).
> **Read-only — tidak ada kode diubah.** Basis bukti: sweep grep seluruh
> `SUM()`/agregasi uang, pembacaan file per temuan (path:line), suite kesetaraan
> (24 subtest, 0 skip, strict == normal), integration penuh hijau.

---

## 1 · Verifikasi fungsi kanonik (fokus #1–#3) — ✅ LULUS

| Angka | Fungsi kanonik (SATU-satunya) | Pembuktian |
|---|---|---|
| Booking Revenue | penulis: `sale.CreateBooking` (Cr 4-2100); pembaca: `ComputePL` (4-%) + rekonsiliasi dokumen | `EQ_BookingRevenue` (4-2100 == Σ fee `recognized`); forfeit legacy menulis 4-2000, TIDAK 4-2100 |
| Outstanding / TotalPaid / Collected | `sale.SumTerminsByUnit(counts_toward_price)` → `ContractFinancialSummary` → `PortfolioFinancials` | `EQ_Outstanding_*` (dashboard, KPR, statement, collection preview) + `EQ_R4` |
| Product Revenue | `project.RevenueAccountForUnitType` (satu resolver) → `sale.resolveAccounts` | satu call-site di RecordBAST; P&L tetap konsisten (revenue = semua 4-%) |
| Notary Deposit | penulis tunggal `notary.Service`; saldo via peran `notary_liability` | `EQ_NotaryDeposit` (2-2300 == Σ held) + test 0-baris-4-% |
| Cash Movement | `GetCashMovements` (arus kas) + `GetMonthlyCashFlow` (dashboard) — keduanya category-driven | `EQ_Payments_vs_LedgerCashIn` (kas == Σ termin + Σ notaris) — *catatan M-3* |
| Revenue/HPP/Margin/ActualCost/Budget/Pajak/Forecast/BuyerCredit | tak berubah sejak Zero-Divergence (registry #1–#20) | strict suite PASS 100% |

**Sweep agregasi uang (fokus #2):** seluruh `SUM()` tersisa = implementasi kanonik
itu sendiri, angka dokumen berlabel (bast_value, list_price pipeline, realisasi
per-item), atau pembaca ledger satu-situs (receivedAdvance). Dashboard TIDAK
menghitung ulang — semua field menunjuk kanonik (registry #20). Laporan
(Dashboard/Statement/Invoice/Receipt/TB/P&L/KPI) terbukti membaca sumber sama
oleh 24 EQ; selisih Rp1 = suite merah.

---

## 2 · TEMUAN — berdasarkan severity

### 🔴 Critical — TIDAK ADA
Tidak ditemukan jalur yang HARI INI merusak angka pada flow yang sudah dipakai.
Dua temuan High bersifat laten: baru berbahaya saat fitur katalog non-properti
mulai dipakai / mapping akun diubah sembarangan.

### 🟠 High (wajib hardening SEBELUM menjual produk non-properti)

**H-1 · Alokasi HPP tidak menyaring produk non_property**
`allocation/repository.go:150 GetUnitInputs` memuat SEMUA unit proyek — dipakai
`ComputeAllocation` (service.go:155) DAN `ComputeBudgeted`/snapshot BAST
(service.go:180). Unit `kelebihan_tanah`/`pdam` ber-`saleable_area`/`list_price`
> 0 akan MENERIMA porsi bobot HPP → HPP rumah terdilusi, COGS salah saat BAST.
Kategori `non_property` saat ini hanya informasional — tidak ditegakkan di
alokasi, pool budgeted, maupun lifecycle.
*Rekomendasi:* filter unit ber-product-type kategori `property` di
`GetUnitInputs` (join master; legacy type tak terdaftar = property) + test
"unit non-properti tidak menerima alokasi" + guard BAST non-property tanpa HPP.

**H-2 · `revenue_account_code` product type tidak divalidasi**
`project.CreateProductType`/`UpdateProductType` menerima kode akun APA PUN
(string bebas). Dipetakan ke akun non-revenue yang ada (mis. `1-1300`) → jurnal
BAST balanced tapi semantik salah (Cr kas sebagai "pendapatan", revenue
understated). Dipetakan ke akun tak ada → BAST tertolak (fail-closed, aman tapi
membingungkan).
*Rekomendasi:* validasi saat simpan — akun harus ADA di COA tenant, aktif, dan
bertipe REVENUE (4-xxxx) — plus dropdown COA di FE (bukan input teks).

### 🟡 Medium

**M-1 · Routing akun produk fail-open** — `product_type.go RevenueAccountForUnitType`
menelan SEMUA error (termasuk error DB transien) menjadi fallback `4-1000`.
Untuk routing uang, gagal-baca seharusnya menggagalkan BAST (fail-closed),
fallback hanya untuk not-found (legacy).

**M-2 · Notary Deposit belum punya jalur refund-ke-customer & payout parsial** —
hanya `held → paid_out` (penuh, ke notaris). Bila transaksi batal dan dana harus
kembali ke customer, tidak ada flow (terpaksa jurnal manual + baris dokumen
tidak berubah status → rekonsiliasi Σ held vs 2-2300 akan MELESET bila jurnal
manual dibuat). Cancellation kontrak sendiri AMAN (tidak menyentuh 2-2300 — fokus #5 lulus).

**M-3 · Dua implementasi agregasi kas** — `GetCashMovements` dan
`GetMonthlyCashFlow` adalah dua SQL terpisah (rule keanggotaan sama:
COA category). Belum ada EQ yang mengikat total keduanya → drift teoretis bila
salah satu diubah sepihak. Konsolidasi/EQ pengikat disarankan.

**M-4 · Klasifikasi arus kas investasi/pendanaan hardcode** —
`repository.go GetCashMovements`: `1-4%`, `!=1-4900`, `2-5000`, `3-%` di SQL
(satu situs, tapi belum peran registry seperti pembaca lain).

### 🟢 Low

| # | Temuan | Lokasi |
|---|---|---|
| L-1 | Derivasi `receipt_type` mengabaikan error Scan → DB error bisa mislabel KWT (dokumen, bukan uang) | `billing/receipt_repository.go` (blok UAT §1) |
| L-2 | Default `"4-1000"` ditulis di DUA tempat (resolver fallback + `resolveAccounts`) — konstanta ganda | `product_type.go` / `sale/service.go` |
| L-3 | `accountLabel` hardcode nama bank ("Bank — BCA" dst) — drift tampilan bila tenant rename COA | `sale/collection.go:97` |
| L-4 | Kode bulk `%02d`: unit >99 → "A-100" (3 digit) — sorting lexical kosmetik | `project/service.go` |
| L-5 | `buildCloseInput` selalu resolve akun forfeit legacy walau booking `recognized` (dependency tak perlu; akun selalu ter-seed) | `sale/booking_service.go` |
| L-6 | Kewajiban Titipan Notaris belum tampil di KPI dashboard (hanya neraca) — visibilitas owner | `reporting/dashboard.go` |
| L-7 | 8 `TODO(tax-advisor)` terbuka (DISENGAJA per aturan CLAUDE.md — celah bertanda, bukan bug): tarif PPN villa mewah, basis PPh, PPN admin/kelebihan-tanah/PDAM, bunga inhouse, restitusi PPh saat batal | tax/scheme/cancellation/product_type |
| L-8 | 81 referensi `legacy/fallback` — SEMUA adalah rail data-driven yang disengaja (booking held/transferred pra-rule, konversi flag TRUE, disposisi & refund booking legacy, fallback product 4-1000). Aktif hanya untuk BARIS HISTORI; booking/unit baru tidak pernah masuk ke sana. Bisa dipangkas setelah saldo legacy 2-2100 nol | sale/cancellation |

**Fokus #6 (Bulk) — LULUS penuh:** satu transaksi GORM + cek duplikat eksplisit
+ UNIQUE `uq_units_project_code` (project_id, code) sebagai lapis kedua anti-race
→ rollback penuh, tidak ada partial insert (dibuktikan integration test:
batch gagal → jumlah unit tak berubah; smoke live 409).

**Fokus #9 (potensi selisih Rp1):** rumus PPN tunggal (`vatAmountOf`), semua
aritmetika decimal (nol float di jalur uang), pembulatan alokasi
largest-remainder, suite selisih-Rp1-merah. Tidak ditemukan jalur pembulatan
ganda baru. Potensi tersisa hanya M-3 (drift teoretis antar dua pembaca kas).

---

## 3 · Vonis production-grade

**✅ PRODUCTION-GRADE (aman dipakai sekarang):**
Ledger & posting engine (append-only, period guard) · seluruh pembaca SSOT
(dashboard, statement, P&L/neraca, AR aging, collection, KPR, actual cost,
budget, pajak, komisi, buyer credit) · **Booking Revenue** (rule klien, terkunci
suite) · **Booking Receipt KWB** · **Notary Deposit core** (held→payout) ·
**Bulk Unit Generator** · multi-tenant isolation · financial consistency suite
sebagai jaring permanen.

**🔧 BUTUH HARDENING (sebelum dipakai untuk kasusnya):**
1. **Product Catalog** — H-1 (filter alokasi non_property), H-2 (validasi akun),
   M-1 (fail-closed) → prasyarat sebelum menjual ruko/kelebihan tanah/PDAM
   sungguhan. Menjual RUMAH hari ini tidak terdampak (perilaku identik lama).
2. **Notary Deposit** — M-2 (refund-ke-customer + payout parsial) → prasyarat
   sebelum volume titipan tinggi/kasus batal.
3. **Konsolidasi pembaca kas** — M-3/M-4 (kualitas arsitektur, bukan bug aktif).
4. Pembersihan Low L-1..L-6 kapan saja (tidak memblokir).

Rekomendasi urutan hardening: **H-1 → H-2 → M-1** (satu sprint "Product Catalog
Hardening"), lalu M-2, lalu M-3/M-4 + Low.
