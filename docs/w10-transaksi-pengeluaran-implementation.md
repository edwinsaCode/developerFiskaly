# W-10 — Transaksi Pengeluaran (Implementasi)

Status: **SHIPPED** (gate otomatis hijau; verifikasi browser sebagian — lihat §10)
Tanggal: 2026-08-13
Dokumen desain: `docs/w10-transaksi-pengeluaran-audit-design.md`

Dokumen ini mencatat apa yang benar-benar dibangun, bukan apa yang direncanakan.
Di mana implementasi berbeda dari desain, perbedaannya ditulis apa adanya.

---

## 1. Arsitektur final

Satu pintu masuk baru di UI, **nol** mesin baru di belakangnya.

```
UI  Keuangan → Transaksi → Pengeluaran
        │
        ▼
POST /api/v1/expenses            (internal/cost/expense_handler.go — tipis)
        │  menerjemahkan "scope" user → CreateCostEntryRequest
        ▼
cost.Service.CreateAndPostCostEntry
        │  satu transaksi DB
        ├─ resolve akun beban  ── master expense_types (operasional)
        │                      └─ taksonomi CostCategory (proyek)
        ├─ validasi akun kas/bank ── ledger.ValidatePaymentAccount
        ├─ INSERT cost_entries
        ├─ ledger.PostingService.PostDraft(DocumentSpec{DocCashOut})
        └─ terbit dokumen BKK (INV-DOC-1) dalam transaksi yang sama
```

Yang **tidak** dibuat, sesuai §1 instruksi:

| Tidak dibuat | Yang dipakai sebagai gantinya |
|---|---|
| tabel `expense_transactions` | `cost_entries` (kolom baru `expense_type_id`) |
| tabel `office_expenses` / `project_expenses` | sda |
| mesin ledger kedua | `ledger.PostingService` |
| mesin posting kedua | `PostDraft` yang sudah ada |
| document type baru | `BKK` (`ledger.DocCashOut`) yang sudah ada |
| reader realisasi RAB baru | `budget.GetRealisasiPerItem` + `GetRealisasiByProject` |
| endpoint reversal khusus | tidak ada — `PostingService.Reverse` (§12) |

Satu transaksi pengeluaran = satu baris `cost_entries` + satu `journal_entries` +
satu `documents`. Dibuktikan oleh `TestW10_20_SatuTransaksiSatuEfekAkuntansi`.

---

## 2. Alur akuntansi

### 2.1 Operasional (scope = `operasional`)

```
Dr  <akun beban dari master jenis pengeluaran>   xxx
    Cr  <akun kas/bank pilihan user>                 xxx
```

`cost_tier` dipaksa `overhead`, `category` diisi `other` **hanya** agar matriks
tier × kategori tetap sah. Akun debit yang sesungguhnya datang dari
`expense_types.expense_account_code`, bukan dari kategori itu — kategori tidak
lagi menentukan akun pada jalur ini.

`project_id` boleh `NULL` (murni korporat) atau terisi (tag pelaporan, §3).

### 2.2 Proyek (scope = `proyek`)

Semantik lama `CostEntry` tidak berubah sedikit pun:

```
tier direct  → Dr 1-3xxx (persediaan, menempel unit) / Cr kas-bank
tier shared  → Dr 1-3xxx (pool proyek)              / Cr kas-bank
tier overhead→ Dr 5-3000 | 5-4000                   / Cr kas-bank
```

Pipeline HPP dan alokasi tidak disentuh.

### 2.3 Bukti verifikasi langsung terhadap server dev (tenant 9900246)

```
preview gaji 5-4100 dari 1-1200
  → Dr 5-4100 Beban Gaji & Tunjangan 1.500.000 / Cr 1-1200 Kas — Petty Cash 1.500.000
preview biaya proyek hard/shared dari 1-1200
  → Dr 1-3100 Persediaan Real Estat — Hard Cost / Cr 1-1200 Kas — Petty Cash
```

---

## 3. Sumber kebenaran

| Pertanyaan | Sumber kanonik | Bukan |
|---|---|---|
| Berapa biaya aktual proyek? | `cost_entries` + jurnal terposting | tabel baru apa pun |
| Berapa realisasi RAB per item? | `budget.GetRealisasiPerItem` (`cost_entries.budget_item_id`) | tag proyek |
| Berapa realisasi RAB per kategori? | `budget.GetRealisasiByProject` (ledger, per kode akun) | sda |
| Status sebuah pengeluaran | diturunkan dari `journal_entries.status` saat dibaca | kolom status tersimpan |
| Nomor bukti kas keluar | `documents` (engine penomoran W-2) | string di kode |
| Akun beban sebuah jenis | `expense_types.expense_account_code` | enum/hardcode |
| Akun kas/bank valid | `accounts.category` (`cash`/`bank`) | daftar kode di handler |

**Tidak ada satu pun nilai akuntansi yang disimpan ganda** (§8). Tidak ada
saldo tersimpan, tidak ada `paid_amount`, tidak ada flag `posted` manual.

---

## 4. Pemetaan jenis pengeluaran (§4)

Migrasi `000072_expense_types` membuat master per tenant:

| code | nama | akun |
|---|---|---|
| `gaji` | Gaji & Tunjangan | 5-4100 |
| `sewa-kantor` | Sewa Kantor | 5-4200 |
| `utilitas` | Listrik, Air & Internet | 5-4300 |
| `transport` | Transport & Perjalanan Dinas | 5-4400 |
| `penyusutan` | Penyusutan | 5-4500 |
| `atk` | ATK & Perlengkapan Kantor | 5-4000 |
| `software` | Software & Langganan | 5-4000 |

Sifatnya (mengikuti pola W-1 `realization_charge_types`):

- **Tenant-scoped**, unik per `(tenant_id, code)`.
- **Aktif/nonaktif**, bukan hapus. Jenis nonaktif ditolak saat transaksi
  (`ErrExpenseTypeInactive`) — histori yang sudah terposting tidak tersentuh.
- **Mapping akun divalidasi sebelum disimpan**: akun harus ada di COA tenant,
  aktif, dan **bertipe beban** (`ErrExpenseAccountInvalid`). Akun bank, pendapatan,
  atau kewajiban ditolak di master, bukan baru ketahuan saat menjurnal.
- **Fail-closed**: jenis tak dikenal → `ErrExpenseTypeUnknown`, tidak pernah
  jatuh ke akun default (`TestW10_10b`).
- **Setiap perubahan mapping tercatat** di `master_data_changes` (kode, field,
  nilai lama, nilai baru, aktor).
- Migrasi **tidak membuat akun COA baru**. Menambah akun ke chart of accounts
  adalah keputusan pemilik, bukan efek samping sebuah fitur.

Sebelum W-10, akun beban ditentukan enum tertutup dua nilai (5-3000 marketing,
5-4000 lainnya) — 5-4100…5-4500 yang sudah ada di COA bawaan tidak bisa dicapai
dari jalur pencatatan mana pun. Itu yang diperbaiki.

---

## 5. Semantik proyek vs RAB (BD-2 + INV-EXP-2)

**Aturan klien (BD-2):** tag proyek ≠ realisasi RAB.

Masalah nyatanya: `GetRealisasiByProject` membaca **ledger** berdasarkan kode
akun taksonomi (1-3000/1-3100/1-3200/1-3300/5-3000/5-4000). Jadi jurnal apa pun
yang ber-`project_id` dan menyentuh akun-akun itu **otomatis** terbaca sebagai
realisasi RAB per kategori. Mengubah reader tersebut akan mengubah perilaku
W-5/W-6 — tidak boleh.

**Penyelesaian — INV-EXP-2 (fail-closed di service):**

> Pengeluaran operasional yang akun bebannya termasuk himpunan akun taksonomi
> `CostCategory` **tidak boleh** di-tag ke proyek.

Himpunan itu diturunkan dari `domain.AllCostCategories` + `ExpenseCostCategories`
— **tidak pernah** ditulis sebagai daftar kode di kode program, sesuai §3
("mapping ditentukan melalui master/domain rule, bukan hardcode di frontend").

Akibat konkretnya pada master bawaan:

| jenis | akun | boleh tag proyek |
|---|---|---|
| gaji, sewa-kantor, utilitas, transport, penyusutan | 5-41xx…5-45xx | ✅ ya |
| atk, software | 5-4000 (akun taksonomi) | ❌ ditolak |

Backend membocorkan keputusan ini sebagai **data**, bukan aturan yang ditebak
frontend: `GET /expense-types` mengembalikan `allows_project_tag` per baris.
UI hanya mematikan dropdown tag proyek dan menjelaskan alasannya. Verifikasi
langsung ke server dev:

```
GET /api/v1/expense-types
  {"code":"atk", "expense_account_code":"5-4000", "allows_project_tag":false}
  {"code":"gaji","expense_account_code":"5-4100", "allows_project_tag":true }
```

Sisanya:

- Tag proyek yang **diizinkan** (mis. gaji) masuk GL dan pelaporan proyek,
  tetapi **tidak** menjadi realisasi RAB — dibuktikan `TestW10_6c`:
  `GetRealisasiPerItem` kosong dan `GetRealisasiByProject` tidak memuat
  `CostCategoryOther`.
- Realisasi RAB **hanya** lahir dari `budget_item_id` (`TestW10_5_6`).
- Pengeluaran operasional dilarang menautkan `budget_item_id`
  (`ErrExpenseTypeWithBudgetItem`) — kalau memang bagian anggaran, ia biaya
  proyek, bukan beban kantor.

---

## 6. Alur dokumen (§6)

- Satu arus kas keluar = satu dokumen **BKK** bernomor, terbit **di dalam**
  transaksi yang sama (INV-DOC-1 dari W-3). Tidak ada document type baru.
- Fail-closed: master `document_types` tenant hilang → **seluruh transaksi
  batal**, bukan jurnal tanpa bukti (`TestW10_14`).
- Nomor bukti ikut pada respons `POST /expenses` dan pada daftar, sehingga
  layar sukses bisa menunjukkan buktinya tanpa membuka modul lain.
- **Tidak ada backfill** dokumen historis.

---

## 7. Atomicity (§7)

Empat keadaan terlarang, dua di antaranya diuji dari sisi berlawanan:

| Keadaan terlarang | Bukti |
|---|---|
| CostEntry ok / jurnal gagal | `TestW10_15` — gagal simpan entry → jurnal, baris jurnal, dokumen ikut rollback |
| Jurnal ok / CostEntry gagal | sda |
| BKK ok / jurnal rollback | `TestW10_14` — dokumen gagal terbit → seluruh transaksi batal |
| Jurnal terposting / dokumen gagal | sda |

Jurnal tidak seimbang secara struktural mustahil: `PostingService.validateLines`
menolak <2 baris, nominal negatif, pecahan sen, baris debit-dan-kredit sekaligus,
dan Σdebit ≠ Σkredit (`TestW10_13`).

---

## 8. Perilaku pembalikan (§12)

- Tidak ada sub-ledger baru, tidak ada endpoint reversal khusus W-10.
- `cost_entries` yang sudah terposting **immutable** — posting ulang ditolak
  (`TestW10_17`), jumlah baris jurnal dan dokumen tidak bertambah.
- Koreksi memakai `PostingService.Reverse` yang sudah ada. Karena status tidak
  pernah disimpan, membalik jurnal **dari luar modul cost** langsung membuat
  `GET /expenses/{id}` melaporkan `reversed` dan biaya aktual proyek kembali
  nol (`TestW10_18`). Ini bukti §8 yang paling keras: tidak ada yang bisa drift
  karena tidak ada yang disimpan.
- R-1/R-2/R-4 dari W-9 tetap pekerjaan terpisah.

---

## 9. Bukti pengujian (§17 + §19)

### 9.1 Gate otomatis

| Perintah | Hasil |
|---|---|
| `go build ./...` | OK |
| `go vet ./...` | OK |
| `go vet -tags integration ./...` | OK |
| `go test ./...` | tanpa kegagalan |
| `go test -tags integration ./internal/cost/ -count=1` | `ok esaproperti/internal/cost 10.750s` (24 test) |
| `go test -tags integration -p 1 ./... -count=1` | 25 paket **ok** (regresi penuh) |
| `npx tsc --noEmit --incremental false` | bersih |

> Catatan penting: regresi penuh **harus** dijalankan dengan `-p 1`. Lihat
> temuan F-4 di §11 — ini bukan regresi W-10.

### 9.2 Peta 20 tes wajib → test

Berkas: `backend/internal/cost/w10_expense_integration_test.go`

| # | Yang diminta | Test |
|---|---|---|
| 1 | Office expense → GL benar | `TestW10_1_PengeluaranKantor_JurnalBenar` |
| 2 | Office expense → BKK terbit | `TestW10_2_PengeluaranKantor_MenerbitkanBKK` |
| 3 | Project expense → GL benar | `TestW10_3_4_BiayaProyek_JurnalDanBiayaProyek` |
| 4 | Project expense → tampak di biaya proyek | sda (`ListByProject`, `AccumulatedByProject`) |
| 5 | Project + RAB → realisasi RAB benar | `TestW10_5_6_RealisasiRAB_HanyaDariTautanItem` |
| 6 | Project tanpa RAB → bukan realisasi | sda + `TestW10_6c` |
| 7 | Unit tagging valid | `TestW10_7_UnitTaggingValid` |
| 8 | Unit dari proyek lain ditolak | `TestW10_8_UnitDariProyekLain_Ditolak` |
| 9 | Jenis nonaktif ditolak | `TestW10_9_JenisNonaktif_Ditolak` |
| 10 | Jenis tenant A tak bisa dipakai tenant B | `TestW10_10_JenisTenantLain_Ditolak` (dua arah) |
| 11 | Akun non-bank ditolak (T-5) | `TestW10_11_AkunPembayaranBukanKasBank_Ditolak` |
| 12 | Akun bank nonaktif ditolak | `TestW10_12_AkunBankNonaktif_Ditolak` |
| 13 | Jurnal tak seimbang mustahil | `TestW10_13_JurnalTidakSeimbang_Mustahil` |
| 14 | Gagal terbit dokumen → rollback | `TestW10_14_GagalTerbitDokumen_RollbackPenuh` |
| 15 | Gagal jurnal → rollback | `TestW10_15_GagalSimpanEntry_RollbackJurnal` |
| 16 | Periode tertutup ditolak | `TestW10_16_PeriodeTertutup_Ditolak` |
| 17 | Biaya terposting immutable | `TestW10_17_BiayaTerposting_Immutable` |
| 18 | Pembalikan tanpa drift status | `TestW10_18_Pembalikan_StatusTurunanIkut` |
| 19 | Submit bersamaan aman | `TestW10_19_SubmitBersamaan_TidakTumpangTindih` |
| 20 | Tidak ada entri akuntansi ganda | `TestW10_20_SatuTransaksiSatuEfekAkuntansi` |

Tambahan di luar daftar wajib: `TestW10_6b` (INV-EXP-2 ditegakkan),
`TestW10_10b` (jenis tak dikenal tidak jatuh ke akun default),
`TestW10_12b` (master menolak akun non-beban + perubahan mapping teraudit),
`TestW10_BatasJalurOperasional` (4 subtest batas jalur operasional).

Regresi W-5 / W-7 / W-8 / KPR-KWD / Booking-KWB tercakup oleh suite paket
masing-masing yang ikut hijau pada `-p 1 ./...`.

### 9.3 Verifikasi terhadap server dev (read-only, tanpa menulis data)

Endpoint `POST /expenses/preview` murni hitung — tidak menulis apa pun. Semua
hasil di bawah diambil dari backend yang berjalan (127.0.0.1:8085, tenant 9900246):

| Kasus | Hasil |
|---|---|
| gaji (5-4100) dari 1-1200 | 200 — Dr 5-4100 / Cr 1-1200 |
| biaya proyek hard/shared dari 1-1200 | 200 — Dr 1-3100 / Cr 1-1200 |
| akun bayar 4-1000 (pendapatan) | **400** `akun pembayaran harus akun kas/bank yang aktif` |
| akun bayar 5-4100 (beban) | **400** sda |
| atk (5-4000) + tag proyek | **400** INV-EXP-2 |
| gaji (5-4100) + tag proyek | 200 (diizinkan) |
| `payment_method: "payable"` | **400** `hutang usaha belum didukung` |
| jenis pengeluaran pada scope proyek | **400** `hanya untuk biaya operasional` |

---

## 10. Verifikasi browser (§18) — **tuntas**

Dijalankan di Chrome pada sesi login milik pemilik (tenant 9900246 "NATA ALAM",
`http://localhost:3005`), tanpa membuat satu pun transaksi baru.

| # | Cek | Hasil |
|---|---|---|
| 1 | `/accounting/pengeluaran` termuat | LULUS — judul "Transaksi Pengeluaran", entri sidebar **Pengeluaran** aktif |
| 2 | Empty state | LULUS (kode) — `ExpenseList.tsx:43-54` informatif + arahan ke formulir. Tidak bisa dilihat di tenant ini karena sudah ada 5 baris |
| 3 | Selector jenis pengeluaran | LULUS — 7 jenis termuat dari `GET /expense-types` |
| 4 | Scope kantor | LULUS — kartu "Operasional / Kantor" aktif, memunculkan Jenis Pengeluaran |
| 5 | Scope proyek | LULUS — kartu "Biaya Proyek" menukar field ke Proyek + Kategori Biaya, jenis pengeluaran hilang |
| 6 | Selector proyek | LULUS — UAT Residence (1543), Nata Residence (1739) |
| 7 | Selector unit | LULUS — A-1, B-1, C-1, B-01 + opsi "— Biaya bersama proyek —" |
| 8 | Selector item RAB | LULUS — "RAB aktif: RAB UAT Residence v1", 3 item + "— Tanpa tautan RAB —" |
| 9 | Jenis `allows_project_tag=false` (ATK) | LULUS — tag proyek **dinonaktifkan** + alasan akun 5-4000 termasuk taksonomi biaya proyek |
| 10 | Jenis `allows_project_tag=true` (Gaji) | LULUS — tag aktif + teks BD-2 ("tidak terhitung sebagai realisasi RAB") dan catatan "akan didebit ke akun 5-4100" |
| 11 | Selector kas/bank | LULUS — 5 akun dari COA (`1-1300`, `1-1400`, `1-1500`, `1-1100`, `1-1200`), default "Bank — BCA" |
| 12 | Error state akun tidak valid | LULUS (by construction) — UI hanya menawarkan akun kas/bank; penolakan dibuktikan di API: `4-1000` dan `5-4100` → 400 (§9.3) |
| 13 | Validasi nominal | **CACAT (must-fix, UI)** — lihat F-5 |
| 14 | Validasi field wajib | LULUS — Penerima & Deskripsi memunculkan pesan merah dan submit berhenti |
| 15 | Pratinjau jurnal | LULUS — modal "Konfirmasi Pengeluaran" menampilkan **D 5-4100 Rp 1.500.000 / K 1-1300 Rp 1.500.000**, berlabel "dihitung backend, bukan frontend", seimbang |
| 16 | Pratinjau BKK | LULUS — modal menyatakan BKK diterbitkan saat posting; nomornya memang baru lahir saat commit (W-2 `last_val`, tanpa reservasi), dan terlihat nyata di daftar: BKK/2026/000007…000012 |
| 17 | Halaman biaya proyek lama | LULUS — `/proyek/1543/biaya` "3 entri", tiap baris membawa nomor BKK-nya |
| 18 | Reload setelah navigasi | LULUS — muat ulang penuh merender data yang sama, form kembali bersih, konsol tanpa error |
| 19 | Tidak ada data ganda | LULUS — `cost_entries` tenant = **5 baris**; daftar pengeluaran = 5 baris; halaman proyek 1543 = 3 baris (subset yang benar) |
| 20 | Status turunan, bukan tersimpan | LULUS — `SHOW COLUMNS FROM cost_entries` tidak punya kolom status/posted; `expenseStatusOf(posted_at, reversal)` di `expense_query.go:216` yang menentukannya, dan `IsRABRealization = BudgetItemID != nil` |

Transaksi nyata **tidak** dibuat (§18): `SELECT COUNT(*) FROM cost_entries WHERE
tenant_id=9900246 AND DATE(created_at)=CURDATE()` → **0**. Modal konfirmasi
ditutup lewat **Batal**.

---

## 11. Keterbatasan yang diketahui

| # | Hal | Sikap |
|---|---|---|
| L-1 | `atk` dan `software` sama-sama ke 5-4000 | Memecahnya jadi baris laba rugi sendiri butuh **akun COA baru** — keputusan pemilik. Belum ada API CRUD akun. |
| L-2 | `payment_method=payable` ditolak di `/expenses`, tetapi jalur lama biaya proyek masih menerimanya | Opsi di UI lama sudah **dinonaktifkan** (bukan dihapus diam-diam). AP adalah increment terpisah (§9). |
| L-3 | Jalur lama `createAndPostCostEntryAction` masih create-lalu-post dalam dua panggilan HTTP | `/expenses` sudah atomik. Menyatukan jalur lama = perubahan kontrak, di luar W-10. |
| L-4 | Tidak ada idempotency key pada `POST /expenses` | Submit ganda menghasilkan dua transaksi berbeda dengan dua nomor BKK berbeda — terdeteksi, bukan tertimpa (`TestW10_19`). Kunci idempotensi butuh keputusan produk. |
| L-5 | Validasi akun kas/bank kini **menolak** payload yang dulu diterima diam-diam | Disengaja (§5). Klien API lama yang mengirim kode akun sembarang akan mulai mendapat 400. |
| F-5 | ~~Nominal kosong tidak memunculkan pesan error~~ | **SELESAI 2026-08-14** (disetujui pemilik). `RupiahInput` kini punya opsi `showErrorWhenEmpty`; aturan tampilnya pindah ke fungsi murni `shouldShowRupiahError` di `components/ui/rupiah.ts` dan diuji (`rupiah.test.ts`, `npm test`). Bawaannya tetap menelan error pada kotak kosong — form yang menghitung error secara *eager* (DisbursementModal, BookingFormModal, ChargeGroupsPanel) tidak berubah sedikit pun. Hanya `ExpenseForm` yang menyalakannya. Sisa: teks error bertahan basi sampai `validate()` berikutnya (kosmetik), dan CostEntryForm/RABManager/ScheduleForm punya cacat yang sama tapi **tidak** disentuh (di luar scope F-5). |
| F-4 | `internal/reporting` dan `internal/legacyar` sama-sama memakai tenant `9900777` | Bentrok saat `go test` paralel. Butuh `-p 1`. **Bukan** regresi W-10 — perlu perbaikan terpisah. |

Temuan lama yang masih terbuka dan tidak disentuh W-10: F-1 (drift versi Go
`go.mod` 1.25 vs Dockerfile 1.22), F-2 (`PORT` di `frontend/.env` tidak
berpengaruh), T-7 (Jurnal Umum "Simpan & Posting" 400), T-8 (`parseFloat` uang
di `JournalCreateForm.tsx`).

---

## 12. Berkas yang berubah

### Migrasi

- `backend/migrations/000072_expense_types.up.sql` / `.down.sql`
  – tabel `expense_types`, kolom `cost_entries.expense_type_id` (NULL = jalur
  lama, histori tidak berubah), seed 7 jenis default untuk seluruh tenant
  (idempoten, hanya ke akun yang sudah ada).

### Backend

| Berkas | Peran |
|---|---|
| `internal/cost/expense_type.go` | master jenis pengeluaran + audit `master_data_changes` + seed tenant baru |
| `internal/cost/expense_handler.go` | rute `/expenses`, `/expense-types`; terjemahan scope; penolakan payable |
| `internal/cost/expense_query.go` | pembacaan daftar/detail — status & nomor dokumen **diturunkan**, bukan disimpan |
| `internal/cost/service.go` | resolusi akun beban dari master, INV-EXP-2, validasi unit-proyek |
| `internal/cost/accounts.go` | himpunan akun taksonomi diturunkan dari domain |
| `internal/cost/errors.go` | error baru (jenis pengeluaran, INV-EXP-2, payable, kas/bank) |
| `internal/cost/model.go`, `repository.go` | kolom `expense_type_id`, resolver |
| `internal/cost/handler.go` | pemasangan rute baru |
| `internal/cost/w10_expense_integration_test.go` | 24 test §17 |

### Frontend

| Berkas | Peran |
|---|---|
| `app/(app)/accounting/pengeluaran/page.tsx` | halaman server + peringatan master kosong |
| `app/(app)/accounting/pengeluaran/actions.ts` | server action preview/create + konteks proyek (unit, item RAB plan aktif) |
| `components/expense/ExpenseForm.tsx` | form dua scope, pratinjau jurnal sebelum submit, panel sukses (BKK + jurnal + dampak RAB) |
| `components/expense/ExpenseList.tsx` | daftar + empty state informatif |
| `lib/api/expense.ts`, `lib/types/api.ts` | klien API + tipe (`allows_project_tag`, `is_rab_realization`) |
| `components/layout/Sidebar.tsx`, `CommandPalette.tsx` | entri navigasi |
| `app/(app)/proyek/[id]/biaya/page.tsx` | §16 — pengeluaran W-10 muncul di sini dengan nama jenis, badge RAB, nomor BKK |
| `components/biaya/CostEntryForm.tsx` | §5 `CashBankSelect` menggantikan input kode bebas; §9 opsi payable dinonaktifkan; arahan ke halaman Pengeluaran |

Halaman `Proyek → Biaya` **tidak dihapus** dan **tidak** meminta input kedua:
W-10 menulis ke tabel yang sama, jadi barisnya sudah ada di sana. Yang ditambah
hanya label agar terbaca (§16).

---

## 13. Yang masih harus diputuskan pemilik

1. **Akun COA baru** untuk memisahkan ATK dan Software dari 5-4000 (L-1).
2. **Kunci idempotensi** untuk `POST /expenses` — perlu atau cukup deteksi (L-4).
3. **Modul Hutang Usaha (AP)** sebagai increment terpisah agar `payable`
   punya jalur pelunasan (§9, L-2).
