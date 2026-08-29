# W-6 & W-7 — Snapshot Keuangan Historis + Impor Piutang Lama

**Status: USULAN DESAIN — belum disetujui, belum ada kode.**
Tanggal review: 2026-08-10. Basis: kondisi repo setelah W-5 (J-14a) selesai.

Dokumen ini menjawab dua kebutuhan bisnis baru dan **menolak** beberapa asumsi
yang tersirat di dalam permintaan, dengan alasan yang dijelaskan. Bagian §0
merangkum keputusan yang butuh persetujuan sebelum implementasi dimulai.

---

## 0 · Ringkasan keputusan & yang butuh persetujuan

**Rekomendasi utama: DUA domain terpisah, bukan satu.**

| | W-6 Snapshot Historis | W-7 Piutang Lama |
|---|---|---|
| Hakikat data | Angka **laporan** masa lalu | **Saldo hidup** yang akan ditagih |
| Masuk ledger? | **TIDAK** | **YA** |
| Mesin yang dipakai | Baru, kecil, terisolasi | Mesin AR yang **sudah ada** |
| Bisa dibayar? | Tidak relevan | Ya, lewat kas/bank + kwitansi |
| Risiko ke angka aktif | Nol (tidak dibaca siapa pun) | Nyata → dijaga invarian |
| Paket | `internal/histfin` | `internal/legacyar` |

Menggabungkan keduanya jadi satu modul "Migrasi" akan memaksa satu mesin melayani
dua kebenaran yang berbeda: satu yang tidak boleh menyentuh ledger, satu yang
wajib menyentuhnya. Yang layak disatukan hanya **pintu masuk UI-nya**.

### Keputusan yang saya minta persetujuannya

| # | Keputusan | Rekomendasi saya |
|---|---|---|
| **D-1** | Akun lawan untuk piutang hasil impor | Akun baru **`3-9000 Ekuitas Saldo Awal`** (suspense migrasi), bukan langsung `3-2000` |
| **D-2** | Pustaka Excel | Tambah dependensi **`github.com/xuri/excelize/v2`**; fallback CSV bila ditolak |
| **D-3** | Snapshot boleh untuk tahun berjalan? | **Tidak.** `tahun_snapshot < tahun go-live`, fail-closed |
| **D-4** | Jenis dokumen kwitansi piutang lama | Jenis baru **`KWL`** (masuk `cashDocumentTypes`), bukan menumpang KWT |
| **D-5** | Kelebihan bayar piutang lama | **Ditolak** — tidak ada saldo kredit untuk customer tanpa kontrak |
| **D-6** | Hapus buku (write-off) piutang lama | **Masuk lingkup**, butuh akun baru `5-4600 Beban Kerugian Piutang` |
| **D-7** | Urutan rilis | **W-6 dulu** (kecil, nol risiko ledger), baru W-7 dalam 4 fase |
| **D-8** | Laba/rugi tahun berjalan di snapshot Neraca | **Dihitung sistem** dari Laba Rugi tahun yang sama, tidak diketik admin |

Satu pertanyaan akuntansi yang harus dijawab **klien**, bukan saya — lihat
**EDGE-6** (§14): bagaimana memperlakukan Laba Rugi tahun go-live bila sistem
mulai dipakai di tengah tahun.

---

# BAGIAN I — W-6 · SNAPSHOT KEUANGAN HISTORIS

## 1 · Business rules

| Kode | Aturan |
|---|---|
| S-1 | Satu snapshot = satu **tahun buku penuh** milik satu tenant. Kunci unik `(tenant_id, fiscal_year)`. |
| S-2 | Satu snapshot memuat **dua laporan**: Neraca per 31 Desember tahun itu, dan Laba Rugi periode 1 Jan–31 Des tahun itu. Bukan akumulasi antar tahun. |
| S-3 | Tahun bebas — 2019, 2021, 2024, tidak harus berurutan, tidak harus lengkap. Tidak ada batas jumlah. |
| S-4 | `fiscal_year` **wajib lebih kecil** dari tahun go-live sistem. Tahun yang sudah punya jurnal posted ditolak (fail-closed). |
| S-5 | Angka diisi **per akun COA**, bukan per kelompok bebas. Yang muncul di laporan adalah akun yang diisi saja. |
| S-6 | Status: `draft` → `final`. Draft boleh tidak seimbang. **Final wajib** `Aset = Kewajiban + Ekuitas` persis. |
| S-7 | Laba/rugi tahun berjalan **tidak diketik** — dihitung `Σ Pendapatan − Σ Beban` dari Laba Rugi tahun yang sama, lalu disisipkan sebagai baris ekuitas saat validasi & penyajian (D-8). |
| S-8 | Snapshot `final` **read-only**. Mengubahnya wajib "Buka Kembali" bersama alasan → status kembali `draft`, revisi naik, jejak audit tercatat. |
| S-9 | Snapshot **tidak pernah** dibaca oleh dashboard, trial balance, tutup buku, AR, HPP, atau perhitungan apa pun. Konsumennya hanya layar laporan historis (daftar putih di §12). |
| S-10 | Tidak ada arus kas historis. Tidak ada detail transaksi. Tidak ada jurnal. |

## 2 · Accounting treatment

**Tidak ada.** Snapshot tidak menghasilkan jurnal, tidak menyentuh `journal_entries`,
`journal_lines`, atau saldo akun mana pun.

Alasannya bukan kemalasan, ini konsekuensi arsitektur yang ada sekarang:

1. **Laba Rugi di sistem ini kumulatif sampai `as_of`** (`queryPLRows`, tanpa
   batas bawah kecuali diminta). Memposting pendapatan 2023/2024/2025 akan
   menaikkan Laba Rugi berjalan **kecuali** setiap tahun juga ditutup dengan
   entri penutup. Artinya: merekonstruksi buku besar tiga tahun ke belakang —
   persis "sistem migrasi accounting historis yang kompleks" yang tidak
   diinginkan.
2. **Dua kebenaran untuk satu tanggal.** Jurnal saldo awal go-live sudah
   menyatakan posisi neraca pembuka. Snapshot tahun terakhir menyatakan hal
   yang sama. Bila keduanya masuk ledger, saldo terhitung dua kali.
3. Klien secara eksplisit tidak butuh histori transaksi. Data pelaporan
   dilayani paling murah oleh tabel pelaporan.

**Hubungan dengan Saldo Awal go-live:** neraca penutup snapshot tahun terakhir
*seharusnya* sama dengan jurnal saldo awal. Sistem **menampilkan perbandingan**
("Snapshot 2025 vs Saldo Awal sistem") dan memberi peringatan bila berbeda —
tapi **tidak** menyalin, tidak memposting, tidak memaksa. Menyalin otomatis
adalah kopling yang justru sedang dihindari (roadmap, bukan v1).

## 3 · Domain / bounded context

Bounded context baru: **Historical Financials**. Paket `internal/histfin`.

```
internal/histfin/
  model.go       → Snapshot, SnapshotLine, Statement, Status
  service.go     → validasi keseimbangan, transisi status (tanpa akses DB langsung)
  repository.go  → GORM, global scope tenant
  handler.go     → HTTP
  report.go      → penyusun NeracaHistoris / PLHistoris (fungsi murni)
```

Aturan ketergantungan:
- `histfin` **boleh** import `domain` dan membaca `ledger.Account` (klasifikasi
  tipe akun untuk seksi laporan). Read-only, tanpa posting.
- Tidak ada paket lain yang boleh import `histfin` — kecuali `cmd/api` untuk
  pemasangan rute. Ini yang membuat "tidak mengganggu ledger" bisa dibuktikan
  secara mekanis, bukan sekadar dijanjikan.

## 4 · Entity & aggregate

**Aggregate root: `FinancialSnapshot` (per tahun).** Baris hanya hidup di
dalamnya; tidak ada operasi yang menyentuh baris tanpa lewat root (yang menjaga
S-6/S-7).

```go
type Snapshot struct {
    ID, TenantID uint64
    FiscalYear   int              // unik per tenant
    Status       Status           // draft | final
    Revision     int              // naik setiap kali dibuka-kembali lalu difinalkan
    Notes        string
    FinalizedAt  *time.Time
    FinalizedBy  *uint64
    CreatedBy, UpdatedBy *uint64
}

type SnapshotLine struct {
    ID, TenantID, SnapshotID uint64
    Statement   Statement       // balance_sheet | income_statement (diturunkan dari tipe akun)
    AccountID   uint64          // FK logis ke accounts
    AccountCode string          // SNAPSHOT — laporan lama tidak berubah bila COA diubah
    AccountName string          // SNAPSHOT
    AccountType string          // SNAPSHOT (aset/kewajiban/ekuitas/pendapatan/beban)
    Amount      domain.Money    // arah normal akun; DECIMAL(20,4)
    SortOrder   int
}
```

Snapshot kode/nama/tipe akun mengikuti konvensi yang sudah dipakai di repo
(`ChargeItem.DepositAccountCode`): dokumen historis tidak boleh berubah isinya
karena master diedit setelahnya.

**Amount selalu positif dalam arah normal akun.** Admin mengisi "Kas 150.000.000",
bukan debit/kredit. Kontra-akun (`1-4900 Akumulasi Penyusutan`) tetap diisi
positif dan diperlakukan sebagai pengurang oleh tipe akunnya — sama seperti
`ComputeTrialBalance` sekarang.

## 5 · ERD konseptual

```
tenants ──1:N── financial_snapshots ──1:N── financial_snapshot_lines
                        │                              │
                        └──1:N── financial_snapshot_audits
                                                       └── (logical FK) accounts
```

Migrasi **000068**:

```sql
CREATE TABLE financial_snapshots (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  tenant_id BIGINT UNSIGNED NOT NULL,
  fiscal_year SMALLINT NOT NULL,
  status VARCHAR(10) NOT NULL DEFAULT 'draft',
  revision INT NOT NULL DEFAULT 0,
  notes VARCHAR(500) NULL,
  finalized_at DATETIME(3) NULL, finalized_by BIGINT UNSIGNED NULL,
  created_by BIGINT UNSIGNED NULL, updated_by BIGINT UNSIGNED NULL,
  created_at DATETIME(3), updated_at DATETIME(3),
  UNIQUE KEY uk_snapshot_year (tenant_id, fiscal_year),
  KEY idx_snapshot_tenant (tenant_id),
  CONSTRAINT chk_snapshot_status CHECK (status IN ('draft','final'))
);

CREATE TABLE financial_snapshot_lines (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  tenant_id BIGINT UNSIGNED NOT NULL,
  snapshot_id BIGINT UNSIGNED NOT NULL,
  statement VARCHAR(20) NOT NULL,
  account_id BIGINT UNSIGNED NOT NULL,
  account_code VARCHAR(20) NOT NULL,
  account_name VARCHAR(150) NOT NULL,
  account_type VARCHAR(20) NOT NULL,
  amount DECIMAL(20,4) NOT NULL DEFAULT '0.0000',
  sort_order INT NOT NULL DEFAULT 0,
  created_at DATETIME(3), updated_at DATETIME(3),
  UNIQUE KEY uk_snapshot_account (snapshot_id, account_id),
  KEY idx_snapshot_lines_tenant (tenant_id)
);

CREATE TABLE financial_snapshot_audits (   -- append-only
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  tenant_id BIGINT UNSIGNED NOT NULL,
  snapshot_id BIGINT UNSIGNED NOT NULL,
  event VARCHAR(20) NOT NULL,            -- created|saved|finalized|reopened
  revision INT NOT NULL,
  reason VARCHAR(500) NULL,              -- WAJIB untuk reopened
  total_assets DECIMAL(20,4) NOT NULL DEFAULT '0.0000',
  total_liab_equity DECIMAL(20,4) NOT NULL DEFAULT '0.0000',
  net_income DECIMAL(20,4) NOT NULL DEFAULT '0.0000',
  actor_id BIGINT UNSIGNED NULL,
  created_at DATETIME(3), updated_at DATETIME(3),
  KEY idx_snapshot_audit (tenant_id, snapshot_id)
);

ALTER TABLE tenants ADD COLUMN go_live_date DATE NULL;  -- S-4
```

Jejak audit menyimpan **total**, bukan salinan seluruh baris: tidak ada uang yang
bergerak di sini, jadi yang perlu bisa dipertanggungjawabkan adalah "siapa
mengubah laporan tahun berapa, kapan, jadi berapa" — bukan diff per akun.

## 6 · Lifecycle

```
     (buat tahun)                (isi/simpan berulang)
  ──────────────────► draft ◄──────────────────────────┐
                        │                              │
                        │ finalkan (seimbang wajib)    │ buka kembali + alasan
                        ▼                              │ (owner saja, revision++)
                      final ─────────────────────────►─┘
```

Tidak ada `deleted`. Snapshot yang salah tahun boleh dihapus **hanya** selagi
`draft` dan belum pernah `final`.

## 7 · Reporting

- `GET /histfin/snapshots` → daftar tahun + status + total.
- `GET /histfin/snapshots/{year}` → header + baris (untuk editor).
- `GET /histfin/snapshots/{year}/neraca` → bentuk **`NeracaReport` yang sama**
  dengan laporan hidup, ditambah `is_historical: true` + `source: "snapshot"`.
- `GET /histfin/snapshots/{year}/laba-rugi` → bentuk `PLReport` yang sama.

Memakai *bentuk payload* yang sama membuat komponen `NeracaSection` / `PLSection`
di frontend dipakai ulang apa adanya. Yang berbeda hanya banner "Data Historis".

## 8 · Jawaban langsung atas pertanyaan arsitektur (bagian A)

| Pertanyaan | Jawaban |
|---|---|
| Bounded context baru atau bagian Accounting? | **Baru** (`internal/histfin`). Accounting adalah mesin transaksi; ini data pelaporan. Menaruhnya di `ledger` akan membuat paket posting punya jalur yang tidak memposting. |
| Masuk ledger atau cukup reporting data? | **Cukup reporting data.** Alasan penuh di §2. |
| Bagaimana memastikan tidak mengganggu ledger aktif? | Tiga lapis: (a) tabel terpisah, tidak ada jurnal; (b) tidak ada paket lain yang import `histfin` — dijaga uji arsitektur; (c) `fiscal_year < tahun go-live`, dan tahun yang punya jurnal posted ditolak. |
| Validasi Aktiva = Liabilitas + Ekuitas | Dihitung saat simpan (indikator langsung) dan **ditegakkan saat finalisasi**. Ekuitas mencakup laba tahun berjalan hasil hitung (S-7). Draft boleh timpang; final tidak. |
| Hubungan laba/rugi tahun itu dengan Neraca tahun itu | Satu angka, satu sumber: `Σ Pendapatan − Σ Beban` dari Laba Rugi tahun itu **adalah** baris "Laba/Rugi Tahun Berjalan" di Neraca tahun itu. Admin tidak boleh mengetiknya (D-8). Pemeriksaan lunak antar tahun: `Laba Ditahan(N) ≈ Laba Ditahan(N−1) + Laba(N−1)` → **peringatan saja**, karena dividen dan koreksi tahun lalu itu nyata. |
| Bagaimana bila admin hanya mengisi beberapa akun? | Boleh, statusnya tetap `draft`; laporan hanya menampilkan akun yang terisi; finalisasi ditolak sampai seimbang. Sistem **tidak pernah** menambal selisih ke akun penyeimbang otomatis — angka karangan lebih berbahaya daripada laporan yang belum selesai (aturan ketidakpastian CLAUDE.md). |
| Membedakan historis vs aktif | Tabel berbeda, endpoint berbeda (`/histfin/*`), badge "Data Historis" di setiap layar, dan batas tahun yang keras. Tidak ada satu pun query aktif yang menyentuh tabel snapshot. |
| Audit trail & siapa yang boleh mengubah | `financial_snapshot_audits` append-only. Tulis = `owner` + `accountant`; **finalkan & buka-kembali = `owner` saja**; `viewer` baca saja. |
| Perlu lock/finalize per tahun? | **Ya.** Tanpa itu tidak ada bedanya laporan yang sudah disepakati dengan draf setengah jadi. |
| Bila 2025 sudah ada lalu dikoreksi? | `final` → "Buka Kembali" + alasan wajib → `draft` → koreksi → finalkan lagi, `revision` naik, dua baris audit tercatat. Angka lama tidak disimpan sebagai versi penuh (bukan ledger); yang disimpan adalah siapa/kapan/mengapa + total sebelum-sesudah. |

## 9 · Frontend spec W-6

### Halaman & navigasi

| Rute | Isi |
|---|---|
| `/migrasi` | Hub "Migrasi & Data Historis" — 2 kartu: *Snapshot Keuangan Tahunan*, *Impor Piutang Lama*. Ditautkan dari Pengaturan → Akuntansi & Lainnya. |
| `/migrasi/snapshot` | Daftar tahun + tombol "Tambah Tahun". |
| `/migrasi/snapshot/{year}` | Editor snapshot (2 tab). |
| `/laporan` | Pemilih tahun mendapat opsi historis, dengan badge. |

### Wireframe — daftar tahun

```
┌ Snapshot Keuangan Tahunan ───────────────────── [ + Tambah Tahun ] ┐
│ Kondisi keuangan sebelum sistem ini dipakai. Angka di sini TIDAK   │
│ masuk buku besar dan tidak memengaruhi laporan berjalan.          │
├───────┬──────────┬───────────────┬───────────────┬────────────────┤
│ Tahun │ Status   │ Total Aset    │ Laba/Rugi     │                │
│ 2025  │ ● Final  │ 4.250.000.000 │   450.000.000 │ [Lihat] [Buka] │
│ 2024  │ ● Final  │ 3.100.000.000 │   310.000.000 │ [Lihat] [Buka] │
│ 2023  │ ○ Draft  │ 2.000.000.000 │   180.000.000 │ [Lanjutkan]    │
└───────┴──────────┴───────────────┴───────────────┴────────────────┘
Empty state: "Belum ada snapshot. Mulai dari tahun terakhir sebelum
sistem dipakai (2025) agar Neraca pembuka bisa dibandingkan."  [+ Tambah 2025]
```

### Wireframe — editor

```
┌ Snapshot 2025            ○ Draft · revisi 1      [Simpan] [Finalkan] ┐
│ ( Neraca )  ( Laba Rugi )                                            │
├──────────────────────────────────────────────────────────────────────┤
│ AKTIVA                                            │ + Tambah Akun    │
│  1-1100 Kas — Kas Besar .................. [ 150.000.000 ]  [x]      │
│  1-1300 Bank — BCA ....................... [ 900.000.000 ]  [x]      │
│  1-2000 Piutang Usaha .................... [ 245.000.000 ]  [x]      │
│  1-3100 Persediaan — Hard Cost ........... [2.955.000.000]  [x]      │
│                                    Total Aktiva  4.250.000.000       │
│ KEWAJIBAN & EKUITAS                                                  │
│  2-1000 Hutang Usaha ..................... [ 300.000.000 ]  [x]      │
│  3-1000 Modal Disetor .................... [3.000.000.000]  [x]      │
│  3-2000 Laba Ditahan ..................... [ 500.000.000 ]  [x]      │
│  3-3000 Laba/Rugi Tahun Berjalan ......... ( 450.000.000 ) 🔒 dihitung│
│                          Total Kewajiban+Ekuitas  4.250.000.000      │
├──────────────────────────────────────────────────────────────────────┤
│ ✓ Seimbang — selisih Rp 0                        [Finalkan] aktif    │
└──────────────────────────────────────────────────────────────────────┘
```

Bila timpang, baris bawah berubah jadi peringatan:
`⚠ Belum seimbang — Aktiva lebih besar Rp 25.000.000. Finalisasi dinonaktifkan.`
Tombol Finalkan disabled + `title` menjelaskan sebabnya (bukan diam-diam mati).

### State & aksi

| State | Perilaku |
|---|---|
| Kosong | CTA informatif menyebut tahun yang disarankan (memori `feedback-erp-business-flow`). |
| Draft | Semua field editable; badge abu; tombol Finalkan bergantung keseimbangan. |
| Final | Seluruh field read-only; badge hijau; hanya `owner` melihat "Buka Kembali". |
| Buka Kembali | Modal wajib alasan (min. 10 karakter) sebelum tombol aktif. |
| Error simpan | Toast merah dengan pesan server apa adanya. |

### Endpoint yang dipakai layar

`GET|POST /histfin/snapshots` · `GET|PUT /histfin/snapshots/{year}` ·
`POST /histfin/snapshots/{year}/finalize` · `POST /histfin/snapshots/{year}/reopen` ·
`GET /histfin/snapshots/{year}/neraca|laba-rugi` · `GET /histfin/snapshots/{year}/audits`

---

# BAGIAN II — W-7 · IMPOR PIUTANG LAMA

## 10 · Business rules

| Kode | Aturan |
|---|---|
| L-1 | Yang diimpor **hanya saldo outstanding per customer**, bukan histori pembayaran, bukan histori tagihan. |
| L-2 | Piutang hasil impor adalah **piutang customer yang sama** dengan piutang lain — akun `1-2000`, mesin aging yang sama. Tidak ada mekanisme piutang kedua. (Konsisten dengan D-3 klien di W-5.) |
| L-3 | Customer lama **tidak wajib** punya kontrak, unit, sale, invoice, atau jadwal cicilan. |
| L-4 | Satu batch impor = satu `as_of_date` (tanggal cutoff migrasi) = tanggal jurnalnya. Bukan per baris. |
| L-5 | Impor **dua fase**: pratinjau (tidak menyentuh apa pun) → konfirmasi (satu transaksi, satu jurnal). |
| L-6 | File yang sama tidak bisa diimpor dua kali (hash file). Baris yang sama tidak bisa menggandakan saldo (kunci bisnis). |
| L-7 | Pembayaran piutang lama memakai jalur kas yang sama: jurnal + **kwitansi bernomor** (INV-DOC-1). |
| L-8 | Kelebihan bayar **ditolak** (D-5). Customer tanpa kontrak tidak punya mesin saldo kredit. |
| L-9 | Batch bisa **dibatalkan** (jurnal pembalik) **hanya** bila belum ada satu pun pembayaran pada baris-barisnya. |
| L-10 | Hapus buku memerlukan alasan + peran `owner`, dan memposting beban kerugian piutang (D-6). |

## 11 · Accounting treatment

### Saat impor (satu jurnal per batch, `source = 'opening_balance'`)

```
Tanggal: as_of_date batch          Referensi: IMP-{batch_id}
  Dr  1-2000 Piutang Usaha   150.000.000   (Customer A — ref KAV-12)
  Dr  1-2000 Piutang Usaha    75.000.000   (Customer B — ref KAV-31)
  Dr  1-2000 Piutang Usaha    20.000.000   (Customer C)
      Cr  3-9000 Ekuitas Saldo Awal            245.000.000
```

**Mengapa `3-9000` dan bukan `3-2000` (D-1).** Piutang lama bukan *tambahan*
kekayaan — ia **bagian dari saldo awal** yang kebetulan masuk lewat pintu lain.
Bila dikreditkan langsung ke Laba Ditahan, dan admin juga mengetik total piutang
di layar Saldo Awal, ekuitas menggelembung tanpa jejak. Dengan akun perantara:

> Setelah seluruh saldo awal masuk (piutang lewat importer, sisanya lewat layar
> Saldo Awal yang memakai `3-9000` sebagai penyeimbang), **saldo `3-9000` harus
> nol**. Tidak nol = migrasi belum selesai atau ada yang dobel.

Ini pola standar *Opening Balance Equity* dan memberi satu angka yang bisa
dipantau, bukan sekadar keyakinan. Layar Saldo Awal mendapat widget kecil:
`Ekuitas Saldo Awal (3-9000): Rp 245.000.000 — belum nol, migrasi belum tuntas.`

Satu baris debit **per customer** (bukan satu baris agregat) supaya buku besar
`1-2000` bisa dibaca per customer tanpa membuka tabel lain.

Jurnal ini bukan pergerakan kas → tidak tunduk INV-DOC-1. Memakai `source =
'opening_balance'` yang sudah ada berarti **nol perubahan** pada logika audit
dokumen (`document.SourceOpeningBalance` sudah dikecualikan).

### Saat pembayaran

```
Tanggal: tanggal terima            Dokumen: KWL/2026/000001
  Dr  1-1300 Bank — BCA        30.000.000
      Cr  1-2000 Piutang Usaha             30.000.000
```

Sama persis dengan pola pelunasan piutang yang sudah ada — hanya sumber barisnya
yang berbeda. Kwitansi wajib (INV-DOC-1), jenis dokumen baru `KWL` (D-4) supaya
Buku Dokumen bisa membedakan "kwitansi rumah" dari "kwitansi piutang lama".

### Saat hapus buku (opsional, D-6)

```
  Dr  5-4600 Beban Kerugian Piutang   20.000.000
      Cr  1-2000 Piutang Usaha                     20.000.000
```

### Saat pembatalan batch (L-9)

Jurnal pembalik penuh atas jurnal impor, baris `legacy_receivables` → `cancelled`.
Ledger tetap append-only (Invariant #5).

## 12 · Customer identity

**Rekomendasi: customer biasa + metadata sumber. Tidak ada entity baru.**

```sql
ALTER TABLE customers
  ADD COLUMN source VARCHAR(20) NOT NULL DEFAULT 'system',   -- system | legacy_import
  ADD COLUMN external_ref VARCHAR(50) NULL;                  -- kode di sistem/berkas lama
CREATE UNIQUE INDEX uk_customer_external_ref ON customers (tenant_id, external_ref);
```

Alasan:
- Layar AR, statement, dan penagihan sudah tahu cara menampilkan `customers`.
  Entity kedua berarti setiap layar harus tahu dua jenis customer selamanya.
- Customer lama yang **beli unit baru** adalah kejadian yang diinginkan bisnis.
  Dengan satu master, itu cukup membuat kontrak baru atas customer yang sama —
  dan piutangnya menyatu di satu daftar. Dengan entity terpisah, dia jadi dua
  orang yang berbeda di mata sistem.
- `source` hanyalah asal-usul, bukan perilaku. Tidak ada satu pun cabang logika
  yang bergantung padanya selain pelabelan UI dan penyaringan.

**Pencocokan saat impor:** `external_ref` (persis) → nama ternormalisasi
(huruf kecil, spasi tunggal) → tidak ketemu = customer baru. Pratinjau selalu
menyebut mana yang "cocok dengan customer yang sudah ada" dan mana yang "akan
dibuat baru"; admin bisa memaksa "buat baru" per baris. Kode customer baru
dibuat otomatis (`LGC-000001`) bila template tidak mengisinya.

## 13 · Entity, ERD, & integrasi mesin AR

```
customers ──1:N── legacy_receivables ──1:N── legacy_receivable_payments
     ▲                    ▲                            │
     │                    │                            └── journal_entries + documents
legacy_import_batches ────┘
     └──1:N── legacy_import_batch_rows   (staging pratinjau, dibuang setelah commit)
```

Migrasi **000069**:

```sql
CREATE TABLE legacy_import_batches (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  tenant_id BIGINT UNSIGNED NOT NULL,
  status VARCHAR(20) NOT NULL DEFAULT 'preview',   -- preview|committed|reversed|discarded
  file_name VARCHAR(255) NOT NULL,
  file_sha256 CHAR(64) NOT NULL,
  template_version VARCHAR(10) NOT NULL,
  as_of_date DATE NOT NULL,
  row_count INT NOT NULL DEFAULT 0,
  total_amount DECIMAL(20,4) NOT NULL DEFAULT '0.0000',
  journal_entry_id BIGINT UNSIGNED NULL,           -- terisi saat committed
  reversal_journal_id BIGINT UNSIGNED NULL,
  committed_at DATETIME(3) NULL, committed_by BIGINT UNSIGNED NULL,
  created_by BIGINT UNSIGNED NULL,
  created_at DATETIME(3), updated_at DATETIME(3),
  UNIQUE KEY uk_batch_file (tenant_id, file_sha256),
  KEY idx_batch_tenant (tenant_id)
);

CREATE TABLE legacy_receivables (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  tenant_id BIGINT UNSIGNED NOT NULL,
  customer_id BIGINT UNSIGNED NOT NULL,
  import_batch_id BIGINT UNSIGNED NOT NULL,
  legacy_project VARCHAR(150) NULL,
  legacy_reference VARCHAR(100) NULL,              -- no. kontrak/kavling lama
  original_amount DECIMAL(20,4) NOT NULL DEFAULT '0.0000',  -- immutable
  paid_amount    DECIMAL(20,4) NOT NULL DEFAULT '0.0000',
  written_off_amount DECIMAL(20,4) NOT NULL DEFAULT '0.0000',
  as_of_date DATE NOT NULL,
  due_date   DATE NOT NULL,                        -- default = as_of_date
  status VARCHAR(20) NOT NULL DEFAULT 'open',      -- open|settled|written_off|cancelled
  notes VARCHAR(500) NULL,
  created_by BIGINT UNSIGNED NULL,
  created_at DATETIME(3), updated_at DATETIME(3),
  KEY idx_legacy_tenant (tenant_id),
  KEY idx_legacy_customer (tenant_id, customer_id),
  UNIQUE KEY uk_legacy_ref (tenant_id, customer_id, legacy_reference)
);

CREATE TABLE legacy_receivable_payments (          -- append-only
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  tenant_id BIGINT UNSIGNED NOT NULL,
  legacy_receivable_id BIGINT UNSIGNED NOT NULL,
  amount DECIMAL(20,4) NOT NULL DEFAULT '0.0000',
  paid_at DATE NOT NULL,
  cash_account_code VARCHAR(20) NOT NULL,
  journal_entry_id BIGINT UNSIGNED NOT NULL,
  document_id BIGINT UNSIGNED NOT NULL,
  reference VARCHAR(100) NULL, notes VARCHAR(500) NULL,
  idempotency_key VARCHAR(100) NULL,
  created_by BIGINT UNSIGNED NULL,
  created_at DATETIME(3), updated_at DATETIME(3),
  KEY idx_legacy_pay (tenant_id, legacy_receivable_id),
  UNIQUE KEY uk_legacy_pay_idem (tenant_id, idempotency_key)
);
```

`legacy_import_batch_rows` (staging pratinjau) menyimpan hasil parsing + hasil
validasi per baris; dihapus setelah commit/discard.

### Integrasi ke mesin AR — inilah bagian terpentingnya

Paket `receivable` (W-4) sudah dirancang untuk ini: ia menerima `Row` dari
**penghasil mana pun**. Yang dibutuhkan hanya **satu nilai enum baru**:

```go
const SourceLegacy Source = "legacy"     // piutang proyek lama hasil impor
```

lalu `legacyar` menyediakan `ReceivableRows(ctx, tenantID) []receivable.Row`
persis seperti `charge` melakukannya untuk biaya realisasi, dan
`reporting.GetARAging` menambahkannya sebagai sumber ketiga.

**Tidak ada mesin aging baru, tidak ada laporan piutang kedua, tidak ada
perhitungan uang baru.** Ini alasan utama mengapa saya menolak menyimpan angka
di tabel terpisah yang tidak tersambung ke AR — dan mengapa desain ini tidak
melakukannya.

Pemetaan `Row` untuk baris legacy:

| Field `receivable.Row` | Isi untuk legacy |
|---|---|
| `Source` | `legacy` |
| `ContractID`, `UnitID` | `0` — UI harus tahan terhadap nol (tidak ada tautan unit) |
| `RefID` | `legacy_receivables.id` |
| `BuyerName` | nama customer |
| `UnitCode` | `legacy_project` atau `"—"` |
| `Label` | `legacy_reference` / "Piutang proyek lama" |
| `InvoiceNumber` | `"—"` (memang tidak ada dokumen tagihan lama) |
| `DueDate` | `due_date` |
| `Amount` / `PaidAmount` | `original_amount` / `paid_amount` |

### Invarian baru

| Kode | Invarian |
|---|---|
| **INV-LEG-1** | `Σ (original_amount − paid_amount − written_off_amount)` untuk baris `open` == kontribusi legacy pada saldo `1-2000`. |
| **INV-LEG-2** | Setiap pembayaran piutang lama punya **tepat satu** jurnal dan **tepat satu** dokumen bernomor (perluasan INV-DOC-1). |
| **INV-AR-4** | `Σ outstanding aging(semua sumber) == saldo 1-2000`. Inilah penjaga sesungguhnya terhadap "mekanisme piutang kedua". |

## 14 · Import flow

```
1. UNDUH TEMPLATE   GET /legacy-receivables/import/template  → .xlsx bernama versi
2. UNGGAH           POST /legacy-receivables/import (multipart + as_of_date)
                    → parse, validasi, simpan staging, status=preview
                    → TIDAK ADA jurnal, TIDAK ADA customer dibuat
3. PRATINJAU        GET /legacy-receivables/import/{batch}/preview
                    → per baris: valid / error / peringatan duplikat / cocok customer
4. KONFIRMASI       POST /legacy-receivables/import/{batch}/commit
                    → SATU transaksi: buat customer baru → buat legacy_receivables
                      → posting SATU jurnal → status=committed
5. HASIL            ringkasan: n baris, m customer baru, total, no. jurnal
6. (opsional) BATAL POST /legacy-receivables/import/{batch}/reverse   (L-9)
```

### Template resmi (hasil review kolom klien)

| Kolom | Wajib | Catatan review |
|---|---|---|
| `nama_customer` | ✔ | Kunci pencocokan cadangan. |
| `outstanding` | ✔ | **Sisa** yang masih ditagih. Bukan nilai kontrak, bukan yang sudah dibayar. |
| `kode_customer` | – | = *Customer Code / External Reference* usulan klien. Kunci pencocokan utama; sangat disarankan diisi. |
| `proyek_lama` | – | Teks bebas. **Bukan** FK ke `projects` — proyek lama tidak ada di sistem dan tidak boleh dibuat-buat. |
| `no_referensi` | – | No. kontrak/kavling lama. Menjadi kunci anti-duplikat bersama customer. |
| `tanggal_jatuh_tempo` | – | Kosong → `as_of_date` batch (langsung dihitung jatuh tempo — memang utang lama). |
| `no_hp`, `email` | – | Berguna untuk penagihan; mengisi master customer baru. |
| `keterangan` | – | Catatan bebas. |

**Yang saya keluarkan dari usulan klien: `as_of_date` per baris.** Tanggal cutoff
migrasi harus **satu** untuk seluruh batch — ia menentukan tanggal jurnal. Per
baris hanya mengundang batch dengan lima tanggal berbeda dan jurnal yang tidak
jelas bertanggal apa. Dipindah ke form unggah (dipilih sekali, tampil jelas).

Mekanik template:
- Sheet `PIUTANG` (data) + sheet `PETUNJUK` (contoh + penjelasan kolom).
- Sel tersembunyi `template_version`; versi tidak dikenal → tolak seluruh file.
- Angka **harus** sel numerik. Bila teks, parser hanya menerima format Indonesia
  yang tidak ambigu; `1.500,50` diterima, `1,500.50` ditolak sebagai error baris
  — menebak pemisah desimal pada kolom uang tidak dapat diterima (Invariant #2).

### Validasi per baris

nama kosong · outstanding ≤ 0 · outstanding bukan angka · `kode_customer` dobel
di dalam file · `(customer, no_referensi)` sudah ada di DB · customer tidak aktif ·
tanggal jatuh tempo tidak valid. Setiap error membawa **nomor baris Excel** dan
pesan berbahasa Indonesia. Baris error tidak memblokir baris lain — admin bisa
memilih "impor yang valid saja" atau memperbaiki file lalu unggah ulang.

## 15 · Payment allocation flow

```
Customer Legacy A — Piutang proyek lama
  Piutang awal      Rp 100.000.000     (impor batch #3, per 31-12-2025)
  Pembayaran        Rp  30.000.000     KWL/2026/000007 · 12-08-2026 · Bank BCA
  ─────────────────────────────────
  Outstanding       Rp  70.000.000
```

`POST /legacy-receivables/{id}/payments` — dalam satu transaksi:

1. Kunci baris (`SELECT … FOR UPDATE`), cek idempotency key.
2. Validasi `amount ≤ outstanding` (D-5: lebih bayar ditolak dengan pesan yang
   menyebut sisa persisnya).
3. Posting jurnal `Dr kas/bank / Cr 1-2000` (akun kas divalidasi lewat
   `accounts.category` — mengikuti aturan COA-driven yang sudah berlaku).
4. Terbitkan dokumen `KWL` lewat engine penomoran (W-2) dan tautkan ke jurnal
   (W-3) — **gagal menerbitkan dokumen = seluruh transaksi batal**.
5. `paid_amount += amount`; `status = settled` bila outstanding nol.
6. Kembalikan `{receipt_number, amount, outstanding_after}` — angka outstanding
   baru datang dari server, tidak dihitung di browser.

**Ketertelusuran:** setiap pembayaran menunjuk `legacy_receivable_id` +
`journal_entry_id` + `document_id`. Layar customer menampilkan riwayatnya, dan
Buku Dokumen menampilkan kwitansinya. Pertanyaan "pembayaran ini mengurangi
piutang yang mana" dijawab oleh baris itu sendiri, bukan oleh rekonstruksi.

**Satu customer, beberapa baris piutang lama:** pembayaran dilakukan per baris
(admin memilih baris mana). Alokasi otomatis FIFO lintas-baris sengaja **tidak**
masuk v1 — piutang lama biasanya sedikit per orang, dan alokasi otomatis yang
salah lebih mahal daripada dua klik.

## 16 · Duplikasi & idempotensi

| Lapis | Mekanisme | Perilaku |
|---|---|---|
| File | `UNIQUE (tenant_id, file_sha256)` | Unggah file yang sama → 409 "sudah diimpor sebagai batch #12 pada 10-08-2026". |
| Baris | `UNIQUE (tenant_id, customer_id, legacy_reference)` | Ditolak saat commit; ditandai lebih dulu di pratinjau. |
| Baris tanpa referensi | Kunci lunak `(customer, jumlah)` | **Peringatan**, bukan penolakan — dua utang bernilai sama itu mungkin. Admin mencentang "ya, ini memang berbeda". |
| Commit | Row-lock + cek status batch | Commit kedua pada batch `committed` mengembalikan hasil yang sama (bukan jurnal kedua). |
| Pembayaran | `UNIQUE (tenant_id, idempotency_key)` | Pola yang sudah dipakai `ReceivePayment`. |

## 17 · Audit trail

- `legacy_import_batches` menyimpan siapa/kapan/berkas apa/hash/jumlah baris/
  total/nomor jurnal — batch **tidak pernah dihapus**, hanya berubah status.
- `legacy_receivable_payments` append-only, tiap baris menunjuk jurnal + dokumen.
- Perubahan `original_amount` **tidak diizinkan**. Koreksi = batalkan batch
  (bila belum ada pembayaran) atau hapus buku sebagian — keduanya berjejak jurnal.
- Pembalikan batch menyimpan `reversal_journal_id` di batch yang sama.

## 18 · Permission

| Aksi | Peran |
|---|---|
| Unduh template, lihat pratinjau, lihat daftar | `owner`, `accountant` |
| Unggah file | `owner`, `accountant` |
| **Commit impor** | `owner`, `accountant` (`RequireWrite`) |
| Terima pembayaran | `owner`, `accountant` |
| **Batalkan batch / hapus buku** | `owner` saja |
| Lihat piutang di laporan AR | semua termasuk `viewer` |

## 19 · Reporting

- `/accounting/receivable` — baris legacy **berbaur** dengan house & realization,
  dengan chip sumber "Piutang Lama" dan subtotal `BySource` ketiga. Ini yang
  menjawab "admin dapat melihat siapa yang masih berutang" dan "total piutang"
  tanpa laporan baru.
- Halaman `/accounting/piutang-lama` — daftar per customer (outstanding, umur,
  kontak), drill-down ke baris + riwayat pembayaran, tombol Terima Pembayaran.
- Dashboard & neraca ikut benar dengan sendirinya: saldo `1-2000` sudah memuatnya.
- Statement 360 yang ada berbasis **kontrak** dan tetap begitu; customer legacy
  tanpa kontrak dilayani halaman di atas. Memaksa mereka masuk statement kontrak
  akan berarti mengarang kontrak — persis yang dilarang klien.

## 20 · Hubungan dengan Accounting, AR, Customer, Sales

| Modul | Dampak |
|---|---|
| `ledger` | Nol perubahan mesin. Dua akun COA baru (`3-9000`, dan `5-4600` bila D-6 disetujui). Memakai `source='opening_balance'` yang sudah ada. |
| `receivable` | **Satu konstanta baru** (`SourceLegacy`). Tidak ada perubahan algoritma. |
| `reporting` | Sumber ketiga di `GetARAging`, label sumber di CSV. |
| `customer` | Dua kolom (`source`, `external_ref`) + pembuatan massal saat impor. |
| `document` | Satu jenis dokumen baru (`KWL`) + masuk `cashDocumentTypes()`. |
| `sale` / `charge` | **Nol.** Piutang lama tidak menyentuh kontrak, unit, BAST, HPP, komisi, atau pajak. |
| `closing` | Nol. Piutang lama bukan pendapatan, jadi tidak ikut tutup buku. |

## 21 · Risiko & edge case

| # | Risiko | Penanganan |
|---|---|---|
| **E-1** | Customer dobel karena beda ejaan ("PT Maju" vs "PT. Maju") | Pencocokan nama ternormalisasi + peringatan di pratinjau. Penggabungan customer **tidak** ada di v1 — disiplin `kode_customer` yang mencegahnya. Disebut eksplisit di petunjuk template. |
| **E-2** | `as_of_date` jatuh di periode yang sudah dikunci | Ditolak `PeriodChecker` dengan pesan jelas di pratinjau, bukan saat commit. |
| **E-3** | Admin mengetik total piutang lama **juga** di layar Saldo Awal | Saldo `3-9000` tidak nol → widget peringatan. Inilah gunanya D-1. |
| **E-4** | File 5.000 baris | Batas 2.000 baris/berkas + batas ukuran unggah 5 MB; lebih dari itu, pecah berkas. Batas **disebutkan di layar**, tidak diam-diam memotong. |
| **E-5** | Piutang lama tidak akan pernah tertagih | Hapus buku (D-6) dengan alasan + peran owner. Tanpa ini, aging akan menumpuk utang mati selamanya. |
| **E-6** | **Sistem mulai di tengah tahun** (mis. go-live Agustus 2026) | Snapshot hanya sampai 2025; Laba Rugi 2026 di sistem hanya mencakup Agustus–Desember. **Ini pertanyaan untuk klien**: (a) terima apa adanya, atau (b) jurnal saldo awal memuat pendapatan/beban YTD Jan–Jul sehingga Laba Rugi 2026 utuh. Saya **tidak menebak** — implikasi pajaknya nyata. `// TODO(tax-advisor)` bila belum terjawab saat implementasi. |
| **E-7** | Customer lama membeli unit baru | Berfungsi apa adanya: satu master customer, dua sumber piutang, satu total. Justru keuntungan desain ini. |
| **E-8** | Pembayaran diterima untuk piutang lama yang batchnya mau dibatalkan | L-9 memblokir pembalikan bila ada pembayaran. |
| **E-9** | Snapshot dibuat untuk tahun yang ternyata punya jurnal | Ditolak saat pembuatan (S-4) **dan** saat finalisasi (pemeriksaan ulang — jurnal bisa muncul di antaranya). |
| **E-10** | Unggah berkas = permukaan serangan baru (belum ada di repo) | Batasi ekstensi + MIME + ukuran; parsing di memori, berkas **tidak** disimpan ke disk (hanya hash-nya). |

## 22 · Frontend spec W-7

### Halaman

| Rute | Isi |
|---|---|
| `/migrasi/piutang-lama` | Wizard impor + riwayat batch |
| `/accounting/piutang-lama` | Daftar piutang lama per customer + terima pembayaran |
| `/accounting/receivable` | +chip sumber "Piutang Lama" + subtotal ketiga |

### Wireframe — wizard impor

```
┌ Impor Piutang Lama ────────────────────────────────────────────────┐
│  ①Template ──── ②Unggah ──── ③Pratinjau ──── ④Konfirmasi           │
├────────────────────────────────────────────────────────────────────┤
│ ① Unduh template resmi, isi, lalu unggah kembali.                  │
│    Kolom wajib: nama_customer, outstanding.                        │
│                            [ ⬇ Unduh Template Excel (v1) ]         │
├────────────────────────────────────────────────────────────────────┤
│ ② Tanggal saldo (cutoff) [ 31-12-2025 ]  ← jadi tanggal jurnal     │
│    [ Pilih Berkas… ]  piutang-lama.xlsx (24 KB)   [ Unggah ]       │
└────────────────────────────────────────────────────────────────────┘
```

```
┌ ③ Pratinjau — batch #7 · 31-12-2025 ───────────────────────────────┐
│ ✓ 42 baris siap   ⚠ 3 perlu perhatian   ✕ 2 error                  │
│ 38 customer sudah ada · 4 akan dibuat baru · Total Rp 2.415.000.000 │
├──┬───────────────┬───────────┬───────────────┬─────────────────────┤
│ 4│ Budi Santoso  │ KAV-12    │ 150.000.000   │ ✓ cocok: Budi S.    │
│ 5│ PT Maju Jaya  │ KAV-31    │  75.000.000   │ + customer baru     │
│ 6│ Siti Aminah   │ —         │  20.000.000   │ ⚠ mirip baris 11 ☐  │
│ 7│ (kosong)      │ KAV-40    │  10.000.000   │ ✕ nama wajib diisi  │
├──┴───────────────┴───────────┴───────────────┴─────────────────────┤
│ Jurnal yang akan terbentuk: Dr 1-2000 · Cr 3-9000 Rp 2.415.000.000 │
│              [ Batalkan ]   [ Impor 42 baris yang valid ]          │
└────────────────────────────────────────────────────────────────────┘
```

Pratinjau **menampilkan jurnalnya** sebelum apa pun terjadi — pola yang sama
dengan pratinjau pembayaran di layar penagihan.

### Wireframe — daftar & pembayaran

```
┌ Piutang Proyek Lama ─────────────────── Total: Rp 2.385.000.000 ───┐
│ [cari customer…]                        42 baris · 3 lunas          │
├──────────────┬───────────┬───────────────┬─────────┬───────────────┤
│ Customer     │ Referensi │ Outstanding   │ Umur    │               │
│ Budi Santoso │ KAV-12    │ 150.000.000   │ 222 hr  │ [Terima Bayar]│
│ PT Maju Jaya │ KAV-31    │  45.000.000   │ 222 hr  │ [Terima Bayar]│
│              │           │ ↳ 2 pembayaran · terakhir KWL/2026/000007│
└──────────────┴───────────┴───────────────┴─────────┴───────────────┘

Modal Terima Pembayaran
  Outstanding saat ini      Rp 75.000.000
  Jumlah diterima           [ 30.000.000 ]
  Tanggal   [12-08-2026]    Kas/Bank [ Bank — BCA ▾ ]
  Referensi [ ]             Catatan  [ ]
  ── Jurnal: Dr Bank BCA 30.000.000 / Cr Piutang Usaha 30.000.000 ──
                                   [ Batal ]  [ Terima Pembayaran ]
Toast sukses: "KWL/2026/000008 — Rp 30.000.000 diterima. Sisa Rp 45.000.000."
```

Angka "sisa" pada toast berasal dari respons server (`outstanding_after`) —
konsisten dengan pelajaran W-5: browser tidak menghitung uang.

### State

| State | Perilaku |
|---|---|
| Kosong (belum pernah impor) | CTA menjelaskan alur 4 langkah + tombol unduh template. |
| Unggah gagal parse | Layar error dengan daftar masalah per baris + tombol "Unggah ulang". |
| Batch `preview` menggantung | Muncul di riwayat sebagai "Belum dikonfirmasi" + aksi lanjutkan/buang. |
| Batch `committed` | Baris riwayat menampilkan nomor jurnal (tertaut ke Jurnal). |
| Batch `reversed` | Badge abu + tautan ke jurnal pembalik. |
| Piutang lunas | Baris berpindah ke tab "Lunas", tidak dihapus. |

### Endpoint

`GET /legacy-receivables/import/template` ·
`POST /legacy-receivables/import` ·
`GET /legacy-receivables/import/{id}/preview` ·
`POST /legacy-receivables/import/{id}/commit` ·
`POST /legacy-receivables/import/{id}/reverse` ·
`GET /legacy-receivables` · `GET /legacy-receivables/{id}` ·
`POST /legacy-receivables/{id}/payments` ·
`POST /legacy-receivables/{id}/write-off` (D-6)

---

## 23 · Rencana rilis

| Fase | Isi | Migrasi |
|---|---|---|
| **W-6** | Snapshot penuh (BE+FE). Berdiri sendiri, nol risiko ledger. | 000068 |
| **W-7a** | Skema + identitas customer (`source`, `external_ref`) + akun `3-9000` + jenis dokumen `KWL`. Tanpa UI. | 000069 |
| **W-7b** | Template + unggah + pratinjau + commit + jurnal batch. | — |
| **W-7c** | Integrasi AR (`SourceLegacy`) + pembayaran + kwitansi + halaman piutang lama. | — |
| **W-7d** | Pembalikan batch, hapus buku, poles UX, laporan CSV. | 000070 (bila D-6) |

W-6 lebih dulu karena ia tidak menyentuh uang sama sekali: klien mendapat hasil
yang bisa dilihat sementara bagian yang berisiko dikerjakan hati-hati.

## 24 · Rencana pengujian (wajib hijau sebelum fase dianggap selesai)

**Unit**
- Validasi keseimbangan snapshot: seimbang / timpang / laba dihitung, bukan diketik.
- Parser angka Excel: numerik, `1.500,50`, `1,500.50` (ditolak), kosong, negatif.
- Kunci duplikat baris & pencocokan nama ternormalisasi.
- `receivable.BuildAging` dengan sumber `legacy` → subtotal ketiga muncul.

**Integrasi (MySQL)**
- Commit batch memposting **tepat satu** jurnal seimbang; commit kedua tidak
  memposting apa pun.
- **INV-LEG-1**: Σ outstanding legacy == kontribusi legacy pada `1-2000`.
- **INV-AR-4**: Σ outstanding aging semua sumber == saldo `1-2000`.
- Pembayaran: saldo turun di baris **dan** di ledger, kwitansi terbit, dokumen
  tertaut ke jurnal (INV-DOC-1).
- Lebih bayar ditolak; idempotency key ganda tidak membuat jurnal kedua.
- `as_of_date` di periode terkunci → ditolak.
- Pembalikan batch ditolak setelah ada pembayaran.
- **Isolasi tenant**: batch/piutang/snapshot tenant lain mengembalikan nol baris.
- Snapshot: tahun ber-jurnal ditolak; `final` tidak bisa diubah tanpa reopen.

---

## 25 · Yang sengaja TIDAK dibuat

| Tidak dibuat | Alasan |
|---|---|
| Arus kas historis | Klien menyatakan tidak butuh. |
| Histori transaksi/pembayaran lama | Klien menyatakan tidak butuh; hanya menambah data yang tak pernah dibaca. |
| Kontrak/unit palsu untuk customer lama | Akan meracuni inventori, pipeline, HPP, komisi, dan pajak. |
| Mesin aging kedua / laporan piutang kedua | Melanggar INV-AR-1 dan keputusan klien D-3. |
| Penyalinan otomatis snapshot → jurnal saldo awal | Kopling yang menghidupkan kembali risiko dobel-hitung. Roadmap, bukan v1. |
| Alokasi FIFO otomatis lintas baris legacy | Dua klik lebih murah daripada alokasi otomatis yang salah. |
| Penggabungan (merge) customer duplikat | Lingkup tersendiri; dicegah lewat disiplin `kode_customer`. |
