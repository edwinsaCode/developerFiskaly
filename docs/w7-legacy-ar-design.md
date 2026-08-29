# W-7 — Piutang Proyek Lama (Legacy AR): Audit + Desain

Status: **DESAIN — belum ada kode.**
Tanggal: 2026-08-10
Prasyarat yang sudah selesai: W-1 … W-6.

Dokumen ini adalah hasil audit kode aktual + desain lengkap. Tidak ada satu baris
implementasi pun yang ditulis sebelum bagian ini disetujui.

---

## 0. Ringkasan keputusan (baca ini dulu)

| # | Keputusan | Inti |
|---|---|---|
| K-1 | **Import TIDAK memposting jurnal** | Legacy AR adalah **sub-ledger** dari akun kontrol piutang yang sudah ada. Posisi buku besar tetap datang dari Saldo Awal yang mekanismenya sudah hidup. |
| K-2 | **Tidak ada akun lawan baru** | `3-9000` tetap mati. Kalau ternyata Saldo Awal belum memuat piutangnya, wizard menawarkan jurnal **saldo awal** (mekanisme lama) dengan akun lawan **dipilih user dari COA**. |
| K-3 | **Tidak ada jenis dokumen baru** | Pembayaran legacy memakai **BKM** (`cash_in`) lewat jalur `CreateAndPost` + `DocumentSpec`. KWL terbukti **tidak dibutuhkan**. |
| K-4 | **Tidak ada mesin AR kedua** | `receivable.Source` bertambah satu nilai: `legacy`. Sisanya adapter + reader — persis pola `charge` → `reporting`. |
| K-5 | **Tidak ada `termin_payments` untuk legacy** | Tabel itu `unit_id`/`project_id` NOT NULL; memakainya berarti mengarang unit/proyek — dilarang eksplisit. |
| K-6 | **Customer legacy berdiri sendiri** | Nama customer di-snapshot di baris piutangnya. Tautan ke master `customers` **opsional**, bukan syarat. |
| K-7 | **Lebih bayar ditolak** | Pemetaan konsisten dengan aturan yang sudah ada: kelebihan bayar pasca-BAST ditolak; legacy setara pasca-BAST. |

Dua hal yang **tidak** bisa saya putuskan sendiri ada di §16 (BUSINESS DECISION).

---

## 1. W-7 Architecture Review

### 1.1 Apa yang sudah ada dan boleh dipakai ulang

**`internal/receivable` — INV-AR-1.**
Satu-satunya mesin bucketing di repo. `Source` di sana bukan alasan membuat laporan
kedua; ia **dimensi pada satu daftar**. Yang perlu disentuh hanya tiga tempat:

```go
// receivable.go
const SourceLegacy Source = "legacy"                       // + konstanta
func (s Source) Valid() bool { … || s == SourceLegacy }    // + cabang
for _, s := range []Source{SourceHouse, SourceRealization, SourceLegacy} { … }  // urutan BySource
```

Tidak ada perubahan lain pada mesin aging. `BuildAging`, `FilterBySource`,
bucket, `collectionRate` — semuanya sudah agnostik terhadap asal baris.

**`internal/reporting` — seam komposisi yang memang disiapkan untuk ini.**
`reporting.Service` sudah memegang reader yang dipasang lewat chaining:

```go
type ARAgingReader interface            { GetARScheduleRows(…) ([]receivable.Row, error) }
type RealizationReceivableReader interface { ReceivableRows(…) ([]receivable.Row, error) }
func (s *Service) WithARReader(r ARAgingReader) *Service
func (s *Service) WithRealizationReader(r RealizationReceivableReader) *Service
```

W-7 menambah pasangan ketiga dengan bentuk yang **identik**. Arah import tetap
`legacyar` (transaksional) ⟵ `reporting` (read model): interface-nya tinggal di
`reporting`, dan keduanya hanya bertemu di `receivable.Row` (TD-3 / D-W4-2).

**`internal/ledger` — jalur kas + dokumen.**
`postCashJournalTx`-style di `charge/repository.go:595` adalah pola yang persis
dibutuhkan: `CreateAndPost` dengan `Document: ledger.DocumentSpec{…}` dalam satu
transaksi, dokumen terbit setelah `MarkPosted`, dan `enforceDocument` fail-closed
menolak jurnal kas tanpa dokumen. W-7 tidak perlu satu baris pun plumbing baru.

**`internal/document` — engine penomoran config-driven.**
`BKM` (`TypeCashIn`) sudah ada dan sudah bermakna "kas masuk milik perusahaan".
Menambah jenis dokumen adalah **data di `document_types`**, bukan kode — itu
sebabnya `KWL` tidak perlu dilahirkan sekarang (lihat §6.4).

**Saldo Awal.**
Bukan modul tersendiri: ia jurnal manual biasa dengan `source = "opening_balance"`
(`ledger/handler.go:411`, `OpeningBalanceForm.tsx:72`), dikecualikan dari INV-DOC-1
karena "menetapkan POSISI, bukan pergerakan". Ini fakta paling penting untuk §5.

### 1.2 Apa yang TIDAK bisa dipakai ulang, dan kenapa

| Kandidat | Kenapa gugur |
|---|---|
| `termin_payments` | `UnitID uint64 gorm:"not null"`, `ProjectID uint64 gorm:"not null"` (`sale/model.go:19-20`). Memakainya = mengarang unit + proyek. Dilarang eksplisit di brief. |
| `ReceivePayment` / `PaymentCommitParams` | Seluruh strukturnya ber-anchor `ScheduleID`/`ContractID` (`payment_allocation.go:98-99`). Legacy tidak punya keduanya. |
| `payment_allocations` | Sub-ledger alokasi **termin → cicilan/charge item**. Baris legacy tidak punya termin. |
| `charge_items` / invoice | Itu piutang yang lahir dari invoice (R-5). Legacy justru tidak boleh mengarang invoice lama. |
| Customer Statement (`sale/statement.go`) | Ber-anchor kontrak. Legacy tanpa kontrak → tidak muncul di sana (lihat §12.4). |

Kesimpulan arsitektur: **legacy AR butuh penyimpan sendiri, tetapi tidak butuh
mesin aging sendiri dan tidak butuh mesin kas sendiri.** Bounded context baru yang
tipis, menempel ke tiga seam yang sudah ada.

### 1.3 Bentuk akhir

```
                      ┌──────────────────────────┐
   Excel klien ──────►│  internal/legacyar       │
                      │  (bounded context baru)  │
                      │  • batch + staging       │
                      │  • legacy_receivables    │
                      │  • settlement + audit    │
                      └───────┬──────────┬───────┘
                              │          │
              ReceivableRows()│          │CreateAndPost + DocumentSpec{BKM}
                              ▼          ▼
                 ┌────────────────┐   ┌──────────────────┐
                 │   reporting    │   │  ledger/document │
                 │ (WithLegacy…)  │   │  (jalur kas W-3) │
                 └───────┬────────┘   └──────────────────┘
                         ▼
              receivable.BuildAging()   ← SATU mesin, INV-AR-1 utuh
                         ▼
              GET /reports/ar-aging  (house + realization + legacy)
```

---

## 2. Domain Model

Kosakata yang dipakai konsisten di kode, API, dan layar:

| Istilah | Arti | Bukan |
|---|---|---|
| **Piutang Lama** (`LegacyReceivable`) | Satu kewajiban customer dari proyek lama, dinyatakan sebagai **satu saldo terutang per tanggal cutoff**. | Bukan invoice, bukan cicilan, bukan kontrak. |
| **Sumber Lama** (`source_label`) | Nama proyek/pekerjaan lama tempat piutang itu lahir, apa adanya dari klien. | Bukan FK ke `projects`. Proyek lama memang tidak ada di sistem. |
| **Tanggal Cutoff** (`as_of_date`) | Tanggal posisi saldo dinyatakan. Satu per batch. | Bukan tanggal transaksi. Tidak pernah dipakai sebagai tanggal jurnal. |
| **Akun Kontrol** (`control_account_code`) | Akun piutang di buku besar tempat saldo ini sudah/akan berada. | Bukan akun baru. Dipilih dari peran `RoleReceivable`. |
| **Pelunasan Lama** (`LegacyPayment`) | Satu penerimaan kas yang mengurangi satu Piutang Lama. | Bukan termin. Tidak pernah membuat piutang baru. |
| **Batch Import** (`ImportBatch`) | Satu berkas Excel yang diunggah, divalidasi, lalu dikomit sekaligus. | Bukan wadah transaksi. Ia jejak audit unggahan. |

Invariant domain yang W-7 tambahkan:

- **INV-LAR-1** — `outstanding = original_amount − Σ(pelunasan)`, dan `outstanding ≥ 0`
  selalu. Tidak ada saldo kredit di jalur legacy.
- **INV-LAR-2** — setiap `LegacyPayment` menunjuk **tepat satu** jurnal terposting
  dan **tepat satu** dokumen bernomor. (Turunan INV-DOC-1, bukan aturan kedua.)
- **INV-LAR-3** — `paid_amount` pada `legacy_receivables` adalah **cache**. Ia tidak
  pernah berubah tanpa baris `legacy_receivable_payments` di transaksi yang sama.
  (Pola guard #1 dari FE-2.)
- **INV-LAR-4** — baris legacy tidak pernah menulis `journal_entries` pada saat
  **import**. Satu-satunya jurnal yang lahir dari domain ini adalah jurnal
  **pelunasan** (dan pembaliknya).

---

## 3. Entity / Aggregate

**Aggregate root: `LegacyReceivable`.** Ia yang dikunci (`FOR UPDATE`) saat
pelunasan, ia yang punya status, ia yang muncul di laporan.

```
LegacyReceivable (aggregate root)
├── id, tenant_id
├── batch_id                 → ImportBatch (asal-usul, immutable)
├── customer_name            snapshot, otoritatif
├── customer_id?             tautan OPSIONAL ke master customers
├── source_label             "Proyek Lama A"
├── external_ref?            no. referensi klien (kunci idempotensi)
├── phone?, email?
├── control_account_code     snapshot akun kontrol ("1-2000")
├── as_of_date               tanggal cutoff batch
├── due_date?                jatuh tempo; kosong → as_of_date
├── original_amount          DECIMAL(20,4), > 0, IMMUTABLE
├── paid_amount              DECIMAL(20,4), cache dari settlement
├── status                   open | paid | written_off
└── notes?

LegacyPayment (child, append-only)
├── legacy_receivable_id, tenant_id
├── amount (boleh NEGATIF hanya untuk baris void — mirror)
├── payment_date, cash_account_code
├── journal_entry_id, document_id, document_number
├── voids_payment_id?        baris void menunjuk baris aslinya
└── idempotency_key?, created_by

ImportBatch (append-only, jejak audit)
├── file_name, file_hash (SHA-256), file_size
├── as_of_date, control_account_code
├── status: draft | committed | discarded
├── row_count, valid_count, error_count
└── committed_at, committed_by

ImportBatchRow (staging, apa adanya dari berkas)
├── batch_id, row_number
├── raw_* (semua kolom sebagai STRING mentah)
├── parse_status: ok | error | warning
├── errors JSON
└── legacy_receivable_id?    diisi saat commit
```

**Kenapa `original_amount` immutable.** Kalau angkanya salah ketik, koreksinya
adalah membatalkan barisnya dan mengimpor ulang — bukan mengedit. Alasannya sama
dengan alasan ledger append-only: begitu satu pelunasan menempel padanya, mengubah
nominalnya secara diam-diam mengubah arti pelunasan yang sudah terjadi.

**Kenapa staging (`ImportBatchRow`) menyimpan nilai MENTAH.** Ketika klien protes
"angkanya bukan segitu", yang harus bisa dibuka adalah **apa yang mereka kirim**,
bukan hasil parsing kita. Ini pelajaran yang sama dengan snapshot kode akun di W-6.

---

## 4. Desain `source = "legacy"`

### 4.1 Perubahan di `internal/receivable` (total: 3 baris efektif)

```go
const SourceLegacy Source = "legacy" // saldo piutang proyek lama (legacy_receivables)

func (s Source) Valid() bool {
	return s == SourceHouse || s == SourceRealization || s == SourceLegacy
}

// BuildAging — urutan BySource
for _, s := range []Source{SourceHouse, SourceRealization, SourceLegacy} { … }
```

Legacy diletakkan **terakhir** dengan sengaja: yang dibaca duluan owner adalah
piutang buku berjalan; piutang lama adalah ekor yang sedang dihabiskan.

### 4.2 Adapter di `internal/legacyar`

```go
// ReceivableRows adalah SATU-SATUNYA jalan keluar data piutang lama ke laporan.
// Bentuknya meniru charge.ReceivableRows persis — supaya tidak ada orang yang
// tergoda menulis SQL aging kedua di sini.
func (s *Service) ReceivableRows(ctx context.Context, tenantID uint64) ([]receivable.Row, error)
func (s *Service) ReceivableRowsByCustomer(ctx context.Context, tenantID, customerID uint64) ([]receivable.Row, error)
```

Pemetaan kolom → `receivable.Row`:

| `receivable.Row` | Nilai legacy | Catatan |
|---|---|---|
| `Source` | `SourceLegacy` | |
| `ContractID` | `0` | legacy memang tak berkontrak — layar bercabang di sini |
| `UnitID` | `0` | idem |
| `RefID` | `legacy_receivables.id` | kunci baris di layar & aksi bayar |
| `BuyerName` | `customer_name` | snapshot, bukan JOIN |
| `UnitCode` | `""` | legacy tak berunit. **Jangan** dititipi nama proyek lama — komentar `receivable.go:54` melarangnya eksplisit; itu tugas `Label`. Layar merender "—" untuk sumber legacy. |
| `Label` | `source_label` | "Proyek Lama A" |
| `InvoiceNumber` | `external_ref` atau `""` | `""` → mesin merender "—" (sudah ada) |
| `DueDate` | `COALESCE(due_date, as_of_date)` | lihat §4.3 |
| `Amount` | `original_amount` | |
| `PaidAmount` | `paid_amount` | cache; SSOT-nya `legacy_receivable_payments` |
| `Received` | `paid_amount >= original_amount` | mesin memindahkannya ke `TotalCollected` |

Filter query: `status = 'open'` saja. Baris `paid` sudah keluar sendiri lewat
`Received`, tetapi memfilter di SQL menghindari menarik ribuan baris lunas setiap
kali laporan dibuka — alasan yang sama dengan `ReceivableRowsByContract`.

### 4.3 Umur piutang legacy — keputusan yang perlu dinyatakan terang

Baris legacy **tidak punya jatuh tempo yang sebenarnya** kecuali klien menuliskannya.
Aturannya:

- `due_date` diisi di template → itu yang dipakai. Umur dihitung normal.
- `due_date` kosong → jatuh tempo = **tanggal cutoff**. Artinya: "per tanggal ini,
  uang ini seharusnya sudah ada di tangan". Konsekuensinya seluruh baris legacy
  langsung masuk bucket `> 90 hari` bila cutoff-nya lama.

Itu **disengaja dan benar**: piutang proyek lama yang belum tertagih memang tunggakan.
Yang tidak boleh terjadi adalah ia diam-diam masuk "Belum Jatuh Tempo" dan
menyamarkan risiko. Layar menyebut ini eksplisit di catatan kaki tabel.

### 4.4 Wiring di `main.go`

```go
reportingSvc := reporting.NewService(…).
	WithARReader(saleSvc).
	WithRealizationReader(chargeSvc).
	WithLegacyReader(legacyarSvc)   // ← satu baris
```

Dan `GetARAging` bertambah satu cabang simetris dengan yang sudah ada:

```go
if src != receivable.SourceHouse && src != receivable.SourceRealization && s.legacy != nil {
	rows2, err := s.legacy.ReceivableRows(ctx, tenantID)
	if err != nil { return AgingReport{}, fmt.Errorf("baris piutang lama: %w", err) }
	rows = append(rows, rows2...)
}
```

Kegagalan reader **dikembalikan sebagai error**, tidak pernah ditelan — sama dengan
komentar yang sudah ada di `ar_aging.go` untuk reader realisasi. Piutang yang hilang
diam-diam dari total lebih berbahaya daripada laporan yang gagal terbuka.

---

## 5. Perlakuan Akuntansi (bagian terpenting)

### 5.1 Pertanyaan yang sebenarnya

Bukan "akun lawan apa untuk piutang lama", melainkan: **apakah saldo ini sudah ada
di buku besar atau belum?**

Audit menjawabnya: sistem **sudah punya** mekanisme untuk menaruhnya —
Saldo Awal (`source = 'opening_balance'`). Klien yang membuka buku di sistem ini
memang seharusnya sudah menulis `Dr 1-2000 Piutang Usaha` di sana, bersama kas,
persediaan, hutang, dan modal.

Yang **tidak** dipunyai sistem adalah **rinciannya**: siapa yang berutang berapa.
Buku besar hanya tahu satu angka gabungan.

Jadi persoalan W-7 bukan persoalan jurnal. Ia persoalan **sub-ledger**.

### 5.2 Keputusan K-1 — import tidak memposting jurnal apa pun

`legacy_receivables` adalah **buku pembantu piutang** untuk akun kontrol yang sudah
ada. Persis peran `payment_allocations` terhadap kas, dan persis semangat W-6
(`internal/histfin`) yang tidak pernah menyentuh ledger.

Empat alasan, berurutan dari yang paling mahal:

1. **Double counting.** Kalau Saldo Awal sudah memuat Piutang Usaha (dan itu kasus
   normal), jurnal import kedua **melipatgandakan aset**. Ini risiko nomor satu di
   fitur ini, dan satu-satunya cara benar menutupnya adalah tidak memposting sama
   sekali lalu **mencocokkan**.
2. **Append-only + periode.** Jurnal import bertanggal cutoff (mis. 2025-12-31)
   akan ditolak `PeriodChecker` bila periodenya sudah ditutup. Dipaksa bertanggal
   hari ini, ia salah menyatakan posisi pembukaan.
3. **Tidak ada kejadian ekonomi baru.** Import adalah pencatatan rincian atas fakta
   yang sudah diakui. Menerbitkan jurnal untuk itu mengarang transaksi — persis yang
   dilarang brief.
4. **Akun lawan jadi tidak relevan.** Tanpa jurnal, tidak ada `3-9000`, tidak ada
   clearing, tidak ada akun ekuitas dadakan. Persoalan yang tidak perlu ada, hilang.

### 5.3 Konsekuensinya: kewajiban rekonsiliasi (bukan opsional)

Kalau import tidak memposting, ada satu bahaya baru yang harus ditutup di layar:
**sub-ledger bisa tidak cocok dengan buku besar.** Karena itu wizard import punya
langkah **Cocokkan dengan Buku Besar** yang wajib dilewati:

```
Akun kontrol            : 1-2000 Piutang Usaha
Saldo buku besar        : Rp 300.000.000      ← journal_lines terposting, per CUTOFF
 − mutasi operasi       : Rp           0      ← house AR + realisasi (bukan jatah legacy)
 − efek pelunasan legacy: Rp           0      ← sudah tercermin di sisa sub-ledger
─────────────────────────────────────────────
 = porsi saldo awal     : Rp 300.000.000
Sudah ada rinciannya    : Rp  33.000.000      ← Σ NILAI ASLI piutang lama yang sudah masuk
Akan diimpor batch ini  : Rp 267.000.000      ← 3 baris
─────────────────────────────────────────────
Selisih setelah import  : Rp           0   ✓ cocok
```

Bentuknya sengaja **"kupas lapis"**, bukan "bandingkan dua angka besar". Saldo akun
kontrol dipecah dulu menurut asal jurnalnya (`AccountMovementBySource`) supaya yang
dibandingkan dengan rincian piutang lama hanyalah **porsi saldo awal**-nya, dan yang
dibandingkan dari sisi sub-ledger adalah **nilai asli**, bukan sisa tagihan.

Alasannya bukan kerapian melainkan daya tahan: kalau yang dibandingkan saldo hidup
lawan sisa tagihan, rekonsiliasi akan pecah setiap kali ada customer legacy membayar
— padahal tidak ada yang salah. Kontrol yang menyalakan alarm setiap hari akan
berhenti dibaca orang, dan kontrol yang tidak dibaca sama saja tidak ada. Dengan
bentuk ini, pembayaran menurunkan buku besar DAN dijelaskan oleh barisnya sendiri,
sehingga selisihnya tetap nol.

**Jurnal pembalik mewarisi source jurnal yang dibalikkannya.** Buku besar ini
append-only, jadi satu-satunya cara sah membatalkan saldo awal yang salah adalah
membalikkannya. Kalau pembalik itu jatuh ke kelompok "mutasi operasi", koreksi yang
benar justru membuat porsi saldo awal tampak terlalu besar **selamanya** — dan
rekonsiliasi melaporkan selisih yang tidak pernah bisa ditutup oleh apa pun.
Aturan ini dipatok di `ledger.AccountMovementBySource` (bukan di pemanggilnya, supaya
setiap konsumen mendapat atribusi asal-ekonomi yang sama) dan dikunci oleh
`TestB3_ReversedOpeningBalanceStaysMatched`.

Tiga kemungkinan, tiga perlakuan:

| Selisih | Arti | Perlakuan |
|---|---|---|
| **0** | Saldo Awal sudah memuat piutang ini | Import jalan. Tidak ada jurnal. |
| **Porsi saldo awal < rincian** | Saldo Awal belum memuatnya (atau belum diisi sama sekali) | Wizard menawarkan **"Buat jurnal saldo awal untuk selisih"** — lihat §5.4. Boleh juga dilewati dengan alasan tertulis. |
| **Porsi saldo awal > rincian** | Ada piutang di GL yang tidak ada rinciannya di mana pun | Import tetap jalan; layar menandai sisa yang belum berinci. Ini temuan, bukan error — dan justru berguna. |

Selisih **tidak pernah memblokir** import. Memblokir akan membuat admin mengarang
angka supaya cocok. Yang benar: import jalan, selisihnya kelihatan, dan ada tombol
untuk membereskannya.

### 5.4 Kalau jurnal memang dibutuhkan (K-2)

Bila klien memilih menutup selisih, yang dipakai adalah **mekanisme Saldo Awal yang
sudah ada**, bukan mekanisme baru:

```
Tanggal   : tanggal cutoff batch
Source    : opening_balance          ← dikecualikan dari INV-DOC-1, sudah ada
Dr  1-2000 Piutang Usaha            267.000.000
    Cr  <akun lawan pilihan user>   267.000.000
```

Aturan akun lawan:

- **Dipilih user dari COA**, tidak pernah dihardcode.
- **Default cerdas**: sistem membaca jurnal `opening_balance` tenant yang sudah ada
  dan mengusulkan akun ekuitas yang dipakai di sana (biasanya `3-1000 Modal` atau
  Laba Ditahan). Kalau belum ada Saldo Awal sama sekali, tidak ada default — user
  wajib memilih.
- **Dibatasi tipe `equity`** dan disertai penjelasan dampak neraca di layar, apa
  adanya: *"Aset (Piutang) naik Rp 267.000.000. Ekuitas naik sebesar yang sama.
  Tidak ada pendapatan yang diakui."*
- **Bukan akun clearing.** Clearing/suspense dipakai saat ada kaki kedua yang masih
  menggantung. Di sini tidak ada: ini penetapan posisi pembukaan, dan lawannya
  memang ekuitas. Menaruhnya di clearing hanya menunda pertanyaan yang sama.

Jurnalnya satu per batch, ditautkan ke `import_batches.opening_journal_id` supaya
asal-usulnya terbaca dua arah.

### 5.5 Akun kontrol — snapshot, bukan konstanta

`control_account_code` disimpan **di setiap baris** `legacy_receivables`, bukan
dibaca ulang saat pembayaran. Alasannya sama dengan `tax_rules` menyimpan
`rule_id + revision` dan `charge_items` menyimpan akun liability-nya: kalau
konfigurasi berubah tahun depan, pelunasan piutang lama harus tetap mengkredit akun
tempat piutangnya **dibentuk**.

Pilihannya dibatasi ke anggota `ledger.RoleReceivable` (`1-2000`, `1-2100`, `1-2200`),
default `1-2000`. Catatan keterbatasan: `roleCodes` masih daftar kode statis di
`account_role.go:46`, jadi kalau klien ingin akun khusus "Piutang Proyek Lama"
(mis. `1-2050`), akun itu harus ditambahkan ke registry — perubahan satu baris,
tetapi keputusan COA yang harus datang dari klien, bukan dari saya. Lihat §16.

### 5.6 Ringkasan dampak neraca

| Kejadian | Jurnal | Dampak |
|---|---|---|
| Import 3 baris legacy | **tidak ada** | Tidak ada. Neraca tidak bergerak sedikit pun. |
| Jurnal penutup selisih (opsional) | `Dr 1-2000 / Cr <ekuitas>` | Aset ↑, Ekuitas ↑. Tidak ada pendapatan. |
| Budi bayar Rp 50 jt | `Dr 1-1100 Kas / Cr 1-2000` | Kas ↑, Piutang ↓. Total aset tetap. Tidak ada pendapatan. |
| Void pelunasan | jurnal pembalik + dokumen JR | Kebalikannya, append-only. |

**Tidak ada pendapatan yang pernah diakui di seluruh jalur W-7.** Pendapatan proyek
lama sudah diakui di buku lama; mengakuinya lagi di sini akan menggandakan laba.

---

## 6. Alur Pembayaran

### 6.1 Bentuk

`POST /legacy-receivables/{id}/payments`

```jsonc
{
  "date": "2026-08-12",
  "amount": "50000000",
  "cash_account_code": "1-1100",
  "notes": "transfer BCA",
  "idempotency_key": "…"        // opsional; header Idempotency-Key juga diterima
}
```

### 6.2 Satu transaksi, urutan tetap

```
BEGIN
 1. SELECT … FOR UPDATE  legacy_receivables WHERE id=? AND tenant_id=?
 2. Validasi (§13.2): status open · amount > 0 · amount ≤ outstanding
                      · akun kas sah (COA category cash|bank) · periode terbuka
 3. posting.CreateAndPost(
        Date: date, Source: "legacy_ar_payment",
        Lines: [ Dr <cash_account_code>, Cr <control_account_code> ],
        Document: ledger.DocumentSpec{TypeCode: ledger.DocCashIn},   // BKM
    )
    → jurnal terposting + dokumen BKM terbit + tertaut, semuanya di tx ini
 4. INSERT legacy_receivable_payments (… journal_entry_id, document_id …)
 5. UPDATE legacy_receivables SET paid_amount = paid_amount + ?,
           status = IF(paid_amount + ? >= original_amount, 'paid', 'open')
 6. INSERT legacy_receivable_audits ('payment_recorded', …)
COMMIT
```

Langkah 4 dan 5 **tidak boleh terpisah** — itu INV-LAR-3, pola guard #1 dari FE-2:
tidak ada `paid_amount` yang berubah tanpa baris settlement-nya.

### 6.3 Apa yang alur ini sengaja TIDAK lakukan

- **Tidak menyentuh `termin_payments`.** Tabel itu ber-anchor unit/proyek.
- **Tidak menyentuh `payment_allocations`.** Tidak ada cicilan/charge item untuk
  dialokasikan; menaruh baris di sana akan merusak arti sub-ledger itu.
- **Tidak membuat invoice, kontrak, unit, atau piutang baru.**
- **Tidak menyentuh Customer Statement.** Statement ber-anchor kontrak (§12.4).

Konsekuensi yang harus disadari: **collection rate** di `/accounting/receivable`
akan ikut menghitung baris legacy. Itu benar — uang yang tertagih tetap uang yang
tertagih — tetapi karena `total_scheduled` legacy adalah saldo pembukaan, angkanya
akan turun saat legacy pertama kali diimpor. Layar menyebutkan ini sekali di
catatan kaki; ia bukan bug.

### 6.4 Dokumen: BKM, dan kenapa KWL tidak dibuat

Brief melarang membuat jenis `KWL` sebelum terbukti perlu. Hasil audit:

| Kebutuhan | Terpenuhi oleh BKM? |
|---|---|
| INV-DOC-1: satu pergerakan kas = satu dokumen bernomor | **Ya.** `IssueForJournal` + `enforceDocument` sudah menjaminnya. |
| Nomor unik, reset tahunan, seri per tenant | **Ya.** Engine W-2, konfigurasi di `document_types`. |
| Telusur dua arah jurnal ↔ dokumen | **Ya.** `journal_entries.document_id` + `documents.source_*`. |
| Arti "kas masuk milik perusahaan" | **Ya.** Pelunasan piutang memang kas perusahaan, bukan titipan. |
| Lembar bukti untuk **diserahkan ke customer** | **Tidak** — BKM voucher internal. |

Jadi: KWL **tidak dibutuhkan** untuk kebenaran akuntansi maupun invariant. Ia hanya
relevan bila klien ingin menyerahkan kwitansi fisik untuk pembayaran piutang lama.
Kalaupun nanti diminta, jawabannya **bukan** kode baru: tambah satu baris di master
`document_types` (`KWL`, prefix, format, reset) dan ganti `TypeCode` di satu tempat.
Engine W-2 memang dirancang begitu. Saya tidak membuatnya sekarang.

### 6.5 Void

`POST /legacy-receivables/{id}/payments/{paymentID}/void`, satu transaksi:
jurnal pembalik lewat jalur `Reverse` yang sudah ada (dokumen **JR** menunjuk BKM
aslinya, D-W3-5), lalu baris `legacy_receivable_payments` **negatif** yang menunjuk
baris aslinya lewat `voids_payment_id` — mirror, bukan hapus. Pola persis
`charge_item_void`. `paid_amount` dikurangi, `status` kembali `open`.

### 6.6 Lebih bayar (K-7)

Ditolak: `amount > outstanding` → `400`, pesan menyebut sisa tagihan yang sebenarnya.

Alasan, dari aturan yang **sudah ada** dan bukan aturan baru: sistem mengizinkan
kelebihan bayar **pra-BAST** (jadi saldo kredit buyer, karena masih ada kontrak
berjalan yang akan menyerapnya) dan **menolaknya pasca-BAST**. Piutang lama setara
pasca-BAST — barangnya sudah lama diserahkan, tidak ada kewajiban berjalan yang bisa
menyerap kelebihan, dan tidak ada kontrak tempat menggantungkan saldo kredit.

Kalau customer benar-benar menyetor lebih, jalurnya sudah ada dan bukan milik W-7:
itu penerimaan kas biasa (jurnal manual + BKM) yang dikredit ke akun yang dipilih
akuntan. Menciptakan "saldo kredit legacy" berarti membuat mekanisme baru untuk
kasus yang jarang — persis yang brief larang.

---

## 7. Alur Import

Tiga langkah, dan **satu-satunya langkah yang menulis `legacy_receivables` adalah
langkah 3**.

```
┌─ 1. UNDUH TEMPLATE ────────────────────────────────────────────┐
│ GET /legacy-ar/template.xlsx                                   │
│ • header terkunci + baris contoh + sheet "Petunjuk"            │
└────────────────────────────────────────────────────────────────┘
                              ▼
┌─ 2. UNGGAH + PRATINJAU ────────────────────────────────────────┐
│ POST /legacy-ar/batches  (multipart: file, as_of_date,         │
│                           control_account_code)                │
│ • hash berkas → tolak kalau identik dengan batch committed     │
│ • parse SELURUH baris, validasi per baris                      │
│ • simpan ke staging (batch = draft) — BUKAN ke piutang         │
│ • balas: ringkasan + baris + error per baris + tie-out GL      │
└────────────────────────────────────────────────────────────────┘
                              ▼
┌─ 3. KONFIRMASI ────────────────────────────────────────────────┐
│ POST /legacy-ar/batches/{id}/commit                            │
│ • tolak bila masih ada baris error  → tidak ada import separuh │
│ • SATU transaksi: insert semua legacy_receivables,             │
│   (opsional) jurnal saldo awal selisih, batch → committed      │
│ • balas: berhasil N, gagal 0, total nilai, nomor jurnal        │
└────────────────────────────────────────────────────────────────┘
```

### 7.1 Sepuluh syarat brief → di mana dipenuhi

| # | Syarat | Pemenuhan |
|---|---|---|
| 1 | Unduh template | `GET /legacy-ar/template.xlsx`, dihasilkan server (§7.2) |
| 2 | Upload Excel | Langkah 2, multipart, `.xlsx` + `.csv` |
| 3 | Preview sebelum import | Langkah 2 menyimpan staging & mengembalikan seluruh baris |
| 4 | Validasi per baris | §13.1, hasilnya per-baris bukan satu pesan global |
| 5 | Error jelas | `{row: 7, column: "outstanding", message: "…"}` + label kolom Indonesia |
| 6 | Tidak ada import separuh | Commit satu transaksi; ada satu baris error → seluruh batch ditolak |
| 7 | Konfirmasi import | Langkah 3 dipicu tombol terpisah, dengan ringkasan + tie-out |
| 8 | Hasil sukses/gagal | Respons commit + halaman riwayat batch |
| 9 | Audit trail | `import_batches` + `import_batch_rows` (mentah) + `legacy_receivable_audits` |
| 10 | Cegah import ganda | §8, tiga lapis |

### 7.2 Template resmi

Sheet **`Piutang Lama`**, header baris 1 terkunci:

| Kolom | Wajib | Contoh | Aturan |
|---|---|---|---|
| `nama_customer` | ✅ | Budi Santoso | 2–200 karakter |
| `proyek_lama` | ✅ | Proyek Lama A | 1–200 karakter |
| `outstanding` | ✅ | 150000000 | > 0, maks 4 desimal |
| `no_referensi` | — | LAMA-A-014 | unik per tenant bila diisi |
| `tanggal_jatuh_tempo` | — | 2025-12-31 | `YYYY-MM-DD`; kosong → cutoff |
| `telepon` | — | 08123456789 | |
| `email` | — | budi@… | format email bila diisi |
| `catatan` | — | sisa termin 3 | maks 500 |

Sheet kedua **`Petunjuk`** memuat arti tiap kolom, contoh yang benar, dan satu
kalimat yang paling menentukan keberhasilan fitur ini:

> Tulis **sisa tagihan per tanggal cutoff**, bukan harga jual dan bukan total
> pembayaran yang sudah masuk. Riwayat pembayaran lama tidak perlu diisi.

**Tanggal cutoff dan akun kontrol tidak ada di berkas** — keduanya diisi di form
unggah. Satu batch = satu posisi per satu tanggal. Membiarkannya per baris
mengundang satu berkas berisi lima tanggal cutoff berbeda, dan tie-out ke buku
besar jadi tidak punya arti.

### 7.3 Membaca angka dari Excel — jebakan Invariant #2

Sel Excel bertipe angka disimpan sebagai **float64** di dalam berkasnya. Membacanya
sebagai float lalu mengubahnya ke `Money` adalah pelanggaran Invariant #2 yang akan
lolos review karena tidak ada `float64` yang terlihat di kode kita.

Aturan implementasi: gunakan `GetCellValue()` yang mengembalikan **string apa adanya**,
lalu `domain.NewMoney(string)`. Tidak pernah `GetCellFloat`. Nilai mentahnya juga
disimpan di `import_batch_rows.raw_outstanding` sehingga selisih pembulatan apa pun
bisa dibuktikan asalnya.

Pustaka: `github.com/xuri/excelize/v2` (dependensi baru — lihat §15.3). Berkas CSV
diterima juga lewat `encoding/csv` untuk klien yang berkasnya sudah rapi.

### 7.4 Batas

| Batas | Nilai | Alasan |
|---|---|---|
| Ukuran berkas | 5 MB (`http.MaxBytesReader`) | daftar piutang, bukan arsip |
| Jumlah baris | 5.000 | di atas itu commit satu transaksi jadi kunci panjang |
| Batch draft per tenant | 5 | mencegah staging jadi tempat sampah |

Batas yang terlampaui **ditolak dengan pesan yang menyebut angkanya**, tidak pernah
dipotong diam-diam.

---

## 8. Strategi Idempotensi

Empat lapis, dari yang paling kasar ke paling halus:

**L1 — Hash berkas.** SHA-256 isi berkas, `UNIQUE (tenant_id, file_hash)` **hanya
untuk batch berstatus `committed`** (kolom `committed_hash` yang diisi saat commit,
NULL selama draft). Mengunggah berkas yang sama persis dua kali → `409` yang
menyebut batch sebelumnya: *"Berkas ini sudah diimpor pada 10 Agu 2026 (Batch #4,
3 baris)."*

**L2 — `external_ref`.** `UNIQUE (tenant_id, external_ref)` pada `legacy_receivables`
bila terisi. Ini menutup lubang yang tidak bisa ditutup L1: berkas diedit sedikit
(hash berubah) tetapi isinya piutang yang sama. Kolom ini juga alasan kenapa
`no_referensi` ada di template meski opsional — layar mendorong pengisiannya.

**L3 — Kunci alami, sebagai peringatan.** `(normalisasi(nama), proyek_lama)` yang
sudah pernah masuk → baris ditandai **warning** di pratinjau, bukan error. Nama
orang berulang secara sah, dan satu customer bisa punya dua piutang di proyek yang
sama. Commit dengan warning menuntut centang eksplisit *"Saya sudah memeriksa,
ini bukan duplikat"*, dan centang itu tercatat di audit.

**L4 — `Idempotency-Key` pada commit dan pada pembayaran.** Pola yang sudah dipakai
`termin_payments` (`uk_termin_idempotency`) dan `charge_payments` (`uk_cp_idem`):
kunci unik per tenant, pengiriman kedua mengembalikan hasil pertama dengan `409` +
payload aslinya, bukan efek kedua.

Kenapa berlapis: L1 menangkap klik ganda, L2 menangkap berkas yang diedit,
L3 menangkap salah paham manusia, L4 menangkap jaringan. Tidak ada satu pun yang
cukup sendirian.

---

## 9. Audit Trail

Tiga lapis, masing-masing menjawab pertanyaan berbeda:

| Tabel | Pertanyaan yang dijawab |
|---|---|
| `import_batches` | Siapa mengunggah berkas apa, kapan, dengan cutoff dan akun kontrol apa, jadi berapa baris, dan jurnal saldo awal mana (kalau ada) yang menyertainya. |
| `import_batch_rows` | **Apa persisnya yang klien kirim** — nilai mentah per sel, hasil parse, dan error apa yang muncul. Ini yang dibuka saat klien protes. |
| `legacy_receivable_audits` | Apa yang terjadi pada satu piutang sepanjang hidupnya: `imported`, `payment_recorded`, `payment_voided`, `linked_to_customer`, `written_off`, `note_edited`. |

Aturan yang dibawa dari W-6: **audit mencatat KEADAAN, bukan klik.** Operasi yang
ditolak tidak meninggalkan baris audit — kalau tidak, riwayat penuh dengan percobaan
yang tidak pernah terjadi. Semua tabel append-only; tidak ada `UPDATE` maupun
`DELETE` di jalur produksi.

Nomor dokumen dan nomor jurnal pelunasan disimpan **di baris pembayarannya**
(`document_number` di-snapshot, bukan hanya `document_id`), supaya riwayat tetap
terbaca meski nanti dokumen dibaca dari layar lain.

---

## 10. ERD Konseptual

```
                    ┌──────────────────┐
                    │  import_batches  │  append-only
                    │  file_hash, cutoff, control_acct
                    │  status, counts, opening_journal_id ──┐
                    └────────┬─────────┘                    │
                     1       │       1                      │
                             │                              │  (opsional)
           ┌─────────────────┴──────────────┐               │
           ▼ N                              ▼ N             ▼
  ┌──────────────────┐            ┌────────────────────┐  ┌──────────────┐
  │ import_batch_rows│            │ legacy_receivables │  │journal_entries│
  │  raw_* (string)  │  ──────►   │  customer_name     │  │source=        │
  │  parse_status    │  saat      │  source_label      │  │opening_balance│
  │  errors JSON     │  commit    │  original_amount   │  └──────────────┘
  └──────────────────┘            │  paid_amount(cache)│
                                  │  control_acct_code │
                                  │  status            │
                                  │  customer_id? ─────┼──► customers (OPSIONAL)
                                  └───┬────────────┬───┘
                                    1 │            │ 1
                                      ▼ N          ▼ N
                    ┌──────────────────────────┐  ┌───────────────────────────┐
                    │legacy_receivable_payments│  │ legacy_receivable_audits  │
                    │ amount (neg utk void)    │  │ event, actor, payload     │
                    │ journal_entry_id ────────┼─►│ append-only               │
                    │ document_id/number       │  └───────────────────────────┘
                    │ voids_payment_id?        │
                    └───────────┬──────────────┘
                                │
                                ▼
                    journal_entries ──► documents (BKM / JR)   ← INV-DOC-1
```

Garis putus-putus (`customer_id`, `opening_journal_id`) adalah **logical FK**, sesuai
konvensi repo: legacy tidak boleh gagal diimpor hanya karena customer-nya tidak ada
di master.

Yang **tidak** ada di ERD ini, dan itu disengaja: tidak ada relasi ke `projects`,
`units`, `sale_contracts`, `payment_schedules`, `invoices`, `charge_items`.

---

## 11. Desain API

Semua di bawah `/api/v1`, semua tenant-scoped lewat middleware yang sudah ada.

### Import
| Method | Path | Role | Keterangan |
|---|---|---|---|
| GET | `/legacy-ar/template.xlsx` | authenticated | template resmi, dihasilkan server |
| POST | `/legacy-ar/batches` | owner\|accountant | multipart; parse + validasi + staging; **tidak menulis piutang** |
| GET | `/legacy-ar/batches` | authenticated | riwayat import |
| GET | `/legacy-ar/batches/{id}` | authenticated | pratinjau: baris + error + tie-out |
| POST | `/legacy-ar/batches/{id}/commit` | owner\|accountant | satu transaksi, all-or-nothing |
| DELETE | `/legacy-ar/batches/{id}` | owner\|accountant | buang draft; batch committed **tidak bisa** dihapus |

### Piutang
| Method | Path | Role | Keterangan |
|---|---|---|---|
| GET | `/legacy-receivables` | authenticated | `?status=&q=&customer_id=` |
| GET | `/legacy-receivables/{id}` | authenticated | detail + riwayat bayar + audit |
| PATCH | `/legacy-receivables/{id}` | owner\|accountant | hanya `notes`, `phone`, `email`, `due_date`, `customer_id`. **Nominal tidak pernah** |
| POST | `/legacy-receivables/{id}/payments` | owner\|accountant\|admin | §6 |
| POST | `/legacy-receivables/{id}/payments/{pid}/void` | **owner** | jurnal pembalik + dokumen JR |
| GET | `/legacy-ar/reconciliation` | authenticated | tie-out akun kontrol vs seluruh sub-ledger AR |

### Yang berubah pada endpoint yang sudah ada
`GET /reports/ar-aging?source=legacy` — hanya karena `receivable.Source.Valid()`
menerima nilai baru. **Tidak ada endpoint aging kedua.** Handler-nya
(`reporting/handler.go:281`) tidak berubah sama sekali; pesan errornya bertambah
satu kata: *"pakai house, realization, legacy, atau all"*.

---

## 12. Alur & Wireframe Frontend

### 12.1 Halaman

| Route | Judul | Isi |
|---|---|---|
| `/accounting/legacy-ar` | Piutang Proyek Lama | daftar + wizard import + riwayat batch |
| `/accounting/legacy-ar/{id}` | detail satu piutang | riwayat bayar, audit, tombol Catat Pembayaran |
| `/accounting/receivable` | **sudah ada** | +1 tab sumber, +1 label, +1 cabang aksi |

Nav: tab baru di `AccountingNav`, **tepat setelah "Piutang"** — bukan di sebelah
Saldo Awal. Ia dibaca bersama piutang berjalan, bukan bersama pembukaan buku.

### 12.2 Wireframe — daftar

```
Piutang Proyek Lama
Saldo piutang dari proyek sebelum sistem ini dipakai. Ditagih dan dilunasi
seperti piutang lain; laporan umur piutangnya tetap satu, di halaman Piutang.

┌────────────────────────────────────────────────────────────────────────────┐
│ Outstanding      Sudah Tertagih    Customer      Cocok dgn Buku Besar      │
│ Rp 217.000.000   Rp 50.000.000     3 orang       ✓ selisih Rp 0            │
└────────────────────────────────────────────────────────────────────────────┘

[ Cari nama / proyek… ]   Status: (Semua)(Belum Lunas)(Lunas)   [+ Import Excel]

┌────────────────────────────────────────────────────────────────────────────┐
│ Customer      Proyek Lama    Ref         Cutoff      Nilai   Sisa   Aksi   │
│ Budi Santoso  Proyek Lama A  LAMA-A-014  31/12/2025  150 jt  100jt [Bayar] │
│ Andi Wijaya   Proyek Lama B  —           31/12/2025   75 jt   75jt [Bayar] │
│ Citra Dewi    Proyek Lama C  LAMA-C-002  31/12/2025   42 jt   42jt [Bayar] │
└────────────────────────────────────────────────────────────────────────────┘
```

Empty state:

> **Belum ada piutang proyek lama**
> Kalau masih ada tagihan dari proyek sebelum sistem ini dipakai, unggah daftarnya
> sekali saja. Yang dibutuhkan cuma **sisa tagihan per customer** — riwayat
> pembayaran lama tidak perlu diisi.
> `[ Unduh Template ]` `[ Import Excel ]`

### 12.3 Wireframe — wizard import (3 langkah)

```
Langkah 1 — Unggah
  Tanggal cutoff  [ 31/12/2025 ]   ← posisi saldo dinyatakan per tanggal ini
  Akun kontrol    [ 1-2000 Piutang Usaha ▾ ]
  Berkas          [ Pilih berkas… ]   (belum punya? [Unduh Template])

Langkah 2 — Pratinjau  ────────────────── 3 baris siap · 1 baris bermasalah
  ✓ 2  Budi Santoso   Proyek Lama A   Rp 150.000.000
  ✓ 3  Andi Wijaya    Proyek Lama B   Rp  75.000.000
  ⚠ 4  Citra Dewi     Proyek Lama C   Rp  42.000.000
        └ Nama & proyek ini sudah pernah diimpor (Batch #2). Duplikat?
  ✕ 5  (kosong)       Proyek Lama D   Rp  10.000.000
        └ Kolom nama_customer wajib diisi
  ┌──────────────────────────────────────────────────────────────────────┐
  │ Ada 1 baris yang harus diperbaiki. Perbaiki berkasnya lalu unggah    │
  │ ulang — sistem tidak mengimpor sebagian.            [Unggah Ulang]   │
  └──────────────────────────────────────────────────────────────────────┘

Langkah 3 — Cocokkan & Konfirmasi
  Saldo 1-2000 di buku besar            Rp 300.000.000
  Sudah ada rinciannya                  Rp  33.000.000
  Akan diimpor batch ini                Rp 267.000.000
  ──────────────────────────────────────────────────
  Selisih setelah import                Rp           0   ✓

  ☐ Ada duplikat yang sudah saya periksa
                                     [Batal]  [Import 3 Baris]
```

Bila selisih **tidak** nol:

```
  Selisih setelah import              Rp 267.000.000   ⚠
  Buku besar belum memuat piutang ini. Neraca akan menampilkan piutang
  Rp 33.000.000 sementara daftar ini Rp 300.000.000.

  ( ) Buat jurnal saldo awal untuk selisih
        Akun lawan [ 3-1000 Modal Disetor ▾ ]   ← usulan dari Saldo Awal Anda
        Dampak: Aset (Piutang) +267 jt · Ekuitas +267 jt · tanpa pendapatan
  ( ) Lanjut tanpa jurnal — alasan: [ ..................... ]
```

### 12.4 Integrasi ke `/accounting/receivable` (perubahan minimal)

Empat baris di `ARAgingView.tsx`:

```tsx
const sourceLabel = { house: "Harga Rumah", realization: "Biaya Realisasi",
                      legacy: "Proyek Lama" };            // +1
filterTabs.push({ value: "legacy", label: "Proyek Lama" }); // +1
<Td mono>{row.unit_code || "—"}</Td>                        // +1 (legacy tak berunit)
// kolom Aksi — cabang ketiga:
row.source === "legacy"
  ? <Link href={`/accounting/legacy-ar/${row.ref_id}`}>Detail →</Link>
  : row.source === "realization" ? …tagihan… : …statement…
```

Cabang aksi memang perlu karena `contract_id`/`unit_id` legacy bernilai 0 — tautan
statement/tagihan akan mengarah ke halaman kosong. Layar sudah bercabang per sumber
sejak W-4, jadi ini melanjutkan pola, bukan membuatnya.

**Customer Statement tidak menampilkan piutang legacy.** Statement ber-anchor
kontrak; legacy tidak punya kontrak. Detail per-customer legacy hidup di
`/accounting/legacy-ar/{id}` — itu **drill-down**, bukan laporan aging kedua, persis
seperti `/penjualan/{unit}/tagihan` untuk realisasi. Halaman itu **tidak** menghitung
bucket, tidak menghitung collection rate, dan tidak menampilkan ringkasan umur.

### 12.5 Modal Catat Pembayaran

```
Catat Pembayaran — Budi Santoso · Proyek Lama A
  Sisa tagihan          Rp 100.000.000
  Tanggal               [ 12/08/2026 ]
  Jumlah                [ Rp 50.000.000 ]     ← ditolak bila > sisa
  Terima di             [ 1-1100 Kas Besar ▾ ]  (CashBankSelect, COA-driven)
  Catatan               [ transfer BCA ]
  ┌──────────────────────────────────────────────────────────────────┐
  │ Dr 1-1100 Kas Besar        50.000.000                            │
  │    Cr 1-2000 Piutang Usaha            50.000.000                 │
  │ Bukti kas BKM akan terbit otomatis. Tidak ada pendapatan diakui. │
  └──────────────────────────────────────────────────────────────────┘
                                        [Batal]  [Catat Pembayaran]
```

Pratinjau jurnal ditampilkan karena inilah satu-satunya cara admin tahu bahwa
pembayaran piutang lama **tidak** menambah pendapatan — salah paham paling mahal
di fitur ini.

---

## 13. Aturan Validasi

### 13.1 Import — per baris

| Kolom | Aturan | Pesan (bahasa layar) |
|---|---|---|
| `nama_customer` | wajib, 2–200 | "Kolom nama_customer wajib diisi" |
| `proyek_lama` | wajib, 1–200 | "Kolom proyek_lama wajib diisi" |
| `outstanding` | wajib, parsable `Money`, **> 0**, ≤ 4 desimal | "Nilai harus lebih besar dari 0" / "Nilai tidak dikenali: `1.5jt`" |
| `outstanding` < 0 | **error, bukan saldo kredit** | "Nilai negatif tidak bisa diimpor. Piutang lebih bayar bukan piutang." |
| `no_referensi` | unik per tenant | "Referensi LAMA-A-014 sudah dipakai piutang #12" |
| `tanggal_jatuh_tempo` | `YYYY-MM-DD`, ≥ 1990, tidak > cutoff + 10 th | "Format tanggal harus YYYY-MM-DD" |
| `email` | format bila diisi | |
| duplikat alami | **warning**, bukan error | "Sudah pernah diimpor (Batch #2). Duplikat?" |

Batch-level: cutoff wajib & tidak boleh di masa depan; akun kontrol wajib anggota
`RoleReceivable`; minimal 1 baris valid; maksimal 5.000 baris.

Nilai negatif ditolak sebagai **invalid input** — ini keputusan yang sudah dikunci
di brief W-6 dan tidak diubah di sini.

### 13.2 Pembayaran

| Aturan | Alasan |
|---|---|
| piutang `status = 'open'` | yang lunas/hapus buku tidak menerima pembayaran |
| `amount > 0` | |
| `amount ≤ outstanding` | K-7 — lebih bayar ditolak |
| akun kas anggota kategori `cash`/`bank` | otoritas COA yang sama dengan `ValidatePaymentAccount` |
| akun kontrol = yang di-snapshot di baris | pelunasan mengkredit akun tempat piutang dibentuk |
| periode akuntansi terbuka | `PeriodChecker` yang sudah ada |
| tanggal tidak di masa depan | |
| baris dikunci `FOR UPDATE` | dua admin membayar bersamaan tidak boleh dobel |

### 13.3 Yang sengaja TIDAK divalidasi

- **Keberadaan customer di master.** Legacy berdiri sendiri (K-6).
- **Keberadaan proyek lama di `projects`.** Proyek itu memang tidak ada.
- **Kecocokan nominal dengan buku besar.** Itu ditampilkan sebagai tie-out, bukan
  dijadikan syarat — memblokirnya akan membuat admin mengarang angka.

---

## 14. Strategi Test

### Unit (tanpa DB)
- Parser template: header hilang, kolom tertukar, sel angka dari Excel dibaca sebagai
  **string** (bukti Invariant #2), pemisah ribuan, nilai negatif, sel kosong.
- `receivable.SourceLegacy.Valid()`, dan **`BuildAging` dengan tiga sumber**
  memastikan urutan `BySource` deterministik.
- Perencanaan pembayaran: `amount = outstanding` → `status` jadi `paid`;
  `amount > outstanding` → error; void → `paid_amount` kembali persis.
- **`Σ(payments) == original_amount`** saat lunas — tes alokasi uang wajib (CLAUDE.md).

### Integration (MySQL, `-tags integration`)
- **Import tidak memposting jurnal**: hitung `journal_entries` sebelum/sesudah commit
  → **selisih 0**. Ini tes yang membuktikan K-1, sepadan dengan tes W-6.
- Commit all-or-nothing: satu baris error → **nol** baris `legacy_receivables`.
- Idempotensi L1/L2/L4: unggah berkas sama → 409; `external_ref` ganda → 409;
  `Idempotency-Key` diulang → hasil pertama, bukan efek kedua.
- Pembayaran: jurnal **balanced**, `document_id` **tidak NULL**, jenis dokumen
  **BKM**, dan `documents.source_id` menunjuk balik ke jurnalnya (INV-DOC-1 dua arah).
- Void: jurnal pembalik ada, dokumen **JR** menunjuk BKM aslinya, `paid_amount` pulih.
- **Isolasi tenant**: tenant B tidak melihat satu baris pun batch/piutang tenant A
  (wajib per CLAUDE.md).
- **Aging gabungan**: house + realization + legacy dalam satu `GET /reports/ar-aging`;
  `total_piutang` = jumlah ketiganya; `?source=legacy` mengembalikan legacy saja.
- Periode tertutup → pembayaran ditolak.

### Yang harus diverifikasi di UI berjalan (bukan dari `tsc`)
Pelajaran `frontend-runtime-fix`: klaim "selesai" hanya sah setelah alur ini
dijalankan di browser — unduh template, unggah berkas rusak, lihat error per baris,
perbaiki, import, cek `/accounting/receivable` menampilkan ketiga sumber, catat
pembayaran, buka Buku Dokumen dan temukan BKM-nya.

---

## 15. Strategi Migrasi

### 15.1 `000069_legacy_ar.up.sql` — satu migrasi, lima tabel

Semua mengikuti konvensi wajib: `id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY`,
`tenant_id BIGINT UNSIGNED NOT NULL`, `created_at`/`updated_at DATETIME(3)`,
index pada `tenant_id`, uang `DECIMAL(20,4) NOT NULL DEFAULT '0.0000'`.

```
legacy_ar_batches           uk_lab_hash (tenant_id, committed_hash)
legacy_ar_batch_rows        idx (tenant_id, batch_id, row_number)
legacy_receivables          uk_lr_extref (tenant_id, external_ref)
                            idx (tenant_id, status, customer_name)
legacy_receivable_payments  uk_lrp_idem (tenant_id, idempotency_key)
                            idx (tenant_id, legacy_receivable_id)
legacy_receivable_audits    idx (tenant_id, legacy_receivable_id, id)
```

`CHECK (original_amount > 0)` dan `CHECK (paid_amount >= 0)` di level DB — pola yang
sama dengan CHECK `cost_tier` di 000035/000036.

### 15.2 Tanpa backfill
Tidak ada data lama yang perlu dipindahkan: fitur ini justru **memasukkan** data
yang selama ini di luar sistem. `down.sql` cukup `DROP TABLE` kelima tabel; tidak ada
kolom tabel lain yang disentuh, tidak ada data existing yang berisiko.

### 15.3 Dependensi baru
`github.com/xuri/excelize/v2` — dibutuhkan untuk **menghasilkan** template dan
**membaca** `.xlsx`. Ini satu-satunya pustaka baru. Alternatif tanpa dependensi
(CSV saja) ditolak karena brief menyebut Excel dan klien akan mengirim `.xlsx`.
`go.sum` di-commit sebelum build produksi.

### 15.4 Urutan implementasi
```
P1  migrasi 000069 + domain + repository + tes isolasi tenant
P2  receivable.SourceLegacy + reader + wiring + tes aging gabungan   ← nilai paling awal
P3  parser + template + staging + pratinjau
P4  commit satu transaksi + idempotensi + tie-out
P5  pembayaran + void + dokumen BKM
P6  frontend: halaman, wizard, modal bayar, +3 baris di ARAgingView
```

P2 sengaja lebih dulu daripada import: begitu satu baris legacy bisa disisipkan
manual dan muncul benar di laporan aging gabungan, seluruh risiko arsitektur W-7
sudah lunas. Sisanya pekerjaan yang bisa diprediksi.

---

## 16. Analisis Risiko & Double-Counting

### R-1 · Double counting piutang — RISIKO TERBESAR
**Skenario.** Saldo Awal sudah memuat `Dr 1-2000 Rp 300 jt`. Import memposting
jurnal lagi → neraca menunjukkan piutang Rp 567 jt untuk uang Rp 300 jt.
**Mitigasi.** K-1: import **tidak pernah** memposting. Ditambah tie-out wajib di
langkah 3 dan tes integrasi yang mengukur `journal_entries` bertambah **0**.
**Sisa risiko.** Rendah. Kalau klien memilih "buat jurnal selisih" padahal Saldo Awal
sudah memuatnya, tie-out akan menunjukkan selisihnya nol dan opsi itu tidak muncul.

### R-2 · Sub-ledger tidak cocok dengan buku besar
**Skenario.** Klien tidak pernah mengisi Saldo Awal. AR report menunjukkan Rp 267 jt,
neraca menunjukkan Rp 0.
**Mitigasi.** Tie-out wajib + `GET /legacy-ar/reconciliation` yang bisa dibuka kapan
saja. Ini justru kontrol yang **belum** dimiliki sistem hari ini, dan W-7 melahirkannya.
**Sisa risiko.** Sedang → mitigasi lanjutan: laporan rekonsiliasi AR menyeluruh
(house + realisasi + legacy vs GL) layak jadi increment sendiri.

### R-3 · Pendapatan tercatat dua kali
**Skenario.** Pelunasan legacy dikredit ke `4-xxxx`.
**Mitigasi.** Kredit **selalu** ke akun kontrol yang di-snapshot di baris piutangnya;
tidak ada jalur kode yang bisa memilih akun pendapatan. Pratinjau jurnal di modal
menyatakannya ke admin. **Sisa risiko.** Sangat rendah.

### R-4 · Import ganda
**Mitigasi.** Empat lapis §8. **Sisa risiko.** Rendah; L3 sengaja peringatan agar
duplikat sah tetap bisa masuk.

### R-5 · Legacy mencemari metrik
**Skenario.** Dashboard/collection rate berubah drastis begitu legacy masuk, dan
owner mengira ada yang rusak.
**Mitigasi.** Legacy punya `source` sendiri sehingga selalu bisa disaring; `by_source`
menampilkan subtotalnya terpisah; catatan kaki menjelaskan sekali.
**Sisa risiko.** Rendah, tapi **perlu dicek**: audit dashboard/laporan lain yang
membaca `RoleReceivable` dari GL akan otomatis ikut naik nilainya begitu jurnal
selisih dibuat — itu benar secara akuntansi, tetapi harus tidak mengagetkan.

### R-6 · Mesin AR kedua menyelinap masuk
**Skenario.** Halaman detail legacy pelan-pelan menumbuhkan bucket sendiri, lalu
subtotal sendiri, lalu jadi laporan kedua yang berselisih dengan yang pertama.
**Mitigasi.** Dinyatakan sebagai aturan di §12.4: halaman detail **tidak** menghitung
bucket/umur/collection rate. Satu-satunya jalan keluar data legacy ke laporan adalah
`ReceivableRows` → `receivable.BuildAging`. **Sisa risiko.** Rendah, tapi ini risiko
yang paling mudah kambuh — layak jadi catatan review.

### R-7 · `float64` menyelinap lewat Excel
**Mitigasi.** §7.3 — hanya `GetCellValue()` (string), tidak pernah `GetCellFloat`.
Nilai mentah disimpan di staging sebagai bukti. Ditegakkan lewat unit test.

### R-8 · Kunci panjang saat commit besar
**Skenario.** 5.000 baris dalam satu transaksi.
**Mitigasi.** Batas 5.000 baris; insert batch; tidak ada jurnal per baris (K-1) —
justru K-1 yang membuat commit besar tetap murah.

---

## BUSINESS DECISION — dua hal yang tidak bisa saya putuskan

### BD-1 · Akun kontrol: pakai `1-2000` bersama, atau akun terpisah?

| Opsi | Konsekuensi |
|---|---|
| **A. Pakai `1-2000` Piutang Usaha (default desain ini)** | Tidak ada perubahan COA. Neraca menampilkan satu angka piutang. Pemisahan legacy vs berjalan hanya ada di laporan, tidak di neraca. |
| **B. Akun baru, mis. `1-2050 Piutang Proyek Lama`** | Neraca langsung memperlihatkan berapa piutang yang berasal dari proyek lama — berguna bila risiko tak tertagihnya berbeda. Butuh penambahan akun ke COA **dan** ke `ledger.RoleReceivable` (`account_role.go:46`, satu baris). |

Desain ini berjalan pada keduanya karena akun kontrol di-snapshot per baris.
Yang dibutuhkan hanya jawaban klien/akuntan: **apakah piutang proyek lama perlu
tampil terpisah di neraca?** Kalau tidak dijawab, desain berjalan dengan Opsi A.

### BD-2 · Penyisihan piutang tak tertagih

Piutang dari proyek lama yang belum selesai punya risiko gagal tagih yang secara
material berbeda dari piutang buku berjalan. Apakah perlu penyisihan (CKPN /
penyisihan piutang ragu-ragu), berapa persen, dan berdasarkan umur berapa —
adalah keputusan akuntan, bukan keputusan arsitektur.

W-7 **tidak** mengimplementasikan penyisihan apa pun. Yang disiapkan hanyalah
ruangnya: kolom `status` sudah mengenal nilai `written_off`, dan hapus buku nantinya
adalah jurnal biasa (`Dr Beban Piutang Tak Tertagih / Cr <akun kontrol>`) dengan
audit event `written_off` — tanpa mengubah satu pun struktur di dokumen ini.

`// TODO(tax-advisor): perlakuan pajak atas penghapusan piutang proyek lama —
syarat fiskal penghapusan piutang menurut PPh badan belum dikonfirmasi.`

---

## Ringkasan sentuhan kode (perkiraan)

| Area | Sifat |
|---|---|
| `internal/legacyar/**` | **baru** — bounded context, ±6 berkas |
| `internal/receivable/receivable.go` | **+3 baris** — konstanta, `Valid()`, urutan |
| `internal/reporting/service.go` | **+1 interface, +1 setter** |
| `internal/reporting/ar_aging.go` | **+1 cabang** simetris |
| `internal/reporting/handler.go` | **+1 kata** di pesan error |
| `migrations/000069_legacy_ar.*` | **baru** |
| `cmd/api/main.go` | **+1 baris** wiring |
| `frontend/**` | 2 halaman baru + wizard + modal; **+3 baris** di `ARAgingView.tsx` |
| `go.mod` | **+1** `excelize/v2` |

Nol perubahan pada `sale`, `charge`, `document`, `ledger`, `histfin`, `customer`.
Itu ukuran paling jujur bahwa seam-nya memang sudah benar sejak W-4.
