# W-10 (kandidat) — Transaksi Pengeluaran: AUDIT + DESAIN

**Status:** AUDIT + DESAIN SAJA — tidak ada kode, migrasi, seed, data uji, atau perubahan
file kode dalam pekerjaan ini. Dokumen ini satu-satunya keluaran.
**Tanggal:** 2026-08-13
**Metode:** pembacaan kode aktual (`backend/internal/**`, `frontend/**`). Dokumen desain lama
TIDAK diperlakukan sebagai kebenaran; setiap kali dokumen dan kode berbeda, yang menang kode
dan selisihnya dilaporkan sebagai temuan.

Notasi: **FACT** = terbaca langsung di kode (ada rujukan berkas:baris). **INFERENCE** =
kesimpulan dari fakta-fakta itu, ditandai tingkat keyakinannya.

---

## 1. Temuan audit

### T-1 (FACT) — Jalur biaya operasional tingkat-tenant SUDAH ADA di backend

`internal/cost/handler.go:93-100` memasang rute tanpa proyek:

```go
r.Route("/cost-entries", func(r chi.Router) {
    r.With(auth.RequireWrite()).Post("/", h.createTenantCostEntry)
    r.Post("/preview", h.previewTenantCostEntry)
    r.Route("/{id}", func(r chi.Router) {
        r.Get("/", h.getCostEntry)
        r.With(auth.RequireWrite()).Post("/post", h.postCostEntry)
    })
})
```

`service.validate` (`cost/service.go:238-249`) secara eksplisit mengizinkan `ProjectID == 0`
untuk tier `overhead`, dan `CreateCostEntry` (`:352-356`) menulis `project_id = NULL` pada
baris jurnalnya. Jadi "beban kantor, tanpa proyek, Dr beban / Cr bank" **bukan kemampuan
baru** — ia sudah berjalan di backend hari ini.

### T-2 (FACT) — Jalur itu tidak pernah dipanggil frontend

Seluruh pemanggilan di frontend memakai varian ber-proyek:

```
app/(app)/proyek/[id]/biaya/actions.ts:23  /projects/{id}/cost-entries/preview
app/(app)/proyek/[id]/biaya/actions.ts:38  /projects/{id}/cost-entries
lib/api/cost.ts:5,26,47                    /projects/{id}/cost-entries…
```

Tidak ada satu pun pemanggilan `POST /cost-entries` tingkat-tenant. **INFERENCE (TINGGI):**
keluhan klien "Proyek → Biaya terlalu terbatas" bukan keluhan tentang mesin akuntansi,
melainkan tentang **pintu masuk**: pintunya cuma satu dan letaknya di dalam proyek.

### T-3 (FACT) — Taksonomi beban adalah enum tertutup dua nilai

`internal/domain/cost_category.go:75-86` — `ExpenseAccountCode()` hanya memetakan
`marketing → "5-3000"` dan `other → "5-4000"`. `ExpenseCostCategories` (`:45`) berisi
tepat dua nilai itu.

Padahal COA bawaan (`internal/ledger/coa.go:92-101`) sudah punya akun bebannya:

| Kode | Nama | Bisa dicapai mesin biaya? |
|---|---|---|
| 5-3000 | Beban Pemasaran | ya (`marketing`) |
| 5-3100 | Beban Komisi Penjualan | tidak |
| 5-4000 | Beban Umum & Administrasi | ya (`other`) |
| 5-4100 | Beban Gaji & Tunjangan | tidak |
| 5-4200 | Beban Sewa Kantor | tidak |
| 5-4300 | Beban Utilitas | tidak |
| 5-4400 | Beban Perjalanan Dinas | tidak |
| 5-4500 | Beban Penyusutan | tidak |
| 5-5000 | Beban Bunga | tidak |

**INFERENCE (TINGGI):** listrik, internet, ATK, software, dan transport yang diminta klien
hari ini semuanya akan mendarat di `other → 5-4000`. Laporan laba rugi lalu memperlihatkan
satu gumpalan "Beban Umum & Administrasi" — persis informasi yang ingin dipecah klien.
**Ini gap yang sesungguhnya**, dan letaknya di taksonomi, bukan di ledger dan bukan di COA.

### T-4 (FACT) — `"2-1000"` di-hardcode di mesin biaya

`internal/cost/service.go:186-196`:

```go
creditCode = "2-1000" // Hutang Usaha (default)
if req.PaymentMethod == PaymentMethodBank {
    creditCode = req.BankAccountCode
}
```

Watch-out #3 ("jangan hardcode kode akun") **sudah dilanggar hari ini**, bukan oleh fitur
baru. `ledger.RoleCodeList(ledger.RolePayable)` (`account_role.go:48`) adalah pintu kanonik
yang tersedia dan tidak dipakai di sini.

### T-5 (FACT, MEDIUM-HIGH) — `bank_account_code` tidak pernah divalidasi sebagai kas/bank

`validate` hanya memeriksa **tidak kosong** (`cost/service.go:262-264`), lalu
`ResolveAccount` (`cost/repository.go:27-37`) mencari akun berdasarkan kode **tanpa filter
tipe, kategori, maupun `is_active`**.

Konsekuensi: mengirim `payment_method="bank"`, `bank_account_code="4-1000"` menghasilkan
jurnal balanced `Dr 5-4000 / Cr 4-1000` — mengkredit **pendapatan**, bukan bank. Karena
akun itu bukan kas/bank, `cashMovement` (`ledger/document_issuer.go:160-179`) menyimpulkan
jurnal ini tidak menyentuh kas, sehingga **tidak ada BKK yang terbit** dan INV-DOC-1 tidak
menyala. Ledger tetap konsisten menurut definisinya sendiri; yang salah adalah faktanya.

Ini bertentangan dengan aturan COA-driven yang sudah dipatok di tempat lain:
`ledger.ValidatePaymentAccount` (`account_category.go:36-50`) menolak akun non-aset,
nonaktif, dan berkategori bukan `cash|bank`, dan dipakai jalur penerimaan pembayaran.

### T-6 (FACT) — Frontend biaya memperkuat masalahnya

`frontend/components/biaya/CostEntryForm.tsx`:

- `:20-25` — empat kategori di-hardcode di frontend.
- `:155-158` — teks di layar: *"Kategori Pemasaran & Lain-lain (beban langsung) tidak
  tersedia di sini — catat via Jurnal Umum…"* Ini kalimat yang dikutip klien.
- `:195-204` — **input teks bebas** "Kode Akun Bank" (`placeholder="mis. 1-1100"`),
  bukan `CashBankSelect` yang dipakai layar pembayaran lain. Inilah yang membuat T-5 bisa
  terjadi lewat UI, bukan hanya lewat API.

### T-7 (FACT, HIGH untuk UX) — Jalan keluar yang disarankan layar itu sendiri **gagal** untuk pembayaran kas

`frontend/components/accounting/JournalCreateForm.tsx:100-113` — tombol **"Simpan &
Posting"** memanggil `postJournal(token, entry.id)` **tanpa `document_type`**.

Backend: untuk jurnal kas KELUAR, `ManualDocumentChoices` mengembalikan tiga pilihan
(`ledger/document_issuer.go:243-251`), dan `ResolveManualDocument` mengembalikan
`ErrDocumentTypeRequired` bila tidak ada yang dipilih (`:268-273`) → handler membalas
**400** (`ledger/handler.go:532-539`).

**INFERENCE (TINGGI):** admin yang mengikuti instruksi di layar Biaya — "catat via Jurnal
Umum" — mengisi jurnal `Dr 5-4300 / Cr 1-1300`, menekan "Simpan & Posting", dan menerima
error. Jurnalnya tersimpan sebagai draft; ia baru bisa diposting dari halaman detail jurnal
(`JournalDetail.tsx:70` memang mengirim `doc.docType`). Jadi jalur resmi untuk biaya kantor
hari ini adalah jalur yang patah di tombol pertamanya.

### T-8 (FACT) — Uang memakai `parseFloat` di form jurnal manual

`JournalCreateForm.tsx:20-27,66` — `parseFloat`, penjumlahan `number`, dan cek seimbang
`Math.abs(totalDebit - totalCredit) < 0.001`. Backend tetap memparse string ke desimal
(`ledger/handler.go:783-788`), jadi ledger tidak tercemar; tapi pola ini bertentangan
dengan Invariant #2 di lapisan UI dan sudah pernah dikoreksi di komponen lain
(`RupiahInput`/`validateRupiah` di form biaya). Bukan blocker W-10, tapi jangan ditiru.

### T-9 (FACT) — Tidak ada jalur pelunasan Hutang Usaha

Pencarian `"2-1000"` di seluruh `internal/` (non-test) hanya menemukan tiga tempat:
definisi COA, registry peran, dan hardcode T-4. **Tidak ada satu pun kode yang MENDEBIT
2-1000.** Artinya `payment_method = "payable"` hari ini adalah jalan buntu: utangnya lahir,
dan satu-satunya cara melunasinya adalah jurnal manual.

### T-10 (FACT) — Tidak ada pembatalan biaya

`cost/handler.go:93-100` tidak punya rute hapus/void/reverse. Koreksi biaya yang sudah
diposting hanya bisa lewat pintu umum `POST /ledger/journals/{id}/reverse`, dan pintu itu
**terbuka** untuk jurnal biaya karena hanya `sale` yang mendaftarkan kepemilikan
(`cmd/api/main.go:162` — satu-satunya pemanggil `AddJournalOwnership`).

Akibat praktisnya: membalik jurnal biaya dari layar Jurnal membuat GL benar, tetapi baris
`cost_entries` tetap tampil di daftar Biaya **tanpa penanda apa pun** bahwa jurnalnya sudah
dibalik. (Angkanya tidak ikut salah — lihat T-11.)

### T-11 (FACT) — Total biaya berasal dari jurnal, bukan dari `cost_entries`

`internal/ledger/actual_cost.go:10-23,106-107` adalah pembaca kanonik tunggal:

```
ActualCost = Σ debit (jurnal non-reversal) − Σ kredit (jurnal source='reversal')
```

Konsumennya `cost.AccumulatedBy*`, `allocation`, `budget` realisasi, `closing`, dashboard.
`budget/repository.go:280-285` menegaskannya: *"BUKAN lagi Σ cost_entries.amount…
Jurnal manual ber-tag project pun kini terhitung (SoT = jurnal)."*

**Konsekuensi desain terpenting di dokumen ini:** setiap jurnal yang benar dan ber-tag
`project_id` **sudah** masuk ke realisasi proyek tanpa tabel tambahan apa pun.

### T-12 (FACT) — Tapi drill-down per item RAB memang membaca `cost_entries`

`budget/repository.go:328-350` — `GetRealisasiPerItem` menjumlahkan `cost_entries.amount`
per `budget_item_id` (hanya yang jurnalnya sudah diposting). Tidak ada kolom
`budget_item_id` di `journal_lines`.

**INFERENCE (TINGGI):** pengeluaran yang ingin ditautkan ke **item RAB** wajib melewati
`cost_entries`. Fitur pengeluaran yang menulis tabel sendiri tidak akan pernah bisa
menautkan ke RAB tanpa menduplikasi mekanisme ini — inilah alasan teknis paling keras untuk
tidak membuat domain baru.

### T-13 (FACT) — Konflik dokumen vs kode: isolasi tenant

`CLAUDE.md` menyatakan *"Isolasi tenant via GORM global scope … GORM global scope
`WHERE tenant_id = ?` di setiap repository"*. Kode tidak memakai global scope: tidak ada
`Scopes(`/global-scope di `internal/platform`, dan setiap repository menulis predikatnya
sendiri (`cost/repository.go:31,51,61,73`). Yang berlaku adalah **predikat eksplisit
per-query + integration test isolasi** (`ledger/tenant_isolation_test.go`).

Dilaporkan apa adanya sesuai instruksi; **bukan** usulan mengubah CLAUDE.md dalam W-10.

### T-14 (FACT) — Tidak ada master vendor

`vendor` adalah kolom teks bebas `size:200` di `cost_entries` (`cost/model.go:56`) dan di
`charge`. Tidak ada tabel vendor, tidak ada normalisasi.

### T-15 (FACT) — Tidak ada idempotency pada pencatatan biaya

`CreateCostEntry` tidak menerima kunci idempoten dan tidak punya unique key alami.
Dua klik = dua entri + dua jurnal. Bandingkan `charge`/`collection` yang memang punya
idempotency. Belum pernah menjadi masalah karena volumenya rendah; akan naik kalau layar
pengeluaran dipakai harian.

---

## 2. Seam yang sudah ada dan bisa dipakai

| Seam | Berkas | Yang diberikannya untuk W-10 |
|---|---|---|
| `PostingService.Create/PostDraft/CreateAndPost/Reverse` | `ledger/posting_service.go:132,224,288,330` | satu-satunya penulis jurnal; balanced + whole-rupiah + periode dijaga di sini |
| `PeriodChecker` | `posting_service.go:134,356` | periode tertutup ditolak saat buat **dan** saat balik (tanggal pembalik) |
| `DocumentSpec` + `DefaultCashSpec` + `ManualDocumentChoices` | `ledger/document_issuer.go:50,199,243` | penentuan BKM/BKK otomatis dari arah kas; BTP/RFC sengaja tidak bisa ditebak |
| `enforceDocument` (fail-closed) | `document_issuer.go:371-391` | jurnal kas tanpa dokumen = transaksi batal |
| `AccountRoleRegistry` | `ledger/account_role.go` | pengganti sah untuk hardcode `2-1000` (T-4) |
| `AccountCategory` + `ValidatePaymentAccount` + `GET /ledger/accounts/cash-bank` | `account_category.go:36`, `handler.go:108` | pengganti sah untuk input teks bebas (T-5, T-6) |
| `cost.Service` + `TxRunner` | `cost/service.go:162,338` | mesin biaya proyek yang sudah atomik (jurnal + baris dalam satu tx) |
| `PreviewCostEntry` | `cost/service.go:309` | pratinjau jurnal dihitung backend — FE tidak pernah tahu kode akun |
| `charge.RealizationChargeType` + `MasterDataChange` + `validateDepositAccountDB` | `charge/charge_type.go` | **preseden W-1**: master data-driven, CRUD admin, audit append-only, validasi mapping akun fail-closed |
| `ledger.RecurringJournal` | `ledger/recurring.go` | biaya bulanan tetap (sewa, internet) sudah bisa diotomatiskan, lengkap dengan BKK otomatis (`:227-234`) |
| `budget.BudgetItemLookup` | `cost/service.go:60-67` | validasi tautan item RAB |
| `ledger.JournalOwnershipReader` | `ledger/handler.go:611-650` | seam penjaga pembalikan generik (baru dipakai `sale`) |

---

## 3. Yang SUDAH ADA (jangan dibangun ulang)

1. Pencatatan beban operasional tanpa proyek → `POST /cost-entries`, tier `overhead` (T-1).
2. Pencatatan beban ber-proyek sebagai cost center → `project_id` opsional di body yang sama.
3. Jurnal 2 baris Dr beban / Cr bank, atomik, ber-BKK otomatis saat diposting
   (`cost/service.go:425-437` → `PostJournal(..., cashOut)`).
4. Pratinjau jurnal dari backend sebelum simpan.
5. Akun beban granular di COA (T-3) — akunnya ada, yang tidak ada adalah jalan menujunya.
6. Penomoran dokumen BKK/BKM/JR, registry dokumen, dan penegakan INV-DOC-1.
7. Penjagaan periode tertutup di kedua arah (buat & balik).
8. Realisasi proyek yang otomatis menghitung jurnal ber-tag proyek (T-11).
9. Template jurnal berulang untuk beban bulanan tetap.
10. Mesin approval (`internal/approval`) — opt-in, bisa dipasang di atas pengeluaran bila
    klien menginginkannya.

## 4. Yang GENUINELY belum ada

| # | Kekurangan | Bukti |
|---|---|---|
| G-A | **Granularitas akun beban.** Tidak ada cara memilih 5-4300 Utilitas, 5-4200 Sewa, dst. dari jalur biaya mana pun | T-3 |
| G-B | **Pintu masuk pengeluaran di UI.** Endpoint tingkat-tenant tidak punya layar | T-2 |
| G-C | **Validasi akun pembayaran di jalur biaya.** Kode bank bebas, tanpa cek COA | T-5, T-6 |
| G-D | **Akun lawan non-kas yang benar.** `2-1000` hardcode, dan tidak ada jalur pelunasannya | T-4, T-9 |
| G-E | **Status & pembatalan biaya.** Daftar biaya tidak tahu jurnalnya draft, terposting, atau sudah dibalik | T-10 |
| G-F | **Idempotency pencatatan** | T-15 |
| G-G | **Lampiran bukti (foto nota)** — tidak ada mekanisme attachment di seluruh sistem | pencarian kode |
| G-H | **PPN Masukan atas belanja** — akun 1-5100 ada, tidak ada jalur yang mengisinya | `coa.go:51` |

Perhatikan bahwa **tidak satu pun** dari G-A…G-H membutuhkan mesin akuntansi baru.

---

## 5. Alur akuntansi

Semua contoh memakai mesin yang sudah ada. Tidak ada jurnal baru yang ditemukan sendiri.

### 5.1 Pengeluaran operasional dibayar kas/bank

```
Internet kantor Rp1.500.000, dibayar Bank BCA, 13 Agustus 2026

Dr  5-4300  Beban Utilitas                 1.500.000
    Cr  1-1300  Bank — BCA                             1.500.000

source        = "system"       (dibuat cost.Service lewat PostingService)
project_id    = NULL           (atau diisi bila di-tag cost center — lihat BD-2)
unit_id       = NULL           (dilarang untuk tier overhead)
cost_tier     = overhead
dokumen       = BKK/2026/000123  (otomatis, karena mengkredit akun berkategori bank)
```

Perubahan terhadap hari ini: `5-4300` menggantikan `5-4000`, dan itu **satu-satunya**
perubahan akuntansi yang diperlukan untuk skenario ini.

### 5.2 Pengeluaran operasional belum dibayar

```
Dr  5-4300  Beban Utilitas                 1.500.000
    Cr  2-1000  Hutang Usaha                           1.500.000

dokumen = TIDAK ADA (benar — tidak ada kas yang bergerak)
```

Konsekuensi yang harus disadari klien: setelah ini **tidak ada layar mana pun** yang bisa
melunasi utang tersebut (T-9). Lihat BD-3.

### 5.3 Pengeluaran proyek — material/vendor

```
Material Rp20.000.000, proyek "Griya Asri", unit A-12, dibayar Bank BCA

Dr  1-3100  Persediaan Real Estat — Hard Cost   20.000.000
    Cr  1-1300  Bank — BCA                                  20.000.000

cost_tier  = direct       (karena unit_id diisi)
project_id / unit_id ter-tag di KEDUA baris
dokumen    = BKK
```

**Tidak berubah sama sekali** dari perilaku hari ini. Ini kapitalisasi, bukan beban; ia
mengalir ke HPP unit saat BAST lewat mekanisme yang sudah ada. Layar Pengeluaran hanya
menjadi pintu lain menuju `CreateCostEntry` yang sama.

### 5.4 Pengeluaran proyek — pemasaran proyek

```
Dr  5-3000  Beban Pemasaran                  8.000.000
    Cr  1-1300  Bank — BCA                                8.000.000

cost_tier = overhead, project_id = terisi, unit_id = NULL
```

Sudah berjalan hari ini lewat API; yang belum ada hanyalah layarnya.

### 5.5 Koreksi

Satu-satunya mekanisme sah: jurnal pembalik (`Invariant #5`). Tidak ada edit, tidak ada
hapus. Rincian di §13.

---

## 6. Model data konseptual

Prinsip: **tidak ada tabel transaksi baru.** Yang ditambahkan adalah satu master dan satu
kolom penunjuk.

### 6.1 Master baru — `expense_types` (Jenis Pengeluaran)

Cermin persis pola W-1 `realization_charge_types` (`charge/charge_type.go:38-50`):

```
expense_types
  id, tenant_id
  code                 (unik per tenant, dinormalisasi lowercase — pola normalizeChargeTypeCode)
  name                 "Listrik & Air", "Internet", "ATK", "Software/Langganan", "Transport"
  expense_account_code akun beban 5-xxxx tujuan debit
  is_active
  created_at, updated_at
```

Aturan yang menyertainya, semuanya sudah punya preseden:

- **Fail-closed resolver.** Kode tak terdaftar / nonaktif / mapping akun kosong → error
  **sebelum** ada jurnal (`chargeTypePolicyOf`).
- **Validasi mapping akun saat master disimpan, bukan saat uang bergerak.** Akun harus ada
  di COA tenant, aktif, dan bertipe `expense` (`validateDepositAccountDB` versi beban).
- **Audit append-only** perubahan master lewat `master_data_changes` yang sudah ada
  (`charge/charge_type.go:288-297`) — mengubah akun sebuah jenis pengeluaran adalah
  keputusan finansial.
- **Admin mengelolanya sendiri** (CRUD), tanpa programmer.

### 6.2 Perluasan `cost_entries` — satu kolom

```
cost_entries
  + expense_type_id  BIGINT UNSIGNED NULL   (index; hanya bermakna untuk cost_tier='overhead')
```

Aturan: bila `cost_tier = overhead` dan `expense_type_id` terisi, akun debit **berasal dari
master**, bukan dari `Category.ExpenseAccountCode()`. Bila kosong, perilaku lama berlaku
persis (`marketing → 5-3000`, `other → 5-4000`) — jadi seluruh data dan seluruh pemanggil
lama tidak berubah artinya.

`domain.CostCategory` **tetap** enum tertutup. Ia berhenti menjadi "penentu akun" untuk
overhead dan tinggal menjadi taksonomi kasar untuk pencocokan item RAB
(`validate` → `ErrBudgetItemCategoryMismatch`, `cost/service.go:270-277`). Ini penting:
mengubah enum menjadi terbuka akan merusak matriks tier×kategori dan pencocokan RAB.

### 6.3 Yang TIDAK ditambahkan, dan alasannya

| Ditolak | Alasan |
|---|---|
| tabel `expenses` / `transactions` sendiri | mesin biaya kedua; dan tidak akan bisa menautkan ke item RAB (T-12) |
| tabel `vendors` | hari ini teks bebas; menambah master adalah keputusan bisnis, bukan kebutuhan akuntansi (BD-6) |
| kolom `status` di `cost_entries` | status diturunkan dari jurnalnya (`posted_at`, `reverses_id`) — menyimpannya menciptakan sumber kebenaran kedua |
| jenis dokumen baru | BKK sudah tepat (§12) |
| tabel lampiran | belum terbukti diperlukan (BD-7) |

---

## 7. API konseptual

Endpoint **baru**: satu, dan itu pun hanya master.

```
GET    /expense-types                    daftar jenis pengeluaran (admin & dropdown)
POST   /expense-types                    buat        (RequireWrite)
PUT    /expense-types/{id}               ubah/nonaktifkan (RequireWrite)
```

Endpoint **yang sudah ada dan dipakai apa adanya**:

```
POST   /cost-entries/preview             pratinjau jurnal (dry-run)
POST   /cost-entries                     catat  (body: expense_type_id, cost_tier, amount,
                                          payment_method, bank_account_code, date, vendor,
                                          description, project_id?, unit_id?, phase_id?,
                                          budget_item_id?)
POST   /cost-entries/{id}/post           posting → BKK terbit bila kas keluar
GET    /cost-entries/{id}
GET    /ledger/accounts/cash-bank        sumber dropdown rekening (menggantikan input teks)
```

Perubahan pada endpoint lama (semuanya aditif, tidak memutus kontrak):

1. `create/preview` menerima `expense_type_id` opsional.
2. `bank_account_code` divalidasi lewat `ledger.ValidatePaymentAccount` (T-5) — ini
   **memperketat** kontrak: payload yang selama ini salah akan mulai ditolak 400. Itu
   diinginkan, tapi harus disebut sebagai perubahan perilaku.
3. `2-1000` diambil dari `RoleCodeList(RolePayable)` alih-alih literal (T-4).

**Catatan atomicity (bukan bagian kontrak, tapi harus diperbaiki bersamaan):**
frontend hari ini memanggil create lalu post sebagai **dua** permintaan HTTP
(`proyek/[id]/biaya/actions.ts:38,45`). Bila post gagal, tertinggal draft yatim. Backend
sudah punya `CreateAndPost` (`posting_service.go:288`); pilihan yang bersih adalah satu
endpoint `POST /cost-entries?post=true` atau server action yang menjamin urutannya. Ini
diusulkan sebagai bagian W-10 karena layar pengeluaran akan dipakai jauh lebih sering.

---

## 8. Alur UI/UX

### 8.1 Penempatan

`Keuangan → Transaksi → Pengeluaran` sebagai layar tingkat-tenant baru
(`app/(app)/transaksi/pengeluaran`). Layar `Proyek → [Proyek] → Biaya` **tetap ada** dan
tetap berfungsi — ia adalah tampilan ber-konteks proyek dari data yang sama. Yang dihapus
dari sana hanyalah kalimat "catat via Jurnal Umum", karena setelah W-10 kalimat itu tidak
lagi benar.

### 8.2 Bentuk formulir

Langkah 1 — **Jenis pengeluaran** (satu pilihan, menentukan segalanya):

```
( ) Operasional / Kantor      biaya perusahaan; langsung menjadi beban
( ) Proyek                    biaya pembangunan; menempel pada proyek/unit
```

Langkah 2 — bidang yang muncul, **masing-masing dengan alasan domainnya**:

| Bidang | Operasional | Proyek | Alasan domain |
|---|---|---|---|
| Jenis pengeluaran (master) | wajib | — | menentukan akun beban (G-A) |
| Kategori biaya (tanah/hard/soft/pendanaan) | — | wajib | menentukan akun Persediaan; matriks tier×kategori |
| Proyek | opsional (cost center) | wajib | `validate` mensyaratkan proyek untuk direct/shared |
| Unit | dilarang | opsional | unit terisi ⇒ tier `direct` ⇒ masuk HPP unit |
| Fase | dilarang tanpa proyek | opsional | `ErrPhaseRequiresProject` |
| Item RAB | dilarang tanpa proyek | opsional | drill-down realisasi per item (T-12) |
| Nominal | wajib | wajib | rupiah bulat (Invariant #2) |
| Tanggal | wajib | wajib | menentukan periode & penjagaan tutup buku |
| Cara bayar (kas/bank vs hutang) | wajib | wajib | menentukan akun kredit |
| Rekening kas/bank | wajib bila kas | wajib bila kas | **dropdown dari `/ledger/accounts/cash-bank`**, bukan teks (G-C) |
| Vendor/pihak | wajib | wajib | sudah wajib hari ini di form |
| Keterangan | wajib | wajib | deskripsi jurnal |

Bidang yang **tidak** ditambahkan meski terlihat berguna: nomor faktur vendor, kategori
pajak, pusat biaya bebas, lampiran. Tidak ada satupun yang punya konsumen di domain hari
ini; menambahkannya berarti kolom yang diisi tapi tidak pernah dibaca.

Langkah 3 — **Tinjau**: memanggil `/cost-entries/preview` dan menampilkan jurnal persis
seperti yang akan diposting (pola `BackendJournalLine`, `CostEntryForm.tsx:333-349`).
Frontend tidak pernah menghitung kode akun.

Langkah 4 — **Simpan & Posting** → jurnal terposting + nomor BKK ditampilkan.

### 8.3 Daftar

Satu tabel pengeluaran tingkat-tenant dengan filter (rentang tanggal, jenis, proyek,
rekening) dan kolom **Status** yang **diturunkan dari jurnal**: `Draft` / `Terposting` /
`Dibalik` — menutup G-E tanpa kolom status baru.

### 8.4 Layar master

`Pengaturan → Jenis Pengeluaran`: CRUD + kolom akun beban + riwayat perubahan (dari
`master_data_changes`), meniru layar W-1 yang sudah ada.

---

## 9. Integrasi dengan Project Cost

**Tidak ada integrasi — ini objek yang sama.** Scope "Proyek" pada layar Pengeluaran
memanggil `cost.Service.CreateCostEntry` yang persis sama dengan layar Biaya proyek, dengan
payload yang sama, validasi yang sama, dan jurnal yang sama.

Yang menjamin tidak ada mesin kedua:

1. Hanya `cost.Service` yang boleh membuat `cost_entries`, dan hanya ia yang tahu matriks
   tier×kategori, aturan Product Catalog untuk unit, dan tautan RAB.
2. Layar Pengeluaran tidak boleh memanggil `POST /ledger/journals` untuk apa pun yang
   berbau biaya. Kalau sebuah kebutuhan tidak bisa diungkapkan lewat `CreateCostEntry`,
   itu sinyal untuk memperluas `cost`, bukan untuk menulis jurnal sendiri.
3. Tidak ada penulisan ganda: satu aksi user = satu `CreateCostEntry` = satu jurnal.

---

## 10. Integrasi dengan General Ledger

Satu-satunya penulis tetap `PostingService`. Yang otomatis ikut benar karenanya:

- balanced, rupiah bulat, minimal 2 baris, akun harus ada (`validateLines`);
- periode tertutup ditolak;
- append-only; koreksi hanya lewat pembalik;
- jurnal tampil di Buku Besar dan Neraca Saldo tanpa pekerjaan tambahan;
- baris ber-tag `project_id` otomatis terhitung sebagai realisasi proyek (T-11) — **bila**
  akunnya termasuk kode yang dibaca (§17 R-C).

---

## 11. Integrasi Kas/Bank

- Rekening dipilih dari `GET /ledger/accounts/cash-bank`, yang bersumber pada
  `accounts.category ∈ {cash, bank}` — otoritas yang sama dengan `cashMovement` dan
  `ValidatePaymentAccount`. Satu definisi, tiga pemakai.
- `ValidatePaymentAccount` dipanggil di `cost.validate` (memperbaiki T-5).
- Arah kas menentukan dokumen secara otomatis: kas berkurang → **BKK**
  (`DefaultCashSpec`, `document_issuer.go:216-219`).
- `payment_method = "payable"` tidak menyentuh kas → tidak ada dokumen, dan itu benar.

---

## 12. Implikasi dokumen/kwitansi

**Tidak ada jenis dokumen baru.** `TypeCashOut` (BKK, "Bukti Kas Keluar") sudah terdaftar
di seed (`document/seed.go:27,41`) dan sudah otomatis terbit lewat
`PostJournal(..., cashOut=true)` di jalur biaya (`cost/service.go:433-436`).

Yang **tidak** boleh dipilih otomatis dan karenanya tidak relevan untuk pengeluaran
perusahaan: **BTP** (uang milik pihak ketiga) dan **RFC** (uang milik customer). Alasannya
sudah dipatok di `document_issuer.go:194-198` — kepemilikan ekonomis uang tidak bisa
disimpulkan dari arah debit/kredit. Belanja kantor selalu uang perusahaan → BKK.

INV-DOC-1 terjaga tanpa tambahan: `enforceDocument` memeriksa **keadaan akhir** jurnal, jadi
jalur baru yang lupa menyatakan dokumen akan tertahan, bukan lolos.

---

## 13. Implikasi pembalikan (reversal)

Aturan W-9/D-W8-7 dihormati sepenuhnya:

1. Koreksi pengeluaran = **jurnal pembalik**, tidak pernah edit/hapus.
2. Pembalik harus tunduk pada periode tertutup — sudah dijaga di `reverseWithin`
   (`posting_service.go:356`).
3. Pembalik jurnal kas menerbitkan **JR** yang menunjuk dokumen aslinya
   (`document_issuer.go:328-334`); nomor BKK asli tidak pernah diubah.

Pertanyaan yang harus dijawab W-10: apakah pengeluaran perlu penjaga kepemilikan R-1?

**INFERENCE (TINGGI): belum perlu, dan memasangnya sekarang justru berbahaya.**
`cost_entries` bukan sub-ledger yang mengklaim saldo — seluruh angka diturunkan dari jurnal
(T-11), jadi membalik jurnal biaya dari pintu umum **tidak** membuat GL dan laporan berbeda.
Yang rusak hanya tampilan: daftar biaya tidak menunjukkan bahwa jurnalnya sudah dibalik.
Memasang penjaga kepemilikan sekarang akan **memblokir satu-satunya jalur koreksi yang ada**
(T-10), karena `cost` tidak punya endpoint pembatalan sendiri.

Urutan yang benar: **tampilkan status dulu** (§8.3, diturunkan dari `posted_at`/`reverses_id`),
lalu — bila klien menginginkan koreksi ber-alasan dan ber-audit — buat
`POST /cost-entries/{id}/reverse` yang memanggil `PostingService.Reverse` di dalam satu
transaksi, **baru** daftarkan `JournalOwnershipReader` untuk `cost` supaya pintu umum
tertutup. Dua langkah itu harus berpasangan; melakukan yang kedua tanpa yang pertama
membuat biaya tidak bisa dikoreksi sama sekali.

---

## 14. Implikasi tutup buku (period closing)

Tidak ada yang baru diperlukan:

- pembuatan jurnal bertanggal periode tertutup → ditolak (`posting_service.go:132-140`);
- pembalik bertanggal periode tertutup → ditolak, dipetakan ke **409** dengan jalan keluarnya
  (`ledger/handler.go:699-703`);
- tutup buku tahunan memindahkan saldo beban ke 3-3000 lewat `ClosingService`; akun beban
  baru dari master ikut otomatis karena penutupan bekerja atas tipe akun, bukan daftar kode.

Yang perlu ditegaskan di UI: pesan error periode tertutup harus muncul sebagai penjelasan,
bukan "gagal menyimpan".

## 15. Isolasi tenant

Pola yang berlaku (T-13) adalah **predikat eksplisit di setiap query**, bukan global scope.
W-10 mengikuti pola yang ada:

- `expense_types` punya `tenant_id NOT NULL` + index + unique `(tenant_id, code)`;
- resolver master menyaring `tenant_id` (pola `resolveChargeTypePolicyDB`);
- validasi akun beban menyaring `tenant_id` pada tabel `accounts`;
- `expense_type_id` yang menunjuk master milik tenant lain harus gagal resolve
  (fail-closed), bukan diam-diam memakai akun default.

Dibuktikan integration test terhadap MySQL nyata, bukan mock (§16).

---

## 16. Matriks test

**Unit (domain/service, tanpa DB)**

| # | Yang dibuktikan |
|---|---|
| U-1 | resolver jenis pengeluaran: kode tak dikenal / nonaktif / akun kosong → error, tanpa jurnal |
| U-2 | `expense_type_id` kosong ⇒ perilaku lama persis (`marketing→5-3000`, `other→5-4000`) |
| U-3 | `expense_type_id` terisi pada tier ≠ overhead → ditolak |
| U-4 | akun kredit `payable` diambil dari registry peran, bukan literal |
| U-5 | `bank_account_code` bukan kategori cash/bank → ditolak sebelum jurnal |
| U-6 | nominal pecahan / nol / negatif → ditolak |
| U-7 | preview == jurnal yang benar-benar dibuat (baris, akun, nominal identik) |

**Integration (MySQL nyata, jalur produksi)**

| # | Yang dibuktikan |
|---|---|
| I-1 | biaya operasional kas: 1 jurnal terposting, Σdebit==Σkredit, tepat 1 dokumen BKK |
| I-2 | biaya operasional payable: 1 jurnal, **nol** dokumen (tidak menyentuh kas) |
| I-3 | akun beban dari master benar-benar terdebit (5-4300, bukan 5-4000) |
| I-4 | tenant A tidak bisa memakai `expense_type` milik tenant B (nol hasil, transaksi batal) |
| I-5 | posting ke periode tertutup ditolak; tidak ada jurnal maupun dokumen tertinggal |
| I-6 | pembalik: GL kembali nol, JR terbit menunjuk BKK asli, BKK asli tidak berubah |
| I-7 | biaya operasional **tidak** mengubah `AccumulatedByProject` (bukan kapitalisasi) |
| I-8 | biaya proyek lewat layar Pengeluaran menghasilkan jurnal identik dengan layar Biaya proyek |
| I-9 | atomicity: kegagalan di penulisan kedua tidak meninggalkan jurnal yatim |
| I-10 | mengubah akun sebuah jenis pengeluaran tercatat di `master_data_changes` |

**Regresi wajib (membuktikan tidak ada yang rusak)**

| # | Yang dibuktikan |
|---|---|
| R-1 | seluruh suite W-6/W-7/W-8 tetap hijau |
| R-2 | realisasi RAB per item tidak berubah untuk data lama |
| R-3 | dashboard `actual_cost` (kode Persediaan) tidak berubah |

---

## 17. Risiko duplikasi akuntansi

| Risiko | Mengapa tertutup |
|---|---|
| **R-A — dua jurnal untuk satu transaksi** | hanya `CreateCostEntry` yang menulis; layar Pengeluaran tidak punya jalur ke `POST /ledger/journals` |
| **R-B — dua sumber kebenaran angka** | total selalu dari `ActualCostByCode` atas jurnal terposting; `cost_entries` tetap dokumen sumber (T-11) |
| **R-C — beban operasional ber-proyek tampak/tidak tampak di realisasi RAB** | **RISIKO NYATA.** `GetRealisasiByProject` (`budget/repository.go:297-320`) hanya membaca kode dari `AllCostCategories` + `ExpenseCostCategories` — yaitu 1-3xxx, 5-3000, 5-4000. Beban di 5-4300 yang di-tag proyek akan tampil di Buku Besar per proyek **tetapi tidak** di realisasi RAB. Ini keputusan bisnis (BD-2), bukan bug — tapi harus diputuskan sadar, bukan ditemukan belakangan |
| **R-D — user memasukkan transaksi dua kali** | satu pintu masuk; layar Biaya proyek menjadi tampilan ber-konteks dari data yang sama, bukan formulir kedua yang berdiri sendiri |
| **R-E — jenis pengeluaran dipetakan ke akun non-beban** | validasi mapping saat master disimpan (tipe akun harus `expense`, aktif, milik tenant), fail-closed |
| **R-F — pengeluaran kapitalisasi disamarkan sebagai beban** | matriks tier×kategori tidak berubah; scope Proyek tetap memakai kategori kapitalisasi dan tetap tidak bisa memilih jenis pengeluaran operasional |

---

## 18. Rekomendasi: apakah ini menjadi W-10?

**Ya — dengan cakupan yang jauh lebih kecil daripada yang tersirat dalam permintaan.**

Ini bukan "fitur transaksi baru". Ini tiga hal:

1. **Master jenis pengeluaran** (data-driven, meniru W-1 satu banding satu) — menutup G-A.
2. **Layar Keuangan → Transaksi → Pengeluaran** di atas endpoint yang sudah ada — menutup G-B.
3. **Pengetatan jalur biaya yang sudah ada**: validasi rekening COA-driven (G-C), akun
   hutang dari registry (G-D), status diturunkan dari jurnal (G-E), create+post atomik.

Cakupan yang **dikeluarkan** dari W-10, dengan alasan:

| Dikeluarkan | Alasan |
|---|---|
| Jalur pelunasan Hutang Usaha | domain baru (AP), bergantung BD-3; jangan disisipkan |
| Master vendor | keputusan bisnis (BD-6), tidak dibutuhkan akuntansi |
| Lampiran bukti | belum terbukti perlu (BD-7); tidak ada mekanisme di sistem |
| PPN Masukan | menyentuh domain pajak; butuh keputusan tersendiri (BD-8) |
| Approval pengeluaran | mesin sudah ada dan opt-in; pasang hanya bila diminta (BD-5) |
| Endpoint pembatalan biaya + penjaga R-1 untuk `cost` | harus berpasangan, dan pasangannya butuh keputusan siapa yang boleh membalik (BD-4) |
| Perbaikan `parseFloat` di form jurnal manual (T-8) | temuan terpisah, bukan bagian pengeluaran |

Temuan T-7 (tombol "Simpan & Posting" gagal untuk jurnal kas) **direkomendasikan masuk
W-10** meski di luar layar pengeluaran: ia adalah alasan langsung mengapa klien merasa
sistemnya buntu, dan perbaikannya kecil (kirim pilihan dokumen dari layar buat jurnal).

---

## 19. Apakah W-9 (reversal sovereignty) wajib lebih dulu?

**Tidak wajib. W-10 boleh berjalan paralel — dengan tiga batasan yang eksplisit.**

Keadaan aktual, bukan keadaan menurut dokumen:

- **R-2 SUDAH SELESAI.** `reverseWithin` memeriksa periode tertutup atas `reverseDate`
  (`posting_service.go:356`), dan handler memetakannya ke 409 dengan jalan keluar
  (`handler.go:699-703`). Temuan lama "tidak pernah memanggil IsPeriodClosed" tidak lagi
  berlaku.
- **R-1 SEAM-nya SUDAH ADA dan fail-closed** (`handler.go:611-650`), tetapi baru
  **satu** domain yang mendaftar: `sale` (`cmd/api/main.go:162`). `legacyar`, `charge`,
  `notary`, `commission`, `billing` masih bisa dibalik dari pintu umum.
- **G-1 (test GL ↔ sub-ledger)** adalah tentang piutang. Pengeluaran tidak punya sub-ledger
  yang mengklaim saldo, jadi tidak menambah permukaan risiko G-1.

Alasan W-10 tidak membuka inkonsistensi baru: fitur ini **tidak menciptakan klaim
sub-ledger baru**. Setiap angka yang ditampilkannya sudah diturunkan dari jurnal terposting
(T-11). Membalik jurnal biaya dari pintu umum menggerakkan GL **dan** seluruh laporan
turunannya secara serempak; tidak ada tabel kedua yang tertinggal menyatakan hal berbeda.
Ini berbeda tajam dari kasus penjualan/piutang yang melahirkan R-1.

**Batasan yang harus dipegang:**

1. **W-10 tidak boleh menambahkan kolom status/saldo yang disimpan** di `cost_entries`
   (mis. `is_reversed`, `outstanding`). Begitu ada angka tersimpan yang bisa menyimpang dari
   jurnal, pengeluaran berubah menjadi sub-ledger dan W-9 langsung menjadi prasyarat.
2. **Status di UI wajib diturunkan dari jurnal** (`posted_at`, `reverses_id`), bukan ditulis
   saat aksi.
3. **Endpoint pembatalan biaya + pendaftaran `JournalOwnershipReader` untuk `cost`** —
   bila kelak dibuat — masuk lingkup W-9, bukan W-10, dan keduanya harus dikerjakan
   berpasangan (§13).

Bila klien memilih BD-3 (jalur pelunasan hutang usaha) atau BD-4 (pembatalan biaya
ber-alasan), **batasan di atas gugur** dan W-9 harus didahulukan, karena keduanya
melahirkan klaim sub-ledger sungguhan.

---

## 20. KEPUTUSAN BISNIS yang perlu dijawab klien

| # | Pertanyaan | Mengapa penting | Rekomendasi |
|---|---|---|---|
| **BD-1** | Jenis pengeluaran dikelola admin sebagai master (Listrik, Internet, ATK, …) yang menunjuk akun beban — atau admin langsung memilih akun COA di formulir? | Menentukan apakah orang yang mencatat perlu paham akuntansi | **Master** (pola W-1): admin memilih "Internet", bukan "5-4300" |
| **BD-2** | Beban operasional yang di-tag ke sebuah proyek: apakah harus ikut terhitung di **realisasi RAB** proyek itu, atau cukup tampil di Buku Besar per proyek? | R-C: hari ini hanya 1-3xxx/5-3000/5-4000 yang terhitung sebagai realisasi | Cukup di GL per proyek; jangan ubah arti "realisasi RAB" tanpa keputusan sadar |
| **BD-3** | Apakah pengeluaran "belum dibayar" (hutang usaha) dibutuhkan sekarang? | Bila ya, sistem butuh jalur pelunasan hutang yang **hari ini tidak ada** (T-9) | Tunda; catat pengeluaran saat dibayar. Bila dibutuhkan, jadikan increment tersendiri |
| **BD-4** | Siapa boleh membatalkan pengeluaran yang sudah diposting, dan apakah alasan wajib diisi? | Menentukan apakah `cost` butuh endpoint pembatalan + penjaga R-1 (§13, §19) | Tahap 1: cukup tampilkan status; pembatalan lewat jurnal pembalik seperti sekarang |
| **BD-5** | Perlu persetujuan (approval) untuk pengeluaran di atas nominal tertentu? | Mesin approval sudah ada dan opt-in; memasangnya belakangan lebih mahal | Tentukan sekarang, pasang belakangan bila jawabannya "ya" |
| **BD-6** | Vendor: tetap teks bebas, atau master vendor? | Teks bebas berarti "PLN", "P.L.N.", "pln" menjadi tiga pihak berbeda di laporan | Teks bebas dulu; master vendor bila klien butuh laporan per vendor |
| **BD-7** | Perlu lampiran foto nota/faktur? | Tidak ada mekanisme attachment di seluruh sistem — ini pekerjaan tersendiri | Tidak di W-10 |
| **BD-8** | Belanja operasional ber-PPN: perlu mencatat PPN Masukan (1-5100)? | Mengubah jurnal menjadi 3 baris dan menyentuh domain pajak | Tidak di W-10 kecuali klien PKP dan memang mengkreditkan PPN |
| **BD-9** | Beban bulanan tetap (sewa kantor, internet): pakai template **jurnal berulang** yang sudah ada, atau harus diinput manual tiap bulan di layar Pengeluaran? | Fitur berulang sudah jalan dan sudah ber-BKK otomatis | Pakai yang sudah ada; tautkan dari layar Pengeluaran |
| **BD-10** | Daftar jenis pengeluaran awal yang ingin dipakai klien | Menentukan isi seed master | Minta daftar riilnya; jangan dikarang |

---

## Catatan penutup

Pekerjaan ini **tidak** mengubah satu baris kode, migrasi, seed, maupun data. Tidak ada
transaksi dummy yang dibuat. Tidak ada yang disentuh di luar repositori esaProperti.

Kesimpulan yang paling ringkas: **klien tidak sedang meminta fitur akuntansi baru.** Mesinnya
sudah ada dan benar. Yang hilang adalah (a) daftar jenis pengeluaran yang bisa dikelola
sendiri, dan (b) pintu masuk yang tidak mengharuskan orang membuka sebuah proyek terlebih
dahulu — ditambah beberapa pengetatan yang seharusnya sudah ada sejak awal.

Menunggu approval sebelum melanjutkan ke implementasi.
