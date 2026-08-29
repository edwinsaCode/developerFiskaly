# Final Business Architecture Validation — esaProperti

**Tanggal:** 2026-08-06
**Basis:** keputusan D-1..D-4 + 4 keputusan tambahan yang dikonfirmasi owner & akuntan perusahaan (2026-08-06)
**Pendahulu:** `docs/business-architecture-freeze-2026-08.md`
**Status:** VALIDASI — **tidak ada kode, migrasi, atau file sumber yang diubah.**

---

## §0 · Verdict — ARSITEKTUR TERKUNCI ✅

**Seluruh keputusan bisnis final. R-1..R-5 dijawab owner 2026-08-06. Tidak ada kontradiksi tersisa antara business rule dan arsitektur.**

| Area | Verdict |
|---|---|
| D-1 Kelebihan Tanah ikut HPP **dari nilai tanah saja** | ✅ **TERKUNCI** — basis alokasi diperluas per cost pool (§1) |
| D-2 Document context (system + custom cash documents) | ✅ **TERKUNCI** (§3) |
| D-3 Biaya realisasi jadi Piutang **saat invoice terbit** | ✅ **TERKUNCI** (§2) |
| D-4 Numbering reset tahunan | ✅ **TERKUNCI** (§4) |
| Booking, Kekurangan KPR | ✅ sudah sesuai, tinggal bersihkan permukaan usang |
| Master Produk & Master Jenis Biaya Realisasi | ✅ **TERKUNCI** — desain siap dibangun |
| Kelebihan Tanah bukan lagi Charge Addon | ✅ **TERKUNCI**, butuh audit data sebelum eksekusi |

Yang tersisa murni pekerjaan implementasi, dengan urutan di §7.

### Prinsip yang mengikat seluruh implementasi

1. **Single Source of Truth** — satu aturan, satu tempat. Tidak boleh ada dua definisi untuk satu angka.
2. **Append-only ledger** — jurnal yang sudah diposting tidak pernah diedit atau dihapus. Koreksi hanya lewat jurnal pembalik.
3. **Fail closed** — data master yang belum lengkap **menolak** transaksi, tidak pernah jatuh ke nilai default diam-diam.
4. **Domain Driven Design** — aturan bisnis tinggal di `domain`, bukan tersebar di SQL, handler, atau frontend.
5. **Tidak ada duplicate source of truth** — pembaca baru wajib membaca SSOT yang sudah ada, bukan menghitung ulang sendiri.
6. **Snapshot immutable** — HPP yang sudah beku saat BAST tidak pernah dihitung ulang.

### Dua konsekuensi angka yang perlu diketahui saat rilis

- **D-1:** porsi **biaya tanah** yang ditanggung tiap rumah akan turun, karena kelebihan tanah kini ikut memikul biaya tanah. Porsi biaya konstruksi rumah **tidak berubah sama sekali** — kelebihan tanah tidak menyentuh pool konstruksi. Unit yang sudah BAST tidak bergerak (snapshot beku).
- **D-3:** neraca akan menampilkan Piutang Customer dan Titipan Realisasi yang sebelumnya tidak ada, karena tagihan realisasi kini diakui saat invoice terbit. Grup yang sudah berjalan disamakan lewat jurnal susulan sekali jalan (R-3).

---

## §1 · Validasi D-1 — Kelebihan Tanah ikut HPP

### 1.1 Kabar baik: mekanismenya memang sudah ada

Instruksi owner *"gunakan engine alokasi luas yang sudah ada, jangan membuat engine inventory tanah baru"* **tepat sasaran dan bisa dipenuhi**. Buktinya di `internal/allocation/repository.go:185`:

```go
if !domain.ProductCategory(*row.Category).ParticipatesInHPP() {
    continue // non-properti: tidak pernah menjadi penerima alokasi HPP
}
```

Denominator alokasi disaring persis oleh fungsi kanonik `ParticipatesInHPP()`. Begitu `kelebihan_tanah` berubah kategori menjadi `property`, unit kelebihan tanah **otomatis masuk basis alokasi** — tanpa satu baris pun engine baru. Ini seam yang sudah dirancang benar sejak awal.

Dan "mengurangi stok tanah" juga sudah terpenuhi **dalam nilai rupiah**: saat kelebihan tanah dijual, porsi biaya tanah yang teralokasi padanya berpindah `1-3000 Persediaan Tanah → 5-1000 HPP`. Persediaan tanah berkurang persis sebesar bagian yang terjual. Tidak perlu buku kuantitas m² terpisah, sesuai instruksi owner.

### 1.2 Kabar yang perlu keputusan: ini membalik keputusan teknis H-1

Pengecualian di atas **bukan kelalaian**. Ia dipasang sengaja pada hardening H-1, dan alasannya ditulis di `repository.go:154-158`:

> *"Unit non-properti (kelebihan tanah, sambungan PDAM, …) punya saleable_area / list_price > 0, sehingga bila ikut masuk basis mereka **MENYEDOT bobot dan mendilusi HPP rumah** — baik di alokasi biaya aktual maupun di snapshot budgeted saat BAST."*

Keputusan owner sekarang membalik itu, dan itu **memang keputusan yang benar** — kelebihan tanah nyata-nyata memakan biaya tanah, jadi ia memang harus menanggung HPP. Tapi efeknya harus disadari:

> Contoh owner sendiri: tanah 10.000 m², rumah 9.500 m², kelebihan 500 m².
> **Sekarang:** seluruh biaya tanah ditanggung 9.500 m² rumah.
> **Setelah D-1:** ditanggung 10.000 m². **HPP setiap rumah turun ±5%, laba kotor setiap rumah naik ±5%.**

Unit yang **sudah BAST aman** — HPP-nya beku di `allocation_snapshots` (migrasi 000027) dan tidak akan berubah. Yang berubah adalah unit yang belum BAST, dan proyeksi margin di RAB.

> ### ✅ R-1 — FINAL: alokasi per cost pool
>
> **Keputusan owner:** HPP Kelebihan Tanah berasal dari **nilai tanah**. Ia tidak boleh menyerap biaya konstruksi maupun biaya pembangunan rumah, dan tidak boleh mendilusi HPP konstruksi rumah. Engine yang ada **diperluas** agar basis alokasi bisa berbeda per cost pool — bukan engine baru.
>
> **Matriks partisipasi final** (aturan domain, bukan konfigurasi — taksonomi tertutup):
>
> | Kategori produk | Pool Tanah | Pool Konstruksi | Pool Soft | Pool Financing |
> |---|---|---|---|---|
> | `property` (Rumah, Ruko) | ✅ | ✅ | ✅ | ✅ |
> | `land` (Kelebihan Tanah) — **kategori baru** | ✅ | ❌ | ❌ | ❌ |
> | `non_property` | ❌ | ❌ | ❌ | ❌ |
>
> `land` hanya menyerap pool Tanah, mengikuti kalimat owner secara harfiah: *"HPP tersebut berasal dari nilai tanah."*
>
> **Konsekuensi desain:** `ProductCategory` bertambah satu nilai (`land`), dan fungsi kanonik berubah dari `ParticipatesInHPP()` menjadi `ParticipatesInCostPool(pool)`. `ParticipatesInHPP()` tetap ada sebagai turunan (`property` atau `land`) supaya pembaca lama tidak perlu tahu detail pool. Bobot alokasi menjadi vektor per pool, bukan satu vektor tunggal — perluasan `buildWeights`, bukan engine baru.
>
> **`saleable_area`:** karena satu-satunya pool bersama antara rumah dan kelebihan tanah adalah pool Tanah, basis pool Tanah **wajib luas tanah** agar satuannya sebanding. Pool Konstruksi hanya berisi produk `property`, jadi basisnya boleh tetap seperti sekarang tanpa risiko pencampuran satuan.

> ### ✅ R-2 — FINAL: tidak ada perhitungan ulang histori
>
> Snapshot yang sudah BAST bersifat **immutable**. Aturan baru hanya berlaku untuk transaksi yang belum punya snapshot. Tidak ada recalculation histori.
>
> Infrastrukturnya sudah siap: `allocation_config_versions` (migrasi 000029) menyimpan versi basis per proyek, dan `allocation_snapshots` (migrasi 000027) mem-pin versi yang dipakai saat BAST. Perubahan basis diterbitkan sebagai **versi baru**, versi lama tidak disentuh.

### 1.3 Yang sudah pasti dan tidak perlu ditanya lagi

- `kelebihan_tanah` → kategori `property`, akun pendapatan sendiri (usulan `4-1100 Pendapatan Penjualan Tanah`) supaya margin rumah dan margin tanah tidak tercampur di laporan.
- `kavling` dan `pdam` → **dinonaktifkan** (`is_active = false`), **tidak dihapus**. Resolver bersifat fail-closed: menghapus tipe yang masih dirujuk unit membuat BAST unit itu tertolak selamanya.
- Kelebihan Tanah dicabut dari Charge Group `kind = addon`. Perlu audit data dulu: grup addon yang sudah terlanjur diposting **tidak boleh ditulis ulang** (invariant #5) — hanya jalur baru yang berubah, jalur lama diselesaikan apa adanya atau dikoreksi lewat jurnal pembalik bila owner memintanya.
- `// TODO(tax-advisor)` PPN kelebihan tanah masih terbuka. Sebagai produk penjualan penuh, ini sekarang perlu jawaban dari akuntan.

---

## §2 · Validasi D-3 — Biaya realisasi menjadi Piutang saat generate

### 2.1 Jurnal owner, dibaca sebagai alur

Owner menuliskan posisi kumulatifnya. Sebagai alur peristiwa, artinya:

| Peristiwa | Jurnal | Ada hari ini? |
|---|---|---|
| **Generate** biaya realisasi Rp20jt | `Dr 1-2000 Piutang Customer 20jt` / `Cr 2-2400 Titipan Realisasi 20jt` | ❌ **tidak ada jurnal sama sekali** |
| Customer bayar Rp8jt | `Dr Kas 8jt` / `Cr 1-2000 Piutang Customer 8jt` | ⚠️ ada, tapi **kredit ke `2-2400`, bukan `1-2000`** |
| Perusahaan bayar PDAM/Notaris | `Dr 2-2400 Titipan` / `Cr Kas` | ✅ **sudah benar** |
| Sisa pasca-BAST | tetap di `1-2000`, muncul di daftar piutang | ❌ tidak ada |

### 2.2 Seberapa besar perubahannya

Hari ini `internal/charge` menerbitkan jurnal di **5 titik**: `ReceivePayment` (`service.go:430`), `RecordPayout` (578), `Refund` (702), `TransferToGroup` (897), `VoidPayment` (1032). Sementara `CreateGroup` (101), `AddItems` (155), `CancelItem` (210), dan `CancelGroup` (251) **sama sekali tidak menyentuh ledger** — tagihan titipan hari ini murni catatan billing.

D-3 mengubah itu secara mendasar: **setiap operasi yang mengubah nilai tagihan menjadi peristiwa ledger.**

| Operasi | Sekarang | Setelah D-3 |
|---|---|---|
| `CreateGroup` / `AddItems` | tanpa jurnal | **jurnal baru** `Dr 1-2000 / Cr 2-2400` |
| `CancelItem` / `CancelGroup` | tanpa jurnal | **jurnal pembalik** |
| `TrueUpAndSettle` (K-4, koreksi nilai) | tanpa jurnal | **jurnal penyesuaian** |
| `ReceivePayment` | `Cr 2-2400` | **`Cr 1-2000`** |
| `Refund` | `Dr 2-2400 / Cr Kas` | perlu ditinjau (lihat R-4) |

Ini bukan tambal-sulam; ini pergeseran `charge` dari modul billing menjadi modul yang ikut memegang buku besar piutang. **Konsekuensi terpenting:** setelah D-3, akun `1-2000 Piutang Customer` punya **dua penulis** — `sale` (piutang unit) dan `charge` (piutang realisasi).

Padahal `charge` **sudah** menulis langsung ke agregat `sale` hari ini (`sale.TerminPayment`, `sale.PaymentAllocation` — `charge/service.go:325, 440-470, 806, 907-932, 975-1054). Menambahkan kepemilikan piutang di atas coupling yang sudah bermasalah itu adalah **technical debt paling mahal dalam daftar ini** (TD-1, §5). Rekomendasi saya tegas: **rapikan kepemilikan piutang lebih dulu, baru implementasikan D-3** — bukan sebaliknya. Kalau dibalik, kita akan membongkarnya lagi.

### 2.3 Kabar baik: tiga hal sudah beres

- **Pembayaran fleksibel per item** (Notaris 10jt lunas, PDAM 3jt dari 5jt, Listrik 4jt dari 5jt): ✅ sudah didukung `charge_items` + alokasi per item.
- **Lebih bayar direfund atau dialihkan**: ✅ sudah ada — `ChargeSettlement` dengan aksi `refund`, `transfer_house`, `transfer_group`, `void_payment`.
- **Kwitansi 4 angka** (Total Tagihan, Dibayar Sekarang, Total Dibayar, Sisa Terutang): ✅ sudah tercetak di KWR (`billing/receipt_print.go:290-324`).
- **BAST tidak lagi disyaratkan**: tinggal cabut `charge.CheckBAST` + kebijakan tenant `require_realization_settled`. Lurus, tanpa risiko.

### 2.4 Tiga keputusan final

> ### ✅ R-5 — FINAL: piutang lahir saat Invoice diterbitkan
>
> Lifecycle terkunci:
>
> ```
> Charge Group dibuat        → belum ada jurnal, belum ada piutang
>         ↓
> Invoice diterbitkan        → Dr 1-2000 Piutang Customer / Cr 2-2400 Titipan Realisasi
>         ↓
> Pembayaran customer        → Dr Kas / Cr 1-2000 Piutang Customer
>         ↓
> Pelunasan / Outstanding    → sisa tetap di 1-2000, muncul di daftar piutang
> ```
>
> Alasannya sejalan dengan keputusan §6 owner: **piutang wajib punya dasar dokumen yang sah.** Admin bebas menyusun dan mengoreksi item selama grup belum di-invoice — tanpa mengotori ledger. Setelah invoice terbit, setiap perubahan nilai tagihan wajib menerbitkan jurnal penyesuaian atau pembalik.
>
> **Konsekuensi desain:** `CreateGroup`/`AddItems` tetap tanpa jurnal (seperti sekarang). Titik jurnal baru ada di `IssueInvoice`. `CancelItem`/`CancelGroup` menerbitkan jurnal **hanya bila** grupnya sudah pernah di-invoice. Pembayaran vendor tetap `Dr Titipan / Cr Kas` — tidak berubah.

> ### ✅ R-3 — FINAL: jurnal susulan, append-only, satu aturan
>
> Grup titipan yang sudah berjalan disamakan lewat **jurnal susulan sekali jalan**: untuk setiap grup terbuka yang sudah pernah di-invoice, posting `Dr 1-2000 / Cr 2-2400` sebesar sisa tagihan per tanggal cut-off. **Tidak boleh ada dua rule berjalan bersamaan. Tidak boleh edit histori.**

> ### ✅ R-4 — FINAL: lebih bayar adalah pilihan admin, bukan otomatisasi
>
> Bila terjadi lebih bayar, sistem **menyediakan tiga pilihan** dan tidak memutuskan sendiri:
>
> 1. Refund ke customer
> 2. Dialihkan ke item realisasi lain
> 3. Dialihkan menjadi Buyer Credit
>
> Ketiganya sudah punya rumah di `ChargeSettlement` (`refund`, `transfer_group`, `transfer_house`). Yang perlu dibangun: kelebihan bayar **tidak boleh mengendap sebagai piutang bersaldo kredit** — ia ditahan sebagai dana belum-berdisposisi sampai admin memilih, dan setiap pilihan menerbitkan dokumen serta jurnalnya sendiri.

---

## §3 · Validasi D-2 — Document Context ✅

**Konsisten. Tidak ada kontradiksi tersisa.** Keputusan owner (system documents + custom cash documents) persis memetakan ke rancangan `internal/document` di §4 dokumen freeze.

```
document_types (master)
  code, name
  direction        'in' | 'out'
  prefix
  numbering_scope  'yearly'          ← D-4
  is_system        true  → tidak bisa dihapus/diubah admin
                   false → dibuat admin sendiri

  is_system = true : Invoice, KWT, KWB, KWR, Memo Internal Transfer,
                     Refund Voucher, + dokumen sistem lain
  is_system = false: Pembayaran Lahan, Subkontraktor, Security,
                     Konsultan, dst — admin bebas menambah
```

Penegakan "setiap uang masuk & keluar wajib berdokumen" dilakukan satu invariant, bukan disiplin manusia:

> **INV-DOC-1** — setiap `JournalEntry` yang menyentuh akun ber-peran `ledger.RoleCashBank` **wajib** punya `document_id`. Posting service menolak yang tidak punya.

Bisa ditulis sekarang karena `RoleCashBank` sudah COA-driven (`ledger/account_role.go:76-80`): rekening bank baru yang ditambahkan admin ikut terjaga otomatis, tanpa sentuhan kode.

**Satu catatan operasional:** invariant ini fail-closed. Begitu dinyalakan, setiap jalur kas keluar yang belum berdokumen **berhenti bekerja**. Urutannya wajib: bangun registry → backfill dokumen untuk jurnal kas historis → **baru** nyalakan penegakan. Kalau dinyalakan lebih dulu, sistem produksi berhenti menerima transaksi.

**Delapan jalur kas keluar yang hari ini tanpa dokumen sama sekali:** bayar tanah, bayar vendor, bayar subkontraktor, bayar pihak ketiga titipan (`charge_payouts`), refund customer (`charge_settlements`, `refunds`), bayar komisi, bayar pajak (`tax_payments`), pengeluaran overhead.

---

## §4 · Validasi D-4 — Numbering reset tahunan ✅

**Konsisten, dan jalur migrasinya aman.**

```
document_sequences
  PRIMARY KEY (tenant_id, doc_type, period_key)
  period_key : '2026' | '2027' | ...
  next_val   : BIGINT UNSIGNED
```

Hari ini ada dua mesin berbeda: `receipt_sequences` (kunci `tenant_id + doc_type`) dan `invoice_sequences` (kunci `tenant_id` saja, tanpa doc_type). Keduanya perpetual — `next_val` tidak pernah reset meskipun format nomornya memuat tahun.

**Aturan migrasi yang menjamin nomor lama tidak bentrok:** saat cut-over, baris `period_key = '2026'` di-seed dengan `next_val` yang berjalan sekarang. Jadi sisa tahun 2026 melanjutkan urutan yang klien sudah lihat (tidak ada nomor ganda), dan 1 Januari 2027 dimulai dari `000001` sesuai keputusan owner. Nomor historis tidak berubah satu pun.

**Dua aturan tambahan yang saya sarankan dikunci sekarang:**
1. Nomor dialokasikan **di dalam transaksi posting yang sama** — tidak ada nomor tanpa jurnal, tidak ada jurnal tanpa nomor. (Pola ini sudah dipakai hari ini dan sudah benar.)
2. Dokumen yang di-void **tetap memegang nomornya**. Nomor tidak pernah dipakai ulang — itu jejak audit, bukan slot kosong.

---

## §5 · Technical debt yang akan menyebabkan refactor besar

Diurutkan menurut biaya bila dibiarkan. TD-1 dan TD-2 **harus** ditangani sebelum D-3, bukan sesudahnya.

| ID | Debt | Kenapa ini berbahaya sekarang | Kapan |
|---|---|---|---|
| **TD-1** | `charge` menulis langsung agregat `sale` (`TerminPayment`, `PaymentAllocation` — 5 lokasi) | D-3 menjadikan `charge` **penulis kedua akun `1-2000`**. Menumpuk kepemilikan piutang di atas coupling yang sudah salah = pasti dibongkar ulang | **sebelum D-3** |
| **TD-2** | Dua laporan aging terpisah yang tak pernah bertemu (`reporting` dari `payment_schedules`, `charge` dari `charge_items`) + statement buta titipan | Setelah D-3 keduanya menjadi **akun ledger yang sama**. Dua aging atas satu akun akan langsung terlihat sebagai laporan yang saling bertentangan | **bersamaan D-3** |
| **TD-3** | `charge` (transaksional) mengimpor `reporting` (read model) | Arah dependensi terbalik; ini yang melahirkan aging kedua | bersamaan TD-2 |
| **TD-4** | Titipan Notaris punya dua jalur & dua akun: modul `internal/notary` (`2-2300`) vs charge item "Notaris" (`2-2400`) | Owner menetapkan Notaris sebagai jenis titipan biasa → `internal/notary` kehilangan alasan eksistensinya. Konsistensi `EQ_NotaryDeposit` hanya mengunci `2-2300`, **buta** terhadap uang notaris di `2-2400` | bersamaan master titipan |
| **TD-5** | `kind = addon` menghasilkan jurnal identik dengan `realization`, hanya deskripsi berbeda (`charge/service.go:396`) | Owner mencabut Kelebihan Tanah dari addon → `addon` kehilangan alasan hidup. Butuh audit data grup addon existing | bersamaan D-1 |
| **TD-6** | Skema mati `hpp_trueup_runs` / `hpp_trueup_lines` tanpa kode (P0-4, migrasi 000029-032) | D-1 menggeser basis HPP → mekanisme true-up justru **makin relevan**, bukan makin bisa ditunda | setelah D-1 |
| **TD-7** | Registry peran akun otoritatif hanya di sisi baca, bukan sisi posting (dinyatakan sendiri di `account_role.go:12-14`) | D-3 menambah jalur posting baru ke `1-2000`. Tanpa registry dua sisi, dashboard dan jurnal bisa menyimpang diam-diam | bersamaan D-3 |
| **TD-8** | Perubahan master data tidak diaudit sama sekali | Owner memberi admin kuasa mengubah master sendiri. Kalau akun sebuah jenis titipan diubah, jurnal lama tetap benar — tapi **tidak ada catatan siapa mengubah apa, kapan** | bersamaan master |
| **TD-9** | `sale` 8.631 LOC menampung ≥5 bounded context | Bukan bug, tapi inilah sebab setiap perubahan aturan terasa "refactor besar" | terakhir, opsional |
| **TD-10** | Permukaan usang: deskripsi COA `2-2100` masih menyebut reklas/hangus; DTO booking masih menerima `refundable` | Membingungkan pembaca berikutnya, bukan salah hitung | kapan saja |

---

## §6 · Konsistensi lintas bounded context

| Konteks | Konsisten dengan keputusan final? | Catatan |
|---|---|---|
| **Product Catalog** | ⚠️ setelah D-1 | `kelebihan_tanah` → `property` + akun pendapatan sendiri; `kavling`/`pdam` dinonaktifkan; CRUD dilengkapi (nonaktif, bukan hapus) |
| **Sales** | ✅ | Booking, KPR, BAST sudah sesuai. BAST cukup dicabut gate realisasinya |
| **Charge Group (Titipan)** | 🔴 D-3 | Perubahan terbesar: 4 titik jurnal baru + 1 berubah. Master jenis titipan belum ada |
| **Billing / Receivable** | 🔴 setelah D-3 | `1-2000` dapat penulis kedua; aging & statement wajib disatukan (TD-1, TD-2) |
| **Accounting / Ledger** | ✅ struktur sehat | Perlu `journal_entries.document_id` + registry dua sisi (TD-7) |
| **Cash Management** | 🔴 | 8 jalur kas keluar tanpa dokumen; INV-DOC-1 belum ada |
| **Document** | 🔴 belum ada | Rancangan sudah disetujui (D-2), tinggal dibangun |
| **Reporting / Dashboard** | ⚠️ | Membaca ledger posted, jadi ikut benar otomatis — **kecuali** dua aging terpisah (TD-2) |
| **Statement 360** | 🔴 | Tidak punya bagian titipan sama sekali. Setelah D-3 wajib ada, karena itu piutang yang sama |
| **Collection** | ⚠️ | Perlu menangani piutang realisasi, bukan hanya cicilan unit |
| **Receipt** | ✅ | KWT/KWB/KWR + 4 angka sudah sesuai; tinggal pindah penomoran ke Document |
| **Approval / Tax / Commission** | ✅ | Tidak tersentuh keputusan ini |

---

## §7 · Urutan implementasi yang direkomendasikan

Disusun agar tidak ada pekerjaan yang dibongkar ulang.

Seluruh prasyarat keputusan sudah terjawab (R-1..R-5 final). Yang tersisa hanya prasyarat **teknis** antar-wave.

| Wave | Isi | Prasyarat |
|---|---|---|
| ~~W-0~~ | ~~Jawab R-1 … R-5~~ — **selesai 2026-08-06** | — |
| **W-1** | Master Jenis Biaya Realisasi + seam `domain.BillingTreatment`; satukan `internal/notary` ke dalamnya lewat `deposit_account_code`; nonaktifkan `pdam`/`kavling` (audit data dulu); audit master data (TD-8) | — |
| **W-2** | Bounded context Document: `document_types`, `documents`, `document_sequences` (reset tahunan); migrasi 2 tabel sekuens lama tanpa mengubah nomor historis; backfill dokumen untuk jurnal kas historis | W-1 |
| **W-3** | Nyalakan **INV-DOC-1** + dokumen untuk seluruh jalur kas keluar; custom cash document | W-2 backfill **selesai** |
| **W-4** | **Bereskan kepemilikan piutang** — `charge` berhenti menulis agregat `sale`; putus impor `charge → reporting`; satukan aging & statement jadi satu eksposur customer (TD-1, TD-2, TD-3) | — |
| **W-5** | **D-3 / R-5**: jurnal saat invoice terbit, kredit pembayaran ke `1-2000`, penyesuaian saat item dibatalkan pasca-invoice, tiga pilihan lebih bayar (R-4), cabut gate BAST, jurnal susulan cut-over (R-3) | **W-2**, **W-4** |
| **W-6** | **D-1 / R-1**: `ProductCategory.land`, `ParticipatesInCostPool(pool)`, bobot per cost pool, `kelebihan_tanah` → `land` + akun pendapatan sendiri, cabut dari `addon` | W-1 |
| **W-7** | P0-4 HPP true-up (TD-6); registry akun dua sisi (TD-7); bersihkan permukaan usang (TD-10) | W-6 |
| **W-8** | Pecah `sale` per konteks (TD-9) — opsional | — |

**Dua urutan yang tidak boleh dibalik:**
- **W-4 sebelum W-5.** Kalau D-3 dibangun di atas coupling `charge → sale` yang sekarang, coupling itu harus dibongkar ulang setelahnya.
- **W-2 sebelum W-5.** R-5 mengikat kelahiran piutang pada penerbitan dokumen; jurnalnya butuh `document_id` yang baru ada setelah konteks Document berdiri.

Pekerjaan Tab Unit yang tertunda (PATCH unit, paginasi, aksi kartu) masuk setelah **W-1**, karena W-1 mengubah bentuk master yang menjadi dasar layarnya.

---

## §8 · Ringkasan: aturan yang terkunci

| ID | Aturan final | Konsekuensi teknis |
|---|---|---|
| **R-1** | Kelebihan Tanah punya HPP **dari nilai tanah saja**; tidak menyerap biaya konstruksi/soft/financing; tidak mendilusi HPP rumah di pool selain tanah | `ProductCategory` bertambah `land`; `ParticipatesInHPP()` → `ParticipatesInCostPool(pool)`; `buildWeights` dipanggil per pool, bukan sekali |
| **R-2** | Snapshot ber-BAST **immutable**; aturan baru hanya berlaku untuk transaksi yang belum bersnapshot; tidak ada perhitungan ulang histori | Basis baru masuk lewat `allocation_config_versions`; `allocation_snapshots` tidak pernah disentuh |
| **R-3** | Grup berjalan disamakan lewat **jurnal susulan append-only**; tidak boleh dua aturan hidup bersamaan; tidak boleh edit histori | Satu jalur cut-over bertanggal, bukan cabang data-driven seperti booking fee |
| **R-4** | Lebih bayar: admin memilih **refund / alihkan ke item lain / jadikan Buyer Credit**. Sistem **tidak memutuskan sendiri** | Kelebihan ditahan sebagai dana belum-berdisposisi; `ChargeSettlement` jadi satu-satunya jalan keluar; tiap pilihan berdokumen |
| **R-5** | Piutang lahir **saat Invoice diterbitkan**, bukan saat Charge Group dibuat | Titik jurnal ada di `IssueInvoice`; `CreateGroup`/`AddItems` tetap tanpa jurnal; pembatalan pasca-invoice wajib jurnal penyesuaian |

**Tidak ada kontradiksi tersisa** antara business rule owner dan arsitektur. Implementasi dimulai dari **W-1**.

**Aturan operasional yang tetap mengikat selama implementasi:**
- Jangan **hapus** product type `pdam`/`kavling` — resolver fail-closed; unit lama akan gagal BAST selamanya. Nonaktifkan (`is_active=false`) setelah audit data.
- Jangan tulis ulang jurnal grup addon/titipan lama (invariant #5). Koreksi hanya lewat jurnal pembalik atau susulan.
- Setiap wave ditutup dengan `go test ./...` hijau sebelum dinyatakan selesai.

---

**Catatan metode.** Dokumen ini murni hasil pembacaan kode, skema, dan migrasi — tidak ada file sumber yang diubah, tidak ada test yang dijalankan, dan tidak ada perilaku runtime yang diverifikasi. Klaim di sini adalah klaim tentang **struktur kode dan skema**, bukan tentang hasil eksekusi. Angka "±5%" pada §1.2 adalah aritmetika dari contoh owner sendiri (500/10.000), bukan hasil perhitungan atas data produksi.
