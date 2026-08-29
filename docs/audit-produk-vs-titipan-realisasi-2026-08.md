# Audit domain — Produk Penjualan vs Titipan Biaya Realisasi

Tanggal: 2026-08-06 · Status: **AUDIT — belum ada perubahan kode**
Pemicu: koreksi domain klien 2026-08-06.

> **Aturan bisnis baru (klien):** produk penjualan = **Rumah, Ruko, Kavling, Kelebihan Tanah**.
> **PDAM, Listrik, BPHTB, Notaris** = komponen **Titipan Biaya Realisasi**, ditagihkan lewat
> Billing Batch 2 — **bukan** lewat lifecycle penjualan produk.

Addendum untuk `docs/uat-fe-proyek-tab-unit-audit-2026-08.md` (Opsi A dikonfirmasi: tabel `units`
tidak di-rename, hanya kosakata UI yang berubah).

---

## 1. Verdict

**Domain model saat ini BELUM mencerminkan keputusan tersebut.** Ada tiga overlap nyata dan satu
kekosongan master.

Dan jawaban langsung atas pertanyaan Anda — *"apakah Product Catalog dipakai juga sebagai master untuk
Billing, misalnya hanya untuk COA mapping?"*:

> **Tidak. Nol coupling.** Package `internal/charge` tidak pernah menyentuh `product_types` /
> `ProductType` / `ProductPolicy` — terverifikasi lewat grep menyeluruh. `charge_items.label` adalah
> `VARCHAR(120)` **teks bebas**, tanpa referensi ke master mana pun.

Jadi "Product Catalog sebagai master billing" bukan deskripsi sistem sekarang — itu akan menjadi
**keputusan arsitektur baru**. Karena itu permintaan Anda untuk menjelaskan arsitekturnya dulu tepat.

---

## 2. Peta: di mana keempat komponen ini hidup sekarang

| Komponen | Product Catalog (`product_types`) | Charge Group (`charge_items`) | Modul Notaris (`notary_deposits`) |
|---|---|---|---|
| Rumah | ✅ `property` → **4-1000** | — | — |
| Ruko | ✅ `property` → **4-1000** | — | — |
| Kavling | ✅ `property` → **4-1000** | — | — |
| **Kelebihan Tanah** | ✅ `non_property` → **4-2000 (pendapatan)** | ✅ kind `addon` → **2-2400 (kewajiban)** | — |
| **PDAM** | ✅ `non_property` → **4-2000 (pendapatan)** | ✅ item `realization` → 2-2400 | — |
| **Listrik** | ❌ tidak ada master | ✅ teks bebas → 2-2400 | — |
| **BPHTB** | ❌ tidak ada master | ✅ teks bebas → 2-2400 | — |
| **Notaris** | ❌ tidak ada master | ✅ teks bebas → **2-2400** | ✅ modul sendiri → **2-2300** |

Ringkasnya: **yang seharusnya jadi master (jenis titipan) justru teks bebas, dan yang seharusnya bukan
produk (PDAM) justru ada di master produk.** Persis terbalik.

---

## 3. Temuan

### D-1 · PDAM dimodelkan sebagai produk yang dijual — dan mengakui pendapatan

`defaultProductTypes` (`backend/internal/project/product_type.go:240`) dan seed migrasi
`000057_product_types.up.sql` memasukkan:

```
{Code: "pdam", Name: "Sambungan PDAM", Category: non_property, RevenueAccountCode: "4-2000"}
```

Konsekuensi konkret, bukan teoretis: unit dengan `unit_type = pdam` yang di-BAST akan melewati seluruh
blok HPP (`sale/service.go:460` — `!policy.ParticipatesInHPP()` → `HPPMethodNone`) lalu **mengakui
pendapatan penuh ke 4-2000 Pendapatan Lain-lain**.

Menurut aturan baru, PDAM tidak boleh pernah menyentuh `4-xxxx`. **Ini pelanggaran langsung.**

### D-2 · Kelebihan Tanah punya dua perlakuan akuntansi yang saling bertentangan

| Jalur admin | Jurnal | Perlakuan |
|---|---|---|
| Dibuat sebagai **unit** `unit_type = kelebihan_tanah`, lalu BAST | Cr **4-2000** | Pendapatan |
| Ditagih sebagai **charge group** `kind = addon` | Cr **2-2400** | Kewajiban (titipan) |

Ini bukan dugaan. Di `charge/service.go:396`, cabang `if g.Kind == KindAddon` **hanya mengubah teks
deskripsi**; jurnalnya identik dengan `realization` — `Dr Kas/Bank / Cr 2-2400 Titipan Realisasi`.

Menurut aturan baru, Kelebihan Tanah **adalah produk penjualan** → maka **jalur `addon` yang salah**.

### D-3 · Notaris punya dua jalur kewajiban dengan dua akun berbeda

| Jalur | Akun | Tabel | Menu |
|---|---|---|---|
| Modul `internal/notary` (UAT Batch 2 §3) | **2-2300** Titipan Notaris | `notary_deposits` | Sidebar → Titipan Notaris |
| Charge item berlabel "Notaris" di grup `realization` | **2-2400** Titipan Realisasi | `charge_items` | Penjualan → Grup Tagihan |

Dua *source of truth* untuk peristiwa ekonomi yang sama. Saldo titipan notaris satu tenant terbelah ke
dua akun tergantung menu mana yang dipakai admin — dan tidak ada satu pun mekanisme yang mencegahnya.
`EQ_NotaryDeposit` di consistency suite hanya mengunci Σ deposit `held` == saldo 2-2300; ia **tidak
tahu** ada uang notaris lain yang duduk di 2-2400.

Ini overlap paling serius dalam audit ini, dan ia **sudah ada sebelum** koreksi domain klien.

### D-4 · Tidak ada master jenis Titipan Biaya Realisasi sama sekali

`charge_items.label VARCHAR(120)` = teks bebas. Form pembuatan grup (`ChargeGroupsPanel.tsx:532-534`)
meng-hardcode tiga baris default:

```tsx
{ label: "Notaris", amount: "", due_date: "" },
{ label: "PDAM",    amount: "", due_date: "" },
{ label: "Listrik", amount: "", due_date: "" },
```

…dan placeholder `"mis. BPHTB"`. Akibatnya `"PDAM"`, `"Pdam"`, `"Sambungan PDAM"`, `"PDAM + meteran"`
adalah **empat jenis berbeda** di data. Yang hilang karenanya:

- Tidak bisa laporan titipan **per jenis** (berapa total BPHTB tahun ini?).
- Tidak bisa mapping COA per jenis (semua dipaksa ke satu akun 2-2400).
- Tidak bisa harga/vendor acuan per jenis.
- Tidak bisa aturan refundable per jenis (BPHTB disetor ke negara ≠ PDAM yang bisa sisa).

### D-5 · UI `addon` menyesatkan

Hint peringatan *"dicatat sebagai Titipan Realisasi (2-2400) — bukan pendapatan"* di
`ChargeGroupsPanel.tsx` hanya dirender bila `kind === "realization"`. Padahal `addon` memposting ke akun
yang **persis sama**. Admin yang menagih kelebihan tanah lewat addon mengira ia mencatat penjualan.

### D-6 · Listrik & BPHTB tidak ada di master mana pun

Hanya hidup sebagai string yang diketik ulang di setiap kontrak.

### D-7 (latent, bukan pelanggaran) · fallback legacy di `resolveUnitPolicy`

`sale/service.go:146` — bila resolver tidak terpasang, unit diperlakukan sebagai `property` dengan akun
`4-1000`. Terdokumentasi sebagai jalur unit-test saja (produksi selalu memasang resolver di `main.go`)
dan dikunci integration test. Saya catat sebagai risiko laten, bukan temuan.

---

## 4. Tiga arsitektur yang mungkin

### Opsi 1 — Dua master, satu seam keputusan akuntansi ★ **rekomendasi**

```
product_types                    realization_charge_types  (BARU)
─────────────                    ────────────────────────
rumah, ruko, kavling,            pdam, listrik, bphtb, notaris,
kelebihan_tanah                  + bebas ditambah tenant
        │                                  │
        │ ProductPolicy                    │ ChargeTypePolicy
        │ .RevenueAccountCode              │ .DepositAccountCode
        └──────────────┬───────────────────┘
                       ▼
        domain.BillingTreatment   ← SATU fungsi kanonik
        revenue │ deposit_liability
```

- `product_types` menjadi **satu-satunya master produk yang dijual**. `pdam` dicabut.
- `realization_charge_types` (tabel baru) menjadi **satu-satunya master jenis titipan**:
  `code`, `name`, `deposit_account_code` (default `2-2400`), `default_vendor`, `default_amount`,
  `is_refundable`, `is_active`.
- `charge_items` mendapat `charge_type_id` (FK logis). `label` dipertahankan sebagai teks tampilan
  untuk baris lama (kompatibilitas histori) tapi item baru **wajib** merujuk master — fail-closed,
  pola yang sama dengan `ResolveProductPolicy`.
- Yang dibagi bukan tabelnya, melainkan **kontrak domain**: `domain.BillingTreatment` sejajar
  `domain.ProductCategory` — satu-satunya fungsi yang menjawab *"peristiwa ini menghasilkan pendapatan
  atau kewajiban?"*. `ProductPolicy` selalu `revenue`; `ChargeTypePolicy` selalu `deposit_liability`.

**Kenapa ini yang benar:** dua master memang punya atribut yang berbeda secara hakiki. Produk punya
partisipasi HPP, lifecycle 9-state, luas & harga sebagai basis alokasi. Titipan punya vendor, akun
kewajiban, sifat refundable, dan tidak punya lifecycle sama sekali. Menyatukannya menghasilkan tabel
lebar yang setengah kolomnya selalu NULL.

**Bonus arsitektural:** `deposit_account_code` per jenis langsung **menyelesaikan D-3** — `notaris`
dapat dipetakan ke `2-2300` sementara `pdam`/`listrik`/`bphtb` ke `2-2400`, atau semuanya ke `2-2400`.
Granularitas akun jadi konfigurasi tenant, bukan kode.

### Opsi 2 — Satu tabel `catalog_items` dengan diskriminator `kind`

`kind: sale_product | realization_charge` dalam satu master. **Tidak direkomendasikan:** menyentuh
semantik `units.unit_type` dan `charge_items` sekaligus (blast radius besar), menghasilkan tabel lebar
dan jarang terisi, dan unifikasi ini tidak dibayar oleh manfaat bisnis apa pun.

### Opsi 3 — Kategori ketiga `realization_deposit` di `product_types`

Terlihat hemat ("satu master"), tapi **cacat tipe**: `ProductPolicy` membawa `RevenueAccountCode`, dan
untuk item titipan akun itu adalah kewajiban — namanya berbohong. Lebih buruk lagi, ia tetap
mengizinkan pembuatan **unit** bertipe PDAM — persis bug D-1 yang sedang kita cabut. **Ditolak.**

---

## 5. Rencana perubahan bila Opsi 1 disetujui (belum dikerjakan)

### 5.1 Cabut PDAM dari Product Catalog — dengan audit data dulu, bukan langsung hapus

Resolver bersifat **fail-closed**: jika `pdam` dihapus sementara masih ada unit yang memakainya, unit
itu akan **menolak BAST** selamanya. Urutannya harus:

1. **Audit dulu** di DB tiap tenant: berapa unit `unit_type = 'pdam'`, berapa di antaranya sudah BAST.
2. Bila nol unit → hapus baris seed + `DELETE` aman dari `product_types`.
3. Bila ada unit **belum** BAST → migrasikan menjadi charge item titipan, lalu hapus unitnya.
4. Bila ada unit **sudah** BAST → pendapatan `4-2000` sudah diakui dan jurnalnya **immutable**
   (invariant #5). Koreksi hanya lewat **jurnal pembalik** + reklasifikasi ke `2-2400`, dan itu
   **keputusan klien**, bukan keputusan kita. Sampai diputuskan: `is_active = false` agar tidak ada
   unit PDAM baru, tanpa merusak histori.

Seed `defaultProductTypes` dan migrasi `000057` diperbaiki untuk tenant **baru** tanpa menunggu poin 4.

### 5.2 Nasib `kind = addon`

Karena Kelebihan Tanah kini resmi produk penjualan, jalur `addon` kehilangan alasan keberadaannya.
Dua sub-opsi — **butuh keputusan Anda**:

- **(a)** Hapus `addon`. Kelebihan tanah selalu jadi unit/produk, ditagih lewat harga kontrak.
  Paling bersih untuk SSOT; mengubah alur kerja admin.
- **(b)** Pertahankan `addon` untuk penagihan produk tambahan tanpa membuat unit, tapi **perbaiki
  akuntansinya** menjadi pendapatan (bukan `2-2400`) dan **tautkan ke `product_types`**.

Sebelum keduanya: audit apakah sudah ada data `charge_groups.kind = 'addon'` di produksi. Bila ada,
jurnalnya sudah terlanjur `2-2400` dan perlu keputusan reklasifikasi yang sama seperti §5.1 poin 4.

### 5.3 Nasib modul Titipan Notaris (D-3)

Rekomendasi: **satu alur, dua akun bila klien mau**. Notaris ditagih lewat charge group seperti
komponen realisasi lain; `realization_charge_types.deposit_account_code` untuk `notaris` disetel ke
`2-2300` sehingga granularitas akun yang sudah ada tetap terjaga. Modul `notary_deposits` dibekukan
read-only untuk data lama — `EQ_NotaryDeposit` tetap hijau untuk baris historis, tidak ada jurnal yang
dihapus. **Butuh keputusan klien**: apakah notaris tetap punya akun sendiri (`2-2300`) atau dilebur ke
`2-2400`.

### 5.4 Frontend

- `ChargeGroupsPanel`: item dipilih dari master (dropdown), bukan diketik; hapus tiga baris default
  hardcode; hint akun ditampilkan untuk **semua** kind, memakai akun dari master (memperbaiki D-5).
- Pengaturan → seksi baru **"Jenis Titipan Realisasi"**, CRUD sejajar Katalog Produk.
- Katalog Produk: teks penjelas bahwa isinya **hanya produk yang dijual**; PDAM/Listrik/BPHTB/Notaris
  diarahkan ke seksi titipan.

### 5.5 Yang **tidak** berubah

Aturan K-1..K-5 tetap benar dan tidak disentuh. Kas masuk tetap satu tabel (`termin_payments`),
sub-ledger alokasi tetap satu (`payment_allocations`), aging tetap satu mesin (`BuildARAging`),
`2-2400` tetap kewajiban. Resolver produk tetap fail-closed.

---

## 6. Dampak ke rencana Tab Unit

Opsi A tetap berlaku, dengan satu penyederhanaan: setelah PDAM keluar, **semua** produk yang tersisa
(Rumah, Ruko, Kavling, Kelebihan Tanah) adalah objek fisik yang dijual. Maka:

- **P2-B gugur sebagian** — tidak perlu lagi seksi terpisah "Produk & Layanan Lain" di tab Unit, karena
  tidak ada lagi produk jasa di katalog.
- Kosakata UI: cukup **"Produk"** (tidak perlu "Unit & Produk").
- **Pertanyaan domain yang terbuka:** `kelebihan_tanah` saat ini berkategori `non_property`, artinya
  **tidak ikut pool HPP**. Tapi tanah lebih adalah tanah — biaya perolehannya nyata. Apakah menurut
  klien kelebihan tanah **ikut** alokasi HPP? Ini mengubah `ParticipatesInHPP()` dan karenanya angka
  HPP per unit. Sesuai aturan ketidakpastian CLAUDE.md, saya tidak menebak.

---

## 7. Keputusan yang saya butuhkan sebelum menulis kode

1. **Opsi 1** untuk arsitektur master (rekomendasi) — setuju?
2. **`kind = addon`**: hapus (a) atau perbaiki jadi pendapatan + tautan ke Product Catalog (b)?
3. **Notaris**: tetap akun sendiri `2-2300`, atau dilebur ke `2-2400`?
4. **Kelebihan Tanah**: ikut pool HPP (`property`) atau tidak (`non_property`, seperti sekarang)?
5. Konfirmasi dari klien untuk `saleable_area` (masih terbuka dari audit sebelumnya).

Setelah nomor 1–4 dijawab, saya jalankan audit data di DB dulu (§5.1), lalu susun migrasi & implementasi.
