# W-11 Tahap 4 — Frontend Spec: Pembayaran Tagihan Vendor

**Status:** SPEC SAJA — belum ada satu baris pun UI pembayaran yang dibangun di Tahap 4.
Tahap 4 sengaja backend-only sesuai lingkup yang dikunci pemilik. Dokumen ini adalah
kontrak yang harus dipenuhi ketika layar pembayaran dikerjakan, supaya keputusan desain
tidak ditemukan ulang (dan salah) di kemudian hari.

**Prasyarat baca:** `w11-final-decision-lock.md` (D-1..D-20), `w11-hutang-usaha-design.md`.

---

## 0. Satu kalimat

Kasir membayar tagihan vendor yang **sudah terposting**, uang keluar dari akun kas/bank,
BKK terbit otomatis, dan sisa tagihan turun — bukan karena ada kolom yang ditulis ulang,
melainkan karena alokasi pembayaran bertambah.

---

## 1. Halaman

| # | Rute | Nama | Kegunaan |
|---|------|------|----------|
| P-1 | `/accounting/pembayaran-vendor` | Daftar Pembayaran | riwayat semua pembayaran vendor + status dibalik |
| P-2 | `/accounting/pembayaran-vendor/baru` | Bayar Tagihan Vendor | form 2-langkah (Input → Pratinjau → Simpan) |
| P-3 | `/accounting/pembayaran-vendor/[id]` | Detail Pembayaran | jurnal, BKK, alokasi ke tagihan, tombol Balikkan |
| P-4 | — (panel di `/accounting/hutang/[id]`) | Riwayat Pembayaran Tagihan | tabel pembayaran yang menyentuh tagihan ini |

P-4 bukan halaman baru: ia tab/section di halaman detail tagihan yang sudah ada dari Tahap 3.

> **Catatan rute (diputuskan saat implementasi Tahap 5).** Spesifikasi awal menulis
> rute bergaya API (`/ap/payments`). Yang dibangun memakai rute berbahasa Indonesia
> di bawah `/accounting/…`, mengikuti seluruh rute aplikasi yang sudah ada
> (`/accounting/hutang`, `/accounting/jurnal`). Rute `/ap/*` tidak pernah dibuat.

---

## 2. Wireframe

### P-2 — Bayar Tagihan Vendor (langkah 1: Input)

```
┌───────────────────────────────────────────────────────────────────┐
│  Bayar Tagihan Vendor                                    [Batal]  │
├───────────────────────────────────────────────────────────────────┤
│  Vendor        [ CV Karya Bangun            ▾ ]                   │
│  Tanggal bayar [ 15/08/2026 ]                                     │
│  Sumber dana   [ 1-1300 · Bank BCA Operasional  ▾ ]  ← kas/bank   │
│  Jumlah bayar  [ Rp 40.000.000                ]                   │
│                                                                   │
│  Alokasi ke tagihan                                               │
│   (•) Otomatis — jatuh tempo paling tua dulu                      │
│   ( ) Pilih sendiri                                               │
│                                                                   │
│  ┌─ Tagihan terbuka vendor ini ──────────────────────────────┐    │
│  │ INV/2026/000012  jt 20/08  Rp 100.000.000  sisa 100.000.000│   │
│  │ INV/2026/000019  jt 05/09  Rp  30.000.000  sisa  30.000.000│   │
│  │                              Total sisa:  Rp 130.000.000   │   │
│  └───────────────────────────────────────────────────────────┘    │
│                                                                   │
│                                        [ Lihat Pratinjau → ]      │
└───────────────────────────────────────────────────────────────────┘
```

### P-2 — langkah 2: Pratinjau (belum menulis apa pun)

```
┌───────────────────────────────────────────────────────────────────┐
│  Pratinjau Pembayaran                              [← Ubah]       │
├───────────────────────────────────────────────────────────────────┤
│  Alokasi                                                          │
│   INV/2026/000012   dibayar Rp 40.000.000   sisa →  Rp 60.000.000 │
│                                                                   │
│  Jurnal yang akan terbentuk                                       │
│   D  2-1000  Hutang Usaha ............ Rp 40.000.000              │
│   K  1-1300  Bank BCA Operasional .... Rp 40.000.000              │
│                                     ✓ seimbang                    │
│                                                                   │
│  Dokumen: BKK akan terbit otomatis dengan nomor berikutnya.       │
│                                                                   │
│                        [ Batal ]   [ Simpan Pembayaran ]          │
└───────────────────────────────────────────────────────────────────┘
```

Pola dua-langkah ini sama dengan modal pembayaran customer yang sudah ada
(lihat memory `payment-allocation-fe1`). Jangan bikin pola ketiga.

### P-3 — Detail Pembayaran

```
┌───────────────────────────────────────────────────────────────────┐
│  BKK/2026/000031        CV Karya Bangun        Rp 40.000.000      │
│  15/08/2026 · Bank BCA Operasional · invoice   [ Balikkan ]       │
├───────────────────────────────────────────────────────────────────┤
│  Alokasi                                                          │
│   INV/2026/000012 ................................ Rp 40.000.000  │
│                                                                   │
│  Jurnal  JE#4471                                                  │
│   D 2-1000 Hutang Usaha       Rp 40.000.000                       │
│   K 1-1300 Bank BCA           Rp 40.000.000                       │
└───────────────────────────────────────────────────────────────────┘
```

Jika sudah dibalik, header berubah jadi badge **Dibalik** + baris
"Dibalik 16/08/2026 · jurnal pembalik JE#4488", tombol Balikkan hilang,
dan jurnal ASLI tetap ditampilkan (append-only harus terlihat, bukan
cuma benar di database).

---

## 3. Flow

```
Daftar Tagihan (Tahap 3)
        │  tagihan status=posted, sisa > 0
        ▼
   [ Bayar ]  ──► P-2 Input ──► P-2 Pratinjau ──► POST /ap/payments
                     ▲                │                    │
                     └──── Ubah ──────┘                    ▼
                                                    P-3 Detail (BKK terbit)
                                                           │
                                                    [ Balikkan ] → konfirmasi
                                                           ▼
                                              jurnal pembalik + alokasi mati
```

Aturan navigasi: setelah simpan berhasil, nomor BKK harus langsung terbaca —
kasir membutuhkannya untuk arsip fisik. Jangan kembali ke daftar.

> **Deviasi navigasi (Tahap 5, diimplementasikan).** Alih-alih redirect otomatis
> ke P-3, form menampilkan **panel hasil di tempat** yang memuat nomor BKK, nomor
> jurnal, dan rincian alokasi, dengan tautan eksplisit ke P-3. Alasannya: redirect
> otomatis merampas satu-satunya momen kasir melihat hasil posting berdampingan
> dengan angka yang baru saja ia masukkan, dan pada jawaban `replayed` (idempotensi)
> redirect membuat kasir tak pernah tahu bahwa permintaannya adalah pengulangan.
> Kebutuhan aslinya — BKK terbaca segera — tetap terpenuhi.

---

## 4. State

| State | Pemicu | Tampilan |
|-------|--------|----------|
| `idle` | halaman dibuka | form kosong, daftar tagihan belum dimuat |
| `vendor-dipilih` | vendor dipilih | muat tagihan terbuka; kalau kosong → empty state |
| `pratinjau-loading` | klik Lihat Pratinjau | tombol disabled + spinner |
| `pratinjau-siap` | 200 dari `/preview` | panel jurnal + alokasi |
| `menyimpan` | klik Simpan | tombol disabled, **Idempotency-Key sudah dibuat sebelum request pertama** |
| `tersimpan` | 201 (atau 200 `replayed`) | panel hasil di tempat: BKK, jurnal, alokasi + tautan ke P-3 |
| `gagal` | 4xx/5xx | pesan error di tempat, form TIDAK direset, key idempotensi **dipertahankan** |

**Empty state wajib informatif (aturan `feedback-erp-business-flow`):**

- vendor tanpa tagihan terposting → "Vendor ini belum punya tagihan terposting.
  Tagihan draft tidak bisa dibayar." + CTA *Lihat tagihan vendor*.
- semua tagihan lunas → "Semua tagihan vendor ini sudah lunas." + CTA *Lihat riwayat pembayaran*.
- tenant belum punya akun kas/bank → "Belum ada akun kas/bank aktif." + CTA *Buka Bagan Akun*.

**Idempotency-Key:** dibuat sekali per sesi form (UUID v4) saat pratinjau ditampilkan,
bukan per klik. Kalau kasir menekan Simpan dua kali karena jaringan lambat, request kedua
membawa key yang sama dan backend mengembalikan pembayaran yang sama (`replayed: true`) —
UI harus memperlakukan itu sebagai **sukses**, bukan error, dan tetap ke P-3.

---

## 5. Aksi

| Aksi | Hak | Konfirmasi | Catatan |
|------|-----|-----------|---------|
| Lihat pratinjau | baca | tidak | tidak menulis apa pun |
| Simpan pembayaran | tulis | **ya**, modal konfirmasi posting | kirim Idempotency-Key |
| Balikkan pembayaran | tulis | **ya**, modal + tanggal pembalikan | tidak bisa dibalik dua kali |

Modal balik harus menyebut konsekuensinya dengan kalimat manusia:
"Pembayaran ini akan dibalik dengan jurnal pembalik. Jurnal asli tetap tersimpan.
Sisa tagihan yang terkait akan naik kembali sebesar Rp X."

> **Dua deviasi (Tahap 5, diimplementasikan).**
>
> 1. **Konfirmasi posting ditambahkan.** Spesifikasi awal menganggap pratinjau
>    sudah menjadi konfirmasi. Pemilik meminta konfirmasi posting yang eksplisit,
>    jadi Simpan membuka ConfirmModal berisi jumlah, sumber dana, dan jumlah
>    tagihan yang tersentuh. Pratinjau tetap ada — ia menjawab "apa yang akan
>    dijurnal", modal menjawab "apakah benar sekarang".
> 2. **Tidak ada field alasan pada pembalikan.** Backend `reverseBody` hanya
>    menerima `reverse_date`; tidak ada kolom alasan di mana pun pada jalur ini.
>    Meminta alasan di layar lalu membuangnya sebelum request adalah form
>    bohong — jadi modal meminta **tanggal pembalikan** (boleh dikosongkan →
>    tanggal server). Bila alasan pembalikan memang ingin tersimpan, itu
>    perubahan backend + migrasi, di luar cakupan Tahap 5.

---

## 6. Endpoint

| Metode | Path | Dipakai oleh |
|--------|------|--------------|
| `GET` | `/ap/payments?vendor_id=&status=&page=` | P-1 |
| `POST` | `/ap/payments/preview` | P-2 langkah 2 |
| `POST` | `/ap/payments` (header `Idempotency-Key`) | P-2 simpan |
| `GET` | `/ap/payments/{id}` | P-3 |
| `POST` | `/ap/payments/{id}/reverse` | P-3 tombol Balikkan |
| `GET` | `/ap/invoices/{id}/payments` | P-4 |
| `GET` | `/ledger/accounts/cash-bank` | selector sumber dana (sudah ada) |

Selector sumber dana **wajib** memakai `CashBankSelect` yang sudah ada
(memory `coa-driven-cash-bank`). Dilarang menghardcode daftar bank.

### 6.1 `outstanding_before` / `outstanding_after` — hanya untuk pembayaran yang dihitung

`AllocationView` membawa `outstanding_before` dan `outstanding_after` **hanya** ketika
backend baru saja menghitung pembayarannya: jawaban `POST /preview` dan jawaban
`POST /ap/payments` yang benar-benar memposting. Pada tiga jalur lain —
`GET /ap/payments/{id}`, `GET /ap/payments` (daftar), dan jawaban **replay**
idempotensi — kedua field dikirim sebagai string kosong. Itu disengaja: backend
tidak merekonstruksi posisi historis sebuah tagihan, karena posisi itu bergerak
setiap kali ada pembayaran atau pembalikan lain sesudahnya.

Konsekuensi wajib bagi layar: **string kosong tidak boleh dirender sebagai
`Rp 0`.** P-3 tidak menampilkan kedua kolom itu sama sekali (dengan catatan kaki
yang mengarahkan ke detail tagihan), dan panel hasil P-2 mencetak `—` bila
kosong. Layar yang mencetak "Rp 0" di situ sedang mengarang angka yang tak pernah
dikirim backend — persis pelanggaran D-12.

---

## 7. Yang TIDAK ada di layar ini

Sengaja, bukan karena lupa:

- **Uang muka vendor & retensi** — backend menolaknya di Tahap 4. Jangan tampilkan
  pilihan `payment_kind` selain `invoice`; radio yang di-disable lebih membingungkan
  daripada tidak ada.
- **Kolom `paid_amount` / `outstanding` di form** — sisa selalu dibaca dari server.
  Frontend tidak boleh menghitung sisa sendiri, sekali pun untuk optimistic update:
  sumber kebenarannya sub-ledger alokasi, dan dua transaksi bisa berebut sisa yang sama.
- **Bayar sebagian per baris tagihan di mode otomatis** — mode otomatis membagi
  tertua-dulu; kalau kasir mau kontrol, ia pindah ke mode eksplisit.
