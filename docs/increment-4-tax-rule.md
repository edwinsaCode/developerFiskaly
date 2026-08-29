# Increment 4 — Tax Rule Konfiguratif

> Blueprint: `erp-blueprint-review.md` §7 (CORE — "tarif tidak boleh di kode").
> Prinsip dipegang: additive-only, configuration over hardcoding, policy seam,
> append-only ledger, tanpa refactor modul yang sudah di-approve.

## Ringkasan

`tax_rates` dipromosikan menjadi **Tax Rule penuh** sesuai blueprint §7:
tarif + **cakupan per kategori proyek** (`applies_to` ↔ `projects.tax_category`)
+ **akun jurnal konfigurable** + masa berlaku (`effective_from/to`) + `is_active`
+ seam `trigger_event`/`calc_base`. Regulasi berubah (mis. tarif subsidi baru) =
**tambah baris rule dengan `effective_from` baru — tanpa ubah kode** (dibuktikan
integration test). Akun 5-2000/2-4000 yang sebelumnya hardcode di `AccrueTax`
kini dibaca dari rule.

## Baseline yang sudah sehat (tidak diubah)

Tarif bertanggal per `rate_code`, snapshot tarif di obligation (perubahan rule
tak menyentuh akrual lama), akrual PPh atomik di transaksi BAST, VAT report
rekonsiliasi ledger. Increment ini menambah dimensi **kategori** + **akun** +
**masa berlaku** di atasnya.

## Perubahan Database (migration 000039, reversible, ADD-only)

| Objek | Isi |
|---|---|
| `projects.tax_category` | `subsidi\|komersial` (CHECK), default `komersial` (konservatif — tarif tertinggi). **PENENTU tarif** (blueprint §7); BUKAN `customers.segment` (analitik) dan BUKAN `financing_sources.type` (bank) |
| `tax_rates` +8 kolom | `name`, `applies_to` (CHECK all\|subsidi\|komersial), `trigger_event` (seam, default bast), `calc_base` (seam, default transfer_value), `debit_account`, `credit_account`, `effective_to`, `is_active`. Backfill baris lama: akun default + applies_to `all` |
| Seed per tenant | PPh Final **subsidi 1%** + **komersial 2,5%** (PP 34/2016) — SQL utk tenant existing; `tax.SeedDefaultRates` (diperluas, kini juga dipanggil saat registrasi tenant) utk tenant baru. Baris `all` 2,5% lama tetap sebagai fallback |
| `tax_obligations` +2 kolom | `tax_rule_id` + `applies_to` — **provenance**: "kenapa unit ini kena 1%" terjawab dari obligation |

Down migration teruji (rule per-kategori dihapus sebelum kolom dilepas; obligation & jurnal tidak disentuh).

## Domain & Seam

- **`domain.TaxCategory`** (subsidi|komersial) — SSOT bertipe (pola CostTier).
- **`tax.TaxRuleResolver`** (seam): `ResolveRule(rate_code, category, date)` —
  aturan pemilihan terisolasi dari kode posting: aktif + berlaku pada tanggal +
  **specificity kategori > `all`** + effective terbaru. Tidak ada rule →
  `ErrRateNotConfigured` (tidak pernah default diam-diam).
- **`tax.ProjectTaxCategoryReader`**: baca penentu tarif dari proyek.
- `WithRuleResolution(...)` opsional (functional option): tanpa opsi = jalur
  legacy (test lama utuh); **produksi & jalur BAST atomik memasangnya**.
- Akun default legacy terpusat: `DefaultPPhExpenseAccount`/`DefaultPPhPayableAccount`
  — hanya fallback untuk baris rule lama berakun kosong.

## Alur akrual (Event 5a) setelah increment ini

```
BAST unit → AccruePPhFinalInTx (atomik dlm tx BAST)
  → baca projects.tax_category            (subsidi | komersial)
  → ResolveRule(pph_final_pengalihan, kategori, tanggal BAST)
      subsidi → 1%   |   komersial → 2,5%   |   fallback 'all'
  → jurnal Dr <rule.debit_account> / Cr <rule.credit_account>   (konfigurasi!)
  → obligation: snapshot tarif + tax_rule_id + applies_to        (provenance)
```

Tanpa proyek (akrual manual tanpa project) → kategori default **komersial**
(konservatif). Perubahan `tax_category` proyek hanya berefek pada akrual
BERIKUTNYA (append-only; tidak ada recalculation).

## API

| Method | Path | Perubahan |
|---|---|---|
| POST | `/tax/tax-rates` | + `name`, `applies_to`, `trigger_event`, `calc_base`, `debit_account`/`credit_account` (berpasangan), `effective_to` |
| GET | `/tax/tax-rates` | kini mengembalikan daftar rule nyata (sebelumnya placeholder) |
| POST | `/projects` | + `tax_category` (kosong = komersial) |
| **PUT** | **`/projects/{id}/tax-category`** | baru — ubah penentu tarif (efek ke akrual berikutnya saja) |

## Test (15 package hijau, `-tags integration`)

- **Unit**: akrual subsidi 1% vs komersial 2,5% via resolver + jurnal memakai
  **akun dari konfigurasi rule** (bukan hardcode — dibuktikan dengan akun
  khusus 5-2100/2-4100); provenance `tax_rule_id`+`applies_to` di obligation;
  default konservatif tanpa proyek; fallback akun legacy; validasi SetTaxRate
  (applies_to, rentang effective, akun berpasangan).
- **Integration (MySQL)**: specificity subsidi>all & komersial; skenario
  "regulasi berubah" (tambah rule effective 2027 → 2026 tetap 1%, 2027 jadi
  1,5%); `is_active=0` di-skip; `effective_to` kedaluwarsa → fallback `all`;
  **akrual via jalur BAST atomik** per kategori proyek dengan jurnal balanced;
  **snapshot**: mengubah rule setelah akrual tidak mengubah obligation lama;
  rule berakun khusus (5-2900/2-4900) benar-benar dipakai jurnal.
- **Regresi**: seluruh test lama lulus — resolver opsional; `GetCurrentRate`
  legacy tidak berubah (VAT provider aman).

## Risiko & Backward Compatibility

- Rendah. Semua additive; obligation & jurnal lama tidak disentuh; proyek
  existing di-default `komersial` (= perilaku tarif lama 2,5%, tidak ada
  perubahan angka bagi data berjalan).
- Perilaku baru yang disengaja: proyek yang DITANDAI `subsidi` mulai akrual 1%
  pada BAST berikutnya — persis tujuan increment.
- `TODO(tax-advisor)` yang sudah ada dipertahankan (basis pengalihan bruto,
  HGB/leasehold, PPN 11/12%); baru: tidak ada — increment ini murni
  konfigurasi, bukan interpretasi aturan baru.

## Non-goals (seam tersedia)

`trigger_event` invoice/payment (BPHTB/AJB — blueprint §7 tabel), PPN Masukan
via Vendor Invoice (P2/G3), `calc_base` alternatif, UI konfigurasi rule.

## Next

Sesuai blueprint CORE yang tersisa: **Booking** / **Refund–Cancellation** /
**Unit Lifecycle state machine** / **Generic Approval Workflow** — urutan
menunggu keputusan owner setelah review.
