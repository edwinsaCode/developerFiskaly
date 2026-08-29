# W-4 — Kepemilikan Piutang & Satu Eksposur Customer

Status: **DESAIN → IMPLEMENTASI** · 2026-08-09
Prasyarat: W-3 FULLY CLOSED (INV-DOC-1). Penerus: **W-5** (J-14a + cabut gate BAST).

---

## 0. Kenapa W-4 ada

Hari ini seorang customer bisa punya **dua tagihan yang tidak pernah dijumlahkan**:

| | Harga rumah | Biaya realisasi |
|---|---|---|
| Sumber baris | `payment_schedules` | `charge_items` (grup open, ber-`due_date`) |
| Pemilik tulis sub-ledger kas | `sale` | **`charge` — menulis tabel `sale` langsung** |
| Aging | `GET /reports/ar-aging` | `GET /charges/aging` (**tidak dipakai UI mana pun**) |
| Statement | `GET /sale-contracts/{id}/statement` | seksi terpisah, dirakit di browser |

Akibatnya: tidak ada satu pun angka di sistem yang menjawab *"customer ini
berutang berapa?"*. Admin harus menjumlahkan dua layar di kepalanya.

W-5 akan memperburuk ini kalau dibiarkan. Keputusan klien (D-3, final) berbunyi:
sisa biaya realisasi saat BAST **langsung menjadi Piutang Customer** dan wajib
masuk aging yang dapat ditagih — dan **tidak boleh** ada mekanisme piutang kedua.
Menambahkan J-14a di atas dua jalur piutang yang terpisah justru **melahirkan**
piutang kedua itu. Jadi penyatuan harus terjadi lebih dulu.

Tiga cacat yang ditutup:

- **TD-1** — `charge` adalah pemilik kedua atas sub-ledger kas/piutang: ia
  `Create` baris `sale.TerminPayment` dan `sale.PaymentAllocation` sendiri.
- **TD-2** — aging dan statement tidak pernah menjumlahkan seluruh eksposur.
- **TD-3** — `charge` (paket transaksional) meng-import `reporting` (paket read
  model). Arah ketergantungan terbalik.

---

## 1. Keputusan desain

### D-W4-1 — Piutang punya satu paket pemilik: `internal/receivable`

Paket **baru**, murni value object (hanya import `domain`), tanpa DB dan tanpa
HTTP. Isinya:

- `receivable.Row` — satu kewajiban customer yang jatuh tempo, apa pun asalnya.
- `receivable.Source` — `house` | `realization`. **Dimensi, bukan laporan
  terpisah.**
- `receivable.BuildAging(rows, asOf)` — mesin aging (pindahan dari
  `reporting.BuildARAging`, logika tidak berubah sedikit pun).
- `Bucket`, `Status`, `AgingRow`, `AgingReport`.

Kenapa paket baru dan bukan `domain`? `domain` adalah kosakata nilai (Money);
aging adalah *model baca*. Kenapa bukan tetap di `reporting`? Karena selama ia
tinggal di `reporting`, setiap penghasil baris wajib import paket laporan — persis
TD-3.

`reporting` menyimpan **type alias** (`type ARScheduleRow = receivable.Row`, dst.)
sehingga kontrak JSON `/reports/ar-aging` dan seluruh kode pemanggil tidak berubah
satu byte pun. Pemindahan mesin bukan pemindahan API.

### D-W4-2 — Arah ketergantungan dibalik lewat interface, bukan import

```
        domain
          ▲
      receivable
       ▲      ▲
  reporting   charge          sale
       ▲                       ▲
       └── interface ReceivableSource ──┘   (dipasang di main.go)
```

`reporting` mendefinisikan `RealizationReceivableReader`; `charge.Service`
memenuhinya secara struktural. `charge` **tidak lagi import `reporting`** — TD-3
selesai. Pola ini identik dengan `ARAgingReader`/`WithARReader` yang sudah ada,
jadi bukan mekanisme baru yang harus dipelajari orang.

### D-W4-3 — `sale` memiliki tabelnya sendiri; `charge` menyatakan maksud

`charge` berhenti melakukan `tx.Create(&sale.TerminPayment{…})`. Sebagai
gantinya `sale` mengekspos **port transaksional** (`internal/sale/subledger.go`):

```go
func RecordChargeReceiptInTx(ctx, tx, in ChargeReceiptInput) (uint64, error)
func RecordChargeAllocationsInTx(ctx, tx, tenantID, terminID, allocs) error
func RecordChargeVoidMirrorsInTx(ctx, tx, tenantID, allocs, createdBy) error
func ListChargeAllocationsInTx(ctx, tx, tenantID, terminID) ([]PaymentAllocation, error)
func FindChargeReceiptByIdempotencyKeyInTx(ctx, tx, tenantID, key) (*TerminPayment, error)
```

Port menerima `*gorm.DB` transaksi milik pemanggil — atomisitas `charge` utuh,
tapi **invarian sub-ledger** (`counts_toward_price=false` untuk penerimaan
realisasi, `payment_source` yang sah, `allocation_type` yang sah, tanda negatif
mirror void) sekarang ditegakkan di satu tempat: pemiliknya.

Ini bukan sekadar kerapian. Hari ini nilai `CountsTowardPrice: false` ditulis di
tiga tempat berbeda di `charge`; satu kali seseorang lupa, penerimaan titipan
akan tampil sebagai pembayaran harga rumah dan Laba Rugi ikut salah.

### D-W4-4 — Eksposur customer = satu daftar, satu mesin, satu tanggal

`GET /reports/ar-aging` sekarang mengembalikan **kedua sumber dalam satu
laporan**, dengan:

- `source` pada setiap baris (`house` | `realization`),
- `by_source` — subtotal outstanding + overdue per sumber,
- query `?source=house|realization|all` (default `all`) untuk mempersempit.

`GET /charges/aging` **tetap ada** dan kini hanyalah `?source=realization` yang
dijalankan lewat mesin dan penghasil baris yang sama — bukan salinan kedua.

Statement kontrak mendapat blok `exposure` yang dihitung **di server**:

```json
"exposure": {
  "house_outstanding": "…",  "house_overdue": "…",
  "realization_outstanding": "…", "realization_overdue": "…",
  "total_outstanding": "…",  "total_overdue": "…"
}
```

Prinsip: angka "total yang harus ditagih ke customer ini" **tidak boleh** lahir
dari penjumlahan di browser. Kalau dua layar boleh menjumlahkan sendiri, dua
layar akan berbeda.

### D-W4-5 — Yang SENGAJA tidak dikerjakan di W-4

- **Tidak** memindahkan `charge_items` menjadi baris ledger piutang. Sebelum
  BAST, tagihan realisasi belum melahirkan jurnal apa pun (titipan lahir saat
  kas masuk, 2-2400) — memaksanya jadi jurnal piutang sekarang akan mendahului
  keputusan W-5 dan menabrak Invariant #7.
- **Tidak** mencabut gate BAST. Itu W-5.
- **Tidak** membuat halaman piutang per-customer lintas-kontrak. Eksposur
  disatukan pada level laporan dan statement; agregasi per customer master
  menunggu setelah W-5 mengubah bentuk piutangnya.
- **Tidak** menyentuh `payment_allocations` sebagai skema. Sub-ledger sudah
  satu tabel; yang salah cuma siapa yang menulisnya.

---

## 2. Invarian yang dijaga

| Kode | Bunyi | Cara dibuktikan |
|---|---|---|
| **INV-AR-1** | Satu mesin aging. Tidak ada fungsi bucketing kedua di seluruh repo. | `grep` tidak menemukan `bucketFor`/`daysBetween` di luar `internal/receivable` |
| **INV-AR-2** | `Σ outstanding(ar-aging source=all)` == `Σ outstanding(house)` + `Σ outstanding(realization)` | test integrasi |
| **INV-AR-3** | Statement `exposure.total_outstanding` == baris aging kontrak itu, per tanggal yang sama | test integrasi |
| **INV-OWN-1** | Hanya paket `sale` yang menulis `termin_payments` & `payment_allocations`. | `grep` `sale.TerminPayment{`/`sale.PaymentAllocation{` di luar `internal/sale` = 0 |
| **INV-DEP-1** | `charge` tidak import `reporting`. | `go list -deps` / grep |

Invarian lama yang tidak boleh tergores: #2 (tanpa float), #6 (isolasi tenant di
setiap join), #5 (append-only — void tetap mirror negatif, bukan hapus).

---

## 3. Rencana kerja

| # | Langkah | File |
|---|---|---|
| 1 | Paket `receivable` (Row/Source/Bucket/Status/BuildAging) | `internal/receivable/receivable.go` (baru) |
| 2 | `reporting` alias + `BuildARAging` jadi pembungkus | `reporting/model.go`, `reporting/ar_aging.go` |
| 3 | Reader realisasi + penyatuan di `GetARAging` | `reporting/service.go`, `reporting/ar_aging.go`, `reporting/handler.go` |
| 4 | Repo house isi `Source`/`Label`/`RefID` | `reporting/repository.go` |
| 5 | Port sub-ledger milik `sale` | `internal/sale/subledger.go` (baru) |
| 6 | `charge` memakai port; berhenti import `reporting` | `charge/service.go`, `charge/query.go` |
| 7 | Blok `exposure` di statement | `sale/statement.go`, `sale/service.go` |
| 8 | Wiring | `cmd/api/main.go`, `reporting/handler.go` |
| 9 | Test (unit + integrasi INV-AR-1..3, INV-OWN-1) | `receivable/*_test.go`, `reporting/*_integration_test.go` |
| 10 | Frontend (§4) | lihat spec |

---

## 4. Spec Frontend W-4

### 4.1 Peta halaman

| Halaman | Perubahan |
|---|---|
| `/accounting/receivable` — **Piutang Customer** | Dari "aging cicilan rumah" menjadi **satu eksposur**: filter sumber, kolom Sumber, kartu subtotal per sumber |
| `/accounting/receivable/[contractId]` — **Statement** | Kartu header eksposur tunggal di atas seksi harga rumah & realisasi yang sudah ada |
| `/accounting/collection` | Tidak berubah bentuk; angkanya ikut karena sumber datanya sama |

Tidak ada halaman baru. W-4 adalah pekerjaan *penyatuan*, dan penyatuan yang
menambah halaman adalah kontradiksi.

### 4.2 Wireframe — `/accounting/receivable`

```
┌──────────────────────────────────────────────────────────────────────┐
│ Piutang Customer                                    [Export CSV]     │
│ Seluruh yang masih ditagih ke customer — harga rumah dan biaya       │
│ realisasi dalam satu daftar.                                          │
├──────────────────────────────────────────────────────────────────────┤
│ Per tanggal [2026-08-09]   Sumber: (•Semua) ( )Harga Rumah ( )Realisasi│
├──────────────────────────────────────────────────────────────────────┤
│ ┌─ Total Piutang ─┐ ┌─ Belum J.T. ─┐ ┌─ Overdue ─┐ ┌─ Collection ─┐  │
│ │  Rp 1.240.000.000│ │ Rp 890.000.000│ │Rp350.000.000│ │  73,50 %     │  │
│ └─────────────────┘ └──────────────┘ └───────────┘ └──────────────┘  │
│ ┌────────────────────────────────────────────────────────────────┐   │
│ │ Harga Rumah      Rp 1.100.000.000   overdue Rp 300.000.000     │   │  ← BARU
│ │ Biaya Realisasi  Rp   140.000.000   overdue Rp  50.000.000     │   │
│ └────────────────────────────────────────────────────────────────┘   │
├──────────────────────────────────────────────────────────────────────┤
│ Sumber │ Pembeli │ Unit / Tagihan │ Invoice │ J.Tempo │ Sisa │ Umur   │
│ ─────────────────────────────────────────────────────────────────────│
│ [Rumah]│ Budi S. │ A-12 · Cicilan 3│ INV/…/7 │ 10 Jul  │ 60jt │ 30 hr │
│ [Real.]│ Budi S. │ A-12 · PDAM     │ —       │ 20 Jul  │  4jt │ 20 hr │
└──────────────────────────────────────────────────────────────────────┘
```

Baris realisasi dan baris rumah **berbaur dan diurut bersama menurut jatuh
tempo**. Mengelompokkannya kembali per sumber akan mengembalikan dua daftar yang
justru sedang kita hapus; kolom Sumber + filter sudah cukup untuk memisahkan
saat memang dibutuhkan.

### 4.3 Wireframe — kartu eksposur di Statement

```
┌──────────────────────────────────────────────────────────────────────┐
│ Total yang masih ditagih ke Budi Santoso           Rp 64.000.000     │
│ ├ Harga rumah          Rp 60.000.000   (menunggak Rp 60.000.000)     │
│ └ Biaya realisasi      Rp  4.000.000   (menunggak Rp  4.000.000)     │
│ Per 9 Agu 2026 · dihitung server                                      │
└──────────────────────────────────────────────────────────────────────┘
```

Ditempatkan **di atas** tabel cicilan, sebelum seksi realisasi — pertanyaan
pertama admin saat membuka rekening koran adalah "berutang berapa", bukan
"cicilan nomor berapa".

Menunggak = 0 → baris rincian tetap tampil dengan angka nol, **tidak
disembunyikan**. Menyembunyikan nol membuat admin ragu apakah datanya belum
dimuat atau memang nihil.

### 4.4 State & empty state

| State | Tampilan |
|---|---|
| Memuat | skeleton kartu + 5 baris tabel |
| Gagal | `ErrorState` yang sudah ada — dibedakan tegas dari kosong |
| Kosong (filter `all`) | "Belum ada piutang. Tagihan muncul di sini setelah jadwal cicilan dibuat atau tagihan biaya realisasi diterbitkan." + CTA → `/penjualan` |
| Kosong (filter `realization`) | "Tidak ada tagihan biaya realisasi yang jatuh tempo." + CTA "Lihat semua sumber" (reset filter) |
| Kosong (filter `house`) | "Tidak ada cicilan harga rumah yang jatuh tempo." + CTA "Lihat semua sumber" |

Empty state per-filter dibedakan karena "kosong karena difilter" dan "kosong
karena memang belum ada" adalah dua situasi berbeda dengan tindakan berbeda.

### 4.5 Aksi

| Aksi | Perilaku |
|---|---|
| Ubah tanggal | `router.push(?as_of=…&source=…)` — filter dipertahankan |
| Ubah sumber | `router.push(?as_of=…&source=…)` — tanggal dipertahankan |
| Klik baris `house` | → `/accounting/receivable/{contract_id}` (statement) |
| Klik baris `realization` | → `/penjualan/{unit_id}/tagihan` (kelola tagihan) |
| Export CSV | ikut `source` yang aktif |

### 4.6 Endpoint yang dipakai

| Endpoint | Perubahan |
|---|---|
| `GET /reports/ar-aging?as_of=&source=` | **additive**: `source` per baris, `by_source`, query `source` |
| `GET /sale-contracts/{id}/statement?as_of=` | **additive**: blok `exposure` |
| `GET /charges/aging?as_of=` | tetap; kini mesin & penghasil baris yang sama |

Semua perubahan additive — tidak ada field yang dihapus atau berganti arti,
sehingga layar lama tetap benar selama transisi.
