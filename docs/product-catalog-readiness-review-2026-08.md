# Production Readiness Review — Product Catalog

**Tanggal:** 2026-08-03
**Lingkup:** hardening H-1 (dilusi HPP), H-2 (validasi akun pendapatan), M-1 (fail-open)
**Status:** SIAP PRODUKSI dengan 1 prasyarat data (tenant 3, lihat §7)

---

## 1. Business rule yang sekarang berlaku

Kategori produk adalah **business rule utama**, bukan label tampilan.

| Kategori | Ikut alokasi/HPP | Ikut progress fisik | Bisa dijual/ditagih/dibayar | Pengakuan pendapatan |
|---|---|---|---|---|
| `property` | **Ya** — masuk basis alokasi, BAST melepas HPP dari persediaan | Ya | Ya | Akun katalog (default `4-1000`) |
| `non_property` | **Tidak pernah** — tidak masuk basis, tidak menerima biaya langsung, tidak punya snapshot alokasi | Tidak | Ya (penuh: kontrak, jadwal, kwitansi, statement, collection) | Akun katalog (mis. `4-2000`) |

Aturan ini dijawab **hanya** oleh satu fungsi kanonik:
`domain.ProductCategory.ParticipatesInHPP()` (`internal/domain/product_category.go`).

- SQL tidak pernah memutuskan sendiri — query hanya membaca kolom `category` mentah,
  keputusannya tetap di Go (`allocation.GetUnitInputs` memanggil predikat kanonik).
- Kategori tidak dikenal / kosong / beda case → `false` (fail-closed), bukan
  "dianggap properti".
- `HPPEligibleCategories()` **diturunkan** dari predikat kanonik, sehingga tidak
  bisa berbeda kesimpulan (dijaga unit test).

Konsekuensi baru yang perlu diketahui operasional:

1. **Biaya langsung (`cost_tier=direct`) ke unit non-properti DITOLAK.** Alasan
   akuntansi: biaya direct mendebit persediaan unit dan baru lepas menjadi HPP
   saat BAST unit itu. Unit non-properti tidak pernah mengakui HPP → biayanya
   akan mengendap selamanya di neraca (persediaan hantu, aset overstated).
   Biaya semacam itu harus dicatat sebagai `shared` (dialokasikan ke unit
   properti) atau `overhead`.
2. **`unit_type` wajib terdaftar di katalog.** Unit dengan tipe di luar katalog
   tidak bisa di-BAST dan tidak bisa dihitung alokasinya — error jelas, bukan
   diam-diam dianggap rumah.
3. **Produk nonaktif tidak bisa dipakai bertransaksi** (resolver menolak, bukan
   mengabaikan flag).
4. **`revenue_account_code` wajib akun Pendapatan yang ada & aktif.** Aset,
   Kewajiban, Ekuitas, dan Beban ditolak di titik konfigurasi.

Tidak ada perubahan pada definisi HPP, titik pengakuan pendapatan, urutan
event BAST, atau perlakuan uang muka. Untuk produk `property`, tidak ada satu
pun aturan uang yang berubah.

---

## 2. Fail-closed: seluruh fail-open dihapus

| Titik | Perilaku LAMA (fail-open) | Perilaku SEKARANG |
|---|---|---|
| Resolusi akun pendapatan saat BAST | resolver gagal / tipe tak dikenal → diam-diam pakai `4-1000` | `ErrProductPolicyUnresolved`, **tanpa jurnal** |
| Akun pendapatan kosong | lolos, jurnal dibuat dengan akun default | ditolak sebelum jurnal dibuat |
| DB error saat resolve kebijakan produk | fallback ke properti | error dibungkus `ErrProductPolicyUnresolved` |
| Basis alokasi | `unit_type` apa pun ikut basis | tipe tak terdaftar → `ErrUnitTypeUnregistered` (perhitungan dibatalkan) |
| Biaya direct ke unit | unit apa pun boleh | non-properti → `ErrUnitNotHPPEligible`; resolver gagal → `ErrUnitProductPolicyUnresolved` |
| Mapping akun produk (create/update) | teks bebas, tidak divalidasi | `ErrRevenueAccountInvalid` bila bukan revenue aktif |

Semua penolakan terjadi **sebelum** jurnal dibuat. Dibuktikan oleh test yang
menghitung jumlah jurnal & baris cost entry sebelum-sesudah request ditolak.

Satu jalur legacy yang sengaja dipertahankan: `sale.Service` tanpa resolver
(`productPolicies == nil`) memakai default properti + `4-1000`. Ini hanya
terjadi di unit test lama yang tidak menyentuh DB; wiring produksi
(`cmd/api/main.go`) **selalu** memasang resolver, dan sejak review ini suite
konsistensi juga memasangnya sehingga menjalankan jalur produksi.

---

## 3. Bukti: produk properti menghasilkan jurnal identik

1. **Data produksi: basis alokasi tidak berubah sama sekali.**
   `SELECT COUNT(*) FROM units` = **58**;
   `COUNT(*)` dengan join katalog `category='property'` = **58**.
   Filter kategori tidak membuang satu unit pun → hasil alokasi & HPP
   bit-identical untuk seluruh tenant existing.
2. **Mapping akun produksi = konstanta lama.** Seluruh `product_types` di luar
   tenant 3 memetakan `property → 4-1000` dan `non_property → 4-2000`. Untuk
   produk properti, akun yang dikredit persis sama dengan hardcode sebelumnya.
3. **Fixture nominal-eksak tetap hijau.** Suite `sale` (BAST budgeted, booking,
   scheme), `closing`, `cancellation`, `commission`, `project` menegaskan
   angka & akun jurnal secara eksplisit dan semuanya masih PASS setelah katalog
   dipasang di jalur produksi.
4. **Financial Consistency Suite tetap PASS** (§5) — termasuk
   `EQ_HPP_Ledger_vs_SaleRecords`, `EQ_Revenue_DashboardProject_vs_PL`,
   `EQ_ActualCost_Dashboard_vs_CostRepo`.

---

## 4. Bukti: produk non-properti tidak pernah masuk HPP / progress

Test integrasi `TestConsistency_NonPropertyProduct`
(`internal/reporting/consistency_nonproperty_integration_test.go`) menjalankan
satu proyek dengan produk campuran: 2 rumah (@100 m²) + 1 sambungan PDAM
(250 m² — sengaja lebih besar dari kedua rumah digabung).

| Yang dibuktikan | Hasil |
|---|---|
| Basis alokasi | 2 unit (PDAM di luar basis) |
| HPP rumah | **150.000.000** persis (300jt × 100/200). Bila PDAM ikut: 66.666.666,6667 |
| HPP PDAM | 0; `hpp_method = "none"`; nol snapshot alokasi |
| Debit `5-1000` unit PDAM | 0 |
| Kredit persediaan (`1-3000/3100/3200/3300`) unit PDAM | 0 |
| Pendapatan PDAM | Cr `4-2000` 5.000.000; Cr `4-1000` = 0 |
| Progress fisik | tetap 40% (input manual, bukan turunan daftar unit) |
| Biaya aktual proyek | tetap 300jt |
| Neraca | balanced; Σdebit − Σkredit seluruh jurnal posted = 0 |
| Statement / collection / kwitansi produk non-properti | berjalan penuh, setara dengan summary kanonik |

Catatan `ParticipatesInProjectProgress()`: progress fisik proyek adalah entri
manual append-only (`project_progress_entries`), bukan turunan daftar unit —
jadi tidak ada tempat yang perlu "difilter"; predikatnya disediakan sebagai
kunci agar implementasi progress berbasis unit di masa depan tidak bisa
memasukkan produk non-properti diam-diam.

---

## 5. Hasil test

| Suite | Perintah | Hasil |
|---|---|---|
| Build & vet | `go build ./...`, `go vet ./...`, `go vet -tags integration ./...` | **OK** |
| Unit test | `go test ./...` | **19 paket ok**, 0 gagal |
| Integration | `go test -count=1 -tags integration ./...` | **20 paket ok**, 0 gagal |
| Financial Consistency (utama) | `-run TestConsistency_FinancialNumbers` | **24/24 subtest PASS** |
| Financial Consistency (produk campuran) | `-run TestConsistency_NonPropertyProduct` | **13/13 subtest PASS** |
| Frontend | `npx tsc --noEmit` | **0 error** |

Test baru yang ditambahkan:

- `internal/domain/product_category_test.go` — aturan kanonik + anti duplicate SoT.
- `internal/cost/product_catalog_test.go` — guard biaya direct (tanpa jurnal saat ditolak).
- `internal/project/product_catalog_integration_test.go` — validasi mapping akun (aset/liabilitas/beban/tidak ada/nonaktif), update path, produk nonaktif, isolasi tenant.
- `internal/sale/product_catalog_integration_test.go` — non-properti tidak mendilusi HPP; pendapatan tanpa COGS; `unit_type` tak terdaftar menolak BAST.
- `internal/allocation/repository_integration_test.go` — basis alokasi (exclude, empty basis, fail-closed, tenant-scoped).
- `internal/reporting/consistency_nonproperty_integration_test.go` — 13 kesetaraan lintas pembaca dengan produk campuran.

---

## 6. Migrasi

**`000058_product_catalog_backfill`** (applied; `schema_migrations` = 58, `dirty` = 0).

Mendaftarkan setiap `unit_type` historis yang belum ada di katalog sebagai
produk `property` dengan akun `4-1000` — yaitu **mengubah fallback implisit lama
menjadi baris katalog eksplisit yang bisa diaudit**. Idempoten (`NOT EXISTS`),
tidak menyentuh baris katalog yang sudah ada.

Verifikasi pasca-migrasi pada DB nyata:

- unit tanpa baris katalog: **0**
- unit dengan `unit_type` kosong: **0**
- katalog: 52 `property`, 22 `non_property`
- unit produksi per kategori: 58 `property`, **0 `non_property`**
- unit non-properti dengan biaya langsung (persediaan hantu): **0**
- BAST non-properti dengan HPP ≠ 0: **0**

`down.sql` menghapus baris hasil backfill (yang kodenya cocok dengan
`units.unit_type` dan bukan bagian seed 000057), dengan catatan jujur di
komentar bahwa rollback tidak bisa membedakan baris yang sejak itu diedit user.

---

## 7. Risiko & prasyarat produksi

**P1 — Tenant 3 tidak punya COA.** Tenant 3 hanya memiliki 1 akun (`1-2200`),
bukan 45 seperti tenant lain, sehingga mapping produknya (`4-1000`/`4-2000`)
menunjuk akun yang tidak ada.

- **Bukan regresi**: tenant ini sudah tidak bisa memposting jurnal pendapatan
  sebelum hardening (0 journal entries, 0 BAST, 1 unit, 1 kontrak). Penyebabnya
  tenant dibuat sebelum register menyeed COA.
- **Efek sekarang**: transaksi gagal lebih awal dengan error konfigurasi yang
  jelas, bukan gagal saat posting.
- **Tindakan**: jalankan seed COA untuk tenant 3 (atau hapus bila tenant uji)
  sebelum tenant itu dipakai. Perlu keputusan owner — belum dieksekusi karena
  di luar lingkup hardening ini.

**P2 — Perubahan perilaku yang perlu diumumkan ke pengguna:** biaya `direct` ke
unit non-properti kini ditolak. Hari ini dampaknya nol (tidak ada unit
non-properti di produksi), tapi begitu produk non-properti dipakai, tim biaya
harus memakai tier `shared`/`overhead`.

**P3 — Reklasifikasi kategori produk tidak tersedia.** `UpdateProductType`
sengaja tidak menerima `category`: mengubah kategori setelah ada transaksi akan
mengubah arti histori (HPP yang sudah diakui, basis alokasi yang sudah
di-snapshot). Bila nanti dibutuhkan, itu harus berupa proses ber-jurnal
pembalik, bukan UPDATE kolom.

---

## 8. Dilaporkan, sengaja TIDAK diubah

1. **Counter stok unit menghitung semua unit tanpa memandang kategori**
   (`reporting/dashboard.go` funnel unit per proyek dan
   `reporting/repository.go GetPipelineStats`). Bila nanti ada unit
   non-properti, "unit tersedia" akan mencampur rumah dengan sambungan PDAM.
   Ini angka operasional (count), **bukan uang** — tidak menyentuh jurnal,
   HPP, maupun laporan keuangan, dan dampaknya hari ini nol (0 unit
   non-properti). Perlu keputusan produk: pisahkan menjadi dua baris, atau
   biarkan sebagai "seluruh item terjual".
2. **M-2 (refund Titipan Notaris), M-3/M-4 (pembaca kas), dan seluruh temuan
   Low** dari audit arsitektur 2026-07-30 berada di luar lingkup Product
   Catalog dan belum dikerjakan.

---

## 9. Kesimpulan

Product Catalog **layak produksi**. Aturan partisipasi produk punya satu sumber
kebenaran, seluruh jalur uang fail-closed, mapping akun tidak bisa menunjuk
akun yang salah, dan data historis sudah dinormalisasi lewat migrasi yang
terverifikasi. Produk properti terbukti menghasilkan angka identik dengan
sebelum hardening; produk non-properti terbukti tidak pernah menyentuh HPP
maupun progress proyek sambil tetap berfungsi penuh sebagai barang dagangan.

Satu prasyarat data (tenant 3 tanpa COA) perlu diselesaikan owner sebelum
tenant tersebut dipakai.
