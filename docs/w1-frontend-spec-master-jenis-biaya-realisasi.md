# W-1 Frontend Spec — Master Jenis Biaya Realisasi

Status: **spec + implementasi** (2026-08-06). Backend W-1a/W-1b sudah SHIPPED
(migration 000061 + 000062, suite integration 21 paket hijau).

Keputusan bisnis yang dilayani halaman ini:

> "Admin harus bisa mengelola master ini sendiri (CRUD) tanpa programmer."
> Titipan Biaya Realisasi = Notaris, BPHTB, PDAM, Listrik — **semuanya titipan,
> tidak pernah pendapatan.** "Tidak ada Kavling", dan PDAM **bukan produk**.

Konsekuensi UI-nya cuma satu kalimat: **nama jenis biaya tidak boleh lagi
diketik bebas.** Selama ia free-text, tidak ada yang menjamin uang customer
mendarat di akun titipan yang benar — dan salah akun titipan berarti salah
neraca, bukan sekadar salah label.

---

## 1. Halaman & penempatan

| # | Halaman | Rute | Komponen | Peran |
|---|---------|------|----------|-------|
| A | Pengaturan → tab **Biaya Realisasi** | `/pengaturan` | `components/settings/RealizationChargeTypesSection.tsx` (baru) | admin master CRUD |
| B | Detail Unit → tab Tagihan Terpisah | `/proyek/[id]` → unit → panel | `components/penjualan/ChargeGroupsPanel.tsx` (diubah) | konsumen master |

Tab **Biaya Realisasi** diletakkan tepat setelah tab **Produk** — dua master itu
adalah pasangan yang saling menjelaskan: Produk = yang **dijual** (pendapatan),
Biaya Realisasi = yang **dititipkan** (kewajiban). Admin yang bingung "PDAM
taruh di mana?" menemukan jawabannya karena keduanya bersebelahan.

---

## 2. Wireframe

### A. Pengaturan → Biaya Realisasi

```
┌───────────────────────────────────────────────────────────────────────────┐
│ Jenis Biaya Realisasi                                    [ + Jenis Biaya ] │
│ Biaya yang ditagih ke customer atas nama pihak ketiga. Uangnya TITIPAN     │
│ (kewajiban) — tidak pernah menjadi pendapatan perusahaan.                  │
├───────────────────────────────────────────────────────────────────────────┤
│ Notaris (notaris)                                       [Ubah] [Nonaktif]  │
│ Titipan → 2-2400 · Titipan Biaya Realisasi                                 │
│ ─────────────────────────────────────────────────────────────────────────  │
│ BPHTB (bphtb)                                           [Ubah] [Nonaktif]  │
│ Titipan → 2-2400 · Titipan Biaya Realisasi                                 │
│ ─────────────────────────────────────────────────────────────────────────  │
│ PDAM (pdam)                                             [Ubah] [Nonaktif]  │
│ Listrik (listrik)                                       [Ubah] [Nonaktif]  │
│ IPL (ipl)                          [nonaktif]           [Ubah] [Aktifkan]  │
├───────────────────────────────────────────────────────────────────────────┤
│ ▸ Riwayat perubahan (audit)                                                │
│   06 Agu 2026 · listrik · is_active: true → false · oleh Admin #3          │
│   06 Agu 2026 · ipl · created → 2-2300 · oleh Admin #3                     │
└───────────────────────────────────────────────────────────────────────────┘
```

Modal tambah/ubah:

```
┌── Tambah Jenis Biaya Realisasi ───────────────────────────┐
│ Kode *            [ ipl                    ]              │
│   huruf kecil, tanpa spasi. Permanen setelah dipakai.     │
│ Nama *            [ Iuran Pengelolaan Ling ]              │
│ Akun Titipan *    [ 2-2400 — Titipan Biaya Realisasi  ▾ ] │
│   Hanya akun KEWAJIBAN. Ke sinilah uang customer          │
│   mendarat saat dibayar, dan dari sinilah ia keluar saat  │
│   perusahaan membayar vendor.                             │
│                                                            │
│ ⚠ Semua jenis biaya realisasi bersifat TITIPAN            │
│   (deposit_liability). Tidak ada opsi "pendapatan" —      │
│   itu keputusan bisnis yang dikunci, bukan setelan.        │
│                                       [Batal]  [Simpan]   │
└────────────────────────────────────────────────────────────┘
```

### B. Modal Buat Grup Tagihan (yang berubah)

```
Jenis  [ Biaya Realisasi (titipan) ▾ ]   Nama Grup [ Biaya Realisasi ]

⚠ Semua pembayaran grup ini dicatat sebagai Titipan Realisasi —
  bukan pendapatan, dan tidak mengurangi harga rumah.

Item Tagihan
  Jenis Biaya            Jumlah         Jatuh Tempo (ops.)
  [ Notaris        ▾ ]   [ 4.000.000 ]  [ 2026-09-01 ]      ✕
  [ BPHTB          ▾ ]   [ 2.000.000 ]  [          ]        ✕
  [ — pilih —      ▾ ]   [           ]  [          ]        ✕
  + Tambah baris

  Semua item di atas mengendap di akun 2-2400. Satu grup hanya boleh
  memakai SATU akun titipan — refund & transfer bergerak di level grup.
```

Untuk `kind = addon` kolom pertama **tetap** `Input` teks bebas: produk tambahan
bukan titipan dan tidak punya master jenis biaya.

---

## 3. Flow

**F-1 Admin menambah jenis biaya baru**
1. Pengaturan → Biaya Realisasi → `+ Jenis Biaya`
2. Isi kode + nama, pilih akun kewajiban → Simpan
3. `POST /charges/types` → 201 → daftar dimuat ulang → toast sukses
4. Jenis itu **langsung** muncul di dropdown Buat Grup Tagihan (dimuat saat modal dibuka)

**F-2 Admin menonaktifkan jenis biaya**
1. Klik `Nonaktifkan` → `PATCH /charges/types/{id}` `{is_active:false}`
2. Baris tetap tampil dengan badge `nonaktif` — **tidak dihapus**, karena item
   tagihan lama menunjuk kode ini
3. Jenis nonaktif hilang dari dropdown grup baru, tapi grup lama tetap terbaca

**F-3 Admin membuat grup realisasi**
1. Modal terbuka → `GET /charges/types` (aktif saja yang dipilihkan)
2. Setiap baris item memilih jenis biaya dari dropdown
3. Submit → backend `assertSingleDepositAccount` memvalidasi ulang

**F-4 Admin menambah item ke grup realisasi yang sudah ada**
1. Dropdown **hanya menawarkan** jenis biaya yang akun titipannya sama dengan
   akun grup itu (diturunkan dari item existing)
2. Kalau grup adalah baris historis tanpa `charge_type_code`, semua jenis aktif
   ditawarkan dan backend yang memutuskan

**F-5 Master kosong (fail-closed terlihat sebelum submit)**
Kalau `GET /charges/types` kosong, modal Buat Grup tidak menampilkan form item
untuk `kind=realization`; ia menampilkan CTA ke Pengaturan. Lebih baik user
diarahkan daripada menabrak 422 setelah mengetik lima baris.

---

## 4. State

`RealizationChargeTypesSection`
| state | tipe | asal |
|---|---|---|
| `rows` | `RealizationChargeType[]` | `GET /charges/types` |
| `history` | `MasterDataChange[]` | `GET /charges/types/history?limit=20` |
| `liabilityAccounts` | `Account[]` | `GET /ledger/accounts` difilter `type==="liability" && is_active` |
| `loading` / `busy` | boolean | — |
| `modalOpen`, `editing` | boolean, `RealizationChargeType \| null` | — |
| `code`, `name`, `account` | string | form |
| `historyOpen` | boolean | disclosure audit |

`CreateGroupModal` / `AddItemsModal`
| state | tipe | catatan |
|---|---|---|
| `chargeTypes` | `RealizationChargeType[]` | difilter `is_active` |
| `items[].charge_type_code` | string | menggantikan `items[].label` untuk realization |

Untuk realization, `label` **tidak lagi diketik user**: frontend mengirimnya
sebagai salinan `Name` jenis biaya terpilih. Label tetap disimpan per item —
bukan sekadar di-join dari master saat baca — supaya nama pada tagihan lama
tidak ikut berubah ketika master di-rename kemudian. Kode (`charge_type_code`)
adalah kunci akuntansinya; label adalah snapshot tampilannya.

---

## 5. Aksi & validasi klien

| Aksi | Guard klien | Error server yang dipetakan |
|---|---|---|
| Simpan jenis baru | kode & nama non-kosong, akun terpilih | `ErrChargeTypeExists` → "Kode sudah dipakai" |
| Ubah jenis | nama non-kosong | `ErrDepositAccountInvalid` → pesan server ditampilkan apa adanya |
| Buat grup realisasi | tiap baris terisi wajib punya `charge_type_code` + jumlah > 0 | `ErrChargeTypeRequired`, `ErrDepositAccountMixed` |
| Tambah item | dropdown sudah dipersempit ke akun titipan grup | `ErrDepositAccountMixed` (jaring pengaman) |

Pesan error server ditampilkan **apa adanya** — pesan Go-nya sudah ditulis
dalam bahasa Indonesia yang menjelaskan tindakan perbaikan, dan menerjemahkan
ulang di frontend hanya menciptakan sumber kebenaran kedua.

---

## 6. Endpoint

| Method | Path | Dipakai oleh |
|---|---|---|
| GET | `/charges/types` | Section A, CreateGroupModal, AddItemsModal |
| POST | `/charges/types` | Section A (create) |
| PATCH | `/charges/types/{typeID}` | Section A (ubah / aktif-nonaktif) |
| GET | `/charges/types/history?limit=` | Section A (audit TD-8) |
| GET | `/ledger/accounts` | Section A (dropdown akun kewajiban) |
| POST | `/charges/groups` | CreateGroupModal (`items[].charge_type_code`) |
| POST | `/charges/groups/{id}/items` | AddItemsModal (`items[].charge_type_code`) |

Semua endpoint tulis berada di balik `auth.RequireWrite()`.
