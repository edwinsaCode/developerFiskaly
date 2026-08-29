# W-7 — Frontend Spec: Piutang Proyek Lama (Legacy AR)

Status: **spec final, dasar implementasi**
Backend rujukan: `internal/legacyar` (handler `/api/v1/legacy-ar`), `internal/receivable`, `internal/reporting`

---

## 0. Satu kalimat yang mengatur seluruh layar ini

> **Impor piutang lama adalah pengisian RINCIAN atas saldo yang sudah ada di buku besar — bukan pencatatan piutang baru.**

Setiap keputusan UI di bawah ini turun dari kalimat itu. Kalau sebuah layar membuat admin
mengira ia sedang "menambah piutang", layar itu salah — sekalipun angkanya benar.

Konsekuensi yang harus TERBACA di layar, bukan hanya benar di database:

| Yang harus terbaca | Cara layar menyampaikannya |
|---|---|
| Impor tidak membuat jurnal | Langkah rekonsiliasi menampilkan "Jurnal yang akan dibuat: tidak ada" secara eksplisit |
| Selisih bukan error sistem | Selisih ditampilkan sebagai fakta + tiga pilihan tindakan, bukan sebagai blokade merah |
| Pelunasan legacy = kas masuk sungguhan | Modal bayar menampilkan pratinjau jurnal + janji nomor BKM |
| Piutang lama bukan laporan terpisah | Tidak ada halaman aging legacy. Ia muncul di `/accounting/receivable` sebagai sumber ketiga |

---

## 1. Peta halaman

```
/accounting/legacy-ar                  Daftar piutang proyek lama + rekonsiliasi + entri impor
/accounting/legacy-ar/import           Wizard impor 5 langkah
/accounting/legacy-ar/[id]             Detail satu piutang: riwayat bayar, audit, aksi lunasi
/accounting/receivable                 (SUDAH ADA) — bertambah satu sumber: "Proyek Lama"
```

Penempatan di navigasi: tab **"Piutang Proyek Lama"** di `AccountingNav`, bertetangga dengan
**Saldo Awal** dan **Laporan Historis**. Ketiganya menjawab pertanyaan yang sama — *"apa yang
sudah ada sebelum buku ini dibuka?"* — dan admin yang sedang mengerjakan onboarding akan
mengerjakan ketiganya berurutan dalam satu duduk.

Yang **tidak** dibuat: menu "Aging Legacy", "Laporan Piutang Lama", atau kartu piutang lama di
dashboard yang menghitung sendiri. Angka piutang lama hanya lahir dari mesin `receivable`.

---

## 2. Halaman: Daftar Piutang Proyek Lama

### 2.1 Wireframe

```
┌──────────────────────────────────────────────────────────────────────────────┐
│ Jurnal │ Buku Besar │ … │ Saldo Awal │ ▸Piutang Proyek Lama │ Laporan Historis │
├──────────────────────────────────────────────────────────────────────────────┤
│ Piutang Proyek Lama                                    [ Impor dari Excel ]  │
│ Sisa tagihan dari proyek yang berjalan sebelum sistem ini dipakai.           │
│ Saldonya sudah ada di buku besar lewat Saldo Awal — di sini rinciannya.      │
│                                                                              │
│ ┌── Kecocokan dengan Buku Besar ─────────────────────── per 31 Des 2025 ──┐  │
│ │  Saldo awal 1-2000 di buku besar        Rincian piutang lama            │  │
│ │  Rp 1.000.000.000                       Rp 1.000.000.000                │  │
│ │  ──────────────────────────────────────────────────────────────────     │  │
│ │  ✓ COCOK — selisih Rp 0                                                 │  │
│ │  Saldo 1-2000 saat ini Rp 850.000.000 (turun Rp 150.000.000 oleh        │  │
│ │  pelunasan piutang lama — bukan selisih).            [ Rincian ▾ ]      │  │
│ └────────────────────────────────────────────────────────────────────────┘  │
│                                                                              │
│ ┌ Total Piutang Lama ┐┌ Sudah Dibayar ┐┌ Sisa Tagihan ┐┌ Sudah Lunas ┐      │
│ │ Rp 1.000.000.000   ││ Rp 150.000.000││ Rp 850.000.000││ 1 dari 3    │      │
│ └────────────────────┘└───────────────┘└──────────────┘└─────────────┘      │
│                                                                              │
│ [Cari nama/rujukan…] [Proyek lama: semua ▾] [Status: semua ▾]  [Lunasi…]    │
│                                                                              │
│ Customer      Proyek Lama        Rujukan  Jatuh Tempo   Asli    Bayar  Sisa  │
│ ─────────────────────────────────────────────────────────────────────────── │
│ ☐ Budi S.     Griya Asri (2019)  REF-001  30 Jun 2025   300jt  100jt  200jt │
│ ☐ Andi W.     Griya Asri (2019)  REF-002  30 Sep 2025   250jt   50jt  200jt │
│ ☑ Citra D.    Ruko Pasar Baru    REF-003  31 Mar 2026   450jt      —  450jt │
│                                                              [ Lunasi (1) ] │
└──────────────────────────────────────────────────────────────────────────────┘
```

### 2.2 Kartu Kecocokan — aturan tampil

Kartu ini adalah kontrol akuntansi, bukan hiasan. Tiga keadaan, tiga tampilan berbeda:

| Keadaan | Warna/nada | Isi |
|---|---|---|
| `matched === true` | `success`, tenang | "✓ COCOK — selisih Rp 0" |
| `difference ≠ 0` | `warning`, **bukan** `danger` | Selisih + kalimat penjelas + tautan ke Saldo Awal |
| `ledger_available === false` | `neutral` | "Saldo buku besar belum bisa dibaca" — **tidak** menampilkan angka selisih |

Keadaan ketiga penting: menampilkan "selisih Rp 1.000.000.000" ketika akun kontrolnya belum ada
sama sekali akan membuat admin mengejar selisih yang tidak pernah ada.

Bagian **Rincian ▾** (tertutup secara default) memecah saldo buku besar apa adanya:

```
Saldo 1-2000 per 31 Des 2025            Rp 850.000.000
  dari Saldo Awal                       Rp 1.000.000.000   ← dibandingkan
  dari operasi berjalan (BAST, dsb.)    Rp 0
  dikurangi pelunasan piutang lama      (Rp 150.000.000)
Rincian piutang lama (nilai asli)       Rp 1.000.000.000
Selisih                                 Rp 0
```

Kolom "dibandingkan" itu satu-satunya yang boleh dipakai: membandingkan rincian dengan saldo
1-2000 **hidup** akan membuat rekonsiliasi rusak setiap kali ada customer membayar — dan
rekonsiliasi yang rusak setiap hari akan berhenti dibaca dalam sepekan.

### 2.3 Empty state

Wajib informatif + CTA (aturan `feedback-erp-business-flow`):

> **Belum ada piutang proyek lama**
> Kalau developer masih punya tagihan dari proyek yang selesai sebelum sistem ini dipakai,
> impor rinciannya di sini. Saldo totalnya dicatat lewat **Saldo Awal**; yang diisi di sini
> adalah siapa saja yang menyusun angka itu.
> `[ Unduh Template Excel ]  [ Impor dari Excel ]`

### 2.4 State

| State | Sumber | Catatan |
|---|---|---|
| `rows`, `summary`, `source_labels` | `GET /legacy-ar` (server component) | Semua total dari server |
| `reconciliation` | `GET /legacy-ar/reconciliation` | Dipanggil paralel dengan daftar |
| `selected: Set<number>` | client | Untuk pelunasan gabungan |
| filter `q`, `source_label`, `status` | URL search params | Bisa di-bookmark & di-share |

Baris berstatus `paid` dan `written_off` tidak bisa dicentang.

---

## 3. Wizard Impor — 5 langkah

Alur wajib (keputusan klien #12): **Unduh Template → Unggah → Pratinjau → Cocokkan dengan Buku
Besar → Konfirmasi → Hasil.**

```
 ①Berkas ──── ②Pratinjau ──── ③Cocokkan GL ──── ④Konfirmasi ──── ⑤Selesai
```

Langkah ① dan ② dikirim dalam SATU request (`POST /legacy-ar/batches` mengembalikan
`{batch, rows, reconciliation}`) supaya tidak ada keadaan antara yang bisa gagal separuh jalan.

### Langkah ① Berkas

```
┌──────────────────────────────────────────────────────────────┐
│ 1. Ambil templatnya                                          │
│    Kolom dan urutannya sudah ditentukan. Berkas dari format  │
│    lain tidak bisa dibaca.       [ ⭳ Unduh Template Excel ]  │
│                                                              │
│ 2. Tanggal posisi saldo  [ 31/12/2025 ]                      │
│    Satu tanggal untuk SELURUH berkas. Ini yang dibandingkan  │
│    dengan saldo awal di buku besar.                          │
│                                                              │
│ 3. Akun kontrol   [ 1-2000 — Piutang Customer ▾ ]            │
│    Akun yang menampung piutang ini di buku besar.            │
│                                                              │
│ 4. Berkas         [ Pilih berkas .xlsx ]                     │
│                                        [ Baca Berkas → ]     │
└──────────────────────────────────────────────────────────────┘
```

Tanggal posisi default: **31 Desember tahun lalu** — bukan hari ini. Piutang lama hampir selalu
diambil dari neraca penutup tahun sebelumnya, dan default "hari ini" akan diam-diam salah.

### Langkah ② Pratinjau

Tabel seluruh baris dengan kolom **mentah** dari Excel dan hasil bacanya berdampingan.
Baris `error` bernada `danger`, `warning` bernada `warning`, keduanya menampilkan pesan per kolom.

```
 #  Customer     Nominal (di berkas) → dibaca      Status
 1  Budi Santoso "300.000.000"       → Rp 300.000.000   ✓
 2  Andi Wijaya  "Rp 250.000.000"    → Rp 250.000.000   ✓
 3  (kosong)     "450.000.000"       → —                ✕ Nama customer wajib diisi
```

Menampilkan sel apa adanya di sebelah hasil baca bukan kemewahan: ketika klien protes
"angkanya bukan segitu", yang harus bisa dibuka adalah apa yang mereka kirim.

Tombol **Lanjut** dinonaktifkan bila `error_count > 0`, dengan alasan tertulis:
*"Perbaiki N baris bermasalah di berkas lalu unggah ulang. Impor sebagian tidak diizinkan —
sebagian masuk sebagian tidak adalah keadaan terburuk yang mungkin."*

### Langkah ③ Cocokkan dengan Buku Besar — **langkah WAJIB**

```
┌──────────────────────────────────────────────────────────────────────┐
│ Cocokkan dengan Buku Besar                                           │
│                                                                      │
│   Saldo awal 1-2000 di buku besar (per 31 Des 2025)  Rp 800.000.000  │
│   Rincian yang akan diimpor                          Rp 1.000.000.000│
│   ────────────────────────────────────────────────────────────────   │
│   Selisih                                          (Rp 200.000.000)  │
│                                                                      │
│   ⚠ Rinciannya lebih besar Rp 200.000.000 daripada yang tercatat di  │
│     buku besar. Sistem TIDAK akan membuat jurnal untuk menutup       │
│     selisih ini sendiri.                                             │
│                                                                      │
│   Pilih satu:                                                        │
│   ◯ Perbaiki dulu — batalkan impor, betulkan Saldo Awal / berkasnya  │
│   ◯ Buat jurnal saldo awal untuk selisihnya                          │
│       Akun lawan [ pilih dari daftar akun ▾ ]   ← wajib dipilih user │
│       Jurnal: Dr 1-2000 200jt / Cr <pilihan Anda> 200jt              │
│   ◯ Lanjutkan apa adanya, selisih dicatat                            │
│       Alasan [__________________________________]  ← wajib diisi     │
│                                            [ ← Kembali ] [ Lanjut → ]│
└──────────────────────────────────────────────────────────────────────┘
```

Aturan yang tidak boleh dilanggar layar ini:

1. Tidak ada opsi default yang terpilih. Ini keputusan akuntansi; menebaknya untuk admin adalah
   cara paling halus untuk merusak neraca.
2. Dropdown akun lawan memuat **seluruh COA aktif**, tanpa nilai awal, tanpa rekomendasi
   `3-9000`. Akun mana yang benar adalah keputusan akuntan klien.
3. Bila `matched === true`, langkah ini tetap DITAMPILKAN (sebagai konfirmasi hijau) — bukan
   dilewati. Kontrol yang hanya muncul saat ada masalah tidak pernah dipercaya.

### Langkah ④ Konfirmasi

Ringkasan sekali baca + kalimat yang paling penting di seluruh fitur ini:

```
   Akan diimpor          3 piutang senilai Rp 1.000.000.000
   Akun kontrol          1-2000 Piutang Customer
   Tanggal posisi        31 Desember 2025
   Jurnal yang dibuat    TIDAK ADA — saldo piutang ini sudah tercatat
                         di buku besar lewat Saldo Awal. Impor ini
                         hanya mengisi rinciannya.
                                          [ ← Kembali ] [ Impor Sekarang ]
```

(Bila user memilih opsi ke-2 di langkah ③, baris terakhir berubah menjadi jurnal saldo awal yang
akan dibuat, lengkap dengan akun lawan pilihannya.)

### Langkah ⑤ Selesai

Hasil + rekonsiliasi setelah impor + tautan ke daftar. Bila selisih ditutup dengan jurnal,
sertakan tautan ke jurnalnya.

---

## 4. Halaman Detail Piutang

```
┌──────────────────────────────────────────────────────────────────────┐
│ ← Piutang Proyek Lama                                                │
│ Budi Santoso                                             [ Lunasi ]  │
│ Perumahan Griya Asri (2019) · REF-001 · 0811000001                   │
│                                                                      │
│ ┌ Nilai Asli ──┐┌ Sudah Dibayar ┐┌ Sisa Tagihan ┐┌ Status ────────┐ │
│ │ Rp 300jt     ││ Rp 100jt      ││ Rp 200jt     ││ Belum Lunas    │ │
│ └──────────────┘└───────────────┘└──────────────┘└────────────────┘ │
│                                                                      │
│ Riwayat Pembayaran                                                   │
│ 15 Jan 2026  Rp 100.000.000  BKM/2026/000012  ke 1-1100  [Batalkan] │
│                                                                      │
│ Jejak Audit                                                          │
│ 10 Agu 2026  Diimpor      Rp 300.000.000  sisa Rp 300.000.000       │
│ 15 Jan 2026  Dibayar      Rp 100.000.000  sisa Rp 200.000.000       │
│                                                                      │
│ Asal data: berkas piutang-lama.xlsx, batch #4, posisi 31 Des 2025    │
└──────────────────────────────────────────────────────────────────────┘
```

Riwayat pembayaran menampilkan baris pembatalan sebagai baris tersendiri bernilai negatif —
tidak menghapus baris aslinya (Invariant #5, append-only).

---

## 5. Modal Pelunasan

Satu komponen dipakai dari dua tempat: tombol massal di daftar (beberapa piutang) dan tombol di
detail (satu piutang).

```
┌── Lunasi Piutang Proyek Lama ───────────────────────────────┐
│ Tanggal bayar      [ 10/08/2026 ]                           │
│ Rekening tujuan    [ 1-1100 Kas ▾ ]      ← CashBankSelect   │
│                                                             │
│ Alokasi — tentukan berapa untuk piutang yang mana:          │
│   Budi Santoso   sisa Rp 200.000.000  bayar [ 100.000.000 ] │
│   Andi Wijaya    sisa Rp 200.000.000  bayar [  50.000.000 ] │
│                                       ──────────────────────│
│                                       Total  Rp 150.000.000 │
│                                                             │
│ Jurnal yang akan dibuat:                                    │
│   Dr 1-1100 Kas              Rp 150.000.000                 │
│     Cr 1-2000 Piutang Customer   Rp 150.000.000             │
│   Bukti Kas Masuk (BKM) terbit otomatis dengan nomor urut.  │
│                                                             │
│ Catatan [_______________________]                           │
│                              [ Batal ]  [ Terima Pembayaran ]│
└─────────────────────────────────────────────────────────────┘
```

Aturan:

- **Alokasi selalu eksplisit per piutang.** Tidak ada mode "bayar sekian ke total customer" —
  pengurangan buta atas total customer adalah persis cara alokasi tertukar diam-diam.
- Nominal > sisa langsung ditolak **di layar** dengan pesan konkret
  (*"Melebihi sisa tagihan Rp 200.000.000"*), dan tetap ditolak lagi oleh server
  (`ErrOverpayment` → 422). Validasi layar itu kenyamanan; yang mengikat tetap server.
- `idempotency_key` di-generate sekali saat modal dibuka (`crypto.randomUUID()`) dan **dipakai
  ulang pada percobaan kedua**. Inilah yang membuat klik ganda/retry jaringan tidak menghasilkan
  dua penerimaan kas.
- Tombol submit dikunci selama request berjalan; hasil menampilkan **nomor BKM** yang terbit.

---

## 6. Perubahan pada layar yang sudah ada

### 6.1 `/accounting/receivable` (ARAgingView)

Piutang lama masuk sebagai sumber ketiga pada mesin yang sama. Perubahannya minimal — itu
memang buktinya bahwa abstraksi W-4 benar:

| Titik | Perubahan |
|---|---|
| `ReceivableSource` | `"house" \| "realization" \| "legacy"` |
| `sourceLabel` | `+ legacy: "Proyek Lama"` |
| `filterTabs` | `+ { value: "legacy", label: "Proyek Lama" }` |
| kolom Unit | `row.unit_code || "—"` — piutang lama memang tidak punya unit |
| kolom Aksi | `legacy` → tautan ke `/accounting/legacy-ar/{ref_id}` |
| badge sumber | `legacy` → `variant="accent"` |
| `parseSource()` di page | menerima `"legacy"` |
| empty state | cabang khusus `legacy`: "belum ada piutang proyek lama" + CTA impor |

Yang **tidak** berubah: perhitungan bucket, subtotal, collection rate, total. Semuanya tetap
lahir di server dari satu mesin.

### 6.2 `AccountingNav`

Satu entri baru setelah "Saldo Awal".

---

## 7. Kontrak endpoint

| Aksi | Endpoint | Catatan |
|---|---|---|
| Unduh template | `GET /legacy-ar/template` | xlsx; unduh lewat proxy same-origin |
| Unggah + pratinjau | `POST /legacy-ar/batches` (multipart) | `file`, `as_of_date`, `control_account_code`, `notes` → `{batch, rows, reconciliation}` |
| Pratinjau ulang | `GET /legacy-ar/batches/{id}` | untuk kembali ke wizard yang belum di-commit |
| Impor | `POST /legacy-ar/batches/{id}/commit` | `{skip_reason?, opening_counter_account_code?, opening_description?}` |
| Batalkan draft | `POST /legacy-ar/batches/{id}/discard` | |
| Daftar | `GET /legacy-ar?status=&q=&source_label=&only_open=` | |
| Detail | `GET /legacy-ar/{id}` | |
| Rekonsiliasi | `GET /legacy-ar/reconciliation?as_of=&control_account_code=` | |
| Terima pembayaran | `POST /legacy-ar/payments` | `{allocations[], cash_account_code, payment_date, notes, idempotency_key}` |
| Batalkan pembayaran | `POST /legacy-ar/payments/{id}/void` | `{reason}` — wajib |

Pemetaan status → perlakuan layar:

| Status | Arti | Layar |
|---|---|---|
| 409 | keadaan sudah begitu (batch ganda, sudah lunas, sudah dibatalkan) | pesan + **jangan** tawarkan ulangi |
| 422 | yang dikirim tidak sah (lebih bayar, ada baris error, akun salah) | pesan di dekat field terkait, boleh diperbaiki lalu ulangi |
| 404 | tidak ditemukan | kembali ke daftar |

---

## 8. Aturan desain

Mengikuti `design-system-warm-ledger`:

- **Nol warna Tailwind mentah.** Semua lewat token: `text-text-primary`, `text-text-secondary`,
  `text-text-tertiary`, `bg-surface`, `border-border`, `text-accent`, `text-success`,
  `text-warning`, `text-danger`.
- Semua nominal lewat `<Rupiah>`, semua tanggal lewat `<Tanggal>`. Tidak ada `toLocaleString`
  yang ditulis tangan.
- Angka rata kanan + `tabular-nums`.
- Aksi tulis dibungkus `<Can>`/`RequireWrite` sesuai peran, seragam dengan layar akuntansi lain.

## 9. Yang sengaja TIDAK dibangun

| Tidak dibangun | Alasan |
|---|---|
| Halaman aging legacy terpisah | INV-AR-1 — satu mesin, satu daftar. Sumber adalah dimensi, bukan alasan bikin laporan kedua |
| Tombol "Hapus Buku" / write-off | BD-2 — perlakuan akuntansinya belum diputuskan akuntan klien. Status domainnya ada, jalurnya tidak |
| Edit nominal piutang | `original_amount` immutable; koreksi = batalkan batch & impor ulang |
| Terima lebih bayar legacy | Ditolak (keputusan #8). Saldo kredit legacy tidak punya rumah akuntansi |
| Master customer legacy terpisah | Identitas melekat di baris piutangnya; tautan ke master `customers` opsional |
