# W-2 — Frontend Spec: Penomoran Dokumen (Document Numbering Engine)

Status: **SHIPPED** · 2026-08-07
Keputusan owner yang dilayani: satu engine penomoran untuk seluruh dokumen; reset
tahunan berlaku **forward-only**; dokumen historis **tidak berubah sedikit pun**;
tahun baru mulai dari `000001`.

---

## 1. Masalah yang diselesaikan di layar

Sebelum W-2, bentuk nomor kwitansi dan invoice adalah **konstanta di dalam kode
Go**. Admin yang ingin mengubah prefix (`KWT` → `KW`), menambah bulan ke dalam
nomor, atau menerbitkan jenis dokumen baru (Bukti Kas Keluar) harus menunggu
rilis program. Setelah W-2, semua itu adalah **data**, dan layar ini adalah
tempat data itu dikelola.

Satu hal yang **tidak** boleh dilakukan dari layar mana pun: mengubah nomor
dokumen yang sudah terbit. Registry `documents` append-only, dan UI hanya
membacanya.

---

## 2. Halaman & penempatan

| Hal | Nilai |
|---|---|
| Rute | `/pengaturan` → tab **Penomoran Dokumen** |
| Komponen | `components/settings/DocumentNumberingSection.tsx` |
| API client | `frontend/lib/api/document.ts` |
| Hak akses | baca: semua user; tulis: `RequireWrite` (server-side) |

Penempatannya sengaja bersebelahan dengan **Katalog Produk** dan **Biaya
Realisasi**: ketiganya master data yang menentukan perilaku sistem tanpa
sentuhan programmer.

---

## 3. Wireframe

```
┌─ Penomoran Dokumen ──────────────────────────────────── [+ Jenis Dokumen] ─┐
│ Semua nomor dokumen — kwitansi, invoice, memo transfer — keluar dari satu  │
│ mesin. Yang membedakan hanya konfigurasi di bawah ini.                     │
│                                                                            │
│ ┌────────────────────────────────────────────────────────────────────────┐ │
│ │ Kwitansi Pembayaran Rumah         [inti]                               │ │
│ │ house_payment · KWT · reset tahunan · 6 digit                          │ │
│ │ Tahun 2026 · 15 dokumen terbit · berikutnya  ➜  KWT/2026/000016        │ │
│ │                                                   [Ubah] [Nonaktifkan] │ │
│ ├────────────────────────────────────────────────────────────────────────┤ │
│ │ Kwitansi Booking Fee              [inti]                               │ │
│ │ booking · KWB · reset tahunan · 6 digit                                │ │
│ │ Tahun 2026 · 13 dokumen terbit · berikutnya  ➜  KWB/2026/000016        │ │
│ ├────────────────────────────────────────────────────────────────────────┤ │
│ │ Bukti Kas Keluar                                                       │ │
│ │ cash_voucher · BKK · reset tahunan · 4 digit                           │ │
│ │ Tahun 2026 · 0 dokumen terbit · berikutnya  ➜  BKK-202608-0001         │ │
│ └────────────────────────────────────────────────────────────────────────┘ │
│                                                                            │
│ ▸ Riwayat perubahan (7)                                                    │
└────────────────────────────────────────────────────────────────────────────┘

┌─ Dokumen Terbit ─────────────── [semua jenis ▾] [2026 ▾] ──────────────────┐
│ KWT/2026/000015   Kwitansi Pembayaran Rumah   07 Agu 2026   Rp 25.000.000  │
│ INV/2026/000004   Invoice                     05 Agu 2026   Rp 12.500.000  │
│ MTI/2026/000002   Memo Transfer Internal      04 Agu 2026   Rp  1.500.000  │
└────────────────────────────────────────────────────────────────────────────┘
```

### Modal tambah/ubah jenis dokumen

```
┌─ Tambah Jenis Dokumen ─────────────────────────────┐
│ Kode      [ cash_voucher        ]  (permanen)      │
│ Nama      [ Bukti Kas Keluar    ]                  │
│ Prefix    [ BKK ]   Digit [ 4 ]                    │
│ Format    [ {prefix}-{year}{month}-{seq}        ]  │
│           Placeholder: {prefix} {year} {month} {seq}│
│ Reset     ( • ) Tahunan   ( ) Tidak pernah          │
│                                                     │
│ ┌ Pratinjau ────────────────────────────────────┐  │
│ │  BKK-202608-0001                              │  │
│ └───────────────────────────────────────────────┘  │
│                              [Batal] [Simpan]      │
└─────────────────────────────────────────────────────┘
```

Pratinjau dirender **di klien** dengan aturan yang sama persis dengan
`document.formatNumber` — supaya admin melihat akibat setiap ketikan sebelum
menekan Simpan. Server tetap memvalidasi ulang; klien tidak dipercaya.

---

## 4. Alur

**A. Melihat nomor berikutnya** — buka `/pengaturan` → tab Penomoran Dokumen.
`GET /documents/preview` mengembalikan, per jenis: tahun buku berjalan, nomor
terakhir terpakai, nomor berikutnya, dan jumlah dokumen terbit tahun itu.
Read-only — membuka layar ini **tidak** memakai nomor.

**B. Mengubah bentuk nomor** — Ubah → sunting prefix/format/digit → pratinjau
berubah seketika → Simpan. Berlaku untuk dokumen **berikutnya** saja.

**C. Menambah jenis dokumen** — + Jenis Dokumen → isi kode (permanen), nama,
prefix, format → Simpan. Modul mana pun kini bisa meminta nomor jenis itu tanpa
perubahan kode.

**D. Menonaktifkan** — hanya untuk jenis buatan admin. Jenis inti (`is_system`)
ditolak server: mematikannya membuat penerimaan pembayaran gagal total.

**E. Menelusuri dokumen terbit** — panel bawah, filter jenis + tahun.

---

## 5. State

| State | Sumber | Catatan |
|---|---|---|
| `types` | `GET /documents/types` | termasuk `is_system`, `is_active` |
| `preview` | `GET /documents/preview` | nomor berikutnya + jumlah terbit |
| `documents` | `GET /documents?type=&year=&limit=` | registry, append-only |
| `history` | `GET /documents/types/history?limit=` | audit perubahan konfigurasi |
| `form` | lokal | pratinjau dihitung dari `form`, bukan dari server |

**Empty state** (aturan tetap: informatif + CTA) — bila belum ada jenis dokumen
sama sekali, tampilkan peringatan bahwa **kwitansi dan invoice tidak bisa terbit**
(resolver fail-closed) beserta tombol tambah. Ini keadaan rusak, bukan keadaan
kosong yang wajar, jadi nadanya peringatan.

---

## 6. Aksi & endpoint

| Aksi UI | Endpoint | Sukses | Gagal |
|---|---|---|---|
| Muat daftar | `GET /documents/types` | render kartu | pesan galat |
| Muat pratinjau | `GET /documents/preview` | nomor berikutnya | kartu tanpa pratinjau |
| Muat registry | `GET /documents` | tabel | tabel kosong |
| Muat audit | `GET /documents/types/history` | accordion | disembunyikan |
| Tambah jenis | `POST /documents/types` | 201 → toast + reload | 409 kode ganda; 400 format tak sah |
| Ubah jenis | `PATCH /documents/types/{id}` | 200 → toast + reload | 400 (mis. menonaktifkan jenis inti) |

Kode `document_type` **tidak pernah** bisa diubah — dipakai modul lain saat
meminta nomor; mengubahnya memutus penomoran seluruh dokumen jenis itu.

---

## 7. Yang sengaja TIDAK ada di UI

- **Mengedit nomor dokumen yang sudah terbit.** Registry append-only.
- **Menyetel ulang penghitung (`last_val`).** Menurunkannya = menerbitkan nomor
  kembar. Bila benar-benar perlu, itu pekerjaan DBA dengan jejak audit, bukan
  tombol di layar admin.
- **Memilih tahun buku saat menerbitkan.** Tahun mengikuti waktu penerbitan —
  keputusan engine, bukan input pengguna.
