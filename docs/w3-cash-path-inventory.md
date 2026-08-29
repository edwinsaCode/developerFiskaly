# W-3 — Inventaris Seluruh Jalur Kas & Pemetaan Document Type

Status: **GATE sebelum implementasi.** Dokumen ini adalah hasil audit yang diminta
owner ("lakukan audit seluruh jalur cash-in dan cash-out … Setelah audit selesai,
tampilkan daftar seluruh cash-in/cash-out path dan document type yang akan
digunakan masing-masing sebelum implementasi").

Referensi: `docs/w3-review-inv-doc-1.md` (review arsitektur), keputusan owner
D-W3-1..D-W3-7 (final).

---

## Prinsip final (owner)

> **Setiap pergerakan uang masuk maupun uang keluar wajib memiliki satu
> dokumen/kwitansi bernomor yang dapat ditelusuri ke jurnal accounting.**

Bentuk formal INV-DOC-1:

```
∀ je ∈ journal_entries WHERE je.posted_at IS NOT NULL
    ∧ ∃ jl ∈ je.lines : AccountInRole(jl.account, RoleCashBank)
    ∧ je.source <> 'opening_balance'                       -- D-W3-3
  ⟹ je.document_id IS NOT NULL
```

Definisi "menyentuh kas" **COA-driven**: `accounts.category IN ('cash','bank')`
via `ledger.AccountInRole(a, RoleCashBank)` — bukan daftar kode hardcoded.

---

## Metodologi audit

1. Statis: seluruh 23 call site `ledger.CreateJournalRequest` / `postJournalTx` /
   `postJournal` ditelusuri, tiap baris jurnal diklasifikasi debit/kredit kas.
2. Dinamis: sweep DB atas semua `journal_entries.posted_at IS NOT NULL` yang
   punya ≥1 baris ke akun `category IN ('cash','bank')`, dikelompokkan per
   `source` dan arah. Hasil: **76 jurnal kas** (54 masuk / 22 keluar).
3. Silang: setiap jurnal kas hasil sweep dipetakan balik ke jalur kode. Tidak
   ditemukan jurnal kas yang tidak punya jalur kode yang teridentifikasi.

---

## A. CASH IN — debit akun Cash/Bank

| # | Jalur | Kode | Atomik? | Dokumen hari ini | Document type W-3 |
|---|---|---|---|---|---|
| I-1 | Termin / DP / cicilan / pelunasan customer | `sale/repository.go:576` `CommitPayment` | ✅ 1 tx | KWT | **KWT** — ✅ W-3.2 (fail-open ditutup + `LinkJournalBySource`) |
| I-2 | Booking fee | `sale/booking_repo.go:58` `CreateBookingAtomic` | ✅ 1 tx | KWB | **KWB** — ✅ W-3.2 (fail-open ditutup + tautan) |
| I-3 | Pencairan KPR dari bank | `CommitPayment`, `PaymentSource=kpr_disbursement` | ✅ 1 tx | KWD | **KWD** — ✅ W-3.2 (forward-only; 8 KWT historis tidak disentuh) |
| I-4 | Pembayaran biaya realisasi / titipan customer | `charge/service.go:495` `RecordPayment` | ✅ 1 tx | KWR | **KWR** — ✅ W-3.2 (generator wajib + tautan) |
| I-5 | Penerimaan titipan notaris | `notary/notary.go` `ReceiveDeposit` | — | — | **jalur DITUTUP** (410); hanya backfill data lama |
| I-6 | Saldo awal | `ledger/handler.go:385` `source=opening_balance` | ✅ draft→post | — | **DIKECUALIKAN** (D-W3-3) |
| I-7 | Jurnal manual yang mendebit kas | `ledger/handler.go:385` `source=manual` | ✅ `PostDraft` 1 tx | — | **BKM** — ✅ W-3.2 (`/document-choices` + `document_type`) |
| I-8 | Recurring journal yang mendebit kas | `ledger/recurring.go:180` | ✅ `CreateAndPost` | — | **BKM** — ✅ W-3.2 (`DefaultCashSpec`) |

## B. CASH OUT — kredit akun Cash/Bank

| # | Jalur | Kode | Atomik? | Dokumen hari ini | Document type W-3 |
|---|---|---|---|---|---|
| O-1 | Biaya proyek dibayar kas (land / hard / soft / financing / overhead) | `cost/service.go:346,380` | ✅ `TxRunner` | — | **BKK** — ✅ W-3.2 (`PostJournal(cashOut)` → `PostDraft`) |
| O-2 | Setoran pajak | `tax/service.go:245` `PayTax` | ✅ `InTx` | — | **BKK** — ✅ W-3.2 (akrual TIDAK berdokumen) |
| O-3 | Pembayaran komisi sales | `commission/service.go:257` `Pay` | ✅ 1 tx | — | **BKK** — ✅ W-3.2 (`postCashJournal`) |
| O-4 | Payout titipan realisasi ke pihak ketiga (PDAM/PLN/BPHTB/IMB/Sertifikat) | `charge/service.go:642` `RecordPayout` | ✅ 1 tx | — | **BTP** — ✅ W-3.2 |
| O-5 | Payout titipan notaris | `notary/notary.go:210` `Payout` | ✅ 1 tx | — | **BTP** — ✅ W-3.2 |
| O-6 | Refund sisa titipan realisasi ke customer | `charge/service.go:773` `Refund` | ✅ 1 tx | — | **RFC** — ✅ W-3.2 |
| O-7 | Refund pembatalan kontrak | `cancellation/refund_service.go:115` `PayRefund` | ✅ 1 tx | — | **RFC** — ✅ W-3.2 |
| O-8 | Void pembayaran realisasi (kas dikembalikan) | `charge/service.go:1182` `VoidPayment` | ✅ 1 tx | — | **JR** + ref KWR asal — ✅ W-3.2 (`ReversesDocumentID`) |
| O-9 | Jurnal manual yang mengkredit kas | `ledger/handler.go:385` | ✅ `PostDraft` 1 tx | — | **BKK / BTP / RFC** — ✅ W-3.2 (operator wajib memilih) |
| O-10 | Recurring journal yang mengkredit kas | `ledger/recurring.go:180` | ✅ `CreateAndPost` | — | **BKK** — ✅ W-3.2 |
| O-11 | Reversal jurnal kas apa pun | `ledger/posting_service.go:185` `Reverse` (HTTP `handler.go:531`, `cancellation:598/624`, `commission:323`) | ✅ 1 tx | — | **JR** + ref dokumen asal — ✅ W-3.2 (otomatis di dalam `Reverse`, bukan parameter pemanggil) |

## C. Jalur ber-jurnal yang TIDAK menyentuh kas — di luar INV-DOC-1

Diverifikasi baris per baris, bukan diasumsikan:

| Jalur | Kode | Isi jurnal |
|---|---|---|
| BAST: pengakuan pendapatan + HPP + PPh | `sale` Event 3/4/5 | 1-2000 / 4-x / 5-x / 2-4000 |
| Akad KPR (reklas piutang) | `sale/scheme_flow.go` | 1-2200 ↔ 1-2000 |
| Booking convert / forfeit | `sale/booking_repo.go:198,320,437` | 2-2100 → 2-2000 / 4-2000 |
| Akrual komisi | `commission/service.go` accrual | 5-x / 2-x |
| Clawback komisi | `commission/service.go:351` | 1-2100 / 5-x |
| Settlement pembatalan | `cancellation/service.go:521-538` | Uang Muka / Pendapatan Lain / **Hutang Refund** |
| Transfer titipan antar grup | `charge/service.go:1013` | titipan → titipan (punya memo **MTI**) |
| Transfer titipan → harga rumah | `sale/deposit_transfer.go:80` | titipan → uang muka (punya memo **MTI**) |
| Tutup periode | `ledger/closing.go` | akun nominal → laba ditahan |
| True-up HPP | `closing/service.go:495` | 1-3xxx / 5-1000 |

---

## D. Katalog document type sesudah W-3

| Prefix | Kode tipe | Arah | Kepemilikan ekonomis dana | Status |
|---|---|---|---|---|
| KWT | `house_payment` | masuk | uang perusahaan (harga unit) | ada (W-2) |
| KWB | `booking` | masuk | uang perusahaan (Pendapatan Booking) | ada (W-2) |
| KWR | `realization` | masuk | titipan pihak ketiga (liability) | ada (W-2) |
| **KWD** | `kpr_disbursement` | masuk | uang perusahaan, **pembayar = BANK** | **SHIPPED** (W-3.1, migrasi 000066) |
| **BKM** | `cash_in` | masuk | penerimaan lain / manual | **SHIPPED** (W-3.1, migrasi 000066) |
| **BKK** | `cash_out` | keluar | uang perusahaan | **SHIPPED** (W-3.1, migrasi 000066) |
| **BTP** | `third_party_payout` | keluar | uang titipan pihak ketiga | **SHIPPED** (W-3.1, migrasi 000066) |
| **RFC** | `customer_refund` | keluar | uang customer | **SHIPPED** (W-3.1, migrasi 000066) |
| **JR** | `journal_reversal` | dua arah | mengikuti dokumen asal | **SHIPPED** (W-3.1, migrasi 000066) |
| MTI | `internal_transfer` | — | bukan kas | ada (W-2) |
| INV | `invoice` | — | bukan kas | ada (W-2) |

**Catatan BKM.** D-W3-6 mengunci tiga seri kas KELUAR. Untuk kas MASUK, empat
seri yang ada (KWT/KWB/KWR/KWD) semuanya terikat customer atau bank; jurnal
manual/recurring yang mendebit kas — mis. setoran modal pemilik, penerimaan
bunga bank, pencairan pinjaman — tidak punya tipe yang cocok. Tanpa BKM,
D-W3-4 ("manual yang menyentuh kas WAJIB pilih tipe") tidak bisa dipenuhi untuk
kasus tersebut kecuali dengan memaksakan kwitansi customer pada penerimaan yang
bukan dari customer. BKM adalah pasangan simetris BKK dengan logika pemisahan
yang sama (economic ownership: uang perusahaan, bukan titipan/customer).
Owner menyetujui BKM pada 2026-08-07; katalog kas final = KWT/KWB/KWR/KWD/BKM
masuk dan BKK/BTP/RFC/JR keluar.

**Catatan penamaan kode.** Kode yang benar-benar dikirim di migrasi 000066 dan
`document/seed.go` adalah `cash_in`/`cash_out`/`third_party_payout` — bukan
`cash_receipt`/`cash_payment`/`deposit_payout` yang sempat tertulis di draf tabel
ini. Tabel di atas sudah diselaraskan ke kode yang hidup di database; nama draf
tidak pernah ada di kode mana pun.

---

## E. Koreksi atas premis "booking belum menghasilkan kwitansi"

Diverifikasi terhadap kode dan data, bukan diterima apa adanya:

- `sale/booking_service.go:138` menyetel `GenerateReceipt: true`, dan data dev
  berisi **14 receipt bertipe `booking`** untuk 14 termin `booking_fee`.
  Jadi booking **sudah** menerbitkan KWB lewat Document Numbering Engine W-2 —
  bukan jalur kwitansi terpisah. Instruksi "jangan membuat jalur kwitansi khusus
  di luar Document Domain" otomatis terpenuhi.
- Yang **benar-benar rusak**, dan inilah yang W-3 perbaiki:
  1. Kwitansi diterbitkan di balik **fail-open**
     (`booking_repo.go:~95`: `if p.GenerateReceipt && r.receiptTx != nil`).
     Kalau flag false atau seam nil, jurnal kas tetap terposting **tanpa**
     dokumen. Ini kelas lubang yang sama persis dengan yang hendak ditutup
     INV-DOC-1.
  2. **Tidak ada tautan ke jurnal.** `journal_entries` belum punya
     `document_id`; `documents` belum punya `journal_entry_id`. Dokumen tidak
     dapat ditelusuri ke accounting — syarat "terhubung ke journal" gagal.

Perbaikan W-3: hapus percabangan opsional pada jalur kas, terbitkan dokumen di
dalam transaksi yang sama, dan simpan `journal_entries.document_id`.

## F. Anomali data lama yang harus diputuskan saat backfill

| Anomali | Jumlah (dev) | Perlakuan |
|---|---|---|
| Termin `kpr_disbursement` bernomor KWT | 8 | **Dibiarkan** — dokumen historis immutable (aturan W-2). KWD forward-only. |
| Jurnal kas posted tanpa dokumen sama sekali | 17 | Backfill dokumen retro sesuai tabel A/B |
| Jurnal kas dari `cost_entries` tanpa dokumen | 7 | Backfill sebagai **BKK** |
| Jurnal kas sudah punya receipt/memo | 52 | Tinggal isi `document_id` |
| **Total backfill** | **~24 dokumen baru + 52 tautan** | |

### F.1 Hasil W-3.3 — SHIPPED 2026-08-07

`cmd/backfill-cash-documents` (default DRY-RUN, `--apply` untuk menulis).
Dijalankan pada dev: **76 jurnal kas tanpa dokumen → 31 ditautkan ke kwitansi
yang sudah ada, 44 dokumen retro terbit, 1 dikecualikan, 0 ditandai.**
Jalan kedua kali menemukan 0 pekerjaan — idempoten.

Angka berbeda dari perkiraan tabel di atas karena data dev berubah sejak audit
dan dua tenant sisa integration test ikut terpindai; perbandingan yang berlaku
adalah hasil verifikasi akhir, bukan perkiraannya.

Tiga keputusan yang diambil saat implementasi:

1. **Saldo awal DIKECUALIKAN dari INV-DOC-1.** Jurnal `source='opening_balance'`
   mendebit kas, tetapi yang ditetapkan adalah POSISI kas, bukan PERGERAKAN kas
   — tidak ada uang berpindah pada tanggal itu. Menerbitkan BKM untuknya berarti
   mengarang penerimaan yang tidak pernah terjadi. Enforcement W-3.5 wajib
   memakai pengecualian yang sama, kalau tidak saldo awal tenant baru akan
   ditolak.
2. **Dokumen retro bertanggal terbit HARI BACKFILL, bukan tanggal jurnalnya.**
   Mengikuti aturan W-2 yang sudah terkunci (`fiscalYearFor`): seri tahun yang
   sudah ditutup tidak boleh disusupi nomor baru. Tanggal transaksi tetap
   terbaca dari jurnal yang tertaut.
3. **Sisi kas MASUK selalu BKM.** Empat seri kas masuk lainnya
   (KWT/KWB/KWR/KWD) semuanya lahir dari baris bisnis dan sudah tertangani jalur
   "tautkan". Menebak KWT dari `source` untuk jurnal yang kwitansinya memang
   tidak ada hanya akan menciptakan kwitansi customer yang tidak pernah
   diserahkan ke siapa pun.

Verifikasi akhir terhadap seluruh dev DB — keempatnya nol:

| Cek | Hasil |
|---|---|
| jurnal kas terposting tanpa dokumen (non-saldo-awal) | 0 |
| `document_id` menunjuk dokumen yang tidak ada | 0 |
| satu dokumen dipakai dua jurnal | 0 |
| dokumen kas tanpa jurnal | 0 |

---

## G. Blocker atomisitas — WAJIB selesai sebelum numbering (W-3.0)

Perintah owner: *"Jangan menambahkan numbering/document requirement di atas
transaksi yang masih bisa menghasilkan partial posting."*

| ID | Masalah | Lokasi |
|---|---|---|
| B-1 | Daftar kode bank hardcoded `{1-1300,1-1400,1-1500}` — melanggar COA-driven cash/bank | `tax/model.go:270` `IsValidBankCode` |
| B-2 | `PayTax`: 4 operasi (create → post → update obligation → save payment) tanpa transaksi | `tax/service.go:245-315` |
| B-3 | `CreateCostEntry` / `PostCostEntry`: jurnal & entry tanpa transaksi, post terpisah | `cost/service.go:346,380` |
| B-4 | `createJournal` HTTP tidak sadar kas sama sekali | `ledger/handler.go:385-429` |
| B-5 | `PostingService.Reverse`: `Create` + `MarkPosted` di luar transaksi | `ledger/posting_service.go:185-193` |
| B-6 | `recurring.go`: `Create` + `Post` di luar transaksi | `ledger/recurring.go:180-215` |

B-1..B-3 dan B-5..B-6 masuk W-3.0. B-4 selesai bersama enforcement (D-W3-4).

---

## H. Urutan implementasi (sesuai perintah owner)

1. **W-3.0** — perbaiki B-1, B-2, B-3, B-5, B-6 + test atomisitas.
2. **W-3.1** — migrasi: `journal_entries.document_id`, `documents.reverses_document_id`, seed KWD/BKM/BKK/BTP/RFC/JR.
3. **W-3.2** — sambungkan tiap jalur kas (tabel A & B) ke `document.Issue` di dalam transaksi yang sama.
4. **W-3.3** — backfill (§F).
5. **W-3.4** — audit mode: endpoint + laporan jurnal kas tanpa dokumen.
6. **W-3.5** — fail-closed: `DocumentChecker` seam di `PostingService` (pola `WithPeriodChecker`) + `EQ_INV_DOC_1`.
7. **W-3.6** — frontend spec + implementasi.

---

## I. Temuan W-3.2 — deadlock penomoran di bawah beban bersamaan

Menyambungkan seluruh jalur kas membuat `document.Allocate` dipanggil oleh
hampir setiap transaksi kas, dan itu memunculkan cacat yang sebelumnya tidak
pernah terpicu karena penomoran hanya dipakai kwitansi.

`Allocate` dulu menaikkan seri dengan satu statement:

```sql
INSERT INTO document_sequences (...)
SELECT ?, ?, ?, COALESCE((SELECT MAX(d.sequence_no) FROM documents d WHERE ...), 0) + 1, ...
ON DUPLICATE KEY UPDATE last_val = last_val + 1
```

`INSERT ... SELECT` membaca tabel sumber dengan **shared next-key lock**, bukan
consistent read. Dua transaksi kas bersamaan pada tenant + jenis dokumen yang
sama menghasilkan siklus:

| Langkah | Transaksi A | Transaksi B |
|---|---|---|
| 1 | gap-S di `documents` → sisip baris seri (X) | |
| 2 | | gap-S di `documents` (kompatibel) → duplikat → **menunggu** X milik A |
| 3 | `INSERT INTO documents` → butuh insert-intention di gap milik B → **menunggu B** | |

→ `Error 1213 Deadlock`. Terdeteksi lewat `TestW30_RunDue_Bersamaan_TidakGandaPosting`,
tetapi bukan masalah jurnal berulang: dua kasir yang menerima uang pada detik
yang sama memicu hal yang persis sama.

**Perbaikan:** lantai seri dibaca lebih dulu sebagai `SELECT` biasa (tidak
mengunci apa pun), lalu upsert memakai nilai literal — statement penguncinya
menyentuh **satu baris** `document_sequences` saja, sehingga urutan kunci antar
transaksi tidak mungkin terbalik. Yang kalah cukup menunggu sampai pemenang
commit, dan tidak ada nomor yang terpakai oleh transaksi yang batal.

---

## J. Hasil W-3.4 — audit mode, SHIPPED 2026-08-07

Dua permukaan, satu mesin (`document.AuditCashDocuments`, read-only sepenuhnya):

| Permukaan | Untuk siapa |
|---|---|
| `GET /documents/audit?limit=` | tenant yang sedang login; tanpa `RequireWrite` — auditor perlu membacanya tanpa hak tulis |
| `cmd/audit-cash-documents [--tenant] [--limit]` | ops; menyapu seluruh tenant, **exit code 1** bila ada pelanggaran sehingga bisa dipakai sebagai gerbang rilis |

Lima larangan pada acceptance criterion owner dipetakan satu-satu:

| Kode | Larangan | Cara mendeteksi |
|---|---|---|
| `cash_journal_without_document` | jurnal kas terposting tanpa dokumen | jurnal kas `document_id IS NULL`, kecuali `opening_balance` |
| `document_without_transaction` | dokumen bernomor tanpa transaksi yang berhasil | dokumen jenis kas yang tidak dipegang jurnal terposting mana pun (alasan spesifik: yatim / menunjuk jurnal hantu / jurnal belum terposting) |
| `cash_movement_two_documents` | satu pergerakan kas, dua dokumen | jurnal memegang dokumen bisnis **dan** ada dokumen lain yang lahir dari jurnal itu — lolos dari kedua unique index |
| `document_two_cash_movements` | satu dokumen, dua pergerakan kas | `GROUP BY document_id HAVING COUNT(*) > 1` |
| `sequence_behind` | penomoran rusak | counter seri tertinggal di belakang nomor tertinggi yang terbit |

Ringkasan laporan menyebut `cash_journals_posted / documented / exempt` supaya
angka pelanggaran selalu terbaca terhadap ukuran bukunya.

**Koreksi rancangan saat menjalankannya pada data nyata.** Versi pertama `L-5`
menghitung **lubang** di seri (`last_val` > jumlah dokumen terbit) dan langsung
melaporkan 5 "pelanggaran" di tenant 7/10/11 — semuanya palsu. Dua sebab, dan
keduanya membatalkan premis ceknya:

1. Penomoran pra-W-2 memakai **satu counter bersama** untuk KWT dan KWB. Saat
   diregistrasi ke Document Engine dengan nomor aslinya dipertahankan, setiap
   seri memang terbaca berlubang. Itu riwayat, bukan kerusakan.
2. Nomor yang dialokasikan transaksi gagal **tidak** meninggalkan lubang:
   `last_val` ikut rollback karena `Allocate` wajib berjalan di transaksi
   pemanggil. Jadi cek lubang tidak punya true positive sama sekali.

Larangan "nomor terbakar oleh transaksi yang gagal" karena itu ditegakkan oleh
`document_without_transaction`, dan `L-5` diganti menjadi arah sebaliknya yang
justru berbahaya: counter **tertinggal** di belakang dokumen terbit, artinya
penerbitan berikutnya mengarah ke nomor yang sudah dipakai. `Allocate`
menyembuhkannya sendiri lewat pembacaan lantai seri, tetapi kondisinya hanya
bisa lahir dari penulisan langsung ke database dan harus terlihat.

`limit` memotong daftar temuan, **tidak** memotong `count` — laporan yang
diam-diam terpotong terbaca lebih sehat daripada bukunya.

Status dev DB sesudah W-3.3: **✓ INV-DOC-1 utuh di seluruh tenant** (6 tenant,
71 jurnal kas terposting, semuanya berdokumen). W-3.5 boleh dinyalakan.

Bukti: `internal/document/audit_integration_test.go` — A-1..A-9, setiap
larangan ditanam sebagai kerusakan nyata lalu dibuktikan tertangkap.

---

## §K. Hasil W-3.5 — penegakan fail-closed, SHIPPED 2026-08-08

INV-DOC-1 berhenti menjadi laporan dan menjadi **syarat posting**: jurnal yang
menggerakkan kas tidak bisa selesai diposting tanpa dokumen bernomor tertaut.

### K.1 Penyimpangan yang disengaja dari rencana §H

Rencana menyebut *"`DocumentChecker` seam di `PostingService` (pola
`WithPeriodChecker`)"*. Pola itu **tidak dipakai**, dan alasannya bagian dari
keputusan ini.

`WithPeriodChecker` opt-in: 20+ call site memasangnya sendiri-sendiri, dan
sebagiannya memang tidak memasangnya. Untuk guard periode itu masih bisa
diterima. Untuk INV-DOC-1 tidak: seam opt-in berarti setiap jalur kas baru harus
**ingat** memasangnya, dan satu yang lupa sudah cukup melubangi invariant —
persis lubang yang W-3 ada untuk menutupnya.

Karena itu pemeriksaan dipasang **tanpa syarat** di dalam `PostingService`
sendiri (`enforceDocument`), di ketiga pintu posting: `Post`, `PostDraft`,
`reverseWithin`. Tidak ada cara mematikannya dari luar.

Satu-satunya jalan keluar: `s.documents() == nil`, yaitu store in-memory pada
unit test. Wiring produksi selalu GORM, jadi jalur itu tidak pernah fail-open di
produksi — dan itu ditulis di komentar fungsinya supaya tidak berubah diam-diam.

### K.2 Pengecualian saldo awal

`source = "opening_balance"` mendebit kas tetapi menetapkan **posisi**, bukan
**pergerakan** — tidak ada uang yang berpindah, tidak ada bukti yang bisa
dilampirkan. Dikecualikan di backfill (W-3.3), di audit (W-3.4), dan sekarang di
penegakan (W-3.5). Tanpa pengecualian ini tenant baru tidak bisa dibuka sama
sekali.

### K.3 Perubahan urutan di jalur yang dokumennya lahir dari baris bisnis

Tiga jalur dulu **memposting dulu, menerbitkan dokumen belakangan**, karena
dokumennya (KWT/KWB) butuh kwitansi, yang butuh termin, yang butuh id jurnal.
Urutan itu mustahil dipertahankan setelah `Post` menuntut dokumen, dan hook
commit-time tidak ada di GORM/MySQL. Urutannya dibalik menjadi:

    draft journal → baris bisnis → dokumen → tautan → post

| Jalur | Berkas | Dokumen |
|---|---|---|
| Pelunasan termin | `internal/sale/repository.go` | KWT |
| Booking fee | `internal/sale/booking_repo.go` | KWB |
| Biaya realisasi | `internal/charge/service.go` + `repository.go` | KWR |

Seluruhnya tetap satu transaksi — yang berubah hanya urutan di dalamnya.

### K.4 Dua cacat yang baru terlihat setelah penegakan dinyalakan

1. **`Post` menolak sesudah menandai posted.** `Post` dulu bukan transaksi:
   `MarkPosted` sudah commit, lalu `enforceDocument` menolak. Penolakannya
   justru **melahirkan** hal yang dilarang — jurnal kas terposting tanpa
   dokumen. `Post` kini dibungkus `inTx` seperti `PostDraft`.
2. **Fixture consistency lebih longgar dari produksi.** `eqSetup` membangun
   `PostingService` tanpa `WithJournalTx`, jadi rollback tidak pernah terjadi
   dan cacat (1) tidak terlihat. Fixture disamakan dengan `main.go`
   (`WithPeriodChecker` + `WithJournalTx`). Seeder `cmd/demo-seed` disamakan
   juga.

Keduanya hanya muncul karena EQ_INV_DOC_1 menguji **sisi negatif**. Audit yang
hanya dijalankan pada buku sehat tidak akan pernah menemukannya.

### K.5 Bukti

`internal/reporting/invdoc1_integration_test.go` — `TestIntegration_EQ_INV_DOC_1`
menjalankan jalur produksi (booking KWB → kontrak KPR → DP KWT → akad →
pencairan KWD → titipan warisan BKM → kas keluar manual BKK → pembalik JR →
saldo awal dikecualikan) lalu membuktikan:

- audit W-3.4 bersih, dan `berdokumen + dikecualikan == kas terposting`;
- setiap tautan dua arah utuh, tidak ada dokumen dipakai dua jurnal;
- tidak ada dokumen kas bernomor yang tidak dipegang jurnal terposting;
- kas tanpa dokumen **ditolak** dengan `ErrDocumentRequired` — lewat `PostDraft`
  maupun `Post` telanjang — dan penolakannya **tidak meninggalkan jejak**:
  jurnal tetap draft, jumlah dokumen tidak bertambah, `last_val` tidak bergerak;
- buku tetap bersih sesudah percobaan yang ditolak.

Suite penuh (`go test -tags integration ./...`): hijau. CLI audit terhadap dev
DB: ✓ INV-DOC-1 utuh di 8 tenant (75 jurnal kas terposting).

### K.6 B-4 — status

Blocker B-4 (`POST /journals/{id}/post` tidak sadar kas) sudah tertutup di W-3.2:
endpoint memuat draft, menghitung `EntryCashSpec`, dan menolak 400 + daftar
`choices` bila jenis dokumen wajib tetapi tidak dipilih. `GET
/journals/{id}/document-choices` memberi frontend jawaban yang sama sebelum alur
posting dimulai. `createJournal` sendiri hanya membuat **draft** — kas belum
bergerak di sana, jadi ia memang tidak perlu tahu soal dokumen.

---

## §L. Hasil W-3.6 — bukti kas di layar, SHIPPED 2026-08-08

Spec lengkap (peta halaman, wireframe, state, endpoint, dan daftar "sengaja
tidak dibuat") ada di `docs/w3-6-frontend-spec.md`. Bagian ini hanya mencatat
hasil dan keputusan yang lahir saat implementasi.

### L.1 Mengapa W-3.6 bukan pekerjaan kosmetik

Sesudah W-3.5 backend **menolak** memposting jurnal kas tanpa dokumen. Tanpa
W-3.6, penolakan itu sampai ke akuntan sebagai error 400 di tengah alur:
tombol ditekan, gagal, dan tidak ada petunjuk apa yang diminta. Aturan yang
benar dengan penyampaian yang buruk tetap terasa seperti bug — dan yang akan
dilaporkan ke kita adalah "posting jurnal rusak", bukan "saya lupa memilih
jenis bukti".

### L.2 Yang dibangun

| Layar | Perubahan |
|---|---|
| `/accounting/jurnal` | kolom **Bukti** (nomor atau `—`), dari `document_number` di ringkasan — LEFT JOIN, bukan N+1 fetch |
| `/accounting/jurnal/[id]` | panel bukti: pilihan jenis sebelum posting (draft kas), baris `Bukti <nomor>` sesudah posting, dan **tidak muncul sama sekali** untuk jurnal non-kas |
| `/accounting/dokumen` | BARU — Buku Dokumen: kartu kesehatan INV-DOC-1 + register nomor, tiap baris menaut balik ke jurnalnya |

Tiga tambahan backend, semuanya read-only dan tidak menyentuh aturan posting:
`GET /journals/{id}` kini membawa `document_number`/`document_type_code`/
`document_type_name`/`document_issued_at`; `GET /journals` membawa
`document_number`; `QueryService.DocumentOf` membacanya lewat raw SQL karena
`ledger` tidak boleh import `document` (arah ketergantungan satu arah — sama
alasannya dengan `document_id` yang logical FK).

### L.3 Keputusan tampilan yang punya alasan, bukan selera

- **Panel disembunyikan untuk jurnal non-kas.** Menempelkan "jurnal ini tidak
  butuh bukti" pada setiap jurnal akrual hanya menambah kebisingan di layar
  yang paling sering dibuka.
- **Teks pilihan menjelaskan kepemilikan ekonomis**, bukan nama dokumen:
  "uang perusahaan yang keluar" (BKK) vs "meneruskan titipan customer" (BTP)
  vs "mengembalikan uang customer" (RFC). Itulah pertanyaan yang sebenarnya
  sedang dijawab operator; nama resminya tetap datang dari master di server.
- **Tombol posting nonaktif selama pilihan belum dimuat.** Membiarkannya aktif
  berarti mengizinkan posting kas sebelum sistem tahu bukti apa yang
  diperlukan — persis kegagalan yang ingin dihindari layar ini.
- **Daftar `choices` dari respons 400 menimpa daftar di layar.** Server yang
  paling tahu apa yang sah; layar tidak boleh keras kepala.
- **Kartu audit tidak mengikuti filter jenis/tahun.** Audit yang ikut terfilter
  akan terbaca "bersih" hanya karena penggunanya sedang menyaring.
- **Keadaan ketiga kartu audit.** `clean = true` pada buku yang belum punya
  pergerakan kas ditampilkan **netral**, bukan hijau — "bersih" pada buku
  kosong bukan kabar baik.
- **Dokumen ditaruh di nav akuntansi, bukan di `/pengaturan`.** `/pengaturan`
  menjawab "bagaimana nomor dibentuk" (master, jarang disentuh);
  `/accounting/dokumen` menjawab "nomor apa saja yang sudah terbit" (register,
  dibaca tiap hari). Menggabungkannya memaksa akuntan masuk ke layar setelan
  untuk pekerjaan harian.

### L.4 Bukti — verifikasi end-to-end di browser

Tenant bersih (`w36@dev.test`), dijalankan lewat UI sungguhan, bukan hanya
`tsc`:

1. Buku kosong → kartu audit **netral** ("belum ada pergerakan kas yang
   diposting"), register menampilkan empty state + dua CTA.
2. Draft kas (Dr beban / Cr bank) → panel menampilkan tiga pilihan dengan
   kalimat kepemilikan ekonomis; posting default → `BKK/2026/000001`.
3. Draft kas kedua dengan BTP dipilih di layar → `BTP/2026/000001`, panel
   berubah menjadi `Bukti BTP/2026/000001 · Bukti Pembayaran Titipan · terbit
   8 Agu 2026`.
4. Draft **non-kas** → panel tidak muncul sama sekali, posting berjalan biasa,
   tidak ada nomor yang terpakai.
5. Kartu audit sesudahnya: **✓ Bukti kas utuh** — 2 jurnal kas terposting, 2
   berdokumen, 0 saldo awal; register menaut ke Jurnal #5535 dan #5536.
6. `POST /journals/{id}/post` tanpa `document_type` pada jurnal kas → 400 +
   daftar `choices`, jurnal tetap draft.

`npx tsc --noEmit`: bersih. `go build ./...`, `go vet ./...`: bersih. Suite
integrasi penuh (`go test -tags integration ./...`): hijau.

### L.5 Yang sengaja tidak dibuat

Penerbitan dokumen manual, edit/hapus dokumen, dan cetak PDF. Alasannya di
`docs/w3-6-frontend-spec.md` §7 — inti: tombol penerbit nomor lepas justru
melahirkan larangan INV-DOC-1 nomor dua (dokumen tanpa transaksi).

---

## §M · Coverage audit final — W-3 FULLY CLOSED (2026-08-09)

Audit penutup atas permintaan owner: **bukan** hanya BKK dan BTP, melainkan
seluruh jalur kas terhadap aturan

> Setiap cash movement yang diposting, cash-in maupun cash-out, wajib memiliki
> tepat satu Document bernomor yang dapat ditelusuri ke journal.

Audit ini **read-only** — tidak ada kode yang diubah untuk menutupnya. Yang
diperiksa: sumber (service/fungsi), jurnal, jenis dokumen, penomoran, tautan,
atomisitas, perilaku saat penerbitan dokumen gagal, dan nasib nomor saat rollback.

### M.1 Tiga pola, bukan sebelas jalur terpisah

Setiap jalur kas jatuh ke salah satu dari tiga pola. Ini penting: menambah jalur
kas baru berarti memilih pola, bukan menulis penanganan dokumen dari nol.

**Pola A — dokumen lahir dari baris bisnis** (KWT, KWB, KWR, KWD).
`draft journal → baris bisnis → kwitansi (Allocate+Register) → LinkJournalBySource
→ PostDraft(spec kosong)`. Nomor dialokasikan `document.Allocate`; registry dicatat
`document.Register` dengan `source = receipts#id`. Jenis kwitansi **diturunkan**
dari `termin_payments.payment_source` di `createReceiptForTerminInTx` — satu SoT,
bukan parameter terpisah yang bisa menyimpang.

**Pola B — dokumen lahir dari jurnalnya sendiri** (BKM, BKK, BTP, RFC).
`CreateAndPost(Document: spec)` atau `PostDraft(spec)` → `applyDocument` →
`document.IssueForJournal` (Allocate + Register + LinkJournal) dengan
`source = journal_entries#id`. Jenisnya datang dari **jalur bisnis**, bukan dari
arah debit/kredit: BTP dan RFC terlihat identik di jurnal, yang membedakan adalah
uang siapa yang bergerak.

**Pola C — pembalik** (JR). `Reverse` → `reversalDocument` otomatis: kalau jurnal
asal menyentuh kas, pembaliknya mendapat nomor JR sendiri yang menunjuk
`reverses_document_id` ke dokumen asli. Tanpa parameter — satu pemanggil yang
lupa meminta sudah cukup untuk melubangi invariant.

### M.2 Cash In — enam jalur

| # | Sumber | Jurnal | Dokumen | Penomoran | Tautan | Pola |
|---|---|---|---|---|---|---|
| I-1 | `sale.CreateBookingAtomic` (`booking_repo.go:67-127`) | Dr kas / Cr Titipan Booking (atau Pendapatan Booking) | **KWB** `booking` | `Allocate` di `nextReceiptNumber` | `LinkJournalBySource("receipts", …)` :122 | A |
| I-2 | `sale.ReceivePayment` → `repository.go:600-692` | Dr kas / Cr Uang Muka \| Piutang | **KWT** `house_payment` | idem | `LinkJournalBySource` :681 | A |
| I-3 | jalur yang sama, `payment_source = kpr_disbursement` | Dr kas / Cr 1-2200 Pembiayaan | **KWD** `kpr_disbursement` | idem, **seri sendiri** | idem | A |
| I-4 | `charge.ReceivePayment` (`service.go:470-565`) | Dr kas / Cr Titipan Realisasi | **KWR** `realization` | idem | `LinkJournalBySource` :560 | A |
| I-5 | `notary.ReceiveDeposit` (`notary.go:169-178`) — intake **ditutup 410** sejak W-1 | Dr kas / Cr 2-2300 | **BKM** `cash_in` | `IssueForJournal` | otomatis | B |
| I-6 | jurnal manual (`ledger/handler.go:501`) & jurnal berulang (`recurring.go:230`) | apa pun yang mendebit kas | **BKM** `cash_in` | `IssueForJournal` | otomatis | B |

Untuk I-6 kas masuk, `ManualDocumentChoices` mengembalikan **satu** pilihan saja
(BKM) dan `ResolveManualDocument` mengisinya sendiri. Alasannya bukan kemalasan:
KWT/KWB/KWR/KWD semuanya lahir dari baris bisnis dan tidak pernah dari jurnal
manual, jadi yang tersisa memang hanya BKM. Untuk jurnal berulang jenis
disimpulkan `DefaultCashSpec` dari arah pergerakan — cron tidak punya lawan
bicara yang bisa ditanya.

### M.3 Cash Out — empat jalur

| # | Sumber | Jurnal | Dokumen | Penomoran | Pola |
|---|---|---|---|---|---|
| O-1 | `cost` realisasi biaya (`repository.go:204-210`, `cashOut = PaymentMethod==bank`) | Dr biaya/WIP / Cr kas | **BKK** `cash_out` | `PostDraft(spec)` → `IssueForJournal` | B |
| O-2 | `tax.PayTax` (`tax/repository.go:93-99`) | Dr Utang Pajak / Cr kas | **BKK** | idem | B |
| O-3 | `commission.Pay` (`commission/service.go:277-283`) | Dr Utang Komisi / Cr kas | **BKK** | `CreateAndPost(spec)` | B |
| O-4 | jurnal manual & berulang kas keluar | apa pun yang mengkredit kas | **BKK** \| **BTP** \| **RFC** — dipilih operator | `PostDraft(spec)` | B |
| O-5 | `charge.RecordPayout` (`service.go:655-658`) | Dr Titipan Realisasi / Cr kas | **BTP** `third_party_payout` | `CreateAndPost(spec)` | B |
| O-6 | `notary.PayOut` (`notary.go:247-256`) | Dr 2-2300 / Cr kas | **BTP** | idem | B |
| O-7 | `cancellation.PayRefund` (`refund_service.go:155-164`) | Dr Hutang Refund / Cr kas | **RFC** `customer_refund` | idem | B |
| O-8 | `charge.RefundResidual` (`service.go:786-789`) | Dr Titipan Realisasi / Cr kas | **RFC** | idem | B |
| O-9 | `charge.VoidPayment` (`service.go:1197-1209`) | pembalik KWR | **JR** + `reverses_document_id` | `IssueReversal` | C |
| O-10 | `PostingService.Reverse` — **setiap** pembalik jurnal kas | swap Dr/Cr | **JR** | `reversalDocument` otomatis | C |

Untuk O-4, `ManualDocumentChoices` memberi **tiga** pilihan dan
`ResolveManualDocument` menolak posting tanpa pilihan
(`ErrDocumentTypeRequired`). Ketiganya identik dari sisi debit/kredit; yang
membedakan hanya kepemilikan ekonomis dana, dan itu hanya diketahui operator.

### M.4 Delapan dimensi — jawaban yang berlaku untuk SEMUA jalur

Pertanyaan 6, 7, dan 8 tidak punya jawaban per jalur, karena mekanismenya satu
dan sama untuk semuanya. Itu sendiri adalah temuan audit yang penting.

**6 · Atomik?** Ya, seluruhnya. Setiap jalur berjalan dalam satu transaksi DB
yang memuat: jurnal, baris bisnis, alokasi nomor, baris registry dokumen, dan
tautan `journal_entries.document_id`. `Post`/`PostDraft`/`CreateAndPost` dibungkus
`s.inTx`; jalur yang sudah punya transaksi sendiri (charge, commission,
cancellation, notary, booking) membangun `PostingService` **dari `tx`** lewat
`ledger.NewGORMRepository(tx)`, sehingga `documents()` menulis di transaksi yang
sama — bukan di koneksi lain.

**7 · Kalau penerbitan dokumen gagal?** Seluruh transaksi rollback: jurnal tidak
jadi posted, baris bisnis tidak jadi tersimpan, kas tidak bergerak.
`applyDocument` mengembalikan error, `CreateAndPost`/`PostDraft` meneruskannya
keluar dari `inTx`. Penyebab kegagalan yang mungkin semuanya fail-closed:
`resolveType` menolak kode tak dikenal / jenis nonaktif / prefix kosong /
kebijakan reset tidak sah (**tanpa fallback prefix default**), `uk_doc_source`
menolak sumber yang sudah berdokumen, `uk_doc_number` menolak nomor kembar,
`uk_je_document` menolak dokumen yang sudah dipakai jurnal lain, dan guard
`document_id IS NULL` di `LinkJournal` menolak jurnal yang sudah bertaut.

**8 · Nomor tetap tidak terpakai kalau rollback?** Ya. `document_sequences`
dinaikkan dengan `INSERT … ON DUPLICATE KEY UPDATE last_val = last_val + 1` **di
dalam transaksi yang sama** — bukan sequence otonom, jadi rollback mengembalikan
`last_val`. Diuji langsung di `TestRollback_NomorIkutBatal`
(`document_integration_test.go:243`): alokasi lalu rollback, alokasi berikutnya
tetap mendapat `KWT/2026/000002`. Diuji sekali lagi dari sisi penegakan di
`TestIntegration_EQ_INV_DOC_1` — percobaan posting kas tanpa dokumen ditolak dan
`sequenceState` tidak berubah ("penolakan membakar nomor").

Preview nomor (`GET /documents/preview`) sengaja tidak memakai jalur ini —
`TestPreview_TidakMemakaiNomor`.

### M.5 Booking — fail-open yang sebelumnya ditemukan

Tidak ada jalur kwitansi baru yang dibuat; yang ada memang sudah memakai Document
Engine W-2. Yang ditutup adalah fail-open-nya, di tiga lapis:

1. **Seam tidak lagi opsional.** `p.GenerateReceipt` sebagai syarat sudah hilang.
   `booking_repo.go:110` sekarang: `if r.receiptTx == nil { return fmt.Errorf(...) }`
   — wiring yang tidak lengkap menjadi kegagalan yang terlihat, bukan kwitansi
   yang diam-diam hilang. Pola yang sama di `sale/repository.go:663` dan
   `charge/service.go:543`.
2. **Urutan dibalik.** Jurnal booking dibuat sebagai **draft** (`Create`, bukan
   `CreateAndPost`), lalu termin, lalu KWB, lalu `LinkJournalBySource`, dan baru
   `PostDraft`. Selama tautannya belum ada, kas belum bergerak sama sekali.
3. **Backstop tak bisa dilewati.** Seandainya langkah 1 dan 2 dilanggar lagi
   nanti, `enforceDocument` di dalam `PostDraft` menolak — ia memeriksa **keadaan
   akhir jurnal**, bukan niat pemanggilnya.

Diverifikasi hidup di `TestIntegration_EQ_INV_DOC_1`, yang menjalankan booking →
kontrak KPR → DP → akad → pencairan lewat jalur produksi lalu mengaudit bukunya.

### M.6 BKM tersedia sebagai jenis dokumen

Terdaftar di master sistem: `internal/document/seed.go:25,40` —
`{Code: "cash_in", Name: "Bukti Kas Masuk", Prefix: "BKM"}`, dan ikut daftar
jenis kas yang diaudit (`audit.go:364`). Di dev sekarang **22 jurnal kas
terposting** memegang dokumen `cash_in`. Konstanta `ledger.DocCashIn` meminjam
nilainya dari package `document` supaya tidak pernah ada dua daftar yang
berselisih.

### M.7 Bukti — buku nyata, bukan niat kode

Audit seluruh basis data dev (17 tenant, seluruh jenis), 2026-08-09:

| Pemeriksaan | Temuan |
|---|---|
| Jurnal kas terposting tanpa dokumen (non `opening_balance`) | **0** |
| Dokumen kas tanpa transaksi | **0** |
| Satu dokumen dipakai >1 jurnal | **0** |
| Satu sumber punya >1 dokumen | **0** |
| Nomor kembar | **0** |
| Seri tertinggal (`last_val < MAX(sequence_no)`) | **0** |

Sebaran jurnal kas terposting: `cash_in` 22 · `cash_out` 21 · `house_payment` 18
· `booking` 13 · `customer_refund` 1 · saldo awal (dikecualikan) 1.

Suite integrasi: **22 paket PASS**. Yang relevan langsung:
`TestIntegration_EQ_INV_DOC_1` (sisi positif + sisi negatif),
`TestW31_*` (tautan dua arah, larangan dokumen ganda, rollback),
`TestW34_*` (tujuh skenario audit), `TestW30_PayTax_*` (atomisitas),
`TestIntegration_ChargeGroup_FullLifecycle` (KWR + BTP + JR),
`TestIntegration_Refund_FromBooking` (RFC).

### M.8 Batas kejujuran audit ini

Tiga hal yang **tidak** dibuktikan oleh data dev, hanya oleh kode + test:

- `charge.RefundResidual` (O-8) tidak dipanggil test integrasi mana pun. Ia
  memakai `postCashJournalTx` dengan spec RFC — pola yang sama dengan O-5/O-7
  yang teruji — dan tertutup `enforceDocument`, tetapi belum pernah dijalankan
  end-to-end.
- KWR, KWD, BTP, dan JR belum muncul di data dev (hanya di database test).
  Nol pelanggaran pada jenis yang belum pernah terbit bukan bukti apa pun; yang
  membuktikan jalurnya adalah test integrasi di M.7.
- `notary.ReceiveDeposit` (I-5) sudah ditutup untuk produksi (HTTP 410 sejak
  W-1); yang tersisa hanya penumbuh data warisan di test.

Ketiganya bukan lubang invariant — `enforceDocument` menutup semuanya secara
struktural — melainkan batas cakupan **pengujian**, dan pantas dicatat apa
adanya.

### M.9 Kesimpulan

Sepuluh jalur cash-in/cash-out yang diminta owner sudah tertelusur delapan
dimensi. Tidak ada jalur yang bisa memposting pergerakan kas tanpa dokumen
bernomor: yang lupa menyatakan dokumen, salah urutan, atau menambah jalur baru
tanpa membaca dokumen ini sekalipun akan berhenti di `enforceDocument`, di dalam
transaksi, sebelum kas bergerak.

**W-3 FULLY CLOSED.**
