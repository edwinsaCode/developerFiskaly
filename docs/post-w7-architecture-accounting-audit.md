# POST-W7 Architecture & Accounting Consistency Audit

**Tanggal:** 2026-08-11
**Cakupan:** seluruh repository sesudah W-1 … W-7 selesai
**Sifat:** read-only. Tidak ada kode, database, migrasi, tenant, atau artefak yang diubah/dihapus selama audit ini.

**Konvensi label** yang dipakai di seluruh dokumen:

| Label | Arti |
| --- | --- |
| **FACT** | Terbukti langsung dari kode, migrasi, atau data. Selalu disertai `file:line` atau query. |
| **INFERENCE** | Kesimpulan teknis saya di atas FACT. Bisa salah kalau ada konteks yang saya belum lihat. |
| **BUSINESS DECISION** | Bukan urusan engineering. Harus diputuskan owner/akuntan. |

**Severity** hanya dipakai untuk temuan yang menghalangi: `CRITICAL` (angka salah / uang bocor sekarang), `HIGH` (bisa membuat pembukuan salah tanpa alarm), `MEDIUM` (benar hari ini, rapuh besok), `LOW` (kebersihan / defense-in-depth).

**Catatan tentang data:** semua tenant di database dev ini adalah tenant uji (`esaProperti Dev`, `UAT Batch 2`, `Legacy AR Test`, `W5 UI Check`, `UI Smoke PS1`). Angka dari database dipakai untuk **mengilustrasikan** temuan yang asalnya dari kode, bukan sebagai bukti kerusakan produksi.

---

## 1. Executive Verdict

**Fondasi domainnya sehat. Ada satu retakan struktural yang harus ditutup sebelum increment berikutnya, dan itu bukan di W-7.**

Yang benar-benar bagus, dan saya buktikan bukan cuma dari dokumen:

- **Document Domain (W-2/W-3) adalah bagian terkuat dari sistem ini.** Satu mesin penomoran, penegakan INV-DOC-1 yang *fail-closed* di dalam transaksi, ditambah audit detektif terpisah. Pada data hidup, jurnal kas terposting yang tidak punya dokumen berjumlah **satu** — dan itu justru `opening_balance`, satu-satunya pengecualian yang disengaja.
- **W-5, W-6, dan W-7 masing-masing memasang disiplin yang benar** di wilayahnya: piutang realisasi lahir saat invoice (bukan saat jadwal dibuat), snapshot historis berdiri di luar ledger dengan guard berbasis jurnal terposting, dan impor legacy tidak pernah menyentuh `journal_entries`.
- **Tidak ada mesin AR kedua.** `internal/receivable` betul-betul satu-satunya tempat aging dihitung, dan `Source` betul-betul cuma dimensi.

Yang retak:

> **Piutang harga rumah (`house`) adalah satu-satunya sumber piutang yang belum pernah didisiplinkan.** Layar aging menampilkan **jadwal penagihan komersial**, sementara buku besar mencatat **piutang akuntansi** yang baru lahir saat BAST. Keduanya diberi label yang sama: "Piutang".

Ini bukan bug W-7. Ini adalah pekerjaan yang W-5 lakukan untuk realisasi dan W-7 lakukan untuk legacy, tapi belum pernah dilakukan untuk rumah — dan karena `house` adalah sumber piutang **terbesar**, retakan ini yang paling mahal.

Konsekuensi langsung: **formula rekonsiliasi yang diajukan dalam brief audit ini salah** (§5). Bukan salah tulis — memang tidak bisa benar selama `house` belum punya sub-ledger dalam pengertian akuntansi.

Temuan blocking kedua tidak berhubungan: **endpoint pembalik jurnal generik** (`POST /ledger/journals/{id}/reverse`) bisa membalik jurnal milik sub-ledger mana pun tanpa memberi tahu pemiliknya (§10).

Verdict: **fondasinya layak dibangun di atasnya, setelah dua hal di atas ditutup.** Sisanya technical debt yang jujur, bukan kejutan.

---

## 2. System Boundary After W-7

Peta bounded context yang sebenarnya ada di kode (bukan yang ada di dokumen):

```
                        ┌─────────────────────────────────────┐
                        │            internal/domain          │
                        │  Money, ProductCategory, Treatment  │
                        │  (tidak import apa pun dari internal)│
                        └──────────────────┬──────────────────┘
                                           │ dipakai semua
        ┌──────────────────────────────────┼──────────────────────────────────┐
        │                                  │                                  │
┌───────▼────────┐              ┌──────────▼──────────┐            ┌──────────▼─────────┐
│ internal/ledger│◄─────────────┤  internal/document  │            │ internal/receivable│
│  SoT: SEMUA    │  IssueFor    │  SoT: NOMOR BUKTI   │            │  SoT: MESIN AGING  │
│  saldo & jurnal│  Journal     │  satu mesin, W-2    │            │  murni, tanpa DB   │
│  append-only   │              │  audit detektif     │            │  3 Source          │
└───────┬────────┘              └─────────────────────┘            └──────────▲─────────┘
        │ CreateAndPost                                                       │ rows
        │                                                                     │
┌───────┴──────────────────────────────────────────────────────────┐  ┌───────┴────────┐
│  Penulis ledger (masing-masing punya sub-ledger sendiri)         │  │ internal/      │
│                                                                  │  │ reporting      │
│  sale/        kontrak, jadwal, termin, BAST, KPR                 │──┤ (perakit,      │
│  charge/      biaya realisasi, titipan, invoice realisasi        │──┤  bukan pemilik)│
│  legacyar/    piutang proyek lama (impor TANPA jurnal)           │──┤                │
│  cost/ tax/ commission/ cancellation/ notary/ closing/           │  └────────────────┘
└──────────────────────────────────────────────────────────────────┘
                                           ▲
                        ┌──────────────────┴──────────────────┐
                        │          internal/histfin           │
                        │  SoT: ANGKA LAPORAN HISTORIS        │
                        │  DI LUAR ledger. Hanya baca ledger  │
                        │  untuk guard tahun.                 │
                        └─────────────────────────────────────┘
```

**FACT — batasnya nyata, bukan konvensi penamaan.** `internal/histfin` menyalin satu konstanta alih-alih mengimpor `internal/document`, dengan alasan yang ditulis eksplisit di kode:

```go
// internal/histfin/repository.go:165-177
// sourceOpeningBalance disalin dari document.SourceOpeningBalance. Disalin, bukan
// diimport, supaya histfin tidak menarik ketergantungan ke paket dokumen hanya
// demi satu literal — nilainya juga tercatat di kolom journal_entries.source.
const sourceOpeningBalance = "opening_balance"
```

**INFERENCE:** ini keputusan yang benar. Satu literal yang duplikat lebih murah daripada satu edge di grafik dependensi. Tapi ia harus tercatat sebagai duplikasi yang **disengaja** — dan sekarang tercatat (§9, M-3).

**FACT — tidak ada import siklik.** `go build ./...` dan `go vet ./...` lolos; Go menolak siklus di level kompilasi, jadi ini terbukti secara struktural, bukan lewat inspeksi.

**FACT — `internal/domain` bersih.** Tidak ada file di `internal/domain/` yang mengimpor paket `internal/*` lain (CLAUDE.md §Konvensi package dipatuhi).

---

## 3. Domain Ownership Map

| Konsep | Pemilik (SoT) | Penulis lain? | Verdict |
| --- | --- | --- | --- |
| Saldo akun, jurnal | `internal/ledger` | tidak — semua lewat `PostingService` | ✅ tunggal |
| Nomor dokumen | `internal/document` | tidak (§8) | ✅ tunggal |
| Aging / umur piutang | `internal/receivable` | tidak | ✅ tunggal |
| Baris piutang `house` | `internal/reporting/repository.go` (query `payment_schedules`) | — | ⚠️ **§5 H-1** |
| Baris piutang `realization` | `internal/charge/query.go` | — | ✅ |
| Baris piutang `legacy` | `internal/legacyar` | — | ✅ |
| Angka laporan historis | `internal/histfin` | tidak | ✅ tunggal |
| Titipan biaya realisasi | `internal/charge` | `internal/notary` (jalur notaris lama) | ⚠️ §9 M-4 |
| Kwitansi customer | `internal/billing` (nomor dari `document`) | — | ✅ |
| Peran akun → kode COA | `internal/ledger/account_role.go` | **22 file lain** meng-hardcode kode | ⚠️ **§9 M-1** |

**FACT — reporting adalah perakit, bukan pemilik.** `internal/reporting/ar_aging.go` menggabungkan tiga pembaca lalu menyerahkan ke `receivable.BuildAging(...)`. Tidak ada perhitungan umur/bucket di `reporting`.

**FACT — tapi reporting membaca tabel transaksional langsung.** `internal/reporting/repository.go:215-260` melakukan `SELECT … FROM payment_schedules ps JOIN sale_contracts … JOIN units …` — yaitu tabel milik `internal/sale`, tanpa melewati seam apa pun. Sama halnya `charge/query.go` untuk realisasi (tapi itu di dalam paketnya sendiri, jadi sah).

**INFERENCE (MEDIUM, T-2):** untuk `charge` dan `legacyar`, pembacanya ada **di dalam** paket pemilik dan reporting hanya memanggil interface. Untuk `house`, reporting menulis SQL-nya sendiri di atas tabel milik `sale`. Itu asimetri yang membuat perubahan skema `payment_schedules` bisa memecahkan laporan tanpa satu pun test di `sale` yang menyala. Ini juga sebabnya H-1 (§5) bisa lolos selama ini: tidak ada satu pun orang di `internal/sale` yang "memiliki" definisi piutang rumah.

---

## 4. Accounting Flow Audit

Tiap alur ditelusuri **handler → service → repository → ledger/document**, bukan dari nama file.

Kolom: **GL?** = masuk buku besar. **AR?** = muncul di daftar piutang. **Snap?** = tersentuh histfin. **Kas?** = menggerakkan kas/bank. **Balik?** = cara koreksinya.

---

### A. Booking (terima booking fee)

- **Peristiwa ekonomi:** developer menerima uang muka pemesanan sebelum ada kontrak.
- **Jurnal:** Dr Kas/Bank · Cr **4-2100 Pendapatan Booking**
- **Sumber:** `internal/sale/booking_repo.go:122`
- **Dokumen:** **KWB** (receipt `booking`), ditautkan ke jurnal lewat `document.LinkJournalBySource(tx, tenant, "receipts", receiptID, entry.ID)`
- **GL?** ya · **AR?** tidak · **Snap?** tidak · **Kas?** masuk · **Balik?** jurnal pembalik

**FACT:** booking di-kredit ke **pendapatan**, bukan kewajiban — ini keputusan klien final (`booking-fee-revenue-rule`, 2026-07-29), sengaja menyimpang dari CLAUDE.md Invariant #7. **Bukan temuan.** Saya catat di sini hanya supaya auditor berikutnya tidak melaporkannya sebagai pelanggaran invariant.

**FACT:** live data — 13 dokumen `booking`, 13/13 tertaut jurnal. Tidak ada KWB yatim.

---

### B. Pembayaran customer (cicilan / termin harga rumah)

- **Peristiwa:** buyer membayar cicilan sesuai jadwal.
- **Jurnal (pra-BAST):** Dr Kas/Bank · Cr **2-2000 Uang Muka Penjualan** (kewajiban)
- **Jurnal (pasca-BAST):** Dr Kas/Bank · Cr **1-2000 Piutang Usaha**
- **Sumber:** `internal/sale/service.go:20-21`, routing di `ReceivePayment`; penautan dokumen `internal/sale/repository.go:700`
- **Dokumen:** **KWT** · **GL?** ya · **AR?** ya (`house`) · **Kas?** masuk · **Balik?** pembalik

**FACT:** routing pra/pasca-BAST benar dan sesuai Invariant #7 (`gap1-payment-routing`). **INFERENCE:** inilah sumber H-1 — sebelum BAST uang customer menjadi **kewajiban**, jadi tidak ada piutang di GL; tapi jadwalnya sudah tampil sebagai "Piutang" di layar aging. Lihat §5.

---

### C. Pencairan KPR

- **Peristiwa:** bank mencairkan dana KPR ke developer.
- **Jurnal:** Dr Bank · Cr **1-2200 Piutang Bank (KPR)** (pasca-akad), sisanya direklas ke 1-2000 (T-3, `t3-kpr-shortfall-customer-receivable`)
- **Sumber:** `internal/sale/scheme_flow.go:815-840` (`ResolveReceivableAccount` → `FinancingReceivableAccount`)
- **Dokumen:** **KWD** (`ReceiptTypeKPRDisbursement`, `internal/billing/receipt.go:37`)
- **GL?** ya · **AR?** ❌ **tidak** · **Kas?** masuk · **Balik?** pembalik

**FACT — 1-2200 tidak punya representasi di daftar piutang.** `internal/ledger/account_role.go:47` memasukkan `1-2200` ke `RoleReceivable`, tapi `internal/receivable` hanya punya tiga `Source` dan tidak satu pun membaca 1-2200. Live: tenant `UI Smoke PS1` (id 10) memegang **Rp 20.000.000** di 1-2200 yang tidak muncul di aging mana pun.

**INFERENCE (MEDIUM, M-2):** ini secara akuntansi mungkin **benar** — piutang ke bank bukan piutang customer, dan mencampurnya ke aging customer justru salah. Tapi artinya "daftar piutang" di aplikasi ini **bukan** daftar seluruh piutang. Yang perlu diperbaiki bukan datanya, melainkan **klaimnya**: §5 harus berhenti menyebut rumus "GL 1-2000 = Σ sub-ledger" sebagai rekonsiliasi menyeluruh.

---

### D. Terima titipan biaya realisasi

- **Peristiwa:** customer menitipkan dana untuk PDAM/BPHTB/Notaris/Listrik.
- **Jurnal:** Dr Kas/Bank · Cr **2-2400 Titipan Biaya Realisasi** (per jenis biaya; akun di-snapshot di item, `000063`)
- **Sumber:** `internal/charge/service.go`; kwitansi `ReceiptTypeRealization`
- **Dokumen:** **KWR** · **GL?** ya · **AR?** tidak (belum jadi piutang) · **Kas?** masuk

**FACT:** bukan pendapatan. Konsisten dengan K-1..K-5 (`billing-batch2-charge-group`).

---

### E. Invoice biaya realisasi (J-14a — lahirnya piutang, R-5)

- **Peristiwa:** tagihan realisasi diterbitkan; sisanya yang belum dibayar **menjadi piutang customer**.
- **Jurnal:** Dr Kas (yang sudah masuk) · Dr **1-2000 Piutang Customer** (sisa) · Cr **2-2400 Titipan** (total)
- **Sumber:** `internal/charge` (recognition), `charge_receivable_recognitions` (append-only, `000067`)
- **Dokumen:** **INV** · **GL?** ya · **AR?** ya (`realization`) · **Kas?** tidak · **Balik?** pembalik + delta recognition

**FACT — piutang realisasi hanya diakui bila memang diakui.** `internal/charge/query.go:179-300`:

```sql
WHERE ci.tenant_id = ? AND ci.status = 'open' AND cg.status = 'open'
  AND ci.recognized_at IS NOT NULL
  AND ci.recognized_amount > 0
  AND COALESCE(ci.due_date, ci.recognized_due_date) IS NOT NULL
```

**INFERENCE:** inilah disiplin yang benar, dan inilah yang **tidak ada** di jalur `house`. Bandingkan langsung dengan §5.

**FACT:** live — hanya 2 dokumen `realization` (KWR) dan 13 `invoice`. 13 dokumen `invoice` **tidak** tertaut jurnal (0/13) — itu benar, invoice bukan pergerakan kas, jadi di luar cakupan INV-DOC-1.

---

### F. BAST (serah terima) — pengakuan pendapatan & HPP

- **Peristiwa:** unit diserahkan; pendapatan dan HPP diakui; uang muka direklas jadi piutang.
- **Jurnal (`buildEvent3Lines`, `internal/sale/service.go:643-670`):**
  Dr **2-2000 Uang Muka** (seluruh yang sudah diterima) · Dr **1-2000 Piutang** (`gross − advance`) · Cr **4-xxxx Pendapatan** · Cr **2-3000 PPN Keluaran** (bila PKP)
  ditambah Dr **5-1000 HPP** · Cr **1-3xxx Persediaan** (budgeted HPP, `p02-p03-hpp-snapshot`)
- **Dokumen:** tidak ada (tidak menyentuh kas) · **GL?** ya · **AR?** ya · **Kas?** tidak

**FACT — inilah satu-satunya titik lahir piutang harga rumah:**

```go
// internal/sale/service.go:663
// Dr Piutang (sisa yang belum dibayar, bruto jika PKP)
piutang := gross.Sub(advance)
```

Satu unit → **satu** baris piutang gelondongan. Tidak ada per-cicilan.

**FACT — gate "realisasi harus lunas" sudah dicabut.** Migrasi `000067` menghapus `tenants.require_realization_settled` setelah menulis baris audit lebih dulu. Sesuai D-3.

---

### G. Refund ke customer

- **Jurnal:** Dr Kewajiban terkait (2-2000 / 2-2100 / 2-2200 / 2-2400) · Cr Kas/Bank
- **Sumber:** `internal/cancellation/refund_service.go:164`, `internal/charge/service.go:819`
- **Dokumen:** **RFC** · **Kas?** keluar · **Balik?** pembalik + JR

**FACT:** RFC **tidak pernah dipilih otomatis**. `internal/ledger/document_issuer.go:194-198` menjelaskan alasannya: BTP dan RFC menyatakan uang itu **milik siapa**, dan itu tidak bisa disimpulkan dari arah debit/kredit. Ini desain yang benar dan saya konfirmasi tidak ada jalur yang menebaknya.

---

### H. Payout titipan ke pihak ketiga (bayar PDAM/notaris/dll)

- **Jurnal:** Dr **2-2400 / 2-2300 Titipan** · Cr Kas/Bank
- **Sumber:** `internal/charge/service.go:688`, `internal/notary/notary.go:256`
- **Dokumen:** **BTP** · **Kas?** keluar

**FACT:** ada **dua** jalur payout titipan — `charge` (2-2400, kanonik sejak K-1) dan `notary` (2-2300, warisan UAT Batch 2). Lihat §9 M-4.

---

### I. Impor Piutang Proyek Lama (W-7)

- **Peristiwa:** rincian atas saldo piutang yang **sudah ada** di buku besar sebagai saldo awal.
- **Jurnal:** **TIDAK ADA** (INV-LAR-4)
- **Sumber:** `internal/legacyar` · **Dokumen:** tidak ada · **GL?** ❌ · **AR?** ya (`legacy`) · **Kas?** tidak

**FACT — dibuktikan oleh test, bukan klaim:** `TestA_ImportMatchesLedger`, `TestB_DifferenceIsReportedNotFabricated`, `TestNoPartialImport`, `TestF_DuplicateBatchRejected`, `TestF2_DoubleCommitRejected` (`internal/legacyar/*_test.go`).

**FACT — risiko double-count nol secara struktural**, karena impor secara harfiah tidak menulis ke `journal_entries` sama sekali.

---

### J. Pelunasan Piutang Proyek Lama

- **Jurnal:** Dr Kas/Bank · Cr **1-2000** (akun kontrol dari master, `DefaultControlAccount`)
- **Sumber:** `internal/legacyar/payment.go:186`
- **Dokumen:** **BKM** (`ledger.DocCashIn`) · **Kas?** masuk
- **Balik (void):** jurnal cermin + dokumen **JR** yang menunjuk dokumen asli — `internal/legacyar/payment.go:328-345`

**FACT — void legacy TIDAK memakai `PostingService.Reverse`.** Ia membuat jurnal baru bergaris terbalik dengan `Source: SourceLegacyPayment`, sehingga `journal_entries.is_reversing = 0` dan `reverses_id = NULL`. Keterkaitannya hanya lewat `documents.reverses_document_id` dan `legacy_ar_payments.voids_payment_id`.

**INFERENCE (MEDIUM, M-5):** ini **sengaja dan hasilnya benar** untuk rekonsiliasi — karena source-nya tetap `legacy_ar_payment`, atribusi §5 tidak perlu bergantung pada aturan "pembalik mewarisi source". Tapi efek sampingnya: pembalik ini tidak terlihat sebagai pembalik oleh ledger, sehingga (a) UI jurnal menampilkan tombol "Buat Jurnal Pembalik" di atasnya, dan (b) guard `HasReversingEntry` tidak melindunginya (perlindungan datang dari `VoidExists` + unique key `voids_payment_id`). Perlindungannya ada, tapi letaknya di tempat yang berbeda dari jalur lain.

---

### K. Saldo Awal (Opening Balance)

- **Jurnal:** ya, `source = 'opening_balance'`, akun lawan **dipilih user dari COA** (tidak ada akun clearing hardcode)
- **Dokumen:** **DIKECUALIKAN** dari INV-DOC-1 — `internal/ledger/document_issuer.go:349`
- **GL?** ya · **AR?** tidak langsung (rinciannya lewat legacy) · **Kas?** bisa ya

**FACT — pengecualian ini satu-satunya yang aktif di data hidup.** Query jurnal kas terposting tanpa dokumen:

```
source           is_reversing   n
opening_balance  0              1
```

Tidak ada yang lain. INV-DOC-1 berlaku penuh.

---

### L. Historical Financial Snapshot (W-6)

- **Peristiwa:** admin mengetikkan angka Neraca & Laba Rugi tahun lampau.
- **Jurnal:** **TIDAK ADA** · **GL?** ❌ · **AR?** ❌ · **Kas?** ❌
- **Guard:** `internal/histfin/repository.go:165-177` — tahun yang **punya jurnal terposting** (di luar `opening_balance`) ditolak.

**FACT — guard-nya berbasis jurnal, bukan `go_live_date`.** Ini penting: tanggal go-live bisa diedit, jurnal terposting tidak. **FACT:** dikunci `TestIntegration_W6_TahunBerjurnalDitolak` dan `TestIntegration_W6_SnapshotTidakMenyentuhLedger`.

---

## 5. GL ↔ Subledger Reconciliation

Brief audit meminta saya memverifikasi:

> `GL 1-2000 = system receivable outstanding + realization receivable outstanding + legacy receivable outstanding`

dan secara eksplisit meminta saya **tidak** menganggapnya benar hanya karena test W-7 hijau. Saya mengujinya terhadap data, dan hasilnya:

### 5.1 Formula tersebut SALAH — di kedua arah

```
tenant_id  nama              gl_1_2000        house_sub       real_sub      legacy_sub     selisih
1          esaProperti Dev   3.500.000.000    0               0             0              +3.500.000.000
9900246    UAT Batch 2       0                200.000.000     0             0                -200.000.000
9901005    W5 UI Check       675.725.000      0               25.725.000    650.000.000              0
9900777    Legacy AR Test    100.000.000      0               0             100.000.000              0
```

Dua tenant pecah, dan **pecahnya ke arah berlawanan** — itu tanda bahwa ini bukan satu bug, melainkan dua definisi yang berbeda.

### 5.2 Akar masalah — FACT, dari kode

**Sisi sub-ledger** (`internal/reporting/repository.go:215-260`):

```sql
FROM payment_schedules ps
JOIN sale_contracts c ON c.id = ps.sale_contract_id AND c.tenant_id = ps.tenant_id
JOIN units u          ON u.id = ps.unit_id          AND u.tenant_id = ps.tenant_id
...
WHERE ps.tenant_id = ? AND ps.status <> 'superseded'
```

→ **setiap jadwal cicilan yang belum di-supersede**, termasuk unit yang belum BAST.

**Sisi buku besar** (`internal/sale/service.go:663`):

```go
piutang := gross.Sub(advance)   // hanya di buildEvent3Lines — yaitu HANYA saat BAST
```

→ 1-2000 baru bergerak **saat BAST**, sekali, gelondongan per unit.

Verifikasi silang: tenant `UAT Batch 2` punya unit 1638 dengan `bast_ada = 0` dan Rp 200.000.000 jadwal terbuka — muncul penuh di aging, nol di GL. Tenant `esaProperti Dev` punya Rp 3.5 M piutang hasil BAST tanpa satu pun baris rincian.

**INFERENCE (HIGH, H-1):** layar `/accounting/receivable` mencampur dua hal berbeda di bawah satu kata:

| | `house` | `realization` | `legacy` |
| --- | --- | --- | --- |
| Kapan masuk daftar | jadwal dibuat | **invoice diterbitkan** | diimpor sebagai rincian saldo GL |
| Kapan masuk GL | **BAST** | invoice diterbitkan | sudah ada (saldo awal) |
| Cocok dengan GL? | ❌ | ✅ | ✅ |

W-5 memasang disiplin ini untuk realisasi (`recognized_at IS NOT NULL`). W-7 memasangnya untuk legacy (impor = rincian atas saldo yang sudah ada). **`house` belum pernah didisiplinkan.**

Dan tenant yang cocok sempurna (`W5 UI Check`, `Legacy AR Test`) cocok justru karena **tidak punya piutang `house` sama sekali**. Kalau saya berhenti di test W-7 yang hijau, saya akan melaporkan rekonsiliasi ini sebagai LULUS.

### 5.3 Formula yang sebenarnya berlaku hari ini

```
GL(1-2000)  =  Σ (gross − advance) unit yang SUDAH BAST
             − Σ pembayaran house pasca-BAST
             + Σ charge_items yang recognized
             − Σ pembayaran realisasi
             + Σ legacy_receivables.original_amount
             − Σ legacy payments
             ± reklas T-3 dari 1-2200
```

Dari enam suku itu, **hanya empat** punya sub-ledger yang bisa dilihat orang. Dua suku pertama tidak.

### 5.4 Jawaban atas pertanyaan spesifik dalam brief

| Pertanyaan | Jawaban |
| --- | --- |
| Ada source piutang lain di luar formula? | **Ya.** `1-2100` dan `1-2200` ikut `RoleReceivable` (`account_role.go:47`) tapi tak punya sub-ledger. Live: tenant 10 memegang Rp 20 jt di 1-2200. |
| Alokasi pembayaran mengurangi source yang benar? | **Ya.** `payment_allocations.allocation_type` memisahkan `charge_item`/`charge_item_void`; legacy punya tabel pembayaran sendiri dengan `legacy_receivable_id` eksplisit; `TestG_AllocationNotSwapped` mengunci dua customer tidak tertukar. |
| Pembalik memulihkan atribusi source? | **Ya, untuk saldo awal.** Aturannya dipatok di `ledger.AccountMovementBySource` (pembalik dihitung pada source jurnal yang dibalik), dikunci `TestB3_ReversedOpeningBalanceStaysMatched`. Untuk void legacy, atribusi benar karena source-nya memang tidak berubah (§4-J). |
| Write-off yang belum ada bikin masalah ke depan? | **Belum sekarang** — `written_off` cuma status domain tanpa jurnal, jadi tak ada yang bocor. Tapi begitu write-off berjurnal, ia akan mengurangi GL tanpa mengurangi `original_amount`, dan §5.3 harus tumbuh satu suku lagi. Ini BUSINESS DECISION (BD-2, §19). |
| Saldo awal masuk rekonsiliasi dengan benar? | **Ya** — justru inilah yang membuat rekonsiliasi W-7 stabil. `LegacyReconciliationCard` membandingkan **porsi saldo awal** lawan **Σ nilai ASLI**, bukan saldo hidup lawan sisa tagihan. |

**Rekomendasi:** jangan "perbaiki formula" di §5. Perbaiki `house` (§18, Increment W-8).

---

## 6. Historical Snapshot / Opening Balance / Legacy AR

Ketiganya sering tertukar. Bedanya tegas di kode:

| | Histfin (W-6) | Opening Balance | Legacy AR (W-7) |
| --- | --- | --- | --- |
| Menulis `journal_entries`? | **Tidak** | **Ya** (`source='opening_balance'`) | **Tidak** (INV-LAR-4) |
| Sifat | angka laporan | saldo hidup | rincian atas saldo hidup |
| Cakupan | per tahun, berdiri sendiri | satu titik cutoff | per piutang |
| Pengaruh ke periode berjalan | tidak ada | ya (saldo pembuka) | tidak (hanya memberi nama pada saldo) |
| Dokumen | — | dikecualikan INV-DOC-1 | — (pelunasannya BKM) |

**FACT — tidak ada skenario double-count di antara ketiganya hari ini.**

1. Histfin tidak pernah menyentuh `journal_entries` — dikunci `TestIntegration_W6_SnapshotTidakMenyentuhLedger`.
2. Legacy AR tidak pernah menyentuh `journal_entries` — dikunci `TestA_ImportMatchesLedger`.
3. Hanya Opening Balance yang masuk GL, dan ia satu-satunya sumber angka untuk laporan tahun berjalan.

**FACT — guard histfin sengaja MENGIZINKAN tahun yang punya jurnal saldo awal.** `internal/histfin/repository.go:165-177` menghitung jurnal terposting **kecuali** `opening_balance`. **INFERENCE:** ini benar, dan penting: jurnal saldo awal biasanya bertanggal 31-12-2025 (akhir tahun terakhir historis), sehingga tanpa pengecualian ini snapshot 2025 akan ditolak justru pada kasus yang paling normal.

**INFERENCE (MEDIUM, T-3) — risiko yang belum ada, tapi akan ada.** Snapshot 2025 dan saldo awal per 31-12-2025 **menyatakan fakta yang sama dari dua sumber**. Selama tidak ada laporan yang menggabungkannya, tidak ada double count — dan hari ini tidak ada satu pun kode yang menggabungkannya (`histfin` dan `reporting` tidak saling impor; saya cek). Tapi begitu ada **Neraca Komparatif 2025 vs 2026**, siapa pun yang menyusunnya harus tahu bahwa kolom 2025 wajib datang dari histfin dan **tidak boleh** dari GL. Itu perlu ditulis sebagai aturan sebelum fiturnya dibangun, bukan sesudah.

**BUSINESS DECISION (EDGE-6, masih OPEN):** perlakuan Laba Rugi **tahun berjalan** ketika go-live jatuh di tengah tahun. Belum diputuskan; dicatat di `w6-historical-snapshot` dan tetap terbuka. Bukan blocker untuk increment berikutnya.

---

## 7. Document & Cash Audit

### 7.1 Inventaris jalur kas — dari kode, bukan dari dokumen

Brief meminta saya tidak memakai angka "11 jalur" dari dokumen sebagai fakta. Berikut hasil penelusuran call-site sebenarnya.

**Kas MASUK (7 jalur):**

| # | Jalur | Dokumen | Call site |
| --- | --- | --- | --- |
| 1 | Booking fee | KWB | `sale/booking_repo.go:122` |
| 2 | Cicilan/termin harga rumah | KWT | `sale/repository.go:700` |
| 3 | Pembayaran biaya realisasi | KWR | `billing/receipt_repository.go:163` |
| 4 | Pencairan KPR | KWD | `billing/receipt_repository.go:165` |
| 5 | Pelunasan piutang proyek lama | BKM | `legacyar/payment.go:186` |
| 6 | Terima titipan notaris | BKM | `notary/notary.go:178` |
| 7 | Jurnal manual / recurring kas masuk | BKM | `ledger.ManualDocumentChoices` → satu pilihan |

**Kas KELUAR (6 jalur):**

| # | Jalur | Dokumen | Call site |
| --- | --- | --- | --- |
| 8 | Realisasi biaya proyek / operasional | BKK | `cost/repository.go:209` |
| 9 | Pembayaran pajak | BKK | `tax/repository.go:98` |
| 10 | Pembayaran komisi | BKK | `commission/service.go:282` |
| 11 | Payout titipan realisasi ke vendor | BTP | `charge/service.go:688` |
| 12 | Payout titipan notaris | BTP | `notary/notary.go:256` |
| 13 | Refund ke customer | RFC | `cancellation/refund_service.go:164`, `charge/service.go:819` |
| 14 | Jurnal manual kas keluar | BKK / BTP / RFC (operator memilih) | `ledger.ResolveManualDocument` |

**Plus:** setiap pembalik jurnal kas → **JR** otomatis (`posting_service.go:397-410`), dan **saldo awal** → dikecualikan.

Jadi angkanya **13 jalur bisnis + 2 jalur lintas-domain (pembalik, saldo awal)**, bukan 11. Selisihnya karena dokumen lama tidak menghitung jalur notaris (2 jalur) sebagai jalur tersendiri.

### 7.2 Atomicity & rollback

**FACT — dokumen terbit SESUDAH jurnal jadi, di transaksi yang sama:**

```go
// internal/ledger/document_issuer.go:300-307
// Dipanggil sesudah MarkPosted dan bukan sebelumnya: dokumen bernomor tanpa
// transaksi yang berhasil termasuk yang dilarang INV-DOC-1, jadi nomor baru
// boleh terbit setelah jurnalnya benar-benar jadi — dan karena keduanya satu
// transaksi, kegagalan di langkah ini membatalkan jurnalnya juga.
```

**FACT — nomor tidak hangus saat rollback**, karena alokasi nomor ikut transaksi yang sama (`document.Allocate(tx, …)` menerima `tx`, bukan koneksi baru).

**FACT — pembayaran + kwitansi atomik:** `billing.CreateReceiptForTerminInTx` secara eksplisit "tidak membuka transaksi baru; kegagalan meng-rollback transaksi pemanggil" (`receipt_repository.go:130-135`).

### 7.3 Penegakan INV-DOC-1

**FACT — fail-closed, bukan opt-in.** `enforceDocument` (`document_issuer.go:344-391`) dipanggil dari **semua** jalur posting (`Post`, `PostDraft`, `CreateAndPost`, `Reverse`) dan mengevaluasi **keadaan akhir yang sudah terposting**, bukan niat pemanggil. Doc-comment-nya menjelaskan mengapa ini bukan seam `With…`: "satu yang lupa sudah cukup untuk melubangi invariant ini".

**FACT — `MarkPosted` tidak punya pemanggil di luar `PostingService`** (hanya `posting_service.go:207` dan `:393`). Tidak ada pintu belakang.

**FACT — terbukti pada data hidup:** dari seluruh jurnal kas terposting di semua tenant, hanya **satu** yang tidak punya dokumen — dan itu `opening_balance`, pengecualian yang disengaja.

**INFERENCE (LOW, L-1) — satu vektor fail-open tersisa.** Deteksi "ini pergerakan kas" bergantung pada `AccountInRole(acc, RoleCashBank)`, dan `RoleCashBank` sengaja **bukan** berbasis kode akun melainkan `accounts.category` yang **bisa diedit admin** (`account_role.go`, keputusan `coa-driven-cash-bank`). Konsekuensinya: akun bank yang salah dikategorikan sebagai `other_asset` akan lolos dari INV-DOC-1 tanpa suara. Keputusan COA-driven itu sendiri benar — yang kurang adalah alarm ketika kategori akun kas/bank diubah.

### 7.4 Audit detektif

**FACT** — `internal/document/audit.go` menyediakan pemeriksaan terpisah dari penjaga preventif: jurnal kas tanpa dokumen (`:149`), dokumen tanpa jurnal (`:203`), dan **satu dokumen dipakai >1 jurnal** (`:278-283`). Ini pola yang benar: penjaga di jalur tulis, detektif di jalur baca.

---

## 8. Numbering Audit

**FACT — hanya ada satu mesin penomoran.** Semua penerbitan nomor bermuara ke `document.Allocate(tx, tenantID, typeCode, issuedAt)`:

- Kwitansi customer: `billing/receipt_repository.go:273-275` → `document.Allocate` (bukan generator sendiri)
- Jurnal & bukti kas: `document.IssueForJournal` → `Allocate`
- Pembalik: `document.IssueForJournal(… TypeJournalReversal …)` (`journal_link.go:113`)

**FACT — tidak ada increment langsung.** Tidak ditemukan `UPDATE … SET next_val`/`last_val` di luar `internal/document`.

**FACT — tidak ada prefix hardcode di luar Document Domain.** Sebelas tipe/prefix (KWT, KWB, KWR, MTI, INV, KWD, BKM, BKK, BTP, RFC, JR) semuanya didefinisikan di `internal/document/seed.go:17-44` dan tersimpan di tabel `document_types` (tenant bisa menambah tipe custom).

**FACT — reset tahunan forward-only** dan `fiscal_year` = **waktu terbit**, bukan tanggal transaksi:

```go
// internal/ledger/document_issuer.go:325-327
// issuedAt = SEKARANG, bukan entry.Date. Aturan W-2 (lihat fiscalYearFor):
// nomor dokumen adalah identitas lembar bukti, dan seri tahun berjalan tidak
// boleh disusupi dokumen yang tanggal transaksinya mundur ke tahun lalu.
```

### Tabel sequence lama — masih ada, sudah mati (LOW, C-1)

**FACT:**

```
document_sequences   27 baris   ← HIDUP
invoice_sequences     6 baris   ← tidak dirujuk kode Go mana pun
receipt_sequences    12 baris   ← tidak dirujuk kode Go mana pun
```

Model Go-nya sudah dihapus saat W-2 (`billing/receipt.go:62`, `billing/model.go:57` mencatatnya). Yang tersisa hanya tabel dengan counter basi.

**INFERENCE:** tidak berbahaya sekarang (tidak ada yang membacanya), tapi berbahaya bagi **manusia**: DBA yang melihat `receipt_sequences` berisi angka akan wajar menyimpulkan itu sumber nomor kwitansi. **Tidak saya hapus** (§15). Rekomendasi: migrasi `DROP TABLE` terpisah dengan pencatatan nilai terakhir.

---

## 9. Master Data Audit

### M-1 (MEDIUM) — AccountRoleRegistry ada, tapi bukan satu-satunya pintu

**FACT:** `internal/ledger/account_role.go` mendefinisikan peran→kode secara kanonik:

```go
var roleCodes = map[AccountRole][]string{
	RoleReceivable:       {"1-2000", "1-2100", "1-2200"},
	RolePayable:          {"2-1000"},
	RoleBookingLiability: {"2-2100"},
	RoleInventory:        {"1-3000", "1-3100", "1-3200", "1-3300"},
	…
}
```

**FACT:** meski begitu, kode akun literal masih tersebar di **22 file** di luar registry:

```
9× internal/sale/service.go        6× internal/sale/collection.go
6× internal/domain/cost_category.go 5× internal/project/product_type.go
5× internal/cancellation/service.go 4× internal/commission/service.go
4× internal/closing/service.go      3× internal/tax/service.go
3× internal/sale/booking.go         2× internal/scheme/service.go … dst
```

**INFERENCE:** mayoritas ini adalah **default yang sah** — mis. `charge.AccountCodeTitipanRealisasi = "2-2400"` dan `legacyar.DefaultControlAccount = "1-2000"` adalah konstanta bernama dengan komentar, dan keduanya bisa di-override (legacy AR menerima akun kontrol dari input). Yang jadi masalah bukan keberadaannya, melainkan bahwa **registry tidak wajib dilewati**, sehingga penulis kode berikutnya tidak punya sinyal bahwa registry itu ada.

### M-2 (MEDIUM) — akun ber-`RoleReceivable` tanpa sub-ledger

Sudah dibahas di §4-C dan §5.4. `1-2100` dan `1-2200` masuk peran piutang tapi tak punya representasi di aging.

### M-3 (LOW) — `"opening_balance"` hidup di tiga tempat

**FACT:** kanonik di `document/backfill.go:45`, alias di `ledger/document_issuer.go:349`, salinan sengaja di `histfin/repository.go:177`. Alasannya ditulis eksplisit (§2). Bukan kelalaian — tapi kalau nilainya berubah, tiga tempat harus ikut.

### M-4 (MEDIUM) — dua jalur titipan hidup berdampingan

**FACT:** `2-2300 Titipan Notaris` (UAT Batch 2 §3, `ledger/coa.go:57`) dan `2-2400 Titipan Biaya Realisasi` (K-1, `ledger/coa.go:64`). Paket `internal/notary` masih punya jalur terima+payout sendiri (`notary.go:178`, `:256`) terpisah dari `internal/charge`.

**INFERENCE:** notaris kini termasuk **jenis biaya realisasi** menurut koreksi klien 2026-08-06 (`produk-vs-titipan-realisasi`) dan sudah ada sebagai master (`charge_type.go:318`). Jadi `internal/notary` adalah jalur warisan yang masih hidup. W-1 menutup **intake**-nya (`410 Gone`) tapi paketnya masih menerima dan membayar. Perlu diputuskan: apakah data notaris lama dimigrasikan ke `charge` (2-2400) atau `2-2300` dibiarkan sebagai akun historis yang membeku. → §19 BD-4.

### M-5 (MEDIUM) — nama bank di-hardcode di pratinjau pembayaran

**FACT** — `internal/sale/collection.go:97-118`:

```go
func accountLabel(code string) string {
	switch code {
	case "1-1100": return "Kas"
	case "1-1300": return "Bank — BCA"
	case "1-1400": return "Bank — Mandiri"
	case "1-1500": return "Bank — BRI"
	…
```

**INFERENCE:** ini bertentangan langsung dengan keputusan `coa-driven-cash-bank` (semua opsi kas/bank harus datang dari COA, `BANK_OPTIONS` dihapus). Tenant yang menamai `1-1300` sebagai "Bank BNI" akan melihat **"Bank — BCA"** di pratinjau pembayaran. Salah di layar operator, saat memutuskan uang masuk ke mana. Perbaikannya sepele: baca `accounts.name`.

### Hardcode yang SAH (bukan temuan)

- `internal/charge/charge_type.go:318-321` — notaris/BPHTB/PDAM/listrik sebagai **seed default** master jenis biaya. Ini data awal yang bisa diedit tenant, bukan logika. Benar.
- `internal/ledger/coa.go` — seed bagan akun. Benar.
- `internal/document/seed.go` — seed tipe dokumen. Benar.
- Nama jenis biaya di **komentar** (`receivable.go:66`, `charge/model.go:43`) — dokumentasi, bukan logika.

**FACT:** tidak ditemukan satu pun `switch` atau `if` **logika bisnis** yang bercabang berdasarkan nama jenis biaya (`"pdam"`, `"notaris"`, dst). W-1 memang menutup itu.

---

## 10. Reversal Audit

### R-1 (HIGH) — endpoint pembalik generik melewati pemilik sub-ledger

**FACT** — `internal/ledger/handler.go:121`:

```go
r.With(auth.RequireWrite()).Post("/reverse", h.reverseJournal)
```

Handler-nya (`:597-632`) hanya memvalidasi tenant, id, dan format tanggal, lalu memanggil `posting.Reverse`. **Tidak ada** filter `source`, tidak ada pengecekan siapa pemilik jurnal itu.

**FACT** — UI mengeksposnya untuk **semua** jurnal terposting (`frontend/components/accounting/JournalDetail.tsx:274-281`):

```tsx
{isPosted && !journal.is_reversing && (
  <button onClick={() => setShowReverseModal(true)}>Buat Jurnal Pembalik</button>
)}
```

Satu-satunya syarat: sudah diposting dan bukan pembalik. `source` tidak dilihat.

**INFERENCE:** admin bisa membuka `/accounting/jurnal/{id}` untuk jurnal pelunasan piutang lama, menekan "Buat Jurnal Pembalik", dan hasilnya:

- GL 1-2000 **naik** kembali sebesar pembayaran itu ✅ (benar dari sisi ledger)
- `legacy_ar_payments` **tidak berubah** — sub-ledger tetap menyatakan lunas ❌
- `LegacyARDetail` tetap menampilkan pembayaran itu aktif dengan tombol "Batalkan" ❌
- Rekonsiliasi §5 pecah, dan **tidak ada alarm** ❌

Efek yang sama berlaku untuk jurnal BAST (unit tetap `handed_over`), pembayaran house (`payment_allocations` utuh), dan invoice realisasi (`charge_receivable_recognitions` utuh).

Yang membuat ini HIGH dan bukan MEDIUM: setiap domain **sudah punya** jalur pembatalan yang benar (`legacyar.VoidPayment`, `cancellation`, `charge` void) — jalur generik ini bukan satu-satunya cara, ia hanya cara yang **lebih mudah ditemukan** oleh admin di layar jurnal.

**Catatan:** ini bukan regresi W-7. Endpoint ini sudah ada sejak lama; W-5/W-7 justru yang menambah jumlah sub-ledger yang bisa dirusaknya.

### R-2 (MEDIUM) — `reverseWithin` tidak memeriksa periode tutup

**FACT:** `PostingService` punya `PeriodChecker` (`posting_service.go:58-96`), dan ia dipakai **tepat satu kali**, di jalur pembuatan jurnal (`:133-134`). `reverseWithin` (`:345-410`) memanggil `FindByID` → `IsPosted` → `HasReversingEntry` → `Create` → `MarkPosted` **tanpa** menyentuh `s.periods`.

**INFERENCE:** jurnal pembalik bisa diposting ke dalam periode yang sudah ditutup, padahal jurnal baru ditolak. Bisa jadi ini disengaja (koreksi kadang memang harus masuk periode lama) — tapi kalau begitu, tanggal pembaliknya seharusnya dipaksa ke periode terbuka, bukan dibiarkan bebas. Saat ini `reverseDate` datang mentah dari request body.

### R-3 — pembalik mewarisi source (BENAR, bukan temuan)

**FACT:** aturan ini dipatok di `ledger.AccountMovementBySource`, bukan di pemanggilnya, dan dikunci `TestB3_ReversedOpeningBalanceStaysMatched`. Alasannya tepat: ledger append-only, membalik adalah satu-satunya koreksi sah; kalau pembalik jatuh ke kelompok `reversal`, koreksi yang **benar** justru membuat porsi saldo awal tampak kelebihan selamanya.

### R-4 (MEDIUM) — void legacy tidak ber-flag `is_reversing`

Sudah dibahas di §4-J. Konsekuensi praktis: jurnal void legacy masih menampilkan tombol "Buat Jurnal Pembalik" di UI (karena `!journal.is_reversing` bernilai true), sehingga admin bisa membalik pembaliknya. R-1 dan R-4 bertemu di sini.

### R-5 — proteksi double-reversal

**FACT:** jalur generik dijaga `HasReversingEntry` → `ErrJournalAlreadyReversed` → HTTP 409. Jalur legacy dijaga `VoidExists` + unique key pada `voids_payment_id` (`isDuplicateKey` → `ErrPaymentVoided`). Keduanya terjaga, dengan mekanisme berbeda. Dikunci `TestVoidPaymentReverses` dan `TestPaymentIdempotency`.

### R-6 — nomor dokumen historis tidak berubah saat pembalikan

**FACT:** pembalik menerbitkan dokumen **JR baru** yang menunjuk dokumen asli lewat `documents.reverses_document_id` (`journal_link.go:113`, `document_issuer.go:328-334`). Dokumen asli tidak disentuh. Live data: 0 dokumen dengan `reverses_document_id` terisi — konsisten, karena satu-satunya pembalik di DB ini membalik jurnal saldo awal yang **tidak** menyentuh kas, dan `reversalDocument` sengaja melewati jurnal non-kas.

---

## 11. Migration Audit

**FACT:** 69 migrasi, **semua** punya pasangan `.down.sql`. Tidak ada yang saya jalankan atau rollback.

| Migrasi | Isi | Catatan |
| --- | --- | --- |
| `000063` | `charge_items.deposit_account_code` (snapshot per item) + backfill + `charge_settlement_lines` + generated column `charge_group_live` | Backfill mengisi dari master; snapshot di item = benar (master boleh berubah, item tidak) |
| `000064` | DROP + re-ADD generated column & index, agar `'paid'` ikut dikecualikan | ⚠️ lihat O-1 |
| `000065` | Document Numbering Engine (`document_sequences`, `last_val`) | `last_val` bukan `next_val` — benar, nilai terakhir yang terbit tidak ambigu |
| `000066` | `journal_entries.document_id` + `UNIQUE uk_je_document`, `documents.reverses_document_id`, 6 tipe dokumen via `INSERT IGNORE` | `INSERT IGNORE` = idempoten, aman di-rerun |
| `000067` | kolom `recognized_*`, tabel `charge_receivable_recognitions` (append-only), **DROP** `tenants.require_realization_settled` | Baris audit ditulis **sebelum** drop; `down` mengembalikan kolom dengan default OFF |
| `000069` | `legacy_ar_batches` / `legacy_receivables` / `legacy_ar_payments`, unique `committed_hash`, CHECK constraints | Kolom staging bertipe string mentah — sesuai aturan "nominal Excel sebagai string" |

**FACT — CLAUDE.md dipatuhi:** setiap tabel baru punya `id BIGINT UNSIGNED AUTO_INCREMENT PK`, `tenant_id BIGINT UNSIGNED NOT NULL`, `created_at/updated_at DATETIME(3)`, dan index pada `tenant_id`. Semua kolom uang `DECIMAL(20,4) NOT NULL DEFAULT '0.0000'`.

**FACT — unique key W-7 memakai `committed_hash`, bukan `file_hash`.** Artinya berkas yang di-upload lalu dibatalkan (draft) tidak memblokir upload ulang; hanya batch yang **sudah di-commit** yang menolak duplikat. Benar.

### O-1 (LOW) — churn generated column 000063→000064

**FACT:** `000063` membuat generated column + unique index di tabel `invoices`; `000064` langsung men-`DROP` dan membuatnya lagi dengan definisi berbeda.

**INFERENCE:** pada MySQL, menambah/menghapus generated column + index pada tabel besar adalah operasi rebuild yang mengunci. Di tenant dengan ratusan ribu invoice, dua migrasi berurutan ini berarti dua kali rebuild. Bukan bug — catatan operasional untuk deploy pertama ke data produksi besar: jadwalkan di jendela maintenance, dan `000063` boleh digabung ke `000064` **hanya** kalau belum pernah dijalankan di lingkungan mana pun.

### FK & tenant isolation

**FACT:** W-7 memakai **logical FK** (kolom id tanpa constraint) — keputusan sadar yang tercatat di `fe2-payment-allocations`, agar impor tidak terikat pada urutan tabel dan agar hapus-tenant tidak terhalang. Konsistensinya dijaga di service + test, bukan di DB.

**INFERENCE (LOW):** trade-off yang bisa diterima, tapi berarti integritas referensial legacy AR **sepenuhnya bergantung pada kode**. Kalau nanti ada tooling perbaikan data langsung ke DB, tidak ada jaring pengaman.

---

## 12. Test Coverage & Gaps

**FACT:** `go test ./...` hijau; suite integrasi `legacyar`/`ledger`/`receivable` hijau (`ok esaproperti/internal/legacyar 12.746s`, `ledger 0.546s`, `receivable 0.005s`). **Saya tidak melaporkan itu sebagai bukti kebenaran** — berikut isinya.

Distribusi (jumlah `func Test*`): `sale` 138 · `billing` 68 · `cost` 57 · `ledger` 54 · `allocation` 47 · `document` 42 · `tax` 42 · `project` 34 · `budget` 35 · `domain` 32 · `charge` 28 · `legacyar` 24 · `reporting` 23 · `histfin` 15 · `tenant` 15 · `scheme` 14 · `approval` 10 · `customer` 6 · `receivable` 6 · `salesorg` 5 · `cancellation` 4 · `commission` 4 · `notary` 3 · `closing` 3 · `crm` 0 · `platform` 0.

### 12.1 Invariant yang PUNYA test sungguhan

| Invariant | Test | Menguji perilaku? |
| --- | --- | --- |
| INV-LAR-4 (impor tanpa jurnal) | `TestA_ImportMatchesLedger` | ✅ menghitung jurnal sebelum/sesudah |
| Selisih dilaporkan, tidak dikarang | `TestB_DifferenceIsReportedNotFabricated` | ✅ |
| Akun lawan dipilih user | `TestB2_OpeningJournalUsesUserChosenCounter` | ✅ |
| Pembalik mewarisi source | `TestB3_ReversedOpeningBalanceStaysMatched` | ✅ menguji aturan, bukan implementasi |
| Overpayment ditolak (penuh & parsial) | `TestE_…`, `TestE2_OverpaymentAfterPartial` | ✅ dua jalur |
| Impor duplikat ditolak | `TestF_DuplicateBatchRejected`, `TestF2_DoubleCommitRejected` | ✅ |
| Alokasi tidak tertukar antar customer | `TestG_AllocationNotSwapped` | ✅ |
| Legacy tampil di SATU laporan piutang | `TestH_LegacyAppearsInOneReceivableReport` | ✅ |
| Tidak ada impor sebagian | `TestNoPartialImport` | ✅ rollback |
| Akun penerimaan wajib kas/bank | `TestCashAccountMustBeCashOrBank` | ✅ |
| Idempotensi pembayaran | `TestPaymentIdempotency` | ✅ |
| Isolasi tenant legacy | `TestTenantIsolation` | ✅ |
| Snapshot tak menyentuh ledger | `TestIntegration_W6_SnapshotTidakMenyentuhLedger` | ✅ |
| Tahun berjurnal ditolak | `TestIntegration_W6_TahunBerjurnalDitolak` | ✅ |
| Snapshot per tahun berdiri sendiri | `TestIntegration_W6_TahunIndependen`, `TestPerTahunBerdiriSendiri` | ✅ |
| Neraca seimbang wajib ada laba | `TestComputeTotals_SeimbangTanpaLabaAdalahTidakSeimbang` | ✅ bagus — menguji hal yang mudah salah |
| Total aging = Σ per sumber | `TestBuildAging_TotalSamaDenganJumlahPerSumber` | ✅ |
| Atomicity recurring journal | `internal/ledger/recurring_atomicity_integration_test.go` | ✅ pakai goroutine |

### 12.2 Invariant yang TIDAK punya test — dan berisiko

| # | Invariant tanpa test | Risiko |
| --- | --- | --- |
| **G-1** | **GL 1-2000 == Σ sub-ledger** | **HIGH** — tidak ada satu pun test yang membandingkan buku besar dengan daftar piutang. Kalau ada, H-1 (§5) akan ketahuan sejak hari pertama. Ini gap test paling mahal di sistem ini. |
| **G-2** | Membalik jurnal milik sub-ledger lewat endpoint generik | **HIGH** — R-1 tidak akan tertangkap test mana pun |
| **G-3** | Pembalik ke periode tertutup | MEDIUM — R-2 |
| **G-4** | INV-DOC-1 saat kategori akun kas diubah admin | MEDIUM — L-1 |
| **G-5** | Bayar ke jenis biaya master yang **non-aktif** | MEDIUM — brief menyebut ini eksplisit; saya tidak menemukan test `inactive master` di `charge` |
| **G-6** | Impor legacy konkuren (dua admin, berkas sama, bersamaan) | MEDIUM — `TestF2_DoubleCommitRejected` menguji dua commit berurutan, bukan bersamaan. Unique key `committed_hash` kemungkinan besar menyelamatkan, tapi belum dibuktikan. |
| **G-7** | Isolasi tenant untuk `histfin` sudah ada; untuk `document`/`reporting` **tidak** | MEDIUM |
| **G-8** | Nominal Excel malformed ekstrem (spasi non-breaking, tanda kurung negatif, notasi ilmiah) | LOW — `TestParseAmount` ada, cakupannya belum saya verifikasi lengkap |

**FACT — hanya 3 file test memakai konkurensi** (`sale/credit_application_integration_test.go`, `ledger/recurring_atomicity_integration_test.go`, `sale/payment_allocation_integration_test.go`). Untuk sistem dengan alokasi uang dan nomor dokumen berurutan, itu tipis.

**FACT — `internal/receivable` punya 6 test, semuanya unit murni, 0 integrasi.** Untuk paket yang memegang satu-satunya mesin aging, saya berharap ada minimal satu test integrasi yang memuat ketiga sumber sekaligus dari DB sungguhan. `TestH_LegacyAppearsInOneReceivableReport` di `legacyar` sebagian menutupi ini, tapi dari sisi legacy saja.

### 12.3 Test yang hanya membuktikan detail implementasi

- `TestGetARAging_DelegatesToReader` dan `TestGetARAging_NotConfigured` (`internal/reporting`) — menguji bahwa fungsi memanggil kolaboratornya. Ini mengunci struktur, bukan perilaku; akan pecah saat refactor yang benar dan tidak akan pecah saat angkanya salah. Bukan bahaya, tapi jangan dihitung sebagai cakupan.
- `TestTemplateIsNotSelfImporting` (`legacyar`) — justru **kebalikannya**: kelihatan sepele, tapi menguji hal nyata (template contoh tidak boleh lolos sebagai data sungguhan). Bagus.

---

## 13. Frontend/UX Audit

Catatan metode: kedua dev server dihentikan pada pembersihan sesi sebelumnya dan ekstensi Chrome terakhir dilaporkan terputus, sehingga bagian ini berbasis **pembacaan kode frontend**, bukan runtime. Sesuai memori `frontend-runtime-fix`, saya **tidak** mengklaim apa pun di sini sebagai "terverifikasi berjalan". Temuan F-1 dan F-2 di bawah bersifat FACT (terbaca dari kode), bukan hasil pengamatan UI.

### F-1 (HIGH) — kata "Piutang" dipakai untuk dua hal berbeda

**FACT** — `frontend/components/accounting/ARAgingView.tsx:378`:

> "Piutang muncul dari **jadwal pembayaran** (DP, termin, pelunasan) pada kontrak penjualan, dan dari tagihan biaya realisasi."

Copy ini **akurat menggambarkan kode** — dan itulah masalahnya. Layar bernama "Piutang" dengan `SummaryCard label="Total Piutang"` (`:120`) menampilkan angka yang, untuk sumber `house`, **bukan piutang menurut buku besar** (§5).

**INFERENCE:** akuntan yang membandingkan "Total Piutang" di layar ini dengan Neraca akan menemukan selisih dan tidak punya cara menjelaskannya dari dalam aplikasi. Perbaikan copy saja tidak cukup — tapi copy adalah bagian dari perbaikannya.

### F-2 (MEDIUM) — tombol "Buat Jurnal Pembalik" muncul di jurnal milik sub-ledger

**FACT** — `JournalDetail.tsx:274-281` (dibahas di §10 R-1). Ini sisi UX dari R-1: aksi yang secara akuntansi berbahaya ditampilkan sebagai tombol biasa, tanpa peringatan bahwa jurnal ini dimiliki domain lain.

### Yang sudah benar (diverifikasi lewat kode)

**FACT — LegacyARDetail jujur soal akuntansinya** (`LegacyARDetail.tsx:157-160`):

> "Impor tidak membuat jurnal piutang — saldonya sudah ada di buku besar sebagai saldo awal. Yang berjurnal hanya pelunasan di bawah."

Ini persis yang harus dibaca admin. Begitu juga teks konfirmasi pembatalan (`:305-308`): "Pembayaran tidak dihapus. Sistem membuat jurnal pembalik dan sisa tagihan kembali naik — riwayatnya tetap utuh supaya bisa dijelaskan saat diaudit."

**FACT — empty state informatif + CTA** (`ARAgingView.tsx:331-349`, `LegacyARDetail.tsx:169-172`), sesuai `feedback-erp-business-flow`.

**FACT — format uang & tanggal lewat komponen bersama** (`<Rupiah>`, `<Tanggal>`), tidak ada `toLocaleString` liar. `LegacyARDetail.tsx:333-335` bahkan mencatat alasannya: API mengirim stempel waktu penuh, yang dibaca admin harus tanggal.

**FACT — nol warna Tailwind mentah** di komponen W-7 yang saya baca; semua lewat token (`text-text-primary`, `bg-surface`, `text-danger`), sesuai `design-system-warm-ledger`.

**FACT — tidak ada detail teknis bocor ke toast.** `internal/legacyar/errors.go:5-9` menjelaskan bahwa pesan error sengaja tanpa prefiks nama paket karena sampai ke layar admin apa adanya. Semua 18 pesan berbahasa Indonesia dan operasional ("berkas ini sudah pernah diimpor", bukan "duplicate key violation").

### F-3 (MEDIUM) — nama bank salah di pratinjau pembayaran

Sisi UI dari M-5 (§9). Operator melihat "Bank — BCA" untuk akun `1-1300` apa pun namanya di COA tenant.

---

## 14. Security / Tenant Isolation

**FACT — CLAUDE.md Invariant #6 tidak sesuai kode.** Invariant menyatakan isolasi lewat "GORM global scope". Pencarian `Scopes(`, `globalScope`, `TenantScope`, `RegisterCallback` **tidak menemukan global scope di mana pun**. Isolasi sebenarnya dilakukan dengan `Where("tenant_id = ?")` eksplisit per query.

**INFERENCE (MEDIUM, T-1):** implementasinya **lebih aman dari kelihatannya** (eksplisit > implisit, dan tidak bisa dimatikan tanpa terlihat), tapi dokumen invariant-nya salah. Dokumen yang salah tentang mekanisme keamanan lebih berbahaya daripada tidak ada dokumen: reviewer berikutnya akan mengira ada jaring pengaman otomatis dan tidak memeriksa query barunya. **Perbaiki CLAUDE.md agar cocok dengan kode**, bukan sebaliknya.

**FACT — sapuan sistematis atas seluruh `Raw(`/`Exec(`** menemukan hanya **dua** query tanpa `tenant_id`, keduanya sah:

- `internal/billing/repository.go:350` — `SELECT … FROM tenants WHERE id = ?`
- `internal/charge/query.go:371` — idem

Tabel `tenants` memang di-key oleh `id` itu sendiri.

**FACT (LOW, L-2) — satu query tanpa defense-in-depth.** `internal/ledger/query.go:399-440`: `lineQ` di-scope tenant dengan benar (`journal_lines.tenant_id = ?`), tetapi query lanjutannya di `:438` adalah `Where("id IN ?", ids).Find(&entries)` tanpa filter tenant. **Aman secara turunan** — `ids` mustahil berisi id lintas tenant karena berasal dari query yang sudah di-scope. Tapi kalau suatu saat `lineQ` diubah, kebocorannya tidak akan tertangkap di tempat ini.

**FACT — otorisasi:** semua endpoint tulis W-1..W-7 memakai `auth.RequireWrite()`. Tidak ditemukan endpoint yang mengambil `tenant_id` dari body request alih-alih dari token — semua lewat `tenantIDFrom(r)`.

**FACT — tidak ada IDOR yang saya temukan:** setiap `FindByID` di `legacyar`, `histfin`, `charge`, `document` menerima `tenantID` sebagai parameter wajib, bukan opsional.

**GAP (G-7, §12):** test isolasi tenant ada untuk `legacyar`, `histfin`, `ledger`, `billing`, `project`, `sale`, `cost`, `tax`, `salesorg`, `allocation`, `tenant` — **tidak ada** untuk `document` dan `reporting`. `reporting` adalah paket yang paling banyak menulis SQL mentah lintas tabel; ia justru yang paling butuh.

---

## 15. Cleanup Audit

**Tidak ada yang saya hapus.** Berikut inventarisnya untuk keputusan Anda.

| # | Artefak | Status | Rekomendasi |
| --- | --- | --- | --- |
| C-1 | Tabel `invoice_sequences` (6 baris), `receipt_sequences` (12 baris) | Mati — tidak dirujuk kode Go | Migrasi `DROP TABLE` tersendiri, catat nilai terakhir di komentar migrasi |
| C-2 | `backend/cmd/backfill-allocations` | One-shot, sudah dijalankan (FE-2 P3) | Arsipkan atau beri header "sudah dijalankan, jangan ulang" |
| C-3 | `backend/cmd/backfill-cash-documents` | One-shot W-3 | idem |
| C-4 | `backend/cmd/backfill-recognition` | One-shot W-5 | idem |
| C-5 | `backend/cmd/audit-cash-documents` | **Bukan** one-shot — alat audit INV-DOC-1 | **Pertahankan** |
| C-6 | `backend/cmd/demo-seed`, `cmd/reset-demo` | Alat demo | Pertahankan, tapi pastikan tidak ter-build di image produksi |
| C-7 | `backend/tmp/build-errors.log` | Sampah build | Tambahkan `backend/tmp/` ke `.gitignore` |
| C-8 | Tenant uji di DB dev: `esaProperti Dev`(1), `UI Smoke PS1`(10), `UAT Batch 2`(9900246), `Legacy AR Test`(9900777), `W5 UI Check`(9901005) | Hanya di DB dev | Biarkan di dev; pastikan tidak ikut ke produksi |
| C-9 | `docs/` — beberapa dokumen desain menggambarkan keadaan sebelum koreksi (mis. R4 Opsi A yang sudah OBSOLETE karena `booking-fee-revenue-rule`) | Menyesatkan pembaca baru | Beri banner "SUPERSEDED oleh …" di kepala dokumen, jangan hapus |

**FACT:** scaffolding dev dari sesi sebelumnya (route `dev-session`, `cmd/mktok`, token sementara) **sudah bersih** — tidak ditemukan di kode maupun di `internal/platform`.

**FACT:** tidak ada `.env` dengan kredensial produksi di repo; yang ada `.env` lokal + `.env.example` di tiga lokasi.

---

## 16. Business / Accounting Gap Analysis

### A. COMPLETED — sudah selesai dan terbukti

W-1 master jenis biaya realisasi · W-2 Document Numbering Engine · W-3 INV-DOC-1 · W-4 kepemilikan piutang tunggal · W-5 J-14a piutang realisasi · W-6 snapshot historis · W-7 piutang proyek lama · booking fee sebagai pendapatan · HPP budgeted (PSAK 44) · cost 3-tier · payment scheme · tax rule konfiguratif · approval workflow · PPh Final Pengalihan (42 test) · closing/tutup buku tahunan · COA management · jurnal manual · recurring journal · period management.

**Catatan atas daftar 16 fitur di brief:** beberapa yang disebut "belum selesai" sebenarnya **sudah ada**. **FACT:** `PeriodManager.tsx` + `internal/closing` (Period Closing), `JournalCreateForm.tsx` + `POST /ledger/journals` (Manual Journal), `COAManager.tsx` (COA Management), `RecurringJournalView.tsx` + `recurring_atomicity_integration_test.go` (Recurring Journal), `internal/tax` dengan `RateCodePPhFinalPengalihan` (PPh Final), `CustomerStatementView.tsx` (Customer Statement), `billing/print_test.go` + `receipt_print.go` (Kwitansi cetak), `OpeningBalanceForm.tsx` (Opening Balance UX). Yang benar-benar **tidak ada**: Bank Reconciliation (0 rujukan), Fixed Asset (roadmap-only), Allowance/write-off legacy.

### B. SAFE / NO ACTION

- `2-2300` vs `2-2400` berdampingan — aman selama data lama tidak dipindah diam-diam (tapi lihat BD-4)
- Logical FK di W-7 — trade-off sadar
- `opening_balance` dikecualikan dari INV-DOC-1 — benar, saldo awal bukan pergerakan kas nyata
- `1-2200` di luar aging customer — kemungkinan benar secara akuntansi (§4-C)
- Duplikasi konstanta `"opening_balance"` — disengaja, terdokumentasi

### C. TECHNICAL DEBT

T-1 CLAUDE.md #6 salah menggambarkan mekanisme isolasi tenant · T-2 reporting menulis SQL di atas tabel milik `sale` · T-3 aturan snapshot-vs-GL untuk laporan komparatif belum ditulis · C-1..C-9 (§15) · M-1 registry peran akun bukan pintu wajib · O-1 churn migrasi 000063/64 · G-1..G-8 gap test (§12).

### D. ACCOUNTING GAP — benar-benar kurang secara akuntansi

| Gap | Dampak |
| --- | --- |
| **`house` tidak punya sub-ledger piutang** (H-1) | Daftar piutang tidak bisa direkonsiliasi ke Neraca. **Ini yang paling mahal.** |
| **Pembalik generik melewati sub-ledger** (R-1) | GL dan sub-ledger bisa berpisah tanpa alarm |
| Bank Reconciliation belum ada | Saldo bank di buku tidak pernah diadu dengan rekening koran |
| Allowance / write-off piutang | Piutang macet tidak bisa dicadangkan; Neraca menyatakan piutang lebih besar dari yang tertagih |
| PPN end-to-end (SPT keluaran/masukan) | Ada akun `2-3000` dan perhitungan di BAST, tapi bukan siklus pelaporan penuh |
| PPh 23 / PPh 21 | Belum ada |
| Fixed Asset | Belum ada; aset developer (kantor, kendaraan) tidak disusutkan |

### E. BUSINESS DECISION REQUIRED

Lihat §19.

### F. NEXT FEATURE CANDIDATES

Diurutkan sesuai kriteria brief (kebenaran akuntansi → kebutuhan operasional → pelaporan → kepatuhan → risiko → effort):

| Prioritas | Kandidat | Kebenaran akuntansi | Operasional | Effort |
| --- | --- | --- | --- | --- |
| **1** | **W-8: Sub-ledger piutang harga rumah + rekonsiliasi GL** | ⭐⭐⭐ | ⭐⭐⭐ | sedang |
| **2** | **W-9: Kedaulatan pembalikan (R-1/R-2/R-4)** | ⭐⭐⭐ | ⭐⭐ | kecil–sedang |
| 3 | Bank Reconciliation | ⭐⭐⭐ | ⭐⭐⭐ | sedang–besar |
| 4 | Allowance / write-off piutang | ⭐⭐⭐ | ⭐ | sedang (blokir BD-2) |
| 5 | PPN end-to-end | ⭐⭐ | ⭐⭐ | besar |
| 6 | Fixed Asset + penyusutan | ⭐⭐ | ⭐ | besar |
| 7 | PPh 23 / 21 | ⭐⭐ | ⭐ | sedang |
| 8 | Invoice/Kwitansi PDF resmi | ⭐ | ⭐⭐ | kecil |

**Saya sengaja tidak menaruh nomor 8 di atas** meski effort-nya paling kecil. Kriteria pertama adalah kebenaran akuntansi, dan PDF tidak memperbaiki satu angka pun.

---

## 17. Technical Debt

Ringkasan berprioritas, dengan referensi:

| ID | Debt | Severity | Referensi |
| --- | --- | --- | --- |
| H-1 | Piutang `house` = jadwal, bukan piutang | **HIGH** | `reporting/repository.go:215-260`, `sale/service.go:663` |
| R-1 | Pembalik generik melewati pemilik sub-ledger | **HIGH** | `ledger/handler.go:597-632`, `JournalDetail.tsx:274-281` |
| G-1 | Tidak ada test GL ↔ sub-ledger | **HIGH** | §12.2 |
| R-2 | `reverseWithin` tak cek periode tutup | MEDIUM | `posting_service.go:345-410` vs `:133` |
| M-2 | `1-2100`/`1-2200` ber-peran piutang tanpa sub-ledger | MEDIUM | `account_role.go:47` |
| M-4 | Dua jalur titipan (`notary` vs `charge`) | MEDIUM | `notary.go:178,256` |
| M-5/F-3 | Nama bank hardcode di pratinjau | MEDIUM | `sale/collection.go:97-118` |
| R-4 | Void legacy tak ber-flag `is_reversing` | MEDIUM | `legacyar/payment.go:328-345` |
| T-1 | CLAUDE.md #6 ≠ kode | MEDIUM | §14 |
| T-2 | Reporting menulis SQL milik `sale` | MEDIUM | §3 |
| M-1 | Registry peran akun bukan pintu wajib | MEDIUM | 22 file |
| G-5..G-8 | Gap test (master non-aktif, konkurensi, isolasi `reporting`/`document`) | MEDIUM | §12.2 |
| L-1 | INV-DOC-1 bergantung kategori COA yang bisa diedit | LOW | `document_issuer.go:160-178` |
| L-2 | `query.go:438` tanpa filter tenant (aman turunan) | LOW | §14 |
| O-1 | Churn generated column 000063→64 | LOW | §11 |
| C-1..C-9 | Cleanup | LOW | §15 |
| M-3 | Konstanta `opening_balance` di 3 tempat | LOW | §9 |

**FACT — satu hal yang tidak masuk daftar debt, dan patut disebut:** tidak ditemukan satu pun `float64` yang menyentuh nilai uang di jalur produksi. Semua lewat `domain.Money`/`shopspring/decimal`, semua kolom uang `DECIMAL(20,4)`. Invariant #2 dipatuhi tanpa pengecualian.

---

## 18. Recommended Next Increment

### W-8 — Sub-ledger Piutang Harga Rumah & Rekonsiliasi GL

**Kenapa ini, bukan yang lain.** Tiga alasan, berurutan:

1. Ini satu-satunya temuan yang membuat **angka di layar berbeda dari angka di Neraca** tanpa penjelasan. Semua temuan lain adalah risiko; yang ini sudah terjadi.
2. W-5 dan W-7 sudah melakukan pekerjaan ini untuk dua sumber lain. Polanya sudah terbukti, tinggal diterapkan pada sumber ketiga — yang kebetulan yang terbesar.
3. Tanpa ini, **rekonsiliasi GL ↔ sub-ledger tidak bisa dijadikan invariant sistem**, dan setiap increment finansial berikutnya dibangun di atas fondasi yang tidak bisa diverifikasi.

**Ruang lingkup yang saya usulkan** (bukan implementasi — keputusan Anda):

1. **Pisahkan dua konsep secara eksplisit di layar.** "Jadwal Penagihan" (komersial, semua jadwal) vs "Piutang" (akuntansi, hanya yang sudah diakui). Bukan sekadar ganti label — dua angka, dua kolom, dua arti.
2. **Beri `house` disiplin pengakuan yang sama seperti realisasi:** baris piutang `house` hanya masuk aging bila unitnya sudah BAST (atau kriteria pengakuan lain yang Anda tetapkan — lihat BD-1 §19).
3. **Turunkan rincian per cicilan dari piutang gelondongan BAST**, supaya sisa Rp X di GL bisa dijelaskan menjadi "cicilan #4 Rp A + pelunasan Rp B".
4. **Jadikan rekonsiliasi sebagai layar, bukan test.** Pola `LegacyReconciliationCard` sudah ada dan terbukti bisa dibaca akuntan — perluas ke seluruh 1-2000: porsi saldo awal, porsi BAST, porsi realisasi, porsi legacy, selisih.
5. **Tutup G-1:** satu test integrasi yang memuat ketiga sumber dari DB sungguhan dan menuntut `GL(1-2000) == Σ sub-ledger` persis sampai rupiah terakhir.

**Yang TIDAK termasuk W-8:** mengubah kapan pendapatan diakui, mengubah perlakuan uang muka, atau menyentuh HPP. W-8 hanya membuat **daftar piutang** setuju dengan **buku besar**; ia tidak mengubah satu pun jurnal yang sudah benar.

### W-9 (bisa digabung ke W-8 kalau ruangnya cukup) — Kedaulatan Pembalikan

Menutup R-1, R-2, R-4: endpoint pembalik generik menolak jurnal yang dimiliki sub-ledger (arahkan ke jalur pembatalan domainnya), `reverseWithin` menghormati periode tutup, dan void legacy diberi flag yang benar.

Kecil, dan mencegah kelas kerusakan yang tidak bisa dideteksi setelah terjadi.

---

## 19. Business Decisions Required

Hanya hal yang **belum** final. Keputusan yang sudah Anda kunci (D-1..D-4, R-1..R-5, BD-1, K-1..K-5, T-1..T-5, booking fee, HPP budgeted) tidak saya buka lagi.

### BD-1 — Kapan piutang harga rumah lahir?

Hari ini GL bilang **saat BAST**; layar bilang **saat jadwal dibuat**. Salah satunya harus mengalah, dan itu keputusan akuntansi, bukan teknis.

- **Opsi A (saya rekomendasikan):** piutang lahir saat **BAST** (status quo GL, sejalan PSAK 44 & Invariant #7). Layar aging menampilkan hanya yang sudah diakui; jadwal yang belum BAST pindah ke layar "Jadwal Penagihan".
- **Opsi B:** piutang lahir saat **invoice/termin jatuh tempo diterbitkan** (sejajar dengan R-5 untuk realisasi). Lebih konsisten antar sumber, tapi mengubah jurnal: setiap termin jatuh tempo akan menghasilkan Dr 1-2000 / Cr 2-2000, dan itu perubahan pengakuan yang butuh persetujuan akuntan.

Ini **blocker untuk W-8** — desainnya bercabang total tergantung jawabannya.

### BD-2 — Allowance / impairment / write-off piutang (masih terbuka dari W-7)

Belum diimplementasikan dan tidak boleh dianggap selesai. Yang dibutuhkan dari akuntan: dasar pencadangan (umur? kasus per kasus?), akun beban & kontra-aset, dan apakah write-off menghapus dari daftar atau menandainya.

### BD-3 — Apakah `1-2200 Piutang Bank (KPR)` harus muncul di daftar piutang?

Menurut saya **tidak** (piutang ke bank ≠ piutang customer), tapi kalau tidak, aplikasi harus berhenti menyebut layarnya "seluruh piutang" dan menyediakan tempat lain untuk memantau pencairan KPR yang tertunda.

### BD-4 — Nasib `2-2300 Titipan Notaris`

Notaris sekarang adalah jenis biaya realisasi (2-2400). Pilihannya: (a) migrasikan saldo 2-2300 ke 2-2400 dan bekukan 2-2300 sebagai akun historis, atau (b) biarkan keduanya hidup permanen. Ada implikasi ke laporan dan ke jalur payout.

### BD-5 — EDGE-6 (dari W-6, masih terbuka)

Perlakuan Laba Rugi tahun berjalan saat go-live jatuh di tengah tahun. Bukan blocker.

### BD-6 — Aturan laporan komparatif: snapshot vs GL

Sebelum Neraca Komparatif dibangun, tetapkan: kolom tahun historis **wajib** dari histfin, **tidak boleh** dari GL. Murah kalau ditulis sekarang, mahal kalau ditemukan setelah laporan terbit.

---

## 20. Final Architecture Verdict

**Fondasi domainnya konsisten dan layak dibangun di atasnya — setelah satu retakan ditutup.**

Yang saya temukan setelah menelusuri kode, migrasi, test, frontend, dan data:

**Fondasi yang kokoh.** Ledger append-only tanpa pintu belakang (`MarkPosted` tidak punya pemanggil luar). Document Domain dengan penegakan fail-closed yang terbukti pada data hidup, bukan pada dokumen. Satu mesin aging, satu mesin penomoran, satu tempat angka historis. Isolasi tenant yang eksplisit di setiap query dan lolos sapuan sistematis. Nol `float64` di jalur uang. Batas paket yang dijaga sampai ke level "menyalin satu konstanta daripada menambah satu edge dependensi".

**Satu retakan struktural.** Piutang harga rumah tidak pernah didisiplinkan seperti realisasi (W-5) dan legacy (W-7). Akibatnya daftar piutang dan buku besar mengukur dua hal berbeda di bawah satu nama, dan rekonsiliasi GL ↔ sub-ledger tidak bisa dijadikan invariant. Ini bukan kelalaian W-7 — ini pekerjaan yang belum kebagian giliran.

**Satu lubang kedaulatan.** Pembalik jurnal generik bisa merusak sub-ledger mana pun tanpa alarm, dan tombolnya ada di UI, satu klik dari setiap jurnal.

**Satu pelajaran metodologis yang saya catat untuk audit berikutnya:** formula rekonsiliasi di brief ini terlihat benar, dan test W-7 hijau. Kalau saya berhenti di situ, saya akan melaporkan LULUS. Yang membuka temuan terbesar adalah menjalankan formula itu terhadap data sungguhan di semua tenant — dan menemukan bahwa dua tenant pecah ke arah yang berlawanan. Tenant yang cocok sempurna cocok justru karena **tidak punya** sumber piutang yang bermasalah.

**Verdict:** lanjut ke **W-8 (sub-ledger piutang harga rumah + rekonsiliasi GL)**, digabung atau diikuti **W-9 (kedaulatan pembalikan)**. Keduanya menutup satu-satunya dua hal yang bisa membuat pembukuan salah tanpa ada yang tahu. Sesudahnya, sistem ini punya fondasi finansial yang bisa diverifikasi — dan increment berikutnya bisa dipilih berdasarkan nilai bisnis, bukan berdasarkan risiko.

**BD-1 harus dijawab sebelum W-8 dimulai.**
