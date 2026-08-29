# W-5 — J-14a: Biaya Realisasi menjadi Piutang Customer

**Status:** desain final, siap implementasi
**Tanggal:** 2026-08-10
**Prasyarat:** W-1 (master jenis biaya), W-2 (penomoran), W-3 (INV-DOC-1), W-4 (kepemilikan piutang) — semuanya SHIPPED.
**Menutup:** J-14 (`docs/business-architecture-freeze-2026-08.md` §6.1/§6.2), G-8, G-9, R-5, R-3, R-4.

---

## 1 · Keputusan yang mengikat

Tiga keputusan klien yang sudah final dan **tidak dibuka lagi**:

| Kode | Isi | Sumber |
|---|---|---|
| **D-3** | Sisa biaya realisasi menjadi **Piutang Customer** (J-14a). Gate BAST tidak boleh mensyaratkan realisasi lunas. Tidak boleh ada mekanisme piutang kedua. | keputusan klien |
| **R-5** | **Piutang lahir saat Invoice diterbitkan** — bukan saat grup dibuat, bukan saat item ditambah. | validasi 2026-08 §2.4 |
| **R-3** | Grup berjalan disamakan lewat **jurnal susulan sekali jalan**, append-only, satu aturan. | validasi 2026-08 §2.4 |
| **R-4** | Lebih bayar adalah **pilihan admin** (refund / alih item / buyer credit). Tidak boleh mengendap sebagai piutang bersaldo kredit. | validasi 2026-08 §2.4 |

### 1.1 Mendamaikan D-3 dengan R-5

D-3 berbunyi: *pada saat BAST*, posisi kumulatifnya `Dr Kas 8jt / Dr Piutang 12jt / Cr Titipan 20jt`.
R-5 berbunyi: piutang hanya boleh lahir dengan dasar dokumen — yaitu Invoice.

Keduanya dipenuhi sekaligus dengan satu aturan:

> **BAST menerbitkan invoice realisasi yang belum terbit.** Piutangnya tetap lahir dari invoice (R-5), dan pada saat BAST sisanya sudah menjadi Piutang Customer (D-3).

Ini bukan gate: BAST tidak pernah ditolak karena realisasi belum lunas. Yang dilakukan BAST adalah *menerbitkan tagihan*, bukan *menuntut pembayaran*.

---

## 2 · Model pengakuan

### 2.1 Keadaan yang disimpan

Pengakuan adalah atribut **item**, bukan grup — karena akun kewajibannya pun atribut item (W-1).

```
charge_items
  + recognized_amount     DECIMAL(20,4) NOT NULL DEFAULT 0   -- nominal tagihan yang SUDAH di-invoice
  + recognized_at         DATETIME(3)   NULL                 -- kapan pertama kali diakui
  + recognized_due_date   DATE          NULL                 -- jatuh tempo piutangnya
  + recognized_invoice_id BIGINT UNSIGNED NULL               -- invoice yang mengakuinya (terakhir)
```

Kontribusi item ke akun `1-2000` **diturunkan**, tidak disimpan dua kali:

```
kontribusi(item) = max(0, recognized_amount − paid)      untuk item open
                 = 0                                      untuk item cancelled
```

`paid` memakai formula kanonik yang sudah ada (`charge_item` + `charge_item_void`). `max(0, …)` itulah penegakan **R-4**: kelebihan bayar tidak pernah menjadi piutang bersaldo kredit — ia jatuh ke residual `2-2400` dan menunggu disposisi admin.

### 2.2 Jejak audit

```
charge_receivable_recognitions   (append-only)
  tenant_id, charge_group_id, charge_item_id, unit_id
  delta                     DECIMAL(20,4)   -- bertanda: + menambah piutang, − mengurangi
  reason                    VARCHAR(30)     -- invoice | bast | payment | void_payment |
                                            -- transfer_in | cancel_item | cancel_group |
                                            -- trueup | catch_up | sale_cancelled
  deposit_account_code, receivable_account_code
  journal_entry_id, invoice_id, created_by
```

### 2.3 Invariant

> **INV-REC-1** — untuk setiap `charge_item`:
> `Σ charge_receivable_recognitions.delta == max(0, recognized_amount − paid)` bila item open,
> `== 0` bila item cancelled.

> **INV-REC-2** — setiap perubahan kontribusi WAJIB punya baris jurnal terposting. Tidak ada perubahan piutang tanpa jurnal, dan tidak ada jurnal tanpa baris audit.

INV-REC-1 diuji langsung dari database (integration test), bukan dari memori proses.

---

## 3 · Titik jurnal

| Operasi | Sebelum W-5 | Setelah W-5 |
|---|---|---|
| `CreateGroup` / `AddItems` | tanpa jurnal | **tetap tanpa jurnal** (R-5) |
| `IssueInvoice` | tanpa jurnal | **`Dr 1-2000 / Cr <akun titipan per item>`** sebesar outstanding item |
| `RecordBAST` | gate menolak bila belum lunas | **gate dicabut**; invoice realisasi diterbitkan otomatis |
| `ReceivePayment` | `Dr Kas / Cr 2-2400` | `Dr Kas / Cr 1-2000` untuk porsi item yang sudah diakui; sisanya tetap `Cr <titipan>` |
| `TransferToGroup` (sisi tujuan) | `Cr 2-2400` tujuan | `Cr 1-2000` untuk porsi item tujuan yang sudah diakui |
| `RecordPayout` | `Dr 2-2400 / Cr Kas` | **tidak berubah** (K-5 raise tidak menaikkan piutang — belum di-invoice) |
| `Refund` | `Dr 2-2400 / Cr Kas` | **tidak berubah** (refund berasal dari residual, bukan dari piutang) |
| `VoidPayment` | `Dr 2-2400 / Cr Kas` | membalik **persis apa yang dulu dikredit** (porsi 1-2000 + porsi titipan) |
| `CancelItem` / `CancelGroup` | tanpa jurnal | **jurnal pembalik** bila item sudah diakui |
| `TrueUpAndSettle` | tanpa jurnal | **jurnal penyesuaian** bila tagihan turun di bawah nilai yang sudah diakui |
| Pembatalan penjualan pasca-BAST | tidak menyentuh charge | **membalik seluruh pengakuan** unit itu |

Aturan tunggal yang menghasilkan seluruh baris di atas:

```
target  = max(0, recognized_amount − paid)   (0 bila item cancelled)
delta   = target − kontribusi_tercatat
delta > 0 → Dr 1-2000 / Cr <akun titipan item>
delta < 0 → Dr <akun titipan item> / Cr 1-2000
delta = 0 → tidak ada jurnal
```

Pembayaran dan void tidak menerbitkan jurnal pengakuan terpisah: perubahan kontribusinya sudah menjadi **baris kredit/debit di dalam jurnal kasnya sendiri**. Satu peristiwa, satu jurnal.

### 3.1 Kenapa K-5 (payout overrun) tidak menaikkan piutang

Payout melebihi tagihan menaikkan `charge_items.amount`, tapi **tidak** `recognized_amount`. Kenaikan itu belum pernah ditagihkan ke customer lewat dokumen apa pun; menjadikannya piutang berarti menagih tanpa dasar dokumen — persis yang R-5 larang. Kelebihannya muncul sebagai *outstanding belum di-invoice* di layar tagihan, dengan CTA menerbitkan invoice susulan.

### 3.2 Konsekuensi yang harus dipahami pembaca laporan

Setelah sebuah grup di-invoice, saldo `2-2400` grup itu **tidak lagi sama dengan residual**:

```
saldo 2-2400 grup = residual + Σ kontribusi piutang
```

Ini benar secara akuntansi (kewajiban ke pihak ketiga sudah diakui penuh saat tagihan terbit) tetapi berbeda dari angka "Sisa Titipan" di layar grup, yang tetap berarti *uang tunai yang masih dipegang*. Kedua angka ditampilkan berdampingan di UI dengan label eksplisit.

---

## 4 · Aging & statement

`charge.receivableRows` berubah dari "setiap item open ber-jatuh-tempo" menjadi **"setiap item yang sudah diakui"**:

```sql
WHERE ci.status = 'open' AND cg.status = 'open'
  AND ci.recognized_at IS NOT NULL
  AND ci.recognized_amount > 0
```

dengan `amount = recognized_amount` dan `due_date = COALESCE(ci.due_date, ci.recognized_due_date)`.

Akibatnya laporan piutang **sama persis** dengan buku besar `1-2000` — inilah yang J-14b tidak bisa berikan dan alasan D-3 dipilih. Item yang belum di-invoice tidak hilang: ia tampil di layar Tagihan sebagai *"Belum ditagihkan"* dengan CTA Terbitkan Invoice, dan di ringkasan portofolio sebagai backlog. Yang berubah hanyalah: ia bukan piutang sampai ada dokumennya.

---

## 5 · R-3 — jurnal susulan sekali jalan

Perintah CLI `backfill-recognition` (bukan endpoint publik; sekali jalan, ber-audit):

1. Cari setiap grup `open` yang **sudah pernah punya invoice REALISASI** (`invoices.charge_group_id`, status ≠ cancelled).
2. Untuk setiap item open: `recognized_amount := amount`, `recognized_at := invoice.issue_date`, `recognized_due_date := COALESCE(item.due_date, invoice.due_date)`, `recognized_invoice_id := invoice.id`.
3. Posting satu jurnal per grup, tanggal cut-off, `Dr 1-2000 / Cr <akun titipan>` sebesar Σ `max(0, amount − paid)`, reason `catch_up`.
4. Idempoten: grup yang sudah punya baris audit `catch_up` dilewati.

Grup yang **belum pernah** di-invoice tidak disentuh sama sekali — tidak ada dua aturan berjalan bersamaan (R-3).

---

## 6 · Pencabutan gate BAST

Dihapus:

- `sale.RealizationBASTGate` + field `realizationGate` + `SetRealizationBASTGate` + pemanggilannya di `RecordBAST`
- `sale.ErrRealizationOutstandingBAST`
- `charge.CheckBAST`, `RequireRealizationSettled`, `SetRequireRealizationSettled`, `requireRealizationSettledDB`, `realizationOutstandingByUnit`
- endpoint `GET /charges/policy`, `PUT /charges/policy`
- kolom `tenants.require_realization_settled`
- `BillingPolicySection` toggle di frontend

**Dipertahankan:** `tenant_policy_changes` + `ListPolicyChanges` + `GET /charges/policy/history`. Audit kebijakan bersifat append-only (R-A): riwayat tidak boleh ikut hilang hanya karena kebijakannya dicabut. Migrasi menuliskan satu baris terakhir `true → false` untuk setiap tenant yang gate-nya menyala, dengan catatan alasannya — sehingga riwayatnya tetap utuh dan bisa dijelaskan saat audit.

---

## 7 · Pembatalan penjualan pasca-BAST

`cancellation.Process` hari ini membalik Event 3, mirror HPP, dan akrual pajak — tetapi tidak tahu apa-apa tentang charge. Setelah W-5, penjualan yang dibatalkan bisa meninggalkan piutang realisasi menggantung di `1-2000`. Karena itu ditambahkan seam:

```go
// sale/cancellation seam — diimplementasikan charge.Service
ReverseRecognitionInTx(ctx, tx, tenantID, unitID, createdBy) error
```

Membalik seluruh kontribusi piutang unit itu (`Dr <titipan> / Cr 1-2000`), `recognized_amount := 0`, baris audit `sale_cancelled`. Dana yang sudah diterima **tidak** ikut disentuh: disposisinya tetap urusan refund/transfer (R-4).

---

## 8 · Spesifikasi Frontend

### 8.1 Peta halaman

| Halaman | Perubahan |
|---|---|
| `/tagihan/[groupId]` (detail grup) | badge status pengakuan; blok "Piutang & Titipan"; CTA invoice diperjelas |
| `/tagihan` (portofolio) | kolom baru **Belum ditagihkan**; filter cepat |
| `/pengaturan` | `BillingPolicySection` → `PolicyHistorySection` (riwayat saja, tanpa toggle) |
| `/piutang` (aging) | catatan kaki: baris realisasi = yang sudah di-invoice |
| BAST (`RecordBASTButton`) | teks konfirmasi memberitahu invoice realisasi akan diterbitkan otomatis |

### 8.2 Wireframe — detail grup

```
┌─ Tagihan Realisasi — Blok A2 ──────────────────────────────┐
│ [Sudah ditagihkan · INV/2026/000123]        [Cetak Invoice]│
├────────────────────────────────────────────────────────────┤
│  Total Tagihan   Dibayar     Sisa Tagihan   Sisa Titipan   │
│  Rp20.000.000    Rp8.000.000 Rp12.000.000   Rp3.000.000    │
│                               └ piutang      └ kas dipegang│
├────────────────────────────────────────────────────────────┤
│ Item              Jenis   Tagihan  Dibayar  Sisa   Status  │
│ Notaris           notary   10.000    10.000     0  ✔ lunas │
│ BPHTB             bphtb     5.000     0     5.000  ● piutang│
│ Listrik           listrik   5.000     0     5.000  ● piutang│
│ Sertifikat (baru) notary    2.000     0     2.000  ○ belum ditagihkan │
├────────────────────────────────────────────────────────────┤
│ ⓘ Rp2.000.000 belum ditagihkan — belum menjadi piutang.    │
│   [Terbitkan Invoice Susulan]                              │
└────────────────────────────────────────────────────────────┘
```

Status per item (satu kolom, tiga keadaan):

- `○ Belum ditagihkan` — `recognized_at == null` → abu-abu
- `● Piutang` — diakui & masih ada sisa → amber
- `✔ Lunas` — diakui & sisa nol → hijau

### 8.3 Alur

```
Admin membuat grup + item
        ↓                      (tidak ada jurnal, tidak ada piutang)
[Terbitkan Invoice]  ──────▶  konfirmasi: "Rp12.000.000 akan diakui
        ↓                      sebagai Piutang Customer. Lanjutkan?"
Invoice terbit ────────────▶  badge grup: Sudah ditagihkan
        ↓                      baris muncul di /piutang
Customer bayar ────────────▶  sisa tagihan turun, badge item → Lunas
        ↓
BAST tanpa syarat lunas ───▶  invoice otomatis bila masih ada yang belum
```

### 8.4 State & aksi

| State | Sumber | Aksi |
|---|---|---|
| `group.recognized` (turunan: ada item `recognized_at`) | `GET /charges/groups/{id}` | menentukan badge & label CTA |
| `group.unbilled` = Σ `amount − recognized_amount` item open | payload grup | menampilkan blok "Belum ditagihkan" |
| `item.recognition_status` | payload item | warna badge per baris |

Aksi: **Terbitkan Invoice** (`POST /charges/groups/{id}/invoice`) — satu-satunya aksi baru; teks tombol berubah menjadi *"Terbitkan Invoice Susulan"* bila grup sudah pernah di-invoice dan masih ada `unbilled > 0`.

**Yang ditagihkan ≠ piutang yang terbentuk.** `unbilled` adalah nominal yang akan *diakui*; piutang yang lahir adalah `Σ max(0, recognized_amount − paid)`. Bila ada uang yang sudah diterima sebelum ditagihkan (titipan murni), piutangnya lebih kecil dari nilai yang ditagihkan. Modal karena itu tidak boleh menyebut angka `unbilled` sebagai "akan menjadi Piutang Customer"; ia menyebutnya **"Nilai yang akan ditagihkan"** dan — hanya bila `group.paid > 0` — menambahkan peringatan bahwa pembayaran di muka langsung memotong piutang. Selisih pastinya tidak dihitung di browser: angka final datang dari payload grup setelah refresh.

### 8.5 Endpoint

| Metode | Path | Perubahan |
|---|---|---|
| `POST` | `/charges/groups/{id}/invoice` | sekarang transaksional + memposting jurnal pengakuan; respons menambahkan `recognized_amount` |
| `GET` | `/charges/groups/{id}` | item menambahkan `recognized_amount`, `recognized_at`, `recognition_status`, `receivable`, `unbilled`; grup menambahkan `recognized`, `unbilled`, `receivable`, `invoice_number` |
| `GET` | `/charges/policy` | **dihapus** |
| `PUT` | `/charges/policy` | **dihapus** |
| `GET` | `/charges/policy/history` | tetap (audit append-only) |

### 8.6 Empty state

Grup tanpa item yang diakui:

> **Belum ditagihkan.** Tagihan ini masih berupa draft — belum menjadi piutang dan belum muncul di laporan piutang. Terbitkan invoice untuk menagihkannya ke customer.
> `[Terbitkan Invoice]`

---

## 9 · Yang sengaja TIDAK dikerjakan

- **State grup ke-4 `receivable`** (usulan freeze §8.4). Pengakuan adalah atribut item, dan satu grup bisa memuat item yang diakui dan yang belum sekaligus — state di level grup akan berbohong. Badge turunan sudah cukup.
- **Mengubah `Refund`.** Refund mengambil dari residual (uang yang benar-benar dipegang), bukan dari piutang. Tidak ada yang perlu berubah.
- **Piutang untuk grup `addon`.** Addon bukan titipan; jalur produknya dipindah di W-6. Pengakuan hanya berlaku untuk `kind = realization`.
