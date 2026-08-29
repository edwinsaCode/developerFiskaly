# W-3 — Architecture Review (read-only): INV-DOC-1 + Dokumen Jalur Kas

**Tanggal:** 2026-08-07 · **Sifat:** review, **tidak satu baris kode pun diubah**
**Prasyarat:** W-2 (Document Numbering Engine) SHIPPED
**Ruang lingkup W-3 menurut roadmap:**
- `business-architecture-freeze-2026-08.md:575` — *"Nyalakan **INV-DOC-1** (fail-closed) + dokumen untuk 8 jalur kas keluar"*
- `final-business-architecture-validation-2026-08.md:280` — *"Nyalakan **INV-DOC-1** + dokumen untuk seluruh jalur kas keluar; custom cash document"*

---

## §0 · Ringkasan untuk pengambil keputusan

INV-DOC-1 berbunyi: **tidak ada uang bergerak tanpa dokumen.** Setiap jurnal
terposting yang menyentuh akun kas/bank wajib punya dokumen bernomor.

Tiga temuan yang menentukan bentuk W-3:

1. **Belum ada tautan jurnal ↔ dokumen sama sekali.** `journal_entries` tidak
   punya `document_id`; `documents` tidak punya `journal_entry_id`. W-2
   membangun registry nomor, bukan tautan ke ledger. Invariant ini **tidak bisa
   ditegakkan hari ini** — bukan karena aturannya belum dinyalakan, tetapi
   karena kolom yang akan diperiksa belum ada. Ini keputusan skema pertama W-3.

2. **Jalur kas bukan 8, melainkan 12.** Roadmap menghitung *kategori bisnis*;
   kode punya 12 tempat yang benar-benar memposting jurnal kas, tiga di
   antaranya generik (jurnal manual, saldo awal, jurnal berulang) yang bisa
   menyentuh akun apa pun termasuk kas. Tiga yang generik itulah lubang
   terbesar, dan roadmap tidak menyebutnya.

3. **Dua jalur kas belum atomik.** `tax.PayTax` dan `cost.CreateCostEntry`
   memposting jurnal di luar transaksi DB. Menambahkan alokasi nomor dokumen ke
   jalur yang belum atomik akan **memperburuk**, bukan memperbaiki: kegagalan di
   tengah meninggalkan jurnal kas terposting + nomor dokumen terpakai + baris
   bisnisnya tidak ada. Keduanya harus dijadikan transaksional **sebelum**
   INV-DOC-1 dinyalakan.

Yang saya butuhkan dari Bapak/Ibu: **7 keputusan** di §7. Empat di antaranya
teknis-tapi-berkonsekuensi-bisnis (bentuk tautan, kapan nomor diambil, apa yang
dikecualikan, bagaimana pembalikan bernomor); tiga sisanya murni keputusan
bisnis (katalog jenis dokumen kas keluar, KWD pencairan KPR, dan nasib jurnal
manual yang menyentuh kas).

---

## §1 · Business flow — ke mana saja uang keluar

Peta ini dari **kode**, bukan dari dokumen arsitektur. Kolom "Tx" = apakah
jurnal + baris bisnisnya berada dalam satu transaksi DB.

### 1.1 Kas KELUAR

| # | Peristiwa bisnis | Kode | Jurnal | Tx | Dokumen hari ini |
|---|---|---|---|---|---|
| O-1 | Belanja tanah / vendor / subkon (bayar langsung) | `cost/service.go:346` `CreateCostEntry` (`payment_method=bank`) | Dr 1-3xxx Persediaan / Cr Kas | ❌ | tidak ada |
| O-2 | Overhead & beban marketing (bayar langsung) | sama, tier `overhead` | Dr 5-3000/5-4000 / Cr Kas | ❌ | tidak ada |
| O-3 | Bayar pihak ketiga dari titipan realisasi | `charge/service.go:642` `RecordPayout` | Dr 2-2400 Titipan / Cr Kas | ✅ | tidak ada |
| O-4 | Refund sisa titipan ke customer | `charge/service.go:773` `Refund` | Dr 2-2400 / Cr Kas | ✅ | tidak ada |
| O-5 | Void pembayaran realisasi (kas dikembalikan) | `charge/service.go:1182` `VoidPayment` | Dr 2-2400 / Cr Kas | ✅ | tidak ada |
| O-6 | Bayar refund booking / pembatalan | `cancellation/refund_service.go:154` `PayRefund` | Dr Hutang Refund / Cr Kas | ✅ | tidak ada |
| O-7 | Bayar komisi | `commission/service.go:275` `Pay` | Dr 2-6200 Utang Komisi / Cr Kas | ✅ | tidak ada |
| O-8 | Setor PPh Final | `tax/service.go:286` `PayTax` | Dr 2-4000 Hutang PPh / Cr Kas | ❌ | tidak ada |
| O-9 | Bayar titipan notaris (jalur warisan) | `notary/notary.go:244` `Payout` | Dr 2-2300 / Cr Kas | ✅ | tidak ada |

### 1.2 Kas MASUK

| # | Peristiwa | Kode | Dokumen hari ini |
|---|---|---|---|
| I-1 | Booking fee | `sale/booking_repo.go:66` | ✅ KWB |
| I-2 | DP / termin / pelunasan | `sale/repository.go` `CommitPayment` | ✅ KWT |
| I-3 | Titipan biaya realisasi | `charge/service.go:495` | ✅ KWR |
| I-4 | Alih dana internal | `charge/service.go:1013` | ✅ MTI (memo) |
| I-5 | **Pencairan KPR** | `CommitPayment`, `payment_source=kpr_disbursement` | ⚠️ **menumpang KWT** |
| I-6 | Titipan notaris (warisan, ditutup W-1) | `notary/notary.go:166` | tidak ada |

### 1.3 Jalur GENERIK — bisa menyentuh kas, tidak terikat modul mana pun

| # | Jalur | Kode | Tx | Dokumen |
|---|---|---|---|---|
| G-1 | **Jurnal manual** (akun bebas, termasuk kas) | `ledger/handler.go:412` `createJournal` | ❌ (draft) | tidak ada |
| G-2 | **Saldo awal** (`source=opening_balance`) | jalur yang sama | ❌ | tidak ada |
| G-3 | **Jurnal berulang** (template bulanan) | `ledger/recurring.go:200` | ❌ | tidak ada |
| G-4 | **Pembalikan** jurnal apa pun | `ledger/posting_service.go:146` `Reverse` | — | tidak ada |
| G-5 | Settlement pembatalan unit | `cancellation/service.go:650` | ✅ | tidak ada |

**Jalur yang PASTI tidak menyentuh kas** (diperiksa, tidak perlu dokumen):
BAST (pendapatan/HPP/pajak), akad KPR, akrual komisi, akrual PPh, tutup buku
(`ledger/closing.go` — hanya 4-/5-/ekuitas), HPP true-up (`closing/service.go`
— hanya 1-3xxx/5-1000).

---

## §2 · Existing implementation — apa yang sudah ada dan apa yang belum

### 2.1 Yang sudah ada (W-2, terbukti lewat test)

- `document_types` / `document_sequences` / `documents` — satu engine, semua jenis.
- `document.Allocate` / `Register` / `Issue` — wajib di transaksi pemanggil;
  row lock-nya yang mencegah nomor kembar.
- `resolveType` **fail-closed**: kode tak dikenal, jenis nonaktif, atau format
  rusak → error, sebelum ada nomor terbit.
- `documents` UNIQUE `(tenant, source_table, source_id)` — satu sumber satu dokumen.
- `ledger.AccountInRole(a, RoleCashBank)` — otoritas tunggal "akun ini kas/bank",
  COA-driven lewat `accounts.category` (`ledger/account_role.go:76-80`).
  **Ini yang membuat INV-DOC-1 bisa ditulis sebagai satu predikat, bukan daftar kode.**

### 2.2 Yang BELUM ada — dan inilah isi W-3

**A. Tautan jurnal ↔ dokumen.**
`journal_entries` (`ledger/model.go:30-45`) tidak punya `document_id`.
`documents` (`document/model.go`) tidak punya `journal_entry_id`.
Registry W-2 menaut dokumen ke **baris sumber bisnis** (`receipts`, `invoices`,
`charge_settlements`) — bukan ke jurnal. Untuk kwitansi tautan itu tidak
langsung: `receipts` → `termin_payments` → `journal_entry_id`. Tiga lompatan,
dan hanya berlaku untuk satu dari dua belas jalur.

**B. Titik penegakan.**
`PostingService.Post` (`ledger/posting_service.go:124`) adalah satu-satunya
pintu ke `posted_at`. Di situlah INV-DOC-1 harus diperiksa. Precedent seam-nya
sudah ada: `WithPeriodChecker(pc PeriodChecker)` (baris 76) — `ledger` tidak
mengimpor modul pemeriksa, pemeriksa yang disuntikkan ke `ledger`. Pola yang
sama bisa dipakai untuk `DocumentChecker`, sehingga arah impor tetap bersih
(`document` tidak mengimpor `ledger`, `ledger` tidak mengimpor `document`).

**C. Jenis dokumen kas keluar.** Master hari ini hanya punya 5 jenis
(`document/seed.go`): `house_payment`, `booking`, `realization`,
`internal_transfer`, `invoice`. **Nol** jenis kas keluar.

**D. Backfill dokumen untuk jurnal kas historis.** Belum ada.

**E. State `void` untuk dokumen.** §8.5 freeze doc mendefinisikan
`posted → void`; W-2 tidak mengimplementasikannya (`documents` tanpa kolom status).

### 2.3 Cacat yang ditemukan saat pemetaan (bukan bagian ruang lingkup, tapi memblokir)

| ID | Temuan | Lokasi | Kenapa memblokir W-3 |
|---|---|---|---|
| **B-1** | `tax.PayTax` memakai **daftar kode bank hardcode** `{1-1300, 1-1400, 1-1500}` | `tax/model.go:270-280`, dipakai `tax/service.go:252` | Bertentangan dengan keputusan COA-driven cash/bank. Tenant yang kas-nya `1-1100` **tidak bisa** menyetor pajak; tenant yang mengubah `1-1400` jadi `other_asset` **tetap bisa**. INV-DOC-1 memakai `AccountInRole(RoleCashBank)` — dua definisi "bank" yang berbeda pada satu jalur yang sama |
| **B-2** | `tax.PayTax` **tidak transaksional** — `CreateJournal` → `PostJournal` → `UpdateObligationStatus` → `SavePayment` empat operasi terpisah | `tax/service.go:286-310` | Gagal di langkah 3/4 → jurnal kas keluar sudah terposting, obligation masih `outstanding`, tidak ada baris `tax_payments`. Menambahkan dokumen ke sini akan menambah satu artefak yatim lagi |
| **B-3** | `cost.CreateCostEntry` membuat jurnal **draft** lalu menyimpan `cost_entries` di luar transaksi; `PostCostEntry` memposting tanpa transaksi | `cost/service.go:346-370`, `380` | Sumber langsung Keputusan **D-W3-2** (kapan nomor diambil) |
| **B-4** | Jurnal manual & saldo awal masuk lewat endpoint yang **tidak mengenal konsep kas** | `ledger/handler.go:385-429` | Lubang terbesar INV-DOC-1: siapa pun dengan hak tulis bisa memindahkan kas tanpa dokumen |

---

## §3 · SSOT — siapa otoritas apa setelah W-3

| Pertanyaan | Otoritas kanonik | Status |
|---|---|---|
| "Akun ini kas/bank?" | `ledger.AccountInRole(a, RoleCashBank)` → `accounts.category` | ✅ sudah tunggal |
| "Nomor dokumen berikutnya?" | `document.Allocate` → `document_sequences` | ✅ sudah tunggal (W-2) |
| "Jenis dokumen apa saja yang ada?" | `document_types` (data, bukan kode) | ✅ sudah tunggal (W-2) |
| **"Jurnal ini punya dokumen?"** | **belum ada** — W-3 harus menciptakannya | ❌ |
| **"Dokumen ini mewakili jurnal yang mana?"** | **belum ada** | ❌ |
| "Boleh tidak jurnal ini diposting?" | `PostingService.Post` (+ `PeriodChecker`) | ✅ satu pintu, tinggal ditambah satu pemeriksa |

Setelah W-3, kalimat invariantnya harus bisa dieksekusi apa adanya:

```
∀ je ∈ journal_entries WHERE je.posted_at IS NOT NULL
    ∧ ∃ jl ∈ je.lines : AccountInRole(jl.account, RoleCashBank)
  ⟹ je.document_id IS NOT NULL
```

dan diuji sebagai `EQ_*` di suite konsistensi — bukan sekadar diperiksa saat menulis.

---

## §4 · Dependency

| Arah | Isi | Status |
|---|---|---|
| W-3 ← W-2 | registry + engine penomoran | ✅ selesai |
| W-3 ← COA `category` | klasifikasi kas/bank | ✅ selesai (`coa-driven-cash-bank`) |
| W-3 ← B-1..B-4 | jalur kas harus atomik & konsisten dulu | ❌ **belum**, bagian dari W-3 |
| W-3 → W-4 | J-14 (sisa titipan → piutang) | independen, boleh paralel |
| W-3 → W-5 | Kelebihan Tanah | independen |
| Arah impor | `ledger` ⟂ `document` (tidak saling impor; seam via interface) | harus dijaga |

**Tidak ada dependency ke W-4/W-5.** W-3 berdiri sendiri.

Catatan: kedua dokumen arsitektur **berbeda soal isi W-4/W-5**
(`business-architecture-freeze-2026-08.md` §12 vs
`final-business-architecture-validation-2026-08.md:280-289`). Tidak
mempengaruhi W-3, tetapi perlu diselaraskan sebelum W-4 dimulai.

Catatan kedua: ringkasan proyek di beberapa tempat masih menyebut P0-4 sebagai
skema mati (*"000029-032 belum ada kode"*). Diperiksa ulang: `internal/closing`
mengimplementasikan HPP true-up penuh dan sudah ter-wiring di
`cmd/api/main.go:96`. Tidak berkonsekuensi untuk W-3 (true-up tidak menyentuh
kas), tetapi catatan yang usang itu sudah dikoreksi.

---

## §5 · Kondisi data hari ini (diukur, bukan diperkirakan)

Query atas database dev, jurnal **terposting** yang menyentuh akun berkategori
`cash`/`bank`:

| | Jumlah |
|---|---|
| Jurnal kas terposting | **76** |
| — bertaut ke `termin_payments` (punya kwitansi/memo) | 52 |
| — bertaut ke `cost_entries` (**tidak** punya dokumen) | 7 |
| — tanpa taut apa pun (**tidak** punya dokumen) | 17 |
| Total baris `documents` | 39 |

17 jurnal tanpa taut, apa adanya dari `description`:

```
Saldo Awal                                   ← G-2
Pembayaran refund #7 kepada Smoke Buyer      ← O-6
Sewa Kantor Bulanan (jurnal berulang 2026-07)← G-3
Titipan notaris diterima / dibayarkan (2)    ← I-6 / O-9
Payout Notaris / Listrik / PDAM / BPHTB /
  IMB / Sertifikat (7)                       ← O-3
Refund sisa titipan realisasi grup #7, #69   ← O-4
Pembalik pembayaran realisasi (2)            ← O-5
```

**Ukuran backfill W-3 di basis data ini: 24 jurnal** (7 cost + 17 lepas).
Bentuknya, bukan angkanya, yang penting — di produksi jumlahnya berbeda,
kategorinya sama persis.

Satu lagi yang terukur: **8 pencairan KPR** (`payment_source=kpr_disbursement`)
hari ini menerbitkan kwitansi bertipe `house_payment` — persis anomali "KWD
menumpang KWT" yang dicatat §5.2 freeze doc. Sesuai aturan forward-only, 8
dokumen itu **tidak akan disentuh** apa pun keputusannya.

---

## §6 · Risiko akuntansi

**R-1 · Fail-closed pada kas keluar lebih tajam daripada pada kas masuk.**
W-1/W-2 fail-closed menghentikan *penerimaan*. INV-DOC-1 fail-closed
menghentikan *pembayaran* — gaji vendor, setoran pajak, refund customer. Master
jenis dokumen yang salah konfigurasi berarti perusahaan tidak bisa membayar
siapa pun. Karena itu penegakan **wajib bertahap**: seed jenis → backfill →
laporkan pelanggaran (mode audit) → baru tolak.

**R-2 · Nomor terbakar pada jurnal yang tidak jadi.**
`cost` membuat jurnal *draft* lebih dulu. Kalau nomor dokumen diambil saat
draft, setiap draft yang dibuang meninggalkan lubang di seri. Auditor menanyakan
lubang. (→ **D-W3-2**)

**R-3 · Pembalikan menggandakan dokumen atau tidak punya sama sekali.**
`PostingService.Reverse` menerbitkan jurnal baru. Kalau jurnal asal menyentuh
kas, jurnal pembalik juga. Tanpa aturan eksplisit, `Reverse` akan **gagal** di
bawah INV-DOC-1 — dan `Reverse` adalah satu-satunya cara sah mengoreksi ledger
(invariant #5). Mengunci kemampuan koreksi adalah risiko terbesar dari
penegakan yang naif. (→ **D-W3-5**)

**R-4 · Saldo awal bukan pergerakan kas.**
`opening_balance` menetapkan posisi, tidak memindahkan uang. Menuntut "bukti kas
keluar" untuk saldo awal adalah salah secara akuntansi. Harus dikecualikan
secara eksplisit — dan pengecualiannya harus sempit, jangan menjadi pintu
belakang. (→ **D-W3-3**)

**R-5 · Void dokumen vs ledger append-only.**
Freeze doc §8.5 menginginkan `documents` punya state `void`. Tetapi invariant #5
mengatakan koreksi hanya lewat jurnal pembalik. Kalau dokumen bisa di-void
sementara jurnalnya dibalik dengan jurnal baru, ada dua model koreksi yang
berbeda pada satu peristiwa. Rekomendasi saya: **`documents` tetap
append-only tanpa status** — pembalikan menerbitkan dokumennya sendiri, dan
"batal" terbaca dari adanya dokumen pembalik. Satu model koreksi, bukan dua.

**R-6 · Titipan bukan uang perusahaan.**
O-3 (payout ke pihak ketiga) dan O-4 (refund titipan) memindahkan uang yang
secara hukum milik customer/pihak ketiga. Menyeragamkannya ke dalam satu seri
"Bukti Kas Keluar" bersama belanja vendor membuat jejak titipan lebih sulit
ditelusuri saat diperiksa. (→ **D-W3-6**)

**R-7 · Dua definisi "bank" pada satu jalur (B-1).**
Sudah dijelaskan di §2.3. Ini bukan risiko teoretis: `tax.PayTax` akan menolak
akun yang oleh INV-DOC-1 dianggap kas, dan menerima akun yang tidak.

**R-8 · Idempotensi registry vs jalur yang bisa diulang.**
`documents` UNIQUE `(tenant, source_table, source_id)` berarti satu baris sumber
= satu dokumen. Untuk jalur yang barisnya lahir sekali (cost entry, commission,
refund) itu tepat. Untuk **jurnal manual** sumbernya adalah `journal_entries`
itu sendiri — juga satu-lawan-satu, aman. Tidak ada jalur yang butuh dua
dokumen atas satu sumber. Diperiksa: aman.

---

## §7 · Yang perlu diputuskan Bapak/Ibu

Empat pertama teknis tapi berkonsekuensi ke bentuk laporan dan audit; tiga
terakhir murni bisnis.

### D-W3-1 · Di mana tautan jurnal ↔ dokumen disimpan?

| Opsi | Bentuk | Konsekuensi |
|---|---|---|
| **A (rekomendasi)** | `journal_entries.document_id` BIGINT NULL | Invariant dibaca persis seperti bunyinya; `Post` memeriksa satu kolom di baris yang sedang diposting. Dokumen tanpa jurnal (invoice, dan itu benar) tetap sah — kolomnya hanya kosong di sisi jurnal |
| B | `documents.journal_entry_id` | Penegakan harus query balik ke `documents` setiap posting; invoice memaksa kolom nullable juga; tidak ada keuntungan |

**Rekomendasi: A.**

### D-W3-2 · Kapan nomor dokumen diambil pada alur dua langkah (cost)?

| Opsi | Perilaku | Konsekuensi |
|---|---|---|
| **A (rekomendasi)** | Nomor diambil saat **posting** | Tidak ada lubang seri dari draft yang dibuang. Konsisten dengan W-2: "tidak ada nomor tanpa jurnal". Konsekuensi: nomor BKK belum terlihat saat entri masih draft |
| B | Nomor diambil saat **draft dibuat** | Nomor terlihat sejak awal (enak untuk operator), tapi setiap draft batal = satu lubang permanen di seri |

**Rekomendasi: A.** Kalau operator butuh nomor sejak awal, itu tanda alur
draft-nya yang perlu dihapus untuk kas keluar — bukan seri yang perlu berlubang.

### D-W3-3 · Apa yang dikecualikan dari INV-DOC-1?

Usulan pengecualian **hanya satu**: `source = 'opening_balance'` (bukan
pergerakan kas — lihat R-4). Semua yang lain — termasuk jurnal manual, jurnal
berulang, dan settlement pembatalan — **wajib** berdokumen.

**Pertanyaan:** setuju daftar pengecualian hanya berisi saldo awal?

### D-W3-4 · Jurnal manual yang menyentuh kas — tolak, atau wajibkan pilih jenis dokumen?

| Opsi | Perilaku |
|---|---|
| **A (rekomendasi)** | Boleh, **tetapi wajib memilih jenis dokumen kas** (mis. BKK/BKM); sistem menerbitkan nomor otomatis dalam transaksi yang sama |
| B | Jurnal manual **dilarang** menyentuh akun kas sama sekali; semua pergerakan kas harus lewat modul |
| C | Dibiarkan bebas (tidak menegakkan INV-DOC-1 untuk jurnal manual) |

**Rekomendasi: A.** B lebih "bersih" secara teori tetapi menutup satu-satunya
katup untuk kejadian yang belum dimodelkan; C membuat invariantnya kosong —
siapa pun bisa memindahkan kas lewat jurnal manual.

### D-W3-5 · Jurnal pembalik yang menyentuh kas — bernomor sendiri atau ikut dokumen asal?

| Opsi | Perilaku |
|---|---|
| **A (rekomendasi)** | Menerbitkan **dokumennya sendiri** dengan jenis khusus (usulan `journal_reversal`, prefix **JR**), menyebut nomor dokumen asal di keterangan |
| B | Mewarisi `document_id` dokumen asal (dua jurnal, satu dokumen) |
| C | Dikecualikan dari INV-DOC-1 |

**Rekomendasi: A.** B melanggar "satu sumber satu dokumen" dan membuat laporan
kas menghitung satu dokumen dua kali. C membuka lubang: setiap orang bisa
memindahkan kas dengan membalik jurnal.

### D-W3-6 · Katalog jenis dokumen kas keluar — berapa seri? ⭐ keputusan bisnis

| Opsi | Bentuk | Konsekuensi praktis |
|---|---|---|
| A | **8 seri terpisah** sesuai §5.2 freeze doc: BT tanah, BV vendor, BS subkon, BN notaris/pihak ketiga, RFC refund, BK komisi, BP pajak, BU pengeluaran lain | Paling deskriptif; tetapi BT/BV/BS di kode adalah **satu jalur yang sama** (`cost`) yang hanya berbeda `CostCategory` — tiga seri dari satu tombol. Admin mengelola 8 seri |
| B | **1 seri** — BKK Bukti Kas Keluar untuk semua kas keluar | Paling sederhana, sesuai kebiasaan pembukuan Indonesia (satu buku BKK); detail ada di jurnal & keterangan. Tetapi uang titipan tercampur satu seri dengan uang perusahaan |
| **C (rekomendasi)** | **3 seri menurut *milik siapa uangnya***: **BKK** kas keluar operasional (tanah, vendor, subkon, overhead, komisi, pajak) · **BTP** pembayaran titipan ke pihak ketiga (notaris, PLN, PDAM, BPHTB, dll) · **RFC** pengembalian dana ke customer | Jejak titipan (bukan uang perusahaan) terpisah dan bisa direkonsiliasi sendiri — yang justru ditanyakan saat diperiksa (R-6). Admin hanya mengelola 3 seri, dan tetap boleh menambah jenis sendiri (`is_system=false`) sesuai keputusan D-2 |

**Rekomendasi: C.** Pemisahannya mengikuti perbedaan yang berarti secara hukum
dan akuntansi (uang perusahaan / uang titipan / pengembalian ke customer), bukan
perbedaan kategori biaya yang sudah terekam di jurnal.

### D-W3-7 · Pencairan KPR — seri sendiri (KWD) atau tetap KWT? ⭐ keputusan bisnis

Hari ini 8 pencairan terbit sebagai **KWT** (kwitansi pembayaran rumah).
Pembayarnya **bank**, bukan customer.

| Opsi | Konsekuensi |
|---|---|
| **A (rekomendasi)** | Jenis baru **`kpr_disbursement` → KWD**. Kwitansi kepada bank tidak lagi tercampur dengan kwitansi kepada customer; laporan penerimaan per sumber dana jadi jujur |
| B | Tetap KWT | Nol pekerjaan; tetapi "kwitansi untuk customer" berisi transaksi yang customer-nya tidak pernah membayar |

Apa pun keputusannya, **8 dokumen KWT yang sudah terbit tidak berubah** —
forward-only, sesuai keputusan W-2.

---

## §8 · Rencana kerja W-3 setelah keputusan turun

Disusun agar setiap langkah bisa dihentikan tanpa meninggalkan sistem separuh jalan.

| Tahap | Isi | Bisa dihentikan? |
|---|---|---|
| **W-3.0** | Perbaiki B-1..B-3: `tax.PayTax` pakai `ledger.ValidatePaymentAccount` + satu transaksi; `cost` create/post dalam transaksi | ✅ perbaikan berdiri sendiri |
| **W-3.1** | Migrasi: `journal_entries.document_id` (nullable) + seed jenis dokumen kas keluar sesuai D-W3-6/7 | ✅ kolom kosong = perilaku lama |
| **W-3.2** | Setiap jalur kas menerbitkan dokumen di dalam transaksinya (12 jalur, satu per satu) | ✅ per jalur |
| **W-3.3** | Backfill dokumen untuk jurnal kas historis (24 di dev), memakai jenis yang tepat, tanpa menyentuh dokumen lama | ✅ |
| **W-3.4** | `DocumentChecker` di `PostingService` — mode **audit** (mencatat pelanggaran, tidak menolak) + laporan pelanggaran | ✅ |
| **W-3.5** | Nyalakan **fail-closed** + `EQ_INV_DOC_1` di suite konsistensi | titik tanpa jalan kembali |
| **W-3.6** | Frontend spec + implementasi: layar Bukti Kas Keluar, pemilihan jenis dokumen pada jurnal manual, nomor dokumen di layar biaya/komisi/pajak/refund | ✅ |

---

## §9 · Yang saya kerjakan sendiri tanpa menunggu keputusan

Sesuai mode kerja yang berlaku (lanjut end-to-end kecuali blueprint/invariant
berubah atau ada keputusan produk):

- B-1, B-2, B-3 — perbaikan konsistensi & atomisitas (tidak mengubah aturan bisnis apa pun)
- Bentuk seam `DocumentChecker` dan arah impor
- Struktur migrasi, backfill, dan test
- Seluruh frontend spec W-3

Yang **tidak** saya putuskan sendiri: tujuh butir §7.

---

**Catatan penutup.** Tidak ada baris kode, migrasi, atau file sumber yang
diubah untuk menghasilkan dokumen ini. Peta jalur kas berasal dari pembacaan
kode; angka di §5 berasal dari query baca-saja atas database dev.
