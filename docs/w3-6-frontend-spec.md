# W-3.6 — Spec Frontend: Bukti Kas (INV-DOC-1)

Status: **spec + implementasi**, 2026-08-08
Prasyarat: W-3.0 … W-3.5 SHIPPED (lihat `docs/w3-cash-path-inventory.md` §A–§K)

---

## 1. Masalah yang diselesaikan layar ini

Sesudah W-3.5, backend menolak memposting jurnal kas tanpa dokumen bernomor.
Tanpa W-3.6, penolakan itu sampai ke pengguna sebagai **error 400 di tengah
alur** — akuntan menekan "Posting Jurnal", gagal, dan tidak tahu apa yang
diminta sistem. Aturan yang benar dengan penyampaian yang buruk tetap terasa
seperti bug.

Tiga hal yang harus terlihat di layar:

| Pertanyaan pengguna | Jawaban W-3.6 |
|---|---|
| "Bukti apa yang akan terbit kalau saya posting ini?" | Panel bukti di detail jurnal, **sebelum** tombol posting ditekan |
| "Nomor bukti transaksi ini berapa?" | Kolom **Bukti** di daftar jurnal + baris di detail jurnal |
| "Apakah semua kas saya ada buktinya?" | Halaman **Buku Dokumen** + kartu kesehatan INV-DOC-1 |

Bukan tujuan W-3.6: mengubah aturan bisnis apa pun. Nol keputusan bisnis baru.

---

## 2. Peta halaman

```
/accounting/jurnal              Daftar jurnal      → + kolom "Bukti"
/accounting/jurnal/[id]         Detail jurnal      → + panel bukti & alur posting
/accounting/dokumen             BARU — Buku Dokumen (register + audit INV-DOC-1)
/pengaturan                     (tetap) master penomoran, W-2
```

`AccountingNav` bertambah satu tab: **Dokumen**, di antara "Jurnal Berulang" dan
"Periode & Tutup Buku" — dokumen adalah register harian, bukan setelan.

Pemisahan yang disengaja: **`/pengaturan`** menjawab "bagaimana nomor dibentuk"
(master, jarang disentuh); **`/accounting/dokumen`** menjawab "nomor apa saja
yang sudah terbit" (register, dibaca tiap hari). Menggabungkannya memaksa
akuntan masuk ke layar setelan untuk pekerjaan harian.

---

## 3. Layar 1 — Detail jurnal: panel bukti

### 3.1 Wireframe — draft yang menyentuh kas

```
┌────────────────────────────────────────────────────────────────┐
│  Jurnal #1042                                        [ Draft ] │
│  Pembayaran listrik kantor Agustus                             │
│  Tanggal 2026-08-08 · Sumber manual                            │
├────────────────────────────────────────────────────────────────┤
│  … baris jurnal …                                              │
├────────────────────────────────────────────────────────────────┤
│  ⓘ  Jurnal ini menggerakkan kas                                │
│      Setiap pergerakan kas wajib punya bukti bernomor.         │
│      Nomornya terbit saat posting — bukan sekarang.            │
│                                                                │
│      Jenis bukti                                               │
│      ( • ) BKK — Bukti Kas Keluar                              │
│            uang perusahaan yang keluar                         │
│      (   ) BTP — Bukti Titipan Pihak Ketiga                    │
│            meneruskan titipan customer (notaris, PDAM, listrik)│
│      (   ) RFC — Refund Customer                               │
│            mengembalikan uang customer                         │
│                                                                │
│                                    [ Posting Jurnal ]          │
└────────────────────────────────────────────────────────────────┘
```

### 3.2 Wireframe — jurnal yang sudah diposting

```
│  📄 Bukti  BKK/2026/000031                                     │
│     Bukti Kas Keluar · terbit 8 Agu 2026                       │
```

### 3.3 Wireframe — jurnal non-kas (draft)

Panel bukti **tidak ditampilkan sama sekali**. Tombol posting seperti semula.
Menampilkan "jurnal ini tidak butuh bukti" pada setiap jurnal akrual hanya
menambah kebisingan pada layar yang paling sering dibuka.

### 3.4 Alur

```
buka detail jurnal (draft)
        │
        ├─ GET /journals/{id}/document-choices
        │
        ├─ touches_cash = false ─────────► panel disembunyikan; posting biasa
        │
        ├─ touches_cash = true, required = false
        │       └─ panel menampilkan satu jenis (default) sebagai informasi
        │          tanpa pilihan; posting mengirim default itu
        │
        └─ touches_cash = true, required = true
                └─ radio wajib dipilih (default terpilih di awal);
                   [Posting Jurnal] mengirim `document_type`
                        │
                        ├─ 200 → panel berubah jadi baris "📄 Bukti <nomor>"
                        └─ 400 → pesan server + `choices` dari server
                                 menggantikan daftar di layar
```

### 3.5 State

| State | Asal | Catatan |
|---|---|---|
| `choices` | `GET /journals/{id}/document-choices` | dimuat sekali saat jurnal draft terbuka |
| `docType` | pilihan pengguna | diinisialisasi dari `choices.default` |
| `loadingChoices` | lokal | panel menampilkan skeleton, tombol posting non-aktif |
| `error` | respons posting | daftar `choices` dari body 400 menimpa state `choices` |

Tombol posting **dinonaktifkan** selama `loadingChoices`. Membiarkannya aktif
berarti mengizinkan posting kas sebelum sistem tahu bukti apa yang diperlukan —
tepat kegagalan yang ingin dihindari layar ini.

### 3.6 Aksi & endpoint

| Aksi | Endpoint |
|---|---|
| Muat pilihan bukti | `GET /journals/{id}/document-choices` |
| Posting | `POST /journals/{id}/post` body `{ "document_type": "BKK" }` |
| Baca nomor bukti | `GET /journals/{id}` → `document_number`, `document_type_code` |

---

## 4. Layar 2 — Daftar jurnal: kolom Bukti

Satu kolom baru di antara **Referensi** dan **Debit**:

```
Tanggal     Deskripsi                  Referensi   Bukti              Debit
2026-08-08  Pembayaran listrik kantor  —           BKK/2026/000031    1.250.000
2026-08-08  Akrual komisi Agustus      KOM-08      —                    950.000
```

`—` untuk jurnal non-kas: sebuah tanda hubung menyampaikan "tidak berlaku"
tanpa perlu satu kalimat penjelasan di setiap baris.

Nomor bukti dikirim server (`document_number` pada ringkasan jurnal) lewat LEFT
JOIN — bukan N+1 fetch per baris.

---

## 5. Layar 3 — `/accounting/dokumen` (Buku Dokumen)

### 5.1 Wireframe

```
Buku Dokumen
Setiap nomor bukti yang pernah terbit, dan jurnal yang dibuktikannya

┌─ Kesehatan bukti kas (INV-DOC-1) ─────────────────────────────┐
│  ✓ Utuh                                                        │
│  75 jurnal kas terposting · 74 berdokumen · 1 saldo awal       │
└────────────────────────────────────────────────────────────────┘

[ Semua jenis ▾ ]  [ 2026 ▾ ]                       75 dokumen

Nomor              Jenis                  Tanggal      Nilai        Jurnal
KWT/2026/000012    Kwitansi Termin        8 Agu 2026   45.000.000   #1039 →
BKK/2026/000031    Bukti Kas Keluar       8 Agu 2026    1.250.000   #1042 →
KWD/2026/000003    Kwitansi Pencairan     7 Agu 2026  430.000.000   #1031 →
```

### 5.2 Kartu kesehatan — tiga keadaan

| Keadaan | Tampilan |
|---|---|
| `clean = true` | hijau, "✓ Utuh", tiga angka cakupan |
| `violations > 0` | merah, judul tiap pemeriksaan + jumlah + daftar temuan |
| `cash_posted = 0` | netral, "Belum ada pergerakan kas yang diposting" |

Keadaan ketiga penting: audit "bersih" pada buku kosong bukan kabar baik, dan
menampilkannya sebagai hijau akan menyesatkan.

### 5.3 Empty state (aturan proyek: informatif + CTA)

```
        Belum ada dokumen terbit

  Nomor bukti terbit sendiri saat transaksi kas
  diposting — kwitansi termin, booking, kas keluar.
  Tidak ada yang perlu dibuat manual di sini.

  [ Buka Jurnal ]   [ Atur format penomoran ]
```

### 5.4 State & endpoint

| State | Endpoint |
|---|---|
| `types` (isi filter) | `GET /documents/types` |
| `rows` | `GET /documents?type=&year=&limit=200` |
| `audit` | `GET /documents/audit?limit=20` |

Filter jenis & tahun berubah → hanya `rows` dimuat ulang; kartu audit selalu
seluruh tenant, tidak mengikuti filter. Audit yang ikut terfilter akan terbaca
"bersih" hanya karena penggunanya sedang menyaring.

---

## 6. Perubahan backend yang diperlukan spec ini

Ketiganya read-only, tidak menyentuh aturan posting:

1. `GET /journals/{id}` → tambah `document_number`, `document_type_code`.
2. `GET /journals` (ringkasan) → tambah `document_number` via LEFT JOIN.
3. `QueryService.DocumentOf` / kolom join — dibaca lewat raw query ke
   `documents` karena `ledger` tidak boleh import `document` (arah
   ketergantungan satu arah, logical FK — sama seperti `document_id` sendiri).

---

## 7. Yang sengaja TIDAK dibuat

- **Halaman "terbitkan dokumen manual".** Nomor lahir dari transaksi; tombol
  penerbit nomor lepas justru menghasilkan larangan INV-DOC-1 nomor dua
  (dokumen tanpa transaksi).
- **Edit/hapus dokumen.** Dokumen mengikuti sifat jurnalnya: append-only.
  Koreksi lewat jurnal pembalik, yang menerbitkan JR sendiri.
- **Cetak/PDF bukti.** Di luar cakupan W-3; kwitansi cetak sudah punya jalurnya
  sendiri di modul billing.
