# UAT Frontend — Menu Proyek → Tab Unit
## Audit lengkap, daftar kekurangan, dan usulan implementasi

Tanggal: 2026-08-06 · Status: **AUDIT — belum ada perubahan kode**
Ruang lingkup: `frontend/components/proyek/*`, `frontend/app/(app)/proyek/[id]/unit/[unitId]/page.tsx`,
`frontend/components/settings/ProductTypesSection.tsx`, `backend/internal/project/*`.

---

## 0. Ringkasan eksekutif

| # | Pertanyaan audit | Verdict |
|---|---|---|
| A1 | "Tambah Unit" vs "Tambah Produk" | **Nama tertinggal dari model.** Backend sudah generik (unit = item jual apa pun), UI masih bilang "Unit". Tapi masalah sebenarnya bukan label — non-properti (PDAM) ikut lifecycle & kanban properti. |
| A2 | Jenis produk hardcode? | **Jalur unit BERSIH** — FE & BE dua-duanya baca Product Catalog, resolver fail-closed. Sisa 2 residu: taksonomi tandingan di ChargeGroupsPanel, dan UI menampilkan **kode** mentah, bukan nama katalog. |
| A3 | Product Type benar-benar CRUD? | **C ✓ R ✓ U sebagian ✗ D tidak ada.** Tenant *bisa* menambah jenis produk sendiri tanpa coding (terverifikasi). Tapi salah ketik = permanen. |
| A4 | Card punya Edit/Delete/Duplicate/Manage? | **Nol dari empat.** Card = satu `<Link>`. Backend juga **tidak punya** PATCH/DELETE/duplicate unit sama sekali. "Manage" (transition) sudah ada di backend tapi tidak terjangkau dari tab ini. |
| A5 | UX untuk ratusan unit | **Tidak layak skala.** Endpoint kirim semua unit tanpa paginasi/cari/urut; papan render semua kartu; satu-satunya filter adalah Fase; tidak ada aksi massal. |

**Dua temuan yang saya nilai P0 (bukan kosmetik):**

1. **`saleable_area` diberi label berbeda di dua form yang menulis kolom yang sama** — "Luas Bangunan (m²)" di `AddUnitButton`, "Luas Tanah (m²)" di `BulkUnitWizard`. Kolom ini adalah **basis alokasi HPP** (`basis_type: saleable_area`). Dua admin yang jujur mengikuti label masing-masing akan mengisi besaran yang berbeda maknanya → bobot alokasi campur aduk → **HPP per unit salah**, meskipun Σ alokasi tetap tie-out sempurna. Invariant #3 tetap hijau sementara angkanya salah — persis kelas bug yang paling mahal.
2. **Tidak ada cara mengoreksi unit setelah dibuat.** Tidak ada `PATCH /units/{id}` di backend. Salah ketik kode unit, salah harga, salah fase, salah luas → tidak bisa diperbaiki lewat aplikasi. Untuk ERP ini blocker operasional.

---

## A1 — "Tambah Unit" atau "Tambah Produk"?

### Temuan

Model backend **sudah** generik:

- `units.unit_type` = **kode product type** dari master `product_types` (`backend/internal/project/product_type.go`).
- Katalog bawaan: `rumah`, `ruko`, `kavling` (property) + `kelebihan_tanah`, `pdam` (non_property).
- `domain.ProductCategory` adalah aturan kanonik: `property` ⇒ ikut pool HPP + progress fisik; `non_property` ⇒ tidak.

Form penambahan **sudah** memakai istilah produk: field-nya berlabel **"Jenis Produk"** dan diisi dari
`fetchProductTypes()`. Yang tertinggal hanya kulit luarnya:

| Tempat | Teks sekarang |
|---|---|
| `ProyekDetailTabs.tsx` | tab **"Papan Unit"** |
| `AddUnitButton.tsx` | tombol **"+ Tambah Unit"**, judul modal **"Tambah Unit"** |
| `BulkUnitWizard.tsx` | **"⊞ Buat per Blok"** |
| `UnitBoard.tsx` | empty state **"Belum ada unit"** |

### Analisis (bukan sekadar label)

Mengganti "Unit" → "Produk" **saja** justru menutupi masalah yang lebih dalam:

- Semua produk — termasuk PDAM dan kelebihan tanah — masuk ke **papan kanban lifecycle properti** yang sama
  (`available → booked/reserved → ppjb → sold`). "PPJB PDAM" tidak bermakna. Non-properti tidak punya
  pipeline penjualan; ia adalah item tagihan.
- Ada **taksonomi tandingan**: `frontend/components/penjualan/ChargeGroupsPanel.tsx:586` meng-hardcode
  `<option value="addon">Produk Tambahan (kelebihan tanah, dll.)</option>`. Jadi "kelebihan tanah" hidup di
  **dua tempat**: sebagai product type di katalog, dan sebagai jenis charge di penjualan. Sebelum menamai
  ulang, ini harus diselesaikan — kalau tidak, kita cuma menambah kebingungan.

### Rekomendasi

**Opsi A (rekomendasi) — ganti kosakata UI, pertahankan tabel `units`, pisahkan tampilan per kategori.**

- Tabel `units` **tidak** di-rename. 18 tabel merujuk `unit_id` (kontrak, jadwal, snapshot HPP, jurnal,
  booking, komisi, pembatalan, charge group, titipan notaris). Rename = migrasi besar, nol nilai bisnis,
  dan menyentuh `allocation_snapshots` yang sudah beku. Ledger append-only tidak boleh diguncang untuk
  alasan penamaan.
- UI memakai kata payung **"Produk"**: tab **"Unit & Produk"**, tombol **"+ Tambah Produk"**.
- Papan **hanya** menampilkan produk `property` (kanban lifecycle punya makna di sana). Produk
  `non_property` tampil di **tabel terpisah** di tab yang sama ("Produk & Layanan Lain") tanpa kolom
  lifecycle — hanya kode, jenis, harga, status terjual/belum.
- `ChargeGroupsPanel` opsi `addon` diselaraskan: pilihan produk tambahan diambil dari katalog kategori
  `non_property`, bukan literal.

**Opsi B — rename entity `units` → `products`.** Tidak direkomendasikan: biaya migrasi tinggi, risiko
menyentuh snapshot HPP, manfaat murni semantik.

> Keputusan produk yang saya butuhkan dari Anda: **Opsi A atau B.** Sisa dokumen mengasumsikan A.

---

## A2 — Apakah jenis produk berasal dari Product Catalog/database?

### Verdict: YA di jalur unit — terverifikasi, bukan asumsi

**Frontend**
- `AddUnitButton.tsx`: `fetchProductTypes(token).then(pts => pts.filter(p => p.is_active))` — dropdown
  100% dari API. Ada empty-state informatif bila katalog kosong dan hint saat memilih non-properti.
- `BulkUnitWizard.tsx`: sumber yang sama.
- **Tidak ada** array literal jenis produk di komponen unit mana pun.

**Backend**
- `ResolveProductPolicy` **fail-closed** penuh: kode tak dikenal, produk nonaktif, akun pendapatan kosong/
  bukan tipe revenue/nonaktif, dan error DB → semuanya **error**, tidak ada fallback diam-diam ke `4-1000`.
- Akun pendapatan BAST di-resolve dari mapping katalog (COA-driven, editable per tenant).
- `defaultProductTypes` hanyalah **seed idempoten** saat register — bukan whitelist. Tenant bebas menambah.

### Residu yang masih perlu ditutup

| # | Temuan | Dampak | Keputusan |
|---|---|---|---|
| R-1 | `ChargeGroupsPanel.tsx:586` hardcode opsi `addon` "Produk Tambahan (kelebihan tanah, dll.)" | Taksonomi kedua di luar katalog | **Perbaiki** — ambil dari katalog `non_property` |
| R-2 | `UnitCard.tsx` & halaman detail unit menampilkan **`unit.unit_type` mentah** (`kelebihan_tanah`) alih-alih nama katalog ("Kelebihan Tanah") | Katalog adalah SSOT nama, UI mengabaikannya; user melihat slug | **Perbaiki** — BE sertakan `product_name` + `product_category` di DTO unit |
| R-3 | `ProductTypesSection.tsx` punya map `CATEGORY_LABEL`/`CATEGORY_HINT` hardcode | **Bukan defect.** `ProductCategory` memang enum tertutup di `domain` (menentukan partisipasi HPP). Yang hardcode hanya terjemahan Indonesianya. | Biarkan, beri komentar penjelas |

---

## A3 — Apakah Product Type benar-benar CRUD?

| Operasi | Status | Bukti |
|---|---|---|
| **C**reate | ✅ | `POST /product-types` + form di Pengaturan → Katalog Produk |
| **R**ead | ✅ | `GET /product-types` |
| **U**pdate | ⚠️ **sebagian** | `PATCH /product-types/{id}` hanya menerima `name`, `revenue_account_code`, `is_active`. `code` & `category` **immutable**. |
| **D**elete | ❌ **tidak ada** | Tidak ada route DELETE; UI hanya punya toggle aktif/nonaktif |

**Tenant bisa menambah jenis produk sendiri tanpa coding: YA.** Ini terverifikasi — tidak ada whitelist,
tidak ada enum kode produk, resolver membaca DB.

### Apakah immutability `code` & `category` itu benar?

**Ya, dan harus dipertahankan:**

- `code` adalah nilai yang tersimpan di `units.unit_type` (FK logis). Mengubahnya akan **memutus** seluruh
  unit yang sudah memakainya → `ResolveProductPolicy` gagal fail-closed di BAST berikutnya.
- `category` menentukan apakah produk **ikut pool HPP dan progress fisik**. Mengubahnya setelah ada unit =
  mereklasifikasi dasar HPP secara diam-diam — melanggar semangat invariant #4.

**Tapi UI tidak menjelaskan apa pun.** User melihat dua field yang tidak bisa disentuh tanpa alasan, lalu
menyimpulkan aplikasinya rusak. Ini kekurangan UX, bukan kekurangan aturan.

### Kekurangan nyata

| # | Kekurangan | Dampak |
|---|---|---|
| K-1 | Tidak ada DELETE sama sekali | Product type salah ketik (`ruk0`) permanen mengotori katalog. Toggle nonaktif menyembunyikannya dari dropdown, tapi baris tetap ada selamanya. |
| K-2 | Tidak ada indikator **pemakaian** | User tidak tahu apakah aman menonaktifkan sebuah produk — berapa unit yang memakainya? |
| K-3 | Tidak ada default per produk (harga acuan, luas acuan, preset `type_label`) | Tiap unit diketik ulang dari nol — beban terbesar saat input ratusan unit |
| K-4 | `code` & `category` terkunci tanpa penjelasan di UI | Terbaca sebagai bug |

---

## A4 — Aksi pada card produk: Edit / Delete / Duplicate / Manage

### Kondisi sekarang: **nol dari empat**

`UnitCard.tsx` seluruhnya adalah satu `<Link href="/proyek/{id}/unit/{unitId}">`. Tidak ada tombol,
tidak ada menu konteks. Halaman detail unit **sepenuhnya read-only** — tidak ada aksi apa pun.

Di backend:

```
GET  /projects/{id}/units            ← list
POST /projects/{id}/units            ← create
POST /projects/{id}/units/bulk       ← create massal
GET  /units/{id}                     ← detail
GET  /units/{id}/transitions         ← riwayat status
POST /units/{id}/transition          ← ubah status
```

**Tidak ada** `PATCH /units/{id}`, **tidak ada** `DELETE /units/{id}`, **tidak ada** duplicate.

Catatan penting: kemampuan **"Manage"** (ubah status, 9 state lifecycle, dengan gate approval untuk
`hold`/`blocked`) **sudah dibangun** di backend sejak Increment 6 — tapi dari tab Unit tidak ada satu pun
jalan untuk memanggilnya. Fitur yang sudah dibayar tapi tidak terlihat.

### Rancangan implementasi

#### 1. `PATCH /units/{id}` — Edit, dengan gate fail-closed

Field unit tidak sama derajatnya. Sebagian menyentuh basis akuntansi.

| Kelompok | Field | Kapan boleh diubah |
|---|---|---|
| **Bebas** | `type_label`, `buyer_ref` | Kapan saja |
| **Terkunci basis** | `saleable_area`, `list_price`, `unit_type`, `code`, `phase_id` | Hanya bila **semua** benar: `status = available`, **belum** ada `allocation_snapshots` untuk unit ini, **belum** ada `cost_entries.unit_id` (biaya direct), **belum** ada kontrak/booking |
| **Tidak pernah lewat PATCH** | `status` | Hanya via `POST /units/{id}/transition` (state machine + gate approval) |
| **Turunan** | `sale_date`, `sale_price` | Ditetapkan sistem saat BAST |

Alasan: `saleable_area` adalah **basis alokasi HPP**; mengubahnya setelah snapshot beku akan membuat
snapshot tidak lagi konsisten dengan master — melanggar invariant #3 & #5. Fail-closed: bila ada satu saja
referensi, PATCH tolak dengan **409** dan daftar alasan yang bisa dibaca manusia
("unit sudah punya kontrak SC/2026/000123"), bukan error generik.

**Audit trail:** setiap perubahan field kelompok "terkunci basis" dicatat ke tabel baru
`unit_revisions` (`unit_id`, `field`, `old_value`, `new_value`, `changed_by`, `reason` **wajib**,
`created_at`) — mengikuti pola `tenant_policy_changes` dari R-A. Migrasi baru, bukan AutoMigrate.

#### 2. `DELETE /units/{id}` — hapus aman, bukan hapus paksa

18 tabel merujuk `unit_id`. Hard delete tanpa gate akan meninggalkan jurnal & snapshot yatim →
melanggar invariant #5 (ledger append-only tidak boleh kehilangan konteks).

Aturan:
- Hapus **hanya** bila `status = available` **dan** nol referensi di: `sale_contracts`, `bookings`,
  `payment_schedules`, `cost_entries`, `allocation_snapshots`, `journal_lines`, `receipts`,
  `charge_groups`, `unit_status_transitions` (selain baris pembuatan), `commissions`, `cancellations`,
  `notary_deposits`, `hpp_trueup`.
- Bila ada referensi → **409** dengan daftar penghalang, dan UI menawarkan **"Arsipkan"** sebagai gantinya.
- Arsip = status `blocked` dengan alasan `archived` (sudah ada di state machine, ber-gate approval) —
  **bukan** kolom `deleted_at` baru. Reuse lifecycle yang sudah ada, jangan bikin konsep kedua.

#### 3. `POST /units/{id}/duplicate` — Duplicate

Body: `{ code | code_pattern, count?, phase_id? }`. Menyalin `unit_type`, `type_label`, `saleable_area`,
`list_price`, `phase_id`. Status hasil **selalu** `available`. Tidak menyalin `buyer_ref`, `sale_*`.
Atomik seperti `bulkCreateUnits`. Nilai tambahnya di atas Bulk Wizard: "satu lagi seperti ini, kode beda"
tanpa mengetik ulang harga & luas.

#### 4. "Manage" — tanpa endpoint baru

Modal **Kelola Status** memakai `GET /units/{id}/transitions` (riwayat) + `POST /units/{id}/transition`
yang **sudah ada**. Modal menampilkan status sekarang, transisi legal berikutnya, tanda gembok pada
transisi yang butuh approval, dan timeline riwayat.

#### 5. Card baru

```
┌──────────────────────────────────────┐
│ A-01                      [Tersedia] │  ← judul = Link ke detail
│ Rumah · 36/72                    ⋯   │  ← nama katalog, bukan slug; kebab menu
│ 72 m² luas jual                      │
│ ──────────────────────────────────── │
│ Harga            Rp 450.000.000      │
└──────────────────────────────────────┘
                                    ⋯ → Edit
                                        Duplikat
                                        Kelola status
                                        ─────────
                                        Hapus / Arsipkan
```

Catatan teknis: card sekarang adalah `<Link>` yang membungkus **seluruh** isi. Menaruh tombol di dalam
`<a>` adalah HTML tidak valid dan menyebabkan navigasi tak sengaja. Struktur harus dibalik: wrapper `<div>`,
judul yang jadi `<Link>`, menu kebab sebagai sibling.

---

## A5 — Review UX tab Unit untuk admin yang mengelola ratusan unit

### Bottleneck (diurutkan berdasarkan rasa sakit)

| # | Bottleneck | Bukti | Dampak pada 500 unit |
|---|---|---|---|
| B-1 | **Tidak ada paginasi/pencarian/urutan di API** | `listUnitsByProject` mengembalikan array telanjang, tanpa envelope, tanpa parameter | Satu payload berisi 500 objek tiap kali tab dibuka |
| B-2 | **Semua kartu dirender** | `UnitBoard` map seluruh `filtered`, tanpa virtualisasi | 500 node DOM; scroll berat; kolom "Tersedia" bisa berisi 400 kartu |
| B-3 | **Tidak ada pencarian kode unit** | Satu-satunya kontrol adalah `Select` "Filter Fase" | Mencari "C-17" = scroll manual |
| B-4 | **Tidak ada filter status / jenis produk / rentang harga** | — | Tidak bisa menjawab "kavling mana yang belum terjual di fase 2" |
| B-5 | **Tidak ada aksi massal** | Bulk Wizard hanya **membuat** | Naikkan harga satu blok 5% = mustahil (bahkan satuan pun mustahil, lihat A4) |
| B-6 | **Kanban jadi view utama** | 5 kolom fixed | Kanban efektif untuk ±30 kartu. Untuk ratusan, tabel padat jauh lebih baik |
| B-7 | **Header tab kosong** | `<div className="flex justify-end mb-3">` berisi 2 tombol saja | Tidak ada ringkasan: total unit, terjual, nilai stok, nilai terjual |
| B-8 | **Label `saleable_area` bertentangan** | "Luas Bangunan (m²)" vs "Luas Tanah (m²)" untuk kolom yang sama | **Basis alokasi HPP tercemar** — lihat §0 |
| B-9 | **Tidak ada pemisahan LT/LB** | `Unit` hanya punya `SaleableArea`; "36/72" hidup di `type_label` teks bebas | Tidak bisa laporan LT/LB; tidak bisa pakai luas tanah sebagai basis alokasi alternatif |
| B-10 | **Filter tidak persist** | State lokal `useState`, bukan URL | Buka detail unit → kembali → filter fase reset |
| B-11 | **Tidak ada ekspor** | — | Developer hidup dari price list; tidak ada CSV |
| B-12 | **Produk non-properti di kanban properti** | `COLUMNS` sama untuk semua | "PPJB PDAM" |
| B-13 | **Pola kode unit kaku** | Bulk Wizard mengunci `${block}-${NN}` | Tidak bisa pola tenant sendiri, tidak bisa isi celah nomor yang bolong |
| B-14 | **Detail unit read-only & buntu** | Tidak ada aksi, tidak ada navigasi antar-unit | Kembali ke papan untuk tiap unit |

### Rancangan UX

**Header tab — strip KPI + kontrol**

```
┌─────────────────────────────────────────────────────────────────────────────┐
│  248 unit   ·   181 tersedia   ·   67 terjual                               │
│  Nilai stok Rp 81,4 M          ·   Nilai terjual Rp 30,1 M                  │
├─────────────────────────────────────────────────────────────────────────────┤
│ [🔍 Cari kode unit…]  [Fase ▾] [Status ▾] [Jenis ▾]   [▦ Papan │ ☰ Tabel]   │
│                                        [+ Tambah Produk] [⊞ Buat per Blok]  │
└─────────────────────────────────────────────────────────────────────────────┘
```

Angka KPI **diturunkan dari `meta` endpoint** (agregat SQL), bukan dihitung ulang di browser dari halaman
yang sedang tampil — konsisten dengan aturan SSOT: satu definisi, dihitung di backend.

**View "Tabel" (default bila unit > 60)**

```
☐ │ Kode  │ Jenis          │ Fase   │ Luas jual │ Harga         │ Status    │ Pembeli   │ ⋯
☐ │ A-01  │ Rumah 36/72    │ Fase 1 │    72 m²  │ 450.000.000   │ Tersedia  │ —         │ ⋯
☑ │ A-02  │ Rumah 36/72    │ Fase 1 │    72 m²  │ 450.000.000   │ PPJB      │ Budi S.   │ ⋯
```
Header sticky, kolom bisa diurutkan, checkbox pilih. Saat ada yang terpilih muncul **bar aksi massal**:
`3 unit terpilih — [Pindah fase] [Ubah harga] [Ubah status] [Arsipkan] [Ekspor]`.

**View "Papan"** tetap ada untuk mode pipeline (< 60 unit atau setelah difilter), dengan virtualisasi
per kolom.

**Aksi massal & invariant.** Ubah harga / pindah fase massal memakai **PATCH per unit di dalam satu
transaksi** dengan gate yang sama seperti edit tunggal. Unit yang tertolak gate dilaporkan per baris
("A-07: sudah ada kontrak — dilewati"), bukan seluruh batch gagal diam-diam. Semua tercatat di
`unit_revisions` dengan satu `reason` untuk seluruh batch.

---

## B. Daftar kekurangan berprioritas

### P0 — integritas data / blocker operasional

| ID | Kekurangan | Usulan |
|---|---|---|
| **P0-A** | Label `saleable_area` bertentangan antar-form padahal ini basis alokasi HPP | Samakan jadi **"Luas Jual (m²)"** dengan helper text eksplisit "dipakai sebagai dasar alokasi biaya (HPP) bila proyek memakai basis luas". **Butuh keputusan klien**: luas jual = luas bangunan atau luas tanah? Sesuai aturan ketidakpastian CLAUDE.md, saya **tidak menebak** — akan ditinggal `// TODO(client): definisi luas jual` sampai dijawab. |
| **P0-B** | Tidak ada `PATCH /units/{id}` — unit tidak bisa dikoreksi sama sekali | Endpoint edit ber-gate + `unit_revisions` (§A4.1) |
| **P0-C** | `listUnitsByProject` tanpa paginasi/filter/cari | Envelope `{data, meta}` + query params (§C.2) |

### P1 — kelengkapan fungsi

| ID | Kekurangan | Usulan |
|---|---|---|
| P1-A | Card tanpa aksi apa pun | Kebab menu Edit/Duplikat/Kelola status/Hapus (§A4.5) |
| P1-B | Tidak ada DELETE unit | `DELETE /units/{id}` ber-gate referensi + fallback Arsipkan (§A4.2) |
| P1-C | Tidak ada duplicate unit | `POST /units/{id}/duplicate` (§A4.3) |
| P1-D | "Manage status" tidak terjangkau dari tab Unit | Modal memakai endpoint yang **sudah ada** — nol backend baru |
| P1-E | Tidak ada DELETE product type | `DELETE /product-types/{id}` — 409 bila ada unit memakai kodenya |
| P1-F | Tidak ada indikator pemakaian product type | `GET /product-types` sertakan `unit_count` |
| P1-G | UI menampilkan kode mentah, bukan nama katalog | DTO unit sertakan `product_name`, `product_category` |
| P1-H | Tidak ada tabel + aksi massal | `UnitTable` + bulk action bar (§A5) |
| P1-I | Header tab tanpa KPI | Strip KPI dari `meta` |

### P2 — penyempurnaan

| ID | Kekurangan | Usulan |
|---|---|---|
| P2-A | Kosakata "Unit" vs "Produk" | Opsi A §A1 |
| P2-B | Non-properti di kanban properti | Seksi terpisah "Produk & Layanan Lain" |
| P2-C | `ChargeGroupsPanel` hardcode addon | Ambil dari katalog `non_property` |
| P2-D | Tidak ada LT/LB terpisah | Kolom `land_area` + `building_area` (deskriptif; `saleable_area` **tetap** basis alokasi) |
| P2-E | Tidak ada ekspor CSV | `GET /projects/{id}/units/export.csv` |
| P2-F | Filter tidak persist | Angkat ke URL search params |
| P2-G | Preset per product type (harga/luas/type_label acuan) | Kolom default di `product_types`, mengisi form otomatis |
| P2-H | Pola kode unit kaku, tak bisa isi celah | Pola kustom + deteksi nomor bolong di Bulk Wizard |
| P2-I | `code` & `category` terkunci tanpa penjelasan | Teks penjelas + tampilkan jumlah unit yang terdampak |
| P2-J | Detail unit buntu | Aksi + navigasi prev/next unit |

---

## C. Rancangan teknis

### C.1 Migrasi (golang-migrate, MySQL — bukan AutoMigrate)

```
000061_unit_revisions.up.sql
  CREATE TABLE unit_revisions (
    id, tenant_id, unit_id, field, old_value, new_value,
    reason (NOT NULL), changed_by, created_at DATETIME(3), updated_at DATETIME(3)
  ) + INDEX (tenant_id), INDEX (tenant_id, unit_id)

000062_unit_area_split.up.sql        -- P2-D, opsional
  ALTER TABLE units
    ADD land_area     DECIMAL(20,4) NOT NULL DEFAULT '0.0000',
    ADD building_area DECIMAL(20,4) NOT NULL DEFAULT '0.0000';
  -- saleable_area TIDAK disentuh: tetap satu-satunya basis alokasi HPP.
  -- Kolom baru bersifat deskriptif/pelaporan sampai klien memutuskan definisi luas jual.

000063_product_type_defaults.up.sql  -- P2-G, opsional
  ALTER TABLE product_types
    ADD default_list_price   DECIMAL(20,4) NOT NULL DEFAULT '0.0000',
    ADD default_area         DECIMAL(20,4) NOT NULL DEFAULT '0.0000',
    ADD default_type_label   VARCHAR(30) NULL;
```

Tidak ada migrasi untuk delete product type / arsip unit — dua-duanya memakai struktur yang sudah ada.

### C.2 API

```
PATCH  /units/{id}                    body: field yang berubah + reason
                                      409 + daftar penghalang bila gate gagal
DELETE /units/{id}                    409 + daftar referensi bila tidak aman
POST   /units/{id}/duplicate          {code | code_pattern, count?, phase_id?}
GET    /projects/{id}/units           ?q=&status=&unit_type=&phase_id=&sort=&page=&per_page=
                                      → { data: [...], meta: { total, page, per_page,
                                          summary: { available, sold, stock_value, sold_value } } }
PATCH  /projects/{id}/units/bulk      aksi massal ber-gate, hasil per baris
DELETE /product-types/{id}            409 bila unit_count > 0
GET    /product-types                 tiap item + unit_count
GET    /projects/{id}/units/export.csv
```

**Kompatibilitas:** `GET /projects/{id}/units` sekarang mengembalikan array telanjang dan sudah dipakai
frontend + harness UAT. Perubahan ke envelope adalah **breaking**. Rencana: kirim envelope hanya bila ada
query param paginasi/filter; tanpa param tetap array (perilaku lama) sampai semua pembaca dipindahkan,
lalu envelope jadi default di rilis berikutnya.

### C.3 Frontend

Komponen baru:
`UnitToolbar` (KPI + cari + filter + toggle view) · `UnitTable` + `UnitBulkActionBar` ·
`UnitCardMenu` · `EditUnitModal` · `DuplicateUnitModal` · `ManageUnitStatusModal` ·
`DeleteUnitDialog` (menampilkan penghalang) · `NonPropertyProductsSection`.

Komponen diubah:
`ProyekDetailTabs` (header) · `UnitBoard` (virtualisasi, filter dari toolbar, hanya `property`) ·
`UnitCard` (struktur link/menu, nama katalog) · halaman detail unit (aksi + prev/next) ·
`ProductTypesSection` (delete, unit_count, penjelasan field terkunci) ·
`ChargeGroupsPanel` (addon dari katalog).

Semua warna lewat token design system (aturan "nol warna tailwind mentah").

### C.4 Kepatuhan invariant

| Invariant | Bagaimana rancangan ini menghormatinya |
|---|---|
| #2 uang bukan float | `list_price` tetap `domain.Money`; edit massal harga memakai `Money`, bukan persentase float — input persen dikonversi via `decimal` lalu dibulatkan deterministik |
| #3 alokasi sampai rupiah terakhir | `saleable_area` **tidak bisa** diubah setelah ada snapshot/biaya; basis alokasi tidak pernah bergeser di belakang snapshot |
| #4 HPP = biaya terakumulasi unit | Edit ber-gate `allocation_snapshots` kosong |
| #5 ledger append-only | Tidak ada hard delete unit yang punya jurnal; koreksi ber-audit di `unit_revisions`, bukan menimpa diam-diam |
| #6 isolasi tenant | Semua endpoint baru lewat repository ber-scope `tenant_id`; wajib integration test lintas-tenant |
| SSOT | KPI & agregat dihitung di backend dari satu definisi, bukan dijumlah ulang di browser |

### C.5 Test yang harus hijau sebelum fase ini dinyatakan selesai

- PATCH ditolak bila unit punya snapshot / kontrak / biaya direct (satu test per penghalang).
- DELETE ditolak bila ada referensi; diterima bila `available` & bersih.
- Duplicate menghasilkan `status = available` dan tidak menyalin `buyer_ref`/`sale_*`.
- Bulk PATCH: unit tertolak dilaporkan per baris, unit lain tetap diterapkan, satu transaksi.
- DELETE product type ditolak bila `unit_count > 0`.
- Paginasi: `Σ(len(data) tiap halaman) == meta.total`.
- Lintas-tenant: PATCH/DELETE unit tenant lain → 0 baris terpengaruh.
- Regresi: seluruh suite UAT (702 cek) tetap 0 gagal.

---

## D. Yang saya butuhkan keputusannya sebelum menulis kode

1. ~~**Opsi A atau B**~~ → **DIPUTUSKAN 2026-08-06: Opsi A.** Entity & tabel `units` tidak di-rename;
   hanya kosakata UI yang berubah menjadi "Produk". Catatan: setelah koreksi domain klien
   (lihat `audit-produk-vs-titipan-realisasi-2026-08.md`), P2-B gugur sebagian — tidak ada lagi produk
   jasa di katalog, jadi cukup **"Produk"**, bukan "Unit & Produk".
2. **Definisi "luas jual"** (P0-A): **DITANGGUHKAN atas permintaan klien 2026-08-06** — akan
   dikonfirmasi langsung ke klien karena mempengaruhi alokasi HPP. Tidak ada asumsi yang diambil.
   Sampai dijawab, P0-A tetap terbuka dan label yang bertentangan **tidak** disamakan sepihak.
3. **Cakupan gelombang pertama.** Rekomendasi: **P0-A + P0-B + P0-C + P1-A..D + P1-G** — itu memulihkan
   integritas basis alokasi, membuat unit bisa dikoreksi, dan membuat papan sanggup menampung ratusan unit.
   P1-E/F, P1-H/I dan seluruh P2 menyusul di gelombang kedua.
