# W-11 — Hutang Usaha & Termin Kontraktor

**Status:** DESIGN ONLY — **REVISI 2 (refinement)**. Belum ada kode, belum ada migrasi, belum ada perubahan schema.
**Tanggal audit:** 2026-08-14 · **Refinement:** 2026-08-14
**Prasyarat:** Tahap 1 (penutupan lubang API `payment_method=payable`) — SELESAI & di-approve.
**Revisi 1:** di-approve pemilik sebagai **DESIGN REVIEW** (bukan approval coding).
**Instruksi penutup:** dokumen ini menunggu review pemilik atas refinement. Tidak ada permintaan approval coding di sini.

> ⚠️ **DIGANTIKAN SEBAGIAN.** Keputusan final ada di **[`w11-final-decision-lock.md`](./w11-final-decision-lock.md)** (2026-08-14). Dokumen ini tetap berlaku sebagai **analisis dan bukti** (audit sistem, penolakan kandidat COA, temuan U-1/U-2), tetapi bila ada selisih, **decision lock yang menang**. Khususnya: BD-1..BD-15 di sini sudah diselesaikan menjadi D-1..D-20 di sana; uang muka vendor naik dari P1 ke **P0**; PPN masuk **P0** dan PPh **ditunda**; asimetri reversal dikeluarkan ke **W-11b**.

> ### Constraint terkunci oleh pemilik (Revisi 2)
>
> **C-1 (dari T-1 & T-2) — ARCHITECTURAL CONSTRAINT, WAJIB.**
> `cost_entries` **tetap** menjadi sumber biaya untuk realisasi proyek/RAB. **Dilarang** membuat baris AP invoice/termin terpisah yang menyebabkan `GetRealisasiPerItem` kehilangan biaya kontraktor. W-11 dibangun dengan **mempertahankan integrasi** ke `cost_entries` dan ledger existing — **bukan** membuat parallel source of truth.
>
> **C-2 (BD-1)** — Hutang lahir saat tagihan/termin **sudah disetujui dan kewajiban kepada vendor resmi diakui**; bukan saat pembayaran. → dipetakan ke kode existing di **U.1**.
> **C-3 (BD-3)** — Retensi didukung sejak awal W-11; biaya/WIP tetap bruto, kewajiban langsung vs kewajiban retensi harus dapat dibedakan. → **U.3**.
> **C-4 (BD-4)** — COA **belum final**. Audit COA lengkap dulu, tunjukkan gap. Tanpa migrasi/seed akun sebelum approval. → **U.5**.
> **C-5 (BD-5)** — Vendor Master minimal ber-tenant, bukan VARCHAR bebas sebagai source of truth; `vendor VARCHAR(200)` tetap diperhitungkan untuk kompatibilitas historis. Jangan over-engineer. → **U.6**.

---

## Konvensi label

Setiap pernyataan dalam dokumen ini diberi label:

| Label | Arti |
|---|---|
| **[FAKTA]** | Kondisi kode/data yang sudah terverifikasi. Ada bukti file:line atau hasil query. |
| **[TEMUAN]** | Kesimpulan hasil audit — bukan kode yang tertulis, tapi konsekuensi yang mengikat desain. |
| **[REKOMENDASI]** | Usulan Claude. **Bukan keputusan.** Bisa ditolak tanpa merusak dokumen. |
| **[KEPUTUSAN BISNIS]** | Harus dijawab pemilik/klien sebelum coding dimulai. Claude tidak boleh menebak. |

---

## Ringkasan eksekutif (baca ini kalau cuma punya 3 menit)

Tiga temuan menentukan seluruh bentuk W-11:

1. **[TEMUAN T-1 — paling menentukan]** Realisasi RAB dan biaya aktual proyek dibaca dari **ledger**, bukan dari pembayaran. Konsekuensinya: begitu hutang diakui (Dr biaya proyek / Cr 2-1000), realisasi RAB **otomatis bertambah** tanpa mengubah satu baris pun reader W-5/W-6; dan pembayaran hutang (Dr 2-1000 / Cr Bank) **tidak menyentuh akun biaya sama sekali**, jadi double counting **secara struktural mustahil** — bukan karena disiplin, tapi karena bentuk jurnalnya. Ini menjawab BAGIAN B.2/B.3 dengan aman.

2. **[TEMUAN T-2 — batasan data model terkeras]** Drill-down realisasi *per item RAB* (`GetRealisasiPerItem`, `internal/budget/repository.go:331`) tidak membaca ledger — ia membaca `SUM(cost_entries.amount)`. Artinya: **kalau baris biaya AP ditaruh di tabel baru dan bukan di `cost_entries`, realisasi per-item RAB akan diam-diam kehilangan semua biaya kontraktor.** Ini satu-satunya alasan teknis paling kuat kenapa AP harus menulis ke `cost_entries`, bukan ke tabel baris tersendiri.

3. **[TEMUAN T-3]** Hampir semua mesin yang dibutuhkan W-11 **sudah ada dan sudah terbukti**: penomoran & BKK (W-2/W-3), aging (W-4), rekonsiliasi sub-ledger vs GL kupas-lapis (W-7), idempotency + row-lock pembayaran (`charge.RecordPayout`), gate approval, guard periode tutup, dan reversal ber-dokumen. W-11 **seharusnya penambahan, bukan penemuan**. Yang benar-benar baru hanya: master vendor, entitas kewajiban (AP invoice/termin), alokasi pembayaran vendor, dan 2 akun COA usulan (retensi & uang muka vendor).

**Verdict:** W-11 layak dibangun, dan bisa dibangun kecil. Ruang lingkup P0 muat dalam satu increment. Yang menahan bukan teknis — melainkan **12 keputusan bisnis di BAGIAN Q** yang harus dijawab pemilik lebih dulu.

---

# BAGIAN A — AUDIT SISTEM EXISTING

Audit ini menelusuri lifecycle API → service → repository → ledger/posting → reporting, bukan sekadar grep nama file.

## A.1 `cost_entries` — jalur tulis biaya proyek

**[FAKTA]** Schema aktual (`SHOW CREATE TABLE cost_entries`, AUTO_INCREMENT=725):

| Kolom | Tipe | Catatan |
|---|---|---|
| `id` | BIGINT UNSIGNED AI PK | |
| `tenant_id` | BIGINT UNSIGNED NOT NULL | terindeks |
| `project_id` | BIGINT UNSIGNED NULL | nullable sejak Increment 2 (overhead) |
| `phase_id` | BIGINT UNSIGNED NULL | |
| `unit_id` | BIGINT UNSIGNED NULL | |
| `budget_item_id` | BIGINT UNSIGNED NULL | FK → `budget_items`, `ON DELETE SET NULL` |
| `category` | VARCHAR | taksonomi biaya |
| `cost_tier` | VARCHAR | `direct` / `shared` / `overhead` |
| `amount` | DECIMAL(20,4) | |
| `vendor` | VARCHAR(200) NOT NULL DEFAULT `''` | **free text** |
| `payment_method` | VARCHAR(20) NOT NULL | COMMENT `'bank\|payable'` |
| `journal_entry_id` | BIGINT UNSIGNED | |

**[FAKTA]** CHECK constraint yang hidup: `chk_ce_tier_valid`, `chk_ce_tier_category`, `chk_ce_tier_project`, `chk_ce_tier_unit` (Increment 2, migrasi 000035/000036). Aturan tier ditegakkan di level DB, bukan cuma aplikasi.

**[FAKTA]** Lifecycle tulis: `handler.decodeCostDTO` → `service.CreateCostEntry` → `resolveDebitCreditCodes` → `PostCostEntry` → `ledger.PostingService.CreateAndPost`.

**[FAKTA]** `internal/cost/service.go` — pemetaan akun:

```go
func resolveDebitCreditCodes(tier domain.CostTier, req CreateCostEntryRequest) (debitCode, creditCode string) {
	if tier == domain.CostTierOverhead {
		debitCode = req.Category.ExpenseAccountCode()
	} else {
		debitCode = req.Category.InventoryAccountCode()
	}
	creditCode = payableAccountCode()
	if req.PaymentMethod == PaymentMethodBank {
		creditCode = req.BankAccountCode
	}
	return debitCode, creditCode
}
```

**[FAKTA]** Fungsi ini **pure** dan dipakai bersama oleh preview dan create — itulah jaminan bahwa pratinjau jurnal tidak pernah berbeda dari jurnal yang benar-benar diposting. W-11 wajib memakai pola yang sama.

**[FAKTA]** `PostCostEntry` meneruskan `e.PaymentMethod == PaymentMethodBank` sebagai flag `cashOut`. Artinya entri payable **sengaja tidak menerbitkan dokumen kas** — dan itu benar, karena tidak ada uang yang bergerak saat hutang diakui. Perilaku ini tetap benar di bawah W-11 dan tidak perlu diubah.

## A.2 `payment_method = "payable"` — status setelah Tahap 1

**[FAKTA]** Tahap 1 menambahkan guard di `internal/cost/handler.go` (`decodeCostDTO`), sebelum parsing amount/tanggal, menutup keempat entry point legacy:

```go
if PaymentMethod(dto.PaymentMethod) == PaymentMethodPayable {
	writeCostError(w, http.StatusBadRequest, ErrPayableNotSupported.Error())
	return dto, domain.Zero, time.Time{}, false
}
```

**[TEMUAN]** Guard ini ada di **lapisan transport**, bukan domain. `service.CreateCostEntry` masih menerima `payable` bila dipanggil langsung dari dalam proses (dan memang masih dipanggil begitu oleh `cmd/demo-seed`). Ini **desain yang tepat untuk W-11**: W-11 tinggal menambah pemanggil sah yang baru di lapisan service, tanpa harus membongkar guard yang melindungi rute lama. Guard transport tetap dipertahankan — rute `/projects/{id}/cost-entries` **tidak** boleh jadi pintu AP.

## A.3 Akun 2-1000 Hutang Usaha

**[FAKTA]** `internal/ledger/account_role.go:29,48` — `RolePayable → {"2-1000"}`. Akun sudah terdaftar dalam registry peran.

**[FAKTA]** Konstitusi registry: setiap pemilihan akun untuk perhitungan saldo **wajib** lewat `RoleCodeList` / `AccountInRole`. Menulis `a.code IN (...)` atau `LIKE '1-3%'` di query baru adalah pelanggaran. W-11 terikat aturan ini.

**[FAKTA]** Hasil query ledger (read-only, seluruh tenant): **tidak pernah ada satu pun baris debit yang diposting ke 2-1000.** Dr = 0.0000 di semua tenant. Hanya kredit.

**[TEMUAN]** Ini bukti langsung bahwa 2-1000 hari ini adalah **akun satu arah** — saldo bisa lahir tapi tidak pernah bisa mati. Itulah lubang yang W-11 tutup.

## A.4 `RolePayable` dan peran terkait

**[FAKTA]** `internal/ledger/account_role.go:48,52,53`:
- `RolePayable → {"2-1000"}`
- `RoleVATOutput → {"2-3000"}` (PPN Keluaran)
- `RoleVATInput → {"1-5100"}` (PPN Masukan)

**[TEMUAN]** `RoleVATInput` sudah terdaftar tapi **belum pernah ada produsen** — tidak ada jalur di repo yang mendebit 1-5100. PPN Masukan hari ini adalah slot kosong yang menunggu W-11 atau modul pajak pembelian. Ini penting untuk BAGIAN G.

## A.5 Vendor sebagai VARCHAR

**[FAKTA]** Tidak ada tabel `vendors` di schema. Nol.

**[FAKTA]** Kolom vendor free-text ada di **dua tempat independen**: `cost_entries.vendor VARCHAR(200)` dan kolom vendor di `charge_payouts`. Dua kolom, dua ejaan, tidak ada relasi.

**[FAKTA]** Data dev: 11 baris punya `vendor` tidak kosong. Tenant 9900246 punya 5 nama vendor berbeda — `CV Bangun Perkasa`, `Konsultan Adi`, `pemilik lahan`, `PT Tanah Jaya`, `PT Udin` — semuanya bank-paid, nol payable.

**[TEMUAN]** Nilai seperti `pemilik lahan` dan `PT Udin` menunjukkan kolom ini dipakai sebagai catatan bebas, bukan identitas entitas. Mengelompokkan aging AP berdasarkan string seperti ini akan pecah diam-diam pada typo pertama.

## A.6 `budget_item_id`

**[FAKTA]** Nullable, FK ke `budget_items` dengan `ON DELETE SET NULL`.

**[FAKTA]** `internal/budget/repository.go:331` — `GetRealisasiPerItem`:

```go
Table("cost_entries ce").
Select("ce.budget_item_id, SUM(ce.amount) as total").
Joins("JOIN budget_items bi ON bi.id = ce.budget_item_id AND bi.tenant_id = ce.tenant_id").
Joins("JOIN journal_entries je ON je.id = ce.journal_entry_id AND je.tenant_id = ce.tenant_id").
Where("ce.tenant_id = ? AND bi.budget_plan_id = ? AND ce.budget_item_id IS NOT NULL AND je.posted_at IS NOT NULL", ...)
```

**[TEMUAN — T-2, batasan data model terkeras]** Realisasi per item RAB **tidak dibaca dari ledger** — ia dibaca dari `SUM(cost_entries.amount)`, posted-only. Ini satu-satunya reader realisasi yang bergantung pada tabel, bukan jurnal.

Konsekuensi yang mengikat: **kalau baris biaya AP disimpan di tabel baru (`ap_invoice_lines`) dan bukan di `cost_entries`, drill-down per item RAB akan diam-diam nol untuk seluruh biaya kontraktor** — padahal justru termin kontraktor-lah biaya proyek terbesar. Ini bug senyap paling mahal yang bisa lahir dari W-11, dan hanya bisa dicegah di tahap desain.

## A.7 `project_id`

**[FAKTA]** Nullable sejak Increment 2, karena overhead tidak punya proyek. `chk_ce_tier_project` menegakkan kombinasi tier↔project di DB.

**[FAKTA]** W-10 / INV-EXP-2: tag proyek ≠ realisasi RAB. Beban operasional yang akunnya masuk himpunan taksonomi CostCategory **dilarang** di-tag proyek — fail-closed di `plan()`.

**[TEMUAN]** Aturan ini berlaku penuh untuk W-11: AP untuk beban kantor tidak boleh di-tag proyek, AP untuk termin kontraktor harus di-tag proyek. Tidak perlu aturan baru — aturan lama tinggal diwarisi.

## A.8 Baris jurnal existing untuk payable

**[FAKTA]** Bentuk jurnal saat ini untuk `payment_method=payable`:

```
Dr  1-3xxx (inventory/WIP)  atau  5-xxxx (beban, tier overhead)   X
    Cr  2-1000 Hutang Usaha                                            X
```

**[FAKTA]** Semua baris payable existing berada di `journal_entries.source = 'system'`.

**[TEMUAN]** `source = 'system'` terlalu generik untuk dipakai rekonsiliasi kupas-lapis (BAGIAN J). W-11 perlu source yang khas (`ap_invoice`, `ap_payment`) supaya sub-ledger bisa dipisah dari jurnal manual dan saldo awal. Ini konsisten dengan pola W-7.

## A.9 Document registry & penomoran (W-2)

**[FAKTA]** Satu engine, tabel `document_sequences` berkunci `(tenant_id, document_type_code, fiscal_year)`. `last_val` = nomor terakhir terpakai (bukan `next_val`). Tahun fiskal diambil dari **waktu penerbitan**. Reset tahunan forward-only.

**[FAKTA]** Menambah jenis dokumen **tidak menyentuh package** — cukup baris master di `document_types`.

**[FAKTA]** 11 jenis dokumen hidup: KWT, KWB, KWR, MTI, INV, KWD, BKM, BKK, BTP, RFC, JR.

**[FAKTA]** `internal/ledger/document_issuer.go:62-67` mengekspos konstanta `DocCashIn` (BKM), `DocCashOut` (BKK), `DocThirdPartyPayout` (BTP), `DocCustomerRefund` (RFC), `DocKPRDisbursement` (KWD), `DocJournalReversal` (JR).

**[TEMUAN]** Pembayaran AP **tidak butuh mekanisme nomor baru**. BKK sudah ada dan sudah tepat secara ekonomi (uang perusahaan sendiri keluar, bukan titipan pihak ketiga).

## A.10 BKK & INV-DOC-1 (W-3)

**[FAKTA]** INV-DOC-1: satu pergerakan kas = satu dokumen bernomor. Ditegakkan **fail-closed** di dalam transaksi, di `PostingService.enforceDocument` — sengaja **bukan** seam opt-in bergaya `With…`.

**[FAKTA]** Dua pengecualian saja: `Source == opening_balance`, dan store non-GORM.

**[FAKTA]** Pilihan dokumen kas-keluar manual: `[BKK, BTP, RFC]` — pilihannya mengkodekan **kepemilikan ekonomi uang**, bukan sekadar label.

**[TEMUAN]** Pembayaran hutang usaha = uang perusahaan sendiri → **BKK**, dan `DefaultCashSpec` sudah memilih BKK otomatis untuk kas keluar. W-11 tidak perlu menulis logika dokumen sama sekali; cukup tidak melawannya.

## A.11 Reversal

**[FAKTA]** `PostingService.Reverse` / `reverseWithin` menerbitkan dokumen JR yang menunjuk jurnal asal. Ledger append-only dipertahankan.

**[FAKTA]** Guard periode dicek pada **tanggal reversal**, bukan tanggal jurnal asal (aturan R-2/W-8). Koreksi terhadap periode tertutup dilakukan dengan menanggalkan reversal di periode terbuka.

**[TEMUAN]** Pembalikan AP dan pembalikan pembayaran AP mewarisi seluruh mekanisme ini gratis. W-11 tidak boleh membuat jalur reversal sendiri.

## A.12 Period closing

**[FAKTA]** `IsPeriodClosed` diimplementasikan **satu kali** di `internal/ledger/repository.go:176`, dipanggil dari `Create` dan dari `reverseWithin`.

**[TEMUAN]** Karena AP dan pembayaran AP keduanya lewat `PostingService`, guard periode berlaku otomatis. Tidak perlu pengecekan tambahan di service W-11 — dan menambahkannya justru akan menciptakan sumber kebenaran kedua.

## A.13 Mesin AP / aging yang bisa dicermin

**[FAKTA]** `internal/receivable/receivable.go` (425 baris) adalah **satu-satunya** mesin aging di repo (INV-AR-1). Ia package **value-object murni** — hanya mengimpor `domain` dan `decimal`, tidak menyentuh DB.

**[FAKTA]** Isinya: `Row`, `Bucket` (`current` / `1_30` / `31_60` / `61_90` / `90_plus`), `Status`, `DaysBetween`, `EffectiveStatus`, `BuildAging`, `SourceSummary`, `FilterBySource`.

**[TEMUAN]** Karena murni, **produsen mana pun bisa memancarkan `receivable.Row` tanpa inversi dependensi**. Aging AP tidak perlu mesin bucketing kedua — cukup memakai aturan bucket yang sama dengan laporan berkolom vendor. Membuat bucketing sendiri di package `payable` akan melanggar semangat INV-AR-1.

## A.14 Pajak existing

**[FAKTA]** `internal/tax/model.go` — enum `TriggerEvent` sudah punya `TriggerInvoice` dan `TriggerPayment` **terdeklarasi tapi belum terimplementasi**; yang hidup baru `bast`.

**[FAKTA]** `VATReport` sudah menghitung PPN Masukan sebagai Σ Dr 1-5100 dari ledger — laporannya siap, produsennya belum ada.

**[FAKTA]** COA **tidak punya** akun PPh 23 maupun PPh 4(2).

**[TEMUAN]** Seam pemotongan pajak saat pembayaran sudah punya kosakata (`TriggerPayment`). Yang belum ada: akun hutang PPh. Ini menentukan bentuk jawaban BAGIAN G.

## A.15 Biaya proyek / realisasi RAB

**[FAKTA]** `internal/ledger/actual_cost.go:106` — ekspresi netting kanonik:

```sql
COALESCE(SUM(CASE WHEN je.source <> 'reversal' THEN jl.debit ELSE 0 END), 0)
- COALESCE(SUM(CASE WHEN je.source = 'reversal' THEN jl.credit ELSE 0 END), 0)
```

posted-only, di-scope `ProjectID` / `PhaseID`.

**[FAKTA]** `internal/budget/repository.go:309` — `GetRealisasiByProject` mendelegasikan ke `ActualCostByCode`. Ia **tidak** membaca `cost_entries`.

**[TEMUAN — T-1]** Karena realisasi = debit pada akun taksonomi yang di-tag proyek:
- Pengakuan AP (`Dr 1-3xxx / Cr 2-1000`) **otomatis** menjadi realisasi RAB pada saat pengakuan.
- Pembayaran AP (`Dr 2-1000 / Cr Bank`) **tidak menyentuh akun taksonomi sama sekali** → tidak mungkin dihitung dua kali.

Ini memenuhi batasan pemilik ("jangan ubah `GetRealisasiByProject` atau reader W-5/W-6") **secara konstruksi**, bukan secara disiplin.

## A.16 Saldo awal

**[FAKTA]** `Source == opening_balance` adalah satu dari dua pengecualian INV-DOC-1 — saldo awal boleh tanpa dokumen kas.

**[FAKTA]** Pola W-7 (`internal/legacyar`): "Impor piutang lama adalah **BUKU PEMBANTU**, bukan kejadian ekonomi" (INV-LAR-4). Impor **tidak pernah** menulis jurnal; hanya pelunasan yang menulis jurnal. Saldo GL-nya sudah ada lewat saldo awal.

**[TEMUAN]** Hutang lama (payable historis) punya cetakan yang **persis sama**. W-11 tidak perlu menemukan pola baru; tinggal mencerminkan `legacyar`.

## A.17 Jurnal manual

**[FAKTA]** Jurnal manual bisa menyentuh 2-1000 langsung, tanpa lewat sub-ledger AP mana pun.

**[TEMUAN]** Karena itu invariant "AP outstanding == saldo GL 2-1000" **tidak boleh** dirumuskan sebagai kesamaan mutlak. Ia harus dirumuskan per-source, sama seperti rekonsiliasi W-7. Merumuskannya sebagai kesamaan mutlak akan membuat sistem gagal pada jurnal manual pertama yang sah.

## A.18 Isolasi tenant

**[FAKTA]** 463 kemunculan `tenant_id = ?` eksplisit di repository (di luar test). Tidak ada registrasi GORM callback / global scope yang ditemukan.

**[TEMUAN — perlu dicatat jujur]** CLAUDE.md menyebut mekanismenya "GORM global scope", tetapi implementasi nyatanya adalah **WHERE eksplisit di setiap query**. Invariantnya tetap terpenuhi, tetapi mekanisme yang dinamai di dokumen bukan mekanisme yang dipakai di kode. Ini bukan bug; ini ketidakcocokan dokumentasi. W-11 harus mengikuti **kode** (WHERE eksplisit + `auth.TenantIDFrom(ctx)` di handler), dan harus dibuktikan integration test lintas-tenant seperti increment lain.

---

# BAGIAN B — SEMANTIK AKUNTANSI

## B.1 Kapan hutang lahir?

Tiga kandidat, dengan konsekuensi lengkapnya:

### Opsi A — saat progres pekerjaan disetujui (BAP)

| Aspek | Konsekuensi |
|---|---|
| GL | Biaya & hutang diakui saat pekerjaan selesai secara fisik — matching paling tepat secara akrual |
| RAB | Realisasi mengikuti progres fisik, cocok dengan modul Progress Fisik (migrasi 000051) |
| WIP | Tepat: WIP bertambah saat nilai terbentuk |
| Laba rugi | Paling akurat pada cut-off periode |
| Jatuh tempo | **Tidak ada** — BAP tidak punya tanggal jatuh tempo. Aging tidak bisa dimulai |
| Audit trail | Lemah — belum ada dokumen eksternal dari vendor |
| PPN Masukan | **Tidak bisa** dikreditkan — belum ada Faktur Pajak |

### Opsi B — saat tagihan vendor diterima

| Aspek | Konsekuensi |
|---|---|
| GL | Standar praktik AP; hutang lahir saat ada klaim sah dari pihak luar |
| RAB | Realisasi bertambah saat tagihan masuk |
| WIP | Tepat, selama tagihan hanya boleh dibuat atas progres yang sudah disetujui |
| Laba rugi | Berisiko salah periode: pekerjaan Desember, tagihan datang Januari |
| Jatuh tempo | **Ada** — tanggal invoice + termin pembayaran. Aging jalan |
| Audit trail | Kuat — nomor invoice vendor, tanggal, nilai, lampiran |
| PPN Masukan | Bisa — Faktur Pajak menyertai tagihan |

### Opsi C — saat approval internal

| Aspek | Konsekuensi |
|---|---|
| GL | Mencampur tata kelola dengan pengakuan. Tagihan yang sudah diterima tapi belum di-approve menjadi **tak terlihat di neraca** → kewajiban dilaporkan lebih rendah dari kenyataan |
| RAB | Realisasi tertunda oleh antrean administrasi, bukan oleh fakta ekonomi |
| Jatuh tempo | Ada, tapi jam aging mundur karena keterlambatan internal — menyembunyikan keterlambatan bayar |
| Audit trail | Kuat untuk otorisasi, tapi salah tempat |

### [REKOMENDASI] Kombinasi B + A sebagai prasyarat + C sebagai gate pembayaran

- **Pengakuan GL = Opsi B** (tagihan vendor diterima/diverifikasi). Inilah saat kewajiban hukum lahir dan satu-satunya saat semua atribut yang dibutuhkan aging tersedia.
- **Opsi A jadi prasyarat kelayakan**, bukan pemicu jurnal: tagihan tidak boleh dibuat melebihi progres yang sudah disetujui. Ini kontrol, bukan pengakuan.
- **Opsi C jadi gate pada pembayaran**, bukan pada pengakuan. Hutang yang belum di-approve tetap muncul di neraca — hanya belum boleh dibayar. `approval.RequireApproved` dengan `TargetPayment` sudah menyediakan ini dan sifatnya OPT-IN.
- **Masalah cut-off periode** (pekerjaan Des, tagihan Jan) diselesaikan terpisah dengan akrual: `Dr biaya / Cr 2-6000 Biaya Akrual` di akhir periode, dibalik di periode berikutnya. **[FAKTA]** akun 2-6000 Biaya Akrual **sudah ada** di COA. Ini masuk P2, bukan P0.

**[KEPUTUSAN BISNIS BD-1]** — lihat BAGIAN Q.

## B.2 AP vs Realisasi RAB — keputusan paling penting

### Apakah hutang proyek yang diakui langsung jadi realisasi RAB?

**[FAKTA]** Ya — dan itu terjadi otomatis, tanpa kode tambahan. `GetRealisasiByProject` → `ActualCostByCode` menghitung debit pada akun taksonomi yang di-tag proyek, posted-only. Jurnal `Dr 1-3100 (project_id=X) / Cr 2-1000` langsung masuk hitungan.

**[TEMUAN]** Ini konsisten dengan akrual dan dengan `cost_tier` yang sudah ada. Realisasi RAB memang harus mengikuti **biaya yang sudah terjadi**, bukan kas yang sudah keluar — kalau tidak, RAB akan selalu terlihat under-spent sampai vendor dibayar.

### Apakah `project_id` wajib?

**[REKOMENDASI]** Wajib **secara kondisional**, mengikuti aturan yang sudah ada — jangan bikin aturan baru:
- Baris AP dengan `cost_tier = direct` / `shared` → `project_id` **wajib** (`chk_ce_tier_project` sudah menegakkan).
- Baris AP dengan `cost_tier = overhead` → `project_id` **wajib NULL** (INV-EXP-2: akun taksonomi tidak boleh di-tag proyek untuk beban operasional).

Satu tagihan vendor boleh memuat beberapa baris dengan proyek berbeda. Itulah alasan model header/lines di BAGIAN T.

### Apakah `budget_item_id` wajib untuk biaya proyek?

**[FAKTA]** Hari ini nullable, dan `GetRealisasiPerItem` sengaja mengabaikan baris tanpa `budget_item_id` (komentar di `repository.go:328-330` menyatakan baris itu "masuk ke `GetRealisasiByProject`").

**[REKOMENDASI]** Tetap **opsional**, tapi **sangat dianjurkan** di UI (default terpilih bila item RAB bisa ditebak dari kategori+proyek). Mewajibkannya akan memblokir kasus sah — misalnya tagihan yang membentang beberapa item RAB atau biaya yang belum dianggarkan. Tapi tanpa `budget_item_id`, biaya hanya muncul di realisasi tingkat proyek dan **hilang dari drill-down per item**, yang membingungkan pengguna.

**[KEPUTUSAN BISNIS BD-2].**

### Bagaimana overhead diperlakukan?

**[REKOMENDASI]** Persis seperti W-10: `Dr 5-xxxx / Cr 2-1000`, `project_id` NULL, tidak masuk realisasi RAB mana pun. Tidak ada perlakuan baru.

## B.3 Double counting tidak boleh terjadi

**[TEMUAN — konsekuensi T-1]** Pembayaran AP menghasilkan jurnal:

```
Dr  2-1000 Hutang Usaha    X
    Cr  1-1xxx Bank             X
```

Tidak ada akun taksonomi biaya yang tersentuh. `ActualCostByCode` menyaring berdasarkan daftar kode akun biaya — jurnal ini **tidak masuk ke himpunan itu sama sekali**, jadi ia tak terlihat oleh reader realisasi.

**Kesimpulan:** double counting bukan risiko yang perlu dijaga dengan aturan, flag, atau kolom status. Ia **mustahil terjadi** selama pembayaran hanya mendebit kewajiban dan mengkredit kas. Satu-satunya cara merusaknya adalah kalau seseorang menulis pembayaran AP yang menyentuh akun biaya — dan itu sudah dilarang oleh bentuk jurnalnya sendiri.

**[REKOMENDASI]** Kunci ini sebagai invariant yang diuji, bukan sekadar diasumsikan — lihat INV-AP-3 di BAGIAN T.7.

---

# BAGIAN C — TERMIN KONTRAKTOR

## Lifecycle lengkap

```
Kontrak ──> Progres fisik ──> BAP / persetujuan progres ──> Tagihan vendor
   │                                                              │
   │                                                              v
   │                                                      Pengakuan hutang
   │                                                     (Dr biaya / Cr AP)
   │                                                              │
   │                                          ┌───────────────────┴──────────┐
   │                                          v                              v
   │                                    Retensi ditahan              Hutang dibayar
   │                                    (Cr 2-1100)               (Dr AP / Cr Bank + BKK)
   │                                          │                              │
   │                                          v                              v
   └──────────> Sisa nilai kontrak     Pelepasan retensi            Outstanding = 0
                belum ditagih          (Dr 2-1100 / Cr Bank)
```

## Apakah W-11 perlu entitas kontrak vendor?

Yang **hanya bisa** dijawab kalau entitas kontrak ada:
- Berapa persen dari nilai kontrak yang sudah ditagih vendor ini?
- Berapa sisa nilai kontrak yang belum ditagih?
- Apakah tagihan ini melebihi nilai kontrak?
- Berapa persen retensi default untuk kontraktor ini?

Yang **sudah bisa** dijawab tanpa entitas kontrak:
- Berapa total hutang ke vendor ini?
- Berapa yang jatuh tempo?
- Berapa biaya proyek ini dari kontraktor?

**[REKOMENDASI]** **Tidak ada entitas kontrak di P0.** Termin = tagihan AP dengan dua atribut tambahan: `contract_ref VARCHAR` (teks bebas) dan `progress_percent DECIMAL`. Entitas `vendor_contracts` yang sebenarnya masuk **P1**, dan hanya kalau pemilik memang butuh kontrol "sisa nilai kontrak".

Alasannya: kontrol nilai kontrak adalah kontrol **pengadaan**, bukan kontrol **akuntansi**. Ketiadaannya tidak membuat satu angka pun di laporan keuangan salah. Ia membuat W-11 tidak bisa mencegah over-billing — yang penting, tapi bisa menyusul.

**[REKOMENDASI]** Kombinasi minimal untuk P0: **vendor + kewajiban AP (invoice/termin) + pembayaran + alokasi**. Empat entitas. Tanpa PO, tanpa GRN, tanpa kontrak, tanpa katalog material.

**[KEPUTUSAN BISNIS BD-3].**

---

# BAGIAN D — RETENSI

Kasus: termin bruto Rp100.000.000, retensi 5%.

## Opsi 1 — retensi dipisah ke akun kewajiban sendiri **[REKOMENDASI]**

**Saat tagihan diakui:**

```
Dr  1-3100 Biaya Konstruksi (WIP)   100.000.000   [project_id = X]
    Cr  2-1000 Hutang Usaha               95.000.000
    Cr  2-1100 Hutang Retensi *)           5.000.000
```

**Saat termin dibayar:**

```
Dr  2-1000 Hutang Usaha              95.000.000
    Cr  1-1xxx Bank                       95.000.000     [BKK]
```

**Saat retensi dilepas** (masa pemeliharaan berakhir / cacat selesai diperbaiki):

```
Dr  2-1100 Hutang Retensi             5.000.000
    Cr  1-1xxx Bank                        5.000.000     [BKK]
```

**Saldo setelah setiap tahap:**

| Tahap | Biaya/WIP | 2-1000 | 2-1100 | Kas | Outstanding AP |
|---|---:|---:|---:|---:|---:|
| Tagihan diakui | 100.000.000 | 95.000.000 | 5.000.000 | 0 | 100.000.000 |
| Termin dibayar | 100.000.000 | 0 | 5.000.000 | −95.000.000 | 5.000.000 |
| Retensi dilepas | 100.000.000 | 0 | 0 | −100.000.000 | 0 |

**Keunggulan:** saldo 2-1000 = benar-benar yang jatuh tempo sekarang, jadi aging AP jujur. Retensi punya jatuh tempo sendiri (biasanya 6–12 bulan) dan tidak mencemari bucket 1–30 hari. Biaya diakui penuh 100 juta sejak awal — retensi adalah penundaan **pembayaran**, bukan penundaan **biaya**.

**Kelemahan:** butuh akun COA baru.

## Opsi 2 — retensi sebagai atribut, tetap di 2-1000

Seluruh 100 juta dikreditkan ke 2-1000; retensi dicatat sebagai kolom di kewajiban AP dan hanya membatasi berapa yang boleh dibayar.

**Keunggulan:** tanpa akun baru, tanpa migrasi COA.
**Kelemahan:** aging AP melaporkan 5 juta sebagai jatuh tempo padahal secara kontrak belum; pelepasan retensi tidak punya tanggal jatuh tempo alami; neraca tidak memisahkan kewajiban jangka pendek dari kewajiban tertahan.

## Usulan akun baru

**[REKOMENDASI — usulan saja, bukan pembuatan]** `2-1100 Hutang Retensi Kontraktor`. **Tidak ada COA dibuat sekarang.** Kalau opsi 1 dipilih, akun ini lahir bersama migrasi W-11 dan didaftarkan ke `AccountRoleRegistry` sebagai `RoleRetentionPayable`.

**[KEPUTUSAN BISNIS BD-3 (retensi) & BD-4.**

---

# BAGIAN E — VENDOR

## Opsi A — pertahankan `vendor VARCHAR(200)`

**Keunggulan:** nol migrasi, nol UI baru, data existing langsung jalan.
**Kelemahan:** aging per vendor mengelompokkan berdasarkan string. `PT Bangun Perkasa`, `PT. Bangun Perkasa`, dan `pt bangun perkasa` menjadi tiga vendor. Tidak ada NPWP → PPh 23 tidak bisa dihitung. Tidak ada rekening bank → pembayaran tidak bisa diverifikasi. Tidak ada termin pembayaran → jatuh tempo harus diketik manual setiap tagihan.

## Opsi B — master vendor ber-tenant **[REKOMENDASI]**

Field minimum:

| Field | Alasan |
|---|---|
| `name` | identitas |
| `npwp` | wajib untuk PPh 23 / PPh 4(2) dan tarif non-NPWP yang lebih tinggi |
| `address` | dokumen & faktur |
| `bank_account_name` / `bank_account_number` / `bank_name` | verifikasi pembayaran, cegah salah transfer |
| `payment_terms_days` | jatuh tempo otomatis = tanggal invoice + termin |
| `is_active` | vendor nonaktif tak muncul di dropdown tapi histori tetap utuh |
| `tenant_id` | isolasi |

**[REKOMENDASI]** Opsi B, dan masuk **P0** — bukan P1. Alasannya: aging per vendor adalah alasan utama AP ada. Kalau pengelompokannya berbasis string, laporannya salah sejak hari pertama dan tidak ada yang tahu.

## Perlakuan nilai VARCHAR existing tanpa migrasi data sekarang

**[REKOMENDASI]** Aditif murni, tanpa backfill:

1. `cost_entries.vendor VARCHAR(200)` **tetap ada apa adanya**. Tidak dihapus, tidak diubah, tidak di-backfill.
2. Tambah `cost_entries.vendor_id BIGINT UNSIGNED NULL` (FK ke `vendors`, `ON DELETE SET NULL`).
3. Baris lama: `vendor_id` NULL, `vendor` terisi teks. Tetap tampil di UI (fallback ke teks).
4. Baris baru dari jalur AP: `vendor_id` terisi, dan `vendor` diisi snapshot nama vendor **saat itu** — supaya histori tidak berubah kalau vendor di-rename. Ini pola snapshot yang sama dengan `TermsSnapshot` (Increment 3) dan snapshot akun di item charge (INV-CG-1).
5. Merge/normalisasi vendor lama = **operasi terpisah, opsional, P2**, dijalankan pemilik lewat UI kalau memang mau — bukan migrasi otomatis yang menebak.

Nol risiko terhadap data existing, karena tidak ada satu baris pun yang disentuh.

---

# BAGIAN F — UANG MUKA VENDOR

## Alur jurnal

**Saat uang muka dibayar** (belum ada biaya, belum ada pekerjaan):

```
Dr  1-5300 Uang Muka Vendor *)     30.000.000        ← ASET, bukan biaya
    Cr  1-1xxx Bank                     30.000.000        [BKK]
```

**Saat tagihan/termin diakui** (pekerjaan sudah ada), dengan kompensasi uang muka:

```
Dr  1-3100 Biaya Konstruksi (WIP)  100.000.000   [project_id = X]
    Cr  1-5300 Uang Muka Vendor          30.000.000        ← kompensasi
    Cr  2-1000 Hutang Usaha              70.000.000
```

**Saat sisa dibayar:**

```
Dr  2-1000 Hutang Usaha             70.000.000
    Cr  1-1xxx Bank                      70.000.000        [BKK]
```

## Kenapa tidak ada double counting

**[TEMUAN]** Uang muka **tidak pernah menyentuh akun biaya**. Ia lahir sebagai aset (1-5300) dan mati sebagai kredit terhadap aset itu di jurnal pengakuan. Total debit ke akun biaya sepanjang seluruh siklus tetap 100.000.000 — persis sekali. Total kas keluar 30 + 70 = 100.000.000 — persis sekali. Realisasi RAB bertambah 100 juta pada saat pengakuan saja, karena hanya jurnal pengakuan yang mendebit akun taksonomi.

Kalau uang muka salah dicatat sebagai biaya sejak awal (`Dr 1-3100 / Cr Bank`), maka pengakuan tagihan penuh 100 juta kemudian **akan** menggandakan 30 juta itu. Itulah kesalahan yang desain ini cegah.

**[REKOMENDASI — usulan akun]** `1-5300 Uang Muka Vendor` (aset lancar). COA existing punya `1-5000 Biaya Dibayar di Muka` dan `1-5100 PPN Masukan`, jadi ruang 1-5xxx memang untuk keluarga ini. **Tidak dibuat sekarang.**

## Fondasi untuk jalur kas-keluar non-beban

**[TEMUAN]** Ini bagian terpenting dari BAGIAN F, dan menyambung ke BAGIAN P.

Hari ini **setiap** jalur kas-keluar di repo berbentuk beban: `cost_entries` mewajibkan `category` + `cost_tier`, `expenses` mewajibkan jenis beban. Tidak ada satu pun jalur yang bisa mengeluarkan uang untuk mendebit sesuatu yang **bukan** biaya — kecuali jurnal manual.

Uang muka vendor adalah kasus pertama yang memaksa jalur itu ada: `Dr <akun aset> / Cr Bank`. Begitu jalur itu dibangun untuk uang muka, ia **sudah** menjadi jalur yang dibutuhkan aset tetap (`Dr 1-4000 / Cr Bank`), karena bentuknya identik — yang berbeda hanya akun debitnya.

**[REKOMENDASI]** Bangun pembayaran W-11 sebagai **penyelesaian kewajiban generik**: akun debit diambil dari kewajiban yang dilunasi, bukan dari taksonomi biaya. Seam-nya persis seperti `expense_types.expense_account_code` di W-10 — akun ditentukan data, bukan `switch` di kode. Dengan itu, W-13 tidak perlu menulis mesin pembayaran sendiri.

---

# BAGIAN G — PAJAK

Tidak ada modul pajak yang dibangun di W-11. Yang dijawab di sini: **apa yang tidak boleh dirusak** supaya PPh/PPN tidak harus membongkar W-11 nanti.

## Ketergantungan yang teraudit

| Pajak | Status existing | Relevansi W-11 |
|---|---|---|
| **PPN Masukan** | `RoleVATInput → 1-5100` **[FAKTA]**; `VATReport` sudah menjumlahkan Dr 1-5100 **[FAKTA]**; **belum ada produsen** **[TEMUAN]** | Tinggi — tagihan vendor ber-Faktur Pajak adalah produsen alaminya |
| **PPh 23** (jasa konstruksi non-kualifikasi, konsultan, sewa) | Tidak ada akun di COA **[FAKTA]** | Tinggi — pemotongan terjadi persis di jalur pembayaran W-11 |
| **PPh 4(2)** (jasa konstruksi berkualifikasi, final) | Tidak ada akun di COA **[FAKTA]** | Tinggi — mayoritas kontraktor properti kena ini |
| **PPh 21** (orang pribadi) | Tidak ada **[FAKTA]** | Rendah untuk W-11 — relevan hanya kalau vendor perorangan. Bisa diabaikan di P0 |
| **PPN Keluaran** | `RoleVATOutput → 2-3000`, sudah hidup di sisi penjualan **[FAKTA]** | Tidak relevan langsung; hanya jadi pasangan PPN Masukan di SPT |

## G.1 Apakah PPN Masukan lahir saat AP/tagihan vendor dibuat?

**[REKOMENDASI]** **Ya — bila tagihan disertai Faktur Pajak.** PPN Masukan dapat dikreditkan berdasarkan Faktur Pajak, bukan berdasarkan pembayaran. Bentuknya:

```
Dr  1-3100 Biaya Konstruksi      100.000.000
Dr  1-5100 PPN Masukan            11.000.000
    Cr  2-1000 Hutang Usaha           111.000.000
```

Hutang ke vendor adalah nilai bruto termasuk PPN — itu yang benar-benar harus dibayar. Biaya proyek tetap 100 juta, jadi realisasi RAB tidak tercemar PPN. Ini penting: **PPN tidak boleh masuk realisasi RAB**, dan bentuk jurnal ini menjaminnya karena 1-5100 bukan akun taksonomi biaya.

**[REKOMENDASI]** P0 cukup menyediakan **field opsional** `ppn_amount` + `faktur_pajak_number` di kewajiban AP dan menerbitkan baris jurnal ke 1-5100 bila diisi. Itu 1 baris jurnal tambahan, bukan modul pajak. Kalau ditunda, `VATReport` tetap kosong seperti sekarang — tidak ada yang rusak, hanya belum lengkap.

**[KEPUTUSAN BISNIS BD-7.**

## G.2 Withholding: saat pengakuan AP atau saat pembayaran?

**[TEMUAN]** Ini pertanyaan fiskal, bukan teknis. Secara PPh, saat terutang adalah **mana yang lebih dulu** antara saat pembayaran dan saat terutangnya penghasilan. Praktik lapangan Indonesia umumnya memotong **saat pembayaran**, karena bukti potong menyertai transfer.

Konsekuensi teknis kedua opsi:

**Potong saat pengakuan:**
```
Dr  1-3100                       100.000.000
    Cr  2-1200 Hutang PPh 23 *)         2.000.000
    Cr  2-1000 Hutang Usaha            98.000.000
```
Outstanding AP = 98 juta = persis yang akan ditransfer. Aging jujur. Tapi kalau tarif/kualifikasi ternyata salah, koreksinya menyentuh jurnal biaya.

**Potong saat pembayaran:**
```
Dr  2-1000 Hutang Usaha          100.000.000
    Cr  2-1200 Hutang PPh 23 *)         2.000.000
    Cr  1-1xxx Bank                    98.000.000     [BKK]
```
Outstanding AP = 100 juta, kas keluar 98 juta. Aging menampilkan bruto — sedikit menyesatkan, tapi koreksi tarif tidak pernah menyentuh biaya proyek.

**[REKOMENDASI]** **Potong saat pembayaran.** Alasan: (a) sesuai praktik dan sesuai kapan bukti potong terbit; (b) koreksi kesalahan tarif hanya menyentuh jurnal pembayaran, tidak pernah mengganggu biaya proyek atau realisasi RAB yang sudah dilaporkan; (c) sejalan dengan `TriggerPayment` yang sudah ada di kosakata `internal/tax`.

`// TODO(tax-advisor): konfirmasi saat terutang PPh 23 / PPh 4(2) untuk termin kontraktor — apakah pemotongan saat pembayaran dapat diterima bila pengakuan biaya terjadi di periode pajak sebelumnya.`

**[KEPUTUSAN BISNIS BD-6.**

## G.3 Apakah W-11 perlu extension point withholding?

**[REKOMENDASI]** **Ya — seam-nya, bukan implementasinya.**

Konkretnya: layanan pembayaran AP menerima daftar **potongan (deductions)** — nol atau lebih baris `{account_code, amount, reason}` — yang dikredit selain kas. Di P0 daftar itu selalu kosong dan tidak ada UI-nya. Saat modul pajak datang, `TaxRuleResolver` yang sudah ada tinggal mengisi daftar itu; struktur jurnal, dokumen BKK, idempotency, dan alokasi tidak berubah sama sekali.

Biaya menyediakan seam ini sekarang: satu parameter slice yang selalu kosong. Biaya **tidak** menyediakannya: seluruh jalur pembayaran AP harus dibongkar saat PPh masuk, termasuk alokasi dan rekonsiliasi. Asimetrinya jelas.

---

# BAGIAN H — PEMBAYARAN

## Kasus yang harus ditangani

| Kasus | Perlakuan |
|---|---|
| **Bayar lunas** | Satu pembayaran, satu alokasi, outstanding → 0 |
| **Bayar sebagian** | Satu pembayaran, satu alokasi < outstanding. Kewajiban tetap hidup |
| **Beberapa pembayaran atas satu kewajiban** | N pembayaran, N alokasi ke kewajiban yang sama. Σ alokasi ≤ nilai kewajiban |
| **Satu pembayaran untuk beberapa kewajiban** | Satu pembayaran, N alokasi. Σ alokasi = nilai pembayaran. Ini kasus normal — transfer mingguan ke satu vendor melunasi 3 termin |
| **Outstanding** | Turunan: nilai kewajiban − Σ alokasi non-terbalik. **Bukan kolom tersimpan** |
| **Lebih bayar** | Ditolak di P0 — lihat BD-12 |
| **Pembayaran ganda** | Dicegah idempotency key + row lock — lihat BAGIAN N |

## Bentuk jurnal

Satu pembayaran = **satu jurnal**, berapa pun jumlah alokasinya:

```
Dr  2-1000 Hutang Usaha        (Σ alokasi ke 2-1000)
Dr  2-1100 Hutang Retensi      (Σ alokasi retensi, bila ada)
    Cr  2-1200 Hutang PPh *)          (potongan, bila ada — P1)
    Cr  1-1xxx Bank                   (kas keluar aktual)          [BKK]
```

**[REKOMENDASI]** Satu pembayaran → satu jurnal → satu BKK. Bukan satu BKK per kewajiban. Alasannya INV-DOC-1: **satu pergerakan kas = satu dokumen**. Satu transfer bank adalah satu pergerakan, walaupun melunasi tiga termin.

## Dokumen

**[FAKTA]** `DefaultCashSpec` memilih BKK otomatis untuk kas keluar. `enforceDocument` menolak jurnal kas tanpa dokumen, fail-closed, di dalam transaksi.

**[REKOMENDASI]** **Tidak ada mekanisme nomor baru.** Tidak ada jenis dokumen baru. Pembayaran AP memakai BKK yang sudah ada, lewat engine W-2 yang sudah ada. Pemilik boleh saja meminta jenis dokumen AP tersendiri nanti — itu cukup baris master di `document_types`, tanpa sentuhan kode (**[FAKTA]** W-2).

---

# BAGIAN I — REVERSAL & PERIODE

## Membalik pengakuan hutang

**[REKOMENDASI]** Lewat `PostingService.Reverse` yang sudah ada. Menerbitkan JR, menunjuk jurnal asal, ledger tetap append-only. Kewajiban AP ditandai terbalik lewat **turunan** (jurnalnya punya reversal), bukan lewat kolom status.

**Prasyarat:** kewajiban yang sudah dibayar sebagian **tidak boleh** dibalik sebelum pembayarannya dibalik lebih dulu. Kalau tidak, alokasi akan menggantung ke kewajiban yang sudah tidak ada. Ini validasi service, bukan validasi ledger.

## Membalik pembayaran

**[REKOMENDASI]** Balik jurnal pembayarannya lewat `Reverse`, dan tandai alokasinya terbalik. Outstanding kewajiban naik kembali secara otomatis karena ia turunan dari Σ alokasi non-terbalik.

## Kalau periodenya sudah tutup

**[FAKTA]** `IsPeriodClosed` (`internal/ledger/repository.go:176`) dicek di `Create` dan di `reverseWithin`, pada **tanggal reversal**.

**[REKOMENDASI]** Warisi apa adanya. Koreksi terhadap periode tertutup dilakukan dengan menanggalkan jurnal pembalik di periode terbuka. Tidak ada pengecualian untuk AP, dan tidak ada pengecekan periode tambahan di service W-11 — menambahkannya akan menciptakan sumber kebenaran kedua tentang "periode mana yang tertutup".

## Bagaimana sub-ledger AP tetap terikat GL

Lihat BAGIAN J — jawabannya rekonsiliasi kupas-lapis per `journal_entries.source`, dengan reversal diatribusikan ke source yang dibaliknya (pola W-7 yang sudah terbukti).

## Status

**[REKOMENDASI]** **Tidak ada kolom status tersimpan** di kewajiban AP. Status diturunkan:

| Status | Rumus |
|---|---|
| `reversed` | jurnal pengakuannya punya reversal |
| `paid` | Σ alokasi non-terbalik == nilai kewajiban |
| `partial` | 0 < Σ alokasi < nilai kewajiban |
| `overdue` | outstanding > 0 dan `due_date` < hari ini |
| `outstanding` | selebihnya |

Ini konsisten dengan `receivable.EffectiveStatus` di sisi piutang (**[FAKTA]** W-4) dan dengan instruksi pemilik: jangan buat status tersimpan yang bisa bertentangan dengan ledger.

---

# BAGIAN J — SUB-LEDGER AP

## Apakah AP butuh sub-ledger?

**[REKOMENDASI]** **Ya.** Alasannya bukan saldo — saldo sudah ada di GL. Alasannya adalah **atribut yang tidak bisa disimpan GL**: siapa vendornya, nomor invoice vendornya, kapan jatuh temponya, termin ke berapa, retensi berapa. Tanpa itu, aging AP mustahil dan pembayaran tidak bisa dialokasikan.

## Sumber kebenaran — dan kenapa hanya ada satu

| Pertanyaan | Sumber kebenaran |
|---|---|
| Berapa total hutang usaha perusahaan? | **GL** — saldo 2-1000 + 2-1100 |
| Berapa hutang ke vendor X? | **Sub-ledger AP** |
| Kapan jatuh tempo? | **Sub-ledger AP** |
| Berapa outstanding kewajiban ini? | **Sub-ledger AP** (turunan alokasi) |
| Apakah biaya proyek sudah tercatat? | **GL** — lewat `ActualCostByCode` |

**[REKOMENDASI]** Tidak ada satu angka pun yang punya dua sumber. Sub-ledger **tidak** menyimpan total hutang perusahaan; GL **tidak** menyimpan vendor. Yang dijamin adalah keduanya cocok — lewat rekonsiliasi, bukan lewat duplikasi.

## INV-AP-1 — invariant tie-out yang benar

**[TEMUAN — koreksi terhadap rumusan naif]** Merumuskan "AP outstanding == saldo GL 2-1000" sebagai kesamaan mutlak **akan gagal pada jurnal manual sah pertama** (A.17) dan pada saldo awal (A.16). Rumusan yang benar mengikuti pola W-7:

> **INV-AP-1:** Untuk setiap tenant dan setiap tanggal *asOf*:
> `Σ outstanding(kewajiban AP)` == `Σ pergerakan 2-1000 dan 2-1100 yang bersumber dari {ap_invoice, ap_payment}`, dengan reversal diatribusikan ke source yang dibaliknya.
>
> Selisih terhadap saldo GL total **wajib dapat dijelaskan seluruhnya** oleh: saldo awal (`opening_balance`), hutang lama (`legacy_ap`, bila ada), dan jurnal manual (`manual`). Selisih yang tak terjelaskan = pelanggaran.

**[FAKTA]** Mesin untuk ini **sudah ada**: `LedgerBalanceService.AccountBalance(code, asOf)` dan `AccountMovementBySource(code, asOf)` (`internal/ledger/balance_service.go`), sudah terbukti dipakai W-7 untuk 1-2000. Tidak ada mesin baru yang perlu ditulis.

## Cakupan yang harus masuk rekonsiliasi

| Komponen | Perlakuan |
|---|---|
| Kewajiban AP | Sumber `ap_invoice` |
| Alokasi pembayaran | Sumber `ap_payment` |
| Reversal | Diatribusikan ke source asalnya, tidak jadi lapisan sendiri |
| Saldo awal | Lapisan `opening_balance` — dilaporkan, tidak dipaksa cocok |
| Hutang lama (legacy) | Lapisan `legacy_ap` — buku pembantu, **tanpa jurnal** (pola INV-LAR-4) |
| Hutang retensi | Ikut sub-ledger, di akun 2-1100 |
| Jurnal manual | Lapisan `manual` — dilaporkan sebagai selisih terjelaskan |

---

# BAGIAN K — AGING AP

## Kolom laporan

| Kolom | Sumber |
|---|---|
| Vendor | `vendors.name` (atau snapshot teks untuk baris lama) |
| Proyek | `cost_entries.project_id` → nama proyek; kosong untuk overhead |
| Nomor invoice | invoice vendor |
| Tanggal invoice | tanggal kewajiban |
| Jatuh tempo | `due_date` |
| Outstanding | turunan |
| Current / 1–30 / 31–60 / 61–90 / >90 | bucket |

## Konsistensi dengan W-4

**[FAKTA]** `internal/receivable` adalah satu-satunya mesin bucketing di repo (INV-AR-1), dan ia package murni tanpa DB.

**[REKOMENDASI]** **Pakai ulang aturan bucket-nya, jangan tulis yang kedua.** Konkretnya: ekspor primitif bucketing dari `receivable` (`DaysBetween` sudah publik; `bucketFor` perlu diekspor) dan konsumsi dari sisi AP. Laporan AP tetap punya struct sendiri dengan kolom bervendor — karena vendor bukan pelanggan dan memaksakan `receivable.Row` untuk AP akan memalsukan semantiknya.

Satu **aturan** bucketing, dua **laporan**. Kalau nanti pemilik mengubah definisi ">90 hari", ia berubah di satu tempat untuk piutang dan hutang sekaligus.

**Alternatif yang ditolak:** package `payable` dengan enum bucket sendiri. Terlihat lebih rapi, tapi melanggar semangat INV-AR-1 dan menjamin kedua laporan akan menyimpang diam-diam.

---

# BAGIAN L — UI / UX

## Penempatan navigasi

**[FAKTA]** Grup KEUANGAN di `frontend/components/layout/Sidebar.tsx:65-136` saat ini: Invoice Customer, Piutang Customer, Jadwal Penagihan, Penagihan, Titipan Notaris (warisan), Pengeluaran, Jurnal, Daftar Akun, Saldo Awal, Periode Akuntansi, Pajak, Laporan.

**[REKOMENDASI]** Sisipkan **Hutang Usaha** setelah *Pengeluaran* — bersebelahan dengan sisi kas-keluar, dan simetris dengan *Piutang Customer* di sisi atas.

## L.1 Dashboard AP

```
┌──────────────────────────────────────────────────────────────────────┐
│  Hutang Usaha                                    [+ Tagihan Baru]    │
├──────────────────────────────────────────────────────────────────────┤
│  ┌────────────┐ ┌────────────┐ ┌────────────┐ ┌────────────┐        │
│  │ Total      │ │ Jatuh tempo│ │ Jatuh tempo│ │ Retensi    │        │
│  │ Hutang     │ │ ≤ 7 hari   │ │ TERLAMBAT  │ │ Ditahan    │        │
│  │ Rp 1,2 M   │ │ Rp 340 jt  │ │ Rp 85 jt   │ │ Rp 60 jt   │        │
│  └────────────┘ └────────────┘ └────────────┘ └────────────┘        │
│                                                                      │
│  Aging                                                               │
│  Vendor              Current    1-30    31-60   61-90    >90   Total │
│  PT Konstruksi G.    120.000    45.000       -       -      -  165jt │
│  CV Bangun Perkasa    30.000       -   25.000       -      -   55jt  │
│  ...                                                                 │
│                                                                      │
│  Jatuh tempo terdekat                                    [lihat >]   │
│  • PT Konstruksi Gemilang — INV/2026/0412 — 18 Agu — Rp 45.000.000   │
└──────────────────────────────────────────────────────────────────────┘
```

**Empty state (wajib informatif + CTA):**
> **Belum ada hutang usaha.**
> Hutang usaha lahir saat tagihan atau termin dari vendor dicatat. Biaya proyek yang dibayar langsung lewat bank tidak menciptakan hutang.
> `[+ Catat Tagihan Vendor]`   `[Kelola Vendor]`

## L.2 Daftar hutang

Filter: vendor, proyek, status (outstanding / partial / overdue / paid / reversed), rentang jatuh tempo.
Kolom: Vendor · Proyek · No. Invoice · Tgl Invoice · Jatuh Tempo · Nilai · Terbayar · **Outstanding** · Status.
Baris terlambat ditandai dengan token warna, bukan tailwind mentah (aturan Design System Warm Ledger).

## L.3 Vendor

Daftar + form: nama, NPWP, alamat, rekening bank, termin pembayaran (hari), status aktif.
Detail vendor menampilkan: total outstanding, riwayat tagihan, riwayat pembayaran.

## L.4 Detail kewajiban

```
┌──────────────────────────────────────────────────────────────────────┐
│  ← Hutang Usaha                                                      │
│  PT Konstruksi Gemilang — INV/2026/0412          [Bayar] [Balikkan]  │
├──────────────────────────────────────────────────────────────────────┤
│  Tgl Invoice  12 Jul 2026        Jatuh Tempo  18 Agu 2026 (4 hari)   │
│  Proyek       Griya Asri Fase 1  Termin       Termin 3 (60%)         │
│                                                                      │
│  Nilai bruto                                        Rp 100.000.000   │
│  Retensi 5%                                        (Rp   5.000.000)  │
│  ──────────────────────────────────────────────────────────────────  │
│  Hutang usaha                                       Rp  95.000.000   │
│  Terbayar                                          (Rp  45.000.000)  │
│  OUTSTANDING                                        Rp  50.000.000   │
│                                                                      │
│  Rincian Biaya                                                       │
│  Item RAB              Kategori         Tier         Nilai           │
│  Struktur Blok A       Hard Cost        direct       80.000.000      │
│  Finishing Blok A      Hard Cost        direct       20.000.000      │
│                                                                      │
│  Riwayat Pembayaran                                                  │
│  02 Agu 2026  BKK/2026/000341  BCA 1234  Rp 45.000.000   [lihat]     │
│                                                                      │
│  Jurnal                                                              │
│  JE #5981 · 12 Jul 2026 · terposting                     [lihat]     │
└──────────────────────────────────────────────────────────────────────┘
```

## L.5 Alur pembayaran

**[REKOMENDASI]** Dua langkah, mencerminkan modal pembayaran pelanggan yang sudah ada (Payment Allocation FE-1):

**Langkah 1 — Input:** vendor, tanggal, rekening kas/bank (`CashBankSelect` yang sudah ada, COA-driven), nominal, catatan.
**Langkah 2 — Pratinjau:** alokasi otomatis ke kewajiban tertua lebih dulu (dapat diubah manual), plus pratinjau jurnal yang **dihasilkan fungsi yang sama** dengan yang akan memposting (pola `resolveDebitCreditCodes`). Baru lalu Simpan.

Pratinjau tidak boleh dihitung ulang di frontend. Ini aturan yang sudah berlaku di seluruh repo.

## L.6 Integrasi dengan Proyek → Biaya/Termin

**[REKOMENDASI]** Halaman proyek menampilkan biaya dari **sumber yang sama** — `cost_entries`, dibaca lewat reader realisasi yang sudah ada. Baris yang berasal dari AP diberi penanda dan tautan ke kewajibannya:

```
Tgl        Kategori    Vendor              Nilai         Sumber
12 Jul     Hard Cost   PT Konstruksi G.    80.000.000    Termin 3 →
05 Jul     Hard Cost   CV Bangun Perkasa   15.000.000    Bank
```

**Tidak ada sumber kebenaran kedua.** Halaman proyek tidak menjumlahkan ulang dari tabel AP; halaman AP tidak menjumlahkan ulang biaya proyek. Keduanya membaca `cost_entries` / ledger, masing-masing dengan lensa berbeda. Inilah alasan struktural kenapa baris AP harus mendarat di `cost_entries` (T-2).

---

# BAGIAN M — PERMUKAAN API

Usulan minimal. **Tidak ada implementasi.**

## P0 — benar-benar dibutuhkan

| Method | Path | Alasan wajib |
|---|---|---|
| `GET` | `/vendors` | dropdown & daftar |
| `POST` | `/vendors` | tanpa ini AP tak punya identitas vendor |
| `PATCH` | `/vendors/{id}` | koreksi NPWP/rekening tanpa kehilangan histori |
| `GET` | `/ap/invoices` | daftar hutang + filter |
| `POST` | `/ap/invoices/preview` | pratinjau jurnal — pola wajib di repo ini |
| `POST` | `/ap/invoices` | pengakuan hutang |
| `GET` | `/ap/invoices/{id}` | detail + riwayat |
| `POST` | `/ap/payments` | pembayaran + alokasi, ber-idempotency-key |
| `POST` | `/ap/invoices/{id}/reverse` | koreksi |
| `POST` | `/ap/payments/{id}/reverse` | koreksi pembayaran |
| `GET` | `/ap/aging` | alasan utama AP ada |

## P1 — penting, bisa menyusul

| Method | Path | Catatan |
|---|---|---|
| `GET` | `/ap/reconciliation` | tie-out INV-AP-1 kupas-lapis |
| `POST` | `/ap/advances` | uang muka vendor |
| `POST` | `/ap/retentions/{id}/release` | pelepasan retensi |
| `GET` | `/vendors/{id}/statement` | statement vendor |

## P2 — ditunda

`/ap/contracts` (kontrak vendor & sisa nilai kontrak), `/ap/legacy` (impor hutang lama), `/ap/accruals` (akrual cut-off), endpoint approve tersendiri.

## Yang sengaja TIDAK ada

**[REKOMENDASI]** **Tidak ada** `POST /ap/invoices/{id}/approve`. Approval bukan milik W-11 — `approval.RequireApproved` sudah ada, OPT-IN, dan `TargetPayment` sudah ada di kosakata (**[FAKTA]**). W-11 cukup memanggil gate itu di jalur pembayaran. Membuat endpoint approve sendiri = mesin approval kedua.

---

# BAGIAN N — IDEMPOTENCY & CONCURRENCY

Semua pola di bawah ini **sudah ada di repo** dan tinggal dicerminkan.

| Risiko | Penanganan | Preseden |
|---|---|---|
| **Double submit pembayaran** | `Idempotency-Key` header → lookup **di dalam** transaksi; kunci sama + target sama → kembalikan hasil lama; kunci sama + target beda → `ErrIdempotencyConflict` (HTTP 409) | `charge.RecordPayout` (`internal/charge/service.go:618-730`) |
| **Nomor invoice vendor ganda** | Unique key `(tenant_id, vendor_id, invoice_number)` — **atau** peringatan lunak. Lihat BD-8 | — |
| **Pembayaran bersamaan atas kewajiban yang sama** | `SELECT ... FOR UPDATE` pada baris kewajiban sebelum menghitung outstanding, di dalam transaksi | `Clauses(gormLockingUpdate())` di `charge.RecordPayout` |
| **Lebih bayar** | Σ alokasi dihitung **setelah** row lock, di dalam transaksi yang sama; melebihi outstanding → tolak | Increment 5 (row-lock pattern) |
| **Nomor dokumen bentrok** | Sudah ditangani engine W-2 (`document_sequences` `last_val`, forward-only) | **[FAKTA]** W-2 |
| **Jurnal tak seimbang** | `validateLines` menolak sebelum commit | `PostingService` |
| **Kas tanpa dokumen** | `enforceDocument` fail-closed di dalam transaksi | **[FAKTA]** INV-DOC-1 |
| **Webhook/request ganda (integrasi masa depan)** | Idempotency key yang sama sudah cukup — tidak perlu mekanisme baru | — |

**[REKOMENDASI]** Satu transaksi atomik per pembayaran: lock kewajiban → hitung outstanding → validasi alokasi → posting jurnal → terbitkan BKK → simpan pembayaran + alokasi. Kalau ada satu yang gagal, tidak ada yang tersisa. Ini persis aturan FE-2: **tidak ada jurnal tanpa alokasi**.

---

# BAGIAN O — AUDIT DATA EXISTING

Seluruh audit ini **read-only** (`SELECT` / `SHOW`). Tidak ada `INSERT`/`UPDATE`/`DELETE`, tidak ada transaksi simulasi.

## O.1 Semua `cost_entries` dengan `payment_method='payable'`

**[FAKTA]** **4 baris. Seluruh database.**

| Tenant | ID | Proyek | Tier / Kategori | Nilai | Vendor | JE |
|---|---|---|---|---|---|---|
| 1 (esaProperti Dev) | 1 | 1 | shared / land | 5.000.000.000 | PT Agraria Nusantara | 1 |
| 1 (esaProperti Dev) | 2 | 1 | shared / hard | 8.000.000.000 | PT Konstruksi Gemilang | 2 |
| 9901005 (W5 UI Check) | 480 | 2446 | shared / land | 5.000.000.000 | PT Agraria Nusantara | 5975 |
| 9901005 (W5 UI Check) | 481 | 2446 | shared / hard | 8.000.000.000 | PT Konstruksi Gemilang | 5976 |

**[FAKTA]** Semuanya: sudah terposting, `source='system'`, `budget_item_id` **NULL**, dan berasal dari `cmd/demo-seed/main.go:255` yang memanggil `svc.CreateCostEntry` langsung — sehingga **melewati guard transport Tahap 1**.

## O.2 Tenant yang terdampak

**[FAKTA]** Hanya 2 tenant, keduanya **bukan tenant produksi**:
- Tenant 1 = `esaProperti Dev` — demo seed
- Tenant 9901005 = `W5 UI Check` — tenant uji UI

**[FAKTA — koreksi penamaan]** Pesan pemilik dan memori sebelumnya menyebut tenant 9900246 sebagai "NATA ALAM". Tabel `tenants` menunjukkan **`9900246 = "UAT Batch 2"`**, sementara ada tenant **terpisah** `9 = "Nata Alam"`.

**Klaim substantifnya tetap berlaku untuk keduanya:**
- Tenant 9900246 (`UAT Batch 2`) — tenant dengan volume tertinggi (5 cost entries, 40 journal entries, 16 unit): **0 payable**.
- Tenant 9 (`Nata Alam`): **0 payable**.

## O.3 Baris jurnal pada 2-1000

**[FAKTA]**

| Tenant | Jumlah baris terposting | Total Debit | Total Kredit |
|---|---:|---:|---:|
| 1 | 2 | **0,0000** | 13.000.000.000 |
| 9901005 | 2 | **0,0000** | 13.000.000.000 |
| lainnya | 0 | 0 | 0 |

**[TEMUAN]** **Nol debit ke 2-1000 di seluruh sejarah database, di semua tenant.** Saldo yang lahir dari jalur payable tidak pernah bisa mati. Ini konfirmasi empiris terhadap alasan W-11 ada.

## O.4 Nilai vendor

**[FAKTA]** 11 baris punya `vendor` tidak kosong. Tenant 9900246 punya 5 nama berbeda (`CV Bangun Perkasa`, `Konsultan Adi`, `pemilik lahan`, `PT Tanah Jaya`, `PT Udin`) — semuanya bank-paid, nol payable.

## O.5 Saldo awal & jurnal manual

**[FAKTA]** Sebaran `journal_entries.source`:
- Tenant 9900246: `system` × 40 saja.
- Tenant 9901005: `system` × 9, `opening_balance` × 2, `legacy_ar_payment` × 2, `reversal` × 1.

**[FAKTA]** Tidak ada saldo awal pada 2-1000 di tenant mana pun.

## O.6 Strategi

| Kategori | Strategi | Alasan |
|---|---|---|
| **Payable historis (tenant nyata)** | **Tidak ada.** Tidak ada datanya | Nol baris payable di tenant produksi |
| **Payable demo/seed (4 baris)** | **Jangan disentuh.** Ledger append-only — baris itu secara historis benar. Yang diperbaiki adalah `cmd/demo-seed` supaya tenant demo berikutnya melahirkan kewajiban AP yang bisa dilunasi | Menghapus jurnal terposting melanggar invariant #5 |
| **Payable tenant nyata** | Semua payable baru lahir lewat jalur W-11 dengan kewajiban, vendor, dan jatuh tempo | — |
| **Apakah perlu migrasi data?** | **Tidak.** Migrasi **schema** perlu (tabel baru + kolom nullable). Migrasi **data**/backfill: nol baris | Semua kolom baru nullable; tidak ada baris lama yang perlu diisi |
| **Apakah perlu cleanup?** | **Tidak untuk produksi.** Untuk tenant demo: opsional, dan kalau dilakukan harus lewat **jurnal pembalik**, bukan `DELETE` | Invariant #5 |
| **Tie-out saldo awal** | Untuk tenant yang mulai dengan hutang berjalan: saldo awal 2-1000 (tanpa dokumen, sudah diizinkan) + rincian buku pembantu **tanpa jurnal** (pola INV-LAR-4) | Persis W-7 |

**[TEMUAN]** Ini kondisi terbaik yang mungkin untuk membangun W-11: tidak ada beban data historis sama sekali di tenant produksi. Desain bisa dipilih atas dasar kebenaran, bukan atas dasar kompatibilitas ke belakang.

**[KEPUTUSAN BISNIS BD-10.**

---

# BAGIAN P — KETERGANTUNGAN ASET TETAP (W-13)

Tidak ada implementasi Fixed Asset di sini. Yang dijawab: apa yang W-11 **harus sediakan** supaya W-13 tidak perlu membongkarnya.

**[FAKTA]** COA sudah punya 1-4000 / 1-4100 / 1-4900 (aset tetap & akumulasi penyusutan).

**[TEMUAN]** Yang belum ada bukan akunnya — melainkan **jalur kas keluar yang mendebit sesuatu selain biaya**. Setiap jalur kas-keluar hari ini berbentuk beban (`cost_entries` butuh kategori+tier; `expenses` butuh jenis beban). Pembelian aset (`Dr 1-4000 / Cr Bank`) hari ini hanya mungkin lewat jurnal manual.

**Yang W-11 wajib sediakan:**

| Kebutuhan W-13 | Disediakan W-11 |
|---|---|
| **Vendor** | Master vendor (BAGIAN E) — sama persis, tanpa perubahan |
| **Hutang ke vendor** | Kewajiban AP — pembelian aset kredit = kewajiban AP yang akun debitnya 1-4000 |
| **Uang muka** | `1-5300 Uang Muka Vendor` + mekanisme kompensasi (BAGIAN F) — identik untuk aset |
| **Kas keluar non-beban** | Penyelesaian kewajiban generik: akun debit dari kewajiban, bukan dari taksonomi biaya |
| **Dokumen** | BKK — sama, tanpa jenis dokumen baru |
| **Aging & outstanding** | Sama, tanpa perubahan |

**[REKOMENDASI — seam kritis]** Kewajiban AP harus membawa **resolusi akun debitnya sendiri**, bukan mewarisinya dari taksonomi biaya. Polanya persis `expense_types.expense_account_code` di W-10: akun ditentukan data, bukan `switch` di kode.

Dengan seam itu:
- Termin kontraktor → akun debit `1-3100` (WIP), di-tag proyek → masuk realisasi RAB.
- Beban kantor → akun debit `5-xxxx`, tanpa proyek → tidak masuk RAB.
- **Pembelian aset (W-13)** → akun debit `1-4000`, tanpa proyek → tidak masuk RAB, tidak masuk laba rugi.

Ketiganya jalur kode yang sama. W-13 menambah **data**, bukan mesin.

**Kalau seam ini tidak dibuat sekarang:** W-13 harus membangun jalur pembelian + pembayaran + dokumen + idempotency sendiri, atau membongkar W-11. Biaya menyediakannya sekarang: memilih kolom akun di tabel kewajiban alih-alih menurunkannya dari kategori. Praktis nol.

---

# BAGIAN Q — KEPUTUSAN BISNIS YANG DIBUTUHKAN

**Rekomendasi Claude di bawah ini BUKAN keputusan.** Semua harus dijawab pemilik/klien sebelum coding.

### BD-1 — Titik pengakuan hutang ✅ **DIJAWAB PEMILIK (C-2)**
**Keputusan:** hutang lahir saat tagihan/termin **sudah disetujui dan kewajiban kepada vendor resmi diakui** — bukan saat pembayaran.
**Pemetaan ke kode existing:** lihat **U.0** — siklus `Create` (draft) → `RequireApproved` → `IsPeriodClosed` → `Post` sudah ada seluruhnya; audit trail di `approval_requests` + `approval_actions` (append-only) + snapshot beku. Nol mesin baru.
**Turunan yang masih terbuka:** BD-14.

### BD-2 — `budget_item_id` untuk biaya AP proyek
Wajib, atau opsional-dianjurkan?
**[REKOMENDASI]** Opsional tapi didorong kuat di UI. Konsekuensi kalau kosong: biaya tetap muncul di realisasi proyek, tapi **hilang dari drill-down per item RAB** — pemilik harus sadar trade-off ini.

### BD-3 — Retensi ✅ **SEBAGIAN DIJAWAB (C-3)**
**Keputusan:** retensi didukung sejak awal W-11; biaya/WIP tetap bruto; kewajiban pembayaran langsung dan kewajiban retensi harus dapat dibedakan. Jurnal & saldo per tahap: **U.3**.
**Masih terbuka — (b) MEMBLOKIR CODING:** akun kewajiban terpisah, atau tetap di 2-1000 dengan atribut? Bahan keputusan lengkap di **U.5**. Konsekuensi bila digabung: aging melaporkan retensi yang sah ditahan sebagai terlambat bayar.
**Masih terbuka:** (c) persentase default, (d) pemicu pelepasan — tanggal atau persetujuan manual.

### BD-4 — Akun COA baru ⏳ **BELUM FINAL (C-4) — MEMBLOKIR CODING**
Audit COA lengkap ada di **U.5**: dari 8 kebutuhan, **5 sudah tercukupi** oleh COA existing (2-1000, kas/bank COA-driven, 1-5100, akun taksonomi proyek, akun beban overhead). Gap sesungguhnya **3 akun**, dengan alasan penolakan per kandidat existing.
**Pertanyaan:** setujui 1 akun (retensi, P0), 2 akun (+uang muka vendor, P1), atau 3 akun (+hutang PPh dipotong, bergantung BD-6)?
Tanpa migrasi/seed akun sebelum keputusan ini.

### BD-5 — Master vendor vs teks bebas ✅ **DIJAWAB PEMILIK (C-5)**
**Keputusan:** Vendor Master minimal ber-tenant sebagai source of truth, bukan VARCHAR bebas. `vendor VARCHAR(200)` tetap diperhitungkan untuk kompatibilitas historis. Jangan over-engineer.
**Field final & perlakuan data lama:** **U.6**. Satu usulan tambahan di luar daftar pemilik (`bank_account_name`) — boleh dicoret tanpa konsekuensi.

### BD-6 — PPh 23 / PPh 4(2)
(a) Apakah perusahaan memotong PPh atas termin kontraktor? (b) Kalau ya, PPh 23 atau 4(2) — tergantung kualifikasi kontraktor? (c) Dipotong saat pengakuan atau saat pembayaran?
**[REKOMENDASI]** Saat pembayaran, lewat seam potongan. **Tarif dan kualifikasi tidak ditebak Claude** — `// TODO(tax-advisor)`.

### BD-7 — PPN Masukan
Apakah W-11 P0 sudah mencatat PPN Masukan dari Faktur Pajak vendor, atau ditunda ke modul pajak?
**[REKOMENDASI]** Sertakan di P0 sebagai field opsional (`ppn_amount` + `faktur_pajak_number`). Biayanya satu baris jurnal; manfaatnya `VATReport` berhenti kosong sebelah.

### BD-8 — Nomor invoice ganda
Nomor invoice vendor yang sama: **tolak keras** (unique key) atau **peringatkan** dan izinkan lanjut?
**[REKOMENDASI]** Peringatan lunak di P0 — sebagian vendor mengulang nomor lintas tahun, dan penolakan keras akan memblokir input yang sah. Naikkan ke penolakan keras bila pemilik memang menegakkan penomoran unik.

### BD-9 — Approval pembayaran
Apakah pembayaran AP butuh persetujuan sebelum diposting? Kalau ya, ambang nilainya berapa dan siapa penyetujunya?
**[REKOMENDASI]** Pakai `approval.RequireApproved` + `TargetPayment` yang sudah ada, sifatnya OPT-IN — tenant yang tidak mengaktifkan workflow tidak terkena gate. Nol mesin baru.

### BD-10 — Hutang historis
Apakah ada hutang berjalan dari sebelum sistem yang harus diimpor? (Audit menunjukkan **nol** di tenant produksi.)
**[REKOMENDASI]** Kalau tidak ada: lewati sepenuhnya. Kalau ada: pola W-7 — buku pembantu tanpa jurnal, saldo GL lewat saldo awal. P2.

### BD-11 — Pembayaran sebagian
Apakah pembayaran sebagian diizinkan bebas, atau harus sesuai jadwal termin?
**[REKOMENDASI]** Bebas. Realitas lapangan properti penuh pembayaran sebagian, dan membatasinya hanya memaksa orang memalsukan angka.

### BD-12 — Lebih bayar ke vendor
Kalau pembayaran melebihi outstanding: **tolak**, atau **terima** dan catat sebagai uang muka/piutang vendor?
**[REKOMENDASI]** Tolak di P0. Terima sebagai uang muka vendor (1-5300) di P1 — mekanismenya sudah tersedia dari BAGIAN F. Catatan: ini **berbeda** dari sisi pelanggan, di mana lebih bayar pra-BAST diizinkan sebagai saldo kredit — karena kelebihan bayar ke vendor menciptakan **aset**, bukan kewajiban.

### BD-13 — Entitas kontrak vendor
Apakah W-11 perlu melacak nilai kontrak dan sisa nilai yang belum ditagih, atau cukup tagihan lepas dengan referensi kontrak berupa teks?
**[REKOMENDASI]** Teks bebas di P0; entitas `vendor_contracts` di P1 bila kontrol over-billing memang dibutuhkan.

### BD-14 — Posting langsung tanpa workflow aktif *(baru, turunan BD-1)*
Tenant yang **belum** mengonfigurasi workflow approval: boleh memposting kewajiban AP langsung (draft → post tanpa langkah approve manual), atau tetap wajib ada langkah persetujuan eksplisit?
**[REKOMENDASI]** Boleh langsung — konsisten dengan sifat **OPT-IN** `RequireApproved` yang sudah berlaku di seluruh repo. Governance bertahap, tenant kecil tidak terblokir.

### BD-15 — Perlakuan payable warisan pra-W-11 *(baru)*
Untuk saldo 2-1000 yang lahir sebelum W-11 (kini hanya di tenant demo): (A) biarkan sebagai lapisan `system` di rekonsiliasi, (B) adopsi jadi kewajiban AP tanpa jurnal (pola W-7), atau (C) balik dengan jurnal pembalik lalu catat ulang?
**[REKOMENDASI]** A sekarang — nol tenant produksi terdampak; B disiapkan sebagai jalur P2. Rincian di **U.8**.

---

# BAGIAN R — GRAPH DEPENDENSI

## Graph yang diusulkan pemilik

```
W-10 Expense → W-11 AP → {AP Aging, PPN Masukan, Withholding, Vendor payment, Project cost/RAB}
             → W-12 Bank Reconciliation → W-13 Fixed Asset
```

## Koreksi berdasarkan audit

**[TEMUAN R-a] `Project cost / RAB` bukan keluaran W-11 — ia sudah ada dan tidak berubah.**
`GetRealisasiByProject` membaca ledger (T-1). Begitu AP memposting jurnalnya, realisasi RAB bertambah **tanpa kode apa pun di W-11**. Menempatkannya sebagai dependen mengesankan ada pekerjaan di sana; tidak ada. Yang benar: RAB adalah **konsumen otomatis**, bukan dependen.

**[TEMUAN R-b] `AP Aging` bergantung pada W-4, bukan hanya pada W-11.**
Mesin bucketing ada di `internal/receivable` (INV-AR-1). Aging AP mewarisi aturan itu; ia bukan turunan murni W-11.

**[TEMUAN R-c] W-11 bergantung pada W-2 dan W-3, bukan hanya pada W-10.**
Setiap pembayaran AP adalah pergerakan kas → wajib BKK (INV-DOC-1, fail-closed). Tanpa W-2/W-3, pembayaran AP tidak bisa diposting sama sekali. Ini dependensi keras yang hilang dari graph asli.

**[TEMUAN R-d] W-13 bergantung pada W-11, bukan pada W-12.**
Aset tetap butuh: vendor, hutang vendor, uang muka, kas keluar non-beban — semuanya dari W-11 (BAGIAN P). Rekonsiliasi bank **tidak** dibutuhkan untuk mencatat pembelian aset. W-12 dan W-13 sebenarnya **independen** satu sama lain; keduanya hanya sama-sama butuh W-11.

**[TEMUAN R-e] W-12 bergantung pada W-11 lewat kelengkapan, bukan lewat kode.**
Rekonsiliasi bank mencocokkan pergerakan kas terhadap rekening koran. Selama pembayaran vendor belum tercatat di sistem, rekonsiliasi akan selalu punya selisih tak terjelaskan. Jadi W-11 mendahului W-12 karena **kelengkapan data**, bukan karena ketergantungan API.

**[TEMUAN R-f] Withholding bergantung pada modul pajak, bukan pada W-11.**
W-11 hanya menyediakan **seam** (daftar potongan di pembayaran). Aturan dan tarifnya milik `internal/tax` dan `TaxRuleResolver` yang sudah ada.

## Graph terkoreksi

```
                    W-2 Numbering ──┐
                    W-3 INV-DOC-1 ──┤  (dependensi keras: kas wajib berdokumen)
                    W-4 Aging ──────┤  (dependensi aturan: bucket dipakai ulang)
                    W-10 Expense ───┤  (dependensi pola: taksonomi & INV-EXP-2)
                    W-7 legacyar ───┘  (dependensi pola: sub-ledger tanpa jurnal)
                            │
                            v
                    ┌───────────────┐
                    │   W-11  AP    │
                    │ Vendor        │
                    │ Kewajiban     │
                    │ Pembayaran    │
                    │ Alokasi       │
                    └───────┬───────┘
                            │
        ┌───────────────┬───┴────────┬──────────────┬──────────────┐
        v               v            v              v              v
   AP Aging       Rekonsiliasi   Seam potongan   W-12 Bank    W-13 Fixed
   (pakai W-4)    AP↔GL          (isi: pajak)    Reconcile    Asset
                  (pakai W-7)                    (kelengkapan) (pakai seam
                                                               kas non-beban)

   Realisasi RAB / Biaya proyek  ← konsumen OTOMATIS, nol perubahan kode
```

W-12 dan W-13 **paralel** — tidak ada yang memblokir yang lain.

---

# BAGIAN S — RUANG LINGKUP W-11

## P0 — wajib supaya AP benar-benar terpakai

| # | Item | Alasan wajib |
|---|---|---|
| 1 | Master vendor (CRUD, ber-tenant) | tanpa ini aging salah diam-diam |
| 2 | Kewajiban AP (header + baris biaya ke `cost_entries`) | inti modul; T-2 memaksa baris mendarat di `cost_entries` |
| 3 | Pengakuan hutang + pratinjau jurnal | pratinjau adalah pola wajib repo ini |
| 4 | Pembayaran + alokasi + BKK + idempotency | tanpa ini 2-1000 tetap akun satu arah |
| 5 | Outstanding & status **turunan** | tidak ada status tersimpan |
| 6 | Aging AP (pakai ulang aturan bucket W-4) | alasan utama AP ada |
| 7 | Reversal kewajiban & reversal pembayaran | koreksi wajib ada sejak hari pertama |
| 8 | Retensi (bila BD-3 = ya) | termin konstruksi hampir selalu ada retensi |
| 9 | PPN Masukan opsional (bila BD-7 = ya) | 1 baris jurnal, menghidupkan `VATReport` |
| 10 | Seam daftar potongan (kosong di P0) | mencegah pembongkaran saat pajak masuk |
| 11 | UI: dashboard, daftar, vendor, detail, modal bayar | tanpa UI, backend tak terpakai |
| 12 | Penanda sumber AP di halaman biaya proyek | mencegah sumber kebenaran kedua |
| 13 | Test: INV-AP-1, INV-AP-2, INV-AP-3, isolasi tenant, periode tutup, idempotency | gerbang "fase selesai" |

## P1 — penting, bisa menyusul

Uang muka vendor + kompensasi · Pelepasan retensi ber-alur sendiri · Laporan rekonsiliasi AP↔GL · Statement vendor · Gate approval pembayaran · Entitas kontrak vendor & sisa nilai kontrak · Lebih bayar → uang muka.

## P2 — masa depan

Impor hutang lama (pola W-7) · Akrual cut-off periode ke 2-6000 · Jenis dokumen AP tersendiri · Normalisasi/merge vendor lama · Withholding penuh (menunggu modul pajak) · Jadwal termin kontrak.

## Dikecualikan tegas dari W-11

| Dikecualikan | Alasan |
|---|---|
| **Purchase Order** | Kontrol pengadaan, bukan akuntansi. Tidak ada angka laporan keuangan yang salah tanpanya |
| **Modul procurement** | Di luar bisnis developer properti — pemilik menyatakan ini eksplisit |
| **GRN / penerimaan barang** | Butuh inventory. Developer properti membeli jasa konstruksi, bukan stok |
| **Inventory** | Tidak ada persediaan barang dagang dalam model bisnis ini |
| **e-Faktur** | Integrasi DJP, modul tersendiri |
| **Pelaporan pajak lengkap** | `internal/tax` yang punya. W-11 hanya menyediakan seam |
| **Impor rekening koran** | W-12 |
| **Fixed Asset** | W-13. W-11 hanya menyediakan fondasinya (BAGIAN P) |
| **Bank Reconciliation** | W-12 |

**[TEMUAN]** Audit **tidak** menemukan satu pun dari daftar di atas yang mutlak diperlukan untuk correctness W-11. Semua jurnal AP bisa benar, seimbang, berdokumen, ter-aging, dan terrekonsiliasi tanpa satu pun dari mereka.

---

# BAGIAN T — REKOMENDASI FINAL

## T.1 Verdict

**W-11 layak dibangun dan sebaiknya dibangun kecil.** Ia menutup lubang nyata dan terverifikasi: 2-1000 hari ini adalah akun satu arah — nol debit sepanjang sejarah database. Fondasinya sudah lengkap; W-11 sebagian besar adalah **perakitan mesin yang sudah ada**, bukan penemuan mesin baru. Yang menahan bukan teknis melainkan 13 keputusan bisnis di BAGIAN Q.

## T.2 Rekomendasi arsitektur

Package baru `internal/payable`, mengikuti konvensi repo (`repository.go` / `service.go` / `handler.go`), dengan aturan:

- **Tidak** membuat mesin aging kedua — pakai ulang aturan bucket `internal/receivable`.
- **Tidak** membuat mesin penomoran kedua — pakai `internal/document` lewat `ledger.DocumentSpec`.
- **Tidak** membuat mesin approval kedua — panggil `approval.RequireApproved`.
- **Tidak** membuat guard periode kedua — warisi dari `PostingService`.
- **Tidak** mengubah `GetRealisasiByProject`, `GetRealisasiPerItem`, atau reader W-5/W-6 apa pun.
- **Menulis** baris biaya AP ke `cost_entries` (konsekuensi T-2), bukan ke tabel baris paralel.
- Semua pemilihan akun lewat `AccountRoleRegistry`; tidak ada `code IN (...)` atau `LIKE`.

## T.3 Alur akuntansi

**Termin kontraktor dengan retensi 5% (contoh pemilik, Rp100 juta):**
```
Pengakuan   Dr 1-3100 WIP [proyek X]  100.000.000
              Cr 2-1000 Hutang Usaha        95.000.000
              Cr 2-1100 Hutang Retensi       5.000.000
Pembayaran  Dr 2-1000 Hutang Usaha     95.000.000
              Cr 1-1xxx Bank                95.000.000   [BKK]
Retensi     Dr 2-1100 Hutang Retensi    5.000.000
              Cr 1-1xxx Bank                 5.000.000   [BKK]
```

**Tanpa retensi (bentuk paling sederhana — persis contoh pemilik):**
```
Pengakuan   Dr 1-3100 WIP [proyek X]  100.000.000
              Cr 2-1000 Hutang Usaha       100.000.000
Pembayaran  Dr 2-1000 Hutang Usaha    100.000.000
              Cr 1-1xxx Bank               100.000.000   [BKK]
```
**Contoh yang pemilik berikan ternyata benar apa adanya** — audit tidak menemukan alasan untuk mengubahnya. Retensi, PPN, dan PPh adalah **lapisan opsional di atasnya**, bukan koreksi terhadapnya.

**Dengan PPN Masukan:** tambah `Dr 1-5100 PPN Masukan` di jurnal pengakuan; hutang menjadi bruto.
**Dengan uang muka:** `Cr 1-5300` di jurnal pengakuan sebagai kompensasi.
**Dengan PPh (P1):** `Cr 2-1200` di jurnal pembayaran; kas keluar berkurang sebesar potongan.

## T.4 Rekomendasi model data

**Tabel baru (4):**

```
vendors
  id, tenant_id, name, npwp, address,
  bank_name, bank_account_name, bank_account_number,
  payment_terms_days, is_active, created_at, updated_at
  UNIQUE (tenant_id, name)   ← lihat BD-8 untuk kebijakan duplikat
  INDEX (tenant_id)

ap_invoices                              -- kewajiban / termin
  id, tenant_id, vendor_id, vendor_name_snapshot,
  invoice_number, invoice_date, due_date,
  contract_ref, progress_percent,
  gross_amount, retention_amount, ppn_amount, payable_amount,
  faktur_pajak_number,
  journal_entry_id, description,
  created_by, created_at, updated_at
  INDEX (tenant_id), INDEX (tenant_id, vendor_id), INDEX (tenant_id, due_date)

ap_payments
  id, tenant_id, vendor_id, payment_date,
  bank_account_code, amount,
  journal_entry_id, document_id,
  idempotency_key, notes,
  created_by, created_at, updated_at
  UNIQUE (tenant_id, idempotency_key)
  INDEX (tenant_id)

ap_payment_allocations
  id, tenant_id, ap_payment_id, ap_invoice_id,
  allocation_type,        -- invoice | retention | advance
  amount, reversed_at,
  created_at, updated_at
  INDEX (tenant_id), INDEX (tenant_id, ap_invoice_id)
```

**Kolom tambahan pada tabel existing (semua NULLABLE, nol backfill):**
```
cost_entries.vendor_id      BIGINT UNSIGNED NULL  FK vendors ON DELETE SET NULL
cost_entries.ap_invoice_id  BIGINT UNSIGNED NULL  FK ap_invoices ON DELETE SET NULL
```

**Yang sengaja TIDAK dibuat:** `ap_invoice_lines` (baris biaya ada di `cost_entries` — T-2), kolom `status` (turunan), kolom `outstanding` (turunan), `purchase_orders`, `goods_receipts`, `vendor_contracts` (P1).

Semua kolom uang `DECIMAL(20,4) NOT NULL DEFAULT '0.0000'`; semua tabel punya `tenant_id` + index; `created_at`/`updated_at` `DATETIME(3)`; migrasi lewat `backend/migrations/` (golang-migrate), tanpa AutoMigrate.

## T.5 Rekomendasi API

11 endpoint P0 (BAGIAN M). Tanpa endpoint approve. Pratinjau jurnal wajib memakai fungsi resolusi akun yang sama dengan jalur posting.

## T.6 Alur UI

Menu **Keuangan → Hutang Usaha** setelah *Pengeluaran*: dashboard (KPI + aging + jatuh tempo terdekat) → daftar hutang (filter vendor/proyek/status/jatuh tempo) → detail kewajiban (rincian nilai, rincian biaya per item RAB, riwayat pembayaran, tautan jurnal) → modal bayar dua langkah (Input → Pratinjau alokasi & jurnal → Simpan). Halaman vendor terpisah. Halaman biaya proyek menampilkan penanda sumber + tautan ke kewajiban — membaca sumber yang sama, bukan menjumlahkan ulang. Empty state informatif + CTA di semua halaman. Warna hanya lewat token design system.

## T.7 Invariant akuntansi

| Kode | Invariant | Cara uji |
|---|---|---|
| **INV-AP-1** | Σ outstanding kewajiban AP == Σ pergerakan 2-1000 & 2-1100 bersumber `{ap_invoice, ap_payment}`; selisih terhadap saldo GL total wajib terjelaskan penuh oleh `opening_balance` + `legacy_ap` + `manual` | integration: posting campuran, lalu `AccountMovementBySource` |
| **INV-AP-2** | Σ alokasi non-terbalik atas satu kewajiban ≤ nilai kewajiban. Selalu. Termasuk di bawah pembayaran konkuren | integration: N goroutine membayar kewajiban yang sama |
| **INV-AP-3** | Pembayaran AP tidak pernah mendebit maupun mengkredit akun taksonomi CostCategory | unit: periksa baris jurnal pembayaran terhadap `RoleCodeList` taksonomi |
| **INV-AP-4** | Realisasi RAB proyek sebelum dan sesudah pembayaran AP **identik** (uji anti-double-count langsung) | integration: snapshot `GetRealisasiByProject` sebelum/sesudah |
| **INV-AP-5** | Setiap jurnal pembayaran AP punya tepat satu dokumen BKK (warisan INV-DOC-1) | integration: coba posting tanpa dokumen → wajib gagal |
| **INV-AP-6** | Kewajiban dengan pembayaran non-terbalik tidak dapat dibalik | unit + integration |
| **INV-AP-7** | Isolasi tenant: query AP lintas tenant mengembalikan nol baris | integration (wajib per CLAUDE.md) |
| **INV-AP-8** *(Revisi 2 — penjaga langsung C-1)* | Untuk setiap kewajiban AP: `Σ cost_entries.amount` == `Σ debit ke akun taksonomi` pada jurnal pengakuannya. Retensi, PPN, dan offset uang muka **tidak boleh** mengurangi `amount` | integration: uji ketiga skenario U.2/U.3/U.4 |
| **INV-AP-9** *(Revisi 2)* | Kewajiban AP yang jurnalnya masih draft: nol di realisasi RAB, nol di saldo GL, nol di outstanding, nol di aging | integration: snapshot 4 pembacaan sebelum & sesudah `Post` |
| **INV-AP-10** *(Revisi 2)* | Draft yang tanggalnya jatuh di periode yang sudah tertutup **tidak dapat** di-post (gap U-2) | integration: buat draft → tutup periode → coba post → wajib gagal |

## T.8 Strategi migrasi

Satu migrasi (000073+): 4 tabel baru, 2 kolom nullable pada `cost_entries`, 2–3 akun COA baru (sesuai BD-4) + pendaftaran `AccountRoleRegistry`, dan baris `document_types` bila BD memilih jenis dokumen AP tersendiri.

**Migrasi data: NOL.** Tidak ada backfill, tidak ada `UPDATE`, tidak ada baris existing yang disentuh. Ini mungkin karena audit membuktikan tidak ada payable di tenant produksi mana pun (BAGIAN O). Rollback = drop tabel + drop kolom; tidak ada data yang hilang karena tidak ada data yang dipindah.

Perbaikan `cmd/demo-seed` (supaya tenant demo baru melahirkan AP yang bisa dilunasi) adalah perubahan kode, bukan migrasi, dan tidak menyentuh 4 baris demo yang sudah ada.

## T.9 Strategi test

- **Unit:** resolusi akun debit/kredit (semua kombinasi tier × kategori × retensi × PPN); perhitungan outstanding & status turunan; alokasi (lunas, sebagian, banyak-ke-satu, satu-ke-banyak); bucket aging; validasi lebih bayar.
- **Integration (`-tags integration`, wajib `-p 1` — F-4, bentrok tenant 9900777):** INV-AP-1 s/d INV-AP-7; penolakan periode tertutup; konflik idempotency (409); pembayaran konkuren di bawah row lock; reversal berantai; jurnal seimbang di setiap jalur.
- **Frontend:** Node 22 built-in runner + `--experimental-strip-types` (`npm test`), **tanpa dependency baru**. Uji validasi alokasi dan aturan tampil-error rupiah.
- **Gerbang selesai:** `go test ./...` hijau, `npx tsc --noEmit` bersih, `npm test` hijau, **dan** verifikasi browser di `http://localhost:PORT` (loopback saja, dibuktikan `ss -ltn`). Klaim "selesai" tidak boleh bersandar pada `tsc` saja.

## T.10 Graph dependensi

Lihat BAGIAN R. Ringkas: W-2 + W-3 + W-4 + W-7 + W-10 → **W-11** → {AP Aging, Rekonsiliasi AP↔GL, seam potongan, W-12, W-13}. W-12 dan W-13 paralel. Realisasi RAB adalah konsumen otomatis, bukan dependen.

## T.11 Keputusan bisnis yang dibutuhkan

13 butir di BAGIAN Q (BD-1 s/d BD-13). **Yang memblokir coding: BD-1, BD-3, BD-4, BD-5.** Sisanya dapat dijawab selama increment berjalan tanpa mengubah bentuk data model.

## T.12 Ruang lingkup W-11 yang diusulkan

Sebagaimana P0 di BAGIAN S: 13 butir — master vendor, kewajiban AP, pengakuan + pratinjau, pembayaran + alokasi + BKK + idempotency, outstanding/status turunan, aging, reversal dua arah, retensi (bersyarat BD-3), PPN Masukan opsional (bersyarat BD-7), seam potongan kosong, UI lengkap, penanda sumber di halaman biaya proyek, dan paket test invariant.

## T.13 Yang tegas di luar lingkup

Purchase Order · procurement · GRN · inventory · e-Faktur · pelaporan pajak lengkap · impor rekening koran · Fixed Asset (W-13) · Bank Reconciliation (W-12) · perubahan apa pun pada reader W-5/W-6 · perubahan apa pun pada semantik ledger · perbaikan F-4 · migrasi data.

---

# BAGIAN U — DESIGN REFINEMENT (Revisi 2)

Refinement atas empat keputusan pemilik (C-2 s/d C-5) dan sepuluh permintaan audit. Tidak ada kode, tidak ada migrasi, tidak ada perubahan data.

---

## U.0 — TEMUAN BARU YANG MENGUBAH JAWABAN BD-1

**[FAKTA]** `PostingService` **sudah punya siklus dua fase**, lengkap, dan sudah dipakai jalur lain:

| Method | `internal/ledger/posting_service.go` | Arti |
|---|---|---|
| `Create` | :132 | buat jurnal **DRAFT** (`posted_at` NULL). Cek periode dilakukan di sini |
| `Draft` | :269 | ambil draft |
| `Post` | :178 | tandai posted + tegakkan INV-DOC-1 di dalam transaksi |
| `PostDraft` | :224 | posting + terbitkan dokumen dalam **satu** transaksi |
| `CreateAndPost` | :288 | satu fase — yang dipakai `cost_entries` hari ini |

**[FAKTA]** Semua reader realisasi menyaring **posted-only**: `ActualCostByCode` (`actual_cost.go:106`) dan `GetRealisasiPerItem` (`budget/repository.go:331`) sama-sama mensyaratkan `je.posted_at IS NOT NULL`.

**[FAKTA]** Komentar `PostDraft` menyatakan aturan yang disengaja: dokumen **tidak** terbit saat draft dibuat, karena *"draft boleh saja tidak pernah diposting, dan nomor yang terpakai untuk transaksi yang tidak jadi termasuk yang dilarang INV-DOC-1."*

**[TEMUAN — U-1, menentukan]** Gabungan ketiga fakta itu memberi C-2 **persis** yang pemilik minta, dengan **nol mesin baru**:

- Tagihan/termin masuk → kewajiban AP tercatat + jurnal dibuat sebagai **DRAFT**.
  Draft **tidak** terposting ⇒ **tidak** masuk realisasi RAB, **tidak** masuk saldo GL, **tidak** masuk outstanding AP, **tidak** masuk aging. Ia belum menjadi kewajiban di mata sistem.
- Persetujuan diberikan → jurnal **di-post**.
  Pada detik yang sama, dan lewat satu peristiwa yang sama: hutang muncul di GL, outstanding AP lahir, jam aging mulai berjalan, dan realisasi RAB bertambah.

"Hutang lahir saat kewajiban resmi diakui" jadi **bukan aturan yang harus dijaga disiplin** — ia adalah arti harfiah dari `posted_at`. Semua reader existing sudah menghormatinya tanpa satu baris pun perubahan.

**[TEMUAN — U-2, gap yang harus ditutup]** `Post` (:178) dan `post` (:199) **tidak memeriksa periode**. Pemeriksaan periode hanya ada di `Create` (:132, pada tanggal jurnal) dan di `reverseWithin`.

Konsekuensi: draft yang dibuat 20 Des lalu baru disetujui 10 Jan — **setelah Desember ditutup** — akan terposting ke periode tertutup tanpa penolakan. Hari ini risikonya kecil karena hampir semua jalur memakai `CreateAndPost` (draft berumur nol detik). C-2 memperpanjang umur draft dari nol detik menjadi berhari-hari, sehingga **memindahkan gap ini dari teoretis ke rutin**.

**[REKOMENDASI]** W-11 memanggil `IsPeriodClosed` yang **sudah ada** (`ledger/repository.go:176`) pada langkah approve→post, dan menolak dengan pesan yang menyuruh pengguna me-*redate* kewajiban ke periode terbuka. Ini **memakai ulang** satu-satunya implementasi guard periode — bukan menulis guard kedua, dan bukan mengubah semantik ledger.

**[KOREKSI terhadap Revisi 1]** BAGIAN I sebelumnya menyatakan "tidak perlu pengecekan periode tambahan di service W-11". Itu benar untuk jalur satu fase, dan **tidak cukup** untuk jalur dua fase yang C-2 minta. Pernyataan ini menggantikannya.

**[TEMUAN — U-3, cara "approved" direpresentasikan & audit trail-nya]**
Kosakata dan audit trail-nya **sudah lengkap** di `internal/approval` — tidak ada yang perlu dibuat:

| Kebutuhan C-2 | Yang sudah ada | Bukti |
|---|---|---|
| Jenis target | `TargetCost`, `TargetPayment` sudah terdaftar | `approval/model.go:29-30` |
| Status | `pending / in_review / approved / rejected / cancelled` | `model.go:52-56` |
| Gate | `RequireApproved` — **OPT-IN**: tanpa workflow aktif → tanpa gate | `approval/service.go:408-427` |
| Siapa-kapan-apa | `Action{ActorID, ActorRole, Decision, Comment, CreatedAt}` — **APPEND-ONLY**, tidak ada jalur update/delete di seluruh aplikasi | `model.go:164-176` |
| Nilai yang disetujui | `Request.Amount` DECIMAL(20,4) | `model.go:144` |
| Anti-ubah-diam-diam | `TargetSnapshotJS` + `WorkflowSnapshotJS` + `WorkflowRevision` — evaluasi dari **snapshot beku** (Increment 5.1) | `model.go:147-150` |
| Siapa mengajukan | `RequestedBy`, `Notes` | `model.go:151-152` |
| Kapan diputuskan | `DecidedAt` | `model.go:153` |

**[TEMUAN]** `TargetSnapshotJS` menutup risiko terbesar C-2: seseorang menyetujui termin Rp100 juta, lalu nilainya diubah jadi Rp150 juta sebelum diposting. Karena Increment 5.1 mengevaluasi dari snapshot, perubahan setelah persetujuan **membatalkan kecocokan** — bukan lolos diam-diam.

**[REKOMENDASI]** Urutan yang mengikat, dan alasan urutannya:
1. Kewajiban AP dibuat → **jurnal draft** (butuh `target_id` yang stabil; approval tidak bisa menunjuk sesuatu yang belum ada).
2. `approval.Request` dibuat dengan `TargetType = TargetCost`, `TargetID = ap_invoice.id`, `Amount = gross_amount`.
3. Approve/reject dicatat sebagai `Action` (append-only).
4. Saat post: `RequireApproved` → cek periode → `PostingService.Post`.

Karena gate-nya **OPT-IN**, tenant yang belum mengonfigurasi workflow tetap bisa memakai W-11 — draft langsung boleh di-post oleh pembuatnya. Governance bertahap, tanpa memblokir tenant kecil.

**[KEPUTUSAN BISNIS BD-14 — baru]** Apakah tenant yang **tidak** punya workflow aktif boleh memposting kewajiban AP secara langsung (setara `CreateAndPost`, tanpa langkah approve manual)? **[REKOMENDASI]** Ya — konsisten dengan sifat OPT-IN yang sudah berlaku di seluruh repo.

---

## U.1 — LIFECYCLE KONKRET: AP + `cost_entries`

Contoh: Termin 3 kontraktor, Rp100.000.000, proyek Griya Asri Fase 1, dua item RAB (Struktur 80 jt, Finishing 20 jt).

### Tahap 1 — Termin dicatat (belum disetujui)

| Yang ditulis | Isi |
|---|---|
| `ap_invoices` | 1 baris: vendor, no. invoice, tgl invoice, jatuh tempo, gross 100.000.000 |
| `cost_entries` | **2 baris** — 80.000.000 (item Struktur) & 20.000.000 (item Finishing); `payment_method='payable'`; `project_id` terisi; `ap_invoice_id` menunjuk kewajiban; `journal_entry_id` menunjuk jurnal draft |
| `journal_entries` | 1 baris **DRAFT** (`posted_at` NULL), `source='ap_invoice'` |

```
Dr  1-3100 Persediaan RE — Hard Cost  [proyek=Griya Asri]   100.000.000
    Cr  2-1000 Hutang Usaha                                      100.000.000
                                                    ── DRAFT, belum diposting ──
```

**Efek terhadap pembacaan (semuanya nol, karena posted-only):**

| Pembacaan | Nilai |
|---|---|
| Realisasi RAB proyek (`ActualCostByCode`) | **0** — `posted_at IS NULL` |
| Realisasi per item RAB (`GetRealisasiPerItem`) | **0** — join `journal_entries` mensyaratkan posted |
| Saldo GL 2-1000 | **0** |
| Outstanding AP / aging | **0** |

### Tahap 2 — Disetujui → diposting

`RequireApproved(TargetCost, ap_invoice.id)` lolos → cek `IsPeriodClosed` → `PostingService.Post`.

Yang berubah: **hanya `posted_at`**. Tidak ada jurnal baru, tidak ada baris `cost_entries` baru, tidak ada nilai yang dihitung ulang.

| Pembacaan | Nilai |
|---|---|
| Realisasi RAB proyek | **100.000.000** |
| Realisasi per item — Struktur | **80.000.000** |
| Realisasi per item — Finishing | **20.000.000** |
| Saldo GL 2-1000 | **100.000.000** kredit |
| Outstanding AP | **100.000.000** |
| Aging | mulai berjalan dari `due_date` |

**[TEMUAN]** Enam angka di atas lahir dari **satu** peristiwa (`posted_at` terisi) dan dibaca dari **dua** representasi yang saling terikat: baris `cost_entries` dan baris `journal_lines`. Ikatannya dijaga INV-AP-8 (U.7).

### Tahap 3 — Pembayaran

```
Dr  2-1000 Hutang Usaha                                   100.000.000
    Cr  1-1300 Bank — BCA                                      100.000.000
                                          [BKK/2026/000xxx, source='ap_payment']
```

| Pembacaan | Nilai | Perubahan |
|---|---|---|
| Realisasi RAB proyek | 100.000.000 | **TIDAK BERUBAH** |
| Realisasi per item | 80 jt / 20 jt | **TIDAK BERUBAH** |
| Saldo GL 2-1000 | **0** | lunas |
| Outstanding AP | **0** | lunas |
| Kas | −100.000.000 | |

### Bukti tidak ada double counting

**Jalur A — realisasi tingkat proyek.** `ActualCostByCode` menyaring baris jurnal **hanya** pada daftar kode akun taksonomi (`InventoryAccountCode` / `ExpenseAccountCode`). Jurnal pembayaran menyentuh **2-1000 dan 1-1300** — keduanya di luar daftar itu. Jurnal pembayaran **tidak pernah masuk ke query-nya sama sekali**, bukan "masuk lalu dinetting".

**Jalur B — realisasi per item RAB.** `GetRealisasiPerItem` menjumlahkan `cost_entries.amount`. Pembayaran **tidak menulis baris `cost_entries`** — ia hanya menulis `ap_payments` + `ap_payment_allocations`. Tidak ada yang bertambah.

**Jalur C — GL.** Debit ke akun biaya terjadi **tepat sekali**, di jurnal pengakuan.

**[TEMUAN]** Ketiga jalur aman karena alasan struktural yang **berbeda-beda** — bukan karena satu aturan yang bisa lupa diterapkan. Itu yang membuat C-1 aman untuk dikunci.

---

## U.2 — PARTIAL PAYMENT: Rp100 jt → 60 jt → 40 jt

### T0 — Pengakuan (setelah disetujui & diposting)

```
Dr  1-3100 Persediaan RE — Hard Cost  [proyek]   100.000.000
    Cr  2-1000 Hutang Usaha                           100.000.000
```

| | Nilai |
|---|---:|
| Biaya/WIP diakui | 100.000.000 |
| Realisasi RAB | 100.000.000 |
| Saldo 2-1000 | 100.000.000 |
| Σ alokasi | 0 |
| **Outstanding** | **100.000.000** |
| Status turunan | `outstanding` |

### T1 — Bayar Rp60 juta

```
Dr  2-1000 Hutang Usaha                           60.000.000
    Cr  1-1300 Bank — BCA                              60.000.000     [BKK #1]
```

`ap_payments` 1 baris (60 jt) + `ap_payment_allocations` 1 baris (60 jt → kewajiban ini, `allocation_type='invoice'`).

| | Nilai |
|---|---:|
| Biaya/WIP | 100.000.000 (**tetap**) |
| Realisasi RAB | 100.000.000 (**tetap**) |
| Saldo 2-1000 | 40.000.000 |
| Σ alokasi | 60.000.000 |
| **Outstanding** | **40.000.000** |
| Status turunan | `partial` |
| Kas | −60.000.000 |

### T2 — Bayar Rp40 juta

```
Dr  2-1000 Hutang Usaha                           40.000.000
    Cr  1-1300 Bank — BCA                              40.000.000     [BKK #2]
```

| | Nilai |
|---|---:|
| Biaya/WIP | 100.000.000 (**tetap**) |
| Realisasi RAB | 100.000.000 (**tetap**) |
| Saldo 2-1000 | **0** |
| Σ alokasi | 100.000.000 |
| **Outstanding** | **0** |
| Status turunan | `paid` |
| Kas | −100.000.000 |

**[FAKTA]** `outstanding` **tidak disimpan** — ia selalu `payable_amount − Σ(alokasi non-terbalik)`, dihitung di dalam transaksi setelah row lock (INV-AP-2). Dua pembayaran bersamaan tidak bisa sama-sama membaca outstanding 100 juta.

**[FAKTA]** Dua pembayaran = dua pergerakan kas = **dua BKK**. Bukan satu BKK dipecah. Ini konsekuensi langsung INV-DOC-1.

---

## U.3 — RETENSI: Rp100 juta, retensi 5%

### T0 — Pengakuan termin

```
Dr  1-3100 Persediaan RE — Hard Cost  [proyek]   100.000.000
    Cr  2-1000 Hutang Usaha                            95.000.000
    Cr  <akun retensi>                                  5.000.000
```

| | Nilai |
|---|---:|
| **Biaya/WIP** | **100.000.000** ← bruto, sesuai C-3 |
| Realisasi RAB | 100.000.000 |
| AP current (2-1000) | 95.000.000 |
| Kewajiban retensi | 5.000.000 |
| Total kewajiban ke vendor | 100.000.000 |
| Kas | 0 |

**[TEMUAN]** Retensi **tidak mengurangi biaya**. Ia hanya memecah **sisi kredit**. `cost_entries.amount` tetap 100.000.000 — sehingga `GetRealisasiPerItem` dan `ActualCostByCode` tetap sepakat (INV-AP-8). Kalau retensi keliru mengurangi `amount`, kedua reader itu langsung menyimpang 5 juta — inilah cara paling mudah merusak C-1, dan sekarang tertutup invariant.

### T1 — Termin dibayar (di luar retensi)

```
Dr  2-1000 Hutang Usaha                            95.000.000
    Cr  1-1300 Bank — BCA                               95.000.000     [BKK]
```

| | Nilai |
|---|---:|
| Biaya/WIP | 100.000.000 (**tetap**) |
| AP current | **0** |
| Kewajiban retensi | 5.000.000 |
| Outstanding AP (aging) | **0** ← jam aging berhenti, benar |
| Outstanding retensi | 5.000.000 ← jatuh tempo sendiri |
| Kas | −95.000.000 |

**[TEMUAN]** Inilah alasan akuntansi utama memisahkan retensi: kalau 5 juta tetap di 2-1000, laporan aging akan melaporkannya **terlambat bayar** padahal secara kontrak memang belum boleh dibayar. Vendor akan ditagih atas sesuatu yang ditahan sah.

### T2 — Pelepasan retensi (masa pemeliharaan berakhir)

```
Dr  <akun retensi>                                  5.000.000
    Cr  1-1300 Bank — BCA                                5.000.000     [BKK]
```

| | Nilai |
|---|---:|
| Biaya/WIP | 100.000.000 (**tetap sejak T0**) |
| AP current | 0 |
| Kewajiban retensi | **0** |
| Kas | −100.000.000 |

**[FAKTA]** Total debit ke akun biaya sepanjang tiga tahap: **100.000.000, tepat sekali**. Total kas keluar: 95 + 5 = **100.000.000, tepat sekali**.

**Akun retensi mana yang dipakai:** lihat U.5 — jawabannya **gap**, dan pemilik yang memutuskan.

---

## U.4 — UANG MUKA VENDOR: advance Rp20 jt → termin Rp100 jt

### T0 — Uang muka dibayar (belum ada pekerjaan, belum ada biaya)

```
Dr  <akun uang muka vendor>                        20.000.000
    Cr  1-1300 Bank — BCA                               20.000.000     [BKK]
```

| | Nilai |
|---|---:|
| **Biaya/WIP** | **0** ← belum ada biaya |
| **Realisasi RAB** | **0** ← akun uang muka bukan akun taksonomi |
| Aset uang muka | 20.000.000 |
| Saldo 2-1000 | 0 |
| Kas | −20.000.000 |

**[TEMUAN]** Ini titik kegagalan paling umum. Kalau uang muka dicatat `Dr 1-3100 / Cr Bank`, realisasi RAB naik 20 juta **sebelum ada pekerjaan**, lalu naik 100 juta lagi saat termin diakui → **total 120 juta untuk pekerjaan 100 juta**. Bentuk jurnal di atas mencegahnya karena akun uang muka **tidak ada** dalam daftar kode taksonomi yang dibaca `ActualCostByCode`.

### T1 — Termin Rp100 juta diakui, uang muka dikompensasi

```
Dr  1-3100 Persediaan RE — Hard Cost  [proyek]   100.000.000
    Cr  <akun uang muka vendor>                        20.000.000     ← offset
    Cr  2-1000 Hutang Usaha                            80.000.000
```

| | Nilai |
|---|---:|
| **Biaya/WIP** | **100.000.000** ← sekali, bukan 120 juta |
| Realisasi RAB | 100.000.000 |
| Aset uang muka | **0** ← habis terkompensasi |
| Saldo 2-1000 | 80.000.000 |
| **Outstanding AP** | **80.000.000** |
| Kas | −20.000.000 |

**[FAKTA]** `cost_entries.amount` = **100.000.000** (bruto), bukan 80 juta. Uang muka mengurangi **kewajiban**, bukan **biaya**. INV-AP-8 tetap terpenuhi: Σ `amount` (100 jt) == Σ debit taksonomi (100 jt).

### T2 — Sisa dibayar

```
Dr  2-1000 Hutang Usaha                            80.000.000
    Cr  1-1300 Bank — BCA                               80.000.000     [BKK]
```

| | Nilai |
|---|---:|
| Biaya/WIP | 100.000.000 (**tetap**) |
| Realisasi RAB | 100.000.000 (**tetap**) |
| Saldo 2-1000 | 0 |
| Outstanding | 0 |
| Kas | −100.000.000 |

**Bukti tidak ada pengakuan biaya ganda:**

| Jurnal | Debit ke akun taksonomi biaya |
|---|---:|
| T0 uang muka | **0** (debit ke akun aset uang muka) |
| T1 pengakuan termin | **100.000.000** |
| T2 pelunasan | **0** (debit ke 2-1000) |
| **Total** | **100.000.000** |

Uang muka **tidak pernah** menyentuh akun biaya: ia lahir sebagai aset dan mati sebagai kredit terhadap aset itu sendiri.

---

## U.5 — AUDIT COA LENGKAP

**[FAKTA]** COA existing (`internal/ledger/coa.go`), seluruh akun relevan:

| Kode | Nama | Tipe |
|---|---|---|
| 1-1100 / 1-1200 | Kas Besar / Petty Cash | Aset |
| 1-1300 / 1-1400 / 1-1500 | Bank BCA / Mandiri / BRI | Aset |
| 1-2000 | Piutang Usaha | Aset |
| 1-2100 | Piutang Lain-lain | Aset |
| 1-2200 | Piutang Bank (KPR) | Aset |
| 1-3000 / 1-3100 / 1-3200 / 1-3300 | Persediaan RE — Tanah / Hard / Soft / Biaya Pembiayaan | Aset |
| 1-4000 / 1-4100 / 1-4900 | Aset Tetap Peralatan / Kendaraan / Akum. Penyusutan | Aset |
| 1-5000 | Biaya Dibayar di Muka | Aset |
| 1-5100 | **PPN Masukan** | Aset |
| 2-1000 | **Hutang Usaha** | Kewajiban |
| 2-2000 / 2-2100 / 2-2200 | Uang Muka Penjualan / Titipan Booking / Hutang Refund | Kewajiban |
| 2-2300 / 2-2400 | Titipan Notaris / Titipan Realisasi | Kewajiban |
| 2-3000 | PPN Keluaran | Kewajiban |
| 2-4000 | Hutang PPh Final Pengalihan | Kewajiban |
| 2-5000 | Hutang Bank | Kewajiban |
| 2-6000 | Biaya Akrual | Kewajiban |
| 2-6100 / 2-6200 | Hutang Gaji & Tunjangan / Utang Komisi | Kewajiban |
| 5-1000 … 5-5000 | HPP dan beban-beban | Beban |

**[FAKTA]** Tidak ada akun `1-1600+`, `2-1100`, `2-1200`, `1-5200`, `1-5300` — ruang kode itu kosong.

### Tabel gap

| Purpose | Existing COA | Suitable? | Gap |
|---|---|---|---|
| **Hutang usaha (AP)** | **2-1000 Hutang Usaha** | ✅ **YA** — sudah terdaftar `RolePayable` | **Tidak ada gap** |
| **Kas/bank pembayaran vendor** | 1-1100 … 1-1500, dipilih lewat `accounts.category` (cash/bank) & `GET /ledger/accounts/cash-bank` | ✅ **YA** — mekanisme COA-driven sudah jalan | **Tidak ada gap** |
| **PPN Masukan** | **1-5100 PPN Masukan** | ✅ **YA** — sudah terdaftar `RoleVATInput`; `VATReport` sudah menjumlahkannya | **Tidak ada gap** (yang kurang cuma **produsen**, bukan akun) |
| **Liability retensi** | 2-1000 Hutang Usaha | ⚠️ **BISA, tapi merusak aging** — 5 juta yang sah ditahan akan terbaca terlambat bayar | **GAP** |
| " | 2-6000 Biaya Akrual | ❌ **TIDAK** — 2-6000 untuk beban yang **sudah terjadi tapi belum ditagih**. Retensi kebalikannya: sudah ditagih, biaya sudah diakui, hanya **pembayaran** yang ditahan. Mencampur dua populasi ini merusak analisis cut-off | |
| " | 2-2400 Titipan Realisasi | ❌ **TIDAK — arah ekonominya terbalik.** 2-2400 adalah uang **pembeli** yang kita pegang (titipan pihak ketiga). Retensi adalah uang **kita** yang belum kita bayar ke vendor. Mencampurnya melanggar aturan charge group (K-1..K-5) | |
| " | 2-5000 Hutang Bank | ❌ **TIDAK** — pinjaman bank, bukan kewajiban dagang | |
| **Uang muka vendor** | 1-5000 Biaya Dibayar di Muka | ❌ **TIDAK** — *prepaid expense* adalah pembayaran di muka atas **beban** yang manfaatnya terbagi antar periode (sewa, asuransi). Uang muka konstruksi akan menjadi **persediaan/WIP**, bukan beban periodik. Salah klasifikasi di neraca | **GAP** |
| " | 1-2100 Piutang Lain-lain | ⚠️ **BISA dipaksakan, tapi salah nama** — uang muka vendor bukan piutang: klaim kita atas **barang/jasa**, bukan atas uang kembali. Dapat dipakai sebagai jalan sementara, tapi menyesatkan pembaca neraca | |
| " | 1-3000/1-3100 Persediaan RE | ❌ **TIDAK** — akan langsung masuk realisasi RAB sebelum ada pekerjaan (kesalahan U.4) | |
| **Withholding PPh 23 / 4(2)** | 2-4000 Hutang PPh Final Pengalihan | ❌ **TIDAK — beda subjek pajak.** 2-4000 adalah PPh final atas **pengalihan hak atas tanah/bangunan**, kewajiban kita **sebagai penjual**. PPh 23/4(2) atas jasa kontraktor adalah kewajiban kita **sebagai pemotong** atas penghasilan **pihak lain**: beda SPT masa, beda bukti potong, beda pelaporan. Mencampur = SPT salah | **GAP** (hanya bila BD-6 = ya) |
| " | 2-3000 PPN Keluaran | ❌ **TIDAK** — PPN ≠ PPh | |
| **Beban proyek (debit AP proyek)** | 1-3000/1-3100/1-3200/1-3300 lewat `Category.InventoryAccountCode()` | ✅ **YA** | **Tidak ada gap** |
| **Beban overhead (debit AP non-proyek)** | 5-3000/5-4000 dst lewat `Category.ExpenseAccountCode()` | ✅ **YA** | **Tidak ada gap** |
| **Aset tetap (fondasi W-13)** | 1-4000 / 1-4100 | ✅ **YA** — akunnya ada | Gap-nya **jalur**, bukan akun (BAGIAN P) |

### Ringkasan gap

**[TEMUAN]** Dari 8 kebutuhan, **5 sudah tercukupi penuh** oleh COA existing. Gap sesungguhnya hanya **3 akun**, dan hanya **2** yang menyentuh P0:

| # | Akun yang benar-benar diperlukan | Untuk | Kapan | Alasan accounting |
|---|---|---|---|---|
| 1 | Kewajiban retensi kontraktor | C-3 | **P0** | Kewajiban dagang dengan **jatuh tempo berbeda** dan syarat pelepasan berbeda. Menggabungkannya ke 2-1000 membuat aging melaporkan kewajiban yang sah ditahan sebagai terlambat bayar |
| 2 | Uang muka vendor | Uang muka & fondasi W-13 | **P1** | **Aset**, bukan beban dan bukan piutang. Harus di luar daftar akun taksonomi agar tidak masuk realisasi RAB sebelum ada pekerjaan (U.4) |
| 3 | Hutang PPh dipotong (23 / 4(2)) | BD-6 | **P1/P2** | Kewajiban **pemotong** kepada negara atas penghasilan pihak lain — populasi berbeda dari 2-4000 (kewajiban sendiri sebagai penjual) |

**[REKOMENDASI]** Kode yang diusulkan — **usulan penamaan saja, tidak dibuat, tidak di-seed, tidak dimigrasikan**: `2-1100 Hutang Retensi Kontraktor`, `1-5300 Uang Muka Vendor`, `2-1200 Hutang PPh Dipotong`. Ruang kode ini kosong dan bersebelahan dengan keluarga yang tepat. Setiap akun baru wajib didaftarkan ke `AccountRoleRegistry` (`RoleRetentionPayable`, `RoleVendorAdvance`, `RoleWithholdingPayable`) supaya tidak ada query yang menulis kode akun secara harfiah.

**[KEPUTUSAN BISNIS BD-4 — tetap menunggu pemilik]** Menyetujui 1 akun (retensi, P0), 2 akun (+uang muka, P1), atau 3 akun (+PPh, bergantung BD-6)? Alternatifnya untuk retensi: pakai 2-1000 dengan atribut, dengan konsekuensi aging yang sudah dijelaskan.

---

## U.6 — DATA MODEL: pemetaan field (konsekuensi C-1)

### Yang dipakai pada `cost_entries` — **sumber biaya, tidak dipindah**

| Field | Dipakai AP? | Isi | Alasan |
|---|---|---|---|
| `tenant_id` | ✅ | tenant aktif | isolasi |
| `project_id` | ✅ | dari baris kewajiban | **wajib** untuk tier `direct`/`shared`; **wajib NULL** untuk `overhead` (INV-EXP-2). Sudah ditegakkan `chk_ce_tier_project` |
| `phase_id` | ✅ opsional | fase proyek | scope realisasi |
| `unit_id` | ✅ kondisional | sesuai `chk_ce_tier_unit` | aturan existing, tidak diubah |
| `budget_item_id` | ✅ opsional | item RAB per baris | **satu-satunya jalan** biaya AP muncul di `GetRealisasiPerItem`. Kosong ⇒ hilang dari drill-down item (BD-2) |
| `category` | ✅ | kategori biaya | menentukan akun debit lewat `Category.InventoryAccountCode()` / `ExpenseAccountCode()` |
| `cost_tier` | ✅ | direct/shared/overhead | menentukan cabang akun; immutable, reklasifikasi via pembalik |
| `amount` | ✅ | **nilai biaya bruto** — sebelum retensi, sebelum offset uang muka, **tanpa PPN** | wajib sama dengan debit taksonomi (INV-AP-8) |
| `vendor` (VARCHAR 200) | ✅ **tetap** | **snapshot nama** vendor saat transaksi | kompatibilitas historis (C-5) + histori tak berubah saat vendor di-rename |
| `vendor_id` (**baru**, NULL) | ✅ | FK `vendors` | identitas kanonik |
| `payment_method` | ✅ | `'payable'` | membedakan dari jalur bank langsung |
| `journal_entry_id` | ✅ | jurnal **pengakuan** | N baris `cost_entries` → 1 jurnal. Ikatan ke GL |
| `ap_invoice_id` (**baru**, NULL) | ✅ | FK `ap_invoices` | ikatan ke kewajiban; penanda "sumber: Termin" di UI proyek |

### Yang tetap di kewajiban AP (`ap_invoices`) — **tidak ditaruh di `cost_entries`**

| Field | Kenapa di sini |
|---|---|
| `vendor_id` (kanonik), `vendor_name_snapshot` | atribut kewajiban; `cost_entries.vendor_id` adalah denormalisasi untuk query biaya |
| `invoice_number`, `invoice_date`, `due_date` | atribut **tagihan**, bukan atribut biaya. `cost_entries` tidak punya konsep jatuh tempo dan tidak boleh punya |
| `contract_ref`, `progress_percent` | konteks termin |
| `gross_amount`, `retention_amount`, `ppn_amount`, `payable_amount` | **nilai kewajiban ≠ nilai biaya.** Retensi & PPN memecah sisi kredit; keduanya tidak boleh mengubah `cost_entries.amount` |
| `faktur_pajak_number` | dokumen pajak |
| `journal_entry_id` (pengakuan) | sama dengan yang di baris `cost_entries` — bukan sumber kedua, tapi jalan pintas baca |
| `created_by` | jejak pembuat; keputusan approve ada di `approval_actions` |

### Referensi dokumen — **tidak di keduanya**

**[FAKTA]** Dokumen menempel pada **`journal_entries.document_id`** (`document_issuer.go:322,333,340`). `cost_entries` tidak punya kolom dokumen, dan tidak boleh diberi satu.

| Peristiwa | Dokumen |
|---|---|
| Pengakuan hutang | **tidak ada** — tidak ada kas bergerak |
| Pembayaran hutang | **BKK**, di `journal_entries.document_id` jurnal pembayaran; `ap_payments.document_id` menyimpan tautan baca |
| Pembalikan | **JR**, diterbitkan `reverseWithin` |

### Vendor Master minimal (C-5) — persis kandidat pemilik, tanpa tambahan

`id · tenant_id · name · npwp · address · bank_name · bank_account_number · payment_terms_days · is_active · created_at · updated_at`

**[REKOMENDASI]** Satu field di luar daftar pemilik: `bank_account_name` (nama pemilik rekening) — sering berbeda dari nama vendor (mis. rekening pribadi direktur) dan merupakan penyebab salah transfer paling umum. **Bila dianggap over-engineering, hapus saja** — tidak ada logika yang bergantung padanya.

**Yang sengaja TIDAK ada:** kategori vendor, rating, kontak person berganda, plafon kredit, multi-rekening, dokumen legal. Semuanya fitur procurement, bukan akuntansi.

**Kompatibilitas historis:** `cost_entries.vendor` **tetap ada apa adanya, tanpa backfill**. Baris lama `vendor_id` NULL + teks terisi → UI fallback ke teks. Baris baru mengisi keduanya. Merge vendor lama = operasi manual opsional P2, bukan migrasi yang menebak.

---

## U.7 — SOURCE OF TRUTH (tegas)

| Nilai | **Satu-satunya sumber** | Dibaca lewat | Yang **BUKAN** sumber |
|---|---|---|---|
| **Cost recognition** (biaya diakui) | `journal_lines` debit pada akun taksonomi, **posted-only** | `ledger.ActualCostByCode` | ~~`ap_invoices.gross_amount`~~ — itu nilai tagihan, bukan biaya |
| **Cost per item RAB** | `cost_entries.amount` + `budget_item_id`, posted-only | `budget.GetRealisasiPerItem` | ~~tabel AP~~ (C-1) |
| **AP liability — total perusahaan** | **GL**: saldo 2-1000 (+ akun retensi) | `LedgerBalanceService.AccountBalance` | ~~Σ outstanding sub-ledger~~ |
| **AP liability — per vendor / per tagihan** | `ap_invoices` + `ap_payment_allocations` | service AP | ~~GL~~ (GL tidak menyimpan vendor) |
| **Outstanding satu kewajiban** | `payable_amount − Σ(alokasi non-terbalik)` — **turunan** | dihitung di dalam transaksi setelah row lock | ~~kolom `outstanding` tersimpan~~ (tidak ada) |
| **Status kewajiban** | **turunan** dari jurnal + alokasi + `due_date` | fungsi murni | ~~kolom `status` tersimpan~~ (tidak ada) |
| **Payment** | `ap_payments` + `ap_payment_allocations` | service AP | ~~`ap_invoices`~~ |
| **Efek kas pembayaran** | **GL**: kredit akun kas/bank | `AccountBalance` | ~~`ap_payments.amount`~~ (itu catatan, bukan saldo) |
| **Nomor dokumen** | `documents` lewat `journal_entries.document_id` | engine W-2 | ~~kolom nomor di tabel AP~~ |
| **Approval** | `approval_requests` + `approval_actions` (append-only) | `approval.RequireApproved` | ~~kolom `approved_by` di `ap_invoices`~~ (tidak ada) |
| **GL** | `journal_entries` + `journal_lines` | — | **selalu otoritatif untuk saldo** |
| **RAB realization** | ledger (tingkat proyek) + `cost_entries` (tingkat item) | reader W-5/W-6, **tidak diubah** | ~~apa pun di W-11~~ |

**[REKOMENDASI]** Tidak ada satu nilai pun dengan dua sumber. Yang tampak "dobel" sebenarnya dua **lensa berbeda atas peristiwa yang sama**, dan diikat invariant:

- **INV-AP-1** mengikat sub-ledger AP ke GL (kupas-lapis per `source`).
- **INV-AP-8 (baru)** mengikat kedua reader realisasi:
  > Untuk setiap kewajiban AP: `Σ cost_entries.amount` == `Σ debit ke akun taksonomi` pada jurnal pengakuan kewajiban itu.

**[TEMUAN]** INV-AP-8 adalah penjaga langsung C-1. Ia gagal persis ketika seseorang membuat retensi/PPN/uang muka mengurangi `cost_entries.amount`, atau membuat baris biaya di luar `cost_entries`. Ketiganya adalah cara C-1 bisa dilanggar, dan ketiganya ketahuan oleh satu test.

---

## U.8 — PAYABLE HISTORIS SAAT W-11 MULAI DIPAKAI

**[FAKTA]** Kondisi sekarang (audit read-only, BAGIAN O): 4 baris payable, semuanya tenant demo (1 = `esaProperti Dev`, 9901005 = `W5 UI Check`). **Nol** di tenant produksi. Semuanya `budget_item_id` NULL, `source='system'`, sudah terposting, berasal dari `cmd/demo-seed`. Nol debit ke 2-1000 sepanjang sejarah database.

**Tetap tidak ada migrasi sekarang.** Yang berikut menjelaskan bagaimana baris-baris itu **berperilaku** saat W-11 hidup.

### Bagaimana baris warisan terbaca oleh W-11

| Aspek | Perilaku | Alasan |
|---|---|---|
| Saldo GL 2-1000 | **Ikut terhitung penuh** | GL selalu otoritatif; baris itu jurnal sah yang sudah diposting |
| Realisasi RAB proyek | **Ikut terhitung**, seperti sekarang | `ActualCostByCode` tidak peduli asal-usul |
| Sub-ledger AP (`ap_invoices`) | **Tidak muncul** | tidak ada kewajiban AP yang mewakilinya |
| Aging AP | **Tidak muncul** | tidak ada `due_date` maupun vendor terstruktur |
| Rekonsiliasi INV-AP-1 | **Muncul sebagai lapisan `system`** | inilah yang membuatnya aman |

**[TEMUAN]** Inilah alasan INV-AP-1 sejak Revisi 1 dirumuskan **per-source** dan bukan sebagai kesamaan mutlak. Payable warisan **tidak** membuat rekonsiliasi gagal — ia muncul sebagai selisih yang **terjelaskan dan terberi label**, persis seperti saldo awal dan jurnal manual. Kalau invariantnya dirumuskan "Σ outstanding == saldo 2-1000", W-11 akan gagal di tenant demo sejak hari pertama.

### Yang harus terlihat oleh pengguna

**[REKOMENDASI]** Laporan rekonsiliasi AP menampilkan baris eksplisit:

```
Saldo GL 2-1000 per 31 Agu 2026                      13.000.000.000
  − Kewajiban AP terkelola (ap_invoice/ap_payment)              0
  − Saldo awal (opening_balance)                                0
  − Jurnal manual (manual)                                      0
  ────────────────────────────────────────────────────────────────
  = Payable warisan pra-W-11 (system)               13.000.000.000  ⚠
```

Baris ⚠ adalah **hutang yang tidak punya vendor, tidak punya jatuh tempo, dan tidak bisa dilunasi lewat UI AP**. Ia tidak boleh senyap.

### Tiga jalan keluar untuk saldo warisan — **[KEPUTUSAN BISNIS BD-15, baru]**

| Opsi | Cara | Cocok untuk |
|---|---|---|
| **A — Biarkan** | Tetap sebagai lapisan `system` di rekonsiliasi, selamanya terlihat sebagai warisan | Tenant demo. **Rekomendasi untuk kondisi sekarang**, karena nol tenant produksi terdampak |
| **B — Adopsi** | Buat kewajiban AP **tanpa jurnal** yang menunjuk jurnal warisan (vendor + jatuh tempo diisi manual), lalu pembayarannya lewat mesin W-11 seperti biasa | Kalau nanti ada tenant produksi terlanjur punya payable warisan. Ini **persis pola W-7/INV-LAR-4**: buku pembantu atas saldo yang sudah ada di GL, tanpa jurnal baru |
| **C — Balik** | Jurnal pembalik ber-JR, lalu catat ulang lewat W-11 | Hanya bila baris warisannya memang **salah** secara ekonomi. Tetap append-only — **tidak pernah `DELETE`** |

**[REKOMENDASI]** **Opsi A sekarang** (nol tenant produksi terdampak), dengan **Opsi B disiapkan sebagai jalur P2** karena polanya sudah terbukti di W-7 dan biayanya kecil. **Opsi C tidak dipakai** kecuali ada kekeliruan ekonomi nyata.

**[REKOMENDASI]** Terpisah dari itu: perbaiki `cmd/demo-seed` supaya tenant demo **berikutnya** melahirkan kewajiban AP yang bisa dilunasi. Itu perubahan kode, bukan migrasi, dan tidak menyentuh 4 baris yang sudah ada.

**[REKOMENDASI]** Guard transport Tahap 1 di `internal/cost/handler.go` **tetap dipertahankan** setelah W-11 hidup. Rute `/projects/{id}/cost-entries` tidak boleh jadi pintu AP — pintu AP satu-satunya adalah `POST /ap/invoices`, karena hanya jalur itu yang membuat kewajiban, vendor, dan jatuh tempo.

---

## U.9 — KOREKSI PENAMAAN TENANT

**[FAKTA — dari tabel `tenants`]** Data aktual yang dipakai di seluruh dokumen ini:

| tenant_id | Nama sebenarnya | Payable | Catatan |
|---|---|---|---|
| **9900246** | **UAT Batch 2** | **0** | Tenant volume tertinggi: 5 cost entries, 40 journal entries, 16 unit |
| **9** | **Nata Alam** | **0** | Tenant terpisah |
| 1 | esaProperti Dev | 2 baris | demo seed |
| 9901005 | W5 UI Check | 2 baris | tenant uji UI |

Tenant 9900246 **tidak lagi disebut "Nata Alam"** di mana pun dalam dokumen ini. Kesimpulan substantifnya tidak berubah: **kedua** tenant nyata bersih dari payable.

---

## U.10 — RINGKASAN PERUBAHAN REVISI 2

| # | Bagian | Perubahan |
|---|---|---|
| 1 | Header | Constraint terkunci C-1 s/d C-5 |
| 2 | U.0 | **Temuan baru:** siklus draft→post ledger **sudah ada** ⇒ C-2 tercapai dengan nol mesin baru |
| 3 | U.0 | **Gap baru:** `Post` tidak memeriksa periode; C-2 menaikkan risikonya dari teoretis ke rutin ⇒ W-11 memanggil `IsPeriodClosed` existing saat approve→post |
| 4 | U.0 | **Koreksi Revisi 1:** BAGIAN I ("tidak perlu cek periode tambahan") berlaku untuk jalur satu fase saja |
| 5 | U.0 | Representasi "approved" + audit trail dipetakan penuh ke `internal/approval`; `TargetSnapshotJS` menutup risiko ubah-setelah-approve |
| 6 | U.5 | Audit COA lengkap: **5 dari 8 kebutuhan sudah tercukupi**; gap sesungguhnya **3 akun** (2 menyentuh P0/P1), dengan alasan penolakan per akun |
| 7 | U.6 | Pemetaan field `cost_entries` vs kewajiban AP; dokumen ada di `journal_entries`, bukan di keduanya |
| 8 | U.7 | **INV-AP-8 (baru)** — penjaga langsung C-1: Σ `cost_entries.amount` == Σ debit taksonomi |
| 9 | U.8 | Perilaku payable warisan + **BD-15** (biarkan / adopsi / balik) |
| 10 | Q | **BD-14** (posting langsung tanpa workflow aktif) dan **BD-15** ditambahkan |

**Keputusan bisnis kini 15 butir.** Yang masih memblokir coding: **BD-4** (akun COA — U.5 sudah menyediakan bahan keputusannya) dan **BD-3(b)** (retensi: akun terpisah atau atribut). BD-1 dan BD-5 sudah dijawab pemilik dan sudah dipetakan ke kode existing.

---

## Lampiran — bukti file:line

| Klaim | Bukti |
|---|---|
| Netting biaya aktual | `internal/ledger/actual_cost.go:106` |
| Realisasi proyek delegasi ke ledger | `internal/budget/repository.go:309` |
| Realisasi per item baca `cost_entries` (T-2) | `internal/budget/repository.go:331` |
| `RolePayable → 2-1000` | `internal/ledger/account_role.go:29,48` |
| `RoleVATInput → 1-5100` | `internal/ledger/account_role.go:34,53` |
| Konstanta dokumen (BKK/BTP/RFC/JR) | `internal/ledger/document_issuer.go:62-67` |
| Guard periode tunggal | `internal/ledger/repository.go:176` |
| Idempotency + row lock pembayaran | `internal/charge/service.go:618-730` |
| Gate approval OPT-IN | `internal/approval/service.go:408-427` |
| Mesin aging tunggal | `internal/receivable/receivable.go` |
| Rekonsiliasi kupas-lapis | `internal/legacyar/service.go:200-320` |
| Saldo & pergerakan per source | `internal/ledger/balance_service.go` |
| Guard payable transport (Tahap 1) | `internal/cost/handler.go`, `internal/cost/payable_guard_test.go` |
| Grup navigasi KEUANGAN | `frontend/components/layout/Sidebar.tsx:65-136` |
| **Revisi 2** — siklus draft: `Create` / `Post` / `PostDraft` / `Draft` | `internal/ledger/posting_service.go:132, 178, 224, 269` |
| **Revisi 2** — `Post` **tanpa** cek periode (gap U-2) | `internal/ledger/posting_service.go:178-222` |
| **Revisi 2** — `TargetCost` / `TargetPayment` | `internal/approval/model.go:29-30` |
| **Revisi 2** — status request | `internal/approval/model.go:52-56` |
| **Revisi 2** — snapshot beku (anti ubah-setelah-approve) | `internal/approval/model.go:147-150` |
| **Revisi 2** — `Action` append-only (siapa-kapan-apa) | `internal/approval/model.go:164-176` |
| **Revisi 2** — COA lengkap (audit U.5) | `internal/ledger/coa.go:28-100` |
| **Revisi 2** — dokumen menempel di `journal_entries.document_id` | `internal/ledger/document_issuer.go:322, 333, 340` |

---

**Akhir dokumen — Revisi 2 (refinement). Menunggu review pemilik.**
**Tidak ada kode yang ditulis, tidak ada migrasi, tidak ada seed, tidak ada `ALTER`, tidak ada `INSERT`/`UPDATE`/`DELETE`, F-4 tidak disentuh, W-12/W-13 tidak dimulai.**
