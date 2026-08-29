# W-6 — Frontend Spec: Laporan Historis (Financial Snapshot)

Status: **SHIPPED & terverifikasi di UI berjalan** (2026-08-10)
Backend: `internal/histfin` · Migrasi: `000068_financial_snapshots`

---

## 1. Masalah yang diselesaikan di layar

Klien punya laporan keuangan tahun-tahun sebelum sistem ini dipakai (mis. 2021–2025).
Mereka ingin bisa membuka kembali Neraca dan Laba Rugi tahun itu di dalam sistem —
**tanpa** memasukkan histori transaksi.

Dua hal yang harus tidak pernah terjadi di layar:

1. Angka historis terlihat seolah bagian dari laporan tahun berjalan.
2. Admin merasa harus mengetik ulang jurnal lama supaya laporannya keluar.

Karena itu layarnya **terpisah** dari `/laporan`, bukan tab tambahan di sana.

---

## 2. Halaman

| Hal | Nilai |
|---|---|
| Route | `/accounting/historis` |
| Judul | Laporan Historis |
| Nav | tab baru di `AccountingNav`, tepat setelah "Periode & Tutup Buku" |
| Tipe | Server component (`page.tsx`) + client component (`HistoricalSnapshotView`) |
| Berkas | `app/(app)/accounting/historis/page.tsx`, `components/accounting/HistoricalSnapshotView.tsx`, `lib/api/histfin.ts` |

Server component membaca cookie `esa_session`, mengambil `role` dari payload JWT,
lalu memuat COA lewat `fetchAccounts`. Akun **nonaktif ikut dimuat**: akun yang
sekarang dipensiunkan bisa saja masih hidup di laporan 2023.

Penempatan tab bertetangga dengan "Saldo Awal" disengaja — keduanya menjawab
pertanyaan yang sama: *keadaan sebelum buku ini dibuka*.

---

## 3. Wireframe

```
┌─ AccountingNav ───────────────────────────────────────────────────────────┐
│ Jurnal │ Buku Besar │ … │ Periode & Tutup Buku │ [Laporan Historis] │ Pajak│
└───────────────────────────────────────────────────────────────────────────┘

Laporan Historis
Neraca dan Laba Rugi tahun-tahun sebelum sistem ini dipakai, dicatat sebagai arsip.

┌───────────────────────────────────────────────────────────────────────────┐
│ ⚠ Data historis — di luar buku besar                                      │
│ Tidak ada jurnal yang dibuat, dan angka ini tidak ikut menyusun Neraca,    │
│ Laba Rugi, dashboard, maupun tutup buku tahun berjalan.                    │
└───────────────────────────────────────────────────────────────────────────┘

( 2025 Final ) ( 2024 Draft ) ( 2023 Final )        [ + Tambah Tahun ]

Tahun Buku 2024  [Draft] [revisi 1] [Belum disimpan]   [Simpan][Finalkan][Hapus]
─────────────────────────────────────────────────────────────────────────────
 Isi Data │ Neraca │ Laba Rugi │ Riwayat
─────────────────────────────────────────────────────────────────────────────
 [ Cari akun…            ]   Isi nominal dalam arah normal akun. Nilai negatif
                             diperbolehkan (mis. akumulasi penyusutan, defisit).

 ┌ Aset ───────────────────────────────────────────────────────────────────┐
 │ 1-1100  Kas — Kas Besar                                  [ 500.000.000 ] │
 │ 1-1200  Kas — Petty Cash                                 [           0 ] │
 └─────────────────────────────────────────────────────────────────────────┘
 ┌ Kewajiban ┐ ┌ Ekuitas ┐ ┌ Pendapatan ┐ ┌ Beban ┐        (seksi per tipe)

 ┌ Catatan tahun ini ──────────────────────────────────────────────────────┐
 │ [ sesuai laporan audit KAP, ditandatangani 30 April                    ]│
 └─────────────────────────────────────────────────────────────────────────┘

┌─ strip lengket (sticky bottom) ───────────────────────────────────────────┐
│ Total Aset      Kewajiban+Ekuitas+Laba   Laba/Rugi Tahun Ini   Selisih    │
│ Rp 400.000.000  Rp 400.000.000           Rp 0                  Rp 0  [✓]  │
└───────────────────────────────────────────────────────────────────────────┘
```

Tab **Neraca** dan **Laba Rugi** menampilkan laporan jadi (dua kolom untuk
Neraca), **Riwayat** menampilkan tabel audit.

---

## 4. Alur

```
Belum ada tahun
      │  EmptyState → [Tambah Tahun Buku]
      ▼
Modal "Tambah Tahun Buku Historis"  ──(tahun sudah berjurnal)──► toast merah, modal tetap terbuka
      │ POST /snapshots
      ▼
Draft  ─── isi angka ───► "Belum disimpan"  ─── Simpan ───►  Draft tersimpan
      │                                                          │
      │                                            (seimbang & ada baris)
      │                                                          ▼
      │                                                     [Finalkan] → konfirmasi
      │                                                          ▼
      └───────────────────  Buka Kembali (owner + alasan)  ─── Final (read-only)
```

Aturan yang ditegakkan layar (server tetap penjaga terakhir):

- **Finalkan** mati selama masih ada perubahan belum disimpan, selama belum
  seimbang, dan selama belum ada satu pun angka — masing-masing dengan `title`
  yang menjelaskan sebabnya, bukan tombol mati tanpa alasan.
- **Hapus** hanya muncul untuk draft yang belum pernah difinalkan (`revision == 0`).
- **Buka Kembali** hanya untuk `owner`, wajib alasan ≥10 karakter.
- Tab Neraca/Laba Rugi menolak menampilkan angka saat ada perubahan belum
  disimpan; ia menyuruh menyimpan dulu. Laporan selalu disusun server dari data
  tersimpan — layar tidak pernah menyusun laporan sendiri.

---

## 5. State

| State | Isi | Catatan |
|---|---|---|
| `years` | `SnapshotSummary[]` | pemilih tahun, terbaru dulu |
| `selected` | `number \| null` | tahun aktif |
| `detail` | `SnapshotDetail \| null` | keadaan **tersimpan** dari server |
| `draft` | `Record<accountId, string>` | isian layar, terpisah dari `detail` |
| `dirty` | `boolean` | pembeda "sudah tersimpan" vs "belum" |
| `notes` | `string` | catatan tahun |
| `tab` | `isi \| neraca \| laba-rugi \| riwayat` | |
| `neraca` / `labaRugi` / `audits` | payload laporan | dimuat malas per tab, dibuang tiap kali data berubah |
| `busy` | `boolean` | mengunci tombol selama request |

`draft` sengaja dipisah dari `detail` supaya badge **"Belum disimpan"** selalu
jujur, dan supaya strip total bisa menandai dirinya sebagai *pratinjau layar*
saat kotor: "Angka di atas pratinjau layar. Nilai yang berlaku dihitung server
saat disimpan." Frontend tidak pernah jadi sumber angka uang.

Isian nominal memakai input sendiri, bukan `RupiahInput`: `RupiahInput` membuang
semua non-digit sehingga **tidak bisa menyatakan nilai negatif**, padahal defisit
akumulasi dan akumulasi penyusutan memang negatif. Input di sini menerima satu
tanda minus di depan dan memformat ribuan (`-100.000.000`).

Akun ikhtisar laba rugi (`computed_account_code`, kini `3-3000`) disaring keluar
dari daftar isian. Kodenya **datang dari API**, tidak dihardcode di browser.

---

## 6. Aksi → Endpoint

| Aksi layar | Endpoint | Auth |
|---|---|---|
| Muat daftar tahun | `GET /historical-financials/snapshots` | authenticated |
| Muat satu tahun | `GET /historical-financials/snapshots/{year}` | authenticated |
| Tab Neraca | `GET /historical-financials/snapshots/{year}/neraca` | authenticated |
| Tab Laba Rugi | `GET /historical-financials/snapshots/{year}/laba-rugi` | authenticated |
| Tab Riwayat | `GET /historical-financials/snapshots/{year}/audits` | authenticated |
| Tambah Tahun | `POST /historical-financials/snapshots` | owner\|accountant |
| Simpan | `PUT /historical-financials/snapshots/{year}` (replace-all) | owner\|accountant |
| Finalkan | `POST /historical-financials/snapshots/{year}/finalize` | owner\|accountant |
| Buka Kembali | `POST /historical-financials/snapshots/{year}/reopen` | **owner** |
| Hapus | `DELETE /historical-financials/snapshots/{year}` | owner\|accountant |

`PUT` mengganti **seluruh** isi tahun tersebut; layar selalu mengirim keadaan
penuh, dan baris bernilai nol tidak dikirim (server pun membuangnya).

---

## 7. Empty state

Tanpa satu pun tahun:

> **Belum ada laporan historis**
> Tambahkan tahun buku sebelum sistem dipakai, lalu isi Neraca dan Laba Ruginya
> sebagaimana dilaporkan waktu itu. Setiap tahun berdiri sendiri — angka 2024
> tidak diturunkan dari 2025.
> `[ Tambah Tahun Buku ]`

Kalimat terakhir sengaja menjawab salah paham paling mahal di fitur ini: orang
mengira snapshot adalah akumulasi berjalan.

---

## 8. Hasil verifikasi di UI berjalan

Diuji pada backend `:8085` + `next dev :3005`, tenant dev 9901005 (owner):

- Tambah 2025 → **ditolak** 409 dengan toast "tahun ini sudah punya jurnal
  terposting…" (tenant memang sudah berjurnal di 2025), modal tetap terbuka.
- Tambah 2024 → draft terbuka, editor tampil per seksi tipe akun.
- Isi 1-1100 = 500.000.000 → strip live: Selisih Rp 500.000.000, "Belum seimbang".
- Isi 3-1000 = 500.000.000 → "Seimbang (belum disimpan)".
- Isi 4-1000 = 200.000.000 dan 5-1000 = 200.000.000 → Laba/Rugi Tahun Ini Rp 0.
- Simpan → toast "tersimpan dan seimbang", badge dirty hilang.
- Tab Neraca → hanya aset/kewajiban/ekuitas, Selisih Rp 0, badge "Seimbang".
- Tab Laba Rugi → hanya pendapatan/beban, Laba/Rugi Bersih 2024 Rp 0.
- Finalkan → status Final, revisi 1, tombol Simpan/Finalkan/Hapus hilang.
- Isi Data saat Final → **read-only** (tidak ada input nominal, catatan disabled).
- Riwayat → 3 baris: Tahun dibuka, Angka disimpan, Difinalkan (revisi 1).
- Buka Kembali dengan alasan "pendek" → ditolak di layar; dengan alasan sah →
  kembali Draft, revisi 1 tetap, tombol Hapus tidak muncul lagi.
- Nilai negatif: 3-2000 = **-100.000.000** diterima dan diformat; disimpan dan
  tetap seimbang setelah aset disesuaikan.
- Finalkan pada tahun kosong → mati dengan `title` "Belum ada angka yang diisi".
- Hapus draft 2023 (belum pernah final) → terhapus, kembali ke empty state.

**Invariant utama, diukur langsung di DB setelah seluruh sesi UI di atas:**

```
journal_entries baru  : 0
journal_lines  baru   : 0
financial_snapshots   : 1
snapshot_lines        : 5
snapshot_audits       : 5
```

Snapshot historis tidak pernah menyentuh buku besar.
