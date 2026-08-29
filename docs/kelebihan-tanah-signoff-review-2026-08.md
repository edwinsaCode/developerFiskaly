# Kelebihan Tanah — Final Architecture Sign-off Review

**Tanggal:** 2026-08-21
**Status:** REVIEW — belum ada kode, migrasi, atau perubahan database.
**Dasar:** `docs/kelebihan-tanah-final-architecture-2026-08.md` (diamandemen pada sesi ini dengan 2 koreksi faktual, lihat §1/§2 di bawah), audit langsung kode+data live tambahan 2026-08-21.

Metode: setiap klaim di bawah diverifikasi langsung terhadap kode (`internal/closing/service.go`, `internal/sale/collection.go`, `internal/sale/repository.go`, `internal/charge/repository.go`) dan data live (`payment_allocations`, `termin_payments`, `journal_lines`, `charge_groups`) — bukan diasumsikan dari dokumen sebelumnya.

---

## Ringkasan dulu: dua koreksi faktual ditemukan pada review ini

Review ini menemukan **2 klaim EXISTING FACT yang keliru** di draf arsitektur sebelumnya. Keduanya sudah diamandemen langsung di `docs/kelebihan-tanah-final-architecture-2026-08.md` §E.3 dan §G.1. Dilaporkan di sini juga supaya jelas apa yang berubah dan kenapa:

1. **`hpp_trueup_runs`/`hpp_trueup_lines` TIDAK dormant.** Draf sebelumnya bilang skema ini "belum pernah punya kode konsumen" dan mengusulkannya untuk rekonsiliasi rounding Land. Faktanya: `internal/closing/service.go` (668 baris) mengimplementasikan siklus penuh `MarkCompleted → FinalizeCompletion → Calculate → Approve → Post` — fitur P0-4 yang SUDAH SHIPPED dan aktif dipakai untuk true-up HPP unit properti saat proyek selesai. Detail di §2 bawah.
2. **Charge_group id=11 (addon Kelebihan Tanah, unit 1433) TIDAK bersih.** Draf sebelumnya menyimpulkan "tidak ada satu pun jurnal yang pernah terposting" berdasarkan `recognized_amount=0`. Itu keliru — `recognized_amount` hanya mencerminkan status invoice, bukan status kas. Ada 1 jurnal posted (Rp1.000.000, transfer internal titipan) yang sudah dialokasikan penuh ke item di grup ini lewat `payment_allocations`. Detail di §5 bawah.

Kedua koreksi ini **tidak mengubah arah arsitektur** (R-1, standalone product, per-pool allocation semuanya tetap valid) — keduanya murni soal detail migrasi/cleanup yang harus akurat sebelum implementasi.

---

## 1. HPP historical immutability

🟢 **READY.** Diverifikasi langsung dari kode, bukan diasumsikan.

**Case A — jawaban eksplisit:**

> Unit A sudah Akad ketika `land_stock` belum ada. HPP Unit A sudah posted/finalized. Setelah itu `land_stock` 1.000 m² dibuat, lalu Kelebihan Tanah mulai dijual.

- **HPP Unit A tetap immutable.** Bukti: `internal/sale/repository.go:320-331` — `AllocationSnapshot` ditulis dengan `tx.Create(snap)` **satu kali**, di dalam transaksi atomik yang sama dengan jurnal + `SaleRecord`, pada saat Akad unit itu sendiri. Tidak ada satu pun path `UPDATE`/`Save` terhadap `allocation_snapshots` di seluruh codebase (dicek: hanya `tx.Create` yang menyentuh tabel ini). `UNIQUE KEY uk_alloc_snap_unit (tenant_id, unit_id)` menjamin percobaan tulis kedua akan GAGAL (constraint violation), bukan diam-diam menimpa — jadi bahkan bug pun tidak bisa membuat snapshot lama berubah.
- **Jurnal lama tidak disentuh.** Jurnal Event 3/4 Unit A sudah `posted_at` terisi saat Akad-nya; tidak ada mekanisme apa pun di desain Kelebihan Tanah yang membalik atau menulis ulang jurnal unit lain — invariant #5 (append-only) tidak pernah dilanggar oleh alur `land_sales` manapun, karena `land_sales` hanya pernah menulis jurnalnya SENDIRI, di Akad-nya SENDIRI.
- **`AllocationSnapshot` lama tidak dihitung ulang.** Menambahkan `land_stock` sebagai peserta pool Tanah hanya memengaruhi PEMANGGILAN `Compute()` BERIKUTNYA (mis. saat Unit B atau `land_sale` pertama melakukan Akad setelahnya) — bukan snapshot yang sudah ditulis. Ini sama persis dengan perilaku hari ini: Akad Unit B bulan depan sudah lazim menghasilkan angka pool berbeda dari Akad Unit A bulan lalu, karena biaya proyek terus terakumulasi — Kelebihan Tanah tidak memperkenalkan kelas risiko baru di sini, ia mengikuti pola non-retroaktif yang SUDAH ada.
- **HPP transaksi land sale baru ditentukan tanpa merusak histori:** setiap `land_sale` memanggil `Compute()`/`ResolveLandHPPRate` FRESH pada saat Akad-nya sendiri (persis pola `ResolveHPP` unit properti hari ini: finalized→budgeted→actual, dievaluasi live, lalu dibekukan ke `land_allocations` dengan cara yang identik `tx.Create`-only seperti `allocation_snapshots`). Hasilnya adalah angka BARU untuk transaksi BARU — tidak pernah menulis ulang baris lama mana pun.

**Kesimpulan Case A:** desain sudah benar by construction (pola `Create`-only + unique key sudah ada dan akan diwarisi apa adanya oleh `land_allocations`), tidak perlu perubahan desain.

---

## 2. Review `hpp_trueup_runs`/`hpp_trueup_lines` (TD-6)

🟢 **READY** (setelah koreksi — keputusan v1: tidak dipakai untuk Land).

**Untuk apa tabel ini sebenarnya dibuat:** true-up HPP budgeted→actual untuk unit properti **pada saat proyek/fase selesai** (P0-4, closing process). `internal/closing/service.go`:
- `MarkCompleted` (baris 33-80): `projects.status → 'completed'`.
- `FinalizeCompletion` (baris 83-103): `completed → finalized`, mengunci biaya aktual, "membuka" true-up.
- `Calculate`/`Approve`/`Post` (baris 352-576): hitung varians per unit per `category` (`land|hard|soft|financing`), approve (belum jurnal), lalu `Post` membuat **satu jurnal penyesuaian per run** (`journal_id UNIQUE`, immutable begitu posted).

**Apakah pernah digunakan:** ya — ini bukan kode mati, ini fitur lengkap dengan state machine (`draft→calculated→approved→posted→cancelled`), row-locking (`clause.Locking{Strength:"UPDATE"}`), dan idempotency (`active_scope` generated column + unique index). Salah kalau dianggap dormant.

**Apakah boleh dipakai untuk Land:** **tidak, tanpa perubahan skema.** `hpp_trueup_lines.unit_id BIGINT UNSIGNED NOT NULL` — FK wajib ke `units`, pola yang sama persis dengan `sale_contracts.unit_id` yang sudah terbukti di dokumen utama tidak bisa menampung standalone product tanpa migrasi. `land_stock` bukan baris `units`.

**Apakah true-up berpotensi mengubah HPP/jurnal historis:** tidak — desainnya sudah benar secara akuntansi (satu jurnal penyesuaian baru per run, tidak pernah menulis ulang jurnal Akad asli). Tapi ini **event satu-kali-di-akhir-proyek**, bukan penyeimbang berkala — jadi secara KARAKTER tidak cocok untuk merekonsiliasi rounding lintas banyak `land_sale` yang terjadi terus-menerus SELAMA proyek berjalan (bukan hanya di akhir).

**Apakah true-up diperlukan sama sekali untuk v1: TIDAK.** Alasan: `land_stock` sebagai satu peserta pool berperilaku identik dengan satu unit properti — snapshot HPP-nya dibekukan di titik resolusi saat itu, dan selisih terhadap biaya final proyek **sudah diterima sebagai perilaku normal untuk unit properti hari ini** (itulah alasan true-up unit ada, dijalankan sekali mencakup SEMUA unit terjual). Land bisa mewarisi ketidaksempurnaan yang sama tanpa mekanisme baru — direkonsiliasi nanti, bersama unit properti, pada saat closing proyek (**v2, di luar cakupan sekarang**, butuh perubahan skema `hpp_trueup_lines` yang tidak dikerjakan sekarang).

**Keputusan v1 (final untuk dokumen ini):** **tidak menggunakan true-up untuk Land.** `land_allocations` per transaksi berdiri sebagai snapshot final untuk v1, sama seperti `allocation_snapshots` unit hari ini sebelum true-up pernah ada.

---

## 3. Payment flow standalone Land Sale

🔴→🟢 **DESAIN DIPERBAIKI pada sesi ini** — draf sebelumnya menyatakan "reuse payment infrastructure existing" tanpa memverifikasi apakah itu benar-benar mungkin tanpa perubahan. Setelah dicek: **tidak mungkin tanpa perubahan skema minimal**, tapi perubahannya kecil, additive, dan sudah punya preseden persis di tabel yang sama.

**Fakta terverifikasi:**
- `internal/sale/collection.go` `ReceivePayment` (baris 300-395) — **pintu masuk tunggal** penerimaan pembayaran ("+Catat Penerimaan"). Resolusi anchor (baris 314-343) HANYA punya 3 cabang: `req.ScheduleID` → `sch.UnitID`, `req.ContractID` → `c.UnitID`, `req.UnitID` langsung. Default: `return nil, ErrUnitRequired`. **Tidak ada jalur tanpa unit.**
- `termin_payments.unit_id BIGINT UNSIGNED NOT NULL` dan `receipts.unit_id BIGINT UNSIGNED NOT NULL` (dicek langsung `SHOW CREATE TABLE`) — hard-anchored end-to-end, dari request sampai baris tersimpan.
- **Tapi**: kedua tabel itu **sudah punya preseden kolom nullable** untuk anchor sekunder — `termin_payments.charge_group_id` (nullable, sudah dipakai jalur addon W-13) dan `termin_payments.financing_source_id`/`receipts.charge_group_id` (nullable). Ini membuktikan pola "anchor utama wajib + anchor sekunder opsional" **bukan hal baru** di codebase ini.

**Desain minimum (bukan payment engine kedua):**
1. `termin_payments.unit_id` dan `receipts.unit_id` → **nullable**.
2. Tambah kolom `land_sale_id BIGINT UNSIGNED NULL` di kedua tabel (pola identik `charge_group_id`).
3. `CHECK` constraint (atau validasi service-level, MySQL CHECK sejak 8.0 ditegakkan) memastikan **tepat satu** dari `unit_id`/`land_sale_id` terisi — tidak pernah keduanya NULL, tidak pernah keduanya terisi.
4. `ReceivePaymentRequest` tambah field `LandSaleID *uint64`, cabang keempat di resolusi anchor (§H dokumen utama sudah diamandemen dengan ini).
5. Alur downstream (`preparePayment`, jurnal Event 2, kwitansi/W-2 numbering) **tidak berubah logikanya** — engine penomoran dokumen (`internal/document`, W-2) sudah generik per tenant+jenis dokumen, tidak bergantung `unit_id` sama sekali.

**Hubungan dengan lifecycle Kelebihan Tanah:**
```
Booking/Reservation (land_stock_reservations, tanpa uang di v1 — lihat OPEN DECISION #1)
    → Land Sale dibuat (land_sales, status=draft/akad)
    → DP / Cicilan (jika didukung, OPEN DECISION #2) / Pelunasan
         — SEMUA lewat ReceivePayment(LandSaleID=...), SATU PINTU yang sama
           dengan unit properti, hanya beda anchor
    → Akad/recognition (land_sales.status→akad, ATOMIK dengan posting
      revenue+HPP — persis pola RecordAkad unit properti)
    → HPP Land diambil dari land_allocations (snapshot dibuat di titik Akad)
```
Tidak ada payment system kedua — hanya perluasan anchor pada tabel yang sudah ada, mengikuti pola yang sudah dipakai untuk `charge_group_id`.

⚠️ **Catatan implementasi (bukan blocker desain, masuk test matrix §K):** perlu grep menyeluruh untuk laporan/query mentah yang mengasumsikan `termin_payments.unit_id`/`receipts.unit_id` SELALU NOT NULL (mis. `JOIN units` tanpa `LEFT JOIN`) sebelum migrasi ini dieksekusi — bukan risiko desain, tapi risiko regresi laporan yang harus diuji eksplisit di LT-5.

---

## 4. Accounting boundary — source of truth per domain

🟢 **READY.** Tidak ada duplikasi accounting engine di desain ini.

| Domain | Source of truth | Engine yang dipakai |
|---|---|---|
| Quantity/stok | `land_stock.total/reserved/sold` (counter) + `land_stock_reservations` | Baru (`internal/land`), tapi pola row-lock identik `bookings` |
| Sale price | `land_sales.unit_price_snapshot`/`dpp_amount`/`gross_amount` | Baru, struktur identik `sale_contracts` (reuse struktur PPN, bukan mesin PPN kedua) |
| Revenue | Jurnal Event-3-equivalent milik `land_sales` sendiri | `internal/ledger` — **engine yang sama**, akun tujuan baru (`4-1100`, OPEN DECISION #3) |
| HPP | `internal/allocation` `Compute()` per-pool + `land_allocations` snapshot | **Engine yang sama**, direstrukturisasi jadi per-pool (§E dokumen utama), bukan engine kedua |
| Payment | `internal/sale/collection.go` `ReceivePayment` + `termin_payments` | **Engine yang sama**, anchor diperluas (§3 di atas) — bukan payment system kedua |
| Document/kwitansi | W-2 Document Numbering Engine (`last_val`, per tenant+jenis) | **Engine yang sama**, sudah generik, tidak bergantung `unit_id` |

Tidak ada satu pun baris di tabel ini yang mengusulkan mesin baru — semua "baru" hanya di level DATA (tabel `land_*`), bukan di level ENGINE.

---

## 5. W-13 cleanup — audit ulang 2 charge_group

🟡 **SEBAGIAN READY, SEBAGIAN OPEN BUSINESS DECISION** (lihat rincian).

**Grup 547** (tenant 9900246, unit 2102) — 🟢 **bersih, siap cleanup:**
- Nol baris di `termin_payments`, `receipts`, `payment_allocations` yang mereferensikan grup ini.
- Sequence cleanup: `CancelGroup(547)` (endpoint existing) → item-itemnya berstatus `cancelled` → tidak ada jurnal dibuat (dikonfirmasi dari kode: `CancelGroup` hanya membuat jurnal PEMBALIK bila ada piutang ter-invoice via `syncRecognitionInTx`, dan grup ini `recognized_amount=0` di semua itemnya sehingga tidak ada apa pun untuk dibalik — pemanggilan `syncRecognitionInTx` di sini akan no-op).
- Tidak menyentuh unit 1714 sama sekali (unit berbeda, tenant berbeda).

**Grup 11** (tenant 9900245, unit 1433) — 🔴→🟡 **BUKAN bersih, jangan dieksekusi otomatis:**
- **Ditemukan** (koreksi atas draf sebelumnya): `termin_payments.id=1449` (Rp1.000.000, `journal_entry_id=2391`, posted 2026-08-04) + `payment_allocations.id=1574` mengalokasikan penuh ke `charge_items.id=19` (item grup 11).
- Jurnal 2391: Dr 2-2400 Titipan Realisasi / Cr 2-2400 Titipan Realisasi (akun sama, net nol ke neraca) — **transfer internal** dari `charge_groups.id=7` (kind=`realization`, unit 1433, status=`settled`) ke grup 11. Deskripsi: "Transfer titipan realisasi grup #7 → grup #11".
- **`CancelGroup(11)` akan DITOLAK** oleh sistem sendiri — guard `ErrGroupHasMoney` (`internal/charge/service.go:359-361`) memeriksa `sum.Paid` yang dihitung dari `payment_allocations` (BUKAN dari `recognized_amount`), dan akan mengembalikan Rp1.000.000 untuk item 19. Ini BUKAN keputusan desain — ini fakta yang sistem sudah tegakkan sendiri.
- **Tidak menyentuh unit 1714** — unit berbeda, tenant berbeda, tidak ada jalur apa pun yang bersinggungan.
- **Tidak ada payment/receipt/document lain** yang bergantung ke grup ini di luar yang sudah disebut (`receipts` untuk `termin_payment_id=1449` = 0 baris — belum pernah dicetak kwitansi untuk penerimaan ini).
- **Tidak boleh delete/reversal tanpa business rule eksplisit** (instruksi Anda) — disposisi Rp1.000.000 ini menjadi **OPEN BUSINESS DECISION #5** (§7).

**Exact cleanup sequence yang AMAN dieksekusi sekarang (tanpa menunggu keputusan #5):** hanya grup 547. Grup 11 menunggu.

---

## 6. Final invariants — Land

🟢 **READY**, ditambahkan ke §J dokumen utama:

| Kode | Invariant |
|---|---|
| INV-LAND-1 | `land_stock.reserved_quantity_m2 + land_stock.sold_quantity_m2 ≤ land_stock.total_quantity_m2`, selalu |
| INV-LAND-2 | `available = total − reserved − sold`, dihitung selalu, tidak pernah disimpan sebagai kolom terpisah (satu SoT) |
| INV-LAND-3 | `available ≥ 0`, `reserved ≥ 0`, `sold ≥ 0` — tidak pernah negatif |
| INV-LAND-4 | Kuantitas tidak pernah hilang: `Σ(land_sales.quantity_m2 status≠cancelled) == land_stock.sold_quantity_m2`, per proyek |
| INV-LAND-5 | Reservasi konkuren tidak boleh oversell — ditegakkan via row-lock (`SELECT...FOR UPDATE`) pada `land_stock` sebelum increment `reserved` |
| INV-LAND-6 | `land_sales.unit_price_snapshot`/`dpp_amount`/`gross_amount` immutable setelah baris dibuat — tidak ada path `UPDATE` terhadap kolom ini |
| INV-LAND-7 | `land_allocations` immutable setelah recognition — hanya `tx.Create`, mewarisi pola `allocation_snapshots` persis (§1) |
| INV-LAND-8 | Jurnal `land_sales` append-only — koreksi hanya via jurnal pembalik (invariant #5 CLAUDE.md, tidak ada pengecualian) |
| INV-LAND-9 | Tenant isolation — kelima tabel baru wajib `tenant_id` + GORM global scope, diuji integration test lintas-tenant (invariant #6 CLAUDE.md) |
| INV-LAND-10 | `land_sale` baru **tidak pernah** memicu tulis-ulang `AllocationSnapshot`/jurnal unit properti manapun — dibuktikan by construction di §1 (tidak ada path recompute-and-overwrite di codebase) |

---

## 7. Empat (+1) Open Business Decision

Tidak ditebak atau diputuskan sepihak — masing-masing dengan opsi + konsekuensi teknis + default rekomendasi (dipakai HANYA bila owner belum sempat menjawab sebelum increment terkait, dan bisa diubah karena belum ada histori yang bergantung).

### OD-1 — Booking fee reservasi Kelebihan Tanah
- **Keputusan dibutuhkan:** apakah `land_stock_reservations` memerlukan fee (seperti `bookings.booking_fee`/`fee_disposition` unit properti)?
- **Opsi A:** tanpa fee — reservasi murni penguncian kuantitas sementara (TTL pendek).
- **Opsi B:** dengan fee — mewarisi state machine `fee_disposition` (held/transferred/forfeited/pending_refund/refunded/recognized) seperti `bookings`.
- **Konsekuensi teknis:** Opsi B menambah kompleksitas signifikan (payment door harus menerima pembayaran fee SEBELUM `land_sales` ada — anchor ketiga: `reservation_id`; state machine fee harus direplikasi). Opsi A jauh lebih sederhana, tidak menyentuh payment door sampai `land_sales` benar-benar dibuat.
- **Default rekomendasi:** **Opsi A (tanpa fee)** — nilai Kelebihan Tanah biasanya jauh lebih kecil dari unit rumah, dan tidak ada requirement/dokumen manapun yang pernah menyinggung fee untuk ini.

### OD-2 — Dukungan cicilan/KPR untuk `land_sales`
- **Keputusan dibutuhkan:** apakah standalone Land Sale mendukung skema pembayaran bertahap (seperti `payment_scheme_id`/`scheme_state` unit properti), atau tunai/lunas saja.
- **Opsi A:** tunai/lunas saja — `recognition_date` diisi manual admin saat Akad, tanpa state machine skema.
- **Opsi B:** dukung cicilan — perlu mereplikasi `PaymentSchemePolicy` untuk entitas non-unit (perluasan besar, karena seluruh `internal/scheme` hari ini juga unit-anchored).
- **Konsekuensi teknis:** Opsi B menambah cakupan implementasi berkali lipat (state machine skema, gate Akad per skema, dsb) — TIDAK termasuk di rencana LT-0..LT-8 manapun bila dipilih.
- **Default rekomendasi:** **Opsi A (tunai/lunas)** — semua contoh yang pernah diberikan (termasuk skenario "dua sales rebutan stok tanah") mengasumsikan transaksi sederhana; isolatable ke `land_sales` saja bila nanti mau ditambah cicilan (tidak mengubah `land_stock`/`land_allocations`).

### OD-3 — Kode akun revenue final
- **Keputusan dibutuhkan:** kode & nama pasti akun pendapatan Kelebihan Tanah (arah "terpisah dari rumah" sudah LOCKED via R-1, hanya angka presisnya yang belum).
- **Opsi:** `4-1100 Pendapatan Penjualan Tanah` (usulan existing di R-1) vs kode lain sesuai bagan akun final klien.
- **Konsekuensi teknis:** murni data seed (`chart_of_accounts` + `product_types.revenue_account_code`), tidak ada logika kode yang bergantung pada angka spesifiknya.
- **Default rekomendasi:** pakai `4-1100` sesuai usulan R-1 kecuali klien punya bagan akun standar lain yang harus diikuti.

### OD-4 — PPN Kelebihan Tanah
- **Keputusan dibutuhkan:** perlakuan PPN atas penjualan Kelebihan Tanah (tanah kavling/kelebihan tanah punya aturan PPN yang berbeda dari rumah dalam banyak yurisdiksi).
- **Opsi:** ikuti aturan `is_pkp`/`vat_rate_snapshot` yang sama seperti unit properti, atau aturan khusus tanah.
- **Konsekuensi teknis:** struktur kolom (`dpp_amount`/`is_pkp`/`vat_rate_snapshot`/`gross_amount`) sudah disiapkan generik di `land_sales` (§B.3 dokumen utama) — tinggal isi aturan resolusinya begitu ada kepastian.
- **Default rekomendasi:** **tidak ditebak** — `// TODO(tax-advisor)` dipertahankan persis seperti R-1, sesuai aturan ketidakpastian CLAUDE.md. Ini SATU-SATUNYA item di seluruh desain yang secara eksplisit tidak boleh punya default kode jalan.

### OD-5 — Disposisi Rp1.000.000 pada charge_group id=11 *(baru, ditemukan sesi ini)*
- **Keputusan dibutuhkan:** ke mana Rp1.000.000 titipan yang sudah "menempel" di grup 11 (unit 1433, tenant 9900245) harus berakhir.
- **Opsi A:** transfer balik ke grup 7 (kind=`realization`, sudah `settled`) — grup 11 kembali ke nol, baru bisa `CancelGroup`.
- **Opsi B:** biarkan menempel, tunda cleanup grup 11 sampai LT-5 selesai, lalu pindahkan jadi DP `land_sale` pertama untuk pembeli unit 1433.
- **Opsi C:** lainnya — hanya diketahui pihak yang mencatat transfer #7→#11 pada 2026-08-04.
- **Konsekuensi teknis:** Opsi A paling cepat (grup 11 langsung bisa dibersihkan seperti 547). Opsi B menahan grup 11 tetap terbuka lebih lama tapi menjaga jejak uang tetap sinkron dengan niat aslinya (kalau memang untuk Kelebihan Tanah).
- **Default rekomendasi:** **tidak ada default** — ini uang riil dengan histori transfer yang niatnya tidak diketahui dari data. Grup 11 dibiarkan terbuka apa adanya sampai dijawab; tidak masuk urutan LT manapun sampai ada jawaban.

---

## FINAL ARCHITECTURE SIGN-OFF REVIEW

### 🟢 READY — bisa diimplementasikan tanpa keputusan tambahan
- LT-0: filter unit 5931 dari read path (`/penjualan`)
- LT-1: `ProductCategory.land` + `ParticipatesInCostPool(pool)`
- LT-2: `units.land_area` (kolom baru, default 0, no backfill)
- LT-3: `land_stock` (pool level project, admin-entered)
- Desain HPP historical immutability (§1) — dikonfirmasi by construction, tidak perlu perubahan
- Keputusan v1 tidak memakai `hpp_trueup` untuk Land (§2) — dikoreksi dan dikunci pada review ini
- Desain payment-door extension: `unit_id` nullable + `land_sale_id` pada `termin_payments`/`receipts` (§3) — additive, precedented, tidak membuat payment engine kedua
- Accounting boundary/SoT (§4) — tidak ada duplikasi engine
- Cleanup charge_group 547 (§5) — bersih, siap `CancelGroup` kapan saja

### 🟡 OPEN BUSINESS DECISION — butuh keputusan owner
- OD-1: booking fee reservasi Land
- OD-2: dukungan cicilan `land_sales`
- OD-3: kode akun revenue final (arah sudah locked, angka belum)
- OD-4: PPN Kelebihan Tanah
- OD-5 *(baru)*: disposisi Rp1.000.000 pada charge_group id=11

### 🔴 DESIGN RISK — desain harus diperbaiki sebelum coding
**Tidak ada yang tersisa.** Dua risiko desain riil ditemukan pada review ini (kesalahan klaim soal `hpp_trueup` di §2, dan payment-door `unit_id NOT NULL` yang belum diverifikasi di §3) — keduanya sudah diperbaiki langsung di `docs/kelebihan-tanah-final-architecture-2026-08.md` pada sesi yang sama, dengan desain pengganti yang konkret dan berbasis preseden kode yang ada.

---

Tidak ada 🔴 tersisa. Berhenti di sini, menunggu approval Anda untuk lanjut ke implementasi (LT-0 dst) — dan menunggu jawaban OD-1 s.d. OD-5 sebelum increment yang bergantung padanya (LT-4/LT-5/LT-8) dieksekusi.
