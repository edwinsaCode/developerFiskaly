# Audit & Design — UAT Batch 2 (Booking Receipt · Product Catalog · Notary Deposit · Bulk Unit · Fixed Asset)

> 2026-07-29 · Mode Audit → Design → Implement → Verify (berkelanjutan).
> Prinsip: nol duplicate SoT baru — semua angka keuangan tetap dari fungsi kanonik
> SSOT (registry). Semua perubahan additive & append-only.

## 1 · Booking Receipt (kwitansi booking terpisah)

**Audit:** kwitansi booking saat ini memakai jalur & penomoran yang SAMA dengan
kwitansi pembayaran rumah (`KWT/{yyyy}/{seq}`, satu `receipt_sequences` per tenant;
`billing.GenerateReceiptInTx` dipanggil `CreateBookingAtomic`). Tidak ada penanda tipe.

**Design (migration 000054):**
- `receipts.receipt_type VARCHAR(20) NOT NULL DEFAULT 'house_payment'`
  (`house_payment` | `booking`) — histori otomatis house_payment.
- `receipt_sequences` + kolom `doc_type` (PK → `(tenant_id, doc_type)`) —
  penomoran TERPISAH: rumah `KWT/{yyyy}/{seq}`, booking **`KWB/{yyyy}/{seq}`**.
- Booking flow membuat kwitansi bertipe `booking`; print template berjudul
  "KWITANSI BOOKING" + keterangan "Pendapatan Booking — di luar harga unit".
- Kwitansi rumah tidak berubah. Tidak ada jurnal (receipt = dokumen, tetap).

## 2 · Product Catalog (de-hardcode "rumah")

**Audit — asumsi unit-only/hardcode ditemukan:**
- `units.unit_type` = teks bebas tanpa master/validasi (`project/handler.go:460`).
- Akun pendapatan BAST **hardcode `4-1000`** (`sale/service.go:504`) untuk semua unit.
- Tidak ada mapping produk→akun. (Pembaca laporan sudah role-driven via registry —
  `RoleUnitSalesRevenue` dsb — jadi aman.)

**Design (migration 000057, additive — TANPA rename tabel `units`):**
- Master **`product_types`** per tenant: `code` (unik/tenant), `name`,
  `category` (`property` = ber-HPP/alokasi | `non_property`),
  `revenue_account_code` (COA-driven, editable), `is_active`.
- Seed default per tenant: `rumah`, `ruko`, `kavling` (property → 4-1000);
  `kelebihan_tanah`, `pdam` (non_property → 4-2000). **Mapping bisa diedit** —
  angka P&L konsisten apa pun akunnya (revenue kanonik = semua 4-%).
  `TODO(tax-advisor): akun & perlakuan PPN utk kelebihan tanah / PDAM.`
- `units.unit_type` = KODE product type; create unit memvalidasi ke master aktif.
  Baris lama (villa dll) TIDAK disentuh — BAST fallback 4-1000 bila kode tak
  terdaftar (kompat histori).
- **Routing jurnal BAST:** akun pendapatan di-resolve dari product type unit
  (seam `ProductAccountResolver`, wiring opsional; nil = perilaku lama 4-1000).
  HPP: product non_property tanpa biaya teralokasi → HPP 0 natural (v1).
- Endpoint: `GET/POST/PATCH /product-types` (master data). FE: dropdown tipe unit
  dari master + kelola master.

## 3 · Notary Deposit (Titipan Notaris)

**Audit:** tidak ada flow; risiko dana notaris tercampur pendapatan bila dicatat
manual ke 4-xxxx. Audit jurnal existing: tidak ada yang menyentuh konsep ini.

**Design (migration 000056):**
- Akun **`2-2300 Titipan Notaris` (Liability)** semua tenant + seed COA +
  `RoleNotaryLiability` di AccountRoleRegistry.
- Tabel `notary_deposits`: customer, unit (ops.), amount, status
  (`held` → `paid_out`), `receive_journal_id`, `payout_journal_id`, notary_name.
- Jurnal (satu penulis, package `internal/notary`):
  terima `Dr Kas/Bank / Cr 2-2300`; bayar ke notaris `Dr 2-2300 / Cr Kas/Bank`.
  TIDAK PERNAH menyentuh 4-xxxx (bukan pendapatan developer).
- Endpoint: `POST /notary-deposits`, `POST /notary-deposits/{id}/payout`, `GET` list.
- Suite: **EQ_NotaryDeposit** — saldo 2-2300 == Σ deposit `held` (dokumen↔ledger).

## 4 · Bulk Unit Generator

**Audit:** hanya create satu-per-satu (`POST /projects/{id}/units`); tidak ada field
label tipe rumah (Type 36) — `saleable_area` dipakai luas tanah (basis alokasi).

**Design (migration 000055):**
- `units.type_label VARCHAR(30) NULL` (mis. "36/72") — label komersial, bukan uang.
- Endpoint `POST /projects/{projectID}/units/bulk`:
  `{block, unit_start, unit_end, unit_type, type_label?, saleable_area, list_price}`
  → kode `"{BLOCK}-{NN}"` (A-01..A-12), SATU transaksi (gagal satu = batal semua,
  mis. kode duplikat), cap 500 unit/batch. Create satuan tetap ada.
- FE: wizard "Buat Unit per Blok" di halaman unit proyek (pratinjau daftar kode).

## 5 · Fixed Asset & Depreciation — AUDIT KESIAPAN

COA sudah punya 1-4xxx (aset tetap + 1-4900 akumulasi depresiasi; arus kas sudah
mengklasifikasikan 1-4% sebagai investasi) — tetapi TIDAK ada modul (register aset,
jadwal depresiasi, jurnal depresiasi berkala). **Tidak dibutuhkan flow inti penjualan
saat ini** → sesuai instruksi, didokumentasikan sebagai roadmap terpisah
(`docs/fixed-asset-roadmap-2026-07.md`) tanpa menyentuh modul penjualan.

## Guard SSOT (dicek sebelum & sesudah implementasi)
- Tidak ada SQL baru yang menghitung angka keuangan di luar kanonik: saldo notaris
  via `LedgerBalanceService(RoleNotaryLiability)`; revenue per produk otomatis di
  `ComputePL` (semua 4-%); bulk unit & receipt tidak menyentuh perhitungan uang.
- Suite konsistensi + strict wajib PASS; jurnal baru dilaporkan di laporan akhir.
